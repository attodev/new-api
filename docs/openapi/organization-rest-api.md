# 조직 관련 Backend REST API 명세

작성일: 2026-06-15  
최종 수정: 2026-10-14 (단계 C 작업 8: `assignable-users` 목록을 `users/lookup` 정확 조회로 바꾸고 개인 잔액·구독이 있는 사용자의 할당 거부를 추가. 이전 — upstream 재기반 단계 B: develop 라우트 38개 기준으로 정정 — 조직 삭제·Excel 내보내기/가져오기 추가, quota 상한 2^53-1, 소유자·자기 자신 대상 규칙, 접근 토큰 스코프, 감사 로그. 같은 날 최종 리뷰 수정: 비활성 조직 거부, 조직 이름 변경, 대기 충전 주문이 있는 조직 삭제 거부, 자기 플랜 할당·취소 금지, 가져오기의 actor 행 거부, 로그 기간 제한, 구독 응답의 사용자 제한 뷰)

## 1. 문서 범위

이 문서는 현재 구현된 조직 관리 관련 REST API를 정리한다. 기준 코드는 다음 파일이다.

- `router/organization-router.go` (조직 라우트 38개: `/api/organizations` 4개 + `/api/organization` 34개)
- `middleware/fork_access_token_routes.go` (접근 토큰 스코프)
- `controller/organization.go`, `controller/organization_validation.go`, `controller/organization_audit.go`
- `controller/organization_subscription.go`
- `controller/topup_target.go`, `controller/topup*.go`
- `model/organization.go`
- `model/organization_subscription.go`
- `service/org_user_export.go` (Excel)
- `common/page_info.go`

기본 prefix는 `/api`다. 이 문서에 적힌 엔드포인트는 `router/organization-router.go`의 38개와 정확히 일치한다(빠진 것도 남는 것도 없다).

응답은 대부분 다음 형태를 사용한다.

```json
{
  "success": true,
  "message": "",
  "data": {}
}
```

일부 결제 API는 기존 결제 API와 동일하게 다음 형태를 사용한다.

```json
{
  "message": "success",
  "data": {}
}
```

## 2. 공통 규칙

### 2.1 인증과 권한

| 권한 | 의미 | 주요 접근 범위 |
| --- | --- | --- |
| 전체 관리자 | `role = root` | `/api/organizations/*`(RootAuth + casbin `organization:read`/`organization:write`), 선택 조직 대상 `/api/organization/*` 조회·구독 관리·Excel |
| 조직 소유자 | `organization_role = owner` | 자기 조직 모든 관리, 조직 지갑 조회·충전, owner 행 관리 |
| 조직 관리자 | `organization_role = admin` | 멤버 추가(member/admin)·역할 변경(member↔admin)·제거, 사용자 관리, 대시보드/로그/구독 관리 |
| 조직 일반 사용자 | `organization_role = member` | 자기 활성 조직 구독 조회 등 제한된 API |

조직 관리자 권한은 `admin` 또는 `owner`다. 조직 소유자 전용 권한은 `owner`만 해당한다 (조직 지갑 조회·충전 API 전체).

공통 대상 규칙(멤버십·quota·상태 변경):

- 자기 자신은 대상이 될 수 없다 (`organization admins cannot modify their own membership`, 멤버십 PUT은 `organization admins cannot reassign themselves`). 자기 quota는 전체 관리자가 바꾼다.
- `owner` 역할 사용자를 대상으로 하는 변경은 그 owner 본인만 할 수 있다 (`only the organization owner can manage the owner`). 구독 할당·취소는 전체 관리자도 owner를 대상으로 할 수 있다.
- `owner` 역할은 어떤 API로도 부여할 수 없다. 소유권은 조직 생성 시 `owner_user_id`로만 정해지고, 소유권 이전은 아직 없다.
- 전역 관리자(`role >= admin`) 사용자는 조직 API로 관리할 수 없다.

전체 관리자가 `/api/organization/*` 계열의 조직별 화면 API를 호출할 때는 대부분 `organization_id` query가 필수다. 조직 소유자/관리자는 자신의 `organization_id`가 자동으로 사용된다.

비활성 조직(`status = 2`):

- 회원의 relay 요청은 과금 전에 HTTP 403 `organization is disabled`로 거부된다(레거시 Midjourney 포함).
- `/api/organization/*`의 쓰기 요청(GET이 아닌 모든 요청: 사용자 수정·멤버십·가져오기·plan·구독 할당/취소·조직 지갑 pay/amount)은 `{"success": false, "message": "organization is disabled"}`로 거부된다. 단일 지점은 `/api/organization` 그룹 미들웨어 `RejectDisabledOrganizationMutation`이다. 조회(GET)는 계속 된다.
- 전체 관리자는 영향이 없다(`/api/organizations/*`, `?organization_id=`로 비활성 조직을 관리). 다시 쓰려면 3.3으로 `status: 1`.
- 이미 결제된 조직 충전 주문의 정산은 비활성 조직에도 적립된다.

정지된 조직 회원(`users.organization_member_status = 2`, spec §9 결정 7):

- 조직 소유자/관리자가 5.4 `member_status: 2`로 정지한다. 플랫폼 `users.status`는 바뀌지 않는다(로그인은 계속된다).
- 정지된 회원의 relay 요청은 과금 전에 HTTP 403 `organization membership is suspended`(`access_denied`)로 거부된다(`service.NewBillingSession`의 조직 분기, 조직 플랜 회원 포함). 레거시 Midjourney도 `CheckMidjourneyOrganizationBilling`에서 같은 문구로 거부된다(Midjourney 응답 `code: 4`).
- 본인 조회(GET)는 계속된다: `/wallet` 요약, `GET /api/organization/subscription/self`, `GET /api/organization/topup/self`.
- `/api/organization/*`의 쓰기 요청(GET이 아닌 모든 요청)은 HTTP 403 `{"success": false, "message": "organization membership is suspended"}`로 거부된다(같은 미들웨어 `RejectDisabledOrganizationMutation`). 정지된 조직 관리자도 회원 재개·한도 변경·가져오기·plan 할당을 할 수 없다. owner는 정지할 수 없으므로 조직 충전(pay/amount)은 영향이 없다. 전체 관리자는 영향이 없다.
- 무료 모델(가격 0, `FreeModel`)은 과금 세션을 만들지 않으므로 정지로 막히지 않는다(비활성 조직과 같다).
- 정지는 realtime WebSocket 연결 시작(handshake) 때만 검사한다. 이미 열린 realtime 세션은 정지해도 끊기지 않는다.
- 조직을 떠나면(멤버십 해제, `remove_absent`, 조직 삭제 시 owner) `organization_member_status`는 `1`, `organization_remark`는 빈 문자열로 초기화된다.

조직 회원의 개인 잔액(`users.quota`는 조직 관리자가 정하는 회원 한도다):

