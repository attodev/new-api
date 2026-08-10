package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWalletAutoRechargeTestDB(t *testing.T) {
	t.Helper()
	originalCryptoSecret := common.CryptoSecret
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalBillingEnabled := setting.TossBillingEnabled
	originalWalletAutoRechargeEnabled := setting.TossWalletAutoRechargeEnabled
	originalTestMode := setting.TossTestMode
	common.CryptoSecret = "wallet-auto-recharge-test-secret"
	t.Setenv("CRYPTO_SECRET", common.CryptoSecret)
	setting.TossTestMode = false
	setting.TossBillingClientKey = "wallet_auto_test_ck"
	setting.TossBillingSecretKey = "wallet_auto_test_sk"
	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = true
	t.Cleanup(func() {
		common.CryptoSecret = originalCryptoSecret
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossWalletAutoRechargeEnabled = originalWalletAutoRechargeEnabled
		setting.TossTestMode = originalTestMode
		SetWalletAutoRechargeTossPaymentLookup(nil)
	})

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

	require.NoError(t, DB.AutoMigrate(&User{}, &Organization{}, &TopUp{}, &SubscriptionOrder{}, &UserBillingKey{}, &UserSubscription{}, &WalletAutoRecharge{}, &TossPaymentEvent{}, &TossRecurringOrderIDProtocolState{}))
	require.NoError(t, initializeTossRecurringOrderIDProtocolState(true))
}

func stubWalletCharger(done bool, total int64, err error) TossBillingCharger {
	return func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		if err != nil {
			return nil, err
		}
		return &TossBillingChargeResult{
			Done:            done,
			Total:           total,
			PaymentKey:      "pay_" + orderID,
			ProviderPayload: `{"status":"DONE"}`,
		}, nil
	}
}

func withWalletAutoRechargeTestMID(t *testing.T, key *UserBillingKey) *UserBillingKey {
	t.Helper()
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	key.ProviderCredential = credential
	key.ProviderClientKeyHash = TossBillingClientKeyFingerprint("wallet_auto_test_ck")
	return key
}

func seedProviderAttemptedWalletPolicy(t *testing.T) (WalletAutoRecharge, TopUp, string) {
	t.Helper()
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "provider-attempt-fence-owner", AffCode: "provider-attempt-fence-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	plainKey := "provider-attempt-fence-key"
	encryptedKey, err := common.EncryptString(plainKey)
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 1002, UserId: 1, CustomerKey: "provider-attempt-fence-customer",
		EncryptedKey: encryptedKey, BillingKeyHash: tossBillingKeyHash(plainKey), Status: BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: key.Id, CustomerKey: key.CustomerKey,
		AuthTradeNo: "provider_attempt_fence_policy", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		NextChargeTime: now.Add(-time.Minute).Unix(), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	identity := walletAutoRechargeAttemptIdentity{PolicyID: policy.Id, CycleKey: fmt.Sprintf("s:%d", policy.NextChargeTime), Attempt: 0}
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	topUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		Money: TossUSDEquivalent(10000), Quota: TossCreditQuotaFromKRW(10000),
		TradeNo:            tossWalletOpaqueOrderIDPrefix + strings.Repeat("b", 40),
		ProviderCredential: providerCredential, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		CreateTime: now.Unix(), Status: common.TopUpStatusPending, ProviderAttempted: true, ProviderClaimTime: now.Unix(),
		WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
		WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		WalletAutoRechargeCreationToken: common.GetUUID(),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"last_trade_no": topUp.TradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "provider request in flight",
	}).Error)
	policy.LastTradeNo = topUp.TradeNo
	policy.LastError = walletAutoRechargeProviderPendingPrefix + "provider request in flight"
	return policy, topUp, plainKey
}

func setWalletAutoRechargeUnitPriceForTest(t *testing.T, unitPrice float64) {
	t.Helper()
	original := setting.TossUnitPrice
	setting.TossUnitPrice = unitPrice
	t.Cleanup(func() { setting.TossUnitPrice = original })
}

func TestWalletAutoRechargeRequestRejectsNonCanonicalProviderFingerprint(t *testing.T) {
	canonical := TossClientKeyFingerprint("live_ck_wallet_request_fingerprint")
	base := CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, CustomerKey: "cust_wallet_request",
		AuthTradeNo: "wallet_request_fingerprint", ProviderCredential: "encrypted-provider-secret",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
	}
	for _, malformed := range []string{
		strings.ToUpper(canonical),
		" " + canonical,
		canonical[:len(canonical)-1],
		strings.Repeat("z", len(canonical)),
	} {
		request := base
		request.ProviderClientKeyHash = malformed
		_, err := request.normalizeAndValidateForMode(false)
		require.Error(t, err, malformed)
	}
}

func TestExpireStaleUnattemptedWalletAutoRechargesReleasesOnlyPristineAuth(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := GetDBTimestamp()
	makePolicy := func(targetID int, status string) WalletAutoRecharge {
		activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, targetID, WalletAutoRechargeTypeScheduled)
		return WalletAutoRecharge{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: targetID,
			ActiveKey: &activeKey, OwnerUserId: 1, Amount: 10, Status: status,
			AuthTradeNo: fmt.Sprintf("wallet_auth_stale_%d", targetID), CustomerKey: "customer-key",
			ProviderCredential: "encrypted-secret", ProviderClientKeyHash: "client-key-fingerprint",
			IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
			CreateTime: now - 4000, UpdateTime: now - 4000,
		}
	}

	stale := makePolicy(101, WalletAutoRechargeStatusPending)
	fresh := makePolicy(102, WalletAutoRechargeStatusPending)
	fresh.CreateTime = now
	withIssueMarker := makePolicy(103, WalletAutoRechargeStatusPending)
	withIssueMarker.IssueAttempted = true
	withClaimMarker := makePolicy(104, WalletAutoRechargeStatusPending)
	withClaimMarker.IssueClaimTime = now - 5000
	withChargeMarker := makePolicy(105, WalletAutoRechargeStatusPending)
	withChargeMarker.LastTradeNo = "wallet_auto_105_existing"
	require.NoError(t, DB.Create(&[]WalletAutoRecharge{
		stale, fresh, withIssueMarker, withClaimMarker, withChargeMarker,
	}).Error)

	expired, err := ExpireStaleUnattemptedWalletAutoRecharges(now - 3000)
	require.NoError(t, err)
	require.Equal(t, int64(1), expired)

	var rows []WalletAutoRecharge
	require.NoError(t, DB.Order("target_id asc").Find(&rows).Error)
	require.Len(t, rows, 5)
	require.Equal(t, WalletAutoRechargeStatusCancelled, rows[0].Status)
	require.Nil(t, rows[0].ActiveKey)
	require.Empty(t, rows[0].ProviderCredential)
	require.Empty(t, rows[0].ProviderClientKeyHash)
	for i := 1; i < len(rows); i++ {
		require.Equal(t, WalletAutoRechargeStatusPending, rows[i].Status)
		require.NotNil(t, rows[i].ActiveKey)
	}
}

func TestCreatePendingWalletAutoRechargeRejectsDuplicateActiveType(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "duplicate-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "duplicate-owner"}).Error)
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
	require.NoError(t, DB.Create(&User{Id: 1, Username: "custom-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "custom-owner"}).Error)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{"TossTestMode": "true"}))

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetType:    TopUpTargetTypeUser,
		TargetId:      1,
		OwnerUserId:   1,
		Amount:        20000,
		AuthTradeNo:   "custom-interval-auth",
		IntervalUnit:  WalletAutoRechargeIntervalCustom,
		CustomSeconds: 3600,
	})

	require.NoError(t, err)
	require.Equal(t, 1, policy.IntervalValue)

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	require.Equal(t, 1, stored.IntervalValue)
}

func TestNextWalletChargeTimeMonthlyUsesNextFirstDayAnchor(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)

	tests := []struct {
		name string
		base time.Time
		want time.Time
	}{
		{
			name: "end of month schedules next month first day",
			base: time.Date(2026, 6, 30, 13, 45, 0, 0, loc),
			want: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "node-local first day still follows the UTC anchor",
			base: time.Date(2026, 7, 1, 0, 0, 0, 0, loc),
			want: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "after first day schedules following month first day",
			base: time.Date(2026, 7, 2, 10, 30, 0, 0, loc),
			want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nextWalletChargeTime(tc.base, WalletAutoRechargeIntervalMonth, 1, 0)

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestThresholdWalletAutoRechargeDailyLimitUsesCanonicalUTCDateAcrossProcessLocations(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	originalLocal := time.Local
	t.Cleanup(func() { time.Local = originalLocal })
	require.NoError(t, DB.Create(&User{Id: 1, Username: "timezone-limit-owner", Group: "default", AffCode: "timezone-limit-owner"}).Error)
	encryptedKey, err := common.EncryptString("timezone-limit-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 994, UserId: 1, CustomerKey: "timezone-limit-customer", EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 994, CustomerKey: "timezone-limit-customer", Amount: 10000,
		ThresholdQuota: walletAutoRechargeQuota(10000), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID}, nil
	}
	base := time.Unix(GetDBTimestamp(), 0).UTC()
	locations := []*time.Location{
		time.FixedZone("UTC+14-threshold-master", 14*60*60),
		time.FixedZone("UTC-12-threshold-master", -12*60*60),
	}
	for attempt := 0; attempt < 5; attempt++ {
		// Simulate masters whose process-local calendar dates disagree for the
		// exact same database-clock instant. The fourth and fifth invocations must
		// be stopped before a new TopUp reservation or provider POST.
		time.Local = locations[attempt%len(locations)]
		now := base.In(time.Local)
		require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now, WalletAutoRechargeDailyLimit, charger))
		require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("quota", 0).Error)
		// Keep every synthetic attempt in the same real DB hour. Production waits
		// for the cooldown; clearing it here isolates the daily counter contract.
		require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Update("cooldown_until", 0).Error)
	}

	require.Equal(t, WalletAutoRechargeDailyLimit, postCalls)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, reloaded.DailyChargeDateUTC)
	require.Equal(t, base.UTC().Format("2006-01-02"), reloaded.DailyChargeDate)
	require.Equal(t, WalletAutoRechargeDailyLimit, reloaded.DailyChargeCount)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(WalletAutoRechargeDailyLimit), topUpCount)
}

func TestThresholdWalletAutoRechargePreservesLegacyLocalDateLimitWhileRebasingToUTC(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "legacy-timezone-owner", Group: "default", AffCode: "legacy-timezone-owner"}).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	localDate := now.In(time.Local).Format("2006-01-02")
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 995, CustomerKey: "legacy-timezone-customer", Amount: 10000,
		ThresholdQuota: walletAutoRechargeQuota(10000), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		DailyChargeDate: localDate, DailyChargeCount: WalletAutoRechargeDailyLimit,
		LastChargeTime: now.Add(-30 * time.Minute).Unix(), DailyChargeDateUTC: false,
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now, WalletAutoRechargeDailyLimit, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, nil
	}))
	require.Zero(t, postCalls)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, reloaded.DailyChargeDateUTC)
	require.Equal(t, now.UTC().Format("2006-01-02"), reloaded.DailyChargeDate)
	require.Equal(t, WalletAutoRechargeDailyLimit, reloaded.DailyChargeCount)
}

func TestThresholdWalletAutoRechargeSafelyRebasesLegacyDailyCounterToUTC(t *testing.T) {
	originalLocal := time.Local
	time.Local = time.FixedZone("KST-daily-counter", 9*60*60)
	t.Cleanup(func() { time.Local = originalLocal })
	now := time.Date(2026, 7, 12, 0, 30, 0, 0, time.Local)
	utcMidnight := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		lastCharge    int64
		cooldownUntil int64
		wantCount     int
		wantCharge    bool
	}{
		{
			name:          "same UTC day preserves limit even during cooldown",
			lastCharge:    now.Add(-20 * time.Minute).Unix(),
			cooldownUntil: now.Add(time.Hour).Unix(),
			wantCount:     WalletAutoRechargeDailyLimit,
			wantCharge:    false,
		},
		{
			name:       "missing timestamp preserves uncertain count",
			lastCharge: 0,
			wantCount:  WalletAutoRechargeDailyLimit,
			wantCharge: false,
		},
		{
			name:       "future timestamp preserves uncertain count",
			lastCharge: now.Add(time.Hour).Unix(),
			wantCount:  WalletAutoRechargeDailyLimit,
			wantCharge: false,
		},
		{
			name:       "proven prior UTC day resets count",
			lastCharge: utcMidnight.Add(-time.Minute).Unix(),
			wantCount:  0,
			wantCharge: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			require.NoError(t, DB.Create(&User{
				Id: 1, Username: "utc-counter-owner", AffCode: "utc-counter-owner", Quota: 0,
			}).Error)
			policy := WalletAutoRecharge{
				Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
				TargetId: 1, OwnerUserId: 1, ThresholdQuota: 1,
				Status: WalletAutoRechargeStatusActive,
				// false marks a deployed process-local date whose timezone is no
				// longer knowable after a multi-master handoff.
				DailyChargeDate: now.Format("2006-01-02"), DailyChargeCount: WalletAutoRechargeDailyLimit,
				DailyChargeDateUTC: false, LastChargeTime: test.lastCharge, CooldownUntil: test.cooldownUntil,
			}
			require.NoError(t, DB.Create(&policy).Error)

			shouldCharge, err := walletThresholdShouldCharge(DB, &policy, now.UTC())
			require.NoError(t, err)
			require.Equal(t, test.wantCharge, shouldCharge)

			var stored WalletAutoRecharge
			require.NoError(t, DB.First(&stored, policy.Id).Error)
			require.True(t, stored.DailyChargeDateUTC)
			require.Equal(t, now.UTC().Format("2006-01-02"), stored.DailyChargeDate)
			require.Equal(t, test.wantCount, stored.DailyChargeCount)
		})
	}
}

func TestThresholdWalletAutoRechargeNegativeDailyCounterFailsClosed(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "negative-daily-counter-owner", AffCode: "negative-daily-counter-owner", Quota: 0,
	}).Error)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, ThresholdQuota: 1,
		Status: WalletAutoRechargeStatusActive, DailyChargeDate: now.AddDate(0, 0, -1).Format("2006-01-02"),
		DailyChargeCount: -1, DailyChargeDateUTC: true, LastChargeTime: now.Add(-25 * time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	shouldCharge, err := walletThresholdShouldCharge(DB, &policy, now)
	require.NoError(t, err)
	require.False(t, shouldCharge)

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	require.True(t, stored.DailyChargeDateUTC)
	require.Equal(t, WalletAutoRechargeDailyLimit, stored.DailyChargeCount)
}

func TestThresholdWalletAutoRechargeConflictingCanonicalDateFailsClosed(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "conflicting-canonical-date-owner", AffCode: "conflicting-canonical-date-owner", Quota: 0,
	}).Error)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, ThresholdQuota: 1, Status: WalletAutoRechargeStatusActive,
		DailyChargeDate:  now.AddDate(0, 0, -1).Format("2006-01-02"),
		DailyChargeCount: WalletAutoRechargeDailyLimit, DailyChargeDateUTC: true,
		LastChargeTime: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	shouldCharge, err := walletThresholdShouldCharge(DB, &policy, now)
	require.NoError(t, err)
	require.False(t, shouldCharge)

	var stored WalletAutoRecharge
	require.NoError(t, DB.First(&stored, policy.Id).Error)
	require.True(t, stored.DailyChargeDateUTC)
	require.Equal(t, now.Format("2006-01-02"), stored.DailyChargeDate)
	require.Equal(t, WalletAutoRechargeDailyLimit, stored.DailyChargeCount)

	negativeTimestamp := policy
	negativeTimestamp.Id = 0
	negativeTimestamp.AuthTradeNo = "negative-canonical-timestamp"
	negativeTimestamp.LastChargeTime = -1
	require.NoError(t, DB.Create(&negativeTimestamp).Error)
	shouldCharge, err = walletThresholdShouldCharge(DB, &negativeTimestamp, now)
	require.NoError(t, err)
	require.False(t, shouldCharge)
	stored = WalletAutoRecharge{}
	require.NoError(t, DB.First(&stored, negativeTimestamp.Id).Error)
	require.Equal(t, WalletAutoRechargeDailyLimit, stored.DailyChargeCount)

	for i, corruptDate := range []string{now.AddDate(0, 0, 1).Format("2006-01-02"), "not-a-date"} {
		corrupt := policy
		corrupt.Id = 0
		corrupt.AuthTradeNo = fmt.Sprintf("corrupt-canonical-date-%d", i)
		corrupt.DailyChargeDate = corruptDate
		corrupt.LastChargeTime = now.Add(-25 * time.Hour).Unix()
		require.NoError(t, DB.Create(&corrupt).Error)
		shouldCharge, err = walletThresholdShouldCharge(DB, &corrupt, now)
		require.NoError(t, err)
		require.False(t, shouldCharge)
		stored = WalletAutoRecharge{}
		require.NoError(t, DB.First(&stored, corrupt.Id).Error)
		require.Equal(t, WalletAutoRechargeDailyLimit, stored.DailyChargeCount)
	}
}

func TestActivateMonthlyWalletAutoRechargeIgnoresImmediateCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", AffCode: "monthly-owner"}).Error)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:           1,
		UserId:       1,
		CustomerKey:  "customer-monthly",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	}).Error)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:              WalletAutoRechargeTypeScheduled,
		TargetType:        TopUpTargetTypeUser,
		TargetId:          1,
		OwnerUserId:       1,
		CustomerKey:       "customer-monthly",
		AuthTradeNo:       "monthly-immediate-auth",
		Amount:            20000,
		IntervalUnit:      WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
	})
	require.NoError(t, err)
	originalNextChargeTime := policy.NextChargeTime

	called := false
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue("monthly-immediate-auth", "auth-monthly", "customer-monthly")
	require.NoError(t, err)
	require.True(t, claimed)
	activated, err := ActivateClaimedWalletAutoRechargeFromToss("monthly-immediate-auth", claimToken, 1, "card", "****1234", false, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		called = true
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID}, nil
	})

	require.NoError(t, err)
	require.False(t, called)
	require.Equal(t, WalletAutoRechargeStatusActive, activated.Status)
	require.Equal(t, originalNextChargeTime, activated.NextChargeTime)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, originalNextChargeTime, reloaded.NextChargeTime)
	require.Zero(t, reloaded.LastChargeTime)
}

func TestCreatePendingThresholdWalletAutoRechargeStoresThresholdQuotaDirectly(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "threshold-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "threshold-owner"}).Error)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        1,
		OwnerUserId:     1,
		CustomerKey:     "customer-quota-threshold",
		AuthTradeNo:     "quota-threshold-auth",
		Amount:          10000,
		ThresholdAmount: 999999,
		ThresholdQuota:  250000,
	})

	require.NoError(t, err)
	require.Equal(t, int64(250000), policy.ThresholdQuota)
	require.Equal(t, 999999.0, policy.ThresholdAmount)
}

func TestCancelWalletAutoRechargeQueuesBillingKeyRevocation(t *testing.T) {
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
	require.Equal(t, BillingKeyStatusPendingRevocation, billingKey.Status)

	var policy WalletAutoRecharge
	require.NoError(t, DB.First(&policy, 1).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
}

func TestCancelWalletAutoRechargeKeepsDuplicateProviderKeyUsedBySubscription(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "duplicate-key-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", AffCode: "duplicate-key-owner",
	}).Error)
	plainKey := "duplicate-provider-billing-key"
	encryptedKey, err := common.EncryptString(plainKey)
	require.NoError(t, err)
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	keyRows := []UserBillingKey{
		{Id: 301, UserId: 1, CustomerKey: "duplicate-customer", EncryptedKey: encryptedKey, BillingKeyHash: tossBillingKeyHash(plainKey), ProviderCredential: providerCredential, Status: BillingKeyStatusActive},
		{Id: 302, UserId: 1, CustomerKey: "duplicate-customer", EncryptedKey: encryptedKey, BillingKeyHash: tossBillingKeyHash(plainKey), ProviderCredential: providerCredential, Status: BillingKeyStatusActive},
	}
	require.NoError(t, DB.Create(&keyRows).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := &WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 301, Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(policy).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		UserId: 1, PlanId: 501, Status: "active", AutoRenew: true, BillingKeyId: 302,
		StartTime: GetDBTimestamp(), EndTime: GetDBTimestamp() + 86400, NextBillingTime: GetDBTimestamp() + 3600,
	}).Error)

	billingKeyID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Zero(t, billingKeyID)
	var keys []UserBillingKey
	require.NoError(t, DB.Where("id IN ?", []int{301, 302}).Order("id asc").Find(&keys).Error)
	require.Len(t, keys, 2)
	require.Equal(t, BillingKeyStatusActive, keys[0].Status)
	require.Equal(t, BillingKeyStatusActive, keys[1].Status)

	require.NoError(t, DB.Model(&UserSubscription{}).Where("billing_key_id = ?", 302).
		Updates(map[string]interface{}{"auto_renew": false, "next_billing_time": 0}).Error)
	billingKeyID, err = CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Equal(t, 301, billingKeyID)
	require.NoError(t, DB.Where("id IN ?", []int{301, 302}).Order("id asc").Find(&keys).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, keys[0].Status)
	require.Equal(t, BillingKeyStatusPendingRevocation, keys[1].Status)
}

func TestWalletActivationRejectsProviderKeyAlreadyPendingRevocation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "pending-key-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", AffCode: "pending-key-owner",
	}).Error)
	providerClientHash := TossBillingClientKeyFingerprint("wallet_auto_test_ck")
	keyID, err := StoreTossBillingKeyPendingRevocationWithProviderSnapshot(
		1, "pending-key-customer", "pending-provider-key", "card", "****1234",
		"wallet_auto_test_sk", providerClientHash,
	)
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "pending-key-customer", AuthTradeNo: "pending-key-wallet-issue", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "pending-key-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)

	activated, attachedKeyID, err := StoreAndActivateClaimedWalletAutoRechargeFromToss(
		policy.AuthTradeNo, claimToken, policy.CustomerKey, "pending-provider-key", "card", "****1234",
		"wallet_auto_test_sk", providerClientHash, false, stubWalletCharger(true, 10000, nil),
	)
	require.ErrorIs(t, err, ErrTossBillingKeyInactive)
	require.Nil(t, activated)
	require.Zero(t, attachedKeyID)
	var keyCount int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("billing_key_hash = ?", tossBillingKeyHash("pending-provider-key")).Count(&keyCount).Error)
	require.Equal(t, int64(1), keyCount)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusPending, storedPolicy.Status)
	require.Zero(t, storedPolicy.BillingKeyId)
}

