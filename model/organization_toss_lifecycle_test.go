package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func setupOrganizationTossLifecycleTest(t *testing.T, userID, organizationID int) {
	t.Helper()
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}))
	require.NoError(t, DB.Create(&User{
		Id:       userID,
		Username: fmt.Sprintf("org-toss-user-%d", userID),
		Password: "password",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Group:    "default",
		AffCode:  fmt.Sprintf("org-toss-aff-%d", userID),
	}).Error)
	require.NoError(t, DB.Create(&Organization{
		Id:          organizationID,
		Name:        fmt.Sprintf("org-toss-%d", organizationID),
		OwnerUserId: 999999,
		Status:      OrganizationStatusEnabled,
	}).Error)
}

func TestOrganizationJoinPreservesLegacyPendingTossTopUpEvidence(t *testing.T) {
	tests := []struct {
		name                string
		providerOrderSuffix string
		providerOrderTime   int64
	}{
		{name: "recorded payment key", providerOrderSuffix: "_payment_key", providerOrderTime: 1234},
		{name: "legacy wallet charged marker", providerOrderSuffix: ":charged", providerOrderTime: 0},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := 9100 + index
			organizationID := 9200 + index
			setupOrganizationTossLifecycleTest(t, userID, organizationID)
			tradeNo := fmt.Sprintf("legacy_personal_toss_%d", index)
			topUp := &TopUp{
				UserId:            userID,
				TargetType:        TopUpTargetTypeUser,
				TargetId:          userID,
				Amount:            10000,
				TradeNo:           tradeNo,
				ProviderOrderId:   tradeNo + tt.providerOrderSuffix,
				ProviderOrderTime: tt.providerOrderTime,
				PaymentMethod:     PaymentMethodToss,
				PaymentProvider:   PaymentProviderToss,
				Status:            common.TopUpStatusPending,
			}
			require.NoError(t, DB.Create(topUp).Error)

			err := AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember)
			require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)

			var user User
			require.NoError(t, DB.First(&user, userID).Error)
			require.Zero(t, user.OrganizationId)
			var persisted TopUp
			require.NoError(t, DB.First(&persisted, topUp.Id).Error)
			require.Equal(t, common.TopUpStatusPending, persisted.Status)
			require.Equal(t, topUp.ProviderOrderId, persisted.ProviderOrderId)
		})
	}
}

func TestOrganizationJoinPreservesLegacyAttachedTossSubscriptionOrder(t *testing.T) {
	const (
		userID         = 9301
		organizationID = 9302
	)
	setupOrganizationTossLifecycleTest(t, userID, organizationID)
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          77,
		TradeNo:         "legacy_attached_subscription",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		BillingKeyId:    4455,
	}
	require.NoError(t, DB.Create(order).Error)

	err := AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember)
	require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)

	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Zero(t, user.OrganizationId)
}

func TestOrganizationJoinPreservesIncompleteRecurringProtocolEvidence(t *testing.T) {
	for _, kind := range []string{"wallet", "subscription"} {
		t.Run(kind, func(t *testing.T) {
			userID := 9320
			organizationID := 9321
			setupOrganizationTossLifecycleTest(t, userID, organizationID)
			if kind == "wallet" {
				require.NoError(t, DB.Create(&TopUp{
					UserId: userID, TargetType: TopUpTargetTypeUser, TargetId: userID,
					Amount: 10000, TradeNo: "opaque_wallet_lifecycle_evidence", ProviderOrderId: "opaque_wallet_lifecycle_evidence",
					PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
					Status: common.TopUpStatusPending, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
				}).Error)
			} else {
				require.NoError(t, DB.Create(&SubscriptionOrder{
					UserId: userID, PlanId: 77, TradeNo: "opaque_subscription_lifecycle_evidence",
					PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
					Status: common.TopUpStatusPending, RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
				}).Error)
			}

			err := AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember)
			require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)
			var user User
			require.NoError(t, DB.First(&user, userID).Error)
			require.Zero(t, user.OrganizationId)
			if kind == "wallet" {
				var row TopUp
				require.NoError(t, DB.Where("trade_no = ?", "opaque_wallet_lifecycle_evidence").First(&row).Error)
				require.Equal(t, common.TopUpStatusPending, row.Status)
				require.Equal(t, tossWalletOrderIDVersionOpaque, row.WalletOrderIdVersion)
			} else {
				var row SubscriptionOrder
				require.NoError(t, DB.Where("trade_no = ?", "opaque_subscription_lifecycle_evidence").First(&row).Error)
				require.Equal(t, common.TopUpStatusPending, row.Status)
				require.Equal(t, tossRecurringOrderIDVersionOpaque, row.RenewalOrderIdVersion)
			}
		})
	}
}

