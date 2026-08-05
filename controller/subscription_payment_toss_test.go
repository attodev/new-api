package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	"github.com/stretchr/testify/require"
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
	originalTossConfig := setting.GetTossConfigSnapshot()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
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
	t.Setenv("CRYPTO_SECRET", "toss-billing-controller-test-secret")
	t.Setenv(model.TossOptionSecretEncryptionEnv, "true")
	common.CryptoSecret = "toss-billing-controller-test-secret"
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":        "true",
		"TossBillingEnabled": "true",
	}, ""))
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	if err := model.DB.AutoMigrate(&model.UserSubscription{}, &model.UserBillingKey{}, &model.WalletAutoRecharge{}, &model.TossPaymentEvent{}); err != nil {
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
		restoreTossConfigForOptionTest(t, originalTossConfig)
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
	})
}

func seedControllerTossRenewalContract(t *testing.T, sub *model.UserSubscription, plan *model.SubscriptionPlan, providerAmount int64) {
	t.Helper()
	require.NotNil(t, sub)
	require.NotNil(t, plan)
	initialOrder := &model.SubscriptionOrder{
		UserId: sub.UserId, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo:       fmt.Sprintf("controller_toss_initial_contract_%d", sub.Id),
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess, ProviderAmount: providerAmount, ProviderCurrency: "KRW",
		BillingKeyId: sub.BillingKeyId,
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(initialOrder, plan))
	require.NoError(t, model.SetTossRenewalContractFromInitialOrder(sub, initialOrder))
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", sub.Id).
		Update("toss_renewal_contract_snapshot", sub.TossRenewalContractSnapshot).Error)
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

func TestTossSubscriptionCheckoutSnapshotFingerprintsAllTerms(t *testing.T) {
	plan := &model.SubscriptionPlan{
		Id: 7, Title: "Pro", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1,
		QuotaResetPeriod: model.SubscriptionResetMonthly, TotalAmount: 5000000,
	}
	base := newTossSubscriptionCheckoutSnapshot(plan, 13000, "KRW")
	require.NotNil(t, base)
	require.Regexp(t, `^[a-f0-9]{40}$`, base.SnapshotFingerprint)

	changedPlan := *plan
	changedPlan.DurationValue = 2
	changedTerms := newTossSubscriptionCheckoutSnapshot(&changedPlan, 13000, "KRW")
	require.NotNil(t, changedTerms)
	require.NotEqual(t, base.SnapshotFingerprint, changedTerms.SnapshotFingerprint)

	changedAmount := newTossSubscriptionCheckoutSnapshot(plan, 14000, "KRW")
	require.NotNil(t, changedAmount)
	require.NotEqual(t, base.SnapshotFingerprint, changedAmount.SnapshotFingerprint)
}

func TestTossSubscriptionCheckoutSnapshotRejectsAmountsOutsideCardContract(t *testing.T) {
	plan := &model.SubscriptionPlan{
		Id: 7, Title: "Pro", PriceAmount: 10, Currency: "USD",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1,
	}
	require.Nil(t, newTossSubscriptionCheckoutSnapshot(plan, setting.TossCardMinimumAmountKRW-1, "KRW"))
	require.Nil(t, newTossSubscriptionCheckoutSnapshot(plan, setting.TossMaximumChargeAmountKRW+1, "KRW"))
	require.NotNil(t, newTossSubscriptionCheckoutSnapshot(plan, setting.TossCardMinimumAmountKRW, "KRW"))
}

func TestTossBillingChargeTimeoutMeetsDocumentedMinimum(t *testing.T) {
	if tossBillingChargeTimeout < 60*time.Second {
		t.Fatalf("tossBillingChargeTimeout = %s want at least 60s", tossBillingChargeTimeout)
	}
}

func TestTossBillingIssueKnownKeyIsCleanableDespiteInvalid2xxMetadata(t *testing.T) {
	err := errors.New("invalid Toss billing issue response: missing card information")
	require.False(t, isTossBillingIssueResponseUncertain(
		&tossBillingIssueResponse{BillingKey: "billing_known_cleanup_key"},
		http.StatusOK,
		err,
	))
	require.True(t, isTossBillingIssueResponseUncertain(nil, http.StatusOK, err))
	require.True(t, isTossBillingIssueResponseUncertain(
		&tossBillingIssueResponse{BillingKey: " invalid billing key "},
		http.StatusOK,
		err,
	))
}

func TestTossBillingIssueRecoveryRetainsSnapshotWhenOriginalCredentialExpired(t *testing.T) {
	err := newTossAPIError(
		http.MethodPost,
		http.StatusUnauthorized,
		[]byte(`{"code":"UNAUTHORIZED_KEY","message":"expired"}`),
	)

	// A rejection returned by the very first provider request proves that no
	// older response was lost and can be closed normally.
	require.False(t, isTossBillingIssueRecoveryUncertain(nil, http.StatusUnauthorized, err, false))
	// Once a previous ISSUE may have reached Toss, the same response proves only
	// that its API-key namespace is no longer readable. The durable authKey must
	// remain available for operator reconciliation instead of being erased.
	require.True(t, isTossBillingIssueRecoveryUncertain(nil, http.StatusUnauthorized, err, true))
}

func TestTossBillingCancellationMustMatchExpectedOrderAndAmount(t *testing.T) {
	result := &tossConfirmResponse{
		PaymentKey:    "pay_cancel_identity",
		Type:          "BILLING",
		OrderId:       "toss_sub_other_order",
		Status:        "CANCELED",
		TotalAmount:   15000,
		BalanceAmount: 0,
		Currency:      "KRW",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 15000, Company: "card", Number: "****1234"},
	}
	_, err := classifyTossBillingChargeResponse(result, "toss_sub_expected_order", 15000)
	require.Error(t, err)
	require.NotErrorIs(t, err, model.ErrTossBillingChargeCanceled)

	result.OrderId = "toss_sub_expected_order"
	result.TotalAmount = 14000
	_, err = classifyTossBillingChargeResponse(result, "toss_sub_expected_order", 15000)
	require.Error(t, err)
	require.NotErrorIs(t, err, model.ErrTossBillingChargeCanceled)
}

func TestSubscriptionTossBillingFailRedirectPreservesFailureContext(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionOrder{}); err != nil {
		t.Fatalf("migrate subscription order table: %v", err)
	}
	order := model.SubscriptionOrder{
		UserId: 101, PlanId: 202, TradeNo: "toss_sub_fail",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderCredential: "encrypted-secret",
	}
	if err := model.DB.Create(&order).Error; err != nil {
		t.Fatalf("create pristine subscription order: %v", err)
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
	if err := model.DB.First(&order, order.Id).Error; err != nil {
		t.Fatalf("reload failed subscription order: %v", err)
	}
	if order.Status != common.TopUpStatusExpired {
		t.Fatalf("failed pristine order status = %q want %q", order.Status, common.TopUpStatusExpired)
	}
	if order.ProviderCredential != "" {
		t.Fatal("failed pristine order should discard its unused provider credential")
	}
}

