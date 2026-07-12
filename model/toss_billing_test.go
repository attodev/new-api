package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTossBillingModelTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalSecret := common.CryptoSecret
	originalTossSnapshot := setting.GetTossConfigSnapshot()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	require.NoError(t, DB.AutoMigrate(
		&User{},
		&Organization{},
		&UserSubscription{},
		&UserBillingKey{},
		&TossPaymentEvent{},
		&TossRecurringOrderIDProtocolState{},
		&WalletAutoRecharge{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPlan{},
	))
	require.NoError(t, initializeTossRecurringOrderIDProtocolState(true))
	t.Setenv("CRYPTO_SECRET", "toss-billing-test-secret-at-least-32-bytes")
	t.Setenv(TossOptionSecretEncryptionEnv, "true")
	common.CryptoSecret = "toss-billing-test-secret-at-least-32-bytes"
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
		"TossTestMode":                  "false",
		"TossClientKey":                 "",
		"TossSecretKey":                 "",
		"TossTestClientKey":             "",
		"TossTestSecretKey":             "",
		"TossBillingClientKey":          "ck_test_billing_model_default",
		"TossBillingSecretKey":          "test_sk_billing_model_default",
		"TossBillingTestClientKey":      "",
		"TossBillingTestSecretKey":      "",
		"TossUnitPrice":                 "1000",
		"TossMinTopUp":                  "100",
	}, ""))
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		common.CryptoSecret = originalSecret
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(
			tossOptionValuesForTest(originalTossSnapshot), originalTossSnapshot.Revision,
		))
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
	})
}

func requireTossRenewalOrderByAttemptForTest(t *testing.T, subscriptionID, attempt int) SubscriptionOrder {
	t.Helper()
	var order SubscriptionOrder
	require.NoError(t, DB.Where("renewal_subscription_id = ? AND renewal_attempt = ?", subscriptionID, attempt).
		Order("id desc").First(&order).Error)
	return order
}

func seedTossRenewalContractForTest(t *testing.T, subID int, plan *SubscriptionPlan, providerAmount int64) {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, subID).Error)
	order := &SubscriptionOrder{
		UserId:           sub.UserId,
		PlanId:           plan.Id,
		Money:            plan.PriceAmount,
		TradeNo:          fmt.Sprintf("toss_sub_seed_contract_%d", sub.Id),
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusSuccess,
		ProviderAmount:   providerAmount,
		ProviderCurrency: "KRW",
		BillingKeyId:     sub.BillingKeyId,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	require.NoError(t, SetTossRenewalContractFromInitialOrder(&sub, order))
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).
		Update("toss_renewal_contract_snapshot", sub.TossRenewalContractSnapshot).Error)
}

func seedTossBillingSubscription(t *testing.T, billingKey string, failCount int) int {
	t.Helper()
	var user User
	err := DB.First(&user, 7).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		require.NoError(t, DB.Create(&User{Id: 7, Username: "toss-billing-user", Password: "password", Status: common.UserStatusEnabled, Group: "default", AffCode: "toss-billing-aff"}).Error)
	} else {
		require.NoError(t, err)
	}
	keyId, err := StoreTossBillingKey(7, "cust_test", billingKey, "현대", "433012******1234")
	require.NoError(t, err)
	sub := &UserSubscription{
		Id:               11,
		UserId:           7,
		PlanId:           3,
		Status:           "active",
		StartTime:        time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:          time.Now().Add(24 * time.Hour).Unix(),
		AutoRenew:        true,
		BillingKeyId:     keyId,
		BillingFailCount: failCount,
		NextBillingTime:  time.Now().Unix(),
	}
	require.NoError(t, DB.Create(sub).Error)
	return keyId
}

func seedTossBillingPlan(t *testing.T, title string, priceAmount float64) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	plan := &SubscriptionPlan{
		Id:            3,
		Title:         title,
		PriceAmount:   priceAmount,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	var sub UserSubscription
	if err := DB.Where("plan_id = ?", plan.Id).First(&sub).Error; err == nil {
		seedTossRenewalContractForTest(t, sub.Id, plan, TossPlanKRW(plan.PriceAmount))
	}
}

func TestUserBillingKeyStatusColumnFitsPendingRevocation(t *testing.T) {
	field, ok := reflect.TypeOf(UserBillingKey{}).FieldByName("Status")
	require.True(t, ok)
	tag := field.Tag.Get("gorm")
	require.NotContains(t, tag, "varchar(16)")
	require.True(t, strings.Contains(tag, "varchar(32)") || strings.Contains(tag, "varchar(64)"), "gorm tag %q should fit %q", tag, BillingKeyStatusPendingRevocation)
}

func TestValidateTossBillingCryptoConfigurationRequiresPersistentSecret(t *testing.T) {
	originalSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = originalSecret })

	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-process-secret"
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent)

	t.Setenv("CRYPTO_SECRET", "persistent-toss-secret-at-least-32-bytes")
	common.CryptoSecret = "persistent-toss-secret-at-least-32-bytes"
	require.NoError(t, ValidateTossBillingCryptoConfiguration())

	common.CryptoSecret = "different-active-secret"
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent)
}

func TestValidateTossBillingCryptoConfigurationRequiresMinimumByteEntropyBoundary(t *testing.T) {
	originalSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
	t.Setenv("SESSION_SECRET", "")

	shortASCII := strings.Repeat("a", tossBillingCryptoSecretMinBytes-1)
	t.Setenv("CRYPTO_SECRET", shortASCII)
	common.CryptoSecret = shortASCII
	err := ValidateTossBillingCryptoConfiguration()
	require.ErrorIs(t, err, ErrTossBillingCryptoSecretNotPersistent)
	require.Contains(t, err.Error(), "at least 32 bytes")

	exactASCII := strings.Repeat("b", tossBillingCryptoSecretMinBytes)
	t.Setenv("CRYPTO_SECRET", "  "+exactASCII+"\t")
	common.CryptoSecret = exactASCII
	require.NoError(t, ValidateTossBillingCryptoConfiguration(), "trimmed 32-byte secret must remain valid")

	shortUnicode := strings.Repeat("가", 10) // 30 UTF-8 bytes, despite 10 runes.
	t.Setenv("CRYPTO_SECRET", shortUnicode)
	common.CryptoSecret = shortUnicode
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent)

	validUnicode := strings.Repeat("가", 11) // 33 UTF-8 bytes.
	t.Setenv("CRYPTO_SECRET", validUnicode)
	common.CryptoSecret = validUnicode
	require.NoError(t, ValidateTossBillingCryptoConfiguration())
}

func TestValidateTossBillingCryptoConfigurationAppliesMinimumToSessionFallback(t *testing.T) {
	originalSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
	t.Setenv("CRYPTO_SECRET", "")

	short := strings.Repeat("s", tossBillingCryptoSecretMinBytes-1)
	t.Setenv("SESSION_SECRET", short)
	common.CryptoSecret = short
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent)

	valid := strings.Repeat("s", tossBillingCryptoSecretMinBytes)
	t.Setenv("SESSION_SECRET", valid)
	common.CryptoSecret = valid
	require.NoError(t, ValidateTossBillingCryptoConfiguration())

	shortCrypto := strings.Repeat("c", tossBillingCryptoSecretMinBytes-1)
	t.Setenv("CRYPTO_SECRET", shortCrypto)
	common.CryptoSecret = shortCrypto
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent, "an explicit weak CRYPTO_SECRET must not fall back to SESSION_SECRET")
	t.Setenv("CRYPTO_SECRET", "")

	t.Setenv("SESSION_SECRET", "random_string")
	common.CryptoSecret = "random_string"
	require.ErrorIs(t, ValidateTossBillingCryptoConfiguration(), ErrTossBillingCryptoSecretNotPersistent)
}

func TestStoreTossBillingKeyRejectsEphemeralCryptoSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-process-secret"

	_, err := StoreTossBillingKeyWithSecret(7, "cust_ephemeral", "billing_ephemeral", "현대", "433012******1234", "test_sk_ephemeral")
	require.ErrorIs(t, err, ErrTossBillingCryptoSecretNotPersistent)
}

func TestEncryptProviderCredentialRejectsEphemeralCryptoSecret(t *testing.T) {
	originalSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-provider-credential-secret"

	_, err := EncryptProviderCredential("live_sk_regular_topup")
	require.ErrorIs(t, err, ErrTossBillingCryptoSecretNotPersistent)
	// Empty snapshots remain valid for legacy/non-provider rows and do not need
	// an encryption key.
	empty, err := EncryptProviderCredential("")
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestGetTossBillingKeyPlainWithSecretCandidatesOrdersCurrentSameMIDSecretBeforeStoredSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "ck_same_mid"
	setting.TossBillingSecretKey = "sk_old_same_mid"

	keyID, err := StoreTossBillingKeyWithSecret(7, "cust_rotation", "billing_rotation", "현대", "433012******1234", "sk_old_same_mid")
	require.NoError(t, err)
	setting.TossBillingSecretKey = "sk_current_same_mid"

	billingKey, customerKey, secrets, err := GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, "billing_rotation", billingKey)
	require.Equal(t, "cust_rotation", customerKey)
	require.Equal(t, []string{"sk_current_same_mid", "sk_old_same_mid"}, secrets)
}

func TestGetTossBillingKeyPlainWithSecretCandidatesRejectsDifferentMID(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "ck_original_mid"
	setting.TossBillingSecretKey = "sk_original_mid"
	keyID, err := StoreTossBillingKeyWithSecret(7, "cust_mid", "billing_mid", "현대", "433012******1234", "sk_original_mid")
	require.NoError(t, err)

	setting.TossBillingClientKey = "ck_different_mid"
	setting.TossBillingSecretKey = "sk_different_mid"
	_, _, _, err = GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.ErrorIs(t, err, ErrTossBillingMIDMismatch)
}

func TestLegacyBillingKeyNeverInfersMIDFromEnvironmentPrefix(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = ""
	setting.TossBillingSecretKey = ""
	keyID, err := StoreTossBillingKeyWithSecret(7, "cust_legacy", "billing_legacy", "현대", "433012******1234", "live_sk_old")
	require.NoError(t, err)
	setting.TossBillingClientKey = "live_ck_stable_mid"
	setting.TossBillingSecretKey = "live_sk_current"

	_, _, secrets, err := GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_old"}, secrets)

	setting.TossBillingClientKey = "test_ck_other_environment"
	setting.TossBillingSecretKey = "test_sk_other_environment"
	_, _, secrets, err = GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_old"}, secrets)
}

func TestStoreTossBillingKeyFingerprintsSecretPairAcrossModeToggle(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalTestMode := setting.TossTestMode
	originalLiveClient := setting.TossBillingClientKey
	originalLiveSecret := setting.TossBillingSecretKey
	originalTestClient := setting.TossBillingTestClientKey
	originalTestSecret := setting.TossBillingTestSecretKey
	t.Cleanup(func() {
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalLiveClient
		setting.TossBillingSecretKey = originalLiveSecret
		setting.TossBillingTestClientKey = originalTestClient
		setting.TossBillingTestSecretKey = originalTestSecret
	})
	setting.TossBillingClientKey = "live_ck_billing_mid"
	setting.TossBillingSecretKey = "live_sk_billing_issue"
	setting.TossBillingTestClientKey = "test_ck_billing_mid"
	setting.TossBillingTestSecretKey = "test_sk_billing_issue"
	setting.TossTestMode = true // Config changed after a live auth request started.

	keyID, err := StoreTossBillingKeyWithSecret(7, "cust_mode_toggle", "billing_mode_toggle", "현대", "433012******1234", "live_sk_billing_issue")
	require.NoError(t, err)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, tossBillingClientKeyHash("live_ck_billing_mid"), key.ProviderClientKeyHash)
	require.NotEqual(t, tossBillingClientKeyHash("test_ck_billing_mid"), key.ProviderClientKeyHash)
}

