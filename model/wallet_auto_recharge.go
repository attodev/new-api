package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WalletAutoRechargeTypeScheduled = "scheduled"
	WalletAutoRechargeTypeThreshold = "threshold"
)

const (
	WalletAutoRechargeStatusPending       = "pending"
	WalletAutoRechargeStatusActive        = "active"
	WalletAutoRechargeStatusCancelPending = "cancel_pending"
	WalletAutoRechargeStatusCancelled     = "cancelled"
	WalletAutoRechargeStatusFailed        = "failed"
)

const (
	WalletAutoRechargeIntervalMonth  = "month"
	WalletAutoRechargeIntervalDay    = "day"
	WalletAutoRechargeIntervalCustom = "custom"
)

const (
	WalletAutoRechargeThresholdCooldownSeconds = 3600
	WalletAutoRechargeDailyLimit               = 3
	WalletAutoRechargeMinimumCustomSeconds     = 60
	WalletAutoRechargeMaximumCustomSeconds     = 365 * 24 * 60 * 60
	WalletAutoRechargeMaximumDayInterval       = 3650
	WalletAutoRechargeMaximumMonthInterval     = 120
	WalletAutoRechargeOrderIDMinBytes          = 6
	WalletAutoRechargeOrderIDMaxBytes          = 64

	walletAutoRechargeProviderPendingPrefix              = "wallet auto recharge provider pending: "
	walletAutoRechargeReconciliationPendingPrefix        = "wallet auto recharge reconciliation pending: "
	walletAutoRechargeManualReconciliationPrefix         = "wallet auto recharge manual reconciliation required: "
	walletAutoRechargeLateDONEFencePrefix                = "wallet auto recharge late DONE replacement fence: "
	walletAutoRechargeEventResolutionPendingPrefix       = "wallet auto recharge reconciliation pending: fulfillment event resolution: "
	walletAutoRechargeDoneMismatchPrefix                 = "wallet auto recharge reconciliation pending: provider DONE payload mismatch"
	walletAutoRechargeIssueClaimTTLSeconds         int64 = 5 * 60
	walletAutoRechargeChargeClaimTTLSeconds        int64 = 5 * 60
	walletAutoRechargeQueueRetryDelaySeconds       int64 = 2 * 60
)

var (
	ErrWalletAutoRechargeIssueConflict              = errors.New("wallet auto recharge billing issue snapshot conflicts with the existing authorization")
	ErrWalletAutoRechargeIssueAuthorizationExpired  = errors.New("wallet auto recharge billing issue authorization expired")
	ErrWalletAutoRechargeClaimLost                  = errors.New("wallet auto recharge billing issue claim lost")
	ErrWalletAutoRechargeChargeClaimLost            = errors.New("wallet auto recharge charge claim lost")
	ErrWalletAutoRechargeStatusInvalid              = errors.New("wallet auto recharge status invalid")
	ErrWalletAutoRechargeCancelled                  = errors.New("wallet auto recharge already cancelled")
	ErrWalletAutoRechargeTargetInvalid              = errors.New("wallet auto recharge target is no longer eligible")
	ErrWalletAutoRechargeReconciliationRequired     = errors.New("wallet auto recharge provider result requires reconciliation")
	ErrWalletAutoRechargeAttemptOutsideGrace        = errors.New("wallet auto recharge attempt is outside the operational grace period")
	ErrWalletAutoRechargeAttemptAssociationConflict = errors.New("wallet auto recharge logical attempt identity conflicts with persisted top-ups")
)

type WalletAutoRecharge struct {
	Id                 int     `json:"id"`
	PresetId           int     `json:"preset_id" gorm:"index"`
	Type               string  `json:"type" gorm:"type:varchar(32);index"`
	TargetType         string  `json:"target_type" gorm:"type:varchar(32);index"`
	TargetId           int     `json:"target_id" gorm:"index"`
	ActiveKey          *string `json:"-" gorm:"type:varchar(128);uniqueIndex"`
	OwnerUserId        int     `json:"owner_user_id" gorm:"index"`
	BillingKeyId       int     `json:"billing_key_id" gorm:"index"`
	Amount             float64 `json:"amount"`
	ThresholdAmount    float64 `json:"threshold_amount"`
	ThresholdQuota     int64   `json:"threshold_quota"`
	IntervalUnit       string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue      int     `json:"interval_value"`
	CustomSeconds      int64   `json:"custom_seconds"`
	ChargeImmediately  bool    `json:"charge_immediately"`
	NextChargeTime     int64   `json:"next_charge_time" gorm:"index"`
	LastChargeTime     int64   `json:"last_charge_time"`
	CooldownUntil      int64   `json:"cooldown_until" gorm:"index"`
	DailyChargeCount   int     `json:"daily_charge_count"`
	DailyChargeDate    string  `json:"daily_charge_date" gorm:"type:varchar(10)"`
	DailyChargeDateUTC bool    `json:"-" gorm:"default:false"`
	Status             string  `json:"status" gorm:"type:varchar(16);index"`
	FailCount          int     `json:"fail_count"`
	LastError          string  `json:"last_error" gorm:"type:varchar(255)"`
	CardCompany        string  `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked   string  `json:"card_number_masked" gorm:"type:varchar(32)"`
	LastTradeNo        string  `json:"last_trade_no" gorm:"type:varchar(64);index"`
	// SettlementRetryTime fairly leases provider/local settlement recovery.
	// It is separate from UpdateTime, which is also used for threshold-policy
	// scheduling and therefore cannot safely be repurposed as a retry cursor.
	SettlementRetryTime   int64  `json:"-" gorm:"default:0;index"`
	CustomerKey           string `json:"-" gorm:"type:varchar(64);index"`
	AuthTradeNo           string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	ProviderCredential    string `json:"-" gorm:"type:text"`
	ProviderClientKeyHash string `json:"-" gorm:"type:varchar(64);default:''"`
	IssueAuthKey          string `json:"-" gorm:"type:text"`
	IssueAuthKeyHash      string `json:"-" gorm:"type:varchar(64);default:''"`
	IssueCustomerKey      string `json:"-" gorm:"type:varchar(64);default:''"`
	IssueClaimToken       string `json:"-" gorm:"type:varchar(64);default:''"`
	IssueClaimTime        int64  `json:"-" gorm:"default:0;index"`
	IssueRetryTime        int64  `json:"-" gorm:"default:0;index"`
	IssueAttempted        bool   `json:"-" gorm:"default:false"`
	CreateTime            int64  `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime            int64  `json:"update_time" gorm:"autoUpdateTime"`
}

type CreateWalletAutoRechargeRequest struct {
	PresetId              int
	Type                  string
	TargetType            string
	TargetId              int
	OwnerUserId           int
	CustomerKey           string
	AuthTradeNo           string
	ProviderCredential    string
	ProviderClientKeyHash string
	Amount                float64
	ThresholdAmount       float64
	ThresholdQuota        int64
	IntervalUnit          string
	IntervalValue         int
	CustomSeconds         int64
	ChargeImmediately     bool
}

type walletAutoRechargeCharge struct {
	policy         WalletAutoRecharge
	billingKey     string
	customerKey    string
	secretKeys     []string
	tradeNo        string
	chargeKRW      int64
	preparationErr error
	alreadyDone    bool
	lookupPending  bool
	stalePending   bool
	postForbidden  bool
	// cancellationNeedsGrace distinguishes a local cancellation intent from an
	// authoritative provider cancellation event. Both forbid another POST, but
	// a local intent cannot treat an in-window GET 404 as proof that an older
	// provider request never reached Toss.
	cancellationNeedsGrace bool
	shouldCharge           bool
}

type tossWalletAutoRechargeChargeContextKey struct{}
type tossBillingIssueContextKey struct{}

const (
	tossBillingIssueContextWallet  = "wallet"
	tossBillingIssueContextCleanup = "cleanup"
)

// WithTossWalletAutoRechargeChargeContext marks the shared billing HTTP
// adapter invocation as a wallet auto-recharge POST. The marker is independent
// of the provider-facing orderId, so a future opaque ID cannot bypass the
// wallet-specific operational kill switch.
func WithTossWalletAutoRechargeChargeContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tossWalletAutoRechargeChargeContextKey{}, true)
}

// WithTossWalletAutoRechargeIssueContext marks a new billing-key ISSUE request
// as belonging to the separately gated wallet contract. Keep ISSUE metadata
// separate from charge metadata: cleanup replays need a different authorization
// rule even though all three operations share controller HTTP adapters.
func WithTossWalletAutoRechargeIssueContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tossBillingIssueContextKey{}, tossBillingIssueContextWallet)
}

// WithTossBillingIssueCleanupContext authorizes only an exact replay of an
// already-attempted billing-key ISSUE for orphan-key cleanup. It deliberately
// bypasses product enable flags so disabling payments cannot strand a provider
// credential, while the HTTP adapter still requires a fresh configuration and
// the caller-provided secret/idempotency key.
func WithTossBillingIssueCleanupContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tossBillingIssueContextKey{}, tossBillingIssueContextCleanup)
}

func IsTossWalletAutoRechargeIssueContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	mode, _ := ctx.Value(tossBillingIssueContextKey{}).(string)
	return mode == tossBillingIssueContextWallet
}

func IsTossBillingIssueCleanupContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	mode, _ := ctx.Value(tossBillingIssueContextKey{}).(string)
	return mode == tossBillingIssueContextCleanup
}

func IsTossWalletAutoRechargeChargeContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	marked, _ := ctx.Value(tossWalletAutoRechargeChargeContextKey{}).(bool)
	return marked
}

func walletAutoRechargeAttemptOutsideGrace(policy *WalletAutoRecharge, createTime, now int64) bool {
	if createTime <= 0 || createTime <= now-TossBillingOperationalGraceSeconds {
		return true
	}
	// A scheduled charge is authorized by its original due time, not by the
	// later time at which a worker happened to persist the TopUp. Otherwise a
	// task arriving just before the 24-hour boundary could silently open a
	// second full replay window.
	return policy != nil && policy.Type == WalletAutoRechargeTypeScheduled && policy.NextChargeTime > 0 &&
		policy.NextChargeTime <= now-TossBillingOperationalGraceSeconds
}

// TossBillingPaymentLookup resolves an ambiguous wallet auto-recharge attempt
// by its durable Toss orderId and the secret that was active for that attempt.
// It is injected by controller to avoid a model -> controller import cycle.
type TossBillingPaymentLookup func(ctx context.Context, secretKeys []string, orderId string, amount int64) (*TossBillingChargeResult, error)

var walletAutoRechargeTossPaymentLookup TossBillingPaymentLookup

func SetWalletAutoRechargeTossPaymentLookup(fn TossBillingPaymentLookup) {
	walletAutoRechargeTossPaymentLookup = fn
}

// WalletAutoRechargeIssueReconciler retries a durable billing-key issue
// snapshot without creating a new authorization or changing provider namespace.
type WalletAutoRechargeIssueReconciler func(ctx context.Context, policy WalletAutoRecharge) (resolved bool, err error)

var walletAutoRechargeIssueReconciler WalletAutoRechargeIssueReconciler

func SetWalletAutoRechargeIssueReconciler(fn WalletAutoRechargeIssueReconciler) {
	walletAutoRechargeIssueReconciler = fn
}

func walletAutoRechargeMinimumKRW() int64 {
	return int64(setting.TossEffectiveMinTopUp())
}

func isValidWalletAutoRechargeOrderID(orderID string) bool {
	if len(orderID) < WalletAutoRechargeOrderIDMinBytes || len(orderID) > WalletAutoRechargeOrderIDMaxBytes {
		return false
	}
	for i := 0; i < len(orderID); i++ {
		ch := orderID[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func validateWalletAutoRechargeInterval(unit string, value int, customSeconds int64) error {
	switch unit {
	case WalletAutoRechargeIntervalMonth:
		if value <= 0 || value > WalletAutoRechargeMaximumMonthInterval {
			return fmt.Errorf("wallet auto recharge month interval must be between 1 and %d", WalletAutoRechargeMaximumMonthInterval)
		}
	case WalletAutoRechargeIntervalDay:
		if value <= 0 || value > WalletAutoRechargeMaximumDayInterval {
			return fmt.Errorf("wallet auto recharge day interval must be between 1 and %d", WalletAutoRechargeMaximumDayInterval)
		}
	case WalletAutoRechargeIntervalCustom:
		if customSeconds < WalletAutoRechargeMinimumCustomSeconds || customSeconds > WalletAutoRechargeMaximumCustomSeconds {
			return fmt.Errorf(
				"wallet auto recharge custom seconds must be between %d and %d",
				WalletAutoRechargeMinimumCustomSeconds,
				WalletAutoRechargeMaximumCustomSeconds,
			)
		}
	default:
		return errors.New("wallet auto recharge interval is required")
	}
	return nil
}

func validateWalletAutoRechargeIntervalForCurrentMode(unit string, value int, customSeconds int64) error {
	return validateWalletAutoRechargeIntervalForMode(unit, value, customSeconds, setting.GetTossConfigSnapshot().TestMode)
}

func validateWalletAutoRechargeIntervalForMode(unit string, value int, customSeconds int64, testMode bool) error {
	if err := validateWalletAutoRechargeInterval(unit, value, customSeconds); err != nil {
		return err
	}
	if unit == WalletAutoRechargeIntervalCustom && !testMode {
		return errors.New("wallet auto recharge custom interval is available only in Toss test mode")
	}
	return nil
}

func (req CreateWalletAutoRechargeRequest) normalizeAndValidate() (CreateWalletAutoRechargeRequest, error) {
	return req.normalizeAndValidateForMode(setting.GetTossConfigSnapshot().TestMode)
}

func (req CreateWalletAutoRechargeRequest) normalizeAndValidateForMode(testMode bool) (CreateWalletAutoRechargeRequest, error) {
	req.AuthTradeNo = strings.TrimSpace(req.AuthTradeNo)
	if !isValidWalletAutoRechargeOrderID(req.AuthTradeNo) {
		return req, errors.New("invalid wallet auto recharge Toss order ID")
	}
	if req.Type != WalletAutoRechargeTypeScheduled && req.Type != WalletAutoRechargeTypeThreshold {
		return req, errors.New("invalid wallet auto recharge type")
	}
	if req.TargetType != TopUpTargetTypeUser && req.TargetType != TopUpTargetTypeOrganization {
		return req, errors.New("invalid wallet auto recharge target")
	}
	if req.TargetId <= 0 || req.OwnerUserId <= 0 {
		return req, errors.New("invalid wallet auto recharge owner or target")
	}
	if (strings.TrimSpace(req.ProviderCredential) == "") != (strings.TrimSpace(req.ProviderClientKeyHash) == "") {
		return req, errors.New("wallet auto recharge provider credential snapshot is incomplete")
	}
	if req.ProviderClientKeyHash != "" && !IsValidTossClientKeyFingerprint(req.ProviderClientKeyHash) {
		return req, errors.New("wallet auto recharge provider client-key fingerprint is invalid")
	}
	if req.Amount <= 0 {
		return req, errors.New("wallet auto recharge amount must be positive")
	}
	chargeKRW := walletAutoRechargeKRW(req.Amount)
	if chargeKRW < walletAutoRechargeMinimumKRW() || chargeKRW > setting.TossMaximumChargeAmountKRW {
		return req, errors.New("wallet auto recharge amount is outside Toss limits")
	}
	if walletAutoRechargeQuota(req.Amount) <= 0 || walletAutoRechargeMoney(req.Amount) <= 0 {
		return req, errors.New("wallet auto recharge quota conversion is invalid")
	}
	switch req.Type {
	case WalletAutoRechargeTypeScheduled:
		if req.IntervalUnit == WalletAutoRechargeIntervalMonth {
			req.ChargeImmediately = false
		}
		if req.IntervalUnit == WalletAutoRechargeIntervalCustom && req.IntervalValue <= 0 {
			req.IntervalValue = 1
		}
		if err := validateWalletAutoRechargeIntervalForMode(req.IntervalUnit, req.IntervalValue, req.CustomSeconds, testMode); err != nil {
			return req, err
		}
	case WalletAutoRechargeTypeThreshold:
		req.ChargeImmediately = false
		if req.ThresholdQuota < 0 {
			return req, errors.New("wallet auto recharge threshold quota cannot be negative")
		}
	}
	return req, nil
}

func buildPendingWalletAutoRecharge(req CreateWalletAutoRechargeRequest) (*WalletAutoRecharge, error) {
	return buildPendingWalletAutoRechargeForMode(req, setting.GetTossConfigSnapshot().TestMode)
}

func buildPendingWalletAutoRechargeForMode(req CreateWalletAutoRechargeRequest, testMode bool) (*WalletAutoRecharge, error) {
	req, err := req.normalizeAndValidateForMode(testMode)
	if err != nil {
		return nil, err
	}

	now := time.Unix(GetDBTimestamp(), 0).UTC()
	activeKey := walletAutoRechargeActiveKey(req.TargetType, req.TargetId, req.Type)
	policy := &WalletAutoRecharge{
		PresetId:              req.PresetId,
		Type:                  req.Type,
		TargetType:            req.TargetType,
		TargetId:              req.TargetId,
		ActiveKey:             &activeKey,
		OwnerUserId:           req.OwnerUserId,
		Amount:                req.Amount,
		ThresholdAmount:       req.ThresholdAmount,
		ThresholdQuota:        req.ThresholdQuota,
		IntervalUnit:          req.IntervalUnit,
		IntervalValue:         req.IntervalValue,
		CustomSeconds:         req.CustomSeconds,
		ChargeImmediately:     req.ChargeImmediately,
		Status:                WalletAutoRechargeStatusPending,
		CustomerKey:           req.CustomerKey,
		AuthTradeNo:           req.AuthTradeNo,
		ProviderCredential:    req.ProviderCredential,
		ProviderClientKeyHash: req.ProviderClientKeyHash,
		CreateTime:            now.Unix(),
		UpdateTime:            now.Unix(),
	}
	if req.Type == WalletAutoRechargeTypeScheduled {
		nextChargeTime, err := nextWalletChargeTime(now, req.IntervalUnit, req.IntervalValue, req.CustomSeconds)
		if err != nil {
			return nil, err
		}
		policy.NextChargeTime = nextChargeTime.Unix()
	}
	return policy, nil
}

func createPendingWalletAutoRechargeTx(tx *gorm.DB, policy *WalletAutoRecharge) error {
	if policy == nil {
		return errors.New("wallet auto recharge policy is required")
	}
	if err := ensureNoOtherWalletAutoRechargePaymentFenceTx(tx, policy.TargetType, policy.TargetId, 0, ""); err != nil {
		return err
	}
	if err := tx.Create(policy).Error; err != nil {
		if isWalletAutoRechargeActiveKeyConflict(err) {
			return errors.New("active wallet auto recharge already exists")
		}
		return err
	}
	return nil
}

func lockWalletAutoRechargeOwnerTx(tx *gorm.DB, ownerUserID int) error {
	if tx == nil || ownerUserID <= 0 {
		return ErrWalletAutoRechargeTargetInvalid
	}
	var owner User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "status", "role", "organization_id", "organization_role").
		Where("id = ?", ownerUserID).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletAutoRechargeTargetInvalid
		}
		return err
	}
	return nil
}

func lockWalletAutoRechargeOrganizationTx(tx *gorm.DB, targetType string, targetID int) error {
	if targetType != TopUpTargetTypeOrganization {
		return nil
	}
	var organization Organization
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", targetID).First(&organization).Error; err != nil {
		return err
	}
	return nil
}

// lockWalletAutoRechargeFinancialMutationTx establishes the same global lock
// order used by billing lifecycle cleanup: owner first, then organization
// (when applicable), provider-key identity, and policy. Settlement and terminal
// webhook paths can eventually queue the policy's billing key for revocation;
// taking the policy first would otherwise deadlock with BILLING_DELETED, which
// holds owner/key rows before disabling the policy. The explicit key-before-
// policy order also protects late cleanup after the owner or organization row
// has already been deleted and can no longer act as a serialization root.
//
// Deleted owners/organizations no longer need a row lock, but late provider
// evidence must still be allowed to close or reconcile the surviving financial
// rows. This deliberately mirrors lockTossBillingOwnerTx's cleanup semantics.
func lockWalletAutoRechargeFinancialMutationTx(tx *gorm.DB, policyID int) (*WalletAutoRecharge, error) {
	if tx == nil || policyID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var reference WalletAutoRecharge
	if err := tx.Select("owner_user_id", "target_type", "target_id", "billing_key_id").
		Where("id = ?", policyID).First(&reference).Error; err != nil {
		return nil, err
	}
	if err := lockTossBillingOwnerTx(tx, reference.OwnerUserId); err != nil {
		return nil, err
	}
	if reference.TargetType == TopUpTargetTypeOrganization && reference.TargetId > 0 {
		var organization Organization
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ?", reference.TargetId).First(&organization).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if reference.BillingKeyId > 0 && tx.Migrator().HasTable(&UserBillingKey{}) {
		if _, err := getTossBillingKeyIdentityRowsTx(tx, reference.BillingKeyId, true); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	var policy WalletAutoRecharge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", policyID).First(&policy).Error; err != nil {
		return nil, err
	}
	if policy.OwnerUserId != reference.OwnerUserId || policy.TargetType != reference.TargetType || policy.TargetId != reference.TargetId ||
		policy.BillingKeyId != reference.BillingKeyId {
		return nil, ErrWalletAutoRechargeClaimLost
	}
	return &policy, nil
}

func lockAndValidateWalletAutoRechargeOwnerTx(tx *gorm.DB, policy *WalletAutoRecharge) error {
	if policy == nil {
		return ErrWalletAutoRechargeTargetInvalid
	}
	if err := lockWalletAutoRechargeOwnerTx(tx, policy.OwnerUserId); err != nil {
		return err
	}
	if err := lockWalletAutoRechargeOrganizationTx(tx, policy.TargetType, policy.TargetId); err != nil {
		return err
	}
	return validateWalletAutoRechargeChargeTarget(tx, policy)
}

func CreatePendingWalletAutoRecharge(req CreateWalletAutoRechargeRequest) (*WalletAutoRecharge, error) {
	policy, err := buildPendingWalletAutoRecharge(req)
	if err != nil {
		return nil, err
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAndValidateWalletAutoRechargeOwnerTx(tx, policy); err != nil {
			return err
		}
		return createPendingWalletAutoRechargeTx(tx, policy)
	})
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func ClaimWalletAutoRechargeBillingIssue(tradeNo, authKey, customerKey string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	customerKey = strings.TrimSpace(customerKey)
	if tradeNo == "" {
		return "", false, errors.New("wallet auto recharge tradeNo is required")
	}
	if err := validateTossBillingIssueSnapshotInput(authKey, customerKey); err != nil {
		return "", false, err
	}
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return "", false, err
	}
	encryptedAuthKey, err := common.EncryptString(authKey)
	if err != nil {
		return "", false, err
	}
	authKeyHash := common.GenerateHMAC(authKey)
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&WalletAutoRecharge{}).
		Where("auth_trade_no = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL)", tradeNo, WalletAutoRechargeStatusPending).
		Where("(issue_claim_token = '' OR issue_claim_token IS NULL OR issue_claim_time <= ?)", now-walletAutoRechargeIssueClaimTTLSeconds).
		Where("(issue_auth_key_hash = '' OR issue_auth_key_hash IS NULL OR issue_auth_key_hash = ?)", authKeyHash).
		Where("(issue_customer_key = '' OR issue_customer_key IS NULL OR issue_customer_key = ?)", customerKey).
		Updates(map[string]interface{}{
			"issue_auth_key":      encryptedAuthKey,
			"issue_auth_key_hash": authKeyHash,
			"issue_customer_key":  customerKey,
			"issue_claim_token":   token,
			"issue_claim_time":    now,
			"issue_retry_time":    0,
			"update_time":         now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var policy WalletAutoRecharge
	if err := DB.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return "", false, err
	}
	if policy.Status == WalletAutoRechargeStatusActive || policy.BillingKeyId > 0 {
		return "", false, nil
	}
	if policy.Status != WalletAutoRechargeStatusPending {
		return "", false, ErrWalletAutoRechargeStatusInvalid
	}
	if (policy.IssueAuthKeyHash != "" && policy.IssueAuthKeyHash != authKeyHash) ||
		(policy.IssueCustomerKey != "" && policy.IssueCustomerKey != customerKey) {
		return "", false, ErrWalletAutoRechargeIssueConflict
	}
	return "", false, nil
}

func ClaimStoredWalletAutoRechargeBillingIssue(tradeNo string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("wallet auto recharge tradeNo is required")
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&WalletAutoRecharge{}).
		Where("auth_trade_no = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_auth_key <> ''", tradeNo,
			[]string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusCancelPending}).
		Where("(issue_claim_token = '' OR issue_claim_token IS NULL OR issue_claim_time <= ?)", now-walletAutoRechargeIssueClaimTTLSeconds).
		Updates(map[string]interface{}{
			"issue_claim_token": token,
			"issue_claim_time":  now,
			"update_time":       now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var policy WalletAutoRecharge
	if err := DB.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return "", false, err
	}
	if policy.Status == WalletAutoRechargeStatusActive || policy.BillingKeyId > 0 {
		return "", false, nil
	}
	if policy.Status != WalletAutoRechargeStatusPending && policy.Status != WalletAutoRechargeStatusCancelPending {
		return "", false, ErrWalletAutoRechargeStatusInvalid
	}
	return "", false, nil
}

func ReleaseWalletAutoRechargeBillingIssueClaim(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return nil
	}
	return DB.Model(&WalletAutoRecharge{}).
		Where("auth_trade_no = ? AND issue_claim_token = ?", tradeNo, token).
		Updates(map[string]interface{}{
			"issue_claim_token": "",
			"issue_claim_time":  0,
			"update_time":       GetDBTimestamp(),
		}).Error
}

// GetClaimedWalletAutoRechargeBillingIssueState reloads the authoritative row
// after claim acquisition. Attempt and cleanup decisions must use this state,
// not a candidate loaded before another node's MarkAttempted/Release cycle.
func GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, token string) (*WalletAutoRecharge, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return nil, ErrWalletAutoRechargeClaimLost
	}
	var policy WalletAutoRecharge
	if err := DB.Where("auth_trade_no = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_claim_token = ?",
		tradeNo, []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusCancelPending}, token).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWalletAutoRechargeClaimLost
		}
		return nil, err
	}
	return &policy, nil
}

func GetClaimedWalletAutoRechargeBillingIssue(tradeNo, token string) (authKey, customerKey, secretKey string, err error) {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return "", "", "", err
	}
	policy, err := GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, token)
	if err != nil {
		return "", "", "", err
	}
	authKey, err = common.DecryptString(policy.IssueAuthKey)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt authKey: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	customerKey = strings.TrimSpace(policy.IssueCustomerKey)
	if err := validateTossBillingIssueSnapshotInput(authKey, customerKey); err != nil {
		return "", "", "", err
	}
	if policy.IssueAuthKeyHash == "" || policy.IssueAuthKeyHash != common.GenerateHMAC(authKey) {
		return "", "", "", fmt.Errorf("%w: authKey integrity check failed", ErrTossBillingIssueSnapshotInvalid)
	}
	if strings.TrimSpace(policy.CustomerKey) == "" || policy.CustomerKey != customerKey {
		return "", "", "", fmt.Errorf("%w: customerKey integrity check failed", ErrTossBillingIssueSnapshotInvalid)
	}
	secretKey, err = DecryptProviderCredential(policy.ProviderCredential)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt provider credential: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	if strings.TrimSpace(secretKey) == "" {
		return "", "", "", fmt.Errorf("%w: stored provider credential is empty", ErrTossBillingIssueSnapshotInvalid)
	}
	return authKey, customerKey, secretKey, nil
}

func MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrWalletAutoRechargeClaimLost
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTWalletAutoRecharge)
		if err != nil {
			return err
		}
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
			return err
		}
		if (policy.Status != WalletAutoRechargeStatusPending && policy.Status != WalletAutoRechargeStatusCancelPending) ||
			policy.BillingKeyId != 0 || policy.IssueClaimToken != token || policy.IssueAuthKey == "" {
			return ErrWalletAutoRechargeClaimLost
		}
		if policy.IssueAttempted {
			return nil
		}
		if postBarrier.PolicyError != nil {
			return postBarrier.PolicyError
		}
		now := getDBTimestampTx(tx)
		result := tx.Model(&WalletAutoRecharge{}).
			Where("id = ? AND issue_attempted = ?", policy.Id, false).
			Updates(map[string]interface{}{
				"issue_attempted":  true,
				"issue_claim_time": now,
				"update_time":      now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWalletAutoRechargeClaimLost
		}
		return nil
	})
}

// TransitionClaimedWalletAutoRechargeBillingIssueToCleanup atomically fences a
// keyless ISSUE request from activation while retaining the exact snapshot and
// lease required to recover and delete a provider key. The cancel_pending form
// is intentionally idempotent for cleanup workers, but only the current claim
// owner may enter or refresh it. A stale worker must never cancel an active
// replacement or revoke the key attached by a newer owner.
func TransitionClaimedWalletAutoRechargeBillingIssueToCleanup(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrWalletAutoRechargeClaimLost
	}
	now := GetDBTimestamp()
	result := DB.Model(&WalletAutoRecharge{}).
		Where("auth_trade_no = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_claim_token = ?", tradeNo,
			[]string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusCancelPending}, token).
		Where("issue_attempted = ? AND issue_auth_key <> '' AND issue_auth_key_hash <> '' AND issue_customer_key <> '' AND provider_credential <> ''", true).
		Updates(map[string]interface{}{
			"active_key":       nil,
			"status":           WalletAutoRechargeStatusCancelPending,
			"issue_claim_time": now,
			"update_time":      now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	// MySQL can report zero for an idempotent cancel_pending refresh within the
	// same timestamp second. Accept only the complete, still-owned cleanup state.
	var policy WalletAutoRecharge
	if err := DB.Select("status", "billing_key_id", "active_key", "issue_claim_token", "issue_auth_key", "issue_auth_key_hash", "issue_customer_key", "issue_attempted", "provider_credential").
		Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletAutoRechargeClaimLost
		}
		return err
	}
	if policy.Status == WalletAutoRechargeStatusCancelPending &&
		policy.BillingKeyId == 0 && policy.ActiveKey == nil &&
		policy.IssueClaimToken == token && policy.IssueAttempted &&
		strings.TrimSpace(policy.IssueAuthKey) != "" &&
		strings.TrimSpace(policy.IssueAuthKeyHash) != "" &&
		strings.TrimSpace(policy.IssueCustomerKey) != "" &&
		strings.TrimSpace(policy.ProviderCredential) != "" {
		return nil
	}
	return ErrWalletAutoRechargeClaimLost
}

// PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection moves
// a claimed ISSUE request into a rotated API-key namespace only after the
// caller received a definitive authentication rejection from the exact stored
// credential. The replacement credential and its unattempted marker are
// committed together; MarkWalletAutoRechargeBillingIssueAttempt must authorize
// the next provider POST separately.
//
// The durable client-key fingerprint is required to match nextClientKey. Toss
// keeps client keys stable when secret keys are reissued, so this prevents a
// retry from crossing into another billing MID.
func PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
	tradeNo, token, rejectedSecret, nextClientKey, nextSecret string,
) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	rejectedSecret = strings.TrimSpace(rejectedSecret)
	nextClientKey = strings.TrimSpace(nextClientKey)
	nextSecret = strings.TrimSpace(nextSecret)
	nextClientKeyHash := TossBillingClientKeyFingerprint(nextClientKey)
	if tradeNo == "" || token == "" || rejectedSecret == "" || nextClientKeyHash == "" ||
		nextSecret == "" || rejectedSecret == nextSecret {
		return ErrWalletAutoRechargeClaimLost
	}
	nextCredential, err := EncryptProviderCredential(nextSecret)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "billing_key_id", "issue_claim_token", "issue_auth_key", "issue_attempted", "provider_credential", "provider_client_key_hash").
			Where("auth_trade_no = ?", tradeNo).
			First(&policy).Error; err != nil {
			return err
		}
		if policy.Status != WalletAutoRechargeStatusPending || policy.BillingKeyId != 0 ||
			policy.IssueClaimToken != token || !policy.IssueAttempted ||
			strings.TrimSpace(policy.IssueAuthKey) == "" ||
			!IsValidTossClientKeyFingerprint(policy.ProviderClientKeyHash) ||
			policy.ProviderClientKeyHash != nextClientKeyHash {
			return ErrWalletAutoRechargeClaimLost
		}
		persistedSecret, err := DecryptProviderCredential(policy.ProviderCredential)
		if err != nil {
			return err
		}
		if strings.TrimSpace(persistedSecret) != rejectedSecret {
			return ErrWalletAutoRechargeClaimLost
		}
		now := getDBTimestampTx(tx)
		updated := tx.Model(&WalletAutoRecharge{}).
			Where("id = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_claim_token = ? AND issue_attempted = ? AND provider_credential = ? AND provider_client_key_hash = ?",
				policy.Id, WalletAutoRechargeStatusPending, token, true, policy.ProviderCredential, policy.ProviderClientKeyHash).
			Updates(map[string]interface{}{
				"provider_credential": nextCredential,
				"issue_attempted":     false,
				"issue_claim_time":    now,
				"update_time":         now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWalletAutoRechargeClaimLost
		}
		return nil
	})
}

func walletAutoRechargeIssueClearUpdates() map[string]interface{} {
	return map[string]interface{}{
		"provider_credential": "",
		"issue_auth_key":      "",
		"issue_auth_key_hash": "",
		"issue_customer_key":  "",
		"issue_claim_token":   "",
		"issue_claim_time":    0,
		"issue_retry_time":    0,
		"issue_attempted":     false,
	}
}

func walletAutoRechargeIssueTerminalClearUpdates() map[string]interface{} {
	updates := walletAutoRechargeIssueClearUpdates()
	// Active policies retain this non-secret MID fingerprint after ISSUE so
	// future TopUp attempts can bind recovery to the same Toss merchant. A
	// terminal policy has no future charge and can release that snapshot too.
	updates["provider_client_key_hash"] = ""
	return updates
}

func CancelClaimedWalletAutoRechargeBillingIssue(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrWalletAutoRechargeClaimLost
	}
	updates := walletAutoRechargeIssueTerminalClearUpdates()
	updates["active_key"] = nil
	updates["status"] = WalletAutoRechargeStatusCancelled
	updates["update_time"] = GetDBTimestamp()
	result := DB.Model(&WalletAutoRecharge{}).
		Where("auth_trade_no = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_claim_token = ?",
			tradeNo, []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusCancelPending}, token).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var policy WalletAutoRecharge
	if err := DB.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return err
	}
	if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusFailed {
		return nil
	}
	if policy.Status == WalletAutoRechargeStatusActive {
		return ErrWalletAutoRechargeClaimLost
	}
	return ErrWalletAutoRechargeClaimLost
}

func loadClaimedWalletAutoRechargeForActivation(tx *gorm.DB, tradeNo, claimToken string, expectedBillingKeyID int) (*WalletAutoRecharge, error) {
	var reference WalletAutoRecharge
	if err := tx.Select("owner_user_id", "target_type", "target_id").Where("auth_trade_no = ?", tradeNo).First(&reference).Error; err != nil {
		return nil, err
	}
	if err := lockWalletAutoRechargeOwnerTx(tx, reference.OwnerUserId); err != nil {
		return nil, err
	}
	if err := lockWalletAutoRechargeOrganizationTx(tx, reference.TargetType, reference.TargetId); err != nil {
		return nil, err
	}
	var policy WalletAutoRecharge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return nil, err
	}
	if policy.Status == WalletAutoRechargeStatusActive {
		if expectedBillingKeyID > 0 && policy.BillingKeyId == expectedBillingKeyID {
			return &policy, nil
		}
		return nil, ErrWalletAutoRechargeStatusInvalid
	}
	if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusCancelPending {
		return nil, ErrWalletAutoRechargeCancelled
	}
	if policy.Status != WalletAutoRechargeStatusPending {
		return nil, ErrWalletAutoRechargeStatusInvalid
	}
	if policy.BillingKeyId > 0 || policy.IssueClaimToken != claimToken {
		return nil, ErrWalletAutoRechargeClaimLost
	}
	if err := ensureNoOtherWalletAutoRechargePaymentFenceTx(tx, policy.TargetType, policy.TargetId, policy.Id, ""); err != nil {
		return nil, err
	}
	// Keep the age gate in this attachment transaction. The provider request can
	// cross the controller's preflight boundary while it is in flight; a key
	// returned after the one-time authorization window is cleanup-only and must
	// never become locally active.
	if policy.CreateTime <= 0 || policy.CreateTime <= getDBTimestampTx(tx)-TossSubscriptionPurchaseReservationMaxAgeSeconds {
		return nil, ErrWalletAutoRechargeIssueAuthorizationExpired
	}
	// This check must live in the same transaction as attachment. A user or
	// organization can be disabled while Toss is issuing the billing key, and a
	// pre-issue controller check alone cannot safely authorize activation.
	if err := validateWalletAutoRechargeChargeTarget(tx, &policy); err != nil {
		return nil, err
	}
	if policy.Type == WalletAutoRechargeTypeScheduled {
		if err := validateWalletAutoRechargeIntervalForCurrentMode(policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds); err != nil {
			return nil, err
		}
	}
	return &policy, nil
}

func attachClaimedWalletAutoRechargeTx(tx *gorm.DB, policy *WalletAutoRecharge, claimToken string, billingKeyId int, cardCompany, cardMasked string, chargeNow bool, now time.Time) (bool, error) {
	if policy.Status == WalletAutoRechargeStatusActive && policy.BillingKeyId == billingKeyId {
		return false, nil
	}
	updates := walletAutoRechargeIssueClearUpdates()
	updates["billing_key_id"] = billingKeyId
	updates["card_company"] = cardCompany
	updates["card_number_masked"] = cardMasked
	updates["status"] = WalletAutoRechargeStatusActive
	updates["active_key"] = walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, policy.Type)
	updates["last_error"] = ""
	updates["fail_count"] = 0
	updates["update_time"] = now.Unix()
	if walletScheduledShouldChargeImmediately(*policy, chargeNow) {
		updates["next_charge_time"] = now.Unix()
		policy.NextChargeTime = now.Unix()
	}
	result := tx.Model(&WalletAutoRecharge{}).
		Where("id = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_claim_token = ?", policy.Id, WalletAutoRechargeStatusPending, claimToken).
		Updates(updates)
	if result.Error != nil {
		if isWalletAutoRechargeActiveKeyConflict(result.Error) {
			return false, errors.New("active wallet auto recharge already exists")
		}
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		var current WalletAutoRecharge
		if err := tx.Where("id = ?", policy.Id).First(&current).Error; err != nil {
			return false, err
		}
		*policy = current
		if current.Status == WalletAutoRechargeStatusActive && current.BillingKeyId == billingKeyId {
			return false, nil
		}
		if current.Status == WalletAutoRechargeStatusCancelled {
			return false, ErrWalletAutoRechargeCancelled
		}
		if current.IssueClaimToken != claimToken {
			return false, ErrWalletAutoRechargeClaimLost
		}
		return false, ErrWalletAutoRechargeStatusInvalid
	}
	if err := tx.Where("id = ?", policy.Id).First(policy).Error; err != nil {
		return false, err
	}
	return true, nil
}

func finishClaimedWalletAutoRechargeActivation(policy *WalletAutoRecharge, activatedNow, chargeNow bool, charger TossBillingCharger, now time.Time) (*WalletAutoRecharge, error) {
	if activatedNow && walletScheduledShouldChargeImmediately(*policy, chargeNow) {
		processErr := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, WalletAutoRechargeDailyLimit, charger)
		if err := DB.First(policy, policy.Id).Error; err != nil {
			return nil, err
		}
		return policy, processErr
	}
	return policy, nil
}

func ActivateClaimedWalletAutoRechargeFromToss(tradeNo, claimToken string, billingKeyId int, cardCompany string, cardMasked string, chargeNow bool, charger TossBillingCharger) (*WalletAutoRecharge, error) {
	if tradeNo == "" {
		return nil, errors.New("wallet auto recharge tradeNo is required")
	}
	if strings.TrimSpace(claimToken) == "" {
		return nil, ErrWalletAutoRechargeClaimLost
	}
	if billingKeyId <= 0 {
		return nil, errors.New("wallet auto recharge billing key is invalid")
	}

	now := time.Unix(GetDBTimestamp(), 0).UTC()
	var policy WalletAutoRecharge
	activatedNow := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		loaded, err := loadClaimedWalletAutoRechargeForActivation(tx, tradeNo, claimToken, billingKeyId)
		if err != nil {
			return err
		}
		policy = *loaded
		activatedNow, err = attachClaimedWalletAutoRechargeTx(tx, &policy, claimToken, billingKeyId, cardCompany, cardMasked, chargeNow, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return finishClaimedWalletAutoRechargeActivation(&policy, activatedNow, chargeNow, charger, now)
}

// StoreAndActivateClaimedWalletAutoRechargeFromToss commits the active billing
// key row and the claim-guarded policy attachment together. A crash can leave
// the durable issue snapshot for same-idempotency recovery, but cannot expose a
// locally active, unlinked key that cancellation/account lifecycle code misses.
func StoreAndActivateClaimedWalletAutoRechargeFromToss(
	tradeNo, claimToken, customerKey, billingKey, cardCompany, cardMasked, secretKey, providerClientKeyHash string,
	chargeNow bool,
	charger TossBillingCharger,
) (*WalletAutoRecharge, int, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	claimToken = strings.TrimSpace(claimToken)
	customerKey = strings.TrimSpace(customerKey)
	billingKey = strings.TrimSpace(billingKey)
	if tradeNo == "" || claimToken == "" || customerKey == "" || billingKey == "" {
		return nil, 0, errors.New("invalid claimed wallet auto recharge billing key")
	}
	if !IsTossWalletAutoRechargeOperationallyEnabled() {
		return nil, 0, ErrTossBillingOperationallyDisabled
	}
	now := time.Unix(GetDBTimestamp(), 0).UTC()
	var policy WalletAutoRecharge
	var billingKeyID int
	activatedNow := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if !IsTossWalletAutoRechargeOperationallyEnabled() {
			return ErrTossBillingOperationallyDisabled
		}
		loaded, err := loadClaimedWalletAutoRechargeForActivation(tx, tradeNo, claimToken, 0)
		if err != nil {
			// A stale worker can observe an already-active policy after the same
			// idempotent provider issue. Treat it as success only when the attached
			// local row decrypts to this exact provider key/customer pair.
			if errors.Is(err, ErrWalletAutoRechargeStatusInvalid) {
				var current WalletAutoRecharge
				if loadErr := tx.Where("auth_trade_no = ?", tradeNo).First(&current).Error; loadErr == nil && current.Status == WalletAutoRechargeStatusActive && current.BillingKeyId > 0 {
					plain, storedCustomerKey, keyErr := getTossBillingKeyPlainTx(tx, current.BillingKeyId)
					if keyErr == nil && plain == billingKey && storedCustomerKey == customerKey {
						policy = current
						billingKeyID = current.BillingKeyId
						return nil
					}
				}
			}
			return err
		}
		policy = *loaded
		if policy.CustomerKey != customerKey {
			return ErrWalletAutoRechargeIssueConflict
		}
		billingKeyID, err = storeTossBillingKeyWithSecretStatusTx(
			tx,
			policy.OwnerUserId,
			customerKey,
			billingKey,
			cardCompany,
			cardMasked,
			secretKey,
			BillingKeyStatusActive,
			providerClientKeyHash,
		)
		if err != nil {
			return err
		}
		activatedNow, err = attachClaimedWalletAutoRechargeTx(tx, &policy, claimToken, billingKeyID, cardCompany, cardMasked, chargeNow, now)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	activated, processErr := finishClaimedWalletAutoRechargeActivation(&policy, activatedNow, chargeNow, charger, now)
	return activated, billingKeyID, processErr
}

func nextWalletChargeTime(base time.Time, unit string, value int, customSeconds int64) (time.Time, error) {
	if err := validateWalletAutoRechargeInterval(unit, value, customSeconds); err != nil {
		return time.Time{}, err
	}
	base = base.UTC()
	var next time.Time
	switch unit {
	case WalletAutoRechargeIntervalDay:
		next = base.AddDate(0, 0, value)
	case WalletAutoRechargeIntervalCustom:
		// customSeconds is bounded well below time.Duration's overflow limit by
		// validateWalletAutoRechargeInterval.
		next = base.Add(time.Duration(customSeconds) * time.Second)
	case WalletAutoRechargeIntervalMonth:
		next = nextMonthlyWalletChargeTime(base, value)
	}
	if !next.After(base) || next.Unix() <= base.Unix() {
		return time.Time{}, errors.New("wallet auto recharge next charge time overflowed")
	}
	return next, nil
}

func nextMonthlyWalletChargeTime(base time.Time, value int) time.Time {
	next := time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location())
	if !next.After(base) {
		next = next.AddDate(0, 1, 0)
	}
	if value > 1 {
		next = next.AddDate(0, value-1, 0)
	}
	return next
}

func walletScheduledShouldChargeImmediately(policy WalletAutoRecharge, chargeNow bool) bool {
	if policy.Type != WalletAutoRechargeTypeScheduled {
		return false
	}
	if policy.IntervalUnit == WalletAutoRechargeIntervalMonth {
		return false
	}
	return chargeNow || policy.ChargeImmediately
}

func CancelWalletAutoRecharge(id int, targetType string, targetId int) error {
	_, err := CancelWalletAutoRechargeAndGetBillingKey(id, targetType, targetId)
	return err
}

// walletAutoRechargeCancellationPaymentFenceTx decides whether a cancellation
// must keep the policy's unique active key. A pending wallet TopUp is treated as
// provider-capable even when a legacy binary did not persist ProviderAttempted:
// older releases could lose the POST response before writing any newer marker.
// The caller holds the owner/organization/policy lifecycle locks, so a truly
// pristine new-version row cannot cross its final POST gate after this check.
func walletAutoRechargeCancellationPaymentFenceTx(tx *gorm.DB, policy *WalletAutoRecharge) (*TopUp, string, bool, error) {
	if tx == nil || policy == nil || policy.Id <= 0 {
		return nil, "", false, ErrWalletAutoRechargeClaimLost
	}
	if walletAutoRechargePolicyNeedsReplacementFence(policy) {
		return nil, policy.LastError, true, nil
	}
	if !tx.Migrator().HasTable(&TopUp{}) {
		if strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix) {
			message := walletAutoRechargeManualReconciliationPrefix + "provider-attempt marker exists without payment storage"
			return nil, truncateRunes(message, 255), true, nil
		}
		return nil, "", false, nil
	}

	pendingTopUp, err := findWalletAutoRechargePendingSettlementTopUp(tx, policy)
	if err != nil {
		if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
			message := walletAutoRechargeManualReconciliationPrefix + "cancellation found ambiguous provider charge attempts"
			return nil, truncateRunes(message, 255), true, nil
		}
		return nil, "", false, err
	}
	if pendingTopUp != nil {
		if pendingTopUp.Status == common.TopUpStatusSuccess && policy.LastTradeNo == pendingTopUp.TradeNo &&
			policy.LastChargeTime > 0 && (policy.LastError == "" ||
			strings.HasPrefix(policy.LastError, walletAutoRechargeEventResolutionPendingPrefix)) {
			return nil, "", false, nil
		}
		message := policy.LastError
		if !hasWalletAutoRechargeSettlementMarker(message) &&
			!strings.HasPrefix(message, walletAutoRechargeManualReconciliationPrefix) {
			message = walletAutoRechargeProviderPendingPrefix + "cancellation requested; existing attempt requires GET-only recovery"
		}
		return pendingTopUp, truncateRunes(message, 255), true, nil
	}
	if strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix) {
		message := walletAutoRechargeManualReconciliationPrefix + "cancellation found a provider-attempt marker without a unique payment row"
		return nil, truncateRunes(message, 255), true, nil
	}
	return nil, "", false, nil
}

// preserveWalletAutoRechargePaymentFenceForCancellationTx disables new POSTs
// while retaining the target/type unique key until the already-durable payment
// attempt is resolved. It intentionally does not queue billing-key revocation:
// deleting a key does not cancel a charge request that already reached Toss.
func preserveWalletAutoRechargePaymentFenceForCancellationTx(tx *gorm.DB, policy *WalletAutoRecharge) (bool, error) {
	pendingTopUp, message, fenced, err := walletAutoRechargeCancellationPaymentFenceTx(tx, policy)
	if err != nil || !fenced {
		return false, err
	}
	activeKey, err := walletAutoRechargeAvailableFenceKeyTx(tx, policy)
	if err != nil {
		return false, err
	}
	tradeNo := strings.TrimSpace(policy.LastTradeNo)
	if pendingTopUp != nil {
		tradeNo = pendingTopUp.TradeNo
	}
	updates := map[string]interface{}{
		"active_key":            activeKey,
		"status":                WalletAutoRechargeStatusCancelPending,
		"last_trade_no":         tradeNo,
		"last_error":            message,
		"settlement_retry_time": 0,
		"update_time":           getDBTimestampTx(tx),
	}
	result := tx.Model(&WalletAutoRecharge{}).
		Where("id = ? AND status IN ?", policy.Id, []string{
			WalletAutoRechargeStatusPending,
			WalletAutoRechargeStatusActive,
			WalletAutoRechargeStatusFailed,
			WalletAutoRechargeStatusCancelPending,
		}).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		var current WalletAutoRecharge
		if err := tx.Where("id = ?", policy.Id).First(&current).Error; err != nil {
			return false, err
		}
		activeKeyMatches := current.ActiveKey == nil && activeKey == nil
		if current.ActiveKey != nil && activeKey != nil {
			activeKeyMatches = *current.ActiveKey == activeKey.(string)
		}
		if current.Status != WalletAutoRechargeStatusCancelPending || !activeKeyMatches ||
			current.LastTradeNo != tradeNo || current.LastError != message {
			return false, ErrWalletAutoRechargeClaimLost
		}
		*policy = current
		return true, nil
	}
	if activeKey == nil {
		policy.ActiveKey = nil
	} else {
		key := activeKey.(string)
		policy.ActiveKey = &key
	}
	policy.Status = WalletAutoRechargeStatusCancelPending
	policy.LastTradeNo = tradeNo
	policy.LastError = message
	policy.SettlementRetryTime = 0
	return true, nil
}

func preserveWalletAutoRechargePaymentFencesForLifecycleTx(tx *gorm.DB, where string, args ...interface{}) error {
	if tx == nil {
		return errors.New("wallet auto recharge lifecycle fence requires a transaction")
	}
	var policies []WalletAutoRecharge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(where, args...).
		Where("status IN ?", []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusActive}).
		Order("id asc").
		Find(&policies).Error; err != nil {
		return err
	}
	for i := range policies {
		if _, err := preserveWalletAutoRechargePaymentFenceForCancellationTx(tx, &policies[i]); err != nil {
			return err
		}
	}
	return nil
}

func markUncertainWalletAutoRechargeIssuesCancelPendingTx(tx *gorm.DB, where string, args ...interface{}) error {
	if tx == nil {
		return errors.New("wallet auto recharge cleanup transition requires a transaction")
	}
	query := tx.Model(&WalletAutoRecharge{}).
		Where(where, args...).
		Where("status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_attempted = ?",
			[]string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusActive}, true)
	return query.Updates(map[string]interface{}{
		"active_key":  nil,
		"status":      WalletAutoRechargeStatusCancelPending,
		"update_time": getDBTimestampTx(tx),
	}).Error
}

// deactivateWalletAutoRechargePoliciesForLifecycleTx preserves every ISSUE
// request that may already have reached Toss, while erasing credentials for
// provider-pristine or fully attached policies before making them terminal.
//
// MarkAttempted does not take the account/organization lifecycle root lock. It
// can therefore win between the first uncertain transition and the pristine
// cancellation CAS. The latter excludes attempted rows, and the final pass
// moves any such concurrent winner to cleanup-only cancel_pending. Conversely,
// if this cancellation wins the row first, MarkAttempted's status CAS fails and
// no provider ISSUE is authorized.
func deactivateWalletAutoRechargePoliciesForLifecycleTx(tx *gorm.DB, where string, args ...interface{}) error {
	if err := preserveWalletAutoRechargePaymentFencesForLifecycleTx(tx, where, args...); err != nil {
		return err
	}
	if err := markUncertainWalletAutoRechargeIssuesCancelPendingTx(tx, where, args...); err != nil {
		return err
	}
	updates := walletAutoRechargeIssueTerminalClearUpdates()
	updates["active_key"] = nil
	updates["status"] = WalletAutoRechargeStatusCancelled
	updates["update_time"] = getDBTimestampTx(tx)
	if err := tx.Model(&WalletAutoRecharge{}).
		Where(where, args...).
		Where("status IN ?", []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusActive}).
		Where("billing_key_id > 0 OR issue_attempted = ? OR issue_attempted IS NULL", false).
		Updates(updates).Error; err != nil {
		return err
	}
	return markUncertainWalletAutoRechargeIssuesCancelPendingTx(tx, where, args...)
}

func tossBillingKeysHaveOtherLiveReferencesTx(tx *gorm.DB, billingKeyIDs []int, excludedPolicyID int) (bool, error) {
	if tx == nil || len(billingKeyIDs) == 0 {
		return false, nil
	}
	var count int64
	if tx.Migrator().HasTable(&UserSubscription{}) {
		if err := tx.Model(&UserSubscription{}).
			Where("billing_key_id IN ? AND auto_renew = ? AND status = ?", billingKeyIDs, true, "active").
			Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
	}
	if count > 0 {
		return true, nil
	}
	if tx.Migrator().HasTable(&WalletAutoRecharge{}) {
		query := tx.Model(&WalletAutoRecharge{}).
			Where("billing_key_id IN ? AND status IN ?", billingKeyIDs, []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusActive})
		if excludedPolicyID > 0 {
			query = query.Where("id <> ?", excludedPolicyID)
		}
		count = 0
		if err := query.Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if !tx.Migrator().HasTable(&SubscriptionOrder{}) {
		return false, nil
	}
	count = 0
	if err := tx.Model(&SubscriptionOrder{}).
		Where("billing_key_id IN ? AND payment_provider = ? AND status = ?", billingKeyIDs, PaymentProviderToss, common.TopUpStatusPending).
		Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CancelWalletAutoRechargeAndGetBillingKey atomically wins against activation
// and unlinks only the selected policy. A provider billing key can be shared by
// subscription and wallet contracts, so it is queued for remote deletion only
// after the last live local reference disappears.
func CancelWalletAutoRechargeAndGetBillingKey(id int, targetType string, targetId int) (int, error) {
	billingKeyID := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		// Billing-key attachment paths serialize on the owner before linking a
		// policy/order to a shared provider key. Take the same lock first so the
		// last-reference decision below cannot race a concurrent attachment and
		// revoke a key that just became live elsewhere.
		var reference WalletAutoRecharge
		if err := tx.Select("owner_user_id", "target_type", "target_id").
			Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).
			First(&reference).Error; err != nil {
			return err
		}
		if reference.OwnerUserId > 0 {
			var owner User
			if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id").Where("id = ?", reference.OwnerUserId).First(&owner).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if reference.TargetType == TopUpTargetTypeOrganization && reference.TargetId > 0 {
			var organization Organization
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id").Where("id = ?", reference.TargetId).First(&organization).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).
			First(&policy).Error; err != nil {
			return err
		}
		if policy.Status != WalletAutoRechargeStatusCancelled {
			preserved, err := preserveWalletAutoRechargePaymentFenceForCancellationTx(tx, &policy)
			if err != nil {
				return err
			}
			if preserved {
				// The policy is disabled (cancel_pending), but remains a live
				// replacement/payment fence until GET/webhook reconciliation.
				billingKeyID = 0
				if policy.BillingKeyId > 0 {
					queued, queueErr := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
					if queueErr != nil {
						return queueErr
					}
					if queued {
						billingKeyID = policy.BillingKeyId
					}
				}
				return nil
			}
		}
		cancellableStatuses := []string{
			WalletAutoRechargeStatusPending,
			WalletAutoRechargeStatusActive,
			WalletAutoRechargeStatusFailed,
		}
		preserveUncertainIssue := func() (bool, error) {
			// The provider may have issued a billing key even though its response was
			// lost. The attempted marker alone is authoritative: a missing/corrupt
			// auth snapshot requires operator reconciliation rather than destructive
			// cleanup. Preserve every ISSUE field and move the policy out of use.
			cleanup := tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND target_type = ? AND target_id = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_attempted = ?",
					id, targetType, targetId, cancellableStatuses, true).
				Updates(map[string]interface{}{
					"active_key":  nil,
					"status":      WalletAutoRechargeStatusCancelPending,
					"update_time": now,
				})
			if cleanup.Error != nil {
				return false, cleanup.Error
			}
			return cleanup.RowsAffected == 1, nil
		}
		preserved, err := preserveUncertainIssue()
		if err != nil {
			return err
		}
		if preserved {
			policy.Status = WalletAutoRechargeStatusCancelPending
		}
		if policy.Status == WalletAutoRechargeStatusCancelPending && policy.BillingKeyId > 0 && !policy.IssueAttempted {
			updates := walletAutoRechargeIssueTerminalClearUpdates()
			updates["active_key"] = nil
			updates["status"] = WalletAutoRechargeStatusCancelled
			updates["update_time"] = now
			result := tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND target_type = ? AND target_id = ? AND status = ?", id, targetType, targetId, WalletAutoRechargeStatusCancelPending).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWalletAutoRechargeClaimLost
			}
			policy.Status = WalletAutoRechargeStatusCancelled
			policy.ActiveKey = nil
		}
		if policy.Status != WalletAutoRechargeStatusCancelled && policy.Status != WalletAutoRechargeStatusCancelPending {
			updates := walletAutoRechargeIssueTerminalClearUpdates()
			updates["active_key"] = nil
			updates["status"] = WalletAutoRechargeStatusCancelled
			updates["update_time"] = now
			result := tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND target_type = ? AND target_id = ? AND status IN ?", id, targetType, targetId,
					cancellableStatuses).
				Where("billing_key_id > 0 OR issue_attempted = ? OR issue_attempted IS NULL", false).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				preserved, err := preserveUncertainIssue()
				if err != nil {
					return err
				}
				if preserved {
					policy.Status = WalletAutoRechargeStatusCancelPending
					return nil
				}
				if err := tx.Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).First(&policy).Error; err != nil {
					return err
				}
				if policy.Status != WalletAutoRechargeStatusCancelled && policy.Status != WalletAutoRechargeStatusCancelPending {
					return ErrWalletAutoRechargeStatusInvalid
				}
			}
		}
		if err := tx.Select("billing_key_id", "status").Where("id = ?", id).First(&policy).Error; err != nil {
			return err
		}
		billingKeyID = policy.BillingKeyId
		if billingKeyID > 0 {
			queued, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, billingKeyID)
			if err != nil {
				return err
			}
			if !queued {
				billingKeyID = 0
			}
		}
		return nil
	})
	return billingKeyID, err
}

func CancelPendingWalletAutoRechargeByTradeNo(tradeNo string, targetType string, targetId int) error {
	if strings.TrimSpace(tradeNo) == "" {
		return errors.New("wallet auto recharge tradeNo is required")
	}
	var policy WalletAutoRecharge
	if err := DB.Select("id", "status").Where("auth_trade_no = ? AND target_type = ? AND target_id = ?", tradeNo, targetType, targetId).First(&policy).Error; err != nil {
		return err
	}
	if policy.Status != WalletAutoRechargeStatusPending {
		return ErrWalletAutoRechargeStatusInvalid
	}
	_, err := CancelWalletAutoRechargeAndGetBillingKey(policy.Id, targetType, targetId)
	return err
}

// ExpireStaleUnattemptedWalletAutoRecharges releases an abandoned billing-auth
// reservation only when every durable provider-activity marker is pristine.
// Its single conditional update races safely with the success callback: a
// callback claim makes the row ineligible, while a cleanup winner changes the
// status before the callback can snapshot an authKey or contact Toss.
func ExpireStaleUnattemptedWalletAutoRecharges(cutoffUnix int64) (int64, error) {
	now := GetDBTimestamp()
	result := DB.Model(&WalletAutoRecharge{}).
		Where("status = ? AND create_time < ?", WalletAutoRechargeStatusPending, cutoffUnix).
		Where("(billing_key_id = 0 OR billing_key_id IS NULL) AND (last_trade_no = '' OR last_trade_no IS NULL)").
		Where("(issue_auth_key = '' OR issue_auth_key IS NULL) AND (issue_auth_key_hash = '' OR issue_auth_key_hash IS NULL) AND (issue_customer_key = '' OR issue_customer_key IS NULL)").
		Where("issue_attempted = ?", false).
		Where("(issue_claim_token = '' OR issue_claim_token IS NULL) AND (issue_claim_time = 0 OR issue_claim_time IS NULL)").
		Updates(map[string]interface{}{
			"active_key":               nil,
			"status":                   WalletAutoRechargeStatusCancelled,
			"provider_credential":      "",
			"provider_client_key_hash": "",
			"update_time":              now,
		})
	return result.RowsAffected, result.Error
}

