package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fork: O3 — inside an organization users.quota is the member spending cap set
// by organization admins, not money the member paid. It must not buy platform
// subscriptions, and personal money must not flow into it (redemption, personal
// paid top-ups).

func seedOrganizationMemberWithCap(t *testing.T, userId int, orgId int, quota int) model.User {
	t.Helper()
	owner := model.User{Id: userId + 1000, Username: fmt.Sprintf("owner-%d", userId), Password: "x", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, OrganizationId: orgId, OrganizationRole: model.OrganizationRoleOwner, AffCode: fmt.Sprintf("owner-%d", userId)}
	member := model.User{Id: userId, Username: fmt.Sprintf("member-%d", userId), Password: "x", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Quota: quota, OrganizationId: orgId, OrganizationRole: model.OrganizationRoleMember, AffCode: fmt.Sprintf("member-%d", userId)}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: orgId, Name: fmt.Sprintf("org-%d", orgId), OwnerUserId: owner.Id,
		Status: model.OrganizationStatusEnabled, Quota: 1000}).Error)
	return member
}

func requireUserQuota(t *testing.T, userId int, want int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, userId).Error)
	require.Equal(t, want, user.Quota)
}

func TestOrganizationMemberCannotBuySubscriptionWithBalance(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}))
	member := seedOrganizationMemberWithCap(t, 2, 1, 9_000_000_000)
	plan := model.SubscriptionPlan{Title: "pro", PriceAmount: 1, Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(SubscriptionRequestBalancePay, member, http.MethodPost, "/api/subscription/balance/pay",
		fmt.Sprintf(`{"plan_id":%d}`, plan.Id))

	requireOrganizationApiError(t, res, "organization members cannot pay with personal balance")
	requireUserQuota(t, member.Id, 9_000_000_000)
	var subscriptions int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Count(&subscriptions).Error)
	require.Zero(t, subscriptions)
	var orders int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&orders).Error)
	require.Zero(t, orders)
}

func TestOrganizationMemberCannotRedeemIntoMemberCap(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Redemption{}))
	member := seedOrganizationMemberWithCap(t, 2, 1, 100)
	code := model.Redemption{Key: "0123456789abcdef0123456789abcdef", Status: common.RedemptionCodeStatusEnabled, Name: "gift", Quota: 5000}
	require.NoError(t, model.DB.Create(&code).Error)

	res := performOrganizationRequest(TopUp, member, http.MethodPost, "/api/user/topup", fmt.Sprintf(`{"key":%q}`, code.Key))

	requireOrganizationApiError(t, res, "organization members cannot redeem codes into their personal balance")
	requireUserQuota(t, member.Id, 100)
	var reloaded model.Redemption
	require.NoError(t, model.DB.First(&reloaded, code.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, reloaded.Status)
}

func TestOrganizationMemberCannotStartPersonalPaidTopUp(t *testing.T) {
	providerCalls := setupOrganizationPaymentCapacityTest(t)
	owner, _ := createPaymentCapacityOrganization(t, 321, 51, 0, 0)

	res := performOrganizationRequest(RequestPayPalPay, owner, http.MethodPost, "/api/user/paypal/pay",
		`{"amount":10,"payment_method":"paypal"}`)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.JSONEq(t, `{"message":"error","data":"organization members cannot top up their personal balance","message_key":"organization members cannot top up their personal balance"}`, res.Body.String())
	require.Zero(t, providerCalls.Load())
	requireOrganizationTopUpCount(t, 0)

	// The organization wallet flow of the same owner is not affected.
	res = performOrganizationRequest(RequestOrganizationPayPalPay, owner, http.MethodPost, "/api/organization/paypal/pay",
		`{"amount":10,"payment_method":"paypal"}`)
	assert.JSONEq(t, `{"message":"error","data":"Failed to start payment","message_key":"Failed to start payment"}`, res.Body.String())
}
