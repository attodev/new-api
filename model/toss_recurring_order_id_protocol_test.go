package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func deleteTossRecurringOrderIDProtocolStateForTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Where("id = ?", tossRecurringOrderIDProtocolStateID).
		Delete(&TossRecurringOrderIDProtocolState{}).Error)
}

func setTossRecurringOrderIDProtocolVersionForTest(t *testing.T, version int) {
	t.Helper()
	require.NoError(t, DB.Model(&TossRecurringOrderIDProtocolState{}).
		Where("id = ?", tossRecurringOrderIDProtocolStateID).
		UpdateColumn("write_version", version).Error)
}

func seedTossRecurringV2ActivationOptionsForTest(t *testing.T, billing, wallet string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Create(&[]Option{
		{Key: "TossBillingEnabled", Value: billing},
		{Key: "TossWalletAutoRechargeEnabled", Value: wallet},
	}).Error)
}

func setTossRecurringOpaqueOrderIDGeneratorForTest(t *testing.T, generator func(string) (string, error)) {
	t.Helper()
	previous := tossRecurringOpaqueOrderIDGenerator
	tossRecurringOpaqueOrderIDGenerator = generator
	t.Cleanup(func() { tossRecurringOpaqueOrderIDGenerator = previous })
}

func TestInitializeTossRecurringOrderIDProtocolStateRecoversMigrationDDLWindow(t *testing.T) {
	t.Run("new table seeds v2", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		deleteTossRecurringOrderIDProtocolStateForTest(t)

		require.NoError(t, initializeTossRecurringOrderIDProtocolState(true))
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolOpaqueVersion, state.WriteVersion)
	})

	t.Run("pre-existing empty table recovers behind v1 fence after migration crash", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		deleteTossRecurringOrderIDProtocolStateForTest(t)

		require.NoError(t, initializeTossRecurringOrderIDProtocolState(false))
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolLegacyVersion, state.WriteVersion)
		err := DB.Transaction(func(tx *gorm.DB) error {
			_, err := tossRecurringOrderIDWriterVersionTx(tx)
			return err
		})
		require.ErrorIs(t, err, ErrTossRecurringOrderIDV2ActivationRequired,
			"crash recovery must not silently enable fresh recurring charges")
	})

	t.Run("recreated table with live v1 state seeds v1", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		require.NoError(t, DB.Create(&UserSubscription{
			Id: 901, UserId: 901, PlanId: 901, Status: "active", AutoRenew: true,
		}).Error)
		require.NoError(t, DB.Migrator().DropTable(&TossRecurringOrderIDProtocolState{}))
		require.NoError(t, DB.AutoMigrate(&TossRecurringOrderIDProtocolState{}))

		require.NoError(t, initializeTossRecurringOrderIDProtocolState(false))
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolLegacyVersion, state.WriteVersion)
	})

	t.Run("known v2 starts for recovery", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)
		require.NoError(t, initializeTossRecurringOrderIDProtocolState(false), "a rollback reader must start and reconcile known v2 rows")
	})

	t.Run("unknown version fails", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion+1)
		require.ErrorIs(t, initializeTossRecurringOrderIDProtocolState(false), ErrTossRecurringOrderIDProtocolState)
	})

	t.Run("recreated table does not downgrade existing opaque evidence", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		subID := 991
		billingTime := int64(1782840000)
		attempt := 0
		require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
		require.NoError(t, DB.Create(&SubscriptionOrder{
			TradeNo: "opaqueProtocolStateRestore991", PaymentProvider: PaymentProviderToss,
			RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
			RenewalOrderIdVersion: tossRecurringOrderIDProtocolOpaqueVersion,
		}).Error)
		require.NoError(t, DB.Migrator().DropTable(&TossRecurringOrderIDProtocolState{}))
		require.NoError(t, DB.AutoMigrate(&TossRecurringOrderIDProtocolState{}))

		require.ErrorIs(t, initializeTossRecurringOrderIDProtocolState(false), ErrTossRecurringOrderIDProtocolState)
		var count int64
		require.NoError(t, DB.Model(&TossRecurringOrderIDProtocolState{}).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("recreated table does not downgrade existing opaque wallet evidence", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		policyID := 992
		cycleKey := "s:1782840000"
		attempt := 0
		require.NoError(t, DB.AutoMigrate(&TopUp{}))
		require.NoError(t, DB.Create(&TopUp{
			TradeNo:       "opaqueProtocolStateWalletRestore992",
			PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey, WalletAutoRechargeAttempt: &attempt,
			WalletOrderIdVersion: tossRecurringOrderIDProtocolOpaqueVersion,
		}).Error)
		require.NoError(t, DB.Migrator().DropTable(&TossRecurringOrderIDProtocolState{}))
		require.NoError(t, DB.AutoMigrate(&TossRecurringOrderIDProtocolState{}))

		require.ErrorIs(t, initializeTossRecurringOrderIDProtocolState(true), ErrTossRecurringOrderIDProtocolState)
		var count int64
		require.NoError(t, DB.Model(&TossRecurringOrderIDProtocolState{}).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("recreated table rejects opaque prefix whose tuple columns were restored as zero", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
		restoredTradeNo := tossRenewalOpaqueOrderIDPrefix + strings.Repeat("e", 40)
		require.NoError(t, DB.Create(&SubscriptionOrder{
			TradeNo: restoredTradeNo, PaymentMethod: PaymentMethodToss,
			PaymentProvider: "", Status: common.TopUpStatusPending,
		}).Error)
		require.NoError(t, DB.Migrator().DropTable(&TossRecurringOrderIDProtocolState{}))
		require.NoError(t, DB.AutoMigrate(&TossRecurringOrderIDProtocolState{}))

		require.ErrorIs(t, initializeTossRecurringOrderIDProtocolState(true), ErrTossRecurringOrderIDProtocolState)
		var count int64
		require.NoError(t, DB.Model(&TossRecurringOrderIDProtocolState{}).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("recreated table rejects wallet opaque prefix whose tuple columns were restored as zero", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		require.NoError(t, DB.AutoMigrate(&TopUp{}))
		restoredTradeNo := tossWalletOpaqueOrderIDPrefix + strings.Repeat("f", 40)
		require.NoError(t, DB.Create(&TopUp{
			TradeNo: restoredTradeNo, PaymentMethod: PaymentMethodToss,
			PaymentProvider: "", Status: common.TopUpStatusPending,
		}).Error)
		require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", restoredTradeNo).
			Update("payment_provider", nil).Error)
		require.NoError(t, DB.Migrator().DropTable(&TossRecurringOrderIDProtocolState{}))
		require.NoError(t, DB.AutoMigrate(&TossRecurringOrderIDProtocolState{}))

		require.ErrorIs(t, initializeTossRecurringOrderIDProtocolState(true), ErrTossRecurringOrderIDProtocolState)
		var count int64
		require.NoError(t, DB.Model(&TossRecurringOrderIDProtocolState{}).Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestActivateTossRecurringOrderIDProtocolV2RequiresExplicitDrain(t *testing.T) {
	t.Run("no acknowledgement leaves v1", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
		t.Setenv(TossRecurringOrderIDV2ActivationDrainedEnv, "")
		t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "")
		require.NoError(t, activateTossRecurringOrderIDProtocolV2IfRequested())
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolLegacyVersion, state.WriteVersion)
	})

	for _, test := range []struct {
		name    string
		billing string
		wallet  string
		seed    bool
	}{
		{name: "missing option rows"},
		{name: "billing enabled", billing: "true", wallet: "false", seed: true},
		{name: "wallet malformed", billing: "false", wallet: "not-a-bool", seed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
			t.Setenv(TossRecurringOrderIDV2ActivationDrainedEnv, "true")
			t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "")
			if test.seed {
				seedTossRecurringV2ActivationOptionsForTest(t, test.billing, test.wallet)
			}
			require.ErrorIs(t, activateTossRecurringOrderIDProtocolV2IfRequested(), ErrTossRecurringOrderIDV2ActivationUnsafe)
			var state TossRecurringOrderIDProtocolState
			require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
			require.Equal(t, tossRecurringOrderIDProtocolLegacyVersion, state.WriteVersion)
		})
	}

	t.Run("dedicated drain flips v1 to v2", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
		t.Setenv(TossRecurringOrderIDV2ActivationDrainedEnv, "true")
		t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "")
		seedTossRecurringV2ActivationOptionsForTest(t, "false", "false")
		require.NoError(t, activateTossRecurringOrderIDProtocolV2IfRequested())
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolOpaqueVersion, state.WriteVersion)
	})

	t.Run("legacy schema drain also flips v1 to v2", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
		t.Setenv(TossRecurringOrderIDV2ActivationDrainedEnv, "")
		t.Setenv(TossRecurringProtocolMigrationDrainedEnv, "true")
		seedTossRecurringV2ActivationOptionsForTest(t, "false", "false")
		require.NoError(t, activateTossRecurringOrderIDProtocolV2IfRequested())
		var state TossRecurringOrderIDProtocolState
		require.NoError(t, DB.First(&state, tossRecurringOrderIDProtocolStateID).Error)
		require.Equal(t, tossRecurringOrderIDProtocolOpaqueVersion, state.WriteVersion)
	})

	t.Run("already v2 is idempotent", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)
		t.Setenv(TossRecurringOrderIDV2ActivationDrainedEnv, "true")
		require.NoError(t, activateTossRecurringOrderIDProtocolV2IfRequested())
	})
}

