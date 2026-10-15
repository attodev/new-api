package operation_setting

// fork: O11 — a fresh install shows payment methods without the compliance
// click (team 3f6ab2e33). Persisted options still override at load time.
func init() {
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = CurrentComplianceTermsVersion
}
