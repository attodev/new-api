package setting

import "testing"

func TestTossActiveKeysToggle(t *testing.T) {
	defer func() {
		TossTestMode = false
		TossClientKey, TossSecretKey = "", ""
		TossTestClientKey, TossTestSecretKey = "", ""
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
