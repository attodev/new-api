package ratio_setting

import "testing"

func TestGetCreateCacheRatioGPT56FamilyIsExplicit(t *testing.T) {
	InitRatioSettings()

	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		ratio, ok := GetCreateCacheRatio(model)
		if !ok {
			t.Fatalf("%s: expected an explicit cache-creation ratio entry, got none (falls back to default)", model)
		}
		if ratio != 1.25 {
			t.Fatalf("%s: cache creation ratio = %v, want 1.25", model, ratio)
		}
	}
}
