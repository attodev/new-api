# 모델 마켓플레이스 할인 표시 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 모델/벤더 할인을 사용자용 가격 페이지에 표시한다 — 벤더 사이드바 `X% off` 배지, 모델 카드·테이블의 `~~원가~~ 할인가` + `X% off` 배지.

**Architecture:** 백엔드 `GetPricing`이 모델별 유효 할인%(모델>벤더)와 벤더별 할인%를 응답에 포함(`discount_percent`). 프론트는 이 값을 읽어 사이드바/카드/테이블에 배지와 할인가를 렌더. 할인가 = 기존 표시가 × (1−%/100). 동적가격 모델은 배지만.

**Tech Stack:** Go (model/pricing.go, setting/ratio_setting), React 19 + TS (`web/default/src/features/pricing/`).

---

## File Structure
- Modify: `setting/ratio_setting/model_discount.go` — 퍼센트 접근자 추가.
- Modify: `setting/ratio_setting/model_discount_test.go` — 접근자 테스트.
- Modify: `model/pricing.go` — `Pricing`/`PricingVendor`에 `DiscountPercent`, `updatePricing()`에서 채움.
- Modify: `model/option.go` — 할인 옵션 변경 시 pricing 캐시 무효화.
- Modify: `web/default/src/features/pricing/types.ts` — 타입 필드.
- Modify: `web/default/src/features/pricing/lib/price.ts` — `discountPercent` 파라미터.
- Modify: `web/default/src/features/pricing/components/model-card.tsx`
- Modify: `web/default/src/features/pricing/components/pricing-columns.tsx`
- Modify: `web/default/src/features/pricing/components/pricing-sidebar.tsx`

---

### Task 1: 백엔드 — 할인 퍼센트 접근자

**Files:**
- Modify: `setting/ratio_setting/model_discount.go`
- Test: `setting/ratio_setting/model_discount_test.go`

- [ ] **Step 1: 실패 테스트 추가**

`model_discount_test.go`에 추가:
```go
func TestDiscountPercentAccessors(t *testing.T) {
	_ = UpdateModelDiscountByJSONString(`{"pct-model":25}`)
	_ = UpdateVendorDiscountByJSONString(`{"pct-vendor":15}`)

	if p, ok := GetModelDiscountPercent("pct-model"); !ok || p != 25 {
		t.Fatalf("model percent want 25,true got %v,%v", p, ok)
	}
	if _, ok := GetModelDiscountPercent("nope"); ok {
		t.Fatalf("unset model should be false")
	}
	if p, ok := GetVendorDiscountPercent("pct-vendor"); !ok || p != 15 {
		t.Fatalf("vendor percent want 15,true got %v,%v", p, ok)
	}
	if _, ok := GetVendorDiscountPercent("nope"); ok {
		t.Fatalf("unset vendor should be false")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run TestDiscountPercentAccessors -v`
Expected: 컴파일 에러(미정의).

- [ ] **Step 3: 구현 추가**

`setting/ratio_setting/model_discount.go`에 추가:
```go
// GetModelDiscountPercent: 저장된 모델 할인율(percent)을 반환. 미설정 시 (0,false).
func GetModelDiscountPercent(modelName string) (float64, bool) {
	return modelDiscountMap.Get(FormatMatchingModelName(modelName))
}

// GetVendorDiscountPercent: 저장된 벤더 할인율(percent)을 반환. 미설정 시 (0,false).
func GetVendorDiscountPercent(vendorName string) (float64, bool) {
	return vendorDiscountMap.Get(vendorName)
}
```

- [ ] **Step 4: 통과 확인**

Run: `cd /home/molla/new-api && go test ./setting/ratio_setting/ -run TestDiscountPercentAccessors -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add setting/ratio_setting/model_discount.go setting/ratio_setting/model_discount_test.go
git commit -m "feat(pricing): add discount percent accessors"
```

---

### Task 2: 백엔드 — pricing 응답에 할인% 포함 + 캐시 무효화

