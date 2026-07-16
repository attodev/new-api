package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedTossSubscriptionIssueCleanupClaimTestOrder(t *testing.T, tradeNo, token string) SubscriptionOrder {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	order := SubscriptionOrder{
		UserId:                       701,
		PlanId:                       17,
		TradeNo:                      tradeNo,
		PaymentMethod:                PaymentMethodToss,
		PaymentProvider:              PaymentProviderToss,
		Status:                       common.TopUpStatusPending,
		CreateTime:                   GetDBTimestamp(),
		BillingClaimToken:            token,
		BillingClaimTime:             GetDBTimestamp(),
		BillingIssueAuthKey:          "encrypted-auth-snapshot",
		BillingIssueAuthKeyHash:      "auth-snapshot-hash",
		BillingIssueCustomerKey:      "cleanup-customer",
		BillingIssueAttempted:        true,
		ProviderCredential:           "encrypted-provider-credential",
		ProviderClientKeyHash:        "provider-client-key-hash",
		BillingChargeProtocolVersion: 1,
	}
	require.NoError(t, DB.Create(&order).Error)
	return order
}

func TestTransitionClaimedTossSubscriptionBillingIssueToCleanupPreservesEvidence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	order := seedTossSubscriptionIssueCleanupClaimTestOrder(t, "toss-sub-cleanup-owned", "owned-token")

	require.NoError(t, TransitionClaimedTossSubscriptionBillingIssueToCleanup(order.TradeNo, "owned-token"))

	var got SubscriptionOrder
	require.NoError(t, DB.First(&got, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, got.Status)
	require.Equal(t, "owned-token", got.BillingClaimToken)
	require.True(t, got.BillingIssueAttempted)
	require.Equal(t, order.BillingIssueAuthKey, got.BillingIssueAuthKey)
	require.Equal(t, order.BillingIssueAuthKeyHash, got.BillingIssueAuthKeyHash)
	require.Equal(t, order.BillingIssueCustomerKey, got.BillingIssueCustomerKey)
	require.Equal(t, order.ProviderCredential, got.ProviderCredential)
}

func TestTransitionClaimedTossSubscriptionBillingIssueToCleanupRejectsStaleOwnerAfterAttach(t *testing.T) {
	setupTossBillingModelTestDB(t)
	order := seedTossSubscriptionIssueCleanupClaimTestOrder(t, "toss-sub-cleanup-stale", "old-token")
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_claim_token": "new-owner-token",
		"billing_key_id":      812,
	}).Error)

	err := TransitionClaimedTossSubscriptionBillingIssueToCleanup(order.TradeNo, "old-token")
	require.ErrorIs(t, err, ErrTossBillingClaimLost)

	var got SubscriptionOrder
	require.NoError(t, DB.First(&got, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, got.Status)
	require.Equal(t, "new-owner-token", got.BillingClaimToken)
	require.Equal(t, 812, got.BillingKeyId)
	require.True(t, got.BillingIssueAttempted)
}

func TestCancelUnattemptedTossSubscriptionOrderStaleReadPreservesNewIssueOwner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	order := SubscriptionOrder{
		UserId: 704, PlanId: 21, TradeNo: "toss-sub-preclaim-stale-read",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(&order).Error)

	stale, err := GetSubscriptionOrderByTradeNoWithError(order.TradeNo)
	require.NoError(t, err)
	require.Empty(t, stale.BillingIssueAuthKey)
	claimToken, claimed, err := ClaimTossSubscriptionBillingIssue(order.TradeNo, "new-owner-auth", "new-owner-customer")
	require.NoError(t, err)
	require.True(t, claimed)

	cancelled, err := CancelUnattemptedTossSubscriptionOrder(stale.TradeNo, stale.UserId)
	require.NoError(t, err)
	require.False(t, cancelled)

	var got SubscriptionOrder
	require.NoError(t, DB.First(&got, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, got.Status)
	require.Equal(t, claimToken, got.BillingClaimToken)
	require.NotEmpty(t, got.BillingIssueAuthKey)
	require.NotEmpty(t, got.BillingIssueAuthKeyHash)
	require.Equal(t, "new-owner-customer", got.BillingIssueCustomerKey)
}
