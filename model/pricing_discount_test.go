package model

import (
	"testing"

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
	p := Pricing{ModelName: "gpt-4o", VendorID: 7}
	applyPricingDiscount(&p, map[int]string{7: "OpenAI"})
	require.Equal(t, 10.0, p.DiscountPercent)
	require.Equal(t, "OpenAI", modelVendorNameCache["gpt-4o"])

	// cache keys are normalised like GetModelDiscountMultiplier
	g := Pricing{ModelName: "gpt-4-gizmo-abc", VendorID: 7}
	applyPricingDiscount(&g, map[int]string{7: "OpenAI"})
	require.Equal(t, "OpenAI", modelVendorNameCache["gpt-4-gizmo-*"])
	name, ok := resolveVendorName("gpt-4-gizmo-other")
	require.True(t, ok)
	require.Equal(t, "OpenAI", name)
}

func TestResolveVendorNameAndInvalidate(t *testing.T) {
	t.Cleanup(resetModelVendorNameCache)
	resetModelVendorNameCache()
	p := Pricing{ModelName: "gpt-4o", VendorID: 7}
	applyPricingDiscount(&p, map[int]string{7: "OpenAI"})
	name, ok := resolveVendorName("gpt-4o")
	require.True(t, ok)
	require.Equal(t, "OpenAI", name)

	InvalidatePricingCache()
	require.Nil(t, modelVendorNameCache)
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
