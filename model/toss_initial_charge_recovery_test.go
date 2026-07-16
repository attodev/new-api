package model

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestClaimTossSubscriptionFirstChargeHasSingleOwnerAndNormalizesPreMarkerSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	credential, err := EncryptProviderCredential("sk_initial_claim_exact")
	require.NoError(t, err)
	order := SubscriptionOrder{
		TradeNo: "toss_sub_initial_claim", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		BillingKeyId: 41, BillingAttempted: false, ProviderCredential: credential,
		BillingClaimToken: "crashed-node", BillingClaimTime: GetDBTimestamp() - tossSubscriptionBillingClaimTTLSeconds - 1,
	}
	require.NoError(t, DB.Create(&order).Error)

	token, claimed, err := ClaimTossSubscriptionFirstChargeOrder(order.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, token)
	_, secondClaimed, err := ClaimTossSubscriptionFirstChargeOrder(order.TradeNo)
	require.NoError(t, err)
	require.False(t, secondClaimed, "a live cross-node lease must have one owner")

	secret, err := GetClaimedTossSubscriptionFirstChargeAttemptSecret(order.TradeNo, token)
	require.NoError(t, err)
	require.Equal(t, "sk_initial_claim_exact", secret)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.True(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingAttemptCredential)
	stored, err := DecryptProviderCredential(order.BillingAttemptCredential)
	require.NoError(t, err)
	require.Equal(t, secret, stored)
}

func TestClaimTossUnattemptedSubscriptionChargeRequiresDurableProtocolAndExactCredential(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	credential, err := EncryptProviderCredential("sk_unattempted_exact")
	require.NoError(t, err)

	tests := []struct {
		name       string
		version    int
		credential string
		wantClaim  bool
		wantUnsafe bool
	}{
		{name: "durable exact", version: tossBillingChargeProtocolDurableAttempt, credential: credential, wantClaim: true},
		{name: "legacy missing", version: tossBillingChargeProtocolLegacy, wantUnsafe: true},
		{name: "legacy exact uses attempted recovery", version: tossBillingChargeProtocolLegacy, credential: credential, wantUnsafe: true},
		{name: "durable missing", version: tossBillingChargeProtocolDurableAttempt, wantUnsafe: true},
		{name: "unknown version", version: 99, credential: credential, wantUnsafe: true},
		{name: "durable corrupt", version: tossBillingChargeProtocolDurableAttempt, credential: "corrupt", wantUnsafe: true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := SubscriptionOrder{
				TradeNo:       fmt.Sprintf("toss_sub_unattempted_protocol_%d", i),
				PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
				Status: common.TopUpStatusPending, BillingKeyId: 41,
				ProviderCredential: tt.credential, BillingChargeProtocolVersion: tt.version,
			}
			require.NoError(t, DB.Create(&order).Error)
			token, claimed, claimErr := ClaimTossUnattemptedSubscriptionChargeOrder(order.TradeNo)
			if tt.wantUnsafe {
				require.ErrorIs(t, claimErr, ErrTossBillingCrossCredentialRetryUnsafe)
				require.False(t, claimed)
				require.Empty(t, token)
				return
			}
			require.NoError(t, claimErr)
			require.Equal(t, tt.wantClaim, claimed)
			require.NotEmpty(t, token)
			require.NoError(t, ReleaseTossSubscriptionBillingClaim(order.TradeNo, token))
		})
	}
}

func TestProcessTossRenewalKillSwitchBlocksProviderPostWithoutFailureCount(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_renewal_kill_switch", 0)
	seedTossBillingPlan(t, "Kill switch renewal", 10)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })
	chargeCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })
	setting.TossBillingEnabled = false

	err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossBillingOperationallyDisabled)
	require.Zero(t, chargeCalls)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.True(t, sub.AutoRenew)
	require.Equal(t, 0, sub.BillingFailCount)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.False(t, order.BillingAttempted)
	require.Empty(t, order.BillingClaimToken)
}

func TestProcessTossRenewalExpiredAttemptSecretUsesSameMIDLookupWithoutNewPost(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_expired_attempt_secret", 0)
	seedTossBillingPlan(t, "Expired attempt secret", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	postCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	order := requireTossRenewalOrderByAttemptForTest(t, 11, 0)
	exactSecret, err := DecryptProviderCredential(order.BillingAttemptCredential)
	require.NoError(t, err)
	require.NotEmpty(t, exactSecret)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)

	rotatedSecret := "test_sk_same_mid_after_old_expiry"
	setting.TossBillingSecretKey = rotatedSecret
	lookups := make([]string, 0, 2)
	previousLookup := tossRenewalPaymentLookup
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookups = append(lookups, secretKey)
		if secretKey == exactSecret {
			return nil, errors.New("toss api failed: code=UNAUTHORIZED_KEY")
		}
		require.Equal(t, rotatedSecret, secretKey)
		return nil, ErrTossBillingPaymentNotFound
	})
	t.Cleanup(func() { SetTossRenewalPaymentLookup(previousLookup) })

	err = ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossBillingCrossCredentialRetryUnsafe)
	require.Equal(t, []string{exactSecret, rotatedSecret}, lookups)
	require.Equal(t, 1, postCalls, "a same-MID GET fallback must never become a POST under the rotated secret")
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.NotEmpty(t, order.BillingClaimToken)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
}

func TestStaleSubscriptionReconcileExcludesFreshClaimAndTerminalCASRejectsLostOwner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		7, "cust_fresh_claim", "billing_fresh_claim", "현대", "433012******1234",
		setting.TossBillingSecretKey, TossBillingClientKeyFingerprint(setting.TossBillingClientKey),
	)
	require.NoError(t, err)
	var billingKey UserBillingKey
	require.NoError(t, DB.First(&billingKey, keyID).Error)
	now := GetDBTimestamp()
	order := SubscriptionOrder{
		TradeNo: "toss_sub_fresh_claim_cleanup", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		CreateTime: now - 3600, BillingKeyId: keyID,
		ProviderCredential:           billingKey.ProviderCredential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
		BillingClaimToken:            "live-callback", BillingClaimTime: now,
	}
	require.NoError(t, DB.Create(&order).Error)

	previousReconciler := tossSubscriptionOrderReconciler
	reconcileCalls := 0
	SetTossSubscriptionOrderReconciler(func(ctx context.Context, order SubscriptionOrder) (bool, error) {
		reconcileCalls++
		return true, nil
	})
	t.Cleanup(func() { SetTossSubscriptionOrderReconciler(previousReconciler) })
	reconciled, err := ReconcileStaleTossPendingSubscriptionOrders(context.Background(), now-300, 10)
	require.NoError(t, err)
	require.Zero(t, reconciled)
	require.Zero(t, reconcileCalls, "an old order with a fresh provider-call lease is not stale")

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_time", now-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	claimToken, claimed, err := ClaimTossUnattemptedSubscriptionChargeOrder(order.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_token", "new-owner").Error)

	err = ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(order.TradeNo, claimToken)
	require.ErrorIs(t, err, ErrTossBillingClaimLost)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status, "a stale cleanup owner must not revoke the current owner's key")
}