func TestCancelPendingTossSubscriptionOrderRequiresOwner(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.SubscriptionOrder{}); err != nil {
		t.Fatalf("migrate subscription order table: %v", err)
	}
	order := model.SubscriptionOrder{
		UserId: 301, PlanId: 302, TradeNo: "toss_sub_cancel_owner",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	if err := model.DB.Create(&order).Error; err != nil {
		t.Fatalf("create subscription order: %v", err)
	}

	gin.SetMode(gin.TestMode)
	forbiddenRecorder := httptest.NewRecorder()
	forbidden, _ := gin.CreateTestContext(forbiddenRecorder)
	forbidden.Set("id", order.UserId+1)
	forbidden.Params = gin.Params{{Key: "trade_no", Value: order.TradeNo}}
	forbidden.Request = httptest.NewRequest(http.MethodDelete, "/api/subscription/toss/pending/"+order.TradeNo, nil)
	CancelPendingTossSubscriptionOrder(forbidden)
	var forbiddenResponse struct {
		Success bool `json:"success"`
	}
	if err := common.Unmarshal(forbiddenRecorder.Body.Bytes(), &forbiddenResponse); err != nil {
		t.Fatalf("decode foreign cancellation response: %v", err)
	}
	if forbiddenResponse.Success {
		t.Fatalf("another user unexpectedly cancelled the reservation: %s", forbiddenRecorder.Body.String())
	}
	if err := model.DB.First(&order, order.Id).Error; err != nil {
		t.Fatalf("reload subscription order: %v", err)
	}
	if order.Status != common.TopUpStatusPending {
		t.Fatalf("order status after foreign cancellation = %q want pending", order.Status)
	}

	ownerRecorder := httptest.NewRecorder()
	owner, _ := gin.CreateTestContext(ownerRecorder)
	owner.Set("id", order.UserId)
	owner.Params = gin.Params{{Key: "trade_no", Value: order.TradeNo}}
	owner.Request = httptest.NewRequest(http.MethodDelete, "/api/subscription/toss/pending/"+order.TradeNo, nil)
	CancelPendingTossSubscriptionOrder(owner)
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("owner cancellation status = %d body=%s", ownerRecorder.Code, ownerRecorder.Body.String())
	}
	if err := model.DB.First(&order, order.Id).Error; err != nil {
		t.Fatalf("reload cancelled subscription order: %v", err)
	}
	if order.Status != common.TopUpStatusExpired {
		t.Fatalf("owner-cancelled status = %q want expired", order.Status)
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
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_snapshot","type":"BILLING","orderId":"toss_sub_snapshot","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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

func TestSubscriptionTossBillingConfirmKeepsRecoverableOrderWhenBillingKeyIssueResponseMayBeLost(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.TopUp{}, &model.Log{}); err != nil {
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
		UserId:                7,
		PlanId:                plan.Id,
		Money:                 10,
		TradeNo:               "toss_sub_issue_lost",
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		CreateTime:            common.GetTimestamp(),
		ProviderAmount:        15000,
		ProviderCurrency:      "KRW",
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("ck_issue_lost_original"),
	}).Insert(); err != nil {
		t.Fatalf("insert subscription order: %v", err)
	}

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
	setting.TossBillingClientKey = "ck_issue_lost_original"
	setting.TossBillingSecretKey = "sk_fallback_secret"
	setting.TossTestMode = false

	var attempts int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v1/billing/authorizations/issue" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_issue_lost_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("initial issue Authorization = %q want order credential", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_issue_lost" {
			t.Fatalf("initial issue Idempotency-Key = %q want original trade_no", got)
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
	if order.Status != common.TopUpStatusPending {
		t.Fatalf("order status = %q want pending for same-idempotency issue recovery", order.Status)
	}
	if order.CompleteTime != 0 {
		t.Fatalf("order complete_time = %d want unset", order.CompleteTime)
	}
	if order.BillingClaimToken == "" || order.BillingClaimTime == 0 {
		t.Fatalf("billing claim should be retained after uncertain response: token=%q time=%d", order.BillingClaimToken, order.BillingClaimTime)
	}
	if !order.BillingIssueAttempted || order.BillingIssueAuthKey == "" || order.BillingIssueAuthKeyHash == "" {
		t.Fatalf("recoverable issue snapshot is incomplete: %#v", order)
	}
	if strings.Contains(order.BillingIssueAuthKey, "auth_lost") {
		t.Fatal("authKey must be encrypted at rest")
	}
	serialized, err := common.Marshal(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if strings.Contains(string(serialized), "auth_lost") || strings.Contains(string(serialized), "billing_issue_auth") {
		t.Fatalf("sensitive issue snapshot leaked through JSON: %s", serialized)
	}
	originalClaimToken := order.BillingClaimToken
	originalAuthHash := order.BillingIssueAuthKeyHash
	forgedRecorder := httptest.NewRecorder()
	forgedContext, _ := gin.CreateTestContext(forgedRecorder)
	forgedContext.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=forged_auth&customerKey=forged_customer&trade_no=toss_sub_issue_lost", nil)
	SubscriptionTossBillingConfirm(forgedContext)
	order = model.GetSubscriptionOrderByTradeNo("toss_sub_issue_lost")
	if order == nil || order.Status != common.TopUpStatusPending || order.BillingClaimToken != originalClaimToken || order.BillingIssueAuthKeyHash != originalAuthHash {
		t.Fatalf("forged callback changed recoverable issue snapshot: %#v", order)
	}
	if attempts != tossAPIMaxAttempts {
		t.Fatalf("forged callback unexpectedly reached provider: attempts=%d", attempts)
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
			strings.Contains(log.Content, "automatic same-order recovery scheduled") &&
			strings.Contains(log.Content, "toss_sub_issue_lost") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected uncertain billing-key recovery log, got %#v", logs)
	}

	// Simulate a different node taking over after the claim TTL and after the
	// active Toss secret has rotated. Recovery must use the stored issue-time
	// credential, the same one-time authKey, and the same orderId idempotency key.
	if err := model.DB.Model(&model.SubscriptionOrder{}).
		Where("trade_no = ?", "toss_sub_issue_lost").
		Update("billing_claim_time", common.GetTimestamp()-10*60).Error; err != nil {
		t.Fatalf("age issue claim: %v", err)
	}
	setting.TossBillingClientKey = "ck_issue_rotated_active"
	setting.TossBillingSecretKey = "sk_rotated_active_secret"
	var recoveryIssueCalls, recoveryChargeCalls int
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("sk_issue_lost_secret:"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("recovery Authorization = %q want stored issue-time credential", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_issue_lost" {
			t.Fatalf("recovery Idempotency-Key = %q want original trade_no", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/authorizations/issue":
			recoveryIssueCalls++
			var payload map[string]string
			if err := common.DecodeJson(r.Body, &payload); err != nil {
				t.Fatalf("decode recovery issue payload: %v", err)
			}
			if payload["authKey"] != "auth_lost" || payload["customerKey"] != "cust_lost" {
				t.Fatalf("unexpected recovery issue payload %#v", payload)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"billingKey":"billing_issue_recovered","customerKey":"cust_lost","card":{"company":"현대","number":"433012******1234"}}`)),
			}, nil
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/v1/billing/billing_issue_recovered":
			recoveryChargeCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_issue_recovered","type":"BILLING","orderId":"toss_sub_issue_lost","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
			}, nil
		default:
			t.Fatalf("unexpected recovery request %s %s", r.Method, r.URL.EscapedPath())
			return nil, nil
		}
	})}
	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	if err != nil {
		t.Fatalf("recover lost billing-key issue response: %v", err)
	}
	if !resolved || recoveryIssueCalls != 1 || recoveryChargeCalls != 1 {
		t.Fatalf("recovery result resolved=%t issue_calls=%d charge_calls=%d", resolved, recoveryIssueCalls, recoveryChargeCalls)
	}
	order = model.GetSubscriptionOrderByTradeNo("toss_sub_issue_lost")
	if order == nil || order.Status != common.TopUpStatusSuccess {
		t.Fatalf("recovered order = %#v want success", order)
	}
	if order.BillingIssueAuthKey != "" || order.BillingIssueAuthKeyHash != "" || order.BillingIssueCustomerKey != "" || order.BillingIssueAttempted {
		t.Fatalf("successful recovery must clear issue snapshot: %#v", order)
	}
	if order.BillingClaimToken != "" || order.BillingClaimTime != 0 {
		t.Fatalf("successful recovery must release claim: token=%q time=%d", order.BillingClaimToken, order.BillingClaimTime)
	}
	var storedKey model.UserBillingKey
	if err := model.DB.First(&storedKey, order.BillingKeyId).Error; err != nil {
		t.Fatalf("load recovered billing key: %v", err)
	}
	if storedKey.ProviderClientKeyHash != model.TossBillingClientKeyFingerprint("ck_issue_lost_original") {
		t.Fatalf("recovered billing key MID fingerprint = %q want issue-time namespace", storedKey.ProviderClientKeyHash)
	}
}

func TestSubscriptionTossBillingConfirmDoesNotIssueWhenAnotherNodeOwnsClaim(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}))
	require.NoError(t, model.DB.Create(&model.User{
		Id:              7,
		Username:        "toss-claim-user",
		Status:          common.UserStatusEnabled,
		TossCustomerKey: "cust_claim",
	}).Error)
	plan := &model.SubscriptionPlan{
		Id:            3311,
		Title:         "Claimed Plan",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:           7,
		PlanId:           plan.Id,
		Money:            10,
		TradeNo:          "toss_sub_claimed_elsewhere",
		PaymentMethod:    model.PaymentMethodToss,
		PaymentProvider:  model.PaymentProviderToss,
		Status:           common.TopUpStatusPending,
		ProviderAmount:   15000,
		ProviderCurrency: "KRW",
	}).Error)
	claimToken, claimed, err := model.ClaimTossSubscriptionBillingOrder("toss_sub_claimed_elsewhere")
	require.NoError(t, err)
	require.True(t, claimed)

	originalIssuer := subscriptionTossBillingKeyIssuer
	issuerCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		return nil, 0, errors.New("issuer must not be called")
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_claim&customerKey=cust_claim&trade_no=toss_sub_claimed_elsewhere", nil)
	SubscriptionTossBillingConfirm(c)
	require.Zero(t, issuerCalls)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.Where("trade_no = ?", "toss_sub_claimed_elsewhere").First(&order).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.Equal(t, claimToken, order.BillingClaimToken)
}

func TestSubscriptionTossBillingConfirmDoesNotChargeWhenUserDisabledDuringIssue(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.TopUp{},
		&model.Log{},
	))
	require.NoError(t, model.DB.Create(&model.User{
		Id:              7,
		Username:        "toss-disable-during-issue",
		Status:          common.UserStatusEnabled,
		TossCustomerKey: "cust_disable_during_issue",
	}).Error)
	plan := &model.SubscriptionPlan{
		Id:            3320,
		Title:         "Disable During Issue",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential("test_sk_disable_during_issue")
	require.NoError(t, err)
	tradeNo := "toss_sub_disable_during_issue"
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                7,
		PlanId:                plan.Id,
		Money:                 plan.PriceAmount,
		TradeNo:               tradeNo,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
		CreateTime:            common.GetTimestamp(),
		ProviderAmount:        15000,
		ProviderCurrency:      "KRW",
		ProviderCredential:    credential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("test_ck_disable_during_issue"),
	}).Error)

	originalIssuer := subscriptionTossBillingKeyIssuer
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.NoError(t, model.UpdateUserFieldsWithBillingLifecycle(7, map[string]interface{}{
			"status": common.UserStatusDisabled,
		}))
		issued := &tossBillingIssueResponse{
			BillingKey:  "billing_disable_during_issue",
			CustomerKey: customerKey,
		}
		issued.Card.Company = "현대"
		issued.Card.Number = "433012******1234"
		return issued, http.StatusOK, nil
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	deleteCalls := 0
	chargeCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodDelete && r.URL.EscapedPath() == "/v1/billing/billing_disable_during_issue":
			deleteCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		case r.Method == http.MethodPost:
			chargeCalls++
			return nil, errors.New("first charge must not be called")
		default:
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL.EscapedPath())
		}
	})}
	tossAPIBase = "https://api.test.tosspayments.local"
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_disable_during_issue&customerKey=cust_disable_during_issue&trade_no="+tradeNo, nil)
	SubscriptionTossBillingConfirm(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	require.Zero(t, chargeCalls)
	require.Equal(t, 1, deleteCalls)
	order, err := model.GetSubscriptionOrderByTradeNoWithError(tradeNo)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Zero(t, order.BillingKeyId)
	var activeKeys int64
	require.NoError(t, model.DB.Model(&model.UserBillingKey{}).
		Where("user_id = ? AND status = ?", 7, model.BillingKeyStatusActive).
		Count(&activeKeys).Error)
	require.Zero(t, activeKeys)
	var key model.UserBillingKey
	require.NoError(t, model.DB.Where("user_id = ?", 7).First(&key).Error)
	require.Equal(t, model.BillingKeyStatusRevoked, key.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 7).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionTossBillingConfirmDoesNotIssueWithEphemeralCryptoSecret(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	originalCryptoSecret := common.CryptoSecret
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")
	common.CryptoSecret = "ephemeral-process-secret"
	t.Cleanup(func() { common.CryptoSecret = originalCryptoSecret })

	originalIssuer := subscriptionTossBillingKeyIssuer
	issuerCalls := 0
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issuerCalls++
		return nil, 0, errors.New("issuer must not be called")
	}
	t.Cleanup(func() { subscriptionTossBillingKeyIssuer = originalIssuer })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_ephemeral&customerKey=cust_ephemeral&trade_no=toss_sub_ephemeral", nil)
	SubscriptionTossBillingConfirm(c)

	require.Zero(t, issuerCalls)
	require.Equal(t, http.StatusFound, recorder.Code)
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
	if key.CustomerKey != "cust_expected" {
		t.Fatalf("billing key customer_key = %q want request customer key", key.CustomerKey)
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
	if !order.BillingAttempted || order.BillingAttemptCredential == "" {
		t.Fatal("expected exact first-charge attempt credential to remain for same-order recovery")
	}
	attemptSecret, err := model.DecryptProviderCredential(order.BillingAttemptCredential)
	if err != nil {
		t.Fatalf("decrypt first-charge attempt credential: %v", err)
	}
	if attemptSecret != "sk_processing_secret" {
		t.Fatalf("first-charge attempt secret = %q want issue-time secret", attemptSecret)
	}
	var key model.UserBillingKey
	if err := model.DB.First(&key, order.BillingKeyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	if key.Status != model.BillingKeyStatusActive {
		t.Fatalf("billing key status = %q want active", key.Status)
	}
}

func TestSubscriptionTossBillingConfirmKeepsDonePayloadMismatchRecoverable(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "mismatch-user", Status: common.UserStatusEnabled, TossCustomerKey: "cust_mismatch"}).Error)
	plan := &model.SubscriptionPlan{Id: 3391, Title: "Mismatch Plan", PriceAmount: 10, Currency: "USD", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	credential, err := model.EncryptProviderCredential("test_sk_mismatch")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 7, PlanId: plan.Id, Money: 10, TradeNo: "toss_sub_done_mismatch", PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending, ProviderAmount: 15000,
		CreateTime: common.GetTimestamp(), ProviderCurrency: "KRW", ProviderCredential: credential,
	}).Error)

	issued := &tossBillingIssueResponse{BillingKey: "billing_done_mismatch", CustomerKey: "cust_mismatch"}
	issued.Card.Company = "현대"
	issued.Card.Number = "433012******1234"
	originalIssuer := subscriptionTossBillingKeyIssuer
	subscriptionTossBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		return issued, http.StatusOK, nil
	}
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		subscriptionTossBillingKeyIssuer = originalIssuer
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"paymentKey":"pay_done_mismatch","type":"BILLING","orderId":"toss_sub_done_mismatch","status":"DONE","totalAmount":14000,"balanceAmount":14000,"currency":"KRW","method":"카드","card":{"amount":14000,"company":"현대","number":"433012******1234"}}`))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/toss/confirm?authKey=auth_mismatch&customerKey=cust_mismatch&trade_no=toss_sub_done_mismatch", nil)
	SubscriptionTossBillingConfirm(c)

	order := model.GetSubscriptionOrderByTradeNo("toss_sub_done_mismatch")
	require.NotNil(t, order)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, order.BillingKeyId).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", order.TradeNo, model.TossPaymentEventTypeFinancialMismatch).First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
	created, err := persistTossFinancialMismatchEvent(&tossConfirmResponse{
		PaymentKey:    "pay_done_mismatch",
		OrderId:       order.TradeNo,
		Status:        "DONE",
		TotalAmount:   14000,
		BalanceAmount: 14000,
	}, "webhook redelivery")
	require.NoError(t, err)
	require.False(t, created)
	var mismatchCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ? AND event_type = ?", order.TradeNo, model.TossPaymentEventTypeFinancialMismatch).Count(&mismatchCount).Error)
	require.Equal(t, int64(1), mismatchCount)
}

func TestTossSubscriptionPartialCancellationMismatchesKeepDistinctSnapshots(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	const (
		orderID    = "toss_sub_partial_mismatch_snapshots"
		paymentKey = "pay_sub_partial_mismatch_snapshots"
	)
	for _, balance := range []int64{10000, 5000} {
		err := recordTossSubscriptionFinancialMismatchEvent(
			context.Background(),
			orderID,
			paymentKey,
			"PARTIAL_CANCELED",
			15000,
			balance,
			`{"status":"PARTIAL_CANCELED"}`,
			"malformed cancellation snapshot",
		)
		require.NoError(t, err)
	}
	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFinancialMismatch).Order("balance_amount desc").Find(&events).Error)
	require.Len(t, events, 2)
	require.NotEqual(t, events[0].EventKey, events[1].EventKey)
}

