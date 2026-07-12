package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopUp struct {
	Id                int     `json:"id"`
	UserId            int     `json:"user_id" gorm:"index"`
	TargetType        string  `json:"target_type" gorm:"type:varchar(32);default:'user'"`
	TargetId          int     `json:"target_id" gorm:"default:0;index"`
	Amount            int64   `json:"amount"`
	Money             float64 `json:"money"`
	Quota             int     `json:"quota" gorm:"default:0"`
	TradeNo           string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	ProviderOrderId   string  `json:"provider_order_id" gorm:"type:varchar(255);index"`
	ProviderOrderTime int64   `json:"provider_order_time" gorm:"default:0;index"`
	// ProviderRetryTime is the next time a background provider reconciliation
	// may reserve this order. It is separate from ProviderOrderTime so retry
	// fairness never destroys the paymentKey issuance timestamp used to enforce
	// Toss's approval window.
	ProviderRetryTime int64 `json:"-" gorm:"default:0;index"`
	// Keep this field untyped in GORM on purpose. The MySQL dialector maps an
	// unconstrained, unindexed string to LONGTEXT, while PostgreSQL and SQLite
	// map it to TEXT. Toss responses can be up to the HTTP response limit and
	// must not be truncated by MySQL's 64 KiB TEXT type.
	ProviderPayload       string `json:"-"`
	ProviderCredential    string `json:"-" gorm:"type:text"`
	ProviderClientKeyHash string `json:"-" gorm:"type:varchar(64);default:'';index:idx_topup_toss_credential_source,priority:2"`
	// ProviderIdempotencyKey pins the Toss confirmation namespace to the exact
	// paymentKey supplied by the provider. A browser callback is unauthenticated;
	// using only trade_no would let a forged paymentKey consume the real order's
	// 15-day idempotency result. Legacy attempted rows leave this empty and keep
	// using trade_no so their already-sent request remains safely repeatable.
	ProviderIdempotencyKey string `json:"-" gorm:"type:varchar(300);default:''"`
	// ProviderConfirmProtocolVersion is selected when the checkout row is
	// created, before any browser can return a paymentKey. Version 0 is
	// permanently legacy/orderId-scoped; version 1 is paymentKey-scoped. This
	// durable creation-time marker is required for rolling upgrades because an
	// old binary can POST /confirm without knowing ProviderAttempted, leaving an
	// ambiguous row that otherwise looks identical to an unattempted new row.
	ProviderConfirmProtocolVersion int `json:"-" gorm:"default:0"`
	// ProviderAttempted is a durable hand-off barrier between the local final
	// authorization gate and the provider request. Membership/account lifecycle
	// changes must not commit while a personal charge can still be sent.
	ProviderAttempted bool `json:"-" gorm:"default:false;index"`
	// ProviderClaim* is a short cross-process lease used when recovery needs to
	// repeat an idempotent provider request for this exact order.
	ProviderClaimToken string `json:"-" gorm:"type:varchar(64);default:''"`
	ProviderClaimTime  int64  `json:"-" gorm:"default:0;index"`
	PaymentMethod      string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider    string `json:"payment_provider" gorm:"type:varchar(50);default:'';index:idx_topup_toss_credential_source,priority:1"`
	CreateTime         int64  `json:"create_time" gorm:"index:idx_topup_toss_credential_source,priority:3"`
	CompleteTime       int64  `json:"complete_time"`
	Status             string `json:"status"`
	// WalletAutoRecharge* is the immutable logical-attempt identity for server-
	// initiated Toss wallet charges. It decouples recovery from the externally
	// visible orderId and lets a later rollout move to opaque order IDs without
	// allowing two new-version workers to create the same charge twice.
	WalletAutoRechargeId            *int    `json:"-" gorm:"uniqueIndex:idx_topup_toss_wallet_attempt,priority:1"`
	WalletAutoRechargeCycleKey      *string `json:"-" gorm:"type:varchar(48);uniqueIndex:idx_topup_toss_wallet_attempt,priority:2"`
	WalletAutoRechargeAttempt       *int    `json:"-" gorm:"uniqueIndex:idx_topup_toss_wallet_attempt,priority:3"`
	WalletOrderIdVersion            int     `json:"-" gorm:"default:0;index"`
	WalletAutoRechargeCreationToken string  `json:"-" gorm:"type:varchar(64);default:''"`
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

// TossTopUpStatusRefundPending is intentionally unknown to older binaries.
// During a rolling deployment they must not treat a refund-owned order as a
// recoverable failed/expired checkout and grant quota while a new worker is
// canceling the provider payment.
const TossTopUpStatusRefundPending = "refund_pending"

var (
	ErrPaymentMethodMismatch                 = errors.New("payment method mismatch")
	ErrTopUpNotFound                         = errors.New("topup not found")
	ErrTopUpStatusInvalid                    = errors.New("topup status invalid")
	ErrTopUpQuotaCapacityExceeded            = errors.New("top-up quota capacity exceeded")
	ErrTossPaymentKeyConflict                = errors.New("toss payment key conflict")
	ErrTossTopUpConfirmClaimLost             = errors.New("toss top-up confirm claim lost")
	ErrTossTopUpLifecycleInactive            = errors.New("toss top-up charge lifecycle is inactive")
	ErrTossTopUpCredentialConflict           = errors.New("toss top-up confirm credential conflicts with the durable attempt")
	ErrTossTopUpProtocolInvalid              = errors.New("toss top-up confirm protocol state is invalid")
	ErrTossTopUpBindingResetLegacy           = errors.New("legacy Toss top-up payment binding cannot be reset")
	ErrTossTopUpBindingResetUnsafe           = errors.New("toss top-up payment binding cannot be safely reset")
	ErrTossTopUpSettlementTargetMissing      = errors.New("toss top-up settlement target is missing")
	ErrTossRefundRequiredPrecedesFulfillment = errors.New("a durable Toss refund requirement prevents local fulfillment")
	errTossSettlementClaimLost               = errors.New("toss settlement claim lost")
)

const (
	// A confirmation request owns at most a 65-second provider context followed
	// by an 8-second authoritative lookup. Two minutes leaves scheduling/DB
	// margin for a live owner while allowing a crashed pre-POST owner to be
	// recovered well inside Toss's ten-minute confirmation window. Reclaiming a
	// slow live owner is still financially safe because every worker reuses the
	// exact persisted credential and idempotency key.
	tossTopUpConfirmClaimTTLSeconds     int64 = 2 * 60
	tossTopUpReconcileRetryDelaySeconds int64 = 2 * 60
	// A recorded paymentKey can be confirmed for roughly ten minutes. Every
	// still-approvable checkout belongs to the first lane immediately, ordered by
	// its immutable provider deadline (EDF). Waiting for a smaller "urgent"
	// margin would discard recovery capacity when a large poison backlog is
	// already rotating. Expired/refund work stays in a separate retry-time lane.
	tossTopUpApprovalWindowSeconds int64 = 10 * 60
	// TossTopUpRecoveryWorkerCount is shared with the scheduler batch size so a
	// row is never reserved longer than it can wait for an HTTP worker.
	TossTopUpRecoveryWorkerCount = 16
	// TossTopUpRecoverySQLiteWorkerCount keeps network calls concurrent while
	// SQLite serializes their short durable state transitions.
	TossTopUpRecoverySQLiteWorkerCount = 4
	tossGeneralTopUpTradeNoLikePattern = "toss!_%"
	tossTopUpRefundRecoveryExistsSQL   = `EXISTS (
		SELECT 1 FROM toss_payment_events
		WHERE toss_payment_events.order_id = top_ups.trade_no
		  AND toss_payment_events.payment_key = top_ups.provider_order_id
		  AND toss_payment_events.event_type = ?
		  AND toss_payment_events.reconciliation_status = ?
	)`
	// TossPaymentKeyScopedIdempotencyEnv is a legacy rolling-upgrade escape
	// hatch. New installs default to the paymentKey-scoped protocol because an
	// orderId-only idempotency namespace lets an untrusted callback bind a
	// different paymentKey first and consume the real callback's cached result.
	// Operators upgrading a mixed old/new fleet may explicitly set false while
	// checkouts are paused; after every node has the dual reader and v0 pending
	// rows are drained, remove the override (or set true) before resuming.
	TossPaymentKeyScopedIdempotencyEnv = "TOSS_PAYMENT_KEY_SCOPED_IDEMPOTENCY_ENABLED"
	tossTopUpConfirmProtocolLegacy     = 0
	tossTopUpConfirmProtocolPaymentKey = 1
)

func IsTossPaymentKeyScopedIdempotencyEnabled() bool {
	raw := strings.TrimSpace(os.Getenv(TossPaymentKeyScopedIdempotencyEnv))
	if raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	// A malformed deployment value must not silently downgrade newly-created
	// payment attempts into the vulnerable legacy namespace. Explicit false is
	// still honored for the documented, checkout-paused rolling migration.
	return err != nil || enabled
}

// IsPristinePaymentKeyScopedTossTopUp identifies a v1 callback binding for
// which the durable invariant proves that no provider POST was authorized.
// Callers may use this to choose the no-provider-proof reset path, but the
// mutation must still go through ResetClaimedPendingTossTopUpPaymentBinding,
// which repeats every predicate under the owner/order locks.
func IsPristinePaymentKeyScopedTossTopUp(topUp *TopUp) bool {
	return topUp != nil &&
		topUp.PaymentProvider == PaymentProviderToss &&
		topUp.PaymentMethod == PaymentMethodToss &&
		topUp.Status == common.TopUpStatusPending &&
		topUp.ProviderConfirmProtocolVersion == tossTopUpConfirmProtocolPaymentKey &&
		!topUp.ProviderAttempted &&
		strings.TrimSpace(topUp.ProviderIdempotencyKey) == "" &&
		strings.TrimSpace(topUp.ProviderPayload) == ""
}

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

// CreatePendingTossTopUpCheckout binds personal/organization eligibility and
// the immutable pending order in one owner-locked transaction. A membership
// change cannot slip between the controller preflight and checkout creation.
func CreatePendingTossTopUpCheckout(topUp *TopUp) error {
	if topUp == nil || topUp.UserId <= 0 || topUp.TradeNo == "" ||
		topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss ||
		topUp.Status != common.TopUpStatusPending {
		return errors.New("invalid pending Toss top-up checkout")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := validateTossTopUpChargeLifecycle(tx, topUp, true); err != nil {
			return err
		}
		// The environment gate chooses the protocol only for a brand-new row.
		// Never reinterpret an existing v0 checkout after the fleet setting
		// changes: an old node may already have sent its orderId-keyed POST while
		// leaving every newly-added attempt column at the zero value.
		topUp.ProviderConfirmProtocolVersion = tossTopUpConfirmProtocolLegacy
		if IsTossPaymentKeyScopedIdempotencyEnabled() {
			topUp.ProviderConfirmProtocolVersion = tossTopUpConfirmProtocolPaymentKey
		}
		return tx.Create(topUp).Error
	})
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
	if int64(quota) > math.MaxInt32 {
		return fmt.Errorf("%w: top-up quota exceeds database capacity", ErrTopUpQuotaCapacityExceeded)
	}
	maxCurrentQuota := int64(math.MaxInt32) - int64(quota)
	switch topUp.EffectiveTargetType() {
	case TopUpTargetTypeOrganization:
		targetId := topUp.EffectiveTargetId()
		result := tx.Model(&Organization{}).
			// A legacy or concurrently over-consumed balance can be negative. A
			// top-up must still be able to reduce that debt; only the upper bound is
			// relevant when preventing an INT overflow.
			Where("id = ? AND quota <= ?", targetId, maxCurrentQuota).
			Update("quota", gorm.Expr("quota + ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&Organization{}).Where("id = ?", targetId).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return errors.New("organization not found")
			}
			return fmt.Errorf("%w: organization quota capacity exceeded", ErrTopUpQuotaCapacityExceeded)
		}
		return nil
	default:
		result := tx.Model(&User{}).
			Where("id = ? AND quota <= ?", topUp.UserId, maxCurrentQuota).
			Update("quota", gorm.Expr("quota + ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return errors.New("user not found")
			}
			return fmt.Errorf("%w: user quota capacity exceeded", ErrTopUpQuotaCapacityExceeded)
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
	if topUp == nil {
		return 0
	}
	if topUp.Quota != 0 {
		// A non-zero Quota is proof that the immutable snapshot column was
		// populated. Never reinterpret a corrupt snapshot through legacy Money or
		// today's amount conversion, and enforce the portable SQL INT ceiling even
		// on SQLite where a manually damaged row can hold a wider integer.
		if topUp.Quota < 1 || int64(topUp.Quota) > math.MaxInt32 {
			return 0
		}
		return topUp.Quota
	}
	// Legacy Toss rows written before Quota was introduced still carry the
	// immutable checkout quote in Money. Any non-zero Money means that snapshot
	// exists; corrupt or unrepresentable snapshots must fail closed instead of
	// silently recalculating rights with today's TossUnitPrice.
	if topUp.Money != 0 {
		if topUp.Money <= 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) ||
			common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			return 0
		}
		quota, ok := tossTopUpPositiveInt(decimal.NewFromFloat(topUp.Money).
			Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
		if !ok {
			return 0
		}
		return quota
	}
	if topUp.Amount > 0 {
		if quota := TossCreditQuotaFromKRW(topUp.Amount); quota > 0 {
			return quota
		}
	}
	return 0
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
	topUp, err := GetTopUpByTradeNoWithError(tradeNo)
	if err != nil {
		return nil
	}
	return topUp
}

// GetTopUpByTradeNoWithError preserves database failures so callers can
// distinguish a missing order from a temporarily unavailable database.
func GetTopUpByTradeNoWithError(tradeNo string) (*TopUp, error) {
	return GetTopUpByTradeNoWithErrorContext(context.Background(), tradeNo)
}

// GetTopUpByTradeNoWithErrorContext is the deadline-aware form used by
// provider webhooks. Keep the legacy wrapper for non-request workers.
func GetTopUpByTradeNoWithErrorContext(ctx context.Context, tradeNo string) (*TopUp, error) {
	if tradeNo == "" {
		return nil, ErrTopUpNotFound
	}
	var topUp TopUp
	if err := dbWithContext(ctx).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTopUpNotFound
		}
		return nil, err
	}
	return &topUp, nil
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	return UpdatePendingTopUpStatusForMethod(tradeNo, expectedPaymentProvider, "", targetStatus)
}

