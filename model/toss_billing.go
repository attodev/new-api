package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserBillingKey stores a Toss billing key (암호화) for recurring charges.
type UserBillingKey struct {
	Id                 int    `json:"id"`
	UserId             int    `json:"user_id" gorm:"index"`
	CustomerKey        string `json:"customer_key" gorm:"type:varchar(64);index"`
	EncryptedKey       string `json:"-" gorm:"type:text"` // AES-GCM(billingKey)
	BillingKeyHash     string `json:"-" gorm:"type:varchar(64);index"`
	ProviderCredential string `json:"-" gorm:"type:text"`
	// ProviderClientKeyHash identifies the Toss billing MID that issued this key.
	// The client key is public, but keeping only its hash avoids persisting more
	// provider configuration than is needed. It lets secret-key rotation prefer
	// the current secret only when it belongs to the same MID.
	ProviderClientKeyHash string `json:"-" gorm:"type:varchar(64)"`
	CardCompany           string `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked      string `json:"card_number_masked" gorm:"type:varchar(32)"`
	Status                string `json:"status" gorm:"type:varchar(32);default:'active'"` // active | pending_revocation | revoked
	// RevocationRetryTime is a retry lease and fairness cursor for remote key
	// deletion. Failed low-ID rows must not occupy every bounded batch forever.
	RevocationRetryTime int64 `json:"-" gorm:"default:0;index"`
	CreateTime          int64 `json:"create_time"`
}

const (
	BillingKeyStatusActive            = "active"
	BillingKeyStatusPendingRevocation = "pending_revocation"
	BillingKeyStatusRevoked           = "revoked"
	TossBillingMaxFails               = 3
	// TossBillingOperationalGraceSeconds is the longest overdue period in which
	// a recurring charge may still be created after an outage. The bound is
	// enforced in due selection and again immediately before the provider POST.
	TossBillingOperationalGraceSeconds int64 = 24 * 60 * 60
	// Toss retains an Idempotency-Key result for 15 days from the first
	// request. Provider POST recovery must become GET-only before this lower
	// bound can no longer prove that replaying the key returns the same result.
	TossProviderIdempotencyRetentionSeconds int64 = 15 * 24 * 60 * 60
	// Automatic subscription billing shorter than one scheduler-safe hour can turn a delayed
	// scheduler or a misconfigured custom plan into a rapid sequence of card
	// charges. Short plans remain available for non-Toss/manual subscriptions,
	// but recurring card billing is deliberately bounded here.
	TossSubscriptionMinimumBillingPeriodSeconds int64 = 60 * 60

	TossTopUpAmountModeKRW   = "krw"
	TossTopUpAmountModeQuota = "quota"
)

func isValidStoredTossCustomerKey(customerKey string) bool {
	if customerKey != strings.TrimSpace(customerKey) || len(customerKey) < 2 || len(customerKey) > TossBillingIssueCustomerKeyMaxBytes {
		return false
	}
	hasSpecial := false
	for i := 0; i < len(customerKey); i++ {
		ch := customerKey[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '-', ch == '_', ch == '=', ch == '.', ch == '@':
			hasSpecial = true
		default:
			return false
		}
	}
	return hasSpecial
}

var ErrTossBillingKeyAlreadyDeleted = errors.New("toss billing key already deleted")
var ErrTossBillingChargePending = errors.New("toss billing charge still processing")
var ErrTossBillingPaymentNotFound = errors.New("toss billing payment not found")
var ErrTossBillingChargeTerminal = errors.New("toss billing charge reached a terminal status")
var ErrTossBillingChargeCanceled = errors.New("toss billing payment was canceled")
var ErrTossBillingCryptoSecretNotPersistent = errors.New("Toss billing requires a persistent CRYPTO_SECRET or SESSION_SECRET")

// ErrTossBillingKeyIdentityUnresolved means a BILLING_DELETED webhook could
// refer to an active legacy row whose provider identity cannot be resolved
// safely because its ciphertext is unreadable or its plaintext is invalid.
// Callers must not acknowledge the webhook as reconciled while this error is
// present: Toss does not provide a billing-key lookup API after deletion.
var ErrTossBillingKeyIdentityUnresolved = errors.New("Toss billing key identity is unresolved")
var ErrTossBillingMIDMismatch = errors.New("stored Toss billing key belongs to a different MID")
var ErrTossBillingClaimLost = errors.New("Toss billing order claim lost")
var ErrTossBillingIssueConflict = errors.New("Toss billing issue snapshot conflicts with the existing authorization")
var ErrTossBillingIssueSnapshotInvalid = errors.New("Toss billing issue snapshot is invalid")
var ErrTossBillingIssueOutsideRecoveryWindow = errors.New("Toss billing issue authorization is outside the recovery window")
var ErrTossBillingUserInactive = errors.New("Toss billing user is not active")
var ErrTossBillingKeyInactive = errors.New("Toss billing key is not active")
var ErrPersonalTossBillingInFlight = errors.New("a personal Toss payment is still in flight")
var ErrTossBillingOperationallyDisabled = errors.New("Toss billing is disabled")
var ErrTossBillingCrossCredentialRetryUnsafe = errors.New("Toss billing retry under a different credential is unsafe")
var ErrTossBillingRenewalOutsideGrace = errors.New("Toss billing renewal is outside the operational grace period")
var ErrTossBillingCancellationPrecedesCharge = errors.New("a durable Toss cancellation prevents another recurring charge")
var ErrTossSubscriptionBillingPeriodTooShort = errors.New("Toss subscription billing period must be at least 1 hour")
var ErrTossRenewalIdentityConflict = errors.New("Toss renewal logical attempt identity conflicts with persisted orders")
var errTossRenewalWinnerRetry = errors.New("retry Toss renewal logical-attempt winner resolution")

func IsTossBillingOperationallyEnabled() bool {
	return setting.GetTossConfigSnapshot().BillingEnabled && operation_setting.IsPaymentComplianceConfirmed()
}

// IsTossWalletAutoRechargeOperationallyEnabled is the authorization boundary
// for non-subscription automatic payments. Toss reviews this use case
// separately from recurring subscriptions, so subscription billing alone must
// never authorize a wallet policy activation or a new provider charge.
// Lookup, settlement, and key cleanup paths deliberately do not use this gate.
func IsTossWalletAutoRechargeOperationallyEnabled() bool {
	snapshot := setting.GetTossConfigSnapshot()
	return snapshot.BillingEnabled &&
		snapshot.WalletAutoRechargeEnabled &&
		operation_setting.IsPaymentComplianceConfirmed()
}

// lockTossSubscriptionOrderUserTx establishes the global lifecycle lock order:
// owner user first, then subscription/order/key rows. Membership and account
// transitions use the same owner-first order, so their commit is mutually
// exclusive with a provider-attempt marker without holding a DB transaction
// open during network I/O.
func lockTossSubscriptionOrderUserTx(tx *gorm.DB, tradeNo string) (*User, error) {
	if tx == nil || strings.TrimSpace(tradeNo) == "" {
		return nil, ErrTossBillingUserInactive
	}
	var reference SubscriptionOrder
	if err := tx.Select("user_id").Where("trade_no = ?", tradeNo).First(&reference).Error; err != nil {
		return nil, err
	}
	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "status", "organization_id").
		Where("id = ?", reference.UserId).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTossBillingUserInactive
		}
		return nil, err
	}
	return &user, nil
}

func loadOrBackfillTossRenewalContractTx(tx *gorm.DB, sub *UserSubscription) (*TossRenewalContract, error) {
	if tx == nil {
		tx = DB
	}
	if contract, err := ResolveTossRenewalContract(sub); err == nil {
		return contract, nil
	} else if !errors.Is(err, ErrTossRenewalContractUnavailable) {
		return nil, err
	}
	// An absent creation timestamp cannot prove which historical purchase
	// created this subscription. Never infer a commercial contract from the
	// mutable current plan or an unbounded set of old successful orders.
	if sub == nil || sub.CreatedAt <= 0 || sub.StartTime <= 0 || sub.EndTime <= sub.StartTime {
		return nil, ErrTossRenewalContractUnavailable
	}

	query := tx.Where(
		"user_id = ? AND plan_id = ? AND billing_key_id = ? AND payment_provider = ? AND status = ? AND plan_snapshot <> ''",
		sub.UserId, sub.PlanId, sub.BillingKeyId, PaymentProviderToss, common.TopUpStatusSuccess,
	).Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Where("renewal_order_id_version = ?", 0).
		Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL")
	query = query.Where("complete_time BETWEEN ? AND ?", sub.CreatedAt-3600, sub.CreatedAt+3600)
	var candidates []SubscriptionOrder
	if err := query.Order("complete_time desc, id desc").Limit(2).Find(&candidates).Error; err != nil {
		return nil, err
	}
	if len(candidates) != 1 {
		return nil, ErrTossRenewalContractUnavailable
	}
	plan, err := ResolveTossSubscriptionOrderPlan(&candidates[0])
	if err != nil {
		return nil, err
	}
	expectedEnd, err := calcPlanEndTime(time.Unix(sub.StartTime, 0), plan)
	if err != nil {
		return nil, err
	}
	if expectedEnd != sub.EndTime || sub.AmountTotal != plan.TotalAmount ||
		strings.TrimSpace(sub.UpgradeGroup) != strings.TrimSpace(plan.UpgradeGroup) {
		return nil, ErrTossRenewalContractUnavailable
	}
	canceled, err := hasAuthoritativeTossCancellationForOrderTx(tx, candidates[0].TradeNo)
	if err != nil {
		return nil, err
	}
	if canceled {
		return nil, ErrTossRenewalContractUnavailable
	}
	if err := SetTossRenewalContractFromInitialOrder(sub, &candidates[0]); err != nil {
		return nil, err
	}
	updated := tx.Model(&UserSubscription{}).
		Where("id = ? AND (toss_renewal_contract_snapshot = '' OR toss_renewal_contract_snapshot IS NULL)", sub.Id).
		Update("toss_renewal_contract_snapshot", sub.TossRenewalContractSnapshot)
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected == 0 {
		var current UserSubscription
		if err := tx.Select("id", "plan_id", "toss_renewal_contract_snapshot").Where("id = ?", sub.Id).First(&current).Error; err != nil {
			return nil, err
		}
		sub.TossRenewalContractSnapshot = current.TossRenewalContractSnapshot
	}
	return ResolveTossRenewalContract(sub)
}

func disableTossRenewalWithoutContractTx(tx *gorm.DB, sub *UserSubscription) error {
	if tx == nil || sub == nil || sub.Id <= 0 {
		return nil
	}
	now := getDBTimestampTx(tx)
	result := tx.Model(&UserSubscription{}).
		Where("id = ? AND auto_renew = ?", sub.Id, true).
		Updates(map[string]interface{}{
			"auto_renew":         false,
			"next_billing_time":  0,
			"billing_retry_time": 0,
			"updated_at":         now,
		})
	if result.Error != nil {
		return result.Error
	}
	sub.AutoRenew = false
	sub.NextBillingTime = 0
	sub.BillingRetryTime = 0
	return nil
}

const tossLegacyRenewalContractBackfillBatchSize = 200

var ErrTossRenewalContractMigrationDeferred = errors.New("legacy Toss renewal contract migration is deferred until configuration repair")

func HasPendingLegacyTossRenewalContracts() (bool, error) {
	if DB == nil || !DB.Migrator().HasTable(&UserSubscription{}) {
		return false, nil
	}
	return hasPendingLegacyTossRenewalContractsDB(DB)
}

func hasPendingLegacyTossRenewalContractsDB(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, errors.New("database is unavailable")
	}
	var candidate struct {
		Id int
	}
	err := db.Model(&UserSubscription{}).
		Select("id").
		Where("status = ? AND auto_renew = ? AND billing_key_id > 0", "active", true).
		Where("toss_renewal_contract_snapshot IS NULL OR toss_renewal_contract_snapshot = ?", "").
		Order("id ASC").
		Take(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

// BackfillLegacyTossRenewalContractsAtCurrentTerms is the one-time rolling
// upgrade bridge for subscriptions created before renewal contracts were
// snapshotted. The legacy scheduler priced the next charge from the then-current
// plan and Toss unit price, so startup freezes exactly those effective terms
// after database options have been loaded. Subsequent plan/config edits cannot
// alter the contract.
//
// Rows whose current terms cannot safely produce a recurring card charge are
// disabled instead of being left for the scheduler to repeatedly guess at a
// commercial contract. The operation is idempotent and uses a compare-and-set
// write so multiple rolling-upgrade processes cannot overwrite a snapshot.
func BackfillLegacyTossRenewalContractsAtCurrentTerms() (backfilled int64, disabled int64, err error) {
	if DB == nil || !DB.Migrator().HasTable(&UserSubscription{}) ||
		!DB.Migrator().HasTable(&SubscriptionPlan{}) || !DB.Migrator().HasTable(&UserBillingKey{}) {
		return 0, 0, nil
	}
	pending, err := HasPendingLegacyTossRenewalContracts()
	if err != nil || !pending {
		return 0, 0, err
	}

	configSnapshot, err := getFreshTossConfigSnapshot(true)
	if err != nil {
		// An unbound/quarantined legacy option generation is deliberately not
		// trusted to price a recurring contract. Defer the migration while keeping
		// the process alive and all Toss provider POSTs fail-closed; an explicit
		// all-disabled repair can later supply an attested price/key snapshot.
		if errors.Is(err, ErrTossConfigRevisionStale) {
			state, stateErr := GetTossConfigState()
			if stateErr != nil {
				return 0, 0, stateErr
			}
			if state.RepairRequired {
				return 0, 0, errors.Join(ErrTossRenewalContractMigrationDeferred, err)
			}
		}
		return 0, 0, err
	}
	expectedRevision := strings.TrimSpace(configSnapshot.Revision)
	if err := ensureTossConfigurationMaintenanceGate(expectedRevision); err != nil {
		return 0, 0, err
	}
	backfilled, disabled, err = backfillLegacyTossRenewalContractsPinned(configSnapshot, expectedRevision)
	if err != nil {
		return backfilled, disabled, err
	}
	if err := clearTossConfigurationMaintenanceGate(expectedRevision); err != nil {
		return backfilled, disabled, err
	}
	return backfilled, disabled, nil
}

func backfillLegacyTossRenewalContractsPinned(configSnapshot setting.TossConfigSnapshot, expectedRevision string) (backfilled int64, disabled int64, err error) {
	expectedRevision = strings.TrimSpace(expectedRevision)
	if expectedRevision == "" {
		return 0, 0, ErrTossConfigRevisionStale
	}
	unitPrice := configSnapshot.UnitPrice
	if unitPrice <= 0 || math.IsNaN(unitPrice) || math.IsInf(unitPrice, 0) ||
		unitPrice > float64(setting.TossMaximumChargeAmountKRW) {
		return 0, 0, fmt.Errorf("invalid Toss unit price for legacy renewal contract migration")
	}

	// TossUnitPrice is not secret-encrypted. If a persisted value exists, it
	// must match the already-loaded atomic runtime snapshot; otherwise startup
	// may have failed part-way through option loading and freezing terms would
	// record a stale/default price.
	if DB.Migrator().HasTable(&Option{}) {
		var option Option
		optionErr := DB.Select("value").Where(commonKeyCol+" = ?", "TossUnitPrice").First(&option).Error
		if optionErr == nil {
			persistedUnitPrice, parseErr := strconv.ParseFloat(strings.TrimSpace(option.Value), 64)
			if parseErr != nil || !subscriptionPricesEqual(persistedUnitPrice, unitPrice) {
				return 0, 0, fmt.Errorf("%w: TossUnitPrice is not the loaded database value", ErrTossConfigRevisionStale)
			}
		} else if !errors.Is(optionErr, gorm.ErrRecordNotFound) {
			return 0, 0, optionErr
		}
	}
	cutoffID := 0
	if err := DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentRevision) != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		var gate Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).First(&gate).Error; err != nil {
			return err
		}
		if strings.TrimSpace(gate.Value) != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		return tx.Model(&UserSubscription{}).Select("COALESCE(MAX(id), 0)").Scan(&cutoffID).Error
	}); err != nil {
		return 0, 0, err
	}
	if cutoffID <= 0 {
		return 0, 0, nil
	}

	lastID := 0
	for {
		var candidates []struct {
			Id int
		}
		query := DB.Model(&UserSubscription{}).
			Select("id").
			Where("id > ? AND id <= ?", lastID, cutoffID).
			Where("status = ? AND auto_renew = ? AND billing_key_id > 0", "active", true).
			Where("toss_renewal_contract_snapshot IS NULL OR toss_renewal_contract_snapshot = ?", "").
			Order("id ASC").
			Limit(tossLegacyRenewalContractBackfillBatchSize)
		if err := query.Find(&candidates).Error; err != nil {
			return backfilled, disabled, err
		}
		if len(candidates) == 0 {
			return backfilled, disabled, nil
		}

		for _, candidate := range candidates {
			lastID = candidate.Id
			rowBackfilled, rowDisabled, rowErr := backfillLegacyTossRenewalContractAtCurrentTerms(candidate.Id, unitPrice, expectedRevision)
			if rowErr != nil {
				return backfilled, disabled, rowErr
			}
			if rowBackfilled {
				backfilled++
			}
			if rowDisabled {
				disabled++
			}
		}
	}
}

func backfillLegacyTossRenewalContractAtCurrentTerms(subID int, unitPrice float64, expectedRevision string) (backfilled bool, disabled bool, err error) {
	if subID <= 0 {
		return false, false, nil
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentRevision) != strings.TrimSpace(expectedRevision) {
			return ErrTossConfigRevisionStale
		}
		var gate Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).First(&gate).Error; err != nil {
			return err
		}
		if strings.TrimSpace(gate.Value) != strings.TrimSpace(expectedRevision) {
			return ErrTossConfigRevisionStale
		}
		var reference UserSubscription
		if err := tx.Select("id", "user_id").Where("id = ?", subID).First(&reference).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		var owner User
		ownerErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "organization_id").
			Where("id = ?", reference.UserId).First(&owner).Error
		if ownerErr != nil && !errors.Is(ownerErr, gorm.ErrRecordNotFound) {
			return ownerErr
		}

		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subID).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if sub.Status != "active" || !sub.AutoRenew || sub.BillingKeyId <= 0 ||
			strings.TrimSpace(sub.TossRenewalContractSnapshot) != "" {
			return nil
		}

		disable := func() error {
			if err := disableTossRenewalWithoutContractTx(tx, &sub); err != nil {
				return err
			}
			disabled = true
			return nil
		}

		if ownerErr != nil || owner.Id != sub.UserId || owner.Status != common.UserStatusEnabled || owner.OrganizationId > 0 {
			return disable()
		}

		var billingKey UserBillingKey
		keyErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "user_id", "status").
			Where("id = ?", sub.BillingKeyId).First(&billingKey).Error
		if errors.Is(keyErr, gorm.ErrRecordNotFound) {
			return disable()
		}
		if keyErr != nil {
			return keyErr
		}
		if billingKey.UserId != sub.UserId || billingKey.Status != BillingKeyStatusActive {
			return disable()
		}

		plan, planErr := getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if errors.Is(planErr, gorm.ErrRecordNotFound) {
			return disable()
		}
		if planErr != nil {
			return planErr
		}
		providerAmount := TossPlanKRWWithUnitPrice(plan.PriceAmount, unitPrice)
		if providerAmount <= 0 || !IsTossCardAmountPayableKRW(providerAmount) ||
			ValidateTossSubscriptionBillingPlan(plan) != nil {
			return disable()
		}

		legacyOrder := &SubscriptionOrder{
			UserId:           sub.UserId,
			PlanId:           plan.Id,
			Money:            plan.PriceAmount,
			TradeNo:          fmt.Sprintf("toss_legacy_contract_%d", sub.Id),
			PaymentMethod:    PaymentMethodToss,
			PaymentProvider:  PaymentProviderToss,
			Status:           common.TopUpStatusSuccess,
			ProviderAmount:   providerAmount,
			ProviderCurrency: "KRW",
			BillingKeyId:     sub.BillingKeyId,
		}
		if err := SetTossSubscriptionOrderPlanSnapshot(legacyOrder, plan); err != nil {
			return disable()
		}
		if err := SetTossRenewalContractFromInitialOrder(&sub, legacyOrder); err != nil {
			return disable()
		}

		updated := tx.Model(&UserSubscription{}).
			Where("id = ? AND user_id = ? AND plan_id = ? AND billing_key_id = ?", sub.Id, sub.UserId, sub.PlanId, sub.BillingKeyId).
			Where("status = ? AND auto_renew = ?", "active", true).
			Where("toss_renewal_contract_snapshot IS NULL OR toss_renewal_contract_snapshot = ?", "").
			Updates(map[string]interface{}{
				"toss_renewal_contract_snapshot": sub.TossRenewalContractSnapshot,
				"updated_at":                     getDBTimestampTx(tx),
			})
		if updated.Error != nil {
			return updated.Error
		}
		backfilled = updated.RowsAffected == 1
		return nil
	})
	return backfilled, disabled, err
}

func disableAndExpireInvalidTossRenewalOrderTx(tx *gorm.DB, sub *UserSubscription, order *SubscriptionOrder) error {
	if tx == nil || sub == nil || order == nil || order.Id <= 0 {
		return ErrSubscriptionOrderStatusInvalid
	}
	if err := disableTossRenewalWithoutContractTx(tx, sub); err != nil {
		return err
	}
	result := tx.Model(&SubscriptionOrder{}).
		Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
		Updates(map[string]interface{}{
			"status":              common.TopUpStatusExpired,
			"complete_time":       getDBTimestampTx(tx),
			"billing_claim_token": "",
			"billing_claim_time":  0,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTossBillingClaimLost
	}
	return nil
}

func isFatalTossRenewalContractError(err error) bool {
	return errors.Is(err, ErrTossRenewalContractUnavailable) ||
		errors.Is(err, ErrSubscriptionPlanSnapshotInvalid) ||
		errors.Is(err, ErrSubscriptionPlanSnapshotMismatch)
}

func getTossRenewalContractForSubscription(subID int) (*TossRenewalContract, error) {
	var contract *TossRenewalContract
	unavailable := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var reference UserSubscription
		if err := tx.Select("user_id").Where("id = ?", subID).First(&reference).Error; err != nil {
			return err
		}
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", reference.UserId).First(&user).Error; err != nil {
			return err
		}
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subID).First(&sub).Error; err != nil {
			return err
		}
		resolved, err := loadOrBackfillTossRenewalContractTx(tx, &sub)
		if isFatalTossRenewalContractError(err) {
			if disableErr := disableTossRenewalWithoutContractTx(tx, &sub); disableErr != nil {
				return disableErr
			}
			unavailable = true
			return nil
		}
		if err != nil {
			return err
		}
		contract = resolved
		return nil
	})
	if err != nil {
		return nil, err
	}
	if unavailable {
		return nil, ErrTossRenewalContractUnavailable
	}
	return contract, nil
}

// ValidateTossSubscriptionBillingPlan applies payment-specific cadence limits
// without restricting short plans paid through balance or other providers.
func ValidateTossSubscriptionBillingPlan(plan *SubscriptionPlan) error {
	if plan == nil {
		return errors.New("Toss subscription plan is unavailable")
	}
	base := time.Unix(1_700_000_000, 0).UTC()
	end, err := calcPlanEndTime(base, plan)
	if err != nil {
		return err
	}
	if end-base.Unix() < TossSubscriptionMinimumBillingPeriodSeconds {
		return ErrTossSubscriptionBillingPeriodTooShort
	}
	return nil
}

const TossRenewalTradeNoPrefix = "toss_sub_renew_"

const (
	tossRecurringOrderIDVersionLegacy = 1
	// Opaque rows are resolved exclusively through their immutable association;
	// the provider-facing ID intentionally carries no subscription identity.
	tossRecurringOrderIDVersionOpaque = 2
)

const (
	tossBillingChargeProtocolLegacy         = 0
	tossBillingChargeProtocolDurableAttempt = 1
)

// IsPotentialLegacyTossSubscriptionCharge reports the one rolling-upgrade
// shape that must be treated as already attempted even though the additive
// BillingAttempted column still reads false. Old binaries persisted the exact
// provider credential before POST, so v0 rows with that snapshot must enter
// exact-credential GET-before-POST recovery. A v0 row without the snapshot is
// ambiguous and must fail closed instead of borrowing the currently active
// credential.
func IsPotentialLegacyTossSubscriptionCharge(order *SubscriptionOrder) bool {
	return order != nil &&
		order.PaymentProvider == PaymentProviderToss &&
		order.Status == common.TopUpStatusPending &&
		order.BillingKeyId > 0 &&
		!order.BillingAttempted &&
		order.BillingChargeProtocolVersion == tossBillingChargeProtocolLegacy &&
		strings.TrimSpace(order.ProviderCredential) != ""
}

func tossRenewalTradeNoLikePattern() string {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(TossRenewalTradeNoPrefix)
	return escaped + "%"
}

const tossSubscriptionBillingClaimTTLSeconds int64 = 5 * 60
const tossBillingRevocationRetryDelaySeconds int64 = 2 * 60

const (
	// Toss documents authKey as at most 300 characters. It is an opaque ASCII
	// token, so the byte limit is equivalent and also bounds encrypted storage.
	TossBillingIssueAuthKeyMaxBytes = 300
	// Billing.billingKey is documented as at most 200 characters.
	TossBillingKeyMaxRunes = 200
	// This project persists its canonical customer key in a varchar(64).
	TossBillingIssueCustomerKeyMaxBytes = 64
)

// IsValidTossBillingKey applies the provider token constraints before a key is
// persisted, hashed, or treated as a resolved legacy identity. A decryptable
// plaintext that fails these checks is corrupt state, not a verified non-match.
func IsValidTossBillingKey(billingKey string) bool {
	if billingKey != strings.TrimSpace(billingKey) {
		return false
	}
	length := 0
	for _, r := range billingKey {
		length++
		if length > TossBillingKeyMaxRunes || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return length > 0
}

func tossBillingKeyHash(billingKey string) string {
	return common.GenerateHMAC(strings.TrimSpace(billingKey))
}

// getTossBillingKeyIdentityRowsTx resolves every local row that represents the
// same provider billing key. Older deployments could create duplicate rows, so
// provider DELETE and last-reference decisions must use the provider identity
// rather than one surrogate database id.
func getTossBillingKeyIdentityRowsTx(tx *gorm.DB, billingKeyID int, lock bool) ([]UserBillingKey, error) {
	if billingKeyID <= 0 {
		return nil, nil
	}
	db := tx
	if db == nil {
		db = DB
	}
	var selected UserBillingKey
	if err := db.Where("id = ?", billingKeyID).First(&selected).Error; err != nil {
		return nil, err
	}
	identityHash := strings.TrimSpace(selected.BillingKeyHash)
	selectedPlain := ""
	if strings.TrimSpace(selected.EncryptedKey) != "" {
		if plain, err := common.DecryptString(selected.EncryptedKey); err == nil {
			selectedPlain = plain
			if identityHash == "" {
				identityHash = tossBillingKeyHash(plain)
			}
		}
	}
	query := db.Where("user_id = ? AND customer_key = ?", selected.UserId, selected.CustomerKey)
	if identityHash != "" {
		query = query.Where("id = ? OR billing_key_hash = ? OR billing_key_hash = '' OR billing_key_hash IS NULL", selected.Id, identityHash)
	} else {
		query = query.Where("id = ? OR billing_key_hash = '' OR billing_key_hash IS NULL", selected.Id)
	}
	if lock && tx != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var candidates []UserBillingKey
	if err := query.Order("id asc").Find(&candidates).Error; err != nil {
		return nil, err
	}
	rows := make([]UserBillingKey, 0, len(candidates))
	selectedMID := selected.ProviderClientKeyHash
	for i := range candidates {
		candidate := candidates[i]
		candidateMID := candidate.ProviderClientKeyHash
		if IsValidTossClientKeyFingerprint(selectedMID) && IsValidTossClientKeyFingerprint(candidateMID) && selectedMID != candidateMID {
			continue
		}
		matched := candidate.Id == selected.Id ||
			(identityHash != "" && strings.EqualFold(strings.TrimSpace(candidate.BillingKeyHash), identityHash))
		if !matched && selectedPlain != "" && strings.TrimSpace(candidate.EncryptedKey) != "" {
			plain, err := common.DecryptString(candidate.EncryptedKey)
			matched = err == nil && plain == selectedPlain
		}
		if matched {
			rows = append(rows, candidate)
		}
	}
	return rows, nil
}

func tossBillingKeyIdentityIDs(rows []UserBillingKey) []int {
	ids := make([]int, 0, len(rows))
	for i := range rows {
		ids = appendPositiveUniqueInt(ids, rows[i].Id)
	}
	return ids
}

func tossBillingClientKeyHash(clientKey string) string {
	return TossClientKeyFingerprint(clientKey)
}

// TossBillingClientKeyFingerprint returns the non-secret MID fingerprint that
// can be snapshotted alongside an encrypted provider credential.
func TossBillingClientKeyFingerprint(clientKey string) string {
	return tossBillingClientKeyHash(clientKey)
}

func tossActiveBillingClientKey(snapshot setting.TossConfigSnapshot) string {
	if snapshot.TestMode {
		if strings.TrimSpace(snapshot.BillingTestClientKey) != "" && strings.TrimSpace(snapshot.BillingTestSecretKey) != "" {
			return snapshot.BillingTestClientKey
		}
		return snapshot.TestClientKey
	}
	if strings.TrimSpace(snapshot.BillingClientKey) != "" && strings.TrimSpace(snapshot.BillingSecretKey) != "" {
		return snapshot.BillingClientKey
	}
	return snapshot.ClientKey
}

const tossBillingCryptoSecretMinBytes = 32

func validateTossBillingCryptoSecret(envName, secret string) error {
	if secret == "random_string" {
		return fmt.Errorf("%w: %s uses the default placeholder", ErrTossBillingCryptoSecretNotPersistent, envName)
	}
	// AES-256 key derivation cannot add entropy to a weak operator secret. Toss
	// credentials are long-lived, high-value ciphertext, so reject short keys at
	// this payment-specific gate even though common.CryptoSecret remains backward
	// compatible for unrelated application features. len(string) is deliberately
	// a byte count; environment secrets are trimmed before this check.
	if len(secret) < tossBillingCryptoSecretMinBytes {
		return fmt.Errorf("%w: %s must be at least %d bytes", ErrTossBillingCryptoSecretNotPersistent, envName, tossBillingCryptoSecretMinBytes)
	}
	if secret != common.CryptoSecret {
		return fmt.Errorf("%w: %s does not match the active encryption key", ErrTossBillingCryptoSecretNotPersistent, envName)
	}
	return nil
}

// ValidateTossBillingCryptoConfiguration verifies that encrypted billing
// credentials will remain decryptable after a restart. common.CryptoSecret is
// random when neither environment variable is configured, which is suitable
// for ephemeral data but not for long-lived billing credentials.
func ValidateTossBillingCryptoConfiguration() error {
	if cryptoSecret := strings.TrimSpace(os.Getenv("CRYPTO_SECRET")); cryptoSecret != "" {
		return validateTossBillingCryptoSecret("CRYPTO_SECRET", cryptoSecret)
	}
	if sessionSecret := strings.TrimSpace(os.Getenv("SESSION_SECRET")); sessionSecret != "" {
		return validateTossBillingCryptoSecret("SESSION_SECRET", sessionSecret)
	}
	return ErrTossBillingCryptoSecretNotPersistent
}

func IsTossBillingCryptoConfigurationSafe() bool {
	return ValidateTossBillingCryptoConfiguration() == nil
}

func EncryptProviderCredential(secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", nil
	}
	// Provider credentials are persisted for GET-before-POST recovery across
	// restarts. This applies to one-time top-ups as well as recurring billing;
	// encrypting with common.CryptoSecret's random process default would make an
	// ambiguous paid order permanently unverifiable after the next restart.
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return "", err
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

// preserveExactTossAttemptCredential enforces the API-key half of Toss's
// idempotency namespace. The ordinary POST gate may create the snapshot only
// for a provably unattempted row. Once a ciphertext exists it is compared by
// plaintext and preserved byte-for-byte; moving to another secret is reserved
// for the explicit provider-credential-rejection promotion CAS.
func preserveExactTossAttemptCredential(storedCredential, expectedSecret string, attempted bool) (bool, error) {
	storedCredential = strings.TrimSpace(storedCredential)
	expectedSecret = strings.TrimSpace(expectedSecret)
	if expectedSecret == "" {
		return false, ErrTossBillingCrossCredentialRetryUnsafe
	}
	if storedCredential == "" {
		if attempted {
			return false, ErrTossBillingCrossCredentialRetryUnsafe
		}
		return false, nil
	}
	storedSecret, err := DecryptProviderCredential(storedCredential)
	if err != nil || strings.TrimSpace(storedSecret) == "" || storedSecret != expectedSecret {
		return false, ErrTossBillingCrossCredentialRetryUnsafe
	}
	return true, nil
}

// TossPlanKRW converts a plan's USD-equivalent price to KRW integer for Toss billing.
// This is the single authoritative KRW conversion source used by both the
// subscription controller and the recurring billing cron.
func TossPlanKRW(priceAmount float64) int64 {
	return TossPlanKRWWithUnitPrice(priceAmount, setting.GetTossConfigSnapshot().UnitPrice)
}

func TossPlanKRWWithUnitPrice(priceAmount, unit float64) int64 {
	if priceAmount <= 0 || math.IsNaN(priceAmount) || math.IsInf(priceAmount, 0) ||
		unit <= 0 || math.IsNaN(unit) || math.IsInf(unit, 0) {
		return 0
	}
	charge := decimal.NewFromFloat(priceAmount).Mul(decimal.NewFromFloat(unit)).Round(0)
	if charge.LessThan(decimal.NewFromInt(1)) || charge.GreaterThan(decimal.NewFromInt(setting.TossMaximumChargeAmountKRW)) {
		return 0
	}
	return charge.IntPart()
}

func IsTossCardAmountPayableKRW(amount int64) bool {
	return amount >= setting.TossCardMinimumAmountKRW && amount <= setting.TossMaximumChargeAmountKRW
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

func parseTossRenewalTradeNoWithAttempt(tradeNo string) (subId int, nextBillingTime int64, attempt int, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(tradeNo), TossRenewalTradeNoPrefix)
	if !found {
		return 0, 0, 0, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, 0, false
	}
	parsedSubId, err := strconv.Atoi(parts[0])
	if err != nil || parsedSubId <= 0 {
		return 0, 0, 0, false
	}
	parsedNextBillingTime, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || parsedNextBillingTime <= 0 {
		return 0, 0, 0, false
	}
	parsedAttempt := 0
	if len(parts) == 3 {
		parsedAttempt, err = strconv.Atoi(parts[2])
		if err != nil || parsedAttempt <= 0 {
			return 0, 0, 0, false
		}
	}
	return parsedSubId, parsedNextBillingTime, parsedAttempt, true
}

func ParseTossRenewalTradeNo(tradeNo string) (subId int, nextBillingTime int64, ok bool) {
	subId, nextBillingTime, _, ok = parseTossRenewalTradeNoWithAttempt(tradeNo)
	return subId, nextBillingTime, ok
}

func tossRenewalTradeNo(subID int, nextBillingTime int64, attempt int) string {
	base := fmt.Sprintf("%s%d_%d", TossRenewalTradeNoPrefix, subID, nextBillingTime)
	if attempt <= 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, attempt)
}

type tossRenewalAttemptIdentity struct {
	SubscriptionID int
	BillingTime    int64
	Attempt        int
}

func (identity tossRenewalAttemptIdentity) valid() bool {
	return identity.SubscriptionID > 0 && identity.BillingTime > 0 && identity.Attempt >= 0
}

func sameTossRenewalAttemptIdentity(left, right tossRenewalAttemptIdentity) bool {
	return left == right
}

// subscriptionOrderRenewalIdentity treats complete association metadata as
// authoritative for opaque v2 rows and validates legacy rows against their
// structured orderId. Partial metadata or a tuple/string mismatch is never
// guessed through because it could join two different provider charges.
func subscriptionOrderRenewalIdentity(order *SubscriptionOrder) (tossRenewalAttemptIdentity, bool, error) {
	if order == nil {
		return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
	}
	associationCount := 0
	if order.RenewalSubscriptionId != nil {
		associationCount++
	}
	if order.RenewalBillingTime != nil {
		associationCount++
	}
	if order.RenewalAttempt != nil {
		associationCount++
	}
	if associationCount == 0 {
		if HasTossRenewalOpaqueOrderIDPrefix(order.TradeNo) {
			return tossRenewalAttemptIdentity{}, false, ErrTossRecurringOrderIDEvidenceCorrupt
		}
		// An opaque provider orderId carries no recoverable logical identity in
		// the string itself. Version-2 rows therefore require the complete
		// immutable association tuple; treating a malformed v2 row whose text
		// happens to resemble a legacy ID as legacy could attach it to the wrong
		// provider attempt and authorize an unsafe retry.
		if order.RenewalOrderIdVersion != 0 {
			return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
		}
		subID, billingTime, attempt, ok := parseTossRenewalTradeNoWithAttempt(order.TradeNo)
		identity := tossRenewalAttemptIdentity{SubscriptionID: subID, BillingTime: billingTime, Attempt: attempt}
		if !ok || !identity.valid() {
			return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
		}
		return identity, false, nil
	}
	if associationCount != 3 {
		return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
	}
	identity := tossRenewalAttemptIdentity{
		SubscriptionID: *order.RenewalSubscriptionId,
		BillingTime:    *order.RenewalBillingTime,
		Attempt:        *order.RenewalAttempt,
	}
	if !identity.valid() {
		return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
	}
	switch order.RenewalOrderIdVersion {
	case 0, tossRecurringOrderIDVersionLegacy:
		parsedSubID, parsedBillingTime, parsedAttempt, ok := parseTossRenewalTradeNoWithAttempt(order.TradeNo)
		parsed := tossRenewalAttemptIdentity{SubscriptionID: parsedSubID, BillingTime: parsedBillingTime, Attempt: parsedAttempt}
		if !ok || !sameTossRenewalAttemptIdentity(identity, parsed) {
			return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
		}
	case tossRecurringOrderIDVersionOpaque:
		if !validTossRenewalOpaqueOrderID(order.TradeNo) {
			return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
		}
	default:
		return tossRenewalAttemptIdentity{}, false, ErrTossRenewalIdentityConflict
	}
	return identity, true, nil
}

// ResolveTossRenewalOrderIdentity is the metadata-first classifier shared by
// settlement, cancellation, recovery, and webhook paths. Non-renewal purchase
// orders return isRenewal=false; partial or contradictory association metadata
// returns a typed conflict and must stop provider writes.
func ResolveTossRenewalOrderIdentity(order *SubscriptionOrder) (subID int, billingTime int64, attempt int, isRenewal bool, err error) {
	if order == nil {
		return 0, 0, 0, false, nil
	}
	associationCount := 0
	if order.RenewalSubscriptionId != nil {
		associationCount++
	}
	if order.RenewalBillingTime != nil {
		associationCount++
	}
	if order.RenewalAttempt != nil {
		associationCount++
	}
	if associationCount == 0 {
		if HasTossRenewalOpaqueOrderIDPrefix(order.TradeNo) {
			return 0, 0, 0, false, ErrTossRecurringOrderIDEvidenceCorrupt
		}
		if order.RenewalOrderIdVersion != 0 {
			return 0, 0, 0, false, ErrTossRenewalIdentityConflict
		}
		parsedSubID, parsedBillingTime, parsedAttempt, ok := parseTossRenewalTradeNoWithAttempt(order.TradeNo)
		if !ok {
			return 0, 0, 0, false, nil
		}
		if order.PaymentProvider != PaymentProviderToss {
			return 0, 0, 0, false, ErrPaymentMethodMismatch
		}
		return parsedSubID, parsedBillingTime, parsedAttempt, true, nil
	}
	identity, _, identityErr := subscriptionOrderRenewalIdentity(order)
	if identityErr != nil {
		return 0, 0, 0, false, identityErr
	}
	if order.PaymentProvider != PaymentProviderToss {
		return 0, 0, 0, false, ErrPaymentMethodMismatch
	}
	return identity.SubscriptionID, identity.BillingTime, identity.Attempt, true, nil
}

// ensureNoIncompleteTossRenewalOrderIdentityTx is a global corruption fence
// for recurring subscription writes. A nonzero protocol version asserts that
// the immutable association tuple was written atomically with the provider
// order ID. If any such row is incomplete, its opaque order may be invisible
// to a subscription-scoped lookup; allowing another recurring POST anywhere
// would risk creating a second physical charge for that hidden attempt.
func ensureNoIncompleteTossRenewalOrderIdentityTx(tx *gorm.DB) error {
	if tx == nil {
		tx = DB
	}
	if tx == nil {
		return ErrTossRenewalIdentityConflict
	}
	var rows []SubscriptionOrder
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("renewal_subscription_id IS NULL OR renewal_billing_time IS NULL OR renewal_attempt IS NULL").
		Where("trade_no LIKE ? ESCAPE '!' OR (payment_provider = ? AND (renewal_order_id_version <> ? OR renewal_subscription_id IS NOT NULL OR renewal_billing_time IS NOT NULL OR renewal_attempt IS NOT NULL))",
			tossRenewalOpaqueOrderIDLikePattern(), PaymentProviderToss, 0).
		Order("id asc").Limit(1).Find(&rows).Error
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return ErrTossRecurringOrderIDEvidenceCorrupt
}

func ResolveTossRenewalOrderIdentityByTradeNoWithContext(ctx context.Context, tradeNo string) (subID int, billingTime int64, attempt int, isRenewal bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return 0, 0, 0, false, nil
	}
	var order SubscriptionOrder
	err = dbWithContext(ctx).Where("trade_no = ?", tradeNo).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		parsedSubID, parsedBillingTime, parsedAttempt, ok := parseTossRenewalTradeNoWithAttempt(tradeNo)
		return parsedSubID, parsedBillingTime, parsedAttempt, ok, nil
	}
	if err != nil {
		return 0, 0, 0, false, err
	}
	return ResolveTossRenewalOrderIdentity(&order)
}

func ResolveTossRenewalOrderIdentityByTradeNo(tradeNo string) (subID int, billingTime int64, attempt int, isRenewal bool, err error) {
	return ResolveTossRenewalOrderIdentityByTradeNoWithContext(context.Background(), tradeNo)
}

func validateTossRenewalOrderIdentity(order *SubscriptionOrder, expected tossRenewalAttemptIdentity) error {
	identity, _, err := subscriptionOrderRenewalIdentity(order)
	if err != nil || !sameTossRenewalAttemptIdentity(identity, expected) {
		return ErrTossRenewalIdentityConflict
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	return nil
}

// findTossRenewalOrderForIdentityTx dual-reads the immutable association and
// the Phase-A legacy orderId. An old all-NULL row is backfilled only after its
// structured ID proves the exact tuple. Two physical rows for one logical
// attempt fail closed instead of selecting an arbitrary provider charge.
func findTossRenewalOrderForIdentityTx(tx *gorm.DB, identity tossRenewalAttemptIdentity) (*SubscriptionOrder, error) {
	if tx == nil {
		tx = DB
	}
	if !identity.valid() {
		return nil, ErrTossRenewalIdentityConflict
	}
	var associated SubscriptionOrder
	associatedErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"renewal_subscription_id = ? AND renewal_billing_time = ? AND renewal_attempt = ?",
		identity.SubscriptionID, identity.BillingTime, identity.Attempt,
	).First(&associated).Error
	if associatedErr != nil && !errors.Is(associatedErr, gorm.ErrRecordNotFound) {
		return nil, associatedErr
	}

	legacyTradeNo := tossRenewalTradeNo(identity.SubscriptionID, identity.BillingTime, identity.Attempt)
	var legacy SubscriptionOrder
	legacyErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", legacyTradeNo).First(&legacy).Error
	if legacyErr != nil && !errors.Is(legacyErr, gorm.ErrRecordNotFound) {
		return nil, legacyErr
	}
	if associatedErr == nil && legacyErr == nil && associated.Id != legacy.Id {
		return nil, ErrTossRenewalIdentityConflict
	}
	if associatedErr == nil {
		if err := validateTossRenewalOrderIdentity(&associated, identity); err != nil {
			return nil, err
		}
		return &associated, nil
	}
	if legacyErr != nil {
		return nil, nil
	}
	legacyIdentity, associatedMetadata, err := subscriptionOrderRenewalIdentity(&legacy)
	if err != nil || !sameTossRenewalAttemptIdentity(legacyIdentity, identity) || legacy.PaymentProvider != PaymentProviderToss {
		return nil, ErrTossRenewalIdentityConflict
	}
	if associatedMetadata {
		// A concurrent Phase-A worker can backfill this exact legacy row after
		// our association lookup missed but before the row lock is acquired.
		// Reusing that same validated physical winner is safe and avoids falsely
		// disabling an otherwise healthy subscription.
		return &legacy, nil
	}
	result := tx.Model(&SubscriptionOrder{}).
		Where("id = ? AND renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL", legacy.Id).
		Updates(map[string]interface{}{
			"renewal_subscription_id":  identity.SubscriptionID,
			"renewal_billing_time":     identity.BillingTime,
			"renewal_attempt":          identity.Attempt,
			"renewal_order_id_version": tossRecurringOrderIDVersionLegacy,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrTossRenewalIdentityConflict
	}
	legacy.RenewalSubscriptionId = &identity.SubscriptionID
	legacy.RenewalBillingTime = &identity.BillingTime
	legacy.RenewalAttempt = &identity.Attempt
	legacy.RenewalOrderIdVersion = tossRecurringOrderIDVersionLegacy
	return &legacy, nil
}

func findTossRenewalOrderForIdentity(identity tossRenewalAttemptIdentity) (*SubscriptionOrder, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var order *SubscriptionOrder
		err := DB.Transaction(func(tx *gorm.DB) error {
			var err error
			order, err = findTossRenewalOrderForIdentityTx(tx, identity)
			return err
		})
		if err == nil || !isTossRenewalAssociationUniqueConflict(err) {
			return order, err
		}
		if attempt > 0 {
			return nil, ErrTossRenewalIdentityConflict
		}
		// PostgreSQL aborts the transaction after a unique violation, so classify
		// the now-committed winner in a fresh transaction rather than querying the
		// failed one. A true dual-row conflict becomes the typed error on retry.
	}
	return nil, ErrTossRenewalIdentityConflict
}

// resolveTossRenewalEffectiveAttemptTx derives the next provider order suffix
// from durable terminal rows, never from BillingFailCount alone. The deployed
// worker always uses attempt zero. Therefore a new suffix is safe during Phase
// A only after the base physical order is already failed: an old worker will
// find that terminal base row and stop before issuing its own provider POST.
// Gaps, duplicate tuples, or a higher attempt beside a live lower attempt are
// corruption/partial-rollout evidence and fail closed.
func resolveTossRenewalEffectiveAttemptTx(tx *gorm.DB, subID int, billingTime int64, billingFailCount int) (int, error) {
	if tx == nil {
		tx = DB
	}
	if subID <= 0 || billingTime <= 0 || billingFailCount < 0 {
		return 0, ErrTossRenewalIdentityConflict
	}
	if err := ensureNoIncompleteTossRenewalOrderIdentityTx(tx); err != nil {
		return 0, err
	}
	base := tossRenewalTradeNo(subID, billingTime, 0)
	escapedBase := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(base)
	var rows []SubscriptionOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("(renewal_subscription_id = ? AND renewal_billing_time = ?) OR trade_no = ? OR trade_no LIKE ? ESCAPE '!'",
			subID, billingTime, base, escapedBase+"!_%").
		Order("id asc").Limit(102).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) > 100 {
		return 0, ErrTossRenewalIdentityConflict
	}
	byAttempt := make(map[int]SubscriptionOrder, len(rows))
	maxAttempt := -1
	for i := range rows {
		identity, _, err := subscriptionOrderRenewalIdentity(&rows[i])
		if err != nil || identity.SubscriptionID != subID || identity.BillingTime != billingTime || identity.Attempt > billingFailCount ||
			rows[i].PaymentProvider != PaymentProviderToss {
			return 0, ErrTossRenewalIdentityConflict
		}
		if previous, exists := byAttempt[identity.Attempt]; exists && previous.Id != rows[i].Id {
			return 0, ErrTossRenewalIdentityConflict
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
			return 0, ErrTossRenewalIdentityConflict
		}
		switch row.Status {
		case common.TopUpStatusFailed:
			if attempt >= billingFailCount {
				return 0, ErrTossRenewalIdentityConflict
			}
			continue
		case common.TopUpStatusPending, common.TopUpStatusSuccess:
			if attempt != maxAttempt {
				return 0, ErrTossRenewalIdentityConflict
			}
			return attempt, nil
		default:
			return 0, ErrTossRenewalIdentityConflict
		}
	}
	return maxAttempt + 1, nil
}

func resolveTossRenewalEffectiveAttempt(subID int, billingTime int64, billingFailCount int) (int, error) {
	attempt := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		attempt, err = resolveTossRenewalEffectiveAttemptTx(tx, subID, billingTime, billingFailCount)
		return err
	})
	return attempt, err
}

func isTossRenewalAssociationUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return (strings.Contains(message, "duplicate") || strings.Contains(message, "unique")) &&
		(strings.Contains(message, "idx_subscription_order_toss_renewal_attempt") ||
			strings.Contains(message, "renewal_subscription_id") || strings.Contains(message, "renewal_billing_time"))
}

// haltTossRenewalAfterIdentityConflict prevents any new provider POST while
// preserving every existing order for GET-only/manual reconciliation. It does
// not increment failure counts, expire orders, or revoke the billing key: one
// of the conflicting rows may already represent moved money.
func haltTossRenewalAfterIdentityConflict(subID int, cause error) error {
	if !errors.Is(cause, ErrTossRenewalIdentityConflict) || subID <= 0 {
		return cause
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subID).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		return tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(map[string]interface{}{
			"auto_renew":         false,
			"next_billing_time":  0,
			"billing_retry_time": 0,
			"updated_at":         getDBTimestampTx(tx),
		}).Error
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("halt Toss renewal after identity conflict: %w", err))
	}
	return cause
}

// disableTossRenewalAfterLocalContractError stops an invalid local renewal
// contract without consuming BillingFailCount. BillingFailCount is reserved for
// definitive provider declines so it remains an accurate retry/order-ID cursor.
func disableTossRenewalAfterLocalContractError(subID int) error {
	if subID <= 0 {
		return gorm.ErrRecordNotFound
	}
	return runTossSettlementTransaction(DB, func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOwnerTx(tx, subID); err != nil {
			return err
		}
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", subID).First(&sub).Error; err != nil {
			return err
		}
		return disableTossRenewalWithoutContractTx(tx, &sub)
	})
}

// getTossRenewalRetrySourceOrderTx returns the earliest durable order from the
// same subscription billing cycle. A provider-declined retry gets a fresh
// orderId, but it must not get fresh commercial terms if an administrator edits
// the plan between attempts.
func getTossRenewalRetrySourceOrderTx(tx *gorm.DB, sub *UserSubscription, billingTime int64, attempt int) (*SubscriptionOrder, error) {
	if sub == nil || sub.Id <= 0 {
		return nil, errors.New("subscription is invalid")
	}
	if billingTime <= 0 || attempt <= 0 {
		return nil, nil
	}
	if billingTime != sub.NextBillingTime {
		return nil, fmt.Errorf("%w: renewal billing cycle", ErrSubscriptionPlanSnapshotMismatch)
	}
	if tx == nil {
		tx = DB
	}
	for previousAttempt := 0; previousAttempt < attempt; previousAttempt++ {
		previousOrder, err := findTossRenewalOrderForIdentityTx(tx, tossRenewalAttemptIdentity{
			SubscriptionID: sub.Id,
			BillingTime:    billingTime,
			Attempt:        previousAttempt,
		})
		if err != nil {
			return nil, err
		}
		if previousOrder == nil {
			return nil, ErrTossRenewalIdentityConflict
		}
		if previousOrder.PaymentProvider != PaymentProviderToss {
			return nil, ErrPaymentMethodMismatch
		}
		if previousOrder.UserId != sub.UserId || previousOrder.PlanId != sub.PlanId {
			return nil, fmt.Errorf("%w: renewal owner or plan id", ErrSubscriptionPlanSnapshotMismatch)
		}
		if previousOrder.Status != common.TopUpStatusFailed {
			return nil, fmt.Errorf("%w: prior renewal attempt is not terminal", ErrSubscriptionOrderStatusInvalid)
		}
		if previousOrder.ProviderAmount <= 0 ||
			!strings.EqualFold(strings.TrimSpace(previousOrder.ProviderCurrency), "KRW") {
			return nil, fmt.Errorf("%w: prior renewal provider amount", ErrSubscriptionPlanSnapshotMismatch)
		}
		if _, err := resolveSubscriptionOrderPlanTx(tx, previousOrder); err != nil {
			return nil, err
		}
		return previousOrder, nil
	}
	return nil, nil
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
	if amountKRW <= 0 || amountKRW > setting.TossMaximumChargeAmountKRW {
		return 0
	}
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amountKRW)]; ok && ds > 0 {
		discount = ds
	}
	if ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) ||
		discount <= 0 || math.IsNaN(discount) || math.IsInf(discount, 0) {
		return 0
	}
	charge := decimal.NewFromInt(amountKRW).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount)).
		Round(0)
	if charge.LessThan(decimal.NewFromInt(1)) || charge.GreaterThan(decimal.NewFromInt(setting.TossMaximumChargeAmountKRW)) {
		return 0
	}
	return charge.IntPart()
}

// TossUSDEquivalent converts charged KRW to the USD-equivalent stored in TopUp.Money.
func TossUSDEquivalent(chargedKRW int64) float64 {
	unit := setting.GetTossConfigSnapshot().UnitPrice
	if chargedKRW <= 0 || chargedKRW > setting.TossMaximumChargeAmountKRW ||
		unit <= 0 || math.IsNaN(unit) || math.IsInf(unit, 0) {
		return 0
	}
	result := decimal.NewFromInt(chargedKRW).Div(decimal.NewFromFloat(unit)).InexactFloat64()
	if result <= 0 || math.IsNaN(result) || math.IsInf(result, 0) {
		return 0
	}
	return result
}

func TossCreditQuotaFromKRW(chargedKRW int64) int {
	unit := setting.GetTossConfigSnapshot().UnitPrice
	if unit <= 0 || math.IsNaN(unit) || math.IsInf(unit, 0) ||
		chargedKRW <= 0 || chargedKRW > setting.TossMaximumChargeAmountKRW ||
		common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0
	}
	quota, ok := tossTopUpPositiveInt(decimal.NewFromInt(chargedKRW).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Div(decimal.NewFromFloat(unit)))
	if !ok {
		return 0
	}
	return quota
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
	return setting.GetTossConfigSnapshot().UnitPrice
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

func tossTopUpPositiveInt(value decimal.Decimal) (int, bool) {
	// The user/organization quota columns are bigint, so this 32-bit bound is not a
	// storage limit but a deliberate ceiling on a single payment: it keeps the
	// immutable top_ups.quota snapshot within signed-32-bit range and caps how much
	// any one Toss checkout can be worth. Widening it is a payment-policy decision,
	// not a schema one.
	maxInt := int64(math.MaxInt32)
	if value.LessThan(decimal.NewFromInt(1)) || value.GreaterThan(decimal.NewFromInt(maxInt)) {
		return 0, false
	}
	return int(value.IntPart()), true
}

func QuoteTossTopUp(amount int64, amountMode string, group string) TossTopUpQuote {
	return QuoteTossTopUpWithUnitPrice(amount, amountMode, group, tossTopUpUnitPrice())
}

func QuoteTossTopUpWithUnitPrice(amount int64, amountMode string, group string, unit float64) TossTopUpQuote {
	mode := NormalizeTossTopUpAmountMode(amountMode)
	if unit <= 0 || math.IsNaN(unit) || math.IsInf(unit, 0) {
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
	if amount <= 0 || factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) ||
		common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return quote
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	switch mode {
	case TossTopUpAmountModeQuota:
		credit := decimal.NewFromInt(amount)
		chargeDecimal := credit.
			Mul(decimal.NewFromFloat(unit)).
			Mul(decimal.NewFromFloat(factor)).
			Round(0)
		if chargeDecimal.LessThan(decimal.NewFromInt(1)) ||
			chargeDecimal.GreaterThan(decimal.NewFromInt(setting.TossMaximumChargeAmountKRW)) {
			return quote
		}
		quota, ok := tossTopUpPositiveInt(credit.Mul(quotaPerUnit))
		if !ok {
			return quote
		}
		quote.ChargeKRW = chargeDecimal.IntPart()
		quote.CreditAmount = credit.InexactFloat64()
		quote.CreditQuota = quota
	default:
		if amount > setting.TossMaximumChargeAmountKRW {
			return quote
		}
		charge := decimal.NewFromInt(amount)
		unitDec := decimal.NewFromFloat(unit)
		factorDec := decimal.NewFromFloat(factor)
		credit := charge.
			Div(unitDec).
			Div(factorDec)
		// Keep multiplication before division for the integer quota path. Doing
		// the recurring division first can turn an exact integer (for example
		// 1*3/3) into 0.999... at Decimal's division precision and under-credit.
		quota, ok := tossTopUpPositiveInt(charge.
			Mul(quotaPerUnit).
			Div(unitDec).
			Div(factorDec))
		if !ok {
			return quote
		}
		quote.ChargeKRW = amount
		quote.CreditAmount = credit.InexactFloat64()
		quote.CreditQuota = quota
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
	ProviderStatus  string
	Total           int64
	BalanceAmount   int64
	PaymentKey      string
	ProviderPayload string
}

// TossBillingCharger performs a Toss billing charge. Injected by the controller package
// at init to avoid a model→controller import cycle.
type TossBillingCharger func(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*TossBillingChargeResult, error)

var tossBillingCharger TossBillingCharger

// SetTossBillingCharger wires in the HTTP charger. Called from controller init().
func SetTossBillingCharger(fn TossBillingCharger) { tossBillingCharger = fn }

// TossRenewalPaymentLookup verifies a durable renewal order without creating a
// new payment. It must use the single secret recorded for that POST attempt.
type TossRenewalPaymentLookup func(ctx context.Context, secretKey, orderId string, amount int64) (*TossBillingChargeResult, error)

var tossRenewalPaymentLookup TossRenewalPaymentLookup

func SetTossRenewalPaymentLookup(fn TossRenewalPaymentLookup) {
	tossRenewalPaymentLookup = fn
}

// TossBillingRevoker deletes a billing key at Toss. Injected by the controller package
// to avoid a model→controller import cycle.
type TossBillingRevoker func(ctx context.Context, billingKey, secretKey string) error

var tossBillingRevoker TossBillingRevoker

func SetTossBillingRevoker(fn TossBillingRevoker) { tossBillingRevoker = fn }

func revokeTossBillingRemote(ctx context.Context, billingKey, secretKey string) error {
	if strings.TrimSpace(billingKey) == "" {
		return nil
	}
	if tossBillingRevoker == nil {
		return errors.New("toss billing revoker not configured")
	}
	return tossBillingRevoker(ctx, billingKey, secretKey)
}

func isTossBillingAmbiguousChargeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrTossBillingChargePending) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func recordTossRenewalFulfillmentEvent(tradeNo string, charge *TossBillingChargeResult, amount int64, financialMismatch bool, resolutionNote ...string) error {
	if strings.TrimSpace(tradeNo) == "" || charge == nil {
		return errors.New("invalid Toss renewal fulfillment event")
	}
	note := ""
	if len(resolutionNote) > 0 {
		note = strings.TrimSpace(resolutionNote[0])
	}
	providerStatus := strings.ToUpper(strings.TrimSpace(charge.ProviderStatus))
	if providerStatus == "" && charge.Done {
		providerStatus = "DONE"
	}
	eventType := TossPaymentEventTypeFulfillment
	eventKey := "toss_fulfillment_" + common.Sha1([]byte(tradeNo))
	originalAmount := amount
	if financialMismatch {
		eventType = TossPaymentEventTypeFinancialMismatch
		originalAmount = charge.Total
		eventKey = TossFinancialMismatchEventKey(tradeNo, charge.PaymentKey, providerStatus, originalAmount, charge.BalanceAmount)
	}
	_, err := RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             eventKey,
		EventType:            eventType,
		OrderId:              tradeNo,
		PaymentKey:           charge.PaymentKey,
		Status:               providerStatus,
		BalanceAmount:        charge.BalanceAmount,
		OriginalAmount:       originalAmount,
		ProviderPayload:      charge.ProviderPayload,
		ReconciliationStatus: TossReconciliationStatusRequired,
		ResolutionNote:       note,
	})
	return err
}

// tossSameMIDCurrentBillingSecret returns the current billing secret only when
// the durable client-key fingerprint proves that it belongs to the same MID as
// the original provider attempt. This is suitable for read-only payment lookup:
// a rotated secret for the same MID can still query payments created by the old
// secret. It must never be used to repeat an ambiguous POST, because Toss scopes
// Idempotency-Key by API key as well as URL and method.
func tossSameMIDCurrentBillingSecret(providerClientKeyHash, exactAttemptSecret string) string {
	if !IsValidTossClientKeyFingerprint(providerClientKeyHash) {
		return ""
	}
	currentClientKey, currentSecret := setting.TossActiveBillingKeyPair()
	currentSecret = strings.TrimSpace(currentSecret)
	if currentSecret == "" || currentSecret == strings.TrimSpace(exactAttemptSecret) {
		return ""
	}
	currentClientKeyHash := tossBillingClientKeyHash(currentClientKey)
	if currentClientKeyHash == "" || providerClientKeyHash != currentClientKeyHash {
		return ""
	}
	return currentSecret
}

// lookupTossRenewalAttempt first queries with the exact secret recorded before
// the ambiguous POST. If that secret has expired after a key rotation, a proven
// same-MID current secret may be used for GET only. usedFallback tells the
// caller that a 404 came from a different API-key namespace and therefore must
// not authorize another POST automatically.
func lookupTossRenewalAttempt(ctx context.Context, order *SubscriptionOrder, exactAttemptSecret string, amount int64) (charge *TossBillingChargeResult, usedFallback bool, err error) {
	if tossRenewalPaymentLookup == nil {
		return nil, false, errors.New("toss renewal payment lookup not configured")
	}
	charge, err = tossRenewalPaymentLookup(ctx, exactAttemptSecret, order.TradeNo, amount)
	if !isTossBillingCredentialError(err) {
		return charge, false, err
	}
	fallbackSecret := tossSameMIDCurrentBillingSecret(order.ProviderClientKeyHash, exactAttemptSecret)
	if fallbackSecret == "" {
		return charge, false, err
	}
	charge, err = tossRenewalPaymentLookup(ctx, fallbackSecret, order.TradeNo, amount)
	return charge, true, err
}

// getPendingAttemptedTossRenewalOrder finds provider POST intents independently
// of the mutable subscription scheduling fields. In addition to durable v1
// markers it includes the rolling-upgrade v0 shape: old binaries stored the
// exact ProviderCredential before POST but did not know BillingAttempted.
// Cancellation deliberately clears NextBillingTime, and an administrator can
// delete the subscription, but neither action may make either form disappear
// from reconciliation.
func getPendingAttemptedTossRenewalOrder(subID int) (*SubscriptionOrder, error) {
	if subID <= 0 {
		return nil, nil
	}
	var candidates []SubscriptionOrder
	escapedPrefix := strings.TrimSuffix(tossRenewalTradeNoLikePattern(), "%")
	pattern := fmt.Sprintf("%s%d!_%%", escapedPrefix, subID)
	if err := DB.Where("payment_provider = ? AND status = ?", PaymentProviderToss, common.TopUpStatusPending).
		Where("renewal_subscription_id = ? OR trade_no LIKE ? ESCAPE '!'", subID, pattern).
		Where("billing_attempted = ? OR ((billing_charge_protocol_version = ? OR billing_charge_protocol_version IS NULL) AND provider_credential IS NOT NULL AND provider_credential <> '')",
			true, tossBillingChargeProtocolLegacy).
		Order("create_time asc, id asc").
		Limit(100).
		Find(&candidates).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]int, len(candidates))
	var matched *SubscriptionOrder
	for i := range candidates {
		identity, _, identityErr := subscriptionOrderRenewalIdentity(&candidates[i])
		if identityErr != nil {
			return nil, identityErr
		}
		identityKey := fmt.Sprintf("%d:%d:%d", identity.SubscriptionID, identity.BillingTime, identity.Attempt)
		if previousID, exists := seen[identityKey]; exists && previousID != candidates[i].Id {
			return nil, ErrTossRenewalIdentityConflict
		}
		seen[identityKey] = candidates[i].Id
		if identity.SubscriptionID == subID && matched == nil {
			matched = &candidates[i]
		}
	}
	return matched, nil
}

// normalizePotentialTossRenewalAttempt acquires the provider-call lease before
// converting a v0 pre-marker row into the durable attempted shape. This must
// run before mutable user/subscription/due-time gates: an old binary may have
// moved money and crashed, even if the subscription was subsequently canceled
// or deleted. Once normalized, every path performs exact-secret GET first.
func normalizePotentialTossRenewalAttempt(order *SubscriptionOrder) (*SubscriptionOrder, bool, error) {
	if order == nil || order.BillingAttempted {
		return order, order != nil, nil
	}
	if !IsPotentialLegacyTossSubscriptionCharge(order) {
		return nil, false, ErrTossBillingCrossCredentialRetryUnsafe
	}
	token, claimed, attempted, err := ClaimTossRenewalChargeOrder(order.TradeNo)
	if err != nil {
		return nil, false, err
	}
	if !claimed {
		return nil, false, nil
	}
	release := true
	defer func() {
		if release {
			_ = ReleaseTossSubscriptionBillingClaim(order.TradeNo, token)
		}
	}()
	if !attempted {
		attempted, err = normalizeClaimedTossSubscriptionPotentialAttempt(order.TradeNo, token)
		if err != nil {
			release = false
			return nil, false, err
		}
	}
	if !attempted {
		release = false
		return nil, false, ErrTossBillingCrossCredentialRetryUnsafe
	}
	var normalized SubscriptionOrder
	if err := DB.Where("trade_no = ? AND billing_claim_token = ?", order.TradeNo, token).First(&normalized).Error; err != nil {
		release = false
		return nil, false, err
	}
	if err := ReleaseTossSubscriptionBillingClaim(order.TradeNo, token); err != nil {
		release = false
		return nil, false, err
	}
	release = false
	normalized.BillingClaimToken = ""
	normalized.BillingClaimTime = 0
	return &normalized, true, nil
}

// tossRenewalAttemptMayRepeatPOST applies the normal authorization rules to an
// existing attempted order. A false result does not discard the order: it
// switches processing to GET-only recovery so an authoritative DONE can still
// be fulfilled (or durably flagged for compensation) without authorizing a new
// charge after cancellation, disablement, expiry, or deletion.
func tossRenewalAttemptMayRepeatPOST(order *SubscriptionOrder) (bool, error) {
	if order == nil || !order.BillingAttempted || order.Status != common.TopUpStatusPending {
		return false, nil
	}
	renewalSubID, renewalBillingTime, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentity(order)
	if identityErr != nil {
		return false, identityErr
	}
	if !isRenewal {
		return false, nil
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", renewalSubID).First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if !sub.AutoRenew || sub.Status != "active" || sub.UserId != order.UserId || sub.PlanId != order.PlanId ||
		sub.BillingKeyId <= 0 || sub.BillingKeyId != order.BillingKeyId || sub.NextBillingTime != renewalBillingTime {
		return false, nil
	}
	if sub.EndTime <= GetDBTimestamp()-TossBillingOperationalGraceSeconds {
		return false, nil
	}
	if order.RenewalEndTime > 0 && order.RenewalEndTime != sub.EndTime {
		return false, nil
	}
	contract, err := getTossRenewalContractForSubscription(sub.Id)
	if errors.Is(err, ErrTossRenewalContractUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var contractSub UserSubscription
	if err := DB.Select("id", "user_id", "plan_id", "toss_renewal_contract_snapshot").Where("id = ?", sub.Id).First(&contractSub).Error; err != nil {
		return false, err
	}
	if err := ValidateTossRenewalOrderMatchesContract(order, &contractSub); err != nil {
		if isFatalTossRenewalContractError(err) {
			return false, nil
		}
		return false, err
	}
	if err := ValidateTossSubscriptionBillingPlan(contract.Plan); err != nil {
		return false, nil
	}
	var user User
	if err := DB.Select("id", "status", "organization_id").Where("id = ?", sub.UserId).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if user.Status != common.UserStatusEnabled || user.OrganizationId > 0 {
		return false, nil
	}
	var key UserBillingKey
	if err := DB.Select("id", "status").Where("id = ?", order.BillingKeyId).First(&key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return key.Status == BillingKeyStatusActive, nil
}

// reconcileTossRenewalWithoutNewCharge is the recovery-only path for an order
// whose provider POST intent predates a lifecycle change. It performs GET only.
// A definitive 404 closes this already-outside-grace order without repeating
// POST; transient/pending lookups retain the exact recovery claim.
func reconcileTossRenewalWithoutNewCharge(ctx context.Context, order *SubscriptionOrder) error {
	if order == nil || !order.BillingAttempted {
		return ErrSubscriptionOrderStatusInvalid
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	subID, _, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentity(order)
	if identityErr != nil {
		return identityErr
	}
	if !isRenewal {
		return ErrSubscriptionOrderStatusInvalid
	}
	claimToken, claimed, attempted, err := ClaimTossRenewalChargeOrder(tradeNo)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken)
		}
	}()
	if !attempted {
		return ErrSubscriptionOrderStatusInvalid
	}
	attemptSecret, err := GetClaimedTossRenewalAttemptSecret(tradeNo, claimToken)
	if err != nil {
		retainClaim = true
		return err
	}
	charge, _, lookupErr := lookupTossRenewalAttempt(ctx, order, attemptSecret, order.ProviderAmount)
	switch {
	case lookupErr == nil:
		// Continue below with the authoritative provider state.
	case errors.Is(lookupErr, ErrTossBillingChargePending):
		retainClaim = true
		return nil
	case errors.Is(lookupErr, ErrTossBillingPaymentNotFound):
		return ExpireClaimedTossSubscriptionOrder(tradeNo, claimToken)
	case errors.Is(lookupErr, ErrTossBillingChargeCanceled), errors.Is(lookupErr, ErrTossBillingChargeTerminal):
		payload := ""
		if charge != nil {
			payload = charge.ProviderPayload
		}
		return StopTossSubscriptionBillingAfterCancellation(tradeNo, payload)
	default:
		retainClaim = true
		return lookupErr
	}
	if charge == nil || !charge.Done || charge.Total != order.ProviderAmount {
		if charge == nil {
			retainClaim = true
			return errors.New("Toss renewal lookup returned no payment")
		}
		if eventErr := recordTossRenewalFulfillmentEvent(tradeNo, charge, order.ProviderAmount, true,
			"manual reconciliation required: provider renewal response did not match the immutable order"); eventErr != nil {
			retainClaim = true
			return eventErr
		}
		retainClaim = true
		return errors.New("Toss renewal payment response mismatch; reconciliation required")
	}
	if err := RenewTossSubscriptionWithContext(ctx, subID, tradeNo, order.Money, order.ProviderAmount, charge.ProviderPayload); err != nil {
		note := "refund required or entitlement must be reconciled manually: " + err.Error()
		if eventErr := recordTossRenewalFulfillmentEvent(tradeNo, charge, order.ProviderAmount, false, note); eventErr != nil {
			retainClaim = true
			return errors.Join(err, fmt.Errorf("persist Toss renewal compensation event: %w", eventErr))
		}
		retainClaim = true
		return err
	}
	return ResolveTossPaymentEvents(tradeNo, TossPaymentEventTypeFulfillment)
}

// ProcessTossRenewal charges one due subscription and applies renew/fail bookkeeping.
func ProcessTossRenewal(ctx context.Context, subId int, maxFails int) error {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return err
	}
	attemptedOrder, attemptedOrderErr := getPendingAttemptedTossRenewalOrder(subId)
	if attemptedOrderErr != nil {
		return haltTossRenewalAfterIdentityConflict(subId, attemptedOrderErr)
	}
	if attemptedOrder != nil {
		if !attemptedOrder.BillingAttempted {
			var normalized bool
			attemptedOrder, normalized, attemptedOrderErr = normalizePotentialTossRenewalAttempt(attemptedOrder)
			if attemptedOrderErr != nil {
				return attemptedOrderErr
			}
			if !normalized {
				// Another node owns the exact same recovery lease.
				return nil
			}
		}
		mayRepeatPOST, err := tossRenewalAttemptMayRepeatPOST(attemptedOrder)
		if err != nil {
			return err
		}
		if !mayRepeatPOST {
			return reconcileTossRenewalWithoutNewCharge(ctx, attemptedOrder)
		}
	}
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
	var billingUser User
	if err := DB.Select("id", "status", "organization_id").Where("id = ?", sub.UserId).First(&billingUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeactivateTossBillingForUser(nil, sub.UserId)
		}
		return err
	}
	if billingUser.Status != common.UserStatusEnabled {
		return DeactivateTossBillingForUser(nil, sub.UserId)
	}
	if billingUser.OrganizationId > 0 {
		// Organization membership invalidates personal subscriptions, but it does
		// not invalidate an organization wallet policy owned by the same user. The
		// account-wide deactivator would cancel that legitimate policy and could
		// delete its shared provider key. Disable only Toss subscription renewal;
		// the last-reference check preserves any personal/organization wallet use.
		return CancelTossAutoRenewForUser(ctx, sub.UserId)
	}
	if attemptedOrder == nil {
		// GetDueTossRenewals returns a short-lived scheduling snapshot. Another
		// node may settle the selected cycle before this worker starts, advancing
		// NextBillingTime into the future. Revalidate against database time before
		// constructing any fresh order; attempted pending orders were handled
		// above and remain recoverable regardless of their original due time.
		now := GetDBTimestamp()
		if sub.NextBillingTime <= 0 || sub.NextBillingTime > now || sub.EndTime <= now-TossBillingOperationalGraceSeconds {
			return nil
		}
	}
	// Deterministic tradeNo per billing cycle: if the charge succeeds but
	// RenewTossSubscription fails (NextBillingTime not advanced), the next cron tick
	// retries with the SAME tradeNo → Toss Idempotency-Key dedups the charge and the
	// audit-order insert (unique trade_no) only commits once. A definitive provider
	// decline advances the attempt suffix, but all suffixes in this billing cycle
	// retain the first attempt's immutable plan snapshot. On successful renew,
	// NextBillingTime advances so the next cycle gets fresh terms and a fresh tradeNo.
	effectiveAttempt, attemptErr := resolveTossRenewalEffectiveAttempt(subId, sub.NextBillingTime, sub.BillingFailCount)
	if attemptErr != nil {
		if errors.Is(attemptErr, ErrTossRecurringOrderIDEvidenceCorrupt) {
			return attemptErr
		}
		return haltTossRenewalAfterIdentityConflict(subId, attemptErr)
	}
	tradeNo := tossRenewalTradeNo(subId, sub.NextBillingTime, effectiveAttempt)
	renewalIdentity := tossRenewalAttemptIdentity{
		SubscriptionID: subId,
		BillingTime:    sub.NextBillingTime,
		Attempt:        effectiveAttempt,
	}
	var (
		plan      *SubscriptionPlan
		chargeKRW int64
		err       error
	)
	existingOrder, existingErr := findTossRenewalOrderForIdentity(renewalIdentity)
	existingPendingBeforePrepare := existingErr == nil && existingOrder != nil && existingOrder.Status == common.TopUpStatusPending
	switch {
	case existingErr != nil:
		return haltTossRenewalAfterIdentityConflict(subId, existingErr)
	case existingOrder != nil:
		tradeNo = existingOrder.TradeNo
		if existingOrder.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if existingOrder.UserId != sub.UserId || existingOrder.PlanId != sub.PlanId {
			return fmt.Errorf("%w: renewal owner or plan id", ErrSubscriptionPlanSnapshotMismatch)
		}
		if existingOrder.Status == common.TopUpStatusSuccess {
			// Local success is not sufficient to auto-resolve a legacy event: old
			// writers could label a provider amount/card mismatch as fulfillment.
			// A webhook or transaction reconciliation with strict provider evidence
			// owns automatic resolution.
			return nil
		}
		if existingOrder.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		contract, contractErr := getTossRenewalContractForSubscription(sub.Id)
		if errors.Is(contractErr, ErrTossRenewalContractUnavailable) {
			return nil
		}
		if contractErr != nil {
			return contractErr
		}
		if err := DB.Where("id = ?", sub.Id).First(&sub).Error; err != nil {
			return err
		}
		if strings.TrimSpace(existingOrder.PlanSnapshot) == "" {
			if (existingOrder.Money > 0 && !subscriptionPricesEqual(existingOrder.Money, contract.Money)) ||
				(existingOrder.ProviderAmount > 0 && existingOrder.ProviderAmount != contract.ProviderAmount) {
				return fmt.Errorf("%w: legacy pending renewal terms", ErrSubscriptionPlanSnapshotMismatch)
			}
		} else {
			if contractErr := ValidateTossRenewalOrderMatchesContract(existingOrder, &sub); contractErr != nil {
				if !isFatalTossRenewalContractError(contractErr) {
					return contractErr
				}
				// Prepare owns the locked fail-closed transition for a corrupt
				// pending order. It disables renewal and expires the order without
				// authorizing a provider POST.
				_, _, prepareErr := prepareTossRenewalOrderForAttempt(
					sub.Id,
					renewalIdentity.BillingTime,
					renewalIdentity.Attempt,
					tradeNo,
					contract.Money,
					contract.ProviderAmount,
				)
				return prepareErr
			}
		}
		plan = contract.Plan
		chargeKRW = contract.ProviderAmount
	default:
		contract, contractErr := getTossRenewalContractForSubscription(sub.Id)
		if errors.Is(contractErr, ErrTossRenewalContractUnavailable) {
			// Legacy subscriptions without a uniquely provable paid contract are
			// disabled instead of silently adopting today's mutable plan price.
			return nil
		}
		if contractErr != nil {
			return contractErr
		}
		if err := DB.Where("id = ?", sub.Id).First(&sub).Error; err != nil {
			return err
		}
		retrySource, sourceErr := getTossRenewalRetrySourceOrderTx(nil, &sub, sub.NextBillingTime, effectiveAttempt)
		if sourceErr != nil {
			return sourceErr
		}
		if retrySource != nil {
			if err := ValidateTossRenewalOrderMatchesContract(retrySource, &sub); err != nil {
				return err
			}
		} else {
			// There is no previous attempt in this cycle. The immutable initial
			// contract is the sole authority for price and entitlements.
		}
		plan = contract.Plan
		chargeKRW = contract.ProviderAmount
	}
	if !IsTossCardAmountPayableKRW(chargeKRW) {
		// An invalid local contract amount is not a provider decline. Stop this
		// contract without consuming its card-failure budget or fabricating a
		// failed provider order.
		return disableTossRenewalAfterLocalContractError(subId)
	}
	if err := ValidateTossSubscriptionBillingPlan(plan); err != nil {
		if disableErr := disableTossRenewalAfterLocalContractError(subId); disableErr != nil {
			return errors.Join(err, disableErr)
		}
		return err
	}
	order, preexistingOrder, err := prepareTossRenewalOrderForAttempt(
		subId,
		renewalIdentity.BillingTime,
		renewalIdentity.Attempt,
		tradeNo,
		plan.PriceAmount,
		chargeKRW,
	)
	if err != nil {
		if errors.Is(err, ErrTossRecurringOrderIDEvidenceCorrupt) ||
			errors.Is(err, ErrTossRecurringOrderIDProtocolState) ||
			errors.Is(err, ErrTossRecurringOrderIDWriterTooOld) ||
			errors.Is(err, ErrTossRecurringOrderIDV2ActivationRequired) {
			return err
		}
		return haltTossRenewalAfterIdentityConflict(subId, err)
	}
	if order == nil || order.Status == common.TopUpStatusSuccess {
		return nil
	}
	tradeNo = order.TradeNo
	existingPendingBeforePrepare = (preexistingOrder || IsPotentialLegacyTossSubscriptionCharge(order)) &&
		order.Status == common.TopUpStatusPending
	plan, err = ResolveTossSubscriptionOrderPlan(order)
	if err != nil {
		return err
	}
	if order.ProviderAmount > 0 {
		chargeKRW = order.ProviderAmount
	}
	if !strings.EqualFold(strings.TrimSpace(order.ProviderCurrency), "KRW") {
		return fmt.Errorf("unsupported Toss renewal currency: %s", order.ProviderCurrency)
	}
	if !IsTossCardAmountPayableKRW(chargeKRW) {
		return disableTossRenewalAfterLocalContractError(subId)
	}
	billingKeyId := order.BillingKeyId
	if billingKeyId <= 0 {
		billingKeyId = sub.BillingKeyId
	}
	claimToken, claimed, attempted, err := ClaimTossRenewalChargeOrder(tradeNo)
	if err != nil {
		return err
	}
	if !claimed {
		// Another node owns the provider-call lease, or already finalized it.
		return nil
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken)
		}
	}()
	if !attempted {
		switch order.BillingChargeProtocolVersion {
		case tossBillingChargeProtocolLegacy:
			// A row that predates this scheduler invocation may come from a
			// binary without BillingAttempted. Its ProviderCredential was
			// nevertheless stored before the old POST, so conservatively copy
			// that exact snapshot and enter GET-before-POST recovery. A v0 row
			// without the snapshot is ambiguous and cannot safely create a POST.
			if !existingPendingBeforePrepare {
				retainClaim = true
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			attempted, err = normalizeClaimedTossSubscriptionPotentialAttempt(tradeNo, claimToken)
			if err != nil {
				retainClaim = true
				return err
			}
			if !attempted {
				retainClaim = true
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			order.BillingAttempted = true
			order.BillingAttemptCredential = order.ProviderCredential
		case tossBillingChargeProtocolDurableAttempt:
			// The final POST gate below validates and pins the exact order-time
			// credential before allowing the first provider request.
		default:
			retainClaim = true
			return ErrTossBillingCrossCredentialRetryUnsafe
		}
	}

	failAttempt := func() error {
		disabled, markErr := failTossRenewalAttempt(subId, tradeNo, claimToken, maxFails)
		if markErr != nil {
			return markErr
		}
		if disabled {
			return RevokeStoredTossBillingKey(ctx, billingKeyId)
		}
		return nil
	}

	orderName := TossSubscriptionOrderName(plan.Title, true)
	var charge *TossBillingChargeResult
	providerPOSTInvoked := false
	attemptSecret := ""
	if attempted {
		attemptSecret, err = GetClaimedTossRenewalAttemptSecret(tradeNo, claimToken)
		if err != nil {
			retainClaim = true
			return err
		}
		lookupUsedFallback := false
		charge, lookupUsedFallback, err = lookupTossRenewalAttempt(ctx, order, attemptSecret, chargeKRW)
		switch {
		case err == nil:
			// A prior worker already created the payment. Settle locally below and
			// never issue another provider POST.
		case errors.Is(err, ErrTossBillingChargePending):
			retainClaim = true
			return nil
		case errors.Is(err, ErrTossBillingPaymentNotFound):
			if lookupUsedFallback {
				// The old credential was rejected and a rotated same-MID key also
				// found no payment. Since Toss includes the API key in its
				// idempotency scope, never turn that read-only fallback into a POST
				// under the new secret.
				retainClaim = true
				return fmt.Errorf("%w: same-MID fallback lookup returned 404 for order_id=%s", ErrTossBillingCrossCredentialRetryUnsafe, tradeNo)
			}
			// It is safe to repeat only the same secret/idempotency namespace.
			charge = nil
		case errors.Is(err, ErrTossBillingChargeTerminal):
			return failAttempt()
		case errors.Is(err, ErrTossBillingChargeCanceled):
			if charge == nil {
				retainClaim = true
				return errors.New("Toss renewal cancellation lookup returned no payment")
			}
			return StopTossSubscriptionBillingAfterCancellation(tradeNo, charge.ProviderPayload)
		default:
			// Credential rejection or an unknown lookup failure cannot authorize
			// a POST under a different secret: the first payment may exist.
			retainClaim = true
			return err
		}
	}

	if charge == nil {
		var billingKey, customerKey string
		var secretKeys []string
		if attempted {
			billingKey, customerKey, _, err = getTossBillingKeyPlainWithSecretCandidatesTx(nil, billingKeyId, true, false)
			secretKeys = []string{attemptSecret}
		} else {
			billingKey, customerKey, secretKeys, err = GetTossBillingKeyPlainWithSecretCandidates(billingKeyId)
			if err == nil {
				attemptCredential := order.ProviderCredential
				// A definitive auth rejection may have promoted the next exact
				// namespace and reset BillingAttempted so the final gate runs again.
				// Prefer that durable promoted credential over the rejected
				// creation-time snapshot; it still cannot POST until Mark succeeds.
				if strings.TrimSpace(order.BillingAttemptCredential) != "" {
					attemptCredential = order.BillingAttemptCredential
				}
				secretKeys, err = prioritizeTossStoredCredentialSecret(secretKeys, attemptCredential)
			}
		}
		if err != nil {
			// No provider call occurred for a fresh claim. For a recovery claim,
			// preserve the marker because a previous POST may still exist. Local
			// key/configuration/DB errors must never consume the provider-decline
			// budget or close the unattempted order.
			retainClaim = attempted
			return err
		}
		charge, providerPOSTInvoked, err = chargeClaimedTossRenewal(
			ctx, claimToken, billingKey, customerKey, secretKeys, tradeNo, orderName, chargeKRW,
		)
	}
	if errors.Is(err, ErrTossBillingChargeCanceled) {
		if !providerPOSTInvoked {
			retainClaim = attempted
			return err
		}
		if charge == nil {
			retainClaim = true
			return errors.New("Toss renewal cancellation returned no payment")
		}
		return StopTossSubscriptionBillingAfterCancellation(tradeNo, charge.ProviderPayload)
	}
	if errors.Is(err, ErrTossBillingChargeTerminal) {
		if !providerPOSTInvoked {
			retainClaim = attempted
			return err
		}
		return failAttempt()
	}
	if errors.Is(err, ErrTossBillingOperationallyDisabled) {
		// No POST was authorized by the final gate. Preserve a previous attempt's
		// GET-before-POST marker, but never count an administrative kill switch as
		// a customer billing failure.
		retainClaim = attempted
		return err
	}
	if errors.Is(err, ErrTossBillingRenewalOutsideGrace) {
		// The final pre-POST gate can cross the grace boundary after selection.
		// Preserve an older attempted order for GET-only reconciliation; a fresh
		// order remains unattempted and will be expired by the stale sweep.
		retainClaim = attempted
		return err
	}
	if errors.Is(err, ErrTossBillingCrossCredentialRetryUnsafe) {
		// Namespace ambiguity is not a customer decline. Keep the exact order
		// and claim for GET-only/manual reconciliation; advancing the failure
		// suffix could authorize a second charge under a new orderId.
		retainClaim = true
		return err
	}
	if errors.Is(err, ErrTossBillingClaimLost) {
		// A final claim/intent CAS disagreement is an ownership or protocol
		// failure, never a card decline. Advancing BillingFailCount here would
		// create a new orderId while the current provider attempt may exist.
		retainClaim = true
		return err
	}
	if isTossBillingCredentialError(err) {
		// Toss definitively rejected the API credential, not the customer's
		// payment method. Preserve the exact attempted namespace for recovery or
		// operator action without advancing to a new order ID.
		retainClaim = true
		return err
	}
	if errors.Is(err, ErrTossRecurringOrderIDEvidenceCorrupt) {
		// A corrupt opaque row may belong to another contract. No provider POST
		// occurred at this gate, so preserve an older attempted recovery claim but
		// never disable or count a failure against this healthy subscription.
		retainClaim = attempted
		return err
	}
	if errors.Is(err, ErrTossRenewalIdentityConflict) {
		// The POST-time chain no longer matches the prepared suffix. Preserve the
		// exact row/claim for inspection and disable renewal; treating this as a
		// customer decline would advance to yet another orderId.
		retainClaim = true
		return haltTossRenewalAfterIdentityConflict(subId, err)
	}
	if errors.Is(err, ErrTossBillingCancellationPrecedesCharge) {
		// The final claim-guarded transaction already terminalized this unposted
		// order and disabled the key. Treat it as a clean fail-closed outcome.
		return nil
	}
	if err != nil && !providerPOSTInvoked {
		// chargeClaimedTossRenewal also owns the final local authorization gate.
		// A DB/lifecycle/contract failure from that gate means no provider POST
		// was invoked and therefore cannot be classified as a card decline.
		retainClaim = attempted
		return err
	}
	if isTossBillingAmbiguousChargeError(err) {
		// Keep the claim and exact attempt credential. A stale-claim recovery
		// worker must GET before retrying the same idempotent POST.
		retainClaim = true
		return nil
	}
	if charge != nil && (!charge.Done || charge.Total != chargeKRW) {
		// A syntactically successful provider response can still fail our strict
		// order/amount/card validation. Money may already have moved, so never
		// advance to a fresh orderId (which could charge the card again). Keep the
		// durable attempt claimed and surface it for manual reconciliation.
		if eventErr := recordTossRenewalFulfillmentEvent(tradeNo, charge, chargeKRW, true); eventErr != nil {
			retainClaim = true
			return errors.Join(errors.New("Toss renewal payment response mismatch"), eventErr)
		}
		retainClaim = true
		return errors.New("Toss renewal payment response mismatch; reconciliation required")
	}
	if charge != nil && charge.Done && charge.Total == chargeKRW {
		// A strictly validated DONE result is authoritative even if an adapter
		// also returned ancillary error context after the provider call.
		err = nil
	}
	if err != nil {
		// At this point the error came from an invoked provider POST and survived
		// the explicit ambiguous/auth/local classifications above. The billing
		// adapter guarantees such an error is a definitive provider rejection.
		return failAttempt()
	}
	if charge == nil {
		if providerPOSTInvoked {
			// A callback that returns neither a result nor an error cannot prove the
			// POST failed. Keep the durable marker/claim and force GET-before-POST
			// recovery instead of advancing to another order ID.
			retainClaim = true
			return errors.New("Toss renewal provider POST returned no result; reconciliation required")
		}
		return errors.New("Toss renewal charge result is unavailable")
	}
	if err := RenewTossSubscriptionWithContext(ctx, subId, tradeNo, order.Money, chargeKRW, charge.ProviderPayload); err != nil {
		note := "refund required or entitlement must be reconciled manually: " + err.Error()
		if eventErr := recordTossRenewalFulfillmentEvent(tradeNo, charge, chargeKRW, false, note); eventErr != nil {
			retainClaim = true
			return errors.Join(err, fmt.Errorf("persist Toss renewal fulfillment event: %w", eventErr))
		}
		retainClaim = true
		return err
	}
	return ResolveTossPaymentEvents(tradeNo, TossPaymentEventTypeFulfillment)
}

// StopTossSubscriptionBillingAfterCancellation records a local terminal state
// for a provider-canceled subscription payment and disables only the recurring
// subscription identified by that payment. A pending order is closed so a
// later billing tick cannot generate a fresh tradeNo and charge again. A
// previously successful order keeps its historical success status; the
// separate durable cancellation event remains the source of truth for manual
// reconciliation.
func StopTossSubscriptionBillingAfterCancellation(tradeNo, providerPayload string) error {
	return StopTossSubscriptionBillingAfterCancellationWithContext(context.Background(), tradeNo, providerPayload)
}

func StopTossSubscriptionBillingAfterCancellationWithContext(ctx context.Context, tradeNo, providerPayload string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOrderOwnerForCleanupTx(tx, tradeNo); err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		renewalSubID, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(&order)
		if identityErr != nil {
			return identityErr
		}

		if order.Status == common.TopUpStatusPending {
			updates := map[string]interface{}{
				"status":              common.TopUpStatusFailed,
				"complete_time":       common.GetTimestamp(),
				"billing_claim_token": "",
				"billing_claim_time":  0,
			}
			if strings.TrimSpace(providerPayload) != "" {
				updates["provider_payload"] = providerPayload
			}
			if err := tx.Model(&SubscriptionOrder{}).
				Where("id = ? AND status = ?", order.Id, common.TopUpStatusPending).
				Updates(updates).Error; err != nil {
				return err
			}
		}

		billingKeyID := order.BillingKeyId
		if isRenewalOrder {
			var sub UserSubscription
			subErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id", "billing_key_id").
				Where("id = ?", renewalSubID).
				First(&sub).Error
			if subErr != nil && !errors.Is(subErr, gorm.ErrRecordNotFound) {
				return subErr
			}
			if subErr == nil && billingKeyID <= 0 {
				billingKeyID = sub.BillingKeyId
			}
			if subErr == nil {
				if err := tx.Model(&UserSubscription{}).
					Where("id = ?", renewalSubID).
					Updates(map[string]interface{}{
						"auto_renew":         false,
						"next_billing_time":  0,
						"billing_retry_time": 0,
						"updated_at":         common.GetTimestamp(),
					}).Error; err != nil {
					return err
				}
			}
		} else if billingKeyID > 0 {
			// Initial-order cancellations are mapped through the immutable contract
			// provenance. A provider key may be shared, so user/plan/key equality
			// alone is not enough to cancel another subscription.
			var candidates []UserSubscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND plan_id = ? AND billing_key_id = ? AND auto_renew = ? AND status = ?",
					order.UserId, order.PlanId, billingKeyID, true, "active").
				Find(&candidates).Error; err != nil {
				return err
			}
			for i := range candidates {
				snapshot, err := decodeTossRenewalContractSnapshot(&candidates[i])
				if err != nil || snapshot.TradeNo != order.TradeNo {
					continue
				}
				if err := tx.Model(&UserSubscription{}).
					Where("id = ? AND auto_renew = ?", candidates[i].Id, true).
					Updates(map[string]interface{}{
						"auto_renew":         false,
						"next_billing_time":  0,
						"billing_retry_time": 0,
						"updated_at":         common.GetTimestamp(),
					}).Error; err != nil {
					return err
				}
			}
		}
		if billingKeyID > 0 {
			_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, billingKeyID)
			return err
		}
		return nil
	})
}

func PrepareTossRenewalOrder(subId int, tradeNo string, money float64, providerAmount int64) (*SubscriptionOrder, error) {
	if subId <= 0 {
		return nil, errors.New("subId is invalid")
	}
	if strings.TrimSpace(tradeNo) == "" {
		return nil, errors.New("tradeNo is empty")
	}
	parsedSubID, renewalBillingTime, renewalAttempt, ok := parseTossRenewalTradeNoWithAttempt(tradeNo)
	if !ok || parsedSubID != subId {
		return nil, fmt.Errorf("%w: renewal billing cycle", ErrSubscriptionPlanSnapshotMismatch)
	}
	order, _, err := prepareTossRenewalOrderForAttempt(
		subId, renewalBillingTime, renewalAttempt, tradeNo, money, providerAmount,
	)
	return order, err
}

func prepareTossRenewalOrderForAttempt(subId int, renewalBillingTime int64, renewalAttempt int, tradeNo string, money float64, providerAmount int64) (*SubscriptionOrder, bool, error) {
	if subId <= 0 {
		return nil, false, errors.New("subId is invalid")
	}
	identity := tossRenewalAttemptIdentity{SubscriptionID: subId, BillingTime: renewalBillingTime, Attempt: renewalAttempt}
	if !identity.valid() || strings.TrimSpace(tradeNo) == "" {
		return nil, false, ErrTossRenewalIdentityConflict
	}
	var order SubscriptionOrder
	preexisting := false
	var err error
	for transactionAttempt := 0; transactionAttempt < 2; transactionAttempt++ {
		order = SubscriptionOrder{}
		preexisting = false
		err = DB.Transaction(func(tx *gorm.DB) error {
			var subReference UserSubscription
			if err := tx.Select("user_id").Where("id = ?", subId).First(&subReference).Error; err != nil {
				return err
			}
			var user User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id", "status", "organization_id").Where("id = ?", subReference.UserId).First(&user).Error; err != nil {
				return err
			}
			if user.Status != common.UserStatusEnabled || user.OrganizationId > 0 {
				return ErrTossBillingUserInactive
			}
			var sub UserSubscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subId).First(&sub).Error; err != nil {
				return err
			}
			if !sub.AutoRenew || sub.Status != "active" {
				order = SubscriptionOrder{}
				return nil
			}
			if identity.SubscriptionID != sub.Id || identity.BillingTime != sub.NextBillingTime {
				return fmt.Errorf("%w: renewal billing cycle", ErrSubscriptionPlanSnapshotMismatch)
			}
			effectiveAttempt, err := resolveTossRenewalEffectiveAttemptTx(tx, sub.Id, sub.NextBillingTime, sub.BillingFailCount)
			if err != nil {
				return err
			}
			if identity.Attempt != effectiveAttempt {
				return ErrTossRenewalIdentityConflict
			}
			if sub.BillingKeyId <= 0 {
				return errors.New("billingKeyId is invalid")
			}
			var key UserBillingKey
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("provider_credential", "provider_client_key_hash").Where("id = ?", sub.BillingKeyId).First(&key).Error; err != nil {
				return err
			}
			keyClientKeyHash := ""
			if IsValidTossClientKeyFingerprint(key.ProviderClientKeyHash) {
				keyClientKeyHash = key.ProviderClientKeyHash
			}

			existingOrder, err := findTossRenewalOrderForIdentityTx(tx, identity)
			if err != nil {
				return err
			}
			if existingOrder != nil {
				order = *existingOrder
				preexisting = true
				if order.PaymentProvider != PaymentProviderToss {
					return ErrPaymentMethodMismatch
				}
				if order.UserId != sub.UserId || order.PlanId != sub.PlanId {
					return fmt.Errorf("%w: renewal owner or plan id", ErrSubscriptionPlanSnapshotMismatch)
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
				if order.ProviderClientKeyHash == "" && keyClientKeyHash != "" {
					order.ProviderClientKeyHash = keyClientKeyHash
					changed = true
				}
				if order.Money <= 0 {
					order.Money = money
					changed = true
				}
				if strings.TrimSpace(order.PlanSnapshot) == "" {
					contract, contractErr := loadOrBackfillTossRenewalContractTx(tx, &sub)
					if contractErr != nil {
						if isFatalTossRenewalContractError(contractErr) {
							if err := disableAndExpireInvalidTossRenewalOrderTx(tx, &sub, &order); err != nil {
								return err
							}
							order = SubscriptionOrder{}
							return nil
						}
						return contractErr
					}
					if order.Money <= 0 {
						order.Money = contract.Money
					}
					if order.ProviderAmount <= 0 {
						order.ProviderAmount = contract.ProviderAmount
					}
					if snapshotErr := SetTossRenewalOrderSnapshotFromContract(&order, &sub); snapshotErr != nil {
						if isFatalTossRenewalContractError(snapshotErr) {
							if err := disableAndExpireInvalidTossRenewalOrderTx(tx, &sub, &order); err != nil {
								return err
							}
							order = SubscriptionOrder{}
							return nil
						}
						return snapshotErr
					}
					changed = true
				} else {
					_, contractErr := loadOrBackfillTossRenewalContractTx(tx, &sub)
					if contractErr == nil {
						contractErr = ValidateTossRenewalOrderMatchesContract(&order, &sub)
					}
					if contractErr != nil {
						if !isFatalTossRenewalContractError(contractErr) {
							return contractErr
						}
						if err := disableAndExpireInvalidTossRenewalOrderTx(tx, &sub, &order); err != nil {
							return err
						}
						order = SubscriptionOrder{}
						return nil
					}
				}
				if order.RenewalEndTime <= 0 && !order.BillingAttempted {
					// This remains safe only before the provider-attempt marker: once a
					// POST might have happened, recovery must not invent a historical
					// subscription boundary from mutable current state.
					order.RenewalEndTime = sub.EndTime
					changed = true
				}
				if changed {
					return tx.Save(&order).Error
				}
				return nil
			}
			contract, contractErr := loadOrBackfillTossRenewalContractTx(tx, &sub)
			if contractErr != nil {
				if isFatalTossRenewalContractError(contractErr) {
					if err := disableTossRenewalWithoutContractTx(tx, &sub); err != nil {
						return err
					}
					order = SubscriptionOrder{}
					return nil
				}
				return contractErr
			}
			var plan *SubscriptionPlan
			retrySource, err := getTossRenewalRetrySourceOrderTx(tx, &sub, renewalBillingTime, renewalAttempt)
			if err != nil {
				return err
			}
			if retrySource != nil {
				if err = ValidateTossRenewalOrderMatchesContract(retrySource, &sub); err != nil {
					if isFatalTossRenewalContractError(err) {
						if disableErr := disableTossRenewalWithoutContractTx(tx, &sub); disableErr != nil {
							return disableErr
						}
						order = SubscriptionOrder{}
						return nil
					}
					return err
				}
			}
			plan = contract.Plan
			money = contract.Money
			providerAmount = contract.ProviderAmount
			if err := ValidateTossSubscriptionBillingPlan(plan); err != nil {
				return err
			}
			providerCredential := strings.TrimSpace(key.ProviderCredential)
			if providerCredential == "" {
				// Legacy active billing-key rows may predate the persisted provider
				// credential. A brand-new durable order has provably made no POST yet, so
				// snapshot the first currently authorized same-MID candidate now. Never
				// perform this backfill on an existing pending order: it may already
				// have been posted by an older binary.
				_, _, secretKeys, credentialErr := getTossBillingKeyPlainWithSecretCandidatesTx(tx, sub.BillingKeyId, true, true)
				if credentialErr != nil {
					return credentialErr
				}
				if len(secretKeys) == 0 || strings.TrimSpace(secretKeys[0]) == "" {
					return ErrTossBillingCrossCredentialRetryUnsafe
				}
				providerCredential, credentialErr = EncryptProviderCredential(secretKeys[0])
				if credentialErr != nil {
					return credentialErr
				}
			}
			if _, err := tossRecurringOrderIDWriterVersionTx(tx); err != nil {
				return err
			}
			order = SubscriptionOrder{
				UserId:          sub.UserId,
				PlanId:          sub.PlanId,
				Money:           money,
				PaymentMethod:   PaymentMethodToss,
				PaymentProvider: PaymentProviderToss,
				Status:          common.TopUpStatusPending,
				// Cleanup and recovery compare this field with database-clock cutoffs.
				// A skewed worker clock must not make a fresh renewal look stale.
				CreateTime:                   getDBTimestampTx(tx),
				ProviderAmount:               providerAmount,
				ProviderCurrency:             "KRW",
				ProviderCredential:           providerCredential,
				ProviderClientKeyHash:        keyClientKeyHash,
				BillingKeyId:                 sub.BillingKeyId,
				RenewalEndTime:               sub.EndTime,
				BillingChargeProtocolVersion: tossBillingChargeProtocolDurableAttempt,
				RenewalSubscriptionId:        &identity.SubscriptionID,
				RenewalBillingTime:           &identity.BillingTime,
				RenewalAttempt:               &identity.Attempt,
				RenewalOrderIdVersion:        tossRecurringOrderIDVersionOpaque,
			}
			for candidateAttempt := 0; candidateAttempt < tossOpaqueOrderIDCollisionMaxRetries; candidateAttempt++ {
				order.Id = 0
				order.PlanSnapshot = ""
				order.RenewalCreationToken = common.GetUUID()
				order.TradeNo, err = newTossRecurringOpaqueOrderID(tossRenewalOpaqueOrderIDPrefix)
				if err != nil {
					return err
				}
				if err := SetTossRenewalOrderSnapshotFromContract(&order, &sub); err != nil {
					return err
				}
				createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&order)
				if createResult.Error != nil {
					return createResult.Error
				}
				candidateID := order.Id
				candidateCreationToken := order.RenewalCreationToken
				winner, winnerErr := findTossRenewalOrderForIdentityTx(tx, identity)
				if winnerErr != nil {
					return winnerErr
				}
				if winner == nil {
					// The only other unique key is trade_no. An opaque collision is
					// harmless: keep the singleton lock and try a fresh random ID.
					if candidateAttempt+1 < tossOpaqueOrderIDCollisionMaxRetries {
						continue
					}
					return ErrTossRenewalIdentityConflict
				}
				if candidateID <= 0 || winner.Id != candidateID || winner.RenewalCreationToken != candidateCreationToken {
					// Re-enter the full existing-order branch in a fresh transaction so
					// the database winner receives the same owner/status/contract checks as
					// an order that existed before this call. Do not use RowsAffected here:
					// MySQL CLIENT_FOUND_ROWS can report one for its no-op conflict update.
					return errTossRenewalWinnerRetry
				}
				order = *winner
				return nil
			}
			return ErrTossRenewalIdentityConflict
		})
		if !errors.Is(err, errTossRenewalWinnerRetry) && !isTossRenewalAssociationUniqueConflict(err) {
			break
		}
		if transactionAttempt == 1 {
			err = ErrTossRenewalIdentityConflict
			break
		}
	}
	if err != nil {
		return nil, false, err
	}
	if order.Id == 0 {
		return nil, preexisting, nil
	}
	return &order, preexisting, nil
}

// normalizeClaimedTossSubscriptionPotentialAttempt converts a pre-marker
// SubscriptionOrder into the conservative GET-before-POST shape while the
// caller owns its provider claim. Releases before BillingAttempted existed
// still persisted ProviderCredential before charging, so an existing row with
// that snapshot may already have moved money even though the additive boolean
// reads false after AutoMigrate.
func normalizeClaimedTossSubscriptionPotentialAttempt(tradeNo, token string) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return false, ErrTossBillingClaimLost
	}
	now := GetDBTimestamp()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id > 0 AND billing_claim_token = ? AND billing_attempted = ?",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, token, false).
		Where("billing_charge_protocol_version = ? OR billing_charge_protocol_version IS NULL", tossBillingChargeProtocolLegacy).
		Where("provider_credential IS NOT NULL AND provider_credential <> ''").
		Updates(map[string]interface{}{
			"billing_attempted":          true,
			"billing_attempt_credential": gorm.Expr("provider_credential"),
			"billing_attempt_time": gorm.Expr(
				"CASE WHEN billing_attempt_time > 0 THEN billing_attempt_time WHEN create_time > 0 THEN create_time ELSE ? END", now),
			"billing_claim_time": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	var order SubscriptionOrder
	if err := DB.Select("payment_provider", "status", "billing_key_id", "billing_claim_token", "billing_attempted", "billing_attempt_credential", "provider_credential", "billing_charge_protocol_version").
		Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return false, ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending || order.BillingKeyId <= 0 || order.BillingClaimToken != token {
		return false, ErrTossBillingClaimLost
	}
	if order.BillingAttempted {
		if strings.TrimSpace(order.BillingAttemptCredential) == "" {
			return false, ErrTossBillingCrossCredentialRetryUnsafe
		}
		return true, nil
	}
	if order.BillingChargeProtocolVersion == tossBillingChargeProtocolLegacy && strings.TrimSpace(order.ProviderCredential) != "" {
		return false, errors.New("Toss potential subscription attempt was not normalized")
	}
	return false, nil
}

// ClaimTossRenewalChargeOrder is the cross-process gate for the remote renewal
// POST. attempted reports whether a previous owner may already have called
// Toss, in which case the caller must perform an order GET before any POST.
func ClaimTossRenewalChargeOrder(tradeNo string) (token string, claimed bool, attempted bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, false, errors.New("tradeNo is empty")
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id > 0",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, false, result.Error
	}
	if result.RowsAffected == 1 {
		var marker SubscriptionOrder
		if err := DB.Select("billing_attempted").Where("trade_no = ?", tradeNo).First(&marker).Error; err != nil {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, false, err
		}
		return token, true, marker.BillingAttempted, nil
	}

	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusPending || order.Status == common.TopUpStatusSuccess {
		return "", false, order.BillingAttempted, nil
	}
	return "", false, order.BillingAttempted, ErrSubscriptionOrderStatusInvalid
}

// ClaimTossSubscriptionFirstChargeOrder is the cross-process lease for
// recovering an initial subscription charge whose provider POST may already
// have run. It deliberately accepts only non-renewal orders with a durable
// attempt marker; fresh first charges continue to use the billing-issue claim.
func validateTossInitialChargeOrderIdentity(tradeNo string) error {
	var order SubscriptionOrder
	if err := DB.Select(
		"trade_no", "payment_provider", "renewal_subscription_id", "renewal_billing_time", "renewal_attempt", "renewal_order_id_version",
	).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	_, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(&order)
	if identityErr != nil {
		return identityErr
	}
	if isRenewalOrder {
		return errors.New("renewal order cannot use the initial-charge path")
	}
	return nil
}

func validateTossRenewalChargeOrderIdentity(tradeNo string) error {
	var order SubscriptionOrder
	if err := DB.Select(
		"trade_no", "payment_provider", "renewal_subscription_id", "renewal_billing_time", "renewal_attempt", "renewal_order_id_version",
	).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	_, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(&order)
	if identityErr != nil {
		return identityErr
	}
	if !isRenewalOrder {
		return errors.New("initial subscription order cannot use the renewal-charge path")
	}
	return nil
}

func ClaimTossSubscriptionFirstChargeOrder(tradeNo string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
	}
	if strings.HasPrefix(tradeNo, TossRenewalTradeNoPrefix) || HasTossRenewalOpaqueOrderIDPrefix(tradeNo) {
		return "", false, errors.New("renewal order cannot use the initial-charge claim")
	}
	if err := validateTossInitialChargeOrderIdentity(tradeNo); err != nil {
		return "", false, err
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id > 0",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Where("(billing_attempted = ? OR ((billing_charge_protocol_version = ? OR billing_charge_protocol_version IS NULL) AND provider_credential IS NOT NULL AND provider_credential <> ''))",
			true, tossBillingChargeProtocolLegacy).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		attempted, normalizeErr := normalizeClaimedTossSubscriptionPotentialAttempt(tradeNo, token)
		if normalizeErr != nil {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, normalizeErr
		}
		if !attempted {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, ErrTossBillingCrossCredentialRetryUnsafe
		}
		return token, true, nil
	}

	var order SubscriptionOrder
	if err := DB.Select("payment_provider", "status", "billing_key_id", "billing_attempted", "provider_credential", "billing_charge_protocol_version").
		Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusSuccess {
		return "", false, nil
	}
	if order.Status != common.TopUpStatusPending {
		return "", false, ErrSubscriptionOrderStatusInvalid
	}
	potentialLegacyAttempt := IsPotentialLegacyTossSubscriptionCharge(&order)
	if order.BillingKeyId <= 0 || (!order.BillingAttempted && !potentialLegacyAttempt) {
		return "", false, errors.New("Toss initial charge attempt is unavailable")
	}
	// A different node still owns a live lease.
	return "", false, nil
}

// ClaimTossUnattemptedSubscriptionChargeOrder acquires a recovery lease only
// for a v1 attached order with a valid exact ProviderCredential snapshot and no
// attempt marker. v0/unknown rows and missing snapshots are ambiguous: they
// must not borrow the active credential or be expired as if a POST were known
// not to have occurred.
func ClaimTossUnattemptedSubscriptionChargeOrder(tradeNo string) (token string, claimed bool, err error) {
	return claimTossUnattemptedSubscriptionChargeOrder(tradeNo, false)
}

// ClaimTossUnattemptedRenewalChargeOrder is the renewal counterpart of
// ClaimTossUnattemptedSubscriptionChargeOrder. Keeping a separate entry point
// prevents an opaque association-backed renewal from entering an initial-
// purchase path while preserving the same durable-protocol and exact-
// credential requirements for safe GET-and-expire recovery.
func ClaimTossUnattemptedRenewalChargeOrder(tradeNo string) (token string, claimed bool, err error) {
	return claimTossUnattemptedSubscriptionChargeOrder(tradeNo, true)
}

func claimTossUnattemptedSubscriptionChargeOrder(tradeNo string, renewal bool) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
	}
	validateIdentity := validateTossInitialChargeOrderIdentity
	if renewal {
		validateIdentity = validateTossRenewalChargeOrderIdentity
	}
	if err := validateIdentity(tradeNo); err != nil {
		return "", false, err
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id > 0 AND billing_attempted = ?",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, false).
		Where("billing_charge_protocol_version = ? AND provider_credential IS NOT NULL AND provider_credential <> ''",
			tossBillingChargeProtocolDurableAttempt).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		var claimedOrder SubscriptionOrder
		if err := DB.Select("provider_credential").Where("trade_no = ? AND billing_claim_token = ?", tradeNo, token).First(&claimedOrder).Error; err != nil {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, err
		}
		// Repeat the immutable identity check after acquiring the lease. This
		// closes the validation/update gap if a corrupt administrative write races
		// the first read, and makes the claim itself authoritative for its path.
		if err := validateIdentity(tradeNo); err != nil {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, err
		}
		secret, decryptErr := DecryptProviderCredential(claimedOrder.ProviderCredential)
		if decryptErr != nil || strings.TrimSpace(secret) == "" {
			_ = ReleaseTossSubscriptionBillingClaim(tradeNo, token)
			return "", false, ErrTossBillingCrossCredentialRetryUnsafe
		}
		return token, true, nil
	}

	var order SubscriptionOrder
	if err := DB.Select("payment_provider", "status", "billing_key_id", "billing_attempted", "billing_charge_protocol_version", "provider_credential").
		Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusSuccess {
		return "", false, nil
	}
	if order.Status != common.TopUpStatusPending {
		return "", false, ErrSubscriptionOrderStatusInvalid
	}
	if order.BillingKeyId <= 0 {
		return "", false, errors.New("Toss subscription billing key is unavailable")
	}
	if !order.BillingAttempted && (order.BillingChargeProtocolVersion != tossBillingChargeProtocolDurableAttempt || strings.TrimSpace(order.ProviderCredential) == "") {
		return "", false, ErrTossBillingCrossCredentialRetryUnsafe
	}
	// Either the original worker owns a live lease or it advanced to the
	// attempted recovery state. In both cases this cleanup worker must stand down.
	return "", false, nil
}

// ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation
// performs the terminal initial-charge transition only while the caller still
// owns the distributed claim. The key status and order status change in one
// transaction, so a stale cleanup worker cannot revoke a key after another
// worker has reclaimed or settled the order.
func ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOrderOwnerForCleanupTx(tx, tradeNo); err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "payment_provider", "status", "billing_key_id", "billing_claim_token").
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending || order.BillingKeyId <= 0 {
			return ErrSubscriptionOrderStatusInvalid
		}
		if order.BillingClaimToken != token {
			return ErrTossBillingClaimLost
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND status = ? AND billing_claim_token = ?", order.Id, common.TopUpStatusPending, token).
			Updates(map[string]interface{}{
				"status":              common.TopUpStatusExpired,
				"complete_time":       common.GetTimestamp(),
				"billing_claim_token": "",
				"billing_claim_time":  0,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		_, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, order.BillingKeyId)
		return err
	})
}

// ExpireClaimedTossPendingRenewalOrderAndMarkFailure is the renewal counterpart
// of the claimed initial-charge transition. It closes and counts the attempt
// atomically only for the current lease owner.
func ExpireClaimedTossPendingRenewalOrderAndMarkFailure(subId int, tradeNo, token string, maxFails int) (bool, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if subId <= 0 || tradeNo == "" || token == "" {
		return false, ErrTossBillingClaimLost
	}
	disabled := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOwnerTx(tx, subId); err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "payment_provider", "status", "billing_claim_token").
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		if order.BillingClaimToken != token {
			return ErrTossBillingClaimLost
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND status = ? AND billing_claim_token = ?", order.Id, common.TopUpStatusPending, token).
			Updates(map[string]interface{}{
				"status":              common.TopUpStatusExpired,
				"complete_time":       common.GetTimestamp(),
				"billing_claim_token": "",
				"billing_claim_time":  0,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		var err error
		disabled, err = markTossBillingFailureTx(tx, subId, maxFails)
		return err
	})
	return disabled, err
}

// ExpireClaimedTossSubscriptionOrder closes an invalid/non-renewal-shaped Toss
// order without touching a key, but still requires the current recovery lease.
func ExpireClaimedTossSubscriptionOrder(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_claim_token = ?",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).
		Updates(map[string]interface{}{
			"status":              common.TopUpStatusExpired,
			"complete_time":       common.GetTimestamp(),
			"billing_claim_token": "",
			"billing_claim_time":  0,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTossBillingClaimLost
	}
	return nil
}

// hasTossCancellationForSubscriptionTx closes the webhook event/apply crash
// gap without treating a payment cancellation as deletion of a shared
// billingKey. Only an initial order recorded in this subscription's immutable
// contract, or a renewal order whose ID encodes this subscription, can stop it.
func hasTossCancellationForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, billingKeyID int) (bool, error) {
	if tx == nil || sub == nil || sub.Id <= 0 || billingKeyID <= 0 {
		return false, nil
	}
	canceledOrders := tx.Model(&TossPaymentEvent{}).
		Select("order_id").
		Where("event_type = ? AND status IN ?", TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"})
	var orders []SubscriptionOrder
	if err := tx.Model(&SubscriptionOrder{}).
		Select("trade_no", "user_id", "plan_id", "payment_provider", "renewal_subscription_id", "renewal_billing_time", "renewal_attempt", "renewal_order_id_version").
		Where("billing_key_id = ? AND trade_no IN (?)", billingKeyID, canceledOrders).
		Where("payment_provider = ?", PaymentProviderToss).
		Find(&orders).Error; err != nil {
		return false, err
	}
	originTradeNo := ""
	if snapshot, err := decodeTossRenewalContractSnapshot(sub); err == nil {
		originTradeNo = strings.TrimSpace(snapshot.TradeNo)
	}
	for i := range orders {
		if orders[i].UserId != sub.UserId || orders[i].PlanId != sub.PlanId {
			continue
		}
		renewalSubID, _, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentity(&orders[i])
		if identityErr != nil {
			return false, identityErr
		}
		if isRenewal && renewalSubID == sub.Id {
			return true, nil
		}
		if originTradeNo != "" && orders[i].TradeNo == originTradeNo {
			return true, nil
		}
	}
	return false, nil
}

// MarkTossRenewalChargeAttempt records the exact secret immediately before a
// remote POST. The update is claim-guarded, so a stale worker cannot overwrite
// the credential chosen by the current owner or create a payment afterward.
func MarkTossRenewalChargeAttempt(tradeNo, token, secretKey string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	secretKey = strings.TrimSpace(secretKey)
	if tradeNo == "" || token == "" || secretKey == "" {
		return errors.New("invalid Toss renewal charge attempt")
	}
	renewalSubID, renewalBillingTime, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentityByTradeNo(tradeNo)
	if identityErr != nil {
		return identityErr
	}
	if !isRenewal {
		return errors.New("invalid Toss renewal order id")
	}
	credential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return err
	}
	cancellationStopped := false
	var contractStoppedErr error
	err = DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTBilling)
		if err != nil {
			return err
		}
		user, err := lockTossSubscriptionOrderUserTx(tx, tradeNo)
		if err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		lockedSubID, lockedBillingTime, lockedAttempt, lockedRenewal, identityErr := ResolveTossRenewalOrderIdentity(&order)
		if identityErr != nil || !lockedRenewal || lockedSubID != renewalSubID || lockedBillingTime != renewalBillingTime {
			return ErrTossRenewalIdentityConflict
		}
		if order.Status != common.TopUpStatusPending || order.BillingKeyId <= 0 || order.BillingClaimToken != token {
			return ErrTossBillingClaimLost
		}
		if user.Id != order.UserId || user.Status != common.UserStatusEnabled || user.OrganizationId > 0 {
			return ErrTossBillingUserInactive
		}

		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "user_id", "plan_id", "status", "auto_renew", "billing_key_id", "end_time", "next_billing_time", "billing_fail_count", "toss_renewal_contract_snapshot").
			Where("id = ?", renewalSubID).
			First(&sub).Error; err != nil {
			return err
		}
		if !sub.AutoRenew || sub.Status != "active" || sub.UserId != order.UserId || sub.PlanId != order.PlanId ||
			sub.BillingKeyId != order.BillingKeyId || sub.NextBillingTime != renewalBillingTime ||
			order.RenewalEndTime <= 0 || order.RenewalEndTime != sub.EndTime {
			return ErrSubscriptionOrderStatusInvalid
		}
		effectiveAttempt, err := resolveTossRenewalEffectiveAttemptTx(tx, sub.Id, renewalBillingTime, sub.BillingFailCount)
		if err != nil {
			return err
		}
		if effectiveAttempt != lockedAttempt {
			return ErrTossRenewalIdentityConflict
		}
		if !order.BillingAttempted &&
			(order.BillingChargeProtocolVersion != tossBillingChargeProtocolDurableAttempt || strings.TrimSpace(order.ProviderCredential) == "") {
			return ErrTossBillingCrossCredentialRetryUnsafe
		}
		now := getDBTimestampTx(tx)
		if sub.EndTime <= now-TossBillingOperationalGraceSeconds {
			return ErrTossBillingRenewalOutsideGrace
		}
		canceled, err := hasTossCancellationForSubscriptionTx(tx, &sub, order.BillingKeyId)
		if err != nil {
			return err
		}
		if canceled {
			if err := disableTossRenewalWithoutContractTx(tx, &sub); err != nil {
				return err
			}
			closed := tx.Model(&SubscriptionOrder{}).
				Where("id = ? AND status = ? AND billing_claim_token = ?", order.Id, common.TopUpStatusPending, token).
				Updates(map[string]interface{}{
					"status":              common.TopUpStatusExpired,
					"complete_time":       now,
					"billing_claim_token": "",
					"billing_claim_time":  0,
				})
			if closed.Error != nil {
				return closed.Error
			}
			if closed.RowsAffected != 1 {
				return ErrTossBillingClaimLost
			}
			if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, order.BillingKeyId); err != nil {
				return err
			}
			cancellationStopped = true
			return nil
		}
		contract, contractErr := loadOrBackfillTossRenewalContractTx(tx, &sub)
		if contractErr == nil {
			contractErr = ValidateTossRenewalOrderMatchesContract(&order, &sub)
		}
		if contractErr == nil {
			contractErr = ValidateTossSubscriptionBillingPlan(contract.Plan)
		}
		if contractErr != nil {
			if !isFatalTossRenewalContractError(contractErr) &&
				!errors.Is(contractErr, ErrTossSubscriptionBillingPeriodTooShort) {
				return contractErr
			}
			if err := disableTossRenewalWithoutContractTx(tx, &sub); err != nil {
				return err
			}
			closed := tx.Model(&SubscriptionOrder{}).
				Where("id = ? AND status = ? AND billing_claim_token = ?", order.Id, common.TopUpStatusPending, token).
				Updates(map[string]interface{}{
					"status":              common.TopUpStatusExpired,
					"complete_time":       now,
					"billing_claim_token": "",
					"billing_claim_time":  0,
				})
			if closed.Error != nil {
				return closed.Error
			}
			if closed.RowsAffected != 1 {
				return ErrTossBillingClaimLost
			}
			contractStoppedErr = contractErr
			return nil
		}

		if user.Id != sub.UserId {
			return ErrTossBillingUserInactive
		}

		var key UserBillingKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "user_id", "status").
			Where("id = ?", order.BillingKeyId).
			First(&key).Error; err != nil {
			return err
		}
		if key.UserId != order.UserId || key.Status != BillingKeyStatusActive {
			return ErrTossBillingKeyInactive
		}
		if !order.BillingAttempted && postBarrier.PolicyError != nil {
			return postBarrier.PolicyError
		}
		preserveAttemptCredential, err := preserveExactTossAttemptCredential(
			order.BillingAttemptCredential,
			secretKey,
			order.BillingAttempted,
		)
		if err != nil {
			return err
		}
		attemptCredential := credential
		if !preserveAttemptCredential && strings.TrimSpace(order.ProviderCredential) != "" {
			providerSecret, decryptErr := DecryptProviderCredential(order.ProviderCredential)
			if decryptErr != nil || strings.TrimSpace(providerSecret) == "" {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			if providerSecret == secretKey {
				// Reuse the exact bytes when the order-time snapshot already
				// names this fresh attempt's credential. A v1 order may instead
				// start under a newly active same-MID secret; the new ciphertext
				// then becomes authoritative before the provider POST.
				attemptCredential = order.ProviderCredential
			}
		}

		updates := map[string]interface{}{
			"billing_attempted": true,
			"billing_attempt_time": gorm.Expr(
				"CASE WHEN billing_attempt_time > 0 THEN billing_attempt_time ELSE ? END", now),
			"billing_claim_time": now,
		}
		if !preserveAttemptCredential {
			updates["billing_attempt_credential"] = attemptCredential
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id = ? AND billing_claim_token = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending, order.BillingKeyId, token).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// MySQL reports changed rows by default. A recovery claim acquired in
			// the same DB second can therefore make this guarded UPDATE a no-op
			// even though this worker still owns the exact order/credential. Re-read
			// the locked row and accept only the complete intended post-state;
			// conditional misses and credential changes still fail closed.
			var persisted SubscriptionOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", order.Id).First(&persisted).Error; err != nil {
				return err
			}
			persistedIdentity, _, identityErr := subscriptionOrderRenewalIdentity(&persisted)
			credentialMatches, credentialErr := preserveExactTossAttemptCredential(
				persisted.BillingAttemptCredential,
				secretKey,
				true,
			)
			if identityErr != nil ||
				!sameTossRenewalAttemptIdentity(persistedIdentity, tossRenewalAttemptIdentity{
					SubscriptionID: lockedSubID,
					BillingTime:    lockedBillingTime,
					Attempt:        lockedAttempt,
				}) ||
				persisted.TradeNo != tradeNo || persisted.PaymentProvider != PaymentProviderToss ||
				persisted.Status != common.TopUpStatusPending || persisted.BillingKeyId != order.BillingKeyId ||
				persisted.BillingClaimToken != token || !persisted.BillingAttempted ||
				persisted.BillingAttemptTime <= 0 || persisted.BillingClaimTime <= 0 ||
				credentialErr != nil || !credentialMatches {
				return ErrTossBillingClaimLost
			}
		} else if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
	if err != nil {
		return err
	}
	if cancellationStopped {
		return ErrTossBillingCancellationPrecedesCharge
	}
	if contractStoppedErr != nil {
		return contractStoppedErr
	}
	return nil
}

func GetClaimedTossRenewalAttemptSecret(tradeNo, token string) (string, error) {
	var order SubscriptionOrder
	if err := DB.Select("payment_provider", "status", "billing_claim_token", "billing_attempted", "billing_attempt_credential").
		Where("trade_no = ?", strings.TrimSpace(tradeNo)).First(&order).Error; err != nil {
		return "", err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending {
		return "", ErrSubscriptionOrderStatusInvalid
	}
	if order.BillingClaimToken != strings.TrimSpace(token) {
		return "", ErrTossBillingClaimLost
	}
	if !order.BillingAttempted || strings.TrimSpace(order.BillingAttemptCredential) == "" {
		return "", errors.New("Toss renewal attempt credential is unavailable")
	}
	secretKey, err := DecryptProviderCredential(order.BillingAttemptCredential)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secretKey) == "" {
		return "", errors.New("Toss renewal attempt credential is empty")
	}
	return secretKey, nil
}

// promoteTossRenewalChargeAttemptCredential records the next API-key
// namespace after the provider definitively rejected the current credential.
// This transition does not authorize a provider POST: the next loop iteration
// must still pass MarkTossRenewalChargeAttempt's complete lifecycle and
// operational checks immediately before calling Toss. Persisting the namespace
// separately prevents an operational-disable or process interruption between
// candidates from stranding recovery on the already-rejected credential.
func promoteTossRenewalChargeAttemptCredential(tradeNo, token, currentSecret, nextSecret string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	currentSecret = strings.TrimSpace(currentSecret)
	nextSecret = strings.TrimSpace(nextSecret)
	if tradeNo == "" || token == "" || currentSecret == "" || nextSecret == "" || currentSecret == nextSecret {
		return errors.New("invalid Toss renewal credential promotion")
	}
	credential, err := EncryptProviderCredential(nextSecret)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "payment_provider", "status", "billing_claim_token", "billing_attempted", "billing_attempt_credential").
			Where("trade_no = ?", tradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending || order.BillingClaimToken != token ||
			!order.BillingAttempted || strings.TrimSpace(order.BillingAttemptCredential) == "" {
			return ErrTossBillingClaimLost
		}
		persistedSecret, err := DecryptProviderCredential(order.BillingAttemptCredential)
		if err != nil {
			return err
		}
		if strings.TrimSpace(persistedSecret) != currentSecret {
			return ErrTossBillingClaimLost
		}
		updated := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND payment_provider = ? AND status = ? AND billing_claim_token = ? AND billing_attempted = ? AND billing_attempt_credential = ?",
				order.Id, PaymentProviderToss, common.TopUpStatusPending, token, true, order.BillingAttemptCredential).
			Updates(map[string]interface{}{
				"billing_attempt_credential": credential,
				"billing_attempted":          false,
				"billing_attempt_time":       0,
				"billing_claim_time":         getDBTimestampTx(tx),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
}

// GetClaimedTossSubscriptionFirstChargeAttemptSecret returns the exact secret
// used for a recoverable initial POST. Pre-marker orders are normalized from
// ProviderCredential when their claim is acquired. Once BillingAttempted is
// true, an empty dedicated credential is inconsistent and must fail closed.
func GetClaimedTossSubscriptionFirstChargeAttemptSecret(tradeNo, token string) (string, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return "", ErrTossBillingClaimLost
	}
	if strings.HasPrefix(tradeNo, TossRenewalTradeNoPrefix) || HasTossRenewalOpaqueOrderIDPrefix(tradeNo) {
		return "", errors.New("renewal order cannot use the initial-charge attempt")
	}
	if err := validateTossInitialChargeOrderIdentity(tradeNo); err != nil {
		return "", err
	}
	var order SubscriptionOrder
	if err := DB.Select(
		"payment_provider", "status", "billing_key_id", "billing_claim_token",
		"billing_attempted", "billing_attempt_credential", "provider_credential",
	).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending || order.BillingKeyId <= 0 {
		return "", ErrSubscriptionOrderStatusInvalid
	}
	if order.BillingClaimToken != token {
		return "", ErrTossBillingClaimLost
	}
	if !order.BillingAttempted {
		return "", errors.New("Toss initial charge was not attempted")
	}
	credential := strings.TrimSpace(order.BillingAttemptCredential)
	if credential == "" {
		return "", ErrTossBillingCrossCredentialRetryUnsafe
	}
	secretKey, err := DecryptProviderCredential(credential)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secretKey) == "" {
		return "", errors.New("Toss initial charge attempt credential is empty")
	}
	return secretKey, nil
}

func chargeClaimedTossRenewal(ctx context.Context, token, billingKey, customerKey string, secretKeys []string, orderId, orderName string, amount int64) (*TossBillingChargeResult, bool, error) {
	if tossBillingCharger == nil {
		return nil, false, errors.New("toss billing charger not configured")
	}
	var lastResult *TossBillingChargeResult
	var lastErr error
	lastErrorFromProviderPOST := false
	for i, secretKey := range secretKeys {
		if err := RequireFreshTossConfig(); err != nil {
			return lastResult, false, fmt.Errorf("%w: %v", ErrTossBillingOperationallyDisabled, err)
		}
		if err := MarkTossRenewalChargeAttempt(orderId, token, secretKey); err != nil {
			return nil, false, err
		}
		result, err := tossBillingCharger(ctx, billingKey, customerKey, secretKey, orderId, orderName, amount)
		lastErrorFromProviderPOST = true
		if err == nil {
			return result, true, nil
		}
		lastResult = result
		lastErr = err
		if isTossBillingAmbiguousChargeError(err) || i == len(secretKeys)-1 || !isTossBillingCredentialError(err) {
			break
		}
		// The current credential was rejected definitively, so no payment was
		// created in its API-key namespace. Record the next namespace before the
		// next final authorization gate; that gate may legitimately stop on an
		// operational kill switch or lifecycle change, but recovery must no longer
		// query/retry the credential that Toss already rejected.
		if promoteErr := promoteTossRenewalChargeAttemptCredential(orderId, token, secretKey, secretKeys[i+1]); promoteErr != nil {
			return lastResult, false, promoteErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Toss billing secret is unavailable")
	}
	return lastResult, lastErrorFromProviderPOST, lastErr
}

func validateTossBillingIssueSnapshotInput(authKey, customerKey string) error {
	if authKey == "" {
		return fmt.Errorf("%w: authKey is empty", ErrTossBillingIssueSnapshotInvalid)
	}
	if len(authKey) > TossBillingIssueAuthKeyMaxBytes {
		return fmt.Errorf("%w: authKey exceeds %d bytes", ErrTossBillingIssueSnapshotInvalid, TossBillingIssueAuthKeyMaxBytes)
	}
	customerKey = strings.TrimSpace(customerKey)
	if customerKey == "" {
		return fmt.Errorf("%w: customerKey is empty", ErrTossBillingIssueSnapshotInvalid)
	}
	if len(customerKey) > TossBillingIssueCustomerKeyMaxBytes {
		return fmt.Errorf("%w: customerKey exceeds %d bytes", ErrTossBillingIssueSnapshotInvalid, TossBillingIssueCustomerKeyMaxBytes)
	}
	return nil
}

// ClaimTossSubscriptionBillingIssue snapshots the one-time authKey before any
// provider request and atomically acquires the cross-node issue claim. A
// different authorization can never replace an existing recoverable snapshot.
func ClaimTossSubscriptionBillingIssue(tradeNo, authKey, customerKey string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	customerKey = strings.TrimSpace(customerKey)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
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
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL)",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Where("(billing_issue_auth_key_hash = '' OR billing_issue_auth_key_hash IS NULL OR billing_issue_auth_key_hash = ?)", authKeyHash).
		Where("(billing_issue_customer_key = '' OR billing_issue_customer_key IS NULL OR billing_issue_customer_key = ?)", customerKey).
		Updates(map[string]interface{}{
			"billing_claim_token":         token,
			"billing_claim_time":          now,
			"billing_issue_auth_key":      encryptedAuthKey,
			"billing_issue_auth_key_hash": authKeyHash,
			"billing_issue_customer_key":  customerKey,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusSuccess || order.BillingKeyId > 0 {
		return "", false, nil
	}
	if order.Status != common.TopUpStatusPending {
		return "", false, ErrSubscriptionOrderStatusInvalid
	}
	if (order.BillingIssueAuthKeyHash != "" && order.BillingIssueAuthKeyHash != authKeyHash) ||
		(order.BillingIssueCustomerKey != "" && order.BillingIssueCustomerKey != customerKey) {
		return "", false, ErrTossBillingIssueConflict
	}
	return "", false, nil
}

// ClaimStoredTossSubscriptionBillingIssue lets a background worker recover a
// stale issue claim without accepting replacement browser credentials.
func ClaimStoredTossSubscriptionBillingIssue(tradeNo string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL)",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Where("billing_issue_auth_key <> ''").
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusSuccess || order.BillingKeyId > 0 {
		return "", false, nil
	}
	if order.Status != common.TopUpStatusPending {
		return "", false, ErrSubscriptionOrderStatusInvalid
	}
	return "", false, nil
}

// ClaimStoredTossSubscriptionBillingIssueCleanup acquires a lease for an
// already-attempted issue request whose local order has become terminal. The
// encrypted one-time authorization must remain recoverable until the same
// idempotent issue request yields the provider billing key and that key is
// durably queued for deletion.
func ClaimStoredTossSubscriptionBillingIssueCleanup(tradeNo string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_attempted = ? AND billing_issue_auth_key <> ''",
			tradeNo, PaymentProviderToss, []string{common.TopUpStatusFailed, common.TopUpStatusExpired}, true).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusSuccess || order.BillingKeyId > 0 || order.BillingIssueAuthKey == "" {
		return "", false, nil
	}
	if order.Status != common.TopUpStatusFailed && order.Status != common.TopUpStatusExpired {
		return "", false, ErrSubscriptionOrderStatusInvalid
	}
	return "", false, nil
}

// GetClaimedTossSubscriptionBillingIssueState reloads the authoritative row
// after claim acquisition. Callers must not make attempted/cleanup/rotation
// decisions from a pre-claim candidate: another node may have completed a
// MarkAttempted/Release cycle between candidate selection and this claim.
func GetClaimedTossSubscriptionBillingIssueState(tradeNo, token string) (*SubscriptionOrder, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return nil, ErrTossBillingClaimLost
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ?",
		tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTossBillingClaimLost
		}
		return nil, err
	}
	return &order, nil
}

// GetClaimedTossSubscriptionBillingIssue decrypts only the durable snapshot.
// It never falls back to the currently-active Toss secret, because doing so
// could retry the issue request in a different MID namespace.
func GetClaimedTossSubscriptionBillingIssue(tradeNo, token string) (authKey, customerKey, secretKey string, err error) {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return "", "", "", err
	}
	order, err := GetClaimedTossSubscriptionBillingIssueState(tradeNo, token)
	if err != nil {
		return "", "", "", err
	}
	authKey, err = common.DecryptString(order.BillingIssueAuthKey)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt authKey: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	customerKey = strings.TrimSpace(order.BillingIssueCustomerKey)
	if err := validateTossBillingIssueSnapshotInput(authKey, customerKey); err != nil {
		return "", "", "", err
	}
	if order.BillingIssueAuthKeyHash == "" || order.BillingIssueAuthKeyHash != common.GenerateHMAC(authKey) {
		return "", "", "", fmt.Errorf("%w: authKey integrity check failed", ErrTossBillingIssueSnapshotInvalid)
	}
	secretKey, err = DecryptProviderCredential(order.ProviderCredential)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt provider credential: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	if strings.TrimSpace(secretKey) == "" {
		return "", "", "", fmt.Errorf("%w: stored provider credential is empty", ErrTossBillingIssueSnapshotInvalid)
	}
	return authKey, customerKey, secretKey, nil
}

// GetClaimedTossSubscriptionBillingIssueCleanup is the terminal-order variant
// of GetClaimedTossSubscriptionBillingIssue. It is lookup/delete-only: callers
// must never attach the recovered key or charge it.
func GetClaimedTossSubscriptionBillingIssueCleanup(tradeNo, token string) (authKey, customerKey, secretKey string, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return "", "", "", ErrTossBillingClaimLost
	}
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return "", "", "", err
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ? AND payment_provider = ? AND status IN ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_attempted = ? AND billing_claim_token = ?",
		tradeNo, PaymentProviderToss, []string{common.TopUpStatusFailed, common.TopUpStatusExpired}, true, token).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", "", ErrTossBillingClaimLost
		}
		return "", "", "", err
	}
	authKey, err = common.DecryptString(order.BillingIssueAuthKey)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt authKey: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	customerKey = strings.TrimSpace(order.BillingIssueCustomerKey)
	if err := validateTossBillingIssueSnapshotInput(authKey, customerKey); err != nil {
		return "", "", "", err
	}
	if order.BillingIssueAuthKeyHash == "" || order.BillingIssueAuthKeyHash != common.GenerateHMAC(authKey) {
		return "", "", "", fmt.Errorf("%w: authKey integrity check failed", ErrTossBillingIssueSnapshotInvalid)
	}
	secretKey, err = DecryptProviderCredential(order.ProviderCredential)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: decrypt provider credential: %v", ErrTossBillingIssueSnapshotInvalid, err)
	}
	if strings.TrimSpace(secretKey) == "" {
		return "", "", "", fmt.Errorf("%w: stored provider credential is empty", ErrTossBillingIssueSnapshotInvalid)
	}
	return authKey, customerKey, secretKey, nil
}

// FinishClaimedTossSubscriptionBillingIssueCleanup clears the sensitive
// authorization and request-time provider credential only after its provider
// key has been durably queued for revocation. Pending orders are closed;
// already-terminal orders keep their existing terminal status.
func FinishClaimedTossSubscriptionBillingIssueCleanup(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status == common.TopUpStatusSuccess || order.BillingKeyId > 0 {
			return ErrSubscriptionOrderStatusInvalid
		}
		if order.BillingClaimToken != token || !order.BillingIssueAttempted || order.BillingIssueAuthKey == "" {
			return ErrTossBillingClaimLost
		}
		updates := map[string]interface{}{
			"billing_claim_token":         "",
			"billing_claim_time":          0,
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
			"billing_issue_attempted":     false,
			"provider_credential":         "",
			"provider_client_key_hash":    "",
		}
		if order.Status == common.TopUpStatusPending {
			updates["status"] = common.TopUpStatusExpired
			updates["complete_time"] = getDBTimestampTx(tx)
		} else if order.Status != common.TopUpStatusFailed && order.Status != common.TopUpStatusExpired {
			return ErrSubscriptionOrderStatusInvalid
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND billing_claim_token = ?", order.Id, token).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
}

func MarkTossSubscriptionBillingIssueAttempt(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTBilling)
		if err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss || order.Status != common.TopUpStatusPending ||
			order.BillingKeyId != 0 || order.BillingClaimToken != token || order.BillingIssueAuthKey == "" {
			return ErrTossBillingClaimLost
		}
		if order.BillingIssueAttempted {
			return nil
		}
		if postBarrier.PolicyError != nil {
			return postBarrier.PolicyError
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND billing_issue_attempted = ?", order.Id, false).
			Updates(map[string]interface{}{
				"billing_issue_attempted": true,
				"billing_claim_time":      getDBTimestampTx(tx),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
}

// TransitionClaimedTossSubscriptionBillingIssueToCleanup atomically removes a
// pending ISSUE request from every attach/charge path while preserving the
// exact authorization snapshot and claim needed to recover and delete a key
// whose provider response may already exist. This is the ownership fence that
// must run before an issued key is replayed or revoked: a stale worker must not
// terminalize a newer lease owner's order or delete the key that owner attached.
func TransitionClaimedTossSubscriptionBillingIssueToCleanup(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	now := GetDBTimestamp()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ?", tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).
		Where("billing_issue_attempted = ? AND billing_issue_auth_key <> '' AND billing_issue_auth_key_hash <> '' AND billing_issue_customer_key <> '' AND provider_credential <> ''", true).
		Updates(map[string]interface{}{
			"status":             common.TopUpStatusExpired,
			"complete_time":      now,
			"billing_claim_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var order SubscriptionOrder
	if err := DB.Select("payment_provider").Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTossBillingClaimLost
		}
		return err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	return ErrTossBillingClaimLost
}

// PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection moves a
// claimed ISSUE request into a rotated API-key namespace only after the caller
// received a definitive authentication rejection from the exact persisted
// credential. The new credential is stored before another provider POST and
// its attempt marker is reset in the same transaction. The caller must pass
// MarkTossSubscriptionBillingIssueAttempt again immediately before POSTing it;
// a crash or configuration gate failure therefore leaves a durable, safely
// unattempted promoted namespace instead of an ambiguous request.
//
// This function deliberately requires the active client key as well as the
// secret. The durable client-key fingerprint must prove that the rotation stays
// within the original MID; an empty/legacy fingerprint or a different MID is
// never eligible for promotion.
func PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
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
		return ErrTossBillingClaimLost
	}
	nextCredential, err := EncryptProviderCredential(nextSecret)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "payment_provider", "status", "billing_key_id", "billing_claim_token", "billing_issue_auth_key", "billing_issue_attempted", "provider_credential", "provider_client_key_hash").
			Where("trade_no = ?", tradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending || order.BillingKeyId != 0 ||
			order.BillingClaimToken != token || !order.BillingIssueAttempted ||
			strings.TrimSpace(order.BillingIssueAuthKey) == "" ||
			!IsValidTossClientKeyFingerprint(order.ProviderClientKeyHash) ||
			order.ProviderClientKeyHash != nextClientKeyHash {
			return ErrTossBillingClaimLost
		}
		persistedSecret, err := DecryptProviderCredential(order.ProviderCredential)
		if err != nil {
			return err
		}
		if strings.TrimSpace(persistedSecret) != rejectedSecret {
			return ErrTossBillingClaimLost
		}
		updated := tx.Model(&SubscriptionOrder{}).
			Where("id = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ? AND billing_issue_attempted = ? AND provider_credential = ? AND provider_client_key_hash = ?",
				order.Id, PaymentProviderToss, common.TopUpStatusPending, token, true, order.ProviderCredential, order.ProviderClientKeyHash).
			Updates(map[string]interface{}{
				"provider_credential":     nextCredential,
				"billing_issue_attempted": false,
				"billing_claim_time":      getDBTimestampTx(tx),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
}

// ExpireClaimedTossSubscriptionBillingIssue is the definitive-failure path.
// It clears both the one-time authorization and request-time API credential
// only while the caller still owns a pending order with no attached key.
func ExpireClaimedTossSubscriptionBillingIssue(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return ErrTossBillingClaimLost
	}
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ?",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).
		Updates(map[string]interface{}{
			"status":                      common.TopUpStatusExpired,
			"complete_time":               GetDBTimestamp(),
			"billing_claim_token":         "",
			"billing_claim_time":          0,
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
			"billing_issue_attempted":     false,
			"provider_credential":         "",
			"provider_client_key_hash":    "",
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	if order.Status != common.TopUpStatusPending {
		return nil
	}
	return ErrTossBillingClaimLost
}

// ClaimTossSubscriptionBillingOrder is the cross-process gate in front of
// billing-key issuance. Only one worker can move a pending order with no
// attached key into the provider-call section. A stale claim can be recovered
// with the same Toss idempotency key after the provider timeout window.
func ClaimTossSubscriptionBillingOrder(tradeNo string) (token string, claimed bool, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", false, errors.New("tradeNo is empty")
	}
	now := GetDBTimestamp()
	token = common.GetUUID()
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id = 0",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Where("(billing_claim_token = '' OR billing_claim_token IS NULL OR billing_claim_time <= ?)", now-tossSubscriptionBillingClaimTTLSeconds).
		Updates(map[string]interface{}{
			"billing_claim_token": token,
			"billing_claim_time":  now,
		})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return token, true, nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return "", false, err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return "", false, ErrPaymentMethodMismatch
	}
	if order.Status == common.TopUpStatusPending || order.Status == common.TopUpStatusSuccess {
		return "", false, nil
	}
	return "", false, ErrSubscriptionOrderStatusInvalid
}

// ReleaseTossSubscriptionBillingClaim only releases the caller's own claim.
// It is safe after a status transition and is a no-op if another worker has
// already recovered a stale claim.
func ReleaseTossSubscriptionBillingClaim(tradeNo, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || token == "" {
		return nil
	}
	return DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND billing_claim_token = ?", tradeNo, token).
		Updates(map[string]interface{}{
			"billing_claim_token": "",
			// Keep the last attempt time on every unresolved row for fair background
			// scheduling. Successfully settled rows no longer participate and can
			// discard it.
			"billing_claim_time": gorm.Expr(
				"CASE WHEN status = ? THEN 0 ELSE billing_claim_time END", common.TopUpStatusSuccess),
		}).Error
}

func AttachTossBillingKeyToClaimedOrder(tradeNo string, billingKeyID int, token string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	if tradeNo == "" || billingKeyID <= 0 || token == "" {
		return errors.New("invalid Toss billing-key claim attachment")
	}
	result := DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ?",
			tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).
		Updates(map[string]interface{}{
			"billing_key_id":              billingKeyID,
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
			"billing_issue_attempted":     false,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var order SubscriptionOrder
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	if order.PaymentProvider != PaymentProviderToss {
		return ErrPaymentMethodMismatch
	}
	if order.BillingKeyId == billingKeyID && (order.Status == common.TopUpStatusPending || order.Status == common.TopUpStatusSuccess) {
		return nil
	}
	if order.BillingClaimToken != token {
		return ErrTossBillingClaimLost
	}
	return ErrSubscriptionOrderStatusInvalid
}

// StoreAndAttachClaimedTossSubscriptionBillingKey closes the lifecycle race
// between issuing a provider key and linking it to the local order. The user
// row is locked before the key is stored, so account-disable transactions that
// use the same lock either run first (and block the attach) or run afterward
// (and see the newly stored key and move it to pending_revocation).
func StoreAndAttachClaimedTossSubscriptionBillingKey(
	tradeNo, token, customerKey, billingKey, cardCompany, cardMasked, secretKey, providerClientKeyHash string,
) (billingKeyID int, err error) {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	customerKey = strings.TrimSpace(customerKey)
	billingKey = strings.TrimSpace(billingKey)
	if tradeNo == "" || token == "" || customerKey == "" || billingKey == "" {
		return 0, errors.New("invalid claimed Toss subscription billing key")
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		user, lockErr := lockTossSubscriptionOrderUserTx(tx, tradeNo)
		if lockErr != nil {
			return lockErr
		}
		var order SubscriptionOrder
		if queryErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).
			First(&order).Error; queryErr != nil {
			return queryErr
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		if user.Id != order.UserId || user.Status != common.UserStatusEnabled || user.OrganizationId > 0 {
			return ErrTossBillingUserInactive
		}
		if order.BillingKeyId > 0 || order.BillingClaimToken != token {
			return ErrTossBillingClaimLost
		}
		if strings.TrimSpace(order.BillingIssueAuthKey) == "" {
			return ErrTossBillingIssueSnapshotInvalid
		}
		if order.BillingIssueCustomerKey != customerKey {
			return ErrTossBillingIssueConflict
		}
		if order.CreateTime <= getDBTimestampTx(tx)-TossSubscriptionPurchaseReservationMaxAgeSeconds {
			// The provider response must still be cleaned up by the caller, but an
			// authorization from an old checkout may never become a live key.
			return ErrTossBillingIssueOutsideRecoveryWindow
		}
		plan, planErr := resolveSubscriptionOrderPlanTx(tx, &order)
		if planErr != nil {
			return planErr
		}
		if planErr := ValidateTossSubscriptionBillingPlan(plan); planErr != nil {
			return planErr
		}

		// Never reactivate a provider key that another lifecycle path already
		// queued for deletion. Active duplicates are safe to reuse only when
		// their encrypted value and MID fingerprint match this issue response.
		var existingRows []UserBillingKey
		if queryErr := tx.Where(
			"user_id = ? AND customer_key = ? AND billing_key_hash = ?",
			order.UserId, customerKey, tossBillingKeyHash(billingKey),
		).Order("id asc").Find(&existingRows).Error; queryErr != nil {
			return queryErr
		}
		expectedClientHash := providerClientKeyHash
		if expectedClientHash != "" && !IsValidTossClientKeyFingerprint(expectedClientHash) {
			return ErrTossBillingMIDMismatch
		}
		for i := range existingRows {
			row := &existingRows[i]
			billingKeyID = row.Id
			if row.Status != BillingKeyStatusActive {
				return ErrTossBillingKeyInactive
			}
			plain, decryptErr := common.DecryptString(row.EncryptedKey)
			if decryptErr != nil || plain != billingKey {
				return errors.New("stored Toss billing key integrity check failed")
			}
			if storedHash := row.ProviderClientKeyHash; IsValidTossClientKeyFingerprint(storedHash) && storedHash != expectedClientHash {
				return ErrTossBillingMIDMismatch
			}
		}

		billingKeyID, err = storeTossBillingKeyWithSecretStatusTx(
			tx,
			order.UserId,
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
		var stored UserBillingKey
		if queryErr := tx.Select("id", "user_id", "status").First(&stored, billingKeyID).Error; queryErr != nil {
			return queryErr
		}
		if stored.UserId != order.UserId || stored.Status != BillingKeyStatusActive {
			return ErrTossBillingKeyInactive
		}

		result := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_claim_token = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending, token).
			Updates(map[string]interface{}{
				"billing_key_id":              billingKeyID,
				"billing_claim_time":          getDBTimestampTx(tx),
				"billing_issue_auth_key":      "",
				"billing_issue_auth_key_hash": "",
				"billing_issue_customer_key":  "",
				"billing_issue_attempted":     false,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
	return billingKeyID, err
}

// ValidateClaimedTossSubscriptionFirstCharge is the final local authorization
// check immediately before the first provider charge. It rejects a user or key
// disabled after issue/attach and records a claim-guarded charge-intent marker
// while retaining the claim until the caller has either charged or cleaned up
// the issued provider key.
func ValidateClaimedTossSubscriptionFirstCharge(tradeNo, token string, billingKeyID int, secretKey string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	token = strings.TrimSpace(token)
	secretKey = strings.TrimSpace(secretKey)
	if tradeNo == "" || token == "" || billingKeyID <= 0 || secretKey == "" {
		return ErrTossBillingClaimLost
	}
	credential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		postBarrier, err := inspectTossProviderPOSTBarrierTx(tx, tossProviderPOSTBilling)
		if err != nil {
			return err
		}
		user, err := lockTossSubscriptionOrderUserTx(tx, tradeNo)
		if err != nil {
			return err
		}
		var order SubscriptionOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", tradeNo).
			First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending || order.BillingKeyId != billingKeyID {
			return ErrSubscriptionOrderStatusInvalid
		}
		if user.Id != order.UserId || user.Status != common.UserStatusEnabled || user.OrganizationId > 0 {
			return ErrTossBillingUserInactive
		}
		if order.BillingClaimToken != token {
			return ErrTossBillingClaimLost
		}
		if !order.BillingAttempted && postBarrier.PolicyError != nil {
			return postBarrier.PolicyError
		}
		if !order.BillingAttempted {
			if order.BillingChargeProtocolVersion != tossBillingChargeProtocolLegacy &&
				order.BillingChargeProtocolVersion != tossBillingChargeProtocolDurableAttempt {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
			if strings.TrimSpace(order.ProviderCredential) == "" {
				return ErrTossBillingCrossCredentialRetryUnsafe
			}
		}
		if order.CreateTime <= getDBTimestampTx(tx)-TossSubscriptionPurchaseReservationMaxAgeSeconds {
			return ErrTossBillingIssueOutsideRecoveryWindow
		}
		plan, planErr := resolveSubscriptionOrderPlanTx(tx, &order)
		if planErr != nil {
			return planErr
		}
		if planErr := ValidateTossSubscriptionBillingPlan(plan); planErr != nil {
			return planErr
		}

		var key UserBillingKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "user_id", "status").
			Where("id = ?", billingKeyID).
			First(&key).Error; err != nil {
			return err
		}
		if key.UserId != order.UserId || key.Status != BillingKeyStatusActive {
			return ErrTossBillingKeyInactive
		}
		preserveAttemptCredential, err := preserveExactTossAttemptCredential(
			order.BillingAttemptCredential,
			secretKey,
			order.BillingAttempted,
		)
		if err != nil {
			return err
		}
		attemptCredential := credential
		if !preserveAttemptCredential && strings.TrimSpace(order.ProviderCredential) != "" {
			if _, err := preserveExactTossAttemptCredential(order.ProviderCredential, secretKey, false); err != nil {
				return err
			}
			attemptCredential = order.ProviderCredential
		}
		attemptTime := getDBTimestampTx(tx)
		updates := map[string]interface{}{
			"billing_attempted": true,
			"billing_attempt_time": gorm.Expr(
				"CASE WHEN billing_attempt_time > 0 THEN billing_attempt_time ELSE ? END", attemptTime),
			"billing_claim_time": attemptTime,
		}
		if !preserveAttemptCredential {
			updates["billing_attempt_credential"] = attemptCredential
		}
		intent := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_key_id = ? AND billing_claim_token = ?",
				tradeNo, PaymentProviderToss, common.TopUpStatusPending, billingKeyID, token).
			Updates(updates)
		if intent.Error != nil {
			return intent.Error
		}
		if intent.RowsAffected == 0 {
			// See MarkTossRenewalChargeAttempt: MySQL can report zero for the
			// correct same-second recovery state. Only the exact claimed,
			// non-renewal post-state is an idempotent success.
			var persisted SubscriptionOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", order.Id).First(&persisted).Error; err != nil {
				return err
			}
			_, _, _, isRenewal, identityErr := ResolveTossRenewalOrderIdentity(&persisted)
			credentialMatches, credentialErr := preserveExactTossAttemptCredential(
				persisted.BillingAttemptCredential,
				secretKey,
				true,
			)
			if identityErr != nil || isRenewal || persisted.TradeNo != tradeNo ||
				persisted.PaymentProvider != PaymentProviderToss || persisted.Status != common.TopUpStatusPending ||
				persisted.BillingKeyId != billingKeyID || persisted.BillingClaimToken != token ||
				!persisted.BillingAttempted || persisted.BillingAttemptTime <= 0 || persisted.BillingClaimTime <= 0 ||
				credentialErr != nil || !credentialMatches {
				return ErrTossBillingClaimLost
			}
		} else if intent.RowsAffected != 1 {
			return ErrTossBillingClaimLost
		}
		return nil
	})
}

// StoreTossBillingKey encrypts and persists a billing key, returning its row id.
func StoreTossBillingKey(userId int, customerKey, billingKey, cardCompany, cardMasked string) (int, error) {
	return StoreTossBillingKeyWithSecret(userId, customerKey, billingKey, cardCompany, cardMasked, "")
}

func StoreTossBillingKeyWithSecret(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusActive, "")
}

func StoreTossBillingKeyWithProviderSnapshot(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey, providerClientKeyHash string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusActive, providerClientKeyHash)
}

func StoreTossBillingKeyPendingRevocationWithSecret(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusPendingRevocation, "")
}

// StoreTossBillingKeyPendingRevocationWithProviderSnapshot persists an issued
// key that could not be attached to its owner. The explicit MID fingerprint is
// important during configuration rotation: the issue-time secret may no
// longer appear in the active configuration by the time cleanup is queued.
func StoreTossBillingKeyPendingRevocationWithProviderSnapshot(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey, providerClientKeyHash string) (int, error) {
	return storeTossBillingKeyWithSecretStatus(userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, BillingKeyStatusPendingRevocation, providerClientKeyHash)
}

// GetTossProviderClientKeyHashByTradeNo resolves the MID namespace captured
// before billing-key issuance. Both subscription orders and wallet policy auth
// flows use their trade number as the idempotency key.
func GetTossProviderClientKeyHashByTradeNo(tradeNo string) (string, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return "", nil
	}
	if DB.Migrator().HasTable(&SubscriptionOrder{}) {
		var order SubscriptionOrder
		err := DB.Select("provider_client_key_hash").Where("trade_no = ?", tradeNo).First(&order).Error
		if err == nil {
			if order.ProviderClientKeyHash == "" || IsValidTossClientKeyFingerprint(order.ProviderClientKeyHash) {
				return order.ProviderClientKeyHash, nil
			}
			return "", fmt.Errorf("%w: subscription order has an invalid Toss MID fingerprint", ErrTossBillingMIDMismatch)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
	}
	if !DB.Migrator().HasTable(&WalletAutoRecharge{}) {
		return "", nil
	}
	var policy WalletAutoRecharge
	err := DB.Select("provider_client_key_hash").Where("auth_trade_no = ?", tradeNo).First(&policy).Error
	if err == nil {
		if policy.ProviderClientKeyHash == "" || IsValidTossClientKeyFingerprint(policy.ProviderClientKeyHash) {
			return policy.ProviderClientKeyHash, nil
		}
		return "", fmt.Errorf("%w: wallet policy has an invalid Toss MID fingerprint", ErrTossBillingMIDMismatch)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return "", err
}

func storeTossBillingKeyWithSecretStatus(userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey, status, providerClientKeyHash string) (int, error) {
	return storeTossBillingKeyWithSecretStatusTx(nil, userId, customerKey, billingKey, cardCompany, cardMasked, secretKey, status, providerClientKeyHash)
}

// storeTossBillingKeyWithSecretStatusTx is the transaction-aware form used by
// workflows that must not expose an unlinked active provider key between the
// key-row insert and its owning order/policy update.
func storeTossBillingKeyWithSecretStatusTx(tx *gorm.DB, userId int, customerKey, billingKey, cardCompany, cardMasked, secretKey, status, providerClientKeyHash string) (int, error) {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return 0, err
	}
	db := DB
	if tx != nil {
		db = tx
	}
	customerKey = strings.TrimSpace(customerKey)
	if !isValidStoredTossCustomerKey(customerKey) {
		return 0, errors.New("invalid Toss customer key for local storage")
	}
	if !IsValidTossBillingKey(billingKey) {
		return 0, errors.New("invalid Toss billing key for local storage")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = BillingKeyStatusActive
	}
	if providerClientKeyHash == "" {
		snapshot := setting.GetTossConfigSnapshot()
		// A non-empty secret is evidence for an MID only when it still maps to
		// exactly one configured client key. In particular, never label a
		// pending cleanup row with the *currently active* MID merely because its
		// issue-time secret has already rotated out of configuration.
		if strings.TrimSpace(secretKey) != "" {
			providerClientKeyHash = tossConfiguredClientHashForExactSecret(snapshot, secretKey)
		} else if status == BillingKeyStatusActive {
			// Legacy callers that omit the secret mean "use the active billing
			// credentials". Preserve that compatibility for active rows only.
			providerClientKeyHash = tossBillingClientKeyHash(tossActiveBillingClientKey(snapshot))
		}
	} else if !IsValidTossClientKeyFingerprint(providerClientKeyHash) {
		return 0, errors.New("invalid Toss provider client-key fingerprint")
	}
	enc, err := common.EncryptString(billingKey)
	if err != nil {
		return 0, err
	}
	credential, err := EncryptProviderCredential(secretKey)
	if err != nil {
		return 0, err
	}
	billingKeyHash := tossBillingKeyHash(billingKey)
	// A worker can crash after storing the idempotently-issued key but before
	// attaching it to the order. Reusing that exact row on recovery avoids
	// accumulating local duplicates. Cleanup-only storage must also reuse an
	// existing active identity: its caller performs the live-reference check
	// before DELETE, so inserting a pending duplicate would leave a permanently
	// pending row beside a still-valid recurring contract.
	var existingRows []UserBillingKey
	lookup := db.Where(
		"user_id = ? AND customer_key = ? AND billing_key_hash = ?",
		userId, customerKey, billingKeyHash,
	).Order("id asc")
	if tx != nil {
		lookup = lookup.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if lookupErr := lookup.Find(&existingRows).Error; lookupErr != nil {
		return 0, lookupErr
	}
	for i := range existingRows {
		existing := &existingRows[i]
		plain, decryptErr := common.DecryptString(existing.EncryptedKey)
		if decryptErr != nil || plain != billingKey {
			continue
		}
		storedMID := existing.ProviderClientKeyHash
		if IsValidTossClientKeyFingerprint(storedMID) && IsValidTossClientKeyFingerprint(providerClientKeyHash) && storedMID != providerClientKeyHash {
			continue
		}
		if status == BillingKeyStatusActive {
			if existing.Status != BillingKeyStatusActive {
				// A retry worker may already be deleting this exact provider key.
				// Creating a new active duplicate would let the old worker delete the
				// key underneath the newly activated recurring policy.
				return 0, ErrTossBillingKeyInactive
			}
			return existing.Id, nil
		}
		if status == BillingKeyStatusPendingRevocation && existing.Status != BillingKeyStatusRevoked {
			return existing.Id, nil
		}
	}
	row := &UserBillingKey{
		UserId:                userId,
		CustomerKey:           customerKey,
		EncryptedKey:          enc,
		BillingKeyHash:        billingKeyHash,
		ProviderCredential:    credential,
		ProviderClientKeyHash: providerClientKeyHash,
		CardCompany:           cardCompany,
		CardNumberMasked:      cardMasked,
		Status:                status,
		CreateTime:            common.GetTimestamp(),
	}
	if err := db.Create(row).Error; err != nil {
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

// GetTossBillingKeyPlainWithSecretCandidates returns secrets in the order in
// which a charge should try them. For keys created after MID fingerprinting was
// introduced, the current secret is preferred only when the active client key
// belongs to the same MID; the encrypted issue-time secret is retained as a
// rotation fallback. A configured different MID is rejected for charging.
func GetTossBillingKeyPlainWithSecretCandidates(id int) (key string, customerKey string, secretKeys []string, err error) {
	return getTossBillingKeyPlainWithSecretCandidatesTx(nil, id, true, true)
}

func getTossBillingKeyPlainTx(tx *gorm.DB, id int) (key string, customerKey string, err error) {
	key, customerKey, _, err = getTossBillingKeyPlainWithSecretTx(tx, id)
	return key, customerKey, err
}

func getTossBillingKeyPlainWithSecretTx(tx *gorm.DB, id int) (key string, customerKey string, secretKey string, err error) {
	key, customerKey, secretKeys, err := getTossBillingKeyPlainWithSecretCandidatesTx(tx, id, true, false)
	if err != nil {
		return "", "", "", err
	}
	if len(secretKeys) > 0 {
		secretKey = secretKeys[0]
	}
	return key, customerKey, secretKey, nil
}

func appendUniqueTossSecret(candidates []string, secret string) []string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return candidates
	}
	for _, candidate := range candidates {
		if candidate == secret {
			return candidates
		}
	}
	return append(candidates, secret)
}

// prioritizeTossStoredCredentialSecret keeps a recurring provider POST in the
// exact API-key namespace that was persisted before the order became visible.
// Toss scopes an idempotency key by API key as well as method and URL, so a
// rotated same-MID secret must be a fallback rather than the first fresh POST.
// General billing-key operations intentionally keep their current-secret-first
// ordering and call this helper only at the recurring-order boundary.
func prioritizeTossStoredCredentialSecret(candidates []string, providerCredential string) ([]string, error) {
	providerCredential = strings.TrimSpace(providerCredential)
	if providerCredential == "" {
		return candidates, nil
	}
	storedSecret, err := DecryptProviderCredential(providerCredential)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(storedSecret) == "" {
		return nil, ErrTossBillingCrossCredentialRetryUnsafe
	}
	ordered := make([]string, 0, len(candidates)+1)
	ordered = appendUniqueTossSecret(ordered, storedSecret)
	for _, candidate := range candidates {
		ordered = appendUniqueTossSecret(ordered, candidate)
	}
	return ordered, nil
}

func tossConfiguredClientHashForExactSecret(snapshot setting.TossConfigSnapshot, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	matchedHash := ""
	for _, pair := range []struct {
		client string
		secret string
	}{
		{snapshot.ClientKey, snapshot.SecretKey},
		{snapshot.TestClientKey, snapshot.TestSecretKey},
		{snapshot.BillingClientKey, snapshot.BillingSecretKey},
		{snapshot.BillingTestClientKey, snapshot.BillingTestSecretKey},
	} {
		if secret != strings.TrimSpace(pair.secret) {
			continue
		}
		candidateHash := tossBillingClientKeyHash(pair.client)
		if candidateHash == "" {
			continue
		}
		if matchedHash != "" && matchedHash != candidateHash {
			// The same secret configured for multiple MIDs is ambiguous; do not use
			// it as evidence that a legacy row belongs to the active namespace.
			return ""
		}
		matchedHash = candidateHash
	}
	return matchedHash
}

func backfillLegacyTossBillingKeyMIDFingerprintTx(db *gorm.DB, rowID int, storedCredential, storedSecret, currentHash string, snapshot setting.TossConfigSnapshot) (string, error) {
	if IsValidTossClientKeyFingerprint(currentHash) {
		return currentHash, nil
	}
	inferredHash := tossConfiguredClientHashForExactSecret(snapshot, storedSecret)
	if inferredHash == "" {
		return currentHash, nil
	}
	updated, err := backfillTossMIDFingerprintCASTx(db, &UserBillingKey{}, rowID, storedCredential, currentHash, inferredHash)
	if err != nil {
		return "", err
	}
	if updated {
		return inferredHash, nil
	}

	// Another worker may have filled the legacy field concurrently. Honor the
	// persisted value instead of allowing a stale configuration snapshot to
	// override or temporarily mask it in this request.
	var persisted UserBillingKey
	if err := db.Select("provider_client_key_hash").Where("id = ?", rowID).Take(&persisted).Error; err != nil {
		return "", err
	}
	return persisted.ProviderClientKeyHash, nil
}

// BackfillLegacyTossBillingKeyMIDFingerprints snapshots the current Toss
// configuration and records an MID only when a row's encrypted issue-time
// secret exactly and uniquely maps to one configured client key. Unknown,
// corrupt, or ambiguous legacy credentials are intentionally left blank.
//
// This is run before credential namespace changes so a secret-only rotation
// retains proof that an existing billing key belongs to the same stable MID.
func BackfillLegacyTossBillingKeyMIDFingerprints() error {
	if DB == nil || !DB.Migrator().HasTable(&UserBillingKey{}) {
		return nil
	}
	snapshot, err := GetFreshTossConfigSnapshot()
	if err != nil {
		return err
	}
	_, err = backfillLegacyTossProviderMIDFingerprintsBatched(snapshot, snapshot.Revision, tossLegacyMIDBackfillTables{billingKeys: true})
	return err
}

const tossLegacyMIDBackfillBatchSize = 500
const tossLegacyMIDBackfillWriteBatchSize = 100

type tossLegacyMIDBackfillTables struct {
	billingKeys        bool
	topUps             bool
	subscriptionOrders bool
	walletPolicies     bool
}

type tossLegacyProviderCredentialRow struct {
	Id                    int
	ProviderCredential    string
	ProviderClientKeyHash string
}

func tossInvalidMIDFingerprintPredicate() string {
	const column = "provider_client_key_hash"
	switch {
	case common.UsingPostgreSQL:
		return column + ` IS NULL OR ` + column + ` !~ '^[0-9a-f]{64}$'`
	case common.UsingMySQL:
		// MySQL's ordinary varchar comparison/REGEXP follows the column collation,
		// which is commonly case-insensitive. CAST AS BINARY keeps uppercase hash
		// corruption out of the trusted canonical lowercase namespace.
		return column + ` IS NULL OR NOT (CAST(` + column + ` AS BINARY) REGEXP '^[0-9a-f]{64}$')`
	case common.UsingSQLite:
		// SQLite GLOB is byte/case-sensitive. The negated class catches lowercase
		// 64-byte non-hex values that a length-only prefilter would miss.
		return column + ` IS NULL OR LENGTH(` + column + `) <> 64 OR ` + column + ` GLOB '*[^0-9a-f]*'`
	default:
		// Unknown test dialectors remain fail-safe at the cost of scanning more.
		return "1 = 1"
	}
}

func backfillTossMIDFingerprintCASTx(tx *gorm.DB, tableModel any, rowID int, currentCredential, currentHash, inferredHash string) (bool, error) {
	if tx == nil || rowID <= 0 || !IsValidTossClientKeyFingerprint(inferredHash) {
		return false, errors.New("invalid Toss MID fingerprint backfill")
	}
	updated := false
	err := tx.Transaction(func(locked *gorm.DB) error {
		var persisted tossLegacyProviderCredentialRow
		if err := locked.Model(tableModel).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "provider_credential", "provider_client_key_hash").
			Where("id = ?", rowID).
			Take(&persisted).Error; err != nil {
			return err
		}
		// Compare the exact bytes in Go while holding the row lock. MySQL's usual
		// case-insensitive varchar collation cannot provide an exact-value CAS for
		// uppercase corruption with a plain SQL equality predicate.
		if persisted.ProviderCredential != currentCredential || persisted.ProviderClientKeyHash != currentHash {
			return nil
		}
		result := locked.Model(tableModel).Where("id = ?", rowID).
			Update("provider_client_key_hash", inferredHash)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Toss MID fingerprint backfill update was not persisted")
		}
		updated = true
		return nil
	})
	return updated, err
}

// backfillLegacyTossMIDFingerprintsForModelTx assigns or repairs an MID only
// when the encrypted issue/attempt secret maps exactly and uniquely to one of
// the pre-rotation credential pairs. Empty and malformed fingerprints are both
// non-evidence. Rows that cannot be proved keep their exact stored value and
// are counted for operational visibility; every recovery path must then remain
// on the exact stored credential.
func backfillLegacyTossMIDFingerprintsForModelTx(tx *gorm.DB, tableModel any, snapshot setting.TossConfigSnapshot, scope string, scopeArgs ...any) (int64, error) {
	lastID := 0
	var unresolved int64
	for {
		var windowIDs []int
		if err := tx.Model(tableModel).
			Select("id").
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(tossLegacyMIDBackfillBatchSize).
			Pluck("id", &windowIDs).Error; err != nil {
			return 0, err
		}
		if len(windowIDs) == 0 {
			break
		}
		windowEndID := windowIDs[len(windowIDs)-1]
		var rows []tossLegacyProviderCredentialRow
		query := tx.Model(tableModel).
			Select("id", "provider_credential", "provider_client_key_hash").
			Where("id > ? AND id <= ?", lastID, windowEndID).
			Where("provider_credential IS NOT NULL AND provider_credential <> ?", "").
			Where("(" + tossInvalidMIDFingerprintPredicate() + ")")
		if scope != "" {
			query = query.Where(scope, scopeArgs...)
		}
		if err := query.Order("id ASC").Limit(tossLegacyMIDBackfillBatchSize).Find(&rows).Error; err != nil {
			return 0, err
		}
		for i := range rows {
			if IsValidTossClientKeyFingerprint(rows[i].ProviderClientKeyHash) {
				continue
			}
			secret, err := DecryptProviderCredential(rows[i].ProviderCredential)
			if err != nil || strings.TrimSpace(secret) == "" {
				unresolved++
				continue
			}
			inferredHash := tossConfiguredClientHashForExactSecret(snapshot, secret)
			if inferredHash == "" {
				unresolved++
				continue
			}
			updated, err := backfillTossMIDFingerprintCASTx(tx, tableModel, rows[i].Id, rows[i].ProviderCredential, rows[i].ProviderClientKeyHash, inferredHash)
			if err != nil {
				return 0, err
			}
			if !updated {
				var persisted tossLegacyProviderCredentialRow
				if err := tx.Model(tableModel).Select("provider_client_key_hash").Where("id = ?", rows[i].Id).Take(&persisted).Error; err != nil {
					return 0, err
				}
				if !IsValidTossClientKeyFingerprint(persisted.ProviderClientKeyHash) {
					unresolved++
				}
			}
		}
		lastID = windowEndID
	}

	return unresolved, nil
}

type tossLegacyMIDBackfillTarget struct {
	enabled   bool
	model     any
	scope     string
	scopeArgs []any
}

func tossLegacyMIDBackfillTargets(tables tossLegacyMIDBackfillTables) []tossLegacyMIDBackfillTarget {
	return []tossLegacyMIDBackfillTarget{
		{enabled: tables.billingKeys, model: &UserBillingKey{}},
		{
			enabled:   tables.topUps,
			model:     &TopUp{},
			scope:     "(payment_provider = ? OR payment_method = ?)",
			scopeArgs: []any{PaymentProviderToss, PaymentMethodToss},
		},
		{
			enabled:   tables.subscriptionOrders,
			model:     &SubscriptionOrder{},
			scope:     "(payment_provider = ? OR payment_method = ?)",
			scopeArgs: []any{PaymentProviderToss, PaymentMethodToss},
		},
		{enabled: tables.walletPolicies, model: &WalletAutoRecharge{}},
	}
}

// tossMIDBackfillBatchCommittedHook is test-only instrumentation for proving
// that a concurrent option writer between batches cannot let the stale writer
// commit its final credential generation.
var tossMIDBackfillBatchCommittedHook func()

func backfillLegacyTossMIDFingerprintsForModelBatched(snapshot setting.TossConfigSnapshot, expectedRevision string, tableModel any, scope string, scopeArgs ...any) (int64, error) {
	lastID := 0
	var unresolved int64
	cutoffID := 0
	if err := DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentRevision) != strings.TrimSpace(expectedRevision) {
			return ErrTossConfigRevisionStale
		}
		return tx.Model(tableModel).Select("COALESCE(MAX(id), 0)").Scan(&cutoffID).Error
	}); err != nil {
		return 0, err
	}
	if cutoffID <= 0 {
		return 0, nil
	}
	for {
		// The hash column is intentionally not required to be indexed. Advance by
		// a fixed-size primary-key window first, then apply the invalid-hash
		// predicate only inside that window. This bounds every scan even when a
		// table contains millions of already-valid historical rows.
		var windowIDs []int
		if err := DB.Model(tableModel).
			Select("id").
			Where("id > ? AND id <= ?", lastID, cutoffID).
			Order("id ASC").
			Limit(tossLegacyMIDBackfillWriteBatchSize).
			Pluck("id", &windowIDs).Error; err != nil {
			return unresolved, err
		}
		if len(windowIDs) == 0 {
			return unresolved, nil
		}
		windowEndID := windowIDs[len(windowIDs)-1]
		var rows []tossLegacyProviderCredentialRow
		query := DB.Model(tableModel).
			Select("id", "provider_credential", "provider_client_key_hash").
			Where("id > ? AND id <= ?", lastID, windowEndID).
			Where("provider_credential IS NOT NULL AND provider_credential <> ?", "").
			Where("(" + tossInvalidMIDFingerprintPredicate() + ")")
		if scope != "" {
			query = query.Where(scope, scopeArgs...)
		}
		if err := query.Order("id ASC").Limit(tossLegacyMIDBackfillWriteBatchSize).Find(&rows).Error; err != nil {
			return unresolved, err
		}
		type preparedBackfill struct {
			row          tossLegacyProviderCredentialRow
			inferredHash string
		}
		prepared := make([]preparedBackfill, 0, len(rows))
		for i := range rows {
			// The SQL predicate is only an optimization. Keep the byte-exact Go
			// guard as the correctness boundary for unknown dialects/collations.
			if IsValidTossClientKeyFingerprint(rows[i].ProviderClientKeyHash) {
				continue
			}
			secret, err := DecryptProviderCredential(rows[i].ProviderCredential)
			if err != nil || strings.TrimSpace(secret) == "" {
				unresolved++
				continue
			}
			inferredHash := tossConfiguredClientHashForExactSecret(snapshot, secret)
			if inferredHash == "" {
				unresolved++
				continue
			}
			prepared = append(prepared, preparedBackfill{row: rows[i], inferredHash: inferredHash})
		}

		if len(prepared) > 0 {
			if err := DB.Transaction(func(tx *gorm.DB) error {
				currentRevision, err := lockTossOptionRowsTx(tx)
				if err != nil {
					return err
				}
				if strings.TrimSpace(currentRevision) != strings.TrimSpace(expectedRevision) {
					return ErrTossConfigRevisionStale
				}
				for i := range prepared {
					updated, err := backfillTossMIDFingerprintCASTx(
						tx,
						tableModel,
						prepared[i].row.Id,
						prepared[i].row.ProviderCredential,
						prepared[i].row.ProviderClientKeyHash,
						prepared[i].inferredHash,
					)
					if err != nil {
						return err
					}
					if !updated {
						var persisted tossLegacyProviderCredentialRow
						if err := tx.Model(tableModel).Select("provider_client_key_hash").Where("id = ?", prepared[i].row.Id).Take(&persisted).Error; err != nil {
							return err
						}
						if !IsValidTossClientKeyFingerprint(persisted.ProviderClientKeyHash) {
							unresolved++
						}
					}
				}
				return nil
			}); err != nil {
				return unresolved, err
			}
		}
		if tossMIDBackfillBatchCommittedHook != nil {
			tossMIDBackfillBatchCommittedHook()
		}
		lastID = windowEndID
	}
}

func backfillLegacyTossProviderMIDFingerprintsBatched(snapshot setting.TossConfigSnapshot, expectedRevision string, tables tossLegacyMIDBackfillTables) (int64, error) {
	if strings.TrimSpace(expectedRevision) == "" {
		return 0, ErrTossConfigRevisionStale
	}
	var unresolved int64
	for _, target := range tossLegacyMIDBackfillTargets(tables) {
		if !target.enabled {
			continue
		}
		count, err := backfillLegacyTossMIDFingerprintsForModelBatched(snapshot, expectedRevision, target.model, target.scope, target.scopeArgs...)
		if err != nil {
			return unresolved, err
		}
		unresolved += count
	}
	return unresolved, nil
}

func tossBillingSecretCandidates(row *UserBillingKey, storedSecret string, requireActiveMID bool) ([]string, error) {
	snapshot := setting.GetTossConfigSnapshot()
	clientKey := snapshot.BillingClientKey
	currentSecret := snapshot.BillingSecretKey
	if snapshot.TestMode {
		clientKey = snapshot.BillingTestClientKey
		currentSecret = snapshot.BillingTestSecretKey
	}
	clientKey = strings.TrimSpace(clientKey)
	currentSecret = strings.TrimSpace(currentSecret)
	currentClientHash := tossBillingClientKeyHash(clientKey)
	storedClientHash := row.ProviderClientKeyHash
	storedClientHashValid := IsValidTossClientKeyFingerprint(storedClientHash)
	if storedClientHash == "" {
		storedClientHash = tossConfiguredClientHashForExactSecret(snapshot, storedSecret)
		storedClientHashValid = IsValidTossClientKeyFingerprint(storedClientHash)
	}

	if requireActiveMID && storedClientHashValid && currentClientHash != storedClientHash {
		return nil, ErrTossBillingMIDMismatch
	}

	candidates := make([]string, 0, 2)
	if storedClientHashValid && currentClientHash == storedClientHash {
		candidates = appendUniqueTossSecret(candidates, currentSecret)
	}
	candidates = appendUniqueTossSecret(candidates, storedSecret)
	if len(candidates) == 0 {
		return nil, errors.New("stored Toss billing credential is unavailable")
	}
	return candidates, nil
}

func getTossBillingKeyPlainWithSecretCandidatesTx(tx *gorm.DB, id int, requireActive bool, requireActiveMID bool) (key string, customerKey string, secretKeys []string, err error) {
	db := DB
	if tx != nil {
		db = tx
	}
	var row UserBillingKey
	if err = db.Where("id = ?", id).First(&row).Error; err != nil {
		return "", "", nil, err
	}
	if requireActive && row.Status != BillingKeyStatusActive {
		return "", "", nil, errors.New("billing key revoked")
	}
	plain, err := common.DecryptString(row.EncryptedKey)
	if err != nil {
		return "", "", nil, err
	}
	secret, err := DecryptProviderCredential(row.ProviderCredential)
	if err != nil {
		return "", "", nil, err
	}
	if !IsValidTossClientKeyFingerprint(row.ProviderClientKeyHash) && strings.TrimSpace(secret) != "" {
		row.ProviderClientKeyHash, err = backfillLegacyTossBillingKeyMIDFingerprintTx(db, row.Id, row.ProviderCredential, secret, row.ProviderClientKeyHash, setting.GetTossConfigSnapshot())
		if err != nil {
			return "", "", nil, err
		}
	}
	secretKeys, err = tossBillingSecretCandidates(&row, secret, requireActiveMID)
	if err != nil {
		return "", "", nil, err
	}
	return plain, row.CustomerKey, secretKeys, nil
}

func deactivateTossBillingKeyLinksTx(tx *gorm.DB, id int) error {
	if id <= 0 {
		return nil
	}
	now := getDBTimestampTx(tx)
	if tx.Migrator().HasTable(&UserSubscription{}) {
		if err := tx.Model(&UserSubscription{}).
			Where("billing_key_id = ? AND auto_renew = ?", id, true).
			Updates(map[string]interface{}{
				"auto_renew":         false,
				"next_billing_time":  0,
				"billing_retry_time": 0,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}
	}
	if !tx.Migrator().HasTable(&WalletAutoRecharge{}) {
		return nil
	}
	if err := deactivateWalletAutoRechargePoliciesForLifecycleTx(tx, "billing_key_id = ?", id); err != nil {
		return err
	}
	// Keep the explicit timestamp write for rows that were already terminal and
	// therefore outside the lifecycle helper's active/pending scope.
	return tx.Model(&WalletAutoRecharge{}).
		Where("billing_key_id = ? AND status = ?", id, WalletAutoRechargeStatusCancelled).
		Update("update_time", now).Error
}

// setTossBillingKeyStatusOnlyTx changes the provider-key lifecycle without
// changing any subscription or wallet contract. Callers use it after proving
// that no live reference remains, after Toss authoritatively deleted the key,
// or to restore a legacy pending identity when a live reference blocks DELETE.
func setTossBillingKeyStatusOnlyTx(tx *gorm.DB, id int, status string) error {
	if tx == nil || id <= 0 {
		return nil
	}
	query := tx.Model(&UserBillingKey{}).Where("id = ?", id)
	if status == BillingKeyStatusRevoked {
		return query.Updates(map[string]interface{}{
			"status":                BillingKeyStatusRevoked,
			"encrypted_key":         "",
			"provider_credential":   "",
			"revocation_retry_time": 0,
		}).Error
	}
	if status == BillingKeyStatusActive {
		return query.
			Where("status <> ?", BillingKeyStatusRevoked).
			Updates(map[string]interface{}{
				"status":                BillingKeyStatusActive,
				"revocation_retry_time": 0,
			}).Error
	}
	return query.
		Where("status <> ?", BillingKeyStatusRevoked).
		Update("status", status).Error
}

func reactivatePendingTossBillingKeyIdentityRowsTx(tx *gorm.DB, rows []UserBillingKey) error {
	for i := range rows {
		if rows[i].Status != BillingKeyStatusPendingRevocation {
			continue
		}
		if err := setTossBillingKeyStatusOnlyTx(tx, rows[i].Id, BillingKeyStatusActive); err != nil {
			return err
		}
	}
	return nil
}

// queueTossBillingKeyRevocationIfUnreferencedTx separates ending one recurring
// contract from deleting the provider billing key. Identity rows are locked
// before the reference check, which serializes with attachment paths that lock
// an existing key before linking it. A live subscription, wallet policy, or
// pending subscription charge keeps every provider-identity row usable.
func queueTossBillingKeyRevocationIfUnreferencedTx(tx *gorm.DB, billingKeyID int) (bool, error) {
	if tx == nil || billingKeyID <= 0 {
		return false, nil
	}
	rows, err := getTossBillingKeyIdentityRowsTx(tx, billingKeyID, true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Legacy or manually repaired orders can retain a stale surrogate ID.
			// Ending that contract must not roll back its settlement merely because
			// there is no provider-key row left to delete.
			return false, nil
		}
		return false, err
	}
	identityIDs := tossBillingKeyIdentityIDs(rows)
	if len(identityIDs) == 0 {
		return false, gorm.ErrRecordNotFound
	}
	live, err := tossBillingKeysHaveOtherLiveReferencesTx(tx, identityIDs, 0)
	if err != nil {
		return false, err
	}
	if live {
		// A legacy cleanup path may already have marked one duplicate identity
		// pending. A live contract means provider DELETE is no longer authorized,
		// so restore those local identity rows to the usable lifecycle state.
		if err := reactivatePendingTossBillingKeyIdentityRowsTx(tx, rows); err != nil {
			return false, err
		}
		return false, nil
	}
	for _, identityID := range identityIDs {
		if err := setTossBillingKeyStatusOnlyTx(tx, identityID, BillingKeyStatusPendingRevocation); err != nil {
			return false, err
		}
	}
	return true, nil
}

// QueueTossBillingKeyRevocationIfUnreferenced queues a provider DELETE only
// after the selected contract has already removed its own live reference.
func QueueTossBillingKeyRevocationIfUnreferenced(tx *gorm.DB, billingKeyID int) (bool, error) {
	if billingKeyID <= 0 {
		return false, nil
	}
	if tx != nil {
		return queueTossBillingKeyRevocationIfUnreferencedTx(tx, billingKeyID)
	}
	queued := false
	err := DB.Transaction(func(inner *gorm.DB) error {
		var reference UserBillingKey
		if err := inner.Select("user_id").Where("id = ?", billingKeyID).First(&reference).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if err := lockTossBillingOwnerTx(inner, reference.UserId); err != nil {
			return err
		}
		var err error
		queued, err = queueTossBillingKeyRevocationIfUnreferencedTx(inner, billingKeyID)
		return err
	})
	return queued, err
}

func setTossBillingKeyStatusTx(tx *gorm.DB, id int, status string) error {
	if id <= 0 {
		return nil
	}
	// Charge authorization gates serialize user -> subscription/policy -> key.
	// Take the same owner lock before changing a key and then disabling its
	// links; otherwise a revocation worker could hold key -> subscription while
	// a charge worker holds subscription -> key on MySQL/PostgreSQL.
	if tx.Migrator().HasTable(&User{}) {
		var reference UserBillingKey
		if err := tx.Select("user_id").Where("id = ?", id).First(&reference).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if reference.UserId > 0 {
			var owner User
			if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id").Where("id = ?", reference.UserId).First(&owner).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
	}
	if err := setTossBillingKeyStatusOnlyTx(tx, id, status); err != nil {
		return err
	}
	return deactivateTossBillingKeyLinksTx(tx, id)
}

// RevokeTossBillingKey marks a billing key revoked and disables every local
// recurring-payment path linked to it.
func RevokeTossBillingKey(tx *gorm.DB, id int) error {
	if tx != nil {
		return setTossBillingKeyStatusTx(tx, id, BillingKeyStatusRevoked)
	}
	return DB.Transaction(func(inner *gorm.DB) error {
		return setTossBillingKeyStatusTx(inner, id, BillingKeyStatusRevoked)
	})
}

// MarkTossBillingKeyPendingRevocation keeps a key out of future charges while
// preserving enough state to retry remote deletion at Toss.
func MarkTossBillingKeyPendingRevocation(tx *gorm.DB, id int) error {
	if tx != nil {
		return setTossBillingKeyStatusTx(tx, id, BillingKeyStatusPendingRevocation)
	}
	return DB.Transaction(func(inner *gorm.DB) error {
		return setTossBillingKeyStatusTx(inner, id, BillingKeyStatusPendingRevocation)
	})
}

func appendPositiveUniqueInt(values []int, value int) []int {
	if value <= 0 {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func markTossBillingKeyIDsPendingTx(tx *gorm.DB, keyIDs []int) error {
	for _, keyID := range keyIDs {
		if err := setTossBillingKeyStatusTx(tx, keyID, BillingKeyStatusPendingRevocation); err != nil {
			return err
		}
	}
	return nil
}

// DeactivateTossBillingForUser is the local half of account disable/delete.
// It intentionally performs no network I/O so callers can use it inside their
// user transaction. Pending keys are deleted remotely by the retry task.
func DeactivateTossBillingForUser(tx *gorm.DB, userID int) error {
	if userID <= 0 {
		return nil
	}
	if tx == nil {
		return DB.Transaction(func(inner *gorm.DB) error {
			return DeactivateTossBillingForUser(inner, userID)
		})
	}
	// Some callers already hold this row as part of account lifecycle, while
	// scheduler cleanup enters through the nil-tx wrapper. Always establish the
	// same owner-first order before touching subscriptions, wallet policies, or
	// provider keys; re-locking the row in the same transaction is harmless.
	if err := lockTossBillingOwnerTx(tx, userID); err != nil {
		return err
	}

	var keyIDs []int
	hasBillingKeys := tx.Migrator().HasTable(&UserBillingKey{})
	hasSubscriptions := tx.Migrator().HasTable(&UserSubscription{})
	hasWalletPolicies := tx.Migrator().HasTable(&WalletAutoRecharge{})
	if hasBillingKeys {
		if err := tx.Model(&UserBillingKey{}).
			Where("user_id = ? AND status <> ?", userID, BillingKeyStatusRevoked).
			Pluck("id", &keyIDs).Error; err != nil {
			return err
		}
	}
	var subscriptionKeyIDs []int
	if hasSubscriptions {
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND billing_key_id > 0", userID).
			Pluck("billing_key_id", &subscriptionKeyIDs).Error; err != nil {
			return err
		}
	}
	for _, keyID := range subscriptionKeyIDs {
		keyIDs = appendPositiveUniqueInt(keyIDs, keyID)
	}
	organizationIDs := make([]int, 0)
	if hasWalletPolicies {
		var walletReferences []struct {
			TargetType   string
			TargetId     int
			BillingKeyId int
		}
		if err := tx.Model(&WalletAutoRecharge{}).
			Select("target_type", "target_id", "billing_key_id").
			Where("owner_user_id = ?", userID).
			Order("target_id asc, billing_key_id asc, id asc").
			Find(&walletReferences).Error; err != nil {
			return err
		}
		for i := range walletReferences {
			if walletReferences[i].TargetType == TopUpTargetTypeOrganization {
				organizationIDs = appendPositiveUniqueInt(organizationIDs, walletReferences[i].TargetId)
			}
			keyIDs = appendPositiveUniqueInt(keyIDs, walletReferences[i].BillingKeyId)
		}
	}
	sort.Ints(organizationIDs)
	sort.Ints(keyIDs)
	// Account shutdown can race organization shutdown for the same wallet.
	// Extend the owner-first order through every affected organization and key
	// before mutating subscription/policy rows: owner -> org -> key -> policy.
	// This is compatible with organization lifecycle's org -> key -> policy and
	// avoids a policy/key cycle without ever acquiring an owner after an org.
	if tx.Migrator().HasTable(&Organization{}) {
		for _, organizationID := range organizationIDs {
			var organization Organization
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id").Where("id = ?", organizationID).First(&organization).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
	}
	if hasBillingKeys {
		for _, keyID := range keyIDs {
			if _, err := getTossBillingKeyIdentityRowsTx(tx, keyID, true); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
	}

	now := common.GetTimestamp()
	if hasSubscriptions {
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND auto_renew = ?", userID, true).
			Updates(map[string]interface{}{
				"auto_renew":         false,
				"next_billing_time":  0,
				"billing_retry_time": 0,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}
	}
	if hasWalletPolicies {
		if err := deactivateWalletAutoRechargePoliciesForLifecycleTx(tx, "owner_user_id = ?", userID); err != nil {
			return err
		}
	}
	if !hasBillingKeys {
		return nil
	}
	return markTossBillingKeyIDsPendingTx(tx, keyIDs)
}

// DeactivatePersonalTossBillingForOrganizationJoin closes personal recurring
// billing before the membership becomes visible. Provider-attempted charge
// orders remain pending for GET-only recovery/compensation; orders with no
// charge POST evidence are expired immediately. Attempted ISSUE snapshots stay
// recoverable on the terminal row so the same idempotent key can be discovered
// and revoked by the cleanup worker.
func DeactivatePersonalTossBillingForOrganizationJoin(tx *gorm.DB, userID int) error {
	if userID <= 0 {
		return nil
	}
	if tx == nil {
		return DB.Transaction(func(inner *gorm.DB) error {
			return DeactivatePersonalTossBillingForOrganizationJoin(inner, userID)
		})
	}
	if err := DeactivateTossBillingForUser(tx, userID); err != nil {
		return err
	}
	// A final provider-attempt marker authorizes the exact idempotent request
	// even if the worker has not reached the network call yet. Do not expose the
	// organization membership until that request has reached a terminal state;
	// otherwise a personal charge could land after the personal wallet vanished.
	hasSubscriptionOrders := tx.Migrator().HasTable(&SubscriptionOrder{})
	hasTopUps := tx.Migrator().HasTable(&TopUp{})
	if hasSubscriptionOrders {
		var inFlightSubscriptionCount int64
		if err := tx.Model(&SubscriptionOrder{}).
			Where("user_id = ? AND payment_provider = ? AND status = ?",
				userID, PaymentProviderToss, common.TopUpStatusPending).
			Where("billing_attempted = ? OR billing_key_id > 0 OR billing_attempt_time > 0 OR billing_attempt_credential <> '' OR provider_payload <> '' OR "+
				"renewal_order_id_version <> ? OR renewal_subscription_id IS NOT NULL OR renewal_billing_time IS NOT NULL OR renewal_attempt IS NOT NULL OR trade_no LIKE ? ESCAPE '!'",
				true, 0, tossRenewalOpaqueOrderIDLikePattern()).
			Count(&inFlightSubscriptionCount).Error; err != nil {
			return err
		}
		if inFlightSubscriptionCount > 0 {
			return ErrPersonalTossBillingInFlight
		}
	}
	if hasTopUps {
		var inFlightTopUpCount int64
		now := getDBTimestampTx(tx)
		// Older wallet auto-recharge workers persisted the deterministic TopUp
		// before POST, but recorded no durable provider-attempt marker until a
		// successful response returned. An ambiguous timeout therefore looks the
		// same as an unattempted row after these columns are added with zero-value
		// defaults. Keep every pending wallet_auto_* order behind the membership
		// barrier until its reconciler proves DONE or 404/terminal. Literal
		// underscores are escaped so unrelated trade numbers cannot match.
		walletAutoRechargePattern := "wallet!_auto!_%"
		walletOpaquePattern := tossWalletOpaqueOrderIDLikePattern()
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND payment_provider = ?", userID, PaymentProviderToss).
			Where("(target_type = ? OR target_type = '' OR target_type IS NULL) AND (target_id = ? OR target_id = 0 OR target_id IS NULL)",
				TopUpTargetTypeUser, userID).
			Where("(status = ? AND (trade_no LIKE ? ESCAPE '!' OR trade_no LIKE ? ESCAPE '!' OR "+
				"wallet_order_id_version <> ? OR wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL)) OR "+
				"(provider_attempted = ? AND (status = ? OR provider_claim_time > ?)) OR "+
				"(status = ? AND ((provider_order_id <> '' AND provider_order_id <> trade_no) OR provider_order_time > 0 OR "+
				"provider_payload <> '' OR provider_claim_token <> '' OR provider_claim_time > 0))",
				common.TopUpStatusPending, walletAutoRechargePattern, walletOpaquePattern, 0,
				true, common.TopUpStatusPending, now-walletAutoRechargeChargeClaimTTLSeconds, common.TopUpStatusPending).
			Count(&inFlightTopUpCount).Error; err != nil {
			return err
		}
		if inFlightTopUpCount > 0 {
			return ErrPersonalTossBillingInFlight
		}
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND payment_provider = ? AND status = ? AND provider_attempted = ?", userID, PaymentProviderToss, common.TopUpStatusPending, false).
			Where("(target_type = ? OR target_type = '' OR target_type IS NULL) AND (target_id = ? OR target_id = 0 OR target_id IS NULL)",
				TopUpTargetTypeUser, userID).
			Where("trade_no NOT LIKE ? ESCAPE '!'", walletAutoRechargePattern).
			Where("trade_no NOT LIKE ? ESCAPE '!'", walletOpaquePattern).
			Where("wallet_order_id_version = ?", 0).
			Where("wallet_auto_recharge_id IS NULL AND wallet_auto_recharge_cycle_key IS NULL AND wallet_auto_recharge_attempt IS NULL").
			Where("(provider_order_id = '' OR provider_order_id IS NULL OR provider_order_id = trade_no) AND " +
				"(provider_order_time = 0 OR provider_order_time IS NULL) AND (provider_payload = '' OR provider_payload IS NULL) AND " +
				"(provider_claim_token = '' OR provider_claim_token IS NULL) AND (provider_claim_time = 0 OR provider_claim_time IS NULL)").
			Updates(map[string]interface{}{
				"status":                   common.TopUpStatusExpired,
				"complete_time":            now,
				"provider_credential":      "",
				"provider_client_key_hash": "",
				"provider_claim_token":     "",
				"provider_claim_time":      0,
			}).Error; err != nil {
			return err
		}
	}
	now := getDBTimestampTx(tx)
	// A never-attempted ISSUE snapshot has no provider object to recover and may
	// be erased. Attempted snapshots are preserved on the expired row.
	if !hasSubscriptionOrders {
		return nil
	}
	if err := tx.Model(&SubscriptionOrder{}).
		Where("user_id = ? AND payment_provider = ? AND status = ? AND billing_attempted = ? AND billing_issue_attempted = ? AND "+
			"(billing_key_id = 0 OR billing_key_id IS NULL) AND (billing_attempt_time = 0 OR billing_attempt_time IS NULL) AND "+
			"(billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)",
			userID, PaymentProviderToss, common.TopUpStatusPending, false, false).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Where("renewal_order_id_version = ?", 0).
		Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL").
		Updates(map[string]interface{}{
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
		}).Error; err != nil {
		return err
	}
	return tx.Model(&SubscriptionOrder{}).
		Where("user_id = ? AND payment_provider = ? AND status = ? AND billing_attempted = ? AND "+
			"(billing_key_id = 0 OR billing_key_id IS NULL) AND (billing_attempt_time = 0 OR billing_attempt_time IS NULL) AND "+
			"(billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)",
			userID, PaymentProviderToss, common.TopUpStatusPending, false).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalTradeNoLikePattern()).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Where("renewal_order_id_version = ?", 0).
		Where("renewal_subscription_id IS NULL AND renewal_billing_time IS NULL AND renewal_attempt IS NULL").
		Updates(map[string]interface{}{
			"status":              common.TopUpStatusExpired,
			"complete_time":       now,
			"billing_claim_token": "",
			"billing_claim_time":  0,
		}).Error
}

// DeactivateTossBillingForOrganization cancels organization-targeted wallet
// policies. It is used for organization disable/delete and owner changes.
func DeactivateTossBillingForOrganization(tx *gorm.DB, organizationID int) error {
	if organizationID <= 0 {
		return nil
	}
	if tx == nil {
		return DB.Transaction(func(inner *gorm.DB) error {
			return DeactivateTossBillingForOrganization(inner, organizationID)
		})
	}
	// Public nil-tx callers do not arrive with the organization row locked.
	// Always establish the lifecycle root lock here so every entry point uses
	// org -> key -> policy. A deleted organization needs no lock, but cleanup of
	// its surviving financial rows must still be allowed to finish.
	if tx.Migrator().HasTable(&Organization{}) {
		var organization Organization
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ?", organizationID).First(&organization).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if err := prepareOrganizationTossTopUpsForLifecycleTx(tx, organizationID); err != nil {
		return err
	}

	var keyIDs []int
	if err := tx.Model(&WalletAutoRecharge{}).
		Where("target_type = ? AND target_id = ? AND billing_key_id > 0", TopUpTargetTypeOrganization, organizationID).
		Pluck("billing_key_id", &keyIDs).Error; err != nil {
		return err
	}
	uniqueKeyIDs := make([]int, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		uniqueKeyIDs = appendPositiveUniqueInt(uniqueKeyIDs, keyID)
	}
	sort.Ints(uniqueKeyIDs)
	// Lock every affected provider-key identity in deterministic order before
	// changing wallet policy rows, yielding org -> key(sorted) -> policy.
	// BILLING_DELETED uses owner -> key -> policy, while charge authorization is
	// serialized by the same owner/organization roots before policy -> key. Do
	// not acquire a billing-key owner here: doing so after the organization lock
	// would invert the charge path's owner -> organization order.
	for _, keyID := range uniqueKeyIDs {
		if _, err := getTossBillingKeyIdentityRowsTx(tx, keyID, true); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if err := deactivateWalletAutoRechargePoliciesForLifecycleTx(tx, "target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationID); err != nil {
		return err
	}
	for _, keyID := range uniqueKeyIDs {
		// Organization shutdown ends only organization-owned policies. The same
		// provider billing key may still authorize a personal subscription or
		// wallet policy owned by the payer, so do not force-disable those links.
		if _, err := queueTossBillingKeyRevocationIfUnreferencedTx(tx, keyID); err != nil {
			return err
		}
	}
	return nil
}

func isTossBillingCredentialError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToUpper(err.Error())
	for _, marker := range []string{
		"INVALID_API_KEY",
		"UNAUTHORIZED_KEY",
		"INCORRECT_BASIC_AUTH_FORMAT",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func revokeTossBillingRemoteWithCandidates(ctx context.Context, billingKey string, secretKeys []string) error {
	var lastErr error
	for i, secretKey := range secretKeys {
		err := revokeTossBillingRemote(ctx, billingKey, secretKey)
		if err == nil || errors.Is(err, ErrTossBillingKeyAlreadyDeleted) {
			return nil
		}
		lastErr = err
		if i == len(secretKeys)-1 || !isTossBillingCredentialError(err) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Toss billing secret is unavailable")
	}
	return lastErr
}

// RevokeStoredTossBillingKey attempts provider DELETE only after proving that
// no live local contract still references the provider-key identity. Remote
// failure leaves the identity pending so the retry worker can safely try again.
func RevokeStoredTossBillingKey(ctx context.Context, billingKeyId int) error {
	if billingKeyId <= 0 {
		return nil
	}
	var stored UserBillingKey
	if err := DB.Select("id", "user_id", "status").Where("id = ?", billingKeyId).First(&stored).Error; err != nil {
		return err
	}
	if stored.Status == BillingKeyStatusRevoked {
		return RevokeTossBillingKey(nil, billingKeyId)
	}
	var identityIDs []int
	otherLiveReference := false
	if err := DB.Transaction(func(tx *gorm.DB) error {
		// Attachment paths serialize owner -> contract -> key. Take the owner
		// gate before identity-row locks to avoid a key -> owner deadlock with a
		// concurrent first-charge or wallet activation transaction.
		if err := lockTossBillingOwnerTx(tx, stored.UserId); err != nil {
			return err
		}
		rows, err := getTossBillingKeyIdentityRowsTx(tx, billingKeyId, true)
		if err != nil {
			return err
		}
		identityIDs = tossBillingKeyIdentityIDs(rows)
		otherLiveReference, err = tossBillingKeysHaveOtherLiveReferencesTx(tx, identityIDs, 0)
		if err != nil {
			return err
		}
		if otherLiveReference {
			return reactivatePendingTossBillingKeyIdentityRowsTx(tx, rows)
		}
		for _, identityID := range identityIDs {
			if err := setTossBillingKeyStatusOnlyTx(tx, identityID, BillingKeyStatusPendingRevocation); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if otherLiveReference {
		// Another subscription/policy still owns the same provider key through a
		// legacy duplicate row. Deleting it remotely would break that live owner.
		return nil
	}
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return err
	}
	billingKey, customerKey, secretKeys, err := getTossBillingKeyPlainWithSecretCandidatesTx(nil, billingKeyId, false, false)
	if err != nil {
		return err
	}
	if err := revokeTossBillingRemoteWithCandidates(ctx, billingKey, secretKeys); err != nil {
		return err
	}
	revoked, err := RevokeTossBillingKeyByPlainWithContext(ctx, customerKey, billingKey)
	if err != nil {
		return err
	}
	if !revoked {
		return RevokeTossBillingKey(nil, billingKeyId)
	}
	return nil
}

// RetryPendingTossBillingKeyRevocations retries remote deletion for billing keys
// that were disabled locally but could not be deleted at Toss during the original
// cancellation/failure path.
func RetryPendingTossBillingKeyRevocations(ctx context.Context, limit int) (int64, error) {
	if err := ValidateTossBillingCryptoConfiguration(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	var candidates []UserBillingKey
	if err := DB.Where("status = ?", BillingKeyStatusPendingRevocation).
		Where("revocation_retry_time = 0 OR revocation_retry_time IS NULL OR revocation_retry_time <= ?", now).
		Order("CASE WHEN revocation_retry_time IS NULL OR revocation_retry_time <= 0 THEN create_time ELSE revocation_retry_time END asc, id asc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	nextRetry := now + tossBillingRevocationRetryDelaySeconds
	reserved := make([]UserBillingKey, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		reserve := DB.Model(&UserBillingKey{}).
			Where("id = ? AND status = ?", candidate.Id, BillingKeyStatusPendingRevocation).
			Where("revocation_retry_time = ? OR (revocation_retry_time IS NULL AND ? = 0)", candidate.RevocationRetryTime, candidate.RevocationRetryTime).
			Update("revocation_retry_time", nextRetry)
		if reserve.Error != nil {
			return 0, reserve.Error
		}
		if reserve.RowsAffected != 1 {
			continue
		}
		reserved = append(reserved, *candidate)
	}
	outcomes := make(chan error, len(reserved))
	runBoundedTossMaintenance(reserved, func(candidate UserBillingKey) {
		if err := RevokeStoredTossBillingKey(ctx, candidate.Id); err != nil {
			common.SysError(fmt.Sprintf("failed to retry Toss billing key remote deletion: billing_key_id=%d error=%v", candidate.Id, err))
			outcomes <- err
			return
		}
		outcomes <- nil
	})
	close(outcomes)
	var revoked int64
	for err := range outcomes {
		if err == nil {
			revoked++
		}
	}
	return revoked, nil
}

// failTossRenewalAttempt closes exactly one provider-declined attempt and only
// then increments BillingFailCount. Subsequent policy retries therefore use a
// new tradeNo/idempotency key, while duplicate workers observing the same
// terminal order cannot count the same failure twice.
func failTossRenewalAttempt(subId int, tradeNo, claimToken string, maxFails int) (bool, error) {
	disabled := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTossSubscriptionOwnerTx(tx, subId); err != nil {
			return err
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("trade_no = ? AND payment_provider = ? AND status = ? AND billing_claim_token = ?", tradeNo, PaymentProviderToss, common.TopUpStatusPending, claimToken).
			Updates(map[string]interface{}{
				"status":              common.TopUpStatusFailed,
				"complete_time":       common.GetTimestamp(),
				"billing_claim_token": "",
				"billing_claim_time":  0,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var order SubscriptionOrder
			if err := tx.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
				return err
			}
			if order.PaymentProvider != PaymentProviderToss {
				return ErrPaymentMethodMismatch
			}
			// A concurrent worker already finalized this attempt. Its transaction
			// owns the single failure-count increment.
			if order.Status == common.TopUpStatusFailed || order.Status == common.TopUpStatusSuccess {
				return nil
			}
			if order.Status == common.TopUpStatusPending && order.BillingClaimToken != claimToken {
				return ErrTossBillingClaimLost
			}
			return ErrSubscriptionOrderStatusInvalid
		}
		var err error
		disabled, err = markTossBillingFailureTx(tx, subId, maxFails)
		return err
	})
	return disabled, err
}

// RevokeTossBillingKeyByPlain marks a locally stored key revoked after Toss reports
// that exact billingKey was deleted. The billingKey is encrypted at rest, so we
// decrypt candidate rows and compare in memory.
func RevokeTossBillingKeyByPlain(customerKey, billingKey string) (bool, error) {
	return RevokeTossBillingKeyByPlainWithContext(context.Background(), customerKey, billingKey)
}

func RevokeTossBillingKeyByPlainWithContext(ctx context.Context, customerKey, billingKey string) (bool, error) {
	billingKey = strings.TrimSpace(billingKey)
	if billingKey == "" {
		return false, nil
	}
	statuses := []string{BillingKeyStatusActive, BillingKeyStatusPendingRevocation}
	db := dbWithContext(ctx)
	hashQuery := db.Where("status IN ? AND billing_key_hash = ?", statuses, tossBillingKeyHash(billingKey))
	if strings.TrimSpace(customerKey) != "" {
		hashQuery = hashQuery.Where("customer_key = ?", strings.TrimSpace(customerKey))
	}
	var hashedRows []UserBillingKey
	if err := hashQuery.Find(&hashedRows).Error; err != nil {
		return false, err
	}
	keyIDs := make([]int, 0, len(hashedRows))
	matchedRows := make([]UserBillingKey, 0, len(hashedRows))
	for i := range hashedRows {
		keyIDs = appendPositiveUniqueInt(keyIDs, hashedRows[i].Id)
		matchedRows = append(matchedRows, hashedRows[i])
	}

	query := db.Where("status IN ? AND (billing_key_hash = '' OR billing_key_hash IS NULL)", statuses)
	if strings.TrimSpace(customerKey) != "" {
		query = query.Where("customer_key = ?", strings.TrimSpace(customerKey))
	}
	var rows []UserBillingKey
	if err := query.Find(&rows).Error; err != nil {
		return false, err
	}
	unresolvedRows := make([]UserBillingKey, 0)
	for i := range rows {
		plain, err := common.DecryptString(rows[i].EncryptedKey)
		if err != nil || !IsValidTossBillingKey(plain) {
			// Do not log ciphertext or the provider key. Whether this row can be
			// excluded safely is decided below from non-secret customer ownership.
			unresolvedRows = append(unresolvedRows, rows[i])
			continue
		}
		if plain != billingKey {
			continue
		}
		keyIDs = appendPositiveUniqueInt(keyIDs, rows[i].Id)
		matchedRows = append(matchedRows, rows[i])
	}
	if unresolvedTossBillingKeyRowsMayMatch(customerKey, matchedRows, unresolvedRows) {
		return false, fmt.Errorf("%w: active legacy billing-key candidate cannot be verified", ErrTossBillingKeyIdentityUnresolved)
	}
	if len(keyIDs) == 0 {
		return false, nil
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, keyID := range keyIDs {
			if err := setTossBillingKeyStatusTx(tx, keyID, BillingKeyStatusRevoked); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	return true, nil
}

// unresolvedTossBillingKeyRowsMayMatch narrows a corrupt hashless row by the
// provider customer identity whenever a verified match is available. A Toss
// billingKey belongs to one customerKey, so a corrupt row owned by a different,
// non-empty customer cannot be a duplicate of the deleted key. Empty ownership,
// an explicit customer-scoped lookup, or the absence of any verified match stays
// fail-closed because BILLING_DELETED contains no other recoverable identity.
func unresolvedTossBillingKeyRowsMayMatch(requestedCustomerKey string, matchedRows, unresolvedRows []UserBillingKey) bool {
	if len(unresolvedRows) == 0 {
		return false
	}
	requestedCustomerKey = strings.TrimSpace(requestedCustomerKey)
	if requestedCustomerKey != "" || len(matchedRows) == 0 {
		return true
	}
	matchedCustomers := make(map[string]struct{}, len(matchedRows))
	for i := range matchedRows {
		customer := strings.TrimSpace(matchedRows[i].CustomerKey)
		if customer == "" {
			return true
		}
		matchedCustomers[customer] = struct{}{}
	}
	for i := range unresolvedRows {
		customer := strings.TrimSpace(unresolvedRows[i].CustomerKey)
		if customer == "" {
			return true
		}
		if _, possibleDuplicate := matchedCustomers[customer]; possibleDuplicate {
			return true
		}
	}
	return false
}

type tossBillingKeyHashBackfillUnresolvedError struct {
	count int
}

func (e *tossBillingKeyHashBackfillUnresolvedError) Error() string {
	return fmt.Sprintf("%d active Toss billing-key identities remain unresolved", e.count)
}

func (e *tossBillingKeyHashBackfillUnresolvedError) Unwrap() error {
	return ErrTossBillingKeyIdentityUnresolved
}

func backfillTossBillingKeyHashes(limit int) error {
	if limit <= 0 {
		limit = 500
	}
	lastID := 0
	unresolved := 0
	for {
		var rows []UserBillingKey
		if err := DB.Where("(billing_key_hash = '' OR billing_key_hash IS NULL) AND id > ?", lastID).
			Where("status IN ?", []string{BillingKeyStatusActive, BillingKeyStatusPendingRevocation}).
			Order("id asc").
			Limit(limit).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			if unresolved > 0 {
				return &tossBillingKeyHashBackfillUnresolvedError{count: unresolved}
			}
			return nil
		}
		for i := range rows {
			lastID = rows[i].Id
			plain, err := common.DecryptString(rows[i].EncryptedKey)
			if err != nil || !IsValidTossBillingKey(plain) {
				unresolved++
				continue
			}
			if err := DB.Model(&UserBillingKey{}).
				Where("id = ?", rows[i].Id).
				Update("billing_key_hash", tossBillingKeyHash(plain)).Error; err != nil {
				return err
			}
		}
		if len(rows) < limit {
			if unresolved > 0 {
				return &tossBillingKeyHashBackfillUnresolvedError{count: unresolved}
			}
			return nil
		}
	}
}
