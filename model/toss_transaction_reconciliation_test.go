package model

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestTossClientKeyFingerprintValidationRequiresCanonicalSHA256(t *testing.T) {
	valid := TossClientKeyFingerprint("live_ck_transaction_fingerprint_validation")
	require.True(t, IsValidTossClientKeyFingerprint(valid))
	for _, invalid := range []string{
		"",
		"not-a-fingerprint",
		strings.Repeat("g", 64),
		strings.ToUpper(valid),
		" " + valid,
		valid + " ",
		valid[:len(valid)-1],
	} {
		require.False(t, IsValidTossClientKeyFingerprint(invalid), invalid)
	}
}

func TestTossTransactionCredentialSourceIndexesAreCompositeAndIdempotent(t *testing.T) {
	setupTossBillingModelTestDB(t)
	models := []struct {
		value     any
		indexName string
	}{
		{value: &TopUp{}, indexName: "idx_topup_toss_credential_source"},
		{value: &SubscriptionOrder{}, indexName: "idx_subscription_order_toss_credential_source"},
	}
	for _, item := range models {
		parsed, err := schema.Parse(item.value, &sync.Map{}, schema.NamingStrategy{})
		require.NoError(t, err)
		index, ok := parsed.ParseIndexes()[item.indexName]
		require.True(t, ok, "missing %s", item.indexName)
		require.Len(t, index.Fields, 3)
		require.Equal(t, "payment_provider", index.Fields[0].DBName)
		require.Equal(t, "provider_client_key_hash", index.Fields[1].DBName)
		require.Equal(t, "create_time", index.Fields[2].DBName)

		// Re-running AutoMigrate is the normal deployment path and must not create
		// duplicate indexes or fail on an existing composite index.
		require.NoError(t, DB.AutoMigrate(item.value))
		require.NoError(t, DB.AutoMigrate(item.value))
		require.True(t, DB.Migrator().HasIndex(item.value, item.indexName))
	}
}

func TestTossTransactionReconciliationCursorPersistsAndCASAdvances(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossTransactionReconciliationCursor{}))

	row, err := GetOrCreateTossTransactionReconciliationCursor("mid-source", 100)
	require.NoError(t, err)
	require.EqualValues(t, 100, row.CursorTime)

	row, err = GetOrCreateTossTransactionReconciliationCursor("mid-source", 50)
	require.NoError(t, err)
	require.EqualValues(t, 100, row.CursorTime, "an existing cursor must never rewind")

	require.NoError(t, AdvanceTossTransactionReconciliationCursor("mid-source", 100, 200))
	require.ErrorIs(t, AdvanceTossTransactionReconciliationCursor("mid-source", 100, 300), ErrTossTransactionCursorConflict)
	require.NoError(t, DB.First(row, "source_key = ?", "mid-source").Error)
	require.EqualValues(t, 200, row.CursorTime)
}

