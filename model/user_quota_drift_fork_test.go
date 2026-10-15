package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type driftCall struct{ userID, cacheQuota, dbQuota, diff int }

func captureDrift(t *testing.T) *[]driftCall {
	t.Helper()
	old := userQuotaDriftReporter
	var calls []driftCall
	userQuotaDriftReporter = func(userID, cacheQuota, dbQuota, diff int) {
		calls = append(calls, driftCall{userID, cacheQuota, dbQuota, diff})
	}
	resetUserQuotaDriftMemory()
	t.Cleanup(func() { userQuotaDriftReporter = old; resetUserQuotaDriftMemory() })
	return &calls
}

// driftClock pins userQuotaDriftNow; advance moves it by one SYNC_FREQUENCY interval.
type driftClock struct{ now int64 }

func (c *driftClock) advance() { c.now += int64(userCacheTTLSeconds()) }

func useDriftClock(t *testing.T) *driftClock {
	t.Helper()
	old := userQuotaDriftNow
	clock := &driftClock{now: 1_000_000}
	userQuotaDriftNow = func() int64 { return clock.now }
	t.Cleanup(func() { userQuotaDriftNow = old })
	return clock
}

func TestUserQuotaDriftLogsOnlyWhenStableAcrossTwoObservations(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	enableBatchForResidencyTest(t)
	observeUserQuotaDrift(1, 900, 1000) // first sight: remember only
	require.Empty(t, *calls)
	clock.advance()
	observeUserQuotaDrift(1, 900, 1000) // same cache, same diff → drift
	require.Len(t, *calls, 1)
	require.Equal(t, driftCall{1, 900, 1000, -100}, (*calls)[0])

	observeUserQuotaDrift(2, 900, 1000)
	clock.advance()
	observeUserQuotaDrift(2, 800, 900) // cache moved → user is active, no log
	require.Len(t, *calls, 1)

	observeUserQuotaDrift(3, 900, 1000)
	clock.advance()
	observeUserQuotaDrift(3, 900, 900) // diff gone → cleared
	clock.advance()
	observeUserQuotaDrift(3, 900, 900)
	require.Len(t, *calls, 1)
}

func TestUserQuotaDriftIgnoresOtherNodePendingWindow(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	enableBatchForResidencyTest(t)
	// Another node holds -100 at the first observation; it flushes before the second.
	observeUserQuotaDrift(4, 900, 1000)
	clock.advance()
	observeUserQuotaDrift(4, 900, 900)
	require.Empty(t, *calls)
}

// Spec §4.7 step 3: the second observation must be at least SYNC_FREQUENCY after the
// first. Two concurrent re-reads at the FreshUntil boundary see the same values in the
// same second while another node's delta may still be pending; that is not drift.
func TestUserQuotaDriftRequiresSyncIntervalBetweenObservations(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	enableBatchForResidencyTest(t)
	observeUserQuotaDrift(5, 900, 1000)
	observeUserQuotaDrift(5, 900, 1000) // same second: too close, ignored
	require.Empty(t, *calls)

	clock.now += int64(userCacheTTLSeconds()) - 1
	observeUserQuotaDrift(5, 900, 1000) // still inside the interval of the first
	require.Empty(t, *calls, "an early observation neither reports nor resets the first")

	clock.now++
	observeUserQuotaDrift(5, 900, 1000) // a full interval after the first
	require.Equal(t, []driftCall{{5, 900, 1000, -100}}, *calls)
}

func TestUserQuotaDriftEarlyObservationKeepsFirst(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	enableBatchForResidencyTest(t)
	observeUserQuotaDrift(6, 900, 1000)
	observeUserQuotaDrift(6, 800, 900) // early and different: must not replace the first
	clock.advance()
	observeUserQuotaDrift(6, 900, 1000)
	require.Equal(t, []driftCall{{6, 900, 1000, -100}}, *calls)
}

func TestUserQuotaDriftSkipsLocalPendingUsers(t *testing.T) {
	calls := captureDrift(t)
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9801, 1000)
	clock := useDriftClock(t)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	observeUserQuotaDrift(user.Id, 900, 1000)
	clock.advance()
	observeUserQuotaDrift(user.Id, 900, 1000)
	require.Empty(t, *calls, "local pending → never compared")
}

// End to end through GetUserCache: the SYNC_FREQUENCY re-read is the observation point.
func TestUserQuotaDriftEndToEndReportsUnexplainedDifference(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9802, 1000)
	_, err := GetUserCache(user.Id) // populate: cache == DB, nothing remembered
	require.NoError(t, err)
	require.Empty(t, *calls)

	// cache spends 100, DB never gets it (batch off, nothing pending)
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)

	setHashField(t, user.Id, "FreshUntil", 1)
	got, err := GetUserCache(user.Id) // first observation: remember only
	require.NoError(t, err)
	require.Equal(t, 900, got.Quota)
	require.Empty(t, *calls)

	clock.advance()
	setHashField(t, user.Id, "FreshUntil", 1)
	_, err = GetUserCache(user.Id) // second observation: unchanged cache and diff
	require.NoError(t, err)
	require.Equal(t, []driftCall{{user.Id, 900, 1000, -100}}, *calls)
}

func TestUserQuotaDriftEndToEndSilentWhileBatchPending(t *testing.T) {
	calls := captureDrift(t)
	clock := useDriftClock(t)
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9803, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100) // this node's pending delta

	for i := 0; i < 3; i++ {
		clock.advance()
		setHashField(t, user.Id, "FreshUntil", 1)
		got, err := GetUserCache(user.Id)
		require.NoError(t, err)
		require.Equal(t, 900, got.Quota)
	}
	require.Empty(t, *calls)
}
