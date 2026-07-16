package model

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestCompleteTossBillingOrderUsesOriginalPlanSnapshot(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       8101,
		Username: "snapshot-initial-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	plan := &SubscriptionPlan{
		Id:                      8201,
		Title:                   "Original initial terms",
		PriceAmount:             10,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationCustom,
		CustomSeconds:           7200,
		Enabled:                 true,
		UpgradeGroup:            "snapshot-premium",
		TotalAmount:             1234,
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: 600,
	}
	require.NoError(t, DB.Create(plan).Error)
	order := &SubscriptionOrder{
		UserId:           8101,
		PlanId:           plan.Id,
		Money:            plan.PriceAmount,
		TradeNo:          "toss-plan-snapshot-initial",
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusPending,
		CreateTime:       common.GetTimestamp(),
		ProviderAmount:   10000,
		ProviderCurrency: "KRW",
		BillingKeyId:     8301,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	require.NoError(t, DB.Create(order).Error)

	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"title":                      "Changed after order",
		"duration_unit":              SubscriptionDurationCustom,
		"custom_seconds":             60,
		"upgrade_group":              "changed-group",
		"total_amount":               9999,
		"quota_reset_period":         SubscriptionResetNever,
		"quota_reset_custom_seconds": 0,
	}).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	require.NoError(t, CompleteTossBillingOrder(order.TradeNo, order.BillingKeyId, `{"status":"DONE"}`))
	var sub UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", order.UserId, plan.Id).First(&sub).Error)
	require.Equal(t, int64(7200), sub.EndTime-sub.StartTime)
	require.Equal(t, int64(1234), sub.AmountTotal)
	require.Equal(t, int64(600), sub.NextResetTime-sub.StartTime)
	require.Equal(t, "snapshot-premium", sub.UpgradeGroup)
	var user User
	require.NoError(t, DB.First(&user, order.UserId).Error)
	require.Equal(t, "snapshot-premium", user.Group)
}

func TestRenewTossSubscriptionUsesPreparedPlanSnapshot(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       8401,
		Username: "snapshot-renewal-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	providerCredential, err := common.EncryptString("snapshot-renewal-provider-secret")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:                 8501,
		UserId:             8401,
		ProviderCredential: providerCredential,
		Status:             BillingKeyStatusActive,
	}).Error)
	plan := &SubscriptionPlan{
		Id:                      8601,
		Title:                   "Original renewal terms",
		PriceAmount:             10,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationCustom,
		CustomSeconds:           7200,
		Enabled:                 true,
		UpgradeGroup:            "renewal-snapshot-premium",
		TotalAmount:             777,
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: 600,
	}
	require.NoError(t, DB.Create(plan).Error)
	oldEnd := time.Now().Add(24 * time.Hour).Unix()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:              8701,
		UserId:          8401,
		PlanId:          plan.Id,
		AmountTotal:     111,
		AmountUsed:      100,
		StartTime:       oldEnd - 7200,
		EndTime:         oldEnd,
		Status:          "active",
		AutoRenew:       true,
		NextBillingTime: oldEnd - 60,
		BillingKeyId:    8501,
	}).Error)
	seedTossRenewalContractForTest(t, 8701, plan, 10000)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })
	tradeNo := tossRenewalTradeNo(8701, oldEnd-60, 0)
	order, err := PrepareTossRenewalOrder(8701, tradeNo, plan.PriceAmount, 10000)
	require.NoError(t, err)
	require.NotNil(t, order)
	tradeNo = order.TradeNo
	require.NotEmpty(t, order.PlanSnapshot)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_attempted":          true,
		"billing_attempt_credential": "test-attempt-credential",
	}).Error)

	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"duration_unit":              SubscriptionDurationCustom,
		"custom_seconds":             14400,
		"total_amount":               9999,
		"upgrade_group":              "changed-renewal-group",
		"quota_reset_period":         SubscriptionResetNever,
		"quota_reset_custom_seconds": 0,
	}).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	before := common.GetTimestamp()
	require.NoError(t, RenewTossSubscription(8701, tradeNo, order.Money, order.ProviderAmount, `{"status":"DONE"}`))
	after := common.GetTimestamp()

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 8701).Error)
	require.Equal(t, oldEnd+7200, sub.EndTime)
	require.Equal(t, int64(777), sub.AmountTotal)
	require.Zero(t, sub.AmountUsed)
	require.Equal(t, "renewal-snapshot-premium", sub.UpgradeGroup)
	require.GreaterOrEqual(t, sub.NextResetTime, before+600)
	require.LessOrEqual(t, sub.NextResetTime, after+600)
	var user User
	require.NoError(t, DB.First(&user, 8401).Error)
	require.Equal(t, "renewal-snapshot-premium", user.Group)
}

