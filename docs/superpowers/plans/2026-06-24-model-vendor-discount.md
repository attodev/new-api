# 모델별 / 벤더별 할인 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 모델별·벤더별 할인율(%)을 별도 레이어로 추가하고(모델 우선, 벤더 fallback), 모든 과금 경로(토큰/정액/expr)에 사전차감==정산으로 일관 적용한다.

**Architecture:** 새 옵션 `ModelDiscount`/`VendorDiscount`(모델명/벤더명 → 할인율%)를 `setting/ratio_setting`에 추가. `GetEffectiveDiscountMultiplier(model)`가 모델 우선·벤더 fallback으로 배수(`(100-%)/100`)를 반환. 배수를 `ModelPriceHelper`에서 `groupRatioInfo.GroupRatio`에 1회 접어 넣어 메인 경로(사전차감+텍스트 정산+expr)를 커버하고, 재계산 경로(wss/audio/task/tool)에는 동일 함수를 호출해 적용. 모델→벤더는 캐시로 해결.

**Tech Stack:** Go 1.22+ (GORM, `types.RWMap`), React 19 + Rsbuild 프론트엔드, 옵션은 `options` 테이블 JSON.

**스코프 결정 (중요):** 할인은 **GroupRatio와 동일한 적용 범위**를 갖는다 — 즉 모델의 토큰/완성/캐시/오디오/이미지/정액/expr 비용에 적용되며, GroupRatio가 곱해지는 곳에 함께 곱해진다. (모델·벤더 동시 적용 안 함, 모델 우선.)

---

## File Structure

- Create: `setting/ratio_setting/model_discount.go` — 할인 맵/접근자/검증/유효배수 + 모델→벤더 캐시.
- Create: `setting/ratio_setting/model_discount_test.go` — 단위 테스트.
- Modify: `model/option.go` — OptionMap 기본 등록 + 업데이트 switch.
- Modify: `controller/option.go` — 관리 옵션 키 목록.
- Modify: `relay/helper/price.go` — `ModelPriceHelper`에서 할인 배수 fold-in(메인 경로).
- Modify: `service/quota.go`, `service/task_billing.go`, `service/tool_billing.go` — 재계산 경로에 할인 적용.
- Create: `web/default/src/features/system-settings/models/model-discount-visual-editor.tsx`
- Create: `web/default/src/features/system-settings/models/vendor-discount-visual-editor.tsx`
- Modify: 시스템 설정 모델 섹션 컨테이너 — 두 에디터 노출.

> 참고: Go 측은 `go test ./setting/ratio_setting/...`로 검증. 빌드는 `go build ./...`. 프론트는 `bunx tsc --noEmit`.

---

### Task 1: 할인 설정 레이어 (`model_discount.go`)

**Files:**
- Create: `setting/ratio_setting/model_discount.go`
- Test: `setting/ratio_setting/model_discount_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`setting/ratio_setting/model_discount_test.go`:
```go
package ratio_setting

import "testing"

func TestModelDiscountMultiplier(t *testing.T) {
	if err := UpdateModelDiscountByJSONString(`{"gpt-4":20}`); err != nil {
		t.Fatalf("update model discount: %v", err)
	}
	mul, ok := GetModelDiscountMultiplier("gpt-4")
	if !ok || mul != 0.8 {
		t.Fatalf("want 0.8,true got %v,%v", mul, ok)
	}
	if _, ok := GetModelDiscountMultiplier("unknown-model"); ok {
		t.Fatalf("unknown model should have no discount")
	}
}

func TestVendorDiscountMultiplier(t *testing.T) {
	if err := UpdateVendorDiscountByJSONString(`{"openai":10}`); err != nil {
		t.Fatalf("update vendor discount: %v", err)
	}
	mul, ok := GetVendorDiscountMultiplier("openai")
	if !ok || mul != 0.9 {
		t.Fatalf("want 0.9,true got %v,%v", mul, ok)
	}
}

