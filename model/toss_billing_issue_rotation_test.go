package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPromoteClaimedTossSubscriptionBillingIssueCredentialPersistsUnattemptedNamespace(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	const (
		tradeNo       = "toss_sub_issue_rotate_model"
		claimToken    = "issue-rotate-claim"
		clientKey     = "live_ck_issue_rotate_model"
		oldSecret     = "live_sk_issue_rotate_old"
		rotatedSecret = "live_sk_issue_rotate_new"
	)
	credential, err := EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	order := SubscriptionOrder{
		TradeNo:               tradeNo,
		PaymentMethod:         PaymentMethodToss,
		PaymentProvider:       PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		ProviderCredential:    credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint(clientKey),
		BillingClaimToken:     claimToken,
		BillingClaimTime:      GetDBTimestamp() - 10,
		BillingIssueAuthKey:   "encrypted-auth-snapshot",
		BillingIssueAttempted: true,
	}
	require.NoError(t, DB.Create(&order).Error)

	require.NoError(t, PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		clientKey,
		rotatedSecret,
	))

	var stored SubscriptionOrder
	require.NoError(t, DB.First(&stored, order.Id).Error)
	persistedSecret, err := DecryptProviderCredential(stored.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, rotatedSecret, persistedSecret)
	require.False(t, stored.BillingIssueAttempted, "the promoted namespace must stay unattempted until its own pre-POST gate")
	require.Equal(t, claimToken, stored.BillingClaimToken)
	require.Greater(t, stored.BillingClaimTime, int64(0))

	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt(tradeNo, claimToken))
	require.NoError(t, DB.First(&stored, order.Id).Error)
	require.True(t, stored.BillingIssueAttempted)
}

func TestPromoteClaimedTossSubscriptionBillingIssueCredentialRejectsUnsafeTransitions(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	const (
		tradeNo    = "toss_sub_issue_rotate_reject"
		claimToken = "issue-rotate-reject-claim"
		oldSecret  = "live_sk_issue_rotate_reject_old"
	)
	credential, err := EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	order := SubscriptionOrder{
		TradeNo:               tradeNo,
		PaymentMethod:         PaymentMethodToss,
		PaymentProvider:       PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		ProviderCredential:    credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("live_ck_issue_rotate_original_mid"),
		BillingClaimToken:     claimToken,
		BillingIssueAuthKey:   "encrypted-auth-snapshot",
		BillingIssueAttempted: true,
	}
	require.NoError(t, DB.Create(&order).Error)

	err = PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		"live_ck_issue_rotate_different_mid",
		"live_sk_issue_rotate_other_mid",
	)
	require.ErrorIs(t, err, ErrTossBillingClaimLost)

	canonicalHash := TossBillingClientKeyFingerprint("live_ck_issue_rotate_original_mid")
	for _, malformed := range []string{
		strings.ToUpper(canonicalHash),
		" " + canonicalHash,
		canonicalHash[:len(canonicalHash)-1],
		strings.Repeat("z", len(canonicalHash)),
	} {
		require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
			Update("provider_client_key_hash", malformed).Error)
		err = PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
			tradeNo,
			claimToken,
			oldSecret,
			"live_ck_issue_rotate_original_mid",
			"live_sk_issue_rotate_new",
		)
		require.ErrorIs(t, err, ErrTossBillingClaimLost, malformed)
	}

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"provider_client_key_hash": canonicalHash,
		"billing_issue_attempted":  false,
	}).Error)
	err = PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		"live_ck_issue_rotate_original_mid",
		"live_sk_issue_rotate_new",
	)
	require.ErrorIs(t, err, ErrTossBillingClaimLost, "an unattempted credential has no rejected POST to promote from")

	var stored SubscriptionOrder
	require.NoError(t, DB.First(&stored, order.Id).Error)
	persistedSecret, err := DecryptProviderCredential(stored.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
}

func TestFinishClaimedTossSubscriptionBillingIssueCleanupClearsProviderSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	const tradeNo = "toss_sub_issue_cleanup_finished"
	credential, err := EncryptProviderCredential("live_sk_issue_cleanup_finished")
	require.NoError(t, err)
	order := SubscriptionOrder{
		TradeNo: tradeNo, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("live_ck_issue_cleanup_finished"),
	}
	require.NoError(t, DB.Create(&order).Error)
	token, claimed, err := ClaimTossSubscriptionBillingIssue(tradeNo, "auth_issue_cleanup_finished", "cust_issue_cleanup_finished")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt(tradeNo, token))

	require.NoError(t, FinishClaimedTossSubscriptionBillingIssueCleanup(tradeNo, token))
	var stored SubscriptionOrder
	require.NoError(t, DB.First(&stored, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, stored.Status)
	require.Empty(t, stored.BillingIssueAuthKey)
	require.False(t, stored.BillingIssueAttempted)
	require.Empty(t, stored.ProviderCredential)
	require.Empty(t, stored.ProviderClientKeyHash)
}
