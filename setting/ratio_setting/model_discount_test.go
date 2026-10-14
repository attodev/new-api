package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelDiscountMultiplier(t *testing.T) {
	if err := UpdateModelDiscountByJSONString(`{"gpt-4":20}`); err != nil {
		t.Fatalf("update model discount: %v", err)
	}
	mul, ok := GetModelDiscountMultiplier("gpt-4")
	if !ok || mul != 0.8 {
		t.Fatalf("want 0.8,true got %v,%v", mul, ok)
	}
	if _, ok := GetModelDiscountMultiplier("unknown-model"); ok {
		t.Fatalf("unknown model should have no discount")
	}
}

func TestVendorDiscountMultiplier(t *testing.T) {
	if err := UpdateVendorDiscountByJSONString(`{"openai":10}`); err != nil {
		t.Fatalf("update vendor discount: %v", err)
	}
	mul, ok := GetVendorDiscountMultiplier("openai")
	if !ok || mul != 0.9 {
		t.Fatalf("want 0.9,true got %v,%v", mul, ok)
	}
}

func TestDiscountValidationRejectsOutOfRange(t *testing.T) {
	if err := UpdateModelDiscountByJSONString(`{"x":-1}`); err == nil {
		t.Fatalf("negative percent must be rejected")
	}
	if err := UpdateModelDiscountByJSONString(`{"x":101}`); err == nil {
		t.Fatalf(">100 percent must be rejected")
	}
}

func TestEffectiveDiscountPrecedence(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"m-model":20}`)
	_ = UpdateVendorDiscountByJSONString(`{"acme":10}`)
	SetVendorResolver(func(model string) (string, bool) {
		switch model {
		case "m-model", "v-model":
			return "acme", true
		}
		return "", false
	})

	if got := GetEffectiveDiscountMultiplier("m-model"); got != 0.8 {
		t.Fatalf("model precedence: want 0.8 got %v", got)
	}
	if got := GetEffectiveDiscountMultiplier("v-model"); got != 0.9 {
		t.Fatalf("vendor fallback: want 0.9 got %v", got)
	}
	if got := GetEffectiveDiscountMultiplier("none"); got != 1.0 {
		t.Fatalf("no discount: want 1.0 got %v", got)
	}
}

func TestEffectiveMultiplierCombinesWithGroupConceptually(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"combo":50}`)
	// groupRatio=0.8 가정, 할인 0.5 -> 최종 0.4
	groupRatio := 0.8
	final := groupRatio * GetEffectiveDiscountMultiplier("combo")
	if final != 0.4 {
		t.Fatalf("want 0.4 got %v", final)
	}
}

func TestDiscountPercentAccessors(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"pct-model":25}`)
	_ = UpdateVendorDiscountByJSONString(`{"pct-vendor":15}`)

	if p, ok := GetModelDiscountPercent("pct-model"); !ok || p != 25 {
		t.Fatalf("model percent want 25,true got %v,%v", p, ok)
	}
	if _, ok := GetModelDiscountPercent("nope"); ok {
		t.Fatalf("unset model should be false")
	}
	if p, ok := GetVendorDiscountPercent("pct-vendor"); !ok || p != 15 {
		t.Fatalf("vendor percent want 15,true got %v,%v", p, ok)
	}
	if _, ok := GetVendorDiscountPercent("nope"); ok {
		t.Fatalf("unset vendor should be false")
	}
}

func TestUpdateModelDiscountRejectsNonNumeric(t *testing.T) {
	require.NoError(t, UpdateModelDiscountByJSONString(`{"gpt-4":25}`))
	require.Error(t, UpdateModelDiscountByJSONString(`{"gpt-4":"a"}`))
	mul, ok := GetModelDiscountMultiplier("gpt-4")
	require.True(t, ok)
	require.InDelta(t, 0.75, mul, 1e-9) // 이전 값 유지
}

func TestEffectiveDiscountForPrefersOriginThenBilling(t *testing.T) {
	require.NoError(t, UpdateModelDiscountByJSONString(`{"origin-only-thinking":30,"billing-only":20}`))
	require.NoError(t, UpdateVendorDiscountByJSONString(`{"origin-vendor":40,"billing-vendor":10}`))
	prev := SetVendorResolver(func(model string) (string, bool) {
		switch model {
		case "vendor-origin-thinking":
			return "origin-vendor", true
		case "vendor-billing":
			return "billing-vendor", true
		}
		return "", false
	})
	t.Cleanup(func() {
		SetVendorResolver(prev)
		_ = UpdateModelDiscountByJSONString(`{}`)
		_ = UpdateVendorDiscountByJSONString(`{}`)
	})

	cases := []struct {
		origin, billing string
		mul             float64
		source          string
	}{
		// discount listed on the origin name (marketplace key) wins
		{"origin-only-thinking", "origin-only", 0.7, "model"},
		// origin has none -> billing-name discount applies
		{"billing-only-thinking", "billing-only", 0.8, "model"},
		// origin model discount beats a billing-name discount
		{"origin-only-thinking", "billing-only", 0.7, "model"},
		// origin vendor beats billing model
		{"vendor-origin-thinking", "billing-only", 0.6, "vendor"},
		// billing vendor is the last resort
		{"nothing-thinking", "vendor-billing", 0.9, "vendor"},
		{"nothing-thinking", "nothing", 1, ""},
		{"nothing", "", 1, ""},
	}
	for _, tc := range cases {
		mul, source := GetEffectiveDiscountFor(tc.origin, tc.billing)
		require.InDelta(t, tc.mul, mul, 1e-9, "%s/%s", tc.origin, tc.billing)
		require.Equal(t, tc.source, source, "%s/%s", tc.origin, tc.billing)
		require.InDelta(t, tc.mul, GetEffectiveDiscountMultiplierFor(tc.origin, tc.billing), 1e-9)
	}
}
