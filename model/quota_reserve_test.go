package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupQuotaTestRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
	})
}

// The token cache normally holds a 60-second snapshot of a database row, so both
// quota scripts refresh its TTL on every use -- a snapshot that ages out is always
// re-readable from the row behind it.
//
// A cache entry whose TTL is its lifetime (no row behind it) must not have that
// lifetime cut down to the quota cache's 60 seconds just because it was used.
func TestTokenQuotaScriptsDoNotShortenALongTTL(t *testing.T) {
	setupQuotaTestRedis(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func(token *Token) error
	}{
		{
			name: "reserve",
			call: func(token *Token) error {
				_, err := cacheTryReserveTokenQuota(token.Id, token.Key, 1)
				return err
			},
		},
		{
			name: "delta",
			call: func(token *Token) error {
				_, err := cacheApplyTokenQuotaDelta(token.Id, token.Key, 1)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := &Token{Id: 4321, Key: "long-lived-token-key-" + tc.name, RemainQuota: 500}
			cacheKey := getTokenCacheKey(token.Key)
			require.NoError(t, common.RDB.HSet(ctx, cacheKey, map[string]interface{}{
				"Id": token.Id, "RemainQuota": token.RemainQuota, "UsedQuota": 0,
			}).Err())
			require.NoError(t, common.RDB.Expire(ctx, cacheKey, 24*time.Hour).Err())

			require.NoError(t, tc.call(token))

			require.Greater(t, common.RDB.TTL(ctx, cacheKey).Val(), 23*time.Hour,
				"using the entry must not cut its lifetime down to the quota cache TTL")
		})
	}
}

// The other direction, and the reason the fix is "never shorten" rather than "never
// touch": a database-backed token's cache entry must still have its TTL pushed back
// on use, or a busy token would fall out of cache every 60 seconds and re-read the
// row it is meant to be caching.
func TestTokenQuotaScriptsStillExtendAShortTTL(t *testing.T) {
	setupQuotaTestRedis(t)
	ctx := context.Background()

	token := Token{
		Id:             1234,
		UserId:         7,
		Key:            "database-backed-token-key",
		Status:         common.TokenStatusEnabled,
		Name:           "ordinary",
		ExpiredTime:    -1,
		RemainQuota:    500,
		UsedQuota:      0,
		UnlimitedQuota: false,
	}
	cacheKey := getTokenCacheKey(token.Key)
	require.NoError(t, common.RDB.HSet(ctx, cacheKey, map[string]interface{}{
		"Id": token.Id, "RemainQuota": token.RemainQuota, "UsedQuota": token.UsedQuota,
	}).Err())
	require.NoError(t, common.RDB.Expire(ctx, cacheKey, 5*time.Second).Err())

	_, err := cacheTryReserveTokenQuota(token.Id, token.Key, 1)
	require.NoError(t, err)

	ttl := common.RDB.TTL(ctx, cacheKey).Val()
	require.Greater(t, ttl, 30*time.Second, "a nearly-expired cache entry must be pushed back")
	require.LessOrEqual(t, ttl, time.Duration(quotaCacheTTLSeconds())*time.Second)
}
