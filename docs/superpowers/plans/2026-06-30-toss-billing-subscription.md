# Toss 빌링키 자동결제(정기결제) 구현 계획 — Phase 2

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Toss 빌링키로 구독 플랜을 **서버 주도 자동 정기결제**한다. 구독 시 빌링키를 발급·암호화 저장하고 첫 기간을 청구하며, cron이 만료 시점에 빌링키로 재청구해 구독을 자동 연장한다.

**Architecture:** 기존 구독 시스템(`SubscriptionPlan`/`SubscriptionOrder`/`UserSubscription` + `CompleteSubscriptionOrder`/`CreateUserSubscriptionFromPlanTx` + reset/expire cron)을 재사용한다. 신규는 (1) 빌링키 저장(`UserBillingKey` + AES-GCM 암호화), (2) `UserSubscription`에 자동결제 필드, (3) Toss 빌링 API 클라이언트, (4) 구독시작/확정 컨트롤러, (5) 정기청구 cron. 기존엔 자동 재청구가 없으므로 cron이 핵심 신규 요소다.

**Tech Stack:** Go 1.22 / Gin / GORM / PostgreSQL(운영)·SQLite·MySQL, `shopspring/decimal`, AES-256-GCM(`crypto/aes`,`crypto/cipher`), React 19 / TS, `@tosspayments/tosspayments-sdk`.

**설계 문서:** `docs/superpowers/specs/2026-06-29-toss-payment-integration-design.md` (§5), 구현/하드닝: `docs/superpowers/specs/2026-06-30-toss-payment-implementation-and-hardening.md`

**Toss 빌링 API:**
- 빌링키 발급: `POST https://api.tosspayments.com/v1/billing/authorizations/issue`, Basic auth `base64(secretKey+":")`, body `{authKey, customerKey}` → `{billingKey, card:{company,number}, customerKey}`
- 빌링 청구: `POST https://api.tosspayments.com/v1/billing/{billingKey}`, Basic auth, body `{customerKey, amount, orderId, orderName}` → Payment `{status, totalAmount, orderId, paymentKey, currency, ...}`
- 프론트: `tossPayments.payment({customerKey}).requestBillingAuth({method:'CARD', successUrl, failUrl})` → successUrl로 `authKey`+`customerKey` 리다이렉트.

---

## 파일 구조

**백엔드 (신규)**
- `controller/subscription_payment_toss.go` — 구독 빌링 핸들러(시작/확정/실패/해지) + Toss 빌링 API 클라이언트 + 금액 환산
- `service/toss_billing_task.go` — 정기청구 cron
- 테스트: `common/crypto_test.go`(AES 라운드트립), `controller/subscription_payment_toss_test.go`

**백엔드 (수정)**
- `common/crypto.go` — AES-256-GCM `EncryptString`/`DecryptString`
- `model/toss_billing.go` (신규) — `UserBillingKey` 모델 + 저장/조회 함수
- `model/subscription.go` — `UserSubscription`에 `AutoRenew`/`NextBillingTime`/`BillingKeyId`/`BillingFailCount` 추가 + `CompleteTossBillingOrder` + `RenewTossSubscription`
- `controller/payment_webhook_availability.go` — `isTossBillingEnabled`
- `router/api-router.go` — 라우트
- `main.go` — cron 등록

**프론트엔드 (web/default)**
- `features/subscriptions/api.ts` — `paySubscriptionToss`, `cancelTossAutoRenew` + 타입
- `features/subscriptions/hooks/use-toss-billing.ts` (신규) — billingAuth SDK 훅
- `features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx` — Toss 구독 버튼
- 구독 self 화면 — 자동결제 해지 버튼(등록 카드 표시)

---

## 공통 규칙 (모든 태스크)
- JSON은 `common.Marshal`/`common.Unmarshal`만 사용. 3-DB 호환(GORM, 신규 컬럼은 struct+AutoMigrate; SQLite ALTER COLUMN 금지; `UPDATE...LIMIT` 금지). 보호 식별자(new-api/QuantumNous) 불변.
- Toss 금액은 KRW 정수. 구독 청구 KRW = `round(plan.PriceAmount × setting.TossUnitPrice)` (PriceAmount는 USD-equiv, TossUnitPrice는 ₩/unit).
- 빌링키는 **평문 저장 금지** — AES-GCM 암호화 후 저장. 시크릿키/빌링키 응답·로그 비노출.
- 결제 완료 판정은 Toss 권위 응답(`status=="DONE"`)으로만. 멱등: `CompleteTossBillingOrder`는 order status 가드로 멱등.

---

## Task 1: AES-256-GCM 암호화 헬퍼

**Files:**
- Modify: `common/crypto.go`
- Create: `common/crypto_test.go`

- [ ] **Step 1: 실패 테스트 작성**

Create `common/crypto_test.go`:
```go
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
```

- [ ] **Step 2: 실패 확인**

