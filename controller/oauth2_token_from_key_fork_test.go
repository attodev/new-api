// fork: O13 — POST /oauth2/token-from-key and POST /oauth2/introspect (ported from OpenWebUI controller/oauth2_token_from_key_test.go; the issuance audit goes to develop's audit_logs table, and relay/dashboard rejection is checked against develop's TokenAuth and UserAuth).

package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

const (
	tfkAliceKey     = "alicekey000000000000000000000000000000000000000"
	tfkExhaustedKey = "exhaustedkey0000000000000000000000000000000000"
	tfkDisabledKey  = "disabledkey00000000000000000000000000000000000"
	tfkExpiredKey   = "expiredkey000000000000000000000000000000000000"
	tfkCarolKey     = "carolkey000000000000000000000000000000000000000"
	tfkBobKey       = "bobkey00000000000000000000000000000000000000000"
	tfkIPLimitedKey = "iplimitedkey0000000000000000000000000000000000"
	tfkIPAllowedKey = "ipallowedkey0000000000000000000000000000000000"
	tfkStatusExpKey = "statusexpiredkey00000000000000000000000000000"

	// tfkRemoteIP is httptest.NewRequest's default RemoteAddr host.
	tfkRemoteIP = "192.0.2.1"
)

func setupTokenFromKeyTest(t *testing.T) *oidcTestEnv {
	t.Helper()
	env := setupOIDCProviderTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	// develop's commonKeyCol (used by model.GetTokenByKey) is set by
	// InitLogDB; with LOG_SQL_DSN empty it only points LOG_DB at DB.
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	resetTokenFromKeyLimiterForTest()
	t.Cleanup(resetTokenFromKeyLimiterForTest)

	// carol's account is disabled; bob has no email.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 9).Update("status", common.UserStatusDisabled).Error)

	denied, allowed := "10.9.9.9", tfkRemoteIP+"/32"
	past := time.Now().Add(-time.Hour).Unix()
	for _, tk := range []model.Token{
		{Id: 1, UserId: 7, Key: tfkAliceKey, Name: "a", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100},
		{Id: 2, UserId: 7, Key: tfkExhaustedKey, Name: "b", Status: common.TokenStatusExhausted, ExpiredTime: -1, RemainQuota: 0},
		{Id: 3, UserId: 7, Key: tfkDisabledKey, Name: "c", Status: common.TokenStatusDisabled, ExpiredTime: -1},
		{Id: 4, UserId: 7, Key: tfkExpiredKey, Name: "d", Status: common.TokenStatusEnabled, ExpiredTime: past},
		{Id: 5, UserId: 9, Key: tfkCarolKey, Name: "e", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 6, UserId: 8, Key: tfkBobKey, Name: "f", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 7, UserId: 7, Key: tfkIPLimitedKey, Name: "g", Status: common.TokenStatusEnabled, ExpiredTime: -1, AllowIps: &denied},
		{Id: 8, UserId: 7, Key: tfkIPAllowedKey, Name: "h", Status: common.TokenStatusEnabled, ExpiredTime: -1, AllowIps: &allowed},
		{Id: 9, UserId: 7, Key: tfkStatusExpKey, Name: "i", Status: common.TokenStatusExpired, ExpiredTime: -1},
	} {
		require.NoError(t, model.DB.Create(&tk).Error)
	}

	s := system_setting.GetOAuth2Settings()
	s.TokenFromKeyEnabled = true
	s.TokenFromKeyClientId = system_setting.DefaultTokenFromKeyClientId
	s.TokenFromKeyTTLSeconds = system_setting.DefaultTokenFromKeyTTL
	s.TokenFromKeyRateLimit = system_setting.DefaultTokenFromKeyRate

	env.engine.POST("/oauth2/token-from-key", OAuth2TokenFromKey)
	env.engine.POST("/oauth2/introspect", OAuth2Introspect)
	env.engine.GET("/test/relay", middleware.TokenAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })
	env.engine.GET("/test/user", middleware.UserAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })
	return env
}

func (e *oidcTestEnv) tokenFromKeyFrom(t *testing.T, authorization string, mutate func(*http.Request)) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-from-key", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func (e *oidcTestEnv) tokenFromKey(t *testing.T, authorization string) (int, map[string]any) {
	t.Helper()
	status, res, _ := e.tokenFromKeyFrom(t, authorization, nil)
	return status, res
}

