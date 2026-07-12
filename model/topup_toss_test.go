package model

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 크레딧 계산식이 설계와 일치하는지 검증: quota = Money * QuotaPerUnit
func TestTossQuotaFormula(t *testing.T) {
	prev := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	defer func() { common.QuotaPerUnit = prev }()

	money := 10.0 // 13000 KRW / 1300 = 10 USD-equiv
	quota := int(decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if quota != 5000000 {
		t.Fatalf("quota: got %d want 5000000", quota)
	}
}

func TestCreditedQuotaForTossTopUpPreservesLegacyMoneySnapshotAcrossUnitPriceChanges(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	previousUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		setting.TossUnitPrice = previousUnitPrice
	})

	common.QuotaPerUnit = 100
	legacyTopUp := &TopUp{Amount: 13000, Money: 10}

	setting.TossUnitPrice = 1000
	require.Equal(t, 1000, CreditedQuotaForTossTopUp(legacyTopUp),
		"a lower current unit price must not over-credit the immutable legacy quote")
	require.Equal(t, 1300, TossCreditQuotaFromKRW(legacyTopUp.Amount),
		"test setup must demonstrate the current-price over-credit")

	setting.TossUnitPrice = 2000
	require.Equal(t, 1000, CreditedQuotaForTossTopUp(legacyTopUp),
		"a higher current unit price must not under-credit the immutable legacy quote")
	require.Equal(t, 650, TossCreditQuotaFromKRW(legacyTopUp.Amount),
		"test setup must demonstrate the current-price under-credit")
}

func TestCreditedQuotaForTossTopUpSnapshotPrecedenceAndRecoveryFallback(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	previousUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		setting.TossUnitPrice = previousUnitPrice
	})

	common.QuotaPerUnit = 100
	setting.TossUnitPrice = 1000

	require.Equal(t, 777, CreditedQuotaForTossTopUp(&TopUp{
		Amount: 13000,
		Money:  math.Inf(1),
		Quota:  777,
	}), "stored Quota must remain the highest-priority immutable snapshot")
	require.Equal(t, 1300, CreditedQuotaForTossTopUp(&TopUp{
		Amount: 13000,
		Money:  0,
	}), "rows with neither Quota nor Money may use the recovery fallback")
}

func TestCreditedQuotaForTossTopUpInvalidQuotaSnapshotFailsClosed(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	previousUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		setting.TossUnitPrice = previousUnitPrice
	})

	common.QuotaPerUnit = 100
	setting.TossUnitPrice = 1000
	for _, test := range []struct {
		name  string
		quota int
	}{
		{name: "negative", quota: -1},
		{name: "portable-sql-int-overflow", quota: int(math.MaxInt32) + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Zero(t, CreditedQuotaForTossTopUp(&TopUp{
				Quota:  test.quota,
				Money:  10,
				Amount: 13000,
			}), "a non-zero invalid Quota snapshot must not fall back to valid legacy/current-price fields")
		})
	}
	require.Equal(t, math.MaxInt32, CreditedQuotaForTossTopUp(&TopUp{
		Quota:  math.MaxInt32,
		Money:  10,
		Amount: 13000,
	}), "the portable SQL INT ceiling remains a valid immutable snapshot")
}

func TestCreditedQuotaForTossTopUpInvalidLegacyMoneyFailsClosed(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	previousUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		setting.TossUnitPrice = previousUnitPrice
	})

	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 100
	tests := []struct {
		name  string
		money float64
	}{
		{name: "negative", money: -1},
		{name: "not-a-number", money: math.NaN()},
		{name: "positive-infinity", money: math.Inf(1)},
		{name: "below-one-quota", money: 0.001},
		{name: "quota-column-overflow", money: float64(math.MaxInt32)/common.QuotaPerUnit + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Zero(t, CreditedQuotaForTossTopUp(&TopUp{
				Amount: 13000,
				Money:  test.money,
			}), "an invalid legacy snapshot must never fall through to current-price recovery")
		})
	}
}

func TestCreditedQuotaForTossTopUpLegacyMoneyUsesExistingTruncationRule(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })

	common.QuotaPerUnit = 10
	require.Equal(t, 19, CreditedQuotaForTossTopUp(&TopUp{
		Amount: 13000,
		Money:  1.999,
	}))
}

