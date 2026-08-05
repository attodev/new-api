package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func performTossWebhookForTest(body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", strings.NewReader(body))
	TossWebhook(c)
	return recorder
}

func TestTossWebhookAppliesOneWholeHandlerDeadline(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", strings.NewReader(`{"eventType":"IGNORED"}`))
	started := time.Now()

	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	deadline, ok := c.Request.Context().Deadline()
	require.True(t, ok, "webhook request context must carry the provider response deadline")
	require.Greater(t, deadline.Sub(started), 7*time.Second)
	require.LessOrEqual(t, deadline.Sub(started), tossWebhookProcessingTimeout+100*time.Millisecond)
}

func TestTossWebhookReturns503WhenOrderLockExceedsDeadline(t *testing.T) {
	const orderID = "toss_webhook_lock_timeout"
	LockOrder(orderID)
	lockHeld := true
	t.Cleanup(func() {
		if lockHeld {
			UnlockOrder(orderID)
		}
	})

	requestCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		strings.NewReader(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_webhook_lock_timeout","status":"DONE"}}`),
	).WithContext(requestCtx)

	started := time.Now()
	TossWebhook(c)
	elapsed := time.Since(started)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Less(t, elapsed, time.Second, "webhook must not wait indefinitely on the in-process order lock")

	UnlockOrder(orderID)
	lockHeld = false
	_, exists := orderLocks.Load(orderID)
	require.False(t, exists, "a canceled lock waiter must release its reference")

	reacquireCtx, reacquireCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer reacquireCancel()
	require.True(t, LockOrderWithContext(reacquireCtx, orderID), "the order lock must remain reusable")
	UnlockOrder(orderID)
	_, exists = orderLocks.Load(orderID)
	require.False(t, exists, "the reusable lock must also clean up its map entry")
}

func TestTossWebhookDatabaseLookupHonorsCanceledContextWhileLocked(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL

	dsn := filepath.Join(t.TempDir(), "toss-webhook-context.db") + "?_pragma=busy_timeout(10000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))

	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		_ = sqlDB.Close()
	})

	lockConn, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	defer lockConn.Close()
	_, err = lockConn.ExecContext(context.Background(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)
	locked := true
	defer func() {
		if locked {
			_, _ = lockConn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	requestCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		strings.NewReader(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_db_context_lock","paymentKey":"pay_db_context_lock","status":"DONE"}}`),
	).WithContext(requestCtx)

	started := time.Now()
	TossWebhook(c)
	elapsed := time.Since(started)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Less(t, elapsed, time.Second, "database lock wait must stop when the webhook context is canceled")
	require.Error(t, c.Request.Context().Err(), "handler cleanup must cancel its deadline context")

	_, err = lockConn.ExecContext(context.Background(), "ROLLBACK")
	require.NoError(t, err)
	locked = false
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error, "database remains usable after the canceled query")
}

func TestTossWebhookWaitsForOrderLockBeforeReservingSQLiteConnection(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL

	dsn := filepath.Join(t.TempDir(), "toss-webhook-lock-connection.db") + "?_pragma=busy_timeout(10000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))

	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		_ = sqlDB.Close()
	})

	const orderID = "toss_lock_before_db_connection"
	LockOrder(orderID)
	lockHeld := true
	t.Cleanup(func() {
		if lockHeld {
			UnlockOrder(orderID)
		}
	})

	requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		strings.NewReader(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_lock_before_db_connection","paymentKey":"pay_lock_before_db_connection","status":"DONE"}}`),
	).WithContext(requestCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		TossWebhook(c)
	}()

	require.Eventually(t, func() bool {
		createLock.Lock()
		defer createLock.Unlock()
		value, ok := orderLocks.Load(orderID)
		return ok && value.(*refCountedMutex).refCount >= 2
	}, time.Second, 5*time.Millisecond, "webhook must be waiting on the existing order lock")
	require.Zero(t, sqlDB.Stats().InUse, "an order-lock waiter must not reserve a SQLite connection")

	UnlockOrder(orderID)
	lockHeld = false
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("webhook did not finish after the order lock was released")
	}
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestGetTossPayMoney(t *testing.T) {
	// KRW mode keeps the entered KRW amount as the card charge before ratios.
	prev := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	defer func() { setting.TossUnitPrice = prev }()

	got := getTossPayMoney(10, "default")
	if got != 10 {
		t.Fatalf("getTossPayMoney(10) = %d want 10", got)
	}
}

func TestTossRedirectDoesNotCacheOrLeakCallbackReferrer(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey=pay_sensitive", nil)

	tossRedirect(c, "/console/log")

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, "/console/log", recorder.Header().Get("Location"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
}

func TestTossAPIErrorDoesNotExposeProviderBodyOrMessage(t *testing.T) {
	err := newTossAPIError(http.MethodPost, http.StatusBadRequest, []byte(`{
		"code":"INVALID_REQUEST",
		"message":"customerKey=cust_sensitive paymentKey=pay_sensitive",
		"billingKey":"billing_sensitive"
	}`))
	logged := err.Error()
	require.Contains(t, logged, "code=INVALID_REQUEST")
	require.NotContains(t, logged, "cust_sensitive")
	require.NotContains(t, logged, "pay_sensitive")
	require.NotContains(t, logged, "billing_sensitive")

	malformed := newTossAPIError(http.MethodPost, http.StatusBadRequest, []byte(`{
		"code":"PAYMENT_KEY_pay_sensitive\nforged-log-line"
	}`))
	require.Contains(t, malformed.Error(), "code=UNKNOWN")
	require.NotContains(t, malformed.Error(), "pay_sensitive")
	require.NotContains(t, malformed.Error(), "forged-log-line")

	nested := newTossAPIError(http.MethodPost, http.StatusUnauthorized, []byte(`{
		"version":"2022-11-16",
		"traceId":"trace-sensitive",
		"error":{"code":"UNAUTHORIZED_KEY","message":"billingKey=billing_nested_sensitive"}
	}`))
	require.Contains(t, nested.Error(), "code=UNAUTHORIZED_KEY")
	require.NotContains(t, nested.Error(), "trace-sensitive")
	require.NotContains(t, nested.Error(), "billing_nested_sensitive")
}

func TestTossTransportErrorDoesNotExposeSensitiveResourceURL(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial failed for %s", r.URL.String())
	})}

	_, _, err := doTossAPIRequestWithSecret(
		context.Background(),
		http.MethodGet,
		"https://api.test.tosspayments.local/v1/payments/pay_sensitive_transport_key",
		nil,
		"",
		"sk_sensitive_transport_secret",
		http.StatusOK,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "method=GET")
	require.NotContains(t, err.Error(), "pay_sensitive_transport_key")
	require.NotContains(t, err.Error(), "sk_sensitive_transport_secret")
}

func TestGetTossPayMoneyAppliesDiscount(t *testing.T) {
	prevUnit := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	defer func() { setting.TossUnitPrice = prevUnit }()

	ps := operation_setting.GetPaymentSetting()
	prev := ps.AmountDiscount
	ps.AmountDiscount = map[int]float64{10: 0.9}
	defer func() { ps.AmountDiscount = prev }()

	got := getTossPayMoney(10, "default")
	if got != 9 {
		t.Fatalf("getTossPayMoney with discount = %d want 9", got)
	}
}

func TestPersistTossFulfillmentEventIsIdempotent(t *testing.T) {
	setupTossBillingControllerTestDB(t)

	auth := &tossConfirmResponse{
		PaymentKey:    "pay_fulfillment_1",
		OrderId:       "toss_fulfillment_order_1",
		Status:        "DONE",
		TotalAmount:   13000,
		BalanceAmount: 13000,
	}
	created, err := persistTossFulfillmentEvent(auth)
	require.NoError(t, err)
	require.True(t, created)
	created, err = persistTossFulfillmentEvent(auth)
	require.NoError(t, err)
	require.False(t, created)

	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Find(&events).Error)
	require.Len(t, events, 1)
	require.Equal(t, model.TossPaymentEventTypeFulfillment, events[0].EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, events[0].ReconciliationStatus)
	require.Equal(t, int64(13000), events[0].OriginalAmount)
}

