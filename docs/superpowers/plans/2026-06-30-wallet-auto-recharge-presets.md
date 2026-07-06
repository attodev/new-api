# Wallet Auto Recharge Presets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 관리자 프리셋 기반으로 지갑 정기충전/자동충전을 생성하게 바꾸고, 사용자의 직접 금액 입력을 제거한다.

**Architecture:** 관리자 카탈로그는 새 `wallet_auto_recharge_presets` 테이블로 분리한다. 사용자 정책 생성은 `preset_id`만 받으며, 서버가 프리셋을 조회해 기존 `wallet_auto_recharges` row에 금액/주기/기준잔액 스냅샷을 복사한다.

**Tech Stack:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL, React 19, TypeScript, Base UI/shadcn components, Bun, i18next.

## Global Constraints

- 기존 구독 기능과 섞지 않는다.
- Toss 외 결제수단은 지원하지 않는다.
- 사용자는 프리셋 값을 임의 수정할 수 없다.
- 기존 활성 정책은 프리셋 수정/비활성화 후에도 스냅샷 값으로 계속 실행된다.
- `preset_id` 없는 새 정책 생성 요청은 거부한다.
- 적용 가능한 프리셋이 없고 기존 정책도 없으면 해당 사용자 지갑 탭을 숨긴다.
- 적용 가능한 프리셋이 없어도 기존 정책이 있으면 탭은 표시하고 확인/해지만 허용한다.
- JSON marshal/unmarshal은 `common.Marshal`, `common.Unmarshal`, `common.UnmarshalJsonStr`, `common.DecodeJson`을 사용한다.
- DB 변경은 GORM `AutoMigrate`로 SQLite, MySQL, PostgreSQL 호환성을 유지한다.
- 프론트엔드 명령은 `web/default/`에서 Bun을 사용한다.
- UI 문구는 `web/default/src/i18n/locales/{en,kr,zh,fr,ja,ru,vi}.json`에 반영한다.

---

## File Structure

- `model/wallet_auto_recharge_preset.go`: 새 프리셋 모델, 검증, 관리자 CRUD, 사용자용 필터 조회, 프리셋 스냅샷 생성 helper.
- `model/wallet_auto_recharge_preset_test.go`: 프리셋 검증/조회/비활성화 테스트.
- `model/wallet_auto_recharge.go`: `WalletAutoRecharge.PresetId` 추가, `CreatePendingWalletAutoRecharge`가 `PresetId`를 저장하도록 확장.
- `model/main.go`: `WalletAutoRechargePreset` AutoMigrate 추가.
- `controller/wallet_auto_recharge_preset.go`: 관리자 CRUD API와 사용자/조직 프리셋 조회 API.
- `controller/wallet_auto_recharge.go`: 생성 request를 `preset_id` 중심으로 변경하고 서버 프리셋 값을 복사.
- `controller/wallet_auto_recharge_preset_test.go`: 관리자 API, 사용자 필터, preset 기반 생성 테스트.
- `router/api-router.go`: 관리자/사용자/조직 프리셋 route 추가.
- `web/default/src/features/wallet/types.ts`: 프리셋 타입과 생성 request 변경.
- `web/default/src/features/wallet/api.ts`: 프리셋 조회/관리 API 함수 추가.
- `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`: 프리셋 조회와 `preset_id` 생성 흐름 연결.
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`: 직접 입력 폼을 프리셋 카드 선택 UI로 교체.
- `web/default/src/features/wallet/index.tsx`, `web/default/src/features/organizations/components/organization-wallet-section.tsx`: 프리셋/기존 정책 유무에 따라 탭 표시 제어.
- `web/default/src/features/system-settings/billing/section-registry.tsx`: 과금 설정에 `자동충전 프리셋` 섹션 추가.
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`: 관리자 프리셋 관리 UI.
- `web/default/src/i18n/locales/*.json`: 새 문구 번역.

---

### Task 1: 프리셋 모델과 관리자 CRUD

**Files:**
- Create: `model/wallet_auto_recharge_preset.go`
- Create: `model/wallet_auto_recharge_preset_test.go`
- Modify: `model/main.go`

**Interfaces:**
- Produces:
  - `type WalletAutoRechargePreset struct`
  - `type WalletAutoRechargePresetRequest struct`
  - `func CreateWalletAutoRechargePreset(req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error)`
  - `func UpdateWalletAutoRechargePreset(id int, req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error)`
  - `func DisableWalletAutoRechargePreset(id int) error`
  - `func ListWalletAutoRechargePresets(includeDisabled bool) ([]WalletAutoRechargePreset, error)`
  - `func GetWalletAutoRechargePresetForTarget(id int, rechargeType string, targetType string) (*WalletAutoRechargePreset, error)`

- [ ] **Step 1: Write failing model tests**

Create `model/wallet_auto_recharge_preset_test.go` with tests:

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func setupWalletAutoRechargePresetTestDB(t *testing.T) {
	t.Helper()
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.AutoMigrate(&WalletAutoRechargePreset{}))
}

func TestCreateWalletAutoRechargePresetValidatesScheduledPreset(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })

	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "월 1회 500원",
		Amount:        500,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})
	require.ErrorContains(t, err, "amount is below Toss minimum")

	preset, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:              WalletAutoRechargeTypeScheduled,
		TargetScope:       WalletAutoRechargePresetTargetAll,
		Name:              "월 1회 10000원",
		Description:       "개인/조직 공용",
		Amount:            10000,
		IntervalUnit:      WalletAutoRechargeIntervalMonth,
		IntervalValue:     1,
		ChargeImmediately: true,
		SortOrder:         10,
		Enabled:           true,
	})
	require.NoError(t, err)
	require.NotZero(t, preset.Id)
	require.Equal(t, "월 1회 10000원", preset.Name)
	require.True(t, preset.Enabled)
}