// UpdatePendingTopUpStatusForMethod additionally guards the integration-level
// payment method. This matters when multiple gateways can share a provider
// label but must never authorize each other's callbacks or cleanup workers.
func UpdatePendingTopUpStatusForMethod(tradeNo, expectedPaymentProvider, expectedPaymentMethod, targetStatus string) error {
	return UpdatePendingTopUpStatusForMethodContext(context.Background(), tradeNo, expectedPaymentProvider, expectedPaymentMethod, targetStatus)
}

func UpdatePendingTopUpStatusForMethodContext(ctx context.Context, tradeNo, expectedPaymentProvider, expectedPaymentMethod, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	query := dbWithContext(ctx).Model(&TopUp{}).
		Where("trade_no = ? AND status = ?", tradeNo, common.TopUpStatusPending)
	if expectedPaymentProvider != "" {
		query = query.Where("payment_provider = ?", expectedPaymentProvider)
	}
	if expectedPaymentMethod != "" {
		query = query.Where("payment_method = ?", expectedPaymentMethod)
	}
	result := query.Update("status", targetStatus)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// A concurrent callback may have performed the same transition. Re-read
	// after the failed CAS and treat that final state as an idempotent success.
	topUp, err := GetTopUpByTradeNoWithErrorContext(ctx, tradeNo)
	if err != nil {
		return err
	}
	if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
		return ErrPaymentMethodMismatch
	}
	if expectedPaymentMethod != "" && topUp.PaymentMethod != expectedPaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topUp.Status == targetStatus {
		return nil
	}
	return ErrTopUpStatusInvalid
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
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", referenceId).First(topUp).Error
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
		var reference TopUp
		if err := tx.Select("payment_provider", "payment_method", "status").Where(refCol+" = ?", tradeNo).First(&reference).Error; err != nil {
			return errors.New("top-up order not found")
		}
		if reference.Status == common.TopUpStatusSuccess {
			return nil
		}
		if reference.PaymentProvider == PaymentProviderToss && reference.PaymentMethod == PaymentMethodToss {
			// A generic admin action has no authenticated Toss Payment object and
			// cannot prove paymentKey/order/amount/status or cancellation precedence.
			// Toss orders must be completed only through provider reconciliation.
			return errors.New("Toss top-up cannot be completed manually; use payment reconciliation")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
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
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", referenceId).First(topUp).Error
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
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
	// Legacy wallet auto-recharge workers persisted provider_order_id=trade_no
	// before their billing POST but had no durable attempted flag. A lost
	// response is therefore indistinguishable from an unposted row here. Keep
	// every wallet_auto_* order for its dedicated GET-before-terminal recovery.
	walletAutoRechargePattern := "wallet!_auto!_%"
	res := DB.Model(&TopUp{}).
		Where("payment_provider = ? AND payment_method = ? AND status = ? AND create_time < ? AND provider_order_id = trade_no AND provider_attempted = ?",
			PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, cutoffUnix, false).
		Where("wallet_order_id_version = ?", 0).
		Where("wallet_auto_recharge_id IS NULL AND wallet_auto_recharge_cycle_key IS NULL AND wallet_auto_recharge_attempt IS NULL").
		Where("trade_no NOT LIKE ? ESCAPE '!'", walletAutoRechargePattern).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossWalletOpaqueOrderIDLikePattern()).
		Updates(map[string]interface{}{
			"status":                   common.TopUpStatusExpired,
			"complete_time":            GetDBTimestamp(),
			"provider_credential":      "",
			"provider_client_key_hash": "",
		})
	return res.RowsAffected, res.Error
}

