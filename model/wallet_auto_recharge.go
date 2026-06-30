package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	WalletAutoRechargeTypeScheduled = "scheduled"
	WalletAutoRechargeTypeThreshold = "threshold"
)

const (
	WalletAutoRechargeStatusPending   = "pending"
	WalletAutoRechargeStatusActive    = "active"
	WalletAutoRechargeStatusCancelled = "cancelled"
	WalletAutoRechargeStatusFailed    = "failed"
)

const (
	WalletAutoRechargeIntervalMonth  = "month"
	WalletAutoRechargeIntervalDay    = "day"
	WalletAutoRechargeIntervalCustom = "custom"
)

const (
	WalletAutoRechargeThresholdCooldownSeconds = 3600
	WalletAutoRechargeDailyLimit               = 3
)

type WalletAutoRecharge struct {
	Id                int     `json:"id"`
	PresetId          int     `json:"preset_id" gorm:"index"`
	Type              string  `json:"type" gorm:"type:varchar(32);index"`
	TargetType        string  `json:"target_type" gorm:"type:varchar(32);index"`
	TargetId          int     `json:"target_id" gorm:"index"`
	ActiveKey         *string `json:"-" gorm:"type:varchar(128);uniqueIndex"`
	OwnerUserId       int     `json:"owner_user_id" gorm:"index"`
	BillingKeyId      int     `json:"billing_key_id" gorm:"index"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	ThresholdQuota    int     `json:"threshold_quota"`
	IntervalUnit      string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	NextChargeTime    int64   `json:"next_charge_time" gorm:"index"`
	LastChargeTime    int64   `json:"last_charge_time"`
	CooldownUntil     int64   `json:"cooldown_until" gorm:"index"`
	DailyChargeCount  int     `json:"daily_charge_count"`
	DailyChargeDate   string  `json:"daily_charge_date" gorm:"type:varchar(10)"`
	Status            string  `json:"status" gorm:"type:varchar(16);index"`
	FailCount         int     `json:"fail_count"`
	LastError         string  `json:"last_error" gorm:"type:varchar(255)"`
	CardCompany       string  `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked  string  `json:"card_number_masked" gorm:"type:varchar(32)"`
	LastTradeNo       string  `json:"last_trade_no" gorm:"type:varchar(255);index"`
	CustomerKey       string  `json:"-" gorm:"type:varchar(64);index"`
	AuthTradeNo       string  `json:"-" gorm:"type:varchar(255);uniqueIndex"`
	CreateTime        int64   `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime        int64   `json:"update_time" gorm:"autoUpdateTime"`
}

type CreateWalletAutoRechargeRequest struct {
	PresetId          int
	Type              string
	TargetType        string
	TargetId          int
	OwnerUserId       int
	CustomerKey       string
	AuthTradeNo       string
	Amount            float64
	ThresholdAmount   float64
	IntervalUnit      string
	IntervalValue     int
	CustomSeconds     int64
	ChargeImmediately bool
}

func (req CreateWalletAutoRechargeRequest) normalizeAndValidate() (CreateWalletAutoRechargeRequest, error) {
	if req.Type != WalletAutoRechargeTypeScheduled && req.Type != WalletAutoRechargeTypeThreshold {
		return req, errors.New("invalid wallet auto recharge type")
	}
	if req.TargetType != TopUpTargetTypeUser && req.TargetType != TopUpTargetTypeOrganization {
		return req, errors.New("invalid wallet auto recharge target")
	}
	if req.TargetId <= 0 || req.OwnerUserId <= 0 {
		return req, errors.New("invalid wallet auto recharge owner or target")
	}
	if req.Amount <= 0 {
		return req, errors.New("wallet auto recharge amount must be positive")
	}
	if walletAutoRechargeKRW(req.Amount) < int64(setting.TossMinTopUp) {
		return req, errors.New("wallet auto recharge amount is below Toss minimum")
	}
	switch req.Type {
	case WalletAutoRechargeTypeScheduled:
		switch req.IntervalUnit {
		case WalletAutoRechargeIntervalMonth, WalletAutoRechargeIntervalDay:
			if req.IntervalValue <= 0 {
				return req, errors.New("wallet auto recharge interval value is invalid")
			}
		case WalletAutoRechargeIntervalCustom:
			if req.CustomSeconds <= 0 {
				return req, errors.New("wallet auto recharge custom seconds is invalid")
			}
			if req.IntervalValue <= 0 {
				req.IntervalValue = 1
			}
		default:
			return req, errors.New("wallet auto recharge interval is required")
		}
	case WalletAutoRechargeTypeThreshold:
		if req.ThresholdAmount < 0 {
			return req, errors.New("wallet auto recharge threshold cannot be negative")
		}
	}
	return req, nil
}

func CreatePendingWalletAutoRecharge(req CreateWalletAutoRechargeRequest) (*WalletAutoRecharge, error) {
	req, err := req.normalizeAndValidate()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	activeKey := walletAutoRechargeActiveKey(req.TargetType, req.TargetId, req.Type)
	policy := &WalletAutoRecharge{
		PresetId:          req.PresetId,
		Type:              req.Type,
		TargetType:        req.TargetType,
		TargetId:          req.TargetId,
		ActiveKey:         &activeKey,
		OwnerUserId:       req.OwnerUserId,
		Amount:            req.Amount,
		ThresholdAmount:   req.ThresholdAmount,
		ThresholdQuota:    walletAutoRechargeQuota(req.ThresholdAmount),
		IntervalUnit:      req.IntervalUnit,
		IntervalValue:     req.IntervalValue,
		CustomSeconds:     req.CustomSeconds,
		ChargeImmediately: req.ChargeImmediately,
		Status:            WalletAutoRechargeStatusPending,
		CustomerKey:       req.CustomerKey,
		AuthTradeNo:       req.AuthTradeNo,
		CreateTime:        now.Unix(),
		UpdateTime:        now.Unix(),
	}
	if req.Type == WalletAutoRechargeTypeScheduled {
		policy.NextChargeTime = nextWalletChargeTime(now, req.IntervalUnit, req.IntervalValue, req.CustomSeconds).Unix()
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(policy).Error; err != nil {
			if isWalletAutoRechargeActiveKeyConflict(err) {
				return errors.New("active wallet auto recharge already exists")
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func ActivateWalletAutoRechargeFromToss(tradeNo string, billingKeyId int, cardCompany string, cardMasked string, chargeNow bool, charger TossBillingCharger) (*WalletAutoRecharge, error) {
	if tradeNo == "" {
		return nil, errors.New("wallet auto recharge tradeNo is required")
	}
	if billingKeyId <= 0 {
		return nil, errors.New("wallet auto recharge billing key is invalid")
	}

	now := time.Now()
	var policy WalletAutoRecharge
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
			return err
		}
		if policy.Status == WalletAutoRechargeStatusCancelled {
			return errors.New("wallet auto recharge already cancelled")
		}

		updates := map[string]interface{}{
			"billing_key_id":     billingKeyId,
			"card_company":       cardCompany,
			"card_number_masked": cardMasked,
			"status":             WalletAutoRechargeStatusActive,
			"active_key":         walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, policy.Type),
			"last_error":         "",
			"fail_count":         0,
			"update_time":        now.Unix(),
		}
		if policy.Type == WalletAutoRechargeTypeScheduled && (chargeNow || policy.ChargeImmediately) {
			updates["next_charge_time"] = now.Unix()
			policy.NextChargeTime = now.Unix()
		}
		if err := tx.Model(&policy).Updates(updates).Error; err != nil {
			if isWalletAutoRechargeActiveKeyConflict(err) {
				return errors.New("active wallet auto recharge already exists")
			}
			return err
		}
		policy.BillingKeyId = billingKeyId
		policy.CardCompany = cardCompany
		policy.CardNumberMasked = cardMasked
		policy.Status = WalletAutoRechargeStatusActive
		activeKey := walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, policy.Type)
		policy.ActiveKey = &activeKey
		policy.FailCount = 0
		policy.LastError = ""
		return nil
	})
	if err != nil {
		return nil, err
	}

	if chargeNow || policy.ChargeImmediately {
		if err := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, WalletAutoRechargeDailyLimit, charger); err != nil {
			return nil, err
		}
		if err := DB.First(&policy, policy.Id).Error; err != nil {
			return nil, err
		}
	}
	return &policy, nil
}

func nextWalletChargeTime(base time.Time, unit string, value int, customSeconds int64) time.Time {
	if value <= 0 {
		value = 1
	}
	switch unit {
	case WalletAutoRechargeIntervalDay:
		return base.AddDate(0, 0, value)
	case WalletAutoRechargeIntervalCustom:
		if customSeconds <= 0 {
			customSeconds = 86400
		}
		return base.Add(time.Duration(customSeconds) * time.Second)
	default:
		return base.AddDate(0, value, 0)
	}
}

func CancelWalletAutoRecharge(id int, targetType string, targetId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).First(&policy).Error; err != nil {
			return err
		}
		if policy.Status == WalletAutoRechargeStatusCancelled {
			return nil
		}
		return tx.Model(&policy).Updates(map[string]interface{}{
			"active_key":  nil,
			"status":      WalletAutoRechargeStatusCancelled,
			"update_time": time.Now().Unix(),
		}).Error
	})
}

func CancelPendingWalletAutoRechargeByTradeNo(tradeNo string, targetType string, targetId int) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("wallet auto recharge tradeNo is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Where("auth_trade_no = ? AND target_type = ? AND target_id = ? AND status = ?", tradeNo, targetType, targetId, WalletAutoRechargeStatusPending).First(&policy).Error; err != nil {
			return err
		}
		return tx.Model(&policy).Updates(map[string]interface{}{
			"active_key":  nil,
			"status":      WalletAutoRechargeStatusCancelled,
			"update_time": time.Now().Unix(),
		}).Error
	})
}

func ListWalletAutoRecharges(targetType string, targetId int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("target_type = ? AND target_id = ?", targetType, targetId).Order("id desc").Find(&rows).Error
	return rows, err
}

func GetDueScheduledWalletAutoRecharges(nowUnix int64, limit int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("type = ? AND status = ? AND next_charge_time > 0 AND next_charge_time <= ?", WalletAutoRechargeTypeScheduled, WalletAutoRechargeStatusActive, nowUnix).
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func GetActiveThresholdWalletAutoRecharges(limit int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("type = ? AND status = ?", WalletAutoRechargeTypeThreshold, WalletAutoRechargeStatusActive).
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func ProcessWalletAutoRecharge(ctx context.Context, policyId int, now time.Time, maxFails int, charger TossBillingCharger) error {
	if charger == nil {
		return errors.New("toss wallet auto recharge charger not configured")
	}
	if maxFails <= 0 {
		maxFails = 1
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", policyId).First(&policy).Error; err != nil {
			return err
		}
		if policy.Status != WalletAutoRechargeStatusActive {
			return nil
		}
		if policy.Type == WalletAutoRechargeTypeScheduled && policy.NextChargeTime > now.Unix() {
			return nil
		}
		if policy.Type == WalletAutoRechargeTypeThreshold {
			ok, err := walletThresholdShouldCharge(tx, &policy, now)
			if err != nil || !ok {
				return err
			}
		}

		billingKey, customerKey, err := getTossBillingKeyPlainTx(tx, policy.BillingKeyId)
		if err != nil {
			return markWalletAutoRechargeFailure(tx, &policy, maxFails, err)
		}

		chargeKRW := walletAutoRechargeKRW(policy.Amount)
		tradeNo := walletAutoRechargeTradeNo(policy, now)
		done, total, err := charger(ctx, billingKey, customerKey, tradeNo, "지갑 자동충전", chargeKRW)
		if err != nil || !done || total != chargeKRW {
			if err == nil {
				err = fmt.Errorf("toss wallet auto recharge amount mismatch")
			}
			return markWalletAutoRechargeFailure(tx, &policy, maxFails, err)
		}
		return creditWalletAutoRecharge(tx, &policy, tradeNo, chargeKRW, now)
	})
}

func ProcessWalletAutoRechargeWithConfiguredCharger(ctx context.Context, policyId int, now time.Time, maxFails int) error {
	return ProcessWalletAutoRecharge(ctx, policyId, now, maxFails, tossBillingCharger)
}

func walletAutoRechargeKRW(amount float64) int64 {
	return decimal.NewFromFloat(amount).Round(0).IntPart()
}

func walletAutoRechargeMoney(amountKRW float64) float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromFloat(amountKRW).Div(decimal.NewFromFloat(unit)).InexactFloat64()
}

func walletAutoRechargeQuota(amount float64) int {
	if amount <= 0 {
		return 0
	}
	money := walletAutoRechargeMoney(float64(walletAutoRechargeKRW(amount)))
	return int(decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
}

func walletAutoRechargeTradeNo(policy WalletAutoRecharge, now time.Time) string {
	if policy.Type == WalletAutoRechargeTypeThreshold {
		return fmt.Sprintf("wallet_auto_%d_%s_%d", policy.Id, now.Format("2006010215"), policy.DailyChargeCount+1)
	}
	return fmt.Sprintf("wallet_auto_%d_%d", policy.Id, policy.NextChargeTime)
}

func walletAutoRechargeActiveKey(targetType string, targetId int, rechargeType string) string {
	return fmt.Sprintf("%s:%d:%s", targetType, targetId, rechargeType)
}

func isWalletAutoRechargeActiveKeyConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "wallet_auto_recharges.active_key") ||
		strings.Contains(message, "wallet_auto_recharge.active_key") ||
		strings.Contains(message, "active_key") && strings.Contains(message, "duplicate") ||
		strings.Contains(message, "active_key") && strings.Contains(message, "unique")
}

func walletThresholdShouldCharge(tx *gorm.DB, policy *WalletAutoRecharge, now time.Time) (bool, error) {
	if policy.CooldownUntil > now.Unix() {
		return false, nil
	}

	today := now.Format("2006-01-02")
	if policy.DailyChargeDate != today {
		policy.DailyChargeDate = today
		policy.DailyChargeCount = 0
	}
	if policy.DailyChargeCount >= WalletAutoRechargeDailyLimit {
		return false, nil
	}

	switch policy.TargetType {
	case TopUpTargetTypeOrganization:
		var org Organization
		if err := tx.Select("quota").Where("id = ?", policy.TargetId).First(&org).Error; err != nil {
			return false, err
		}
		return org.Quota <= policy.ThresholdQuota, nil
	default:
		var user User
		if err := tx.Select("quota").Where("id = ?", policy.TargetId).First(&user).Error; err != nil {
			return false, err
		}
		return user.Quota <= policy.ThresholdQuota, nil
	}
}

func creditWalletAutoRecharge(tx *gorm.DB, policy *WalletAutoRecharge, tradeNo string, chargeKRW int64, now time.Time) error {
	quotaToAdd := walletAutoRechargeQuota(float64(chargeKRW))
	if quotaToAdd <= 0 {
		return errors.New("invalid wallet auto recharge quota")
	}
	money := walletAutoRechargeMoney(float64(chargeKRW))

	topUp := &TopUp{
		UserId:          policy.OwnerUserId,
		TargetType:      policy.TargetType,
		TargetId:        policy.TargetId,
		Amount:          chargeKRW,
		Money:           money,
		TradeNo:         tradeNo,
		ProviderOrderId: tradeNo,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		CreateTime:      now.Unix(),
		CompleteTime:    now.Unix(),
		Status:          common.TopUpStatusSuccess,
	}
	if err := tx.Create(topUp).Error; err != nil {
		return err
	}
	if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
		return err
	}

	updates := map[string]interface{}{
		"last_charge_time": now.Unix(),
		"last_trade_no":    tradeNo,
		"fail_count":       0,
		"last_error":       "",
		"update_time":      now.Unix(),
	}
	if policy.Type == WalletAutoRechargeTypeScheduled {
		updates["next_charge_time"] = nextWalletChargeTime(time.Unix(policy.NextChargeTime, 0), policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds).Unix()
	}
	if policy.Type == WalletAutoRechargeTypeThreshold {
		updates["cooldown_until"] = now.Add(WalletAutoRechargeThresholdCooldownSeconds * time.Second).Unix()
		updates["daily_charge_date"] = now.Format("2006-01-02")
		updates["daily_charge_count"] = policy.DailyChargeCount + 1
	}
	return tx.Model(policy).Updates(updates).Error
}

func markWalletAutoRechargeFailure(tx *gorm.DB, policy *WalletAutoRecharge, maxFails int, cause error) error {
	failCount := policy.FailCount + 1
	status := policy.Status
	if failCount >= maxFails {
		status = WalletAutoRechargeStatusFailed
	}
	message := cause.Error()
	if len(message) > 255 {
		message = message[:255]
	}
	updates := map[string]interface{}{
		"fail_count":  failCount,
		"status":      status,
		"last_error":  message,
		"update_time": time.Now().Unix(),
	}
	if status == WalletAutoRechargeStatusFailed {
		updates["active_key"] = nil
	}
	return tx.Model(policy).Updates(updates).Error
}

func getTossBillingKeyPlainTx(tx *gorm.DB, id int) (string, string, error) {
	var row UserBillingKey
	if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
		return "", "", err
	}
	if row.Status != BillingKeyStatusActive {
		return "", "", errors.New("billing key revoked")
	}
	plain, err := common.DecryptString(row.EncryptedKey)
	if err != nil {
		return "", "", err
	}
	return plain, row.CustomerKey, nil
}
