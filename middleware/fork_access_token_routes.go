package middleware

// fork: access-token rules for routes added by the fork (router/fork-router.go).
// Registered in init() so upstream's accessTokenRouteRules literal stays untouched;
// package-level vars are initialised before init runs. Tasks that add fork routes
// append to this map.
var forkAccessTokenRouteRules = map[string]accessTokenRouteRule{
	"POST /api/admin/test-email":   accessTokenScopeRule("option:write"), // O5 admin SMTP test mail
	"POST /api/user/paypal/pay":    accessTokenScopeRule("wallet:write"), // O2 PayPal create order
	"POST /api/user/paypal/amount": accessTokenScopeRule("wallet:read"),  // O2 PayPal amount quote
	"GET /api/user/export":         accessTokenScopeRule("user:read"),    // O4 user Excel export
	"POST /api/user/import":        accessTokenScopeRule("user:write"),   // O4 user Excel import
	// O3 organization member/admin routes (router/organization-router.go); /api/organizations
	// is declared by handlePermissionRoute.
	"GET /api/organization/":                          accessTokenSessionRule,
	"GET /api/organization/dashboard":                 accessTokenSessionRule,
	"GET /api/organization/logs":                      accessTokenSessionRule,
	"GET /api/organization/mj":                        accessTokenSessionRule,
	"GET /api/organization/task":                      accessTokenSessionRule,
	"GET /api/organization/users":                     accessTokenSessionRule,
	"GET /api/organization/users/export":              accessTokenSessionRule,
	"POST /api/organization/users/import":             accessTokenSessionRule,
	"GET /api/organization/assignable-users":          accessTokenSessionRule,
	"GET /api/organization/users/:id":                 accessTokenSessionRule,
	"PATCH /api/organization/users/:id":               accessTokenSessionRule,
	"PUT /api/organization/users/:id/membership":      accessTokenSessionRule,
	"DELETE /api/organization/users/:id/membership":   accessTokenSessionRule,
	"GET /api/organization/subscription/self":         accessTokenScopeRule("profile:read"),
	"GET /api/organization/subscription/plans":        accessTokenSessionRule,
	"POST /api/organization/subscription/plans":       accessTokenSessionRule,
	"PATCH /api/organization/subscription/plans/:id":  accessTokenSessionRule,
	"DELETE /api/organization/subscription/plans/:id": accessTokenSessionRule,
	"GET /api/organization/subscription/users":        accessTokenSessionRule,
	"PUT /api/organization/subscription/users/:id":    accessTokenSessionRule,
	"DELETE /api/organization/subscription/users/:id": accessTokenSessionRule,
	"GET /api/organization/wallet":                    accessTokenScopeRule("wallet:read"),
	"GET /api/organization/topup/self":                accessTokenScopeRule("wallet:read"),
	"POST /api/organization/pay":                      accessTokenScopeRule("wallet:write"),
	"POST /api/organization/amount":                   accessTokenScopeRule("wallet:read"),
	"POST /api/organization/stripe/pay":               accessTokenScopeRule("wallet:write"),
	"POST /api/organization/stripe/amount":            accessTokenScopeRule("wallet:read"),
	"POST /api/organization/paypal/pay":               accessTokenScopeRule("wallet:write"),
	"POST /api/organization/paypal/amount":            accessTokenScopeRule("wallet:read"),
	"POST /api/organization/creem/pay":                accessTokenScopeRule("wallet:write"),
	"POST /api/organization/waffo/amount":             accessTokenScopeRule("wallet:read"),
	"POST /api/organization/waffo/pay":                accessTokenScopeRule("wallet:write"),
	"POST /api/organization/waffo-pancake/amount":     accessTokenScopeRule("wallet:read"),
	"POST /api/organization/waffo-pancake/pay":        accessTokenScopeRule("wallet:write"),
}

func init() {
	for key, rule := range forkAccessTokenRouteRules {
		if _, exists := accessTokenRouteRules[key]; exists {
			panic("fork access token rule duplicates upstream route: " + key)
		}
		accessTokenRouteRules[key] = rule
	}
}
