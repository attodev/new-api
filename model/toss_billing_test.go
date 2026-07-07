package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	require.NoError(t, DB.AutoMigrate(&UserSubscription{}, &UserBillingKey{}))
	common.CryptoSecret = "toss-billing-test-secret"
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		common.CryptoSecret = originalSecret
	})
}

func seedTossBillingSubscription(t *testing.T, billingKey string, failCount int) int {
	t.Helper()
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
}

func TestUserBillingKeyStatusColumnFitsPendingRevocation(t *testing.T) {
	field, ok := reflect.TypeOf(UserBillingKey{}).FieldByName("Status")
	require.True(t, ok)
	tag := field.Tag.Get("gorm")
	require.NotContains(t, tag, "varchar(16)")
	require.True(t, strings.Contains(tag, "varchar(32)") || strings.Contains(tag, "varchar(64)"), "gorm tag %q should fit %q", tag, BillingKeyStatusPendingRevocation)
}

func TestParseTossRenewalTradeNo(t *testing.T) {
	subId, nextBillingTime, ok := ParseTossRenewalTradeNo("toss_sub_renew_11_1782840000")
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
		"toss_sub_renew_11_1782840000_extra",
	} {
		_, _, ok := ParseTossRenewalTradeNo(tradeNo)
		require.False(t, ok, "tradeNo %q should not parse as renewal", tradeNo)
	}
}

func TestProcessTossRenewalRejectsBelowTossCardMinimumWithoutCharge(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "billing_key_low_amount", 0)
	seedTossBillingPlan(t, "Tiny", 1)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 5
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

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
	require.Equal(t, 1, sub.BillingFailCount)
	require.True(t, sub.AutoRenew)
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

func TestProcessTossRenewalUsesStoredBillingSecret(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKeyWithSecret(7, "cust_test", "billing_key_secret_snapshot", "현대", "433012******1234", "sk_old_billing")
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

	var capturedSecret string
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedSecret = secretKey
		return &TossBillingChargeResult{Done: true, Total: amount}, nil
	})
	t.Cleanup(func() {
		SetTossBillingCharger(previousCharger)
	})

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, 3))
	require.Equal(t, "sk_old_billing", capturedSecret)
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

func TestProcessTossRenewalCreatesPendingSnapshotBeforeRemoteCharge(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_pre_snapshot", 0)
	seedTossBillingPlan(t, "Snapshot Renewal", 10)
	var seededSub UserSubscription
	require.NoError(t, DB.First(&seededSub, 11).Error)
	tradeNo := "toss_sub_renew_11_" + fmt.Sprint(seededSub.NextBillingTime)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	var capturedAmount int64
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error) {
		capturedAmount = amount
		var order SubscriptionOrder
		require.NoError(t, DB.Where("trade_no = ?", orderId).First(&order).Error)
		require.Equal(t, common.TopUpStatusPending, order.Status)
		require.Equal(t, int64(15000), order.ProviderAmount)
		require.Equal(t, "KRW", order.ProviderCurrency)
		require.Equal(t, keyId, order.BillingKeyId)
		return &TossBillingChargeResult{
			Done:            true,
			Total:           amount,
			PaymentKey:      "pay_renewal_pre_snapshot",
			ProviderPayload: `{"paymentKey":"pay_renewal_pre_snapshot","status":"DONE","totalAmount":15000,"currency":"KRW"}`,
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
	require.Equal(t, "KRW", order.ProviderCurrency)
	require.Equal(t, keyId, order.BillingKeyId)
}

func TestProcessTossRenewalUsesPendingOrderSnapshotAfterUnitPriceChange(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId := seedTossBillingSubscription(t, "billing_key_existing_snapshot", 0)
	seedTossBillingPlan(t, "Existing Snapshot Renewal", 10)

	var seededSub UserSubscription
	require.NoError(t, DB.First(&seededSub, 11).Error)
	tradeNo := "toss_sub_renew_11_" + fmt.Sprint(seededSub.NextBillingTime)
	credential, err := EncryptProviderCredential("sk_existing_snapshot")
	require.NoError(t, err)
	require.NoError(t, (&SubscriptionOrder{
		UserId:             7,
		PlanId:             3,
		Money:              10,
		TradeNo:            tradeNo,
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		BillingKeyId:       keyId,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}).Insert())

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 5
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

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

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_renew_11_"+fmt.Sprint(sub.NextBillingTime)).First(&order).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.Equal(t, int64(15000), order.ProviderAmount)
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
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_pending", "현대", "433012******1234")
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

	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, []string{"billing_key_pending"}, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
}

func TestRetryPendingTossBillingKeyRevocationsMarksRevokedWhenRemoteAlreadyDeleted(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_already_deleted", "현대", "433012******1234")
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
}

func TestRetryPendingTossBillingKeyRevocationsContinuesAfterDecryptFailure(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_test",
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

	n, err := RetryPendingTossBillingKeyRevocations(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Equal(t, []string{"billing_key_after_corrupt"}, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
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

func TestRevokeTossBillingKeyByPlainSkipsCorruptRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.Create(&UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_test",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       BillingKeyStatusActive,
		CreateTime:   common.GetTimestamp(),
	}).Error)
	keyId, err := StoreTossBillingKey(7, "cust_test", "billing_key_deleted_after_corrupt", "현대", "433012******1234")
	require.NoError(t, err)

	revoked, err := RevokeTossBillingKeyByPlain("cust_test", "billing_key_deleted_after_corrupt")
	require.NoError(t, err)
	require.True(t, revoked)

	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyId).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)
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