func TestWalletAutoRechargePresetTargetFiltering(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	_, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeThreshold,
		TargetScope:   WalletAutoRechargePresetTargetUser,
		Name:          "개인 자동충전",
		Amount:        10000,
		ThresholdAmount: 5000,
		Enabled:       true,
	})
	require.NoError(t, err)

	preset, err := GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeUser)
	require.NoError(t, err)
	require.Equal(t, WalletAutoRechargePresetTargetUser, preset.TargetScope)

	_, err = GetWalletAutoRechargePresetForTarget(1, WalletAutoRechargeTypeThreshold, TopUpTargetTypeOrganization)
	require.ErrorContains(t, err, "wallet auto recharge preset is not available for target")
}

func TestDisableWalletAutoRechargePresetKeepsRow(t *testing.T) {
	setupWalletAutoRechargePresetTestDB(t)
	preset, err := CreateWalletAutoRechargePreset(WalletAutoRechargePresetRequest{
		Type:          WalletAutoRechargeTypeScheduled,
		TargetScope:   WalletAutoRechargePresetTargetAll,
		Name:          "비활성화 테스트",
		Amount:        10000,
		IntervalUnit:  WalletAutoRechargeIntervalMonth,
		IntervalValue: 1,
		Enabled:       true,
	})
	require.NoError(t, err)

	require.NoError(t, DisableWalletAutoRechargePreset(preset.Id))

	var stored WalletAutoRechargePreset
	require.NoError(t, DB.First(&stored, preset.Id).Error)
	require.False(t, stored.Enabled)
}
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRechargePreset -count=1
```

Expected: FAIL because `WalletAutoRechargePreset` and CRUD functions are undefined.

- [ ] **Step 3: Implement preset model**

Create `model/wallet_auto_recharge_preset.go`:

```go
package model

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

const (
	WalletAutoRechargePresetTargetUser         = "user"
	WalletAutoRechargePresetTargetOrganization = "organization"
	WalletAutoRechargePresetTargetAll          = "all"
)

type WalletAutoRechargePreset struct {
	Id                int     `json:"id"`
	Type              string  `json:"type" gorm:"type:varchar(32);index"`
	TargetScope       string  `json:"target_scope" gorm:"type:varchar(32);index"`
	Name              string  `json:"name" gorm:"type:varchar(128)"`
	Description       string  `json:"description" gorm:"type:varchar(255)"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	IntervalUnit      string  `json:"interval_unit" gorm:"type:varchar(16)"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	SortOrder         int     `json:"sort_order" gorm:"index"`
	Enabled           bool    `json:"enabled" gorm:"index"`
	CreateTime        int64   `json:"create_time" gorm:"autoCreateTime"`
	UpdateTime        int64   `json:"update_time" gorm:"autoUpdateTime"`
}