func TestUpdateOptionsBulkBackfillsLegacyMIDBeforeSecretRotation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))

	originalSnapshot := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
			"TossTestMode":             strconv.FormatBool(originalSnapshot.TestMode),
			"TossClientKey":            originalSnapshot.ClientKey,
			"TossSecretKey":            originalSnapshot.SecretKey,
			"TossTestClientKey":        originalSnapshot.TestClientKey,
			"TossTestSecretKey":        originalSnapshot.TestSecretKey,
			"TossBillingClientKey":     originalSnapshot.BillingClientKey,
			"TossBillingSecretKey":     originalSnapshot.BillingSecretKey,
			"TossBillingTestClientKey": originalSnapshot.BillingTestClientKey,
			"TossBillingTestSecretKey": originalSnapshot.BillingTestSecretKey,
		}))
	})
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossTestMode":             "false",
		"TossClientKey":            "live_ck_regular_backfill",
		"TossSecretKey":            "live_sk_regular_backfill",
		"TossTestClientKey":        "test_ck_regular_backfill",
		"TossTestSecretKey":        "test_sk_regular_backfill",
		"TossBillingClientKey":     "live_ck_legacy_billing_mid",
		"TossBillingSecretKey":     "live_sk_legacy_billing_old",
		"TossBillingTestClientKey": "test_ck_legacy_billing_mid",
		"TossBillingTestSecretKey": "test_sk_legacy_billing",
	}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossBillingEnabled":   "true",
		"TossTestMode":         "false",
		"TossBillingClientKey": "live_ck_legacy_billing_mid",
		"TossBillingSecretKey": "live_sk_legacy_billing_old",
	})

	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		7, "cust_legacy_rotation", "billing_legacy_rotation", "현대", "433012******1234",
		"live_sk_legacy_billing_old", TossBillingClientKeyFingerprint("live_ck_legacy_billing_mid"),
	)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyID).Update("provider_client_key_hash", "").Error)

	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, UpdateTossOptionsBulk(
		map[string]string{
			"TossBillingClientKey": "live_ck_legacy_billing_mid",
			"TossBillingSecretKey": "live_sk_legacy_billing_rotated",
		},
		map[string]string{
			"TossTestMode":         "false",
			"TossBillingClientKey": "live_ck_legacy_billing_mid",
			"TossBillingSecretKey": "live_sk_legacy_billing_old",
		},
		nil,
	))

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, TossBillingClientKeyFingerprint("live_ck_legacy_billing_mid"), key.ProviderClientKeyHash)
	_, _, secrets, err := GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_legacy_billing_rotated", "live_sk_legacy_billing_old"}, secrets)
}

func TestGetTossBillingKeyLazilyPersistsUniqueLegacyMIDFingerprint(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	t.Cleanup(func() {
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
	})
	setting.TossTestMode = false
	setting.TossClientKey = "live_ck_lazy_regular"
	setting.TossSecretKey = "live_sk_lazy_regular"
	setting.TossBillingClientKey = "live_ck_lazy_billing"
	setting.TossBillingSecretKey = "live_sk_lazy_billing"

	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		7, "cust_lazy", "billing_lazy", "현대", "433012******1234",
		"live_sk_lazy_billing", TossBillingClientKeyFingerprint("live_ck_lazy_billing"),
	)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyID).Update("provider_client_key_hash", "").Error)

	_, _, secrets, err := GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_lazy_billing"}, secrets)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, TossBillingClientKeyFingerprint("live_ck_lazy_billing"), key.ProviderClientKeyHash)
}

func TestBackfillLegacyTossBillingKeyMIDSkipsAmbiguousSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	t.Cleanup(func() {
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
	})
	setting.TossTestMode = false
	setting.TossClientKey = "live_ck_ambiguous_regular"
	setting.TossSecretKey = "live_sk_shared_ambiguous"
	setting.TossBillingClientKey = "live_ck_ambiguous_billing"
	setting.TossBillingSecretKey = "live_sk_shared_ambiguous"
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossClientKey":        "live_ck_ambiguous_regular",
		"TossSecretKey":        "live_sk_shared_ambiguous",
		"TossBillingClientKey": "live_ck_ambiguous_billing",
		"TossBillingSecretKey": "live_sk_shared_ambiguous",
	})

	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		7, "cust_ambiguous", "billing_ambiguous", "현대", "433012******1234",
		"live_sk_shared_ambiguous", TossBillingClientKeyFingerprint("live_ck_ambiguous_billing"),
	)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyID).Update("provider_client_key_hash", "").Error)

	require.NoError(t, BackfillLegacyTossBillingKeyMIDFingerprints())
	_, _, secrets, err := GetTossBillingKeyPlainWithSecretCandidates(keyID)
	require.NoError(t, err)
	require.Equal(t, []string{"live_sk_shared_ambiguous"}, secrets)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Empty(t, key.ProviderClientKeyHash)
}

func TestBackfillTossMIDFingerprintsRepairsMalformedRowsOnlyFromExactUniqueSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))

	const (
		clientKey       = "live_ck_backfill_malformed"
		uniqueSecret    = "live_sk_backfill_malformed_unique"
		ambiguousSecret = "live_sk_backfill_malformed_ambiguous"
	)
	snapshot := setting.TossConfigSnapshot{
		ClientKey:        clientKey,
		SecretKey:        uniqueSecret,
		BillingClientKey: "live_ck_backfill_other_mid",
		BillingSecretKey: ambiguousSecret,
		TestClientKey:    "test_ck_backfill_third_mid",
		TestSecretKey:    ambiguousSecret,
	}
	uniqueCredential, err := EncryptProviderCredential(uniqueSecret)
	require.NoError(t, err)
	ambiguousCredential, err := EncryptProviderCredential(ambiguousSecret)
	require.NoError(t, err)
	unknownCredential, err := EncryptProviderCredential("live_sk_backfill_unknown")
	require.NoError(t, err)
	expectedHash := TossClientKeyFingerprint(clientKey)
	rows := []TopUp{
		{TradeNo: "toss_backfill_corrupt", ProviderCredential: uniqueCredential, ProviderClientKeyHash: "corrupt-hash", PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss},
		{TradeNo: "toss_backfill_uppercase", ProviderCredential: uniqueCredential, ProviderClientKeyHash: strings.ToUpper(expectedHash), PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss},
		{TradeNo: "toss_backfill_ambiguous", ProviderCredential: ambiguousCredential, ProviderClientKeyHash: "ambiguous-corrupt", PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss},
		{TradeNo: "toss_backfill_unknown", ProviderCredential: unknownCredential, ProviderClientKeyHash: "unknown-corrupt", PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss},
	}
	require.NoError(t, DB.Create(&rows).Error)

	var unresolved int64
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		unresolved, err = backfillLegacyTossMIDFingerprintsForModelTx(
			tx,
			&TopUp{},
			snapshot,
			"payment_provider = ?",
			PaymentProviderToss,
		)
		return err
	}))
	require.Equal(t, int64(2), unresolved)

	for i := range rows {
		require.NoError(t, DB.First(&rows[i], rows[i].Id).Error)
	}
	require.Equal(t, expectedHash, rows[0].ProviderClientKeyHash)
	require.Equal(t, expectedHash, rows[1].ProviderClientKeyHash)
	require.Equal(t, "ambiguous-corrupt", rows[2].ProviderClientKeyHash)
	require.Equal(t, "unknown-corrupt", rows[3].ProviderClientKeyHash)
}

func TestBackfillTossMIDFingerprintCASDoesNotOverwriteChangedCorruptValue(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	row := TopUp{
		TradeNo: "toss_backfill_exact_cas", ProviderClientKeyHash: "newer-corrupt-value",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
	}
	require.NoError(t, DB.Create(&row).Error)

	updated, err := backfillTossMIDFingerprintCASTx(
		DB,
		&TopUp{},
		row.Id,
		row.ProviderCredential,
		"stale-corrupt-value",
		TossClientKeyFingerprint("live_ck_backfill_exact_cas"),
	)
	require.NoError(t, err)
	require.False(t, updated)
	require.NoError(t, DB.First(&row, row.Id).Error)
	require.Equal(t, "newer-corrupt-value", row.ProviderClientKeyHash)
}

func TestTossBillingSecretCandidatesTreatMalformedFingerprintAsExactOnly(t *testing.T) {
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		setting.TossTestMode = original.TestMode
		setting.TossBillingClientKey = original.BillingClientKey
		setting.TossBillingSecretKey = original.BillingSecretKey
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "live_ck_malformed_candidate"
	setting.TossBillingSecretKey = "live_sk_malformed_candidate_current"
	canonical := TossClientKeyFingerprint(setting.TossBillingClientKey)

	for _, malformed := range []string{
		strings.ToUpper(canonical),
		" " + canonical,
		canonical + " ",
		canonical[:len(canonical)-1],
		strings.Repeat("z", len(canonical)),
	} {
		for _, requireActiveMID := range []bool{false, true} {
			candidates, err := tossBillingSecretCandidates(
				&UserBillingKey{ProviderClientKeyHash: malformed},
				"live_sk_malformed_candidate_exact",
				requireActiveMID,
			)
			require.NoError(t, err)
			require.Equal(t, []string{"live_sk_malformed_candidate_exact"}, candidates, malformed)
		}
	}
}

func TestTossSameMIDCurrentBillingSecretRejectsMalformedFingerprint(t *testing.T) {
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		setting.TossTestMode = original.TestMode
		setting.TossBillingClientKey = original.BillingClientKey
		setting.TossBillingSecretKey = original.BillingSecretKey
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "live_ck_same_mid_strict"
	setting.TossBillingSecretKey = "live_sk_same_mid_current"
	canonical := TossClientKeyFingerprint(setting.TossBillingClientKey)
	require.Equal(t, setting.TossBillingSecretKey, tossSameMIDCurrentBillingSecret(canonical, "live_sk_same_mid_old"))
	for _, malformed := range []string{
		strings.ToUpper(canonical),
		" " + canonical,
		canonical[:len(canonical)-1],
		strings.Repeat("z", len(canonical)),
	} {
		require.Empty(t, tossSameMIDCurrentBillingSecret(malformed, "live_sk_same_mid_old"), malformed)
	}
}

func TestStoreTossBillingKeyRejectsNonCanonicalExplicitFingerprint(t *testing.T) {
	setupTossBillingModelTestDB(t)
	canonical := TossClientKeyFingerprint("live_ck_store_noncanonical")
	for _, malformed := range []string{strings.ToUpper(canonical), " " + canonical, canonical + " "} {
		_, err := StoreTossBillingKeyWithProviderSnapshot(
			7,
			"cust_store_noncanonical",
			"billing_store_noncanonical_"+common.Sha1([]byte(malformed)),
			"현대",
			"433012******1234",
			"live_sk_store_noncanonical",
			malformed,
		)
		require.Error(t, err, malformed)
	}
}

func TestGetTossProviderClientKeyHashByTradeNoRejectsMalformedStoredEvidence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}, &WalletAutoRecharge{}))
	order := SubscriptionOrder{
		TradeNo: "toss_sub_invalid_mid_source", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, ProviderClientKeyHash: " restored-invalid-mid ",
	}
	require.NoError(t, DB.Create(&order).Error)
	_, err := GetTossProviderClientKeyHashByTradeNo(order.TradeNo)
	require.ErrorIs(t, err, ErrTossBillingMIDMismatch)

	policy := WalletAutoRecharge{
		AuthTradeNo: "wallet_invalid_mid_source", Type: WalletAutoRechargeTypeScheduled,
		ProviderClientKeyHash: strings.ToUpper(TossClientKeyFingerprint("live_ck_invalid_wallet_source")),
	}
	require.NoError(t, DB.Create(&policy).Error)
	_, err = GetTossProviderClientKeyHashByTradeNo(policy.AuthTradeNo)
	require.ErrorIs(t, err, ErrTossBillingMIDMismatch)
}

func TestPendingRevocationKeyIsNeverAttributedToNewActiveMID(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	t.Cleanup(func() {
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
	})
	setting.TossTestMode = false
	setting.TossClientKey = "live_ck_new_regular"
	setting.TossSecretKey = "live_sk_new_regular"
	setting.TossBillingClientKey = "live_ck_new_billing"
	setting.TossBillingSecretKey = "live_sk_new_billing"

	legacyID, err := StoreTossBillingKeyPendingRevocationWithSecret(
		7, "cust_cleanup_legacy", "billing_cleanup_legacy", "현대", "433012******1234", "live_sk_old_billing",
	)
	require.NoError(t, err)
	oldMIDHash := TossBillingClientKeyFingerprint("live_ck_old_billing")
	explicitID, err := StoreTossBillingKeyPendingRevocationWithProviderSnapshot(
		7, "cust_cleanup_exact", "billing_cleanup_exact", "현대", "433012******1234", "live_sk_old_billing", oldMIDHash,
	)
	require.NoError(t, err)

	for _, tc := range []struct {
		id       int
		wantHash string
	}{
		{id: legacyID, wantHash: ""},
		{id: explicitID, wantHash: oldMIDHash},
	} {
		var key UserBillingKey
		require.NoError(t, DB.First(&key, tc.id).Error)
		require.Equal(t, tc.wantHash, key.ProviderClientKeyHash)
		_, _, secrets, err := getTossBillingKeyPlainWithSecretCandidatesTx(nil, tc.id, false, false)
		require.NoError(t, err)
		require.Equal(t, []string{"live_sk_old_billing"}, secrets)
		require.NotContains(t, secrets, "live_sk_new_billing")
	}
}

