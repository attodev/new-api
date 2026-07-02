package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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

	TossTopUpAmountModeKRW   = "krw"
	TossTopUpAmountModeQuota = "quota"
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

// TossTopUpChargedKRW returns the KRW amount charged for a Toss top-up amount.
// The input amount is the user-entered KRW/quota-equivalent amount; group
// top-up ratios and amount discounts are applied to the actual card charge.
func TossTopUpChargedKRW(amountKRW int64, group string) int64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amountKRW)]; ok && ds > 0 {
		discount = ds
	}
	return decimal.NewFromInt(amountKRW).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount)).
		Round(0).
		IntPart()
}

// TossUSDEquivalent converts charged KRW to the USD-equivalent stored in TopUp.Money.
func TossUSDEquivalent(chargedKRW int64) float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromInt(chargedKRW).Div(decimal.NewFromFloat(unit)).InexactFloat64()
}

type TossTopUpQuote struct {
	AmountMode   string  `json:"amount_mode"`
	InputAmount  int64   `json:"input_amount"`
	ChargeKRW    int64   `json:"charge_amount"`
	CreditAmount float64 `json:"credit_amount"`
	CreditQuota  int     `json:"credit_quota"`
	UnitPrice    float64 `json:"unit_price"`
}

func NormalizeTossTopUpAmountMode(amountMode string) string {
	switch amountMode {
	case TossTopUpAmountModeQuota:
		return TossTopUpAmountModeQuota
	default:
		return TossTopUpAmountModeKRW
	}
}

func tossTopUpUnitPrice() float64 {
	return setting.TossUnitPrice
}

func tossTopUpPriceFactor(amount int64, group string) float64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}
	return ratio * discount
}

func QuoteTossTopUp(amount int64, amountMode string, group string) TossTopUpQuote {
	mode := NormalizeTossTopUpAmountMode(amountMode)
	unit := tossTopUpUnitPrice()
	if unit <= 0 {
		return TossTopUpQuote{
			AmountMode:  mode,
			InputAmount: amount,
			UnitPrice:   0,
		}
	}
	factor := tossTopUpPriceFactor(amount, group)

	quote := TossTopUpQuote{
		AmountMode:  mode,
		InputAmount: amount,
		UnitPrice:   unit,
	}
	if amount <= 0 {
		return quote
	}

	switch mode {
	case TossTopUpAmountModeQuota:
		credit := decimal.NewFromInt(amount)
		charge := credit.
			Mul(decimal.NewFromFloat(unit)).
			Mul(decimal.NewFromFloat(factor)).
			Round(0).
			IntPart()
		quote.ChargeKRW = charge
		quote.CreditAmount = credit.InexactFloat64()
		quote.CreditQuota = int(credit.
			Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
			IntPart())
	default:
		charge := decimal.NewFromInt(amount)
		unitDec := decimal.NewFromFloat(unit)
		factorDec := decimal.NewFromFloat(factor)
		credit := charge.
			Div(unitDec).
			Div(factorDec)
		quote.ChargeKRW = amount
		quote.CreditAmount = credit.InexactFloat64()
		quote.CreditQuota = int(decimal.NewFromInt(amount).
			Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
			Div(unitDec).
			Div(factorDec).
			IntPart())
	}

	return quote
}

// tossNextBillingTime returns when to charge the next period: a lead BEFORE endUnix so the
// renewal cron extends the subscription before ExpireDueSubscriptions can expire it.
// lead = min(1h, period/4), clamped to stay within [startUnix, endUnix].
func tossNextBillingTime(startUnix, endUnix int64) int64 {
	period := endUnix - startUnix
	if period <= 0 {
		return endUnix
	}
	lead := period / 4
	if lead > 3600 {
		lead = 3600
	}
	next := endUnix - lead
	if next < startUnix {
		next = startUnix
	}
	return next
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
	// Re-check right before charging: a cancel that landed after GetDueTossRenewals
	// must not get charged.
	if !sub.AutoRenew || sub.Status != "active" {
		return nil
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
	// Deterministic tradeNo per billing cycle: if the charge succeeds but
	// RenewTossSubscription fails (NextBillingTime not advanced), the next cron tick
	// retries with the SAME tradeNo → Toss Idempotency-Key dedups the charge and the
	// audit-order insert (unique trade_no) only commits once. On successful renew,
	// NextBillingTime advances so the next cycle gets a fresh tradeNo.
	tradeNo := fmt.Sprintf("toss_sub_renew_%d_%d", subId, sub.NextBillingTime)
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