**Files:**
- Modify: `model/pricing.go` (`Pricing` 구조체, `PricingVendor` 구조체, `updatePricing()`)
- Modify: `model/option.go` (할인 옵션 변경 시 무효화)

- [ ] **Step 1: 구조체 필드 추가**

`model/pricing.go` `Pricing` 구조체(필드 그룹, `VendorID` 부근)에 추가:
```go
	DiscountPercent float64 `json:"discount_percent,omitempty"`
```
`PricingVendor` 구조체(`ID`,`Name`,`Description`,`Icon`)에 추가:
```go
	DiscountPercent float64 `json:"discount_percent,omitempty"`
```

- [ ] **Step 2: 벤더 할인% 채우기**

`updatePricing()`의 `vendorsList = append(vendorsList, PricingVendor{...})` 블록에서 `DiscountPercent`를 채운다. 기존:
```go
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorsList = append(vendorsList, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}
```
교체:
```go
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorDiscount := 0.0
		if pct, ok := ratio_setting.GetVendorDiscountPercent(v.Name); ok {
			vendorDiscount = pct
		}
		vendorsList = append(vendorsList, PricingVendor{
			ID:              v.Id,
			Name:            v.Name,
			Description:     v.Description,
			Icon:            v.Icon,
			DiscountPercent: vendorDiscount,
		})
	}
```

- [ ] **Step 3: 모델 유효 할인% 채우기**

`updatePricing()`에서 각 모델 `pricing`을 만들어 `pricingMap = append(pricingMap, pricing)` 하기 직전에, 유효 할인%를 계산해 설정한다. `pricing` 변수에 `ModelName`과 `VendorID`가 이미 설정돼 있고, `vendorIDToName`(Task 없음 — 아래 주의) 매핑이 필요하다.

**주의:** `updatePricing` 내에는 모델→벤더명 매핑이 `vendorMap`(id→*Vendor)로 존재한다. 이를 이용한다. `pricing.BillingMode`/append 직전 위치에 삽입:
```go
		// 유효 할인%: 모델 할인 우선, 없으면 벤더 할인.
		discountPercent := 0.0
		if pct, ok := ratio_setting.GetModelDiscountPercent(pricing.ModelName); ok {
			discountPercent = pct
		} else if v, ok := vendorMap[pricing.VendorID]; ok {
			if pct, ok := ratio_setting.GetVendorDiscountPercent(v.Name); ok {
				discountPercent = pct
			}
		}
		pricing.DiscountPercent = discountPercent
		pricingMap = append(pricingMap, pricing)
```
(append 라인이 이미 있으므로, append 직전에 위 계산 블록을 넣고 기존 append와 중복되지 않게 한다. `vendorMap`이 이 시점 스코프에 있는지 확인 — `updatePricing` 함수 내내 유효. 만약 pricing append가 `vendorMap` 스코프 밖이면, 위 §Step2의 `vendorIDToName` 맵을 함수 상단에서 만들어 사용한다.)

- [ ] **Step 4: 할인 옵션 변경 시 pricing 캐시 무효화**

`model/option.go`의 switch에서 할인 케이스를 즉시 반영되게 한다. 기존:
```go
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
```
교체:
```go
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
		if err == nil {
			InvalidatePricingCache()
		}
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
		if err == nil {
			InvalidatePricingCache()
		}
```
(`InvalidatePricingCache`는 같은 `model` 패키지 — import 불필요.)

- [ ] **Step 5: 빌드**

Run: `cd /home/molla/new-api && go build ./...`
Expected: 성공. (`ratio_setting`이 model/pricing.go에 이미 import됨.)

- [ ] **Step 6: Commit**

```bash
git add model/pricing.go model/option.go
git commit -m "feat(pricing): expose effective/vendor discount percent in pricing API"
```

---

### Task 3: 프론트 — 타입 + 가격 계산에 할인 적용

**Files:**
- Modify: `web/default/src/features/pricing/types.ts`
- Modify: `web/default/src/features/pricing/lib/price.ts`