Run: `cd /home/molla/new-api && go test ./common/ -run 'TestEncryptDecryptRoundTrip|TestDecryptRejectsGarbage'`
Expected: FAIL — `undefined: EncryptString`.

- [ ] **Step 3: 구현**

In `common/crypto.go`, add to the import block: `"crypto/aes"`, `"crypto/cipher"`, `"crypto/rand"`, `"encoding/base64"`, `"errors"`, `"io"`. Then append:
```go
// aesKey derives a 32-byte AES key from CryptoSecret (stable across restarts when
// CRYPTO_SECRET or SESSION_SECRET is set).
func aesKey() [32]byte {
	return sha256.Sum256([]byte(CryptoSecret))
}

// EncryptString encrypts plaintext with AES-256-GCM and returns base64(nonce|ciphertext).
func EncryptString(plaintext string) (string, error) {
	key := aesKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString reverses EncryptString.
func DecryptString(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key := aesKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
```
(`sha256` is already imported in crypto.go.)

- [ ] **Step 4: 통과 확인**

Run: `cd /home/molla/new-api && go test ./common/ -run 'TestEncryptDecryptRoundTrip|TestDecryptRejectsGarbage'`
Expected: PASS

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add common/crypto.go common/crypto_test.go
git commit -m "feat(common): AES-256-GCM EncryptString/DecryptString for secret-at-rest"
```

---

## Task 2: UserBillingKey 모델 + UserSubscription 자동결제 필드

**Files:**
- Create: `model/toss_billing.go`
- Modify: `model/subscription.go` (UserSubscription struct, ~line 235)

- [ ] **Step 1: UserSubscription에 자동결제 필드 추가**

In `model/subscription.go`, `UserSubscription` struct, add after `Source string` field:
```go
	// Toss 자동결제(빌링) 연동 필드
	AutoRenew        bool  `json:"auto_renew" gorm:"default:false"`
	NextBillingTime  int64 `json:"next_billing_time" gorm:"default:0;index"`
	BillingKeyId     int   `json:"billing_key_id" gorm:"default:0;index"`
	BillingFailCount int   `json:"billing_fail_count" gorm:"default:0"`
```

- [ ] **Step 2: UserBillingKey 모델 + 함수 작성**

Create `model/toss_billing.go`:
```go
package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UserBillingKey stores a Toss billing key (암호화) for recurring charges.
type UserBillingKey struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id" gorm:"index"`
	CustomerKey      string `json:"customer_key" gorm:"type:varchar(64);index"`
	EncryptedKey     string `json:"-" gorm:"type:text"` // AES-GCM(billingKey)
	CardCompany      string `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked string `json:"card_number_masked" gorm:"type:varchar(32)"`
	Status           string `json:"status" gorm:"type:varchar(16);default:'active'"` // active | revoked
	CreateTime       int64  `json:"create_time"`
}

const (
	BillingKeyStatusActive  = "active"
	BillingKeyStatusRevoked = "revoked"
)

// StoreTossBillingKey encrypts and persists a billing key, returning its row id.
func StoreTossBillingKey(userId int, customerKey, billingKey, cardCompany, cardMasked string) (int, error) {
	if billingKey == "" {
		return 0, errors.New("empty billing key")
	}
	enc, err := common.EncryptString(billingKey)
	if err != nil {
		return 0, err
	}
	row := &UserBillingKey{
		UserId:           userId,
		CustomerKey:      customerKey,
		EncryptedKey:     enc,
		CardCompany:      cardCompany,
		CardNumberMasked: cardMasked,
		Status:           BillingKeyStatusActive,
		CreateTime:       common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		return 0, err
	}
	return row.Id, nil
}

// GetTossBillingKeyPlain returns the decrypted billing key for an active row.
func GetTossBillingKeyPlain(id int) (key string, customerKey string, err error) {
	var row UserBillingKey
	if err = DB.Where("id = ?", id).First(&row).Error; err != nil {
		return "", "", err
	}
	if row.Status != BillingKeyStatusActive {
		return "", "", errors.New("billing key revoked")
	}
	plain, err := common.DecryptString(row.EncryptedKey)
	if err != nil {
		return "", "", err
	}
	return plain, row.CustomerKey, nil
}

// RevokeTossBillingKey marks a billing key revoked (best-effort).
func RevokeTossBillingKey(tx *gorm.DB, id int) error {
	db := DB
	if tx != nil {
		db = tx
	}
	return db.Model(&UserBillingKey{}).Where("id = ?", id).Update("status", BillingKeyStatusRevoked).Error
}
```

- [ ] **Step 3: AutoMigrate에 UserBillingKey 등록**

Find where models are migrated. Run: `cd /home/molla/new-api && grep -n "AutoMigrate\|UserSubscription{}" model/main.go | head`. In the `AutoMigrate(...)` call list (the one that includes `&UserSubscription{}`), add `&UserBillingKey{}` alongside it.

