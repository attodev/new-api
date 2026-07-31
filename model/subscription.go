package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/samber/hot"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Subscription duration units
const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"

	// Keep arbitrary plan input well inside time.Duration and application
	// scheduling limits. These match the established organization-plan caps.
	SubscriptionPlanMaxDurationValue       = 1200
	SubscriptionPlanMaxCustomSeconds int64 = 31_536_000
)

// Subscription quota reset period
const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
)

var (
	ErrSubscriptionOrderNotFound      = errors.New("subscription order not found")
	ErrSubscriptionOrderStatusInvalid = errors.New("subscription order status invalid")
	ErrSubscriptionPurchaseLimit      = errors.New("purchase limit for this plan has been reached")
	errTossSubscriptionCASRetry       = errors.New("toss subscription CAS retry")
)

// This is an application-owned checkout-reservation lifetime, not a Toss
// authKey expiry guarantee. Toss documents billing authKey as one-time and at
// most 300 characters, but does not publish the ten-minute approval window used
// by normal payments for billing auth. A reservation past this conservative
// local bound can be replaced only when it has no durable provider activity;
// orders with an issue/charge marker remain reserved until reconciliation
// closes them.
const TossSubscriptionPurchaseReservationMaxAgeSeconds int64 = 50 * 60

type TossSubscriptionOrderReconciler func(ctx context.Context, order SubscriptionOrder) (resolved bool, err error)

var tossSubscriptionOrderReconciler TossSubscriptionOrderReconciler

func SetTossSubscriptionOrderReconciler(fn TossSubscriptionOrderReconciler) {
	tossSubscriptionOrderReconciler = fn
}

const (
	subscriptionPlanCacheNamespace     = "new-api:subscription_plan:v1"
	subscriptionPlanInfoCacheNamespace = "new-api:subscription_plan_info:v1"
)

var (
	subscriptionPlanCacheOnce     sync.Once
	subscriptionPlanInfoCacheOnce sync.Once

	subscriptionPlanCache     *cachex.HybridCache[SubscriptionPlan]
	subscriptionPlanInfoCache *cachex.HybridCache[SubscriptionPlanInfo]
)

func subscriptionPlanCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanInfoCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_TTL", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_CAP", 5000)
	if capacity <= 0 {
		capacity = 5000
	}
	return capacity
}

func subscriptionPlanInfoCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}

