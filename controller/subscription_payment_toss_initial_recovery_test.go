package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type recoverableTossFirstChargeFixture struct {
	Order       model.SubscriptionOrder
	Plan        model.SubscriptionPlan
	ExactSecret string
	BillingKey  string
}

func TestInitialTossCancellationResultBlocksReplacementUntilReconciled(t *testing.T) {
	tests := []struct {
		name                string
		status              string
		balance             int64
		strictCancellation  bool
		preseedCancellation bool
		wantSafeFull        bool
		wantBlocked         bool
	}{
		{
			name:   "strict partial cancellation",
			status: "PARTIAL_CANCELED", balance: 5000, strictCancellation: true,
			preseedCancellation: true, wantBlocked: true,
		},
		{
			name:   "malformed full cancellation",
			status: "CANCELED", balance: 0, strictCancellation: false,
			wantBlocked: true,
		},
		{
			name:   "strict full cancellation",
			status: "CANCELED", balance: 0, strictCancellation: true,
			preseedCancellation: true, wantSafeFull: true, wantBlocked: false,
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(
				&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.TossPaymentEvent{},
			))
			user := model.User{Id: 9400 + i, Username: "initial-cancel-barrier-" + strconv.Itoa(i), Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, model.DB.Create(&user).Error)
			plan := model.SubscriptionPlan{
				Id: 9450 + i, Title: "Initial cancellation barrier", PriceAmount: 15, Currency: "USD",
				DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
			}
			require.NoError(t, model.DB.Create(&plan).Error)
			order := model.SubscriptionOrder{
				UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount,
				TradeNo:       "toss_initial_cancel_barrier_" + strconv.Itoa(i),
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusPending, ProviderAmount: 15000, ProviderCurrency: "KRW",
				BillingAttempted: true,
			}
			require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
			require.NoError(t, model.DB.Create(&order).Error)
			if test.preseedCancellation {
				created, err := model.RecordTossPaymentEvent(&model.TossPaymentEvent{
					EventKey:  "initial-cancel-event-" + strconv.Itoa(i),
					EventType: model.TossPaymentEventTypeCancellation,
					OrderId:   order.TradeNo, PaymentKey: "pay_initial_cancel_" + strconv.Itoa(i),
					Status: test.status, OriginalAmount: order.ProviderAmount, BalanceAmount: test.balance,
					ReconciliationStatus: model.TossReconciliationStatusRequired,
				})
				require.NoError(t, err)
				require.True(t, created)
			}

			safeFull, stopErr := stopInitialTossSubscriptionAfterCancellationResult(
				context.Background(),
				&order,
				0,
				test.status,
				order.ProviderAmount,
				test.balance,
				"pay_initial_cancel_"+strconv.Itoa(i),
				`{"status":"`+test.status+`"}`,
				test.strictCancellation,
			)
			require.Equal(t, test.wantSafeFull, safeFull)
			if test.wantSafeFull {
				require.NoError(t, stopErr)
			} else {
				require.Error(t, stopErr)
			}
			require.NoError(t, model.DB.First(&order, order.Id).Error)
			require.Equal(t, common.TopUpStatusFailed, order.Status)

			newReplacement := func(suffix string) *model.SubscriptionOrder {
				replacement := &model.SubscriptionOrder{
					UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount,
					TradeNo:       "toss_initial_cancel_replacement_" + strconv.Itoa(i) + "_" + suffix,
					PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
					Status: common.TopUpStatusPending, ProviderAmount: 15000, ProviderCurrency: "KRW",
				}
				require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(replacement, &plan))
				return replacement
			}
			replacement := newReplacement("first")
			replaceErr := model.CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan)
			if !test.wantBlocked {
				require.NoError(t, replaceErr)
				return
			}
			require.ErrorIs(t, replaceErr, model.ErrPersonalTossBillingInFlight)
			var event model.TossPaymentEvent
			require.NoError(t, model.DB.Where("order_id = ? AND reconciliation_status = ?", order.TradeNo, model.TossReconciliationStatusRequired).First(&event).Error)
			_, err := model.ResolveTossPaymentEventByAdmin(event.Id, 1, "initial cancellation reviewed")
			require.NoError(t, err)
			require.NoError(t, model.CreateTossSubscriptionOrderWithPurchaseReservation(newReplacement("resolved"), &plan))
		})
	}
}