- [ ] **Step 1: 타입 필드 추가**

`types.ts` `PricingModel`에 추가: `discount_percent?: number`. `PricingVendor`에 추가: `discount_percent?: number`.

- [ ] **Step 2: `formatPrice`에 discountPercent 파라미터 추가**

`lib/price.ts` `formatPrice` 시그니처와 본문 수정:
```ts
export function formatPrice(
  model: PricingModel,
  type: PriceType,
  tokenUnit: TokenUnit,
  showWithRecharge = false,
  priceRate = 1,
  usdExchangeRate = 1,
  discountPercent = 0
): string {
  if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) {
    return '-'
  }
  const enableGroups = Array.isArray(model.enable_groups)
    ? model.enable_groups
    : []
  const groupRatio = model.group_ratio || {}
  const minRatio = getMinGroupRatio(enableGroups, groupRatio)

  let priceInUSD = calculateTokenPrice(model, type, minRatio)
  priceInUSD = applyRechargeRate(
    priceInUSD,
    showWithRecharge,
    priceRate,
    usdExchangeRate
  )
  if (discountPercent > 0) {
    priceInUSD = priceInUSD * (1 - discountPercent / 100)
  }
  const price = priceInUSD / TOKEN_UNIT_DIVISORS[tokenUnit]
  return formatCurrencyFromUSD(price, {
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  })
}
```

- [ ] **Step 3: `formatRequestPrice`에 discountPercent 파라미터 추가**

`lib/price.ts` `formatRequestPrice`도 동일 패턴으로 마지막에 `discountPercent = 0` 파라미터를 추가하고, `priceInUSD = (model.model_price || 0) * minRatio` 뒤(그리고 `applyRechargeRate` 뒤)에:
```ts
  if (discountPercent > 0) {
    priceInUSD = priceInUSD * (1 - discountPercent / 100)
  }
```
를 삽입한다. (먼저 함수 본문을 읽어 정확한 위치에 넣을 것.)

- [ ] **Step 4: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "price.ts|types.ts|error TS" | head; echo done`
Expected: 관련 에러 없음(기존 호출부는 새 파라미터가 선택적이라 영향 없음).

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/pricing/types.ts web/default/src/features/pricing/lib/price.ts
git commit -m "feat(pricing): discount-aware price formatting + types"
```

---

### Task 4: 프론트 — 모델 카드 할인 표시

**Files:**
- Modify: `web/default/src/features/pricing/components/model-card.tsx`

먼저 파일을 읽어 Input/Output(및 per-request) 가격 렌더 위치(현행 `formatPrice(props.model,'input',...)` 등)와 푸터 배지 영역을 확인한다. `StatusBadge`는 이미 import되어 있음.

- [ ] **Step 1: 할인 변수 도출**

컴포넌트 본문 상단(렌더 직전)에 추가:
```tsx
  const discountPercent = props.model.discount_percent ?? 0
  const hasDiscount = discountPercent > 0
  const isDynamic = isDynamicPricing // 이미 계산된 동적가격 여부 변수명에 맞춰 사용
```

- [ ] **Step 2: Input/Output(또는 request) 가격을 취소선 원가 + 할인가로**

고정가(토큰/요청) + `hasDiscount && !isDynamic`일 때, 각 `formatPrice(props.model,'input',tokenUnit,showRechargePrice,priceRate,usdExchangeRate)` 표시를 다음으로 감싼다(예시는 input):
```tsx
{hasDiscount && !isDynamic ? (
  <>
    <s className='text-muted-foreground/50'>
      {formatPrice(props.model,'input',tokenUnit,showRechargePrice,priceRate,usdExchangeRate)}
    </s>{' '}
    <span className='text-foreground font-mono font-semibold'>
      {formatPrice(props.model,'input',tokenUnit,showRechargePrice,priceRate,usdExchangeRate,discountPercent)}
    </span>
  </>
) : (
  <span className='text-foreground font-mono font-semibold'>
    {formatPrice(props.model,'input',tokenUnit,showRechargePrice,priceRate,usdExchangeRate)}
  </span>
)}
```
Output(및 per-request `formatRequestPrice`)도 동일 패턴 적용.

