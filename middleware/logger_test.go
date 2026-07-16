package middleware

import (
	"strings"
	"testing"
)

func TestRedactSensitiveQuery(t *testing.T) {
	got := redactSensitiveQuery("/api/subscription/toss/confirm?authKey=secret-auth&customerKey=secret-customer&paymentKey=secret-payment&orderId=secret-order&message=incoming-provider-secret&toss_error_message=redirect-provider-secret&toss_order_id=redirect-order&trade_no=order-1")
	for _, secret := range []string{"secret-auth", "secret-customer", "secret-payment", "secret-order", "incoming-provider-secret", "redirect-provider-secret", "redirect-order", "order-1"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sensitive Toss callback value %q was logged: %s", secret, got)
		}
	}
	if !strings.Contains(got, "%5BREDACTED%5D") {
		t.Fatalf("sensitive Toss callback values were logged: %s", got)
	}
}

func TestRedactSensitiveTossSubscriptionCallbackPath(t *testing.T) {
	for _, path := range []string{
		"/api/subscription/toss/confirm/toss_sub_secret_order?code=OK",
		"/api/subscription/toss/fail/toss_sub_secret_order",
	} {
		got := redactSensitiveQuery(path)
		if strings.Contains(got, "toss_sub_secret_order") {
			t.Fatalf("subscription callback bearer leaked through access path: %s", got)
		}
		if !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("subscription callback bearer was not marked redacted: %s", got)
		}
	}
}

func TestRedactSensitiveQueryDropsMalformedQuery(t *testing.T) {
	got := redactSensitiveQuery("/api/toss/confirm?authKey=%zz")
	if got != "/api/toss/confirm?query=redacted" {
		t.Fatalf("unexpected malformed-query redaction: %s", got)
	}
}