func TestReconcileTossPendingRenewalKeepsDonePayloadMismatchPending(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}))
	require.NoError(t, model.DB.Create(&model.User{
		Id: 7, Username: "stale-mismatch-user", Status: common.UserStatusEnabled, AffCode: "stale-mismatch-user",
	}).Error)
	plan := &model.SubscriptionPlan{
		Id:            3,
		Title:         "Pro",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	providerClientKey := "test_ck_stale_mismatch"
	providerSecret := "test_sk_stale_mismatch"
	keyID, err := model.StoreTossBillingKeyWithProviderSnapshot(
		7,
		"cust_stale_mismatch",
		"billing_stale_mismatch",
		"현대",
		"433012******1234",
		providerSecret,
		model.TossBillingClientKeyFingerprint(providerClientKey),
	)
	require.NoError(t, err)
	credential, err := model.EncryptProviderCredential(providerSecret)
	require.NoError(t, err)
	now := model.GetDBTimestamp()
	nextBillingTime := now
	tradeNo := fmt.Sprintf("toss_sub_renew_11_%d", nextBillingTime)
	sub := &model.UserSubscription{
		Id: 11, UserId: 7, PlanId: plan.Id, Status: "active",
		StartTime: now - 3600, EndTime: now + 3600, AutoRenew: true,
		BillingKeyId: keyID, NextBillingTime: nextBillingTime,
	}
	require.NoError(t, model.DB.Create(sub).Error)
	seedControllerTossRenewalContract(t, sub, plan, 15000)
	order := model.SubscriptionOrder{UserId: 7, PlanId: 3, Money: 10, TradeNo: tradeNo, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending, ProviderAmount: 15000, ProviderCurrency: "KRW",
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(providerClientKey), BillingKeyId: keyID,
		BillingChargeProtocolVersion: 1, RenewalEndTime: sub.EndTime}
	require.NoError(t, model.SetTossRenewalOrderSnapshotFromContract(&order, sub))
	require.NoError(t, model.DB.Create(&order).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossBillingClientKey = providerClientKey
	setting.TossBillingSecretKey = providerSecret
	setting.TossTestMode = false
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"paymentKey":"pay_stale_mismatch","type":"BILLING","orderId":%q,"status":"DONE","totalAmount":14000,"balanceAmount":14000,"currency":"KRW","method":"카드","card":{"amount":14000,"company":"현대","number":"433012******1234"}}`, tradeNo)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.ErrorContains(t, err, "reconciliation required")
	require.False(t, resolved)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", order.TradeNo, model.TossPaymentEventTypeFinancialMismatch).First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
}

func TestReconcileSuccessfulSubscriptionDoesNotAutoResolveLegacyMismatchEvent(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}, &model.TossPaymentEvent{}))
	order := model.SubscriptionOrder{
		UserId:          71,
		PlanId:          91,
		TradeNo:         "toss_sub_success_legacy_mismatch",
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(&order).Error)
	created, err := model.RecordTossPaymentEvent(&model.TossPaymentEvent{
		EventKey:             "legacy-misclassified-subscription-mismatch",
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              order.TradeNo,
		PaymentKey:           "pay_sub_success_legacy_mismatch",
		Status:               "DONE",
		OriginalAmount:       14000,
		BalanceAmount:        14000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.True(t, created)

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), order)
	require.NoError(t, err)
	require.True(t, resolved)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", "legacy-misclassified-subscription-mismatch").First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
}

type claimedTossRenewalFixture struct {
	Order       model.SubscriptionOrder
	KeyID       int
	OldEndTime  int64
	ExactSecret string
}

func setupClaimedTossRenewalFixture(t *testing.T, suffix string) claimedTossRenewalFixture {
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

	originalTestMode := setting.TossTestMode
	originalBillingClient := setting.TossBillingClientKey
	originalBillingSecret := setting.TossBillingSecretKey
	originalUnitPrice := setting.TossUnitPrice
	setting.TossTestMode = false
	setting.TossBillingClientKey = "test_ck_exact_recovery_mid"
	setting.TossBillingSecretKey = "test_sk_stored_before_rotation"
	setting.TossUnitPrice = 1500
	t.Cleanup(func() {
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalBillingClient
		setting.TossBillingSecretKey = originalBillingSecret
		setting.TossUnitPrice = originalUnitPrice
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       7,
		Username: "exact-renewal-" + suffix,
		Status:   common.UserStatusEnabled,
		AffCode:  "exact-renewal-aff-" + suffix,
	}).Error)
	plan := &model.SubscriptionPlan{
		Id:            3340,
		Title:         "Exact Recovery",
		PriceAmount:   10,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationCustom,
		CustomSeconds: 3600,
		Enabled:       true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	keyID, err := model.StoreTossBillingKeyWithSecret(
		7,
		"cust_exact_renewal",
		"billing_exact_renewal_"+suffix,
		"현대",
		"433012******1234",
		"test_sk_stored_before_rotation",
	)
	require.NoError(t, err)
	// The current configuration is deliberately different from both the order
	// snapshot and the exact secret recorded immediately before the lost POST.
	setting.TossBillingSecretKey = "test_sk_active_after_second_rotation"

	now := common.GetTimestamp()
	oldEndTime := now + 3600
	sub := &model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          plan.Id,
		Status:          "active",
		StartTime:       now - 3600,
		EndTime:         oldEndTime,
		AutoRenew:       true,
		BillingKeyId:    keyID,
		NextBillingTime: 1500,
	}
	require.NoError(t, model.DB.Create(sub).Error)
	seedControllerTossRenewalContract(t, sub, plan, 15000)
	providerCredential, err := model.EncryptProviderCredential("test_sk_stored_before_rotation")
	require.NoError(t, err)
	exactSecret := "test_sk_exact_lost_post_" + suffix
	attemptCredential, err := model.EncryptProviderCredential(exactSecret)
	require.NoError(t, err)
	order := model.SubscriptionOrder{
		UserId:                   7,
		PlanId:                   plan.Id,
		Money:                    plan.PriceAmount,
		TradeNo:                  "toss_sub_renew_11_1500",
		PaymentMethod:            model.PaymentMethodToss,
		PaymentProvider:          model.PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		CreateTime:               now - 600,
		ProviderAmount:           15000,
		ProviderCurrency:         "KRW",
		ProviderCredential:       providerCredential,
		BillingKeyId:             keyID,
		BillingClaimToken:        "stale-owner",
		BillingClaimTime:         now - 600,
		BillingAttempted:         true,
		BillingAttemptCredential: attemptCredential,
		RenewalEndTime:           oldEndTime,
	}
	require.NoError(t, model.SetTossRenewalOrderSnapshotFromContract(&order, sub))
	require.NoError(t, model.DB.Create(&order).Error)
	return claimedTossRenewalFixture{Order: order, KeyID: keyID, OldEndTime: oldEndTime, ExactSecret: exactSecret}
}

func TestReconcileTossPendingRenewalUsesExactAttemptSecretAfterResponseLoss(t *testing.T) {
	fixture := setupClaimedTossRenewalFixture(t, "done")
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	requestCount := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, http.MethodGet, r.Method, "cleanup must GET before any repeated POST")
		require.Equal(t, "/v1/payments/orders/"+fixture.Order.TradeNo, r.URL.EscapedPath())
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixture.ExactSecret+":"))
		require.Equal(t, wantAuth, r.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_exact_recovery","type":"BILLING","orderId":"toss_sub_renew_11_1500","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, requestCount)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
	require.Equal(t, common.TopUpStatusSuccess, order.Status)
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.Greater(t, sub.EndTime, fixture.OldEndTime)
}

func TestReconcileTossPendingRenewalCancellationStopsFutureCharges(t *testing.T) {
	cases := []struct {
		name          string
		status        string
		cancelAmount  int64
		balanceAmount int64
	}{
		{name: "full", status: "CANCELED", cancelAmount: 15000, balanceAmount: 0},
		{name: "partial", status: "PARTIAL_CANCELED", cancelAmount: 10000, balanceAmount: 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := setupClaimedTossRenewalFixture(t, tc.name)
			originalBase := tossAPIBase
			originalClient := http.DefaultClient
			requestCount := 0
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestCount++
				require.Equal(t, http.MethodGet, r.Method)
				wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(fixture.ExactSecret+":"))
				require.Equal(t, wantAuth, r.Header.Get("Authorization"))
				body := fmt.Sprintf(
					`{"paymentKey":"pay_exact_cancel_%s","type":"BILLING","orderId":"%s","status":"%s","totalAmount":15000,"balanceAmount":%d,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"},"cancels":[{"cancelAmount":%d,"refundableAmount":%d,"canceledAt":"2026-07-10T12:00:00+09:00","transactionKey":"cancel_exact_%s","cancelStatus":"DONE"}]}`,
					tc.name,
					fixture.Order.TradeNo,
					tc.status,
					tc.balanceAmount,
					tc.cancelAmount,
					tc.balanceAmount,
					tc.name,
				)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			})}
			tossAPIBase = "https://api.test.tosspayments.local"
			t.Cleanup(func() {
				tossAPIBase = originalBase
				http.DefaultClient = originalClient
			})

			resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), fixture.Order)
			require.NoError(t, err)
			require.True(t, resolved)
			require.Equal(t, 1, requestCount)

			var order model.SubscriptionOrder
			require.NoError(t, model.DB.First(&order, fixture.Order.Id).Error)
			require.Equal(t, common.TopUpStatusFailed, order.Status)
			require.Contains(t, order.ProviderPayload, tc.status)
			var sub model.UserSubscription
			require.NoError(t, model.DB.First(&sub, 11).Error)
			require.False(t, sub.AutoRenew)
			var key model.UserBillingKey
			require.NoError(t, model.DB.First(&key, fixture.KeyID).Error)
			require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
			var event model.TossPaymentEvent
			require.NoError(t, model.DB.Where(
				"order_id = ? AND event_type = ?",
				fixture.Order.TradeNo,
				model.TossPaymentEventTypeCancellation,
			).First(&event).Error)
			require.Equal(t, tc.cancelAmount, event.CancelAmount)
			require.Equal(t, tc.balanceAmount, event.BalanceAmount)
			require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)

			// Auto-renew is terminally disabled; another billing tick cannot create
			// a fresh orderId or call Toss again for the canceled period.
			require.NoError(t, model.ProcessTossRenewal(context.Background(), 11, model.TossBillingMaxFails))
			require.Equal(t, 1, requestCount)
		})
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
	if err := model.SetTossSubscriptionOrderPlanSnapshot(order, plan); err != nil {
		t.Fatalf("snapshot subscription order: %v", err)
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_reconcile_done","type":"BILLING","orderId":"toss_sub_reconcile_done","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}, &model.TopUp{}, &model.Log{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "renewal-reconcile-user", Status: common.UserStatusEnabled, AffCode: "renewal-reconcile-user"}).Error; err != nil {
		t.Fatalf("create renewal user: %v", err)
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
	subscription := &model.UserSubscription{
		Id:              11,
		UserId:          7,
		PlanId:          plan.Id,
		Status:          "active",
		StartTime:       1000,
		EndTime:         oldEndTime,
		AutoRenew:       true,
		BillingKeyId:    billingKeyId,
		NextBillingTime: oldNextBillingTime,
	}
	if err := model.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	seedControllerTossRenewalContract(t, subscription, plan, 15000)
	credential, err := model.EncryptProviderCredential("sk_reconcile_renewal")
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
	}
	tradeNo := "toss_sub_renew_11_1500"
	order := &model.SubscriptionOrder{
		UserId:                   7,
		PlanId:                   plan.Id,
		Money:                    10,
		TradeNo:                  tradeNo,
		PaymentMethod:            model.PaymentMethodToss,
		PaymentProvider:          model.PaymentProviderToss,
		Status:                   common.TopUpStatusPending,
		CreateTime:               1000,
		ProviderAmount:           15000,
		ProviderCurrency:         "KRW",
		ProviderCredential:       credential,
		BillingKeyId:             billingKeyId,
		BillingAttempted:         true,
		BillingAttemptCredential: credential,
		RenewalEndTime:           oldEndTime,
	}
	if err := model.SetTossRenewalOrderSnapshotFromContract(order, subscription); err != nil {
		t.Fatalf("snapshot renewal order: %v", err)
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
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_reconcile_renewal","type":"BILLING","orderId":"toss_sub_renew_11_1500","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	settlementStarted := model.GetDBTimestamp()
	resolved, err := reconcileTossPendingSubscriptionOrder(context.Background(), *order)
	settlementFinished := model.GetDBTimestamp()
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
	if sub.EndTime < settlementStarted+3600 || sub.EndTime > settlementFinished+3600 {
		t.Fatalf("subscription end_time = %d want one full hour from settlement [%d,%d]", sub.EndTime, settlementStarted+3600, settlementFinished+3600)
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
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}); err != nil {
		t.Fatalf("migrate subscription billing tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "stale-renewal-user", Status: common.UserStatusEnabled, AffCode: "stale-renewal-user"}).Error; err != nil {
		t.Fatalf("create renewal user: %v", err)
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
	subscription := &model.UserSubscription{
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
	}
	if err := model.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	seedControllerTossRenewalContract(t, subscription, plan, 15000)
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
		// This is a current v1 order whose durable false marker proves that no
		// provider POST was issued. The stale worker may therefore GET once and
		// close it after an authoritative 404.
		BillingChargeProtocolVersion: 1,
		RenewalEndTime:               subscription.EndTime,
	}
	if err := model.SetTossRenewalOrderSnapshotFromContract(order, subscription); err != nil {
		t.Fatalf("snapshot renewal order: %v", err)
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
	originalTossConfig := setting.GetTossConfigSnapshot()
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		restoreTossConfigForOptionTest(t, originalTossConfig)
		system_setting.ServerAddress = originalServerAddress
	})

	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossBillingEnabled":   "true",
		"TossTestMode":         "false",
		"TossClientKey":        "live_ck_regular",
		"TossSecretKey":        "live_sk_regular",
		"TossBillingClientKey": "live_ck_billing",
		"TossBillingSecretKey": "live_sk_billing",
		"TossUnitPrice":        "50",
	}))
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
	if err := model.DB.Create(&model.User{
		Id: 7, Username: "toss-user", Group: "default",
		TossCustomerKey: "cust_" + strings.Repeat("a", tossSDKCustomerKeyMaxBytes),
	}).Error; err != nil {
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
	originalTossConfig := setting.GetTossConfigSnapshot()
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		ps.ComplianceConfirmed = originalComplianceConfirmed
		ps.ComplianceTermsVersion = originalComplianceTermsVersion
		restoreTossConfigForOptionTest(t, originalTossConfig)
		system_setting.ServerAddress = originalServerAddress
	})

	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossBillingEnabled":   "true",
		"TossTestMode":         "false",
		"TossClientKey":        "live_ck_regular",
		"TossSecretKey":        "live_sk_regular",
		"TossBillingClientKey": "live_ck_billing",
		"TossBillingSecretKey": "live_sk_old_billing",
		"TossUnitPrice":        "1300",
	}))
	system_setting.ServerAddress = "https://example.com"

	// A legacy server-side billing key can retain a customerKey wider than the
	// SDK v2 initializer accepts. Starting a new card-registration session must
	// fail before reserving an order, while clearing the legacy value lets the
	// normal random 37-character key be generated for the next request.
	invalidRecorder := httptest.NewRecorder()
	invalidContext, _ := gin.CreateTestContext(invalidRecorder)
	invalidContext.Set("id", 7)
	invalidContext.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/toss/pay", strings.NewReader(`{"plan_id":3}`))
	SubscriptionRequestTossBilling(invalidContext)
	var invalidResponse struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(invalidRecorder.Body.Bytes(), &invalidResponse))
	require.False(t, invalidResponse.Success)
	var invalidOrderCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&invalidOrderCount).Error)
	require.Zero(t, invalidOrderCount)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("toss_customer_key", "").Error)

	gin.SetMode(gin.TestMode)
	plansRecorder := httptest.NewRecorder()
	plansContext, _ := gin.CreateTestContext(plansRecorder)
	GetSubscriptionPlans(plansContext)
	var plansResp struct {
		Success bool                  `json:"success"`
		Data    []SubscriptionPlanDTO `json:"data"`
	}
	if err := common.Unmarshal(plansRecorder.Body.Bytes(), &plansResp); err != nil {
		t.Fatalf("decode public plans response: %v", err)
	}
	if !plansResp.Success || len(plansResp.Data) != 1 || plansResp.Data[0].TossCheckout == nil {
		t.Fatalf("expected public Toss checkout snapshot, got %q", plansRecorder.Body.String())
	}
	publicCheckout := plansResp.Data[0].TossCheckout

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
			TradeNo      string                            `json:"trade_no"`
			SuccessURL   string                            `json:"success_url"`
			FailURL      string                            `json:"fail_url"`
			TossCheckout *TossSubscriptionCheckoutSnapshot `json:"toss_checkout"`
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
	if resp.Data.TossCheckout == nil {
		t.Fatal("expected order-bound Toss checkout snapshot")
	}
	if resp.Data.TossCheckout.PlanId != plan.Id || resp.Data.TossCheckout.PlanTitle != plan.Title ||
		resp.Data.TossCheckout.PriceAmount != plan.PriceAmount || resp.Data.TossCheckout.PriceCurrency != plan.Currency {
		t.Fatalf("unexpected plan checkout snapshot: %+v", resp.Data.TossCheckout)
	}
	if resp.Data.TossCheckout.ProviderAmount != 13000 || resp.Data.TossCheckout.ProviderCurrency != "KRW" {
		t.Fatalf("unexpected provider checkout snapshot: %+v", resp.Data.TossCheckout)
	}
	if resp.Data.TossCheckout.SnapshotFingerprint != publicCheckout.SnapshotFingerprint {
		t.Fatalf("order/public snapshot fingerprint mismatch: order=%q public=%q", resp.Data.TossCheckout.SnapshotFingerprint, publicCheckout.SnapshotFingerprint)
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
	if secret != "live_sk_old_billing" {
		t.Fatalf("stored billing credential = %q want live_sk_old_billing", secret)
	}
	if order.ProviderClientKeyHash != model.TossBillingClientKeyFingerprint("live_ck_billing") {
		t.Fatalf("stored billing client fingerprint = %q want issue-time billing MID", order.ProviderClientKeyHash)
	}
	if order.ProviderAmount != 13000 {
		t.Fatalf("provider amount = %d want 13000", order.ProviderAmount)
	}
	if order.ProviderCurrency != "KRW" {
		t.Fatalf("provider currency = %q want KRW", order.ProviderCurrency)
	}
	if strings.TrimSpace(order.PlanSnapshot) == "" {
		t.Fatal("expected immutable plan snapshot on new Toss subscription order")
	}
	snapshotPlan, err := model.ResolveTossSubscriptionOrderPlan(&order)
	if err != nil {
		t.Fatalf("resolve stored plan snapshot: %v", err)
	}
	if snapshotPlan.Id != plan.Id || snapshotPlan.PriceAmount != plan.PriceAmount {
		t.Fatalf("snapshot plan id/price = %d/%.2f want %d/%.2f", snapshotPlan.Id, snapshotPlan.PriceAmount, plan.Id, plan.PriceAmount)
	}
}