func TestOrganizationJoinAllowsTerminalCompleteWalletProtocolEvidence(t *testing.T) {
	tests := []struct {
		name    string
		version int
		status  string
	}{
		{name: "legacy failed", version: tossWalletOrderIDVersionLegacy, status: common.TopUpStatusFailed},
		{name: "opaque success", version: tossWalletOrderIDVersionOpaque, status: common.TopUpStatusSuccess},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID := 9330 + index
			organizationID := 9340 + index
			setupOrganizationTossLifecycleTest(t, userID, organizationID)
			policyID := 700 + index
			cycleKey := fmt.Sprintf("s:%d", GetDBTimestamp()-3600)
			attempt := 0
			require.NoError(t, DB.Create(&TopUp{
				UserId: userID, TargetType: TopUpTargetTypeUser, TargetId: userID,
				Amount: 10000, TradeNo: fmt.Sprintf("terminal_wallet_protocol_%d", index),
				PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
				Status: test.status, WalletOrderIdVersion: test.version,
				WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey,
				WalletAutoRechargeAttempt: &attempt,
			}).Error)

			require.NoError(t, AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember))
			var user User
			require.NoError(t, DB.First(&user, userID).Error)
			require.Equal(t, organizationID, user.OrganizationId)
			var persisted TopUp
			require.NoError(t, DB.Where("user_id = ?", userID).First(&persisted).Error)
			require.Equal(t, test.status, persisted.Status)
			require.Equal(t, test.version, persisted.WalletOrderIdVersion)
		})
	}
}

func TestOrganizationJoinWaitsForLegacyWalletAutoRechargeOrphanReconciliation(t *testing.T) {
	const (
		userID         = 9351
		organizationID = 9352
	)
	setupOrganizationTossLifecycleTest(t, userID, organizationID)
	keyID, err := StoreTossBillingKeyWithSecret(
		userID,
		"legacy-wallet-customer",
		"legacy-wallet-billing-key",
		"card",
		"****9351",
		setting.TossBillingSecretKey,
	)
	require.NoError(t, err)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, userID, WalletAutoRechargeTypeScheduled)
	policy := &WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeScheduled,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       userID,
		OwnerUserId:    userID,
		BillingKeyId:   keyID,
		Amount:         10000,
		IntervalUnit:   WalletAutoRechargeIntervalMonth,
		IntervalValue:  1,
		Status:         WalletAutoRechargeStatusActive,
		FailCount:      1,
		LastError:      "legacy provider response timeout",
		ActiveKey:      &activeKey,
		NextChargeTime: GetDBTimestamp(),
		CustomerKey:    "legacy-wallet-customer",
	}
	require.NoError(t, DB.Create(policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_legacy_orphan", policy.Id)
	topUp := &TopUp{
		UserId:          userID,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        userID,
		Amount:          10000,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		CreateTime:      GetDBTimestamp(),
		Status:          common.TopUpStatusPending,
		// This is the rolling-upgrade shape: the old worker could have POSTed,
		// while every provider_attempted/claim column has its new zero value.
		ProviderAttempted: false,
	}
	require.NoError(t, DB.Create(topUp).Error)

	err = AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember)
	require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
	var storedPolicy WalletAutoRecharge
	require.NoError(t, DB.First(&storedPolicy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, storedPolicy.Status)
	var storedKey UserBillingKey
	require.NoError(t, DB.First(&storedKey, keyID).Error)
	require.Equal(t, BillingKeyStatusActive, storedKey.Status)

	providerSettlementWon, err := closeWalletAutoRechargeAttemptAfterNotFound(
		policy.Id,
		tradeNo,
		false,
		"authoritative provider lookup returned not found",
	)
	require.NoError(t, err)
	require.False(t, providerSettlementWon)
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)

	require.NoError(t, AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember))
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, organizationID, user.OrganizationId)
}

func TestOrganizationJoinExpiresOnlyPristineTossOrderAndSucceedsAfterTerminalAttempt(t *testing.T) {
	const (
		userID         = 9401
		organizationID = 9402
	)
	setupOrganizationTossLifecycleTest(t, userID, organizationID)
	pristine := &TopUp{
		UserId: userID, TargetType: TopUpTargetTypeUser, TargetId: userID,
		Amount: 10000, TradeNo: "pristine_personal_toss", ProviderOrderId: "pristine_personal_toss",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(pristine).Error)
	attempted := &TopUp{
		UserId: userID, TargetType: TopUpTargetTypeUser, TargetId: userID,
		Amount: 10000, TradeNo: "attempted_personal_toss", ProviderOrderId: "attempted_personal_toss",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		ProviderAttempted: true, ProviderClaimTime: GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(attempted).Error)

	err := AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember)
	require.True(t, errors.Is(err, ErrPersonalTossBillingInFlight))
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", attempted.Id).Updates(map[string]interface{}{
		"status":               common.TopUpStatusFailed,
		"provider_attempted":   false,
		"provider_claim_token": "",
		"provider_claim_time":  0,
	}).Error)

	require.NoError(t, AssignUserToOrganizationWithBillingLifecycle(userID, organizationID, OrganizationRoleMember))
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, organizationID, user.OrganizationId)
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, pristine.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)
}