- [ ] **Step 4: 빌드 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add model/toss_billing.go model/subscription.go model/main.go
git commit -m "feat(payment): UserBillingKey model + UserSubscription auto-renew fields"
```

---

## Task 3: Toss 빌링 API 클라이언트 + enable 가드 + 금액 환산

**Files:**
- Create: `controller/subscription_payment_toss.go`
- Modify: `controller/payment_webhook_availability.go`
- Create: `controller/subscription_payment_toss_test.go`

- [ ] **Step 1: 실패 테스트 작성**

Create `controller/subscription_payment_toss_test.go`:
```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

func TestTossSubscriptionChargeKRW(t *testing.T) {
	prev := setting.TossUnitPrice
	setting.TossUnitPrice = 1500
	defer func() { setting.TossUnitPrice = prev }()

	plan := &model.SubscriptionPlan{PriceAmount: 10}
	got := tossSubscriptionChargeKRW(plan)
	if got != 15000 { // 10 USD-equiv × 1500 ₩/unit
		t.Fatalf("chargeKRW = %d want 15000", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run TestTossSubscriptionChargeKRW`
Expected: FAIL — `undefined: tossSubscriptionChargeKRW`.

- [ ] **Step 3: 클라이언트 + 헬퍼 구현**

Create `controller/subscription_payment_toss.go`:
```go
package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/shopspring/decimal"
)

// tossBillingIssueResponse is the response from /v1/billing/authorizations/issue.
type tossBillingIssueResponse struct {
	BillingKey  string `json:"billingKey"`
	CustomerKey string `json:"customerKey"`
	Card        struct {
		Company string `json:"company"`
		Number  string `json:"number"`
	} `json:"card"`
}

// tossSubscriptionChargeKRW converts a plan's USD-equivalent price to KRW for Toss.
func tossSubscriptionChargeKRW(plan *model.SubscriptionPlan) int64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromFloat(plan.PriceAmount).Mul(decimal.NewFromFloat(unit)).Round(0).IntPart()
}

// issueTossBillingKey exchanges an authKey for a billing key.
func issueTossBillingKey(ctx context.Context, authKey, customerKey string) (*tossBillingIssueResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{"authKey": authKey, "customerKey": customerKey}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/billing/authorizations/issue", strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	cred := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+cred)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("toss billing issue failed: status=%d body=%s", resp.StatusCode, string(rb))
	}
	var out tossBillingIssueResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
}

// chargeTossBilling charges a billing key. Reuses tossConfirmResponse (status/totalAmount/orderId/currency).
func chargeTossBilling(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{
		"customerKey": customerKey,
		"amount":      amount,
		"orderId":     orderId,
		"orderName":   orderName,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/billing/"+billingKey, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	cred := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+cred)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", orderId)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("toss billing charge failed: status=%d body=%s", resp.StatusCode, string(rb))
	}
	var out tossConfirmResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
}
```
(`tossAPIBase` and `tossConfirmResponse` are defined in `controller/topup_toss.go`.)

In `controller/payment_webhook_availability.go`, add after `isTossTopUpEnabled`:
```go
func isTossBillingEnabled() bool {
	// Same prerequisites as Toss top-up (compliance + enabled + active keys + valid ServerAddress).
	return isTossTopUpEnabled()
}
```

- [ ] **Step 4: 통과 확인**

Run: `cd /home/molla/new-api && go test ./controller/ -run TestTossSubscriptionChargeKRW`
Expected: PASS

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add controller/subscription_payment_toss.go controller/subscription_payment_toss_test.go controller/payment_webhook_availability.go
git commit -m "feat(payment): Toss billing API client, charge-KRW conversion, enable guard"
```

---

## Task 4: 구독 완료/갱신 모델 함수

**Files:**
- Modify: `model/subscription.go`

- [ ] **Step 1: CompleteTossBillingOrder 작성**

In `model/subscription.go`, add (model on `CompleteSubscriptionOrder`, but set billing fields on the created subscription):
```go
// CompleteTossBillingOrder completes a pending Toss subscription order and marks the
// created UserSubscription for auto-renew (billingKeyId). Idempotent on order status.
func CompleteTossBillingOrder(tradeNo string, billingKeyId int, providerPayload string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	var logUserId int
	var upgradeGroup string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if order.PaymentProvider != PaymentProviderToss {
			return ErrPaymentMethodMismatch
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil // idempotent
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		plan, err := GetSubscriptionPlanById(order.PlanId)
		if err != nil {
			return err
		}
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		sub, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		// Mark for auto-renew: charge again when the current period ends.
		sub.AutoRenew = true
		sub.BillingKeyId = billingKeyId
		sub.NextBillingTime = sub.EndTime
		sub.BillingFailCount = 0
		sub.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		order.Status = common.TopUpStatusSuccess
		order.CompleteTime = common.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		logUserId = order.UserId
		return nil
	})
	if err != nil {
		return err
	}
	if upgradeGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, upgradeGroup)
	}
	if logUserId > 0 {
		RecordLog(logUserId, LogTypeTopup, "Toss 자동결제 구독 시작")
	}
	return nil
}
```

