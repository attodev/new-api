package controller

import (
	"slices"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// fork: O11 — operation_setting.PayMethods carries hard-coded defaults
// (Alipay/WeChat). Expose them only when the Epay gateway is actually
// configured, so the top-up page never shows methods that cannot pay
// (team 203690366).
func visibleEpayPayMethods(complianceConfirmed bool) []map[string]string {
	if complianceConfirmed && isEpayTopUpEnabled() {
		// Clone so GetTopUpInfo's appends never write into the shared backing array.
		return slices.Clone(operation_setting.PayMethods)
	}
	return []map[string]string{}
}
