package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPaymentMethodsForTossFeatureGateRemovesDisabledLegacyEntry(t *testing.T) {
	methods := []map[string]string{
		{"name": "Legacy", "type": "alipay"},
		{"name": "Stale Toss", "type": model.PaymentMethodToss},
		{"name": "Card", "type": "stripe"},
	}

	filtered := paymentMethodsForTossFeatureGate(methods, false)

	require.Equal(t, []map[string]string{methods[0], methods[2]}, filtered)
	require.Len(t, methods, 3, "filtering must not mutate the configured method slice")
}

func TestPaymentMethodsForTossFeatureGatePreservesEnabledEntry(t *testing.T) {
	methods := []map[string]string{{"name": "Toss", "type": model.PaymentMethodToss}}
	require.Equal(t, methods, paymentMethodsForTossFeatureGate(methods, true))
}

func TestTossTopUpSuccessRedirectPathPreservesOrganizationWalletContext(t *testing.T) {
	require.Equal(t, "/console/log", tossTopUpSuccessRedirectPath(nil))
	require.Equal(t, "/console/log", tossTopUpSuccessRedirectPath(&model.TopUp{
		TargetType: model.TopUpTargetTypeUser,
	}))
	require.Equal(t, "/wallet?show_history=true", tossTopUpSuccessRedirectPath(&model.TopUp{
		TargetType: model.TopUpTargetTypeOrganization,
	}))
}

func TestWalletAutoRechargeRedirectBaseUsesCrossThemeWalletEntry(t *testing.T) {
	require.Equal(t, "/wallet", walletAutoRechargeRedirectBase(nil))
	require.Equal(t, "/wallet", walletAutoRechargeRedirectBase(&model.WalletAutoRecharge{
		TargetType: model.TopUpTargetTypeUser,
	}))
	require.Equal(t, "/wallet", walletAutoRechargeRedirectBase(&model.WalletAutoRecharge{
		TargetType: model.TopUpTargetTypeOrganization,
	}))
}

func TestTossFailRedirectPreservesOrganizationWalletContextReadOnly(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	const orderID = "toss_organization_fail_order"
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          17,
		TargetType:      model.TopUpTargetTypeOrganization,
		TargetId:        29,
		TradeNo:         orderID,
		ProviderOrderId: orderID,
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/toss/fail?orderId="+url.QueryEscape(orderID)+"&code=PAY_PROCESS_CANCELED&message="+url.QueryEscape("organization payment canceled & retry"),
		nil,
	)

	TossFail(c)

	require.Equal(t, http.StatusFound, recorder.Code)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/wallet", location.Path)
	require.Equal(t, orderID, location.Query().Get("toss_order_id"))
	require.Equal(t, "PAY_PROCESS_CANCELED", location.Query().Get("toss_error_code"))
	require.Equal(t, "organization payment canceled & retry", location.Query().Get("toss_error_message"))

	stored, err := model.GetTopUpByTradeNoWithError(orderID)
	require.NoError(t, err)
	require.Equal(t, common.TopUpStatusPending, stored.Status, "fail redirect lookup must remain read-only")
}

func TestTossFailRedirectRejectsUntrustedOrganizationHints(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	const crossProviderOrderID = "toss_cross_provider_order"
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          17,
		TargetType:      model.TopUpTargetTypeOrganization,
		TargetId:        29,
		TradeNo:         crossProviderOrderID,
		ProviderOrderId: crossProviderOrderID,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
	}).Error)

	for _, orderID := range []string{"../organization/wallet", "toss_missing_local_order", crossProviderOrderID} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(
			http.MethodGet,
			"/api/toss/fail?orderId="+url.QueryEscape(orderID)+"&message="+url.QueryEscape("failed & encoded"),
			nil,
		)

		TossFail(c)

		require.Equal(t, http.StatusFound, recorder.Code)
		require.True(t, strings.HasPrefix(recorder.Header().Get("Location"), "/console/topup?"), orderID)
	}
}
