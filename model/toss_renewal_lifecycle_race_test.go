package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareAttemptedTossRenewalLifecycleTest(t *testing.T) (*UserSubscription, *SubscriptionOrder, int) {
	t.Helper()
	keyID := seedTossBillingSubscription(t, "billing_key_lifecycle_race", 0)
	seedTossBillingPlan(t, "Lifecycle Race", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, 1000)
	require.NoError(t, err)
	require.NotNil(t, order)
	tradeNo = order.TradeNo
	require.Equal(t, sub.EndTime, order.RenewalEndTime)

	claimToken, claimed, attempted, err := ClaimTossRenewalChargeOrder(tradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.False(t, attempted)
	require.NoError(t, MarkTossRenewalChargeAttempt(tradeNo, claimToken, setting.TossBillingSecretKey))
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", tradeNo).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(order).Error)
	return &sub, order, keyID
}

func installTossRenewalLifecycleLookup(t *testing.T, order *SubscriptionOrder) (*int, *int) {
	t.Helper()
	lookupCalls := 0
	postCalls := 0
	previousLookup := tossRenewalPaymentLookup
	previousCharger := tossBillingCharger
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, setting.TossBillingSecretKey, secretKey)
		require.Equal(t, order.TradeNo, orderID)
		require.Equal(t, order.ProviderAmount, amount)
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_" + orderID,
			ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("recovery-only lifecycle path must not POST")
	})
	t.Cleanup(func() {
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingCharger(previousCharger)
	})
	return &lookupCalls, &postCalls
}

func TestProcessTossRenewalSettlesAttemptedDONEAfterAutoRenewCancellation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_cancel_after_intent", 0)
	seedTossBillingPlan(t, "Cancel After Intent", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	var before UserSubscription
	require.NoError(t, DB.First(&before, 11).Error)
	plan, err := GetSubscriptionPlanById(before.PlanId)
	require.NoError(t, err)
	expectedEnd, err := calcPlanEndTime(time.Unix(before.EndTime, 0), plan)
	require.NoError(t, err)

	previousCharger := tossBillingCharger
	chargeCalls := 0
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		var attempted SubscriptionOrder
		require.NoError(t, DB.Where("trade_no = ?", orderID).First(&attempted).Error)
		require.True(t, attempted.BillingAttempted)
		require.Equal(t, before.EndTime, attempted.RenewalEndTime)
		require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&UserSubscription{}).Where("id = ?", before.Id).Updates(map[string]interface{}{
				"auto_renew":         false,
				"next_billing_time":  0,
				"billing_retry_time": 0,
			}).Error; err != nil {
				return err
			}
			return MarkTossBillingKeyPendingRevocation(tx, keyID)
		}))
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_cancel_after_intent",
			ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails))
	require.Equal(t, 1, chargeCalls)
	var after UserSubscription
	require.NoError(t, DB.First(&after, before.Id).Error)
	require.Equal(t, expectedEnd, after.EndTime)
	require.False(t, after.AutoRenew)
	require.Zero(t, after.NextBillingTime)
	require.Zero(t, after.BillingRetryTime)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	order := requireTossRenewalOrderByAttemptForTest(t, before.Id, 0)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)

	// The paid period was granted once; the preserved cancellation prevents a
	// fresh provider request on later scheduler runs.
	require.NoError(t, ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails))
	require.Equal(t, 1, chargeCalls)
	require.NoError(t, DB.First(&after, before.Id).Error)
	require.Equal(t, expectedEnd, after.EndTime)
}

func TestProcessTossRenewalRecoverySettlesDisabledUserDONEWithoutPOST(t *testing.T) {
	setupTossBillingModelTestDB(t)
	before, order, keyID := prepareAttemptedTossRenewalLifecycleTest(t)
	plan, err := ResolveTossSubscriptionOrderPlan(order)
	require.NoError(t, err)
	expectedEnd, err := calcPlanEndTime(time.Unix(before.EndTime, 0), plan)
	require.NoError(t, err)
	require.NoError(t, UpdateUserFieldsWithBillingLifecycle(before.UserId, map[string]interface{}{
		"status": common.UserStatusDisabled,
	}))
	lookupCalls, postCalls := installTossRenewalLifecycleLookup(t, order)

	require.NoError(t, ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails))
	require.Equal(t, 1, *lookupCalls)
	require.Zero(t, *postCalls)
	var after UserSubscription
	require.NoError(t, DB.First(&after, before.Id).Error)
	require.Equal(t, expectedEnd, after.EndTime)
	require.False(t, after.AutoRenew)
	require.Zero(t, after.NextBillingTime)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var persistedOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&persistedOrder).Error)
	require.Equal(t, common.TopUpStatusSuccess, persistedOrder.Status)
}

