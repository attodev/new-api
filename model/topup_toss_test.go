package model

import (
	"regexp"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

// 크레딧 계산식이 설계와 일치하는지 검증: quota = Money * QuotaPerUnit
func TestTossQuotaFormula(t *testing.T) {
	prev := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	defer func() { common.QuotaPerUnit = prev }()

	money := 10.0 // 13000 KRW / 1300 = 10 USD-equiv
	quota := int(decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if quota != 5000000 {
		t.Fatalf("quota: got %d want 5000000", quota)
	}
}

func TestTossPaymentConstants(t *testing.T) {
	if PaymentMethodToss != "toss" {
		t.Fatalf("PaymentMethodToss = %q want toss", PaymentMethodToss)
	}
	if PaymentProviderToss != "toss" {
		t.Fatalf("PaymentProviderToss = %q want toss", PaymentProviderToss)
	}
}

func TestGenerateTossCustomerKey(t *testing.T) {
	pattern := regexp.MustCompile(`^cust_[A-Za-z0-9]{32}$`)
	for i := 0; i < 20; i++ {
		key := generateTossCustomerKey()
		if !strings.HasPrefix(key, "cust_") {
			t.Fatalf("key %q does not start with cust_", key)
		}
		if len(key) < 2 || len(key) > 50 {
			t.Fatalf("key %q length %d is outside Toss allowed range 2..50", key, len(key))
		}
		if !pattern.MatchString(key) {
			t.Fatalf("key %q does not match ^cust_[A-Za-z0-9]{32}$", key)
		}
	}
}