func TestTossPaymentConstants(t *testing.T) {
	if PaymentMethodToss != "toss" {
		t.Fatalf("PaymentMethodToss = %q want toss", PaymentMethodToss)
	}
	if PaymentProviderToss != "toss" {
		t.Fatalf("PaymentProviderToss = %q want toss", PaymentProviderToss)
	}
}

func TestGenerateTossCustomerKey(t *testing.T) {
	pattern := regexp.MustCompile(`^cust_[A-Za-z0-9]{32}$`)
	for i := 0; i < 20; i++ {
		key := generateTossCustomerKey()
		if !strings.HasPrefix(key, "cust_") {
			t.Fatalf("key %q does not start with cust_", key)
		}
		if len(key) < 2 || len(key) > 50 {
			t.Fatalf("key %q length %d is outside Toss SDK v2 allowed range 2..50", key, len(key))
		}
		if !pattern.MatchString(key) {
			t.Fatalf("key %q does not match ^cust_[A-Za-z0-9]{32}$", key)
		}
	}
}

func TestClaimPendingTossTopUpConfirmRetryIsExactAndCrossProcessExclusive(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}))
	require.NoError(t, DB.Create(&User{Id: 7, Username: "toss-claim-user", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          7,
		Amount:          13000,
		TradeNo:         "toss_confirm_retry_claim",
		ProviderOrderId: "pay_confirm_retry_claim",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	token, claimedTopUp, claimed, err := ClaimPendingTossTopUpConfirmRetry(
		"toss_confirm_retry_claim",
		"pay_confirm_retry_claim",
		13000,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, token)
	require.Equal(t, "pay_confirm_retry_claim", claimedTopUp.ProviderOrderId)

	otherToken, current, otherClaimed, err := ClaimPendingTossTopUpConfirmRetry(
		"toss_confirm_retry_claim",
		"pay_confirm_retry_claim",
		13000,
	)
	require.NoError(t, err)
	require.False(t, otherClaimed)
	require.Empty(t, otherToken)
	require.Equal(t, token, current.ProviderClaimToken)

	// A process can die after the durable pre-POST marker and before the HTTP
	// request reaches Toss. The short lease must become reclaimable while the
	// provider's ten-minute confirmation window is still open.
	require.NoError(t, DB.Model(&TopUp{}).
		Where("trade_no = ?", "toss_confirm_retry_claim").
		Update("provider_claim_time", GetDBTimestamp()-tossTopUpConfirmClaimTTLSeconds-1).Error)
	reclaimedToken, _, reclaimed, err := ClaimPendingTossTopUpConfirmRetry(
		"toss_confirm_retry_claim",
		"pay_confirm_retry_claim",
		13000,
	)
	require.NoError(t, err)
	require.True(t, reclaimed)
	require.NotEmpty(t, reclaimedToken)
	require.NotEqual(t, token, reclaimedToken)
	token = reclaimedToken

	_, err = ValidatePendingTossTopUpConfirmClaim(
		"toss_confirm_retry_claim",
		"pay_different",
		13000,
		token,
	)
	require.ErrorIs(t, err, ErrTossTopUpConfirmClaimLost)
	require.NoError(t, UpdateClaimedPendingTossTopUpConfirmCredential(
		"toss_confirm_retry_claim",
		"pay_confirm_retry_claim",
		13000,
		token,
		"sk_exact_attempt",
	))
	updated, err := GetTopUpByTradeNoWithError("toss_confirm_retry_claim")
	require.NoError(t, err)
	attemptSecret, err := DecryptProviderCredential(updated.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "sk_exact_attempt", attemptSecret)

	require.NoError(t, ReleasePendingTossTopUpConfirmRetry("toss_confirm_retry_claim", token))
	replacementToken, _, replacementClaimed, err := ClaimPendingTossTopUpConfirmRetry(
		"toss_confirm_retry_claim",
		"pay_confirm_retry_claim",
		13000,
	)
	require.NoError(t, err)
	require.True(t, replacementClaimed)
	require.NotEmpty(t, replacementToken)
	require.NotEqual(t, token, replacementToken)
}

func TestPrepareClaimedTossTopUpPersistsPaymentKeyScopedIdempotency(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}))
	require.NoError(t, DB.Create(&User{Id: 41, Username: "scoped-idempotency", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo    = "toss_scoped_idempotency"
		paymentKey = "pay_scoped_idempotency"
		amount     = int64(13000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 41, TargetType: TopUpTargetTypeUser, TargetId: 41,
		Amount: amount, TradeNo: tradeNo, ProviderOrderId: paymentKey,
		ProviderConfirmProtocolVersion: tossTopUpConfirmProtocolPaymentKey,
		PaymentMethod:                  PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)

	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, claimed)
	idempotencyKey, err := PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, token, "sk_scoped_attempt")
	require.NoError(t, err)
	require.NotEqual(t, tradeNo, idempotencyKey)
	require.Equal(t, tossPaymentKeyScopedConfirmIdempotencyKey(tradeNo, paymentKey), idempotencyKey)

	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.True(t, stored.ProviderAttempted)
	require.Equal(t, idempotencyKey, stored.ProviderIdempotencyKey)
	require.NoError(t, ReleasePendingTossTopUpConfirmRetry(tradeNo, token))

	// Once persisted, the row marker wins even if one upgraded node has a stale
	// rollout environment. This is the new-code half of the rolling bridge.
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	retryToken, _, retried, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, retried)
	retryKey, err := PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, retryToken, "sk_scoped_attempt")
	require.NoError(t, err)
	require.Equal(t, idempotencyKey, retryKey)
}

