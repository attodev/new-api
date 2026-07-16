package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tossRecurringOrderIDProtocolStateID       = 1
	tossRecurringOrderIDProtocolLegacyVersion = 1
	tossRecurringOrderIDProtocolOpaqueVersion = 2
	tossRecurringOrderIDWriterMaxVersion      = tossRecurringOrderIDProtocolOpaqueVersion

	tossRenewalOpaqueOrderIDPrefix       = "trn_"
	tossWalletOpaqueOrderIDPrefix        = "twa_"
	tossOpaqueOrderIDRandomBytes         = 20
	tossOpaqueOrderIDCollisionMaxRetries = 4
)

var (
	ErrTossRecurringOrderIDProtocolState        = errors.New("Toss recurring order ID protocol state is unavailable or invalid")
	ErrTossRecurringOrderIDWriterTooOld         = errors.New("this node cannot create the required Toss recurring order ID version")
	ErrTossRecurringOrderIDV2ActivationRequired = errors.New("Toss recurring order ID v2 activation is required before creating new recurring charges")
	ErrTossRecurringOrderIDV2ActivationUnsafe   = errors.New("Toss recurring order ID v2 activation requires recurring billing to be drained")
	// ErrTossRecurringOrderIDEvidenceCorrupt means at least one recurring
	// payment row advertises a non-legacy order-ID protocol without the complete
	// immutable association tuple. The row cannot be scoped to one contract, so
	// all new recurring POSTs must stop. Callers must not disable or otherwise
	// mutate the healthy contract they happened to be processing: the corrupt
	// evidence may belong to a completely different subscription or wallet.
	ErrTossRecurringOrderIDEvidenceCorrupt = errors.New("Toss recurring order ID evidence is globally corrupt")
)

const TossRecurringOrderIDV2ActivationDrainedEnv = "TOSS_RECURRING_ORDER_ID_V2_ACTIVATION_DRAINED"

var tossRecurringOpaqueOrderIDGenerator = generateSecureTossRecurringOpaqueOrderID

// TossRecurringOrderIDProtocolState is a private, database-authoritative
// singleton. It is intentionally separate from the public/cached Toss options:
// every fresh recurring order must observe one cluster-wide writer version.
// New databases start on the opaque version. Existing version-1 databases are
// advanced only by the explicit, drained activation path below.
type TossRecurringOrderIDProtocolState struct {
	Id           int `json:"-" gorm:"primaryKey;autoIncrement:false"`
	WriteVersion int `json:"-" gorm:"not null"`
}

func (TossRecurringOrderIDProtocolState) TableName() string {
	return "toss_recurring_order_id_protocol_states"
}

func validateTossRecurringOrderIDProtocolStateStructure(state *TossRecurringOrderIDProtocolState) error {
	if state == nil || state.Id != tossRecurringOrderIDProtocolStateID || state.WriteVersion <= 0 {
		return ErrTossRecurringOrderIDProtocolState
	}
	if state.WriteVersion != tossRecurringOrderIDProtocolLegacyVersion &&
		state.WriteVersion != tossRecurringOrderIDProtocolOpaqueVersion {
		return ErrTossRecurringOrderIDProtocolState
	}
	return nil
}

func validateTossRecurringOrderIDWriterCapability(state *TossRecurringOrderIDProtocolState) error {
	if err := validateTossRecurringOrderIDProtocolStateStructure(state); err != nil {
		return err
	}
	if state.WriteVersion > tossRecurringOrderIDWriterMaxVersion {
		return fmt.Errorf("%w: database=%d node_max=%d", ErrTossRecurringOrderIDWriterTooOld, state.WriteVersion, tossRecurringOrderIDWriterMaxVersion)
	}
	return nil
}