- 조직 회원은 `POST /api/subscription/balance/pay`로 개인 잔액 구독을 살 수 없다(`organization members cannot pay with personal balance`).
- 조직 회원은 교환 코드(`POST /api/user/topup`)로 개인 잔액을 채울 수 없다(`organization members cannot redeem codes into their personal balance`).
- 조직 회원은 개인 유료 충전(`/api/user/pay`, `/api/user/amount`, Stripe·PayPal·Creem·Waffo·Waffo Pancake의 pay/amount)을 시작할 수 없다(`organization members cannot top up their personal balance`, 결제 API 형태). 조직 지갑 충전은 7장의 조직 API를 쓴다.

### 2.1.1 접근 토큰 스코프

upstream의 scoped access token으로 호출할 때 적용되는 규칙이다(`middleware/fork_access_token_routes.go`). 브라우저 세션 호출에는 영향이 없다.

| 라우트 | 규칙 |
| --- | --- |
| `/api/organization/*` 기본 (20개: profile, dashboard, logs, mj, task, users 계열, users/lookup, membership, subscription plans/users) | 세션 전용 — 접근 토큰으로는 호출할 수 없다 |
| `GET /api/organization/wallet`, `GET /api/organization/topup/self`, `POST .../amount` 5개 | `wallet:read` |
| `POST .../pay` 6개 | `wallet:write` |
| `GET /api/organization/subscription/self` | `profile:read` |
| `/api/organizations/*` 4개 | `handlePermissionRoute`가 자동 선언 — `organization:read`(GET), `organization:write`(POST/PATCH/DELETE) |

### 2.2 페이지네이션

목록 API는 `common.GetPageQuery`를 사용한다.

| Query | 타입 | 필수 | 기본값 | 범위/처리 |
| --- | --- | --- | --- | --- |
| `p` | integer | 선택 | `1` | 1 미만이면 `p`가 0이 아닌 경우 해당 값이 유지될 수 있으나, 일반적으로 1 이상을 사용한다. |
| `page_size` | integer | 선택 | `10` | 0이면 `ps`, `size` 순으로 fallback한다. 100 초과는 100으로 제한된다. |
| `ps` | integer | 선택 | - | `page_size`가 0일 때만 호환용으로 사용된다. |
| `size` | integer | 선택 | - | `page_size`, `ps`가 0일 때만 호환용으로 사용된다. |

목록 응답의 `data`는 보통 다음 형태다.

```json
{
  "page": 1,
  "page_size": 10,
  "total": 0,
  "items": []
}
```

### 2.3 주요 enum과 값

| 이름 | 타입 | 가능한 값 |
| --- | --- | --- |
| 조직 상태 `status` | integer | `1`: enabled, `2`: disabled |
| 조직 역할 `organization_role` | string | `member`, `admin`, `owner` |
| 로그 타입 `type` | integer | `0`: 전체, `1`: topup, `2`: consume, `3`: manage, `4`: system, `5`: error, `6`: refund |
| 구독 기간 단위 `duration_unit` | string | `year`, `month`, `day`, `hour`, `custom` |
| 조직 구독 사용자 상태 `subscription.status` | string | 주로 `active`, `cancelled` 사용. 만료된 active 구독은 조회/사용 시 자동 갱신될 수 있다. |

### 2.4 숫자 범위 표기 기준

이 문서의 숫자 범위는 두 가지로 나눠 적는다.

- 실제 검증: 컨트롤러 또는 모델 코드가 명시적으로 거부하는 범위
- 저장 타입: Go/DB 타입상 가능한 범위. 별도 검증이 없으면 비정상 값도 저장될 수 있으므로 클라이언트는 권장 범위를 따라야 한다.

조직·사용자 quota와 플랜 `total_amount`의 상한은 지갑 상한 `common.MaxWalletQuota` = 2^53-1 = `9,007,199,254,740,991`이다(team의 5e14·1,000,000,000 표기는 develop에서 바뀌었다). 사용자 ID·plan ID 같은 참조 ID는 `1,000,000,000` 상한을 유지한다. 충전 `amount` 상한은 upstream `getMaxTopUpAmount()`(지갑 상한 ÷ `QuotaPerUnit`, Stripe·PayPal은 10,000)이고, 적립 후 조직 지갑이 지갑 상한을 넘으면 거부된다(`validateTopUpTargetCapacity`).

## 3. 전체 관리자 조직 API

### 3.1 조직 목록 조회

`GET /api/organizations/`

전체 관리자 전용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장, 100 초과 시 100 | 페이지 크기 |

Body: 없음

응답 `data.items[]` 주요 필드:

| 이름 | 타입 | 설명 |
| --- | --- | --- |
| `id` | integer | 조직 ID |
| `name` | string | 조직 이름 |
| `description` | string | 설명 |
| `owner_user_id` | integer | 조직 소유자 사용자 ID |
| `quota` | integer | 조직 잔여 quota |
| `used_quota` | integer | 조직 누적 사용 quota |
| `status` | integer | `1` 또는 `2` |
| `created_at` | integer | Unix timestamp seconds |
| `updated_at` | integer | Unix timestamp seconds |

### 3.2 조직 생성

`POST /api/organizations/`

전체 관리자 전용이다. 조직을 만들고 `owner_user_id` 사용자를 해당 조직의 `owner`로 지정한다.

Body:

| 이름 | 타입 | 필수 | 범위/값 | 실제 검증 |
| --- | --- | --- | --- | --- |
| `name` | string | 필수 | 1-64자 | trim 후 빈 문자열이면 오류. 64자 초과이면 오류. DB 모델은 `varchar(64)`와 unique index를 사용한다. |
| `description` | string | 선택 | 0-255자 | trim 후 저장. 255자 초과이면 오류. DB 모델은 `varchar(255)`다. |
| `owner_user_id` | integer | 필수 | `1`-`1,000,000,000` | 범위 밖이면 오류. 사용자가 존재해야 하고, 이미 조직에 속해 있으면 오류. |
| `quota` | integer | 선택 | `0`-`9,007,199,254,740,991` | 생략 시 0. 범위 밖이면 오류. |

예시:

```json
{
  "name": "atto",
  "description": "atto organization",
  "owner_user_id": 12,
  "quota": 100000
}
```

주의:

- 전역 관리자 사용자를 owner로 지정하는 검증은 이 API 내부에는 별도로 없다. 실제 운영에서는 일반 사용자만 owner 후보로 노출해야 한다.
- 조직 이름은 DB unique index로 중복 생성이 실패한다.

### 3.3 조직 수정

`PATCH /api/organizations/:id`

전체 관리자 전용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 조직 ID. 숫자로 파싱되지 않으면 오류. |

Body:

| 이름 | 타입 | 필수 | 범위/값 | 실제 검증 |
| --- | --- | --- | --- | --- |
| `name` | string | 선택 | 1-64자 | 생성과 같은 검증: trim 후 빈 문자열이면 `name is required`, 64자 초과이면 오류. 다른 조직과 같으면 `organization name already exists` |
| `description` | string | 선택 | 0-255자 | trim 후 저장. 255자 초과이면 오류 |
| `quota` | integer | 선택 | `0`-`9,007,199,254,740,991` | 범위 밖이면 오류. 조직 지갑 잔액을 이 값으로 바꾼다(관리자 충전·조정 경로) |
| `status` | integer | 선택 | `1`, `2` | enum 외 값이면 오류 |

