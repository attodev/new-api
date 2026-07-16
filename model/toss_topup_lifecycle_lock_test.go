package model

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func firstTossLifecycleEvent(events []string, target string) int {
	for index, event := range events {
		if event == target {
			return index
		}
	}
	return -1
}

func captureTossTopUpLifecycleOrder(t *testing.T) *[]string {
	t.Helper()
	events := make([]string, 0, 12)
	queryCallback := "test:toss_topup_lifecycle_lock_order"
	updateCallback := "test:toss_topup_lifecycle_update_order"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			events = append(events, "lock:"+tx.Statement.Table)
		}
	}))
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(updateCallback, func(tx *gorm.DB) {
		events = append(events, "update:"+tx.Statement.Table)
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(queryCallback))
		require.NoError(t, DB.Callback().Update().Remove(updateCallback))
	})
	return &events
}

func seedOrganizationTossTopUpForLockTest(t *testing.T, ownerID, organizationID int, tradeNo, status string) (*User, *Organization, *TopUp) {
	t.Helper()
	owner := &User{
		Id:               ownerID,
		Username:         "toss-topup-lock-owner-" + tradeNo,
		Password:         "password",
		Status:           common.UserStatusEnabled,
		Role:             common.RoleCommonUser,
		Group:            "default",
		OrganizationId:   organizationID,
		OrganizationRole: OrganizationRoleOwner,
		AffCode:          "toss-topup-lock-aff-" + tradeNo,
	}
	require.NoError(t, DB.Create(owner).Error)
	organization := &Organization{
		Id:          organizationID,
		Name:        "Toss top-up lock " + tradeNo,
		OwnerUserId: ownerID,
		Status:      OrganizationStatusEnabled,
	}
	require.NoError(t, DB.Create(organization).Error)
	topUp := &TopUp{
		UserId:          ownerID,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        organizationID,
		Amount:          10000,
		Quota:           777,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          status,
	}
	require.NoError(t, DB.Create(topUp).Error)
	return owner, organization, topUp
}

func requireOwnerOrganizationOrderLocks(t *testing.T, events []string) {
	t.Helper()
	ownerLock := firstTossLifecycleEvent(events, "lock:users")
	organizationLock := firstTossLifecycleEvent(events, "lock:organizations")
	orderLock := firstTossLifecycleEvent(events, "lock:top_ups")
	require.GreaterOrEqual(t, ownerLock, 0, "payer row must be locked")
	require.GreaterOrEqual(t, organizationLock, 0, "organization row must be locked")
	require.GreaterOrEqual(t, orderLock, 0, "top-up row must be locked")
	require.Less(t, ownerLock, organizationLock, "payer lock must precede organization lock")
	require.Less(t, organizationLock, orderLock, "organization lock must precede top-up lock")
}

func TestRechargeTossLocksOwnerOrganizationBeforeOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Log{}))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8101, 8102, "toss_org_settlement_lock_order", common.TopUpStatusPending,
	)
	topUp.ProviderOrderId = "pay_org_settlement_lock_order"
	topUp.ProviderAttempted = true
	require.NoError(t, DB.Save(topUp).Error)

	events := captureTossTopUpLifecycleOrder(t)
	require.NoError(t, RechargeToss(topUp.TradeNo, topUp.ProviderOrderId, "127.0.0.1"))
	requireOwnerOrganizationOrderLocks(t, *events)

	require.NoError(t, DB.First(organization, organization.Id).Error)
	require.Equal(t, 777, organization.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
}

func TestManualCompleteTossRequiresProviderReconciliation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Log{}))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8106, 8107, "toss_org_manual_settlement_lock_order", common.TopUpStatusPending,
	)

	err := ManualCompleteTopUp(topUp.TradeNo, "127.0.0.1")
	require.ErrorContains(t, err, "cannot be completed manually")
	require.NoError(t, DB.First(organization, organization.Id).Error)
	require.Zero(t, organization.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func TestTossRecoveryAndLateSettlementPreserveDisabledOrganization(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Log{}))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8111, 8112, "toss_org_late_recovery_lock_order", common.TopUpStatusExpired,
	)
	require.NoError(t, DB.Model(&Organization{}).Where("id = ?", organization.Id).
		Update("status", OrganizationStatusDisabled).Error)

	events := captureTossTopUpLifecycleOrder(t)
	const paymentKey = "pay_org_late_recovery_lock_order"
	require.NoError(t, RecoverTossTopUpPaymentKeyWithContext(context.Background(), topUp.TradeNo, paymentKey))
	requireOwnerOrganizationOrderLocks(t, *events)

	*events = (*events)[:0]
	require.NoError(t, RechargeToss(topUp.TradeNo, paymentKey, "127.0.0.1"),
		"a provider-confirmed payment must still credit a target disabled after the charge")
	requireOwnerOrganizationOrderLocks(t, *events)

	require.NoError(t, DB.First(organization, organization.Id).Error)
	require.Equal(t, OrganizationStatusDisabled, organization.Status)
	require.Equal(t, 777, organization.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, paymentKey, topUp.ProviderOrderId)
}

