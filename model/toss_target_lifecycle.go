package model

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrOrganizationTossPaymentInFlight prevents an organization lifecycle
// transition from deleting or changing a wallet while a provider request can
// still move money for that target.
var ErrOrganizationTossPaymentInFlight = errors.New("an organization Toss payment is still in flight or requires reconciliation")

// ErrUserTossPaymentInFlight prevents deleting the payer row while a Toss
// provider result may still need that row for atomic quota or subscription
// settlement.
var ErrUserTossPaymentInFlight = errors.New("a user Toss payment is still in flight or requires reconciliation")

func countUnresolvedTossEventsForOrdersTx(tx *gorm.DB, orderIDs *gorm.DB) (int64, error) {
	if tx == nil || orderIDs == nil || !tx.Migrator().HasTable(&TossPaymentEvent{}) {
		return 0, nil
	}
	var unresolved int64
	err := tx.Model(&TossPaymentEvent{}).
		Where("reconciliation_status = ?", TossReconciliationStatusRequired).
		Where("order_id IN (?)", orderIDs).
		Count(&unresolved).Error
	return unresolved, err
}

// prepareOrganizationTossTopUpsForLifecycleTx serializes organization
// disable/owner-change/delete with the final provider-POST gates. Callers hold
// the organization row lock before entering this helper; Toss charge gates use
// that same row lock before they persist ProviderAttempted.
//
// A pristine browser checkout is safe to expire. A wallet-auto row is kept
// even when it predates ProviderAttempted because older workers could persist
// the deterministic TopUp immediately before POST without a separate marker.
// Durable payment evidence and unresolved reconciliation events always block
// the transition so a charged order never loses its quota target.
func prepareOrganizationTossTopUpsForLifecycleTx(tx *gorm.DB, organizationID int) error {
	if tx == nil || organizationID <= 0 || !tx.Migrator().HasTable(&TopUp{}) {
		return nil
	}

	base := func() *gorm.DB {
		return tx.Model(&TopUp{}).
			Where("target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationID).
			Where("(payment_provider = ? OR trade_no LIKE ? ESCAPE '!')", PaymentProviderToss, tossWalletOpaqueOrderIDLikePattern()).
			Where("payment_method = ? OR trade_no LIKE ? ESCAPE '!'", PaymentMethodToss, tossWalletOpaqueOrderIDLikePattern())
	}

	if tx.Migrator().HasTable(&TossPaymentEvent{}) {
		unresolved, err := countUnresolvedTossEventsForOrdersTx(tx, base().Select("trade_no"))
		if err != nil {
			return err
		}
		if unresolved > 0 {
			return ErrOrganizationTossPaymentInFlight
		}
	}

	now := getDBTimestampTx(tx)
	walletAutoRechargePattern := "wallet!_auto!_%"
	walletOpaquePattern := tossWalletOpaqueOrderIDLikePattern()
	var inFlight int64
	if err := base().Where(
		"(status = ? AND (trade_no LIKE ? ESCAPE '!' OR trade_no LIKE ? ESCAPE '!' OR wallet_order_id_version <> ? OR "+
			"wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL)) OR "+
			"(provider_attempted = ? AND (status = ? OR provider_claim_time > ?)) OR "+
			"(status = ? AND ((provider_order_id <> '' AND provider_order_id <> trade_no) OR provider_order_time > 0 OR "+
			"provider_payload <> '' OR provider_claim_token <> '' OR provider_claim_time > 0))",
		common.TopUpStatusPending, walletAutoRechargePattern, walletOpaquePattern, 0,
		true, common.TopUpStatusPending, now-walletAutoRechargeChargeClaimTTLSeconds,
		common.TopUpStatusPending,
	).Count(&inFlight).Error; err != nil {
		return err
	}
	if inFlight > 0 {
		return ErrOrganizationTossPaymentInFlight
	}

	// Expire only a provider-pristine general checkout. This CAS also prevents a
	// callback that records its paymentKey concurrently from being erased.
	return base().
		Where("status = ? AND provider_attempted = ?", common.TopUpStatusPending, false).
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
		}).Error
}

