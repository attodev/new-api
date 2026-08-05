package setting

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

const (
	TossCardMinimumAmountKRW int64 = 100
	TossDefaultUnitPriceKRW        = 1300.0
	// The SDK v2 CARD method opens a card/easy-pay unified window. Easy-pay can
	// use a linked bank account, whose documented minimum is 200 KRW, so a
	// general checkout must satisfy the stricter floor. Card-only billing keeps
	// using TossCardMinimumAmountKRW.
	TossGeneralPaymentMinimumAmountKRW int64 = 200
	// TossMaximumChargeAmountKRW is an application safety bound. Toss card
	// limits vary by issuer, but accepting an unbounded client-supplied int64
	// can overflow quota and price conversions before Toss rejects the charge.
	TossMaximumChargeAmountKRW int64 = 1<<31 - 1
	TossOrderNameMaxRunes            = 100
)

// Toss Payments (토스페이먼츠) settings.
// Toss distinguishes test vs live purely by which key pair is used; there is
// no separate sandbox host. TossTestMode selects the active key pair.
var TossEnabled = false
var TossBillingEnabled = false // 자동결제는 별도 계약/MID가 필요하므로 일반 결제와 분리
// Wallet auto recharge is a non-subscription automatic payment. Toss requires
// a separate contract/risk review for this use case, so subscription billing
// approval alone must never enable it.
var TossWalletAutoRechargeEnabled = false
var TossTestMode = false
var TossClientKey = ""     // live client key (안전하게 프론트로 노출 가능)
var TossSecretKey = ""     // live secret key (서버 전용)
var TossTestClientKey = "" // test client key
var TossTestSecretKey = "" // test secret key
// Billing-only key pairs. Toss recurring billing can require a separately
// contracted MID, so availability checks require an explicit billing key pair.
// The active billing helpers below keep a normal-key fallback only for
// backward-compatible API calls and older stored credentials.
var TossBillingClientKey = ""
var TossBillingSecretKey = ""
var TossBillingTestClientKey = ""
var TossBillingTestSecretKey = ""
var TossUnitPrice = TossDefaultUnitPriceKRW      // 내부 1 unit(USD 환산) 당 KRW (청구 KRW = units × TossUnitPrice)
var TossMinTopUp = int(TossCardMinimumAmountKRW) // 최소 카드 청구 금액(KRW)
var tossConfigRevision = ""

var tossConfigMutex sync.RWMutex

type TossConfigSnapshot struct {
	Revision                  string
	Enabled                   bool
	BillingEnabled            bool
	WalletAutoRechargeEnabled bool
	TestMode                  bool
	ClientKey                 string
	SecretKey                 string
	TestClientKey             string
	TestSecretKey             string
	BillingClientKey          string
	BillingSecretKey          string
	BillingTestClientKey      string
	BillingTestSecretKey      string
	UnitPrice                 float64
	MinTopUp                  int
}

func tossConfigSnapshotLocked() TossConfigSnapshot {
	return TossConfigSnapshot{
		Revision:                  tossConfigRevision,
		Enabled:                   TossEnabled,
		BillingEnabled:            TossBillingEnabled,
		WalletAutoRechargeEnabled: TossWalletAutoRechargeEnabled,
		TestMode:                  TossTestMode,
		ClientKey:                 TossClientKey,
		SecretKey:                 TossSecretKey,
		TestClientKey:             TossTestClientKey,
		TestSecretKey:             TossTestSecretKey,
		BillingClientKey:          TossBillingClientKey,
		BillingSecretKey:          TossBillingSecretKey,
		BillingTestClientKey:      TossBillingTestClientKey,
		BillingTestSecretKey:      TossBillingTestSecretKey,
		UnitPrice:                 TossUnitPrice,
		MinTopUp:                  TossMinTopUp,
	}
}

func GetTossConfigSnapshot() TossConfigSnapshot {
	tossConfigMutex.RLock()
	defer tossConfigMutex.RUnlock()
	return tossConfigSnapshotLocked()
}

