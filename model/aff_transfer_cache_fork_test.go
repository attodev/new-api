package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransferAffQuotaToQuotaSyncsCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9401, 1000)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("aff_quota", 500000).Error)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	var fresh User
	require.NoError(t, DB.First(&fresh, user.Id).Error)
	require.NoError(t, fresh.TransferAffQuotaToQuota(500000))

	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.Equal(t, 501000, dbUser.Quota)
	require.Equal(t, dbUser.Quota, cached.Quota, "cache follows the transfer")
}
