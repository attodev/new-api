package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func setupOrganizationTossTargetLifecycleTest(t *testing.T) (*User, *Organization) {
	t.Helper()
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &TossPaymentEvent{}))
	owner := &User{
		Id:               71,
		Username:         "organization-toss-target-owner",
		Password:         "password",
		Status:           common.UserStatusEnabled,
		Role:             common.RoleCommonUser,
		Group:            "default",
		OrganizationId:   81,
		OrganizationRole: OrganizationRoleOwner,
		AffCode:          "organization-toss-target-owner-aff",
	}
	require.NoError(t, DB.Create(owner).Error)
	organization := &Organization{
		Id:          81,
		Name:        "Organization Toss Target Lifecycle",
		OwnerUserId: owner.Id,
		Status:      OrganizationStatusEnabled,
	}
	require.NoError(t, DB.Create(organization).Error)
	return owner, organization
}

func TestOrganizationLifecycleBlocksProviderAttemptedTossTopUp(t *testing.T) {
	owner, organization := setupOrganizationTossTargetLifecycleTest(t)
	tradeNo := "toss_org_provider_attempted"
	topUp := &TopUp{
		UserId:            owner.Id,
		TargetType:        TopUpTargetTypeOrganization,
		TargetId:          organization.Id,
		Amount:            10000,
		TradeNo:           tradeNo,
		ProviderOrderId:   tradeNo,
		ProviderAttempted: true,
		ProviderClaimTime: GetDBTimestamp(),
		PaymentMethod:     PaymentMethodToss,
		PaymentProvider:   PaymentProviderToss,
		Status:            common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	err := UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{
		"status": OrganizationStatusDisabled,
	})
	require.ErrorIs(t, err, ErrOrganizationTossPaymentInFlight)

	var persistedOrganization Organization
	require.NoError(t, DB.First(&persistedOrganization, organization.Id).Error)
	require.Equal(t, OrganizationStatusEnabled, persistedOrganization.Status)
	var persistedTopUp TopUp
	require.NoError(t, DB.First(&persistedTopUp, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persistedTopUp.Status)

	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
		"status":               common.TopUpStatusFailed,
		"provider_attempted":   false,
		"provider_claim_token": "",
		"provider_claim_time":  0,
	}).Error)
	require.NoError(t, UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{
		"status": OrganizationStatusDisabled,
	}))
}

func TestOrganizationLifecycleExpiresOnlyProviderPristineGeneralTopUp(t *testing.T) {
	owner, organization := setupOrganizationTossTargetLifecycleTest(t)
	tradeNo := "toss_org_pristine_checkout"
	topUp := &TopUp{
		UserId:          owner.Id,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        organization.Id,
		Amount:          10000,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		// Checkout creation snapshots a credential before any provider call; it
		// is still safe to erase while every activity marker remains pristine.
		ProviderCredential: "encrypted-checkout-snapshot",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	require.NoError(t, UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{
		"owner_user_id": 99,
	}))
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)
	require.Empty(t, persisted.ProviderCredential)
}

func TestOrganizationLifecycleBlocksOpaqueWalletTopUpBeforeAttemptMarker(t *testing.T) {
	owner, organization := setupOrganizationTossTargetLifecycleTest(t)
	policyID := 811
	cycleKey := "s:1782840000"
	attempt := 0
	topUp := &TopUp{
		UserId: owner.Id, TargetType: TopUpTargetTypeOrganization, TargetId: organization.Id,
		Amount: 10000, TradeNo: "twa_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey, WalletAutoRechargeAttempt: &attempt,
	}
	require.NoError(t, DB.Create(topUp).Error)

	err := UpdateOrganizationFieldsWithBillingLifecycle(organization.Id, map[string]interface{}{"owner_user_id": 99})
	require.ErrorIs(t, err, ErrOrganizationTossPaymentInFlight)
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
}

func TestOrganizationDeleteWaitsForUnresolvedTossPaymentEvent(t *testing.T) {
	owner, organization := setupOrganizationTossTargetLifecycleTest(t)
	tradeNo := "wallet_auto_991_org_reconciliation"
	topUp := &TopUp{
		UserId:          owner.Id,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        organization.Id,
		Amount:          10000,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo + ":done-mismatch",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusFailed,
	}
	require.NoError(t, DB.Create(topUp).Error)
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "organization-delete-unresolved-toss-event",
		EventType:            TossPaymentEventTypeFulfillment,
		OrderId:              tradeNo,
		PaymentKey:           "payment-key-needing-reconciliation",
		Status:               "DONE",
		OriginalAmount:       topUp.Amount,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	err = DeleteOrganization(organization.Id)
	require.ErrorIs(t, err, ErrOrganizationTossPaymentInFlight)
	var persisted Organization
	require.NoError(t, DB.First(&persisted, organization.Id).Error)

	require.NoError(t, ResolveTossPaymentEvents(tradeNo, TossPaymentEventTypeFulfillment))
	require.NoError(t, DeleteOrganization(organization.Id))
}

