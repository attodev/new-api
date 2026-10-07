package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// registerLogoutRoutes adds the Task 6 routes plus a probe that reports
// whether the session still carries a user.
func registerLogoutRoutes(env *oidcTestEnv) {
	env.engine.POST("/api/oauth2/logout", OAuth2Logout)
	env.engine.GET("/test/whoami", func(c *gin.Context) {
		id := sessions.Default(c).Get("id")
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	root := env.engine.Group("/api/oauth2")
	root.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleRootUser); c.Next() })
	root.POST("/rotate-signing-key", OAuth2RotateSigningKey)
	root.GET("/signing-key-status", OAuth2SigningKeyStatus)
}

func (e *oidcTestEnv) postLogout(t *testing.T, ck *http.Cookie, body map[string]any) (map[string]any, []*http.Cookie) {
	t.Helper()
	raw, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/logout", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
	return res, rec.Result().Cookies()
}

func (e *oidcTestEnv) whoami(t *testing.T, cookies []*http.Cookie) any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/test/whoami", nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
	return res["id"]
}

func mintIDToken(t *testing.T, userId int, issuedAt time.Time) string {
	t.Helper()
	user, err := model.GetUserById(userId, false)
	require.NoError(t, err)
	raw, err := oidc.SignIDToken(oidc.BuildClaims(user, ""), oidcTestClientId, "n", issuedAt.Unix(), issuedAt)
	require.NoError(t, err)
	return raw
}

func redirectOf(res map[string]any) string {
	return res["data"].(map[string]any)["redirect_url"].(string)
}

func TestOIDC_Logout_ClearsSessionAndHonoursRegisteredRedirect(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	require.Equal(t, float64(7), env.whoami(t, []*http.Cookie{ck}))

	hint := mintIDToken(t, 7, time.Now().Add(-5*time.Hour)) // expired hints are fine
	res, cookies := env.postLogout(t, ck, map[string]any{
		"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye", "state": "s1",
	})
	require.Equal(t, true, res["success"])
	require.Equal(t, "https://chat.example.com/bye?state=s1", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies), "session must be cleared")
}

func TestOIDC_Logout_UnregisteredRedirectFallsBackToSignIn(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]any{
		"id_token_hint": mintIDToken(t, 7, time.Now()), "post_logout_redirect_uri": "https://evil.example.com/",
	})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))

	// No hint, but the user confirmed on the page: logs out the current session.
	ck = env.login(t, "7")
	res, cookies = env.postLogout(t, ck, map[string]any{"confirmed": true})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

// requireNeedsConfirmation asserts the end-session answer that asks the page
// to confirm, and that the session survived it.
func (e *oidcTestEnv) requireNeedsConfirmation(t *testing.T, ck *http.Cookie, res map[string]any, cookies []*http.Cookie) {
	t.Helper()
	require.Equal(t, true, res["success"], res)
	data := res["data"].(map[string]any)
	require.Equal(t, true, data["needs_confirmation"])
	v, present := data["redirect_url"]
	require.True(t, present, "redirect_url must be present (null)")
	require.Nil(t, v)
	for _, c := range cookies {
		require.NotEqual(t, "session", c.Name, "an unconfirmed logout must not touch the session cookie")
	}
	// Response cookies first, original last: a cleared cookie would win.
	all := append(append([]*http.Cookie{}, cookies...), ck)
	require.Equal(t, float64(7), e.whoami(t, all), "an unconfirmed logout must not end the session")
}

func TestOIDC_Logout_NoHintNeedsConfirmation(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)

	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]any{"post_logout_redirect_uri": "https://chat.example.com/bye"})
	env.requireNeedsConfirmation(t, ck, res, cookies)

	res, cookies = env.postLogout(t, ck, map[string]any{"post_logout_redirect_uri": "https://chat.example.com/bye", "state": "s2", "confirmed": true})
	require.Equal(t, "https://chat.example.com/bye?state=s2", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

func TestOIDC_Logout_ForgedHintConfirmedLogsOut(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": "not.a.token", "confirmed": true})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

func TestOIDC_Logout_ProviderDisabledNeverCreatesKey(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	system_setting.GetOAuth2Settings().Enabled = false
	oidc.ResetKeyCacheForTest()

	ck := env.login(t, "7")
	for _, hint := range []string{"", "not.a.token", "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiI3In0.c2ln"} {
		res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye"})
		env.requireNeedsConfirmation(t, ck, res, cookies)
	}
	_, hasKey := common.OptionMap[oidc.OptionSigningKeyCurrent]
	require.False(t, hasKey, "end-session must not create a signing key while the provider is off")
	var n int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where(&model.Option{Key: oidc.OptionSigningKeyCurrent}).Count(&n).Error)
	require.Zero(t, n)

	// Confirmed still logs out, and still creates no key.
	res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": "not.a.token", "confirmed": true})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
	_, hasKey = common.OptionMap[oidc.OptionSigningKeyCurrent]
	require.False(t, hasKey)
}

func TestOIDC_Logout_BadHintLeavesSessionAlone(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": "not.a.token", "post_logout_redirect_uri": "https://chat.example.com/bye"})
	env.requireNeedsConfirmation(t, ck, res, cookies)
}

func TestOIDC_Logout_HintSignedByPreviousKey(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	hint := mintIDToken(t, 7, time.Now())
	_, err := oidc.RotateKey()
	require.NoError(t, err)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye"})
	require.Equal(t, "https://chat.example.com/bye", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
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

	// Response never contains key material.
	require.NotContains(t, rec.Body.String(), "PRIVATE KEY")
	require.NotContains(t, rec.Body.String(), "private_pem")
}

// A valid hint for ANOTHER user must not silently log out whoever opens the
// link (RP-Initiated Logout §2): it needs the logged-in user's confirmation.
func TestOIDC_Logout_HintForAnotherUserNeedsConfirmation(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	hint := mintIDToken(t, 8, time.Now())

	res, cookies := env.postLogout(t, ck, map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye"})
	env.requireNeedsConfirmation(t, ck, res, cookies)

	res, cookies = env.postLogout(t, ck, map[string]any{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye", "confirmed": true})
	require.Equal(t, "https://chat.example.com/bye", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))

	// No session user at all: a valid hint alone does not count either.
	res, cookies = env.postLogout(t, nil, map[string]any{"id_token_hint": mintIDToken(t, 7, time.Now())})
	require.Equal(t, true, res["data"].(map[string]any)["needs_confirmation"])
	require.Nil(t, res["data"].(map[string]any)["redirect_url"])
	for _, c := range cookies {
		require.NotEqual(t, "session", c.Name)
	}
}
