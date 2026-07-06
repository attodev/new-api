package setting

import "strings"

const (
	TossCardMinimumAmountKRW int64 = 100
	TossOrderNameMaxRunes          = 100
)

// Toss Payments (토스페이먼츠) settings.
// Toss distinguishes test vs live purely by which key pair is used; there is
// no separate sandbox host. TossTestMode selects the active key pair.
var TossEnabled = false
var TossBillingEnabled = false // 자동결제는 별도 계약/MID가 필요하므로 일반 결제와 분리
var TossTestMode = false
var TossClientKey = ""     // live client key (안전하게 프론트로 노출 가능)
var TossSecretKey = ""     // live secret key (서버 전용)
var TossTestClientKey = "" // test client key
var TossTestSecretKey = "" // test secret key
// Billing-only key pairs. Toss recurring billing can require a separately
// contracted MID, so availability checks require an explicit billing key pair.
// The active billing helpers below keep a normal-key fallback only for
// backward-compatible API calls and older stored credentials.
var TossBillingClientKey = ""
var TossBillingSecretKey = ""
var TossBillingTestClientKey = ""
var TossBillingTestSecretKey = ""
var TossUnitPrice = 1300.0 // 내부 1 unit(USD 환산) 당 KRW (청구 KRW = units × TossUnitPrice)
var TossMinTopUp = 1       // 최소 충전 금액 (단위/units, $ 모델과 동일)

func TossActiveClientKey() string {
	if TossTestMode {
		return TossTestClientKey
	}
	return TossClientKey
}

func TossActiveSecretKey() string {
	if TossTestMode {
		return TossTestSecretKey
	}
	return TossSecretKey
}

func tossConfiguredPair(clientKey, secretKey, fallbackClientKey, fallbackSecretKey string) (string, string) {
	if strings.TrimSpace(clientKey) != "" && strings.TrimSpace(secretKey) != "" {
		return clientKey, secretKey
	}
	return fallbackClientKey, fallbackSecretKey
}

func TossActiveBillingKeyPair() (string, string) {
	if TossTestMode {
		return tossConfiguredPair(TossBillingTestClientKey, TossBillingTestSecretKey, TossTestClientKey, TossTestSecretKey)
	}
	return tossConfiguredPair(TossBillingClientKey, TossBillingSecretKey, TossClientKey, TossSecretKey)
}

// TossExplicitActiveBillingKeyPair is for enablement checks. It intentionally
// does not fall back to normal Toss keys.
func TossExplicitActiveBillingKeyPair() (string, string) {
	if TossTestMode {
		return strings.TrimSpace(TossBillingTestClientKey), strings.TrimSpace(TossBillingTestSecretKey)
	}
	return strings.TrimSpace(TossBillingClientKey), strings.TrimSpace(TossBillingSecretKey)
}

func TossActiveBillingClientKey() string {
	clientKey, _ := TossActiveBillingKeyPair()
	return clientKey
}

func TossActiveBillingSecretKey() string {
	_, secretKey := TossActiveBillingKeyPair()
	return secretKey
}
