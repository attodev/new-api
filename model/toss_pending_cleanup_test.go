package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestExpireStaleTossPendingTopUpsClosesOnlyNeverStartedOrders(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_old_never_started", ProviderOrderId: "toss_old_never_started",
			ProviderCredential: "encrypted-secret", ProviderClientKeyHash: "client-key-fingerprint",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000,
		},
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_recent_never_started", ProviderOrderId: "toss_recent_never_started",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 3000,
		},
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_old_recorded", ProviderOrderId: "pay_old_recorded",
			ProviderCredential: "encrypted-secret", ProviderClientKeyHash: "client-key-fingerprint",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000,
		},
		{
			// Before provider_attempted existed, a wallet charge could time out
			// after POST while retaining this exact pre-POST shape.
			UserId: 7, Amount: 13000, TradeNo: "wallet_auto_17_1000", ProviderOrderId: "wallet_auto_17_1000",
			ProviderCredential: "encrypted-wallet-secret", ProviderClientKeyHash: "wallet-client-key-fingerprint",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000, ProviderAttempted: false,
		},
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_old_attempted_without_key_marker", ProviderOrderId: "toss_old_attempted_without_key_marker",
			ProviderCredential: "encrypted-attempt-secret", ProviderClientKeyHash: "attempt-client-key-fingerprint",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000, ProviderAttempted: true,
		},
		{
			// A nonzero wallet order protocol with no complete association is
			// corrupt/opaque evidence, not a generic pristine top-up.
			UserId: 7, Amount: 13000, TradeNo: "opaque_wallet_order_without_tuple", ProviderOrderId: "opaque_wallet_order_without_tuple",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		},
		{
			UserId: 7, Amount: 13000, TradeNo: "stripe_old_pending", ProviderOrderId: "stripe_old_pending",
			PaymentMethod: PaymentMethodStripe, PaymentProvider: PaymentProviderStripe,
			Status: common.TopUpStatusPending, CreateTime: 1000,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	expired, err := ExpireStaleTossPendingTopUps(2000)
	require.NoError(t, err)
	require.Equal(t, int64(1), expired)

	var oldNeverStarted TopUp
	require.NoError(t, DB.Where("trade_no = ?", "toss_old_never_started").First(&oldNeverStarted).Error)
	require.Equal(t, common.TopUpStatusExpired, oldNeverStarted.Status)
	require.Greater(t, oldNeverStarted.CompleteTime, int64(0))
	require.Empty(t, oldNeverStarted.ProviderCredential)
	require.Empty(t, oldNeverStarted.ProviderClientKeyHash)
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo("toss_recent_never_started").Status)
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo("toss_old_recorded").Status)
	legacyWallet := GetTopUpByTradeNo("wallet_auto_17_1000")
	require.Equal(t, common.TopUpStatusPending, legacyWallet.Status)
	require.Equal(t, "encrypted-wallet-secret", legacyWallet.ProviderCredential)
	require.Equal(t, "wallet-client-key-fingerprint", legacyWallet.ProviderClientKeyHash)
	attempted := GetTopUpByTradeNo("toss_old_attempted_without_key_marker")
	require.Equal(t, common.TopUpStatusPending, attempted.Status)
	require.True(t, attempted.ProviderAttempted)
	require.Equal(t, "encrypted-attempt-secret", attempted.ProviderCredential)
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo("opaque_wallet_order_without_tuple").Status)
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo("stripe_old_pending").Status)
}

