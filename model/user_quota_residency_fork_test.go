package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func withQuotaCacheTTL(t *testing.T, ttl int) {
	t.Helper()
	old := common.QuotaCacheTTL
	common.QuotaCacheTTL = ttl
	t.Cleanup(func() { common.QuotaCacheTTL = old })
}

func TestQuotaCacheTTLSecondsFollowsSwitch(t *testing.T) {
	oldSync := common.SyncFrequency
	common.SyncFrequency = 60
	t.Cleanup(func() { common.SyncFrequency = oldSync })

	withQuotaCacheTTL(t, 0)
	require.False(t, quotaCacheResidencyEnabled())
	require.Equal(t, 60, quotaCacheTTLSeconds(), "off: same as user cache TTL")

	withQuotaCacheTTL(t, 86400)
	require.True(t, quotaCacheResidencyEnabled())
	require.Equal(t, 86400, quotaCacheTTLSeconds())

	withQuotaCacheTTL(t, 10)
	require.Equal(t, 60, quotaCacheTTLSeconds(), "never shorter than SYNC_FREQUENCY")
}
