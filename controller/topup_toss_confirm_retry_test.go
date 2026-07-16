package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupRecordedGeneralTossTopUpForRetry(t *testing.T, orderID, paymentKey, secretKey string) model.TopUp {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{
		Id:       7,
		Username: "toss-confirm-retry-user",
	}).Error)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	now := model.GetDBTimestamp()
	topUp := model.TopUp{
		UserId:                7,
		Amount:                13000,
		Quota:                 321,
		TradeNo:               orderID,
		ProviderOrderId:       paymentKey,
		ProviderOrderTime:     now,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint("old_client_key"),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		CreateTime:            now,
		Status:                common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

func setupScopedGeneralTossCheckout(t *testing.T, orderID, secretKey string) model.TopUp {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	t.Setenv(model.TossPaymentKeyScopedIdempotencyEnv, "true")
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{
		Id: 7, Username: "toss-scoped-confirm-user", Status: common.UserStatusEnabled,
	}).Error)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)

	originalClientKey := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	setting.TossClientKey = "live_ck_scoped_confirm"
	setting.TossSecretKey = secretKey
	setting.TossTestMode = false
	t.Cleanup(func() {
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})

	topUp := model.TopUp{
		UserId: 7, TargetType: model.TopUpTargetTypeUser, TargetId: 7,
		Amount: 13000, Quota: 321, TradeNo: orderID, ProviderOrderId: orderID,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(setting.TossClientKey),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		CreateTime:            model.GetDBTimestamp(),
		Status:                common.TopUpStatusPending,
	}
	require.NoError(t, model.CreatePendingTossTopUpCheckout(&topUp))
	return topUp
}

func TestReconcileTossRecordedTopUpRetriesExactIdempotentConfirmAfterLookup404(t *testing.T) {
	const (
		orderID     = "toss_retry_response_loss"
		paymentKey  = "pay_retry_response_loss"
		exactSecret = "sk_exact_retry_secret"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)

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
	setting.TossClientKey = "new_client_key"
	setting.TossSecretKey = "sk_must_not_be_used"
	setting.TossTestMode = false
	tossAPIBase = "https://api.test.tosspayments.local"

	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(exactSecret+":"))
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, expectedAuthorization, r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			getCalls++
			require.Equal(t, "/v1/payments/"+paymentKey, r.URL.EscapedPath())
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		case http.MethodPost:
			postCalls++
			require.Equal(t, "/v1/payments/confirm", r.URL.EscapedPath())
			require.Equal(t, orderID, r.Header.Get("Idempotency-Key"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var payload struct {
				PaymentKey string `json:"paymentKey"`
				OrderID    string `json:"orderId"`
				Amount     int64  `json:"amount"`
			}
			require.NoError(t, common.Unmarshal(body, &payload))
			require.Equal(t, paymentKey, payload.PaymentKey)
			require.Equal(t, orderID, payload.OrderID)
			require.Equal(t, int64(13000), payload.Amount)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"paymentKey":"pay_retry_response_loss","type":"NORMAL","orderId":"toss_retry_response_loss","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****1111","amount":13000}}`,
				)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, getCalls)
	require.Equal(t, 1, postCalls)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	require.Empty(t, stored.ProviderClaimToken)
	var user model.User
	require.NoError(t, model.DB.First(&user, 7).Error)
	require.Equal(t, 321, user.Quota)
}

func TestReconcileTossRecordedTopUpLeavesPendingWhenIdempotentRetryStillProcessing(t *testing.T) {
	const (
		orderID     = "toss_retry_still_processing"
		paymentKey  = "pay_retry_still_processing"
		exactSecret = "sk_exact_processing_secret"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)

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
	setting.TossClientKey = "new_client_key"
	setting.TossSecretKey = "sk_must_not_be_used"
	setting.TossTestMode = false
	tossAPIBase = "https://api.test.tosspayments.local"

	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(exactSecret+":"))
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, expectedAuthorization, r.Header.Get("Authorization"))
		if r.Method == http.MethodGet {
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		}
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, orderID, r.Header.Get("Idempotency-Key"))
		postCalls++
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"IDEMPOTENT_REQUEST_PROCESSING","message":"processing"}`)),
		}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 2, getCalls, "cleanup must verify again after the retry remains ambiguous")
	require.Equal(t, tossAPIMaxAttempts, postCalls)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Empty(t, stored.ProviderClaimToken)
	var user model.User
	require.NoError(t, model.DB.First(&user, 7).Error)
	require.Zero(t, user.Quota)

	// A later cleanup process can reclaim the order after this worker released
	// its lease; it is not stranded behind a stale local authorization marker.
	token, _, claimed, err := model.ClaimPendingTossTopUpConfirmRetry(orderID, paymentKey, 13000)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.ReleasePendingTossTopUpConfirmRetry(orderID, token))
}

