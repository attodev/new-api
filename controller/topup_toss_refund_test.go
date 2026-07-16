package controller

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTossTopUpContractRejectsBrowserControlledTaxAndEscrow(t *testing.T) {
	base := tossConfirmResponse{
		PaymentKey: "pay_contract_tax", Type: "NORMAL", OrderId: "toss_contract_tax",
		Status: "DONE", TotalAmount: 13000, BalanceAmount: 13000, Currency: "KRW",
		Method: "카드", Card: &tossPaymentCard{Amount: 13000},
	}
	require.True(t, isValidTossTopUpPayment(&base, base.OrderId, base.TotalAmount))

	for _, mutate := range []func(*tossConfirmResponse){
		func(payment *tossConfirmResponse) { payment.TaxFreeAmount = 1 },
		func(payment *tossConfirmResponse) { payment.TaxExemptionAmount = 1 },
		func(payment *tossConfirmResponse) { payment.UseEscrow = true },
		func(payment *tossConfirmResponse) { payment.CultureExpense = true },
	} {
		payment := base
		card := *base.Card
		payment.Card = &card
		mutate(&payment)
		require.False(t, isValidTossTopUpPayment(&payment, payment.OrderId, payment.TotalAmount))
	}
}

func TestTossTopUpRefundOperationPinsKeyUntilBalanceProgressOrRetentionExpiry(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}))
	const (
		orderID    = "toss_refund_key_balance"
		paymentKey = "pay_refund_key_balance"
	)
	require.NoError(t, model.DB.Create(&model.User{Id: 68, Username: "toss-refund-operation"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 68, Amount: 13000, TradeNo: orderID, ProviderOrderId: paymentKey,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	_, err := model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 13000, BalanceAmount: 13000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	first, err := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 13000)
	require.NoError(t, err)
	require.NotEmpty(t, first.IdempotencyKey)
	require.False(t, first.Rotated)

	retry, err := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 13000)
	require.NoError(t, err)
	require.Equal(t, first, retry, "hourly workers must reuse an ambiguous operation for the full provider retention window")

	remainder, err := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 3000)
	require.NoError(t, err)
	require.True(t, remainder.Rotated)
	require.NotEqual(t, first.IdempotencyKey, remainder.IdempotencyKey,
		"a lower authoritative balance proves progress and permits a new remainder operation")

	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).
		Update("refund_operation_time", model.GetDBTimestamp()-model.TossProviderIdempotencyRetentionSeconds).Error)
	afterRetention, err := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 3000)
	require.NoError(t, err)
	require.True(t, afterRetention.Rotated)
	require.NotEqual(t, remainder.IdempotencyKey, afterRetention.IdempotencyKey,
		"an unchanged balance may use a new key only after Toss's 15-day retention expires")

	_, err = model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 4000)
	require.ErrorIs(t, err, model.ErrTossRefundBalanceIncreased)

	validState := map[string]interface{}{
		"refund_operation_key":     afterRetention.IdempotencyKey,
		"refund_operation_time":    afterRetention.FirstAttemptTime,
		"refund_operation_balance": afterRetention.BalanceAmount,
	}
	for name, corrupt := range map[string]map[string]interface{}{
		"future time": {
			"refund_operation_key":     afterRetention.IdempotencyKey,
			"refund_operation_time":    model.GetDBTimestamp() + int64(time.Hour/time.Second),
			"refund_operation_balance": afterRetention.BalanceAmount,
		},
		"malformed key": {
			"refund_operation_key":     "toss_refund_" + strings.Repeat("z", 40),
			"refund_operation_time":    afterRetention.FirstAttemptTime,
			"refund_operation_balance": afterRetention.BalanceAmount,
		},
		"oversized stored balance": {
			"refund_operation_key":     afterRetention.IdempotencyKey,
			"refund_operation_time":    afterRetention.FirstAttemptTime,
			"refund_operation_balance": int64(13001),
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
				Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).
				Updates(corrupt).Error)
			_, prepareErr := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 3000)
			require.ErrorIs(t, prepareErr, model.ErrTossRefundOperationStateInvalid)
			require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
				Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).
				Updates(validState).Error)
		})
	}
}