최소 하나의 필드는 포함해야 한다. 그렇지 않으면 `no organization fields to update` 오류가 발생한다. 성공하면 `organization.update` 감사 로그가 남는다.

예시:

```json
{
  "description": "updated",
  "quota": 500000,
  "status": 1
}
```

### 3.4 조직 삭제

`DELETE /api/organizations/:id`

전체 관리자 전용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 조직 ID |

처리 규칙:

- owner 외 구성원이 남아 있으면 거부된다 (`organization has N non-owner members, remove them first`). 먼저 멤버십 제거 API로 구성원을 내보낸다.
- 대기 중(`status = pending`)인 조직 충전 주문(`target_type = organization`)이 있으면 거부된다 (`organization has N pending top-up orders, wait for them to settle or expire first`). 현재 pending 주문을 만료시키는 것은 Stripe 웹훅뿐이므로, 다른 provider의 버려진 주문은 운영자가 `topups.status`를 직접 `expired`로 바꿔야 삭제가 가능하다(단계 C 항목). 결제 콜백이 삭제된 지갑에 정산하지 못하고 결제된 주문이 남는 것을 막는다.
- 삭제 시 owner의 `organization_id`/`organization_role`을 비우고 owner의 `users.quota`를 0으로 만든다(조직이 배정한 한도이므로). 이전 값은 `organization.delete` 감사 로그의 `previous_quota`에 남는다.
- 조직이 없으면 HTTP 404 `{"success": false, "message": "organization not found"}`.

응답: `{"success": true, "message": "", "data": null}`

## 4. 조직 기본 정보와 모니터링 API

### 4.1 내 조직 profile 조회

`GET /api/organization/`

조직 소유자 또는 조직 관리자 전용이다.

입력값: 없음

응답 `data`: `Organization`

주의:

- 전체 관리자는 이 API에서 조직을 자동 선택하지 않는다. 전체 관리자가 조직 데이터를 보려면 대시보드/로그 등 `organization_id` query를 받는 API를 사용한다.

### 4.2 조직 대시보드 조회

`GET /api/organization/dashboard`

조직 소유자/관리자는 자기 조직을 조회한다. 전체 관리자는 `organization_id` query가 필수다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |
| `preset` | string | 선택 | `today`, `7d`, `30d`, `custom` | `custom`이 아니고 빈 값도 아니면 지정된 값만 허용 |
| `start_timestamp` | integer | 선택 | Unix seconds, `>= 0` 권장 | 시작 시각. 파싱 실패 시 오류 |
| `end_timestamp` | integer | 선택 | Unix seconds, `>= 0` 권장 | 종료 시각. 파싱 실패 시 오류 |

처리 규칙:

- `preset`이 `today`, `7d`, `30d`이고 `start_timestamp`, `end_timestamp` 둘 중 하나라도 빠지면 서버가 기간을 계산한다.
- `start_timestamp > 0`, `end_timestamp > 0`이고 `start_timestamp > end_timestamp`이면 오류다.
- `preset=custom`은 서버가 별도 기간을 계산하지 않는다.

응답 `data`는 조직 사용량 요약, 일별 사용량, 사용자별/모델별 집계 등을 포함한다.

### 4.3 조직 사용 로그 조회

`GET /api/organization/logs`

조직 소유자/관리자는 자기 조직 사용자 로그만 조회한다. 전체 관리자는 `organization_id` query가 필수다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |
| `type` | integer | 선택 | `0`-`6` | 로그 타입. 파싱 실패 시 0으로 처리 |
| `start_timestamp` | integer | 선택 | Unix seconds | 파싱 실패 시 0으로 처리 |
| `end_timestamp` | integer | 선택 | Unix seconds | 파싱 실패 시 0으로 처리 |
| `username` | string | 선택 | 임의 문자열 | 사용자명 필터 (관리자 필터 — `logFilterFromQuery(c, true)`) |
| `token_name` | string | 선택 | 임의 문자열 | 토큰명 필터 |
| `model_name` | string | 선택 | 임의 문자열 | 모델명 필터 |
| `channel` | integer | 선택 | 채널 ID | 파싱 실패 시 0으로 처리 (관리자 필터) |
| `group` | string | 선택 | 사용자 그룹 | 그룹 필터 |
| `request_id` | string | 선택 | 요청 ID | 요청 ID 필터 |
| `upstream_request_id` | string | 선택 | upstream 요청 ID | upstream 요청 ID 필터 |

응답 `data`: `PageInfo<Log[]>`

주의:

- 조직 밖 사용자 로그는 포함되지 않는다.
- 기간 제한(4.3·4.4·4.5 공통): 전역 관리자가 아닌 호출자(조직 소유자/관리자)에게는 upstream 사용자 로그와 같은 `limitLogTimeRange`가 적용된다. 범위가 로그 설정(`log_setting.max_range_days`, 기본 365일)을 넘으면 `The time range cannot exceed N days` 오류, `start_timestamp`가 없으면 최대 범위만큼만 거슬러 올라간다. 전체 관리자는 제한이 없다.
- text 필터는 모델의 `applyExplicitLogTextFilter` 규칙을 따른다.

### 4.4 조직 Midjourney 작업 로그 조회

`GET /api/organization/mj`

조직 소유자/관리자 또는 전체 관리자 조직 선택 조회용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |
| `channel_id` | string | 선택 | 숫자 문자열 권장 | 작업 channel ID 필터 |
| `mj_id` | string | 선택 | 임의 문자열 | Midjourney 작업 ID |
| `start_timestamp` | string | 선택 | Unix milliseconds 문자열 (`submit_time` 단위) | 시작 시각. 4.3의 기간 제한을 초 단위로 바꿔 적용 |
| `end_timestamp` | string | 선택 | Unix milliseconds 문자열 | 종료 시각 |

응답 `data`: `PageInfo<Task[]>`

### 4.5 조직 작업 로그 조회

`GET /api/organization/task`

조직 소유자/관리자 또는 전체 관리자 조직 선택 조회용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |
| `platform` | string | 선택 | task platform 문자열 | 플랫폼 필터 |
| `task_id` | string | 선택 | 임의 문자열 | 작업 ID |
| `status` | string | 선택 | task status 문자열 | 상태 필터 |
| `action` | string | 선택 | task action 문자열 | 액션 필터 |
| `channel_id` | string | 선택 | 숫자 문자열 권장 | 채널 ID |
| `start_timestamp` | integer | 선택 | Unix seconds | 파싱 실패 시 0 |
| `end_timestamp` | integer | 선택 | Unix seconds | 파싱 실패 시 0 |

응답 `data`: `PageInfo<TaskDto[]>`

## 5. 조직 사용자 API

