package billingexpr

import (
	"fmt"
	"math"
)

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding. Every tiered billing path (pre-consume, settlement, breakdown
// validation, log fields) MUST use this function to avoid +-1 discrepancies.
//
// The result saturates at int32 bounds, the single-request quota bound
// (common.MaxRequestQuota; wallet columns are bigint, but one request's
// charge is kept far below that range): an oversized expression result must
// never wrap around and turn a charge into a credit.
func QuotaRound(f float64) int {
	r := math.Round(f)
	if math.IsNaN(r) {
		return 0
	}
	if r >= math.MaxInt32 {
		return math.MaxInt32
	}
	if r <= math.MinInt32 {
		return math.MinInt32
	}
	return int(r)
}

// QuotaRoundStrict is QuotaRound but rejects an out-of-range value instead
// of silently saturating it. Pre-consume callers must use this: a silently
// clamped estimate would go on to charge the wrong amount.
func QuotaRoundStrict(f float64) (int, error) {
	r := math.Round(f)
	if math.IsNaN(r) {
		return 0, fmt.Errorf("quota conversion received NaN")
	}
	if r >= math.MaxInt32 || r <= math.MinInt32 {
		return 0, fmt.Errorf("quota conversion out of range: %g", r)
	}
	return int(r), nil
}