func getSubscriptionPlanCache() *cachex.HybridCache[SubscriptionPlan] {
	subscriptionPlanCacheOnce.Do(func() {
		ttl := subscriptionPlanCacheTTL()
		subscriptionPlanCache = cachex.NewHybridCache[SubscriptionPlan](cachex.HybridCacheConfig[SubscriptionPlan]{
			Namespace: cachex.Namespace(subscriptionPlanCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlan]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlan] {
				return hot.NewHotCache[string, SubscriptionPlan](hot.LRU, subscriptionPlanCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanCache
}

func getSubscriptionPlanInfoCache() *cachex.HybridCache[SubscriptionPlanInfo] {
	subscriptionPlanInfoCacheOnce.Do(func() {
		ttl := subscriptionPlanInfoCacheTTL()
		subscriptionPlanInfoCache = cachex.NewHybridCache[SubscriptionPlanInfo](cachex.HybridCacheConfig[SubscriptionPlanInfo]{
			Namespace: cachex.Namespace(subscriptionPlanInfoCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlanInfo]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlanInfo] {
				return hot.NewHotCache[string, SubscriptionPlanInfo](hot.LRU, subscriptionPlanInfoCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanInfoCache
}

func subscriptionPlanCacheKey(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func InvalidateSubscriptionPlanCache(planId int) {
	if planId <= 0 {
		return
	}
	cache := getSubscriptionPlanCache()
	_, _ = cache.DeleteMany([]string{subscriptionPlanCacheKey(planId)})
	infoCache := getSubscriptionPlanInfoCache()
	_ = infoCache.Purge()
}

// Subscription plan
type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	StripePriceId         string `json:"stripe_price_id" gorm:"type:varchar(128);default:''"`
	CreemProductId        string `json:"creem_product_id" gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id" gorm:"type:varchar(128);default:''"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// RunSubscriptionPlanMutationWithTossBarrier serializes every commercial-plan
// mutation behind the same option row used by Toss maintenance. A crash leaves
// the durable gate in place, so legacy renewal rows can never be frozen from a
// mixture of pre/post-edit plan terms across batches or retrying nodes.
func RunSubscriptionPlanMutationWithTossBarrier(mutate func(*gorm.DB) error) error {
	if DB == nil || mutate == nil {
		return errors.New("database or subscription plan mutation is unavailable")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossOptionRowsTx(tx); err != nil {
			return err
		}
		maintenance, err := tossConfigMaintenanceRequiredTx(tx)
		if err != nil {
			return err
		}
		if maintenance {
			return ErrTossConfigMaintenanceRequired
		}
		if tx.Migrator().HasTable(&UserSubscription{}) {
			pendingRenewal, err := hasPendingLegacyTossRenewalContractsDB(tx)
			if err != nil {
				return err
			}
			if pendingRenewal {
				return ErrTossConfigMaintenanceRequired
			}
		}
		return mutate(tx)
	})
}

func (p *SubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *SubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

// Subscription order (payment -> webhook -> create UserSubscription)
type SubscriptionOrder struct {
	Id     int     `json:"id"`
	UserId int     `json:"user_id" gorm:"index"`
	PlanId int     `json:"plan_id" gorm:"index"`
	Money  float64 `json:"money"`

	TradeNo         string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50);default:'';index:idx_subscription_order_toss_credential_source,priority:1"`
	Status          string `json:"status"`
	CreateTime      int64  `json:"create_time" gorm:"index:idx_subscription_order_toss_credential_source,priority:3"`
	CompleteTime    int64  `json:"complete_time"`

	// See TopUp.ProviderPayload. Omitting an explicit type is intentional so
	// MySQL uses LONGTEXT and PostgreSQL/SQLite continue to use TEXT.
	ProviderPayload       string `json:"provider_payload"`
	PlanSnapshot          string `json:"-" gorm:"type:text"`
	ProviderAmount        int64  `json:"provider_amount" gorm:"default:0"`
	ProviderCurrency      string `json:"provider_currency" gorm:"type:varchar(8);default:''"`
	ProviderCredential    string `json:"-" gorm:"type:text"`
	ProviderClientKeyHash string `json:"-" gorm:"type:varchar(64);default:'';index:idx_subscription_order_toss_credential_source,priority:2"`
	BillingKeyId          int    `json:"billing_key_id" gorm:"default:0;index"`
	BillingClaimToken     string `json:"-" gorm:"type:varchar(64);default:''"`
	BillingClaimTime      int64  `json:"-" gorm:"default:0;index"`
	// BillingIssue* is the durable, issue-only snapshot used to recover a lost
	// /billing/authorizations/issue response. authKey is one-time and Toss does
	// not expose an API that can retrieve an already-issued billing key, so the
	// encrypted authKey and its exact customer namespace must survive until the
	// idempotent issue request is definitively resolved.
	BillingIssueAuthKey     string `json:"-" gorm:"type:text"`
	BillingIssueAuthKeyHash string `json:"-" gorm:"type:varchar(64);default:''"`
	BillingIssueCustomerKey string `json:"-" gorm:"type:varchar(64);default:''"`
	BillingIssueAttempted   bool   `json:"-" gorm:"default:false"`
	// BillingAttempted and BillingAttemptCredential are the durable marker for
	// a billing provider POST, including both the initial subscription charge
	// and renewals. Recovery workers must GET the order with this exact
	// credential before they are allowed to repeat that idempotent POST.
	BillingAttempted         bool   `json:"-" gorm:"default:false"`
	BillingAttemptCredential string `json:"-" gorm:"type:text"`
	// BillingChargeProtocolVersion distinguishes pre-marker rolling-upgrade
	// rows from orders created by code that durably gates every provider POST.
	// v0 may already have charged even when BillingAttempted reads false after
	// AutoMigrate; v1 may trust false as a pristine no-POST state.
	BillingChargeProtocolVersion int `json:"-" gorm:"default:0"`
	// BillingAttemptTime is the immutable timestamp of the first provider POST
	// intent. Unlike BillingClaimTime it is never refreshed by GET/recovery
	// leases, so entitlement-expiry deferral cannot be extended indefinitely.
	BillingAttemptTime int64 `json:"-" gorm:"default:0"`
	// RenewalEndTime freezes the subscription period boundary before the
	// provider POST. It lets recovery distinguish an already-paid renewal from
	// a later subscription state without relaxing authorization for a new
	// charge after cancellation or account disablement.
	RenewalEndTime int64 `json:"-" gorm:"default:0"`
	// Renewal* identifies one logical automatic-charge attempt independently of
	// the provider-facing orderId. Nullable columns keep initial purchases and
	// pre-migration rows outside the composite unique index on SQLite, MySQL and
	// PostgreSQL; attempt zero is represented by a non-nil pointer to zero.
	RenewalSubscriptionId *int   `json:"-" gorm:"uniqueIndex:idx_subscription_order_toss_renewal_attempt,priority:1"`
	RenewalBillingTime    *int64 `json:"-" gorm:"uniqueIndex:idx_subscription_order_toss_renewal_attempt,priority:2"`
	RenewalAttempt        *int   `json:"-" gorm:"uniqueIndex:idx_subscription_order_toss_renewal_attempt,priority:3"`
	RenewalOrderIdVersion int    `json:"-" gorm:"default:0;index"`
	RenewalCreationToken  string `json:"-" gorm:"type:varchar(64);default:''"`
}

func (o *SubscriptionOrder) Insert() error {
	if o.CreateTime == 0 {
		o.CreateTime = common.GetTimestamp()
	}
	return DB.Create(o).Error
}

// CreateTossSubscriptionOrderWithPurchaseReservation serializes checkout
// creation per user and treats pending Toss purchase orders as reservations
// against MaxPurchasePerUser. This prevents multiple cards from being charged
// before the first paid callback can create its UserSubscription.
func CreateTossSubscriptionOrderWithPurchaseReservation(order *SubscriptionOrder, plan *SubscriptionPlan) error {
	if order == nil || plan == nil || order.UserId <= 0 || order.PlanId <= 0 || order.TradeNo == "" {
		return errors.New("invalid Toss subscription purchase reservation")
	}
	_, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(order)
	if identityErr != nil || isRenewalOrder || order.PlanId != plan.Id || order.PaymentProvider != PaymentProviderToss || order.PaymentMethod != PaymentMethodToss ||
		order.Status != common.TopUpStatusPending || strings.HasPrefix(order.TradeNo, TossRenewalTradeNoPrefix) ||
		HasTossRenewalOpaqueOrderIDPrefix(order.TradeNo) {
		return errors.New("invalid Toss subscription purchase order")
	}
	if err := ValidateTossSubscriptionBillingPlan(plan); err != nil {
		return err
	}
	order.BillingChargeProtocolVersion = tossBillingChargeProtocolDurableAttempt
	return DB.Transaction(func(tx *gorm.DB) error {
		var lockedUser User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "organization_id").Where("id = ?", order.UserId).First(&lockedUser).Error; err != nil {
			return err
		}
		if lockedUser.Status != common.UserStatusEnabled || lockedUser.OrganizationId > 0 {
			return ErrTossBillingUserInactive
		}
		now := getDBTimestampTx(tx)
		if order.CreateTime == 0 {
			order.CreateTime = now
		}
		blockingFinancialState, err := hasBlockingTossInitialPaymentStateTx(tx, order.UserId, plan.Id)
		if err != nil {
			return err
		}
		if blockingFinancialState {
			// This barrier is independent of MaxPurchasePerUser. A plan that permits
			// repeated purchases still must not open a replacement checkout while an
			// earlier provider attempt or unresolved partial/mismatched payment can
			// represent money without a settled local entitlement.
			return ErrPersonalTossBillingInFlight
		}
		if plan.MaxPurchasePerUser > 0 {
			// Release only stale reservations that provably never reached a
			// provider POST. Every durable issue/charge marker must still be
			// pristine; an inconsistent partial marker is safer to reconcile
			// manually than to close as if no provider activity occurred.
			staleCutoff := now - TossSubscriptionPurchaseReservationMaxAgeSeconds
			if err := tx.Model(&SubscriptionOrder{}).
				Where("user_id = ? AND plan_id = ? AND payment_provider = ? AND status = ? AND create_time < ?", order.UserId, plan.Id, PaymentProviderToss, common.TopUpStatusPending, staleCutoff).
				Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
				Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
				Where("renewal_order_id_version = ?", 0).
				Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL").
				Where("(billing_key_id = 0 OR billing_key_id IS NULL)").
				Where("(billing_claim_token = '' OR billing_claim_token IS NULL) AND (billing_claim_time = 0 OR billing_claim_time IS NULL)").
				Where("(billing_issue_auth_key = '' OR billing_issue_auth_key IS NULL) AND (billing_issue_auth_key_hash = '' OR billing_issue_auth_key_hash IS NULL) AND (billing_issue_customer_key = '' OR billing_issue_customer_key IS NULL)").
				Where("billing_issue_attempted = ? AND billing_attempted = ?", false, false).
				Where("(billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)").
				Updates(map[string]interface{}{
					"status":                   common.TopUpStatusExpired,
					"complete_time":            now,
					"provider_credential":      "",
					"provider_client_key_hash": "",
				}).Error; err != nil {
				return err
			}

			var subscriptionCount int64
			if err := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND plan_id = ?", order.UserId, plan.Id).
				Count(&subscriptionCount).Error; err != nil {
				return err
			}
			var reservationCount int64
			if err := tx.Model(&SubscriptionOrder{}).
				Where("user_id = ? AND plan_id = ? AND payment_provider = ? AND status = ?", order.UserId, plan.Id, PaymentProviderToss, common.TopUpStatusPending).
				Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
				Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
				Where("renewal_order_id_version = ?", 0).
				Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL").
				Count(&reservationCount).Error; err != nil {
				return err
			}
			if subscriptionCount+reservationCount >= int64(plan.MaxPurchasePerUser) {
				return ErrSubscriptionPurchaseLimit
			}
		}
		return tx.Create(order).Error
	})
}

func hasBlockingTossInitialPaymentStateTx(tx *gorm.DB, userID, planID int) (bool, error) {
	if tx == nil || userID <= 0 || planID <= 0 {
		return false, errors.New("invalid Toss initial payment barrier scope")
	}
	if err := ensureNoIncompleteTossRenewalOrderIdentityTx(tx); err != nil {
		return false, err
	}
	initialOrders := func() *gorm.DB {
		return tx.Model(&SubscriptionOrder{}).
			Where("user_id = ? AND plan_id = ? AND payment_provider = ?", userID, planID, PaymentProviderToss).
			Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
			Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
			Where("renewal_order_id_version = ?", 0).
			Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL")
	}

	var attemptedPending int64
	if err := initialOrders().
		Where("status = ?", common.TopUpStatusPending).
		Where("billing_issue_attempted = ? OR billing_attempted = ?", true, true).
		Limit(1).Count(&attemptedPending).Error; err != nil {
		return false, err
	}
	if attemptedPending > 0 {
		return true, nil
	}

	tradeNos := initialOrders().Select("trade_no")
	var unresolvedFinancialEvents int64
	if err := tx.Model(&TossPaymentEvent{}).
		Where("order_id IN (?)", tradeNos).
		Where("reconciliation_status = ?", TossReconciliationStatusRequired).
		Where(
			"event_type IN ? OR (event_type = ? AND (status = ? OR balance_amount <> ?))",
			[]string{TossPaymentEventTypeFulfillment, TossPaymentEventTypeFinancialMismatch},
			TossPaymentEventTypeCancellation,
			"PARTIAL_CANCELED",
			0,
		).
		Limit(1).Count(&unresolvedFinancialEvents).Error; err != nil {
		return false, err
	}
	return unresolvedFinancialEvents > 0, nil
}

func (o *SubscriptionOrder) Update() error {
	return DB.Save(o).Error
}

func GetSubscriptionOrderByTradeNo(tradeNo string) *SubscriptionOrder {
	order, err := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return nil
	}
	return order
}

// GetSubscriptionOrderByTradeNoWithError preserves database failures so
// webhook handlers can retry them instead of treating them as a missing order.
func GetSubscriptionOrderByTradeNoWithError(tradeNo string) (*SubscriptionOrder, error) {
	return GetSubscriptionOrderByTradeNoWithErrorContext(context.Background(), tradeNo)
}

func GetSubscriptionOrderByTradeNoWithErrorContext(ctx context.Context, tradeNo string) (*SubscriptionOrder, error) {
	if tradeNo == "" {
		return nil, ErrSubscriptionOrderNotFound
	}
	var order SubscriptionOrder
	if err := dbWithContext(ctx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionOrderNotFound
		}
		return nil, err
	}
	return &order, nil
}

func AttachTossBillingKeyToOrder(tradeNo string, billingKeyId int) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	if billingKeyId <= 0 {
		return errors.New("billingKeyId is invalid")
	}
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL OR billing_key_id = ?)",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, billingKeyId).
		Update("billing_key_id", billingKeyId)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	order, err := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	if order.BillingKeyId == billingKeyId &&
		(order.Status == common.TopUpStatusPending || order.Status == common.TopUpStatusSuccess) {
		return nil
	}
	return ErrSubscriptionOrderStatusInvalid
}

// User subscription instance
type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_user_sub_active,priority:3"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_user_sub_active,priority:2"` // active/expired/cancelled

	Source string `json:"source" gorm:"type:varchar(32);default:'order'"` // order/admin

	// Toss 자동결제(빌링) 연동 필드
	AutoRenew       bool  `json:"auto_renew" gorm:"default:false"`
	NextBillingTime int64 `json:"next_billing_time" gorm:"default:0;index"`
	// BillingRetryTime is the next time the bounded renewal queue may select
	// this subscription. Reserving it before provider/model work prevents a
	// permanently malformed oldest row from starving every later renewal.
	BillingRetryTime int64 `json:"-" gorm:"default:0;index"`
	BillingKeyId     int   `json:"billing_key_id" gorm:"default:0;index"`
	BillingFailCount int   `json:"billing_fail_count" gorm:"default:0"`
	// TossRenewalContractSnapshot is the write-once commercial contract from
	// the initial paid order. Plan edits affect new purchases only; renewals keep
	// the agreed price, provider amount, period, quota, reset, and group terms.
	TossRenewalContractSnapshot string `json:"-" gorm:"type:text"`

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *UserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type SubscriptionSummary struct {
	Subscription *UserSubscription `json:"subscription"`
}

func ValidateSubscriptionPlanDuration(plan *SubscriptionPlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 || plan.CustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("custom_seconds must be between 1 and %d", SubscriptionPlanMaxCustomSeconds)
		}
	case SubscriptionDurationYear, SubscriptionDurationMonth, SubscriptionDurationDay, SubscriptionDurationHour:
		if plan.DurationValue <= 0 || plan.DurationValue > SubscriptionPlanMaxDurationValue {
			return fmt.Errorf("duration_value must be between 1 and %d", SubscriptionPlanMaxDurationValue)
		}
		if plan.CustomSeconds < 0 || plan.CustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("custom_seconds must be between 0 and %d", SubscriptionPlanMaxCustomSeconds)
		}
	default:
		return fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
	return nil
}

func ValidateSubscriptionPlanReset(plan *SubscriptionPlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetCustom {
		if plan.QuotaResetCustomSeconds <= 0 || plan.QuotaResetCustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("quota_reset_custom_seconds must be between 1 and %d", SubscriptionPlanMaxCustomSeconds)
		}
		return nil
	}
	if plan.QuotaResetCustomSeconds < 0 || plan.QuotaResetCustomSeconds > SubscriptionPlanMaxCustomSeconds {
		return fmt.Errorf("quota_reset_custom_seconds must be between 0 and %d", SubscriptionPlanMaxCustomSeconds)
	}
	return nil
}

func ValidateSubscriptionPlanTiming(plan *SubscriptionPlan) error {
	if err := ValidateSubscriptionPlanDuration(plan); err != nil {
		return err
	}
	return ValidateSubscriptionPlanReset(plan)
}

func calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error) {
	if err := ValidateSubscriptionPlanDuration(plan); err != nil {
		return 0, err
	}
	var end time.Time
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		end = start.AddDate(plan.DurationValue, 0, 0)
	case SubscriptionDurationMonth:
		end = start.AddDate(0, plan.DurationValue, 0)
	case SubscriptionDurationDay:
		end = start.Add(time.Duration(plan.DurationValue) * 24 * time.Hour)
	case SubscriptionDurationHour:
		end = start.Add(time.Duration(plan.DurationValue) * time.Hour)
	case SubscriptionDurationCustom:
		end = start.Add(time.Duration(plan.CustomSeconds) * time.Second)
	}
	endUnix := end.Unix()
	if !end.After(start) || endUnix <= start.Unix() {
		return 0, errors.New("subscription plan end time overflowed")
	}
	return endUnix, nil
}

func CalcSubscriptionPlanEndTime(start time.Time, durationUnit string, durationValue int, customSeconds int64) (int64, error) {
	plan := &SubscriptionPlan{
		DurationUnit:  durationUnit,
		DurationValue: durationValue,
		CustomSeconds: customSeconds,
	}
	return calcPlanEndTime(start, plan)
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64 {
	if ValidateSubscriptionPlanReset(plan) != nil {
		return 0
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetNever {
		return 0
	}
	var next time.Time
	switch period {
	case SubscriptionResetDaily:
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, 1)
	case SubscriptionResetWeekly:
		// Align to next Monday 00:00
		weekday := int(base.Weekday()) // Sunday=0
		// Convert to Monday=1..Sunday=7
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, daysUntil)
	case SubscriptionResetMonthly:
		// Align to first day of next month 00:00
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).
			AddDate(0, 1, 0)
	case SubscriptionResetCustom:
		next = base.Add(time.Duration(plan.QuotaResetCustomSeconds) * time.Second)
	default:
		return 0
	}
	nextUnix := next.Unix()
	if !next.After(base) || nextUnix <= base.Unix() {
		return 0
	}
	if endUnix > 0 && nextUnix > endUnix {
		return 0
	}
	return nextUnix
}

func CalcSubscriptionNextResetTime(base time.Time, resetPeriod string, resetCustomSeconds int64, endUnix int64) int64 {
	plan := &SubscriptionPlan{
		QuotaResetPeriod:        resetPeriod,
		QuotaResetCustomSeconds: resetCustomSeconds,
	}
	return calcNextResetTime(base, plan, endUnix)
}

func GetSubscriptionPlanById(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(nil, id)
}

// GetSubscriptionPlanByIdForPayment bypasses the display cache so a checkout
// never snapshots a price, enabled flag, quota, or duration that another API
// node has already changed in the database. Once the order is created, its
// immutable plan snapshot remains the user's request-time contract.
func GetSubscriptionPlanByIdForPayment(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(DB, id)
}

func getSubscriptionPlanByIdTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	key := subscriptionPlanCacheKey(id)
	if tx == nil && key != "" {
		if cached, found, err := getSubscriptionPlanCache().Get(key); err == nil && found {
			return &cached, nil
		}
	}
	var plan SubscriptionPlan
	query := DB
	if tx != nil {
		query = tx
	}
	if err := query.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	if tx == nil {
		_ = getSubscriptionPlanCache().SetWithTTL(key, plan, subscriptionPlanCacheTTL())
	}
	return &plan, nil
}

func CountUserSubscriptionsByPlan(userId int, planId int) (int64, error) {
	if userId <= 0 || planId <= 0 {
		return 0, errors.New("invalid userId or planId")
	}
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", userId, planId).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func getUserGroupByIdTx(tx *gorm.DB, userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid userId")
	}
	if tx == nil {
		tx = DB
	}
	var group string
	if err := tx.Model(&User{}).Where("id = ?", userId).Select(commonGroupCol).Find(&group).Error; err != nil {
		return "", err
	}
	return group, nil
}

func downgradeUserGroupForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	if upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	if currentGroup != upgradeGroup {
		return "", nil
	}
	var activeSub UserSubscription
	activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> ''",
		sub.UserId, "active", now, sub.Id).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
		return "", nil
	}
	prevGroup := strings.TrimSpace(sub.PrevUserGroup)
	if prevGroup == "" || prevGroup == currentGroup {
		return "", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
		Update("group", prevGroup).Error; err != nil {
		return "", err
	}
	return prevGroup, nil
}

func CreateUserSubscriptionFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, source string) (*UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if err := ValidateSubscriptionPlanTiming(plan); err != nil {
		return nil, err
	}
	// Serialize the purchase-limit check and subscription insert per user.
	// Without this row lock, two paid callbacks can both observe count=0 for a
	// max=1 plan and attempt to fulfill the same user concurrently.
	var lockedUser User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", userId).First(&lockedUser).Error; err != nil {
		return nil, err
	}
	if plan.MaxPurchasePerUser > 0 {
		var count int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND plan_id = ?", userId, plan.Id).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, ErrSubscriptionPurchaseLimit
		}
	}
	nowUnix := getDBTimestampTx(tx)
	now := time.Unix(nowUnix, 0)
	endUnix, err := calcPlanEndTime(now, plan)
	if err != nil {
		return nil, err
	}
	resetBase := now
	nextReset := calcNextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := getUserGroupByIdTx(tx, userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", upgradeGroup).Error; err != nil {
				return nil, err
			}
		}
	}
	sub := &UserSubscription{
		UserId:        userId,
		PlanId:        plan.Id,
		AmountTotal:   plan.TotalAmount,
		AmountUsed:    0,
		StartTime:     now.Unix(),
		EndTime:       endUnix,
		Status:        "active",
		Source:        source,
		LastResetTime: lastReset,
		NextResetTime: nextReset,
		UpgradeGroup:  upgradeGroup,
		PrevUserGroup: prevGroup,
		CreatedAt:     common.GetTimestamp(),
		UpdatedAt:     common.GetTimestamp(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// Complete a subscription order (idempotent). Creates a UserSubscription snapshot from the plan.
// expectedPaymentProvider guards against cross-gateway callback attacks (empty skips the check).
// actualPaymentMethod updates the order's PaymentMethod to reflect the real payment type used (empty skips update).
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	var logUserId int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		plan, err := getSubscriptionPlanByIdTx(tx, order.PlanId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			// still allow completion for already purchased orders
		}
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		_, err = CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		order.Status = common.TopUpStatusSuccess
		order.CompleteTime = common.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
			order.PaymentMethod = actualPaymentMethod
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		logUserId = order.UserId
		logPlanTitle = plan.Title
		logMoney = order.Money
		logPaymentMethod = order.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, upgradeGroup)
	}
	if logUserId > 0 {
		msg := fmt.Sprintf("subscription purchased successfully, plan: %s, amount paid: %.2f, payment method: %s", logPlanTitle, logMoney, logPaymentMethod)
		RecordLog(logUserId, LogTypeTopup, msg)
	}
	return nil
}

func upsertSubscriptionTopUpTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}
	now := common.GetTimestamp()
	var topup TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = TopUp{
				UserId:        order.UserId,
				Amount:        0,
				Money:         order.Money,
				TradeNo:       order.TradeNo,
				PaymentMethod: order.PaymentMethod,
				CreateTime:    order.CreateTime,
				CompleteTime:  now,
				Status:        common.TopUpStatusSuccess,
			}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = common.TopUpStatusSuccess
	return tx.Save(&topup).Error
}

func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	query := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND status = ?", tradeNo, common.TopUpStatusPending)
	if expectedPaymentProvider != "" {
		query = query.Where("payment_provider = ?", expectedPaymentProvider)
	}
	result := query.Updates(map[string]interface{}{
		"status":        common.TopUpStatusExpired,
		"complete_time": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	order, err := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return err
	}
	if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
		return ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending {
		return nil
	}
	return ErrSubscriptionOrderStatusInvalid
}

// CancelUnattemptedTossSubscriptionOrder releases a checkout reservation only
// while there is still no durable evidence that billing authorization, billing
// key issuance, or a charge request has started. The conditional update is the
// cross-node synchronization point: either this cancellation wins and a later
// success callback cannot claim the order, or the callback records its claim
// first and this function leaves the order untouched.
func CancelUnattemptedTossSubscriptionOrder(tradeNo string, userID int) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return false, errors.New("tradeNo is empty")
	}
	now := GetDBTimestamp()
	query := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ?", tradeNo, PaymentProviderToss, common.TopUpStatusPending)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	result := query.
		Where("(billing_key_id = 0 OR billing_key_id IS NULL)").
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL) AND (billing_claim_time = 0 OR billing_claim_time IS NULL)").
		Where("(billing_issue_auth_key = '' OR billing_issue_auth_key IS NULL) AND (billing_issue_auth_key_hash = '' OR billing_issue_auth_key_hash IS NULL) AND (billing_issue_customer_key = '' OR billing_issue_customer_key IS NULL)").
		Where("billing_issue_attempted = ? AND billing_attempted = ?", false, false).
		Where("(billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)").
		Updates(map[string]interface{}{
			"status":                   common.TopUpStatusExpired,
			"complete_time":            now,
			"provider_credential":      "",
			"provider_client_key_hash": "",
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}

	order, err := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return false, err
	}
	// Do not reveal another user's order through the authenticated endpoint.
	if userID > 0 && order.UserId != userID {
		return false, ErrSubscriptionOrderNotFound
	}
	if order.PaymentProvider != PaymentProviderToss {
		return false, ErrPaymentMethodMismatch
	}
	// Terminal orders and orders with provider activity are intentionally
	// idempotent no-ops. Their normal settlement/reconciliation path owns them.
	return false, nil
}

func ExpireTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	transitioned := false
	err := runTossSettlementTransaction(DB, func(tx *gorm.DB) error {
		transitioned = false
		if _, err := lockTossSubscriptionOrderOwnerForCleanupTx(tx, tradeNo); err != nil {
			return err
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusExpired,
				"complete_time": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		transitioned = true

		var order SubscriptionOrder
		if err := tx.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.BillingKeyId <= 0 {
			return nil
		}
		return tx.Model(&UserBillingKey{}).
			Where("id = ? AND status <> ?", order.BillingKeyId, BillingKeyStatusRevoked).
			Update("status", BillingKeyStatusPendingRevocation).Error
	})
	if err != nil || transitioned {
		return err
	}
	order, err := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending {
		return nil
	}
	return ErrSubscriptionOrderStatusInvalid
}

func ExpireTossPendingRenewalOrderAndMarkFailure(subId int, tradeNo string, maxFails int) (bool, error) {
	if subId <= 0 {
		return false, errors.New("subId is invalid")
	}
	if tradeNo == "" {
		return false, errors.New("tradeNo is empty")
	}
	disabled := false
	transitioned := false
	err := runTossSettlementTransaction(DB, func(tx *gorm.DB) error {
		disabled = false
		transitioned = false
		if _, err := lockTossSubscriptionOwnerTx(tx, subId); err != nil {
			return err
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusExpired,
				"complete_time": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		transitioned = true
		var err error
		disabled, err = markTossBillingFailureTx(tx, subId, maxFails)
		return err
	})
	if err != nil || transitioned {
		return disabled, err
	}
	order, lookupErr := GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if lookupErr != nil {
		return false, lookupErr
	}
	if order.PaymentProvider != PaymentProviderToss {
		return false, ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending {
		return false, nil
	}
	return false, ErrSubscriptionOrderStatusInvalid
}

func ReconcileStaleTossPendingSubscriptionOrders(ctx context.Context, cutoffUnix int64, limit int) (int64, error) {
	if tossSubscriptionOrderReconciler == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}
	// create_time identifies old orders, but it does not prove that an old order
	// is currently idle. A user can finish billing authentication long after the
	// order was created, at which point the callback owns a fresh provider-call
	// lease. Excluding live claims here prevents a cleanup worker on another node
	// from observing the pre-POST marker, seeing a transient 404, and expiring the
	// order while the callback is about to charge it.
	claimCutoff := GetDBTimestamp() - tossSubscriptionBillingClaimTTLSeconds
	var rows []SubscriptionOrder
	if err := DB.Where("payment_provider = ? AND create_time < ?", PaymentProviderToss, cutoffUnix).
		Where("((status = ? AND (billing_key_id > 0 OR ((billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_auth_key <> ''))) OR (status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_attempted = ? AND billing_issue_auth_key <> ''))",
			common.TopUpStatusPending, []string{common.TopUpStatusFailed, common.TopUpStatusExpired}, true).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", claimCutoff).
		// Never-attempted rows go first. A failed reconciliation preserves its
		// last claim time on release, which moves it behind other due rows and
		// prevents a permanently broken LIMIT-sized prefix from starving newer
		// paid orders forever.
		Order("CASE WHEN billing_claim_time IS NULL OR billing_claim_time <= 0 THEN 0 ELSE billing_claim_time END asc, create_time asc, id asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	type reconciliationResult struct {
		ok      bool
		err     error
		tradeNo string
	}
	outcomes := make(chan reconciliationResult, len(rows))
	runBoundedTossMaintenance(rows, func(row SubscriptionOrder) {
		ok, err := tossSubscriptionOrderReconciler(ctx, row)
		outcomes <- reconciliationResult{ok: ok, err: err, tradeNo: row.TradeNo}
	})
	close(outcomes)
	var resolved int64
	var lastErr error
	for outcome := range outcomes {
		if outcome.err != nil {
			lastErr = outcome.err
			common.SysError(fmt.Sprintf("failed to reconcile stale Toss subscription order %s: %v", outcome.tradeNo, outcome.err))
			continue
		}
		if outcome.ok {
			resolved++
		}
	}
	return resolved, lastErr
}

func ExpireStaleTossPendingSubscriptionOrders(cutoffUnix int64) (int64, error) {
	result := DB.Model(&SubscriptionOrder{}).
		Where("payment_provider = ? AND status = ? AND create_time < ?", PaymentProviderToss, common.TopUpStatusPending, cutoffUnix).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Where("renewal_order_id_version = ?", 0).
		Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL").
		Where("(billing_key_id = 0 OR billing_key_id IS NULL)").
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL) AND (billing_claim_time = 0 OR billing_claim_time IS NULL)").
		Where("(billing_issue_auth_key = '' OR billing_issue_auth_key IS NULL) AND (billing_issue_auth_key_hash = '' OR billing_issue_auth_key_hash IS NULL) AND (billing_issue_customer_key = '' OR billing_issue_customer_key IS NULL)").
		Where("billing_issue_attempted = ? AND billing_attempted = ?", false, false).
		Where("(billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)").
		Updates(map[string]interface{}{
			"status":                      common.TopUpStatusExpired,
			"complete_time":               GetDBTimestamp(),
			"provider_credential":         "",
			"provider_client_key_hash":    "",
			"billing_claim_token":         "",
			"billing_claim_time":          0,
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
			"billing_issue_attempted":     false,
		})
	return result.RowsAffected, result.Error
}

// Admin bind (no payment). Creates a UserSubscription from a plan.
func AdminBindSubscription(userId int, planId int, sourceNote string) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid userId or planId")
	}
	plan, err := GetSubscriptionPlanById(planId)
	if err != nil {
		return "", err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		return err
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(plan.UpgradeGroup) != "" {
		_ = UpdateUserGroupCache(userId, plan.UpgradeGroup)
		return plan.UpgradeGroup, nil
	}
	return "", nil
}

func calcSubscriptionBalanceQuota(priceAmount float64) (int64, error) {
	if priceAmount <= 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("quota unit configuration error")
	}
	quota := decimal.NewFromFloat(priceAmount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil().
		IntPart()
	return quota, nil
}

// PurchaseSubscriptionWithBalance creates a subscription by deducting the user's wallet quota.
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	if userId <= 0 || planId <= 0 {
		return errors.New("invalid userId or planId")
	}

	var logPlanTitle string
	var logMoney float64
	var chargedQuota int64
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("plan is not enabled")
		}
		if plan.PriceAmount < 0 {
			return errors.New("plan price cannot be negative")
		}

		requiredQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
		if err != nil {
			return err
		}

		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if requiredQuota > 0 && user.Quota < requiredQuota {
			return errors.New("insufficient balance")
		}
		if requiredQuota > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", requiredQuota)).Error; err != nil {
				return err
			}
		}

		if _, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, PaymentMethodBalance); err != nil {
			return err
		}

		now := getDBTimestampTx(tx)
		tradeNo := fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
		order := &SubscriptionOrder{
			UserId:          userId,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodBalance,
			PaymentProvider: PaymentProviderBalance,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", requiredQuota),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		logPlanTitle = plan.Title
		logMoney = plan.PriceAmount
		chargedQuota = requiredQuota
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		return nil
	})
	if err != nil {
		return err
	}

	if chargedQuota > 0 {
		if err := cacheDecrUserQuota(userId, int64(chargedQuota)); err != nil {
			common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
		}
	}
	if upgradeGroup != "" {
		_ = UpdateUserGroupCache(userId, upgradeGroup)
	}
	msg := fmt.Sprintf("subscription purchased with balance, plan: %s, amount paid: %.2f, quota deducted: %d", logPlanTitle, logMoney, chargedQuota)
	RecordLog(userId, LogTypeTopup, msg)
	return nil
}

// GetAllActiveUserSubscriptions returns all active subscriptions for a user.
func GetAllActiveUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	err := DB.Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

// HasActiveUserSubscription returns whether the user has any active subscription.
// This is a lightweight existence check to avoid heavy pre-consume transactions.
func HasActiveUserSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var count int64
	if err := DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAllUserSubscriptions returns all subscriptions (active and expired) for a user.
func GetAllUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	var subs []UserSubscription
	err := DB.Where("user_id = ?", userId).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

func buildSubscriptionSummaries(subs []UserSubscription) []SubscriptionSummary {
	if len(subs) == 0 {
		return []SubscriptionSummary{}
	}
	result := make([]SubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		result = append(result, SubscriptionSummary{
			Subscription: &subCopy,
		})
	}
	return result
}

// AdminInvalidateUserSubscription marks a user subscription as cancelled and ends it immediately.
func AdminInvalidateUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	remoteKeys := make([]tossBillingRevocationCandidate, 0)
	seenRemoteKeys := make(map[int]struct{})
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOwnerTx(tx, userSubscriptionId); err != nil {
			return err
		}
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		shouldReleaseBillingKey := sub.AutoRenew && sub.BillingKeyId > 0
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":             "cancelled",
			"end_time":           now,
			"auto_renew":         false,
			"next_billing_time":  0,
			"billing_retry_time": 0,
			"updated_at":         now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if shouldReleaseBillingKey {
			queued, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, sub.BillingKeyId)
			if err != nil {
				return err
			}
			if queued {
				collectTossBillingRevocationCandidateTx(tx, &remoteKeys, seenRemoteKeys, sub.BillingKeyId, "admin invalidate")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	revokeTossBillingCandidates(context.Background(), remoteKeys, "admin invalidate")
	if downgradeGroup != "" {
		return downgradeGroup, nil
	}
	return "", nil
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	remoteKeys := make([]tossBillingRevocationCandidate, 0)
	seenRemoteKeys := make(map[int]struct{})
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOwnerTx(tx, userSubscriptionId); err != nil {
			return err
		}
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		shouldReleaseBillingKey := sub.AutoRenew && sub.BillingKeyId > 0
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if err := tx.Where("id = ?", userSubscriptionId).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		if shouldReleaseBillingKey {
			queued, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, sub.BillingKeyId)
			if err != nil {
				return err
			}
			if queued {
				collectTossBillingRevocationCandidateTx(tx, &remoteKeys, seenRemoteKeys, sub.BillingKeyId, "admin delete")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	revokeTossBillingCandidates(context.Background(), remoteKeys, "admin delete")
	if downgradeGroup != "" {
		return downgradeGroup, nil
	}
	return "", nil
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
}

// ExpireDueSubscriptions marks expired subscriptions and handles group downgrade.
func ExpireDueSubscriptions(limit int) (int, error) {
	return expireDueSubscriptions(limit, false, false)
}

// ExpireDueSubscriptionsIncludingTossAutoRenew marks expired subscriptions,
// including Toss auto-renew subscriptions that are normally left for the
// recurring billing task.
func ExpireDueSubscriptionsIncludingTossAutoRenew(limit int) (int, error) {
	return expireDueSubscriptions(limit, true, true)
}

// ExpireDueSubscriptionsIncludingTossAutoRenewLocalOnly marks expired
// subscriptions without calling Toss remotely. Billing keys are moved to
// pending_revocation so remote cleanup can retry when payment operations are
// allowed again.
func ExpireDueSubscriptionsIncludingTossAutoRenewLocalOnly(limit int) (int, error) {
	return expireDueSubscriptions(limit, true, false)
}

// ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace expires overdue
// auto-renew subscriptions only after an operational grace period. This keeps
// a short-lived credential/configuration outage from permanently cancelling a
// customer's renewal, while still preventing very old renewals from being
// charged unexpectedly when billing is re-enabled much later.
func ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(limit int, graceSeconds int64, revokeRemote bool) (int, error) {
	if graceSeconds < 0 {
		graceSeconds = 0
	}
	now := GetDBTimestamp()
	return expireDueSubscriptionsBefore(limit, true, revokeRemote, now-graceSeconds, now, true)
}

func expireDueSubscriptions(limit int, includeTossAutoRenew bool, revokeRemote bool) (int, error) {
	now := GetDBTimestamp()
	return expireDueSubscriptionsBefore(limit, includeTossAutoRenew, revokeRemote, now, now, false)
}

const tossRenewalExpiryRecoveryDeferralSeconds int64 = tossSubscriptionBillingClaimTTLSeconds + 2*60

// hasRecentCurrentCycleTossRenewalAttemptTx protects only the exact current
// renewal cycle, and only long enough for a live five-minute claim plus two
// one-minute recovery ticks. Old cycles, another subscription's order, a
// different billing key, and indefinitely abandoned attempts cannot keep
// entitlement active forever.
func hasRecentCurrentCycleTossRenewalAttemptTx(tx *gorm.DB, sub *UserSubscription, now int64) (bool, error) {
	if tx == nil || sub == nil || sub.Id <= 0 || sub.BillingKeyId <= 0 || sub.EndTime <= 0 {
		return false, nil
	}
	if err := ensureNoIncompleteTossRenewalOrderIdentityTx(tx); err != nil {
		// An incomplete opaque row cannot be scoped to a subscription. Abort the
		// expiry batch rather than accidentally expiring the entitlement whose
		// provider attempt is hidden by that corrupt evidence.
		return false, err
	}
	escapedPrefix := strings.TrimSuffix(tossRenewalTradeNoLikePattern(), "%")
	pattern := fmt.Sprintf("%s%d!_%%", escapedPrefix, sub.Id)
	var orders []SubscriptionOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "trade_no", "payment_provider", "billing_key_id", "renewal_end_time", "create_time", "billing_claim_time", "billing_attempt_time", "renewal_subscription_id", "renewal_billing_time", "renewal_attempt", "renewal_order_id_version").
		Where("payment_provider = ? AND status = ? AND billing_attempted = ? AND billing_key_id = ? AND renewal_end_time = ?",
			PaymentProviderToss, common.TopUpStatusPending, true, sub.BillingKeyId, sub.EndTime).
		Where("renewal_subscription_id = ? OR trade_no LIKE ? ESCAPE '!'", sub.Id, pattern).
		Order("id desc").
		Limit(32).
		Find(&orders).Error; err != nil {
		return false, err
	}
	cutoff := now - tossRenewalExpiryRecoveryDeferralSeconds
	for i := range orders {
		subID, billingTime, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentity(&orders[i])
		if identityErr != nil {
			return false, identityErr
		}
		if !isRenewal || subID != sub.Id {
			continue
		}
		// A normal cancellation intentionally clears NextBillingTime after a
		// provider POST may already have succeeded. In that state the immutable
		// subscription association, billing key, RenewalEndTime, pending/attempted
		// markers, and bounded first-attempt timestamp above are the current-cycle
		// proof. When scheduling is still present, retain the stronger exact-time
		// comparison so an unrelated logical cycle cannot defer expiry.
		if sub.NextBillingTime > 0 && billingTime != sub.NextBillingTime {
			continue
		}
		attemptTime := orders[i].BillingAttemptTime
		if attemptTime <= 0 {
			// Legacy rows predate BillingAttemptTime. Backfill once from the oldest
			// immutable evidence available; a recently refreshed claim must never
			// turn an old attempt into a fresh entitlement deferral.
			attemptTime = orders[i].CreateTime
			if attemptTime <= 0 || (orders[i].BillingClaimTime > 0 && orders[i].BillingClaimTime < attemptTime) {
				attemptTime = orders[i].BillingClaimTime
			}
			if attemptTime > 0 {
				updated := tx.Model(&SubscriptionOrder{}).
					Where("id = ? AND (billing_attempt_time = 0 OR billing_attempt_time IS NULL)", orders[i].Id).
					Update("billing_attempt_time", attemptTime)
				if updated.Error != nil {
					return false, updated.Error
				}
				if updated.RowsAffected == 0 {
					if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", orders[i].Id).
						Select("billing_attempt_time").Scan(&attemptTime).Error; err != nil {
						return false, err
					}
				}
			}
		}
		if attemptTime > cutoff {
			return true, nil
		}
	}
	return false, nil
}

func expireDueSubscriptionsBefore(limit int, includeTossAutoRenew bool, revokeRemote bool, dueBefore, now int64, onlyTossAutoRenew bool) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	if dueBefore > now {
		dueBefore = now
	}
	var subs []UserSubscription
	query := DB.Where("status = ? AND end_time > 0 AND end_time <= ?", "active", dueBefore)
	if onlyTossAutoRenew {
		query = query.Where("auto_renew = ? AND billing_key_id > 0", true)
	} else if !includeTossAutoRenew {
		query = query.Where("(auto_renew = ? OR billing_key_id = 0)", false)
	}
	if err := query.
		Order("end_time asc, id asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	expiredCount := 0
	userIds := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if sub.UserId > 0 {
			userIds[sub.UserId] = struct{}{}
		}
	}
	for userId := range userIds {
		cacheGroup := ""
		remoteKeys := make([]tossBillingRevocationCandidate, 0)
		seenRemoteKeys := make(map[int]struct{})
		err := DB.Transaction(func(tx *gorm.DB) error {
			if err := lockTossBillingOwnerTx(tx, userId); err != nil {
				return err
			}
			var dueSubs []UserSubscription
			dueQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userId, "active", dueBefore)
			if onlyTossAutoRenew {
				dueQuery = dueQuery.Where("auto_renew = ? AND billing_key_id > 0", true)
			} else if !includeTossAutoRenew {
				dueQuery = dueQuery.Where("(auto_renew = ? OR billing_key_id = 0)", false)
			}
			if err := dueQuery.Find(&dueSubs).Error; err != nil {
				return err
			}
			updatedAt := common.GetTimestamp()
			hasProtectedRenewal := false
			for i := range dueSubs {
				// The regular expiry sweep normally owns subscriptions whose
				// auto-renew flag has been cleared. If cancellation landed after a
				// provider POST timed out, the exact current-cycle payment still needs
				// the same short GET-only recovery window as the auto-renew sweep.
				// Manual subscriptions (no billing key), unrelated orders, and terminal
				// renewal orders cannot satisfy the helper's strict match.
				checkPendingTossRenewal := onlyTossAutoRenew ||
					(!includeTossAutoRenew && !dueSubs[i].AutoRenew && dueSubs[i].BillingKeyId > 0)
				if checkPendingTossRenewal {
					protected, err := hasRecentCurrentCycleTossRenewalAttemptTx(tx, &dueSubs[i], now)
					if err != nil {
						return err
					}
					if protected {
						hasProtectedRenewal = true
						continue
					}
				}
				shouldReleaseBillingKey := dueSubs[i].AutoRenew && dueSubs[i].BillingKeyId > 0
				dueSubs[i].Status = "expired"
				dueSubs[i].AutoRenew = false
				dueSubs[i].UpdatedAt = updatedAt
				if err := tx.Save(&dueSubs[i]).Error; err != nil {
					return err
				}
				if shouldReleaseBillingKey {
					queued, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, dueSubs[i].BillingKeyId)
					if err != nil {
						return err
					}
					if queued && revokeRemote {
						collectTossBillingRevocationCandidateTx(tx, &remoteKeys, seenRemoteKeys, dueSubs[i].BillingKeyId, "subscription expiry")
					}
				}
				expiredCount++
			}
			if hasProtectedRenewal {
				// Its provider result is still within bounded GET-only recovery.
				// Keep the upgraded group until the payment settles or the deferral
				// expires; an overdue-but-active protected entitlement still owns it.
				return nil
			}

			// If there's an active upgraded subscription, keep current group. The
			// default expiry task leaves due auto-renew subscriptions with a billing
			// key to the billing task, so they should still protect the current group
			// unless this fallback is explicitly expiring them too.
			var activeSub UserSubscription
			activeQueryBuilder := tx.Where("user_id = ? AND status = ? AND upgrade_group <> '' AND end_time > ?", userId, "active", now)
			if !includeTossAutoRenew {
				activeQueryBuilder = tx.Where("user_id = ? AND status = ? AND upgrade_group <> '' AND (end_time > ? OR (auto_renew = ? AND billing_key_id > 0))",
					userId, "active", now, true)
			}
			activeQuery := activeQueryBuilder.Order("end_time desc, id desc").
				Limit(1).
				Find(&activeSub)
			if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
				return nil
			}

			// No active upgraded subscription, downgrade to previous group if needed.
			var lastExpired UserSubscription
			expiredQuery := tx.Where("user_id = ? AND status = ? AND upgrade_group <> ''",
				userId, "expired").
				Order("end_time desc, id desc").
				Limit(1).
				Find(&lastExpired)
			if expiredQuery.Error != nil || expiredQuery.RowsAffected == 0 {
				return nil
			}
			upgradeGroup := strings.TrimSpace(lastExpired.UpgradeGroup)
			prevGroup := strings.TrimSpace(lastExpired.PrevUserGroup)
			if upgradeGroup == "" || prevGroup == "" {
				return nil
			}
			currentGroup, err := getUserGroupByIdTx(tx, userId)
			if err != nil {
				return err
			}
			if currentGroup != upgradeGroup || currentGroup == prevGroup {
				return nil
			}
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("group", prevGroup).Error; err != nil {
				return err
			}
			cacheGroup = prevGroup
			return nil
		})
		if err != nil {
			return expiredCount, err
		}
		if cacheGroup != "" {
			_ = UpdateUserGroupCache(userId, cacheGroup)
		}
		if revokeRemote {
			revokeTossBillingCandidates(context.Background(), remoteKeys, "subscription expiry")
		}
	}
	return expiredCount, nil
}

// SubscriptionPreConsumeRecord stores idempotent pre-consume operations per request.
type SubscriptionPreConsumeRecord struct {
	Id                 int    `json:"id"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	PreConsumed        int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status             string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *SubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionPreConsumeRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func maybeResetUserSubscriptionWithPlanTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return nil
	}
	if NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		return nil
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTime(base, plan, sub.EndTime)
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = calcNextResetTime(base, plan, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return tx.Save(sub).Error
		}
		return nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return tx.Save(sub).Error
}

func getUserSubscriptionEntitlementPlanTx(tx *gorm.DB, sub *UserSubscription) (*SubscriptionPlan, error) {
	if sub == nil {
		return nil, errors.New("subscription is nil")
	}
	if strings.TrimSpace(sub.TossRenewalContractSnapshot) != "" {
		contract, err := ResolveTossRenewalContract(sub)
		if err != nil {
			return nil, err
		}
		return contract.Plan, nil
	}
	if sub.BillingKeyId > 0 {
		contract, err := loadOrBackfillTossRenewalContractTx(tx, sub)
		if err == nil {
			return contract.Plan, nil
		}
		if !isFatalTossRenewalContractError(err) {
			return nil, err
		}
		// A legacy Toss subscription whose original contract cannot be proved
		// must not inherit a newly edited plan's quota/reset cadence. Keep the
		// already-granted period and quota usable, disable future charging, and
		// conservatively stop further periodic resets for this legacy period.
		if err := disableTossRenewalWithoutContractTx(tx, sub); err != nil {
			return nil, err
		}
		if sub.NextResetTime != 0 {
			if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).
				Update("next_reset_time", 0).Error; err != nil {
				return nil, err
			}
			sub.NextResetTime = 0
		}
		return &SubscriptionPlan{
			Id:               sub.PlanId,
			TotalAmount:      sub.AmountTotal,
			QuotaResetPeriod: SubscriptionResetNever,
		}, nil
	}
	return getSubscriptionPlanByIdTx(tx, sub.PlanId)
}

// PreConsumeUserSubscription pre-consumes from any active subscription total quota.
func PreConsumeUserSubscription(requestId string, userId int, modelName string, quotaType int, amount int64) (*SubscriptionPreConsumeResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := GetDBTimestamp()

	returnValue := &SubscriptionPreConsumeResult{}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing SubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if existing.Status == "refunded" {
				return errors.New("subscription pre-consume already refunded")
			}
			var sub UserSubscription
			if err := tx.Where("id = ?", existing.UserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = existing.PreConsumed
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = sub.AmountUsed
			returnValue.AmountUsedAfter = sub.AmountUsed
			return nil
		}

		var subs []UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return errors.New("no active subscription")
		}
		if len(subs) == 0 {
			return errors.New("no active subscription")
		}
		for _, candidate := range subs {
			sub := candidate
			plan, err := getUserSubscriptionEntitlementPlanTx(tx, &sub)
			if err != nil {
				return err
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
				return err
			}
			usedBefore := sub.AmountUsed
			if sub.AmountTotal > 0 {
				remain := sub.AmountTotal - usedBefore
				if remain < amount {
					continue
				}
			}
			record := &SubscriptionPreConsumeRecord{
				RequestId:          requestId,
				UserId:             userId,
				UserSubscriptionId: sub.Id,
				PreConsumed:        amount,
				Status:             "consumed",
			}
			if err := tx.Create(record).Error; err != nil {
				var dup SubscriptionPreConsumeRecord
				if err2 := tx.Where("request_id = ?", requestId).First(&dup).Error; err2 == nil {
					if dup.Status == "refunded" {
						return errors.New("subscription pre-consume already refunded")
					}
					returnValue.UserSubscriptionId = sub.Id
					returnValue.PreConsumed = dup.PreConsumed
					returnValue.AmountTotal = sub.AmountTotal
					returnValue.AmountUsedBefore = sub.AmountUsed
					returnValue.AmountUsedAfter = sub.AmountUsed
					return nil
				}
				return err
			}
			sub.AmountUsed += amount
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = amount
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = usedBefore
			returnValue.AmountUsedAfter = sub.AmountUsed
			return nil
		}
		return fmt.Errorf("subscription quota insufficient, need=%d", amount)
	})
	if err != nil {
		return nil, err
	}
	return returnValue, nil
}