func TestIsValidTossBillingCharge(t *testing.T) {
	valid := &tossConfirmResponse{
		PaymentKey:    "pay_toss_sub_order",
		Type:          "BILLING",
		OrderId:       "toss_sub_order",
		Status:        "DONE",
		TotalAmount:   15000,
		BalanceAmount: 15000,
		Currency:      "krw",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 15000, Company: "현대", Number: "433012******1234"},
	}
	if !isValidTossBillingCharge(valid, "toss_sub_order", 15000) {
		t.Fatal("expected valid billing charge")
	}

	cases := []struct {
		name string
		resp *tossConfirmResponse
	}{
		{name: "nil", resp: nil},
		{name: "wrong type", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "NORMAL", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "wrong method", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "간편결제", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "wrong card amount", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 14999}}},
		{name: "wrong status", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "READY", TotalAmount: 15000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "wrong order", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "other", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "wrong amount", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 1, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1}}},
		{name: "wrong balance", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, BalanceAmount: 14999, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "wrong currency", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "USD", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
		{name: "missing card", resp: &tossConfirmResponse{PaymentKey: "pay_toss_sub_order", Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "카드"}},
		{name: "missing payment key", resp: &tossConfirmResponse{Type: "BILLING", OrderId: "toss_sub_order", Status: "DONE", TotalAmount: 15000, Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 15000}}},
	}
	for _, tc := range cases {
		if isValidTossBillingCharge(tc.resp, "toss_sub_order", 15000) {
			t.Fatalf("%s: expected invalid billing charge", tc.name)
		}
	}

	contractMismatches := []struct {
		name   string
		mutate func(*tossConfirmResponse)
	}{
		{name: "tax free amount", mutate: func(p *tossConfirmResponse) { p.TaxFreeAmount = 1 }},
		{name: "tax exemption amount", mutate: func(p *tossConfirmResponse) { p.TaxExemptionAmount = 1 }},
		{name: "escrow", mutate: func(p *tossConfirmResponse) { p.UseEscrow = true }},
		{name: "culture expense", mutate: func(p *tossConfirmResponse) { p.CultureExpense = true }},
	}
	for _, tc := range contractMismatches {
		t.Run(tc.name, func(t *testing.T) {
			mismatched := *valid
			card := *valid.Card
			mismatched.Card = &card
			tc.mutate(&mismatched)
			require.False(t, isValidTossBillingCharge(&mismatched, valid.OrderId, valid.TotalAmount))
		})
	}
}

