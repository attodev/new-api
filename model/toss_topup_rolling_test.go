package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// legacyTossTopUpRolling models the columns known to the release immediately
// before paymentKey-scoped confirmation idempotency was introduced. Keeping it
// in this test also exercises a real old-node Save after the additive migration.
type legacyTossTopUpRolling struct {
	Id                 int
	UserId             int
	TargetType         string `gorm:"type:varchar(32);default:'user'"`
	TargetId           int    `gorm:"default:0;index"`
	Amount             int64
	Money              float64
	Quota              int    `gorm:"default:0"`
	TradeNo            string `gorm:"unique;type:varchar(255);index"`
	ProviderOrderId    string `gorm:"type:varchar(255);index"`
	ProviderOrderTime  int64  `gorm:"default:0;index"`
	ProviderCredential string `gorm:"type:text"`
	PaymentMethod      string `gorm:"type:varchar(50)"`
	PaymentProvider    string `gorm:"type:varchar(50);default:''"`
	CreateTime         int64
	CompleteTime       int64
	Status             string
}

func (legacyTossTopUpRolling) TableName() string { return "top_ups" }

// legacyTossSubscriptionOrderRolling represents a binary that knows the
// attempt marker but predates BillingChargeProtocolVersion. Its Save must not
// erase a v1 marker written by a newer node during a rolling deployment.
type legacyTossSubscriptionOrderRolling struct {
	Id                       int
	UserId                   int
	PlanId                   int
	TradeNo                  string `gorm:"unique;type:varchar(255);index"`
	PaymentMethod            string `gorm:"type:varchar(50)"`
	PaymentProvider          string `gorm:"type:varchar(50);default:''"`
	ProviderCredential       string `gorm:"type:text"`
	BillingKeyId             int    `gorm:"default:0;index"`
	BillingAttempted         bool   `gorm:"default:false"`
	BillingAttemptCredential string `gorm:"type:text"`
	CreateTime               int64
	Status                   string
}

func (legacyTossSubscriptionOrderRolling) TableName() string { return "subscription_orders" }

type legacyTossTransactionCursorRolling struct {
	SourceKey  string `gorm:"type:varchar(64);primaryKey"`
	CursorTime int64  `gorm:"index"`
	UpdateTime int64
}

func (legacyTossTransactionCursorRolling) TableName() string {
	return "toss_transaction_reconciliation_cursors"
}

