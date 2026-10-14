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

// freeModelGroupRatio is the group ratio the free-model gate reads: the
// pre-discount base, so a model/vendor discount (even 100%) never turns a
// paid model into a "free model" that skips the billing session — the
// organization and member-suspension checks still run and reserve 0. A group
// that is itself free (ratio 0) still counts as free.
func freeModelGroupRatio(info hosttypes.GroupRatioInfo) float64 {
	if info.DiscountSource == "" {
		return info.GroupRatio
	}
	return info.BaseGroupRatio
}