func TestTossTransactionReconciliationSourceLeaseSerializesExpiresAndPreservesAliasCAS(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&TossTransactionReconciliationCursor{},
		&TossTransactionReconciliationPageCursor{},
	))
	const (
		sourceKey = "mid-source-lease"
		aliasKey  = "legacy-source-lease"
		ownerA    = "lease-owner-a"
		ownerB    = "lease-owner-b"
	)
	require.NoError(t, DB.Create(&TossTransactionReconciliationCursor{
		SourceKey: aliasKey, CursorTime: 100, UpdateTime: GetDBTimestamp(),
	}).Error)
	cursor, aliases, err := GetOrCreateTossTransactionReconciliationCursorWithAliases(
		sourceKey,
		200,
		[]TossTransactionReconciliationCursorAlias{{SourceKey: aliasKey}},
	)
	require.NoError(t, err)
	require.EqualValues(t, 100, cursor.CursorTime, "the alias bridge must preserve the oldest high-water mark")
	require.Equal(t, []string{aliasKey}, aliases)

	acquired, err := AcquireTossTransactionReconciliationLeaseWithContext(context.Background(), sourceKey, ownerA, 60)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = AcquireTossTransactionReconciliationLeaseWithContext(context.Background(), sourceKey, ownerB, 60)
	require.NoError(t, err)
	require.False(t, acquired, "a second node must not enter the same provider source")

	// Emulate MySQL's changed-rows behavior for a same-second, same-owner
	// renewal. SQLite/PostgreSQL normally report the matched row; the reload
	// fallback must make both semantics equivalent.
	forceNextTossAttemptUpdateRowsAffectedZero(t, "toss_transaction_reconciliation_cursors", "lease_owner")
	acquired, err = AcquireTossTransactionReconciliationLeaseWithContext(context.Background(), sourceKey, ownerA, 60)
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(
		context.Background(), sourceKey, aliases, ownerA, 100, 200,
	))
	require.NoError(t, AdvanceTossTransactionReconciliationRecentCursorWithAliasesLeasedWithContext(
		context.Background(), sourceKey, aliases, ownerA, 200, 0, 150,
	))
	var bridged []TossTransactionReconciliationCursor
	require.NoError(t, DB.Where("source_key IN ?", []string{sourceKey, aliasKey}).Order("source_key").Find(&bridged).Error)
	require.Len(t, bridged, 2)
	for i := range bridged {
		require.EqualValues(t, 200, bridged[i].CursorTime)
		require.EqualValues(t, 150, bridged[i].RecentCursorTime)
	}
	var leased TossTransactionReconciliationCursor
	require.NoError(t, DB.First(&leased, "source_key = ?", sourceKey).Error)
	require.Equal(t, ownerA, leased.LeaseOwner, "cursor and alias advances must not erase the source lease")
	page, err := GetOrCreateTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneHistorical, ownerA, 200, 190, 250,
	)
	require.NoError(t, err)
	require.Empty(t, page.StartingAfter)

	// Simulate a process crash by leaving its owner token behind and expiring
	// only the durable deadline. A successor can acquire, and the stale worker
	// can no longer create, advance, or complete page checkpoints, nor publish
	// either higher-level cursor.
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", sourceKey).
		Update("lease_until", GetDBTimestamp()-1).Error)
	acquired, err = AcquireTossTransactionReconciliationLeaseWithContext(context.Background(), sourceKey, ownerB, 60)
	require.NoError(t, err)
	require.True(t, acquired)
	_, err = GetOrCreateTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneRecent, ownerA, 150, 140, 160,
	)
	require.ErrorIs(t, err, ErrTossTransactionLeaseLost)
	require.ErrorIs(t, AdvanceTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneHistorical, ownerA,
		200, 190, 250, "", "transaction-after-page-one",
	), ErrTossTransactionLeaseLost)
	var unchangedPage TossTransactionReconciliationPageCursor
	require.NoError(t, DB.Where("source_key = ? AND lane = ?", sourceKey, TossTransactionPageLaneHistorical).
		First(&unchangedPage).Error)
	require.Empty(t, unchangedPage.StartingAfter)
	require.NoError(t, AdvanceTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneHistorical, ownerB,
		200, 190, 250, "", "transaction-after-page-one",
	))
	require.ErrorIs(t, CompleteTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneHistorical, ownerA,
		200, 190, 250, "transaction-after-page-one",
	), ErrTossTransactionLeaseLost)
	require.NoError(t, CompleteTossTransactionReconciliationPageCursorLeasedWithContext(
		context.Background(), sourceKey, TossTransactionPageLaneHistorical, ownerB,
		200, 190, 250, "transaction-after-page-one",
	))
	require.ErrorIs(t, AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(
		context.Background(), sourceKey, aliases, ownerA, 200, 300,
	), ErrTossTransactionLeaseLost)
	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(
		context.Background(), sourceKey, aliases, ownerB, 200, 300,
	))
	require.NoError(t, ReleaseTossTransactionReconciliationLeaseWithContext(context.Background(), sourceKey, ownerB))
}