func (e *oidcTestEnv) introspectWith(t *testing.T, form url.Values, basicId, basicSecret string) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicId != "" || basicSecret != "" {
		req.SetBasicAuth(basicId, basicSecret)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func (e *oidcTestEnv) introspect(t *testing.T, token string, secret string) (int, map[string]any) {
	t.Helper()
	status, res, _ := e.introspectWith(t, url.Values{"token": {token}, "token_type_hint": {"access_token"}}, oidcTestClientId, secret)
	return status, res
}

// webLoginUserinfo runs the browser code flow for userId and returns userinfo.
func (e *oidcTestEnv) webLoginUserinfo(t *testing.T, userId int) map[string]any {
	t.Helper()
	login := e.login(t, userId)
	status, res := e.authorize(t, login.access, baseAuthorizeParams(""))
	require.Equal(t, http.StatusOK, status, res)
	code := redirectURL(t, res).Query().Get("code")
	status, tok, _ := e.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}}, true)
	require.Equal(t, http.StatusOK, status, tok)
	status, info, _ := e.userinfo(t, tok["access_token"].(string))
	require.Equal(t, http.StatusOK, status)
	return info
}

func requireTokenFromKeyError(t *testing.T, status int, res map[string]any, wantStatus int, wantCode, msg string) {
	t.Helper()
	require.Equal(t, wantStatus, status, "%s: %v", msg, res)
	require.Equal(t, wantCode, res["error"], msg)
	require.NotEmpty(t, res["message"], msg)
	require.Equal(t, res["message"], res["error_description"], msg)
	require.NotContains(t, res, "access_token", msg)
}

// Security requirement 1: the token works for userinfo and introspection, and
// nowhere else — not as a relay API key and not as a dashboard credential.
func TestTokenFromKey_IssuesUserinfoOnlyTokenMatchingWebLogin(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	status, res, hdr := env.tokenFromKeyFrom(t, "Bearer sk-"+tfkAliceKey, nil)
	require.Equal(t, http.StatusOK, status, res)
	require.Equal(t, "Bearer", res["token_type"])
	require.EqualValues(t, 600, res["expires_in"])
	require.Equal(t, "no-store", hdr.Get("Cache-Control"))
	access := res["access_token"].(string)
	require.Len(t, access, 48)

	status, info, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, env.webLoginUserinfo(t, 7), info, "userinfo must match the web login exactly")
	require.Equal(t, "7", info["sub"])
	require.Equal(t, "alice@example.com", info["email"])

	status, intro := env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, intro["active"])

	// A channel suffix and a missing sk- prefix parse like TokenAuth.
	status, _ = env.tokenFromKey(t, "bearer "+tfkAliceKey+"-12")
	require.Equal(t, http.StatusOK, status)

	// The token is useless as a relay key and as a dashboard credential.
	for _, path := range []string{"/test/relay", "/test/user"} {
		for _, header := range []string{"Bearer " + access, "Bearer sk-" + access} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()
			env.engine.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "%s accepted the OIDC token: %s", path, rec.Body.String())
		}
	}
	// The API key itself is still a working relay key (sanity check of the probe).
	req := httptest.NewRequest(http.MethodGet, "/test/relay", nil)
	req.Header.Set("Authorization", "Bearer sk-"+tfkAliceKey)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// Security requirement 7: one audit row per issued token, with the user,
// API key id, client_id and lifetime — never the key or the token.
func TestTokenFromKey_AuditWithoutSecrets(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusOK, status, res)
	access := res["access_token"].(string)

	// Rejections are not audited.
	status, _ = env.tokenFromKey(t, "Bearer sk-"+tfkDisabledKey)
	require.Equal(t, http.StatusUnauthorized, status)

	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&audits).Error)
	require.Len(t, audits, 1)
	audit := audits[0]
	require.Equal(t, "oidc.token_from_key.issue", audit.Action)
	require.Equal(t, model.AuditCategorySecurity, audit.Category)
	require.Equal(t, 7, audit.UserId)
	require.Equal(t, "alice", audit.Username)
	require.Equal(t, common.RoleCommonUser, audit.ActorRole)
	require.Equal(t, "api_key", audit.AuthMethod)
	require.Equal(t, tfkRemoteIP, audit.Ip)
	require.True(t, audit.Success)
	require.NotNil(t, audit.Other.Op)
	params, err := common.Marshal(audit.Other.Op.Params)
	require.NoError(t, err)
	require.JSONEq(t, `{"user_id":7,"token_id":1,"client_id":"openwebui-mcp","ttl_seconds":600}`, string(params))

	row, err := common.Marshal(audit)
	require.NoError(t, err)
	for _, secret := range []string{tfkAliceKey, access} {
		require.NotContains(t, string(row), secret)
	}
}

