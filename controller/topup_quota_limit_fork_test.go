package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 1fa9b3f1b (controller/topup_quota_limit_test.go). Team's
// wallet ceiling common.MaxQuota is develop's common.MaxWalletQuota (2^53-1),
// and develop's pay endpoints answer every rejection through paymentError.

const walletTopUpLimitExceededBody = `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`

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

func TestGetMaxTopUpAmountInTokenMode(t *testing.T) {
	withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeTokens)

	// Token mode truncates to whole units, so the largest accepted amount is
	// the last token before the first unit that no longer fits the wallet:
	// (floor(MaxWalletQuota/500000)+1)*500000 - 1.
	maxAmount := getMaxTopUpAmount()
	require.Equal(t, int64(9_007_199_254_999_999), maxAmount)

	quota, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	require.Equal(t, 9_007_199_254_500_000, quota)
	require.LessOrEqual(t, quota, common.MaxWalletQuota)

	_, err = validateTopUpQuota(maxAmount + 1)
	require.EqualError(t, err, fmt.Sprintf("Top-up amount cannot exceed %d", maxAmount))
}

// TestPayEndpointsRejectTopUpThatWouldOverflowWallet covers the remaining
// order-creating entry points: each must refuse before inserting a top-up row
// (and before contacting its provider) when the wallet cannot absorb the credit.
func TestPayEndpointsRejectTopUpThatWouldOverflowWallet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(*gin.Context)
		path    string
		body    string
	}{
		{name: "epay", handler: RequestEpay, path: "/api/user/pay", body: `{"amount":10,"payment_method":"alipay"}`},
		{name: "stripe", handler: RequestStripePay, path: "/api/user/stripe/pay", body: `{"amount":10,"payment_method":"stripe"}`},
		{name: "stripe quote", handler: RequestStripeAmount, path: "/api/user/stripe/amount", body: `{"amount":10,"payment_method":"stripe"}`},
		{name: "creem", handler: RequestCreemPay, path: "/api/user/creem/pay", body: `{"product_id":"prod_capacity","payment_method":"creem"}`},
		{name: "waffo", handler: RequestWaffoPay, path: "/api/user/waffo/pay", body: `{"amount":10}`},
		{name: "waffo quote", handler: RequestWaffoAmount, path: "/api/user/waffo/amount", body: `{"amount":10}`},
		{name: "waffo pancake", handler: RequestWaffoPancakePay, path: "/api/user/waffo-pancake/pay", body: `{"amount":10}`},
		{name: "waffo pancake quote", handler: RequestWaffoPancakeAmount, path: "/api/user/waffo-pancake/amount", body: `{"amount":10}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			// Initialize production column quoting: the Stripe quote reads the
			// user group (a reserved-word column) before its capacity check.
			t.Setenv("LOG_SQL_DSN", "")
			require.NoError(t, model.InitLogDB())
			withTopUpQuotaSettings(t, 500000, operation_setting.QuotaDisplayTypeUSD)
			withPayEndpointTestSettings(t)

			user := model.User{
				Id:       610,
				Username: "pay-endpoint-capacity",
				Password: "x",
				Role:     common.RoleCommonUser,
				AffCode:  "pay-endpoint-capacity",
				Group:    "default",
				Status:   common.UserStatusEnabled,
				Quota:    common.MaxWalletQuota - 1,
			}
			require.NoError(t, model.DB.Create(&user).Error)

			res := performOrganizationRequest(tc.handler, user, http.MethodPost, tc.path, tc.body)

			assert.Equal(t, http.StatusOK, res.Code)
			assert.JSONEq(t, walletTopUpLimitExceededBody, res.Body.String())
			requireOrganizationTopUpCount(t, 0)

			var stored model.User
			require.NoError(t, model.DB.First(&stored, user.Id).Error)
			assert.Equal(t, common.MaxWalletQuota-1, stored.Quota)
		})
	}
}

// withPayEndpointTestSettings enables every provider just far enough to reach
// its capacity check, and pins the minimum top-ups the requests rely on.
func withPayEndpointTestSettings(t *testing.T) {
	t.Helper()
	oldMinTopUp := operation_setting.MinTopUp
	oldStripeMinTopUp := setting.StripeMinTopUp
	oldWaffoMinTopUp := setting.WaffoMinTopUp
	oldPancakeMinTopUp := setting.WaffoPancakeMinTopUp
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
		operation_setting.MinTopUp = oldMinTopUp
		setting.StripeMinTopUp = oldStripeMinTopUp
		setting.WaffoMinTopUp = oldWaffoMinTopUp
		setting.WaffoPancakeMinTopUp = oldPancakeMinTopUp
		setting.CreemProducts = oldCreemProducts
		setting.WaffoEnabled = oldWaffoEnabled
		setting.WaffoPancakeMerchantID = oldPancakeMerchant
		setting.WaffoPancakePrivateKey = oldPancakeKey
		setting.WaffoPancakeProductID = oldPancakeProduct
		paymentSetting.ComplianceConfirmed = oldComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = oldComplianceTermsVersion
		paymentSetting.AmountDiscount = oldDiscount
	})

	operation_setting.MinTopUp = 1
	setting.StripeMinTopUp = 1
	setting.WaffoMinTopUp = 1
	setting.WaffoPancakeMinTopUp = 1
	setting.CreemProducts = `[{"productId":"prod_capacity","name":"capacity","price":10,"currency":"USD","quota":5000000}]`
	setting.WaffoEnabled = true
	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = "private-key"
	setting.WaffoPancakeProductID = "product"
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	paymentSetting.AmountDiscount = map[int]float64{}
}
