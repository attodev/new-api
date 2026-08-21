package service

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestDecimalToQuota_Saturates guards the billing invariant that an
// oversized decimal quota product (price * user-controlled multiplier)
// clamps to int32 bounds instead of wrapping into a negative charge - the
// same property common.QuotaFromFloat provides for float64 products, but
// for the decimal-based paths (audio, tool-call surcharge).
func TestDecimalToQuota_Saturates(t *testing.T) {
	require.Equal(t, 42, decimalToQuota(decimal.NewFromInt(42)))
	require.Equal(t, -42, decimalToQuota(decimal.NewFromInt(-42)))
	require.Equal(t, math.MaxInt32, decimalToQuota(decimal.NewFromInt(math.MaxInt32).Mul(decimal.NewFromInt(1000))))
	require.Equal(t, math.MinInt32, decimalToQuota(decimal.NewFromInt(math.MinInt32).Mul(decimal.NewFromInt(1000))))
}
