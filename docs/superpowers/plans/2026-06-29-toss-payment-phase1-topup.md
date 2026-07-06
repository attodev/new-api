# Toss 결제 Phase 1 (일회성 충전) 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 유저가 Toss Payments(원화 결제)로 일회성 충전을 할 수 있게 한다. 프론트는 Toss v2 SDK로 결제창을 열고, 서버는 `/v1/payments/confirm`으로 승인 후 크레딧을 적립한다.

**Architecture:** 기존 PayPal 충전 패턴(리다이렉트 + 서버 confirm + 멱등 적립)을 따른다. 차이점은 (1) 프론트가 `pay_link`로 이동하는 대신 Toss SDK `requestPayment()`로 결제창을 열고, (2) Toss는 test/live를 키 쌍으로만 구분한다. 백엔드는 `controller/topup_toss.go` + `model.RechargeToss` + 설정/라우트, 프론트는 `web/default`의 wallet feature에 전용 훅을 추가한다.

**Tech Stack:** Go 1.22 / Gin / GORM, `shopspring/decimal`, React 19 / TypeScript, `@tosspayments/tosspayments-sdk`, Bun.

**설계 문서:** `docs/superpowers/specs/2026-06-29-toss-payment-integration-design.md`

---

## 파일 구조

**백엔드 (생성):**
- `setting/payment_toss.go` — Toss 설정 변수 + test/live 키 헬퍼
- `controller/topup_toss.go` — Toss 충전 핸들러 + confirm API 클라이언트
- `setting/payment_toss_test.go`, `controller/topup_toss_test.go`, `model/topup_toss_test.go` — 테스트

**백엔드 (수정):**
- `model/topup.go` — `PaymentMethodToss`/`PaymentProviderToss` 상수 + `RechargeToss`
- `model/option.go` — 설정 영속화(양방향)
- `controller/payment_webhook_availability.go` — `isTossTopUpEnabled`
- `controller/topup.go` — `GetTopUpInfo`에 Toss 노출
- `router/api-router.go` — 라우트 등록

**프론트엔드 (수정):**
- `web/default/src/features/wallet/constants.ts` — `PAYMENT_TYPES.TOSS`, 색상
- `web/default/src/features/wallet/types.ts` — `TopupInfo` Toss 필드
- `web/default/src/features/wallet/api.ts` — `requestTossPayment`, `calculateTossAmount`, `TossPaySession` 타입
- `web/default/src/features/wallet/lib/payment.ts` — `isTossPayment` + min/default 분기
- `web/default/src/features/wallet/hooks/use-payment.ts` — 금액계산 Toss 분기
- `web/default/src/features/wallet/hooks/use-toss-payment.ts` — **신규** SDK 훅
- `web/default/src/features/wallet/hooks/index.ts` — export 추가
- `web/default/src/features/wallet/index.tsx` — confirm 핸들러 Toss 분기
- `web/default/src/features/system-settings/integrations/payment-settings-section.tsx` — 설정 UI
- `web/default/package.json` — `@tosspayments/tosspayments-sdk` 의존성

---

## 금액/크레딧 규칙 (모든 태스크가 따름)

- `req.Amount` = 유저가 입력한 **원화(KRW) 정수**.
- `getTossPayMoney(amountKRW, group)` = `round(amountKRW * topupGroupRatio)` → Toss에 전달할 **청구 원화**(int64). `topupGroupRatio`는 `common.GetTopupGroupRatio(group)`, 0이면 1.
- `TopUp.Amount` = 청구 원화(KRW). `TopUp.Money` = `chargedKRW / TossUnitPrice`(USD 환산, 리포트용).
- 적립 크레딧 = `decimal(Money).Mul(QuotaPerUnit).IntPart()`.
- `TossConfirm`은 Toss가 돌려준 `amount` 쿼리값이 `TopUp.Amount`와 정확히 일치할 때만 승인 진행.

---

## Task 1: Toss 설정 + 영속화

**Files:**
- Create: `setting/payment_toss.go`
- Create: `setting/payment_toss_test.go`
- Modify: `model/option.go` (writer 블록 ~line 95 이후, switch ~line 432 이후)

- [ ] **Step 1: 실패하는 테스트 작성**

Create `setting/payment_toss_test.go`:

```go
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
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./setting/ -run TestTossActiveKeysToggle`
Expected: FAIL — `undefined: TossActiveClientKey` 등.

- [ ] **Step 3: 설정 파일 작성**

Create `setting/payment_toss.go`:

```go
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./setting/ -run TestTossActiveKeysToggle`
Expected: PASS

- [ ] **Step 5: option.go 영속화 추가**

In `model/option.go`, after the PayPal writer block (after line 95 `common.OptionMap["PayPalMinTopUp"] = ...`), add:

