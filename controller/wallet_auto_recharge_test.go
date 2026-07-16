package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	operation_setting "github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWalletAutoRechargeRotatedCredential404NeverAuthorizesNewPost(t *testing.T) {
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	calls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"INVALID_API_KEY","message":"rotated"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"NOT_FOUND_PAYMENT","message":"not visible"}`)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	result, err := lookupWalletAutoRechargeTossPayment(
		context.Background(),
		[]string{"sk_wallet_attempt", "sk_wallet_rotated"},
		"wallet_auto_77_rotated_404",
		10000,
	)
	require.Nil(t, result)
	require.ErrorIs(t, err, model.ErrTossBillingChargePending)
	require.NotErrorIs(t, err, model.ErrTossBillingPaymentNotFound)
	require.Equal(t, 2, calls)
}

func setupWalletAutoRechargeControllerTestDB(t *testing.T) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.TopUp{},
		&model.SubscriptionOrder{},
		&model.TossPaymentEvent{},
		&model.UserBillingKey{},
		&model.WalletAutoRecharge{},
		&model.WalletAutoRechargePreset{},
		&model.TossRecurringOrderIDProtocolState{},
	))
	require.NoError(t, model.DB.Create(&model.TossRecurringOrderIDProtocolState{Id: 1, WriteVersion: 2}).Error)
}

func TestWalletAutoRechargeRequestDoesNotExposeSDKWhenContractGateDisabled(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossWalletAutoRechargeEnabled": "false",
	}))

	user := model.User{
		Id: 91, Username: "wallet-gate-user", Password: "x",
		Role: common.RoleCommonUser, AffCode: "wallet-gate-user",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/wallet/auto-recharge/scheduled",
		`{"preset_id":1,"preset_fingerprint":"not-exposed"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.NotContains(t, res.Body.String(), `"client_key"`)
	var count int64
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestWalletAutoRechargeRequestRejectsCustomerKeyOutsideSDKContractBeforePendingCreate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		customerKey string
	}{
		{
			name:        "legacy key above v2 limit",
			customerKey: "cust_" + strings.Repeat("a", 46), // 51 bytes: valid server storage, invalid SDK v2 input.
		},
		{
			name:        "character outside v2 allowlist",
			customerKey: "cust/legacy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupWalletAutoRechargeControllerTestDB(t)
			enableTossBillingForTest(t)

			user := model.User{
				Id:              92,
				Username:        "wallet-sdk-customer-key",
				Password:        "x",
				Role:            common.RoleCommonUser,
				AffCode:         "wallet-sdk-customer-key",
				TossCustomerKey: tc.customerKey,
			}
			require.NoError(t, model.DB.Create(&user).Error)
			preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
				Type:              model.WalletAutoRechargeTypeScheduled,
				TargetScope:       model.WalletAutoRechargePresetTargetUser,
				Name:              "wallet-sdk-customer-key",
				Amount:            10000,
				IntervalUnit:      model.WalletAutoRechargeIntervalMonth,
				IntervalValue:     1,
				ChargeImmediately: true,
				Enabled:           true,
			})

			res := performOrganizationRequest(
				RequestWalletScheduledRecharge,
				user,
				http.MethodPost,
				"/api/user/wallet/auto-recharge/scheduled",
				`{"preset_id":`+strconv.Itoa(preset.Id)+`,"preset_fingerprint":"`+preset.TermsFingerprint+`"}`,
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":false`)
			require.NotContains(t, res.Body.String(), `"client_key"`)
			require.NotContains(t, res.Body.String(), `"customer_key"`)
			var count int64
			require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestWalletAutoRechargeProviderPOSTGateIsIndependentFromSubscriptionBilling(t *testing.T) {
	originalBillingEnabled := setting.TossBillingEnabled
	originalWalletAutoRechargeEnabled := setting.TossWalletAutoRechargeEnabled
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossWalletAutoRechargeEnabled = originalWalletAutoRechargeEnabled
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})

	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = false
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	result, err := tossBillingChargeForModel(
		context.Background(),
		"billing-key",
		"customer-key",
		"live_sk_wallet",
		"wallet_auto_91_gate",
		"wallet recharge",
		1000,
	)
	require.Nil(t, result)
	require.ErrorIs(t, err, model.ErrTossBillingOperationallyDisabled)
	require.True(t, setting.TossBillingEnabled)
}

