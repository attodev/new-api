# 조직 관리 통합 스펙

## 문서 목적

이 문서는 2026-06-03부터 2026-06-08까지 진행된 조직 관리 관련 작업을 하나로 모은 통합 스펙이다. 기존에는 조직 관리, 조직 과금, quota 금액 입력, 조직 사용량 대시보드, 차트 확장 문서가 각각 나뉘어 있었고 일부 기능은 구현 중 새로 추가되었다. 이 문서는 현재 구현된 기능을 기준으로 최종 동작, 권한, 데이터 흐름, API, UI, 테스트 범위를 한곳에 정리한다.

참고한 기존 문서:

- `docs/superpowers/specs/2026-06-03-organization-design.md`
- `docs/superpowers/specs/2026-06-03-organization-billing-owner-design.md`
- `docs/superpowers/specs/2026-06-03-quota-money-input-design.md`
- `docs/superpowers/specs/2026-06-04-organization-usage-dashboard-design.md`
- `docs/superpowers/specs/2026-06-07-organization-dashboard-charts-design.md`

## 배경

기존 시스템의 전역 권한은 `User.Role`로 나뉜다.

- 일반 사용자: `RoleCommonUser = 1`
- 관리자: `RoleAdminUser = 10`
- 전체 관리자: `RoleRootUser = 100`

기존 `User.Group`은 조직이나 팀 멤버십이 아니라 모델 접근, 채널 선택, 그룹별 과금 배율, 토큰 그룹, 구독 업그레이드, 로그, 성능 지표에 이미 사용된다. 따라서 조직 기능은 `Group`을 재사용하지 않고 별도 필드와 테이블로 구성한다.

## 목표

- 전체 관리자가 조직을 만들고 조직 소유자를 지정할 수 있다.
- 사용자는 하나의 조직에만 속할 수 있다.
- 조직 역할은 `member`, `admin`, `owner`로 구분한다.
- 조직 소유자와 조직 관리자는 자기 조직 사용자와 사용량을 관리할 수 있다.
- 조직 사용자는 개인 충전 대신 조직에서 할당받은 한도 안에서 사용한다.
- 조직 전체 사용량은 조직 자체 quota 지갑에서 차감된다.
- 전체 관리자는 조직 소유자와 같은 조직 대시보드/로그 화면을 보되, 대상 조직을 선택할 수 있다.
- 조직 대시보드는 관리자 대시보드에 가까운 차트 기능을 제공한다.

## 비목표

- 기존 전역 관리자 권한 체계를 범용 RBAC로 대체하지 않는다.
- 기존 `User.Group`의 의미를 바꾸지 않는다.
- 조직 관리자가 전역 설정, 채널, 모델 가격, 결제 설정, 전역 사용자 역할을 관리할 수 있게 하지 않는다.
- 다중 조직 멤버십, 조직 초대, 조직별 API key는 이번 범위에 포함하지 않는다.

## 데이터 모델

### Organization

`organizations` 테이블은 조직 자체와 조직 선불 지갑을 표현한다.

- `id`
- `name`
- `description`
- `owner_user_id`
- `quota`
- `used_quota`
- `status`
- `created_at`
- `updated_at`

상태 값:

- `OrganizationStatusEnabled = 1`
- `OrganizationStatusDisabled = 2`

### User 조직 필드

`users` 테이블에는 조직 멤버십 필드를 추가한다.

- `organization_id`
- `organization_role`

조직 역할:

- `member`: 조직 일반 사용자
- `admin`: 조직 사용자와 사용량을 관리할 수 있는 조직 관리자
- `owner`: 조직 소유자. 조직 사용자 배정, 역할 변경, 조직 지갑 충전을 수행할 수 있다.

### TopUp 대상

충전 기록은 개인 충전과 조직 충전을 구분할 수 있게 대상 필드를 가진다.

- `target_type`: `user` 또는 `organization`
- `target_id`: 실제 충전 대상 사용자 ID 또는 조직 ID

기존 개인 충전은 `target_type=user`로 해석된다. 조직 충전은 `target_type=organization`으로 저장되고 성공 시 조직 `quota`를 증가시킨다.

## 권한 모델

전역 역할과 조직 역할은 독립적이다. 조직 관리자 또는 조직 소유자는 전역 역할로는 일반 사용자일 수 있다.