func TestPrepareClaimedTossTopUpKeepsLegacyAttemptNamespace(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}))
	require.NoError(t, DB.Create(&User{Id: 42, Username: "legacy-idempotency", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo    = "toss_legacy_idempotency"
		paymentKey = "pay_legacy_idempotency"
		amount     = int64(13000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 42, TargetType: TopUpTargetTypeUser, TargetId: 42,
		Amount: amount, TradeNo: tradeNo, ProviderOrderId: paymentKey,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, claimed)
	firstKey, err := PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, token, "sk_legacy_attempt")
	require.NoError(t, err)
	require.Equal(t, tradeNo, firstKey)
	require.NoError(t, ReleasePendingTossTopUpConfirmRetry(tradeNo, token))

	// Enabling the gate cannot change an attempt an old/gate-off node may have
	// already sent. Empty+attempted is permanently interpreted as legacy.
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	retryToken, _, retried, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, retried)
	retryKey, err := PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, retryToken, "sk_legacy_attempt")
	require.NoError(t, err)
	require.Equal(t, tradeNo, retryKey)
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Empty(t, stored.ProviderIdempotencyKey)
}

func TestResetClaimedTossTopUpScopedBindingAllowsRealCallback(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}))
	require.NoError(t, DB.Create(&User{Id: 43, Username: "reset-scoped-binding", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo      = "toss_reset_scoped_binding"
		forgedKey    = "pay_forged_scoped_binding"
		realKey      = "pay_real_scoped_binding"
		amount       = int64(13000)
		originalTime = int64(1234)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 43, TargetType: TopUpTargetTypeUser, TargetId: 43,
		Amount: amount, TradeNo: tradeNo, ProviderOrderId: forgedKey,
		ProviderOrderTime: originalTime, ProviderRetryTime: originalTime + 1,
		ProviderConfirmProtocolVersion: tossTopUpConfirmProtocolPaymentKey,
		PaymentMethod:                  PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, forgedKey, amount)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = PrepareClaimedPendingTossTopUpConfirm(tradeNo, forgedKey, amount, token, "sk_scoped_reset")
	require.NoError(t, err)

	reset, err := ResetClaimedPendingTossTopUpPaymentBinding(tradeNo, forgedKey, amount, token)
	require.NoError(t, err)
	require.True(t, reset)
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, tradeNo, stored.ProviderOrderId)
	require.Zero(t, stored.ProviderOrderTime)
	require.Zero(t, stored.ProviderRetryTime)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderIdempotencyKey)
	require.Empty(t, stored.ProviderClaimToken)
	require.NotEmpty(t, stored.ProviderCredential, "the exact MID credential remains available for the real callback")
	require.NoError(t, RecordTossPaymentKey(tradeNo, realKey))
	stored, err = GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, realKey, stored.ProviderOrderId)
}