func TestReconcileStaleTossRecordedTopUpsDoesNotLetFailedLimitBatchStarveNextOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId:            7,
			Amount:            13000,
			TradeNo:           "toss_recorded_stuck_oldest",
			ProviderOrderId:   "pay_toss_recorded_stuck_oldest",
			ProviderOrderTime: 1000,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			Status:            common.TopUpStatusPending,
		},
		{
			UserId:            7,
			Amount:            13000,
			TradeNo:           "toss_recorded_next_due",
			ProviderOrderId:   "pay_toss_recorded_next_due",
			ProviderOrderTime: 1100,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			Status:            common.TopUpStatusPending,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossTopUpReconciler
	var seen []string
	SetTossTopUpReconciler(func(ctx context.Context, topUp TopUp) (bool, error) {
		seen = append(seen, topUp.TradeNo)
		if topUp.TradeNo == "toss_recorded_stuck_oldest" {
			return false, errors.New("permanent credential failure")
		}
		return true, nil
	})
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), 2000, 1)
	require.Error(t, err)
	require.Zero(t, resolved)
	require.Equal(t, []string{"toss_recorded_stuck_oldest"}, seen)

	resolved, err = ReconcileStaleTossRecordedTopUps(context.Background(), 2000, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"toss_recorded_stuck_oldest", "toss_recorded_next_due"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsSelectsOnlyTerminalRowsWithActiveRefundFence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &TossPaymentEvent{}))
	rows := []TopUp{
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_terminal_refund_required", ProviderOrderId: "pay_terminal_refund_required",
			ProviderOrderTime: 1000, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: TossTopUpStatusRefundPending,
		},
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_terminal_refund_resolved", ProviderOrderId: "pay_terminal_refund_resolved",
			ProviderOrderTime: 900, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusFailed,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)
	require.NoError(t, DB.Create(&[]TossPaymentEvent{
		{
			EventKey: TossTopUpRefundRequiredEventKey(rows[0].TradeNo, rows[0].ProviderOrderId), EventType: TossPaymentEventTypeRefundRequired,
			OrderId: rows[0].TradeNo, PaymentKey: rows[0].ProviderOrderId, OriginalAmount: rows[0].Amount,
			ReconciliationStatus: TossReconciliationStatusRequired,
		},
		{
			EventKey: TossTopUpRefundRequiredEventKey(rows[1].TradeNo, rows[1].ProviderOrderId), EventType: TossPaymentEventTypeRefundRequired,
			OrderId: rows[1].TradeNo, PaymentKey: rows[1].ProviderOrderId, OriginalAmount: rows[1].Amount,
			ReconciliationStatus: TossReconciliationStatusResolved,
		},
	}).Error)

	previous := tossTopUpReconciler
	var seen []string
	SetTossTopUpReconciler(func(_ context.Context, topUp TopUp) (bool, error) {
		seen = append(seen, topUp.TradeNo)
		return true, nil
	})
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), 2000, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, resolved)
	require.Equal(t, []string{"toss_terminal_refund_required"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsPrioritizesEarliestApprovalDeadline(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	now := GetDBTimestamp()
	rows := []TopUp{
		{
			UserId: 7, Amount: 13000, TradeNo: "toss_deadline_earliest", ProviderOrderId: "pay_deadline_earliest",
			ProviderOrderTime: now - 500, ProviderRetryTime: now - 1,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		},
		{
			// A zero retry marker used to sort ahead of the older paymentKey even
			// though this row has substantially more approval time remaining.
			UserId: 7, Amount: 13000, TradeNo: "toss_deadline_later", ProviderOrderId: "pay_deadline_later",
			ProviderOrderTime: now - 100, ProviderRetryTime: 0,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossTopUpReconciler
	var seen []string
	SetTossTopUpReconciler(func(_ context.Context, topUp TopUp) (bool, error) {
		seen = append(seen, topUp.TradeNo)
		return true, nil
	})
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), now-50, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"toss_deadline_earliest"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsProtectsLiveDeadlineWhenPoisonRetriesReturn(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &TossPaymentEvent{}))
	now := GetDBTimestamp()

	// This is larger than eight complete SQLite recovery ticks (4 workers * 8).
	// Every poison row represents an old failed attempt whose two-minute retry
	// lease has just become due again. Ordering only by ProviderOrderTime would
	// select the first four forever at each returning lease boundary and never
	// reach the newer live checkout inserted after the backlog.
	const poisonCount = 129
	poison := make([]TopUp, poisonCount)
	for i := range poison {
		poison[i] = TopUp{
			UserId:            7,
			Amount:            13000,
			TradeNo:           fmt.Sprintf("toss_returned_poison_%03d", i),
			ProviderOrderId:   fmt.Sprintf("pay_returned_poison_%03d", i),
			ProviderOrderTime: now - 20*60,
			ProviderRetryTime: now,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			CreateTime:        now - 20*60,
			Status:            common.TopUpStatusPending,
		}
	}
	require.NoError(t, DB.CreateInBatches(&poison, 16).Error)

	live := TopUp{
		UserId:            7,
		Amount:            13000,
		TradeNo:           "toss_live_deadline_behind_returned_poison",
		ProviderOrderId:   "pay_live_deadline_behind_returned_poison",
		ProviderOrderTime: now - 60,
		ProviderRetryTime: now,
		PaymentMethod:     PaymentMethodToss,
		PaymentProvider:   PaymentProviderToss,
		CreateTime:        now - 60,
		Status:            common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&live).Error)

	// A deposited virtual-account refund can be deliberately deferred for 24
	// hours while an operator supplies refund account information. It must not be
	// promoted into the checkout deadline lane or selected before its retry time.
	manualRefund := TopUp{
		UserId:            7,
		Amount:            14000,
		TradeNo:           "toss_manual_va_refund_deferred",
		ProviderOrderId:   "pay_manual_va_refund_deferred",
		ProviderOrderTime: now - 24*60*60,
		ProviderRetryTime: now + 24*60*60,
		PaymentMethod:     PaymentMethodToss,
		PaymentProvider:   PaymentProviderToss,
		CreateTime:        now - 24*60*60,
		Status:            TossTopUpStatusRefundPending,
	}
	require.NoError(t, DB.Create(&manualRefund).Error)
	require.NoError(t, DB.Create(&TossPaymentEvent{
		EventKey:             TossTopUpRefundRequiredEventKey(manualRefund.TradeNo, manualRefund.ProviderOrderId),
		EventType:            TossPaymentEventTypeRefundRequired,
		OrderId:              manualRefund.TradeNo,
		PaymentKey:           manualRefund.ProviderOrderId,
		OriginalAmount:       manualRefund.Amount,
		BalanceAmount:        manualRefund.Amount,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}).Error)

	previous := tossTopUpReconciler
	var seenMu sync.Mutex
	seen := make(map[string]struct{})
	SetTossTopUpReconciler(func(_ context.Context, topUp TopUp) (bool, error) {
		seenMu.Lock()
		seen[topUp.TradeNo] = struct{}{}
		seenMu.Unlock()
		if topUp.TradeNo == live.TradeNo {
			return true, nil
		}
		return false, errors.New("permanent credential failure")
	})
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), now-15, 100)
	require.Error(t, err)
	require.Equal(t, int64(1), resolved)
	seenMu.Lock()
	defer seenMu.Unlock()
	require.Contains(t, seen, live.TradeNo, "the live checkout must enter a finite SQLite tick before its approval deadline")
	require.NotContains(t, seen, manualRefund.TradeNo, "the 24-hour manual refund deferral must remain authoritative")
}