func TestTossRecurringOpaqueOrderIDReadersRejectCrossTypePrefix(t *testing.T) {
	subID, policyID := 71, 72
	billingTime := int64(1782840000)
	cycleKey := "s:1782840000"
	attempt := 0
	suffix := strings.Repeat("a", 40)

	_, _, err := subscriptionOrderRenewalIdentity(&SubscriptionOrder{
		TradeNo: tossWalletOpaqueOrderIDPrefix + suffix, PaymentProvider: PaymentProviderToss,
		RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
		RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
	})
	require.ErrorIs(t, err, ErrTossRenewalIdentityConflict)

	_, _, err = topUpWalletAutoRechargeAttemptIdentity(&TopUp{
		TradeNo:              tossRenewalOpaqueOrderIDPrefix + suffix,
		WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey, WalletAutoRechargeAttempt: &attempt,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	})
	require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict)

	for _, malformed := range []string{
		tossRenewalOpaqueOrderIDPrefix + "abc",
		tossRenewalOpaqueOrderIDPrefix + strings.Repeat("g", 40),
		tossRenewalOpaqueOrderIDPrefix + strings.Repeat("A", 40),
	} {
		_, _, err = subscriptionOrderRenewalIdentity(&SubscriptionOrder{
			TradeNo: malformed, PaymentProvider: PaymentProviderToss,
			RenewalSubscriptionId: &subID, RenewalBillingTime: &billingTime, RenewalAttempt: &attempt,
			RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
		})
		require.ErrorIs(t, err, ErrTossRenewalIdentityConflict, malformed)
	}

	for _, malformed := range []string{
		tossWalletOpaqueOrderIDPrefix + "abc",
		tossWalletOpaqueOrderIDPrefix + strings.Repeat("g", 40),
		tossWalletOpaqueOrderIDPrefix + strings.Repeat("A", 40),
	} {
		_, _, err = topUpWalletAutoRechargeAttemptIdentity(&TopUp{
			TradeNo:              malformed,
			WalletAutoRechargeId: &policyID, WalletAutoRechargeCycleKey: &cycleKey, WalletAutoRechargeAttempt: &attempt,
			WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		})
		require.ErrorIs(t, err, ErrWalletAutoRechargeAttemptAssociationConflict, malformed)
	}
}