func TestPersistTossFinancialMismatchIsDistinctFromFulfillment(t *testing.T) {
	setupTossBillingControllerTestDB(t)

	auth := &tossConfirmResponse{
		PaymentKey:    "pay_financial_mismatch_1",
		OrderId:       "toss_financial_mismatch_order_1",
		Status:        "DONE",
		TotalAmount:   12000,
		BalanceAmount: 12000,
	}
	created, err := persistTossFinancialMismatchEvent(auth, "amount mismatch")
	require.NoError(t, err)
	require.True(t, created)
	created, err = persistTossFinancialMismatchEvent(auth, "redelivery")
	require.NoError(t, err)
	require.False(t, created)
	created, err = persistTossFulfillmentEvent(auth)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, model.ResolveTossPaymentEvents(auth.OrderId, model.TossPaymentEventTypeFulfillment))

	var mismatch model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", auth.OrderId, model.TossPaymentEventTypeFinancialMismatch).First(&mismatch).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, mismatch.ReconciliationStatus)
	require.Equal(t, "amount mismatch", mismatch.ResolutionNote)
	var fulfillment model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", auth.OrderId, model.TossPaymentEventTypeFulfillment).First(&fulfillment).Error)
	require.Equal(t, model.TossReconciliationStatusResolved, fulfillment.ReconciliationStatus)
	require.NotEqual(t, mismatch.EventKey, fulfillment.EventKey)
}

func TestIsValidServerAddress(t *testing.T) {
	cases := map[string]bool{
		"https://pay.example.com": true,
		"http://localhost:3000":   true,
		"http://127.0.0.1:3000":   true,
		"http://pay.example.com":  false,
		"https://u:p@example.com": false,
		"https://example.com?q=1": false,
		"https://example.com/#x":  false,
		"https://example.com/a b": false,
		"":                        false,
		"example.com":             false,
		"ftp://x.com":             false,
		"   ":                     false,
	}
	for addr, want := range cases {
		if got := isValidServerAddress(addr); got != want {
			t.Errorf("isValidServerAddress(%q)=%v want %v", addr, got, want)
		}
	}
}

func TestTossPaymentAvailabilityRequiresPositiveUnitPrice(t *testing.T) {
	originalCryptoSecret := common.CryptoSecret
	t.Setenv("CRYPTO_SECRET", "toss-availability-test-secret-at-least-32-bytes")
	common.CryptoSecret = "toss-availability-test-secret-at-least-32-bytes"
	ps := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := ps.ComplianceConfirmed
	originalComplianceTermsVersion := ps.ComplianceTermsVersion
	originalEnabled := setting.TossEnabled
	originalBillingEnabled := setting.TossBillingEnabled
	originalWalletAutoRechargeEnabled := setting.TossWalletAutoRechargeEnabled
	originalTestMode := setting.TossTestMode
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalUnitPrice := setting.TossUnitPrice
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		common.CryptoSecret = originalCryptoSecret
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossEnabled = originalEnabled
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossWalletAutoRechargeEnabled = originalWalletAutoRechargeEnabled
		setting.TossTestMode = originalTestMode
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
		setting.TossBillingClientKey = originalBillingClientKey
		setting.TossBillingSecretKey = originalBillingSecretKey
		setting.TossUnitPrice = originalUnitPrice
		system_setting.ServerAddress = originalServerAddress
	})

	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossEnabled = true
	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = false
	setting.TossTestMode = false
	setting.TossClientKey = "live_ck_regular"
	setting.TossSecretKey = "live_sk_regular"
	setting.TossBillingClientKey = ""
	setting.TossBillingSecretKey = ""
	system_setting.ServerAddress = "https://example.com"

	setting.TossUnitPrice = 0
	if isTossTopUpEnabled() {
		t.Fatal("top-up should be disabled when TossUnitPrice is not positive")
	}
	if isTossBillingEnabled() {
		t.Fatal("billing should be disabled when TossUnitPrice is not positive")
	}

	setting.TossUnitPrice = 1300
	if !isTossTopUpEnabled() {
		t.Fatal("top-up should be enabled with positive TossUnitPrice and configured keys")
	}
	if isTossBillingEnabled() {
		t.Fatal("billing should require an explicit Toss billing key pair, not fall back to normal Toss keys")
	}

	setting.TossBillingClientKey = "live_ck_billing"
	setting.TossBillingSecretKey = "live_sk_billing"
	if !isTossBillingEnabled() {
		t.Fatal("billing should be enabled with positive TossUnitPrice and configured billing keys")
	}
	if isTossWalletAutoRechargeEnabled() {
		t.Fatal("subscription billing must not implicitly enable wallet auto recharge")
	}
	setting.TossWalletAutoRechargeEnabled = true
	if !isTossWalletAutoRechargeEnabled() {
		t.Fatal("wallet auto recharge should require its explicit contract gate")
	}

	setting.TossClientKey = "live_gck_widget"
	setting.TossSecretKey = "live_gsk_widget"
	if isTossTopUpEnabled() {
		t.Fatal("payment-window integration must reject Toss widget keys")
	}
	setting.TossBillingClientKey = "live_gck_billing_widget"
	setting.TossBillingSecretKey = "live_gsk_billing_widget"
	if isTossBillingEnabled() {
		t.Fatal("automatic billing must reject Toss widget keys")
	}

	setting.TossClientKey = "live_sk_misfiled_secret"
	setting.TossSecretKey = "live_sk_server_secret"
	if isTossTopUpEnabled() {
		t.Fatal("top-up must not expose a secret stored in the client-key field")
	}
	setting.TossBillingClientKey = "live_sk_misfiled_billing_secret"
	setting.TossBillingSecretKey = "live_sk_billing_secret"
	if isTossBillingEnabled() {
		t.Fatal("billing must not expose a secret stored in the client-key field")
	}

	setting.TossClientKey = "live_ck_regular"
	setting.TossSecretKey = "live_sk_regular"
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-toss-topup-secret"
	if isTossTopUpEnabled() {
		t.Fatal("top-up must require a persistent encryption key for restart-safe recovery")
	}
}

func TestTossTopUpRejectsCardAmountBelowTossMinimum(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	ps := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := ps.ComplianceConfirmed
	originalComplianceTermsVersion := ps.ComplianceTermsVersion
	originalEnabled := setting.TossEnabled
	originalTestMode := setting.TossTestMode
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalUnitPrice := setting.TossUnitPrice
	originalMinTopUp := setting.TossMinTopUp
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossEnabled = originalEnabled
		setting.TossTestMode = originalTestMode
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
		setting.TossUnitPrice = originalUnitPrice
		setting.TossMinTopUp = originalMinTopUp
		system_setting.ServerAddress = originalServerAddress
	})

	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.TossEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "live_ck_toss"
	setting.TossSecretKey = "live_sk_toss"
	setting.TossUnitPrice = 50
	setting.TossMinTopUp = 1
	system_setting.ServerAddress = "https://example.com"

	cases := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{name: "amount preview", handler: RequestTossAmount},
		{name: "payment request", handler: RequestTossPay},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", 7)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/user/toss/pay", strings.NewReader(`{"amount":1,"payment_method":"toss"}`))

			tc.handler(c)

			var resp struct {
				Success bool `json:"success"`
			}
			if err := common.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Success {
				t.Fatalf("expected %s to reject below-minimum Toss card amount, got success response %q", tc.name, recorder.Body.String())
			}
		})
	}
}

func TestValidateTossConfirmAmount(t *testing.T) {
	// 저장된 주문 금액과 Toss가 돌려준 금액이 다르면 거부되어야 한다.
	topUp := &model.TopUp{Amount: 13000, PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending}
	if err := validateTossConfirm(topUp, "toss_x", 13000); err != nil {
		t.Fatalf("matching amount should pass, got %v", err)
	}
	if err := validateTossConfirm(topUp, "toss_x", 9999); err == nil {
		t.Fatalf("amount mismatch should fail")
	}
	bad := &model.TopUp{Amount: 13000, PaymentProvider: model.PaymentProviderPayPal, Status: common.TopUpStatusPending}
	if err := validateTossConfirm(bad, "toss_x", 13000); err == nil {
		t.Fatalf("provider mismatch should fail")
	}
	wrongMethod := &model.TopUp{Amount: 13000, PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending}
	if err := validateTossConfirm(wrongMethod, "toss_x", 13000); err == nil {
		t.Fatalf("payment method mismatch should fail")
	}
}

