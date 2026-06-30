# 지갑 자동충전 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 기존 구독과 분리된 `정기결제`와 `자동결제`를 만들고, Toss 빌링키 결제 성공 시 일반 지갑 충전 잔액으로 반영한다.

**Architecture:** 새 도메인은 `wallet_auto_recharges` 테이블, 모델 서비스, 컨트롤러, 1분 주기 백그라운드 잡으로 구성한다. Toss 빌링키 발급/충전 함수는 기존 Toss 구독 결제 구현을 재사용하되, 성공 정산은 `TopUp`과 `CreditTopUpTarget`을 통해 개인/조직 지갑 quota에 적립한다. 프론트엔드는 기존 지갑 화면 안에 `충전`, `정기결제`, `자동결제` 탭을 추가하고 개인/조직 API prefix만 다르게 쓰는 공용 컴포넌트를 둔다.

**Tech Stack:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL, React 19, TypeScript, Base UI Tabs, Tailwind CSS, Bun, Toss Payments SDK.

## Global Constraints

- `common/json.go`의 `common.Marshal`, `common.Unmarshal`, `common.UnmarshalJsonStr`, `common.DecodeJson`, `common.GetJsonType`을 사용하고, marshal/unmarshal 용도로 `encoding/json`을 직접 호출하지 않는다.
- SQLite, MySQL >= 5.7.8, PostgreSQL >= 9.6을 모두 지원한다. DB 로직은 GORM 중심으로 작성하고, DB별 raw SQL이나 부분 인덱스에 의존하지 않는다.
- 프론트엔드 명령은 `web/default/`에서 `bun`을 사용한다.
- `nеw-аρi`, `QuаntumΝоuѕ` 관련 기존 표기, 저작권, 메타데이터, 브랜드 정보는 수정하거나 제거하지 않는다.
- 기존 구독 결제/갱신 정책의 의미와 테이블은 변경하지 않는다.
- Toss만 지원한다. Stripe, PayPal, Creem, Waffo 계열은 이번 기능에 연결하지 않는다.
- 개인 지갑과 조직 지갑을 모두 지원한다. 조직 정책 생성/해지는 조직 소유자만 가능하다.
- 같은 대상에는 활성 정기결제 1개와 활성 자동결제 1개만 허용한다.
- 자동결제 기본 안전장치는 cooldown 1시간, 일일 성공 충전 제한 3회, 최대 실패 3회다.
- 새 UI 문구는 `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`에 번역 키를 추가하고 `bun run i18n:sync`로 동기화한다.

---

## File Structure

### Backend

- Create: `model/wallet_auto_recharge.go`
  - `WalletAutoRecharge` GORM 모델, 상태/유형 상수, 유효성 검사, pending 생성, Toss confirm 활성화, cancel, 조회, due 조회, charge 성공 정산을 담당한다.
- Create: `model/wallet_auto_recharge_test.go`
  - 모델 검증, 활성 정책 중복, 즉시 충전 정산, 자동결제 cooldown/일일 제한을 검증한다.
- Create: `controller/wallet_auto_recharge.go`
  - 개인/조직 API handler, Toss billing auth 시작, confirm/fail callback, 권한 검증을 담당한다.
- Create: `controller/wallet_auto_recharge_test.go`
  - 개인 API와 조직 소유자 권한, 비소유자 거부, callback 동작을 검증한다.
- Create: `service/wallet_auto_recharge_task.go`
  - master node에서 1분마다 due 정기결제와 threshold 자동결제를 처리한다.
- Create: `service/wallet_auto_recharge_task_test.go`
  - worker가 due 정책만 처리하고 중복 실행을 막는지 검증한다.
- Modify: `model/main.go`
  - `DB.AutoMigrate`에 `&WalletAutoRecharge{}`를 추가한다.
- Modify: `router/api-router.go`
  - 개인/조직 API와 Toss confirm/fail callback route를 추가한다.
- Modify: `main.go`
  - `service.StartWalletAutoRechargeTask()`를 시작한다.

### Frontend

- Modify: `web/default/src/features/wallet/types.ts`
  - 자동충전 정책, 요청/응답 타입을 추가한다.
- Modify: `web/default/src/features/wallet/api.ts`
  - 개인 자동충전 API 함수와 Toss billing auth 요청 함수를 추가한다.
- Create: `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`
  - 공용 로딩/생성/해지 훅과 Toss billing auth 호출을 제공한다.
- Create: `web/default/src/features/wallet/components/auto-recharge-card.tsx`
  - `정기결제`와 `자동결제` 탭에서 공통으로 쓰는 카드 UI를 제공한다.
- Modify: `web/default/src/features/wallet/index.tsx`
  - 기존 지갑 UI를 `충전` 탭으로 감싸고 `정기결제`, `자동결제` 탭을 추가한다.
- Modify: `web/default/src/features/organizations/components/organization-wallet.tsx`
  - 조직 지갑에도 같은 탭과 자동충전 카드 UI를 추가한다.
- Modify: `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`
  - 새 UI 문구를 모든 지원 언어에 추가한다.

---

### Task 1: 모델과 마이그레이션

**Files:**
- Create: `model/wallet_auto_recharge.go`
- Create: `model/wallet_auto_recharge_test.go`
- Modify: `model/main.go`

**Interfaces:**
- Consumes:
  - `model.TopUp`, `model.CreditTopUpTarget(tx *gorm.DB, topUp *TopUp, quota int) error`
  - `model.GetTossBillingKeyPlain(id int) (key string, customerKey string, err error)`
  - `model.RevokeTossBillingKey(tx *gorm.DB, id int) error`
  - `common.QuotaPerUnit`, `common.TopUpStatusSuccess`
