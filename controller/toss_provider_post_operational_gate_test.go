package controller

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func withTossProviderPostOperationalGateTestState(t *testing.T) {
	t.Helper()
	originalEnabled := setting.TossEnabled
	originalBillingEnabled := setting.TossBillingEnabled
	originalWalletEnabled := setting.TossWalletAutoRechargeEnabled
	paymentSetting := operation_setting.GetPaymentSetting()
	originalCompliance := paymentSetting.ComplianceConfirmed
	originalTerms := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		setting.TossEnabled = originalEnabled
		setting.TossBillingEnabled = originalBillingEnabled
		setting.TossWalletAutoRechargeEnabled = originalWalletEnabled
		paymentSetting.ComplianceConfirmed = originalCompliance
		paymentSetting.ComplianceTermsVersion = originalTerms
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestTossGeneralConfirmHTTPBoundaryRechecksOperationalGate(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossEnabled = false

	result, status, err := confirmTossPaymentWithSecret(
		context.Background(), "pay_operational_gate", "toss_operational_gate", 1000,
		"test_sk_operational_gate", "idem_operational_gate",
	)
	require.ErrorContains(t, err, "operationally disabled")
	require.Nil(t, result)
	require.Zero(t, status)
}

func TestTossSubscriptionBillingHTTPBoundariesRecheckOperationalGate(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = false

	issued, issueStatus, issueErr := issueTossBillingKeyWithSecret(
		context.Background(), "auth_operational_gate", "customer_operational_gate",
		"issue_operational_gate", "test_sk_operational_gate",
	)
	require.ErrorIs(t, issueErr, model.ErrTossBillingOperationallyDisabled)
	require.Nil(t, issued)
	require.Zero(t, issueStatus)

	charged, chargeStatus, chargeErr := chargeTossBillingWithSecret(
		context.Background(), "billing_operational_gate", "customer_operational_gate",
		"test_sk_operational_gate", "subscription_operational_gate", "subscription", 1000,
	)
	require.ErrorIs(t, chargeErr, model.ErrTossBillingOperationallyDisabled)
	require.Nil(t, charged)
	require.Zero(t, chargeStatus)
}

func TestTossWalletBillingIssueHTTPBoundaryRechecksSeparateGate(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = true
	setting.TossWalletAutoRechargeEnabled = false

	issued, status, err := issueTossBillingKeyWithSecret(
		model.WithTossWalletAutoRechargeIssueContext(context.Background()),
		"auth_wallet_operational_gate", "customer_wallet_operational_gate",
		"wallet_issue_operational_gate", "test_sk_operational_gate",
	)
	require.ErrorIs(t, err, model.ErrTossBillingOperationallyDisabled)
	require.Nil(t, issued)
	require.Zero(t, status)
}

func TestTossBillingIssueCleanupReplayRemainsAvailableWhileDisabled(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = false
	setting.TossWalletAutoRechargeEnabled = false

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		postCalls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/billing/authorizations/issue", r.URL.EscapedPath())
		require.Equal(t, "cleanup_exact_order", r.Header.Get("Idempotency-Key"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"billingKey":"billing_cleanup_exact","customerKey":"customer_cleanup_exact","card":{"company":"현대","number":"433012******1234"}}`,
			)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKeyWithSecret(
		model.WithTossBillingIssueCleanupContext(context.Background()),
		"auth_cleanup_exact", "customer_cleanup_exact", "cleanup_exact_order", "test_sk_cleanup_exact",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, issued)
	require.Equal(t, "billing_cleanup_exact", issued.BillingKey)
	require.Equal(t, 1, postCalls)
}

func TestTossBillingIssueCleanupReplayRequiresPinnedCredential(t *testing.T) {
	withTossProviderPostOperationalGateTestState(t)
	setting.TossBillingEnabled = false

	issued, status, err := issueTossBillingKeyWithSecret(
		model.WithTossBillingIssueCleanupContext(context.Background()),
		"auth_cleanup_missing_secret", "customer_cleanup_missing_secret", "cleanup_missing_secret", "",
	)
	require.ErrorContains(t, err, "exact secret")
	require.Nil(t, issued)
	require.Zero(t, status)
}

func TestTossBillingIssueCleanupReplayBypassesUnattestedConfigWithExactNamespace(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   "__internal_toss_config_write_lock",
		Value: "unattested_cleanup_test_revision",
	}).Error)
	_, freshErr := model.GetFreshTossConfigSnapshot()
	require.ErrorIs(t, freshErr, model.ErrTossConfigRevisionStale)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})

	postCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		postCalls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/billing/authorizations/issue", r.URL.EscapedPath())
		require.Equal(t, "cleanup_unattested_order", r.Header.Get("Idempotency-Key"))
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("test_sk_cleanup_unattested:")), r.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"billingKey":"billing_cleanup_unattested","customerKey":"customer_cleanup_unattested","card":{"company":"현대","number":"433012******1234"}}`,
			)),
		}, nil
	})}
	tossAPIBase = "https://api.test.tosspayments.local"

	issued, status, err := issueTossBillingKeyWithSecret(
		model.WithTossBillingIssueCleanupContext(context.Background()),
		"auth_cleanup_unattested", "customer_cleanup_unattested",
		"cleanup_unattested_order", "test_sk_cleanup_unattested",
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, issued)
	require.Equal(t, "billing_cleanup_unattested", issued.BillingKey)
	require.Equal(t, 1, postCalls)

	ordinary, ordinaryStatus, ordinaryErr := issueTossBillingKeyWithSecret(
		context.Background(), "auth_unattested_blocked", "customer_unattested_blocked",
		"unattested_blocked_order", "test_sk_cleanup_unattested",
	)
	require.ErrorIs(t, ordinaryErr, model.ErrTossBillingOperationallyDisabled)
	require.ErrorContains(t, ordinaryErr, model.ErrTossConfigRevisionStale.Error())
	require.Nil(t, ordinary)
	require.Zero(t, ordinaryStatus)
	require.Equal(t, 1, postCalls, "ordinary ISSUE must fail before the provider POST")
}
