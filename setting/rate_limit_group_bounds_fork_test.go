package setting

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// develop caps counts at maxModelRequestRateLimitCount (MaxInt64 / 24h in
// seconds), not the MaxInt32 cap team used.
func TestCheckModelRequestRateLimitGroupBounds(t *testing.T) {
	limit := maxModelRequestRateLimitCount
	require.NoError(t, CheckModelRequestRateLimitGroup(fmt.Sprintf(`{"vip":[%d,%d]}`, limit, limit)))
	require.EqualError(t,
		CheckModelRequestRateLimitGroup(fmt.Sprintf(`{"vip":[%d,1]}`, limit+1)),
		fmt.Sprintf("group vip [%d, 1] exceeds max rate limit %d", limit+1, limit))
	require.EqualError(t,
		CheckModelRequestRateLimitGroup(fmt.Sprintf(`{"vip":[1,%d]}`, limit+1)),
		fmt.Sprintf("group vip [1, %d] exceeds max rate limit %d", limit+1, limit))
	require.EqualError(t, CheckModelRequestRateLimitGroup(`{"vip":[-1,1]}`), "group vip has negative rate limit values: [-1, 1]")
	require.EqualError(t, CheckModelRequestRateLimitGroup(`{"vip":[1,0]}`), "group vip has negative rate limit values: [1, 0]")
	require.Error(t, CheckModelRequestRateLimitGroup(`not json`))
}

// The accepted count bound must keep the token-bucket capacity count*duration
// inside int64 for every window up to a day, so the middleware's saturating
// guard is a backstop rather than the normal path.
func TestModelRequestRateLimitCountBoundCannotWrapCapacity(t *testing.T) {
	require.LessOrEqual(t, maxModelRequestRateLimitCount, int64(math.MaxInt64)/maxRateLimitDurationSeconds)
	require.Positive(t, maxModelRequestRateLimitCount*maxRateLimitDurationSeconds)
}

func TestModelRequestRateLimitGroupJSONRoundTrip(t *testing.T) {
	ModelRequestRateLimitMutex.RLock()
	old := ModelRequestRateLimitGroup
	ModelRequestRateLimitMutex.RUnlock()
	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		ModelRequestRateLimitGroup = old
		ModelRequestRateLimitMutex.Unlock()
	})

	require.NoError(t, UpdateModelRequestRateLimitGroupByJSONString(`{"vip":[10,20]}`))
	total, success, found := GetGroupRateLimit("vip")
	require.True(t, found)
	require.Equal(t, 10, total)
	require.Equal(t, 20, success)
	require.JSONEq(t, `{"vip":[10,20]}`, ModelRequestRateLimitGroup2JSONString())

	_, _, found = GetGroupRateLimit("missing")
	require.False(t, found)
}