### 5.1 조직 사용자 목록 조회

`GET /api/organization/users`

조직 소유자/관리자 또는 전체 관리자 조직 선택 조회용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |
| `keyword` | string | 선택 | 임의 문자열 | `username` 또는 `display_name` 부분 일치 |
| `order_by` | string | 선택 | `username`, `display_name`, `organization_role`, `quota`, `used_quota`, `group`, `status`, `organization_member_status` | 그 밖의 값은 `id` |
| `order_dir` | string | 선택 | `asc`, `desc` | `desc`가 아니면 `asc` |

응답 `data`: `PageInfo<OrganizationUser[]>`

`OrganizationUser`는 제한된 사용자 뷰다: `id`, `username`, `display_name`, `role`, `status`, `member_status`, `quota`, `used_quota`, `group`, `organization_id`, `organization_role`, `organization_remark`. `status`는 플랫폼 상태(읽기 전용, 전체 관리자만 바꾼다), `member_status`는 조직 범위 상태(`1` active, `2` suspended, 컬럼 `users.organization_member_status`), `organization_remark`는 조직 범위 메모(컬럼 `users.organization_remark`)다. 플랫폼 메모 `users.remark`, 비밀번호 해시, access token, 이메일, 결제 고객 ID, setting은 포함하지 않는다.

처리 규칙:

- 조직 소속 사용자 중 `role < RoleAdminUser`인 사용자만 조회한다.
- 전역 관리자 계정은 조직 사용자 목록에서 제외된다.
- 정렬 컬럼은 dialect별로 quoting된다(`group` 예약어 대응).

### 5.2 추가할 사용자 정확 조회

`GET /api/organization/users/lookup`

조직 소유자/관리자용이다 (`organization admin permission required`). 조직 미소속 사용자 목록은 없다(spec §9 결정 6): 멤버를 추가하려면 username 또는 email을 정확히 알아야 한다.

Query (둘 중 정확히 하나):

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `username` | string | 택1 | 앞뒤 공백 제거 | username 정확 일치 |
| `email` | string | 택1 | 앞뒤 공백 제거, 소문자화 | `LOWER(email)` 정확 일치 |

둘 다 없거나 둘 다 주면 `provide either a username or an email`.

응답 `data` (`OrganizationMemberLookup`): `id`, `username`, `display_name`, `organization_id`. `quota`, `used_quota`, `remark`, 이메일, 역할·상태는 포함하지 않는다.

```json
{
  "success": true,
  "message": "",
  "data": { "id": 42, "username": "alice", "display_name": "Alice", "organization_id": 0 }
}
```

처리 규칙:

- 대상은 `role < RoleAdminUser`이고 `organization_id = 0`(미소속)이거나 actor와 같은 조직인 사용자다. 부분 일치·접두 일치는 하지 않는다.
- 일치하는 사용자가 정확히 한 명이 아니면(없음, 다른 조직 소속, 전역 관리자) HTTP 404 `{"success": false, "message": "user not found"}`.
- 조회 결과는 할당 가능 여부를 보장하지 않는다. 개인 잔액·구독 거부는 5.5의 할당 시점에 판정한다.
- 같은 조직 구성원도 찾을 수 있다(`organization_id`가 actor 조직과 같음). 웹 화면은 이 경우 `Already a member`로 표시하고 추가 버튼을 막는다.
- 웹 화면은 입력값을 먼저 `username`으로 조회하고, 404이고 입력값에 `@`가 있으면 `email`로 다시 조회한다(`@`가 들어간 username도 찾을 수 있다).

### 5.3 조직 사용자 상세 조회

`GET /api/organization/users/:id`

조직 소유자/관리자만 자기 조직의 일반 사용자를 조회할 수 있다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 사용자 ID |

응답 `data`: `OrganizationUser` (5.1과 같은 제한 뷰)

처리 규칙:

- 대상 사용자는 actor와 같은 조직이어야 한다.
- 대상 사용자의 전역 `role`이 관리자 이상이면 거부된다.

### 5.4 조직 사용자 수정

`PATCH /api/organization/users/:id`

조직 소유자/관리자만 자기 조직의 일반 사용자를 수정할 수 있다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 사용자 ID |

Body:

| 이름 | 타입 | 필수 | 범위/값 | 실제 검증 |
| --- | --- | --- | --- | --- |
| `member_status` | integer | 선택 | `1`(active), `2`(suspended) | enum 외 값이면 `member_status must be one of: 1, 2`. `users.organization_member_status`에 쓴다 |
| `quota` | integer | 선택 | `0`-`9,007,199,254,740,991` | 범위 밖이면 오류. active 조직 구독 사용자는 프론트에서 입력을 막는다. |
| `organization_remark` | string | 선택 | 0-255자 | trim 후 `users.organization_remark`에 저장. 255자 초과이면 `organization_remark must be at most 255 characters` |

쓰기는 호출자 조직으로 범위를 건다(`WHERE id = ? AND organization_id = ?`). 권한 확인 뒤 대상이 조직을 떠났으면 아무것도 쓰지 않고 `user is no longer a member of this organization`을 돌려준다. Excel 가져오기의 기존 구성원 갱신도 같은 규칙으로 행 오류(`line N (<username>): failed to update: user is no longer a member of this organization`)가 된다.

플랫폼 필드 `status`·`remark`는 조직 관리자가 쓸 수 없다(spec §9 결정 7). 본문에 있으면 `unsupported field: status` / `unsupported field: remark`로 거부된다(엄격 디코딩).

최소 하나의 필드는 포함해야 한다. 그렇지 않으면 `no organization user fields to update` 오류가 발생한다.

예시:

```json
{
  "quota": 100000,
  "member_status": 2,
  "organization_remark": "team member"
}
```

주의:

- 이 API는 `quota`를 `0`-`9,007,199,254,740,991` 범위로 강제한다.
- active 조직 구독이 있는 사용자는 quota 방식이 아니라 plan 방식으로 관리하므로 클라이언트에서 quota 입력을 비활성화해야 한다.
- 자기 자신은 수정할 수 없고, owner 행은 owner 본인만 수정할 수 있다(2.1 공통 대상 규칙).
- 성공하면 `organization.user.update` 감사 로그가 남는다(바뀐 필드 포함).

### 5.5 사용자 조직 멤버십 할당/역할 변경

`PUT /api/organization/users/:id/membership`

조직 admin 이상 권한이 필요하다 (admin, owner).

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 할당할 사용자 ID |

Body:

