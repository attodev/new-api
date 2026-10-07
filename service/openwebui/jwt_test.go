package openwebui

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const (
	testSecret    = "0123456789abcdef0123456789abcdef"
	testOldSecret = "fedcba9876543210fedcba9876543210"
	testWrongKey  = "wrongwrongwrongwrongwrongwrongwr"
	testIssuer    = "open-webui"
	testLeeway    = 60 * time.Second
	testSubject   = "7f3c2a1e"
	testEmail     = "kim@example.com"
)

func testConfig() JWTConfig {
	return JWTConfig{Secret: testSecret, PreviousSecret: testOldSecret, Issuer: testIssuer, Leeway: testLeeway}
}

func signHS256(t *testing.T, secret string, claims jwt.Claims) string {
	t.Helper()
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return raw
}

func baseClaims(now time.Time) UserClaims {
	return UserClaims{
		Email: testEmail,
		Name:  "Kim",
		Role:  "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testSubject,
			Issuer:    testIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(300 * time.Second)),
		},
	}
}

func TestVerifyUserJWT_Valid(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now()))
	claims, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
	require.Equal(t, testSubject, claims.Subject)
	require.Equal(t, testEmail, claims.Email)
	require.Equal(t, "user", claims.Role)
}

func TestVerifyUserJWT_AcceptsPreviousSecret(t *testing.T) {
	raw := signHS256(t, testOldSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_RejectsWrongSecret(t *testing.T) {
	raw := signHS256(t, testWrongKey, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_PreviousSecretIgnoredWhenEmpty(t *testing.T) {
	cfg := testConfig()
	cfg.PreviousSecret = ""
	raw := signHS256(t, testOldSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, cfg)
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsExpired(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(-10*time.Minute)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTExpired)
}

func TestVerifyUserJWT_ExpiredWithinLeewayPasses(t *testing.T) {
	// exp 30s in the past, leeway 60s → accepted
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(-330*time.Second)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_RejectsMissingExp(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.ExpiresAt = nil
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNoExp)
}

func TestVerifyUserJWT_RejectsWrongIssuer(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Issuer = "someone-else"
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadIssuer)
}

func TestVerifyUserJWT_IssuedAtAheadWithinLeewayPasses(t *testing.T) {
	// Open WebUI clock 30s ahead of ours.
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(30*time.Second)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_IssuedAtBeyondLeewayRejected(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(5*time.Minute)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNotYetValid)
}

func TestVerifyUserJWT_RejectsAlgNone(t *testing.T) {
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, baseClaims(time.Now())).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, baseClaims(time.Now())).SignedString(key)
	require.NoError(t, err)
	_, err = VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsMissingSub(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Subject = ""
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNoSub)
}

func TestVerifyUserJWT_RejectsMalformed(t *testing.T) {
	_, err := VerifyUserJWT("not.a.jwt", testConfig())
	require.ErrorIs(t, err, ErrJWTMalformed)
}

func TestVerifyUserJWT_EmptySecret(t *testing.T) {
	cfg := testConfig()
	cfg.Secret = ""
	raw := signHS256(t, testSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, cfg)
	require.ErrorIs(t, err, ErrJWTEmptySecret)
}

func TestVerifyUserJWT_AudiencePresentIsIgnored(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Audience = jwt.ClaimStrings{"anything"}
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_EmptyIssuerRejected(t *testing.T) {
	cfg := testConfig()
	cfg.Issuer = ""
	claims := baseClaims(time.Now())
	claims.Issuer = "anything"
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, cfg)
	require.ErrorIs(t, err, ErrJWTEmptyIssuer)
}
