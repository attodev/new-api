package model

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrTossRecurringProtocolMigrationUnsafe = errors.New("Toss recurring payment protocol migration requires billing to be disabled")

const TossRecurringProtocolMigrationDrainedEnv = "TOSS_RECURRING_PROTOCOL_MIGRATION_DRAINED"

// requireTossRecurringProtocolMigrationSafe prevents a new master from
// silently beginning the durable-attempt protocol while an old recurring
// worker can still charge the same cycle under the legacy orderId rules.
//
// The check deliberately runs before AutoMigrate. A fresh database has neither
// legacy table and is safe. An already-migrated database is also safe to start
// normally. Only the transition from a legacy/partial schema is fenced, and an
// operator must stop and drain every old recurring worker before retrying the
// migration. Old subscription workers understand TossBillingEnabled, but the
// deployed legacy wallet worker does not understand TossWalletAutoRechargeEnabled
// and can keep charging while that option is absent or false. Persisted live
// recurring rows are therefore an independent migration fence, not merely a
// fallback for the options table.
func requireTossRecurringProtocolMigrationSafe() error {
	if DB == nil {
		return nil
	}
	subscriptionTableExists := DB.Migrator().HasTable(&SubscriptionOrder{})
	walletTableExists := DB.Migrator().HasTable(&TopUp{})
	subscriptionIdentitySchemaIncomplete := subscriptionTableExists &&
		(!DB.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalSubscriptionId") ||
			!DB.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalBillingTime") ||
			!DB.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalAttempt") ||
			!DB.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalOrderIdVersion"))
	walletIdentitySchemaIncomplete := walletTableExists &&
		(!DB.Migrator().HasColumn(&TopUp{}, "WalletAutoRechargeId") ||
			!DB.Migrator().HasColumn(&TopUp{}, "WalletAutoRechargeCycleKey") ||
			!DB.Migrator().HasColumn(&TopUp{}, "WalletAutoRechargeAttempt") ||
			!DB.Migrator().HasColumn(&TopUp{}, "WalletOrderIdVersion"))
	legacySubscriptionSchema := subscriptionTableExists &&
		(subscriptionIdentitySchemaIncomplete ||
			!DB.Migrator().HasColumn(&SubscriptionOrder{}, "RenewalCreationToken") ||
			!DB.Migrator().HasIndex(&SubscriptionOrder{}, "idx_subscription_order_toss_renewal_attempt"))
	legacyWalletSchema := walletTableExists &&
		(walletIdentitySchemaIncomplete ||
			!DB.Migrator().HasColumn(&TopUp{}, "WalletAutoRechargeCreationToken") ||
			!DB.Migrator().HasIndex(&TopUp{}, "idx_topup_toss_wallet_attempt"))
	if !legacySubscriptionSchema && !legacyWalletSchema {
		return nil
	}

	// An opaque order ID with a schema that cannot carry its immutable tuple is
	// proof of a partial restore/downgrade, not ordinary legacy data. AutoMigrate
	// would add zero/NULL columns and make the provider charge look like an
	// unrelated initial checkout, so no drain acknowledgement may repair it
	// automatically.
	if legacySubscriptionSchema {
		if subscriptionIdentitySchemaIncomplete {
			evidence, err := hasTossRenewalOpaquePrefixEvidenceBeforeMigration()
			if err != nil {
				return fmt.Errorf("inspect opaque Toss renewal migration evidence: %w", err)
			}
			if evidence {
				return fmt.Errorf("%w: opaque renewal order exists without the complete identity schema", ErrTossRecurringOrderIDEvidenceCorrupt)
			}
		} else if err := ensureNoIncompleteTossRenewalOrderIdentityTx(DB); err != nil {
			return fmt.Errorf("inspect Toss renewal identity migration evidence: %w", err)
		}
	}
	if legacyWalletSchema {
		if walletIdentitySchemaIncomplete {
			evidence, err := hasTossWalletOpaquePrefixEvidenceBeforeMigration()
			if err != nil {
				return fmt.Errorf("inspect opaque Toss wallet migration evidence: %w", err)
			}
			if evidence {
				return fmt.Errorf("%w: opaque wallet order exists without the complete identity schema", ErrTossRecurringOrderIDEvidenceCorrupt)
			}
		} else if err := ensureNoIncompleteWalletAutoRechargeOrderIdentityTx(DB); err != nil {
			return fmt.Errorf("inspect Toss wallet identity migration evidence: %w", err)
		}
	}

	if DB.Migrator().HasTable(&Option{}) {
		var options []Option
		if err := DB.Select(commonKeyCol+", value").Where(commonKeyCol+" IN ?", []string{
			"TossBillingEnabled",
			"TossWalletAutoRechargeEnabled",
		}).Find(&options).Error; err != nil {
			return fmt.Errorf("inspect Toss recurring migration gate: %w", err)
		}
		for _, option := range options {
			// Missing rows are legacy false. Once a row exists, however, only an
			// explicit false is safe; malformed values must never be interpreted as
			// a disabled payment worker or bypassed by the drain acknowledgement.
			if !strings.EqualFold(strings.TrimSpace(option.Value), "false") {
				return tossRecurringProtocolMigrationUnsafeError("a recurring billing option is enabled or invalid")
			}
		}
	}

	activeSubscriptions, err := hasActiveLegacyTossRecurringSubscriptions()
	if err != nil {
		return fmt.Errorf("inspect active Toss subscription migration state: %w", err)
	}
	activeWalletPolicies, err := hasActiveLegacyTossWalletPolicies()
	if err != nil {
		return fmt.Errorf("inspect active Toss wallet migration state: %w", err)
	}
	pendingSubscriptionCallbacks, err := hasPendingLegacyTossSubscriptionCallbacks()
	if err != nil {
		return fmt.Errorf("inspect pending Toss subscription callback migration state: %w", err)
	}
	if !activeSubscriptions && !activeWalletPolicies && !pendingSubscriptionCallbacks {
		return nil
	}
	if strings.TrimSpace(os.Getenv(TossRecurringProtocolMigrationDrainedEnv)) != "true" {
		reason := "live recurring rows remain"
		switch {
		case activeSubscriptions && activeWalletPolicies && pendingSubscriptionCallbacks:
			reason = "active auto-renew subscriptions, active/pending wallet policies, and pending Toss subscription callbacks remain"
		case activeSubscriptions && activeWalletPolicies:
			reason = "active auto-renew subscriptions and active/pending wallet policies remain"
		case activeSubscriptions && pendingSubscriptionCallbacks:
			reason = "active auto-renew subscriptions and pending Toss subscription callbacks remain"
		case activeWalletPolicies && pendingSubscriptionCallbacks:
			reason = "active/pending wallet policies and pending Toss subscription callbacks remain"
		case activeSubscriptions:
			reason = "active auto-renew subscription rows remain"
		case activeWalletPolicies:
			reason = "active or pending wallet auto-recharge policies remain"
		case pendingSubscriptionCallbacks:
			reason = "pending Toss subscription callbacks remain"
		}
		return tossRecurringProtocolMigrationUnsafeError(reason)
	}
	return nil
}

func hasTossRenewalOpaquePrefixEvidenceBeforeMigration() (bool, error) {
	migrator := DB.Migrator()
	if !migrator.HasTable(&SubscriptionOrder{}) || !migrator.HasColumn(&SubscriptionOrder{}, "TradeNo") {
		return false, nil
	}
	query := DB.Model(&SubscriptionOrder{}).Where("trade_no LIKE ? ESCAPE '!'", tossRenewalOpaqueOrderIDLikePattern())
	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasTossWalletOpaquePrefixEvidenceBeforeMigration() (bool, error) {
	migrator := DB.Migrator()
	if !migrator.HasTable(&TopUp{}) || !migrator.HasColumn(&TopUp{}, "TradeNo") {
		return false, nil
	}
	query := DB.Model(&TopUp{}).Where("trade_no LIKE ? ESCAPE '!'", tossWalletOpaqueOrderIDLikePattern())
	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasPendingLegacyTossSubscriptionCallbacks() (bool, error) {
	migrator := DB.Migrator()
	if !migrator.HasTable(&SubscriptionOrder{}) || !migrator.HasColumn(&SubscriptionOrder{}, "Status") {
		return false, nil
	}

	query := DB.Model(&SubscriptionOrder{}).Where("status = ?", "pending")
	hasProvider := migrator.HasColumn(&SubscriptionOrder{}, "PaymentProvider")
	hasMethod := migrator.HasColumn(&SubscriptionOrder{}, "PaymentMethod")
	switch {
	case hasProvider && hasMethod:
		query = query.Where("payment_provider = ? OR payment_method = ?", PaymentProviderToss, PaymentMethodToss)
	case hasProvider:
		query = query.Where("payment_provider = ?", PaymentProviderToss)
	case hasMethod:
		query = query.Where("payment_method = ?", PaymentMethodToss)
	default:
		// A legacy subscription-order schema without a provider discriminator
		// cannot prove that an in-flight callback is unrelated to Toss.
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasActiveLegacyTossRecurringSubscriptions() (bool, error) {
	migrator := DB.Migrator()
	if !migrator.HasTable(&UserSubscription{}) || !migrator.HasColumn(&UserSubscription{}, "AutoRenew") {
		return false, nil
	}

	query := DB.Model(&UserSubscription{}).Where("auto_renew = ?", true)
	if migrator.HasColumn(&UserSubscription{}, "Status") {
		query = query.Where("status = ?", "active")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasActiveLegacyTossWalletPolicies() (bool, error) {
	migrator := DB.Migrator()
	if !migrator.HasTable(&WalletAutoRecharge{}) || !migrator.HasColumn(&WalletAutoRecharge{}, "Status") {
		return false, nil
	}

	var count int64
	if err := DB.Model(&WalletAutoRecharge{}).
		Where("status IN ?", []string{
			WalletAutoRechargeStatusActive,
			WalletAutoRechargeStatusPending,
			WalletAutoRechargeStatusCancelPending,
		}).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func tossRecurringProtocolMigrationUnsafeError(reason string) error {
	return fmt.Errorf(
		"%w: %s; set recurring billing options false, stop every old master/scheduler and billing callback route, wait at least 5 minutes without rotating MID/keys/test mode, then set %s=true only on the sole migration master for this attempt; remove it immediately after success and never restart an old binary",
		ErrTossRecurringProtocolMigrationUnsafe,
		reason,
		TossRecurringProtocolMigrationDrainedEnv,
	)
}
