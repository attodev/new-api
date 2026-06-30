package controller

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupWalletAutoRechargeControllerTestDB(t *testing.T) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.TopUp{},
		&model.UserBillingKey{},
		&model.WalletAutoRecharge{},
	))
}

func enableTossBillingForTest(t *testing.T) {
	t.Helper()

	originalEnabled := setting.TossEnabled
	originalClientKey := setting.TossClientKey
	originalSecretKey := setting.TossSecretKey
	originalTestMode := setting.TossTestMode
	originalServerAddress := system_setting.ServerAddress

	t.Cleanup(func() {
		setting.TossEnabled = originalEnabled
		setting.TossClientKey = originalClientKey
		setting.TossSecretKey = originalSecretKey
		setting.TossTestMode = originalTestMode
		system_setting.ServerAddress = originalServerAddress
	})

	setting.TossEnabled = true
	setting.TossTestMode = false
	setting.TossClientKey = "ck_test_wallet_auto"
	setting.TossSecretKey = "sk_test_wallet_auto"
	system_setting.ServerAddress = "https://wallet.example.com"
}

func TestOrganizationMemberCannotCreateWalletAutoRecharge(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	member := model.User{
		Id:               1,
		Username:         "member",
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   1,
		OrganizationRole: model.OrganizationRoleMember,
		AffCode:          "member-wallet-auto",
	}
	org := model.Organization{
		Id:          1,
		Name:        "Acme",
		OwnerUserId: 2,
		Quota:       1000,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		member,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"amount":10,"interval_unit":"month","interval_value":1,"charge_immediately":false}`,
	)

	requireOrganizationApiError(t, res, "organization owner permission required")
}

func TestOrganizationUserWithoutOwnerRoleCannotCreateWalletAutoRechargeEvenIfOrgOwnerMatches(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	member := model.User{
		Id:               8,
		Username:         "member-owner-mismatch",
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   12,
		OrganizationRole: model.OrganizationRoleMember,
		AffCode:          "member-owner-mismatch-wallet-auto",
	}
	org := model.Organization{
		Id:          12,
		Name:        "Acme",
		OwnerUserId: member.Id,
		Quota:       1000,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		member,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"amount":10,"interval_unit":"month","interval_value":1,"charge_immediately":false}`,
	)

	requireOrganizationApiError(t, res, "organization owner permission required")
}

func TestOrganizationOwnerCanCreateScheduledWalletAutoRecharge(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	owner := model.User{
		Id:               1,
		Username:         "owner",
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   7,
		OrganizationRole: model.OrganizationRoleOwner,
		AffCode:          "owner-wallet-auto",
	}
	org := model.Organization{
		Id:          7,
		Name:        "Acme",
		OwnerUserId: 1,
		Quota:       1000,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		owner,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"amount":10,"interval_unit":"month","interval_value":1,"charge_immediately":true}`,
	)

	require.Equal(t, http.StatusOK, res.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			ClientKey   string `json:"client_key"`
			CustomerKey string `json:"customer_key"`
			TradeNo     string `json:"trade_no"`
			SuccessURL  string `json:"success_url"`
			FailURL     string `json:"fail_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "ck_test_wallet_auto", payload.Data.ClientKey)
	require.NotEmpty(t, payload.Data.CustomerKey)
	require.NotEmpty(t, payload.Data.TradeNo)
	require.Contains(t, payload.Data.SuccessURL, "/api/wallet/auto-recharge/toss/confirm?trade_no=")
	require.Contains(t, payload.Data.FailURL, "/api/wallet/auto-recharge/toss/fail?trade_no=")

	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&policy, "auth_trade_no = ?", payload.Data.TradeNo).Error)
	require.Equal(t, model.WalletAutoRechargeStatusPending, policy.Status)
	require.Equal(t, model.TopUpTargetTypeOrganization, policy.TargetType)
	require.Equal(t, org.Id, policy.TargetId)
	require.Equal(t, owner.Id, policy.OwnerUserId)
	require.Equal(t, payload.Data.CustomerKey, policy.CustomerKey)
	require.True(t, policy.ChargeImmediately)
}

func TestCancelOrganizationWalletAutoRechargeRejectsNonOwner(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)

	member := model.User{
		Id:               3,
		Username:         "member",
		Password:         "x",
		Role:             common.RoleCommonUser,
		OrganizationId:   9,
		OrganizationRole: model.OrganizationRoleMember,
		AffCode:          "member-wallet-cancel",
	}
	org := model.Organization{
		Id:          9,
		Name:        "Acme",
		OwnerUserId: 1,
		Quota:       1000,
		Status:      model.OrganizationStatusEnabled,
	}
	activeKey := "organization:9:scheduled"
	policy := model.WalletAutoRecharge{
		Id:            12,
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeOrganization,
		TargetId:      9,
		OwnerUserId:   1,
		Amount:        10,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Status:        model.WalletAutoRechargeStatusActive,
		ActiveKey:     &activeKey,
	}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&policy).Error)

	res := performOrganizationRequest(
		CancelOrganizationWalletAutoRecharge,
		member,
		http.MethodDelete,
		"/api/organization/wallet/auto-recharge/12",
		"",
		gin.Param{Key: "id", Value: "12"},
	)

	requireOrganizationApiError(t, res, "organization owner permission required")
}

func TestWalletAutoRechargeTossConfirmIssueFailureReleasesPendingPolicy(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey string) (*tossBillingIssueResponse, int, error) {
		return nil, 0, errors.New("issue failed")
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
	})

	user := model.User{
		Id:       6,
		Username: "issue-failure-owner",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "wallet-auto-issue-failure",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-6",
		AuthTradeNo:   "wallet-auto-issue-failure-trade",
		Amount:        10,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.NoError(t, err)

	_, err = model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-6",
		AuthTradeNo:   "wallet-auto-issue-failure-duplicate-before-release",
		Amount:        10,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.ErrorContains(t, err, "active wallet auto recharge already exists")

	res := performOrganizationRequest(
		WalletAutoRechargeTossConfirm,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no=wallet-auto-issue-failure-trade&authKey=fake-auth&customerKey=customer-6",
		"",
	)

	require.Equal(t, http.StatusFound, res.Code)
	location, err := url.QueryUnescape(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/wallet?wallet_auto_recharge=failed", location)

	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.NotEqual(t, model.WalletAutoRechargeStatusPending, reloaded.Status)
	require.Nil(t, reloaded.ActiveKey)

	retryPolicy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-6",
		AuthTradeNo:   "wallet-auto-issue-failure-after-release",
		Amount:        10,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.NoError(t, err)
	require.NotZero(t, retryPolicy.Id)
}

func TestWalletAutoRechargeTossFailCancelsPendingPolicy(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)

	user := model.User{
		Id:       5,
		Username: "owner",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "wallet-auto-fail",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.WalletAutoRecharge{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		Amount:        10,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Status:        model.WalletAutoRechargeStatusPending,
		CustomerKey:   "customer-5",
		AuthTradeNo:   "wallet-auto-fail-trade",
	}).Error)

	res := performOrganizationRequest(
		WalletAutoRechargeTossFail,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/fail?trade_no=wallet-auto-fail-trade&code=USER_CANCEL",
		"",
	)

	require.Equal(t, http.StatusFound, res.Code)
	location, err := url.QueryUnescape(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/wallet?wallet_auto_recharge=failed", location)

	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&policy, "auth_trade_no = ?", "wallet-auto-fail-trade").Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
}
