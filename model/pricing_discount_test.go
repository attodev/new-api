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
	t.Cleanup(func() { _ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`); modelVendorNameCache = nil })
	p := Pricing{ModelName: "gpt-4o", VendorID: 7}
	applyPricingDiscount(&p, map[int]string{7: "OpenAI"})
	require.Equal(t, 10.0, p.DiscountPercent)
	require.Equal(t, "OpenAI", modelVendorNameCache["gpt-4o"])
}
