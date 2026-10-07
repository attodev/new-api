package oidc

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func withIssuer(t *testing.T, addr string) {
	t.Helper()
	original := system_setting.ServerAddress
	system_setting.ServerAddress = addr
	t.Cleanup(func() { system_setting.ServerAddress = original })
}

func TestIssuer_TrimsTrailingSlash(t *testing.T) {
	withIssuer(t, "https://newapi.example.com/")
	require.Equal(t, "https://newapi.example.com", Issuer())
}

func TestSignIDToken_ClaimsAndKid(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", DisplayName: "Alice", Email: "a@example.com", Role: common.RoleCommonUser, Group: "default"}, "")
	now := time.Unix(1700000000, 0)

	raw, err := SignIDToken(claims, "openwebui", "nonce-1", 1699999000, now)
	require.NoError(t, err)

	key, err := CurrentKey()
	require.NoError(t, err)
	parsed, err := jwt.ParseWithClaims(raw, &IDTokenClaims{}, func(tok *jwt.Token) (any, error) {
		require.Equal(t, "RS256", tok.Method.Alg())
		require.Equal(t, key.Kid, tok.Header["kid"])
		return &key.Private.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithoutClaimsValidation())
	require.NoError(t, err)
	got := parsed.Claims.(*IDTokenClaims)
	require.Equal(t, "https://newapi.example.com", got.Issuer)
	require.Equal(t, "7", got.Subject)
	require.Equal(t, jwt.ClaimStrings{"openwebui"}, got.Audience)
	require.Equal(t, now.Unix(), got.IssuedAt.Unix())
	require.Equal(t, now.Add(AccessTokenTTL).Unix(), got.ExpiresAt.Unix())
	require.Equal(t, int64(1699999000), got.AuthTime)
	require.Equal(t, "nonce-1", got.Nonce)
	require.Equal(t, "a@example.com", got.Email)
	require.True(t, got.EmailVerified)
	require.Equal(t, "Alice", got.Name)
}

func TestSignIDToken_OmitsEmptyNonce(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Group: "default"}, "")
	raw, err := SignIDToken(claims, "openwebui", "", time.Now().Unix(), time.Now())
	require.NoError(t, err)
	got, err := VerifyIDTokenHint(raw, "openwebui")
	require.NoError(t, err)
	require.Equal(t, "", got.Nonce)
}

func TestVerifyIDTokenHint_AcceptsExpiredAndPreviousKey_RejectsForeign(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Group: "default"}, "")

	expired, err := SignIDToken(claims, "openwebui", "n", time.Now().Add(-3*time.Hour).Unix(), time.Now().Add(-3*time.Hour))
	require.NoError(t, err)
	got, err := VerifyIDTokenHint(expired, "openwebui")
	require.NoError(t, err, "expiry is ignored for id_token_hint")
	require.Equal(t, "7", got.Subject)

	// Rotate: the old token must still verify through the previous key.
	_, err = RotateKey()
	require.NoError(t, err)
	_, err = VerifyIDTokenHint(expired, "openwebui")
	require.NoError(t, err)

	// Wrong audience / issuer are rejected.
	_, err = VerifyIDTokenHint(expired, "someone-else")
	require.Error(t, err)
	withIssuer(t, "https://other.example.com")
	_, err = VerifyIDTokenHint(expired, "openwebui")
	require.Error(t, err)

	// A token signed by a key we never had is rejected.
	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "https://newapi.example.com", "aud": "openwebui", "sub": "7"}).SignedString([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	_, err = VerifyIDTokenHint(foreign, "openwebui")
	require.Error(t, err)
	_, err = VerifyIDTokenHint("garbage", "openwebui")
	require.Error(t, err)
}
