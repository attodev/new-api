// fork: O13 — public OIDC provider endpoints (ported from OpenWebUI router/oauth2-router.go). Called from one line in router/main.go.

package router

import (
	"github.com/QuantumNous/new-api/controller"

	"github.com/gin-gonic/gin"
)

// SetOAuth2Router registers the public OIDC provider endpoints. The browser-
// facing /oauth2/authorize and /oauth2/logout are SPA pages (served by the web
// router's NoRoute fallback) that call /api/oauth2/* — see
// openwebui_routes_fork.go — so they must never be registered here. No IP
// rate limit: /oauth2/token, /oauth2/userinfo and /oauth2/introspect are
// called server-to-server from the one Open WebUI host for every user's
// sign-in; /oauth2/token-from-key limits itself per client IP and per API key.
func SetOAuth2Router(router *gin.Engine) {
	router.GET("/.well-known/openid-configuration", controller.OAuth2Discovery)
	oauth2Router := router.Group("/oauth2")
	{
		oauth2Router.GET("/jwks", controller.OAuth2JWKS)
		oauth2Router.POST("/token", controller.OAuth2Token)
		oauth2Router.GET("/userinfo", controller.OAuth2UserInfo)
		oauth2Router.POST("/introspect", controller.OAuth2Introspect)
		oauth2Router.POST("/token-from-key", controller.OAuth2TokenFromKey)
	}
}