func TestTossTopUpRefundOperationConcurrentPrepareReturnsSameKey(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}))
	const (
		orderID    = "toss_refund_concurrent_prepare"
		paymentKey = "pay_refund_concurrent_prepare"
	)
	require.NoError(t, model.DB.Create(&model.User{Id: 682, Username: "toss-refund-concurrent"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 682, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	_, err := model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	type outcome struct {
		operation model.TossTopUpRefundOperation
		err       error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			operation, prepareErr := model.PrepareTossTopUpRefundOperationWithContext(context.Background(), orderID, paymentKey, 1000)
			outcomes <- outcome{operation: operation, err: prepareErr}
		}()
	}
	close(start)
	first := <-outcomes
	second := <-outcomes
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.NotEmpty(t, first.operation.IdempotencyKey)
	require.Equal(t, first.operation.IdempotencyKey, second.operation.IdempotencyKey,
		"concurrent workers must converge on the key committed under the refund-fence row lock")
}

func TestTossTopUpRefundPOSTStateFailsClosed(t *testing.T) {
	base := tossConfirmResponse{Status: "DONE", TotalAmount: 1000, BalanceAmount: 1000}
	valid := []tossConfirmResponse{
		base,
		{Status: "WAITING_FOR_DEPOSIT", TotalAmount: 1000, BalanceAmount: 1000},
		{Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 300},
	}
	for i := range valid {
		require.NoError(t, validateTossTopUpRefundPOSTState(&valid[i]))
	}

	invalid := []tossConfirmResponse{
		{Status: "READY", TotalAmount: 1000, BalanceAmount: 1000},
		{Status: "IN_PROGRESS", TotalAmount: 1000, BalanceAmount: 1000},
		{Status: "FUTURE_STATUS", TotalAmount: 1000, BalanceAmount: 1000},
		{Status: "DONE", TotalAmount: 1000, BalanceAmount: 999},
		{Status: "WAITING_FOR_DEPOSIT", TotalAmount: 1000, BalanceAmount: 999},
		{Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 0},
		{Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 1000},
	}
	for i := range invalid {
		require.Error(t, validateTossTopUpRefundPOSTState(&invalid[i]))
	}
}

func TestCancelRequiredTossTopUpDoesNotPOSTForUnsupportedAuthoritativeState(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}))
	const (
		orderID    = "toss_refund_unsupported_state"
		paymentKey = "pay_refund_unsupported_state"
	)
	require.NoError(t, model.DB.Create(&model.User{Id: 681, Username: "toss-refund-unsupported"}).Error)
	topUp := model.TopUp{
		UserId: 681, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err := model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "READY", OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	providerCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		t.Fatal("unsupported authoritative payment state must not authorize cancellation POST")
		return nil, nil
	})}
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "READY",
		TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW",
	}
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, auth))
	require.Zero(t, providerCalls)
	var fence model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&fence).Error)
	require.Empty(t, fence.RefundOperationKey)
}

