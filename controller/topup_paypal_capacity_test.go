package controller

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// setupPayPalCapacityControllerTest isolates RequestPayPalPay from the real
// PayPal API: every outbound request is counted and failed locally, so a test
// can prove a capacity rejection happens before any provider order exists.
func setupPayPalCapacityControllerTest(t *testing.T) *atomic.Int32 {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	originalUnitPrice := setting.PayPalUnitPrice
	originalMinTopUp := setting.PayPalMinTopUp
	originalClient := http.DefaultClient

	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	setting.PayPalUnitPrice = 1
	setting.PayPalMinTopUp = 1

	var providerCalls atomic.Int32
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, errors.New("paypal provider is unavailable in tests")
	})}

	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
		setting.PayPalUnitPrice = originalUnitPrice
		setting.PayPalMinTopUp = originalMinTopUp
		http.DefaultClient = originalClient
	})
	return &providerCalls
}

func requireTopUpCount(t *testing.T, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
	require.Equal(t, want, count)
}

func TestRequestPayPalPayRejectsTopUpThatWouldOverflowUserWallet(t *testing.T) {
	providerCalls := setupPayPalCapacityControllerTest(t)
	user := model.User{
		Id:       301,
		Username: "paypal-capacity-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "paypal-capacity-user",
		Group:    "default",
		Quota:    common.MaxQuota - 1,
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestPayPalPay,
		user,
		http.MethodPost,
		"/api/user/paypal/pay",
		`{"amount":10,"payment_method":"paypal"}`,
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
	require.Zero(t, providerCalls.Load(), "no PayPal order may be created for an uncreditable top-up")
	requireTopUpCount(t, 0)
}

func TestRequestPayPalAmountRejectsTopUpThatWouldOverflowUserWallet(t *testing.T) {
	setupPayPalCapacityControllerTest(t)
	user := model.User{
		Id:       302,
		Username: "paypal-capacity-quote-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "paypal-capacity-quote-user",
		Group:    "default",
		Quota:    common.MaxQuota - 1,
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestPayPalAmount,
		user,
		http.MethodPost,
		"/api/user/paypal/amount",
		`{"amount":10,"payment_method":"paypal"}`,
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
}

func TestRequestOrganizationPayPalPayChecksOrganizationWalletCapacity(t *testing.T) {
	t.Run("full organization wallet rejects before provider order", func(t *testing.T) {
		providerCalls := setupPayPalCapacityControllerTest(t)
		owner, _ := createPaymentCapacityOrganization(t, 311, 41, 0, common.MaxQuota-1)

		res := performOrganizationRequest(
			RequestOrganizationPayPalPay,
			owner,
			http.MethodPost,
			"/api/organization/paypal/pay",
			`{"amount":10,"payment_method":"paypal"}`,
		)

		requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
		require.Zero(t, providerCalls.Load())
		requireTopUpCount(t, 0)
	})

	t.Run("full owner wallet does not block an organization top-up", func(t *testing.T) {
		providerCalls := setupPayPalCapacityControllerTest(t)
		owner, _ := createPaymentCapacityOrganization(t, 312, 42, common.MaxQuota-1, 0)

		res := performOrganizationRequest(
			RequestOrganizationPayPalPay,
			owner,
			http.MethodPost,
			"/api/organization/paypal/pay",
			`{"amount":10,"payment_method":"paypal"}`,
		)

		// The capacity check passes against the organization wallet, so the
		// handler proceeds to the (stubbed, failing) provider call.
		requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgPaymentStartFailed))
		require.Positive(t, providerCalls.Load())
	})
}

// createPaymentCapacityOrganization creates an organization owner with the
// given personal wallet balance and an organization with the given balance.
func createPaymentCapacityOrganization(t *testing.T, ownerId int, orgId int, ownerQuota int64, orgQuota int64) (model.User, model.Organization) {
	t.Helper()
	owner := model.User{
		Id:               ownerId,
		Username:         "capacity-owner-" + common.GetRandomString(6),
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   orgId,
		OrganizationRole: model.OrganizationRoleOwner,
		AffCode:          "capacity-owner-" + common.GetRandomString(6),
		Group:            "default",
		Quota:            ownerQuota,
	}
	org := model.Organization{
		Id:          orgId,
		Name:        "capacity-org-" + common.GetRandomString(6),
		OwnerUserId: ownerId,
		Quota:       orgQuota,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	return owner, org
}
