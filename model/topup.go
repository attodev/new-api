package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id" gorm:"index"`
	TargetType         string  `json:"target_type" gorm:"type:varchar(32);default:'user'"`
	TargetId           int     `json:"target_id" gorm:"default:0;index"`
	Amount             int64   `json:"amount"`
	Money              float64 `json:"money"`
	Quota              int     `json:"quota" gorm:"default:0"`
	TradeNo            string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	ProviderOrderId    string  `json:"provider_order_id" gorm:"type:varchar(255);index"`
	ProviderOrderTime  int64   `json:"provider_order_time" gorm:"default:0;index"`
	ProviderCredential string  `json:"-" gorm:"type:text"`
	PaymentMethod      string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider    string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime         int64   `json:"create_time"`
	CompleteTime       int64   `json:"complete_time"`
	Status             string  `json:"status"`
}

const (
	TopUpTargetTypeUser         = "user"
	TopUpTargetTypeOrganization = "organization"
)

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
	PaymentMethodPayPal       = "paypal"
	PaymentMethodToss         = "toss"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
	PaymentProviderPayPal       = "paypal"
	PaymentProviderToss         = "toss"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
)

type TossTopUpReconciler func(ctx context.Context, topUp TopUp) (resolved bool, err error)

var tossTopUpReconciler TossTopUpReconciler

