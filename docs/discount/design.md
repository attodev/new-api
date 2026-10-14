# 모델 / 벤더 할인 — 설계 문서

> 대상: new-api 가격(ratio) 시스템의 "할인" 레이어 (백엔드 과금 + 가격 페이지 표시 + 사용 로그 상세)
> 최종 정리: 2026-10-14 (upstream 재기반 단계 C, overlay O6). team 원본(2026-06-25)을 develop 구조에 맞게 고쳤다.
> 관리자 사용법은 [admin-guide.md](./admin-guide.md) 참고. upstream 파일의 훅 위치는 `docs/fork-overlays.md`의 `O6` 행.

## 1. 개념

기존 가격 체계는 다음으로 구성된다.

- **모델별 가격**: `ModelRatio`(토큰 단가 배수), `ModelPrice`(요청당 정액), `Completion/Cache/Audio/Image Ratio` — "정가"를 정하는 값.
- **그룹 배수**: `GroupRatio`, `GroupGroupRatio` — **그룹(사용자 묶음)** 단위로 **모든 모델에 일괄** 적용되는 배수.

여기에 **"할인" 레이어** 두 가지를 추가한다(정가는 그대로 두고 할인만 별도로 켜고 끔).

- **모델 할인 (Model discount)**: 특정 **모델**에 X% 할인.
- **벤더 할인 (Vendor discount)**: 특정 **벤더(provider)** 의 모델에 X% 할인.

### 우선순위 (핵심)
한 요청에 적용되는 **유효 할인**은 다음 순서로 단 하나만 결정된다(`ratio_setting.GetEffectiveDiscountFor(origin, billing)`).

1. 요청 모델명(`OriginModelName`)에 **모델 할인**이 있으면 → 그 값
2. 없으면 요청 모델의 **벤더 할인**
3. 없으면 과금 모델명(`GetBillingModelName()`, 요청 모델명과 다를 때만)의 **모델 할인**
4. 없으면 과금 모델의 **벤더 할인**
5. 모두 없으면 할인 없음(배수 1.0, source `""`)

즉 모델 할인이 벤더 할인을 **덮어쓰고**(모델 > 벤더 > 없음), 요청 모델명이 과금 모델명보다 먼저다. 마켓플레이스는 `Pricing.ModelName`(요청 모델명)으로 할인을 보여 주므로 "보이는 할인 == 청구되는 할인"이 된다. team은 요청 모델명 하나만 봤고, 단계 C 계획은 처음에 과금 모델명을 썼다가 Task 2 리뷰 ruling으로 이 순서로 바꿨다. 0%도 "설정된 값"이라 조회가 거기서 멈춘다. 할인은 **전역**(모든 사용자)에 적용된다.

### 값
- 입력/저장은 **할인율 %(0~100)**. (100% = 무료까지 허용)
- 내부 배수 = `(100 - percent) / 100` (예: 20% → 0.8, 30% → 0.7).

## 2. 저장 / 설정

- 옵션 키 **`ModelDiscount`**: JSON `{ "<모델명>": <percent> }` (조회 키는 `FormatMatchingModelName`으로 정규화)
- 옵션 키 **`VendorDiscount`**: JSON `{ "<벤더명>": <percent> }`
- 둘 다 `options` 테이블에 JSON 문자열로 저장(크로스-DB 안전, 새 컬럼 없음).
- 정의/접근자: `setting/ratio_setting/model_discount.go` (team 파일 그대로 + origin/billing 조회 함수)
  - `GetModelDiscountMultiplier` / `GetVendorDiscountMultiplier` → 배수(+존재 여부)
  - `GetModelDiscountPercent` / `GetVendorDiscountPercent` → 저장된 %(가격 페이지 표시용)
  - `GetEffectiveDiscountFor(origin, billing)` → (배수, `"model"|"vendor"|""`), `GetEffectiveDiscountMultiplierFor`는 배수만
  - `UpdateModelDiscountByJSONString` / `UpdateVendorDiscountByJSONString` → 0–100 숫자 map만 받고, 실패하면 이전 값 유지
  - 벤더 해석(모델→벤더명)은 `model` 패키지가 주입하는 resolver(`SetVendorResolver`)로 처리 — 순환 의존 회피. 벤더 할인이 하나도 없으면 resolver 호출 자체를 건너뜀(hot-path 비용·캐시 미준비 방지). resolver의 모델→벤더명 캐시는 자체 `RWMutex`를 쓰고 `updatePricingLock`을 잡지 않는다.
