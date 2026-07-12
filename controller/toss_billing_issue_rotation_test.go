package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionBillingIssueDefinitiveOldKeyRejectionPromotesBeforeRetry(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.Log{},
	))

	const (
		userID        = 801
		planID        = 8801
		tradeNo       = "toss_sub_issue_rotate_controller"
		customerKey   = "cust_issue_rotate_controller"
		authKey       = "auth_issue_rotate_controller"
		clientKey     = "live_ck_issue_rotate_controller"
		oldSecret     = "live_sk_issue_rotate_controller_old"
		rotatedSecret = "live_sk_issue_rotate_controller_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "issue-rotate-controller", Status: common.UserStatusEnabled, TossCustomerKey: customerKey,
	}).Error)
	plan := model.SubscriptionPlan{
		Id: planID, Title: "Issue credential rotation", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: userID, PlanId: planID, Money: plan.PriceAmount, TradeNo: tradeNo,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
		ProviderAmount: 15000, ProviderCurrency: "KRW", ProviderCredential: credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(clientKey),
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	claimToken, claimed, err := model.ClaimTossSubscriptionBillingIssue(tradeNo, authKey, customerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	storedAuthKey, storedCustomerKey, storedSecretKey, err := model.GetClaimedTossSubscriptionBillingIssue(tradeNo, claimToken)
	require.NoError(t, err)

	originalTestMode := setting.TossTestMode
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalIssuer := subscriptionTossBillingKeyIssuer
	t.Cleanup(func() {
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
		subscriptionTossBillingKeyIssuer = originalIssuer
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret

	issuerSecrets := make([]string, 0, 2)
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.Equal(t, authKey, gotAuthKey)
		require.Equal(t, customerKey, gotCustomerKey)
		require.Equal(t, tradeNo, idempotencyKey)
		issuerSecrets = append(issuerSecrets, secretKey)
		if len(issuerSecrets) == 1 {
			require.Equal(t, oldSecret, secretKey)
			return nil, http.StatusUnauthorized, &tossAPIError{
				Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
			}
		}
		require.Equal(t, rotatedSecret, secretKey)
		var persisted model.SubscriptionOrder
		require.NoError(t, model.DB.First(&persisted, order.Id).Error)
		persistedSecret, decryptErr := model.DecryptProviderCredential(persisted.ProviderCredential)
		require.NoError(t, decryptErr)
		require.Equal(t, rotatedSecret, persistedSecret, "the retry namespace must be durable before the provider POST")
		require.True(t, persisted.BillingIssueAttempted, "the promoted namespace needs its own durable POST intent")
		return nil, 0, context.DeadlineExceeded
	}

	resolved, retainClaim, err := processClaimedTossSubscriptionBillingIssue(
		context.Background(),
		&order,
		&plan,
		claimToken,
		storedAuthKey,
		storedCustomerKey,
		storedSecretKey,
	)
	require.False(t, resolved)
	require.True(t, retainClaim)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{oldSecret, rotatedSecret}, issuerSecrets)

	var persisted model.SubscriptionOrder
	require.NoError(t, model.DB.First(&persisted, order.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, rotatedSecret, persistedSecret)
	require.True(t, persisted.BillingIssueAttempted)
	require.Equal(t, claimToken, persisted.BillingClaimToken)
}

func TestSubscriptionBillingIssuePriorAttemptNeverCrossesCredentialNamespace(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.Log{},
	))

	const (
		userID        = 802
		planID        = 8802
		tradeNo       = "toss_sub_issue_rotate_prior"
		customerKey   = "cust_issue_rotate_prior"
		authKey       = "auth_issue_rotate_prior"
		clientKey     = "live_ck_issue_rotate_prior"
		oldSecret     = "live_sk_issue_rotate_prior_old"
		rotatedSecret = "live_sk_issue_rotate_prior_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "issue-rotate-prior", Status: common.UserStatusEnabled, TossCustomerKey: customerKey,
	}).Error)
	plan := model.SubscriptionPlan{
		Id: planID, Title: "Prior issue credential", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	encryptedAuthKey, err := common.EncryptString(authKey)
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: userID, PlanId: planID, Money: plan.PriceAmount, TradeNo: tradeNo,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
		ProviderAmount: 15000, ProviderCurrency: "KRW", ProviderCredential: credential,
		ProviderClientKeyHash:   model.TossBillingClientKeyFingerprint(clientKey),
		BillingClaimToken:       "prior-claim",
		BillingClaimTime:        model.GetDBTimestamp(),
		BillingIssueAuthKey:     encryptedAuthKey,
		BillingIssueAuthKeyHash: common.GenerateHMAC(authKey),
		BillingIssueCustomerKey: customerKey,
		BillingIssueAttempted:   true,
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)

	originalTestMode := setting.TossTestMode
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalIssuer := subscriptionTossBillingKeyIssuer
	t.Cleanup(func() {
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
		subscriptionTossBillingKeyIssuer = originalIssuer
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret
	issuerCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		require.Equal(t, oldSecret, secretKey)
		return nil, http.StatusUnauthorized, &tossAPIError{
			Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
		}
	}

	resolved, retainClaim, err := processClaimedTossSubscriptionBillingIssue(
		context.Background(),
		&order,
		&plan,
		order.BillingClaimToken,
		authKey,
		customerKey,
		oldSecret,
	)
	require.False(t, resolved)
	require.True(t, retainClaim)
	require.Error(t, err)
	require.Equal(t, 1, issuerCalls)

	var persisted model.SubscriptionOrder
	require.NoError(t, model.DB.First(&persisted, order.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
	require.True(t, persisted.BillingIssueAttempted)
}

func TestWalletBillingIssueDefinitiveOldKeyRejectionPromotesBeforeRetry(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	const (
		userID        = 803
		tradeNo       = "wallet_auto_issue_rotate_controller"
		customerKey   = "cust_wallet_issue_rotate_controller"
		authKey       = "auth_wallet_issue_rotate_controller"
		clientKey     = "live_ck_wallet_auto_billing"
		oldSecret     = "live_sk_wallet_issue_rotate_controller_old"
		rotatedSecret = "live_sk_wallet_issue_rotate_controller_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "wallet-issue-rotate", Status: common.UserStatusEnabled, Group: "default", AffCode: "wallet-issue-rotate",
	}).Error)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: userID, OwnerUserId: userID, CustomerKey: customerKey, AuthTradeNo: tradeNo,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(clientKey),
		Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(tradeNo, authKey, customerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	storedAuthKey, storedCustomerKey, storedSecretKey, err := model.GetClaimedWalletAutoRechargeBillingIssue(tradeNo, claimToken)
	require.NoError(t, err)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret
	issuerSecrets := make([]string, 0, 2)
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.Equal(t, authKey, gotAuthKey)
		require.Equal(t, customerKey, gotCustomerKey)
		require.Equal(t, tradeNo, idempotencyKey)
		issuerSecrets = append(issuerSecrets, secretKey)
		if len(issuerSecrets) == 1 {
			require.Equal(t, oldSecret, secretKey)
			return nil, http.StatusUnauthorized, &tossAPIError{
				Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
			}
		}
		require.Equal(t, rotatedSecret, secretKey)
		var persisted model.WalletAutoRecharge
		require.NoError(t, model.DB.First(&persisted, policy.Id).Error)
		persistedSecret, decryptErr := model.DecryptProviderCredential(persisted.ProviderCredential)
		require.NoError(t, decryptErr)
		require.Equal(t, rotatedSecret, persistedSecret, "the retry namespace must be durable before the provider POST")
		require.True(t, persisted.IssueAttempted, "the promoted namespace needs its own durable POST intent")
		return nil, 0, context.DeadlineExceeded
	}

	resolved, retainClaim, err := processClaimedWalletAutoRechargeBillingIssue(
		context.Background(),
		policy,
		claimToken,
		storedAuthKey,
		storedCustomerKey,
		storedSecretKey,
	)
	require.False(t, resolved)
	require.True(t, retainClaim)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{oldSecret, rotatedSecret}, issuerSecrets)

	var persisted model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&persisted, policy.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, rotatedSecret, persistedSecret)
	require.True(t, persisted.IssueAttempted)
	require.Equal(t, claimToken, persisted.IssueClaimToken)
}

