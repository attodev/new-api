package model

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// updateDiscountOption stores a discount map and drops the pricing cache so
// the marketplace and billing see the new percent immediately.
func updateDiscountOption(key, value string) error {
	var err error
	switch key {
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
	}
	if err == nil {
		InvalidatePricingCache()
	}
	return err
}

// effectiveDiscountPercent is the percent shown for a model: its own model
// discount wins over its vendor's discount; 0 when neither is set.
func effectiveDiscountPercent(modelName, vendorName string) float64 {
	if pct, ok := ratio_setting.GetModelDiscountPercent(modelName); ok {
		return pct
	}
	if vendorName != "" {
		if pct, ok := ratio_setting.GetVendorDiscountPercent(vendorName); ok {
			return pct
		}
	}
	return 0
}

// vendorDiscountPercent is the stored vendor discount percent (0 when unset).
func vendorDiscountPercent(name string) float64 {
	pct, _ := ratio_setting.GetVendorDiscountPercent(name)
	return pct
}

// modelVendorNameCache maps modelName -> vendor name. It is written only from
// updatePricing (which holds updatePricingLock) and read under updatePricingLock.
var modelVendorNameCache map[string]string

func init() {
	ratio_setting.SetVendorResolver(resolveVendorName)
}

// resolveVendorName answers the vendor name of a model for vendor discounts.
// It reads the pricing cache (refreshing it if stale) and must not be called
// while channelSyncLock is held (lock order: updatePricingLock -> channelSyncLock).
func resolveVendorName(modelName string) (string, bool) {
	GetPricing()
	updatePricingLock.Lock() // sync.Mutex (model/pricing.go)
	defer updatePricingLock.Unlock()
	name, ok := modelVendorNameCache[modelName]
	return name, ok && name != ""
}

// applyPricingDiscount fills the display discount percent of a pricing row and
// records the model's vendor name for resolveVendorName. Called from
// updatePricing while updatePricingLock is held.
func applyPricingDiscount(p *Pricing, vendorNameByID map[int]string) {
	vendorName := vendorNameByID[p.VendorID]
	p.DiscountPercent = effectiveDiscountPercent(p.ModelName, vendorName)
	if modelVendorNameCache == nil {
		modelVendorNameCache = map[string]string{}
	}
	modelVendorNameCache[p.ModelName] = vendorName
}
