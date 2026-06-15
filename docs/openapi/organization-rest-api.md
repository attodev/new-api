# 조직 관련 Backend REST API 명세

작성일: 2026-06-15

## 1. 문서 범위

이 문서는 현재 구현된 조직 관리 관련 REST API를 정리한다. 기준 코드는 다음 파일이다.

- `router/api-router.go`
- `controller/organization.go`
- `controller/organization_subscription.go`
- `controller/topup*.go`
- `model/organization.go`
- `model/organization_subscription.go`
- `common/page_info.go`

기본 prefix는 `/api`다.

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
| 전체 관리자 | `role = root` | `/api/organizations/*`, 선택 조직 대상 `/api/organization/*` |
| 조직 소유자 | `organization_role = owner` | 자기 조직 관리, 조직 지갑 충전, 멤버 추가 |
| 조직 관리자 | `organization_role = admin` | 자기 조직 사용자 관리, 대시보드/로그/구독 관리 |
| 조직 일반 사용자 | `organization_role = member` | 자기 활성 조직 구독 조회 등 제한된 API |

조직 관리자 권한은 `admin` 또는 `owner`다. 조직 소유자 전용 권한은 `owner`만 해당한다.

전체 관리자가 `/api/organization/*` 계열의 조직별 화면 API를 호출할 때는 대부분 `organization_id` query가 필수다. 조직 소유자/관리자는 자신의 `organization_id`가 자동으로 사용된다.

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

예를 들어 `quota`는 대부분 `int`이며 음수 검증이 있는 API와 없는 API가 섞여 있다.

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
| `quota` | integer | 선택 | `0`-`1,000,000,000` | 생략 시 0. 범위 밖이면 오류. |

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
| `description` | string | 선택 | 0-255자 | trim 후 저장. 255자 초과이면 오류 |
| `quota` | integer | 선택 | `0`-`1,000,000,000` | 범위 밖이면 오류 |
| `status` | integer | 선택 | `1`, `2` | enum 외 값이면 오류 |

최소 하나의 필드는 포함해야 한다. 그렇지 않으면 `no organization fields to update` 오류가 발생한다.

예시:

```json
{
  "description": "updated",
  "quota": 500000,
  "status": 1
}
```

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
| `username` | string | 선택 | 임의 문자열 | 사용자명 필터 |
| `token_name` | string | 선택 | 임의 문자열 | 토큰명 필터 |
| `model_name` | string | 선택 | 임의 문자열 | 모델명 필터 |
| `channel` | integer | 선택 | 채널 ID | 파싱 실패 시 0으로 처리 |
| `group` | string | 선택 | 사용자 그룹 | 그룹 필터 |
| `request_id` | string | 선택 | 요청 ID | 요청 ID 필터 |
| `upstream_request_id` | string | 선택 | upstream 요청 ID | upstream 요청 ID 필터 |

응답 `data`: `PageInfo<Log[]>`

주의:

- 조직 밖 사용자 로그는 포함되지 않는다.
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
| `start_timestamp` | string | 선택 | Unix seconds 문자열 권장 | 시작 시각 |
| `end_timestamp` | string | 선택 | Unix seconds 문자열 권장 | 종료 시각 |

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

응답 `data`: `PageInfo<User[]>`

처리 규칙:

- 조직 소속 사용자 중 `role < RoleAdminUser`인 사용자만 조회한다.
- 전역 관리자 계정은 조직 사용자 목록에서 제외된다.

### 5.2 조직에 할당 가능한 사용자 목록 조회

`GET /api/organization/assignable-users`

조직 소유자 전용이다.

Query:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `p` | integer | 선택 | 1 이상 권장 | 페이지 번호 |
| `page_size` | integer | 선택 | 1-100 권장 | 페이지 크기 |

응답 `data`: `PageInfo<User[]>`

처리 규칙:

- 전역 관리자 계정은 제외된다.
- 자기 자신은 제외된다.
- 현재 구현은 `organization_id = actor.OrganizationId OR organization_id = 0` 조건을 사용한다. 즉 같은 조직 사용자와 조직 미소속 사용자가 함께 포함될 수 있다.

### 5.3 조직 사용자 상세 조회

`GET /api/organization/users/:id`

조직 소유자/관리자만 자기 조직의 일반 사용자를 조회할 수 있다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 사용자 ID |

응답 `data`: `User`

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
| `status` | integer | 선택 | `1`, `2` | enum 외 값이면 오류 |
| `quota` | integer | 선택 | `0`-`1,000,000,000` | 범위 밖이면 오류. active 조직 구독 사용자는 프론트에서 입력을 막는다. |
| `remark` | string | 선택 | 0-255자 | trim 후 저장. 255자 초과이면 오류 |

