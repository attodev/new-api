package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"gorm.io/gorm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setupTossBillingControllerTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.CryptoSecret = "toss-billing-controller-test-secret"
	if err := model.DB.AutoMigrate(&model.UserBillingKey{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		common.CryptoSecret = originalSecret
	})
}

func TestTossSubscriptionChargeKRW(t *testing.T) {
	prev := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	defer func() { setting.TossUnitPrice = prev }()

	plan := &model.SubscriptionPlan{PriceAmount: 10}
	got := tossSubscriptionChargeKRW(plan)
	if got != 15000 { // 10 USD-equiv × 1500 ₩/unit
		t.Fatalf("chargeKRW = %d want 15000", got)
	}
}

func TestTossBillingChargeTimeoutMeetsDocumentedMinimum(t *testing.T) {
	if tossBillingChargeTimeout < 60*time.Second {
		t.Fatalf("tossBillingChargeTimeout = %s want at least 60s", tossBillingChargeTimeout)
	}
}

func TestSubscriptionTossBillingFailRedirectPreservesFailureContext(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionOrder{}); err != nil {
		t.Fatalf("migrate subscription order table: %v", err)
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "trade_no", Value: "toss_sub_fail"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/fail/toss_sub_fail?code=USER_CANCEL&message=%EC%B9%B4%EB%93%9C%20%EB%93%B1%EB%A1%9D%20%EC%B7%A8%EC%86%8C", nil)

	SubscriptionTossBillingFail(c)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d want %d", recorder.Code, http.StatusFound)
	}
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "/console/topup?") {
		t.Fatalf("redirect location = %q want /console/topup with query", location)
	}
	if !strings.Contains(location, "toss_error_code=USER_CANCEL") {
		t.Fatalf("redirect location should preserve Toss failure code, got %q", location)
	}
	if !strings.Contains(location, "toss_order_id=toss_sub_fail") {
		t.Fatalf("redirect location should preserve Toss trade no, got %q", location)
	}
	if !strings.Contains(location, "toss_error_message=") {
		t.Fatalf("redirect location should preserve Toss failure message, got %q", location)
	}
}

