package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func injectAttemptedAfterNextClaimUpdate(t *testing.T, table string, id int, column string) {
	t.Helper()
	callbackName := fmt.Sprintf("test:mark-attempted-after-claim:%s:%d", table, id)
	var injected atomic.Bool
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if injected.CompareAndSwap(false, true) {
			if _, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context, fmt.Sprintf("UPDATE %s SET %s = ? WHERE id = ?", table, column), true, id); err != nil {
				tx.AddError(err)
			}
		}
	}))
}

func injectCorruptAttemptedSnapshotAfterNextClaimUpdate(t *testing.T, table string, id int, attemptedColumn, authColumn string) {
	t.Helper()
	callbackName := fmt.Sprintf("test:corrupt-attempted-after-claim:%s:%d", table, id)
	var injected atomic.Bool
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if injected.CompareAndSwap(false, true) {
			query := fmt.Sprintf("UPDATE %s SET %s = ?, %s = ? WHERE id = ?", table, attemptedColumn, authColumn)
			if _, err := tx.Statement.ConnPool.ExecContext(tx.Statement.Context, query, true, "corrupt-auth-ciphertext", id); err != nil {
				tx.AddError(err)
			}
		}
	}))
}

func TestCorruptSubscriptionBillingIssueSnapshotPreservesOnlyAttemptedRequest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attempted bool
	}{
		{name: "attempted snapshot is retained", attempted: true},
		{name: "unattempted snapshot is expired", attempted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTossBillingControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}))
			tradeNo := "toss_sub_corrupt_issue_attempted"
			if !tc.attempted {
				tradeNo = "toss_sub_corrupt_issue_unattempted"
			}
			providerCredential, err := model.EncryptProviderCredential("live_sk_corrupt_subscription_issue")
			require.NoError(t, err)
			order := model.SubscriptionOrder{
				UserId: 811, PlanId: 8811, TradeNo: tradeNo,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusPending, CreateTime: model.GetDBTimestamp(),
				ProviderCredential:    providerCredential,
				ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_corrupt_subscription_issue"),
				BillingIssueAuthKey:   "corrupt-auth-ciphertext", BillingIssueAuthKeyHash: "corrupt-auth-hash",
				BillingIssueCustomerKey: "cust_corrupt_subscription_issue", BillingIssueAttempted: false,
			}
			require.NoError(t, model.DB.Create(&order).Error)
			if tc.attempted {
				// The candidate passed below is unattempted. Simulate another node
				// completing MarkAttempted/Release immediately after this worker's
				// pre-claim read but before its claim UPDATE returns.
				injectAttemptedAfterNextClaimUpdate(t, "subscription_orders", order.Id, "billing_issue_attempted")
			}

			resolved, err := reconcileTossSubscriptionBillingIssue(context.Background(), &order)
			var stored model.SubscriptionOrder
			require.NoError(t, model.DB.First(&stored, order.Id).Error)
			if tc.attempted {
				require.False(t, resolved)
				require.ErrorIs(t, err, model.ErrTossBillingIssueSnapshotInvalid)
				require.Equal(t, common.TopUpStatusPending, stored.Status)
				require.True(t, stored.BillingIssueAttempted)
				require.Equal(t, "corrupt-auth-ciphertext", stored.BillingIssueAuthKey)
				require.NotEmpty(t, stored.BillingClaimToken, "the reconciliation claim should remain leased for operator-visible recovery")
				require.NotEmpty(t, stored.ProviderCredential)
				require.NotEmpty(t, stored.ProviderClientKeyHash)
			} else {
				require.True(t, resolved)
				require.NoError(t, err)
				require.Equal(t, common.TopUpStatusExpired, stored.Status)
				require.False(t, stored.BillingIssueAttempted)
				require.Empty(t, stored.BillingIssueAuthKey)
				require.Empty(t, stored.BillingClaimToken)
				require.Empty(t, stored.ProviderCredential)
				require.Empty(t, stored.ProviderClientKeyHash)
			}
		})
	}
}

