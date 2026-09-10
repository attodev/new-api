package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// The token cache normally holds a 60-second snapshot of a database row, so both
// quota scripts refresh its TTL on every use -- a snapshot that ages out is always
// re-readable from the row behind it.
//
// The OAuth2 relay token has no row behind it. Redis IS its storage, deliberately
// written with a 24-hour TTL (IssueOAuth2Token), and refreshing that down to the
// quota cache's 60 seconds does not shorten a cache entry, it shortens the session:
// sixty seconds after the user's last request the hash expires, hydrateTokenCache
// falls through to a table with no matching row, and every call after that is 401
// until the user signs in again. Observed as the model list silently emptying on a
// reload, which a sign-out and sign-in "fixed".
func TestTokenQuotaScriptsDoNotShortenTheOAuth2TokenTTL(t *testing.T) {
	setupOAuth2TestRedis(t)
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
			token, err := IssueOAuth2Token(3, "default", "openwebui")
			require.NoError(t, err)
			cacheKey := getTokenCacheKey(token.Key)

			require.Greater(t, common.RDB.TTL(ctx, cacheKey).Val(), 23*time.Hour,
				"precondition: the freshly issued token holds its full session TTL")

			require.NoError(t, tc.call(token))

			require.Greater(t, common.RDB.TTL(ctx, cacheKey).Val(), 23*time.Hour,
				"using the token must not cut its session down to the quota cache TTL")
		})
	}
}

// The other direction, and the reason the fix is "never shorten" rather than "never
// touch": a database-backed token's cache entry must still have its TTL pushed back
// on use, or a busy token would fall out of cache every 60 seconds and re-read the
// row it is meant to be caching.
func TestTokenQuotaScriptsStillExtendAShortTTL(t *testing.T) {
	setupOAuth2TestRedis(t)
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