func TestTossVirtualAccountRefundCompletionFailsClosed(t *testing.T) {
	topUp := &model.TopUp{TradeNo: "toss_va_refund_state", ProviderOrderId: "pay_va_refund_state", Amount: 1000}
	base := tossConfirmResponse{
		PaymentKey: topUp.ProviderOrderId, Type: "NORMAL", OrderId: topUp.TradeNo,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW",
		Method: "가상계좌", ApprovedAt: "2026-07-12T10:00:00+09:00",
	}
	tests := []struct {
		name     string
		refund   *tossPaymentVirtualAccount
		unfunded bool
		wantFull bool
		wantErr  error
	}{
		{name: "completed", refund: &tossPaymentVirtualAccount{RefundStatus: "COMPLETED"}, wantFull: true},
		{name: "unfunded none", refund: &tossPaymentVirtualAccount{RefundStatus: "NONE"}, unfunded: true, wantFull: true},
		{name: "funded none", refund: &tossPaymentVirtualAccount{RefundStatus: "NONE"}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "pending", refund: &tossPaymentVirtualAccount{RefundStatus: "PENDING"}, wantErr: errTossTopUpVirtualAccountRefundPending},
		{name: "failed", refund: &tossPaymentVirtualAccount{RefundStatus: "FAILED"}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "partially failed", refund: &tossPaymentVirtualAccount{RefundStatus: "PARTIAL_FAILED"}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "missing object", wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "missing field", refund: &tossPaymentVirtualAccount{}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "whitespace", refund: &tossPaymentVirtualAccount{RefundStatus: " COMPLETED "}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "lowercase", refund: &tossPaymentVirtualAccount{RefundStatus: "completed"}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
		{name: "future enum", refund: &tossPaymentVirtualAccount{RefundStatus: "REVIEWING"}, wantErr: errTossTopUpVirtualAccountRefundUnproved},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payment := base
			payment.VirtualAccount = testCase.refund
			if testCase.unfunded {
				payment.ApprovedAt = ""
			}
			require.Equal(t, testCase.wantFull, isFullyCanceledTossTopUp(&payment, topUp))
			if testCase.wantErr == nil {
				require.NoError(t, tossVirtualAccountRefundWaitError(&payment))
			} else {
				require.ErrorIs(t, tossVirtualAccountRefundWaitError(&payment), testCase.wantErr)
			}
		})
	}

	for name, method := range map[string]string{
		"missing method":    "",
		"whitespace method": " 카드 ",
		"future method":     "CRYPTO",
	} {
		t.Run(name, func(t *testing.T) {
			payment := base
			payment.Method = method
			payment.VirtualAccount = nil
			require.False(t, isFullyCanceledTossTopUp(&payment, topUp))
			require.ErrorIs(t, tossVirtualAccountRefundWaitError(&payment), errTossTopUpVirtualAccountRefundUnproved)
		})
	}

	t.Run("approvedAt whitespace is not unfunded proof", func(t *testing.T) {
		payment := base
		payment.ApprovedAt = " "
		payment.VirtualAccount = &tossPaymentVirtualAccount{RefundStatus: "NONE"}
		require.False(t, isFullyCanceledTossTopUp(&payment, topUp))
		require.ErrorIs(t, tossVirtualAccountRefundWaitError(&payment), errTossTopUpVirtualAccountRefundUnproved)
	})

	t.Run("known non-virtual method completes synchronously", func(t *testing.T) {
		payment := base
		payment.Method = "카드"
		payment.VirtualAccount = nil
		require.True(t, isFullyCanceledTossTopUp(&payment, topUp))
		require.NoError(t, tossVirtualAccountRefundWaitError(&payment))
	})
}