func TestResetClaimedTossTopUpBindingDoesNotEraseConcurrentSettlement(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 45, Username: "reset-settlement-race", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo    = "toss_reset_settlement_race"
		paymentKey = "pay_reset_settlement_race"
		amount     = int64(13000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 45, TargetType: TopUpTargetTypeUser, TargetId: 45,
		Amount: amount, Quota: 999, TradeNo: tradeNo, ProviderOrderId: paymentKey,
		ProviderConfirmProtocolVersion: tossTopUpConfirmProtocolPaymentKey,
		PaymentMethod:                  PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, token, "sk_reset_settlement_race")
	require.NoError(t, err)

	// Model the competing settlement committing after the verifier captured its
	// claim but before it attempted to erase the rejected callback binding.
	require.NoError(t, RechargeToss(tradeNo, paymentKey, "127.0.0.1"))
	reset, err := ResetClaimedPendingTossTopUpPaymentBinding(tradeNo, paymentKey, amount, token)
	require.False(t, reset)
	require.ErrorIs(t, err, ErrTossTopUpConfirmClaimLost)

	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	require.Empty(t, stored.ProviderClaimToken)
	var user User
	require.NoError(t, DB.First(&user, 45).Error)
	require.Equal(t, 999, user.Quota)
}

func TestResetClaimedTossTopUpRejectsLegacyOrEventBearingBinding(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}))
	require.NoError(t, DB.Create(&User{Id: 44, Username: "unsafe-binding-reset", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo    = "toss_unsafe_binding_reset"
		paymentKey = "pay_unsafe_binding_reset"
		amount     = int64(13000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 44, TargetType: TopUpTargetTypeUser, TargetId: 44,
		Amount: amount, TradeNo: tradeNo, ProviderOrderId: paymentKey,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, amount)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, token, "sk_legacy_reset")
	require.NoError(t, err)
	reset, err := ResetClaimedPendingTossTopUpPaymentBinding(tradeNo, paymentKey, amount, token)
	require.False(t, reset)
	require.ErrorIs(t, err, ErrTossTopUpBindingResetLegacy)

	// Upgrade the test row to a scoped attempt, then prove durable provider
	// evidence blocks erasure even under the correct live claim.
	scopedKey := tossPaymentKeyScopedConfirmIdempotencyKey(tradeNo, paymentKey)
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).
		Updates(map[string]interface{}{
			"provider_idempotency_key":          scopedKey,
			"provider_confirm_protocol_version": tossTopUpConfirmProtocolPaymentKey,
		}).Error)
	require.NoError(t, DB.Create(&TossPaymentEvent{
		EventKey: "unsafe-binding-reset-event", EventType: TossPaymentEventTypeFulfillment,
		OrderId: tradeNo, PaymentKey: paymentKey, Status: "DONE",
		ReconciliationStatus: TossReconciliationStatusRequired,
	}).Error)
	reset, err = ResetClaimedPendingTossTopUpPaymentBinding(tradeNo, paymentKey, amount, token)
	require.False(t, reset)
	require.ErrorIs(t, err, ErrTossTopUpBindingResetUnsafe)
	stored, lookupErr := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, lookupErr)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	require.True(t, stored.ProviderAttempted)
}

func TestClaimPendingTossTopUpConfirmRetryRejectsOtherPaymentMethod(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	require.NoError(t, DB.Create(&TopUp{
		UserId:          7,
		Amount:          13000,
		TradeNo:         "toss_other_method_claim",
		ProviderOrderId: "pay_other_method_claim",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	_, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(
		"toss_other_method_claim",
		"pay_other_method_claim",
		13000,
	)
	require.False(t, claimed)
	require.True(t, errors.Is(err, ErrPaymentMethodMismatch))
}

func TestClaimedTossTopUpCredentialUpdateRejectsDisabledUser(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}))
	require.NoError(t, DB.Create(&User{Id: 21, Username: "disabled-after-claim", Status: common.UserStatusEnabled}).Error)
	const (
		tradeNo    = "toss_disabled_after_claim"
		paymentKey = "pay_disabled_after_claim"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          21,
		Amount:          13000,
		TradeNo:         tradeNo,
		ProviderOrderId: paymentKey,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, 13000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 21).Update("status", common.UserStatusDisabled).Error)

	err = UpdateClaimedPendingTossTopUpConfirmCredential(tradeNo, paymentKey, 13000, token, "sk_must_not_be_used")
	require.ErrorIs(t, err, ErrTossTopUpLifecycleInactive)
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Empty(t, stored.ProviderCredential)
}