func TestWalletBillingIssuePriorAttemptNeverCrossesCredentialNamespace(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	const (
		userID        = 804
		tradeNo       = "wallet_auto_issue_rotate_prior"
		customerKey   = "cust_wallet_issue_rotate_prior"
		authKey       = "auth_wallet_issue_rotate_prior"
		clientKey     = "live_ck_wallet_auto_billing"
		oldSecret     = "live_sk_wallet_issue_rotate_prior_old"
		rotatedSecret = "live_sk_wallet_issue_rotate_prior_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "wallet-issue-prior", Status: common.UserStatusEnabled, Group: "default", AffCode: "wallet-issue-prior",
	}).Error)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: userID, OwnerUserId: userID, CustomerKey: customerKey, AuthTradeNo: tradeNo,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(clientKey),
		Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(tradeNo, authKey, customerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, claimToken))
	require.NoError(t, model.DB.First(policy, policy.Id).Error)
	storedAuthKey, storedCustomerKey, storedSecretKey, err := model.GetClaimedWalletAutoRechargeBillingIssue(tradeNo, claimToken)
	require.NoError(t, err)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret
	issuerCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		require.Equal(t, oldSecret, secretKey)
		return nil, http.StatusUnauthorized, &tossAPIError{
			Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
		}
	}

	resolved, retainClaim, err := processClaimedWalletAutoRechargeBillingIssue(
		context.Background(),
		policy,
		claimToken,
		storedAuthKey,
		storedCustomerKey,
		storedSecretKey,
	)
	require.False(t, resolved)
	require.True(t, retainClaim)
	require.Error(t, err)
	require.Equal(t, 1, issuerCalls)

	var persisted model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&persisted, policy.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
	require.True(t, persisted.IssueAttempted)
}