func TestTossFailRedirectPreservesFailureContext(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.TopUp{}); err != nil {
		t.Fatalf("migrate topup table: %v", err)
	}
	require.NoError(t, model.DB.Create(&model.TopUp{
		TradeNo:         "toss_fail_order",
		ProviderOrderId: "toss_fail_order",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/fail?orderId=toss_fail_order&code=PAY_PROCESS_CANCELED&message=%EA%B2%B0%EC%A0%9C%20%EC%B7%A8%EC%86%8C", nil)

	TossFail(c)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusFound)
	}
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "/console/topup?") {
		t.Fatalf("redirect location = %q want /console/topup with query", location)
	}
	if !strings.Contains(location, "toss_error_code=PAY_PROCESS_CANCELED") {
		t.Fatalf("redirect location should preserve Toss failure code, got %q", location)
	}
	if !strings.Contains(location, "toss_order_id=toss_fail_order") {
		t.Fatalf("redirect location should preserve Toss order id, got %q", location)
	}
	if !strings.Contains(location, "toss_error_message=") {
		t.Fatalf("redirect location should preserve Toss failure message, got %q", location)
	}
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", "toss_fail_order").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status, "browser fail redirect must not authoritatively close an order")
}

func TestIsTossTerminalFailStatus(t *testing.T) {
	cases := map[string]bool{
		"EXPIRED":          true,
		"ABORTED":          true,
		"CANCELED":         false,
		"PARTIAL_CANCELED": false,
		"DONE":             false,
		"READY":            false,
		"":                 false,
	}
	for status, want := range cases {
		if got := isTossTerminalFailStatus(status); got != want {
			t.Errorf("isTossTerminalFailStatus(%q)=%v want %v", status, got, want)
		}
	}
}

func TestIsTossCancelStatus(t *testing.T) {
	cases := map[string]bool{
		"CANCELED":         true,
		"PARTIAL_CANCELED": true,
		"DONE":             false,
		"EXPIRED":          false,
		"ABORTED":          false,
		"READY":            false,
		"":                 false,
	}
	for status, want := range cases {
		if got := isTossCancelStatus(status); got != want {
			t.Errorf("isTossCancelStatus(%q)=%v want %v", status, got, want)
		}
	}
}

func TestShouldRetryTossCancellationWebhookOnlyForMatchedLaggingPayment(t *testing.T) {
	auth := &tossConfirmResponse{PaymentKey: "pay_cancel_lag", OrderId: "order_cancel_lag", Status: "DONE"}
	require.True(t, shouldRetryTossCancellationWebhook("CANCELED", auth, "order_cancel_lag", "pay_cancel_lag"))

	auth.Status = "PARTIAL_CANCELED"
	require.True(t, shouldRetryTossCancellationWebhook("CANCELED", auth, "order_cancel_lag", "pay_cancel_lag"))
	require.False(t, shouldRetryTossCancellationWebhook("PARTIAL_CANCELED", auth, "order_cancel_lag", "pay_cancel_lag"))
	auth.Status = "CANCELED"
	require.False(t, shouldRetryTossCancellationWebhook("PARTIAL_CANCELED", auth, "order_cancel_lag", "pay_cancel_lag"))
	auth.Status = "DONE"
	require.False(t, shouldRetryTossCancellationWebhook("DONE", auth, "order_cancel_lag", "pay_cancel_lag"))
	require.False(t, shouldRetryTossCancellationWebhook("CANCELED", auth, "unrelated_order", "pay_cancel_lag"))
	require.False(t, shouldRetryTossCancellationWebhook("CANCELED", auth, "order_cancel_lag", "unrelated_payment"))
}

func TestShouldRetryTossDoneWebhookOnlyForMatchedPreDonePayment(t *testing.T) {
	auth := &tossConfirmResponse{PaymentKey: "pay_done_lag", OrderId: "order_done_lag", Status: "READY"}
	require.True(t, shouldRetryTossDoneWebhook("DONE", auth, "order_done_lag", "pay_done_lag"))

	auth.Status = "IN_PROGRESS"
	require.True(t, shouldRetryTossDoneWebhook("DONE", auth, "order_done_lag", "pay_done_lag"))
	for _, status := range []string{"DONE", "WAITING_FOR_DEPOSIT", "CANCELED", "PARTIAL_CANCELED", "ABORTED", "EXPIRED"} {
		auth.Status = status
		require.False(t, shouldRetryTossDoneWebhook("DONE", auth, "order_done_lag", "pay_done_lag"), status)
	}
	auth.Status = "IN_PROGRESS"
	require.False(t, shouldRetryTossDoneWebhook("CANCELED", auth, "order_done_lag", "pay_done_lag"))
	require.False(t, shouldRetryTossDoneWebhook("DONE", auth, "unrelated_order", "pay_done_lag"))
	require.False(t, shouldRetryTossDoneWebhook("DONE", auth, "order_done_lag", "unrelated_payment"))
}

func TestIsTossPaymentWebhookEventType(t *testing.T) {
	cases := map[string]bool{
		"PAYMENT_STATUS_CHANGED": true,
		"CANCEL_STATUS_CHANGED":  false,
		"BILLING_DELETED":        false,
		"DEPOSIT_CALLBACK":       false,
		"":                       false,
	}
	for eventType, want := range cases {
		if got := isTossPaymentWebhookEventType(eventType); got != want {
			t.Errorf("isTossPaymentWebhookEventType(%q)=%v want %v", eventType, got, want)
		}
	}
}

func TestGetTossPaymentEscapesPaymentKey(t *testing.T) {
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s want GET", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/v1/payments/payment%2Flookup%3Fkey" {
			t.Fatalf("escaped path = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"payment/lookup?key","type":"NORMAL","orderId":"toss_order","status":"DONE","totalAmount":13000,"currency":"KRW"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	payment, status, err := getTossPayment(context.Background(), "payment/lookup?key")
	if err != nil {
		t.Fatalf("getTossPayment error = %v", err)
	}
	if status != http.StatusOK || payment.PaymentKey != "payment/lookup?key" {
		t.Fatalf("status=%d payment=%#v", status, payment)
	}
}

func TestTossConfirmUsesStoredProviderCredential(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := model.EncryptProviderCredential("sk_old_toss")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	if err := model.DB.Create(&model.TopUp{
		UserId:             7,
		Amount:             13000,
		Money:              10,
		TradeNo:            "toss_stored_secret",
		ProviderOrderId:    "toss_stored_secret",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		CreateTime:         model.GetDBTimestamp(),
		Status:             common.TopUpStatusPending,
	}).Error; err != nil {
		t.Fatalf("create topup: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
		common.QuotaPerUnit = originalQuotaPerUnit
	})

	setting.TossSecretKey = "sk_new_toss"
	setting.TossTestMode = false
	common.QuotaPerUnit = 1

	var usedStoredSecret bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/payments/confirm" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_old_toss:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want stored credential auth", got)
		}
		usedStoredSecret = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stored_secret","type":"NORMAL","orderId":"toss_stored_secret","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"현대","number":"433012******1234","amount":13000}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey=pay_stored_secret&orderId=toss_stored_secret&amount=13000", nil)

	TossConfirm(c)

	if recorder.Code != http.StatusFound {
		t.Fatalf("confirm status = %d want 302", recorder.Code)
	}
	if !usedStoredSecret {
		t.Fatal("expected Toss confirm to call API with stored provider credential")
	}
	var topUp model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_stored_secret").First(&topUp).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if topUp.Status != common.TopUpStatusSuccess {
		t.Fatalf("topup status = %q want success", topUp.Status)
	}
}

