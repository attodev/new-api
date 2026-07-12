package model

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func forceNextTossAttemptUpdateRowsAffectedZero(t *testing.T, table, attemptedColumn string) {
	t.Helper()
	callbackName := fmt.Sprintf("test:toss-attempt-noop:%p:%s", t, attemptedColumn)
	var forced atomic.Bool
	require.NoError(t, DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if forced.Load() || tx.Statement == nil || tx.Statement.Table != table {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		if _, ok := updates[attemptedColumn]; !ok {
			return
		}
		if forced.CompareAndSwap(false, true) {
			// Emulate MySQL's default changed-rows result for an UPDATE whose
			// guarded target already contains every requested value.
			tx.RowsAffected = 0
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Update().Remove(callbackName))
	})
}

func forceNextTossRenewalAttemptUpdateError(t *testing.T, injected error) {
	t.Helper()
	callbackName := fmt.Sprintf("test:toss-renewal-attempt-error:%p", t)
	var forced atomic.Bool
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if forced.Load() || tx.Statement == nil || tx.Statement.Table != "subscription_orders" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		if _, ok := updates["billing_attempted"]; !ok {
			return
		}
		if forced.CompareAndSwap(false, true) {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Update().Remove(callbackName))
	})
}

func forceNextTossRenewalAttemptConditionalMiss(t *testing.T) {
	t.Helper()
	callbackName := fmt.Sprintf("test:toss-renewal-attempt-miss:%p", t)
	var forced atomic.Bool
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if forced.Load() || tx.Statement == nil || tx.Statement.Table != "subscription_orders" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		if _, ok := updates["billing_attempted"]; !ok {
			return
		}
		if forced.CompareAndSwap(false, true) {
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "1 = 0"},
			}})
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Update().Remove(callbackName))
	})
}

func TestPreMarkerTossRenewalUsesExactCredentialGETBeforePOST(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_legacy_get_first", 0)
	keyCredential, err := EncryptProviderCredential(setting.TossBillingSecretKey)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyID).Updates(map[string]interface{}{
		"provider_credential":      keyCredential,
		"provider_client_key_hash": TossBillingClientKeyFingerprint(setting.TossBillingClientKey),
	}).Error)
	seedTossBillingPlan(t, "Legacy GET first", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	tradeNo = order.TradeNo
	require.False(t, order.BillingAttempted)
	require.Equal(t, tossBillingChargeProtocolDurableAttempt, order.BillingChargeProtocolVersion)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_charge_protocol_version", tossBillingChargeProtocolLegacy).Error)
	order.BillingChargeProtocolVersion = tossBillingChargeProtocolLegacy
	exactSecret, err := DecryptProviderCredential(order.ProviderCredential)
	require.NoError(t, err)
	require.NotEmpty(t, exactSecret)

	sequence := make([]string, 0, 2)
	previousLookup := tossRenewalPaymentLookup
	previousCharger := tossBillingCharger
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		sequence = append(sequence, "get")
		require.Equal(t, exactSecret, secretKey)
		require.Equal(t, tradeNo, orderID)
		return nil, ErrTossBillingPaymentNotFound
	})
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		sequence = append(sequence, "post")
		require.Equal(t, exactSecret, secretKey)
		var marked SubscriptionOrder
		require.NoError(t, DB.Where("trade_no = ?", orderID).First(&marked).Error)
		require.True(t, marked.BillingAttempted)
		require.Equal(t, marked.ProviderCredential, marked.BillingAttemptCredential)
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_legacy_get_first", ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() {
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails))
	require.Equal(t, []string{"get", "post"}, sequence)
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(order).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
}

func TestPreMarkerTossRenewalWithoutExactCredentialFailsClosed(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_legacy_missing_exact", 0)
	seedTossBillingPlan(t, "Legacy missing exact", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	tradeNo = order.TradeNo
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_charge_protocol_version": tossBillingChargeProtocolLegacy,
		"provider_credential":             "",
	}).Error)

	lookupCalls := 0
	previousLookup := tossRenewalPaymentLookup
	previousCharger := tossBillingCharger
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("legacy order without exact credential must not POST")
	})
	t.Cleanup(func() {
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingCharger(previousCharger)
	})

	err = ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.Zero(t, lookupCalls)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.False(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingClaimToken, "unsafe legacy evidence remains leased for manual reconciliation")
}