func TestSubscriptionTossBillingConfirmUsesOrderChargeSnapshot(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", TossCustomerKey: "cust_snapshot"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3307,
		Title:         "Snapshot Plan",
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
	credential, err := model.EncryptProviderCredential("sk_order_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	if err := (&model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            "toss_sub_snapshot",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}).Insert(); err != nil {
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

	var chargedAmount int64
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_order_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/authorizations/issue":
			var payload map[string]string
			if err := common.DecodeJson(r.Body, &payload); err != nil {
				t.Fatalf("decode issue payload: %v", err)
			}
			if payload["authKey"] != "auth_snapshot" || payload["customerKey"] != "cust_snapshot" {
				t.Fatalf("unexpected issue payload %#v", payload)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_snapshot","customerKey":"cust_snapshot","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_snapshot":
			var payload struct {
				CustomerKey string `json:"customerKey"`
				Amount      int64  `json:"amount"`
				OrderId     string `json:"orderId"`
				OrderName   string `json:"orderName"`
			}
			if err := common.DecodeJson(r.Body, &payload); err != nil {
				t.Fatalf("decode charge payload: %v", err)
			}
			chargedAmount = payload.Amount
			if payload.CustomerKey != "cust_snapshot" || payload.OrderId != "toss_sub_snapshot" {
				t.Fatalf("unexpected charge payload %#v", payload)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_snapshot","orderId":"toss_sub_snapshot","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "trade_no", Value: "toss_sub_snapshot"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm/toss_sub_snapshot?authKey=auth_snapshot&customerKey=cust_snapshot", nil)

	SubscriptionTossBillingConfirm(c)

	if chargedAmount != 15000 {
		t.Fatalf("charged amount = %d want stored snapshot 15000", chargedAmount)
	}
	order := model.GetSubscriptionOrderByTradeNo("toss_sub_snapshot")
	if order == nil {
		t.Fatal("subscription order missing")
	}
	if order.Status != common.TopUpStatusSuccess {
		t.Fatalf("order status = %q want success", order.Status)
	}
}

func TestSubscriptionTossBillingConfirmExpiresOrderWhenBillingKeyIssueResponseMayBeLost(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", TossCustomerKey: "cust_lost"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3310,
		Title:         "Lost Issue Plan",
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
	credential, err := model.EncryptProviderCredential("sk_issue_lost_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	if err := (&model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            "toss_sub_issue_lost",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}).Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "sk_regular_secret"
	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false

	var attempts int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/billing/authorizations/issue" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		attempts++
		return nil, errors.New("response lost")
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_lost&customerKey=cust_lost&trade_no=toss_sub_issue_lost", nil)

	SubscriptionTossBillingConfirm(c)

	if attempts != tossAPIMaxAttempts {
		t.Fatalf("attempts = %d want %d", attempts, tossAPIMaxAttempts)
	}
	order := model.GetSubscriptionOrderByTradeNo("toss_sub_issue_lost")
	if order == nil {
		t.Fatal("subscription order missing")
	}
	if order.Status != common.TopUpStatusExpired {
		t.Fatalf("order status = %q want expired so unknown remote billing key cannot be auto-charged", order.Status)
	}
	if order.CompleteTime <= 0 {
		t.Fatalf("order complete_time = %d want set", order.CompleteTime)
	}
	var keyCount int64
	if err := model.DB.Model(&model.UserBillingKey{}).Count(&keyCount).Error; err != nil {
		t.Fatalf("count billing keys: %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("billing key count = %d want 0", keyCount)
	}
	var logs []model.Log
	if err := model.LOG_DB.Where("user_id = ? AND type = ?", 7, model.LogTypeTopup).Find(&logs).Error; err != nil {
		t.Fatalf("query logs: %v", err)
	}
	found := false
	for _, log := range logs {
		if strings.Contains(log.Content, "Toss billing key issue uncertain") &&
			strings.Contains(log.Content, "manual Toss console check required") &&
			strings.Contains(log.Content, "toss_sub_issue_lost") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected uncertain billing-key manual reconciliation log, got %#v", logs)
	}
}

func TestSubscriptionTossBillingConfirmQueuesIssuedBillingKeyWhenValidationDeleteFails(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", TossCustomerKey: "cust_expected"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3311,
		Title:         "Validation Mismatch Plan",
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
	credential, err := model.EncryptProviderCredential("sk_issue_validation_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	if err := (&model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            "toss_sub_issue_validation",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}).Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "sk_regular_secret"
	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false

	var deleteAttempts int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/authorizations/issue":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_validation_mismatch","customerKey":"cust_other","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/v1/billing/billing_validation_mismatch":
			deleteAttempts++
			return nil, errors.New("delete response lost")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_validation&customerKey=cust_expected&trade_no=toss_sub_issue_validation", nil)

	SubscriptionTossBillingConfirm(c)

	if deleteAttempts != tossAPIMaxAttempts {
		t.Fatalf("deleteAttempts = %d want %d", deleteAttempts, tossAPIMaxAttempts)
	}
	order := model.GetSubscriptionOrderByTradeNo("toss_sub_issue_validation")
	if order == nil {
		t.Fatal("subscription order missing")
	}
	if order.Status != common.TopUpStatusExpired {
		t.Fatalf("order status = %q want expired", order.Status)
	}
	var key model.UserBillingKey
	if err := model.DB.Where("status = ?", model.BillingKeyStatusPendingRevocation).First(&key).Error; err != nil {
		t.Fatalf("expected pending revocation billing key: %v", err)
	}
	if key.UserId != 7 {
		t.Fatalf("billing key user_id = %d want 7", key.UserId)
	}
	if key.CustomerKey != "cust_other" {
		t.Fatalf("billing key customer_key = %q want issued customer key", key.CustomerKey)
	}
	plain, err := common.DecryptString(key.EncryptedKey)
	if err != nil {
		t.Fatalf("decrypt queued billing key: %v", err)
	}
	if plain != "billing_validation_mismatch" {
		t.Fatalf("queued billing key = %q want billing_validation_mismatch", plain)
	}
}

func TestSubscriptionTossBillingConfirmKeepsPendingWhenFirstChargeStillProcessing(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", TossCustomerKey: "cust_processing"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3311,
		Title:         "Processing Plan",
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
	credential, err := model.EncryptProviderCredential("sk_processing_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	if err := (&model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            "toss_sub_processing",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         common.GetTimestamp(),
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
	}).Insert(); err != nil {
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

	var deleted bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_processing_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/authorizations/issue":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_processing","customerKey":"cust_processing","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_processing":
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"IDEMPOTENT_REQUEST_PROCESSING","message":"request is processing"}`)),
			}, nil
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/toss_sub_processing":
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not found"}`)),
			}, nil
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/v1/billing/billing_processing":
			deleted = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_processing&customerKey=cust_processing&trade_no=toss_sub_processing", nil)

	SubscriptionTossBillingConfirm(c)

	if recorder.Code != http.StatusFound {
		t.Fatalf("confirm status = %d want 302", recorder.Code)
	}
	if deleted {
		t.Fatal("billing key should not be deleted while Toss charge is still processing")
	}
	order := model.GetSubscriptionOrderByTradeNo("toss_sub_processing")
	if order == nil {
		t.Fatal("subscription order missing")
	}
	if order.Status != common.TopUpStatusPending {
		t.Fatalf("order status = %q want pending", order.Status)
	}
	if order.BillingKeyId <= 0 {
		t.Fatal("expected billing key to remain attached for later order lookup recovery")
	}
	var key model.UserBillingKey
	if err := model.DB.First(&key, order.BillingKeyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusActive {
		t.Fatalf("billing key status = %q want active", key.Status)
	}
}

func TestReconcileTossPendingSubscriptionOrderCompletesDonePaymentByOrderLookup(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", TossCustomerKey: "cust_reconcile_done"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3312,
		Title:         "Reconcile Plan",
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
	billingKeyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_reconcile_done", "billing_reconcile_done", "현대", "433012******1234", "sk_reconcile_secret")
	if err != nil {
		t.Fatalf("store billing key: %v", err)
	}
	credential, err := model.EncryptProviderCredential("sk_reconcile_secret")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	order := &model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            "toss_sub_reconcile_done",
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         1000,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
		BillingKeyId:       billingKeyId,
	}
	if err := order.Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "sk_regular_secret"
	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_reconcile_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/orders/toss_sub_reconcile_done" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_reconcile_done","orderId":"toss_sub_reconcile_done","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	if err != nil {
		t.Fatalf("reconcile pending subscription: %v", err)
	}
	if !resolved {
		t.Fatal("expected DONE payment lookup to resolve pending subscription order")
	}
	updated := model.GetSubscriptionOrderByTradeNo("toss_sub_reconcile_done")
	if updated == nil {
		t.Fatal("subscription order missing")
	}
	if updated.Status != common.TopUpStatusSuccess {
		t.Fatalf("order status = %q want success", updated.Status)
	}
	var activeCount int64
	if err := model.DB.Model(&model.UserSubscription{}).Where("user_id = ? AND plan_id = ? AND auto_renew = ?", 7, plan.Id, true).Count(&activeCount).Error; err != nil {
		t.Fatalf("count active subscription: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active auto-renew subscription count = %d want 1", activeCount)
	}
}

func TestReconcileTossPendingSubscriptionOrderRenewsPendingRenewalOrder(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3313,
		Title:         "Renewal Reconcile Plan",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationCustom,
		CustomSeconds: 3600,
		Enabled:       true,
	}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	model.InvalidateSubscriptionPlanCache(plan.Id)
	billingKeyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_reconcile_renewal", "billing_reconcile_renewal", "현대", "433012******1234", "sk_reconcile_renewal")
	if err != nil {
		t.Fatalf("store billing key: %v", err)
	}
	const oldEndTime int64 = 2000
	const oldNextBillingTime int64 = 1500
	if err := model.DB.Create(&model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          plan.Id,
		Status:          "active",
		StartTime:       1000,
		EndTime:         oldEndTime,
		AutoRenew:       true,
		BillingKeyId:    billingKeyId,
		NextBillingTime: oldNextBillingTime,
	}).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	credential, err := model.EncryptProviderCredential("sk_reconcile_renewal")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	tradeNo := "toss_sub_renew_11_1500"
	order := &model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            tradeNo,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         1000,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
		BillingKeyId:       billingKeyId,
	}
	if err := order.Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

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

	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_reconcile_renewal:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/orders/toss_sub_renew_11_1500" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_reconcile_renewal","orderId":"toss_sub_renew_11_1500","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	if err != nil {
		t.Fatalf("reconcile pending renewal: %v", err)
	}
	if !resolved {
		t.Fatal("expected DONE renewal lookup to resolve pending renewal order")
	}
	updated := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if updated == nil {
		t.Fatal("subscription order missing")
	}
	if updated.Status != common.TopUpStatusSuccess {
		t.Fatalf("order status = %q want success", updated.Status)
	}
	var sub model.UserSubscription
	if err := model.DB.First(&sub, 11).Error; err != nil {
		t.Fatalf("load existing subscription: %v", err)
	}
	if sub.EndTime != oldEndTime+3600 {
		t.Fatalf("subscription end_time = %d want %d", sub.EndTime, oldEndTime+3600)
	}
	if sub.NextBillingTime <= oldNextBillingTime {
		t.Fatalf("next_billing_time = %d want greater than %d", sub.NextBillingTime, oldNextBillingTime)
	}
	var subscriptionCount int64
	if err := model.DB.Model(&model.UserSubscription{}).Where("user_id = ? AND plan_id = ?", 7, plan.Id).Count(&subscriptionCount).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subscriptionCount != 1 {
		t.Fatalf("subscription count = %d want existing subscription only", subscriptionCount)
	}
}