### 전체 관리자

전체 관리자는 다음을 할 수 있다.

- 조직 목록 조회
- 조직 생성
- 조직 설명, 상태, quota 수정
- 조직 생성 시 소유자 지정
- 조직 대시보드 조회
- 조직 사용로그 조회
- 조직 작업로그 조회

전체 관리자가 조직 대시보드/로그를 볼 때는 화면에서 대상 조직을 선택한다. 선택한 조직 ID는 API 요청의 `organization_id`로 전달된다.

### 조직 소유자

조직 소유자는 다음을 할 수 있다.

- 자기 조직 사용자 목록 조회
- 자기 조직 사용자 quota, 상태, remark 수정
- 조직에 새 사용자 배정
- 조직 내 사용자 역할 변경
- 조직 지갑 조회 및 충전
- 조직 대시보드 조회
- 조직 사용로그 조회
- 조직 작업로그 조회

조직 소유자는 조직 내 사용량과 조직 quota 지갑을 운영하는 책임자다.

### 조직 관리자

조직 관리자는 다음을 할 수 있다.

- 자기 조직 사용자 목록 조회
- 자기 조직 사용자 quota, 상태, remark 수정
- 조직 대시보드 조회
- 조직 사용로그 조회
- 조직 작업로그 조회

조직 관리자는 조직 지갑 충전과 조직 소유자 지정은 수행하지 않는다.

### 조직 일반 사용자

조직 일반 사용자는 다음을 할 수 있다.

- 자기 지갑 화면에서 본인에게 할당된 quota와 사용량 확인
- 조직에서 할당받은 한도 내에서 API 사용

조직 일반 사용자의 지갑 화면은 개인 충전 중심이 아니라 조직 할당 한도 확인 중심으로 동작한다.

### 제한 사항

조직 관리자/소유자는 다음 작업을 할 수 없다.

- 전역 `User.Role` 변경
- 기존 `User.Group` 변경
- 전역 admin/root 사용자 관리
- 조직 밖 사용자 관리
- 채널, 모델, 결제, 시스템 설정 변경
- 비밀번호 변경 또는 초기화

## 조직 생성과 사용자 배정

전체 관리자는 조직을 생성하면서 소유자를 지정한다.

소유자 후보 목록은 다음 조건을 만족해야 한다.

- 전역 admin/root가 아니다.
- 이미 다른 조직에 속해 있지 않다.

백엔드도 같은 제약을 검증한다. 이미 조직에 속한 사용자를 조직 소유자로 지정하려 하면 `user already belongs to an organization` 오류가 발생한다.

조직 소유자는 조직에 속하지 않은 일반 사용자를 자기 조직에 배정할 수 있다. 배정 시 조직 역할은 `member`, `admin`, `owner` 중 하나로 지정한다.

## 조직 quota와 과금 흐름

조직 기능의 핵심은 개인 사용자처럼 조직도 선불 quota를 가진다는 점이다.

### 조직이 없는 사용자

조직이 없는 일반 사용자는 기존과 동일하게 본인 quota에서 선차감과 정산이 이루어진다.

### 조직 사용자

조직 사용자가 API를 호출하면 다음 두 조건을 모두 만족해야 한다.

- 사용자 본인의 quota 한도가 충분해야 한다.
- 조직 자체 quota 잔액이 충분해야 한다.

사용 비용은 실제 호출자인 사용자에게 기록되지만, 조직 전체 선불 예산에서도 함께 차감된다.

예시:

```text
요청 비용: 25 quota

사용자 quota: 1000 -> 975
사용자 used_quota: +25
조직 quota: 500000 -> 499975
조직 used_quota: +25
로그 user_id: 실제 호출 사용자
```

실제 비용이 선차감보다 적으면 사용자 quota와 조직 quota 모두 차액이 환불된다. 실패 또는 환불도 두 대상에 함께 반영된다.

### 조직 사용자 quota 의미

조직 사용자 quota는 전체 관리자에게 받은 돈이 아니라 조직 내부에서 배정한 사용 한도다. 실제 조직 단위 선불 예산은 `organizations.quota`다.

## 조직 지갑

조직 소유자는 조직 지갑 화면에서 조직 quota를 충전할 수 있다. 개인 지갑과 가능한 UI 흐름을 공유하되, 충전 대상은 개인 사용자가 아니라 조직이다.