```go
	common.OptionMap["TossEnabled"] = strconv.FormatBool(setting.TossEnabled)
	common.OptionMap["TossTestMode"] = strconv.FormatBool(setting.TossTestMode)
	common.OptionMap["TossClientKey"] = setting.TossClientKey
	common.OptionMap["TossSecretKey"] = setting.TossSecretKey
	common.OptionMap["TossTestClientKey"] = setting.TossTestClientKey
	common.OptionMap["TossTestSecretKey"] = setting.TossTestSecretKey
	common.OptionMap["TossUnitPrice"] = strconv.FormatFloat(setting.TossUnitPrice, 'f', -1, 64)
	common.OptionMap["TossMinTopUp"] = strconv.Itoa(setting.TossMinTopUp)
```

In the `SetOption`/load switch, after the `case "PayPalMinTopUp":` block (after line 432), add:

```go
	case "TossEnabled":
		setting.TossEnabled = value == "true"
	case "TossTestMode":
		setting.TossTestMode = value == "true"
	case "TossClientKey":
		setting.TossClientKey = value
	case "TossSecretKey":
		setting.TossSecretKey = value
	case "TossTestClientKey":
		setting.TossTestClientKey = value
	case "TossTestSecretKey":
		setting.TossTestSecretKey = value
	case "TossUnitPrice":
		setting.TossUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "TossMinTopUp":
		setting.TossMinTopUp, _ = strconv.Atoi(value)
```

- [ ] **Step 6: 컴파일 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 7: 커밋**

```bash
cd /home/molla/new-api
git add setting/payment_toss.go setting/payment_toss_test.go model/option.go
git commit -m "feat(payment): Toss 설정 변수 및 영속화 추가"
```

---

## Task 2: 모델 상수 + RechargeToss

**Files:**
- Modify: `model/topup.go` (상수 블록 line 35-52, 함수는 `RechargePayPal` 뒤 line 634 이후)
- Create: `model/topup_toss_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `model/topup_toss_test.go`:

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

// 크레딧 계산식이 설계와 일치하는지 검증: quota = Money * QuotaPerUnit
func TestTossQuotaFormula(t *testing.T) {
	prev := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	defer func() { common.QuotaPerUnit = prev }()

	money := 10.0 // 13000 KRW / 1300 = 10 USD-equiv
	quota := int(decimal.NewFromFloat(money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if quota != 5000000 {
		t.Fatalf("quota: got %d want 5000000", quota)
	}
}

func TestTossPaymentConstants(t *testing.T) {
	if PaymentMethodToss != "toss" {
		t.Fatalf("PaymentMethodToss = %q want toss", PaymentMethodToss)
	}
	if PaymentProviderToss != "toss" {
		t.Fatalf("PaymentProviderToss = %q want toss", PaymentProviderToss)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./model/ -run 'TestTossQuotaFormula|TestTossPaymentConstants'`
Expected: FAIL — `undefined: PaymentMethodToss`.

- [ ] **Step 3: 상수 추가**

In `model/topup.go`, add to the payment method const block (after line 41 `PaymentMethodPayPal`):

```go
	PaymentMethodToss = "toss"
```

And to the provider const block (after line 51 `PaymentProviderPayPal`):

```go
	PaymentProviderToss = "toss"
```

- [ ] **Step 4: RechargeToss 작성**

In `model/topup.go`, after `RechargePayPal` (after line 634), add:

```go
// RechargeToss credits a successful Toss top-up idempotently.
// The caller must validate the Toss confirm response (status DONE, amount match)
// before calling this, and must hold the order lock.
func RechargeToss(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // idempotent: already credited
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("top-up order status error")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		quotaToAdd = int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("invalid top-up quota")
		}
		return CreditTopUpTarget(tx, topUp, quotaToAdd)
	})

	if err != nil {
		common.SysError("toss topup failed: " + err.Error())
		return errors.New("top-up failed, please try again later")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Toss top-up successful — quota: %v, payment amount: %d KRW", logger.FormatQuota(quotaToAdd), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentProviderToss)
	}

	return nil
}
```

- [ ] **Step 5: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./model/ -run 'TestTossQuotaFormula|TestTossPaymentConstants'`
Expected: PASS

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add model/topup.go model/topup_toss_test.go
git commit -m "feat(payment): Toss 모델 상수 및 RechargeToss 추가"
```

---

## Task 3: Toss 충전 컨트롤러 — 결제 시작 + 금액 계산

**Files:**
- Create: `controller/topup_toss.go`
- Create: `controller/topup_toss_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

Create `controller/topup_toss_test.go`:

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
)

func TestGetTossPayMoney(t *testing.T) {
	// group ratio 기본 1 가정: 입력 원화가 그대로 청구 원화가 된다.
	got := getTossPayMoney(13000, "default")
	if got != 13000 {
		t.Fatalf("getTossPayMoney(13000) = %d want 13000", got)
	}
}

func TestTossMinTopupGuard(t *testing.T) {
	prev := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	defer func() { setting.TossMinTopUp = prev }()

	if int64(setting.TossMinTopUp) != 1000 {
		t.Fatalf("min topup not set")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run 'TestGetTossPayMoney|TestTossMinTopupGuard'`
Expected: FAIL — `undefined: getTossPayMoney`.

- [ ] **Step 3: 컨트롤러 파일 생성 (결제 시작 + 금액)**

Create `controller/topup_toss.go`:

