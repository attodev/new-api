package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

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

func userCacheRepairListed(id int) bool {
	userCacheRepairMu.Lock()
	defer userCacheRepairMu.Unlock()
	_, ok := userCacheRepairIDs[id]
	return ok
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
	require.True(t, userCacheRepairListed(user.Id), "re-added while pending")
	batchUpdate()
	repairUserCaches()
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
	require.False(t, userCacheRepairListed(user.Id), "removed once repaired")
}

func TestUserCacheRepairKeepsNoteRecordedDuringPass(t *testing.T) {
	resetUserCacheRepairForTest(t)
	useUserCacheMiniRedis(t)
	_ = noteUserCacheSyncFailure(31, errors.New("boom"))
	ids := takeUserCacheRepairIDs()
	require.Equal(t, []int{31}, ids)
	require.False(t, userCacheRepairListed(31), "list cleared by the take")
	// a failure noted while the pass is in progress
	_ = noteUserCacheSyncFailure(32, errors.New("boom"))
	repairUserCachesFor(ids)
	require.False(t, userCacheRepairListed(31))
	require.True(t, userCacheRepairListed(32), "note during the pass survives")
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

func TestUserCacheRepairRechecksPendingAfterDelete(t *testing.T) {
	truncateTables(t)
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9703, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_ = noteUserCacheSyncFailure(user.Id, errors.New("boom"))

	userCacheRepairAfterDelete = func(id int) { addNewRecord(BatchUpdateTypeUserQuota, id, -100) }
	t.Cleanup(func() { userCacheRepairAfterDelete = nil })
	repairUserCaches()
	userCacheRepairAfterDelete = nil
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
	require.True(t, userCacheRepairListed(user.Id), "delta slipped in after the check: listed again")

	batchUpdate()
	repairUserCaches()
	require.False(t, userCacheRepairListed(user.Id))
}

func TestUserCacheRepairRedisDownRequeuesRemainingOnce(t *testing.T) {
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	for _, id := range []int{41, 42, 43} {
		_ = noteUserCacheSyncFailure(id, errors.New("boom"))
	}
	server.SetError("simulated outage")
	repairUserCaches()
	server.SetError("")
	for _, id := range []int{41, 42, 43} {
		require.True(t, userCacheRepairListed(id), "id %d stays listed", id)
	}
	repairUserCaches()
	for _, id := range []int{41, 42, 43} {
		require.False(t, userCacheRepairListed(id))
	}
}

func TestFlushBatchUpdatesRunsRepairWithBatchOff(t *testing.T) {
	truncateTables(t)
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9704, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.True(t, server.Exists(getUserCacheKey(user.Id)))
	_ = noteUserCacheSyncFailure(user.Id, errors.New("boom"))

	prev := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = prev })
	FlushBatchUpdates()
	require.False(t, server.Exists(getUserCacheKey(user.Id)), "repair pass ran on shutdown")
	require.False(t, userCacheRepairListed(user.Id))
}

// A reserve that misses the hash and cannot hydrate it (here: an auth fence newer
// than the database row) falls back to the database with a nil error. The hash may be
// written later from a pre-deduction snapshot, so the user is listed for repair too.
func TestReserveMissFallbackWithoutErrorIsListed(t *testing.T) {
	truncateTables(t)
	resetUserCacheRepairForTest(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9703, 1000)
	require.False(t, server.Exists(getUserCacheKey(user.Id)), "hash absent")
	require.NoError(t, SetUserAuthVersionFence(user.Id, 99))

	ok, err := TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 900, getUserQuotaFromDB(t, user.Id), "served by the database fallback")
	require.False(t, server.Exists(getUserCacheKey(user.Id)), "fence rejected hydration")
	require.True(t, userCacheRepairListed(user.Id), "nil-error miss fallback is listed")
}