func TestRevokeStoredTossBillingKeyUsesCurrentSecretAndFallsBackOnAuthError(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "ck_revoke_mid"
	setting.TossBillingSecretKey = "sk_revoke_old"
	keyID, err := StoreTossBillingKeyWithSecret(7, "cust_revoke", "billing_revoke", "현대", "433012******1234", "sk_revoke_old")
	require.NoError(t, err)
	setting.TossBillingSecretKey = "sk_revoke_current"

	var secrets []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		secrets = append(secrets, secretKey)
		if secretKey == "sk_revoke_current" {
			return errors.New("toss api failed: status=401 body=INVALID_API_KEY")
		}
		return nil
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	require.NoError(t, RevokeStoredTossBillingKey(context.Background(), keyID))
	require.Equal(t, []string{"sk_revoke_current", "sk_revoke_old"}, secrets)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestRevokeStoredTossBillingKeyKeepsLiveSharedContractsWithoutRemoteDelete(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_revoke_failure", 0)
	activeKey := "user:7:scheduled"
	require.NoError(t, DB.Create(&WalletAutoRecharge{
		Type:         WalletAutoRechargeTypeScheduled,
		TargetType:   TopUpTargetTypeUser,
		TargetId:     7,
		OwnerUserId:  7,
		BillingKeyId: keyID,
		Status:       WalletAutoRechargeStatusActive,
		ActiveKey:    &activeKey,
	}).Error)
	remoteDeletes := 0
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		remoteDeletes++
		return errors.New("temporary network failure")
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	require.NoError(t, RevokeStoredTossBillingKey(context.Background(), keyID))
	require.Zero(t, remoteDeletes)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.True(t, sub.AutoRenew)
	var policy WalletAutoRecharge
	require.NoError(t, DB.Where("billing_key_id = ?", keyID).First(&policy).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)
	require.NotNil(t, policy.ActiveKey)
}

func TestUserDisableAndDeleteDeactivateTossBilling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*User) error
	}{
		{name: "disable", mutate: func(user *User) error {
			user.Status = common.UserStatusDisabled
			return user.Update(false)
		}},
		{name: "soft delete", mutate: func(user *User) error { return DeleteUserById(user.Id) }},
		{name: "hard delete", mutate: func(user *User) error { return HardDeleteUserById(user.Id) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			user := &User{Id: 7, Username: "billing-user", Password: "password", Status: common.UserStatusEnabled, Group: "default", AffCode: "billing-user-aff"}
			require.NoError(t, DB.Create(user).Error)
			keyID := seedTossBillingSubscription(t, "billing_user_lifecycle", 0)
			activeKey := "user:7:threshold"
			require.NoError(t, DB.Create(&WalletAutoRecharge{
				Type:         WalletAutoRechargeTypeThreshold,
				TargetType:   TopUpTargetTypeUser,
				TargetId:     7,
				OwnerUserId:  7,
				BillingKeyId: keyID,
				Status:       WalletAutoRechargeStatusActive,
				ActiveKey:    &activeKey,
			}).Error)

			require.NoError(t, tc.mutate(user))
			var key UserBillingKey
			require.NoError(t, DB.First(&key, keyID).Error)
			require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
			var sub UserSubscription
			require.NoError(t, DB.First(&sub, 11).Error)
			require.False(t, sub.AutoRenew)
			var policy WalletAutoRecharge
			require.NoError(t, DB.Where("billing_key_id = ?", keyID).First(&policy).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
			require.Nil(t, policy.ActiveKey)
		})
	}
}

func TestOrganizationDisableOwnerChangeAndDeleteDeactivateWalletBilling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(int) error
	}{
		{name: "disable", mutate: func(id int) error {
			return UpdateOrganizationFieldsWithBillingLifecycle(id, map[string]interface{}{"status": OrganizationStatusDisabled})
		}},
		{name: "owner change", mutate: func(id int) error {
			return UpdateOrganizationFieldsWithBillingLifecycle(id, map[string]interface{}{"owner_user_id": 8})
		}},
		{name: "delete", mutate: DeleteOrganization},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			owner := &User{Id: 7, Username: "org-owner", Password: "password", Status: common.UserStatusEnabled, Group: "default", OrganizationId: 3, OrganizationRole: OrganizationRoleOwner, AffCode: "org-owner-aff"}
			require.NoError(t, DB.Create(owner).Error)
			require.NoError(t, DB.Create(&Organization{Id: 3, Name: "Billing Org", OwnerUserId: 7, Status: OrganizationStatusEnabled}).Error)
			keyID, err := StoreTossBillingKeyWithSecret(7, "cust_org", "billing_org_lifecycle", "현대", "433012******1234", "sk_org")
			require.NoError(t, err)
			activeKey := "organization:3:scheduled"
			require.NoError(t, DB.Create(&WalletAutoRecharge{
				Type:         WalletAutoRechargeTypeScheduled,
				TargetType:   TopUpTargetTypeOrganization,
				TargetId:     3,
				OwnerUserId:  7,
				BillingKeyId: keyID,
				Status:       WalletAutoRechargeStatusActive,
				ActiveKey:    &activeKey,
			}).Error)

			require.NoError(t, tc.mutate(3))
			var key UserBillingKey
			require.NoError(t, DB.First(&key, keyID).Error)
			require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
			var policy WalletAutoRecharge
			require.NoError(t, DB.Where("billing_key_id = ?", keyID).First(&policy).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
			require.Nil(t, policy.ActiveKey)
		})
	}
}

func TestParseTossRenewalTradeNo(t *testing.T) {
	subId, nextBillingTime, ok := ParseTossRenewalTradeNo("toss_sub_renew_11_1782840000")
	require.True(t, ok)
	require.Equal(t, 11, subId)
	require.Equal(t, int64(1782840000), nextBillingTime)

	subId, nextBillingTime, ok = ParseTossRenewalTradeNo("toss_sub_renew_11_1782840000_2")
	require.True(t, ok)
	require.Equal(t, 11, subId)
	require.Equal(t, int64(1782840000), nextBillingTime)

	for _, tradeNo := range []string{
		"",
		"toss_sub_reconcile_done",
		"toss_sub_renew_0_1782840000",
		"toss_sub_renew_11_0",
		"toss_sub_renew_11",
		"toss_sub_renew_11_next",
		"toss_sub_renew_11_1782840000_0",
		"toss_sub_renew_11_1782840000_extra",
	} {
		_, _, ok := ParseTossRenewalTradeNo(tradeNo)
		require.False(t, ok, "tradeNo %q should not parse as renewal", tradeNo)
	}
}

func TestPrepareTossRenewalOrderPersistsLogicalAttemptAssociation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_phase_a_association", 0)
	seedTossBillingPlan(t, "Phase A association", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	require.NotNil(t, order.RenewalSubscriptionId)
	require.NotNil(t, order.RenewalBillingTime)
	require.NotNil(t, order.RenewalAttempt)
	require.Equal(t, sub.Id, *order.RenewalSubscriptionId)
	require.Equal(t, sub.NextBillingTime, *order.RenewalBillingTime)
	require.Equal(t, sub.BillingFailCount, *order.RenewalAttempt)
	require.Equal(t, tossRecurringOrderIDVersionOpaque, order.RenewalOrderIdVersion)
	require.True(t, validTossRenewalOpaqueOrderID(order.TradeNo))
	require.NotEmpty(t, order.RenewalCreationToken)
}

func TestPrepareTossRenewalOrderBackfillsExactLegacyLogicalAttempt(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_phase_a_legacy_backfill", 0)
	seedTossBillingPlan(t, "Phase A legacy backfill", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	legacy := &SubscriptionOrder{
		UserId: sub.UserId, PlanId: sub.PlanId, Money: 1, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: TossPlanKRW(1), ProviderCurrency: "KRW",
		BillingKeyId: keyID, RenewalEndTime: sub.EndTime, CreateTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(legacy).Error)

	winner, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	require.Equal(t, legacy.Id, winner.Id)
	require.NotNil(t, winner.RenewalSubscriptionId)
	require.NotNil(t, winner.RenewalBillingTime)
	require.NotNil(t, winner.RenewalAttempt)
	require.Equal(t, tossRecurringOrderIDVersionLegacy, winner.RenewalOrderIdVersion)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where(
		"renewal_subscription_id = ? AND renewal_billing_time = ? AND renewal_attempt = ?",
		sub.Id, sub.NextBillingTime, sub.BillingFailCount,
	).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestProcessTossRenewalHaltsOnLegacyAndAssociatedDuplicate(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_phase_a_duplicate", 0)
	seedTossBillingPlan(t, "Phase A duplicate", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	legacyTradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, sub.BillingFailCount)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: sub.UserId, PlanId: sub.PlanId, TradeNo: legacyTradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: TossPlanKRW(1), ProviderCurrency: "KRW",
		BillingKeyId: keyID, RenewalEndTime: sub.EndTime, CreateTime: GetDBTimestamp(),
	}).Error)
	subID := sub.Id
	billingTime := sub.NextBillingTime
	attempt := sub.BillingFailCount
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: sub.UserId, PlanId: sub.PlanId, TradeNo: "toss_sub_renew_v2_duplicate_safe_id",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: TossPlanKRW(1), ProviderCurrency: "KRW",
		BillingKeyId: keyID, RenewalEndTime: sub.EndTime, CreateTime: GetDBTimestamp(),
		RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}).Error)

	postCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("identity conflict must not POST")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&sub, sub.Id).Error)
	require.False(t, sub.AutoRenew)
	require.Zero(t, sub.NextBillingTime)
	var pendingCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("status = ?", common.TopUpStatusPending).Count(&pendingCount).Error)
	require.EqualValues(t, 2, pendingCount, "conflicting evidence must remain available for reconciliation")
}

func TestOpaqueRenewalWithoutAssociationFailsClosedWithoutBackfill(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	identity := tossRenewalAttemptIdentity{
		SubscriptionID: 91,
		BillingTime:    1782840000,
		Attempt:        0,
	}
	order := &SubscriptionOrder{
		TradeNo:               tossRenewalTradeNo(identity.SubscriptionID, identity.BillingTime, identity.Attempt),
		PaymentMethod:         PaymentMethodToss,
		PaymentProvider:       PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}

	_, associated, err := subscriptionOrderRenewalIdentity(order)
	require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	require.False(t, associated)
	_, _, _, isRenewal, err := ResolveTossRenewalOrderIdentity(order)
	require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	require.False(t, isRenewal)
	require.NoError(t, DB.Create(order).Error)

	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := findTossRenewalOrderForIdentityTx(tx, identity)
		return err
	}), ErrTossRenewalIdentityConflict)

	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Nil(t, persisted.RenewalSubscriptionId)
	require.Nil(t, persisted.RenewalBillingTime)
	require.Nil(t, persisted.RenewalAttempt)
	require.Equal(t, tossRecurringOrderIDVersionOpaque, persisted.RenewalOrderIdVersion)
}

func TestEveryNonzeroRenewalProtocolRequiresCompleteAssociation(t *testing.T) {
	for _, version := range []int{
		tossRecurringOrderIDVersionLegacy,
		tossRecurringOrderIDVersionOpaque,
		tossRecurringOrderIDVersionOpaque + 17,
	} {
		order := &SubscriptionOrder{
			TradeNo:               "toss_sub_renew_91_1782840000",
			PaymentMethod:         PaymentMethodToss,
			PaymentProvider:       PaymentProviderToss,
			RenewalOrderIdVersion: version,
		}
		_, _, err := subscriptionOrderRenewalIdentity(order)
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict, "version=%d", version)
		_, _, _, isRenewal, err := ResolveTossRenewalOrderIdentity(order)
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict, "version=%d", version)
		require.False(t, isRenewal)
	}
}

func TestProcessTossRenewalBlocksInvisibleIncompleteProtocolRow(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_incomplete_protocol", 0)
	seedTossBillingPlan(t, "Incomplete protocol", 1)

	unrelatedSubID := 999
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 7, PlanId: 3, TradeNo: "opaqueHiddenRenewalEvidence",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, BillingAttempted: true,
		RenewalSubscriptionId: &unrelatedSubID,
		// The other two tuple columns are deliberately missing.
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}).Error)

	postCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("incomplete protocol evidence must block POST")
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.Zero(t, postCalls)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.True(t, sub.AutoRenew, "unrelated global corruption must not disable a healthy subscription")
	require.Equal(t, 0, sub.BillingFailCount)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "no replacement renewal row may be created")
}

func TestMarkTossRenewalChargeAttemptBlocksProtocolCorruptionInsertedAfterPreparation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_final_incomplete_protocol", 0)
	seedTossBillingPlan(t, "Final incomplete protocol", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)

	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	token, claimed, attempted, err := ClaimTossRenewalChargeOrder(order.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.False(t, attempted)

	unrelatedSubID := sub.Id + 999
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: sub.UserId, PlanId: sub.PlanId, TradeNo: "opaqueInsertedBeforeFinalGate",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, BillingAttempted: true,
		RenewalSubscriptionId: &unrelatedSubID,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}).Error)

	err = MarkTossRenewalChargeAttempt(order.TradeNo, token, setting.TossBillingSecretKey)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.False(t, order.BillingAttempted)
	require.Empty(t, order.BillingAttemptCredential)
	require.NoError(t, DB.First(&sub, sub.Id).Error)
	require.True(t, sub.AutoRenew)
	require.Equal(t, 0, sub.BillingFailCount)
}

