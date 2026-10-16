package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Ported from team model/subscription_balance_quota_test.go (3f1cbeeb7).
// 2^53-1 = 6361 * 1416003655831, so with QuotaPerUnit 6361 a price of
// 1416003655831 lands exactly on common.MaxWalletQuota and one more unit
// crosses it.
func TestCalcSubscriptionBalanceQuotaRespectsWalletCeiling(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 6361
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	quota, err := calcSubscriptionBalanceQuota(1_416_003_655_831)
	require.NoError(t, err)
	require.Equal(t, common.MaxWalletQuota, quota)

	_, err = calcSubscriptionBalanceQuota(1_416_003_655_832)
	require.Error(t, err)
	_, err = calcSubscriptionBalanceQuota(1e30)
	require.Error(t, err)
}