func TestReconcileStaleTossRecordedTopUpsUsesAllSQLiteCapacityForLiveEDFBeforePoison(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &TossPaymentEvent{}))
	now := GetDBTimestamp()

	// SQLite reserves four provider calls per 15-second scheduler tick. Eighty
	// live rows are therefore the exact five-minute capacity boundary. They have
	// nine minutes left, so an implementation that waits for a final-five-minute
	// "urgent" lane wastes the first four minutes and lets this older poison pool
	// consume all twenty finite batches.
	const (
		poisonCount = 129
		liveCount   = 80
		ticks       = 20
	)
	poison := make([]TopUp, poisonCount)
	for i := range poison {
		poison[i] = TopUp{
			UserId: 7, Amount: 13000,
			TradeNo: fmt.Sprintf("toss_edf_capacity_poison_%03d", i), ProviderOrderId: fmt.Sprintf("pay_edf_capacity_poison_%03d", i),
			ProviderOrderTime: now - 20*60, ProviderRetryTime: 0,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			CreateTime: now - 20*60, Status: common.TopUpStatusPending,
		}
	}
	require.NoError(t, DB.CreateInBatches(&poison, 16).Error)
	live := make([]TopUp, liveCount)
	for i := range live {
		// One-second spacing also proves EDF ordering remains deterministic while
		// every row is still safely inside the ten-minute approval window.
		live[i] = TopUp{
			UserId: 7, Amount: 13000,
			TradeNo: fmt.Sprintf("toss_edf_capacity_live_%03d", i), ProviderOrderId: fmt.Sprintf("pay_edf_capacity_live_%03d", i),
			ProviderOrderTime: now - 60 + int64(i%10), ProviderRetryTime: 0,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			CreateTime: now - 60 + int64(i%10), Status: common.TopUpStatusPending,
		}
	}
	require.NoError(t, DB.CreateInBatches(&live, 16).Error)

	previous := tossTopUpReconciler
	var seenMu sync.Mutex
	seenLive := make(map[string]struct{}, liveCount)
	poisonCalls := 0
	SetTossTopUpReconciler(func(_ context.Context, topUp TopUp) (bool, error) {
		seenMu.Lock()
		defer seenMu.Unlock()
		if strings.HasPrefix(topUp.TradeNo, "toss_edf_capacity_live_") {
			seenLive[topUp.TradeNo] = struct{}{}
			return true, nil
		}
		poisonCalls++
		return false, errors.New("poison row should remain in the background lane")
	})
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	var resolvedTotal int64
	for tick := 0; tick < ticks; tick++ {
		resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), now-15, 100)
		require.NoError(t, err)
		resolvedTotal += resolved
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	require.EqualValues(t, liveCount, resolvedTotal)
	require.Len(t, seenLive, liveCount)
	require.Zero(t, poisonCalls, "expired poison work must not consume live-checkout deadline capacity")
}

