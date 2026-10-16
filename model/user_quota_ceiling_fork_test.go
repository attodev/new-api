package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Ported from team model/user_quota_reserve_test.go (ba6afab57). Team let
// IncreaseUserQuota push a balance above the wallet ceiling (refunds were
// uncapped); develop's increaseUserQuota applies the ceiling atomically and
// returns ErrWalletQuotaLimitExceeded, which is the behaviour asserted here.

func TestIncreaseUserQuotaRejectsSingleDeltaAboveWalletCeiling(t *testing.T) {
	setupSQLiteModelTestDB(t)
	user := &User{Username: "increase-delta", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	for _, db := range []bool{true, false} {
		require.Error(t, IncreaseUserQuota(user.Id, common.MaxWalletQuota+1, db), "db=%t", db)
	}

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, 0, reloaded.Quota)
}

func TestIncreaseUserQuotaAtWalletCeilingIsRejected(t *testing.T) {
	setupSQLiteModelTestDB(t)
	user := &User{Username: "increase-ceiling", Password: "password", Status: common.UserStatusEnabled, Quota: common.MaxWalletQuota}
	require.NoError(t, DB.Create(user).Error)

	require.ErrorIs(t, IncreaseUserQuota(user.Id, 5, true), ErrWalletQuotaLimitExceeded)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota, reloaded.Quota)
}
