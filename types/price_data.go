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

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	// NaN and +Inf both compare false against "<= 0" (a NaN comparison is
	// always false; +Inf is > 0), so a bare "ratio <= 0" guard lets both
	// through to poison every downstream quota multiplication.
	if !(ratio > 0) || math.IsInf(ratio, 1) {
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
