package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// NaN and +Inf both compare false against "<= 0" (NaN always, +Inf because it
// is positive), so a naive "ratio <= 0" guard lets them through and poisons
// every downstream quota multiplication. They must never be stored.
var nonFiniteOtherRatios = []struct {
	name  string
	ratio float64
}{
	{"nan", math.NaN()},
	{"posinf", math.Inf(1)},
	{"neginf", math.Inf(-1)},
}

func TestAddOtherRatioRejectsNonFinite(t *testing.T) {
	p := &PriceData{}

	for _, tc := range nonFiniteOtherRatios {
		p.AddOtherRatio(tc.name, tc.ratio)
		assert.False(t, p.HasOtherRatio(tc.name), "%s must not be stored as a ratio", tc.name)
	}
	assert.Nil(t, p.OtherRatios())

	p.AddOtherRatio("valid", 1.5)
	assert.True(t, p.HasOtherRatio("valid"))
	assert.Equal(t, map[string]float64{"valid": 1.5}, p.OtherRatios())
	assert.Equal(t, 150.0, p.ApplyOtherRatiosToFloat(100))
}

func TestReplaceOtherRatiosRejectsNonFinite(t *testing.T) {
	p := &PriceData{}

	ratios := map[string]float64{"valid": 1.5}
	for _, tc := range nonFiniteOtherRatios {
		ratios[tc.name] = tc.ratio
	}
	assert.True(t, p.ReplaceOtherRatios(ratios))
	assert.Equal(t, map[string]float64{"valid": 1.5}, p.OtherRatios())
	assert.Equal(t, 150.0, p.ApplyOtherRatiosToFloat(100))

	// Only non-finite entries: nothing is stored and the replace reports false.
	onlyBad := map[string]float64{}
	for _, tc := range nonFiniteOtherRatios {
		onlyBad[tc.name] = tc.ratio
	}
	assert.False(t, p.ReplaceOtherRatios(onlyBad))
	assert.Nil(t, p.OtherRatios())
	assert.Equal(t, 100.0, p.ApplyOtherRatiosToFloat(100))
}
