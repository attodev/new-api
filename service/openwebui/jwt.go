// Package openwebui implements the Open WebUI relay integration: verifying
// the per-request user JWT Open WebUI attaches to every call made with the
// shared service key, mapping it to a new-api user, and substituting that
// user's own token so the existing billing pipeline charges the right person.
package openwebui

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// UserClaims is the payload Open WebUI's _mint_forward_user_jwt() produces.
// The subject is the Open WebUI user id; it is the only stable identity key.
type UserClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

type JWTConfig struct {
	Secret         string
	PreviousSecret string
	Issuer         string
	Leeway         time.Duration
}

var (
	ErrJWTEmptySecret  = errors.New("openwebui: jwt secret not configured")
	ErrJWTMalformed    = errors.New("openwebui: jwt malformed")
	ErrJWTBadSignature = errors.New("openwebui: jwt signature or algorithm invalid")
	ErrJWTExpired      = errors.New("openwebui: jwt expired")
	ErrJWTNotYetValid  = errors.New("openwebui: jwt issued in the future")
	ErrJWTNoExp        = errors.New("openwebui: jwt missing exp")
	ErrJWTBadIssuer    = errors.New("openwebui: jwt issuer mismatch")
	ErrJWTNoSub        = errors.New("openwebui: jwt missing sub")
)

// VerifyUserJWT validates raw against cfg.Secret and, on a signature
// failure only, against cfg.PreviousSecret (zero-downtime rotation).
// Only HS256 is accepted; exp is required; iss must match; leeway applies to
// exp and iat; aud and jti are not required (Open WebUI does not send them).
func VerifyUserJWT(raw string, cfg JWTConfig) (*UserClaims, error) {
	if cfg.Secret == "" {
		return nil, ErrJWTEmptySecret
	}
	claims, err := parseWithSecret(raw, cfg, cfg.Secret)
	if errors.Is(err, ErrJWTBadSignature) && cfg.PreviousSecret != "" {
		claims, err = parseWithSecret(raw, cfg, cfg.PreviousSecret)
	}
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, ErrJWTNoSub
	}
	return claims, nil
}

func parseWithSecret(raw string, cfg JWTConfig, secret string) (*UserClaims, error) {
	claims := &UserClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(cfg.Leeway),
	)
	_, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, mapJWTError(err)
	}
	return claims, nil
}

// mapJWTError collapses jwt/v5's joined errors into our sentinels. Order
// matters: a token can be both expired and badly signed, and the signature
// verdict must win so the previous-secret retry happens.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrJWTBadSignature
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrJWTMalformed
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return ErrJWTNoExp
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrJWTExpired
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued), errors.Is(err, jwt.ErrTokenNotValidYet):
		return ErrJWTNotYetValid
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return ErrJWTBadIssuer
	}
	return ErrJWTMalformed
}
