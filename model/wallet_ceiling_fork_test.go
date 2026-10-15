package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// The check-in reward and the invitation transfer both add quota to the user
// wallet, so they stay within common.MaxWalletQuota. A rejected check-in
// reward must not leave the day marked as done.

// disableUserQuotaBatchForWalletCeilingTest also prepares the check-in table
// (the package TestMain does not migrate it) and empties it afterwards.
func disableUserQuotaBatchForWalletCeilingTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Checkin{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM checkins") })
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = old })
}

func enableFixedCheckinReward(t *testing.T, reward int) {
	t.Helper()
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.Enabled = true
	setting.MinQuota = reward
	setting.MaxQuota = reward
	t.Cleanup(func() { *setting = old })
}

func requireUserQuota(t *testing.T, userId int, want int) {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	require.Equal(t, want, user.Quota)
}

func TestUserCheckinCreditsReward(t *testing.T) {
	truncateTables(t)
	disableUserQuotaBatchForWalletCeilingTest(t)
	enableFixedCheckinReward(t, 1000)
	user := insertUserForPaymentGuardTest(t, 801, 0)

	checkin, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, checkin.QuotaAwarded)
	requireUserQuota(t, user.Id, 1000)

	_, err = UserCheckin(user.Id)
	require.EqualError(t, err, "Already checked in today")
}

func TestUserCheckinAtWalletCeilingIsRejectedAndNotRecorded(t *testing.T) {
	truncateTables(t)
	disableUserQuotaBatchForWalletCeilingTest(t)
	enableFixedCheckinReward(t, 1000)
	user := insertUserForPaymentGuardTest(t, 802, common.MaxWalletQuota-999)

	checkin, err := UserCheckin(user.Id)
	require.Nil(t, checkin)
	require.EqualError(t, err, "Check-in failed: wallet quota limit reached")
	requireUserQuota(t, user.Id, common.MaxWalletQuota-999)

	checked, err := HasCheckedInToday(user.Id)
	require.NoError(t, err)
	require.False(t, checked, "a rejected reward must not consume the day's check-in")
}

// The MySQL/PostgreSQL path credits inside the check-in transaction; SQLite
// supports the same transaction, so it is exercised directly here.
func TestUserCheckinWithTransactionAtWalletCeilingRollsBack(t *testing.T) {
	truncateTables(t)
	disableUserQuotaBatchForWalletCeilingTest(t)
	user := insertUserForPaymentGuardTest(t, 803, common.MaxWalletQuota-999)

	checkin := &Checkin{UserId: user.Id, CheckinDate: "2026-10-13", QuotaAwarded: 1000, CreatedAt: common.GetTimestamp()}
	result, err := userCheckinWithTransaction(checkin, user.Id, 1000)
	require.Nil(t, result)
	require.EqualError(t, err, "Check-in failed: wallet quota limit reached")
	requireUserQuota(t, user.Id, common.MaxWalletQuota-999)

	var count int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)

	checkin = &Checkin{UserId: user.Id, CheckinDate: "2026-10-14", QuotaAwarded: 999, CreatedAt: common.GetTimestamp()}
	result, err = userCheckinWithTransaction(checkin, user.Id, 999)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireUserQuota(t, user.Id, common.MaxWalletQuota)
}

func TestUserCheckinZeroRewardStillRecords(t *testing.T) {
	truncateTables(t)
	disableUserQuotaBatchForWalletCeilingTest(t)
	enableFixedCheckinReward(t, 0)
	user := insertUserForPaymentGuardTest(t, 804, common.MaxWalletQuota)

	checkin, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.Equal(t, 0, checkin.QuotaAwarded)
	requireUserQuota(t, user.Id, common.MaxWalletQuota)
}

func TestTransferAffQuotaToQuotaRespectsWalletCeiling(t *testing.T) {
	truncateTables(t)
	disableUserQuotaBatchForWalletCeilingTest(t)
	user := insertUserForPaymentGuardTest(t, 805, common.MaxWalletQuota-10)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("aff_quota", 1000000).Error)

	var current User
	require.NoError(t, DB.First(&current, user.Id).Error)
	err := current.TransferAffQuotaToQuota(1000000)
	require.ErrorIs(t, err, ErrAffTransferWalletLimit)
	require.EqualError(t, err, "Invitation quota transfer failed: wallet quota limit reached")

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota-10, got.Quota)
	require.Equal(t, 1000000, got.AffQuota)
}

func TestCheckAffTransferCeiling(t *testing.T) {
	require.NoError(t, checkAffTransferCeiling(common.MaxWalletQuota-5, 5))
	require.ErrorIs(t, checkAffTransferCeiling(common.MaxWalletQuota-5, 6), ErrAffTransferWalletLimit)
}
