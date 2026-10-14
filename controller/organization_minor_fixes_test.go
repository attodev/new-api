package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — final-review minor fixes (M6, M8, subscription/self shape).

func countOrganizationAuditRows(t *testing.T, action string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.AuditLog{}).Where("action = ?", action).Count(&count).Error)
	return count
}

// M6: an organization with a pending organization top-up cannot be deleted; the
// provider callback would otherwise fail with "organization not found" and
// strand a paid order.
func TestDeleteOrganizationRefusesWhilePendingTopUpExists(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Id: 9, Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root-m6"}
	owner := model.User{Id: 1, Username: "owner", Password: "x", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-m6"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Status: model.OrganizationStatusEnabled}
	pending := model.TopUp{UserId: 1, Amount: 10, Money: 10, TradeNo: "org-pending-1", Status: common.TopUpStatusPending,
		TargetType: model.TopUpTargetTypeOrganization, TargetId: 1}
	for _, row := range []any{&root, &owner, &org, &pending} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	deleteOrg := func() string {
		return performOrganizationRequest(DeleteOrganization, root, http.MethodDelete, "/api/organizations/1", "",
			gin.Param{Key: "id", Value: "1"}).Body.String()
	}

	body := deleteOrg()
	require.Contains(t, body, `"success":false`)
	require.Contains(t, body, "pending top-up")
	var count int64
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(1), count)

	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("id = ?", pending.Id).Update("status", common.TopUpStatusExpired).Error)
	require.Contains(t, deleteOrg(), `"success":true`)
}

// M8: updating or disabling a plan id that does not belong to the caller's
// organization is a 404 and writes no audit row.
func TestOrganizationPlanUpdateAndDisableReturnNotFoundForForeignPlan(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	owner := model.User{Id: 1, Username: "owner", Password: "x", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-m8"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Status: model.OrganizationStatusEnabled}
	foreign := model.OrganizationSubscriptionPlan{OrganizationId: 2, Title: "Foreign", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 10, Enabled: true}
	for _, row := range []any{&owner, &org, &foreign} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	for _, planId := range []int{foreign.Id, 999} {
		id := fmt.Sprintf("%d", planId)
		res := performOrganizationRequest(UpdateOrganizationSubscriptionPlan, owner, http.MethodPatch, "/api/organization/subscription/plans/"+id,
			`{"title":"Hijacked","total_amount":1}`, gin.Param{Key: "id", Value: id})
		require.Equal(t, http.StatusNotFound, res.Code)
		require.Contains(t, res.Body.String(), `"success":false`)
		res = performOrganizationRequest(DeleteOrganizationSubscriptionPlan, owner, http.MethodDelete, "/api/organization/subscription/plans/"+id,
			"", gin.Param{Key: "id", Value: id})
		require.Equal(t, http.StatusNotFound, res.Code)
		require.Contains(t, res.Body.String(), `"success":false`)
	}
	require.Zero(t, countOrganizationAuditRows(t, "organization.plan.update"))
	require.Zero(t, countOrganizationAuditRows(t, "organization.plan.disable"))
	var reloaded model.OrganizationSubscriptionPlan
	require.NoError(t, model.DB.First(&reloaded, foreign.Id).Error)
	require.Equal(t, "Foreign", reloaded.Title)
	require.True(t, reloaded.Enabled)
}

// GET /api/organization/subscription/self returns the caller through the
// restricted organizationUserView, never the full users row.
func TestMyOrganizationSubscriptionDoesNotReturnTheFullUser(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	accessToken := "member-secret-access-token"
	member := model.User{Id: 2, Username: "member", Password: "member-password-hash", Email: "member@example.com", AccessToken: &accessToken,
		Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-self"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Status: model.OrganizationStatusEnabled}
	plan := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Team Basic", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	for _, row := range []any{&member, &org, &plan} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	sub := model.OrganizationUserSubscription{OrganizationId: 1, UserId: member.Id, PlanId: plan.Id, AmountTotal: 500, StartTime: time.Now().Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
	require.NoError(t, model.DB.Create(&sub).Error)

	res := performOrganizationRequest(GetMyOrganizationSubscription, member, http.MethodGet, "/api/organization/subscription/self", "")

	body := res.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"subscription"`)
	require.Contains(t, body, `"username":"member"`)
	require.Contains(t, body, `"Team Basic"`)
	for _, leaked := range []string{`"password"`, `"access_token"`, `"email"`, "member@example.com", "member-secret-access-token"} {
		require.NotContains(t, body, leaked)
	}
}
