package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestStaleAttemptedSubscriptionIssueRecoversKeyForCleanupOnly(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "stale-age", common.TopUpStatusPending)
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("create_time", model.GetDBTimestamp()-model.TossSubscriptionPurchaseReservationMaxAgeSeconds-1).Error)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	issueCalls, cleanupCalls := installTossSubscriptionIssueCleanupStubs(t, order, "stale_issue_cleanup")

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, *issueCalls, "cleanup may repeat only the original idempotent ISSUE")
	require.Equal(t, 1, *cleanupCalls)

	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Zero(t, order.BillingKeyId)
	require.Empty(t, order.BillingIssueAuthKey)
	require.False(t, order.BillingIssueAttempted)
	var subscriptions int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", order.UserId).Count(&subscriptions).Error)
	require.Zero(t, subscriptions)
}

func TestStaleAttemptedSubscriptionIssueStaysTerminalWhenCleanupIsUncertain(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "stale-uncertain", common.TopUpStatusPending)
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("create_time", model.GetDBTimestamp()-model.TossSubscriptionPurchaseReservationMaxAgeSeconds-1).Error)
	require.NoError(t, model.DB.First(&order, order.Id).Error)

	originalIssuer := subscriptionTossBillingKeyIssuer
	issueCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, 0, context.DeadlineExceeded
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.Error(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, issueCalls)

	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status, "cleanup uncertainty must not return the order to a chargeable state")
	require.Zero(t, order.BillingKeyId)
	require.NotEmpty(t, order.BillingIssueAuthKey, "exact authorization must remain for later cleanup-only retry")
	require.True(t, order.BillingIssueAttempted)
	require.NotEmpty(t, order.BillingClaimToken)
}

func TestAttemptedSubscriptionIssuePastIdempotencyWindowDoesNotReplayPOST(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "idempotency-expired", common.TopUpStatusPending)
	replayCutoff := model.GetDBTimestamp() - model.TossProviderIdempotencyRetentionSeconds - 1
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("create_time", replayCutoff).Error)
	require.NoError(t, model.DB.First(&order, order.Id).Error)

	originalIssuer := subscriptionTossBillingKeyIssuer
	issueCalls := 0
	subscriptionTossBillingKeyIssuer = func(context.Context, string, string, string, string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, 0, errors.New("provider POST must not be replayed")
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.ErrorIs(t, err, errTossBillingIssueIdempotencyWindowExpired)
	require.False(t, resolved)
	require.Zero(t, issueCalls)

	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.True(t, order.BillingIssueAttempted)
	require.NotEmpty(t, order.BillingIssueAuthKey)
	require.NotEmpty(t, order.BillingClaimToken)
}

