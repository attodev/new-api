# 모델 / 벤더 할인 — 설계 문서

> 대상: new-api 가격(ratio) 시스템의 "할인" 레이어 (백엔드 과금 + 가격 페이지 표시 + 사용 로그 계산식)
> 최종 정리: 2026-06-25
> 관리자 사용법은 [admin-guide.md](./admin-guide.md) 참고.

## 1. 개념

기존 가격 체계는 다음으로 구성된다.

- **모델별 가격**: `ModelRatio`(토큰 단가 배수), `ModelPrice`(요청당 정액), `Completion/Cache/Audio/Image Ratio` — "정가"를 정하는 값.
- **그룹 배수**: `GroupRatio`, `GroupGroupRatio` — **그룹(사용자 묶음)** 단위로 **모든 모델에 일괄** 적용되는 배수.

여기에 **"할인" 레이어** 두 가지를 추가한다(정가는 그대로 두고 할인만 별도로 켜고/끔).

- **모델 할인 (Model discount)**: 특정 **모델**에 X% 할인.
- **벤더 할인 (Vendor discount)**: 특정 **벤더(provider)** 의 모델에 X% 할인.

### 우선순위 (핵심)
한 모델에 적용되는 **유효 할인**은 다음 순서로 단 하나만 결정된다.

1. 그 모델에 **모델 할인**이 설정돼 있으면 → 모델 할인 사용
2. 없으면, 그 모델의 **벤더 할인**이 설정돼 있으면 → 벤더 할인 사용
3. 둘 다 없으면 → 할인 없음(배수 1.0)

즉 모델 할인이 벤더 할인을 **덮어쓴다**(모델 > 벤더 > 없음). 할인은 **전역**(모든 사용자)에 적용된다.

### 값
- 입력/저장은 **할인율 %(0~100)**. (100% = 무료까지 허용)
- 내부 배수 = `(100 - percent) / 100` (예: 20% → 0.8, 30% → 0.7).

## 2. 저장 / 설정

- 옵션 키 **`ModelDiscount`**: JSON `{ "<모델명>": <percent> }`
- 옵션 키 **`VendorDiscount`**: JSON `{ "<벤더명>": <percent> }`
- 둘 다 `options` 테이블에 JSON 문자열로 저장(크로스-DB 안전).
- 정의/접근자: `setting/ratio_setting/model_discount.go`
  - `GetModelDiscountMultiplier` / `GetVendorDiscountMultiplier` → 배수(+존재 여부)
  - `GetModelDiscountPercent` / `GetVendorDiscountPercent` → 저장된 %(가격 페이지 표시용)
  - `GetEffectiveDiscountMultiplier(model)` → 모델>벤더>1.0 우선순위 적용한 최종 배수
  - 벤더 해석(모델→벤더명)은 `model` 패키지가 주입하는 resolver(`SetVendorResolver`)로 처리 — 순환 의존 회피, 벤더 할인이 하나도 없으면 resolver 호출 자체를 건너뜀(hot-path 비용/캐시 미준비 방지).
- 옵션 연동: `model/option.go`(OptionMap 기본값 + 업데이트 switch), `controller/option.go`. 할인 옵션 변경 시 `InvalidatePricingCache()`로 가격 캐시 즉시 무효화.

## 3. 과금 통합

핵심 설계: **유효 할인 배수를 `HandleGroupRatio`(GroupRatioInfo를 만드는 단일 지점)에서 `GroupRatio`에 한 번 접어 넣는다(fold).**

`relay/helper/price.go`:
```
groupRatioInfo.GroupRatio *= GetEffectiveDiscountMultiplier(model)
groupRatioInfo.DiscountMultiplier = mul
groupRatioInfo.DiscountSource     = "model" | "vendor" | ""
```

이렇게 하면:
- **사전 차감(pre-consume)·텍스트 정산·expr·per-call(MJ)** 이 모두 같은 `GroupRatioInfo`를 공유 → **사전차감 == 정산**(과/소청구 없음).
- 재시도 시 `controller/relay.go`가 `HandleGroupRatio`로 재계산해도 할인이 유지됨(재시도 안전).
- `GroupRatio`와 **곱연산으로 스택**(그룹 할인 × 모델/벤더 할인 동시 적용).
- 세 가지 가격 모드 공통:
  ```
  토큰:  quota = tokens × ModelRatio × CompletionRatio × GroupRatio × Discount × QuotaPerUnit
  정액:  quota = ModelPrice × GroupRatio × Discount × QuotaPerUnit
  expr:  quota = (rawCost/1e6) × GroupRatio × Discount × QuotaPerUnit
  ```