func TestWalletActivationRejectsContractGateDisabledAfterProviderIssue(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "gate-off-activation-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", AffCode: "gate-off-activation-owner",
	}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "gate-off-activation-customer", AuthTradeNo: "gate-off-wallet-issue", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "gate-off-activation-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)

	// The provider ISSUE completed, but an administrator disabled the separate
	// non-subscription contract before the response could be attached locally.
	setting.TossWalletAutoRechargeEnabled = false
	activated, billingKeyID, err := StoreAndActivateClaimedWalletAutoRechargeFromToss(
		policy.AuthTradeNo, claimToken, policy.CustomerKey, "gate-off-provider-key", "card", "****9876",
		"wallet_auto_test_sk", TossBillingClientKeyFingerprint("wallet_auto_test_ck"), false, stubWalletCharger(true, 10000, nil),
	)
	require.ErrorIs(t, err, ErrTossBillingOperationallyDisabled)
	require.Nil(t, activated)
	require.Zero(t, billingKeyID)
	var keyCount int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Count(&keyCount).Error)
	require.Zero(t, keyCount)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusPending, storedPolicy.Status)
	require.Zero(t, storedPolicy.BillingKeyId)
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
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           11,
		UserId:       1,
		CustomerKey:  "customer-1",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

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
	require.Equal(t, int64(10*common.QuotaPerUnit), user.Quota)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "target_type = ? AND target_id = ?", TopUpTargetTypeUser, 1).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, PaymentProviderToss, topUp.PaymentProvider)
	require.Equal(t, int64(13000), topUp.Amount)
	require.Equal(t, float64(10), topUp.Money)
	require.NotNil(t, topUp.WalletAutoRechargeId)
	require.NotNil(t, topUp.WalletAutoRechargeCycleKey)
	require.NotNil(t, topUp.WalletAutoRechargeAttempt)
	require.Equal(t, policy.Id, *topUp.WalletAutoRechargeId)
	require.Equal(t, fmt.Sprintf("s:%d", policy.NextChargeTime), *topUp.WalletAutoRechargeCycleKey)
	require.Zero(t, *topUp.WalletAutoRechargeAttempt)
	require.Equal(t, tossWalletOrderIDVersionOpaque, topUp.WalletOrderIdVersion)
	require.True(t, validTossWalletOpaqueOrderID(topUp.TradeNo))
	require.NotEmpty(t, topUp.WalletAutoRechargeCreationToken)
}

func TestWalletAutoRechargeLegacyLogicalAttemptBackfillsAssociation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	identity := walletAutoRechargeAttemptIdentity{PolicyID: 41, CycleKey: "s:1782840000", Attempt: 0}
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	legacy := &TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		Amount: 10000, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(legacy).Error)
	var winner *TopUp
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		winner, err = findWalletAutoRechargeTopUpForIdentityTx(tx, identity)
		return err
	}))
	require.Equal(t, legacy.Id, winner.Id)
	require.NotNil(t, winner.WalletAutoRechargeId)
	require.NotNil(t, winner.WalletAutoRechargeCycleKey)
	require.NotNil(t, winner.WalletAutoRechargeAttempt)
	require.Equal(t, identity.PolicyID, *winner.WalletAutoRechargeId)
	require.Equal(t, identity.CycleKey, *winner.WalletAutoRechargeCycleKey)
	require.Equal(t, identity.Attempt, *winner.WalletAutoRechargeAttempt)
	require.Equal(t, tossWalletOrderIDVersionLegacy, winner.WalletOrderIdVersion)
}

func TestWalletAutoRechargeThresholdIdentityUsesUTCWithDeployedLocalReadAlias(t *testing.T) {
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	originalLocal := time.Local
	time.Local = seoul
	t.Cleanup(func() { time.Local = originalLocal })
	now := time.Date(2026, 7, 10, 0, 30, 0, 0, seoul)

	scheduled := WalletAutoRecharge{
		Id: 61, Type: WalletAutoRechargeTypeScheduled,
		NextChargeTime: 1783611000, FailCount: 0,
	}
	require.Equal(t, "wallet_auto_61_1783611000", walletAutoRechargeTradeNo(scheduled, now))
	scheduledIdentities, err := walletAutoRechargeAttemptIdentitiesForPolicy(&scheduled, now)
	require.NoError(t, err)
	require.Equal(t, []walletAutoRechargeAttemptIdentity{{PolicyID: 61, CycleKey: "s:1783611000", Attempt: 0}}, scheduledIdentities)
	parsedScheduled, ok := parseLegacyWalletAutoRechargeTradeNoForPolicyType("wallet_auto_61_1783611000", WalletAutoRechargeTypeScheduled)
	require.True(t, ok)
	require.Equal(t, scheduledIdentities[0], parsedScheduled)

	threshold := WalletAutoRecharge{
		Id: 62, Type: WalletAutoRechargeTypeThreshold,
		DailyChargeCount: 1, FailCount: 0,
	}
	require.Equal(t, "wallet_auto_62_2026070915_2", walletAutoRechargeTradeNo(threshold, now))
	thresholdIdentities, err := walletAutoRechargeAttemptIdentitiesForPolicy(&threshold, now)
	require.NoError(t, err)
	require.Len(t, thresholdIdentities, 2)
	require.Equal(t, walletAutoRechargeAttemptIdentity{PolicyID: 62, CycleKey: "t:2026070915:2", Attempt: 0}, thresholdIdentities[0])
	require.Equal(t, walletAutoRechargeAttemptIdentity{PolicyID: 62, CycleKey: "t:2026071000:2", Attempt: 0}, thresholdIdentities[1])
	parsedThreshold, ok := parseLegacyWalletAutoRechargeTradeNoForPolicyType("wallet_auto_62_2026070915_2", WalletAutoRechargeTypeThreshold)
	require.True(t, ok)
	require.Equal(t, thresholdIdentities[0], parsedThreshold)

	zeroCandidates, ok := walletAutoRechargeLegacyTradeNoCandidates(thresholdIdentities[0])
	require.True(t, ok)
	require.Equal(t, []string{"wallet_auto_62_2026070915_2", "wallet_auto_62_2026070915_2_0"}, zeroCandidates)
}

func TestEnsureWalletAutoRechargePendingTopUpReadsPhaseABridgeAliases(t *testing.T) {
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	originalLocal := time.Local
	time.Local = seoul
	t.Cleanup(func() { time.Local = originalLocal })
	now := time.Date(2026, 7, 10, 0, 30, 0, 0, seoul)

	tests := []struct {
		name                   string
		tradeNoForIdentities   func([]walletAutoRechargeAttemptIdentity) string
		associateIdentityIndex *int
	}{
		{
			name: "explicit attempt-zero suffix",
			tradeNoForIdentities: func(identities []walletAutoRechargeAttemptIdentity) string {
				tradeNo, ok := legacyWalletAutoRechargeTradeNo(identities[0])
				require.True(t, ok)
				return tradeNo + "_0"
			},
		},
		{
			name: "deployed process-local threshold tuple",
			tradeNoForIdentities: func(identities []walletAutoRechargeAttemptIdentity) string {
				tradeNo, ok := legacyWalletAutoRechargeTradeNo(identities[1])
				require.True(t, ok)
				return tradeNo
			},
			associateIdentityIndex: func() *int { value := 1; return &value }(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy := WalletAutoRecharge{
				Id: 71, Type: WalletAutoRechargeTypeThreshold,
				TargetType: TopUpTargetTypeUser, TargetId: 9, OwnerUserId: 9,
				DailyChargeCount: 1, FailCount: 0,
			}
			identities, err := walletAutoRechargeAttemptIdentitiesForPolicy(&policy, now)
			require.NoError(t, err)
			require.Len(t, identities, 2)
			credential, err := EncryptProviderCredential("wallet_auto_test_sk")
			require.NoError(t, err)
			legacy := &TopUp{
				UserId: 9, TargetType: TopUpTargetTypeUser, TargetId: 9,
				Amount: 10000, TradeNo: test.tradeNoForIdentities(identities),
				ProviderCredential: credential,
				PaymentMethod:      PaymentMethodToss, PaymentProvider: PaymentProviderToss,
				Status: common.TopUpStatusPending,
			}
			if test.associateIdentityIndex != nil {
				identity := identities[*test.associateIdentityIndex]
				legacy.WalletAutoRechargeId = &identity.PolicyID
				legacy.WalletAutoRechargeCycleKey = &identity.CycleKey
				legacy.WalletAutoRechargeAttempt = &identity.Attempt
				legacy.WalletOrderIdVersion = tossWalletOrderIDVersionLegacy
			}
			require.NoError(t, DB.Create(legacy).Error)
			primaryTradeNo, ok := legacyWalletAutoRechargeTradeNo(identities[0])
			require.True(t, ok)

			var winner *TopUp
			var preexisting, alreadyDone bool
			require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
				winner, preexisting, alreadyDone, err = ensureWalletAutoRechargePendingTopUp(
					tx, &policy, identities[0], identities, primaryTradeNo, 10000, getDBTimestampTx(tx), "wallet_auto_test_sk",
				)
				return err
			}))
			require.Equal(t, legacy.Id, winner.Id)
			require.Equal(t, legacy.TradeNo, winner.TradeNo)
			require.True(t, preexisting)
			require.False(t, alreadyDone)
			var count int64
			require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestWalletAutoRechargeAttemptZeroAliasesConflict(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	identity := walletAutoRechargeAttemptIdentity{PolicyID: 81, CycleKey: "s:1782840000", Attempt: 0}
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	rows := []TopUp{
		{UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, TradeNo: tradeNo,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending},
		{UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, TradeNo: tradeNo + "_0",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending},
	}
	require.NoError(t, DB.Create(&rows).Error)
	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := findWalletAutoRechargeTopUpForIdentityTx(tx, identity)
		return err
	}), ErrWalletAutoRechargeAttemptAssociationConflict)
}

func TestOpaqueWalletAutoRechargeWithoutAssociationFailsClosedWithoutBackfill(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	identity := walletAutoRechargeAttemptIdentity{
		PolicyID: 82,
		CycleKey: "s:1782840000",
		Attempt:  0,
	}
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	topUp := &TopUp{
		UserId:               1,
		TargetType:           TopUpTargetTypeUser,
		TargetId:             1,
		Amount:               10000,
		TradeNo:              tradeNo,
		PaymentMethod:        PaymentMethodToss,
		PaymentProvider:      PaymentProviderToss,
		Status:               common.TopUpStatusPending,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	}

	_, associated, err := topUpWalletAutoRechargeAttemptIdentity(topUp)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	require.False(t, associated)
	belongs, err := walletAutoRechargeTopUpBelongsToPolicy(topUp, &WalletAutoRecharge{
		Id: identity.PolicyID, TargetType: topUp.TargetType, TargetId: topUp.TargetId, OwnerUserId: topUp.UserId,
	})
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	require.False(t, belongs)
	require.NoError(t, DB.Create(topUp).Error)

	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := findWalletAutoRechargeTopUpForIdentityTx(tx, identity)
		return err
	}), ErrWalletAutoRechargeAttemptAssociationConflict)

	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Nil(t, persisted.WalletAutoRechargeId)
	require.Nil(t, persisted.WalletAutoRechargeCycleKey)
	require.Nil(t, persisted.WalletAutoRechargeAttempt)
	require.Equal(t, tossWalletOrderIDVersionOpaque, persisted.WalletOrderIdVersion)
}

func TestWalletAutoRechargePendingScannerDetectsLocalAndUTCBridgeConflict(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	now := time.Date(2026, 7, 10, 0, 30, 0, 0, seoul)
	policy := WalletAutoRecharge{
		Id: 91, Type: WalletAutoRechargeTypeThreshold,
		TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		DailyChargeCount: 1, FailCount: 0,
		LastError: walletAutoRechargeProviderPendingPrefix + "charge attempt recorded",
	}
	identities, err := walletAutoRechargeAttemptIdentitiesForPolicy(&policy, now)
	require.NoError(t, err)
	require.Len(t, identities, 2)
	rows := make([]TopUp, 0, len(identities))
	for _, identity := range identities {
		identity := identity
		tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
		require.True(t, ok)
		rows = append(rows, TopUp{
			UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
			Amount: 10000, TradeNo: tradeNo,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status:               common.TopUpStatusPending,
			WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
			WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionLegacy,
		})
	}
	require.NoError(t, DB.Create(&rows).Error)
	policy.LastTradeNo = rows[0].TradeNo

	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
		return err
	}), ErrWalletAutoRechargeAttemptAssociationConflict)
}

func TestResolveWalletAutoRechargeEffectiveAttemptRequiresContiguousTerminalChain(t *testing.T) {
	const cycleKey = "s:1782840000"
	makePolicy := func(failCount int) WalletAutoRecharge {
		return WalletAutoRecharge{
			Id: 101, Type: WalletAutoRechargeTypeScheduled,
			TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
			NextChargeTime: 1782840000, FailCount: failCount,
		}
	}
	makeTopUp := func(attempt int, status string) TopUp {
		identity := walletAutoRechargeAttemptIdentity{PolicyID: 101, CycleKey: cycleKey, Attempt: attempt}
		tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
		require.True(t, ok)
		return TopUp{
			UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
			Amount: 10000, TradeNo: tradeNo,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: status, CreateTime: GetDBTimestamp(),
			WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
			WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionLegacy,
		}
	}
	resolve := func(t *testing.T, policy *WalletAutoRecharge) (int, error) {
		t.Helper()
		cycleIdentity := walletAutoRechargeAttemptIdentity{PolicyID: policy.Id, CycleKey: cycleKey, Attempt: 0}
		var attempt int
		err := DB.Transaction(func(tx *gorm.DB) error {
			var err error
			attempt, err = resolveWalletAutoRechargeEffectiveAttemptTx(tx, policy, []walletAutoRechargeAttemptIdentity{cycleIdentity})
			return err
		})
		return attempt, err
	}

	t.Run("pre-provider failures keep HEAD attempt zero", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(2)
		attempt, err := resolve(t, &policy)
		require.NoError(t, err)
		require.Zero(t, attempt)
	})

	t.Run("failed base unlocks attempt one", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(1)
		base := makeTopUp(0, common.TopUpStatusFailed)
		require.NoError(t, DB.Create(&base).Error)
		attempt, err := resolve(t, &policy)
		require.NoError(t, err)
		require.Equal(t, 1, attempt)
	})

	t.Run("expired base unlocks attempt one", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(1)
		base := makeTopUp(0, common.TopUpStatusExpired)
		require.NoError(t, DB.Create(&base).Error)
		attempt, err := resolve(t, &policy)
		require.NoError(t, err)
		require.Equal(t, 1, attempt)
	})

	t.Run("provider payment evidence blocks retry", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(1)
		base := makeTopUp(0, common.TopUpStatusFailed)
		base.ProviderOrderId = "pay_wallet_terminal_evidence"
		require.NoError(t, DB.Create(&base).Error)
		_, err := resolve(t, &policy)
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	})

	t.Run("higher attempt without predecessors conflicts", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(2)
		row := makeTopUp(2, common.TopUpStatusPending)
		require.NoError(t, DB.Create(&row).Error)
		_, err := resolve(t, &policy)
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	})

	t.Run("live lower attempt beside higher attempt conflicts", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(1)
		rows := []TopUp{
			makeTopUp(0, common.TopUpStatusPending),
			makeTopUp(1, common.TopUpStatusPending),
		}
		require.NoError(t, DB.Create(&rows).Error)
		_, err := resolve(t, &policy)
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	})

	t.Run("terminal row without committed failure conflicts", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy := makePolicy(0)
		base := makeTopUp(0, common.TopUpStatusFailed)
		require.NoError(t, DB.Create(&base).Error)
		_, err := resolve(t, &policy)
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	})
}

func TestValidateWalletAutoRechargeChargeAttemptRechecksContiguousChainAfterPreparation(t *testing.T) {
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
			setupWalletAutoRechargeTestDB(t)
			setWalletAutoRechargeUnitPriceForTest(t, 1000)
			require.NoError(t, DB.Create(&User{
				Id: 1, Username: "wallet-final-chain-owner", AffCode: "wallet-final-chain-owner",
				Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
			}).Error)
			encryptedKey, err := common.EncryptString("wallet-final-chain-billing-key")
			require.NoError(t, err)
			require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
				Id: 61, UserId: 1, CustomerKey: "wallet-final-chain-customer",
				EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
			})).Error)

			now := time.Unix(GetDBTimestamp(), 0).In(time.Local)
			activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
			policy := WalletAutoRecharge{
				Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
				ActiveKey: &activeKey, OwnerUserId: 1, BillingKeyId: 61,
				CustomerKey: "wallet-final-chain-customer", Amount: 10000,
				IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
				Status: WalletAutoRechargeStatusActive, NextChargeTime: now.Add(-time.Minute).Unix(),
				FailCount: 1,
			}
			require.NoError(t, DB.Create(&policy).Error)

			baseIdentity := walletAutoRechargeAttemptIdentity{
				PolicyID: policy.Id, CycleKey: fmt.Sprintf("s:%d", policy.NextChargeTime), Attempt: 0,
			}
			baseTradeNo, ok := legacyWalletAutoRechargeTradeNo(baseIdentity)
			require.True(t, ok)
			base := TopUp{
				UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
				Amount: 10000, TradeNo: baseTradeNo,
				PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
				Status: common.TopUpStatusFailed, CreateTime: GetDBTimestamp(),
				WalletAutoRechargeId: &baseIdentity.PolicyID, WalletAutoRechargeCycleKey: &baseIdentity.CycleKey,
				WalletAutoRechargeAttempt: &baseIdentity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionLegacy,
			}
			base.ProviderOrderId = base.TradeNo + ":terminal"
			require.NoError(t, DB.Create(&base).Error)

			prepared, err := prepareWalletAutoRechargeCharge(policy.Id, now, 3)
			require.NoError(t, err)
			require.True(t, prepared.shouldCharge)
			require.False(t, prepared.alreadyDone)
			require.NotEqual(t, base.TradeNo, prepared.tradeNo)

			var suffix TopUp
			require.NoError(t, DB.Where("trade_no = ?", prepared.tradeNo).First(&suffix).Error)
			require.NotNil(t, suffix.WalletAutoRechargeAttempt)
			require.Equal(t, 1, *suffix.WalletAutoRechargeAttempt)
			require.False(t, suffix.ProviderAttempted)

			if mutation.deleteBase {
				require.NoError(t, DB.Delete(&TopUp{}, base.Id).Error)
			} else {
				require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", base.Id).
					Update("status", mutation.baseStatus).Error)
			}

			err = ValidateWalletAutoRechargeChargeAttempt(policy.Id, prepared.tradeNo)
			require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)

			var storedPolicy WalletAutoRecharge
			require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
			require.NotNil(t, storedPolicy.ActiveKey)
			require.True(t, strings.HasPrefix(storedPolicy.LastError, walletAutoRechargeManualReconciliationPrefix))

			require.NoError(t, DB.First(&suffix, suffix.Id).Error)
			require.Equal(t, common.TopUpStatusPending, suffix.Status)
			require.False(t, suffix.ProviderAttempted)
		})
	}
}

func TestProcessWalletAutoRechargeHaltsDuplicateLogicalAttemptWithoutPost(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "wallet-association-owner", AffCode: "wallet-association-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("wallet-association-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 51, UserId: 1, CustomerKey: "wallet-association-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Now().UTC().Truncate(time.Second)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		ActiveKey: &activeKey, OwnerUserId: 1, BillingKeyId: 51, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	identity, err := walletAutoRechargeAttemptIdentityForPolicy(&policy, now)
	require.NoError(t, err)
	legacyTradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	base := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		ProviderCredential: providerCredential,
		PaymentMethod:      PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: GetDBTimestamp(),
	}
	legacy := base
	legacy.TradeNo = legacyTradeNo
	require.NoError(t, DB.Create(&legacy).Error)
	opaque := base
	opaque.TradeNo = "wallet_auto_v2_duplicate_safe_id"
	opaque.WalletAutoRechargeId = &identity.PolicyID
	opaque.WalletAutoRechargeCycleKey = &identity.CycleKey
	opaque.WalletAutoRechargeAttempt = &identity.Attempt
	opaque.WalletOrderIdVersion = tossWalletOrderIDVersionOpaque
	require.NoError(t, DB.Create(&opaque).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("association conflict must not POST")
		},
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	var pendingCount int64
	require.NoError(t, DB.Model(&TopUp{}).Where("status = ?", common.TopUpStatusPending).Count(&pendingCount).Error)
	require.EqualValues(t, 2, pendingCount)
}

func TestProcessWalletAutoRechargeGlobalCorruptionDoesNotHaltHealthyPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "wallet-global-corruption-owner", AffCode: "wallet-global-corruption-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("wallet-global-corruption-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 52, UserId: 1, CustomerKey: "wallet-global-corruption-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Now().UTC().Truncate(time.Second)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		ActiveKey: &activeKey, OwnerUserId: 1, BillingKeyId: 52, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	// The advertised opaque protocol is incomplete and cannot be associated
	// with this policy. It must stop every new POST without sacrificing the
	// healthy policy that this worker happened to select.
	unrelatedPolicyID := policy.Id + 999
	require.NoError(t, DB.Create(&TopUp{
		UserId: 99, TargetType: TopUpTargetTypeUser, TargetId: 99,
		TradeNo: "opaqueHiddenWalletEvidence", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		WalletAutoRechargeId: &unrelatedPolicyID,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	}).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("global corruption must block POST")
		},
	)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.Zero(t, policy.FailCount)
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "no replacement wallet order may be created")
}

func TestWalletAutoRechargeNonzeroProtocolRequiresCompleteAssociation(t *testing.T) {
	for _, version := range []int{
		tossWalletOrderIDVersionLegacy,
		tossWalletOrderIDVersionOpaque,
		tossWalletOrderIDVersionOpaque + 17,
	} {
		topUp := &TopUp{
			TradeNo:              "wallet_auto_91_1782840000",
			PaymentMethod:        PaymentMethodToss,
			PaymentProvider:      PaymentProviderToss,
			WalletOrderIdVersion: version,
		}
		_, _, err := topUpWalletAutoRechargeAttemptIdentity(topUp)
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict, "version=%d", version)
	}
}

func TestWalletAutoRechargeFinalGateRejectsLocalUTCAliasInsertedAfterPreparation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalLocal := time.Local
	time.Local = time.FixedZone("KST-test", 9*60*60)
	t.Cleanup(func() { time.Local = originalLocal })

	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	createdAt := time.Date(2026, 7, 12, 0, 30, 0, 0, time.Local)
	localIdentity := walletAutoRechargeAttemptIdentity{
		PolicyID: policy.Id,
		CycleKey: "t:" + createdAt.Format("2006010215") + ":1",
		Attempt:  0,
	}
	localTradeNo, ok := legacyWalletAutoRechargeTradeNo(localIdentity)
	require.True(t, ok)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
		"trade_no":                       localTradeNo,
		"create_time":                    createdAt.Unix(),
		"wallet_auto_recharge_cycle_key": localIdentity.CycleKey,
	}).Error)
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"last_trade_no": localTradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "prepared local attempt",
	}).Error)

	utcIdentity := localIdentity
	utcIdentity.CycleKey = "t:" + createdAt.UTC().Format("2006010215") + ":1"
	require.NotEqual(t, localIdentity.CycleKey, utcIdentity.CycleKey)
	utcTradeNo, ok := legacyWalletAutoRechargeTradeNo(utcIdentity)
	require.True(t, ok)
	alias := topUp
	alias.Id = 0
	alias.TradeNo = utcTradeNo
	alias.CreateTime = createdAt.Unix()
	alias.WalletAutoRechargeCycleKey = &utcIdentity.CycleKey
	alias.WalletAutoRechargeCreationToken = common.GetUUID()
	require.NoError(t, DB.Create(&alias).Error)

	err := ValidateWalletAutoRechargeChargeAttempt(policy.Id, localTradeNo)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	var rows []TopUp
	require.NoError(t, DB.Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	for i := range rows {
		require.False(t, rows[i].ProviderAttempted)
	}
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.NotNil(t, storedPolicy.ActiveKey)
}