지원 대상:

- Epay
- Stripe
- PayPal
- Creem
- Waffo
- Waffo Pancake

충전 성공 시 `top_ups.target_type=organization`, `target_id=organization.id` 기준으로 조직 quota가 증가한다.

조직에 속한 일반 사용자 지갑은 개인 충전 대신 다음 정보 확인 중심으로 바뀐다.

- 본인에게 할당된 quota
- 본인의 used quota
- 조직에서 할당된 사용 한도 안내

## quota 금액 입력

UI에서 quota raw 단위만 노출하면 금액 감각과 맞지 않기 때문에, 관리자와 조직 관리자 화면은 금액 기준 입력을 지원한다.

기준 환산:

```text
500,000 quota = $1
```

적용 범위:

- 조직 사용자 quota 입력
- 관리자 사용자 quota 조정 다이얼로그의 빠른 금액 버튼
- 조직 quota 표시와 충전 화면

백엔드 저장 단위는 여전히 quota다. 프론트에서 표시/입력 시 금액과 quota를 변환한다.

## 조직 대시보드

조직 대시보드는 조직 소유자, 조직 관리자, 전체 관리자가 볼 수 있다.

### 접근 방식

- 조직 소유자/관리자: 자기 조직 기준
- 전체 관리자: 화면에서 선택한 조직 기준

전체 관리자의 마지막 선택 조직은 브라우저 localStorage에 저장된다. 조직 대시보드, 조직 사용로그, 조직 작업로그 사이를 이동하거나 다른 메뉴에 갔다 돌아와도 마지막으로 선택한 조직이 유지된다.

### 기간

지원 프리셋:

- 오늘
- 최근 7일
- 최근 30일
- custom

API 쿼리:

- `start_timestamp`
- `end_timestamp`
- `preset`
- 전체 관리자일 때 `organization_id`

### 응답 데이터

조직 대시보드는 다음 데이터를 반환한다.

- `organization`: 조직 이름, quota, used quota
- `summary`: 기간 사용량, 기간 요청 수, 멤버 수, 활성 멤버 수
- `daily_usage`: 날짜별 조직 전체 사용량
- `top_users`: 기간 내 사용자별 사용량
- `top_models`: 기간 내 모델별 사용량
- `model_usage`: 날짜+모델별 사용량
- `user_usage`: 날짜+사용자별 사용량

집계는 `quota_data`와 `users.organization_id`를 기준으로 수행한다. 조직 밖 사용자의 데이터는 포함하지 않는다.

### 화면 구성

조직 대시보드는 다음 순서로 구성된다.

1. 조직 잔액, 조직 사용량, 기간 사용량, 기간 요청 수 요약 카드
2. 조직 사용량 분포 차트
3. 모델 분석 차트
4. 사용자 사용량 순위/추세 차트
5. Top 사용자 표
6. Top 모델 표

관리자 대시보드의 `VChart` 기반 차트 패턴을 재사용한다.

## 조직 사용로그와 작업로그

조직 메뉴에는 일반 메뉴의 사용로그/작업로그에 대응하는 조직 전용 화면이 있다.

- 조직 사용로그: `/organization/usage-logs/common`
- 조직 작업로그: `/organization/usage-logs/task`
- 조직 이미지/그리기 로그: `/organization/usage-logs/drawing`

접근 가능:

- 조직 소유자
- 조직 관리자
- 전체 관리자

전체 관리자는 조직 선택기를 통해 대상 조직을 지정한다. 선택된 조직은 대시보드와 같은 localStorage 키로 유지된다.

백엔드 로그 API는 조직 사용자 ID 목록을 구한 뒤 `user_id IN (...)` 조건으로 조회한다. Midjourney/작업 로그도 동일하게 조직 사용자 범위로 제한된다.

## API

### 입력값 검증 정책

조직 관련 쓰기 API는 서버에서 입력값 범위를 강제한다. 문서에 적힌 길이와 숫자 범위는 권장이 아니라 API가 실제로 검증하는 조건이다.

쓰기 API body는 strict JSON decoding을 사용한다. 허용되지 않은 필드를 보내면 `unsupported field: <field>` 오류가 발생한다.

