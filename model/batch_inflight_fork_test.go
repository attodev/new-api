package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// resetUserQuotaInFlightForTest clears the in-flight marks now and when the test ends.
func resetUserQuotaInFlightForTest(t *testing.T) {
	t.Helper()
	reset := func() {
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		userQuotaInFlight = map[int]int{}
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func TestUserQuotaBatchPendingCoversQueueAndInFlight(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	user := createReserveTestUser(t, 1000)

	require.False(t, userQuotaBatchPending(user.Id))
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	require.True(t, userQuotaBatchPending(user.Id), "queued")

	// Simulate the flush window: the store is taken (marking users in flight under
	// the same lock) and replaced, rows not yet written.
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	taken := takeBatchStoreLocked(BatchUpdateTypeUserQuota)
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	require.Contains(t, taken, user.Id)
	require.True(t, userQuotaBatchPending(user.Id), "in flight")

	clearUserQuotaInFlight(user.Id)
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestUserQuotaInFlightIsRefcountedAcrossOverlappingBatches(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	user := createReserveTestUser(t, 1000)

	take := func() {
		addNewRecord(BatchUpdateTypeUserQuota, user.Id, -1)
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		takeBatchStoreLocked(BatchUpdateTypeUserQuota)
		batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	}
	take() // batch A
	take() // batch B overlaps A
	require.True(t, userQuotaBatchPending(user.Id))

	clearUserQuotaInFlight(user.Id) // A finishes
	require.True(t, userQuotaBatchPending(user.Id), "B still owes its delta")
	clearUserQuotaInFlight(user.Id) // B finishes
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestBatchUpdateClearsInFlightPerRow(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	a := createReserveTestUser(t, 1000)
	b := createReserveTestUser(t, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, a.Id, -100)
	addNewRecord(BatchUpdateTypeUserQuota, b.Id, -200)

	// Observe in-flight state at each users-row write. No batch lock is held during
	// the write, so querying userQuotaBatchPending here is safe.
	type snapshot struct{ a, b bool }
	var mu sync.Mutex
	var writes []snapshot
	const cbName = "fork_test_observe_inflight"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "users" {
			return
		}
		snap := snapshot{a: userQuotaBatchPending(a.Id), b: userQuotaBatchPending(b.Id)}
		mu.Lock()
		writes = append(writes, snap)
		mu.Unlock()
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove(cbName)) })

	batchUpdate()

	require.Len(t, writes, 2)
	require.Equal(t, snapshot{true, true}, writes[0], "both users are in flight from the store swap")
	one := writes[1].a != writes[1].b
	require.True(t, one, "at the second write only the not-yet-written user is still in flight, got %+v", writes[1])
	require.False(t, userQuotaBatchPending(a.Id))
	require.False(t, userQuotaBatchPending(b.Id))
	require.Equal(t, 900, getUserQuotaFromDB(t, a.Id))
	require.Equal(t, 800, getUserQuotaFromDB(t, b.Id))
}