func TestProcessTossRenewalRecoveryPersistsRefundRequiredForLostEntitlement(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, sub *UserSubscription, keyID int)
	}{
		{
			name: "cancelled",
			mutate: func(t *testing.T, sub *UserSubscription, keyID int) {
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
						"status":             "cancelled",
						"auto_renew":         false,
						"next_billing_time":  0,
						"billing_retry_time": 0,
					}).Error; err != nil {
						return err
					}
					return MarkTossBillingKeyPendingRevocation(tx, keyID)
				}))
			},
		},
		{
			name: "expired",
			mutate: func(t *testing.T, sub *UserSubscription, keyID int) {
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
					if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
						"status":             "expired",
						"auto_renew":         false,
						"next_billing_time":  0,
						"billing_retry_time": 0,
					}).Error; err != nil {
						return err
					}
					return MarkTossBillingKeyPendingRevocation(tx, keyID)
				}))
			},
		},
		{
			name: "deleted",
			mutate: func(t *testing.T, sub *UserSubscription, keyID int) {
				require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
					if err := MarkTossBillingKeyPendingRevocation(tx, keyID); err != nil {
						return err
					}
					return tx.Delete(&UserSubscription{}, sub.Id).Error
				}))
			},
		},
		{
			name: "cycle changed",
			mutate: func(t *testing.T, sub *UserSubscription, keyID int) {
				require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
					"end_time":           sub.EndTime + 3600,
					"auto_renew":         false,
					"next_billing_time":  0,
					"billing_retry_time": 0,
				}).Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			before, order, keyID := prepareAttemptedTossRenewalLifecycleTest(t)
			tt.mutate(t, before, keyID)
			lookupCalls, postCalls := installTossRenewalLifecycleLookup(t, order)

			err := ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails)
			require.Error(t, err)
			require.Equal(t, 1, *lookupCalls)
			require.Zero(t, *postCalls)
			var persistedOrder SubscriptionOrder
			require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&persistedOrder).Error)
			if tt.name == "cycle changed" {
				require.Equal(t, common.TopUpStatusFailed, persistedOrder.Status)
				require.Empty(t, persistedOrder.BillingClaimToken)
			} else {
				require.Equal(t, common.TopUpStatusPending, persistedOrder.Status)
				require.True(t, persistedOrder.BillingAttempted)
				require.NotEmpty(t, persistedOrder.BillingClaimToken)
			}
			var event TossPaymentEvent
			require.NoError(t, DB.Where("order_id = ? AND event_type = ?", order.TradeNo, TossPaymentEventTypeFulfillment).First(&event).Error)
			require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)
			require.Contains(t, event.ResolutionNote, "refund required")

			// A live reconciliation claim plus the lifecycle state prevents another
			// provider POST while an operator resolves entitlement versus refund.
			require.NoError(t, ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails))
			require.Equal(t, 1, *lookupCalls)
			require.Zero(t, *postCalls)
			if tt.name == "deleted" {
				var deleted UserSubscription
				require.ErrorIs(t, DB.First(&deleted, before.Id).Error, gorm.ErrRecordNotFound)
			}
		})
	}
}

func TestProcessTossRenewalStaleCandidateDoesNotChargeFutureCycle(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_stale_candidate", 0)
	seedTossBillingPlan(t, "Stale Candidate", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	chargeCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_stale_candidate",
			ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	// The first worker settles the due cycle and advances NextBillingTime.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, chargeCalls)
	var renewed UserSubscription
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.Greater(t, renewed.NextBillingTime, GetDBTimestamp())

	// A stale scheduler candidate for the same subscription ID must re-read the
	// future due time and exit before creating or posting the next cycle.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, chargeCalls)
	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("renewal_subscription_id = ?", 11).
		Count(&orderCount).Error)
	require.Equal(t, int64(1), orderCount)
}

