package model

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tossOptionSecretEnvelopePrefix     = "enc:v1:"
	tossConfigRevisionDigestPrefix     = "toss:v2:"
	tossConfigRevisionQuarantinePrefix = "toss:v2q:"
	// tossConfigMaintenanceGateKey is an internal, database-authoritative fence.
	// An explicit legacy configuration repair creates it in the same transaction
	// as the new all-disabled attestation. Recurring payments cannot be enabled
	// until bounded MID/renewal migrations finish and clear the fence.
	tossConfigMaintenanceGateKey = "__internal_toss_config_maintenance_required"
	// TossOptionSecretEncryptionEnv is an explicit rolling-upgrade gate. Enable
	// it only after every application node understands the enc:v1 envelope.
	// Until then, legacy plaintext remains readable but cannot be migrated
	// automatically or replaced with another non-empty secret.
	TossOptionSecretEncryptionEnv = "TOSS_OPTION_SECRET_ENCRYPTION_ENABLED"
)

var ErrTossConfigMaintenanceRequired = errors.New("Toss configuration maintenance is required before payments can be enabled")
var ErrTossConfigRepairNotRequired = errors.New("Toss configuration repair is no longer required")

func tossConfigRevisionWithDigest(generation, digest string) string {
	return tossConfigRevisionDigestPrefix + strings.TrimSpace(generation) + ":" + strings.ToLower(strings.TrimSpace(digest))
}

func tossConfigQuarantineRevisionWithDigest(generation, digest string) string {
	return tossConfigRevisionQuarantinePrefix + strings.TrimSpace(generation) + ":" + strings.ToLower(strings.TrimSpace(digest))
}

func tossConfigRevisionDigestWithPrefix(revision, prefix string) (string, bool) {
	revision = strings.TrimSpace(revision)
	if !strings.HasPrefix(revision, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(revision, prefix)
	generation, digest, ok := strings.Cut(remainder, ":")
	if !ok || strings.TrimSpace(generation) == "" || len(digest) != sha256.Size*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", false
	}
	return strings.ToLower(digest), true
}

func tossConfigRevisionDigest(revision string) (string, bool) {
	return tossConfigRevisionDigestWithPrefix(revision, tossConfigRevisionDigestPrefix)
}

func tossConfigQuarantineRevisionDigest(revision string) (string, bool) {
	return tossConfigRevisionDigestWithPrefix(revision, tossConfigRevisionQuarantinePrefix)
}

// TossConfigState is safe to expose to an authenticated administrator. It
// contains no revision digest, client key, or secret material.
type TossConfigState struct {
	RepairRequired      bool
	MaintenanceRequired bool
	RepairToken         string
}

func tossConfigRepairToken(revision string) string {
	return common.GenerateHMAC("toss-config-repair-v1:" + strings.TrimSpace(revision))
}

func isInternalTossOptionKey(key string) bool {
	return key == tossOptionWriteLockKey || key == tossConfigMaintenanceGateKey
}

func GetTossConfigState() (TossConfigState, error) {
	state := TossConfigState{}
	if DB == nil || !DB.Migrator().HasTable(&Option{}) {
		return state, nil
	}

	_, _, attested, err := readTossConfigRevisionAttestation()
	if err != nil || !attested {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrTossConfigRevisionStale) ||
			errors.Is(err, ErrTossOptionSecretDecrypt) || errors.Is(err, ErrTossConfigStoredValuesInvalid) {
			state.RepairRequired = true
		} else if err != nil {
			return state, err
		}
	}
	if state.RepairRequired {
		// Materialize/read the opaque serialization row so the browser can bind
		// an explicit repair to the exact broken generation it inspected. The
		// token is an HMAC and does not expose the revision's secret-derived digest.
		err := DB.Transaction(func(tx *gorm.DB) error {
			revision, lockErr := lockTossOptionRowsTx(tx)
			if lockErr != nil {
				return lockErr
			}
			state.RepairToken = tossConfigRepairToken(revision)
			return nil
		})
		if err != nil {
			return TossConfigState{}, err
		}
	}

	var maintenance Option
	if err := DB.Select("value").Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).First(&maintenance).Error; err == nil {
		state.MaintenanceRequired = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return TossConfigState{}, err
	}
	pendingRenewal, err := HasPendingLegacyTossRenewalContracts()
	if err != nil {
		return TossConfigState{}, err
	}
	state.MaintenanceRequired = state.MaintenanceRequired || pendingRenewal
	return state, nil
}

func setTossConfigMaintenanceRequiredTx(tx *gorm.DB, revision string) error {
	if tx == nil {
		return errors.New("database transaction is unavailable")
	}
	revision = strings.TrimSpace(revision)
	if _, ok := tossConfigRevisionDigest(revision); !ok {
		return ErrTossConfigRevisionStale
	}
	row := Option{Key: tossConfigMaintenanceGateKey, Value: revision}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&row).Error
}

func tossConfigMaintenanceRequiredTx(tx *gorm.DB) (bool, error) {
	if tx == nil {
		return true, errors.New("database transaction is unavailable")
	}
	var count int64
	if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).Count(&count).Error; err != nil {
		return true, err
	}
	return count > 0, nil
}