func seedRecoverableTossFirstCharge(t *testing.T, suffix string, legacyAttemptCredential bool) recoverableTossFirstChargeFixture {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
		&model.TopUp{},
		&model.Log{},
	))
	user := model.User{Id: 91, Username: "first-charge-" + suffix, Status: common.UserStatusEnabled, Group: "default", TossCustomerKey: "cust_first_" + suffix}
	require.NoError(t, model.DB.Create(&user).Error)
	plan := model.SubscriptionPlan{
		Id: 4901, Title: "First charge recovery", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	exactSecret := "sk_exact_first_" + suffix
	billingKey := "billing_first_" + suffix
	keyID, err := model.StoreTossBillingKeyWithProviderSnapshot(
		user.Id, user.TossCustomerKey, billingKey, "현대", "433012******1234",
		exactSecret, model.TossBillingClientKeyFingerprint("ck_first_"+suffix),
	)
	require.NoError(t, err)
	providerCredential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	attemptCredential := providerCredential
	billingAttempted := true
	protocolVersion := 1
	if legacyAttemptCredential {
		// A pre-marker order persisted ProviderCredential before the provider
		// POST, but the additive attempt columns read as zero after AutoMigrate.
		// Recovery must claim this v0 shape and copy that exact credential before
		// the GET-before-POST retry; attempted=true with an empty exact snapshot
		// would instead be a corrupt state that correctly fails closed.
		attemptCredential = ""
		billingAttempted = false
		protocolVersion = 0
	}
	order := model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo: "toss_sub_first_" + suffix, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
		CreateTime: common.GetTimestamp() - 600, ProviderAmount: 15000, ProviderCurrency: "KRW",
		ProviderCredential: providerCredential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("ck_first_" + suffix),
		BillingKeyId: keyID, BillingClaimToken: "crashed-owner", BillingClaimTime: common.GetTimestamp() - 600,
		BillingAttempted: billingAttempted, BillingAttemptCredential: attemptCredential,
		BillingChargeProtocolVersion: protocolVersion,
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	return recoverableTossFirstChargeFixture{Order: order, Plan: plan, ExactSecret: exactSecret, BillingKey: billingKey}
}

func TestReconcileTossInitialChargeAfterCrashAnd404RepeatsOnlySameSecretAndOrder(t *testing.T) {
	fixture := seedRecoverableTossFirstCharge(t, "crash_404", true)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixture.ExactSecret+":"))
	requests := make([]string, 0, 2)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, wantAuthorization, r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/"+fixture.Order.TradeNo:
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT"}`))}, nil
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/"+fixture.BillingKey:
			require.Equal(t, fixture.Order.TradeNo, r.Header.Get("Idempotency-Key"))
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"paymentKey":"pay_first_crash_404","type":"BILLING","orderId":"toss_sub_first_crash_404","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`))}, nil
		default:
			t.Fatalf("unexpected Toss request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, []string{
		http.MethodGet + " /v1/payments/orders/" + fixture.Order.TradeNo,
		http.MethodPost + " /v1/billing/" + fixture.BillingKey,
	}, requests)

	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	attemptSecret, err := model.DecryptProviderCredential(order.BillingAttemptCredential)
	require.NoError(t, err)
	require.Equal(t, fixture.ExactSecret, attemptSecret, "legacy marker must be upgraded to the exact first-charge secret")
	var subscriptions int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", order.UserId).Count(&subscriptions).Error)
	require.Equal(t, int64(1), subscriptions)

	// A redelivery/repeated cleanup sees local success and never calls Toss or
	// creates a second subscription.
	resolved, err = reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Len(t, requests, 2)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", order.UserId).Count(&subscriptions).Error)
	require.Equal(t, int64(1), subscriptions)
}