func TestTossConfirmUsesRotatedSecretForLookupOnlyAfterStoredSecretExpires(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 8, Username: "toss-rotated-user", Group: "default"}).Error)
	credential, err := model.EncryptProviderCredential("sk_expired_toss")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                8,
		Amount:                13000,
		Money:                 10,
		TradeNo:               "toss_rotated_secret",
		ProviderOrderId:       "toss_rotated_secret",
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint("ck_current_toss"),
		CreateTime:            model.GetDBTimestamp(),
		Status:                common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalClientKey := setting.TossClientKey
	originalSecret := setting.TossSecretKey
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecret
		common.QuotaPerUnit = originalQuotaPerUnit
	})
	setting.TossClientKey = "ck_current_toss"
	setting.TossSecretKey = "sk_current_toss"
	common.QuotaPerUnit = 1

	var exactPosts, activePosts, exactGets, activeGets int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		storedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_expired_toss:"))
		if r.Header.Get("Authorization") == storedAuth {
			if r.Method == http.MethodPost {
				exactPosts++
			} else {
				require.Equal(t, http.MethodGet, r.Method)
				exactGets++
			}
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`)),
			}, nil
		}
		currentAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_current_toss:"))
		require.Equal(t, currentAuth, r.Header.Get("Authorization"))
		if r.Method == http.MethodPost {
			activePosts++
		}
		require.Equal(t, http.MethodGet, r.Method, "a rotated API key is lookup-only because Toss idempotency is API-key scoped")
		activeGets++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_rotated_secret","type":"NORMAL","orderId":"toss_rotated_secret","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"현대","number":"433012******1234","amount":13000}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey=pay_rotated_secret&orderId=toss_rotated_secret&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Equal(t, 1, exactPosts)
	require.Equal(t, 1, exactGets)
	require.Equal(t, 1, activeGets)
	require.Zero(t, activePosts)
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", "toss_rotated_secret").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	attemptSecret, err := model.DecryptProviderCredential(topUp.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "sk_expired_toss", attemptSecret, "lookup fallback must never replace the exact provider POST credential")
}

func TestLookupWalletAutoRechargeFallsBackOnToss400CredentialCode(t *testing.T) {
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	calls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		oldAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_wallet_old:"))
		if r.Header.Get("Authorization") == oldAuth {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"INVALID_API_KEY","message":"rotated"}`)),
			}, nil
		}
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("sk_wallet_new:")), r.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_wallet_lookup","type":"BILLING","orderId":"wallet_auto_77_lookup","status":"DONE","totalAmount":10000,"balanceAmount":10000,"currency":"KRW","method":"카드","card":{"amount":10000,"company":"card","number":"****7777"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, err := lookupWalletAutoRechargeTossPayment(context.Background(), []string{"sk_wallet_old", "sk_wallet_new"}, "wallet_auto_77_lookup", 10000)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "DONE", result.ProviderStatus)
	require.Equal(t, 2, calls)
}

func TestTossConfirmLeavesOrderPendingWhenIdempotentRequestIsStillProcessing(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 9, Username: "toss-pending-user", Group: "default"}).Error)
	credential, err := model.EncryptProviderCredential("sk_pending_toss")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:             9,
		Amount:             13000,
		Money:              10,
		TradeNo:            "toss_processing_order",
		ProviderOrderId:    "toss_processing_order",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		CreateTime:         time.Now().Unix(),
		Status:             common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"IDEMPOTENT_REQUEST_PROCESSING","message":"processing"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not visible yet"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey=pay_processing&orderId=toss_processing_order&amount=13000", nil)
	TossConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", "toss_processing_order").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.Equal(t, "pay_processing", topUp.ProviderOrderId)
}

func TestPersistTossCancellationEventsUsesActualPartialCancelAmount(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}, &model.TopUp{}, &model.SubscriptionOrder{}))
	require.NoError(t, model.DB.Create(&model.TopUp{
		TradeNo: "toss_partial_event", PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	auth := &tossConfirmResponse{
		PaymentKey:    "pay_partial_event",
		OrderId:       "toss_partial_event",
		Status:        "PARTIAL_CANCELED",
		TotalAmount:   1000,
		BalanceAmount: 700,
		Cancels: []tossPaymentCancel{{
			CancelAmount:   300,
			TransactionKey: "cancel_transaction_1",
			CancelStatus:   "DONE",
		}},
	}

	created, amount, err := persistTossCancellationEvents(auth)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(300), amount)
	created, amount, err = persistTossCancellationEvents(auth)
	require.NoError(t, err)
	require.Zero(t, created)
	require.Zero(t, amount)
	auth.BalanceAmount = 400
	auth.Cancels = append(auth.Cancels, tossPaymentCancel{
		CancelAmount:   300,
		TransactionKey: "cancel_transaction_2",
		CancelStatus:   "DONE",
	})
	created, amount, err = persistTossCancellationEvents(auth)
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Equal(t, int64(300), amount)

	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Order("transaction_key asc").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, int64(300), events[0].CancelAmount)
	require.Equal(t, int64(700), events[0].BalanceAmount)
	require.Equal(t, "cancel_transaction_1", events[0].TransactionKey)
}

func TestTossWebhookReturnsRetryableStatusOnDatabaseFailure(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_db_unavailable","paymentKey":"pay_db_unavailable","status":"DONE"}}`),
	)
	TossWebhook(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestTossWebhookRejectsOversizedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		strings.NewReader(strings.Repeat("x", int(tossWebhookMaxBodyBytes)+1)),
	)
	TossWebhook(c)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestTossWebhookRequestsRetryForMissingBillingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", strings.NewReader(`{"eventType":"BILLING_DELETED"}`))
	c.Request.RemoteAddr = "13.124.18.147:443"

	TossWebhook(c)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestTossWebhookRejectsNestedOnlyBillingDeletedKey(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	keyID, err := model.StoreTossBillingKey(7, "cust_nested_delete", "billing_nested_delete", "card", "****1234")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		strings.NewReader(`{"eventType":"BILLING_DELETED","data":{"billingKey":"billing_nested_delete"}}`),
	)
	c.Request.RemoteAddr = "13.124.18.147:443"

	TossWebhook(c)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	var stored model.UserBillingKey
	require.NoError(t, model.DB.First(&stored, keyID).Error)
	require.Equal(t, model.BillingKeyStatusActive, stored.Status)
}

func TestTossConfirmRejectsDonePaymentWithoutCard(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := model.DB.Create(&model.TopUp{
		UserId:          7,
		Amount:          13000,
		Money:           10,
		TradeNo:         "toss_no_card",
		ProviderOrderId: "toss_no_card",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      model.GetDBTimestamp(),
		Status:          common.TopUpStatusPending,
	}).Error; err != nil {
		t.Fatalf("create topup: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
		common.QuotaPerUnit = originalQuotaPerUnit
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false
	common.QuotaPerUnit = 1

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/payments/confirm" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_no_card","type":"NORMAL","orderId":"toss_no_card","status":"DONE","totalAmount":13000,"currency":"KRW","method":"카드"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/toss/confirm?paymentKey=pay_no_card&orderId=toss_no_card&amount=13000", nil)

	TossConfirm(c)

	if recorder.Code != http.StatusFound {
		t.Fatalf("confirm status = %d want 302", recorder.Code)
	}
	var topUp model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_no_card").First(&topUp).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if topUp.Status != common.TopUpStatusPending {
		t.Fatalf("topup status = %q want pending", topUp.Status)
	}
	var user model.User
	if err := model.DB.First(&user, 7).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.Quota != 0 {
		t.Fatalf("user quota = %d want 0", user.Quota)
	}
}

func TestReconcileTossRecordedTopUpExpiresStaleReadyPayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := model.EncryptProviderCredential("live_sk_test_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	topUp := model.TopUp{
		UserId:             7,
		Amount:             13000,
		Money:              10,
		TradeNo:            "toss_stale_ready",
		ProviderOrderId:    "pay_stale_ready",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		CreateTime:         1,
		Status:             common.TopUpStatusPending,
	}
	if err := model.DB.Create(&topUp).Error; err != nil {
		t.Fatalf("create topup: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_ready" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_ready","type":"NORMAL","orderId":"toss_stale_ready","status":"READY","totalAmount":13000,"currency":"KRW"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	if err != nil {
		t.Fatalf("reconcileTossRecordedTopUp error = %v", err)
	}
	if !resolved {
		t.Fatal("expected stale READY payment to resolve as expired")
	}
	var reloaded model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_stale_ready").First(&reloaded).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if reloaded.Status != common.TopUpStatusExpired {
		t.Fatalf("topup status = %q want expired", reloaded.Status)
	}
}

