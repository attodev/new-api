package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUserQuotaBatchPendingCoversQueueAndInFlight(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9501, 1000)

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

func TestBatchUpdateClearsInFlightPerRow(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	// The shared helper leaves aff_code empty (unique), so two users need distinct codes.
	a := &User{Id: 9502, Username: "inflight_a", Status: common.UserStatusEnabled, Quota: 1000, AffCode: "inflight_a"}
	b := &User{Id: 9503, Username: "inflight_b", Status: common.UserStatusEnabled, Quota: 1000, AffCode: "inflight_b"}
	require.NoError(t, DB.Create(a).Error)
	require.NoError(t, DB.Create(b).Error)
	addNewRecord(BatchUpdateTypeUserQuota, a.Id, -100)
	addNewRecord(BatchUpdateTypeUserQuota, b.Id, -200)
	batchUpdate()
	require.False(t, userQuotaBatchPending(a.Id))
	require.False(t, userQuotaBatchPending(b.Id))
	var dbA, dbB User
	require.NoError(t, DB.First(&dbA, a.Id).Error)
	require.NoError(t, DB.First(&dbB, b.Id).Error)
	require.Equal(t, 900, dbA.Quota)
	require.Equal(t, 800, dbB.Quota)
}