func ReconcileStaleTossRecordedTopUps(ctx context.Context, cutoffUnix int64, limit int) (int64, error) {
	if tossTopUpReconciler == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	maximumBatch := TossTopUpRecoveryWorkerCount
	if common.UsingSQLite {
		maximumBatch = TossTopUpRecoverySQLiteWorkerCount
	}
	if limit <= 0 || limit > maximumBatch {
		limit = maximumBatch
	}
	now := GetDBTimestamp()
	claimCutoff := now - tossTopUpConfirmClaimTTLSeconds
	hasRefundEventTable := DB.Migrator().HasTable(&TossPaymentEvent{})
	var rows []TopUp
	rowsQuery := DB.Where("payment_provider = ? AND payment_method = ? AND trade_no LIKE ? ESCAPE '!' AND provider_order_id <> '' AND provider_order_id <> trade_no AND ((provider_order_time > 0 AND provider_order_time < ?) OR ((provider_order_time = 0 OR provider_order_time IS NULL) AND create_time < ?))",
		PaymentProviderToss, PaymentMethodToss, tossGeneralTopUpTradeNoLikePattern, cutoffUnix, cutoffUnix)
	if hasRefundEventTable {
		rowsQuery = rowsQuery.Where("status = ? OR (status IN ? AND "+tossTopUpRefundRecoveryExistsSQL+")",
			common.TopUpStatusPending, []string{TossTopUpStatusRefundPending, common.TopUpStatusFailed, common.TopUpStatusExpired},
			TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired)
	} else {
		rowsQuery = rowsQuery.Where("status = ?", common.TopUpStatusPending)
	}
	if err := rowsQuery.
		Where("(provider_retry_time = 0 OR provider_retry_time IS NULL OR provider_retry_time <= ?)", now).
		Where("(provider_claim_token = '' OR provider_claim_token IS NULL OR provider_claim_time <= ?)", claimCutoff).
		// Preserve the hard confirmation deadline without allowing old poison
		// rows to monopolize every retry cycle. Every still-approvable pending
		// checkout enters the first lane and is ordered by provider time (earliest
		// deadline first). Refund-owned, expired, and other terminal rows remain in
		// the background lane where retry time is the rotating fairness cursor,
		// including a virtual-account refund deferred for operator action.
		Clauses(clause.OrderBy{Expression: clause.Expr{
			SQL: "CASE WHEN status = ? AND " +
				"CASE WHEN provider_order_time > 0 THEN provider_order_time WHEN create_time > 0 THEN create_time ELSE 0 END > ? " +
				"THEN 0 ELSE 1 END ASC, " +
				"CASE WHEN status = ? AND " +
				"CASE WHEN provider_order_time > 0 THEN provider_order_time WHEN create_time > 0 THEN create_time ELSE 0 END > ? " +
				"THEN CASE WHEN provider_order_time > 0 THEN provider_order_time WHEN create_time > 0 THEN create_time ELSE 0 END ELSE NULL END ASC, " +
				"COALESCE(provider_retry_time, 0) ASC, " +
				"CASE WHEN provider_order_time > 0 THEN provider_order_time WHEN create_time > 0 THEN create_time ELSE 0 END ASC, id ASC",
			Vars: []interface{}{
				common.TopUpStatusPending,
				now - tossTopUpApprovalWindowSeconds,
				common.TopUpStatusPending,
				now - tossTopUpApprovalWindowSeconds,
			},
			WithoutParentheses: true,
		}}).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == limit {
		// Do not include order/payment identifiers. Saturation is an operational
		// signal to inspect callback crashes and database/provider latency before
		// the fixed Toss confirmation window is exhausted.
		common.SysLog(fmt.Sprintf("Toss recorded top-up recovery batch saturated: limit=%d", limit))
	}
	var resolved int64
	var lastErr error
	reservedIDs := make([]int, 0, len(rows))
	reservedRows := make([]TopUp, 0, len(rows))
	for i := range rows {
		// Reserve each due row before the provider call. Unresolved or permanently
		// broken rows are moved behind other due work, so an oldest LIMIT-sized
		// batch cannot starve newer paid orders forever. The exact observed retry
		// value makes the reservation portable and safe across multiple masters.
		nextRetry := now + tossTopUpReconcileRetryDelaySeconds
		reserveQuery := DB.Model(&TopUp{}).
			Where("id = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND provider_order_id = ?",
				rows[i].Id, PaymentProviderToss, PaymentMethodToss, rows[i].Status, rows[i].ProviderOrderId).
			Where("(provider_retry_time = ? OR (provider_retry_time IS NULL AND ? = 0))", rows[i].ProviderRetryTime, rows[i].ProviderRetryTime).
			Where("(provider_claim_token = '' OR provider_claim_token IS NULL OR provider_claim_time <= ?)", claimCutoff)
		if rows[i].Status != common.TopUpStatusPending {
			reserveQuery = reserveQuery.Where(tossTopUpRefundRecoveryExistsSQL, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired)
		}
		reserve := reserveQuery.Update("provider_retry_time", nextRetry)
		if reserve.Error != nil {
			lastErr = reserve.Error
			common.SysError(fmt.Sprintf("failed to reserve stale Toss top-up order %s: %v", rows[i].TradeNo, reserve.Error))
			continue
		}
		if reserve.RowsAffected != 1 {
			continue
		}
		reservedIDs = append(reservedIDs, rows[i].Id)
		rows[i].ProviderRetryTime = nextRetry
		reservedRows = append(reservedRows, rows[i])
	}
	type reconciliationResult struct {
		ok      bool
		err     error
		tradeNo string
	}
	outcomes := make(chan reconciliationResult, len(reservedRows))
	runBoundedTossMaintenanceWithWorkers(
		reservedRows,
		TossTopUpRecoveryWorkerCount,
		TossTopUpRecoverySQLiteWorkerCount,
		func(row TopUp) {
			ok, err := tossTopUpReconciler(ctx, row)
			outcomes <- reconciliationResult{ok: ok, err: err, tradeNo: row.TradeNo}
		},
	)
	close(outcomes)
	for outcome := range outcomes {
		if outcome.err != nil {
			lastErr = outcome.err
			common.SysError(fmt.Sprintf("failed to reconcile stale Toss top-up order %s: %v", outcome.tradeNo, outcome.err))
			continue
		}
		if outcome.ok {
			resolved++
		}
	}
	if len(reservedIDs) > 0 {
		// A batch can spend several seconds per provider lookup. Push any retry
		// timestamp that elapsed while processing the batch forward as one final
		// portable update, otherwise the same LIMIT-sized batch could immediately
		// monopolize the next run. New callback/claim activity has a future retry
		// time or fresh lease and is deliberately left untouched.
		finishedAt := GetDBTimestamp()
		deferQuery := DB.Model(&TopUp{}).
			Where("id IN ? AND payment_provider = ? AND payment_method = ? AND status IN ? AND provider_retry_time <= ?",
				reservedIDs, PaymentProviderToss, PaymentMethodToss,
				[]string{common.TopUpStatusPending, TossTopUpStatusRefundPending, common.TopUpStatusFailed, common.TopUpStatusExpired}, finishedAt)
		if hasRefundEventTable {
			deferQuery = deferQuery.Where("status = ? OR "+tossTopUpRefundRecoveryExistsSQL,
				common.TopUpStatusPending, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired)
		} else {
			deferQuery = deferQuery.Where("status = ?", common.TopUpStatusPending)
		}
		deferResult := deferQuery.
			Where("(provider_claim_token = '' OR provider_claim_token IS NULL OR provider_claim_time <= ?)", finishedAt-tossTopUpConfirmClaimTTLSeconds).
			Update("provider_retry_time", finishedAt+tossTopUpReconcileRetryDelaySeconds)
		if deferResult.Error != nil {
			lastErr = deferResult.Error
			common.SysError(fmt.Sprintf("failed to defer unresolved Toss top-up reconciliation batch: %v", deferResult.Error))
		}
	}
	return resolved, lastErr
}