func ListWalletAutoRecharges(targetType string, targetId int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("target_type = ? AND target_id = ?", targetType, targetId).Order("id desc").Find(&rows).Error
	return rows, err
}

func GetWalletAutoRechargeForTarget(id int, targetType string, targetId int) (*WalletAutoRecharge, error) {
	var policy WalletAutoRecharge
	if err := DB.Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).First(&policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func GetWalletAutoRechargeByTradeNoForTarget(tradeNo string, targetType string, targetId int) (*WalletAutoRecharge, error) {
	var policy WalletAutoRecharge
	if err := DB.Where("auth_trade_no = ? AND target_type = ? AND target_id = ?", tradeNo, targetType, targetId).First(&policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func GetWalletAutoRechargeByChargeTradeNo(tradeNo string) (*WalletAutoRecharge, bool, error) {
	return GetWalletAutoRechargeByChargeTradeNoWithContext(context.Background(), tradeNo)
}

func GetWalletAutoRechargeByChargeTradeNoWithContext(ctx context.Context, tradeNo string) (*WalletAutoRecharge, bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	db := dbWithContext(ctx)
	policyID := 0
	var topUp TopUp
	topUpErr := db.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if topUpErr == nil {
		associationPresent := topUp.WalletAutoRechargeId != nil ||
			topUp.WalletAutoRechargeCycleKey != nil || topUp.WalletAutoRechargeAttempt != nil
		if associationPresent {
			identity, _, identityErr := topUpWalletAutoRechargeAttemptIdentity(&topUp)
			if identityErr != nil {
				return nil, false, identityErr
			}
			policyID = identity.PolicyID
		} else if HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo) {
			return nil, false, ErrTossRecurringOrderIDEvidenceCorrupt
		}
	} else if !errors.Is(topUpErr, gorm.ErrRecordNotFound) {
		return nil, false, topUpErr
	}
	if policyID == 0 {
		rest, ok := strings.CutPrefix(tradeNo, "wallet_auto_")
		if !ok {
			return nil, false, nil
		}
		policyPart, _, ok := strings.Cut(rest, "_")
		if !ok {
			return nil, false, nil
		}
		parsedPolicyID, err := strconv.Atoi(policyPart)
		if err != nil || parsedPolicyID <= 0 {
			return nil, false, nil
		}
		policyID = parsedPolicyID
	}
	var policy WalletAutoRecharge
	if err := db.Where("id = ?", policyID).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if topUpErr == nil {
		belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if err != nil || !belongs {
			return nil, false, err
		}
	} else if !strings.HasPrefix(tradeNo, fmt.Sprintf("wallet_auto_%d_", policy.Id)) {
		return nil, false, nil
	}
	return &policy, true, nil
}

func HasWalletAutoRechargeProviderActivity(policy *WalletAutoRecharge) (bool, error) {
	if policy == nil {
		return false, nil
	}
	if err := ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(DB); err != nil {
		// An incomplete opaque row cannot be mapped back to a policy reliably.
		// Fail lifecycle cleanup globally rather than report a false "no activity"
		// and erase the only policy/key evidence needed for reconciliation.
		return true, err
	}
	if policy.BillingKeyId > 0 || strings.TrimSpace(policy.LastTradeNo) != "" || strings.TrimSpace(policy.IssueAuthKey) != "" || policy.IssueAttempted {
		return true, nil
	}
	var count int64
	err := DB.Model(&TopUp{}).
		Where("payment_provider = ? AND payment_method = ?", PaymentProviderToss, PaymentMethodToss).
		Where("wallet_auto_recharge_id = ? OR trade_no LIKE ? ESCAPE '!'", policy.Id, walletAutoRechargeTradeNoLikePattern(policy.Id)).
		Count(&count).Error
	return count > 0, err
}

func GetDueScheduledWalletAutoRecharges(nowUnix int64, limit int) ([]WalletAutoRecharge, error) {
	return GetDueScheduledWalletAutoRechargesExcluding(nowUnix, limit, nil)
}

func GetDueScheduledWalletAutoRechargesExcluding(nowUnix int64, limit int, excludedIDs []int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	staleCutoff := nowUnix - TossBillingOperationalGraceSeconds
	query := DB.Where("type = ? AND status = ? AND next_charge_time > ? AND next_charge_time <= ?", WalletAutoRechargeTypeScheduled, WalletAutoRechargeStatusActive, staleCutoff, nowUnix).
		Where("last_error IS NULL OR (last_error NOT LIKE ? AND last_error NOT LIKE ? AND last_error NOT LIKE ?)",
			walletAutoRechargeProviderPendingPrefix+"%", walletAutoRechargeReconciliationPendingPrefix+"%", walletAutoRechargeManualReconciliationPrefix+"%")
	if len(excludedIDs) > 0 {
		query = query.Not("id IN ?", excludedIDs)
	}
	err := query.
		Order("next_charge_time asc, id asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// AdvanceStaleScheduledWalletAutoRecharges skips a schedule that could not run
// for longer than the payment operational grace period. Resuming global billing
// weeks later must not immediately create a surprise catch-up charge. Threshold
// policies are intentionally excluded because their trigger is current balance,
// not a missed point in time.
func AdvanceStaleScheduledWalletAutoRecharges(nowUnix int64, limit int) (int64, error) {
	if nowUnix <= 0 {
		nowUnix = GetDBTimestamp()
	}
	if limit <= 0 {
		limit = 100
	}
	cutoff := nowUnix - TossBillingOperationalGraceSeconds
	var rows []WalletAutoRecharge
	if err := DB.Where("type = ? AND status = ? AND next_charge_time > 0 AND next_charge_time <= ?",
		WalletAutoRechargeTypeScheduled, WalletAutoRechargeStatusActive, cutoff).
		Where("last_error IS NULL OR (last_error NOT LIKE ? AND last_error NOT LIKE ? AND last_error NOT LIKE ?)",
			walletAutoRechargeProviderPendingPrefix+"%", walletAutoRechargeReconciliationPendingPrefix+"%", walletAutoRechargeManualReconciliationPrefix+"%").
		Order("next_charge_time asc, id asc").
		Limit(walletAutoRechargeBoundedScanLimit(limit)).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	now := time.Unix(nowUnix, 0).UTC()
	var advanced int64
	for i := range rows {
		if advanced >= int64(limit) {
			break
		}
		var rowAdvanced bool
		err := DB.Transaction(func(tx *gorm.DB) error {
			var policy WalletAutoRecharge
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rows[i].Id).First(&policy).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if policy.Type != WalletAutoRechargeTypeScheduled || policy.Status != WalletAutoRechargeStatusActive ||
				policy.NextChargeTime <= 0 || policy.NextChargeTime > cutoff ||
				hasWalletAutoRechargeSettlementMarker(policy.LastError) ||
				strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix) {
				return nil
			}

			// Preparation takes the same policy-row lock before it creates its
			// durable TopUp. Whichever transaction wins is therefore authoritative:
			// a prepared provider attempt is never hidden by advancing its schedule.
			pendingTopUp, err := findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
			if err != nil {
				return err
			}
			if pendingTopUp != nil && pendingTopUp.Status == common.TopUpStatusPending {
				return nil
			}

			next, err := nextWalletChargeTime(now, policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds)
			if err != nil {
				return err
			}
			result := tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND type = ? AND status = ? AND next_charge_time = ? AND next_charge_time <= ?",
					policy.Id, WalletAutoRechargeTypeScheduled, WalletAutoRechargeStatusActive, policy.NextChargeTime, cutoff).
				Where("last_error IS NULL OR (last_error NOT LIKE ? AND last_error NOT LIKE ? AND last_error NOT LIKE ?)",
					walletAutoRechargeProviderPendingPrefix+"%", walletAutoRechargeReconciliationPendingPrefix+"%", walletAutoRechargeManualReconciliationPrefix+"%").
				Updates(map[string]interface{}{
					"next_charge_time": next.Unix(),
					"last_error":       "scheduled charge skipped after extended billing outage",
					"update_time":      nowUnix,
				})
			if result.Error != nil {
				return result.Error
			}
			rowAdvanced = result.RowsAffected == 1
			return nil
		})
		if err != nil {
			return advanced, err
		}
		if rowAdvanced {
			advanced++
		}
	}
	return advanced, nil
}

func GetActiveThresholdWalletAutoRecharges(limit int) ([]WalletAutoRecharge, error) {
	return GetActiveThresholdWalletAutoRechargesExcluding(limit, nil)
}

func GetActiveThresholdWalletAutoRechargesExcluding(limit int, excludedIDs []int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	query := DB.Where("type = ? AND status = ?", WalletAutoRechargeTypeThreshold, WalletAutoRechargeStatusActive).
		Where("last_error IS NULL OR (last_error NOT LIKE ? AND last_error NOT LIKE ? AND last_error NOT LIKE ?)",
			walletAutoRechargeProviderPendingPrefix+"%", walletAutoRechargeReconciliationPendingPrefix+"%", walletAutoRechargeManualReconciliationPrefix+"%")
	if len(excludedIDs) > 0 {
		query = query.Not("id IN ?", excludedIDs)
	}
	err := query.
		Order("update_time asc, id asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// GetPendingWalletAutoRechargeSettlements reserves cancelled/failed policies as
// well as active ones: a provider may already have charged the card, so local
// credit must still be reconciled. Reserving before processing prevents a
// poison row from occupying the head of every bounded run. It also repairs the
// legacy crash shape where a durable TopUp exists but last_trade_no was never
// copied to the policy before the process stopped.
func GetPendingWalletAutoRechargeSettlements(limit int) ([]WalletAutoRecharge, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var candidates []WalletAutoRecharge
	err := DB.Where(
		"last_trade_no <> ? AND (last_error LIKE ? OR last_error LIKE ?)",
		"",
		walletAutoRechargeProviderPendingPrefix+"%",
		walletAutoRechargeReconciliationPendingPrefix+"%",
	).
		Where("settlement_retry_time = 0 OR settlement_retry_time IS NULL OR settlement_retry_time <= ?", now).
		Order("CASE WHEN settlement_retry_time IS NULL OR settlement_retry_time <= 0 THEN update_time ELSE settlement_retry_time END asc, update_time asc, id asc").
		Limit(walletAutoRechargeBoundedScanLimit(limit)).
		Find(&candidates).Error
	if err != nil {
		return nil, err
	}

	nextRetry := now + walletAutoRechargeQueueRetryDelaySeconds
	reserved := make([]WalletAutoRecharge, 0, limit)
	for i := range candidates {
		if len(reserved) >= limit {
			break
		}
		candidate, recoverable, err := reserveMarkedWalletAutoRechargeSettlement(candidates[i].Id, now, nextRetry)
		if err != nil {
			return reserved, err
		}
		if recoverable {
			reserved = append(reserved, candidate)
		}
	}
	if len(reserved) >= limit {
		return reserved, nil
	}

	// A correlated SQL prefix expression is different across SQLite, MySQL,
	// and PostgreSQL. Scan a bounded policy batch in Go, then use the escaped
	// per-policy LIKE pattern to find only that policy's pending Toss TopUp.
	// In particular, policy 1 must never claim wallet_auto_12_*.
	remaining := limit - len(reserved)
	var legacyCandidates []WalletAutoRecharge
	if err := DB.Where("status IN ?", []string{
		WalletAutoRechargeStatusActive,
		WalletAutoRechargeStatusCancelled,
		WalletAutoRechargeStatusFailed,
	}).
		Where("last_trade_no = '' OR last_trade_no IS NULL").
		Where("last_error IS NULL OR last_error NOT LIKE ?", walletAutoRechargeManualReconciliationPrefix+"%").
		Where("settlement_retry_time = 0 OR settlement_retry_time IS NULL OR settlement_retry_time <= ?", now).
		Order("CASE WHEN settlement_retry_time IS NULL OR settlement_retry_time <= 0 THEN update_time ELSE settlement_retry_time END asc, update_time asc, id asc").
		Limit(walletAutoRechargeBoundedScanLimit(remaining)).
		Find(&legacyCandidates).Error; err != nil {
		return reserved, err
	}
	for i := range legacyCandidates {
		if len(reserved) >= limit {
			break
		}
		candidate, ok, err := reserveLegacyWalletAutoRechargeSettlement(legacyCandidates[i].Id, now, nextRetry)
		if err != nil {
			return reserved, err
		}
		if ok {
			reserved = append(reserved, candidate)
		}
	}
	return reserved, nil
}

// reserveMarkedWalletAutoRechargeSettlement leases every inspected marker so a
// bounded queue rotates past manual-only rows, but returns only states that the
// automatic ProcessWalletAutoRecharge path can actually complete. Authoritative
// cancellation/DONE-conflict rows remain available to TossPaymentEvent admin
// reconciliation without repeatedly poisoning this worker queue.
func reserveMarkedWalletAutoRechargeSettlement(policyID int, now, nextRetry int64) (WalletAutoRecharge, bool, error) {
	var reserved WalletAutoRecharge
	recoverable := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		policy := *lockedPolicy
		if strings.TrimSpace(policy.LastTradeNo) == "" ||
			!hasWalletAutoRechargeSettlementMarker(policy.LastError) ||
			policy.SettlementRetryTime > now {
			return nil
		}

		var topUp TopUp
		topUpErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", policy.LastTradeNo).
			First(&topUp).Error
		if topUpErr != nil && !errors.Is(topUpErr, gorm.ErrRecordNotFound) {
			return topUpErr
		}
		var cancellationCount int64
		if err := tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND event_type = ? AND status IN ? AND reconciliation_status = ?", policy.LastTradeNo, TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"}, TossReconciliationStatusRequired).
			Count(&cancellationCount).Error; err != nil {
			return err
		}
		if cancellationCount > 0 {
			blocked, err := haltWalletAutoRechargeForRecordedCancellationTx(tx, &policy, false)
			if err != nil {
				return err
			}
			if blocked {
				recoverable = false
				return tx.Model(&WalletAutoRecharge{}).
					Where("id = ?", policy.Id).
					Update("settlement_retry_time", 0).Error
			}
		}
		recoverable = topUpErr == nil && cancellationCount == 0 && isWalletAutoRechargeSettlementQueueRecoverable(&policy, &topUp)
		if !recoverable {
			manualReason := walletAutoRechargeSettlementQueueManualReason(&policy, &topUp, topUpErr, cancellationCount)
			manualMessage := truncateRunes(walletAutoRechargeManualReconciliationPrefix+manualReason, 255)
			if err := haltWalletAutoRechargeForReconciliationTx(tx, policy.Id, policy.LastTradeNo, manualMessage); err != nil {
				return err
			}
			return tx.Model(&WalletAutoRecharge{}).
				Where("id = ?", policy.Id).
				Update("settlement_retry_time", 0).Error
		}

		result := tx.Model(&WalletAutoRecharge{}).
			Where("id = ? AND last_trade_no = ?", policy.Id, policy.LastTradeNo).
			Where("last_error LIKE ? OR last_error LIKE ?",
				walletAutoRechargeProviderPendingPrefix+"%", walletAutoRechargeReconciliationPendingPrefix+"%").
			Where("settlement_retry_time = ? OR (settlement_retry_time IS NULL AND ? = 0)", policy.SettlementRetryTime, policy.SettlementRetryTime).
			Update("settlement_retry_time", nextRetry)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			recoverable = false
			return nil
		}
		if recoverable {
			policy.SettlementRetryTime = nextRetry
			reserved = policy
		}
		return nil
	})
	return reserved, recoverable, err
}

func walletAutoRechargeSettlementQueueManualReason(policy *WalletAutoRecharge, topUp *TopUp, topUpErr error, cancellationCount int64) string {
	if cancellationCount > 0 {
		return "authoritative cancellation event requires admin reconciliation"
	}
	if errors.Is(topUpErr, gorm.ErrRecordNotFound) || topUp == nil {
		return "durable payment row is missing"
	}
	if policy != nil && strings.HasPrefix(policy.LastError, walletAutoRechargeDoneMismatchPrefix) {
		return "provider DONE payload mismatch"
	}
	providerOrderID := strings.TrimSpace(topUp.ProviderOrderId)
	if providerOrderID == topUp.TradeNo+":terminal" {
		return "provider terminal result requires admin reconciliation"
	}
	if providerOrderID == topUp.TradeNo+":done-mismatch" {
		return "provider DONE payload mismatch"
	}
	if strings.TrimSpace(topUp.Status) != "" {
		return "unsupported payment state " + topUp.Status
	}
	return "unsupported payment state"
}

func isWalletAutoRechargeSettlementQueueRecoverable(policy *WalletAutoRecharge, topUp *TopUp) bool {
	if policy == nil || topUp == nil ||
		strings.HasPrefix(policy.LastError, walletAutoRechargeDoneMismatchPrefix) ||
		topUp.TradeNo != policy.LastTradeNo ||
		topUp.PaymentProvider != PaymentProviderToss ||
		topUp.PaymentMethod != PaymentMethodToss {
		return false
	}
	belongs, err := walletAutoRechargeTopUpBelongsToPolicy(topUp, policy)
	if err != nil || !belongs {
		return false
	}
	providerOrderID := strings.TrimSpace(topUp.ProviderOrderId)
	if providerOrderID == topUp.TradeNo+":terminal" || providerOrderID == topUp.TradeNo+":done-mismatch" {
		return false
	}
	return topUp.Status == common.TopUpStatusPending || topUp.Status == common.TopUpStatusSuccess
}

func walletAutoRechargeBoundedScanLimit(remaining int) int {
	const (
		minimum = 32
		maximum = 512
	)
	if remaining <= 0 {
		return 0
	}
	if remaining > maximum/8 {
		return maximum
	}
	limit := remaining * 8
	if limit < minimum {
		return minimum
	}
	return limit
}

func hasWalletAutoRechargeSettlementMarker(lastError string) bool {
	return strings.HasPrefix(lastError, walletAutoRechargeProviderPendingPrefix) ||
		strings.HasPrefix(lastError, walletAutoRechargeReconciliationPendingPrefix)
}

func reserveLegacyWalletAutoRechargeSettlement(policyID int, now, nextRetry int64) (WalletAutoRecharge, bool, error) {
	var reserved WalletAutoRecharge
	found := false
	var associationConflict error
	err := DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", policyID).First(&policy).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if policy.Status != WalletAutoRechargeStatusActive &&
			policy.Status != WalletAutoRechargeStatusCancelled &&
			policy.Status != WalletAutoRechargeStatusFailed {
			return nil
		}
		if strings.TrimSpace(policy.LastTradeNo) != "" || policy.SettlementRetryTime > now ||
			strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix) {
			return nil
		}

		pendingTopUp, err := findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
		if err != nil {
			if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
				message := walletAutoRechargeManualReconciliationPrefix + err.Error()
				if haltErr := haltWalletAutoRechargeForAssociationConflictTx(tx, &policy, policy.LastTradeNo, message); haltErr != nil {
					return errors.Join(err, haltErr)
				}
				associationConflict = err
				return nil
			}
			return err
		}
		if pendingTopUp == nil || pendingTopUp.Status != common.TopUpStatusPending {
			// Persist a negative-scan lease as the cursor. Without moving it, the
			// same oldest no-TopUp policies occupy every bounded scan forever and a
			// real orphan just beyond the batch boundary can never be discovered.
			return tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND status IN ?", policy.Id, []string{
					WalletAutoRechargeStatusActive,
					WalletAutoRechargeStatusCancelled,
					WalletAutoRechargeStatusFailed,
				}).
				Where("last_trade_no = '' OR last_trade_no IS NULL").
				Where("settlement_retry_time = ? OR (settlement_retry_time IS NULL AND ? = 0)", policy.SettlementRetryTime, policy.SettlementRetryTime).
				Update("settlement_retry_time", nextRetry).Error
		}

		marker := policy.LastError
		if !hasWalletAutoRechargeSettlementMarker(marker) {
			marker = walletAutoRechargeProviderPendingPrefix + "recovered legacy durable charge attempt"
		}
		result := tx.Model(&WalletAutoRecharge{}).
			Where("id = ? AND status IN ?", policy.Id, []string{
				WalletAutoRechargeStatusActive,
				WalletAutoRechargeStatusCancelled,
				WalletAutoRechargeStatusFailed,
			}).
			Where("last_trade_no = '' OR last_trade_no IS NULL").
			Where("settlement_retry_time = ? OR (settlement_retry_time IS NULL AND ? = 0)", policy.SettlementRetryTime, policy.SettlementRetryTime).
			Updates(map[string]interface{}{
				"last_trade_no":         pendingTopUp.TradeNo,
				"last_error":            marker,
				"settlement_retry_time": nextRetry,
				"update_time":           now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		policy.LastTradeNo = pendingTopUp.TradeNo
		policy.LastError = marker
		policy.SettlementRetryTime = nextRetry
		policy.UpdateTime = now
		reserved = policy
		found = true
		return nil
	})
	if err == nil && associationConflict != nil {
		err = associationConflict
	}
	return reserved, found, err
}

func ReconcileStaleWalletAutoRechargeBillingIssues(ctx context.Context, limit int) (int64, error) {
	if walletAutoRechargeIssueReconciler == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	cutoff := now - walletAutoRechargeIssueClaimTTLSeconds
	var candidates []WalletAutoRecharge
	if err := DB.Where("status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_auth_key <> ''",
		[]string{WalletAutoRechargeStatusCancelPending, WalletAutoRechargeStatusPending}).
		Where("issue_claim_token = '' OR issue_claim_token IS NULL OR issue_claim_time <= ?", cutoff).
		Where("issue_retry_time = 0 OR issue_retry_time IS NULL OR issue_retry_time <= ?", now).
		// Retry age comes before status. Otherwise a full batch of permanently
		// broken cancel_pending rows can starve every normal pending recovery.
		Order("CASE WHEN issue_retry_time IS NULL OR issue_retry_time <= 0 THEN CASE WHEN issue_claim_time > 0 THEN issue_claim_time ELSE create_time END ELSE issue_retry_time END asc, issue_claim_time asc, status asc, id asc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	nextRetry := now + walletAutoRechargeQueueRetryDelaySeconds
	rows := make([]WalletAutoRecharge, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		reserve := DB.Model(&WalletAutoRecharge{}).
			Where("id = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND issue_auth_key <> ''",
				candidate.Id, []string{WalletAutoRechargeStatusCancelPending, WalletAutoRechargeStatusPending}).
			Where("issue_claim_token = '' OR issue_claim_token IS NULL OR issue_claim_time <= ?", cutoff).
			Where("issue_retry_time = ? OR (issue_retry_time IS NULL AND ? = 0)", candidate.IssueRetryTime, candidate.IssueRetryTime).
			Update("issue_retry_time", nextRetry)
		if reserve.Error != nil {
			return 0, reserve.Error
		}
		if reserve.RowsAffected != 1 {
			continue
		}
		candidate.IssueRetryTime = nextRetry
		rows = append(rows, *candidate)
	}
	type reconciliationResult struct {
		ok      bool
		err     error
		tradeNo string
	}
	outcomes := make(chan reconciliationResult, len(rows))
	runBoundedTossMaintenance(rows, func(row WalletAutoRecharge) {
		ok, err := walletAutoRechargeIssueReconciler(ctx, row)
		outcomes <- reconciliationResult{ok: ok, err: err, tradeNo: row.AuthTradeNo}
	})
	close(outcomes)
	var resolved int64
	var lastErr error
	for outcome := range outcomes {
		if outcome.err != nil {
			lastErr = outcome.err
			common.SysError(fmt.Sprintf("failed to reconcile wallet auto recharge billing issue %s: %v", outcome.tradeNo, outcome.err))
			continue
		}
		if outcome.ok {
			resolved++
		}
	}
	return resolved, lastErr
}

func ProcessWalletAutoRecharge(ctx context.Context, policyId int, now time.Time, maxFails int, charger TossBillingCharger) error {
	if charger == nil {
		return errors.New("toss wallet auto recharge charger not configured")
	}
	if maxFails <= 0 {
		maxFails = 1
	}
	now = walletAutoRechargeProcessLocalTime(now)

	charge, err := prepareWalletAutoRechargeCharge(policyId, now, maxFails)
	if err != nil {
		return err
	}
	if charge.preparationErr != nil {
		return charge.preparationErr
	}
	if !charge.shouldCharge {
		return nil
	}

	if !charge.alreadyDone {
		var result *TossBillingChargeResult
		postCharge := func() (*TossBillingChargeResult, error) {
			if strings.TrimSpace(charge.billingKey) == "" || strings.TrimSpace(charge.customerKey) == "" || len(charge.secretKeys) == 0 {
				return nil, errors.New("wallet auto recharge attempt credential is unavailable")
			}
			var lastResult *TossBillingChargeResult
			var lastErr error
			for i, secretKey := range charge.secretKeys {
				if err := RequireFreshTossConfig(); err != nil {
					return lastResult, fmt.Errorf("%w: %v", ErrTossBillingOperationallyDisabled, err)
				}
				// The exact API key is part of Toss's idempotency namespace. Persist
				// it under the same lifecycle/grace transaction that authorizes each
				// individual POST, including a rotated-key fallback after a definitive
				// credential rejection.
				if err := claimWalletAutoRechargeProviderPOSTCredential(policyId, charge.tradeNo, secretKey); err != nil {
					return nil, err
				}
				candidateResult, candidateErr := charger(
					WithTossWalletAutoRechargeChargeContext(ctx),
					charge.billingKey,
					charge.customerKey,
					secretKey,
					charge.tradeNo,
					"지갑 자동충전",
					charge.chargeKRW,
				)
				if candidateErr == nil {
					return candidateResult, nil
				}
				lastResult = candidateResult
				lastErr = candidateErr
				// Ambiguous outcomes remain bound to the just-persisted credential.
				// Only a definitive auth rejection may advance to another same-MID
				// candidate, which gets its own fresh gate and durable snapshot.
				if isTossBillingAmbiguousChargeError(candidateErr) || i == len(charge.secretKeys)-1 || !isTossBillingCredentialError(candidateErr) {
					break
				}
				if promoteErr := promoteWalletAutoRechargeProviderCredentialAfterRejection(
					policyId,
					charge.tradeNo,
					secretKey,
					charge.secretKeys[i+1],
				); promoteErr != nil {
					return lastResult, promoteErr
				}
			}
			if lastErr == nil {
				lastErr = errors.New("Toss billing secret is unavailable")
			}
			if isTossBillingAmbiguousChargeError(lastErr) && !errors.Is(lastErr, ErrTossBillingChargePending) {
				lastErr = fmt.Errorf("%w: %v", ErrTossBillingChargePending, lastErr)
			}
			return lastResult, lastErr
		}
		if charge.lookupPending {
			if walletAutoRechargeTossPaymentLookup == nil {
				return errors.New("toss wallet auto recharge payment lookup not configured")
			}
			result, err = walletAutoRechargeTossPaymentLookup(ctx, charge.secretKeys, charge.tradeNo, charge.chargeKRW)
			if errors.Is(err, ErrTossBillingPaymentNotFound) {
				if charge.postForbidden {
					if charge.cancellationNeedsGrace && !charge.stalePending {
						return ErrWalletAutoRechargeReconciliationRequired
					}
					var settlementWon bool
					var closeErr error
					if charge.cancellationNeedsGrace {
						settlementWon, closeErr = closeStaleWalletAutoRechargeAttempt(policyId, charge.tradeNo)
					} else {
						settlementWon, closeErr = closeCancellationBlockedWalletAutoRechargeAttempt(policyId, charge.tradeNo)
					}
					if closeErr != nil {
						return closeErr
					}
					if !settlementWon {
						return ErrWalletAutoRechargeReconciliationRequired
					}
					charge.alreadyDone = true
					result = nil
					err = nil
				} else if charge.stalePending {
					settlementWon, closeErr := closeStaleWalletAutoRechargeAttempt(policyId, charge.tradeNo)
					if closeErr != nil {
						return closeErr
					}
					if !settlementWon {
						return ErrWalletAutoRechargeAttemptOutsideGrace
					}
					charge.alreadyDone = true
					result = nil
					err = nil
				} else {
					result, err = postCharge()
				}
			}
		} else {
			result, err = postCharge()
		}
		if !charge.alreadyDone {
			providerStatus := ""
			if result != nil {
				providerStatus = strings.ToUpper(strings.TrimSpace(result.ProviderStatus))
			}
			if result != nil && providerStatus == "DONE" && (!result.Done || result.Total != charge.chargeKRW ||
				(result.BalanceAmount > 0 && result.BalanceAmount != result.Total)) {
				return MarkWalletAutoRechargeDoneMismatch(policyId, charge.tradeNo, result)
			}
			if result != nil && (providerStatus == "CANCELED" || providerStatus == "PARTIAL_CANCELED") {
				strictCancellation := errors.Is(err, ErrTossBillingChargeCanceled)
				safeFullCancellation := strictCancellation && providerStatus == "CANCELED" && result.BalanceAmount == 0
				if !safeFullCancellation && !(strictCancellation && providerStatus == "PARTIAL_CANCELED") {
					// A cancellation-shaped response that failed strict identity,
					// amount, card, or event-persistence validation may still
					// represent provider-side money movement. Freeze this policy
					// before the generic failure path can advance to another orderId.
					return MarkWalletAutoRechargeFinancialMismatch(policyId, charge.tradeNo, result)
				}
			}
			if errors.Is(err, ErrTossBillingChargeCanceled) || errors.Is(err, ErrTossBillingChargeTerminal) {
				if result == nil || strings.TrimSpace(result.ProviderStatus) == "" {
					return markWalletAutoRechargeProviderPending(policyId, charge.tradeNo, errors.New("Toss terminal payment result is unavailable"))
				}
				requiresReconciliation := strings.EqualFold(strings.TrimSpace(result.ProviderStatus), "PARTIAL_CANCELED")
				return ApplyWalletAutoRechargeWebhookTerminal(
					policyId,
					charge.tradeNo,
					result.ProviderStatus,
					result.ProviderPayload,
					requiresReconciliation,
					now,
				)
			}
			if errors.Is(err, ErrTossBillingChargePending) {
				return markWalletAutoRechargeProviderPending(policyId, charge.tradeNo, err)
			}
			if errors.Is(err, ErrWalletAutoRechargeReconciliationRequired) {
				return markWalletAutoRechargeProviderPending(policyId, charge.tradeNo, err)
			}
			if errors.Is(err, ErrWalletAutoRechargeAttemptOutsideGrace) {
				settlementWon, closeErr := closeStaleWalletAutoRechargeAttempt(policyId, charge.tradeNo)
				if closeErr != nil {
					return closeErr
				}
				if !settlementWon {
					return err
				}
				charge.alreadyDone = true
			}
			if !charge.alreadyDone && (errors.Is(err, ErrTossBillingOperationallyDisabled) ||
				errors.Is(err, ErrWalletAutoRechargeClaimLost) ||
				errors.Is(err, ErrWalletAutoRechargeTargetInvalid) ||
				errors.Is(err, ErrTossBillingKeyInactive)) {
				// These errors mean that the last local authorization gate rejected
				// the provider POST. Keep the durable pending attempt intact so an
				// administrator action or a later re-enable can safely resume with
				// GET-before-POST on the same orderId.
				return err
			}
			if !charge.alreadyDone && (errors.Is(err, ErrTossRecurringOrderIDEvidenceCorrupt) ||
				errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) ||
				errors.Is(err, ErrTossBillingCrossCredentialRetryUnsafe)) {
				// Identity and API-key namespace conflicts are protocol failures, not
				// customer declines. A scoped association conflict is already halted;
				// global corrupt evidence must leave this potentially unrelated policy
				// untouched. Preserve the exact row and never advance FailCount.
				return err
			}
			if !charge.alreadyDone && charge.stalePending && (err != nil || result == nil || !result.Done || result.Total != charge.chargeKRW) {
				// Once the attempt is outside this application's operational replay
				// window, an
				// ambiguous lookup must retain this exact order for GET-only recovery.
				// Closing it here would let a later task create a different orderId.
				if err == nil {
					err = ErrTossBillingChargePending
				}
				return markWalletAutoRechargeProviderPending(policyId, charge.tradeNo, err)
			}
			if !charge.alreadyDone && (err != nil || result == nil || !result.Done || result.Total != charge.chargeKRW) {
				if err == nil {
					err = fmt.Errorf("toss wallet auto recharge amount mismatch")
				}
				return markWalletAutoRechargeChargeFailure(policyId, charge.tradeNo, maxFails, err)
			}
			if !charge.alreadyDone {
				// Settle the provider result through the same atomic recovery path as
				// an authoritative DONE webhook. Another worker can cross the grace
				// boundary and expire the TopUp after this worker's final POST gate but
				// before the provider responds; the settlement path deliberately accepts
				// pending/failed/expired states, credits exactly once, persists the
				// paymentKey/payload/event, and leaves a terminal policy terminal.
				// Persist the provider proof first. If local quota crediting fails, this
				// exact order remains locally settleable without another provider call.
				if err := RecordWalletAutoRechargeProviderDONEEvidenceWithContext(ctx, policyId, charge.tradeNo, result); err != nil {
					if !errors.Is(err, ErrWalletAutoRechargeReconciliationRequired) {
						_ = markWalletAutoRechargeReconciliationPending(policyId, charge.tradeNo, err)
					}
					return err
				}
				if err := SettleWalletAutoRechargeWebhookDoneWithContext(
					ctx,
					policyId,
					charge.tradeNo,
					result.PaymentKey,
					result.ProviderPayload,
					now,
				); err != nil {
					_ = markWalletAutoRechargeReconciliationPending(policyId, charge.tradeNo, err)
					return err
				}
				return nil
			}
		}
	}

	if err := completeWalletAutoRechargeTopUp(policyId, charge.tradeNo, now); err != nil {
		_ = markWalletAutoRechargeReconciliationPending(policyId, charge.tradeNo, err)
		_ = recordWalletAutoRechargeFulfillmentEventByTradeNo(charge.tradeNo)
		return err
	}
	if eventErr := recordWalletAutoRechargeFulfillmentEventByTradeNo(charge.tradeNo); eventErr != nil {
		_ = markWalletAutoRechargeEventResolutionPending(policyId, charge.tradeNo, eventErr)
		return eventErr
	}
	if resolveErr := ResolveTossPaymentEvents(charge.tradeNo, TossPaymentEventTypeFulfillment); resolveErr != nil {
		if markErr := markWalletAutoRechargeEventResolutionPending(policyId, charge.tradeNo, resolveErr); markErr != nil {
			return errors.Join(resolveErr, markErr)
		}
		return resolveErr
	}
	return nil
}