- Produces:
  - `type WalletAutoRecharge struct`
  - `func CreatePendingWalletAutoRecharge(req CreateWalletAutoRechargeRequest) (*WalletAutoRecharge, error)`
  - `func ActivateWalletAutoRechargeFromToss(tradeNo string, billingKeyId int, cardCompany string, cardMasked string, chargeNow bool, charger TossBillingCharger) (*WalletAutoRecharge, error)`
  - `func CancelWalletAutoRecharge(id int, targetType string, targetId int) error`
  - `func ListWalletAutoRecharges(targetType string, targetId int) ([]WalletAutoRecharge, error)`
  - `func ProcessWalletAutoRecharge(ctx context.Context, policyId int, now time.Time, maxFails int, charger TossBillingCharger) error`
  - `func ProcessWalletAutoRechargeWithConfiguredCharger(ctx context.Context, policyId int, now time.Time, maxFails int) error`
  - `func GetDueScheduledWalletAutoRecharges(nowUnix int64, limit int) ([]WalletAutoRecharge, error)`
  - `func GetActiveThresholdWalletAutoRecharges(limit int) ([]WalletAutoRecharge, error)`

- [ ] **Step 1: Write failing model tests**

Add this test scaffold to `model/wallet_auto_recharge_test.go`. The tests use in-memory SQLite because GORM code must remain portable to MySQL and PostgreSQL.

```go
package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWalletAutoRechargeTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, DB.AutoMigrate(&User{}, &Organization{}, &TopUp{}, &UserBillingKey{}, &WalletAutoRecharge{}))
}

func stubWalletCharger(done bool, total int64, err error) TossBillingCharger {
	return func(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (bool, int64, error) {
		return done, total, err
	}
}

func TestCreatePendingWalletAutoRechargeRejectsDuplicateActiveType(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, Amount: 10, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusActive, NextChargeTime: now + 3600,
	}).Error)

	_, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, Amount: 20, IntervalUnit: WalletAutoRechargeIntervalMonth, IntervalValue: 1,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "active wallet auto recharge already exists")
}

func TestProcessWalletAutoRechargeCreditsUserWallet(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Quota: 0, AffCode: "wallet-auto-owner"}).Error)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{Id: 11, UserId: 1, CustomerKey: "customer-1", EncryptedKey: enc, Status: BillingKeyStatusActive}).Error)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeScheduled, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 11, Amount: 10, IntervalUnit: WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, Status: WalletAutoRechargeStatusActive, NextChargeTime: time.Now().Add(-time.Minute).Unix(),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err := ProcessWalletAutoRecharge(context.Background(), policy.Id, time.Now(), 3, stubWalletCharger(true, 13000, nil))

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, 1).Error)
	require.Equal(t, int(10*common.QuotaPerUnit), user.Quota)
	var topUp TopUp
	require.NoError(t, DB.First(&topUp, "target_type = ? AND target_id = ?", TopUpTargetTypeUser, 1).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, PaymentProviderToss, topUp.PaymentProvider)
}

func TestProcessThresholdWalletAutoRechargeRespectsCooldownAndDailyLimit(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	now := time.Now()
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Quota: 0, AffCode: "wallet-threshold-owner"}).Error)
	enc, err := common.EncryptString("billing-key")
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UserBillingKey{Id: 12, UserId: 1, CustomerKey: "customer-2", EncryptedKey: enc, Status: BillingKeyStatusActive}).Error)
	policy := WalletAutoRecharge{
		Type: WalletAutoRechargeTypeThreshold, TargetType: TopUpTargetTypeUser, TargetId: 1,
		OwnerUserId: 1, BillingKeyId: 12, Amount: 10, ThresholdAmount: 5,
		ThresholdQuota: int(5 * common.QuotaPerUnit), Status: WalletAutoRechargeStatusActive,
		CooldownUntil: now.Add(time.Hour).Unix(), DailyChargeCount: 3, DailyChargeDate: now.Format("2006-01-02"),
	}
	require.NoError(t, DB.Create(&policy).Error)

	err := ProcessWalletAutoRecharge(context.Background(), policy.Id, now, 3, stubWalletCharger(true, 13000, nil))

	require.NoError(t, err)
	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}
```

- [ ] **Step 2: Run model tests and confirm failure**

Run:

```bash
go test ./model -run WalletAutoRecharge -count=1
```

Expected: FAIL because `WalletAutoRecharge`, request types, and functions do not exist.

- [ ] **Step 3: Add the model implementation**

Create `model/wallet_auto_recharge.go` with the model, constants, request type, validations, and charge settlement. Use GORM APIs and no raw SQL.