func TestSubscriptionBillingIssueReloadsAttemptedStateAfterClaimRace(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.Log{},
	))

	const (
		userID        = 805
		planID        = 8805
		tradeNo       = "toss_sub_issue_attempted_claim_race"
		customerKey   = "cust_issue_attempted_claim_race"
		authKey       = "auth_issue_attempted_claim_race"
		clientKey     = "live_ck_issue_attempted_claim_race"
		oldSecret     = "live_sk_issue_attempted_claim_race_old"
		rotatedSecret = "live_sk_issue_attempted_claim_race_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "issue-attempted-claim-race", Status: common.UserStatusEnabled, TossCustomerKey: customerKey,
	}).Error)
	plan := model.SubscriptionPlan{
		Id: planID, Title: "Attempted claim race", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: userID, PlanId: planID, Money: plan.PriceAmount, TradeNo: tradeNo,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
		ProviderAmount: 15000, ProviderCurrency: "KRW", ProviderCredential: credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(clientKey),
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	firstToken, claimed, err := model.ClaimTossSubscriptionBillingIssue(tradeNo, authKey, customerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.ReleaseTossSubscriptionBillingClaim(tradeNo, firstToken))
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.False(t, order.BillingIssueAttempted)
	injectAttemptedAfterNextClaimUpdate(t, "subscription_orders", order.Id, "billing_issue_attempted")

	originalIssuer := subscriptionTossBillingKeyIssuer
	originalTestMode := setting.TossTestMode
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	t.Cleanup(func() {
		subscriptionTossBillingKeyIssuer = originalIssuer
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret
	issuerCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		require.Equal(t, oldSecret, secretKey)
		return nil, http.StatusUnauthorized, &tossAPIError{
			Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
		}
	}

	resolved, err := reconcileTossSubscriptionBillingIssue(context.Background(), &order)
	require.False(t, resolved)
	require.Error(t, err)
	require.Equal(t, 1, issuerCalls, "fresh attempted state must block cross-credential fallback")
	var persisted model.SubscriptionOrder
	require.NoError(t, model.DB.First(&persisted, order.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
	require.True(t, persisted.BillingIssueAttempted)
	require.NotEmpty(t, persisted.BillingClaimToken)
}

func TestWalletBillingIssueReloadsAttemptedStateAfterClaimRace(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	const (
		userID        = 806
		tradeNo       = "wallet_auto_issue_attempted_claim_race"
		customerKey   = "cust_wallet_issue_attempted_claim_race"
		authKey       = "auth_wallet_issue_attempted_claim_race"
		clientKey     = "live_ck_wallet_auto_billing"
		oldSecret     = "live_sk_wallet_issue_attempted_claim_race_old"
		rotatedSecret = "live_sk_wallet_issue_attempted_claim_race_new"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "wallet-issue-attempted-race", Status: common.UserStatusEnabled, Group: "default", AffCode: "wallet-issue-attempted-race",
	}).Error)
	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: userID, OwnerUserId: userID, CustomerKey: customerKey, AuthTradeNo: tradeNo,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(clientKey),
		Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	firstToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(tradeNo, authKey, customerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(tradeNo, firstToken))
	require.NoError(t, model.DB.First(policy, policy.Id).Error)
	require.False(t, policy.IssueAttempted)
	injectAttemptedAfterNextClaimUpdate(t, "wallet_auto_recharges", policy.Id, "issue_attempted")

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })
	setting.TossBillingClientKey = clientKey
	setting.TossBillingSecretKey = rotatedSecret
	issuerCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, gotAuthKey, gotCustomerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		require.Equal(t, oldSecret, secretKey)
		return nil, http.StatusUnauthorized, &tossAPIError{
			Method: http.MethodPost, StatusCode: http.StatusUnauthorized, Code: "UNAUTHORIZED_KEY",
		}
	}

	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), *policy)
	require.False(t, resolved)
	require.Error(t, err)
	require.Equal(t, 1, issuerCalls, "fresh attempted state must block cross-credential fallback")
	var persisted model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&persisted, policy.Id).Error)
	persistedSecret, err := model.DecryptProviderCredential(persisted.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, persistedSecret)
	require.True(t, persisted.IssueAttempted)
	require.NotEmpty(t, persisted.IssueClaimToken)
}
