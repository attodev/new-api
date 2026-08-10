# 쿼터 상한 인상 및 bigint 확장

## 1. 개요

조직 쿼터 저장 실패(`quota must be between 0 and 1000000000`)를 해결하는 과정에서, 쿼터 상한과 저장 타입을 함께 변경했다. 이 문서는 무엇을 왜 어떻게 바꿨는지, 그리고 검토 과정에서 발견해 되돌린 판단까지 기록한다.

**커밋 3개** (기준: `ee27c8a1`)

| 커밋 | 내용 |
|---|---|
| `17096f1a` | 쿼터 상한 인상 + 쿼터 컬럼 bigint 확장 (백엔드) |
| `2ea7b204` | 숫자 약어 표기 단계 추가 (프론트엔드) |
| `2ce03042` | Toss 1회 결제 한도 안내 (프론트엔드) |

전체 63개 파일, +476/-356. 세 커밋 모두 단독으로 빌드된다.

---

## 2. 왜 바꿨는가

### 2.1 최초 증상

조직 사용자 화면에서 특정 멤버의 쿼터를 수정하고 저장하면 실패했다.

```
quota must be between 0 and 1000000000
```

`MaxOrganizationQuota = 1_000_000_000`이 원인이었다. 기본 `QuotaPerUnit = 500,000`(원시 단위 = $1)으로 환산하면 **$2,000**에 불과해, 실사용에 턱없이 부족했다.

### 2.2 단순히 상수만 올릴 수 없는 이유

상한을 1e9 초과로 올리면 32비트 컬럼(`int`, 최대 약 21.4억)을 넘는다. 즉 **상한과 저장 타입은 함께 움직여야 한다.**

- PostgreSQL: `ERROR: integer out of range` → 저장 실패
- MySQL (비엄격 모드): 조용히 2147483647로 절삭
- SQLite: `INTEGER`가 8바이트라 정상 동작

같은 값이 DB에 따라 저장되거나 잘리거나 실패한다. `CLAUDE.md` Rule 2(세 DB 동시 호환) 위반이다.

### 2.3 실제로 편집하던 필드는 `users.quota`였다

조사 중 확인한 핵심 사실이다. 조직 사용자 테이블의 쿼터 수정은 `PATCH /api/organization/users/:id` → `UpdateOrganizationUser`로 가고, 이 핸들러는 `updates["quota"]`를 **`users` 테이블**에 쓴다. `organizations.quota`가 아니다.

즉 조직 쿼터만 확장해도 최초 증상은 해결되지 않았다. `users.quota`도 함께 확장해야 했고, 이것이 변경 규모가 커진 직접 원인이다.

---

## 3. 무엇을 바꿨는가

### 3.1 단일 상한 상수

`common/constants.go`에 하나만 정의하고 검증 계층이 이를 참조한다.

```go
const MaxQuota int64 = 500_000_000_000_000 // 기본 QuotaPerUnit에서 정확히 $1B
```

```go
// controller/organization_validation.go
MaxOrganizationQuota = common.MaxQuota
```

**5e14를 고른 이유.** 컬럼이 bigint(최대 약 9.22e18)이므로 제약은 DB가 아니라 **프론트엔드**다. 쿼터는 평범한 JSON 숫자로 전송되고, JavaScript는 `Number.MAX_SAFE_INTEGER`(9,007,199,254,740,991) 위에서 정수 정밀도를 잃는다.

처음에는 9e15로 잡았으나 이는 MAX_SAFE_INTEGER의 99.9%로 여유가 0.1%뿐이었다. 5e14는 **약 18배 여유**를 남긴다.

| 값 | 달러 환산 | MAX_SAFE_INTEGER 대비 여유 |
|---|---|---|
| 9e15 (초기안) | $18,000,000,000 | 1.001배 |
| **5e14 (채택)** | **$1,000,000,000** | **18.0배** |

이 여유가 클라이언트 산술을 안전하게 만든다 — 대시보드가 여러 잔액을 합산해도, 쿼터 입력 UI가 입력값에 `QuotaPerUnit`을 곱해도 중간 결과가 정밀도를 잃지 않는다. 5e14는 `$1B`와 정확히 왕복 변환된다(`5e14 / 500000 = 1e9`, `1e9 * 500000 = 5e14`, 둘 다 오차 없음).