// initializeTossRecurringOrderIDProtocolState is migration-only. seedMissing
// is true when this migration observed that the table did not exist before
// AutoMigrate. A pre-existing empty table is also recoverable here: SQLite and
// MySQL can durably commit the AutoMigrate DDL before the singleton insert, so
// a process crash in that window must not brick every later startup. The repair
// is deliberately more conservative than the same-run fresh-install path:
// opaque/unknown rows are rejected, while an already-existing empty table is
// always restored to the v1 fence. Even with no current recurring evidence, an
// old node may still be alive during a rolling upgrade; only the explicit,
// drained activation path may advance that recovered database to v2.
//
// Runtime payment paths never create or repair this row: a
// missing/restored/corrupt gate must stop fresh provider attempts instead of
// silently reverting to v1.
func initializeTossRecurringOrderIDProtocolState(seedMissing bool) error {
	if DB == nil {
		return ErrTossRecurringOrderIDProtocolState
	}
	tableCreatedByThisMigration := seedMissing
	return DB.Transaction(func(tx *gorm.DB) error {
		if !seedMissing {
			var count int64
			if err := tx.Model(&TossRecurringOrderIDProtocolState{}).Count(&count).Error; err != nil {
				return fmt.Errorf("%w: inspect singleton: %v", ErrTossRecurringOrderIDProtocolState, err)
			}
			seedMissing = count == 0
		}
		if seedMissing {
			newerEvidence, evidenceErr := hasTossRecurringOrderIDEvidenceOutsideLegacyProtocolTx(tx)
			if evidenceErr != nil {
				return fmt.Errorf("%w: inspect existing recurring order versions: %v", ErrTossRecurringOrderIDProtocolState, evidenceErr)
			}
			if newerEvidence {
				// A restored database may retain v2/unknown order rows while losing
				// the private singleton table. Treating the newly recreated table as
				// a fresh install and seeding v1 would be a silent writer downgrade.
				return fmt.Errorf("%w: protocol table is new but non-legacy recurring order evidence already exists", ErrTossRecurringOrderIDProtocolState)
			}
			legacyEvidence, evidenceErr := hasAnyTossRecurringOrderIDEvidenceTx(tx)
			if evidenceErr != nil {
				return fmt.Errorf("%w: inspect existing recurring payment state: %v", ErrTossRecurringOrderIDProtocolState, evidenceErr)
			}
			writeVersion := tossRecurringOrderIDProtocolLegacyVersion
			if tableCreatedByThisMigration && !legacyEvidence {
				// Only the migration invocation that observed the table as absent may
				// classify a database with no recurring evidence as a fresh install.
				writeVersion = tossRecurringOrderIDProtocolOpaqueVersion
			}
			// Every other missing singleton is a partial restore or a DDL-crash
			// recovery. Keep the v1 fence until the explicit drained procedure.
			seed := TossRecurringOrderIDProtocolState{
				Id:           tossRecurringOrderIDProtocolStateID,
				WriteVersion: writeVersion,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
				return fmt.Errorf("%w: seed singleton: %v", ErrTossRecurringOrderIDProtocolState, err)
			}
		}

		var states []TossRecurringOrderIDProtocolState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id asc").Limit(2).Find(&states).Error; err != nil {
			return fmt.Errorf("%w: load singleton: %v", ErrTossRecurringOrderIDProtocolState, err)
		}
		if len(states) != 1 {
			return fmt.Errorf("%w: expected one singleton row, found %d", ErrTossRecurringOrderIDProtocolState, len(states))
		}
		// Version 2 is a known reader protocol. A rolled-back Phase-A node must
		// still start and reconcile existing opaque attempts, while its fresh-row
		// writer is fenced separately below. Truly unknown versions fail startup.
		return validateTossRecurringOrderIDProtocolStateStructure(&states[0])
	})
}