type WalletAutoRechargePresetRequest struct {
	Type              string  `json:"type"`
	TargetScope       string  `json:"target_scope"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Amount            float64 `json:"amount"`
	ThresholdAmount   float64 `json:"threshold_amount"`
	IntervalUnit      string  `json:"interval_unit"`
	IntervalValue     int     `json:"interval_value"`
	CustomSeconds     int64   `json:"custom_seconds"`
	ChargeImmediately bool    `json:"charge_immediately"`
	SortOrder         int     `json:"sort_order"`
	Enabled           bool    `json:"enabled"`
}

func (req WalletAutoRechargePresetRequest) normalizeAndValidate() (WalletAutoRechargePresetRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Type != WalletAutoRechargeTypeScheduled && req.Type != WalletAutoRechargeTypeThreshold {
		return req, errors.New("invalid wallet auto recharge preset type")
	}
	if req.TargetScope != WalletAutoRechargePresetTargetUser &&
		req.TargetScope != WalletAutoRechargePresetTargetOrganization &&
		req.TargetScope != WalletAutoRechargePresetTargetAll {
		return req, errors.New("invalid wallet auto recharge preset target scope")
	}
	if req.Name == "" {
		return req, errors.New("wallet auto recharge preset name is required")
	}
	if walletAutoRechargeKRW(req.Amount) < int64(setting.TossMinTopUp) {
		return req, errors.New("wallet auto recharge preset amount is below Toss minimum")
	}
	if req.Type == WalletAutoRechargeTypeScheduled {
		switch req.IntervalUnit {
		case WalletAutoRechargeIntervalMonth, WalletAutoRechargeIntervalDay:
			if req.IntervalValue <= 0 {
				return req, errors.New("wallet auto recharge preset interval value is invalid")
			}
		case WalletAutoRechargeIntervalCustom:
			if req.CustomSeconds <= 0 {
				return req, errors.New("wallet auto recharge preset custom seconds is invalid")
			}
			if req.IntervalValue <= 0 {
				req.IntervalValue = 1
			}
		default:
			return req, errors.New("wallet auto recharge preset interval is required")
		}
	}
	if req.Type == WalletAutoRechargeTypeThreshold && req.ThresholdAmount < 0 {
		return req, errors.New("wallet auto recharge preset threshold cannot be negative")
	}
	return req, nil
}

func CreateWalletAutoRechargePreset(req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error) {
	req, err := req.normalizeAndValidate()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	preset := &WalletAutoRechargePreset{
		Type:              req.Type,
		TargetScope:       req.TargetScope,
		Name:              req.Name,
		Description:       req.Description,
		Amount:            req.Amount,
		ThresholdAmount:   req.ThresholdAmount,
		IntervalUnit:      req.IntervalUnit,
		IntervalValue:     req.IntervalValue,
		CustomSeconds:     req.CustomSeconds,
		ChargeImmediately: req.ChargeImmediately,
		SortOrder:         req.SortOrder,
		Enabled:           req.Enabled,
		CreateTime:        now,
		UpdateTime:        now,
	}
	return preset, DB.Create(preset).Error
}

func UpdateWalletAutoRechargePreset(id int, req WalletAutoRechargePresetRequest) (*WalletAutoRechargePreset, error) {
	req, err := req.normalizeAndValidate()
	if err != nil {
		return nil, err
	}
	var preset WalletAutoRechargePreset
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"type":               req.Type,
		"target_scope":       req.TargetScope,
		"name":               req.Name,
		"description":        req.Description,
		"amount":             req.Amount,
		"threshold_amount":   req.ThresholdAmount,
		"interval_unit":      req.IntervalUnit,
		"interval_value":     req.IntervalValue,
		"custom_seconds":     req.CustomSeconds,
		"charge_immediately": req.ChargeImmediately,
		"sort_order":         req.SortOrder,
		"enabled":            req.Enabled,
		"update_time":        time.Now().Unix(),
	}
	if err := DB.Model(&preset).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &preset, DB.First(&preset, id).Error
}

func DisableWalletAutoRechargePreset(id int) error {
	return DB.Model(&WalletAutoRechargePreset{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled":     false,
		"update_time": time.Now().Unix(),
	}).Error
}

func ListWalletAutoRechargePresets(includeDisabled bool) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	query := DB.Model(&WalletAutoRechargePreset{})
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	err := query.Order("sort_order asc, id asc").Find(&rows).Error
	return rows, err
}

func walletAutoRechargePresetMatchesTarget(scope string, targetType string) bool {
	return scope == WalletAutoRechargePresetTargetAll ||
		scope == targetType ||
		(scope == WalletAutoRechargePresetTargetUser && targetType == TopUpTargetTypeUser) ||
		(scope == WalletAutoRechargePresetTargetOrganization && targetType == TopUpTargetTypeOrganization)
}

func GetWalletAutoRechargePresetForTarget(id int, rechargeType string, targetType string) (*WalletAutoRechargePreset, error) {
	var preset WalletAutoRechargePreset
	if err := DB.First(&preset, id).Error; err != nil {
		return nil, err
	}
	if !preset.Enabled {
		return nil, errors.New("wallet auto recharge preset is disabled")
	}
	if preset.Type != rechargeType {
		return nil, errors.New("wallet auto recharge preset type mismatch")
	}
	if !walletAutoRechargePresetMatchesTarget(preset.TargetScope, targetType) {
		return nil, errors.New("wallet auto recharge preset is not available for target")
	}
	return &preset, nil
}

func ListWalletAutoRechargePresetsForTarget(targetType string) ([]WalletAutoRechargePreset, error) {
	var rows []WalletAutoRechargePreset
	err := DB.Where("enabled = ?", true).
		Where("target_scope = ? OR target_scope = ?", targetType, WalletAutoRechargePresetTargetAll).
		Order("sort_order asc, id asc").
		Find(&rows).Error
	return rows, err
}
```

- [ ] **Step 4: Add migrations**

Modify `model/main.go`:

```go
&WalletAutoRecharge{},
&WalletAutoRechargePreset{},
```

and in `migrateDBFast`:

```go
{&WalletAutoRecharge{}, "WalletAutoRecharge"},
{&WalletAutoRechargePreset{}, "WalletAutoRechargePreset"},
```

- [ ] **Step 5: Run model tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./model -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add model/wallet_auto_recharge_preset.go model/wallet_auto_recharge_preset_test.go model/main.go
git commit -m "feat(wallet): add auto recharge preset model"
```

---

### Task 2: 프리셋 기반 생성 API와 사용자 조회 API

**Files:**
- Create: `controller/wallet_auto_recharge_preset.go`
- Create: `controller/wallet_auto_recharge_preset_test.go`
- Modify: `controller/wallet_auto_recharge.go`
- Modify: `router/api-router.go`
- Modify: `model/wallet_auto_recharge.go`
- Test: `controller/wallet_auto_recharge_test.go`

**Interfaces:**
- Consumes:
  - `model.GetWalletAutoRechargePresetForTarget(id, rechargeType, targetType)`
  - `model.ListWalletAutoRechargePresetsForTarget(targetType)`
- Produces:
  - `GET /api/user/wallet/auto-recharge/presets`
  - `GET /api/organization/wallet/auto-recharge/presets`
  - `GET /api/admin/wallet/auto-recharge/presets`
  - `POST /api/admin/wallet/auto-recharge/presets`
  - `PUT /api/admin/wallet/auto-recharge/presets/:id`
  - `DELETE /api/admin/wallet/auto-recharge/presets/:id`

- [ ] **Step 1: Write failing controller tests**

Create `controller/wallet_auto_recharge_preset_test.go`:

```go
package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupWalletAutoRechargePresetControllerTestDB(t *testing.T) {
	t.Helper()
	setupWalletAutoRechargeControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.WalletAutoRechargePreset{}))
	originalMin := setting.TossMinTopUp
	setting.TossMinTopUp = 1000
	t.Cleanup(func() { setting.TossMinTopUp = originalMin })
}