// TossConfigSnapshotDigest returns a stable, one-way fingerprint of every
// payment-affecting Toss option. Revision is deliberately excluded: the
// database revision token binds one generation to this digest. Length-prefixing
// each value prevents delimiter ambiguity without serializing secret values.
func TossConfigSnapshotDigest(snapshot TossConfigSnapshot) string {
	values := []string{
		strconv.FormatBool(snapshot.Enabled),
		strconv.FormatBool(snapshot.BillingEnabled),
		strconv.FormatBool(snapshot.WalletAutoRechargeEnabled),
		strconv.FormatBool(snapshot.TestMode),
		snapshot.ClientKey,
		snapshot.SecretKey,
		snapshot.TestClientKey,
		snapshot.TestSecretKey,
		snapshot.BillingClientKey,
		snapshot.BillingSecretKey,
		snapshot.BillingTestClientKey,
		snapshot.BillingTestSecretKey,
		strconv.FormatFloat(snapshot.UnitPrice, 'g', -1, 64),
		strconv.Itoa(snapshot.MinTopUp),
	}
	hash := sha256.New()
	var size [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func IsTossOptionKey(key string) bool {
	switch key {
	case "TossEnabled", "TossBillingEnabled", "TossWalletAutoRechargeEnabled", "TossTestMode",
		"TossClientKey", "TossSecretKey", "TossTestClientKey", "TossTestSecretKey",
		"TossBillingClientKey", "TossBillingSecretKey", "TossBillingTestClientKey", "TossBillingTestSecretKey",
		"TossUnitPrice", "TossMinTopUp":
		return true
	default:
		return false
	}
}

// NormalizeLegacyTossOptionValues keeps installations that persisted the old
// 0/1 KRW minimum from losing their entire Toss configuration at startup. The
// provider card floor is authoritative, so these legacy values become 100 KRW.
func NormalizeLegacyTossOptionValues(values map[string]string) bool {
	raw, ok := values["TossMinTopUp"]
	if !ok {
		return false
	}
	minimum, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || minimum < 0 || minimum >= TossCardMinimumAmountKRW {
		return false
	}
	values["TossMinTopUp"] = strconv.FormatInt(TossCardMinimumAmountKRW, 10)
	return true
}

func parseTossOptionValues(snapshot TossConfigSnapshot, values map[string]string) (TossConfigSnapshot, error) {
	for key, value := range values {
		if !IsTossOptionKey(key) {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "TossEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.Enabled = parsed
		case "TossBillingEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.BillingEnabled = parsed
		case "TossWalletAutoRechargeEnabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.WalletAutoRechargeEnabled = parsed
		case "TossTestMode":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.TestMode = parsed
		case "TossClientKey":
			snapshot.ClientKey = value
		case "TossSecretKey":
			snapshot.SecretKey = value
		case "TossTestClientKey":
			snapshot.TestClientKey = value
		case "TossTestSecretKey":
			snapshot.TestSecretKey = value
		case "TossBillingClientKey":
			snapshot.BillingClientKey = value
		case "TossBillingSecretKey":
			snapshot.BillingSecretKey = value
		case "TossBillingTestClientKey":
			snapshot.BillingTestClientKey = value
		case "TossBillingTestSecretKey":
			snapshot.BillingTestSecretKey = value
		case "TossUnitPrice":
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 || parsed > float64(TossMaximumChargeAmountKRW) {
				if err == nil {
					err = fmt.Errorf("must be positive and at most %d KRW", TossMaximumChargeAmountKRW)
				}
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.UnitPrice = parsed
		case "TossMinTopUp":
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < int(TossCardMinimumAmountKRW) || int64(parsed) > TossMaximumChargeAmountKRW {
				if err == nil {
					err = fmt.Errorf("must be between %d and %d KRW", TossCardMinimumAmountKRW, TossMaximumChargeAmountKRW)
				}
				return snapshot, fmt.Errorf("parse %s: %w", key, err)
			}
			snapshot.MinTopUp = parsed
		}
	}
	return snapshot, nil
}

func ValidateTossOptionValues(values map[string]string) error {
	tossConfigMutex.RLock()
	snapshot := tossConfigSnapshotLocked()
	tossConfigMutex.RUnlock()
	_, err := parseTossOptionValues(snapshot, values)
	return err
}

// ResolveTossConfigSnapshot applies persisted option values to the current
// defaults without mutating process state. Callers that serialize configuration
// writes in the database use this to derive the exact pre-update MID snapshot.
func ResolveTossConfigSnapshot(values map[string]string) (TossConfigSnapshot, error) {
	tossConfigMutex.RLock()
	snapshot := tossConfigSnapshotLocked()
	tossConfigMutex.RUnlock()
	return parseTossOptionValues(snapshot, values)
}

// ApplyTossOptionValues swaps all supplied Toss settings while holding one
// lock. Readers using GetTossConfigSnapshot never observe a mixed client/secret
// pair while an admin rotates keys or changes the active environment.
func applyTossOptionValues(values map[string]string, revision *string) error {
	tossConfigMutex.Lock()
	defer tossConfigMutex.Unlock()
	snapshot, err := parseTossOptionValues(tossConfigSnapshotLocked(), values)
	if err != nil {
		return err
	}
	if revision != nil {
		snapshot.Revision = strings.TrimSpace(*revision)
	}
	tossConfigRevision = snapshot.Revision
	TossEnabled = snapshot.Enabled
	TossBillingEnabled = snapshot.BillingEnabled
	TossWalletAutoRechargeEnabled = snapshot.WalletAutoRechargeEnabled
	TossTestMode = snapshot.TestMode
	TossClientKey = snapshot.ClientKey
	TossSecretKey = snapshot.SecretKey
	TossTestClientKey = snapshot.TestClientKey
	TossTestSecretKey = snapshot.TestSecretKey
	TossBillingClientKey = snapshot.BillingClientKey
	TossBillingSecretKey = snapshot.BillingSecretKey
	TossBillingTestClientKey = snapshot.BillingTestClientKey
	TossBillingTestSecretKey = snapshot.BillingTestSecretKey
	TossUnitPrice = snapshot.UnitPrice
	TossMinTopUp = snapshot.MinTopUp
	return nil
}

func ApplyTossOptionValues(values map[string]string) error {
	return applyTossOptionValues(values, nil)
}

// ApplyTossOptionValuesWithRevision publishes the database configuration
// generation atomically with its key pairs. Payment paths compare this value
// with the authoritative lock row immediately before any provider POST.
func ApplyTossOptionValuesWithRevision(values map[string]string, revision string) error {
	return applyTossOptionValues(values, &revision)
}

func TossEffectiveMinTopUp() int {
	minimum := GetTossConfigSnapshot().MinTopUp
	if minimum > int(TossCardMinimumAmountKRW) {
		return minimum
	}
	return int(TossCardMinimumAmountKRW)
}

func TossEffectiveGeneralTopUp() int {
	minimum := GetTossConfigSnapshot().MinTopUp
	if minimum > int(TossGeneralPaymentMinimumAmountKRW) {
		return minimum
	}
	return int(TossGeneralPaymentMinimumAmountKRW)
}

func TossActiveKeyPair() (string, string) {
	return TossActiveKeyPairFromSnapshot(GetTossConfigSnapshot())
}

func TossActiveKeyPairFromSnapshot(snapshot TossConfigSnapshot) (string, string) {
	if snapshot.TestMode {
		return snapshot.TestClientKey, snapshot.TestSecretKey
	}
	return snapshot.ClientKey, snapshot.SecretKey
}

func TossActiveClientKey() string {
	clientKey, _ := TossActiveKeyPair()
	return clientKey
}

func TossActiveSecretKey() string {
	_, secretKey := TossActiveKeyPair()
	return secretKey
}

func tossConfiguredPair(clientKey, secretKey, fallbackClientKey, fallbackSecretKey string) (string, string) {
	if strings.TrimSpace(clientKey) != "" && strings.TrimSpace(secretKey) != "" {
		return clientKey, secretKey
	}
	return fallbackClientKey, fallbackSecretKey
}

func TossActiveBillingKeyPair() (string, string) {
	return TossActiveBillingKeyPairFromSnapshot(GetTossConfigSnapshot())
}

func TossActiveBillingKeyPairFromSnapshot(snapshot TossConfigSnapshot) (string, string) {
	if snapshot.TestMode {
		return tossConfiguredPair(snapshot.BillingTestClientKey, snapshot.BillingTestSecretKey, snapshot.TestClientKey, snapshot.TestSecretKey)
	}
	return tossConfiguredPair(snapshot.BillingClientKey, snapshot.BillingSecretKey, snapshot.ClientKey, snapshot.SecretKey)
}

// TossExplicitActiveBillingKeyPair is for enablement checks. It intentionally
// does not fall back to normal Toss keys.
func TossExplicitActiveBillingKeyPair() (string, string) {
	snapshot := GetTossConfigSnapshot()
	if snapshot.TestMode {
		return strings.TrimSpace(snapshot.BillingTestClientKey), strings.TrimSpace(snapshot.BillingTestSecretKey)
	}
	return strings.TrimSpace(snapshot.BillingClientKey), strings.TrimSpace(snapshot.BillingSecretKey)
}

func TossActiveBillingClientKey() string {
	clientKey, _ := TossActiveBillingKeyPair()
	return clientKey
}

func TossActiveBillingSecretKey() string {
	_, secretKey := TossActiveBillingKeyPair()
	return secretKey
}