// RefundSubscriptionPreConsume is idempotent and refunds pre-consumed subscription quota by requestId.
func RefundSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(&record).Error
		}
		if err := PostConsumeUserSubscriptionDelta(record.UserSubscriptionId, -record.PreConsumed); err != nil {
			return err
		}
		record.Status = "refunded"
		return tx.Save(&record).Error
	})
}

// ResetDueSubscriptions resets subscriptions whose next_reset_time has passed.
func ResetDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", now, "active").
		Order("next_reset_time asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, sub := range subs {
		subCopy := sub
		err := DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ?", subCopy.Id, now).
				First(&locked).Error; err != nil {
				return nil
			}
			plan, err := getUserSubscriptionEntitlementPlanTx(tx, &locked)
			if err != nil || plan == nil {
				return err
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &locked, plan, now); err != nil {
				return err
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
	}
	return resetCount, nil
}

// CleanupSubscriptionPreConsumeRecords removes old idempotency records to keep table small.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	cutoff := GetDBTimestamp() - olderThanSeconds
	res := DB.Where("updated_at < ?", cutoff).Delete(&SubscriptionPreConsumeRecord{})
	return res.RowsAffected, res.Error
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

func GetSubscriptionPlanInfoByUserSubscriptionId(userSubscriptionId int) (*SubscriptionPlanInfo, error) {
	if userSubscriptionId <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}
	cacheKey := fmt.Sprintf("sub:%d", userSubscriptionId)
	if cached, found, err := getSubscriptionPlanInfoCache().Get(cacheKey); err == nil && found {
		return &cached, nil
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
	if err != nil {
		return nil, err
	}
	info := &SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: plan.Title,
	}
	_ = getSubscriptionPlanInfoCache().SetWithTTL(cacheKey, *info, subscriptionPlanInfoCacheTTL())
	return info, nil
}