func TestReconcileTossRecordedTopUpKeepsRecentReadyPaymentPending(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "toss-recent-ready", Group: "default"}).Error)
	credential, err := model.EncryptProviderCredential("sk_recent_ready")
	require.NoError(t, err)
	topUp := model.TopUp{
		UserId:             7,
		Amount:             13000,
		Quota:              100,
		TradeNo:            "toss_recent_ready",
		ProviderOrderId:    "pay_recent_ready",
		ProviderOrderTime:  model.GetDBTimestamp() - 2*60,
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		CreateTime:         model.GetDBTimestamp() - 3*60,
		Status:             common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)

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
	setting.TossClientKey = "ck_recent_ready"
	setting.TossSecretKey = "sk_recent_ready"
	setting.TossTestMode = false
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_recent_ready","type":"NORMAL","orderId":"toss_recent_ready","status":"READY","totalAmount":13000,"currency":"KRW"}`)),
		}, nil
	})}

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	require.NoError(t, err)
	require.False(t, resolved)
	stored, err := model.GetTopUpByTradeNoWithError(topUp.TradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestReconcileTossRecordedTopUpCreditsDonePayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := model.EncryptProviderCredential("live_sk_test_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	topUp := model.TopUp{
		UserId:             7,
		Amount:             13000,
		Money:              10,
		TradeNo:            "toss_stale_done",
		ProviderOrderId:    "pay_stale_done",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		CreateTime:         1,
		Status:             common.TopUpStatusPending,
	}
	if err := model.DB.Create(&topUp).Error; err != nil {
		t.Fatalf("create topup: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
		common.QuotaPerUnit = originalQuotaPerUnit
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false
	common.QuotaPerUnit = 1
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_done" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_done","type":"NORMAL","orderId":"toss_stale_done","status":"DONE","totalAmount":13000,"balanceAmount":13000,"currency":"KRW","method":"카드","card":{"company":"현대","number":"433012******1234","amount":13000}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	if err != nil {
		t.Fatalf("reconcileTossRecordedTopUp error = %v", err)
	}
	if !resolved {
		t.Fatal("expected DONE payment to resolve by crediting")
	}
	var reloaded model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_stale_done").First(&reloaded).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if reloaded.Status != common.TopUpStatusSuccess {
		t.Fatalf("topup status = %q want success", reloaded.Status)
	}
	var user model.User
	if err := model.DB.First(&user, 7).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.Quota != 10 {
		t.Fatalf("user quota = %d want 10", user.Quota)
	}
}

func TestReconcileTossRecordedTopUpQueuesPartialCanceledPendingPayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := model.EncryptProviderCredential("live_sk_test_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	topUp := model.TopUp{
		UserId:             7,
		Amount:             13000,
		Money:              10,
		TradeNo:            "toss_stale_partial_canceled",
		ProviderOrderId:    "pay_stale_partial_canceled",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		CreateTime:         1,
		Status:             common.TopUpStatusPending,
	}
	if err := model.DB.Create(&topUp).Error; err != nil {
		t.Fatalf("create topup: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_partial_canceled" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_partial_canceled","type":"NORMAL","orderId":"toss_stale_partial_canceled","status":"PARTIAL_CANCELED","totalAmount":13000,"balanceAmount":3000,"currency":"KRW","method":"카드","card":{"company":"현대","number":"433012******1234","amount":13000},"cancels":[{"cancelAmount":10000,"refundableAmount":3000,"canceledAt":"2026-07-10T15:00:00+09:00","transactionKey":"cancel_tx_stale_partial","cancelStatus":"DONE"}]}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	if err != nil {
		t.Fatalf("reconcileTossRecordedTopUp error = %v", err)
	}
	if resolved {
		t.Fatal("expected PARTIAL_CANCELED payment with remaining balance to stay in refund recovery")
	}
	var reloaded model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_stale_partial_canceled").First(&reloaded).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if reloaded.Status != model.TossTopUpStatusRefundPending {
		t.Fatalf("topup status = %q want refund_pending", reloaded.Status)
	}
	var refundEvent model.TossPaymentEvent
	if err := model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(topUp.TradeNo, topUp.ProviderOrderId)).First(&refundEvent).Error; err != nil {
		t.Fatalf("load refund requirement: %v", err)
	}
	if refundEvent.ReconciliationStatus != model.TossReconciliationStatusRequired {
		t.Fatalf("refund reconciliation status = %q want required", refundEvent.ReconciliationStatus)
	}
	var count int64
	if err := model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", 7, model.LogTypeTopup).Count(&count).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if count == 0 {
		t.Fatal("expected manual reconciliation log for partial canceled pending payment")
	}
}

func TestTossWebhookRevokesBillingKeyFromTopLevelBillingDeletedPayload(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_deleted_from_toss", "현대", "433012******1234")
	if err != nil {
		t.Fatalf("StoreTossBillingKey error = %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"BILLING_DELETED","createdAt":"2026-06-30T12:00:00.000000","billingKey":"billing_deleted_from_toss","reason":"USER_REMOVED"}`))
	c.Request.RemoteAddr = "13.124.18.147:443"

	TossWebhook(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook status = %d want 200", recorder.Code)
	}
	var key model.UserBillingKey
	if err := model.DB.First(&key, keyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusRevoked {
		t.Fatalf("status = %q want %q", key.Status, model.BillingKeyStatusRevoked)
	}
}

func TestTossWebhookReclassifiesStaleDoneSubscriptionAsCanceled(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription webhook tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	credential, err := model.EncryptProviderCredential("live_sk_test_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	initialOrder := &model.SubscriptionOrder{
		UserId:             7,
		PlanId:             3,
		Money:              15000,
		TradeNo:            "toss_sub_cancel",
		PaymentMethod:      "toss",
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		Status:             common.TopUpStatusSuccess,
	}
	if err := initialOrder.Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "live_sk_test_secret"
	setting.TossTestMode = false

	var lookedUp bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_sub_cancel" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		lookedUp = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_sub_cancel","type":"BILLING","orderId":"toss_sub_cancel","status":"CANCELED","totalAmount":15000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"},"cancels":[{"cancelAmount":15000,"refundableAmount":0,"canceledAt":"2026-07-10T15:00:00+09:00","transactionKey":"cancel_tx_sub_full","cancelStatus":"DONE"}]}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_cancel","paymentKey":"pay_sub_cancel","status":"DONE"}}`))

	TossWebhook(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook status = %d want 200", recorder.Code)
	}
	if !lookedUp {
		t.Fatal("expected canceled subscription order to be rechecked against Toss")
	}
	var count int64
	if err := model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", 7, model.LogTypeTopup).Count(&count).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if count == 0 {
		t.Fatal("expected manual reconciliation log for canceled subscription order")
	}
}

func TestTossSubscriptionCancellationWebhookRetriesUntilAuthoritativeCancelVisible(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}, &model.TossPaymentEvent{}))
	credential, err := model.EncryptProviderCredential("live_sk_cancel_lag")
	require.NoError(t, err)
	require.NoError(t, (&model.SubscriptionOrder{
		UserId:             7,
		PlanId:             3,
		Money:              15000,
		TradeNo:            "toss_sub_cancel_lag",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderCredential: credential,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		Status:             common.TopUpStatusSuccess,
	}).Insert())

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossSecretKey = "live_sk_cancel_lag"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_sub_cancel_lag","type":"BILLING","orderId":"toss_sub_cancel_lag","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`,
			)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	recorder := performTossWebhookForTest(
		`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_cancel_lag","paymentKey":"pay_sub_cancel_lag","status":"CANCELED"}}`,
	)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var cancellationEvents int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ?", "toss_sub_cancel_lag", model.TossPaymentEventTypeCancellation).
		Count(&cancellationEvents).Error)
	require.Zero(t, cancellationEvents)
}

