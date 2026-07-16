package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyTossRecurringSubscriptionOrder struct {
	Id      int    `gorm:"primaryKey"`
	TradeNo string `gorm:"uniqueIndex"`
}

func (legacyTossRecurringSubscriptionOrder) TableName() string { return "subscription_orders" }

type legacyPendingTossSubscriptionOrder struct {
	Id              int    `gorm:"primaryKey"`
	TradeNo         string `gorm:"uniqueIndex"`
	PaymentMethod   string
	PaymentProvider string
	Status          string
}

func (legacyPendingTossSubscriptionOrder) TableName() string { return "subscription_orders" }

type legacyTossRecurringTopUp struct {
	Id      int    `gorm:"primaryKey"`
	TradeNo string `gorm:"uniqueIndex"`
}

func (legacyTossRecurringTopUp) TableName() string { return "top_ups" }

type legacyTossRecurringUserSubscription struct {
	Id        int    `gorm:"primaryKey"`
	AutoRenew bool   `gorm:"default:false"`
	Status    string `gorm:"type:varchar(32)"`
}

func (legacyTossRecurringUserSubscription) TableName() string { return "user_subscriptions" }

type legacyTossRecurringWalletPolicy struct {
	Id     int    `gorm:"primaryKey"`
	Status string `gorm:"type:varchar(16)"`
}

func (legacyTossRecurringWalletPolicy) TableName() string { return "wallet_auto_recharges" }

func setupTossRecurringMigrationGuardTestDB(t *testing.T) {
	t.Helper()
	t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "")
	originalDB := DB
	originalSQLite := common.UsingSQLite
	originalMySQL := common.UsingMySQL
	originalPostgreSQL := common.UsingPostgreSQL
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		DB = originalDB
		common.UsingSQLite = originalSQLite
		common.UsingMySQL = originalMySQL
		common.UsingPostgreSQL = originalPostgreSQL
	})
}

func TestTossRecurringMigrationGuardRejectsEnabledLegacySchema(t *testing.T) {
	tests := []struct {
		name        string
		legacyModel any
		optionKey   string
	}{
		{name: "subscription renewal", legacyModel: &legacyTossRecurringSubscriptionOrder{}, optionKey: "TossBillingEnabled"},
		{name: "wallet auto recharge", legacyModel: &legacyTossRecurringTopUp{}, optionKey: "TossWalletAutoRechargeEnabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossRecurringMigrationGuardTestDB(t)
			require.NoError(t, DB.AutoMigrate(&Option{}, test.legacyModel))
			require.NoError(t, DB.Create(&Option{Key: test.optionKey, Value: " true "}).Error)

			err := requireTossRecurringProtocolMigrationSafe()
			require.ErrorIs(t, err, ErrTossRecurringProtocolMigrationUnsafe)
			require.Contains(t, err.Error(), "wait at least 5 minutes")
		})
	}
}

func TestTossRecurringMigrationGuardEnabledOrInvalidOptionCannotBeAcknowledged(t *testing.T) {
	for _, value := range []string{"true", "1", "disabled", ""} {
		t.Run("value="+value, func(t *testing.T) {
			setupTossRecurringMigrationGuardTestDB(t)
			t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "true")
			require.NoError(t, DB.AutoMigrate(&Option{}, &legacyTossRecurringTopUp{}))
			require.NoError(t, DB.Create(&Option{Key: "TossWalletAutoRechargeEnabled", Value: value}).Error)

			err := requireTossRecurringProtocolMigrationSafe()
			require.ErrorIs(t, err, ErrTossRecurringProtocolMigrationUnsafe)
			require.Contains(t, err.Error(), "enabled or invalid")
		})
	}
}

func TestTossRecurringMigrationGuardAllowsDisabledLegacySchema(t *testing.T) {
	setupTossRecurringMigrationGuardTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&Option{},
		&legacyTossRecurringSubscriptionOrder{},
		&legacyTossRecurringTopUp{},
	))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossBillingEnabled", Value: "false"},
		{Key: "TossWalletAutoRechargeEnabled", Value: "false"},
	}).Error)
	require.NoError(t, requireTossRecurringProtocolMigrationSafe())
}