func setupUserTossDeletionLifecycleTest(t *testing.T, userID int) *User {
	t.Helper()
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}, &TossPaymentEvent{}))
	user := &User{
		Id:       userID,
		Username: "toss-deletion-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		Group:    "default",
		AffCode:  "toss-deletion-user-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func TestUserDeletionWaitsForProviderAttemptedTossTopUp(t *testing.T) {
	for _, hardDelete := range []bool{false, true} {
		name := "soft-delete"
		if hardDelete {
			name = "hard-delete"
		}
		t.Run(name, func(t *testing.T) {
			user := setupUserTossDeletionLifecycleTest(t, 171)
			tradeNo := "toss_user_delete_provider_attempted"
			topUp := &TopUp{
				UserId:            user.Id,
				TargetType:        TopUpTargetTypeUser,
				TargetId:          user.Id,
				Amount:            10000,
				TradeNo:           tradeNo,
				ProviderOrderId:   tradeNo,
				ProviderAttempted: true,
				ProviderClaimTime: GetDBTimestamp(),
				PaymentMethod:     PaymentMethodToss,
				PaymentProvider:   PaymentProviderToss,
				Status:            common.TopUpStatusPending,
			}
			require.NoError(t, DB.Create(topUp).Error)

			var err error
			if hardDelete {
				err = user.HardDelete()
			} else {
				err = user.Delete()
			}
			require.ErrorIs(t, err, ErrUserTossPaymentInFlight)
			require.NoError(t, DB.First(&User{}, user.Id).Error)

			require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(map[string]interface{}{
				"status":               common.TopUpStatusFailed,
				"provider_attempted":   false,
				"provider_claim_token": "",
				"provider_claim_time":  0,
			}).Error)
			if hardDelete {
				require.NoError(t, user.HardDelete())
			} else {
				require.NoError(t, user.Delete())
			}
		})
	}
}

func TestUserHardDeleteWaitsForAttemptedTossSubscriptionCharge(t *testing.T) {
	user := setupUserTossDeletionLifecycleTest(t, 172)
	order := &SubscriptionOrder{
		UserId:                   user.Id,
		PlanId:                   91,
		TradeNo:                  "toss_sub_delete_provider_attempted",
		PaymentMethod:            PaymentMethodToss,
		PaymentProvider:          PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		BillingAttempted:         true,
		BillingAttemptCredential: "encrypted-attempt-secret",
		BillingAttemptTime:       GetDBTimestamp(),
		BillingClaimTime:         GetDBTimestamp(),
	}
	require.NoError(t, DB.Create(order).Error)

	err := user.HardDelete()
	require.ErrorIs(t, err, ErrUserTossPaymentInFlight)
	require.NoError(t, DB.First(&User{}, user.Id).Error)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
		"status":                     common.TopUpStatusFailed,
		"billing_attempted":          false,
		"billing_attempt_credential": "",
		"billing_attempt_time":       0,
		"billing_claim_time":         0,
	}).Error)
	require.NoError(t, user.HardDelete())
}

func TestUserDeleteExpiresProviderPristineTossCheckout(t *testing.T) {
	user := setupUserTossDeletionLifecycleTest(t, 173)
	tradeNo := "toss_user_delete_pristine"
	topUp := &TopUp{
		UserId:             user.Id,
		TargetType:         TopUpTargetTypeUser,
		TargetId:           user.Id,
		Amount:             10000,
		TradeNo:            tradeNo,
		ProviderOrderId:    tradeNo,
		ProviderCredential: "encrypted-checkout-snapshot",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)

	require.NoError(t, user.Delete())
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)
	require.Empty(t, persisted.ProviderCredential)
}

