package model

import (
	"sync"

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

// modelVendorNameCache maps FormatMatchingModelName(modelName) -> vendor name.
// It has its own lock so the billing-path resolver never takes updatePricingLock:
// writers (updatePricing via applyPricingDiscount, InvalidatePricingCache) already
// hold updatePricingLock and take modelVendorNameMu inside it.
var (
	modelVendorNameCache map[string]string
	modelVendorNameMu    sync.RWMutex
)

func init() {
	ratio_setting.SetVendorResolver(resolveVendorName)
}

func resetModelVendorNameCache() {
	modelVendorNameMu.Lock()
	modelVendorNameCache = nil
	modelVendorNameMu.Unlock()
}

// lookupModelVendorName returns the cached vendor name and whether the cache
// has been built at all.
func lookupModelVendorName(key string) (name string, built bool) {
	modelVendorNameMu.RLock()
	defer modelVendorNameMu.RUnlock()
	return modelVendorNameCache[key], modelVendorNameCache != nil
}

// resolveVendorName answers the vendor name of a model for vendor discounts.
// It never takes updatePricingLock; when the cache has not been built it calls
// GetPricing() once to build it (which takes updatePricingLock itself), so it must
// not be called while channelSyncLock is held (lock order: updatePricingLock -> channelSyncLock).
func resolveVendorName(modelName string) (string, bool) {
	key := ratio_setting.FormatMatchingModelName(modelName)
	name, built := lookupModelVendorName(key)
	if !built {
		GetPricing()
		name, _ = lookupModelVendorName(key)
	}
	return name, name != ""
}

// applyPricingDiscount fills the display discount percent of a pricing row and
// records the model's vendor name for resolveVendorName. Called from
// updatePricing while updatePricingLock is held.
func applyPricingDiscount(p *Pricing, vendorNameByID map[int]string) {
	vendorName := vendorNameByID[p.VendorID]
	p.DiscountPercent = effectiveDiscountPercent(p.ModelName, vendorName)
	modelVendorNameMu.Lock()
	defer modelVendorNameMu.Unlock()
	if modelVendorNameCache == nil {
		modelVendorNameCache = map[string]string{}
	}
	modelVendorNameCache[ratio_setting.FormatMatchingModelName(p.ModelName)] = vendorName
}