// Security requirement 2: quota is ignored, but disabled, expired and unknown
// keys are refused, and the key's IP allow-list applies.
func TestTokenFromKey_KeyChecks(t *testing.T) {
	env := setupTokenFromKeyTest(t)

	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	require.Equal(t, http.StatusOK, status, "an exhausted key still identifies its owner: %v", res)

	for _, auth := range []string{
		"",
		"Bearer ",
		"Basic " + tfkAliceKey,
		"Bearer sk-doesnotexist",
		"Bearer sk-" + tfkDisabledKey,
		"Bearer sk-" + tfkExpiredKey,
		"Bearer sk-" + tfkStatusExpKey,
	} {
		status, res := env.tokenFromKey(t, auth)
		requireTokenFromKeyError(t, status, res, http.StatusUnauthorized, "invalid_key", auth)
	}

	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkIPLimitedKey)
	requireTokenFromKeyError(t, status, res, http.StatusForbidden, "ip_not_allowed", "ip limited")
	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkIPAllowedKey)
	require.Equal(t, http.StatusOK, status, res)

	// A deleted key is unknown.
	require.NoError(t, model.DB.Delete(&model.Token{Id: 1}).Error)
	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	requireTokenFromKeyError(t, status, res, http.StatusUnauthorized, "invalid_key", "deleted")
}

// Security requirement 3: the owner must be enabled and have an email.
func TestTokenFromKey_UserChecks(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkCarolKey)
	requireTokenFromKeyError(t, status, res, http.StatusForbidden, "user_disabled", "disabled user")
	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkBobKey)
	requireTokenFromKeyError(t, status, res, http.StatusForbidden, "email_required", "no email")
}

// Security requirement 4: nothing is issued while the provider is off or
// unconfigured (404, like every other OIDC endpoint) or the feature switch is
// off (403 feature_disabled, the code the Open WebUI MCP side expects).
func TestTokenFromKey_Availability(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	s := system_setting.GetOAuth2Settings()

	s.TokenFromKeyEnabled = false
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	requireTokenFromKeyError(t, status, res, http.StatusForbidden, "feature_disabled", "switch off")
	s.TokenFromKeyEnabled = true

	for name, mutate := range map[string]func(*system_setting.OAuth2Settings){
		"provider off":       func(s *system_setting.OAuth2Settings) { s.Enabled = false },
		"no client secret":   func(s *system_setting.OAuth2Settings) { s.ClientSecret = "" },
		"blank redirect uri": func(s *system_setting.OAuth2Settings) { s.RedirectURI = " " },
	} {
		saved := *s
		mutate(s)
		status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
		require.Equal(t, http.StatusNotFound, status, name)
		require.Equal(t, "oauth2 is not enabled", res["error"], name)
		status, _ = env.introspect(t, "anything", oidcTestClientSecret)
		require.Equal(t, http.StatusNotFound, status, name)
		*s = saved
	}

	// Nothing was issued or audited along the way.
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
}

// Security requirement 5: per-key limit (configurable).
func TestTokenFromKey_PerKeyRateLimit(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	system_setting.GetOAuth2Settings().TokenFromKeyRateLimit = 2
	for i := 0; i < 2; i++ {
		status, _ := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
		require.Equal(t, http.StatusOK, status)
	}
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	requireTokenFromKeyError(t, status, res, http.StatusTooManyRequests, "rate_limited", "per key")
	// Another key of the same user is counted separately.
	status, _ = env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	require.Equal(t, http.StatusOK, status)
}

// Security requirement 5: per-IP limit, counted before the key is looked up
// so guessing keys from one address is slow; the address is develop's
// trusted client IP, so a spoofed X-Forwarded-For does not reset it.
func TestTokenFromKey_PerIPRateLimit(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	require.NoError(t, common.ConfigureTrustedProxies(env.engine, nil)) // TRUSTED_PROXIES=none

	for i := 0; i < tokenFromKeyIPRate; i++ {
		spoof := func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113."+strconv.Itoa(i%250)) }
		status, _, _ := env.tokenFromKeyFrom(t, "Bearer sk-guess"+strconv.Itoa(i), spoof)
		require.Equal(t, http.StatusUnauthorized, status, i)
	}
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	requireTokenFromKeyError(t, status, res, http.StatusTooManyRequests, "rate_limited", "per ip")

	// Another client address is unaffected.
	otherIP := func(r *http.Request) { r.RemoteAddr = "198.51.100.7:4321" }
	status, res, _ = env.tokenFromKeyFrom(t, "Bearer sk-"+tfkAliceKey, otherIP)
	require.Equal(t, http.StatusOK, status, res)
}

