package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestWalletAutoRechargeTaskQueriesScheduledAndThresholdPolicies(t *testing.T) {
	require.Equal(t, 1*time.Minute, walletAutoRechargeTickInterval)
	require.Equal(t, 50*time.Minute, walletAutoRechargePendingAuthMaxAge)
	require.Equal(t, 8, walletAutoRechargeBatchSize)
	require.Equal(t, 8, walletAutoRechargeWorkerCount)
	require.Equal(t, 3, walletAutoRechargeMaxFails)
	require.NotNil(t, chargeWalletAutoRecharge)
	require.NotNil(t, model.ProcessWalletAutoRechargeWithConfiguredCharger)
}

func TestWalletAutoRechargeTaskRequiresSeparateContractGate(t *testing.T) {
	original := setting.GetTossConfigSnapshot()
	originalCryptoSecret := common.CryptoSecret
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Setenv("CRYPTO_SECRET", "wallet-task-contract-gate-test-at-least-32-bytes")
	common.CryptoSecret = "wallet-task-contract-gate-test-at-least-32-bytes"
	t.Cleanup(func() {
		common.CryptoSecret = originalCryptoSecret
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
		_ = setting.ApplyTossOptionValues(map[string]string{
			"TossEnabled":                   strconv.FormatBool(original.Enabled),
			"TossBillingEnabled":            strconv.FormatBool(original.BillingEnabled),
			"TossWalletAutoRechargeEnabled": strconv.FormatBool(original.WalletAutoRechargeEnabled),
			"TossTestMode":                  strconv.FormatBool(original.TestMode),
			"TossClientKey":                 original.ClientKey,
			"TossSecretKey":                 original.SecretKey,
			"TossTestClientKey":             original.TestClientKey,
			"TossTestSecretKey":             original.TestSecretKey,
			"TossBillingClientKey":          original.BillingClientKey,
			"TossBillingSecretKey":          original.BillingSecretKey,
			"TossBillingTestClientKey":      original.BillingTestClientKey,
			"TossBillingTestSecretKey":      original.BillingTestSecretKey,
			"TossUnitPrice":                 strconv.FormatFloat(original.UnitPrice, 'f', -1, 64),
			"TossMinTopUp":                  strconv.Itoa(original.MinTopUp),
		})
	})

	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "false",
		"TossTestMode":                  "false",
		"TossBillingClientKey":          "live_ck_wallet_task",
		"TossBillingSecretKey":          "live_sk_wallet_task",
		"TossUnitPrice":                 "1000",
	}))
	require.False(t, isWalletAutoRechargeTaskRunnable())

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossWalletAutoRechargeEnabled": "true",
	}))
	require.True(t, isWalletAutoRechargeTaskRunnable())
}

func TestWalletAutoRechargeTaskExpiresAbandonedAuthWhileBillingDisabled(t *testing.T) {
	setupTossBillingServiceTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	originalBillingEnabled := setting.TossBillingEnabled
	setting.TossBillingEnabled = false
	t.Cleanup(func() { setting.TossBillingEnabled = originalBillingEnabled })

	activeKey := "user:77:scheduled"
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: 77, ActiveKey: &activeKey, OwnerUserId: 77, AuthTradeNo: "wallet_auth_abandoned",
		Status: model.WalletAutoRechargeStatusPending, CustomerKey: "customer-abandoned",
		ProviderCredential: "encrypted-secret", ProviderClientKeyHash: "client-key-fingerprint",
		CreateTime: model.GetDBTimestamp() - int64(walletAutoRechargePendingAuthMaxAge/time.Second) - 1,
	}
	require.NoError(t, model.DB.Create(&policy).Error)

	walletAutoRechargeRunning.Store(false)
	runWalletAutoRechargeOnce()
	require.NoError(t, model.DB.First(&policy, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.ProviderCredential)
}

func TestWalletAutoRechargeTaskRunsIssueCleanupWhileBillingDisabled(t *testing.T) {
	setupTossBillingServiceTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	originalBillingEnabled := setting.TossBillingEnabled
	setting.TossBillingEnabled = false
	t.Cleanup(func() {
		setting.TossBillingEnabled = originalBillingEnabled
		model.SetWalletAutoRechargeIssueReconciler(nil)
	})

	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, AuthTradeNo: "wallet_cleanup_disabled",
		Status: model.WalletAutoRechargeStatusCancelPending, IssueAuthKey: "encrypted-cleanup-snapshot",
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	calls := 0
	model.SetWalletAutoRechargeIssueReconciler(func(ctx context.Context, candidate model.WalletAutoRecharge) (bool, error) {
		calls++
		require.Equal(t, policy.Id, candidate.Id)
		return true, nil
	})

	walletAutoRechargeRunning.Store(false)
	runWalletAutoRechargeOnce()
	require.Equal(t, 1, calls)
}
