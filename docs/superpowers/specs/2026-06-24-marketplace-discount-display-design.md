# 모델 마켓플레이스 할인 표시 설계

> 작성일: 2026-06-24
> 대상: 모델 마켓플레이스(가격 페이지)에서 모델/벤더 할인을 표시
> 선행: 모델/벤더 할인 기능(백엔드 과금 + 관리자 에디터)은 `discount` 브랜치에 구현 완료. 본 설계는 그 할인을 **사용자용 가격 페이지에 표시**하는 후속 작업.
> 관련 코드: `model/pricing.go`, `setting/ratio_setting/model_discount.go`, `web/default/src/features/pricing/`

## 1. 배경 / 목표

할인(모델별·벤더별 %)은 과금에는 반영되지만 **사용자용 모델 마켓플레이스에는 표시되지 않는다**. 사용자가 첨부한 스크린샷처럼:

- **벤더 사이드바**: 각 벤더 칩에 `X% off` 배지.
- **모델 카드 / 테이블**: Input/Output 가격을 `~~원가~~ 할인가`(취소선 원가 + 할인 적용가)로, 그리고 `X% off` 배지.

를 구현한다.

### 비목표 (YAGNI)
- 할인 로직 자체 변경(이미 구현됨). 본 작업은 **표시 전용**.
- 그룹별/사용자별 가격 표시 변경 — 현행 유지(할인은 기존 표시가 위에 곱).
- 기간/프로모션 타이머 등 — 범위 밖.

## 2. 결정 사항
- 모델 카드/테이블에 표시하는 할인 = **유효 할인(모델 할인 우선, 없으면 벤더 할인)** — 과금 우선순위와 동일.
- 벤더 사이드바 배지 = **그 벤더 자체의 할인%**(일부 모델이 개별 할인으로 덮어써도 사이드바는 벤더값을 표기 — 스크린샷과 동일, 단순·명료).
- 할인가 = 기존 표시가 × `(1 − percent/100)`. 기존 표시가는 현행대로 최소 그룹비율을 반영하며, 할인은 그 위에 곱한다(과금의 `GroupRatio × DiscountMul`과 동일한 합성).
- 동적가격(billingexpr) 모델은 고정 $/M가 없으므로 **취소선 가격은 생략하고 배지만** 표시.
- 할인 0% → 배지·취소선 없이 현행 그대로.

## 3. 접근 방식 (대안)
- **A. 백엔드가 유효 할인%를 pricing API에 포함 (채택)** — `GetPricing`이 모델별 유효 할인%·벤더별 할인%를 계산해 응답. 프론트는 렌더링만. 과금과 단일 일관, 프론트 로직 최소.
- B. 원본 할인 맵을 프론트로 내려 프론트에서 우선순위 계산 — 비즈니스 로직 중복 → 기각.

## 4. 상세 설계

### 4.1 백엔드 — 퍼센트 접근자 (`setting/ratio_setting/model_discount.go`)
저장된 percent를 그대로 반환(배수 역산 시 반올림 오차 방지):
```go
func GetModelDiscountPercent(modelName string) (float64, bool)  // FormatMatchingModelName 적용, 미설정 시 (0,false)
func GetVendorDiscountPercent(vendorName string) (float64, bool) // 미설정 시 (0,false)
```
(기존 `GetModelDiscountMultiplier`/`GetVendorDiscountMultiplier`와 동일 매칭 규칙.)

### 4.2 백엔드 — pricing 응답에 할인% 포함 (`model/pricing.go`)
- `Pricing` 구조체에 `DiscountPercent float64 \`json:"discount_percent,omitempty"\`` 추가.
- `PricingVendor` 구조체에 `DiscountPercent float64 \`json:"discount_percent,omitempty"\`` 추가.
- `updatePricing()` 내부(락 구간, 이미 `vendorIDToName`/`pricingMap` 보유)에서 채운다:
  - 벤더 목록 빌드 시: `PricingVendor.DiscountPercent` = `GetVendorDiscountPercent(v.Name)`의 값(없으면 0).
  - 각 모델 pricing 빌드 시: 유효 할인% =
    1. `GetModelDiscountPercent(model)` 있으면 그 값,
    2. 없으면 그 모델의 벤더명으로 `GetVendorDiscountPercent(vendor)` 있으면 그 값,
    3. 없으면 0.
    → `pricing.DiscountPercent`에 저장. (모델→벤더는 `vendorIDToName[p.VendorID]`로 해석 — 같은 함수에서 이미 만든 매핑 재사용.)