func TestTossRecurringOpaqueOrderIDReadersRejectLostAssociationTuple(t *testing.T) {
	suffix := strings.Repeat("a", 40)
	renewal := &SubscriptionOrder{
		TradeNo:       tossRenewalOpaqueOrderIDPrefix + suffix,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
	}
	_, _, _, isRenewal, err := ResolveTossRenewalOrderIdentity(renewal)
	require.False(t, isRenewal)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)

	wallet := &TopUp{
		TradeNo:       tossWalletOpaqueOrderIDPrefix + suffix,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
	}
	_, _, err = topUpWalletAutoRechargeAttemptIdentity(wallet)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
}

func TestFreshV2TossRenewalUsesOpaqueIDAndExistingRecoveryBypassesFence(t *testing.T) {
	setupTossBillingModelTestDB(t)
	seedTossBillingSubscription(t, "protocol-v2-renewal-key", 0)
	seedTossBillingPlan(t, "Protocol v2 renewal", 1)
	setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)
	var sub UserSubscription
	require.NoError(t, DB.First(&sub, 11).Error)
	legacyTradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0)

	created, err := PrepareTossRenewalOrder(sub.Id, legacyTradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	require.Equal(t, tossRecurringOrderIDVersionOpaque, created.RenewalOrderIdVersion)
	require.True(t, validTossRenewalOpaqueOrderID(created.TradeNo))
	require.Len(t, created.TradeNo, len(tossRenewalOpaqueOrderIDPrefix)+2*tossOpaqueOrderIDRandomBytes)
	require.NotEqual(t, legacyTradeNo, created.TradeNo)
	require.Equal(t, sub.Id, *created.RenewalSubscriptionId)
	require.Equal(t, sub.NextBillingTime, *created.RenewalBillingTime)
	require.Zero(t, *created.RenewalAttempt)

	deleteTossRecurringOrderIDProtocolStateForTest(t)
	reused, err := PrepareTossRenewalOrder(sub.Id, legacyTradeNo, 1, TossPlanKRW(1))
	require.NoError(t, err)
	require.Equal(t, created.Id, reused.Id)
	require.Equal(t, created.TradeNo, reused.TradeNo)
}

