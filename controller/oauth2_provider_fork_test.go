// fork: O13 — ported from OpenWebUI controller/oauth2_provider_test.go; logins use develop's login sessions (Bearer access token) instead of gin cookie sessions.

package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	oidcTestClientId     = "openwebui"
	oidcTestClientSecret = "0123456789abcdef"
	oidcTestRedirect     = "https://chat.example.com/oauth/oidc/login/callback"
	oidcTestIssuer       = "https://newapi.example.com"
	oidcTestPostLogout   = "https://chat.example.com/bye"
)

type oidcTestEnv struct {
	engine *gin.Engine
}

// oidcLogin is one develop login session: the in-memory access token the SPA
// sends as Bearer, the refresh cookie value and the session id.
type oidcLogin struct {
	access  string
	refresh string
	sid     string
}

// setupOIDCTestDB points model.DB and model.LOG_DB at a fresh SQLite file
// (WAL + immediate transactions, like develop's session tests) and restores
// every global it touches.
func setupOIDCTestDB(t *testing.T, tables ...any) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMain, originalLog := common.MainDatabaseType(), common.LogDatabaseType()
	originalRedis, originalSecret := common.RedisEnabled, common.SessionSecret
	originalOptionMap := common.OptionMap
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.SessionSecret = "oidc-provider-test-secret"
	common.OptionMap = make(map[string]string)

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/oidc.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	base := []any{&model.User{}, &model.UserSession{}, &model.Option{}, &model.AuditLog{}}
	require.NoError(t, db.AutoMigrate(append(base, tables...)...))
	model.DB, model.LOG_DB = db, db

	// Rejection paths log warnings; keep the test output quiet.
	common.LogWriterMu.Lock()
	originalWriter, originalErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = io.Discard, io.Discard
	common.LogWriterMu.Unlock()

	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = originalWriter, originalErrorWriter
		common.LogWriterMu.Unlock()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMain, originalLog)
		common.RedisEnabled, common.SessionSecret = originalRedis, originalSecret
		common.OptionMap = originalOptionMap
	})
	return db
}

func setupOIDCProviderTest(t *testing.T) *oidcTestEnv {
	t.Helper()
	db := setupOIDCTestDB(t, &model.Organization{})

	originalAddr := system_setting.ServerAddress
	system_setting.ServerAddress = oidcTestIssuer
	oidc.ResetKeyCacheForTest()
	oidc.ResetStoresForTest()

	require.NoError(t, db.Create(&model.Organization{Id: 3, Name: "Acme", OwnerUserId: 7}).Error)
	// users.aff_code has a unique index, so every fixture user needs its own.
	for _, u := range []model.User{
		{Id: 7, Username: "alice", Password: "secret-pass", DisplayName: "Alice", Email: "Alice@Example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", OrganizationId: 3, AuthVersion: 1},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, Group: "vip", AuthVersion: 1},
		{Id: 9, Username: "carol", Password: "secret-pass", Email: "carol@example.com", AffCode: "aff-carol", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", AuthVersion: 1},
	} {
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, model.PublishUserAuthCache(u.Id))
	}

	originalSettings := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{
		Enabled: true, ClientId: oidcTestClientId, ClientSecret: oidcTestClientSecret,
		RedirectURI: oidcTestRedirect, AppUrl: "https://chat.example.com",
		PostLogoutRedirectURI: oidcTestPostLogout,
	}

	t.Cleanup(func() {
		system_setting.ServerAddress = originalAddr
		*system_setting.GetOAuth2Settings() = originalSettings
		oidc.ResetKeyCacheForTest()
		oidc.ResetStoresForTest()
	})

	engine := gin.New()
	engine.GET("/.well-known/openid-configuration", OAuth2Discovery)
	engine.GET("/oauth2/jwks", OAuth2JWKS)
	engine.POST("/oauth2/token", OAuth2Token)
	engine.GET("/oauth2/userinfo", OAuth2UserInfo)
	engine.POST("/api/oauth2/authorize", middleware.UserAuth(), OAuth2Authorize)
	return &oidcTestEnv{engine: engine}
}

// login opens a develop login session for userId, the way a password sign-in does.
func (e *oidcTestEnv) login(t *testing.T, userId int) oidcLogin {
	t.Helper()
	bundle, err := service.CreateLoginSession(userId, "password", "127.0.0.1", "oidc-test")
	require.NoError(t, err)
	return oidcLogin{access: bundle.AccessToken, refresh: bundle.RefreshToken, sid: bundle.Session.SID}
}

func (e *oidcTestEnv) authorize(t *testing.T, access string, params map[string]string) (int, map[string]any) {
	t.Helper()
	body, err := common.Marshal(params)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/authorize", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	}
	return rec.Code, res
}