func TestTossTaxContractMismatchBlocksDONEAcrossInitialRenewalAndWalletPaths(t *testing.T) {
	initial := &tossConfirmResponse{
		PaymentKey: "pay_tax_contract_initial", Type: "BILLING", OrderId: "toss_sub_tax_contract_initial",
		Status: "DONE", TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드",
		TaxFreeAmount: 100, Card: &tossPaymentCard{Amount: 1000},
	}
	_, err := classifyTossBillingChargeResponse(initial, initial.OrderId, initial.TotalAmount)
	require.ErrorContains(t, err, "DONE payload mismatch")

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		orderID := strings.TrimPrefix(request.URL.EscapedPath(), "/v1/payments/orders/")
		var body string
		switch orderID {
		case "toss_renewal_tax_contract":
			body = `{"paymentKey":"pay_tax_contract_renewal","type":"BILLING","orderId":"toss_renewal_tax_contract","status":"DONE","totalAmount":1000,"balanceAmount":1000,"taxExemptionAmount":100,"currency":"KRW","method":"카드","card":{"amount":1000}}`
		case "wallet_auto_91_tax_contract":
			body = `{"paymentKey":"pay_tax_contract_wallet","type":"BILLING","orderId":"wallet_auto_91_tax_contract","status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","useEscrow":true,"cultureExpense":true,"card":{"amount":1000}}`
		default:
			return nil, fmt.Errorf("unexpected lookup order: %s", orderID)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	renewal, err := lookupTossRenewalPayment(context.Background(), "live_sk_tax_contract", "toss_renewal_tax_contract", 1000)
	require.ErrorContains(t, err, "payload mismatch")
	require.NotNil(t, renewal)
	require.False(t, renewal.Done)

	wallet, err := lookupWalletAutoRechargeTossPayment(context.Background(), []string{"live_sk_tax_contract"}, "wallet_auto_91_tax_contract", 1000)
	require.ErrorContains(t, err, "payload mismatch")
	require.NotNil(t, wallet)
	require.False(t, wallet.Done)
}

func TestTossPaymentTypeValidationSeparatesGenericShapeFromBillingContract(t *testing.T) {
	payment := &tossConfirmResponse{
		PaymentKey:    "pay_type_validation",
		Type:          "BILLING",
		OrderId:       "order_type_validation",
		Status:        "DONE",
		TotalAmount:   15000,
		BalanceAmount: 15000,
		Currency:      "KRW",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 15000, Company: "현대", Number: "433012******1234"},
	}
	require.NoError(t, validateTossPaymentResponseShape(payment))
	require.NoError(t, validateTossBillingChargeResponseShape(payment))
	require.True(t, isValidTossBillingCharge(payment, payment.OrderId, payment.TotalAmount))

	for _, paymentType := range []string{"NORMAL", "BRANDPAY"} {
		payment.Type = paymentType
		require.NoError(t, validateTossPaymentResponseShape(payment), paymentType)
		require.Error(t, validateTossBillingChargeResponseShape(payment), paymentType)
		require.False(t, isValidTossBillingCharge(payment, payment.OrderId, payment.TotalAmount), paymentType)
	}

	for _, paymentType := range []string{"", "UNKNOWN", "billing"} {
		payment.Type = paymentType
		require.Error(t, validateTossPaymentResponseShape(payment), paymentType)
	}
}

