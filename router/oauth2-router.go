package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetOAuth2Router(router *gin.Engine) {
	oauth2Router := router.Group("/oauth2")
	{
		sessionInitRouter := oauth2Router.Group("")
		sessionInitRouter.Use(middleware.UserAuth())
		sessionInitRouter.POST("/session-init", controller.OAuth2SessionInit)

		oauth2Router.POST("/token", controller.OAuth2Token)
		oauth2Router.GET("/userinfo", controller.OAuth2UserInfo)
	}
}
