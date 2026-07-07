package service

import (
	"context"
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
	common.CryptoSecret = "toss-billing-service-test-secret"
	require.NoError(t, model.DB.AutoMigrate(&model.UserSubscription{}, &model.UserBillingKey{}))

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

func TestRunTossBillingOnceExpiresOverdueAutoRenewWhenBillingTaskNotRunnable(t *testing.T) {
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

	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_key_task_unavailable", "Hyundai", "433012******1234")
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
		revoked = append(revoked, billingKey)
		return nil
	})

	runTossBillingOnce()

	require.Equal(t, []string{"billing_key_task_unavailable"}, revoked)

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)

	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyId).Error)
	require.Equal(t, model.BillingKeyStatusRevoked, key.Status)
}

func TestRunTossBillingOnceExpiresOverdueAutoRenewLocallyWhenComplianceDisabled(t *testing.T) {
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

	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_key_compliance_disabled", "Hyundai", "433012******1234")
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
		revoked = append(revoked, billingKey)
		return nil
	})

	runTossBillingOnce()

	require.Empty(t, revoked)

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.Equal(t, "expired", sub.Status)
	require.False(t, sub.AutoRenew)

	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyId).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
}
