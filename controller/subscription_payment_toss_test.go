package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

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
