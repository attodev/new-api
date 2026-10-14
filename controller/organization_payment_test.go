package controller

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization wallet top-ups check the organization wallet's
// capacity, not the owner's personal wallet, before any provider order exists.

const organizationTopUpLimitExceededBody = `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// setupOrganizationPaymentCapacityTest reuses develop's PayPal capacity setup
// (sqlite DB, QuotaPerUnit 500000, USD display, PayPal unit price/min 1), adds
// the organization and top-up tables, and fails every outbound HTTP request
// locally so a test can prove a rejection happens before a provider call.
func setupOrganizationPaymentCapacityTest(t *testing.T) *atomic.Int32 {
	t.Helper()
	setupPayPalCapacityTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))
	require.NoError(t, model.MigrateForkOrganizationTables(model.DB))

	previousRedis := common.RedisEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousClient := http.DefaultClient
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	var providerCalls atomic.Int32
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, errors.New("payment provider is unavailable in tests")
	})}
	t.Cleanup(func() {
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		http.DefaultClient = previousClient
	})
	return &providerCalls
}

func requireOrganizationTopUpCount(t *testing.T, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Count(&count).Error)
	require.Equal(t, want, count)
}

// createPaymentCapacityOrganization creates an organization owner with the
// given personal wallet balance and an organization with the given balance.
func createPaymentCapacityOrganization(t *testing.T, ownerId int, orgId int, ownerQuota int, orgQuota int64) (model.User, model.Organization) {
	t.Helper()
	owner := model.User{
		Id:               ownerId,
		Username:         "capacity-owner-" + common.GetRandomString(6),
		Password:         "x",
		Role:             common.RoleCommonUser,
		Status:           common.UserStatusEnabled,
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

func TestRequestOrganizationPayPalPayChecksOrganizationWalletCapacity(t *testing.T) {
	t.Run("full organization wallet rejects before provider order", func(t *testing.T) {
		providerCalls := setupOrganizationPaymentCapacityTest(t)
		owner, _ := createPaymentCapacityOrganization(t, 311, 41, 0, int64(common.MaxWalletQuota-1))

		res := performOrganizationRequest(
			RequestOrganizationPayPalPay,
			owner,
			http.MethodPost,
			"/api/organization/paypal/pay",
			`{"amount":10,"payment_method":"paypal"}`,
		)

		assert.Equal(t, http.StatusOK, res.Code)
		assert.JSONEq(t, organizationTopUpLimitExceededBody, res.Body.String())
		require.Zero(t, providerCalls.Load(), "no PayPal order may be created for an uncreditable top-up")
		requireOrganizationTopUpCount(t, 0)
	})

	t.Run("full owner wallet does not block an organization top-up", func(t *testing.T) {
		providerCalls := setupOrganizationPaymentCapacityTest(t)
		owner, _ := createPaymentCapacityOrganization(t, 312, 42, common.MaxWalletQuota-1, 0)

		res := performOrganizationRequest(
			RequestOrganizationPayPalPay,
			owner,
			http.MethodPost,
			"/api/organization/paypal/pay",
			`{"amount":10,"payment_method":"paypal"}`,
		)

		// The capacity check passes against the organization wallet, so the
		// handler proceeds to the (stubbed, failing) provider call.
		assert.Equal(t, http.StatusOK, res.Code)
		assert.JSONEq(t, `{"message":"error","data":"Failed to start payment","message_key":"Failed to start payment"}`, res.Body.String())
		require.Positive(t, providerCalls.Load())
		requireOrganizationTopUpCount(t, 0)
	})
}

func TestRequestEpayRejectsTopUpThatWouldOverflowOrganizationWallet(t *testing.T) {
	setupOrganizationPaymentCapacityTest(t)
	owner, _ := createPaymentCapacityOrganization(t, 603, 71, 0, int64(common.MaxWalletQuota-1))

	res := performOrganizationRequest(
		RequestOrganizationEpay,
		owner,
		http.MethodPost,
		"/api/organization/pay",
		`{"amount":10,"payment_method":"alipay"}`,
	)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.JSONEq(t, organizationTopUpLimitExceededBody, res.Body.String())
	requireOrganizationTopUpCount(t, 0)
}

// fork: O3 — a disabled organization wallet cannot be topped up.
func TestDisabledOrganizationTopUpIsRefused(t *testing.T) {
	providerCalls := setupOrganizationPaymentCapacityTest(t)
	owner, org := createPaymentCapacityOrganization(t, 331, 61, 0, 0)
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", org.Id).
		Update("status", model.OrganizationStatusDisabled).Error)

	for _, handler := range []struct {
		name string
		fn   func(c *gin.Context)
		path string
		body string
	}{
		{"paypal pay", RequestOrganizationPayPalPay, "/api/organization/paypal/pay", `{"amount":10,"payment_method":"paypal"}`},
		{"paypal amount", RequestOrganizationPayPalAmount, "/api/organization/paypal/amount", `{"amount":10}`},
		{"epay", RequestOrganizationEpay, "/api/organization/pay", `{"amount":10,"payment_method":"alipay"}`},
	} {
		res := performOrganizationRequest(handler.fn, owner, http.MethodPost, handler.path, handler.body)
		assert.JSONEq(t, `{"message":"error","data":"organization is disabled","message_key":"organization is disabled"}`, res.Body.String(), handler.name)
	}
	require.Zero(t, providerCalls.Load())
	requireOrganizationTopUpCount(t, 0)
}
