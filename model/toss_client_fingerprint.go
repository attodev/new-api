package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// TossClientKeyFingerprint identifies the Toss MID namespace associated with
// an order without persisting the public client key itself. Toss keeps the
// client key stable when only a secret is reissued, so matching fingerprints
// allow a narrowly-scoped same-MID secret-rotation fallback.
func TossClientKeyFingerprint(clientKey string) string {
	clientKey = strings.TrimSpace(clientKey)
	if clientKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(clientKey))
	return hex.EncodeToString(sum[:])
}

// IsValidTossClientKeyFingerprint accepts only the canonical representation
// produced by TossClientKeyFingerprint. A non-empty but malformed value is not
// MID evidence: restore/migration damage must fall back to the exact encrypted
// operation credential instead of grouping unrelated orders together.
func IsValidTossClientKeyFingerprint(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value != strings.ToLower(value) || len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
