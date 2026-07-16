package model

import (
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func tossOptionValuesForTest(snapshot setting.TossConfigSnapshot) map[string]string {
	return map[string]string{
		"TossEnabled":                   strconv.FormatBool(snapshot.Enabled),
		"TossBillingEnabled":            strconv.FormatBool(snapshot.BillingEnabled),
		"TossWalletAutoRechargeEnabled": strconv.FormatBool(snapshot.WalletAutoRechargeEnabled),
		"TossTestMode":                  strconv.FormatBool(snapshot.TestMode),
		"TossClientKey":                 snapshot.ClientKey,
		"TossSecretKey":                 snapshot.SecretKey,
		"TossTestClientKey":             snapshot.TestClientKey,
		"TossTestSecretKey":             snapshot.TestSecretKey,
		"TossBillingClientKey":          snapshot.BillingClientKey,
		"TossBillingSecretKey":          snapshot.BillingSecretKey,
		"TossBillingTestClientKey":      snapshot.BillingTestClientKey,
		"TossBillingTestSecretKey":      snapshot.BillingTestSecretKey,
		"TossUnitPrice":                 strconv.FormatFloat(snapshot.UnitPrice, 'f', -1, 64),
		"TossMinTopUp":                  strconv.Itoa(snapshot.MinTopUp),
	}
}

func persistAttestedTossOptionsForTest(t *testing.T, updates map[string]string) string {
	t.Helper()
	complete := defaultTossOptionValues()
	for key, value := range updates {
		complete[key] = value
	}
	desiredEnabled := map[string]string{
		"TossEnabled":                   complete["TossEnabled"],
		"TossBillingEnabled":            complete["TossBillingEnabled"],
		"TossWalletAutoRechargeEnabled": complete["TossWalletAutoRechargeEnabled"],
	}
	complete["TossEnabled"] = "false"
	complete["TossBillingEnabled"] = "false"
	complete["TossWalletAutoRechargeEnabled"] = "false"
	state, err := GetTossConfigState()
	require.NoError(t, err)
	require.True(t, state.RepairRequired)
	require.NoError(t, RepairTossOptionsBulk(
		complete,
		state.RepairToken,
		func(map[string]string, map[string]string) error { return nil },
	))
	require.NoError(t, CompleteTossConfigurationMaintenance())
	if desiredEnabled["TossEnabled"] == "true" || desiredEnabled["TossBillingEnabled"] == "true" || desiredEnabled["TossWalletAutoRechargeEnabled"] == "true" {
		require.NoError(t, UpdateTossOptionsBulk(
			desiredEnabled,
			tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
			func(map[string]string, map[string]string) error { return nil },
		))
	}
	var revision Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&revision).Error)
	_, attested := tossConfigRevisionDigest(revision.Value)
	require.True(t, attested)
	return revision.Value
}

func TestUpdateTossOptionsBulkUsesLockedPersistedSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))

	original := setting.GetTossConfigSnapshot()
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossTestMode":       "false",
		"TossClientKey":      "live_ck_stale_process",
		"TossSecretKey":      "live_sk_stale_process",
		"TossUnitPrice":      "1300",
		"TossMinTopUp":       "100",
	}))
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValues(tossOptionValuesForTest(original)))
	})
	fallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossClientKey": "live_ck_persisted_mid",
		"TossSecretKey": "live_sk_persisted_mid",
		"TossMinTopUp":  "100",
	})
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":   "false",
		"TossClientKey": "live_ck_stale_process",
		"TossSecretKey": "live_sk_stale_process",
	}))
	validatedPersistedState := false
	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{"TossEnabled": "true"},
		fallback,
		func(current, updates map[string]string) error {
			validatedPersistedState = current["TossClientKey"] == "live_ck_persisted_mid" &&
				current["TossSecretKey"] == "live_sk_persisted_mid"
			return nil
		},
	))
	require.True(t, validatedPersistedState)
	require.Equal(t, "live_ck_persisted_mid", setting.GetTossConfigSnapshot().ClientKey)
	require.Equal(t, "live_sk_persisted_mid", setting.GetTossConfigSnapshot().SecretKey)
	require.Equal(t, 100, setting.GetTossConfigSnapshot().MinTopUp)
	var storedMinimum Option
	require.NoError(t, DB.Where("key = ?", "TossMinTopUp").First(&storedMinimum).Error)
	require.Equal(t, "100", storedMinimum.Value)
	var revision Option
	require.NoError(t, DB.Where("key = ?", tossOptionWriteLockKey).First(&revision).Error)
	require.NotEmpty(t, revision.Value)
	require.Equal(t, revision.Value, setting.GetTossConfigSnapshot().Revision)

	options, err := AllOption()
	require.NoError(t, err)
	for _, option := range options {
		require.NotEqual(t, tossOptionWriteLockKey, option.Key)
	}
}

