package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedWalletAutoRechargeIssueCleanupClaimTest(t *testing.T, userID int, tradeNo string) (*WalletAutoRecharge, string) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id: userID, Username: tradeNo, AffCode: tradeNo,
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	credential, err := EncryptProviderCredential("wallet-cleanup-secret")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: userID, OwnerUserId: userID, CustomerKey: "wallet-cleanup-customer",
		AuthTradeNo: tradeNo, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet-cleanup-client"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(tradeNo, "wallet-cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, claimToken))
	require.NoError(t, DB.First(policy, policy.Id).Error)
	return policy, claimToken
}

func TestTransitionClaimedWalletAutoRechargeBillingIssueToCleanupPreservesEvidence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, claimToken := seedWalletAutoRechargeIssueCleanupClaimTest(t, 8101, "wallet-cleanup-owned")
	require.NotNil(t, policy.ActiveKey)

	require.NoError(t, TransitionClaimedWalletAutoRechargeBillingIssueToCleanup(policy.AuthTradeNo, claimToken))

	var got WalletAutoRecharge
	require.NoError(t, DB.First(&got, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, got.Status)
	require.Nil(t, got.ActiveKey)
	require.Zero(t, got.BillingKeyId)
	require.Equal(t, claimToken, got.IssueClaimToken)
	require.True(t, got.IssueAttempted)
	require.Equal(t, policy.IssueAuthKey, got.IssueAuthKey)
	require.Equal(t, policy.IssueAuthKeyHash, got.IssueAuthKeyHash)
	require.Equal(t, policy.IssueCustomerKey, got.IssueCustomerKey)
	require.Equal(t, policy.ProviderCredential, got.ProviderCredential)
}

func TestTransitionClaimedWalletAutoRechargeBillingIssueToCleanupRejectsStaleOwnerAfterActivation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, oldToken := seedWalletAutoRechargeIssueCleanupClaimTest(t, 8102, "wallet-cleanup-stale-active")
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"status":            WalletAutoRechargeStatusActive,
		"billing_key_id":    913,
		"issue_claim_token": "new-owner-token",
	}).Error)

	err := TransitionClaimedWalletAutoRechargeBillingIssueToCleanup(policy.AuthTradeNo, oldToken)
	require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost)

	var got WalletAutoRecharge
	require.NoError(t, DB.First(&got, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, got.Status)
	require.Equal(t, 913, got.BillingKeyId)
	require.Equal(t, "new-owner-token", got.IssueClaimToken)
	require.NotNil(t, got.ActiveKey)
}
