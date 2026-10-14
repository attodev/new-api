package model

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
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
	modelVendorNameCache       map[string]string
	modelVendorNamePublishedAt time.Time
	modelVendorNameMu          sync.RWMutex
)

// vendorNameRefreshAge is the snapshot age after which a resolve starts one
// background pricing rebuild (and serves the current snapshot meanwhile), so a
// vendor change reaches billing even when nothing else calls GetPricing. It is
// also the minimum gap between refresh attempts, so a failing rebuild is not
// retried on every billed request.
const vendorNameRefreshAge = time.Minute

var (
	// getPricingForRefresh is the background rebuild; a var so tests can count
	// and block it.
	getPricingForRefresh = GetPricing
	// vendorNameRefreshMu is held by the running refresh: TryLock keeps a
	// second refresher from starting.
	vendorNameRefreshMu sync.Mutex
	// vendorNameRefreshLast is the start of the last refresh (UnixNano).
	vendorNameRefreshLast atomic.Int64
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
	modelVendorNamePublishedAt = time.Time{}
	modelVendorNameMu.Unlock()
	vendorNameColdBuildMu.Lock()
	vendorNameColdBuildLast = time.Time{}
	vendorNameColdBuildMu.Unlock()
	vendorNameRefreshLast.Store(0)
}

// publishModelVendorNames swaps in a fully built snapshot. Called at the end of
// a successful updatePricing; the map must not be written afterwards. An empty
// rebuild (no enabled model at all, e.g. the ability query returned nothing)
// keeps a non-empty previous snapshot rather than dropping every vendor
// discount; it still counts as a refresh. A failed vendor query is handled per
// model in applyPricingDiscount.
func publishModelVendorNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	modelVendorNameMu.Lock()
	if len(names) > 0 || len(modelVendorNameCache) == 0 {
		modelVendorNameCache = names
	}
	modelVendorNamePublishedAt = time.Now()
	modelVendorNameMu.Unlock()
}

// lookupModelVendorName returns the snapshot's vendor name and whether a
// snapshot has been published at all.
func lookupModelVendorName(key string) (name string, built bool) {
	name, built, _ = lookupModelVendorNameAge(key)
	return name, built
}

func lookupModelVendorNameAge(key string) (name string, built bool, age time.Duration) {
	modelVendorNameMu.RLock()
	defer modelVendorNameMu.RUnlock()
	return modelVendorNameCache[key], modelVendorNameCache != nil, time.Since(modelVendorNamePublishedAt)
}

// resolveVendorName answers the vendor name of a model for vendor discounts.
// It reads the published snapshot only. On a true cold start (no snapshot yet)
// it builds the pricing cache once per vendorNameColdBuildRetry window (which
// takes updatePricingLock), so it must not be called while channelSyncLock is
// held (lock order: updatePricingLock -> channelSyncLock). A snapshot older
// than vendorNameRefreshAge starts a background rebuild and is served as is.
func resolveVendorName(modelName string) (string, bool) {
	key := ratio_setting.FormatMatchingModelName(modelName)
	name, built, age := lookupModelVendorNameAge(key)
	if !built {
		coldBuildModelVendorNames()
		name, _ = lookupModelVendorName(key)
	} else if age > vendorNameRefreshAge {
		startVendorNameRefresh()
	}
	return name, name != ""
}

// startVendorNameRefresh starts one background pricing rebuild unless one is
// running (TryLock) or one started within vendorNameRefreshAge. It never
// blocks the caller; the rebuild publishes the new snapshot when it finishes.
func startVendorNameRefresh() {
	if last := vendorNameRefreshLast.Load(); last != 0 && time.Since(time.Unix(0, last)) < vendorNameRefreshAge {
		return
	}
	if !vendorNameRefreshMu.TryLock() {
		return
	}
	vendorNameRefreshLast.Store(time.Now().UnixNano())
	refresh := getPricingForRefresh
	gopool.Go(func() { // gopool recovers and logs a panic
		defer vendorNameRefreshMu.Unlock()
		refresh()
	})
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
// A model that names a vendor missing from vendorNameByID (updatePricing
// ignores a failed vendor query) keeps the previous snapshot's name, so a
// transient DB error does not drop its vendor discount. Only such rows read
// the snapshot; it is replaced only by publishModelVendorNames under
// updatePricingLock, so every read in one rebuild sees the same snapshot.
func applyPricingDiscount(p *Pricing, vendorNameByID map[int]string, names map[string]string) {
	key := ratio_setting.FormatMatchingModelName(p.ModelName)
	vendorName, known := vendorNameByID[p.VendorID]
	if !known && p.VendorID > 0 {
		vendorName, _ = lookupModelVendorName(key)
	}
	p.DiscountPercent = effectiveDiscountPercent(p.ModelName, vendorName)
	names[key] = vendorName
}
