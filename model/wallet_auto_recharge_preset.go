package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

const (
	WalletAutoRechargePresetTargetUser         = "user"
	WalletAutoRechargePresetTargetOrganization = "organization"
	WalletAutoRechargePresetTargetAll          = "all"
)

type WalletAutoRechargePreset struct {
	Id                int     `json:"id"`
	Type              string  `json:"type" gorm:"type:varchar(32);index"`
	TargetScope       string  `json:"target_scope" gorm:"type:varchar(32);index"`
	Name              string  `json:"name" gorm:"type:varchar(128)"`
	Description       string  `json:"description" gorm:"type:varchar(255)"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	IntervalUnit      string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	SortOrder         int     `json:"sort_order" gorm:"index"`
	Enabled           bool    `json:"enabled" gorm:"index"`
	CreateTime        int64   `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime        int64   `json:"update_time" gorm:"autoUpdateTime"`
}

type WalletAutoRechargePresetRequest struct {
	Type              string  `json:"type"`
	TargetScope       string  `json:"target_scope"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	IntervalUnit      string  `json:"interval_unit"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	SortOrder         int     `json:"sort_order"`
	Enabled           bool    `json:"enabled"`
}

func (req WalletAutoRechargePresetRequest) normalizeAndValidate() (WalletAutoRechargePresetRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.TargetScope = strings.TrimSpace(req.TargetScope)

	if req.Type != WalletAutoRechargeTypeScheduled && req.Type != WalletAutoRechargeTypeThreshold {
		return req, errors.New("invalid wallet auto recharge preset type")
	}
	if req.TargetScope != WalletAutoRechargePresetTargetUser &&
		req.TargetScope != WalletAutoRechargePresetTargetOrganization &&
		req.TargetScope != WalletAutoRechargePresetTargetAll {
		return req, errors.New("invalid wallet auto recharge preset target scope")
	}
	if req.Name == "" {
		return req, errors.New("wallet auto recharge preset name is required")
	}
	if req.Amount <= 0 {
		return req, errors.New("wallet auto recharge preset amount must be positive")
	}
	if walletAutoRechargeKRW(req.Amount) < int64(setting.TossMinTopUp) {
		return req, errors.New("wallet auto recharge preset amount is below Toss minimum")
	}

	switch req.Type {
	case WalletAutoRechargeTypeScheduled:
		switch req.IntervalUnit {
		case WalletAutoRechargeIntervalMonth, WalletAutoRechargeIntervalDay:
			if req.IntervalValue <= 0 {
				return req, errors.New("wallet auto recharge preset interval value is invalid")
			}
		case WalletAutoRechargeIntervalCustom:
			if req.CustomSeconds <= 0 {
				return req, errors.New("wallet auto recharge preset custom seconds is invalid")
			}
			if req.IntervalValue <= 0 {
				req.IntervalValue = 1
			}
		default:
			return req, errors.New("wallet auto recharge preset interval is required")
		}
	case WalletAutoRechargeTypeThreshold:
		if req.ThresholdAmount < 0 {
			return req, errors.New("wallet auto recharge preset threshold cannot be negative")
		}
	}

	return req, nil
}

func CreateWalletAutoRechargePreset(req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error) {
	req, err := req.normalizeAndValidate()
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	preset := &WalletAutoRechargePreset{
		Type:              req.Type,
		TargetScope:       req.TargetScope,
		Name:              req.Name,
		Description:       req.Description,
		Amount:            req.Amount,
		ThresholdAmount:   req.ThresholdAmount,
		IntervalUnit:      req.IntervalUnit,
		IntervalValue:     req.IntervalValue,
		CustomSeconds:     req.CustomSeconds,
		ChargeImmediately: req.ChargeImmediately,
		SortOrder:         req.SortOrder,
		Enabled:           req.Enabled,
		CreateTime:        now,
		UpdateTime:        now,
	}
	if err := DB.Create(preset).Error; err != nil {
		return nil, err
	}
	return preset, nil
}

func UpdateWalletAutoRechargePreset(id int, req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error) {
	req, err := req.normalizeAndValidate()
	if err != nil {
		return nil, err
	}

	var preset WalletAutoRechargePreset
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"type":               req.Type,
		"target_scope":       req.TargetScope,
		"name":               req.Name,
		"description":        req.Description,
		"amount":             req.Amount,
		"threshold_amount":   req.ThresholdAmount,
		"interval_unit":      req.IntervalUnit,
		"interval_value":     req.IntervalValue,
		"custom_seconds":     req.CustomSeconds,
		"charge_immediately": req.ChargeImmediately,
		"sort_order":         req.SortOrder,
		"enabled":            req.Enabled,
		"update_time":        time.Now().Unix(),
	}
	if err := DB.Model(&preset).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}
	return &preset, nil
}

func DisableWalletAutoRechargePreset(id int) error {
	return DB.Model(&WalletAutoRechargePreset{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled":     false,
		"update_time": time.Now().Unix(),
	}).Error
}

func ListWalletAutoRechargePresets(includeDisabled bool) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	query := DB.Model(&WalletAutoRechargePreset{})
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	err := query.Order("sort_order asc, id asc").Find(&rows).Error
	return rows, err
}

func walletAutoRechargePresetMatchesTarget(scope string, targetType string) bool {
	return scope == WalletAutoRechargePresetTargetAll ||
		scope == targetType ||
		(scope == WalletAutoRechargePresetTargetUser && targetType == TopUpTargetTypeUser) ||
		(scope == WalletAutoRechargePresetTargetOrganization && targetType == TopUpTargetTypeOrganization)
}

func GetWalletAutoRechargePresetForTarget(id int, rechargeType string, targetType string) (*WalletAutoRechargePreset, error) {
	var preset WalletAutoRechargePreset
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}
	if !preset.Enabled {
		return nil, errors.New("wallet auto recharge preset is disabled")
	}
	if preset.Type != rechargeType {
		return nil, errors.New("wallet auto recharge preset type mismatch")
	}
	if !walletAutoRechargePresetMatchesTarget(preset.TargetScope, targetType) {
		return nil, errors.New("wallet auto recharge preset is not available for target")
	}
	return &preset, nil
}

func ListWalletAutoRechargePresetsForTarget(targetType string) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	err := DB.Where("enabled = ?", true).
		Where("target_scope = ? OR target_scope = ?", targetType, WalletAutoRechargePresetTargetAll).
		Order("sort_order asc, id asc").
		Find(&rows).Error
	return rows, err
}
