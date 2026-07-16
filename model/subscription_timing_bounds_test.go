package model

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionPlanTimingBoundsRejectOverflowingCustomValues(t *testing.T) {
	base := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	maxPlan := &SubscriptionPlan{
		DurationUnit:            SubscriptionDurationCustom,
		CustomSeconds:           SubscriptionPlanMaxCustomSeconds,
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: SubscriptionPlanMaxCustomSeconds,
	}
	end, err := calcPlanEndTime(base, maxPlan)
	require.NoError(t, err)
	require.Equal(t, base.Unix()+SubscriptionPlanMaxCustomSeconds, end)
	require.Equal(t, end, calcNextResetTime(base, maxPlan, end))

	tooLong := *maxPlan
	tooLong.CustomSeconds = SubscriptionPlanMaxCustomSeconds + 1
	_, err = calcPlanEndTime(base, &tooLong)
	require.Error(t, err)
	tooLong.CustomSeconds = math.MaxInt64
	_, err = calcPlanEndTime(base, &tooLong)
	require.Error(t, err)

	tooLong = *maxPlan
	tooLong.QuotaResetCustomSeconds = SubscriptionPlanMaxCustomSeconds + 1
	require.Zero(t, calcNextResetTime(base, &tooLong, 0))
	tooLong.QuotaResetCustomSeconds = math.MaxInt64
	require.Zero(t, calcNextResetTime(base, &tooLong, 0))
}

func TestSubscriptionPlanTimingBoundsRejectDurationAndUnixOverflow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	valid := &SubscriptionPlan{DurationUnit: SubscriptionDurationHour, DurationValue: SubscriptionPlanMaxDurationValue}
	end, err := calcPlanEndTime(base, valid)
	require.NoError(t, err)
	require.Greater(t, end, base.Unix())

	invalid := *valid
	invalid.DurationValue = SubscriptionPlanMaxDurationValue + 1
	_, err = calcPlanEndTime(base, &invalid)
	require.Error(t, err)

	nearUnixLimit := &SubscriptionPlan{DurationUnit: SubscriptionDurationCustom, CustomSeconds: 20}
	_, err = calcPlanEndTime(time.Unix(math.MaxInt64-10, 0), nearUnixLimit)
	require.Error(t, err)
}

func TestCreateUserSubscriptionRejectsInvalidResetTiming(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	user := User{Id: 8891, Username: "timing-user", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := &SubscriptionPlan{
		Id: 8892, Title: "Invalid reset", DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: math.MaxInt64,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, createErr := CreateUserSubscriptionFromPlanTx(tx, user.Id, plan, "test")
		return createErr
	})
	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)
}