func TestLegacyWalletAliasConflictKeepsReplacementFenceThroughDelayedDONE(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "legacy-fence-owner", AffCode: "legacy-fence-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("legacy-fence-billing-key")
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 998, UserId: 1, CustomerKey: "legacy-fence-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)

	originalLocal := time.Local
	t.Cleanup(func() { time.Local = originalLocal })
	createdAt := time.Unix(GetDBTimestamp(), 0).UTC()
	creatorLocation := time.FixedZone("KST-legacy-fence", 9*60*60)
	recoveryLocation := time.FixedZone("PST-legacy-fence", -8*60*60)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, BillingKeyId: key.Id,
		CustomerKey: key.CustomerKey, ProviderClientKeyHash: key.ProviderClientKeyHash,
		AuthTradeNo: "wallet_auth_legacy_fence", Amount: 10000, ThresholdQuota: 1 << 30,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	identity := walletAutoRechargeAttemptIdentity{
		PolicyID: policy.Id,
		CycleKey: "t:" + createdAt.In(creatorLocation).Format("2006010215") + ":1",
		Attempt:  0,
	}
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	topUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		Amount: 10000, Money: TossUSDEquivalent(10000), Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderCredential: providerCredential,
		ProviderClientKeyHash: key.ProviderClientKeyHash,
		PaymentMethod:         PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		CreateTime: createdAt.Unix(), Status: common.TopUpStatusPending,
		ProviderAttempted: true, ProviderClaimTime: createdAt.Unix(),
		WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
		WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionLegacy,
		WalletAutoRechargeCreationToken: common.GetUUID(),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"last_trade_no": tradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "ambiguous legacy creator attempt",
	}).Error)

	SetWalletAutoRechargeTossPaymentLookup(func(context.Context, []string, string, int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})
	time.Local = recoveryLocation
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, createdAt.In(recoveryLocation), TossBillingMaxFails,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, ErrTossBillingChargePending
		})
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix))

	for _, replacementType := range []string{WalletAutoRechargeTypeThreshold, WalletAutoRechargeTypeScheduled} {
		req := CreateWalletAutoRechargeRequest{
			Type: replacementType, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
			CustomerKey: key.CustomerKey, AuthTradeNo: "replacement_" + replacementType,
			Amount: 10000, ThresholdQuota: 1 << 30,
			IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		}
		_, createErr := CreatePendingWalletAutoRecharge(req)
		require.Error(t, createErr, "replacement type=%s", replacementType)
	}

	for i := 0; i < 2; i++ {
		require.NoError(t, SettleWalletAutoRechargeWebhookDone(
			policy.Id, tradeNo, "pay_delayed_legacy_done", `{"status":"DONE"}`, createdAt.Add(time.Duration(i+1)*time.Minute),
		))
	}
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix))
	_, err = CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: key.CustomerKey, AuthTradeNo: "replacement_after_done",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.Error(t, err)
}

func TestCancelProviderAttemptKeepsFenceUntilDelayedDONESettlesOnce(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "cancel-payment-fence-owner", AffCode: "cancel-payment-fence-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("cancel-payment-fence-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 999, UserId: 1, CustomerKey: "cancel-payment-fence-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 999, CustomerKey: "cancel-payment-fence-customer",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		NextChargeTime: now.Add(-time.Minute).Unix(), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	identity := walletAutoRechargeAttemptIdentity{PolicyID: policy.Id, CycleKey: fmt.Sprintf("s:%d", policy.NextChargeTime), Attempt: 0}
	tradeNo := tossWalletOpaqueOrderIDPrefix + strings.Repeat("a", 40)
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	topUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		Money: TossUSDEquivalent(10000), Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo,
		ProviderCredential: providerCredential, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		CreateTime: now.Unix(), Status: common.TopUpStatusPending, ProviderAttempted: true, ProviderClaimTime: now.Unix(),
		WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
		WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		WalletAutoRechargeCreationToken: common.GetUUID(),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"last_trade_no": tradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "provider request in flight",
	}).Error)

	billingKeyID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Equal(t, policy.BillingKeyId, billingKeyID)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	var pendingKey UserBillingKey
	require.NoError(t, DB.First(&pendingKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, pendingKey.Status)
	_, err = CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "replacement-before-done", AuthTradeNo: "replacement_before_cancel_done",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.Error(t, err)

	for i := 0; i < 2; i++ {
		require.NoError(t, SettleWalletAutoRechargeWebhookDone(
			policy.Id, tradeNo, "pay_cancel_delayed_done", `{"status":"DONE"}`, now.Add(time.Duration(i+1)*time.Minute),
		))
	}
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)

	replacement, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "replacement-after-done", AuthTradeNo: "replacement_after_cancel_done",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, replacement.ActiveKey)
}

func TestLifecycleAndBillingDeletionKeepProviderAttemptReplacementFence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, policy WalletAutoRecharge, plainKey string)
	}{
		{
			name: "user billing lifecycle",
			mutate: func(t *testing.T, _ WalletAutoRecharge, _ string) {
				require.NoError(t, DeactivateTossBillingForUser(nil, 1))
			},
		},
		{
			name: "provider billing deletion",
			mutate: func(t *testing.T, _ WalletAutoRecharge, plainKey string) {
				revoked, err := RevokeTossBillingKeyByPlain("provider-attempt-fence-customer", plainKey)
				require.NoError(t, err)
				require.True(t, revoked)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, topUp, plainKey := seedProviderAttemptedWalletPolicy(t)
			test.mutate(t, policy, plainKey)

			require.NoError(t, DB.First(&policy, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelPending, policy.Status)
			require.NotNil(t, policy.ActiveKey)
			_, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
				Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
				CustomerKey: "lifecycle-replacement", AuthTradeNo: "lifecycle_replacement",
				Amount: 10000, ThresholdQuota: 1,
			})
			require.Error(t, err)

			for i := 0; i < 2; i++ {
				require.NoError(t, SettleWalletAutoRechargeWebhookDone(
					policy.Id, topUp.TradeNo, "pay_lifecycle_delayed_done", `{"status":"DONE"}`,
					time.Unix(GetDBTimestamp(), 0).Add(time.Duration(i)*time.Minute),
				))
			}
			var user User
			require.NoError(t, DB.First(&user, 1).Error)
			require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
			require.NoError(t, DB.First(&policy, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
			require.Nil(t, policy.ActiveKey)
		})
	}
}

func TestCancellationChecksCurrentPendingAttemptAfterPriorSuccessfulCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"last_charge_time": GetDBTimestamp() - 3600,
		"last_trade_no":    topUp.TradeNo,
		"last_error":       "",
	}).Error)

	billingKeyID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Equal(t, policy.BillingKeyId, billingKeyID)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.Equal(t, topUp.TradeNo, policy.LastTradeNo)
	require.True(t, strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix))
}

func TestMissingMarkedCurrentTopUpCannotAdvanceToNewProviderOrder(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	require.NoError(t, DB.Delete(&TopUp{}, topUp.Id).Error)
	postCalls := 0
	err := ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), TossBillingMaxFails,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("missing marked attempt must never POST a replacement")
		})
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix))
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCancelPendingAuthoritativeNoPaymentTerminalReleasesFence(t *testing.T) {
	for _, providerStatus := range []string{"ABORTED", "EXPIRED"} {
		t.Run(providerStatus, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
			_, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
			require.NoError(t, err)
			require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(
				policy.Id, topUp.TradeNo, providerStatus, fmt.Sprintf(`{"status":%q}`, providerStatus), false, time.Now(),
			))
			require.NoError(t, DB.First(&policy, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
			require.Nil(t, policy.ActiveKey)
			var storedTopUp TopUp
			require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
			if providerStatus == "EXPIRED" {
				require.Equal(t, common.TopUpStatusExpired, storedTopUp.Status)
			} else {
				require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
			}
			require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
		})
	}
}

func TestCancelledStaleNotFoundKeepsExactFenceUntilLateDONESettles(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
		"create_time":         GetDBTimestamp() - TossBillingOperationalGraceSeconds - 1,
		"provider_claim_time": GetDBTimestamp() - TossBillingOperationalGraceSeconds - 1,
	}).Error)
	_, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	SetWalletAutoRechargeTossPaymentLookup(func(context.Context, []string, string, int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), TossBillingMaxFails,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			return nil, errors.New("cancelled stale recovery must remain GET-only")
		})
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, strings.HasPrefix(policy.LastError, walletAutoRechargeLateDONEFencePrefix))

	for i := 0; i < 2; i++ {
		require.NoError(t, SettleWalletAutoRechargeWebhookDone(
			policy.Id, topUp.TradeNo, "pay_cancelled_stale_late_done", `{"status":"DONE"}`,
			time.Unix(GetDBTimestamp(), 0).Add(time.Duration(i)*time.Minute),
		))
	}
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
}

func TestRecordedStrictFullCancellationClosesUncreditedAttemptAfterApplyCrash(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "recorded-full-cancel-crash", EventType: TossPaymentEventTypeCancellation,
		OrderId: topUp.TradeNo, PaymentKey: "pay_recorded_full_cancel", Status: "CANCELED",
		OriginalAmount: topUp.Amount, CancelAmount: topUp.Amount, BalanceAmount: 0,
		ProviderPayload:      `{"status":"CANCELED","balanceAmount":0}`,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	charge, err := prepareWalletAutoRechargeCharge(policy.Id, time.Now(), TossBillingMaxFails)
	require.NoError(t, err)
	require.False(t, charge.shouldCharge)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.Nil(t, policy.ActiveKey)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
}

func TestDistinctPartialCancellationCannotBeMaskedByAnotherFullCancellation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, pendingTopUp, _ := seedProviderAttemptedWalletPolicy(t)
	priorTradeNo := fmt.Sprintf("wallet_auto_%d_prior_partial", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		TradeNo: priorTradeNo, ProviderOrderId: "pay_prior_partial",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CompleteTime: GetDBTimestamp(),
	}).Error)
	for _, event := range []TossPaymentEvent{
		{
			EventKey: "distinct-partial", EventType: TossPaymentEventTypeCancellation,
			OrderId: priorTradeNo, PaymentKey: "pay_prior_partial", Status: "PARTIAL_CANCELED",
			OriginalAmount: 10000, CancelAmount: 5000, BalanceAmount: 5000,
			ReconciliationStatus: TossReconciliationStatusRequired,
		},
		{
			EventKey: "distinct-full", EventType: TossPaymentEventTypeCancellation,
			OrderId: pendingTopUp.TradeNo, PaymentKey: "pay_distinct_full", Status: "CANCELED",
			OriginalAmount: pendingTopUp.Amount, CancelAmount: pendingTopUp.Amount, BalanceAmount: 0,
			ReconciliationStatus: TossReconciliationStatusRequired,
		},
	} {
		event := event
		require.NoError(t, DB.Create(&event).Error)
	}
	charge, err := prepareWalletAutoRechargeCharge(policy.Id, time.Now(), TossBillingMaxFails)
	require.NoError(t, err)
	require.True(t, charge.shouldCharge)
	require.True(t, charge.postForbidden)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&policy))
}

func TestLateCancellationOfOldSuccessPersistsMarkerBesideReplacement(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "late-old-cancel-owner", AffCode: "late-old-cancel-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	oldPolicy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		AuthTradeNo: "late_old_cancel_policy", Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusCancelled, LastChargeTime: GetDBTimestamp() - 60,
	}
	require.NoError(t, DB.Create(&oldPolicy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_late_cancel", oldPolicy.Id)
	require.NoError(t, DB.Model(&oldPolicy).Update("last_trade_no", tradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		TradeNo: tradeNo, ProviderOrderId: "pay_late_old_cancel",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CompleteTime: GetDBTimestamp(),
	}).Error)
	replacement := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		AuthTradeNo: "late_old_cancel_replacement", Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&replacement).Error)
	err := ApplyWalletAutoRechargeWebhookTerminal(
		oldPolicy.Id, tradeNo, "PARTIAL_CANCELED", `{"status":"PARTIAL_CANCELED","balanceAmount":5000}`, true, time.Now(),
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.NoError(t, DB.First(&oldPolicy, oldPolicy.Id).Error)
	require.Nil(t, oldPolicy.ActiveKey)
	require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&oldPolicy))
	require.ErrorIs(t, ensureNoOtherWalletAutoRechargePaymentFenceTx(
		DB, replacement.TargetType, replacement.TargetId, replacement.Id, "",
	), ErrWalletAutoRechargeReconciliationRequired)
	_, err = CancelWalletAutoRechargeAndGetBillingKey(oldPolicy.Id, oldPolicy.TargetType, oldPolicy.TargetId)
	require.NoError(t, err)
	require.NoError(t, DB.First(&oldPolicy, oldPolicy.Id).Error)
	require.Nil(t, oldPolicy.ActiveKey)
	require.Equal(t, WalletAutoRechargeStatusCancelled, oldPolicy.Status)
}

func TestAdminResolveWalletCancellationReleasesFenceAndRedeliveryIsSticky(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	event := TossPaymentEvent{
		EventKey: "admin-wallet-partial-release", EventType: TossPaymentEventTypeCancellation,
		OrderId: topUp.TradeNo, PaymentKey: "pay_admin_wallet_partial", Status: "PARTIAL_CANCELED",
		TransactionKey: "cancel_admin_wallet_partial", OriginalAmount: topUp.Amount,
		CancelAmount: topUp.Amount / 2, BalanceAmount: topUp.Amount / 2,
		ProviderPayload:      `{"status":"PARTIAL_CANCELED","balanceAmount":5000}`,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, err := RecordTossPaymentEvent(&event)
	require.NoError(t, err)
	require.True(t, created)
	err = ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id, topUp.TradeNo, "PARTIAL_CANCELED", event.ProviderPayload, true, time.Now(),
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)

	resolved, err := ResolveTossPaymentEventByAdmin(event.Id, 91, "partial cancellation reviewed and quota handled")
	require.NoError(t, err)
	require.Equal(t, TossReconciliationStatusResolved, resolved.ReconciliationStatus)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)

	// Exact terminal and DONE redeliveries remain non-crediting no-ops after
	// the admin decision; neither may recreate the policy/key fence.
	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id, topUp.TradeNo, "PARTIAL_CANCELED", event.ProviderPayload, true, time.Now(),
	))
	require.NoError(t, RecordWalletAutoRechargeProviderDONEEvidenceWithContext(context.Background(), policy.Id, topUp.TradeNo, &TossBillingChargeResult{
		Done: true, ProviderStatus: "DONE", Total: topUp.Amount, PaymentKey: "pay_late_after_admin",
		ProviderPayload: `{"status":"DONE"}`,
	}))
	require.NoError(t, SettleWalletAutoRechargeWebhookDone(
		policy.Id, topUp.TradeNo, "pay_late_after_admin", `{"status":"DONE"}`, time.Now(),
	))
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
	var owner User
	require.NoError(t, DB.First(&owner, policy.OwnerUserId).Error)
	require.Zero(t, owner.Quota)
}

func TestAdminResolveWalletEventsKeepsFenceUntilLastRequiredSibling(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	events := []TossPaymentEvent{
		{
			EventKey: "admin-wallet-sibling-one", EventType: TossPaymentEventTypeCancellation,
			OrderId: topUp.TradeNo, PaymentKey: "pay_admin_wallet_siblings", Status: "PARTIAL_CANCELED",
			TransactionKey: "cancel_admin_wallet_sibling_one", OriginalAmount: topUp.Amount,
			CancelAmount: 2000, BalanceAmount: 8000, ReconciliationStatus: TossReconciliationStatusRequired,
		},
		{
			EventKey: "admin-wallet-sibling-two", EventType: TossPaymentEventTypeCancellation,
			OrderId: topUp.TradeNo, PaymentKey: "pay_admin_wallet_siblings", Status: "PARTIAL_CANCELED",
			TransactionKey: "cancel_admin_wallet_sibling_two", OriginalAmount: topUp.Amount,
			CancelAmount: 3000, BalanceAmount: 5000, ReconciliationStatus: TossReconciliationStatusRequired,
		},
	}
	for i := range events {
		created, err := RecordTossPaymentEvent(&events[i])
		require.NoError(t, err)
		require.True(t, created)
	}
	_, err := ResolveTossPaymentEventByAdmin(events[0].Id, 92, "first cancellation line reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.NotNil(t, policy.ActiveKey)
	require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&policy))

	_, err = ResolveTossPaymentEventByAdmin(events[1].Id, 92, "last cancellation line reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)
}

func TestAdminResolveClosesOnlyPristineNeverAuthorizedOpaqueSibling(t *testing.T) {
	for _, test := range []struct {
		name              string
		makePristine      bool
		expectFenceRemain bool
	}{
		{name: "pristine opaque sibling closes", makePristine: true},
		{name: "provider attempted sibling remains fenced", expectFenceRemain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, sibling, _ := seedProviderAttemptedWalletPolicy(t)
			if test.makePristine {
				require.NoError(t, DB.Model(&sibling).Updates(map[string]interface{}{
					"provider_attempted":   false,
					"provider_claim_time":  0,
					"provider_claim_token": "",
				}).Error)
			}
			oldTradeNo := fmt.Sprintf("wallet_auto_%d_admin_old_success", policy.Id)
			require.NoError(t, DB.Create(&TopUp{
				UserId: policy.OwnerUserId, TargetType: policy.TargetType, TargetId: policy.TargetId,
				Amount: sibling.Amount, TradeNo: oldTradeNo, ProviderOrderId: "pay_admin_old_success",
				PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
				Status: common.TopUpStatusSuccess, CompleteTime: GetDBTimestamp(),
			}).Error)
			event := TossPaymentEvent{
				EventKey:  "admin-old-success-" + strings.ReplaceAll(test.name, " ", "_"),
				EventType: TossPaymentEventTypeCancellation, OrderId: oldTradeNo,
				PaymentKey: "pay_admin_old_success", Status: "PARTIAL_CANCELED",
				TransactionKey: "cancel_admin_old_success", OriginalAmount: sibling.Amount,
				CancelAmount: sibling.Amount / 2, BalanceAmount: sibling.Amount / 2,
				ReconciliationStatus: TossReconciliationStatusRequired,
			}
			created, err := RecordTossPaymentEvent(&event)
			require.NoError(t, err)
			require.True(t, created)
			_, err = ResolveTossPaymentEventByAdmin(event.Id, 98, "historical cancellation reviewed")
			require.NoError(t, err)

			require.NoError(t, DB.First(&sibling, sibling.Id).Error)
			require.NoError(t, DB.First(&policy, policy.Id).Error)
			if test.expectFenceRemain {
				require.Equal(t, common.TopUpStatusPending, sibling.Status)
				require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&policy))
				require.NotNil(t, policy.ActiveKey)
			} else {
				require.Equal(t, common.TopUpStatusFailed, sibling.Status)
				require.Equal(t, sibling.TradeNo+":terminal", sibling.ProviderOrderId)
				require.Nil(t, policy.ActiveKey)
				require.Empty(t, policy.LastError)
			}
		})
	}
}

func TestAdminResolveWalletEventTerminalizesPendingBeforeFenceRelease(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	event := TossPaymentEvent{
		EventKey: "admin-wallet-direct-before-apply", EventType: TossPaymentEventTypeFinancialMismatch,
		OrderId: topUp.TradeNo, PaymentKey: "pay_admin_direct", Status: "DONE",
		OriginalAmount: topUp.Amount + 1, BalanceAmount: topUp.Amount + 1,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	require.NoError(t, DB.Create(&event).Error)
	_, err := ResolveTossPaymentEventByAdmin(event.Id, 93, "provider amount reviewed before apply")
	require.NoError(t, err)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
}

func TestAdminResolveWalletEventDoesNotClearAssociationAliasFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	associationMessage := walletAutoRechargeManualReconciliationPrefix + ErrWalletAutoRechargeAttemptAssociationConflict.Error()
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"status": WalletAutoRechargeStatusFailed, "last_error": associationMessage,
	}).Error)
	event := TossPaymentEvent{
		EventKey: "admin-wallet-association-alias", EventType: TossPaymentEventTypeFinancialMismatch,
		OrderId: topUp.TradeNo, PaymentKey: "pay_admin_alias", Status: "DONE",
		OriginalAmount: topUp.Amount + 1, ReconciliationStatus: TossReconciliationStatusRequired,
	}
	require.NoError(t, DB.Create(&event).Error)
	_, err := ResolveTossPaymentEventByAdmin(event.Id, 94, "one physical order reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.NotNil(t, policy.ActiveKey)
	require.Equal(t, associationMessage, policy.LastError)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}

func TestFullCancellationAndAdminResolveCannotDowngradeAssociationAliasFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	associationMessage := walletAutoRechargeManualReconciliationPrefix + ErrWalletAutoRechargeAttemptAssociationConflict.Error()
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"status": WalletAutoRechargeStatusFailed, "last_error": associationMessage,
	}).Error)
	aliasTradeNo := fmt.Sprintf("wallet_auto_%d_possible_alias", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: policy.OwnerUserId, TargetType: policy.TargetType, TargetId: policy.TargetId,
		Amount: topUp.Amount, TradeNo: aliasTradeNo, ProviderOrderId: "pay_possible_alias",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CompleteTime: GetDBTimestamp(),
	}).Error)
	event := TossPaymentEvent{
		EventKey: "association-one-alias-full-cancel", EventType: TossPaymentEventTypeCancellation,
		OrderId: topUp.TradeNo, PaymentKey: "pay_one_alias_full_cancel", Status: "CANCELED",
		TransactionKey: "cancel_one_alias_full", OriginalAmount: topUp.Amount,
		CancelAmount: topUp.Amount, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, err := RecordTossPaymentEvent(&event)
	require.NoError(t, err)
	require.True(t, created)
	err = ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id, topUp.TradeNo, "CANCELED", `{"status":"CANCELED","balanceAmount":0}`, false, time.Now(),
	)
	require.NoError(t, err)
	_, err = ResolveTossPaymentEventByAdmin(event.Id, 96, "one canceled alias reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.NotNil(t, policy.ActiveKey)
	require.Equal(t, associationMessage, policy.LastError)

	thresholdKey := walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, WalletAutoRechargeTypeThreshold)
	replacement := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: policy.TargetType, TargetId: policy.TargetId,
		OwnerUserId: policy.OwnerUserId, AuthTradeNo: "association_alias_threshold_replacement",
		Amount: float64(topUp.Amount), ThresholdQuota: 1 << 30, Status: WalletAutoRechargeStatusActive, ActiveKey: &thresholdKey,
	}
	require.NoError(t, DB.Create(&replacement).Error)
	require.ErrorIs(t, ensureNoOtherWalletAutoRechargePaymentFenceTx(
		DB, replacement.TargetType, replacement.TargetId, replacement.Id, "",
	), ErrWalletAutoRechargeReconciliationRequired)
}

func TestResolvedWalletFinancialMismatchRedeliveryDoesNotReopenFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp, _ := seedProviderAttemptedWalletPolicy(t)
	result := &TossBillingChargeResult{
		Done: true, ProviderStatus: "DONE", Total: topUp.Amount + 1,
		BalanceAmount: topUp.Amount + 1, PaymentKey: "pay_admin_financial_sticky",
		ProviderPayload: `{"status":"DONE","totalAmount":10001}`,
	}
	err := MarkWalletAutoRechargeFinancialMismatch(policy.Id, topUp.TradeNo, result)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	var event TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", TossFinancialMismatchEventKey(
		topUp.TradeNo, result.PaymentKey, result.ProviderStatus, result.Total, result.BalanceAmount,
	)).First(&event).Error)
	_, err = ResolveTossPaymentEventByAdmin(event.Id, 95, "financial mismatch manually handled")
	require.NoError(t, err)
	require.NoError(t, MarkWalletAutoRechargeFinancialMismatch(policy.Id, topUp.TradeNo, result))
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Nil(t, policy.ActiveKey)
	require.Empty(t, policy.LastError)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)
}

