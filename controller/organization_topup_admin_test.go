package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// fork: O3 — root expires an organization's abandoned (pending) top-up orders
// so the organization can be deleted; settled orders are untouched.

func seedOrganizationTopUp(t *testing.T, payerId int, targetType string, targetId int, status string, tradeNo string) model.TopUp {
	t.Helper()
	topUp := model.TopUp{UserId: payerId, Amount: 10, Money: 10, TradeNo: tradeNo, PaymentMethod: "stripe",
		CreateTime: common.GetTimestamp(), Status: status, TargetType: targetType, TargetId: targetId}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

func TestOrganizationRootExpiresPendingTopUpsThenDeletes(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id IN ?", []int{f.admin.Id, f.member.Id}).
		Updates(map[string]any{"organization_id": 0, "organization_role": ""}).Error)
	pendingA := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-a")
	pendingB := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-b")
	settled := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusSuccess, "org-success")
	otherOrg := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id+1, common.TopUpStatusPending, "other-org-pending")
	personal := seedOrganizationTopUp(t, f.admin.Id, model.TopUpTargetTypeUser, 0, common.TopUpStatusPending, "personal-pending")
	root := model.User{Id: 9999, Role: common.RoleRootUser}

	// Pending orders block the deletion until they are expired.
	res := orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, false, "")

	res = orgSecurityRequest(ExpireOrganizationTopUps, root, http.MethodPost, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, true, "")
	var payload struct {
		Data struct {
			Expired int64 `json:"expired"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.EqualValues(t, 2, payload.Data.Expired)

	for id, want := range map[int]string{
		pendingA.Id: common.TopUpStatusExpired, pendingB.Id: common.TopUpStatusExpired,
		settled.Id: common.TopUpStatusSuccess, otherOrg.Id: common.TopUpStatusPending, personal.Id: common.TopUpStatusPending,
	} {
		var reloaded model.TopUp
		require.NoError(t, f.db.First(&reloaded, id).Error)
		require.Equal(t, want, reloaded.Status, fmt.Sprintf("top-up %d", id))
	}

	var audits []model.AuditLog
	require.NoError(t, f.db.Where("action = ?", "organization.topup.expire").Find(&audits).Error)
	require.Len(t, audits, 1)
	requireOrgSecurityAuditParam(t, audits[0], "expired", "2")

	res = orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, true, "")
}

func TestOrganizationExpireTopUpsRequiresRootAndAnExistingOrganization(t *testing.T) {
	f := setupOrgSecurityTest(t)
	seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending")

	res := orgSecurityRequest(ExpireOrganizationTopUps, f.owner, http.MethodPost, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, false, "root permission required")

	root := model.User{Id: 9999, Role: common.RoleRootUser}
	res = orgSecurityRequest(ExpireOrganizationTopUps, root, http.MethodPost, "/", "", orgSecurityIdParam(f.org.Id+100))
	require.Equal(t, http.StatusNotFound, res.Code)

	var pending int64
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("status = ?", common.TopUpStatusPending).Count(&pending).Error)
	require.EqualValues(t, 1, pending)
}