func TestReconcileTossRecordedTopUpUsesRotatedSameMIDSecretForLookupOnly(t *testing.T) {
	const (
		orderID      = "toss_retry_rotated_lookup_only"
		paymentKey   = "pay_retry_rotated_lookup_only"
		exactSecret  = "sk_expired_exact_secret"
		activeSecret = "sk_rotated_active_secret"
		clientKey    = "old_client_key"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)

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
	setting.TossSecretKey = activeSecret
	setting.TossTestMode = false
	tossAPIBase = "https://api.test.tosspayments.local"

	exactAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(exactSecret+":"))
	activeAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(activeSecret+":"))
	var exactGets, activeGets, exactPosts, activePosts int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		authorization := r.Header.Get("Authorization")
		switch r.Method {
		case http.MethodGet:
			switch authorization {
			case exactAuthorization:
				exactGets++
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`)),
				}, nil
			case activeAuthorization:
				activeGets++
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
				}, nil
			default:
				t.Fatalf("unexpected GET authorization %q", authorization)
			}
		case http.MethodPost:
			if authorization == activeAuthorization {
				activePosts++
				t.Fatal("rotated secret must never authorize a recovery POST")
			}
			require.Equal(t, exactAuthorization, authorization)
			exactPosts++
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return nil, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 2, exactGets)
	require.Equal(t, 2, activeGets)
	require.Equal(t, 1, exactPosts)
	require.Zero(t, activePosts)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	storedSecret, err := model.DecryptProviderCredential(stored.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, exactSecret, storedSecret)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestTossConfirmKillSwitchPerformsLookupButBlocksProviderPOST(t *testing.T) {
	const (
		orderID     = "toss_disabled_confirm"
		paymentKey  = "pay_disabled_confirm"
		exactSecret = "sk_disabled_confirm"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, orderID, exactSecret)
	setting.TossEnabled = false

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			postCalls++
			t.Fatalf("operational kill switch must block provider POST")
		}
		getCalls++
		require.Equal(t, "/v1/payments/"+paymentKey, r.URL.EscapedPath())
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId, "kill-switch lookup must not persist an unverified browser paymentKey")
	require.Equal(t, topUp.ProviderCredential, stored.ProviderCredential)
}

func TestTossConfirmKillSwitchPersistsAuthenticatedDoneKeyBeforeSettlementFailure(t *testing.T) {
	const (
		orderID     = "toss_disabled_paid_recovery"
		paymentKey  = "pay_disabled_paid_recovery"
		exactSecret = "sk_disabled_paid_recovery"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)
	setting.TossEnabled = false

	// Simulate a local settlement target disappearing after checkout. The
	// authenticated provider payment must remain recoverable even though the
	// immediate credit transaction cannot finish.
	require.NoError(t, model.DB.Delete(&model.User{}, 7).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			require.Equal(t, "/v1/payments/"+paymentKey, r.URL.EscapedPath())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"paymentKey":"pay_disabled_paid_recovery","type":"NORMAL","orderId":"toss_disabled_paid_recovery","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****1111","amount":13000}}`,
				)),
			}, nil
		case http.MethodPost:
			postCalls++
			t.Fatalf("operational kill switch must block provider POST")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return nil, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId,
		"an authenticated paid paymentKey must select the recorded-payment recovery lane")
	require.Greater(t, stored.ProviderOrderTime, int64(0))

	var fulfillment model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFulfillment).First(&fulfillment).Error)
	require.Equal(t, paymentKey, fulfillment.PaymentKey)
	require.Equal(t, model.TossReconciliationStatusRequired, fulfillment.ReconciliationStatus)
}

