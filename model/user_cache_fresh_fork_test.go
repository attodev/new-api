package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func setHashField(t *testing.T, userID int, field string, value any) {
	t.Helper()
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(userID), field, value).Err())
}

func TestFreshUntilRefreshKeepsQuotaAndTTL(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9301, 1000)
	first, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id))
	require.Greater(t, first.FreshUntil, common.GetTimestamp())

	// cache spends 100 (DB still 1000), profile changes in DB
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("email", "new@example.com").Error)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 1000*time.Second).Err())

	setHashField(t, user.Id, "FreshUntil", common.GetTimestamp()-1) // fresh-until passes, TTL does not
	second, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", second.Email, "profile re-read from DB")
	require.Equal(t, 900, second.Quota, "cached balance kept and returned")
	require.Equal(t, 1000*time.Second, userHashTTL(t, user.Id), "refresh does not extend TTL")
	require.Greater(t, second.FreshUntil, common.GetTimestamp(), "fresh again")
}

func TestGetUserCacheReturnsCachedQuotaAfterInfoRefresh(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9302, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = cacheApplyUserQuotaDelta(user.Id, -250)
	require.NoError(t, err)
	setHashField(t, user.Id, "FreshUntil", 1) // force stale
	got, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 750, got.Quota)
}

func TestStaleUserInfoRejectedWhileAuthFencePending(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9303, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	setHashField(t, user.Id, "FreshUntil", 1)
	var fresh User
	require.NoError(t, DB.First(&fresh, user.Id).Error)
	require.NoError(t, SetUserAuthVersionFence(user.Id, fresh.AuthVersion+1))
	_, err = GetUserCache(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending, "stale info is not served while a newer version is pending")
}

func TestWriteUserCacheMatchesUpstreamWhenResidencyOff(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 0)
	user := insertUserForPaymentGuardTest(t, 9304, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 2*time.Second, userHashTTL(t, user.Id))
	exists, err := common.RDB.HExists(t.Context(), getUserCacheKey(user.Id), "FreshUntil").Result()
	require.NoError(t, err)
	require.False(t, exists)
}
