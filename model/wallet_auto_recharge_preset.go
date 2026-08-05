package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	ThresholdQuota    int64   `json:"threshold_quota"`
	IntervalUnit      string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	SortOrder         int     `json:"sort_order" gorm:"index"`
	Enabled           bool    `json:"enabled" gorm:"index"`
	CreateTime        int64   `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime        int64   `json:"update_time" gorm:"autoUpdateTime"`
	TermsFingerprint  string  `json:"terms_fingerprint" gorm:"-"`
}

// WalletAutoRechargePresetTerms is the immutable financial contract the user
// sees before opening Toss billing authorization. Names and sort order are
// deliberately excluded because changing display-only metadata must not
// invalidate an otherwise identical authorization session.
type WalletAutoRechargePresetTerms struct {
	PresetId          int     `json:"preset_id"`
	Type              string  `json:"type"`
	TargetScope       string  `json:"target_scope"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	ThresholdQuota    int64   `json:"threshold_quota"`
	IntervalUnit      string  `json:"interval_unit"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	Enabled           bool    `json:"enabled"`
}

var ErrWalletAutoRechargePresetChanged = errors.New("wallet auto recharge preset terms changed; refresh and try again")

func (preset WalletAutoRechargePreset) Terms() WalletAutoRechargePresetTerms {
	return WalletAutoRechargePresetTerms{
		PresetId:          preset.Id,
		Type:              preset.Type,
		TargetScope:       preset.TargetScope,
		Amount:            preset.Amount,
		ThresholdAmount:   preset.ThresholdAmount,
		ThresholdQuota:    preset.ThresholdQuota,
		IntervalUnit:      preset.IntervalUnit,
		IntervalValue:     preset.IntervalValue,
		CustomSeconds:     preset.CustomSeconds,
		ChargeImmediately: preset.ChargeImmediately,
		Enabled:           preset.Enabled,
	}
}

func walletAutoRechargePresetTermsFingerprint(preset WalletAutoRechargePreset) (string, error) {
	payload, err := common.Marshal(preset.Terms())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func populateWalletAutoRechargePresetTermsFingerprint(preset *WalletAutoRechargePreset) error {
	if preset == nil {
		return errors.New("wallet auto recharge preset is required")
	}
	fingerprint, err := walletAutoRechargePresetTermsFingerprint(*preset)
	if err != nil {
		return err
	}
	preset.TermsFingerprint = fingerprint
	return nil
}

type WalletAutoRechargePresetRequest struct {
	Type              string  `json:"type"`
	TargetScope       string  `json:"target_scope"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	ThresholdQuota    int64   `json:"threshold_quota"`
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
	chargeKRW := walletAutoRechargeKRW(req.Amount)
	if chargeKRW <= 0 || chargeKRW > setting.TossMaximumChargeAmountKRW {
		return req, errors.New("wallet auto recharge preset amount is outside Toss limits")
	}
	if chargeKRW < walletAutoRechargeMinimumKRW() {
		return req, errors.New("wallet auto recharge preset amount is below Toss minimum")
	}
	if walletAutoRechargeQuota(req.Amount) <= 0 || walletAutoRechargeMoney(req.Amount) <= 0 {
		return req, errors.New("wallet auto recharge preset quota conversion is invalid")
	}

	switch req.Type {
	case WalletAutoRechargeTypeScheduled:
		if req.IntervalUnit == WalletAutoRechargeIntervalMonth {
			req.ChargeImmediately = false
		}
		if req.IntervalUnit == WalletAutoRechargeIntervalCustom && req.IntervalValue <= 0 {
			req.IntervalValue = 1
		}
		if err := validateWalletAutoRechargeIntervalForCurrentMode(req.IntervalUnit, req.IntervalValue, req.CustomSeconds); err != nil {
			return req, err
		}
	case WalletAutoRechargeTypeThreshold:
		req.ChargeImmediately = false
		if req.ThresholdQuota < 0 {
			return req, errors.New("wallet auto recharge preset threshold quota cannot be negative")
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
		ThresholdQuota:    req.ThresholdQuota,
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
	if err := populateWalletAutoRechargePresetTermsFingerprint(preset); err != nil {
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
		"threshold_quota":    req.ThresholdQuota,
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
	if err := populateWalletAutoRechargePresetTermsFingerprint(&preset); err != nil {
		return nil, err
	}
	return &preset, nil
}

func DisableWalletAutoRechargePreset(id int) error {
	result := DB.Model(&WalletAutoRechargePreset{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled":     false,
		"update_time": time.Now().Unix(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("wallet auto recharge preset not found")
	}
	return nil
}

func ListWalletAutoRechargePresets(includeDisabled bool) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	query := DB.Model(&WalletAutoRechargePreset{})
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("sort_order asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		if err := populateWalletAutoRechargePresetTermsFingerprint(&rows[index]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func walletAutoRechargePresetMatchesTarget(scope string, targetType string) bool {
	return scope == WalletAutoRechargePresetTargetAll ||
		scope == targetType ||
		(scope == WalletAutoRechargePresetTargetUser && targetType == TopUpTargetTypeUser) ||
		(scope == WalletAutoRechargePresetTargetOrganization && targetType == TopUpTargetTypeOrganization)
}

func walletAutoRechargePresetHasValidThresholdQuota(preset WalletAutoRechargePreset) bool {
	return preset.Type != WalletAutoRechargeTypeThreshold || preset.ThresholdQuota > 0
}

func validateWalletAutoRechargePresetForTarget(preset WalletAutoRechargePreset, rechargeType string, targetType string) error {
	return validateWalletAutoRechargePresetForTargetMode(preset, rechargeType, targetType, setting.GetTossConfigSnapshot().TestMode)
}

func validateWalletAutoRechargePresetForTargetMode(preset WalletAutoRechargePreset, rechargeType string, targetType string, testMode bool) error {
	if !preset.Enabled {
		return errors.New("wallet auto recharge preset is disabled")
	}
	if preset.Type != rechargeType {
		return errors.New("wallet auto recharge preset type mismatch")
	}
	if !walletAutoRechargePresetMatchesTarget(preset.TargetScope, targetType) {
		return errors.New("wallet auto recharge preset is not available for target")
	}
	if !walletAutoRechargePresetHasValidThresholdQuota(preset) {
		return errors.New("wallet auto recharge preset threshold quota is invalid")
	}
	if preset.Type == WalletAutoRechargeTypeScheduled {
		if err := validateWalletAutoRechargeIntervalForMode(preset.IntervalUnit, preset.IntervalValue, preset.CustomSeconds, testMode); err != nil {
			return err
		}
	}
	return nil
}

func GetWalletAutoRechargePresetForTarget(id int, rechargeType string, targetType string) (*WalletAutoRechargePreset, error) {
	return GetWalletAutoRechargePresetForTargetMode(id, rechargeType, targetType, setting.GetTossConfigSnapshot().TestMode)
}

func GetWalletAutoRechargePresetForTargetMode(id int, rechargeType string, targetType string, testMode bool) (*WalletAutoRechargePreset, error) {
	var preset WalletAutoRechargePreset
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}
	if err := validateWalletAutoRechargePresetForTargetMode(preset, rechargeType, targetType, testMode); err != nil {
		return nil, err
	}
	if err := populateWalletAutoRechargePresetTermsFingerprint(&preset); err != nil {
		return nil, err
	}
	return &preset, nil
}

func ListWalletAutoRechargePresetsForTarget(targetType string) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	query := DB.Where("enabled = ?", true).
		Where("target_scope = ? OR target_scope = ?", targetType, WalletAutoRechargePresetTargetAll).
		Where("type <> ? OR threshold_quota > ?", WalletAutoRechargeTypeThreshold, 0)
	if !setting.GetTossConfigSnapshot().TestMode {
		query = query.Where("type <> ? OR interval_unit <> ?", WalletAutoRechargeTypeScheduled, WalletAutoRechargeIntervalCustom)
	}
	if err := query.Order("sort_order asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		if err := populateWalletAutoRechargePresetTermsFingerprint(&rows[index]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// CreatePendingWalletAutoRechargeFromPreset locks and validates the exact
// financial terms the browser previously displayed, then creates the immutable
// policy snapshot in the same transaction. An admin update cannot race between
// the comparison and policy creation.
func CreatePendingWalletAutoRechargeFromPreset(req CreateWalletAutoRechargeRequest, expectedFingerprint string, testMode bool) (*WalletAutoRecharge, *WalletAutoRechargePreset, error) {
	expectedFingerprint = strings.TrimSpace(expectedFingerprint)
	if req.PresetId <= 0 || expectedFingerprint == "" {
		return nil, nil, ErrWalletAutoRechargePresetChanged
	}

	var policy *WalletAutoRecharge
	var preset WalletAutoRechargePreset
	err := DB.Transaction(func(tx *gorm.DB) error {
		candidate := &WalletAutoRecharge{
			Type:        req.Type,
			TargetType:  req.TargetType,
			TargetId:    req.TargetId,
			OwnerUserId: req.OwnerUserId,
			Status:      WalletAutoRechargeStatusPending,
		}
		if err := lockAndValidateWalletAutoRechargeOwnerTx(tx, candidate); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&preset, req.PresetId).Error; err != nil {
			return err
		}
		if err := validateWalletAutoRechargePresetForTargetMode(preset, req.Type, req.TargetType, testMode); err != nil {
			return err
		}
		if err := populateWalletAutoRechargePresetTermsFingerprint(&preset); err != nil {
			return err
		}
		if preset.TermsFingerprint != expectedFingerprint {
			return ErrWalletAutoRechargePresetChanged
		}

		req.Amount = preset.Amount
		req.ThresholdAmount = preset.ThresholdAmount
		req.ThresholdQuota = preset.ThresholdQuota
		req.IntervalUnit = preset.IntervalUnit
		req.IntervalValue = preset.IntervalValue
		req.CustomSeconds = preset.CustomSeconds
		req.ChargeImmediately = preset.ChargeImmediately

		built, err := buildPendingWalletAutoRechargeForMode(req, testMode)
		if err != nil {
			return err
		}
		if err := createPendingWalletAutoRechargeTx(tx, built); err != nil {
			return err
		}
		policy = built
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return policy, &preset, nil
}