func TestCorruptWalletBillingIssueSnapshotPreservesOnlyAttemptedRequest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attempted bool
	}{
		{name: "attempted snapshot is retained", attempted: true},
		{name: "unattempted snapshot is cancelled", attempted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWalletAutoRechargeControllerTestDB(t)
			tradeNo := "wallet_auto_corrupt_issue_attempted"
			targetID := 812
			if !tc.attempted {
				tradeNo = "wallet_auto_corrupt_issue_unattempted"
				targetID = 813
			}
			providerCredential, err := model.EncryptProviderCredential("live_sk_corrupt_wallet_issue")
			require.NoError(t, err)
			activeKey := "user:" + tradeNo
			policy := model.WalletAutoRecharge{
				Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
				TargetId: targetID, OwnerUserId: targetID, ActiveKey: &activeKey,
				Status: model.WalletAutoRechargeStatusPending, CustomerKey: "cust_corrupt_wallet_issue", AuthTradeNo: tradeNo,
				ProviderCredential:    providerCredential,
				ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_corrupt_wallet_issue"),
				Amount:                10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
				IssueAuthKey: "corrupt-auth-ciphertext", IssueAuthKeyHash: "corrupt-auth-hash",
				IssueCustomerKey: "cust_corrupt_wallet_issue", IssueAttempted: false,
				CreateTime: model.GetDBTimestamp(), UpdateTime: model.GetDBTimestamp(),
			}
			require.NoError(t, model.DB.Create(&policy).Error)
			if tc.attempted {
				injectAttemptedAfterNextClaimUpdate(t, "wallet_auto_recharges", policy.Id, "issue_attempted")
			}

			resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), policy)
			var stored model.WalletAutoRecharge
			require.NoError(t, model.DB.First(&stored, policy.Id).Error)
			if tc.attempted {
				require.False(t, resolved)
				require.ErrorIs(t, err, model.ErrTossBillingIssueSnapshotInvalid)
				require.Equal(t, model.WalletAutoRechargeStatusPending, stored.Status)
				require.True(t, stored.IssueAttempted)
				require.Equal(t, "corrupt-auth-ciphertext", stored.IssueAuthKey)
				require.NotEmpty(t, stored.IssueClaimToken, "the reconciliation claim should remain leased for operator-visible recovery")
				require.NotEmpty(t, stored.ProviderCredential)
				require.NotEmpty(t, stored.ProviderClientKeyHash)
			} else {
				require.True(t, resolved)
				require.NoError(t, err)
				require.Equal(t, model.WalletAutoRechargeStatusCancelled, stored.Status)
				require.False(t, stored.IssueAttempted)
				require.Empty(t, stored.IssueAuthKey)
				require.Empty(t, stored.IssueClaimToken)
				require.Empty(t, stored.ProviderCredential)
				require.Empty(t, stored.ProviderClientKeyHash)
			}
		})
	}
}

func TestSubscriptionBillingConfirmReloadsAttemptedStateAfterClaimRace(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}))
	const (
		userID      = 814
		planID      = 8814
		tradeNo     = "toss_sub_browser_attempted_claim_race"
		customerKey = "cust_sub_browser_attempted_claim_race"
	)
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "sub-browser-attempted-race", Status: common.UserStatusEnabled, TossCustomerKey: customerKey,
	}).Error)
	plan := model.SubscriptionPlan{
		Id: planID, Title: "Browser attempted race", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	providerCredential, err := model.EncryptProviderCredential("live_sk_sub_browser_attempted_race")
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: userID, PlanId: planID, Money: plan.PriceAmount, TradeNo: tradeNo,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: model.GetDBTimestamp(),
		ProviderAmount: 15000, ProviderCurrency: "KRW", ProviderCredential: providerCredential,
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	injectCorruptAttemptedSnapshotAfterNextClaimUpdate(
		t,
		"subscription_orders",
		order.Id,
		"billing_issue_attempted",
		"billing_issue_auth_key",
	)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/subscription/toss/confirm?trade_no="+tradeNo+"&authKey=auth_sub_browser_attempted_race&customerKey="+customerKey,
		nil,
	)
	SubscriptionTossBillingConfirm(c)

	var stored model.SubscriptionOrder
	require.NoError(t, model.DB.First(&stored, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.True(t, stored.BillingIssueAttempted)
	require.Equal(t, "corrupt-auth-ciphertext", stored.BillingIssueAuthKey)
	require.NotEmpty(t, stored.BillingClaimToken)
}

func TestWalletBillingConfirmReloadsAttemptedStateAfterClaimRace(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	const (
		tradeNo     = "wallet_auto_browser_attempted_claim_race"
		customerKey = "cust_wallet_browser_attempted_claim_race"
	)
	providerCredential, err := model.EncryptProviderCredential("live_sk_wallet_browser_attempted_race")
	require.NoError(t, err)
	activeKey := "user:" + tradeNo
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: 815, OwnerUserId: 815, ActiveKey: &activeKey,
		Status: model.WalletAutoRechargeStatusPending, CustomerKey: customerKey, AuthTradeNo: tradeNo,
		ProviderCredential: providerCredential,
		Amount:             10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		CreateTime: model.GetDBTimestamp(), UpdateTime: model.GetDBTimestamp(),
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	injectCorruptAttemptedSnapshotAfterNextClaimUpdate(
		t,
		"wallet_auto_recharges",
		policy.Id,
		"issue_attempted",
		"issue_auth_key",
	)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no="+tradeNo+"&authKey=auth_wallet_browser_attempted_race&customerKey="+customerKey,
		nil,
	)
	WalletAutoRechargeTossConfirm(c)

	var stored model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&stored, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusPending, stored.Status)
	require.True(t, stored.IssueAttempted)
	require.Equal(t, "corrupt-auth-ciphertext", stored.IssueAuthKey)
	require.NotEmpty(t, stored.IssueClaimToken)
}
