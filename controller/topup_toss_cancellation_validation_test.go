package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func validTossCancellationForTest() *tossConfirmResponse {
	return &tossConfirmResponse{
		PaymentKey:    "pay_cancel_validation",
		Type:          "BILLING",
		OrderId:       "order_cancel_validation",
		Status:        "PARTIAL_CANCELED",
		TotalAmount:   1000,
		BalanceAmount: 500,
		Currency:      "KRW",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 1000, Company: "card", Number: "****1111"},
		Cancels: []tossPaymentCancel{
			{CancelAmount: 300, TransactionKey: "cancel_tx_1", CancelStatus: "DONE"},
			{CancelAmount: 200, TransactionKey: "cancel_tx_2", CancelStatus: "DONE"},
		},
	}
}

func TestExpectedTossCancellationRequiresExactCardPaymentContract(t *testing.T) {
	valid := validTossCancellationForTest()
	require.True(t, isValidTossCardCancellation(valid, valid.OrderId, valid.TotalAmount))

	tests := []struct {
		name   string
		mutate func(*tossConfirmResponse)
	}{
		{name: "amount", mutate: func(p *tossConfirmResponse) { p.TotalAmount-- }},
		{name: "currency", mutate: func(p *tossConfirmResponse) { p.Currency = "USD" }},
		{name: "missing card", mutate: func(p *tossConfirmResponse) { p.Card = nil }},
		{name: "order", mutate: func(p *tossConfirmResponse) { p.OrderId = "different_order" }},
		{name: "status", mutate: func(p *tossConfirmResponse) { p.Status = "DONE" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payment := validTossCancellationForTest()
			testCase.mutate(payment)
			require.False(t, isValidTossCardCancellation(payment, valid.OrderId, valid.TotalAmount))
		})
	}
}

func TestTossTransactionKeyRejectsWhitespaceControlAndOversizeValues(t *testing.T) {
	require.True(t, isValidTossTransactionKey(strings.Repeat("x", tossTransactionKeyMaxRunes)))
	for _, value := range []string{
		" leading",
		"trailing ",
		"internal space",
		"line\nbreak",
		"tab\tkey",
		strings.Repeat("x", tossTransactionKeyMaxRunes+1),
	} {
		require.False(t, isValidTossTransactionKey(value), value)

		payment := validTossCancellationForTest()
		payment.Cancels[0].TransactionKey = value
		_, _, err := persistTossCancellationEventsWithContext(context.Background(), payment)
		require.Error(t, err, "an unsafe cancel.transactionKey must be rejected before ledger persistence")
	}
}

func TestTossCardCancellationPreservesTaxAndEscrowMismatchEvidence(t *testing.T) {
	valid := validTossCancellationForTest()
	for _, tc := range []struct {
		name   string
		mutate func(*tossConfirmResponse)
	}{
		{name: "tax free amount", mutate: func(p *tossConfirmResponse) { p.TaxFreeAmount = 100 }},
		{name: "tax exemption amount", mutate: func(p *tossConfirmResponse) { p.TaxExemptionAmount = 100 }},
		{name: "escrow", mutate: func(p *tossConfirmResponse) { p.UseEscrow = true }},
		{name: "culture expense", mutate: func(p *tossConfirmResponse) { p.CultureExpense = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payment := validTossCancellationForTest()
			tc.mutate(payment)
			require.True(t, isValidTossCardCancellation(payment, valid.OrderId, valid.TotalAmount),
				"a cancellation is monotonic financial evidence and must not be dropped because fulfillment would be rejected")
		})
	}
}