func TestReconcileTossInitialChargeOldExact404ExpiresWithoutPOST(t *testing.T) {
	fixture := seedRecoverableTossFirstCharge(t, "old_exact_404", false)
	staleTime := model.GetDBTimestamp() - model.TossSubscriptionPurchaseReservationMaxAgeSeconds - 1
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", fixture.Order.Id).Updates(map[string]interface{}{
		"create_time":        staleTime,
		"billing_claim_time": staleTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	getCalls, postCalls := 0, 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			require.Equal(t, "/v1/payments/orders/"+fixture.Order.TradeNo, r.URL.EscapedPath())
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT"}`))}, nil
		case http.MethodPost:
			postCalls++
			t.Fatal("an initial charge outside the recovery window must never POST")
		}
		return nil, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Empty(t, order.BillingClaimToken)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, fixture.Order.BillingKeyId).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
}

func TestReconcileTossInitialChargeExpiredSecretUsesSameMIDGETWithoutNewPOST(t *testing.T) {
	fixture := seedRecoverableTossFirstCharge(t, "expired_secret", false)
	activeSecret := "sk_current_first_expired_secret"
	originalTestMode := setting.TossTestMode
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	t.Cleanup(func() {
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = "ck_first_expired_secret"
	setting.TossBillingSecretKey = activeSecret
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	exactAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixture.ExactSecret+":"))
	activeAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(activeSecret+":"))
	requests := make([]string, 0, 2)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.Header.Get("Authorization"))
		require.Equal(t, http.MethodGet, r.Method, "rotated credential fallback is lookup-only")
		switch r.Header.Get("Authorization") {
		case exactAuthorization:
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`))}, nil
		case activeAuthorization:
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT"}`))}, nil
		default:
			t.Fatalf("unexpected authorization %q", r.Header.Get("Authorization"))
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.False(t, resolved)
	require.ErrorIs(t, err, model.ErrTossBillingCrossCredentialRetryUnsafe)
	require.Equal(t, []string{
		http.MethodGet + " " + exactAuthorization,
		http.MethodGet + " " + activeAuthorization,
	}, requests)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.NotEmpty(t, order.BillingClaimToken)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, fixture.Order.BillingKeyId).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
}

func TestReconcileTossInitialCharge404DoesNotPostAfterUserDisabled(t *testing.T) {
	fixture := seedRecoverableTossFirstCharge(t, "disabled", false)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", fixture.Order.UserId).Update("status", common.UserStatusDisabled).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	getCalls, postCalls := 0, 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodGet:
			getCalls++
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT"}`))}, nil
		case http.MethodPost:
			postCalls++
			t.Fatal("disabled user must not repeat the initial charge POST")
		}
		return nil, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.False(t, resolved)
	require.ErrorIs(t, err, model.ErrTossBillingUserInactive)
	require.Equal(t, 1, getCalls)
	require.Zero(t, postCalls)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.NotEmpty(t, order.BillingClaimToken)
}