func TestTossRecoveryDoesNotReopenOrderAfterTargetDeletion(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8121, 8122, "toss_org_deleted_recovery_target", common.TopUpStatusExpired,
	)
	require.NoError(t, DB.Delete(&Organization{}, organization.Id).Error)

	err := RecoverTossTopUpPaymentKeyWithContext(context.Background(), topUp.TradeNo, "pay_deleted_recovery_target")
	require.ErrorIs(t, err, ErrTossTopUpSettlementTargetMissing)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
	require.Equal(t, topUp.TradeNo, topUp.ProviderOrderId)
}

func TestTossRecoveryAfterTargetDeletionRemainsIdempotentForSettledOrder(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8126, 8127, "toss_org_deleted_settled_recovery", common.TopUpStatusSuccess,
	)
	require.NoError(t, DB.Delete(&Organization{}, organization.Id).Error)

	const paymentKey = "pay_deleted_settled_recovery"
	require.NoError(t, RecoverTossTopUpPaymentKeyWithContext(context.Background(), topUp.TradeNo, paymentKey),
		"an already-settled row needs no surviving quota target to reconcile provider metadata")
	require.NoError(t, RechargeToss(topUp.TradeNo, paymentKey, "toss-deleted-target-idempotency"),
		"an already-settled row must remain idempotent without crediting a deleted target")
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, paymentKey, topUp.ProviderOrderId)
}

func TestOrganizationDeleteLocksOwnerBeforeOrganizationAndTopUpMutation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	owner, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8131, 8132, "toss_org_delete_lock_order", common.TopUpStatusPending,
	)

	events := captureTossTopUpLifecycleOrder(t)
	require.NoError(t, DeleteOrganization(organization.Id))
	ownerLock := firstTossLifecycleEvent(*events, "lock:users")
	organizationLock := firstTossLifecycleEvent(*events, "lock:organizations")
	topUpUpdate := firstTossLifecycleEvent(*events, "update:top_ups")
	require.GreaterOrEqual(t, ownerLock, 0)
	require.GreaterOrEqual(t, organizationLock, 0)
	require.GreaterOrEqual(t, topUpUpdate, 0)
	require.Less(t, ownerLock, organizationLock,
		"organization deletion must not invert checkout's owner -> organization locks")
	require.Less(t, organizationLock, topUpUpdate)

	require.ErrorIs(t, DB.First(&Organization{}, organization.Id).Error, gorm.ErrRecordNotFound)
	require.NoError(t, DB.First(owner, owner.Id).Error)
	require.Zero(t, owner.OrganizationId)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
}

func TestUserDeletionLocksOrganizationBeforeTopUpMutation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	owner, _, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8141, 8142, "toss_user_delete_org_topup_lock_order", common.TopUpStatusPending,
	)

	events := captureTossTopUpLifecycleOrder(t)
	require.NoError(t, owner.Delete())
	ownerLock := firstTossLifecycleEvent(*events, "lock:users")
	organizationLock := firstTossLifecycleEvent(*events, "lock:organizations")
	topUpUpdate := firstTossLifecycleEvent(*events, "update:top_ups")
	require.GreaterOrEqual(t, ownerLock, 0)
	require.GreaterOrEqual(t, organizationLock, 0)
	require.GreaterOrEqual(t, topUpUpdate, 0)
	require.Less(t, ownerLock, organizationLock)
	require.Less(t, organizationLock, topUpUpdate,
		"user deletion must prelock organization roots before expiring orders")
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
}

func TestRechargeTossOrganizationLifecycleRaceSettlesExactlyOnce(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&Organization{},
		&WalletAutoRecharge{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPlan{},
	))
	_, organization, topUp := seedOrganizationTossTopUpForLockTest(
		t, 8151, 8152, "toss_org_settlement_lifecycle_race", common.TopUpStatusPending,
	)
	topUp.ProviderOrderId = "pay_org_settlement_lifecycle_race"
	topUp.ProviderAttempted = true
	topUp.ProviderClaimTime = GetDBTimestamp()
	require.NoError(t, DB.Save(topUp).Error)

	const settlementWorkers = 6
	start := make(chan struct{})
	settlementErrors := make([]error, settlementWorkers)
	var lifecycleError error
	var wg sync.WaitGroup
	wg.Add(settlementWorkers + 1)
	for index := 0; index < settlementWorkers; index++ {
		go func(worker int) {
			defer wg.Done()
			<-start
			settlementErrors[worker] = RechargeToss(topUp.TradeNo, topUp.ProviderOrderId, "127.0.0.1")
		}(index)
	}
	go func() {
		defer wg.Done()
		<-start
		lifecycleError = UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{
			"status": OrganizationStatusDisabled,
		})
	}()
	close(start)
	wg.Wait()

	for _, err := range settlementErrors {
		require.NoError(t, err)
	}
	if lifecycleError != nil && !errors.Is(lifecycleError, ErrOrganizationTossPaymentInFlight) {
		message := strings.ToLower(lifecycleError.Error())
		require.True(t, strings.Contains(message, "database is locked") || strings.Contains(message, "sqlite_busy"),
			"unexpected lifecycle error: %v", lifecycleError)
	}
	// If lifecycle lost to the live attempt (or SQLite write contention), it is
	// safe immediately after the exactly-once settlement commits.
	require.NoError(t, UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{
		"status": OrganizationStatusDisabled,
	}))

	require.NoError(t, DB.First(organization, organization.Id).Error)
	require.Equal(t, OrganizationStatusDisabled, organization.Status)
	require.Equal(t, 777, organization.Quota)
	require.NoError(t, DB.First(topUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
}