최소 하나의 필드는 포함해야 한다. 그렇지 않으면 `no organization user fields to update` 오류가 발생한다.

예시:

```json
{
  "quota": 100000,
  "status": 1,
  "remark": "team member"
}
```

주의:

- 이 API는 `quota`를 `0`-`1,000,000,000` 범위로 강제한다.
- active 조직 구독이 있는 사용자는 quota 방식이 아니라 plan 방식으로 관리하므로 클라이언트에서 quota 입력을 비활성화해야 한다.

### 5.5 사용자 조직 멤버십 할당

`PUT /api/organization/users/:id/membership`

조직 소유자 전용이다.

Path:

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `id` | integer | 필수 | 양의 정수 | 할당할 사용자 ID |

Body:

| 이름 | 타입 | 필수 | 가능한 값 | 설명 |
| --- | --- | --- | --- | --- |
| `organization_role` | string | 필수 | `member`, `admin`, `owner` | 대상 사용자에게 부여할 조직 역할 |

예시:

```json
{
  "organization_role": "member"
}
```

처리 규칙:

- 조직 소유자 본인은 재할당할 수 없다.
- 전역 관리자 사용자는 조직 소유자가 관리할 수 없다.
- 역할 값이 `member`, `admin`, `owner`가 아니면 오류다.
- 현재 구현은 대상 사용자가 이미 다른 조직에 속해 있는지 이 API 내부에서 직접 차단하지 않는다. 프론트의 할당 후보 목록에서 조직 미소속 사용자만 선택하도록 제한해야 한다.

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
| `user` | object | 사용자 정보 |
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
| `total_amount` | integer | 선택 | `0`-`1,000,000,000` | 범위 밖이면 오류. 생략 시 0 |

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

- `id`와 `organization_id`가 모두 일치하는 plan만 수정한다.
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
- 같은 사용자에게 기존 active 구독이 있으면 취소되고 새 active 구독이 생성된다.
- 새 구독의 `amount_total`은 plan의 `total_amount`다.

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

- 해당 조직/사용자의 active 조직 구독을 취소한다.
- 만료 시간이 지났더라도 `status=active`인 구독은 취소 대상이다.
- active 구독이 없으면 모델 레벨에서 오류가 날 수 있다.

## 7. 조직 지갑과 충전 API

조직 지갑 API는 조직 소유자 전용이다. 호출 시 서버가 topup target을 `organization`으로 설정하므로, 결제 완료 후 quota는 개인 사용자가 아니라 조직에 적립된다.

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
| POST | `/api/organization/waffo/amount` | `WaffoPayRequest` | Waffo 금액 계산 |
| POST | `/api/organization/waffo-pancake/amount` | `WaffoPancakePayRequest` | Waffo Pancake 금액 계산 |

#### `AmountRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `min_topup`-`1,000,000,000` | 충전 단위. quota 표시 방식이 tokens이면 token 수로 해석될 수 있다. |

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
| `amount` | integer | 필수 | `waffo_min_topup`-`1,000,000,000` | 충전 단위 |
| `pay_method_index` | integer 또는 null | 선택 | 서버 결제수단 목록 index | nil이면 Waffo 자동 선택 |
| `pay_method_type` | string | 선택 | deprecated | 예전 프론트 호환용 |
| `pay_method_name` | string | 선택 | deprecated | 예전 프론트 호환용 |

#### `WaffoPancakePayRequest`

| 이름 | 타입 | 필수 | 범위/값 | 설명 |
| --- | --- | --- | --- | --- |
| `amount` | integer | 필수 | `waffo_pancake_min_topup`-`1,000,000,000` | 충전 단위 |

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
| `amount` | integer | 필수 | `min_topup`-`1,000,000,000` | 충전 단위 |
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
| 조직 quota 또는 사용자 quota 범위 초과 | `quota must be between 0 and 1000000000` |
| 조직 구독 plan 총량 범위 초과 | `total_amount must be between 0 and 1000000000` |
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
- `/api/organization/subscription/plans/:id` 수정 API는 현재 부분 수정이어도 `title`을 요구한다.
- 조직 쓰기 API는 strict JSON decoding을 사용한다. 문서에 없는 body 필드는 무시하지 않고 `unsupported field: <field>`로 거부한다.
- 일부 query 숫자 필드는 파싱 실패 시 오류가 아니라 0으로 처리된다. 문서의 "실제 검증" 항목을 우선한다.
- 조직 사용자 수정 API의 `quota`는 `0`-`1,000,000,000` 범위를 강제한다.
- 결제 API의 최소 충전 금액은 운영 설정값에 따라 달라진다. 설정 API 또는 관리자 설정을 함께 확인해야 한다.