func DeferPendingTossTopUpProviderRetryWithContext(ctx context.Context, orderID, paymentKey string, retryAt int64) error {
	orderID = strings.TrimSpace(orderID)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderID == "" || paymentKey == "" || retryAt <= GetDBTimestamp() {
		return errors.New("invalid Toss top-up retry deferral")
	}
	result := dbWithContext(ctx).Model(&TopUp{}).
		Where("trade_no = ? AND provider_order_id = ? AND payment_provider = ? AND payment_method = ?",
			orderID, paymentKey, PaymentProviderToss, PaymentMethodToss).
		Where("status = ? OR (status IN ? AND "+tossTopUpRefundRecoveryExistsSQL+")",
			common.TopUpStatusPending, []string{TossTopUpStatusRefundPending, common.TopUpStatusFailed, common.TopUpStatusExpired},
			TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
		Update("provider_retry_time", retryAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTopUpStatusInvalid
	}
	return nil
}

// RecordTossPaymentKey persists the Toss paymentKey onto the order's provider_order_id.
// This MUST succeed before the payment is approved: the stale-pending sweep treats
// provider_order_id == trade_no as "never approved" and expires such orders, so an
// approved-but-unrecorded order could otherwise be wrongly expired with no credit.
// Returns an error if the value was not persisted (row missing or update lost).
func RecordTossPaymentKey(tradeNo string, paymentKey string) error {
	return RecordTossPaymentKeyWithContext(context.Background(), tradeNo, paymentKey)
}

func RecordTossPaymentKeyWithContext(ctx context.Context, tradeNo string, paymentKey string) error {
	if tradeNo == "" || paymentKey == "" {
		return errors.New("toss paymentKey persist: missing tradeNo or paymentKey")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	now := GetDBTimestampWithContext(ctx)
	result := dbWithContext(ctx).Model(&TopUp{}).
		Where(refCol+" = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND (provider_order_id = '' OR provider_order_id IS NULL OR provider_order_id = "+refCol+" OR provider_order_id = ?)",
			tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, paymentKey).
		Updates(map[string]interface{}{
			"provider_order_id": paymentKey,
			// Keep the first paymentKey timestamp immutable. Browser callback
			// redelivery must not extend the provider approval window forever.
			// Do not inspect provider_order_id here: MySQL evaluates single-table
			// UPDATE assignments from left to right, so that expression could see
			// the newly assigned paymentKey and leave the first timestamp at zero.
			"provider_order_time": gorm.Expr("CASE WHEN provider_order_time IS NULL OR provider_order_time <= 0 THEN ? ELSE provider_order_time END", now),
			// A callback is active now. Give its cross-node claim enough time to
			// finish (or expire after a crash) before background recovery runs.
			"provider_retry_time": now + tossTopUpConfirmClaimTTLSeconds,
		})
	if result.Error != nil {
		return result.Error
	}
	// RowsAffected is unreliable for an unchanged-value update on MySQL, so
	// classify the persisted row after every CAS attempt.
	topUp, err := GetTopUpByTradeNoWithErrorContext(ctx, tradeNo)
	if err != nil {
		return err
	}
	if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
		return ErrPaymentMethodMismatch
	}
	if topUp.ProviderOrderId == paymentKey {
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if topUp.ProviderOrderTime <= 0 {
			return errors.New("toss paymentKey persist: timestamp not stored")
		}
		return nil
	}
	if topUp.ProviderOrderId != "" && topUp.ProviderOrderId != topUp.TradeNo {
		return ErrTossPaymentKeyConflict
	}
	if topUp.Status != common.TopUpStatusPending {
		return ErrTopUpStatusInvalid
	}
	return errors.New("toss paymentKey persist: value not stored")
}

// lockTossTopUpSettlementRootsTx establishes the common lifecycle lock order
// for an existing Toss top-up without treating a disabled target as unpaid:
// payer -> organization (when applicable) -> order. The initial order read is
// deliberately non-locking; immutable root fields are checked again after the
// order lock so a corrupted/concurrent rewrite cannot make us lock one target
// and credit another.
//
// Charge authorization performs stricter active-owner validation separately.
// Settlement and authoritative recovery must only require the original target
// rows to still exist, because money that reached Toss before a later disable
// still has to be credited exactly once.
func lockTossTopUpSettlementRootsTx(tx *gorm.DB, tradeNo string) (TopUp, error) {
	var reference TopUp
	if err := tx.Select("user_id", "target_type", "target_id").
		Where("trade_no = ?", tradeNo).First(&reference).Error; err != nil {
		return TopUp{}, err
	}

	var owner User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", reference.UserId).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TopUp{}, fmt.Errorf("%w: user_id=%d", ErrTossTopUpSettlementTargetMissing, reference.UserId)
		}
		return TopUp{}, err
	}
	if reference.EffectiveTargetType() == TopUpTargetTypeOrganization {
		var organization Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ?", reference.EffectiveTargetId()).First(&organization).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return TopUp{}, fmt.Errorf("%w: organization_id=%d", ErrTossTopUpSettlementTargetMissing, reference.EffectiveTargetId())
			}
			return TopUp{}, err
		}
	}

	var topUp TopUp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return TopUp{}, err
	}
	if topUp.UserId != reference.UserId ||
		topUp.EffectiveTargetType() != reference.EffectiveTargetType() ||
		topUp.EffectiveTargetId() != reference.EffectiveTargetId() {
		return TopUp{}, errors.New("Toss top-up settlement target changed concurrently")
	}
	return topUp, nil
}

