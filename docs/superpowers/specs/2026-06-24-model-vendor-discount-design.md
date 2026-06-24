# 모델별 / 벤더별 할인 기능 설계

> 작성일: 2026-06-24
> 대상: 모델 가격(ratio) 시스템에 "할인" 레이어 추가
> 관련 코드: `setting/ratio_setting/`, `relay/helper/price.go`, `service/quota.go`, `model/option.go`, `model/pricing.go`

## 1. 배경 / 문제

현재 가격 시스템에는 다음만 존재한다.

- **모델별 가격**: `ModelRatio`(토큰 단가 배수), `ModelPrice`(요청당 정액), `CompletionRatio`/`Cache`/`Audio`/`Image` 비율 — 전부 "가격을 정하는" 값.
- **할인(배수)**: `GroupRatio`, `GroupGroupRatio` — **그룹(사용자 묶음)** 단위로 **모든 모델에 일괄** 적용.

즉 "**특정 모델만 할인**"하거나 "**특정 벤더의 모델을 할인**"하는 레이어가 없다. 모델 단가(`ModelRatio`)를 직접 낮추는 방법뿐인데, 이는 정가를 잃고 프로모션처럼 켜고/끄기가 어렵다.

## 2. 목표

- **모델별 할인율**과 **벤더별 할인율**을 별도 레이어로 추가한다(정가는 그대로 두고 할인만 적용/해제 가능).
- 할인 적용 우선순위: **모델 할인이 있으면 모델 할인, 없으면 벤더 할인, 둘 다 없으면 할인 없음.** (모델·벤더 동시 적용하지 않음 — 둘 중 하나만)
- 할인은 **전역**(모든 사용자)에 적용된다(특정 그룹/사용자 한정 아님).
- 모든 가격 모드(토큰 `ModelRatio`, 정액 `ModelPrice`, 동적 `billingexpr`)에 동일하게 적용된다.
- **사전 차감(pre-consume)과 최종 정산(settlement)이 동일한 할인을 적용**하여 과/소청구가 없어야 한다.

### 비목표 (YAGNI)

- 그룹별/사용자별/조직별 모델 할인 — 범위 밖(요청은 전역 모델·벤더 할인).
- 기간 한정(시간 기반) 프로모션 — 범위 밖(필요 시 billingexpr로 가능).
- `ModelRatio` 등 기존 값의 의미 변경 — 손대지 않는다.

## 3. 결정 사항

- 할인 값은 **할인율 %(0~100)** 로 입력·저장한다. 내부 배수 = `(100 - percent) / 100`. (예: 20 → 0.8)
- **100% 할인(무료) 허용** — 검증 범위 `0 ≤ percent ≤ 100`.
- 벤더 할인은 **벤더명(string)** 으로 키잉한다(그룹비율과 일관, 편집 용이). 벤더명 변경 시 할인 재설정이 필요하다는 점을 UI/문서에 명시.
- 모델 할인은 **모델명(string)** 으로 키잉하며, `ModelRatio`와 동일하게 `FormatMatchingModelName`으로 정규화 후 정확 매칭한다.

## 4. 접근 방식 (대안 비교)

- **A. 별도 `ModelDiscount`/`VendorDiscount` 옵션 레이어 (채택)** — 새 옵션 맵을 두고 과금의 "유효 배수"에 1개만 곱한다. 정가 보존, 켜고/끄기 용이, 로그/정산 일관.
- B. `ModelRatio` 직접 하향 — 정가 손실, 프로모션/복원 개념 없음 → 기각.
- C. `billingexpr` 표현식에 할인 인코딩 — 단순 %할인엔 과하고 재사용 토글 불가 → 기각.

## 5. 상세 설계

### 5.1 설정 레이어 (`setting/ratio_setting/model_discount.go`, 신규)

`group_ratio.go`/`model_ratio.go` 패턴을 따른다.

```go
// 둘 다 percent(0~100) 저장. 기본 빈 맵.
var modelDiscountMap  // map[string]float64 (model name -> percent)
var vendorDiscountMap // map[string]float64 (vendor name -> percent)

func ModelDiscount2JSONString() string
func VendorDiscount2JSONString() string
func UpdateModelDiscountByJSONString(jsonStr string) error   // 검증: 0<=v<=100
func UpdateVendorDiscountByJSONString(jsonStr string) error  // 검증: 0<=v<=100
func GetModelDiscountCopy() map[string]float64
func GetVendorDiscountCopy() map[string]float64

// percent -> multiplier. 항목 없으면 ok=false.
func GetModelDiscountMultiplier(modelName string) (float64, bool)
func GetVendorDiscountMultiplier(vendorName string) (float64, bool)
```

### 5.2 유효 할인 배수 결정

벤더 해석에는 모델명→벤더명 매핑이 필요하다. `model/pricing.go`의 캐시(`GetPricing()` → `ModelName`+`VendorID`, `GetVendors()` → id→name)를 활용하거나, 전용 경량 캐시(`map[modelName]vendorName`)를 만들어 모델/벤더 변경 시 무효화한다(billing은 hot-path이므로 요청마다 DB 조회 금지).

