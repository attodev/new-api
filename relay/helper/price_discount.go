package helper

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// foldDiscountIntoGroupRatio applies the effective model/vendor discount once,
// at the single point every billing path (pre-consume, settlement, tiered,
// per-call, retry) reads GroupRatioInfo from.
func foldDiscountIntoGroupRatio(info *hosttypes.GroupRatioInfo, modelName string) {
	info.BaseGroupRatio = info.GroupRatio
	mul := ratio_setting.GetEffectiveDiscountMultiplier(modelName)
	info.DiscountMultiplier = mul
	if mul == 1 {
		return
	}
	if _, ok := ratio_setting.GetModelDiscountMultiplier(modelName); ok {
		info.DiscountSource = "model"
	} else {
		info.DiscountSource = "vendor"
	}
	info.GroupRatio *= mul
}