func TestDiscountValidationRejectsOutOfRange(t *testing.T) {
	if err := UpdateModelDiscountByJSONString(`{"x":-1}`); err == nil {
		t.Fatalf("negative percent must be rejected")
	}
	if err := UpdateModelDiscountByJSONString(`{"x":101}`); err == nil {
		t.Fatalf(">100 percent must be rejected")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run 'TestModelDiscount|TestVendorDiscount|TestDiscountValidation' -v`
Expected: 컴파일 에러(미정의 함수) 또는 FAIL.

- [ ] **Step 3: 구현 작성**

`setting/ratio_setting/model_discount.go`:
```go
package ratio_setting

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// 모델명/벤더명 -> 할인율 percent(0~100). 내부 배수 = (100-percent)/100.
var modelDiscountMap = types.NewRWMap[string, float64]()
var vendorDiscountMap = types.NewRWMap[string, float64]()

func ModelDiscount2JSONString() string  { return modelDiscountMap.MarshalJSONString() }
func VendorDiscount2JSONString() string { return vendorDiscountMap.MarshalJSONString() }

func GetModelDiscountCopy() map[string]float64  { return modelDiscountMap.ReadAll() }
func GetVendorDiscountCopy() map[string]float64 { return vendorDiscountMap.ReadAll() }

func validateDiscountJSON(jsonStr string) error {
	m := make(map[string]float64)
	if err := common.UnmarshalJsonStr(jsonStr, &m); err != nil {
		return err
	}
	for name, percent := range m {
		if percent < 0 || percent > 100 {
			return errors.New("discount percent for '" + name + "' must be between 0 and 100")
		}
	}
	return nil
}

func UpdateModelDiscountByJSONString(jsonStr string) error {
	if err := validateDiscountJSON(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonString(modelDiscountMap, jsonStr)
}

func UpdateVendorDiscountByJSONString(jsonStr string) error {
	if err := validateDiscountJSON(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonString(vendorDiscountMap, jsonStr)
}

func percentToMultiplier(percent float64) float64 {
	return (100 - percent) / 100
}

// GetModelDiscountMultiplier: 모델명에 할인이 설정돼 있으면 (배수, true).
func GetModelDiscountMultiplier(modelName string) (float64, bool) {
	percent, ok := modelDiscountMap.Get(FormatMatchingModelName(modelName))
	if !ok {
		return 1, false
	}
	return percentToMultiplier(percent), true
}

// GetVendorDiscountMultiplier: 벤더명에 할인이 설정돼 있으면 (배수, true).
func GetVendorDiscountMultiplier(vendorName string) (float64, bool) {
	percent, ok := vendorDiscountMap.Get(vendorName)
	if !ok {
		return 1, false
	}
	return percentToMultiplier(percent), true
}
```

(`FormatMatchingModelName`은 같은 패키지 `model_ratio.go`에 이미 존재. `common.UnmarshalJsonStr`은 CLAUDE.md Rule 1 준수.)

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run 'TestModelDiscount|TestVendorDiscount|TestDiscountValidation' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add setting/ratio_setting/model_discount.go setting/ratio_setting/model_discount_test.go
git commit -m "feat(pricing): add model/vendor discount setting layer"
```

---

### Task 2: 모델→벤더 캐시 + 유효 할인 배수

**Files:**
- Modify: `setting/ratio_setting/model_discount.go`
- Test: `setting/ratio_setting/model_discount_test.go`

`GetEffectiveDiscountMultiplier`는 모델 우선, 없으면 벤더 fallback. 벤더 해석은 주입식 resolver로 두어 `ratio_setting`이 `model` 패키지에 의존(순환)하지 않게 한다.

- [ ] **Step 1: 실패하는 테스트 작성**

`model_discount_test.go`에 추가:
```go
func TestEffectiveDiscountPrecedence(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"m-model":20}`)
	_ = UpdateVendorDiscountByJSONString(`{"acme":10}`)
	SetVendorResolver(func(model string) (string, bool) {
		switch model {
		case "m-model", "v-model":
			return "acme", true
		}
		return "", false
	})

	// 모델 할인 우선
	if got := GetEffectiveDiscountMultiplier("m-model"); got != 0.8 {
		t.Fatalf("model precedence: want 0.8 got %v", got)
	}
	// 모델 할인 없음 -> 벤더 fallback
	if got := GetEffectiveDiscountMultiplier("v-model"); got != 0.9 {
		t.Fatalf("vendor fallback: want 0.9 got %v", got)
	}
	// 둘 다 없음 -> 1.0
	if got := GetEffectiveDiscountMultiplier("none"); got != 1.0 {
		t.Fatalf("no discount: want 1.0 got %v", got)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run TestEffectiveDiscountPrecedence -v`
Expected: 컴파일 에러(미정의 `SetVendorResolver`/`GetEffectiveDiscountMultiplier`).

- [ ] **Step 3: 구현 추가**

`model_discount.go`에 추가:
```go
// vendorResolver: 모델명 -> 벤더명. model 패키지가 init 시 주입(순환 의존 회피).
var vendorResolver func(modelName string) (string, bool)

func SetVendorResolver(fn func(modelName string) (string, bool)) {
	vendorResolver = fn
}

// GetEffectiveDiscountMultiplier: 모델 할인 > 벤더 할인 > 1.0.
func GetEffectiveDiscountMultiplier(modelName string) float64 {
	if mul, ok := GetModelDiscountMultiplier(modelName); ok {
		return mul
	}
	if vendorResolver != nil {
		if vendor, ok := vendorResolver(modelName); ok {
			if mul, ok := GetVendorDiscountMultiplier(vendor); ok {
				return mul
			}
		}
	}
	return 1
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run TestEffectiveDiscountPrecedence -v`
Expected: PASS.

- [ ] **Step 5: 벤더 resolver를 model 패키지에서 주입**

`model/pricing.go`에 캐시 기반 resolver를 등록한다. 파일 끝에 추가하고, 기존 `InvalidatePricingCache()` 안에서 캐시를 비운다.

`model/pricing.go` 끝에 추가:
```go
// modelVendorNameCache: 모델명 -> 벤더명. pricing 캐시에서 파생.
var modelVendorNameCache map[string]string

func resolveVendorName(modelName string) (string, bool) {
	if modelVendorNameCache == nil {
		buildModelVendorNameCache()
	}
	name, ok := modelVendorNameCache[modelName]
	return name, ok && name != ""
}

func buildModelVendorNameCache() {
	cache := make(map[string]string)
	vendorIDToName := make(map[int]string)
	for _, v := range GetVendors() { // PricingVendor: Id,Name
		vendorIDToName[v.Id] = v.Name
	}
	for _, p := range GetPricing() { // Pricing: ModelName, VendorID
		if name, ok := vendorIDToName[p.VendorID]; ok {
			cache[p.ModelName] = name
		}
	}
	modelVendorNameCache = cache
}
```
그리고 `InvalidatePricingCache()` 본문에 `modelVendorNameCache = nil` 한 줄 추가.

`PricingVendor`의 필드명(`Id`,`Name`)은 `model/pricing.go`의 정의를 확인해 정확히 맞춘다(다르면 그 이름 사용).

`model` 패키지 init에서 resolver 주입 — `model/main.go`의 적절한 초기화 지점(또는 `model/pricing.go`의 `init()`)에 추가:
```go
func init() {
	ratio_setting.SetVendorResolver(resolveVendorName)
}
```
(import: `"github.com/QuantumNous/new-api/setting/ratio_setting"`. 이미 다른 곳에서 import 중이면 재사용.)

- [ ] **Step 6: 빌드 확인**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 성공(순환 의존 없음 — model→ratio_setting 단방향).

- [ ] **Step 7: Commit**

```bash
git add setting/ratio_setting/model_discount.go setting/ratio_setting/model_discount_test.go model/pricing.go model/main.go
git commit -m "feat(pricing): resolve effective discount (model>vendor) with cached vendor lookup"
```

---

### Task 3: 옵션 연동 (저장/로드/관리 키)

**Files:**
- Modify: `model/option.go` (OptionMap 등록 ~146-151, 업데이트 switch ~539-544)
- Modify: `controller/option.go` (관리 키 목록 ~22-23)

- [ ] **Step 1: OptionMap 기본 등록 추가**

`model/option.go`의 `common.OptionMap["GroupGroupRatio"] = ...` 줄 다음에 추가:
```go
	common.OptionMap["ModelDiscount"] = ratio_setting.ModelDiscount2JSONString()
	common.OptionMap["VendorDiscount"] = ratio_setting.VendorDiscount2JSONString()
```

- [ ] **Step 2: 업데이트 switch 케이스 추가**

`model/option.go`의 `case "GroupGroupRatio":` 블록 다음에 추가:
```go
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
```

- [ ] **Step 3: 관리 옵션 키 목록 추가**

`controller/option.go`의 옵션 키 슬라이스(예: `"ModelRatio",` 부근)에 추가:
```go
	"ModelDiscount",
	"VendorDiscount",
```

- [ ] **Step 4: 빌드 + 기존 옵션 테스트 확인**

Run: `cd /home/molla/new-api && go build ./... && go test ./model/ -run Option -v`
Expected: 빌드 성공. (옵션 관련 테스트 없으면 빌드만 확인.)

- [ ] **Step 5: Commit**

```bash
git add model/option.go controller/option.go
git commit -m "feat(pricing): wire ModelDiscount/VendorDiscount options"
```

---

### Task 4: 메인 과금 경로에 할인 fold-in (`price.go`)

**Files:**
- Modify: `relay/helper/price.go` (`ModelPriceHelper`, line ~68 직후)
- Test: `setting/ratio_setting/model_discount_test.go` (배수 결합 단위 검증)

`ModelPriceHelper`는 `groupRatioInfo := HandleGroupRatio(c, info)` 직후, tiered/토큰/정액 분기 **이전에** 할인 배수를 `GroupRatio`에 접어 넣는다. 이 값은 `relayInfo.PriceData.GroupRatioInfo.GroupRatio`로 저장되어 사전차감과 텍스트 정산([service/text_quota.go:169](../../service/text_quota.go))이 동일 값을 사용 → 일관 보장. expr 경로(`modelPriceHelperTiered`)도 같은 `groupRatioInfo`를 받으므로 커버됨.

- [ ] **Step 1: fold-in 코드 추가**

`relay/helper/price.go`에서:
```go
	groupRatioInfo := HandleGroupRatio(c, info)
```
바로 다음 줄에 추가:
```go
	// 모델/벤더 할인(모델 우선)을 유효 group ratio에 접어 넣어 사전차감·정산·expr이
	// 동일 배수를 쓰게 한다(GroupRatio와 동일 적용 범위).
	groupRatioInfo.GroupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(info.OriginModelName)
```
(`ratio_setting`은 이미 import됨.)

- [ ] **Step 2: 결합 배수 단위 테스트 추가**

`model_discount_test.go`에 추가:
```go
func TestEffectiveMultiplierCombinesWithGroupConceptually(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"combo":50}`)
	// groupRatio=0.8 가정, 할인 0.5 -> 최종 0.4
	groupRatio := 0.8
	final := groupRatio * GetEffectiveDiscountMultiplier("combo")
	if final != 0.4 {
		t.Fatalf("want 0.4 got %v", final)
	}
}
```

- [ ] **Step 3: 테스트 + 빌드 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run TestEffectiveMultiplier -v && go build ./...`
Expected: PASS + 빌드 성공.

- [ ] **Step 4: Commit**

```bash
git add relay/helper/price.go setting/ratio_setting/model_discount_test.go
git commit -m "feat(pricing): apply model/vendor discount on main billing path (pre-consume + text settlement + expr)"
```

---

### Task 5: 재계산 정산 경로에 할인 적용

**Files:**
- Modify: `service/quota.go` (`PreWssConsumeQuota` ~108, `calculateAudioQuota`)
- Modify: `service/task_billing.go` (~300)
- Modify: `service/tool_billing.go` (~52, ~73)

이 경로들은 `relayInfo.PriceData.GroupRatioInfo`를 쓰지 않고 `ratio_setting.GetGroupRatio(group)`를 직접 호출하므로, 동일 결과를 위해 `GetEffectiveDiscountMultiplier(model)`를 곱해 준다. **사전차감(Task4)과 정산이 같은 배수를 쓰도록**, 결합식은 항상 `groupRatio * GetEffectiveDiscountMultiplier(model)`.

- [ ] **Step 1: `service/quota.go` PreWssConsumeQuota**

`groupRatio := ratio_setting.GetGroupRatio(relayInfo.UsingGroup)` (line ~108)와 그 아래 `groupRatio = ratio_setting.GetGroupRatio(autoGroup.(string))` 분기 **이후**, groupRatio 확정 직후에 한 줄 추가:
```go
	groupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName)
```
(`modelName := relayInfo.OriginModelName`이 위에 이미 있음.)

- [ ] **Step 2: `service/quota.go` calculateAudioQuota**

`calculateAudioQuota(info QuotaInfo)`에서 `groupRatio := decimal.NewFromFloat(info.GroupRatio)` 직후에 추가:
```go
	groupRatio = groupRatio.Mul(decimal.NewFromFloat(ratio_setting.GetEffectiveDiscountMultiplier(info.ModelName)))
```
(audio는 `usePrice` 분기와 토큰 분기 둘 다 `groupRatio`를 쓰므로, 두 분기 공통 진입부 `info.GroupRatio` 사용 지점 각각에 동일 처리. 함수 내 `groupRatio` 선언이 분기별로 2곳이면 2곳 모두 같은 한 줄 추가.)

- [ ] **Step 3: `service/task_billing.go`**

`groupRatio := ratio_setting.GetGroupRatio(group)` (line ~300) 이후, `finalGroupRatio` 확정 지점 직전에:
```go
	groupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName)
```
(`modelName`이 스코프에 있는지 확인; line ~283에서 `GetModelRatio(modelName)` 호출하므로 존재.)

- [ ] **Step 4: `service/tool_billing.go`**

`quota := int(math.Round(totalPrice * common.QuotaPerUnit * groupRatio))` (line ~52)와 `... price * common.QuotaPerUnit * groupRatio)` (line ~73)에서, 각 함수가 받는 `groupRatio` 파라미터에 모델 할인을 곱한다. 각 함수 시작부에서 모델명을 받는지 확인 후, 함수 진입 직후 한 줄:
```go
	groupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName)
```
모델명 파라미터가 없으면, 호출부에서 `groupRatio`에 곱해 전달하도록 호출부를 수정(모델명이 있는 호출부에서 처리). 어느 쪽이든 **곱해지는 값이 `GetGroupRatio(group) * GetEffectiveDiscountMultiplier(model)`**가 되도록 한다.

- [ ] **Step 5: 빌드 + 회귀**

Run: `cd /home/molla/new-api && go build ./... && go test ./service/... ./relay/... 2>&1 | tail -20`
Expected: 빌드 성공, 기존 테스트 통과(또는 변동 없음).

- [ ] **Step 6: Commit**

```bash
git add service/quota.go service/task_billing.go service/tool_billing.go
git commit -m "feat(pricing): apply model/vendor discount on wss/audio/task/tool billing paths"
```

---

### Task 6: 프론트엔드 — 모델 할인 에디터

**Files:**
- Create: `web/default/src/features/system-settings/models/model-discount-visual-editor.tsx`
- Modify: 시스템 설정 모델 섹션 컨테이너(그룹 비율 에디터가 렌더되는 곳)

기존 [`group-ratio-visual-editor.tsx`](../../web/default/src/features/system-settings/models/group-ratio-visual-editor.tsx)를 미러한다. 차이점만:
- 옵션 키: `ModelDiscount`.
- 행: `모델명(텍스트 입력)` + `할인율 %(number, min 0 max 100)`.
- 저장 시 `{ [modelName]: percent }` JSON으로 `UpdateOption('ModelDiscount', json)`.
- 헤더 설명: "모델별 할인율(%). 0~100. 같은 모델에 모델 할인이 있으면 벤더 할인보다 우선합니다."
- JSON 모드 토글 포함(그룹 에디터와 동일).

- [ ] **Step 1: 컴포넌트 작성** — `group-ratio-visual-editor.tsx`를 복사해 위 차이점 반영(라벨/키/검증 0~100). 그룹비율 에디터의 로드(`getOption`/상태)·저장(`updateOption`) 흐름을 그대로 따른다.

- [ ] **Step 2: 컨테이너에 추가** — 그룹 비율 에디터가 import·렌더되는 모델 설정 섹션 파일에서 `ModelDiscountVisualEditor`를 동일하게 추가.

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "model-discount|error TS" | head; echo done`
Expected: 관련 에러 없음.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/system-settings/models/model-discount-visual-editor.tsx web/default/src/features/system-settings/
git commit -m "feat(pricing): model discount admin editor"
```

---

### Task 7: 프론트엔드 — 벤더 할인 에디터

**Files:**
- Create: `web/default/src/features/system-settings/models/vendor-discount-visual-editor.tsx`
- Modify: 동일 컨테이너

- [ ] **Step 1: 컴포넌트 작성** — Task 6과 동일 패턴이되 행의 모델명 입력 대신 **벤더 선택**(기존 벤더 목록 API 사용; 모델 관리 화면에서 벤더 목록을 가져오는 기존 훅/엔드포인트 재사용). 옵션 키 `VendorDiscount`. 헤더 설명: "벤더별 할인율(%). 해당 벤더 모델에 모델 할인이 없을 때만 적용됩니다."

- [ ] **Step 2: 컨테이너에 추가** — 모델 할인 에디터 옆/아래에 `VendorDiscountVisualEditor` 렌더.

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "vendor-discount|error TS" | head; echo done`
Expected: 관련 에러 없음.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/system-settings/models/vendor-discount-visual-editor.tsx web/default/src/features/system-settings/
git commit -m "feat(pricing): vendor discount admin editor"
```

---

### Task 8: 엔드투엔드 수동 검증 + 문서

**Files:** (없음 — 검증) / Modify: 관련 사용자 문서(있으면)

- [ ] **Step 1: 격리 백엔드 기동(요금 검증)**

```bash
cd /home/molla/new-api
nohup env -u SQL_DSN -u LOG_SQL_DSN SQLITE_PATH=/tmp/disc/db.sqlite \
  CRITICAL_RATE_LIMIT_ENABLE=false GLOBAL_API_RATE_LIMIT_ENABLE=false GLOBAL_WEB_RATE_LIMIT_ENABLE=false \
  ./new-api --port 3100 > /tmp/disc/srv.log 2>&1 & disown
```
admin 시드(`POST /api/setup`). `ModelDiscount`/`VendorDiscount` 옵션을 `POST /api/option`으로 설정 후 `GET /api/option`으로 반영 확인.

- [ ] **Step 2: 과금 일치 수동 확인**

모델 할인 20% 설정 → 동일 요청의 사전차감과 최종 로그 quota가 정가 대비 0.8배인지, 그리고 사전차감==정산인지 확인(로그/quota). 벤더 할인만 설정한 모델은 벤더 배수가 적용되는지, 모델 할인이 있으면 벤더가 무시되는지 확인.

- [ ] **Step 3: 문서 갱신** — 시스템 설정 가격 관련 문서/화면 설명에 "모델 할인 + 벤더 할인(모델 우선) / %입력 / GroupRatio와 곱연산" 추가.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs(pricing): document model/vendor discount layers"
```

---

## Self-Review

**Spec coverage:**
- 모델 할인 % → Task 1.
- 벤더 할인 % + 모델 우선 fallback → Task 2 (`GetEffectiveDiscountMultiplier`).
- 옵션 저장/로드/관리 → Task 3.
- 세 모드(토큰/정액/expr) 사전차감==정산 → Task 4 (메인) + Task 5 (재계산 경로).
- 0~100 검증, 100% 허용 → Task 1 `validateDiscountJSON`.
- 벤더는 벤더명 키 → Task 1/2.
- 모델→벤더 캐시(hot-path) → Task 2.
- 관리 UI 2종 → Task 6, 7.
- 테스트(우선순위/경계/일치) → Task 1,2,4 + Task 8 수동.
- 문서 → Task 8.
모든 spec 요구가 태스크에 매핑됨.

**Placeholder scan:** Go 핵심(Task 1~5)은 실제 코드 포함. 프론트(Task 6,7)는 기존 `group-ratio-visual-editor.tsx` 미러 + 명확한 차이점 명시(키/라벨/검증). Task 5의 일부는 "스코프에 modelName이 있는지 확인 후" 같은 조건부 지시가 있으나, 적용할 정확한 식(`groupRatio * GetEffectiveDiscountMultiplier(model)`)과 대상 file:line을 명시함.

**Type/이름 일관성:** `GetEffectiveDiscountMultiplier(model) float64`, `GetModelDiscountMultiplier`/`GetVendorDiscountMultiplier (float64,bool)`, `UpdateModelDiscountByJSONString`/`UpdateVendorDiscountByJSONString`, `SetVendorResolver`, `resolveVendorName` — Task 1·2 정의와 Task 3·4·5 사용에서 일치. 옵션 키 문자열 `"ModelDiscount"`/`"VendorDiscount"`가 Task 3 전반에서 일치.

**주의 사항(실행자):** Task 5의 `tool_billing.go`/`task_billing.go`/`calculateAudioQuota`는 함수 시그니처(모델명 가용성)를 먼저 확인하고, 모델명이 없으면 호출부에서 곱해 전달한다. 핵심 불변식은 **모든 과금 식이 `... × GroupRatio × GetEffectiveDiscountMultiplier(model) × ...`** 형태가 되는 것.