```go
// 우선순위: 모델 > 벤더 > 없음(1.0)
func GetEffectiveDiscountMultiplier(modelName string) float64 {
    if mul, ok := GetModelDiscountMultiplier(modelName); ok {
        return mul
    }
    if vendor, ok := lookupVendorName(modelName); ok {
        if mul, ok := GetVendorDiscountMultiplier(vendor); ok {
            return mul
        }
    }
    return 1.0
}
```

`lookupVendorName(modelName)`은 캐시 기반. 매핑이 없으면 벤더 할인 미적용.

### 5.3 과금 통합 (사전차감 == 정산)

할인 배수를 **공유 price 계산에 한 번** 접어 넣어 사전차감(`relay/helper/price.go`)과 정산(`service/quota.go`)이 동일 값을 쓰게 한다. 가능하면 price 계산이 만들어 `relayInfo`/price 구조체로 전달하는 기존 `GroupRatio`와 같은 자리에 `DiscountMultiplier`를 추가해 한 곳에서 계산하고 양쪽에서 참조한다(중복 계산 금지).

최종 식(세 모드):
```
토큰:  quota = tokens × ModelRatio × CompletionRatio × GroupRatio × DiscountMul × QuotaPerUnit
정액:  quota = ModelPrice × GroupRatio × DiscountMul × QuotaPerUnit
expr:  quota = (rawCost / 1e6) × GroupRatio × DiscountMul × QuotaPerUnit
```
`DiscountMul`은 §5.2로 결정된 1개 값. `GroupRatio`와는 곱연산으로 스택.

적용 지점(현행 코드 기준):
- `relay/helper/price.go`: `ratio := modelRatio * groupRatioInfo.GroupRatio`(토큰), `modelPrice * QuotaPerUnit * GroupRatio`(정액), expr 경로 `... * GroupRatio`(line ~264) 각각에 `* DiscountMul` 추가.
- `service/quota.go`: `ratio := groupRatio.Mul(modelRatio)` 정산식에 `DiscountMul` 반영.

### 5.4 옵션 연동 (관리자 저장/로드)

`ModelRatio` 등과 동일 경로:
- `model/option.go`:
  - 기본 등록: `common.OptionMap["ModelDiscount"] = ratio_setting.ModelDiscount2JSONString()`, `["VendorDiscount"] = ...`.
  - 업데이트 switch: `case "ModelDiscount": UpdateModelDiscountByJSONString(value)`, `case "VendorDiscount": ...`.
- `controller/option.go`: 관리 옵션 키 목록에 `"ModelDiscount"`, `"VendorDiscount"` 추가.

### 5.5 관리자 UI (`web/default/src/features/system-settings/models/`)

- **모델 할인 에디터** (`model-discount-visual-editor.tsx`, 신규): 모델명 + 할인율% 입력 행 목록 + JSON 모드. `group-ratio-visual-editor.tsx` 미러.
- **벤더 할인 에디터** (`vendor-discount-visual-editor.tsx`, 신규): 벤더 선택(기존 벤더 목록 API 활용) + 할인율% + JSON 모드.
- 시스템 설정 모델 섹션에 두 에디터 노출. 저장은 `UpdateOption`로 각각 `ModelDiscount`/`VendorDiscount` 키 전송.

### 5.6 투명성 / 로그

`DiscountMul`이 유효 배수에 접혀 들어가므로 기존 usage-log의 비율/청구식 표시에 자동 반영된다. (선택: 로그 상세에 "적용 할인(모델/벤더, %)"을 별도 표기 — 후속 가능, 본 범위에선 비목표.)

## 6. 테스트 / 검증

- **Go 단위 테스트** (`setting/ratio_setting/model_discount_test.go`):
  - percent→multiplier 변환, 경계값(0, 100).
  - `GetEffectiveDiscountMultiplier` 우선순위: 모델만/벤더만/둘다(모델 우선)/없음(1.0).
  - 잘못된 값(음수, >100) 거부.
- **과금 일치 테스트**: 동일 입력에 대해 사전차감과 정산의 할인 적용이 일치(세 모드).
- **벤더 매핑**: 모델→벤더 캐시 조회, 매핑 없을 때 벤더 할인 미적용.
- 크로스-DB 영향 없음(옵션 JSON 저장, GORM 옵션 테이블 사용).

## 7. 위험 / 주의

| 위험 | 완화 |
|---|---|
| 사전차감과 정산의 할인 불일치 → 과/소청구 | 할인 배수를 공유 price 계산에서 1회 산출, 양쪽이 동일 값 참조 |
| billing hot-path에서 모델→벤더 DB 조회로 지연 | 캐시(pricing/vendors 또는 전용 맵), 변경 시 무효화 |
| 벤더명 변경 시 할인 키 불일치(orphan) | UI/문서 경고; 필요 시 후속으로 ID 키 전환 검토 |
| 100% 할인 오용(무료) | 관리자 전용 + 명시적 입력. 정책상 허용 |
| 모델·벤더 할인 동시 기대 | 명세상 "둘 중 하나(모델 우선)"임을 UI 문구로 안내 |

## 8. 문서

구현 후 모델 가격/할인 관련 사용자 문서(있다면)와 시스템 설정 화면 설명에 두 할인 레이어와 우선순위를 기술한다.
