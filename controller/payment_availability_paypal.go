package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// fork: O2 — PayPal availability, same compliance gate as the other providers.
func isPayPalTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.PayPalClientId) != "" &&
		strings.TrimSpace(setting.PayPalClientSecret) != "" &&
		strings.TrimSpace(setting.PayPalWebhookID) != ""
}

func isPayPalWebhookEnabled() bool { return isPayPalTopUpEnabled() }

// appendPayPalPayMethod adds the PayPal entry to GetTopUpInfo's pay method list.
func appendPayPalPayMethod(payMethods []map[string]string) []map[string]string {
	if !isPayPalTopUpEnabled() {
		return payMethods
	}
	for _, method := range payMethods {
		if method["type"] == model.PaymentMethodPayPal {
			return payMethods
		}
	}
	return append(payMethods, map[string]string{
		"name":      "PayPal",
		"type":      model.PaymentMethodPayPal,
		"color":     "#003087",
		"min_topup": strconv.Itoa(setting.PayPalMinTopUp),
	})
}
