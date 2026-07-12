package setting

import (
	"strconv"
	"sync"
	"testing"
)

func TestTossOptionValuesRejectUnsafeNumericConfiguration(t *testing.T) {
	if err := ValidateTossOptionValues(map[string]string{"TossWalletAutoRechargeEnabled": "not-a-boolean"}); err == nil {
		t.Fatal("invalid TossWalletAutoRechargeEnabled should be rejected")
	}
	for _, value := range []string{"0", "-1", "NaN", "+Inf", "2147483648"} {
		if err := ValidateTossOptionValues(map[string]string{"TossUnitPrice": value}); err == nil {
			t.Fatalf("TossUnitPrice %q should be rejected", value)
		}
	}
	for _, value := range []string{"99", "2147483648"} {
		if err := ValidateTossOptionValues(map[string]string{"TossMinTopUp": value}); err == nil {
			t.Fatalf("TossMinTopUp %q should be rejected", value)
		}
	}
}

func TestTossEffectiveMinTopUpNeverDropsBelowCardMinimum(t *testing.T) {
	original := TossMinTopUp
	t.Cleanup(func() { TossMinTopUp = original })

	TossMinTopUp = 1
	if got := TossEffectiveMinTopUp(); got != int(TossCardMinimumAmountKRW) {
		t.Fatalf("effective minimum = %d want %d", got, TossCardMinimumAmountKRW)
	}
	if got := TossEffectiveGeneralTopUp(); got != int(TossGeneralPaymentMinimumAmountKRW) {
		t.Fatalf("general effective minimum = %d want %d", got, TossGeneralPaymentMinimumAmountKRW)
	}
	TossMinTopUp = 5000
	if got := TossEffectiveMinTopUp(); got != 5000 {
		t.Fatalf("configured effective minimum = %d want 5000", got)
	}
	if got := TossEffectiveGeneralTopUp(); got != 5000 {
		t.Fatalf("configured general effective minimum = %d want 5000", got)
	}
}

func TestNormalizeLegacyTossOptionValuesClampsOldMinimum(t *testing.T) {
	values := map[string]string{"TossMinTopUp": "1"}
	if !NormalizeLegacyTossOptionValues(values) {
		t.Fatal("legacy minimum was not recognized")
	}
	if values["TossMinTopUp"] != "100" {
		t.Fatalf("normalized minimum = %q want 100", values["TossMinTopUp"])
	}
	invalid := map[string]string{"TossMinTopUp": "-1"}
	if NormalizeLegacyTossOptionValues(invalid) {
		t.Fatal("negative minimum must not be normalized")
	}
}

func TestTossActiveKeysToggle(t *testing.T) {
	defer func() {
		TossBillingEnabled = false
		TossTestMode = false
		TossClientKey, TossSecretKey = "", ""
		TossTestClientKey, TossTestSecretKey = "", ""
		TossBillingClientKey, TossBillingSecretKey = "", ""
		TossBillingTestClientKey, TossBillingTestSecretKey = "", ""
	}()

	TossClientKey = "live_ck"
	TossSecretKey = "live_sk"
	TossTestClientKey = "test_ck"
	TossTestSecretKey = "test_sk"

	TossTestMode = false
	if got := TossActiveClientKey(); got != "live_ck" {
		t.Fatalf("live client key: got %q want live_ck", got)
	}
	if got := TossActiveSecretKey(); got != "live_sk" {
		t.Fatalf("live secret key: got %q want live_sk", got)
	}

	TossTestMode = true
	if got := TossActiveClientKey(); got != "test_ck" {
		t.Fatalf("test client key: got %q want test_ck", got)
	}
	if got := TossActiveSecretKey(); got != "test_sk" {
		t.Fatalf("test secret key: got %q want test_sk", got)
	}
}