func TestRenewTossSubscriptionRejectsOrderOutsideImmutableCycleContract(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 8741, Username: "renewal-contract-guard", Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	providerCredential, err := common.EncryptString("immutable-renewal-provider-secret")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id: 8742, UserId: 8741, ProviderCredential: providerCredential, Status: BillingKeyStatusActive,
	}).Error)
	plan := &SubscriptionPlan{
		Id: 8743, Title: "Immutable renewal", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationDay, DurationValue: 1, Enabled: true, TotalAmount: 500,
	}
	require.NoError(t, DB.Create(plan).Error)
	nextBillingTime := time.Now().Unix()
	oldEnd := nextBillingTime + 3600
	require.NoError(t, DB.Create(&UserSubscription{
		Id: 8744, UserId: 8741, PlanId: plan.Id, AmountTotal: plan.TotalAmount,
		StartTime: oldEnd - 86400, EndTime: oldEnd, Status: "active", AutoRenew: true,
		NextBillingTime: nextBillingTime, BillingKeyId: 8742,
	}).Error)
	seedTossRenewalContractForTest(t, 8744, plan, 10000)
	tradeNo := tossRenewalTradeNo(8744, nextBillingTime, 0)
	order, err := PrepareTossRenewalOrder(8744, tradeNo, plan.PriceAmount, 10000)
	require.NoError(t, err)
	require.NotNil(t, order)
	tradeNo = order.TradeNo
	tamperedSnapshot := strings.Replace(order.PlanSnapshot, `"total_amount":500`, `"total_amount":999`, 1)
	require.NotEqual(t, order.PlanSnapshot, tamperedSnapshot)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"plan_snapshot":              tamperedSnapshot,
		"billing_attempted":          true,
		"billing_attempt_credential": "test-attempt-credential",
	}).Error)

	err = RenewTossSubscription(8744, tradeNo, plan.PriceAmount, 10000, `{"status":"DONE"}`)
	require.ErrorIs(t, err, ErrSubscriptionPlanSnapshotMismatch)
	var storedOrder SubscriptionOrder
	require.NoError(t, DB.First(&storedOrder, order.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedOrder.Status)
	var storedSub UserSubscription
	require.NoError(t, DB.First(&storedSub, 8744).Error)
	require.Equal(t, oldEnd, storedSub.EndTime)
	require.False(t, storedSub.AutoRenew)
	require.Zero(t, storedSub.NextBillingTime)
}

func TestRenewTossSubscriptionRejectsMalformedAndInitialOrderIDs(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	for _, tradeNo := range []string{"ordinary_initial_order", TossRenewalTradeNoPrefix + "malformed"} {
		err := RenewTossSubscription(1, tradeNo, 10, 10000)
		require.ErrorIs(t, err, ErrSubscriptionOrderStatusInvalid)
	}
}

func TestInvalidTossSubscriptionPlanSnapshotDoesNotSettle(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       8801,
		Username: "snapshot-invalid-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	plan := &SubscriptionPlan{
		Id:            8901,
		Title:         "Snapshot validation",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	order := &SubscriptionOrder{
		UserId:           8801,
		PlanId:           plan.Id,
		Money:            plan.PriceAmount,
		TradeNo:          "toss-plan-snapshot-invalid",
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusPending,
		CreateTime:       common.GetTimestamp(),
		ProviderAmount:   10000,
		ProviderCurrency: "KRW",
		BillingKeyId:     9001,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	order.PlanSnapshot = strings.Replace(order.PlanSnapshot, `"plan_id":8901`, `"plan_id":8902`, 1)
	require.NoError(t, DB.Create(order).Error)

	err := CompleteTossBillingOrder(order.TradeNo, order.BillingKeyId, `{"status":"DONE"}`)
	require.ErrorIs(t, err, ErrSubscriptionPlanSnapshotMismatch)
	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", order.UserId).Count(&count).Error)
	require.Zero(t, count)
}

func TestGetSubscriptionPlanByIdForPaymentBypassesStaleDisplayCache(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}))
	plan := SubscriptionPlan{
		Id: 99201, Title: "Cached plan", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })

	cached, err := GetSubscriptionPlanById(plan.Id)
	require.NoError(t, err)
	require.Equal(t, 10.0, cached.PriceAmount)
	require.True(t, cached.Enabled)

	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"price_amount": 25,
		"enabled":      false,
	}).Error)
	stillCached, err := GetSubscriptionPlanById(plan.Id)
	require.NoError(t, err)
	require.Equal(t, 10.0, stillCached.PriceAmount, "display reads should establish the stale-cache regression")

	fresh, err := GetSubscriptionPlanByIdForPayment(plan.Id)
	require.NoError(t, err)
	require.Equal(t, 25.0, fresh.PriceAmount)
	require.False(t, fresh.Enabled)
}