| 이름 | 타입 | 필수 | 가능한 값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_role` | string | 필수 | `member`, `admin` | 대상 사용자에게 부여할 조직 역할. `owner`는 거부된다 |

예시:

```json
{
  "organization_role": "member"
}
```

처리 규칙:

- 조직 admin 이상이면 호출 가능하다. 조직 관리자도 새 멤버를 `member` 또는 `admin`으로 추가할 수 있고, 기존 구성원의 역할을 member↔admin으로 바꿀 수 있다. (웹 화면은 5.2 조회로 한 명을 찾은 뒤 추가한다. 추가할 역할 선택과 표의 Role 드롭다운은 owner에게만 보이고, 관리자는 `member`로 추가한다.)
- `owner` 역할은 누구도 부여할 수 없다 (`ownership is assigned when the organization is created`).
- 자기 자신은 대상이 될 수 없다 (`organization admins cannot reassign themselves`). owner 행은 owner 본인만 대상으로 할 수 있다.
- 전역 관리자 사용자는 조직에서 관리할 수 없다 (`global admin users cannot be managed by organization admins`).
- 역할 값이 `member`, `admin`, `owner`가 아니면 `invalid organization role`.
- 대상 사용자가 이미 다른 조직에 속해 있으면 모델이 거부한다 (`user belongs to another organization`). 조직이 비활성이면 `organization is not active`.
- **개인 잔액 거부(spec §9 결정 6):** 조직 밖에서 들어오는 사용자가 개인 잔액(`users.quota > 0`), 추천(affiliate) 잔액(`users.aff_quota > 0` — 아직 잔액으로 옮기지 않은 추천 수익도 개인 돈이다)이나 활성 개인 구독(`user_subscriptions.status = 'active' AND end_time > now`)을 가지고 있으면 거부한다 (`user still has a personal balance, affiliate balance or subscription; it must be cleared before joining an organization`, `message_key` 같음). 조직에 들어오면 `users.quota`는 조직 관리자가 정하는 한도가 되므로, 사용자가 잔액을 다 쓰거나 운영자가 정리한 뒤에만 들어올 수 있다. 판정은 `model.AssignUserToOrganization`의 사용자 행 잠금 트랜잭션 안에서 한다. 초대·수락 흐름은 없다. 만료·취소된 구독은 막지 않는다. 이미 같은 조직 구성원의 역할 변경에는 적용하지 않는다.
- 이미 조직 구성원인 경우 이 API로 역할을 변경할 수 있다.
- 성공하면 `organization.user.assign` 감사 로그가 남는다.

### 5.6 사용자 조직 멤버십 제거

`DELETE /api/organization/users/:id/membership`

조직 admin 이상 권한이 필요하다 (admin, owner).

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 제거할 사용자 ID |

처리 규칙:

- 조직 admin 이상이면 호출 가능하다.
- 자기 자신은 제거할 수 없다 (`organization admins cannot modify their own membership`).
- 대상 사용자가 같은 조직 소속이 아니면 `user does not belong to your organization`.
- 대상 사용자가 owner이면 `organization owner cannot be removed`.
- 성공 시 해당 사용자의 `organization_id = 0`, `organization_role = ""`가 되고 `users.quota`는 0이 된다(조직이 배정한 한도이므로 개인 잔액으로 남기지 않는다). 이전 값은 `organization.user.remove` 감사 로그의 `previous_quota`에 남는다.
- 같은 트랜잭션에서 그 사용자의 활성 조직 구독도 취소된다(다시 추가돼도 이전 플랜이 되살아나지 않는다).
- 제거 뒤 도착한 환급(비동기 작업·Midjourney·요청 실패)은 조직 지갑에만 돌아가고 회원 한도에는 더하지 않는다.

응답:

```json
{
  "success": true,
  "message": ""
}
```

### 5.7 조직 사용자 Excel 내보내기

`GET /api/organization/users/export`

조직 소유자/관리자 또는 전체 관리자(`organization_id` query 필수)용이다.

응답: `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` 첨부 파일 `org-users-<조직ID>-<unix>.xlsx` (시트 `OrgUsers`). 실패하면 JSON 오류 응답이다.

열: `username`, `display_name`, `organization_role`, `quota`, `used_quota`, `group`, `member_status`, `organization_remark`, `initial_password`. `member_status`는 `active`/`suspended`다. 조직 소속이면서 `role < admin`인 사용자만 포함한다. 플랫폼 `status`·`remark`는 내보내지 않는다.

### 5.8 조직 사용자 Excel 가져오기

`POST /api/organization/users/import`

조직 소유자/관리자 또는 전체 관리자(`organization_id` query 필수)용이다. `multipart/form-data`.

| 이름 | 타입 | 필수 | 설명 |
| --- | --- | --- | --- |
| `file` | file | 필수 | 5.7 형식의 `.xlsx`, 최대 10 MiB |
| `remove_absent` | string | 선택 | `"true"`이면 파일에 없는 기존 구성원을 조직에서 제거한다(owner와 플랫폼 관리자는 제외) |

처리 규칙:

- 없는 username은 `initial_password`로 새 사용자를 만들고, 조직 밖 사용자는 조직에 넣고, 기존 구성원은 표시 이름·역할·quota·`member_status`·`organization_remark`를 갱신한다. quota는 시트 값을 절대값으로 한 번에 쓴다. `member_status`는 `active`/`suspended`(대소문자 무시)만 받고 그 밖의 값은 행 오류(`invalid member_status "…" (use active or suspended)`), 빈 칸은 기존 값 유지다. `organization_remark`는 시트에 그 열이 있을 때만 쓴다(열이 없으면 기존 값 유지, 열이 있고 칸이 비면 지움, 255자 초과는 행 오류).
- 옛 열 `status`·`remark`가 있는 시트는 전체가 거부된다(`unsupported column: status — use member_status`, `unsupported column: remark — use organization_remark`). 가져오기는 플랫폼 `users.status`·`users.remark`를 쓰지 않으며, 새 사용자의 플랫폼 상태는 항상 enabled다.
- `group` 열은 읽지 않는다(내보내기 전용). 새 사용자는 기본 그룹을 받는다.
- `owner` 행은 오류로 처리한다. 전역 관리자 계정은 만들거나 바꾸지 않는다.
- 가져오는 사용자 자신의 행은 오류로 처리한다 (`line N (<username>): an import cannot change the importing admin's own row`). `remove_absent`는 가져오는 사용자를 제거하지 않는다.
- 조직 밖 기존 사용자 행에는 5.5의 개인 잔액 거부가 똑같이 적용된다. 그 행은 `line N (<username>): failed to assign: user still has a personal balance, affiliate balance or subscription; ...` 행 오류가 되고 사용자는 조직에 들어가지 않는다. 가져오기가 새로 만든 사용자는 거부하지 않는다(가입 보너스 `QuotaForNewUser`는 다른 사람의 잔액이 아니다 — `model.AssignCreatedUserToOrganization`).

응답: `{"success": true, "data": {"assigned": n, "created": n, "updated": n, "removed": n, ... , "errors": [...]}}` (`OrgImportResult`). 요청마다 `organization.user.import` 감사 로그가 남는다.

## 6. 조직 구독 API

### 6.1 내 활성 조직 구독 조회

`GET /api/organization/subscription/self`

로그인 사용자용이다.

입력값: 없음

응답:

| 상태 | 응답 |
| --- | --- |
| 조직 미소속 | `data: null` |
| 활성 조직 구독 없음 | `data: null` |
| 활성 조직 구독 있음 | `OrganizationUserSubscriptionSummary` |