func baseAuthorizeParams(verifierChallenge string) map[string]string {
	p := map[string]string{
		"response_type": "code", "client_id": oidcTestClientId, "redirect_uri": oidcTestRedirect,
		"scope": "openid email profile", "state": "st-1", "nonce": "n-1",
	}
	if verifierChallenge != "" {
		p["code_challenge"] = verifierChallenge
		p["code_challenge_method"] = "S256"
	}
	return p
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func redirectURL(t *testing.T, res map[string]any) *url.URL {
	t.Helper()
	data, _ := res["data"].(map[string]any)
	require.NotNil(t, data, "expected data.redirect_url in %v", res)
	u, err := url.Parse(data["redirect_url"].(string))
	require.NoError(t, err)
	return u
}

// requireHardReject asserts a non-redirectable authorize refusal: no
// redirect_url is handed to the browser, and the page gets a translatable
// message.
func requireHardReject(t *testing.T, res map[string]any, code, message string) {
	t.Helper()
	require.Equal(t, false, res["success"], res)
	require.Equal(t, code, res["error"], res)
	_, hasData := res["data"]
	require.False(t, hasData, "must not hand the browser a redirect: %v", res)
	require.Equal(t, message, res["message"])
	require.Equal(t, message, res["message_key"])
}

func (e *oidcTestEnv) token(t *testing.T, form url.Values, basic bool) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		req.SetBasicAuth(oidcTestClientId, oidcTestClientSecret)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func (e *oidcTestEnv) userinfo(t *testing.T, bearer string) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func (e *oidcTestEnv) get(t *testing.T, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestOIDC_DiscoveryAndJWKS(t *testing.T) {
	env := setupOIDCProviderTest(t)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, oidcTestIssuer, doc["issuer"])
	require.Equal(t, oidcTestIssuer+"/oauth2/logout", doc["end_session_endpoint"])

	rec = httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth2/jwks", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var jwks map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &jwks))
	require.Len(t, jwks["keys"].([]any), 1)

	system_setting.GetOAuth2Settings().Enabled = false
	require.Equal(t, http.StatusNotFound, env.get(t, "/.well-known/openid-configuration"))
}

func TestOIDC_Authorize_RequiresLogin(t *testing.T) {
	env := setupOIDCProviderTest(t)
	code, _ := env.authorize(t, "", baseAuthorizeParams(""))
	require.Equal(t, http.StatusUnauthorized, code)

	// A revoked login session is no login either.
	login := env.login(t, 7)
	_, err := model.RevokeUserSession(7, login.sid, "test")
	require.NoError(t, err)
	code, _ = env.authorize(t, login.access, baseAuthorizeParams(""))
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestOIDC_FullCodeFlow_WithPKCEAndBasicAuth(t *testing.T) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 7)
	verifier := "a-very-long-code-verifier-string-0123456789"

	status, res := env.authorize(t, login.access, baseAuthorizeParams(pkceChallenge(verifier)))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, res["success"], res)
	u := redirectURL(t, res)
	require.Equal(t, "https://chat.example.com/oauth/oidc/login/callback", u.Scheme+"://"+u.Host+u.Path)
	code := u.Query().Get("code")
	require.Len(t, code, 48)
	require.Equal(t, "st-1", u.Query().Get("state"))

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {verifier}}
	status, tok, hdr := env.token(t, form, true)
	require.Equal(t, http.StatusOK, status, tok)
	require.Equal(t, "Bearer", tok["token_type"])
	require.Equal(t, float64(3600), tok["expires_in"])
	require.Equal(t, "openid email profile", tok["scope"])
	require.Equal(t, "no-store", hdr.Get("Cache-Control"))
	access := tok["access_token"].(string)
	require.Len(t, access, 48)

	// id_token verifies with the published key and carries the nonce.
	key, err := oidc.CurrentKey()
	require.NoError(t, err)
	parsed, err := jwt.ParseWithClaims(tok["id_token"].(string), &oidc.IDTokenClaims{}, func(*jwt.Token) (any, error) { return &key.Private.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(oidcTestIssuer), jwt.WithAudience(oidcTestClientId))
	require.NoError(t, err)
	idc := parsed.Claims.(*oidc.IDTokenClaims)
	require.Equal(t, "7", idc.Subject)
	require.Equal(t, "n-1", idc.Nonce)
	require.Equal(t, "alice@example.com", idc.Email)
	require.Equal(t, "Alice", idc.Name)

	// userinfo
	status, info, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "7", info["sub"])
	require.Equal(t, "alice@example.com", info["email"])
	require.Equal(t, []any{"user"}, info["roles"])
	require.Equal(t, []any{"default", "Acme"}, info["groups"])

	// code reuse fails
	status, res2, _ := env.token(t, form, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res2["error"])
}