func TestTossWebhookCompletesPendingSubscriptionOnDoneStatus(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription webhook tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Toss Plan",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	model.InvalidateSubscriptionPlanCache(plan.Id)
	keyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_webhook_done", "billing_webhook_done", "현대", "433012******1234", "sk_order_secret")
	if err != nil {
		t.Fatalf("StoreTossBillingKeyWithSecret error = %v", err)
	}
	credential, err := model.EncryptProviderCredential("sk_order_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	initialOrder := &model.SubscriptionOrder{
		UserId:             7,
		PlanId:             3,
		Money:              10,
		TradeNo:            "toss_sub_done_webhook",
		PaymentMethod:      "toss",
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		BillingKeyId:       keyId,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}
	if err := model.SetTossSubscriptionOrderPlanSnapshot(initialOrder, plan); err != nil {
		t.Fatalf("snapshot subscription order: %v", err)
	}
	if err := initialOrder.Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	originalUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
		setting.TossUnitPrice = originalUnitPrice
	})

	setting.TossSecretKey = "sk_regular_secret"
	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false
	setting.TossUnitPrice = 2000

	var lookedUp bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_sub_done_webhook" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_order_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		lookedUp = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_sub_done_webhook","type":"BILLING","orderId":"toss_sub_done_webhook","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_done_webhook","paymentKey":"pay_sub_done_webhook","status":"DONE"}}`))

	TossWebhook(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook status = %d want 200", recorder.Code)
	}
	if !lookedUp {
		t.Fatal("expected DONE subscription order to be rechecked against Toss")
	}
	order := model.GetSubscriptionOrderByTradeNo("toss_sub_done_webhook")
	if order == nil {
		t.Fatal("subscription order missing")
	}
	if order.Status != common.TopUpStatusSuccess {
		t.Fatalf("order status = %q want success", order.Status)
	}
	if !strings.Contains(order.ProviderPayload, "pay_sub_done_webhook") {
		t.Fatalf("provider payload = %q want Toss payment payload", order.ProviderPayload)
	}
	var sub model.UserSubscription
	if err := model.DB.Where("user_id = ? AND plan_id = ?", 7, 3).First(&sub).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if !sub.AutoRenew || sub.BillingKeyId != keyId {
		t.Fatalf("subscription autoRenew=%v billingKeyId=%d want true/%d", sub.AutoRenew, sub.BillingKeyId, keyId)
	}
}

func TestTossWebhookRenewsExistingSubscriptionInsteadOfCreatingAnother(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "toss-renew-user", Status: common.UserStatusEnabled}).Error)
	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Toss Renewal Plan",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	keyID, err := model.StoreTossBillingKeyWithSecret(7, "cust_webhook_renew", "billing_webhook_renew", "현대", "433012******1234", "sk_renew_order")
	require.NoError(t, err)
	credential, err := model.EncryptProviderCredential("sk_renew_order")
	require.NoError(t, err)
	now := common.GetTimestamp()
	originalEnd := now + 86400
	subscription := &model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          plan.Id,
		StartTime:       now - 86400,
		EndTime:         originalEnd,
		Status:          "active",
		AutoRenew:       true,
		BillingKeyId:    keyID,
		NextBillingTime: 1000,
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	seedControllerTossRenewalContract(t, subscription, plan, 15000)
	orderID := "toss_sub_renew_11_1000"
	renewalOrder := &model.SubscriptionOrder{
		UserId:                   7,
		PlanId:                   plan.Id,
		Money:                    10,
		TradeNo:                  orderID,
		PaymentMethod:            model.PaymentMethodToss,
		PaymentProvider:          model.PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		CreateTime:               now,
		BillingKeyId:             keyID,
		ProviderAmount:           15000,
		ProviderCurrency:         "KRW",
		ProviderCredential:       credential,
		BillingAttempted:         true,
		BillingAttemptCredential: credential,
		RenewalEndTime:           originalEnd,
	}
	require.NoError(t, model.SetTossRenewalOrderSnapshotFromContract(renewalOrder, subscription))
	require.NoError(t, renewalOrder.Insert())

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/payments/pay_sub_renew_webhook", r.URL.EscapedPath())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_sub_renew_webhook","type":"BILLING","orderId":"toss_sub_renew_11_1000","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_renew_11_1000","paymentKey":"pay_sub_renew_webhook","status":"DONE"}}`))
	TossWebhook(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var renewed model.UserSubscription
	require.NoError(t, model.DB.First(&renewed, 11).Error)
	require.Greater(t, renewed.EndTime, originalEnd)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 7).Count(&subscriptionCount).Error)
	require.EqualValues(t, 1, subscriptionCount)
	order := model.GetSubscriptionOrderByTradeNo(orderID)
	require.NotNil(t, order)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
}

func TestTossWebhookSettlesWalletAutoRechargeAndFinalizesPolicyOnce(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	gin.SetMode(gin.TestMode)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	originalUnitPrice := setting.TossUnitPrice
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
		setting.TossUnitPrice = originalUnitPrice
	})
	setting.TossBillingSecretKey = "sk_active_wallet_webhook"
	setting.TossTestMode = false
	setting.TossUnitPrice = 1000

	require.NoError(t, model.DB.Create(&model.User{Id: 81, Username: "wallet-webhook-owner", Role: common.RoleCommonUser, AffCode: "wallet-webhook-owner"}).Error)
	now := time.Now()
	activeKey := fmt.Sprintf("%s:%d:%s", model.TopUpTargetTypeUser, 81, model.WalletAutoRechargeTypeScheduled)
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: 81, OwnerUserId: 81,
		Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: model.WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: now.Add(-time.Minute).Unix(), FailCount: 2,
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_webhook_controller_done", policy.Id)
	credential, err := model.EncryptProviderCredential("sk_snapshot_wallet_webhook")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 81, TargetType: model.TopUpTargetTypeUser, TargetId: 81, Amount: 10000, Money: 10,
		Quota: model.TossCreditQuotaFromKRW(10000), TradeNo: tradeNo, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	getCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		getCalls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/payments/pay_wallet_controller_done", r.URL.EscapedPath())
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("sk_snapshot_wallet_webhook:")), r.Header.Get("Authorization"))
		body := fmt.Sprintf(`{"paymentKey":"pay_wallet_controller_done","type":"BILLING","orderId":"%s","status":"DONE","totalAmount":10000,"balanceAmount":10000,"currency":"KRW","method":"카드","card":{"amount":10000,"company":"card","number":"****1111"}}`, tradeNo)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	event := fmt.Sprintf(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"%s","paymentKey":"pay_wallet_controller_done","status":"DONE"}}`, tradeNo)
	require.Equal(t, http.StatusOK, performTossWebhookForTest(event).Code)
	require.Equal(t, http.StatusOK, performTossWebhookForTest(event).Code)
	require.Equal(t, 2, getCalls)

	var user model.User
	require.NoError(t, model.DB.First(&user, 81).Error)
	require.Equal(t, int64(model.TossCreditQuotaFromKRW(10000)), user.Quota)
	var topUp model.TopUp
	require.NoError(t, model.DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "pay_wallet_controller_done", topUp.ProviderOrderId)
	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, tradeNo, reloaded.LastTradeNo)
	require.Zero(t, reloaded.FailCount)
	require.Empty(t, reloaded.LastError)
	require.Greater(t, reloaded.NextChargeTime, policy.NextChargeTime)
}

func TestTossWebhookPartialWalletCancellationPersistsEventAndStopsRecharge(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	gin.SetMode(gin.TestMode)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossBillingSecretKey = "sk_active_wallet_cancel"
	setting.TossTestMode = false

	require.NoError(t, model.DB.Create(&model.User{Id: 82, Username: "wallet-cancel-owner", Role: common.RoleCommonUser, AffCode: "wallet-cancel-owner"}).Error)
	keyID, err := model.StoreTossBillingKeyWithSecret(82, "wallet-cancel-customer", "wallet-cancel-key", "card", "****2222", "sk_snapshot_wallet_cancel")
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d:%s", model.TopUpTargetTypeUser, 82, model.WalletAutoRechargeTypeScheduled)
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: 82, OwnerUserId: 82,
		BillingKeyId: keyID, Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: model.WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_webhook_partial", policy.Id)
	credential, err := model.EncryptProviderCredential("sk_snapshot_wallet_cancel")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 82, TargetType: model.TopUpTargetTypeUser, TargetId: 82, Amount: 10000, TradeNo: tradeNo,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("sk_snapshot_wallet_cancel:")), r.Header.Get("Authorization"))
		body := fmt.Sprintf(`{"paymentKey":"pay_wallet_partial","type":"BILLING","orderId":"%s","status":"PARTIAL_CANCELED","totalAmount":10000,"balanceAmount":3000,"currency":"KRW","method":"카드","card":{"amount":10000,"company":"card","number":"****2222"},"cancels":[{"cancelAmount":7000,"transactionKey":"cancel-wallet-partial","cancelStatus":"DONE"}]}`, tradeNo)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	event := fmt.Sprintf(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"%s","paymentKey":"pay_wallet_partial","status":"PARTIAL_CANCELED"}}`, tradeNo)
	require.Equal(t, http.StatusOK, performTossWebhookForTest(event).Code)

	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.First(&cancellation, "order_id = ? AND event_type = ?", tradeNo, model.TossPaymentEventTypeCancellation).Error)
	require.Equal(t, int64(7000), cancellation.CancelAmount)
	require.Equal(t, int64(3000), cancellation.BalanceAmount)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusFailed, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
	var topUp model.TopUp
	require.NoError(t, model.DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusFailed, topUp.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 82).Error)
	require.Zero(t, user.Quota)
}