func TestTossTransactionReconciliationPageCursorResumesAndReplacesOnlyAfterTimeCursorMoves(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&TossTransactionReconciliationCursor{},
		&TossTransactionReconciliationPageCursor{},
	))
	const sourceKey = "mid-page-cursor-source"
	require.NoError(t, DB.Create(&TossTransactionReconciliationCursor{
		SourceKey:        sourceKey,
		CursorTime:       100,
		RecentCursorTime: 500,
		UpdateTime:       100,
	}).Error)

	page, err := GetOrCreateTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 100, 90, 200,
	)
	require.NoError(t, err)
	require.Empty(t, page.StartingAfter)
	require.NoError(t, AdvanceTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 100, 90, 200, "", "tx-page-2",
	))

	page, err = GetOrCreateTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 100, 90, 250,
	)
	require.NoError(t, err)
	require.EqualValues(t, 200, page.EndTime, "a resumed scan must finish its original closed interval")
	require.Equal(t, "tx-page-2", page.StartingAfter)
	_, err = GetOrCreateTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 100, 91, 250,
	)
	require.ErrorIs(t, err, ErrTossTransactionPageCursorConflict)

	// The recent tail owns an independent checkpoint and cannot be blocked by a
	// long historical backlog.
	recent, err := GetOrCreateTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneRecent, 500, 490, 600,
	)
	require.NoError(t, err)
	require.Equal(t, TossTransactionPageLaneRecent, recent.Lane)

	// Once the authoritative time cursor moves, an old page row is stale. A new
	// worker atomically replaces it; an in-flight old worker's later CAS fails.
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", sourceKey).
		Update("cursor_time", 200).Error)
	page, err = GetOrCreateTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 200, 190, 300,
	)
	require.NoError(t, err)
	require.EqualValues(t, 200, page.CursorTime)
	require.Empty(t, page.StartingAfter)
	require.ErrorIs(t, AdvanceTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 100, 90, 200, "tx-page-2", "tx-page-3",
	), ErrTossTransactionPageCursorConflict)
	require.NoError(t, CompleteTossTransactionReconciliationPageCursor(
		sourceKey, TossTransactionPageLaneHistorical, 200, 190, 300, "",
	))
}

func TestTossTransactionReconciliationCursorWritesHonorCanceledContext(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(
		&TossTransactionReconciliationCursor{},
		&TossTransactionReconciliationPageCursor{},
	))
	require.NoError(t, DB.Create(&TossTransactionReconciliationCursor{
		SourceKey: "canceled-page-source", CursorTime: 100, UpdateTime: 100,
	}).Error)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GetOrCreateTossTransactionReconciliationPageCursorWithContext(
		ctx, "canceled-page-source", TossTransactionPageLaneHistorical, 100, 90, 200,
	)
	require.ErrorIs(t, err, context.Canceled)
	var count int64
	require.NoError(t, DB.Model(&TossTransactionReconciliationPageCursor{}).Count(&count).Error)
	require.Zero(t, count)

	_, _, err = GetOrCreateTossTransactionReconciliationCursorWithAliasesWithContext(
		ctx, "another-canceled-source", 100, nil,
	)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTossTransactionReconciliationCursorAliasBridgeIsOneWayAndGapFree(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossTransactionReconciliationCursor{}))

	const (
		targetKey = "mid-cursor-source"
		aliasKey  = "legacy-secret-source"
	)
	// A legacy cursor can be introduced after the MID cursor already exists.
	// Ensuring the bridge must rewind the target exactly once to the older mark.
	require.NoError(t, DB.Create(&TossTransactionReconciliationCursor{
		SourceKey: targetKey, CursorTime: 200, RecentCursorTime: 1000, UpdateTime: 200,
	}).Error)
	row, aliases, err := GetOrCreateTossTransactionReconciliationCursorWithAliases(
		targetKey,
		300,
		[]TossTransactionReconciliationCursorAlias{{SourceKey: aliasKey, InitialCursor: 50}},
	)
	require.NoError(t, err)
	require.EqualValues(t, 50, row.CursorTime)
	require.Equal(t, []string{aliasKey}, aliases)

	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliases(targetKey, aliases, 50, 100))
	var cursors []TossTransactionReconciliationCursor
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.Len(t, cursors, 2)
	require.EqualValues(t, 100, cursors[0].CursorTime)
	require.EqualValues(t, 100, cursors[1].CursorTime)
	require.EqualValues(t, 1000, cursors[0].RecentCursorTime)
	require.EqualValues(t, 1000, cursors[1].RecentCursorTime)

	// A mixed-version worker may move the alias inside the target's next window.
	// Since the target scans the entire [100,200] interval, the alias can safely
	// join it at 200 instead of becoming a permanent alias<target conflict.
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", aliasKey).Update("cursor_time", 150).Error)
	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliases(targetKey, aliases, 100, 200))
	cursors = nil
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.EqualValues(t, 200, cursors[0].CursorTime)
	require.EqualValues(t, 200, cursors[1].CursorTime)

	// An alias already beyond this target window is never rewound.
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", aliasKey).Update("cursor_time", 250).Error)
	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliases(targetKey, aliases, 200, 225))
	cursors = nil
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.EqualValues(t, 250, cursors[0].CursorTime)
	require.EqualValues(t, 225, cursors[1].CursorTime)

	// The following wider target window catches up without rewinding the alias.
	require.NoError(t, AdvanceTossTransactionReconciliationCursorWithAliases(targetKey, aliases, 225, 300))
	cursors = nil
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.EqualValues(t, 300, cursors[0].CursorTime)
	require.EqualValues(t, 300, cursors[1].CursorTime)
}