```go
package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	WalletAutoRechargeTypeScheduled = "scheduled"
	WalletAutoRechargeTypeThreshold = "threshold"

	WalletAutoRechargeStatusPending   = "pending"
	WalletAutoRechargeStatusActive    = "active"
	WalletAutoRechargeStatusCancelled = "cancelled"
	WalletAutoRechargeStatusFailed    = "failed"

	WalletAutoRechargeIntervalMonth  = "month"
	WalletAutoRechargeIntervalDay    = "day"
	WalletAutoRechargeIntervalCustom = "custom"

	WalletAutoRechargeThresholdCooldownSeconds = 3600
	WalletAutoRechargeDailyLimit               = 3
)

type WalletAutoRecharge struct {
	Id                 int     `json:"id"`
	Type               string  `json:"type" gorm:"type:varchar(32);index"`
	TargetType         string  `json:"target_type" gorm:"type:varchar(32);index"`
	TargetId           int     `json:"target_id" gorm:"index"`
	OwnerUserId        int     `json:"owner_user_id" gorm:"index"`
	BillingKeyId       int     `json:"billing_key_id" gorm:"index"`
	Amount             float64 `json:"amount"`
	ThresholdAmount    float64 `json:"threshold_amount"`
	ThresholdQuota     int     `json:"threshold_quota"`
	IntervalUnit       string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue      int     `json:"interval_value"`
	CustomSeconds      int64   `json:"custom_seconds"`
	ChargeImmediately  bool    `json:"charge_immediately"`
	NextChargeTime     int64   `json:"next_charge_time" gorm:"index"`
	LastChargeTime     int64   `json:"last_charge_time"`
	CooldownUntil      int64   `json:"cooldown_until" gorm:"index"`
	DailyChargeCount   int     `json:"daily_charge_count"`
	DailyChargeDate    string  `json:"daily_charge_date" gorm:"type:varchar(10)"`
	Status             string  `json:"status" gorm:"type:varchar(16);index"`
	FailCount          int     `json:"fail_count"`
	LastError          string  `json:"last_error" gorm:"type:varchar(255)"`
	CardCompany        string  `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked   string  `json:"card_number_masked" gorm:"type:varchar(32)"`
	LastTradeNo        string  `json:"last_trade_no" gorm:"type:varchar(255);index"`
	CustomerKey        string  `json:"-" gorm:"type:varchar(64);index"`
	AuthTradeNo        string  `json:"-" gorm:"type:varchar(255);uniqueIndex"`
	CreateTime         int64   `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime         int64   `json:"update_time" gorm:"autoUpdateTime"`
}

type CreateWalletAutoRechargeRequest struct {
	Type              string
	TargetType        string
	TargetId          int
	OwnerUserId       int
	CustomerKey       string
	AuthTradeNo       string
	Amount            float64
	ThresholdAmount   float64
	IntervalUnit      string
	IntervalValue     int
	CustomSeconds     int64
	ChargeImmediately bool
}

func (req CreateWalletAutoRechargeRequest) validate() error {
	if req.Type != WalletAutoRechargeTypeScheduled && req.Type != WalletAutoRechargeTypeThreshold {
		return errors.New("invalid wallet auto recharge type")
	}
	if req.TargetType != TopUpTargetTypeUser && req.TargetType != TopUpTargetTypeOrganization {
		return errors.New("invalid wallet auto recharge target")
	}
	if req.TargetId <= 0 || req.OwnerUserId <= 0 {
		return errors.New("invalid wallet auto recharge owner or target")
	}
	if req.Amount < float64(setting.TossMinTopUp) {
		return errors.New("wallet auto recharge amount is below Toss minimum")
	}
	if req.Type == WalletAutoRechargeTypeScheduled && req.IntervalUnit == "" {
		return errors.New("wallet auto recharge interval is required")
	}
	if req.Type == WalletAutoRechargeTypeThreshold && req.ThresholdAmount < 0 {
		return errors.New("wallet auto recharge threshold cannot be negative")
	}
	return nil
}

func CreatePendingWalletAutoRecharge(req CreateWalletAutoRechargeRequest) (*WalletAutoRecharge, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	now := time.Now()
	policy := &WalletAutoRecharge{
		Type: req.Type, TargetType: req.TargetType, TargetId: req.TargetId, OwnerUserId: req.OwnerUserId,
		Amount: req.Amount, ThresholdAmount: req.ThresholdAmount,
		ThresholdQuota: int(decimal.NewFromFloat(req.ThresholdAmount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()),
		IntervalUnit: req.IntervalUnit, IntervalValue: req.IntervalValue, CustomSeconds: req.CustomSeconds,
		ChargeImmediately: req.ChargeImmediately, Status: WalletAutoRechargeStatusPending,
		CustomerKey: req.CustomerKey, AuthTradeNo: req.AuthTradeNo, NextChargeTime: nextWalletChargeTime(now, req.IntervalUnit, req.IntervalValue, req.CustomSeconds).Unix(),
	}
	return policy, DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&WalletAutoRecharge{}).Where("target_type = ? AND target_id = ? AND type = ? AND status IN ?", req.TargetType, req.TargetId, req.Type, []string{WalletAutoRechargeStatusPending, WalletAutoRechargeStatusActive}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("active wallet auto recharge already exists")
		}
		return tx.Create(policy).Error
	})
}

func nextWalletChargeTime(base time.Time, unit string, value int, customSeconds int64) time.Time {
	if value <= 0 {
		value = 1
	}
	switch unit {
	case WalletAutoRechargeIntervalDay:
		return base.AddDate(0, 0, value)
	case WalletAutoRechargeIntervalCustom:
		if customSeconds <= 0 {
			customSeconds = 86400
		}
		return base.Add(time.Duration(customSeconds) * time.Second)
	default:
		return base.AddDate(0, value, 0)
	}
}

func CancelWalletAutoRecharge(id int, targetType string, targetId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Where("id = ? AND target_type = ? AND target_id = ?", id, targetType, targetId).First(&policy).Error; err != nil {
			return err
		}
		if policy.Status == WalletAutoRechargeStatusCancelled {
			return nil
		}
		if policy.BillingKeyId > 0 {
			if err := RevokeTossBillingKey(tx, policy.BillingKeyId); err != nil {
				return err
			}
		}
		return tx.Model(&policy).Updates(map[string]interface{}{"status": WalletAutoRechargeStatusCancelled}).Error
	})
}

func ListWalletAutoRecharges(targetType string, targetId int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("target_type = ? AND target_id = ?", targetType, targetId).Order("id desc").Find(&rows).Error
	return rows, err
}

func GetDueScheduledWalletAutoRecharges(nowUnix int64, limit int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("type = ? AND status = ? AND next_charge_time > 0 AND next_charge_time <= ?", WalletAutoRechargeTypeScheduled, WalletAutoRechargeStatusActive, nowUnix).Limit(limit).Find(&rows).Error
	return rows, err
}

func GetActiveThresholdWalletAutoRecharges(limit int) ([]WalletAutoRecharge, error) {
	var rows []WalletAutoRecharge
	err := DB.Where("type = ? AND status = ?", WalletAutoRechargeTypeThreshold, WalletAutoRechargeStatusActive).Limit(limit).Find(&rows).Error
	return rows, err
}

func ProcessWalletAutoRecharge(ctx context.Context, policyId int, now time.Time, maxFails int, charger TossBillingCharger) error {
	if charger == nil {
		return errors.New("toss wallet auto recharge charger not configured")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var policy WalletAutoRecharge
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", policyId).First(&policy).Error; err != nil {
			return err
		}
		if policy.Status != WalletAutoRechargeStatusActive {
			return nil
		}
		if policy.Type == WalletAutoRechargeTypeScheduled && policy.NextChargeTime > now.Unix() {
			return nil
		}
		if policy.Type == WalletAutoRechargeTypeThreshold {
			ok, err := walletThresholdShouldCharge(tx, &policy, now)
			if err != nil || !ok {
				return err
			}
		}
		billingKey, customerKey, err := GetTossBillingKeyPlain(policy.BillingKeyId)
		if err != nil {
			return markWalletAutoRechargeFailure(tx, &policy, maxFails, err)
		}
		chargeKRW := walletAutoRechargeKRW(policy.Amount)
		tradeNo := walletAutoRechargeTradeNo(policy, now)
		done, total, err := charger(ctx, billingKey, customerKey, tradeNo, "지갑 자동충전", chargeKRW)
		if err != nil || !done || total != chargeKRW {
			if err == nil {
				err = fmt.Errorf("toss wallet auto recharge amount mismatch")
			}
			return markWalletAutoRechargeFailure(tx, &policy, maxFails, err)
		}
		return creditWalletAutoRecharge(tx, &policy, tradeNo, chargeKRW, now)
	})
}

func ProcessWalletAutoRechargeWithConfiguredCharger(ctx context.Context, policyId int, now time.Time, maxFails int) error {
	return ProcessWalletAutoRecharge(ctx, policyId, now, maxFails, tossBillingCharger)
}
```