- [ ] **Step 3: 할인 배지 추가**

좌측 푸터 배지 영역(`Token-based`/`Per Request` 배지 부근)에 `hasDiscount`일 때:
```tsx
{hasDiscount && (
  <StatusBadge label={`${discountPercent}% off`} variant='success' size='sm' copyable={false} />
)}
```

- [ ] **Step 4: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "model-card|error TS" | head; echo done`
Expected: 관련 에러 없음.

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/pricing/components/model-card.tsx
git commit -m "feat(pricing): show discount badge and discounted price on model card"
```

---

### Task 5: 프론트 — 테이블(컬럼) 할인 표시

**Files:**
- Modify: `web/default/src/features/pricing/components/pricing-columns.tsx`

먼저 파일을 읽어 가격 컬럼의 `formatPrice`/`formatRequestPrice` 호출과 `model`/`discount` 접근 방법을 확인한다. (모델은 `row.original` 등으로 접근.)

- [ ] **Step 1: 가격 셀에 할인 적용**

가격 셀 렌더에서 `const discountPercent = model.discount_percent ?? 0`를 도출하고, `discountPercent>0 && !isDynamic`이면 input/output(또는 request) 값을 `~~원가~~ 할인가`(취소선 `text-muted-foreground/50` + 할인가)로 렌더. 할인가는 `formatPrice(..., discountPercent)` / `formatRequestPrice(..., discountPercent)`로 계산. 동적가격이면 배지만.

- [ ] **Step 2: 인라인 배지**

가격 셀(또는 인접)에서 `discountPercent>0`일 때 `${discountPercent}% off` 배지(StatusBadge success, size sm)를 추가.

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "pricing-columns|error TS" | head; echo done`
Expected: 관련 에러 없음.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/pricing/components/pricing-columns.tsx
git commit -m "feat(pricing): show discount in pricing table"
```

---

### Task 6: 프론트 — 벤더 사이드바 배지

**Files:**
- Modify: `web/default/src/features/pricing/components/pricing-sidebar.tsx`

`vendorOptions` 생성부(`...props.vendors.map((vendor) => ({ value, label, count, icon }))`)에서 각 옵션에 `suffix`를 추가. `FilterChip`은 이미 `option.suffix`를 카운트 배지 영역에 렌더한다.

- [ ] **Step 1: suffix 추가**

`vendor` 매핑 객체에 추가:
```ts
      suffix:
        vendor.discount_percent && vendor.discount_percent > 0
          ? t('{{percent}}% off', { percent: vendor.discount_percent })
          : undefined,
```
(`t`가 해당 스코프에 있는지 확인; 없으면 `\`${vendor.discount_percent}% off\``로 처리.)

> 주의: `suffix`가 있으면 기존 카운트가 가려질 수 있음(`FilterChip`은 `props.option.suffix ?? props.option.count` 렌더). 카운트와 할인을 **둘 다** 보여주려면 `FilterChip`을 수정해 count와 suffix를 나란히 렌더(예: count 배지 + 별도 success 배지). 스크린샷은 카운트와 `X% off`가 **둘 다** 보이므로, `FilterChip`에서 `count`(기존)와 `suffix`(할인, success 톤 별도 span)를 각각 렌더하도록 한다.

- [ ] **Step 2: FilterChip에서 count+할인 둘 다 렌더**

`FilterChip`의 배지 렌더부를 수정: 기존 count 배지는 유지하고, `option.suffix`가 있으면 그 옆에 success 톤 배지를 추가:
```tsx
{props.option.count != null && (
  <span className='rounded-md px-1.5 py-0.5 text-[10px] ...'>{props.option.count}</span>
)}
{props.option.suffix && (
  <span className='rounded-md px-1.5 py-0.5 text-[10px] bg-emerald-500/15 text-emerald-600'>
    {props.option.suffix}
  </span>
)}
```
(기존 클래스 톤에 맞춰 색상 조정. `FilterOption` 타입에 `suffix?: string`이 이미 있으면 그대로, 없으면 추가.)

