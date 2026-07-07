package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestWalletAutoRechargeTaskQueriesScheduledAndThresholdPolicies(t *testing.T) {
	require.Equal(t, 1*time.Minute, walletAutoRechargeTickInterval)
	require.Equal(t, 100, walletAutoRechargeBatchSize)
	require.Equal(t, 3, walletAutoRechargeMaxFails)
	require.NotNil(t, chargeWalletAutoRecharge)
	require.NotNil(t, model.ProcessWalletAutoRechargeWithConfiguredCharger)
}