### 4.3 프론트 — 타입 (`features/pricing/types.ts`)
- `PricingModel`에 `discount_percent?: number` 추가.
- `PricingVendor`에 `discount_percent?: number` 추가.

### 4.4 프론트 — 가격 계산 (`features/pricing/lib/price.ts`)
- `formatPrice`/`formatRequestPrice`에 **선택적 할인 배수 적용**을 추가한다. 구체적으로, 할인가 계산용으로 `priceInUSD`에 `(1 - percent/100)`을 곱하는 경로를 제공(예: 함수에 `discountPercent` 옵션 파라미터 추가, 기본 0 = 현행과 동일).
- 카드/테이블은 **원가**(discountPercent=0)와 **할인가**(`model.discount_percent`)를 각각 포맷해 둘 다 렌더링.

### 4.5 프론트 — 모델 카드 (`components/model-card.tsx`)
- `discount_percent > 0`이고 토큰/요청 기반(고정가)일 때:
  - Input/Output(또는 per-request) 가격을 `<s>원가</s> 할인가` 형태로 렌더(취소선 색은 `text-muted-foreground/50` 등 기존 톤).
  - 좌측 푸터 배지 영역에 `StatusBadge label={`${discount}% off`} variant='success' size='sm'` 추가, 그리고/또는 스크린샷처럼 카드 우하단에 동일 배지.
- 동적가격 모델: 취소선 가격 생략, 배지만.

### 4.6 프론트 — 테이블 (`components/pricing-columns.tsx`)
- 가격 컬럼에서 `discount_percent > 0`이면 `~~원가~~ 할인가`(취소선) + `X% off` 배지(인라인).
- 동적가격: 배지만.

### 4.7 프론트 — 벤더 사이드바 (`components/pricing-sidebar.tsx`)
- 벤더 칩 옵션에 `suffix = vendor.discount_percent ? `${vendor.discount_percent}% off` : undefined` 추가 → 기존 카운트 배지 영역에 함께 렌더(이미 `suffix` 지원).

## 5. 데이터 흐름
```
관리자가 ModelDiscount/VendorDiscount 옵션 설정
        ↓ (옵션 갱신 → InvalidatePricingCache)
updatePricing(): 모델별 유효 할인% + 벤더별 할인% 계산 → Pricing/PricingVendor.DiscountPercent
        ↓ GET /api/pricing
프론트: 사이드바 벤더 배지 / 카드·테이블 취소선 원가+할인가+배지 렌더
```

## 6. 테스트 / 검증
- 백엔드: `model_discount_test.go`에 `GetModelDiscountPercent`/`GetVendorDiscountPercent` 단위 테스트(설정값/경계/미설정 시 false). `updatePricing` 채움은 라이브 페이지로 시각 검증(DB 의존).
- 프론트: `bunx tsc --noEmit` 통과. 배포 서버에서 스크린샷과 동일하게 사이드바/카드/테이블에 할인 표시되는지 시각 확인.
- 일관성: 모델 카드의 유효 할인%가 과금 할인(모델>벤더)과 일치하는지 표본 확인.

## 7. 위험 / 주의
| 위험 | 완화 |
|---|---|
| pricing 캐시가 옵션 변경 후 갱신 안 됨 | 옵션 갱신 시 `InvalidatePricingCache` 호출 경로 확인(이미 ModelDiscount/VendorDiscount는 `model/option.go` switch에서 처리되나, pricing 캐시 무효화가 트리거되는지 점검 — 필요 시 해당 옵션 갱신 시 무효화 추가) |
| 동적가격 모델의 취소선 가격 부정확 | 동적가격은 배지만 표시(취소선 생략) |
| 할인가 반올림/표기 불일치 | 기존 `formatCurrencyFromUSD` 자릿수 규칙 재사용, 원가·할인가 동일 포맷 |
| 그룹비율과 할인 이중 표기 혼동 | 표시가는 현행(최소 그룹비율) 위에 할인만 곱 — 과금 합성과 동일하게 1회씩 |

## 8. 주의: pricing 캐시 무효화
옵션(`ModelDiscount`/`VendorDiscount`) 변경 시 pricing 캐시(`pricingMap`/`modelVendorNameCache`)가 무효화되어 새 할인%가 반영돼야 한다. 구현 시 옵션 갱신 경로에서 `model.InvalidatePricingCache()`가 호출되는지 확인하고, 누락 시 `ModelDiscount`/`VendorDiscount` 갱신 후 무효화를 추가한다.