func TestUserDeleteBlocksOpaqueWalletTopUpBeforeAttemptMarker(t *testing.T) {
	user := setupUserTossDeletionLifecycleTest(t, 174)
	policyID := 812
	cycleKey := "s:1782840001"
	attempt := 0
	topUp := &TopUp{
		UserId: user.Id, TargetType: TopUpTargetTypeUser, TargetId: user.Id,
		Amount: 10000, TradeNo: "twa_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		Status: common.TopUpStatusPending, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey, WalletAutoRechargeAttempt: &attempt,
	}
	require.NoError(t, DB.Create(topUp).Error)

	err := user.Delete()
	require.ErrorIs(t, err, ErrUserTossPaymentInFlight)
	require.NoError(t, DB.First(&User{}, user.Id).Error)
	var persisted TopUp
	require.NoError(t, DB.First(&persisted, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
}

func TestUserHardDeleteMovesAttemptedBillingIssueToRecoverableCleanup(t *testing.T) {
	user := setupUserTossDeletionLifecycleTest(t, 174)
	const (
		tradeNo     = "toss_sub_delete_attempted_issue"
		authKey     = "auth-key-issued-before-user-delete"
		customerKey = "cust_delete_issue"
		secretKey   = "sk_delete_issue"
	)
	encryptedAuthKey, err := common.EncryptString(authKey)
	require.NoError(t, err)
	encryptedSecret, err := EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	order := &SubscriptionOrder{
		UserId:                  user.Id,
		PlanId:                  92,
		TradeNo:                 tradeNo,
		PaymentMethod:           PaymentMethodToss,
		PaymentProvider:         PaymentProviderToss,
		Status:                  common.TopUpStatusPending,
		ProviderCredential:      encryptedSecret,
		BillingClaimToken:       "live-issue-owner",
		BillingClaimTime:        GetDBTimestamp(),
		BillingIssueAuthKey:     encryptedAuthKey,
		BillingIssueAuthKeyHash: common.GenerateHMAC(authKey),
		BillingIssueCustomerKey: customerKey,
		BillingIssueAttempted:   true,
	}
	require.NoError(t, DB.Create(order).Error)

	// ISSUE can create a key but cannot charge money. Deletion succeeds while
	// preserving exactly the snapshot needed by the cleanup-only reconciler.
	require.NoError(t, user.HardDelete())
	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)
	require.True(t, persisted.BillingIssueAttempted)
	require.Equal(t, encryptedAuthKey, persisted.BillingIssueAuthKey)
	require.Equal(t, "live-issue-owner", persisted.BillingClaimToken)

	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	claimToken, claimed, err := ClaimStoredTossSubscriptionBillingIssueCleanup(tradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	recoveredAuthKey, recoveredCustomerKey, recoveredSecret, err := GetClaimedTossSubscriptionBillingIssueCleanup(tradeNo, claimToken)
	require.NoError(t, err)
	require.Equal(t, authKey, recoveredAuthKey)
	require.Equal(t, customerKey, recoveredCustomerKey)
	require.Equal(t, secretKey, recoveredSecret)
}

func TestUserHardDeleteQueuesAttachedUnattemptedBillingKeyWithoutCharging(t *testing.T) {
	user := setupUserTossDeletionLifecycleTest(t, 175)
	keyID, err := StoreTossBillingKeyWithSecret(
		user.Id,
		"cust_delete_attached",
		"billing-key-delete-attached",
		"card",
		"****0175",
		"sk_delete_attached",
	)
	require.NoError(t, err)
	var billingKey UserBillingKey
	require.NoError(t, DB.First(&billingKey, keyID).Error)
	order := &SubscriptionOrder{
		UserId:                       user.Id,
		PlanId:                       93,
		TradeNo:                      "toss_sub_delete_attached_unattempted",
		PaymentMethod:                PaymentMethodToss,
		PaymentProvider:              PaymentProviderToss,
		Status:                       common.TopUpStatusPending,
		BillingKeyId:                 keyID,
		BillingClaimToken:            "live-attached-owner",
		BillingClaimTime:             GetDBTimestamp(),
		BillingAttempted:             false,
		ProviderCredential:           billingKey.ProviderCredential,
		BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
	}
	require.NoError(t, DB.Create(order).Error)

	require.NoError(t, user.HardDelete())
	var key UserBillingKey
	require.NoError(t, DB.First(&key, keyID).Error)
	require.Equal(t, BillingKeyStatusPendingRevocation, key.Status)
	var persisted SubscriptionOrder
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, persisted.Status)
	require.False(t, persisted.BillingAttempted)

	// The ordinary no-attempt cleanup claim still works without the user row;
	// it can expire the order, but it has no authorization to POST a charge.
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("billing_claim_time", GetDBTimestamp()-tossSubscriptionBillingClaimTTLSeconds-1).Error)
	claimToken, claimed, err := ClaimTossUnattemptedSubscriptionChargeOrder(order.TradeNo)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(order.TradeNo, claimToken))
	require.NoError(t, DB.First(&persisted, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, persisted.Status)
}