`OrganizationUserSubscriptionSummary`:

| 이름 | 타입 | 설명 |
| --- | --- | --- |
| `subscription` | object | 사용자 조직 구독 |
| `user` | object | 5.1의 `OrganizationUser` 제한 뷰(`id`, `username`, `display_name`, `role`, `status`, `member_status`, `quota`, `used_quota`, `group`, `organization_id`, `organization_role`, `organization_remark`). `subscription/self`에서는 호출자 본인이며 `organization_remark`는 빈 문자열이다(관리자 메모는 회원에게 보이지 않는다). 비밀번호·접근 토큰·email 등은 없다 |
| `plan` | object | 조직 구독 plan |

### 6.2 조직 구독 plan 목록 조회

`GET /api/organization/subscription/plans`

조직 소유자/관리자 또는 전체 관리자 조직 선택 조회용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |

응답 `data`: `OrganizationSubscriptionPlan[]`

정렬: `sort_order asc, id asc`

### 6.3 조직 구독 plan 생성

`POST /api/organization/subscription/plans`

조직 소유자/관리자 또는 전체 관리자 조직 선택 생성용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 생성 대상 조직 ID |

Body:

| 이름 | 타입 | 필수 | 범위/값 | 실제 검증/처리 |
| --- | --- | --- | --- | --- |
| `title` | string | 필수 | 1-128자 | trim 후 빈 문자열이면 오류. 128자 초과이면 오류 |
| `subtitle` | string | 선택 | 0-255자 | trim 후 저장. 255자 초과이면 오류 |
| `duration_unit` | string | 선택 | `year`, `month`, `day`, `hour`, `custom` | 빈 값이면 `month`. enum 외 값이면 오류 |
| `duration_value` | integer | 선택 | `0`-`1200` | `duration_unit != custom`이고 0이면 1로 보정. 음수 또는 1200 초과이면 오류 |
| `custom_seconds` | integer | `duration_unit=custom`이면 필수 성격 | custom: `1`-`31,536,000`, non-custom: `0`-`31,536,000` | custom 기간이면 0 이하 오류. 31,536,000초 초과이면 오류 |
| `sort_order` | integer | 선택 | `-1,000,000`-`1,000,000` | 정렬용. 범위 밖이면 오류 |
| `total_amount` | integer | 선택 | `0`-`9,007,199,254,740,991` | 범위 밖이면 오류. 생략 시 0 |

위 표에 없는 body 필드는 허용하지 않는다. 예를 들어 `quota_reset_period`, `quota_reset_custom_seconds`, `upgrade_group`, `enabled`를 보내면 `unsupported field: <field>` 오류가 발생한다. 생성 시 `enabled`는 내부 기본값 `true`로 저장되고, quota reset과 upgrade group은 현재 정책상 각각 `never`, `0`, 빈 문자열로 고정된다.

예시:

```json
{
  "title": "Team Basic",
  "subtitle": "기본 팀 플랜",
  "duration_unit": "month",
  "duration_value": 1,
  "total_amount": 500000,
  "sort_order": 10
}
```

기간 계산:

| `duration_unit` | 계산 방식 |
| --- | --- |
| `year` | 시작 시각에서 `duration_value`년 추가 |
| `month` | 시작 시각에서 `duration_value`개월 추가 |
| `day` | 시작 시각에서 `duration_value * 24h` 추가 |
| `hour` | 시작 시각에서 `duration_value * 1h` 추가 |
| `custom` | 시작 시각에서 `custom_seconds`초 추가 |

### 6.4 조직 구독 plan 수정

`PATCH /api/organization/subscription/plans/:id`

조직 소유자/관리자 또는 전체 관리자 조직 선택 수정용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | plan ID |

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 수정 대상 조직 ID |

Body:

생성과 동일한 body를 사용한다. 현재 구현상 수정도 `title`이 필수다. 즉 부분 수정처럼 `enabled`만 보내는 요청은 `title is required` 오류가 발생한다.

추가 처리:

- `id`와 `organization_id`가 모두 일치하는 plan만 수정한다. 일치하는 plan이 없으면(다른 조직의 plan 포함) HTTP 404 `{"success": false, "message": "subscription plan not found"}`이고 감사 로그를 남기지 않는다.
- `quota_reset_period`, `quota_reset_custom_seconds`, `upgrade_group`, `enabled`는 요청 body로 받을 수 없다.

### 6.5 조직 구독 plan 비활성화

`DELETE /api/organization/subscription/plans/:id`

조직 소유자/관리자 또는 전체 관리자 조직 선택 처리용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | plan ID |

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 대상 조직 ID |

Body: 없음

처리:

- row를 삭제하지 않고 `enabled=false`로 업데이트한다.
- 대상 조직의 plan이 아니면 6.4와 같은 HTTP 404이고 감사 로그를 남기지 않는다.
- 이미 사용자에게 할당된 구독은 별도 취소 전까지 남을 수 있다.

### 6.6 조직 사용자 구독 할당 목록 조회

`GET /api/organization/subscription/users`

조직 소유자/관리자 또는 전체 관리자 조직 선택 조회용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 조회할 조직 ID |

응답 `data`: `OrganizationUserSubscriptionSummary[]`

정렬: `status asc, end_time desc, id desc`

주의:

- 이 API는 active뿐 아니라 cancelled 등 모든 status의 구독 record를 반환할 수 있다. UI는 active 목록 표시 시 cancelled/expired를 필터링한다.
- 각 항목의 `user`는 5.1의 `OrganizationUser` 제한 뷰다(6.1과 같음). 2026-10-14 수동 실행에서 찾은 비밀번호 해시 노출(`users` 행 전체)은 단계 B에서 고쳤다.

### 6.7 조직 사용자에게 구독 plan 할당

`PUT /api/organization/subscription/users/:id`

조직 소유자/관리자 또는 전체 관리자 조직 선택 할당용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 구독을 할당할 사용자 ID |

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 대상 조직 ID |

Body:

| 이름 | 타입 | 필수 | 범위/값 | 실제 검증 |
| --- | --- | --- | --- | --- |
| `plan_id` | integer | 필수 | `1`-`1,000,000,000` | 범위 밖이면 오류. 대상 조직의 plan이어야 한다. |

예시:

```json
{
  "plan_id": 3
}
```

처리 규칙:

- 대상 사용자는 같은 조직 소속이어야 한다.
- 대상 사용자의 전역 role이 관리자 이상이면 거부된다.
- owner를 대상으로 하는 할당은 owner 본인 또는 전체 관리자만 할 수 있다.
- 조직 소유자/관리자는 자기 자신에게 플랜을 할당할 수 없다 (`organization admins cannot manage their own subscription`). 자기 플랜은 전체 관리자가 `organization_id` query로 할당한다.
- 같은 사용자에게 기존 active 구독이 있으면 취소되고 새 active 구독이 생성된다.
- 새 구독의 `amount_total`은 plan의 `total_amount`다.
- 성공하면 `organization.subscription.assign` 감사 로그가 남는다. plan 생성·수정·비활성화와 구독 취소도 각각 `organization.plan.create`/`organization.plan.update`/`organization.plan.disable`/`organization.subscription.cancel`을 남긴다.

