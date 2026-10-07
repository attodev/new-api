// Package oidc implements new-api's built-in OpenID Connect provider used by
// Open WebUI: signing keys, authorization codes, access tokens, id_tokens,
// claims and the discovery document. HTTP handling lives in
// controller/oauth2_provider.go.
package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	// Option names deliberately contain no "." (so they are not routed into
	// config.GlobalConfig) and END in "Key" (so the suffix filter in
	// controller.GetOptions hides them; it also excludes them by name).
	OptionSigningKeyCurrent  = "OIDCSigningCurrentKey"
	OptionSigningKeyPrevious = "OIDCSigningPreviousKey"

	// PreviousKeyRetention is how long a retired key stays in the JWKS so
	// id_tokens (1 h lifetime) and id_token_hints signed with it still verify.
	PreviousKeyRetention = 24 * time.Hour

	rsaBits = 2048
)

// SigningKey is a parsed RSA key with its stable identifier.
type SigningKey struct {
	Kid       string
	Private   *rsa.PrivateKey
	CreatedAt int64
}

// storedSigningKey is the JSON persisted in the options table.
type storedSigningKey struct {
	Kid        string `json:"kid"`
	PrivatePEM string `json:"private_pem"`
	CreatedAt  int64  `json:"created_at"`
	// RotatedAt is set when the key becomes the previous key; retention counts from here.
	RotatedAt int64 `json:"rotated_at,omitempty"`
}

var (
	keyMu       sync.Mutex
	cachedRaw   = map[string]string{} // option name -> raw JSON last parsed
	cachedKeys  = map[string]*SigningKey{}
	cachedStore = map[string]storedSigningKey{}
)

// ComputeKid derives a deterministic key id: base64url(SHA-256(PKIX DER)[:16]).
func ComputeKid(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func readOption(name string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[name]
}

// loadKeyLocked parses the option into a SigningKey, reusing the cached parse
// when the stored JSON is unchanged. Returns (nil, nil) when the option is absent.
func loadKeyLocked(name string) (*SigningKey, storedSigningKey, error) {
	raw := readOption(name)
	if raw == "" {
		delete(cachedRaw, name)
		delete(cachedKeys, name)
		delete(cachedStore, name)
		return nil, storedSigningKey{}, nil
	}
	if cachedRaw[name] == raw {
		return cachedKeys[name], cachedStore[name], nil
	}
	var s storedSigningKey
	if err := common.UnmarshalJsonStr(raw, &s); err != nil {
		return nil, storedSigningKey{}, err
	}
	block, _ := pem.Decode([]byte(s.PrivatePEM))
	if block == nil {
		return nil, storedSigningKey{}, errors.New("oidc: signing key option holds no PEM block")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, storedSigningKey{}, err
	}
	key := &SigningKey{Kid: s.Kid, Private: priv, CreatedAt: s.CreatedAt}
	cachedRaw[name] = raw
	cachedKeys[name] = key
	cachedStore[name] = s
	return key, s, nil
}

func newStoredKey() (storedSigningKey, *SigningKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return storedSigningKey{}, nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	now := common.GetTimestamp()
	s := storedSigningKey{Kid: ComputeKid(&priv.PublicKey), PrivatePEM: string(pemBytes), CreatedAt: now}
	return s, &SigningKey{Kid: s.Kid, Private: priv, CreatedAt: now}, nil
}

func saveStoredKey(name string, s storedSigningKey) error {
	raw, err := common.Marshal(s)
	if err != nil {
		return err
	}
	return model.UpdateOption(name, string(raw))
}

// currentKeyLocked loads the current key, generating and persisting one on
// first use. Caller must hold keyMu.
func currentKeyLocked() (*SigningKey, error) {
	key, _, err := loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	if key != nil {
		return key, nil
	}
	s, _, err := newStoredKey()
	if err != nil {
		return nil, err
	}
	if err := saveStoredKey(OptionSigningKeyCurrent, s); err != nil {
		return nil, err
	}
	// Re-read so the cache reflects exactly what was stored. This sees this
	// process's own OptionMap write; multi-node first-boot races are resolved
	// by the periodic option sync (last DB writer wins, and tokens already
	// issued by the losing node stop verifying).
	key, _, err = loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("oidc: signing key vanished after save")
	}
	return key, nil
}

// CurrentKey returns the active signing key, generating and persisting one on
// first use. Safe for concurrent callers within a process.
func CurrentKey() (*SigningKey, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	return currentKeyLocked()
}

// previousKeyLocked returns the previous key if it exists and is still within
// retention; an expired one is deleted from the options table.
func previousKeyLocked() (*SigningKey, error) {
	key, s, err := loadKeyLocked(OptionSigningKeyPrevious)
	if err != nil || key == nil {
		return nil, err
	}
	since := s.RotatedAt
	if since == 0 {
		since = s.CreatedAt
	}
	if common.GetTimestamp()-since > int64(PreviousKeyRetention.Seconds()) {
		if err := model.UpdateOption(OptionSigningKeyPrevious, ""); err != nil {
			return nil, err
		}
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, OptionSigningKeyPrevious)
		common.OptionMapRWMutex.Unlock()
		delete(cachedRaw, OptionSigningKeyPrevious)
		delete(cachedKeys, OptionSigningKeyPrevious)
		delete(cachedStore, OptionSigningKeyPrevious)
		return nil, nil
	}
	return key, nil
}

// VerificationKeys returns the keys a token may currently be signed with:
// the current key first, then the still-retained previous key. current and
// previous are read under one lock so a concurrent rotation cannot yield a
// set containing only the old key.
func VerificationKeys() ([]*SigningKey, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	cur, err := currentKeyLocked()
	if err != nil {
		return nil, err
	}
	prev, err := previousKeyLocked()
	if err != nil {
		return nil, err
	}
	keys := []*SigningKey{cur}
	if prev != nil && prev.Kid != cur.Kid {
		keys = append(keys, prev)
	}
	return keys, nil
}

// RotateKey demotes the current key to previous (stamping RotatedAt) and
// installs a freshly generated current key. Any older previous key is dropped.
// The whole operation holds keyMu; the new key is generated before anything is
// written so a failure leaves no half-state.
func RotateKey() (*SigningKey, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	if _, err := currentKeyLocked(); err != nil {
		return nil, err
	}
	_, curStored, err := loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	s, key, err := newStoredKey()
	if err != nil {
		return nil, err
	}
	curStored.RotatedAt = common.GetTimestamp()
	if err := saveStoredKey(OptionSigningKeyPrevious, curStored); err != nil {
		return nil, err
	}
	if err := saveStoredKey(OptionSigningKeyCurrent, s); err != nil {
		return nil, err
	}
	if _, _, err := loadKeyLocked(OptionSigningKeyCurrent); err != nil {
		return nil, err
	}
	if _, _, err := loadKeyLocked(OptionSigningKeyPrevious); err != nil {
		return nil, err
	}
	return key, nil
}

// JWKS renders the verification keys as a JSON Web Key Set.
func JWKS() (map[string]any, error) {
	keys, err := VerificationKeys()
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		pub := &k.Private.PublicKey
		list = append(list, map[string]any{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": k.Kid,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	return map[string]any{"keys": list}, nil
}

// ResetKeyCacheForTest drops the in-process parse cache.
func ResetKeyCacheForTest() {
	keyMu.Lock()
	defer keyMu.Unlock()
	cachedRaw = map[string]string{}
	cachedKeys = map[string]*SigningKey{}
	cachedStore = map[string]storedSigningKey{}
}