func TestRequireFreshTossConfigRejectsAndRefreshesStaleNode(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":        "true",
		"TossBillingEnabled": "true",
	}, "stale_revision"))

	err := RequireFreshTossConfig()

	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	require.Equal(t, authoritativeRevision, setting.GetTossConfigSnapshot().Revision)
	require.False(t, setting.GetTossConfigSnapshot().Enabled)
	require.False(t, setting.GetTossConfigSnapshot().BillingEnabled)
	latest, err := GetFreshTossConfigSnapshot()
	require.NoError(t, err)
	require.Equal(t, authoritativeRevision, latest.Revision)
}

func TestRefreshTossOptionsCoalescesConcurrentRevisionMismatch(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":        "true",
		"TossBillingEnabled": "true",
	}, "stale_revision"))

	const workers = 16
	start := make(chan struct{})
	results := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, caughtUp, err := refreshTossOptionsAfterRevisionMismatch(authoritativeRevision)
			results <- caughtUp
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	reloadOwners := 0
	coalescedWaiters := 0
	for caughtUp := range results {
		if caughtUp {
			coalescedWaiters++
		} else {
			reloadOwners++
		}
	}
	require.Equal(t, 1, reloadOwners, "only one waiter may perform the full option reload")
	require.Equal(t, workers-1, coalescedWaiters)
	require.Equal(t, authoritativeRevision, setting.GetTossConfigSnapshot().Revision)
}

func TestRefreshTossOptionsDoesNotAcceptRevisionChangedWhileWaiting(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{"TossEnabled": "false"})

	latest, caughtUp, err := refreshTossOptionsAfterRevisionMismatch("older_observed_revision")
	require.NoError(t, err)
	require.False(t, caughtUp, "a request that observed a superseded revision must remain stale")
	require.Equal(t, authoritativeRevision, latest.Revision)
}

