package operation_setting

// fork: O11 — every install with no stored compliance rows shows payment
// methods without the compliance click (team 3f6ab2e33). Persisted options
// still override at load time. If upstream bumps CurrentComplianceTermsVersion,
// such installs accept it automatically: decide again then, and review the
// hard-coded 'v1' in web/src/features/system-settings/billing/index.tsx:75.
func init() {
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = CurrentComplianceTermsVersion
}