// Security requirement 5 with Redis on (multi-node): both limits are shared
// counters in Redis.
func TestTokenFromKey_RateLimits_Redis(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB, originalEnabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB, common.RedisEnabled = originalRDB, originalEnabled
		oidc.ResetStoresForTest()
	})
	oidc.ResetStoresForTest()
	require.NoError(t, model.PublishUserAuthCache(7))

	system_setting.GetOAuth2Settings().TokenFromKeyRateLimit = 1
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusOK, status, res)
	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	requireTokenFromKeyError(t, status, res, http.StatusTooManyRequests, "rate_limited", "per key in redis")
	require.True(t, mr.Exists("rateLimit:OIDCK:key:1"), mr.Keys())
	require.True(t, mr.Exists("rateLimit:OIDCK:ip:"+tfkRemoteIP), mr.Keys())
	require.Greater(t, mr.TTL("rateLimit:OIDCK:key:1"), time.Duration(0))

	// The per-IP counter is shared the same way.
	mr.Set("rateLimit:OIDCK:ip:"+tfkRemoteIP, strconv.Itoa(tokenFromKeyIPRate))
	status, res = env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	requireTokenFromKeyError(t, status, res, http.StatusTooManyRequests, "rate_limited", "per ip in redis")
}

func TestTokenFromKey_ExpiredTokenRejected(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	access, err := oidc.IssueAccessTokenWithTTL(oidc.AccessToken{UserId: 7, ClientId: "openwebui-mcp", Scope: tokenFromKeyScope}, time.Second)
	require.NoError(t, err)
	status, _, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status)
	time.Sleep(1100 * time.Millisecond)
	status, _, _ = env.userinfo(t, access)
	require.Equal(t, http.StatusUnauthorized, status)
	_, res := env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, map[string]any{"active": false}, res)
}

// Security requirement 6: introspection.
func TestIntrospect(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	_, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	access := res["access_token"].(string)

	status, res, hdr := env.introspectWith(t, url.Values{"token": {access}}, oidcTestClientId, oidcTestClientSecret)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "no-store", hdr.Get("Cache-Control"))
	require.Equal(t, true, res["active"])
	require.Equal(t, "openwebui-mcp", res["client_id"])
	require.Equal(t, "7", res["sub"])
	require.Equal(t, "alice", res["username"])
	require.Equal(t, tokenFromKeyScope, res["scope"])
	require.Equal(t, "Bearer", res["token_type"])
	require.Equal(t, oidcTestIssuer, res["iss"])
	exp, iat := res["exp"].(float64), res["iat"].(float64)
	require.InDelta(t, float64(time.Now().Unix()), iat, 5)
	require.Equal(t, float64(600), exp-iat)
	// Only RFC 7662 claims: no token, key, secret or user record.
	allowed := map[string]bool{"active": true, "client_id": true, "scope": true, "sub": true, "username": true, "token_type": true, "iss": true, "exp": true, "iat": true}
	for k := range res {
		require.True(t, allowed[k], "unexpected introspection field %q", k)
	}
	body, err := common.Marshal(res)
	require.NoError(t, err)
	for _, secret := range []string{access, tfkAliceKey, oidcTestClientSecret} {
		require.NotContains(t, string(body), secret)
	}

	// client_secret_post works too.
	status, res, _ = env.introspectWith(t, url.Values{"token": {access}, "client_id": {oidcTestClientId}, "client_secret": {oidcTestClientSecret}}, "", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, res["active"])

	// Client authentication failures.
	for name, tc := range map[string]struct {
		form       url.Values
		id, secret string
	}{
		"wrong secret":   {url.Values{"token": {access}}, oidcTestClientId, "wrong-secret-xxxxxxxx"},
		"wrong client":   {url.Values{"token": {access}}, "openwebui-mcp", oidcTestClientSecret},
		"no credentials": {url.Values{"token": {access}}, "", ""},
		"wrong post":     {url.Values{"token": {access}, "client_id": {oidcTestClientId}, "client_secret": {"nope"}}, "", ""},
	} {
		status, res, hdr := env.introspectWith(t, tc.form, tc.id, tc.secret)
		require.Equal(t, http.StatusUnauthorized, status, name)
		require.Equal(t, "invalid_client", res["error"], name)
		require.NotEmpty(t, hdr.Get("WWW-Authenticate"), name)
		require.NotContains(t, res, "active", name)
	}

	for _, unknown := range []string{"unknown", ""} {
		status, res = env.introspect(t, unknown, oidcTestClientSecret)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, map[string]any{"active": false}, res)
	}

	// Web-login tokens are introspectable too, under the Open WebUI client_id.
	web, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: 7, ClientId: oidcTestClientId, Scope: "openid"})
	require.NoError(t, err)
	_, res = env.introspect(t, web, oidcTestClientSecret)
	require.Equal(t, true, res["active"])
	require.Equal(t, oidcTestClientId, res["client_id"])
	require.Equal(t, float64(3600), res["exp"].(float64)-res["iat"].(float64))

	// A disabled owner makes the token inactive (and revokes it).
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	_, res = env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, map[string]any{"active": false}, res)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusEnabled).Error)
	_, res = env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, map[string]any{"active": false}, res, "the token stays revoked")
}

