// fork: O13 — ported from OpenWebUI router/oauth2-router_test.go, plus the /api/openwebui and /api/oauth2 registration and SPA fallback checks.

package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOAuth2Router_RegistersExpectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetOAuth2Router(engine)

	paths := make(map[string]string)
	for _, r := range engine.Routes() {
		paths[r.Method+" "+r.Path] = r.Handler
	}

	require.Contains(t, paths, "GET /.well-known/openid-configuration")
	require.Contains(t, paths, "GET /oauth2/jwks")
	require.Contains(t, paths, "POST /oauth2/token")
	require.Contains(t, paths, "GET /oauth2/userinfo")
	require.Contains(t, paths, "POST /oauth2/introspect")
	require.Contains(t, paths, "POST /oauth2/token-from-key")
	require.NotContains(t, paths, "POST /oauth2/session-init")
	require.Len(t, paths, 6)
}

// /oauth2/authorize and /oauth2/logout are SPA pages: the backend routes
// under /oauth2 must leave them (and their query strings) to NoRoute, past
// the landing pages.
func TestSetOAuth2Router_LeavesSPAPagesToNoRoute(t *testing.T) {
	engine := newLandingTestEngine(t)
	SetOAuth2Router(engine)

	for _, path := range []string{
		"/oauth2/authorize?response_type=code&client_id=openwebui&state=123",
		"/oauth2/authorize",
		"/oauth2/logout?id_token_hint=a.b.c&post_logout_redirect_uri=https%3A%2F%2Fchat.example.com%2F&state=1",
		"/oauth2/logout",
	} {
		rec := get(t, engine, path, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, path)
		require.Equal(t, "spa", rec.Body.String(), path)
	}
	// GET on the POST-only token endpoint is not a route either.
	require.Equal(t, "spa", get(t, engine, "/oauth2/token", nil).Body.String())
}

func TestOpenWebUIRoutesAreSessionOnly(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	registered := map[string]bool{}
	for _, r := range engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, key := range []string{
		"GET /api/openwebui/relay-status",
		"GET /api/openwebui/links",
		"POST /api/openwebui/links",
		"DELETE /api/openwebui/links/:id",
		"POST /api/oauth2/authorize",
		"POST /api/oauth2/logout",
		"POST /api/oauth2/rotate-signing-key",
		"GET /api/oauth2/signing-key-status",
	} {
		require.True(t, registered[key], key)
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		require.Equal(t, "session", rule.Kind(), key)
	}
}

// The admin and root routes require a dashboard login; the end-session API
// carries the same origin guard as /api/user/auth/logout because it may
// delete the refresh cookie.
func TestOpenWebUIRoutesGuards(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/openwebui/relay-status"},
		{http.MethodGet, "/api/openwebui/links?user_id=1"},
		{http.MethodPost, "/api/openwebui/links"},
		{http.MethodDelete, "/api/openwebui/links/1"},
		{http.MethodPost, "/api/oauth2/authorize"},
		{http.MethodPost, "/api/oauth2/rotate-signing-key"},
		{http.MethodGet, "/api/oauth2/signing-key-status"},
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}")))
		require.Equal(t, http.StatusUnauthorized, rec.Code, tc.path)
	}

	originalSecure := common.SessionCookieSecure
	common.SessionCookieSecure = true
	t.Cleanup(func() { common.SessionCookieSecure = originalSecure })
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/logout", strings.NewReader(`{"confirmed":true}`))
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "AUTH_ORIGIN_FORBIDDEN")
	require.Empty(t, rec.Result().Cookies())
}
