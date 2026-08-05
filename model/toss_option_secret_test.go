package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func preserveTossOptionSecretTestState(t *testing.T) {
	t.Helper()
	originalSnapshot := setting.GetTossConfigSnapshot()
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(
			tossOptionValuesForTest(originalSnapshot), originalSnapshot.Revision,
		))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func attestExistingTossOptionRowsForTest(t *testing.T) string {
	t.Helper()
	var existing []Option
	require.NoError(t, DB.Where(commonKeyCol+" IN ?", tossOptionKeys()).Find(&existing).Error)
	present := make(map[string]bool, len(existing))
	for i := range existing {
		present[existing[i].Key] = true
	}
	missing := make([]Option, 0)
	for key, value := range defaultTossOptionValues() {
		if !present[key] {
			missing = append(missing, Option{Key: key, Value: value})
		}
	}
	if len(missing) > 0 {
		require.NoError(t, DB.Create(&missing).Error)
	}
	var rows []*Option
	require.NoError(t, DB.Where(commonKeyCol+" IN ?", tossOptionKeys()).Find(&rows).Error)
	values, _, digest, _, err := resolveTossOptionRows(rows)
	require.NoError(t, err)
	revision := tossConfigRevisionWithDigest(common.GetUUID(), digest)
	require.NoError(t, DB.Create(&Option{Key: tossOptionWriteLockKey, Value: revision}).Error)
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(values, revision))
	return revision
}

func TestLoadTossOptionSecretsDualReadsLegacyAndMigratesOnlyAfterGate(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Setenv(TossOptionSecretEncryptionEnv, "false")
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossTestMode":       "false",
		"TossClientKey":      "",
		"TossSecretKey":      "stale_in_memory_secret",
		"TossUnitPrice":      "1300",
		"TossMinTopUp":       "100",
	}))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossEnabled", Value: "true"},
		{Key: "TossTestMode", Value: "false"},
		{Key: "TossClientKey", Value: "live_ck_legacy_option"},
		{Key: "TossSecretKey", Value: "live_sk_legacy_option"},
	}).Error)
	attestExistingTossOptionRowsForTest(t)

	loadOptionsFromDatabase()
	require.True(t, setting.GetTossConfigSnapshot().Enabled)
	require.Equal(t, "live_sk_legacy_option", setting.GetTossConfigSnapshot().SecretKey)
	var stored Option
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.Equal(t, "live_sk_legacy_option", stored.Value)
	common.OptionMapRWMutex.RLock()
	_, secretCached := common.OptionMap["TossSecretKey"]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, secretCached)

	// The operator enables this only after every node can read enc:v1. The next
	// sync migrates the existing row without changing the plaintext snapshot.
	t.Setenv(TossOptionSecretEncryptionEnv, "true")
	loadOptionsFromDatabase()
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.True(t, strings.HasPrefix(stored.Value, tossOptionSecretEnvelopePrefix))
	require.NotContains(t, stored.Value, "live_sk_legacy_option")
	require.Equal(t, "live_sk_legacy_option", setting.GetTossConfigSnapshot().SecretKey)

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":   "false",
		"TossSecretKey": "stale_after_migration",
	}))
	loadOptionsFromDatabase()
	require.True(t, setting.GetTossConfigSnapshot().Enabled)
	require.Equal(t, "live_sk_legacy_option", setting.GetTossConfigSnapshot().SecretKey)
}

func TestUpdateTossOptionSecretsWritesEncryptedEnvelopeOnly(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Setenv(TossOptionSecretEncryptionEnv, "true")
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossClientKey":      "",
		"TossSecretKey":      "",
		"TossUnitPrice":      "1300",
		"TossMinTopUp":       "100",
	})
	fallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())

	require.NoError(t, UpdateTossOptionsBulk(map[string]string{
		"TossClientKey": "live_ck_encrypted_option",
		"TossSecretKey": "live_sk_encrypted_option",
	}, fallback, nil))

	var stored Option
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.True(t, strings.HasPrefix(stored.Value, tossOptionSecretEnvelopePrefix))
	require.NotContains(t, stored.Value, "live_sk_encrypted_option")
	plain, legacy, err := decryptTossOptionSecret("TossSecretKey", stored.Value)
	require.NoError(t, err)
	require.False(t, legacy)
	require.Equal(t, "live_sk_encrypted_option", plain)
	require.Equal(t, "live_sk_encrypted_option", setting.GetTossConfigSnapshot().SecretKey)
	common.OptionMapRWMutex.RLock()
	_, secretCached := common.OptionMap["TossSecretKey"]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, secretCached)
}

