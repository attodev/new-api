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

type expirePersonalOrdersPayload struct {
	Data model.PersonalOrderExpiry `json:"data"`
}

func expirePersonalTopUpsAsAdmin(t *testing.T, body string) model.PersonalOrderExpiry {
	t.Helper()
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
	requireOrgSecurityResult(t, res, true, "")
	var payload expirePersonalOrdersPayload
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	return payload.Data
}

func seedSubscriptionOrder(t *testing.T, userId int, tradeNo string, createTime int64) model.SubscriptionOrder {
	t.Helper()
	order := model.SubscriptionOrder{UserId: userId, PlanId: 1, Money: 10, TradeNo: tradeNo, PaymentMethod: "epay",
		Status: common.TopUpStatusPending, CreateTime: createTime}
	require.NoError(t, model.DB.Create(&order).Error)
	return order
}

func requireSubscriptionOrderStatuses(t *testing.T, want map[int]string) {
	t.Helper()
	for id, status := range want {
		var reloaded model.SubscriptionOrder
		require.NoError(t, model.DB.First(&reloaded, id).Error)
		require.Equal(t, status, reloaded.Status, fmt.Sprintf("subscription order %d", id))
		if status == common.TopUpStatusExpired {
			require.Positive(t, reloaded.CompleteTime, "expired like ExpireSubscriptionOrder")
		}
	}
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
	subOld := seedSubscriptionOrder(t, payer.Id, "sub-old", twoHoursAgo)
	subRecent := seedSubscriptionOrder(t, payer.Id, "sub-recent", tenMinutesAgo)
	otherSubOld := seedSubscriptionOrder(t, other.Id, "other-sub-old", twoHoursAgo)

	payload := expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.Equal(t, model.PersonalOrderExpiry{ExpiredTopUps: 1, SkippedRecentTopUps: 1, ExpiredSubscriptions: 1, SkippedRecentSubscriptions: 1}, payload)
	requireSubscriptionOrderStatuses(t, map[int]string{
		subOld.Id: common.TopUpStatusExpired, subRecent.Id: common.TopUpStatusPending, otherSubOld.Id: common.TopUpStatusPending,
	})
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
	requireOrgSecurityAuditParam(t, audits[0], "expired_subscriptions", "1")
	requireOrgSecurityAuditParam(t, audits[0], "skipped_recent_subscriptions", "1")

	// The ten-minute-old orders may still be in checkout: they keep refusing the join.
	res := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, false, orgAssignPersonalBalanceMessage)

	// Once the recent top-up is abandoned too, the recent subscription order
	// alone still refuses the join.
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("id = ?", recent.Id).Update("create_time", twoHoursAgo).Error)
	payload = expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.Equal(t, model.PersonalOrderExpiry{ExpiredTopUps: 1, SkippedRecentSubscriptions: 1}, payload)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, false, orgAssignPersonalBalanceMessage)

	// Once it is abandoned too, a third expiry clears the way.
	require.NoError(t, f.db.Model(&model.SubscriptionOrder{}).Where("id = ?", subRecent.Id).Update("create_time", twoHoursAgo).Error)
	payload = expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.Equal(t, model.PersonalOrderExpiry{ExpiredSubscriptions: 1}, payload)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, true, "")
	orgId, err := model.GetUserOrganizationId(payer.Id)
	require.NoError(t, err)
	require.Equal(t, f.org.Id, orgId)
	requireTopUpStatuses(t, map[int]string{orgOrder.Id: common.TopUpStatusPending, otherOld.Id: common.TopUpStatusPending})

	// Without user_id every user's abandoned personal order is expired; the
	// organization order is still untouched.
	payload = expirePersonalTopUpsAsAdmin(t, "")
	require.Equal(t, model.PersonalOrderExpiry{ExpiredTopUps: 1, ExpiredSubscriptions: 1}, payload)
	requireTopUpStatuses(t, map[int]string{orgOrder.Id: common.TopUpStatusPending, otherOld.Id: common.TopUpStatusExpired})
	requireSubscriptionOrderStatuses(t, map[int]string{otherSubOld.Id: common.TopUpStatusExpired})
}

func TestAdminExpirePendingTopUpsRejectsInvalidUserId(t *testing.T) {
	setupOrgSecurityTest(t)
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	for _, body := range []string{`{"user_id": -1}`, `{"user_id": "x"}`, `not json`} {
		res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
		requireOrgSecurityResult(t, res, false, "Invalid parameters")
	}
}
