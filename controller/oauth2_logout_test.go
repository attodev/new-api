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

func (e *oidcTestEnv) postLogout(t *testing.T, ck *http.Cookie, body map[string]string) (map[string]any, []*http.Cookie) {
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
	res, cookies := env.postLogout(t, ck, map[string]string{
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
	res, cookies := env.postLogout(t, ck, map[string]string{
		"id_token_hint": mintIDToken(t, 7, time.Now()), "post_logout_redirect_uri": "https://evil.example.com/",
	})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))

	// No hint at all: still logs out the current session.
	ck = env.login(t, "7")
	res, cookies = env.postLogout(t, ck, map[string]string{})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

func TestOIDC_Logout_BadHintLeavesSessionAlone(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]string{"id_token_hint": "not.a.token", "post_logout_redirect_uri": "https://chat.example.com/bye"})
	require.Equal(t, "/", redirectOf(res))
	all := append([]*http.Cookie{ck}, cookies...)
	require.Equal(t, float64(7), env.whoami(t, all), "forged hint must not end the session")
}

func TestOIDC_Logout_HintSignedByPreviousKey(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	hint := mintIDToken(t, 7, time.Now())
	_, err := oidc.RotateKey()
	require.NoError(t, err)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]string{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye"})
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