func TestTossRecurringMigrationGuardRejectsLiveRowsWithoutEnabledOption(t *testing.T) {
	tests := []struct {
		name         string
		migrate      []any
		seed         any
		optionKey    string
		createOption bool
	}{
		{
			name: "active subscription without option table",
			migrate: []any{
				&legacyTossRecurringSubscriptionOrder{},
				&legacyTossRecurringUserSubscription{},
			},
			seed: &legacyTossRecurringUserSubscription{AutoRenew: true, Status: "active"},
		},
		{
			name: "active subscription with false option",
			migrate: []any{
				&Option{},
				&legacyTossRecurringSubscriptionOrder{},
				&legacyTossRecurringUserSubscription{},
			},
			seed:         &legacyTossRecurringUserSubscription{AutoRenew: true, Status: "active"},
			optionKey:    "TossBillingEnabled",
			createOption: true,
		},
		{
			name: "active wallet with absent option row",
			migrate: []any{
				&Option{},
				&legacyTossRecurringTopUp{},
				&legacyTossRecurringWalletPolicy{},
			},
			seed: &legacyTossRecurringWalletPolicy{Status: WalletAutoRechargeStatusActive},
		},
		{
			name: "pending wallet with false option",
			migrate: []any{
				&Option{},
				&legacyTossRecurringTopUp{},
				&legacyTossRecurringWalletPolicy{},
			},
			seed:         &legacyTossRecurringWalletPolicy{Status: WalletAutoRechargeStatusPending},
			optionKey:    "TossWalletAutoRechargeEnabled",
			createOption: true,
		},
		{
			name: "cancel-pending wallet with false option",
			migrate: []any{
				&Option{},
				&legacyTossRecurringTopUp{},
				&legacyTossRecurringWalletPolicy{},
			},
			seed:         &legacyTossRecurringWalletPolicy{Status: WalletAutoRechargeStatusCancelPending},
			optionKey:    "TossWalletAutoRechargeEnabled",
			createOption: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossRecurringMigrationGuardTestDB(t)
			require.NoError(t, DB.AutoMigrate(test.migrate...))
			if test.createOption {
				require.NoError(t, DB.Create(&Option{Key: test.optionKey, Value: "false"}).Error)
			}
			require.NoError(t, DB.Create(test.seed).Error)

			err := requireTossRecurringProtocolMigrationSafe()
			require.ErrorIs(t, err, ErrTossRecurringProtocolMigrationUnsafe)
			require.Contains(t, err.Error(), "wait at least 5 minutes")
		})
	}
}

func TestTossRecurringMigrationGuardAllowsLiveRowsOnlyAfterExplicitDrainAcknowledgement(t *testing.T) {
	setupTossRecurringMigrationGuardTestDB(t)
	t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "true")
	require.NoError(t, DB.AutoMigrate(
		&Option{},
		&legacyTossRecurringSubscriptionOrder{},
		&legacyTossRecurringTopUp{},
		&legacyTossRecurringUserSubscription{},
		&legacyTossRecurringWalletPolicy{},
	))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossBillingEnabled", Value: "false"},
		{Key: "TossWalletAutoRechargeEnabled", Value: "false"},
	}).Error)
	require.NoError(t, DB.Create(&legacyTossRecurringUserSubscription{AutoRenew: true, Status: "active"}).Error)
	require.NoError(t, DB.Create(&legacyTossRecurringWalletPolicy{Status: WalletAutoRechargeStatusActive}).Error)

	require.NoError(t, requireTossRecurringProtocolMigrationSafe())
}

func TestTossRecurringMigrationGuardRequiresDrainForPendingSubscriptionCallback(t *testing.T) {
	setupTossRecurringMigrationGuardTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &legacyPendingTossSubscriptionOrder{}))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossBillingEnabled", Value: "false"},
		{Key: "TossWalletAutoRechargeEnabled", Value: "false"},
	}).Error)
	require.NoError(t, DB.Create(&legacyPendingTossSubscriptionOrder{
		TradeNo: "pending-toss-subscription-callback", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: "pending",
	}).Error)

	err := requireTossRecurringProtocolMigrationSafe()
	require.ErrorIs(t, err, ErrTossRecurringProtocolMigrationUnsafe)
	require.Contains(t, err.Error(), "pending Toss subscription callbacks")

	t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "true")
	require.NoError(t, requireTossRecurringProtocolMigrationSafe())
}