Add these helper bodies in the same file:

```go
func walletAutoRechargeKRW(amount float64) int64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromFloat(amount).Mul(decimal.NewFromFloat(unit)).Round(0).IntPart()
}

func walletAutoRechargeTradeNo(policy WalletAutoRecharge, now time.Time) string {
	if policy.Type == WalletAutoRechargeTypeThreshold {
		return fmt.Sprintf("wallet_auto_%d_%s_%d", policy.Id, now.Format("2006010215"), policy.DailyChargeCount+1)
	}
	return fmt.Sprintf("wallet_auto_%d_%d", policy.Id, policy.NextChargeTime)
}

func walletThresholdShouldCharge(tx *gorm.DB, policy *WalletAutoRecharge, now time.Time) (bool, error) {
	if policy.CooldownUntil > now.Unix() {
		return false, nil
	}
	today := now.Format("2006-01-02")
	if policy.DailyChargeDate != today {
		policy.DailyChargeDate = today
		policy.DailyChargeCount = 0
	}
	if policy.DailyChargeCount >= WalletAutoRechargeDailyLimit {
		return false, nil
	}
	var quota int
	switch policy.TargetType {
	case TopUpTargetTypeOrganization:
		if err := tx.Model(&Organization{}).Where("id = ?", policy.TargetId).Select("quota").First(&quota).Error; err != nil {
			return false, err
		}
	default:
		if err := tx.Model(&User{}).Where("id = ?", policy.TargetId).Select("quota").First(&quota).Error; err != nil {
			return false, err
		}
	}
	return quota <= policy.ThresholdQuota, nil
}

func creditWalletAutoRecharge(tx *gorm.DB, policy *WalletAutoRecharge, tradeNo string, chargeKRW int64, now time.Time) error {
	topUp := &TopUp{
		UserId: policy.OwnerUserId, TargetType: policy.TargetType, TargetId: policy.TargetId,
		Amount: chargeKRW, Money: policy.Amount, TradeNo: tradeNo, ProviderOrderId: tradeNo,
		PaymentMethod: PaymentMethodToss, PaymentProvider: PaymentProviderToss,
		CreateTime: now.Unix(), CompleteTime: now.Unix(), Status: common.TopUpStatusSuccess,
	}
	quotaToAdd := int(decimal.NewFromFloat(policy.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
	if quotaToAdd <= 0 {
		return errors.New("invalid wallet auto recharge quota")
	}
	if err := tx.Create(topUp).Error; err != nil {
		return err
	}
	if err := CreditTopUpTarget(tx, topUp, quotaToAdd); err != nil {
		return err
	}
	updates := map[string]interface{}{
		"last_charge_time": now.Unix(),
		"last_trade_no": tradeNo,
		"fail_count": 0,
		"last_error": "",
	}
	if policy.Type == WalletAutoRechargeTypeScheduled {
		updates["next_charge_time"] = nextWalletChargeTime(time.Unix(policy.NextChargeTime, 0), policy.IntervalUnit, policy.IntervalValue, policy.CustomSeconds).Unix()
	}
	if policy.Type == WalletAutoRechargeTypeThreshold {
		updates["cooldown_until"] = now.Add(WalletAutoRechargeThresholdCooldownSeconds * time.Second).Unix()
		updates["daily_charge_date"] = now.Format("2006-01-02")
		updates["daily_charge_count"] = policy.DailyChargeCount + 1
	}
	return tx.Model(policy).Updates(updates).Error
}

func markWalletAutoRechargeFailure(tx *gorm.DB, policy *WalletAutoRecharge, maxFails int, cause error) error {
	failCount := policy.FailCount + 1
	status := policy.Status
	if failCount >= maxFails {
		status = WalletAutoRechargeStatusFailed
	}
	msg := cause.Error()
	if len(msg) > 255 {
		msg = msg[:255]
	}
	return tx.Model(policy).Updates(map[string]interface{}{
		"fail_count": failCount,
		"status": status,
		"last_error": msg,
	}).Error
}
```

- [ ] **Step 4: Wire migration**

Modify `model/main.go` in the `DB.AutoMigrate` list by adding `&WalletAutoRecharge{}` next to `&UserBillingKey{}`:

```go
&UserBillingKey{},
&WalletAutoRecharge{},
&SubscriptionPreConsumeRecord{},
```

- [ ] **Step 5: Run model tests and format**

Run:

```bash
gofmt -w model/wallet_auto_recharge.go model/wallet_auto_recharge_test.go model/main.go
go test ./model -run WalletAutoRecharge -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add model/wallet_auto_recharge.go model/wallet_auto_recharge_test.go model/main.go
git commit -m "feat: add wallet auto recharge model"
```

---

### Task 2: 백그라운드 결제 잡

