package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTossConfirmKeepsPendingAfterSuccessfulMalformedResponseAndLookupMiss(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 71, Username: "toss-ambiguous-user", Group: "default"}).Error)

	const (
		clientKey  = "live_ck_ambiguous_mid"
		secretKey  = "live_sk_ambiguous_mid"
		orderID    = "toss_ambiguous_2xx"
		paymentKey = "pay_ambiguous_2xx"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                71,
		Amount:                13000,
		Money:                 10,
		Quota:                 10,
		TradeNo:               orderID,
		ProviderOrderId:       orderID,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(clientKey),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		CreateTime:            model.GetDBTimestamp(),
		Status:                common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalClientKey := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossClientKey = clientKey
	setting.TossSecretKey = secretKey
	setting.TossTestMode = false

	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(secretKey+":"))
	postCalls := 0
	getCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, wantAuthorization, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/payments/confirm":
			postCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"paymentKey":`)),
			}, nil
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/"+paymentKey:
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"code":"NOT_FOUND_PAYMENT"}`)),
			}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, postCalls)
	require.Equal(t, 1, getCalls)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
}

func TestConfirmTossBillingChargeKeeps2xxDecodeFailurePendingAfterLookupMiss(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	const (
		secretKey = "live_sk_billing_ambiguous"
		orderID   = "toss_sub_ambiguous_2xx"
	)
	postCalls := 0
	getCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_ambiguous":
			postCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"paymentKey":`)),
			}, nil
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/"+orderID:
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"code":"NOT_FOUND_PAYMENT"}`)),
			}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, err := confirmTossBillingChargeOrLookupWithSecret(
		context.Background(),
		"billing_ambiguous",
		"customer_ambiguous",
		secretKey,
		orderID,
		"구독",
		15000,
	)

	require.Nil(t, result)
	require.ErrorIs(t, err, model.ErrTossBillingChargePending)
	require.Equal(t, 1, postCalls)
	require.Equal(t, 1, getCalls)
}

func runTossConfirmAmbiguousProviderResponseCase(t *testing.T, orderID, paymentKey string, postStatus int, postBody string) {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 72, Username: "toss-ambiguous-provider-user", Group: "default"}).Error)

	const (
		clientKey = "live_ck_ambiguous_provider_mid"
		secretKey = "live_sk_ambiguous_provider_mid"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                72,
		Amount:                13000,
		Money:                 10,
		Quota:                 10,
		TradeNo:               orderID,
		ProviderOrderId:       orderID,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(clientKey),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		CreateTime:            model.GetDBTimestamp(),
		Status:                common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalClientKey := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossClientKey = clientKey
	setting.TossSecretKey = secretKey
	setting.TossTestMode = false

	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(secretKey+":"))
	postCalls := 0
	getCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, wantAuthorization, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/payments/confirm":
			postCalls++
			return &http.Response{
				StatusCode: postStatus,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(postBody)),
			}, nil
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/"+paymentKey:
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBufferString(`{"code":"NOT_FOUND_PAYMENT"}`)),
			}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, postCalls)
	require.Equal(t, 1, getCalls)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
}

func TestTossConfirmKeepsPendingAfterAmbiguousProviderResponseAndLookupMiss(t *testing.T) {
	tests := []struct {
		name       string
		orderID    string
		paymentKey string
		postStatus int
		postBody   string
	}{
		{
			name:       "incomplete successful response",
			orderID:    "toss_ambiguous_empty_2xx",
			paymentKey: "pay_ambiguous_empty_2xx",
			postStatus: http.StatusOK,
			postBody:   `{}`,
		},
		{
			name:       "already processed payment",
			orderID:    "toss_ambiguous_already_processed",
			paymentKey: "pay_ambiguous_already_processed",
			postStatus: http.StatusBadRequest,
			postBody:   `{"code":"ALREADY_PROCESSED_PAYMENT","message":"already processed"}`,
		},
		{
			name:       "provider error",
			orderID:    "toss_ambiguous_provider_error",
			paymentKey: "pay_ambiguous_provider_error",
			postStatus: http.StatusBadRequest,
			postBody:   `{"code":"PROVIDER_ERROR","message":"retry later"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runTossConfirmAmbiguousProviderResponseCase(t, tc.orderID, tc.paymentKey, tc.postStatus, tc.postBody)
		})
	}
}

func TestConfirmTossBillingChargeKeepsAmbiguousProviderResponsesPendingAfterLookupMiss(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
	tests := []struct {
		name       string
		postStatus int
		postBody   string
	}{
		{
			name:       "incomplete successful response",
			postStatus: http.StatusOK,
			postBody:   `{}`,
		},
		{
			name:       "already processed payment",
			postStatus: http.StatusBadRequest,
			postBody:   `{"code":"ALREADY_PROCESSED_PAYMENT","message":"already processed"}`,
		},
		{
			name:       "provider error",
			postStatus: http.StatusBadRequest,
			postBody:   `{"code":"PROVIDER_ERROR","message":"retry later"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalBase := tossAPIBase
			originalClient := http.DefaultClient
			t.Cleanup(func() {
				tossAPIBase = originalBase
				http.DefaultClient = originalClient
			})

			const (
				secretKey = "live_sk_billing_provider_ambiguous"
				orderID   = "toss_sub_provider_ambiguous"
			)
			postCalls := 0
			getCalls := 0
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_provider_ambiguous":
					postCalls++
					return &http.Response{
						StatusCode: tc.postStatus,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(bytes.NewBufferString(tc.postBody)),
					}, nil
				case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/"+orderID:
					getCalls++
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(bytes.NewBufferString(`{"code":"NOT_FOUND_PAYMENT"}`)),
					}, nil
				default:
					t.Fatalf("unexpected Toss request %s %s", r.Method, r.URL.EscapedPath())
					return nil, nil
				}
			})}
			tossAPIBase = "https://api.test.tosspayments.local"

			result, err := confirmTossBillingChargeOrLookupWithSecret(
				context.Background(),
				"billing_provider_ambiguous",
				"customer_provider_ambiguous",
				secretKey,
				orderID,
				"구독",
				15000,
			)

			require.Nil(t, result)
			require.ErrorIs(t, err, model.ErrTossBillingChargePending)
			require.Equal(t, 1, postCalls)
			require.Equal(t, 1, getCalls)
		})
	}
}

func TestIssueTossBillingKeyTreatsIncompleteSuccessfulResponseAsUncertain(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/billing/authorizations/issue", r.URL.EscapedPath())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKeyWithSecret(
		context.Background(),
		"auth_incomplete_2xx",
		"customer_incomplete_2xx",
		"toss_issue_incomplete_2xx",
		"live_sk_issue_incomplete_2xx",
	)

	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, issued)
	require.Empty(t, issued.BillingKey)
	require.Error(t, err)
	require.True(t, isTossBillingIssueResponseUncertain(issued, status, err))
}

func TestIssueTossBillingKeyTreatsTemporaryProviderErrorAsUncertain(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/billing/authorizations/issue", r.URL.EscapedPath())
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"code":"PROVIDER_ERROR","message":"retry later"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKeyWithSecret(
		context.Background(),
		"auth_provider_error",
		"customer_provider_error",
		"toss_issue_provider_error",
		"live_sk_issue_provider_error",
	)

	require.Equal(t, http.StatusBadRequest, status)
	require.Nil(t, issued)
	require.Error(t, err)
	require.True(t, isTossBillingIssueResponseUncertain(issued, status, err))
}