func ProcessWalletAutoRechargeWithConfiguredCharger(ctx context.Context, policyId int, now time.Time, maxFails int) error {
	return ProcessWalletAutoRecharge(ctx, policyId, now, maxFails, tossBillingCharger)
}

func walletAutoRechargeKRW(amount float64) int64 {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || amount != math.Trunc(amount) {
		return 0
	}
	charge := decimal.NewFromFloat(amount).Round(0)
	if charge.LessThan(decimal.NewFromInt(1)) || charge.GreaterThan(decimal.NewFromInt(setting.TossMaximumChargeAmountKRW)) {
		return 0
	}
	return charge.IntPart()
}

func walletAutoRechargeMoney(amountKRW float64) float64 {
	return TossUSDEquivalent(decimal.NewFromFloat(amountKRW).Round(0).IntPart())
}

func walletAutoRechargeQuota(amount float64) int64 {
	if amount <= 0 {
		return 0
	}
	return int64(TossCreditQuotaFromKRW(walletAutoRechargeKRW(amount)))
}

func walletAutoRechargeTradeNo(policy WalletAutoRecharge, now time.Time) string {
	return walletAutoRechargeTradeNoForAttempt(policy, now, policy.FailCount)
}

func walletAutoRechargeProcessLocalTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Unix(GetDBTimestamp(), 0).In(time.Local)
	}
	return now.In(time.Local)
}

func walletAutoRechargeProcessUTCTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Unix(GetDBTimestamp(), 0).UTC()
	}
	return now.UTC()
}

// walletAutoRechargeUTCDailyState returns the one canonical calendar bucket
// used by every threshold worker. DailyChargeDate predates the UTC marker and
// may therefore contain a date formatted in an unknown process-local timezone.
// Rebase those legacy rows without discarding real charge history: reset a
// positive counter only when LastChargeTime proves that its most recent charge
// happened before the current UTC day. Missing, future, or otherwise
// inconsistent evidence preserves the counter for the remainder of this UTC
// day, which is the fail-closed side of the migration.
func walletAutoRechargeUTCDailyState(policy *WalletAutoRecharge, now time.Time) (string, int) {
	if now.IsZero() {
		now = time.Unix(GetDBTimestamp(), 0)
	}
	utcNow := now.UTC()
	today := utcNow.Format("2006-01-02")
	utcMidnight := time.Date(utcNow.Year(), utcNow.Month(), utcNow.Day(), 0, 0, 0, 0, time.UTC).Unix()
	if policy == nil {
		return today, 0
	}

	count := policy.DailyChargeCount
	counterCorrupt := count < 0
	if counterCorrupt {
		// A negative financial counter is corrupt, not an empty day. Treat it as
		// exhausted so corruption cannot open three additional provider charges.
		count = WalletAutoRechargeDailyLimit
	}
	if policy.DailyChargeDateUTC {
		if policy.DailyChargeDate == today {
			return today, count
		}
		storedDay, err := time.ParseInLocation("2006-01-02", policy.DailyChargeDate, time.UTC)
		if !counterCorrupt && err == nil && storedDay.Unix() < utcMidnight &&
			(policy.LastChargeTime == 0 || policy.LastChargeTime > 0 && policy.LastChargeTime < utcMidnight) {
			// The marker makes an older canonical UTC date authoritative. Unlike an
			// unmarked legacy date, it can reset normally even if very old rows lack
			// LastChargeTime. A current/future/negative timestamp that contradicts
			// the date is corruption and remains fail-closed below.
			return today, 0
		}
		// A malformed/future date, or an older date contradicted by a current,
		// future, or negative charge timestamp, is canonical-state corruption.
		// Rebase the label but never reset its financial counter.
		return today, count
	}
	if count == 0 {
		return today, 0
	}
	if counterCorrupt {
		return today, count
	}

	if policy.LastChargeTime > 0 && policy.LastChargeTime < utcMidnight {
		return today, 0
	}
	return today, count
}

func walletAutoRechargeTradeNoForAttempt(policy WalletAutoRecharge, now time.Time, attempt int) string {
	var base string
	if policy.Type == WalletAutoRechargeTypeThreshold {
		// New logical attempts use the same hour on every master. The current-local
		// tuple remains a read alias below for rolling-upgrade legacy rows, but it
		// must not remain the primary identity or different process timezones can
		// authorize distinct retries for the same threshold cycle.
		base = fmt.Sprintf("wallet_auto_%d_%s_%d", policy.Id, walletAutoRechargeProcessUTCTime(now).Format("2006010215"), policy.DailyChargeCount+1)
	} else {
		base = fmt.Sprintf("wallet_auto_%d_%d", policy.Id, policy.NextChargeTime)
	}
	if attempt <= 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, attempt)
}

const (
	tossWalletOrderIDVersionLegacy = 1
	tossWalletOrderIDVersionOpaque = 2
)

type walletAutoRechargeAttemptIdentity struct {
	PolicyID int
	CycleKey string
	Attempt  int
}

func (identity walletAutoRechargeAttemptIdentity) valid() bool {
	return identity.PolicyID > 0 && identity.Attempt >= 0 &&
		strings.TrimSpace(identity.CycleKey) == identity.CycleKey && identity.CycleKey != "" && len(identity.CycleKey) <= 48
}