- [ ] **Step 2: RenewTossSubscription 작성**

Append to `model/subscription.go`:
```go
// RenewTossSubscription extends a subscription for another period after a successful
// recurring billing charge. Extends EndTime from the current EndTime (no drift), resets
// quota usage, advances NextBillingTime, records an audit order, and clears fail count.
func RenewTossSubscription(subId int, tradeNo string, money float64) error {
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", subId).First(&sub).Error; err != nil {
			return err
		}
		plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
		base := time.Unix(sub.EndTime, 0)
		newEnd, err := calcPlanEndTime(base, plan)
		if err != nil {
			return err
		}
		sub.EndTime = newEnd
		sub.NextBillingTime = newEnd
		sub.AmountUsed = 0
		sub.LastResetTime = common.GetTimestamp()
		sub.NextResetTime = calcNextResetTime(time.Unix(common.GetTimestamp(), 0), plan, newEnd)
		sub.Status = "active"
		sub.BillingFailCount = 0
		sub.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		// Audit order (idempotent on unique trade_no).
		order := &SubscriptionOrder{
			UserId:          sub.UserId,
			PlanId:          sub.PlanId,
			Money:           money,
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodToss,
			PaymentProvider: PaymentProviderToss,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      common.GetTimestamp(),
			CompleteTime:    common.GetTimestamp(),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		return nil
	})
}

// MarkTossBillingFailure increments the fail counter and disables auto-renew after maxFails.
func MarkTossBillingFailure(subId int, maxFails int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", subId).First(&sub).Error; err != nil {
			return err
		}
		sub.BillingFailCount++
		if sub.BillingFailCount >= maxFails {
			sub.AutoRenew = false
		}
		sub.UpdatedAt = common.GetTimestamp()
		return tx.Save(&sub).Error
	})
}

// GetDueTossRenewals returns active auto-renew Toss subscriptions due for charge.
func GetDueTossRenewals(now int64, limit int) ([]UserSubscription, error) {
	var subs []UserSubscription
	err := DB.Where("auto_renew = ? AND billing_key_id > 0 AND next_billing_time > 0 AND next_billing_time <= ? AND status = ?",
		commonTrueVal, now, "active").
		Order("next_billing_time asc").Limit(limit).Find(&subs).Error
	return subs, err
}
```
(`calcPlanEndTime`, `calcNextResetTime`, `getSubscriptionPlanByIdTx`, `PaymentMethodToss`, `PaymentProviderToss`, `commonTrueVal` already exist in the model package. `time` is imported in subscription.go.)

- [ ] **Step 3: 빌드 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 에러 없음.

- [ ] **Step 4: 커밋**
```bash
cd /home/molla/new-api
git add model/subscription.go
git commit -m "feat(payment): Toss subscription complete/renew/fail model functions"
```

---

## Task 5: 구독 빌링 컨트롤러 + 라우트

**Files:**
- Modify: `controller/subscription_payment_toss.go`
- Modify: `router/api-router.go`

> 먼저 `SubscriptionRequestStripePay`(controller/subscription_payment_stripe.go)와 `RequestTossPay`(controller/topup_toss.go)를 읽어 검증/플랜조회/`prepareOrganizationTopUpTarget` 외 패턴을 맞춘다.

- [ ] **Step 1: 핸들러 추가**