func TestRequireFreshTossConfigRejectsUnattestedRevision(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, DB.Create(&Option{Key: tossOptionWriteLockKey, Value: common.GetUUID()}).Error)

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestRequireFreshTossConfigRejectsPersistedRowsWithoutRevision(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossEnabled", Value: "true"},
		{Key: "TossClientKey", Value: "live_ck_unattested_restore"},
		{Key: "TossSecretKey", Value: "live_sk_unattested_restore"},
	}).Error)
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":   "true",
		"TossClientKey": "live_ck_unattested_restore",
		"TossSecretKey": "live_sk_unattested_restore",
	}, ""))

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestRequireFreshTossConfigRejectsMissingDefaultValuedAttestedRow(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	persistAttestedTossOptionsForTest(t, map[string]string{})
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "TossTestClientKey").Delete(&Option{}).Error)

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	loadOptionsFromDatabase()
	_, err = GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestRequireFreshTossConfigRejectsOutOfBandRowsWithoutRevisionAdvance(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{
		"TossClientKey": "live_ck_attested_mid",
		"TossSecretKey": "live_sk_attested_mid",
	})
	before := setting.GetTossConfigSnapshot()
	require.Equal(t, authoritativeRevision, before.Revision)

	// Simulate an old node's generic single-row writer. It does not know about
	// the revision marker and can otherwise publish a mixed key pair.
	require.NoError(t, DB.Model(&Option{}).
		Where(commonKeyCol+" = ?", "TossClientKey").
		UpdateColumn("value", "live_ck_out_of_band_mid").Error)
	var revision Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&revision).Error)
	require.Equal(t, authoritativeRevision, revision.Value)

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	require.Equal(t, before.ClientKey, setting.GetTossConfigSnapshot().ClientKey)
	require.Equal(t, before.SecretKey, setting.GetTossConfigSnapshot().SecretKey)

	loadOptionsFromDatabase()
	require.Empty(t, setting.GetTossConfigSnapshot().SecretKey, "periodic sync must fail closed instead of blessing partial rows")
	_, err = GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestRequireFreshTossConfigRejectsSameRevisionMemoryMutation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{
		"TossClientKey": "live_ck_memory_attested",
		"TossSecretKey": "live_sk_memory_attested",
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossClientKey": "live_ck_memory_tampered",
	}, authoritativeRevision))

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestUntrustedTossRowsRemainQuarantinedUntilAllCredentialSlotsAreRepaired(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":   "true",
		"TossClientKey": "live_ck_quarantine_original",
		"TossSecretKey": "live_sk_quarantine_original",
	})
	require.NoError(t, DB.Model(&Option{}).
		Where(commonKeyCol+" = ?", "TossClientKey").
		UpdateColumn("value", "live_ck_quarantine_partial").Error)

	err := UpdateTossOptionsBulk(
		map[string]string{"TossMinTopUp": "200"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale, "an unrelated update must not bless mixed credentials while enabled")

	err = UpdateTossOptionsBulk(
		map[string]string{
			"TossEnabled":                   "false",
			"TossBillingEnabled":            "false",
			"TossWalletAutoRechargeEnabled": "false",
		},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	var revision Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&revision).Error)
	_, err = GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)

	err = UpdateTossOptionsBulk(
		map[string]string{"TossEnabled": "true"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale, "quarantine must survive a later enable attempt")

	completeRepair := map[string]string{
		"TossTestMode":                  "false",
		"TossClientKey":                 "live_ck_quarantine_repaired",
		"TossSecretKey":                 "live_sk_quarantine_repaired",
		"TossTestClientKey":             "",
		"TossTestSecretKey":             "",
		"TossBillingClientKey":          "",
		"TossBillingSecretKey":          "",
		"TossBillingTestClientKey":      "",
		"TossBillingTestSecretKey":      "",
		"TossEnabled":                   "false",
		"TossBillingEnabled":            "false",
		"TossWalletAutoRechargeEnabled": "false",
		"TossUnitPrice":                 "1300",
		"TossMinTopUp":                  "100",
	}
	err = UpdateTossOptionsBulk(
		completeRepair,
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		func(map[string]string, map[string]string) error { return nil },
	)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	state, err := GetTossConfigState()
	require.NoError(t, err)
	require.True(t, state.RepairRequired)
	require.NotEmpty(t, state.RepairToken)
	require.NoError(t, RepairTossOptionsBulk(
		completeRepair,
		state.RepairToken,
		func(map[string]string, map[string]string) error { return nil },
	))
	require.NoError(t, DB.Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&revision).Error)
	_, attested := tossConfigRevisionDigest(revision.Value)
	require.True(t, attested)
	err = UpdateTossOptionsBulk(
		map[string]string{"TossEnabled": "true"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	)
	require.ErrorIs(t, err, ErrTossConfigMaintenanceRequired)
	require.NoError(t, CompleteTossConfigurationMaintenance())
	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{"TossEnabled": "true"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	))
}

func TestAttestedEnabledTossConfigRequiresPersistentCryptoSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
	})
	// Keep the process key unchanged so the persisted attestation remains
	// verifiable. The environment value alone is deliberately non-persistent
	// (and does not match the active key), which must prevent re-enabling Toss.
	t.Setenv("CRYPTO_SECRET", "short-lived")

	err := UpdateTossOptionsBulk(
		map[string]string{"TossEnabled": "true"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	)
	require.ErrorIs(t, err, ErrTossBillingCryptoSecretNotPersistent)

	// Emergency disable never authorizes a provider POST and must remain
	// possible even while the persistent secret is being repaired.
	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{
			"TossEnabled":                   "false",
			"TossBillingEnabled":            "false",
			"TossWalletAutoRechargeEnabled": "false",
		},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	))
}