func TestClaimedTossTopUpCredentialUpdateRejectsRecordedCancellationBeforePOST(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}))
	require.NoError(t, DB.Create(&User{Id: 23, Username: "cancel-before-post", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}).Error)
	const (
		tradeNo    = "toss_cancel_before_confirm_post"
		paymentKey = "pay_cancel_before_confirm_post"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 23, TargetType: TopUpTargetTypeUser, TargetId: 23,
		Amount: 13000, TradeNo: tradeNo, ProviderOrderId: paymentKey,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, 13000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Create(&TossPaymentEvent{
		EventKey: "cancel-before-confirm-post", EventType: TossPaymentEventTypeCancellation,
		OrderId: tradeNo, PaymentKey: paymentKey, Status: "CANCELED",
		ReconciliationStatus: TossReconciliationStatusRequired,
	}).Error)

	err = UpdateClaimedPendingTossTopUpConfirmCredential(tradeNo, paymentKey, 13000, token, "sk_must_not_post")
	require.ErrorIs(t, err, ErrTossCancellationPrecedesFulfillment)
	var stored TopUp
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&stored).Error)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderClaimToken)
	require.Empty(t, stored.ProviderCredential)
}

func TestClaimedOrganizationTossTopUpRejectsOwnershipChange(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &Organization{}, &TopUp{}))
	require.NoError(t, DB.Create(&Organization{Id: 30, Name: "topup-owner-change", OwnerUserId: 22, Status: OrganizationStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{
		Id:               22,
		Username:         "topup-original-owner",
		Status:           common.UserStatusEnabled,
		OrganizationId:   30,
		OrganizationRole: OrganizationRoleOwner,
	}).Error)
	const (
		tradeNo    = "toss_org_owner_changed"
		paymentKey = "pay_org_owner_changed"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          22,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        30,
		Amount:          13000,
		TradeNo:         tradeNo,
		ProviderOrderId: paymentKey,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)
	token, _, claimed, err := ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey, 13000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&Organization{}).Where("id = ?", 30).Update("owner_user_id", 999).Error)

	_, err = ValidatePendingTossTopUpConfirmClaim(tradeNo, paymentKey, 13000, token)
	require.ErrorIs(t, err, ErrTossTopUpLifecycleInactive)
}

func TestClosePendingTossTopUpForReconcileDefersToFreshForeignClaim(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	now := GetDBTimestamp()
	const (
		tradeNo    = "toss_reconcile_foreign_claim"
		paymentKey = "pay_reconcile_foreign_claim"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:             7,
		Amount:             13000,
		TradeNo:            tradeNo,
		ProviderOrderId:    paymentKey,
		ProviderOrderTime:  100,
		ProviderRetryTime:  200,
		ProviderClaimToken: "foreign-live-claim",
		ProviderClaimTime:  now,
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}).Error)

	closed, err := ClosePendingTossTopUpForReconcile(tradeNo, paymentKey, common.TopUpStatusExpired, "", 100, 200)
	require.NoError(t, err)
	require.False(t, closed)
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, "foreign-live-claim", stored.ProviderClaimToken)
}

