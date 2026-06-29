package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
