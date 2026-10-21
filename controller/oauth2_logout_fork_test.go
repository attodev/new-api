// fork: O13 — end-session tests rewritten for develop's login sessions (ported from OpenWebUI controller/oauth2_logout_test.go). The "session" is a user_sessions row reached through the SPA's Bearer access token or the refresh cookie.

package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// registerLogoutRoutes adds the end-session API and the root signing-key
// endpoints (behind a stand-in for RootAuth).
func registerLogoutRoutes(env *oidcTestEnv) {
	env.engine.POST("/api/oauth2/logout", OAuth2Logout)
	root := env.engine.Group("/api/oauth2")
	root.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleRootUser); c.Next() })
	root.POST("/rotate-signing-key", OAuth2RotateSigningKey)
	root.GET("/signing-key-status", OAuth2SigningKeyStatus)
}

// logoutRequest is what the SPA page sends: the Bearer access token it holds
// in memory and, where the browser sends it, the refresh cookie.
type logoutRequest struct {
	bearer  string
	refresh string
	body    map[string]any
	raw     string
}

type logoutResult struct {
	res     map[string]any
	cookies []*http.Cookie
}

func (e *oidcTestEnv) postLogout(t *testing.T, r logoutRequest) logoutResult {
	t.Helper()
	raw := r.raw
	if raw == "" && r.body != nil {
		encoded, err := common.Marshal(r.body)
		require.NoError(t, err)
		raw = string(encoded)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/logout", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	if r.refresh != "" {
		req.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: r.refresh})
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return logoutResult{res: res, cookies: rec.Result().Cookies()}
}

func (r logoutResult) redirect(t *testing.T) string {
	t.Helper()
	require.Equal(t, true, r.res["success"], r.res)
	data := r.res["data"].(map[string]any)
	require.NotEqual(t, true, data["needs_confirmation"], r.res)
	return data["redirect_url"].(string)
}

// refreshCleared reports whether the response deletes the refresh cookie.
func (r logoutResult) refreshCleared() bool {
	for _, c := range r.cookies {
		if c.Name == service.RefreshCookieName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

func sessionActive(t *testing.T, sid string) bool {
	t.Helper()
	var s model.UserSession
	require.NoError(t, model.DB.Where("sid = ?", sid).First(&s).Error)
	return s.Status == model.UserSessionStatusActive && s.RevokedAt == 0
}

func requireSessionRevokedByEndSession(t *testing.T, sid string) {
	t.Helper()
	var s model.UserSession
	require.NoError(t, model.DB.Where("sid = ?", sid).First(&s).Error)
	require.Equal(t, model.UserSessionStatusRevoked, s.Status)
	require.NotZero(t, s.RevokedAt)
	require.Equal(t, "oidc_end_session", s.RevokedReason)
}

// requireNeedsConfirmation asserts the answer that asks the page to confirm,
// and that the session and its cookies survived it.
func requireNeedsConfirmation(t *testing.T, r logoutResult, sids ...string) {
	t.Helper()
	require.Equal(t, true, r.res["success"], r.res)
	data := r.res["data"].(map[string]any)
	require.Equal(t, true, data["needs_confirmation"])
	v, present := data["redirect_url"]
	require.True(t, present, "redirect_url must be present (null)")
	require.Nil(t, v)
	require.Empty(t, r.cookies, "an unconfirmed logout must not touch any cookie")
	for _, sid := range sids {
		require.True(t, sessionActive(t, sid), "an unconfirmed logout must not end the session")
	}
}

func mintIDToken(t *testing.T, userId int, issuedAt time.Time) string {
	t.Helper()
	user, err := model.GetUserById(userId, false)
	require.NoError(t, err)
	raw, err := oidc.SignIDToken(oidc.BuildClaims(user, ""), oidcTestClientId, "n", issuedAt.Unix(), issuedAt)
	require.NoError(t, err)
	return raw
}

// (a) A verified hint for the session user ends the session: the row is
// revoked, the refresh cookie deleted, and the registered redirect honoured.
func TestOIDC_Logout_HintForSessionUserRevokesSession(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	login := env.login(t, 7)

	hint := mintIDToken(t, 7, time.Now().Add(-5*time.Hour)) // expired hints are fine
	r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: login.refresh, body: map[string]any{
		"id_token_hint": hint, "post_logout_redirect_uri": oidcTestPostLogout, "state": "s1",
	}})
	require.Equal(t, oidcTestPostLogout+"?state=s1", r.redirect(t))
	requireSessionRevokedByEndSession(t, login.sid)
	require.True(t, r.refreshCleared(), "refresh cookie must be deleted")
}