func seedWalletEventReplacementTarget(t *testing.T, suffix string) (WalletAutoRecharge, WalletAutoRecharge, TopUp) {
	t.Helper()
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "event-target-" + suffix, AffCode: "event-target-" + suffix,
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	plainKey := "event-target-key-" + suffix
	encryptedKey, err := common.EncryptString(plainKey)
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 1201, UserId: 1, CustomerKey: "event-target-customer-" + suffix,
		EncryptedKey: encryptedKey, BillingKeyHash: tossBillingKeyHash(plainKey), Status: BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)
	oldPolicy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: key.Id, CustomerKey: key.CustomerKey,
		AuthTradeNo: "event_target_old_" + suffix, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusCancelled, LastChargeTime: GetDBTimestamp() - 60,
	}
	require.NoError(t, DB.Create(&oldPolicy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_event_%s", oldPolicy.Id, suffix)
	require.NoError(t, DB.Model(&oldPolicy).Update("last_trade_no", tradeNo).Error)
	oldPolicy.LastTradeNo = tradeNo
	topUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		Money: TossUSDEquivalent(10000), Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderOrderId: "pay_" + suffix,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CreateTime: GetDBTimestamp() - 120, CompleteTime: GetDBTimestamp() - 60,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	replacementKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	replacement := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: key.Id, CustomerKey: key.CustomerKey,
		AuthTradeNo: "event_target_replacement_" + suffix, Amount: 10000, ThresholdQuota: 1 << 30,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &replacementKey,
	}
	require.NoError(t, DB.Create(&replacement).Error)
	return oldPolicy, replacement, topUp
}

func TestWalletRequiredEventsBlockReplacementBeforeApply(t *testing.T) {
	for _, test := range []struct {
		name      string
		eventType string
		rawInsert bool
	}{
		{name: "public cancellation writer", eventType: TossPaymentEventTypeCancellation},
		{name: "public financial mismatch writer", eventType: TossPaymentEventTypeFinancialMismatch},
		{name: "pre-upgrade raw required event", eventType: TossPaymentEventTypeCancellation, rawInsert: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			oldPolicy, replacement, topUp := seedWalletEventReplacementTarget(t, strings.ReplaceAll(test.name, " ", "_"))
			event := TossPaymentEvent{
				EventKey: "target-event-" + strings.ReplaceAll(test.name, " ", "_"), EventType: test.eventType,
				OrderId: topUp.TradeNo, PaymentKey: topUp.ProviderOrderId,
				Status: "PARTIAL_CANCELED", TransactionKey: "cancel_target_event",
				OriginalAmount: topUp.Amount, CancelAmount: topUp.Amount / 2, BalanceAmount: topUp.Amount / 2,
				ReconciliationStatus: TossReconciliationStatusRequired,
			}
			if test.eventType == TossPaymentEventTypeFinancialMismatch {
				event.Status = "DONE"
				event.OriginalAmount++
			}
			if test.rawInsert {
				require.NoError(t, DB.Create(&event).Error)
			} else {
				created, err := RecordTossPaymentEvent(&event)
				require.NoError(t, err)
				require.True(t, created)
			}
			postCalls := 0
			err := ProcessWalletAutoRecharge(context.Background(), replacement.Id, time.Now(), TossBillingMaxFails,
				func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
					postCalls++
					return nil, errors.New("required old event must block replacement POST")
				})
			require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
			require.Zero(t, postCalls)
			if !test.rawInsert {
				require.NoError(t, DB.First(&oldPolicy, oldPolicy.Id).Error)
				require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&oldPolicy))
			}
		})
	}
}

func TestWalletCancellationBatchPersistsMissingLegacyOrderFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, _, _ := seedProviderAttemptedWalletPolicy(t)
	missingTradeNo := fmt.Sprintf("wallet_auto_%d_missing_restored_order", policy.Id)
	event := &TossPaymentEvent{
		EventKey: "wallet-batch-missing-legacy-order", EventType: TossPaymentEventTypeCancellation,
		OrderId: missingTradeNo, PaymentKey: "pay_missing_restored_order", Status: "PARTIAL_CANCELED",
		TransactionKey: "cancel_missing_restored_order", OriginalAmount: 10000,
		CancelAmount: 5000, BalanceAmount: 5000, ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, amount, err := RecordTossCancellationEventsForOrderWithContext(
		context.Background(), missingTradeNo, []*TossPaymentEvent{event},
	)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.EqualValues(t, 5000, amount)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.True(t, walletAutoRechargePolicyNeedsReplacementFence(&policy))
	require.True(t, walletAutoRechargeHasPermanentAssociationFence(policy.LastError))
	var stored TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", event.EventKey).First(&stored).Error)
	require.Equal(t, TossReconciliationStatusRequired, stored.ReconciliationStatus)
}

func TestWalletFinancialEventPersistsMissingLegacyOrderFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, _, _ := seedProviderAttemptedWalletPolicy(t)
	missingTradeNo := fmt.Sprintf("wallet_auto_%d_missing_financial_order", policy.Id)
	event := TossPaymentEvent{
		EventKey: "wallet-financial-missing-legacy-order", EventType: TossPaymentEventTypeFinancialMismatch,
		OrderId: missingTradeNo, PaymentKey: "pay_missing_financial_order", Status: "DONE",
		OriginalAmount: 10001, BalanceAmount: 10001, ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, err := RecordTossPaymentEvent(&event)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.True(t, walletAutoRechargeHasPermanentAssociationFence(policy.LastError))
	require.NotNil(t, policy.ActiveKey)

	_, err = ResolveTossPaymentEventByAdmin(event.Id, 97, "missing financial order reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.True(t, walletAutoRechargeHasPermanentAssociationFence(policy.LastError))
	require.NotNil(t, policy.ActiveKey)
}

func TestLegacyNilActiveKeyReconciliationMarkerStillBlocksCrossTypeReplacement(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "legacy-nil-fence-owner", AffCode: "legacy-nil-fence-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	legacy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, AuthTradeNo: "legacy_nil_active_key_fence", Amount: 10000,
		Status: WalletAutoRechargeStatusFailed, ActiveKey: nil,
		LastError: walletAutoRechargeReconciliationPendingPrefix + "provider cancellation retains balance",
	}
	require.NoError(t, DB.Create(&legacy).Error)
	_, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "legacy-nil-fence-replacement", AuthTradeNo: "legacy_nil_fence_replacement",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
}

func TestTargetWideFenceSerializesScheduledAndThresholdProviderPOSTs(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "cross-type-fence-owner", AffCode: "cross-type-fence-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("cross-type-fence-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 1000, UserId: 1, CustomerKey: "cross-type-fence-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	scheduledKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	thresholdKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	scheduled := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 1000, CustomerKey: "cross-type-fence-customer",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		AuthTradeNo: "cross_type_scheduled", NextChargeTime: now.Add(-time.Minute).Unix(),
		Status: WalletAutoRechargeStatusActive, ActiveKey: &scheduledKey,
	}
	threshold := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 1000, CustomerKey: "cross-type-fence-customer",
		AuthTradeNo: "cross_type_threshold", Amount: 10000, ThresholdQuota: 1 << 30,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &thresholdKey,
	}
	require.NoError(t, DB.Create(&scheduled).Error)
	require.NoError(t, DB.Create(&threshold).Error)

	scheduledPosts := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), scheduled.Id, now, TossBillingMaxFails,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			scheduledPosts++
			return nil, ErrTossBillingChargePending
		}))
	require.Equal(t, 1, scheduledPosts)
	require.NoError(t, DB.First(&scheduled, scheduled.Id).Error)
	require.True(t, strings.HasPrefix(scheduled.LastError, walletAutoRechargeProviderPendingPrefix))

	thresholdPosts := 0
	err = ProcessWalletAutoRecharge(context.Background(), threshold.Id, now, TossBillingMaxFails,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			thresholdPosts++
			return nil, errors.New("cross-type unresolved fence must block POST")
		})
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.Zero(t, thresholdPosts)

	var scheduledTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", scheduled.LastTradeNo).First(&scheduledTopUp).Error)
	require.NoError(t, SettleWalletAutoRechargeWebhookDone(
		scheduled.Id, scheduledTopUp.TradeNo, "pay_cross_type_scheduled", `{"status":"DONE"}`, now.Add(time.Minute),
	))
	require.NoError(t, ensureNoOtherWalletAutoRechargePaymentFenceTx(
		DB, threshold.TargetType, threshold.TargetId, threshold.Id, threshold.LastTradeNo,
	))
	thresholdNow := time.Unix(GetDBTimestamp(), 0).UTC()
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), threshold.Id, thresholdNow, TossBillingMaxFails,
		func(_ context.Context, _, _, _, orderID, _ string, amount int64) (*TossBillingChargeResult, error) {
			thresholdPosts++
			return &TossBillingChargeResult{
				Done: true, ProviderStatus: "DONE", Total: amount,
				PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`,
			}, nil
		}))
	require.Equal(t, 1, thresholdPosts)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(2*TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestDefinitiveWalletDeclineWritesTerminalMarkerAndDoesNotPoisonTargetFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "definitive-decline-owner", AffCode: "definitive-decline-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("definitive-decline-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 1001, UserId: 1, CustomerKey: "definitive-decline-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 1001, CustomerKey: "definitive-decline-customer",
		AuthTradeNo: "definitive_decline_policy", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		NextChargeTime: now.Add(-time.Minute).Unix(), Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1,
		func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
			return nil, errors.New("CARD_DECLINED")
		}))
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, policy.Status)
	require.Nil(t, policy.ActiveKey)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", policy.LastTradeNo).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)
	require.True(t, topUp.ProviderAttempted)

	replacement, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "definitive-decline-replacement", AuthTradeNo: "definitive_decline_replacement",
		Amount: 10000, ThresholdQuota: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, replacement.ActiveKey)
}

func TestWalletAutoRechargeThresholdLegacyCycleHourValidatesPlausibleCreatorTimezone(t *testing.T) {
	createdAt := time.Date(2026, 7, 12, 0, 30, 0, 0, time.UTC)
	tests := []struct {
		name      string
		cycleHour string
		want      bool
	}{
		{name: "UTC plus fourteen", cycleHour: "2026071214", want: true},
		{name: "UTC minus twelve", cycleHour: "2026071112", want: true},
		{name: "quarter hour timezone rounds into plausible hour", cycleHour: "2026071206", want: true},
		{name: "beyond positive timezone range", cycleHour: "2026071215", want: false},
		{name: "beyond negative timezone range", cycleHour: "2026071111", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := walletAutoRechargeAttemptIdentity{
				PolicyID: 1, CycleKey: "t:" + test.cycleHour + ":1", Attempt: 0,
			}
			require.Equal(t, test.want, walletAutoRechargeThresholdIdentityMatchesCreateTime(identity, createdAt))
		})
	}
}

func TestProcessWalletAutoRechargeRecoversOpaqueLocalCycleAcrossMasterTimezones(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	originalLocal := time.Local
	t.Cleanup(func() { time.Local = originalLocal })

	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "cross-timezone-recovery-owner", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "cross-timezone-recovery-owner",
	}).Error)
	encryptedKey, err := common.EncryptString("cross-timezone-recovery-key")
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 996, UserId: 1, CustomerKey: "cross-timezone-recovery-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)

	creatorLocation := time.FixedZone("creator-KST", 9*60*60)
	recoveryLocation := time.FixedZone("recovery-PST", -8*60*60)
	createdAt := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, BillingKeyId: key.Id,
		CustomerKey: key.CustomerKey, ProviderClientKeyHash: key.ProviderClientKeyHash,
		Amount: 10000, ThresholdQuota: 0, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	identity := walletAutoRechargeAttemptIdentity{
		PolicyID: policy.Id,
		CycleKey: "t:" + createdAt.In(creatorLocation).Format("2006010215") + ":1",
		Attempt:  0,
	}
	tradeNo := tossWalletOpaqueOrderIDPrefix + strings.Repeat("a", 40)
	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	topUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		Amount: 10000, Money: TossUSDEquivalent(10000), Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderCredential: providerCredential,
		ProviderClientKeyHash: key.ProviderClientKeyHash,
		PaymentMethod:         PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		CreateTime: createdAt.Unix(), Status: common.TopUpStatusPending,
		WalletAutoRechargeId: &identity.PolicyID, WalletAutoRechargeCycleKey: &identity.CycleKey,
		WalletAutoRechargeAttempt: &identity.Attempt, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		WalletAutoRechargeCreationToken: common.GetUUID(),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"last_trade_no": tradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "creator master pending attempt",
	}).Error)

	time.Local = recoveryLocation
	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, tradeNo, orderID)
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, createdAt.In(recoveryLocation), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			require.Equal(t, tradeNo, orderID)
			return &TossBillingChargeResult{
				Done: true, Total: amount, PaymentKey: "pay_cross_timezone_recovery",
				ProviderPayload: `{"status":"DONE"}`,
			}, nil
		})
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)

	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	require.True(t, storedPolicy.DailyChargeDateUTC)
	require.Equal(t, 1, storedPolicy.DailyChargeCount)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
}

func TestProcessWalletAutoRechargeChargesPresetKRWWithoutGroupOrDiscountAdjustment(t *testing.T) {
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
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           31,
		UserId:       1,
		CustomerKey:  "customer-31",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

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
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		chargedAmount = amount
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
	})

	require.NoError(t, err)
	require.Equal(t, int64(20000), chargedAmount)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(20*common.QuotaPerUnit), user.Quota)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "target_type = ? AND target_id = ?", TopUpTargetTypeUser, 1).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, int64(20000), topUp.Amount)
	require.Equal(t, float64(20), topUp.Money)
	require.Equal(t, "pay_"+topUp.TradeNo, topUp.ProviderOrderId)
	require.Equal(t, `{"status":"DONE"}`, topUp.ProviderPayload)
}

func TestProcessWalletAutoRechargeKeepsPendingTopUpWhenPostChargeCreditFails(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{
		Id:               1,
		Username:         "owner",
		Group:            "default",
		OrganizationId:   404,
		OrganizationRole: OrganizationRoleOwner,
		AffCode:          "wallet-auto-owner-reconcile",
	}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 404, Name: "reconcile-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           32,
		UserId:       1,
		CustomerKey:  "customer-32",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

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
	tradeNo := ""
	chargeErr := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		tradeNo = orderID
		require.NoError(t, DB.Delete(&Organization{}, 404).Error)
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
	})

	require.Error(t, chargeErr)
	require.Equal(t, 1, calls)

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
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestProcessThresholdWalletAutoRechargeReusesChargedTopUpAfterHourChanges(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{
		Id:               1,
		Username:         "owner",
		Group:            "default",
		OrganizationId:   504,
		OrganizationRole: OrganizationRoleOwner,
		AffCode:          "wallet-threshold-reconcile-owner",
	}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 504, Name: "threshold-reconcile-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           34,
		UserId:       1,
		CustomerKey:  "customer-34",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

	// Production derives the first-attempt clock from the database. Keep this
	// initial POST in the DB's current hour; the second call below is the part
	// that deliberately verifies recovery after the hour changes.
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	policy := WalletAutoRecharge{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        504,
		OwnerUserId:     1,
		BillingKeyId:    34,
		Amount:          10000,
		ThresholdAmount: 5000,
		ThresholdQuota:  walletAutoRechargeQuota(5000),
		Status:          WalletAutoRechargeStatusActive,
	}
	require.NoError(t, DB.Create(&policy).Error)

	staleCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(-time.Hour), 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		staleCalls++
		return nil, errors.New("stale threshold hour must not create a provider attempt")
	})
	require.NoError(t, err)
	require.Zero(t, staleCalls)
	var untouchedTopUps int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&untouchedTopUps).Error)
	require.Zero(t, untouchedTopUps)
	var untouchedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&untouchedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, untouchedPolicy.Status)
	require.Empty(t, untouchedPolicy.LastTradeNo)

	calls := 0
	var chargeErr error
	for attempt := 0; attempt < 2 && calls == 0; attempt++ {
		// If this test itself crosses the wall-clock hour, the first call correctly
		// no-ops. Refresh the DB clock and verify the next tick performs one POST.
		now = time.Unix(GetDBTimestamp(), 0).UTC()
		chargeErr = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			calls++
			require.NoError(t, DB.Delete(&Organization{}, 504).Error)
			return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
		})
	}
	require.Error(t, chargeErr)
	require.Equal(t, 1, calls, "charge error: %v", chargeErr)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	tradeNo := reloaded.LastTradeNo
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Contains(t, reloaded.LastError, "reconciliation")

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.Equal(t, "pay_"+tradeNo, topUp.ProviderOrderId)

	require.NoError(t, DB.Create(&Organization{Id: 504, Name: "threshold-reconcile-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return &TossBillingChargeResult{Done: true, ProviderStatus: "DONE", Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
	})
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(2*time.Hour), 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return nil, errors.New("unexpected new wallet charge")
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)

	var org Organization
	require.NoError(t, DB.First(&org, 504).Error)
	require.Equal(t, int64(10*common.QuotaPerUnit), org.Quota)
}

func TestProcessThresholdWalletAutoRechargeResumesReconciliationTopUpWithoutChargedMarkerAfterHourChanges(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{
		Id:               1,
		Username:         "owner",
		Group:            "default",
		OrganizationId:   505,
		OrganizationRole: OrganizationRoleOwner,
		AffCode:          "wallet-threshold-marker-owner",
	}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 505, Name: "threshold-marker-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           35,
		UserId:       1,
		CustomerKey:  "customer-35",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

	// Production derives the first-attempt clock from the database. Keep this
	// initial POST in the DB's current hour; the second call below is the part
	// that deliberately verifies recovery after the hour changes.
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	policy := WalletAutoRecharge{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        505,
		OwnerUserId:     1,
		BillingKeyId:    35,
		Amount:          10000,
		ThresholdAmount: 5000,
		ThresholdQuota:  walletAutoRechargeQuota(5000),
		Status:          WalletAutoRechargeStatusActive,
	}
	require.NoError(t, DB.Create(&policy).Error)

	calls := 0
	var chargeErr error
	for attempt := 0; attempt < 2 && calls == 0; attempt++ {
		now = time.Unix(GetDBTimestamp(), 0).UTC()
		chargeErr = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			calls++
			require.NoError(t, DB.Delete(&Organization{}, 505).Error)
			return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
		})
	}
	require.Error(t, chargeErr)
	require.Equal(t, 1, calls, "charge error: %v", chargeErr)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	tradeNo := reloaded.LastTradeNo
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", tradeNo).Update("provider_order_id", tradeNo).Error)
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Contains(t, reloaded.LastError, "reconciliation")

	require.NoError(t, DB.Create(&Organization{Id: 505, Name: "threshold-marker-org", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return &TossBillingChargeResult{Done: true, ProviderStatus: "DONE", Total: amount, PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE"}`}, nil
	})
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(2*time.Hour), 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return nil, errors.New("unexpected new wallet charge")
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "pay_"+tradeNo, topUp.ProviderOrderId)

	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)

	var org Organization
	require.NoError(t, DB.First(&org, 505).Error)
	require.Equal(t, int64(10*common.QuotaPerUnit), org.Quota)
}

func TestCompleteWalletAutoRechargeTopUpCreditsCancelledPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Group: "default", AffCode: "wallet-cancelled-reconcile-owner"}).Error)

	now := time.Date(2026, 6, 30, 11, 0, 0, 0, time.UTC)
	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusCancelled,
		NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_cancelled_charged", policy.Id)
	require.NoError(t, DB.Model(&policy).Update("last_trade_no", tradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          1,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        1,
		Amount:          10000,
		Money:           10,
		Quota:           TossCreditQuotaFromKRW(10000),
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo + ":charged",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		CreateTime:      now.Unix(),
		Status:          common.TopUpStatusPending,
	}).Error)

	err := completeWalletAutoRechargeTopUp(policy.Id, tradeNo, now)
	require.NoError(t, err)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, reloaded.Status)
	require.Nil(t, reloaded.ActiveKey)
}

func TestActivateCustomWalletAutoRechargeFromTossLeavesPolicyActiveWhenImmediateCreditNeedsReconciliation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossTestMode":             "true",
		"TossBillingTestClientKey": "wallet_auto_test_ck",
		"TossBillingTestSecretKey": "wallet_auto_test_sk",
	}))
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})
	require.NoError(t, DB.Create(&User{
		Id:               1,
		Username:         "owner",
		Group:            "default",
		AffCode:          "wallet-auto-activation-reconcile",
		OrganizationId:   405,
		OrganizationRole: OrganizationRoleOwner,
	}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 405, Name: "activation-reconcile", OwnerUserId: 1, Status: OrganizationStatusEnabled}).Error)

	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           33,
		UserId:       1,
		CustomerKey:  "customer-33",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:              WalletAutoRechargeTypeScheduled,
		TargetType:        TopUpTargetTypeOrganization,
		TargetId:          405,
		OwnerUserId:       1,
		CustomerKey:       "customer-33",
		AuthTradeNo:       "wallet-auto-activation-reconcile",
		Amount:            10000,
		IntervalUnit:      WalletAutoRechargeIntervalCustom,
		IntervalValue:     1,
		CustomSeconds:     60,
		ChargeImmediately: true,
	})
	require.NoError(t, err)

	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "auth-custom", "customer-33")
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = ActivateClaimedWalletAutoRechargeFromToss(policy.AuthTradeNo, claimToken, 33, "card", "****1234", false, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		require.NoError(t, DB.Delete(&Organization{}, 405).Error)
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "pay_" + orderID}, nil
	})

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
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           41,
		UserId:       1,
		CustomerKey:  "customer-41",
		EncryptedKey: enc,
		Status:       BillingKeyStatusActive,
	})).Error)

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

func TestWalletThresholdShouldChargeComparesStoredThresholdQuota(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "quota-threshold-user",
		Quota:    249999,
		AffCode:  "quota-threshold-user",
	}).Error)

	policy := &WalletAutoRecharge{
		Type:               WalletAutoRechargeTypeThreshold,
		TargetType:         TopUpTargetTypeUser,
		TargetId:           1,
		ThresholdAmount:    1,
		ThresholdQuota:     250000,
		Status:             WalletAutoRechargeStatusActive,
		DailyChargeDateUTC: true,
	}

	ok, err := walletThresholdShouldCharge(DB, policy, time.Now())

	require.NoError(t, err)
	require.True(t, ok)
}

func TestWalletAutoRechargeDoneMismatchNeverCreditsOrCreatesAnotherCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "mismatch-owner", Role: common.RoleCommonUser, AffCode: "mismatch-owner"}).Error)
	enc, err := common.EncryptString("billing-mismatch")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 91, UserId: 1, CustomerKey: "customer-mismatch", EncryptedKey: enc, Status: BillingKeyStatusActive,
	})).Error)

	now := time.Unix(GetDBTimestamp(), 0).UTC()
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 91, CustomerKey: "customer-mismatch", Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusActive, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	calls := 0
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return &TossBillingChargeResult{
			ProviderStatus: "DONE", Total: amount + 1, PaymentKey: "pay_invalid_" + orderID,
			ProviderPayload: `{"status":"DONE","totalAmount":10001}`,
		}, errors.New("strict DONE validation failed")
	}
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, charger)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.Equal(t, 1, calls)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)
	require.Contains(t, reloaded.LastError, "DONE payload mismatch")

	var key UserBillingKey
	require.NoError(t, DB.First(&key, 91).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", reloaded.LastTradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.Equal(t, topUp.TradeNo+":done-mismatch", topUp.ProviderOrderId)
	require.NotEqual(t, "pay_invalid_"+topUp.TradeNo, topUp.ProviderOrderId)

	var event TossPaymentEvent
	require.NoError(t, DB.First(&event, "order_id = ?", topUp.TradeNo).Error)
	require.Equal(t, "pay_invalid_"+topUp.TradeNo, event.PaymentKey)
	require.Equal(t, TossPaymentEventTypeFinancialMismatch, event.EventType)
	require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)

	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(time.Hour), 3, charger)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Zero(t, user.Quota)
}

