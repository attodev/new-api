package ratio_setting

import "testing"

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