func TestReconcileTossPendingSubscriptionOrderExpiresRenewalAndDisablesAutoRenew(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3314,
		Title:         "Stale Renewal Plan",
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
	billingKeyId, err := model.StoreTossBillingKeyWithSecret(7, "cust_reconcile_stale", "billing_reconcile_stale", "현대", "433012******1234", "sk_reconcile_stale")
	if err != nil {
		t.Fatalf("store billing key: %v", err)
	}
	if err := model.DB.Create(&model.UserSubscription{
		Id:               12,
		UserId:           7,
		PlanId:           plan.Id,
		Status:           "active",
		StartTime:        1000,
		EndTime:          2000,
		AutoRenew:        true,
		BillingKeyId:     billingKeyId,
		NextBillingTime:  1500,
		BillingFailCount: 0,
	}).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	credential, err := model.EncryptProviderCredential("sk_reconcile_stale")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	tradeNo := "toss_sub_renew_12_1500"
	order := &model.SubscriptionOrder{
		UserId:             7,
		PlanId:             plan.Id,
		Money:              10,
		TradeNo:            tradeNo,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		Status:             common.TopUpStatusPending,
		CreateTime:         1000,
		ProviderAmount:     15000,
		ProviderCurrency:   "KRW",
		ProviderCredential: credential,
		BillingKeyId:       billingKeyId,
	}
	if err := order.Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

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

	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_reconcile_stale:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want order credential auth", got)
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/orders/toss_sub_renew_12_1500" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	if err != nil {
		t.Fatalf("reconcile stale renewal: %v", err)
	}
	if !resolved {
		t.Fatal("expected missing renewal payment to resolve by expiring the pending renewal order")
	}
	updated := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if updated == nil {
		t.Fatal("subscription order missing")
	}
	if updated.Status != common.TopUpStatusExpired {
		t.Fatalf("order status = %q want expired", updated.Status)
	}
	var key model.UserBillingKey
	if err := model.DB.First(&key, billingKeyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusPendingRevocation {
		t.Fatalf("billing key status = %q want pending_revocation", key.Status)
	}
	var sub model.UserSubscription
	if err := model.DB.First(&sub, 12).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if sub.BillingFailCount != 1 {
		t.Fatalf("billing fail count = %d want 1", sub.BillingFailCount)
	}
	if sub.AutoRenew {
		t.Fatal("auto-renew should be disabled after stale renewal expires")
	}
}

