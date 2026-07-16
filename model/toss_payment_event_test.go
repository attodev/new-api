package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRecordTossPaymentEventIsIdempotent(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossPaymentEvent{}))

	event := &TossPaymentEvent{
		EventKey:       "toss-cancel-event-1",
		EventType:      TossPaymentEventTypeCancellation,
		OrderId:        "toss_order_1",
		PaymentKey:     "pay_1",
		Status:         "PARTIAL_CANCELED",
		TransactionKey: "cancel_tx_1",
		CancelAmount:   300,
		BalanceAmount:  700,
		OriginalAmount: 1000,
	}
	created, err := RecordTossPaymentEvent(event)
	require.NoError(t, err)
	require.True(t, created)

	duplicate := *event
	duplicate.Id = 0
	created, err = RecordTossPaymentEvent(&duplicate)
	require.NoError(t, err)
	require.False(t, created)

	var rows []TossPaymentEvent
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, int64(300), rows[0].CancelAmount)
	require.Equal(t, TossReconciliationStatusRequired, rows[0].ReconciliationStatus)

	require.Error(t, ResolveTossPaymentEvents("toss_order_1", TossPaymentEventTypeCancellation))
	require.NoError(t, DB.First(&rows[0], rows[0].Id).Error)
	require.Equal(t, TossReconciliationStatusRequired, rows[0].ReconciliationStatus)
	_, err = ResolveTossPaymentEventByAdmin(rows[0].Id, 1, "cancellation reviewed")
	require.NoError(t, err)
	require.NoError(t, DB.First(&rows[0], rows[0].Id).Error)
	require.Equal(t, TossReconciliationStatusResolved, rows[0].ReconciliationStatus)
	require.Equal(t, "cancellation reviewed", rows[0].ResolutionNote)
	require.Positive(t, rows[0].ResolvedTime)
	require.Equal(t, 1, rows[0].ResolvedBy)
}

func TestResolveTossPaymentEventsAutomaticallyResolvesOnlyFulfillment(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossPaymentEvent{}))

	orderID := "toss_auto_resolve_scope"
	for _, event := range []*TossPaymentEvent{
		{EventKey: "auto-resolve-fulfillment", EventType: TossPaymentEventTypeFulfillment, OrderId: orderID},
		{EventKey: "auto-resolve-cancellation", EventType: TossPaymentEventTypeCancellation, OrderId: orderID},
		{EventKey: "auto-resolve-financial", EventType: TossPaymentEventTypeFinancialMismatch, OrderId: orderID},
		{EventKey: "auto-resolve-refund-required", EventType: TossPaymentEventTypeRefundRequired, OrderId: orderID, PaymentKey: "pay_refund_fence", OriginalAmount: 1000},
	} {
		created, err := RecordTossPaymentEvent(event)
		require.NoError(t, err)
		require.True(t, created)
	}
	require.Error(t, ResolveTossPaymentEvents(orderID, ""))
	require.Error(t, ResolveTossPaymentEvents(orderID, TossPaymentEventTypeFinancialMismatch))
	require.NoError(t, ResolveTossPaymentEvents(orderID, TossPaymentEventTypeFulfillment))

	var events []TossPaymentEvent
	require.NoError(t, DB.Where("order_id = ?", orderID).Find(&events).Error)
	require.Len(t, events, 4)
	for i := range events {
		if events[i].EventType == TossPaymentEventTypeFulfillment {
			require.Equal(t, TossReconciliationStatusResolved, events[i].ReconciliationStatus)
		} else {
			require.Equal(t, TossReconciliationStatusRequired, events[i].ReconciliationStatus)
		}
		if events[i].EventType == TossPaymentEventTypeRefundRequired {
			_, err := ResolveTossPaymentEventByAdmin(events[i].Id, 1, "claimed manual refund")
			require.ErrorIs(t, err, ErrTossPaymentEventStatusInvalid)
		}
	}
}