func TestCancelRequiredTossTopUpReusesKeyUntilProviderBalanceChanges(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 70, Username: "toss-refund-key-user"}).Error)
	const (
		orderID     = "toss_refund_balance_retry"
		paymentKey  = "pay_refund_balance_retry"
		exactSecret = "sk_refund_balance_retry"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 70, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	keys := make([]string, 0, 3)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/payments/"+paymentKey+"/cancel", request.URL.EscapedPath())
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		body := `{"paymentKey":"pay_refund_balance_retry","type":"NORMAL","orderId":"toss_refund_balance_retry","status":"PARTIAL_CANCELED","totalAmount":1000,"balanceAmount":300,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":700,"refundableAmount":300,"transactionKey":"tx_refund_balance_first","cancelStatus":"DONE"}]}`
		if len(keys) == 3 {
			body = `{"paymentKey":"pay_refund_balance_retry","type":"NORMAL","orderId":"toss_refund_balance_retry","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":700,"refundableAmount":300,"transactionKey":"tx_refund_balance_first","cancelStatus":"DONE"},{"cancelAmount":300,"refundableAmount":0,"transactionKey":"tx_refund_balance_second","cancelStatus":"DONE"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	fullBalance := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, fullBalance))
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, fullBalance))
	lowerBalance := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "PARTIAL_CANCELED",
		TotalAmount: 1000, BalanceAmount: 300, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
		Cancels: []tossPaymentCancel{{CancelAmount: 700, RefundableAmount: 300, TransactionKey: "tx_refund_balance_first", CancelStatus: "DONE"}},
	}
	require.NoError(t, cancelRequiredTossTopUp(context.Background(), &topUp, lowerBalance))

	require.Len(t, keys, 3)
	require.Equal(t, keys[0], keys[1], "same authoritative balance must replay the same provider operation")
	require.NotEqual(t, keys[1], keys[2], "a lower authoritative balance must use a new provider operation")
	require.True(t, strings.HasPrefix(keys[0], "toss_refund_"))
	var fence model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&fence).Error)
	require.Equal(t, keys[2], fence.RefundOperationKey)
	require.Equal(t, int64(300), fence.RefundOperationBalance)
}

func TestCancelRequiredTossTopUpRejectsMismatchedVirtualAccountResponseBeforeLedgerWrite(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 701, Username: "refund-response-identity"}).Error)
	const (
		orderID    = "toss_refund_response_identity"
		paymentKey = "pay_refund_response_identity"
		secretKey  = "live_sk_refund_response_identity"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 701, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey:             model.TossTopUpRefundRequiredEventKey(orderID, paymentKey),
		EventType:            model.TossPaymentEventTypeRefundRequired,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               "DONE",
		OriginalAmount:       1000,
		BalanceAmount:        1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_different_response_identity","type":"NORMAL","orderId":"toss_refund_response_identity","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"가상계좌","approvedAt":"2026-07-12T10:00:00+09:00","virtualAccount":{"refundStatus":"PENDING"},"cancels":[{"cancelAmount":1000,"refundableAmount":0,"transactionKey":"tx_different_response_identity","cancelStatus":"DONE"}]}`,
			)),
		}, nil
	})}
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, auth))

	var count int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).
		Count(&count).Error)
	require.Zero(t, count)

	required, err := model.HasRequiredTossTopUpRefundWithContext(context.Background(), orderID, paymentKey)
	require.NoError(t, err)
	require.True(t, required, "the original refund fence must remain active for a later authoritative retry")
}

func TestCancelRequiredTossTopUpPersistsPartialSuccessBeforeReturning(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 702, Username: "refund-partial-response"}).Error)
	const (
		orderID    = "toss_refund_partial_response"
		paymentKey = "pay_refund_partial_response"
		secretKey  = "live_sk_refund_partial_response"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 702, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey:             model.TossTopUpRefundRequiredEventKey(orderID, paymentKey),
		EventType:            model.TossPaymentEventTypeRefundRequired,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               "DONE",
		OriginalAmount:       1000,
		BalanceAmount:        1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_refund_partial_response","type":"NORMAL","orderId":"toss_refund_partial_response","status":"PARTIAL_CANCELED","totalAmount":1000,"balanceAmount":600,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":400,"refundableAmount":600,"transactionKey":"tx_refund_partial_response","cancelStatus":"DONE"}]}`,
			)),
		}, nil
	})}
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, auth))

	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).First(&event).Error)
	require.Equal(t, int64(400), event.CancelAmount)
	require.Equal(t, "tx_refund_partial_response", event.TransactionKey)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)

	required, err := model.HasRequiredTossTopUpRefundWithContext(context.Background(), orderID, paymentKey)
	require.NoError(t, err)
	require.True(t, required)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, topUp.Status)
}

func TestCancelRequiredTossTopUpPinsCached5xxAcrossHoursAndRotatesAfterRetention(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 69, Username: "toss-refund-5xx-user"}).Error)
	const (
		orderID     = "toss_refund_cached_5xx"
		paymentKey  = "pay_refund_cached_5xx"
		exactSecret = "sk_refund_cached_5xx"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 69, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	keys := make([]string, 0, tossAPIMaxAttempts*2+1)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		if len(keys) <= tossAPIMaxAttempts*2 {
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"PROVIDER_ERROR"}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_refund_cached_5xx","type":"NORMAL","orderId":"toss_refund_cached_5xx","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":1000,"refundableAmount":0,"transactionKey":"tx_refund_cached_5xx","cancelStatus":"DONE"}]}`,
		))}, nil
	})}
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}

	require.Error(t, cancelRequiredTossTopUp(context.Background(), &topUp, auth))
	require.Len(t, keys, tossAPIMaxAttempts)
	for i := 1; i < len(keys); i++ {
		require.Equal(t, keys[0], keys[i], "all retries inside one provider operation must reuse the same key")
	}

	nextHourlyReservation := topUp
	nextHourlyReservation.ProviderRetryTime += int64(time.Hour / time.Second)
	require.Error(t, cancelRequiredTossTopUp(context.Background(), &nextHourlyReservation, auth))
	require.Len(t, keys, tossAPIMaxAttempts*2)
	for i := 1; i < len(keys); i++ {
		require.Equal(t, keys[0], keys[i], "scheduler time must not change an ambiguous provider operation inside 15 days")
	}

	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).
		Update("refund_operation_time", model.GetDBTimestamp()-model.TossProviderIdempotencyRetentionSeconds).Error)
	require.NoError(t, cancelRequiredTossTopUp(context.Background(), &nextHourlyReservation, auth))
	require.Len(t, keys, tossAPIMaxAttempts*2+1)
	require.NotEqual(t, keys[0], keys[len(keys)-1],
		"after 15 days an unchanged authoritative balance must use the newly pinned operation key")
}