func TestWalletAutoRechargeCancellationMismatchFreezesWithoutNewOrder(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  string
		balance int64
	}{
		{name: "partial cancellation validation mismatch", status: "PARTIAL_CANCELED", balance: 5000},
		{name: "full cancellation validation mismatch", status: "CANCELED", balance: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
			require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).
				Update("create_time", GetDBTimestamp()).Error)

			lookupCalls := 0
			SetWalletAutoRechargeTossPaymentLookup(func(context.Context, []string, string, int64) (*TossBillingChargeResult, error) {
				lookupCalls++
				return &TossBillingChargeResult{
					ProviderStatus: test.status,
					Total:          topUp.Amount,
					BalanceAmount:  test.balance,
					PaymentKey:     "pay_cancel_mismatch_" + test.status,
					ProviderPayload: fmt.Sprintf(
						`{"status":%q,"totalAmount":%d,"balanceAmount":%d}`,
						test.status, topUp.Amount, test.balance,
					),
				}, errors.New("strict cancellation validation failed")
			})
			postCalls := 0
			err := ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
				func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
					postCalls++
					return nil, errors.New("cancellation mismatch must not POST")
				},
			)
			require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
			require.Equal(t, 1, lookupCalls)
			require.Zero(t, postCalls)

			var storedPolicy WalletAutoRecharge
			require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
			require.NotNil(t, storedPolicy.ActiveKey)
			require.Zero(t, storedPolicy.FailCount)
			var storedTopUp TopUp
			require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
			require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
			require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
			var event TossPaymentEvent
			require.NoError(t, DB.Where("order_id = ? AND event_type = ?", topUp.TradeNo, TossPaymentEventTypeFinancialMismatch).First(&event).Error)
			require.Equal(t, test.status, event.Status)
			require.Equal(t, test.balance, event.BalanceAmount)
			require.Equal(t, "pay_cancel_mismatch_"+test.status, event.PaymentKey)
			require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)

			require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Hour), 3,
				func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
					postCalls++
					return nil, errors.New("terminal policy must not POST")
				},
			))
			require.Equal(t, 1, lookupCalls)
			require.Zero(t, postCalls)
			var count int64
			require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestSettleWalletAutoRechargeWebhookDoneIsAtomicAndIdempotent(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "webhook-owner", Role: common.RoleCommonUser, AffCode: "webhook-owner"}).Error)
	now := time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(), FailCount: 2,
		SettlementRetryTime: now.Add(time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_webhook_done", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Money: 10,
		Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_webhook_done", `{"status":"DONE"}`, now))
	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_webhook_done", `{"status":"DONE"}`, now.Add(time.Minute)))

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "pay_webhook_done", topUp.ProviderOrderId)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Equal(t, now.Unix(), reloaded.LastChargeTime)
	require.Zero(t, reloaded.FailCount)
	require.Empty(t, reloaded.LastError)
	require.Zero(t, reloaded.SettlementRetryTime)
	require.Greater(t, reloaded.NextChargeTime, policy.NextChargeTime)
	var event TossPaymentEvent
	require.NoError(t, DB.First(&event, "order_id = ? AND event_type = ?", tradeNo, TossPaymentEventTypeFulfillment).Error)
	require.Equal(t, "pay_webhook_done", event.PaymentKey)
	require.Equal(t, TossReconciliationStatusResolved, event.ReconciliationStatus)
}

func TestWalletAutoRechargeDONEEvidenceSurvivesQuotaCapacityFailure(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "capacity-owner", Role: common.RoleCommonUser,
		AffCode: "capacity-owner", Quota: common.MaxQuota - 5,
	}).Error)
	now := time.Date(2026, 7, 10, 2, 30, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_capacity_done", policy.Id)
	quota := TossCreditQuotaFromKRW(10000)
	require.Greater(t, quota, 5)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Money: 10, Quota: quota,
		TradeNo: tradeNo, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	evidence := &TossBillingChargeResult{
		Done: true, ProviderStatus: "DONE", Total: 10000, BalanceAmount: 10000,
		PaymentKey: "pay_capacity_done", ProviderPayload: `{"status":"DONE"}`,
	}
	require.NoError(t, RecordWalletAutoRechargeProviderDONEEvidenceWithContext(context.Background(), policy.Id, tradeNo, evidence))
	err := SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, evidence.PaymentKey, evidence.ProviderPayload, now)
	require.ErrorIs(t, err, ErrTopUpQuotaCapacityExceeded)

	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.Equal(t, evidence.PaymentKey, topUp.ProviderOrderId)
	var event TossPaymentEvent
	require.NoError(t, DB.First(&event, "order_id = ? AND event_type = ?", tradeNo, TossPaymentEventTypeFulfillment).Error)
	require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Contains(t, storedPolicy.LastError, walletAutoRechargeReconciliationPendingPrefix)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, common.MaxQuota-5, user.Quota)

	// Once capacity becomes available, the same recorded payment settles without
	// another provider charge and resolves the durable operator work item.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("quota", 0).Error)
	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, evidence.PaymentKey, evidence.ProviderPayload, now.Add(time.Minute)))
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(quota), user.Quota)
	require.NoError(t, DB.First(&event, "order_id = ? AND event_type = ?", tradeNo, TossPaymentEventTypeFulfillment).Error)
	require.Equal(t, TossReconciliationStatusResolved, event.ReconciliationStatus)
}

func TestSettleWalletAutoRechargeWebhookDoneRepairsPrecreditedPolicyOnly(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "repair-owner", Role: common.RoleCommonUser, AffCode: "repair-owner", Quota: 12345}).Error)
	now := time.Date(2026, 7, 10, 3, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, FailCount: 2,
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_precredited", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderOrderId: "pay_precredited", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusSuccess, CompleteTime: now.Add(-time.Minute).Unix(),
	}).Error)

	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_precredited", `{"status":"DONE"}`, now))
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(12345), user.Quota)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Equal(t, now.Unix(), reloaded.LastChargeTime)
	require.Zero(t, reloaded.FailCount)
	require.Equal(t, 1, reloaded.DailyChargeCount)
}

func TestSettleWalletAutoRechargeWebhookDonePropagatesAndRetriesEventResolution(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "resolve-owner", Role: common.RoleCommonUser, AffCode: "resolve-owner", Quota: 777}).Error)
	now := time.Date(2026, 7, 10, 3, 30, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_resolve_retry", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderOrderId: "pay_resolve_retry", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusSuccess, CompleteTime: now.Add(-time.Minute).Unix(),
	}).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "wallet-resolve-retry", EventType: TossPaymentEventTypeFulfillment, OrderId: tradeNo,
		PaymentKey: "pay_resolve_retry", Status: "DONE", OriginalAmount: 10000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	forcedErr := errors.New("forced fulfillment resolve failure")
	callbackName := "test:wallet_resolve_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "toss_payment_events" {
			tx.AddError(forcedErr)
		}
	}))
	err = SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_resolve_retry", `{"status":"DONE"}`, now)
	require.ErrorIs(t, err, forcedErr)
	var event TossPaymentEvent
	require.NoError(t, DB.First(&event, "event_key = ?", "wallet-resolve-retry").Error)
	require.Equal(t, TossReconciliationStatusRequired, event.ReconciliationStatus)
	var pendingResolution WalletAutoRecharge
	require.NoError(t, DB.First(&pendingResolution, policy.Id).Error)
	require.Contains(t, pendingResolution.LastError, "fulfillment event resolution")
	firstNextChargeTime := pendingResolution.NextChargeTime
	require.NoError(t, DB.Callback().Update().Remove(callbackName))

	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_resolve_retry", `{"status":"DONE"}`, now.Add(time.Minute)))
	require.NoError(t, DB.First(&event, "event_key = ?", "wallet-resolve-retry").Error)
	require.Equal(t, TossReconciliationStatusResolved, event.ReconciliationStatus)
	require.NoError(t, DB.First(&pendingResolution, policy.Id).Error)
	require.Empty(t, pendingResolution.LastError)
	require.Equal(t, firstNextChargeTime, pendingResolution.NextChargeTime)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(777), user.Quota)
}

func TestWalletAutoRechargeWebhookTerminalIsIdempotentAndAdvancesRetrySequence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "terminal-owner", Role: common.RoleCommonUser, AffCode: "terminal-owner"}).Error)
	now := time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := walletAutoRechargeTradeNo(policy, now)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "EXPIRED", `{"status":"EXPIRED"}`, false, now))
	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "EXPIRED", `{"status":"EXPIRED"}`, false, now.Add(time.Minute)))
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
	require.Equal(t, tradeNo+":terminal", topUp.ProviderOrderId)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, 1, reloaded.FailCount)
	require.Equal(t, WalletAutoRechargeStatusActive, reloaded.Status)
	require.NotEqual(t, tradeNo, walletAutoRechargeTradeNo(reloaded, now))
}

func TestWalletAutoRechargePartialCancellationStopsPolicyAndKey(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "partial-owner", Role: common.RoleCommonUser, AffCode: "partial-owner"}).Error)
	enc, err := common.EncryptString("partial-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{Id: 92, UserId: 1, CustomerKey: "partial-customer", EncryptedKey: enc, Status: BillingKeyStatusActive}).Error)
	now := time.Date(2026, 7, 10, 5, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 92, Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_partial", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	err = ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "PARTIAL_CANCELED", `{"status":"PARTIAL_CANCELED"}`, true, now)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)
	require.Contains(t, reloaded.LastError, "reconciliation")
	var key UserBillingKey
	require.NoError(t, DB.First(&key, 92).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Zero(t, topUp.CompleteTime)
}

func TestWalletAutoRechargeFullCancellationStopsPolicyAndPreventsNewCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "full-cancel-owner", Role: common.RoleCommonUser, AffCode: "full-cancel-owner"}).Error)
	enc, err := common.EncryptString("full-cancel-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{Id: 95, UserId: 1, CustomerKey: "full-cancel-customer", EncryptedKey: enc, Status: BillingKeyStatusActive}).Error)
	now := time.Date(2026, 7, 10, 5, 15, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 95, Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_full_cancel", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "CANCELED", `{"status":"CANCELED","balanceAmount":0}`, false, now))
	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "CANCELED", `{"status":"CANCELED","balanceAmount":0}`, false, now.Add(time.Minute)))

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
	require.Nil(t, reloaded.ActiveKey)
	require.Contains(t, reloaded.LastError, "provider payment canceled")
	var key UserBillingKey
	require.NoError(t, DB.First(&key, 95).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)

	postCalls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(2*time.Minute), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("fully canceled policy must not create a new charge")
	}))
	require.Zero(t, postCalls)
}

func TestWalletAutoRechargeTerminalRaceDisablesRecordedChargeSettlement(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "terminal-race-owner", Role: common.RoleCommonUser, AffCode: "terminal-race-owner"}).Error)
	enc, err := common.EncryptString("terminal-race-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{Id: 93, UserId: 1, CustomerKey: "terminal-race-customer", EncryptedKey: enc, Status: BillingKeyStatusActive}).Error)
	now := time.Date(2026, 7, 10, 5, 30, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 93, Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_terminal_race", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderOrderId: "pay_terminal_race", ProviderPayload: `{"paymentKey":"pay_terminal_race"}`,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	err = ApplyWalletAutoRechargeWebhookTerminal(policy.Id, tradeNo, "CANCELED", `{"paymentKey":"pay_terminal_race","status":"CANCELED"}`, true, now)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, tradeNo+":terminal", topUp.ProviderOrderId)
	calls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(time.Hour), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return nil, errors.New("terminal reconciliation must not settle or recharge")
	}))
	require.Zero(t, calls)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Zero(t, user.Quota)
}

func TestWalletAutoRechargeLookupPartialCancellationNeverCreatesRetryCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "lookup-partial-owner", Role: common.RoleCommonUser, AffCode: "lookup-partial-owner"}).Error)
	enc, err := common.EncryptString("lookup-partial-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{Id: 94, UserId: 1, CustomerKey: "lookup-partial-customer", EncryptedKey: enc, Status: BillingKeyStatusActive})).Error)
	// The final pre-POST gate uses the database clock. Keep this attempt inside
	// the operational grace window regardless of when the test suite is run.
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 94, CustomerKey: "lookup-partial-customer", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, Status: WalletAutoRechargeStatusActive,
		ActiveKey: &activeKey, NextChargeTime: now.Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	postCalls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, ErrTossBillingChargePending
	}))
	require.Equal(t, 1, postCalls)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return &TossBillingChargeResult{
			ProviderStatus: "PARTIAL_CANCELED", Total: amount, PaymentKey: "pay_lookup_partial",
			ProviderPayload: `{"paymentKey":"pay_lookup_partial","status":"PARTIAL_CANCELED","balanceAmount":3000}`,
		}, ErrTossBillingChargeCanceled
	})
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(time.Minute), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("partial cancellation must not POST again")
	})
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.Equal(t, 1, postCalls)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", reloaded.LastTradeNo).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", topUp.ProviderOrderId)
}

func TestStoreAndActivateClaimedWalletAutoRechargeAttachesKeyBeforeCancellation(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "atomic-owner", Role: common.RoleCommonUser, AffCode: "atomic-owner"}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "atomic-customer", AuthTradeNo: "atomic-wallet-issue", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "atomic-auth", "atomic-customer")
	require.NoError(t, err)
	require.True(t, claimed)

	activated, billingKeyID, err := StoreAndActivateClaimedWalletAutoRechargeFromToss(
		policy.AuthTradeNo, claimToken, "atomic-customer", "atomic-provider-key", "card", "****1111",
		"wallet_auto_test_sk", TossBillingClientKeyFingerprint("wallet_auto_test_ck"), false, stubWalletCharger(true, 10000, nil),
	)
	require.NoError(t, err)
	require.Equal(t, billingKeyID, activated.BillingKeyId)
	require.Greater(t, billingKeyID, 0)
	var active WalletAutoRecharge
	require.NoError(t, DB.First(&active, policy.Id).Error)
	require.Empty(t, active.ProviderCredential,
		"the policy no longer needs a provider secret after the key row is attached")
	require.NotEmpty(t, active.ProviderClientKeyHash,
		"an active policy retains its non-secret MID fingerprint for future TopUp recovery")

	latestID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, TopUpTargetTypeUser, 1)
	require.NoError(t, err)
	require.Equal(t, billingKeyID, latestID)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, billingKeyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, reloaded.Status)
	require.Equal(t, billingKeyID, reloaded.BillingKeyId)
	require.Empty(t, reloaded.ProviderCredential)
	require.Empty(t, reloaded.ProviderClientKeyHash,
		"a terminal policy no longer needs its ISSUE secret or MID snapshot")
}

func TestStoreAndActivateClaimedWalletAutoRechargeRejectsExpiredAuthorizationInTransaction(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "expired-wallet-issue-owner", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "expired-wallet-issue-owner",
	}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "expired-wallet-customer", AuthTradeNo: "expired-wallet-issue", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "expired-wallet-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).
		Update("create_time", GetDBTimestamp()-TossSubscriptionPurchaseReservationMaxAgeSeconds-1).Error)

	activated, billingKeyID, err := StoreAndActivateClaimedWalletAutoRechargeFromToss(
		policy.AuthTradeNo, claimToken, policy.CustomerKey, "must-not-attach", "card", "****4444",
		"wallet_auto_test_sk", TossBillingClientKeyFingerprint("wallet_auto_test_ck"), false, stubWalletCharger(true, 10000, nil),
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeIssueAuthorizationExpired)
	require.Nil(t, activated)
	require.Zero(t, billingKeyID)
	var keyCount int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Count(&keyCount).Error)
	require.Zero(t, keyCount, "the stale provider key must not be stored before controller cleanup")
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusPending, reloaded.Status)
	require.Zero(t, reloaded.BillingKeyId)
	require.Equal(t, claimToken, reloaded.IssueClaimToken)
}

func TestProcessWalletAutoRechargeLegacyChargedMarkerSettlesWithoutProviderCall(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "legacy-owner", Role: common.RoleCommonUser, AffCode: "legacy-owner"}).Error)
	now := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Unix(),
		LastError: walletAutoRechargeReconciliationPendingPrefix + "legacy charged marker",
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_legacy_charged", policy.Id)
	require.NoError(t, DB.Model(&policy).Update("last_trade_no", tradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000, Quota: TossCreditQuotaFromKRW(10000),
		TradeNo: tradeNo, ProviderOrderId: tradeNo + ":charged", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	calls := 0
	err := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		calls++
		return nil, errors.New("legacy settlement must not call Toss")
	})
	require.NoError(t, err)
	require.Zero(t, calls)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	var markerEvent TossPaymentEvent
	require.NoError(t, DB.Where("order_id = ? AND event_type = ?", tradeNo, TossPaymentEventTypeFulfillment).First(&markerEvent).Error)
	require.Equal(t, tradeNo+":charged", markerEvent.PaymentKey)
	require.Equal(t, TossReconciliationStatusResolved, markerEvent.ReconciliationStatus)

	actualPaymentKey := "pay_" + tradeNo
	require.NoError(t, SettleWalletAutoRechargeWebhookDoneWithContext(
		context.Background(),
		policy.Id,
		tradeNo,
		actualPaymentKey,
		`{"status":"DONE","totalAmount":10000}`,
		now.Add(time.Minute),
	))
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	require.Equal(t, actualPaymentKey, topUp.ProviderOrderId)
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	var fulfillmentEvents []TossPaymentEvent
	require.NoError(t, DB.Where("order_id = ? AND event_type = ?", tradeNo, TossPaymentEventTypeFulfillment).Find(&fulfillmentEvents).Error)
	require.Len(t, fulfillmentEvents, 2)
	for i := range fulfillmentEvents {
		require.Equal(t, TossReconciliationStatusResolved, fulfillmentEvents[i].ReconciliationStatus)
	}
}

func TestStoreAndActivateClaimedWalletAutoRechargeRevalidatesDisabledTargetInTransaction(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "disabled-target", Role: common.RoleCommonUser, AffCode: "disabled-target"}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "disabled-customer", AuthTradeNo: "disabled-target-trade", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "disabled-auth", "disabled-customer")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("status", common.UserStatusDisabled).Error)

	_, billingKeyID, err := StoreAndActivateClaimedWalletAutoRechargeFromToss(
		policy.AuthTradeNo, claimToken, "disabled-customer", "must-not-persist", "card", "****3333",
		"wallet_auto_test_sk", TossBillingClientKeyFingerprint("wallet_auto_test_ck"), false, stubWalletCharger(true, 10000, nil),
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeTargetInvalid)
	require.Zero(t, billingKeyID)
	var keyCount int64
	require.NoError(t, DB.Model(&UserBillingKey{}).Count(&keyCount).Error)
	require.Zero(t, keyCount)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusPending, reloaded.Status)
	require.Zero(t, reloaded.BillingKeyId)
}

func TestClaimWalletAutoRechargeBillingIssueAllowsOnlyOneLiveOwner(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "claim-owner", Role: common.RoleCommonUser, AffCode: "claim-owner"}).Error)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "claim-customer", AuthTradeNo: "claim-trade", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	firstToken, firstClaimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "same-auth", "claim-customer")
	require.NoError(t, err)
	require.True(t, firstClaimed)
	require.NotEmpty(t, firstToken)
	secondToken, secondClaimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "same-auth", "claim-customer")
	require.NoError(t, err)
	require.False(t, secondClaimed)
	require.Empty(t, secondToken)
	conflictToken, conflictClaimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "different-auth", "claim-customer")
	require.ErrorIs(t, err, ErrWalletAutoRechargeIssueConflict)
	require.False(t, conflictClaimed)
	require.Empty(t, conflictToken)
}

func TestClaimWalletAutoRechargeBillingIssuePreservesProviderAttemptEvidence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "claim-attempt-owner", Role: common.RoleCommonUser, AffCode: "claim-attempt-owner"}).Error)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "claim-attempt-customer", AuthTradeNo: "claim-attempt-trade", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)

	firstToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(
		policy.AuthTradeNo,
		"same-auth",
		"claim-attempt-customer",
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, firstToken))
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).
		Where("id = ?", policy.Id).
		Update("issue_claim_time", GetDBTimestamp()-walletAutoRechargeIssueClaimTTLSeconds-1).Error)

	secondToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(
		policy.AuthTradeNo,
		"same-auth",
		"claim-attempt-customer",
	)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotEmpty(t, secondToken)
	require.NotEqual(t, firstToken, secondToken)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, reloaded.IssueAttempted,
		"a repeated success callback must not erase evidence of a prior provider call")
}

func TestThresholdWalletAutoRechargeCrashBeforeProviderPostReusesDurableOrder(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "threshold-crash-owner",
		Group:    "default",
		AffCode:  "threshold-crash-owner",
	}).Error)

	encryptedBillingKey, err := common.EncryptString("threshold-crash-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           95,
		UserId:       1,
		CustomerKey:  "threshold-crash-customer",
		EncryptedKey: encryptedBillingKey,
		Status:       BillingKeyStatusActive,
	})).Error)

	now := time.Unix(GetDBTimestamp(), 0).In(time.Local)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type:                  WalletAutoRechargeTypeThreshold,
		TargetType:            TopUpTargetTypeUser,
		TargetId:              1,
		OwnerUserId:           1,
		BillingKeyId:          95,
		CustomerKey:           "threshold-crash-customer",
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
		Amount:                10000,
		ThresholdAmount:       5000,
		ThresholdQuota:        walletAutoRechargeQuota(5000),
		Status:                WalletAutoRechargeStatusActive,
		ActiveKey:             &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	// Simulate a process crash after the DB transaction durably records the
	// attempt, but before the provider POST is made.
	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, now, 3)
	require.NoError(t, err)
	require.True(t, prepared.shouldCharge)
	require.False(t, prepared.alreadyDone)
	require.False(t, prepared.lookupPending)
	require.NotEmpty(t, prepared.tradeNo)

	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, prepared.tradeNo, storedPolicy.LastTradeNo)
	require.True(t, strings.HasPrefix(storedPolicy.LastError, walletAutoRechargeProviderPendingPrefix))

	var pending TopUp
	require.NoError(t, DB.First(&pending, "trade_no = ?", prepared.tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, pending.Status)
	require.Equal(t, int64(10000), pending.Amount)

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, prepared.tradeNo, orderID)
		require.Equal(t, int64(10000), amount)
		require.NotEmpty(t, secretKeys)
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(2*time.Hour), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		require.Equal(t, "threshold-crash-billing-key", billingKey)
		require.Equal(t, "threshold-crash-customer", customerKey)
		require.Equal(t, "wallet_auto_test_sk", secretKey)
		require.Equal(t, prepared.tradeNo, orderID)
		require.Equal(t, int64(10000), amount)
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_" + orderID,
			ProviderPayload: `{"paymentKey":"pay_recovered","status":"DONE"}`,
		}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)

	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)
	require.NoError(t, DB.First(&pending, "trade_no = ?", prepared.tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, pending.Status)
	require.Equal(t, "pay_"+prepared.tradeNo, pending.ProviderOrderId)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)

	// The credited balance is now above the threshold. A later worker pass must
	// neither create a second order nor credit the first one again.
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(3*time.Hour), 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("completed durable attempt must not POST again")
	}))
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func seedStaleWalletAutoRechargeAttempt(t *testing.T) (WalletAutoRecharge, TopUp) {
	t.Helper()
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "stale-wallet-owner",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "stale-wallet-owner",
	}).Error)

	encryptedBillingKey, err := common.EncryptString("stale-wallet-billing-key")
	require.NoError(t, err)
	key := withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           501,
		UserId:       1,
		CustomerKey:  "stale-wallet-customer",
		EncryptedKey: encryptedBillingKey,
		Status:       BillingKeyStatusActive,
	})
	require.NoError(t, DB.Create(key).Error)

	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type:                  WalletAutoRechargeTypeThreshold,
		TargetType:            TopUpTargetTypeUser,
		TargetId:              1,
		OwnerUserId:           1,
		BillingKeyId:          key.Id,
		CustomerKey:           key.CustomerKey,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
		Amount:                10000,
		ThresholdAmount:       1000,
		ThresholdQuota:        0,
		Status:                WalletAutoRechargeStatusActive,
		ActiveKey:             &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	identityTime := time.Unix(GetDBTimestamp(), 0).In(time.Local)
	identities, err := walletAutoRechargeAttemptIdentitiesForPolicyAttempt(&policy, identityTime, 0)
	require.NoError(t, err)
	// Seed the deployed v1 process-local writer. New v2 rows use the UTC
	// primary, but v1 validation deliberately keeps the old exact orderId.
	identity := identities[len(identities)-1]
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
		"last_trade_no": tradeNo,
		"last_error":    walletAutoRechargeProviderPendingPrefix + "stale durable attempt",
	}).Error)
	policy.LastTradeNo = tradeNo
	policy.LastError = walletAutoRechargeProviderPendingPrefix + "stale durable attempt"

	providerCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	topUp := TopUp{
		UserId:                          1,
		TargetType:                      TopUpTargetTypeUser,
		TargetId:                        1,
		Amount:                          10000,
		Money:                           TossUSDEquivalent(10000),
		Quota:                           TossCreditQuotaFromKRW(10000),
		TradeNo:                         tradeNo,
		ProviderCredential:              providerCredential,
		ProviderClientKeyHash:           policy.ProviderClientKeyHash,
		PaymentMethod:                   PaymentMethodToss,
		PaymentProvider:                 PaymentProviderToss,
		CreateTime:                      identityTime.Unix() - TossBillingOperationalGraceSeconds - 1,
		Status:                          common.TopUpStatusPending,
		WalletAutoRechargeId:            &identity.PolicyID,
		WalletAutoRechargeCycleKey:      &identity.CycleKey,
		WalletAutoRechargeAttempt:       &identity.Attempt,
		WalletOrderIdVersion:            tossWalletOrderIDVersionLegacy,
		WalletAutoRechargeCreationToken: common.GetUUID(),
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return policy, topUp
}