func isUnboundTossConfigRevision(revision string) bool {
	revision = strings.TrimSpace(revision)
	if len(revision) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(revision)
	return err == nil && len(decoded) == 16
}

var (
	ErrTossOptionSecretEncryptionDisabled = errors.New("Toss option secret encryption is not enabled")
	ErrTossOptionSecretDecrypt            = errors.New("failed to decrypt Toss option secret")
)

func isTossSecretOption(key string) bool {
	switch key {
	case "TossSecretKey", "TossTestSecretKey", "TossBillingSecretKey", "TossBillingTestSecretKey":
		return true
	default:
		return false
	}
}

func IsTossOptionSecretEncryptionEnabled() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(TossOptionSecretEncryptionEnv)))
	return err == nil && enabled
}

// defaultTossOptionValues is the safe baseline for an authoritative database
// snapshot. A missing option row must never inherit a value from an older
// in-process snapshot: rows can disappear during a restore, manual repair, or
// mixed-version rollout, and reviving a stale enable flag or credential would
// authorize provider requests in the wrong MID namespace.
func defaultTossOptionValues() map[string]string {
	return map[string]string{
		"TossEnabled":                   "false",
		"TossBillingEnabled":            "false",
		"TossWalletAutoRechargeEnabled": "false",
		"TossTestMode":                  "false",
		"TossClientKey":                 "",
		"TossSecretKey":                 "",
		"TossTestClientKey":             "",
		"TossTestSecretKey":             "",
		"TossBillingClientKey":          "",
		"TossBillingSecretKey":          "",
		"TossBillingTestClientKey":      "",
		"TossBillingTestSecretKey":      "",
		"TossUnitPrice":                 strconv.FormatFloat(setting.TossDefaultUnitPriceKRW, 'f', -1, 64),
		"TossMinTopUp":                  strconv.FormatInt(setting.TossCardMinimumAmountKRW, 10),
	}
}

func tossOptionSecretCipher() (cipher.AEAD, error) {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(common.CryptoSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptTossOptionSecret produces a versioned AES-GCM envelope. The option
// key is authenticated as associated data so ciphertext cannot be moved from
// one Toss secret slot to another.
func encryptTossOptionSecret(key, plaintext string) (string, error) {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return "", nil
	}
	if !isTossSecretOption(key) {
		return "", fmt.Errorf("%w: unsupported secret option %s", ErrTossOptionValidation, key)
	}
	gcm, err := tossOptionSecretCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(key))
	return tossOptionSecretEnvelopePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// decryptTossOptionSecret is a dual reader. Values without an envelope are
// legacy plaintext and are returned only so an already-running installation
// can be upgraded in two phases. enc:v1 values never fall back to plaintext on
// an invalid key or malformed ciphertext.
func decryptTossOptionSecret(key, stored string) (plaintext string, legacy bool, err error) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return "", false, nil
	}
	if !strings.HasPrefix(stored, tossOptionSecretEnvelopePrefix) {
		if strings.HasPrefix(strings.ToLower(stored), "enc:") {
			return "", false, fmt.Errorf("%w: unsupported envelope version", ErrTossOptionSecretDecrypt)
		}
		legacy := strings.ToLower(stored)
		if !strings.HasPrefix(legacy, "live_sk_") && !strings.HasPrefix(legacy, "test_sk_") {
			return "", false, fmt.Errorf("%w: invalid legacy secret format", ErrTossOptionSecretDecrypt)
		}
		return stored, true, nil
	}
	if !isTossSecretOption(key) {
		return "", false, fmt.Errorf("%w: unsupported secret option %s", ErrTossOptionSecretDecrypt, key)
	}
	gcm, err := tossOptionSecretCipher()
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrTossOptionSecretDecrypt, err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, tossOptionSecretEnvelopePrefix))
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", false, fmt.Errorf("%w: invalid envelope", ErrTossOptionSecretDecrypt)
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(key))
	if err != nil {
		return "", false, fmt.Errorf("%w: authentication failed", ErrTossOptionSecretDecrypt)
	}
	return string(plain), false, nil
}

func decodeTossOptionValues(stored map[string]string) (map[string]string, map[string]string, error) {
	plain := make(map[string]string, len(stored))
	legacy := make(map[string]string)
	for key, value := range stored {
		if !isTossSecretOption(key) {
			plain[key] = value
			continue
		}
		decoded, isLegacy, err := decryptTossOptionSecret(key, value)
		if err != nil {
			return nil, nil, err
		}
		plain[key] = decoded
		if isLegacy && decoded != "" {
			// Keep the exact stored representation for the conditional migration
			// update; encryptTossOptionSecret normalizes it before sealing.
			legacy[key] = value
		}
	}
	return plain, legacy, nil
}

func ensureTossOptionWriteLockRowTx(tx *gorm.DB) error {
	lockRow := Option{Key: tossOptionWriteLockKey, Value: common.GetUUID()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lockRow).Error; err != nil {
		return err
	}
	return nil
}

