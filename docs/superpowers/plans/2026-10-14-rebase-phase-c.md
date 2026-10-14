# Upstream Rebase 단계 C 구현 계획 — O6 할인, O7 타이밍, 단계 B 이월

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `develop`(upstream `7de10f2c6` + O1~O5·O8·O10 + O3 조직) 위에 모델/벤더 할인 계층(O6)과 요청 타이밍 분해(O7)를 훅 한 줄 + fork 파일 방식으로 다시 올리고, 단계 B가 넘긴 제품 결정(멤버 동의·조직 범위 상태/메모)과 소형 강화를 끝낸다.

**Architecture:** O6은 team의 `setting/ratio_setting/model_discount.go`를 그대로 복사하고 `HandleGroupRatio` 한 지점에서 `GroupRatio`에 배수를 접어 넣는다(사전차감 == 정산). 표시는 upstream의 새 가격 페이지·사용 로그 상세·설정 탭 구조 위에 다시 만든다(spec §9 결정 3). O7은 team 파일 4개(미들웨어, 응답 writer, 바디 wrapper, 로그 helper)를 복사하고 upstream 파일에는 `doRequest` 3줄·라우터 1줄·`RelayInfo` 필드·`GenerateTextOtherInfo` 1줄만 둔다. 이월 항목은 조직 overlay(O3)의 fork 파일 안에서 끝낸다.

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, GORM(SQLite/MySQL/PostgreSQL 동시 호환), `types.RWMap`, React 19 + Base UI + TanStack Router/Query(`web/src`), vitest + @testing-library, oxlint/oxfmt, Bun.

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §4.6(O6), §4.7(O7), §6, §7, §8 단계 C, §9 결정 3·6·7. team 설계 원본: `git show team:docs/discount/design.md`, `git show team:docs/superpowers/specs/2026-07-02-timing-breakdown-design.md`. 단계 B 이월 목록: `docs/2026-10-14-rebase-phase-b.md` §4.

## Global Constraints

