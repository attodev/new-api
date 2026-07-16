package model

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTossSettlementCASTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	dsn := filepath.Join(t.TempDir(), "toss-settlement.db") +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)

	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	require.NoError(t, DB.AutoMigrate(
		&User{},
		&Log{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserBillingKey{},
		&TossPaymentEvent{},
		&TossRecurringOrderIDProtocolState{},
	))
	require.NoError(t, initializeTossRecurringOrderIDProtocolState(true))

	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB = originalDB
		LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})
}

func runTossSettlementConcurrently(count int, fn func() error) []error {
	errs := make([]error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(index int) {
			defer wg.Done()
			<-start
			errs[index] = fn()
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func requireNoTossSettlementErrors(t *testing.T, errs []error) {
	t.Helper()
	for _, err := range errs {
		require.NoError(t, err)
	}
}

func TestClaimTossUnattemptedSubscriptionChargeConcurrentSingleOwner(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.AutoMigrate(&WalletAutoRecharge{}))
	providerCredential, err := common.EncryptString("claim-cas-provider-secret")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id: 901, UserId: 900, CustomerKey: "cust_claim_cas",
		Status: BillingKeyStatusActive, CreateTime: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId: 900, PlanId: 902, TradeNo: "toss_sub_claim_cas",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp() - 3600,
		BillingKeyId: 901, BillingClaimToken: "stale-owner",
		BillingClaimTime:             GetDBTimestamp() - tossSubscriptionBillingClaimTTLSeconds - 1,
		ProviderCredential:           providerCredential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
	}).Error)

	var winners atomic.Int32
	errs := runTossSettlementConcurrently(8, func() error {
		_, claimed, err := ClaimTossUnattemptedSubscriptionChargeOrder("toss_sub_claim_cas")
		if claimed {
			winners.Add(1)
		}
		return err
	})
	requireNoTossSettlementErrors(t, errs)
	require.EqualValues(t, 1, winners.Load(), "only one cleanup worker may own the terminal transition")

	var order SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "toss_sub_claim_cas").First(&order).Error)
	require.NotEmpty(t, order.BillingClaimToken)
	require.NotEqual(t, "stale-owner", order.BillingClaimToken)
	require.NoError(t, ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(order.TradeNo, order.BillingClaimToken))
	require.NoError(t, DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, 901).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}

func TestRechargeTossConcurrentSettlementCreditsWalletOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       101,
		Username: "toss-cas-topup-user",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          101,
		Amount:          1000,
		Quota:           321,
		TradeNo:         "toss-cas-topup",
		ProviderOrderId: "toss-cas-topup",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	errs := runTossSettlementConcurrently(8, func() error {
		return RechargeToss("toss-cas-topup", "payment-key-cas", "127.0.0.1")
	})
	requireNoTossSettlementErrors(t, errs)

	var user User
	require.NoError(t, DB.First(&user, 101).Error)
	require.Equal(t, 321, user.Quota)
	topUp, err := GetTopUpByTradeNoWithError("toss-cas-topup")
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "payment-key-cas", topUp.ProviderOrderId)

	var logCount int64
	require.NoError(t, DB.Model(&Log{}).
		Where("user_id = ? AND type = ?", 101, LogTypeTopup).
		Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)
}