### 3.2 int64 / bigint로 확장한 필드

```go
// model/organization.go
Quota       int64 `gorm:"type:bigint;default:0"`
UsedQuota   int64 `gorm:"type:bigint;default:0;column:used_quota"`

// model/user.go
Quota            int64 `gorm:"type:bigint;default:0"`
UsedQuota        int64 `gorm:"type:bigint;default:0;column:used_quota"`
AffQuota         int64 `gorm:"type:bigint;default:0;column:aff_quota"`
AffHistoryQuota  int64 `gorm:"type:bigint;default:0;column:aff_history"`
```

추가로 대시보드 응답 구조체, `WalletAutoRecharge(.Preset).ThresholdQuota`, 조직 쿼터 접근 함수들.

**`AffQuota`를 함께 확장한 이유.** `TransferAffQuotaToQuota`가 한 트랜잭션 안에서 `AffQuota`와 `Quota` 사이로 값을 직접 이동시킨다. 넓은 컬럼에서 좁은 컬럼으로 값이 옮겨지는 구조를 남길 수 없다. `AffCount`는 개수이므로 `int` 유지.

**요청 DTO는 포인터 유지.** `CLAUDE.md` Rule 6에 따라 `*int64 + omitempty`로 두어, 명시적 `"quota": 0`이 upstream에서 유실되지 않는다.

### 3.3 타입 확장의 파급

`User.Quota`는 코드베이스 전반에서 쓰이므로 컴파일러가 연쇄적으로 끌고 갔다.

| 영역 | 파일 수 | 성격 |
|---|---|---|
| 조직 쿼터 | 8 | 최초 요청 범위 |
| 사용자 쿼터 + 공통 | 10 | `user.go`, `option.go`, `logger`, `relay`, 컨텍스트 키 |
| 결제/충전 | 15 | 타입 전파, 상한 분리 |
| service 계층 | 12 | 정산·환불·export |
| 프론트엔드 | 8 | 표기, Toss 안내 |
| (테스트) | 17 | |

**이 파급은 축소가 아니라 정리 방향이었다.** 좁히는 캐스팅을 추가한 게 아니라 제거했다:

- `service/funding_source.go`의 `int(delta)` 캐스팅 — 양쪽이 int64가 되어 삭제
- `logger.FormatQuota`/`LogQuota`가 int64를 받게 되어 추가로 여러 개 삭제
- `model/subscription.go`의 `int(decimal.IntPart())` 좁히기 삭제

`Token.RemainQuota`는 **별개 단위 도메인**이라 `int`로 남기고, 경계에서 명시적으로 캐스팅한다. `Channel.UsedQuota` 컬럼은 원래부터 `int64`였고, `UpdateChannelUsedQuota(id int, quota int)`의 파라미터만 `int`로 유지해 배치 저장소 경계에서 확대 캐스팅한다.

### 3.4 옵션 파싱

쿼터 단위 설정값이 int64가 되면서 `model/option.go`의 파싱을 바꿨다.

```go
// strconv.Atoi / Itoa  →  strconv.ParseInt(v, 10, 64) / FormatInt
QuotaForNewUser, QuotaForInviter, QuotaForInvitee, QuotaRemindThreshold
```

`PreConsumedQuota`와 레이트리밋 키는 `Atoi` 그대로 둔다(해당 도메인 아님).

---

## 4. 상한을 적용하지 않은 곳 (의도적)

### 4.1 결제 금액 — 별도 상한으로 분리

```go
MaxTopUpAmount int64 = 1_000_000_000  // 기존 값 유지
```

`rejectTopUpAmountTooLarge`가 `MaxOrganizationQuota`를 공유하고 있었다. 상한을 올리면서 **결제 게이트웨이로 넘어가는 금액의 가드가 1e9 → 9e15로 6자리 함께 완화**되는 문제가 있었다. 이 값은 쿼터가 아니라 통화 단위이고 이후 `QuotaPerUnit`이 곱해지므로, 별도 상수로 분리해 원래 값을 복원했다.

