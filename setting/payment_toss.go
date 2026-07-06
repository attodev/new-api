package setting

// Toss Payments (토스페이먼츠) settings.
// Toss distinguishes test vs live purely by which key pair is used; there is
// no separate sandbox host. TossTestMode selects the active key pair.
var TossEnabled = false
var TossTestMode = false
var TossClientKey = ""     // live client key (안전하게 프론트로 노출 가능)
var TossSecretKey = ""     // live secret key (서버 전용)
var TossTestClientKey = "" // test client key
var TossTestSecretKey = "" // test secret key
var TossUnitPrice = 1300.0 // 내부 1 unit(USD 환산) 당 KRW
var TossMinTopUp = 1000    // 최소 충전 금액 (KRW)

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