func TestTossTransactionReconciliationRecentCursorMergesNewestAliasAndCASAdvances(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossTransactionReconciliationCursor{}))

	const (
		targetKey = "mid-recent-cursor-source"
		aliasKey  = "legacy-recent-cursor-source"
	)
	require.NoError(t, DB.Create(&[]TossTransactionReconciliationCursor{
		{SourceKey: targetKey, CursorTime: 200, RecentCursorTime: 1000, UpdateTime: 200},
		{SourceKey: aliasKey, CursorTime: 50, RecentCursorTime: 1500, UpdateTime: 50},
	}).Error)

	row, aliases, err := GetOrCreateTossTransactionReconciliationCursorWithAliases(
		targetKey,
		300,
		[]TossTransactionReconciliationCursorAlias{{SourceKey: aliasKey}},
	)
	require.NoError(t, err)
	require.EqualValues(t, 50, row.CursorTime, "historical cursor must merge to the oldest alias")
	require.EqualValues(t, 1500, row.RecentCursorTime, "recent cursor must merge to the newest completed scan")
	require.Equal(t, []string{aliasKey}, aliases)

	var cursors []TossTransactionReconciliationCursor
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.Len(t, cursors, 2)
	for i := range cursors {
		require.EqualValues(t, 1500, cursors[i].RecentCursorTime)
	}

	require.NoError(t, AdvanceTossTransactionReconciliationRecentCursorWithAliases(targetKey, aliases, 50, 1500, 1600))
	cursors = nil
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	for i := range cursors {
		require.EqualValues(t, 1600, cursors[i].RecentCursorTime)
	}

	// A mixed-version worker may advance the historical target while the recent
	// provider scan is running. The stale recent CAS must publish nothing, so the
	// next run repeats that idempotent window from 1600.
	require.NoError(t, DB.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", targetKey).Update("cursor_time", 75).Error)
	require.ErrorIs(t,
		AdvanceTossTransactionReconciliationRecentCursorWithAliases(targetKey, aliases, 50, 1600, 1700),
		ErrTossTransactionCursorConflict,
	)
	cursors = nil
	require.NoError(t, DB.Where("source_key IN ?", []string{targetKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	for i := range cursors {
		require.EqualValues(t, 1600, cursors[i].RecentCursorTime)
	}
}

func TestRecoveredTossTopUpCannotSettleAfterAuthoritativeCancellation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&User{}, &TopUp{}, &TossPaymentEvent{}))
	require.NoError(t, DB.Create(&User{
		Id:       71,
		Username: "recovered-cancelled-topup",
		Status:   common.UserStatusEnabled,
	}).Error)
	const (
		orderID    = "toss_recovered_cancelled"
		paymentKey = "pay_recovered_cancelled"
	)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          71,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        71,
		Amount:          13000,
		Quota:           777,
		TradeNo:         orderID,
		ProviderOrderId: paymentKey,
		PaymentMethod:   PaymentMethodToss,
		PaymentProvider: PaymentProviderToss,
		Status:          common.TopUpStatusExpired,
	}).Error)
	require.NoError(t, DB.Create(&TossPaymentEvent{
		EventKey:             "cancel_recovered_topup",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               "CANCELED",
		ReconciliationStatus: TossReconciliationStatusRequired,
	}).Error)

	require.NoError(t, RecoverTossTopUpPaymentKeyWithContext(context.Background(), orderID, paymentKey))
	err := RechargeTossWithContext(context.Background(), orderID, paymentKey, "toss-recovery-test")
	require.ErrorIs(t, err, ErrTossCancellationPrecedesFulfillment)

	stored, err := GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
	var user User
	require.NoError(t, DB.First(&user, 71).Error)
	require.Zero(t, user.Quota)
}