**Files:**
- Create: `service/wallet_auto_recharge_task.go`
- Create: `service/wallet_auto_recharge_task_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes:
  - `model.GetDueScheduledWalletAutoRecharges(nowUnix int64, limit int)`
  - `model.GetActiveThresholdWalletAutoRecharges(limit int)`
  - `model.ProcessWalletAutoRecharge(ctx context.Context, policyId int, now time.Time, maxFails int, charger TossBillingCharger)`
- Produces:
  - `func StartWalletAutoRechargeTask()`
  - `func runWalletAutoRechargeOnce()`

- [ ] **Step 1: Write failing service tests**

Create `service/wallet_auto_recharge_task_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestWalletAutoRechargeTaskQueriesScheduledAndThresholdPolicies(t *testing.T) {
	require.Equal(t, 1*time.Minute, walletAutoRechargeTickInterval)
	require.Equal(t, 100, walletAutoRechargeBatchSize)
	require.Equal(t, 3, walletAutoRechargeMaxFails)
	require.NotNil(t, chargeWalletAutoRecharge)
	require.NotNil(t, model.ProcessWalletAutoRecharge)
}
```

- [ ] **Step 2: Run service tests and confirm failure**

Run:

```bash
go test ./service -run WalletAutoRecharge -count=1
```

Expected: FAIL because the task file and constants do not exist.

- [ ] **Step 3: Add task implementation**

Create `service/wallet_auto_recharge_task.go`:

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
	walletAutoRechargeTickInterval = 1 * time.Minute
	walletAutoRechargeBatchSize    = 100
	walletAutoRechargeMaxFails     = 3
)

var (
	walletAutoRechargeOnce    sync.Once
	walletAutoRechargeRunning atomic.Bool
)

func StartWalletAutoRechargeTask() {
	walletAutoRechargeOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("wallet auto recharge task started: tick=%s", walletAutoRechargeTickInterval))
			ticker := time.NewTicker(walletAutoRechargeTickInterval)
			defer ticker.Stop()
			runWalletAutoRechargeOnce()
			for range ticker.C {
				runWalletAutoRechargeOnce()
			}
		})
	})
}

func runWalletAutoRechargeOnce() {
	if !walletAutoRechargeRunning.CompareAndSwap(false, true) {
		return
	}
	defer walletAutoRechargeRunning.Store(false)
	ctx := context.Background()
	now := time.Now()
	scheduled, err := model.GetDueScheduledWalletAutoRecharges(now.Unix(), walletAutoRechargeBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query scheduled failed: %v", err))
	} else {
		for i := range scheduled {
			chargeWalletAutoRecharge(ctx, &scheduled[i], now)
		}
	}
	threshold, err := model.GetActiveThresholdWalletAutoRecharges(walletAutoRechargeBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query threshold failed: %v", err))
		return
	}
	for i := range threshold {
		chargeWalletAutoRecharge(ctx, &threshold[i], now)
	}
}

func chargeWalletAutoRecharge(ctx context.Context, policy *model.WalletAutoRecharge, now time.Time) {
	if err := model.ProcessWalletAutoRechargeWithConfiguredCharger(ctx, policy.Id, now, walletAutoRechargeMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: charge failed policy=%d: %v", policy.Id, err))
	}
}
```

- [ ] **Step 4: Start task in main**

Modify `main.go` next to the existing subscription/Toss tasks:

```go
service.StartTossBillingTask()
service.StartWalletAutoRechargeTask()
```

- [ ] **Step 5: Run service tests**

Run:

```bash
gofmt -w service/wallet_auto_recharge_task.go service/wallet_auto_recharge_task_test.go main.go
go test ./service -run WalletAutoRecharge -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add service/wallet_auto_recharge_task.go service/wallet_auto_recharge_task_test.go main.go
git commit -m "feat: process wallet auto recharge jobs"
```

---

### Task 3: API와 Toss Billing Auth 콜백

**Files:**
- Create: `controller/wallet_auto_recharge.go`
- Create: `controller/wallet_auto_recharge_test.go`
- Modify: `router/api-router.go`

**Interfaces:**
- Consumes:
  - `isTossBillingEnabled()`
  - `requirePaymentCompliance(c *gin.Context) bool`
  - `issueTossBillingKey(ctx, authKey, customerKey string)`
  - `model.GetOrCreateTossCustomerKey(userId int)`
  - `model.StoreTossBillingKey(userId, customerKey, billingKey, cardCompany, cardMasked string)`
  - Task 1 model functions.
- Produces:
  - `func GetWalletAutoRecharge(c *gin.Context)`
  - `func RequestWalletScheduledRecharge(c *gin.Context)`
  - `func RequestWalletThresholdRecharge(c *gin.Context)`
  - `func CancelWalletAutoRecharge(c *gin.Context)`
  - `func GetOrganizationWalletAutoRecharge(c *gin.Context)`
  - `func RequestOrganizationWalletScheduledRecharge(c *gin.Context)`
  - `func RequestOrganizationWalletThresholdRecharge(c *gin.Context)`
  - `func CancelOrganizationWalletAutoRecharge(c *gin.Context)`
  - `func WalletAutoRechargeTossConfirm(c *gin.Context)`
  - `func WalletAutoRechargeTossFail(c *gin.Context)`

- [ ] **Step 1: Write failing controller tests**

Create tests that use `performOrganizationRequest` patterns already present in `controller/organization_test.go`. Include at least:

```go
func TestOrganizationMemberCannotCreateWalletAutoRecharge(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-wallet-auto"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 2, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(RequestOrganizationWalletScheduledRecharge, member, `{"amount":10,"interval_unit":"month","interval_value":1,"charge_immediately":false}`)

	requireOrganizationApiError(t, res, "organization owner permission required")
}
```

- [ ] **Step 2: Run controller tests and confirm failure**

Run:

```bash
go test ./controller -run WalletAutoRecharge -count=1
```

Expected: FAIL because handlers do not exist.

- [ ] **Step 3: Add request/response structs and target resolver**

In `controller/wallet_auto_recharge.go`, define:

```go
type walletAutoRechargeRequest struct {
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	IntervalUnit      string  `json:"interval_unit"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
}

type walletAutoRechargeTossResponse struct {
	ClientKey   string `json:"client_key"`
	CustomerKey string `json:"customer_key"`
	TradeNo     string `json:"trade_no"`
	SuccessURL  string `json:"success_url"`
	FailURL     string `json:"fail_url"`
}