func TestRefundReconciliationPastFifteenDaysGETPreventsDuplicateCancellationPOST(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	const (
		orderID     = "toss_refund_get_before_repost"
		paymentKey  = "pay_refund_get_before_repost"
		exactSecret = "sk_refund_get_before_repost"
	)
	require.NoError(t, model.DB.Create(&model.User{Id: 691, Username: "toss-refund-get-first"}).Error)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 691, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderOrderTime:  model.GetDBTimestamp() - int64(16*24*time.Hour/time.Second),
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			getCalls++
			body := `{"paymentKey":"pay_refund_get_before_repost","type":"NORMAL","orderId":"toss_refund_get_before_repost","status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"계좌이체"}`
			if getCalls == 2 {
				body = `{"paymentKey":"pay_refund_get_before_repost","type":"NORMAL","orderId":"toss_refund_get_before_repost","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"계좌이체","cancels":[{"cancelAmount":1000,"refundableAmount":0,"transactionKey":"tx_refund_get_before_repost","cancelStatus":"DONE"}]}`
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		case http.MethodPost:
			postCalls++
			if getCalls > 1 {
				t.Fatal("authoritative CANCELED/balance=0 GET must suppress a new cancellation operation")
			}
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"PROVIDER_ERROR"}`))}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", request.Method, request.URL.EscapedPath())
			return nil, nil
		}
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.Error(t, err)
	require.False(t, resolved)
	require.Equal(t, tossAPIMaxAttempts, postCalls)

	// Even once the ambiguous operation itself is older than the provider's
	// retention window, the next worker must GET current state before deciding
	// whether a new key and POST are necessary.
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).
		Update("refund_operation_time", model.GetDBTimestamp()-model.TossProviderIdempotencyRetentionSeconds).Error)
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", orderID).
		Update("provider_retry_time", topUp.ProviderRetryTime+int64(time.Hour/time.Second)).Error)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	resolved, err = reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 2, getCalls)
	require.Equal(t, tossAPIMaxAttempts, postCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
}

func TestReconcileTossRecordedTopUpAutomaticallyRefundsMutatedPaymentMethod(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 71, Username: "toss-refund-user"}).Error)
	const (
		orderID     = "toss_auto_refund_transfer"
		paymentKey  = "pay_auto_refund_transfer"
		exactSecret = "sk_auto_refund_transfer"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 71, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderOrderTime: model.GetDBTimestamp(),
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(exactSecret+":"))
	observedIdempotencyKey := ""
	getCalls := 0
	cancelCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, expectedAuthorization, request.Header.Get("Authorization"))
		require.Equal(t, "/v1/payments/"+paymentKey, strings.TrimSuffix(request.URL.EscapedPath(), "/cancel"))
		switch {
		case request.Method == http.MethodGet:
			getCalls++
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_auto_refund_transfer","type":"NORMAL","orderId":"toss_auto_refund_transfer","status":"DONE","totalAmount":13000,"balanceAmount":13000,"suppliedAmount":11818,"vat":1182,"taxFreeAmount":0,"taxExemptionAmount":0,"currency":"KRW","method":"계좌이체","useEscrow":false}`,
			))}, nil
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.EscapedPath(), "/cancel"):
			cancelCalls++
			observedIdempotencyKey = request.Header.Get("Idempotency-Key")
			require.True(t, strings.HasPrefix(observedIdempotencyKey, "toss_refund_"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			var payload struct {
				CancelReason string `json:"cancelReason"`
			}
			require.NoError(t, common.Unmarshal(body, &payload))
			require.NotEmpty(t, payload.CancelReason)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_auto_refund_transfer","type":"NORMAL","orderId":"toss_auto_refund_transfer","status":"CANCELED","totalAmount":13000,"balanceAmount":0,"suppliedAmount":0,"vat":0,"taxFreeAmount":0,"taxExemptionAmount":0,"currency":"KRW","method":"계좌이체","useEscrow":false,"cancels":[{"cancelAmount":13000,"cancelReason":"Automatic full refund","refundableAmount":0,"transactionKey":"tx_auto_refund_transfer","cancelStatus":"DONE"}]}`,
			))}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", request.Method, request.URL.EscapedPath())
			return nil, nil
		}
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, getCalls)
	require.Equal(t, 1, cancelCalls)
	require.NotEmpty(t, observedIdempotencyKey)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Zero(t, user.Quota)
	var required int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("order_id = ? AND reconciliation_status = ?", orderID, model.TossReconciliationStatusRequired).
		Count(&required).Error)
	require.Zero(t, required)
}