func recoverSettledTossTopUpPaymentKeyWithContext(ctx context.Context, topUp *TopUp, paymentKey string) error {
	if topUp == nil || topUp.Status != common.TopUpStatusSuccess {
		return ErrTopUpStatusInvalid
	}
	if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
		return ErrPaymentMethodMismatch
	}
	if topUp.ProviderOrderId != "" && topUp.ProviderOrderId != topUp.TradeNo && topUp.ProviderOrderId != paymentKey {
		return ErrTossPaymentKeyConflict
	}
	now := GetDBTimestampWithContext(ctx)
	updates := map[string]interface{}{
		"provider_order_id":    paymentKey,
		"provider_retry_time":  now,
		"provider_claim_token": "",
		"provider_claim_time":  0,
	}
	if topUp.ProviderOrderTime <= 0 {
		updates["provider_order_time"] = now
	}
	result := dbWithContext(ctx).Model(&TopUp{}).
		Where("id = ? AND payment_provider = ? AND payment_method = ? AND status = ?",
			topUp.Id, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusSuccess).
		Where("provider_order_id = '' OR provider_order_id IS NULL OR provider_order_id = trade_no OR provider_order_id = ?", paymentKey).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	persisted, err := GetTopUpByTradeNoWithErrorContext(ctx, topUp.TradeNo)
	if err != nil {
		return err
	}
	if persisted.Status != common.TopUpStatusSuccess {
		return ErrTopUpStatusInvalid
	}
	if persisted.ProviderOrderId != paymentKey {
		if persisted.ProviderOrderId != "" && persisted.ProviderOrderId != persisted.TradeNo {
			return ErrTossPaymentKeyConflict
		}
		return errors.New("Toss transaction recovery paymentKey was not stored")
	}
	return nil
}

// RecoverTossTopUpPaymentKeyWithContext makes an authoritative Transaction API
// approval durable before quota settlement. Unlike the browser confirmation
// path, reconciliation can discover a valid DONE payment after a local order
// was already expired or failed because both the callback and webhook were
// unavailable. The conditional update may reopen only a Toss top-up whose
// provider identity is still unset or already matches this paymentKey.
//
// RechargeTossWithContext remains the only quota-settlement path and performs
// the cancellation-precedence check under the same order lock. If this process
// stops after this function returns, the persisted paymentKey leaves the order
// recoverable by the ordinary pending-payment sweeper.
func RecoverTossTopUpPaymentKeyWithContext(ctx context.Context, tradeNo, paymentKey string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	if tradeNo == "" || paymentKey == "" {
		return errors.New("Toss transaction recovery requires tradeNo and paymentKey")
	}

	allowedStatuses := []string{
		common.TopUpStatusPending,
		common.TopUpStatusFailed,
		common.TopUpStatusExpired,
		common.TopUpStatusSuccess,
	}
	err := runTossSettlementTransaction(dbWithContext(ctx), func(tx *gorm.DB) error {
		topUp, err := lockTossTopUpSettlementRootsTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		statusAllowed := false
		for _, status := range allowedStatuses {
			if topUp.Status == status {
				statusAllowed = true
				break
			}
		}
		if !statusAllowed {
			return ErrTopUpStatusInvalid
		}
		if topUp.ProviderOrderId != "" && topUp.ProviderOrderId != topUp.TradeNo && topUp.ProviderOrderId != paymentKey {
			return ErrTossPaymentKeyConflict
		}

		now := getDBTimestampTx(tx)
		updates := map[string]interface{}{
			"provider_order_id":    paymentKey,
			"provider_retry_time":  now,
			"provider_claim_token": "",
			"provider_claim_time":  0,
		}
		if topUp.ProviderOrderTime <= 0 {
			updates["provider_order_time"] = now
		}
		if topUp.Status != common.TopUpStatusSuccess {
			updates["status"] = common.TopUpStatusPending
			updates["complete_time"] = 0
		}
		result := tx.Model(&TopUp{}).
			Where("id = ? AND payment_provider = ? AND payment_method = ? AND status = ?",
				topUp.Id, PaymentProviderToss, PaymentMethodToss, topUp.Status).
			Where("provider_order_id = '' OR provider_order_id IS NULL OR provider_order_id = trade_no OR provider_order_id = ?", paymentKey).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		// The row is locked and the predicates were classified above. A zero
		// RowsAffected on MySQL can mean every assigned value was unchanged.
		if result.RowsAffected == 0 {
			var persisted TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&persisted).Error; err != nil {
				return err
			}
			if persisted.ProviderOrderId != paymentKey ||
				(persisted.Status != common.TopUpStatusPending && persisted.Status != common.TopUpStatusSuccess) {
				return errors.New("Toss transaction recovery paymentKey was not stored")
			}
		}
		return nil
	})
	if err == nil {
		return nil
	}
	// A success row is already financially terminal. If its payer or target was
	// deleted later, metadata reconciliation must still be able to attach the
	// authoritative paymentKey and finish idempotently without reopening or
	// crediting anything. Non-success rows must never use this target-free path.
	current, lookupErr := GetTopUpByTradeNoWithErrorContext(ctx, tradeNo)
	if lookupErr == nil && current.Status == common.TopUpStatusSuccess {
		return recoverSettledTossTopUpPaymentKeyWithContext(ctx, current, paymentKey)
	}
	return err
}

// ClaimPendingTossTopUpConfirmRetry is the cross-process authorization gate for
// repeating a one-time Toss confirmation after its result could not be
// established. The exact order, paymentKey and amount are part of the CAS so a
// stale worker cannot approve a different payment after local state changed.
//
// A crashed owner can be reclaimed after the short lease. Provider-side
// duplication is independently prevented by reusing the creation-time
// protocol's persisted credential/idempotency pair: legacy orders retain the
// orderId key, while scoped orders retain their exact paymentKey-derived key.
func ClaimPendingTossTopUpConfirmRetry(tradeNo, paymentKey string, amount int64) (token string, topUp *TopUp, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	if tradeNo == "" || paymentKey == "" || amount <= 0 {
		return "", nil, false, errors.New("invalid Toss top-up confirm retry")
	}

	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&TopUp{}).
		Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ?",
			tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey).
		Where("(provider_claim_token = '' OR provider_claim_token IS NULL OR provider_claim_time <= ?)", now-tossTopUpConfirmClaimTTLSeconds).
		Updates(map[string]interface{}{
			"provider_claim_token": token,
			"provider_claim_time":  now,
			"provider_retry_time":  now + tossTopUpConfirmClaimTTLSeconds,
		})
	if result.Error != nil {
		return "", nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		var claimedTopUp TopUp
		if err := DB.Where("trade_no = ? AND provider_claim_token = ?", tradeNo, token).First(&claimedTopUp).Error; err != nil {
			_ = ReleasePendingTossTopUpConfirmRetry(tradeNo, token)
			return "", nil, false, err
		}
		return token, &claimedTopUp, true, nil
	}

	current, lookupErr := GetTopUpByTradeNoWithError(tradeNo)
	if lookupErr != nil {
		return "", nil, false, lookupErr
	}
	if current.PaymentProvider != PaymentProviderToss || current.PaymentMethod != PaymentMethodToss {
		return "", current, false, ErrPaymentMethodMismatch
	}
	if current.Amount != amount {
		return "", current, false, errors.New("Toss top-up confirm retry amount mismatch")
	}
	if strings.TrimSpace(current.ProviderOrderId) != paymentKey {
		return "", current, false, ErrTossPaymentKeyConflict
	}
	if current.Status != common.TopUpStatusPending {
		return "", current, false, nil
	}
	// Another process owns a fresh lease. It will either settle the order or
	// release the claim; this caller must not send another remote request.
	return "", current, false, nil
}

