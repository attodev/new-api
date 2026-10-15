package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func enableBatchForResidencyTest(t *testing.T) {
	t.Helper()
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		batchUpdate()
		common.BatchUpdateEnabled = old
	})
}

// Before residency: the hash expired inside the batch window, the next reserve
// rehydrated the stale DB balance, and after the flush DB=800 while cache=900.
func TestResidentQuotaCacheSurvivesBatchWindow(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)

	user := insertUserForPaymentGuardTest(t, 9203, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	ok, err := TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, ok)

	server.FastForward(10 * time.Second) // longer than SYNC_FREQUENCY, shorter than quota TTL
	require.True(t, server.Exists(getUserCacheKey(user.Id)), "hash must survive")

	ok, err = TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, ok)

	batchUpdate()
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	cachedQuota, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	require.Equal(t, 800, dbUser.Quota)
	require.Equal(t, dbUser.Quota, cachedQuota)

	ok, err = TryReserveUserQuota(user.Id, 900)
	require.NoError(t, err)
	require.False(t, ok, "cannot spend more than the real balance")
}

func TestIdleUserHashExpiresAfterQuotaTTL(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 30)
	user := insertUserForPaymentGuardTest(t, 9204, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	server.FastForward(31 * time.Second)
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, cached.Quota, "rehydrated from DB after idle expiry")
}
