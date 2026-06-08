package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentComplianceDefaultIsConfirmed(t *testing.T) {
	setting := GetPaymentSetting()

	require.True(t, setting.ComplianceConfirmed)
	require.Equal(t, CurrentComplianceTermsVersion, setting.ComplianceTermsVersion)
	require.True(t, IsPaymentComplianceConfirmed())
}

func TestPaymentComplianceRejectsOutdatedTermsVersion(t *testing.T) {
	setting := GetPaymentSetting()
	originalConfirmed := setting.ComplianceConfirmed
	originalTermsVersion := setting.ComplianceTermsVersion
	t.Cleanup(func() {
		setting.ComplianceConfirmed = originalConfirmed
		setting.ComplianceTermsVersion = originalTermsVersion
	})

	setting.ComplianceConfirmed = true
	setting.ComplianceTermsVersion = "outdated"

	require.False(t, IsPaymentComplianceConfirmed())
}
