package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// fork: O3 — an admin expires a user's abandoned (pending) personal top-up
// orders, which otherwise refuse the user's organization join forever.

func expirePersonalTopUpsAsAdmin(t *testing.T, body string) expireTopUpsPayload {
	t.Helper()
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
	requireOrgSecurityResult(t, res, true, "")
	var payload expireTopUpsPayload
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	return payload
}

func TestAdminExpiresAbandonedPersonalTopUpsThenUserCanJoinOrganization(t *testing.T) {
	f := setupOrgSecurityTest(t)
	mkUser := func(name string) model.User {
		u := model.User{Username: name, AffCode: "aff-" + name, Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
		require.NoError(t, f.db.Create(&u).Error)
		return u
	}
	payer := mkUser("payer")
	other := mkUser("other")
	twoHoursAgo := common.GetTimestamp() - 2*3600
	tenMinutesAgo := common.GetTimestamp() - 10*60

	old := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusPending, "personal-old", twoHoursAgo)
	recent := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusPending, "personal-recent", tenMinutesAgo)
	// a legacy row with an empty target type is personal too
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("id = ?", recent.Id).Update("target_type", "").Error)
	orgOrder := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-old", twoHoursAgo)
	settled := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusSuccess, "personal-settled", twoHoursAgo)
	otherOld := seedOrganizationTopUp(t, other.Id, model.TopUpTargetTypeUser, other.Id, common.TopUpStatusPending, "other-old", twoHoursAgo)

	payload := expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.EqualValues(t, 1, payload.Data.Expired)
	require.EqualValues(t, 1, payload.Data.SkippedRecent)
	requireTopUpStatuses(t, map[int]string{
		old.Id: common.TopUpStatusExpired, recent.Id: common.TopUpStatusPending, orgOrder.Id: common.TopUpStatusPending,
		settled.Id: common.TopUpStatusSuccess, otherOld.Id: common.TopUpStatusPending,
	})

	var audits []model.AuditLog
	require.NoError(t, f.db.Where("action = ?", "topup.expire").Find(&audits).Error)
	require.Len(t, audits, 1)
	requireOrgSecurityAuditParam(t, audits[0], "user_id", fmt.Sprint(payer.Id))
	requireOrgSecurityAuditParam(t, audits[0], "expired", "1")
	requireOrgSecurityAuditParam(t, audits[0], "skipped_recent", "1")

	// The ten-minute-old order may still be in checkout: it keeps refusing the join.
	res := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, false, orgAssignPersonalBalanceMessage)

	// Once it is abandoned too, a second expiry clears the way.
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("id = ?", recent.Id).Update("create_time", twoHoursAgo).Error)
	payload = expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.EqualValues(t, 1, payload.Data.Expired)
	require.EqualValues(t, 0, payload.Data.SkippedRecent)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, true, "")
	orgId, err := model.GetUserOrganizationId(payer.Id)
	require.NoError(t, err)
	require.Equal(t, f.org.Id, orgId)
	requireTopUpStatuses(t, map[int]string{orgOrder.Id: common.TopUpStatusPending, otherOld.Id: common.TopUpStatusPending})

	// Without user_id every user's abandoned personal order is expired; the
	// organization order is still untouched.
	payload = expirePersonalTopUpsAsAdmin(t, "")
	require.EqualValues(t, 1, payload.Data.Expired)
	require.EqualValues(t, 0, payload.Data.SkippedRecent)
	requireTopUpStatuses(t, map[int]string{orgOrder.Id: common.TopUpStatusPending, otherOld.Id: common.TopUpStatusExpired})
}

func TestAdminExpirePendingTopUpsRejectsInvalidUserId(t *testing.T) {
	setupOrgSecurityTest(t)
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	for _, body := range []string{`{"user_id": -1}`, `{"user_id": "x"}`, `not json`} {
		res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
		requireOrgSecurityResult(t, res, false, "Invalid parameters")
	}
}