func TestDiscovery_AdvertisesIntrospection(t *testing.T) {
	setupTokenFromKeyTest(t)
	doc := oidc.DiscoveryDocument()
	require.Equal(t, oidcTestIssuer+"/oauth2/introspect", doc["introspection_endpoint"])
	require.Equal(t, []string{"client_secret_basic", "client_secret_post"}, doc["introspection_endpoint_auth_methods_supported"])
}

// A Redis counter left without a TTL (e.g. an EXPIRE that failed after
// INCR) must not block that IP or key for good: every hit re-arms the window.
func TestTokenFromKey_RateLimit_Redis_RearmsMissingTTL(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB, originalEnabled := common.RDB, common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB, common.RedisEnabled = originalRDB, originalEnabled
		oidc.ResetStoresForTest()
	})
	oidc.ResetStoresForTest()
	require.NoError(t, model.PublishUserAuthCache(7))

	ipKey, keyKey := "rateLimit:OIDCK:ip:"+tfkRemoteIP, "rateLimit:OIDCK:key:1"
	require.NoError(t, mr.Set(ipKey, "3"))
	require.NoError(t, mr.Set(keyKey, "3"))
	require.Zero(t, mr.TTL(ipKey), "precondition: no TTL")

	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusOK, status, res)
	for _, k := range []string{ipKey, keyKey} {
		ttl := mr.TTL(k)
		require.Greater(t, ttl, time.Duration(0), k)
		require.LessOrEqual(t, ttl, time.Duration(tokenFromKeyWindow)*time.Second, k)
	}
	v, err := mr.Get(keyKey)
	require.NoError(t, err)
	require.Equal(t, "4", v)

	// Once the window passes the counters are gone and requests flow again.
	system_setting.GetOAuth2Settings().TokenFromKeyRateLimit = 4
	status, _ = env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusTooManyRequests, status)
	mr.FastForward(time.Duration(tokenFromKeyWindow+1) * time.Second)
	status, _ = env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusOK, status)
}

// The Open WebUI service key is a shared relay credential, not a person's
// key: it never identifies anyone, whether or not the relay is on.
func TestTokenFromKey_RefusesOpenWebUIServiceKey(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	original := *system_setting.GetOpenWebUISettings()
	t.Cleanup(func() { *system_setting.GetOpenWebUISettings() = original })

	for _, relayOn := range []bool{false, true} {
		ow := system_setting.GetOpenWebUISettings()
		ow.ServiceApiKey = tfkAliceKey
		ow.RelayEnabled = relayOn
		for _, auth := range []string{"Bearer sk-" + tfkAliceKey, "Bearer " + tfkAliceKey + "-3"} {
			status, res := env.tokenFromKey(t, auth)
			requireTokenFromKeyError(t, status, res, http.StatusUnauthorized, "invalid_key", auth)
		}
	}
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)

	// Other keys of the same user still work.
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	require.Equal(t, http.StatusOK, status, res)
}

// Settings loaded from the database at startup skip option validation, so
// issuance itself refuses a token-from-key client_id that collides with the
// Open WebUI client_id (ignoring case and spaces, as MySQL *_ci would).
func TestTokenFromKey_RefusesClientIdCollision(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	s := system_setting.GetOAuth2Settings()
	for _, tc := range []struct{ clientId, tfkClientId string }{
		{"openwebui-mcp", "openwebui-mcp"},
		{" OpenWebUI-MCP ", "openwebui-mcp"},
		{"openwebui-mcp", ""}, // blank means the default openwebui-mcp
		{"openwebui", " OPENWEBUI "},
	} {
		s.ClientId, s.TokenFromKeyClientId = tc.clientId, tc.tfkClientId
		status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
		requireTokenFromKeyError(t, status, res, http.StatusForbidden, "feature_disabled", tc.clientId+"/"+tc.tfkClientId)
	}
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
}