func TestOverdueTossRenewalStartsFullPeriodAtSettlement(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	seedTossBillingSubscription(t, "billing_key_overdue_short_period", 0)
	plan := &SubscriptionPlan{
		Id:            3,
		Title:         "Overdue Short Period",
		PriceAmount:   1,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationDay,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	seedTossRenewalContractForTest(t, 11, plan, TossPlanKRW(plan.PriceAmount))
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          now - 23*60*60,
		"next_billing_time": now - 23*60*60,
	}).Error)

	chargeCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_" + orderID,
			ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	settlementStarted := GetDBTimestamp()
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	settlementFinished := GetDBTimestamp()
	require.Equal(t, 1, chargeCalls)

	var renewed UserSubscription
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.GreaterOrEqual(t, renewed.EndTime, settlementStarted+int64((24*time.Hour)/time.Second))
	require.LessOrEqual(t, renewed.EndTime, settlementFinished+int64((24*time.Hour)/time.Second))
	require.Greater(t, renewed.NextBillingTime, settlementFinished)

	// A second scheduler snapshot must not charge another period merely to catch
	// up with the old wall-clock expiry.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, chargeCalls)
}

func TestTossSubscriptionBillingRejectsSubHourlyCadence(t *testing.T) {
	shortPlan := &SubscriptionPlan{
		Id:            501,
		DurationUnit:  SubscriptionDurationCustom,
		CustomSeconds: TossSubscriptionMinimumBillingPeriodSeconds - 1,
	}
	require.ErrorIs(t, ValidateTossSubscriptionBillingPlan(shortPlan), ErrTossSubscriptionBillingPeriodTooShort)
	require.ErrorIs(t, CreateTossSubscriptionOrderWithPurchaseReservation(&SubscriptionOrder{
		UserId:          1,
		PlanId:          shortPlan.Id,
		TradeNo:         "short_toss_subscription",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}, shortPlan), ErrTossSubscriptionBillingPeriodTooShort)
	require.NoError(t, ValidateTossSubscriptionBillingPlan(&SubscriptionPlan{
		DurationUnit:  SubscriptionDurationHour,
		DurationValue: 1,
	}))
}

func TestStaleTossRenewalIsExcludedAndFinalGateRejectsPOST(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_outside_grace", 0)
	seedTossBillingPlan(t, "Outside Grace", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	now := GetDBTimestamp()
	staleEnd := now - TossBillingOperationalGraceSeconds - 1
	staleBillingTime := now - TossBillingOperationalGraceSeconds - 3600
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          staleEnd,
		"next_billing_time": staleBillingTime,
	}).Error)

	due, err := GetDueTossRenewals(now, 10)
	require.NoError(t, err)
	require.Empty(t, due)

	tradeNo := tossRenewalTradeNo(11, staleBillingTime, 0)
	order, err := PrepareTossRenewalOrder(11, tradeNo, 1, 1000)
	require.NoError(t, err)
	require.NotNil(t, order)
	tradeNo = order.TradeNo
	token, claimed, attempted, err := ClaimTossRenewalChargeOrder(tradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.False(t, attempted)
	require.ErrorIs(t, MarkTossRenewalChargeAttempt(tradeNo, token, setting.TossBillingSecretKey), ErrTossBillingRenewalOutsideGrace)

	var persisted SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&persisted).Error)
	require.False(t, persisted.BillingAttempted)
}