func TestPrepareTossRenewalOrderGlobalCorruptionDoesNotHaltHealthySubscription(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_prepare_global_corruption", 0)
	seedTossBillingPlan(t, "Prepare global corruption", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	originalNextBillingTime := sub.NextBillingTime

	unrelatedSubID := sub.Id + 999
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 99, PlanId: 99, TradeNo: "opaqueBeforeRenewalPrepare",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, BillingAttempted: true,
		RenewalSubscriptionId: &unrelatedSubID,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	}).Error)

	tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0)
	order, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
	require.Nil(t, order)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.NoError(t, DB.First(&sub, sub.Id).Error)
	require.True(t, sub.AutoRenew)
	require.Equal(t, "active", sub.Status)
	require.Equal(t, originalNextBillingTime, sub.NextBillingTime)
	require.Zero(t, sub.BillingFailCount)
	var ownCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("renewal_subscription_id = ?", sub.Id).Count(&ownCount).Error)
	require.Zero(t, ownCount)
}

func TestOpaqueRenewalAssociationCannotEnterInitialChargePaths(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	require.NoError(t, DB.Create(&User{
		Id: 91, Username: "opaque-renewal-owner", AffCode: "opaque-renewal-owner",
		Status: common.UserStatusEnabled,
	}).Error)
	plan := &SubscriptionPlan{
		Id: 91, Title: "Opaque renewal", PriceAmount: 1, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(plan).Error)
	subID := 991
	billingTime := time.Now().Unix()
	attempt := 0
	newOrder := func(status string) *SubscriptionOrder {
		return &SubscriptionOrder{
			UserId: 91, PlanId: plan.Id, Money: 1, TradeNo: "trn_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: status, BillingKeyId: 71,
			RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
			RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
		}
	}
	reservation := newOrder(common.TopUpStatusPending)
	require.Error(t, CreateTossSubscriptionOrderWithPurchaseReservation(reservation, plan))

	stored := newOrder(common.TopUpStatusPending)
	stored.BillingAttempted = true
	stored.BillingAttemptCredential = "opaque-attempt-credential"
	require.NoError(t, DB.Create(stored).Error)
	_, claimed, err := ClaimTossSubscriptionFirstChargeOrder(stored.TradeNo)
	require.Error(t, err)
	require.False(t, claimed)
	_, claimed, err = ClaimTossUnattemptedSubscriptionChargeOrder(stored.TradeNo)
	require.Error(t, err)
	require.False(t, claimed)
	_, err = GetClaimedTossSubscriptionFirstChargeAttemptSecret(stored.TradeNo, "claim-token")
	require.Error(t, err)
	require.ErrorIs(t, CompleteTossBillingOrder(stored.TradeNo, stored.BillingKeyId, "{}"), ErrSubscriptionOrderStatusInvalid)

	stored.Status = common.TopUpStatusSuccess
	sub := &UserSubscription{Id: subID, UserId: stored.UserId, PlanId: stored.PlanId, BillingKeyId: stored.BillingKeyId}
	require.Error(t, SetTossRenewalContractFromInitialOrder(sub, stored))
	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", stored.UserId, stored.PlanId).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestUnattemptedChargeClaimsSeparateOpaqueRenewalFromInitialPurchase(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	credential, err := EncryptProviderCredential("test_sk_unattempted_claim_identity")
	require.NoError(t, err)
	subID := 712
	billingTime := int64(1782840000)
	attempt := 0
	renewal := &SubscriptionOrder{
		TradeNo: "trn_ffffffffffffffffffffffffffffffffffffffff", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		BillingKeyId: 91, ProviderCredential: credential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
		RenewalSubscriptionId:        &subID,
		RenewalBillingTime:           &billingTime,
		RenewalAttempt:               &attempt,
		RenewalOrderIdVersion:        tossRecurringOrderIDVersionOpaque,
	}
	require.NoError(t, DB.Create(renewal).Error)
	_, claimed, err := ClaimTossUnattemptedSubscriptionChargeOrder(renewal.TradeNo)
	require.Error(t, err)
	require.False(t, claimed)
	token, claimed, err := ClaimTossUnattemptedRenewalChargeOrder(renewal.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, token)
	require.NoError(t, ReleaseTossSubscriptionBillingClaim(renewal.TradeNo, token))

	initial := &SubscriptionOrder{
		TradeNo: "initialPurchaseClaim712", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		BillingKeyId: 92, ProviderCredential: credential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
	}
	require.NoError(t, DB.Create(initial).Error)
	_, claimed, err = ClaimTossUnattemptedRenewalChargeOrder(initial.TradeNo)
	require.Error(t, err)
	require.False(t, claimed)
	token, claimed, err = ClaimTossUnattemptedSubscriptionChargeOrder(initial.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, token)
	require.NoError(t, ReleaseTossSubscriptionBillingClaim(initial.TradeNo, token))
}

func TestTossRenewalAssociationUniqueIndexAllowsNullPurchaseRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&[]SubscriptionOrder{
		{TradeNo: "ordinary_toss_purchase_a", PaymentProvider: PaymentProviderToss},
		{TradeNo: "ordinary_toss_purchase_b", PaymentProvider: PaymentProviderToss},
	}).Error)
	subID := 55
	billingTime := int64(1782840000)
	attempt := 0
	first := &SubscriptionOrder{
		TradeNo: "toss_sub_renew_55_1782840000", PaymentProvider: PaymentProviderToss,
		RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
	}
	require.NoError(t, DB.Create(first).Error)
	duplicate := *first
	duplicate.Id = 0
	duplicate.TradeNo = "opaque_duplicate_association_55"
	require.Error(t, DB.Create(&duplicate).Error)
}

func TestResolveTossRenewalEffectiveAttemptRequiresContiguousTerminalChain(t *testing.T) {
	const (
		subID       = 77
		billingTime = int64(1782840000)
	)
	makeOrder := func(attempt int, status string) SubscriptionOrder {
		return SubscriptionOrder{
			TradeNo:       tossRenewalTradeNo(subID, billingTime, attempt),
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: status,
		}
	}
	resolve := func(t *testing.T, failCount int) (int, error) {
		t.Helper()
		var attempt int
		err := DB.Transaction(func(tx *gorm.DB) error {
			var err error
			attempt, err = resolveTossRenewalEffectiveAttemptTx(tx, subID, billingTime, failCount)
			return err
		})
		return attempt, err
	}
	setup := func(t *testing.T) {
		t.Helper()
		setupTossBillingModelTestDB(t)
		require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	}

	t.Run("pre-provider failures keep HEAD attempt zero", func(t *testing.T) {
		setup(t)
		attempt, err := resolve(t, 2)
		require.NoError(t, err)
		require.Zero(t, attempt)
	})

	t.Run("failed base unlocks attempt one", func(t *testing.T) {
		setup(t)
		base := makeOrder(0, common.TopUpStatusFailed)
		require.NoError(t, DB.Create(&base).Error)
		attempt, err := resolve(t, 1)
		require.NoError(t, err)
		require.Equal(t, 1, attempt)
	})

	t.Run("pending retry is recovered", func(t *testing.T) {
		setup(t)
		rows := []SubscriptionOrder{
			makeOrder(0, common.TopUpStatusFailed),
			makeOrder(1, common.TopUpStatusPending),
		}
		require.NoError(t, DB.Create(&rows).Error)
		attempt, err := resolve(t, 1)
		require.NoError(t, err)
		require.Equal(t, 1, attempt)
	})

	t.Run("higher attempt without predecessors conflicts", func(t *testing.T) {
		setup(t)
		row := makeOrder(2, common.TopUpStatusPending)
		require.NoError(t, DB.Create(&row).Error)
		_, err := resolve(t, 2)
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	})

	t.Run("live lower attempt beside higher attempt conflicts", func(t *testing.T) {
		setup(t)
		rows := []SubscriptionOrder{
			makeOrder(0, common.TopUpStatusPending),
			makeOrder(1, common.TopUpStatusPending),
		}
		require.NoError(t, DB.Create(&rows).Error)
		_, err := resolve(t, 1)
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	})

	t.Run("terminal row without committed failure conflicts", func(t *testing.T) {
		setup(t)
		base := makeOrder(0, common.TopUpStatusFailed)
		require.NoError(t, DB.Create(&base).Error)
		_, err := resolve(t, 0)
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)
	})
}