type tossBillingRevocationCandidate struct {
	keyId int
}

// lockTossBillingOwnerTx is the first lock in every subscription/key lifecycle
// transaction. Charge authorization uses the same owner -> subscription/order
// -> key order, preventing cleanup from deadlocking with the final pre-POST
// gate on MySQL and PostgreSQL. A deleted owner needs no lock, but cleanup of
// the remaining financial rows must still be allowed to finish.
func lockTossBillingOwnerTx(tx *gorm.DB, userId int) error {
	if tx == nil || userId <= 0 || !tx.Migrator().HasTable(&User{}) {
		return nil
	}
	var owner User
	err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", userId).First(&owner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func lockTossSubscriptionOwnerTx(tx *gorm.DB, userSubscriptionId int) (int, error) {
	if tx == nil || userSubscriptionId <= 0 {
		return 0, gorm.ErrRecordNotFound
	}
	var reference UserSubscription
	if err := tx.Select("user_id").Where("id = ?", userSubscriptionId).First(&reference).Error; err != nil {
		return 0, err
	}
	if err := lockTossBillingOwnerTx(tx, reference.UserId); err != nil {
		return 0, err
	}
	return reference.UserId, nil
}

func lockTossSubscriptionOrderOwnerForCleanupTx(tx *gorm.DB, tradeNo string) (int, error) {
	if tx == nil || strings.TrimSpace(tradeNo) == "" {
		return 0, gorm.ErrRecordNotFound
	}
	var reference SubscriptionOrder
	if err := tx.Select("user_id").Where("trade_no = ?", tradeNo).First(&reference).Error; err != nil {
		return 0, err
	}
	if err := lockTossBillingOwnerTx(tx, reference.UserId); err != nil {
		return 0, err
	}
	return reference.UserId, nil
}

