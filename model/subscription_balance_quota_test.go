package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Upstream a073f74b3 bounds wallet-priced subscription purchases with
// WalletQuotaFromDecimalStrict; the fork's wallet ceiling is common.MaxQuota.
func TestCalcSubscriptionBalanceQuotaRespectsWalletCeiling(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	quota, err := calcSubscriptionBalanceQuota(1_000_000_000) // exactly MaxQuota
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, quota)

	_, err = calcSubscriptionBalanceQuota(1_000_000_001)
	require.Error(t, err)
	_, err = calcSubscriptionBalanceQuota(1e30)
	require.Error(t, err)
}