호출처: `controller/topup.go`, `topup_waffo.go`, `topup_waffo_pancake.go` (총 6곳)

### 4.2 Toss 1회 결제 한도 — `math.MaxInt32` 유지

`tossTopUpPositiveInt`의 상한 2,147,483,647은 **컬럼 제약이 아니라 1회 결제 정책**이다. 유지했고, "user/organization 쿼터 컬럼이 SQL INT이므로"라는 이제 거짓이 된 주석을 사실에 맞게 수정했다.

### 4.3 증가 함수 — 상한 검사 없음

이 판단은 한 번 잘못 넣었다가 검토에서 되돌린 것이다. 자세한 경위는 6절 참조.

```go
// increaseUserQuota / IncreaseOrganizationQuota — 상한 검사 없음
func increaseUserQuota(id int, quota int64) (err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota)).Error
	...
}
```

**상한이 강제되는 곳:**
- 요청 검증 (`validateInt64Range`, `controller/organization.go` 3곳 + `organization_subscription.go`)
- 결제 유입 (`CreditTopUpTarget`의 기존 원자적 가드 — `math.MaxInt32` → `common.MaxQuota`로 변경)

**강제되지 않는 곳 (알려진 공백).** 관리자 부여 경로 `ManageUser`의 `add_quota`/`override`([controller/user.go:944](../controller/user.go), [:967](../controller/user.go))는 부호만 검사하고 상한을 보지 않는다. 그래서 root 관리자는 `common.MaxQuota`를 넘는, 나아가 `Number.MAX_SAFE_INTEGER`를 넘는 잔액을 쓸 수 있고 이후 프론트가 그 값을 부정확하게 읽는다.

이 컬럼을 bigint로 확장하면서 **MySQL/PostgreSQL이 제공하던 out-of-range 거부가 사라졌다** — 검증이 없던 이 경로에서는 그것이 사실상 유일한 방어였다. SQLite는 `INTEGER`가 8바이트라 원래부터 무제한이었으므로 현 배포 환경에서 달라진 것은 없고, PG/MySQL로 전환할 때 드러난다. root 권한이 필요하므로 공격 경로가 아니라 footgun이다. 9절 참조.

---

## 5. 프론트엔드 변경

### 5.1 숫자 약어 표기 단계 추가 (`2ea7b204`)

상한이 커지자 약어 접미사가 고갈되어 실제로 사용자에게 보이는 버그가 났다.

| 함수 | 파일 | 이전 | 이후 |
|---|---|---|---|
| `formatQuota` (TOKENS 모드) | `lib/currency.ts` | `9000000000000k` | `9000T` |
| `formatTokens` | `lib/format.ts` | `9000000000.00M` | `9000.00T` |
| `renderNumber` | `classic/helpers/render.jsx` | `9000000.0B` | `9000.0T` |

`currency.ts`는 `/1000` 하드코딩을 단계 테이블로 바꿨다. `Intl`의 compact notation을 쓰지 않은 이유는 중간 범위 출력이 바뀌기 때문이다 — 소문자 `k`가 대문자 `K`로 변하고, 고정 소수점 자릿수(`1.00M`)를 잃는다.

`formatLogQuota`는 `abbreviate: false`이므로 손대지 않았다(정확한 값 유지).

**불가피한 변경 1건**: `currency.ts` TOKENS 모드에서 1e6이 `1000k` → `1M`. M 단계를 추가하면 필연적이며, k를 1e9까지 유지하면 같은 버그가 한 단계 위에서 재발한다.

### 5.2 Toss 1회 결제 한도 안내 (`2ce03042`)

한도(2,147,483,647원)가 서버와 `isValidTossPaymentSession`에서만 강제되고 **구매자에게 표시되지 않았다.** 제출해서 오류를 읽어야만 알 수 있었다.

금액 입력란 아래에 상시 안내를 넣고, 초과 시 같은 줄이 경고로 바뀐다.