func TestFreshV2WalletAutoRechargeUsesOpaqueIDAndExistingRecoveryBypassesFence(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, now, _ := seedWalletProtocolFencePolicy(t)
	setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)

	created, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
	require.NoError(t, err)
	require.True(t, created.shouldCharge)
	require.True(t, validTossWalletOpaqueOrderID(created.tradeNo))
	require.Len(t, created.tradeNo, len(tossWalletOpaqueOrderIDPrefix)+2*tossOpaqueOrderIDRandomBytes)
	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", created.tradeNo).First(&topUp).Error)
	require.Equal(t, tossWalletOrderIDVersionOpaque, topUp.WalletOrderIdVersion)
	require.Equal(t, policy.Id, *topUp.WalletAutoRechargeId)
	require.NotNil(t, topUp.WalletAutoRechargeCycleKey)
	require.Zero(t, *topUp.WalletAutoRechargeAttempt)

	deleteTossRecurringOrderIDProtocolStateForTest(t)
	reused, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
	require.NoError(t, err)
	require.Equal(t, created.tradeNo, reused.tradeNo)
	require.True(t, reused.lookupPending)
}

func TestV2TossRecurringWritersRetryOpaqueTradeNoCollision(t *testing.T) {
	t.Run("subscription", func(t *testing.T) {
		setupTossBillingModelTestDB(t)
		seedTossBillingSubscription(t, "protocol-v2-renewal-collision-key", 0)
		seedTossBillingPlan(t, "Protocol v2 renewal collision", 1)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)
		first := tossRenewalOpaqueOrderIDPrefix + strings.Repeat("a", 40)
		second := tossRenewalOpaqueOrderIDPrefix + strings.Repeat("b", 40)
		collisionSubID, collisionBillingTime, collisionAttempt := 999, GetDBTimestamp(), 0
		require.NoError(t, DB.Create(&SubscriptionOrder{
			TradeNo: first, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status:                common.TopUpStatusPending,
			RenewalSubscriptionId: &collisionSubID, RenewalBillingTime: &collisionBillingTime,
			RenewalAttempt: &collisionAttempt, RenewalOrderIdVersion: tossRecurringOrderIDVersionOpaque,
		}).Error)
		calls := 0
		setTossRecurringOpaqueOrderIDGeneratorForTest(t, func(prefix string) (string, error) {
			calls++
			if calls == 1 {
				return first, nil
			}
			return second, nil
		})
		var sub UserSubscription
		require.NoError(t, DB.First(&sub, 11).Error)
		created, err := PrepareTossRenewalOrder(sub.Id, tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0), 1, TossPlanKRW(1))
		require.NoError(t, err)
		require.Equal(t, second, created.TradeNo)
		require.Equal(t, 2, calls)
	})

	t.Run("wallet", func(t *testing.T) {
		setupWalletAutoRechargeTestDB(t)
		policy, now, _ := seedWalletProtocolFencePolicy(t)
		setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion)
		first := tossWalletOpaqueOrderIDPrefix + strings.Repeat("c", 40)
		second := tossWalletOpaqueOrderIDPrefix + strings.Repeat("d", 40)
		collisionPolicyID, collisionAttempt := policy.Id+999, 0
		collisionCycle := "collision-cycle"
		require.NoError(t, DB.Create(&TopUp{
			TradeNo: first, PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
			Status:               common.TopUpStatusPending,
			WalletAutoRechargeId: &collisionPolicyID, WalletAutoRechargeCycleKey: &collisionCycle,
			WalletAutoRechargeAttempt: &collisionAttempt, WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
		}).Error)
		calls := 0
		setTossRecurringOpaqueOrderIDGeneratorForTest(t, func(prefix string) (string, error) {
			calls++
			if calls == 1 {
				return first, nil
			}
			return second, nil
		})
		created, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
		require.NoError(t, err)
		require.Equal(t, second, created.tradeNo)
		require.Equal(t, 2, calls)
	})
}