func TestReconcileTossRecordedTopUpQueuesUncreditedBrowserMutatedPartialBalance(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 78, Username: "toss-stale-partial-user"}).Error)
	const (
		orderID     = "toss_stale_mutated_partial"
		paymentKey  = "pay_stale_mutated_partial"
		exactSecret = "sk_stale_mutated_partial"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 78, Amount: 13000, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderOrderTime: model.GetDBTimestamp() - 900,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "the first observation must only install the durable remaining-balance fence")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_stale_mutated_partial","type":"NORMAL","orderId":"toss_stale_mutated_partial","status":"PARTIAL_CANCELED","totalAmount":13000,"balanceAmount":3000,"currency":"KRW","method":"계좌이체","cancels":[{"cancelAmount":10000,"refundableAmount":3000,"transactionKey":"tx_stale_mutated_partial","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, requests)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_stale_mutated_partial").First(&cancellation).Error)
	require.EqualValues(t, 10000, cancellation.CancelAmount)
}

func TestTossWebhookQueuesVirtualAccountRefundBeforeDepositWithoutProviderPost(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 72, Username: "toss-waiting-refund-user"}).Error)
	const (
		orderID     = "toss_waiting_virtual_refund"
		paymentKey  = "pay_waiting_virtual_refund"
		exactSecret = "sk_waiting_virtual_refund"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	now := model.GetDBTimestamp()
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 72, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: orderID, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderRetryTime: now + 120,
		ProviderClaimToken: "live-confirm-claim", ProviderClaimTime: now,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "the webhook may only verify and queue; cancellation belongs to the background worker")
		require.Equal(t, "/v1/payments/"+paymentKey, request.URL.EscapedPath())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_waiting_virtual_refund","type":"NORMAL","orderId":"toss_waiting_virtual_refund","status":"WAITING_FOR_DEPOSIT","totalAmount":13000,"balanceAmount":13000,"suppliedAmount":11818,"vat":1182,"currency":"KRW","method":"가상계좌","useEscrow":false}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_waiting_virtual_refund","paymentKey":"pay_waiting_virtual_refund","status":"WAITING_FOR_DEPOSIT"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	require.LessOrEqual(t, stored.ProviderRetryTime, model.GetDBTimestamp())
	require.Empty(t, stored.ProviderClaimToken)
	require.Zero(t, stored.ProviderClaimTime)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeRefundRequired).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
}

