package controller

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupWalletAutoRechargePresetControllerTestDB(t *testing.T) {
	t.Helper()
	setupWalletAutoRechargeControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.WalletAutoRechargePreset{}))
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })
}

func TestAdminCanCreateListAndDisableWalletAutoRechargePreset(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	admin := model.User{Id: 1, Username: "admin", Role: common.RoleAdminUser}

	createRes := performOrganizationRequest(
		CreateWalletAutoRechargePreset,
		admin,
		http.MethodPost,
		"/api/admin/wallet/auto-recharge/presets",
		`{"type":"scheduled","target_scope":"all","name":"월 1회 10000원","amount":10000,"interval_unit":"month","interval_value":1,"charge_immediately":true,"enabled":true}`,
	)
	require.Equal(t, http.StatusOK, createRes.Code)

	listRes := performOrganizationRequest(
		ListWalletAutoRechargePresets,
		admin,
		http.MethodGet,
		"/api/admin/wallet/auto-recharge/presets",
		"",
	)
	require.Equal(t, http.StatusOK, listRes.Code)
	var listPayload struct {
		Success bool                             `json:"success"`
		Data    []model.WalletAutoRechargePreset `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRes.Body.Bytes(), &listPayload))
	require.True(t, listPayload.Success)
	require.Len(t, listPayload.Data, 1)

	disableRes := performOrganizationRequest(
		DeleteWalletAutoRechargePreset,
		admin,
		http.MethodDelete,
		"/api/admin/wallet/auto-recharge/presets/1",
		"",
		gin.Param{Key: "id", Value: "1"},
	)
	require.Equal(t, http.StatusOK, disableRes.Code)

	var stored model.WalletAutoRechargePreset
	require.NoError(t, model.DB.First(&stored, 1).Error)
	require.False(t, stored.Enabled)
}

func TestCreateWalletAutoRechargePresetAcceptsThresholdQuota(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	admin := model.User{Id: 1, Username: "admin", Role: common.RoleAdminUser}

	body := `{
		"type":"threshold",
		"target_scope":"all",
		"name":"Quota threshold",
		"description":"",
		"amount":10000,
		"threshold_amount":0,
		"threshold_quota":250000,
		"interval_unit":"month",
		"interval_value":1,
		"custom_seconds":0,
		"charge_immediately":false,
		"sort_order":1,
		"enabled":true
	}`
	res := performOrganizationRequest(
		CreateWalletAutoRechargePreset,
		admin,
		http.MethodPost,
		"/api/admin/wallet/auto-recharge/presets",
		body,
	)

	require.Equal(t, http.StatusOK, res.Code)
	var preset model.WalletAutoRechargePreset
	require.NoError(t, model.DB.Where("name = ?", "Quota threshold").First(&preset).Error)
	require.Equal(t, 250000, preset.ThresholdQuota)
}

func TestUserPresetListFiltersByWalletTarget(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	user := model.User{Id: 2, Username: "user", Role: common.RoleCommonUser, AffCode: "preset-user"}
	require.NoError(t, model.DB.Create(&user).Error)
	_, err := model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetUser,
		Name: "개인", Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1, Enabled: true,
	})
	require.NoError(t, err)
	_, err = model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetOrganization,
		Name: "조직", Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1, Enabled: true,
	})
	require.NoError(t, err)

	res := performOrganizationRequest(
		GetWalletAutoRechargePresets,
		user,
		http.MethodGet,
		"/api/user/wallet/auto-recharge/presets",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool                             `json:"success"`
		Data    []model.WalletAutoRechargePreset `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "개인", payload.Data[0].Name)
}

func TestWalletAutoRechargeCreateRequiresPresetAndCopiesSnapshot(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	enableTossBillingForTest(t)
	user := model.User{Id: 3, Username: "owner", Role: common.RoleCommonUser, AffCode: "preset-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	preset, err := model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetUser,
		Name: "월 1회 20000원", Amount: 20000, IntervalUnit: model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, ChargeImmediately: true, Enabled: true,
	})
	require.NoError(t, err)

	legacyRes := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		`{"amount":10000,"interval_unit":"month","interval_value":1}`,
	)
	require.Equal(t, http.StatusOK, legacyRes.Code)
	var legacyPayload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(legacyRes.Body.Bytes(), &legacyPayload))
	require.False(t, legacyPayload.Success)

	res := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		`{"preset_id":`+strconv.Itoa(preset.Id)+`,"amount":999999,"interval_unit":"day","interval_value":9}`,
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			TradeNo string `json:"trade_no"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&policy, "auth_trade_no = ?", payload.Data.TradeNo).Error)
	require.Equal(t, preset.Id, policy.PresetId)
	require.Equal(t, float64(20000), policy.Amount)
	require.Equal(t, model.WalletAutoRechargeIntervalMonth, policy.IntervalUnit)
	require.Equal(t, 1, policy.IntervalValue)
	require.True(t, policy.ChargeImmediately)
}
