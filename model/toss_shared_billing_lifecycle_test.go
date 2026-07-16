package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A subscription renewal and wallet recharge are separate financial contracts
// even when Toss returns the same billing key. Their final owner/key gates must
// serialize local authorization without collapsing the two deterministic order
// IDs, and each settlement must remain exactly-once.
func TestSharedBillingKeyConcurrentSchedulersSettleIndependentOrders(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	originalWalletEnabled := setting.TossWalletAutoRechargeEnabled
	setting.TossWalletAutoRechargeEnabled = true
	t.Cleanup(func() { setting.TossWalletAutoRechargeEnabled = originalWalletEnabled })

	keyID := seedTossBillingSubscription(t, "billing_key_concurrent_contracts", 0)
	seedTossBillingPlan(t, "Concurrent contracts", 1)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	var before UserSubscription
	require.NoError(t, DB.First(&before, 11).Error)

	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 7, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       7,
		OwnerUserId:    7,
		BillingKeyId:   keyID,
		CustomerKey:    "cust_test",
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		NextChargeTime: now.Unix(),
		Status:         WalletAutoRechargeStatusActive,
		ActiveKey:      &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	// A single physical SQLite connection still permits both provider calls to
	// overlap because every final authorization transaction commits before its
	// network call. This models the owner-row serialization used across nodes.
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	providerStarted := make(chan string, 2)
	releaseProvider := make(chan struct{})
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, "billing_key_concurrent_contracts", billingKey)
		require.Equal(t, "cust_test", customerKey)
		providerStarted <- orderID
		<-releaseProvider
		return &TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  "DONE",
			Total:           amount,
			PaymentKey:      "pay_" + orderID,
			ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	}
	previousCharger := tossBillingCharger
	SetTossBillingCharger(charger)
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	renewalResult := make(chan error, 1)
	walletResult := make(chan error, 1)
	go func() {
		renewalResult <- ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails)
	}()
	go func() {
		walletResult <- ProcessWalletAutoRecharge(context.Background(), policy.Id, now, TossBillingMaxFails, charger)
	}()

	orderIDs := make([]string, 0, 2)
	for len(orderIDs) < 2 {
		select {
		case orderID := <-providerStarted:
			orderIDs = append(orderIDs, orderID)
		case <-time.After(5 * time.Second):
			close(releaseProvider)
			t.Fatal("both shared-key provider calls did not pass their final authorization gates")
		}
	}
	close(releaseProvider)
	require.NoError(t, <-renewalResult)
	require.NoError(t, <-walletResult)
	require.NotEqual(t, orderIDs[0], orderIDs[1])
	require.True(t, strings.HasPrefix(orderIDs[0], tossRenewalOpaqueOrderIDPrefix) || strings.HasPrefix(orderIDs[1], tossRenewalOpaqueOrderIDPrefix))
	require.True(t, strings.HasPrefix(orderIDs[0], tossWalletOpaqueOrderIDPrefix) || strings.HasPrefix(orderIDs[1], tossWalletOpaqueOrderIDPrefix))

	var after UserSubscription
	require.NoError(t, DB.First(&after, before.Id).Error)
	require.Greater(t, after.EndTime, before.EndTime)
	require.True(t, after.AutoRenew)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	require.Greater(t, storedPolicy.LastChargeTime, int64(0))
	require.Greater(t, storedPolicy.NextChargeTime, now.Unix())
	var user User
	require.NoError(t, DB.First(&user, 7).Error)
	require.Equal(t, TossCreditQuotaFromKRW(10000), user.Quota)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)

	var walletTopUps int64
	require.NoError(t, DB.Model(&TopUp{}).
		Where("payment_provider = ? AND status = ? AND trade_no = ?", PaymentProviderToss, common.TopUpStatusSuccess, storedPolicy.LastTradeNo).
		Count(&walletTopUps).Error)
	require.Equal(t, int64(1), walletTopUps)
}