func walletAutoRechargeAttemptIdentitiesForPolicy(policy *WalletAutoRecharge, now time.Time) ([]walletAutoRechargeAttemptIdentity, error) {
	if policy == nil {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	return walletAutoRechargeAttemptIdentitiesForPolicyAttempt(policy, now, policy.FailCount)
}

func walletAutoRechargeAttemptIdentitiesForPolicyAttempt(policy *WalletAutoRecharge, now time.Time, attempt int) ([]walletAutoRechargeAttemptIdentity, error) {
	if policy == nil || policy.Id <= 0 || attempt < 0 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	identity := walletAutoRechargeAttemptIdentity{PolicyID: policy.Id, Attempt: attempt}
	if policy.Type == WalletAutoRechargeTypeThreshold {
		identity.CycleKey = fmt.Sprintf("t:%s:%d", walletAutoRechargeProcessUTCTime(now).Format("2006010215"), policy.DailyChargeCount+1)
	} else if policy.Type == WalletAutoRechargeTypeScheduled && policy.NextChargeTime > 0 {
		identity.CycleKey = fmt.Sprintf("s:%d", policy.NextChargeTime)
	} else {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if !identity.valid() {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	identities := []walletAutoRechargeAttemptIdentity{identity}
	if policy.Type == WalletAutoRechargeTypeThreshold {
		// Read the deployed process-local tuple during rolling upgrades. UTC stays
		// first and is the only tuple written by a new opaque-order attempt.
		localIdentity := identity
		localIdentity.CycleKey = fmt.Sprintf("t:%s:%d", walletAutoRechargeProcessLocalTime(now).Format("2006010215"), policy.DailyChargeCount+1)
		if localIdentity != identity {
			identities = append(identities, localIdentity)
		}
	}
	return identities, nil
}

func walletAutoRechargeThresholdIdentityMatchesCreateTime(identity walletAutoRechargeAttemptIdentity, createdAt time.Time) bool {
	threshold, ok := strings.CutPrefix(identity.CycleKey, "t:")
	if !ok || createdAt.IsZero() {
		return false
	}
	parts := strings.Split(threshold, ":")
	if len(parts) != 2 {
		return false
	}
	cycleHour, err := time.ParseInLocation("2006010215", parts[0], time.UTC)
	if err != nil {
		return false
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count <= 0 {
		return false
	}
	// A legacy cycle stores only the creator's formatted wall-clock hour, not
	// its timezone. Compare it with the persisted creation instant and accept
	// every real-world UTC offset (-12 through +14), including half/quarter-hour
	// zones whose minute component was discarded by the old format. Anything
	// outside that window is corrupt evidence and must halt before a POST.
	delta := cycleHour.Sub(createdAt.UTC().Truncate(time.Hour))
	return delta >= -12*time.Hour && delta <= 14*time.Hour
}

// walletAutoRechargeCompatibleAttemptIdentitiesForTopUp reconstructs every
// Phase-A logical alias from the persisted creation instant rather than from a
// later worker's clock. This is used at the final POST gate, where a local-hour
// row and the short-lived UTC-hour writer must still be treated as one charge.
func walletAutoRechargeCompatibleAttemptIdentitiesForTopUp(
	policy *WalletAutoRecharge,
	topUp *TopUp,
	identity walletAutoRechargeAttemptIdentity,
) ([]walletAutoRechargeAttemptIdentity, error) {
	if policy == nil || topUp == nil || identity.PolicyID != policy.Id || !identity.valid() {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if policy.Type == WalletAutoRechargeTypeScheduled {
		return normalizedWalletAutoRechargeAttemptIdentities([]walletAutoRechargeAttemptIdentity{identity})
	}
	if policy.Type != WalletAutoRechargeTypeThreshold || topUp.CreateTime <= 0 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	threshold, ok := strings.CutPrefix(identity.CycleKey, "t:")
	if !ok {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	parts := strings.Split(threshold, ":")
	if len(parts) != 2 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count <= 0 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	createdAt := time.Unix(topUp.CreateTime, 0)
	if !walletAutoRechargeThresholdIdentityMatchesCreateTime(identity, createdAt) {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	localIdentity := identity
	localIdentity.CycleKey = fmt.Sprintf("t:%s:%d", createdAt.In(time.Local).Format("2006010215"), count)
	utcIdentity := identity
	utcIdentity.CycleKey = fmt.Sprintf("t:%s:%d", createdAt.UTC().Format("2006010215"), count)
	identities, err := normalizedWalletAutoRechargeAttemptIdentities([]walletAutoRechargeAttemptIdentity{
		identity,
		localIdentity,
		utcIdentity,
	})
	if err != nil {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	return identities, nil
}

func walletAutoRechargeAttemptIdentityForPolicy(policy *WalletAutoRecharge, now time.Time) (walletAutoRechargeAttemptIdentity, error) {
	identities, err := walletAutoRechargeAttemptIdentitiesForPolicy(policy, now)
	if err != nil || len(identities) == 0 {
		return walletAutoRechargeAttemptIdentity{}, err
	}
	return identities[0], nil
}

func legacyWalletAutoRechargeTradeNo(identity walletAutoRechargeAttemptIdentity) (string, bool) {
	if !identity.valid() {
		return "", false
	}
	if scheduled, found := strings.CutPrefix(identity.CycleKey, "s:"); found {
		nextChargeTime, err := strconv.ParseInt(scheduled, 10, 64)
		if err != nil || nextChargeTime <= 0 {
			return "", false
		}
		base := fmt.Sprintf("wallet_auto_%d_%d", identity.PolicyID, nextChargeTime)
		if identity.Attempt <= 0 {
			return base, true
		}
		return fmt.Sprintf("%s_%d", base, identity.Attempt), true
	}
	threshold, found := strings.CutPrefix(identity.CycleKey, "t:")
	if !found {
		return "", false
	}
	parts := strings.Split(threshold, ":")
	if len(parts) != 2 || len(parts[0]) != 10 {
		return "", false
	}
	if _, err := time.Parse("2006010215", parts[0]); err != nil {
		return "", false
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil || count <= 0 {
		return "", false
	}
	base := fmt.Sprintf("wallet_auto_%d_%s_%d", identity.PolicyID, parts[0], count)
	if identity.Attempt <= 0 {
		return base, true
	}
	return fmt.Sprintf("%s_%d", base, identity.Attempt), true
}

func walletAutoRechargeLegacyTradeNoCandidates(identity walletAutoRechargeAttemptIdentity) ([]string, bool) {
	canonical, ok := legacyWalletAutoRechargeTradeNo(identity)
	if !ok {
		return nil, false
	}
	candidates := []string{canonical}
	if identity.Attempt == 0 {
		// Read the short-lived intermediate Phase-A writer that explicitly
		// appended `_0`; the deployed HEAD writer and the corrected Phase-A writer
		// both use the unsuffixed canonical form for attempt zero.
		candidates = append(candidates, canonical+"_0")
	}
	return candidates, true
}

func containsWalletAutoRechargeTradeNo(candidates []string, tradeNo string) bool {
	for _, candidate := range candidates {
		if tradeNo == candidate {
			return true
		}
	}
	return false
}

func parseLegacyWalletAutoRechargeTradeNoForPolicyType(tradeNo, policyType string) (walletAutoRechargeAttemptIdentity, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(tradeNo), "wallet_auto_")
	if !found {
		return walletAutoRechargeAttemptIdentity{}, false
	}
	parts := strings.Split(rest, "_")
	policyID, err := strconv.Atoi(parts[0])
	if err != nil || policyID <= 0 {
		return walletAutoRechargeAttemptIdentity{}, false
	}
	identity := walletAutoRechargeAttemptIdentity{PolicyID: policyID}
	switch policyType {
	case WalletAutoRechargeTypeScheduled:
		if len(parts) != 2 && len(parts) != 3 {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		nextChargeTime, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || nextChargeTime <= 0 {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		identity.CycleKey = fmt.Sprintf("s:%d", nextChargeTime)
		if len(parts) == 3 {
			identity.Attempt, err = strconv.Atoi(parts[2])
			if err != nil || identity.Attempt < 0 {
				return walletAutoRechargeAttemptIdentity{}, false
			}
		}
	case WalletAutoRechargeTypeThreshold:
		if len(parts) != 3 && len(parts) != 4 {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		if len(parts[1]) != 10 {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		if _, err := time.Parse("2006010215", parts[1]); err != nil {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		count, err := strconv.Atoi(parts[2])
		if err != nil || count <= 0 {
			return walletAutoRechargeAttemptIdentity{}, false
		}
		identity.CycleKey = fmt.Sprintf("t:%s:%d", parts[1], count)
		if len(parts) == 4 {
			identity.Attempt, err = strconv.Atoi(parts[3])
			if err != nil || identity.Attempt < 0 {
				return walletAutoRechargeAttemptIdentity{}, false
			}
		}
	default:
		return walletAutoRechargeAttemptIdentity{}, false
	}
	return identity, identity.valid()
}

func parseLegacyWalletAutoRechargeTradeNo(tradeNo string) (walletAutoRechargeAttemptIdentity, bool) {
	scheduled, scheduledOK := parseLegacyWalletAutoRechargeTradeNoForPolicyType(tradeNo, WalletAutoRechargeTypeScheduled)
	threshold, thresholdOK := parseLegacyWalletAutoRechargeTradeNoForPolicyType(tradeNo, WalletAutoRechargeTypeThreshold)
	if scheduledOK == thresholdOK {
		// A three-part ID can be either a scheduled retry or a HEAD threshold
		// attempt. Callers with policy context must use the typed parser instead of
		// guessing across provider identities.
		return walletAutoRechargeAttemptIdentity{}, false
	}
	if scheduledOK {
		return scheduled, true
	}
	return threshold, true
}

func topUpWalletAutoRechargeAttemptIdentity(topUp *TopUp) (walletAutoRechargeAttemptIdentity, bool, error) {
	if topUp == nil {
		return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	associationCount := 0
	if topUp.WalletAutoRechargeId != nil {
		associationCount++
	}
	if topUp.WalletAutoRechargeCycleKey != nil {
		associationCount++
	}
	if topUp.WalletAutoRechargeAttempt != nil {
		associationCount++
	}
	if associationCount == 0 {
		if HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo) {
			return walletAutoRechargeAttemptIdentity{}, false, ErrTossRecurringOrderIDEvidenceCorrupt
		}
		// Opaque order IDs deliberately do not encode the wallet policy/cycle.
		// A v2 row without all association columns is corrupt and must never be
		// reclassified merely because its text also parses as a legacy ID.
		if topUp.WalletOrderIdVersion != 0 {
			return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		identity, ok := parseLegacyWalletAutoRechargeTradeNo(topUp.TradeNo)
		if !ok {
			return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		return identity, false, nil
	}
	if associationCount != 3 {
		return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	identity := walletAutoRechargeAttemptIdentity{
		PolicyID: *topUp.WalletAutoRechargeId,
		CycleKey: *topUp.WalletAutoRechargeCycleKey,
		Attempt:  *topUp.WalletAutoRechargeAttempt,
	}
	if !identity.valid() {
		return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	switch topUp.WalletOrderIdVersion {
	case 0, tossWalletOrderIDVersionLegacy:
		candidates, ok := walletAutoRechargeLegacyTradeNoCandidates(identity)
		if !ok || !containsWalletAutoRechargeTradeNo(candidates, topUp.TradeNo) {
			return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
	case tossWalletOrderIDVersionOpaque:
		if !validTossWalletOpaqueOrderID(topUp.TradeNo) {
			return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
	default:
		return walletAutoRechargeAttemptIdentity{}, false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	return identity, true, nil
}

func walletAutoRechargeTopUpBelongsToPolicy(topUp *TopUp, policy *WalletAutoRecharge) (bool, error) {
	if topUp == nil || policy == nil {
		return false, nil
	}
	if HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo) &&
		(topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss) {
		return false, ErrTossRecurringOrderIDEvidenceCorrupt
	}
	if topUp.PaymentProvider != PaymentProviderToss {
		return false, nil
	}
	if topUp.PaymentMethod != PaymentMethodToss {
		return false, nil
	}
	associationPresent := topUp.WalletAutoRechargeId != nil ||
		topUp.WalletAutoRechargeCycleKey != nil || topUp.WalletAutoRechargeAttempt != nil
	if associationPresent {
		// Association-bearing rows are written by the new protocol and must carry
		// the complete immutable wallet target. Do not let a valid logical-attempt
		// tuple reassign a charge across users or organizations.
		if topUp.TargetType != policy.TargetType || topUp.TargetId != policy.TargetId || topUp.UserId != policy.OwnerUserId {
			return false, nil
		}
		identity, _, err := topUpWalletAutoRechargeAttemptIdentity(topUp)
		if err != nil {
			return false, err
		}
		return identity.PolicyID == policy.Id, nil
	}
	if topUp.WalletOrderIdVersion != 0 {
		// Opaque order IDs have no trustworthy policy identity without the full
		// association tuple. Do not let a corrupt v2 row whose text mimics the
		// legacy prefix enter settlement, cancellation, or recovery paths.
		return false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo) {
		return false, ErrTossRecurringOrderIDEvidenceCorrupt
	}
	// Historical rows predate target snapshots. GORM materializes their empty
	// target_type as "user", while equally old policies may still contain an
	// empty target, so raw target equality would misclassify a wallet charge as
	// a generic top-up. Preserve the exact legacy order namespace and, whenever
	// both sides carry owner evidence, require it independently as well.
	if !strings.HasPrefix(topUp.TradeNo, fmt.Sprintf("wallet_auto_%d_", policy.Id)) {
		return false, nil
	}
	if topUp.UserId > 0 && policy.OwnerUserId > 0 && topUp.UserId != policy.OwnerUserId {
		return false, nil
	}
	return true, nil
}

func walletAutoRechargeIdentityInSet(identity walletAutoRechargeAttemptIdentity, candidates []walletAutoRechargeAttemptIdentity) bool {
	for _, candidate := range candidates {
		if identity == candidate {
			return true
		}
	}
	return false
}

func normalizedWalletAutoRechargeAttemptIdentities(identities []walletAutoRechargeAttemptIdentity) ([]walletAutoRechargeAttemptIdentity, error) {
	result := make([]walletAutoRechargeAttemptIdentity, 0, len(identities))
	for _, identity := range identities {
		if !identity.valid() {
			return nil, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		if !walletAutoRechargeIdentityInSet(identity, result) {
			result = append(result, identity)
		}
	}
	if len(result) == 0 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PolicyID != result[j].PolicyID {
			return result[i].PolicyID < result[j].PolicyID
		}
		if result[i].CycleKey != result[j].CycleKey {
			return result[i].CycleKey < result[j].CycleKey
		}
		return result[i].Attempt < result[j].Attempt
	})
	return result, nil
}

func findWalletAutoRechargeTopUpForIdentitiesTx(tx *gorm.DB, identities []walletAutoRechargeAttemptIdentity) (*TopUp, error) {
	if tx == nil {
		tx = DB
	}
	identities, err := normalizedWalletAutoRechargeAttemptIdentities(identities)
	if err != nil {
		return nil, err
	}

	associatedQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&TopUp{})
	for i, identity := range identities {
		condition := "(wallet_auto_recharge_id = ? AND wallet_auto_recharge_cycle_key = ? AND wallet_auto_recharge_attempt = ?)"
		if i == 0 {
			associatedQuery = associatedQuery.Where(condition, identity.PolicyID, identity.CycleKey, identity.Attempt)
		} else {
			associatedQuery = associatedQuery.Or(condition, identity.PolicyID, identity.CycleKey, identity.Attempt)
		}
	}
	var associatedRows []TopUp
	if err := associatedQuery.Order("wallet_auto_recharge_cycle_key asc, id asc").Limit(len(identities) + 1).Find(&associatedRows).Error; err != nil {
		return nil, err
	}

	tradeIdentity := make(map[string]walletAutoRechargeAttemptIdentity, len(identities)*2)
	tradeNos := make([]string, 0, len(identities)*2)
	for _, identity := range identities {
		candidates, ok := walletAutoRechargeLegacyTradeNoCandidates(identity)
		if !ok {
			return nil, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		for _, tradeNo := range candidates {
			if existing, found := tradeIdentity[tradeNo]; found {
				if existing != identity {
					return nil, ErrWalletAutoRechargeAttemptAssociationConflict
				}
				continue
			}
			tradeIdentity[tradeNo] = identity
			tradeNos = append(tradeNos, tradeNo)
		}
	}
	sort.Strings(tradeNos)
	var legacyRows []TopUp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("trade_no IN ?", tradeNos).
		Order("trade_no asc, id asc").
		Limit(len(tradeNos) + 1).
		Find(&legacyRows).Error; err != nil {
		return nil, err
	}

	physicalRows := make(map[int]TopUp, len(associatedRows)+len(legacyRows))
	for i := range associatedRows {
		physicalRows[associatedRows[i].Id] = associatedRows[i]
	}
	for i := range legacyRows {
		physicalRows[legacyRows[i].Id] = legacyRows[i]
	}
	if len(physicalRows) > 1 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if len(physicalRows) == 0 {
		return nil, nil
	}
	var row TopUp
	for _, candidate := range physicalRows {
		row = candidate
		break
	}
	if row.PaymentProvider != PaymentProviderToss || row.PaymentMethod != PaymentMethodToss {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}

	associationCount := 0
	if row.WalletAutoRechargeId != nil {
		associationCount++
	}
	if row.WalletAutoRechargeCycleKey != nil {
		associationCount++
	}
	if row.WalletAutoRechargeAttempt != nil {
		associationCount++
	}
	if associationCount != 0 && associationCount != 3 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if associationCount == 3 {
		resolved, _, identityErr := topUpWalletAutoRechargeAttemptIdentity(&row)
		if identityErr != nil || !walletAutoRechargeIdentityInSet(resolved, identities) {
			return nil, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		return &row, nil
	}
	if row.WalletOrderIdVersion != 0 {
		// Exact legacy-candidate lookup intentionally bypasses the generic parser
		// to resolve the scheduled/threshold ambiguity. Preserve the same opaque
		// invariant here before that specialized all-NULL backfill path.
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}

	identity, found := tradeIdentity[row.TradeNo]
	if !found {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	// This all-NULL row is deliberately resolved through the exact candidate
	// map above. Calling the generic parser here would reintroduce the ambiguous
	// scheduled-retry/threshold-HEAD interpretation this bridge avoids.
	result := tx.Model(&TopUp{}).
		Where("id = ? AND wallet_auto_recharge_id IS NULL AND wallet_auto_recharge_cycle_key IS NULL AND wallet_auto_recharge_attempt IS NULL", row.Id).
		Updates(map[string]interface{}{
			"wallet_auto_recharge_id":        identity.PolicyID,
			"wallet_auto_recharge_cycle_key": identity.CycleKey,
			"wallet_auto_recharge_attempt":   identity.Attempt,
			"wallet_order_id_version":        tossWalletOrderIDVersionLegacy,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		var concurrent TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.Id).First(&concurrent).Error; err != nil {
			return nil, err
		}
		resolved, _, identityErr := topUpWalletAutoRechargeAttemptIdentity(&concurrent)
		if identityErr != nil || !walletAutoRechargeIdentityInSet(resolved, identities) {
			return nil, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		return &concurrent, nil
	}
	row.WalletAutoRechargeId = &identity.PolicyID
	row.WalletAutoRechargeCycleKey = &identity.CycleKey
	row.WalletAutoRechargeAttempt = &identity.Attempt
	row.WalletOrderIdVersion = tossWalletOrderIDVersionLegacy
	return &row, nil
}

func findWalletAutoRechargeTopUpForIdentityTx(tx *gorm.DB, identity walletAutoRechargeAttemptIdentity) (*TopUp, error) {
	return findWalletAutoRechargeTopUpForIdentitiesTx(tx, []walletAutoRechargeAttemptIdentity{identity})
}

func walletAutoRechargeCycleKeyInSet(cycleKey string, candidates []walletAutoRechargeAttemptIdentity) bool {
	for _, candidate := range candidates {
		if candidate.CycleKey == cycleKey {
			return true
		}
	}
	return false
}

// ensureNoIncompleteWalletAutoRechargeOrderIdentityTx is the wallet analogue
// of the subscription protocol corruption fence. Once a row advertises a
// nonzero order-ID protocol, all three immutable association columns are
// mandatory. A global fence is intentional: an incomplete opaque row cannot
// be reliably scoped back to its policy, so a narrower query could authorize
// a replacement charge for the hidden logical attempt.
func ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(tx *gorm.DB) error {
	if tx == nil {
		tx = DB
	}
	if tx == nil {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	var rows []TopUp
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("wallet_auto_recharge_id IS NULL OR wallet_auto_recharge_cycle_key IS NULL OR wallet_auto_recharge_attempt IS NULL").
		Where("trade_no LIKE ? ESCAPE '!' OR (payment_provider = ? AND (wallet_order_id_version <> ? OR wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL))",
			tossWalletOpaqueOrderIDLikePattern(), PaymentProviderToss, 0).
		Order("id asc").Limit(1).Find(&rows).Error
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return ErrTossRecurringOrderIDEvidenceCorrupt
}

// resolveWalletAutoRechargeEffectiveAttemptTx allows a suffix only when every
// lower physical order in the cycle is durably terminal and the locked policy
// failure count proves that terminal transition was committed. This keeps a
// mixed Phase-A deployment safe: the old worker always opens attempt zero, and
// a terminal base row makes that old path stop before its provider POST.
func resolveWalletAutoRechargeEffectiveAttemptTx(
	tx *gorm.DB,
	policy *WalletAutoRecharge,
	cycleIdentities []walletAutoRechargeAttemptIdentity,
) (int, error) {
	if tx == nil || policy == nil || policy.Id <= 0 || policy.FailCount < 0 {
		return 0, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if err := ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(tx); err != nil {
		return 0, err
	}
	cycleIdentities, err := normalizedWalletAutoRechargeAttemptIdentities(cycleIdentities)
	if err != nil {
		return 0, err
	}
	cycleKeys := make([]string, 0, len(cycleIdentities))
	legacyBases := make([]string, 0, len(cycleIdentities))
	for _, identity := range cycleIdentities {
		if identity.PolicyID != policy.Id {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		cycleKeys = append(cycleKeys, identity.CycleKey)
		baseIdentity := identity
		baseIdentity.Attempt = 0
		base, ok := legacyWalletAutoRechargeTradeNo(baseIdentity)
		if !ok {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		if !containsWalletAutoRechargeTradeNo(legacyBases, base) {
			legacyBases = append(legacyBases, base)
		}
	}

	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&TopUp{}).
		Where("wallet_auto_recharge_id = ? AND wallet_auto_recharge_cycle_key IN ?", policy.Id, cycleKeys)
	for _, base := range legacyBases {
		escapedBase := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(base)
		query = query.Or("trade_no = ? OR trade_no LIKE ? ESCAPE '!'", base, escapedBase+"!_%")
	}
	var rows []TopUp
	if err := query.Order("id asc").Limit(102).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) > 100 {
		return 0, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	byAttempt := make(map[int]TopUp, len(rows))
	maxAttempt := -1
	for i := range rows {
		if rows[i].PaymentProvider != PaymentProviderToss || rows[i].PaymentMethod != PaymentMethodToss {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		associationPresent := rows[i].WalletAutoRechargeId != nil ||
			rows[i].WalletAutoRechargeCycleKey != nil || rows[i].WalletAutoRechargeAttempt != nil
		var identity walletAutoRechargeAttemptIdentity
		if associationPresent {
			var identityErr error
			identity, _, identityErr = topUpWalletAutoRechargeAttemptIdentity(&rows[i])
			if identityErr != nil {
				return 0, identityErr
			}
		} else {
			if rows[i].WalletOrderIdVersion != 0 {
				return 0, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			var ok bool
			identity, ok = parseLegacyWalletAutoRechargeTradeNoForPolicyType(rows[i].TradeNo, policy.Type)
			if !ok {
				return 0, ErrWalletAutoRechargeAttemptAssociationConflict
			}
		}
		if identity.PolicyID != policy.Id || !walletAutoRechargeCycleKeyInSet(identity.CycleKey, cycleIdentities) ||
			identity.Attempt > policy.FailCount {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		if previous, exists := byAttempt[identity.Attempt]; exists && previous.Id != rows[i].Id {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		byAttempt[identity.Attempt] = rows[i]
		if identity.Attempt > maxAttempt {
			maxAttempt = identity.Attempt
		}
	}
	if maxAttempt < 0 {
		return 0, nil
	}
	for attempt := 0; attempt <= maxAttempt; attempt++ {
		row, exists := byAttempt[attempt]
		if !exists {
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		switch row.Status {
		case common.TopUpStatusFailed, common.TopUpStatusExpired:
			if attempt >= policy.FailCount || hasWalletAutoRechargeRealProviderPaymentKey(&row) || isLegacyChargedWalletAutoRechargeTopUp(&row) ||
				strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix) ||
				strings.HasPrefix(policy.LastError, walletAutoRechargeReconciliationPendingPrefix) {
				return 0, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			continue
		case common.TopUpStatusPending, common.TopUpStatusSuccess:
			if attempt != maxAttempt {
				return 0, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			return attempt, nil
		default:
			return 0, ErrWalletAutoRechargeAttemptAssociationConflict
		}
	}
	return maxAttempt + 1, nil
}

func isWalletAutoRechargeLegacyBridgeAlias(topUp *TopUp) bool {
	if topUp == nil || topUp.WalletOrderIdVersion == tossWalletOrderIDVersionOpaque {
		return false
	}
	identity, _, err := topUpWalletAutoRechargeAttemptIdentity(topUp)
	if err != nil {
		return true
	}
	canonical, ok := legacyWalletAutoRechargeTradeNo(identity)
	if !ok || topUp.TradeNo != canonical {
		return true
	}
	threshold, isThreshold := strings.CutPrefix(identity.CycleKey, "t:")
	if !isThreshold {
		return false
	}
	if topUp.CreateTime <= 0 {
		return true
	}
	parts := strings.Split(threshold, ":")
	if len(parts) != 2 {
		return true
	}
	// The deployed writer formats the process-local hour immediately before
	// inserting the row. A different stored hour identifies the short-lived UTC
	// bridge writer; it is GET-only because an old worker may still create the
	// canonical local-hour order.
	return parts[0] != time.Unix(topUp.CreateTime, 0).In(time.Local).Format("2006010215")
}

// walletAutoRechargeTradeNoLikePattern escapes every literal underscore in the
// generated order prefix. Without an explicit ESCAPE clause, SQL LIKE treats
// underscores as single-character wildcards, so policy 12 could accidentally
// discover policy 123's pending order for the same wallet target.
func walletAutoRechargeTradeNoLikePattern(policyID int) string {
	return fmt.Sprintf("wallet!_auto!_%d!_%%", policyID)
}

func walletAutoRechargeActiveKey(targetType string, targetId int, rechargeType string) string {
	return fmt.Sprintf("%s:%d:%s", targetType, targetId, rechargeType)
}

// walletAutoRechargeMessageNeedsReplacementFence identifies a financial halt
// whose provider outcome is not authoritative enough to permit another policy
// for the same wallet slot. Event-resolution failures happen after local
// settlement and therefore do not represent an unresolved card charge.
func walletAutoRechargeMessageNeedsReplacementFence(message string) bool {
	return strings.HasPrefix(message, walletAutoRechargeManualReconciliationPrefix) ||
		strings.HasPrefix(message, walletAutoRechargeLateDONEFencePrefix) ||
		(strings.HasPrefix(message, walletAutoRechargeReconciliationPendingPrefix) &&
			!strings.HasPrefix(message, walletAutoRechargeEventResolutionPendingPrefix))
}

func walletAutoRechargePolicyNeedsReplacementFence(policy *WalletAutoRecharge) bool {
	return policy != nil && walletAutoRechargeMessageNeedsReplacementFence(policy.LastError)
}

func walletAutoRechargeHasNonReleasableReconciliationFence(lastError string) bool {
	return strings.HasPrefix(lastError, walletAutoRechargeManualReconciliationPrefix) ||
		(strings.HasPrefix(lastError, walletAutoRechargeReconciliationPendingPrefix) &&
			!strings.HasPrefix(lastError, walletAutoRechargeEventResolutionPendingPrefix))
}

// walletAutoRechargeHasPermanentAssociationFence separates evidence that an
// administrator can close by resolving a concrete Toss payment event from an
// ambiguous local association. An event decision is exact to one order/payment
// snapshot; it must never clear a marker that says more than one physical order
// may belong to the same logical wallet attempt.
func walletAutoRechargeHasPermanentAssociationFence(lastError string) bool {
	if !strings.HasPrefix(lastError, walletAutoRechargeManualReconciliationPrefix) {
		return false
	}
	reason := strings.TrimSpace(strings.TrimPrefix(lastError, walletAutoRechargeManualReconciliationPrefix))
	return !strings.HasPrefix(reason, "authoritative cancellation event blocks automatic charge") &&
		!strings.HasPrefix(reason, "authoritative cancellation event requires admin reconciliation") &&
		!strings.HasPrefix(reason, "provider cancellation payload mismatch")
}

func walletAutoRechargePolicyHasUnresolvedPaymentFence(policy *WalletAutoRecharge) bool {
	if policy == nil {
		return false
	}
	if policy.LastChargeTime > 0 && (policy.LastError == "" ||
		strings.HasPrefix(policy.LastError, walletAutoRechargeEventResolutionPendingPrefix)) {
		return false
	}
	return policy.Status == WalletAutoRechargeStatusCancelPending ||
		strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix) ||
		walletAutoRechargePolicyNeedsReplacementFence(policy)
}

func walletAutoRechargeTopUpHasUnresolvedMoneyEvidence(topUp *TopUp) bool {
	if topUp == nil || topUp.Status == common.TopUpStatusSuccess {
		return false
	}
	if topUp.Status == common.TopUpStatusPending || topUp.Status == TossTopUpStatusRefundPending {
		return true
	}
	providerOrderID := strings.TrimSpace(topUp.ProviderOrderId)
	if providerOrderID == topUp.TradeNo+":terminal" {
		return false
	}
	return topUp.ProviderAttempted || topUp.WalletOrderIdVersion != tossWalletOrderIDVersionOpaque ||
		hasWalletAutoRechargeRealProviderPaymentKey(topUp) || isLegacyChargedWalletAutoRechargeTopUp(topUp) ||
		topUp.ProviderClaimTime > 0 || strings.TrimSpace(topUp.ProviderClaimToken) != "" ||
		topUp.ProviderOrderTime > 0 || strings.TrimSpace(topUp.ProviderPayload) != ""
}

func walletAutoRechargePaymentEventScopeTx(tx *gorm.DB, policy *WalletAutoRecharge) *gorm.DB {
	associatedOrders := tx.Model(&TopUp{}).
		Select("trade_no").
		Where("wallet_auto_recharge_id = ?", policy.Id)
	query := tx.Model(&TossPaymentEvent{}).
		Where("event_type IN ?", []string{TossPaymentEventTypeCancellation, TossPaymentEventTypeFinancialMismatch})
	if strings.TrimSpace(policy.LastTradeNo) != "" {
		return query.Where(
			"order_id LIKE ? ESCAPE '!' OR order_id IN (?) OR order_id = ?",
			walletAutoRechargeTradeNoLikePattern(policy.Id), associatedOrders, policy.LastTradeNo,
		)
	}
	return query.Where(
		"order_id LIKE ? ESCAPE '!' OR order_id IN (?)",
		walletAutoRechargeTradeNoLikePattern(policy.Id), associatedOrders,
	)
}

func walletAutoRechargeRequiredPaymentEventCountTx(tx *gorm.DB, policy *WalletAutoRecharge) (int64, error) {
	if tx == nil || policy == nil || policy.Id <= 0 || !tx.Migrator().HasTable(&TossPaymentEvent{}) {
		return 0, nil
	}
	var count int64
	err := walletAutoRechargePaymentEventScopeTx(tx, policy).
		Where("reconciliation_status = ?", TossReconciliationStatusRequired).
		Count(&count).Error
	return count, err
}

func walletAutoRechargeHasUnresolvedAssociatedTopUpTx(tx *gorm.DB, policy *WalletAutoRecharge) (bool, error) {
	if tx == nil || policy == nil || policy.Id <= 0 {
		return false, nil
	}
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("payment_provider = ? AND payment_method = ?", PaymentProviderToss, PaymentMethodToss)
	if strings.TrimSpace(policy.LastTradeNo) != "" {
		query = query.Where(
			"wallet_auto_recharge_id = ? OR trade_no LIKE ? ESCAPE '!' OR trade_no = ?",
			policy.Id, walletAutoRechargeTradeNoLikePattern(policy.Id), policy.LastTradeNo,
		)
	} else {
		query = query.Where(
			"wallet_auto_recharge_id = ? OR trade_no LIKE ? ESCAPE '!'",
			policy.Id, walletAutoRechargeTradeNoLikePattern(policy.Id),
		)
	}
	var topUps []TopUp
	if err := query.Order("id asc").Find(&topUps).Error; err != nil {
		return false, err
	}
	for i := range topUps {
		belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUps[i], policy)
		if err != nil || !belongs {
			// A row selected by the immutable policy association or exact legacy
			// namespace but failing validation is itself unresolved alias evidence.
			return true, nil
		}
		if walletAutoRechargeTopUpHasUnresolvedMoneyEvidence(&topUps[i]) {
			return true, nil
		}
	}
	return false, nil
}

// terminalizeWalletAutoRechargeNeverAuthorizedTopUpsTx closes only v2 rows
// that provably never crossed the final provider-POST gate. Event persistence,
// admin resolution, and that gate share the owner root, so ProviderAttempted
// cannot turn true behind this transaction. Legacy rows and any claim/order/
// payload evidence remain fenced because older writers did not have this proof.
func terminalizeWalletAutoRechargeNeverAuthorizedTopUpsTx(tx *gorm.DB, policy *WalletAutoRecharge, exceptTradeNo string) error {
	if tx == nil || policy == nil || policy.Id <= 0 {
		return nil
	}
	var topUps []TopUp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("wallet_auto_recharge_id = ? AND payment_provider = ? AND payment_method = ? AND status = ?",
			policy.Id, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending).
		Order("id asc").Find(&topUps).Error; err != nil {
		return err
	}
	now := getDBTimestampTx(tx)
	for i := range topUps {
		topUp := &topUps[i]
		if topUp.TradeNo == strings.TrimSpace(exceptTradeNo) ||
			topUp.WalletOrderIdVersion != tossWalletOrderIDVersionOpaque ||
			topUp.WalletAutoRechargeId == nil || topUp.WalletAutoRechargeCycleKey == nil || topUp.WalletAutoRechargeAttempt == nil ||
			topUp.ProviderAttempted || topUp.ProviderClaimTime > 0 || strings.TrimSpace(topUp.ProviderClaimToken) != "" ||
			topUp.ProviderOrderTime > 0 || strings.TrimSpace(topUp.ProviderPayload) != "" ||
			hasWalletAutoRechargeRealProviderPaymentKey(topUp) || isWalletAutoRechargeLocallySettleable(topUp) ||
			(strings.TrimSpace(topUp.ProviderOrderId) != "" && topUp.ProviderOrderId != topUp.TradeNo) {
			continue
		}
		belongs, err := walletAutoRechargeTopUpBelongsToPolicy(topUp, policy)
		if err != nil || !belongs {
			continue
		}
		result := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND provider_attempted = ?", topUp.Id, common.TopUpStatusPending, false).
			Where("(provider_claim_time = 0 OR provider_claim_time IS NULL) AND (provider_claim_token = '' OR provider_claim_token IS NULL)").
			Where("(provider_order_time = 0 OR provider_order_time IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)").
			Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", topUp.TradeNo}).
			Updates(map[string]interface{}{
				"provider_order_id":    topUp.TradeNo + ":terminal",
				"provider_order_time":  now,
				"provider_retry_time":  0,
				"provider_claim_token": "",
				"provider_claim_time":  0,
				"complete_time":        now,
				"status":               common.TopUpStatusFailed,
			})
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func walletAutoRechargeResolvedEventReplayTx(tx *gorm.DB, policy *WalletAutoRecharge, eventKey, tradeNo, eventType, providerStatus string) (bool, bool, error) {
	if tx == nil || policy == nil || policy.Id <= 0 || !tx.Migrator().HasTable(&TossPaymentEvent{}) {
		return false, false, nil
	}
	query := tx.Model(&TossPaymentEvent{}).
		Where("reconciliation_status = ? AND event_type = ?", TossReconciliationStatusResolved, eventType)
	if strings.TrimSpace(eventKey) != "" {
		query = query.Where("event_key = ?", eventKey)
	} else {
		query = query.Where("order_id = ? AND status = ?", strings.TrimSpace(tradeNo), strings.ToUpper(strings.TrimSpace(providerStatus)))
	}
	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil || count == 0 {
		return false, false, err
	}
	remaining, err := walletAutoRechargeRequiredPaymentEventCountTx(tx, policy)
	if err != nil {
		return false, false, err
	}
	return true, remaining > 0, nil
}

// ensureNoOtherWalletAutoRechargePaymentFenceTx is the target-wide backstop
// behind the type-scoped active_key uniqueness. Scheduled and threshold
// policies are normally independent, but neither may create/authorize a new
// provider charge while the other type has unresolved money evidence.
// Callers hold the shared owner/organization lifecycle root before invoking it.
func ensureNoOtherWalletAutoRechargePaymentFenceTx(tx *gorm.DB, targetType string, targetID, currentPolicyID int, currentTradeNo string) error {
	if tx == nil || targetID <= 0 {
		return ErrWalletAutoRechargeTargetInvalid
	}
	var policies []WalletAutoRecharge
	policyQuery := tx.Select("id", "active_key", "status", "last_error", "last_charge_time", "last_trade_no").
		Where("target_type = ? AND target_id = ?", targetType, targetID)
	if err := policyQuery.Find(&policies).Error; err != nil {
		return err
	}
	for i := range policies {
		if policies[i].Id != currentPolicyID && walletAutoRechargePolicyHasUnresolvedPaymentFence(&policies[i]) {
			return ErrWalletAutoRechargeReconciliationRequired
		}
		// Event persistence is the durable hand-off before terminal Apply. Scan
		// it independently of policy/TopUp status so an old successful charge
		// with a newly observed partial cancellation cannot leave a crash window
		// in which a replacement sends another provider POST. New wallet event
		// writers also take the owner root, making this final-gate read serialize
		// with insertion; the scan remains the rolling-upgrade backstop.
		requiredEvents, err := walletAutoRechargeRequiredPaymentEventCountTx(tx, &policies[i])
		if err != nil {
			return err
		}
		if requiredEvents > 0 {
			return ErrWalletAutoRechargeReconciliationRequired
		}
	}
	if !tx.Migrator().HasTable(&TopUp{}) {
		return nil
	}

	var topUps []TopUp
	if err := tx.Where("target_type = ? AND target_id = ? AND payment_provider = ? AND payment_method = ? AND status IN ?",
		targetType, targetID, PaymentProviderToss, PaymentMethodToss, []string{
			common.TopUpStatusPending,
			common.TopUpStatusFailed,
			common.TopUpStatusExpired,
			TossTopUpStatusRefundPending,
		}).
		Where("wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL OR "+
			"wallet_order_id_version <> ? OR trade_no LIKE ? ESCAPE '!' OR trade_no LIKE ? ESCAPE '!'",
			0, "wallet!_auto!_%", tossWalletOpaqueOrderIDLikePattern()).
		Order("id asc").
		Find(&topUps).Error; err != nil {
		return err
	}
	currentTradeNo = strings.TrimSpace(currentTradeNo)
	for i := range topUps {
		if currentTradeNo != "" && topUps[i].TradeNo == currentTradeNo {
			continue
		}
		if !walletAutoRechargeTopUpHasUnresolvedMoneyEvidence(&topUps[i]) {
			continue
		}
		return ErrWalletAutoRechargeReconciliationRequired
	}
	return nil
}

func walletAutoRechargeCanonicalActiveKey(policy *WalletAutoRecharge) (string, error) {
	if policy == nil || policy.Id <= 0 || policy.TargetId <= 0 ||
		(policy.TargetType != TopUpTargetTypeUser && policy.TargetType != TopUpTargetTypeOrganization) ||
		(policy.Type != WalletAutoRechargeTypeScheduled && policy.Type != WalletAutoRechargeTypeThreshold) {
		return "", ErrWalletAutoRechargeAttemptAssociationConflict
	}
	return walletAutoRechargeActiveKey(policy.TargetType, policy.TargetId, policy.Type), nil
}

// walletAutoRechargeAvailableFenceKeyTx returns the canonical unique fence
// only when another policy has not already taken that type slot. A late
// cancellation/mismatch for an older policy must still persist its financial
// marker when a replacement exists; the target-wide marker gate then blocks
// that replacement's next POST without rolling back on a unique-key conflict.
func walletAutoRechargeAvailableFenceKeyTx(tx *gorm.DB, policy *WalletAutoRecharge) (interface{}, error) {
	key, err := walletAutoRechargeCanonicalActiveKey(policy)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := tx.Model(&WalletAutoRecharge{}).
		Where("id <> ? AND active_key = ?", policy.Id, key).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}
	return key, nil
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
	if tx == nil || policy == nil {
		return false, ErrWalletAutoRechargeTargetInvalid
	}
	if now.IsZero() {
		now = time.Unix(GetDBTimestamp(), 0)
	}
	today, dailyChargeCount := walletAutoRechargeUTCDailyState(policy, now)
	dailyStateChanged := policy.DailyChargeDate != today ||
		policy.DailyChargeCount != dailyChargeCount || !policy.DailyChargeDateUTC
	policy.DailyChargeDate = today
	policy.DailyChargeCount = dailyChargeCount
	policy.DailyChargeDateUTC = true
	if dailyStateChanged && policy.Id > 0 {
		if err := tx.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
			"daily_charge_date":     policy.DailyChargeDate,
			"daily_charge_count":    policy.DailyChargeCount,
			"daily_charge_date_utc": true,
		}).Error; err != nil {
			return false, err
		}
	}

	if policy.CooldownUntil > now.Unix() {
		return false, nil
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

func validateWalletAutoRechargeChargeTarget(tx *gorm.DB, policy *WalletAutoRecharge) error {
	var owner User
	if err := tx.Select("id", "status", "role", "organization_id", "organization_role").Where("id = ?", policy.OwnerUserId).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: owner no longer exists", ErrWalletAutoRechargeTargetInvalid)
		}
		return err
	}
	if owner.Status != common.UserStatusEnabled {
		return fmt.Errorf("%w: owner is disabled", ErrWalletAutoRechargeTargetInvalid)
	}
	if owner.Role < common.RoleCommonUser || !common.IsValidateRole(owner.Role) {
		return fmt.Errorf("%w: owner role is invalid", ErrWalletAutoRechargeTargetInvalid)
	}

	switch policy.TargetType {
	case TopUpTargetTypeUser:
		if policy.TargetId != owner.Id {
			return fmt.Errorf("%w: user owner mismatch", ErrWalletAutoRechargeTargetInvalid)
		}
		if owner.OrganizationId > 0 {
			return fmt.Errorf("%w: personal wallet is inactive during organization membership", ErrWalletAutoRechargeTargetInvalid)
		}
	case TopUpTargetTypeOrganization:
		var org Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "owner_user_id", "status").Where("id = ?", policy.TargetId).First(&org).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: organization no longer exists", ErrWalletAutoRechargeTargetInvalid)
			}
			return err
		}
		if org.Status != OrganizationStatusEnabled {
			return fmt.Errorf("%w: organization is disabled", ErrWalletAutoRechargeTargetInvalid)
		}
		if org.OwnerUserId != policy.OwnerUserId || owner.OrganizationId != org.Id || owner.OrganizationRole != OrganizationRoleOwner {
			return fmt.Errorf("%w: organization owner mismatch", ErrWalletAutoRechargeTargetInvalid)
		}
	default:
		return fmt.Errorf("%w: target type is invalid", ErrWalletAutoRechargeTargetInvalid)
	}
	return nil
}

// ValidateWalletAutoRechargeTarget re-reads both the policy and its owner/org
// state. Controllers call it immediately after a remote key issue; activation
// repeats the same validation inside its attachment transaction.
func ValidateWalletAutoRechargeTarget(policyID int) error {
	if policyID <= 0 {
		return ErrWalletAutoRechargeTargetInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Where("id = ?", policyID).First(&policy).Error; err != nil {
			return err
		}
		return validateWalletAutoRechargeChargeTarget(tx, &policy)
	})
}

// ValidateWalletAutoRechargeChargeAttempt is the last local authorization
// gate before a provider POST. It serializes against policy cancellation and
// rechecks the owner/organization, billing key, and durable TopUp attempt.
func ValidateWalletAutoRechargeChargeAttempt(policyID int, tradeNo string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if policyID <= 0 || tradeNo == "" {
		return ErrWalletAutoRechargeClaimLost
	}
	return validateWalletAutoRechargeChargeAttemptWithCredential(policyID, tradeNo, "", "")
}

func validateWalletAutoRechargeChargeAttemptWithCredential(policyID int, tradeNo, providerCredential, expectedSecret string) error {
	cancellationBlocked := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTWalletAutoRecharge)
		if err != nil {
			return err
		}
		// Membership transitions lock the owner first and then deactivate the
		// policy. Use the same order here so the provider-attempt marker and an
		// organization join cannot deadlock or pass each other unnoticed.
		var reference WalletAutoRecharge
		if err := tx.Select("owner_user_id", "target_type", "target_id").Where("id = ?", policyID).First(&reference).Error; err != nil {
			return err
		}
		if reference.OwnerUserId <= 0 {
			return ErrWalletAutoRechargeTargetInvalid
		}
		var owner User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "role", "organization_id", "organization_role").
			Where("id = ?", reference.OwnerUserId).First(&owner).Error; err != nil {
			return err
		}
		if err := lockWalletAutoRechargeOrganizationTx(tx, reference.TargetType, reference.TargetId); err != nil {
			return err
		}
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", policyID).First(&policy).Error; err != nil {
			return err
		}
		if policy.LastTradeNo == tradeNo {
			blocked, err := haltWalletAutoRechargeForRecordedCancellationTx(tx, &policy, true)
			if err != nil {
				return err
			}
			if blocked {
				cancellationBlocked = true
				return nil
			}
		}
		return validateWalletAutoRechargeChargeAttemptTx(tx, policyID, tradeNo, providerCredential, expectedSecret, postBarrier)
	})
	if err != nil {
		if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
			haltErr := DB.Transaction(func(tx *gorm.DB) error {
				lockedPolicy, lockErr := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
				if lockErr != nil {
					return lockErr
				}
				message := walletAutoRechargeManualReconciliationPrefix + err.Error()
				return haltWalletAutoRechargeForAssociationConflictTx(tx, lockedPolicy, tradeNo, message)
			})
			if haltErr != nil {
				return errors.Join(err, haltErr)
			}
		}
		return err
	}
	if cancellationBlocked {
		return ErrWalletAutoRechargeReconciliationRequired
	}
	return nil
}

func validateWalletAutoRechargeChargeAttemptTx(tx *gorm.DB, policyID int, tradeNo, providerCredential, expectedSecret string, postBarrier tossProviderPOSTBarrier) error {
	var policy WalletAutoRecharge
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", policyID).First(&policy).Error; err != nil {
		return err
	}
	if walletAutoRechargeMessageNeedsReplacementFence(policy.LastError) {
		return ErrWalletAutoRechargeReconciliationRequired
	}
	if policy.Status != WalletAutoRechargeStatusActive || policy.BillingKeyId <= 0 || policy.LastTradeNo != tradeNo {
		return ErrWalletAutoRechargeClaimLost
	}
	if err := validateWalletAutoRechargeChargeTarget(tx, &policy); err != nil {
		return err
	}
	var key UserBillingKey
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "user_id", "status").Where("id = ?", policy.BillingKeyId).First(&key).Error; err != nil {
		return err
	}
	if key.UserId != policy.OwnerUserId || key.Status != BillingKeyStatusActive {
		return ErrTossBillingKeyInactive
	}
	var topUp TopUp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return err
	}
	belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
	if associationErr != nil {
		return associationErr
	}
	if !belongs || topUp.Status != common.TopUpStatusPending || isWalletAutoRechargeLocallySettleable(&topUp) {
		return ErrWalletAutoRechargeClaimLost
	}
	if !topUp.ProviderAttempted && postBarrier.PolicyError != nil {
		return postBarrier.PolicyError
	}
	identity, associationPresent, identityErr := topUpWalletAutoRechargeAttemptIdentity(&topUp)
	if identityErr != nil || !associationPresent || isWalletAutoRechargeLegacyBridgeAlias(&topUp) {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	pendingWinner, identityErr := findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
	if identityErr != nil {
		return identityErr
	}
	if pendingWinner == nil || pendingWinner.Id != topUp.Id {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	compatibleIdentities, identityErr := walletAutoRechargeCompatibleAttemptIdentitiesForTopUp(&policy, &topUp, identity)
	if identityErr != nil {
		return identityErr
	}
	cycleIdentities := make([]walletAutoRechargeAttemptIdentity, 0, len(compatibleIdentities))
	for _, candidate := range compatibleIdentities {
		candidate.Attempt = 0
		cycleIdentities = append(cycleIdentities, candidate)
	}
	effectiveAttempt, identityErr := resolveWalletAutoRechargeEffectiveAttemptTx(
		tx,
		&policy,
		cycleIdentities,
	)
	if identityErr != nil {
		return identityErr
	}
	if effectiveAttempt != identity.Attempt {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if err := ensureNoOtherWalletAutoRechargePaymentFenceTx(tx, policy.TargetType, policy.TargetId, policy.Id, tradeNo); err != nil {
		return err
	}
	// This check is deliberately repeated after preparation and uses the DB
	// clock. A worker can pause between GET and POST long enough to cross the
	// recovery boundary, and an old task-supplied timestamp is not authority
	// for issuing a fresh card charge.
	if walletAutoRechargeAttemptOutsideGrace(&policy, topUp.CreateTime, getDBTimestampTx(tx)) {
		return ErrWalletAutoRechargeAttemptOutsideGrace
	}
	if providerCredential == "" {
		return nil
	}
	preserveAttemptCredential, err := preserveExactTossAttemptCredential(
		topUp.ProviderCredential,
		expectedSecret,
		topUp.ProviderAttempted,
	)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{
		"provider_attempted":  true,
		"provider_claim_time": getDBTimestampTx(tx),
	}
	if !preserveAttemptCredential {
		updates["provider_credential"] = providerCredential
	}
	updated := tx.Model(&TopUp{}).
		Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
		Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", tradeNo}).
		Updates(updates)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		// MySQL returns changed rows, so a same-second recovery retry can report
		// zero after writing the already-persisted exact intent. Re-read under the
		// existing policy/order locks and accept only that complete post-state.
		var persisted TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", topUp.Id).First(&persisted).Error; err != nil {
			return err
		}
		belongs, belongsErr := walletAutoRechargeTopUpBelongsToPolicy(&persisted, &policy)
		persistedIdentity, persistedAssociation, identityErr := topUpWalletAutoRechargeAttemptIdentity(&persisted)
		credentialMatches, credentialErr := preserveExactTossAttemptCredential(
			persisted.ProviderCredential,
			expectedSecret,
			true,
		)
		if belongsErr != nil || !belongs || identityErr != nil || !persistedAssociation ||
			persistedIdentity != identity || isWalletAutoRechargeLegacyBridgeAlias(&persisted) ||
			persisted.TradeNo != tradeNo || persisted.PaymentProvider != PaymentProviderToss ||
			persisted.PaymentMethod != PaymentMethodToss || persisted.Status != common.TopUpStatusPending ||
			(persisted.ProviderOrderId != "" && persisted.ProviderOrderId != tradeNo) ||
			!persisted.ProviderAttempted || persisted.ProviderClaimTime <= 0 ||
			credentialErr != nil || !credentialMatches {
			return ErrWalletAutoRechargeClaimLost
		}
	} else if updated.RowsAffected != 1 {
		return ErrWalletAutoRechargeClaimLost
	}
	return nil
}

func claimWalletAutoRechargeProviderPOSTCredential(policyID int, tradeNo, secretKey string) error {
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return errors.New("wallet auto recharge provider secret is unavailable")
	}
	providerCredential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return err
	}
	return validateWalletAutoRechargeChargeAttemptWithCredential(policyID, strings.TrimSpace(tradeNo), providerCredential, secretKey)
}

// promoteWalletAutoRechargeProviderCredentialAfterRejection durably advances
// the exact lookup/idempotency namespace after Toss definitively rejects the
// prior API key. Promotion itself cannot create a charge and therefore must
// survive an operational-disable or lifecycle failure before the next POST
// gate; otherwise recovery would start from the rejected key and treat the
// next key's safe 404 as an unsafe fallback ambiguity forever.
func promoteWalletAutoRechargeProviderCredentialAfterRejection(policyID int, tradeNo, rejectedSecret, nextSecret string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	rejectedSecret = strings.TrimSpace(rejectedSecret)
	nextSecret = strings.TrimSpace(nextSecret)
	if policyID <= 0 || tradeNo == "" || rejectedSecret == "" || nextSecret == "" || rejectedSecret == nextSecret {
		return ErrWalletAutoRechargeClaimLost
	}
	nextCredential, err := EncryptProviderCredential(nextSecret)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", policyID).First(&policy).Error; err != nil {
			return err
		}
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs || topUp.Status != common.TopUpStatusPending || isWalletAutoRechargeLocallySettleable(&topUp) {
			return ErrWalletAutoRechargeClaimLost
		}
		currentSecret, err := DecryptProviderCredential(topUp.ProviderCredential)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentSecret) == nextSecret {
			return nil
		}
		if strings.TrimSpace(currentSecret) != rejectedSecret {
			return ErrWalletAutoRechargeClaimLost
		}
		updated := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND provider_credential = ?", topUp.Id, common.TopUpStatusPending, topUp.ProviderCredential).
			Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", tradeNo}).
			Updates(map[string]interface{}{
				"provider_credential": nextCredential,
				"provider_attempted":  false,
				"provider_claim_time": 0,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrWalletAutoRechargeClaimLost
		}
		return nil
	})
}

func haltWalletAutoRechargeForRecordedCancellationTx(tx *gorm.DB, policy *WalletAutoRecharge, preservePendingRecovery bool) (bool, error) {
	if tx == nil || policy == nil {
		return false, nil
	}
	associatedOrders := tx.Model(&TopUp{}).
		Select("trade_no").
		Where("wallet_auto_recharge_id = ?", policy.Id)
	query := tx.Model(&TossPaymentEvent{}).
		Where("event_type = ? AND status IN ? AND reconciliation_status = ?", TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"}, TossReconciliationStatusRequired).
		Where("order_id LIKE ? ESCAPE '!' OR order_id IN (?)", walletAutoRechargeTradeNoLikePattern(policy.Id), associatedOrders)
	if strings.TrimSpace(policy.LastTradeNo) != "" {
		query = tx.Model(&TossPaymentEvent{}).
			Where("event_type = ? AND status IN ? AND reconciliation_status = ?", TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"}, TossReconciliationStatusRequired).
			Where("order_id LIKE ? ESCAPE '!' OR order_id IN (?) OR order_id = ?", walletAutoRechargeTradeNoLikePattern(policy.Id), associatedOrders, policy.LastTradeNo)
	}
	var events []TossPaymentEvent
	if err := query.Order("id desc").Find(&events).Error; err != nil {
		return false, err
	}
	if len(events) == 0 {
		return false, nil
	}
	missingPaymentRow := false
	for i := range events {
		var row TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("trade_no = ?", events[i].OrderId).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				missingPaymentRow = true
				continue
			}
			return false, err
		}
	}
	if !walletAutoRechargeHasPermanentAssociationFence(policy.LastError) {
		for i := range events {
			event := &events[i]
			if event.Status != "CANCELED" || event.OriginalAmount <= 0 || event.BalanceAmount != 0 ||
				strings.TrimSpace(event.PaymentKey) == "" {
				continue
			}
			samePaymentOnly := true
			for j := range events {
				if events[j].OrderId != event.OrderId || events[j].PaymentKey != event.PaymentKey {
					samePaymentOnly = false
					break
				}
			}
			if !samePaymentOnly {
				continue
			}
			var topUp TopUp
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", event.OrderId).First(&topUp).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					missingPaymentRow = true
					continue
				}
				return false, err
			}
			belongs, belongsErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, policy)
			if belongsErr != nil || !belongs || topUp.Amount != event.OriginalAmount || topUp.Status == common.TopUpStatusSuccess {
				continue
			}
			if hasWalletAutoRechargeRealProviderPaymentKey(&topUp) && topUp.ProviderOrderId != event.PaymentKey {
				continue
			}
			if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed && topUp.Status != common.TopUpStatusExpired {
				continue
			}
			now := getDBTimestampTx(tx)
			terminalMarker := topUp.TradeNo + ":terminal"
			if err := tx.Model(&TopUp{}).Where("id = ? AND status IN ?", topUp.Id, []string{
				common.TopUpStatusPending, common.TopUpStatusFailed, common.TopUpStatusExpired,
			}).Updates(map[string]interface{}{
				"provider_order_id":    terminalMarker,
				"provider_order_time":  now,
				"provider_payload":     event.ProviderPayload,
				"provider_retry_time":  0,
				"provider_claim_token": "",
				"provider_claim_time":  0,
				"complete_time":        now,
				"status":               common.TopUpStatusFailed,
			}).Error; err != nil {
				return false, err
			}
			eventIDs := make([]int, 0, len(events))
			for j := range events {
				eventIDs = append(eventIDs, events[j].Id)
			}
			var remainingEvents int64
			if err := walletAutoRechargePaymentEventScopeTx(tx, policy).
				Where("reconciliation_status = ? AND id NOT IN ?", TossReconciliationStatusRequired, eventIDs).
				Count(&remainingEvents).Error; err != nil {
				return false, err
			}
			unresolvedTopUp, err := walletAutoRechargeHasUnresolvedAssociatedTopUpTx(tx, policy)
			if err != nil {
				return false, err
			}
			if remainingEvents > 0 || unresolvedTopUp {
				break
			}
			if err := tx.Model(&TossPaymentEvent{}).
				Where("id IN ? AND reconciliation_status = ?", eventIDs, TossReconciliationStatusRequired).
				Updates(map[string]interface{}{
					"reconciliation_status": TossReconciliationStatusResolved,
					"resolution_note":       "automatically reconciled after authoritative full cancellation",
					"resolved_time":         now,
					"resolved_by":           0,
				}).Error; err != nil {
				return false, err
			}
			if err := haltWalletAutoRechargeForReconciliationTx(
				tx, policy.Id, topUp.TradeNo, "wallet auto recharge provider payment canceled",
			); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	message := walletAutoRechargeManualReconciliationPrefix + "authoritative cancellation event blocks automatic charge"
	if missingPaymentRow {
		message = walletAutoRechargeManualReconciliationPrefix + "cancellation event has no durable payment row"
	} else if preservePendingRecovery {
		message = walletAutoRechargeReconciliationPendingPrefix + "automatic charge blocked; existing attempt requires GET-only recovery"
	}
	referenceTradeNo := strings.TrimSpace(policy.LastTradeNo)
	if referenceTradeNo == "" {
		referenceTradeNo = strings.TrimSpace(policy.AuthTradeNo)
	}
	if err := haltWalletAutoRechargeForReconciliationTx(tx, policy.Id, referenceTradeNo, message); err != nil {
		return false, err
	}
	return true, nil
}

func persistWalletAutoRechargePreparationFailure(tx *gorm.DB, charge *walletAutoRechargeCharge, policy *WalletAutoRecharge, maxFails int, cause error) error {
	if err := markWalletAutoRechargeFailure(tx, policy.Id, policy.BillingKeyId, maxFails, cause, ""); err != nil {
		return err
	}
	charge.preparationErr = cause
	return nil
}

func prepareWalletAutoRechargeCharge(policyId int, now time.Time, maxFails int) (walletAutoRechargeCharge, error) {
	now = walletAutoRechargeProcessLocalTime(now)
	var charge walletAutoRechargeCharge
	err := DB.Transaction(func(tx *gorm.DB) error {
		var reference WalletAutoRecharge
		if err := tx.Select("owner_user_id", "target_type", "target_id").Where("id = ?", policyId).First(&reference).Error; err != nil {
			return err
		}
		if err := lockWalletAutoRechargeOwnerTx(tx, reference.OwnerUserId); err != nil {
			return err
		}
		if err := lockWalletAutoRechargeOrganizationTx(tx, reference.TargetType, reference.TargetId); err != nil {
			return err
		}
		var policy WalletAutoRecharge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", policyId).First(&policy).Error; err != nil {
			return err
		}
		pendingTopUp, err := findWalletAutoRechargePendingSettlementTopUp(tx, &policy)
		if err != nil {
			if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
				message := walletAutoRechargeManualReconciliationPrefix + err.Error()
				if haltErr := haltWalletAutoRechargeForAssociationConflictTx(tx, &policy, policy.LastTradeNo, message); haltErr != nil {
					return errors.Join(err, haltErr)
				}
				charge.policy = policy
				charge.preparationErr = err
				return nil
			}
			return err
		}
		currentMarkerUnresolved := strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix) ||
			walletAutoRechargeMessageNeedsReplacementFence(policy.LastError)
		postCreditEventOnly := policy.LastChargeTime > 0 &&
			strings.HasPrefix(policy.LastError, walletAutoRechargeEventResolutionPendingPrefix)
		if pendingTopUp == nil && currentMarkerUnresolved && !postCreditEventOnly {
			if policy.Status != WalletAutoRechargeStatusActive {
				charge.policy = policy
				return nil
			}
			conflict := ErrWalletAutoRechargeAttemptAssociationConflict
			message := walletAutoRechargeManualReconciliationPrefix + "financial marker has no unique recoverable payment row"
			if haltErr := haltWalletAutoRechargeForAssociationConflictTx(tx, &policy, policy.LastTradeNo, message); haltErr != nil {
				return errors.Join(conflict, haltErr)
			}
			charge.policy = policy
			charge.preparationErr = conflict
			return nil
		}
		preservePendingRecovery := pendingTopUp != nil && pendingTopUp.Status == common.TopUpStatusPending
		cancellationBlocked, err := haltWalletAutoRechargeForRecordedCancellationTx(tx, &policy, preservePendingRecovery)
		if err != nil {
			return err
		}
		if cancellationBlocked && !preservePendingRecovery {
			charge.policy = policy
			return nil
		}
		if cancellationBlocked {
			// The halt helper changed policy/key lifecycle state. Reload it so the
			// recovery branch does not try an active-policy marker CAS, while still
			// retaining the exact pending order for authoritative GET-only recovery.
			if err := tx.Where("id = ?", policy.Id).First(&policy).Error; err != nil {
				return err
			}
			if pendingTopUp != nil {
				var refreshed TopUp
				if err := tx.Where("id = ?", pendingTopUp.Id).First(&refreshed).Error; err != nil {
					return err
				}
				if refreshed.Status != common.TopUpStatusPending && refreshed.Status != common.TopUpStatusSuccess {
					charge.policy = policy
					return nil
				}
				pendingTopUp = &refreshed
			}
		}
		currentTradeNo := policy.LastTradeNo
		if pendingTopUp != nil {
			currentTradeNo = pendingTopUp.TradeNo
		}
		targetPaymentFenceBlocked := false
		if err := ensureNoOtherWalletAutoRechargePaymentFenceTx(
			tx, policy.TargetType, policy.TargetId, policy.Id, currentTradeNo,
		); err != nil {
			if pendingTopUp == nil || !errors.Is(err, ErrWalletAutoRechargeReconciliationRequired) {
				charge.policy = policy
				charge.preparationErr = err
				return nil
			}
			// A target-wide fence forbids a new POST but must not hide the exact
			// already-durable order. GET-only recovery can still discover DONE and
			// settle that one payment without creating another charge.
			targetPaymentFenceBlocked = true
		}
		if pendingTopUp != nil {
			localCancellationPending := policy.Status == WalletAutoRechargeStatusCancelPending
			charge = walletAutoRechargeCharge{
				policy:                 policy,
				tradeNo:                pendingTopUp.TradeNo,
				chargeKRW:              pendingTopUp.Amount,
				stalePending:           walletAutoRechargeAttemptOutsideGrace(&policy, pendingTopUp.CreateTime, getDBTimestampTx(tx)),
				postForbidden:          cancellationBlocked || localCancellationPending || targetPaymentFenceBlocked,
				cancellationNeedsGrace: localCancellationPending && !cancellationBlocked,
				shouldCharge:           true,
			}
			if policy.LastTradeNo == pendingTopUp.TradeNo && strings.HasPrefix(policy.LastError, walletAutoRechargeDoneMismatchPrefix) {
				// Toss reports DONE, but strict order/amount/currency/card validation
				// failed. Freeze this exact order for manual reconciliation; never
				// create another POST/orderId from the policy.
				charge.shouldCharge = false
				return nil
			}
			if pendingTopUp.Status == common.TopUpStatusSuccess || isWalletAutoRechargeLocallySettleable(pendingTopUp) {
				if pendingTopUp.Status == common.TopUpStatusPending {
					if err := tx.Model(&WalletAutoRecharge{}).
						Where("id = ?", policy.Id).
						Updates(map[string]interface{}{
							"last_trade_no": pendingTopUp.TradeNo,
							"last_error":    walletAutoRechargeReconciliationPendingPrefix + "provider charge recorded",
							"update_time":   getDBTimestampTx(tx),
						}).Error; err != nil {
						return err
					}
				}
				charge.alreadyDone = true
				return nil
			}
			if policy.Status == WalletAutoRechargeStatusActive &&
				(policy.LastTradeNo != pendingTopUp.TradeNo || !strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix)) {
				marker := walletAutoRechargeProviderPendingPrefix + "recovered durable charge attempt"
				marked := tx.Model(&WalletAutoRecharge{}).
					Where("id = ? AND status = ? AND billing_key_id = ?", policy.Id, WalletAutoRechargeStatusActive, policy.BillingKeyId).
					Updates(map[string]interface{}{
						"last_trade_no": pendingTopUp.TradeNo,
						"last_error":    marker,
						"update_time":   getDBTimestampTx(tx),
					})
				if marked.Error != nil {
					return marked.Error
				}
				if marked.RowsAffected != 1 {
					return ErrWalletAutoRechargeClaimLost
				}
				policy.LastTradeNo = pendingTopUp.TradeNo
				policy.LastError = marker
				charge.policy = policy
			}
			if strings.TrimSpace(pendingTopUp.ProviderCredential) == "" {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			secretKey, err := DecryptProviderCredential(pendingTopUp.ProviderCredential)
			if err != nil || strings.TrimSpace(secretKey) == "" {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			secretKeys := make([]string, 0, 3)
			secretKeys = appendUniqueTossSecret(secretKeys, secretKey)
			billingKey := ""
			customerKey := strings.TrimSpace(policy.CustomerKey)
			if policy.BillingKeyId > 0 {
				storedBillingKey, storedCustomerKey, candidates, candidateErr := getTossBillingKeyPlainWithSecretCandidatesTx(tx, policy.BillingKeyId, false, false)
				if candidateErr == nil {
					billingKey = storedBillingKey
					customerKey = storedCustomerKey
					for _, candidate := range candidates {
						secretKeys = appendUniqueTossSecret(secretKeys, candidate)
					}
				}
			}
			charge.billingKey = billingKey
			charge.customerKey = customerKey
			charge.secretKeys = secretKeys
			charge.lookupPending = true
			return nil
		}
		if policy.Status != WalletAutoRechargeStatusActive {
			return nil
		}
		// The service captures a database timestamp before dispatching a bounded
		// batch. A long pending/scheduled batch can cross an hour before this
		// threshold policy reaches preparation. Do not create an identity from a
		// stale UTC hour: every new master must use the same canonical cycle, while
		// an older local-time writer may still create its current-hour alias during
		// a rolling deployment. Leave the policy untouched for the next fresh tick.
		attemptCreateTime := getDBTimestampTx(tx)
		if policy.Type == WalletAutoRechargeTypeThreshold {
			attemptNow := time.Unix(attemptCreateTime, 0).UTC()
			if walletAutoRechargeProcessUTCTime(now).Format("2006010215") != attemptNow.Format("2006010215") {
				return nil
			}
		}
		if policy.Type == WalletAutoRechargeTypeScheduled && policy.NextChargeTime > now.Unix() {
			return nil
		}
		if policy.Type == WalletAutoRechargeTypeScheduled {
			if err := validateWalletAutoRechargeIntervalForCurrentMode(policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds); err != nil {
				return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
			}
		}
		if err := validateWalletAutoRechargeChargeTarget(tx, &policy); err != nil {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		if policy.Type == WalletAutoRechargeTypeThreshold {
			ok, err := walletThresholdShouldCharge(tx, &policy, now)
			if err != nil {
				return err
			}
			if !ok {
				// Threshold policies are processed in bounded least-recently-checked
				// batches. Move a non-due policy to the back of that queue so more
				// than one batch of active policies cannot starve forever.
				return tx.Model(&WalletAutoRecharge{}).
					Where("id = ? AND status = ?", policy.Id, WalletAutoRechargeStatusActive).
					Update("update_time", now.Unix()).Error
			}
		}
		if setting.GetTossConfigSnapshot().UnitPrice <= 0 {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, errors.New("invalid Toss unit price"))
		}

		chargeKRW := walletAutoRechargeKRW(policy.Amount)
		if chargeKRW < walletAutoRechargeMinimumKRW() || chargeKRW > setting.TossMaximumChargeAmountKRW {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, errors.New("wallet auto recharge charge amount is outside Toss limits"))
		}
		if walletAutoRechargeQuota(policy.Amount) <= 0 || walletAutoRechargeMoney(policy.Amount) <= 0 {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, errors.New("wallet auto recharge quota conversion is invalid"))
		}

		var billingKeyOwner UserBillingKey
		if err := tx.Select("id", "user_id", "customer_key", "provider_credential").Where("id = ?", policy.BillingKeyId).First(&billingKeyOwner).Error; err != nil {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		if billingKeyOwner.UserId != policy.OwnerUserId {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, errors.New("wallet auto recharge billing key owner mismatch"))
		}

		billingKey, customerKey, secretKeys, err := getTossBillingKeyPlainWithSecretCandidatesTx(tx, policy.BillingKeyId, true, true)
		if err != nil {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		secretKeys, err = prioritizeTossStoredCredentialSecret(secretKeys, billingKeyOwner.ProviderCredential)
		if err != nil {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		if strings.TrimSpace(policy.CustomerKey) != "" && policy.CustomerKey != customerKey {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, errors.New("wallet auto recharge customer key mismatch"))
		}

		cycleIdentities, err := walletAutoRechargeAttemptIdentitiesForPolicyAttempt(&policy, now, 0)
		if err != nil || len(cycleIdentities) == 0 {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		effectiveAttempt, err := resolveWalletAutoRechargeEffectiveAttemptTx(tx, &policy, cycleIdentities)
		if err != nil {
			if errors.Is(err, ErrTossRecurringOrderIDEvidenceCorrupt) {
				return err
			}
			if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
				message := walletAutoRechargeManualReconciliationPrefix + err.Error()
				if haltErr := haltWalletAutoRechargeForAssociationConflictTx(tx, &policy, policy.LastTradeNo, message); haltErr != nil {
					return errors.Join(err, haltErr)
				}
				charge.policy = policy
				charge.preparationErr = err
				return nil
			}
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		identities, err := walletAutoRechargeAttemptIdentitiesForPolicyAttempt(&policy, now, effectiveAttempt)
		if err != nil || len(identities) == 0 {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, err)
		}
		identity := identities[0]
		tradeNo := walletAutoRechargeTradeNoForAttempt(policy, now, effectiveAttempt)
		expectedLegacyTradeNo, ok := legacyWalletAutoRechargeTradeNo(identity)
		if !ok || tradeNo != expectedLegacyTradeNo {
			return persistWalletAutoRechargePreparationFailure(tx, &charge, &policy, maxFails, ErrWalletAutoRechargeAttemptAssociationConflict)
		}
		secretKey := ""
		if len(secretKeys) > 0 {
			secretKey = secretKeys[0]
		}
		winner, preexisting, alreadyDone, err := ensureWalletAutoRechargePendingTopUp(
			tx, &policy, identity, identities, tradeNo, chargeKRW, attemptCreateTime, secretKey,
		)
		if err != nil {
			if errors.Is(err, ErrTossRecurringOrderIDEvidenceCorrupt) {
				return err
			}
			if errors.Is(err, ErrWalletAutoRechargeAttemptAssociationConflict) {
				message := walletAutoRechargeManualReconciliationPrefix + err.Error()
				if haltErr := haltWalletAutoRechargeForAssociationConflictTx(tx, &policy, tradeNo, message); haltErr != nil {
					return errors.Join(err, haltErr)
				}
				charge.policy = policy
				charge.preparationErr = err
				return nil
			}
			return err
		}
		tradeNo = winner.TradeNo
		chargeKRW = winner.Amount
		lookupPending := preexisting && !alreadyDone
		if lookupPending {
			exactSecret, decryptErr := DecryptProviderCredential(winner.ProviderCredential)
			if decryptErr != nil || strings.TrimSpace(exactSecret) == "" {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			pinnedSecrets := make([]string, 0, len(secretKeys)+1)
			pinnedSecrets = appendUniqueTossSecret(pinnedSecrets, exactSecret)
			for _, candidate := range secretKeys {
				pinnedSecrets = appendUniqueTossSecret(pinnedSecrets, candidate)
			}
			secretKeys = pinnedSecrets
		}
		if !alreadyDone {
			marker := walletAutoRechargeProviderPendingPrefix + "charge attempt recorded"
			marked := tx.Model(&WalletAutoRecharge{}).
				Where("id = ? AND status = ? AND billing_key_id = ?", policy.Id, WalletAutoRechargeStatusActive, policy.BillingKeyId).
				Updates(map[string]interface{}{
					"last_trade_no": tradeNo,
					"last_error":    marker,
					"update_time":   getDBTimestampTx(tx),
				})
			if marked.Error != nil {
				return marked.Error
			}
			if marked.RowsAffected != 1 {
				return ErrWalletAutoRechargeClaimLost
			}
			policy.LastTradeNo = tradeNo
			policy.LastError = marker
		}

		charge = walletAutoRechargeCharge{
			policy:        policy,
			billingKey:    billingKey,
			customerKey:   customerKey,
			secretKeys:    secretKeys,
			tradeNo:       tradeNo,
			chargeKRW:     chargeKRW,
			alreadyDone:   alreadyDone,
			lookupPending: lookupPending,
			stalePending:  lookupPending && walletAutoRechargeAttemptOutsideGrace(&policy, winner.CreateTime, getDBTimestampTx(tx)),
			shouldCharge:  true,
		}
		return nil
	})
	return charge, err
}

func findWalletAutoRechargePendingSettlementTopUp(tx *gorm.DB, policy *WalletAutoRecharge) (*TopUp, error) {
	if tx == nil || policy == nil || policy.Id <= 0 {
		return nil, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if err := ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(tx); err != nil {
		return nil, err
	}
	candidates := make(map[int]TopUp)
	addCandidate := func(topUp *TopUp) error {
		if topUp == nil {
			return nil
		}
		belongs, err := walletAutoRechargeTopUpBelongsToPolicy(topUp, policy)
		if err != nil || !belongs {
			return ErrWalletAutoRechargeAttemptAssociationConflict
		}
		if topUp.Id <= 0 || topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss ||
			(topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusSuccess) {
			return ErrWalletAutoRechargeAttemptAssociationConflict
		}
		candidates[topUp.Id] = *topUp
		if len(candidates) > 1 {
			// Two physical pending/settleable rows for one policy mean that mixed
			// binaries may already have authorized distinct provider orderIds. Never
			// choose one arbitrarily and allow another POST; halt for reconciliation.
			return ErrWalletAutoRechargeAttemptAssociationConflict
		}
		return nil
	}

	if policy.LastTradeNo != "" {
		topUp, err := findWalletAutoRechargeSettlementTopUpByLastTradeNo(tx, policy)
		if err != nil {
			return nil, err
		}
		if err := addCandidate(topUp); err != nil {
			return nil, err
		}
	}

	var rows []TopUp
	err := tx.Where(
		"target_type = ? AND target_id = ? AND payment_provider = ? AND payment_method = ? AND status = ?",
		policy.TargetType,
		policy.TargetId,
		PaymentProviderToss,
		PaymentMethodToss,
		common.TopUpStatusPending,
	).Where("wallet_auto_recharge_id = ? OR trade_no LIKE ? ESCAPE '!'", policy.Id, walletAutoRechargeTradeNoLikePattern(policy.Id)).
		Order("id desc").Limit(32).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	exactPrefix := fmt.Sprintf("wallet_auto_%d_", policy.Id)
	for i := range rows {
		associationPresent := rows[i].WalletAutoRechargeId != nil ||
			rows[i].WalletAutoRechargeCycleKey != nil || rows[i].WalletAutoRechargeAttempt != nil
		if associationPresent {
			identity, _, identityErr := topUpWalletAutoRechargeAttemptIdentity(&rows[i])
			if identityErr != nil {
				return nil, identityErr
			}
			if identity.PolicyID != policy.Id {
				return nil, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			winner, identityErr := findWalletAutoRechargeTopUpForIdentityTx(tx, identity)
			if identityErr != nil {
				return nil, identityErr
			}
			if winner == nil {
				return nil, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			if err := addCandidate(winner); err != nil {
				return nil, err
			}
			continue
		}
		if identity, ok := parseLegacyWalletAutoRechargeTradeNoForPolicyType(rows[i].TradeNo, policy.Type); ok {
			if identity.PolicyID != policy.Id {
				return nil, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			winner, identityErr := findWalletAutoRechargeTopUpForIdentityTx(tx, identity)
			if identityErr != nil {
				return nil, identityErr
			}
			if winner == nil {
				return nil, ErrWalletAutoRechargeAttemptAssociationConflict
			}
			if err := addCandidate(winner); err != nil {
				return nil, err
			}
			continue
		}
		// A pending row is itself the durable provider-attempt marker. Older
		// versions did not always copy it to policy.last_trade_no before the POST,
		// so recover the newest matching order with GET-before-POST semantics.
		if strings.HasPrefix(rows[i].TradeNo, exactPrefix) {
			if err := addCandidate(&rows[i]); err != nil {
				return nil, err
			}
		}
	}
	for _, candidate := range candidates {
		winner := candidate
		return &winner, nil
	}
	return nil, nil
}

func findWalletAutoRechargeSettlementTopUpByLastTradeNo(tx *gorm.DB, policy *WalletAutoRecharge) (*TopUp, error) {
	var topUp TopUp
	err := tx.Where("trade_no = ?", policy.LastTradeNo).First(&topUp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if isWalletAutoRechargeLocallySettleable(&topUp) {
		belongs, belongsErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, policy)
		if belongsErr != nil {
			return nil, belongsErr
		}
		if !belongs {
			return nil, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		return &topUp, nil
	}
	if isMarkedWalletAutoRechargeSettlementTopUp(policy, &topUp) {
		return &topUp, nil
	}
	return nil, nil
}

func isWalletAutoRechargeProviderChargeRecorded(topUp *TopUp) bool {
	return topUp != nil && topUp.Status == common.TopUpStatusPending && hasWalletAutoRechargeRealProviderPaymentKey(topUp)
}

func hasWalletAutoRechargeRealProviderPaymentKey(topUp *TopUp) bool {
	if topUp == nil {
		return false
	}
	providerOrderId := strings.TrimSpace(topUp.ProviderOrderId)
	return topUp.PaymentProvider == PaymentProviderToss &&
		topUp.PaymentMethod == PaymentMethodToss &&
		providerOrderId != "" &&
		providerOrderId != topUp.TradeNo &&
		providerOrderId != topUp.TradeNo+":charged" &&
		providerOrderId != topUp.TradeNo+":done-mismatch" &&
		providerOrderId != topUp.TradeNo+":terminal"
}

func isLegacyChargedWalletAutoRechargeTopUp(topUp *TopUp) bool {
	return topUp != nil &&
		topUp.PaymentProvider == PaymentProviderToss &&
		topUp.PaymentMethod == PaymentMethodToss &&
		topUp.Status == common.TopUpStatusPending &&
		topUp.ProviderOrderId == topUp.TradeNo+":charged"
}

func isWalletAutoRechargeLocallySettleable(topUp *TopUp) bool {
	return isWalletAutoRechargeProviderChargeRecorded(topUp) || isLegacyChargedWalletAutoRechargeTopUp(topUp)
}

func isMarkedWalletAutoRechargeSettlementTopUp(policy *WalletAutoRecharge, topUp *TopUp) bool {
	if policy == nil || topUp == nil ||
		(!strings.HasPrefix(policy.LastError, walletAutoRechargeProviderPendingPrefix) &&
			!strings.HasPrefix(policy.LastError, walletAutoRechargeReconciliationPendingPrefix)) {
		return false
	}
	belongs, err := walletAutoRechargeTopUpBelongsToPolicy(topUp, policy)
	return err == nil && belongs &&
		topUp.PaymentProvider == PaymentProviderToss &&
		topUp.PaymentMethod == PaymentMethodToss &&
		(topUp.Status == common.TopUpStatusPending || topUp.Status == common.TopUpStatusSuccess) &&
		topUp.TargetType == policy.TargetType &&
		topUp.TargetId == policy.TargetId &&
		topUp.TradeNo == policy.LastTradeNo
}

func ensureWalletAutoRechargePendingTopUp(
	tx *gorm.DB,
	policy *WalletAutoRecharge,
	primaryIdentity walletAutoRechargeAttemptIdentity,
	compatibleIdentities []walletAutoRechargeAttemptIdentity,
	tradeNo string,
	chargeKRW int64,
	createTime int64,
	secretKey string,
) (winner *TopUp, preexisting bool, alreadyDone bool, err error) {
	identities, identityErr := normalizedWalletAutoRechargeAttemptIdentities(append(
		[]walletAutoRechargeAttemptIdentity{primaryIdentity}, compatibleIdentities...,
	))
	if identityErr != nil || tx == nil || policy == nil || primaryIdentity.PolicyID != policy.Id ||
		!primaryIdentity.valid() || createTime <= 0 {
		return nil, false, false, ErrWalletAutoRechargeAttemptAssociationConflict
	}
	for _, identity := range identities {
		if identity.PolicyID != policy.Id {
			return nil, false, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
	}
	if err := ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(tx); err != nil {
		return nil, false, false, err
	}
	validateWinner := func(existing *TopUp) (bool, error) {
		if existing == nil || existing.PaymentProvider != PaymentProviderToss || existing.PaymentMethod != PaymentMethodToss ||
			existing.Amount != chargeKRW || existing.TargetType != policy.TargetType || existing.TargetId != policy.TargetId {
			return false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		resolved, _, identityErr := topUpWalletAutoRechargeAttemptIdentity(existing)
		if identityErr != nil || !walletAutoRechargeIdentityInSet(resolved, identities) {
			return false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		if existing.Status == common.TopUpStatusSuccess {
			return true, nil
		}
		if existing.Status != common.TopUpStatusPending {
			return false, errors.New("wallet auto recharge top-up status invalid")
		}
		if strings.TrimSpace(existing.ProviderCredential) == "" {
			// An existing pending row may have been posted by a pre-marker
			// binary. Backfilling today's secret would move recovery into a
			// different provider namespace and could create a duplicate charge.
			return false, ErrTossBillingCrossCredentialRetryUnsafe
		}
		if existing.ProviderClientKeyHash == "" && IsValidTossClientKeyFingerprint(policy.ProviderClientKeyHash) {
			if err := tx.Model(existing).Update("provider_client_key_hash", policy.ProviderClientKeyHash).Error; err != nil {
				return false, err
			}
			existing.ProviderClientKeyHash = policy.ProviderClientKeyHash
		}
		return isWalletAutoRechargeLocallySettleable(existing), nil
	}

	existing, err := findWalletAutoRechargeTopUpForIdentitiesTx(tx, identities)
	if err != nil {
		return nil, false, false, err
	}
	if existing != nil {
		alreadyDone, err = validateWinner(existing)
		return existing, true, alreadyDone, err
	}
	providerCredential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return nil, false, false, err
	}
	if _, err := tossRecurringOrderIDWriterVersionTx(tx); err != nil {
		return nil, false, false, err
	}
	providerClientKeyHash := ""
	if IsValidTossClientKeyFingerprint(policy.ProviderClientKeyHash) {
		providerClientKeyHash = policy.ProviderClientKeyHash
	}

	topUp := &TopUp{
		UserId:                policy.OwnerUserId,
		TargetType:            policy.TargetType,
		TargetId:              policy.TargetId,
		Amount:                chargeKRW,
		Money:                 TossUSDEquivalent(chargeKRW),
		Quota:                 TossCreditQuotaFromKRW(chargeKRW),
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: providerClientKeyHash,
		PaymentMethod:         PaymentMethodToss,
		PaymentProvider:       PaymentProviderToss,
		// The attempt age gate is based on the database clock. Preparation passes
		// the exact timestamp used by the threshold hour guard so an hour rollover
		// between validation and INSERT cannot turn this row into a bridge alias.
		CreateTime:                 createTime,
		Status:                     common.TopUpStatusPending,
		WalletAutoRechargeId:       &primaryIdentity.PolicyID,
		WalletAutoRechargeCycleKey: &primaryIdentity.CycleKey,
		WalletAutoRechargeAttempt:  &primaryIdentity.Attempt,
		WalletOrderIdVersion:       tossWalletOrderIDVersionOpaque,
	}
	for candidateAttempt := 0; candidateAttempt < tossOpaqueOrderIDCollisionMaxRetries; candidateAttempt++ {
		topUp.Id = 0
		topUp.WalletAutoRechargeCreationToken = common.GetUUID()
		topUp.TradeNo, err = newTossRecurringOpaqueOrderID(tossWalletOpaqueOrderIDPrefix)
		if err != nil {
			return nil, false, false, err
		}
		createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(topUp)
		if createResult.Error != nil {
			return nil, false, false, createResult.Error
		}
		candidateID := topUp.Id
		candidateCreationToken := topUp.WalletAutoRechargeCreationToken
		winner, err = findWalletAutoRechargeTopUpForIdentitiesTx(tx, identities)
		if err != nil {
			return nil, false, false, err
		}
		if winner == nil {
			// The only other unique key is trade_no. Keep the singleton lock
			// and retry a random v2 collision without changing the logical tuple.
			if candidateAttempt+1 < tossOpaqueOrderIDCollisionMaxRetries {
				continue
			}
			return nil, false, false, ErrWalletAutoRechargeAttemptAssociationConflict
		}
		// MySQL CLIENT_FOUND_ROWS may report one for the no-op duplicate-key
		// update used by DoNothing. Only an assigned candidate ID that equals the
		// resolved physical winner proves this transaction inserted the row.
		preexisting = candidateID <= 0 || winner.Id != candidateID || winner.WalletAutoRechargeCreationToken != candidateCreationToken
		alreadyDone, err = validateWinner(winner)
		return winner, preexisting, alreadyDone, err
	}
	return nil, false, false, ErrWalletAutoRechargeAttemptAssociationConflict
}

// closeStaleWalletAutoRechargeAttempt terminalizes an attempt only after an
// authoritative order lookup returned not found outside the operational grace
// period. The TopUp, policy, and billing-key lifecycle transition together so
// no worker can create a replacement order while this one is being closed.
//
// The bool reports that a concurrent provider settlement won the CAS. Callers
// must then run the normal idempotent local-settlement path instead of failing
// the policy.
func closeStaleWalletAutoRechargeAttempt(policyID int, tradeNo string) (bool, error) {
	return closeWalletAutoRechargeAttemptAfterNotFound(
		policyID,
		tradeNo,
		true,
		"wallet auto recharge attempt expired after provider lookup returned not found",
	)
}

func closeCancellationBlockedWalletAutoRechargeAttempt(policyID int, tradeNo string) (bool, error) {
	return closeWalletAutoRechargeAttemptAfterNotFound(
		policyID,
		tradeNo,
		false,
		walletAutoRechargeManualReconciliationPrefix+"cancellation blocked attempt was not found at provider",
	)
}

func closeWalletAutoRechargeAttemptAfterNotFound(policyID int, tradeNo string, requireOutsideGrace bool, terminalMessage string) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if policyID <= 0 || tradeNo == "" {
		return false, ErrWalletAutoRechargeClaimLost
	}
	providerSettlementWon := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return ErrWalletAutoRechargeClaimLost
		}
		if topUp.Status == common.TopUpStatusSuccess || isWalletAutoRechargeLocallySettleable(&topUp) {
			providerSettlementWon = true
			return nil
		}
		if topUp.Status == common.TopUpStatusFailed || topUp.Status == common.TopUpStatusExpired {
			// An idempotent peer already completed the same fail-closed path.
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrWalletAutoRechargeClaimLost
		}
		now := getDBTimestampTx(tx)
		if requireOutsideGrace && !walletAutoRechargeAttemptOutsideGrace(&policy, topUp.CreateTime, now) {
			return ErrWalletAutoRechargeClaimLost
		}
		if topUp.ProviderAttempted && topUp.ProviderClaimTime > now-walletAutoRechargeChargeClaimTTLSeconds {
			// A provider call authorized by a fresh final gate may still be in
			// flight. A concurrent GET-404 observation cannot terminalize its row;
			// the same idempotent order must remain recoverable until the lease ages.
			return ErrWalletAutoRechargeClaimLost
		}

		claim := tx.Model(&TopUp{}).
			Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
			Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", tradeNo}).
			Update("status", common.TopUpStatusExpired)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess || isWalletAutoRechargeLocallySettleable(&current) {
				providerSettlementWon = true
				return nil
			}
			if current.Status == common.TopUpStatusFailed || current.Status == common.TopUpStatusExpired {
				return nil
			}
			return ErrWalletAutoRechargeClaimLost
		}

		terminalStatus := WalletAutoRechargeStatusFailed
		if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusCancelPending {
			terminalStatus = WalletAutoRechargeStatusCancelled
		}
		preserveLateDONEFence := topUp.ProviderAttempted || topUp.WalletOrderIdVersion != tossWalletOrderIDVersionOpaque ||
			topUp.ProviderClaimTime > 0 || strings.TrimSpace(topUp.ProviderClaimToken) != "" ||
			topUp.ProviderOrderTime > 0 || strings.TrimSpace(topUp.ProviderPayload) != ""
		var activeKey interface{}
		if preserveLateDONEFence {
			availableKey, keyErr := walletAutoRechargeAvailableFenceKeyTx(tx, &policy)
			if keyErr != nil {
				return keyErr
			}
			activeKey = availableKey
			terminalStatus = WalletAutoRechargeStatusFailed
			if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusCancelPending {
				terminalStatus = WalletAutoRechargeStatusCancelPending
			}
			terminalMessage = truncateRunes(
				walletAutoRechargeLateDONEFencePrefix+"provider-capable order returned not found; late DONE remains possible",
				255,
			)
		}
		if err := tx.Model(&WalletAutoRecharge{}).
			Where("id = ?", policy.Id).
			Updates(map[string]interface{}{
				"active_key":            activeKey,
				"status":                terminalStatus,
				"last_trade_no":         tradeNo,
				"last_error":            terminalMessage,
				"settlement_retry_time": 0,
				"update_time":           now,
			}).Error; err != nil {
			return err
		}
		if policy.BillingKeyId > 0 {
			_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
			return err
		}
		return nil
	})
	return providerSettlementWon, err
}

func haltWalletAutoRechargeForReconciliationTx(tx *gorm.DB, policyID int, tradeNo, message string) error {
	var policy WalletAutoRecharge
	if err := tx.Select("id", "type", "target_type", "target_id", "active_key", "status", "billing_key_id", "last_error").Where("id = ?", policyID).First(&policy).Error; err != nil {
		return err
	}
	if walletAutoRechargeHasPermanentAssociationFence(policy.LastError) {
		activeKey, err := walletAutoRechargeAvailableFenceKeyTx(tx, &policy)
		if err != nil {
			return err
		}
		if err := tx.Model(&WalletAutoRecharge{}).Where("id = ?", policyID).Updates(map[string]interface{}{
			"active_key":  activeKey,
			"update_time": getDBTimestampTx(tx),
		}).Error; err != nil {
			return err
		}
		if policy.BillingKeyId > 0 {
			_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
			return err
		}
		return nil
	}
	terminalStatus := WalletAutoRechargeStatusFailed
	if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusCancelPending {
		terminalStatus = WalletAutoRechargeStatusCancelled
	}
	preserveFence := walletAutoRechargeMessageNeedsReplacementFence(message)
	var activeKey interface{}
	if preserveFence {
		availableKey, err := walletAutoRechargeAvailableFenceKeyTx(tx, &policy)
		if err != nil {
			return err
		}
		activeKey = availableKey
	}
	if err := tx.Model(&WalletAutoRecharge{}).
		Where("id = ?", policyID).
		Updates(map[string]interface{}{
			"active_key":    activeKey,
			"status":        terminalStatus,
			"last_trade_no": tradeNo,
			"last_error":    message,
			"update_time":   getDBTimestampTx(tx),
		}).Error; err != nil {
		return err
	}
	if policy.BillingKeyId > 0 {
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
		return err
	}
	return nil
}

func haltWalletAutoRechargeForAssociationConflictTx(tx *gorm.DB, policy *WalletAutoRecharge, tradeNo, message string) error {
	if tx == nil || policy == nil || policy.Id <= 0 {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	activeKey, err := walletAutoRechargeAvailableFenceKeyTx(tx, policy)
	if err != nil {
		return err
	}
	result := tx.Model(&WalletAutoRecharge{}).
		Where("id = ?", policy.Id).
		Updates(map[string]interface{}{
			"active_key":    activeKey,
			"status":        WalletAutoRechargeStatusFailed,
			"last_trade_no": tradeNo,
			"last_error":    message,
			"update_time":   getDBTimestampTx(tx),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	if activeKey == nil {
		policy.ActiveKey = nil
	} else {
		key := activeKey.(string)
		policy.ActiveKey = &key
	}
	policy.Status = WalletAutoRechargeStatusFailed
	policy.LastTradeNo = tradeNo
	policy.LastError = message
	if policy.BillingKeyId > 0 {
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
		return err
	}
	return nil
}

// resolveWalletAutoRechargePaymentEventByAdmin makes the operator decision and
// the local payment-fence transition one atomic mutation. The lock order is the
// wallet financial order (owner -> organization -> billing-key identity ->
// policy), followed by the event and its exact TopUp row. This matches webhook
// settlement and prevents a resolved event from racing a fresh re-halt.
func resolveWalletAutoRechargePaymentEventByAdmin(eventID, policyID, adminID int, resolutionNote string) (*TossPaymentEvent, error) {
	var resolved TossPaymentEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		policy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			return err
		}
		var event TossPaymentEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", eventID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTossPaymentEventNotFound
			}
			return err
		}
		if event.EventType != TossPaymentEventTypeCancellation && event.EventType != TossPaymentEventTypeFinancialMismatch {
			return ErrTossPaymentEventStatusInvalid
		}
		if event.ReconciliationStatus != TossReconciliationStatusRequired && event.ReconciliationStatus != TossReconciliationStatusResolved {
			return ErrTossPaymentEventStatusInvalid
		}
		if event.ReconciliationStatus == TossReconciliationStatusRequired {
			now := getDBTimestampTx(tx)
			result := tx.Model(&TossPaymentEvent{}).
				Where("id = ? AND reconciliation_status = ?", event.Id, TossReconciliationStatusRequired).
				Updates(map[string]interface{}{
					"reconciliation_status": TossReconciliationStatusResolved,
					"resolution_note":       resolutionNote,
					"resolved_time":         now,
					"resolved_by":           adminID,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTossPaymentEventStatusInvalid
			}
			event.ReconciliationStatus = TossReconciliationStatusResolved
			event.ResolutionNote = resolutionNote
			event.ResolvedTime = now
			event.ResolvedBy = adminID
		}
		if err := releaseWalletAutoRechargeFenceAfterAdminEventTx(tx, policy, &event); err != nil {
			return err
		}
		return tx.Where("id = ?", event.Id).First(&resolved).Error
	})
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func releaseWalletAutoRechargeFenceAfterAdminEventTx(tx *gorm.DB, policy *WalletAutoRecharge, event *TossPaymentEvent) error {
	if tx == nil || policy == nil || event == nil {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}
	var topUp TopUp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", event.OrderId).First(&topUp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Keep the replacement fence when the administrator resolves a legacy
			// event whose exact durable payment row is missing. The audit decision
			// remains recorded, but there is no row that can be made non-settleable.
			return nil
		}
		return err
	}
	belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUp, policy)
	if err != nil {
		return err
	}
	if !belongs {
		return ErrWalletAutoRechargeAttemptAssociationConflict
	}

	exactTerminal := topUp.Status == common.TopUpStatusSuccess
	if topUp.Status == common.TopUpStatusPending || topUp.Status == common.TopUpStatusFailed || topUp.Status == common.TopUpStatusExpired {
		now := getDBTimestampTx(tx)
		updates := map[string]interface{}{
			"provider_order_id":    topUp.TradeNo + ":terminal",
			"provider_order_time":  now,
			"provider_retry_time":  0,
			"provider_claim_token": "",
			"provider_claim_time":  0,
			"complete_time":        now,
			"status":               common.TopUpStatusFailed,
		}
		if strings.TrimSpace(event.ProviderPayload) != "" {
			updates["provider_payload"] = event.ProviderPayload
		}
		if err := tx.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(updates).Error; err != nil {
			return err
		}
		exactTerminal = true
	}
	if !exactTerminal {
		return nil
	}

	remaining, err := walletAutoRechargeRequiredPaymentEventCountTx(tx, policy)
	if err != nil || remaining > 0 {
		return err
	}
	if walletAutoRechargeHasPermanentAssociationFence(policy.LastError) {
		if policy.BillingKeyId > 0 {
			_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
			return err
		}
		return nil
	}
	if err := terminalizeWalletAutoRechargeNeverAuthorizedTopUpsTx(tx, policy, event.OrderId); err != nil {
		return err
	}
	unresolvedTopUp, err := walletAutoRechargeHasUnresolvedAssociatedTopUpTx(tx, policy)
	if err != nil || unresolvedTopUp {
		return err
	}

	terminalStatus := WalletAutoRechargeStatusFailed
	if policy.Status == WalletAutoRechargeStatusCancelled || policy.Status == WalletAutoRechargeStatusCancelPending {
		terminalStatus = WalletAutoRechargeStatusCancelled
	}
	if err := tx.Model(&WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"active_key":            nil,
		"status":                terminalStatus,
		"last_error":            "",
		"settlement_retry_time": 0,
		"update_time":           getDBTimestampTx(tx),
	}).Error; err != nil {
		return err
	}
	if policy.BillingKeyId > 0 {
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
		return err
	}
	return nil
}

func recordWalletAutoRechargeFinancialEventWithContext(ctx context.Context, policyID int, event *TossPaymentEvent) (bool, error) {
	created := false
	err := dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		policy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			return err
		}
		var topUp TopUp
		topUpFound := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", event.OrderId).First(&topUp).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) || !strings.HasPrefix(event.OrderId, fmt.Sprintf("wallet_auto_%d_", policy.Id)) {
				return err
			}
			topUpFound = false
		}
		if topUpFound {
			belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUp, policy)
			if err != nil {
				return err
			}
			if !belongs {
				return ErrWalletAutoRechargeAttemptAssociationConflict
			}
		}
		created, err = recordTossPaymentEventDB(tx, event)
		if err != nil {
			return err
		}
		var stored TossPaymentEvent
		if err := tx.Where("event_key = ?", event.EventKey).First(&stored).Error; err != nil {
			return err
		}
		if stored.ReconciliationStatus == TossReconciliationStatusResolved {
			// An administrator already decided this exact evidence. A duplicate
			// provider observation must not recreate either payment marker.
			return nil
		}
		if stored.ReconciliationStatus != TossReconciliationStatusRequired {
			return ErrTossPaymentEventStatusInvalid
		}

		if topUpFound && topUp.Status != common.TopUpStatusSuccess &&
			(topUp.Status == common.TopUpStatusPending || topUp.Status == common.TopUpStatusFailed || topUp.Status == common.TopUpStatusExpired) {
			now := getDBTimestampTx(tx)
			providerStatus := strings.ToUpper(strings.TrimSpace(stored.Status))
			marker := topUp.TradeNo + ":done-mismatch"
			updates := map[string]interface{}{
				"provider_order_id":    marker,
				"provider_order_time":  now,
				"provider_retry_time":  0,
				"provider_claim_token": "",
				"provider_claim_time":  0,
				"provider_payload":     stored.ProviderPayload,
			}
			if providerStatus != "DONE" {
				updates["provider_order_id"] = topUp.TradeNo + ":terminal"
				updates["complete_time"] = now
				updates["status"] = common.TopUpStatusFailed
			}
			if err := tx.Model(&TopUp{}).Where("id = ?", topUp.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
		if !topUpFound {
			message := walletAutoRechargeManualReconciliationPrefix + "financial event has no durable payment row"
			return haltWalletAutoRechargeForAssociationConflictTx(tx, policy, stored.OrderId, message)
		}
		if walletAutoRechargeHasPermanentAssociationFence(policy.LastError) {
			if policy.BillingKeyId > 0 {
				_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
				return err
			}
			return nil
		}
		message := walletAutoRechargeReconciliationPendingPrefix + "financial mismatch event requires admin reconciliation"
		return haltWalletAutoRechargeForReconciliationTx(tx, policy.Id, stored.OrderId, message)
	})
	return created, err
}

func recordWalletAutoRechargeCancellationEventWithContext(ctx context.Context, policyID int, event *TossPaymentEvent) (bool, error) {
	created := false
	err := dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		policy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			return err
		}
		var topUp TopUp
		topUpFound := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", event.OrderId).First(&topUp).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) || !strings.HasPrefix(event.OrderId, fmt.Sprintf("wallet_auto_%d_", policy.Id)) {
				return err
			}
			topUpFound = false
		}
		if topUpFound {
			belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUp, policy)
			if err != nil {
				return err
			}
			if !belongs {
				return ErrWalletAutoRechargeAttemptAssociationConflict
			}
		}
		created, err = recordTossPaymentEventDB(tx, event)
		if err != nil {
			return err
		}
		_, err = haltWalletAutoRechargeForRecordedCancellationTx(tx, policy, false)
		return err
	})
	return created, err
}

