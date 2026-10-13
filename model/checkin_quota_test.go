package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// The check-in reward is new quota, so it is capped at the wallet ceiling
// common.MaxQuota; a rejected reward must not leave the day marked as done.

func enableFixedCheckinReward(t *testing.T, reward int) {
	t.Helper()
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.Enabled = true
	setting.MinQuota = reward
	setting.MaxQuota = reward
	t.Cleanup(func() { *setting = old })
}

func requireUserQuota(t *testing.T, userId int, want int64) {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	require.Equal(t, want, user.Quota)
}

func TestUserCheckinCreditsReward(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	enableFixedCheckinReward(t, 1000)
	user := insertUserWithQuota(t, 0)

	checkin, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, checkin.QuotaAwarded)
	requireUserQuota(t, user.Id, 1000)

	_, err = UserCheckin(user.Id)
	require.EqualError(t, err, "already checked in today")
}

func TestUserCheckinAtWalletCeilingIsRejectedAndNotRecorded(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	enableFixedCheckinReward(t, 1000)
	user := insertUserWithQuota(t, common.MaxQuota-999)

	checkin, err := UserCheckin(user.Id)
	require.Nil(t, checkin)
	require.EqualError(t, err, "check-in failed: wallet quota limit reached")
	requireUserQuota(t, user.Id, common.MaxQuota-999)

	checked, err := HasCheckedInToday(user.Id)
	require.NoError(t, err)
	require.False(t, checked, "a rejected reward must not consume the day's check-in")
}

// The MySQL/PostgreSQL path credits inside the check-in transaction; SQLite
// supports the same transaction, so it is exercised directly here.
func TestUserCheckinWithTransactionAtWalletCeilingRollsBack(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	user := insertUserWithQuota(t, common.MaxQuota-999)

	checkin := &Checkin{UserId: user.Id, CheckinDate: "2026-10-13", QuotaAwarded: 1000, CreatedAt: common.GetTimestamp()}
	result, err := userCheckinWithTransaction(checkin, user.Id, 1000)
	require.Nil(t, result)
	require.EqualError(t, err, "check-in failed: wallet quota limit reached")
	requireUserQuota(t, user.Id, common.MaxQuota-999)

	var count int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)

	checkin = &Checkin{UserId: user.Id, CheckinDate: "2026-10-14", QuotaAwarded: 999, CreatedAt: common.GetTimestamp()}
	result, err = userCheckinWithTransaction(checkin, user.Id, 999)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireUserQuota(t, user.Id, common.MaxQuota)
}

func TestUserCheckinZeroRewardStillRecords(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	enableFixedCheckinReward(t, 0)
	user := insertUserWithQuota(t, common.MaxQuota)

	checkin, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.Equal(t, 0, checkin.QuotaAwarded)
	requireUserQuota(t, user.Id, common.MaxQuota)
}
