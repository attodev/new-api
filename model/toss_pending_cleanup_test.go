package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestReconcileStaleTossRecordedTopUpsSelectsOnlyRecordedOldPending(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "recorded_old",
			ProviderOrderId: "pay_recorded_old",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      1000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "never_recorded_old",
			ProviderOrderId: "never_recorded_old",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      1000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "recorded_recent",
			ProviderOrderId: "pay_recorded_recent",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      3000,
			Status:          common.TopUpStatusPending,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossTopUpReconciler
	var seen []string
	SetTossTopUpReconciler(func(ctx context.Context, topUp TopUp) (bool, error) {
		seen = append(seen, topUp.TradeNo)
		return true, nil
	})
	t.Cleanup(func() {
		SetTossTopUpReconciler(previous)
	})

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), 2000, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"recorded_old"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsUsesPaymentKeyRecordedTime(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId:            7,
			Amount:            13000,
			Money:             10,
			TradeNo:           "old_order_recent_payment_key",
			ProviderOrderId:   "pay_recent",
			ProviderOrderTime: 3000,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			CreateTime:        1000,
			Status:            common.TopUpStatusPending,
		},
		{
			UserId:            7,
			Amount:            13000,
			Money:             10,
			TradeNo:           "recent_order_old_payment_key",
			ProviderOrderId:   "pay_old",
			ProviderOrderTime: 1000,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			CreateTime:        3000,
			Status:            common.TopUpStatusPending,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossTopUpReconciler
	var seen []string
	SetTossTopUpReconciler(func(ctx context.Context, topUp TopUp) (bool, error) {
		seen = append(seen, topUp.TradeNo)
		return true, nil
	})
	t.Cleanup(func() {
		SetTossTopUpReconciler(previous)
	})

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), 2000, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"recent_order_old_payment_key"}, seen)
}

func TestExpireStaleTossPendingSubscriptionOrdersExpiresOnlyOldTossPending(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	rows := []SubscriptionOrder{
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "old_toss_pending",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      1000,
		},
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "recent_toss_pending",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      3000,
		},
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "old_stripe_pending",
			PaymentMethod:   PaymentMethodStripe,
			PaymentProvider: PaymentProviderStripe,
			Status:          common.TopUpStatusPending,
			CreateTime:      1000,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	expired, err := ExpireStaleTossPendingSubscriptionOrders(2000)
	require.NoError(t, err)
	require.Equal(t, int64(1), expired)

	require.Equal(t, common.TopUpStatusExpired, GetSubscriptionOrderByTradeNo("old_toss_pending").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("recent_toss_pending").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_stripe_pending").Status)
}

func TestExpireStaleTossPendingSubscriptionOrdersSkipsAttachedBillingKeyUntilReconciled(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	keyId, err := StoreTossBillingKey(7, "cust_expire", "billing_expire", "현대", "433012******1234")
	require.NoError(t, err)
	order := &SubscriptionOrder{
		UserId:          7,
		PlanId:          3,
		Money:           10,
		TradeNo:         "old_toss_pending_with_key",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      1000,
		BillingKeyId:    keyId,
	}
	require.NoError(t, DB.Create(order).Error)

	expired, err := ExpireStaleTossPendingSubscriptionOrders(2000)
	require.NoError(t, err)
	require.Equal(t, int64(0), expired)

	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_key").Status)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestReconcileStaleTossPendingSubscriptionOrdersSelectsOnlyOldAttachedPending(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	keyId, err := StoreTossBillingKey(7, "cust_reconcile", "billing_reconcile", "현대", "433012******1234")
	require.NoError(t, err)
	rows := []SubscriptionOrder{
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "old_toss_pending_with_key",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      1000,
			BillingKeyId:    keyId,
		},
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "old_toss_pending_without_key",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      1000,
		},
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "recent_toss_pending_with_key",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      3000,
			BillingKeyId:    keyId,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossSubscriptionOrderReconciler
	var seen []string
	SetTossSubscriptionOrderReconciler(func(ctx context.Context, order SubscriptionOrder) (bool, error) {
		seen = append(seen, order.TradeNo)
		return true, nil
	})
	t.Cleanup(func() {
		SetTossSubscriptionOrderReconciler(previous)
	})

	resolved, err := ReconcileStaleTossPendingSubscriptionOrders(context.Background(), 2000, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"old_toss_pending_with_key"}, seen)
}