func TestTossAdditiveMigrationPreservesNewFieldsAcrossOldNodeSaves(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&legacyTossTopUpRolling{},
		&legacyTossSubscriptionOrderRolling{},
		&legacyTossTransactionCursorRolling{},
	))

	legacyTopUp := legacyTossTopUpRolling{
		UserId:             7,
		TargetType:         TopUpTargetTypeUser,
		TargetId:           7,
		Amount:             13000,
		TradeNo:            "toss_rolling_old_node",
		ProviderOrderId:    "pay_rolling_old_node",
		ProviderOrderTime:  100,
		ProviderCredential: "legacy-encrypted-credential",
		PaymentMethod:      PaymentMethodToss,
		PaymentProvider:    PaymentProviderToss,
		CreateTime:         100,
		Status:             common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&legacyTopUp).Error)
	legacySubscriptionOrder := legacyTossSubscriptionOrderRolling{
		UserId:                   7,
		PlanId:                   3,
		TradeNo:                  "toss_sub_rolling_old_node",
		PaymentMethod:            PaymentMethodToss,
		PaymentProvider:          PaymentProviderToss,
		ProviderCredential:       "legacy-subscription-credential",
		BillingKeyId:             41,
		BillingAttempted:         true,
		BillingAttemptCredential: "legacy-subscription-attempt-credential",
		CreateTime:               100,
		Status:                   common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&legacySubscriptionOrder).Error)
	legacyCursor := legacyTossTransactionCursorRolling{SourceKey: "legacy-source", CursorTime: 100, UpdateTime: 100}
	require.NoError(t, DB.Create(&legacyCursor).Error)

	require.NoError(t, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}, &TossTransactionReconciliationCursor{}))
	require.True(t, DB.Migrator().HasColumn(&TopUp{}, "provider_idempotency_key"))
	require.True(t, DB.Migrator().HasColumn(&TopUp{}, "provider_confirm_protocol_version"))
	require.True(t, DB.Migrator().HasColumn(&SubscriptionOrder{}, "billing_charge_protocol_version"))
	require.True(t, DB.Migrator().HasColumn(&TossTransactionReconciliationCursor{}, "recent_cursor_time"))
	require.True(t, DB.Migrator().HasColumn(&TossTransactionReconciliationCursor{}, "lease_owner"))
	require.True(t, DB.Migrator().HasColumn(&TossTransactionReconciliationCursor{}, "lease_until"))

	var migrated TopUp
	require.NoError(t, DB.First(&migrated, legacyTopUp.Id).Error)
	require.Empty(t, migrated.ProviderIdempotencyKey)
	require.Zero(t, migrated.ProviderConfirmProtocolVersion)
	var migratedSubscriptionOrder SubscriptionOrder
	require.NoError(t, DB.First(&migratedSubscriptionOrder, legacySubscriptionOrder.Id).Error)
	require.Zero(t, migratedSubscriptionOrder.BillingChargeProtocolVersion)
	var migratedCursor TossTransactionReconciliationCursor
	require.NoError(t, DB.First(&migratedCursor, "source_key = ?", legacyCursor.SourceKey).Error)
	require.Zero(t, migratedCursor.RecentCursorTime)
	require.Empty(t, migratedCursor.LeaseOwner)
	require.Zero(t, migratedCursor.LeaseUntil)

	const scopedMarker = "toss_confirm_persisted_new_node_marker"
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", legacyTopUp.Id).
		Updates(map[string]interface{}{
			"provider_idempotency_key":          scopedMarker,
			"provider_confirm_protocol_version": tossTopUpConfirmProtocolPaymentKey,
		}).Error)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", legacySubscriptionOrder.Id).
		Update("billing_charge_protocol_version", tossBillingChargeProtocolDurableAttempt).Error)
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", legacyCursor.SourceKey).
		Updates(map[string]interface{}{
			"recent_cursor_time": 250,
			"lease_owner":        "new-node-lease-owner",
			"lease_until":        500,
		}).Error)

	// Simulate writes from binaries whose structs do not contain either new
	// column. GORM must update only known columns and leave rollout state intact.
	legacyTopUp.Status = common.TopUpStatusFailed
	legacySubscriptionOrder.Status = common.TopUpStatusFailed
	legacyCursor.CursorTime = 150
	legacyCursor.UpdateTime = 150
	require.NoError(t, DB.Save(&legacyTopUp).Error)
	require.NoError(t, DB.Save(&legacySubscriptionOrder).Error)
	require.NoError(t, DB.Save(&legacyCursor).Error)

	require.NoError(t, DB.First(&migrated, legacyTopUp.Id).Error)
	require.Equal(t, scopedMarker, migrated.ProviderIdempotencyKey)
	require.Equal(t, tossTopUpConfirmProtocolPaymentKey, migrated.ProviderConfirmProtocolVersion)
	require.Equal(t, common.TopUpStatusFailed, migrated.Status)
	require.NoError(t, DB.First(&migratedSubscriptionOrder, legacySubscriptionOrder.Id).Error)
	require.Equal(t, tossBillingChargeProtocolDurableAttempt, migratedSubscriptionOrder.BillingChargeProtocolVersion)
	require.Equal(t, common.TopUpStatusFailed, migratedSubscriptionOrder.Status)
	require.NoError(t, DB.First(&migratedCursor, "source_key = ?", legacyCursor.SourceKey).Error)
	require.EqualValues(t, 150, migratedCursor.CursorTime)
	require.EqualValues(t, 250, migratedCursor.RecentCursorTime)
	require.Equal(t, "new-node-lease-owner", migratedCursor.LeaseOwner)
	require.EqualValues(t, 500, migratedCursor.LeaseUntil)
}

