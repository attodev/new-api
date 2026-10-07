package oidc

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// setupOIDCRedis points common.RDB at a fresh miniredis and rebuilds the
// code/access stores so they use it. Call after setupOIDCTestDB, which
// turns Redis off.
func setupOIDCRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
		ResetStoresForTest()
	})
	// The stores capture common.RDB when built, so rebuild them now.
	ResetStoresForTest()
	return mr
}

func TestCodes_Redis_SingleUseWithTTL(t *testing.T) {
	setupOIDCTestDB(t)
	mr := setupOIDCRedis(t)

	verifier := "a-very-long-code-verifier-string-0123456789"
	code, err := IssueCode(AuthCode{
		UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb",
		Nonce: "n-1", Scope: "openid", CodeChallenge: s256(verifier), CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	key := codes().FullKey(code)
	require.True(t, mr.Exists(key), "code must live in Redis: %v", mr.Keys())
	ttl := mr.TTL(key)
	require.Positive(t, ttl, "code must have a TTL")
	require.LessOrEqual(t, ttl, CodeTTL)

	// A failed binding check does not burn the code.
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "wrong")
	require.ErrorIs(t, err, ErrInvalidGrant)
	require.True(t, mr.Exists(key))

	ac, err := ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.NoError(t, err)
	require.Equal(t, 7, ac.UserId)
	require.Equal(t, "n-1", ac.Nonce)
	require.False(t, mr.Exists(key), "a consumed code is deleted from Redis")

	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant, "reuse fails")
}

func TestCodes_Redis_ConcurrentExchangesYieldOneWinner(t *testing.T) {
	setupOIDCTestDB(t)
	setupOIDCRedis(t)
	code, err := IssueCode(AuthCode{UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb", Scope: "openid"})
	require.NoError(t, err)

	const n = 10
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "")
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, e := range errs {
		if e == nil {
			winners++
		} else {
			require.ErrorIs(t, e, ErrInvalidGrant)
		}
	}
	require.Equal(t, 1, winners)
}

func TestAccessTokens_Redis_IssueLookupRevoke(t *testing.T) {
	setupOIDCTestDB(t)
	mr := setupOIDCRedis(t)

	raw, err := IssueAccessToken(AccessToken{UserId: 7, ClientId: "openwebui", Scope: "openid email"})
	require.NoError(t, err)
	key := accessTokens().FullKey(raw)
	require.True(t, mr.Exists(key), "access token must live in Redis: %v", mr.Keys())
	require.LessOrEqual(t, mr.TTL(key), AccessTokenTTL)
	require.Positive(t, mr.TTL(key))

	at, found, err := LookupAccessToken(raw)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 7, at.UserId)
	require.Equal(t, "openid email", at.Scope)

	_, found, err = LookupAccessToken("unknown")
	require.NoError(t, err)
	require.False(t, found)

	RevokeAccessToken(raw)
	require.False(t, mr.Exists(key))
	_, found, err = LookupAccessToken(raw)
	require.NoError(t, err)
	require.False(t, found)
}
