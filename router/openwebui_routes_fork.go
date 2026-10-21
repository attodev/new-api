// fork: O13 — Open WebUI admin API (/api/openwebui) and the same-origin OIDC provider API (/api/oauth2) behind the SPA pages /oauth2/authorize and /oauth2/logout. Called from one line in router/api-router.go; access-token rules are in middleware/fork_access_token_routes.go.

package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerForkOpenWebUIRoutes(apiRouter *gin.RouterGroup) {
	openWebUIRoute := apiRouter.Group("/openwebui")
	openWebUIRoute.Use(middleware.AdminAuth())
	{
		openWebUIRoute.GET("/relay-status", controller.GetOpenWebUIRelayStatus)
		openWebUIRoute.GET("/links", controller.GetOpenWebUILink)
		openWebUIRoute.POST("/links", controller.CreateOpenWebUILink)
		openWebUIRoute.DELETE("/links/:id", controller.DeleteOpenWebUILink)
	}

	oidcRoute := apiRouter.Group("/oauth2")
	{
		oidcRoute.POST("/authorize", middleware.UserAuth(), controller.OAuth2Authorize)
		// No auth middleware: the handler reads the Bearer access token itself
		// and may delete the refresh cookie, so it gets the same guard chain as
		// /api/user/auth/logout.
		oidcRoute.POST("/logout", middleware.SessionCookieOriginGuard(), middleware.CriticalRateLimit(), middleware.DisableCache(), controller.OAuth2Logout)
		oidcRoute.POST("/rotate-signing-key", middleware.RootAuth(), controller.OAuth2RotateSigningKey)
		oidcRoute.GET("/signing-key-status", middleware.RootAuth(), controller.OAuth2SigningKeyStatus)
	}
}
