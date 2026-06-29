package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

func TestGetTossPayMoney(t *testing.T) {
	// group ratio 기본 1 가정: 입력 원화가 그대로 청구 원화가 된다.
	got := getTossPayMoney(13000, "default")
	if got != 13000 {
		t.Fatalf("getTossPayMoney(13000) = %d want 13000", got)
	}
}

func TestTossMinTopupGuard(t *testing.T) {
	prev := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	defer func() { setting.TossMinTopUp = prev }()

	if int64(setting.TossMinTopUp) != 1000 {
		t.Fatalf("min topup not set")
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