type walletRechargeTarget struct {
	TargetType  string
	TargetId    int
	OwnerUserId int
}
```

Add helpers:

- `resolveUserWalletTarget(c)`: current user is owner and target.
- `resolveOrganizationWalletTarget(c)`: current user must have `OrganizationRoleOwner`, target id is `user.OrganizationId`, owner id is current user id.
- `requestWalletAutoRecharge(c, policyType string, target walletRechargeTarget)`: shared create flow.

- [ ] **Step 4: Add handler behavior**

Implement handler flow:

1. `requirePaymentCompliance(c)` must pass.
2. `isTossBillingEnabled()` must pass.
3. Validate amount and interval/threshold through model request validation.
4. Get owner Toss customer key with `model.GetOrCreateTossCustomerKey(target.OwnerUserId)`.
5. Create `AuthTradeNo` like `wallet_auto_auth_<owner>_<unixMilli>_<rand>`.
6. Call `model.CreatePendingWalletAutoRecharge`.
7. Return `client_key`, `customer_key`, `trade_no`, `success_url`, `fail_url`.

Use `system_setting.ServerAddress` and the callback routes:

```go
successURL := strings.TrimRight(system_setting.ServerAddress, "/") + "/api/wallet/auto-recharge/toss/confirm?trade_no=" + reference
failURL := strings.TrimRight(system_setting.ServerAddress, "/") + "/api/wallet/auto-recharge/toss/fail?trade_no=" + reference
```

- [ ] **Step 5: Add Toss confirm/fail**

`WalletAutoRechargeTossConfirm` must:

1. Read `trade_no`, `authKey`, `customerKey`.
2. Load pending policy by `AuthTradeNo`.
3. Compare callback `customerKey` with stored `CustomerKey`.
4. Call `issueTossBillingKey`.
5. Store billing key for `OwnerUserId`.
6. Activate policy through model.
7. Redirect to `/wallet?wallet_auto_recharge=success` for user policies and `/organization/wallet?wallet_auto_recharge=success` for organization policies.

`WalletAutoRechargeTossFail` must mark the pending policy cancelled and redirect with `wallet_auto_recharge=failed`.

- [ ] **Step 6: Wire routes**

Modify `router/api-router.go`:

```go
apiRouter.GET("/wallet/auto-recharge/toss/confirm", controller.WalletAutoRechargeTossConfirm)
apiRouter.GET("/wallet/auto-recharge/toss/fail", controller.WalletAutoRechargeTossFail)
```

Inside `selfRoute`:

```go
selfRoute.GET("/wallet/auto-recharge", controller.GetWalletAutoRecharge)
selfRoute.POST("/wallet/auto-recharge/scheduled", middleware.CriticalRateLimit(), controller.RequestWalletScheduledRecharge)
selfRoute.POST("/wallet/auto-recharge/threshold", middleware.CriticalRateLimit(), controller.RequestWalletThresholdRecharge)
selfRoute.DELETE("/wallet/auto-recharge/:id", controller.CancelWalletAutoRecharge)
```

Inside `organizationRoute`:

```go
organizationRoute.GET("/wallet/auto-recharge", controller.GetOrganizationWalletAutoRecharge)
organizationRoute.POST("/wallet/auto-recharge/scheduled", middleware.CriticalRateLimit(), controller.RequestOrganizationWalletScheduledRecharge)
organizationRoute.POST("/wallet/auto-recharge/threshold", middleware.CriticalRateLimit(), controller.RequestOrganizationWalletThresholdRecharge)
organizationRoute.DELETE("/wallet/auto-recharge/:id", controller.CancelOrganizationWalletAutoRecharge)
```

- [ ] **Step 7: Run controller tests**

Run:

```bash
gofmt -w controller/wallet_auto_recharge.go controller/wallet_auto_recharge_test.go router/api-router.go
go test ./controller -run WalletAutoRecharge -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add controller/wallet_auto_recharge.go controller/wallet_auto_recharge_test.go router/api-router.go
git commit -m "feat: add wallet auto recharge api"
```

---

### Task 4: 개인 지갑 프론트 API, 훅, 카드

**Files:**
- Modify: `web/default/src/features/wallet/types.ts`
- Modify: `web/default/src/features/wallet/api.ts`
- Create: `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`
- Create: `web/default/src/features/wallet/components/auto-recharge-card.tsx`

**Interfaces:**
- Consumes:
  - `@tosspayments/tosspayments-sdk`
  - `api` from `@/lib/api`
  - `Tabs`, `Button`, `Input`, `Switch`, existing wallet styles.
- Produces:
  - `type WalletAutoRechargePolicy`
  - `function getWalletAutoRecharge(scope?: 'user' | 'organization')`
  - `function requestWalletScheduledRecharge(payload, scope?)`
  - `function requestWalletThresholdRecharge(payload, scope?)`
  - `function cancelWalletAutoRecharge(id, scope?)`
  - `function useWalletAutoRecharge(scope: 'user' | 'organization', canManage: boolean)`
  - `function AutoRechargeCard(props)`

- [ ] **Step 1: Add frontend types**

Append to `web/default/src/features/wallet/types.ts`:

```ts
export type WalletAutoRechargeType = 'scheduled' | 'threshold'
export type WalletAutoRechargeStatus = 'pending' | 'active' | 'cancelled' | 'failed'

export interface WalletAutoRechargePolicy {
  id: number
  type: WalletAutoRechargeType
  target_type: 'user' | 'organization'
  target_id: number
  amount: number
  threshold_amount?: number
  threshold_quota?: number
  interval_unit?: 'month' | 'day' | 'custom'
  interval_value?: number
  custom_seconds?: number
  charge_immediately?: boolean
  next_charge_time?: number
  last_charge_time?: number
  cooldown_until?: number
  daily_charge_count?: number
  status: WalletAutoRechargeStatus
  fail_count?: number
  last_error?: string
  card_company?: string
  card_number_masked?: string
}

export interface WalletAutoRechargeRequest {
  amount: number
  threshold_amount?: number
  interval_unit?: 'month' | 'day' | 'custom'
  interval_value?: number
  custom_seconds?: number
  charge_immediately?: boolean
}

export type WalletAutoRechargeResponse = ApiResponse<WalletAutoRechargePolicy[]>
export type WalletAutoRechargeTossResponse = ApiResponse<{
  client_key: string
  customer_key: string
  trade_no: string
  success_url: string
  fail_url: string
}>
```

- [ ] **Step 2: Add frontend API functions**

Append to `web/default/src/features/wallet/api.ts`:

```ts
function walletAutoRechargeBase(scope: 'user' | 'organization') {
  return scope === 'organization'
    ? '/api/organization/wallet/auto-recharge'
    : '/api/user/wallet/auto-recharge'
}