func hasAnyTossRecurringOrderIDEvidenceTx(tx *gorm.DB) (bool, error) {
	if tx == nil {
		return false, ErrTossRecurringOrderIDProtocolState
	}
	type idOnly struct {
		Id int
	}
	if tx.Migrator().HasTable(&SubscriptionOrder{}) {
		var rows []idOnly
		query := tx.Model(&SubscriptionOrder{}).Select("id")
		if tx.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalOrderIdVersion") {
			query = query.Where("(payment_provider = ? AND (renewal_order_id_version <> ? OR renewal_subscription_id IS NOT NULL OR renewal_billing_time IS NOT NULL OR renewal_attempt IS NOT NULL OR trade_no LIKE ? ESCAPE '!')) OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, 0, tossRenewalTradeNoLikePattern(), tossRenewalOpaqueOrderIDLikePattern())
		} else {
			query = query.Where("(payment_provider = ? AND trade_no LIKE ? ESCAPE '!') OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, tossRenewalTradeNoLikePattern(), tossRenewalOpaqueOrderIDLikePattern())
		}
		if err := query.Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TopUp{}) {
		var rows []idOnly
		query := tx.Model(&TopUp{}).Select("id")
		if tx.Migrator().HasColumn(&TopUp{}, "WalletOrderIdVersion") {
			query = query.Where("(payment_provider = ? AND (wallet_order_id_version <> ? OR wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL OR trade_no LIKE ? ESCAPE '!')) OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, 0, "wallet!_auto!_%", tossWalletOpaqueOrderIDLikePattern())
		} else {
			query = query.Where("(payment_provider = ? AND trade_no LIKE ? ESCAPE '!') OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, "wallet!_auto!_%", tossWalletOpaqueOrderIDLikePattern())
		}
		if err := query.Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&UserSubscription{}) &&
		tx.Migrator().HasColumn(&UserSubscription{}, "AutoRenew") {
		var rows []idOnly
		query := tx.Model(&UserSubscription{}).Select("id").Where("auto_renew = ?", true)
		if tx.Migrator().HasColumn(&UserSubscription{}, "Status") {
			query = query.Where("status = ?", "active")
		}
		if err := query.Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&WalletAutoRecharge{}) &&
		tx.Migrator().HasColumn(&WalletAutoRecharge{}, "Status") {
		var rows []idOnly
		if err := tx.Model(&WalletAutoRecharge{}).Select("id").Where("status IN ?", []string{
			WalletAutoRechargeStatusActive,
			WalletAutoRechargeStatusPending,
			WalletAutoRechargeStatusCancelPending,
		}).Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func hasTossRecurringOrderIDEvidenceOutsideLegacyProtocolTx(tx *gorm.DB) (bool, error) {
	if tx == nil {
		return false, ErrTossRecurringOrderIDProtocolState
	}
	type idOnly struct {
		Id int
	}
	if tx.Migrator().HasTable(&SubscriptionOrder{}) {
		var rows []idOnly
		query := tx.Model(&SubscriptionOrder{}).Select("id")
		if tx.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalOrderIdVersion") {
			query = query.Where("(payment_provider = ? AND renewal_order_id_version <> ? AND renewal_order_id_version <> ?) OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, 0, tossRecurringOrderIDProtocolLegacyVersion, tossRenewalOpaqueOrderIDLikePattern())
		} else {
			query = query.Where("trade_no LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern())
		}
		if err := query.Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TopUp{}) {
		var rows []idOnly
		query := tx.Model(&TopUp{}).Select("id")
		if tx.Migrator().HasColumn(&TopUp{}, "WalletOrderIdVersion") {
			query = query.Where("(payment_provider = ? AND wallet_order_id_version <> ? AND wallet_order_id_version <> ?) OR trade_no LIKE ? ESCAPE '!'",
				PaymentProviderToss, 0, tossRecurringOrderIDProtocolLegacyVersion, tossWalletOpaqueOrderIDLikePattern())
		} else {
			query = query.Where("trade_no LIKE ? ESCAPE '!'", tossWalletOpaqueOrderIDLikePattern())
		}
		if err := query.Order("id asc").Limit(1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func generateSecureTossRecurringOpaqueOrderID(prefix string) (string, error) {
	random := make([]byte, tossOpaqueOrderIDRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate Toss recurring order ID: %w", err)
	}
	return prefix + hex.EncodeToString(random), nil
}

func newTossRecurringOpaqueOrderID(prefix string) (string, error) {
	orderID, err := tossRecurringOpaqueOrderIDGenerator(prefix)
	if err != nil {
		return "", err
	}
	orderID = strings.TrimSpace(orderID)
	if !validTossOpaqueOrderIDWithPrefix(orderID, prefix) {
		return "", ErrTossRecurringOrderIDProtocolState
	}
	return orderID, nil
}

func validTossOpaqueOrderIDWithPrefix(orderID, prefix string) bool {
	if len(orderID) != len(prefix)+2*tossOpaqueOrderIDRandomBytes || !strings.HasPrefix(orderID, prefix) {
		return false
	}
	for _, ch := range orderID[len(prefix):] {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') {
			continue
		}
		return false
	}
	return true
}

func hasTossOpaqueOrderIDPrefix(orderID, prefix string) bool {
	return strings.HasPrefix(strings.TrimSpace(orderID), prefix)
}

// HasTossRenewalOpaqueOrderIDPrefix reports the reserved provider-order
// namespace, including malformed or truncated values. Callers must use this
// broader check when classifying untrusted/restored rows: only the exact
// validator below can prove that a fully associated v2 row is valid.
func HasTossRenewalOpaqueOrderIDPrefix(orderID string) bool {
	return hasTossOpaqueOrderIDPrefix(orderID, tossRenewalOpaqueOrderIDPrefix)
}

// HasTossWalletOpaqueOrderIDPrefix is the wallet counterpart of
// HasTossRenewalOpaqueOrderIDPrefix.
func HasTossWalletOpaqueOrderIDPrefix(orderID string) bool {
	return hasTossOpaqueOrderIDPrefix(orderID, tossWalletOpaqueOrderIDPrefix)
}

func tossOpaqueOrderIDLikePattern(prefix string) string {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(prefix)
	return escaped + "%"
}

func tossRenewalOpaqueOrderIDLikePattern() string {
	return tossOpaqueOrderIDLikePattern(tossRenewalOpaqueOrderIDPrefix)
}

func tossWalletOpaqueOrderIDLikePattern() string {
	return tossOpaqueOrderIDLikePattern(tossWalletOpaqueOrderIDPrefix)
}

func validTossRenewalOpaqueOrderID(orderID string) bool {
	return validTossOpaqueOrderIDWithPrefix(orderID, tossRenewalOpaqueOrderIDPrefix)
}

func validTossWalletOpaqueOrderID(orderID string) bool {
	return validTossOpaqueOrderIDWithPrefix(orderID, tossWalletOpaqueOrderIDPrefix)
}

func lockTossRecurringOrderIDProtocolStateTx(tx *gorm.DB) (*TossRecurringOrderIDProtocolState, error) {
	if tx == nil {
		return nil, ErrTossRecurringOrderIDProtocolState
	}
	// SQLite ignores SELECT ... FOR UPDATE. A same-value write obtains its
	// database writer lock so activation and fresh inserts cannot cross the
	// version decision.
	if tx.Dialector != nil && tx.Dialector.Name() == "sqlite" {
		if err := tx.Model(&TossRecurringOrderIDProtocolState{}).
			Where("id = ?", tossRecurringOrderIDProtocolStateID).
			UpdateColumn("write_version", gorm.Expr("write_version")).Error; err != nil {
			return nil, fmt.Errorf("%w: lock singleton: %v", ErrTossRecurringOrderIDProtocolState, err)
		}
	}

	var state TossRecurringOrderIDProtocolState
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", tossRecurringOrderIDProtocolStateID).
		First(&state).Error; err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTossRecurringOrderIDProtocolState, err)
	}
	return &state, nil
}

// tossRecurringOrderIDWriterVersionTx is called only after the
// immutable association lookup proved that a fresh subscription/wallet attempt
// must be inserted. Existing attempts bypass this fence and keep using their
// persisted order ID and protocol version for recovery.
func tossRecurringOrderIDWriterVersionTx(tx *gorm.DB) (int, error) {
	state, err := lockTossRecurringOrderIDProtocolStateTx(tx)
	if err != nil {
		return 0, err
	}
	if err := validateTossRecurringOrderIDWriterCapability(state); err != nil {
		return 0, err
	}
	if state.WriteVersion != tossRecurringOrderIDProtocolOpaqueVersion {
		return 0, fmt.Errorf("%w: database=%d", ErrTossRecurringOrderIDV2ActivationRequired, state.WriteVersion)
	}
	return state.WriteVersion, nil
}

// activateTossRecurringOrderIDProtocolV2IfRequested is migration-only. A v1
// cluster advances only after an operator explicitly attests that every old
// recurring writer and callback route has been drained. The singleton row lock
// is the same lock used by fresh inserts, so the version flip and order insert
// cannot race across protocol generations.
func activateTossRecurringOrderIDProtocolV2IfRequested() error {
	v2DrainAcknowledged := strings.TrimSpace(os.Getenv(TossRecurringOrderIDV2ActivationDrainedEnv)) == "true"
	legacyMigrationDrainAcknowledged := strings.TrimSpace(os.Getenv(TossRecurringProtocolMigrationDrainedEnv)) == "true"
	if !v2DrainAcknowledged && !legacyMigrationDrainAcknowledged {
		return nil
	}
	if DB == nil {
		return ErrTossRecurringOrderIDProtocolState
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockTossRecurringOrderIDProtocolStateTx(tx)
		if err != nil {
			return err
		}
		if err := validateTossRecurringOrderIDProtocolStateStructure(state); err != nil {
			return err
		}
		if state.WriteVersion == tossRecurringOrderIDProtocolOpaqueVersion {
			return nil
		}
		if state.WriteVersion != tossRecurringOrderIDProtocolLegacyVersion {
			return ErrTossRecurringOrderIDProtocolState
		}
		if !tx.Migrator().HasTable(&Option{}) {
			return tossRecurringOrderIDV2ActivationUnsafeError("Toss option table is missing")
		}
		var options []Option
		if err := tx.Select(commonKeyCol+", value").Where(commonKeyCol+" IN ?", []string{
			"TossBillingEnabled",
			"TossWalletAutoRechargeEnabled",
		}).Find(&options).Error; err != nil {
			return fmt.Errorf("inspect Toss recurring order ID v2 activation gate: %w", err)
		}
		values := make(map[string]string, len(options))
		for _, option := range options {
			values[option.Key] = strings.TrimSpace(option.Value)
		}
		for _, key := range []string{"TossBillingEnabled", "TossWalletAutoRechargeEnabled"} {
			if value, ok := values[key]; !ok || value != "false" {
				return tossRecurringOrderIDV2ActivationUnsafeError(key + " must be explicitly false")
			}
		}
		result := tx.Model(&TossRecurringOrderIDProtocolState{}).
			Where("id = ? AND write_version = ?", tossRecurringOrderIDProtocolStateID, tossRecurringOrderIDProtocolLegacyVersion).
			UpdateColumn("write_version", tossRecurringOrderIDProtocolOpaqueVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossRecurringOrderIDProtocolState
		}
		return nil
	})
}

func tossRecurringOrderIDV2ActivationUnsafeError(reason string) error {
	return fmt.Errorf(
		"%w: %s; persist both Toss recurring options as false, stop every old master/scheduler and billing callback route, wait at least 5 minutes without rotating MID/keys/test mode/timezone, then set %s=true only on the sole migration master; remove it immediately after success and never restart a v1-only binary",
		ErrTossRecurringOrderIDV2ActivationUnsafe,
		reason,
		TossRecurringOrderIDV2ActivationDrainedEnv,
	)
}