// collectTossBillingRevocationCandidateTx records only the local key identity.
// Decryption, duplicate-provider-key expansion, and the final live-reference
// decision must happen after the surrounding lifecycle transaction commits in
// RevokeStoredTossBillingKey. Capturing plaintext here allowed older cleanup
// paths to bypass that last-reference gate and delete a provider key still used
// through another local duplicate row.
func collectTossBillingRevocationCandidateTx(_ *gorm.DB, candidates *[]tossBillingRevocationCandidate, seen map[int]struct{}, keyId int, _ string) {
	if keyId <= 0 {
		return
	}
	if _, ok := seen[keyId]; ok {
		return
	}
	seen[keyId] = struct{}{}
	*candidates = append(*candidates, tossBillingRevocationCandidate{keyId: keyId})
}

func revokeTossBillingCandidates(ctx context.Context, candidates []tossBillingRevocationCandidate, reason string) {
	for _, candidate := range candidates {
		if err := RevokeStoredTossBillingKey(ctx, candidate.keyId); err != nil {
			common.SysError(fmt.Sprintf("failed to delete Toss billing key remotely for %s: billing_key_id=%d error=%v", reason, candidate.keyId, err))
		}
	}
}

// CompleteTossBillingOrder completes a pending Toss subscription order and marks the
// created UserSubscription for auto-renew (billingKeyId). Idempotent on order status.
func CompleteTossBillingOrder(tradeNo string, billingKeyId int, providerPayload string) error {
	return completeTossBillingOrder(context.Background(), tradeNo, billingKeyId, providerPayload, false)
}