func TestOpaqueWalletAutoRechargeProviderPOSTGateUsesChargeContext(t *testing.T) {
	originalBillingEnabled := setting.TossBillingEnabled
	originalWalletAutoRechargeEnabled := setting.TossWalletAutoRechargeEnabled
	paymentSetting := operation_setting.GetPaymentSetting()
	originalComplianceConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossWalletAutoRechargeEnabled = originalWalletAutoRechargeEnabled
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})

	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = false
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	result, err := tossBillingChargeForModel(
		model.WithTossWalletAutoRechargeChargeContext(context.Background()),
		"billing-key", "customer-key", "live_sk_wallet",
		"twa_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "wallet recharge", 1000,
	)
	require.Nil(t, result)
	require.ErrorIs(t, err, model.ErrTossBillingOperationallyDisabled)
}

func enableTossBillingForTest(t *testing.T) {
	t.Helper()

	originalTossConfig := setting.GetTossConfigSnapshot()
	originalServerAddress := system_setting.ServerAddress
	originalComplianceConfirmed := operation_setting.GetPaymentSetting().ComplianceConfirmed
	originalComplianceTermsVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion

	t.Cleanup(func() {
		restoreTossConfigForOptionTest(t, originalTossConfig)
		system_setting.ServerAddress = originalServerAddress
		paymentSetting := operation_setting.GetPaymentSetting()
		paymentSetting.ComplianceConfirmed = originalComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = originalComplianceTermsVersion
	})

	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "true",
		"TossTestMode":                  "false",
		"TossClientKey":                 "live_ck_wallet_auto",
		"TossSecretKey":                 "live_sk_wallet_auto",
		"TossBillingClientKey":          "live_ck_wallet_auto_billing",
		"TossBillingSecretKey":          "live_sk_wallet_auto_billing",
		"TossUnitPrice":                 "1000",
	}))
	system_setting.ServerAddress = "https://wallet.example.com"
	paymentSetting := operation_setting.GetPaymentSetting()
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func createWalletAutoRechargePresetForTest(t *testing.T, req model.WalletAutoRechargePresetRequest) *model.WalletAutoRechargePreset {
	t.Helper()
	preset, err := model.CreateWalletAutoRechargePreset(req)
	require.NoError(t, err)
	return preset
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
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetScope:   model.WalletAutoRechargePresetTargetOrganization,
		Name:          "org-scheduled-member",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		member,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"preset_id":`+strconv.Itoa(preset.Id)+`,"preset_fingerprint":"`+preset.TermsFingerprint+`"}`,
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
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetScope:   model.WalletAutoRechargePresetTargetOrganization,
		Name:          "org-scheduled-owner-mismatch",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		member,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"preset_id":`+strconv.Itoa(preset.Id)+`,"preset_fingerprint":"`+preset.TermsFingerprint+`"}`,
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
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:              model.WalletAutoRechargeTypeScheduled,
		TargetScope:       model.WalletAutoRechargePresetTargetOrganization,
		Name:              "org-scheduled-owner",
		Amount:            10000,
		IntervalUnit:      model.WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
		Enabled:           true,
	})

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		owner,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		`{"preset_id":`+strconv.Itoa(preset.Id)+`,"preset_fingerprint":"`+preset.TermsFingerprint+`"}`,
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
	require.Equal(t, "live_ck_wallet_auto_billing", payload.Data.ClientKey)
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
	require.False(t, policy.ChargeImmediately)
	require.NotEmpty(t, policy.ProviderCredential)
	require.Equal(t, model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"), policy.ProviderClientKeyHash)
}