func TestTossWebhookRejectsMismatchedWalletCancellationWithoutStoppingPolicy(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	gin.SetMode(gin.TestMode)
	originalBase, originalClient := tossAPIBase, http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	require.NoError(t, model.DB.Create(&model.User{Id: 83, Username: "wallet-mismatch-owner", Role: common.RoleCommonUser, AffCode: "wallet-mismatch-owner"}).Error)
	keyID, err := model.StoreTossBillingKeyWithSecret(83, "wallet-mismatch-customer", "wallet-mismatch-key", "card", "****3333", "sk_wallet_mismatch")
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d:%s", model.TopUpTargetTypeUser, 83, model.WalletAutoRechargeTypeScheduled)
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: 83, OwnerUserId: 83,
		BillingKeyId: keyID, Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: model.WalletAutoRechargeStatusActive, ActiveKey: &activeKey, NextChargeTime: time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	tradeNo := fmt.Sprintf("wallet_auto_%d_webhook_mismatch", policy.Id)
	credential, err := model.EncryptProviderCredential("sk_wallet_mismatch")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 83, TargetType: model.TopUpTargetTypeUser, TargetId: 83, Amount: 10000, TradeNo: tradeNo,
		ProviderCredential: credential, PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"paymentKey":"pay_wallet_mismatch","type":"BILLING","orderId":"%s","status":"CANCELED","totalAmount":5000,"balanceAmount":0,"cancels":[{"cancelAmount":5000,"transactionKey":"cancel-wallet-mismatch","cancelStatus":"DONE"}]}`, tradeNo)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"
	event := fmt.Sprintf(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"%s","paymentKey":"pay_wallet_mismatch","status":"CANCELED"}}`, tradeNo)
	require.Equal(t, http.StatusServiceUnavailable, performTossWebhookForTest(event).Code)

	var eventCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ?", tradeNo).Count(&eventCount).Error)
	require.Zero(t, eventCount)
	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusActive, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
	var topUp model.TopUp
	require.NoError(t, model.DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)

	// A NORMAL payment with a terminal status must not drive a billing-policy
	// mutation merely because its orderId/paymentKey happen to match.
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"paymentKey":"pay_wallet_mismatch","type":"NORMAL","orderId":"%s","status":"ABORTED","totalAmount":10000,"balanceAmount":10000,"currency":"KRW","method":"카드","card":{"amount":10000}}`, tradeNo)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	abortedEvent := fmt.Sprintf(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"%s","paymentKey":"pay_wallet_mismatch","status":"ABORTED"}}`, tradeNo)
	require.Equal(t, http.StatusOK, performTossWebhookForTest(abortedEvent).Code)
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusActive, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)
	require.NoError(t, model.DB.First(&topUp, "trade_no = ?", tradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
}

func setupTossTopUpAmountModeControllerTest(t *testing.T) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	originalTossConfig := setting.GetTossConfigSnapshot()
	originalServerAddress := system_setting.ServerAddress
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":   "true",
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_toss_amount",
		"TossSecretKey": "live_sk_toss_amount",
		"TossMinTopUp":  "1000",
		"TossUnitPrice": "1300",
	}))
	system_setting.ServerAddress = "https://pay.example.com"
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}

	t.Cleanup(func() {
		restoreTossConfigForOptionTest(t, originalTossConfig)
		system_setting.ServerAddress = originalServerAddress
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	})
}

func TestRequestTossAmountQuotaModeReturnsQuoteWhenChargeMeetsMin(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)

	user := model.User{
		Id:       1,
		Username: "toss-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "toss-user",
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestTossAmount,
		user,
		http.MethodPost,
		"/api/user/toss/amount",
		`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)

	var payload struct {
		Message string `json:"message"`
		Data    struct {
			AmountMode   string  `json:"amount_mode"`
			InputAmount  int64   `json:"input_amount"`
			ChargeAmount int64   `json:"charge_amount"`
			CreditAmount float64 `json:"credit_amount"`
			CreditQuota  int     `json:"credit_quota"`
			UnitPrice    float64 `json:"unit_price"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.Equal(t, "success", payload.Message)
	require.Equal(t, model.TossTopUpAmountModeQuota, payload.Data.AmountMode)
	require.Equal(t, int64(1), payload.Data.InputAmount)
	require.Equal(t, int64(1300), payload.Data.ChargeAmount)
	require.Equal(t, 1.0, payload.Data.CreditAmount)
	require.Equal(t, 500000, payload.Data.CreditQuota)
	require.Equal(t, 1300.0, payload.Data.UnitPrice)
}

func TestRequestTossAmountUsesUnifiedCardAndEasyPayMinimum(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossMinTopUp": "100",
	}))

	user := model.User{
		Id:       92,
		Username: "toss-unified-minimum",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "toss-unified-minimum",
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	below := performOrganizationRequest(
		RequestTossAmount,
		user,
		http.MethodPost,
		"/api/user/toss/amount",
		`{"amount":199,"amount_mode":"krw","payment_method":"toss"}`,
	)
	require.NotContains(t, below.Body.String(), `"message":"success"`)

	minimum := performOrganizationRequest(
		RequestTossAmount,
		user,
		http.MethodPost,
		"/api/user/toss/amount",
		`{"amount":200,"amount_mode":"krw","payment_method":"toss"}`,
	)
	require.Contains(t, minimum.Body.String(), `"message":"success"`)
	require.Contains(t, minimum.Body.String(), `"charge_amount":200`)
}