func TestPreMarkerTossRenewalDeletedSubscriptionStillGetsExactLookupAndCloses404(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_legacy_deleted_recovery", 0)
	seedTossBillingPlan(t, "Legacy deleted recovery", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	tradeNo = order.TradeNo
	exactSecret, err := DecryptProviderCredential(order.ProviderCredential)
	require.NoError(t, err)
	require.NotEmpty(t, exactSecret)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_charge_protocol_version": tossBillingChargeProtocolLegacy,
		"billing_attempted":               false,
		"billing_attempt_credential":      "",
		"billing_attempt_time":            0,
	}).Error)
	require.NoError(t, DB.Delete(&UserSubscription{}, sub.Id).Error)

	lookupCalls := 0
	previousLookup := tossRenewalPaymentLookup
	previousCharger := tossBillingCharger
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, exactSecret, secretKey)
		require.Equal(t, tradeNo, orderID)
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("deleted legacy renewal must be GET-only")
	})
	t.Cleanup(func() {
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.True(t, order.BillingAttempted)
	require.Equal(t, order.ProviderCredential, order.BillingAttemptCredential)
}

func TestPreMarkerTossRenewalCanceledSubscriptionStillFulfillsAuthoritativeDONE(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_legacy_canceled_recovery", 0)
	seedTossBillingPlan(t, "Legacy canceled recovery", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	tradeNo = order.TradeNo
	exactSecret, err := DecryptProviderCredential(order.ProviderCredential)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_charge_protocol_version": tossBillingChargeProtocolLegacy,
		"billing_attempted":               false,
		"billing_attempt_credential":      "",
		"billing_attempt_time":            0,
	}).Error)
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
		"auto_renew":        false,
		"next_billing_time": 0,
	}).Error)

	lookupCalls := 0
	previousLookup := tossRenewalPaymentLookup
	previousCharger := tossBillingCharger
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, exactSecret, secretKey)
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_legacy_canceled_recovery", ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	})
	postCalls := 0
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("canceled legacy renewal must be GET-only")
	})
	t.Cleanup(func() {
		SetTossRenewalPaymentLookup(previousLookup)
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.True(t, order.BillingAttempted)
	require.Equal(t, order.ProviderCredential, order.BillingAttemptCredential)
	var fulfilled UserSubscription
	require.NoError(t, DB.First(&fulfilled, sub.Id).Error)
	require.False(t, fulfilled.AutoRenew, "a cancellation still prevents the next cycle")
	require.Greater(t, fulfilled.EndTime, sub.EndTime, "the already-paid cycle must still be fulfilled")
}

func TestTossRenewalAttemptCredentialIsPinnedUntilExplicitRejectionPromotion(t *testing.T) {
	setupTossBillingModelTestDB(t)
	_, order, _ := prepareAttemptedTossRenewalLifecycleTest(t)
	originalCredential := order.BillingAttemptCredential
	require.NotEmpty(t, originalCredential)

	require.NoError(t, MarkTossRenewalChargeAttempt(order.TradeNo, order.BillingClaimToken, setting.TossBillingSecretKey))
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, originalCredential, order.BillingAttemptCredential, "same-secret retry must preserve the durable ciphertext")

	forceNextTossAttemptUpdateRowsAffectedZero(t, "subscription_orders", "billing_attempted")
	require.NoError(t, MarkTossRenewalChargeAttempt(order.TradeNo, order.BillingClaimToken, setting.TossBillingSecretKey),
		"MySQL changed-rows zero must be revalidated as the same claimed intent")

	err := MarkTossRenewalChargeAttempt(order.TradeNo, order.BillingClaimToken, "test_sk_unpromoted_rotation")
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, originalCredential, order.BillingAttemptCredential)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_attempt_credential", "corrupt-renewal-attempt").Error)
	err = MarkTossRenewalChargeAttempt(order.TradeNo, order.BillingClaimToken, setting.TossBillingSecretKey)
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, "corrupt-renewal-attempt", order.BillingAttemptCredential)
}

func TestProcessTossRenewalFinalGateDBErrorDoesNotCountProviderFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_final_gate_db_error", 0)
	seedTossBillingPlan(t, "Final gate DB error", 1)

	injected := errors.New("injected renewal final-gate update failure")
	forceNextTossRenewalAttemptUpdateError(t, injected)
	providerCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		providerCalls++
		return nil, errors.New("provider must not be called after the local gate fails")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorIs(t, err, injected)
	require.Zero(t, providerCalls)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.False(t, order.BillingAttempted)
	require.Empty(t, order.BillingClaimToken)
}

func TestProcessTossRenewalFinalGateClaimLossDoesNotCountProviderFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_final_gate_claim_loss", 0)
	seedTossBillingPlan(t, "Final gate claim loss", 1)
	forceNextTossRenewalAttemptConditionalMiss(t)

	providerCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		providerCalls++
		return nil, errors.New("provider must not be called after claim loss")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossBillingClaimLost)
	require.Zero(t, providerCalls)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.False(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingClaimToken)
}

func TestProcessTossRenewalInactiveKeyBeforePOSTDoesNotCountProviderFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_inactive_before_post", 0)
	seedTossBillingPlan(t, "Inactive key before POST", 1)
	var before UserSubscription
	require.NoError(t, DB.First(&before, 11).Error)
	order, err := PrepareTossRenewalOrder(
		before.Id,
		tossRenewalTradeNo(before.Id, before.NextBillingTime, before.BillingFailCount),
		1,
		TossPlanKRW(1),
	)
	require.NoError(t, err)
	require.NotNil(t, order)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyID).
		Update("status", BillingKeyStatusRevoked).Error)

	providerCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		providerCalls++
		return nil, errors.New("inactive key must stop before provider POST")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err = ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorContains(t, err, "billing key revoked")
	require.Zero(t, providerCalls)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.False(t, order.BillingAttempted)
	require.Empty(t, order.BillingClaimToken)
}