**새 API 필드를 만들지 않았다.** 이미 같은 값을 담고 있던 `TOSS_MAXIMUM_CHARGE_KRW` 상수를 재사용해 동기화 대상을 늘리지 않았고, 기존 검사 로직은 그대로 두었다.

**KRW 모드에서만 경고한다.** quota 모드의 KRW 청구액은 사용자의 그룹 비율과 금액 할인으로 서버가 산출하며 브라우저 프리뷰가 이를 재현할 수 없다. 기존 `shouldBlockPaymentMethodBeforeQuote`가 같은 이유로 Toss 최소금액을 프리뷰로 판단하지 않는다 — 동일 원칙을 따랐다. 없는 숫자로 추측해 정상 결제를 막는 것을 피한다.

i18n은 `en.json` / `kr.json` 2개 파일(이 저장소의 실제 로케일).

---

## 6. 검토에서 되돌린 판단 — 누적 상한

기록으로 남길 가치가 있는 실수다.

### 6.1 무엇을 넣었나

증가 함수에 WHERE 절 기반 원자적 상한을 추가했다. 반복 충전으로 누적값이 상한을 넘어 MAX_SAFE_INTEGER를 초과하는 것을 막으려는 의도였다.

```go
// 되돌린 코드
Where("id = ? AND quota <= ?", id, common.MaxQuota-quota)
// RowsAffected == 0 이면 capacity 오류 반환
```

### 6.2 왜 위험했나

`IncreaseUserQuota` 호출처를 전수 조사하니 **대부분이 환불·보상 경로**였다.

- `service/funding_source.go` — 지갑 환불, 예약 롤백, 보상 크레딧
- `service/billing_session.go` — relay 정산 차액
- `controller/task_video.go`, `controller/midjourney.go` — 작업 실패 환불

**환불이 적용되지 못하면 사용자가 이미 지불한 쿼터가 사라진다.** 막으려던 정밀도 드리프트보다 심각하다.

추가로 발견한 문제들:

1. **Redis 캐시 불일치** — `IncreaseUserQuota`는 `cacheIncrUserQuota`를 DB 쓰기 **전에** goroutine으로 띄운다. DB만 거부되면 캐시에는 증가가 남아, relay 핫패스가 캐시를 신뢰하므로 **사용 가능한 유령 쿼터**가 생긴다.
2. **배치 업데이트에서 오류 유실** — `model/utils.go`의 `batchUpdate()`는 오류를 `SysLog`만 하고 버린다. `BatchUpdateEnabled` 상태에서 환불이 조용히 사라진다.
3. **정확히 상한값에서 영구 고착** — 검증이 상한을 포함(inclusive)해 허용하므로, 잔액이 정확히 `MaxQuota`가 되면 이후 모든 증가(환불 포함)가 영구 실패한다.
4. **MySQL `RowsAffected` 의미 차이** — DSN에 `clientFoundRows`가 없어 MySQL은 *변경된* 행을 보고한다. 순증감이 0인 배치는 `quota + 0`이 되어 `RowsAffected == 0` → 허위 capacity 오류.

### 6.3 결론

상한은 **새 쿼터가 실제로 유입되는 지점**에만 두는 것이 옳다. 증가 함수는 원래대로 되돌리고, 그 이유를 코드 주석으로 남겼다. 상한은 요청 검증과 `CreditTopUpTarget`(이미 검증된 원자적 패턴)이 담당한다.

---

## 7. 마이그레이션

**수동 DDL은 필요 없다.** GORM `AutoMigrate`가 세 DB 모두에서 `int → bigint` 확장을 처리한다.

| DB | 동작 | 비고 |
|---|---|---|
| SQLite | 임시 테이블 생성 → 복사 → 교체 | 트랜잭션으로 감싸여 중단 시 롤백. 컬럼당 1회 재생성 |
| PostgreSQL | `ALTER TABLE ... TYPE bigint USING ...::bigint` | 안전한 확대. 전체 재작성 + ACCESS EXCLUSIVE 락 |
| MySQL | `MODIFY COLUMN ... bigint` | 안전한 확대. `ALGORITHM=COPY`로 DML 차단 |

