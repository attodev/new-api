package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestStopTossSubscriptionBillingAfterCancellationDisablesOnlyOriginSubscription(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&User{
		Id:       901,
		Username: "cancel-linked-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "cancel-linked-aff",
	}).Error)
	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		901,
		"cancel-linked-customer",
		"cancel-linked-billing-key",
		"현대",
		"433012******1234",
		"test_sk_billing_model_default",
		TossClientKeyFingerprint("ck_test_billing_model_default"),
	)
	require.NoError(t, err)
	for _, id := range []int{911, 912} {
		require.NoError(t, DB.Create(&UserSubscription{
			Id:           id,
			UserId:       901,
			PlanId:       1,
			Status:       "active",
			AutoRenew:    true,
			BillingKeyId: keyID,
		}).Error)
	}
	activeKey := "wallet-cancel-linked"
	require.NoError(t, DB.Create(&WalletAutoRecharge{
		Id:           921,
		OwnerUserId:  901,
		BillingKeyId: keyID,
		ActiveKey:    &activeKey,
		Status:       WalletAutoRechargeStatusActive,
	}).Error)
	plan := &SubscriptionPlan{
		Id: 1, Title: "Cancellation origin", PriceAmount: 1, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	order := &SubscriptionOrder{
		UserId:           901,
		PlanId:           1,
		Money:            plan.PriceAmount,
		TradeNo:          "toss_sub_cancel_linked",
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusSuccess,
		ProviderAmount:   1000,
		ProviderCurrency: "KRW",
		BillingKeyId:     keyID,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	require.NoError(t, DB.Create(order).Error)
	var origin UserSubscription
	require.NoError(t, DB.First(&origin, 911).Error)
	require.NoError(t, SetTossRenewalContractFromInitialOrder(&origin, order))
	require.NoError(t, DB.Model(&origin).Update("toss_renewal_contract_snapshot", origin.TossRenewalContractSnapshot).Error)

	require.NoError(t, StopTossSubscriptionBillingAfterCancellation("toss_sub_cancel_linked", `{"status":"CANCELED"}`))

	var reloadedOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_cancel_linked").First(&reloadedOrder).Error)
	require.Equal(t, common.TopUpStatusSuccess, reloadedOrder.Status)
	require.NoError(t, DB.First(&origin, 911).Error)
	require.False(t, origin.AutoRenew)
	var other UserSubscription
	require.NoError(t, DB.First(&other, 912).Error)
	require.True(t, other.AutoRenew)
	var wallet WalletAutoRecharge
	require.NoError(t, DB.First(&wallet, 921).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, wallet.Status)
	require.NotNil(t, wallet.ActiveKey)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestMarkTossRenewalChargeAttemptRejectsConcurrentAutoRenewCancellation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&User{
		Id:       950,
		Username: "renewal-cancel-race-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "renewal-cancel-race-aff",
	}).Error)
	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		950,
		"renewal-cancel-race-customer",
		"renewal-cancel-race-key",
		"현대",
		"433012******1234",
		"test_sk_billing_model_default",
		TossClientKeyFingerprint("ck_test_billing_model_default"),
	)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserSubscription{
		Id:           951,
		UserId:       950,
		PlanId:       1,
		Status:       "active",
		AutoRenew:    false,
		BillingKeyId: keyID,
	}).Error)
	tradeNo := TossRenewalTradeNoPrefix + "951_12345"
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:            950,
		PlanId:            1,
		TradeNo:           tradeNo,
		PaymentMethod:     PaymentMethodToss,
		PaymentProvider:   PaymentProviderToss,
		Status:            common.TopUpStatusPending,
		BillingKeyId:      keyID,
		BillingClaimToken: "renewal-cancel-race-claim",
	}).Error)

	err = MarkTossRenewalChargeAttempt(tradeNo, "renewal-cancel-race-claim", "test_sk_billing_model_default")
	require.ErrorIs(t, err, ErrSubscriptionOrderStatusInvalid)
	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&order).Error)
	require.False(t, order.BillingAttempted)
	require.Empty(t, order.BillingAttemptCredential)
}