Append to `controller/subscription_payment_toss.go` (add imports `"github.com/QuantumNous/new-api/i18n"`, `"github.com/QuantumNous/new-api/logger"`, `"github.com/QuantumNous/new-api/setting/system_setting"`, `"github.com/gin-gonic/gin"`, `"github.com/thanhpk/randstr"`):
```go
type SubscriptionTossPayRequest struct {
	PlanId int `json:"plan_id"`
}

// SubscriptionRequestTossBilling starts the billing-auth flow for a subscription plan.
func SubscriptionRequestTossBilling(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionTossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgSubPaymentInvalidParams)
		return
	}
	if !isTossBillingEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionNotEnabled)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil || user == nil {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if plan.MaxPurchasePerUser > 0 {
		cnt, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if cnt >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorI18n(c, i18n.MsgSubscriptionPurchaseMax)
			return
		}
	}
	customerKey, err := model.GetOrCreateTossCustomerKey(userId)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	reference := fmt.Sprintf("new-api-toss-sub-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(4))
	tradeNo := "toss_sub_" + common.Sha1([]byte(reference))
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":   setting.TossActiveClientKey(),
			"customer_key": customerKey,
			"trade_no":     tradeNo,
			"success_url":  base + "/api/subscription/toss/confirm?trade_no=" + tradeNo,
			"fail_url":     base + "/api/subscription/toss/fail?trade_no=" + tradeNo,
		},
	})
}

// SubscriptionTossBillingConfirm is the billingAuth successUrl: issues + stores the
// billing key, charges the first period, and activates the subscription.
func SubscriptionTossBillingConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	authKey := c.Query("authKey")
	customerKey := c.Query("customerKey")
	tradeNo := c.Query("trade_no")
	if authKey == "" || customerKey == "" || tradeNo == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm missing params trade_no=%q", tradeNo))
		tossRedirect(c, "/console/topup")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil || order.PaymentProvider != model.PaymentProviderToss {
		tossRedirect(c, "/console/topup")
		return
	}
	if order.Status == common.TopUpStatusSuccess {
		tossRedirect(c, "/console/topup")
		return
	}
	plan, err := model.GetSubscriptionPlanById(order.PlanId)
	if err != nil {
		tossRedirect(c, "/console/topup")
		return
	}

	issued, _, err := issueTossBillingKey(ctx, authKey, customerKey)
	if err != nil || issued.BillingKey == "" {
		logger.LogError(ctx, fmt.Sprintf("Toss billing issue failed trade_no=%s err=%v", tradeNo, err))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}
	billingKeyId, err := model.StoreTossBillingKey(order.UserId, customerKey, issued.BillingKey, issued.Card.Company, issued.Card.Number)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing store failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}

	chargeKRW := tossSubscriptionChargeKRW(plan)
	orderName := fmt.Sprintf("%s 구독", plan.Title)
	result, _, err := chargeTossBilling(ctx, issued.BillingKey, customerKey, tradeNo, orderName, chargeKRW)
	if err != nil || result.Status != "DONE" || result.TotalAmount != chargeKRW {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge not done trade_no=%s err=%v", tradeNo, err))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}

	payload, _ := common.Marshal(result)
	if err := model.CompleteTossBillingOrder(tradeNo, billingKeyId, string(payload)); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing complete order failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss subscription activated trade_no=%s plan=%d user=%d", tradeNo, plan.Id, order.UserId))
	tossRedirect(c, "/console/topup")
}

// SubscriptionTossBillingFail is the billingAuth failUrl.
func SubscriptionTossBillingFail(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	code := c.Query("code")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("Toss billing auth failed trade_no=%s code=%s", tradeNo, code))
	if tradeNo != "" {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
	}
	tossRedirect(c, "/console/topup")
}

// CancelTossAutoRenew disables auto-renew for the user's active Toss subscriptions and revokes the key.
func CancelTossAutoRenew(c *gin.Context) {
	userId := c.GetInt("id")
	if err := model.CancelTossAutoRenewForUser(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
```

> `requirePaymentCompliance`, `tossRedirect`, `LockOrder`/`UnlockOrder`, `isValidServerAddress` are in package `controller`. `GetSubscriptionOrderByTradeNo`, `ExpireSubscriptionOrder`, `GetOrCreateTossCustomerKey` in package `model`. Confirm i18n keys exist: `MsgSubPaymentInvalidParams`, `MsgSubscriptionNotEnabled`, `MsgSubscriptionPurchaseMax`, `MsgUserNotExists`, `MsgPaymentNotConfigured`, `MsgPaymentCreateFailed` (grep `i18n/keys.go`; if any is missing, use the closest existing one and note it).

- [ ] **Step 2: CancelTossAutoRenewForUser 모델 함수**

In `model/subscription.go`, append:
```go
// CancelTossAutoRenewForUser disables auto-renew on the user's active Toss subscriptions
// and revokes the associated billing keys. The current period stays until EndTime.
func CancelTossAutoRenewForUser(userId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var subs []UserSubscription
		if err := tx.Where("user_id = ? AND auto_renew = ?", userId, commonTrueVal).Find(&subs).Error; err != nil {
			return err
		}
		for i := range subs {
			subs[i].AutoRenew = false
			subs[i].UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&subs[i]).Error; err != nil {
				return err
			}
			if subs[i].BillingKeyId > 0 {
				_ = RevokeTossBillingKey(tx, subs[i].BillingKeyId)
			}
		}
		return nil
	})
}
```

- [ ] **Step 3: 라우트 등록**

In `router/api-router.go`:
- Public (no auth) block (near the other `/api/toss/*` public routes ~line 65):
```go
		apiRouter.GET("/subscription/toss/confirm", controller.SubscriptionTossBillingConfirm)
		apiRouter.GET("/subscription/toss/fail", controller.SubscriptionTossBillingFail)
```
- subscriptionRoute (auth) block (near `subscription/stripe/pay` ~line 216):
```go
			subscriptionRoute.POST("/toss/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestTossBilling)
			subscriptionRoute.POST("/toss/cancel", controller.CancelTossAutoRenew)
```

- [ ] **Step 4: 빌드 확인**