// ClosePendingTossTopUpForReconcile closes an exact pending Toss order only
// when the reconciliation snapshot is still current and no other process owns
// a fresh provider claim. claimToken may authorize the caller's own live claim.
// A false/nil result means newer callback or claim activity won the race and the
// order must remain pending for that worker.
func ClosePendingTossTopUpForReconcile(tradeNo, paymentKey, targetStatus, claimToken string, observedOrderTime, observedRetryTime int64) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	claimToken = strings.TrimSpace(claimToken)
	if tradeNo == "" || paymentKey == "" || targetStatus == "" || targetStatus == common.TopUpStatusPending {
		return false, errors.New("invalid Toss top-up reconciliation close")
	}

	now := GetDBTimestamp()
	query := DB.Model(&TopUp{}).
		Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND provider_order_id = ?",
			tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, paymentKey)
	if observedOrderTime > 0 {
		query = query.Where("provider_order_time = ?", observedOrderTime)
	} else {
		query = query.Where("(provider_order_time = 0 OR provider_order_time IS NULL)")
	}
	if observedRetryTime > 0 {
		query = query.Where("provider_retry_time = ?", observedRetryTime)
	} else {
		query = query.Where("(provider_retry_time = 0 OR provider_retry_time IS NULL)")
	}
	claimPredicate := "(provider_claim_token = '' OR provider_claim_token IS NULL OR provider_claim_time <= ?)"
	claimArgs := []interface{}{now - tossTopUpConfirmClaimTTLSeconds}
	if claimToken != "" {
		claimPredicate = "(" + claimPredicate + " OR provider_claim_token = ?)"
		claimArgs = append(claimArgs, claimToken)
	}
	result := query.Where(claimPredicate, claimArgs...).Updates(map[string]interface{}{
		"status":               targetStatus,
		"provider_claim_token": "",
		"provider_claim_time":  0,
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}

	current, err := GetTopUpByTradeNoWithError(tradeNo)
	if err != nil {
		return false, err
	}
	if current.PaymentProvider != PaymentProviderToss || current.PaymentMethod != PaymentMethodToss {
		return false, ErrPaymentMethodMismatch
	}
	if strings.TrimSpace(current.ProviderOrderId) != paymentKey {
		return false, ErrTossPaymentKeyConflict
	}
	if current.Status != common.TopUpStatusPending {
		return true, nil
	}
	// Any changed scheduling timestamp or fresh foreign claim is positive
	// evidence that a newer worker is responsible for the order.
	if current.ProviderOrderTime != observedOrderTime || current.ProviderRetryTime != observedRetryTime {
		return false, nil
	}
	if current.ProviderClaimToken != "" && current.ProviderClaimToken != claimToken && current.ProviderClaimTime > now-tossTopUpConfirmClaimTTLSeconds {
		return false, nil
	}
	return false, errors.New("Toss top-up reconciliation close was not persisted")
}

func ReleasePendingTossTopUpConfirmRetry(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return nil
	}
	return DB.Model(&TopUp{}).
		Where("trade_no = ? AND provider_claim_token = ?", tradeNo, token).
		Updates(map[string]interface{}{
			"provider_claim_token": "",
			"provider_claim_time":  0,
		}).Error
}

// ValidatePendingTossTopUpConfirmClaim rechecks the local authorization just
// before the remote POST. This catches status/payment metadata changes that
// happened after the lease was acquired.
func ValidatePendingTossTopUpConfirmClaim(tradeNo, paymentKey string, amount int64, token string) (*TopUp, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	token = strings.TrimSpace(token)
	if tradeNo == "" || paymentKey == "" || amount <= 0 || token == "" {
		return nil, ErrTossTopUpConfirmClaimLost
	}
	var topUp TopUp
	if err := DB.Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ? AND provider_claim_token = ?",
		tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey, token).
		First(&topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTossTopUpConfirmClaimLost
		}
		return nil, err
	}
	if err := validateTossTopUpChargeLifecycle(DB, &topUp, false); err != nil {
		return nil, err
	}
	return &topUp, nil
}

func validateTossTopUpChargeLifecycle(db *gorm.DB, topUp *TopUp, lock bool) error {
	if db == nil || topUp == nil || topUp.UserId <= 0 {
		return ErrTossTopUpLifecycleInactive
	}
	userQuery := db.Select("id", "status", "organization_id", "organization_role")
	if lock {
		userQuery = userQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var user User
	if err := userQuery.Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: user no longer exists", ErrTossTopUpLifecycleInactive)
		}
		return err
	}
	if user.Status != common.UserStatusEnabled {
		return fmt.Errorf("%w: user is disabled", ErrTossTopUpLifecycleInactive)
	}

	switch topUp.EffectiveTargetType() {
	case TopUpTargetTypeOrganization:
		organizationID := topUp.EffectiveTargetId()
		if organizationID <= 0 {
			return fmt.Errorf("%w: organization target is missing", ErrTossTopUpLifecycleInactive)
		}
		organizationQuery := db.Select("id", "owner_user_id", "status")
		if lock {
			organizationQuery = organizationQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var organization Organization
		if err := organizationQuery.Where("id = ?", organizationID).First(&organization).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: organization no longer exists", ErrTossTopUpLifecycleInactive)
			}
			return err
		}
		if organization.Status != OrganizationStatusEnabled {
			return fmt.Errorf("%w: organization is disabled", ErrTossTopUpLifecycleInactive)
		}
		if organization.OwnerUserId != topUp.UserId || user.OrganizationId != organizationID || !HasOrganizationOwnerRole(user.OrganizationRole) {
			return fmt.Errorf("%w: organization ownership changed", ErrTossTopUpLifecycleInactive)
		}
	default:
		if topUp.EffectiveTargetId() != topUp.UserId {
			return fmt.Errorf("%w: user target changed", ErrTossTopUpLifecycleInactive)
		}
		if user.OrganizationId > 0 {
			return fmt.Errorf("%w: personal wallet is inactive during organization membership", ErrTossTopUpLifecycleInactive)
		}
	}
	return nil
}

func tossPaymentKeyScopedConfirmIdempotencyKey(tradeNo, paymentKey string) string {
	seed := strings.TrimSpace(tradeNo) + "\x00" + strings.TrimSpace(paymentKey)
	return "toss_confirm_" + common.Sha1([]byte(seed))
}