func TestProcessTossRenewalUsesHeadAttemptWhenFailCountHasNoPhysicalRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_fail_count_without_order", 2)
	seedTossBillingPlan(t, "Fail count without order", 1)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	postCalls := 0
	postedOrderID := ""
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(_ context.Context, _, _, _, orderID, _ string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		postedOrderID = orderID
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount, BalanceAmount: amount,
			PaymentKey: "pay_fail_count_without_order", ProviderPayload: `{"status":"DONE"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, TossBillingMaxFails))
	require.Equal(t, 1, postCalls)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, order.TradeNo, postedOrderID)
	require.True(t, validTossRenewalOpaqueOrderID(postedOrderID))
	require.NotNil(t, order.RenewalAttempt)
	require.Zero(t, *order.RenewalAttempt)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
}

func TestMarkTossRenewalChargeAttemptRechecksContiguousChainAfterClaim(t *testing.T) {
	mutations := []struct {
		name       string
		baseStatus string
		deleteBase bool
	}{
		{name: "base deleted", deleteBase: true},
		{name: "base becomes pending", baseStatus: common.TopUpStatusPending},
		{name: "base becomes successful", baseStatus: common.TopUpStatusSuccess},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			seedTossBillingSubscription(t, "billing_final_chain_recheck", 0)
			seedTossBillingPlan(t, "Final chain recheck", 1)
			originalUnitPrice := setting.TossUnitPrice
			setting.TossUnitPrice = 1000
			t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

			var sub UserSubscription
			require.NoError(t, DB.First(&sub, 11).Error)
			baseTradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0)
			base, err := PrepareTossRenewalOrder(sub.Id, baseTradeNo, 1, 1000)
			require.NoError(t, err)
			require.NotNil(t, base)
			baseToken, claimed, attempted, err := ClaimTossRenewalChargeOrder(base.TradeNo)
			require.NoError(t, err)
			require.True(t, claimed)
			require.False(t, attempted)
			require.NoError(t, MarkTossRenewalChargeAttempt(
				base.TradeNo, baseToken, setting.TossBillingSecretKey,
			))
			disabled, err := failTossRenewalAttempt(sub.Id, base.TradeNo, baseToken, 3)
			require.NoError(t, err)
			require.False(t, disabled)

			require.NoError(t, DB.First(&sub, sub.Id).Error)
			require.Equal(t, 1, sub.BillingFailCount)
			suffixTradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 1)
			suffix, err := PrepareTossRenewalOrder(sub.Id, suffixTradeNo, 1, 1000)
			require.NoError(t, err)
			require.NotNil(t, suffix)
			suffixToken, claimed, attempted, err := ClaimTossRenewalChargeOrder(suffix.TradeNo)
			require.NoError(t, err)
			require.True(t, claimed)
			require.False(t, attempted)

			if mutation.deleteBase {
				require.NoError(t, DB.Delete(&SubscriptionOrder{}, base.Id).Error)
			} else {
				require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", base.Id).
					Update("status", mutation.baseStatus).Error)
			}

			err = MarkTossRenewalChargeAttempt(
				suffix.TradeNo, suffixToken, setting.TossBillingSecretKey,
			)
			require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)

			require.NoError(t, DB.First(&suffix, suffix.Id).Error)
			require.Equal(t, common.TopUpStatusPending, suffix.Status)
			require.False(t, suffix.BillingAttempted)
			require.Empty(t, suffix.BillingAttemptCredential)
		})
	}
}

func TestClaimTossSubscriptionBillingOrderHasSingleRecoverableOwner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss_sub_claim_once",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	firstToken, claimed, err := ClaimTossSubscriptionBillingOrder("toss_sub_claim_once")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, firstToken)

	_, claimed, err = ClaimTossSubscriptionBillingOrder("toss_sub_claim_once")
	require.NoError(t, err)
	require.False(t, claimed)
	expired, err := ExpireStaleTossPendingSubscriptionOrders(common.GetTimestamp() - 60)
	require.NoError(t, err)
	require.Zero(t, expired, "cleanup must not expire a recently claimed provider operation")
	require.NoError(t, ReleaseTossSubscriptionBillingClaim("toss_sub_claim_once", "not-the-owner"))
	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_claim_once").First(&order).Error)
	require.Equal(t, firstToken, order.BillingClaimToken)

	require.NoError(t, ReleaseTossSubscriptionBillingClaim("toss_sub_claim_once", firstToken))
	secondToken, claimed, err := ClaimTossSubscriptionBillingOrder("toss_sub_claim_once")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, firstToken, secondToken)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", "toss_sub_claim_once").
		Updates(map[string]interface{}{
			"billing_claim_token": "stale-owner",
			"billing_claim_time":  common.GetTimestamp() - tossSubscriptionBillingClaimTTLSeconds - 1,
		}).Error)
	recoveredToken, claimed, err := ClaimTossSubscriptionBillingOrder("toss_sub_claim_once")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, "stale-owner", recoveredToken)
	require.ErrorIs(t, AttachTossBillingKeyToClaimedOrder("toss_sub_claim_once", 41, secondToken), ErrTossBillingClaimLost)
	require.NoError(t, AttachTossBillingKeyToClaimedOrder("toss_sub_claim_once", 42, recoveredToken))
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_claim_once").First(&order).Error)
	require.Equal(t, 42, order.BillingKeyId)
}

func TestClaimTossSubscriptionBillingOrderRejectsAttachedKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss_sub_claim_attached",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		BillingKeyId:    42,
	}).Error)
	_, claimed, err := ClaimTossSubscriptionBillingOrder("toss_sub_claim_attached")
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestTossSubscriptionBillingIssueSnapshotIsRecoverableAndClaimGuarded(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	credential, err := EncryptProviderCredential("test_sk_issue_original")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:                  "toss_sub_issue_snapshot",
		PaymentMethod:            PaymentMethodToss,
		PaymentProvider:          PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		CreateTime:               100,
		ProviderCredential:       credential,
		BillingAttempted:         true,
		BillingAttemptCredential: "renewal-attempt-must-survive",
	}).Error)

	firstToken, claimed, err := ClaimTossSubscriptionBillingIssue("toss_sub_issue_snapshot", "one_time_auth", "cust_issue")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, firstToken)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_issue_snapshot").First(&order).Error)
	require.NotEmpty(t, order.BillingIssueAuthKey)
	require.NotContains(t, order.BillingIssueAuthKey, "one_time_auth")
	require.Equal(t, common.GenerateHMAC("one_time_auth"), order.BillingIssueAuthKeyHash)
	require.Equal(t, "cust_issue", order.BillingIssueCustomerKey)
	require.True(t, order.BillingAttempted, "issue snapshot must not overwrite the separate renewal marker")
	require.Equal(t, "renewal-attempt-must-survive", order.BillingAttemptCredential)

	authKey, customerKey, secretKey, err := GetClaimedTossSubscriptionBillingIssue("toss_sub_issue_snapshot", firstToken)
	require.NoError(t, err)
	require.Equal(t, "one_time_auth", authKey)
	require.Equal(t, "cust_issue", customerKey)
	require.Equal(t, "test_sk_issue_original", secretKey)

	_, claimed, err = ClaimTossSubscriptionBillingIssue("toss_sub_issue_snapshot", "one_time_auth", "cust_issue")
	require.NoError(t, err)
	require.False(t, claimed, "a live claim must have one owner across nodes")
	_, claimed, err = ClaimTossSubscriptionBillingIssue("toss_sub_issue_snapshot", "different_auth", "cust_issue")
	require.ErrorIs(t, err, ErrTossBillingIssueConflict)
	require.False(t, claimed)

	// Generic stale cleanup must not destroy the only recoverable authKey.
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", "toss_sub_issue_snapshot").
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	expired, err := ExpireStaleTossPendingSubscriptionOrders(200)
	require.NoError(t, err)
	require.Zero(t, expired)

	secondToken, claimed, err := ClaimStoredTossSubscriptionBillingIssue("toss_sub_issue_snapshot")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEqual(t, firstToken, secondToken)
	require.ErrorIs(t, MarkTossSubscriptionBillingIssueAttempt("toss_sub_issue_snapshot", firstToken), ErrTossBillingClaimLost)
	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt("toss_sub_issue_snapshot", secondToken))

	// Rotating the active provider key must not affect issue recovery.
	setting.TossBillingSecretKey = "test_sk_issue_rotated_active"
	authKey, customerKey, secretKey, err = GetClaimedTossSubscriptionBillingIssue("toss_sub_issue_snapshot", secondToken)
	require.NoError(t, err)
	require.Equal(t, "one_time_auth", authKey)
	require.Equal(t, "cust_issue", customerKey)
	require.Equal(t, "test_sk_issue_original", secretKey)

	require.ErrorIs(t, AttachTossBillingKeyToClaimedOrder("toss_sub_issue_snapshot", 41, firstToken), ErrTossBillingClaimLost)
	require.NoError(t, AttachTossBillingKeyToClaimedOrder("toss_sub_issue_snapshot", 42, secondToken))
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_issue_snapshot").First(&order).Error)
	require.Equal(t, 42, order.BillingKeyId)
	require.Empty(t, order.BillingIssueAuthKey)
	require.Empty(t, order.BillingIssueAuthKeyHash)
	require.Empty(t, order.BillingIssueCustomerKey)
	require.False(t, order.BillingIssueAttempted)
	require.True(t, order.BillingAttempted)
	require.Equal(t, "renewal-attempt-must-survive", order.BillingAttemptCredential)

	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss_sub_issue_too_long",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)
	_, _, err = ClaimTossSubscriptionBillingIssue("toss_sub_issue_too_long", strings.Repeat("a", TossBillingIssueAuthKeyMaxBytes+1), "cust_issue")
	require.ErrorIs(t, err, ErrTossBillingIssueSnapshotInvalid)
}

func TestClaimTossSubscriptionBillingIssuePreservesProviderAttemptEvidence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss_sub_issue_attempt_evidence",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	firstToken, claimed, err := ClaimTossSubscriptionBillingIssue(
		"toss_sub_issue_attempt_evidence",
		"one_time_auth",
		"cust_attempt_evidence",
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt(
		"toss_sub_issue_attempt_evidence",
		firstToken,
	))
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", "toss_sub_issue_attempt_evidence").
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)

	secondToken, claimed, err := ClaimTossSubscriptionBillingIssue(
		"toss_sub_issue_attempt_evidence",
		"one_time_auth",
		"cust_attempt_evidence",
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, secondToken)
	require.NotEqual(t, firstToken, secondToken)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_issue_attempt_evidence").First(&order).Error)
	require.True(t, order.BillingIssueAttempted,
		"a repeated success callback must not erase evidence of a prior provider call")
}

func TestClaimTossSubscriptionBillingIssueHasSingleMultiNodeWinner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss_sub_issue_multi_node",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	// Keep SQLite on one physical in-memory connection. The two goroutines still
	// execute independent claim operations, matching two application nodes that
	// serialize through the database CAS.
	sqlDB.SetMaxOpenConns(1)

	type claimResult struct {
		token   string
		claimed bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, claimed, claimErr := ClaimTossSubscriptionBillingIssue("toss_sub_issue_multi_node", "one_time_auth", "cust_multi_node")
			results <- claimResult{token: token, claimed: claimed, err: claimErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.claimed {
			winners++
			require.NotEmpty(t, result.token)
		}
	}
	require.Equal(t, 1, winners)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_issue_multi_node").First(&order).Error)
	require.NotEmpty(t, order.BillingClaimToken)
	require.NotEmpty(t, order.BillingIssueAuthKey)
	require.Equal(t, common.GenerateHMAC("one_time_auth"), order.BillingIssueAuthKeyHash)
}

func TestExpireClaimedTossSubscriptionBillingIssueClearsSensitiveSnapshot(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	credential, err := EncryptProviderCredential("test_sk_issue_expire")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:               "toss_sub_issue_expire",
		PaymentMethod:         PaymentMethodToss,
		PaymentProvider:       PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		ProviderCredential:    credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("test_ck_issue_expire"),
	}).Error)
	token, claimed, err := ClaimTossSubscriptionBillingIssue("toss_sub_issue_expire", "auth_to_clear", "cust_expire")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt("toss_sub_issue_expire", token))
	require.NoError(t, ExpireClaimedTossSubscriptionBillingIssue("toss_sub_issue_expire", token))

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_issue_expire").First(&order).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.NotZero(t, order.CompleteTime)
	require.Empty(t, order.BillingClaimToken)
	require.Zero(t, order.BillingClaimTime)
	require.Empty(t, order.BillingIssueAuthKey)
	require.Empty(t, order.BillingIssueAuthKeyHash)
	require.Empty(t, order.BillingIssueCustomerKey)
	require.False(t, order.BillingIssueAttempted)
	require.Empty(t, order.ProviderCredential)
	require.Empty(t, order.ProviderClientKeyHash)
}

func TestProcessTossRenewalRejectsBelowTossCardMinimumWithoutCharge(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_low_amount", 0)
	plan := &SubscriptionPlan{
		Id: 3, Title: "Tiny", PriceAmount: 0.05, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	require.NoError(t, DB.Create(plan).Error)
	var seededSub UserSubscription
	require.NoError(t, DB.First(&seededSub, 11).Error)
	legacyOrder := &SubscriptionOrder{
		UserId: seededSub.UserId, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo: "toss_sub_legacy_below_minimum", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusSuccess,
		ProviderAmount: 50, ProviderCurrency: "KRW", BillingKeyId: seededSub.BillingKeyId,
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(legacyOrder, plan))
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", seededSub.Id).
		Update("toss_renewal_contract_snapshot", legacyOrder.PlanSnapshot).Error)

	called := false
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		called = true
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.False(t, called, "below-minimum Toss card amount should not call remote billing charge")

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.False(t, sub.AutoRenew)
}

func TestProcessTossRenewalCapsTossOrderName(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_long_order_name", 0)
	seedTossBillingPlan(t, strings.Repeat("가", 120), 1)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	var capturedOrderName string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedOrderName = orderName
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.LessOrEqual(t, len([]rune(capturedOrderName)), 100)
	require.True(t, strings.HasSuffix(capturedOrderName, " 구독 갱신"))
}

func TestProcessTossRenewalUsesStoredBillingSecretBeforeCurrentCredential(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 7, Username: "stored-secret-user", Password: "password", Status: common.UserStatusEnabled, Group: "default", AffCode: "stored-secret-aff"}).Error)
	keyId, err := StoreTossBillingKeyWithProviderSnapshot(
		7, "cust_test", "billing_key_secret_snapshot", "현대", "433012******1234", "sk_old_billing",
		TossBillingClientKeyFingerprint(setting.TossBillingClientKey),
	)
	require.NoError(t, err)
	sub := &UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:         time.Now().Add(24 * time.Hour).Unix(),
		AutoRenew:       true,
		BillingKeyId:    keyId,
		NextBillingTime: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(sub).Error)
	seedTossBillingPlan(t, "Secret Snapshot", 1)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	var capturedSecrets []string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedSecrets = append(capturedSecrets, secretKey)
		if secretKey == setting.TossBillingSecretKey {
			return nil, errors.New("status=401 INVALID_API_KEY")
		}
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, []string{"sk_old_billing"}, capturedSecrets)
}

func TestProcessTossRenewalPromotesFallbackCredentialBeforeFinalGateStops(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 7, Username: "credential-promotion-user", Password: "password", Status: common.UserStatusEnabled, Group: "default", AffCode: "credential-promotion-aff"}).Error)
	currentSecret := setting.TossBillingSecretKey
	fallbackSecret := "sk_stored_credential_promotion"
	keyID, err := StoreTossBillingKeyWithProviderSnapshot(
		7,
		"cust_credential_promotion",
		"billing_key_credential_promotion",
		"현대",
		"433012******1234",
		fallbackSecret,
		TossBillingClientKeyFingerprint(setting.TossBillingClientKey),
	)
	require.NoError(t, err)
	sub := &UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:         time.Now().Add(24 * time.Hour).Unix(),
		AutoRenew:       true,
		BillingKeyId:    keyID,
		NextBillingTime: time.Now().Unix(),
	}
	require.NoError(t, DB.Create(sub).Error)
	seedTossBillingPlan(t, "Credential Promotion", 1)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	var postSecrets []string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postSecrets = append(postSecrets, secretKey)
		if secretKey == fallbackSecret {
			// Simulate an operator kill switch racing immediately after Toss
			// definitively rejects the stored credential. The next namespace must
			// already be durable even though its final POST gate will now stop.
			setting.TossBillingEnabled = false
			return nil, errors.New("status=401 code=INVALID_API_KEY")
		}
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_credential_promotion",
			ProviderStatus:  "DONE",
			ProviderPayload: `{"paymentKey":"pay_credential_promotion","status":"DONE","method":"카드","currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.ErrorIs(t, ProcessTossRenewal(context.Background(), sub.Id, 3), ErrTossBillingOperationallyDisabled)
	require.Equal(t, []string{fallbackSecret}, postSecrets)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.False(t, order.BillingAttempted)
	require.Zero(t, order.BillingAttemptTime)
	recordedSecret, err := DecryptProviderCredential(order.BillingAttemptCredential)
	require.NoError(t, err)
	require.Equal(t, currentSecret, recordedSecret)

	setting.TossBillingEnabled = true
	var lookupSecrets []string
	previousLookup := tossRenewalPaymentLookup
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupSecrets = append(lookupSecrets, secretKey)
		return nil, ErrTossBillingPaymentNotFound
	})
	t.Cleanup(func() { SetTossRenewalPaymentLookup(previousLookup) })

	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, 3))
	require.Empty(t, lookupSecrets)
	require.Equal(t, []string{fallbackSecret, currentSecret}, postSecrets)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
}

