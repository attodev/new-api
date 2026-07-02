package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestQuoteTossTopUpDefaultsToKRWMode(t *testing.T) {
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

	quote := QuoteTossTopUp(13000, "", "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
	require.Equal(t, 1300.0, quote.UnitPrice)
}

func TestQuoteTossTopUpInvalidModeDefaultsToKRWMode(t *testing.T) {
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

	quote := QuoteTossTopUp(13000, "invalid", "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpKRWModeFloorsQuotaUsingDecimal(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 3
	common.QuotaPerUnit = 3
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(1, TossTopUpAmountModeKRW, "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(1), quote.ChargeKRW)
	require.InDelta(t, 1.0/3.0, quote.CreditAmount, 1e-12)
	require.Equal(t, 1, quote.CreditQuota)
}

func TestQuoteTossTopUpQuotaModeChargesUnitPriceTimesQuota(t *testing.T) {
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

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, TossTopUpAmountModeQuota, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpNonPositiveUnitPriceReturnsZeroQuote(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 0
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(13000, TossTopUpAmountModeKRW, "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(0), quote.ChargeKRW)
	require.InDelta(t, 0.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 0, quote.CreditQuota)
	require.Equal(t, 0.0, quote.UnitPrice)
}

func TestQuoteTossTopUpKRWModeKeepsChargeFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10000: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10000, TossTopUpAmountModeKRW, "default")

	require.Equal(t, int64(10000), quote.ChargeKRW)
	require.InDelta(t, 20.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 10000000, quote.CreditQuota)
}

func TestQuoteTossTopUpQuotaModeKeepsCreditFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, int64(5000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}