func CompleteTossBillingOrderWithContext(ctx context.Context, tradeNo string, billingKeyId int, providerPayload string) error {
	return completeTossBillingOrder(ctx, tradeNo, billingKeyId, providerPayload, true)
}

func runTossSettlementTransaction(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	maxAttempts := 1
	if common.UsingSQLite {
		maxAttempts = 12
	}
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = db.Transaction(fn)
		if err == nil || !common.UsingSQLite ||
			(!strings.Contains(strings.ToLower(err.Error()), "database is locked") && !strings.Contains(strings.ToUpper(err.Error()), "SQLITE_BUSY")) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

func completeTossBillingOrder(ctx context.Context, tradeNo string, billingKeyId int, providerPayload string, requestScoped bool) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	if strings.HasPrefix(strings.TrimSpace(tradeNo), TossRenewalTradeNoPrefix) || HasTossRenewalOpaqueOrderIDPrefix(tradeNo) {
		return fmt.Errorf("%w: renewal order cannot use initial settlement", ErrSubscriptionOrderStatusInvalid)
	}
	var logUserId int
	var upgradeGroup string
	cancellationBlocked := false
	err := runTossSettlementTransaction(dbWithContext(ctx), func(tx *gorm.DB) error {
		cancellationBlocked = false
		billingUser, err := lockTossSubscriptionOrderUserTx(tx, tradeNo)
		if err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		_, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(&order)
		if identityErr != nil {
			return identityErr
		}
		if isRenewalOrder {
			return fmt.Errorf("%w: renewal order cannot use initial settlement", ErrSubscriptionOrderStatusInvalid)
		}
		if order.Status != common.TopUpStatusPending {
			return errTossSettlementClaimLost
		}
		if billingUser.Id != order.UserId || billingUser.Status != common.UserStatusEnabled || billingUser.OrganizationId > 0 {
			return errors.New("Toss subscription user is not active")
		}
		effectiveBillingKeyId := billingKeyId
		if effectiveBillingKeyId <= 0 {
			effectiveBillingKeyId = order.BillingKeyId
		}
		if effectiveBillingKeyId <= 0 {
			return errors.New("billingKeyId is invalid")
		}
		canceled, cancellationErr := hasAuthoritativeTossCancellationForOrderTx(tx, tradeNo)
		if cancellationErr != nil {
			return cancellationErr
		}
		if canceled {
			updates := map[string]interface{}{
				"status":              common.TopUpStatusFailed,
				"complete_time":       getDBTimestampTx(tx),
				"billing_claim_token": "",
				"billing_claim_time":  0,
				"billing_key_id":      effectiveBillingKeyId,
			}
			if providerPayload != "" {
				updates["provider_payload"] = providerPayload
			}
			closed := tx.Model(&SubscriptionOrder{}).
				Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
				Updates(updates)
			if closed.Error != nil {
				return closed.Error
			}
			if closed.RowsAffected != 1 {
				return errTossSettlementClaimLost
			}
			if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, effectiveBillingKeyId); err != nil {
				return err
			}
			cancellationBlocked = true
			return nil
		}
		updates := map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": common.GetTimestamp(),
		}
		if billingKeyId > 0 {
			updates["billing_key_id"] = billingKeyId
		}
		if providerPayload != "" {
			updates["provider_payload"] = providerPayload
		}
		claim := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND payment_provider = ? AND status = ?",
				order.Id, PaymentProviderToss, common.TopUpStatusPending)
		if billingKeyId <= 0 {
			claim = claim.Where("billing_key_id > 0")
		}
		result := claim.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errTossSettlementClaimLost
		}

		order.Status = common.TopUpStatusSuccess
		order.BillingKeyId = effectiveBillingKeyId
		plan, err := resolveSubscriptionOrderPlanTx(tx, &order)
		if err != nil {
			return err
		}
		var billingKey UserBillingKey
		keyErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "user_id", "status").Where("id = ?", effectiveBillingKeyId).First(&billingKey).Error
		if keyErr != nil && !errors.Is(keyErr, gorm.ErrRecordNotFound) {
			return keyErr
		}
		billingKeyActive := keyErr == nil && billingKey.UserId == order.UserId && billingKey.Status == BillingKeyStatusActive
		// Legacy initial orders may predate immutable plan snapshots. Fulfill an
		// already-paid period exactly once, but never invent renewal terms from the
		// mutable current plan.
		hasRenewalContract := strings.TrimSpace(order.PlanSnapshot) != ""
		autoRenewAllowed := hasRenewalContract && ValidateTossSubscriptionBillingPlan(plan) == nil && billingKeyActive
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		sub, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		// Mark for auto-renew: charge a lead time before the current period ends so the
		// renewal cron extends EndTime before ExpireDueSubscriptions can expire it.
		sub.AutoRenew = autoRenewAllowed
		sub.BillingKeyId = effectiveBillingKeyId
		if autoRenewAllowed {
			sub.NextBillingTime = tossNextBillingTime(sub.StartTime, sub.EndTime)
		} else {
			sub.NextBillingTime = 0
		}
		sub.BillingFailCount = 0
		sub.UpdatedAt = common.GetTimestamp()
		if hasRenewalContract {
			if err := SetTossRenewalContractFromInitialOrder(sub, &order); err != nil {
				return err
			}
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		if !autoRenewAllowed {
			// A payment created by an older deployment is still fulfilled exactly
			// once, but an unsafe short cadence is never scheduled again.
			if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, effectiveBillingKeyId); err != nil {
				return err
			}
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		logUserId = order.UserId
		return nil
	})
	if cancellationBlocked && err == nil {
		return ErrTossCancellationPrecedesFulfillment
	}
	if errors.Is(err, errTossSettlementClaimLost) {
		order, lookupErr := GetSubscriptionOrderByTradeNoWithErrorContext(ctx, tradeNo)
		switch {
		case lookupErr != nil:
			return lookupErr
		case order.PaymentProvider != PaymentProviderToss:
			return ErrPaymentMethodMismatch
		case order.Status == common.TopUpStatusSuccess:
			return nil
		case order.Status == common.TopUpStatusPending && billingKeyId <= 0 && order.BillingKeyId <= 0:
			return errors.New("billingKeyId is invalid")
		default:
			return ErrSubscriptionOrderStatusInvalid
		}
	}
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, upgradeGroup)
	}
	if logUserId > 0 {
		if requestScoped {
			RecordLogWithContext(ctx, logUserId, LogTypeTopup, "Toss 자동결제 구독 시작")
		} else {
			RecordLog(logUserId, LogTypeTopup, "Toss 자동결제 구독 시작")
		}
	}
	return nil
}

