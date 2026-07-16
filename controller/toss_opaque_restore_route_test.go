package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedRestoredOpaqueWalletTopUpForControllerTest(t *testing.T) model.TopUp {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	tradeNo := "twa_" + strings.Repeat("a", 40)
	topUp := model.TopUp{
		UserId: 1, TargetType: model.TopUpTargetTypeUser, TargetId: 1,
		Amount: 10000, TradeNo: tradeNo, ProviderOrderId: tradeNo,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: "",
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

func TestTossConfirmRejectsRestoredOpaqueWalletOrderBeforePaymentKeyBinding(t *testing.T) {
	topUp := seedRestoredOpaqueWalletTopUpForControllerTest(t)
	gin.SetMode(gin.TestMode)

	originalClient := http.DefaultClient
	providerCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(`{"code":"UNEXPECTED_PROVIDER_CALL"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	query := url.Values{}
	query.Set("paymentKey", "pay_opaque_restore_wallet")
	query.Set("orderId", topUp.TradeNo)
	query.Set("amount", "10000")
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/toss/confirm?"+query.Encode(), nil)

	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Zero(t, providerCalls)
	var stored model.TopUp
	require.NoError(t, model.DB.First(&stored, topUp.Id).Error)
	require.Equal(t, topUp.TradeNo, stored.ProviderOrderId)
	require.False(t, stored.ProviderAttempted)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestTossWebhookReturns503ForRestoredOpaqueWalletOrder(t *testing.T) {
	topUp := seedRestoredOpaqueWalletTopUpForControllerTest(t)
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("id = ?", topUp.Id).
		Update("payment_provider", nil).Error)
	gin.SetMode(gin.TestMode)

	originalClient := http.DefaultClient
	providerCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		return nil, io.EOF
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })

	body := `{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"` + topUp.TradeNo + `","paymentKey":"pay_opaque_restore_wallet","status":"DONE"}}`
	recorder := performTossWebhookForTest(body)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Zero(t, providerCalls)
	var stored model.TopUp
	require.NoError(t, model.DB.First(&stored, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestTossSubscriptionReconcilerRejectsOpaqueRestoreWithNullProviderBeforeGET(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}))
	order := model.SubscriptionOrder{
		UserId:          1,
		TradeNo:         "trn_" + strings.Repeat("b", 40),
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: "",
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&order).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("payment_provider", nil).Error)

	originalClient := http.DefaultClient
	providerCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		return nil, io.EOF
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.False(t, resolved)
	require.ErrorIs(t, err, model.ErrTossRecurringOrderIDEvidenceCorrupt)
	require.Zero(t, providerCalls)
	var stored model.SubscriptionOrder
	require.NoError(t, model.DB.First(&stored, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}
