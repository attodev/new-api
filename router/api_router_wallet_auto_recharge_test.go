package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetApiRouterRegistersWalletAutoRechargePresetRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	routes := engine.Routes()
	requireRoute(t, routes, "GET", "/api/admin/wallet/auto-recharge/presets")
	requireRoute(t, routes, "GET", "/api/user/wallet/auto-recharge/presets")
	requireRoute(t, routes, "GET", "/api/organization/wallet/auto-recharge/presets")
}

func requireRoute(t *testing.T, routes gin.RoutesInfo, method string, path string) {
	t.Helper()

	for _, route := range routes {
		if route.Method == method && route.Path == path {
			return
		}
	}

	require.Failf(t, "route not registered", "%s %s", method, path)
}