func TestTossTopUpEndpointsRejectUnknownAmountMode(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)
	user := model.User{
		Id:       91,
		Username: "toss-invalid-mode-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "toss-invalid-mode-user",
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		path    string
	}{
		{name: "amount", handler: RequestTossAmount, path: "/api/user/toss/amount"},
		{name: "pay", handler: RequestTossPay, path: "/api/user/toss/pay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := performOrganizationRequest(
				tc.handler,
				user,
				http.MethodPost,
				tc.path,
				`{"amount":1000,"amount_mode":"unexpected","payment_method":"toss"}`,
			)
			require.NotContains(t, res.Body.String(), `"message":"success"`)
		})
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestRequestTossPayQuotaModeReturnsStructuredFieldsAndPersistsCreditAmount(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)

	user := model.User{
		Id:       2,
		Username: "toss-pay-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "toss-pay-user",
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestTossPay,
		user,
		http.MethodPost,
		"/api/user/toss/pay",
		`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)

	var payload struct {
		Message string `json:"message"`
		Data    struct {
			OrderID      string  `json:"order_id"`
			Amount       int64   `json:"amount"`
			ChargeAmount int64   `json:"charge_amount"`
			CreditAmount float64 `json:"credit_amount"`
			CreditQuota  int     `json:"credit_quota"`
			UnitPrice    float64 `json:"unit_price"`
			AmountMode   string  `json:"amount_mode"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.Equal(t, "success", payload.Message)
	require.NotEmpty(t, payload.Data.OrderID)
	require.Equal(t, int64(1300), payload.Data.Amount)
	require.Equal(t, int64(1300), payload.Data.ChargeAmount)
	require.Equal(t, 1.0, payload.Data.CreditAmount)
	require.Equal(t, 500000, payload.Data.CreditQuota)
	require.Equal(t, 1300.0, payload.Data.UnitPrice)
	require.Equal(t, model.TossTopUpAmountModeQuota, payload.Data.AmountMode)

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", payload.Data.OrderID).First(&topUp).Error)
	require.Equal(t, int64(1300), topUp.Amount)
	require.Equal(t, 1.0, topUp.Money)
	require.Equal(t, 500000, topUp.Quota)
	require.NotEmpty(t, topUp.ProviderCredential)
}

func TestOrganizationTossAmountQuotaModeUsesChargeForMinValidation(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)

	owner := model.User{
		Id:               3,
		Username:         "org-owner",
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   7,
		OrganizationRole: model.OrganizationRoleOwner,
		AffCode:          "org-owner",
		Group:            "default",
	}
	org := model.Organization{
		Id:          7,
		Name:        "Acme",
		OwnerUserId: owner.Id,
		Quota:       1000,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		RequestOrganizationTossAmount,
		owner,
		http.MethodPost,
		"/api/organization/toss/amount",
		`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Message string `json:"message"`
		Data    struct {
			AmountMode   string `json:"amount_mode"`
			ChargeAmount int64  `json:"charge_amount"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.Equal(t, "success", payload.Message)
	require.Equal(t, model.TossTopUpAmountModeQuota, payload.Data.AmountMode)
	require.Equal(t, int64(1300), payload.Data.ChargeAmount)
}

func TestTossProviderIdentifierValidation(t *testing.T) {
	require.True(t, isValidTossOrderId("order_123-ABC"))
	require.True(t, isValidTossOrderId(strings.Repeat("a", tossOrderIdMaxBytes)))
	require.False(t, isValidTossOrderId(strings.Repeat("a", tossOrderIdMinBytes-1)))
	require.False(t, isValidTossOrderId(strings.Repeat("a", tossOrderIdMaxBytes+1)))
	require.False(t, isValidTossOrderId("order/123"))
	require.False(t, isValidTossOrderId("주문_123"))

	require.True(t, isValidTossPaymentKey(strings.Repeat("가", tossPaymentKeyMaxRunes)))
	require.False(t, isValidTossPaymentKey(""))
	require.False(t, isValidTossPaymentKey(" payment-key"))
	require.False(t, isValidTossPaymentKey("payment-key "))
	require.False(t, isValidTossPaymentKey("payment\nkey"))
	require.False(t, isValidTossPaymentKey("payment key"))
	require.False(t, isValidTossPaymentKey(strings.Repeat("가", tossPaymentKeyMaxRunes+1)))

	require.Error(t, validateTossPaymentResponseShape(&tossConfirmResponse{
		PaymentKey: " payment-key",
		OrderId:    "order_123-ABC",
		Status:     "DONE",
	}))
	require.Error(t, validateTossPaymentResponseShape(&tossConfirmResponse{
		PaymentKey: "payment-key",
		OrderId:    "order_123-ABC",
		Status:     "DONE\nFORGED",
	}))
	require.Error(t, validateTossPaymentResponseShape(&tossConfirmResponse{
		PaymentKey: "payment-key",
		OrderId:    "order_123-ABC",
		Status:     "DONE",
		Currency:   "KRW\nFORGED",
	}))
}

func TestTossSDKCustomerKeyValidation(t *testing.T) {
	valid50 := "cust_" + strings.Repeat("a", tossSDKCustomerKeyMaxBytes-len("cust_"))
	for _, customerKey := range []string{"a-", "cust_random_123", valid50} {
		require.True(t, isValidTossSDKCustomerKey(customerKey), customerKey)
	}
	for _, customerKey := range []string{
		"",
		"a",
		"customer123",
		" cust_random_123",
		"cust/random",
		"고객_123",
		valid50 + "a",
	} {
		require.False(t, isValidTossSDKCustomerKey(customerKey), customerKey)
	}
}

func TestRequestTossPayRejectsLegacyCustomerKeyAboveSDKLimitBeforeOrderCreation(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)

	user := model.User{
		Id:              94,
		Username:        "toss-legacy-long-customer-key",
		Password:        "x",
		Role:            common.RoleCommonUser,
		AffCode:         "toss-legacy-long-customer-key",
		Group:           "default",
		TossCustomerKey: "cust_" + strings.Repeat("a", 46), // 51 bytes: valid for the server API, invalid for SDK v2.
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestTossPay,
		user,
		http.MethodPost,
		"/api/user/toss/pay",
		`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
	)
	require.NotContains(t, res.Body.String(), `"message":"success"`)

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTossAlreadyProcessedErrorCodesRemainAmbiguous(t *testing.T) {
	for _, code := range []string{
		"ALREADY_PROCESSED_PAYMENT",
		"ALREADY_COMPLETED_PAYMENT",
		"ALREADY_CANCELED_PAYMENT",
		"DUPLICATED_ORDER_ID",
		"DUPLICATED_REQUEST",
		"PROVIDER_ERROR",
		"COMMON_ERROR",
		"FORBIDDEN_CONSECUTIVE_REQUEST",
	} {
		err := newTossAPIError(http.MethodPost, http.StatusBadRequest, []byte(fmt.Sprintf(`{"code":%q,"message":"test"}`, code)))
		require.True(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, err), code)
	}
}

func TestTossUnreadableOrUnknownProviderErrorsRemainAmbiguous(t *testing.T) {
	readErr := &tossAPITransportError{Method: http.MethodPost, Err: io.ErrUnexpectedEOF}
	require.True(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, readErr))
	require.True(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, errTossAPIResponseTooLarge))

	unknown := newTossAPIError(http.MethodPost, http.StatusBadRequest, []byte(`<html>proxy error</html>`))
	require.Equal(t, "UNKNOWN", tossAPIErrorCode(unknown))
	require.True(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, unknown))

	for _, code := range []string{
		"INVALID_PAYMENT_KEY",                             // normal payment confirm
		"PAY_PROCESS_ABORTED",                             // provider-declined normal payment
		"NOT_SUPPORTED_CARD_TYPE",                         // billing-key issue
		"INVALID_BILL_KEY_REQUEST",                        // automatic billing charge
		"NOT_SUPPORTED_INSTALLMENT_PLAN_CARD_OR_MERCHANT", // card/billing contract validation
		"INVALID_CARD_INSTALLMENT_PLAN",
	} {
		knownTerminal := newTossAPIError(
			http.MethodPost,
			http.StatusBadRequest,
			[]byte(fmt.Sprintf(`{"code":%q}`, code)),
		)
		require.False(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, knownTerminal), code)
	}

	futureProcessingCode := newTossAPIError(
		http.MethodPost,
		http.StatusBadRequest,
		[]byte(`{"code":"PAYMENT_PROCESSING_DEFERRED_V2"}`),
	)
	require.True(t, isTossAmbiguousPaymentOutcome(http.StatusBadRequest, futureProcessingCode),
		"a newly introduced 4xx code must not release the payment/idempotency binding before this binary understands it")
}
