package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int64) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, int64(0), getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestRechargeTossUsesQuotedCreditQuotaWithoutFloatDrift(t *testing.T) {
	truncateTables(t)

	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 3
	common.QuotaPerUnit = 3
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	})

	insertUserForPaymentGuardTest(t, 170, 0)
	quote := QuoteTossTopUp(1, TossTopUpAmountModeKRW, "default")
	require.Equal(t, 1, quote.CreditQuota)
	topUp := &TopUp{
		UserId:          170,
		Amount:          quote.ChargeKRW,
		Money:           quote.CreditAmount,
		Quota:           quote.CreditQuota,
		TradeNo:         "toss-float-drift",
		ProviderOrderId: "toss-float-drift",
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	err := RechargeToss(topUp.TradeNo, "payment-key", "127.0.0.1")
	require.NoError(t, err)

	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
	assert.Equal(t, int64(1), getUserQuotaForPaymentGuardTest(t, 170))
}

func TestManualCompleteTopUp_CreditsOrganizationWalletTarget(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 501, 100)
	org := &Organization{
		Id:          601,
		Name:        "Acme",
		OwnerUserId: 501,
		Quota:       10,
		Status:      OrganizationStatusEnabled,
	}
	require.NoError(t, DB.Create(org).Error)

	topUp := &TopUp{
		UserId:          501,
		Amount:          2,
		Money:           2,
		TradeNo:         "org-wallet-manual-complete",
		PaymentMethod:   PaymentMethodBalance,
		PaymentProvider: PaymentProviderBalance,
		TargetType:      TopUpTargetTypeOrganization,
		TargetId:        org.Id,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	err := ManualCompleteTopUp(topUp.TradeNo, "127.0.0.1")
	require.NoError(t, err)

	reloadedTopUp := GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloadedTopUp)
	assert.Equal(t, common.TopUpStatusSuccess, reloadedTopUp.Status)
	assert.Equal(t, int64(100), getUserQuotaForPaymentGuardTest(t, 501))

	var reloadedOrg Organization
	require.NoError(t, DB.First(&reloadedOrg, org.Id).Error)
	assert.Equal(t, 10+2*int64(common.QuotaPerUnit), reloadedOrg.Quota)
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}

// Ported from upstream 47ba9d2c6 (TestRechargeEpayEnforcesFinalWalletQuotaLimit).
// This fork settles Epay in the controller, so the model-level equivalent is a
// manually completed Epay order, which shares CreditTopUpTarget with every
// provider's settlement path. The fork's ceiling is common.MaxQuota inclusive.
func TestManualCompleteEpayTopUpEnforcesFinalWalletQuotaLimit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	testCases := []struct {
		name         string
		currentQuota int64
		wantErr      bool
		wantQuota    int64
		wantStatus   string
	}{
		{
			name:         "allows exact highest representable wallet balance",
			currentQuota: common.MaxQuota - 1_000_000,
			wantQuota:    common.MaxQuota,
			wantStatus:   common.TopUpStatusSuccess,
		},
		{
			name:         "rejects balance above the wallet ceiling",
			currentQuota: common.MaxQuota - 1_000_000 + 1,
			wantErr:      true,
			wantQuota:    common.MaxQuota - 1_000_000 + 1,
			wantStatus:   common.TopUpStatusPending,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 506, tc.currentQuota)
			insertTopUpForPaymentGuardTest(t, "EPAYTESTWALLETLIMIT", 506, PaymentProviderEpay)

			err := ManualCompleteTopUp("EPAYTESTWALLETLIMIT", "127.0.0.1")
			if tc.wantErr {
				require.ErrorIs(t, err, ErrTopUpQuotaCapacityExceeded)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, 506))
			assert.Equal(t, tc.wantStatus, getTopUpStatusForPaymentGuardTest(t, "EPAYTESTWALLETLIMIT"))
		})
	}
}

func TestValidateTopUpQuotaCapacityMatchesSettlementCeiling(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 507, common.MaxQuota-1_000_000)

	require.NoError(t, ValidateTopUpQuotaCapacity(507, 1_000_000))
	require.ErrorIs(t, ValidateTopUpQuotaCapacity(507, 1_000_001), ErrTopUpQuotaCapacityExceeded)
	require.ErrorIs(t, ValidateTopUpQuotaCapacity(507, 0), ErrInvalidTopUpQuota)
	require.ErrorIs(t, ValidateTopUpQuotaCapacity(507, -1), ErrInvalidTopUpQuota)
	require.ErrorIs(t, ValidateTopUpQuotaCapacity(507, common.MaxQuota+1), ErrTopUpQuotaCapacityExceeded)
	require.Error(t, ValidateTopUpQuotaCapacity(999_999, 1))
}

func TestValidateTopUpQuotaCapacityAllowsNegativeBalanceDebtRepayment(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 508, -10)

	// A wallet in debt can absorb a full-ceiling credit only up to MaxQuota-(-10).
	require.NoError(t, ValidateTopUpQuotaCapacity(508, common.MaxQuota))
}

func TestValidateOrganizationWalletCapacityReadsOrganizationQuota(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 509, common.MaxQuota)
	require.NoError(t, DB.Create(&Organization{
		Id:          609,
		Name:        "capacity-org",
		OwnerUserId: 509,
		Quota:       common.MaxQuota - 1_000_000,
		Status:      OrganizationStatusEnabled,
	}).Error)

	require.NoError(t, ValidateOrganizationWalletCapacity(609, 1_000_000))
	require.ErrorIs(t, ValidateOrganizationWalletCapacity(609, 1_000_001), ErrTopUpQuotaCapacityExceeded)
	require.ErrorIs(t, ValidateOrganizationWalletCapacity(609, 0), ErrInvalidTopUpQuota)
	require.Error(t, ValidateOrganizationWalletCapacity(999_999, 1))

	require.ErrorIs(t, ValidateTopUpTargetCapacity(TopUpTargetTypeOrganization, 609, 1_000_001), ErrTopUpQuotaCapacityExceeded)
	require.NoError(t, ValidateTopUpTargetCapacity(TopUpTargetTypeOrganization, 609, 1_000_000))
	require.ErrorIs(t, ValidateTopUpTargetCapacity(TopUpTargetTypeUser, 509, 1), ErrTopUpQuotaCapacityExceeded)
}