### 6.8 조직 사용자 구독 취소

`DELETE /api/organization/subscription/users/:id`

조직 소유자/관리자 또는 전체 관리자 조직 선택 처리용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 구독을 취소할 사용자 ID |

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_id` | integer | 전체 관리자만 필수 | `> 0` | 대상 조직 ID |

Body: 없음

처리:

- 해당 조직/사용자의 active 조직 구독을 취소한다. owner의 구독은 owner 본인 또는 전체 관리자만 취소할 수 있다.
- 조직 소유자/관리자는 자기 자신의 구독을 취소할 수 없다 (`organization admins cannot manage their own subscription`).
- 만료 시간이 지났더라도 `status=active`인 구독은 취소 대상이다.
- active 구독이 없으면 모델 레벨에서 오류가 날 수 있다.

## 7. 조직 지갑과 충전 API

조직 지갑 API는 조직 소유자 전용이다(전체 관리자·조직 관리자·member는 `organization owner permission required`). 호출 시 서버가 topup target을 `organization`으로 설정하므로, 결제 완료 후 quota는 개인 사용자가 아니라 조직에 적립된다. 조직형 주문에 `target_id`가 없으면 정산을 거부한다(누구에게도 적립하지 않는다).

결제 provider는 Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake 6종이다(Toss와 지갑 자동충전은 develop에 없다). 결제 설정이 끝나지 않은 provider는 오류를 돌려준다. 예: Epay `Payment information has not been configured by administrator`, PayPal `Unsupported payment channel`. pay/amount 계열의 거부 응답은 결제 API 형태 `{"message": "error", "data": "<사유>"}`다.

관리자가 조직 지갑을 직접 채우거나 조정할 때는 결제 API가 아니라 `PATCH /api/organizations/:id`의 `quota`를 쓴다(3.3).

비활성 조직의 pay/amount는 거부된다(2.1의 비활성 조직 규칙; 핸들러 단계에서도 `ValidateOrganizationWalletCapacity`가 `organization is disabled`를 결제 API 형태로 돌려준다). 조직 회원의 개인 충전 거부는 2.1 참고.

### 7.1 조직 지갑 조회

`GET /api/organization/wallet`

입력값: 없음

응답 `data`: `Organization`

### 7.2 조직 충전 내역 조회

`GET /api/organization/topup/self`

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |
| `keyword` | string | 선택 | 임의 문자열 | 결제 방법, trade no 등 검색 |

응답 `data`: `PageInfo<TopUp[]>`

### 7.3 충전 금액 계산

조직 소유자 전용이며, 개인 충전 금액 계산 API를 조직 지갑 대상으로 실행한다.

| Method | Path | Body | 설명 |
| --- | --- | --- | --- |
| POST | `/api/organization/amount` | `AmountRequest` | Epay 계열 금액 계산 |
| POST | `/api/organization/stripe/amount` | `StripePayRequest` | Stripe 금액 계산 |
| POST | `/api/organization/paypal/amount` | `PayPalPayRequest` | PayPal 금액 계산 |
| POST | `/api/organization/waffo/amount` | `WaffoPayRequest` | Waffo 금액 계산 (웹 조직 지갑의 Waffo 금액 표시가 이 API를 쓴다) |
| POST | `/api/organization/waffo-pancake/amount` | `WaffoPancakePayRequest` | Waffo Pancake 금액 계산 |

#### `AmountRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `min_topup` 이상, `getMaxTopUpAmount()` 이하 | 충전 단위. quota 표시 방식이 tokens이면 token 수로 해석될 수 있다. |

#### `StripePayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `stripe_min_topup`-`10,000` | 충전 단위 |
| `payment_method` | string | 결제 생성 시 필수 | `stripe` | `/stripe/pay`에서 검사 |
| `success_url` | string | 선택 | 신뢰 가능한 redirect URL | `/stripe/pay`에서만 검증 |
| `cancel_url` | string | 선택 | 신뢰 가능한 redirect URL | `/stripe/pay`에서만 검증 |

#### `PayPalPayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `paypal_min_topup`-`10,000` | 충전 단위 |
| `payment_method` | string | 결제 생성 시 필수 | `paypal` | `/paypal/pay`에서 검사 |
| `success_url` | string | 선택 | 신뢰 가능한 redirect URL | 현재 pay flow에서는 검사하지만 성공 URL로 직접 사용하지 않는다 |
| `cancel_url` | string | 선택 | 신뢰 가능한 redirect URL | PayPal cancel URL로 사용 |

#### `WaffoPayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `waffo_min_topup` 이상, `getMaxTopUpAmount()` 이하 | 충전 단위 |
| `pay_method_index` | integer 또는 null | 선택 | 서버 결제수단 목록 index | nil이면 Waffo 자동 선택 |
| `pay_method_type` | string | 선택 | deprecated | 예전 프론트 호환용 |
| `pay_method_name` | string | 선택 | deprecated | 예전 프론트 호환용 |

#### `WaffoPancakePayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `waffo_pancake_min_topup` 이상, `getMaxTopUpAmount()` 이하 | 충전 단위 |

### 7.4 충전 결제 생성

조직 소유자 전용이며, 생성되는 `TopUp` record의 `target_type`은 `organization`, `target_id`는 조직 ID다.

| Method | Path | Body | 주요 성공 응답 |
| --- | --- | --- | --- |
| POST | `/api/organization/pay` | `EpayRequest` | 결제 URL/params |
| POST | `/api/organization/stripe/pay` | `StripePayRequest` | `{ "pay_link": "..." }` |
| POST | `/api/organization/paypal/pay` | `PayPalPayRequest` | `{ "pay_link": "..." }` |
| POST | `/api/organization/creem/pay` | `CreemPayRequest` | `{ "checkout_url": "...", "order_id": "..." }` |
| POST | `/api/organization/waffo/pay` | `WaffoPayRequest` | provider별 checkout data |
| POST | `/api/organization/waffo-pancake/pay` | `WaffoPancakePayRequest` | `{ "checkout_url": "...", "session_id": "...", "order_id": "...", "token": "..." }` |

#### `EpayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `min_topup` 이상, `getMaxTopUpAmount()` 이하 | 충전 단위 |
| `payment_method` | string | 필수 | 관리자가 설정한 pay method 중 하나 | `operation_setting.ContainsPayMethod`로 검사 |

#### `CreemPayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `product_id` | string | 필수 | `setting.CreemProducts`에 존재하는 product ID | 빈 값이면 오류, 설정에 없으면 오류 |
| `payment_method` | string | 필수 | `creem` | Creem 결제 채널 검사 |

## 8. 권한 오류와 대표 오류