func TestFreshTossRenewalWriterFailsClosedOnProtocolFence(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testing.T)
		expected error
	}{
		{
			name: "missing singleton",
			mutate: func(t *testing.T) {
				deleteTossRecurringOrderIDProtocolStateForTest(t)
			},
			expected: ErrTossRecurringOrderIDProtocolState,
		},
		{
			name: "v2 activation required",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
			},
			expected: ErrTossRecurringOrderIDV2ActivationRequired,
		},
		{
			name: "unknown protocol version",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion+1)
			},
			expected: ErrTossRecurringOrderIDProtocolState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			keyID := seedTossBillingSubscription(t, "protocol-fence-renewal-key", 0)
			seedTossBillingPlan(t, "Protocol fence renewal", 1)
			test.mutate(t)

			postCalls := 0
			previousCharger := tossBillingCharger
			SetTossBillingCharger(func(context.Context, string, string, string, string, string, int64) (*TossBillingChargeResult, error) {
				postCalls++
				return nil, nil
			})
			t.Cleanup(func() { SetTossBillingCharger(previousCharger) })

			err := ProcessTossRenewal(context.Background(), 11, TossBillingMaxFails)
			require.ErrorIs(t, err, test.expected)
			require.Zero(t, postCalls)

			var sub UserSubscription
			require.NoError(t, DB.First(&sub, 11).Error)
			require.True(t, sub.AutoRenew)
			require.Equal(t, "active", sub.Status)
			require.Zero(t, sub.BillingFailCount)
			require.Positive(t, sub.NextBillingTime)

			var key UserBillingKey
			require.NoError(t, DB.First(&key, keyID).Error)
			require.Equal(t, BillingKeyStatusActive, key.Status)

			var renewalCount int64
			require.NoError(t, DB.Model(&SubscriptionOrder{}).
				Where("renewal_subscription_id = ?", sub.Id).Count(&renewalCount).Error)
			require.Zero(t, renewalCount)
		})
	}
}

func TestTossRenewalV2CreatesAndExistingOrderBypassesFence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T)
	}{
		{name: "missing singleton after create", mutate: deleteTossRecurringOrderIDProtocolStateForTest},
		{
			name: "v1 recovery-only state after create",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
			},
		},
		{
			name: "unknown protocol after create",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion+1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			seedTossBillingSubscription(t, "protocol-fence-existing-renewal-key", 0)
			seedTossBillingPlan(t, "Existing protocol renewal", 1)
			var sub UserSubscription
			require.NoError(t, DB.First(&sub, 11).Error)
			tradeNo := tossRenewalTradeNo(sub.Id, sub.NextBillingTime, 0)

			created, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
			require.NoError(t, err)
			require.Equal(t, tossRecurringOrderIDVersionOpaque, created.RenewalOrderIdVersion)
			test.mutate(t)

			reused, err := PrepareTossRenewalOrder(sub.Id, tradeNo, 1, TossPlanKRW(1))
			require.NoError(t, err)
			require.Equal(t, created.Id, reused.Id)
			require.Equal(t, created.TradeNo, reused.TradeNo)

			var count int64
			require.NoError(t, DB.Model(&SubscriptionOrder{}).
				Where("renewal_subscription_id = ?", sub.Id).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func seedWalletProtocolFencePolicy(t *testing.T) (WalletAutoRecharge, time.Time, int) {
	t.Helper()
	setWalletAutoRechargeUnitPriceForTest(t, 1000)
	require.NoError(t, DB.Create(&User{
		Id: 1, Username: "protocol-wallet-owner", AffCode: "protocol-wallet-owner",
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
	}).Error)
	encryptedKey, err := common.EncryptString("protocol-wallet-billing-key")
	require.NoError(t, err)
	const keyID = 81
	require.NoError(t, DB.Create(withWalletAutoRechargeTestMID(t, &UserBillingKey{
		Id: keyID, UserId: 1, CustomerKey: "protocol-wallet-customer",
		EncryptedKey: encryptedKey, Status: BillingKeyStatusActive,
	})).Error)

	now := time.Unix(GetDBTimestamp(), 0).In(time.Local)
	activeKey := walletAutoRechargeActiveKey(TopUpTargetTypeUser, 1, WalletAutoRechargeTypeScheduled)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		ActiveKey: &activeKey, OwnerUserId: 1, BillingKeyId: keyID,
		CustomerKey: "protocol-wallet-customer", ProviderClientKeyHash: TossBillingClientKeyFingerprint("wallet_auto_test_ck"),
		Amount: 10000, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: WalletAutoRechargeStatusActive, NextChargeTime: now.Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)
	return policy, now, keyID
}

func TestFreshWalletAutoRechargeWriterFailsClosedOnProtocolFence(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testing.T)
		expected error
	}{
		{name: "missing singleton", mutate: deleteTossRecurringOrderIDProtocolStateForTest, expected: ErrTossRecurringOrderIDProtocolState},
		{
			name: "v2 activation required",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
			},
			expected: ErrTossRecurringOrderIDV2ActivationRequired,
		},
		{
			name: "unknown protocol version",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion+1)
			},
			expected: ErrTossRecurringOrderIDProtocolState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, now, keyID := seedWalletProtocolFencePolicy(t)
			test.mutate(t)

			charge, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
			require.ErrorIs(t, err, test.expected)
			require.False(t, charge.shouldCharge)

			var stored WalletAutoRecharge
			require.NoError(t, DB.First(&stored, policy.Id).Error)
			require.Equal(t, WalletAutoRechargeStatusActive, stored.Status)
			require.Zero(t, stored.FailCount)
			require.NotNil(t, stored.ActiveKey)

			var key UserBillingKey
			require.NoError(t, DB.First(&key, keyID).Error)
			require.Equal(t, BillingKeyStatusActive, key.Status)

			var topUpCount int64
			require.NoError(t, DB.Model(&TopUp{}).
				Where("wallet_auto_recharge_id = ?", policy.Id).Count(&topUpCount).Error)
			require.Zero(t, topUpCount)
		})
	}
}