// PrepareClaimedPendingTossTopUpConfirm snapshots the exact order-time secret
// and returns the durable Idempotency-Key immediately before a confirmation
// POST. It must never replace either value after an attempt: Toss includes the
// API key in its idempotency namespace, so crash recovery can safely repeat
// only the same credential/idempotency pair.
//
// Newly-versioned attempts bind the provider namespace to both orderId and
// paymentKey. This prevents an unauthenticated forged callback from consuming
// the real order's 15-day idempotency result with an unrelated paymentKey.
// Legacy attempted rows intentionally keep the old orderId key. The rollout
// gate must only be enabled after all nodes understand the persisted field.
func PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey string, amount int64, token, secretKey string) (string, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	token = strings.TrimSpace(token)
	secretKey = strings.TrimSpace(secretKey)
	if tradeNo == "" || paymentKey == "" || amount <= 0 || token == "" || secretKey == "" {
		return "", ErrTossTopUpConfirmClaimLost
	}
	credential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return "", err
	}
	cancellationBlocked := false
	idempotencyKey := ""
	err = DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTTopUp)
		if err != nil {
			return err
		}
		var reference TopUp
		if err := tx.Select("user_id", "target_type", "target_id").
			Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ? AND provider_claim_token = ?",
				tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey, token).
			First(&reference).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTossTopUpConfirmClaimLost
			}
			return err
		}
		var owner User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ?", reference.UserId).First(&owner).Error; err != nil {
			return err
		}
		if reference.EffectiveTargetType() == TopUpTargetTypeOrganization {
			var organization Organization
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id").Where("id = ?", reference.EffectiveTargetId()).First(&organization).Error; err != nil {
				return err
			}
		}
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ? AND provider_claim_token = ?",
			tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey, token).
			First(&topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTossTopUpConfirmClaimLost
			}
			return err
		}
		// Recheck the charge owner and target under row locks immediately before
		// authorizing a provider POST. Read-only lookup and settlement do not use
		// this gate, so an already-DONE payment can still be fulfilled safely.
		if err := validateTossTopUpChargeLifecycle(tx, &topUp, false); err != nil {
			return err
		}
		canceled, err := hasAuthoritativeTossCancellationForOrderTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if canceled {
			result := tx.Model(&TopUp{}).
				Where("id = ? AND status = ? AND provider_claim_token = ?", topUp.Id, common.TopUpStatusPending, token).
				Updates(map[string]interface{}{
					"status":               common.TopUpStatusFailed,
					"complete_time":        getDBTimestampTx(tx),
					"provider_attempted":   false,
					"provider_claim_token": "",
					"provider_claim_time":  0,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTossTopUpConfirmClaimLost
			}
			cancellationBlocked = true
			return nil
		}
		refundRequired, err := hasRequiredTossTopUpRefundForOrderTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if refundRequired {
			return ErrTossRefundRequiredPrecedesFulfillment
		}
		if !topUp.ProviderAttempted && postBarrier.PolicyError != nil {
			return postBarrier.PolicyError
		}
		// The secret key is part of Toss's idempotency namespace. A checkout
		// normally snapshots it before the browser is redirected, and the first
		// provider-attempt transaction fills it only for legacy rows where that
		// snapshot is absent. Once present, compare the plaintext and preserve the
		// exact ciphertext; silently replacing it with a rotated/caller-supplied
		// credential could turn a retry of an ambiguous POST into a new charge.
		storedCredential := strings.TrimSpace(topUp.ProviderCredential)
		preserveCredential := false
		if storedCredential != "" {
			storedSecret, decryptErr := DecryptProviderCredential(storedCredential)
			if decryptErr != nil || strings.TrimSpace(storedSecret) == "" || storedSecret != secretKey {
				return ErrTossTopUpCredentialConflict
			}
			preserveCredential = true
		} else if topUp.ProviderAttempted {
			// An attempted row without its exact API key cannot safely reconstruct
			// the old idempotency namespace from the current configuration.
			return ErrTossTopUpCredentialConflict
		}
		expectedScopedKey := tossPaymentKeyScopedConfirmIdempotencyKey(tradeNo, paymentKey)
		storedKey := strings.TrimSpace(topUp.ProviderIdempotencyKey)
		switch {
		case storedKey != "":
			// A non-empty value is a version marker as well as the exact retry
			// key. Even a node whose rollout environment is temporarily stale
			// must honor it rather than falling back to orderId.
			if storedKey != expectedScopedKey {
				return ErrTossPaymentKeyConflict
			}
			idempotencyKey = storedKey
		case topUp.ProviderConfirmProtocolVersion == tossTopUpConfirmProtocolLegacy:
			// v0 is a permanent creation-time decision. In particular, a pre-upgrade
			// worker may have sent orderId while all newly-added attempt fields still
			// read as zero after AutoMigrate.
			idempotencyKey = tradeNo
		case topUp.ProviderConfirmProtocolVersion == tossTopUpConfirmProtocolPaymentKey && !topUp.ProviderAttempted:
			idempotencyKey = expectedScopedKey
		case topUp.ProviderConfirmProtocolVersion == tossTopUpConfirmProtocolPaymentKey:
			// v1 persists its derived key atomically with ProviderAttempted. Seeing
			// attempted=true without that key is an inconsistent/partially-written
			// state; falling back to orderId could create another provider namespace.
			return ErrTossTopUpProtocolInvalid
		default:
			return ErrTossTopUpProtocolInvalid
		}
		updates := map[string]interface{}{
			"provider_attempted":  true,
			"provider_claim_time": getDBTimestampTx(tx),
		}
		if !preserveCredential {
			updates["provider_credential"] = credential
		}
		if storedKey == "" && idempotencyKey != tradeNo {
			updates["provider_idempotency_key"] = idempotencyKey
		}
		result := tx.Model(&TopUp{}).
			Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ? AND provider_claim_token = ?",
				tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey, token).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossTopUpConfirmClaimLost
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cancellationBlocked {
		return "", ErrTossCancellationPrecedesFulfillment
	}
	if idempotencyKey == "" {
		return "", errors.New("Toss top-up confirm idempotency key was not prepared")
	}
	return idempotencyKey, nil
}

// UpdateClaimedPendingTossTopUpConfirmCredential keeps the original error-only
// API for callers/tests that do not send the provider request themselves.
func UpdateClaimedPendingTossTopUpConfirmCredential(tradeNo, paymentKey string, amount int64, token, secretKey string) error {
	_, err := PrepareClaimedPendingTossTopUpConfirm(tradeNo, paymentKey, amount, token, secretKey)
	return err
}

// ResetClaimedPendingTossTopUpPaymentBinding restores a pending checkout after
// Toss has definitively proved that the callback paymentKey cannot move money
// for this local order (for example, a terminal confirm rejection followed by
// an authenticated 404, or a lookup that belongs to another order).
//
// This operation is deliberately unavailable to legacy orderId-key attempts:
// their provider idempotency namespace may already have been consumed by the
// forged request. A v1 row is safe either before its first provider-attempt
// marker (no POST was authorized) or after its exact scoped key was persisted.
// The live claim and order row lock prevent a stale verifier from erasing a
// concurrent settlement.
func ResetClaimedPendingTossTopUpPaymentBinding(tradeNo, paymentKey string, amount int64, token string) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	token = strings.TrimSpace(token)
	if tradeNo == "" || paymentKey == "" || amount <= 0 || token == "" {
		return false, ErrTossTopUpConfirmClaimLost
	}
	expectedIdempotencyKey := tossPaymentKeyScopedConfirmIdempotencyKey(tradeNo, paymentKey)
	reset := false
	err := runTossSettlementTransaction(DB, func(tx *gorm.DB) error {
		topUp, err := lockTossTopUpSettlementRootsTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending || topUp.Amount != amount ||
			strings.TrimSpace(topUp.ProviderOrderId) != paymentKey ||
			strings.TrimSpace(topUp.ProviderClaimToken) != token {
			return ErrTossTopUpConfirmClaimLost
		}
		if topUp.ProviderConfirmProtocolVersion != tossTopUpConfirmProtocolPaymentKey {
			return ErrTossTopUpBindingResetLegacy
		}
		storedIdempotencyKey := strings.TrimSpace(topUp.ProviderIdempotencyKey)
		scopedAttempted := topUp.ProviderAttempted && storedIdempotencyKey == expectedIdempotencyKey
		scopedPristine := !topUp.ProviderAttempted && storedIdempotencyKey == ""
		if !scopedAttempted && !scopedPristine {
			return ErrTossTopUpBindingResetUnsafe
		}
		if strings.TrimSpace(topUp.ProviderPayload) != "" {
			return ErrTossTopUpBindingResetUnsafe
		}
		if tx.Migrator().HasTable(&TossPaymentEvent{}) {
			var eventCount int64
			if err := tx.Model(&TossPaymentEvent{}).
				Where("order_id = ?", tradeNo).
				Limit(1).Count(&eventCount).Error; err != nil {
				return err
			}
			if eventCount > 0 {
				return ErrTossTopUpBindingResetUnsafe
			}
		}
		query := tx.Model(&TopUp{}).
			Where("id = ? AND payment_provider = ? AND payment_method = ? AND status = ? AND amount = ? AND provider_order_id = ? AND provider_claim_token = ? AND provider_confirm_protocol_version = ?",
				topUp.Id, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending, amount, paymentKey, token, tossTopUpConfirmProtocolPaymentKey)
		if scopedAttempted {
			query = query.Where("provider_attempted = ? AND provider_idempotency_key = ?", true, expectedIdempotencyKey)
		} else {
			query = query.Where("provider_attempted = ? AND (provider_idempotency_key = '' OR provider_idempotency_key IS NULL)", false)
		}
		result := query.Updates(map[string]interface{}{
			"provider_order_id":        tradeNo,
			"provider_order_time":      0,
			"provider_retry_time":      0,
			"provider_idempotency_key": "",
			"provider_attempted":       false,
			"provider_claim_token":     "",
			"provider_claim_time":      0,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossTopUpConfirmClaimLost
		}
		reset = true
		return nil
	})
	return reset, err
}