func TestStaleWalletAutoRechargeNotFoundNeverPostsAndFailsClosed(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, topUp.TradeNo, orderID)
		require.Equal(t, topUp.Amount, amount)
		require.NotEmpty(t, secretKeys)
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("stale durable attempt must never POST")
	}
	err := ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, charger)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptOutsideGrace)
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, storedTopUp.Status)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.NotNil(t, storedPolicy.ActiveKey)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)

	// The terminal policy cannot manufacture a replacement order on a later run.
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Hour), 3, charger))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)
}

func TestLateProviderDoneSettlesExpiredStaleWalletAttemptExactlyOnce(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		return nil, errors.New("stale durable attempt must never POST")
	}
	require.ErrorIs(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, charger), ErrWalletAutoRechargeAttemptOutsideGrace)
	_, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "stale-replacement-before-done", AuthTradeNo: "stale_replacement_before_done",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.Error(t, err)

	now := time.Unix(GetDBTimestamp(), 0)
	for i := 0; i < 2; i++ {
		require.NoError(t, SettleWalletAutoRechargeWebhookDone(
			policy.Id,
			topUp.TradeNo,
			"pay_late_stale_wallet_done",
			`{"paymentKey":"pay_late_stale_wallet_done","status":"DONE","totalAmount":10000}`,
			now,
		))
	}

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
	require.Equal(t, "pay_late_stale_wallet_done", storedTopUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.Nil(t, storedPolicy.ActiveKey)
	require.Empty(t, storedPolicy.LastError)
	replacement, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "stale-replacement-after-done", AuthTradeNo: "stale_replacement_after_done",
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, replacement.ActiveKey)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
}

func TestWalletAutoRechargeFreshProviderLeaseRejectsStaleCloserDuringPOST(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	// Start inside the grace window. The test moves the durable attempt past the
	// boundary only after the POST authorization gate has succeeded.
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})

	postStarted := make(chan struct{})
	releasePOST := make(chan struct{})
	processErr := make(chan error, 1)
	go func() {
		processErr <- ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
			func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
				close(postStarted)
				<-releasePOST
				return &TossBillingChargeResult{
					Done:            true,
					ProviderStatus:  "DONE",
					Total:           amount,
					PaymentKey:      "pay_done_after_stale_close",
					ProviderPayload: `{"paymentKey":"pay_done_after_stale_close","status":"DONE","totalAmount":10000}`,
				}, nil
			},
		)
	}()

	select {
	case <-postStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST did not start")
	}
	// Simulate another worker crossing the grace boundary and observing GET 404
	// while the POST is in flight. The fresh provider lease must win.
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).
		Update("create_time", GetDBTimestamp()-TossBillingOperationalGraceSeconds-1).Error)
	settlementWon, err := closeStaleWalletAutoRechargeAttempt(policy.Id, topUp.TradeNo)
	require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost)
	require.False(t, settlementWon)
	var closedTopUp TopUp
	require.NoError(t, DB.First(&closedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, closedTopUp.Status)

	close(releasePOST)
	select {
	case err := <-processErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST result was not settled")
	}

	var settledTopUp TopUp
	require.NoError(t, DB.First(&settledTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, settledTopUp.Status)
	require.Equal(t, "pay_done_after_stale_close", settledTopUp.ProviderOrderId)
	require.Contains(t, settledTopUp.ProviderPayload, "pay_done_after_stale_close")
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusActive, storedKey.Status)

	// A later scheduler pass before the next due time is inert and cannot credit
	// or charge a second time.
	postCalls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Hour), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("terminal policy must not POST again")
		},
	))
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestWalletAutoRechargeCanceledWinsAgainstOlderInFlightDONE(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})

	postStarted := make(chan struct{})
	releasePOST := make(chan struct{})
	processErr := make(chan error, 1)
	go func() {
		processErr <- ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
			func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
				close(postStarted)
				<-releasePOST
				return &TossBillingChargeResult{
					Done: true, ProviderStatus: "DONE", Total: amount,
					PaymentKey: "pay_older_inflight_done", ProviderPayload: `{"status":"DONE"}`,
				}, nil
			},
		)
	}()
	select {
	case <-postStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST did not start")
	}
	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id, topUp.TradeNo, "CANCELED", `{"paymentKey":"pay_older_inflight_done","status":"CANCELED","balanceAmount":0}`, false, time.Now(),
	))
	close(releasePOST)
	select {
	case err := <-processErr:
		require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST result did not return")
	}

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.Contains(t, storedPolicy.LastError, "canceled")
	require.NotContains(t, storedPolicy.LastError, walletAutoRechargeProviderPendingPrefix)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)

	// An older ambiguous result arriving after cancellation is a no-op and must
	// not put the terminal policy back into the pending settlement queue.
	require.NoError(t, markWalletAutoRechargeProviderPending(policy.Id, topUp.TradeNo, ErrTossBillingChargePending))
	var afterMarker WalletAutoRecharge
	require.NoError(t, DB.First(&afterMarker, policy.Id).Error)
	require.Equal(t, storedPolicy.LastError, afterMarker.LastError)
	require.Equal(t, WalletAutoRechargeStatusFailed, afterMarker.Status)
	pendingSettlements, err := GetPendingWalletAutoRechargeSettlements(8)
	require.NoError(t, err)
	require.Empty(t, pendingSettlements)
}

func TestRecordedWalletCancellationBlocksDONEBeforeTerminalApply(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})

	postStarted := make(chan struct{})
	releasePOST := make(chan struct{})
	processErr := make(chan error, 1)
	go func() {
		processErr <- ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
			func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
				close(postStarted)
				<-releasePOST
				return &TossBillingChargeResult{
					Done: true, ProviderStatus: "DONE", Total: amount,
					PaymentKey: "pay_done_before_apply", ProviderPayload: `{"status":"DONE"}`,
				}, nil
			},
		)
	}()
	select {
	case <-postStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST did not start")
	}
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "wallet-cancel-before-terminal-apply",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              topUp.TradeNo,
		PaymentKey:           "pay_done_before_apply",
		Status:               "CANCELED",
		OriginalAmount:       topUp.Amount,
		BalanceAmount:        0,
		ProviderPayload:      `{"status":"CANCELED","balanceAmount":0}`,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	close(releasePOST)
	select {
	case err := <-processErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("provider POST result did not return")
	}

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.Nil(t, storedPolicy.ActiveKey)
	require.Equal(t, "wallet auto recharge provider payment canceled", storedPolicy.LastError)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)

	// The settlement entry point independently enforces the same durable-event
	// precedence even if terminal Apply previously crashed.
	err = SettleWalletAutoRechargeWebhookDone(
		policy.Id, topUp.TradeNo, "pay_done_before_apply", `{"status":"DONE"}`, time.Now(),
	)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)
	var cancellation TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", "wallet-cancel-before-terminal-apply").First(&cancellation).Error)
	require.Equal(t, TossReconciliationStatusResolved, cancellation.ReconciliationStatus)
	pendingSettlements, err := GetPendingWalletAutoRechargeSettlements(8)
	require.NoError(t, err)
	require.Empty(t, pendingSettlements, "manual cancellation reconciliation must not poison the automatic queue")
}

func TestRecordedWalletCancellationBlocksPrepareAndFinalPOSTGate(t *testing.T) {
	t.Run("prepare after prior success", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
		now := GetDBTimestamp()
		require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
			"status":            common.TopUpStatusSuccess,
			"provider_order_id": "pay_prior_success_then_canceled",
			"complete_time":     now - 60,
		}).Error)
		require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
			"type":                  WalletAutoRechargeTypeScheduled,
			"status":                WalletAutoRechargeStatusActive,
			"last_error":            "",
			"last_charge_time":      now - 60,
			"next_charge_time":      now - 1,
			"settlement_retry_time": 0,
		}).Error)
		_, err := RecordTossPaymentEvent(&TossPaymentEvent{
			EventKey: "wallet-prior-success-canceled", EventType: TossPaymentEventTypeCancellation,
			OrderId: topUp.TradeNo, PaymentKey: "pay_prior_success_then_canceled", Status: "CANCELED",
			OriginalAmount: topUp.Amount, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
		})
		require.NoError(t, err)

		prepared, err := prepareWalletAutoRechargeCharge(policy.Id, time.Unix(now, 0), 3)
		require.NoError(t, err)
		require.False(t, prepared.shouldCharge)
		var storedPolicy WalletAutoRecharge
		require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
		require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
		require.True(t, strings.HasPrefix(storedPolicy.LastError, walletAutoRechargeManualReconciliationPrefix))
		var storedKey UserBillingKey
		require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
		require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	})

	t.Run("final post gate", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
		require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
		_, err := RecordTossPaymentEvent(&TossPaymentEvent{
			EventKey: "wallet-final-gate-canceled", EventType: TossPaymentEventTypeCancellation,
			OrderId: topUp.TradeNo, PaymentKey: "pay_final_gate_canceled", Status: "CANCELED",
			OriginalAmount: topUp.Amount, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
		})
		require.NoError(t, err)

		err = ValidateWalletAutoRechargeChargeAttempt(policy.Id, topUp.TradeNo)
		require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost)
		var storedPolicy WalletAutoRecharge
		require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
		require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
		var storedKey UserBillingKey
		require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
		require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	})
}

func TestHistoricalWalletCancellationStillRecoversNewerPendingDONEWithoutPOST(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, pending := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", pending.Id).Update("create_time", GetDBTimestamp()).Error)
	initialQuota := TossCreditQuotaFromKRW(10000)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", policy.OwnerUserId).Update("quota", initialQuota).Error)
	oldTradeNo := fmt.Sprintf("wallet_auto_%d_old_success", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: policy.OwnerUserId, TargetType: policy.TargetType, TargetId: policy.TargetId,
		Amount: 10000, Quota: initialQuota, TradeNo: oldTradeNo, ProviderOrderId: "pay_old_success_canceled",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CreateTime: GetDBTimestamp() - 3600, CompleteTime: GetDBTimestamp() - 3500,
	}).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "wallet-historical-cancel-newer-pending", EventType: TossPaymentEventTypeCancellation,
		OrderId: oldTradeNo, PaymentKey: "pay_old_success_canceled", Status: "CANCELED",
		OriginalAmount: 10000, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, pending.TradeNo, orderID)
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_newer_pending_done", ProviderPayload: `{"status":"DONE"}`,
		}, nil
	})
	postCalls := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("historical cancellation must forbid every new POST")
		},
	))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	var settled TopUp
	require.NoError(t, DB.First(&settled, pending.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, settled.Status)
	require.Equal(t, "pay_newer_pending_done", settled.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(initialQuota+TossCreditQuotaFromKRW(10000)), user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	var fulfillment TossPaymentEvent
	require.NoError(t, DB.Where("order_id = ? AND event_type = ? AND payment_key = ?", pending.TradeNo, TossPaymentEventTypeFulfillment, "pay_newer_pending_done").First(&fulfillment).Error)
	require.Equal(t, "pay_newer_pending_done", fulfillment.PaymentKey)
}

func TestHistoricalWalletCancellationClosesNewerPendingNotFoundWithoutPOST(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, pending := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", pending.Id).Update("create_time", GetDBTimestamp()).Error)
	oldTradeNo := fmt.Sprintf("wallet_auto_%d_old_canceled", policy.Id)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "wallet-historical-cancel-newer-not-found", EventType: TossPaymentEventTypeCancellation,
		OrderId: oldTradeNo, PaymentKey: "pay_old_canceled", Status: "CANCELED",
		OriginalAmount: 10000, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("historical cancellation must forbid new POST")
		},
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeReconciliationRequired)
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, pending.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, storedTopUp.Status)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	require.True(t, strings.HasPrefix(storedPolicy.LastError, walletAutoRechargeLateDONEFencePrefix))
}

func TestSubscriptionCancellationDoesNotStopWalletSharingBillingKey(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, pending := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", pending.Id).Update("create_time", GetDBTimestamp()).Error)
	subscriptionTradeNo := "toss_subscription_shared_key_canceled"
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:           policy.OwnerUserId,
		TradeNo:          subscriptionTradeNo,
		PaymentMethod:    PaymentMethodToss,
		PaymentProvider:  PaymentProviderToss,
		Status:           common.TopUpStatusSuccess,
		BillingKeyId:     policy.BillingKeyId,
		ProviderAmount:   10000,
		ProviderCurrency: "KRW",
	}).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "subscription-cancel-shared-wallet-key",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              subscriptionTradeNo,
		PaymentKey:           "pay_subscription_shared_key_canceled",
		Status:               "CANCELED",
		OriginalAmount:       10000,
		BalanceAmount:        0,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, pending.TradeNo, orderID)
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("wallet charge attempted independently")
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, 1, postCalls)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, pending.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusActive, storedKey.Status)
}

func TestWalletTerminalMutationLocksOwnerBeforePolicyAndPreservesSharedSubscriptionKey(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Create(&UserSubscription{
		Id:           73,
		UserId:       policy.OwnerUserId,
		Status:       "active",
		AutoRenew:    true,
		BillingKeyId: policy.BillingKeyId,
	}).Error)

	lockedTables := make([]string, 0, 4)
	callbackName := "test:wallet_financial_lock_order"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			lockedTables = append(lockedTables, tx.Statement.Table)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})

	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id,
		topUp.TradeNo,
		"CANCELED",
		`{"status":"CANCELED","balanceAmount":0}`,
		false,
		time.Now(),
	))

	ownerLock := -1
	keyLock := -1
	policyLock := -1
	for i, table := range lockedTables {
		switch table {
		case "users":
			if ownerLock < 0 {
				ownerLock = i
			}
		case "user_billing_keys":
			if keyLock < 0 {
				keyLock = i
			}
		case "wallet_auto_recharges":
			if policyLock < 0 {
				policyLock = i
			}
		}
	}
	require.GreaterOrEqual(t, ownerLock, 0, "financial mutation must lock the billing owner")
	require.GreaterOrEqual(t, keyLock, 0, "financial mutation must lock the provider-key identity")
	require.GreaterOrEqual(t, policyLock, 0, "financial mutation must lock the wallet policy")
	require.Less(t, ownerLock, keyLock, "owner lock must precede provider-key mutation")
	require.Less(t, keyLock, policyLock, "provider-key lock must precede policy mutation even when lifecycle root rows are missing")

	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusActive, storedKey.Status,
		"a live subscription sharing the provider key must prevent remote-key deletion")
	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 73).Error)
	require.True(t, subscription.AutoRenew)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
}

func TestWalletTerminalMutationLocksKeyBeforePolicyAfterOwnerDeletion(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Unscoped().Delete(&User{}, policy.OwnerUserId).Error)

	lockedTables := make([]string, 0, 4)
	callbackName := "test:wallet_deleted_owner_financial_lock_order"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			lockedTables = append(lockedTables, tx.Statement.Table)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})

	require.NoError(t, ApplyWalletAutoRechargeWebhookTerminal(
		policy.Id,
		topUp.TradeNo,
		"CANCELED",
		`{"status":"CANCELED","balanceAmount":0}`,
		false,
		time.Now(),
	), "late provider evidence must remain cleanable after the owner row is deleted")

	keyLock := -1
	policyLock := -1
	for i, table := range lockedTables {
		switch table {
		case "user_billing_keys":
			if keyLock < 0 {
				keyLock = i
			}
		case "wallet_auto_recharges":
			if policyLock < 0 {
				policyLock = i
			}
		}
	}
	require.GreaterOrEqual(t, keyLock, 0)
	require.GreaterOrEqual(t, policyLock, 0)
	require.Less(t, keyLock, policyLock,
		"without a surviving owner row, key-before-policy ordering must still prevent a BILLING_DELETED cycle")

	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
}

func TestWalletAutoRechargeAlreadyDoneRecoveryHonorsCancellationBeforeCredit(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
		"create_time":         now,
		"provider_order_id":   "pay_recorded_before_cancel",
		"provider_order_time": now,
		"provider_payload":    `{"status":"DONE"}`,
	}).Error)
	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, time.Unix(now, 0), 3)
	require.NoError(t, err)
	require.True(t, prepared.alreadyDone)
	_, err = RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey: "wallet-cancel-between-prepare-complete", EventType: TossPaymentEventTypeCancellation,
		OrderId: topUp.TradeNo, PaymentKey: "pay_recorded_before_cancel", Status: "CANCELED",
		OriginalAmount: topUp.Amount, BalanceAmount: 0, ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	err = completeWalletAutoRechargeTopUp(policy.Id, topUp.TradeNo, time.Unix(now, 0))
	require.NoError(t, err)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, storedTopUp.Status)
	require.Equal(t, topUp.TradeNo+":terminal", storedTopUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Zero(t, user.Quota)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, policy.BillingKeyId).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, storedKey.Status)
	var cancellation TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", "wallet-cancel-between-prepare-complete").First(&cancellation).Error)
	require.Equal(t, TossReconciliationStatusResolved, cancellation.ReconciliationStatus)
}

func TestScheduledWalletAttemptUsesOriginalDueTimeGraceBoundary(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"type":             WalletAutoRechargeTypeScheduled,
		"next_charge_time": now - TossBillingOperationalGraceSeconds + 1,
	}).Error)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", now).Error)
	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, time.Unix(now, 0), 3)
	require.NoError(t, err)
	require.True(t, prepared.lookupPending)
	require.False(t, prepared.stalePending, "the original due time is initially just inside grace")

	// Advance the authoritative due snapshot across the boundary without opening
	// a new 24-hour window from the freshly-created TopUp timestamp.
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).
		Update("next_charge_time", now-TossBillingOperationalGraceSeconds-1).Error)
	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		return nil, ErrTossBillingPaymentNotFound
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Unix(now+2, 0), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("scheduled attempt outside original due grace must not POST")
		},
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptOutsideGrace)
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, storedTopUp.Status)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, storedPolicy.Status)
}

func TestWalletAutoRechargePersistsExactRotatedSecretBeforeEachPOST(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	oldCredential, err := EncryptProviderCredential("wallet-secret-old")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("provider_credential", oldCredential).Error)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", policy.BillingKeyId).Update("provider_credential", oldCredential).Error)
	setting.TossBillingSecretKey = "wallet-secret-new"

	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, "wallet-secret-old", secretKeys[0])
		return nil, ErrTossBillingPaymentNotFound
	})
	postedSecrets := make([]string, 0, 2)
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postedSecrets = append(postedSecrets, secretKey)
			switch secretKey {
			case "wallet-secret-old":
				return nil, errors.New("INVALID_API_KEY")
			case "wallet-secret-new":
				return nil, context.DeadlineExceeded
			default:
				return nil, fmt.Errorf("unexpected secret %q", secretKey)
			}
		},
	)
	require.NoError(t, err, "ambiguous provider result remains a durable pending attempt")
	require.Equal(t, []string{"wallet-secret-old", "wallet-secret-new"}, postedSecrets)
	var pending TopUp
	require.NoError(t, DB.First(&pending, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, pending.Status)
	persistedSecret, err := DecryptProviderCredential(pending.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "wallet-secret-new", persistedSecret)

	// Recovery starts with the exact ambiguous-attempt credential. A 404 in that
	// same idempotency namespace may safely repeat the POST after another final
	// lifecycle/grace gate.
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, "wallet-secret-new", secretKeys[0])
		return nil, ErrTossBillingPaymentNotFound
	})
	recoveryPosts := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Minute), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			recoveryPosts++
			require.Equal(t, "wallet-secret-new", secretKey)
			return &TossBillingChargeResult{
				Done: true, ProviderStatus: "DONE", Total: amount,
				PaymentKey: "pay_rotated_wallet_recovery", ProviderPayload: `{"status":"DONE"}`,
			}, nil
		},
	))
	require.Equal(t, 1, recoveryPosts)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestWalletAutoRechargeRechecksLifecycleBeforeRotatedSecretFallbackPOST(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	oldCredential, err := EncryptProviderCredential("wallet-gate-old")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("provider_credential", oldCredential).Error)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", policy.BillingKeyId).Update("provider_credential", oldCredential).Error)
	setting.TossBillingSecretKey = "wallet-gate-new"
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		return nil, ErrTossBillingPaymentNotFound
	})
	postedSecrets := make([]string, 0, 2)
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postedSecrets = append(postedSecrets, secretKey)
			require.Equal(t, "wallet-gate-old", secretKey)
			require.NoError(t, CancelWalletAutoRecharge(policy.Id, policy.TargetType, policy.TargetId))
			return nil, errors.New("INVALID_API_KEY")
		},
	)
	require.ErrorIs(t, err, ErrWalletAutoRechargeClaimLost)
	require.Equal(t, []string{"wallet-gate-old"}, postedSecrets, "rotated fallback must run its own gate before POST")
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, storedPolicy.Status)
	require.NotNil(t, storedPolicy.ActiveKey)
	var pending TopUp
	require.NoError(t, DB.First(&pending, topUp.Id).Error)
	persistedSecret, err := DecryptProviderCredential(pending.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "wallet-gate-new", persistedSecret, "a definitive rejection safely promotes the next lookup namespace without authorizing its POST")
}

