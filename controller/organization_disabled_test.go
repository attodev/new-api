package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — RejectDisabledOrganizationMutation is the single choke point for
// writes on /api/organization/*: members and admins of a disabled organization
// cannot mutate anything (members, plans, imports, top-ups); reads still work
// and root is not affected.

func performGuardedOrganizationRequest(actor model.User, method string, path string, body string) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", actor.Id)
		c.Set("role", actor.Role)
		c.Next()
	})
	engine.Use(RejectDisabledOrganizationMutation())
	engine.GET("/api/organization/users", ListOrganizationUsers)
	engine.PATCH("/api/organization/users/:id", UpdateOrganizationUser)
	engine.POST("/api/organization/subscription/plans", CreateOrganizationSubscriptionPlan)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestDisabledOrganizationRefusesAdminMutations(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	owner := model.User{Id: 1, Username: "owner", Password: "x", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-disabled"}
	member := model.User{Id: 2, Username: "member", Password: "x", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-disabled"}
	root := model.User{Id: 3, Username: "root", Password: "x", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root-disabled"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusDisabled}
	for _, row := range []any{&owner, &member, &root, &org} {
		require.NoError(t, model.DB.Create(row).Error)
	}

	res := performGuardedOrganizationRequest(owner, http.MethodPatch, "/api/organization/users/2", `{"quota":5000}`)
	requireOrganizationApiError(t, res, "organization is disabled")
	res = performGuardedOrganizationRequest(owner, http.MethodPost, "/api/organization/subscription/plans", `{"title":"Plan","total_amount":10}`)
	requireOrganizationApiError(t, res, "organization is disabled")
	requireUserQuota(t, member.Id, 100)
	var plans int64
	require.NoError(t, model.DB.Model(&model.OrganizationSubscriptionPlan{}).Count(&plans).Error)
	require.Zero(t, plans)

	// Reads still work for the organization admin.
	res = performGuardedOrganizationRequest(owner, http.MethodGet, "/api/organization/users", "")
	require.Contains(t, res.Body.String(), `"success":true`)

	// Root still manages the disabled organization.
	res = performGuardedOrganizationRequest(root, http.MethodPost, "/api/organization/subscription/plans?organization_id=1", `{"title":"Plan","total_amount":10}`)
	require.Contains(t, res.Body.String(), `"success":true`)

	// Once re-enabled, the admin can mutate again.
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("status", model.OrganizationStatusEnabled).Error)
	res = performGuardedOrganizationRequest(owner, http.MethodPatch, "/api/organization/users/2", `{"quota":5000}`)
	require.Contains(t, res.Body.String(), `"success":true`)
	requireUserQuota(t, member.Id, 5000)
}