Run: `cd /home/molla/new-api && go build ./... && go test ./controller/ -run 'TossSubscription'`
Expected: 빌드 성공, 테스트 PASS.

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add controller/subscription_payment_toss.go model/subscription.go router/api-router.go
git commit -m "feat(payment): Toss subscription billing controller + routes + cancel"
```

---

## Task 6: 정기청구 cron

**Files:**
- Create: `service/toss_billing_task.go`
- Modify: `main.go`

- [ ] **Step 1: cron 작성**

Create `service/toss_billing_task.go` (mirror `service/toss_pending_cleanup_task.go` structure):
```go
package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	tossBillingTickInterval = 1 * time.Minute
	tossBillingBatchSize    = 100
	tossBillingMaxFails     = 3
)

var (
	tossBillingOnce    sync.Once
	tossBillingRunning atomic.Bool
)

// StartTossBillingTask periodically charges due Toss auto-renew subscriptions.
func StartTossBillingTask() {
	tossBillingOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("toss billing task started: tick=%s", tossBillingTickInterval))
			ticker := time.NewTicker(tossBillingTickInterval)
			defer ticker.Stop()
			runTossBillingOnce()
			for range ticker.C {
				runTossBillingOnce()
			}
		})
	})
}

func runTossBillingOnce() {
	if !tossBillingRunning.CompareAndSwap(false, true) {
		return
	}
	defer tossBillingRunning.Store(false)
	ctx := context.Background()
	now := time.Now().Unix()
	subs, err := model.GetDueTossRenewals(now, tossBillingBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: query due renewals failed: %v", err))
		return
	}
	for i := range subs {
		chargeTossRenewal(ctx, &subs[i])
	}
}

func chargeTossRenewal(ctx context.Context, sub *model.UserSubscription) {
	// model.ChargeTossRenewal does the API call + renew/fail bookkeeping (kept in model
	// to avoid an import cycle would be wrong — instead service calls model + controller
	// helper). Here we delegate to the model-level orchestration function.
	if err := model.ProcessTossRenewal(ctx, sub.Id, tossBillingMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: renewal failed sub=%d: %v", sub.Id, err))
	}
}
```

> NOTE: the actual Toss HTTP charge lives in package `controller` (`chargeTossBilling`), but `service` must not import `controller`. To avoid a cycle, move the billing **charge orchestration** into a function the service can call without importing controller. Implement `model.ProcessTossRenewal` to do: decrypt key (model), then perform the charge via an injected charger. Set it up in Step 2.

- [ ] **Step 2: 충전 오케스트레이션 (의존성 주입으로 import 사이클 회피)**

In `model/toss_billing.go`, add a package-level charger hook + orchestration:
```go
import (
	"context"
	"fmt"
	"time"
)

// TossBillingCharger performs a Toss billing charge. Injected by the controller package
// at init to avoid a model→controller import cycle.
// Returns (statusDONE, totalAmount, error).
type TossBillingCharger func(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (done bool, total int64, err error)

var tossBillingCharger TossBillingCharger

func SetTossBillingCharger(fn TossBillingCharger) { tossBillingCharger = fn }

// ProcessTossRenewal charges one due subscription and applies renew/fail bookkeeping.
func ProcessTossRenewal(ctx context.Context, subId int, maxFails int) error {
	if tossBillingCharger == nil {
		return fmt.Errorf("toss billing charger not configured")
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", subId).First(&sub).Error; err != nil {
		return err
	}
	plan, err := GetSubscriptionPlanById(sub.PlanId)
	if err != nil {
		return err
	}
	billingKey, customerKey, err := GetTossBillingKeyPlain(sub.BillingKeyId)
	if err != nil {
		// key missing/revoked → stop auto-renew
		_ = MarkTossBillingFailure(subId, 1) // disable immediately (maxFails=1 path)
		return err
	}
	chargeKRW := tossRenewalChargeKRW(plan)
	tradeNo := fmt.Sprintf("toss_sub_renew_%d_%d", subId, time.Now().Unix())
	orderName := fmt.Sprintf("%s 구독 갱신", plan.Title)
	done, total, err := tossBillingCharger(ctx, billingKey, customerKey, tradeNo, orderName, chargeKRW)
	if err != nil || !done || total != chargeKRW {
		return MarkTossBillingFailure(subId, maxFails)
	}
	return RenewTossSubscription(subId, tradeNo, plan.PriceAmount)
}

// tossRenewalChargeKRW mirrors the controller-side conversion (kept here for the cron path).
func tossRenewalChargeKRW(plan *SubscriptionPlan) int64 {
	// setting import avoided: replicate using exported helper from setting.
	return tossPlanKRW(plan.PriceAmount)
}
```
Add `tossPlanKRW` to `model/toss_billing.go` (imports `"github.com/QuantumNous/new-api/setting"`, `"github.com/shopspring/decimal"`):
```go
func tossPlanKRW(priceAmount float64) int64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromFloat(priceAmount).Mul(decimal.NewFromFloat(unit)).Round(0).IntPart()
}
```

In `controller/subscription_payment_toss.go`, wire the charger in an `init()`:
```go
func init() {
	model.SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (bool, int64, error) {
		res, _, err := chargeTossBilling(ctx, billingKey, customerKey, orderId, orderName, amount)
		if err != nil {
			return false, 0, err
		}
		return res.Status == "DONE", res.TotalAmount, nil
	})
}
```

> Simplify Task 3's `tossSubscriptionChargeKRW` to call `model.TossPlanKRW` to keep ONE conversion source. Export `tossPlanKRW` as `TossPlanKRW` in `model/toss_billing.go` and have the controller helper delegate: `func tossSubscriptionChargeKRW(plan *model.SubscriptionPlan) int64 { return model.TossPlanKRW(plan.PriceAmount) }`. Update Task 3's test expectation accordingly (it still expects 15000 for price 10 × unit 1500).

- [ ] **Step 3: main.go 등록**

In `main.go`, after `service.StartTossPendingCleanupTask()`, add:
```go
	service.StartTossBillingTask()
