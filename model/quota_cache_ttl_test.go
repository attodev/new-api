package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestQuotaCacheTTLOverridesSyncFrequency(t *testing.T) {
	truncateTables(t)
	resetUserQuotaBatchState(t)
	resetTokenQuotaBatchState(t)
	server := useTokenQuotaMiniRedis(t)
	common.QuotaCacheTTL = 600
	common.BatchUpdateEnabled = true
	user := insertUserWithQuota(t, 100)
	token := insertTokenWithQuota(t, 100)

	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = GetTokenByKey(token.Key, true)
	require.NoError(t, err)

	assertTTL := func() {
		t.Helper()
		require.Equal(t, 600*time.Second, server.TTL(getUserCacheKey(user.Id)))
		require.Equal(t, 600*time.Second, server.TTL(getTokenCacheKey(token.Key)))
		require.Equal(t, 2, common.SyncFrequency, "quota TTL must not change synchronization frequency")
	}
	assertTTL()

	// The caches must survive the synchronization interval independently.
	server.FastForward(3 * time.Second)
	reserved, err := TryReserveUserQuota(user.Id, 10)
	require.NoError(t, err)
	require.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 10, false)
	require.NoError(t, err)
	require.True(t, reserved)
	assertTTL()

	server.FastForward(3 * time.Second)
	require.NoError(t, DecreaseUserQuota(user.Id, 1, false))
	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 1))
	assertTTL()
}

func TestQuotaCacheTTLFallback(t *testing.T) {
	oldTTL, oldSync := common.QuotaCacheTTL, common.SyncFrequency
	t.Cleanup(func() {
		common.QuotaCacheTTL, common.SyncFrequency = oldTTL, oldSync
	})
	for _, tc := range []struct {
		name string
		ttl  int
		sync int
		want int
	}{
		{"zero inherits sync frequency", 0, 120, 120},
		{"negative inherits sync frequency", -1, 120, 120},
		{"unset sync defaults to sixty", 0, 0, 60},
		{"negative sync defaults to sixty", 0, -1, 60},
		{"explicit TTL ignores sync frequency", 600, 120, 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.QuotaCacheTTL, common.SyncFrequency = tc.ttl, tc.sync
			require.Equal(t, tc.want, quotaCacheTTLSeconds())
		})
	}
}
