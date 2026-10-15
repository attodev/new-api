package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func resetUserCacheRepairForTest(t *testing.T) {
	t.Helper()
	userCacheRepairMu.Lock()
	userCacheRepairIDs = map[int]struct{}{}
	userCacheRepairMu.Unlock()
	t.Cleanup(func() {
		userCacheRepairMu.Lock()
		userCacheRepairIDs = map[int]struct{}{}
		userCacheRepairMu.Unlock()
	})
}

func TestUserCacheRepairDeletesStaleHashAfterRedisError(t *testing.T) {
	truncateTables(t)
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9701, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	// Redis "down": the reserve falls back to the database and the hash keeps 1000.
	server.SetError("simulated outage")
	ok, err := TryReserveUserQuota(user.Id, 100)
	server.SetError("")
	require.NoError(t, err)
	require.True(t, ok)
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, cached.Quota, "stale: DB is 900")

	repairUserCaches()
	require.False(t, server.Exists(getUserCacheKey(user.Id)), "stale hash removed")
	again, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 900, again.Quota)
}

func TestUserCacheRepairWaitsForPendingBatch(t *testing.T) {
	truncateTables(t)
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9702, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_ = noteUserCacheSyncFailure(user.Id, errors.New("boom"))
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)

	repairUserCaches()
	require.True(t, server.Exists(getUserCacheKey(user.Id)), "kept while a delta is pending")
	batchUpdate()
	repairUserCaches()
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
}

func TestNoteUserCacheSyncFailureIgnoresNil(t *testing.T) {
	resetUserCacheRepairForTest(t)
	useUserCacheMiniRedis(t)
	require.NoError(t, noteUserCacheSyncFailure(1, nil))
	userCacheRepairMu.Lock()
	_, listed := userCacheRepairIDs[1]
	userCacheRepairMu.Unlock()
	require.False(t, listed)
}

func TestNoteUserCacheSyncFailureReturnsErrorUnchanged(t *testing.T) {
	resetUserCacheRepairForTest(t)
	useUserCacheMiniRedis(t)
	boom := errors.New("boom")
	require.Same(t, boom, noteUserCacheSyncFailure(5, boom))
	userCacheRepairMu.Lock()
	_, listed := userCacheRepairIDs[5]
	userCacheRepairMu.Unlock()
	require.True(t, listed)
}

func TestCacheIncrUserQuotaFailureIsListed(t *testing.T) {
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	server.SetError("simulated outage")
	err := cacheIncrUserQuota(77, 5)
	server.SetError("")
	require.Error(t, err)
	userCacheRepairMu.Lock()
	_, listed := userCacheRepairIDs[77]
	userCacheRepairMu.Unlock()
	require.True(t, listed)
}

func TestUserCacheRepairKeepsListedUserWhenRedisStillDown(t *testing.T) {
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	_ = noteUserCacheSyncFailure(88, errors.New("boom"))
	server.SetError("simulated outage")
	repairUserCaches()
	server.SetError("")
	userCacheRepairMu.Lock()
	_, listed := userCacheRepairIDs[88]
	userCacheRepairMu.Unlock()
	require.True(t, listed, "stays listed so a later pass retries")
	repairUserCaches()
	userCacheRepairMu.Lock()
	_, listed = userCacheRepairIDs[88]
	userCacheRepairMu.Unlock()
	require.False(t, listed)
}
