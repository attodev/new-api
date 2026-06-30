package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletAutoRechargeTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, DB.AutoMigrate(&User{}, &Organization{}, &TopUp{}, &UserBillingKey{}, &WalletAutoRecharge{}))
}

func stubWalletCharger(done bool, total int64, err error) TossBillingCharger {
	return func(ctx context.Context, billingKey, customerKey, orderID, orderName string, amount int64) (bool, int64, error) {
		return done, total, err
	}
}

func TestCreatePendingWalletAutoRechargeRejectsDuplicateActiveType(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Now().Unix()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	require.NoError(t, DB.Create(&WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: now + 3600,
		ActiveKey:      &activeKey,
		AuthTradeNo:    "existing-trade-no",
	}).Error)

	_, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetType:    TopUpTargetTypeUser,
		TargetId:      1,
		OwnerUserId:   1,
		Amount:        20000,
		AuthTradeNo:   "new-trade-no",
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "active wallet auto recharge already exists")
}

func TestCreatePendingWalletAutoRechargeNormalizesCustomIntervalValue(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetType:    TopUpTargetTypeUser,
		TargetId:      1,
		OwnerUserId:   1,
		Amount:        20000,
		IntervalUnit:  WalletAutoRechargeIntervalCustom,
		CustomSeconds: 3600,
	})

	require.NoError(t, err)
	require.Equal(t, 1, policy.IntervalValue)

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	require.Equal(t, 1, stored.IntervalValue)
}

func TestCancelWalletAutoRechargeKeepsBillingKeyActive(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           21,
		UserId:       1,
		CustomerKey:  "customer-21",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	require.NoError(t, DB.Create(&WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		BillingKeyId:   21,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: time.Now().Add(time.Hour).Unix(),
		ActiveKey:      &activeKey,
	}).Error)

	require.NoError(t, CancelWalletAutoRecharge(1, TopUpTargetTypeUser, 1))

	var billingKey UserBillingKey
	require.NoError(t, DB.First(&billingKey, 21).Error)
	require.Equal(t, BillingKeyStatusActive, billingKey.Status)

	var policy WalletAutoRecharge
	require.NoError(t, DB.First(&policy, 1).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
}

func TestProcessWalletAutoRechargeCreditsUserWallet(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1300
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", AffCode: "wallet-auto-owner"}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           11,
		UserId:       1,
		CustomerKey:  "customer-1",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		BillingKeyId:   11,
		Amount:         13000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: time.Now().Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, stubWalletCharger(true, 13000, nil))

	require.NoError(t, err)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int(10*common.QuotaPerUnit), user.Quota)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "target_type = ? AND target_id = ?", TopUpTargetTypeUser, 1).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, PaymentProviderToss, topUp.PaymentProvider)
	require.Equal(t, int64(13000), topUp.Amount)
	require.Equal(t, float64(10), topUp.Money)
}

func TestProcessWalletAutoRechargeAppliesOwnerGroupTossTopUpPricing(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	originalRatio := common.TopupGroupRatio2JSONString()
	setting.TossUnitPrice = 1000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{20000: 0.5}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalRatio))
	})

	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Group: "vip", AffCode: "wallet-auto-owner-vip"}).Error)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           31,
		UserId:       1,
		CustomerKey:  "customer-31",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		BillingKeyId:   31,
		Amount:         20000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: time.Now().Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	var chargedAmount int64
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, func(ctx context.Context, billingKey, customerKey, orderID, orderName string, amount int64) (bool, int64, error) {
		chargedAmount = amount
		return true, amount, nil
	})

	require.NoError(t, err)
	require.Equal(t, int64(12000), chargedAmount)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int(12*common.QuotaPerUnit), user.Quota)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "target_type = ? AND target_id = ?", TopUpTargetTypeUser, 1).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, int64(12000), topUp.Amount)
	require.Equal(t, float64(12), topUp.Money)
}

func TestProcessWalletAutoRechargeKeepsPendingTopUpWhenPostChargeCreditFails(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Group: "default", AffCode: "wallet-auto-owner-reconcile"}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           32,
		UserId:       1,
		CustomerKey:  "customer-32",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	now := time.Now()
	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeOrganization,
		TargetId:       404,
		OwnerUserId:    1,
		BillingKeyId:   32,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	calls := 0
	chargeErr := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, orderID, orderName string, amount int64) (bool, int64, error) {
		calls++
		return true, amount, nil
	})

	require.Error(t, chargeErr)
	require.Equal(t, 1, calls)

	tradeNo := walletAutoRechargeTradeNo(policy, now)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.Equal(t, int64(10000), topUp.Amount)
	require.Equal(t, float64(10), topUp.Money)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, reloaded.Status)
	require.Equal(t, 0, reloaded.FailCount)
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Contains(t, reloaded.LastError, "reconciliation")

	require.NoError(t, DB.Create(&Organization{Id: 404, Name: "reconcile-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, orderID, orderName string, amount int64) (bool, int64, error) {
		calls++
		return true, amount, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestActivateWalletAutoRechargeFromTossLeavesPolicyActiveWhenImmediateCreditNeedsReconciliation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Group: "default", AffCode: "wallet-auto-activation-reconcile"}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           33,
		UserId:       1,
		CustomerKey:  "customer-33",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:              WalletAutoRechargeTypeScheduled,
		TargetType:        TopUpTargetTypeOrganization,
		TargetId:          405,
		OwnerUserId:       1,
		CustomerKey:       "customer-33",
		AuthTradeNo:       "wallet-auto-activation-reconcile",
		Amount:            10000,
		IntervalUnit:      WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
	})
	require.NoError(t, err)

	_, err = ActivateWalletAutoRechargeFromToss(policy.AuthTradeNo, 33, "card", "****1234", false, stubWalletCharger(true, 10000, nil))

	require.Error(t, err)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", reloaded.LastTradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func TestProcessThresholdWalletAutoRechargeRespectsCooldownAndDailyLimit(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Now()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", AffCode: "wallet-threshold-owner"}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           12,
		UserId:       1,
		CustomerKey:  "customer-2",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy := WalletAutoRecharge{
		Type:             WalletAutoRechargeTypeThreshold,
		TargetType:       TopUpTargetTypeUser,
		TargetId:         1,
		OwnerUserId:      1,
		BillingKeyId:     12,
		Amount:           10000,
		ThresholdAmount:  5000,
		ThresholdQuota:   walletAutoRechargeQuota(5000),
		Status:           WalletAutoRechargeStatusActive,
		CooldownUntil:    now.Add(time.Hour).Unix(),
		DailyChargeCount: 3,
		DailyChargeDate:  now.Format("2006-01-02"),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, stubWalletCharger(true, 13000, nil))

	require.NoError(t, err)

	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestProcessWalletAutoRechargeMarksFailureWhenChargerFailsBeforeDone(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Group: "default", AffCode: "wallet-auto-owner-charge-fail"}).Error)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           41,
		UserId:       1,
		CustomerKey:  "customer-41",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		BillingKeyId:   41,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		NextChargeTime: time.Now().Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 1, stubWalletCharger(false, 0, errors.New("network failed")))

	require.NoError(t, err)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
}
