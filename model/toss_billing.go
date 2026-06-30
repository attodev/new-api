package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// UserBillingKey stores a Toss billing key (암호화) for recurring charges.
type UserBillingKey struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id" gorm:"index"`
	CustomerKey      string `json:"customer_key" gorm:"type:varchar(64);index"`
	EncryptedKey     string `json:"-" gorm:"type:text"` // AES-GCM(billingKey)
	CardCompany      string `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked string `json:"card_number_masked" gorm:"type:varchar(32)"`
	Status           string `json:"status" gorm:"type:varchar(16);default:'active'"` // active | revoked
	CreateTime       int64  `json:"create_time"`
}

const (
	BillingKeyStatusActive  = "active"
	BillingKeyStatusRevoked = "revoked"
)

// TossPlanKRW converts a plan's USD-equivalent price to KRW integer for Toss billing.
// This is the single authoritative KRW conversion source used by both the
// subscription controller and the recurring billing cron.
func TossPlanKRW(priceAmount float64) int64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromFloat(priceAmount).Mul(decimal.NewFromFloat(unit)).Round(0).IntPart()
}

// TossBillingCharger performs a Toss billing charge. Injected by the controller package
// at init to avoid a model→controller import cycle.
// Returns (statusDONE, totalAmount, error).
type TossBillingCharger func(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (done bool, total int64, err error)

var tossBillingCharger TossBillingCharger

// SetTossBillingCharger wires in the HTTP charger. Called from controller init().
func SetTossBillingCharger(fn TossBillingCharger) { tossBillingCharger = fn }

// ProcessTossRenewal charges one due subscription and applies renew/fail bookkeeping.
func ProcessTossRenewal(ctx context.Context, subId int, maxFails int) error {
	if tossBillingCharger == nil {
		return fmt.Errorf("toss billing charger not configured")
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", subId).First(&sub).Error; err != nil {
		return err
	}
	plan, err := GetSubscriptionPlanById(sub.PlanId)
	if err != nil {
		return err
	}
	billingKey, customerKey, err := GetTossBillingKeyPlain(sub.BillingKeyId)
	if err != nil {
		// key missing/revoked → stop auto-renew immediately
		_ = MarkTossBillingFailure(subId, 1)
		return err
	}
	chargeKRW := TossPlanKRW(plan.PriceAmount)
	tradeNo := fmt.Sprintf("toss_sub_renew_%d_%d", subId, time.Now().Unix())
	orderName := fmt.Sprintf("%s 구독 갱신", plan.Title)
	done, total, err := tossBillingCharger(ctx, billingKey, customerKey, tradeNo, orderName, chargeKRW)
	if err != nil || !done || total != chargeKRW {
		return MarkTossBillingFailure(subId, maxFails)
	}
	return RenewTossSubscription(subId, tradeNo, plan.PriceAmount)
}

// StoreTossBillingKey encrypts and persists a billing key, returning its row id.
func StoreTossBillingKey(userId int, customerKey, billingKey, cardCompany, cardMasked string) (int, error) {
	if billingKey == "" {
		return 0, errors.New("empty billing key")
	}
	enc, err := common.EncryptString(billingKey)
	if err != nil {
		return 0, err
	}
	row := &UserBillingKey{
		UserId:           userId,
		CustomerKey:      customerKey,
		EncryptedKey:     enc,
		CardCompany:      cardCompany,
		CardNumberMasked: cardMasked,
		Status:           BillingKeyStatusActive,
		CreateTime:       common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		return 0, err
	}
	return row.Id, nil
}

// GetTossBillingKeyPlain returns the decrypted billing key for an active row.
func GetTossBillingKeyPlain(id int) (key string, customerKey string, err error) {
	var row UserBillingKey
	if err = DB.Where("id = ?", id).First(&row).Error; err != nil {
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

// RevokeTossBillingKey marks a billing key revoked (best-effort).
func RevokeTossBillingKey(tx *gorm.DB, id int) error {
	db := DB
	if tx != nil {
		db = tx
	}
	return db.Model(&UserBillingKey{}).Where("id = ?", id).Update("status", BillingKeyStatusRevoked).Error
}