func TestRecordTossPaymentEventRejectsSemanticKeyCollision(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossPaymentEvent{}))
	original := &TossPaymentEvent{
		EventKey:       "semantic-collision-key",
		EventType:      TossPaymentEventTypeFinancialMismatch,
		OrderId:        "toss_semantic_collision",
		PaymentKey:     "pay_semantic_collision",
		Status:         "DONE",
		BalanceAmount:  900,
		OriginalAmount: 900,
	}
	created, err := RecordTossPaymentEvent(original)
	require.NoError(t, err)
	require.True(t, created)

	mutations := []func(*TossPaymentEvent){
		func(event *TossPaymentEvent) { event.EventType = TossPaymentEventTypeFulfillment },
		func(event *TossPaymentEvent) { event.OrderId = "toss_other_order" },
		func(event *TossPaymentEvent) { event.PaymentKey = "pay_other_payment" },
		func(event *TossPaymentEvent) { event.OriginalAmount++ },
	}
	for _, mutate := range mutations {
		conflict := *original
		conflict.Id = 0
		conflict.CreateToken = ""
		mutate(&conflict)
		created, err = RecordTossPaymentEvent(&conflict)
		require.False(t, created)
		require.ErrorIs(t, err, ErrTossPaymentEventKeyConflict)
	}

	var stored TossPaymentEvent
	require.NoError(t, DB.Where("event_key = ?", original.EventKey).First(&stored).Error)
	require.Equal(t, TossPaymentEventTypeFinancialMismatch, stored.EventType)
	require.Equal(t, int64(900), stored.OriginalAmount)
}