func MarkWalletAutoRechargeDoneMismatch(policyId int, tradeNo string, result *TossBillingChargeResult) error {
	return MarkWalletAutoRechargeDoneMismatchWithContext(context.Background(), policyId, tradeNo, result)
}

func MarkWalletAutoRechargeDoneMismatchWithContext(ctx context.Context, policyId int, tradeNo string, result *TossBillingChargeResult) error {
	if result == nil || !strings.EqualFold(strings.TrimSpace(result.ProviderStatus), "DONE") {
		return errors.New("wallet auto recharge DONE mismatch result is invalid")
	}
	return markWalletAutoRechargeFinancialMismatchWithContext(ctx, policyId, tradeNo, result)
}

func MarkWalletAutoRechargeFinancialMismatch(policyId int, tradeNo string, result *TossBillingChargeResult) error {
	return MarkWalletAutoRechargeFinancialMismatchWithContext(context.Background(), policyId, tradeNo, result)
}

func MarkWalletAutoRechargeFinancialMismatchWithContext(ctx context.Context, policyId int, tradeNo string, result *TossBillingChargeResult) error {
	return markWalletAutoRechargeFinancialMismatchWithContext(ctx, policyId, tradeNo, result)
}

func markWalletAutoRechargeFinancialMismatchWithContext(ctx context.Context, policyId int, tradeNo string, result *TossBillingChargeResult) error {
	if result == nil {
		return errors.New("wallet auto recharge financial mismatch result is invalid")
	}
	providerStatus := strings.ToUpper(strings.TrimSpace(result.ProviderStatus))
	if providerStatus != "DONE" && providerStatus != "CANCELED" && providerStatus != "PARTIAL_CANCELED" {
		return errors.New("wallet auto recharge financial mismatch status is invalid")
	}
	var policy WalletAutoRecharge
	db := dbWithContext(ctx)
	if err := db.Where("id = ?", policyId).First(&policy).Error; err != nil {
		return err
	}
	var topUp TopUp
	if err := db.Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return err
	}
	if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
		return ErrPaymentMethodMismatch
	}
	belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
	if associationErr != nil {
		return associationErr
	}
	if !belongs {
		return errors.New("wallet auto recharge top-up association mismatch")
	}
	providerPaymentKey := strings.TrimSpace(result.PaymentKey)
	// Never persist the real paymentKey in the field that authorizes local
	// settlement. Strict validation failed, so only a non-settleable marker may
	// be stored here; the real key is preserved in TossPaymentEvent below.
	mismatchMarker := tradeNo + ":done-mismatch"
	messagePrefix := walletAutoRechargeDoneMismatchPrefix
	if providerStatus != "DONE" {
		mismatchMarker = tradeNo + ":terminal"
		messagePrefix = walletAutoRechargeManualReconciliationPrefix + "provider cancellation payload mismatch"
	}
	message := fmt.Sprintf("%s: expected order=%s amount, actual=%d balance=%d status=%s", messagePrefix, tradeNo, result.Total, result.BalanceAmount, providerStatus)
	message = truncateRunes(message, 255)
	// Preserve the real provider key before freezing the local settlement path.
	// If event persistence fails, leave the deterministic order retryable so the
	// same idempotent provider attempt can surface it again on the next run.
	eventAmount := result.Total
	eventKey := TossFinancialMismatchEventKey(tradeNo, providerPaymentKey, providerStatus, eventAmount, result.BalanceAmount)
	if _, eventErr := RecordTossPaymentEventWithContext(ctx, &TossPaymentEvent{
		EventKey:             eventKey,
		EventType:            TossPaymentEventTypeFinancialMismatch,
		OrderId:              tradeNo,
		PaymentKey:           providerPaymentKey,
		Status:               providerStatus,
		BalanceAmount:        result.BalanceAmount,
		OriginalAmount:       eventAmount,
		ProviderPayload:      result.ProviderPayload,
		ReconciliationStatus: TossReconciliationStatusRequired,
		ResolutionNote:       message,
		CreateTime:           GetDBTimestampWithContext(ctx),
	}); eventErr != nil {
		return fmt.Errorf("record wallet auto recharge financial mismatch event: %w", eventErr)
	}
	resolvedReplay := false
	replayHasRequiredSibling := false
	err := db.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId)
		if err != nil {
			return err
		}
		replay, hasRequiredSibling, err := walletAutoRechargeResolvedEventReplayTx(
			tx, lockedPolicy, eventKey, tradeNo, TossPaymentEventTypeFinancialMismatch, providerStatus,
		)
		if err != nil {
			return err
		}
		if replay {
			resolvedReplay = true
			replayHasRequiredSibling = hasRequiredSibling
			return nil
		}
		now := getDBTimestampTx(tx)
		topUpUpdates := map[string]interface{}{
			"provider_order_id":   mismatchMarker,
			"provider_order_time": now,
			"provider_payload":    result.ProviderPayload,
		}
		if providerStatus != "DONE" {
			topUpUpdates["status"] = common.TopUpStatusFailed
			topUpUpdates["complete_time"] = now
		}
		updated := tx.Model(&TopUp{}).
			Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ?", tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending).
			Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", tradeNo, tradeNo + ":done-mismatch", tradeNo + ":terminal", providerPaymentKey}).
			Updates(topUpUpdates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			var topUp TopUp
			if err := tx.Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
				return err
			}
			if topUp.Status == common.TopUpStatusSuccess {
				// A strict-valid settlement won the race. Never rewrite or reverse an
				// already credited top-up from an older mismatch observation.
				return nil
			}
			expectedStatus := common.TopUpStatusPending
			if providerStatus != "DONE" {
				expectedStatus = common.TopUpStatusFailed
			}
			if topUp.Status != expectedStatus || topUp.ProviderOrderId != mismatchMarker {
				return errors.New("wallet auto recharge financial mismatch payment conflict")
			}
		}
		return haltWalletAutoRechargeForReconciliationTx(tx, policyId, tradeNo, message)
	})
	if err != nil {
		return err
	}
	if resolvedReplay {
		if replayHasRequiredSibling {
			return fmt.Errorf("%w: order_id=%s", ErrWalletAutoRechargeReconciliationRequired, tradeNo)
		}
		return nil
	}
	return fmt.Errorf("%w: order_id=%s", ErrWalletAutoRechargeReconciliationRequired, tradeNo)
}