func TestReconcileTossPendingRenewalExpiresOldNonTerminalPayment(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	if err := model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserBillingKey{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "old-nonterminal-renewal-user", Status: common.UserStatusEnabled, AffCode: "old-nonterminal-renewal-user"}).Error; err != nil {
		t.Fatalf("create renewal user: %v", err)
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
	providerClientKey := "ck_billing_test"
	providerSecret := "sk_billing_secret"
	keyId, err := model.StoreTossBillingKeyWithProviderSnapshot(
		7,
		"cust_test",
		"billing_key_stale_renewal",
		"현대",
		"433012******1234",
		providerSecret,
		model.TossBillingClientKeyFingerprint(providerClientKey),
	)
	if err != nil {
		t.Fatalf("store billing key: %v", err)
	}
	credential, err := model.EncryptProviderCredential(providerSecret)
	if err != nil {
		t.Fatalf("encrypt provider credential: %v", err)
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
	seedControllerTossRenewalContract(t, sub, plan, 15000)
	tradeNo := "toss_sub_renew_11_150"
	order := &model.SubscriptionOrder{
		UserId:                       7,
		PlanId:                       3,
		Money:                        10,
		TradeNo:                      tradeNo,
		PaymentMethod:                model.PaymentMethodToss,
		PaymentProvider:              model.PaymentProviderToss,
		Status:                       common.TopUpStatusPending,
		CreateTime:                   1000,
		ProviderAmount:               15000,
		ProviderCurrency:             "KRW",
		ProviderCredential:           credential,
		ProviderClientKeyHash:        model.TossBillingClientKeyFingerprint(providerClientKey),
		BillingKeyId:                 keyId,
		BillingChargeProtocolVersion: 1,
		RenewalEndTime:               sub.EndTime,
	}
	if err := model.SetTossRenewalOrderSnapshotFromContract(order, sub); err != nil {
		t.Fatalf("snapshot renewal order: %v", err)
	}
	if err := model.DB.Create(order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalClientKey := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	originalTestMode := setting.TossTestMode
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossBillingClientKey = originalClientKey
		setting.TossBillingSecretKey = originalSecret
		setting.TossTestMode = originalTestMode
	})
	setting.TossTestMode = false
	setting.TossBillingClientKey = providerClientKey
	setting.TossBillingSecretKey = providerSecret
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/payments/orders/"+tradeNo {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_stale_renewal","type":"BILLING","orderId":"toss_sub_renew_11_150","status":"READY","totalAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
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
	if got := issued.cardNumberForStorage(); got != "****123*" {
		t.Fatalf("cardNumberForStorage = %q want last-four-only masked card number", got)
	}

	issued.Card.Company = strings.Repeat("현", 40)
	issued.Card.Number = "4330123412341234"
	require.Len(t, []rune(issued.cardCompanyForStorage()), tossCardCompanyMaxRunes)
	require.Equal(t, "****1234", issued.cardNumberForStorage())
}

