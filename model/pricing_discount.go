package model

import (
	"sync"
	"time"

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
// It is an immutable snapshot: updatePricing fills a local map and publishes it
// with one assignment (publishModelVendorNames) after its loop, so a resolver
// never sees a half-built map. InvalidatePricingCache leaves the snapshot in
// place; the previous names serve until the next successful rebuild publishes.
// It has its own lock so the billing-path resolver never takes updatePricingLock.
var (
	modelVendorNameCache map[string]string
	modelVendorNameMu    sync.RWMutex
)

// vendorNameColdBuildRetry is the negative-cache window of the first-start
// build: while no snapshot has been published, the resolver triggers at most
// one pricing build per window (a failed or skipped build is not retried on
// every billed request).
const vendorNameColdBuildRetry = time.Minute

var (
	// vendorNameColdBuild builds the pricing cache (and so the snapshot); a var
	// so tests can count and stub it.
	vendorNameColdBuild = func() { GetPricing() }
	// vendorNameColdBuildMu serialises cold builds; lock order:
	// vendorNameColdBuildMu -> updatePricingLock -> modelVendorNameMu.
	vendorNameColdBuildMu   sync.Mutex
	vendorNameColdBuildLast time.Time
)

func init() {
	ratio_setting.SetVendorResolver(resolveVendorName)
}

// resetModelVendorNameCache drops the snapshot (tests only: production never
// clears it, see InvalidatePricingCache).
func resetModelVendorNameCache() {
	modelVendorNameMu.Lock()
	modelVendorNameCache = nil
	modelVendorNameMu.Unlock()
	vendorNameColdBuildMu.Lock()
	vendorNameColdBuildLast = time.Time{}
	vendorNameColdBuildMu.Unlock()
}

// publishModelVendorNames swaps in a fully built snapshot. Called at the end of
// a successful updatePricing; the map must not be written afterwards.
func publishModelVendorNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	modelVendorNameMu.Lock()
	modelVendorNameCache = names
	modelVendorNameMu.Unlock()
}

// lookupModelVendorName returns the snapshot's vendor name and whether a
// snapshot has been published at all.
func lookupModelVendorName(key string) (name string, built bool) {
	modelVendorNameMu.RLock()
	defer modelVendorNameMu.RUnlock()
	return modelVendorNameCache[key], modelVendorNameCache != nil
}

// resolveVendorName answers the vendor name of a model for vendor discounts.
// It reads the published snapshot only. On a true cold start (no snapshot yet)
// it builds the pricing cache once per vendorNameColdBuildRetry window (which
// takes updatePricingLock), so it must not be called while channelSyncLock is
// held (lock order: updatePricingLock -> channelSyncLock).
func resolveVendorName(modelName string) (string, bool) {
	key := ratio_setting.FormatMatchingModelName(modelName)
	name, built := lookupModelVendorName(key)
	if !built {
		coldBuildModelVendorNames()
		name, _ = lookupModelVendorName(key)
	}
	return name, name != ""
}

func coldBuildModelVendorNames() {
	vendorNameColdBuildMu.Lock()
	defer vendorNameColdBuildMu.Unlock()
	if _, built := lookupModelVendorName(""); built {
		return // another resolver's build published meanwhile
	}
	if !vendorNameColdBuildLast.IsZero() && time.Since(vendorNameColdBuildLast) < vendorNameColdBuildRetry {
		return
	}
	vendorNameColdBuildLast = time.Now()
	vendorNameColdBuild()
}

// applyPricingDiscount fills the display discount percent of a pricing row and
// records the model's vendor name into names, the rebuild's local map that
// updatePricing publishes after its loop. Called while updatePricingLock is held.
func applyPricingDiscount(p *Pricing, vendorNameByID map[int]string, names map[string]string) {
	vendorName := vendorNameByID[p.VendorID]
	p.DiscountPercent = effectiveDiscountPercent(p.ModelName, vendorName)
	names[ratio_setting.FormatMatchingModelName(p.ModelName)] = vendorName
}
