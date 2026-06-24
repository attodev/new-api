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