func TestWalletAutoRechargeResumesPromotedCredentialAfterGateInterruption(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("create_time", GetDBTimestamp()).Error)
	oldCredential, err := EncryptProviderCredential("wallet-resume-old")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Update("provider_credential", oldCredential).Error)
	require.NoError(t, DB.Model(&UserBillingKey{}).Where("id = ?", policy.BillingKeyId).Update("provider_credential", oldCredential).Error)
	setting.TossBillingSecretKey = "wallet-resume-new"
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, "wallet-resume-old", secretKeys[0])
		return nil, ErrTossBillingPaymentNotFound
	})
	postedSecrets := make([]string, 0, 2)
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postedSecrets = append(postedSecrets, secretKey)
			require.Equal(t, "wallet-resume-old", secretKey)
			setting.TossBillingEnabled = false
			return nil, errors.New("INVALID_API_KEY")
		},
	)
	require.ErrorIs(t, err, ErrTossBillingOperationallyDisabled)
	require.Equal(t, []string{"wallet-resume-old"}, postedSecrets)
	var pending TopUp
	require.NoError(t, DB.First(&pending, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, pending.Status)
	persistedSecret, err := DecryptProviderCredential(pending.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "wallet-resume-new", persistedSecret)

	setting.TossBillingEnabled = true
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, "wallet-resume-new", secretKeys[0], "recovery must query the promoted exact namespace first")
		return nil, ErrTossBillingPaymentNotFound
	})
	recoveryPosts := 0
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Minute), 3,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			recoveryPosts++
			require.Equal(t, "wallet-resume-new", secretKey)
			return &TossBillingChargeResult{
				Done: true, ProviderStatus: "DONE", Total: amount,
				PaymentKey: "pay_promoted_wallet_resume", ProviderPayload: `{"status":"DONE"}`,
			}, nil
		},
	))
	require.Equal(t, 1, recoveryPosts)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestStaleWalletAutoRechargeDoneSettlesExactlyOnceWithoutPost(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	// Turning off the separate contract blocks new POSTs, but a payment that may
	// already have completed must remain queryable and settle exactly once.
	setting.TossWalletAutoRechargeEnabled = false

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_stale_wallet_done",
			ProviderPayload: `{"paymentKey":"pay_stale_wallet_done","status":"DONE","totalAmount":10000}`,
		}, nil
	})
	postCalls := 0
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("stale durable attempt must never POST")
	}
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, charger))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, storedTopUp.Status)
	require.Equal(t, "pay_stale_wallet_done", storedTopUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)

	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now().Add(time.Hour), 3, charger))
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)
	require.NoError(t, DB.First(&user, policy.OwnerUserId).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestValidateWalletAutoRechargeChargeAttemptRejectsStalePendingOrder(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, topUp := seedStaleWalletAutoRechargeAttempt(t)
	// The shared fixture deliberately models a historical binary whose order
	// cycle was written before CreateTime-based alias detection existed. This
	// test targets the new final gate, so make the old timestamp and logical
	// cycle internally coherent before asserting the independent grace check.
	staleTime := time.Unix(topUp.CreateTime, 0).In(time.Local)
	identities, identityErr := walletAutoRechargeAttemptIdentitiesForPolicyAttempt(&policy, staleTime, 0)
	require.NoError(t, identityErr)
	require.NotEmpty(t, identities)
	identity := identities[len(identities)-1]
	tradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
	require.True(t, ok)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
			"trade_no":                       tradeNo,
			"wallet_auto_recharge_cycle_key": identity.CycleKey,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Update("last_trade_no", tradeNo).Error
	}))
	topUp.TradeNo = tradeNo

	err := ValidateWalletAutoRechargeChargeAttempt(policy.Id, topUp.TradeNo)
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptOutsideGrace)

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, storedTopUp.Status, "the authorization gate must not mutate outside its caller's recovery transaction")
}

func TestProcessWalletAutoRechargeContractGateDisabledBeforeProviderPostPreservesPendingAttempt(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "disabled-charge-owner",
		Group:    "default",
		AffCode:  "disabled-charge-owner",
	}).Error)

	encryptedBillingKey, err := common.EncryptString("disabled-charge-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id:           96,
		UserId:       1,
		CustomerKey:  "disabled-charge-customer",
		EncryptedKey: encryptedBillingKey,
		Status:       BillingKeyStatusActive,
	})).Error)

	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type:                  WalletAutoRechargeTypeScheduled,
		TargetType:            TopUpTargetTypeUser,
		TargetId:              1,
		OwnerUserId:           1,
		BillingKeyId:          96,
		CustomerKey:           "disabled-charge-customer",
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
		Amount:                10000,
		IntervalUnit:          WalletAutoRechargeIntervalMonth,
		IntervalValue:         1,
		Status:                WalletAutoRechargeStatusActive,
		ActiveKey:             &activeKey,
		NextChargeTime:        now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = false
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("disabled Toss billing must not call charger")
	})
	require.ErrorIs(t, err, ErrTossBillingOperationallyDisabled)
	require.Zero(t, postCalls)

	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	require.NotEmpty(t, storedPolicy.LastTradeNo)
	require.True(t, strings.HasPrefix(storedPolicy.LastError, walletAutoRechargeProviderPendingPrefix))
	require.Zero(t, storedPolicy.FailCount)

	var pending TopUp
	require.NoError(t, DB.First(&pending, "trade_no = ?", storedPolicy.LastTradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, pending.Status)
	require.Empty(t, pending.ProviderOrderId)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Equal(t, int64(1), topUpCount)

	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Zero(t, user.Quota)
}

func TestWalletAutoRechargePrefixLookupDoesNotCrossPolicyIDs(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy := WalletAutoRecharge{Id: 12, TargetType: TopUpTargetTypeUser, TargetId: 1}
	crossPolicyTopUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		TradeNo: "wallet_auto_123_1700000000_0", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending, Amount: 10000,
	}
	require.NoError(t, DB.Create(&crossPolicyTopUp).Error)

	hasActivity, err := HasWalletAutoRechargeProviderActivity(&policy)
	require.NoError(t, err)
	require.False(t, hasActivity)

	var recovered *TopUp
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var findErr error
		recovered, findErr = findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
		return findErr
	}))
	require.Nil(t, recovered)

	ownTopUp := TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		TradeNo: "wallet_auto_12_1700000000_0", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending, Amount: 10000,
	}
	require.NoError(t, DB.Create(&ownTopUp).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var findErr error
		recovered, findErr = findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
		return findErr
	}))
	require.NotNil(t, recovered)
	require.Equal(t, ownTopUp.TradeNo, recovered.TradeNo)
}

func TestWalletAutoRechargeProviderActivityFailsClosedOnInvisibleOpaqueEvidence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy := WalletAutoRecharge{Id: 12, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1}
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		TradeNo: "opaqueWalletActivityEvidence", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	}).Error)

	hasActivity, err := HasWalletAutoRechargeProviderActivity(&policy)
	require.True(t, hasActivity)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
}

func TestPendingWalletSettlementGlobalCorruptionDoesNotHaltHealthyPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		ActiveKey: &activeKey, OwnerUserId: 1, BillingKeyId: 77,
		Status: WalletAutoRechargeStatusActive, LastError: "",
	}
	require.NoError(t, DB.Create(&policy).Error)
	unrelatedPolicyID := policy.Id + 999
	require.NoError(t, DB.Create(&TopUp{
		UserId: 99, TargetType: TopUpTargetTypeUser, TargetId: 99,
		TradeNo: "opaqueWalletSettlementEvidence", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		WalletAutoRechargeId: &unrelatedPolicyID,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	}).Error)

	settlements, err := GetPendingWalletAutoRechargeSettlements(1)
	require.Empty(t, settlements)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.Zero(t, policy.FailCount)
	require.Empty(t, policy.LastError)
	require.Zero(t, policy.SettlementRetryTime)
}

func TestWalletAutoRechargeIntervalBoundsRejectDurationOverflow(t *testing.T) {
	base := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	_, err := nextWalletChargeTime(base, WalletAutoRechargeIntervalCustom, 1, math.MaxInt64)
	require.Error(t, err)

	_, err = nextWalletChargeTime(base, WalletAutoRechargeIntervalDay, WalletAutoRechargeMaximumDayInterval+1, 0)
	require.Error(t, err)
	_, err = nextWalletChargeTime(base, WalletAutoRechargeIntervalMonth, WalletAutoRechargeMaximumMonthInterval+1, 0)
	require.Error(t, err)

	_, err = (WalletAutoRechargePresetRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetScope: WalletAutoRechargePresetTargetUser,
		Name: "overflow", Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalCustom,
		IntervalValue: 1, CustomSeconds: math.MaxInt64,
	}).normalizeAndValidate()
	require.Error(t, err)
}

func TestWalletAutoRechargeRejectsFractionalKRWAmounts(t *testing.T) {
	require.Zero(t, walletAutoRechargeKRW(10000.5))

	_, err := (CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, AuthTradeNo: "fractional-amount-auth", Amount: 10000.5,
		IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
	}).normalizeAndValidate()
	require.Error(t, err)

	_, err = (WalletAutoRechargePresetRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetScope: WalletAutoRechargePresetTargetUser,
		Name: "fractional-krw", Amount: 10000.5,
		IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
	}).normalizeAndValidate()
	require.Error(t, err)
}

func TestWalletAutoRechargeRejectsInvalidTossOrderIDs(t *testing.T) {
	base := CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
	}

	base.AuthTradeNo = strings.Repeat("a", WalletAutoRechargeOrderIDMaxBytes+1)
	_, err := base.normalizeAndValidate()
	require.Error(t, err)

	base.AuthTradeNo = "wallet auth contains spaces"
	_, err = base.normalizeAndValidate()
	require.Error(t, err)

	base.AuthTradeNo = "wallet_auth_valid"
	_, err = base.normalizeAndValidate()
	require.NoError(t, err)
}

func TestWalletAutoRechargeInvalidLegacyIntervalNeverPostsCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "invalid-interval-owner", Role: common.RoleCommonUser, AffCode: "invalid-interval-owner"}).Error)
	encryptedKey, err := common.EncryptString("invalid-interval-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 991, UserId: 1, CustomerKey: "invalid-interval-customer", EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Now()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 991, CustomerKey: "invalid-interval-customer", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalCustom, IntervalValue: 1, CustomSeconds: math.MaxInt64,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "must_not_charge"}, nil
	})
	require.Error(t, err)
	require.Zero(t, postCalls)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Zero(t, topUpCount)
}

func TestWalletAutoRechargeLiveModeNeverPostsLegacyCustomSchedule(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{"TossTestMode": "false"}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "live-custom-owner", Role: common.RoleCommonUser, AffCode: "live-custom-owner"}).Error)
	encryptedKey, err := common.EncryptString("live-custom-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 992, UserId: 1, CustomerKey: "live-custom-customer", EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Now().UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 992, CustomerKey: "live-custom-customer", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalCustom, IntervalValue: 1, CustomSeconds: 60,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 1, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "must_not_charge"}, nil
	})
	require.ErrorContains(t, err, "only in Toss test mode")
	require.Zero(t, postCalls)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusFailed, reloaded.Status)
	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.Zero(t, topUpCount)
}

func TestWalletAutoRechargeStaleNodeConfigNeverPostsCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	originalConfig := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValuesWithRevision(tossOptionValuesForTest(originalConfig), originalConfig.Revision))
	})
	t.Setenv(TossOptionSecretEncryptionEnv, "true")
	authoritativeRevision := persistAttestedTossOptionsForTest(t, map[string]string{
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
		"TossBillingClientKey":          "wallet_auto_test_ck",
		"TossBillingSecretKey":          "wallet_auto_test_sk",
	})
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(nil, "stale_wallet_revision"))

	require.NoError(t, DB.Create(&User{Id: 1, Username: "stale-config-owner", Role: common.RoleCommonUser, AffCode: "stale-config-owner"}).Error)
	encryptedKey, err := common.EncryptString("stale-config-billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 993, UserId: 1, CustomerKey: "stale-config-customer", EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)
	now := time.Now().UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 993, CustomerKey: "stale-config-customer", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return &TossBillingChargeResult{Done: true, Total: amount, PaymentKey: "must_not_charge"}, nil
	})
	require.ErrorIs(t, err, ErrTossBillingOperationallyDisabled)
	require.Zero(t, postCalls)
	require.Equal(t, authoritativeRevision, setting.GetTossConfigSnapshot().Revision)
}

func TestThresholdWalletAutoRechargeResetsPersistedDailyCount(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "daily-reset-owner", Role: common.RoleCommonUser, AffCode: "daily-reset-owner"}).Error)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		DailyChargeDate: now.AddDate(0, 0, -1).Format("2006-01-02"), DailyChargeCount: WalletAutoRechargeDailyLimit,
		DailyChargeDateUTC: true, LastChargeTime: now.Add(-25 * time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_daily_reset", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_daily_reset", `{"status":"DONE"}`, now))
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, reloaded.DailyChargeDateUTC)
	require.Equal(t, now.UTC().Format("2006-01-02"), reloaded.DailyChargeDate)
	require.Equal(t, 1, reloaded.DailyChargeCount)
}

func TestThresholdWalletAutoRechargeProcessResetsYesterdayCount(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "daily-process-reset-owner", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, AffCode: "daily-process-reset-owner",
	}).Error)
	encryptedKey, err := common.EncryptString("daily-process-reset-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 992, UserId: 1, CustomerKey: "daily-process-reset-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)

	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeThreshold)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
		TargetId: 1, OwnerUserId: 1, BillingKeyId: 992,
		CustomerKey: "daily-process-reset-customer", Amount: 10000, ThresholdQuota: 0,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		DailyChargeDate:    now.AddDate(0, 0, -1).Format("2006-01-02"),
		DailyChargeCount:   WalletAutoRechargeDailyLimit,
		DailyChargeDateUTC: true, LastChargeTime: now.Add(-25 * time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_daily_process_reset", ProviderPayload: `{"status":"DONE"}`,
		}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, postCalls)

	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, reloaded.DailyChargeDateUTC)
	require.Equal(t, now.UTC().Format("2006-01-02"), reloaded.DailyChargeDate)
	require.Equal(t, 1, reloaded.DailyChargeCount)
}

func TestThresholdWalletAutoRechargeBatchRotatesPastNonDuePolicies(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Unix(GetDBTimestamp(), 0).In(time.Local)
	policies := make([]WalletAutoRecharge, 0, 3)
	for id := 1; id <= 3; id++ {
		require.NoError(t, DB.Create(&User{
			Id: id, Username: fmt.Sprintf("threshold-fairness-%d", id),
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
			AffCode: fmt.Sprintf("threshold-fairness-%d", id), Quota: 100,
		}).Error)
		activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, id, WalletAutoRechargeTypeThreshold)
		policy := WalletAutoRecharge{
			Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser,
			TargetId: id, OwnerUserId: id, Amount: 10000, ThresholdQuota: 0,
			Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
			AuthTradeNo: fmt.Sprintf("threshold-fairness-auth-%d", id), UpdateTime: 1,
		}
		require.NoError(t, DB.Create(&policy).Error)
		require.NoError(t, DB.Model(&policy).Update("update_time", 1).Error)
		policies = append(policies, policy)
	}

	firstBatch, err := GetActiveThresholdWalletAutoRecharges(2)
	require.NoError(t, err)
	require.Equal(t, []int{policies[0].Id, policies[1].Id}, []int{firstBatch[0].Id, firstBatch[1].Id})
	unprocessedBatch, err := GetActiveThresholdWalletAutoRechargesExcluding(2, []int{policies[0].Id, policies[1].Id})
	require.NoError(t, err)
	require.Len(t, unprocessedBatch, 1)
	require.Equal(t, policies[2].Id, unprocessedBatch[0].Id)
	for i := range firstBatch {
		postCalls := 0
		err := ProcessWalletAutoRecharge(context.Background(), firstBatch[i].Id, now, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			postCalls++
			return nil, errors.New("non-due threshold policy must not charge")
		})
		require.NoError(t, err)
		require.Zero(t, postCalls)
	}

	secondBatch, err := GetActiveThresholdWalletAutoRecharges(2)
	require.NoError(t, err)
	require.Equal(t, policies[2].Id, secondBatch[0].Id)
}

func TestDueScheduledWalletAutoRechargeExcludesAlreadyProcessedPolicies(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	policies := make([]WalletAutoRecharge, 0, 3)
	for id := 1; id <= 3; id++ {
		activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, id, WalletAutoRechargeTypeScheduled)
		policy := WalletAutoRecharge{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: id, OwnerUserId: id, Amount: 10000,
			IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
			NextChargeTime: now.Add(-time.Duration(4-id) * time.Hour).Unix(),
			Status:         WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
			AuthTradeNo: fmt.Sprintf("scheduled-exclusion-auth-%d", id),
		}
		require.NoError(t, DB.Create(&policy).Error)
		policies = append(policies, policy)
	}

	rows, err := GetDueScheduledWalletAutoRechargesExcluding(
		now.Unix(),
		2,
		[]int{policies[0].Id, policies[1].Id},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, policies[2].Id, rows[0].Id)
}

func TestWalletAutoRechargeDueQueuesExcludeUnresolvedSettlementPolicies(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC)
	createPolicy := func(targetID int, policyType, lastError string) WalletAutoRecharge {
		activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, targetID, policyType)
		policy := WalletAutoRecharge{
			Type: policyType, TargetType: TopUpTargetTypeUser, TargetId: targetID, OwnerUserId: targetID,
			Amount: 10000, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
			NextChargeTime: now.Add(-time.Hour).Unix(), UpdateTime: int64(targetID),
			LastTradeNo: fmt.Sprintf("wallet_auto_%d_pending", targetID), LastError: lastError,
			AuthTradeNo: fmt.Sprintf("wallet-queue-filter-auth-%d", targetID),
		}
		require.NoError(t, DB.Create(&policy).Error)
		return policy
	}

	scheduledPoison := createPolicy(1, WalletAutoRechargeTypeScheduled, walletAutoRechargeProviderPendingPrefix+"timeout")
	scheduledReady := createPolicy(2, WalletAutoRechargeTypeScheduled, "")
	thresholdPoison := createPolicy(3, WalletAutoRechargeTypeThreshold, walletAutoRechargeReconciliationPendingPrefix+"manual review")
	thresholdReady := createPolicy(4, WalletAutoRechargeTypeThreshold, "")

	scheduled, err := GetDueScheduledWalletAutoRecharges(now.Unix(), 8)
	require.NoError(t, err)
	require.Len(t, scheduled, 1)
	require.Equal(t, []int{scheduledReady.Id}, []int{scheduled[0].Id})
	for i := range scheduled {
		require.NotEqual(t, scheduledPoison.Id, scheduled[i].Id)
	}
	threshold, err := GetActiveThresholdWalletAutoRecharges(8)
	require.NoError(t, err)
	require.Len(t, threshold, 1)
	require.Equal(t, []int{thresholdReady.Id}, []int{threshold[0].Id})
	for i := range threshold {
		require.NotEqual(t, thresholdPoison.Id, threshold[i].Id)
	}
}

func TestStaleScheduledWalletAutoRechargeIsSkippedWithoutCatchUpCharge(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 10, WalletAutoRechargeTypeScheduled)
	stale := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 10, OwnerUserId: 10,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		AuthTradeNo: "wallet-stale-schedule-auth", NextChargeTime: now.Unix() - TossBillingOperationalGraceSeconds - 1,
	}
	require.NoError(t, DB.Create(&stale).Error)

	due, err := GetDueScheduledWalletAutoRecharges(now.Unix(), 8)
	require.NoError(t, err)
	require.Empty(t, due, "an extended-outage schedule must not become an immediate catch-up charge")
	advanced, err := AdvanceStaleScheduledWalletAutoRecharges(now.Unix(), 8)
	require.NoError(t, err)
	require.Equal(t, int64(1), advanced)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, stale.Id).Error)
	require.Greater(t, reloaded.NextChargeTime, now.Unix())
	require.Equal(t, WalletAutoRechargeStatusActive, reloaded.Status)
	require.Zero(t, reloaded.FailCount)
	require.Contains(t, reloaded.LastError, "extended billing outage")
}

func TestScheduledWalletAutoRechargeDoesNotBackfillMissedIntervals(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "missed-schedule-owner", Role: common.RoleCommonUser, AffCode: "missed-schedule-owner"}).Error)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		IntervalUnit: WalletAutoRechargeIntervalDay, IntervalValue: 1,
		NextChargeTime: now.AddDate(0, 0, -30).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_missed_schedule", policy.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
		Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	require.NoError(t, SettleWalletAutoRechargeWebhookDone(policy.Id, tradeNo, "pay_missed_schedule", `{"status":"DONE"}`, now))
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, now.AddDate(0, 0, 1).Unix(), reloaded.NextChargeTime)
	require.Greater(t, reloaded.NextChargeTime, now.Unix())
}

func TestWalletAutoRechargeLastErrorTruncationPreservesUTF8(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy := WalletAutoRecharge{Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1, Status: WalletAutoRechargeStatusActive}
	require.NoError(t, DB.Create(&policy).Error)

	require.NoError(t, markWalletAutoRechargeProviderPending(policy.Id, "wallet_auto_utf8", errors.New(strings.Repeat("가", 300))))
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, utf8.ValidString(reloaded.LastError))
	require.LessOrEqual(t, len([]rune(reloaded.LastError)), 255)
}

func TestCancelWalletAutoRechargePreservesUncertainIssueForCleanup(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "cleanup-owner", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "cleanup-owner"}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "cleanup-customer", AuthTradeNo: "wallet_cleanup_issue", ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"), Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))

	billingKeyID, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	require.Zero(t, billingKeyID)
	var cleanup WalletAutoRecharge
	require.NoError(t, DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, cleanup.Status)
	require.NotEmpty(t, cleanup.IssueAuthKey)
	require.True(t, cleanup.IssueAttempted)
	require.NotEmpty(t, cleanup.ProviderCredential)
	require.NotEmpty(t, cleanup.ProviderClientKeyHash)

	cleanupToken, claimed, err := ClaimStoredWalletAutoRechargeBillingIssue(policy.AuthTradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, CancelClaimedWalletAutoRechargeBillingIssue(policy.AuthTradeNo, cleanupToken))
	require.NoError(t, DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, cleanup.Status)
	require.Empty(t, cleanup.IssueAuthKey)
	require.Empty(t, cleanup.IssueClaimToken)
	require.Empty(t, cleanup.ProviderCredential)
	require.Empty(t, cleanup.ProviderClientKeyHash)
}

func TestCancelPristineWalletAutoRechargeClearsIssueCredentials(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "pristine-cancel-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, AffCode: "pristine-cancel-owner",
	}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "pristine-cancel-customer", AuthTradeNo: "wallet_pristine_cancel", ProviderCredential: credential,
		ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"), Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "pristine-cancel-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, CancelWalletAutoRecharge(policy.Id, policy.TargetType, policy.TargetId))
	var cancelled WalletAutoRecharge
	require.NoError(t, DB.First(&cancelled, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, cancelled.Status)
	require.False(t, cancelled.IssueAttempted)
	require.Empty(t, cancelled.IssueAuthKey)
	require.Empty(t, cancelled.IssueAuthKeyHash)
	require.Empty(t, cancelled.IssueCustomerKey)
	require.Empty(t, cancelled.IssueClaimToken)
	require.Zero(t, cancelled.IssueClaimTime)
	require.Empty(t, cancelled.ProviderCredential)
	require.Empty(t, cancelled.ProviderClientKeyHash)
	require.ErrorIs(t, MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken), ErrWalletAutoRechargeClaimLost,
		"once pristine cancellation wins, the stale claim cannot authorize a provider ISSUE")
}

