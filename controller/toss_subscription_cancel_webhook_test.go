package controller

import (
	"bytes"
	"encoding/base64"
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

func TestTossSubscriptionCancellationWebhookStopsFutureRecurringCharges(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}))
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	originalBase := tossAPIBase
	originalHTTPClient := http.DefaultClient
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
		tossAPIBase = originalBase
		http.DefaultClient = originalHTTPClient
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "live_ck_subscription_cancel"
	setting.TossBillingSecretKey = "live_sk_subscription_cancel"
	require.NoError(t, model.DB.Create(&model.User{
		Id:       701,
		Username: "subscription-cancel-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "subscription-cancel-aff",
	}).Error)
	keyID, err := model.StoreTossBillingKeyWithProviderSnapshot(
		701,
		"subscription-cancel-customer",
		"subscription-cancel-billing-key",
		"현대",
		"433012******1234",
		"live_sk_subscription_cancel",
		model.TossClientKeyFingerprint("live_ck_subscription_cancel"),
	)
	require.NoError(t, err)
	subscription := &model.UserSubscription{
		Id:           702,
		UserId:       701,
		PlanId:       703,
		Status:       "active",
		AutoRenew:    true,
		BillingKeyId: keyID,
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	otherSubscription := &model.UserSubscription{
		Id:           704,
		UserId:       701,
		PlanId:       703,
		Status:       "active",
		AutoRenew:    true,
		BillingKeyId: keyID,
	}
	require.NoError(t, model.DB.Create(otherSubscription).Error)
	credential, err := model.EncryptProviderCredential("stored_sk_subscription_cancel")
	require.NoError(t, err)
	attemptCredential, err := model.EncryptProviderCredential("attempt_sk_subscription_cancel")
	require.NoError(t, err)
	const orderID = "toss_sub_cancel_webhook"
	plan := &model.SubscriptionPlan{
		Id: 703, Title: "Webhook cancellation origin", PriceAmount: 1, Currency: "USD",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	order := &model.SubscriptionOrder{
		UserId:                   701,
		PlanId:                   703,
		Money:                    plan.PriceAmount,
		TradeNo:                  orderID,
		PaymentMethod:            model.PaymentMethodToss,
		PaymentProvider:          model.PaymentProviderToss,
		Status:                   common.TopUpStatusSuccess,
		ProviderAmount:           1000,
		ProviderCurrency:         "KRW",
		BillingKeyId:             keyID,
		ProviderCredential:       credential,
		ProviderClientKeyHash:    model.TossClientKeyFingerprint("stored_ck_subscription_cancel"),
		BillingAttempted:         true,
		BillingAttemptCredential: attemptCredential,
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(order, plan))
	require.NoError(t, model.SetTossRenewalContractFromInitialOrder(subscription, order))
	require.NoError(t, model.DB.Model(subscription).
		Update("toss_renewal_contract_snapshot", subscription.TossRenewalContractSnapshot).Error)
	require.NoError(t, model.DB.Create(order).Error)

	tossAPIBase = "https://api.test.tosspayments.local"
	attemptAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("attempt_sk_subscription_cancel:"))
	storedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("stored_sk_subscription_cancel:"))
	lookupCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		lookupCalls++
		require.Equal(t, http.MethodGet, request.Method)
		require.True(t, strings.Contains(request.URL.Path, "pay_subscription_cancel"))
		switch request.Header.Get("Authorization") {
		case attemptAuthorization:
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`)),
			}, nil
		case storedAuthorization:
			// The active key is for a different MID and must not be tried. The
			// order-time credential is the only safe fallback after the exact
			// attempt credential is rejected.
		default:
			t.Fatalf("unexpected subscription cancellation lookup credential %q", request.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_subscription_cancel","type":"BILLING","orderId":"toss_sub_cancel_webhook","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"taxFreeAmount":100,"currency":"KRW","method":"카드","card":{"amount":1000,"company":"현대","number":"433012******1234"},"cancels":[{"cancelAmount":1000,"transactionKey":"cancel_subscription_tx","cancelStatus":"DONE"}]}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_cancel_webhook","paymentKey":"pay_subscription_cancel","status":"CANCELED"}}`,
	))
	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 2, lookupCalls)
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 702).Error)
	require.False(t, sub.AutoRenew)
	var other model.UserSubscription
	require.NoError(t, model.DB.First(&other, 704).Error)
	require.True(t, other.AutoRenew)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).First(&event).Error)
	require.Equal(t, int64(1000), event.CancelAmount)
}
