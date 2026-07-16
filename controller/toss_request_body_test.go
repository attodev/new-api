package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func oversizedTossPaymentJSON(prefix string) string {
	return prefix + strings.Repeat(" ", int(tossPaymentRequestMaxBodyBytes)+1)
}

func requireTossBodyTooLargeResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Contains(t, response.Message, "64 KiB")
}

func TestDecodeTossPaymentRequestJSONAcceptsNormalRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/toss/pay",
		strings.NewReader(`{"amount":1000,"amount_mode":"krw","payment_method":"toss"}`),
	)

	var request TossPayRequest
	require.NoError(t, decodeTossPaymentRequestJSON(c, &request))
	require.Equal(t, int64(1000), request.Amount)
	require.Equal(t, model.TossTopUpAmountModeKRW, request.AmountMode)
	require.Equal(t, model.PaymentMethodToss, request.PaymentMethod)
}

func TestTossTopUpJSONHandlersRejectOversizedChunkedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "quote", path: "/api/user/toss/amount", handler: RequestTossAmount},
		{name: "pay", path: "/api/user/toss/pay", handler: RequestTossPay},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(
				http.MethodPost,
				testCase.path,
				strings.NewReader(oversizedTossPaymentJSON(`{"amount":1000,"payment_method":"toss"}`)),
			)
			// Exercise the streaming/chunked path so protection does not depend on
			// a trustworthy Content-Length header.
			c.Request.ContentLength = -1

			testCase.handler(c)

			requireTossBodyTooLargeResponse(t, recorder)
		})
	}
}

func TestSubscriptionTossPayRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/subscription/toss/pay",
		strings.NewReader(oversizedTossPaymentJSON(`{"plan_id":1}`)),
	)

	SubscriptionRequestTossBilling(c)

	requireTossBodyTooLargeResponse(t, recorder)
}

func TestWalletAutoRechargeCreateRejectsOversizedChunkedBody(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		strings.NewReader(oversizedTossPaymentJSON(`{"preset_id":1}`)),
	)
	c.Request.ContentLength = -1

	requestWalletAutoRecharge(c, model.WalletAutoRechargeTypeScheduled, walletRechargeTarget{
		TargetType:  model.TopUpTargetTypeUser,
		TargetId:    1,
		OwnerUserId: 1,
	})

	requireTossBodyTooLargeResponse(t, recorder)
}

func TestResolveTossPaymentReconciliationEventRejectsOversizedChunkedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Set("id", 81)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/admin/toss/reconciliation-events/1/resolve",
		strings.NewReader(oversizedTossPaymentJSON(`{"resolution_note":"reviewed"}`)),
	)
	c.Request.ContentLength = -1

	ResolveTossPaymentReconciliationEvent(c)

	requireTossBodyTooLargeResponse(t, recorder)
}