`HandleGroupRatio`를 거치지 않고 `GetGroupRatio`를 직접 재계산하는 경로(WSS 실시간, 태스크, 툴 과금)는 해당 지점에서 `GetEffectiveDiscountMultiplier(model)`를 **명시적으로** 곱해 일관성을 맞춘다(`service/quota.go`, `service/task_billing.go`, `service/tool_billing.go`). 각 경로는 할인을 **정확히 한 번** 적용한다.

## 4. 가격 페이지(마켓플레이스) 표시

백엔드 `GetPricing`(`model/pricing.go`)이 응답에 할인%를 실어 보내고, 프론트는 렌더링만 한다.

- `Pricing.DiscountPercent` = 모델 **유효 할인%**(모델>벤더).
- `PricingVendor.DiscountPercent` = 그 벤더 **자체 할인%**.
- 프론트(`web/default/src/features/pricing/`):
  - **벤더 사이드바**: 벤더 칩에 `X% off` 배지.
  - **모델 카드 / 테이블**: `~~원가~~ 할인가`(취소선 원가 + 할인 적용가) + `X% off` 배지. 할인가 = 표시가 × (1−%/100).
  - 카드에서는 할인 배지를 "종량제(Token-based)" 줄 오른쪽 끝에 표시.
  - **히트맵 색상**(`lib/discount-color.ts`): 1–14% 에메랄드, 15–29% 앰버, 30–49% 오렌지, 50%+ 빨강.
  - 동적가격(billingexpr) 모델: 고정 $/M가 없으므로 **배지만**(취소선 가격 생략). 할인 0%면 표시 없음.

## 5. 사용 로그 계산식 표시

할인이 `group_ratio`에 접혀 있으면 로그에서 "그룹 할인"처럼 보이므로, 로그에는 **분리**해 기록한다.

- `service/text_quota.go`: `other`에 `discount_multiplier`·`discount_source`("model"/"vendor")를 추가하고, `group_ratio`는 **할인 제외 기저값**(`folded / multiplier`)으로 환원해 기록. `user_group_ratio`는 이미 기저값.
- 프론트(`usage-logs` 상세 → "Show formula"): 각 줄을 `… × Group Ratio {기저} × {Model|Vendor} discount {배수} = 금액` 형태로 표기.
  - 그룹비율이 기본값(1)이면 `× Group Ratio 1`은 **생략**.
  - 할인이 없으면 할인 항 생략.
  - 표시 금액 = 실제 청구 quota와 일치(배수를 그대로 곱하므로 부동소수점 오차 없음).

예: `Input: 131 / 1M × $100 × Model discount 0.7 = $0.00917`

> 주의: 로그 형식 변경은 **새 요청부터** 적용된다(과거 로그는 옛 형식 유지). 동적가격 모델은 배지만 표시되고 expr 단가는 재계산하지 않는다.

## 6. 파일 맵

| 영역 | 파일 |
|---|---|
| 할인 설정/접근자/유효배수 | `setting/ratio_setting/model_discount.go` |
| 과금 fold(단일 지점) | `relay/helper/price.go` (`HandleGroupRatio`) |
| 재계산 경로 할인 적용 | `service/quota.go`, `service/task_billing.go`, `service/tool_billing.go` |
| 옵션 연동 | `model/option.go`, `controller/option.go` |
| 가격 API 할인% + 모델→벤더 캐시 | `model/pricing.go` |
| 가격 페이지 표시 | `web/default/src/features/pricing/` (`lib/price.ts`, `lib/discount-color.ts`, `components/model-card.tsx`, `pricing-columns.tsx`, `pricing-sidebar.tsx`, `components/model-details.tsx`) |
| 사용 로그 계산식 | `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx`, `types.ts` |

## 7. 동작 요약 / 불변식

- 할인은 **모델>벤더>없음** 우선순위로 모델당 하나만 적용.
- 모든 과금 식 = `… × GroupRatio × Discount × …`, 할인은 **경로마다 정확히 한 번**, 사전차감==정산.
- 정가(`ModelRatio`/`ModelPrice`)는 변경하지 않음 — 할인은 별도 레이어라 켜고/끄기 자유.
- 옵션 변경 시 가격 캐시 즉시 무효화 → 마켓플레이스/과금에 곧바로 반영.
