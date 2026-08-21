package common

import (
	"fmt"
	"math"
)

// QuotaFromFloat converts a computed quota value to int with saturation.
// Quota products can include user-controlled multipliers (image n, video
// seconds, resolution ratios); an oversized product must never wrap around
// and turn a charge into a credit. The bound is int32 because quota columns
// (user/token/log) are 32-bit integers in the database.
func QuotaFromFloat(value float64) int {
	if math.IsNaN(value) {
		return 0
	}
	if value >= math.MaxInt32 {
		return math.MaxInt32
	}
	if value <= math.MinInt32 {
		return math.MinInt32
	}
	return int(value)
}

// QuotaFromFloatStrict is QuotaFromFloat but rejects an out-of-range value
// instead of silently saturating it. Pre-consume callers must use this: a
// silently clamped estimate would go on to charge the wrong amount, whereas
// settlement callers (the cost is already incurred and must be recorded
// regardless) should keep using the saturating QuotaFromFloat.
func QuotaFromFloatStrict(value float64) (int, error) {
	if math.IsNaN(value) {
		return 0, fmt.Errorf("quota conversion received NaN")
	}
	if value >= math.MaxInt32 || value <= math.MinInt32 {
		return 0, fmt.Errorf("quota conversion out of range: %g", value)
	}
	return int(value), nil
}