func TestConfirmTossBillingChargeClassifiesSuccessfulHTTPPaymentStates(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
	tests := []struct {
		status        string
		balanceAmount int64
		cancelAmount  int64
		wantErr       error
	}{
		{status: "READY", wantErr: model.ErrTossBillingChargePending},
		{status: "IN_PROGRESS", wantErr: model.ErrTossBillingChargePending},
		{status: "WAITING_FOR_DEPOSIT", wantErr: model.ErrTossBillingChargePending},
		{status: "ABORTED", wantErr: model.ErrTossBillingChargeTerminal},
		{status: "EXPIRED", wantErr: model.ErrTossBillingChargeTerminal},
		{status: "CANCELED", cancelAmount: 15000, wantErr: model.ErrTossBillingChargeCanceled},
		{status: "PARTIAL_CANCELED", balanceAmount: 5000, cancelAmount: 10000, wantErr: model.ErrTossBillingChargeCanceled},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			originalBase := tossAPIBase
			originalClient := http.DefaultClient
			t.Cleanup(func() {
				tossAPIBase = originalBase
				http.DefaultClient = originalClient
			})
			orderID := "toss_state_" + strings.ToLower(tc.status)
			body := `{"paymentKey":"pay_` + strings.ToLower(tc.status) + `","type":"BILLING","orderId":"` + orderID + `","status":"` + tc.status + `","totalAmount":15000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`
			if tc.balanceAmount > 0 || tc.cancelAmount > 0 {
				body = `{"paymentKey":"pay_` + strings.ToLower(tc.status) + `","type":"BILLING","orderId":"` + orderID + `","status":"` + tc.status + `","totalAmount":15000,"balanceAmount":` +
					strconv.FormatInt(tc.balanceAmount, 10) + `,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"},"cancels":[{"cancelAmount":` + strconv.FormatInt(tc.cancelAmount, 10) + `,"cancelStatus":"DONE","transactionKey":"tx_` + strings.ToLower(tc.status) + `"}]}`
			}
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, r.Method)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			tossAPIBase = "https://api.test.tosspayments.local"
			if tc.cancelAmount > 0 {
				require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
					TradeNo: orderID, PaymentMethod: model.PaymentMethodToss,
					PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
				}).Error)
			}

			result, err := confirmTossBillingChargeOrLookupWithSecret(context.Background(), "billing_states", "cust_states", "sk_states", orderID, "states", 15000)
			require.NotNil(t, result)
			require.ErrorIs(t, err, tc.wantErr)
			if tc.cancelAmount > 0 {
				var events int64
				require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).Count(&events).Error)
				require.Equal(t, int64(1), events)
			}
		})
	}
}

func TestQueueIssuedTossBillingKeyUsesOrderMIDSnapshotAfterRotation(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}, &model.User{}))
	const (
		tradeNo   = "toss_sub_cleanup_old_mid"
		oldMID    = "ck_cleanup_old_mid"
		oldSecret = "sk_cleanup_old_mid"
	)
	oldHash := model.TossBillingClientKeyFingerprint(oldMID)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 95, TradeNo: tradeNo, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
		ProviderClientKeyHash: oldHash,
	}).Error)
	originalClient := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	t.Cleanup(func() {
		setting.TossBillingClientKey = originalClient
		setting.TossBillingSecretKey = originalSecret
	})
	setting.TossBillingClientKey = "ck_cleanup_new_mid"
	setting.TossBillingSecretKey = "sk_cleanup_new_mid"
	issued := &tossBillingIssueResponse{BillingKey: "billing_cleanup_old_mid", CustomerKey: "cust_cleanup_old_mid"}
	issued.Card.Company = "현대"
	issued.Card.Number = "433012******1234"

	keyID := queueIssuedTossBillingKeyForRevocation(context.Background(), 95, tradeNo, issued.CustomerKey, issued, oldSecret, "test_rotation")
	require.Greater(t, keyID, 0)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
	require.Equal(t, oldHash, key.ProviderClientKeyHash)
	storedSecret, err := model.DecryptProviderCredential(key.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, oldSecret, storedSecret)
}

func TestQueueIssuedTossBillingKeyUsesRequestCustomerKeyWhenProviderEchoExceedsStorage(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}, &model.User{}))
	const (
		userID      = 951
		tradeNo     = "toss_sub_cleanup_long_customer_echo"
		customerKey = "cust_cleanup_canonical"
	)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: userID, TradeNo: tradeNo, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	issued := &tossBillingIssueResponse{
		BillingKey:  "billing_cleanup_long_customer_echo",
		CustomerKey: strings.Repeat("x", 300),
	}
	issued.Card.Company = "현대"
	issued.Card.Number = "433012******1234"

	keyID := queueIssuedTossBillingKeyForRevocation(
		context.Background(), userID, tradeNo, customerKey, issued, "sk_cleanup_long_echo", "test_long_echo",
	)
	require.Greater(t, keyID, 0)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, customerKey, key.CustomerKey)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
}