func TestProcessTossRenewalStoresTossPaymentPayloadOnAuditOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_payload", 0)
	seedTossBillingPlan(t, "Payload Plan", 1)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	var capturedOrderId string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedOrderId = orderId
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_renewal_payload",
			ProviderPayload: `{"paymentKey":"pay_renewal_payload","status":"DONE","totalAmount":1000,"currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", capturedOrderId).First(&order).Error)
	require.Equal(t, PaymentProviderToss, order.PaymentProvider)
	require.Contains(t, order.ProviderPayload, "pay_renewal_payload")
}

func TestReconcileTossRenewalWithoutNewChargeClosesAuthoritativeNotFound(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	keyID := seedTossBillingSubscription(t, "billing_key_stale_get_only", 0)
	credential, err := EncryptProviderCredential(setting.TossBillingSecretKey)
	require.NoError(t, err)
	order := SubscriptionOrder{
		UserId:                   7,
		TradeNo:                  "toss_sub_renew_11_1782840000",
		PaymentMethod:            PaymentMethodToss,
		PaymentProvider:          PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		BillingKeyId:             keyID,
		BillingAttempted:         true,
		BillingAttemptTime:       common.GetTimestamp() - TossBillingOperationalGraceSeconds - 1,
		BillingAttemptCredential: credential,
		ProviderAmount:           1000,
	}
	require.NoError(t, DB.Create(&order).Error)

	previousLookup := tossRenewalPaymentLookup
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})
	t.Cleanup(func() { SetTossRenewalPaymentLookup(previousLookup) })

	require.NoError(t, reconcileTossRenewalWithoutNewCharge(context.Background(), &order))
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Empty(t, order.BillingClaimToken)
}

func TestProcessTossRenewalUsesSnapshotWhenPlanDeletedAfterChargeStarts(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_fulfillment_retry", 0)
	seedTossBillingPlan(t, "Fulfillment Retry", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	chargeCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		if chargeCalls == 1 {
			require.NoError(t, DB.Delete(&SubscriptionPlan{}, 3).Error)
			InvalidateSubscriptionPlanCache(3)
		}
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_renewal_fulfillment",
			ProviderPayload: `{"paymentKey":"pay_renewal_fulfillment","status":"DONE","totalAmount":1000}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, 1, chargeCalls)
	order := requireTossRenewalOrderByAttemptForTest(t, 11, 0)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.NotEmpty(t, order.PlanSnapshot)
	require.Contains(t, order.ProviderPayload, "pay_renewal_fulfillment")
	var eventCount int64
	require.NoError(t, DB.Model(&TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ?", order.TradeNo, TossPaymentEventTypeFulfillment).
		Count(&eventCount).Error)
	require.Zero(t, eventCount)
}

func TestProcessTossRenewalCreatesPendingSnapshotBeforeRemoteCharge(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_pre_snapshot", 0)
	seedTossBillingPlan(t, "Snapshot Renewal", 10)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	var capturedAmount int64
	capturedOrderID := ""
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedAmount = amount
		capturedOrderID = orderId
		var order SubscriptionOrder
		require.NoError(t, DB.Where("trade_no = ?", orderId).First(&order).Error)
		require.Equal(t, common.TopUpStatusPending, order.Status)
		require.Equal(t, int64(10000), order.ProviderAmount)
		require.Equal(t, "KRW", order.ProviderCurrency)
		require.Equal(t, keyId, order.BillingKeyId)
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_renewal_pre_snapshot",
			ProviderPayload: `{"paymentKey":"pay_renewal_pre_snapshot","status":"DONE","totalAmount":10000,"currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, int64(10000), capturedAmount)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", capturedOrderID).First(&order).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.Equal(t, int64(10000), order.ProviderAmount)
	require.Equal(t, "KRW", order.ProviderCurrency)
	require.Equal(t, keyId, order.BillingKeyId)
}

func TestProcessTossRenewalUsesPendingOrderSnapshotAfterUnitPriceChange(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_existing_snapshot", 0)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })
	seedTossBillingPlan(t, "Existing Snapshot Renewal", 10)

	var seededSub UserSubscription
	require.NoError(t, DB.First(&seededSub, 11).Error)
	tradeNo := "toss_sub_renew_11_" + fmt.Sprint(seededSub.NextBillingTime)
	credential, err := EncryptProviderCredential("sk_existing_snapshot")
	require.NoError(t, err)
	require.NoError(t, (&SubscriptionOrder{
		UserId:                       7,
		PlanId:                       3,
		Money:                        10,
		TradeNo:                      tradeNo,
		PaymentMethod:                PaymentMethodToss,
		PaymentProvider:              PaymentProviderToss,
		Status:                       common.TopUpStatusPending,
		CreateTime:                   common.GetTimestamp(),
		BillingKeyId:                 keyId,
		ProviderAmount:               15000,
		ProviderCurrency:             "KRW",
		ProviderCredential:           credential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
	}).Insert())

	setting.TossUnitPrice = 5

	var capturedAmount int64
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedAmount = amount
		require.Equal(t, tradeNo, orderId)
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_existing_snapshot",
			ProviderPayload: `{"paymentKey":"pay_existing_snapshot","status":"DONE","totalAmount":15000,"currency":"KRW"}`,
		}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, int64(15000), capturedAmount)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&order).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.Equal(t, int64(15000), order.ProviderAmount)
}

func TestProcessTossRenewalDoesNotCountPendingChargeAsFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_charge_pending", 0)
	seedTossBillingPlan(t, "Pending Renewal", 10)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingChargePending
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 0, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)

	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.Equal(t, int64(10000), order.ProviderAmount)
}

func TestProcessTossRenewalInvalidSuccessfulResponseRequiresReconciliationWithoutNewAttempt(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_invalid_success", 0)
	seedTossBillingPlan(t, "Invalid Successful Renewal", 10)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	chargeCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargeCalls++
		return &TossBillingChargeResult{
			Done:            false,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_invalid_success",
			ProviderPayload: `{"paymentKey":"pay_invalid_success","status":"DONE","currency":"USD"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	err := ProcessTossRenewal(context.Background(), 11, 3)
	require.ErrorContains(t, err, "reconciliation required")
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.True(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingClaimToken)
	var event TossPaymentEvent
	require.NoError(t, DB.Where("order_id = ? AND event_type = ?", order.TradeNo, TossPaymentEventTypeFinancialMismatch).First(&event).Error)
	require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)

	// The live claim prevents a second node from advancing to a new order/POST.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, 1, chargeCalls)
}

func TestProcessTossRenewalDoesNotCloseAttemptOnNetworkTimeout(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_network_timeout", 0)
	seedTossBillingPlan(t, "Network Timeout Renewal", 10)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Zero(t, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	order := requireTossRenewalOrderByAttemptForTest(t, sub.Id, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
}

func TestProcessTossRenewalAmbiguousAttemptIsClaimedAndRetriesOnlyRecordedSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_claimed_ambiguous", 0)
	seedTossBillingPlan(t, "Claimed Ambiguous Renewal", 1)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	firstSecret := setting.TossBillingSecretKey
	var postSecrets []string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postSecrets = append(postSecrets, secretKey)
		if len(postSecrets) == 1 {
			return nil, context.DeadlineExceeded
		}
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_claimed_ambiguous",
			ProviderPayload: `{"status":"DONE"}`,
		}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	order := requireTossRenewalOrderByAttemptForTest(t, 11, 0)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.True(t, order.BillingAttempted)
	require.NotEmpty(t, order.BillingClaimToken)
	attemptSecret, err := DecryptProviderCredential(order.BillingAttemptCredential)
	require.NoError(t, err)
	require.Equal(t, firstSecret, attemptSecret)

	// A second node cannot POST while the first ambiguous-attempt lease is live.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, []string{firstSecret}, postSecrets)

	// Recover the stale claim after rotating the configured key. GET must use
	// the recorded secret, and a 404 may repeat only that same idempotency
	// namespace—not the newly configured secret.
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_time", common.GetTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	setting.TossBillingSecretKey = "test_sk_rotated_after_ambiguous"
	lookupCalls := 0
	previousLookup := tossRenewalPaymentLookup
	SetTossRenewalPaymentLookup(func(ctx context.Context, secretKey, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, firstSecret, secretKey)
		require.Equal(t, order.TradeNo, orderID)
		return nil, ErrTossBillingPaymentNotFound
	})
	t.Cleanup(func() { SetTossRenewalPaymentLookup(previousLookup) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, []string{firstSecret, firstSecret}, postSecrets)
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
}

func TestProcessTossRenewalDefinitiveFailureClosesAttemptAndUsesNewTradeNo(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_declined_retry", 0)
	seedTossBillingPlan(t, "Declined Renewal", 10)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	var orderIDs []string
	decline := true
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		orderIDs = append(orderIDs, orderID)
		if decline {
			return nil, errors.New("provider card declined")
		}
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 1, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
	firstTradeNo := orderIDs[0]
	var firstOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", firstTradeNo).First(&firstOrder).Error)
	require.Equal(t, common.TopUpStatusFailed, firstOrder.Status)

	decline = false
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Len(t, orderIDs, 2)
	require.NotEqual(t, firstTradeNo, orderIDs[1])
	require.True(t, validTossRenewalOpaqueOrderID(orderIDs[0]))
	require.True(t, validTossRenewalOpaqueOrderID(orderIDs[1]))
	var retryOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", orderIDs[1]).First(&retryOrder).Error)
	require.Equal(t, common.TopUpStatusSuccess, retryOrder.Status)
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 0, sub.BillingFailCount)
}

func TestProcessTossRenewalRetryPreservesCycleTermsAfterPlanEdit(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_immutable_cycle_retry", 0)
	seedTossBillingPlan(t, "Original Cycle Terms", 10)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	decline := true
	var chargedAmounts []int64
	var orderNames []string
	var orderIDs []string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargedAmounts = append(chargedAmounts, amount)
		orderNames = append(orderNames, orderName)
		orderIDs = append(orderIDs, orderID)
		if decline {
			return nil, errors.New("provider card declined")
		}
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 1, sub.BillingFailCount)
	originalEnd := sub.EndTime

	// Editing every financially relevant field after purchase must affect only
	// new purchases, never retries or later cycles of this existing contract.
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", sub.PlanId).Updates(map[string]interface{}{
		"title":                      "Changed Retry Terms",
		"price_amount":               40,
		"duration_unit":              SubscriptionDurationCustom,
		"duration_value":             1,
		"custom_seconds":             60,
		"total_amount":               9999,
		"upgrade_group":              "changed-during-retry",
		"quota_reset_period":         SubscriptionResetCustom,
		"quota_reset_custom_seconds": 30,
	}).Error)
	InvalidateSubscriptionPlanCache(sub.PlanId)
	setting.TossUnitPrice = 2200

	// A second decline creates another orderId, but still uses attempt zero's
	// snapshot rather than the edited plan.
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 2, sub.BillingFailCount)
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", sub.PlanId).Updates(map[string]interface{}{
		"title":          "Changed Again Before Third Attempt",
		"price_amount":   60,
		"custom_seconds": 120,
		"total_amount":   12345,
	}).Error)
	InvalidateSubscriptionPlanCache(sub.PlanId)
	setting.TossUnitPrice = 3000

	decline = false
	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, []int64{10000, 10000, 10000}, chargedAmounts)
	require.Equal(t, []string{
		TossSubscriptionOrderName("Original Cycle Terms", true),
		TossSubscriptionOrderName("Original Cycle Terms", true),
		TossSubscriptionOrderName("Original Cycle Terms", true),
	}, orderNames)

	firstTradeNo := orderIDs[0]
	secondTradeNo := orderIDs[1]
	retryTradeNo := orderIDs[2]
	var firstOrder, secondOrder, retryOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", firstTradeNo).First(&firstOrder).Error)
	require.NoError(t, DB.Where("trade_no = ?", secondTradeNo).First(&secondOrder).Error)
	require.NoError(t, DB.Where("trade_no = ?", retryTradeNo).First(&retryOrder).Error)
	require.Equal(t, common.TopUpStatusFailed, firstOrder.Status)
	require.Equal(t, common.TopUpStatusFailed, secondOrder.Status)
	require.Equal(t, common.TopUpStatusSuccess, retryOrder.Status)
	require.Equal(t, float64(10), retryOrder.Money)
	require.Equal(t, int64(10000), retryOrder.ProviderAmount)
	retryPlan, err := ResolveTossSubscriptionOrderPlan(&retryOrder)
	require.NoError(t, err)
	require.Equal(t, "Original Cycle Terms", retryPlan.Title)
	require.Equal(t, float64(10), retryPlan.PriceAmount)
	require.Equal(t, SubscriptionDurationMonth, retryPlan.DurationUnit)
	require.Equal(t, 1, retryPlan.DurationValue)
	require.Zero(t, retryPlan.TotalAmount)
	require.Empty(t, retryPlan.UpgradeGroup)

	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, time.Unix(originalEnd, 0).AddDate(0, 1, 0).Unix(), sub.EndTime)
	require.Zero(t, sub.AmountTotal)
	require.Empty(t, sub.UpgradeGroup)

	// Force the scheduler snapshot for the following cycle. The immutable
	// initial contract must still win after the edited plan has remained live
	// through a completed renewal.
	firstRenewedEnd := sub.EndTime
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).
		Update("next_billing_time", GetDBTimestamp()-60).Error)
	require.NoError(t, ProcessTossRenewal(context.Background(), sub.Id, 3))
	require.Equal(t, []int64{10000, 10000, 10000, 10000}, chargedAmounts)
	require.Equal(t, TossSubscriptionOrderName("Original Cycle Terms", true), orderNames[len(orderNames)-1])
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, time.Unix(firstRenewedEnd, 0).AddDate(0, 1, 0).Unix(), sub.EndTime)
	require.Zero(t, sub.AmountTotal)
	require.Empty(t, sub.UpgradeGroup)
}

