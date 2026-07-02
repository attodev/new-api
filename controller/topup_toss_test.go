package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
