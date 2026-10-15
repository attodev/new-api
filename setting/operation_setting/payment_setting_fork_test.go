package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// restorePaymentCompliance snapshots the compliance fields and restores them
// when the test ends, so tests that flip the global do not leak.
func restorePaymentCompliance(t *testing.T) {
	t.Helper()
	confirmed := paymentSetting.ComplianceConfirmed
	version := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = confirmed
		paymentSetting.ComplianceTermsVersion = version
	})
}

func TestPaymentComplianceConfirmedByDefaultFork(t *testing.T) {
	setting := GetPaymentSetting()

	require.True(t, setting.ComplianceConfirmed)
	require.Equal(t, CurrentComplianceTermsVersion, setting.ComplianceTermsVersion)
	require.True(t, IsPaymentComplianceConfirmed())
}

func TestPaymentComplianceDefaultYieldsToPersistedOptionFork(t *testing.T) {
	restorePaymentCompliance(t)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"payment_setting.compliance_confirmed": "false",
	}))

	require.False(t, IsPaymentComplianceConfirmed())
}

func TestPaymentComplianceRejectsOutdatedTermsVersionFork(t *testing.T) {
	restorePaymentCompliance(t)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"payment_setting.compliance_confirmed":     "true",
		"payment_setting.compliance_terms_version": "outdated",
	}))

	require.False(t, IsPaymentComplianceConfirmed())
}
