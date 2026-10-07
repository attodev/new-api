package oidc

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/samber/hot"
)

const (
	CodeTTL        = 120 * time.Second
	AccessTokenTTL = 3600 * time.Second

	codeNamespace   = "oauth2:code:v2"
	accessNamespace = "oauth2:access:v1"
	storeCapacity   = 10000
)

// ErrInvalidGrant covers every code-exchange failure (unknown, expired,
// reused, wrong client, wrong redirect_uri, PKCE mismatch). The token
// endpoint maps it to RFC 6749 "invalid_grant" without saying which.
var ErrInvalidGrant = errors.New("oidc: invalid_grant")

// AuthCode is what an authorization code stands for until it is exchanged.
type AuthCode struct {
	UserId              int    `json:"user_id"`
	ClientId            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Nonce               string `json:"nonce,omitempty"`
	Scope               string `json:"scope"`
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
	AuthTime            int64  `json:"auth_time"`
}

// AccessToken is the opaque userinfo-only bearer token's backing record.
type AccessToken struct {
	UserId   int    `json:"user_id"`
	ClientId string `json:"client_id"`
	Scope    string `json:"scope"`
}

var (
	storeMu     sync.Mutex
	codeStore   *cachex.HybridCache[string]
	accessStore *cachex.HybridCache[string]
)

func redisOn() bool { return common.RedisEnabled && common.RDB != nil }

func newStore(ns string, ttl time.Duration) *cachex.HybridCache[string] {
	return cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
		Namespace:    cachex.Namespace(ns),
		Redis:        common.RDB,
		RedisEnabled: redisOn,
		RedisCodec:   cachex.StringCodec{},
		Memory: func() *hot.HotCache[string, string] {
			return hot.NewHotCache[string, string](hot.LRU, storeCapacity).WithTTL(ttl).WithJanitor().Build()
		},
	})
}

func codes() *cachex.HybridCache[string] {
	storeMu.Lock()
	defer storeMu.Unlock()
	if codeStore == nil {
		codeStore = newStore(codeNamespace, CodeTTL)
	}
	return codeStore
}

func accessTokens() *cachex.HybridCache[string] {
	storeMu.Lock()
	defer storeMu.Unlock()
	if accessStore == nil {
		accessStore = newStore(accessNamespace, AccessTokenTTL)
	}
	return accessStore
}

// ResetStoresForTest discards both stores.
func ResetStoresForTest() {
	storeMu.Lock()
	defer storeMu.Unlock()
	codeStore = nil
	accessStore = nil
}

// VerifyPKCE checks base64url(SHA256(verifier)) == challenge for method S256.
// An empty stored challenge means the client did not use PKCE → always true.
func VerifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" {
		return true
	}
	if method != "S256" || verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// IssueCode stores ac under a fresh single-use code.
func IssueCode(ac AuthCode) (string, error) {
	code, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(ac)
	if err != nil {
		return "", err
	}
	if err := codes().SetWithTTL(code, string(raw), CodeTTL); err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeCode validates the binding and deletes the code only on success, so
// an attacker guessing the client_secret or verifier cannot burn a victim's
// code; the code still expires after CodeTTL.
func ConsumeCode(code, clientId, redirectURI, codeVerifier string) (*AuthCode, error) {
	if code == "" {
		return nil, ErrInvalidGrant
	}
	raw, found, err := codes().Get(code)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrInvalidGrant
	}
	var ac AuthCode
	if err := common.UnmarshalJsonStr(raw, &ac); err != nil {
		return nil, ErrInvalidGrant
	}
	if subtle.ConstantTimeCompare([]byte(ac.ClientId), []byte(clientId)) != 1 ||
		ac.RedirectURI != redirectURI ||
		!VerifyPKCE(ac.CodeChallenge, ac.CodeChallengeMethod, codeVerifier) {
		return nil, ErrInvalidGrant
	}
	if _, err := codes().DeleteMany([]string{code}); err != nil {
		return nil, err
	}
	return &ac, nil
}

func IssueAccessToken(at AccessToken) (string, error) {
	token, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(at)
	if err != nil {
		return "", err
	}
	if err := accessTokens().SetWithTTL(token, string(raw), AccessTokenTTL); err != nil {
		return "", err
	}
	return token, nil
}

func LookupAccessToken(raw string) (*AccessToken, bool, error) {
	if raw == "" {
		return nil, false, nil
	}
	stored, found, err := accessTokens().Get(raw)
	if err != nil || !found {
		return nil, false, err
	}
	var at AccessToken
	if err := common.UnmarshalJsonStr(stored, &at); err != nil {
		return nil, false, nil
	}
	return &at, true, nil
}

func RevokeAccessToken(raw string) {
	if raw == "" {
		return
	}
	_, _ = accessTokens().DeleteMany([]string{raw})
}