- 옵션 연동: `model/option.go`(InitOptionMap 두 줄 + updateOptionMap case 한 줄) → `model/pricing_discount.go`의 `updateDiscountOption`. 저장에 성공하면 `InvalidatePricingCache()`로 가격 캐시를 즉시 무효화한다.

## 3. 과금 통합

핵심 설계: **유효 할인 배수를 `HandleGroupRatio`(GroupRatioInfo를 만드는 단일 지점)에서 `GroupRatio`에 한 번 접어 넣는다(fold).**

`relay/helper/price.go:63` 훅 → `relay/helper/price_discount.go`:
```
info.BaseGroupRatio     = info.GroupRatio          // 할인 전 기저값
info.DiscountMultiplier = mul                      // GetEffectiveDiscountFor(origin, billing)
if mul != 1 {
    info.DiscountSource = "model" | "vendor"
    info.GroupRatio    *= mul
}
```

`types.GroupRatioInfo`에 `BaseGroupRatio`, `DiscountMultiplier`, `DiscountSource` 세 필드를 추가했다. `BaseGroupRatio`가 있어서 로그에 기저값을 쓸 때 나눗셈(`folded / mul`)이 필요 없다. team은 나눗셈으로 복원했기 때문에 100% 할인(배수 0)에서 기저값을 잃었다.

이렇게 하면:
- **사전 차감(pre-consume)·텍스트 정산·tiered expr·per-call·이미지·위반 수수료·MJ** 가 모두 같은 `GroupRatioInfo`를 공유 → **사전차감 == 정산**(과/소청구 없음). 웹 검색 같은 도구 과금도 정산 요약의 `GroupRatio`(접힌 값)를 쓴다(develop에는 team의 `service/tool_billing.go`가 없다).
- 재시도 시 `controller/relay.go`가 `HandleGroupRatio`로 새 구조체를 만들어 재계산하므로 할인이 누적되지 않는다(재시도 안전).
- `GroupRatio`와 **곱연산으로 스택**(그룹 배율 × 모델/벤더 할인 동시 적용).
- 세 가지 가격 모드 공통:
  ```
  토큰:  quota = tokens × ModelRatio × CompletionRatio × GroupRatio × Discount × QuotaPerUnit
  정액:  quota = ModelPrice × GroupRatio × Discount × QuotaPerUnit
  expr:  quota = (rawCost/1e6) × GroupRatio × Discount × QuotaPerUnit
  ```

`HandleGroupRatio`를 거치지 않고 `GetGroupRatio`를 직접 다시 계산하는 경로는 그 지점에서 **명시적으로** 한 번 곱한다.
- `service/quota.go:122` `PreWssConsumeQuota`(realtime 사전차감): `GetEffectiveDiscountMultiplierFor(OriginModelName, GetBillingModelName())` — WSS 정산이 읽는 fold와 같은 키.
- `service/task_billing.go:423` `RecalculateTaskQuotaByTokens`: 태스크는 과금 모델명을 저장하지 않으므로 요청 모델명 하나(`(modelName, modelName)`)로 조회. 태스크 사전차감(`ModelPriceHelperPerCall`)도 `BillingModelName`이 비어 같은 키가 된다.