func TestCancelWalletAutoRechargePreservesAttemptedIssueWithoutAuthSnapshot(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "missing-snapshot-cancel-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, AffCode: "missing-snapshot-cancel-owner",
	}).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		CustomerKey: "missing-snapshot-cancel-customer", AuthTradeNo: "wallet_missing_snapshot_cancel",
		ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})
	require.NoError(t, err)
	claimToken, claimed, err := ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "missing-snapshot-cancel-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"issue_auth_key":      "",
		"issue_auth_key_hash": "",
	}).Error)

	require.NoError(t, CancelWalletAutoRecharge(policy.Id, policy.TargetType, policy.TargetId))
	var cleanup WalletAutoRecharge
	require.NoError(t, DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelPending, cleanup.Status)
	require.True(t, cleanup.IssueAttempted)
	require.Equal(t, claimToken, cleanup.IssueClaimToken)
	require.NotEmpty(t, cleanup.ProviderCredential)
	require.NotEmpty(t, cleanup.ProviderClientKeyHash)
}

func TestBillingLifecyclePreservesUncertainWalletIssueCleanup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType string
		targetID   int
	}{
		{name: "user", targetType: TopUpTargetTypeUser, targetID: 1},
		{name: "organization", targetType: TopUpTargetTypeOrganization, targetID: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			authSnapshot, err := common.EncryptString("lifecycle-auth")
			require.NoError(t, err)
			credential, err := EncryptProviderCredential("wallet_auto_test_sk")
			require.NoError(t, err)
			policy := WalletAutoRecharge{
				Type: WalletAutoRechargeTypeScheduled, TargetType: tc.targetType, TargetId: tc.targetID, OwnerUserId: 1,
				AuthTradeNo: "lifecycle_cleanup_" + tc.name, CustomerKey: "lifecycle-customer", Amount: 10000,
				IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, Status: WalletAutoRechargeStatusPending,
				ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
				IssueAuthKey: authSnapshot, IssueAuthKeyHash: common.GenerateHMAC("lifecycle-auth"),
				IssueCustomerKey: "lifecycle-customer", IssueClaimToken: "lifecycle-claim",
				IssueClaimTime: GetDBTimestamp(), IssueRetryTime: GetDBTimestamp() + 60, IssueAttempted: true,
			}
			activeKey := walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, policy.Type)
			policy.ActiveKey = &activeKey
			require.NoError(t, DB.Create(&policy).Error)

			if tc.targetType == TopUpTargetTypeOrganization {
				err = DeactivateTossBillingForOrganization(nil, tc.targetID)
			} else {
				err = DeactivateTossBillingForUser(nil, policy.OwnerUserId)
			}
			require.NoError(t, err)
			var reloaded WalletAutoRecharge
			require.NoError(t, DB.First(&reloaded, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelPending, reloaded.Status)
			require.Nil(t, reloaded.ActiveKey)
			require.NotEmpty(t, reloaded.IssueAuthKey)
			require.True(t, reloaded.IssueAttempted)
			require.Equal(t, "lifecycle-claim", reloaded.IssueClaimToken)
			require.NotEmpty(t, reloaded.ProviderCredential)
			require.NotEmpty(t, reloaded.ProviderClientKeyHash)
		})
	}
}

func TestBillingLifecycleClearsPristineWalletIssueCredentials(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType string
		targetID   int
	}{
		{name: "user", targetType: TopUpTargetTypeUser, targetID: 1},
		{name: "organization", targetType: TopUpTargetTypeOrganization, targetID: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			if tc.targetType == TopUpTargetTypeUser {
				require.NoError(t, DB.Create(&User{
					Id: 1, Username: "lifecycle-pristine-owner", Status: common.UserStatusEnabled,
					Role: common.RoleCommonUser, AffCode: "lifecycle-pristine-owner",
				}).Error)
			} else {
				require.NoError(t, DB.Create(&Organization{
					Id: tc.targetID, Name: "lifecycle-pristine-org", OwnerUserId: 1, Status: OrganizationStatusEnabled,
				}).Error)
			}
			authSnapshot, err := common.EncryptString("pristine-lifecycle-auth")
			require.NoError(t, err)
			credential, err := EncryptProviderCredential("wallet_auto_test_sk")
			require.NoError(t, err)
			activeKey := walletAutoRechargeActiveKey(tc.targetType, tc.targetID, WalletAutoRechargeTypeScheduled)
			policy := WalletAutoRecharge{
				Type: WalletAutoRechargeTypeScheduled, TargetType: tc.targetType, TargetId: tc.targetID, OwnerUserId: 1,
				AuthTradeNo: "lifecycle_pristine_" + tc.name, CustomerKey: "pristine-lifecycle-customer", Amount: 10000,
				IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
				Status: WalletAutoRechargeStatusPending, ActiveKey: &activeKey,
				ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
				IssueAuthKey: authSnapshot, IssueAuthKeyHash: common.GenerateHMAC("pristine-lifecycle-auth"),
				IssueCustomerKey: "pristine-lifecycle-customer", IssueClaimToken: "pristine-lifecycle-claim",
				IssueClaimTime: GetDBTimestamp(), IssueRetryTime: GetDBTimestamp() + 60, IssueAttempted: false,
			}
			require.NoError(t, DB.Create(&policy).Error)

			if tc.targetType == TopUpTargetTypeOrganization {
				err = DeactivateTossBillingForOrganization(nil, tc.targetID)
			} else {
				err = DeactivateTossBillingForUser(nil, policy.OwnerUserId)
			}
			require.NoError(t, err)
			var cancelled WalletAutoRecharge
			require.NoError(t, DB.First(&cancelled, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelled, cancelled.Status)
			require.Nil(t, cancelled.ActiveKey)
			require.False(t, cancelled.IssueAttempted)
			require.Empty(t, cancelled.IssueAuthKey)
			require.Empty(t, cancelled.IssueAuthKeyHash)
			require.Empty(t, cancelled.IssueCustomerKey)
			require.Empty(t, cancelled.IssueClaimToken)
			require.Zero(t, cancelled.IssueClaimTime)
			require.Zero(t, cancelled.IssueRetryTime)
			require.Empty(t, cancelled.ProviderCredential)
			require.Empty(t, cancelled.ProviderClientKeyHash)
		})
	}
}

func TestBillingLifecyclePreservesAttemptedWalletIssueWithoutAuthSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType string
		targetID   int
	}{
		{name: "user", targetType: TopUpTargetTypeUser, targetID: 1},
		{name: "organization", targetType: TopUpTargetTypeOrganization, targetID: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			credential, err := EncryptProviderCredential("wallet_auto_test_sk")
			require.NoError(t, err)
			activeKey := walletAutoRechargeActiveKey(tc.targetType, tc.targetID, WalletAutoRechargeTypeScheduled)
			policy := WalletAutoRecharge{
				Type: WalletAutoRechargeTypeScheduled, TargetType: tc.targetType, TargetId: tc.targetID, OwnerUserId: 1,
				AuthTradeNo: "lifecycle_missing_snapshot_" + tc.name, CustomerKey: "missing-snapshot-customer", Amount: 10000,
				IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
				Status: WalletAutoRechargeStatusPending, ActiveKey: &activeKey,
				ProviderCredential: credential, ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
				IssueClaimToken: "missing-snapshot-claim", IssueClaimTime: GetDBTimestamp(), IssueAttempted: true,
			}
			require.NoError(t, DB.Create(&policy).Error)

			if tc.targetType == TopUpTargetTypeOrganization {
				err = DeactivateTossBillingForOrganization(nil, tc.targetID)
			} else {
				err = DeactivateTossBillingForUser(nil, policy.OwnerUserId)
			}
			require.NoError(t, err)
			var cleanup WalletAutoRecharge
			require.NoError(t, DB.First(&cleanup, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusCancelPending, cleanup.Status,
				"an attempted ISSUE remains operator-recoverable even when its auth snapshot is already unreadable")
			require.Nil(t, cleanup.ActiveKey)
			require.True(t, cleanup.IssueAttempted)
			require.Equal(t, "missing-snapshot-claim", cleanup.IssueClaimToken)
			require.NotEmpty(t, cleanup.ProviderCredential)
			require.NotEmpty(t, cleanup.ProviderClientKeyHash)
		})
	}
}

func TestPendingWalletSettlementRecoversFutureScheduledLegacyOrphanWithoutPrefixMixup(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	recoveryNow := time.Unix(GetDBTimestamp(), 0)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "legacy-wallet-orphan-owner", Role: common.RoleCommonUser, AffCode: "legacy-wallet-orphan-owner",
	}).Error)

	// Give policy 1 the same target as policy 12. Its escaped prefix query must
	// not treat wallet_auto_12_* as a policy-1 order.
	decoy := WalletAutoRecharge{
		Id: 1, Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusFailed, AuthTradeNo: "legacy_wallet_orphan_decoy", UpdateTime: 1,
	}
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Id: 12, Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, AuthTradeNo: "legacy_wallet_orphan_real",
		NextChargeTime: recoveryNow.Add(time.Hour).Unix(), UpdateTime: 2,
	}
	require.NoError(t, DB.Create(&decoy).Error)
	require.NoError(t, DB.Create(&policy).Error)
	credential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	tradeNo := "wallet_auto_12_legacy_orphan"
	require.NoError(t, DB.Create(&TopUp{
		UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1,
		Amount: 10000, Money: 10, Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		ProviderCredential: credential, Status: common.TopUpStatusPending, CreateTime: recoveryNow.Unix(),
	}).Error)

	// Although the schedule is future-dated for the current worker, simulate a
	// later outage cleanup crossing the grace cutoff. The orphan TopUp is the
	// durable attempt and must prevent the schedule from being advanced.
	advanced, err := AdvanceStaleScheduledWalletAutoRecharges(
		recoveryNow.Add(time.Duration(TossBillingOperationalGraceSeconds)*time.Second+2*time.Hour).Unix(),
		8,
	)
	require.NoError(t, err)
	require.Zero(t, advanced)

	queued, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.Equal(t, policy.Id, queued[0].Id)
	require.Equal(t, tradeNo, queued[0].LastTradeNo)
	require.True(t, strings.HasPrefix(queued[0].LastError, walletAutoRechargeProviderPendingPrefix))
	require.Greater(t, queued[0].SettlementRetryTime, recoveryNow.Unix())

	lookupCalls := 0
	SetWalletAutoRechargeTossPaymentLookup(func(ctx context.Context, secretKeys []string, orderID string, amount int64) (*TossBillingChargeResult, error) {
		lookupCalls++
		require.Equal(t, tradeNo, orderID)
		require.Equal(t, int64(10000), amount)
		require.Contains(t, secretKeys, "wallet_auto_test_sk")
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount, PaymentKey: "pay_legacy_wallet_orphan",
			ProviderPayload: `{"paymentKey":"pay_legacy_wallet_orphan","status":"DONE"}`,
		}, nil
	})
	postCalls := 0
	err = ProcessWalletAutoRecharge(context.Background(), policy.Id, recoveryNow, 3, func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		postCalls++
		return nil, errors.New("legacy orphan recovery must not issue a new provider POST")
	})
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, postCalls)

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "pay_legacy_wallet_orphan", topUp.ProviderOrderId)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int64(TossCreditQuotaFromKRW(10000)), user.Quota)
}

func TestAdvanceStaleScheduledWalletAutoRechargePreservesPreparedAttemptForSettlementQueue(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	prepareNow := time.Unix(GetDBTimestamp(), 0)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "prepared-stale-wallet-owner", Role: common.RoleCommonUser, AffCode: "prepared-stale-wallet-owner",
	}).Error)
	encryptedBillingKey, err := common.EncryptString("prepared-stale-wallet-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: 120, UserId: 1, CustomerKey: "prepared-stale-wallet-customer",
		EncryptedKey: encryptedBillingKey, Status: BillingKeyStatusActive,
	})).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1, OwnerUserId: 1,
		BillingKeyId: 120, CustomerKey: "prepared-stale-wallet-customer", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey, AuthTradeNo: "prepared_stale_wallet_auth",
		NextChargeTime: prepareNow.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, prepareNow, 3)
	require.NoError(t, err)
	require.True(t, prepared.shouldCharge)
	require.NotEmpty(t, prepared.tradeNo)
	var before WalletAutoRecharge
	require.NoError(t, DB.First(&before, policy.Id).Error)
	require.Equal(t, prepared.tradeNo, before.LastTradeNo)
	require.True(t, strings.HasPrefix(before.LastError, walletAutoRechargeProviderPendingPrefix))

	advanceNow := prepareNow.Add(time.Duration(TossBillingOperationalGraceSeconds)*time.Second + 2*time.Minute)
	advanced, err := AdvanceStaleScheduledWalletAutoRecharges(advanceNow.Unix(), 8)
	require.NoError(t, err)
	require.Zero(t, advanced)
	var after WalletAutoRecharge
	require.NoError(t, DB.First(&after, policy.Id).Error)
	require.Equal(t, before.NextChargeTime, after.NextChargeTime)
	require.Equal(t, before.LastTradeNo, after.LastTradeNo)
	require.Equal(t, before.LastError, after.LastError)

	queued, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.Equal(t, policy.Id, queued[0].Id)
	require.Equal(t, prepared.tradeNo, queued[0].LastTradeNo)
}

func TestPendingWalletSettlementLegacyScanRotatesPastMoreThanOneBoundedMissBatch(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	const missCount = 33
	for i := 0; i < missCount; i++ {
		policy := WalletAutoRecharge{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: i + 1, OwnerUserId: i + 1, Status: WalletAutoRechargeStatusFailed,
			AuthTradeNo: fmt.Sprintf("wallet_legacy_scan_miss_%d", i),
		}
		require.NoError(t, DB.Create(&policy).Error)
	}
	orphan := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 500, OwnerUserId: 500, Status: WalletAutoRechargeStatusFailed,
		AuthTradeNo: "wallet_legacy_scan_real_orphan",
	}
	require.NoError(t, DB.Create(&orphan).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_legacy_scan_real", orphan.Id)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 500, TargetType: TopUpTargetTypeUser, TargetId: 500,
		Amount: 10000, Quota: TossCreditQuotaFromKRW(10000), TradeNo: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: GetDBTimestamp(),
	}).Error)

	first, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Empty(t, first, "the first bounded scan only leases the oldest no-TopUp misses")
	second, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, orphan.Id, second[0].Id)
	require.Equal(t, tradeNo, second[0].LastTradeNo)

	var oldestMiss WalletAutoRecharge
	require.NoError(t, DB.First(&oldestMiss, 1).Error)
	require.Greater(t, oldestMiss.SettlementRetryTime, int64(0))
}

func TestPendingWalletSettlementSkipsManualTerminalRowsWithoutStarvingRecoverablePayment(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	const poisonCount = 40
	manualPolicyIDs := make(map[int]struct{}, poisonCount)
	for i := 0; i < poisonCount; i++ {
		status := WalletAutoRechargeStatusFailed
		if i%2 == 0 {
			status = WalletAutoRechargeStatusCancelled
		}
		policy := WalletAutoRecharge{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: i + 1, OwnerUserId: i + 1, Status: status,
			AuthTradeNo: fmt.Sprintf("wallet_manual_terminal_auth_%d", i),
			LastError:   walletAutoRechargeReconciliationPendingPrefix + "provider cancellation retains balance",
		}
		require.NoError(t, DB.Create(&policy).Error)
		tradeNo := fmt.Sprintf("wallet_auto_%d_manual_terminal", policy.Id)
		lastError := policy.LastError
		providerOrderID := tradeNo + ":terminal"
		topUpStatus := common.TopUpStatusFailed
		if i == poisonCount-1 {
			lastError = walletAutoRechargeDoneMismatchPrefix + ": total mismatch"
			providerOrderID = tradeNo + ":done-mismatch"
			topUpStatus = common.TopUpStatusPending
		}
		require.NoError(t, DB.Model(&policy).Updates(map[string]interface{}{
			"last_trade_no": tradeNo,
			"last_error":    lastError,
		}).Error)
		require.NoError(t, DB.Create(&TopUp{
			UserId: i + 1, TargetType: TopUpTargetTypeUser, TargetId: i + 1,
			Amount: 10000, TradeNo: tradeNo, ProviderOrderId: providerOrderID,
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status: topUpStatus, CreateTime: GetDBTimestamp(),
		}).Error)
		manualPolicyIDs[policy.Id] = struct{}{}
	}

	recoverable := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 999, OwnerUserId: 999, Status: WalletAutoRechargeStatusFailed,
		AuthTradeNo: "wallet_recoverable_after_manual_poison",
		LastError:   walletAutoRechargeProviderPendingPrefix + "temporary lookup",
	}
	require.NoError(t, DB.Create(&recoverable).Error)
	recoverableTradeNo := fmt.Sprintf("wallet_auto_%d_recoverable", recoverable.Id)
	require.NoError(t, DB.Model(&recoverable).Update("last_trade_no", recoverableTradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 999, TargetType: TopUpTargetTypeUser, TargetId: 999,
		Amount: 10000, TradeNo: recoverableTradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: GetDBTimestamp(),
	}).Error)

	first, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	for i := range first {
		_, manual := manualPolicyIDs[first[i].Id]
		require.False(t, manual, "manual terminal payment must never enter automatic settlement")
	}
	second, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, recoverable.Id, second[0].Id)
	require.Equal(t, recoverableTradeNo, second[0].LastTradeNo)

	var terminal WalletAutoRecharge
	require.NoError(t, DB.First(&terminal, 1).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, terminal.Status)
	require.True(t, strings.HasPrefix(terminal.LastError, walletAutoRechargeManualReconciliationPrefix))
	require.Zero(t, terminal.SettlementRetryTime)

	// Even after any old retry lease would have elapsed, manual terminal rows
	// are permanently outside the automatic queue rather than cycling forever.
	require.NoError(t, DB.Model(&WalletAutoRecharge{}).
		Where("id IN ?", func() []int {
			ids := make([]int, 0, len(manualPolicyIDs))
			for id := range manualPolicyIDs {
				ids = append(ids, id)
			}
			return ids
		}()).
		Update("settlement_retry_time", GetDBTimestamp()-1).Error)
	third, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	for i := range third {
		_, manual := manualPolicyIDs[third[i].Id]
		require.False(t, manual)
	}
}

func TestPendingWalletSettlementMakesPostCreditCancellationManualOnly(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
		TargetId: 77, OwnerUserId: 77, Status: WalletAutoRechargeStatusFailed,
		AuthTradeNo: "wallet_post_credit_cancel_auth",
		LastError:   walletAutoRechargeReconciliationPendingPrefix + "authoritative cancellation event precedes DONE",
	}
	require.NoError(t, DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_post_credit_cancel", policy.Id)
	require.NoError(t, DB.Model(&policy).Update("last_trade_no", tradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId: 77, TargetType: TopUpTargetTypeUser, TargetId: 77,
		Amount: 10000, TradeNo: tradeNo, ProviderOrderId: "pay_post_credit_cancel",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusSuccess, CreateTime: GetDBTimestamp(), CompleteTime: GetDBTimestamp(),
	}).Error)
	event := TossPaymentEvent{
		EventKey: "wallet-post-credit-cancel-event", EventType: TossPaymentEventTypeCancellation,
		OrderId: tradeNo, PaymentKey: "pay_post_credit_cancel", Status: "CANCELED",
		OriginalAmount: 10000, CancelAmount: 10000, BalanceAmount: 0,
		ReconciliationStatus: TossReconciliationStatusRequired, CreateTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(&event).Error)

	queued, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Empty(t, queued)
	var reloaded WalletAutoRecharge
	require.NoError(t, DB.First(&reloaded, policy.Id).Error)
	require.True(t, strings.HasPrefix(reloaded.LastError, walletAutoRechargeManualReconciliationPrefix))
	require.Contains(t, reloaded.LastError, "authoritative cancellation")
	require.Zero(t, reloaded.SettlementRetryTime)

	// Expiring a synthetic lease cannot make the row eligible again, while the
	// durable event remains available to the administrator reconciliation API.
	require.NoError(t, DB.Model(&reloaded).Update("settlement_retry_time", GetDBTimestamp()-1).Error)
	queued, err = GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Empty(t, queued)
	var preservedEvent TossPaymentEvent
	require.NoError(t, DB.First(&preservedEvent, event.Id).Error)
	require.Equal(t, TossReconciliationStatusRequired, preservedEvent.ReconciliationStatus)
}

func TestPendingWalletSettlementReservationDoesNotLetLimitBatchStarveNextPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	rows := []WalletAutoRecharge{
		{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: 1, OwnerUserId: 1, Status: WalletAutoRechargeStatusActive,
			AuthTradeNo: "wallet_settlement_poison_auth",
			LastTradeNo: "wallet_auto_1_poison", LastError: walletAutoRechargeProviderPendingPrefix + "corrupt credential",
			ProviderCredential: "not-a-valid-encrypted-credential", UpdateTime: 100,
		},
		{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: 2, OwnerUserId: 2, Status: WalletAutoRechargeStatusActive,
			AuthTradeNo: "wallet_settlement_valid_auth",
			LastTradeNo: "wallet_auto_2_valid", LastError: walletAutoRechargeProviderPendingPrefix + "temporary lookup",
			UpdateTime: 200,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)
	validCredential, err := EncryptProviderCredential("wallet_auto_test_sk")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&[]TopUp{
		{
			UserId: 1, TargetType: TopUpTargetTypeUser, TargetId: 1, Amount: 10000,
			TradeNo: rows[0].LastTradeNo, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			ProviderCredential: "not-a-valid-encrypted-credential", Status: common.TopUpStatusPending,
		},
		{
			UserId: 2, TargetType: TopUpTargetTypeUser, TargetId: 2, Amount: 10000,
			TradeNo: rows[1].LastTradeNo, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			ProviderCredential: validCredential, Status: common.TopUpStatusPending,
		},
	}).Error)

	first, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, rows[0].Id, first[0].Id)

	// The caller can now fail on the corrupt credential without updating the
	// policy. The pre-processing retry lease must expose the next paid order.
	second, err := GetPendingWalletAutoRechargeSettlements(1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, rows[1].Id, second[0].Id)
}

func TestWalletIssueReconciliationDoesNotLetPoisonLimitBatchStarvePendingPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	rows := []WalletAutoRecharge{
		{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: 1, OwnerUserId: 1, Status: WalletAutoRechargeStatusCancelPending,
			AuthTradeNo: "wallet_issue_poison", IssueAuthKey: "corrupt-snapshot", UpdateTime: 100,
		},
		{
			Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser,
			TargetId: 2, OwnerUserId: 2, Status: WalletAutoRechargeStatusPending,
			AuthTradeNo: "wallet_issue_valid", IssueAuthKey: "recoverable-snapshot", UpdateTime: 200,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	previous := walletAutoRechargeIssueReconciler
	seen := make([]int, 0, 2)
	SetWalletAutoRechargeIssueReconciler(func(ctx context.Context, policy WalletAutoRecharge) (bool, error) {
		seen = append(seen, policy.Id)
		if policy.Id == rows[0].Id {
			return false, errors.New("permanent corrupt cleanup snapshot")
		}
		return true, nil
	})
	t.Cleanup(func() { SetWalletAutoRechargeIssueReconciler(previous) })

	resolved, err := ReconcileStaleWalletAutoRechargeBillingIssues(context.Background(), 1)
	require.Error(t, err)
	require.Zero(t, resolved)
	require.Equal(t, []int{rows[0].Id}, seen)

	resolved, err = ReconcileStaleWalletAutoRechargeBillingIssues(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, []int{rows[0].Id, rows[1].Id}, seen)
}