// RechargeToss credits a successful Toss top-up idempotently.
// The caller must validate the Toss confirm response (status DONE, amount match)
// before calling this. A pending-to-success CAS is the settlement claim, so only
// its winner may credit the wallet across processes.
// paymentKey, if non-empty, is stored as ProviderOrderId for audit traceability.
func RechargeToss(tradeNo string, paymentKey string, callerIp string) (err error) {
	return rechargeToss(context.Background(), tradeNo, paymentKey, callerIp, false)
}

func RechargeTossWithContext(ctx context.Context, tradeNo string, paymentKey string, callerIp string) (err error) {
	return rechargeToss(ctx, tradeNo, paymentKey, callerIp, true)
}

func rechargeToss(ctx context.Context, tradeNo string, paymentKey string, callerIp string, requestScoped bool) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}
	settlementWon := false
	cancellationBlocked := false

	err = runTossSettlementTransaction(dbWithContext(ctx), func(tx *gorm.DB) error {
		quotaToAdd = 0
		settlementWon = false
		cancellationBlocked = false
		lockedTopUp, err := lockTossTopUpSettlementRootsTx(tx, tradeNo)
		if err != nil {
			return err
		}
		*topUp = lockedTopUp
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if paymentKey != "" && topUp.ProviderOrderId != "" && topUp.ProviderOrderId != topUp.TradeNo && topUp.ProviderOrderId != paymentKey {
			return ErrTossPaymentKeyConflict
		}
		refundRequired, err := hasRequiredTossTopUpRefundForOrderTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if refundRequired {
			return ErrTossRefundRequiredPrecedesFulfillment
		}
		if topUp.Status != common.TopUpStatusPending {
			return errTossSettlementClaimLost
		}
		canceled, err := hasAuthoritativeTossCancellationForOrderTx(tx, tradeNo)
		if err != nil {
			return err
		}
		if canceled {
			closed := tx.Model(&TopUp{}).Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
				Updates(map[string]interface{}{
					"status":               common.TopUpStatusFailed,
					"complete_time":        getDBTimestampTx(tx),
					"provider_attempted":   false,
					"provider_claim_token": "",
					"provider_claim_time":  0,
				})
			if closed.Error != nil {
				return closed.Error
			}
			if closed.RowsAffected != 1 {
				return errTossSettlementClaimLost
			}
			cancellationBlocked = true
			return nil
		}
		updates := map[string]interface{}{
			"status":               common.TopUpStatusSuccess,
			"complete_time":        common.GetTimestamp(),
			"provider_attempted":   false,
			"provider_claim_token": "",
			"provider_claim_time":  0,
		}
		if paymentKey != "" {
			updates["provider_order_id"] = paymentKey
		}
		claim := tx.Model(&TopUp{}).
			Where("id = ? AND payment_provider = ? AND payment_method = ? AND status = ?",
				topUp.Id, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending)
		if paymentKey != "" {
			// Never let a settlement overwrite a different paymentKey recorded by
			// another callback. The tradeNo value is the pre-confirm marker used by
			// webhook-first recovery and is the only non-key value allowed here.
			claim = claim.Where("provider_order_id = ? OR provider_order_id = trade_no OR provider_order_id = '' OR provider_order_id IS NULL", paymentKey)
		}
		result := claim.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errTossSettlementClaimLost
		}

		topUp.Status = common.TopUpStatusSuccess
		topUp.ProviderAttempted = false
		quotaToAdd = CreditedQuotaForTossTopUp(topUp)
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}
		if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
			return err
		}
		settlementWon = true
		return nil
	})
	if cancellationBlocked && err == nil {
		return ErrTossCancellationPrecedesFulfillment
	}
	if err != nil {
		// Financially terminal orders are idempotent even after their user or
		// organization row is removed. The target-free success check cannot add
		// quota; it only lets a late duplicate/reconciliation resolve cleanly.
		current, lookupErr := GetTopUpByTradeNoWithErrorContext(ctx, tradeNo)
		if lookupErr == nil && current.PaymentProvider == PaymentProviderToss && current.PaymentMethod == PaymentMethodToss &&
			current.Status == common.TopUpStatusSuccess {
			if paymentKey != "" && current.ProviderOrderId != "" && current.ProviderOrderId != current.TradeNo && current.ProviderOrderId != paymentKey {
				return ErrTossPaymentKeyConflict
			}
			return nil
		}
	}

	if errors.Is(err, errTossSettlementClaimLost) {
		current, lookupErr := GetTopUpByTradeNoWithErrorContext(ctx, tradeNo)
		switch {
		case lookupErr != nil:
			err = lookupErr
		case current.PaymentProvider != PaymentProviderToss || current.PaymentMethod != PaymentMethodToss:
			err = ErrPaymentMethodMismatch
		case current.Status == common.TopUpStatusSuccess:
			return nil
		case paymentKey != "" && current.ProviderOrderId != "" && current.ProviderOrderId != current.TradeNo && current.ProviderOrderId != paymentKey:
			err = ErrTossPaymentKeyConflict
		default:
			err = ErrTopUpStatusInvalid
		}
	}
	if errors.Is(err, ErrTopUpQuotaCapacityExceeded) || errors.Is(err, ErrTossRefundRequiredPrecedesFulfillment) ||
		errors.Is(err, ErrTossTopUpSettlementTargetMissing) {
		// Controllers need these financially meaningful outcomes to install or
		// honor the durable automatic-refund fence, and reconciliation must be
		// able to distinguish an immutable missing target from a transient DB error.
		// They are never returned as API response text; other internal/database
		// errors retain the generic user-facing wrapper below.
		return err
	}
	if err != nil {
		common.SysError("toss topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	if settlementWon {
		content := fmt.Sprintf("Toss 충전 성공 — 적립: %v, 결제 금액: %d원", logger.FormatQuota(quotaToAdd), topUp.Amount)
		if requestScoped {
			RecordTopupLogWithContext(ctx, topUp.UserId, content, callerIp, topUp.PaymentMethod, PaymentProviderToss)
		} else {
			RecordTopupLog(topUp.UserId, content, callerIp, topUp.PaymentMethod, PaymentProviderToss)
		}
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
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error
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
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error
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
// Toss Core/Billing APIs accept 2–300 characters from [A-Za-z0-9_\-=.@], while
// the browser SDK payment() boundary is 50 characters. Toss recommends an
// unpredictable value; "cust_" + 32 random alphanumeric characters is 37, so
// it fits both provider surfaces and this project's varchar(64) storage.
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