각 경로는 할인을 **정확히 한 번** 적용한다(Task 2 경로 추적과 mutation 테스트: `relay/helper/price_discount_e2e_test.go`의 사전차감 == 정산, `service/discount_billing_fork_test.go`의 WSS·태스크 훅).

## 4. 가격 페이지(마켓플레이스) 표시

백엔드 `GetPricing`(`model/pricing.go`)이 응답에 할인%를 실어 보내고, 프런트는 렌더링만 한다.

- `Pricing.DiscountPercent`(`discount_percent`) = 모델 **유효 할인%**(모델 > 벤더, 요청 모델명 기준).
- `PricingVendor.DiscountPercent` = 그 벤더 **자체 할인%**.
- 프런트(`web/src/features/pricing/`, upstream 컴포넌트에 훅을 넣어 다시 만듦 — spec §9 결정 3):
  - **벤더 필터 칩**(`pricing-sidebar.tsx`): `X% off` 배지(벤더 자신의 %).
  - **모델 카드**(`model-card.tsx`): `<DiscountedPrice>` — 취소선 정가(위 줄) + 할인가(아래 줄), 과금 방식 캡션 아래 `<DiscountBadge>`.
  - **표**(`model-price-cell.tsx`·`cached-price-cell.tsx`): Price 열은 취소선 정가 + 할인가 + 배지, Cached 열은 할인가만.
  - **모델 상세**(`model-details.tsx`): 헤더 배지, 기본가·그룹별 가격표에 할인가. upstream 화면에서 상세로 가는 경로가 없어 단위 테스트로만 확인.
  - 가격 포맷터(`lib/price.ts`의 `formatPrice`·`formatGroupPrice`·`formatFixedPrice`·`formatRequestPrice`)는 마지막 선택 인자 `discountPercent = 0`을 받아 충전율 적용 직후 `× (1 − %/100)`.
  - **히트맵 색상**(`lib/discount-color.ts`): 1–14% 에메랄드, 15–29% 앰버, 30–49% 오렌지, 50%+ 빨강.
  - 동적가격(billingexpr) 모델: 고정 $/M가 없으므로 **배지만**(취소선 가격 생략, 표시 가격은 정가). 할인 0%면 표시 없음.

## 5. 사용 로그 상세 표시

할인이 `group_ratio`에 접혀 있으면 로그에서 "그룹 할인"처럼 보이므로, 로그에는 **분리**해 기록한다.

- `service/log_info_generate.go:110` 훅 → `service/log_discount_fork.go`의 `appendDiscountInfo`: `DiscountSource`가 있고 배수가 1이 아니면 `other`에 `discount_multiplier`·`discount_source`("model"/"vendor")를 쓰고, `group_ratio`를 **할인 전 기저값**(`BaseGroupRatio`)으로 덮어쓴다. 배수 0(100% 할인)도 기록한다. `user_group_ratio`는 원래 기저값.
  - `GenerateTextOtherInfo` 한 지점이라 Claude·WSS·오디오 로그(모두 이 함수에 위임)도 같이 덮인다. WSS·오디오 로그의 content 문자열도 `discountBaseGroupRatio`로 기저값을 쓴다(`service/quota.go:220,225,360,365`).
  - MJ·태스크 로그(`GenerateMjOtherInfo`, `task_billing.go`)는 접힌 `group_ratio`를 그대로 쓰고 할인 필드가 없다(단계 D 이후).
- 프런트(`web/src/features/usage-logs/components/dialogs/details-dialog.tsx`의 `BillingBreakdown`): `Group Ratio`(또는 `User Exclusive Ratio`) 행 다음에 `Model discount` / `Vendor discount` 행을 `0.7500x` 형식(`formatRatio`, 소수 4자리 + `x`)으로 추가한다. 배수가 null이거나 1이면, 또는 source가 없으면 행이 없다.
  - team의 "Show formula" 계산식 줄(`… × Model discount 0.7 = $…`)은 upstream 상세에 없다. upstream `BillingBreakdown`의 가격 행은 모두 정가(`model_ratio` 등) 기반이고 합계는 `log.quota`라서, `group_ratio`에서 계산되는 금액이 없어 배수를 다시 곱할 곳이 없다.

