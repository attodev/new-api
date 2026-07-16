package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func configurePersistentTossBillingCryptoForServiceTest(t *testing.T) {
	t.Helper()
	originalSecret := common.CryptoSecret
	t.Setenv("CRYPTO_SECRET", "toss-billing-service-test-secret")
	common.CryptoSecret = "toss-billing-service-test-secret"
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
}

func setupTossBillingServiceTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalSecret := common.CryptoSecret

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	t.Setenv("CRYPTO_SECRET", "toss-billing-service-test-secret")
	common.CryptoSecret = "toss-billing-service-test-secret"
	require.NoError(t, model.DB.AutoMigrate(&model.UserSubscription{}, &model.SubscriptionOrder{}, &model.UserBillingKey{}, &model.WalletAutoRecharge{}, &model.TossRecurringOrderIDProtocolState{}))
	require.NoError(t, model.DB.Create(&model.TossRecurringOrderIDProtocolState{Id: 1, WriteVersion: 2}).Error)

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		common.CryptoSecret = originalSecret
	})
}

func TestTossBillingTaskEnabledRequiresExplicitBillingToggle(t *testing.T) {
	configurePersistentTossBillingCryptoForServiceTest(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalEnabled := setting.TossEnabled
	originalBillingEnabled := setting.TossBillingEnabled
	originalTestMode := setting.TossTestMode
	originalClient := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalTestClient := setting.TossTestClientKey
	originalTestSecret := setting.TossTestSecretKey
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalBillingTestClient := setting.TossBillingTestClientKey
	originalBillingTestSecret := setting.TossBillingTestSecretKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossEnabled = originalEnabled
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossTestMode = originalTestMode
		setting.TossClientKey = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestClientKey = originalTestClient
		setting.TossTestSecretKey = originalTestSecret
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossBillingTestClientKey = originalBillingTestClient
		setting.TossBillingTestSecretKey = originalBillingTestSecret
	})

	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossEnabled = true
	setting.TossBillingEnabled = false
	setting.TossTestMode = false
	setting.TossClientKey = "regular_ck_live_test"
	setting.TossSecretKey = "regular_sk_live_test"
	setting.TossBillingClientKey = ""
	setting.TossBillingSecretKey = ""
	setting.TossTestClientKey = ""
	setting.TossTestSecretKey = ""
	setting.TossBillingTestClientKey = ""
	setting.TossBillingTestSecretKey = ""

	require.False(t, isTossBillingTaskRunnable())

	setting.TossBillingEnabled = true
	require.False(t, isTossBillingTaskRunnable())

	setting.TossBillingClientKey = "billing_ck_live_test"
	setting.TossBillingSecretKey = "billing_sk_live_test"
	require.True(t, isTossBillingTaskRunnable())

	setting.TossBillingSecretKey = ""
	require.False(t, isTossBillingTaskRunnable())

	setting.TossBillingSecretKey = "billing_sk_live_test"
	paymentSetting.ComplianceConfirmed = false
	require.False(t, isTossBillingTaskRunnable())
}

func TestTossBillingRemoteCleanupDoesNotRequireBillingToggle(t *testing.T) {
	configurePersistentTossBillingCryptoForServiceTest(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalEnabled := setting.TossEnabled
	originalBillingEnabled := setting.TossBillingEnabled
	originalTestMode := setting.TossTestMode
	originalClient := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalTestClient := setting.TossTestClientKey
	originalTestSecret := setting.TossTestSecretKey
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalBillingTestClient := setting.TossBillingTestClientKey
	originalBillingTestSecret := setting.TossBillingTestSecretKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossEnabled = originalEnabled
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossTestMode = originalTestMode
		setting.TossClientKey = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestClientKey = originalTestClient
		setting.TossTestSecretKey = originalTestSecret
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossBillingTestClientKey = originalBillingTestClient
		setting.TossBillingTestSecretKey = originalBillingTestSecret
	})

	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossEnabled = false
	setting.TossBillingEnabled = false
	setting.TossTestMode = false
	setting.TossClientKey = ""
	setting.TossSecretKey = ""
	setting.TossBillingClientKey = "billing_ck_live_test"
	setting.TossBillingSecretKey = "billing_sk_live_test"
	setting.TossTestClientKey = ""
	setting.TossTestSecretKey = ""
	setting.TossBillingTestClientKey = ""
	setting.TossBillingTestSecretKey = ""

	require.True(t, isTossBillingRemoteCleanupRunnable())

	setting.TossBillingSecretKey = ""
	require.True(t, isTossBillingRemoteCleanupRunnable())
}

func TestTossBillingTasksRequirePersistentCryptoSecret(t *testing.T) {
	originalSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-toss-secret"

	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalUnitPrice := setting.TossUnitPrice
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossUnitPrice = originalUnitPrice
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossBillingEnabled = true
	setting.TossUnitPrice = 1300
	setting.TossBillingClientKey = "billing_ck_live_test"
	setting.TossBillingSecretKey = "billing_sk_live_test"

	require.False(t, isTossBillingTaskRunnable())
	require.False(t, isTossBillingRemoteCleanupRunnable())
}

