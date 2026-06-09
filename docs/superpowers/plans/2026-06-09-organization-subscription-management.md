# Organization Subscription Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 조직 소유자/관리자가 조직 전용 subscription plan을 만들고 조직 사용자에게 배정하며, 조직 사용 시 사용자별 한도는 quota 또는 조직 subscription 중 하나로 관리되게 한다.

**Architecture:** 전역 subscription과 분리된 `OrganizationSubscriptionPlan` / `OrganizationUserSubscription` 모델을 추가한다. 과금은 조직 사용자에게 활성 조직 subscription이 있으면 subscription 한도와 조직 quota를 함께 차감하고, 없으면 기존 조직 사용자 quota 방식을 유지한다. UI는 기존 조직 메뉴 아래 `조직 Subscription` 화면을 추가하고 일반 조직 사용자는 지갑 요약에서 배정 상태만 확인한다.

**Tech Stack:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL 호환, React 19, TypeScript, Rsbuild, Bun, i18next.

---

## File Structure

Backend:

- Create: `model/organization_subscription.go`
  - 조직 전용 plan과 사용자 subscription 모델, CRUD, 배정, 취소, pre-consume, settle, refund를 담당한다.
- Modify: `model/main.go`
  - `OrganizationSubscriptionPlan`, `OrganizationUserSubscription`, pre-consume record 모델을 AutoMigrate에 추가한다.
- Modify: `model/subscription.go`
  - 기간 계산과 reset 계산을 조직 subscription에서도 쓸 수 있도록 공통 helper를 노출한다.
- Create: `model/organization_subscription_test.go`
  - 모델과 과금 원자성 테스트.
- Create: `controller/organization_subscription.go`
  - 조직 subscription API handler.
- Modify: `router/api-router.go`
  - `/api/organization/subscription/*` route 추가.
- Create: `controller/organization_subscription_test.go`
  - 권한과 조직 범위 테스트.
- Modify: `service/funding_source.go`
  - `OrganizationSubscriptionFunding` 추가.
- Modify: `service/billing_session.go`
  - 조직 사용자 과금 분기에서 활성 조직 subscription 우선 적용.
- Modify: `service/log_info_generate.go`
  - 로그 `other`에 조직 subscription 정보 추가.
- Create: `service/organization_subscription_billing_test.go`
  - 조직 subscription 과금 흐름 테스트.

Frontend:

- Create: `web/default/src/features/organizations/subscriptions/types.ts`
  - 조직 subscription API 타입.
- Create: `web/default/src/features/organizations/subscriptions/api.ts`
  - 조직 subscription API client.
- Create: `web/default/src/features/organizations/subscriptions/organization-subscriptions-page.tsx`
  - 조직 subscription 관리 화면.
- Create: `web/default/src/routes/_authenticated/organization/subscriptions.tsx`
  - route guard와 페이지 연결.
- Modify: `web/default/src/hooks/use-sidebar-data.ts`
  - 조직 메뉴에 `조직 Subscription` 추가.
- Modify: `web/default/src/features/organizations/components/organization-member-wallet-summary.tsx`
  - 일반 조직 사용자에게 배정된 subscription 요약 표시.
- Modify: `web/default/src/i18n/static-keys.ts`
  - 새 UI 문구 key 추가.
- Modify: `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`
  - 번역 추가.

---

### Task 1: Backend Model And Migration

**Files:**
- Create: `model/organization_subscription.go`
- Modify: `model/main.go`
- Modify: `model/subscription.go`
- Test: `model/organization_subscription_test.go`

- [ ] **Step 1: Write failing model tests**

Create `model/organization_subscription_test.go` with tests for plan creation, assignment, replacement, and cross-organization rejection.

```go
package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupOrganizationSubscriptionTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.UsingSQLite = true
	require.NoError(t, DB.AutoMigrate(
		&User{},
		&Organization{},
		&OrganizationSubscriptionPlan{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPreConsumeRecord{},
	))
}

func TestCreateOrganizationSubscriptionFromPlanCancelsExistingActiveSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleOwner}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleMember}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 100000, Status: OrganizationStatusEnabled}).Error)

	planA := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Small", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	planB := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Large", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 5000, Enabled: true}
	require.NoError(t, DB.Create(planA).Error)
	require.NoError(t, DB.Create(planB).Error)

	first, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, planA.Id, 1)
	require.NoError(t, err)
	require.Equal(t, "active", first.Status)

	second, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, planB.Id, 1)
	require.NoError(t, err)
	require.Equal(t, planB.Id, second.PlanId)

	var old OrganizationUserSubscription
	require.NoError(t, DB.First(&old, first.Id).Error)
	require.Equal(t, "cancelled", old.Status)
}

func TestCreateOrganizationUserSubscriptionRejectsOutsideOrganizationUser(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: OrganizationRoleMember}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 100000, Status: OrganizationStatusEnabled}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Small", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)

	_, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.ErrorContains(t, err, "target user is outside organization")
}

func TestPreConsumeOrganizationUserSubscriptionResetsPeriodicQuota(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Daily", DurationUnit: SubscriptionDurationDay, DurationValue: 7, TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	sub := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: 1, EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active", LastResetTime: 1, NextResetTime: 2}
	require.NoError(t, DB.Create(sub).Error)

	res, err := PreConsumeOrganizationUserSubscription("req-1", 1, 2, "gpt-test", 100)
	require.NoError(t, err)
	require.Equal(t, sub.Id, res.OrganizationUserSubscriptionId)
	require.Equal(t, int64(100), res.AmountUsedAfter)
}
```