```go
package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

const tossAPIBase = "https://api.tosspayments.com"

type TossPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

// tossConfirmResponse is the subset of the Toss Payment object we rely on.
type tossConfirmResponse struct {
	PaymentKey  string `json:"paymentKey"`
	OrderId     string `json:"orderId"`
	Status      string `json:"status"`
	TotalAmount int64  `json:"totalAmount"`
	Method      string `json:"method"`
	ApprovedAt  string `json:"approvedAt"`
}

// getTossPayMoney returns the KRW amount to charge for the given entered amount.
func getTossPayMoney(amountKRW int64, group string) int64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	return decimal.NewFromInt(amountKRW).Mul(decimal.NewFromFloat(ratio)).Round(0).IntPart()
}

// tossUSDEquivalent converts charged KRW to the USD-equivalent stored in Money.
func tossUSDEquivalent(chargedKRW int64) float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromInt(chargedKRW).Div(decimal.NewFromFloat(unit)).InexactFloat64()
}

func RequestTossAmount(c *gin.Context) {
	var req TossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Amount < int64(setting.TossMinTopUp) {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": setting.TossMinTopUp})
		return
	}
	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)
	charged := getTossPayMoney(req.Amount, user.Group)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatInt(charged, 10)})
}

func RequestTossPay(c *gin.Context) {
	var req TossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.PaymentMethod != model.PaymentMethodToss {
		common.ApiErrorI18n(c, i18n.MsgTopupUnsupportedProvider)
		return
	}
	if !isTossTopUpEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if req.Amount < int64(setting.TossMinTopUp) {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": setting.TossMinTopUp})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	chargedKRW := getTossPayMoney(req.Amount, user.Group)
	if chargedKRW <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
		return
	}

	reference := fmt.Sprintf("new-api-toss-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	orderId := "toss_" + common.Sha1([]byte(reference))

	topUp := &model.TopUp{
		UserId:          id,
		TargetType:      getTopUpTargetType(c),
		TargetId:        getTopUpTargetId(c),
		Amount:          chargedKRW,
		Money:           tossUSDEquivalent(chargedKRW),
		TradeNo:         orderId,
		ProviderOrderId: orderId,
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss create topup order failed user_id=%d order_id=%s error=%q", id, orderId, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key": setting.TossActiveClientKey(),
			"order_id":   orderId,
			"order_name": fmt.Sprintf("Credit top-up %d KRW", chargedKRW),
			"amount":     chargedKRW,
			"success_url": system_setting.ServerAddress + "/api/toss/confirm",
			"fail_url":    system_setting.ServerAddress + "/api/toss/fail",
		},
	})
}

// confirmTossPayment calls Toss POST /v1/payments/confirm.
func confirmTossPayment(ctx context.Context, paymentKey, orderId string, amount int64) (*tossConfirmResponse, error) {
	payload := map[string]interface{}{
		"paymentKey": paymentKey,
		"orderId":    orderId,
		"amount":     amount,
	}
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/payments/confirm", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	credentials := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toss confirm failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result tossConfirmResponse
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// referenced to keep operation_setting import used by later GetTopUpInfo edits
var _ = operation_setting.GetQuotaDisplayType
```

> Note: `confirmTossPayment`, `TossConfirm`, `TossFail`, `TossWebhook` 핸들러는 Task 4에서 사용됩니다. `var _ = operation_setting...` 줄은 Task 4에서 실제 사용으로 대체되므로 제거합니다. (지금은 import 미사용 컴파일 에러 방지용.)

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run 'TestGetTossPayMoney|TestTossMinTopupGuard'`
Expected: PASS

> `isTossTopUpEnabled`는 Task 5에서 추가됩니다. 이 시점에 `go build ./...`는 실패할 수 있으니, Step 5에서 임시 스텁을 둡니다.

- [ ] **Step 5: isTossTopUpEnabled 임시 스텁로 컴파일 통과**

`controller/topup_toss.go`의 import 아래에 임시로 추가(Task 5에서 제거):

```go
// TEMP STUB — replaced by payment_webhook_availability.go in Task 5
func isTossTopUpEnabled() bool { return strings.TrimSpace(setting.TossActiveSecretKey()) != "" }
```

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add controller/topup_toss.go controller/topup_toss_test.go
git commit -m "feat(payment): Toss 충전 시작/금액 핸들러 및 confirm 클라이언트"
```

---

## Task 4: confirm / fail / webhook 핸들러

**Files:**
- Modify: `controller/topup_toss.go`
- Modify: `controller/topup_toss_test.go`

- [ ] **Step 1: 실패하는 검증 테스트 작성**

Append to `controller/topup_toss_test.go`:

```go
func TestValidateTossConfirmAmount(t *testing.T) {
	// 저장된 주문 금액과 Toss가 돌려준 금액이 다르면 거부되어야 한다.
	topUp := &model.TopUp{Amount: 13000, PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending}
	if err := validateTossConfirm(topUp, "toss_x", 13000); err != nil {
		t.Fatalf("matching amount should pass, got %v", err)
	}
	if err := validateTossConfirm(topUp, "toss_x", 9999); err == nil {
		t.Fatalf("amount mismatch should fail")
	}
	bad := &model.TopUp{Amount: 13000, PaymentProvider: model.PaymentProviderPayPal, Status: common.TopUpStatusPending}
	if err := validateTossConfirm(bad, "toss_x", 13000); err == nil {
		t.Fatalf("provider mismatch should fail")
	}
}
```