> 주의: 로그 형식 변경은 **새 요청부터** 적용된다(과거 로그는 할인 행 없음).

## 6. 파일 맵

| 영역 | 생성(fork 파일) | 수정(upstream 훅, `// fork: O6`) |
|---|---|---|
| 설정 | `setting/ratio_setting/model_discount.go`(+`_test.go`), `model/pricing_discount.go`(+`_test.go`) | `model/option.go:162-163, 662-663`, `model/pricing.go:52, 61, 103, 226, 228, 235, 437`, `types/price_data.go:15-17` |
| 과금·로그 | `relay/helper/price_discount.go`(+`_test.go`, `price_discount_e2e_test.go`), `service/log_discount_fork.go`(+`_test.go`), `service/discount_billing_fork_test.go` | `relay/helper/price.go:63`, `service/quota.go:122, 220, 225, 360, 365`, `service/task_billing.go:423`, `service/log_info_generate.go:110` |
| 가격 페이지 | `web/src/features/pricing/lib/discount-color.ts`, `components/discount-badge.tsx`, `__tests__/discount.test.tsx` | `pricing/types.ts`, `lib/price.ts`, `components/{model-card,model-price-cell,cached-price-cell,pricing-sidebar,model-details}.tsx` |
| 사용 로그 | `web/src/features/usage-logs/__tests__/billing-breakdown-discount.test.tsx` | `usage-logs/types.ts`, `components/dialogs/details-dialog.tsx` |
| 관리자 편집기 | `web/src/features/system-settings/models/{model-discount-visual-editor,vendor-discount-visual-editor,discount-settings-panel}.tsx`, `discount-utils.ts`, `__tests__/discount-editors.test.tsx` | `system-settings/{types.ts,billing/index.tsx,billing/section-registry.tsx}`, `models/{ratio-settings-card,group-ratio-form,group-ratio-visual-editor}.tsx`, `models/__tests__/group-settings.test.tsx` |
| 번역 | — | `web/src/i18n/locales/*.json`(7개 locale, 할인 키 번역 포함) |

관리자 편집기는 upstream `GroupRatioVisualEditor`의 탭(`discounts`)이다. team의 평면 카드·카드별 JSON 토글은 없고, 그룹 폼 전체의 JSON 모드에 두 필드가 들어간다. 검증은 `createGroupSchema`의 `discountMapValidation`(0–100 숫자 map, 빈 문자열 거부)이고, 저장은 `saveGroupRatios`가 바뀐 키만 `PUT /api/option/`한다.

## 7. 동작 요약 / 불변식

- 할인은 **요청 모델(모델 → 벤더) → 과금 모델(모델 → 벤더)** 순서로 한 요청에 하나만 적용.
- 모든 과금 식 = `… × GroupRatio × Discount × …`, 할인은 **경로마다 정확히 한 번**, 사전차감 == 정산.
- 정가(`ModelRatio`/`ModelPrice`)는 변경하지 않음 — 할인은 별도 레이어라 켜고 끄기 자유.
- 100% 할인은 과금 0이고, 로그의 `group_ratio`에는 `BaseGroupRatio`(기저값)가 남는다.
- 할인은 `FreeModel`을 바꾸지 않는다. `relay/helper/price.go`의 무료 모델 판정 4곳은 `freeModelGroupRatio`(할인을 접은 경우 `BaseGroupRatio`, 아니면 `GroupRatio`)를 본다. 100% 할인 모델도 결제 세션(조직·구성원 상태 검사)을 거치고 0을 예약한다.
- 옵션 변경 시 가격 캐시 즉시 무효화 → 마켓플레이스·과금에 곧바로 반영.