func TestValidateTossBillingIssueResponseShapeEnforcesDocumentedKeyBounds(t *testing.T) {
	valid := &tossBillingIssueResponse{
		BillingKey:  "billing_key_valid",
		CustomerKey: "cust_valid",
	}
	valid.Card.Company = "card"
	valid.Card.Number = "****1234"
	require.NoError(t, validateTossBillingIssueResponseShape(valid))

	tooLong := *valid
	tooLong.BillingKey = strings.Repeat("b", model.TossBillingKeyMaxRunes+1)
	require.Error(t, validateTossBillingIssueResponseShape(&tooLong))

	whitespace := *valid
	whitespace.BillingKey = " billing_key_valid"
	require.Error(t, validateTossBillingIssueResponseShape(&whitespace))
	internalWhitespace := *valid
	internalWhitespace.BillingKey = "billing key valid"
	require.Error(t, validateTossBillingIssueResponseShape(&internalWhitespace))

	longCustomer := *valid
	longCustomer.CustomerKey = strings.Repeat("c", 301)
	require.Error(t, validateTossBillingIssueResponseShape(&longCustomer))
}

func TestTossPaymentResponseFullPANSanitizedBeforePersistence(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	company := strings.Repeat("카", 40)
	body := fmt.Sprintf(`{"paymentKey":"pay_full_pan","type":"BILLING","orderId":"toss_full_pan","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"%s","number":"4330123412341234"}}`, company)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	payment, status, err := getTossPaymentWithSecret(context.Background(), "pay_full_pan", "sk_full_pan")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, payment.Card)
	require.Equal(t, "****1234", payment.Card.Number)
	require.Len(t, []rune(payment.Card.Company), tossCardCompanyMaxRunes)

	created, err := persistTossFulfillmentEvent(payment)
	require.NoError(t, err)
	require.True(t, created)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ?", payment.OrderId).First(&event).Error)
	require.NotContains(t, event.ProviderPayload, "4330123412341234")
	require.Contains(t, event.ProviderPayload, `"number":"****1234"`)
	require.NotContains(t, event.ProviderPayload, company)
}