func TestFailTossRenewalAttemptCountsTerminalOrderOnlyOnce(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_single_failure", 0)
	seedTossBillingPlan(t, "Single Failure", 10)
	tradeNo := tossRenewalTradeNo(11, time.Now().Unix(), 0)
	order, err := PrepareTossRenewalOrder(11, tradeNo, 10, TossPlanKRW(10))
	require.NoError(t, err)
	tradeNo = order.TradeNo
	claimToken, claimed, _, err := ClaimTossRenewalChargeOrder(tradeNo)
	require.NoError(t, err)
	require.True(t, claimed)

	disabled, err := failTossRenewalAttempt(11, tradeNo, claimToken, 3)
	require.NoError(t, err)
	require.False(t, disabled)
	disabled, err = failTossRenewalAttempt(11, tradeNo, claimToken, 3)
	require.NoError(t, err)
	require.False(t, disabled)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, 1, sub.BillingFailCount)
}

func TestMarkTossBillingFailureRevokesLocalKeyAtMax(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_max_fail", 2)

	disabled, err := MarkTossBillingFailure(11, 3)
	require.NoError(t, err)
	require.True(t, disabled)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}

func TestCancelTossAutoRenewRevokesRemoteBillingKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_cancel", 0)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	require.NoError(t, CancelTossAutoRenewForUser(context.Background(), 7))
	require.Equal(t, []string{"billing_key_cancel"}, revoked)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestCancelTossAutoRenewKeepsPendingRevocationWhenRemoteDeleteFails(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_cancel_retry", 0)

	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		return errors.New("temporary toss outage")
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	require.NoError(t, CancelTossAutoRenewForUser(context.Background(), 7))

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}

func TestRetryPendingTossBillingKeyRevocationsMarksRevokedAfterRemoteSuccess(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_pending", "현대", "433012******1234", setting.TossBillingSecretKey)
	require.NoError(t, err)
	require.NoError(t, MarkTossBillingKeyPendingRevocation(nil, keyId))
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyId).Update("revocation_retry_time", GetDBTimestamp()-1).Error)

	var pending UserBillingKey
	require.NoError(t, DB.First(&pending, keyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, pending.Status)
	require.NotEmpty(t, pending.EncryptedKey)
	require.NotEmpty(t, pending.ProviderCredential)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, []string{"billing_key_pending"}, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
	require.Empty(t, key.EncryptedKey)
	require.Empty(t, key.ProviderCredential)
	require.Zero(t, key.RevocationRetryTime)
}

func TestRetryPendingTossBillingKeyRevocationsMarksRevokedWhenRemoteAlreadyDeleted(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_already_deleted", "현대", "433012******1234", setting.TossBillingSecretKey)
	require.NoError(t, err)
	require.NoError(t, MarkTossBillingKeyPendingRevocation(nil, keyId))

	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		return ErrTossBillingKeyAlreadyDeleted
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
	require.Empty(t, key.EncryptedKey)
	require.Empty(t, key.ProviderCredential)
	require.Zero(t, key.RevocationRetryTime)
}

func TestRetryPendingTossBillingKeyRevocationsPreservesSecretsAfterRemoteFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_retry_secret", "현대", "433012******1234", setting.TossBillingSecretKey)
	require.NoError(t, err)
	require.NoError(t, MarkTossBillingKeyPendingRevocation(nil, keyId))

	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		return errors.New("temporary Toss deletion outage")
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	before := GetDBTimestamp()
	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, n)

	var pending UserBillingKey
	require.NoError(t, DB.First(&pending, keyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, pending.Status)
	require.NotEmpty(t, pending.EncryptedKey)
	require.NotEmpty(t, pending.ProviderCredential)
	require.Greater(t, pending.RevocationRetryTime, before)
}

func TestRetryPendingTossBillingKeyRevocationsContinuesAfterDecryptFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId: 7,
		// A different non-empty customer proves this corrupt identity cannot be
		// a legacy duplicate of the healthy key processed in the next batch.
		CustomerKey:  "cust_corrupt_unrelated",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusPendingRevocation,
		CreateTime:   common.GetTimestamp(),
	}).Error)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_after_corrupt", "현대", "433012******1234")
	require.NoError(t, err)
	require.NoError(t, MarkTossBillingKeyPendingRevocation(nil, keyId))

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	// With a one-row batch the corrupt oldest key is the whole first batch.
	// Its retry reservation must move it behind the next pending key.
	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, revoked)

	n, err = RetryPendingTossBillingKeyRevocations(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, []string{"billing_key_after_corrupt"}, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestDueTossRenewalReservationDoesNotLetLimitBatchStarveNextSubscription(t *testing.T) {
	setupTossBillingModelTestDB(t)
	now := GetDBTimestamp()
	rows := []UserSubscription{
		{UserId: 7, PlanId: 1, Status: "active", AutoRenew: true, BillingKeyId: 1, EndTime: now + 3600, NextBillingTime: now - 20},
		{UserId: 8, PlanId: 1, Status: "active", AutoRenew: true, BillingKeyId: 2, EndTime: now + 3600, NextBillingTime: now - 10},
	}
	require.NoError(t, DB.Create(&rows).Error)

	first, err := GetDueTossRenewals(now, 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, rows[0].Id, first[0].Id)

	// Simulate processing returning immediately with a permanent plan/snapshot
	// error: no completion mutation occurs. The queue reservation alone must let
	// the next bounded run reach the later valid subscription.
	second, err := GetDueTossRenewals(now, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, rows[1].Id, second[0].Id)
}

func TestRevokeTossBillingKeyDisablesLinkedAutoRenewSubscriptions(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_deleted_webhook", 0)

	require.NoError(t, RevokeTossBillingKey(nil, keyId))

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)
}

func TestBackfillTossBillingKeyHashesProcessesAllBatches(t *testing.T) {
	setupTossBillingModelTestDB(t)
	for i := 0; i < 1002; i++ {
		_, err := StoreTossBillingKey(7, "cust_test", fmt.Sprintf("billing_key_legacy_%d", i), "현대", "433012******1234")
		require.NoError(t, err)
	}
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("1 = 1").Update("billing_key_hash", "").Error)

	require.NoError(t, backfillTossBillingKeyHashes(1000))

	var missing int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("billing_key_hash = '' OR billing_key_hash IS NULL").Count(&missing).Error)
	require.Equal(t, int64(0), missing)
}

func TestRevokeTossBillingKeyByPlainMatchesEncryptedKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_deleted", "현대", "433012******1234")
	require.NoError(t, err)

	revoked, err := RevokeTossBillingKeyByPlain("cust_test", "billing_key_deleted")
	require.NoError(t, err)
	require.True(t, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestStoreTossBillingKeyReusesActiveRowAfterIssueRecovery(t *testing.T) {
	setupTossBillingModelTestDB(t)

	firstID, err := StoreTossBillingKeyWithSecret(7, "cust_issue_recovery", "billing_issue_recovery", "현대", "433012******1234", setting.TossBillingSecretKey)
	require.NoError(t, err)
	secondID, err := StoreTossBillingKeyWithSecret(7, "cust_issue_recovery", "billing_issue_recovery", "현대", "433012******1234", setting.TossBillingSecretKey)
	require.NoError(t, err)
	require.Equal(t, firstID, secondID)

	var count int64
	require.NoError(t, DB.Model(&UserBillingKey{}).
		Where("user_id = ? AND customer_key = ? AND billing_key_hash = ?", 7, "cust_issue_recovery", tossBillingKeyHash("billing_issue_recovery")).
		Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRevokeTossBillingKeyByPlainRevokesAllMatchingDuplicateRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	firstID, err := StoreTossBillingKey(7, "cust_duplicate", "billing_key_duplicate", "현대", "433012******1234")
	require.NoError(t, err)
	var duplicate UserBillingKey
	require.NoError(t, DB.First(&duplicate, firstID).Error)
	duplicate.Id = 0
	duplicate.CreateTime++
	require.NoError(t, DB.Create(&duplicate).Error)
	secondID := duplicate.Id
	otherCustomerID, err := StoreTossBillingKey(7, "cust_other", "billing_key_duplicate", "현대", "433012******1234")
	require.NoError(t, err)
	require.NoError(t, MarkTossBillingKeyPendingRevocation(nil, secondID))
	// Cover a legacy duplicate row that predates the HMAC lookup column.
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", secondID).Update("billing_key_hash", "").Error)

	revoked, err := RevokeTossBillingKeyByPlain("cust_duplicate", "billing_key_duplicate")
	require.NoError(t, err)
	require.True(t, revoked)

	var rows []UserBillingKey
	require.NoError(t, DB.Where("id IN ?", []int{firstID, secondID, otherCustomerID}).Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, BillingKeyStatusRevoked, rows[0].Status)
	require.Equal(t, BillingKeyStatusRevoked, rows[1].Status)
	require.Equal(t, BillingKeyStatusActive, rows[2].Status)
}

func TestRevokeTossBillingKeyByPlainMatchesStoredHashWhenEncryptedKeyIsUnreadable(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_hash_deleted", "현대", "433012******1234")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", keyId).Update("encrypted_key", "not-a-valid-encrypted-key").Error)

	revoked, err := RevokeTossBillingKeyByPlain("", "billing_key_hash_deleted")
	require.NoError(t, err)
	require.True(t, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestRevokeTossBillingKeyByPlainDoesNotLetUnrelatedCorruptCustomerBlockKnownMatch(t *testing.T) {
	setupTossBillingModelTestDB(t)
	corrupt := &UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_unrelated",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(corrupt).Error)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_deleted_after_corrupt", "현대", "433012******1234")
	require.NoError(t, err)

	revoked, err := RevokeTossBillingKeyByPlain("cust_test", "billing_key_deleted_after_corrupt")
	require.NoError(t, err)
	require.True(t, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
	var corruptStored UserBillingKey
	require.NoError(t, DB.First(&corruptStored, corrupt.Id).Error)
	require.Equal(t, BillingKeyStatusActive, corruptStored.Status)
}

func TestRevokeTossBillingKeyByPlainRejectsUnresolvedSameCustomerCandidate(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_unresolved",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}).Error)
	keyID, err := StoreTossBillingKey(7, "cust_unresolved", "billing_key_unresolved_duplicate", "현대", "433012******1234")
	require.NoError(t, err)

	revoked, err := RevokeTossBillingKeyByPlain("", "billing_key_unresolved_duplicate")
	require.False(t, revoked)
	require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestRevokeTossBillingKeyByPlainRejectsUnresolvedOnlyCandidate(t *testing.T) {
	setupTossBillingModelTestDB(t)
	const corruptCiphertext = "not-a-valid-encrypted-key"
	const providerBillingKey = "billing_key_unresolved_only"
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_unresolved_only",
		EncryptedKey: corruptCiphertext,
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}).Error)

	revoked, err := RevokeTossBillingKeyByPlain("", providerBillingKey)
	require.False(t, revoked)
	require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)
	require.NotContains(t, err.Error(), corruptCiphertext)
	require.NotContains(t, err.Error(), providerBillingKey)
}

func TestRevokeTossBillingKeyByPlainRejectsDecryptableInvalidCandidates(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "empty", plaintext: ""},
		{name: "only whitespace", plaintext: "   "},
		{name: "internal whitespace", plaintext: "billing key"},
		{name: "control character", plaintext: "billing\nkey"},
		{name: "too long", plaintext: strings.Repeat("b", TossBillingKeyMaxRunes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			encrypted, err := common.EncryptString(test.plaintext)
			require.NoError(t, err)
			row := &UserBillingKey{
				UserId:       7,
				CustomerKey:  "cust_decryptable_invalid",
				EncryptedKey: encrypted,
				Status:       BillingKeyStatusActive,
				CreateTime:   common.GetTimestamp(),
			}
			require.NoError(t, DB.Create(row).Error)

			revoked, err := RevokeTossBillingKeyByPlain("", "billing_key_valid_target")
			require.False(t, revoked)
			require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)

			var stored UserBillingKey
			require.NoError(t, DB.First(&stored, row.Id).Error)
			require.Equal(t, BillingKeyStatusActive, stored.Status)
			require.Empty(t, stored.BillingKeyHash)
		})
	}
}

func TestRevokeTossBillingKeyByPlainRejectsUnresolvedExplicitCustomerScope(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_explicit_unresolved",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}).Error)
	keyID, err := StoreTossBillingKey(7, "cust_explicit_unresolved", "billing_key_explicit_match", "현대", "433012******1234")
	require.NoError(t, err)

	revoked, err := RevokeTossBillingKeyByPlain("cust_explicit_unresolved", "billing_key_explicit_match")
	require.False(t, revoked)
	require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestBackfillTossBillingKeyHashesReportsActiveUnresolvedRowsAfterHashingHealthyRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	healthyID, err := StoreTossBillingKey(7, "cust_backfill_healthy", "billing_key_backfill_healthy", "현대", "433012******1234")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", healthyID).Update("billing_key_hash", "").Error)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       8,
		CustomerKey:  "cust_backfill_unresolved",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}).Error)

	err = backfillTossBillingKeyHashes(1000)
	require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)

	var healthy UserBillingKey
	require.NoError(t, DB.First(&healthy, healthyID).Error)
	require.Equal(t, tossBillingKeyHash("billing_key_backfill_healthy"), healthy.BillingKeyHash)
}

