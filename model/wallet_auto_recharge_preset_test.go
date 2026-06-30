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

func TestDisableWalletAutoRechargePresetReturnsErrorForMissingRow(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)

	err := DisableWalletAutoRechargePreset(9999)

	require.Error(t, err)
	require.ErrorContains(t, err, "wallet auto recharge preset not found")
}

func TestUpdateWalletAutoRechargePresetPersistsChanges(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })

	preset, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetScope:     WalletAutoRechargePresetTargetUser,
		Name:            "원본 프리셋",
		Description:     "원본 설명",
		Amount:          10000,
		ThresholdAmount: 5000,
		SortOrder:       1,
		Enabled:         true,
	})
	require.NoError(t, err)

	updated, err := UpdateWalletAutoRechargePreset(preset.Id, WalletAutoRechargePresetRequest{
		Type:              WalletAutoRechargeTypeThreshold,
		TargetScope:       WalletAutoRechargePresetTargetOrganization,
		Name:              "수정된 프리셋",
		Description:       "수정된 설명",
		Amount:            20000,
		ThresholdAmount:   7000,
		ChargeImmediately: true,
		SortOrder:         9,
		Enabled:           false,
	})
	require.NoError(t, err)
	require.Equal(t, "수정된 프리셋", updated.Name)
	require.Equal(t, WalletAutoRechargePresetTargetOrganization, updated.TargetScope)
	require.False(t, updated.Enabled)
	require.True(t, updated.ChargeImmediately)

	var stored WalletAutoRechargePreset
	require.NoError(t, DB.First(&stored, preset.Id).Error)
	require.Equal(t, "수정된 설명", stored.Description)
	require.Equal(t, float64(20000), stored.Amount)
	require.Equal(t, float64(7000), stored.ThresholdAmount)
	require.Equal(t, 9, stored.SortOrder)
	require.False(t, stored.Enabled)
	require.True(t, stored.ChargeImmediately)
}

func TestListWalletAutoRechargePresetsFiltersDisabledAndSortsBySortOrder(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })

	first, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "정렬 20",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		SortOrder:     20,
		Enabled:       true,
	})
	require.NoError(t, err)

	_, err = CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "정렬 10 비활성",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		SortOrder:     10,
		Enabled:       false,
	})
	require.NoError(t, err)

	third, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "정렬 10",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		SortOrder:     10,
		Enabled:       true,
	})
	require.NoError(t, err)

	activeOnly, err := ListWalletAutoRechargePresets(false)
	require.NoError(t, err)
	require.Len(t, activeOnly, 2)
	require.Equal(t, third.Id, activeOnly[0].Id)
	require.Equal(t, first.Id, activeOnly[1].Id)

	allPresets, err := ListWalletAutoRechargePresets(true)
	require.NoError(t, err)
	require.Len(t, allPresets, 3)
	require.Equal(t, "정렬 10 비활성", allPresets[0].Name)
	require.Equal(t, "정렬 10", allPresets[1].Name)
	require.Equal(t, first.Id, allPresets[2].Id)
}
