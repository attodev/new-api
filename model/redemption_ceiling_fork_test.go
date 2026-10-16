package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team model/redemption_test.go (3f1cbeeb7). The team wallet
// ceiling common.MaxQuota is develop's common.MaxWalletQuota (2^53-1).

func TestRedeemAllowsBalanceExactlyAtWalletCeiling(t *testing.T) {
	setupSQLiteModelTestDB(t)
	user := &User{Username: "redeem-ceiling", Password: "password", Status: common.UserStatusEnabled, Quota: common.MaxWalletQuota - 10}
	require.NoError(t, DB.Create(user).Error)
	key := "20000000000000000000000000000001"
	require.NoError(t, DB.Create(&Redemption{
		Name:        "ceiling",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       10,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	quota, err := Redeem(key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10, quota)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, common.MaxWalletQuota, reloaded.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, commonKeyCol+" = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
}

func TestRedemptionInsertAndUpdateValidateQuota(t *testing.T) {
	setupSQLiteModelTestDB(t)
	walletExceeds := fmt.Sprintf("wallet quota exceeds %d", common.MaxWalletQuota)

	redemption := &Redemption{
		Name:        "quota-validation",
		Key:         "20000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxWalletQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.EqualError(t, redemption.Insert(), walletExceeds)
	redemption.Quota = 0
	require.EqualError(t, redemption.Insert(), "redemption quota must be positive")
	redemption.Quota = -1
	require.EqualError(t, redemption.Insert(), "redemption quota must be positive")

	redemption.Quota = 100
	require.NoError(t, redemption.Insert())
	redemption.Quota = common.MaxWalletQuota + 1
	require.EqualError(t, redemption.Update(), walletExceeds)
	redemption.Quota = 0
	require.EqualError(t, redemption.Update(), "redemption quota must be positive")

	var stored Redemption
	require.NoError(t, DB.First(&stored, redemption.Id).Error)
	assert.Equal(t, 100, stored.Quota)
}
