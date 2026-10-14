package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// appendDiscountInfo records the model/vendor discount separately from the
// group ratio so the usage log does not show the discount as a group discount.
// group_ratio is rewritten to the pre-discount base; a 0 multiplier (100%
// discount) is recorded as-is and never used as a divisor.
func appendDiscountInfo(other *model.LogOther, relayInfo *relaycommon.RelayInfo) {
	if other == nil || relayInfo == nil {
		return
	}
	g := relayInfo.PriceData.GroupRatioInfo
	if g.DiscountSource == "" || g.DiscountMultiplier == 1 {
		return
	}
	other.SetPublic("discount_multiplier", g.DiscountMultiplier)
	other.SetPublic("discount_source", g.DiscountSource)
	other.SetPublic("group_ratio", discountBaseGroupRatio(relayInfo))
}

// discountBaseGroupRatio is the group ratio shown in logs: the pre-discount
// base when a discount was folded in, otherwise the group ratio itself (also
// for PriceData not built by HandleGroupRatio, where BaseGroupRatio is unset).
func discountBaseGroupRatio(relayInfo *relaycommon.RelayInfo) float64 {
	g := relayInfo.PriceData.GroupRatioInfo
	if g.DiscountSource == "" {
		return g.GroupRatio
	}
	return g.BaseGroupRatio
}