func TestRequireFreshTossConfigFailsClosedWhenObservedRevisionRowDisappears(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})

	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":   "true",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_revision_deleted",
		"TossSecretKey": "live_sk_revision_deleted",
	}, "observed_revision"))
	require.NoError(t, DB.Create(&Option{Key: tossOptionWriteLockKey, Value: "observed_revision"}).Error)
	require.NoError(t, DB.Where("key = ?", tossOptionWriteLockKey).Delete(&Option{}).Error)

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
	require.Equal(t, "observed_revision", setting.GetTossConfigSnapshot().Revision)
	// No implicit refresh may bless the stale key pair while the authoritative
	// generation marker is absent.
	require.Equal(t, "live_sk_revision_deleted", setting.GetTossConfigSnapshot().SecretKey)
}

func TestRequireFreshTossConfigFailsClosedWhenObservedOptionTableDisappears(t *testing.T) {
	setupTossBillingModelTestDB(t)
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":   "true",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_missing_option_table",
		"TossSecretKey": "live_sk_missing_option_table",
	}, "observed_before_table_loss"))
	require.False(t, DB.Migrator().HasTable(&Option{}))

	_, err := GetFreshTossConfigSnapshot()

	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestTossProviderPOSTBarrierFailsClosedWhenObservedOptionTableDisappears(t *testing.T) {
	setupTossBillingModelTestDB(t)
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossBillingEnabled":   "true",
		"TossBillingClientKey": "live_ck_attempt_barrier_table_loss",
		"TossBillingSecretKey": "live_sk_attempt_barrier_table_loss",
	}, "observed_before_attempt_barrier_table_loss"))
	require.False(t, DB.Migrator().HasTable(&Option{}))

	var barrier tossProviderPOSTBarrier
	err := DB.Transaction(func(tx *gorm.DB) error {
		var inspectErr error
		barrier, inspectErr = inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTBilling)
		return inspectErr
	})
	require.NoError(t, err)
	require.ErrorIs(t, barrier.PolicyError, ErrTossConfigRevisionStale,
		"a previously observed durable generation must not become a pre-schema fail-open after table loss")
}

func TestRequireFreshTossConfigAllowsTrulyLegacyEmptyRevision(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), ""))

	snapshot, err := GetFreshTossConfigSnapshot()
	require.NoError(t, err)
	require.Empty(t, snapshot.Revision)
}

func TestRequireFreshTossConfigRejectsCorruptEmptyRevisionRow(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), original.Revision))
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(original), ""))
	require.NoError(t, DB.Create(&Option{Key: tossOptionWriteLockKey, Value: ""}).Error)

	_, err := GetFreshTossConfigSnapshot()
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)
}

func TestGenericOptionWritersRejectTossKeys(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.ErrorIs(t, UpdateOption("TossSecretKey", "live_sk_bypass"), ErrTossOptionValidation)
	require.ErrorIs(t, UpdateOption(tossOptionWriteLockKey, "attacker_revision"), ErrTossOptionValidation)
	require.ErrorIs(t, UpdateOptionsBulk(map[string]string{"TossEnabled": "true"}), ErrTossOptionValidation)
	require.ErrorIs(t, UpdateOptionsBulk(map[string]string{tossOptionWriteLockKey: "attacker_revision"}), ErrTossOptionValidation)
	require.ErrorIs(t, UpdateTossOptionsBulk(
		map[string]string{"TossSecretKey": "live_sk_unpaired"},
		tossOptionValuesForTest(setting.GetTossConfigSnapshot()),
		nil,
	), ErrTossOptionValidation)
}