func TestReconcileTossVirtualAccountPendingBankRefundKeepsFenceWithoutCancelPost(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 79, Username: "toss-va-bank-refund-user"}).Error)
	const (
		orderID     = "toss_va_bank_refund_pending"
		paymentKey  = "pay_va_bank_refund_pending"
		exactSecret = "sk_va_bank_refund_pending"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 79, Amount: 13000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	refundStatus := "PENDING"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "an accepted virtual-account cancellation must be polled, not canceled again")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_va_bank_refund_pending","type":"NORMAL","orderId":"toss_va_bank_refund_pending","status":"CANCELED","totalAmount":13000,"balanceAmount":0,"currency":"KRW","method":"가상계좌","approvedAt":"2026-07-12T10:00:00+09:00","virtualAccount":{"refundStatus":"` + refundStatus + `"},"cancels":[{"cancelAmount":13000,"refundableAmount":0,"transactionKey":"tx_va_bank_refund_pending","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, requests)

	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, topUp.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_va_bank_refund_pending").First(&cancellation).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)

	// Unknown future values must not clear the fence or trigger another cancel.
	// They move to the low-frequency operator lane until their meaning is reviewed.
	refundStatus = "REVIEWING"
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	before := model.GetDBTimestamp()
	resolved, err = reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 2, requests)
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, topUp.Status)
	require.GreaterOrEqual(t, topUp.ProviderRetryTime, before+int64(tossTopUpRefundManualRetryDelay/time.Second))
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
}

func TestTossWebhookFinalizesVirtualAccountFenceAfterBankRefundCompleted(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 81, Username: "toss-va-refund-complete-user"}).Error)
	const (
		orderID     = "toss_va_bank_refund_completed"
		paymentKey  = "pay_va_bank_refund_completed"
		exactSecret = "sk_va_bank_refund_completed"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 81, Amount: 13000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE", OriginalAmount: 13000, BalanceAmount: 13000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_va_bank_refund_completed","type":"NORMAL","orderId":"toss_va_bank_refund_completed","status":"CANCELED","totalAmount":13000,"balanceAmount":0,"currency":"KRW","method":"가상계좌","approvedAt":"2026-07-12T10:00:00+09:00","virtualAccount":{"refundStatus":"COMPLETED"},"cancels":[{"cancelAmount":13000,"refundableAmount":0,"transactionKey":"tx_va_bank_refund_completed","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_va_bank_refund_completed","paymentKey":"pay_va_bank_refund_completed","status":"CANCELED"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusFailed, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusResolved, refundEvent.ReconciliationStatus)
}

func TestTossWebhookFinalizesUnfundedRefundFenceWithoutCancelPost(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 73, Username: "toss-unfunded-refund-user"}).Error)
	const (
		orderID     = "toss_unfunded_virtual_refund"
		paymentKey  = "pay_unfunded_virtual_refund"
		exactSecret = "sk_unfunded_virtual_refund"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 73, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "WAITING_FOR_DEPOSIT",
		OriginalAmount: 13000, BalanceAmount: 13000, ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "an unfunded terminal payment must not be sent to the cancel API")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_unfunded_virtual_refund","type":"NORMAL","orderId":"toss_unfunded_virtual_refund","status":"EXPIRED","totalAmount":13000,"balanceAmount":0,"currency":"KRW","method":"가상계좌","useEscrow":false}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_unfunded_virtual_refund","paymentKey":"pay_unfunded_virtual_refund","status":"EXPIRED"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusExpired, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusResolved, refundEvent.ReconciliationStatus)
}

func TestTossWebhookKeepsRefundFencedPartialCancellationRecoverable(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 74, Username: "toss-partial-refund-user"}).Error)
	const (
		orderID     = "toss_partial_refund_fence"
		paymentKey  = "pay_partial_refund_fence"
		exactSecret = "sk_partial_refund_fence"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId: 74, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err = model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE",
		OriginalAmount: 13000, BalanceAmount: 13000, ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "webhook processing must only verify and persist the partial cancellation")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_partial_refund_fence","type":"NORMAL","orderId":"toss_partial_refund_fence","status":"PARTIAL_CANCELED","totalAmount":13000,"balanceAmount":3000,"taxFreeAmount":100,"currency":"KRW","method":"계좌이체","cancels":[{"cancelAmount":10000,"refundableAmount":3000,"transactionKey":"tx_partial_refund_fence","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_partial_refund_fence","paymentKey":"pay_partial_refund_fence","status":"PARTIAL_CANCELED"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_partial_refund_fence").First(&cancellation).Error)
	require.EqualValues(t, 10000, cancellation.CancelAmount)
}