func TestBillingDeletedDuringSharedKeyProviderCallsSettlesPaidResultsWithoutRearming(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	originalWalletEnabled := setting.TossWalletAutoRechargeEnabled
	setting.TossWalletAutoRechargeEnabled = true
	t.Cleanup(func() { setting.TossWalletAutoRechargeEnabled = originalWalletEnabled })

	const plainBillingKey = "billing_key_deleted_during_shared_calls"
	keyID := seedTossBillingSubscription(t, plainBillingKey, 0)
	seedTossBillingPlan(t, "Deleted during calls", 1)
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	var before UserSubscription
	require.NoError(t, DB.First(&before, 11).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 7, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 7,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1, NextChargeTime: now.Unix(),
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	providerStarted := make(chan struct{}, 2)
	releaseProvider := make(chan struct{})
	charger := func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		require.Equal(t, plainBillingKey, billingKey)
		providerStarted <- struct{}{}
		<-releaseProvider
		return &TossBillingChargeResult{
			Done: true, ProviderStatus: "DONE", Total: amount,
			PaymentKey: "pay_" + orderID, ProviderPayload: `{"status":"DONE","currency":"KRW"}`,
		}, nil
	}
	previousCharger := tossBillingCharger
	SetTossBillingCharger(charger)
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })
	renewalResult := make(chan error, 1)
	walletResult := make(chan error, 1)
	go func() { renewalResult <- ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails) }()
	go func() {
		walletResult <- ProcessWalletAutoRecharge(context.Background(), policy.Id, now, TossBillingMaxFails, charger)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-providerStarted:
		case <-time.After(5 * time.Second):
			close(releaseProvider)
			t.Fatal("both shared-key provider calls did not start")
		}
	}

	revoked, err := RevokeTossBillingKeyByPlain("", plainBillingKey)
	require.NoError(t, err)
	require.True(t, revoked)
	close(releaseProvider)
	require.NoError(t, <-renewalResult)
	require.NoError(t, <-walletResult)

	var after UserSubscription
	require.NoError(t, DB.First(&after, before.Id).Error)
	require.Greater(t, after.EndTime, before.EndTime,
		"a provider-accepted renewal must still grant its paid period")
	require.False(t, after.AutoRenew)
	require.Zero(t, after.NextBillingTime)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, storedPolicy.Status)
	require.Greater(t, storedPolicy.LastChargeTime, int64(0),
		"a provider-accepted wallet charge must still credit exactly once")
	var user User
	require.NoError(t, DB.First(&user, 7).Error)
	require.Equal(t, TossCreditQuotaFromKRW(10000), user.Quota)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusRevoked, key.Status)

	unexpectedPosts := 0
	require.NoError(t, ProcessTossRenewal(context.Background(), before.Id, TossBillingMaxFails))
	require.NoError(t, ProcessWalletAutoRecharge(context.Background(), policy.Id, now.Add(time.Hour), TossBillingMaxFails,
		func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
			unexpectedPosts++
			return nil, context.Canceled
		}))
	require.Zero(t, unexpectedPosts)
}

func TestOrganizationMembershipRenewalCleanupPreservesOwnedOrganizationWallet(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	keyID := seedTossBillingSubscription(t, "billing_key_org_wallet_survives_legacy_sub", 0)
	require.NoError(t, DB.Create(&Organization{
		Id: 81, Name: "shared-key-organization", OwnerUserId: 7, Status: OrganizationStatusEnabled,
	}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 7).Updates(map[string]interface{}{
		"organization_id":   81,
		"organization_role": OrganizationRoleOwner,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeOrganization, 81, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeOrganization, TargetId: 81,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		NextChargeTime: GetDBTimestamp() + 3600, Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	providerCalls := 0
	previousCharger := tossBillingCharger
	SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*TossBillingChargeResult, error) {
		providerCalls++
		return nil, context.Canceled
	})
	t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

	require.NoError(t, ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails))
	require.Zero(t, providerCalls)
	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 11).Error)
	require.False(t, subscription.AutoRenew)
	require.Zero(t, subscription.NextBillingTime)
	require.Equal(t, "active", subscription.Status, "the already-paid personal period remains valid")
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status,
		"personal subscription cleanup must not cancel the owner's organization wallet")
	require.NotNil(t, storedPolicy.ActiveKey)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status)
}