- [ ] **Step 2: Run model tests and verify they fail**

Run:

```bash
go test ./model -run 'TestCreateOrganizationSubscription|TestPreConsumeOrganizationUserSubscription' -count=1
```

Expected: compile fails because `OrganizationSubscriptionPlan`, `OrganizationUserSubscription`, and helper functions do not exist.

- [ ] **Step 3: Expose shared subscription time helpers**

Modify `model/subscription.go` by adding exported wrappers near `calcPlanEndTime` and `calcNextResetTime`:

```go
func CalcSubscriptionPlanEndTime(start time.Time, durationUnit string, durationValue int, customSeconds int64) (int64, error) {
	plan := &SubscriptionPlan{
		DurationUnit:  durationUnit,
		DurationValue: durationValue,
		CustomSeconds: customSeconds,
	}
	return calcPlanEndTime(start, plan)
}

func CalcSubscriptionNextResetTime(base time.Time, resetPeriod string, resetCustomSeconds int64, endUnix int64) int64 {
	plan := &SubscriptionPlan{
		QuotaResetPeriod:        resetPeriod,
		QuotaResetCustomSeconds: resetCustomSeconds,
	}
	return calcNextResetTime(base, plan, endUnix)
}
```

- [ ] **Step 4: Implement organization subscription models and helpers**

Create `model/organization_subscription.go`:

```go
package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type OrganizationSubscriptionPlan struct {
	Id int `json:"id"`

	OrganizationId int    `json:"organization_id" gorm:"index;not null"`
	Title          string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle       string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	TotalAmount  int64  `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *OrganizationSubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *OrganizationSubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

