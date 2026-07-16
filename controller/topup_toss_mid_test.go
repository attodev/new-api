package controller

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func setupTossMIDCredentialCrypto(t *testing.T) {
	t.Helper()
	originalSecret := common.CryptoSecret
	t.Setenv("CRYPTO_SECRET", "toss-mid-controller-test-secret-at-least-32-bytes")
	common.CryptoSecret = "toss-mid-controller-test-secret-at-least-32-bytes"
	t.Cleanup(func() { common.CryptoSecret = originalSecret })
}

func TestTossCredentialRejectionRequiresExplicitAuthenticationCode(t *testing.T) {
	rejection := func(status int, code string) bool {
		return isTossCredentialRejection(status, newTossAPIError(http.MethodPost, status, []byte(`{"code":"`+code+`"}`)))
	}

	require.True(t, rejection(http.StatusUnauthorized, "UNAUTHORIZED_KEY"))
	require.True(t, rejection(http.StatusBadRequest, "INVALID_API_KEY"))
	require.True(t, rejection(http.StatusForbidden, "INCORRECT_BASIC_AUTH_FORMAT"))
	require.False(t, rejection(http.StatusForbidden, "REJECT_CARD_PAYMENT"))
	require.False(t, rejection(http.StatusForbidden, "FDS_ERROR"))
	require.False(t, rejection(http.StatusForbidden, "FORBIDDEN_CONSECUTIVE_REQUEST"))
	require.False(t, rejection(http.StatusUnauthorized, "INVALID_REQUEST"))
}

func TestTossCredentialCandidatesForMIDSameMIDRotation(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_old_same_mid")
	require.NoError(t, err)

	candidates := tossCredentialCandidatesForMID(
		context.Background(),
		credential,
		model.TossClientKeyFingerprint("live_ck_same_mid"),
		"live_ck_same_mid",
		"live_sk_current_same_mid",
	)

	require.Equal(t, []string{"live_sk_old_same_mid", "live_sk_current_same_mid"}, candidates)
}

func TestTossConfirmCredentialCandidatesNeverCrossAPIKeyNamespace(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_old_same_mid")
	require.NoError(t, err)

	candidates := tossConfirmCredentialCandidatesForMID(
		context.Background(),
		credential,
		model.TossClientKeyFingerprint("live_ck_same_mid"),
		"live_ck_same_mid",
		"live_sk_current_same_mid",
	)

	require.Equal(t, []string{"live_sk_old_same_mid"}, candidates)
}

func TestTossConfirmCredentialCandidatesFailClosedWhenStoredCredentialCannotDecrypt(t *testing.T) {
	candidates := tossConfirmCredentialCandidatesForMID(
		context.Background(),
		"corrupt-order-time-credential",
		model.TossClientKeyFingerprint("live_ck_same_mid"),
		"live_ck_same_mid",
		"live_sk_rotated_same_mid",
	)

	require.Empty(t, candidates, "a rotated active secret is GET-only and must never replace a missing POST credential")
}

func TestTossConfirmNeverPostsWhenStoredCredentialCannotDecrypt(t *testing.T) {
	originalClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalClient })
	providerCalls := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		providerCalls++
		t.Fatal("provider request must not be sent without the exact order-time secret")
		return nil, nil
	})}

	result, status, err := confirmTossPaymentWithCredentialForMID(
		context.Background(),
		"pay_fail_closed",
		"toss_fail_closed",
		13000,
		"claim-token",
		"corrupt-order-time-credential",
		model.TossClientKeyFingerprint("live_ck_same_mid"),
		"live_ck_same_mid",
		"live_sk_rotated_same_mid",
	)

	require.Error(t, err)
	require.Nil(t, result)
	require.Zero(t, status)
	require.Zero(t, providerCalls)
}

func TestTossCredentialCandidatesForMIDDifferentMIDDoesNotFallback(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_original_mid")
	require.NoError(t, err)

	candidates := tossCredentialCandidatesForMID(
		context.Background(),
		credential,
		model.TossClientKeyFingerprint("live_ck_original_mid"),
		"live_ck_different_mid",
		"live_sk_different_mid",
	)

	require.Equal(t, []string{"live_sk_original_mid"}, candidates)
}

func TestTossCredentialCandidatesForMIDBlankLegacyHashDoesNotFallback(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_legacy_order")
	require.NoError(t, err)

	candidates := tossCredentialCandidatesForMID(
		context.Background(),
		credential,
		"",
		"live_ck_current_mid",
		"live_sk_current_mid",
	)

	require.Equal(t, []string{"live_sk_legacy_order"}, candidates)
}

func TestTossCredentialCandidatesForMIDMalformedHashDoesNotFallback(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_exact_order_time")
	require.NoError(t, err)
	canonical := model.TossClientKeyFingerprint("live_ck_current_mid")

	for name, malformed := range map[string]string{
		"uppercase":  strings.ToUpper(canonical),
		"short":      canonical[:len(canonical)-2],
		"non hex":    strings.Repeat("z", len(canonical)),
		"whitespace": " " + canonical,
	} {
		t.Run(name, func(t *testing.T) {
			candidates := tossCredentialCandidatesForMID(
				context.Background(),
				credential,
				malformed,
				"live_ck_current_mid",
				"live_sk_must_not_be_used",
			)
			require.Equal(t, []string{"live_sk_exact_order_time"}, candidates)
		})
	}
}

func TestTossCredentialCandidatesForMIDDeduplicatesUnchangedSecret(t *testing.T) {
	setupTossMIDCredentialCrypto(t)
	credential, err := model.EncryptProviderCredential("live_sk_same_secret")
	require.NoError(t, err)

	candidates := tossCredentialCandidatesForMID(
		context.Background(),
		credential,
		model.TossClientKeyFingerprint("live_ck_same_mid"),
		"live_ck_same_mid",
		"live_sk_same_secret",
	)

	require.Equal(t, []string{"live_sk_same_secret"}, candidates)
}

func TestLookupTossBillingAttemptMalformedFingerprintNeverUsesCurrentSecret(t *testing.T) {
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	originalTestMode := setting.TossTestMode
	originalClientKey := setting.TossBillingClientKey
	originalSecret := setting.TossBillingSecretKey
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
		setting.TossTestMode = originalTestMode
		setting.TossBillingClientKey = originalClientKey
		setting.TossBillingSecretKey = originalSecret
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	setting.TossTestMode = false
	setting.TossBillingClientKey = "live_ck_lookup_strict_mid"
	setting.TossBillingSecretKey = "live_sk_lookup_current_must_not_be_used"
	canonical := model.TossClientKeyFingerprint(setting.TossBillingClientKey)

	for name, malformed := range map[string]string{
		"uppercase":  strings.ToUpper(canonical),
		"short":      canonical[:len(canonical)-1],
		"non hex":    strings.Repeat("z", len(canonical)),
		"whitespace": " " + canonical,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":"UNAUTHORIZED_KEY"}`)),
				}, nil
			})}
			order := &model.SubscriptionOrder{
				TradeNo:               "toss_sub_lookup_strict_mid",
				ProviderClientKeyHash: malformed,
			}
			_, usedFallback, err := lookupTossBillingAttemptWithMIDFallback(
				context.Background(),
				order,
				"live_sk_lookup_exact_old",
				13000,
			)
			require.Error(t, err)
			require.False(t, usedFallback)
			require.Equal(t, 1, calls, "malformed MID evidence must not authorize a current-secret lookup")
		})
	}
}