// prepareUserTossPaymentsForDeletionTx runs while the user row is locked by
// User.Delete/HardDelete. Provider POST gates lock that same row before
// persisting their durable attempt marker, so either deletion wins first and
// the gate rejects the missing user, or deletion observes the attempt and
// rolls back until its authoritative result is settled.
func prepareUserTossPaymentsForDeletionTx(tx *gorm.DB, userID int) error {
	if tx == nil || userID <= 0 {
		return nil
	}
	now := getDBTimestampTx(tx)
	walletAutoRechargePattern := "wallet!_auto!_%"
	walletOpaquePattern := tossWalletOpaqueOrderIDLikePattern()

	if tx.Migrator().HasTable(&TopUp{}) {
		// User deletion already owns the payer row. Lock every organization
		// targeted by this payer's Toss top-ups before any bulk TopUp mutation,
		// yielding payer -> organization(sorted) -> order. Without this prefix,
		// deletion could hold a pristine TopUp while DeactivateTossBillingForUser
		// later waited for its organization, opposite organization lifecycle's
		// organization -> TopUp order on MySQL/PostgreSQL.
		if tx.Migrator().HasTable(&Organization{}) {
			var organizationIDs []int
			if err := tx.Model(&TopUp{}).Distinct("target_id").
				Where("user_id = ? AND target_type = ? AND target_id > 0", userID, TopUpTargetTypeOrganization).
				Where("(payment_provider = ? AND payment_method = ?) OR trade_no LIKE ? ESCAPE '!'",
					PaymentProviderToss, PaymentMethodToss, tossWalletOpaqueOrderIDLikePattern()).
				Pluck("target_id", &organizationIDs).Error; err != nil {
				return err
			}
			sort.Ints(organizationIDs)
			for _, organizationID := range organizationIDs {
				var organization Organization
				err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
					Select("id").Where("id = ?", organizationID).First(&organization).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
		}
		base := func() *gorm.DB {
			return tx.Model(&TopUp{}).
				Where("user_id = ?", userID).
				Where("(payment_provider = ? OR trade_no LIKE ? ESCAPE '!')", PaymentProviderToss, tossWalletOpaqueOrderIDLikePattern()).
				Where("payment_method = ? OR trade_no LIKE ? ESCAPE '!'", PaymentMethodToss, tossWalletOpaqueOrderIDLikePattern())
		}
		unresolved, err := countUnresolvedTossEventsForOrdersTx(tx, base().Select("trade_no"))
		if err != nil {
			return err
		}
		if unresolved > 0 {
			return ErrUserTossPaymentInFlight
		}

		var inFlight int64
		if err := base().Where(
			"(status = ? AND (trade_no LIKE ? ESCAPE '!' OR trade_no LIKE ? ESCAPE '!' OR wallet_order_id_version <> ? OR "+
				"wallet_auto_recharge_id IS NOT NULL OR wallet_auto_recharge_cycle_key IS NOT NULL OR wallet_auto_recharge_attempt IS NOT NULL)) OR "+
				"(provider_attempted = ? AND (status = ? OR provider_claim_time > ?)) OR "+
				"(status = ? AND ((provider_order_id <> '' AND provider_order_id <> trade_no) OR provider_order_time > 0 OR "+
				"provider_payload <> '' OR provider_claim_token <> '' OR provider_claim_time > 0))",
			common.TopUpStatusPending, walletAutoRechargePattern, walletOpaquePattern, 0,
			true, common.TopUpStatusPending, now-walletAutoRechargeChargeClaimTTLSeconds,
			common.TopUpStatusPending,
		).Count(&inFlight).Error; err != nil {
			return err
		}
		if inFlight > 0 {
			return ErrUserTossPaymentInFlight
		}

		if err := base().
			Where("status = ? AND provider_attempted = ?", common.TopUpStatusPending, false).
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

	if !tx.Migrator().HasTable(&SubscriptionOrder{}) {
		return nil
	}
	orderBase := func() *gorm.DB {
		return tx.Model(&SubscriptionOrder{}).
			Where("user_id = ?", userID).
			Where("(payment_provider = ? OR trade_no LIKE ? ESCAPE '!')", PaymentProviderToss, tossRenewalOpaqueOrderIDLikePattern())
	}
	unresolved, err := countUnresolvedTossEventsForOrdersTx(tx, orderBase().Select("trade_no"))
	if err != nil {
		return err
	}
	if unresolved > 0 {
		return ErrUserTossPaymentInFlight
	}

	// An ISSUE request can create a provider billing key without moving money.
	// Do not block account deletion indefinitely, but move an already-attempted
	// request onto the terminal cleanup-only path while preserving its encrypted
	// authKey and live claim. A response-lost key can then be recovered with the
	// same idempotency key and deleted even after the user row is gone.
	if err := orderBase().
		Where("status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_attempted = ? AND billing_issue_auth_key <> ''",
			common.TopUpStatusPending, true).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Updates(map[string]interface{}{
			"status":        common.TopUpStatusExpired,
			"complete_time": now,
		}).Error; err != nil {
		return err
	}

	// Conversely, a claim whose ISSUE intent never committed cannot have
	// created a provider key. Expire it and erase the one-time authorization so
	// a worker that resumes after deletion cannot start a new ISSUE request.
	if err := orderBase().
		Where("status = ? AND (billing_key_id = 0 OR billing_key_id IS NULL) AND billing_issue_attempted = ? AND billing_attempted = ?",
			common.TopUpStatusPending, false, false).
		Where("trade_no NOT LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern()).
		Where("(billing_attempt_time = 0 OR billing_attempt_time IS NULL) AND (billing_attempt_credential = '' OR billing_attempt_credential IS NULL) AND (provider_payload = '' OR provider_payload IS NULL)").
		Updates(map[string]interface{}{
			"status":                      common.TopUpStatusExpired,
			"complete_time":               now,
			"provider_credential":         "",
			"provider_client_key_hash":    "",
			"billing_claim_token":         "",
			"billing_claim_time":          0,
			"billing_issue_auth_key":      "",
			"billing_issue_auth_key_hash": "",
			"billing_issue_customer_key":  "",
			"billing_issue_attempted":     false,
		}).Error; err != nil {
		return err
	}

	var inFlight int64
	if err := orderBase().Where(
		"(billing_attempted = ? AND (status = ? OR billing_claim_time > ?)) OR "+
			"(status = ? AND (billing_attempt_time > 0 OR billing_attempt_credential <> '' OR provider_payload <> '' OR trade_no LIKE ? ESCAPE '!'))",
		true, common.TopUpStatusPending, now-tossSubscriptionBillingClaimTTLSeconds,
		common.TopUpStatusPending, tossRenewalOpaqueOrderIDLikePattern(),
	).Count(&inFlight).Error; err != nil {
		return err
	}
	if inFlight > 0 {
		return ErrUserTossPaymentInFlight
	}
	return nil
}
