package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTossWebhookRetriesMatchedDoneUntilAuthoritativeDoneVisible(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	const (
		orderID    = "toss_lagging_done_webhook"
		paymentKey = "pay_lagging_done_webhook"
		secretKey  = "live_sk_lagging_done_webhook"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             1,
		Amount:             1000,
		TradeNo:            orderID,
		ProviderOrderId:    orderID,
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}).Error)

	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_lagging_done_webhook","type":"NORMAL","orderId":"toss_lagging_done_webhook","status":"IN_PROGRESS","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
			)),
		}, nil
	})}

	recorder := performTossWebhookForTest(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_lagging_done_webhook","paymentKey":"pay_lagging_done_webhook","status":"DONE"}}`,
	)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
}

func TestTossWebhookRetriesMatchedDoneForCreditedAndTerminalTopUps(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		status          string
		providerOrderID string
	}{
		{name: "credited", status: common.TopUpStatusSuccess, providerOrderID: "pay_lagging_done_existing"},
		{name: "terminal", status: common.TopUpStatusExpired, providerOrderID: "toss_lagging_done_existing"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			setupTossBillingControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

			const (
				orderID    = "toss_lagging_done_existing"
				paymentKey = "pay_lagging_done_existing"
				secretKey  = "live_sk_lagging_done_existing"
			)
			credential, err := model.EncryptProviderCredential(secretKey)
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.TopUp{
				UserId:             1,
				Amount:             1000,
				TradeNo:            orderID,
				ProviderOrderId:    testCase.providerOrderID,
				ProviderCredential: credential,
				PaymentMethod:      model.PaymentMethodToss,
				PaymentProvider:    model.PaymentProviderToss,
				Status:             testCase.status,
			}).Error)

			originalClient := http.DefaultClient
			t.Cleanup(func() { http.DefaultClient = originalClient })
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(
						`{"paymentKey":"pay_lagging_done_existing","type":"NORMAL","orderId":"toss_lagging_done_existing","status":"IN_PROGRESS","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
					)),
				}, nil
			})}

			recorder := performTossWebhookForTest(
				`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_lagging_done_existing","paymentKey":"pay_lagging_done_existing","status":"DONE"}}`,
			)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		})
	}
}

func TestTossWebhookDoesNotTreatVirtualAccountDoneRegressionAsReadLag(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	const (
		orderID    = "toss_done_regressed_waiting"
		paymentKey = "pay_done_regressed_waiting"
		secretKey  = "live_sk_done_regressed_waiting"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             1,
		Amount:             1000,
		TradeNo:            orderID,
		ProviderOrderId:    paymentKey,
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
	}).Error)

	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_done_regressed_waiting","type":"NORMAL","orderId":"toss_done_regressed_waiting","status":"WAITING_FOR_DEPOSIT","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"가상계좌","virtualAccount":{"refundStatus":"NONE"}}`,
			)),
		}, nil
	})}

	recorder := performTossWebhookForTest(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_done_regressed_waiting","paymentKey":"pay_done_regressed_waiting","status":"DONE"}}`,
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, func() bool {
		required, lookupErr := model.HasRequiredTossTopUpRefundWithContext(t.Context(), orderID, paymentKey)
		require.NoError(t, lookupErr)
		return required
	}())
}

func TestTossWebhookDoneRedeliveryResolvesStaleFulfillmentEvent(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.TossPaymentEvent{}))
	const orderID = "toss_success_with_stale_event"
	const secretKey = "live_sk_success_with_stale_event"
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             1,
		Amount:             1000,
		TradeNo:            orderID,
		ProviderOrderId:    "pay_success_with_stale_event",
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusSuccess,
	}).Error)
	event := &model.TossPaymentEvent{
		EventKey:             "toss_fulfillment_stale_redelivery",
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              orderID,
		PaymentKey:           "pay_success_with_stale_event",
		Status:               "DONE",
		OriginalAmount:       1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	}
	_, err = model.RecordTossPaymentEvent(event)
	require.NoError(t, err)
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossSecretKey = secretKey
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_success_with_stale_event","type":"NORMAL","orderId":"toss_success_with_stale_event","status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_success_with_stale_event","paymentKey":"pay_success_with_stale_event","status":"DONE"}}`,
	))
	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	stored, err := model.GetTossPaymentEventById(event.Id)
	require.NoError(t, err)
	require.Equal(t, model.TossReconciliationStatusResolved, stored.ReconciliationStatus)
}

func TestTossWebhookStaleDoneRedeliveryPersistsCurrentCancellation(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.TossPaymentEvent{}))
	const (
		orderID    = "toss_stale_done_now_canceled"
		paymentKey = "pay_stale_done_now_canceled"
		secretKey  = "live_sk_stale_done_now_canceled"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 1, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusSuccess,
	}).Error)
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossSecretKey = secretKey
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_stale_done_now_canceled","type":"NORMAL","orderId":"toss_stale_done_now_canceled","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":1000,"refundableAmount":0,"transactionKey":"cancel_stale_done_tx","cancelStatus":"DONE"}]}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_stale_done_now_canceled","paymentKey":"pay_stale_done_now_canceled","status":"DONE"}}`,
	))
	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).First(&event).Error)
	require.Equal(t, int64(1000), event.CancelAmount)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
}