func TestRepairTossOptionsReplacesCorruptCiphertextAndRejectsStaleReplay(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	values := defaultTossOptionValues()
	values["TossClientKey"] = "live_ck_repair_corrupt"
	values["TossSecretKey"] = "enc:v1:not-valid-base64"
	rows := make([]Option, 0, len(values)+1)
	for key, value := range values {
		rows = append(rows, Option{Key: key, Value: value})
	}
	rows = append(rows, Option{Key: tossOptionWriteLockKey, Value: common.GetUUID()})
	require.NoError(t, DB.Create(&rows).Error)

	state, err := GetTossConfigState()
	require.NoError(t, err)
	require.True(t, state.RepairRequired)
	require.NotEmpty(t, state.RepairToken)
	repair := defaultTossOptionValues()
	repair["TossClientKey"] = "live_ck_repair_corrupt"
	repair["TossSecretKey"] = "live_sk_repair_replacement"
	require.NoError(t, RepairTossOptionsBulk(repair, state.RepairToken, nil))

	var stored Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "TossSecretKey").First(&stored).Error)
	plain, _, err := decryptTossOptionSecret("TossSecretKey", stored.Value)
	require.NoError(t, err)
	require.Equal(t, "live_sk_repair_replacement", plain)
	require.ErrorIs(t, RepairTossOptionsBulk(repair, state.RepairToken, nil), ErrTossConfigRevisionStale)
}

func TestNormalTossOptionUpdatePreservesUnsubmittedSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossClientKey": "live_ck_preserve_secret",
		"TossSecretKey": "live_sk_preserve_secret",
	})

	require.NoError(t, UpdateTossOptionsBulk(map[string]string{"TossMinTopUp": "200"}, nil, nil))
	var stored Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "TossSecretKey").First(&stored).Error)
	plain, _, err := decryptTossOptionSecret("TossSecretKey", stored.Value)
	require.NoError(t, err)
	require.Equal(t, "live_sk_preserve_secret", plain)
}

func TestGetTossConfigStateClassifiesMalformedStoredValuesAsRepairable(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	persistAttestedTossOptionsForTest(t, nil)
	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", "TossUnitPrice").UpdateColumn("value", "not-a-number").Error)

	state, err := GetTossConfigState()
	require.NoError(t, err)
	require.True(t, state.RepairRequired)
	require.NotEmpty(t, state.RepairToken)
}

func TestTossOptionSecretEnvelopeCoversEverySecretSlotAndBindsAAD(t *testing.T) {
	setupTossBillingModelTestDB(t)
	secretKeys := []string{
		"TossSecretKey",
		"TossTestSecretKey",
		"TossBillingSecretKey",
		"TossBillingTestSecretKey",
	}
	for _, key := range secretKeys {
		t.Run(key, func(t *testing.T) {
			plaintext := "live_sk_" + key
			encrypted, err := encryptTossOptionSecret(key, plaintext)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(encrypted, tossOptionSecretEnvelopePrefix))
			require.NotContains(t, encrypted, plaintext)
			decoded, legacy, err := decryptTossOptionSecret(key, encrypted)
			require.NoError(t, err)
			require.False(t, legacy)
			require.Equal(t, plaintext, decoded)

			otherKey := "TossSecretKey"
			if key == otherKey {
				otherKey = "TossBillingSecretKey"
			}
			_, _, err = decryptTossOptionSecret(otherKey, encrypted)
			require.ErrorIs(t, err, ErrTossOptionSecretDecrypt)
		})
	}
	_, _, err := decryptTossOptionSecret("TossSecretKey", "enc:v2:not-supported")
	require.ErrorIs(t, err, ErrTossOptionSecretDecrypt)
}

func TestLoadTossOptionSecretWrongCryptoKeyFailsClosed(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Setenv(TossOptionSecretEncryptionEnv, "true")
	encrypted, err := encryptTossOptionSecret("TossSecretKey", "live_sk_encrypted_with_old_key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossEnabled", Value: "true"},
		{Key: "TossBillingEnabled", Value: "true"},
		{Key: "TossSecretKey", Value: encrypted},
	}).Error)
	attestExistingTossOptionRowsForTest(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
		"TossSecretKey":                 "live_sk_stale_fallback",
		"TossBillingSecretKey":          "live_sk_stale_billing_fallback",
	}))
	common.OptionMapRWMutex.Lock()
	common.OptionMap["TossSecretKey"] = "must_be_removed"
	common.OptionMapRWMutex.Unlock()

	t.Setenv("CRYPTO_SECRET", "different-persistent-option-secret")
	common.CryptoSecret = "different-persistent-option-secret"
	loadOptionsFromDatabase()

	snapshot := setting.GetTossConfigSnapshot()
	require.False(t, snapshot.Enabled)
	require.False(t, snapshot.BillingEnabled)
	require.False(t, snapshot.WalletAutoRechargeEnabled)
	require.Empty(t, snapshot.SecretKey)
	require.Empty(t, snapshot.TestSecretKey)
	require.Empty(t, snapshot.BillingSecretKey)
	require.Empty(t, snapshot.BillingTestSecretKey)
	common.OptionMapRWMutex.RLock()
	_, secretCached := common.OptionMap["TossSecretKey"]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, secretCached)
}