func TestWalletAutoRechargeRejectsInvalidServerAddressBeforePendingCreate(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	system_setting.ServerAddress = ""

	user := model.User{
		Id:       31,
		Username: "invalid-server-wallet-auto",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "invalid-server-wallet-auto",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:              model.WalletAutoRechargeTypeScheduled,
		TargetScope:       model.WalletAutoRechargePresetTargetUser,
		Name:              "user-scheduled-invalid-server",
		Amount:            10000,
		IntervalUnit:      model.WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
		Enabled:           true,
	})

	res := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		`{"preset_id":`+strconv.Itoa(preset.Id)+`,"preset_fingerprint":"`+preset.TermsFingerprint+`"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.False(t, payload.Success)

	var count int64
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
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
		Amount:        10000,
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

func TestCancelPendingWalletAutoRechargeByTradeNoReleasesPolicy(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)

	user := model.User{
		Id:       41,
		Username: "pending-cleanup-owner",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "pending-cleanup-owner",
	}
	require.NoError(t, model.DB.Create(&user).Error)

	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-41",
		AuthTradeNo:   "wallet-auto-pending-cleanup",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.NoError(t, err)

	res := performOrganizationRequest(
		CancelPendingWalletAutoRecharge,
		user,
		http.MethodDelete,
		"/api/user/wallet/auto-recharge/pending/wallet-auto-pending-cleanup",
		"",
		gin.Param{Key: "trade_no", Value: "wallet-auto-pending-cleanup"},
	)

	require.Equal(t, http.StatusOK, res.Code)

	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, reloaded.Status)
	require.Nil(t, reloaded.ActiveKey)

	retry, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-41",
		AuthTradeNo:   "wallet-auto-pending-cleanup-retry",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.NoError(t, err)
	require.NotZero(t, retry.Id)
}

func TestWalletAutoRechargeTossConfirmIssueFailureReleasesPendingPolicy(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		return nil, http.StatusBadRequest, errors.New("issue failed")
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

	providerCredential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:                  model.WalletAutoRechargeTypeScheduled,
		TargetType:            model.TopUpTargetTypeUser,
		TargetId:              user.Id,
		OwnerUserId:           user.Id,
		CustomerKey:           "customer-6",
		AuthTradeNo:           "wallet-auto-issue-failure-trade",
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
		Amount:                10000,
		IntervalUnit:          model.WalletAutoRechargeIntervalMonth,
		IntervalValue:         1,
	})
	require.NoError(t, err)

	_, err = model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetType:    model.TopUpTargetTypeUser,
		TargetId:      user.Id,
		OwnerUserId:   user.Id,
		CustomerKey:   "customer-6",
		AuthTradeNo:   "wallet-auto-issue-failure-duplicate-before-release",
		Amount:        10000,
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
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
	})
	require.NoError(t, err)
	require.NotZero(t, retryPolicy.Id)
}

func TestWalletAutoRechargeTossConfirmDoesNotCancelWhenImmediateChargeNeedsReconciliation(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossTestMode":             "true",
		"TossBillingTestClientKey": "live_ck_wallet_auto_billing",
		"TossBillingTestSecretKey": "live_sk_wallet_auto_billing",
	}))
	originalUnitPrice := setting.TossUnitPrice
	setting.TossUnitPrice = 1000
	t.Cleanup(func() {
		setting.TossUnitPrice = originalUnitPrice
	})

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalCharger := walletAutoRechargeTossCharger
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		return &tossBillingIssueResponse{
			BillingKey:  "billing-key",
			CustomerKey: customerKey,
			Card: struct {
				Company    string `json:"company"`
				IssuerCode string `json:"issuerCode"`
				Number     string `json:"number"`
			}{Company: "card", Number: "****1234"},
		}, http.StatusOK, nil
	}
	walletAutoRechargeTossCharger = func(ctx context.Context, billingKey, customerKey, secretKey, orderID, orderName string, amount int64) (*model.TossBillingChargeResult, error) {
		return &model.TossBillingChargeResult{Done: true, Total: amount}, nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeTossCharger = originalCharger
	})

	user := model.User{
		Id:               7,
		Username:         "activation-reconcile-owner",
		Password:         "x",
		Role:             common.RoleCommonUser,
		Group:            "default",
		AffCode:          "wallet-auto-controller-reconcile",
		OrganizationId:   407,
		OrganizationRole: model.OrganizationRoleOwner,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 407, Name: "controller-reconcile", OwnerUserId: user.Id, Status: model.OrganizationStatusEnabled}).Error)

	providerCredential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type:                  model.WalletAutoRechargeTypeScheduled,
		TargetType:            model.TopUpTargetTypeOrganization,
		TargetId:              407,
		OwnerUserId:           user.Id,
		CustomerKey:           "customer-7",
		AuthTradeNo:           "wallet-auto-controller-reconcile",
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
		Amount:                10000,
		IntervalUnit:          model.WalletAutoRechargeIntervalCustom,
		IntervalValue:         1,
		CustomSeconds:         60,
		ChargeImmediately:     true,
	})
	require.NoError(t, err)

	res := performOrganizationRequest(
		WalletAutoRechargeTossConfirm,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no=wallet-auto-controller-reconcile&authKey=fake-auth&customerKey=customer-7",
		"",
	)

	require.Equal(t, http.StatusFound, res.Code)
	location, err := url.QueryUnescape(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/wallet?wallet_auto_recharge=success", location)

	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusActive, reloaded.Status)
	require.NotNil(t, reloaded.ActiveKey)

	var topUp model.TopUp
	require.NoError(t, model.DB.First(&topUp, "trade_no = ?", reloaded.LastTradeNo).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
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
		Amount:        10000,
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
		"/api/wallet/auto-recharge/toss/fail?trade_no=wallet-auto-fail-trade&code=USER_CANCEL&message=Buyer+canceled+authentication",
		"",
	)

	require.Equal(t, http.StatusFound, res.Code)
	location, err := url.Parse(res.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/wallet", location.Path)
	require.Equal(t, "failed", location.Query().Get("wallet_auto_recharge"))
	require.Equal(t, "USER_CANCEL", location.Query().Get("toss_error_code"))
	require.Equal(t, "Buyer canceled authentication", location.Query().Get("toss_error_message"))

	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&policy, "auth_trade_no = ?", "wallet-auto-fail-trade").Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
}

func TestWalletAutoRechargeTossConfirmRevokesKeyWhenOwnerDisabledDuringIssue(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 71, Username: "disable-race-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "disable-race-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "disable-race-customer", AuthTradeNo: "disable-race-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalIssuedRevoker := walletAutoRechargeIssuedBillingKeyRevoker
	revoked := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		require.Equal(t, "disable-race-trade", idempotencyKey)
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("status", common.UserStatusDisabled).Error)
		return &tossBillingIssueResponse{BillingKey: "issued-during-disable", CustomerKey: customerKey}, http.StatusOK, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(ctx context.Context, userId int, tradeNo, customerKey string, issued *tossBillingIssueResponse, secretKey, reason string) error {
		revoked++
		require.Equal(t, user.Id, userId)
		require.Equal(t, "issued-during-disable", issued.BillingKey)
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		require.Equal(t, "target_inactive_after_issue", reason)
		return nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalIssuedRevoker
	})

	res := performOrganizationRequest(
		WalletAutoRechargeTossConfirm,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no=disable-race-trade&authKey=disable-race-auth&customerKey=disable-race-customer",
		"",
	)
	require.Equal(t, http.StatusFound, res.Code)
	require.Equal(t, 1, revoked)
	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, reloaded.Status)
	require.Zero(t, reloaded.BillingKeyId)
	require.Empty(t, reloaded.IssueAuthKey)
	var keyCount int64
	require.NoError(t, model.DB.Model(&model.UserBillingKey{}).Count(&keyCount).Error)
	require.Zero(t, keyCount)
}

func TestWalletAutoRechargeTossConfirmDoesNotIssueKeyWhileContractGateDisabled(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 73, Username: "disabled-issue-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "disabled-issue-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "disabled-issue-customer", AuthTradeNo: "disabled-issue-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	issueCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, 0, errors.New("disabled billing must not issue a key")
	}
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })
	setting.TossWalletAutoRechargeEnabled = false

	res := performOrganizationRequest(
		WalletAutoRechargeTossConfirm,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no=disabled-issue-trade&authKey=disabled-issue-auth&customerKey=disabled-issue-customer",
		"",
	)
	require.Equal(t, http.StatusFound, res.Code)
	require.Zero(t, issueCalls)

	var reloaded model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloaded, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusPending, reloaded.Status)
	require.NotEmpty(t, reloaded.IssueAuthKey)
	require.False(t, reloaded.IssueAttempted)
	require.Empty(t, reloaded.IssueClaimToken)
}

func TestWalletAutoRechargeBillingIssueResponseLossRecoversWithOriginalSecretAndOrder(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 72, Username: "response-loss-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "response-loss-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	oldClientHash := model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing")
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "response-loss-customer", AuthTradeNo: "response-loss-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: oldClientHash,
	})
	require.NoError(t, err)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	issueCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		require.Equal(t, "response-loss-auth", authKey)
		require.Equal(t, "response-loss-customer", customerKey)
		require.Equal(t, "response-loss-trade", idempotencyKey)
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		if issueCalls == 1 {
			return nil, 0, context.DeadlineExceeded
		}
		return &tossBillingIssueResponse{BillingKey: "response-loss-key", CustomerKey: customerKey}, http.StatusOK, nil
	}
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })

	res := performOrganizationRequest(
		WalletAutoRechargeTossConfirm,
		user,
		http.MethodGet,
		"/api/wallet/auto-recharge/toss/confirm?trade_no=response-loss-trade&authKey=response-loss-auth&customerKey=response-loss-customer",
		"",
	)
	require.Equal(t, http.StatusFound, res.Code)
	var pending model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&pending, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusPending, pending.Status)
	require.NotEmpty(t, pending.IssueAuthKey)
	require.True(t, pending.IssueAttempted)
	require.NotEmpty(t, pending.IssueClaimToken)

	// Simulate the stale-claim timeout and a live MID/secret rotation. Recovery
	// must still use the encrypted issue-time secret and identical order id.
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"issue_claim_token": "", "issue_claim_time": 0,
	}).Error)
	setting.TossBillingClientKey = "ck_rotated_wallet_billing"
	setting.TossBillingSecretKey = "sk_rotated_wallet_billing"
	require.NoError(t, model.DB.First(&pending, policy.Id).Error)
	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), pending)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 2, issueCalls)

	var active model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&active, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusActive, active.Status)
	require.Greater(t, active.BillingKeyId, 0)
	require.Empty(t, active.IssueAuthKey)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, active.BillingKeyId).Error)
	require.Equal(t, oldClientHash, key.ProviderClientKeyHash)
	storedSecret, err := model.DecryptProviderCredential(key.ProviderCredential)
	require.NoError(t, err)
	require.Equal(t, "live_sk_wallet_auto_billing", storedSecret)
}

func TestCancelledUncertainWalletBillingIssueRecoversAndRevokesProviderKey(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 73, Username: "cleanup-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "cleanup-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "cleanup-customer", AuthTradeNo: "wallet-cleanup-trade", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))

	_, err = model.CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	require.NoError(t, err)
	var cleanup model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelPending, cleanup.Status)
	require.NotEmpty(t, cleanup.IssueAuthKey)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalRevoker := walletAutoRechargeIssuedBillingKeyRevoker
	issueCalls := 0
	cleanupQueueCalls := 0
	revokeCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		require.Equal(t, "cleanup-auth", authKey)
		require.Equal(t, "cleanup-customer", customerKey)
		require.Equal(t, "wallet-cleanup-trade", idempotencyKey)
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		issued := &tossBillingIssueResponse{BillingKey: "cleanup-provider-key", CustomerKey: customerKey}
		issued.Card.Company = "card"
		issued.Card.Number = "****1234"
		return issued, http.StatusOK, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(ctx context.Context, userID int, tradeNo, customerKey string, issued *tossBillingIssueResponse, secretKey, reason string) error {
		cleanupQueueCalls++
		require.Equal(t, user.Id, userID)
		require.Equal(t, policy.AuthTradeNo, tradeNo)
		require.Equal(t, "cleanup-provider-key", issued.BillingKey)
		require.Equal(t, "live_sk_wallet_auto_billing", secretKey)
		require.Equal(t, "cancel_pending_cleanup", reason)
		if cleanupQueueCalls == 1 {
			return errors.New("cleanup queue unavailable")
		}
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalRevoker
	})

	// Cleanup must proceed even when the non-subscription contract gate is off.
	setting.TossWalletAutoRechargeEnabled = false
	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), cleanup)
	require.Error(t, err)
	require.False(t, resolved)
	require.Equal(t, 1, issueCalls)
	require.Equal(t, 1, cleanupQueueCalls)
	require.Zero(t, revokeCalls)

	// A failed durable cleanup queue must retain both the exact issue snapshot
	// and its cancellation state. After the lease expires, the same idempotent
	// issue can be recovered and queued again without orphaning the provider key.
	require.NoError(t, model.DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelPending, cleanup.Status)
	require.NotEmpty(t, cleanup.IssueAuthKey)
	require.True(t, cleanup.IssueAttempted)
	require.NotEmpty(t, cleanup.IssueClaimToken)
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"issue_claim_token": "", "issue_claim_time": 0,
	}).Error)
	require.NoError(t, model.DB.First(&cleanup, policy.Id).Error)

	resolved, err = reconcileWalletAutoRechargeBillingIssue(context.Background(), cleanup)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 2, issueCalls)
	require.Equal(t, 2, cleanupQueueCalls)
	require.Equal(t, 1, revokeCalls)

	require.NoError(t, model.DB.First(&cleanup, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, cleanup.Status)
	require.Empty(t, cleanup.IssueAuthKey)
	require.False(t, cleanup.IssueAttempted)
}

func TestDisabledWalletGateConvertsAttemptedBillingIssueToCleanupOnly(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 74, Username: "disabled-cleanup-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "disabled-cleanup-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "disabled-cleanup-customer", AuthTradeNo: "wallet-disabled-cleanup", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "disabled-cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.MarkWalletAutoRechargeBillingIssueAttempt(policy.AuthTradeNo, claimToken))
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	originalRevoker := walletAutoRechargeIssuedBillingKeyRevoker
	issueCalls := 0
	cleanupCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		require.Equal(t, "disabled-cleanup-auth", authKey)
		require.Equal(t, policy.AuthTradeNo, idempotencyKey)
		return &tossBillingIssueResponse{BillingKey: "disabled-cleanup-key", CustomerKey: customerKey}, http.StatusOK, nil
	}
	walletAutoRechargeIssuedBillingKeyRevoker = func(ctx context.Context, userID int, tradeNo, customerKey string, issued *tossBillingIssueResponse, secretKey, reason string) error {
		cleanupCalls++
		require.Equal(t, "disabled-cleanup-key", issued.BillingKey)
		require.Equal(t, "billing_disabled_issue_cleanup", reason)
		return nil
	}
	t.Cleanup(func() {
		walletAutoRechargeBillingKeyIssuer = originalIssuer
		walletAutoRechargeIssuedBillingKeyRevoker = originalRevoker
	})

	setting.TossWalletAutoRechargeEnabled = false
	var candidate model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&candidate, policy.Id).Error)
	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), candidate)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, issueCalls, "only the previously attempted idempotent ISSUE may be replayed")
	require.Equal(t, 1, cleanupCalls)

	require.NoError(t, model.DB.First(&candidate, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, candidate.Status)
	require.Zero(t, candidate.BillingKeyId)
	require.Empty(t, candidate.IssueAuthKey)
}

func TestUnattemptedWalletCleanupStateNeverCreatesBillingKey(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)

	user := model.User{Id: 75, Username: "unattempted-cleanup-owner", Password: "x", Role: common.RoleCommonUser, AffCode: "unattempted-cleanup-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	credential, err := model.EncryptProviderCredential("live_sk_wallet_auto_billing")
	require.NoError(t, err)
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser, TargetId: user.Id, OwnerUserId: user.Id,
		CustomerKey: "unattempted-cleanup-customer", AuthTradeNo: "wallet-unattempted-cleanup", Amount: 10000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		ProviderCredential: credential, ProviderClientKeyHash: model.TossBillingClientKeyFingerprint("live_ck_wallet_auto_billing"),
	})
	require.NoError(t, err)
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(policy.AuthTradeNo, "unattempted-cleanup-auth", policy.CustomerKey)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, model.ReleaseWalletAutoRechargeBillingIssueClaim(policy.AuthTradeNo, claimToken))
	// Simulate a legacy/corrupt cancellation row that retained an authorization
	// without ever recording a provider request.
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", policy.Id).
		Update("status", model.WalletAutoRechargeStatusCancelPending).Error)

	originalIssuer := walletAutoRechargeBillingKeyIssuer
	issueCalls := 0
	walletAutoRechargeBillingKeyIssuer = func(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return nil, 0, errors.New("unattempted cleanup must not contact Toss")
	}
	t.Cleanup(func() { walletAutoRechargeBillingKeyIssuer = originalIssuer })

	setting.TossWalletAutoRechargeEnabled = false
	var candidate model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&candidate, policy.Id).Error)
	resolved, err := reconcileWalletAutoRechargeBillingIssue(context.Background(), candidate)
	require.NoError(t, err)
	require.True(t, resolved)
	require.Zero(t, issueCalls)
	require.NoError(t, model.DB.First(&candidate, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, candidate.Status)
	require.Empty(t, candidate.IssueAuthKey)
}