func TestOIDC_Token_ClientSecretPostAndFailures(t *testing.T) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 7)
	verifier := "another-very-long-code-verifier-string-0123456789"

	mint := func() string {
		_, res := env.authorize(t, login.access, baseAuthorizeParams(pkceChallenge(verifier)))
		return redirectURL(t, res).Query().Get("code")
	}

	// client_secret_post works
	code := mint()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {verifier}, "client_id": {oidcTestClientId}, "client_secret": {oidcTestClientSecret}}
	status, _, _ := env.token(t, form, false)
	require.Equal(t, http.StatusOK, status)

	// wrong secret → 401 invalid_client with challenge header
	code = mint()
	form.Set("code", code)
	form.Set("client_secret", "wrong-secret-wrong")
	status, res, hdr := env.token(t, form, false)
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "invalid_client", res["error"])
	require.Contains(t, hdr.Get("WWW-Authenticate"), "Basic")
	// ...and the code was not burned by the failed attempt
	form.Set("client_secret", oidcTestClientSecret)
	status, _, _ = env.token(t, form, false)
	require.Equal(t, http.StatusOK, status)

	// wrong verifier
	code = mint()
	bad := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {"nope"}}
	status, res, _ = env.token(t, bad, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res["error"])

	// wrong redirect_uri
	bad.Set("code_verifier", verifier)
	bad.Set("redirect_uri", "https://evil.example.com/cb")
	status, res, _ = env.token(t, bad, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res["error"])

	// unsupported grant type
	status, res, _ = env.token(t, url.Values{"grant_type": {"client_credentials"}}, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "unsupported_grant_type", res["error"])

	// disabled → 404
	system_setting.GetOAuth2Settings().Enabled = false
	status, _, _ = env.token(t, form, true)
	require.Equal(t, http.StatusNotFound, status)
}

func TestOIDC_Authorize_Rejections(t *testing.T) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 7)

	p := baseAuthorizeParams("")
	p["redirect_uri"] = "https://evil.example.com/cb"
	_, res := env.authorize(t, login.access, p)
	requireHardReject(t, res, "invalid_redirect_uri", oidcInvalidRequestMessage)

	p = baseAuthorizeParams("")
	p["client_id"] = "other"
	_, res = env.authorize(t, login.access, p)
	requireHardReject(t, res, "invalid_client", oidcInvalidRequestMessage)

	p = baseAuthorizeParams("")
	p["scope"] = "email profile"
	_, res = env.authorize(t, login.access, p)
	require.Equal(t, "invalid_scope", res["error"])
	require.Equal(t, "invalid_scope", redirectURL(t, res).Query().Get("error"))
	require.Equal(t, "st-1", redirectURL(t, res).Query().Get("state"))

	p = baseAuthorizeParams("")
	p["response_type"] = "token"
	_, res = env.authorize(t, login.access, p)
	require.Equal(t, "unsupported_response_type", redirectURL(t, res).Query().Get("error"))

	// Malformed JSON body.
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/authorize", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer "+login.access)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	var malformed map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &malformed))
	requireHardReject(t, malformed, "invalid_request", oidcInvalidRequestMessage)

	// A user disabled after logging in is stopped by UserAuth itself: develop
	// reads the live user status, so no redirect is handed out.
	login9 := env.login(t, 9)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 9).Update("status", common.UserStatusDisabled).Error)
	status, res := env.authorize(t, login9.access, baseAuthorizeParams(""))
	require.Equal(t, http.StatusUnauthorized, status)
	_, hasData := res["data"]
	require.False(t, hasData)
}

// PKCE: only S256 is accepted when a code is issued; a request may also carry
// no PKCE at all. "plain" (or a method without a challenge) is refused before
// a code exists.
func TestOIDC_Authorize_RejectsNonS256PKCE(t *testing.T) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 7)

	for _, tc := range []struct{ challenge, method string }{
		{"x", "plain"},
		{"x", ""},
		{"x", "s256"},
		{"", "plain"},
		{"", "S256"},
	} {
		p := baseAuthorizeParams("")
		if tc.challenge != "" {
			p["code_challenge"] = tc.challenge
		}
		if tc.method != "" {
			p["code_challenge_method"] = tc.method
		}
		_, res := env.authorize(t, login.access, p)
		require.Equal(t, false, res["success"], tc)
		require.Equal(t, "invalid_request", res["error"], tc)
		u := redirectURL(t, res)
		require.Equal(t, "invalid_request", u.Query().Get("error"), tc)
		require.Empty(t, u.Query().Get("code"), tc)
	}

	// No PKCE at all is allowed (Open WebUI's default).
	_, res := env.authorize(t, login.access, baseAuthorizeParams(""))
	require.Equal(t, true, res["success"], res)
}

