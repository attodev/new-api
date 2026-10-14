package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// fork: routes added by the fork. Each function is called from one line in
// router/api-router.go; keep the call sites listed in docs/fork-overlays.md.

// registerForkPublicRoutes: registers routes on the top-level /api group: anonymous endpoints
// (O5 contact, O2 PayPal browser return/webhook) and the admin SMTP test mail, which carries
// AdminAuth inline.
func registerForkPublicRoutes(apiRouter *gin.RouterGroup) {
	bodyLimit := middleware.AnonymousRequestBodyLimit()
	apiRouter.POST("/contact", middleware.CriticalRateLimit(), bodyLimit, controller.Contact)
	apiRouter.GET("/paypal/capture", controller.PayPalCapture)
	apiRouter.POST("/paypal/webhook", bodyLimit, controller.PayPalWebhook)
	apiRouter.POST("/admin/test-email", middleware.AdminAuth(), middleware.CriticalRateLimit(), controller.TestEmail)
}

// registerForkSelfRoutes: user-authenticated endpoints (O2 PayPal top-up).
func registerForkSelfRoutes(selfRoute *gin.RouterGroup) {
	selfRoute.POST("/paypal/pay", middleware.CriticalRateLimit(), controller.RequestPayPalPay)
	selfRoute.POST("/paypal/amount", controller.RequestPayPalAmount)
}

// registerForkAdminUserRoutes: admin endpoints under /api/user (O4 user Excel export/import,
// O3 expiry of abandoned personal top-up orders).
func registerForkAdminUserRoutes(adminRoute *gin.RouterGroup) {
	adminRoute.GET("/export", controller.ExportUsers)
	adminRoute.POST("/import", controller.ImportUsers)
	adminRoute.POST("/topup/expire", controller.AdminExpirePendingTopUps)
}
