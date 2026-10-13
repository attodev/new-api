package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from upstream 2a0ce3475/47ba9d2c6. Upstream bounds quota at int32;
// this fork's quota columns are bigint and the ceiling is common.MaxQuota,
// which settlement (model.CreditTopUpTarget) treats as an inclusive bound.

func withTopUpQuotaSettings(t *testing.T, quotaPerUnit float64, displayType string) {
	t.Helper()
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = quotaPerUnit
	operation_setting.GetGeneralSetting().QuotaDisplayType = displayType
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})
}

func requireTopUpQuotaErrorKey(t *testing.T, err error, key string) *topUpQuotaError {
	t.Helper()
	var quotaErr *topUpQuotaError
	require.True(t, errors.As(err, &quotaErr), "expected *topUpQuotaError, got %v", err)
	require.Equal(t, key, quotaErr.key)
	return quotaErr
}

func TestTopUpQuotaValidation(t *testing.T) {
	maxStoredAmount := common.MaxQuota / 500000 // 1_000_000_000 at the default QuotaPerUnit

	testCases := []struct {
		name        string
		displayType string
		amount      int64
		wantQuota   int64
		wantErr     bool
	}{
		{
			name:        "currency amount at limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      maxStoredAmount,
			wantQuota:   common.MaxQuota,
		},
		{
			name:        "currency amount above limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      maxStoredAmount + 1,
			wantErr:     true,
		},
		{
			name:        "token amount preserves settlement truncation",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      common.MaxQuota + 500000 - 1,
			wantQuota:   common.MaxQuota,
		},
		{
			name:        "token amount above settlement limit",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      common.MaxQuota + 500000,
			wantErr:     true,
		},
		{
			name:        "token amount below one unit settles to zero",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      499999,
			wantErr:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			withTopUpQuotaSettings(t, 500000, tc.displayType)
			quota, err := getTopUpQuota(tc.amount)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}

func TestValidateTopUpQuotaReturnsMaximumAmount(t *testing.T) {
	withTopUpQuotaSettings(t, 1_000_000, operation_setting.QuotaDisplayTypeUSD)

	maxAmount := decimal.NewFromInt(common.MaxQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	require.Equal(t, int64(500_000_000), maxAmount)
	require.Equal(t, maxAmount, getMaxTopUpAmount())

	creditedQuota, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, creditedQuota)

	_, err = validateTopUpQuota(maxAmount + 1)
	quotaErr := requireTopUpQuotaErrorKey(t, err, i18n.MsgTopupAmountExceedsMax)
	require.Equal(t, maxAmount, quotaErr.args["Max"])
}

func TestGetMaxTopUpAmountInTokenMode(t *testing.T) {
	withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeTokens)

	maxAmount := getMaxTopUpAmount()
	require.Equal(t, common.MaxQuota+500000-1, maxAmount)
	_, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	_, err = validateTopUpQuota(maxAmount + 1)
	requireTopUpQuotaErrorKey(t, err, i18n.MsgTopupAmountExceedsMax)
}

func TestRequestAmountRejectsTopUpThatCannotBeSettled(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	// A larger QuotaPerUnit makes the settlement ceiling (5e8 units) bind before
	// the generic MaxTopUpAmount (1e9) request bound.
	withTopUpQuotaSettings(t, 1_000_000, operation_setting.QuotaDisplayTypeUSD)
	user := model.User{Id: 601, Username: "amount-unsettleable", Password: "x", Role: common.RoleCommonUser, AffCode: "amount-unsettleable"}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(RequestAmount, user, http.MethodPost, "/api/user/amount", `{"amount":500000001}`)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupAmountExceedsMax, map[string]any{"Max": int64(500_000_000)}))
}

func TestRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeUSD)
	user := model.User{
		Id:       602,
		Username: "topup_capacity_user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "topup_capacity_user",
		Quota:    common.MaxQuota - 1_000_000,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&user).Error)

	// 2 units credit exactly 1_000_000 and fill the wallet; 3 units overflow it.
	allowed := performOrganizationRequest(RequestAmount, user, http.MethodPost, "/api/user/amount", `{"amount":2}`)
	require.Contains(t, allowed.Body.String(), `"message":"success"`)

	res := performOrganizationRequest(RequestAmount, user, http.MethodPost, "/api/user/amount", `{"amount":3}`)
	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
}

func TestRequestEpayRejectsTopUpThatWouldOverflowOrganizationWallet(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeUSD)
	owner, _ := createPaymentCapacityOrganization(t, 603, 71, 0, common.MaxQuota-1)

	res := performOrganizationRequest(
		RequestOrganizationEpay,
		owner,
		http.MethodPost,
		"/api/organization/pay",
		`{"amount":10,"payment_method":"alipay"}`,
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
	requireTopUpCount(t, 0)
}