func TestPrepareClaimedTossTopUpConfirmPinsCredentialAndRolloutNamespace(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	require.NoError(t, DB.Create(&User{
		Id: 7, Username: "toss-rolling-owner", Status: common.UserStatusEnabled, AffCode: "toss-rolling-owner",
	}).Error)

	seed := func(tradeNo, paymentKey, claimToken, secret string, attempted bool, protocolVersion int) TopUp {
		t.Helper()
		credential, err := EncryptProviderCredential(secret)
		require.NoError(t, err)
		row := TopUp{
			UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
			Amount: 13000, TradeNo: tradeNo, ProviderOrderId: paymentKey,
			ProviderCredential: credential, ProviderAttempted: attempted,
			ProviderConfirmProtocolVersion: protocolVersion,
			ProviderClaimToken:             claimToken, PaymentMethod: PaymentMethodToss,
			PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		}
		require.NoError(t, DB.Create(&row).Error)
		return row
	}

	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	legacyCheckout := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_roll_created_legacy", ProviderOrderId: "toss_roll_created_legacy",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, CreatePendingTossTopUpCheckout(&legacyCheckout))
	require.Equal(t, tossTopUpConfirmProtocolLegacy, legacyCheckout.ProviderConfirmProtocolVersion)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	scopedCheckout := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_roll_created_scoped", ProviderOrderId: "toss_roll_created_scoped",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, CreatePendingTossTopUpCheckout(&scopedCheckout))
	require.Equal(t, tossTopUpConfirmProtocolPaymentKey, scopedCheckout.ProviderConfirmProtocolVersion)

	legacy := seed("toss_roll_legacy", "pay_roll_legacy", "claim-legacy", "sk_roll_legacy", true, tossTopUpConfirmProtocolLegacy)
	legacyKey, err := PrepareClaimedPendingTossTopUpConfirm(
		legacy.TradeNo, legacy.ProviderOrderId, legacy.Amount, legacy.ProviderClaimToken, "sk_roll_legacy",
	)
	require.NoError(t, err)
	require.Equal(t, legacy.TradeNo, legacyKey, "empty+attempted rows must stay in the old provider namespace")
	var storedLegacy TopUp
	require.NoError(t, DB.First(&storedLegacy, legacy.Id).Error)
	require.Empty(t, storedLegacy.ProviderIdempotencyKey)
	require.Equal(t, legacy.ProviderCredential, storedLegacy.ProviderCredential, "retry must preserve the exact ciphertext snapshot")

	_, err = PrepareClaimedPendingTossTopUpConfirm(
		legacy.TradeNo, legacy.ProviderOrderId, legacy.Amount, legacy.ProviderClaimToken, "sk_rotated_wrong",
	)
	require.ErrorIs(t, err, ErrTossTopUpCredentialConflict)
	require.NoError(t, DB.First(&storedLegacy, legacy.Id).Error)
	require.Equal(t, legacy.ProviderCredential, storedLegacy.ProviderCredential)

	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	gateOff := seed("toss_roll_gate_off", "pay_roll_gate_off", "claim-gate-off", "sk_roll_gate_off", false, tossTopUpConfirmProtocolLegacy)
	gateOffKey, err := PrepareClaimedPendingTossTopUpConfirm(
		gateOff.TradeNo, gateOff.ProviderOrderId, gateOff.Amount, gateOff.ProviderClaimToken, "sk_roll_gate_off",
	)
	require.NoError(t, err)
	require.Equal(t, gateOff.TradeNo, gateOffKey)
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "true")
	gateOffRetryKey, err := PrepareClaimedPendingTossTopUpConfirm(
		gateOff.TradeNo, gateOff.ProviderOrderId, gateOff.Amount, gateOff.ProviderClaimToken, "sk_roll_gate_off",
	)
	require.NoError(t, err)
	require.Equal(t, gateOff.TradeNo, gateOffRetryKey, "enabling later must not move an attempted legacy request")

	scoped := seed("toss_roll_scoped", "pay_roll_scoped", "claim-scoped", "sk_roll_scoped", false, tossTopUpConfirmProtocolPaymentKey)
	scopedKey, err := PrepareClaimedPendingTossTopUpConfirm(
		scoped.TradeNo, scoped.ProviderOrderId, scoped.Amount, scoped.ProviderClaimToken, "sk_roll_scoped",
	)
	require.NoError(t, err)
	require.NotEqual(t, scoped.TradeNo, scopedKey)
	require.True(t, strings.HasPrefix(scopedKey, "toss_confirm_"))
	var storedScoped TopUp
	require.NoError(t, DB.First(&storedScoped, scoped.Id).Error)
	require.Equal(t, scopedKey, storedScoped.ProviderIdempotencyKey)
	require.Equal(t, scoped.ProviderCredential, storedScoped.ProviderCredential)

	// A temporarily stale environment on another new binary still honors the
	// durable marker. This is the dual-reader half of the rolling deployment.
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	scopedRetryKey, err := PrepareClaimedPendingTossTopUpConfirm(
		scoped.TradeNo, scoped.ProviderOrderId, scoped.Amount, scoped.ProviderClaimToken, "sk_roll_scoped",
	)
	require.NoError(t, err)
	require.Equal(t, scopedKey, scopedRetryKey)
}

func TestTossPaymentKeyScopedIdempotencyDefaultsSecurely(t *testing.T) {
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "")
	require.True(t, IsTossPaymentKeyScopedIdempotencyEnabled(), "an omitted rollout override must create scoped checkouts")

	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "not-a-boolean")
	require.True(t, IsTossPaymentKeyScopedIdempotencyEnabled(), "a malformed override must fail secure")

	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	require.False(t, IsTossPaymentKeyScopedIdempotencyEnabled(), "explicit false remains available for a checkout-paused mixed rollout")
}