func TestTossProviderPOSTBarrierBlocksPristineIssueButAllowsAttemptedReplay(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &SubscriptionOrder{}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossBillingEnabled":   "true",
		"TossBillingClientKey": "live_ck_issue_barrier",
		"TossBillingSecretKey": "live_sk_issue_barrier",
	})
	snapshot := setting.GetTossConfigSnapshot()
	require.NoError(t, ensureTossConfigurationMaintenanceGate(snapshot.Revision))
	order := &SubscriptionOrder{
		TradeNo:             "toss_issue_attempt_barrier",
		PaymentMethod:       PaymentMethodToss,
		PaymentProvider:     PaymentProviderToss,
		Status:              common.TopUpStatusPending,
		BillingClaimToken:   "issue-barrier-token",
		BillingIssueAuthKey: "encrypted-auth-key",
	}
	require.NoError(t, DB.Create(order).Error)

	err := MarkTossSubscriptionBillingIssueAttempt(order.TradeNo, order.BillingClaimToken)
	require.ErrorIs(t, err, ErrTossConfigMaintenanceRequired)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.False(t, order.BillingIssueAttempted)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Update("billing_issue_attempted", true).Error)
	require.NoError(t, MarkTossSubscriptionBillingIssueAttempt(order.TradeNo, order.BillingClaimToken))
}

func TestBackfillTossBillingKeyHashesIgnoresRevokedUnresolvedRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       8,
		CustomerKey:  "cust_backfill_revoked",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusRevoked,
		CreateTime:   common.GetTimestamp(),
	}).Error)

	require.NoError(t, backfillTossBillingKeyHashes(1000))
}

func TestBackfillTossBillingKeyHashesReportsDecryptableInvalidRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	invalidPlaintexts := []string{
		"",
		"   ",
		"billing key",
		"billing\nkey",
		strings.Repeat("b", TossBillingKeyMaxRunes+1),
	}
	rowIDs := make([]int, 0, len(invalidPlaintexts))
	for i, plaintext := range invalidPlaintexts {
		encrypted, err := common.EncryptString(plaintext)
		require.NoError(t, err)
		row := &UserBillingKey{
			UserId:       7 + i,
			CustomerKey:  fmt.Sprintf("cust_decryptable_invalid_%d", i),
			EncryptedKey: encrypted,
			Status:       BillingKeyStatusActive,
			CreateTime:   common.GetTimestamp() + int64(i),
		}
		require.NoError(t, DB.Create(row).Error)
		rowIDs = append(rowIDs, row.Id)
	}

	err := backfillTossBillingKeyHashes(1000)
	require.ErrorIs(t, err, ErrTossBillingKeyIdentityUnresolved)
	for _, rowID := range rowIDs {
		var stored UserBillingKey
		require.NoError(t, DB.First(&stored, rowID).Error)
		require.Empty(t, stored.BillingKeyHash)
	}
}

func TestAdminInvalidateUserSubscriptionRevokesTossBillingKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_admin_invalidate", 0)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	_, err := AdminInvalidateUserSubscription(11)
	require.NoError(t, err)
	require.Equal(t, []string{"billing_key_admin_invalidate"}, revoked)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestAdminInvalidateDuplicateBillingKeyKeepsProviderKeyForActiveWallet(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyAID := seedTossBillingSubscription(t, "billing_key_shared_duplicate", 0)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))

	var keyA UserBillingKey
	require.NoError(t, DB.First(&keyA, keyAID).Error)
	keyB := UserBillingKey{
		UserId:                keyA.UserId,
		CustomerKey:           keyA.CustomerKey,
		EncryptedKey:          keyA.EncryptedKey,
		BillingKeyHash:        keyA.BillingKeyHash,
		ProviderCredential:    keyA.ProviderCredential,
		ProviderClientKeyHash: keyA.ProviderClientKeyHash,
		CardCompany:           keyA.CardCompany,
		CardNumberMasked:      keyA.CardNumberMasked,
		Status:                BillingKeyStatusActive,
		CreateTime:            keyA.CreateTime + 1,
	}
	require.NoError(t, DB.Create(&keyB).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, keyA.UserId, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetType:    TopUpTargetTypeUser,
		TargetId:      keyA.UserId,
		ActiveKey:     &activeKey,
		OwnerUserId:   keyA.UserId,
		BillingKeyId:  keyB.Id,
		CustomerKey:   keyA.CustomerKey,
		AuthTradeNo:   "wallet_duplicate_provider_key_active",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Status:        WalletAutoRechargeStatusActive,
	}
	require.NoError(t, DB.Create(&policy).Error)

	remoteDeletes := 0
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		remoteDeletes++
		return nil
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	_, err := AdminInvalidateUserSubscription(11)
	require.NoError(t, err)
	require.Zero(t, remoteDeletes, "another live duplicate row must keep the shared provider key")

	require.NoError(t, DB.First(&keyA, keyA.Id).Error)
	require.Equal(t, BillingKeyStatusActive, keyA.Status)
	require.NoError(t, DB.First(&keyB, keyB.Id).Error)
	require.Equal(t, BillingKeyStatusActive, keyB.Status)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)
}

func TestAdminInvalidateSharedBillingKeyWaitsForLastSubscription(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_shared_subscriptions", 0)
	var first UserSubscription
	require.NoError(t, DB.First(&first, 11).Error)
	second := first
	second.Id = 12
	second.StartTime++
	second.EndTime++
	require.NoError(t, DB.Create(&second).Error)

	remoteDeletes := 0
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		remoteDeletes++
		require.Equal(t, "billing_key_shared_subscriptions", billingKey)
		return nil
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	_, err := AdminInvalidateUserSubscription(first.Id)
	require.NoError(t, err)
	require.Zero(t, remoteDeletes)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
	require.NoError(t, DB.First(&second, second.Id).Error)
	require.True(t, second.AutoRenew)

	_, err = AdminInvalidateUserSubscription(second.Id)
	require.NoError(t, err)
	require.Equal(t, 1, remoteDeletes)
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestCancelTossAutoRenewKeepsSharedWalletPolicyActive(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_shared_subscription_wallet", 0)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 7, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 7,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test",
		AuthTradeNo: "wallet_shared_subscription_key", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	remoteDeletes := 0
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		remoteDeletes++
		return nil
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })

	require.NoError(t, CancelTossAutoRenewForUser(context.Background(), 7))
	require.Zero(t, remoteDeletes)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)

	revokeID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Equal(t, keyID, revokeID)
	require.NoError(t, RevokeStoredTossBillingKey(context.Background(), revokeID))
	require.Equal(t, 1, remoteDeletes)
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestConfirmedBillingDeletionDisablesEverySharedLocalContract(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_provider_deleted", 0)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 7, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 7,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test",
		AuthTradeNo: "wallet_provider_key_deleted", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	revoked, err := RevokeTossBillingKeyByPlain("cust_test", "billing_key_provider_deleted")
	require.NoError(t, err)
	require.True(t, revoked)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)
	require.Zero(t, sub.NextBillingTime)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestPendingCleanupReusesActiveBillingKeyWithLiveReference(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_cleanup_reuse", 0)
	cleanupID, err := StoreTossBillingKeyPendingRevocationWithSecret(
		7, "cust_test", "billing_key_cleanup_reuse", "현대", "****1234", setting.TossBillingSecretKey,
	)
	require.NoError(t, err)
	require.Equal(t, keyID, cleanupID)
	var keyCount int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("billing_key_hash = ?", tossBillingKeyHash("billing_key_cleanup_reuse")).Count(&keyCount).Error)
	require.Equal(t, int64(1), keyCount)

	remoteDeletes := 0
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		remoteDeletes++
		return nil
	})
	t.Cleanup(func() { SetTossBillingRevoker(previousRevoker) })
	require.NoError(t, RevokeStoredTossBillingKey(context.Background(), cleanupID))
	require.Zero(t, remoteDeletes)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestDeactivateOrganizationTossBillingPreservesPersonalSharedKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_org_personal_shared", 0)
	organization := Organization{Id: 41, Name: "shared-key-org", OwnerUserId: 7, Status: 1}
	require.NoError(t, DB.Create(&organization).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeOrganization, organization.Id, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeOrganization, TargetId: organization.Id,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test",
		AuthTradeNo: "wallet_org_personal_shared", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	require.NoError(t, DeactivateTossBillingForOrganization(nil, organization.Id))
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.True(t, sub.AutoRenew)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
}

func TestStoreTossBillingKeyRejectsCustomerKeyOutsideLocalSchema(t *testing.T) {
	setupTossBillingModelTestDB(t)
	_, err := StoreTossBillingKey(7, strings.Repeat("a", TossBillingIssueCustomerKeyMaxBytes)+"_", "billing_key_invalid_customer", "현대", "****1234")
	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestAdminDeleteUserSubscriptionRevokesTossBillingKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_admin_delete", 0)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	_, err := AdminDeleteUserSubscription(11)
	require.NoError(t, err)
	require.Equal(t, []string{"billing_key_admin_delete"}, revoked)

	var sub UserSubscription
	err = DB.First(&sub, 11).Error
	require.Error(t, err)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestExpireDueSubscriptionsSkipsActiveTossAutoRenewUntilBillingTaskHandlesIt(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_expired", 0)
	now := time.Now().Unix()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time": now - 1,
	}).Error)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	n, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, revoked)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, "active", sub.Status)
	require.True(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestExpireDueSubscriptionsIncludingTossAutoRenewExpiresAndRevokesKey(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_expired_include", 0)
	now := time.Now().Unix()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", 11).Updates(map[string]interface{}{
		"end_time": now - 1,
	}).Error)

	var revoked []string
	previousRevoker := tossBillingRevoker
	SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})
	t.Cleanup(func() {
		SetTossBillingRevoker(previousRevoker)
	})

	n, err := ExpireDueSubscriptionsIncludingTossAutoRenew(10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []string{"billing_key_expired_include"}, revoked)

	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestQuoteTossTopUpDefaultsToKRWMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(13000, "", "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
	require.Equal(t, 1300.0, quote.UnitPrice)
}

func TestQuoteTossTopUpInvalidModeDefaultsToKRWMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(13000, "invalid", "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpKRWModeFloorsQuotaUsingDecimal(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 3
	common.QuotaPerUnit = 3
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(1, TossTopUpAmountModeKRW, "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(1), quote.ChargeKRW)
	require.InDelta(t, 1.0/3.0, quote.CreditAmount, 1e-12)
	require.Equal(t, 1, quote.CreditQuota)
}

func TestQuoteTossTopUpQuotaModeChargesUnitPriceTimesQuota(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, TossTopUpAmountModeQuota, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpNonPositiveUnitPriceReturnsZeroQuote(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 0
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(13000, TossTopUpAmountModeKRW, "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(0), quote.ChargeKRW)
	require.InDelta(t, 0.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 0, quote.CreditQuota)
	require.Equal(t, 0.0, quote.UnitPrice)
}

func TestQuoteTossTopUpKRWModeKeepsChargeFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10000: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10000, TossTopUpAmountModeKRW, "default")

	require.Equal(t, int64(10000), quote.ChargeKRW)
	require.InDelta(t, 20.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 10000000, quote.CreditQuota)
}

func TestQuoteTossTopUpQuotaModeKeepsCreditFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, int64(5000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpRejectsChargeAboveApplicationSafetyBound(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1
	common.QuotaPerUnit = 1
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(setting.TossMaximumChargeAmountKRW+1, TossTopUpAmountModeKRW, "default")

	require.Zero(t, quote.ChargeKRW)
	require.Zero(t, quote.CreditAmount)
	require.Zero(t, quote.CreditQuota)
}

func TestQuoteTossTopUpRejectsQuotaOverflow(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1
	common.QuotaPerUnit = 1e18
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(100, TossTopUpAmountModeQuota, "default")

	require.Zero(t, quote.ChargeKRW)
	require.Zero(t, quote.CreditAmount)
	require.Zero(t, quote.CreditQuota)
}

func TestTossAmountConversionsRejectUnsafeValues(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	setting.TossUnitPrice = 1
	common.QuotaPerUnit = 1e18
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
	}()

	require.Zero(t, TossPlanKRW(math.Inf(1)))
	require.Zero(t, TossPlanKRW(float64(setting.TossMaximumChargeAmountKRW+1)))
	require.False(t, IsTossCardAmountPayableKRW(setting.TossMaximumChargeAmountKRW+1))
	require.Zero(t, TossCreditQuotaFromKRW(100))
	require.Zero(t, walletAutoRechargeKRW(float64(setting.TossMaximumChargeAmountKRW+1)))
}
