package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func resetUserQuotaBatchState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int64)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int64)
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	})
}

func insertUserWithQuota(t *testing.T, quota int64) *User {
	t.Helper()
	user := &User{Username: t.Name(), Password: "test", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func TestTryReserveUserQuotaConcurrentRequestsCannotOverspend(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	user := insertUserWithQuota(t, 100)

	const cost int64 = 30
	const attempts = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := TryReserveUserQuota(user.Id, cost)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 3, successes)
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, int64(10), reloaded.Quota)
}

func TestRedisBatchUserReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := insertUserWithQuota(t, 9)

	reserved, err := TryReserveUserQuota(user.Id, 7)
	require.NoError(t, err)
	require.True(t, reserved)

	reserved, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	require.False(t, reserved)

	var beforeFlush User
	require.NoError(t, DB.First(&beforeFlush, user.Id).Error)
	require.Equal(t, int64(9), beforeFlush.Quota)

	batchUpdate()
	var afterFlush User
	require.NoError(t, DB.First(&afterFlush, user.Id).Error)
	require.Equal(t, int64(2), afterFlush.Quota)
}

func TestUserQuotaDeltaIsVisibleInCacheBeforeReturn(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := insertUserWithQuota(t, 20)

	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, DecreaseUserQuota(user.Id, 7, false))

	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(13), cached.Quota)
}