| API 영역 | 필드 | 허용 범위 |
| --- | --- | --- |
| 조직 생성 | `name` | trim 후 1자 이상 64자 이하 |
| 조직 생성/수정 | `description` | trim 후 255자 이하 |
| 조직 생성 | `owner_user_id` | 1 이상 1,000,000,000 이하 |
| 조직 생성/수정 | `quota` | 0 이상 1,000,000,000 이하 |
| 조직 수정 | `status` | `1` enabled 또는 `2` disabled |
| 조직 사용자 수정 | `status` | `1` enabled 또는 `2` disabled |
| 조직 사용자 수정 | `quota` | 0 이상 1,000,000,000 이하 |
| 조직 사용자 수정 | `remark` | trim 후 255자 이하 |
| 조직 구독 플랜 | `title` | trim 후 1자 이상 128자 이하 |
| 조직 구독 플랜 | `subtitle` | trim 후 255자 이하 |
| 조직 구독 플랜 | `duration_unit` | `year`, `month`, `day`, `hour`, `custom` |
| 조직 구독 플랜 | `duration_value` | 0 이상 1200 이하. custom이 아니고 0이면 1로 보정 |
| 조직 구독 플랜 | `custom_seconds` | custom일 때 1 이상 31,536,000 이하 |
| 조직 구독 플랜 | `total_amount` | 0 이상 1,000,000,000 이하 |
| 조직 구독 플랜 | `sort_order` | -1,000,000 이상 1,000,000 이하 |
| 조직 구독 할당 | `plan_id` | 1 이상 1,000,000,000 이하 |

조직 충전 금액도 상한을 가진다. Epay, Waffo, Waffo Pancake 계열은 1,000,000,000 이하, Stripe와 PayPal 계산 API는 10,000 이하만 허용한다.

### 전체 관리자 조직 API

`/api/organizations`는 전체 관리자 전용이다.

- `GET /api/organizations/`: 조직 목록 조회
- `POST /api/organizations/`: 조직 생성
- `PATCH /api/organizations/:id`: 조직 설명, 상태, quota 수정

### 현재 조직 API

`/api/organization`은 인증 사용자 전용이며, 내부에서 조직 역할을 검사한다.

- `GET /api/organization/`: 조직 profile 조회
- `GET /api/organization/dashboard`: 조직 대시보드 조회
- `GET /api/organization/logs`: 조직 사용로그 조회
- `GET /api/organization/mj`: 조직 그리기 로그 조회
- `GET /api/organization/task`: 조직 작업로그 조회
- `GET /api/organization/users`: 조직 사용자 목록
- `GET /api/organization/assignable-users`: 조직 배정 후보 사용자 목록
- `GET /api/organization/users/:id`: 조직 사용자 상세
- `PATCH /api/organization/users/:id`: 조직 사용자 제한 필드 수정
- `PUT /api/organization/users/:id/membership`: 조직 사용자 배정/역할 변경
- `GET /api/organization/wallet`: 조직 지갑 조회
- `GET /api/organization/topup/self`: 조직 충전 내역 조회
- `POST /api/organization/pay`: 조직 Epay 결제
- `POST /api/organization/amount`: 조직 Epay 금액 계산
- `POST /api/organization/stripe/pay`
- `POST /api/organization/stripe/amount`
- `POST /api/organization/paypal/pay`
- `POST /api/organization/paypal/amount`
- `POST /api/organization/creem/pay`
- `POST /api/organization/waffo/pay`
- `POST /api/organization/waffo/amount`
- `POST /api/organization/waffo-pancake/pay`
- `POST /api/organization/waffo-pancake/amount`

## 프론트엔드 메뉴와 화면

조직 메뉴는 다음 항목을 가진다.

- 조직 사용자
- 조직 대시보드
- 조직 사용로그
- 조직 작업로그

조직 메뉴 표시 조건:

- 전체 관리자
- 조직 소유자
- 조직 관리자

전체 관리자는 조직 그룹 전체 메뉴를 볼 수 있다. 조직 소유자/관리자는 자기 조직 기준 화면을 본다.

개인 지갑 라우트는 조직 사용자일 때 역할에 따라 다르게 동작한다.

- 조직 일반 사용자: 개인 충전 대신 조직 할당 한도 요약
- 조직 소유자: 조직 지갑 중심 화면
- 조직 없는 사용자: 기존 개인 지갑

