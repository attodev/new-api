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

func TestUserQuotaDriftLogsOnlyWhenStableAcrossTwoObservations(t *testing.T) {
	calls := captureDrift(t)
	enableBatchForResidencyTest(t)
	observeUserQuotaDrift(1, 900, 1000) // first sight: remember only
	require.Empty(t, *calls)
	observeUserQuotaDrift(1, 900, 1000) // same cache, same diff → drift
	require.Len(t, *calls, 1)
	require.Equal(t, driftCall{1, 900, 1000, -100}, (*calls)[0])

	observeUserQuotaDrift(2, 900, 1000)
	observeUserQuotaDrift(2, 800, 900) // cache moved → user is active, no log
	require.Len(t, *calls, 1)

	observeUserQuotaDrift(3, 900, 1000)
	observeUserQuotaDrift(3, 900, 900) // diff gone → cleared
	observeUserQuotaDrift(3, 900, 900)
	require.Len(t, *calls, 1)
}

func TestUserQuotaDriftIgnoresOtherNodePendingWindow(t *testing.T) {
	calls := captureDrift(t)
	enableBatchForResidencyTest(t)
	// Another node holds -100 at the first observation; it flushes before the second.
	observeUserQuotaDrift(4, 900, 1000)
	observeUserQuotaDrift(4, 900, 900)
	require.Empty(t, *calls)
}

func TestUserQuotaDriftSkipsLocalPendingUsers(t *testing.T) {
	calls := captureDrift(t)
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9801, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	observeUserQuotaDrift(user.Id, 900, 1000)
	observeUserQuotaDrift(user.Id, 900, 1000)
	require.Empty(t, *calls, "local pending → never compared")
}

// End to end through GetUserCache: the SYNC_FREQUENCY re-read is the observation point.
func TestUserQuotaDriftEndToEndReportsUnexplainedDifference(t *testing.T) {
	calls := captureDrift(t)
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

	setHashField(t, user.Id, "FreshUntil", 1)
	_, err = GetUserCache(user.Id) // second observation: unchanged cache and diff
	require.NoError(t, err)
	require.Equal(t, []driftCall{{user.Id, 900, 1000, -100}}, *calls)
}

func TestUserQuotaDriftEndToEndSilentWhileBatchPending(t *testing.T) {
	calls := captureDrift(t)
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
		setHashField(t, user.Id, "FreshUntil", 1)
		got, err := GetUserCache(user.Id)
		require.NoError(t, err)
		require.Equal(t, 900, got.Quota)
	}
	require.Empty(t, *calls)
}