func TestSubscriptionRequestTossBillingRejectsBelowTossCardMinimum(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Tiny",
		PriceAmount:   1,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	if err := model.DB.Create(plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	model.InvalidateSubscriptionPlanCache(plan.Id)

	ps := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := ps.ComplianceConfirmed
	originalComplianceTermsVersion := ps.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalTestMode := setting.TossTestMode
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalUnitPrice := setting.TossUnitPrice
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
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
	setting.TossBillingEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "ck_live_regular"
	setting.TossSecretKey = "sk_live_regular"
	setting.TossBillingClientKey = "ck_live_billing"
	setting.TossBillingSecretKey = "sk_live_billing"
	setting.TossUnitPrice = 50
	system_setting.ServerAddress = "https://example.com"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/toss/pay", strings.NewReader(`{"plan_id":3}`))

	SubscriptionRequestTossBilling(c)

	var resp struct {
		Success bool `json:"success"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected Toss billing request to reject below-minimum card amount, got %q", recorder.Body.String())
	}
	var orderCount int64
	if err := model.DB.Model(&model.SubscriptionOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatalf("count subscription orders: %v", err)
	}
	if orderCount != 0 {
		t.Fatalf("subscription order count = %d want 0", orderCount)
	}
}

func TestSubscriptionRequestTossBillingStoresBillingCredential(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Pro",
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

	ps := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := ps.ComplianceConfirmed
	originalComplianceTermsVersion := ps.ComplianceTermsVersion
	originalBillingEnabled := setting.TossBillingEnabled
	originalTestMode := setting.TossTestMode
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalBillingClientKey := setting.TossBillingClientKey
	originalBillingSecretKey := setting.TossBillingSecretKey
	originalUnitPrice := setting.TossUnitPrice
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		setting.TossBillingEnabled = originalBillingEnabled
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
	setting.TossBillingEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "ck_live_regular"
	setting.TossSecretKey = "sk_live_regular"
	setting.TossBillingClientKey = "ck_live_billing"
	setting.TossBillingSecretKey = "sk_old_billing"
	setting.TossUnitPrice = 1300
	system_setting.ServerAddress = "https://example.com"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/toss/pay", strings.NewReader(`{"plan_id":3}`))

	SubscriptionRequestTossBilling(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Data struct {
			TradeNo    string `json:"trade_no"`
			SuccessURL string `json:"success_url"`
			FailURL    string `json:"fail_url"`
		} `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var order model.SubscriptionOrder
	if err := model.DB.Where("payment_provider = ?", model.PaymentProviderToss).First(&order).Error; err != nil {
		t.Fatalf("load subscription order: %v", err)
	}
	if resp.Data.TradeNo != order.TradeNo {
		t.Fatalf("response trade_no = %q want %q", resp.Data.TradeNo, order.TradeNo)
	}
	successURL, err := url.Parse(resp.Data.SuccessURL)
	if err != nil {
		t.Fatalf("parse success_url: %v", err)
	}
	failURL, err := url.Parse(resp.Data.FailURL)
	if err != nil {
		t.Fatalf("parse fail_url: %v", err)
	}
	if successURL.RawQuery != "" || failURL.RawQuery != "" {
		t.Fatalf("callback URLs should not pre-populate query strings, success=%q fail=%q", resp.Data.SuccessURL, resp.Data.FailURL)
	}
	if !strings.HasPrefix(successURL.Path, "/api/subscription/toss/confirm/toss_sub_") {
		t.Fatalf("success_url path = %q want path trade_no callback", successURL.Path)
	}
	if !strings.HasPrefix(failURL.Path, "/api/subscription/toss/fail/toss_sub_") {
		t.Fatalf("fail_url path = %q want path trade_no callback", failURL.Path)
	}
	secret, err := model.DecryptProviderCredential(order.ProviderCredential)
	if err != nil {
		t.Fatalf("decrypt provider credential: %v", err)
	}
	if secret != "sk_old_billing" {
		t.Fatalf("stored billing credential = %q want sk_old_billing", secret)
	}
	if order.ProviderAmount != 13000 {
		t.Fatalf("provider amount = %d want 13000", order.ProviderAmount)
	}
	if order.ProviderCurrency != "KRW" {
		t.Fatalf("provider currency = %q want KRW", order.ProviderCurrency)
	}
}

func TestIsValidTossBillingCharge(t *testing.T) {
	valid := &tossConfirmResponse{
		OrderId:     "toss_sub_order",
		Status:      "DONE",
		TotalAmount: 15000,
		Currency:    "krw",
		Card:        &tossPaymentCard{Company: "현대", Number: "433012******1234"},
	}
	if !isValidTossBillingCharge(valid, "toss_sub_order", 15000) {
		t.Fatal("expected valid billing charge")
	}

	cases := []struct {
		name string
		resp *tossConfirmResponse
	}{
		{name: "nil", resp: nil},
		{name: "wrong status", resp: &tossConfirmResponse{OrderId: "toss_sub_order", Status: "READY", TotalAmount: 15000, Currency: "KRW", Card: &tossPaymentCard{}}},
		{name: "wrong order", resp: &tossConfirmResponse{OrderId: "other", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Card: &tossPaymentCard{}}},
		{name: "wrong amount", resp: &tossConfirmResponse{OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 1, Currency: "KRW", Card: &tossPaymentCard{}}},
		{name: "wrong currency", resp: &tossConfirmResponse{OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "USD", Card: &tossPaymentCard{}}},
		{name: "missing card", resp: &tossConfirmResponse{OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW"}},
	}
	for _, tc := range cases {
		if isValidTossBillingCharge(tc.resp, "toss_sub_order", 15000) {
			t.Fatalf("%s: expected invalid billing charge", tc.name)
		}
	}
}

func TestReconcileTossPendingRenewalExpiresOldNonTerminalPayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}

	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Pro",
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
	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_key_stale_renewal", "현대", "433012******1234")
	if err != nil {
		t.Fatalf("store billing key: %v", err)
	}
	sub := &model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          3,
		Status:          "active",
		StartTime:       100,
		EndTime:         200,
		AutoRenew:       true,
		BillingKeyId:    keyId,
		NextBillingTime: 150,
	}
	if err := model.DB.Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	tradeNo := "toss_sub_renew_11_150"
	order := &model.SubscriptionOrder{
		UserId:           7,
		PlanId:           3,
		Money:            10,
		TradeNo:          tradeNo,
		PaymentMethod:    model.PaymentMethodToss,
		PaymentProvider:  model.PaymentProviderToss,
		Status:           common.TopUpStatusPending,
		CreateTime:       1000,
		ProviderAmount:   15000,
		ProviderCurrency: "KRW",
		BillingKeyId:     keyId,
	}
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossBillingSecretKey
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossBillingSecretKey = originalSecret
	})
	setting.TossBillingSecretKey = "sk_billing_secret"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/payments/orders/"+tradeNo {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_renewal","orderId":"toss_sub_renew_11_150","status":"READY","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	if err != nil {
		t.Fatalf("reconcile stale renewal: %v", err)
	}
	if !resolved {
		t.Fatal("expected stale non-terminal renewal to resolve as expired")
	}

	reloaded := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if reloaded == nil {
		t.Fatal("subscription order missing")
	}
	if reloaded.Status != common.TopUpStatusExpired {
		t.Fatalf("order status = %q want expired", reloaded.Status)
	}
	var reloadedSub model.UserSubscription
	if err := model.DB.First(&reloadedSub, 11).Error; err != nil {
		t.Fatalf("load subscription: %v", err)
	}
	if reloadedSub.BillingFailCount != 1 {
		t.Fatalf("billing fail count = %d want 1", reloadedSub.BillingFailCount)
	}
	if reloadedSub.AutoRenew {
		t.Fatal("auto-renew should be disabled after stale renewal expires")
	}
	var key model.UserBillingKey
	if err := model.DB.First(&key, keyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusPendingRevocation {
		t.Fatalf("billing key status = %q want pending_revocation", key.Status)
	}
}

