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
	require.False(t, preset.ChargeImmediately)
}

func TestWalletAutoRechargeCustomPresetIsTestModeOnly(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)

	request := WalletAutoRechargePresetRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetScope: WalletAutoRechargePresetTargetUser,
		Name: "test cadence", Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalCustom,
		IntervalValue: 1, CustomSeconds: 60, Enabled: true,
	}
	_, err := CreateWalletAutoRechargePreset(request)
	require.ErrorContains(t, err, "only in Toss test mode")

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{"TossTestMode": "true"}))
	preset, err := CreateWalletAutoRechargePreset(request)
	require.NoError(t, err)
	require.NotEmpty(t, preset.TermsFingerprint)

	rows, err := ListWalletAutoRechargePresetsForTarget(TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{"TossTestMode": "false"}))
	rows, err = ListWalletAutoRechargePresetsForTarget(TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = GetWalletAutoRechargePresetForTarget(preset.Id, WalletAutoRechargeTypeScheduled, TopUpTargetTypeUser)
	require.ErrorContains(t, err, "only in Toss test mode")
}

func TestWalletAutoRechargePresetTargetFiltering(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetScope:     WalletAutoRechargePresetTargetUser,
		Name:            "개인 자동충전",
		Amount:          10000,
		ThresholdAmount: 5000,
		ThresholdQuota:  500000,
		Enabled:         true,
	})
	require.NoError(t, err)

	preset, err := GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Equal(t, WalletAutoRechargePresetTargetUser, preset.TargetScope)

	_, err = GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeOrganization)
	require.ErrorContains(t, err, "wallet auto recharge preset is not available for target")
}

func TestListWalletAutoRechargePresetsForTargetExcludesZeroQuotaThresholdPresets(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)

	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetScope:     WalletAutoRechargePresetTargetUser,
		Name:            "legacy user threshold",
		Amount:          10000,
		ThresholdAmount: 5000,
		ThresholdQuota:  0,
		SortOrder:       1,
		Enabled:         true,
	})
	require.NoError(t, err)

	positiveThreshold, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:           WalletAutoRechargeTypeThreshold,
		TargetScope:    WalletAutoRechargePresetTargetAll,
		Name:           "valid threshold",
		Amount:         10000,
		ThresholdQuota: 500000,
		SortOrder:      2,
		Enabled:        true,
	})
	require.NoError(t, err)

	scheduled, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetUser,
		Name:          "valid scheduled",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		SortOrder:     3,
		Enabled:       true,
	})
	require.NoError(t, err)

	rows, err := ListWalletAutoRechargePresetsForTarget(TopUpTargetTypeUser)

	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, []int{positiveThreshold.Id, scheduled.Id}, []int{rows[0].Id, rows[1].Id})
}

func TestGetWalletAutoRechargePresetForTargetRejectsZeroQuotaThresholdPreset(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)

	legacyThreshold, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:            WalletAutoRechargeTypeThreshold,
		TargetScope:     WalletAutoRechargePresetTargetUser,
		Name:            "legacy threshold",
		Amount:          10000,
		ThresholdAmount: 5000,
		ThresholdQuota:  0,
		Enabled:         true,
	})
	require.NoError(t, err)

	validThreshold, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:           WalletAutoRechargeTypeThreshold,
		TargetScope:    WalletAutoRechargePresetTargetUser,
		Name:           "valid threshold",
		Amount:         10000,
		ThresholdQuota: 500000,
		Enabled:        true,
	})
	require.NoError(t, err)

	_, err = GetWalletAutoRechargePresetForTarget(legacyThreshold.Id, WalletAutoRechargeTypeThreshold, TopUpTargetTypeUser)
	require.ErrorContains(t, err, "wallet auto recharge preset threshold quota is invalid")

	preset, err := GetWalletAutoRechargePresetForTarget(validThreshold.Id, WalletAutoRechargeTypeThreshold, TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Equal(t, validThreshold.Id, preset.Id)
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
	require.False(t, updated.ChargeImmediately)

	var stored WalletAutoRechargePreset
	require.NoError(t, DB.First(&stored, preset.Id).Error)
	require.Equal(t, "수정된 설명", stored.Description)
	require.Equal(t, float64(20000), stored.Amount)
	require.Equal(t, float64(7000), stored.ThresholdAmount)
	require.Equal(t, 9, stored.SortOrder)
	require.False(t, stored.Enabled)
	require.False(t, stored.ChargeImmediately)
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