func TestClosePendingTossTopUpForReconcileAllowsOwnClaimAndRejectsStaleSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	now := GetDBTimestamp()
	const (
		tradeNo    = "toss_reconcile_own_claim"
		paymentKey = "pay_reconcile_own_claim"
		claimToken = "owned-live-claim"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:             7,
		Amount:             13000,
		TradeNo:            tradeNo,
		ProviderOrderId:    paymentKey,
		ProviderOrderTime:  100,
		ProviderRetryTime:  201,
		ProviderClaimToken: claimToken,
		ProviderClaimTime:  now,
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}).Error)

	closed, err := ClosePendingTossTopUpForReconcile(tradeNo, paymentKey, common.TopUpStatusFailed, claimToken, 100, 200)
	require.NoError(t, err)
	require.False(t, closed, "a changed retry marker must invalidate the stale snapshot")

	closed, err = ClosePendingTossTopUpForReconcile(tradeNo, paymentKey, common.TopUpStatusFailed, claimToken, 100, 201)
	require.NoError(t, err)
	require.True(t, closed)
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestRecordTossPaymentKeyPreservesFirstRecordedTime(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	const (
		tradeNo    = "toss_record_time_immutable"
		paymentKey = "pay_record_time_immutable"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:            7,
		Amount:            13000,
		TradeNo:           tradeNo,
		ProviderOrderId:   paymentKey,
		ProviderOrderTime: 123,
		ProviderRetryTime: 456,
		PaymentMethod:     PaymentMethodToss,
		PaymentProvider:   PaymentProviderToss,
		Status:            common.TopUpStatusPending,
	}).Error)

	require.NoError(t, RecordTossPaymentKey(tradeNo, paymentKey))
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, int64(123), stored.ProviderOrderTime)
	require.Greater(t, stored.ProviderRetryTime, GetDBTimestamp())
}

func TestRecordTossPaymentKeyUsesDatabaseClockForRecoverySchedule(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	const tradeNo = "toss_database_clock_schedule"
	require.NoError(t, DB.Create(&TopUp{
		UserId:          7,
		Amount:          13000,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	before := GetDBTimestamp()
	require.NoError(t, RecordTossPaymentKey(tradeNo, "pay_database_clock_schedule"))
	after := GetDBTimestamp()
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.GreaterOrEqual(t, stored.ProviderOrderTime, before)
	require.LessOrEqual(t, stored.ProviderOrderTime, after)
	require.Equal(t, stored.ProviderOrderTime+tossTopUpConfirmClaimTTLSeconds, stored.ProviderRetryTime)
}

func TestRechargeTossDoesNotOverwriteDifferentRecordedPaymentKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 18, Username: "toss-key-conflict-user"}).Error)
	const tradeNo = "toss_settlement_key_conflict"
	require.NoError(t, DB.Create(&TopUp{
		UserId:          18,
		Amount:          13000,
		Quota:           777,
		TradeNo:         tradeNo,
		ProviderOrderId: "pay_already_recorded",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	require.Error(t, RechargeToss(tradeNo, "pay_different_authoritative", "127.0.0.1"))
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, "pay_already_recorded", stored.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, 18).Error)
	require.Zero(t, user.Quota)
}

func TestRechargeTossAllowsWebhookFirstTradeNoMarker(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 19, Username: "toss-webhook-first-user"}).Error)
	const tradeNo = "toss_webhook_first_marker"
	require.NoError(t, DB.Create(&TopUp{
		UserId:          19,
		Amount:          13000,
		Quota:           888,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	require.NoError(t, RechargeToss(tradeNo, "pay_webhook_first", "127.0.0.1"))
	stored, err := GetTopUpByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
	require.Equal(t, "pay_webhook_first", stored.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, 19).Error)
	require.Equal(t, 888, user.Quota)
}

func TestCreditTopUpTargetRejectsQuotaCapacityOverflowAtomically(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}))
	require.NoError(t, DB.Create(&User{Id: 20, Username: "quota-capacity-user", Quota: math.MaxInt32 - 5}).Error)

	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreditTopUpTarget(tx, &TopUp{UserId: 20}, 10)
	})
	require.ErrorContains(t, err, "quota capacity exceeded")
	require.ErrorIs(t, err, ErrTopUpQuotaCapacityExceeded)
	var user User
	require.NoError(t, DB.First(&user, 20).Error)
	require.Equal(t, math.MaxInt32-5, user.Quota)
}