// enforceWalletAutoRechargeCancellationPrecedenceTx prevents an older DONE
// observation from automatically crediting after an authoritative cancellation
// event has already been made durable. Event persistence and terminal-state
// application happen in separate webhook transactions, so every DONE settlement
// transaction must perform this check itself.
func enforceWalletAutoRechargeCancellationPrecedenceTx(tx *gorm.DB, policy *WalletAutoRecharge, topUp *TopUp, tradeNo string) (blocked bool, required bool, err error) {
	var events []TossPaymentEvent
	if err := tx.Select("id", "reconciliation_status").
		Where("order_id = ? AND event_type = ? AND status IN ?", tradeNo, TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"}).
		Find(&events).Error; err != nil {
		return false, false, err
	}
	if len(events) == 0 {
		return false, false, nil
	}
	for i := range events {
		if events[i].ReconciliationStatus == TossReconciliationStatusRequired {
			required = true
			break
		}
	}
	now := getDBTimestampTx(tx)
	if topUp.Status != common.TopUpStatusSuccess {
		terminalMarker := tradeNo + ":terminal"
		if err := tx.Model(&TopUp{}).
			Where("id = ? AND status <> ?", topUp.Id, common.TopUpStatusSuccess).
			Updates(map[string]interface{}{
				"provider_order_id":   terminalMarker,
				"provider_order_time": now,
				"status":              common.TopUpStatusFailed,
			}).Error; err != nil {
			return false, false, err
		}
	}
	if !required {
		// Resolved cancellation evidence remains authoritative over a delayed
		// DONE, but the administrator's decision is sticky: terminalize/no-op
		// without recreating the policy or billing-key fence.
		return true, false, nil
	}
	if err := haltWalletAutoRechargeForReconciliationTx(
		tx,
		policy.Id,
		tradeNo,
		walletAutoRechargeReconciliationPendingPrefix+"authoritative cancellation event precedes DONE",
	); err != nil {
		return false, false, err
	}
	return true, true, nil
}

// RecordWalletAutoRechargeProviderDONEEvidenceWithContext durably records a
// strict-valid provider result before quota crediting. In particular, it can
// reopen a failed/expired stale attempt as a locally-settleable pending row
// without changing the terminal policy or billing-key lifecycle. This closes
// the race where a stale GET-404 transaction commits while an already-authorized
// provider POST is still in flight.
func RecordWalletAutoRechargeProviderDONEEvidenceWithContext(ctx context.Context, policyID int, tradeNo string, result *TossBillingChargeResult) error {
	if result == nil || !result.Done {
		return errors.New("wallet auto recharge provider charge is not complete")
	}
	paymentKey := strings.TrimSpace(result.PaymentKey)
	if policyID <= 0 || strings.TrimSpace(tradeNo) == "" || paymentKey == "" {
		return errors.New("wallet auto recharge provider DONE evidence is invalid")
	}
	db := dbWithContext(ctx)
	cancellationPrecedence := false
	cancellationRequiresReconciliation := false
	err := db.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyID)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return errors.New("wallet auto recharge top-up association mismatch")
		}
		blocked, required, err := enforceWalletAutoRechargeCancellationPrecedenceTx(tx, &policy, &topUp, tradeNo)
		if err != nil {
			return err
		}
		if blocked {
			cancellationPrecedence = true
			cancellationRequiresReconciliation = required
			return nil
		}
		if topUp.Status == common.TopUpStatusSuccess {
			if topUp.ProviderOrderId != paymentKey {
				return errors.New("wallet auto recharge Toss paymentKey conflict")
			}
			return nil
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed && topUp.Status != common.TopUpStatusExpired {
			return ErrTopUpStatusInvalid
		}
		allowedProviderIDs := []string{"", tradeNo, tradeNo + ":charged", paymentKey}
		nowUnix := getDBTimestampTx(tx)
		updated := tx.Model(&TopUp{}).
			Where("id = ? AND status IN ?", topUp.Id, []string{common.TopUpStatusPending, common.TopUpStatusFailed, common.TopUpStatusExpired}).
			Where("provider_order_id IS NULL OR provider_order_id IN ?", allowedProviderIDs).
			Updates(map[string]interface{}{
				"status":              common.TopUpStatusPending,
				"provider_order_id":   paymentKey,
				"provider_order_time": nowUnix,
				"provider_payload":    result.ProviderPayload,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess && current.ProviderOrderId == paymentKey {
				return nil
			}
			if (current.Status == common.TopUpStatusFailed || current.Status == common.TopUpStatusExpired) &&
				current.ProviderOrderId == tradeNo+":terminal" {
				return ErrWalletAutoRechargeReconciliationRequired
			}
			if current.Status != common.TopUpStatusPending || current.ProviderOrderId != paymentKey {
				return errors.New("wallet auto recharge Toss paymentKey conflict")
			}
		}
		return tx.Model(&WalletAutoRecharge{}).
			Where("id = ?", policy.Id).
			Updates(map[string]interface{}{
				"last_trade_no": tradeNo,
				"last_error":    walletAutoRechargeReconciliationPendingPrefix + "provider DONE recorded",
				"update_time":   nowUnix,
			}).Error
	})
	if err != nil {
		return err
	}
	if cancellationPrecedence {
		if cancellationRequiresReconciliation {
			return ErrWalletAutoRechargeReconciliationRequired
		}
		return nil
	}
	return recordWalletAutoRechargeFulfillmentEventByTradeNoWithContext(ctx, tradeNo)
}