func TestWalletAutoRechargeConcurrentWebhookSettlementCreditsOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.AutoMigrate(&WalletAutoRecharge{}))
	require.NoError(t, DB.Create(&User{
		Id:       151,
		Username: "wallet-auto-cas-user",
		Status:   common.UserStatusEnabled,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 151, WalletAutoRechargeTypeScheduled)
	policy := &WalletAutoRecharge{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetType:    TopUpTargetTypeUser,
		TargetId:      151,
		OwnerUserId:   151,
		BillingKeyId:  152,
		Amount:        1000,
		IntervalUnit:  WalletAutoRechargeIntervalDay,
		IntervalValue: 1,
		Status:        WalletAutoRechargeStatusActive,
		ActiveKey:     &activeKey,
	}
	require.NoError(t, DB.Create(policy).Error)
	tradeNo := walletAutoRechargeTradeNo(*policy, time.Now().UTC())
	policy.LastTradeNo = tradeNo
	require.NoError(t, DB.Model(policy).Update("last_trade_no", tradeNo).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          151,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        151,
		Amount:          1000,
		Quota:           654,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	err := runTossSettlementConcurrently(8, func() error {
		return SettleWalletAutoRechargeWebhookDone(
			policy.Id,
			tradeNo,
			"wallet-auto-payment-key-cas",
			`{"status":"DONE"}`,
			time.Now().UTC(),
		)
	})
	requireNoTossSettlementErrors(t, err)

	var user User
	require.NoError(t, DB.First(&user, 151).Error)
	require.Equal(t, 654, user.Quota)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "wallet-auto-payment-key-cas", topUp.ProviderOrderId)
}

func TestConcurrentTossSubscriptionFulfillmentCannotExceedPurchaseLimit(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	user := &User{Id: 111, Username: "toss-limited-user", Status: common.UserStatusEnabled, Group: "default"}
	plan := &SubscriptionPlan{
		Id:                 112,
		Title:              "One purchase only",
		PriceAmount:        10,
		Currency:           "USD",
		DurationUnit:       SubscriptionDurationDay,
		DurationValue:      30,
		Enabled:            true,
		MaxPurchasePerUser: 1,
		TotalAmount:        1000,
		QuotaResetPeriod:   SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(plan).Error)

	tradeNos := []string{"toss_limit_order_1", "toss_limit_order_2"}
	for _, tradeNo := range tradeNos {
		order := &SubscriptionOrder{
			UserId:           user.Id,
			PlanId:           plan.Id,
			Money:            plan.PriceAmount,
			TradeNo:          tradeNo,
			PaymentMethod:    PaymentMethodToss,
			PaymentProvider:  PaymentProviderToss,
			Status:           common.TopUpStatusPending,
			CreateTime:       common.GetTimestamp(),
			ProviderAmount:   10000,
			ProviderCurrency: "KRW",
			BillingKeyId:     113,
		}
		require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
		require.NoError(t, DB.Create(order).Error)
	}

	jobs := make(chan string, len(tradeNos))
	for _, tradeNo := range tradeNos {
		jobs <- tradeNo
	}
	close(jobs)
	errs := runTossSettlementConcurrently(len(tradeNos), func() error {
		return CompleteTossBillingOrder(<-jobs, 113, `{"status":"DONE"}`)
	})
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	require.GreaterOrEqual(t, successes, 1)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestRecordTossPaymentKeyConcurrentClaimsDoNotOverwrite(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          102,
		Amount:          1000,
		Quota:           100,
		TradeNo:         "toss-cas-payment-key",
		ProviderOrderId: "toss-cas-payment-key",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	keys := []string{"payment-key-a", "payment-key-b"}
	errs := make([]error, len(keys))
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(keys))
	for i := range keys {
		go func(index int) {
			defer wg.Done()
			<-start
			errs[index] = RecordTossPaymentKey("toss-cas-payment-key", keys[index])
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTossPaymentKeyConflict):
			conflicts++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)

	topUp, err := GetTopUpByTradeNoWithError("toss-cas-payment-key")
	require.NoError(t, err)
	require.Contains(t, keys, topUp.ProviderOrderId)
	require.Positive(t, topUp.ProviderOrderTime)
	require.NoError(t, DB.Model(&TopUp{}).
		Where("trade_no = ?", topUp.TradeNo).
		Update("status", common.TopUpStatusSuccess).Error)
	require.NoError(t, RecordTossPaymentKey(topUp.TradeNo, topUp.ProviderOrderId))

	otherKey := keys[0]
	if otherKey == topUp.ProviderOrderId {
		otherKey = keys[1]
	}
	require.ErrorIs(t, RecordTossPaymentKey(topUp.TradeNo, otherKey), ErrTossPaymentKeyConflict)
	reloaded, err := GetTopUpByTradeNoWithError(topUp.TradeNo)
	require.NoError(t, err)
	require.Equal(t, topUp.ProviderOrderId, reloaded.ProviderOrderId)
}

func TestCompleteTossBillingOrderConcurrentSettlementCreatesSubscriptionOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       201,
		Username: "toss-cas-subscription-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	plan := &SubscriptionPlan{
		Id:            301,
		Title:         "CAS Plan",
		PriceAmount:   10,
		Currency:      "KRW",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:          201,
		PlanId:          plan.Id,
		Money:           10,
		TradeNo:         "toss-cas-subscription",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
		BillingKeyId:    777,
	}).Error)

	errs := runTossSettlementConcurrently(8, func() error {
		return CompleteTossBillingOrder("toss-cas-subscription", 777, `{"status":"DONE"}`)
	})
	requireNoTossSettlementErrors(t, errs)

	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 201, plan.Id).
		Count(&subscriptionCount).Error)
	require.EqualValues(t, 1, subscriptionCount)

	var topUpCount int64
	require.NoError(t, DB.Model(&TopUp{}).
		Where("trade_no = ?", "toss-cas-subscription").
		Count(&topUpCount).Error)
	require.EqualValues(t, 1, topUpCount)
	order, err := GetSubscriptionOrderByTradeNoWithError("toss-cas-subscription")
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.Equal(t, `{"status":"DONE"}`, order.ProviderPayload)
}

func TestCompleteTossBillingOrderDoesNotActivateDisabledUser(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       202,
		Username: "toss-disabled-subscription-user",
		Status:   common.UserStatusDisabled,
	}).Error)
	plan := &SubscriptionPlan{
		Id:            302,
		Title:         "Disabled User Plan",
		PriceAmount:   10,
		Currency:      "KRW",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:          202,
		PlanId:          plan.Id,
		TradeNo:         "toss-disabled-subscription",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
		BillingKeyId:    778,
	}).Error)

	err := CompleteTossBillingOrder("toss-disabled-subscription", 778, `{"status":"DONE"}`)
	require.ErrorContains(t, err, "not active")
	order, lookupErr := GetSubscriptionOrderByTradeNoWithError("toss-disabled-subscription")
	require.NoError(t, lookupErr)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", 202).Count(&count).Error)
	require.Zero(t, count)
}