## 결제 컴플라이언스 기본값

기존 결제/구독/충전 기능은 컴플라이언스 확인 여부가 false이면 잠기도록 되어 있었다. 이번 작업에서는 개인 사용 환경을 전제로 별도 확인 없이 통과되도록 기본값을 변경했다.

백엔드 기본값:

- `payment_setting.compliance_confirmed = true`
- `payment_setting.compliance_terms_version = v1`

프론트 기본값도 동일하게 `true / v1`로 맞춘다. 단, `terms_version`이 현재 코드의 `CurrentComplianceTermsVersion`과 다르면 미확인으로 판단하는 구조는 유지한다.

## i18n

새 UI 문구는 기본 프론트의 i18n 파일에 반영한다.

대상 언어:

- `en`
- `zh`
- `fr`
- `ja`
- `ru`
- `vi`
- `kr`

문서는 한국어로 작성한다. 이후 기능 문서도 한국어를 기본으로 한다.

## DB 호환성

DB 변경은 SQLite, MySQL, PostgreSQL을 모두 지원해야 한다.

구현 원칙:

- GORM migration을 우선 사용한다.
- raw SQL이 필요한 경우 DB별 quoting 규칙을 따른다.
- reserved word 컬럼은 기존 helper를 사용한다.
- 조직 역할은 문자열로 저장한다.
- JSON marshal/unmarshal은 프로젝트 규칙에 따라 `common.*` wrapper를 사용한다.

## 테스트와 검증

추가/확장된 테스트 범위:

- 조직 생성 시 소유자 지정
- 조직 생성/수정, 조직 사용자 수정, 조직 구독 플랜 생성/수정, 구독 할당 API의 길이/숫자/enum 검증
- 이미 조직에 속한 사용자를 새 조직 소유자로 지정하지 못함
- 조직 관리자/소유자 권한 검증
- 조직 밖 사용자 관리 차단
- 조직 대시보드 집계가 조직 사용자로 제한됨
- 전체 관리자가 선택한 조직의 대시보드를 볼 수 있음
- 전체 관리자 조직 대시보드에 `organization_id`가 필요함
- 전체 관리자가 선택한 조직의 로그만 볼 수 있음
- 조직 대시보드 `model_usage`, `user_usage` 집계
- 결제 컴플라이언스 기본값 true
- 구버전 terms version은 미확인 처리

주요 검증 명령:

```bash
go test ./controller -run 'TestOrganization(RootCanReadSelectedOrganizationDashboard|RootDashboardRequiresOrganizationId|RootCanReadSelectedOrganizationLogs|AdminCanReadDashboard|OwnerCanReadDashboard|RootCanCreateOrganization|RootCanUpdateOrganizationQuota)' -count=1
go test ./model -run 'Test(GetOrganizationDashboardScopesQuotaDataToOrganization|CreateOrganizationRejectsUserAlreadyInOrganization|CreateOrganizationAssignsOwner)' -count=1
go test ./setting/operation_setting -run 'TestPaymentCompliance(DefaultIsConfirmed|RejectsOutdatedTermsVersion)' -count=1
cd web/default && bun run build
git diff --check
```

## 남은 과제와 확장 후보

이번 범위에서 의도적으로 남긴 확장 후보는 다음과 같다.

- 조직 초대 링크
- 조직별 API key
- 다중 조직 멤버십
- 조직별 모델/채널 정책
- 조직별 월간 예산 또는 알림
- 조직 관리자 권한 세분화
- 전체 관리자용 조직 비교 대시보드
- 조직 지갑 환불/정산 리포트
- 조직 사용자별 비용 배분 리포트

## 현재 기준 요약

현재 구현은 “전역 사용자 + 조직 사용자”를 분리해 관리하는 첫 번째 안정화 버전이다. 전역 관리자는 조직을 만들고 전체 조직을 조회할 수 있으며, 조직 소유자와 조직 관리자는 자기 조직의 사용자와 사용량을 관리한다. 조직 구성원의 실제 사용은 사용자 로그로 남되, 비용은 조직 선불 quota 지갑에도 함께 반영된다. 조직 대시보드와 조직 로그는 조직 단위 운영을 위한 기본 관측 화면으로 제공된다.