func TestValidateCreditedQuotaRejectsOverflow(t *testing.T) {
	quota, err := validateCreditedQuota(decimal.NewFromInt(common.MaxQuota))
	require.NoError(t, err)
	require.Equal(t, common.MaxQuota, quota)

	// Settlement truncates fractional quota, so validation must too.
	quota, err = validateCreditedQuota(decimal.RequireFromString("1.9"))
	require.NoError(t, err)
	require.Equal(t, int64(1), quota)

	_, err = validateCreditedQuota(decimal.Zero)
	requireTopUpQuotaErrorKey(t, err, i18n.MsgTopupInvalidQuota)
	_, err = validateCreditedQuota(decimal.RequireFromString("0.5"))
	requireTopUpQuotaErrorKey(t, err, i18n.MsgTopupInvalidQuota)
	_, err = validateCreditedQuota(decimal.NewFromInt(common.MaxQuota + 1))
	requireTopUpQuotaErrorKey(t, err, i18n.MsgTopupQuotaOutOfRange)
}

func TestStripeCreditedQuotaIncludesGroupRatio(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldTopupGroupRatio := common.TopupGroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"vip":2}`))
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldTopupGroupRatio))
	})

	_, err := validateCreditedQuota(getStripeCreditedQuota(500_000_000, "vip"))
	require.NoError(t, err)
	_, err = validateCreditedQuota(getStripeCreditedQuota(500_000_001, "vip"))
	require.Error(t, err)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	assert.True(t, decimal.NewFromInt(500000).Equal(getStripeCreditedQuota(1, "free")))
}

// TestPayEndpointsRejectTopUpThatWouldOverflowWallet covers the remaining
// order-creating entry points: each must refuse before inserting a top-up row
// (and before contacting its provider) when the wallet cannot absorb the credit.
func TestPayEndpointsRejectTopUpThatWouldOverflowWallet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*gin.Context)
		body    string
		creem   bool
	}{
		{name: "epay", handler: RequestEpay, body: `{"amount":10,"payment_method":"alipay"}`},
		{name: "stripe", handler: RequestStripePay, body: `{"amount":10,"payment_method":"stripe"}`},
		{name: "stripe quote", handler: RequestStripeAmount, body: `{"amount":10,"payment_method":"stripe"}`},
		{name: "creem", handler: RequestCreemPay, body: `{"product_id":"prod_capacity","payment_method":"creem"}`, creem: true},
		{name: "waffo", handler: RequestWaffoPay, body: `{"amount":10}`},
		{name: "waffo quote", handler: RequestWaffoAmount, body: `{"amount":10}`},
		{name: "waffo pancake", handler: RequestWaffoPancakePay, body: `{"amount":10}`},
		{name: "waffo pancake quote", handler: RequestWaffoPancakeAmount, body: `{"amount":10}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
			withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeUSD)
			withPayEndpointTestSettings(t)

			user := model.User{
				Id:       610,
				Username: "pay-endpoint-capacity",
				Password: "x",
				Role:     common.RoleCommonUser,
				AffCode:  "pay-endpoint-capacity",
				Group:    "default",
				Quota:    common.MaxQuota - 1,
			}
			require.NoError(t, model.DB.Create(&user).Error)

			res := performOrganizationRequest(tc.handler, user, http.MethodPost, "/api/user/pay", tc.body)

			want := i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded)
			if tc.creem {
				require.JSONEq(t, `{"message":"error","data":"`+want+`"}`, res.Body.String())
			} else {
				requireOrganizationApiError(t, res, want)
			}
			requireTopUpCount(t, 0)
		})
	}
}

func withPayEndpointTestSettings(t *testing.T) {
	t.Helper()
	oldCreemProducts := setting.CreemProducts
	oldWaffoEnabled := setting.WaffoEnabled
	oldPancakeMerchant := setting.WaffoPancakeMerchantID
	oldPancakeKey := setting.WaffoPancakePrivateKey
	oldPancakeProduct := setting.WaffoPancakeProductID
	paymentSetting := operation_setting.GetPaymentSetting()
	oldComplianceConfirmed := paymentSetting.ComplianceConfirmed
	oldComplianceTermsVersion := paymentSetting.ComplianceTermsVersion
	oldDiscount := paymentSetting.AmountDiscount
	t.Cleanup(func() {
		setting.CreemProducts = oldCreemProducts
		setting.WaffoEnabled = oldWaffoEnabled
		setting.WaffoPancakeMerchantID = oldPancakeMerchant
		setting.WaffoPancakePrivateKey = oldPancakeKey
		setting.WaffoPancakeProductID = oldPancakeProduct
		paymentSetting.ComplianceConfirmed = oldComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = oldComplianceTermsVersion
		paymentSetting.AmountDiscount = oldDiscount
	})

	setting.CreemProducts = `[{"productId":"prod_capacity","name":"capacity","price":10,"currency":"USD","quota":5000000}]`
	setting.WaffoEnabled = true
	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = "private-key"
	setting.WaffoPancakeProductID = "product"
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	paymentSetting.AmountDiscount = map[int]float64{}
}
