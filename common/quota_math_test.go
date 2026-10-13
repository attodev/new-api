package common

import (
	"fmt"
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuotaFromFloat guards the billing invariant that oversized quota
// products (e.g. price multiplied by a huge user-supplied count) saturate
// instead of wrapping into a negative charge (credit).
func TestQuotaFromFloat(t *testing.T) {
	assert.Equal(t, 42, QuotaFromFloat(42.4))
	assert.Equal(t, -42, QuotaFromFloat(-42.4))
	// 2000 quota per call * n=18446744073686646784 overflows int64.
	assert.Equal(t, math.MaxInt32, QuotaFromFloat(2000*1.8446744073686647e19))
	assert.Equal(t, math.MinInt32, QuotaFromFloat(-2000*1.8446744073686647e19))
	assert.Equal(t, math.MaxInt32, QuotaFromFloat(math.Inf(1)))
	assert.Equal(t, math.MinInt32, QuotaFromFloat(math.Inf(-1)))
	assert.Equal(t, 0, QuotaFromFloat(math.NaN()))
}

// TestQuotaFromFloatStrict guards a stricter invariant than QuotaFromFloat:
// pre-consume must not silently proceed with a saturated estimate (which
// would charge the wrong amount) - it should reject the request outright.
func TestQuotaFromFloatStrict(t *testing.T) {
	q, err := QuotaFromFloatStrict(42.4)
	assert.NoError(t, err)
	assert.Equal(t, 42, q)

	_, err = QuotaFromFloatStrict(2000 * 1.8446744073686647e19)
	assert.Error(t, err)

	_, err = QuotaFromFloatStrict(math.Inf(1))
	assert.Error(t, err)

	_, err = QuotaFromFloatStrict(math.NaN())
	assert.Error(t, err)
}

// TestQuotaFromFloatRequestBoundStaysInt32 pins the per-request/wallet split
// ported from upstream a073f74b3: single-request conversions keep saturating
// at the int32 range even though the wallet columns are bigint. Only wallet
// helpers use the wider common.MaxQuota ceiling.
func TestQuotaFromFloatRequestBoundStaysInt32(t *testing.T) {
	assert.Equal(t, math.MaxInt32, MaxRequestQuota)
	assert.Equal(t, math.MinInt32, MinRequestQuota)
	assert.Equal(t, MaxRequestQuota, QuotaFromFloat(float64(math.MaxInt32)+42))
	assert.Equal(t, MaxRequestQuota, QuotaFromFloat(float64(MaxQuota)))

	q, err := QuotaFromFloatStrict(float64(math.MaxInt32))
	require.NoError(t, err)
	assert.Equal(t, MaxRequestQuota, q)

	_, err = QuotaFromFloatStrict(3_000_000_000) // above int32, below MaxQuota
	assert.Error(t, err)
}

func TestValidateWalletQuota(t *testing.T) {
	assert.NoError(t, ValidateWalletQuota(MaxQuota))
	assert.NoError(t, ValidateWalletQuota(0))
	// Billing can temporarily overdraw a wallet, so only the upper bound applies.
	assert.NoError(t, ValidateWalletQuota(-MaxQuota-1))
	err := ValidateWalletQuota(MaxQuota + 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("%d", MaxQuota))
}

func TestWalletQuotaFromDecimalStrict(t *testing.T) {
	quota, err := WalletQuotaFromDecimalStrict(decimal.NewFromInt(4_294_500_000))
	require.NoError(t, err)
	assert.Equal(t, int64(4_294_500_000), quota)

	quota, err = WalletQuotaFromDecimalStrict(decimal.NewFromInt(MaxQuota))
	require.NoError(t, err)
	assert.Equal(t, MaxQuota, quota)

	quota, err = WalletQuotaFromDecimalStrict(decimal.RequireFromString("41.5"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), quota)

	quota, err = WalletQuotaFromDecimalStrict(decimal.NewFromInt(MaxQuota + 1))
	assert.Error(t, err)
	assert.Zero(t, quota)

	quota, err = WalletQuotaFromDecimalStrict(decimal.NewFromInt(-MaxQuota - 1))
	assert.Error(t, err)
	assert.Zero(t, quota)

	quota, err = WalletQuotaFromDecimalStrict(decimal.RequireFromString("1e30"))
	assert.Error(t, err)
	assert.Zero(t, quota)
}
