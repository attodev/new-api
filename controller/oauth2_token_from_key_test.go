package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
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
)

func setupTokenFromKeyTest(t *testing.T) *oidcTestEnv {
	t.Helper()
	env := setupOIDCProviderTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.Log{}))
	originalLogDB := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = originalLogDB })
	tokenFromKeyLimiter = common.InMemoryRateLimiter{}

	allowIps := "10.9.9.9"
	past := time.Now().Add(-time.Hour).Unix()
	for _, tk := range []model.Token{
		{Id: 1, UserId: 7, Key: tfkAliceKey, Name: "a", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100},
		{Id: 2, UserId: 7, Key: tfkExhaustedKey, Name: "b", Status: common.TokenStatusExhausted, ExpiredTime: -1, RemainQuota: 0},
		{Id: 3, UserId: 7, Key: tfkDisabledKey, Name: "c", Status: common.TokenStatusDisabled, ExpiredTime: -1},
		{Id: 4, UserId: 7, Key: tfkExpiredKey, Name: "d", Status: common.TokenStatusEnabled, ExpiredTime: past},
		{Id: 5, UserId: 9, Key: tfkCarolKey, Name: "e", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 6, UserId: 8, Key: tfkBobKey, Name: "f", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 7, UserId: 7, Key: tfkIPLimitedKey, Name: "g", Status: common.TokenStatusEnabled, ExpiredTime: -1, AllowIps: &allowIps},
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

func (e *oidcTestEnv) tokenFromKey(t *testing.T, authorization string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token-from-key", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res
}

func (e *oidcTestEnv) introspect(t *testing.T, token string, secret string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(oidcTestClientId, secret)
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res
}

// webLoginUserinfo runs the browser code flow for userId and returns userinfo.
func (e *oidcTestEnv) webLoginUserinfo(t *testing.T, userId string) map[string]any {
	t.Helper()
	cookie := e.login(t, userId)
	status, res := e.authorize(t, cookie, userId, baseAuthorizeParams(""))
	require.Equal(t, http.StatusOK, status, res)
	code := redirectURL(t, res).Query().Get("code")
	status, tok, _ := e.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}}, true)
	require.Equal(t, http.StatusOK, status, tok)
	status, info, _ := e.userinfo(t, tok["access_token"].(string))
	require.Equal(t, http.StatusOK, status)
	return info
}

func TestTokenFromKey_IssuesUserinfoTokenMatchingWebLogin(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusOK, status, res)
	require.Equal(t, "Bearer", res["token_type"])
	require.EqualValues(t, 600, res["expires_in"])
	access := res["access_token"].(string)
	require.NotEmpty(t, access)

	status, info, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, env.webLoginUserinfo(t, "7"), info, "userinfo must match the web login exactly")
	require.Equal(t, "7", info["sub"])
	require.Equal(t, "alice@example.com", info["email"])

	// the token is useless anywhere else
	for _, path := range []string{"/test/relay", "/test/user"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+access)
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, req)
		// UserAuth answers 200 {success:false}; TokenAuth answers 401
		rejected := rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"success":false`)
		require.True(t, rejected, "%s accepted the OIDC token: %d %s", path, rec.Code, rec.Body.String())
	}

	var logs []model.Log
	require.NoError(t, model.DB.Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "token_id=1")
	require.Contains(t, logs[0].Content, "client_id=openwebui-mcp")
	require.NotContains(t, logs[0].Content, tfkAliceKey)
	require.NotContains(t, logs[0].Content, access)
}

func TestTokenFromKey_QuotaIsIgnored(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	require.Equal(t, http.StatusOK, status, res)
}

func TestTokenFromKey_Rejections(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	cases := []struct {
		auth   string
		status int
		code   string
	}{
		{"", http.StatusUnauthorized, "invalid_key"},
		{"Bearer sk-doesnotexist", http.StatusUnauthorized, "invalid_key"},
		{"Bearer sk-" + tfkDisabledKey, http.StatusUnauthorized, "invalid_key"},
		{"Bearer sk-" + tfkExpiredKey, http.StatusUnauthorized, "invalid_key"},
		{"Bearer sk-" + tfkCarolKey, http.StatusForbidden, "user_disabled"},
		{"Bearer sk-" + tfkBobKey, http.StatusForbidden, "email_required"},
		{"Bearer sk-" + tfkIPLimitedKey, http.StatusForbidden, "ip_not_allowed"},
	}
	for _, tc := range cases {
		status, res := env.tokenFromKey(t, tc.auth)
		require.Equal(t, tc.status, status, tc.auth)
		require.Equal(t, tc.code, res["error"], tc.auth)
		require.NotEmpty(t, res["message"])
	}

	system_setting.GetOAuth2Settings().TokenFromKeyEnabled = false
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "feature_disabled", res["error"])
}

func TestTokenFromKey_PerKeyRateLimit(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	system_setting.GetOAuth2Settings().TokenFromKeyRateLimit = 2
	for i := 0; i < 2; i++ {
		status, _ := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
		require.Equal(t, http.StatusOK, status)
	}
	status, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "rate_limited", res["error"])
	// another key of the same user is counted separately
	status, _ = env.tokenFromKey(t, "Bearer sk-"+tfkExhaustedKey)
	require.Equal(t, http.StatusOK, status)
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
	require.Equal(t, false, res["active"])
}

func TestIntrospect(t *testing.T) {
	env := setupTokenFromKeyTest(t)
	_, res := env.tokenFromKey(t, "Bearer sk-"+tfkAliceKey)
	access := res["access_token"].(string)

	status, res := env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, res["active"])
	require.Equal(t, "openwebui-mcp", res["client_id"])
	require.Equal(t, "7", res["sub"])
	require.Equal(t, tokenFromKeyScope, res["scope"])
	require.NotNil(t, res["exp"])

	status, _ = env.introspect(t, access, "wrong-secret-xxxxxxxx")
	require.Equal(t, http.StatusUnauthorized, status)

	status, res = env.introspect(t, "unknown", oidcTestClientSecret)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, map[string]any{"active": false}, res)

	// web-login tokens are introspectable too, under the Open WebUI client_id
	web, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: 7, ClientId: oidcTestClientId, Scope: "openid"})
	require.NoError(t, err)
	_, res = env.introspect(t, web, oidcTestClientSecret)
	require.Equal(t, oidcTestClientId, res["client_id"])

	// a disabled owner makes the token inactive
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	_, res = env.introspect(t, access, oidcTestClientSecret)
	require.Equal(t, false, res["active"])
}

func TestDiscovery_AdvertisesIntrospection(t *testing.T) {
	setupTokenFromKeyTest(t)
	require.Equal(t, oidcTestIssuer+"/oauth2/introspect", oidc.DiscoveryDocument()["introspection_endpoint"])
}
