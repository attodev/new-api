package types

import (
	"fmt"
	"math"
)

type GroupRatioInfo struct {
	GroupRatio         float64
	GroupSpecialRatio  float64
	HasSpecialRatio    bool
	DiscountMultiplier float64 // 적용된 할인 배수(1.0 = 할인 없음)
	DiscountSource     string  // "model" | "vendor" | "" (할인 출처)
}

type PriceData struct {
	FreeModel            bool
	ModelPrice           float64
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
	OtherRatios          map[string]float64
	UsePrice             bool
	Quota                int // MJ / Task
	QuotaToPreConsume    int
	GroupRatioInfo       GroupRatioInfo
}

// IsValidOtherRatio reports whether ratio is safe to multiply into a quota:
// finite and strictly positive. NaN and +Inf both compare false against
// "<= 0" (a NaN comparison is always false; +Inf is > 0), so a bare
// "ratio <= 0" guard alone lets both through to poison every downstream
// quota multiplication. Exported so callers that must build a whole
// OtherRatios map at once (rather than adding one key via AddOtherRatio)
// can apply the same validation before storing it.
func IsValidOtherRatio(ratio float64) bool {
	return ratio > 0 && !math.IsInf(ratio, 1)
}

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	if !IsValidOtherRatio(ratio) {
		return
	}
	if p.OtherRatios == nil {
		p.OtherRatios = make(map[string]float64)
	}
	p.OtherRatios[key] = ratio
}

func (p *PriceData) ToSetting() string {
	return fmt.Sprintf("ModelPrice: %f, ModelRatio: %f, CompletionRatio: %f, CacheRatio: %f, GroupRatio: %f, UsePrice: %t, CacheCreationRatio: %f, CacheCreation5mRatio: %f, CacheCreation1hRatio: %f, QuotaToPreConsume: %d, ImageRatio: %f, AudioRatio: %f, AudioCompletionRatio: %f", p.ModelPrice, p.ModelRatio, p.CompletionRatio, p.CacheRatio, p.GroupRatioInfo.GroupRatio, p.UsePrice, p.CacheCreationRatio, p.CacheCreation5mRatio, p.CacheCreation1hRatio, p.QuotaToPreConsume, p.ImageRatio, p.AudioRatio, p.AudioCompletionRatio)
}