// Browsers never send the refresh cookie (Path=/api/user/auth) to
// /api/oauth2/logout, so the Bearer access token alone identifies the
// session; the cookie is still deleted so this browser is signed out.
func TestOIDC_Logout_BearerOnlyRevokesSessionAndClearsCookie(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	login := env.login(t, 7)
	other := env.login(t, 7) // another device of the same user

	r := env.postLogout(t, logoutRequest{bearer: login.access, body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())}})
	require.Equal(t, "/sign-in", r.redirect(t))
	requireSessionRevokedByEndSession(t, login.sid)
	require.True(t, r.refreshCleared())
	require.True(t, sessionActive(t, other.sid), "only the current session ends")

	// The revoked session can no longer be refreshed.
	_, _, err := service.RefreshLoginSession(login.refresh, "", "127.0.0.1", "oidc-test")
	require.Error(t, err)
}

// Without a Bearer token the refresh cookie identifies the session; the
// session is revoked through the refresh secret, not the sid alone.
func TestOIDC_Logout_RefreshCookieOnly(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	login := env.login(t, 7)

	r := env.postLogout(t, logoutRequest{refresh: login.refresh, body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())}})
	require.Equal(t, "/sign-in", r.redirect(t))
	requireSessionRevokedByEndSession(t, login.sid)
	require.True(t, r.refreshCleared())

	// A forged cookie (right sid, wrong secret) with a valid hint and the
	// user's confirmation clears the cookie but cannot revoke the session.
	victim := env.login(t, 7)
	forged := victim.sid + ".not-the-secret"
	r = env.postLogout(t, logoutRequest{refresh: forged, body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now()), "confirmed": true}})
	require.Equal(t, "/sign-in", r.redirect(t))
	require.True(t, sessionActive(t, victim.sid))
}

// A refresh cookie of a different session than the Bearer token is left
// alone (the same rule as /api/user/auth/logout).
func TestOIDC_Logout_CookieForOtherSessionIsKept(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	login := env.login(t, 7)
	other := env.login(t, 9)

	r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: other.refresh, body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())}})
	require.Equal(t, "/sign-in", r.redirect(t))
	requireSessionRevokedByEndSession(t, login.sid)
	require.False(t, r.refreshCleared())
	require.True(t, sessionActive(t, other.sid))
}

// (b) No hint, a hint that fails verification, a hint for another user, or
// no session user at all: needs_confirmation and nothing changes.
func TestOIDC_Logout_NeedsConfirmation(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	login := env.login(t, 7)

	for name, body := range map[string]map[string]any{
		"no hint":       {"post_logout_redirect_uri": oidcTestPostLogout},
		"garbage hint":  {"id_token_hint": "not.a.token", "post_logout_redirect_uri": oidcTestPostLogout},
		"forged hint":   {"id_token_hint": mintForgedIDToken(t, 7)},
		"other user":    {"id_token_hint": mintIDToken(t, 8, time.Now()), "post_logout_redirect_uri": oidcTestPostLogout},
		"wrong client":  {"id_token_hint": mintIDTokenFor(t, 7, "other-client")},
		"confirmed=str": {"id_token_hint": "not.a.token", "confirmed": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: login.refresh, body: body})
			requireNeedsConfirmation(t, r, login.sid)
		})
	}

	// No session user at all: a valid hint alone does not count.
	r := env.postLogout(t, logoutRequest{body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())}})
	requireNeedsConfirmation(t, r, login.sid)

	// A Bearer token that does not verify is no session user either.
	r = env.postLogout(t, logoutRequest{bearer: login.access + "x", body: map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())}})
	requireNeedsConfirmation(t, r, login.sid)

	// An empty body is an unconfirmed logout.
	r = env.postLogout(t, logoutRequest{bearer: login.access})
	requireNeedsConfirmation(t, r, login.sid)

	// The body is bounded: a confirmation past 64 KiB is never read.
	huge := `{"id_token_hint":"` + strings.Repeat("a", 70<<10) + `","confirmed":true}`
	r = env.postLogout(t, logoutRequest{bearer: login.access, raw: huge})
	requireNeedsConfirmation(t, r, login.sid)
}

