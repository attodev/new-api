package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestTossPayRejectsTopUpThatWouldOverflowUserWallet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		path    string
		body    string
	}{
		{name: "pay quota mode", handler: RequestTossPay, path: "/api/user/toss/pay", body: `{"amount":1,"amount_mode":"quota","payment_method":"toss"}`},
		{name: "pay krw mode", handler: RequestTossPay, path: "/api/user/toss/pay", body: `{"amount":1300,"amount_mode":"krw","payment_method":"toss"}`},
		{name: "quote quota mode", handler: RequestTossAmount, path: "/api/user/toss/amount", body: `{"amount":1,"amount_mode":"quota","payment_method":"toss"}`},
		{name: "quote krw mode", handler: RequestTossAmount, path: "/api/user/toss/amount", body: `{"amount":1300,"amount_mode":"krw","payment_method":"toss"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTossTopUpAmountModeControllerTest(t)
			user := model.User{
				Id:       401,
				Username: "toss-capacity-user",
				Password: "x",
				Role:     common.RoleCommonUser,
				AffCode:  "toss-capacity-user",
				Group:    "default",
				Quota:    common.MaxQuota - 1,
			}
			require.NoError(t, model.DB.Create(&user).Error)

			res := performOrganizationRequest(tc.handler, user, http.MethodPost, tc.path, tc.body)

			requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
			requireTopUpCount(t, 0)
		})
	}
}

func TestRequestTossPayAllowsTopUpThatExactlyFillsUserWallet(t *testing.T) {
	setupTossTopUpAmountModeControllerTest(t)
	// One quota-mode unit credits exactly QuotaPerUnit (500000). Settlement
	// accepts a final balance equal to common.MaxQuota, so the pre-check must too.
	user := model.User{
		Id:       402,
		Username: "toss-capacity-boundary-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "toss-capacity-boundary-user",
		Group:    "default",
		Quota:    common.MaxQuota - 500000,
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestTossPay,
		user,
		http.MethodPost,
		"/api/user/toss/pay",
		`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
	)

	require.Contains(t, res.Body.String(), `"message":"success"`)
	requireTopUpCount(t, 1)
}

func TestRequestOrganizationTossPayChecksOrganizationWalletCapacity(t *testing.T) {
	t.Run("full organization wallet rejects before order creation", func(t *testing.T) {
		setupTossTopUpAmountModeControllerTest(t)
		owner, _ := createPaymentCapacityOrganization(t, 411, 51, 0, common.MaxQuota-1)

		res := performOrganizationRequest(
			RequestOrganizationTossPay,
			owner,
			http.MethodPost,
			"/api/organization/toss/pay",
			`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
		)

		requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
		requireTopUpCount(t, 0)
	})

	t.Run("full owner wallet does not block an organization top-up", func(t *testing.T) {
		setupTossTopUpAmountModeControllerTest(t)
		owner, org := createPaymentCapacityOrganization(t, 412, 52, common.MaxQuota-1, 0)

		res := performOrganizationRequest(
			RequestOrganizationTossPay,
			owner,
			http.MethodPost,
			"/api/organization/toss/pay",
			`{"amount":1,"amount_mode":"quota","payment_method":"toss"}`,
		)

		require.Contains(t, res.Body.String(), `"message":"success"`)
		var topUp model.TopUp
		require.NoError(t, model.DB.First(&topUp).Error)
		require.Equal(t, model.TopUpTargetTypeOrganization, topUp.TargetType)
		require.Equal(t, org.Id, topUp.TargetId)
	})
}