- 작업 트리: `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`. `/Users/molla/work/ai/new-api`(team 체크아웃)는 읽기 전용. team 소스는 `git show team:<path>`로 읽는다.
- Go 명령은 항상 `GOWORK=off`. JSON marshal/unmarshal은 `common.Marshal/Unmarshal/DecodeJson*`만 사용(`encoding/json` 직접 호출 금지).
- DB 호환: GORM 추상화 우선. raw SQL이 불가피하면 `commonGroupCol`/`commonTrueVal`을 쓰고 SQLite에서 `ALTER COLUMN` 금지. 새 컬럼은 구조체 필드 + AutoMigrate.
- upstream 파일 수정은 훅 한 줄(또는 구조체 필드 한 줄)로 제한하고 `// fork: O6` / `// fork: O7` / `// fork: O3` 주석을 단다. 로직은 fork 파일에 둔다. 모든 훅은 `docs/fork-overlays.md`에 `파일:줄` 행으로 기록한다.
- `web/src`의 새 TS/TSX 파일은 1행에 AGPL 헤더(기존 `web/src/features/organizations/*.ts` 파일의 헤더를 복사). 포맷·린트 게이트: 손댄 파일마다 `bunx oxlint -c .oxlintrc.json ./src/...`와 `bunx oxfmt --check ./src/...`에 새 문제 없음(`bun run format`은 트리 전체를 고치므로 금지). i18n 키는 영어 원문이며 7개 locale(`en, zh, zh-TW, fr, ja, ru, vi`)에 모두 추가(`bun run i18n:sync`로 동기화 가능; 번역은 단계 D).
- 커밋: team에서 옮긴 커밋은 `Ported-From: team <path>` 트레일러, 모든 커밋은 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` 트레일러.
- 테스트 환경: SQLite in-memory(`controller/organization_test.go`의 설정을 따른다). MySQL/PostgreSQL DSN 게이트 테스트와 스키마 리허설은 이 장비에 컨테이너가 없어 운영자 작업으로 기록한다.
- 과금 불변식: 할인은 경로마다 정확히 한 번, 사전차감 == 정산, 정가(`ModelRatio`/`ModelPrice`)는 바꾸지 않는다.

## Review Focus

1. **100% 할인 모델** — 과금 0이어야 하고 로그의 `group_ratio`는 할인 전 기저값이 남아야 한다(배수 0으로 나누는 복원은 금지). → Task 2 `TestAppendDiscountInfo_FullDiscountKeepsBaseGroupRatio`.
2. **스트리밍 중 클라이언트가 끊긴 요청** — `llm_ms`는 업스트림 Close 시각, `e2e_ms`는 마지막 Write 시각이라 `gateway_ms`가 음수가 될 수 있다. 0으로 바닥을 쳐야 한다. → Task 6 `TestAppendTimingBreakdown_NegativeGatewayFloorsToZero`.
3. **채널 재시도된 요청** — `llm_ms`는 마지막 시도만, `e2e_ms`는 전체. 사용자에게 보이는 "Gateway Overhead"가 재시도 대기까지 포함한다는 사실이 테스트로 고정돼야 한다. → Task 6 `TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway`.
4. **할인 JSON에 범위 밖·비숫자 값** — `{"gpt-4":101}`, `{"gpt-4":"a"}`는 저장이 거부되고 이전 값이 유지돼야 한다. → Task 1 `TestDiscountValidationRejectsOutOfRange`, `TestUpdateModelDiscountRejectsNonNumeric`.
5. **조직에서 정지된 멤버** — relay는 403, 본인 `/wallet` 요약과 `subscription/self` 읽기는 허용, 플랫폼 `users.status`는 건드리지 않아야 한다. → Task 9 `TestSuspendedOrganizationMemberRelayRejectedButSelfReadsAllowed`.

---

## 파일 구조

| 영역 | 생성 | 수정(훅) |
|---|---|---|
| O6 설정 | `setting/ratio_setting/model_discount.go`(+`_test.go`), `model/pricing_discount.go`(+`_test.go`) | `model/option.go:165,663`, `model/pricing.go:25,57,92,220-228,428`, `types/price_data.go:10-14` |
| O6 과금·로그 | `service/log_discount_fork.go`(+`_test.go`), `relay/helper/price_discount_test.go` | `relay/helper/price.go:60-62`, `service/quota.go:121`, `service/task_billing.go:422`, `service/log_info_generate.go:109` |
| O6 프런트 | `web/src/features/pricing/lib/discount-color.ts`, `web/src/features/pricing/__tests__/discount.test.tsx`, `web/src/features/system-settings/models/{model,vendor}-discount-visual-editor.tsx`(+tests) | `pricing/types.ts`, `pricing/lib/price.ts`, `pricing/components/{model-price-cell,model-card,pricing-sidebar,model-details}.tsx`, `usage-logs/types.ts`, `usage-logs/components/dialogs/details-dialog.tsx`, `system-settings/{types.ts,billing/index.tsx,billing/section-registry.tsx,models/ratio-settings-card.tsx,models/group-ratio-form.tsx,models/group-ratio-visual-editor.tsx}` |
| O7 백엔드 | `middleware/request_timing.go`, `common/timing_response_writer.go`(+`_test.go`), `relay/channel/timing_body.go`(+`_test.go`), `service/log_timing_fork.go`(+`_test.go`) | `constant/context_key.go:11`, `router/relay-router.go:15`, `relay/common/relay_info.go:93,999`, `relay/channel/api_request.go:563-585`, `service/log_info_generate.go:111` |
| O7 프런트 | — | `usage-logs/types.ts`, `usage-logs/components/dialogs/details-dialog.tsx`, `usage-logs/components/columns/common-logs-columns.tsx`, `usage-logs/components/common-log-mobile-card.tsx`, 7 locale |
| 이월 | `model/organization_member_status.go`, `controller/organization_member_lookup.go`, `controller/organization_topup_admin.go` | `controller/organization.go`, `service/org_user_export.go`, `model/user.go`(필드 2줄), `service/billing_session_organization.go`, `service/midjourney_organization.go`, `service/quota.go`, `controller/user.go`, `controller/checkin.go`, `router/organization-router.go`, 조직 프런트 |
| 문서 | `docs/discount/{admin-guide,design}.md`, `docs/discount/images/*`, `docs/2026-10-14-rebase-phase-c.md` | `docs/fork-overlays.md`, spec §9 |

---

### Task 1: O6 할인 설정 계층(백엔드)

**Files:**
- Create: `setting/ratio_setting/model_discount.go`, `setting/ratio_setting/model_discount_test.go`, `model/pricing_discount.go`, `model/pricing_discount_test.go`
- Modify: `model/option.go:165`(InitOptionMap), `model/option.go:663`(updateOptionMap), `model/pricing.go:25`(`Pricing`), `model/pricing.go:57`(`PricingVendor`), `model/pricing.go:92`(`InvalidatePricingCache`), `model/pricing.go:220-228`(벤더 목록), `model/pricing.go:428`(`pricingMap = append` 직전), `types/price_data.go:10-14`

**Interfaces:**
- Consumes: upstream `types.NewRWMap`, `ratio_setting.FormatMatchingModelName`(`model_ratio.go:724`), `model.InvalidatePricingCache()`.
- Produces: `ratio_setting.GetEffectiveDiscountMultiplier(modelName string) float64`, `GetModelDiscountMultiplier(modelName) (float64, bool)`, `GetVendorDiscountMultiplier(vendorName) (float64, bool)`, `GetModelDiscountPercent/GetVendorDiscountPercent(name) (float64, bool)`, `SetVendorResolver(fn func(modelName string) (string, bool))`, `UpdateModelDiscountByJSONString/UpdateVendorDiscountByJSONString(string) error`, `ModelDiscount2JSONString/VendorDiscount2JSONString() string`; `types.GroupRatioInfo{BaseGroupRatio, DiscountMultiplier float64; DiscountSource string}`; `model.Pricing.DiscountPercent`, `model.PricingVendor.DiscountPercent`(`json:"discount_percent,omitempty"`); `model.effectiveDiscountPercent(modelName, vendorName string) float64`.

- [ ] **Step 1: team 파일 복사와 테스트 복사**

```bash
git show team:setting/ratio_setting/model_discount.go > setting/ratio_setting/model_discount.go
git show team:setting/ratio_setting/model_discount_test.go > setting/ratio_setting/model_discount_test.go
```

`model_discount.go`는 그대로 쓴다(API는 Interfaces 참고). `validateDiscountJSON`이 `common.UnmarshalJsonStr`를 쓰는지 확인하고, `encoding/json`을 직접 부르면 `common.*`으로 바꾼다.

- [ ] **Step 2: 비숫자 거부 테스트 추가(실패 확인)**

`setting/ratio_setting/model_discount_test.go` 끝에:

```go
func TestUpdateModelDiscountRejectsNonNumeric(t *testing.T) {
	require.NoError(t, UpdateModelDiscountByJSONString(`{"gpt-4":25}`))
	require.Error(t, UpdateModelDiscountByJSONString(`{"gpt-4":"a"}`))
	mul, ok := GetModelDiscountMultiplier("gpt-4")
	require.True(t, ok)
	require.InDelta(t, 0.75, mul, 1e-9) // 이전 값 유지
}
```

Run: `GOWORK=off go test ./setting/ratio_setting/ -run 'Discount' -v`
Expected: 복사한 6개는 PASS, `TestUpdateModelDiscountRejectsNonNumeric`는 `validateDiscountJSON`이 문자열 값을 `map[string]float64`로 못 읽어 이미 실패하면 PASS일 수 있다 — 그 경우 "이전 값 유지" 단언이 핵심이므로 결과를 그대로 기록한다.

- [ ] **Step 3: GroupRatioInfo 필드**

`types/price_data.go:10-14`의 `GroupRatioInfo`에 추가:

```go
	BaseGroupRatio     float64 `json:"base_group_ratio,omitempty"`     // fork: O6 — 할인 접기 전 GroupRatio
	DiscountMultiplier float64 `json:"discount_multiplier,omitempty"` // fork: O6
	DiscountSource     string  `json:"discount_source,omitempty"`     // fork: O6 — "model" | "vendor" | ""
```

- [ ] **Step 4: 옵션 연동 훅**

`model/option.go:165` 바로 뒤(`ModelRatio` 등록 다음):

```go
	common.OptionMap["ModelDiscount"] = ratio_setting.ModelDiscount2JSONString()   // fork: O6
	common.OptionMap["VendorDiscount"] = ratio_setting.VendorDiscount2JSONString() // fork: O6
```

`model/option.go:663` 뒤 `updateOptionMap` switch:

```go
	case "ModelDiscount", "VendorDiscount": // fork: O6
		err = updateDiscountOption(key, value)
```

`model/pricing_discount.go`(새 파일):

```go
package model

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// updateDiscountOption stores a discount map and drops the pricing cache so
// the marketplace and billing see the new percent immediately.
func updateDiscountOption(key, value string) error {
	var err error
	switch key {
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
	}
	if err == nil {
		InvalidatePricingCache()
	}
	return err
}

// effectiveDiscountPercent is the percent shown for a model: its own model
// discount wins over its vendor's discount; 0 when neither is set.
func effectiveDiscountPercent(modelName, vendorName string) float64 {
	if pct, ok := ratio_setting.GetModelDiscountPercent(modelName); ok {
		return pct
	}
	if vendorName != "" {
		if pct, ok := ratio_setting.GetVendorDiscountPercent(vendorName); ok {
			return pct
		}
	}
	return 0
}

var modelVendorNameCache map[string]string // modelName -> vendor name, rebuilt with pricingMap

func init() {
	ratio_setting.SetVendorResolver(resolveVendorName)
}

// resolveVendorName answers the vendor name of a model for vendor discounts.
// It reads the pricing cache (refreshing it if stale) and must not be called
// while channelSyncLock is held (lock order: updatePricingLock -> channelSyncLock).
func resolveVendorName(modelName string) (string, bool) {
	GetPricing()
	updatePricingLock.Lock() // sync.Mutex (model/pricing.go:65)
	defer updatePricingLock.Unlock()
	name, ok := modelVendorNameCache[modelName]
	return name, ok && name != ""
}
```

- [ ] **Step 5: 가격 API 필드와 채우기**

`model/pricing.go:25` `Pricing`에 `DiscountPercent float64 \`json:"discount_percent,omitempty"\` // fork: O6`, `:57` `PricingVendor`에 같은 필드. `:92` `InvalidatePricingCache` 안에 `modelVendorNameCache = nil // fork: O6`. `:220-228` 벤더 목록을 만드는 루프에서 `PricingVendor{...}` 리터럴에 `DiscountPercent: vendorDiscountPercent(v.Name)` 한 줄(함수는 `pricing_discount.go`에 `func vendorDiscountPercent(name string) float64 { pct, _ := ratio_setting.GetVendorDiscountPercent(name); return pct }`). `:428` `pricingMap = append(pricingMap, pricing)` 직전에 `applyPricingDiscount(&pricing, vendorNameByID) // fork: O6` 한 줄. `vendorNameByID map[int]string`은 벤더 루프에서 같이 채운다(한 줄: `vendorNameByID[v.Id] = v.Name // fork: O6`). `pricing_discount.go`에:

```go
func applyPricingDiscount(p *Pricing, vendorNameByID map[int]string) {
	vendorName := vendorNameByID[p.VendorID]
	p.DiscountPercent = effectiveDiscountPercent(p.ModelName, vendorName)
	if modelVendorNameCache == nil {
		modelVendorNameCache = map[string]string{}
	}
	modelVendorNameCache[p.ModelName] = vendorName
}
```

`updatePricing()`은 이미 `updatePricingLock`을 잡고 있으므로 캐시 쓰기는 안전하다(확인 후 주석에 남긴다).

- [ ] **Step 6: 순수 함수 테스트(실패 → 통과)**

`model/pricing_discount_test.go`:

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestEffectiveDiscountPercentModelBeatsVendor(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"gpt-4":25}`))
	require.NoError(t, ratio_setting.UpdateVendorDiscountByJSONString(`{"OpenAI":10}`))
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
		_ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`)
	})
	require.Equal(t, 25.0, effectiveDiscountPercent("gpt-4", "OpenAI"))
	require.Equal(t, 10.0, effectiveDiscountPercent("gpt-4o", "OpenAI"))
	require.Equal(t, 0.0, effectiveDiscountPercent("claude-3", "Anthropic"))
	require.Equal(t, 0.0, effectiveDiscountPercent("gpt-4o", ""))
}

func TestApplyPricingDiscountFillsCache(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateVendorDiscountByJSONString(`{"OpenAI":10}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`); modelVendorNameCache = nil })
	p := Pricing{ModelName: "gpt-4o", VendorID: 7}
	applyPricingDiscount(&p, map[int]string{7: "OpenAI"})
	require.Equal(t, 10.0, p.DiscountPercent)
	require.Equal(t, "OpenAI", modelVendorNameCache["gpt-4o"])
}
```

Run: `GOWORK=off go test ./model/ -run 'Discount' -v` → 처음엔 컴파일 실패, 구현 후 PASS. 이어서 `GOWORK=off go build ./... && GOWORK=off go test ./setting/... ./model/ ./types/...`.

- [ ] **Step 7: 커밋**

```bash
git add setting/ratio_setting/model_discount.go setting/ratio_setting/model_discount_test.go model/pricing_discount.go model/pricing_discount_test.go model/option.go model/pricing.go types/price_data.go
git commit -m "feat(pricing): port the model/vendor discount setting layer

Ported-From: team setting/ratio_setting/model_discount.go model/pricing.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: O6 과금 fold와 로그 필드

**Files:**
- Create: `service/log_discount_fork.go`, `service/log_discount_fork_test.go`, `relay/helper/price_discount_test.go`
- Modify: `relay/helper/price.go:60-62`(`HandleGroupRatio` return 직전), `service/quota.go:121`(`PreWssConsumeQuota`), `service/task_billing.go:422`(`RecalculateTaskQuotaByTokens`), `service/log_info_generate.go:109`(`GenerateTextOtherInfo`의 `user_group_ratio` 다음)

**Interfaces:**
- Consumes: Task 1의 `GetEffectiveDiscountMultiplier`, `GetModelDiscountMultiplier`, `GroupRatioInfo.{BaseGroupRatio,DiscountMultiplier,DiscountSource}`; upstream `relayInfo.GetBillingModelName()`(`relay/common/relay_info.go:852`), `model.LogOther.SetPublic(key, value) bool`.
- Produces: 로그 `other.discount_multiplier`(float64), `other.discount_source`("model"|"vendor"), `other.group_ratio` = 할인 전 기저값(기존 키 덮어씀); `service.appendDiscountInfo(other *model.LogOther, relayInfo *relaycommon.RelayInfo)`.

- [ ] **Step 1: HandleGroupRatio 테스트(실패 확인)**

`relay/helper/price_discount_test.go`:

```go
package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleGroupRatioFoldsEffectiveDiscount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"gpt-4":30}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateModelDiscountByJSONString(`{}`) })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-4", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{}}

	got := HandleGroupRatio(c, info)

	require.InDelta(t, 0.7, got.DiscountMultiplier, 1e-9)
	require.Equal(t, "model", got.DiscountSource)
	require.InDelta(t, got.BaseGroupRatio*0.7, got.GroupRatio, 1e-9)
}

func TestHandleGroupRatioWithoutDiscountIsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "claude-3", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{}}

	got := HandleGroupRatio(c, info)

	require.InDelta(t, 1.0, got.DiscountMultiplier, 1e-9)
	require.Equal(t, "", got.DiscountSource)
	require.InDelta(t, got.BaseGroupRatio, got.GroupRatio, 1e-9)
}
```

`HandleGroupRatio`가 `relayInfo`의 다른 필드(사용자 특별 배율 등)를 요구하면 `relay/helper`의 기존 테스트에서 RelayInfo를 어떻게 만드는지 보고 맞춘다. Run: `GOWORK=off go test ./relay/helper/ -run HandleGroupRatio -v` → FAIL(필드 미설정).

- [ ] **Step 2: 훅**

`relay/helper/price.go:60-62` `return groupRatioInfo` 직전:

```go
	foldDiscountIntoGroupRatio(&groupRatioInfo, relayInfo.GetBillingModelName()) // fork: O6
```

같은 파일 끝(또는 새 fork 파일 `relay/helper/price_discount.go`)에:

```go
// foldDiscountIntoGroupRatio applies the effective model/vendor discount once,
// at the single point every billing path (pre-consume, settlement, tiered,
// per-call, retry) reads GroupRatioInfo from.
func foldDiscountIntoGroupRatio(info *hosttypes.GroupRatioInfo, modelName string) {
	info.BaseGroupRatio = info.GroupRatio
	mul := ratio_setting.GetEffectiveDiscountMultiplier(modelName)
	info.DiscountMultiplier = mul
	if mul == 1 {
		return
	}
	if _, ok := ratio_setting.GetModelDiscountMultiplier(modelName); ok {
		info.DiscountSource = "model"
	} else {
		info.DiscountSource = "vendor"
	}
	info.GroupRatio *= mul
}
```

fork 파일로 두면 `price.go`의 훅은 한 줄이다(권장). `service/quota.go:121` 뒤: `actualGroupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName) // fork: O6`. `service/task_billing.go:422` 뒤: `finalGroupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName) // fork: O6`(변수명은 그 함수의 실제 이름을 쓴다).

Run: Step 1 테스트 PASS.

- [ ] **Step 3: 로그 helper 테스트(실패 확인)**

`service/log_discount_fork_test.go`:

```go
package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestAppendDiscountInfoRecordsMultiplierAndBaseGroupRatio(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.63)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
		GroupRatio: 0.63, BaseGroupRatio: 0.9, DiscountMultiplier: 0.7, DiscountSource: "model"}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.InDelta(t, 0.7, snap["discount_multiplier"], 1e-9)
	require.Equal(t, "model", snap["discount_source"])
	require.InDelta(t, 0.9, snap["group_ratio"], 1e-9)
}

func TestAppendDiscountInfo_FullDiscountKeepsBaseGroupRatio(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.0)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
		GroupRatio: 0, BaseGroupRatio: 1, DiscountMultiplier: 0, DiscountSource: "vendor"}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.InDelta(t, 0.0, snap["discount_multiplier"], 1e-9)
	require.Equal(t, "vendor", snap["discount_source"])
	require.InDelta(t, 1.0, snap["group_ratio"], 1e-9)
}

func TestAppendDiscountInfoNoDiscountWritesNothing(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.9)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.9, BaseGroupRatio: 0.9, DiscountMultiplier: 1}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.NotContains(t, snap, "discount_multiplier")
	require.InDelta(t, 0.9, snap["group_ratio"], 1e-9)
}
```

`PriceData`의 실제 타입명과 `RelayInfo.PriceData` 필드명은 `types/price_data.go`·`relay_info.go`에서 확인한다. Run: `GOWORK=off go test ./service/ -run AppendDiscountInfo -v` → FAIL.

- [ ] **Step 4: helper와 훅**

`service/log_discount_fork.go`:

```go
package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// appendDiscountInfo records the model/vendor discount separately from the
// group ratio so the usage log does not show the discount as a group discount.
// group_ratio is rewritten to the pre-discount base; a 0 multiplier (100%
// discount) is recorded as-is and never used as a divisor.
func appendDiscountInfo(other *model.LogOther, relayInfo *relaycommon.RelayInfo) {
	if other == nil || relayInfo == nil {
		return
	}
	g := relayInfo.PriceData.GroupRatioInfo
	if g.DiscountSource == "" || g.DiscountMultiplier == 1 {
		return
	}
	other.SetPublic("discount_multiplier", g.DiscountMultiplier)
	other.SetPublic("discount_source", g.DiscountSource)
	other.SetPublic("group_ratio", g.BaseGroupRatio)
}
```

`service/log_info_generate.go:109`(`user_group_ratio` 다음) 한 줄: `appendDiscountInfo(other, relayInfo) // fork: O6`. `GenerateClaudeOtherInfo`/`GenerateWssOtherInfo`/`GenerateAudioOtherInfo`는 모두 이 함수를 거치므로 추가 훅 없음. `GenerateMjOtherInfo`(`:323`)와 `taskBillingOther`(`task_billing.go:161`)는 `group_ratio`를 직접 쓰므로 그대로 둔다(MJ·태스크는 할인 항 미표시 — 기록 §4에 남긴다).

Run: Step 3 테스트 PASS, `GOWORK=off go test ./service/... ./relay/... ./controller/`.

- [ ] **Step 5: 커밋**

```bash
git add relay/helper/price.go relay/helper/price_discount.go relay/helper/price_discount_test.go service/quota.go service/task_billing.go service/log_info_generate.go service/log_discount_fork.go service/log_discount_fork_test.go
git commit -m "feat(pricing): fold the effective discount into the group ratio and log it separately

Ported-From: team relay/helper/price.go service/text_quota.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: O6 가격 페이지 할인 표시(프런트)

**Files:**
- Create: `web/src/features/pricing/lib/discount-color.ts`, `web/src/features/pricing/__tests__/discount.test.tsx`
- Modify: `web/src/features/pricing/types.ts:26,48`, `web/src/features/pricing/lib/price.ts:144,180,215,247`, `web/src/features/pricing/components/model-price-cell.tsx:194,207,223`, `web/src/features/pricing/components/model-card.tsx:229,249`, `web/src/features/pricing/components/pricing-sidebar.tsx:188-201`, `web/src/features/pricing/components/model-details.tsx:851,868,1356,1367`, 7 locale

**Interfaces:**
- Consumes: `/api/pricing`의 `data[].discount_percent`, `vendors[].discount_percent`(Task 1).
- Produces: `discountBadgeClasses(percent: number): string`, `formatPrice(..., discountPercent = 0)`(그리고 `formatGroupPrice`, `formatFixedPrice`, `formatRequestPrice`에 같은 마지막 인자), `DiscountBadge` 컴포넌트(`pricing/components/discount-badge.tsx`, `{ percent: number }` → `{percent}% off`).

- [ ] **Step 1: 테스트 작성(실패 확인)**

`web/src/features/pricing/__tests__/discount.test.tsx`:

```tsx
import { describe, expect, it } from 'vitest'
import { discountBadgeClasses } from '../lib/discount-color'
import { formatPrice } from '../lib/price'

describe('discount display', () => {
  it('maps percent ranges to heat colours', () => {
    expect(discountBadgeClasses(10)).toMatch(/emerald/)
    expect(discountBadgeClasses(20)).toMatch(/amber/)
    expect(discountBadgeClasses(40)).toMatch(/orange/)
    expect(discountBadgeClasses(60)).toMatch(/red/)
  })

  it('applies the discount percent to the formatted price', () => {
    const full = formatPrice(2, 1, 1)
    const discounted = formatPrice(2, 1, 1, 50)
    expect(discounted).not.toEqual(full)
  })
})
```

`formatPrice`의 실제 시그니처(`price.ts:144`)에 맞춰 인자를 채운다. Run: `cd web && bun run test -- pricing/__tests__/discount` → FAIL(모듈 없음).

- [ ] **Step 2: 구현**

```bash
git show team:web/default/src/features/pricing/lib/discount-color.ts > web/src/features/pricing/lib/discount-color.ts
```

AGPL 헤더를 1행에 둔다. `types.ts:26` `PricingVendor`와 `:48` `PricingModel`에 `discount_percent?: number`. `price.ts`의 `formatPrice`(`:144`)·`formatGroupPrice`(`:180`)·`formatFixedPrice`(`:215`)·`formatRequestPrice`(`:247`)에 마지막 선택 인자 `discountPercent = 0`을 추가하고 금액 계산 직후 `* (1 - discountPercent / 100)`을 곱한다(team `price.ts:190,230,303`과 같은 자리). `discount-badge.tsx`(새 파일):

```tsx
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { discountBadgeClasses } from '../lib/discount-color'

export function DiscountBadge(props: { percent?: number; className?: string }) {
  const { t } = useTranslation()
  if (!props.percent || props.percent <= 0) return null
  return (
    <Badge variant='outline' className={cn(discountBadgeClasses(props.percent), props.className)}>
      {t('{{percent}}% off', { percent: props.percent })}
    </Badge>
  )
}
```

`Badge` 경로는 `web/src/components/ui/`에서 확인한다. 표시 규칙(team `design.md` §4): 모델 카드(`model-card.tsx:229,249`)와 표 셀(`model-price-cell.tsx:194,207,223`)은 `discount_percent > 0`일 때 `<s>원가</s> 할인가` + 배지, 동적가격(billingexpr) 모델은 배지만; 벤더 사이드바(`pricing-sidebar.tsx:188-201`)의 벤더 항목에 `vendor.discount_percent` 배지; 모델 상세(`model-details.tsx:851,868,1356,1367`)의 기본가·그룹별 표에 할인가. i18n 키 `{{percent}}% off`를 7개 locale에 추가.

- [ ] **Step 3: 검증**

Run: `cd web && bun run test -- pricing && bun run typecheck && bunx oxlint -c .oxlintrc.json ./src/features/pricing && bunx oxfmt --check ./src/features/pricing` → 모두 통과. `bun run dev`로 `/pricing`을 열어 `ModelDiscount={"gpt-4":25}`를 저장한 뒤(Task 5 전이므로 `PUT /api/option/`로 직접) 배지·취소선이 보이는지 확인하고 스크린샷을 기록 문서용으로 `docs/discount/images/marketplace.png`에 둔다(Task 11에서 사용).

- [ ] **Step 4: 커밋**

```bash
git add web/src/features/pricing web/src/i18n/locales
git commit -m "feat(web): show model and vendor discounts on the pricing page

Ported-From: team web/default/src/features/pricing
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: O6 사용 로그 상세의 할인 행

**Files:**
- Modify: `web/src/features/usage-logs/types.ts:201-202`(`LogOtherData`), `web/src/features/usage-logs/components/dialogs/details-dialog.tsx:168-241`(`BillingBreakdown`), 7 locale
- Test: `web/src/features/usage-logs/__tests__/billing-breakdown-discount.test.tsx`

**Interfaces:**
- Consumes: `other.discount_multiplier`, `other.discount_source`, `other.group_ratio`(기저값) — Task 2.
- Produces: `BillingBreakdown` 행 `Model discount` / `Vendor discount` 값 `×0.7`.

- [ ] **Step 1: 테스트(실패 확인)**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { BillingBreakdown } from '../components/dialogs/details-dialog'

describe('BillingBreakdown discount row', () => {
  it('shows the model discount multiplier when present', () => {
    render(<BillingBreakdown other={{ group_ratio: 0.9, discount_multiplier: 0.7, discount_source: 'model' }} log={{} as never} />)
    expect(screen.getByText('Model discount')).toBeInTheDocument()
    expect(screen.getByText('×0.7')).toBeInTheDocument()
  })

  it('omits the row without a discount', () => {
    render(<BillingBreakdown other={{ group_ratio: 0.9 }} log={{} as never} />)
    expect(screen.queryByText('Model discount')).toBeNull()
    expect(screen.queryByText('Vendor discount')).toBeNull()
  })
})
```

`BillingBreakdown`이 export되지 않았거나 props 형태가 다르면(`details-dialog.tsx:168`) export하고 props에 맞춰 테스트를 고친다. i18n 테스트 설정은 `usage-logs/__tests__`의 기존 테스트가 쓰는 setup을 그대로 따른다. Run: `cd web && bun run test -- billing-breakdown-discount` → FAIL.

- [ ] **Step 2: 구현**

`types.ts:201` 근처 `LogOtherData`에 `discount_multiplier?: number`, `discount_source?: 'model' | 'vendor' | string`. `details-dialog.tsx:233-241`의 Group Ratio 행 다음에:

```tsx
if (other.discount_multiplier != null && other.discount_multiplier !== 1 && other.discount_source) {
  rows.push({
    label: other.discount_source === 'vendor' ? t('Vendor discount') : t('Model discount'),
    value: `×${other.discount_multiplier}`,
  })
}
```

(`rows`의 실제 구조에 맞춘다.) 키 `Model discount`, `Vendor discount`를 7개 locale에 추가. Run: Step 1 PASS, `bun run test -- usage-logs`, typecheck, 파일별 oxlint/oxfmt.

- [ ] **Step 3: 커밋**

```bash
git add web/src/features/usage-logs web/src/i18n/locales
git commit -m "feat(web): show the applied discount in the usage log billing breakdown

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: O6 관리자 할인 편집기(설정 → 결제 → 그룹 가격 탭)

**Files:**
- Create: `web/src/features/system-settings/models/model-discount-visual-editor.tsx`, `web/src/features/system-settings/models/vendor-discount-visual-editor.tsx`, `web/src/features/system-settings/models/__tests__/discount-editors.test.tsx`
- Modify: `web/src/features/system-settings/types.ts:261-267,301-307`, `web/src/features/system-settings/billing/index.tsx:55-61`, `web/src/features/system-settings/billing/section-registry.tsx:44-54`, `web/src/features/system-settings/models/ratio-settings-card.tsx:132-150,245-248,285-288,335-338,349-352,410-440`, `web/src/features/system-settings/models/group-ratio-form.tsx`, `web/src/features/system-settings/models/group-ratio-visual-editor.tsx:88,355-370`, 7 locale

**Interfaces:**
- Consumes: 옵션 키 `ModelDiscount`, `VendorDiscount`(JSON map 문자열, `PUT /api/option/`), `getVendors`(`models/api.ts:134`), `vendorsQueryKeys`(`models/lib/query-keys.ts:36`).
- Produces: `GroupSettingsSection`에 `'discounts'` 탭; `GroupFormValues.ModelDiscount/VendorDiscount: string`.

- [ ] **Step 1: 편집기 복사와 테스트(실패 확인)**

```bash
git show team:web/default/src/features/system-settings/models/model-discount-visual-editor.tsx > web/src/features/system-settings/models/model-discount-visual-editor.tsx
git show team:web/default/src/features/system-settings/models/vendor-discount-visual-editor.tsx > web/src/features/system-settings/models/vendor-discount-visual-editor.tsx
```

import 경로를 `web/src` 구조에 맞추고 AGPL 헤더를 1행에 둔다. 테스트 `discount-editors.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ModelDiscountVisualEditor } from '../model-discount-visual-editor'

describe('ModelDiscountVisualEditor', () => {
  it('emits the JSON map when a row is added', async () => {
    const onChange = vi.fn()
    render(<ModelDiscountVisualEditor value='{}' onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: 'Add model' }))
    await userEvent.type(screen.getByPlaceholderText('Model name'), 'gpt-4')
    await userEvent.type(screen.getByLabelText('Discount (%)'), '25')
    expect(onChange).toHaveBeenLastCalledWith(JSON.stringify({ 'gpt-4': 25 }))
  })

  it('rejects values outside 0-100', async () => {
    render(<ModelDiscountVisualEditor value='{"gpt-4":101}' onChange={vi.fn()} />)
    expect(screen.getByText(/0.*100/)).toBeInTheDocument()
  })
})
```

편집기의 실제 props·placeholder·label 문자열에 맞춘다(team 파일 참조). Run: `cd web && bun run test -- discount-editors` → FAIL(없는 컴포넌트/깨진 import).

- [ ] **Step 2: 폼·스키마·저장 연결**

- `system-settings/types.ts:261-267,301-307`: `ModelDiscount: string; VendorDiscount: string` 추가.
- `billing/index.tsx:55-61` 기본값 `ModelDiscount: '{}'`, `VendorDiscount: '{}'`; `section-registry.tsx:44-54` `getGroupDefaults`에 두 키.
- `ratio-settings-card.tsx`: `createGroupSchema`(`:132-150`)에 team `:196-225`와 같은 JSON-map 검증(`z.string().refine(isJsonMapOfNumbers0to100)`), 기본값·정규화(`:245-248,285-288,335-338,349-352`), `saveGroupRatios`(`:410-440`)의 diff 대상 키에 포함(기존 루프가 모든 키를 돌면 추가 작업 없음 — 확인해서 기록).
- `group-ratio-form.tsx`: `GroupFormValues`에 두 필드, JSON 모드에 두 `JsonCodeEditor` 필드.
- `group-ratio-visual-editor.tsx:88` `GroupSettingsSection`에 `'discounts'`, `:355-370` 탭 목록에 `{ value: 'discounts', label: t('Discounts') }`, 내용 패널에 `ModelDiscountVisualEditor`와 `VendorDiscountVisualEditor`.
- i18n: `Discounts`, `Model Discount`, `Vendor Discount`, `Add model`, `Add vendor`, `Discount (%)`, `Model name`, 편집기 설명 문구(team 파일의 `t()` 키 전부)를 7개 locale에.

Run: Step 1 PASS, `bun run test -- system-settings`, typecheck, 파일별 oxlint/oxfmt, `bun run build`.

- [ ] **Step 3: 브라우저 확인**

`bun run dev` + 백엔드로 `/system-settings/billing/group-pricing`의 "Discounts" 탭에서 `gpt-4=25`, 벤더 `OpenAI=10` 저장 → `/pricing`에서 배지 확인 → 요청 한 번 보낸 뒤 사용 로그 상세에 `Model discount ×0.75`. 스크린샷을 `docs/discount/images/admin-discount-editors.png`로 저장(Task 11).

- [ ] **Step 4: 커밋**

```bash
git add web/src/features/system-settings web/src/i18n/locales
git commit -m "feat(web): add the model and vendor discount editors to the group pricing settings

Ported-From: team web/default/src/features/system-settings/models
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: O7 타이밍 분해(백엔드)

**Files:**
- Create: `middleware/request_timing.go`, `common/timing_response_writer.go`, `common/timing_response_writer_test.go`, `relay/channel/timing_body.go`, `relay/channel/timing_body_test.go`, `service/log_timing_fork.go`, `service/log_timing_fork_test.go`, `relay/channel/api_request_timing_test.go`
- Modify: `constant/context_key.go:11`, `router/relay-router.go:15`(CORS 앞), `relay/common/relay_info.go:93`(필드)·`:999`(메서드), `relay/channel/api_request.go:563,564,568,581-583`, `service/log_info_generate.go:111`(`frt` 다음), `relay/common/relay_info_test.go`

**Interfaces:**
- Consumes: `common.SetContextKey/GetContextKeyTime`, `model.LogOther.SetPublic`, `relayInfo.FirstResponseTime` 패턴.
- Produces: `constant.ContextKeyGatewayEntryTime`, `middleware.RequestTiming() gin.HandlerFunc`, `common.TimingResponseWriter{gin.ResponseWriter}`, `common.GetLastWriteTime(c *gin.Context) time.Time`, `newTimingReadCloser(rc io.ReadCloser, onDone func()) *timingReadCloser`, `(*RelayInfo).SetUpstreamRequestStart()/SetUpstreamResponseEnd()`, `RelayInfo.UpstreamRequestStartTime/UpstreamResponseEndTime time.Time`, 로그 `other.e2e_ms/llm_ms/gateway_ms/pre_llm_ms/post_llm_ms`(int64 ms).

- [ ] **Step 1: team 파일 복사**

```bash
git show team:middleware/request_timing.go > middleware/request_timing.go
git show team:common/timing_response_writer.go > common/timing_response_writer.go
git show team:common/timing_response_writer_test.go > common/timing_response_writer_test.go
git show team:relay/channel/timing_body.go > relay/channel/timing_body.go
git show team:relay/channel/timing_body_test.go > relay/channel/timing_body_test.go
```

`constant/context_key.go:11` 뒤에 `ContextKeyGatewayEntryTime ContextKey = "gateway_entry_time" // fork: O7`. Run: `GOWORK=off go test ./common/ -run Timing && GOWORK=off go test ./relay/channel/ -run TimingReadCloser` → 7개 PASS.

- [ ] **Step 2: RelayInfo 필드·메서드와 테스트(실패 확인)**

`relay/common/relay_info_test.go`에 team의 세 테스트를 옮긴다(`git show team:relay/common/relay_info_test.go | sed -n 60,95p`): `TestRelayInfoSetUpstreamRequestStart`, `TestRelayInfoSetUpstreamResponseEnd`, `TestRelayInfoUpstreamTimestamps_LatestAttemptWins`. Run → 컴파일 실패. `relay_info.go:93` `isFirstResponse` 다음에:

```go
	UpstreamRequestStartTime time.Time // fork: O7 — set just before client.Do; latest attempt wins
	UpstreamResponseEndTime  time.Time // fork: O7 — set at body EOF/Close or on Do error
```

`:999` 근처(`HasSendResponse` 옆)에 team `relay_info.go:691-703`의 두 메서드를 그대로(`// fork: O7`). Run → PASS.

- [ ] **Step 3: doRequest 훅과 테스트**

`relay/channel/api_request_timing_test.go`(team `api_request_test.go:207-270`의 `TestDoRequest_RecordsUpstreamTimingOnSuccess/OnFailure`를 옮기되, develop의 `api_request_redirect_test.go`가 쓰는 `doRequest` 호출 방식·`httptest.Server`를 따른다). 단언: 성공 시 `info.UpstreamRequestStartTime`과 `UpstreamResponseEndTime`이 0이 아니고 End >= Start, 바디를 끝까지 읽기 전에는 End가 0; 실패 시 둘 다 설정. Run → FAIL. `api_request.go` `doRequest`:

```go
	info.SetUpstreamRequestStart() // fork: O7
	resp, err := relayClient.Do(req)
	if err != nil {
		info.SetUpstreamResponseEnd() // fork: O7
		...
	}
	if resp == nil {
		info.SetUpstreamResponseEnd() // fork: O7
		...
	}
	...(upID 블록 뒤, req.Body.Close 앞)
	resp.Body = newTimingReadCloser(resp.Body, info.SetUpstreamResponseEnd) // fork: O7
```

Run → PASS. `DoWssRequest`와 `doRequest`를 거치지 않는 어댑터(AWS SDK, coze 자체 doRequest, dify 업로드 등)는 `llm_ms` 없이 기록된다 — 기록 §4에 남긴다.

- [ ] **Step 4: 로그 helper와 테스트(실패 확인)**

`service/log_timing_fork_test.go`에 team `service/log_info_generate_test.go`의 4개 테스트를 옮기되 `other`가 `*model.LogOther`이므로 `snap := other.Snapshot()`에서 키를 읽고 `int64` 단언은 `snap["e2e_ms"].(int64)`로 한다. 추가:

```go
func TestAppendTimingBreakdown_NegativeGatewayFloorsToZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)
	_, _ = c.Writer.Write([]byte("partial")) // 클라이언트가 끊겨 마지막 Write가 먼저 일어남
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamRequestStartTime = entry
	info.UpstreamResponseEndTime = entry.Add(5 * time.Second) // 업스트림은 더 늦게 Close

	other := model.NewLogOther()
	appendTimingBreakdown(c, info, other)

	snap := other.Snapshot()
	require.Equal(t, int64(0), snap["gateway_ms"])
	require.GreaterOrEqual(t, snap["llm_ms"].(int64), int64(5000))
}
```

Run → 컴파일 실패. `service/log_timing_fork.go`: team `log_info_generate.go:36-88`의 `appendTimingBreakdown`을 옮기되 시그니처 `appendTimingBreakdown(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other *model.LogOther)`, 쓰기는 `other.SetPublic(key, value)`. 주석(재시도 한계 포함)도 가져온다. `service/log_info_generate.go:111`(`other.SetPublic("frt", ...)` 다음) 한 줄: `appendTimingBreakdown(ctx, relayInfo, other) // fork: O7`. Run → 5개 PASS.

- [ ] **Step 5: 라우터 훅과 엔진 전체 테스트**

`router/relay-router.go:15`(CORS 앞): `router.Use(middleware.RequestTiming()) // fork: O7`. `router/relay_router_timing_test.go`(team `relay-router_test.go:23` `TestRequestTimingAppliesEngineWide`를 옮김: 엔진에 `SetRelayRouter`를 적용하고 임의 relay 경로로 요청해 `c.Writer`가 `*common.TimingResponseWriter`이고 entry 키가 설정되는지 확인; 핸들러가 인증에서 막혀도 미들웨어는 돈다). `router/plugin-router.go:375`의 `gatedResponseWriter`가 바깥 writer를 감싸도 마지막 Write 추적이 유지되는지 주석으로 확인. Run: `GOWORK=off go test ./router/ ./middleware/ ./relay/... ./service/... ./common/`.

- [ ] **Step 6: 커밋**

```bash
git add constant/context_key.go middleware/request_timing.go common/timing_response_writer.go common/timing_response_writer_test.go relay/channel/timing_body.go relay/channel/timing_body_test.go relay/channel/api_request.go relay/channel/api_request_timing_test.go relay/common/relay_info.go relay/common/relay_info_test.go service/log_timing_fork.go service/log_timing_fork_test.go service/log_info_generate.go router/relay-router.go router/relay_router_timing_test.go
git commit -m "feat(relay): port the e2e/llm/gateway timing breakdown into usage logs

Ported-From: team middleware/request_timing.go common/timing_response_writer.go relay/channel/timing_body.go service/log_info_generate.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: O7 사용 로그 상세의 타이밍 섹션(프런트)

**Files:**
- Modify: `web/src/features/usage-logs/types.ts:218`(`frt` 옆), `web/src/features/usage-logs/components/dialogs/details-dialog.tsx:728-766`, `web/src/features/usage-logs/components/columns/common-logs-columns.tsx:802`, `web/src/features/usage-logs/components/common-log-mobile-card.tsx:208`, 7 locale
- Test: `web/src/features/usage-logs/__tests__/timing-breakdown.test.tsx`

**Interfaces:**
- Consumes: `other.e2e_ms/llm_ms/gateway_ms`(Task 6), `formatUseTime`, `DetailSection`/`DetailRow`(`./log-detail-layout`).
- Produces: 상세 다이얼로그의 `Timing Breakdown` 섹션(`Model Provider`, `Gateway Overhead (x.x%)`), 표·모바일 카드의 응답 시간이 `e2e_ms/1000` 우선.

- [ ] **Step 1: 테스트(실패 확인)**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DetailsDialog } from '../components/dialogs/details-dialog'

const baseLog = { id: 1, type: 2, use_time: 3, is_stream: false, other: '{"e2e_ms":3200,"llm_ms":2400,"gateway_ms":800,"frt":0}' }

describe('DetailsDialog timing breakdown', () => {
  it('renders provider and gateway rows with the gateway share', () => {
    render(<DetailsDialog log={baseLog as never} isAdmin={false} isRoot={false} open onOpenChange={() => {}} />)
    expect(screen.getByText('Timing Breakdown')).toBeInTheDocument()
    expect(screen.getByText('Model Provider')).toBeInTheDocument()
    expect(screen.getByText(/25\.0%/)).toBeInTheDocument()
  })

  it('hides the section when the keys are absent', () => {
    render(<DetailsDialog log={{ ...baseLog, other: '{}' } as never} isAdmin={false} isRoot={false} open onOpenChange={() => {}} />)
    expect(screen.queryByText('Timing Breakdown')).toBeNull()
  })
})
```

`UsageLog`에 필요한 최소 필드는 `usage-logs/__tests__`의 기존 다이얼로그 테스트(있다면)에서 가져온다. Run: `cd web && bun run test -- timing-breakdown` → FAIL.

- [ ] **Step 2: 구현**

`types.ts:218` 옆에 `e2e_ms?: number; llm_ms?: number; gateway_ms?: number`. `details-dialog.tsx`: `:728` 근처에 `const effectiveUseTime = other?.e2e_ms != null ? other.e2e_ms / 1000 : props.log.use_time`를 두고 Response Time 행의 `props.log.use_time`을 `effectiveUseTime`로; 그리드가 닫힌 뒤(`:764`)와 Request Conversion(`:766`) 사이에 team `details-dialog.tsx:834-853`의 `<DetailSection label={t('Timing Breakdown')}>` 블록(조건 `showTiming && e2e_ms != null && llm_ms != null && gateway_ms != null`). `common-logs-columns.tsx:802`와 `common-log-mobile-card.tsx:208`의 `useTimeSec`를 `other?.e2e_ms != null ? other.e2e_ms / 1000 : log.use_time`로(두 파일 모두 `parseLogOther`가 이미 있는지 확인, 없으면 `lib/format.ts:162` 사용). i18n 키 `Timing Breakdown`, `Model Provider`, `Gateway Overhead` 7개 locale.

Run: Step 1 PASS, `bun run test -- usage-logs`, typecheck, 파일별 oxlint/oxfmt. 조직 로그 화면은 같은 `DetailsDialog`를 쓰므로 추가 변경 없음(브라우저에서 `/organization/usage-logs/common` 상세로 한 번 확인).

- [ ] **Step 3: 커밋**

```bash
git add web/src/features/usage-logs web/src/i18n/locales
git commit -m "feat(web): show the timing breakdown in the usage log details

Ported-From: team web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: 이월 I2 — 조직 멤버 할당의 동의 규칙(결정 6)

**결정(이 계획의 기본값, spec §9 결정 6):** 초대·수락 흐름은 만들지 않는다. 대신 (a) 조직 관리자는 미소속 사용자를 **정확한 username 또는 email로만 조회**할 수 있고 목록 조회는 없앤다, (b) **개인 잔액이 있거나(`users.quota > 0`) 활성 개인 구독이 있는 사용자는 할당을 거부**한다(API·Excel 가져오기 동일), (c) 조회 응답에서 `quota`, `used_quota`, `remark`를 뺀다. 사용자는 먼저 자기 잔액을 다 쓰거나 운영자가 정리한 뒤에만 조직에 들어온다.

**Files:**
- Create: `controller/organization_member_lookup.go`, `controller/organization_member_lookup_test.go`
- Modify: `controller/organization.go:676-708`(`ListAssignableOrganizationUsers` → lookup으로 교체), `controller/organization.go:809`(`AssignOrganizationUser`), `model/organization.go:254`(`AssignUserToOrganization`), `service/org_user_export.go:241,300`, `web/src/features/organizations/components/organization-users-table.tsx`(할당 다이얼로그), `web/src/features/organizations/api.ts`, `docs/openapi/organization-rest-api.md`

**Interfaces:**
- Consumes: `model.GetUserOrganizationId`, `UserSubscription{UserId, Status, EndTime}`(`model/subscription.go:255-263`, status `"active"`).
- Produces: `GET /api/organization/users/lookup?username=<exact>|email=<exact>` → `{id, username, display_name, organization_id}` 또는 404; `model.ErrOrganizationAssignHasPersonalBalance = common.NewMessage("user still has a personal balance or subscription; it must be cleared before joining an organization")`; `model.HasActiveUserSubscription(userId int) (bool, error)`.

- [ ] **Step 1: 테스트(실패 확인)** — `controller/organization_member_lookup_test.go`. 하네스는 `controller/organization_security_test.go:29`의 `setupOrgSecurityTest(t) *orgSecurityFixture`(필드 `db, org, owner, admin, member`)와 `:76`의 `orgSecurityRequest(handler gin.HandlerFunc, actor model.User, method, path, body string, params ...gin.Param) *httptest.ResponseRecorder`를 쓴다(핸들러 직접 호출, 라우터 없음). 미소속 사용자는 `f.db.Create(&model.User{Username: "alice-free", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})`로 만든다.

```go
func TestOrganizationMemberLookupIsExactMatchOnly(t *testing.T) {
	f := setupOrgSecurityTest(t)
	free := model.User{Username: "alice-free", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&free).Error)
	require.NoError(t, f.db.Create(&model.User{Username: "alice-two", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)

	resp := orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?username=alice", "")
	require.Equal(t, http.StatusNotFound, resp.Code)

	resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?username=alice-free", "")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"username":"alice-free"`)
	require.NotContains(t, resp.Body.String(), `"quota"`)
	require.NotContains(t, resp.Body.String(), `"remark"`)
}

func TestAssignRefusesUserWithPersonalBalance(t *testing.T) {
	f := setupOrgSecurityTest(t)
	rich := model.User{Username: "rich", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000}
	require.NoError(t, f.db.Create(&rich).Error)

	resp := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", rich.Id), `{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(rich.Id)})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "personal balance")
	orgId, err := model.GetUserOrganizationId(rich.Id)
	require.NoError(t, err)
	require.Equal(t, 0, orgId)
}

func TestAssignRefusesUserWithActiveSubscription(t *testing.T) {
	f := setupOrgSecurityTest(t)
	sub := model.User{Username: "subscriber", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&sub).Error)
	require.NoError(t, f.db.Create(&model.UserSubscription{UserId: sub.Id, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}).Error)

	resp := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", sub.Id), `{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(sub.Id)})
	require.Contains(t, resp.Body.String(), "personal balance")
}
```

라우트 변경은 `router/organization_router_test.go:42`(`require.Equal(t, 38, len(registered))`)에 `require.Contains(t, registered, "GET /api/organization/users/lookup")`와 `require.NotContains(t, registered, "GET /api/organization/users/assignable")`를 더한다(`registered`의 실제 키 형식을 그 파일에서 확인). Excel: `service/org_user_export_test.go`에 "잔액 있는 미소속 사용자 행은 오류 행"을 추가. Run → FAIL.

- [ ] **Step 2: 구현**

`model/organization.go` `AssignUserToOrganization` 트랜잭션 안, 사용자 잠금 직후:

```go
		if user.OrganizationId == 0 { // joining from outside: personal balance must be clear
			if user.Quota > 0 {
				return ErrOrganizationAssignHasPersonalBalance
			}
			active, err := hasActiveUserSubscriptionTx(tx, userID)
			if err != nil {
				return err
			}
			if active {
				return ErrOrganizationAssignHasPersonalBalance
			}
		}
```

`hasActiveUserSubscriptionTx(tx, userId)`는 `user_subscriptions`에서 `user_id = ? AND status = 'active' AND end_time > ?`를 센다(`model/subscription.go:891` 쿼리 참고). `controller/organization_member_lookup.go`에 `LookupOrganizationMember(c)`: 관리자 권한 확인(`ListAssignableOrganizationUsers`와 같은 가드), `username` 또는 `email` 중 하나 필수, `role < RoleAdminUser AND (organization_id = 0 OR organization_id = actor.OrganizationId)` 정확 일치 1건, 응답 `{id, username, display_name, organization_id}`; 없으면 404 `common.ApiErrorT(c, "user not found")`. `router/organization-router.go`: `GET /users/assignable` 제거, `GET /users/lookup` 추가(세션 전용 — `middleware/fork_access_token_routes.go`와 `router/access_token_scope_test.go` 확인), 라우트 수 단언 38 유지(하나 빼고 하나 더함). `service/org_user_export.go:241,300`의 `AssignUserToOrganization` 오류는 그 행의 오류로 보고한다(이미 그렇다면 확인만). 프런트: 할당 다이얼로그의 목록을 "username 또는 email 입력 → 조회 → 1건 표시 → 역할 선택 → 추가"로 바꾸고 `api.ts`에 `lookupOrganizationMember({username?, email?})`; `getAssignableOrganizationUsers` 삭제; vitest 1건(조회 후 추가 호출). i18n 키 `Look up user`, `Username or email`, `User not found`, `This user still has a personal balance or subscription`.

Run: Step 1 PASS, `GOWORK=off go test ./controller/ ./model/ ./service/... ./router/`, 프런트 게이트. `docs/openapi/organization-rest-api.md`의 assignable 절을 lookup으로 바꾸고 거부 규칙을 적는다.

- [ ] **Step 3: 커밋**

```bash
git add controller/organization.go controller/organization_member_lookup.go controller/organization_member_lookup_test.go model/organization.go service/org_user_export.go service/org_user_export_test.go router/organization-router.go middleware/fork_access_token_routes.go router/access_token_scope_test.go web/src/features/organizations web/src/i18n/locales docs/openapi/organization-rest-api.md
git commit -m "feat(organization): require a clear personal balance to join and replace the assignable list with an exact lookup

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: 이월 I7 — 조직 범위 멤버 상태와 메모(결정 7)

**결정(이 계획의 기본값, spec §9 결정 7):** 조직 관리자는 플랫폼 `users.status`·`users.remark`를 더 이상 쓰지 않는다. 대신 fork 컬럼 `users.organization_member_status`(1 active / 2 suspended)와 `users.organization_remark`(varchar 255)를 쓴다. 정지된 멤버는 relay·MJ가 403, 본인 읽기(`/wallet` 요약, `subscription/self`, `topup/self`)는 허용. 탈퇴 시 두 컬럼을 초기화한다.

**Files:**
- Create: `model/organization_member_status.go`, `model/organization_member_status_test.go`, `controller/organization_member_status_test.go`
- Modify: `model/user.go:102`(필드 2줄, `// fork: O3`), `model/organization.go`(`clearOrganizationMembershipTx` 초기화, `OrganizationMemberStatus*` 상수), `controller/organization.go:28-32`(요청 구조체), `:733-808`(`UpdateOrganizationUser`), `:44-72`(view 컬럼: `remark` → `organization_remark`, `member_status` 추가), `service/org_user_export.go:37,74,213-217,285-286`(status/remark 열 → 조직 범위), `service/billing_session_organization.go`(정지 검사), `service/midjourney_organization.go:15`(정지 검사), `web/src/features/organizations/{types.ts,components/organization-users-table.tsx}`, `docs/openapi/organization-rest-api.md`, `docs/manuals/organization-management-manual.md`

**Interfaces:**
- Produces: `model.OrganizationMemberStatusActive = 1`, `OrganizationMemberStatusSuspended = 2`; `model.ErrOrganizationMemberSuspended = common.NewMessage("organization membership is suspended")`; `model.RequireActiveOrganizationMember(userId int) error`(org_id>0이고 status==2면 오류); `organizationUserView.MemberStatus int \`json:"member_status"\``, `OrganizationRemark string \`json:"organization_remark"\``; PATCH 본문 `{"member_status":2}`, `{"organization_remark":"..."}`(기존 `status`/`remark` 키는 `DecodeJsonStrict`가 거부).

- [ ] **Step 1: 테스트(실패 확인)**

`controller/organization_member_status_test.go`:

같은 하네스(`setupOrgSecurityTest`, `orgSecurityRequest`)를 쓴다.

```go
func TestSuspendedOrganizationMemberRelayRejectedButSelfReadsAllowed(t *testing.T) {
	f := setupOrgSecurityTest(t)
	idParam := gin.Param{Key: "id", Value: strconv.Itoa(f.member.Id)}

	resp := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"member_status":2}`, idParam)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"success":true`)

	fresh, err := model.GetUserById(f.member.Id, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, fresh.Status) // 플랫폼 상태는 그대로
	require.Equal(t, model.OrganizationMemberStatusSuspended, fresh.OrganizationMemberStatus)

	require.ErrorIs(t, model.RequireActiveOrganizationMember(f.member.Id), model.ErrOrganizationMemberSuspended)

	resp = orgSecurityRequest(GetMyOrganizationSubscription, f.member, http.MethodGet, "/api/organization/subscription/self", "")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"success":true`)
}

func TestOrganizationAdminCannotWritePlatformStatusOrRemark(t *testing.T) {
	f := setupOrgSecurityTest(t)
	idParam := gin.Param{Key: "id", Value: strconv.Itoa(f.member.Id)}
	resp := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"status":1}`, idParam)
	require.Contains(t, resp.Body.String(), "unsupported field")
	resp = orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"remark":"x"}`, idParam)
	require.Contains(t, resp.Body.String(), "unsupported field")
}

func TestClearMembershipResetsMemberStatusAndRemark(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", f.member.Id).
		Updates(map[string]any{"organization_member_status": 2, "organization_remark": "late"}).Error)
	require.NoError(t, model.ClearOrganizationMembership(f.member.Id, f.org.Id))
	fresh, err := model.GetUserById(f.member.Id, false)
	require.NoError(t, err)
	require.Equal(t, model.OrganizationMemberStatusActive, fresh.OrganizationMemberStatus)
	require.Equal(t, "", fresh.OrganizationRemark)
}
```

`GetMyOrganizationSubscription`은 `router/organization-router.go:47`의 `subscription/self` 핸들러다.

`service/billing_session_organization_test.go`(기존 파일이 있으면 그 안에): 정지 멤버의 `NewBillingSession`이 403 `NewAPIError`를 돌려주는 테스트. Run → 컴파일 실패.

- [ ] **Step 2: 구현**

`model/user.go:102` 뒤:

```go
	OrganizationMemberStatus int    `json:"organization_member_status" gorm:"type:int;default:1;column:organization_member_status"` // fork: O3
	OrganizationRemark       string `json:"organization_remark" gorm:"type:varchar(255);default:'';column:organization_remark"`   // fork: O3
```

(AutoMigrate가 세 DB에 ADD COLUMN; `GetSelfUserById`의 Select 목록(`user.go:538`)에는 넣지 않는다 — 본인 화면에 필요하면 `buildSelfUserData` 훅에 추가.) `model/organization_member_status.go`에 상수·오류·`RequireActiveOrganizationMember(userId)`(`Select("organization_id","organization_member_status")` 1회 읽기). `clearOrganizationMembershipTx`의 Updates 맵에 `"organization_member_status": OrganizationMemberStatusActive, "organization_remark": ""`. 컨트롤러: `updateOrganizationUserRequest`를 `{MemberStatus *int; Quota *int64; OrganizationRemark *string}`로, `UpdateOrganizationUser`에서 `member_status`는 1/2만, `organization_remark`는 `trimAndValidateText(..., MaxOrganizationRemarkLength)`; view 컬럼에서 `remark` 제거, `organization_member_status`, `organization_remark` 추가. Excel: 헤더 `status`/`remark` 열을 `member_status`(`active`/`suspended`)와 `organization_remark`로 바꾸고 플랫폼 필드는 쓰지 않는다(새 사용자 생성 시 플랫폼 status는 기본 enabled). 과금: `service/billing_session_organization.go`의 `requireActiveOrganization` 직후에 `model.RequireActiveOrganizationMember(relayInfo.UserId)` → 403(같은 `NewErrorWithStatusCode` 패턴); `CheckMidjourneyOrganizationBilling`에도 같은 검사. 프런트: `OrganizationUser`에 `organization_member_status`, `organization_remark`; 사용자 표의 상태 토글이 `member_status`를 보내고 라벨은 `Active`/`Suspended`; 메모 열은 `organization_remark`. i18n 키 `Suspended`, `Suspend`, `Resume`, `Organization note`. 매뉴얼·OpenAPI의 status/remark 절을 고친다.

Run: Step 1 PASS, Go 전체(`./controller/ ./model/ ./service/... ./router/`), 프런트 게이트, `cd web && bun run e2e`(상태 토글 시나리오가 있으면 기대값 갱신).

- [ ] **Step 3: 커밋**

```bash
git add model/user.go model/organization.go model/organization_member_status.go model/organization_member_status_test.go controller/organization.go controller/organization_member_status_test.go service/org_user_export.go service/org_user_export_test.go service/billing_session_organization.go service/billing_session_organization_test.go service/midjourney_organization.go web/src/features/organizations web/src/i18n/locales web/e2e docs/openapi/organization-rest-api.md docs/manuals/organization-management-manual.md
git commit -m "feat(organization): give organization admins an org-scoped member status and note instead of platform fields

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: 이월 소형 강화 묶음(M6·M10·WSS·LIKE·가져오기 트랜잭션·dead code·버튼)

**Files:**
- Create: `controller/organization_topup_admin.go`, `controller/organization_topup_admin_test.go`
- Modify: `router/organization-router.go`(root 라우트 1개 추가 → 39), `service/authz/resources_organization.go`, `controller/user.go:416`(`TransferAffQuota`), `controller/checkin.go:43`(`DoCheckin`), `service/quota.go:88`(`PreWssConsumeQuota`), `controller/organization.go:629-648`(keyword), `service/org_user_export.go:295-300`(생성+배정), `controller/organization_validation.go:23,98`(삭제), `web/src/features/organizations/components/organization-users-table.tsx:885`, `docs/openapi/organization-rest-api.md`

**Interfaces:**
- Produces: `POST /api/organizations/:id/topups/expire`(root) → pending 조직 주문을 `expired`로, 응답 `{expired: n}`, 감사 `organization.topup.expire`; `model.ExpirePendingOrganizationTopUps(organizationId int) (int64, error)`; aff/checkin은 조직 멤버에게 `ErrOrganizationMemberPersonalTopUp` 재사용.

- [ ] **Step 1: 테스트(실패 확인)**

`controller/organization_topup_admin_test.go`: pending 조직 주문 2건 + success 1건을 넣고 root가 expire 호출 → `expired: 2`, success는 그대로, 이어서 `DELETE /api/organizations/:id` 성공. `controller/organization_personal_wallet_test.go`(기존 C1 테스트 파일에 추가): 조직 멤버의 `POST /api/user/aff_transfer`와 `POST /api/user/checkin`이 `organization members cannot top up their personal balance` 메시지로 거부되고 quota 불변. `service/quota_org_test.go`: 조직 멤버 `PreWssConsumeQuota`가 조직 지갑 < 예약액이면 오류. `controller/organization_test.go`: keyword `50%_x`가 LIKE 와일드카드로 해석되지 않음(`sanitizeLikePattern` 사용 후 `ESCAPE '!'`). `service/org_user_export_test.go`: 새 사용자 행에서 배정이 실패하면 사용자도 남지 않음. Run → FAIL.

- [ ] **Step 2: 구현**

- `model/topup_target.go`에 `ExpirePendingOrganizationTopUps`: `UPDATE topups SET status = 'expired' WHERE target_type = ? AND target_id = ? AND status = 'pending'`(GORM `Updates`, `RowsAffected` 반환). `controller/organization_topup_admin.go`에 핸들러 + `recordOrganizationAudit`. 라우트는 `/api/organizations` 그룹(RootAuth)에 추가, `resources_organization.go`의 casbin 범위에 포함, 라우트 수 단언 39로 갱신.
- `controller/user.go:416` `TransferAffQuota`: `GetUserById` 다음에 `if err := model.RejectOrganizationMemberPersonalTopUp(id); err != nil { common.ApiError(c, err); return } // fork: O3`(`model/organization_personal_wallet.go:52`의 함수를 export). `controller/checkin.go:43` `DoCheckin`: `UserCheckin` 전에 같은 한 줄.
- `service/quota.go:88` `PreWssConsumeQuota`: 사용자 잔액 검사 다음에 `if err := requireOrganizationWalletFor(userId, quota); err != nil { return err } // fork: O3`(fork 파일 `service/quota_organization.go`: org_id>0이면 `model.GetOrganizationQuota(orgId) >= quota`, 아니면 `service/funding_source_organization.go:82`의 `ErrInsufficientOrganizationWalletQuota`를 돌려준다).
- `controller/organization.go:629-648`: `keyword`를 `model.SanitizeOrganizationKeyword`(`model/token.go:122` `sanitizeLikePattern`과 같은 규칙을 fork 파일에 복제: `!`→`!!`, `_`→`!_`, `%`→`!%`)로 바꾸고 쿼리에 `ESCAPE '!'`(`model/topup_target.go:141` 패턴).
- `service/org_user_export.go:295-300`: `newUser.Insert`와 `AssignUserToOrganization`을 하나의 `model.DB.Transaction`으로 감싸는 `model.CreateUserInOrganization(user *User, organizationId int, role string) error`를 `model/organization.go`에 추가하고 호출.
- `controller/organization_validation.go`의 `MaxTopUpAmount`, `rejectTopUpAmountTooLarge` 삭제(사용처 없음 확인).
- 프런트 `organization-users-table.tsx:885`: 버튼을 `isOrganizationOwner`일 때만 렌더.
- OpenAPI: expire 엔드포인트, aff/checkin 거부, keyword 이스케이프 규칙.

Run: Step 1 PASS, Go 전체, 프런트 게이트.

- [ ] **Step 3: 커밋**

```bash
git add model/topup_target.go model/organization.go model/organization_personal_wallet.go controller/organization_topup_admin.go controller/organization_topup_admin_test.go controller/organization.go controller/organization_test.go controller/organization_validation.go controller/organization_personal_wallet_test.go controller/user.go controller/checkin.go service/quota.go service/quota_organization.go service/quota_org_test.go service/org_user_export.go service/org_user_export_test.go router/organization-router.go service/authz/resources_organization.go web/src/features/organizations docs/openapi/organization-rest-api.md
git commit -m "fix(organization): expire abandoned org top-ups, fence affiliate/check-in inflows, escape member search, and tidy

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: 문서·기록·전체 검증·병합 리허설

**Files:**
- Create: `docs/discount/admin-guide.md`, `docs/discount/design.md`, `docs/discount/images/{admin-discount-editors,marketplace}.png`, `docs/2026-10-14-rebase-phase-c.md`
- Modify: `docs/fork-overlays.md`(O6·O7 행, O3 추가 행), `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §9(결정 6·7을 확정으로), `docs/manuals/organization-management-manual.md`(Task 8·9 반영 확인)

- [ ] **Step 1: 할인 문서 이식·정정**

```bash
git checkout team -- docs/discount/admin-guide.md docs/discount/design.md
```

정정: 경로 `web/default/src` → `web/src`; "Show formula" 절을 upstream의 `BillingBreakdown` 행 표시(`Model discount ×0.7`)로; 설정 위치를 "설정 → 결제 → 그룹 가격 → Discounts 탭, 저장 버튼 `Save group settings`"로; 파일 맵을 이 계획의 파일 구조로. 이미지는 Task 3·5에서 찍은 스크린샷으로 교체. 타이밍은 별도 가이드 없이 기록 §2에 "로그 상세의 Timing Breakdown" 한 절.

- [ ] **Step 2: 전체 검증**

```bash
GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)" ; cd web && bun run typecheck && bun run test 2>&1 | grep -E "Test Files|Tests " && bun run build 2>&1 | tail -1 && bun run e2e 2>&1 | tail -3; cd ..
```

Expected: FAIL 없음, vitest 전부 통과, 빌드 성공, e2e 19 통과(Task 9가 상태 토글 시나리오를 바꿨다면 갱신된 수).

- [ ] **Step 3: 할인 가이드 시나리오 수동 실행(spec §8 단계 C 산출물)**

로컬 SQLite 서버에서 admin-guide §5 빠른 시작(gpt-4 25%, OpenAI 10% → 마켓플레이스 배지 색 → 요청 → 로그 상세 `Model discount ×0.75`/`Vendor discount ×0.9` → 100% 할인 모델은 과금 0 + group_ratio 기저값) 과 타이밍(상세의 Timing Breakdown, 재시도 요청 1건)을 돌려 결과 표를 기록 §2에 남긴다. 결제 provider 호출은 없다.

- [ ] **Step 4: upstream 병합 리허설**

```bash
git fetch qn && git merge --no-commit --no-ff qn/main 2>&1 | tail -3; git diff --name-only --diff-filter=U; git merge --abort 2>/dev/null || true; git status --short | wc -l
```

충돌 파일이 `docs/fork-overlays.md`의 upstream 파일 집합 안에 있는지 확인(upstream이 움직이지 않았으면 "Already up to date"를 그대로 적고 미검증으로 표시).

- [ ] **Step 5: 기록과 overlay 표**

`docs/2026-10-14-rebase-phase-c.md`: §1 O6, §2 O7, §3 이월(결정 6·7 확정 내용, Task 10 항목), §4 결정 사항(실행 중 ruling 포함), §5 단계 D로 넘기는 것(할인·타이밍 i18n 번역, MJ·태스크 로그의 할인 항, `doRequest` 우회 어댑터의 `llm_ms` 부재, MySQL/PostgreSQL 미실행), §6 검증. `docs/fork-overlays.md`: O6 행(`model/option.go`, `model/pricing.go`, `types/price_data.go`, `relay/helper/price.go`, `service/quota.go`, `service/task_billing.go`, `service/log_info_generate.go`, 프런트 수정 파일), O7 행(`constant/context_key.go`, `router/relay-router.go`, `relay/common/relay_info.go`, `relay/channel/api_request.go`, `service/log_info_generate.go`, 프런트 4파일), O3 추가 행(`model/user.go` 필드 2줄, `controller/user.go`, `controller/checkin.go`, `service/quota.go`); `grep -rn "fork: O" --include='*.go' --include='*.ts' --include='*.tsx' . | grep -v node_modules`로 줄 번호 검증. spec §9 결정 6·7을 "2026-10-XX 확정: …"으로 바꾼다.

- [ ] **Step 6: 커밋**

```bash
git add docs/
git commit -m "docs: record phase C of the upstream rebase and port the discount guide

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

## 계획 결정 사항

1. O6 로그는 team의 `text_quota.go` 훅 대신 `GenerateTextOtherInfo` 한 지점에 두어 Claude/WSS/Audio를 한 번에 덮는다. MJ·태스크 로그는 `group_ratio`를 직접 쓰므로 할인 항을 표시하지 않는다(단계 D 이후).
2. `GroupRatioInfo.BaseGroupRatio`를 추가해 로그의 기저 그룹 배율을 나눗셈 없이 기록한다(100% 할인 안전).
3. 할인 fold는 `relayInfo.GetBillingModelName()`(upstream이 `BillingModelName`을 먼저 해석) 기준이다. team은 `OriginModelName`이었다.
4. O7 `appendTimingBreakdown`은 `*model.LogOther.SetPublic`으로 쓴다(사용자·조직 사용자에게 보임). `pre_llm_ms`/`post_llm_ms`는 저장만 하고 UI 미노출(team과 같음).
5. `doRequest`를 거치지 않는 어댑터(AWS SDK, coze, dify 업로드 등)는 `llm_ms` 없이 `e2e_ms`만 기록된다(team과 같음).
6. 결정 6(I2): 초대 흐름 없이 "정확 조회 + 잔액·구독 있으면 거부". 7(I7): 조직 범위 `organization_member_status`·`organization_remark` 컬럼. 둘 다 계획 리뷰에서 사용자가 바꾸면 Task 8·9만 수정한다.
7. M7의 레거시 MJ 가드는 가격이 호출 전에 알려지지 않아 `> 0` 검사를 유지한다(기록에 남김). WSS는 조직 지갑 검사를 추가한다.
8. M11 casbin `organization` 리소스는 root 전용으로 문서화만 한다(코드 변경 없음).
9. Excel 열 이름 변경(`status`→`member_status`, `remark`→`organization_remark`)은 단계 B 내보내기 파일과 호환되지 않는다. 가져오기는 옛 열이 있으면 "지원하지 않는 열" 오류로 안내한다.
10. 할인 편집기는 upstream `GroupRatioVisualEditor`의 탭(`discounts`)으로 넣는다(team의 평면 카드 구조는 없음).
