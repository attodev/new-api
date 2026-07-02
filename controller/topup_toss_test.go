package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestGetTossPayMoney(t *testing.T) {
	// group ratio 기본 1 가정: 입력 원화가 그대로 청구 원화가 된다.
	got := getTossPayMoney(13000, "default")
	if got != 13000 {
		t.Fatalf("getTossPayMoney(13000) = %d want 13000", got)
	}
}

func TestGetTossPayMoneyAppliesDiscount(t *testing.T) {
	ps := operation_setting.GetPaymentSetting()
	prev := ps.AmountDiscount
	ps.AmountDiscount = map[int]float64{13000: 0.9}
	defer func() { ps.AmountDiscount = prev }()

	got := getTossPayMoney(13000, "default")
	if got != 11700 { // 13000 * 0.9
		t.Fatalf("getTossPayMoney with discount = %d want 11700", got)
	}
}

func TestIsValidServerAddress(t *testing.T) {
	cases := map[string]bool{
		"https://pay.example.com": true,
		"http://localhost:3000":   true,
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

func TestGetTossTopUpQuoteDefaultsToKRWMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := getTossTopUpQuote(13000, "", "default")

	if quote.AmountMode != model.TossTopUpAmountModeKRW {
		t.Fatalf("AmountMode = %q want %q", quote.AmountMode, model.TossTopUpAmountModeKRW)
	}
	if quote.ChargeKRW != 13000 {
		t.Fatalf("ChargeKRW = %d want 13000", quote.ChargeKRW)
	}
	if quote.CreditQuota != 5000000 {
		t.Fatalf("CreditQuota = %d want 5000000", quote.CreditQuota)
	}
}

func TestGetTossTopUpQuoteQuotaMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := getTossTopUpQuote(10, model.TossTopUpAmountModeQuota, "default")

	if quote.AmountMode != model.TossTopUpAmountModeQuota {
		t.Fatalf("AmountMode = %q want quota", quote.AmountMode)
	}
	if quote.ChargeKRW != 13000 {
		t.Fatalf("ChargeKRW = %d want 13000", quote.ChargeKRW)
	}
	if quote.CreditQuota != 5000000 {
		t.Fatalf("CreditQuota = %d want 5000000", quote.CreditQuota)
	}
}

func setupTossControllerTest(t *testing.T) {
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
	setupTossControllerTest(t)

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
	setupTossControllerTest(t)

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
}

func TestOrganizationTossAmountQuotaModeUsesChargeForMinValidation(t *testing.T) {
	setupTossControllerTest(t)

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
