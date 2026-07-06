package setting

import "testing"

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
