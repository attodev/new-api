package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// fork: O12 — 상주(QUOTA_CACHE_TTL > 0)에서 인증 fence 시나리오. upstream 테스트는
// 상주가 꺼진 상태(테스트 기본값)로 그대로 두고, 여기서는 FreshUntil을 HSET으로
// 지나게 만들어 실제 시계를 기다리지 않고 같은 성질을 고정한다. 설계 §4.5.

func dbAuthVersion(t *testing.T, userID int) int64 {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("auth_version").First(&user, userID).Error)
	return user.AuthVersion
}

// Mirrors upstream TestUserAuthFenceRollbackExpiresAndRecovers
// (model/user_cache_auth_version_test.go) with a resident user hash.
func TestResidentHashRecoversAfterRolledBackFenceExpires(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9311, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 1000*time.Second).Err())
	version := dbAuthVersion(t, user.Id)

	// A restrictive update publishes its fence, then rolls back (DB unchanged).
	require.NoError(t, SetUserAuthVersionFence(user.Id, version+1))
	_, err = GetUserCache(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending, "fresh hash is fenced")
	setHashField(t, user.Id, "FreshUntil", 1)
	_, err = GetUserCache(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending, "stale hash re-read is fenced too")

	fenceTTL := userAuthFenceTTLSeconds()
	server.FastForward(time.Duration(fenceTTL+1) * time.Second)
	require.False(t, server.Exists(getUserAuthFenceKey(user.Id)))
	setHashField(t, user.Id, "FreshUntil", 1)

	got, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, version, got.AuthVersion, "DB version unchanged by the rollback")
	require.Equal(t, 900, got.Quota, "cached balance intact")
	require.Equal(t, time.Duration(1000-fenceTTL-1)*time.Second, userHashTTL(t, user.Id), "re-read does not extend TTL")
	require.Greater(t, got.FreshUntil, common.GetTimestamp())
}

// Mirrors upstream TestIndependentRedisAuthVersionAdvanceConvergesAfterCacheTTL
// (service/auth_session_test.go): node A advances the version on its own
// Redis; node B's resident hash converges once FreshUntil passes instead of
// when the hash TTL runs out.
func TestResidentHashConvergesAfterIndependentNodeAdvancesAuthVersion(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	nodeB := common.RDB
	serverA := miniredis.RunT(t)
	nodeA := redis.NewClient(&redis.Options{Addr: serverA.Addr()})
	t.Cleanup(func() { _ = nodeA.Close() })

	user := insertUserForPaymentGuardTest(t, 9312, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	require.NoError(t, nodeB.Expire(t.Context(), getUserCacheKey(user.Id), 1000*time.Second).Err())
	version := dbAuthVersion(t, user.Id)

	common.RDB = nodeA
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "vip").Error)
	next, err := BumpUserAuthVersion(user.Id)
	common.RDB = nodeB
	require.NoError(t, err)
	require.Equal(t, version+1, next)

	got, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, version, got.AuthVersion, "node B serves its fresh hash until FreshUntil passes")

	setHashField(t, user.Id, "FreshUntil", 1)
	got, err = GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, "vip", got.Group)
	require.Equal(t, next, got.AuthVersion)
	require.Equal(t, 900, got.Quota, "cached balance kept")
	require.Equal(t, 1000*time.Second, userHashTTL(t, user.Id), "re-read does not extend TTL")
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err, "converged hash is served again")
	require.Equal(t, next, cached.AuthVersion)
}

// Shared-Redis variant: a committed floor above the hash version rejects the
// resident hash immediately, even while it is fresh, and the DB re-read
// converges it without touching the balance or its TTL.
func TestResidentHashRejectedByCommittedFloorConvergesFromDB(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9313, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 1000*time.Second).Err())
	version := dbAuthVersion(t, user.Id)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).
		Updates(map[string]any{"group": "vip", "auth_version": version + 1}).Error)
	require.NoError(t, publishCommittedUserAuthVersion(user.Id, version+1))

	_, err = cacheGetUserBase(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending, "fresh hash below the committed floor is rejected")
	got, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, "vip", got.Group)
	require.Equal(t, version+1, got.AuthVersion)
	require.Equal(t, 900, got.Quota, "cached balance kept")
	require.Equal(t, 1000*time.Second, userHashTTL(t, user.Id), "re-read does not extend TTL")
}
