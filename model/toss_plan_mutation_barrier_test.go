package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionPlanMutationBarrierBlocksWhileTossMaintenanceIsPinned(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &SubscriptionPlan{}))
	persistAttestedTossOptionsForTest(t, nil)
	revision := setting.GetTossConfigSnapshot().Revision
	require.NoError(t, ensureTossConfigurationMaintenanceGate(revision))

	plan := &SubscriptionPlan{
		Title: "maintenance-blocked-plan", PriceAmount: 1, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	err := RunSubscriptionPlanMutationWithTossBarrier(func(tx *gorm.DB) error {
		return tx.Create(plan).Error
	})
	require.ErrorIs(t, err, ErrTossConfigMaintenanceRequired)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("title = ?", plan.Title).Count(&count).Error)
	require.Zero(t, count)

	require.NoError(t, clearTossConfigurationMaintenanceGate(revision))
	require.NoError(t, RunSubscriptionPlanMutationWithTossBarrier(func(tx *gorm.DB) error {
		return tx.Create(plan).Error
	}))
}