// RenewTossSubscription extends a subscription for another period after a successful
// recurring billing charge. A renewal settled before expiry extends the existing EndTime
// without drift; an overdue renewal starts a complete new period at settlement time so a
// short custom plan cannot be charged repeatedly just to catch up. It resets quota usage,
// records an audit order, and clears fail count. If cancellation or user
// disablement landed after the immutable provider POST intent, the paid period is still
// granted exactly once while auto-renew stays disabled and NextBillingTime stays zero.
func RenewTossSubscription(subId int, tradeNo string, money float64, providerAmount int64, providerPayload ...string) error {
	return RenewTossSubscriptionWithContext(context.Background(), subId, tradeNo, money, providerAmount, providerPayload...)
}

func RenewTossSubscriptionWithContext(ctx context.Context, subId int, tradeNo string, money float64, providerAmount int64, providerPayload ...string) error {
	if subId <= 0 {
		return errors.New("subId is invalid")
	}
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	renewalSubID, renewalBillingTime, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentityByTradeNoWithContext(ctx, tradeNo)
	if identityErr != nil {
		return identityErr
	}
	if !isRenewalOrder || renewalSubID != subId {
		return fmt.Errorf("%w: invalid renewal order id", ErrSubscriptionOrderStatusInvalid)
	}
	payload := ""
	if len(providerPayload) > 0 {
		payload = providerPayload[0]
	}
	var renewedUserId int
	var renewedUserGroup string
	cancellationBlocked := false
	var contractBlockedErr error

	settle := func(tx *gorm.DB) error {
		contractBlockedErr = nil
		// Financial lifecycle timestamps and the overdue renewal base must share
		// the database clock; application-node skew must not grant a shorter or
		// longer paid period.
		now := getDBTimestampTx(tx)
		// Membership/account transitions take the user lock before touching
		// subscriptions or orders. Match that order so a successful provider
		// result is either fulfilled before the transition, or observed afterward
		// with auto-renew disabled; it can never deadlock behind the inverse order.
		var subReference UserSubscription
		if err := tx.Select("user_id").Where("id = ?", subId).First(&subReference).Error; err != nil {
			return err
		}
		var billingUser User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "organization_id").
			Where("id = ?", subReference.UserId).First(&billingUser).Error; err != nil {
			return err
		}
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", subId).First(&sub).Error; err != nil {
			return err
		}
		orderUpdates := map[string]interface{}{
			"status":              common.TopUpStatusSuccess,
			"complete_time":       now,
			"billing_claim_token": "",
			"billing_claim_time":  0,
			"provider_currency": gorm.Expr(
				"CASE WHEN provider_currency IS NULL OR provider_currency = '' THEN ? ELSE provider_currency END", "KRW"),
		}
		if providerAmount > 0 {
			orderUpdates["provider_amount"] = gorm.Expr(
				"CASE WHEN provider_amount <= 0 THEN ? ELSE provider_amount END", providerAmount)
		}
		if payload != "" {
			orderUpdates["provider_payload"] = payload
		}

		claim := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending).
			Updates(orderUpdates)
		if claim.Error != nil {
			return claim.Error
		}

		var order SubscriptionOrder
		if claim.RowsAffected == 0 {
			if err := tx.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrSubscriptionOrderStatusInvalid
				}
				return err
			}
			return errTossSettlementClaimLost
		}
		if err := tx.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		lockedSubID, lockedBillingTime, _, lockedRenewal, identityErr := ResolveTossRenewalOrderIdentity(&order)
		if identityErr != nil || !lockedRenewal || lockedSubID != renewalSubID || lockedBillingTime != renewalBillingTime {
			return ErrTossRenewalIdentityConflict
		}
		if (money > 0 && !subscriptionPricesEqual(order.Money, money)) ||
			(providerAmount > 0 && order.ProviderAmount != providerAmount) {
			return fmt.Errorf("%w: renewal settlement amount", ErrSubscriptionPlanSnapshotMismatch)
		}
		canceled, cancellationErr := hasAuthoritativeTossCancellationForOrderTx(tx, tradeNo)
		if cancellationErr != nil {
			return cancellationErr
		}
		if canceled {
			if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
				"status":              common.TopUpStatusFailed,
				"complete_time":       now,
				"billing_claim_token": "",
				"billing_claim_time":  0,
			}).Error; err != nil {
				return err
			}
			billingKeyID := order.BillingKeyId
			if billingKeyID <= 0 {
				billingKeyID = sub.BillingKeyId
			}
			if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
				"auto_renew":         false,
				"next_billing_time":  0,
				"billing_retry_time": 0,
				"updated_at":         now,
			}).Error; err != nil {
				return err
			}
			if billingKeyID > 0 {
				if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, billingKeyID); err != nil {
					return err
				}
			}
			cancellationBlocked = true
			return nil
		}

		attemptedSnapshotSettlement := order.BillingAttempted && order.RenewalEndTime > 0 &&
			strings.TrimSpace(order.BillingAttemptCredential) != ""
		cycleErr := error(nil)
		if !attemptedSnapshotSettlement || order.UserId != sub.UserId || order.PlanId != sub.PlanId ||
			order.BillingKeyId <= 0 || order.BillingKeyId != sub.BillingKeyId || order.RenewalEndTime != sub.EndTime {
			cycleErr = fmt.Errorf("%w: paid renewal no longer matches the authorized subscription cycle", ErrSubscriptionPlanSnapshotMismatch)
		} else if sub.AutoRenew && sub.NextBillingTime != renewalBillingTime {
			cycleErr = fmt.Errorf("%w: paid renewal billing cycle changed", ErrSubscriptionPlanSnapshotMismatch)
		}
		if cycleErr == nil {
			_, cycleErr = loadOrBackfillTossRenewalContractTx(tx, &sub)
		}
		if cycleErr == nil {
			cycleErr = ValidateTossRenewalOrderMatchesContract(&order, &sub)
		}
		if cycleErr != nil {
			if !isFatalTossRenewalContractError(cycleErr) {
				return cycleErr
			}
			if err := disableTossRenewalWithoutContractTx(tx, &sub); err != nil {
				return err
			}
			if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
				"status":              common.TopUpStatusFailed,
				"complete_time":       now,
				"billing_claim_token": "",
				"billing_claim_time":  0,
			}).Error; err != nil {
				return err
			}
			contractBlockedErr = cycleErr
			return nil
		}
		if sub.Status != "active" || (!attemptedSnapshotSettlement && !sub.AutoRenew) {
			return errors.New("Toss subscription is no longer renewable")
		}
		if billingUser.Id != sub.UserId {
			return errors.New("Toss subscription user changed")
		}
		if !attemptedSnapshotSettlement && (billingUser.Status != common.UserStatusEnabled || billingUser.OrganizationId > 0) {
			return errors.New("Toss subscription user is not active")
		}
		continueAutoRenew := sub.AutoRenew && billingUser.Status == common.UserStatusEnabled && billingUser.OrganizationId == 0
		if attemptedSnapshotSettlement && continueAutoRenew {
			var billingKey UserBillingKey
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id", "status").Where("id = ?", order.BillingKeyId).First(&billingKey).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				continueAutoRenew = false
			} else if billingKey.Status != BillingKeyStatusActive {
				continueAutoRenew = false
			}
		}
		var (
			plan *SubscriptionPlan
			err  error
		)
		plan, err = resolveSubscriptionOrderPlanTx(tx, &order)
		if err != nil {
			return err
		}
		if planErr := ValidateTossSubscriptionBillingPlan(plan); planErr != nil {
			continueAutoRenew = false
		}
		desiredUpgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
		prevUserGroup := sub.PrevUserGroup
		if desiredUpgradeGroup == "" {
			restoredGroup, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
			if err != nil {
				return err
			}
			if restoredGroup != "" {
				renewedUserId = sub.UserId
				renewedUserGroup = restoredGroup
			}
		} else {
			currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
			if err != nil {
				return err
			}
			if prevUserGroup == "" && (strings.TrimSpace(sub.UpgradeGroup) == "" || currentGroup != strings.TrimSpace(sub.UpgradeGroup)) {
				prevUserGroup = currentGroup
			}
			if currentGroup != desiredUpgradeGroup {
				if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
					Update("group", desiredUpgradeGroup).Error; err != nil {
					return err
				}
				renewedUserId = sub.UserId
				renewedUserGroup = desiredUpgradeGroup
			}
		}
		oldEnd := sub.EndTime
		renewalBase := oldEnd
		if renewalBase < now {
			renewalBase = now
		}
		newEnd, err := calcPlanEndTime(time.Unix(renewalBase, 0), plan)
		if err != nil {
			return err
		}
		nextBillingTime := int64(0)
		if continueAutoRenew {
			nextBillingTime = tossNextBillingTime(renewalBase, newEnd)
		}
		nextResetTime := calcNextResetTime(time.Unix(now, 0), plan, newEnd)
		lastResetTime := int64(0)
		if nextResetTime > 0 {
			lastResetTime = now
		}
		subUpdate := tx.Model(&UserSubscription{}).
			Where("id = ? AND end_time = ? AND status = ?", subId, oldEnd, "active")
		if !attemptedSnapshotSettlement {
			subUpdate = subUpdate.Where("auto_renew = ?", true)
		}
		subUpdate = subUpdate.Updates(map[string]interface{}{
			"end_time":           newEnd,
			"auto_renew":         continueAutoRenew,
			"next_billing_time":  nextBillingTime,
			"billing_retry_time": 0,
			"amount_total":       plan.TotalAmount,
			"amount_used":        0,
			"last_reset_time":    lastResetTime,
			"next_reset_time":    nextResetTime,
			"status":             "active",
			"billing_fail_count": 0,
			"upgrade_group":      desiredUpgradeGroup,
			"prev_user_group":    prevUserGroup,
			"updated_at":         now,
		})
		if subUpdate.Error != nil {
			return subUpdate.Error
		}
		if subUpdate.RowsAffected == 0 {
			// A different renewal advanced the same subscription. Roll back this
			// order claim and recompute from the newly committed EndTime.
			return errTossSubscriptionCASRetry
		}

		if order.BillingKeyId <= 0 {
			order.BillingKeyId = sub.BillingKeyId
		}
		if attemptedSnapshotSettlement && !continueAutoRenew && order.BillingKeyId > 0 {
			// A cancellation or disabled user must never schedule another charge.
			// Keep the already-attempted DONE payment's entitlement, while leaving
			// the key queued for deletion. Already-revoked keys remain revoked.
			if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, order.BillingKeyId); err != nil {
				return err
			}
		}
		if strings.TrimSpace(order.ProviderCredential) == "" && sub.BillingKeyId > 0 {
			var key UserBillingKey
			if err := tx.Select("provider_credential").Where("id = ?", sub.BillingKeyId).First(&key).Error; err == nil {
				order.ProviderCredential = key.ProviderCredential
			}
		}
		return tx.Save(&order).Error
	}

	var err error
	for attempt := 0; attempt < 5; attempt++ {
		cancellationBlocked = false
		err = runTossSettlementTransaction(dbWithContext(ctx), settle)
		if !errors.Is(err, errTossSubscriptionCASRetry) {
			break
		}
	}
	if errors.Is(err, errTossSettlementClaimLost) {
		order, lookupErr := GetSubscriptionOrderByTradeNoWithErrorContext(ctx, tradeNo)
		switch {
		case lookupErr != nil:
			return lookupErr
		case order.PaymentProvider != PaymentProviderToss:
			return ErrPaymentMethodMismatch
		case order.Status == common.TopUpStatusSuccess:
			return nil
		default:
			return ErrSubscriptionOrderStatusInvalid
		}
	}
	if err != nil {
		return err
	}
	if contractBlockedErr != nil {
		return contractBlockedErr
	}
	if cancellationBlocked {
		return ErrTossCancellationPrecedesFulfillment
	}
	if renewedUserId > 0 && renewedUserGroup != "" {
		_ = UpdateUserGroupCache(renewedUserId, renewedUserGroup)
	}
	return nil
}

