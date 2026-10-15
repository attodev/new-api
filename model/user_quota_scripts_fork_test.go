package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func userHashTTL(t *testing.T, userID int) time.Duration {
	t.Helper()
	ttl, err := common.RDB.TTL(t.Context(), getUserCacheKey(userID)).Result()
	require.NoError(t, err)
	return ttl
}

func TestUserQuotaReserveExtendsResidentTTLOnlyOnSuccess(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9201, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())

	result, err := cacheTryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id), "success extends to quota TTL")

	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())
	result, err = cacheTryReserveUserQuota(user.Id, 5000)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaInsufficient, result)
	require.Equal(t, 5*time.Second, userHashTTL(t, user.Id), "insufficient leaves TTL alone")

	result, err = cacheApplyUserQuotaDelta(user.Id, 50)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id), "delta extends too")
}

func TestUserQuotaReserveKeepsUpstreamTTLWhenResidencyOff(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 0)
	user := insertUserForPaymentGuardTest(t, 9202, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())

	result, err := cacheTryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 5*time.Second, userHashTTL(t, user.Id), "off: no extension")
}