func recordWalletAutoRechargeFulfillmentEventByTradeNo(tradeNo string) error {
	return recordWalletAutoRechargeFulfillmentEventByTradeNoWithContext(context.Background(), tradeNo)
}

func walletAutoRechargeFulfillmentEventKey(topUp *TopUp) string {
	if topUp == nil {
		return ""
	}
	seed := fmt.Sprintf("%d\x00%s", topUp.Id, strings.TrimSpace(topUp.ProviderOrderId))
	return "wallet_fulfillment_" + common.Sha1([]byte(seed))
}

func recordWalletAutoRechargeFulfillmentEventByTradeNoWithContext(ctx context.Context, tradeNo string) error {
	var topUp TopUp
	if err := dbWithContext(ctx).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return err
	}
	if !isWalletAutoRechargeLocallySettleable(&topUp) && topUp.Status != common.TopUpStatusSuccess {
		return errors.New("wallet auto recharge provider charge is not recorded")
	}
	_, err := RecordTossPaymentEventWithContext(ctx, &TossPaymentEvent{
		EventKey:             walletAutoRechargeFulfillmentEventKey(&topUp),
		EventType:            TossPaymentEventTypeFulfillment,
		OrderId:              topUp.TradeNo,
		PaymentKey:           topUp.ProviderOrderId,
		Status:               "DONE",
		OriginalAmount:       topUp.Amount,
		ProviderPayload:      topUp.ProviderPayload,
		ReconciliationStatus: TossReconciliationStatusRequired,
		CreateTime:           GetDBTimestampWithContext(ctx),
	})
	return err
}

func completeWalletAutoRechargeTopUp(policyId int, tradeNo string, now time.Time) error {
	cancellationBlocked := false
	cancellationRequiresReconciliation := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return errors.New("wallet auto recharge top-up association mismatch")
		}
		blocked, required, err := enforceWalletAutoRechargeCancellationPrecedenceTx(tx, &policy, &topUp, tradeNo)
		if err != nil {
			return err
		}
		if blocked {
			cancellationBlocked = true
			cancellationRequiresReconciliation = required
			return nil
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return finalizeWalletAutoRechargeSettlement(tx, &policy, tradeNo, now)
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if !isWalletAutoRechargeLocallySettleable(&topUp) {
			return errors.New("wallet auto recharge provider charge is not recorded")
		}

		claim := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND provider_order_id = ?", topUp.Id, common.TopUpStatusPending, topUp.ProviderOrderId).
			Updates(map[string]interface{}{
				"complete_time":        now.Unix(),
				"status":               common.TopUpStatusSuccess,
				"provider_attempted":   false,
				"provider_claim_token": "",
				"provider_claim_time":  0,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess {
				return nil
			}
			return ErrTopUpStatusInvalid
		}
		topUp.CompleteTime = now.Unix()
		topUp.Status = common.TopUpStatusSuccess
		quotaToAdd := CreditedQuotaForTossTopUp(&topUp)
		if quotaToAdd <= 0 {
			return errors.New("invalid wallet auto recharge quota")
		}
		if err := CreditTopUpTarget(tx, &topUp, int64(quotaToAdd)); err != nil {
			return err
		}
		return finalizeWalletAutoRechargeSettlement(tx, &policy, tradeNo, now)
	})
	if err != nil {
		return err
	}
	if cancellationBlocked {
		if cancellationRequiresReconciliation {
			return ErrWalletAutoRechargeReconciliationRequired
		}
		return nil
	}
	return nil
}

// SettleWalletAutoRechargeWebhookDone records the authoritative paymentKey,
// credits quota exactly once, and finalizes the policy schedule in one DB
// transaction. It also repairs the case where generic settlement credited the
// TopUp before the wallet policy was finalized.
func SettleWalletAutoRechargeWebhookDone(policyId int, tradeNo, paymentKey, providerPayload string, now time.Time) error {
	return SettleWalletAutoRechargeWebhookDoneWithContext(context.Background(), policyId, tradeNo, paymentKey, providerPayload, now)
}

func SettleWalletAutoRechargeWebhookDoneWithContext(ctx context.Context, policyId int, tradeNo, paymentKey, providerPayload string, now time.Time) error {
	tradeNo = strings.TrimSpace(tradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	if policyId <= 0 || tradeNo == "" || paymentKey == "" {
		return errors.New("invalid wallet auto recharge webhook settlement")
	}
	if now.IsZero() {
		now = time.Unix(GetDBTimestampWithContext(ctx), 0).In(time.Local)
	} else {
		now = now.In(time.Local)
	}
	needsReconciliation := false
	resolvedCancellationBlocked := false
	err := runTossSettlementTransaction(dbWithContext(ctx), func(tx *gorm.DB) error {
		// SQLite may replay this closure after SQLITE_BUSY. Do not retain a flag
		// set by a transaction attempt that was rolled back at commit time.
		needsReconciliation = false
		resolvedCancellationBlocked = false
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return errors.New("wallet auto recharge top-up association mismatch")
		}
		blocked, required, err := enforceWalletAutoRechargeCancellationPrecedenceTx(tx, &policy, &topUp, tradeNo)
		if err != nil {
			return err
		}
		if blocked {
			needsReconciliation = required
			resolvedCancellationBlocked = !required
			return nil
		}
		allowedProviderIDs := []string{"", tradeNo, tradeNo + ":charged", paymentKey}
		metadata := tx.Model(&TopUp{}).
			Where("id = ? AND (provider_order_id IS NULL OR provider_order_id IN ?)", topUp.Id, allowedProviderIDs).
			Updates(map[string]interface{}{
				"provider_order_id":   paymentKey,
				"provider_order_time": now.Unix(),
				"provider_payload":    providerPayload,
			})
		if metadata.Error != nil {
			return metadata.Error
		}
		if metadata.RowsAffected == 0 {
			if err := tx.Where("id = ?", topUp.Id).First(&topUp).Error; err != nil {
				return err
			}
			if topUp.ProviderOrderId != paymentKey {
				return errors.New("wallet auto recharge Toss paymentKey conflict")
			}
		}
		topUp.ProviderOrderId = paymentKey
		topUp.ProviderOrderTime = now.Unix()
		topUp.ProviderPayload = providerPayload

		if topUp.Status == common.TopUpStatusSuccess {
			return finalizeWalletAutoRechargeSettlement(tx, &policy, tradeNo, now)
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed && topUp.Status != common.TopUpStatusExpired {
			needsReconciliation = true
			return tx.Model(&WalletAutoRecharge{}).
				Where("id = ?", policy.Id).
				Updates(map[string]interface{}{
					"last_trade_no": tradeNo,
					"last_error":    walletAutoRechargeReconciliationPendingPrefix + "provider DONE after local terminal state",
					"update_time":   now.Unix(),
				}).Error
		}

		claim := tx.Model(&TopUp{}).
			Where("id = ? AND status IN ? AND provider_order_id = ?", topUp.Id,
				[]string{common.TopUpStatusPending, common.TopUpStatusFailed, common.TopUpStatusExpired}, paymentKey).
			Updates(map[string]interface{}{
				"complete_time":        now.Unix(),
				"status":               common.TopUpStatusSuccess,
				"provider_attempted":   false,
				"provider_claim_token": "",
				"provider_claim_time":  0,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess {
				return finalizeWalletAutoRechargeSettlement(tx, &policy, tradeNo, now)
			}
			return ErrTopUpStatusInvalid
		}
		quotaToAdd := CreditedQuotaForTossTopUp(&topUp)
		if quotaToAdd <= 0 {
			return errors.New("invalid wallet auto recharge quota")
		}
		if err := CreditTopUpTarget(tx, &topUp, int64(quotaToAdd)); err != nil {
			return err
		}
		return finalizeWalletAutoRechargeSettlement(tx, &policy, tradeNo, now)
	})
	if err != nil {
		return err
	}
	if resolvedCancellationBlocked {
		return nil
	}
	if needsReconciliation {
		if eventErr := recordWalletAutoRechargeFulfillmentEventForAnyStateWithContext(ctx, tradeNo); eventErr != nil {
			return fmt.Errorf("record wallet auto recharge fulfillment reconciliation: %w", eventErr)
		}
		return ErrWalletAutoRechargeReconciliationRequired
	}
	if eventErr := recordWalletAutoRechargeFulfillmentEventByTradeNoWithContext(ctx, tradeNo); eventErr != nil {
		_ = markWalletAutoRechargeEventResolutionPendingWithContext(ctx, policyId, tradeNo, eventErr)
		return eventErr
	}
	if resolveErr := ResolveTossPaymentEventsWithContext(ctx, tradeNo, TossPaymentEventTypeFulfillment); resolveErr != nil {
		if markErr := markWalletAutoRechargeEventResolutionPendingWithContext(ctx, policyId, tradeNo, resolveErr); markErr != nil {
			return errors.Join(resolveErr, markErr)
		}
		return resolveErr
	}
	return nil
}

// ApplyWalletAutoRechargeWebhookTerminal closes an authoritative no-balance
// terminal attempt exactly once and advances the policy's deterministic failure
// sequence. PARTIAL_CANCELED/non-zero-balance outcomes instead halt the policy
// and billing key for manual reconciliation, because another automatic charge
// could double-charge the remaining provider balance.
func ApplyWalletAutoRechargeWebhookTerminal(policyId int, tradeNo, providerStatus, providerPayload string, requiresReconciliation bool, now time.Time) error {
	return ApplyWalletAutoRechargeWebhookTerminalWithContext(context.Background(), policyId, tradeNo, providerStatus, providerPayload, requiresReconciliation, now)
}

func ApplyWalletAutoRechargeWebhookTerminalWithContext(ctx context.Context, policyId int, tradeNo, providerStatus, providerPayload string, requiresReconciliation bool, now time.Time) error {
	tradeNo = strings.TrimSpace(tradeNo)
	providerStatus = strings.ToUpper(strings.TrimSpace(providerStatus))
	if policyId <= 0 || tradeNo == "" {
		return errors.New("invalid wallet auto recharge terminal settlement")
	}
	switch providerStatus {
	case "ABORTED", "EXPIRED", "CANCELED", "PARTIAL_CANCELED":
	default:
		return errors.New("invalid wallet auto recharge terminal provider status")
	}
	if providerStatus == "PARTIAL_CANCELED" {
		requiresReconciliation = true
	}
	if now.IsZero() {
		now = time.Unix(GetDBTimestampWithContext(ctx), 0).UTC()
	} else {
		now = now.UTC()
	}
	reconciliation := false
	err := dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		var topUp TopUp
		if err := tx.Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return errors.New("wallet auto recharge top-up association mismatch")
		}
		if providerStatus == "CANCELED" || providerStatus == "PARTIAL_CANCELED" {
			replay, hasRequiredSibling, err := walletAutoRechargeResolvedEventReplayTx(
				tx, &policy, "", tradeNo, TossPaymentEventTypeCancellation, providerStatus,
			)
			if err != nil {
				return err
			}
			if replay {
				// The administrator's decision is sticky. A required sibling still
				// reports reconciliation, but this older resolved snapshot cannot
				// rewrite the TopUp or recreate a policy marker.
				reconciliation = hasRequiredSibling
				return nil
			}
		}

		terminalMarker := tradeNo + ":terminal"
		// A local success or a durable real paymentKey means provider money may
		// already have won this race. Never turn it into a retryable failure.
		if topUp.Status == common.TopUpStatusSuccess {
			reconciliation = true
			return haltWalletAutoRechargeForReconciliationTx(
				tx,
				policy.Id,
				tradeNo,
				walletAutoRechargeReconciliationPendingPrefix+"provider terminal raced recorded charge",
			)
		}
		if hasWalletAutoRechargeRealProviderPaymentKey(&topUp) {
			// A later terminal provider state invalidates automatic full-value
			// credit. Replace the settlement-authorizing field with a fixed marker;
			// the real paymentKey remains in the durable provider payload and (for
			// cancellations) the cancellation event persisted by the controller.
			if err := tx.Model(&TopUp{}).Where("id = ? AND status = ? AND provider_order_id = ?", topUp.Id, topUp.Status, topUp.ProviderOrderId).
				Updates(map[string]interface{}{
					"provider_order_id":   terminalMarker,
					"provider_order_time": now.Unix(),
					"provider_payload":    providerPayload,
					"status":              common.TopUpStatusFailed,
				}).Error; err != nil {
				return err
			}
			if providerStatus == "CANCELED" && !requiresReconciliation {
				return haltWalletAutoRechargeForReconciliationTx(
					tx,
					policy.Id,
					tradeNo,
					"wallet auto recharge provider payment canceled",
				)
			}
			reconciliation = true
			return haltWalletAutoRechargeForReconciliationTx(
				tx,
				policy.Id,
				tradeNo,
				walletAutoRechargeReconciliationPendingPrefix+"provider terminal raced recorded charge",
			)
		}

		if topUp.Status != common.TopUpStatusPending {
			// Webhook redelivery after the first successful terminal CAS is a no-op.
			if topUp.Status == common.TopUpStatusFailed || topUp.Status == common.TopUpStatusExpired {
				if requiresReconciliation {
					reconciliation = true
					return haltWalletAutoRechargeForReconciliationTx(
						tx,
						policy.Id,
						tradeNo,
						walletAutoRechargeReconciliationPendingPrefix+"provider cancellation retains balance",
					)
				}
				if providerStatus == "CANCELED" {
					return haltWalletAutoRechargeForReconciliationTx(
						tx,
						policy.Id,
						tradeNo,
						"wallet auto recharge provider payment canceled",
					)
				}
				if (policy.Status == WalletAutoRechargeStatusCancelPending ||
					(policy.Status == WalletAutoRechargeStatusFailed && strings.HasPrefix(policy.LastError, walletAutoRechargeLateDONEFencePrefix))) &&
					!walletAutoRechargeHasNonReleasableReconciliationFence(policy.LastError) {
					cause := fmt.Errorf("wallet auto recharge provider terminal: %s", providerStatus)
					return markWalletAutoRechargeFailure(tx, policy.Id, policy.BillingKeyId, TossBillingMaxFails, cause, tradeNo)
				}
				return nil
			}
			return ErrTopUpStatusInvalid
		}

		targetStatus := common.TopUpStatusFailed
		if providerStatus == "EXPIRED" {
			targetStatus = common.TopUpStatusExpired
		}
		claim := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND (provider_order_id IS NULL OR provider_order_id IN ?)", topUp.Id, common.TopUpStatusPending,
				[]string{"", tradeNo, terminalMarker}).
			Updates(map[string]interface{}{
				"provider_order_id":   terminalMarker,
				"provider_order_time": now.Unix(),
				"provider_payload":    providerPayload,
				"status":              targetStatus,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess {
				reconciliation = true
				return haltWalletAutoRechargeForReconciliationTx(
					tx,
					policy.Id,
					tradeNo,
					walletAutoRechargeReconciliationPendingPrefix+"provider charge won terminal CAS",
				)
			}
			if hasWalletAutoRechargeRealProviderPaymentKey(&current) {
				if err := tx.Model(&TopUp{}).Where("id = ? AND status = ? AND provider_order_id = ?", current.Id, current.Status, current.ProviderOrderId).
					Updates(map[string]interface{}{
						"provider_order_id":   terminalMarker,
						"provider_order_time": now.Unix(),
						"provider_payload":    providerPayload,
						"status":              common.TopUpStatusFailed,
					}).Error; err != nil {
					return err
				}
				reconciliation = true
				return haltWalletAutoRechargeForReconciliationTx(
					tx,
					policy.Id,
					tradeNo,
					walletAutoRechargeReconciliationPendingPrefix+"provider charge won terminal CAS",
				)
			}
			if (current.Status == common.TopUpStatusFailed || current.Status == common.TopUpStatusExpired) && current.ProviderOrderId == terminalMarker {
				if providerStatus == "CANCELED" {
					return haltWalletAutoRechargeForReconciliationTx(
						tx,
						policy.Id,
						tradeNo,
						"wallet auto recharge provider payment canceled",
					)
				}
				return nil
			}
			return ErrTopUpStatusInvalid
		}

		if requiresReconciliation {
			reconciliation = true
			return haltWalletAutoRechargeForReconciliationTx(
				tx,
				policy.Id,
				tradeNo,
				walletAutoRechargeReconciliationPendingPrefix+"provider cancellation retains balance",
			)
		}
		if providerStatus == "CANCELED" {
			// A CANCELED payment may have reached DONE and then been fully
			// refunded. Starting a fresh order on the next tick can create a
			// charge/refund loop, so stop the policy and queue its billing key for
			// deletion if no other recurring contract still references it.
			return haltWalletAutoRechargeForReconciliationTx(
				tx,
				policy.Id,
				tradeNo,
				"wallet auto recharge provider payment canceled",
			)
		}
		cause := fmt.Errorf("wallet auto recharge provider terminal: %s", providerStatus)
		return markWalletAutoRechargeFailure(tx, policy.Id, policy.BillingKeyId, TossBillingMaxFails, cause, tradeNo)
	})
	if err != nil {
		return err
	}
	if reconciliation {
		return ErrWalletAutoRechargeReconciliationRequired
	}
	return nil
}

func recordWalletAutoRechargeFulfillmentEventForAnyState(tradeNo string) error {
	return recordWalletAutoRechargeFulfillmentEventForAnyStateWithContext(context.Background(), tradeNo)
}

func recordWalletAutoRechargeFulfillmentEventForAnyStateWithContext(ctx context.Context, tradeNo string) error {
	var topUp TopUp
	if err := dbWithContext(ctx).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		return err
	}
	_, err := RecordTossPaymentEventWithContext(ctx, &TossPaymentEvent{
		EventKey:             walletAutoRechargeFulfillmentEventKey(&topUp),
		EventType:            TossPaymentEventTypeFulfillment,
		OrderId:              topUp.TradeNo,
		PaymentKey:           topUp.ProviderOrderId,
		Status:               "DONE",
		OriginalAmount:       topUp.Amount,
		ProviderPayload:      topUp.ProviderPayload,
		ReconciliationStatus: TossReconciliationStatusRequired,
		CreateTime:           GetDBTimestampWithContext(ctx),
	})
	return err
}

func finalizeWalletAutoRechargeSettlement(tx *gorm.DB, policy *WalletAutoRecharge, tradeNo string, now time.Time) error {
	if policy.LastTradeNo == tradeNo && policy.LastChargeTime > 0 && policy.LastError == "" {
		return nil
	}
	if policy.LastTradeNo == tradeNo && policy.LastChargeTime > 0 && strings.HasPrefix(policy.LastError, walletAutoRechargeEventResolutionPendingPrefix) {
		return tx.Model(&WalletAutoRecharge{}).
			Where("id = ?", policy.Id).
			Updates(map[string]interface{}{
				"last_error":            "",
				"settlement_retry_time": 0,
				"update_time":           now.Unix(),
			}).Error
	}
	if policy.Status == WalletAutoRechargeStatusActive {
		return updateWalletAutoRechargeSuccess(tx, policy, tradeNo, now)
	}
	// An association/manual-reconciliation fence can cover more than one
	// physical provider orderId. Settling one delayed DONE must credit that exact
	// payment idempotently, but cannot prove every conflicting alias terminal.
	// Preserve its unique key and operator marker until explicit reconciliation.
	if strings.HasPrefix(policy.LastError, walletAutoRechargeManualReconciliationPrefix) {
		return tx.Model(policy).Updates(map[string]interface{}{
			"last_charge_time":      now.Unix(),
			"last_trade_no":         tradeNo,
			"settlement_retry_time": 0,
			"update_time":           now.Unix(),
		}).Error
	}

	updates := map[string]interface{}{
		"last_charge_time":      now.Unix(),
		"last_trade_no":         tradeNo,
		"last_error":            "",
		"settlement_retry_time": 0,
		"update_time":           now.Unix(),
	}
	releaseFence := policy.ActiveKey != nil
	if releaseFence {
		updates["active_key"] = nil
	}
	if policy.Status == WalletAutoRechargeStatusCancelPending {
		updates["status"] = WalletAutoRechargeStatusCancelled
	}
	if err := tx.Model(policy).Updates(updates).Error; err != nil {
		return err
	}
	if releaseFence && policy.BillingKeyId > 0 {
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
		return err
	}
	return nil
}

func updateWalletAutoRechargeSuccess(tx *gorm.DB, policy *WalletAutoRecharge, tradeNo string, now time.Time) error {
	updates := map[string]interface{}{
		"last_charge_time":      now.Unix(),
		"last_trade_no":         tradeNo,
		"fail_count":            0,
		"last_error":            "",
		"settlement_retry_time": 0,
		"update_time":           now.Unix(),
	}
	if policy.Type == WalletAutoRechargeTypeScheduled {
		// Do not backfill every missed interval after downtime. Advancing from a
		// stale due timestamp can leave next_charge_time in the past and trigger one
		// card charge on every task tick until the schedule catches up.
		nextChargeTime, err := nextWalletChargeTime(now, policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds)
		if err != nil {
			return err
		}
		updates["next_charge_time"] = nextChargeTime.Unix()
	}
	if policy.Type == WalletAutoRechargeTypeThreshold {
		if now.IsZero() {
			now = time.Unix(GetDBTimestamp(), 0)
		}
		updates["cooldown_until"] = now.Add(WalletAutoRechargeThresholdCooldownSeconds * time.Second).Unix()
		today, dailyChargeCount := walletAutoRechargeUTCDailyState(policy, now)
		updates["daily_charge_date"] = today
		updates["daily_charge_date_utc"] = true
		updates["daily_charge_count"] = dailyChargeCount + 1
	}
	result := tx.Model(&WalletAutoRecharge{}).
		Where("id = ? AND status = ?", policy.Id, WalletAutoRechargeStatusActive).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return tx.Model(&WalletAutoRecharge{}).
		Where("id = ?", policy.Id).
		Updates(map[string]interface{}{
			"last_charge_time":      now.Unix(),
			"last_trade_no":         tradeNo,
			"last_error":            "",
			"settlement_retry_time": 0,
			"update_time":           now.Unix(),
		}).Error
}

func markWalletAutoRechargeChargeFailure(policyId int, tradeNo string, maxFails int, cause error) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId); err != nil {
			return err
		}
		now := getDBTimestampTx(tx)
		claim := tx.Model(&TopUp{}).
			Where("trade_no = ? AND payment_provider = ? AND payment_method = ? AND status = ?", tradeNo, PaymentProviderToss, PaymentMethodToss, common.TopUpStatusPending).
			Where("provider_order_id IS NULL OR provider_order_id IN ?", []string{"", tradeNo}).
			Updates(map[string]interface{}{
				"provider_order_id":   tradeNo + ":terminal",
				"provider_order_time": now,
				"complete_time":       now,
				"status":              common.TopUpStatusFailed,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			var current TopUp
			err := tx.Where("trade_no = ?", tradeNo).First(&current).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return markWalletAutoRechargeFailure(tx, policyId, 0, maxFails, cause, tradeNo)
			}
			if err != nil {
				return err
			}
			if current.Status == common.TopUpStatusSuccess || isWalletAutoRechargeLocallySettleable(&current) {
				return tx.Model(&WalletAutoRecharge{}).
					Where("id = ?", policyId).
					Updates(map[string]interface{}{
						"last_trade_no": tradeNo,
						"last_error":    walletAutoRechargeReconciliationPendingPrefix + "provider charge won terminal-result race",
						"update_time":   getDBTimestampTx(tx),
					}).Error
			}
			// Another process already closed the same terminal attempt.
			return nil
		}
		return markWalletAutoRechargeFailure(tx, policyId, 0, maxFails, cause, tradeNo)
	})
}

func markWalletAutoRechargeProviderPending(policyId int, tradeNo string, cause error) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if policyId <= 0 || tradeNo == "" {
		return ErrWalletAutoRechargeClaimLost
	}
	message := walletAutoRechargeProviderPendingPrefix + walletAutoRechargeCauseMessage(cause)
	message = truncateRunes(message, 255)
	return DB.Transaction(func(tx *gorm.DB) error {
		lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, policyId)
		if err != nil {
			return err
		}
		policy := *lockedPolicy
		if policy.Status != WalletAutoRechargeStatusActive || policy.LastTradeNo != tradeNo ||
			strings.HasPrefix(policy.LastError, walletAutoRechargeReconciliationPendingPrefix) ||
			(policy.LastChargeTime > 0 && policy.LastError == "") {
			// A cancellation, local settlement, or reconciliation decision is
			// authoritative over an older ambiguous provider observation.
			return nil
		}
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		belongs, associationErr := walletAutoRechargeTopUpBelongsToPolicy(&topUp, &policy)
		if associationErr != nil {
			return associationErr
		}
		if !belongs {
			return nil
		}
		blocked, _, err := enforceWalletAutoRechargeCancellationPrecedenceTx(tx, &policy, &topUp, tradeNo)
		if err != nil {
			return err
		}
		if blocked || topUp.Status != common.TopUpStatusPending || isWalletAutoRechargeLocallySettleable(&topUp) {
			return nil
		}
		now := getDBTimestampTx(tx)
		return tx.Model(&WalletAutoRecharge{}).
			Where("id = ? AND status = ? AND last_trade_no = ?", policy.Id, WalletAutoRechargeStatusActive, tradeNo).
			Where("last_error NOT LIKE ?", walletAutoRechargeReconciliationPendingPrefix+"%").
			Where("NOT (last_charge_time > ? AND last_error = ?)", 0, "").
			Updates(map[string]interface{}{
				"last_error":  message,
				"update_time": now,
			}).Error
	})
}

func markWalletAutoRechargeReconciliationPending(policyId int, tradeNo string, cause error) error {
	message := walletAutoRechargeReconciliationPendingPrefix + walletAutoRechargeCauseMessage(cause)
	message = truncateRunes(message, 255)
	return DB.Model(&WalletAutoRecharge{}).
		Where("id = ?", policyId).
		Updates(map[string]interface{}{
			"last_trade_no": tradeNo,
			"last_error":    message,
			"update_time":   GetDBTimestamp(),
		}).Error
}

func markWalletAutoRechargeEventResolutionPending(policyId int, tradeNo string, cause error) error {
	return markWalletAutoRechargeEventResolutionPendingWithContext(context.Background(), policyId, tradeNo, cause)
}

func markWalletAutoRechargeEventResolutionPendingWithContext(ctx context.Context, policyId int, tradeNo string, cause error) error {
	message := walletAutoRechargeEventResolutionPendingPrefix + walletAutoRechargeCauseMessage(cause)
	message = truncateRunes(message, 255)
	return dbWithContext(ctx).Model(&WalletAutoRecharge{}).
		Where("id = ?", policyId).
		Updates(map[string]interface{}{
			"last_trade_no": tradeNo,
			"last_error":    message,
			"update_time":   GetDBTimestampWithContext(ctx),
		}).Error
}

func walletAutoRechargeCauseMessage(cause error) string {
	if cause == nil {
		return "unknown error"
	}
	return cause.Error()
}

func markWalletAutoRechargeFailure(tx *gorm.DB, policyId int, billingKeyId int, maxFails int, cause error, tradeNo string) error {
	if maxFails <= 0 {
		maxFails = 1
	}
	message := walletAutoRechargeCauseMessage(cause)
	message = truncateRunes(message, 255)
	now := getDBTimestampTx(tx)
	updates := map[string]interface{}{
		"fail_count":  gorm.Expr("fail_count + ?", 1),
		"last_error":  message,
		"update_time": now,
	}
	if strings.TrimSpace(tradeNo) != "" {
		updates["last_trade_no"] = tradeNo
	}
	incremented := tx.Model(&WalletAutoRecharge{}).
		Where("id = ? AND status = ?", policyId, WalletAutoRechargeStatusActive).
		Updates(updates)
	if incremented.Error != nil {
		return incremented.Error
	}
	if incremented.RowsAffected == 0 {
		var policy WalletAutoRecharge
		if err := tx.Where("id = ?", policyId).First(&policy).Error; err != nil {
			return err
		}
		releasableLateDONEFence := policy.Status == WalletAutoRechargeStatusFailed &&
			strings.HasPrefix(policy.LastError, walletAutoRechargeLateDONEFencePrefix)
		if (policy.Status != WalletAutoRechargeStatusCancelPending && !releasableLateDONEFence) ||
			walletAutoRechargeHasNonReleasableReconciliationFence(policy.LastError) {
			return nil
		}
		terminalStatus := WalletAutoRechargeStatusFailed
		if policy.Status == WalletAutoRechargeStatusCancelPending {
			terminalStatus = WalletAutoRechargeStatusCancelled
		}
		terminalUpdates := map[string]interface{}{
			"active_key":            nil,
			"status":                terminalStatus,
			"last_error":            message,
			"settlement_retry_time": 0,
			"update_time":           now,
		}
		if strings.TrimSpace(tradeNo) != "" {
			terminalUpdates["last_trade_no"] = tradeNo
		}
		if err := tx.Model(&WalletAutoRecharge{}).Where("id = ? AND status = ?", policyId, policy.Status).
			Updates(terminalUpdates).Error; err != nil {
			return err
		}
		if policy.BillingKeyId > 0 {
			_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, policy.BillingKeyId)
			return err
		}
		return nil
	}
	transitioned := tx.Model(&WalletAutoRecharge{}).
		Where("id = ? AND status = ? AND fail_count >= ?", policyId, WalletAutoRechargeStatusActive, maxFails).
		Updates(map[string]interface{}{
			"active_key":  nil,
			"status":      WalletAutoRechargeStatusFailed,
			"update_time": now,
		})
	if transitioned.Error != nil || transitioned.RowsAffected == 0 {
		return transitioned.Error
	}
	if billingKeyId <= 0 {
		var policy WalletAutoRecharge
		if err := tx.Select("billing_key_id").Where("id = ?", policyId).First(&policy).Error; err != nil {
			return err
		}
		billingKeyId = policy.BillingKeyId
	}
	if billingKeyId > 0 {
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, billingKeyId)
		return err
	}
	return nil
}
