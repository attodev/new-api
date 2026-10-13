package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from upstream a073f74b3 (model/redemption_test.go additions), with
// upstream's MaxWalletQuota (2^53-1) replaced by this fork's wallet ceiling
// common.MaxQuota.

func setupRedeemFixture(t *testing.T, redemptionQuota int) (int, string) {
	t.Helper()
	truncateTables(t)
	user := &User{Username: t.Name(), Password: "test", Quota: 0, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	key := "10000000000000000000000000000001"
	require.NoError(t, DB.Create(&Redemption{
		Name:        "fixture",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       redemptionQuota,
		CreatedTime: common.GetTimestamp(),
	}).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaWithinCeiling(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, int64(500), user.Quota)
}

func TestRedeemAllowsBalanceExactlyAtCeiling(t *testing.T) {
	userId, key := setupRedeemFixture(t, 10)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxQuota-10).Error)

	_, err := Redeem(key, userId)
	require.NoError(t, err)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxQuota, user.Quota)
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 11)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxQuota-10).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxQuota-10, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, commonKeyCol+" = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	truncateTables(t)
	overCeiling := common.MaxQuota + 1

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       int(overCeiling),
		CreatedTime: common.GetTimestamp(),
	}
	walletExceeds := fmt.Sprintf("wallet quota exceeds %d", common.MaxQuota)
	require.EqualError(t, redemption.Insert(), walletExceeds)

	redemption.Quota = 0
	require.EqualError(t, redemption.Insert(), "redemption quota must be positive")
	redemption.Quota = -1
	require.EqualError(t, redemption.Insert(), "redemption quota must be positive")

	redemption.Quota = 100
	require.NoError(t, redemption.Insert())
	redemption.Quota = int(overCeiling)
	require.EqualError(t, redemption.Update(), walletExceeds)
	redemption.Quota = 0
	require.EqualError(t, redemption.Update(), "redemption quota must be positive")

	var stored Redemption
	require.NoError(t, DB.First(&stored, redemption.Id).Error)
	assert.Equal(t, 100, stored.Quota)
}
