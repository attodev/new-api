package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestEffectiveDiscountPercentModelBeatsVendor(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"gpt-4":25}`))
	require.NoError(t, ratio_setting.UpdateVendorDiscountByJSONString(`{"OpenAI":10}`))
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
		_ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`)
	})
	require.Equal(t, 25.0, effectiveDiscountPercent("gpt-4", "OpenAI"))
	require.Equal(t, 10.0, effectiveDiscountPercent("gpt-4o", "OpenAI"))
	require.Equal(t, 0.0, effectiveDiscountPercent("claude-3", "Anthropic"))
	require.Equal(t, 0.0, effectiveDiscountPercent("gpt-4o", ""))
}

func TestApplyPricingDiscountFillsCache(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateVendorDiscountByJSONString(`{"OpenAI":10}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`); resetModelVendorNameCache() })
	names := map[string]string{}
	p := Pricing{ModelName: "gpt-4o", VendorID: 7}
	applyPricingDiscount(&p, map[int]string{7: "OpenAI"}, names)
	require.Equal(t, 10.0, p.DiscountPercent)
	require.Equal(t, "OpenAI", names["gpt-4o"])

	// cache keys are normalised like GetModelDiscountMultiplier
	g := Pricing{ModelName: "gpt-4-gizmo-abc", VendorID: 7}
	applyPricingDiscount(&g, map[int]string{7: "OpenAI"}, names)
	require.Equal(t, "OpenAI", names["gpt-4-gizmo-*"])
	publishModelVendorNames(names)
	name, ok := resolveVendorName("gpt-4-gizmo-other")
	require.True(t, ok)
	require.Equal(t, "OpenAI", name)
}

// A rebuild fills a local map; resolvers keep reading the previous snapshot
// until the rebuild publishes, so a half-built map never drops a vendor.
func TestResolveVendorNameReadsPreviousSnapshotDuringRebuild(t *testing.T) {
	t.Cleanup(resetModelVendorNameCache)
	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})

	building := map[string]string{}
	p := Pricing{ModelName: "claude-3", VendorID: 8}
	applyPricingDiscount(&p, map[int]string{8: "Anthropic"}, building)

	name, ok := resolveVendorName("gpt-4o")
	require.True(t, ok, "half-built rebuild must not hide the previous vendor name")
	require.Equal(t, "OpenAI", name)

	// the publish replaces the snapshot wholesale (stale models drop out)
	publishModelVendorNames(building)
	_, ok = resolveVendorName("gpt-4o")
	require.False(t, ok)
	name, ok = resolveVendorName("claude-3")
	require.True(t, ok)
	require.Equal(t, "Anthropic", name)
}

func TestInvalidatePricingCacheKeepsVendorNameSnapshot(t *testing.T) {
	t.Cleanup(resetModelVendorNameCache)
	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})

	InvalidatePricingCache()
	name, ok := lookupModelVendorName("gpt-4o")
	require.True(t, ok, "invalidate must keep the previous snapshot until the next rebuild publishes")
	require.Equal(t, "OpenAI", name)
}

// With no snapshot (first start) the resolver triggers at most one pricing
// build per vendorNameColdBuildRetry window, even when the build publishes
// nothing (DB error) — no rebuild storm on the billing path.
func TestResolveVendorNameColdStartBuildsAtMostOncePerWindow(t *testing.T) {
	resetModelVendorNameCache()
	calls := 0
	original := vendorNameColdBuild
	vendorNameColdBuild = func() { calls++ }
	t.Cleanup(func() { vendorNameColdBuild = original; resetModelVendorNameCache() })

	for i := 0; i < 50; i++ {
		_, ok := resolveVendorName("gpt-4o")
		require.False(t, ok)
	}
	require.Equal(t, 1, calls, "a failed cold build must not be retried on every resolve")

	// an empty pricing table publishes an empty (non-nil) snapshot: no more builds
	resetModelVendorNameCache()
	calls = 0
	vendorNameColdBuild = func() { calls++; publishModelVendorNames(map[string]string{}) }
	for i := 0; i < 50; i++ {
		_, ok := resolveVendorName("gpt-4o")
		require.False(t, ok)
	}
	require.Equal(t, 1, calls)
}

func TestUpdatePricingPublishesVendorNamesAtomically(t *testing.T) {
	resetPricingEndpointTestTables(t)
	t.Cleanup(resetModelVendorNameCache)
	vendor := &Vendor{Name: "OpenAI"}
	require.NoError(t, DB.Create(vendor).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "gpt-4o", VendorID: vendor.Id}).Error)
	insertPricingEndpointChannel(t, 901, 1, dto.ChannelOtherSettings{})
	for i := 0; i < 50; i++ {
		insertPricingEndpointAbility(t, 901, fmt.Sprintf("filler-%02d", i))
	}
	insertPricingEndpointAbility(t, 901, "gpt-4o")
	InitChannelCache()
	GetPricing()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			InvalidatePricingCache()
			GetPricing()
		}
	}()
	for {
		select {
		case <-done:
			name, ok := resolveVendorName("gpt-4o")
			require.True(t, ok)
			require.Equal(t, "OpenAI", name)
			return
		default:
			name, ok := resolveVendorName("gpt-4o")
			require.True(t, ok, "resolver must never see a partial vendor map")
			require.Equal(t, "OpenAI", name)
		}
	}
}

func TestUpdateDiscountOptionInvalidatesOnlyOnSuccess(t *testing.T) {
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
		pricingMap = nil
	})
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"gpt-4":25}`))

	pricingMap = []Pricing{{ModelName: "gpt-4"}}
	require.Error(t, updateDiscountOption("ModelDiscount", `{"gpt-4":"a"}`))
	require.NotNil(t, pricingMap, "invalid JSON must not invalidate the pricing cache")
	pct, ok := ratio_setting.GetModelDiscountPercent("gpt-4")
	require.True(t, ok)
	require.Equal(t, 25.0, pct, "invalid JSON must keep the previous map")

	require.NoError(t, updateDiscountOption("ModelDiscount", `{"gpt-4":30}`))
	require.Nil(t, pricingMap, "valid JSON must invalidate the pricing cache")
	pct, _ = ratio_setting.GetModelDiscountPercent("gpt-4")
	require.Equal(t, 30.0, pct)
}