func TestPrepareClaimedTossTopUpConfirmRejectsUnreadableAttemptCredential(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	require.NoError(t, DB.Create(&User{
		Id: 7, Username: "toss-corrupt-owner", Status: common.UserStatusEnabled, AffCode: "toss-corrupt-owner",
	}).Error)
	row := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_roll_corrupt", ProviderOrderId: "pay_roll_corrupt",
		ProviderCredential: "corrupt-encrypted-credential", ProviderAttempted: true,
		ProviderClaimToken: "claim-corrupt", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&row).Error)

	_, err := PrepareClaimedPendingTossTopUpConfirm(
		row.TradeNo, row.ProviderOrderId, row.Amount, row.ProviderClaimToken, "sk_must_not_replace_corrupt",
	)
	require.ErrorIs(t, err, ErrTossTopUpCredentialConflict)
	var stored TopUp
	require.NoError(t, DB.First(&stored, row.Id).Error)
	require.Equal(t, "corrupt-encrypted-credential", stored.ProviderCredential)
	require.True(t, stored.ProviderAttempted)
}

func TestResetClaimedTossTopUpPaymentBindingRespectsDurableProtocolVersion(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	require.NoError(t, DB.Create(&User{
		Id: 7, Username: "toss-reset-owner", Status: common.UserStatusEnabled, AffCode: "toss-reset-owner",
	}).Error)
	credential, err := EncryptProviderCredential("sk_reset_scoped")
	require.NoError(t, err)
	scoped := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_reset_scoped", ProviderOrderId: "pay_reset_forged",
		ProviderCredential: credential, ProviderConfirmProtocolVersion: tossTopUpConfirmProtocolPaymentKey,
		ProviderClaimToken: "claim-reset-pristine", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&scoped).Error)
	require.True(t, IsPristinePaymentKeyScopedTossTopUp(&scoped))

	reset, err := ResetClaimedPendingTossTopUpPaymentBinding(
		scoped.TradeNo, scoped.ProviderOrderId, scoped.Amount, scoped.ProviderClaimToken,
	)
	require.NoError(t, err)
	require.True(t, reset, "v1 before its first attempt can safely discard a proved-invalid callback key")
	var stored TopUp
	require.NoError(t, DB.First(&stored, scoped.Id).Error)
	require.Equal(t, stored.TradeNo, stored.ProviderOrderId)
	require.Equal(t, tossTopUpConfirmProtocolPaymentKey, stored.ProviderConfirmProtocolVersion)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderIdempotencyKey)
	require.True(t, IsPristinePaymentKeyScopedTossTopUp(&stored))

	// The restored checkout remains v1. A later legitimate paymentKey therefore
	// gets a distinct scoped provider namespace even if the process environment
	// has since become stale.
	t.Setenv(TossPaymentKeyScopedIdempotencyEnv, "false")
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", scoped.Id).Updates(map[string]interface{}{
		"provider_order_id":    "pay_reset_legitimate",
		"provider_claim_token": "claim-reset-legitimate",
	}).Error)
	key, err := PrepareClaimedPendingTossTopUpConfirm(
		scoped.TradeNo, "pay_reset_legitimate", scoped.Amount, "claim-reset-legitimate", "sk_reset_scoped",
	)
	require.NoError(t, err)
	require.NotEqual(t, scoped.TradeNo, key)

	unsafe := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_reset_inconsistent", ProviderOrderId: "pay_reset_inconsistent",
		ProviderCredential: credential, ProviderAttempted: true,
		ProviderConfirmProtocolVersion: tossTopUpConfirmProtocolPaymentKey,
		ProviderClaimToken:             "claim-reset-inconsistent", PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&unsafe).Error)
	_, err = ResetClaimedPendingTossTopUpPaymentBinding(
		unsafe.TradeNo, unsafe.ProviderOrderId, unsafe.Amount, unsafe.ProviderClaimToken,
	)
	require.ErrorIs(t, err, ErrTossTopUpBindingResetUnsafe, "v1 attempted without its atomic key marker must fail closed")

	legacy := TopUp{
		UserId: 7, TargetType: TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, TradeNo: "toss_reset_legacy", ProviderOrderId: "pay_reset_legacy",
		ProviderCredential: credential, ProviderClaimToken: "claim-reset-legacy",
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&legacy).Error)
	_, err = ResetClaimedPendingTossTopUpPaymentBinding(
		legacy.TradeNo, legacy.ProviderOrderId, legacy.Amount, legacy.ProviderClaimToken,
	)
	require.ErrorIs(t, err, ErrTossTopUpBindingResetLegacy)
}