**SQLite에서 실증 확인**: 구 스키마(`quota integer`)에 큰 값과 음수를 넣고 `AutoMigrate` 실행 → 행이 바이트 단위로 보존되고 인덱스도 유지되며, 2차 실행 시 DDL 미발생(idempotent).

**운영 주의**: MySQL/PostgreSQL은 커밋 후 첫 기동에서 `users`, `organizations`, `wallet_auto_recharge*` 테이블에 차단성 재작성이 발생한다. 해당 테이블들은 작아서 실무상 무의미하지만 릴리스 노트에 넣을 만하다.

배포 환경(SQLite)에서 실제 전환 확인:

```
users          quota: bigint, used_quota: bigint, aff_quota: bigint
organizations  quota: bigint, used_quota: bigint
```

---

## 8. 검증

| 항목 | 결과 |
|---|---|
| `go build ./...` (amd64) | 통과 |
| `GOARCH=386` / `GOARCH=arm64` | 통과 |
| 커밋별 단독 빌드 | 3개 모두 통과 |
| `go test ./controller/ ./service/` | 통과 |
| `go test ./model/` | 1건 실패 — **기존 버그** (아래) |
| 프론트 테스트 (`bun test src`) | 133 통과 |
| 프론트 typecheck | 오류 23건 = 기준선 동일, 변경 파일엔 없음 |
| 양쪽 프론트 빌드 | 통과 |
| 서비스 재시작 | HTTP 200, 마이그레이션 오류 없음 |

**32비트 빌드를 확인 이유가 있다.** 실제 버그를 잡았다 — `fmt.Errorf("%d", MaxOrganizationQuota)`에서 무타입 상수가 `int`로 기본 지정되어 `GOARCH=386` 컴파일이 깨졌다. 변경 전 HEAD에서는 정상이었으므로 회귀였다. 상한 상수를 `int64`로 명시해 해결했다.

### 사전 존재 실패 (이 변경과 무관)

변경 전 HEAD에서 재현을 확인한 것들이다.

- `model`: `TestWalletAutoRechargePendingScannerDetectsLocalAndUTCBridgeConflict` — 타임존 브리지 로직 버그
- `relay/channel/claude`: `TestRequestOpenAI2ClaudeMessage_*` 3건
- `relay/helper`: `TestStreamScannerHandler_*`
- `gofmt`: `model/checkin.go`, `service/billing_session.go`

---

## 9. 남은 과제

1. **`model` 타임존 브리지 테스트 실패** — `walletAutoRechargeAttemptIdentitiesForPolicy`의 기존 버그. 별도 추적 필요.
2. **증가 함수의 누적 상한** — 6절 참조. 환불 경로를 깨지 않으면서 누적을 제한하려면, 환불과 신규 크레딧을 별도 함수로 분리하고 후자에만 상한을 두는 설계가 필요하다.
3. **관리자 부여 경로의 상한 부재** — 4.3절의 "알려진 공백". `ManageUser`의 `add_quota`/`override`(`ManageRequest.Value`), `TransferAffQuotaToQuota`, `QuotaForNewUser` 옵션에 상한 검증이 없다. 반복 부여로 누적이 상한을 넘을 수 있고, `override`는 단발로도 넘길 수 있다. 조치는 `validateInt64Range("value", req.Value, 0, common.MaxQuota)`를 두 분기에 추가하는 것으로 충분하다. 현재는 우선순위를 낮춰 보류했다.
4. **`service/org_user_export.go`의 부동소수 변환** — `int(usd * common.QuotaPerUnit)`가 CSV 입력값에 대해 무제한이다. Go에서 범위를 벗어난 float→int 변환은 구현 정의 동작이다. 이 변경과 무관한 기존 문제.
5. **커밋 4분할 미달성** — 조직/사용자 쿼터 분리를 두 차례 시도했으나 중간 커밋이 컴파일되지 않았다. 타입 확장이 하나의 원자적 리팩터링이라(대시보드 사용량 필드, `logger.FormatQuota`, `service/*`가 동시에 바뀜) 파일 단위 분리가 불가능했다. 컴파일되지 않는 커밋은 롤백 용도로 무용하므로 빌드되는 3분할을 택했다.