func TestTossConfirmPastLocalWindowUsesAuthoritativeLookupWithoutProviderPOST(t *testing.T) {
	const (
		orderID     = "toss_expired_browser_callback"
		paymentKey  = "pay_expired_browser_callback"
		exactSecret = "sk_expired_browser_callback"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", orderID).
		Update("create_time", model.GetDBTimestamp()-int64(16*24*time.Hour/time.Second)).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		case http.MethodPost:
			postCalls++
			t.Fatalf("an expired local checkout must never create a new confirm operation")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return nil, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId,
		"a lookup miss must not bind an untrusted paymentKey to an expired checkout")
}

func TestTossConfirmProviderPostWindowBoundaries(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	now := model.GetDBTimestamp()
	recordedWindow := int64(tossRecordedTopUpTerminalAge / time.Second)
	pristineWindow := int64(tossNeverRecordedTopUpTerminalAge / time.Second)

	tests := []struct {
		name  string
		topUp model.TopUp
		want  bool
	}{
		{
			name: "recorded just before fifteen minute grace",
			topUp: model.TopUp{TradeNo: "toss_recorded_before", ProviderOrderId: "pay_recorded_before",
				ProviderOrderTime: now - recordedWindow + 5},
		},
		{
			name: "recorded just after fifteen minute grace",
			topUp: model.TopUp{TradeNo: "toss_recorded_after", ProviderOrderId: "pay_recorded_after",
				ProviderOrderTime: now - recordedWindow - 1},
			want: true,
		},
		{
			name: "pristine just before fifty minute local expiry",
			topUp: model.TopUp{TradeNo: "toss_pristine_before", ProviderOrderId: "toss_pristine_before",
				CreateTime: now - pristineWindow + 5},
		},
		{
			name: "pristine just after fifty minute local expiry",
			topUp: model.TopUp{TradeNo: "toss_pristine_after", ProviderOrderId: "toss_pristine_after",
				CreateTime: now - pristineWindow - 1},
			want: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, isTossConfirmProviderPostWindowElapsed(&testCase.topUp))
		})
	}
}

func TestReconcileTossRecordedTopUpPastApprovalWindowDoesNotReplayConfirm(t *testing.T) {
	const (
		orderID     = "toss_expired_recorded_confirm"
		paymentKey  = "pay_expired_recorded_confirm"
		exactSecret = "sk_expired_recorded_confirm"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)
	topUp.ProviderOrderTime = model.GetDBTimestamp() - int64(16*24*time.Hour/time.Second)
	topUp.ProviderRetryTime = 0
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", orderID).Updates(map[string]interface{}{
		"provider_order_time": topUp.ProviderOrderTime,
		"provider_retry_time": topUp.ProviderRetryTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		case http.MethodPost:
			postCalls++
			t.Fatalf("a recorded payment outside the approval window must not replay /confirm")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return nil, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusExpired, stored.Status)
}