func TestAttemptedSubscriptionIssueRetainsSnapshotWhenIssueCredentialExpires(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "expired-secret", common.TopUpStatusPending)
	originalIssuer := subscriptionTossBillingKeyIssuer
	issueCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, http.StatusUnauthorized, newTossAPIError(
			http.MethodPost,
			http.StatusUnauthorized,
			[]byte(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`),
		)
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.Error(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, issueCalls)

	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.NotEmpty(t, order.BillingIssueAuthKey)
	require.True(t, order.BillingIssueAttempted)
	require.NotEmpty(t, order.BillingClaimToken,
		"an expired issue-time API key cannot prove that the earlier response-lost ISSUE created no billing key")
}

func TestStaleAttemptedWalletIssueRecoversKeyForCleanupOnly(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 8201, Username: "stale-wallet-issue", Password: "x", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "stale-wallet-issue"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: user.Id, OwnerUserId: user.Id, CustomerKey: "stale-wallet-customer",
		AuthTradeNo: "stale-wallet-issue-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "stale-wallet-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).
		Update("create_time", model.GetDBTimestamp()-model.TossSubscriptionPurchaseReservationMaxAgeSeconds-1).Error)
	require.NoError(t, model.DB.First(&policy, policy.Id).Error)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalCleanup := walletAutoRechargeIssuedBillingKeyRevoker
	originalCharger := walletAutoRechargeTossCharger
	issueCalls := 0
	cleanupCalls := 0
	chargeCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		require.Equal(t, "stale-wallet-auth", authKey)
		require.Equal(t, policy.AuthTradeNo, idempotencyKey)
		issued := &tossBillingIssueResponse{BillingKey: "stale-wallet-provider-key", CustomerKey: customerKey}
		issued.Card.Company = "card"
		issued.Card.Number = "****1234"
		return issued, http.StatusOK, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(ctx context.Context, userID int, tradeNo, customerKey string, issued *tossBillingIssueResponse, secretKey, reason string) error {
		cleanupCalls++
		require.Equal(t, user.Id, userID)
		require.Equal(t, "stale_issue_cleanup", reason)
		require.Equal(t, "stale-wallet-provider-key", issued.BillingKey)
		return nil
	}
	walletAutoRechargeTossCharger = func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*model.TossBillingChargeResult, error) {
		chargeCalls++
		return nil, errors.New("stale authorization must never charge")
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalCleanup
		walletAutoRechargeTossCharger = originalCharger
	})

	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), *policy)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, issueCalls, "cleanup may repeat only the original idempotent ISSUE")
	require.Equal(t, 1, cleanupCalls)
	require.Zero(t, chargeCalls)

	require.NoError(t, model.DB.First(&policy, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
	require.Zero(t, policy.BillingKeyId)
	require.Empty(t, policy.IssueAuthKey)
	require.False(t, policy.IssueAttempted)
}

func TestAttemptedWalletIssuePastIdempotencyWindowDoesNotReplayPOST(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 8205, Username: "expired-wallet-idempotency", Password: "x", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "expired-wallet-idempotency"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: user.Id, OwnerUserId: user.Id, CustomerKey: "expired-wallet-idempotency-customer",
		AuthTradeNo: "expired-wallet-idempotency-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "expired-wallet-idempotency-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))
	replayCutoff := model.GetDBTimestamp() - model.TossProviderIdempotencyRetentionSeconds - 1
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).
		Update("create_time", replayCutoff).Error)
	require.NoError(t, model.DB.First(policy, policy.Id).Error)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	issueCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(context.Context, string, string, string, string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, 0, errors.New("provider POST must not be replayed")
	}
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })

	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), *policy)
	require.ErrorIs(t, err, errTossBillingIssueIdempotencyWindowExpired)
	require.False(t, resolved)
	require.Zero(t, issueCalls)

	require.NoError(t, model.DB.First(policy, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelPending, policy.Status)
	require.True(t, policy.IssueAttempted)
	require.NotEmpty(t, policy.IssueAuthKey)
	require.NotEmpty(t, policy.IssueClaimToken)
}

func TestAttemptedWalletIssueRetainsSnapshotWhenIssueCredentialExpires(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 8202, Username: "expired-wallet-issue", Password: "x", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "expired-wallet-issue"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: user.Id, OwnerUserId: user.Id, CustomerKey: "expired-wallet-customer",
		AuthTradeNo: "expired-wallet-issue-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "expired-wallet-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.DB.First(policy, policy.Id).Error)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	issueCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, http.StatusUnauthorized, newTossAPIError(
			http.MethodPost,
			http.StatusUnauthorized,
			[]byte(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`),
		)
	}
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })

	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), *policy)
	require.Error(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, issueCalls)

	require.NoError(t, model.DB.First(policy, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusPending, policy.Status)
	require.NotEmpty(t, policy.IssueAuthKey)
	require.True(t, policy.IssueAttempted)
	require.NotEmpty(t, policy.IssueClaimToken,
		"an expired issue-time API key cannot prove that the earlier response-lost ISSUE created no billing key")
}