(파일 상단 import에 `"github.com/QuantumNous/new-api/common"`, `"github.com/QuantumNous/new-api/model"`가 없으면 추가.)

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run TestValidateTossConfirmAmount`
Expected: FAIL — `undefined: validateTossConfirm`.

- [ ] **Step 3: 핸들러 구현**

In `controller/topup_toss.go`, `var _ = operation_setting.GetQuotaDisplayType` 줄을 삭제하고, 파일 끝에 추가:

```go
func tossRedirect(c *gin.Context, path string) {
	c.Redirect(http.StatusFound, path)
}

func validateTossConfirm(topUp *model.TopUp, orderId string, amount int64) error {
	if topUp == nil {
		return fmt.Errorf("toss local order not found order_id=%s", orderId)
	}
	if topUp.PaymentProvider != model.PaymentProviderToss {
		return fmt.Errorf("toss provider mismatch order_id=%s provider=%s", orderId, topUp.PaymentProvider)
	}
	if topUp.Amount != amount {
		return fmt.Errorf("toss amount mismatch order_id=%s expected=%d actual=%d", orderId, topUp.Amount, amount)
	}
	return nil
}

// TossConfirm handles the successUrl redirect: ?paymentKey&orderId&amount.
func TossConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	paymentKey := c.Query("paymentKey")
	orderId := c.Query("orderId")
	amountStr := c.Query("amount")

	if paymentKey == "" || orderId == "" || amountStr == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm missing params client_ip=%s", c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}
	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm bad amount=%q client_ip=%s", amountStr, c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}

	LockOrder(orderId)
	defer UnlockOrder(orderId)

	topUp := model.GetTopUpByTradeNo(orderId)
	if err := validateTossConfirm(topUp, orderId, amount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed error=%q client_ip=%s", err.Error(), c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		tossRedirect(c, "/console/log")
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm abnormal status order_id=%s status=%q", orderId, topUp.Status))
		tossRedirect(c, "/console/topup")
		return
	}

	result, err := confirmTossPayment(ctx, paymentKey, orderId, amount)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm API failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if result.Status != "DONE" || result.TotalAmount != amount {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm not done order_id=%s status=%s total=%d", orderId, result.Status, result.TotalAmount))
		tossRedirect(c, "/console/topup")
		return
	}

	if err := model.RechargeToss(orderId, c.ClientIP()); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss recharge failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	tossRedirect(c, "/console/log")
}

// TossFail handles the failUrl redirect.
func TossFail(c *gin.Context) {
	ctx := c.Request.Context()
	orderId := c.Query("orderId")
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(ctx, fmt.Sprintf("Toss payment failed order_id=%s code=%s message=%q client_ip=%s", orderId, code, message, c.ClientIP()))
	if orderId != "" {
		_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
	}
	tossRedirect(c, "/console/topup")
}