func TestTossConfirmScopedBindingRestoredAfterTerminalForgedKeyRejection(t *testing.T) {
	const (
		orderID     = "toss_scoped_forged_terminal"
		forgedKey   = "pay_scoped_forged_terminal"
		exactSecret = "live_sk_scoped_forged_terminal"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	var posts, gets int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodPost:
			posts++
			expectedKey := "toss_confirm_" + common.Sha1([]byte(orderID+"\x00"+forgedKey))
			require.Equal(t, expectedKey, r.Header.Get("Idempotency-Key"))
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"INVALID_PAYMENT_KEY","message":"invalid"}`)),
			}, nil
		case http.MethodGet:
			gets++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s", r.Method)
			return nil, nil
		}
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+forgedKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, gets)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderIdempotencyKey)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestTossConfirmKeepsScopedBindingWhenErrorBodyIsUnreadable(t *testing.T) {
	const (
		orderID     = "toss_scoped_unreadable_error"
		paymentKey  = "pay_scoped_unreadable_error"
		exactSecret = "live_sk_scoped_unreadable_error"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	var posts, gets int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodPost:
			posts++
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       tossReadErrorBody{},
			}, nil
		case http.MethodGet:
			gets++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s", request.Method)
			return nil, nil
		}
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, gets)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	require.True(t, stored.ProviderAttempted)
	require.Equal(t, "toss_confirm_"+common.Sha1([]byte(orderID+"\x00"+paymentKey)), stored.ProviderIdempotencyKey)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestTossConfirmScopedBindingRestoredAfterAuthenticated2xxOrderMismatch(t *testing.T) {
	const (
		orderID     = "toss_scoped_2xx_mismatch"
		forgedKey   = "pay_scoped_2xx_mismatch"
		exactSecret = "live_sk_scoped_2xx_mismatch"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_scoped_2xx_mismatch","type":"NORMAL","orderId":"toss_other_2xx_order","status":"DONE","totalAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****1111","amount":13000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+forgedKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderClaimToken)
	var eventCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ? AND payment_key = ?", "toss_other_2xx_order", forgedKey).Count(&eventCount).Error)
	require.Zero(t, eventCount)
}

func TestReconcileTossRecordedTopUpRestoresScopedBindingAfterOrderMismatch(t *testing.T) {
	const (
		orderID     = "toss_scoped_reconcile_mismatch"
		forgedKey   = "pay_scoped_reconcile_mismatch"
		exactSecret = "live_sk_scoped_reconcile_mismatch"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)
	require.NoError(t, model.RecordTossPaymentKey(orderID, forgedKey))
	token, _, claimed, err := model.ClaimPendingTossTopUpConfirmRetry(orderID, forgedKey, 13000)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = model.PrepareClaimedPendingTossTopUpConfirm(orderID, forgedKey, 13000, token, exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.ReleasePendingTossTopUpConfirmRetry(orderID, token))
	current, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_scoped_reconcile_mismatch","type":"NORMAL","orderId":"toss_other_reconcile_order","status":"DONE","totalAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****1111","amount":13000}}`,
			)),
		}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), *current)
	require.NoError(t, err)
	require.False(t, resolved, "restored checkout remains pending for its legitimate callback")
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestTossConfirmInactiveUserRestoresPristineScopedBindingWithoutProviderCall(t *testing.T) {
	const (
		orderID     = "toss_inactive_user_lookup_miss"
		paymentKey  = "pay_inactive_user_lookup_miss"
		exactSecret = "sk_inactive_user_lookup_miss"
	)
	setupScopedGeneralTossCheckout(t, orderID, exactSecret)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("pristine scoped lifecycle rejection must not call Toss: %s", r.Method)
		return nil, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId)
	require.False(t, stored.ProviderAttempted)
	require.Empty(t, stored.ProviderClaimToken)
}

func TestTossConfirmInactiveUserStillSettlesAlreadyDonePayment(t *testing.T) {
	const (
		orderID     = "toss_inactive_user_already_done"
		paymentKey  = "pay_inactive_user_already_done"
		exactSecret = "sk_inactive_user_already_done"
	)
	setupRecordedGeneralTossTopUpForRetry(t, orderID, orderID, exactSecret)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Updates(map[string]interface{}{
		"status": common.UserStatusDisabled,
		"quota":  0,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method, "inactive lifecycle must use lookup-only recovery")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_inactive_user_already_done","type":"NORMAL","orderId":"toss_inactive_user_already_done","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****1111","amount":13000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey="+paymentKey+"&orderId="+orderID+"&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "/console/log", recorder.Header().Get("Location"))
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 7).Error)
	require.Equal(t, 321, user.Quota)
}

