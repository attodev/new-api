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

func TestGetModelRatioGPT56FamilyIsExplicit(t *testing.T) {
	InitRatioSettings()

	want := map[string]float64{
		"gpt-5.5":       2.5,
		"gpt-5.6-sol":   2.5,
		"gpt-5.6-terra": 1.25,
		"gpt-5.6-luna":  0.5,
	}
	for model, wantRatio := range want {
		ratio, ok, _ := GetModelRatio(model)
		if !ok {
			t.Fatalf("%s: expected an explicit model ratio entry, got none", model)
		}
		if ratio != wantRatio {
			// Without this entry, an unconfigured model falls back to ratio 37.5 -
			// a ~37x overcharge relative to gpt-5.6-terra's real price.
			t.Fatalf("%s: model ratio = %v, want %v", model, ratio, wantRatio)
		}
	}
}