func TestAdminCanCreateListAndDisableWalletAutoRechargePreset(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	admin := model.User{Id: 1, Username: "admin", Role: common.RoleAdminUser}

	createRes := performOrganizationRequest(
		CreateWalletAutoRechargePreset,
		admin,
		http.MethodPost,
		"/api/admin/wallet/auto-recharge/presets",
		`{"type":"scheduled","target_scope":"all","name":"월 1회 10000원","amount":10000,"interval_unit":"month","interval_value":1,"charge_immediately":true,"enabled":true}`,
	)
	require.Equal(t, http.StatusOK, createRes.Code)

	listRes := performOrganizationRequest(
		ListWalletAutoRechargePresets,
		admin,
		http.MethodGet,
		"/api/admin/wallet/auto-recharge/presets",
		"",
	)
	require.Equal(t, http.StatusOK, listRes.Code)
	var listPayload struct {
		Success bool                              `json:"success"`
		Data    []model.WalletAutoRechargePreset `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRes.Body.Bytes(), &listPayload))
	require.True(t, listPayload.Success)
	require.Len(t, listPayload.Data, 1)

	disableRes := performOrganizationRequest(
		DeleteWalletAutoRechargePreset,
		admin,
		http.MethodDelete,
		"/api/admin/wallet/auto-recharge/presets/1",
		"",
		gin.Param{Key: "id", Value: "1"},
	)
	require.Equal(t, http.StatusOK, disableRes.Code)

	var stored model.WalletAutoRechargePreset
	require.NoError(t, model.DB.First(&stored, 1).Error)
	require.False(t, stored.Enabled)
}

func TestUserPresetListFiltersByWalletTarget(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	user := model.User{Id: 2, Username: "user", Role: common.RoleCommonUser, AffCode: "preset-user"}
	require.NoError(t, model.DB.Create(&user).Error)
	_, err := model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetUser,
		Name: "개인", Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1, Enabled: true,
	})
	require.NoError(t, err)
	_, err = model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetOrganization,
		Name: "조직", Amount: 10000, IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1, Enabled: true,
	})
	require.NoError(t, err)

	res := performOrganizationRequest(
		GetWalletAutoRechargePresets,
		user,
		http.MethodGet,
		"/api/user/wallet/auto-recharge/presets",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool                              `json:"success"`
		Data    []model.WalletAutoRechargePreset `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "개인", payload.Data[0].Name)
}