func TestIssueTossBillingKeySendsIdempotencyKey(t *testing.T) {
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalSecret := setting.TossSecretKey
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossSecretKey = originalSecret
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})

	setting.TossSecretKey = "sk_regular_secret"
	setting.TossBillingClientKey = "ck_billing"
	setting.TossBillingSecretKey = "sk_billing_secret"
	setting.TossTestMode = false

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/billing/authorizations/issue" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_test_order" {
			t.Fatalf("Idempotency-Key = %q want toss_sub_test_order", got)
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_billing_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want billing secret auth", got)
		}
		var payload map[string]string
		if err := common.DecodeJson(r.Body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["authKey"] != "auth_test" || payload["customerKey"] != "cust_test" {
			t.Fatalf("unexpected payload %#v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_test","customerKey":"cust_test","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKey(context.Background(), "auth_test", "cust_test", "toss_sub_test_order")
	if err != nil {
		t.Fatalf("issueTossBillingKey error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d want 200", status)
	}
	if issued.BillingKey != "billing_test" || issued.CustomerKey != "cust_test" {
		t.Fatalf("unexpected response %#v", issued)
	}
}

func TestTossBillingIssueResponseUsesVersionedCardFieldsForStorage(t *testing.T) {
	var issued tossBillingIssueResponse
	if err := common.Unmarshal([]byte(`{"billingKey":"billing_test","customerKey":"cust_test","card":{"issuerCode":"61","number":"43301234****123*"}}`), &issued); err != nil {
		t.Fatalf("unmarshal issue response: %v", err)
	}

	if got := issued.cardCompanyForStorage(); got != "61" {
		t.Fatalf("cardCompanyForStorage = %q want issuer code 61", got)
	}
	if got := issued.cardNumberForStorage(); got != "43301234****123*" {
		t.Fatalf("cardNumberForStorage = %q want masked card number", got)
	}
}

func TestIssueTossBillingKeyRetriesTransientFailureWithSameIdempotencyKey(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	attempts := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_retry_order" {
			t.Fatalf("attempt %d Idempotency-Key = %q want toss_sub_retry_order", attempts, got)
		}
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"INTERNAL_SERVER_ERROR"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_retry","customerKey":"cust_retry","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKey(context.Background(), "auth_retry", "cust_retry", "toss_sub_retry_order")
	if err != nil {
		t.Fatalf("issueTossBillingKey error = %v", err)
	}
	if status != http.StatusOK || issued.BillingKey != "billing_retry" {
		t.Fatalf("status=%d issued=%#v", status, issued)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d want 2", attempts)
	}
}

func TestChargeTossBillingEscapesBillingKeyAndRetriesNetworkError(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	attempts := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if got := r.URL.EscapedPath(); got != "/v1/billing/billing%2Fretry%3Fkey" {
			t.Fatalf("escaped path = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_charge_retry" {
			t.Fatalf("Idempotency-Key = %q want toss_sub_charge_retry", got)
		}
		if attempts == 1 {
			return nil, errors.New("temporary network failure")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_retry","orderId":"toss_sub_charge_retry","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, status, err := chargeTossBilling(context.Background(), "billing/retry?key", "cust_retry", "toss_sub_charge_retry", "구독", 15000)
	if err != nil {
		t.Fatalf("chargeTossBilling error = %v", err)
	}
	if status != http.StatusOK || !isValidTossBillingCharge(result, "toss_sub_charge_retry", 15000) {
		t.Fatalf("status=%d result=%#v", status, result)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d want 2", attempts)
	}
}

func TestConfirmTossBillingChargeFallsBackToOrderLookupAfterTransientFailure(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	var postAttempts int
	var lookedUp bool
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_lookup":
			postAttempts++
			return nil, errors.New("response lost")
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/toss_sub_lookup":
			lookedUp = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_lookup","orderId":"toss_sub_lookup","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, err := confirmTossBillingChargeOrLookup(context.Background(), "billing_lookup", "cust_lookup", "toss_sub_lookup", "구독", 15000)
	if err != nil {
		t.Fatalf("confirmTossBillingChargeOrLookup error = %v", err)
	}
	if !isValidTossBillingCharge(result, "toss_sub_lookup", 15000) {
		t.Fatalf("unexpected recovered result %#v", result)
	}
	if postAttempts != tossAPIMaxAttempts {
		t.Fatalf("postAttempts = %d want %d", postAttempts, tossAPIMaxAttempts)
	}
	if !lookedUp {
		t.Fatal("expected fallback order lookup")
	}
}

func TestTossBillingChargeForModelFallsBackToOrderLookupAfterTransientFailure(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	var postAttempts int
	var lookedUp bool
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_test_secret:"))
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("Authorization = %q want active billing secret fallback", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_model_lookup":
			postAttempts++
			return nil, errors.New("response lost")
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/v1/payments/orders/toss_sub_model_lookup":
			lookedUp = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_model_lookup","orderId":"toss_sub_model_lookup","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, err := tossBillingChargeForModel(context.Background(), "billing_model_lookup", "cust_model_lookup", "", "toss_sub_model_lookup", "구독", 15000)
	if err != nil {
		t.Fatalf("tossBillingChargeForModel error = %v", err)
	}
	if result == nil || !result.Done || result.Total != 15000 {
		t.Fatalf("result=%+v want done true total 15000", result)
	}
	if result.PaymentKey != "pay_model_lookup" || !strings.Contains(result.ProviderPayload, "pay_model_lookup") {
		t.Fatalf("result should preserve Toss payment payload, got %+v", result)
	}
	if postAttempts != tossAPIMaxAttempts {
		t.Fatalf("postAttempts = %d want %d", postAttempts, tossAPIMaxAttempts)
	}
	if !lookedUp {
		t.Fatal("expected fallback order lookup")
	}
}

func TestHandleTossBillingActivationFailureMarksKeyPendingWhenRemoteDeleteFails(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_activation_fail", "현대", "433012******1234")
	if err != nil {
		t.Fatalf("StoreTossBillingKey error = %v", err)
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("temporary delete failure")
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	handleTossBillingActivationFailure(context.Background(), 7, "toss_sub_activation_fail", "billing_activation_fail", "sk_test_secret", keyId, 15000, errors.New("db down"))

	var key model.UserBillingKey
	if err := model.DB.First(&key, keyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusPendingRevocation {
		t.Fatalf("status = %q want %q", key.Status, model.BillingKeyStatusPendingRevocation)
	}
}

func TestDeleteTossBillingKeyEscapesBillingKey(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s want DELETE", r.Method)
		}
		if got := r.URL.EscapedPath(); got != "/v1/billing/billing%2Fdelete%3Fkey" {
			t.Fatalf("escaped path = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	status, err := deleteTossBillingKey(context.Background(), "billing/delete?key")
	if err != nil {
		t.Fatalf("deleteTossBillingKey error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d want 200", status)
	}
}

func TestDeleteTossBillingKeyTreatsNotFoundAsAlreadyDeleted(t *testing.T) {
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_BILLING_KEY","message":"not found"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	status, err := deleteTossBillingKey(context.Background(), "already_deleted")
	if !errors.Is(err, model.ErrTossBillingKeyAlreadyDeleted) {
		t.Fatalf("error = %v want ErrTossBillingKeyAlreadyDeleted", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d want 404", status)
	}
}
