package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestFreshTossCheckoutGatesRejectDisabledAuthoritativeSnapshot(t *testing.T) {
	paymentSetting := operation_setting.GetPaymentSetting()
	originalCompliance := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	originalConfig := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalCompliance
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
		restoreTossConfigForOptionTest(t, originalConfig)
	})

	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
	}))

	// Reproduce the meaningful part of the race: the preliminary in-memory
	// observation was enabled, while the freshly attested DB snapshot returned
	// to this request already contains an administrator's disable.
	staleRuntime := setting.GetTossConfigSnapshot()
	require.True(t, staleRuntime.Enabled)
	require.True(t, staleRuntime.BillingEnabled)
	require.True(t, staleRuntime.WalletAutoRechargeEnabled)

	topUpDisabled := staleRuntime
	topUpDisabled.Enabled = false
	require.False(t, isFreshTossTopUpCheckoutEnabled(topUpDisabled))

	billingDisabled := staleRuntime
	billingDisabled.BillingEnabled = false
	require.False(t, isFreshTossBillingCheckoutEnabled(billingDisabled))
	require.False(t, isFreshTossWalletAutoRechargeCheckoutEnabled(billingDisabled))

	walletDisabled := staleRuntime
	walletDisabled.WalletAutoRechargeEnabled = false
	require.False(t, isFreshTossWalletAutoRechargeCheckoutEnabled(walletDisabled))
}

func TestFreshTossCheckoutGatesRecheckCompliance(t *testing.T) {
	paymentSetting := operation_setting.GetPaymentSetting()
	originalCompliance := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalCompliance
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})

	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	snapshot := setting.TossConfigSnapshot{
		Enabled:                   true,
		BillingEnabled:            true,
		WalletAutoRechargeEnabled: true,
	}

	require.False(t, isFreshTossTopUpCheckoutEnabled(snapshot))
	require.False(t, isFreshTossBillingCheckoutEnabled(snapshot))
	require.False(t, isFreshTossWalletAutoRechargeCheckoutEnabled(snapshot))
}
