package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestStaleWalletIssueWorkerDoesNotRevokeNewOwnerActiveKey(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{
		Id: 8203, Username: "wallet-stale-cleanup-owner", Password: "x",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "wallet-stale-cleanup-owner",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: user.Id, OwnerUserId: user.Id, CustomerKey: "wallet-stale-cleanup-customer",
		AuthTradeNo: "wallet-stale-cleanup-active", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	oldToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "wallet-stale-cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.DB.First(policy, policy.Id).Error)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalIssuedRevoker := walletAutoRechargeIssuedBillingKeyRevoker
	originalStoredRevoker := walletAutoRechargeStoredBillingKeyRevoker
	issuedCleanupCalls := 0
	storedCleanupCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(context.Context, string, string, string, string) (*tossBillingIssueResponse, int, error) {
		// Simulate a newer lease owner attaching and activating the exact key while
		// this stale worker is paused in the provider ISSUE request.
		require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
			"status":            model.WalletAutoRechargeStatusActive,
			"billing_key_id":    917,
			"issue_claim_token": "new-owner-token",
		}).Error)
		return &tossBillingIssueResponse{
			BillingKey:  "wallet-shared-new-owner-key",
			CustomerKey: "different-customer-for-validation-failure",
		}, 200, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(context.Context, int, string, string, *tossBillingIssueResponse, string, string) error {
		issuedCleanupCalls++
		return nil
	}
	walletAutoRechargeStoredBillingKeyRevoker = func(context.Context, int) error {
		storedCleanupCalls++
		return nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalIssuedRevoker
		walletAutoRechargeStoredBillingKeyRevoker = originalStoredRevoker
	})

	resolved, retainClaim, err := processClaimedWalletAutoRechargeBillingIssue(
		context.Background(), policy, oldToken, "wallet-stale-cleanup-auth", policy.CustomerKey, "live_sk_wallet_auto_billing",
	)
	require.ErrorIs(t, err, model.ErrWalletAutoRechargeClaimLost)
	require.False(t, resolved)
	require.False(t, retainClaim)
	require.Zero(t, issuedCleanupCalls)
	require.Zero(t, storedCleanupCalls)

	var got model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&got, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusActive, got.Status)
	require.Equal(t, 917, got.BillingKeyId)
	require.Equal(t, "new-owner-token", got.IssueClaimToken)
	require.NotNil(t, got.ActiveKey)
}

func TestWalletIssueCleanupReplaySurvivesUnattestedConfig(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{
		Id: 8204, Username: "wallet-unattested-cleanup-owner", Password: "x",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "wallet-unattested-cleanup-owner",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: user.Id, OwnerUserId: user.Id, CustomerKey: "wallet-unattested-cleanup-customer",
		AuthTradeNo: "wallet-unattested-cleanup", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "wallet-unattested-cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.TransitionClaimedWalletAutoRechargeBillingIssueToCleanup(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.DB.First(policy, policy.Id).Error)

	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   "__internal_toss_config_write_lock",
		Value: "unattested_wallet_cleanup_revision",
	}).Error)
	_, freshErr := model.GetFreshTossConfigSnapshot()
	require.ErrorIs(t, freshErr, model.ErrTossConfigRevisionStale)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalRevoker := walletAutoRechargeIssuedBillingKeyRevoker
	issueCalls := 0
	cleanupCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(context.Context, string, string, string, string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return &tossBillingIssueResponse{
			BillingKey:  "wallet-unattested-cleanup-key",
			CustomerKey: policy.CustomerKey,
		}, 200, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(context.Context, int, string, string, *tossBillingIssueResponse, string, string) error {
		cleanupCalls++
		return nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalRevoker
	})

	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), *policy)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, issueCalls)
	require.Equal(t, 1, cleanupCalls)

	var got model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&got, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, got.Status)
	require.Zero(t, got.BillingKeyId)
	require.Empty(t, got.IssueAuthKey)
}
