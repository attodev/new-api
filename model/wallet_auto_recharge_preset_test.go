package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func setupWalletAutoRechargePresetTestDB(t *testing.T) {
	t.Helper()
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.AutoMigrate(&WalletAutoRechargePreset{}))
}

func TestCreateWalletAutoRechargePresetValidatesScheduledPreset(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })

	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "월 1회 500원",
		Amount:        500,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})
	require.ErrorContains(t, err, "amount is below Toss minimum")

	preset, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:              WalletAutoRechargeTypeScheduled,
		TargetScope:       WalletAutoRechargePresetTargetAll,
		Name:              "월 1회 10000원",
		Description:       "개인/조직 공용",
		Amount:            10000,
		IntervalUnit:      WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
		SortOrder:         10,
		Enabled:           true,
	})
	require.NoError(t, err)
	require.NotZero(t, preset.Id)
	require.Equal(t, "월 1회 10000원", preset.Name)
	require.True(t, preset.Enabled)
}

func TestWalletAutoRechargePresetTargetFiltering(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetScope:     WalletAutoRechargePresetTargetUser,
		Name:            "개인 자동충전",
		Amount:          10000,
		ThresholdAmount: 5000,
		Enabled:         true,
	})
	require.NoError(t, err)

	preset, err := GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Equal(t, WalletAutoRechargePresetTargetUser, preset.TargetScope)

	_, err = GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeOrganization)
	require.ErrorContains(t, err, "wallet auto recharge preset is not available for target")
}

func TestDisableWalletAutoRechargePresetKeepsRow(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	preset, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "비활성화 테스트",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})
	require.NoError(t, err)

	require.NoError(t, DisableWalletAutoRechargePreset(preset.Id))

	var stored WalletAutoRechargePreset
	require.NoError(t, DB.First(&stored, preset.Id).Error)
	require.False(t, stored.Enabled)
}