func TestRecordTossCancellationEventsKeepsCumulativeLineageAfterDetailedRedelivery(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &TossPaymentEvent{}))
	require.NoError(t, DB.Create(&TopUp{
		TradeNo:         "toss_cancel_lineage",
		PaymentProvider: PaymentProviderToss,
		PaymentMethod:   PaymentMethodToss,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	legacy := &TossPaymentEvent{
		EventKey:               "toss_cancel_legacy_snapshot_300",
		EventType:              TossPaymentEventTypeCancellation,
		OrderId:                "toss_cancel_lineage",
		PaymentKey:             "pay_cancel_lineage",
		Status:                 "PARTIAL_CANCELED",
		CancelAmount:           300,
		CancelAmountCumulative: true,
		BalanceAmount:          700,
		OriginalAmount:         1000,
		ProviderPayload:        `{"status":"PARTIAL_CANCELED","balanceAmount":700}`,
	}
	created, amount, err := RecordTossCancellationEventsForOrderWithContext(context.Background(), legacy.OrderId, []*TossPaymentEvent{legacy})
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(300), amount)

	// The same refund later arrives with its transactionKey. It must not create
	// a second operator task or report another 300 KRW as newly canceled.
	detailedSame := &TossPaymentEvent{
		EventKey:             "toss_cancel_transaction_1",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              legacy.OrderId,
		PaymentKey:           legacy.PaymentKey,
		Status:               "PARTIAL_CANCELED",
		TransactionKey:       "cancel_transaction_1",
		CancelAmount:         300,
		BalanceAmount:        700,
		OriginalAmount:       1000,
		ProviderPayload:      `{"cancels":[{"transactionKey":"cancel_transaction_1","cancelAmount":300}]}`,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, amount, err = RecordTossCancellationEventsForOrderWithContext(context.Background(), legacy.OrderId, []*TossPaymentEvent{detailedSame})
	require.NoError(t, err)
	require.Zero(t, created)
	require.Zero(t, amount)

	// A later second refund is represented as the next cumulative snapshot. Only
	// the 200 KRW delta is new even though the detailed response contains both
	// provider transactions.
	detailedLater := []*TossPaymentEvent{
		{
			EventKey:             "toss_cancel_transaction_1_again",
			EventType:            TossPaymentEventTypeCancellation,
			OrderId:              legacy.OrderId,
			PaymentKey:           legacy.PaymentKey,
			Status:               "PARTIAL_CANCELED",
			TransactionKey:       "cancel_transaction_1",
			CancelAmount:         300,
			BalanceAmount:        500,
			OriginalAmount:       1000,
			ProviderPayload:      `{"balanceAmount":500,"cancels":[{"transactionKey":"cancel_transaction_1","cancelAmount":300},{"transactionKey":"cancel_transaction_2","cancelAmount":200}]}`,
			ReconciliationStatus: TossReconciliationStatusRequired,
		},
		{
			EventKey:             "toss_cancel_transaction_2",
			EventType:            TossPaymentEventTypeCancellation,
			OrderId:              legacy.OrderId,
			PaymentKey:           legacy.PaymentKey,
			Status:               "PARTIAL_CANCELED",
			TransactionKey:       "cancel_transaction_2",
			CancelAmount:         200,
			BalanceAmount:        500,
			OriginalAmount:       1000,
			ProviderPayload:      `{"balanceAmount":500}`,
			ReconciliationStatus: TossReconciliationStatusRequired,
		},
	}
	created, amount, err = RecordTossCancellationEventsForOrderWithContext(context.Background(), legacy.OrderId, detailedLater)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(200), amount)

	var rows []TossPaymentEvent
	require.NoError(t, DB.Where("payment_key = ?", legacy.PaymentKey).Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	for i := range rows {
		require.True(t, rows[i].CancelAmountCumulative)
		require.Empty(t, rows[i].TransactionKey)
	}
}

func TestTossPaymentEventAdminListDetailAndResolve(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossPaymentEvent{}))

	cancellation := &TossPaymentEvent{
		EventKey:             "toss-admin-cancel-1",
		EventType:            TossPaymentEventTypeCancellation,
		OrderId:              "toss_admin_order_1",
		PaymentKey:           "pay_admin_1",
		Status:               "PARTIAL_CANCELED",
		CancelAmount:         400,
		BalanceAmount:        600,
		OriginalAmount:       1000,
		ProviderPayload:      `{"paymentKey":"pay_admin_1"}`,
		ReconciliationStatus: TossReconciliationStatusRequired,
	}
	created, err := RecordTossPaymentEvent(cancellation)
	require.NoError(t, err)
	require.True(t, created)
	_, err = RecordTossPaymentEvent(&TossPaymentEvent{
		EventKey:             "toss-admin-fulfillment-1",
		EventType:            TossPaymentEventTypeFulfillment,
		OrderId:              "toss_admin_order_2",
		PaymentKey:           "pay_admin_2",
		Status:               "DONE",
		OriginalAmount:       2000,
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	events, total, err := ListTossPaymentEvents(TossPaymentEventFilter{
		ReconciliationStatus: TossReconciliationStatusRequired,
		EventType:            TossPaymentEventTypeCancellation,
	}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, events, 1)
	require.Equal(t, cancellation.EventKey, events[0].EventKey)

	detail, err := GetTossPaymentEventById(cancellation.Id)
	require.NoError(t, err)
	require.Equal(t, cancellation.ProviderPayload, detail.ProviderPayload)

	resolved, err := ResolveTossPaymentEventByAdmin(cancellation.Id, 91, "Refund verified in Toss console; quota adjusted manually.")
	require.NoError(t, err)
	require.Equal(t, TossReconciliationStatusResolved, resolved.ReconciliationStatus)
	require.Equal(t, 91, resolved.ResolvedBy)
	require.Positive(t, resolved.ResolvedTime)
	require.Equal(t, "Refund verified in Toss console; quota adjusted manually.", resolved.ResolutionNote)

	// A duplicate admin request is an idempotent read and must not overwrite the
	// original resolver's audit record.
	again, err := ResolveTossPaymentEventByAdmin(cancellation.Id, 92, "different note")
	require.NoError(t, err)
	require.Equal(t, 91, again.ResolvedBy)
	require.Equal(t, resolved.ResolvedTime, again.ResolvedTime)
	require.Equal(t, resolved.ResolutionNote, again.ResolutionNote)

	events, total, err = ListTossPaymentEvents(TossPaymentEventFilter{
		ReconciliationStatus: TossReconciliationStatusRequired,
	}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, events, 1)
	require.Equal(t, TossPaymentEventTypeFulfillment, events[0].EventType)

	_, err = ResolveTossPaymentEventByAdmin(99999, 91, "not found")
	require.ErrorIs(t, err, ErrTossPaymentEventNotFound)
}
