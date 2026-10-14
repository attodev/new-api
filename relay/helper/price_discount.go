package helper

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// foldDiscountIntoGroupRatio applies the effective model/vendor discount once,
// at the single point every billing path (pre-consume, settlement, tiered,
// per-call, retry) reads GroupRatioInfo from. The discount is looked up by the
// origin model name first (the marketplace key) and then by the billing model
// name.
func foldDiscountIntoGroupRatio(info *hosttypes.GroupRatioInfo, originName, billingName string) {
	info.BaseGroupRatio = info.GroupRatio
	mul, source := ratio_setting.GetEffectiveDiscountFor(originName, billingName)
	info.DiscountMultiplier = mul
	if mul == 1 {
		return
	}
	info.DiscountSource = source
	info.GroupRatio *= mul
}