func TestIssueTossBillingKeyRetriesTransientFailureWithSameIdempotencyKey(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
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
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
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

	attempts := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if got := r.URL.EscapedPath(); got != "/v1/billing/billing%2Fretry%3Fkey" {
			t.Fatalf("escaped path = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "toss_sub_charge_retry" {
			t.Fatalf("Idempotency-Key = %q want toss_sub_charge_retry", got)
		}
		requestBody, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		var payload struct {
			TaxFreeAmount      *int64 `json:"taxFreeAmount"`
			TaxExemptionAmount *int64 `json:"taxExemptionAmount"`
		}
		require.NoError(t, common.Unmarshal(requestBody, &payload))
		require.NotNil(t, payload.TaxFreeAmount)
		require.Zero(t, *payload.TaxFreeAmount)
		require.NotNil(t, payload.TaxExemptionAmount)
		require.Zero(t, *payload.TaxExemptionAmount)
		if attempts == 1 {
			return nil, errors.New("temporary network failure")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_retry","type":"BILLING","orderId":"toss_sub_charge_retry","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
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
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_lookup","type":"BILLING","orderId":"toss_sub_lookup","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
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

	var postAttempts int
	var lookedUp bool
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("live_sk_test_secret:"))
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
				Body:       io.NopCloser(strings.NewReader(`{"paymentKey":"pay_model_lookup","type":"BILLING","orderId":"toss_sub_model_lookup","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`)),
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

func TestLookupTossRenewalPaymentClassifiesRecoveryStates(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		TradeNo: "toss_sub_recovery", PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	statusCode := http.StatusNotFound
	body := `{"code":"NOT_FOUND_PAYMENT","message":"not found"}`
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/payments/orders/toss_sub_recovery", r.URL.EscapedPath())
		return &http.Response{
			StatusCode: statusCode,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	_, err := lookupTossRenewalPayment(context.Background(), "test_sk_attempt", "toss_sub_recovery", 15000)
	require.ErrorIs(t, err, model.ErrTossBillingPaymentNotFound)

	statusCode = http.StatusOK
	body = `{"paymentKey":"pay_recovery","type":"BILLING","orderId":"toss_sub_recovery","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`
	result, err := lookupTossRenewalPayment(context.Background(), "test_sk_attempt", "toss_sub_recovery", 15000)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "pay_recovery", result.PaymentKey)

	body = `{"paymentKey":"pay_recovery","type":"BILLING","orderId":"toss_sub_recovery","status":"PARTIAL_CANCELED","totalAmount":15000,"balanceAmount":5000,"currency":"KRW","method":"카드","card":{"amount":15000,"company":"현대","number":"433012******1234"}}`
	_, err = lookupTossRenewalPayment(context.Background(), "test_sk_attempt", "toss_sub_recovery", 15000)
	require.Error(t, err)
	require.NotErrorIs(t, err, model.ErrTossBillingPaymentNotFound)
	require.NotErrorIs(t, err, model.ErrTossBillingChargeTerminal)
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_type = ? AND order_id = ?", model.TossPaymentEventTypeCancellation, "toss_sub_recovery").First(&event).Error)
	require.Equal(t, int64(10000), event.CancelAmount)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
}

func TestHandleTossBillingActivationFailureKeepsPaidOrderRecoverable(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}, &model.TossPaymentEvent{}))
	keyId, err := model.StoreTossBillingKey(7, "cust_test", "billing_activation_fail", "현대", "433012******1234")
	if err != nil {
		t.Fatalf("StoreTossBillingKey error = %v", err)
	}
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:           7,
		PlanId:           1,
		TradeNo:          "toss_sub_activation_fail",
		PaymentMethod:    model.PaymentMethodToss,
		PaymentProvider:  model.PaymentProviderToss,
		Status:           common.TopUpStatusPending,
		BillingKeyId:     keyId,
		ProviderAmount:   15000,
		ProviderCurrency: "KRW",
	}).Error)
	providerPayload := `{"paymentKey":"pay_activation_fail","orderId":"toss_sub_activation_fail","status":"DONE"}`
	handleTossBillingActivationFailure(
		context.Background(),
		7,
		"toss_sub_activation_fail",
		keyId,
		15000,
		&tossConfirmResponse{PaymentKey: "pay_activation_fail", OrderId: "toss_sub_activation_fail", Status: "DONE", TotalAmount: 15000},
		providerPayload,
		errors.New("db down"),
		false,
	)

	var key model.UserBillingKey
	if err := model.DB.First(&key, keyId).Error; err != nil {
		t.Fatalf("load billing key: %v", err)
	}
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
	var order model.SubscriptionOrder
	require.NoError(t, model.DB.Where("trade_no = ?", "toss_sub_activation_fail").First(&order).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
	require.Contains(t, order.ProviderPayload, "pay_activation_fail")
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ?", "toss_sub_activation_fail").First(&event).Error)
	require.Equal(t, model.TossPaymentEventTypeFulfillment, event.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
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

	setting.TossSecretKey = "live_sk_test_secret"
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

	setting.TossSecretKey = "live_sk_test_secret"
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