func TestWalletAutoRechargeCreateRequiresPresetAndCopiesSnapshot(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	enableTossBillingForTest(t)
	user := model.User{Id: 3, Username: "owner", Role: common.RoleCommonUser, AffCode: "preset-owner"}
	require.NoError(t, model.DB.Create(&user).Error)
	preset, err := model.CreateWalletAutoRechargePreset(model.WalletAutoRechargePresetRequest{
		Type: model.WalletAutoRechargeTypeScheduled, TargetScope: model.WalletAutoRechargePresetTargetUser,
		Name: "월 1회 20000원", Amount: 20000, IntervalUnit: model.WalletAutoRechargeIntervalMonth,
		IntervalValue: 1, ChargeImmediately: true, Enabled: true,
	})
	require.NoError(t, err)

	legacyRes := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		`{"amount":10000,"interval_unit":"month","interval_value":1}`,
	)
	require.Equal(t, http.StatusOK, legacyRes.Code)
	var legacyPayload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(legacyRes.Body.Bytes(), &legacyPayload))
	require.False(t, legacyPayload.Success)

	res := performOrganizationRequest(
		RequestWalletScheduledRecharge,
		user,
		http.MethodPost,
		"/api/user/wallet/auto-recharge/scheduled",
		`{"preset_id":1,"amount":999999,"interval_unit":"day","interval_value":9}`,
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool `json:"success"`
		Data struct {
			TradeNo string `json:"trade_no"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&policy, "auth_trade_no = ?", payload.Data.TradeNo).Error)
	require.Equal(t, preset.Id, policy.PresetId)
	require.Equal(t, float64(20000), policy.Amount)
	require.Equal(t, model.WalletAutoRechargeIntervalMonth, policy.IntervalUnit)
	require.Equal(t, 1, policy.IntervalValue)
	require.True(t, policy.ChargeImmediately)
}
```

- [ ] **Step 2: Run failing controller tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./controller -run "WalletAutoRechargePreset|RequiresPreset" -count=1
```

Expected: FAIL with undefined controller handlers and missing `PresetId`.

- [ ] **Step 3: Add `preset_id` to policy model**

Modify `model/wallet_auto_recharge.go`:

```go
type WalletAutoRecharge struct {
	Id       int `json:"id"`
	PresetId int `json:"preset_id" gorm:"index"`
}

type CreateWalletAutoRechargeRequest struct {
	PresetId int
}
```

Insert `PresetId` into the existing structs without deleting any current fields. `WalletAutoRecharge.Id` already exists; the snippet shows the new field placement, not a full replacement struct.

In `CreatePendingWalletAutoRecharge`, set:

```go
PresetId: req.PresetId,
```

- [ ] **Step 4: Replace create request with preset id**

Modify `controller/wallet_auto_recharge.go` request struct:

```go
type walletAutoRechargeRequest struct {
	PresetId int `json:"preset_id"`
}
```

In `requestWalletAutoRecharge`, after JSON decode and before `GetOrCreateTossCustomerKey`, add:

```go
if req.PresetId <= 0 {
	common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	return
}
preset, err := model.GetWalletAutoRechargePresetForTarget(req.PresetId, policyType, target.TargetType)
if err != nil {
	common.ApiError(c, err)
	return
}
```

Then build `CreateWalletAutoRechargeRequest` from `preset` only:

```go
PresetId:          preset.Id,
Amount:            preset.Amount,
ThresholdAmount:   preset.ThresholdAmount,
IntervalUnit:      preset.IntervalUnit,
IntervalValue:     preset.IntervalValue,
CustomSeconds:     preset.CustomSeconds,
ChargeImmediately: preset.ChargeImmediately,
```

- [ ] **Step 5: Add preset controller handlers**

Create `controller/wallet_auto_recharge_preset.go`:

```go
package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ListWalletAutoRechargePresets(c *gin.Context) {
	rows, err := model.ListWalletAutoRechargePresets(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func CreateWalletAutoRechargePreset(c *gin.Context) {
	var req model.WalletAutoRechargePresetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.CreateWalletAutoRechargePreset(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func UpdateWalletAutoRechargePreset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var req model.WalletAutoRechargePresetRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.UpdateWalletAutoRechargePreset(id, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preset)
}

func DeleteWalletAutoRechargePreset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.DisableWalletAutoRechargePreset(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetWalletAutoRechargePresets(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRechargePresetsForTarget(target.TargetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func GetOrganizationWalletAutoRechargePresets(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRechargePresetsForTarget(target.TargetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
```

- [ ] **Step 6: Add routes**

Modify `router/api-router.go`:

```go
selfRoute.GET("/wallet/auto-recharge/presets", controller.GetWalletAutoRechargePresets)
```

before user create routes.

Add admin routes inside the existing `adminRoute := userRoute.Group("/")` block that already uses `middleware.AdminAuth()`:

```go
adminRoute.GET("/wallet/auto-recharge/presets", controller.ListWalletAutoRechargePresets)
adminRoute.POST("/wallet/auto-recharge/presets", controller.CreateWalletAutoRechargePreset)
adminRoute.PUT("/wallet/auto-recharge/presets/:id", controller.UpdateWalletAutoRechargePreset)
adminRoute.DELETE("/wallet/auto-recharge/presets/:id", controller.DeleteWalletAutoRechargePreset)
```

Add organization route:

```go
organizationRoute.GET("/wallet/auto-recharge/presets", controller.GetOrganizationWalletAutoRechargePresets)
```

- [ ] **Step 7: Run controller tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./controller -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1
env GOCACHE=/tmp/go-build-cache go test ./router -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
git add model/wallet_auto_recharge.go controller/wallet_auto_recharge.go controller/wallet_auto_recharge_preset.go controller/wallet_auto_recharge_preset_test.go controller/wallet_auto_recharge_test.go router/api-router.go
git commit -m "feat(wallet): create auto recharge policies from presets"
```

---

### Task 3: 사용자 지갑 프리셋 선택 UI

**Files:**
- Modify: `web/default/src/features/wallet/types.ts`
- Modify: `web/default/src/features/wallet/api.ts`
- Modify: `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`
- Modify: `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- Modify: `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
- Modify: `web/default/src/features/wallet/index.tsx`
- Modify: `web/default/src/features/organizations/components/organization-wallet-section.tsx`

**Interfaces:**
- Consumes:
  - `GET /api/user/wallet/auto-recharge/presets`
  - `GET /api/organization/wallet/auto-recharge/presets`
  - create request body `{ preset_id: number }`
- Produces:
  - UI hides `정기결제`/`자동결제` tabs when no preset and no existing policy.
  - UI sends only `preset_id` when creating a policy.

- [ ] **Step 1: Write failing frontend tests**

Modify `web/default/src/features/wallet/components/auto-recharge-card.test.ts` to include:

```ts
import { describe, expect, test } from 'bun:test'
import { getVisibleAutoRechargeModes, buildPresetCreatePayload } from './auto-recharge-card'

describe('auto recharge preset UI helpers', () => {
  test('hides mode without preset and without existing policy', () => {
    expect(
      getVisibleAutoRechargeModes([], [])
    ).toEqual([])
  })

  test('shows mode when preset exists', () => {
    expect(
      getVisibleAutoRechargeModes(
        [{ id: 1, type: 'scheduled', target_scope: 'all', name: '월 1회', amount: 10000, enabled: true }],
        []
      )
    ).toEqual(['scheduled'])
  })

  test('shows mode when existing policy exists even without preset', () => {
    expect(
      getVisibleAutoRechargeModes(
        [],
        [{ id: 7, type: 'threshold', status: 'active', amount: 10000 } as never]
      )
    ).toEqual(['threshold'])
  })

  test('builds create payload with preset id only', () => {
    expect(buildPresetCreatePayload(12)).toEqual({ preset_id: 12 })
  })
})
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: FAIL because helper functions and preset types are missing.

- [ ] **Step 3: Add frontend types and API**

Modify `web/default/src/features/wallet/types.ts`:

```ts
export type WalletAutoRechargeTargetScope = 'user' | 'organization' | 'all'

export interface WalletAutoRechargePreset {
  id: number
  type: WalletAutoRechargeType
  target_scope: WalletAutoRechargeTargetScope
  name: string
  description?: string
  amount: number
  threshold_amount?: number
  interval_unit?: 'month' | 'day' | 'custom'
  interval_value?: number
  custom_seconds?: number
  charge_immediately?: boolean
  sort_order?: number
  enabled: boolean
}

export type WalletAutoRechargePresetResponse = ApiResponse<WalletAutoRechargePreset[]>

export interface WalletAutoRechargeRequest {
  preset_id: number
}
```

Modify `web/default/src/features/wallet/api.ts`:

```ts
export async function getWalletAutoRechargePresets(
  scope: 'user' | 'organization' = 'user'
): Promise<WalletAutoRechargePresetResponse> {
  const res = await api.get(`${walletAutoRechargeBase(scope)}/presets`)
  return res.data
}
```

- [ ] **Step 4: Update hook**

Modify `use-wallet-auto-recharge.ts`:

```ts
const [presets, setPresets] = useState<WalletAutoRechargePreset[]>([])

const refreshPresets = useCallback(async () => {
  const response = await getWalletAutoRechargePresets(scope)
  if (isApiSuccess(response) && Array.isArray(response.data)) {
    setPresets(response.data)
  }
}, [scope])

useEffect(() => {
  void refresh()
  void refreshPresets()
}, [refresh, refreshPresets])
```

Return `presets` from the hook. Keep existing policy refresh behavior.

- [ ] **Step 5: Replace manual form with preset selection helpers**

Modify `auto-recharge-card.tsx` exports:

```ts
export function buildPresetCreatePayload(presetId: number): WalletAutoRechargeRequest {
  return { preset_id: presetId }
}

export function getVisibleAutoRechargeModes(
  presets: Array<Pick<WalletAutoRechargePreset, 'type'>>,
  policies: Array<Pick<WalletAutoRechargePolicy, 'type' | 'status'>>
): WalletAutoRechargeType[] {
  return (['scheduled', 'threshold'] as WalletAutoRechargeType[]).filter(
    (mode) =>
      presets.some((preset) => preset.type === mode) ||
      policies.some(
        (policy) =>
          policy.type === mode &&
          (policy.status === 'active' || policy.status === 'pending')
      )
  )
}
```

Change `AutoRechargeCardProps`:

```ts
presets: WalletAutoRechargePreset[]
onCreateScheduled: (payload: WalletAutoRechargeRequest) => Promise<boolean>
onCreateThreshold: (payload: WalletAutoRechargeRequest) => Promise<boolean>
```

Remove amount/threshold/interval inputs. Render preset cards:

```tsx
{availablePresets.map((preset) => (
  <button
    key={preset.id}
    type='button'
    disabled={disabled}
    onClick={() => handleSelectPreset(preset.id)}
    className='w-full rounded-md border p-3 text-left hover:bg-muted'
  >
    <div className='font-medium'>{preset.name}</div>
    {preset.description ? (
      <div className='text-muted-foreground text-sm'>{preset.description}</div>
    ) : null}
    <div className='text-muted-foreground text-sm'>
      {t('Recharge amount')}: {preset.amount}
    </div>
  </button>
))}
```

Use `buildPresetCreatePayload(presetId)` in `handleSelectPreset`.

- [ ] **Step 6: Hide wallet tabs based on presets and policies**

In `web/default/src/features/wallet/index.tsx` and organization wallet section, pass `presets` from hook. Build visible tabs with `getVisibleAutoRechargeModes(presets, policies)`.

If neither auto recharge mode is visible, only render the existing `Top up` tab. If one mode is visible, render only that tab plus `Top up`. If the active tab becomes hidden, reset active tab to `topup`.

- [ ] **Step 7: Run frontend tests**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
BUN_TMPDIR=/tmp bun run build
```

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
git add web/default/src/features/wallet/types.ts web/default/src/features/wallet/api.ts web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/wallet/components/auto-recharge-card.test.ts web/default/src/features/wallet/index.tsx web/default/src/features/organizations/components/organization-wallet-section.tsx
git commit -m "feat(wallet): select auto recharge presets in wallet UI"
```

---

### Task 4: 관리자 프리셋 관리 UI

**Files:**
- Create: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- Create: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
- Modify: `web/default/src/features/system-settings/billing/section-registry.tsx`
- Modify: `web/default/src/features/system-settings/billing/index.tsx`
- Modify: `web/default/src/features/wallet/api.ts`
- Modify: `web/default/src/features/wallet/types.ts`

**Interfaces:**
- Consumes:
  - Admin preset CRUD endpoints from Task 2.
- Produces:
  - Billing settings section id `wallet-auto-recharge-presets`.
  - Admin functions `listAdminWalletAutoRechargePresets`, `createAdminWalletAutoRechargePreset`, `updateAdminWalletAutoRechargePreset`, `deleteAdminWalletAutoRechargePreset`.

- [ ] **Step 1: Write failing admin UI tests**

Create `wallet-auto-recharge-presets-section.test.ts`:

```ts
import { describe, expect, test } from 'bun:test'
import {
  normalizePresetForm,
  getPresetSummary,
} from './wallet-auto-recharge-presets-section'

describe('wallet auto recharge preset admin helpers', () => {
  test('normalizes scheduled preset form values', () => {
    expect(
      normalizePresetForm({
        type: 'scheduled',
        target_scope: 'all',
        name: ' 월 1회 ',
        description: '',
        amount: '10000',
        threshold_amount: '',
        interval_unit: 'month',
        interval_value: '1',
        custom_seconds: '',
        charge_immediately: true,
        sort_order: '10',
        enabled: true,
      })
    ).toEqual({
      type: 'scheduled',
      target_scope: 'all',
      name: '월 1회',
      description: '',
      amount: 10000,
      threshold_amount: 0,
      interval_unit: 'month',
      interval_value: 1,
      custom_seconds: 0,
      charge_immediately: true,
      sort_order: 10,
      enabled: true,
    })
  })

  test('summarizes threshold preset', () => {
    expect(
      getPresetSummary({
        id: 1,
        type: 'threshold',
        target_scope: 'user',
        name: '자동',
        amount: 20000,
        threshold_amount: 5000,
        enabled: true,
      })
    ).toContain('5000')
  })
})
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: FAIL because component/helper file does not exist.

- [ ] **Step 3: Add admin API functions**

Modify `web/default/src/features/wallet/api.ts`:

```ts
export async function listAdminWalletAutoRechargePresets(): Promise<WalletAutoRechargePresetResponse> {
  const res = await api.get('/api/admin/wallet/auto-recharge/presets')
  return res.data
}

export async function createAdminWalletAutoRechargePreset(
  preset: WalletAutoRechargePresetRequest
): Promise<ApiResponse<WalletAutoRechargePreset>> {
  const res = await api.post('/api/admin/wallet/auto-recharge/presets', preset)
  return res.data
}

export async function updateAdminWalletAutoRechargePreset(
  id: number,
  preset: WalletAutoRechargePresetRequest
): Promise<ApiResponse<WalletAutoRechargePreset>> {
  const res = await api.put(`/api/admin/wallet/auto-recharge/presets/${id}`, preset)
  return res.data
}

export async function deleteAdminWalletAutoRechargePreset(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/admin/wallet/auto-recharge/presets/${id}`)
  return res.data
}
```

- [ ] **Step 4: Build admin section component**

Create `wallet-auto-recharge-presets-section.tsx` with:

```tsx
export interface WalletAutoRechargePresetFormState {
  type: WalletAutoRechargeType
  target_scope: WalletAutoRechargeTargetScope
  name: string
  description: string
  amount: string
  threshold_amount: string
  interval_unit: 'month' | 'day' | 'custom'
  interval_value: string
  custom_seconds: string
  charge_immediately: boolean
  sort_order: string
  enabled: boolean
}

export function normalizePresetForm(form: WalletAutoRechargePresetFormState): WalletAutoRechargePresetRequest {
  return {
    type: form.type,
    target_scope: form.target_scope,
    name: form.name.trim(),
    description: form.description.trim(),
    amount: Number(form.amount || 0),
    threshold_amount: Number(form.threshold_amount || 0),
    interval_unit: form.interval_unit,
    interval_value: Number(form.interval_value || 0),
    custom_seconds: Number(form.custom_seconds || 0),
    charge_immediately: form.charge_immediately,
    sort_order: Number(form.sort_order || 0),
    enabled: form.enabled,
  }
}

export function getPresetSummary(preset: WalletAutoRechargePreset) {
  if (preset.type === 'scheduled') {
    return `${preset.amount} / ${preset.interval_value || 1} ${preset.interval_unit || 'month'}`
  }
  return `${preset.threshold_amount ?? 0} 이하 시 ${preset.amount}`
}
```

The component must implement these behaviors:

- load presets with `listAdminWalletAutoRechargePresets`.
- show scheduled and threshold groups.
- show create/edit form fields.
- call create/update/delete admin API functions.
- use switches for `enabled` and `charge_immediately`.
- use selects for `type`, `target_scope`, `interval_unit`.
- use numeric inputs for amounts/order/interval values.

- [ ] **Step 5: Register billing section**

Modify `section-registry.tsx`:

```tsx
import { WalletAutoRechargePresetsSection } from '../integrations/wallet-auto-recharge-presets-section'
```

Add section before `checkin`:

```tsx
{
  id: 'wallet-auto-recharge-presets',
  titleKey: 'Auto Recharge Presets',
  build: () => <WalletAutoRechargePresetsSection />,
}
```

- [ ] **Step 6: Run admin UI tests and build**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
BUN_TMPDIR=/tmp bun run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts web/default/src/features/system-settings/billing/section-registry.tsx web/default/src/features/wallet/api.ts web/default/src/features/wallet/types.ts
git commit -m "feat(settings): manage wallet auto recharge presets"
```

---

### Task 5: i18n, 회귀 검증, 최종 빌드

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/kr.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`
- Test: relevant backend/frontend tests from Tasks 1-4.

**Interfaces:**
- Consumes all previous tasks.
- Produces a verified branch ready for final code review and service rebuild.

- [ ] **Step 1: Add locale keys**

Add these source keys to all locale files. Use the English values below for `en`, `zh`, `fr`, `ru`, and `vi` if no established local translation is already present. Use the Korean values shown after the table for `kr`, and Japanese values shown after that for `ja`.

```json
{
  "Auto Recharge Presets": "Auto Recharge Presets",
  "Scheduled recharge presets": "Scheduled recharge presets",
  "Auto recharge presets": "Auto recharge presets",
  "Preset name": "Preset name",
  "Preset description": "Preset description",
  "Target scope": "Target scope",
  "Applies to personal wallets": "Applies to personal wallets",
  "Applies to organization wallets": "Applies to organization wallets",
  "Applies to all wallets": "Applies to all wallets",
  "No auto recharge presets are available.": "No auto recharge presets are available.",
  "This preset is no longer available. Existing policy values are preserved.": "This preset is no longer available. Existing policy values are preserved.",
  "Select this preset": "Select this preset",
  "Disable preset": "Disable preset"
}
```

For `kr.json`, use:

```json
{
  "Auto Recharge Presets": "자동충전 프리셋",
  "Scheduled recharge presets": "정기결제 프리셋",
  "Auto recharge presets": "자동결제 프리셋",
  "Preset name": "프리셋 이름",
  "Preset description": "프리셋 설명",
  "Target scope": "적용 대상",
  "Applies to personal wallets": "개인 지갑에 적용",
  "Applies to organization wallets": "조직 지갑에 적용",
  "Applies to all wallets": "모든 지갑에 적용",
  "No auto recharge presets are available.": "사용 가능한 자동충전 프리셋이 없습니다.",
  "This preset is no longer available. Existing policy values are preserved.": "이 프리셋은 더 이상 사용할 수 없습니다. 기존 정책 값은 유지됩니다.",
  "Select this preset": "이 프리셋 선택",
  "Disable preset": "프리셋 비활성화"
}
```

For `ja.json`, use:

```json
{
  "Auto Recharge Presets": "自動チャージプリセット",
  "Scheduled recharge presets": "定期チャージプリセット",
  "Auto recharge presets": "自動チャージプリセット",
  "Preset name": "プリセット名",
  "Preset description": "プリセット説明",
  "Target scope": "適用対象",
  "Applies to personal wallets": "個人ウォレットに適用",
  "Applies to organization wallets": "組織ウォレットに適用",
  "Applies to all wallets": "すべてのウォレットに適用",
  "No auto recharge presets are available.": "利用可能な自動チャージプリセットはありません。",
  "This preset is no longer available. Existing policy values are preserved.": "このプリセットは利用できなくなりました。既存のポリシー値は保持されます。",
  "Select this preset": "このプリセットを選択",
  "Disable preset": "プリセットを無効化"
}
```

- [ ] **Step 2: Run i18n sync**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun run i18n:sync
BUN_TMPDIR=/tmp bun test src/i18n/languages.test.ts
```

Expected: PASS. If `_reports/_sync-report.json` changes only because the script rewrites metadata, review before committing.

- [ ] **Step 3: Run focused backend tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./model -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1
env GOCACHE=/tmp/go-build-cache go test ./controller -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1
env GOCACHE=/tmp/go-build-cache go test ./service -run WalletAutoRecharge -count=1
env GOCACHE=/tmp/go-build-cache go test ./router -count=1
```

Expected: PASS.

- [ ] **Step 4: Run payment regression tests**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go test ./controller -run "Toss|TopUp|OrganizationWallet|Organization.*Wallet|SubscriptionToss|WalletAutoRecharge" -count=1
env GOCACHE=/tmp/go-build-cache go test ./model -run "RechargeWaffoPancake|UpdatePendingTopUpStatus|ManualCompleteTopUp|CompleteSubscriptionOrder|ExpireSubscriptionOrder|WalletAutoRecharge" -count=1
```

Expected: PASS.

- [ ] **Step 5: Run frontend tests and build**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
BUN_TMPDIR=/tmp bun run build
```

Expected: PASS.

- [ ] **Step 6: Build backend**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go build -o /tmp/new-api-wallet-auto-recharge/new-api-bin .
```

Expected: PASS. If Go VCS stamping fails with `error obtaining VCS status`, rerun:

```bash
env GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o /tmp/new-api-wallet-auto-recharge/new-api-bin .
```

Expected: PASS.

- [ ] **Step 7: Commit final i18n/verification fixes**

If Step 1 changed locale files, commit:

```bash
git add web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json web/default/src/i18n/locales/zh.json web/default/src/i18n/locales/fr.json web/default/src/i18n/locales/ja.json web/default/src/i18n/locales/ru.json web/default/src/i18n/locales/vi.json
git commit -m "fix(i18n): add wallet auto recharge preset translations"
```

If verification required code fixes, commit the changed files with:

```bash
git add model/wallet_auto_recharge_preset.go model/wallet_auto_recharge.go controller/wallet_auto_recharge.go controller/wallet_auto_recharge_preset.go router/api-router.go web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx
git commit -m "fix(wallet): stabilize auto recharge presets"
```

Do not create an empty commit.

---

## Self-Review Checklist

- Spec coverage:
  - DB 테이블형 프리셋: Task 1.
  - 관리자 CRUD와 과금 설정 UI: Task 2, Task 4.
  - 사용자 `preset_id` 기반 생성과 직접 입력 제거: Task 2, Task 3.
  - 개인/조직/둘 다 적용 범위: Task 1, Task 2, Task 3.
  - 프리셋 스냅샷 유지: Task 2.
  - 프리셋 없음 탭 숨김 및 기존 정책 해지만 허용: Task 3.
  - i18n/회귀/빌드: Task 5.
- Placeholder scan: no `TBD`, `TODO`, `implement later`, or unresolved optional behavior remains.
- Type consistency:
  - Backend request uses `PresetId int`.
  - Frontend request uses `preset_id: number`.
  - Preset type values are `scheduled`, `threshold`; target scopes are `user`, `organization`, `all`.
