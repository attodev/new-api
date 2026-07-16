package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBackfillLegacyTossRenewalContractsRejectsUnloadedPersistedUnitPrice(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &Option{}))
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossUnitPrice":      "4321",
	})
	require.NoError(t, DB.Create(&UserSubscription{
		Id: 991, UserId: 991, PlanId: 991, Status: "active", AutoRenew: true, BillingKeyId: 991,
	}).Error)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1234
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	backfilled, disabled, err := BackfillLegacyTossRenewalContractsAtCurrentTerms()
	require.True(t, errors.Is(err, ErrTossConfigRevisionStale))
	require.Zero(t, backfilled)
	require.Zero(t, disabled)
}

func TestBackfillLegacyTossRenewalContractsFreezesDeploymentTermsAndDisablesUnsafeRows(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}, &Option{}))

	user := &User{
		Id:       701,
		Username: "legacy-renewal-contract-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "legacy-renewal-contract-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	keyID, err := StoreTossBillingKey(user.Id, "cust_legacy_contract", "billing_legacy_contract", "현대", "433012******1234")
	require.NoError(t, err)

	plan := &SubscriptionPlan{
		Id:                      702,
		Title:                   "Legacy Contract",
		PriceAmount:             2.5,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationMonth,
		DurationValue:           1,
		Enabled:                 true,
		UpgradeGroup:            "premium",
		TotalAmount:             987654,
		QuotaResetPeriod:        SubscriptionResetMonthly,
		QuotaResetCustomSeconds: 0,
	}
	require.NoError(t, DB.Create(plan).Error)
	persistAttestedTossOptionsForTest(t, map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossUnitPrice":      "1234",
	})

	now := time.Now().Unix()
	valid := &UserSubscription{
		Id:               703,
		UserId:           user.Id,
		PlanId:           plan.Id,
		AmountTotal:      plan.TotalAmount,
		StartTime:        now - 86400,
		EndTime:          now + 86400,
		Status:           "active",
		AutoRenew:        true,
		NextBillingTime:  now + 86400,
		BillingRetryTime: now + 60,
		BillingKeyId:     keyID,
		UpgradeGroup:     plan.UpgradeGroup,
	}
	unsafe := &UserSubscription{
		Id:               704,
		UserId:           user.Id,
		PlanId:           999999,
		StartTime:        now - 86400,
		EndTime:          now + 86400,
		Status:           "active",
		AutoRenew:        true,
		NextBillingTime:  now + 86400,
		BillingRetryTime: now + 60,
		BillingKeyId:     keyID,
	}
	require.NoError(t, DB.Create(valid).Error)
	require.NoError(t, DB.Create(unsafe).Error)
	// Reproduce a pre-column deployed row, for which AutoMigrate presents NULL.
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id IN ?", []int{valid.Id, unsafe.Id}).
		Update("toss_renewal_contract_snapshot", gorm.Expr("NULL")).Error)

	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1234
	t.Cleanup(func() { setting.TossUnitPrice = originalUnitPrice })

	backfilled, disabled, err := BackfillLegacyTossRenewalContractsAtCurrentTerms()
	require.NoError(t, err)
	require.EqualValues(t, 1, backfilled)
	require.EqualValues(t, 1, disabled)

	var migrated UserSubscription
	require.NoError(t, DB.First(&migrated, valid.Id).Error)
	require.True(t, migrated.AutoRenew)
	require.NotEmpty(t, migrated.TossRenewalContractSnapshot)
	contract, err := ResolveTossRenewalContract(&migrated)
	require.NoError(t, err)
	require.Equal(t, plan.PriceAmount, contract.Money)
	require.Equal(t, int64(3085), contract.ProviderAmount)
	require.Equal(t, plan.DurationUnit, contract.Plan.DurationUnit)
	require.Equal(t, plan.DurationValue, contract.Plan.DurationValue)
	require.Equal(t, plan.TotalAmount, contract.Plan.TotalAmount)
	require.Equal(t, plan.UpgradeGroup, contract.Plan.UpgradeGroup)
	require.Equal(t, plan.QuotaResetPeriod, contract.Plan.QuotaResetPeriod)

	var disabledSub UserSubscription
	require.NoError(t, DB.First(&disabledSub, unsafe.Id).Error)
	require.False(t, disabledSub.AutoRenew)
	require.Zero(t, disabledSub.NextBillingTime)
	require.Zero(t, disabledSub.BillingRetryTime)

	// Plan and unit-price changes after the deployment bridge must not mutate
	// the commercial contract, and a second run must be a no-op.
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]interface{}{
		"price_amount":   9.0,
		"duration_unit":  SubscriptionDurationDay,
		"duration_value": 7,
		"total_amount":   1,
		"upgrade_group":  "changed",
	}).Error)
	setting.TossUnitPrice = 2000
	backfilled, disabled, err = BackfillLegacyTossRenewalContractsAtCurrentTerms()
	require.NoError(t, err)
	require.Zero(t, backfilled)
	require.Zero(t, disabled)

	require.NoError(t, DB.First(&migrated, valid.Id).Error)
	contract, err = ResolveTossRenewalContract(&migrated)
	require.NoError(t, err)
	require.Equal(t, 2.5, contract.Money)
	require.Equal(t, int64(3085), contract.ProviderAmount)
	require.Equal(t, SubscriptionDurationMonth, contract.Plan.DurationUnit)
	require.Equal(t, 987654, int(contract.Plan.TotalAmount))
}
