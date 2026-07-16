package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromoteClaimedWalletAutoRechargeBillingIssueCredentialPersistsUnattemptedNamespace(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	const (
		tradeNo       = "wallet_auto_issue_rotate_model"
		claimToken    = "wallet-issue-rotate-claim"
		clientKey     = "live_ck_wallet_issue_rotate_model"
		oldSecret     = "live_sk_wallet_issue_rotate_old"
		rotatedSecret = "live_sk_wallet_issue_rotate_new"
	)
	credential, err := EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 901, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 901, OwnerUserId: 901,
		Status: WalletAutoRechargeStatusPending, ActiveKey: &activeKey,
		CustomerKey: "cust_wallet_issue_rotate", AuthTradeNo: tradeNo,
		ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint(clientKey),
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		IssueClaimToken: claimToken, IssueClaimTime: GetDBTimestamp() - 10,
		IssueAuthKey: "encrypted-auth-snapshot", IssueAttempted: true,
		CreateTime: GetDBTimestamp(), UpdateTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	require.NoError(t, PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		clientKey,
		rotatedSecret,
	))

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	persistedSecret, err := DecryptProviderCredential(stored.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, rotatedSecret, persistedSecret)
	require.False(t, stored.IssueAttempted, "the promoted namespace must stay unattempted until its own pre-POST gate")
	require.Equal(t, claimToken, stored.IssueClaimToken)
	require.Greater(t, stored.IssueClaimTime, int64(0))

	require.NoError(t, MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, claimToken))
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	require.True(t, stored.IssueAttempted)
}

func TestPromoteClaimedWalletAutoRechargeBillingIssueCredentialRejectsUnsafeTransitions(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	const (
		tradeNo    = "wallet_auto_issue_rotate_reject"
		claimToken = "wallet-issue-rotate-reject-claim"
		oldSecret  = "live_sk_wallet_issue_rotate_reject_old"
	)
	credential, err := EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 902, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 902, OwnerUserId: 902,
		Status: WalletAutoRechargeStatusPending, ActiveKey: &activeKey,
		CustomerKey: "cust_wallet_issue_reject", AuthTradeNo: tradeNo,
		ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("live_ck_wallet_issue_original_mid"),
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		IssueClaimToken: claimToken, IssueAuthKey: "encrypted-auth-snapshot", IssueAttempted: true,
		CreateTime: GetDBTimestamp(), UpdateTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err = PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		"live_ck_wallet_issue_different_mid",
		"live_sk_wallet_issue_other_mid",
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost)

	canonicalHash := TossBillingClientKeyFingerprint("live_ck_wallet_issue_original_mid")
	for _, malformed := range []string{
		strings.ToUpper(canonicalHash),
		" " + canonicalHash,
		canonicalHash[:len(canonicalHash)-1],
		strings.Repeat("z", len(canonicalHash)),
	} {
		require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).
			Update("provider_client_key_hash", malformed).Error)
		err = PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
			tradeNo,
			claimToken,
			oldSecret,
			"live_ck_wallet_issue_original_mid",
			"live_sk_wallet_issue_new",
		)
		require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost, malformed)
	}

	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"status":                   WalletAutoRechargeStatusCancelPending,
		"issue_attempted":          true,
		"provider_client_key_hash": canonicalHash,
	}).Error)
	err = PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
		tradeNo,
		claimToken,
		oldSecret,
		"live_ck_wallet_issue_original_mid",
		"live_sk_wallet_issue_new",
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost, "a cancelled policy must never create a rotated billing key")

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	persistedSecret, err := DecryptProviderCredential(stored.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, stored.Status)
	require.True(t, stored.IssueAttempted)
}
