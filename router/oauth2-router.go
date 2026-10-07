package router

import (
	"github.com/QuantumNous/new-api/controller"

	"github.com/gin-gonic/gin"
)

// SetOAuth2Router registers the public OIDC provider endpoints. The browser-
// facing /oauth2/authorize and /oauth2/logout are SPA pages (served by the web
// router's NoRoute fallback) that call /api/oauth2/* — see api-router.go.
func SetOAuth2Router(router *gin.Engine) {
	router.GET("/.well-known/openid-configuration", controller.OAuth2Discovery)
	oauth2Router := router.Group("/oauth2")
	{
		oauth2Router.GET("/jwks", controller.OAuth2JWKS)
		oauth2Router.POST("/token", controller.OAuth2Token)
		oauth2Router.GET("/userinfo", controller.OAuth2UserInfo)
	}
}