export async function getWalletAutoRecharge(
  scope: 'user' | 'organization' = 'user'
): Promise<WalletAutoRechargeResponse> {
  const res = await api.get(walletAutoRechargeBase(scope))
  return res.data
}

export async function requestWalletScheduledRecharge(
  request: WalletAutoRechargeRequest,
  scope: 'user' | 'organization' = 'user'
): Promise<WalletAutoRechargeTossResponse> {
  const res = await api.post(`${walletAutoRechargeBase(scope)}/scheduled`, request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestWalletThresholdRecharge(
  request: WalletAutoRechargeRequest,
  scope: 'user' | 'organization' = 'user'
): Promise<WalletAutoRechargeTossResponse> {
  const res = await api.post(`${walletAutoRechargeBase(scope)}/threshold`, request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function cancelWalletAutoRecharge(
  id: number,
  scope: 'user' | 'organization' = 'user'
): Promise<ApiResponse> {
  const res = await api.delete(`${walletAutoRechargeBase(scope)}/${id}`)
  return res.data
}
```

Also import the new response/request types from `./types`.

- [ ] **Step 3: Add Toss billing auth hook**

Create `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { loadTossPayments } from '@tosspayments/tosspayments-sdk'
import {
  cancelWalletAutoRecharge,
  getWalletAutoRecharge,
  requestWalletScheduledRecharge,
  requestWalletThresholdRecharge,
} from '../api'
import type { WalletAutoRechargePolicy, WalletAutoRechargeRequest } from '../types'

export function useWalletAutoRecharge(
  scope: 'user' | 'organization',
  canManage: boolean
) {
  const [policies, setPolicies] = useState<WalletAutoRechargePolicy[]>([])
  const [loading, setLoading] = useState(false)
  const [processing, setProcessing] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await getWalletAutoRecharge(scope)
      if (resp.success !== false && resp.data) setPolicies(resp.data)
    } finally {
      setLoading(false)
    }
  }, [scope])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const startBillingAuth = useCallback(async (resp: Awaited<ReturnType<typeof requestWalletScheduledRecharge>>) => {
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
  }, [])

  const createScheduled = useCallback(async (payload: WalletAutoRechargeRequest) => {
    if (!canManage) return false
    setProcessing(true)
    try {
      return await startBillingAuth(await requestWalletScheduledRecharge(payload, scope))
    } finally {
      setProcessing(false)
    }
  }, [canManage, scope, startBillingAuth])

  const createThreshold = useCallback(async (payload: WalletAutoRechargeRequest) => {
    if (!canManage) return false
    setProcessing(true)
    try {
      return await startBillingAuth(await requestWalletThresholdRecharge(payload, scope))
    } finally {
      setProcessing(false)
    }
  }, [canManage, scope, startBillingAuth])

  const cancel = useCallback(async (id: number) => {
    if (!canManage) return false
    setProcessing(true)
    try {
      const resp = await cancelWalletAutoRecharge(id, scope)
      const ok = resp.success === true || resp.message === 'success'
      if (ok) await refresh()
      return ok
    } finally {
      setProcessing(false)
    }
  }, [canManage, refresh, scope])

  return { policies, loading, processing, refresh, createScheduled, createThreshold, cancel }
}
```

- [ ] **Step 4: Add card component**

Create `web/default/src/features/wallet/components/auto-recharge-card.tsx` with props:

```ts
interface AutoRechargeCardProps {
  mode: 'scheduled' | 'threshold'
  policies: WalletAutoRechargePolicy[]
  loading: boolean
  processing: boolean
  canManage: boolean
  minTopup: number
  onCreateScheduled: (payload: WalletAutoRechargeRequest) => Promise<boolean>
  onCreateThreshold: (payload: WalletAutoRechargeRequest) => Promise<boolean>
  onCancel: (id: number) => Promise<boolean>
}
```

The component must render:

- Current active policy summary when present.
- Amount input.
- For scheduled mode: interval select (`month`, `day`), interval value input, `Switch` for immediate first charge.
- For threshold mode: threshold amount input and recharge amount input.
- Disabled form and cancel button when `canManage` is false.
- Submit button that calls the matching create function.

Use visible labels through `t('정기결제')`, `t('자동결제')`, `t('충전 금액')`, `t('기준 잔액')`, `t('즉시 첫 충전')`, `t('해지')`.

- [ ] **Step 5: Typecheck frontend slices**

Run:

```bash
cd web/default
bun run typecheck
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/wallet/types.ts web/default/src/features/wallet/api.ts web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts web/default/src/features/wallet/components/auto-recharge-card.tsx
git commit -m "feat: add wallet auto recharge frontend primitives"
```

---

### Task 5: 개인/조직 지갑 탭 통합과 i18n

**Files:**
- Modify: `web/default/src/features/wallet/index.tsx`
- Modify: `web/default/src/features/organizations/components/organization-wallet.tsx`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`

**Interfaces:**
- Consumes:
  - Task 4 `AutoRechargeCard`
  - Task 4 `useWalletAutoRecharge`
  - Existing `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`
- Produces:
  - Personal wallet tabs: `충전`, `정기결제`, `자동결제`
  - Organization wallet tabs with owner-only management.

- [ ] **Step 1: Integrate personal wallet tabs**

In `web/default/src/features/wallet/index.tsx`, import:

```ts
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AutoRechargeCard } from './components/auto-recharge-card'
import { useWalletAutoRecharge } from './hooks/use-wallet-auto-recharge'
```

Inside `Wallet`, initialize:

```ts
const walletAutoRecharge = useWalletAutoRecharge('user', true)
```

Wrap the existing recharge/subscription grid and affiliate card in `TabsContent value='recharge'`. Add:

```tsx
<Tabs defaultValue='recharge' className='w-full'>
  <TabsList className='mb-2'>
    <TabsTrigger value='recharge'>{t('충전')}</TabsTrigger>
    <TabsTrigger value='scheduled'>{t('정기결제')}</TabsTrigger>
    <TabsTrigger value='threshold'>{t('자동결제')}</TabsTrigger>
  </TabsList>
  <TabsContent value='recharge'>{/* existing wallet content */}</TabsContent>
  <TabsContent value='scheduled'>
    <AutoRechargeCard
      mode='scheduled'
      policies={walletAutoRecharge.policies}
      loading={walletAutoRecharge.loading}
      processing={walletAutoRecharge.processing}
      canManage
      minTopup={topupInfo?.toss_min_topup || getMinTopupAmount(topupInfo)}
      onCreateScheduled={walletAutoRecharge.createScheduled}
      onCreateThreshold={walletAutoRecharge.createThreshold}
      onCancel={walletAutoRecharge.cancel}
    />
  </TabsContent>
  <TabsContent value='threshold'>
    <AutoRechargeCard
      mode='threshold'
      policies={walletAutoRecharge.policies}
      loading={walletAutoRecharge.loading}
      processing={walletAutoRecharge.processing}
      canManage
      minTopup={topupInfo?.toss_min_topup || getMinTopupAmount(topupInfo)}
      onCreateScheduled={walletAutoRecharge.createScheduled}
      onCreateThreshold={walletAutoRecharge.createThreshold}
      onCancel={walletAutoRecharge.cancel}
    />
  </TabsContent>
</Tabs>
```

- [ ] **Step 2: Integrate organization wallet tabs**

In `organization-wallet.tsx`, import the same Tabs, `AutoRechargeCard`, and `useWalletAutoRecharge`.

Compute owner management:

```ts
const canManageAutoRecharge = organization?.organization_role === 'owner'
```

Use the current auth user from `useAuthStore` and compare `organization.owner_user_id === authUser.id` because `Organization` already exposes `owner_user_id`.

Initialize:

```ts
const walletAutoRecharge = useWalletAutoRecharge('organization', !!canManageAutoRecharge)
```

Render the same three tabs. In the organization `정기결제` and `자동결제` tabs, pass `canManage={!!canManageAutoRecharge}`.

- [ ] **Step 3: Keep organization API/types unchanged**

Use Task 4's `scope='organization'` API functions directly from the organization wallet component. Do not add aliases to `web/default/src/features/organizations/api.ts`, and do not change `web/default/src/features/organizations/types.ts`.

- [ ] **Step 4: Add i18n keys**

Add the following English-source keys to all six locale files:

```json
{
  "충전": "Top up",
  "정기결제": "Scheduled payment",
  "자동결제": "Auto payment",
  "충전 금액": "Top-up amount",
  "기준 잔액": "Balance threshold",
  "즉시 첫 충전": "Charge immediately",
  "해지": "Cancel",
  "정해진 주기마다 지갑에 자동 충전": "Automatically top up the wallet on a schedule",
  "잔액이 기준 이하일 때 지갑에 자동 충전": "Automatically top up the wallet when the balance falls below the threshold",
  "조직 소유자만 자동결제를 변경할 수 있습니다": "Only the organization owner can change auto payments"
}
```

For non-English locale files, translate the values while preserving the exact Korean keys above.

- [ ] **Step 5: Run frontend validation**

Run:

```bash
cd web/default
bun run i18n:sync
bun run typecheck
bun run lint
```

Expected: all commands exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/wallet/index.tsx web/default/src/features/organizations/components/organization-wallet.tsx web/default/src/i18n/locales/en.json web/default/src/i18n/locales/zh.json web/default/src/i18n/locales/fr.json web/default/src/i18n/locales/ja.json web/default/src/i18n/locales/ru.json web/default/src/i18n/locales/vi.json
git commit -m "feat: add wallet auto recharge tabs"
```

---

### Task 6: 전체 검증과 서비스 재기동

**Files:**
- Modify only files changed by Tasks 1-5 when verification exposes a defect.

**Interfaces:**
- Consumes all previous tasks.
- Produces verified backend binary and refreshed `newapi` service.

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
go test ./model -run WalletAutoRecharge -count=1
go test ./controller -run WalletAutoRecharge -count=1
go test ./service -run WalletAutoRecharge -count=1
```

Expected: PASS for all three.

- [ ] **Step 2: Run payment regression tests**

Run:

```bash
go test ./model -run "Toss|TopUp|PaymentMethod|Organization" -count=1
go test ./controller -run "Toss|TopUp|OrganizationWallet|Organization.*Wallet|SubscriptionToss" -count=1
```

Expected: PASS. Existing Toss top-up and Toss subscription behavior must remain unchanged.

- [ ] **Step 3: Run frontend validation**

Run:

```bash
cd web/default
bun run typecheck
bun run lint
bun run build
```

Expected: PASS.

- [ ] **Step 4: Build backend**

Run from repository root:

```bash
go build -o new-api-bin .
```

Expected: command exits 0 and updates `new-api-bin`.

- [ ] **Step 5: Restart service**

Use the same deployment pattern currently used for the `newapi` service in this environment. Before replacing a running binary, preserve the previous binary with a timestamped backup if that is the established local practice.

Run:

```bash
sudo systemctl restart newapi
sudo systemctl status newapi --no-pager
```

Expected: status shows `active (running)`.

- [ ] **Step 6: Commit final verification note if source changed**

If Step 1-3 revealed fixes, commit the fixes:

```bash
git add model/wallet_auto_recharge.go controller/wallet_auto_recharge.go service/wallet_auto_recharge_task.go web/default/src/features/wallet/components/auto-recharge-card.tsx
git commit -m "fix: stabilize wallet auto recharge"
```

If no files changed after Task 5, do not create an empty commit.

---

## Self-Review

- Spec coverage: 개인/조직 지갑, 정기결제, 자동결제, Toss-only, 일반 `TopUp` 연동, 조직 소유자 권한, 즉시 첫 충전 옵션, threshold quota 변환, cooldown/일일 제한, 기존 구독 분리를 Task 1-5에서 모두 다룬다.
- DB compatibility: 모델과 조회는 GORM을 사용하며 raw SQL/부분 인덱스를 요구하지 않는다. row lock은 기존 코드처럼 `gorm:query_option`을 쓰는 형태로 제한한다.
- JSON rule: 새 Go 코드 계획에 `encoding/json` marshal/unmarshal 호출이 없다.
- Frontend: Base UI Tabs, 기존 wallet feature 구조, Bun 명령, 모든 locale 동기화를 포함한다.
- Verification: focused tests, payment regression, frontend build, backend build, service restart를 포함한다.
