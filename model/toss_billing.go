package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// UserBillingKey stores a Toss billing key (암호화) for recurring charges.
type UserBillingKey struct {
	Id                 int    `json:"id"`
	UserId             int    `json:"user_id" gorm:"index"`
	CustomerKey        string `json:"customer_key" gorm:"type:varchar(64);index"`
	EncryptedKey       string `json:"-" gorm:"type:text"` // AES-GCM(billingKey)
	BillingKeyHash     string `json:"-" gorm:"type:varchar(64);index"`
	ProviderCredential string `json:"-" gorm:"type:text"`
	CardCompany        string `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked   string `json:"card_number_masked" gorm:"type:varchar(32)"`
	Status             string `json:"status" gorm:"type:varchar(32);default:'active'"` // active | pending_revocation | revoked
	CreateTime         int64  `json:"create_time"`
}

const (
	BillingKeyStatusActive            = "active"
	BillingKeyStatusPendingRevocation = "pending_revocation"
	BillingKeyStatusRevoked           = "revoked"
	TossBillingMaxFails               = 3

	TossTopUpAmountModeKRW   = "krw"
	TossTopUpAmountModeQuota = "quota"
)

var ErrTossBillingKeyAlreadyDeleted = errors.New("toss billing key already deleted")
var ErrTossBillingChargePending = errors.New("toss billing charge still processing")

const TossRenewalTradeNoPrefix = "toss_sub_renew_"

func tossBillingKeyHash(billingKey string) string {
	return common.GenerateHMAC(strings.TrimSpace(billingKey))
}

func EncryptProviderCredential(secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", nil
	}
	return common.EncryptString(secret)
}

func DecryptProviderCredential(encrypted string) (string, error) {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return "", nil
	}
	return common.DecryptString(encrypted)
}

// TossPlanKRW converts a plan's USD-equivalent price to KRW integer for Toss billing.
// This is the single authoritative KRW conversion source used by both the
// subscription controller and the recurring billing cron.
func TossPlanKRW(priceAmount float64) int64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		return 0
	}
	return decimal.NewFromFloat(priceAmount).Mul(decimal.NewFromFloat(unit)).Round(0).IntPart()
}

func IsTossCardAmountPayableKRW(amount int64) bool {
	return amount >= setting.TossCardMinimumAmountKRW
}

func TossSubscriptionOrderName(title string, renewal bool) string {
	suffix := " 구독"
	fallback := "구독"
	if renewal {
		suffix = " 구독 갱신"
		fallback = "구독 갱신"
	}

	base := strings.TrimSpace(title)
	if base == "" {
		return fallback
	}

	maxBaseRunes := setting.TossOrderNameMaxRunes - len([]rune(suffix))
	if maxBaseRunes <= 0 {
		return truncateRunes(strings.TrimSpace(suffix), setting.TossOrderNameMaxRunes)
	}
	return truncateRunes(base, maxBaseRunes) + suffix
}

func ParseTossRenewalTradeNo(tradeNo string) (subId int, nextBillingTime int64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(tradeNo), TossRenewalTradeNoPrefix)
	if !found {
		return 0, 0, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 2 {
		return 0, 0, false
	}
	parsedSubId, err := strconv.Atoi(parts[0])
	if err != nil || parsedSubId <= 0 {
		return 0, 0, false
	}
	parsedNextBillingTime, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || parsedNextBillingTime <= 0 {
		return 0, 0, false
	}
	return parsedSubId, parsedNextBillingTime, true
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
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

func TossCreditQuotaFromKRW(chargedKRW int64) int {
	unit := setting.TossUnitPrice
	if unit <= 0 || chargedKRW <= 0 {
		return 0
	}
	return int(decimal.NewFromInt(chargedKRW).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Div(decimal.NewFromFloat(unit)).
		IntPart())
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

// TossBillingChargeResult is the provider result needed to settle and audit a
// recurring Toss billing charge without importing controller DTOs into model.
type TossBillingChargeResult struct {
	Done            bool
	Total           int64
	PaymentKey      string
	ProviderPayload string
}

// TossBillingCharger performs a Toss billing charge. Injected by the controller package
// at init to avoid a model→controller import cycle.
type TossBillingCharger func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error)

var tossBillingCharger TossBillingCharger

// SetTossBillingCharger wires in the HTTP charger. Called from controller init().
func SetTossBillingCharger(fn TossBillingCharger) { tossBillingCharger = fn }

// TossBillingRevoker deletes a billing key at Toss. Injected by the controller package
// to avoid a model→controller import cycle.
type TossBillingRevoker func(ctx context.Context, billingKey, secretKey string) error

var tossBillingRevoker TossBillingRevoker

func SetTossBillingRevoker(fn TossBillingRevoker) { tossBillingRevoker = fn }

func revokeTossBillingRemote(ctx context.Context, billingKey, secretKey string) error {
	if strings.TrimSpace(billingKey) == "" || tossBillingRevoker == nil {
		return nil
	}
	return tossBillingRevoker(ctx, billingKey, secretKey)
}

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
	chargeKRW := TossPlanKRW(plan.PriceAmount)
	// Deterministic tradeNo per billing cycle: if the charge succeeds but
	// RenewTossSubscription fails (NextBillingTime not advanced), the next cron tick
	// retries with the SAME tradeNo → Toss Idempotency-Key dedups the charge and the
	// audit-order insert (unique trade_no) only commits once. On successful renew,
	// NextBillingTime advances so the next cycle gets a fresh tradeNo.
	tradeNo := fmt.Sprintf("%s%d_%d", TossRenewalTradeNoPrefix, subId, sub.NextBillingTime)
	if existingOrder := GetSubscriptionOrderByTradeNo(tradeNo); existingOrder != nil && existingOrder.PaymentProvider == PaymentProviderToss {
		if existingOrder.Status == common.TopUpStatusSuccess {
			return nil
		}
		if existingOrder.Status == common.TopUpStatusPending && existingOrder.ProviderAmount > 0 {
			chargeKRW = existingOrder.ProviderAmount
		}
	}
	if !IsTossCardAmountPayableKRW(chargeKRW) {
		disabled, markErr := MarkTossBillingFailure(subId, maxFails)
		if markErr != nil {
			return markErr
		}
		if disabled {
			billingKey, _, secretKey, keyErr := GetTossBillingKeyPlainWithSecret(sub.BillingKeyId)
			if keyErr == nil {
				return revokeTossBillingRemoteAndMarkRevoked(ctx, billingKey, secretKey, sub.BillingKeyId)
			}
		}
		return nil
	}
	order, err := PrepareTossRenewalOrder(subId, tradeNo, plan.PriceAmount, chargeKRW)
	if err != nil {
		return err
	}
	if order == nil || order.Status == common.TopUpStatusSuccess {
		return nil
	}
	if order.ProviderAmount > 0 {
		chargeKRW = order.ProviderAmount
	}
	if !strings.EqualFold(strings.TrimSpace(order.ProviderCurrency), "KRW") {
		return fmt.Errorf("unsupported Toss renewal currency: %s", order.ProviderCurrency)
	}
	if !IsTossCardAmountPayableKRW(chargeKRW) {
		disabled, markErr := MarkTossBillingFailure(subId, maxFails)
		if markErr != nil {
			return markErr
		}
		if disabled {
			billingKey, _, secretKey, keyErr := GetTossBillingKeyPlainWithSecret(order.BillingKeyId)
			if keyErr == nil {
				return revokeTossBillingRemoteAndMarkRevoked(ctx, billingKey, secretKey, order.BillingKeyId)
			}
		}
		return nil
	}
	billingKeyId := order.BillingKeyId
	if billingKeyId <= 0 {
		billingKeyId = sub.BillingKeyId
	}
	billingKey, customerKey, secretKey, err := GetTossBillingKeyPlainWithSecret(billingKeyId)
	if err != nil {
		// key missing/revoked → stop auto-renew immediately
		_, _ = MarkTossBillingFailure(subId, 1)
		return err
	}
	orderName := TossSubscriptionOrderName(plan.Title, true)
	charge, err := tossBillingCharger(ctx, billingKey, customerKey, secretKey, tradeNo, orderName, chargeKRW)
	if errors.Is(err, ErrTossBillingChargePending) {
		return nil
	}
	if err != nil || charge == nil || !charge.Done || charge.Total != chargeKRW {
		disabled, markErr := MarkTossBillingFailure(subId, maxFails)
		if markErr != nil {
			return markErr
		}
		if disabled {
			return revokeTossBillingRemoteAndMarkRevoked(ctx, billingKey, secretKey, billingKeyId)
		}
		return nil
	}
	return RenewTossSubscription(subId, tradeNo, order.Money, chargeKRW, charge.ProviderPayload)
}

func PrepareTossRenewalOrder(subId int, tradeNo string, money float64, providerAmount int64) (*SubscriptionOrder, error) {
	if subId <= 0 {
		return nil, errors.New("subId is invalid")
	}
	if strings.TrimSpace(tradeNo) == "" {
		return nil, errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	var order SubscriptionOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", subId).First(&sub).Error; err != nil {
			return err
		}
		if !sub.AutoRenew || sub.Status != "active" {
			order = SubscriptionOrder{}
			return nil
		}
		if sub.BillingKeyId <= 0 {
			return errors.New("billingKeyId is invalid")
		}
		var key UserBillingKey
		if err := tx.Select("provider_credential").Where("id = ?", sub.BillingKeyId).First(&key).Error; err != nil {
			return err
		}

		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(&order).Error
		if err == nil {
			if order.PaymentProvider != PaymentProviderToss {
				return ErrPaymentMethodMismatch
			}
			if order.Status == common.TopUpStatusSuccess {
				return nil
			}
			if order.Status != common.TopUpStatusPending {
				return ErrSubscriptionOrderStatusInvalid
			}
			changed := false
			if order.ProviderAmount <= 0 {
				order.ProviderAmount = providerAmount
				changed = true
			}
			if strings.TrimSpace(order.ProviderCurrency) == "" {
				order.ProviderCurrency = "KRW"
				changed = true
			}
			if order.BillingKeyId <= 0 {
				order.BillingKeyId = sub.BillingKeyId
				changed = true
			}
			if strings.TrimSpace(order.ProviderCredential) == "" {
				order.ProviderCredential = key.ProviderCredential
				changed = true
			}
			if order.Money <= 0 {
				order.Money = money
				changed = true
			}
			if changed {
				return tx.Save(&order).Error
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		order = SubscriptionOrder{
			UserId:             sub.UserId,
			PlanId:             sub.PlanId,
			Money:              money,
			TradeNo:            tradeNo,
			PaymentMethod:      PaymentMethodToss,
			PaymentProvider:    PaymentProviderToss,
			Status:             common.TopUpStatusPending,
			CreateTime:         common.GetTimestamp(),
			ProviderAmount:     providerAmount,
			ProviderCurrency:   "KRW",
			ProviderCredential: key.ProviderCredential,
			BillingKeyId:       sub.BillingKeyId,
		}
		return tx.Create(&order).Error
	})
	if err != nil {
		return nil, err
	}
	if order.Id == 0 {
		return nil, nil
	}
	return &order, nil
}

// StoreTossBillingKey encrypts and persists a billing key, returning its row id.
func StoreTossBillingKey(userId int, customerKey, billingKey, cardCompany, cardMasked string) (int, error) {
	return StoreTossBillingKeyWithSecret(userId, customerKey, billingKey, cardCompany, cardMasked, "")
}

func StoreTossBillingKeyWithSecret(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusActive)
}

func StoreTossBillingKeyPendingRevocationWithSecret(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusPendingRevocation)
}

func storeTossBillingKeyWithSecretStatus(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey, status string) (int, error) {
	billingKey = strings.TrimSpace(billingKey)
	if billingKey == "" {
		return 0, errors.New("empty billing key")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = BillingKeyStatusActive
	}
	enc, err := common.EncryptString(billingKey)
	if err != nil {
		return 0, err
	}
	credential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return 0, err
	}
	row := &UserBillingKey{
		UserId:             userId,
		CustomerKey:        customerKey,
		EncryptedKey:       enc,
		BillingKeyHash:     tossBillingKeyHash(billingKey),
		ProviderCredential: credential,
		CardCompany:        cardCompany,
		CardNumberMasked:   cardMasked,
		Status:             status,
		CreateTime:         common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		return 0, err
	}
	return row.Id, nil
}

// GetTossBillingKeyPlain returns the decrypted billing key for an active row.
func GetTossBillingKeyPlain(id int) (key string, customerKey string, err error) {
	key, customerKey, _, err = getTossBillingKeyPlainWithSecretTx(nil, id)
	return key, customerKey, err
}

func GetTossBillingKeyPlainWithSecret(id int) (key string, customerKey string, secretKey string, err error) {
	return getTossBillingKeyPlainWithSecretTx(nil, id)
}

func getTossBillingKeyPlainTx(tx *gorm.DB, id int) (key string, customerKey string, err error) {
	key, customerKey, _, err = getTossBillingKeyPlainWithSecretTx(tx, id)
	return key, customerKey, err
}

func getTossBillingKeyPlainWithSecretTx(tx *gorm.DB, id int) (key string, customerKey string, secretKey string, err error) {
	db := DB
	if tx != nil {
		db = tx
	}
	var row UserBillingKey
	if err = db.Where("id = ?", id).First(&row).Error; err != nil {
		return "", "", "", err
	}
	if row.Status != BillingKeyStatusActive {
		return "", "", "", errors.New("billing key revoked")
	}
	plain, err := common.DecryptString(row.EncryptedKey)
	if err != nil {
		return "", "", "", err
	}
	secret, err := DecryptProviderCredential(row.ProviderCredential)
	if err != nil {
		return "", "", "", err
	}
	return plain, row.CustomerKey, secret, nil
}

// RevokeTossBillingKey marks a billing key revoked (best-effort).
func RevokeTossBillingKey(tx *gorm.DB, id int) error {
	db := DB
	if tx != nil {
		db = tx
	}
	if err := db.Model(&UserBillingKey{}).Where("id = ?", id).Update("status", BillingKeyStatusRevoked).Error; err != nil {
		return err
	}
	return db.Model(&UserSubscription{}).
		Where("billing_key_id = ? AND auto_renew = ?", id, commonTrueVal).
		Updates(map[string]interface{}{
			"auto_renew": false,
			"updated_at": common.GetTimestamp(),
		}).Error
}

// MarkTossBillingKeyPendingRevocation keeps a key out of future charges while
// preserving enough state to retry remote deletion at Toss.
func MarkTossBillingKeyPendingRevocation(tx *gorm.DB, id int) error {
	db := DB
	if tx != nil {
		db = tx
	}
	return db.Model(&UserBillingKey{}).
		Where("id = ? AND status <> ?", id, BillingKeyStatusRevoked).
		Update("status", BillingKeyStatusPendingRevocation).Error
}

func revokeTossBillingRemoteAndMarkRevoked(ctx context.Context, billingKey, secretKey string, keyIds ...int) error {
	if err := revokeTossBillingRemote(ctx, billingKey, secretKey); err != nil {
		if !errors.Is(err, ErrTossBillingKeyAlreadyDeleted) {
			return err
		}
	}
	for _, keyId := range keyIds {
		if keyId <= 0 {
			continue
		}
		if err := RevokeTossBillingKey(nil, keyId); err != nil {
			return err
		}
	}
	return nil
}

// RetryPendingTossBillingKeyRevocations retries remote deletion for billing keys
// that were disabled locally but could not be deleted at Toss during the original
// cancellation/failure path.
func RetryPendingTossBillingKeyRevocations(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []UserBillingKey
	if err := DB.Where("status = ?", BillingKeyStatusPendingRevocation).
		Order("id asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var revoked int64
	for i := range rows {
		plain, err := common.DecryptString(rows[i].EncryptedKey)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to decrypt pending Toss billing key for remote deletion: billing_key_id=%d error=%v", rows[i].Id, err))
			continue
		}
		secret, err := DecryptProviderCredential(rows[i].ProviderCredential)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to decrypt pending Toss billing credential for remote deletion: billing_key_id=%d error=%v", rows[i].Id, err))
			continue
		}
		if err := revokeTossBillingRemoteAndMarkRevoked(ctx, plain, secret, rows[i].Id); err != nil {
			common.SysError(fmt.Sprintf("failed to retry Toss billing key remote deletion: billing_key_id=%d error=%v", rows[i].Id, err))
			continue
		}
		revoked++
	}
	return revoked, nil
}

// RevokeTossBillingKeyByPlain marks a locally stored key revoked after Toss reports
// that exact billingKey was deleted. The billingKey is encrypted at rest, so we
// decrypt candidate rows and compare in memory.
func RevokeTossBillingKeyByPlain(customerKey, billingKey string) (bool, error) {
	billingKey = strings.TrimSpace(billingKey)
	if billingKey == "" {
		return false, nil
	}
	statuses := []string{BillingKeyStatusActive, BillingKeyStatusPendingRevocation}
	hashQuery := DB.Where("status IN ? AND billing_key_hash = ?", statuses, tossBillingKeyHash(billingKey))
	if strings.TrimSpace(customerKey) != "" {
		hashQuery = hashQuery.Where("customer_key = ?", strings.TrimSpace(customerKey))
	}
	var hashed UserBillingKey
	if err := hashQuery.First(&hashed).Error; err == nil {
		if err := RevokeTossBillingKey(nil, hashed.Id); err != nil {
			return false, err
		}
		return true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	query := DB.Where("status IN ? AND (billing_key_hash = '' OR billing_key_hash IS NULL)", statuses)
	if strings.TrimSpace(customerKey) != "" {
		query = query.Where("customer_key = ?", strings.TrimSpace(customerKey))
	}
	var rows []UserBillingKey
	if err := query.Find(&rows).Error; err != nil {
		return false, err
	}
	for i := range rows {
		plain, err := common.DecryptString(rows[i].EncryptedKey)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to decrypt Toss billing key for webhook matching: billing_key_id=%d error=%v", rows[i].Id, err))
			continue
		}
		if plain != billingKey {
			continue
		}
		if err := RevokeTossBillingKey(nil, rows[i].Id); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func backfillTossBillingKeyHashes(limit int) error {
	if limit <= 0 {
		limit = 500
	}
	lastID := 0
	for {
		var rows []UserBillingKey
		if err := DB.Where("(billing_key_hash = '' OR billing_key_hash IS NULL) AND id > ?", lastID).
			Order("id asc").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for i := range rows {
			lastID = rows[i].Id
			plain, err := common.DecryptString(rows[i].EncryptedKey)
			if err != nil {
				common.SysError(fmt.Sprintf("failed to decrypt Toss billing key for hash backfill: billing_key_id=%d error=%v", rows[i].Id, err))
				continue
			}
			if err := DB.Model(&UserBillingKey{}).
				Where("id = ?", rows[i].Id).
				Update("billing_key_hash", tossBillingKeyHash(plain)).Error; err != nil {
				return err
			}
		}
		if len(rows) < limit {
			return nil
		}
	}
}
