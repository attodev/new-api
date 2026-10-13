package common

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

// Quota conversions use two domains with different bounds (the split comes
// from upstream a073f74b3, adapted to this fork's wallet ceiling):
//
//   - Single-request charges (QuotaFromFloat, QuotaFromFloatStrict: the
//     pre-consume estimate and the settlement of one request) saturate at the
//     int32 range, MaxRequestQuota/MinRequestQuota. A single request never
//     legitimately approaches it, and keeping it far below the int64 range
//     means batched accumulation of many saturated charges cannot wrap.
//     Upstream calls these bounds MaxQuota/MinQuota.
//   - Wallet balances and wallet mutations (top-ups, redemption codes, admin
//     grants, token budgets, wallet-priced purchases) are bounded by MaxQuota
//     (constants.go), this fork's bigint wallet ceiling. Upstream's equivalent
//     is MaxWalletQuota (2^53-1); the fork's ceiling is deliberately lower,
//     see MaxQuota.
const (
	MaxRequestQuota = math.MaxInt32
	MinRequestQuota = math.MinInt32
)

// ValidateWalletQuota enforces the upper bound shared by wallet mutations.
// Negative balances remain valid because billing can temporarily overdraw a
// wallet; callers that accept credits must apply their own positive check.
func ValidateWalletQuota(quota int64) error {
	if quota > MaxQuota {
		return fmt.Errorf("wallet quota exceeds %d", MaxQuota)
	}
	return nil
}

// saturateQuotaBounded converts an already-rounded quota to int64, clamping
// it to [minQuota, maxQuota]; NaN becomes 0. The bounds themselves are valid
// values. The second result reports whether a clamp (or NaN fallback) was
// needed, which strict callers turn into an error.
func saturateQuotaBounded(value float64, maxQuota int64, minQuota int64) (int64, bool) {
	switch {
	case math.IsNaN(value):
		return 0, true
	case value > float64(maxQuota):
		return maxQuota, true
	case value < float64(minQuota):
		return minQuota, true
	default:
		return int64(value), false
	}
}

// QuotaFromFloat converts a computed single-request quota value to int with
// saturation. Quota products can include user-controlled multipliers (image
// n, video seconds, resolution ratios); an oversized product must never wrap
// around and turn a charge into a credit.
func QuotaFromFloat(value float64) int {
	quota, _ := saturateQuotaBounded(value, MaxRequestQuota, MinRequestQuota)
	return int(quota)
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
	quota, clamped := saturateQuotaBounded(value, MaxRequestQuota, MinRequestQuota)
	if clamped {
		return 0, fmt.Errorf("quota conversion out of range: %g", value)
	}
	return int(quota), nil
}

// WalletQuotaFromDecimalStrict rounds a wallet or top-up quota to an integer
// and rejects a value outside [-MaxQuota, MaxQuota]. The result is int64
// because the wallet domain is wider than int on 32-bit builds.
func WalletQuotaFromDecimalStrict(d decimal.Decimal) (int64, error) {
	rounded := d.Round(0)
	if rounded.GreaterThan(decimal.NewFromInt(MaxQuota)) || rounded.LessThan(decimal.NewFromInt(-MaxQuota)) {
		return 0, fmt.Errorf("wallet quota conversion out of range: %s", rounded.String())
	}
	return rounded.IntPart(), nil
}
