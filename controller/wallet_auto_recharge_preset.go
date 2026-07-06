package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ListWalletAutoRechargePresets(c *gin.Context) {
	rows, err := model.ListWalletAutoRechargePresets(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func CreateWalletAutoRechargePreset(c *gin.Context) {
	var req model.WalletAutoRechargePresetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.CreateWalletAutoRechargePreset(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func UpdateWalletAutoRechargePreset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var req model.WalletAutoRechargePresetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.UpdateWalletAutoRechargePreset(id, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func DeleteWalletAutoRechargePreset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.DisableWalletAutoRechargePreset(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetWalletAutoRechargePresets(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRechargePresetsForTarget(target.TargetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func GetOrganizationWalletAutoRechargePresets(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRechargePresetsForTarget(target.TargetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
