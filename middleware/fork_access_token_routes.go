package middleware

// fork: access-token rules for routes added by the fork (router/fork-router.go).
// Registered in init() so upstream's accessTokenRouteRules literal stays untouched;
// package-level vars are initialised before init runs. Tasks that add fork routes
// append to this map.
var forkAccessTokenRouteRules = map[string]accessTokenRouteRule{
	"POST /api/admin/test-email": accessTokenScopeRule("option:write"), // O5 admin SMTP test mail
	"GET /api/user/export":       accessTokenScopeRule("user:read"),    // O4 user Excel export
	"POST /api/user/import":      accessTokenScopeRule("user:write"),   // O4 user Excel import
}

func init() {
	for key, rule := range forkAccessTokenRouteRules {
		if _, exists := accessTokenRouteRules[key]; exists {
			panic("fork access token rule duplicates upstream route: " + key)
		}
		accessTokenRouteRules[key] = rule
	}
}