// TossWebhook handles async settlement notifications (e.g. virtual account DONE).
func TossWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	var event struct {
		EventType string `json:"eventType"`
		Data      struct {
			OrderId     string `json:"orderId"`
			Status      string `json:"status"`
			TotalAmount int64  `json:"totalAmount"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &event); err != nil {
		logger.LogError(ctx, "Toss webhook parse failed: "+err.Error())
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if event.Data.Status != "DONE" || event.Data.OrderId == "" {
		c.Status(http.StatusOK)
		return
	}

	orderId := event.Data.OrderId
	LockOrder(orderId)
	defer UnlockOrder(orderId)

	topUp := model.GetTopUpByTradeNo(orderId)
	if err := validateTossConfirm(topUp, orderId, event.Data.TotalAmount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook validation failed error=%q", err.Error()))
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		c.Status(http.StatusOK)
		return
	}
	if err := model.RechargeToss(orderId, c.ClientIP()); err != nil {
		c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
		return
	}
	c.Status(http.StatusOK)
}
```

Step 3에서 `operation_setting` import가 더 이상 쓰이지 않으면 import 목록에서 제거한다.

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run TestValidateTossConfirmAmount`
Expected: PASS

- [ ] **Step 5: 컴파일 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add controller/topup_toss.go controller/topup_toss_test.go
git commit -m "feat(payment): Toss confirm/fail/webhook 핸들러"
```

---

## Task 5: enable 헬퍼 + GetTopUpInfo 노출 + 라우트

**Files:**
- Modify: `controller/payment_webhook_availability.go`
- Modify: `controller/topup_toss.go` (Task 3의 임시 스텁 제거)
- Modify: `controller/topup.go` (`GetTopUpInfo`)
- Modify: `router/api-router.go`

- [ ] **Step 1: enable 헬퍼 추가**

In `controller/payment_webhook_availability.go`, after `isPayPalTopUpEnabled` (line 27), add:

```go
func isTossTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !setting.TossEnabled {
		return false
	}
	return strings.TrimSpace(setting.TossActiveClientKey()) != "" &&
		strings.TrimSpace(setting.TossActiveSecretKey()) != ""
}
```

- [ ] **Step 2: Task 3 임시 스텁 제거**

In `controller/topup_toss.go`, delete the TEMP STUB line added in Task 3 Step 5:

```go
func isTossTopUpEnabled() bool { return strings.TrimSpace(setting.TossActiveSecretKey()) != "" }
```

- [ ] **Step 3: GetTopUpInfo에 Toss 노출**

In `controller/topup.go` `GetTopUpInfo`, after the PayPal block (after line 95), add a Toss method block:

```go
	// Toss
	if isTossTopUpEnabled() {
		hasToss := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodToss {
				hasToss = true
				break
			}
		}
		if !hasToss {
			payMethods = append(payMethods, map[string]string{
				"name":      "Toss",
				"type":      model.PaymentMethodToss,
				"color":     "#0051BA",
				"min_topup": strconv.Itoa(setting.TossMinTopUp),
			})
		}
	}
```

And add to the `data := gin.H{...}` map (after line 125 `enable_waffo_pancake_topup`):

```go
		"enable_toss_topup":  isTossTopUpEnabled(),
		"toss_min_topup":     setting.TossMinTopUp,
		"toss_client_key":    setting.TossActiveClientKey(),
```

> `toss_client_key`는 공개 키이므로 노출 안전. 시크릿 키는 절대 포함하지 않는다.

- [ ] **Step 4: 라우트 등록**

In `router/api-router.go`:

Public 블록 (after line 62 `apiRouter.POST("/paypal/webhook", ...)`):

```go
		apiRouter.GET("/toss/confirm", controller.TossConfirm)
		apiRouter.GET("/toss/fail", controller.TossFail)
		apiRouter.POST("/toss/webhook", controller.TossWebhook)
```

User 블록 (after line 107 `selfRoute.POST("/paypal/amount", ...)`):

```go
				selfRoute.POST("/toss/pay", middleware.CriticalRateLimit(), controller.RequestTossPay)
				selfRoute.POST("/toss/amount", controller.RequestTossAmount)
```

- [ ] **Step 5: 컴파일 + 전체 테스트**

Run: `cd /home/molla/new-api && go build ./... && go test ./controller/ ./model/ ./setting/ -run Toss`
Expected: 빌드 성공, 모든 Toss 테스트 PASS.

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add controller/payment_webhook_availability.go controller/topup_toss.go controller/topup.go router/api-router.go
git commit -m "feat(payment): Toss enable 헬퍼, topup/info 노출, 라우트 등록"
```

---

## Task 6: 조직(organization) 충전 미러

**Files:**
- Modify: `controller/topup_toss.go`
- Modify: `router/api-router.go`

> 기존 `RequestOrganizationPayPalPay`가 어떻게 target을 세팅하는지 따른다. 먼저 그 구현을 확인한다.

- [ ] **Step 1: 기존 조직 핸들러 패턴 확인**

Run: `cd /home/molla/new-api && grep -n "func RequestOrganizationPayPalPay" controller/*.go`
그 함수를 읽어 `setTopUpTarget`/검증 흐름을 확인한다. (대개 조직 owner 검증 후 `setTopUpTarget(c, organization, orgId)` 호출 뒤 `RequestPayPalPay(c)` 위임.)

- [ ] **Step 2: 조직 핸들러 추가**

In `controller/topup_toss.go`, append (실제 위임 형태는 Step 1에서 확인한 PayPal 조직 핸들러와 동일하게 맞춘다):

```go
func RequestOrganizationTossPay(c *gin.Context) {
	actor, org, err := getOrganizationWalletActor(c)
	if err != nil || org == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	_ = actor
	setTopUpTarget(c, model.TopUpTargetTypeOrganization, org.Id)
	RequestTossPay(c)
}
```

> Step 1에서 확인한 PayPal 조직 핸들러가 추가 권한 검증을 한다면 동일하게 반영한다. `getOrganizationWalletActor`는 `controller/topup_target.go`에 이미 존재.

- [ ] **Step 3: 라우트 등록**

In `router/api-router.go`, organization 블록 (after line 199 `organizationRoute.POST("/paypal/amount", ...)`):

```go
			organizationRoute.POST("/toss/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationTossPay)
```

- [ ] **Step 4: 컴파일 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 5: 커밋**

```bash
cd /home/molla/new-api
git add controller/topup_toss.go router/api-router.go
git commit -m "feat(payment): Toss 조직 충전 미러 라우트"
```

---

## Task 7: 프론트 — 상수 + 타입

**Files:**
- Modify: `web/default/src/features/wallet/constants.ts`
- Modify: `web/default/src/features/wallet/types.ts`

- [ ] **Step 1: PAYMENT_TYPES / 색상에 Toss 추가**

In `constants.ts`, `PAYMENT_TYPES` 객체에 추가:

```typescript
  TOSS: 'toss',
```

`PAYMENT_ICON_COLORS`에 추가:

```typescript
  [PAYMENT_TYPES.TOSS]: '#0051BA',
```

- [ ] **Step 2: TopupInfo 타입에 Toss 필드 추가**

In `types.ts`, `TopupInfo` 인터페이스에 추가:

```typescript
  enable_toss_topup?: boolean
  toss_min_topup?: number
  toss_client_key?: string
```

`TopupInfo` 위쪽 응답 타입 블록에 추가:

```typescript
export type TossPaymentResponse = ApiResponse<{
  client_key: string
  order_id: string
  order_name: string
  amount: number
  success_url: string
  fail_url: string
}>
```

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit` (또는 프로젝트의 타입체크 스크립트)
Expected: 에러 없음.

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/wallet/constants.ts web/default/src/features/wallet/types.ts
git commit -m "feat(web): Toss 결제 상수 및 타입 추가"
```

---

## Task 8: 프론트 — SDK 의존성 + API 함수

**Files:**
- Modify: `web/default/package.json` (+ lockfile)
- Modify: `web/default/src/features/wallet/api.ts`

- [ ] **Step 1: SDK 설치**

Run: `cd /home/molla/new-api/web/default && bun add @tosspayments/tosspayments-sdk`
Expected: `package.json` dependencies에 `@tosspayments/tosspayments-sdk` 추가, lockfile 갱신.

- [ ] **Step 2: API 함수 추가**

In `web/default/src/features/wallet/api.ts`, import 블록의 타입에 `TossPaymentResponse`를 추가하고(기존 `import type { ... } from './types'` 라인), 파일에 함수 추가:

```typescript
export async function calculateTossAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/toss/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestTossPayment(
  request: PaymentRequest
): Promise<TossPaymentResponse> {
  const res = await api.post('/api/user/toss/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}
```

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit`
Expected: 에러 없음.

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/package.json web/default/bun.lock* web/default/src/features/wallet/api.ts
git commit -m "feat(web): Toss SDK 의존성 및 API 함수"
```

---

## Task 9: 프론트 — lib 헬퍼

**Files:**
- Modify: `web/default/src/features/wallet/lib/payment.ts`
- Create: `web/default/src/features/wallet/lib/payment.toss.test.ts` (테스트 인프라가 있을 경우)

- [ ] **Step 1: 실패하는 테스트 작성 (vitest/jest 존재 시)**

먼저 확인: `cd /home/molla/new-api/web/default && ls src/features/wallet/*.test.* src/features/wallet/**/*.test.* 2>/dev/null`. 테스트 러너가 있으면(`wallet-stats-card.test.tsx` 존재 확인됨) 작성:

Create `web/default/src/features/wallet/lib/payment.toss.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { isTossPayment } from './payment'
import { PAYMENT_TYPES } from '../constants'

describe('isTossPayment', () => {
  it('true for toss', () => {
    expect(isTossPayment(PAYMENT_TYPES.TOSS)).toBe(true)
  })
  it('false for others', () => {
    expect(isTossPayment(PAYMENT_TYPES.STRIPE)).toBe(false)
  })
})
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api/web/default && bun run test src/features/wallet/lib/payment.toss.test.ts`
Expected: FAIL — `isTossPayment` not exported.

- [ ] **Step 3: 헬퍼 추가**

In `web/default/src/features/wallet/lib/payment.ts`, after `isPayPalPayment`:

```typescript
export function isTossPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.TOSS
}
```

In `getDefaultPaymentType`, `enable_waffo_pancake_topup` 분기 뒤에 추가:

```typescript
  if (topupInfo.enable_toss_topup) {
    return PAYMENT_TYPES.TOSS
  }
```

In `getMinTopupAmount`, `enable_waffo_pancake_topup` 분기 뒤에 추가:

```typescript
  if (topupInfo.enable_toss_topup) {
    return topupInfo.toss_min_topup || DEFAULT_MIN_TOPUP
  }
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api/web/default && bun run test src/features/wallet/lib/payment.toss.test.ts`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/wallet/lib/payment.ts web/default/src/features/wallet/lib/payment.toss.test.ts
git commit -m "feat(web): isTossPayment 및 default/min 분기"
```

---

## Task 10: 프론트 — Toss SDK 결제 훅

**Files:**
- Create: `web/default/src/features/wallet/hooks/use-toss-payment.ts`
- Modify: `web/default/src/features/wallet/hooks/index.ts`

- [ ] **Step 1: 훅 작성**

Create `web/default/src/features/wallet/hooks/use-toss-payment.ts`:

```typescript
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { loadTossPayments, ANONYMOUS } from '@tosspayments/tosspayments-sdk'
import { requestTossPayment, isApiSuccess } from '../api'

// Toss 충전: 백엔드에서 결제 세션 정보를 받아 SDK로 결제창을 연다.
export function useTossPayment() {
  const [processing, setProcessing] = useState(false)

  const processTossPayment = useCallback(async (topupAmount: number) => {
    try {
      setProcessing(true)
      const amount = Math.floor(topupAmount)
      const response = await requestTossPayment({ amount, payment_method: 'toss' })

      if (!isApiSuccess(response) || !response.data) {
        toast.error(response.message || i18next.t('Payment request failed'))
        return false
      }

      const { client_key, order_id, order_name, amount: chargeAmount, success_url, fail_url } =
        response.data

      const tossPayments = await loadTossPayments(client_key)
      const payment = tossPayments.payment({ customerKey: ANONYMOUS })

      await payment.requestPayment({
        method: 'CARD',
        amount: { currency: 'KRW', value: chargeAmount },
        orderId: order_id,
        orderName: order_name,
        successUrl: success_url,
        failUrl: fail_url,
      })
      // requestPayment가 결제창으로 리다이렉트하므로 이 지점 이후는 도달하지 않는다.
      return true
    } catch (err) {
      // 유저가 결제창을 닫으면 SDK가 reject한다 — 조용히 실패 처리.
      const message = (err as { message?: string })?.message
      if (message) toast.error(message)
      return false
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, processTossPayment }
}
```

- [ ] **Step 2: export 추가**

In `web/default/src/features/wallet/hooks/index.ts`, add:

```typescript
export * from './use-toss-payment'
```

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit`
Expected: 에러 없음. (SDK 타입 정의가 인식되어야 함.)

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/wallet/hooks/use-toss-payment.ts web/default/src/features/wallet/hooks/index.ts
git commit -m "feat(web): Toss SDK 결제 훅 추가"
```

---

## Task 11: 프론트 — 금액 계산 분기 + 결제 확정 분기

**Files:**
- Modify: `web/default/src/features/wallet/hooks/use-payment.ts`
- Modify: `web/default/src/features/wallet/index.tsx`

- [ ] **Step 1: 금액 계산에 Toss 분기 추가**

In `use-payment.ts`, import에 `calculateTossAmount`(from `../api`)와 `isTossPayment`(from `../lib`) 추가. `calculatePaymentAmount` 내부, `isPancake` 분기 뒤에:

```typescript
        const isToss = isTossPayment(paymentType)
```
(상단 변수 선언부에 추가) 그리고 분기 체인에 추가:
```typescript
        } else if (isToss) {
          response = await calculateTossAmount({ amount: topupAmount })
```

- [ ] **Step 2: index.tsx 결제 확정에 Toss 분기**

In `web/default/src/features/wallet/index.tsx`, import에 `isTossPayment`(from `./lib`)와 `useTossPayment`(from `./hooks`) 추가. 컴포넌트 내 훅 사용부에 추가:

```typescript
  const { processTossPayment } = useTossPayment()
```

`handlePaymentConfirm`을 수정:

```typescript
  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return

    const type = selectedPaymentMethod.type
    let success = false
    if (isWaffoPancakePayment(type)) {
      success = await processWaffoPancakePayment(topupAmount)
    } else if (isTossPayment(type)) {
      success = await processTossPayment(topupAmount)
    } else {
      success = await processPayment(topupAmount, type)
    }

    if (success) {
      setConfirmDialogOpen(false)
      await fetchUser()
    }
  }
```

- [ ] **Step 3: 타입체크 + 빌드**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit && bun run build`
Expected: 에러 없음.

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/wallet/hooks/use-payment.ts web/default/src/features/wallet/index.tsx
git commit -m "feat(web): Toss 금액 계산 및 결제 확정 분기 연결"
```

---

## Task 12: 프론트 — 관리자 설정 UI

**Files:**
- Modify: `web/default/src/features/system-settings/integrations/payment-settings-section.tsx`

> 먼저 이 파일에서 PayPal 필드가 어떻게 schema/폼/onSubmit/UI에 배선되는지 읽고 그대로 미러링한다.

- [ ] **Step 1: 기존 PayPal 배선 확인**

Run: `cd /home/molla/new-api && grep -n "PayPal" web/default/src/features/system-settings/integrations/payment-settings-section.tsx`
PayPal 필드의 (a) zod schema 항목, (b) defaultValues, (c) onSubmit 비교/updates push, (d) `<FormField>` UI 위치를 파악한다.

- [ ] **Step 2: zod schema에 Toss 필드 추가**

`paymentSchema`에 추가:

```typescript
  TossEnabled: z.boolean(),
  TossTestMode: z.boolean(),
  TossClientKey: z.string(),
  TossSecretKey: z.string(),
  TossTestClientKey: z.string(),
  TossTestSecretKey: z.string(),
  TossUnitPrice: z.coerce.number().min(0),
  TossMinTopUp: z.coerce.number().min(0),
```

- [ ] **Step 3: defaultValues + onSubmit 비교 로직 추가**

defaultValues 객체에 위 8개 키를 현재 옵션값으로 채우고(PayPal 항목과 동일 방식), onSubmit에서 PayPal과 동일한 패턴으로 변경분만 `updates.push({ key, value })` 한다. boolean/number는 문자열로 직렬화:

```typescript
  if (sanitized.TossEnabled !== initial.TossEnabled) {
    updates.push({ key: 'TossEnabled', value: String(sanitized.TossEnabled) })
  }
  if (sanitized.TossTestMode !== initial.TossTestMode) {
    updates.push({ key: 'TossTestMode', value: String(sanitized.TossTestMode) })
  }
  if (sanitized.TossClientKey !== initial.TossClientKey) {
    updates.push({ key: 'TossClientKey', value: sanitized.TossClientKey })
  }
  if (sanitized.TossSecretKey !== initial.TossSecretKey) {
    updates.push({ key: 'TossSecretKey', value: sanitized.TossSecretKey })
  }
  if (sanitized.TossTestClientKey !== initial.TossTestClientKey) {
    updates.push({ key: 'TossTestClientKey', value: sanitized.TossTestClientKey })
  }
  if (sanitized.TossTestSecretKey !== initial.TossTestSecretKey) {
    updates.push({ key: 'TossTestSecretKey', value: sanitized.TossTestSecretKey })
  }
  if (sanitized.TossUnitPrice !== initial.TossUnitPrice) {
    updates.push({ key: 'TossUnitPrice', value: String(sanitized.TossUnitPrice) })
  }
  if (sanitized.TossMinTopUp !== initial.TossMinTopUp) {
    updates.push({ key: 'TossMinTopUp', value: String(sanitized.TossMinTopUp) })
  }
```

- [ ] **Step 4: UI 섹션 추가 (PayPal 섹션 뒤)**

PayPal `<FormField>` 그룹 뒤에 Toss 섹션을 PayPal과 동일한 컴포넌트(`Separator`, `FormField`, `Switch`/`Input`)로 추가한다. 필드: TossEnabled(스위치), TossTestMode(스위치), TossClientKey, TossSecretKey, TossTestClientKey, TossTestSecretKey, TossUnitPrice, TossMinTopUp. 시크릿 키 입력은 `type="password"`로.

(정확한 JSX는 Step 1에서 읽은 PayPal 블록을 복제해 이름만 치환.)

- [ ] **Step 5: 타입체크 + 빌드**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit && bun run build`
Expected: 에러 없음.

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/system-settings/integrations/payment-settings-section.tsx
git commit -m "feat(web): Toss 결제 관리자 설정 UI"
```

---

## Task 13: 통합 검증

- [ ] **Step 1: 백엔드 전체 테스트**

Run: `cd /home/molla/new-api && go build ./... && go test ./controller/ ./model/ ./setting/`
Expected: 모두 PASS.

- [ ] **Step 2: 프론트 빌드**

Run: `cd /home/molla/new-api/web/default && bun run tsc --noEmit && bun run build`
Expected: 에러 없음.

- [ ] **Step 3: 수동 E2E 체크리스트 (Toss 테스트 키 필요)**

1. 관리자 설정에서 Toss 활성화 + test 모드 + 테스트 키 입력, UnitPrice/MinTopUp 설정, 저장.
2. 충전 페이지에서 Toss 결제수단이 노출되는지 확인.
3. 금액 입력 → 확정 → Toss 결제창이 열리는지 확인.
4. 테스트 카드로 결제 → `/console/log`로 돌아오고 크레딧이 적립됐는지 확인.
5. 동일 주문으로 confirm을 재호출해도 (브라우저 새로고침/중복) 이중 적립되지 않는지 확인.
6. 결제 실패/취소 시 `/console/topup`로 돌아오고 주문이 failed인지 확인.

- [ ] **Step 4: 최종 커밋 (필요 시)**

```bash
cd /home/molla/new-api
git status
```

---

## Self-Review 결과

- **스펙 커버리지:** §3 설정/영속화(Task 1), 모델 상수·환산(Task 2), §4.1 핸들러(Task 3·4), enable/노출/라우트(Task 5), 조직 미러(Task 6), §4.4 프론트(Task 7–12). Phase 2(빌링)는 본 계획 범위 밖 — 별도 계획서.
- **확인 필요 가정(구현 중 검증):** (a) `RequestOrganizationPayPalPay`의 정확한 위임/권한 검증 형태(Task 6 Step 1에서 읽고 맞춤), (b) `payment-settings-section.tsx`의 PayPal 배선 세부(Task 12 Step 1), (c) 프론트 테스트 러너 명령(`bun run test`)·타입체크 명령이 실제 스크립트명과 일치하는지(`web/default/package.json` scripts 확인). 이들은 기존 코드를 읽어 정확히 맞춘다.
- **타입 일관성:** 백엔드 `getTossPayMoney`(int64)·`tossUSDEquivalent`·`RechargeToss`·`validateTossConfirm`·`confirmTossPayment` 시그니처가 호출부와 일치. 프론트 `TossPaymentResponse.data` 필드(client_key/order_id/order_name/amount/success_url/fail_url)가 백엔드 `RequestTossPay` 응답과 일치.
- **플레이스홀더:** 코드 스텝은 실제 코드 포함. Task 6/12의 일부는 "기존 블록을 읽고 미러"로 위임 — 해당 블록이 길고 가변적이라 복제가 안전하기 때문(읽기 스텝을 선행 배치).
</content>
</invoke>