func TestRunTossBillingOncePreservesRecentOverdueAutoRenewDuringOperationalOutage(t *testing.T) {
	setupTossBillingServiceTestDB(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalUnitPrice := setting.TossUnitPrice
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalBillingTestClient := setting.TossBillingTestClientKey
	originalBillingTestSecret := setting.TossBillingTestSecretKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossUnitPrice = originalUnitPrice
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossBillingTestClientKey = originalBillingTestClient
		setting.TossBillingTestSecretKey = originalBillingTestSecret
		model.SetTossBillingRevoker(nil)
	})

	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossBillingEnabled = false
	setting.TossUnitPrice = 1300
	setting.TossBillingClientKey = ""
	setting.TossBillingSecretKey = ""
	setting.TossBillingTestClientKey = ""
	setting.TossBillingTestSecretKey = ""

	keyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_task_unavailable", "Hyundai", "433012******1234", "stored_sk_task_unavailable")
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       now - 86400,
		EndTime:         now - 1,
		AutoRenew:       true,
		BillingKeyId:    keyId,
		NextBillingTime: now - 3600,
	}).Error)

	var revoked []string
	model.SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		require.Equal(t, "stored_sk_task_unavailable", secretKey)
		revoked = append(revoked, billingKey)
		return nil
	})

	runTossBillingOnce()

	require.Empty(t, revoked)

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.Equal(t, "active", sub.Status)
	require.True(t, sub.AutoRenew)

	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyId).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
}

func TestRunTossBillingOnceExpiresAutoRenewBeyondOperationalGrace(t *testing.T) {
	setupTossBillingServiceTestDB(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossUnitPrice = originalUnitPrice
		model.SetTossBillingRevoker(nil)
	})

	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossBillingEnabled = false
	setting.TossUnitPrice = 1300

	keyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_compliance_disabled", "Hyundai", "433012******1234", "stored_sk_compliance_disabled")
	require.NoError(t, err)
	now := time.Now().Unix()
	staleEnd := now - int64(tossBillingOperationalGracePeriod/time.Second) - 1
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       staleEnd - 86400,
		EndTime:         staleEnd,
		AutoRenew:       true,
		BillingKeyId:    keyId,
		NextBillingTime: now - 3600,
	}).Error)

	var revoked []string
	model.SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		require.Equal(t, "stored_sk_compliance_disabled", secretKey)
		revoked = append(revoked, billingKey)
		return nil
	})

	tossBillingRunning.Store(false)
	runTossBillingOnce()

	// Expiry is local and bounded; remote deletion is delegated to the fair
	// retry queue so a slow Toss DELETE cannot block the scheduler tick.
	require.Empty(t, revoked)

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)

	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyId).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)

	tossBillingRunning.Store(false)
	runTossBillingOnce()
	require.Equal(t, []string{"billing_key_compliance_disabled"}, revoked)
	require.NoError(t, model.DB.First(&key, keyId).Error)
	require.Equal(t, model.BillingKeyStatusRevoked, key.Status)
}

func TestRunTossBillingOnceNeverChargesStaleRenewalAfterServiceRecovery(t *testing.T) {
	setupTossBillingServiceTestDB(t)
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalTestMode := setting.TossTestMode
	originalUnitPrice := setting.TossUnitPrice
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossTestMode = originalTestMode
		setting.TossUnitPrice = originalUnitPrice
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		model.SetTossBillingCharger(nil)
		model.SetTossBillingRevoker(nil)
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossBillingEnabled = true
	setting.TossTestMode = false
	setting.TossUnitPrice = 1300
	setting.TossBillingClientKey = "live_ck_recovered_service"
	setting.TossBillingSecretKey = "live_sk_recovered_service"

	keyID, err := model.StoreTossBillingKeyWithSecret(7, "cust_recovered_service", "billing_key_recovered_service", "Hyundai", "433012******1234", "live_sk_recovered_service")
	require.NoError(t, err)
	now := model.GetDBTimestamp()
	staleEnd := now - int64(tossBillingOperationalGracePeriod/time.Second) - 1
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id: 21, UserId: 7, PlanId: 3, Status: "active", StartTime: staleEnd - 86400, EndTime: staleEnd,
		AutoRenew: true, BillingKeyId: keyID, NextBillingTime: staleEnd - 3600,
	}).Error)
	chargeCalls := 0
	model.SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*model.TossBillingChargeResult, error) {
		chargeCalls++
		return nil, errors.New("stale renewal must expire before any provider POST")
	})
	var revoked []string
	model.SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		revoked = append(revoked, billingKey)
		return nil
	})

	tossBillingRunning.Store(false)
	runTossBillingOnce()

	require.Zero(t, chargeCalls)
	require.Empty(t, revoked)
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 21).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)

	tossBillingRunning.Store(false)
	runTossBillingOnce()
	require.Zero(t, chargeCalls)
	require.Equal(t, []string{"billing_key_recovered_service"}, revoked)
}

func TestStaleRenewalCleanupDoesNotBlockRecentRenewalSelection(t *testing.T) {
	setupTossBillingServiceTestDB(t)
	keyID, err := model.StoreTossBillingKeyWithSecret(
		7, "cust_stale_backlog", "billing_key_stale_backlog", "Hyundai", "433012******1234", "stored_sk_stale_backlog",
	)
	require.NoError(t, err)
	now := model.GetDBTimestamp()
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:              31,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       now - 3*24*60*60,
		EndTime:         now - model.TossBillingOperationalGraceSeconds - 1,
		AutoRenew:       true,
		BillingKeyId:    keyID,
		NextBillingTime: now - model.TossBillingOperationalGraceSeconds - 3600,
	}).Error)

	// Due selection and the final provider gate independently apply the grace
	// cutoff, so draining one stale batch must not suppress recent valid rows in
	// the same scheduler tick.
	require.True(t, expireStaleTossRenewalsBeforeCharging(context.Background(), 1))
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 31).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)
}
