package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerForkOrganizationRoutes(engine.Group("/api"))
	registered := map[string]bool{}
	for _, r := range engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/organizations/", "POST /api/organizations/", "PATCH /api/organizations/:id", "DELETE /api/organizations/:id",
		"POST /api/organizations/:id/topups/expire",
		"GET /api/organization/", "GET /api/organization/dashboard", "GET /api/organization/logs", "GET /api/organization/mj", "GET /api/organization/task",
		"GET /api/organization/users", "GET /api/organization/users/export", "POST /api/organization/users/import",
		"GET /api/organization/users/lookup", "GET /api/organization/users/:id", "PATCH /api/organization/users/:id",
		"PUT /api/organization/users/:id/membership", "DELETE /api/organization/users/:id/membership",
		"GET /api/organization/subscription/self", "GET /api/organization/subscription/plans", "POST /api/organization/subscription/plans",
		"PATCH /api/organization/subscription/plans/:id", "DELETE /api/organization/subscription/plans/:id",
		"GET /api/organization/subscription/users", "PUT /api/organization/subscription/users/:id", "DELETE /api/organization/subscription/users/:id",
		"GET /api/organization/wallet", "GET /api/organization/topup/self",
		"POST /api/organization/pay", "POST /api/organization/amount",
		"POST /api/organization/stripe/pay", "POST /api/organization/stripe/amount",
		"POST /api/organization/paypal/pay", "POST /api/organization/paypal/amount",
		"POST /api/organization/creem/pay",
		"POST /api/organization/waffo/amount", "POST /api/organization/waffo/pay",
		"POST /api/organization/waffo-pancake/amount", "POST /api/organization/waffo-pancake/pay",
	} {
		require.True(t, registered[want], "missing route %s", want)
	}
	for _, gone := range []string{"POST /api/organization/toss/pay", "GET /api/organization/wallet/auto-recharge"} {
		require.False(t, registered[gone], "excluded route %s is registered", gone)
	}
	require.True(t, registered["GET /api/organization/users/lookup"])
	require.False(t, registered["GET /api/organization/assignable-users"], "the assignable-users listing was replaced by the exact lookup")
	require.Equal(t, 39, len(registered)) // 5 platform (4 + top-up expiry) + 34 member routes (team 46 minus Toss 2 and auto-recharge 6)
	require.Equal(t, authz.Permission{Resource: "organization", Action: "write"}, authz.OrganizationWrite)
	_ = http.MethodGet
}
