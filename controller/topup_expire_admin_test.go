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
	// A Stripe checkout session stays payable for 24 hours, so only orders
	// older than a day are abandoned; a two-hour-old one may still be paid.
	dayAgo := common.GetTimestamp() - 25*3600
	twoHoursAgo := common.GetTimestamp() - 2*3600

	old := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusPending, "personal-old", dayAgo)
	recent := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusPending, "personal-recent", twoHoursAgo)
	// a legacy row with an empty target type is personal too
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("id = ?", recent.Id).Update("target_type", "").Error)
	orgOrder := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeOrganization, f.org.Id, common.TopUpStatusPending, "org-old", dayAgo)
	settled := seedOrganizationTopUp(t, payer.Id, model.TopUpTargetTypeUser, payer.Id, common.TopUpStatusSuccess, "personal-settled", dayAgo)
	otherOld := seedOrganizationTopUp(t, other.Id, model.TopUpTargetTypeUser, other.Id, common.TopUpStatusPending, "other-old", dayAgo)
	subOld := seedSubscriptionOrder(t, payer.Id, "sub-old", dayAgo)
	subRecent := seedSubscriptionOrder(t, payer.Id, "sub-recent", twoHoursAgo)
	otherSubOld := seedSubscriptionOrder(t, other.Id, "other-sub-old", dayAgo)

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

	// The two-hour-old orders may still be in checkout: they keep refusing the join.
	res := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, false, orgAssignPersonalBalanceMessage)

	// Once the recent top-up is abandoned too, the recent subscription order
	// alone still refuses the join.
	require.NoError(t, f.db.Model(&model.TopUp{}).Where("id = ?", recent.Id).Update("create_time", dayAgo).Error)
	payload = expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.Equal(t, model.PersonalOrderExpiry{ExpiredTopUps: 1, SkippedRecentSubscriptions: 1}, payload)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, false, orgAssignPersonalBalanceMessage)

	// Once it is abandoned too, a third expiry clears the way.
	require.NoError(t, f.db.Model(&model.SubscriptionOrder{}).Where("id = ?", subRecent.Id).Update("create_time", dayAgo).Error)
	payload = expirePersonalTopUpsAsAdmin(t, fmt.Sprintf(`{"user_id": %d}`, payer.Id))
	require.Equal(t, model.PersonalOrderExpiry{ExpiredSubscriptions: 1}, payload)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(payer.Id))
	requireOrgSecurityResult(t, res, true, "")
	orgId, err := model.GetUserOrganizationId(payer.Id)
	require.NoError(t, err)
	require.Equal(t, f.org.Id, orgId)
	requireTopUpStatuses(t, map[int]string{orgOrder.Id: common.TopUpStatusPending, otherOld.Id: common.TopUpStatusPending})
	requireSubscriptionOrderStatuses(t, map[int]string{otherSubOld.Id: common.TopUpStatusPending})
}

// There is no all-users sweep: an admin who forgets the body must not expire
// every user's open checkout.
func TestAdminExpirePendingTopUpsRequiresUserId(t *testing.T) {
	f := setupOrgSecurityTest(t)
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	dayAgo := common.GetTimestamp() - 25*3600
	order := seedOrganizationTopUp(t, f.member.Id, model.TopUpTargetTypeUser, f.member.Id, common.TopUpStatusPending, "personal-abandoned", dayAgo)
	subscription := seedSubscriptionOrder(t, f.member.Id, "sub-abandoned", dayAgo)

	for _, body := range []string{``, `{}`, `{"user_id": 0}`} {
		res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
		requireOrgSecurityResult(t, res, false, "user_id is required")
		require.Contains(t, res.Body.String(), `"message_key":"user_id is required"`, body)
	}
	requireTopUpStatuses(t, map[int]string{order.Id: common.TopUpStatusPending})
	requireSubscriptionOrderStatuses(t, map[int]string{subscription.Id: common.TopUpStatusPending})

	var audits int64
	require.NoError(t, f.db.Model(&model.AuditLog{}).Where("action = ?", "topup.expire").Count(&audits).Error)
	require.Zero(t, audits)

	// The model refuses a missing user too, with a translatable message.
	_, err := model.ExpirePendingPersonalTopUps(0)
	var message *common.Message
	require.ErrorAs(t, err, &message)
	require.Equal(t, "Invalid user ID", message.Key)
}

func TestAdminExpirePendingTopUpsRejectsInvalidUserId(t *testing.T) {
	setupOrgSecurityTest(t)
	admin := model.User{Id: 9998, Role: common.RoleAdminUser}
	for _, body := range []string{`{"user_id": -1}`, `{"user_id": "x"}`, `not json`} {
		res := orgSecurityRequest(AdminExpirePendingTopUps, admin, http.MethodPost, "/api/user/topup/expire", body)
		requireOrgSecurityResult(t, res, false, "Invalid parameters")
	}
}
