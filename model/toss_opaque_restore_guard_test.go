package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRestoredOpaqueRenewalRowBlocksFreshRecurringAndInitialPaths(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "opaque-restore-renewal-key", 0)
	seedTossBillingPlan(t, "Opaque restore renewal", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)

	tradeNo := tossRenewalOpaqueOrderIDPrefix + strings.Repeat("c", 40)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 7, PlanId: 3, TradeNo: tradeNo,
		// A partial restore can lose the discriminator independently of trade_no.
		// The reserved opaque prefix must remain sufficient global evidence.
		PaymentMethod: PaymentMethodToss, PaymentProvider: "",
		Status: common.TopUpStatusPending, BillingKeyId: keyID,
	}).Error)

	_, err := PrepareTossRenewalOrder(
		sub.Id,
		tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount),
		1,
		TossPlanKRW(1),
	)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)

	var plan SubscriptionPlan
	require.NoError(t, DB.First(&plan, 3).Error)
	err = CreateTossSubscriptionOrderWithPurchaseReservation(&SubscriptionOrder{
		UserId: 7, PlanId: 3, TradeNo: "toss_sub_after_opaque_restore",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}, &plan)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)

	expired, err := ExpireStaleTossPendingSubscriptionOrders(GetDBTimestamp() + 1)
	require.NoError(t, err)
	require.Zero(t, expired)
	var restored SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&restored).Error)
	require.Equal(t, common.TopUpStatusPending, restored.Status)

	err = CompleteTossBillingOrder(tradeNo, keyID, "")
	require.Error(t, err)
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id <> ?", sub.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestRestoredOpaqueWalletRowBlocksClassifierWriterAndGenericCleanup(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, now, _ := seedWalletProtocolFencePolicy(t)
	tradeNo := tossWalletOpaqueOrderIDPrefix + strings.Repeat("d", 40)
	require.NoError(t, DB.Create(&TopUp{
		UserId: policy.OwnerUserId, TargetType: policy.TargetType, TargetId: policy.TargetId,
		TradeNo: tradeNo, ProviderOrderId: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: "",
		Status: common.TopUpStatusPending,
	}).Error)

	_, isWallet, err := GetWalletAutoRechargeByChargeTradeNo(tradeNo)
	require.False(t, isWallet)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)

	charge, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
	require.False(t, charge.shouldCharge)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)

	expired, err := ExpireStaleTossPendingTopUps(GetDBTimestamp() + 1)
	require.NoError(t, err)
	require.Zero(t, expired)
	var restored TopUp
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&restored).Error)
	require.Equal(t, common.TopUpStatusPending, restored.Status)

	hasActivity, err := HasWalletAutoRechargeProviderActivity(&policy)
	require.True(t, hasActivity)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
}

func TestIncompleteVersionZeroRecurringAssociationIsGloballyCorrupt(t *testing.T) {
	t.Run("subscription", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
		billingTime := int64(1782840000)
		require.NoError(t, DB.Create(&SubscriptionOrder{
			TradeNo: "partial_version_zero_renewal", PaymentProvider: PaymentProviderToss,
			RenewalBillingTime: &billingTime,
		}).Error)
		require.ErrorIs(t, ensureNoIncompleteTossRenewalOrderIdentityTx(DB), ErrTossRecurringOrderIDEvidenceCorrupt)
	})

	t.Run("wallet", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policyID := 71
		require.NoError(t, DB.Create(&TopUp{
			TradeNo: "partial_version_zero_wallet", PaymentMethod: PaymentMethodToss,
			PaymentProvider: PaymentProviderToss, WalletAutoRechargeId: &policyID,
		}).Error)
		require.ErrorIs(t, ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(DB), ErrTossRecurringOrderIDEvidenceCorrupt)
	})
}

func TestUserDeletionLifecycleDoesNotExpireRestoredOpaqueRenewalAsInitial(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&User{
		Id: 77, Username: "opaque-restore-delete-owner", Password: "password",
		Status: common.UserStatusEnabled, Group: "default", AffCode: "opaque-restore-delete-owner",
	}).Error)
	tradeNo := tossRenewalOpaqueOrderIDPrefix + strings.Repeat("e", 40)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 77, TradeNo: tradeNo, PaymentMethod: PaymentMethodToss,
		PaymentProvider: "", Status: common.TopUpStatusPending,
	}).Error)

	err := DB.Transaction(func(tx *gorm.DB) error {
		return prepareUserTossPaymentsForDeletionTx(tx, 77)
	})
	require.ErrorIs(t, err, ErrUserTossPaymentInFlight)
	var restored SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&restored).Error)
	require.Equal(t, common.TopUpStatusPending, restored.Status)
}