func TestReconcileStaleTossRecordedTopUpsCapsReservationToWorkerCapacity(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	now := GetDBTimestamp()
	rows := make([]TopUp, TossTopUpRecoverySQLiteWorkerCount+1)
	for i := range rows {
		rows[i] = TopUp{
			UserId: 7, Amount: 13000,
			TradeNo: fmt.Sprintf("toss_capacity_%d", i), ProviderOrderId: fmt.Sprintf("pay_capacity_%d", i),
			ProviderOrderTime: now - 100,
			PaymentMethod:     PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		}
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossTopUpReconciler
	SetTossTopUpReconciler(func(_ context.Context, _ TopUp) (bool, error) { return true, nil })
	t.Cleanup(func() { SetTossTopUpReconciler(previous) })

	resolved, err := ReconcileStaleTossRecordedTopUps(context.Background(), now-50, 100)
	require.NoError(t, err)
	require.EqualValues(t, TossTopUpRecoverySQLiteWorkerCount, resolved)

	var untouched int64
	require.NoError(t, DB.Model(&TopUp{}).
		Where("provider_retry_time = 0 OR provider_retry_time IS NULL").Count(&untouched).Error)
	require.EqualValues(t, 1, untouched)
}

func TestReconcileStaleTossSubscriptionOrdersDoesNotLetFailedLimitBatchStarveNextOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	providerCredential, err := EncryptProviderCredential("sk_stale_subscription_cleanup")
	require.NoError(t, err)

	rows := []SubscriptionOrder{
		{
			UserId:                       7,
			PlanId:                       3,
			TradeNo:                      "toss_sub_stuck_oldest",
			PaymentMethod:                PaymentMethodToss,
			PaymentProvider:              PaymentProviderToss,
			Status:                       common.TopUpStatusPending,
			CreateTime:                   1000,
			BillingKeyId:                 1,
			ProviderCredential:           providerCredential,
			BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
		},
		{
			UserId:                       7,
			PlanId:                       3,
			TradeNo:                      "toss_sub_next_due",
			PaymentMethod:                PaymentMethodToss,
			PaymentProvider:              PaymentProviderToss,
			Status:                       common.TopUpStatusPending,
			CreateTime:                   1100,
			BillingKeyId:                 1,
			ProviderCredential:           providerCredential,
			BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := tossSubscriptionOrderReconciler
	var seen []string
	SetTossSubscriptionOrderReconciler(func(ctx context.Context, order SubscriptionOrder) (bool, error) {
		seen = append(seen, order.TradeNo)
		token, claimed, err := ClaimTossUnattemptedSubscriptionChargeOrder(order.TradeNo)
		if err != nil {
			return false, err
		}
		if !claimed {
			return false, errors.New("subscription reconciliation claim was not acquired")
		}
		if err := ReleaseTossSubscriptionBillingClaim(order.TradeNo, token); err != nil {
			return false, err
		}
		if order.TradeNo == "toss_sub_stuck_oldest" {
			return false, errors.New("permanent subscription snapshot failure")
		}
		return true, nil
	})
	t.Cleanup(func() { SetTossSubscriptionOrderReconciler(previous) })

	resolved, err := ReconcileStaleTossPendingSubscriptionOrders(context.Background(), 2000, 1)
	require.Error(t, err)
	require.Zero(t, resolved)
	require.Equal(t, []string{"toss_sub_stuck_oldest"}, seen)

	resolved, err = ReconcileStaleTossPendingSubscriptionOrders(context.Background(), 2000, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []string{"toss_sub_stuck_oldest", "toss_sub_next_due"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsSelectsOnlyRecordedOldPending(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "toss_recorded_old",
			ProviderOrderId: "pay_toss_recorded_old",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      1000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "toss_never_recorded_old",
			ProviderOrderId: "toss_never_recorded_old",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      1000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "toss_recorded_recent",
			ProviderOrderId: "pay_toss_recorded_recent",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      3000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:          7,
			Amount:          13000,
			Money:           10,
			TradeNo:         "toss_recorded_other_method",
			ProviderOrderId: "pay_toss_recorded_other_method",
			PaymentMethod:   PaymentMethodStripe,
			PaymentProvider: PaymentProviderToss,
			CreateTime:      1000,
			Status:          common.TopUpStatusPending,
		},
		{
			UserId:            7,
			Amount:            13000,
			TradeNo:           "wallet_auto_7_1000",
			ProviderOrderId:   "pay_wallet_auto_7_1000",
			ProviderOrderTime: 1000,
			PaymentMethod:     PaymentMethodToss,
			PaymentProvider:   PaymentProviderToss,
			CreateTime:        1000,
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
	require.Equal(t, []string{"toss_recorded_old"}, seen)
}

func TestReconcileStaleTossRecordedTopUpsUsesPaymentKeyRecordedTime(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	rows := []TopUp{
		{
			UserId:            7,
			Amount:            13000,
			Money:             10,
			TradeNo:           "toss_old_order_recent_payment_key",
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
			TradeNo:           "toss_recent_order_old_payment_key",
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
	require.Equal(t, []string{"toss_recent_order_old_payment_key"}, seen)
}

func TestExpireStaleTossPendingSubscriptionOrdersExpiresOnlyOldTossPending(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	rows := []SubscriptionOrder{
		{
			UserId: 7, PlanId: 3, Money: 10, TradeNo: "old_toss_pending",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000,
			ProviderCredential: "encrypted-secret", ProviderClientKeyHash: "client-key-fingerprint",
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
			UserId:                  7,
			PlanId:                  3,
			Money:                   10,
			TradeNo:                 "old_toss_pending_with_issue_snapshot",
			PaymentMethod:           PaymentMethodToss,
			PaymentProvider:         PaymentProviderToss,
			Status:                  common.TopUpStatusPending,
			CreateTime:              1000,
			BillingIssueAuthKey:     "encrypted-auth-key",
			BillingIssueAuthKeyHash: "auth-key-hash",
			BillingIssueCustomerKey: "cust_issue_snapshot",
		},
		{
			UserId:                7,
			PlanId:                3,
			Money:                 10,
			TradeNo:               "old_toss_pending_with_issue_marker",
			PaymentMethod:         PaymentMethodToss,
			PaymentProvider:       PaymentProviderToss,
			Status:                common.TopUpStatusPending,
			CreateTime:            1000,
			BillingIssueAttempted: true,
		},
		{
			UserId:                   7,
			PlanId:                   3,
			Money:                    10,
			TradeNo:                  "old_toss_pending_with_charge_marker",
			PaymentMethod:            PaymentMethodToss,
			PaymentProvider:          PaymentProviderToss,
			Status:                   common.TopUpStatusPending,
			CreateTime:               1000,
			BillingAttempted:         true,
			BillingAttemptCredential: "encrypted-attempt-secret",
		},
		{
			UserId:          7,
			PlanId:          3,
			Money:           10,
			TradeNo:         "old_toss_pending_with_provider_payload",
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusPending,
			CreateTime:      1000,
			ProviderPayload: `{"status":"DONE"}`,
		},
		{
			UserId:           7,
			PlanId:           3,
			Money:            10,
			TradeNo:          "old_toss_pending_with_partial_claim",
			PaymentMethod:    PaymentMethodToss,
			PaymentProvider:  PaymentProviderToss,
			Status:           common.TopUpStatusPending,
			CreateTime:       1000,
			BillingClaimTime: 1500,
		},
		{
			UserId: 7, PlanId: 3, Money: 10, TradeNo: "opaque_subscription_order_without_tuple",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: common.TopUpStatusPending, CreateTime: 1000,
			RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
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

	expiredOrder := GetSubscriptionOrderByTradeNo("old_toss_pending")
	require.Equal(t, common.TopUpStatusExpired, expiredOrder.Status)
	require.Empty(t, expiredOrder.ProviderCredential)
	require.Empty(t, expiredOrder.ProviderClientKeyHash)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("recent_toss_pending").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_issue_snapshot").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_issue_marker").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_charge_marker").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_provider_payload").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("old_toss_pending_with_partial_claim").Status)
	require.Equal(t, common.TopUpStatusPending, GetSubscriptionOrderByTradeNo("opaque_subscription_order_without_tuple").Status)
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

func TestReconcileStaleTossPendingSubscriptionOrdersSelectsOldAttachedOrRecoverableIssuePending(t *testing.T) {
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
			UserId:                  7,
			PlanId:                  3,
			Money:                   10,
			TradeNo:                 "old_toss_pending_with_issue_snapshot",
			PaymentMethod:           PaymentMethodToss,
			PaymentProvider:         PaymentProviderToss,
			Status:                  common.TopUpStatusPending,
			CreateTime:              1000,
			BillingIssueAuthKey:     "encrypted-auth-key",
			BillingIssueAuthKeyHash: "auth-key-hash",
			BillingIssueCustomerKey: "cust_issue_snapshot",
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
	require.Equal(t, int64(2), resolved)
	require.Equal(t, []string{"old_toss_pending_with_key", "old_toss_pending_with_issue_snapshot"}, seen)
}