func TestUpdateTossOptionsBulkBackfillsEveryLegacyProviderMIDBeforeRotation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &TopUp{}, &SubscriptionOrder{}))

	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValues(tossOptionValuesForTest(original)))
	})
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":              "true",
		"TossBillingEnabled":       "true",
		"TossTestMode":             "false",
		"TossClientKey":            "live_ck_legacy_regular_mid",
		"TossSecretKey":            "live_sk_legacy_regular_old",
		"TossTestClientKey":        "test_ck_legacy_regular_mid",
		"TossTestSecretKey":        "test_sk_legacy_regular",
		"TossBillingClientKey":     "live_ck_legacy_billing_mid",
		"TossBillingSecretKey":     "live_sk_legacy_billing_old",
		"TossBillingTestClientKey": "test_ck_legacy_billing_mid",
		"TossBillingTestSecretKey": "test_sk_legacy_billing",
		"TossUnitPrice":            "1300",
		"TossMinTopUp":             "100",
	}))
	// Legacy provider rows can only be mapped to an MID when the pre-rotation
	// pair is authoritative in the options table. Runtime state alone may be
	// stale after a restore and must not be resurrected by the atomic writer.
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossEnabled", Value: "true"},
		{Key: "TossBillingEnabled", Value: "true"},
		{Key: "TossWalletAutoRechargeEnabled", Value: "false"},
		{Key: "TossTestMode", Value: "false"},
		{Key: "TossClientKey", Value: "live_ck_legacy_regular_mid"},
		{Key: "TossSecretKey", Value: "live_sk_legacy_regular_old"},
		{Key: "TossTestClientKey", Value: "test_ck_legacy_regular_mid"},
		{Key: "TossTestSecretKey", Value: "test_sk_legacy_regular"},
		{Key: "TossBillingClientKey", Value: "live_ck_legacy_billing_mid"},
		{Key: "TossBillingSecretKey", Value: "live_sk_legacy_billing_old"},
		{Key: "TossBillingTestClientKey", Value: "test_ck_legacy_billing_mid"},
		{Key: "TossBillingTestSecretKey", Value: "test_sk_legacy_billing"},
		{Key: "TossUnitPrice", Value: "1300"},
		{Key: "TossMinTopUp", Value: "100"},
	}).Error)
	attestExistingTossOptionRowsForTest(t)
	fallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())

	regularCredential, err := EncryptProviderCredential("live_sk_legacy_regular_old")
	require.NoError(t, err)
	billingCredential, err := EncryptProviderCredential("live_sk_legacy_billing_old")
	require.NoError(t, err)

	topUp := &TopUp{
		TradeNo:            "toss_legacy_mid_topup",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		ProviderCredential: regularCredential,
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(topUp).Error)
	order := &SubscriptionOrder{
		TradeNo:            "toss_sub_legacy_mid_order",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		ProviderCredential: billingCredential,
		Status:             common.TopUpStatusSuccess,
	}
	require.NoError(t, DB.Create(order).Error)
	policy := &WalletAutoRecharge{
		Type:               WalletAutoRechargeTypeScheduled,
		Status:             WalletAutoRechargeStatusCancelled,
		ProviderCredential: billingCredential,
	}
	require.NoError(t, DB.Create(policy).Error)
	billingKey := &UserBillingKey{
		UserId:             7,
		CustomerKey:        "legacy-mid-customer",
		ProviderCredential: billingCredential,
		Status:             BillingKeyStatusRevoked,
	}
	require.NoError(t, DB.Create(billingKey).Error)

	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{
			"TossClientKey":        "live_ck_legacy_regular_mid",
			"TossSecretKey":        "live_sk_legacy_regular_rotated",
			"TossBillingClientKey": "live_ck_legacy_billing_mid",
			"TossBillingSecretKey": "live_sk_legacy_billing_rotated",
		},
		fallback,
		nil,
	))

	regularHash := TossClientKeyFingerprint("live_ck_legacy_regular_mid")
	billingHash := TossClientKeyFingerprint("live_ck_legacy_billing_mid")
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, regularHash, topUp.ProviderClientKeyHash)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, billingHash, order.ProviderClientKeyHash)
	require.NoError(t, DB.First(policy, policy.Id).Error)
	require.Equal(t, billingHash, policy.ProviderClientKeyHash)
	require.NoError(t, DB.First(billingKey, billingKey.Id).Error)
	require.Equal(t, billingHash, billingKey.ProviderClientKeyHash)
}