| 상황 | 대표 오류 메시지 |
| --- | --- |
| 전체 관리자 전용 API를 일반 사용자가 호출 | `root permission required` |
| 전체 관리자가 `organization_id` 없이 조직 선택 API 호출 | `organization_id is required` |
| 조직 소유자/관리자 권한이 필요한 API를 일반 조직 사용자가 호출 | `organization admin permission required` |
| 조직 소유자 전용 API를 관리자/member가 호출 | `organization owner permission required` |
| 다른 조직 사용자 또는 전역 관리자 사용자를 관리하려 함 | `organization target permission denied` |
| body에 허용되지 않은 필드가 들어옴 | `unsupported field: <field>` |
| 조직 이름 누락 | `name is required` |
| 조직 이름 64자 초과 | `name must be at most 64 characters` |
| 조직 설명 255자 초과 | `description must be at most 255 characters` |
| owner 사용자 ID가 0 이하 | `owner_user_id must be greater than 0` |
| owner 사용자 ID가 1,000,000,000 초과 | `owner_user_id must be at most 1000000000` |
| 조직 구독 plan title 누락 | `title is required` |
| 조직 구독 plan title 128자 초과 | `title must be at most 128 characters` |
| 조직 구독 plan subtitle 255자 초과 | `subtitle must be at most 255 characters` |
| 조직 quota 또는 사용자 quota 범위 초과 | `quota must be between 0 and 9007199254740991` |
| 조직 구독 plan 총량 범위 초과 | `total_amount must be between 0 and 9007199254740991` |
| 자기 자신의 멤버십·quota·상태 변경 | `organization admins cannot modify their own membership` |
| 자기 자신에게 멤버십 PUT | `organization admins cannot reassign themselves` |
| owner가 아닌 사용자가 owner 행 변경 | `only the organization owner can manage the owner` |
| `owner` 역할 부여 시도 | `ownership is assigned when the organization is created` |
| 다른 조직 사용자를 멤버로 추가 | `user belongs to another organization` |
| 개인 잔액(`quota > 0`), 추천 잔액(`aff_quota > 0`)이나 활성 개인 구독이 있는 사용자를 멤버로 추가(API·가져오기) | `user still has a personal balance, affiliate balance or subscription; it must be cleared before joining an organization` |
| 멤버 조회에 username/email이 없거나 둘 다 있음 | `provide either a username or an email` |
| 멤버 조회에 정확히 일치하는 사용자 없음 | HTTP 404 `user not found` |
| owner 제거 시도 | `organization owner cannot be removed` |
| 구성원이 남은 조직 삭제 | `organization has N non-owner members, remove them first` |
| 대기 중인 조직 충전 주문이 있는 조직 삭제 | `organization has N pending top-up orders, wait for them to settle or expire first` |
| 다른 조직과 같은 이름으로 변경 | `organization name already exists` |
| 비활성 조직의 쓰기 API·relay·충전 | `organization is disabled` (relay는 HTTP 403) |
| 정지된 조직 회원의 relay·레거시 Midjourney·`/api/organization/*` 쓰기 | `organization membership is suspended` (relay와 쓰기 요청은 HTTP 403) |
| 수정 중 대상이 조직을 떠남 | `user is no longer a member of this organization` |
| 조직 사용자 수정에 플랫폼 `status`/`remark` | `unsupported field: status` / `unsupported field: remark` |
| 잘못된 member_status | `member_status must be one of: 1, 2` |
| 가져오기 시트에 옛 `status`/`remark` 열 | `unsupported column: status — use member_status` / `unsupported column: remark — use organization_remark` (전체 거부) |
| 자기 자신에게 플랜 할당·취소 | `organization admins cannot manage their own subscription` |
| 가져오기 시트에 가져오는 사용자 자신의 행 | `line N (<username>): an import cannot change the importing admin's own row` (행 오류) |
| 대상 조직에 없는 plan id 수정·비활성화 | HTTP 404 `subscription plan not found` |
| 조직 로그 기간이 로그 설정 상한 초과 | `The time range cannot exceed N days` |
| 조직 회원의 개인 잔액 구독 결제 | `organization members cannot pay with personal balance` |
| 조직 회원의 교환 코드 사용 | `organization members cannot redeem codes into their personal balance` |
| 조직 회원의 개인 유료 충전 | `organization members cannot top up their personal balance` |
| 조직 구독 plan 기간 값 범위 초과 | `duration_value must be between 0 and 1200` |
| custom 기간인데 custom_seconds 범위가 잘못됨 | `custom_seconds must be between 1 and 31536000` |
| non-custom 기간인데 custom_seconds 범위가 잘못됨 | `custom_seconds must be between 0 and 31536000` |
| 잘못된 duration_unit | `duration_unit must be one of: year, month, day, hour, custom` |
| 잘못된 status | `status must be one of: 1, 2` |
| 잘못된 plan_id 하한 | `plan_id must be greater than 0` |
| 잘못된 plan_id 상한 | `plan_id must be at most 1000000000` |
| 잘못된 대시보드 preset | `invalid preset` |

## 9. 구현상 주의점

- 조직 quota는 조직 전체 예산이다. 조직 구독 사용자도 최종적으로 조직 quota가 부족하면 요청이 실패한다.
- 조직 구독 사용자는 개인 quota fallback을 하지 않는다.
- 조직 지갑이 선차감을 감당하지 못하면 relay 오류 문구는 개인 잔액이 아니라 조직 지갑을 가리킨다(`organization quota insufficient, remaining: …`, HTTP 403).
- 환급은 과금 때 고정한 조직으로 간다(비동기 작업 `TaskPrivateData.OrganizationId`, 레거시 Midjourney `midjourneys.organization_id`). 회원 한도는 그 조직에 아직 소속일 때만 돌려준다. 조직 `used_quota`도 고정한 조직에 쌓인다.
- `/api/organization/subscription/plans/:id` 수정 API는 현재 부분 수정이어도 `title`을 요구한다.
- 조직 쓰기 API는 strict JSON decoding을 사용한다. 문서에 없는 body 필드는 무시하지 않고 `unsupported field: <field>`로 거부한다.
- 일부 query 숫자 필드는 파싱 실패 시 오류가 아니라 0으로 처리된다. 문서의 "실제 검증" 항목을 우선한다.
- 조직·조직 사용자·plan의 quota 계열 값은 `0`-`9,007,199,254,740,991`(2^53-1) 범위를 강제한다.
- 조직 변경 API 12종은 `organization.*` 감사 로그를 남긴다(create, update, delete, user.assign, user.update, user.remove, user.import, plan.create, plan.update, plan.disable, subscription.assign, subscription.cancel).
- 조직 필드(`organization_id`, `organization_role`)는 사용자 캐시에 넣지 않는다. 멤버십이 바뀌면 해당 사용자 캐시를 무효화한다.
- 결제 API의 최소 충전 금액은 운영 설정값에 따라 달라진다. 설정 API 또는 관리자 설정을 함께 확인해야 한다.