// The option validator lets an admin clear oauth2.redirect_uri while the
// provider is on; an empty (or blank) value means "not configured", which
// must look like a disabled provider on every endpoint.
func TestOIDC_EmptyRedirectURIMeansNotConfigured(t *testing.T) {
	for name, blank := range map[string]string{"empty": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			testOIDCRedirectURINotConfigured(t, blank)
		})
	}
}

func testOIDCRedirectURINotConfigured(t *testing.T, blank string) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 7)
	system_setting.GetOAuth2Settings().RedirectURI = blank

	p := baseAuthorizeParams("")
	p["redirect_uri"] = blank
	_, res := env.authorize(t, login.access, p)
	requireHardReject(t, res, "invalid_client", oidcInvalidRequestMessage)

	// A code minted for the blank redirect_uri (e.g. before the setting was
	// cleared) cannot be exchanged either.
	code, err := oidc.IssueCode(oidc.AuthCode{UserId: 7, ClientId: oidcTestClientId, RedirectURI: blank, Scope: "openid", AuthTime: common.GetTimestamp()})
	require.NoError(t, err)
	status, tok, _ := env.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {blank}}, true)
	require.Equal(t, http.StatusNotFound, status, tok)
	_, hasToken := tok["access_token"]
	require.False(t, hasToken)

	require.Equal(t, http.StatusNotFound, env.get(t, "/.well-known/openid-configuration"))
	require.Equal(t, http.StatusNotFound, env.get(t, "/oauth2/jwks"))
}

// Users without an email cannot sign in to the external app: a page message,
// never a redirect.
func TestOIDC_Authorize_EmailRequired(t *testing.T) {
	env := setupOIDCProviderTest(t)
	login := env.login(t, 8)
	_, res := env.authorize(t, login.access, baseAuthorizeParams(""))
	requireHardReject(t, res, "email_required", oidcEmailRequiredMessage)

	// Whitespace is no email either.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 8).Update("email", "   ").Error)
	_, res = env.authorize(t, login.access, baseAuthorizeParams(""))
	requireHardReject(t, res, "email_required", oidcEmailRequiredMessage)
}

// The handler's own status check (reached when the dashboard auth cache is
// still behind) answers with an access_denied redirect.
func TestOIDC_Authorize_HandlerDeniesDisabledUser(t *testing.T) {
	setupOIDCProviderTest(t)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	for _, id := range []int{7, 404} {
		body, err := common.Marshal(baseAuthorizeParams(""))
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth2/authorize", strings.NewReader(string(body)))
		c.Set("id", id)
		OAuth2Authorize(c)
		var res map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
		require.Equal(t, "access_denied", res["error"], id)
		require.Equal(t, "access_denied", redirectURL(t, res).Query().Get("error"))
		require.Equal(t, "st-1", redirectURL(t, res).Query().Get("state"))
	}
}

func TestOIDC_UserInfo_Failures(t *testing.T) {
	env := setupOIDCProviderTest(t)
	status, res, hdr := env.userinfo(t, "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "invalid_token", res["error"])
	require.Contains(t, hdr.Get("WWW-Authenticate"), "Bearer")

	status, _, _ = env.userinfo(t, "unknown-token")
	require.Equal(t, http.StatusUnauthorized, status)

	// A dashboard access token is not an OIDC access token.
	login := env.login(t, 7)
	status, _, _ = env.userinfo(t, login.access)
	require.Equal(t, http.StatusUnauthorized, status)

	// token for a user who is disabled afterwards
	access, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: 7, ClientId: oidcTestClientId, Scope: "openid"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	status, _, _ = env.userinfo(t, access)
	require.Equal(t, http.StatusUnauthorized, status)
	_, found, _ := oidc.LookupAccessToken(access)
	require.False(t, found, "token revoked once the user is found disabled")
}

// RFC 6749 §2.3.1: client_secret_basic credentials are form-urlencoded
// before being base64-encoded, so the server must decode them.
func TestOIDC_Token_BasicAuthCredentialsAreFormDecoded(t *testing.T) {
	env := setupOIDCProviderTest(t)
	const secret = "s3cr+t/=:@#&!%16chars"
	system_setting.GetOAuth2Settings().ClientSecret = secret
	login := env.login(t, 7)
	_, res := env.authorize(t, login.access, baseAuthorizeParams(""))
	code := redirectURL(t, res).Query().Get("code")
	require.NotEmpty(t, code)

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(oidcTestClientId), url.QueryEscape(secret))
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