func TestOrganizationLifecycleLocksSharedKeyBeforeWalletPolicyMutation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_org_lifecycle_lock_order", 0)
	require.NoError(t, DB.Create(&Organization{
		Id: 82, Name: "lock-order-organization", OwnerUserId: 7, Status: OrganizationStatusEnabled,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeOrganization, 82, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeOrganization, TargetId: 82,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	events := make([]string, 0, 8)
	queryCallback := "test:organization_lifecycle_key_lock"
	updateCallback := "test:organization_lifecycle_policy_update"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			events = append(events, "lock:"+tx.Statement.Table)
		}
	}))
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(updateCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "wallet_auto_recharges" {
			events = append(events, "update:"+tx.Statement.Table)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(queryCallback))
		require.NoError(t, DB.Callback().Update().Remove(updateCallback))
	})

	require.NoError(t, DeactivateTossBillingForOrganization(nil, 82))
	organizationLock := -1
	keyLock := -1
	policyUpdate := -1
	for i, event := range events {
		if event == "lock:organizations" && organizationLock < 0 {
			organizationLock = i
		}
		if event == "lock:user_billing_keys" && keyLock < 0 {
			keyLock = i
		}
		if event == "update:wallet_auto_recharges" && policyUpdate < 0 {
			policyUpdate = i
		}
	}
	require.GreaterOrEqual(t, organizationLock, 0, "nil-tx organization cleanup must establish its own lifecycle root lock")
	require.GreaterOrEqual(t, keyLock, 0, "organization lifecycle must lock the affected provider-key identity")
	require.GreaterOrEqual(t, policyUpdate, 0, "organization lifecycle must cancel its wallet policy")
	require.Less(t, organizationLock, keyLock, "organization lock must precede provider-key identity locks")
	require.Less(t, keyLock, policyUpdate, "key identity must be locked before policy mutation to avoid BILLING_DELETED deadlocks")

	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, storedPolicy.Status)
	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 11).Error)
	require.True(t, subscription.AutoRenew)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, key.Status,
		"the organization shutdown must preserve a personal subscription sharing the provider key")
}

func TestUserBillingDeactivationLocksOwnerBeforeWalletPolicyMutation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_user_deactivation_lock_order", 0)
	require.NoError(t, DB.Create(&Organization{
		Id: 83, Name: "user-deactivation-lock-org", OwnerUserId: 7, Status: OrganizationStatusEnabled,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeOrganization, 83, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeOrganization, TargetId: 83,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)

	events := make([]string, 0, 8)
	queryCallback := "test:user_deactivation_owner_lock"
	updateCallback := "test:user_deactivation_policy_update"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			events = append(events, "lock:"+tx.Statement.Table)
		}
	}))
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(updateCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "wallet_auto_recharges" {
			events = append(events, "update:"+tx.Statement.Table)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(queryCallback))
		require.NoError(t, DB.Callback().Update().Remove(updateCallback))
	})

	require.NoError(t, DeactivateTossBillingForUser(nil, 7))
	ownerLock := -1
	organizationLock := -1
	keyLock := -1
	policyUpdate := -1
	for i, event := range events {
		if event == "lock:users" && ownerLock < 0 {
			ownerLock = i
		}
		if event == "lock:organizations" && organizationLock < 0 {
			organizationLock = i
		}
		if event == "lock:user_billing_keys" && keyLock < 0 {
			keyLock = i
		}
		if event == "update:wallet_auto_recharges" && policyUpdate < 0 {
			policyUpdate = i
		}
	}
	require.GreaterOrEqual(t, ownerLock, 0)
	require.GreaterOrEqual(t, organizationLock, 0)
	require.GreaterOrEqual(t, keyLock, 0)
	require.GreaterOrEqual(t, policyUpdate, 0)
	require.Less(t, ownerLock, organizationLock)
	require.Less(t, organizationLock, keyLock)
	require.Less(t, keyLock, policyUpdate,
		"account cleanup must use owner -> org -> key -> policy across competing lifecycle paths")

	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 11).Error)
	require.False(t, subscription.AutoRenew)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, storedPolicy.Status)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}

func TestUserBillingDeactivationCleansFinancialRowsAfterOwnerWasDeleted(t *testing.T) {
	setupTossBillingModelTestDB(t)
	keyID := seedTossBillingSubscription(t, "billing_key_deleted_owner_cleanup", 0)
	require.NoError(t, DB.Create(&Organization{
		Id: 84, Name: "deleted-owner-org", OwnerUserId: 7, Status: OrganizationStatusEnabled,
	}).Error)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeOrganization, 84, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeOrganization, TargetId: 84,
		OwnerUserId: 7, BillingKeyId: keyID, CustomerKey: "cust_test", Amount: 10000,
		IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
	}
	require.NoError(t, DB.Create(&policy).Error)
	require.NoError(t, DB.Delete(&Organization{}, 84).Error)
	require.NoError(t, DB.Unscoped().Delete(&User{}, 7).Error)

	require.NoError(t, DeactivateTossBillingForUser(nil, 7),
		"a missing owner needs no lock, but its surviving financial rows still require cleanup")
	var subscription UserSubscription
	require.NoError(t, DB.First(&subscription, 11).Error)
	require.False(t, subscription.AutoRenew)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusCancelled, storedPolicy.Status)
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
}
