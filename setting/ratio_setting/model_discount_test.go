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
