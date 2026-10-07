package oidc

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the OIDC issuer: the configured server address without a trailing slash.
func Issuer() string {
	return strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
}

// IDTokenClaims is the id_token payload. roles/groups are deliberately left to
// userinfo to keep the token small; email+name are included so Open WebUI can
// skip the userinfo call when it wants to.
type IDTokenClaims struct {
	jwt.RegisteredClaims
	AuthTime      int64  `json:"auth_time"`
	Nonce         string `json:"nonce,omitempty"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

var ErrIDTokenInvalid = errors.New("oidc: id_token invalid")

// SignIDToken issues an RS256 id_token for clientId, valid for AccessTokenTTL.
func SignIDToken(c Claims, clientId, nonce string, authTime int64, now time.Time) (string, error) {
	key, err := CurrentKey()
	if err != nil {
		return "", err
	}
	claims := IDTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer(),
			Subject:   c.Sub,
			Audience:  jwt.ClaimStrings{clientId},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
		AuthTime:      authTime,
		Nonce:         nonce,
		Email:         c.Email,
		EmailVerified: c.EmailVerified,
		Name:          c.Name,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = key.Kid
	return token.SignedString(key.Private)
}

// VerifyIDTokenHint checks that raw was signed by one of our verification keys
// for clientId and our issuer. Expiry is deliberately NOT enforced: end-session
// receives id_tokens that may be hours old.
func VerifyIDTokenHint(raw, clientId string) (*IDTokenClaims, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrIDTokenInvalid
	}
	keys, err := VerificationKeys()
	if err != nil {
		return nil, err
	}
	claims := &IDTokenClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(Issuer()),
		jwt.WithAudience(clientId),
		jwt.WithoutClaimsValidation(),
	)
	_, err = parser.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		for _, k := range keys {
			if kid == "" || k.Kid == kid {
				return &k.Private.PublicKey, nil
			}
		}
		return nil, ErrIDTokenInvalid
	})
	if err != nil {
		return nil, ErrIDTokenInvalid
	}
	// WithoutClaimsValidation disables iss/aud checks too, so enforce them here.
	if claims.Issuer != Issuer() {
		return nil, ErrIDTokenInvalid
	}
	audOK := false
	for _, a := range claims.Audience {
		if a == clientId {
			audOK = true
		}
	}
	if !audOK {
		return nil, ErrIDTokenInvalid
	}
	return claims, nil
}
