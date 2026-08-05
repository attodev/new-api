package common

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	prev := CryptoSecret
	CryptoSecret = "test-secret-stable"
	defer func() { CryptoSecret = prev }()

	plain := "bky_live_abcdef0123456789"
	enc, err := EncryptString(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == "" || enc == plain {
		t.Fatalf("ciphertext invalid: %q", enc)
	}
	dec, err := DecryptString(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("roundtrip mismatch: got %q want %q", dec, plain)
	}
	// 같은 평문도 매번 다른 ciphertext(랜덤 nonce)
	enc2, _ := EncryptString(plain)
	if enc2 == enc {
		t.Fatalf("ciphertext should differ across calls (nonce)")
	}
}

func TestDecryptRejectsGarbage(t *testing.T) {
	if _, err := DecryptString("not-base64-or-too-short"); err == nil {
		t.Fatalf("expected error decrypting garbage")
	}
}