func TestWalletV2CreatesAndExistingTopUpBypassesFence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T)
	}{
		{name: "missing singleton after create", mutate: deleteTossRecurringOrderIDProtocolStateForTest},
		{
			name: "v1 recovery-only state after create",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolLegacyVersion)
			},
		},
		{
			name: "unknown protocol after create",
			mutate: func(t *testing.T) {
				setTossRecurringOrderIDProtocolVersionForTest(t, tossRecurringOrderIDProtocolOpaqueVersion+1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWalletAutoRechargeTestDB(t)
			policy, now, _ := seedWalletProtocolFencePolicy(t)

			created, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
			require.NoError(t, err)
			require.True(t, created.shouldCharge)
			require.NotEmpty(t, created.tradeNo)
			var original TopUp
			require.NoError(t, DB.Where("trade_no = ?", created.tradeNo).First(&original).Error)
			require.Equal(t, tossWalletOrderIDVersionOpaque, original.WalletOrderIdVersion)
			test.mutate(t)

			reused, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
			require.NoError(t, err)
			require.Equal(t, created.tradeNo, reused.tradeNo)
			require.True(t, reused.lookupPending)

			var count int64
			require.NoError(t, DB.Model(&TopUp{}).
				Where("wallet_auto_recharge_id = ?", policy.Id).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestWalletFinalPOSTGateGlobalCorruptionDoesNotHaltHealthyPolicy(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	policy, now, _ := seedWalletProtocolFencePolicy(t)

	prepared, err := prepareWalletAutoRechargeCharge(policy.Id, now, TossBillingMaxFails)
	require.NoError(t, err)
	require.True(t, prepared.shouldCharge)
	var ownTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", prepared.tradeNo).First(&ownTopUp).Error)
	require.False(t, ownTopUp.ProviderAttempted)

	unrelatedPolicyID := policy.Id + 999
	require.NoError(t, DB.Create(&TopUp{
		UserId: 99, TargetType: TopUpTargetTypeUser, TargetId: 99,
		TradeNo: "opaqueInsertedBeforeWalletFinalGate", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		WalletAutoRechargeId: &unrelatedPolicyID,
		WalletOrderIdVersion: tossWalletOrderIDVersionOpaque,
	}).Error)

	err = claimWalletAutoRechargeProviderPOSTCredential(policy.Id, prepared.tradeNo, setting.TossBillingSecretKey)
	require.ErrorIs(t, err, ErrTossRecurringOrderIDEvidenceCorrupt)
	require.NoError(t, DB.First(&policy, policy.Id).Error)
	require.Equal(t, WalletAutoRechargeStatusActive, policy.Status)
	require.NotNil(t, policy.ActiveKey)
	require.Zero(t, policy.FailCount)
	require.NoError(t, DB.First(&ownTopUp, ownTopUp.Id).Error)
	require.False(t, ownTopUp.ProviderAttempted)
}