func TestUpdateTossOptionsBulkAllowsRotationButIsolatesUnresolvedLegacyMID(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &TopUp{}))

	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValues(tossOptionValuesForTest(original)))
	})
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_unresolved_mid",
		"TossSecretKey": "live_sk_unresolved_old",
		"TossUnitPrice": "1300",
		"TossMinTopUp":  "100",
	}))
	fallback := tossOptionValuesForTest(setting.GetTossConfigSnapshot())
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":   "false",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_unresolved_mid",
		"TossSecretKey": "live_sk_unresolved_old",
		"TossUnitPrice": "1300",
		"TossMinTopUp":  "100",
	})
	topUp := &TopUp{
		TradeNo:            "toss_unresolved_legacy_mid",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		ProviderCredential: "corrupt-ciphertext",
		Status:             common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{
			"TossClientKey": "live_ck_unresolved_mid",
			"TossSecretKey": "live_sk_unresolved_rotated",
		},
		fallback,
		nil,
	))
	var stored Option
	require.NoError(t, DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	plain, _, err := decryptTossOptionSecret("TossSecretKey", stored.Value)
	require.NoError(t, err)
	require.Equal(t, "live_sk_unresolved_rotated", plain)

	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Empty(t, topUp.ProviderClientKeyHash,
		"a corrupt historical credential must remain outside the new MID namespace")

	// A readable credential from a different historical MID is likewise exact
	// only. An empty fingerprint must never authorize fallback to the newly
	// configured secret after rotation.
	candidates, err := tossBillingSecretCandidates(&UserBillingKey{}, "live_sk_other_historical_mid", false)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_other_historical_mid"}, candidates)
}

func TestBatchedTossMIDBackfillCommitsSafePrefixAndRejectsStaleFinalRotation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &TopUp{}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":   "false",
		"TossClientKey": "live_ck_batched_mid",
		"TossSecretKey": "live_sk_batched_old",
	})
	credential, err := EncryptProviderCredential("live_sk_batched_old")
	require.NoError(t, err)
	rows := make([]TopUp, tossLegacyMIDBackfillWriteBatchSize+25)
	for i := range rows {
		rows[i] = TopUp{
			TradeNo:            "toss_batched_mid_" + strconv.Itoa(i),
			PaymentMethod:      PaymentMethodToss,
			PaymentProvider:    PaymentProviderToss,
			ProviderCredential: credential,
			Status:             common.TopUpStatusSuccess,
		}
	}
	require.NoError(t, DB.Create(&rows).Error)

	hookCalls := 0
	tossMIDBackfillBatchCommittedHook = func() {
		hookCalls++
		if hookCalls == 1 {
			require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", tossOptionWriteLockKey).UpdateColumn("value", common.GetUUID()).Error)
		}
	}
	t.Cleanup(func() { tossMIDBackfillBatchCommittedHook = nil })
	err = UpdateTossOptionsBulk(map[string]string{
		"TossClientKey": "live_ck_batched_mid",
		"TossSecretKey": "live_sk_batched_rotated",
	}, nil, nil)
	require.ErrorIs(t, err, ErrTossConfigRevisionStale)

	expectedHash := TossClientKeyFingerprint("live_ck_batched_mid")
	var hashed int64
	require.NoError(t, DB.Model(&TopUp{}).Where("provider_client_key_hash = ?", expectedHash).Count(&hashed).Error)
	require.EqualValues(t, tossLegacyMIDBackfillWriteBatchSize, hashed)
	var stored Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "TossSecretKey").First(&stored).Error)
	plain, _, decryptErr := decryptTossOptionSecret("TossSecretKey", stored.Value)
	require.NoError(t, decryptErr)
	require.Equal(t, "live_sk_batched_old", plain)
}
