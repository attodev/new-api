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
	apiRouter.POST("/admin/test-email", middleware.AdminAuth(), middleware.CriticalRateLimit(), controller.TestEmail)
}

// registerForkSelfRoutes: user-authenticated endpoints (filled by O2).
func registerForkSelfRoutes(selfRoute *gin.RouterGroup) {}

// registerForkAdminUserRoutes: admin endpoints under /api/user (filled by O4).
func registerForkAdminUserRoutes(adminRoute *gin.RouterGroup) {}