func TestReconcileTossRecordedTopUpKillSwitchDoesNotRetryOrFailLookupMiss(t *testing.T) {
	const (
		orderID     = "toss_disabled_retry"
		paymentKey  = "pay_disabled_retry"
		exactSecret = "sk_disabled_retry"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)
	setting.TossEnabled = false

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	getCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		getCalls++
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
		}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, getCalls)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestReconcileTossRecordedTopUpRejectsBillingPaymentBeforeTerminalMutation(t *testing.T) {
	const (
		orderID     = "toss_wrong_type_terminal"
		paymentKey  = "pay_wrong_type_terminal"
		exactSecret = "sk_wrong_type_terminal"
	)
	topUp := setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_wrong_type_terminal","type":"BILLING","orderId":"toss_wrong_type_terminal","status":"EXPIRED","totalAmount":13000,"currency":"KRW","method":"카드","card":{"amount":13000}}`,
			)),
		}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.Error(t, err)
	require.False(t, resolved)
	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestTossWebhookRejectsBillingPaymentBeforeTopUpTerminalMutation(t *testing.T) {
	const (
		orderID     = "toss_wrong_type_webhook"
		paymentKey  = "pay_wrong_type_webhook"
		exactSecret = "sk_wrong_type_webhook"
	)
	setupRecordedGeneralTossTopUpForRetry(t, orderID, paymentKey, exactSecret)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_wrong_type_webhook","type":"BILLING","orderId":"toss_wrong_type_webhook","status":"EXPIRED","totalAmount":13000,"currency":"KRW","method":"카드","card":{"amount":13000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_wrong_type_webhook","paymentKey":"pay_wrong_type_webhook","status":"EXPIRED"}}`),
	)
	TossWebhook(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestTossWebhookRecoversLateDoneAfterLocalTerminalState(t *testing.T) {
	const (
		orderID     = "toss_late_done_terminal"
		paymentKey  = "pay_late_done_terminal"
		exactSecret = "sk_late_done_terminal"
	)
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.TossPaymentEvent{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 27, Username: "late-done-user"}).Error)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             27,
		Amount:             13000,
		Quota:              444,
		TradeNo:            orderID,
		ProviderOrderId:    paymentKey,
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusExpired,
		CreateTime:         1,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	expectedAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(exactSecret+":"))
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, expectedAuthorization, r.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_late_done_terminal","type":"NORMAL","orderId":"toss_late_done_terminal","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"card","number":"****2727","amount":13000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_late_done_terminal","paymentKey":"pay_late_done_terminal","status":"DONE"}}`),
	)
	TossWebhook(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
	require.Equal(t, paymentKey, stored.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 27).Error)
	require.Equal(t, 444, user.Quota)
	var eventCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFulfillment).Count(&eventCount).Error)
	require.Zero(t, eventCount)
}

func TestTossWebhookLateDonePayloadMismatchFallsBackToDurableEvent(t *testing.T) {
	const (
		orderID     = "toss_late_done_mismatch"
		paymentKey  = "pay_late_done_mismatch"
		exactSecret = "sk_late_done_mismatch"
	)
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.TossPaymentEvent{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 28, Username: "late-done-mismatch-user"}).Error)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             28,
		Amount:             13000,
		Quota:              555,
		TradeNo:            orderID,
		ProviderOrderId:    paymentKey,
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusExpired,
		CreateTime:         1,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_late_done_mismatch","type":"NORMAL","orderId":"toss_late_done_mismatch","status":"DONE","totalAmount":12000,"currency":"KRW","method":"카드","card":{"amount":12000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_late_done_mismatch","paymentKey":"pay_late_done_mismatch","status":"DONE"}}`),
	)
	TossWebhook(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusExpired, stored.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 28).Error)
	require.Zero(t, user.Quota)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFinancialMismatch).First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
	require.Equal(t, paymentKey, event.PaymentKey)
}

func TestTossWebhookCreditedOrderMismatchDoesNotResolveLegacyFulfillment(t *testing.T) {
	const (
		orderID     = "toss_credited_legacy_mismatch"
		paymentKey  = "pay_credited_legacy_mismatch"
		exactSecret = "sk_credited_legacy_mismatch"
	)
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.TossPaymentEvent{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 29, Username: "credited-legacy-mismatch", Quota: 555}).Error)
	credential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 29, Amount: 13000, Quota: 555, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess,
	}).Error)
	created, err := model.RecordTossPaymentEvent(&model.TossPaymentEvent{
		EventKey:             "legacy-credited-misclassified-fulfillment",
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               "DONE",
		OriginalAmount:       12000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.True(t, created)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_credited_legacy_mismatch","type":"NORMAL","orderId":"toss_credited_legacy_mismatch","status":"DONE","totalAmount":12000,"balanceAmount":12000,"currency":"KRW","method":"카드","card":{"amount":12000}}`,
			)),
		}, nil
	})}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_credited_legacy_mismatch","paymentKey":"pay_credited_legacy_mismatch","status":"DONE"}}`),
	)
	TossWebhook(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var legacy model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", "legacy-credited-misclassified-fulfillment").First(&legacy).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, legacy.ReconciliationStatus)
	var mismatch model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFinancialMismatch).First(&mismatch).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, mismatch.ReconciliationStatus)
}