// mintForgedIDToken signs well-formed claims for userId with a key that was
// never published, under the current key's kid.
func mintForgedIDToken(t *testing.T, userId int) string {
	t.Helper()
	current, err := oidc.CurrentKey()
	require.NoError(t, err)
	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, oidc.IDTokenClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: oidcTestIssuer, Subject: strconv.Itoa(userId), Audience: jwt.ClaimStrings{oidcTestClientId},
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}})
	token.Header["kid"] = current.Kid
	raw, err := token.SignedString(forger)
	require.NoError(t, err)
	return raw
}

func mintIDTokenFor(t *testing.T, userId int, clientId string) string {
	t.Helper()
	user, err := model.GetUserById(userId, false)
	require.NoError(t, err)
	now := time.Now()
	raw, err := oidc.SignIDToken(oidc.BuildClaims(user, ""), clientId, "n", now.Unix(), now)
	require.NoError(t, err)
	return raw
}

// (c) The user's confirmation logs out without a valid hint.
func TestOIDC_Logout_ConfirmedLogsOut(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)

	for name, body := range map[string]map[string]any{
		"no hint":     {"confirmed": true},
		"forged hint": {"id_token_hint": "not.a.token", "confirmed": true},
		"other user":  {"id_token_hint": mintIDToken(t, 8, time.Now()), "confirmed": true, "post_logout_redirect_uri": oidcTestPostLogout, "state": "s2"},
	} {
		t.Run(name, func(t *testing.T) {
			login := env.login(t, 7)
			r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: login.refresh, body: body})
			want := "/sign-in"
			if body["state"] != nil {
				want = oidcTestPostLogout + "?state=s2"
			}
			require.Equal(t, want, r.redirect(t))
			requireSessionRevokedByEndSession(t, login.sid)
			require.True(t, r.refreshCleared())
		})
	}

	// Confirmed with no session at all still answers with the redirect, but
	// touches no cookie: a cross-site POST carries no Bearer token and must
	// not be able to delete this browser's refresh cookie.
	r := env.postLogout(t, logoutRequest{body: map[string]any{"confirmed": true}})
	require.Equal(t, "/sign-in", r.redirect(t))
	require.Empty(t, r.cookies)
}

