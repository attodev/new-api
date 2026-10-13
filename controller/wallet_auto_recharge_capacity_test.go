package controller

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func requireNoWalletAutoRechargePolicies(t *testing.T) {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Count(&count).Error)
	require.Zero(t, count)
}

func walletAutoRechargePresetBody(preset *model.WalletAutoRechargePreset) string {
	return `{"preset_id":` + strconv.Itoa(preset.Id) + `,"preset_fingerprint":"` + preset.TermsFingerprint + `"}`
}

func TestRequestWalletThresholdRechargeRejectsThresholdPlusChargeAboveCeiling(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	user := model.User{Id: 501, Username: "threshold-capacity", Password: "x", Role: common.RoleCommonUser, AffCode: "threshold-capacity"}
	require.NoError(t, model.DB.Create(&user).Error)
	// The policy charges whenever the balance is at or below the threshold, so
	// a threshold this close to the ceiling can never be credited.
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:           model.WalletAutoRechargeTypeThreshold,
		TargetScope:    model.WalletAutoRechargePresetTargetUser,
		Name:           "threshold-near-ceiling",
		Amount:         10000,
		ThresholdQuota: common.MaxQuota - 1,
		Enabled:        true,
	})

	res := performOrganizationRequest(
		RequestWalletThresholdRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/threshold",
		walletAutoRechargePresetBody(preset),
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
	requireNoWalletAutoRechargePolicies(t)
}

func TestRequestWalletThresholdRechargeIgnoresCurrentBalanceAboveThreshold(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	// A full wallet is not charged until it falls to the threshold, at which
	// point threshold + charge fits; registering the policy must be allowed.
	user := model.User{Id: 502, Username: "threshold-full-wallet", Password: "x", Role: common.RoleCommonUser, AffCode: "threshold-full-wallet", Quota: common.MaxQuota - 1}
	require.NoError(t, model.DB.Create(&user).Error)
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:           model.WalletAutoRechargeTypeThreshold,
		TargetScope:    model.WalletAutoRechargePresetTargetUser,
		Name:           "threshold-low",
		Amount:         10000,
		ThresholdQuota: 1000,
		Enabled:        true,
	})

	res := performOrganizationRequest(
		RequestWalletThresholdRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/threshold",
		walletAutoRechargePresetBody(preset),
	)

	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestRequestWalletScheduledRechargeRejectsChargeThatWouldOverflowWallet(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	user := model.User{Id: 503, Username: "scheduled-capacity", Password: "x", Role: common.RoleCommonUser, AffCode: "scheduled-capacity", Quota: common.MaxQuota - 1}
	require.NoError(t, model.DB.Create(&user).Error)
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetScope:   model.WalletAutoRechargePresetTargetUser,
		Name:          "scheduled-capacity",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})

	res := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		walletAutoRechargePresetBody(preset),
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
	requireNoWalletAutoRechargePolicies(t)
}

func TestRequestOrganizationWalletScheduledRechargeChecksOrganizationWallet(t *testing.T) {
	setupWalletAutoRechargeControllerTestDB(t)
	enableTossBillingForTest(t)
	owner, _ := createPaymentCapacityOrganization(t, 511, 61, 0, common.MaxQuota-1)
	preset := createWalletAutoRechargePresetForTest(t, model.WalletAutoRechargePresetRequest{
		Type:          model.WalletAutoRechargeTypeScheduled,
		TargetScope:   model.WalletAutoRechargePresetTargetOrganization,
		Name:          "org-scheduled-capacity",
		Amount:        10000,
		IntervalUnit:  model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})

	res := performOrganizationRequest(
		RequestOrganizationWalletScheduledRecharge,
		owner,
		http.MethodPost,
		"/api/organization/wallet/auto-recharge/scheduled",
		walletAutoRechargePresetBody(preset),
	)

	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgTopupQuotaCapacityExceeded))
	requireNoWalletAutoRechargePolicies(t)
}
