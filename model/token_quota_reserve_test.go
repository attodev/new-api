package model

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useTokenQuotaMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	oldSyncFrequency := common.SyncFrequency
	common.RedisEnabled = true
	common.SyncFrequency = 2
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
		common.SyncFrequency = oldSyncFrequency
	})
	return server
}

func TestTokenQuotaDeltaIsVisibleInCacheBeforeReturn(t *testing.T) {
	truncateTables(t)
	resetTokenQuotaBatchState(t)
	useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true
	tok := insertTokenWithQuota(t, 20)

	_, err := GetTokenByKey(tok.Key, true)
	require.NoError(t, err)
	require.NoError(t, DecreaseTokenQuota(tok.Id, tok.Key, 7))

	cached, err := cacheGetTokenByKey(tok.Key)
	require.NoError(t, err)
	require.Equal(t, 13, cached.RemainQuota)
	require.Equal(t, 7, cached.UsedQuota)
}

func TestTokenQuotaMutationsRefreshCacheTTLWhileBatchPending(t *testing.T) {
	truncateTables(t)
	resetTokenQuotaBatchState(t)
	server := useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true
	tok := insertTokenWithQuota(t, 20)

	_, err := GetTokenByKey(tok.Key, true)
	require.NoError(t, err)
	server.FastForward(1500 * time.Millisecond)

	reserved, err := TryReserveTokenQuota(tok.Id, tok.Key, 7, false)
	require.NoError(t, err)
	require.True(t, reserved)
	require.Greater(t, server.TTL(getTokenCacheKey(tok.Key)), time.Second)

	server.FastForward(1500 * time.Millisecond)
	require.NoError(t, DecreaseTokenQuota(tok.Id, tok.Key, 1))
	require.Greater(t, server.TTL(getTokenCacheKey(tok.Key)), time.Second)

	// The original TTL and the reservation-refreshed TTL have both elapsed.
	// The delta refresh must keep the authoritative cache available while its
	// database updates are still pending.
	server.FastForward(time.Second)
	_, err = GetTokenByKey(tok.Key, false)
	require.NoError(t, err)
	require.True(t, hasPendingBatchRecord(BatchUpdateTypeTokenQuota, tok.Id))
}

func TestTokenReserveFailsClosedWhenCacheMissingWithPendingBatch(t *testing.T) {
	truncateTables(t)
	resetTokenQuotaBatchState(t)
	useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true
	tok := insertTokenWithQuota(t, 9)

	reserved, err := TryReserveTokenQuota(tok.Id, tok.Key, 7, false)
	require.NoError(t, err)
	require.True(t, reserved)
	require.NoError(t, common.RDB.Del(context.Background(), getTokenCacheKey(tok.Key)).Err())
	_, err = GetTokenByKey(tok.Key, false)
	require.ErrorIs(t, err, ErrQuotaCachePending)

	reserved, err = TryReserveTokenQuota(tok.Id, tok.Key, 1, false)
	require.ErrorIs(t, err, ErrQuotaCachePending)
	require.False(t, reserved)
}

func TestHasPendingBatchRecordIncludesInFlightDelta(t *testing.T) {
	resetTokenQuotaBatchState(t)
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
	batchUpdateInFlight[BatchUpdateTypeTokenQuota][42] = -7
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	require.True(t, hasPendingBatchRecord(BatchUpdateTypeTokenQuota, 42))
}

func resetTokenQuotaBatchState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
	batchUpdateStores[BatchUpdateTypeTokenQuota] = make(map[int]int64)
	batchUpdateInFlight[BatchUpdateTypeTokenQuota] = make(map[int]int64)
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
		batchUpdateStores[BatchUpdateTypeTokenQuota] = make(map[int]int64)
		batchUpdateInFlight[BatchUpdateTypeTokenQuota] = make(map[int]int64)
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	})
}

func insertTokenWithQuota(t *testing.T, remainQuota int) *Token {
	t.Helper()
	tok := &Token{
		UserId:      1,
		Key:         randomTestKey(t),
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
	}
	require.NoError(t, DB.Create(tok).Error)
	return tok
}

func randomTestKey(t *testing.T) string {
	t.Helper()
	return t.Name()
}

func TestTryDecreaseTokenQuota_SucceedsWhenEnough(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 100)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, 40)
	require.NoError(t, err)
	require.True(t, ok)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 60, reloaded.RemainQuota)
	require.Equal(t, 40, reloaded.UsedQuota)
}

func TestTryDecreaseTokenQuota_RejectsWhenInsufficient(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 10)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, 40)
	require.NoError(t, err)
	require.False(t, ok)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 10, reloaded.RemainQuota, "balance must be untouched on rejection")
}

func TestTryDecreaseTokenQuota_RejectsNegativeAmount(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 10)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, -5)
	require.Error(t, err)
	require.False(t, ok)
}

// TestTryDecreaseTokenQuota_ConcurrentRequestsCannotOverspend reproduces the
// race a plain "read remain_quota, check, then decrease" sequence allows:
// many concurrent requests each read the same balance, all pass the check,
// and all decrement - driving the balance negative by far more than any
// single check actually permitted. The guarded UPDATE (remain_quota >= ?)
// must let through only as many callers as the starting balance covers.
func TestTryDecreaseTokenQuota_ConcurrentRequestsCannotOverspend(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 100)

	const cost = 30
	const attempts = 10 // only floor(100/30) = 3 of these may succeed

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, cost)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 3, successes, "exactly floor(100/30) attempts should have succeeded")

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 100-3*cost, reloaded.RemainQuota)
	require.GreaterOrEqual(t, reloaded.RemainQuota, 0, "balance must never go negative under concurrency")
}

func TestRedisBatchReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	truncateTables(t)
	resetTokenQuotaBatchState(t)
	useTokenQuotaMiniRedis(t)
	common.BatchUpdateEnabled = true

	tok := insertTokenWithQuota(t, 9)

	reserved, err := TryReserveTokenQuota(tok.Id, tok.Key, 7, false)
	require.NoError(t, err)
	require.True(t, reserved)

	var beforeFlush Token
	require.NoError(t, DB.First(&beforeFlush, tok.Id).Error)
	require.Equal(t, 9, beforeFlush.RemainQuota, "batch delta must still be pending in the database")

	reserved, err = TryReserveTokenQuota(tok.Id, tok.Key, 3, false)
	require.NoError(t, err)
	require.False(t, reserved, "stale database balance must not authorize a second spend")

	cached, err := cacheGetTokenByKey(tok.Key)
	require.NoError(t, err)
	require.Equal(t, 2, cached.RemainQuota)
	require.Equal(t, 7, cached.UsedQuota)

	batchUpdate()
	var afterFlush Token
	require.NoError(t, DB.First(&afterFlush, tok.Id).Error)
	require.Equal(t, 2, afterFlush.RemainQuota)
	require.Equal(t, 7, afterFlush.UsedQuota)
}