func TestLoadTossOptionsMissingSecretRowClearsStaleSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":          "true",
		"TossBillingEnabled":   "true",
		"TossSecretKey":        "live_sk_stale_missing_row",
		"TossBillingSecretKey": "live_sk_stale_billing_missing_row",
	}))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossEnabled", Value: "true"},
		{Key: "TossBillingEnabled", Value: "true"},
		{Key: "TossClientKey", Value: "live_ck_missing_secret"},
		{Key: "TossBillingClientKey", Value: "live_ck_missing_billing_secret"},
	}).Error)
	attestExistingTossOptionRowsForTest(t)

	loadOptionsFromDatabase()
	snapshot := setting.GetTossConfigSnapshot()
	require.Empty(t, snapshot.SecretKey)
	require.Empty(t, snapshot.BillingSecretKey)
	require.False(t, snapshot.WalletAutoRechargeEnabled)
}

func TestLoadTossOptionsMissingRowsResetEntireStaleSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
		"TossTestMode":                  "true",
		"TossClientKey":                 "live_ck_stale_missing_rows",
		"TossSecretKey":                 "live_sk_stale_missing_rows",
		"TossTestClientKey":             "test_ck_stale_missing_rows",
		"TossTestSecretKey":             "test_sk_stale_missing_rows",
		"TossBillingClientKey":          "live_ck_billing_stale_missing_rows",
		"TossBillingSecretKey":          "live_sk_billing_stale_missing_rows",
		"TossBillingTestClientKey":      "test_ck_billing_stale_missing_rows",
		"TossBillingTestSecretKey":      "test_sk_billing_stale_missing_rows",
		"TossUnitPrice":                 "9999",
		"TossMinTopUp":                  "999",
	}))
	// Simulate a partial restore/manual repair that retained only one Toss row.
	// No absent field may inherit the previous process snapshot.
	require.NoError(t, DB.Create(&Option{Key: "TossEnabled", Value: "false"}).Error)
	attestExistingTossOptionRowsForTest(t)

	loadOptionsFromDatabase()

	snapshot := setting.GetTossConfigSnapshot()
	require.False(t, snapshot.Enabled)
	require.False(t, snapshot.BillingEnabled)
	require.False(t, snapshot.WalletAutoRechargeEnabled)
	require.False(t, snapshot.TestMode)
	require.Empty(t, snapshot.ClientKey)
	require.Empty(t, snapshot.SecretKey)
	require.Empty(t, snapshot.TestClientKey)
	require.Empty(t, snapshot.TestSecretKey)
	require.Empty(t, snapshot.BillingClientKey)
	require.Empty(t, snapshot.BillingSecretKey)
	require.Empty(t, snapshot.BillingTestClientKey)
	require.Empty(t, snapshot.BillingTestSecretKey)
	require.Equal(t, setting.TossDefaultUnitPriceKRW, snapshot.UnitPrice)
	require.Equal(t, int(setting.TossCardMinimumAmountKRW), snapshot.MinTopUp)
}

func TestLoadTossOptionsEmptyAuthoritativeSetFailsClosed(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":        "true",
		"TossBillingEnabled": "true",
		"TossSecretKey":      "live_sk_stale_empty_table",
	}))

	loadOptionsFromDatabase()
	snapshot := setting.GetTossConfigSnapshot()
	require.False(t, snapshot.Enabled)
	require.False(t, snapshot.BillingEnabled)
	require.Empty(t, snapshot.SecretKey)
}

func TestUpdateTossOptionEmptySecretDoesNotRequireEncryptionKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Setenv(TossOptionSecretEncryptionEnv, "false")
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	// Model the rolling-upgrade state that this path is meant to recover: an
	// attested legacy plaintext credential exists, but this node has no durable
	// encryption key. Clearing it must remain possible because no new secret is
	// persisted and the provider can only become less capable.
	values := defaultTossOptionValues()
	values["TossClientKey"] = "live_ck_to_clear"
	values["TossSecretKey"] = "live_sk_to_clear"
	rows := make([]Option, 0, len(values))
	for key, value := range values {
		rows = append(rows, Option{Key: key, Value: value})
	}
	require.NoError(t, DB.Create(&rows).Error)
	attestExistingTossOptionRowsForTest(t)
	fallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())

	require.NoError(t, UpdateTossOptionsBulk(map[string]string{
		"TossClientKey": "",
		"TossSecretKey": "",
	}, fallback, nil))
	var stored Option
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.Empty(t, stored.Value)
	require.Empty(t, setting.GetTossConfigSnapshot().SecretKey)
}

