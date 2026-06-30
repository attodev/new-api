package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
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
