package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	oidcTestClientId     = "openwebui"
	oidcTestClientSecret = "0123456789abcdef"
	oidcTestRedirect     = "https://chat.example.com/oauth/oidc/login/callback"
	oidcTestIssuer       = "https://newapi.example.com"
)

type oidcTestEnv struct {
	engine *gin.Engine
}

func setupOIDCProviderTest(t *testing.T) *oidcTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	originalAddr := system_setting.ServerAddress
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)
	system_setting.ServerAddress = oidcTestIssuer
	oidc.ResetKeyCacheForTest()
	oidc.ResetStoresForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Organization{}, &model.Option{}))
	model.DB = db
	require.NoError(t, db.Create(&model.Organization{Id: 3, Name: "Acme", OwnerUserId: 7}).Error)
	// users.aff_code has a unique index, so every fixture user needs its own.
	for _, u := range []model.User{
		{Id: 7, Username: "alice", Password: "secret-pass", DisplayName: "Alice", Email: "Alice@Example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", OrganizationId: 3},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, Group: "vip"},
		{Id: 9, Username: "carol", Password: "secret-pass", Email: "carol@example.com", AffCode: "aff-carol", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, Group: "default"},
	} {
		require.NoError(t, db.Create(&u).Error)
	}

	originalSettings := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{
		Enabled: true, ClientId: oidcTestClientId, ClientSecret: oidcTestClientSecret,
		RedirectURI: oidcTestRedirect, AppUrl: "https://chat.example.com",
		PostLogoutRedirectURI: "https://chat.example.com/bye",
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		system_setting.ServerAddress = originalAddr
		*system_setting.GetOAuth2Settings() = originalSettings
		oidc.ResetKeyCacheForTest()
		oidc.ResetStoresForTest()
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-secret"))))
	// test-only login: puts a user into the session exactly like setupLogin does
	engine.POST("/test/login/:id", func(c *gin.Context) {
		var user model.User
		require.NoError(t, model.DB.First(&user, "id = ?", c.Param("id")).Error)
		s := sessions.Default(c)
		s.Set("id", user.Id)
		s.Set("username", user.Username)
		s.Set("role", user.Role)
		s.Set("status", user.Status)
		s.Set("group", user.Group)
		require.NoError(t, s.Save())
		c.Status(http.StatusOK)
	})
	engine.GET("/.well-known/openid-configuration", OAuth2Discovery)
	engine.GET("/oauth2/jwks", OAuth2JWKS)
	engine.POST("/oauth2/token", OAuth2Token)
	engine.GET("/oauth2/userinfo", OAuth2UserInfo)
	authorize := engine.Group("/api/oauth2")
	authorize.Use(middleware.UserAuth())
	authorize.POST("/authorize", OAuth2Authorize)
	return &oidcTestEnv{engine: engine}
}

// login returns the session cookie for userId.
func (e *oidcTestEnv) login(t *testing.T, userId string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/test/login/"+userId, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "session" {
			return ck
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func (e *oidcTestEnv) authorize(t *testing.T, cookie *http.Cookie, userId string, params map[string]string) (int, map[string]any) {
	t.Helper()
	body, err := common.Marshal(params)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/authorize", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
		req.Header.Set("New-Api-User", userId)
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
	rec = httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOIDC_Authorize_RequiresLogin(t *testing.T) {
	env := setupOIDCProviderTest(t)
	code, _ := env.authorize(t, nil, "", baseAuthorizeParams(""))
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestOIDC_FullCodeFlow_WithPKCEAndBasicAuth(t *testing.T) {
	env := setupOIDCProviderTest(t)
	ck := env.login(t, "7")
	verifier := "a-very-long-code-verifier-string-0123456789"

	status, res := env.authorize(t, ck, "7", baseAuthorizeParams(pkceChallenge(verifier)))
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
	ck := env.login(t, "7")
	verifier := "another-very-long-code-verifier-string-0123456789"

	mint := func() string {
		_, res := env.authorize(t, ck, "7", baseAuthorizeParams(pkceChallenge(verifier)))
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
	ck := env.login(t, "7")

	p := baseAuthorizeParams("")
	p["redirect_uri"] = "https://evil.example.com/cb"
	_, res := env.authorize(t, ck, "7", p)
	require.Equal(t, false, res["success"])
	require.Equal(t, "invalid_redirect_uri", res["error"])
	_, hasData := res["data"]
	require.False(t, hasData, "must not hand the browser to an unregistered redirect_uri")
	require.NotEmpty(t, res["message"])

	p = baseAuthorizeParams("")
	p["client_id"] = "other"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_client", res["error"])

	p = baseAuthorizeParams("")
	p["scope"] = "email profile"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_scope", res["error"])
	require.Equal(t, "invalid_scope", redirectURL(t, res).Query().Get("error"))
	require.Equal(t, "st-1", redirectURL(t, res).Query().Get("state"))

	p = baseAuthorizeParams("")
	p["response_type"] = "token"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "unsupported_response_type", redirectURL(t, res).Query().Get("error"))

	p = baseAuthorizeParams("x")
	p["code_challenge_method"] = "plain"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_request", redirectURL(t, res).Query().Get("error"))

	// no email → non-redirectable email_required
	ck8 := env.login(t, "8")
	_, res = env.authorize(t, ck8, "8", baseAuthorizeParams(""))
	require.Equal(t, "email_required", res["error"])
	_, hasData = res["data"]
	require.False(t, hasData)
	require.Equal(t, i18n.Translate(i18n.LangKo, i18n.MsgOIDCEmailRequired), res["message"])

	// a session that already records the user as disabled is stopped by
	// UserAuth itself, before the handler: no redirect is handed out
	ck9 := env.login(t, "9")
	_, res = env.authorize(t, ck9, "9", baseAuthorizeParams(""))
	require.Equal(t, false, res["success"])
	_, hasData = res["data"]
	require.False(t, hasData)

	// user disabled after logging in (the session still says enabled) → the
	// handler's own status check answers with an access_denied redirect
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	_, res = env.authorize(t, ck, "7", baseAuthorizeParams(""))
	require.Equal(t, "access_denied", res["error"])
	require.Equal(t, "access_denied", redirectURL(t, res).Query().Get("error"))
	require.Equal(t, "st-1", redirectURL(t, res).Query().Get("state"))
}

func TestOIDC_UserInfo_Failures(t *testing.T) {
	env := setupOIDCProviderTest(t)
	status, res, hdr := env.userinfo(t, "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "invalid_token", res["error"])
	require.Contains(t, hdr.Get("WWW-Authenticate"), "Bearer")

	status, _, _ = env.userinfo(t, "unknown-token")
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
	ck := env.login(t, "7")
	_, res := env.authorize(t, ck, "7", baseAuthorizeParams(""))
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