// A hint signed by the previous (rotated-out) key still verifies.
func TestOIDC_Logout_HintSignedByPreviousKey(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	hint := mintIDToken(t, 7, time.Now())
	_, err := oidc.RotateKey()
	require.NoError(t, err)
	login := env.login(t, 7)
	r := env.postLogout(t, logoutRequest{bearer: login.access, body: map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": oidcTestPostLogout}})
	require.Equal(t, oidcTestPostLogout, r.redirect(t))
	requireSessionRevokedByEndSession(t, login.sid)
}

// (d) A provider that is off (or not fully configured) never touches the
// signing keys: hints are not verified and no key is created.
func TestOIDC_Logout_ProviderDisabledNeverCreatesKey(t *testing.T) {
	for name, disable := range map[string]func(*system_setting.OAuth2Settings){
		"disabled":         func(s *system_setting.OAuth2Settings) { s.Enabled = false },
		"no redirect_uri":  func(s *system_setting.OAuth2Settings) { s.RedirectURI = "" },
		"no client secret": func(s *system_setting.OAuth2Settings) { s.ClientSecret = "" },
	} {
		t.Run(name, func(t *testing.T) {
			env := setupOIDCProviderTest(t)
			registerLogoutRoutes(env)
			disable(system_setting.GetOAuth2Settings())
			oidc.ResetKeyCacheForTest()

			login := env.login(t, 7)
			for _, hint := range []string{"", "not.a.token", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiI3In0.c2ln"} {
				r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: login.refresh, body: map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": oidcTestPostLogout}})
				requireNeedsConfirmation(t, r, login.sid)
			}
			requireNoSigningKey(t)

			// Confirmed still logs out, and still creates no key.
			r := env.postLogout(t, logoutRequest{bearer: login.access, refresh: login.refresh, body: map[string]any{"id_token_hint": "not.a.token", "confirmed": true}})
			require.Equal(t, "/sign-in", r.redirect(t))
			requireSessionRevokedByEndSession(t, login.sid)
			requireNoSigningKey(t)
		})
	}
}

func requireNoSigningKey(t *testing.T) {
	t.Helper()
	_, hasKey := common.OptionMap[oidc.OptionSigningKeyCurrent]
	require.False(t, hasKey, "end-session must not create a signing key while the provider is off")
	var n int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where(&model.Option{Key: oidc.OptionSigningKeyCurrent}).Count(&n).Error)
	require.Zero(t, n)
}

// (e) Only the registered post_logout_redirect_uri, matched exactly, is
// honoured; anything else goes to /sign-in.
func TestOIDC_Logout_PostLogoutRedirectExactMatchOnly(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)

	for _, uri := range []string{
		"https://evil.example.com/",
		oidcTestPostLogout + "/",
		oidcTestPostLogout + "?x=1",
		"HTTPS://chat.example.com/bye",
		" " + oidcTestPostLogout,
		"",
	} {
		login := env.login(t, 7)
		r := env.postLogout(t, logoutRequest{bearer: login.access, body: map[string]any{"confirmed": true, "post_logout_redirect_uri": uri, "state": "s"}})
		require.Equal(t, "/sign-in", r.redirect(t), uri)
		requireSessionRevokedByEndSession(t, login.sid)
	}

	// No registered URI: an empty request value does not match the empty setting.
	system_setting.GetOAuth2Settings().PostLogoutRedirectURI = ""
	login := env.login(t, 7)
	r := env.postLogout(t, logoutRequest{bearer: login.access, body: map[string]any{"confirmed": true, "post_logout_redirect_uri": ""}})
	require.Equal(t, "/sign-in", r.redirect(t))

	// Exact match without state carries no query.
	system_setting.GetOAuth2Settings().PostLogoutRedirectURI = oidcTestPostLogout
	login = env.login(t, 7)
	r = env.postLogout(t, logoutRequest{bearer: login.access, body: map[string]any{"confirmed": true, "post_logout_redirect_uri": oidcTestPostLogout}})
	require.Equal(t, oidcTestPostLogout, r.redirect(t))
}

func TestOIDC_SigningKeyStatusAndRotate(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)

	get := func() map[string]any {
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/oauth2/signing-key-status", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var res map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
		return res["data"].(map[string]any)
	}
	before := get()
	cur := before["current"].(map[string]any)
	require.NotEmpty(t, cur["kid"])
	require.NotZero(t, cur["created_at"])
	require.Nil(t, before["previous"])

	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/oauth2/rotate-signing-key", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var rotated map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &rotated))
	after := rotated["data"].(map[string]any)
	require.NotEqual(t, cur["kid"], after["current"].(map[string]any)["kid"])
	require.Equal(t, cur["kid"], after["previous"].(map[string]any)["kid"])
	require.Equal(t, after, get())

	// Response never contains key material.
	require.NotContains(t, rec.Body.String(), "PRIVATE KEY")
	require.NotContains(t, rec.Body.String(), "private_pem")
}