func TestTossRefundRequirementAtomicallyBlocksQuotaFulfillment(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 22, Username: "refund-fence-user"}).Error)
	const (
		orderID    = "toss_refund_fence_order"
		paymentKey = "pay_refund_fence_order"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 22, Amount: 13000, Quota: 900, TradeNo: orderID,
		ProviderOrderId: paymentKey, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	created, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 13000, BalanceAmount: 13000,
		ReconciliationStatus: TossReconciliationStatusRequired, ResolutionNote: "test contract mismatch",
	})
	require.NoError(t, err)
	require.True(t, created)
	require.ErrorIs(t, RechargeToss(orderID, paymentKey, "127.0.0.1"), ErrTossRefundRequiredPrecedesFulfillment)

	var user User
	require.NoError(t, DB.First(&user, 22).Error)
	require.Zero(t, user.Quota)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, TossTopUpStatusRefundPending, topUp.Status)
	require.ErrorIs(t, RecoverTossTopUpPaymentKeyWithContext(context.Background(), orderID, paymentKey), ErrTopUpStatusInvalid)
}

func TestFinalizeFullyRefundedTossTopUpRequiresAndResolvesCancellationEvidence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 23, Username: "refund-finalize-user"}).Error)
	const (
		orderID    = "toss_refund_finalize_order"
		paymentKey = "pay_refund_finalize_order"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 23, Amount: 14000, Quota: 901, TradeNo: orderID,
		ProviderOrderId: paymentKey, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	_, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 14000, BalanceAmount: 14000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.Error(t, FinalizeFullyRefundedTossTopUpWithContext(context.Background(), orderID, paymentKey, `{"status":"CANCELED"}`))

	_, err = RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "toss_cancel_refund_finalize", EventType: TossPaymentEventTypeCancellation,
		OrderId: orderID, PaymentKey: paymentKey, Status: "CANCELED",
		CancelAmount: 14000, BalanceAmount: 0, OriginalAmount: 14000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, FinalizeFullyRefundedTossTopUpWithContext(context.Background(), orderID, paymentKey, `{"status":"CANCELED"}`))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	var required int64
	require.NoError(t, DB.Model(&TossPaymentEvent{}).Where("order_id = ? AND reconciliation_status = ?", orderID, TossReconciliationStatusRequired).Count(&required).Error)
	require.Zero(t, required)
	var user User
	require.NoError(t, DB.First(&user, 23).Error)
	require.Zero(t, user.Quota)
}

func TestTossRefundRequirementKeepsUncreditedTerminalOrderBehindFence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 24, Username: "refund-reopen-user"}).Error)
	const (
		orderID    = "toss_refund_reopen_order"
		paymentKey = "pay_refund_reopen_order"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 24, Amount: 15000, Quota: 902, TradeNo: orderID,
		ProviderOrderId: orderID, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusExpired, CompleteTime: 10,
	}).Error)
	_, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 15000, BalanceAmount: 15000,
	})
	require.NoError(t, err)

	var fenced TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&fenced).Error)
	require.Equal(t, TossTopUpStatusRefundPending, fenced.Status)
	require.EqualValues(t, 10, fenced.CompleteTime)
	require.Equal(t, paymentKey, fenced.ProviderOrderId)
	require.Positive(t, fenced.ProviderRetryTime)
	require.ErrorIs(t, RechargeToss(orderID, paymentKey, "test"), ErrTossRefundRequiredPrecedesFulfillment)

	var user User
	require.NoError(t, DB.First(&user, 24).Error)
	require.Zero(t, user.Quota)
}

func TestDuplicateTossRefundEvidencePreservesManualRetryDeferral(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 27, Username: "refund-deferral-user"}).Error)
	const (
		orderID    = "toss_refund_deferral"
		paymentKey = "pay_refund_deferral"
		amount     = int64(18000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 27, Amount: amount, Quota: 905, TradeNo: orderID,
		ProviderOrderId: paymentKey, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	evidence := &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: amount, BalanceAmount: amount,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	_, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), evidence)
	require.NoError(t, err)
	retryAt := GetDBTimestamp() + 24*60*60
	require.NoError(t, DeferPendingTossTopUpProviderRetryWithContext(context.Background(), orderID, paymentKey, retryAt))

	duplicate := *evidence
	duplicate.Status = "PARTIAL_CANCELED"
	duplicate.BalanceAmount = amount / 2
	created, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), &duplicate)
	require.NoError(t, err)
	require.False(t, created)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, TossTopUpStatusRefundPending, topUp.Status)
	require.Equal(t, retryAt, topUp.ProviderRetryTime)
}