func lockTossOptionRowsTx(tx *gorm.DB) (string, error) {
	if err := ensureTossOptionWriteLockRowTx(tx); err != nil {
		return "", err
	}
	var lockRow Option
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(commonKeyCol+" = ?", tossOptionWriteLockKey).
		First(&lockRow).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(lockRow.Value), nil
}

func lockTossOptionWritesTx(tx *gorm.DB) (previousRevision string, generation string, err error) {
	previousRevision, err = lockTossOptionRowsTx(tx)
	if err != nil {
		return "", "", err
	}
	generation = common.GetUUID()
	result := tx.Model(&Option{}).
		Where(commonKeyCol+" = ? AND value = ?", tossOptionWriteLockKey, previousRevision).
		UpdateColumn("value", generation)
	if result.Error != nil {
		return "", "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", "", errors.New("failed to advance Toss configuration revision")
	}
	return previousRevision, generation, nil
}

func finalizeTossOptionWritesTx(tx *gorm.DB, generation, digest string, quarantined bool) (string, error) {
	generation = strings.TrimSpace(generation)
	if tx == nil || !isUnboundTossConfigRevision(generation) || len(strings.TrimSpace(digest)) != sha256.Size*2 {
		return "", errors.New("invalid Toss configuration revision attestation")
	}
	revision := tossConfigRevisionWithDigest(generation, digest)
	if quarantined {
		revision = tossConfigQuarantineRevisionWithDigest(generation, digest)
	}
	result := tx.Model(&Option{}).
		Where(commonKeyCol+" = ? AND value = ?", tossOptionWriteLockKey, generation).
		UpdateColumn("value", revision)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", ErrTossConfigRevisionStale
	}
	return revision, nil
}

// migrateLegacyTossOptionSecrets is intentionally gated for rolling upgrades:
// first deploy code that can dual-read enc:v1 to every node, then enable the
// environment flag everywhere. Conditional updates prevent a stale sync from
// overwriting a concurrent credential rotation.
func migrateLegacyTossOptionSecrets(legacy map[string]string) error {
	if len(legacy) == 0 || !IsTossOptionSecretEncryptionEnabled() {
		return nil
	}
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossOptionRowsTx(tx); err != nil {
			return err
		}
		for key, plaintext := range legacy {
			encrypted, err := encryptTossOptionSecret(key, plaintext)
			if err != nil {
				return err
			}
			result := tx.Model(&Option{}).
				Where(commonKeyCol+" = ?", key).
				Where(tossOptionExactValuePredicate(), plaintext).
				UpdateColumn("value", encrypted)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: Toss option changed while encrypting %s", ErrTossOptionValidation, key)
			}
		}
		return nil
	})
}

// Option values use the database's default text collation. On MySQL that is
// commonly case-insensitive, while Toss secrets and base64 ciphertext are
// case-sensitive. Conditional secret migrations must therefore force a byte
// comparison or a stale migration could overwrite a concurrently rotated key
// whose only observed difference happens to be letter case.
func tossOptionExactValuePredicate() string {
	switch {
	case common.UsingMySQL:
		return "BINARY `value` = BINARY ?"
	case common.UsingSQLite:
		return "CAST(`value` AS BLOB) = CAST(? AS BLOB)"
	case common.UsingPostgreSQL:
		// convert_to has returned bytea since well before PostgreSQL 9.6. Bytea
		// equality is independent of a column's deterministic/nondeterministic
		// text collation and therefore remains case- and normalization-sensitive.
		return `convert_to("value", 'UTF8') = convert_to(?, 'UTF8')`
	default:
		return "value = ?"
	}
}

func requireTossOptionSecretWriteSafety(values map[string]string) error {
	for key, value := range values {
		if !isTossSecretOption(key) || strings.TrimSpace(value) == "" {
			continue
		}
		if !IsTossOptionSecretEncryptionEnabled() {
			return fmt.Errorf("%w: set %s=true after all nodes support encrypted Toss options", ErrTossOptionSecretEncryptionDisabled, TossOptionSecretEncryptionEnv)
		}
		return ValidateTossBillingCryptoConfiguration()
	}
	return nil
}

func applyTossOptionMapValues(values map[string]string) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string, len(values))
	}
	for _, key := range []string{
		"TossSecretKey",
		"TossTestSecretKey",
		"TossBillingSecretKey",
		"TossBillingTestSecretKey",
	} {
		delete(common.OptionMap, key)
	}
	for key, value := range values {
		if isTossSecretOption(key) {
			continue
		}
		common.OptionMap[key] = value
	}
}

// failClosedTossOptionSecrets resets the entire payment-affecting snapshot to
// the disabled defaults. Clearing only secrets is insufficient: a stale test
// mode, client key, price, or minimum could later be paired with repaired
// credentials and silently resurrect a mixed configuration.
func failClosedTossOptionSecrets() {
	values := defaultTossOptionValues()
	_ = setting.ApplyTossOptionValues(values)
	applyTossOptionMapValues(values)
}