func TestUpdateTossOptionsDoesNotResurrectMissingCredentialFromStaleFallback(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":   "true",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_stale_deleted_row",
		"TossSecretKey": "live_sk_stale_deleted_row",
		"TossUnitPrice": "1300",
		"TossMinTopUp":  "100",
	}))
	staleFallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())
	// The database is authoritative and no longer has either credential row.
	// An unrelated safe disable must not recreate them from stale process state.
	require.NoError(t, DB.Create(&Option{Key: "TossEnabled", Value: "true"}).Error)

	err := UpdateTossOptionsBulk(map[string]string{
		"TossEnabled": "false",
	}, staleFallback, nil)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)

	var storedClient, storedSecret Option
	require.ErrorIs(t, DB.Where("key = ?", "TossClientKey").First(&storedClient).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, DB.Where("key = ?", "TossSecretKey").First(&storedSecret).Error, gorm.ErrRecordNotFound)
	require.Empty(t, storedClient.Value)
	require.Empty(t, storedSecret.Value)
	snapshot := setting.GetTossConfigSnapshot()
	require.True(t, snapshot.Enabled)
	require.Equal(t, "live_ck_stale_deleted_row", snapshot.ClientKey)
	require.Equal(t, "live_sk_stale_deleted_row", snapshot.SecretKey)
}

func TestUpdateTossOptionSecretGateFailureKeepsPairAtomic(t *testing.T) {
	setupTossBillingModelTestDB(t)
	preserveTossOptionSecretTestState(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Setenv(TossOptionSecretEncryptionEnv, "false")
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossClientKey", Value: "live_ck_atomic_old"},
		{Key: "TossSecretKey", Value: "live_sk_atomic_old"},
	}).Error)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":   "false",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_atomic_old",
		"TossSecretKey": "live_sk_atomic_old",
		"TossUnitPrice": "1300",
		"TossMinTopUp":  "100",
	}))

	err := UpdateTossOptionsBulk(map[string]string{
		"TossClientKey": "live_ck_atomic_new",
		"TossSecretKey": "live_sk_atomic_new",
	}, tossOptionValuesForTest(setting.GetTossConfigSnapshot()), nil)
	require.ErrorIs(t, err, ErrTossOptionSecretEncryptionDisabled)
	var rows []Option
	require.NoError(t, DB.Where("key IN ?", []string{"TossClientKey", "TossSecretKey"}).Find(&rows).Error)
	stored := make(map[string]string, len(rows))
	for _, row := range rows {
		stored[row.Key] = row.Value
	}
	require.Equal(t, "live_ck_atomic_old", stored["TossClientKey"])
	require.Equal(t, "live_sk_atomic_old", stored["TossSecretKey"])
	require.Equal(t, "live_ck_atomic_old", setting.GetTossConfigSnapshot().ClientKey)
	require.Equal(t, "live_sk_atomic_old", setting.GetTossConfigSnapshot().SecretKey)
}

func TestLegacyTossSecretMigrationUsesByteExactCASUnderNoCaseCollation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	// Simulate the case-insensitive text collation commonly used by MySQL. Toss
	// secrets are case-sensitive, so SQL's default equality is not a safe CAS.
	require.NoError(t, DB.Exec(`CREATE TABLE options (
		"key" text PRIMARY KEY,
		"value" text COLLATE NOCASE
	)`).Error)
	require.NoError(t, DB.Create(&Option{
		Key:   "TossSecretKey",
		Value: "live_sk_CASE_SENSITIVE",
	}).Error)

	err := migrateLegacyTossOptionSecrets(map[string]string{
		"TossSecretKey": "live_sk_case_sensitive",
	})
	require.ErrorIs(t, err, ErrTossOptionValidation)

	var stored Option
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.Equal(t, "live_sk_CASE_SENSITIVE", stored.Value,
		"a stale migration must not overwrite a case-distinct rotated secret")
}

func TestTossOptionExactValuePredicateUsesPostgreSQLByteaEquality(t *testing.T) {
	originalSQLite := common.UsingSQLite
	originalMySQL := common.UsingMySQL
	originalPostgreSQL := common.UsingPostgreSQL
	t.Cleanup(func() {
		common.UsingSQLite = originalSQLite
		common.UsingMySQL = originalMySQL
		common.UsingPostgreSQL = originalPostgreSQL
	})
	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = true

	require.Equal(t,
		`convert_to("value", 'UTF8') = convert_to(?, 'UTF8')`,
		tossOptionExactValuePredicate(),
	)
}
