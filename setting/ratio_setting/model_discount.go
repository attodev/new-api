package ratio_setting

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// 모델명/벤더명 -> 할인율 percent(0~100). 내부 배수 = (100-percent)/100.
var modelDiscountMap = types.NewRWMap[string, float64]()
var vendorDiscountMap = types.NewRWMap[string, float64]()

func ModelDiscount2JSONString() string  { return modelDiscountMap.MarshalJSONString() }
func VendorDiscount2JSONString() string { return vendorDiscountMap.MarshalJSONString() }

func GetModelDiscountCopy() map[string]float64  { return modelDiscountMap.ReadAll() }
func GetVendorDiscountCopy() map[string]float64 { return vendorDiscountMap.ReadAll() }

func validateDiscountJSON(jsonStr string) error {
	m := make(map[string]float64)
	if err := common.UnmarshalJsonStr(jsonStr, &m); err != nil {
		return err
	}
	for name, percent := range m {
		if percent < 0 || percent > 100 {
			return errors.New("discount percent for '" + name + "' must be between 0 and 100")
		}
	}
	return nil
}

func UpdateModelDiscountByJSONString(jsonStr string) error {
	if err := validateDiscountJSON(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonString(modelDiscountMap, jsonStr)
}

func UpdateVendorDiscountByJSONString(jsonStr string) error {
	if err := validateDiscountJSON(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonString(vendorDiscountMap, jsonStr)
}

func percentToMultiplier(percent float64) float64 {
	return (100 - percent) / 100
}

// GetModelDiscountMultiplier: 모델명에 할인이 설정돼 있으면 (배수, true).
func GetModelDiscountMultiplier(modelName string) (float64, bool) {
	percent, ok := modelDiscountMap.Get(FormatMatchingModelName(modelName))
	if !ok {
		return 1, false
	}
	return percentToMultiplier(percent), true
}

// GetVendorDiscountMultiplier: 벤더명에 할인이 설정돼 있으면 (배수, true).
func GetVendorDiscountMultiplier(vendorName string) (float64, bool) {
	percent, ok := vendorDiscountMap.Get(vendorName)
	if !ok {
		return 1, false
	}
	return percentToMultiplier(percent), true
}

// GetModelDiscountPercent: 저장된 모델 할인율(percent)을 반환. 미설정 시 (0,false).
func GetModelDiscountPercent(modelName string) (float64, bool) {
	return modelDiscountMap.Get(FormatMatchingModelName(modelName))
}

// GetVendorDiscountPercent: 저장된 벤더 할인율(percent)을 반환. 미설정 시 (0,false).
func GetVendorDiscountPercent(vendorName string) (float64, bool) {
	return vendorDiscountMap.Get(vendorName)
}

// vendorResolver: 모델명 -> 벤더명. model 패키지가 init 시 주입(순환 의존 회피).
var vendorResolver func(modelName string) (string, bool)

// SetVendorResolver installs the model -> vendor resolver and returns the
// previous one so tests can restore it.
func SetVendorResolver(fn func(modelName string) (string, bool)) func(modelName string) (string, bool) {
	prev := vendorResolver
	vendorResolver = fn
	return prev
}

// vendorDiscountFor: 모델이 속한 벤더의 할인 배수.
// 벤더 할인이 하나도 설정돼 있지 않으면 벤더 해석(모델→벤더 캐시/조회)을 아예
// 건너뛴다. 일반적인(할인 없는) 과금 경로의 불필요한 비용을 피하고, 벤더 resolver가
// 의존하는 pricing 캐시가 준비되지 않은 환경(예: 단위 테스트)에서의 호출도 막는다.
func vendorDiscountFor(modelName string) (float64, bool) {
	if vendorResolver == nil || vendorDiscountMap.Len() == 0 {
		return 1, false
	}
	vendor, ok := vendorResolver(modelName)
	if !ok {
		return 1, false
	}
	return GetVendorDiscountMultiplier(vendor)
}

// GetEffectiveDiscountFor resolves a request's discount. The marketplace keys
// discounts by the origin model name, so the origin name is tried first and the
// billing model name (e.g. a reasoning-suffix model billed as its base) is the
// fallback. For each name a model discount wins over its vendor's discount:
// origin model > origin vendor > billing model > billing vendor > (1, "").
// source is "model" or "vendor" when a discount entry was found.
func GetEffectiveDiscountFor(originName, billingName string) (mul float64, source string) {
	for i, name := range [2]string{originName, billingName} {
		if name == "" || (i == 1 && name == originName) {
			continue
		}
		if mul, ok := GetModelDiscountMultiplier(name); ok {
			return mul, "model"
		}
		if mul, ok := vendorDiscountFor(name); ok {
			return mul, "vendor"
		}
	}
	return 1, ""
}

// GetEffectiveDiscountMultiplierFor is GetEffectiveDiscountFor without the source.
func GetEffectiveDiscountMultiplierFor(originName, billingName string) float64 {
	mul, _ := GetEffectiveDiscountFor(originName, billingName)
	return mul
}

// GetEffectiveDiscountMultiplier: 모델 할인 > 벤더 할인 > 1.0 (단일 모델명).
func GetEffectiveDiscountMultiplier(modelName string) float64 {
	return GetEffectiveDiscountMultiplierFor(modelName, "")
}
