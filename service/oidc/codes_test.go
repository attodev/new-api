// fork: O13 — ported from OpenWebUI service/oidc/codes_test.go.

package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	require.True(t, VerifyPKCE(s256(verifier), "S256", verifier))
	require.False(t, VerifyPKCE(s256(verifier), "S256", verifier+"x"))
	require.False(t, VerifyPKCE(s256(verifier), "plain", verifier))
	require.False(t, VerifyPKCE(s256(verifier), "S256", ""))
	require.True(t, VerifyPKCE("", "", "anything"), "no challenge stored → nothing to check")
}

func TestIssueAndConsumeCode_SingleUseAndBound(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	verifier := "a-very-long-code-verifier-string-0123456789"
	code, err := IssueCode(AuthCode{
		UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb",
		Nonce: "n-1", Scope: "openid email profile",
		CodeChallenge: s256(verifier), CodeChallengeMethod: "S256", AuthTime: 1700000000,
	})
	require.NoError(t, err)
	require.Len(t, code, 48)

	// wrong client
	_, err = ConsumeCode(code, "other", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
	// wrong redirect
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb2", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
	// wrong verifier
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "nope")
	require.ErrorIs(t, err, ErrInvalidGrant)
	// missing verifier when a challenge was stored
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "")
	require.ErrorIs(t, err, ErrInvalidGrant)

	// A failed attempt must not have burned the code: the right call succeeds.
	ac, err := ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.NoError(t, err)
	require.Equal(t, 7, ac.UserId)
	require.Equal(t, "n-1", ac.Nonce)
	require.Equal(t, int64(1700000000), ac.AuthTime)

	// Second use fails.
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)

	// Unknown code fails.
	_, err = ConsumeCode("does-not-exist", "openwebui", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
}

func TestConsumeCode_WithoutPKCEIgnoresVerifier(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	code, err := IssueCode(AuthCode{UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb", Scope: "openid"})
	require.NoError(t, err)
	ac, err := ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "stray-verifier")
	require.NoError(t, err)
	require.Equal(t, 7, ac.UserId)
}

func TestAccessTokenStore(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	raw, err := IssueAccessToken(AccessToken{UserId: 7, ClientId: "openwebui", Scope: "openid email profile"})
	require.NoError(t, err)
	require.Len(t, raw, 48)

	at, found, err := LookupAccessToken(raw)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 7, at.UserId)

	_, found, err = LookupAccessToken("unknown")
	require.NoError(t, err)
	require.False(t, found)

	RevokeAccessToken(raw)
	_, found, err = LookupAccessToken(raw)
	require.NoError(t, err)
	require.False(t, found)
}

func TestConsumeCode_ConcurrentExchangesYieldOneWinner(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
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

// Token-from-key access tokens carry iat/exp and expire on their own,
// shorter TTL even in the memory store (whose default TTL is AccessTokenTTL).
func TestIssueAccessTokenWithTTL(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	before := time.Now().Unix()
	raw, err := IssueAccessTokenWithTTL(AccessToken{UserId: 7, ClientId: "openwebui-mcp", Scope: "openid", TokenId: 3}, 600*time.Second)
	require.NoError(t, err)
	at, found, err := LookupAccessToken(raw)
	require.NoError(t, err)
	require.True(t, found)
	require.GreaterOrEqual(t, at.IssuedAt, before)
	require.Equal(t, int64(600), at.ExpiresAt-at.IssuedAt)
	require.Equal(t, 3, at.TokenId)

	// A TTL above the store's lifetime (or none) falls back to AccessTokenTTL.
	for _, ttl := range []time.Duration{0, -time.Second, 2 * AccessTokenTTL} {
		raw, err := IssueAccessTokenWithTTL(AccessToken{UserId: 7}, ttl)
		require.NoError(t, err)
		at, _, err := LookupAccessToken(raw)
		require.NoError(t, err)
		require.Equal(t, int64(AccessTokenTTL.Seconds()), at.ExpiresAt-at.IssuedAt, ttl)
	}

	short, err := IssueAccessTokenWithTTL(AccessToken{UserId: 7}, time.Second)
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	_, found, err = LookupAccessToken(short)
	require.NoError(t, err)
	require.False(t, found, "a short-lived token must not outlive its exp")
}

// A stored record whose exp has passed is not found, even if the store has
// not evicted it yet; records without exp (issued before iat/exp existed)
// keep working until the store drops them.
func TestLookupAccessToken_HonoursStoredExp(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	require.NoError(t, accessTokens().SetWithTTL("stale", `{"user_id":7,"client_id":"x","scope":"openid","exp":1}`, time.Minute))
	_, found, err := LookupAccessToken("stale")
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, accessTokens().SetWithTTL("legacy", `{"user_id":7,"client_id":"x","scope":"openid"}`, time.Minute))
	at, found, err := LookupAccessToken("legacy")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 7, at.UserId)
}