func TestAttachTossBillingKeyDoesNotOverwriteDifferentClaim(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		TradeNo:         "toss-cas-billing-key-attach",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
		BillingKeyId:    111,
	}).Error)

	require.NoError(t, AttachTossBillingKeyToOrder("toss-cas-billing-key-attach", 111))
	require.ErrorIs(t,
		AttachTossBillingKeyToOrder("toss-cas-billing-key-attach", 222),
		ErrSubscriptionOrderStatusInvalid,
	)
	order, err := GetSubscriptionOrderByTradeNoWithError("toss-cas-billing-key-attach")
	require.NoError(t, err)
	require.Equal(t, 111, order.BillingKeyId)
}

func TestRenewTossSubscriptionConcurrentSameOrderExtendsOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       601,
		Username: "toss-cas-renewal-user",
		Status:   common.UserStatusEnabled,
	}).Error)
	plan := &SubscriptionPlan{
		Id:            401,
		Title:         "Renewal CAS Plan",
		PriceAmount:   20,
		Currency:      "KRW",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   2000,
	}
	require.NoError(t, DB.Create(plan).Error)
	providerCredential, err := common.EncryptString("renewal-cas-provider-secret")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{
		Id:                 111,
		UserId:             601,
		ProviderCredential: providerCredential,
		Status:             BillingKeyStatusActive,
	}).Error)
	oldEnd := time.Date(2030, time.January, 15, 0, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:              501,
		UserId:          601,
		PlanId:          plan.Id,
		StartTime:       time.Unix(oldEnd, 0).AddDate(0, -1, 0).Unix(),
		EndTime:         oldEnd,
		Status:          "active",
		AutoRenew:       true,
		NextBillingTime: oldEnd,
		BillingKeyId:    111,
	}).Error)
	seedTossRenewalContractForTest(t, 501, plan, 20000)
	tradeNo := tossRenewalTradeNo(501, oldEnd, 0)
	order, err := PrepareTossRenewalOrder(501, tradeNo, plan.PriceAmount, 20000)
	require.NoError(t, err)
	require.NotNil(t, order)
	tradeNo = order.TradeNo
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"billing_attempted":          true,
		"billing_attempt_credential": "test-attempt-credential",
	}).Error)

	errs := runTossSettlementConcurrently(8, func() error {
		return RenewTossSubscription(501, tradeNo, 20, 20000, `{"status":"DONE"}`)
	})
	requireNoTossSettlementErrors(t, errs)

	expectedEnd, err := calcPlanEndTime(time.Unix(oldEnd, 0), plan)
	require.NoError(t, err)
	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 501).Error)
	require.Equal(t, expectedEnd, subscription.EndTime)
	order, err = GetSubscriptionOrderByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	require.Equal(t, int64(20000), order.ProviderAmount)
}

func TestExpireTossRenewalConcurrentTransitionCountsFailureOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&UserSubscription{
		Id:        701,
		UserId:    702,
		PlanId:    703,
		Status:    "active",
		AutoRenew: true,
		StartTime: common.GetTimestamp() - 100,
		EndTime:   common.GetTimestamp() + 100,
		UpdatedAt: common.GetTimestamp(),
		CreatedAt: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:          702,
		PlanId:          703,
		TradeNo:         "toss-cas-renewal-failure",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	errs := runTossSettlementConcurrently(8, func() error {
		_, err := ExpireTossPendingRenewalOrderAndMarkFailure(701, "toss-cas-renewal-failure", 10)
		return err
	})
	requireNoTossSettlementErrors(t, errs)

	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 701).Error)
	require.Equal(t, 1, subscription.BillingFailCount)
	require.True(t, subscription.AutoRenew)
}

func TestPaymentOrderLookupHelpersPreserveDatabaseErrors(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	forcedErr := errors.New("forced query failure")
	require.NoError(t, DB.Callback().Query().Before("gorm:query").
		Register("test:force_payment_lookup_failure", func(tx *gorm.DB) {
			tx.AddError(forcedErr)
		}))

	topUp, err := GetTopUpByTradeNoWithError("unavailable-topup")
	require.Nil(t, topUp)
	require.ErrorIs(t, err, forcedErr)
	order, err := GetSubscriptionOrderByTradeNoWithError("unavailable-subscription")
	require.Nil(t, order)
	require.ErrorIs(t, err, forcedErr)
	require.Nil(t, GetTopUpByTradeNo("unavailable-topup"))
	require.Nil(t, GetSubscriptionOrderByTradeNo("unavailable-subscription"))
}