func TestRenewalExpiryDefersRecentCurrentCycleAttemptForGETOnlyDONE(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_expiry_recovery", 0)
	seedTossBillingPlan(t, "Expiry Recovery", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          now - TossBillingOperationalGraceSeconds + 1,
		"next_billing_time": now,
	}).Error)
	previousCharger := tossBillingCharger
	previousLookup := tossRenewalPaymentLookup
	postCalls := 0
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
		SetTossRenewalPaymentLookup(previousLookup)
	})
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, postCalls)

	var attempted SubscriptionOrder
	require.NoError(t, DB.Where("payment_provider = ? AND billing_attempted = ?", PaymentProviderToss, true).First(&attempted).Error)
	require.Equal(t, common.TopUpStatusPending, attempted.Status)
	require.Equal(t, now-TossBillingOperationalGraceSeconds+1, attempted.RenewalEndTime)
	require.Greater(t, attempted.BillingAttemptTime, int64(0))
	// Cross the expiry boundary while the provider-call claim is still within
	// its bounded recovery window.
	time.Sleep(2 * time.Second)
	expired, err := ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(10, TossBillingOperationalGraceSeconds, false)
	require.NoError(t, err)
	require.Zero(t, expired)
	var protected UserSubscription
	require.NoError(t, DB.First(&protected, 11).Error)
	require.Equal(t, "active", protected.Status)
	require.True(t, protected.AutoRenew)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", attempted.Id).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	lookupCalls := 0
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, attempted.TradeNo, orderID)
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_expiry_recovery_done", ProviderPayload: `{"status":"DONE"}`,
		}, nil
	})
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("outside-grace recovery must never POST")
	})
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)
	var renewed UserSubscription
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.Equal(t, "active", renewed.Status)
	require.Greater(t, renewed.EndTime, GetDBTimestamp())
	var settled SubscriptionOrder
	require.NoError(t, DB.First(&settled, attempted.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, settled.Status)

	// The settled cycle is idempotent and cannot extend entitlement twice.
	settledEnd := renewed.EndTime
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.Equal(t, settledEnd, renewed.EndTime)
}

func TestNormalExpiryDefersCanceledRecentTossRenewalUntilGETRecovery(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_normal_expiry_recovery", 0)
	seedTossBillingPlan(t, "Normal Expiry Recovery", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	now := GetDBTimestamp()
	expiredEnd := now - 1
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          expiredEnd,
		"next_billing_time": now - 1,
	}).Error)

	previousCharger := tossBillingCharger
	previousLookup := tossRenewalPaymentLookup
	previousRevoker := tossBillingRevoker
	postCalls := 0
	lookupCalls := 0
	var providerPayment *TossBillingChargeResult
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		providerPayment = &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_normal_expiry_recovery", ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}
		// The provider accepted the payment, but the response was lost.
		return nil, context.DeadlineExceeded
	})
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		return errors.New("temporary Toss revocation outage")
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingRevoker(previousRevoker)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, postCalls)
	require.NotNil(t, providerPayment)
	var attempted SubscriptionOrder
	require.NoError(t, DB.Where("payment_provider = ? AND billing_attempted = ?", PaymentProviderToss, true).First(&attempted).Error)
	require.Equal(t, common.TopUpStatusPending, attempted.Status)
	require.Equal(t, expiredEnd, attempted.RenewalEndTime)
	require.Greater(t, attempted.BillingAttemptTime, int64(0))

	require.NoError(t, CancelTossAutoRenewForUser(context.Background(), 7))
	var cancelled UserSubscription
	require.NoError(t, DB.First(&cancelled, 11).Error)
	require.False(t, cancelled.AutoRenew)
	require.Zero(t, cancelled.NextBillingTime)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	// The ambiguous provider POST is still a live recovery reference. Deleting
	// its key before the bounded GET-only recovery would make the paid outcome
	// unverifiable; the key is queued only after that exact attempt settles.
	require.Equal(t, BillingKeyStatusActive, key.Status)

	// The normal sweep owns auto-renew-disabled subscriptions, but must leave
	// this exact paid-or-unknown cycle active for bounded GET-only recovery.
	expired, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	require.Zero(t, expired)
	var protected UserSubscription
	require.NoError(t, DB.First(&protected, 11).Error)
	require.Equal(t, "active", protected.Status)
	require.Equal(t, expiredEnd, protected.EndTime)
	require.False(t, protected.AutoRenew)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", attempted.Id).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, attempted.TradeNo, orderID)
		require.Equal(t, providerPayment.Total, amount)
		return providerPayment, nil
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)
	var renewed UserSubscription
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.Equal(t, "active", renewed.Status)
	require.Greater(t, renewed.EndTime, expiredEnd)
	require.False(t, renewed.AutoRenew)
	require.Zero(t, renewed.NextBillingTime)
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var settled SubscriptionOrder
	require.NoError(t, DB.First(&settled, attempted.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, settled.Status)

	// The paid period is granted exactly once and cancellation still prevents
	// any later provider POST.
	settledEnd := renewed.EndTime
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)
	require.NoError(t, DB.First(&renewed, 11).Error)
	require.Equal(t, settledEnd, renewed.EndTime)
}