func TestProcessClaimedTossSubscriptionIssueKillSwitchKeepsSnapshotWithoutProviderCall(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.Log{}))
	user := model.User{Id: 96, Username: "issue-kill-switch", Status: common.UserStatusEnabled, TossCustomerKey: "cust_issue_kill_switch"}
	require.NoError(t, model.DB.Create(&user).Error)
	plan := model.SubscriptionPlan{Id: 4960, Title: "Issue kill switch", PriceAmount: 10, Currency: "USD", DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential("sk_issue_kill_switch")
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: "toss_sub_issue_kill_switch",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
		CreateTime:     common.GetTimestamp(),
		ProviderAmount: 15000, ProviderCurrency: "KRW", ProviderCredential: credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("ck_issue_kill_switch"),
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	claimToken, claimed, err := model.ClaimTossSubscriptionBillingIssue(order.TradeNo, "auth_issue_kill_switch", user.TossCustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	authKey, customerKey, secretKey, err := model.GetClaimedTossSubscriptionBillingIssue(order.TradeNo, claimToken)
	require.NoError(t, err)
	originalIssuer := subscriptionTossBillingKeyIssuer
	issuerCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		return nil, 0, nil
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })
	setting.TossBillingEnabled = false

	resolved, retainClaim, err := processClaimedTossSubscriptionBillingIssue(context.Background(), &order, &plan, claimToken, authKey, customerKey, secretKey)
	require.False(t, resolved)
	require.True(t, retainClaim)
	require.ErrorIs(t, err, model.ErrTossBillingOperationallyDisabled)
	require.Zero(t, issuerCalls)
	var stored model.SubscriptionOrder
	require.NoError(t, model.DB.First(&stored, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Zero(t, stored.BillingKeyId)
	require.Equal(t, claimToken, stored.BillingClaimToken)
	require.NotEmpty(t, stored.BillingIssueAuthKey)
	require.False(t, stored.BillingIssueAttempted)
}

func TestTossWebhookUsesExactAttemptCredentialForClosedDonePayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 97, Username: "closed-webhook", Status: common.UserStatusEnabled}).Error)
	credential, err := model.EncryptProviderCredential("sk_closed_subscription_original")
	require.NoError(t, err)
	attemptCredential, err := model.EncryptProviderCredential("sk_closed_subscription_attempt")
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: 97, PlanId: 4970, Money: 10, TradeNo: "toss_sub_closed_done",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusExpired, ProviderAmount: 15000, ProviderCurrency: "KRW",
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("ck_closed_subscription"),
		BillingAttempted: true, BillingAttemptCredential: attemptCredential,
	}
	require.NoError(t, model.DB.Create(&order).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	wantAuthorization := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_closed_subscription_attempt:"))
	lookupCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		lookupCalls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/payments/pay_closed_subscription", r.URL.EscapedPath())
		require.Equal(t, wantAuthorization, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"paymentKey":"pay_closed_subscription","type":"BILLING","orderId":"toss_sub_closed_done","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_closed_done","paymentKey":"pay_closed_subscription","status":"DONE"}}`))

	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, lookupCalls)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	var subscriptions int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", order.UserId).Count(&subscriptions).Error)
	require.Zero(t, subscriptions)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", order.TradeNo, model.TossPaymentEventTypeFulfillment).First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
	require.Equal(t, "pay_closed_subscription", event.PaymentKey)
}

func seedAttemptedTossSubscriptionIssueForCleanup(t *testing.T, suffix string, status string) model.SubscriptionOrder {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.Log{}))
	user := model.User{Id: 980, Username: "issue-cleanup-" + suffix, Status: common.UserStatusEnabled, Group: "default", TossCustomerKey: "cust_issue_cleanup_" + suffix}
	require.NoError(t, model.DB.Create(&user).Error)
	plan := model.SubscriptionPlan{Id: 4980, Title: "Issue cleanup", PriceAmount: 10, Currency: "USD", DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 86400, Enabled: true}
	require.NoError(t, model.DB.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential("sk_issue_cleanup_" + suffix)
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: "toss_sub_issue_cleanup_" + suffix,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
		CreateTime: common.GetTimestamp() - 600, ProviderAmount: 15000, ProviderCurrency: "KRW",
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("ck_issue_cleanup_" + suffix),
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)
	claimToken, claimed, err := model.ClaimTossSubscriptionBillingIssue(order.TradeNo, "auth_issue_cleanup_"+suffix, user.TossCustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkTossSubscriptionBillingIssueAttempt(order.TradeNo, claimToken))
	require.NoError(t, model.ReleaseTossSubscriptionBillingClaim(order.TradeNo, claimToken))
	if status != common.TopUpStatusPending {
		require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("id = ?", order.Id).Updates(map[string]interface{}{
			"status": status, "complete_time": common.GetTimestamp(),
		}).Error)
	}
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	return order
}

func installTossSubscriptionIssueCleanupStubs(t *testing.T, order model.SubscriptionOrder, reason string) (issueCalls *int, cleanupCalls *int) {
	t.Helper()
	originalIssuer := subscriptionTossBillingKeyIssuer
	originalCleanup := subscriptionTossIssuedBillingKeyCleanup
	issuedCount := 0
	cleanedCount := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuedCount++
		require.Equal(t, "auth_issue_cleanup_"+strings.TrimPrefix(order.TradeNo, "toss_sub_issue_cleanup_"), authKey)
		require.Equal(t, order.TradeNo, idempotencyKey)
		issued := &tossBillingIssueResponse{BillingKey: "billing_cleanup_only", CustomerKey: customerKey}
		issued.Card.Company = "card"
		issued.Card.Number = "****1234"
		return issued, http.StatusOK, nil
	}
	subscriptionTossIssuedBillingKeyCleanup = func(ctx context.Context, userID int, tradeNo, customerKey string, issued *tossBillingIssueResponse, secretKey, gotReason string) error {
		cleanedCount++
		require.Equal(t, order.UserId, userID)
		require.Equal(t, order.TradeNo, tradeNo)
		require.Equal(t, "billing_cleanup_only", issued.BillingKey)
		require.Equal(t, reason, gotReason)
		return nil
	}
	t.Cleanup(func() {
		subscriptionTossBillingKeyIssuer = originalIssuer
		subscriptionTossIssuedBillingKeyCleanup = originalCleanup
	})
	return &issuedCount, &cleanedCount
}

func TestDisabledBillingRecoversUncertainSubscriptionIssueForCleanupWithoutCharge(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "disabled", common.TopUpStatusPending)
	issueCalls, cleanupCalls := installTossSubscriptionIssueCleanupStubs(t, order, "billing_disabled_cleanup")
	setting.TossBillingEnabled = false

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, *issueCalls)
	require.Equal(t, 1, *cleanupCalls)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Empty(t, order.BillingIssueAuthKey)
	require.False(t, order.BillingIssueAttempted)
	require.Empty(t, order.BillingClaimToken)
}

func TestExpiredSubscriptionOrderRecoversUncertainIssueForCleanup(t *testing.T) {
	order := seedAttemptedTossSubscriptionIssueForCleanup(t, "expired", common.TopUpStatusExpired)
	issueCalls, cleanupCalls := installTossSubscriptionIssueCleanupStubs(t, order, "terminal_order_cleanup")

	resolved, err := model.ReconcileStaleTossPendingSubscriptionOrders(context.Background(), common.GetTimestamp()-300, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	require.Equal(t, 1, *issueCalls)
	require.Equal(t, 1, *cleanupCalls)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Empty(t, order.BillingIssueAuthKey)
	require.False(t, order.BillingIssueAttempted)
}
