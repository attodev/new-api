package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

// takeUserQuotaStoreForTest takes the user-quota store the way batchUpdate does
// (marking its users in flight) and returns it.
func takeUserQuotaStoreForTest(t *testing.T) map[int]int {
	t.Helper()
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	store := takeBatchStoreLocked(BatchUpdateTypeUserQuota)
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
	return store
}

func TestFlushUserQuotaRowRequeuesOnDBError(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	user := insertUserForPaymentGuardTest(t, 9601, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	quotaStore := takeUserQuotaStoreForTest(t)
	require.Equal(t, -100, quotaStore[user.Id])

	// Make the single users-row write fail once.
	const cbName = "fork_test_fail_users_update"
	failed := false
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if failed || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "users" {
			return
		}
		failed = true
		_ = tx.AddError(errors.New("injected users update failure"))
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(cbName) })

	flushUserQuotaRow(user.Id, quotaStore, 100, 1)
	require.True(t, failed)

	require.True(t, userQuotaBatchPending(user.Id), "requeued after failure")
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	require.Equal(t, -100, batchUpdateStores[BatchUpdateTypeUserQuota][user.Id])
	require.Equal(t, 0, userQuotaInFlight[user.Id], "mark cleared after requeue")
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	require.Equal(t, 100, batchUpdateStores[BatchUpdateTypeUsedQuota][user.Id])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeRequestCount].Lock()
	require.Equal(t, 1, batchUpdateStores[BatchUpdateTypeRequestCount][user.Id])
	batchUpdateLocks[BatchUpdateTypeRequestCount].Unlock()
	require.Equal(t, 1000, getUserQuotaFromDB(t, user.Id), "failed write changed nothing")

	batchUpdate()
	require.Equal(t, 900, getUserQuotaFromDB(t, user.Id), "retry landed")
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	require.Equal(t, 100, dbUser.UsedQuota)
	require.Equal(t, 1, dbUser.RequestCount)
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestFlushUserQuotaRowKeepsMarkForIDNotTaken(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	user := insertUserForPaymentGuardTest(t, 9603, 1000)

	// Another batch took this user's quota delta and is still writing it.
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -50)
	_ = takeUserQuotaStoreForTest(t)
	require.True(t, userQuotaBatchPending(user.Id))

	// This batch only has a used-quota/request-count entry for the user: its
	// quota store does not contain the id, so it never took a mark.
	flushUserQuotaRow(user.Id, map[int]int{}, 7, 1)

	require.True(t, userQuotaBatchPending(user.Id), "other batch's mark must survive")
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	require.Equal(t, 7, dbUser.UsedQuota)
	require.Equal(t, 1000, dbUser.Quota)
	clearUserQuotaInFlight(user.Id) // the other batch finishes
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestFlushBatchUpdatesWritesQueuedDeltas(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	resetUserQuotaInFlightForTest(t)
	user := insertUserForPaymentGuardTest(t, 9602, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -300)
	FlushBatchUpdates()
	require.Equal(t, 700, getUserQuotaFromDB(t, user.Id))
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestFlushBatchUpdatesIsNoopWhenBatchDisabled(t *testing.T) {
	truncateTables(t)
	resetUserQuotaInFlightForTest(t)
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = old })
	user := insertUserForPaymentGuardTest(t, 9604, 1000)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUserQuota][user.Id] = -300
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	t.Cleanup(func() {
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		delete(batchUpdateStores[BatchUpdateTypeUserQuota], user.Id)
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	})
	FlushBatchUpdates()
	require.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
}