func TestNormalExpiryDoesNotDeferIneligibleTossRenewal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, keyID int, endTime, now int64)
	}{
		{
			name: "bounded old attempt with refreshed claim",
			setup: func(t *testing.T, keyID int, endTime, now int64) {
				attemptTime := now - tossRenewalExpiryRecoveryDeferralSeconds - 1
				require.NoError(t, DB.Create(&SubscriptionOrder{
					UserId: 7, PlanId: 3, TradeNo: tossRenewalTradeNo(11, now-10, 0),
					PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
					Status: common.TopUpStatusPending, BillingKeyId: keyID,
					BillingAttempted: true, BillingAttemptTime: attemptTime,
					BillingClaimTime: now, RenewalEndTime: endTime, CreateTime: attemptTime,
				}).Error)
			},
		},
		{
			name: "recent terminal failed order",
			setup: func(t *testing.T, keyID int, endTime, now int64) {
				require.NoError(t, DB.Create(&SubscriptionOrder{
					UserId: 7, PlanId: 3, TradeNo: tossRenewalTradeNo(11, now-10, 0),
					PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
					Status: common.TopUpStatusFailed, BillingKeyId: keyID,
					BillingAttempted: true, BillingAttemptTime: now,
					RenewalEndTime: endTime, CreateTime: now,
					ProviderPayload: `{"status":"CANCELED"}`,
				}).Error)
			},
		},
		{
			name: "recent unrelated cycle",
			setup: func(t *testing.T, keyID int, endTime, now int64) {
				require.NoError(t, DB.Create(&SubscriptionOrder{
					UserId: 7, PlanId: 3, TradeNo: tossRenewalTradeNo(11, now-10, 0),
					PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
					Status: common.TopUpStatusPending, BillingKeyId: keyID,
					BillingAttempted: true, BillingAttemptTime: now,
					RenewalEndTime: endTime - 1, CreateTime: now,
				}).Error)
			},
		},
		{
			name: "manual subscription",
			setup: func(t *testing.T, keyID int, endTime, now int64) {
				require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).
					Update("billing_key_id", 0).Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			keyID := seedTossBillingSubscription(t, "billing_key_normal_expiry_ineligible", 0)
			seedTossBillingPlan(t, "Normal Expiry Ineligible", 1)
			now := GetDBTimestamp()
			endTime := now - 1
			require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
				"auto_renew": false,
				"end_time":   endTime,
			}).Error)
			tt.setup(t, keyID, endTime, now)

			expired, err := ExpireDueSubscriptions(10)
			require.NoError(t, err)
			require.Equal(t, 1, expired)
			var sub UserSubscription
			require.NoError(t, DB.First(&sub, 11).Error)
			require.Equal(t, "expired", sub.Status)
		})
	}
}

func TestRenewalExpiryDeferralIsBoundedAndIgnoresUnrelatedOrders(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_expiry_bounded", 0)
	seedTossBillingPlan(t, "Expiry Bounded", 1)
	now := GetDBTimestamp()
	staleEnd := now - TossBillingOperationalGraceSeconds - 1
	staleBillingTime := staleEnd - 3600
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          staleEnd,
		"next_billing_time": staleBillingTime,
	}).Error)
	tradeNo := tossRenewalTradeNo(11, staleBillingTime, 0)
	legacyAttemptTime := now - tossRenewalExpiryRecoveryDeferralSeconds - 1
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 7, PlanId: 3, TradeNo: tradeNo, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		BillingKeyId: keyID, BillingAttempted: true, RenewalEndTime: staleEnd,
		CreateTime:       legacyAttemptTime,
		BillingClaimTime: now,
	}).Error)
	// A recent order for a different cycle must not extend the old entitlement.
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 7, PlanId: 3, TradeNo: tossRenewalTradeNo(11, staleBillingTime-1, 1), PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		BillingKeyId: keyID, BillingAttempted: true, RenewalEndTime: staleEnd - 100,
		CreateTime: now, BillingClaimTime: now,
	}).Error)

	expired, err := ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(10, TossBillingOperationalGraceSeconds, false)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)
	var legacyAttempt SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&legacyAttempt).Error)
	require.Equal(t, legacyAttemptTime, legacyAttempt.BillingAttemptTime)

	// Terminal and deleted subscriptions are outside the active-only expiry
	// selection and are never recreated by an attempted renewal row.
	cancelled := UserSubscription{
		Id: 12, UserId: 7, PlanId: 3, Status: "cancelled", EndTime: staleEnd,
		AutoRenew: true, BillingKeyId: keyID, NextBillingTime: staleBillingTime,
	}
	require.NoError(t, DB.Create(&cancelled).Error)
	deleted := UserSubscription{
		Id: 13, UserId: 7, PlanId: 3, Status: "active", EndTime: staleEnd,
		AutoRenew: true, BillingKeyId: keyID, NextBillingTime: staleBillingTime,
	}
	require.NoError(t, DB.Create(&deleted).Error)
	require.NoError(t, DB.Delete(&deleted).Error)
	_, err = ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(10, TossBillingOperationalGraceSeconds, false)
	require.NoError(t, err)
	var stillCancelled UserSubscription
	require.NoError(t, DB.First(&stillCancelled, cancelled.Id).Error)
	require.Equal(t, "cancelled", stillCancelled.Status)
	var deletedCount int64
	require.NoError(t, DB.Unscoped().Model(&UserSubscription{}).Where("id = ?", deleted.Id).Count(&deletedCount).Error)
	require.Zero(t, deletedCount)
}

