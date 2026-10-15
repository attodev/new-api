package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// useEpayTestState sets the compliance flag, the Epay gateway settings and
// PayMethods for one test and restores every global afterwards.
func useEpayTestState(t *testing.T, compliance bool, epayConfigured bool) {
	t.Helper()

	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalVersion := paymentSetting.ComplianceTermsVersion
	originalAddress := operation_setting.PayAddress
	originalID := operation_setting.EpayId
	originalKey := operation_setting.EpayKey
	originalMethods := operation_setting.PayMethods
	originalStripeSecret := setting.StripeApiSecret
	originalStripeWebhook := setting.StripeWebhookSecret
	originalStripePrice := setting.StripePriceId
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalVersion
		operation_setting.PayAddress = originalAddress
		operation_setting.EpayId = originalID
		operation_setting.EpayKey = originalKey
		operation_setting.PayMethods = originalMethods
		setting.StripeApiSecret = originalStripeSecret
		setting.StripeWebhookSecret = originalStripeWebhook
		setting.StripePriceId = originalStripePrice
	})

	paymentSetting.ComplianceConfirmed = compliance
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.PayMethods = []map[string]string{{"type": "alipay", "name": "Alipay"}}
	if epayConfigured {
		operation_setting.PayAddress = "https://epay.example.test"
		operation_setting.EpayId = "id"
		operation_setting.EpayKey = "key"
	} else {
		operation_setting.PayAddress = ""
		operation_setting.EpayId = ""
		operation_setting.EpayKey = ""
	}
	setting.StripeApiSecret = ""
	setting.StripeWebhookSecret = ""
	setting.StripePriceId = ""
}

func TestVisibleEpayPayMethodsPayMethods(t *testing.T) {
	t.Run("compliance confirmed but Epay not configured hides methods", func(t *testing.T) {
		useEpayTestState(t, true, false)
		got := visibleEpayPayMethods(true)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
	t.Run("compliance confirmed and Epay configured exposes methods", func(t *testing.T) {
		useEpayTestState(t, true, true)
		require.Equal(t, operation_setting.PayMethods, visibleEpayPayMethods(true))
	})
	t.Run("compliance not confirmed hides methods", func(t *testing.T) {
		useEpayTestState(t, true, true)
		got := visibleEpayPayMethods(false)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}

type topUpInfoPayMethod struct {
	Type string `json:"type"`
}

func getTopUpInfoPayMethodTypes(t *testing.T) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/info", nil)
	GetTopUpInfo(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			PayMethods []topUpInfoPayMethod `json:"pay_methods"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	types := make([]string, 0, len(resp.Data.PayMethods))
	for _, method := range resp.Data.PayMethods {
		types = append(types, method.Type)
	}
	return types
}

func TestGetTopUpInfoPayMethodsHidesUnconfiguredEpay(t *testing.T) {
	useEpayTestState(t, true, false)
	require.NotContains(t, getTopUpInfoPayMethodTypes(t), "alipay")

	// Other gateways still appear when they are configured.
	setting.StripeApiSecret = "sk_test"
	setting.StripeWebhookSecret = "whsec_test"
	setting.StripePriceId = "price_test"
	types := getTopUpInfoPayMethodTypes(t)
	require.NotContains(t, types, "alipay")
	require.Contains(t, types, "stripe")
}

func TestGetTopUpInfoPayMethodsShowsConfiguredEpay(t *testing.T) {
	useEpayTestState(t, true, true)
	require.Contains(t, getTopUpInfoPayMethodTypes(t), "alipay")
}

func TestGetTopUpInfoDoesNotMutateSharedPayMethods(t *testing.T) {
	useEpayTestState(t, true, true)
	// Spare capacity makes a stray append write into the shared backing array.
	shared := make([]map[string]string, 1, 4)
	shared[0] = map[string]string{"type": "alipay", "name": "Alipay"}
	operation_setting.PayMethods = shared

	setting.StripeApiSecret = "sk_test"
	setting.StripeWebhookSecret = "whsec_test"
	setting.StripePriceId = "price_test"
	require.Contains(t, getTopUpInfoPayMethodTypes(t), "stripe")

	require.Len(t, operation_setting.PayMethods, 1)
	require.Equal(t, "alipay", operation_setting.PayMethods[0]["type"])
	require.Nil(t, shared[:2][1], "append must not write into the shared backing array")
}