func SetTossTopUpReconciler(fn TossTopUpReconciler) {
	tossTopUpReconciler = fn
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func (topUp *TopUp) EffectiveTargetType() string {
	if topUp.TargetType == TopUpTargetTypeOrganization {
		return TopUpTargetTypeOrganization
	}
	return TopUpTargetTypeUser
}

func (topUp *TopUp) EffectiveTargetId() int {
	if topUp.TargetId > 0 {
		return topUp.TargetId
	}
	return topUp.UserId
}

func CreditTopUpTarget(tx *gorm.DB, topUp *TopUp, quota int) error {
	if quota <= 0 {
		return errors.New("invalid top-up quota")
	}
	switch topUp.EffectiveTargetType() {
	case TopUpTargetTypeOrganization:
		targetId := topUp.EffectiveTargetId()
		result := tx.Model(&Organization{}).Where("id = ?", targetId).Update("quota", gorm.Expr("quota + ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("organization not found")
		}
		return nil
	default:
		result := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	}
}

func CreditedQuotaForTopUp(topUp *TopUp) int {
	if topUp != nil && topUp.Quota > 0 {
		return topUp.Quota
	}
	if topUp == nil {
		return 0
	}
	return int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
}

func CreditedQuotaForTossTopUp(topUp *TopUp) int {
	if topUp != nil && topUp.Quota > 0 {
		return topUp.Quota
	}
	if topUp != nil && topUp.Amount > 0 {
		if quota := TossCreditQuotaFromKRW(topUp.Amount); quota > 0 {
			return quota
		}
	}
	return CreditedQuotaForTopUp(topUp)
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}

	var quota int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota = int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if customerId != "" {
			if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("stripe_customer", customerId).Error; err != nil {
				return err
			}
		}
		err = CreditTopUpTarget(tx, topUp, quota)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("online top-up successful, quota: %v, payment amount: %d", logger.FormatQuota(quota), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds limits the time window for top-up record queries (seconds).
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff returns the earliest create_time (Unix timestamp in seconds) allowed for queries.
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

func GetOrganizationTopUps(organizationId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).
		Where("target_type = ? AND target_id = ? AND create_time >= ?", TopUpTargetTypeOrganization, organizationId, topUpQueryCutoff())
	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// GetAllTopUps retrieves all platform top-up records (admin use, no time window restriction)
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit is the safety upper bound for COUNT when searching top-up records,
// preventing unbounded COUNT on very large tables from causing DoS.
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps searches a user's top-up records by order number
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

func SearchOrganizationTopUps(organizationId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).
		Where("target_type = ? AND target_id = ? AND create_time >= ?", TopUpTargetTypeOrganization, organizationId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count organization search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search organization topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps searches all platform top-up records by order number (admin use, no time window restriction)
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp allows admin to manually complete an order and credit the user
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("order number is missing")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// row-level lock to prevent concurrent manual completions
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}

		// idempotent: return immediately if already succeeded
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("order status is not pending payment, cannot complete manually")
		}

		// calculate quota to credit:
		// - Stripe/PayPal/Toss orders: Money is USD amount after group-rate conversion, multiply by QuotaPerUnit
		// - Other orders (e.g. Epay): Amount is USD amount, multiply by QuotaPerUnit
		if topUp.PaymentProvider == PaymentProviderToss {
			quotaToAdd = CreditedQuotaForTossTopUp(topUp)
		} else if topUp.PaymentProvider == PaymentProviderStripe || topUp.PaymentProvider == PaymentProviderPayPal {
			quotaToAdd = CreditedQuotaForTopUp(topUp)
		} else {
			dAmount := decimal.NewFromInt(topUp.Amount)
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		}
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}

		// mark as complete
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// credit target wallet quota (write to DB immediately for consistency)
		if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
	}

	// record log outside transaction to avoid blocking
	RecordTopupLog(userId, fmt.Sprintf("admin manual top-up successful, quota: %v, payment amount: %f", logger.FormatQuota(quotaToAdd), payMoney), callerIp, paymentMethod, "admin")
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}

	var quota int64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem uses Amount directly as top-up quota (integer)
		quota = topUp.Amount

		// if customer email is provided, try to update user email (only when user email is empty)
		if customerEmail != "" {
			// check if user's current email is empty
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// update to the email used at payment time if user email is empty
			if user.Email == "" {
				if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("email", customerEmail).Error; err != nil {
					return err
				}
			}
		}

		err = CreditTopUpTarget(tx, topUp, int(quota))
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("Creem top-up successful, quota: %v, payment amount: %.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargePayPal(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderPayPal {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// Use decimal arithmetic to avoid float64 → int type mismatch on PostgreSQL.
		quotaToAdd = int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		return CreditTopUpTarget(tx, topUp, quotaToAdd)
	})

	if err != nil {
		common.SysError("paypal topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("PayPal top-up successful — quota: %v, payment amount: %.2f USD", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentProviderPayPal)

	return nil
}

// ExpireStaleTossPendingTopUps marks Toss pending orders created before cutoffUnix as expired.
// Toss's payment window is ~30 minutes; checkout sessions the user closes (PAY_PROCESS_CANCELED)
// send no webhook, so without this sweep their pending orders would linger forever.
//
// Only orders that never recorded a paymentKey are swept. At creation provider_order_id is set
// to the orderId (== trade_no); any confirm attempt that reached RecordTossPaymentKey overwrites
// it with the Toss paymentKey (≠ trade_no). So provider_order_id = trade_no uniquely identifies
// orders that were never approved at Toss — the only ones safe to expire. Orders that DID record
// a paymentKey are reconciled separately using provider_order_time, because Toss's 10-minute
// approval deadline starts when the paymentKey is issued, not when the local order was created.
func ExpireStaleTossPendingTopUps(cutoffUnix int64) (int64, error) {
	res := DB.Model(&TopUp{}).
		Where("payment_provider = ? AND status = ? AND create_time < ? AND provider_order_id = trade_no",
			PaymentProviderToss, common.TopUpStatusPending, cutoffUnix).
		Update("status", common.TopUpStatusExpired)
	return res.RowsAffected, res.Error
}

func ReconcileStaleTossRecordedTopUps(ctx context.Context, cutoffUnix int64, limit int) (int64, error) {
	if tossTopUpReconciler == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []TopUp
	if err := DB.Where("payment_provider = ? AND status = ? AND provider_order_id <> '' AND provider_order_id <> trade_no AND ((provider_order_time > 0 AND provider_order_time < ?) OR ((provider_order_time = 0 OR provider_order_time IS NULL) AND create_time < ?))",
		PaymentProviderToss, common.TopUpStatusPending, cutoffUnix, cutoffUnix).
		Order("create_time asc, id asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var resolved int64
	var lastErr error
	for i := range rows {
		ok, err := tossTopUpReconciler(ctx, rows[i])
		if err != nil {
			lastErr = err
			common.SysError(fmt.Sprintf("failed to reconcile stale Toss top-up order %s: %v", rows[i].TradeNo, err))
			continue
		}
		if ok {
			resolved++
		}
	}
	return resolved, lastErr
}

// RecordTossPaymentKey persists the Toss paymentKey onto the order's provider_order_id.
// This MUST succeed before the payment is approved: the stale-pending sweep treats
// provider_order_id == trade_no as "never approved" and expires such orders, so an
// approved-but-unrecorded order could otherwise be wrongly expired with no credit.
// Returns an error if the value was not persisted (row missing or update lost).
func RecordTossPaymentKey(tradeNo string, paymentKey string) error {
	if tradeNo == "" || paymentKey == "" {
		return errors.New("toss paymentKey persist: missing tradeNo or paymentKey")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	now := common.GetTimestamp()
	if err := DB.Model(&TopUp{}).
		Where(refCol+" = ? AND payment_provider = ?", tradeNo, PaymentProviderToss).
		Updates(map[string]interface{}{
			"provider_order_id":   paymentKey,
			"provider_order_time": now,
		}).Error; err != nil {
		return err
	}
	// RowsAffected is unreliable across DBs for unchanged-value updates (MySQL returns 0),
	// so verify the persisted value directly.
	var topUp TopUp
	if err := DB.Select("provider_order_id", "provider_order_time").Where(refCol+" = ?", tradeNo).First(&topUp).Error; err != nil {
		return err
	}
	if topUp.ProviderOrderId != paymentKey {
		return errors.New("toss paymentKey persist: value not stored")
	}
	if topUp.ProviderOrderTime <= 0 {
		return errors.New("toss paymentKey persist: timestamp not stored")
	}
	return nil
}

// RechargeToss credits a successful Toss top-up idempotently.
// The caller must validate the Toss confirm response (status DONE, amount match)
// before calling this, and must hold the order lock.
// paymentKey, if non-empty, is stored as ProviderOrderId for audit traceability.
func RechargeToss(tradeNo string, paymentKey string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // idempotent: already credited
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		if paymentKey != "" {
			topUp.ProviderOrderId = paymentKey
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		quotaToAdd = CreditedQuotaForTossTopUp(topUp)
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}
		return CreditTopUpTarget(tx, topUp, quotaToAdd)
	})

	if err != nil {
		common.SysError("toss topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Toss 충전 성공 — 적립: %v, 결제 금액: %d원", logger.FormatQuota(quotaToAdd), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentProviderToss)
	}

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // idempotent: return immediately if already succeeded
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo top-up successful, quota: %v, payment amount: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		quotaToAdd = int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake top-up successful, quota: %v, payment amount: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}

// generateTossCustomerKey returns a random Toss-compatible customerKey.
// Toss requires customerKey to be 2–50 chars from [A-Za-z0-9_\-=.@].
// "cust_" + 32 random alphanumeric chars = 37 chars, well within limits.
func generateTossCustomerKey() string {
	return "cust_" + randstr.String(32)
}

// GetOrCreateTossCustomerKey returns the user's stable Toss customerKey,
// generating and persisting a random one on first use (race-safe via conditional update).
func GetOrCreateTossCustomerKey(userId int) (string, error) {
	var user User
	if err := DB.Select("id", "toss_customer_key").Where("id = ?", userId).First(&user).Error; err != nil {
		return "", err
	}
	if user.TossCustomerKey != "" {
		return user.TossCustomerKey, nil
	}
	key := generateTossCustomerKey()
	res := DB.Model(&User{}).
		Where("id = ? AND (toss_customer_key = '' OR toss_customer_key IS NULL)", userId).
		Update("toss_customer_key", key)
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		// Another concurrent request set it first — re-read the winner.
		if err := DB.Select("toss_customer_key").Where("id = ?", userId).First(&user).Error; err != nil {
			return "", err
		}
		return user.TossCustomerKey, nil
	}
	return key, nil
}
