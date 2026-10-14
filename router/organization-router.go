package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// fork: O3 — organization routes. Platform administration (/api/organizations)
// is casbin-guarded; member/organization-admin routes (/api/organization) keep
// the fork's organization-role checks inside the handlers, and writes from a
// disabled organization are refused by RejectDisabledOrganizationMutation.
var organizationPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/", permission: authz.OrganizationRead, handler: controller.ListOrganizations},
	{method: http.MethodPost, path: "/", permission: authz.OrganizationWrite, handler: controller.CreateOrganization},
	{method: http.MethodPatch, path: "/:id", permission: authz.OrganizationWrite, handler: controller.UpdateOrganization},
	{method: http.MethodDelete, path: "/:id", permission: authz.OrganizationWrite, handler: controller.DeleteOrganization},
}

func registerForkOrganizationRoutes(apiRouter *gin.RouterGroup) {
	organizationsRoute := apiRouter.Group("/organizations")
	organizationsRoute.Use(middleware.RootAuth())
	for _, route := range organizationPermissionRoutes {
		handlePermissionRoute(organizationsRoute, route.method, route.path, route.permission, route.handler)
	}

	organizationRoute := apiRouter.Group("/organization")
	organizationRoute.Use(middleware.UserAuth())
	organizationRoute.Use(controller.RejectDisabledOrganizationMutation())
	{
		organizationRoute.GET("/", controller.GetOrganizationProfile)
		organizationRoute.GET("/dashboard", controller.GetOrganizationDashboard)
		organizationRoute.GET("/logs", controller.GetOrganizationLogs)
		organizationRoute.GET("/mj", controller.GetOrganizationMidjourney)
		organizationRoute.GET("/task", controller.GetOrganizationTask)
		organizationRoute.GET("/users", controller.ListOrganizationUsers)
		organizationRoute.GET("/users/export", controller.ExportOrganizationUsers)
		organizationRoute.POST("/users/import", controller.ImportOrganizationUsers)
		organizationRoute.GET("/assignable-users", controller.ListAssignableOrganizationUsers)
		organizationRoute.GET("/users/:id", controller.GetOrganizationUser)
		organizationRoute.PATCH("/users/:id", controller.UpdateOrganizationUser)
		organizationRoute.PUT("/users/:id/membership", controller.AssignOrganizationUser)
		organizationRoute.DELETE("/users/:id/membership", controller.RemoveOrganizationUserMembership)
		organizationRoute.GET("/subscription/self", controller.GetMyOrganizationSubscription)
		organizationRoute.GET("/subscription/plans", controller.ListOrganizationSubscriptionPlans)
		organizationRoute.POST("/subscription/plans", controller.CreateOrganizationSubscriptionPlan)
		organizationRoute.PATCH("/subscription/plans/:id", controller.UpdateOrganizationSubscriptionPlan)
		organizationRoute.DELETE("/subscription/plans/:id", controller.DeleteOrganizationSubscriptionPlan)
		organizationRoute.GET("/subscription/users", controller.ListOrganizationUserSubscriptions)
		organizationRoute.PUT("/subscription/users/:id", controller.AssignOrganizationUserSubscription)
		organizationRoute.DELETE("/subscription/users/:id", controller.CancelOrganizationUserSubscription)
		organizationRoute.GET("/wallet", controller.GetOrganizationWallet)
		organizationRoute.GET("/topup/self", controller.GetOrganizationTopUps)
		organizationRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationEpay)
		organizationRoute.POST("/amount", controller.RequestOrganizationAmount)
		organizationRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationStripePay)
		organizationRoute.POST("/stripe/amount", controller.RequestOrganizationStripeAmount)
		organizationRoute.POST("/paypal/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationPayPalPay)
		organizationRoute.POST("/paypal/amount", controller.RequestOrganizationPayPalAmount)
		organizationRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationCreemPay)
		organizationRoute.POST("/waffo/amount", controller.RequestOrganizationWaffoAmount)
		organizationRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationWaffoPay)
		organizationRoute.POST("/waffo-pancake/amount", controller.RequestOrganizationWaffoPancakeAmount)
		organizationRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationWaffoPancakePay)
	}
}
