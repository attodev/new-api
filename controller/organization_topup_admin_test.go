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

func seedOrganizationTopUp(t *testing.T, payerId int, targetType string, targetId int, status string, tradeNo string, createTime int64) model.TopUp {
	t.Helper()
	topUp := model.TopUp{UserId: payerId, Amount: 10, Money: 10, TradeNo: tradeNo, PaymentMethod: "stripe",
		CreateTime: createTime, Status: status, TargetType: targetType, TargetId: targetId}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

type expireTopUpsPayload struct {
	Data struct {
		Expired       int64 `json:"expired"`
		SkippedRecent int64 `json:"skipped_recent"`
	} `json:"data"`
}

func expireOrganizationTopUpsAsRoot(t *testing.T, orgId int) expireTopUpsPayload {
	t.Helper()
	root := model.User{Id: 9999, Role: common.RoleRootUser}
	res := orgSecurityRequest(ExpireOrganizationTopUps, root, http.MethodPost, "/", "", orgSecurityIdParam(orgId))
	requireOrgSecurityResult(t, res, true, "")
	var payload expireTopUpsPayload
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	return payload
}

func requireTopUpStatuses(t *testing.T, want map[int]string) {
	t.Helper()
	for id, status := range want {
		var reloaded model.TopUp
		require.NoError(t, model.DB.First(&reloaded, id).Error)
		require.Equal(t, status, reloaded.Status, fmt.Sprintf("top-up %d", id))
	}
}

// An enabled organization keeps orders younger than the safety age: the
// owner may still be in the provider checkout.
func TestOrganizationRootExpiresOnlyAbandonedTopUpsOfEnabledOrganization(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id IN ?", []int{f.admin.Id, f.member.Id}).
		Updates(map[string]any{"organization_id": 0, "organization_role": ""}).Error)
	old := common.GetTimestamp() - 2*3600
	now := common.GetTimestamp()
	pendingA := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-a", old)
	pendingB := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-b", old)
	recent := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-recent", now)
	settled := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusSuccess, "org-success", old)
	otherOrg := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id+1, common.TopUpStatusPending, "other-org-pending", old)
	personal := seedOrganizationTopUp(t, f.admin.Id, model.TopUpTargetTypeUser, 0, common.TopUpStatusPending, "personal-pending", old)

	payload := expireOrganizationTopUpsAsRoot(t, f.org.Id)
	require.EqualValues(t, 2, payload.Data.Expired)
	require.EqualValues(t, 1, payload.Data.SkippedRecent)
	requireTopUpStatuses(t, map[int]string{
		pendingA.Id: common.TopUpStatusExpired, pendingB.Id: common.TopUpStatusExpired, recent.Id: common.TopUpStatusPending,
		settled.Id: common.TopUpStatusSuccess, otherOrg.Id: common.TopUpStatusPending, personal.Id: common.TopUpStatusPending,
	})

	var audits []model.AuditLog
	require.NoError(t, f.db.Where("action = ?", "organization.topup.expire").Find(&audits).Error)
	require.Len(t, audits, 1)
	requireOrgSecurityAuditParam(t, audits[0], "expired", "2")
	requireOrgSecurityAuditParam(t, audits[0], "skipped_recent", "1")

	// The recent order still blocks the deletion.
	root := model.User{Id: 9999, Role: common.RoleRootUser}
	res := orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, false, "organization has 1 pending top-up orders, wait for them to settle or expire first")
}

// A disabled organization cannot be topped up any more, so every pending
// order is expired regardless of age and the organization can be deleted.
func TestOrganizationRootExpiresAllPendingTopUpsOfDisabledOrganizationThenDeletes(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id IN ?", []int{f.admin.Id, f.member.Id}).
		Updates(map[string]any{"organization_id": 0, "organization_role": ""}).Error)
	require.NoError(t, f.db.Model(&model.Organization{}).Where("id = ?", f.org.Id).Update("status", model.OrganizationStatusDisabled).Error)
	old := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-old", common.GetTimestamp()-2*3600)
	recent := seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending-recent", common.GetTimestamp())
	root := model.User{Id: 9999, Role: common.RoleRootUser}

	res := orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, false, "")

	payload := expireOrganizationTopUpsAsRoot(t, f.org.Id)
	require.EqualValues(t, 2, payload.Data.Expired)
	require.Zero(t, payload.Data.SkippedRecent)
	requireTopUpStatuses(t, map[int]string{old.Id: common.TopUpStatusExpired, recent.Id: common.TopUpStatusExpired})

	res = orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, true, "")
}

func TestOrganizationExpireTopUpsRequiresRootAndAnExistingOrganization(t *testing.T) {
	f := setupOrgSecurityTest(t)
	seedOrganizationTopUp(t, f.owner.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-pending", common.GetTimestamp()-2*3600)

	res := orgSecurityRequest(ExpireOrganizationTopUps, f.owner, http.MethodPost, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, false, "root permission required")

	root := model.User{Id: 9999, Role: common.RoleRootUser}
	res = orgSecurityRequest(ExpireOrganizationTopUps, root, http.MethodPost, "/", "", orgSecurityIdParam(f.org.Id+100))
	require.Equal(t, http.StatusNotFound, res.Code)

	var pending int64
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("status = ?", common.TopUpStatusPending).Count(&pending).Error)
	require.EqualValues(t, 1, pending)
}