func TestResolvedTossRefundRequirementCannotReopenFinalizedOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 25, Username: "refund-finalized-race-user"}).Error)
	const (
		orderID    = "toss_refund_finalized_race"
		paymentKey = "pay_refund_finalized_race"
		amount     = int64(16000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 25, Amount: amount, Quota: 903, TradeNo: orderID,
		ProviderOrderId: paymentKey, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	refund := &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: amount, BalanceAmount: amount,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	_, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), refund)
	require.NoError(t, err)
	_, err = RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "toss_cancel_refund_finalized_race", EventType: TossPaymentEventTypeCancellation,
		OrderId: orderID, PaymentKey: paymentKey, Status: "CANCELED",
		CancelAmount: amount, OriginalAmount: amount, BalanceAmount: 0,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, FinalizeFullyRefundedTossTopUpWithContext(context.Background(), orderID, paymentKey, `{"status":"CANCELED"}`))

	// A stale authenticated DONE observation can arrive after finalization. The
	// duplicate evidence must not reactivate either the order or its fence.
	created, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), refund)
	require.NoError(t, err)
	require.False(t, created)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	var required int64
	require.NoError(t, DB.Model(&TossPaymentEvent{}).
		Where("order_id = ? AND reconciliation_status = ?", orderID, TossReconciliationStatusRequired).
		Count(&required).Error)
	require.Zero(t, required)
}

func TestFinalizeUnfundedTossRefundRequirementClosesWithoutCancellation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 26, Username: "unfunded-refund-user"}).Error)
	const (
		orderID    = "toss_unfunded_refund"
		paymentKey = "pay_unfunded_refund"
		amount     = int64(17000)
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 26, Amount: amount, Quota: 904, TradeNo: orderID,
		ProviderOrderId: paymentKey, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	_, err := RecordTossTopUpRefundRequirementWithContext(context.Background(), &TossPaymentEvent{
		EventKey: TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "WAITING_FOR_DEPOSIT",
		OriginalAmount: amount, BalanceAmount: amount, ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, FinalizeUnfundedTossTopUpRefundRequirementWithContext(
		context.Background(), orderID, paymentKey, "EXPIRED", `{"status":"EXPIRED"}`,
	))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
	var event TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&event).Error)
	require.Equal(t, TossReconciliationStatusResolved, event.ReconciliationStatus)
	var user User
	require.NoError(t, DB.First(&user, 26).Error)
	require.Zero(t, user.Quota)
}

func TestCreditTopUpTargetCanRestoreNegativeBalance(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}))
	require.NoError(t, DB.Create(&User{Id: 21, Username: "negative-balance-user", Quota: -50}).Error)

	err := DB.Transaction(func(tx *gorm.DB) error {
		return CreditTopUpTarget(tx, &TopUp{UserId: 21}, 75)
	})
	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, 21).Error)
	require.Equal(t, 25, user.Quota)
}

func TestTossTopUpQuotaMustFitPortableDatabaseInt(t *testing.T) {
	quota, ok := tossTopUpPositiveInt(decimal.NewFromInt(int64(math.MaxInt32) + 1))
	require.False(t, ok)
	require.Zero(t, quota)
}

func TestGeneralTossMutationsRejectOtherPaymentMethod(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &Log{}))
	require.NoError(t, DB.Create(&User{Id: 17, Username: "other-method-user"}).Error)
	topUp := &TopUp{
		UserId:          17,
		Amount:          13000,
		Quota:           321,
		TradeNo:         "toss_other_method_mutation",
		ProviderOrderId: "toss_other_method_mutation",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	require.ErrorIs(t, RecordTossPaymentKey(topUp.TradeNo, "pay_other_method_mutation"), ErrPaymentMethodMismatch)
	require.Error(t, RechargeToss(topUp.TradeNo, "pay_other_method_mutation", "127.0.0.1"))
	require.ErrorIs(t, UpdatePendingTopUpStatusForMethod(
		topUp.TradeNo,
		PaymentProviderToss,
		PaymentMethodToss,
		common.TopUpStatusFailed,
	), ErrPaymentMethodMismatch)

	stored, err := GetTopUpByTradeNoWithError(topUp.TradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, topUp.TradeNo, stored.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, 17).Error)
	require.Zero(t, user.Quota)
}