func TestProcessTossRenewalEmptyProviderResponseKeepsDurableAttempt(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_empty_provider_response", 0)
	seedTossBillingPlan(t, "Empty provider response", 1)

	providerCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		providerCalls++
		return nil, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorContains(t, err, "returned no result")
	require.Equal(t, 1, providerCalls)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.True(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingClaimToken)
}

func TestTossInitialChargeAttemptCredentialIsPinnedAcrossRecoveryValidation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_initial_pin", 0)
	seedTossBillingPlan(t, "Initial credential pin", 1)
	plan, err := GetSubscriptionPlanById(3)
	require.NoError(t, err)
	exactSecret := setting.TossBillingSecretKey
	providerCredential, err := EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	order := &SubscriptionOrder{
		UserId: 7, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo: "toss_sub_initial_credential_pin", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		CreateTime: GetDBTimestamp(), ProviderAmount: TossPlanKRW(plan.PriceAmount), ProviderCurrency: "KRW",
		ProviderCredential: providerCredential, BillingKeyId: keyID, BillingClaimToken: "claim-initial-pin",
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	require.NoError(t, DB.Create(order).Error)

	require.NoError(t, ValidateClaimedTossSubscriptionFirstCharge(order.TradeNo, order.BillingClaimToken, keyID, exactSecret))
	require.NoError(t, DB.First(order, order.Id).Error)
	originalAttemptCredential := order.BillingAttemptCredential
	require.NotEmpty(t, originalAttemptCredential)

	forceNextTossAttemptUpdateRowsAffectedZero(t, "subscription_orders", "billing_attempted")
	require.NoError(t, ValidateClaimedTossSubscriptionFirstCharge(order.TradeNo, order.BillingClaimToken, keyID, exactSecret))
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, originalAttemptCredential, order.BillingAttemptCredential)

	err = ValidateClaimedTossSubscriptionFirstCharge(order.TradeNo, order.BillingClaimToken, keyID, "test_sk_unpromoted_initial_rotation")
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, originalAttemptCredential, order.BillingAttemptCredential)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_attempt_credential", "corrupt-initial-attempt").Error)
	err = ValidateClaimedTossSubscriptionFirstCharge(order.TradeNo, order.BillingClaimToken, keyID, exactSecret)
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, "corrupt-initial-attempt", order.BillingAttemptCredential)
}

func TestWalletAutoRechargeAttemptCredentialIsPinnedUntilExplicitRejectionPromotion(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "wallet-pin-owner", Status: common.UserStatusEnabled, Group: "default", AffCode: "wallet-pin-owner",
	}).Error)
	encryptedBillingKey, err := common.EncryptString("wallet-pin-billing-key")
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 91, UserId: 1, CustomerKey: "wallet-pin-customer",
		EncryptedKey: encryptedBillingKey, Status: BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: key.Id, CustomerKey: key.CustomerKey,
		ProviderClientKeyHash: key.ProviderClientKeyHash, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		NextChargeTime: now.Unix(), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, now, 3)
	require.NoError(t, err)
	require.True(t, prepared.shouldCharge)
	require.NotEmpty(t, prepared.tradeNo)

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", prepared.tradeNo).First(&topUp).Error)
	originalCredential := topUp.ProviderCredential
	require.NotEmpty(t, originalCredential)

	require.NoError(t, claimWalletAutoRechargeProviderPOSTCredential(policy.Id, prepared.tradeNo, "wallet_auto_test_sk"))
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.True(t, topUp.ProviderAttempted)
	require.Equal(t, originalCredential, topUp.ProviderCredential)

	forceNextTossAttemptUpdateRowsAffectedZero(t, "top_ups", "provider_attempted")
	require.NoError(t, claimWalletAutoRechargeProviderPOSTCredential(policy.Id, prepared.tradeNo, "wallet_auto_test_sk"))
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, originalCredential, topUp.ProviderCredential)

	err = claimWalletAutoRechargeProviderPOSTCredential(policy.Id, prepared.tradeNo, "wallet_auto_test_sk_unpromoted")
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, originalCredential, topUp.ProviderCredential)

	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).
		Update("provider_credential", "corrupt-wallet-attempt").Error)
	err = claimWalletAutoRechargeProviderPOSTCredential(policy.Id, prepared.tradeNo, "wallet_auto_test_sk")
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, "corrupt-wallet-attempt", topUp.ProviderCredential)

	// A pending attempt without its exact provider credential could have been
	// posted by a pre-marker binary. Recovery must stop before both GET and POST
	// instead of borrowing the active billing-key credential.
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("provider_credential", "").Error)
	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("missing exact credential must not POST")
		})
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.Zero(t, lookupCalls)
	require.Zero(t, postCalls)
}