func TestTaxMismatchedFirstChargeCancellationIsPersistedAndStopsRetry(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionOrder{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{
		Id: 9101, Username: "tax-mismatch-cancel-user", Status: common.UserStatusEnabled,
	}).Error)
	const orderID = "toss_sub_tax_mismatch_canceled"
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 9101, PlanId: 1, TradeNo: orderID,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: 1000, ProviderCurrency: "KRW",
		BillingAttempted: true,
	}).Error)

	providerPayment := &tossConfirmResponse{
		PaymentKey: "pay_tax_mismatch_canceled", Type: "BILLING", OrderId: orderID,
		Status: "DONE", TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드",
		TaxFreeAmount: 100, Card: &tossPaymentCard{Amount: 1000},
	}
	require.False(t, isValidTossBillingCharge(providerPayment, orderID, 1000),
		"the tax-mismatched DONE response must never grant a subscription")

	providerPayment.Status = "CANCELED"
	providerPayment.BalanceAmount = 0
	providerPayment.Cancels = []tossPaymentCancel{{
		CancelAmount: 1000, TransactionKey: "cancel_tax_mismatch_first_charge", CancelStatus: "DONE",
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", nil)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&order).Error)
	require.True(t, handleAuthoritativeTossSubscriptionCancellation(c, &order, providerPayment))
	require.Equal(t, http.StatusOK, recorder.Code)

	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&order).Error)
	require.Equal(t, common.TopUpStatusFailed, order.Status,
		"authoritative cancellation must close the pending order so it cannot be charged again")
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_tax_mismatch_first_charge").First(&event).Error)
	require.Equal(t, model.TossPaymentEventTypeCancellation, event.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
}

func TestTossTopUpAcceptsCardlessEasyPayFundingButBillingStaysCardOnly(t *testing.T) {
	maxInt64 := int64(^uint64(0) >> 1)
	tests := []struct {
		name    string
		method  string
		total   int64
		card    *tossPaymentCard
		easyPay *tossPaymentEasyPay
		want    bool
	}{
		{
			name:   "direct card",
			method: "카드",
			total:  1000,
			card:   &tossPaymentCard{Amount: 1000},
			want:   true,
		},
		{
			name:    "easy pay registered card",
			method:  "간편결제",
			total:   1000,
			card:    &tossPaymentCard{Amount: 1000},
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY"},
			want:    true,
		},
		{
			name:    "easy pay registered card and points",
			method:  "간편결제",
			total:   1000,
			card:    &tossPaymentCard{Amount: 700},
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", DiscountAmount: 300},
			want:    true,
		},
		{
			name:    "linked account or money",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: 1000},
			want:    true,
		},
		{
			name:    "points only",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", DiscountAmount: 1000},
			want:    true,
		},
		{
			name:    "money and points",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: 700, DiscountAmount: 300},
			want:    true,
		},
		{
			name:    "missing provider",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Amount: 1000},
			want:    false,
		},
		{
			name:    "no funded amount",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY"},
			want:    false,
		},
		{
			name:    "components below total",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: 900, DiscountAmount: 99},
			want:    false,
		},
		{
			name:    "component above total",
			method:  "간편결제",
			total:   1000,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: 1001},
			want:    false,
		},
		{
			name:    "overflowing component sum",
			method:  "간편결제",
			total:   maxInt64,
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: maxInt64, DiscountAmount: 1},
			want:    false,
		},
		{
			name:   "card amount below total",
			method: "카드",
			total:  1000,
			card:   &tossPaymentCard{Amount: 999},
			want:   false,
		},
		{
			name:    "card method with easy pay payload",
			method:  "카드",
			total:   1000,
			card:    &tossPaymentCard{Amount: 700},
			easyPay: &tossPaymentEasyPay{Provider: "TOSSPAY", DiscountAmount: 300},
			want:    false,
		},
		{
			name:   "easy pay method without easy pay payload",
			method: "간편결제",
			total:  1000,
			card:   &tossPaymentCard{Amount: 1000},
			want:   false,
		},
		{
			name:   "unsupported response method",
			method: "가상계좌",
			total:  1000,
			card:   &tossPaymentCard{Amount: 1000},
			want:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payment := &tossConfirmResponse{
				PaymentKey:    "pay_easy_topup",
				Type:          "NORMAL",
				OrderId:       "order_easy_topup",
				Status:        "DONE",
				TotalAmount:   testCase.total,
				BalanceAmount: testCase.total,
				Currency:      "KRW",
				Method:        testCase.method,
				Card:          testCase.card,
				EasyPay:       testCase.easyPay,
			}
			require.Equal(t, testCase.want, isValidTossTopUpPayment(payment, payment.OrderId, payment.TotalAmount))
			if testCase.want && payment.TotalAmount > 0 {
				payment.BalanceAmount--
				require.False(t, isValidTossTopUpPayment(payment, payment.OrderId, payment.TotalAmount))
			}

			payment.Status = "PARTIAL_CANCELED"
			payment.BalanceAmount = 500
			require.Equal(t, testCase.want, isValidTossTopUpCancellation(payment, payment.OrderId, payment.TotalAmount))
			require.False(t, isValidTossCardCancellation(payment, payment.OrderId, payment.TotalAmount))
		})
	}
}