func TestTossRecurringMigrationGuardRejectsNonExactDrainAcknowledgement(t *testing.T) {
	for _, value := range []string{"false", "TRUE", "1", "yes"} {
		t.Run("value="+value, func(t *testing.T) {
			setupTossRecurringMigrationGuardTestDB(t)
			t.Setenv(TossRecurringProtocolMigrationDrainedEnv, value)
			require.NoError(t, DB.AutoMigrate(
				&legacyTossRecurringTopUp{},
				&legacyTossRecurringWalletPolicy{},
			))
			require.NoError(t, DB.Create(&legacyTossRecurringWalletPolicy{
				Status: WalletAutoRechargeStatusActive,
			}).Error)

			require.ErrorIs(t, requireTossRecurringProtocolMigrationSafe(), ErrTossRecurringProtocolMigrationUnsafe)
		})
	}
}

func TestTossRecurringMigrationGuardAllowsTerminalOrNonRecurringRows(t *testing.T) {
	setupTossRecurringMigrationGuardTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&legacyTossRecurringSubscriptionOrder{},
		&legacyTossRecurringTopUp{},
		&legacyTossRecurringUserSubscription{},
		&legacyTossRecurringWalletPolicy{},
	))
	require.NoError(t, DB.Create(&[]legacyTossRecurringUserSubscription{
		{AutoRenew: false, Status: "active"},
		{AutoRenew: true, Status: "expired"},
	}).Error)
	require.NoError(t, DB.Create(&[]legacyTossRecurringWalletPolicy{
		{Status: WalletAutoRechargeStatusCancelled},
		{Status: WalletAutoRechargeStatusFailed},
	}).Error)
	require.NoError(t, requireTossRecurringProtocolMigrationSafe())
}

func TestTossRecurringMigrationGuardAllowsFreshOrMigratedSchema(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		setupTossRecurringMigrationGuardTestDB(t)
		require.NoError(t, DB.AutoMigrate(&Option{}))
		require.NoError(t, DB.Create(&Option{Key: "TossBillingEnabled", Value: "true"}).Error)
		require.NoError(t, requireTossRecurringProtocolMigrationSafe())
	})

	t.Run("already migrated", func(t *testing.T) {
		setupTossRecurringMigrationGuardTestDB(t)
		require.NoError(t, DB.AutoMigrate(&Option{}, &SubscriptionOrder{}, &TopUp{}))
		require.NoError(t, DB.Create(&Option{Key: "TossBillingEnabled", Value: "true"}).Error)
		require.NoError(t, requireTossRecurringProtocolMigrationSafe())
	})
}

func TestTossRecurringMigrationGuardRejectsPartialIndexMigration(t *testing.T) {
	setupTossRecurringMigrationGuardTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Option{}, &SubscriptionOrder{}, &TopUp{}))
	require.NoError(t, DB.Migrator().DropIndex(&SubscriptionOrder{}, "idx_subscription_order_toss_renewal_attempt"))
	require.NoError(t, DB.Create(&Option{Key: "TossBillingEnabled", Value: "true"}).Error)
	require.ErrorIs(t, requireTossRecurringProtocolMigrationSafe(), ErrTossRecurringProtocolMigrationUnsafe)
}

func TestTossRecurringMigrationGuardRejectsOpaqueRowsRestoredWithoutIdentityColumns(t *testing.T) {
	tests := []struct {
		name      string
		model     any
		tradeNo   string
		optionKey string
	}{
		{
			name:      "subscription",
			model:     &legacyPendingTossSubscriptionOrder{},
			tradeNo:   tossRenewalOpaqueOrderIDPrefix + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			optionKey: "TossBillingEnabled",
		},
		{
			name:      "wallet",
			model:     &legacyTossRecurringTopUp{},
			tradeNo:   tossWalletOpaqueOrderIDPrefix + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			optionKey: "TossWalletAutoRechargeEnabled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossRecurringMigrationGuardTestDB(t)
			t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "true")
			require.NoError(t, DB.AutoMigrate(&Option{}, test.model))
			require.NoError(t, DB.Create(&Option{Key: test.optionKey, Value: "false"}).Error)
			switch test.name {
			case "subscription":
				require.NoError(t, DB.Create(&legacyPendingTossSubscriptionOrder{
					TradeNo: test.tradeNo, PaymentMethod: PaymentMethodToss,
					PaymentProvider: "", Status: "success",
				}).Error)
			case "wallet":
				require.NoError(t, DB.Create(&legacyTossRecurringTopUp{TradeNo: test.tradeNo}).Error)
			}

			err := requireTossRecurringProtocolMigrationSafe()
			require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
		})
	}
}