func TestTossActiveBillingKeysFallbackAndOverride(t *testing.T) {
	defer func() {
		TossTestMode = false
		TossClientKey, TossSecretKey = "", ""
		TossTestClientKey, TossTestSecretKey = "", ""
		TossBillingClientKey, TossBillingSecretKey = "", ""
		TossBillingTestClientKey, TossBillingTestSecretKey = "", ""
	}()

	TossClientKey = "live_ck"
	TossSecretKey = "live_sk"
	TossTestClientKey = "test_ck"
	TossTestSecretKey = "test_sk"

	TossTestMode = false
	if got := TossActiveBillingClientKey(); got != "live_ck" {
		t.Fatalf("billing client fallback = %q want live_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "live_sk" {
		t.Fatalf("billing secret fallback = %q want live_sk", got)
	}

	TossBillingClientKey = "billing_live_ck"
	TossBillingSecretKey = "billing_live_sk"
	if got := TossActiveBillingClientKey(); got != "billing_live_ck" {
		t.Fatalf("billing client override = %q want billing_live_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "billing_live_sk" {
		t.Fatalf("billing secret override = %q want billing_live_sk", got)
	}

	TossTestMode = true
	if got := TossActiveBillingClientKey(); got != "test_ck" {
		t.Fatalf("billing test client fallback = %q want test_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "test_sk" {
		t.Fatalf("billing test secret fallback = %q want test_sk", got)
	}

	TossBillingTestClientKey = "billing_test_ck"
	TossBillingTestSecretKey = "billing_test_sk"
	if got := TossActiveBillingClientKey(); got != "billing_test_ck" {
		t.Fatalf("billing test client override = %q want billing_test_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "billing_test_sk" {
		t.Fatalf("billing test secret override = %q want billing_test_sk", got)
	}
}

func TestTossActiveBillingKeysFallbackAsPair(t *testing.T) {
	defer func() {
		TossTestMode = false
		TossClientKey, TossSecretKey = "", ""
		TossTestClientKey, TossTestSecretKey = "", ""
		TossBillingClientKey, TossBillingSecretKey = "", ""
		TossBillingTestClientKey, TossBillingTestSecretKey = "", ""
	}()

	TossClientKey = "live_ck"
	TossSecretKey = "live_sk"
	TossTestClientKey = "test_ck"
	TossTestSecretKey = "test_sk"

	TossTestMode = false
	TossBillingSecretKey = "billing_live_sk"
	if got := TossActiveBillingClientKey(); got != "live_ck" {
		t.Fatalf("partial billing live client = %q want live_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "live_sk" {
		t.Fatalf("partial billing live secret = %q want live_sk", got)
	}

	TossBillingSecretKey = ""
	TossBillingClientKey = "billing_live_ck"
	if got := TossActiveBillingClientKey(); got != "live_ck" {
		t.Fatalf("partial billing live client-only client = %q want live_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "live_sk" {
		t.Fatalf("partial billing live client-only secret = %q want live_sk", got)
	}

	TossTestMode = true
	TossBillingClientKey = ""
	TossBillingTestSecretKey = "billing_test_sk"
	if got := TossActiveBillingClientKey(); got != "test_ck" {
		t.Fatalf("partial billing test client = %q want test_ck", got)
	}
	if got := TossActiveBillingSecretKey(); got != "test_sk" {
		t.Fatalf("partial billing test secret = %q want test_sk", got)
	}
}

func TestApplyTossOptionValuesPublishesKeyPairAtomically(t *testing.T) {
	original := GetTossConfigSnapshot()
	t.Cleanup(func() {
		_ = ApplyTossOptionValues(map[string]string{
			"TossEnabled":                   boolString(original.Enabled),
			"TossBillingEnabled":            boolString(original.BillingEnabled),
			"TossWalletAutoRechargeEnabled": boolString(original.WalletAutoRechargeEnabled),
			"TossTestMode":                  boolString(original.TestMode),
			"TossClientKey":                 original.ClientKey,
			"TossSecretKey":                 original.SecretKey,
			"TossTestClientKey":             original.TestClientKey,
			"TossTestSecretKey":             original.TestSecretKey,
			"TossBillingClientKey":          original.BillingClientKey,
			"TossBillingSecretKey":          original.BillingSecretKey,
			"TossBillingTestClientKey":      original.BillingTestClientKey,
			"TossBillingTestSecretKey":      original.BillingTestSecretKey,
			"TossUnitPrice":                 floatString(original.UnitPrice),
			"TossMinTopUp":                  intString(original.MinTopUp),
		})
	})
	requireNoError(t, ApplyTossOptionValues(map[string]string{"TossWalletAutoRechargeEnabled": "true"}))
	if !GetTossConfigSnapshot().WalletAutoRechargeEnabled {
		t.Fatal("wallet auto recharge gate was not published in the Toss snapshot")
	}

	first := map[string]string{
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_generation_a",
		"TossSecretKey": "live_sk_generation_a",
	}
	second := map[string]string{
		"TossTestMode":  "false",
		"TossClientKey": "live_ck_generation_b",
		"TossSecretKey": "live_sk_generation_b",
	}
	requireNoError(t, ApplyTossOptionValues(first))

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan string, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			values := first
			if i%2 == 1 {
				values = second
			}
			if err := ApplyTossOptionValues(values); err != nil {
				select {
				case errCh <- err.Error():
				default:
				}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			clientKey, secretKey := TossActiveKeyPair()
			if (clientKey == "live_ck_generation_a" && secretKey == "live_sk_generation_a") ||
				(clientKey == "live_ck_generation_b" && secretKey == "live_sk_generation_b") {
				continue
			}
			select {
			case errCh <- clientKey + "/" + secretKey:
			default:
			}
			return
		}
	}()
	wg.Wait()
	close(errCh)
	if mismatch := <-errCh; mismatch != "" {
		t.Fatalf("observed mixed Toss key pair: %s", mismatch)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func floatString(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func intString(value int) string {
	return strconv.Itoa(value)
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