func TestPersistExpectedTossTopUpCancellationAcceptsEasyPayOnly(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}, &model.TopUp{}))
	require.NoError(t, model.DB.Create(&model.TopUp{
		TradeNo:           "order_easy_cancel",
		PaymentMethod:     model.PaymentMethodToss,
		PaymentProvider:   model.PaymentProviderToss,
		ProviderOrderId:   "pay_easy_cancel",
		ProviderOrderTime: 1,
		Amount:            1000,
		Status:            common.TopUpStatusSuccess,
	}).Error)

	payment := &tossConfirmResponse{
		PaymentKey:    "pay_easy_cancel",
		Type:          "NORMAL",
		OrderId:       "order_easy_cancel",
		Status:        "PARTIAL_CANCELED",
		TotalAmount:   1000,
		BalanceAmount: 500,
		Currency:      "KRW",
		Method:        "간편결제",
		EasyPay:       &tossPaymentEasyPay{Provider: "TOSSPAY", Amount: 1000},
		Cancels: []tossPaymentCancel{{
			CancelAmount:   500,
			TransactionKey: "cancel_easy_topup",
			CancelStatus:   "DONE",
		}},
	}

	created, canceled, err := persistExpectedTossTopUpCancellationEvents(context.Background(), payment, payment.OrderId, payment.TotalAmount)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(500), canceled)

	_, _, err = persistExpectedTossCancellationEvents(context.Background(), payment, payment.OrderId, payment.TotalAmount)
	require.Error(t, err, "billing/card-only cancellation validation must remain strict")
}

func TestTossEasyPayResponseIsBoundedAndShapeValidated(t *testing.T) {
	payment := &tossConfirmResponse{
		PaymentKey:  "pay_easy_shape",
		Type:        "NORMAL",
		OrderId:     "order_easy_shape",
		Status:      "DONE",
		TotalAmount: 1000,
		Currency:    "KRW",
		EasyPay: &tossPaymentEasyPay{
			Provider: strings.Repeat("A", tossEasyPayMaxRunes+20) + "\n",
			Amount:   1000,
		},
	}

	sanitizeTossPaymentResponse(payment)
	require.Len(t, []rune(payment.EasyPay.Provider), tossEasyPayMaxRunes)
	require.NotContains(t, payment.EasyPay.Provider, "\n")
	require.NoError(t, validateTossPaymentResponseShape(payment))

	payment.EasyPay.Amount = -1
	require.Error(t, validateTossPaymentResponseShape(payment))
}