func TestTossWebhookQueuesRemainingBalanceForClosedBrowserMutatedPartialCancellation(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 76, Username: "toss-uncredited-partial-user"}).Error)
	const (
		orderID     = "toss_uncredited_partial_cancel"
		paymentKey  = "pay_uncredited_partial_cancel"
		exactSecret = "sk_uncredited_partial_cancel"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 76, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusFailed,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method, "webhook processing must persist and queue without issuing a provider write")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_uncredited_partial_cancel","type":"NORMAL","orderId":"toss_uncredited_partial_cancel","status":"PARTIAL_CANCELED","totalAmount":13000,"balanceAmount":3000,"currency":"KRW","method":"계좌이체","cancels":[{"cancelAmount":10000,"refundableAmount":3000,"transactionKey":"tx_uncredited_partial_cancel","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_uncredited_partial_cancel","paymentKey":"pay_uncredited_partial_cancel","status":"PARTIAL_CANCELED"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, requests)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	require.LessOrEqual(t, stored.ProviderRetryTime, model.GetDBTimestamp())

	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	require.EqualValues(t, 3000, refundEvent.BalanceAmount)

	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_uncredited_partial_cancel").First(&cancellation).Error)
	require.EqualValues(t, 10000, cancellation.CancelAmount)
}

func TestTossWebhookFinalCancellationLagPersistsPartialFenceAndRetries(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 80, Username: "toss-final-cancel-lag-user"}).Error)
	const (
		orderID     = "toss_final_cancel_lag"
		paymentKey  = "pay_final_cancel_lag"
		exactSecret = "sk_final_cancel_lag"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 80, Amount: 13000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_final_cancel_lag","type":"NORMAL","orderId":"toss_final_cancel_lag","status":"PARTIAL_CANCELED","totalAmount":13000,"balanceAmount":3000,"currency":"KRW","method":"카드","card":{"amount":13000},"cancels":[{"cancelAmount":10000,"refundableAmount":3000,"transactionKey":"tx_final_cancel_lag_partial","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_final_cancel_lag","paymentKey":"pay_final_cancel_lag","status":"CANCELED"}}`)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, "Toss must redeliver until the final cancellation is authoritative")

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_final_cancel_lag_partial").First(&cancellation).Error)
	require.EqualValues(t, 10000, cancellation.CancelAmount)
}

func TestTossWebhookPersistsCancellationForHistoricallyCreditedLegacyContract(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.TossPaymentEvent{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 75, Username: "toss-legacy-contract-user", Quota: 321}).Error)
	const (
		orderID     = "toss_legacy_contract_cancel"
		paymentKey  = "pay_legacy_contract_cancel"
		exactSecret = "sk_legacy_contract_cancel"
	)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 75, Amount: 13000, Quota: 321, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`{"paymentKey":"pay_legacy_contract_cancel","type":"NORMAL","orderId":"toss_legacy_contract_cancel","status":"CANCELED","totalAmount":13000,"balanceAmount":0,"taxFreeAmount":100,"currency":"KRW","method":"계좌이체","cancels":[{"cancelAmount":13000,"refundableAmount":0,"transactionKey":"tx_legacy_contract_cancel","cancelStatus":"DONE"}]}`,
		))}, nil
	})}

	recorder := performTossWebhookForTest(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_legacy_contract_cancel","paymentKey":"pay_legacy_contract_cancel","status":"CANCELED"}}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "tx_legacy_contract_cancel").First(&cancellation).Error)
	require.Equal(t, model.TossPaymentEventTypeCancellation, cancellation.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
}
