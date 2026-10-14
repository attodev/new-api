package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRechargePayPalCreditsOnceAndRejectsOtherProviders(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})
	oldUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldUnit })

	user := insertUserForPaymentGuardTest(t, 901, 1_000)
	insertTopUpForPaymentGuardTest(t, "pp-ok", user.Id, PaymentProviderPayPal)
	insertTopUpForPaymentGuardTest(t, "pp-stripe", user.Id, PaymentProviderStripe)

	require.NoError(t, RechargePayPal("pp-ok", "127.0.0.1"))
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 1_000+4_995_000, got.Quota) // Money 9.99 USD * QuotaPerUnit 500_000

	require.NoError(t, RechargePayPal("pp-ok", "127.0.0.1")) // idempotent
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 1_000+4_995_000, got.Quota)
	require.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "pp-ok"))

	err := RechargePayPal("pp-stripe", "127.0.0.1")
	require.Error(t, err)
	require.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "pp-stripe"))
}