func TestRenewalExpiryGlobalCorruptionRollsBackHealthySubscription(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_expiry_global_corruption", 0)
	seedTossBillingPlan(t, "Expiry global corruption", 1)
	now := GetDBTimestamp()
	staleEnd := now - TossBillingOperationalGraceSeconds - 1
	staleBillingTime := staleEnd - 3600
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time":          staleEnd,
		"next_billing_time": staleBillingTime,
	}).Error)
	unrelatedSubID := 999
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 99, PlanId: 99, TradeNo: "opaqueExpiryGlobalCorruption",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, BillingAttempted: true,
		RenewalSubscriptionId: &unrelatedSubID,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}).Error)

	expired, err := ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(10, TossBillingOperationalGraceSeconds, false)
	require.Zero(t, expired)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, "active", sub.Status)
	require.True(t, sub.AutoRenew)
	require.Equal(t, staleBillingTime, sub.NextBillingTime)
	require.Zero(t, sub.BillingFailCount)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestDurableCancellationEventStopsNextTossRenewalBeforePOST(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_cancel_event_gap", 0)
	seedTossBillingPlan(t, "Cancellation Gap", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 7,
		PlanId: 3,
		Money:  1,
		// seedTossBillingPlan stores this exact initial-order provenance in the
		// subscription's immutable renewal contract. Only cancellation of that
		// order (or this subscription's own renewal order) may stop renewal.
		TradeNo:          "toss_sub_seed_contract_11",
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusSuccess,
		BillingKeyId:     keyID,
		ProviderAmount:   1000,
		ProviderCurrency: "KRW",
	}).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "cancel_event_before_subscription_stop",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              "toss_sub_seed_contract_11",
		PaymentKey:           "pay_prior_canceled",
		Status:               "CANCELED",
		OriginalAmount:       1000,
		CancelAmount:         1000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	postCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("durable cancellation must block renewal POST")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Zero(t, postCalls)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)
	require.Zero(t, sub.NextBillingTime)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	renewal := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusExpired, renewal.Status)
	require.False(t, renewal.BillingAttempted)
}

func TestWalletCancellationEventDoesNotStopSubscriptionSharingBillingKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_shared_wallet_cancel", 0)
	seedTossBillingPlan(t, "Shared Cancellation", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 7, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Id:             71,
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       7,
		OwnerUserId:    7,
		BillingKeyId:   keyID,
		Status:         WalletAutoRechargeStatusActive,
		ActiveKey:      &activeKey,
		LastTradeNo:    "wallet_auto_71_later_success",
		NextChargeTime: GetDBTimestamp() + 3600,
	}
	require.NoError(t, DB.Create(&policy).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "older_wallet_cancel_for_shared_key",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              "wallet_auto_71_older_charge",
		PaymentKey:           "pay_wallet_older_canceled",
		Status:               "CANCELED",
		OriginalAmount:       1000,
		CancelAmount:         1000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	postCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("subscription charge attempted independently")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Equal(t, 1, postCalls)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.True(t, sub.AutoRenew)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}