```

- [ ] **Step 4: 빌드 + 테스트**

Run: `cd /home/molla/new-api && go build ./... && go test ./controller/ ./common/ -run 'Toss|Encrypt'`
Expected: 빌드 성공(특히 import 사이클 없음), 테스트 PASS.

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add service/toss_billing_task.go model/toss_billing.go controller/subscription_payment_toss.go main.go
git commit -m "feat(payment): Toss recurring billing cron (charger injection avoids import cycle)"
```

---

## Task 7: 프론트 — API + 빌링 훅

**Files:**
- Modify: `web/default/src/features/subscriptions/api.ts`
- Create: `web/default/src/features/subscriptions/hooks/use-toss-billing.ts`

> 먼저 `subscriptions/api.ts`의 `paySubscriptionStripe`와 `SubscriptionPayResponse` 타입, 그리고 `features/wallet/hooks/use-toss-payment.ts`(SDK 사용 패턴)를 읽는다.

- [ ] **Step 1: api 함수 추가**

In `web/default/src/features/subscriptions/api.ts`, add (mirror `paySubscriptionStripe`'s http client usage):
```typescript
export async function paySubscriptionToss(data: { plan_id: number }) {
  const res = await api.post('/api/subscription/toss/pay', data, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function cancelTossAutoRenew() {
  const res = await api.post('/api/subscription/toss/cancel', {})
  return res.data
}
```

- [ ] **Step 2: 빌링 훅 작성**

Create `web/default/src/features/subscriptions/hooks/use-toss-billing.ts`:
```typescript
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import { paySubscriptionToss } from '../api'

// Toss 자동결제 구독: 빌링키 인증창을 연다.
export function useTossBilling() {
  const [processing, setProcessing] = useState(false)

  const subscribeWithToss = useCallback(async (planId: number) => {
    try {
      setProcessing(true)
      const resp = await paySubscriptionToss({ plan_id: planId })
      const ok = resp?.success === true || resp?.message === 'success'
      if (!ok || !resp?.data) {
        toast.error(resp?.message || i18next.t('Payment request failed'))
        return false
      }
      const { client_key, customer_key, success_url, fail_url } = resp.data
      const tossPayments = await loadTossPayments(client_key)
      const payment = tossPayments.payment({ customerKey: customer_key })
      await payment.requestBillingAuth({
        method: 'CARD',
        successUrl: success_url,
        failUrl: fail_url,
      })
      return true
    } catch (err) {
      const e = err as { code?: string; message?: string }
      if (e?.code && e.code !== 'USER_CANCEL') {
        toast.error(i18next.t('Payment request failed'))
      }
      return false
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, subscribeWithToss }
}
```

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bun run typecheck`
Expected: wallet/subscriptions에 새 에러 없음.

- [ ] **Step 4: 커밋**
```bash
cd /home/molla/new-api
git add web/default/src/features/subscriptions/api.ts web/default/src/features/subscriptions/hooks/use-toss-billing.ts
git commit -m "feat(web): Toss subscription billing api + requestBillingAuth hook"
```

---

## Task 8: 프론트 — 구독 다이얼로그에 Toss 버튼 + 해지 UI

**Files:**
- Modify: `web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx`
- Modify: 구독 self 화면 컴포넌트(자동결제 해지 버튼)

> 먼저 `subscription-purchase-dialog.tsx`에서 Stripe/Creem 결제 버튼이 어떻게 렌더·호출되는지 읽고, `isTossBillingEnabled` 노출 플래그를 어디서 받는지 확인한다(필요 시 백엔드 `GetSubscriptionPlans` 또는 topup/info에 `enable_toss_billing` 노출 추가). 백엔드 노출이 필요하면 `controller/subscription.go`의 plans/self 응답 또는 `GetTopUpInfo`에 `"enable_toss_billing": isTossBillingEnabled()` 추가.

- [ ] **Step 1: 노출 플래그 (백엔드)**

In `controller/topup.go` `GetTopUpInfo` data map, add: `"enable_toss_billing": isTossBillingEnabled(),`. In `web/default/src/features/wallet/types.ts` `TopupInfo`, add `enable_toss_billing?: boolean`.

- [ ] **Step 2: 다이얼로그에 Toss 구독 버튼**

In `subscription-purchase-dialog.tsx`, import `useTossBilling` from `../../hooks/use-toss-billing` and the topup-info hook used for flags. Add a Toss button alongside the Stripe/Creem buttons, gated on the `enable_toss_billing` flag, calling `subscribeWithToss(plan.id)`. Mirror the existing button markup/disabled/processing pattern.

- [ ] **Step 3: 자동결제 해지 버튼 (구독 self 화면)**

In the subscription self view (the component listing the user's active subscriptions — find via `grep -rn "subscription/self\|GetSubscriptionSelf\|auto_renew" web/default/src`), add a "자동결제 해지" button for active Toss auto-renew subscriptions that calls `cancelTossAutoRenew()` then refetches. Show registered card masked number if available.

- [ ] **Step 4: 타입체크 + 빌드**

Run: `cd /home/molla/new-api/web/default && bun run typecheck && bun run build`
Expected: 에러 없음.

- [ ] **Step 5: 커밋**
```bash
cd /home/molla/new-api
git add web/default/src controller/topup.go
git commit -m "feat(web): Toss subscribe button + auto-renew cancel UI; expose enable_toss_billing"
```

---

## Task 9: 통합 검증

- [ ] **Step 1: 백엔드 빌드 + Toss 테스트**

Run: `cd /home/molla/new-api && go build ./... && go test ./common/ ./controller/ ./model/ -run 'Toss|Encrypt'`
Expected: 빌드 성공, 모든 관련 테스트 PASS.

- [ ] **Step 2: 프론트 빌드**

Run: `cd /home/molla/new-api/web/default && bun run typecheck && bun run build`
Expected: 새 에러 없음, 빌드 성공.

- [ ] **Step 3: 수동 E2E 체크리스트 (Toss 테스트 키 + 짧은 주기 플랜)**

1. 관리자: **짧은 기간 구독 플랜** 생성(예: DurationUnit=custom, CustomSeconds=300=5분; QuotaResetPeriod=never; UpgradeGroup 지정 가능).
2. 유저: 구독 다이얼로그에서 **Toss로 구독** → 빌링 인증창 → 정상 카드 등록 → successUrl(`/api/subscription/toss/confirm`) → 첫 청구 → 구독 활성(그룹 업그레이드 확인).
3. **자동 갱신 관찰**: 약 5분 뒤 cron(1분 주기)이 빌링키로 재청구 → EndTime 연장, 새 SubscriptionOrder(success) 기록, 그룹 유지.
4. **해지**: 자동결제 해지 → `AutoRenew=false`, 빌링키 revoked → 다음 주기에 청구 안 됨 → EndTime 후 만료·다운그레이드.
5. **실패 처리**: (선택) 빌링키 손상/거부 시 3회 실패 후 `AutoRenew=false` 자동.

- [ ] **Step 4: DB 확인 쿼리(운영 검증용)**

```sql
SELECT id,user_id,plan_id,status,auto_renew,next_billing_time,billing_key_id,billing_fail_count,end_time FROM user_subscriptions WHERE billing_key_id>0 ORDER BY id DESC;
SELECT id,user_id,status FROM user_billing_keys ORDER BY id DESC;
SELECT id,plan_id,status,payment_provider,trade_no FROM subscription_orders WHERE payment_provider='toss' ORDER BY id DESC;
```

---

## Self-Review

- **스펙(§5) 커버리지:** 빌링키 발급·암호화(Task 1·2·3·5), `UserSubscription` 자동결제 필드(Task 2), 구독 시작·첫청구·완료(Task 5+4), 정기청구 cron(Task 6), 해지(Task 5·8), 프론트(Task 7·8). ✓
- **확인 필요(구현 중 검증):** (a) i18n 키 존재 여부(Task 5 Step1 주석) — 없으면 가까운 키 사용; (b) `model/main.go` AutoMigrate 목록 위치(Task 2 Step3); (c) 구독 self 화면 컴포넌트 경로(Task 8 Step3) — grep로 확정; (d) `subscriptions/api.ts`의 http 클라이언트 import 이름(`api`) 확인.
- **타입/시그니처 일관성:** 금액 환산은 `model.TossPlanKRW`(단일 소스)로 통일(Task 3 헬퍼가 위임, Task 6에서 재사용). `chargeTossBilling`/`issueTossBillingKey`는 `(*resp,int,error)`. `tossConfirmResponse`/`tossAPIBase`/`tossRedirect`/`isValidServerAddress`/`LockOrder`는 Phase 1(topup_toss.go) 정의 재사용. cron→model은 charger 주입으로 import 사이클 회피, controller가 `init()`에서 주입.
- **플레이스홀더:** 코드 스텝은 실제 코드 포함. Task 8의 일부 UI는 "기존 버튼 패턴 복제"로 위임(다이얼로그 마크업이 길고 가변적이라 읽고 미러가 안전 — 읽기 스텝 선행).
</content>
