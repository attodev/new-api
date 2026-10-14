package router

import (
	"testing"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/stretchr/testify/require"
)

// fork: O3 — the personal top-up expiry sits in the admin /api/user group next
// to /topup/complete and needs the same billing:write access-token scope.
func TestAdminTopUpExpireRouteRegistered(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["POST /api/user/topup/expire"])
	rule, ok := middleware.AccessTokenRouteRule("POST /api/user/topup/expire")
	require.True(t, ok)
	require.Equal(t, "scope", rule.Kind())
	require.Equal(t, "billing:write", rule.Scope())
}