- [ ] **Step 3: 타입체크**

Run: `cd /home/molla/new-api/web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "pricing-sidebar|error TS" | head; echo done`
Expected: 관련 에러 없음.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/pricing/components/pricing-sidebar.tsx
git commit -m "feat(pricing): show vendor discount badge in pricing sidebar"
```

---

### Task 7: 빌드·배포·시각 검증

**Files:** (없음 — 검증)

- [ ] **Step 1: 전체 빌드**

Run: `cd /home/molla/new-api && go build ./... && cd web/default && bunx tsc --noEmit -p tsconfig.json 2>&1 | grep -iE "error TS" | head; echo done`
Expected: Go 빌드 성공, TS 에러 없음.

- [ ] **Step 2: 프론트 빌드 + 바이너리 재빌드 + 서비스 재시작**

```bash
cd /home/molla/new-api/web/default && bun run build
cd /home/molla/new-api && go build -o new-api.new . && mv -f new-api.new new-api
sudo -n systemctl restart new-api.service && sleep 3 && systemctl is-active new-api.service
```

- [ ] **Step 3: 시각 검증**

`admin`/`atto1234`로 로그인 → 시스템 설정에서 일부 모델/벤더에 할인 설정 → 모델 마켓플레이스(가격 페이지)에서:
- 벤더 사이드바에 `X% off` 배지(카운트와 함께) 표시
- 모델 카드/테이블에 `~~원가~~ 할인가` + `X% off` 배지
- 모델 할인이 있는 모델은 모델%가, 없는 모델은 벤더%가 표시(우선순위)
- 할인 0% 모델은 변화 없음

- [ ] **Step 4: Commit (필요 시 보정)**

보정이 있었다면 해당 파일 커밋.

---

## Self-Review

**Spec coverage:**
- 퍼센트 접근자 → Task 1.
- pricing API에 모델/벤더 할인% → Task 2 (구조체 + updatePricing).
- 캐시 무효화(§8) → Task 2 Step 4.
- 프론트 타입 + 할인가 계산 → Task 3.
- 카드 표시 → Task 4. 테이블 → Task 5. 사이드바 → Task 6.
- 동적가격 배지만 / 0% 미표시 → Task 4·5 조건.
- 시각 검증 → Task 7.
모든 spec 요구가 매핑됨.

**Placeholder scan:** 백엔드(Task 1·2)·price.ts(Task 3)는 완전한 코드. 프론트 렌더(Task 4·5·6)는 기존 JSX를 읽고 적용하는 지시 + 구체 스니펫(취소선+할인가 패턴, 배지, suffix) 포함. "먼저 파일을 읽어"는 정확한 현행 라인 확인용이며 적용할 코드 패턴은 명시.

**Type/이름 일관성:** `GetModelDiscountPercent`/`GetVendorDiscountPercent (float64,bool)` — Task 1 정의, Task 2 사용 일치. `DiscountPercent`(Go) / `discount_percent`(JSON·TS) 일치. `formatPrice`/`formatRequestPrice`의 새 `discountPercent` 파라미터 — Task 3 정의, Task 4·5 사용 일치. `discount_percent`는 PricingModel·PricingVendor 양쪽(Task 3) — Task 4(model)·Task 6(vendor) 사용 일치.

**실행자 주의:** Task 2 Step 3의 모델→벤더 매핑은 `updatePricing` 스코프의 `vendorMap`(id→*Vendor)을 쓰되, append 지점에서 스코프 밖이면 함수 상단에 `vendorIDToName`(또는 `vendorMap`) 가시성을 확보해 사용. Task 6은 count와 할인 배지를 **둘 다** 보이게 `FilterChip` 수정 필요(기존은 `suffix ?? count`).