// MarkTossBillingFailure increments the fail counter and disables auto-renew after maxFails.
// It returns true when this call disables auto-renew.
func MarkTossBillingFailure(subId int, maxFails int) (bool, error) {
	disabled := false
	err := runTossSettlementTransaction(DB, func(tx *gorm.DB) error {
		disabled = false
		if _, err := lockTossSubscriptionOwnerTx(tx, subId); err != nil {
			return err
		}
		var err error
		disabled, err = markTossBillingFailureTx(tx, subId, maxFails)
		return err
	})
	return disabled, err
}

func markTossBillingFailureTx(tx *gorm.DB, subId int, maxFails int) (bool, error) {
	if maxFails <= 0 {
		maxFails = 1
	}
	now := getDBTimestampTx(tx)
	increment := tx.Model(&UserSubscription{}).
		Where("id = ?", subId).
		Updates(map[string]interface{}{
			"billing_fail_count": gorm.Expr("billing_fail_count + ?", 1),
			"updated_at":         now,
		})
	if increment.Error != nil {
		return false, increment.Error
	}
	if increment.RowsAffected == 0 {
		return false, gorm.ErrRecordNotFound
	}

	var sub UserSubscription
	if err := tx.Where("id = ?", subId).First(&sub).Error; err != nil {
		return false, err
	}
	if sub.BillingFailCount < maxFails {
		return false, nil
	}

	disable := tx.Model(&UserSubscription{}).
		Where("id = ? AND auto_renew = ? AND billing_fail_count >= ?", subId, true, maxFails).
		Updates(map[string]interface{}{
			"auto_renew": false,
			"updated_at": now,
		})
	if disable.Error != nil {
		return false, disable.Error
	}
	if sub.BillingKeyId > 0 {
		if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, sub.BillingKeyId); err != nil {
			return false, err
		}
	}
	return disable.RowsAffected > 0, nil
}

const tossRenewalQueueRetryDelaySeconds int64 = 2 * 60

// GetDueTossRenewals reserves active auto-renew Toss subscriptions due for
// charge. The retry timestamp is both a short cross-node lease and the queue's
// fairness cursor: even if processing returns before it can classify a broken
// row, the next bounded run can reach later due subscriptions.
func GetDueTossRenewals(now int64, limit int) ([]UserSubscription, error) {
	if now <= 0 {
		now = GetDBTimestamp()
	}
	if limit <= 0 {
		limit = 100
	}
	staleCutoff := now - TossBillingOperationalGraceSeconds
	var candidates []UserSubscription
	if err := DB.Where("auto_renew = ? AND billing_key_id > 0 AND next_billing_time > 0 AND next_billing_time <= ? AND status = ?",
		true, now, "active").
		Where("end_time > ?", staleCutoff).
		Where("billing_retry_time = 0 OR billing_retry_time IS NULL OR billing_retry_time <= ?", now).
		Order("CASE WHEN billing_retry_time IS NULL OR billing_retry_time <= 0 THEN next_billing_time ELSE billing_retry_time END asc, next_billing_time asc, id asc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	nextRetry := now + tossRenewalQueueRetryDelaySeconds
	reserved := make([]UserSubscription, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		result := DB.Model(&UserSubscription{}).
			Where("id = ? AND auto_renew = ? AND billing_key_id > 0 AND next_billing_time = ? AND next_billing_time <= ? AND status = ?",
				candidate.Id, true, candidate.NextBillingTime, now, "active").
			Where("end_time > ?", staleCutoff).
			Where("billing_retry_time = ? OR (billing_retry_time IS NULL AND ? = 0)", candidate.BillingRetryTime, candidate.BillingRetryTime).
			Update("billing_retry_time", nextRetry)
		if result.Error != nil {
			return reserved, result.Error
		}
		if result.RowsAffected != 1 {
			continue
		}
		candidate.BillingRetryTime = nextRetry
		reserved = append(reserved, *candidate)
	}
	return reserved, nil
}

// CancelTossAutoRenewForUser disables auto-renew on the user's active Toss
// subscriptions. A provider key is deleted only when no other subscription,
// wallet policy, or pending charge still references it. The current period
// stays until EndTime.
func CancelTossAutoRenewForUser(ctx context.Context, userId int) error {
	remoteKeys := make([]tossBillingRevocationCandidate, 0)
	seenRemoteKeys := make(map[int]struct{})
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTossBillingOwnerTx(tx, userId); err != nil {
			return err
		}
		var subs []UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND auto_renew = ?", userId, commonTrueVal).
			Find(&subs).Error; err != nil {
			return err
		}
		for i := range subs {
			subs[i].AutoRenew = false
			subs[i].NextBillingTime = 0
			subs[i].BillingRetryTime = 0
			subs[i].UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&subs[i]).Error; err != nil {
				return err
			}
			if subs[i].BillingKeyId > 0 {
				queued, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, subs[i].BillingKeyId)
				if err != nil {
					return err
				}
				if queued {
					collectTossBillingRevocationCandidateTx(tx, &remoteKeys, seenRemoteKeys, subs[i].BillingKeyId, "user cancellation")
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	revokeTossBillingCandidates(ctx, remoteKeys, fmt.Sprintf("user cancellation user_id=%d", userId))
	return nil
}

// Update subscription used amount by delta (positive consume more, negative refund).
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userSubscriptionId).
			First(&sub).Error; err != nil {
			return err
		}
		newUsed := sub.AmountUsed + delta
		if newUsed < 0 {
			newUsed = 0
		}
		if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
		}
		sub.AmountUsed = newUsed
		return tx.Save(&sub).Error
	})
}
