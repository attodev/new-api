package types

import (
	"math"
	"testing"
)

// TestAddOtherRatio_RejectsNaNAndInf reproduces a real bug: the guard only
// checked "ratio <= 0", but NaN and +Inf both compare false against <= 0 (a
// NaN comparison is always false, and +Inf > 0), so both passed through
// unguarded. A NaN or +Inf multiplier poisons every downstream quota
// multiplication it touches (int(NaN * quota) or int(Inf * quota) is
// undefined/overflowing behavior, not a rejected value).
func TestAddOtherRatio_RejectsNaNAndInf(t *testing.T) {
	p := &PriceData{}

	p.AddOtherRatio("nan", math.NaN())
	if _, ok := p.OtherRatios["nan"]; ok {
		t.Fatal("NaN must not be stored as a ratio")
	}

	p.AddOtherRatio("posinf", math.Inf(1))
	if _, ok := p.OtherRatios["posinf"]; ok {
		t.Fatal("+Inf must not be stored as a ratio")
	}

	p.AddOtherRatio("neginf", math.Inf(-1))
	if _, ok := p.OtherRatios["neginf"]; ok {
		t.Fatal("-Inf must not be stored as a ratio")
	}

	p.AddOtherRatio("valid", 1.5)
	if p.OtherRatios["valid"] != 1.5 {
		t.Fatalf("a normal positive ratio must still be stored, got %v", p.OtherRatios["valid"])
	}
}
