package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func isPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

func isStripeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.StripeApiSecret) != "" &&
		strings.TrimSpace(setting.StripeWebhookSecret) != "" &&
		strings.TrimSpace(setting.StripePriceId) != ""
}

func isPayPalTopUpEnabled() bool {
	return strings.TrimSpace(setting.PayPalClientId) != "" &&
		strings.TrimSpace(setting.PayPalClientSecret) != "" &&
		strings.TrimSpace(setting.PayPalWebhookID) != ""
}

func isTossTopUpEnabled() bool {
	tossConfig := setting.GetTossConfigSnapshot()
	if !isTossTopUpOperationallyEnabled() {
		return false
	}
	// Regular top-ups also persist the exact API credential for ambiguous
	// confirmation recovery. Without a stable encryption key those snapshots
	// become unreadable after restart, so do not advertise or create checkouts.
	if !model.IsTossBillingCryptoConfigurationSafe() {
		return false
	}
	if tossConfig.UnitPrice <= 0 {
		return false
	}
	clientKey, secretKey := tossConfig.ClientKey, tossConfig.SecretKey
	if tossConfig.TestMode {
		clientKey, secretKey = tossConfig.TestClientKey, tossConfig.TestSecretKey
	}
	return strings.TrimSpace(clientKey) != "" &&
		strings.TrimSpace(secretKey) != "" &&
		isSafeTossRuntimeCredentialPair(clientKey, secretKey, tossConfig.TestMode) &&
		isValidServerAddress(system_setting.ServerAddress)
}

// isTossTopUpOperationallyEnabled is the server-side kill switch for any new
// provider POST. Read-only lookup and local reconciliation remain available so
// disabling payments cannot hide a charge that already completed at Toss.
func isTossTopUpOperationallyEnabled() bool {
	return isPaymentComplianceConfirmed() && setting.GetTossConfigSnapshot().Enabled
}

// The preliminary availability checks above intentionally use the in-memory
// snapshot for a fast UI response. A concurrent atomic option refresh can make
// that observation stale before a checkout handler reads the authoritative DB
// revision, so every new browser flow must also validate the snapshot returned
// by GetFreshTossConfigSnapshot before it creates any durable order/session.
func isFreshTossTopUpCheckoutEnabled(snapshot setting.TossConfigSnapshot) bool {
	return snapshot.Enabled && isPaymentComplianceConfirmed()
}

func isFreshTossBillingCheckoutEnabled(snapshot setting.TossConfigSnapshot) bool {
	return snapshot.BillingEnabled && isPaymentComplianceConfirmed()
}

func isFreshTossWalletAutoRechargeCheckoutEnabled(snapshot setting.TossConfigSnapshot) bool {
	return snapshot.BillingEnabled && snapshot.WalletAutoRechargeEnabled && isPaymentComplianceConfirmed()
}

func isTossBillingEnabled() bool {
	tossConfig := setting.GetTossConfigSnapshot()
	// Toss recurring billing requires a separate Toss contract/MID capability; keep it
	// opt-in even when normal Toss top-up is enabled.
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !tossConfig.BillingEnabled {
		return false
	}
	if !model.IsTossBillingCryptoConfigurationSafe() {
		return false
	}
	if tossConfig.UnitPrice <= 0 {
		return false
	}
	clientKey, secretKey := tossConfig.BillingClientKey, tossConfig.BillingSecretKey
	if tossConfig.TestMode {
		clientKey, secretKey = tossConfig.BillingTestClientKey, tossConfig.BillingTestSecretKey
	}
	return strings.TrimSpace(clientKey) != "" &&
		strings.TrimSpace(secretKey) != "" &&
		isSafeTossRuntimeCredentialPair(clientKey, secretKey, tossConfig.TestMode) &&
		isValidServerAddress(system_setting.ServerAddress)
}

// isTossWalletAutoRechargeEnabled is deliberately stricter than subscription
// billing availability. Toss treats non-subscription automatic payments as a
// separately reviewed use case, so a recurring-subscription MID is not enough.
func isTossWalletAutoRechargeEnabled() bool {
	tossConfig := setting.GetTossConfigSnapshot()
	return tossConfig.WalletAutoRechargeEnabled && isTossBillingEnabled()
}

// isTossWalletAutoRechargeOperationallyEnabled is the final no-new-POST gate.
// It intentionally excludes credentials so an existing attempt can still use
// its encrypted credential for GET reconciliation and billing-key cleanup.
func isTossWalletAutoRechargeOperationallyEnabled() bool {
	return model.IsTossWalletAutoRechargeOperationallyEnabled()
}

// isSafeTossRuntimeCredentialPair protects legacy rows that predate atomic
// option validation. In particular, a secret accidentally persisted in a
// client-key column must never be returned to payment-page JavaScript.
func isSafeTossRuntimeCredentialPair(clientKey, secretKey string, testMode bool) bool {
	expected := "live"
	if testMode {
		expected = "test"
	}
	// Legacy rows bypassed the atomic settings validator. Require the same
	// official API-individual ck/sk contract at the final runtime boundary so an
	// unknown or secret-like value can never be emitted as a browser client key.
	return validateTossKeyPairEnvironment(clientKey, secretKey, expected) == nil
}

func isStripeWebhookConfigured() bool {
	return strings.TrimSpace(setting.StripeWebhookSecret) != ""
}

func isStripeWebhookEnabled() bool {
	return isStripeTopUpEnabled()
}

func isCreemTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(setting.CreemProducts)
	return strings.TrimSpace(setting.CreemApiKey) != "" &&
		products != "" &&
		products != "[]"
}

func isCreemWebhookConfigured() bool {
	return strings.TrimSpace(setting.CreemWebhookSecret) != ""
}

func isCreemWebhookEnabled() bool {
	return isCreemTopUpEnabled() && isCreemWebhookConfigured()
}

func isWaffoTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !setting.WaffoEnabled {
		return false
	}

	return isWaffoWebhookConfigured()
}

func isWaffoWebhookConfigured() bool {
	if setting.WaffoSandbox {
		return strings.TrimSpace(setting.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPublicCert) != ""
	}

	return strings.TrimSpace(setting.WaffoApiKey) != "" &&
		strings.TrimSpace(setting.WaffoPrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPublicCert) != ""
}

func isWaffoWebhookEnabled() bool {
	return isWaffoTopUpEnabled()
}

func isWaffoPancakeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	// Presence-of-credentials = enabled. Webhook public keys ship inside
	// the SDK; mode (test/prod) is read from each event.
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPancakeProductID) != ""
}

func isWaffoPancakeWebhookConfigured() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isWaffoPancakeWebhookEnabled() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isEpayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && len(operation_setting.PayMethods) > 0
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(operation_setting.PayAddress) != "" &&
		strings.TrimSpace(operation_setting.EpayId) != "" &&
		strings.TrimSpace(operation_setting.EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayTopUpEnabled()
}
