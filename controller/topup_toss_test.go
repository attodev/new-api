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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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

func TestIsValidServerAddress(t *testing.T) {
	cases := map[string]bool{
		"https://pay.example.com": true,
		"http://localhost:3000":   true,
		"http://127.0.0.1:3000":   true,
		"http://pay.example.com":  false,
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
	ps := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := ps.ComplianceConfirmed
	originalComplianceTermsVersion := ps.ComplianceTermsVersion
	originalEnabled := setting.TossEnabled
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
		setting.TossEnabled = originalEnabled
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
	setting.TossEnabled = true
	setting.TossBillingEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "ck_live_regular"
	setting.TossSecretKey = "sk_live_regular"
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

	setting.TossBillingClientKey = "ck_live_billing"
	setting.TossBillingSecretKey = "sk_live_billing"
	if !isTossBillingEnabled() {
		t.Fatal("billing should be enabled with positive TossUnitPrice and configured billing keys")
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
	setting.TossClientKey = "ck_live_toss"
	setting.TossSecretKey = "sk_live_toss"
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
	topUp := &model.TopUp{Amount: 13000, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending}
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
}

func TestTossFailRedirectPreservesFailureContext(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.TopUp{}); err != nil {
		t.Fatalf("migrate topup table: %v", err)
	}
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

	setting.TossSecretKey = "sk_test_secret"
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"payment/lookup?key","orderId":"toss_order","status":"DONE","totalAmount":13000,"currency":"KRW"}`)),
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
		CreateTime:         1,
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stored_secret","orderId":"toss_stored_secret","status":"DONE","totalAmount":13000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
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
		CreateTime:      1,
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false
	common.QuotaPerUnit = 1

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/payments/confirm" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_no_card","orderId":"toss_no_card","status":"DONE","totalAmount":13000,"currency":"KRW"}`)),
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
	topUp := model.TopUp{
		UserId:          7,
		Amount:          13000,
		Money:           10,
		TradeNo:         "toss_stale_ready",
		ProviderOrderId: "pay_stale_ready",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      1,
		Status:          common.TopUpStatusPending,
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_ready" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_ready","orderId":"toss_stale_ready","status":"READY","totalAmount":13000,"currency":"KRW"}`)),
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

func TestReconcileTossRecordedTopUpCreditsDonePayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	topUp := model.TopUp{
		UserId:          7,
		Amount:          13000,
		Money:           10,
		TradeNo:         "toss_stale_done",
		ProviderOrderId: "pay_stale_done",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      1,
		Status:          common.TopUpStatusPending,
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false
	common.QuotaPerUnit = 1
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_done" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_done","orderId":"toss_stale_done","status":"DONE","totalAmount":13000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
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

func TestReconcileTossRecordedTopUpLogsPartialCanceledPendingPayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate topup tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user", Group: "default"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	topUp := model.TopUp{
		UserId:          7,
		Amount:          13000,
		Money:           10,
		TradeNo:         "toss_stale_partial_canceled",
		ProviderOrderId: "pay_stale_partial_canceled",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      1,
		Status:          common.TopUpStatusPending,
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

	setting.TossSecretKey = "sk_test_secret"
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/payments/pay_stale_partial_canceled" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_partial_canceled","orderId":"toss_stale_partial_canceled","status":"PARTIAL_CANCELED","totalAmount":13000,"currency":"KRW"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossRecordedTopUp(context.Background(), topUp)
	if err != nil {
		t.Fatalf("reconcileTossRecordedTopUp error = %v", err)
	}
	if !resolved {
		t.Fatal("expected PARTIAL_CANCELED payment to resolve")
	}
	var reloaded model.TopUp
	if err := model.DB.Where("trade_no = ?", "toss_stale_partial_canceled").First(&reloaded).Error; err != nil {
		t.Fatalf("load topup: %v", err)
	}
	if reloaded.Status != common.TopUpStatusFailed {
		t.Fatalf("topup status = %q want failed", reloaded.Status)
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

func TestTossWebhookRechecksCanceledSubscriptionPaymentStatus(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionOrder{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription webhook tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "toss-user"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := (&model.SubscriptionOrder{
		UserId:          7,
		PlanId:          3,
		Money:           15000,
		TradeNo:         "toss_sub_cancel",
		PaymentMethod:   "toss",
		PaymentProvider: model.PaymentProviderToss,
		Status:          common.TopUpStatusSuccess,
	}).Insert(); err != nil {
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

	setting.TossSecretKey = "sk_test_secret"
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_sub_cancel","orderId":"toss_sub_cancel","status":"CANCELED","totalAmount":15000,"currency":"KRW"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_sub_cancel","paymentKey":"pay_sub_cancel","status":"CANCELED"}}`))

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
	if err := (&model.SubscriptionOrder{
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_sub_done_webhook","orderId":"toss_sub_done_webhook","status":"DONE","totalAmount":15000,"currency":"KRW","card":{"company":"현대","number":"433012******1234"}}`)),
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

func setupTossTopUpAmountModeControllerTest(t *testing.T) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	originalTossEnabled := setting.TossEnabled
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	originalServerAddress := system_setting.ServerAddress
	originalMinTopUp := setting.TossMinTopUp
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount

	setting.TossEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "ck_test_toss"
	setting.TossSecretKey = "sk_test_toss"
	system_setting.ServerAddress = "https://pay.example.com"
	setting.TossMinTopUp = 1000
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}

	t.Cleanup(func() {
		setting.TossEnabled = originalTossEnabled
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
		setting.TossTestMode = originalTestMode
		system_setting.ServerAddress = originalServerAddress
		setting.TossMinTopUp = originalMinTopUp
		setting.TossUnitPrice = originalUnitPrice
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
