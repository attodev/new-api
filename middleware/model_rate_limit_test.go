package middleware

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Ported from upstream a073f74b3: the model request rate limit window and the
// token-bucket capacity (count*duration) must saturate instead of wrapping
// int64 into a negative or tiny capacity.

func TestRateLimitDurationSecondsSaturates(t *testing.T) {
	require.Equal(t, int64(0), rateLimitDurationSeconds(0))
	require.Equal(t, int64(0), rateLimitDurationSeconds(-5))
	require.Equal(t, int64(60), rateLimitDurationSeconds(1))
	require.Equal(t, int64(24*60*60), rateLimitDurationSeconds(24*60))
	if math.MaxInt > math.MaxInt64/60 {
		require.Equal(t, int64(math.MaxInt64), rateLimitDurationSeconds(math.MaxInt))
	}
}

func TestRateLimitCapacityDoesNotWrap(t *testing.T) {
	require.Equal(t, int64(0), rateLimitCapacity(0, 60))
	require.Equal(t, int64(0), rateLimitCapacity(10, 0))
	require.Equal(t, int64(0), rateLimitCapacity(-1, 60))
	require.Equal(t, int64(600), rateLimitCapacity(10, 60))
	require.Equal(t, int64(math.MaxInt32)*86400, rateLimitCapacity(math.MaxInt32, 86400))
	require.Equal(t, int64(math.MaxInt64), rateLimitCapacity(math.MaxInt32, math.MaxInt64))
	require.Equal(t, int64(math.MaxInt64), rateLimitCapacity(2, math.MaxInt64/2+1))
}