type OrganizationUserSubscription struct {
	Id             int `json:"id"`
	OrganizationId int `json:"organization_id" gorm:"index;index:idx_org_user_sub_active,priority:1"`
	UserId         int `json:"user_id" gorm:"index;index:idx_org_user_sub_active,priority:2"`
	PlanId         int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_org_user_sub_active,priority:4"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_org_user_sub_active,priority:3"`

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`
	AssignedByUserId int `json:"assigned_by_user_id" gorm:"index;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *OrganizationUserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *OrganizationUserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type OrganizationSubscriptionSummary struct {
	Subscription *OrganizationUserSubscription `json:"subscription"`
	Plan         *OrganizationSubscriptionPlan `json:"plan,omitempty"`
	User         *User                         `json:"user,omitempty"`
}

type OrganizationSubscriptionPreConsumeRecord struct {
	Id                             int    `json:"id"`
	RequestId                      string `json:"request_id" gorm:"uniqueIndex;type:varchar(191)"`
	OrganizationId                 int    `json:"organization_id" gorm:"index"`
	UserId                         int    `json:"user_id" gorm:"index"`
	OrganizationUserSubscriptionId int    `json:"organization_user_subscription_id" gorm:"index"`
	PreConsumed                    int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	CreatedAt                      int64  `json:"created_at" gorm:"bigint"`
}

func (r *OrganizationSubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	r.CreatedAt = common.GetTimestamp()
	return nil
}

type OrganizationSubscriptionPreConsumeResult struct {
	OrganizationUserSubscriptionId int
	PreConsumed                    int64
	AmountTotal                    int64
	AmountUsedAfter                int64
	PlanId                         int
	PlanTitle                      string
}

func GetOrganizationSubscriptionPlanById(organizationId int, planId int) (*OrganizationSubscriptionPlan, error) {
	if organizationId <= 0 || planId <= 0 {
		return nil, errors.New("invalid organization or plan id")
	}
	var plan OrganizationSubscriptionPlan
	if err := DB.Where("id = ? AND organization_id = ?", planId, organizationId).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func CreateOrganizationUserSubscriptionFromPlan(organizationId int, userId int, planId int, assignedBy int) (*OrganizationUserSubscription, error) {
	if organizationId <= 0 || userId <= 0 || planId <= 0 {
		return nil, errors.New("invalid organization subscription assignment")
	}
	return createOrganizationUserSubscriptionFromPlanTx(DB, organizationId, userId, planId, assignedBy)
}

func createOrganizationUserSubscriptionFromPlanTx(tx *gorm.DB, organizationId int, userId int, planId int, assignedBy int) (*OrganizationUserSubscription, error) {
	var target User
	if err := tx.Where("id = ?", userId).First(&target).Error; err != nil {
		return nil, err
	}
	if target.OrganizationId != organizationId {
		return nil, errors.New("target user is outside organization")
	}

	var plan OrganizationSubscriptionPlan
	if err := tx.Where("id = ? AND organization_id = ?", planId, organizationId).First(&plan).Error; err != nil {
		return nil, err
	}
	if !plan.Enabled {
		return nil, errors.New("organization subscription plan is disabled")
	}

	nowUnix := GetDBTimestamp()
	now := time.Unix(nowUnix, 0)
	endUnix, err := CalcSubscriptionPlanEndTime(now, plan.DurationUnit, plan.DurationValue, plan.CustomSeconds)
	if err != nil {
		return nil, err
	}
	nextReset := CalcSubscriptionNextResetTime(now, NormalizeResetPeriod(plan.QuotaResetPeriod), plan.QuotaResetCustomSeconds, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = nowUnix
	}

	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := getUserGroupByIdTx(tx, userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := tx.Model(&User{}).Where("id = ?", userId).Update("group", upgradeGroup).Error; err != nil {
				return nil, err
			}
		}
	}

	var created *OrganizationUserSubscription
	err = tx.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&OrganizationUserSubscription{}).
			Where("organization_id = ? AND user_id = ? AND status = ? AND end_time > ?", organizationId, userId, "active", nowUnix).
			Updates(map[string]interface{}{"status": "cancelled", "end_time": nowUnix}).Error; err != nil {
			return err
		}
		sub := &OrganizationUserSubscription{
			OrganizationId:   organizationId,
			UserId:           userId,
			PlanId:           plan.Id,
			AmountTotal:      plan.TotalAmount,
			AmountUsed:       0,
			StartTime:        nowUnix,
			EndTime:          endUnix,
			Status:           "active",
			LastResetTime:    lastReset,
			NextResetTime:    nextReset,
			UpgradeGroup:     upgradeGroup,
			PrevUserGroup:    prevGroup,
			AssignedByUserId: assignedBy,
		}
		if err := tx.Create(sub).Error; err != nil {
			return err
		}
		created = sub
		return nil
	})
	return created, err
}

func HasActiveOrganizationUserSubscription(organizationId int, userId int) (bool, error) {
	now := GetDBTimestamp()
	var count int64
	err := DB.Model(&OrganizationUserSubscription{}).
		Where("organization_id = ? AND user_id = ? AND status = ? AND end_time > ?", organizationId, userId, "active", now).
		Count(&count).Error
	return count > 0, err
}

func PreConsumeOrganizationUserSubscription(requestId string, organizationId int, userId int, modelName string, amount int64) (*OrganizationSubscriptionPreConsumeResult, error) {
	if requestId == "" || organizationId <= 0 || userId <= 0 || amount <= 0 {
		return nil, errors.New("invalid organization subscription pre-consume")
	}
	var result OrganizationSubscriptionPreConsumeResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing OrganizationSubscriptionPreConsumeRecord
		if err := tx.Where("request_id = ?", requestId).First(&existing).Error; err == nil {
			var sub OrganizationUserSubscription
			if err := tx.First(&sub, existing.OrganizationUserSubscriptionId).Error; err != nil {
				return err
			}
			var plan OrganizationSubscriptionPlan
			_ = tx.First(&plan, sub.PlanId).Error
			result = OrganizationSubscriptionPreConsumeResult{OrganizationUserSubscriptionId: sub.Id, PreConsumed: existing.PreConsumed, AmountTotal: sub.AmountTotal, AmountUsedAfter: sub.AmountUsed, PlanId: sub.PlanId, PlanTitle: plan.Title}
			return nil
		}
		now := GetDBTimestamp()
		var sub OrganizationUserSubscription
		if err := tx.Where("organization_id = ? AND user_id = ? AND status = ? AND end_time > ?", organizationId, userId, "active", now).
			Order("end_time asc, id asc").First(&sub).Error; err != nil {
			return fmt.Errorf("no active organization subscription: %w", err)
		}
		var plan OrganizationSubscriptionPlan
		if err := tx.First(&plan, sub.PlanId).Error; err != nil {
			return err
		}
		if err := maybeResetOrganizationSubscriptionWithPlanTx(tx, &sub, &plan, now); err != nil {
			return err
		}
		if sub.AmountTotal > 0 && sub.AmountUsed+amount > sub.AmountTotal {
			return errors.New("organization subscription quota insufficient")
		}
		if err := tx.Model(&OrganizationUserSubscription{}).Where("id = ?", sub.Id).
			Update("amount_used", gorm.Expr("amount_used + ?", amount)).Error; err != nil {
			return err
		}
		record := &OrganizationSubscriptionPreConsumeRecord{RequestId: requestId, OrganizationId: organizationId, UserId: userId, OrganizationUserSubscriptionId: sub.Id, PreConsumed: amount}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		result = OrganizationSubscriptionPreConsumeResult{OrganizationUserSubscriptionId: sub.Id, PreConsumed: amount, AmountTotal: sub.AmountTotal, AmountUsedAfter: sub.AmountUsed + amount, PlanId: plan.Id, PlanTitle: plan.Title}
		return nil
	})
	return &result, err
}

func maybeResetOrganizationSubscriptionWithPlanTx(tx *gorm.DB, sub *OrganizationUserSubscription, plan *OrganizationSubscriptionPlan, now int64) error {
	if sub.NextResetTime <= 0 || now < sub.NextResetTime {
		return nil
	}
	base := time.Unix(now, 0)
	next := CalcSubscriptionNextResetTime(base, NormalizeResetPeriod(plan.QuotaResetPeriod), plan.QuotaResetCustomSeconds, sub.EndTime)
	return tx.Model(&OrganizationUserSubscription{}).Where("id = ?", sub.Id).
		Updates(map[string]interface{}{"amount_used": 0, "last_reset_time": now, "next_reset_time": next}).Error
}

func PostConsumeOrganizationUserSubscriptionDelta(subscriptionId int, delta int64) error {
	if subscriptionId <= 0 || delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var sub OrganizationUserSubscription
		if err := tx.First(&sub, subscriptionId).Error; err != nil {
			return err
		}
		nextUsed := sub.AmountUsed + delta
		if nextUsed < 0 {
			nextUsed = 0
		}
		return tx.Model(&OrganizationUserSubscription{}).Where("id = ?", subscriptionId).
			Update("amount_used", nextUsed).Error
	})
}

func RefundOrganizationSubscriptionPreConsume(requestId string) error {
	if requestId == "" {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record OrganizationSubscriptionPreConsumeRecord
		if err := tx.Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return nil
		}
		var sub OrganizationUserSubscription
		if err := tx.First(&sub, record.OrganizationUserSubscriptionId).Error; err != nil {
			return err
		}
		nextUsed := sub.AmountUsed - record.PreConsumed
		if nextUsed < 0 {
			nextUsed = 0
		}
		if err := tx.Model(&OrganizationUserSubscription{}).Where("id = ?", sub.Id).
			Update("amount_used", nextUsed).Error; err != nil {
			return err
		}
		return tx.Delete(&record).Error
	})
}
```

- [ ] **Step 5: Register migrations**

Modify `model/main.go` AutoMigrate sections so `OrganizationSubscriptionPlan`, `OrganizationUserSubscription`, and `OrganizationSubscriptionPreConsumeRecord` are migrated with other models:

```go
&OrganizationSubscriptionPlan{},
&OrganizationUserSubscription{},
&OrganizationSubscriptionPreConsumeRecord{},
```

- [ ] **Step 6: Run model tests**

Run:

```bash
go test ./model -run 'TestCreateOrganizationSubscription|TestPreConsumeOrganizationUserSubscription' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add model/organization_subscription.go model/organization_subscription_test.go model/main.go model/subscription.go
git commit -m "feat: add organization subscription models"
```

---

### Task 2: Backend Billing Integration

**Files:**
- Modify: `service/funding_source.go`
- Modify: `service/billing_session.go`
- Modify: `service/log_info_generate.go`
- Test: `service/organization_subscription_billing_test.go`

- [ ] **Step 1: Write failing billing tests**

Create `service/organization_subscription_billing_test.go`:

```go
package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupOrganizationSubscriptionBillingDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.UsingSQLite = true
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Organization{}, &model.OrganizationSubscriptionPlan{}, &model.OrganizationUserSubscription{}, &model.OrganizationSubscriptionPreConsumeRecord{}))
}

func TestOrganizationSubscriptionBillingUsesPlanAndOrganizationQuota(t *testing.T) {
	setupOrganizationSubscriptionBillingDB(t)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "member", Quota: 10, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Status: common.UserStatusEnabled}).Error)
	plan := &model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Team Plan", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	require.NoError(t, model.DB.Create(plan).Error)
	_, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	c := &gin.Context{}
	relayInfo := &relaycommon.RelayInfo{UserId: 2, RequestId: "org-sub-1", OriginModelName: "gpt-test"}
	session, apiErr := NewBillingSession(c, relayInfo, 100)
	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, session.funding.Source())
	require.Equal(t, 100, relayInfo.FinalPreConsumedQuota)

	require.NoError(t, session.postConsume(c, 120))
	var sub model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&sub).Error)
	require.Equal(t, int64(120), sub.AmountUsed)
	orgQuota, err := model.GetOrganizationQuota(1)
	require.NoError(t, err)
	require.Equal(t, 880, orgQuota)
	userQuota, err := model.GetUserQuota(2, false)
	require.NoError(t, err)
	require.Equal(t, 10, userQuota)
}

func TestOrganizationSubscriptionInsufficientDoesNotFallbackToUserQuota(t *testing.T) {
	setupOrganizationSubscriptionBillingDB(t)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "member", Quota: 1000, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Status: common.UserStatusEnabled}).Error)
	plan := &model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Tiny", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 50, Enabled: true}
	require.NoError(t, model.DB.Create(plan).Error)
	_, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	c := &gin.Context{}
	relayInfo := &relaycommon.RelayInfo{UserId: 2, RequestId: "org-sub-2", OriginModelName: "gpt-test"}
	_, apiErr := NewBillingSession(c, relayInfo, 100)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "organization subscription")
}
```

- [ ] **Step 2: Run billing tests and verify they fail**

Run:

```bash
go test ./service -run 'TestOrganizationSubscription' -count=1
```

Expected: compile fails because `OrganizationSubscriptionFunding` and billing branch do not exist.

- [ ] **Step 3: Add OrganizationSubscriptionFunding**

Modify `service/funding_source.go` after `OrganizationWalletFunding`:

```go
type OrganizationSubscriptionFunding struct {
	requestId      string
	memberId       int
	organizationId int
	modelName      string
	amount         int64
	subscriptionId int
	preConsumed    int64
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (o *OrganizationSubscriptionFunding) Source() string { return BillingSourceSubscription }

func (o *OrganizationSubscriptionFunding) PreConsume(_ int) error {
	res, err := model.PreConsumeOrganizationUserSubscription(o.requestId, o.organizationId, o.memberId, o.modelName, o.amount)
	if err != nil {
		return err
	}
	o.subscriptionId = res.OrganizationUserSubscriptionId
	o.preConsumed = res.PreConsumed
	o.AmountTotal = res.AmountTotal
	o.AmountUsedAfter = res.AmountUsedAfter
	o.PlanId = res.PlanId
	o.PlanTitle = res.PlanTitle
	if err := model.DecreaseOrganizationQuota(o.organizationId, int(o.preConsumed)); err != nil {
		_ = model.RefundOrganizationSubscriptionPreConsume(o.requestId)
		return err
	}
	return nil
}

func (o *OrganizationSubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if err := model.PostConsumeOrganizationUserSubscriptionDelta(o.subscriptionId, int64(delta)); err != nil {
		return err
	}
	if delta > 0 {
		return model.DecreaseOrganizationQuota(o.organizationId, delta)
	}
	return model.IncreaseOrganizationQuota(o.organizationId, -delta)
}

func (o *OrganizationSubscriptionFunding) Refund() error {
	if o.preConsumed <= 0 {
		return nil
	}
	if err := model.RefundOrganizationSubscriptionPreConsume(o.requestId); err != nil {
		return err
	}
	return model.IncreaseOrganizationQuota(o.organizationId, int(o.preConsumed))
}
```

- [ ] **Step 4: Branch organization billing session**

Modify `service/billing_session.go` in `tryWallet`, inside `if user.OrganizationId > 0`, before `funding = &OrganizationWalletFunding`:

```go
hasOrgSub, err := model.HasActiveOrganizationUserSubscription(user.OrganizationId, relayInfo.UserId)
if err != nil {
	return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
}
if hasOrgSub {
	subConsume := int64(preConsumedQuota)
	if subConsume <= 0 {
		subConsume = 1
	}
	funding = &OrganizationSubscriptionFunding{
		requestId:      relayInfo.RequestId,
		memberId:       relayInfo.UserId,
		organizationId: user.OrganizationId,
		modelName:      relayInfo.OriginModelName,
		amount:         subConsume,
	}
	session := &BillingSession{relayInfo: relayInfo, funding: funding}
	if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
		return nil, apiErr
	}
	return session, nil
}
```

- [ ] **Step 5: Sync RelayInfo organization subscription fields**

If `relay/common.RelayInfo` lacks organization subscription fields, add:

```go
OrganizationSubscriptionId int
OrganizationSubscriptionPlanId int
OrganizationSubscriptionPlanTitle string
OrganizationSubscriptionPreConsumed int64
OrganizationSubscriptionAmountTotal int64
OrganizationSubscriptionAmountUsedAfterPreConsume int64
```

Then in `BillingSession.preConsume`, when funding is `*OrganizationSubscriptionFunding`, set these fields and keep `BillingSource = BillingSourceSubscription`.

- [ ] **Step 6: Add log metadata**

Modify `service/log_info_generate.go` where subscription metadata is written:

```go
if relayInfo.OrganizationSubscriptionId != 0 {
	other["organization_subscription_id"] = relayInfo.OrganizationSubscriptionId
	other["organization_subscription_plan_id"] = relayInfo.OrganizationSubscriptionPlanId
	other["organization_subscription_plan_title"] = relayInfo.OrganizationSubscriptionPlanTitle
	other["organization_subscription_amount_total"] = relayInfo.OrganizationSubscriptionAmountTotal
	other["organization_subscription_amount_used_after_pre_consume"] = relayInfo.OrganizationSubscriptionAmountUsedAfterPreConsume
}
```

- [ ] **Step 7: Run billing tests**

Run:

```bash
go test ./service -run 'TestOrganizationSubscription' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add service/funding_source.go service/billing_session.go service/log_info_generate.go service/organization_subscription_billing_test.go relay/common
git commit -m "feat: bill organization subscriptions"
```

---

### Task 3: Organization Subscription API

**Files:**
- Create: `controller/organization_subscription.go`
- Modify: `router/api-router.go`
- Test: `controller/organization_subscription_test.go`

- [ ] **Step 1: Write failing controller tests**

Create `controller/organization_subscription_test.go` with these tests:

```go
func TestOrganizationOwnerCanCreateOrganizationSubscriptionPlan(t *testing.T)
func TestOrganizationAdminCanAssignOrganizationSubscriptionToMember(t *testing.T)
func TestOrganizationMemberCannotManageOrganizationSubscriptions(t *testing.T)
func TestOrganizationRootCanManageSelectedOrganizationSubscription(t *testing.T)
func TestOrganizationAdminCannotAssignPlanToOutsideOrganizationUser(t *testing.T)
```

Each test should follow `controller/organization_test.go` patterns: initialize in-memory DB, seed users/organizations, attach session values with `performOrganizationRequest`, and assert JSON contains expected plan/subscription IDs.

- [ ] **Step 2: Run controller tests and verify they fail**

Run:

```bash
go test ./controller -run 'TestOrganization.*Subscription' -count=1
```

Expected: compile fails because handlers do not exist.

- [ ] **Step 3: Implement target resolver**

Create `controller/organization_subscription.go` with:

```go
func resolveOrganizationSubscriptionTarget(c *gin.Context) (int, bool) {
	return resolveOrganizationAdminTarget(c)
}
```

This reuses root organization selection and organization owner/admin scoping.

- [ ] **Step 4: Implement plan handlers**

Add handlers:

```go
func ListOrganizationSubscriptionPlans(c *gin.Context)
func CreateOrganizationSubscriptionPlan(c *gin.Context)
func UpdateOrganizationSubscriptionPlan(c *gin.Context)
func UpdateOrganizationSubscriptionPlanStatus(c *gin.Context)
```

Validation:

- title must not be empty
- total_amount must be `>= 0`
- duration must match existing subscription duration rules
- reset period must use `model.NormalizeResetPeriod`
- plan `id` must belong to resolved organization

- [ ] **Step 5: Implement user subscription handlers**

Add handlers:

```go
func ListOrganizationUserSubscriptions(c *gin.Context)
func GetOrganizationUserSubscription(c *gin.Context)
func GetMyOrganizationSubscription(c *gin.Context)
func CreateOrganizationUserSubscription(c *gin.Context)
func InvalidateOrganizationUserSubscription(c *gin.Context)
func DeleteOrganizationUserSubscription(c *gin.Context)
```

Rules:

- target user must belong to resolved organization
- selected plan must belong to resolved organization
- invalidate/delete must target subscription in resolved organization
- create uses `model.CreateOrganizationUserSubscriptionFromPlan`

- [ ] **Step 6: Register routes**

Modify `router/api-router.go` under `organizationRoute`:

```go
organizationRoute.GET("/subscription/plans", controller.ListOrganizationSubscriptionPlans)
organizationRoute.POST("/subscription/plans", controller.CreateOrganizationSubscriptionPlan)
organizationRoute.PUT("/subscription/plans/:id", controller.UpdateOrganizationSubscriptionPlan)
organizationRoute.PATCH("/subscription/plans/:id/status", controller.UpdateOrganizationSubscriptionPlanStatus)
organizationRoute.GET("/subscription/users", controller.ListOrganizationUserSubscriptions)
organizationRoute.GET("/subscription/users/me", controller.GetMyOrganizationSubscription)
organizationRoute.GET("/subscription/users/:id", controller.GetOrganizationUserSubscription)
organizationRoute.POST("/subscription/users/:id/subscriptions", controller.CreateOrganizationUserSubscription)
organizationRoute.POST("/subscription/user_subscriptions/:id/invalidate", controller.InvalidateOrganizationUserSubscription)
organizationRoute.DELETE("/subscription/user_subscriptions/:id", controller.DeleteOrganizationUserSubscription)
```

- [ ] **Step 7: Run controller tests**

Run:

```bash
go test ./controller -run 'TestOrganization.*Subscription' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add controller/organization_subscription.go controller/organization_subscription_test.go router/api-router.go
git commit -m "feat: add organization subscription api"
```

---

### Task 4: Frontend Organization Subscription Management

**Files:**
- Create: `web/default/src/features/organizations/subscriptions/types.ts`
- Create: `web/default/src/features/organizations/subscriptions/api.ts`
- Create: `web/default/src/features/organizations/subscriptions/organization-subscriptions-page.tsx`
- Create: `web/default/src/routes/_authenticated/organization/subscriptions.tsx`
- Modify: `web/default/src/hooks/use-sidebar-data.ts`
- Modify: `web/default/src/routeTree.gen.ts` via route generation/build

- [ ] **Step 1: Add types**

Create `web/default/src/features/organizations/subscriptions/types.ts`:

```ts
import type { ApiResponse } from '@/features/organizations/types'

export type OrganizationSubscriptionPlan = {
  id: number
  organization_id: number
  title: string
  subtitle?: string
  duration_unit: 'year' | 'month' | 'day' | 'hour' | 'custom'
  duration_value: number
  custom_seconds?: number
  quota_reset_period: 'never' | 'daily' | 'weekly' | 'monthly' | 'custom'
  quota_reset_custom_seconds?: number
  enabled: boolean
  sort_order: number
  total_amount: number
  upgrade_group?: string
}

export type OrganizationUserSubscription = {
  id: number
  organization_id: number
  user_id: number
  plan_id: number
  amount_total: number
  amount_used: number
  start_time: number
  end_time: number
  status: string
  next_reset_time?: number
}

export type OrganizationSubscriptionSummary = {
  subscription?: OrganizationUserSubscription
  plan?: OrganizationSubscriptionPlan
  user?: {
    id: number
    username: string
    display_name?: string
    organization_role?: string
  }
}

export type OrganizationSubscriptionPlanPayload = {
  plan: Partial<OrganizationSubscriptionPlan>
}

export type OrganizationSubscriptionAssignPayload = {
  plan_id: number
}

export type OrganizationSubscriptionResponse<T> = ApiResponse<T>
```

- [ ] **Step 2: Add API client**

Create `web/default/src/features/organizations/subscriptions/api.ts`:

```ts
import { api } from '@/lib/api'
import type {
  OrganizationSubscriptionAssignPayload,
  OrganizationSubscriptionPlan,
  OrganizationSubscriptionPlanPayload,
  OrganizationSubscriptionResponse,
  OrganizationSubscriptionSummary,
} from './types'

function orgQuery(organizationId?: number) {
  return organizationId ? `?organization_id=${organizationId}` : ''
}

export async function getOrganizationSubscriptionPlans(organizationId?: number) {
  const res = await api.get<OrganizationSubscriptionResponse<OrganizationSubscriptionPlan[]>>(
    `/api/organization/subscription/plans${orgQuery(organizationId)}`
  )
  return res.data
}

export async function createOrganizationSubscriptionPlan(data: OrganizationSubscriptionPlanPayload, organizationId?: number) {
  const res = await api.post<OrganizationSubscriptionResponse<OrganizationSubscriptionPlan>>(
    `/api/organization/subscription/plans${orgQuery(organizationId)}`,
    data
  )
  return res.data
}

export async function updateOrganizationSubscriptionPlan(id: number, data: OrganizationSubscriptionPlanPayload, organizationId?: number) {
  const res = await api.put<OrganizationSubscriptionResponse<OrganizationSubscriptionPlan>>(
    `/api/organization/subscription/plans/${id}${orgQuery(organizationId)}`,
    data
  )
  return res.data
}

export async function updateOrganizationSubscriptionPlanStatus(id: number, enabled: boolean, organizationId?: number) {
  const res = await api.patch<OrganizationSubscriptionResponse>(
    `/api/organization/subscription/plans/${id}/status${orgQuery(organizationId)}`,
    { enabled }
  )
  return res.data
}

export async function getOrganizationSubscriptionUsers(organizationId?: number) {
  const res = await api.get<OrganizationSubscriptionResponse<OrganizationSubscriptionSummary[]>>(
    `/api/organization/subscription/users${orgQuery(organizationId)}`
  )
  return res.data
}

export async function assignOrganizationUserSubscription(userId: number, data: OrganizationSubscriptionAssignPayload, organizationId?: number) {
  const res = await api.post<OrganizationSubscriptionResponse>(
    `/api/organization/subscription/users/${userId}/subscriptions${orgQuery(organizationId)}`,
    data
  )
  return res.data
}

export async function invalidateOrganizationUserSubscription(id: number, organizationId?: number) {
  const res = await api.post<OrganizationSubscriptionResponse>(
    `/api/organization/subscription/user_subscriptions/${id}/invalidate${orgQuery(organizationId)}`
  )
  return res.data
}
```

- [ ] **Step 3: Add management page**

Create `web/default/src/features/organizations/subscriptions/organization-subscriptions-page.tsx`. Use existing `SectionPageLayout`, `TitledCard`, `Input`, `Button`, `Select`, `Badge`, and `toast`. The page must:

- load organizations for root and persist selected organization with `organization:last-selected-id`
- list organization plans
- show a compact form for title, total amount, duration, reset period, enabled
- list organization users with current active subscription
- provide a plan select and assign button per user
- provide invalidate button when active subscription exists

- [ ] **Step 4: Add route guard**

Create `web/default/src/routes/_authenticated/organization/subscriptions.tsx`:

```tsx
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { hasOrganizationAdminRole } from '@/lib/organization-roles'
import { ROLE } from '@/lib/roles'
import { OrganizationSubscriptionsPage } from '@/features/organizations/subscriptions/organization-subscriptions-page'

export const Route = createFileRoute('/_authenticated/organization/subscriptions')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    const isRoot = (auth.user?.role ?? 0) >= ROLE.SUPER_ADMIN
    const isOrganizationAdmin = hasOrganizationAdminRole(auth.user?.organization_role)
    if (!auth.user || (!isRoot && !isOrganizationAdmin)) {
      throw redirect({ to: '/403' })
    }
  },
  component: OrganizationSubscriptionsPage,
})
```

- [ ] **Step 5: Add sidebar menu**

Modify `web/default/src/hooks/use-sidebar-data.ts` organization group with:

```ts
{
  title: t('Organization Subscription'),
  url: '/organization/subscriptions',
  icon: CreditCard,
}
```

Import `CreditCard` from `lucide-react` if it is not already imported.

- [ ] **Step 6: Build frontend**

Run:

```bash
bun run build
```

Expected: build succeeds and route tree updates if the router plugin is configured.

- [ ] **Step 7: Commit**

```bash
git add web/default/src/features/organizations/subscriptions web/default/src/routes/_authenticated/organization/subscriptions.tsx web/default/src/hooks/use-sidebar-data.ts web/default/src/routeTree.gen.ts
git commit -m "feat: add organization subscription UI"
```

---

### Task 5: Member Wallet Summary And I18n

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-member-wallet-summary.tsx`
- Modify: `web/default/src/i18n/static-keys.ts`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`

- [ ] **Step 1: Add self subscription API**

Add to `web/default/src/features/organizations/subscriptions/api.ts`:

```ts
export async function getMyOrganizationSubscription() {
  const res = await api.get<OrganizationSubscriptionResponse<OrganizationSubscriptionSummary | null>>(
    '/api/organization/subscription/users/me'
  )
  return res.data
}
```

Task 3 already adds the matching backend route and handler:
`GET /api/organization/subscription/users/me`.

- [ ] **Step 2: Update wallet summary**

Modify `OrganizationMemberWalletSummary` to fetch `getMyOrganizationSubscription()` and render:

```tsx
{subscription?.subscription && subscription.plan && (
  <TitledCard
    title={t('Organization subscription')}
    description={t('Your organization manages this subscription plan.')}
    icon={<Building2 className='h-4 w-4' />}
  >
    <div className='grid gap-2 text-sm sm:grid-cols-3'>
      <div>
        <div className='text-muted-foreground'>{t('Plan')}</div>
        <div className='font-medium'>{subscription.plan.title}</div>
      </div>
      <div>
        <div className='text-muted-foreground'>{t('Remaining')}</div>
        <div className='font-medium'>
          {subscription.subscription.amount_total - subscription.subscription.amount_used}
        </div>
      </div>
      <div>
        <div className='text-muted-foreground'>{t('Expires')}</div>
        <div className='font-medium'>
          {new Date(subscription.subscription.end_time * 1000).toLocaleDateString()}
        </div>
      </div>
    </div>
  </TitledCard>
)}
```

- [ ] **Step 3: Add i18n keys**

Add these keys to `static-keys.ts` and all locale JSON files:

```json
"Organization Subscription": "Organization Subscription",
"Organization subscription": "Organization subscription",
"Your organization manages this subscription plan.": "Your organization manages this subscription plan.",
"Plan": "Plan",
"Remaining": "Remaining",
"Expires": "Expires",
"Assign plan": "Assign plan",
"Invalidate plan": "Invalidate plan"
```

Use Korean meaning as reference while writing non-English translations, and keep English keys unchanged.

- [ ] **Step 4: Run i18n sync**

Run:

```bash
bun run i18n:sync
```

Expected: locale files remain valid JSON.

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/organizations/components/organization-member-wallet-summary.tsx web/default/src/features/organizations/subscriptions/api.ts web/default/src/i18n
git commit -m "feat: show organization subscription summary"
```

---

### Task 6: Final Verification

**Files:**
- All files touched by previous tasks.

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
go test ./model -run 'TestCreateOrganizationSubscription|TestPreConsumeOrganizationUserSubscription' -count=1
go test ./service -run 'TestOrganizationSubscription' -count=1
go test ./controller -run 'TestOrganization.*Subscription' -count=1
```

Expected: all focused tests PASS.

- [ ] **Step 2: Run frontend build**

Run:

```bash
cd web/default
bun run build
```

Expected: build succeeds.

- [ ] **Step 3: Run broad tests and record existing failures**

Run:

```bash
go test ./...
```

Expected: current baseline may still fail for the pre-existing failures recorded in the design doc. Confirm no new organization subscription package/test failures are introduced.

- [ ] **Step 4: Inspect git diff**

Run:

```bash
git diff --stat
git diff --check
git status --short
```

Expected: no whitespace errors. Only intended files modified.

- [ ] **Step 5: Final commit if needed**

If verification caused generated route/i18n changes not yet committed:

```bash
git add web/default/src/routeTree.gen.ts web/default/src/i18n
git commit -m "chore: refresh generated frontend files"
```

---

## Self-Review Notes

- Spec coverage: model separation, organization-only management, mixed quota/subscription operation, organization quota as final budget, root organization selection, member summary, and tests are all mapped to tasks.
- Red flag scan: no incomplete markers or open-ended implementation gaps remain in this plan.
- Type consistency: organization plan/subscription names are consistent across backend and frontend tasks.