func TestPersistTossCancellationEventsValidatesCompleteStateBeforeInsert(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tossConfirmResponse)
	}{
		{name: "negative balance", mutate: func(p *tossConfirmResponse) { p.BalanceAmount = -1 }},
		{name: "balance above total", mutate: func(p *tossConfirmResponse) { p.BalanceAmount = 1001 }},
		{name: "partial with zero balance", mutate: func(p *tossConfirmResponse) { p.BalanceAmount = 0 }},
		{name: "full cancel with balance", mutate: func(p *tossConfirmResponse) { p.Status = "CANCELED" }},
		{name: "zero cancel", mutate: func(p *tossConfirmResponse) { p.Cancels[0].CancelAmount = 0 }},
		{name: "sum mismatch", mutate: func(p *tossConfirmResponse) { p.Cancels[1].CancelAmount = 100 }},
		{name: "duplicate transaction", mutate: func(p *tossConfirmResponse) { p.Cancels[1].TransactionKey = "cancel_tx_1" }},
		{name: "oversize transaction", mutate: func(p *tossConfirmResponse) { p.Cancels[1].TransactionKey = strings.Repeat("x", 65) }},
		{name: "no completed cancellation", mutate: func(p *tossConfirmResponse) {
			p.Cancels[0].CancelStatus = "IN_PROGRESS"
			p.Cancels[1].CancelStatus = "FAILED"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTossBillingControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}))
			payment := validTossCancellationForTest()
			tt.mutate(payment)

			created, amount, err := persistTossCancellationEvents(payment)
			require.Error(t, err)
			require.Zero(t, created)
			require.Zero(t, amount)
			var count int64
			require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestPersistTossCancellationEventsAcceptsFullAndLegacyCumulativeStates(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}, &model.TopUp{}, &model.SubscriptionOrder{}))
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{TradeNo: "order_cancel_full", PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending},
		{TradeNo: "order_cancel_legacy", PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending},
	}).Error)

	full := &tossConfirmResponse{
		PaymentKey:    "pay_cancel_full",
		OrderId:       "order_cancel_full",
		Status:        "CANCELED",
		TotalAmount:   1000,
		BalanceAmount: 0,
		Cancels: []tossPaymentCancel{{
			CancelAmount:   1000,
			TransactionKey: "cancel_tx_full",
			CancelStatus:   "DONE",
		}},
	}
	created, amount, err := persistTossCancellationEvents(full)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(1000), amount)

	legacy := &tossConfirmResponse{
		PaymentKey:    "pay_cancel_legacy",
		OrderId:       "order_cancel_legacy",
		Status:        "PARTIAL_CANCELED",
		TotalAmount:   1000,
		BalanceAmount: 700,
	}
	created, amount, err = persistTossCancellationEvents(legacy)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(300), amount)
	created, amount, err = persistTossCancellationEvents(legacy)
	require.NoError(t, err)
	require.Zero(t, created)
	require.Zero(t, amount)

	var legacyEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ?", legacy.OrderId).First(&legacyEvent).Error)
	require.Equal(t, int64(300), legacyEvent.CancelAmount)
	require.True(t, legacyEvent.CancelAmountCumulative)
	require.Empty(t, legacyEvent.TransactionKey)

	legacy.Status = "PARTIAL_CANCELED"
	legacy.BalanceAmount = 500
	created, amount, err = persistTossCancellationEvents(legacy)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(200), amount)
	legacyEvent = model.TossPaymentEvent{}
	require.NoError(t, model.DB.Where("order_id = ?", legacy.OrderId).Order("id desc").Take(&legacyEvent).Error)
	require.Equal(t, int64(500), legacyEvent.CancelAmount)
	require.True(t, legacyEvent.CancelAmountCumulative)
}

func TestPersistTossCancellationEventsPreservesOfficialCancelAuditFields(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}, &model.TopUp{}))
	require.NoError(t, model.DB.Create(&model.TopUp{
		TradeNo:         "order_cancel_validation",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	receiptKey := "receipt_cancel_1"
	cancelRequestId := "cancel_request_1"
	payment := validTossCancellationForTest()
	payment.Cancels[0].CancelReason = "customer requested a partial refund"
	payment.Cancels[0].TaxFreeAmount = 10
	payment.Cancels[0].TaxExemptionAmount = 20
	payment.Cancels[0].CardDiscountAmount = 30
	payment.Cancels[0].TransferDiscountAmount = 40
	payment.Cancels[0].EasyPayDiscountAmount = 50
	payment.Cancels[0].ReceiptKey = &receiptKey
	payment.Cancels[0].CancelRequestId = &cancelRequestId

	created, _, err := persistTossCancellationEvents(payment)
	require.NoError(t, err)
	require.Equal(t, 2, created)

	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_tx_1").First(&event).Error)
	var payload struct {
		Cancels []tossPaymentCancel `json:"cancels"`
	}
	require.NoError(t, common.UnmarshalJsonStr(event.ProviderPayload, &payload))
	require.Len(t, payload.Cancels, 2)
	cancel := payload.Cancels[0]
	require.Equal(t, "customer requested a partial refund", cancel.CancelReason)
	require.Equal(t, int64(10), cancel.TaxFreeAmount)
	require.Equal(t, int64(20), cancel.TaxExemptionAmount)
	require.Equal(t, int64(30), cancel.CardDiscountAmount)
	require.Equal(t, int64(40), cancel.TransferDiscountAmount)
	require.Equal(t, int64(50), cancel.EasyPayDiscountAmount)
	require.NotNil(t, cancel.ReceiptKey)
	require.Equal(t, receiptKey, *cancel.ReceiptKey)
	require.NotNil(t, cancel.CancelRequestId)
	require.Equal(t, cancelRequestId, *cancel.CancelRequestId)
}
