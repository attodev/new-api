# 지갑 자동충전 프리셋 설계

## 목적

현재 지갑 `정기결제`와 `자동결제`는 사용자가 충전금액, 주기, 기준잔액을 직접 입력한다. 이를 관리자 프리셋 선택 방식으로 바꾼다.

목표는 다음과 같다.

- 관리자가 정기충전/자동충전 정책 프리셋을 만든다.
- 사용자는 직접 숫자를 입력하지 않고, 적용 가능한 프리셋 중 하나를 선택한다.
- 기존 구독 기능과 섞지 않고, 지갑 자동충전 메뉴 안에서만 동작한다.
- 선택된 프리셋은 사용자 정책에 스냅샷으로 복사한다.
- 관리자가 프리셋을 수정하거나 비활성화해도 기존 사용자 정책은 그대로 유지된다.

## 범위

이번 설계에서 지원하는 것:

- 정기충전 프리셋.
- 자동충전 프리셋.
- 개인 지갑, 조직 지갑, 둘 다에 적용 가능한 프리셋 범위.
- 관리자 시스템 설정의 과금 설정 안에서 프리셋 관리.
- 사용자 지갑 화면에서 프리셋 선택 기반 정책 생성.
- 적용 가능한 프리셋이 없으면 해당 메뉴 숨김.
- 적용 가능한 프리셋이 없어도 기존 정책이 있으면 메뉴 표시 후 확인/해지만 허용.

이번 설계에서 지원하지 않는 것:

- 사용자가 프리셋 값을 임의 수정하는 기능.
- Toss 외 결제수단.
- 기존 구독 플랜과 프리셋 공유.
- 프리셋 비활성화 시 기존 사용자 정책 자동 중단.
- 사용자별 또는 조직별 전용 프리셋 할당. 프리셋 적용 범위는 `개인`, `조직`, `둘 다`까지만 둔다.

## 결정 사항

- 프리셋은 완성형 정책 묶음이다.
  - 정기충전: 충전금액, 주기, 즉시충전 기본값.
  - 자동충전: 기준잔액, 충전금액.
- 프리셋은 개인/조직 공용 카탈로그로 관리한다.
- 각 프리셋에는 적용 대상 `user`, `organization`, `all`을 둔다.
- 프리셋 이름은 필수, 설명은 선택이다.
- 기존 정책은 스냅샷 유지 방식이다.
- 관리 위치는 `시스템 설정 > 과금 설정 > 자동충전 프리셋`이다.
- 적용 가능한 프리셋이 없고 기존 정책도 없으면 사용자 지갑에서 해당 탭 자체를 숨긴다.
- 적용 가능한 프리셋이 없지만 기존 정책이 있으면 탭을 표시하고, 기존 정책 확인/해지만 허용한다.

## 접근 방식

별도 DB 테이블 `wallet_auto_recharge_presets`를 추가한다. 기존 사용자 정책 테이블 `wallet_auto_recharges`는 실행 중인 정책을 나타내고, 새 프리셋 테이블은 관리자 카탈로그만 나타낸다.

이 방식을 선택한 이유:

- 사용자 정책과 관리자 카탈로그의 책임이 분리된다.
- 프리셋 활성화, 비활성화, 정렬, 적용 대상 필터링을 명확하게 처리할 수 있다.
- JSON 옵션 방식보다 검증과 테스트가 쉽다.
- 기존 자동충전 실행 흐름을 크게 흔들지 않는다.

## 데이터 모델

새 GORM 모델 `WalletAutoRechargePreset`을 추가한다.

핵심 필드:

- `id`
- `type`: `scheduled` 또는 `threshold`
- `target_scope`: `user`, `organization`, `all`
- `name`: 사용자에게 보이는 이름. 필수.
- `description`: 사용자에게 보이는 보조 설명. 선택.
- `amount`: 충전할 KRW 금액.
- `threshold_amount`: 자동충전 기준 KRW 금액. `threshold` 유형에서만 사용.
- `interval_unit`: `month`, `day`, `custom`. `scheduled` 유형에서만 사용.
- `interval_value`
- `custom_seconds`
- `charge_immediately`: 정기충전 생성 시 즉시 첫 충전 기본값.
- `sort_order`: 낮을수록 먼저 표시.
- `enabled`: 사용자에게 선택 가능 여부.
- `create_time`
- `update_time`

기존 `wallet_auto_recharges`에는 `preset_id`를 nullable 필드로 추가한다. 정책 생성 시 선택된 프리셋 id를 저장하되, 실행에 필요한 값은 현재처럼 정책 row에 복사한다.

복사되는 값:

- `amount`
- `threshold_amount`
- `threshold_quota`
- `interval_unit`
- `interval_value`
- `custom_seconds`
- `charge_immediately`

따라서 프리셋이 나중에 수정/비활성화되어도 기존 정책의 결제 금액, 주기, 기준잔액은 변하지 않는다.

## 관리자 기능

관리자는 `시스템 설정 > 과금 설정 > 자동충전 프리셋`에서 프리셋을 관리한다.

관리 UI 기능:

- 정기충전 프리셋 목록.
- 자동충전 프리셋 목록.
- 프리셋 추가.
- 프리셋 수정.
- 프리셋 활성화/비활성화.
- 정렬 순서 변경.

운영 UI에서는 삭제보다 비활성화를 기본 동작으로 둔다. 기존 사용자 정책이 `preset_id`를 참조할 수 있기 때문이다. 실제 API의 `DELETE`도 물리 삭제가 아니라 `enabled=false`로 바꾸는 비활성화로 처리한다.

관리자 API:

- `GET /api/admin/wallet/auto-recharge/presets`
- `POST /api/admin/wallet/auto-recharge/presets`
- `PUT /api/admin/wallet/auto-recharge/presets/:id`
- `DELETE /api/admin/wallet/auto-recharge/presets/:id`

관리자 API는 `AdminAuth`를 사용한다.

## 사용자 기능

사용자 지갑의 `정기결제`와 `자동결제` 탭은 직접 입력 폼 대신 프리셋 목록을 보여준다.

표시 규칙:

- 개인 지갑은 `target_scope = user` 또는 `all` 프리셋만 본다.
- 조직 지갑은 `target_scope = organization` 또는 `all` 프리셋만 본다.
- `enabled = true`인 프리셋만 새 정책 생성에 사용할 수 있다.
- 적용 가능한 정기충전 프리셋이 있으면 `정기결제` 탭을 표시한다.
- 적용 가능한 자동충전 프리셋이 있으면 `자동결제` 탭을 표시한다.
- 적용 가능한 프리셋이 없고 기존 정책도 없으면 해당 탭을 숨긴다.
- 적용 가능한 프리셋이 없지만 기존 정책이 있으면 해당 탭을 표시하고 기존 정책 확인/해지만 허용한다.

사용자가 프리셋을 선택하면 프론트엔드는 `preset_id`만 서버에 보낸다. 서버는 프리셋을 다시 조회하고, 프리셋 값을 정책 row에 복사한다. 서버는 클라이언트가 보낸 금액, 주기, 기준잔액을 신뢰하지 않는다.

사용자용 조회 API:

- `GET /api/user/wallet/auto-recharge/presets`
- `GET /api/organization/wallet/auto-recharge/presets`

정책 생성 API는 기존 endpoint를 유지하되 요청 body를 `preset_id` 중심으로 바꾼다.

- `POST /api/user/wallet/auto-recharge/scheduled`
- `POST /api/user/wallet/auto-recharge/threshold`
- `POST /api/organization/wallet/auto-recharge/scheduled`
- `POST /api/organization/wallet/auto-recharge/threshold`

이 설계의 정책은 관리자 프리셋 강제이므로, 새 구현은 `preset_id`가 없는 생성 요청을 거부한다. 기존 직접 입력 payload는 더 이상 정책 생성에 사용할 수 없다.

## 검증 규칙

프리셋 생성/수정 검증:

- `type`은 `scheduled` 또는 `threshold`만 허용한다.
- `target_scope`는 `user`, `organization`, `all`만 허용한다.
- `name`은 공백일 수 없다.
- `amount`는 Toss 최소 충전액 이상이어야 한다.
- `scheduled`는 유효한 `interval_unit`과 양수 `interval_value`가 필요하다.
- `custom` 주기는 양수 `custom_seconds`가 필요하다.
- `threshold`는 `threshold_amount >= 0`이어야 한다.

사용자 정책 생성 검증:

- `preset_id`가 필요하다.
- 프리셋이 존재해야 한다.
- 프리셋이 활성화되어 있어야 한다.
- endpoint 유형과 프리셋 유형이 일치해야 한다.
- 지갑 대상과 프리셋 적용 범위가 일치해야 한다.
- Toss 빌링과 결제 컴플라이언스 조건은 기존 자동충전과 동일하게 유지한다.
- 같은 대상에 같은 유형의 active/pending 정책이 있으면 기존처럼 실패한다.

## 오류 처리

- 프리셋이 없거나 비활성화된 경우 생성 요청은 실패한다.
- 적용 대상이 맞지 않는 프리셋을 선택하면 생성 요청은 실패한다.
- 프리셋 값이 현재 Toss 최소 충전액보다 작으면 생성 요청은 실패한다.
- 프리셋이 비활성화되어도 기존 활성 정책은 중단하지 않는다.
- 기존 정책이 참조하는 프리셋 row가 없어도 정책 실행에는 영향을 주지 않는다. 실행은 정책 row의 스냅샷 값을 사용한다.

## 프론트엔드 설계

관리자 화면:

- 과금 설정 화면에 `자동충전 프리셋` 섹션을 추가한다.
- 정기충전과 자동충전을 탭 또는 구분 섹션으로 보여준다.
- 각 프리셋은 이름, 설명, 적용 대상, 충전금액, 주기 또는 기준잔액, 활성 상태, 정렬 순서를 표시한다.
- 생성/수정 폼은 프리셋 유형에 따라 필요한 필드만 보여준다.

사용자 지갑 화면:

- 기존 `AutoRechargeCard`의 직접 입력 폼을 프리셋 선택 카드 목록으로 바꾼다.
- 프리셋 카드에는 이름, 설명, 충전금액, 주기 또는 기준잔액을 보여준다.
- 사용자가 프리셋을 선택하고 Toss 빌링 인증을 시작한다.
- 기존 정책이 있으면 현재 정책 영역에 스냅샷 값과 카드 정보를 보여준다.
- 기존 정책이 있고 선택 가능한 프리셋이 없으면 새 설정 UI는 숨기고 해지 액션만 보여준다.

## 테스트 계획

백엔드:

- 프리셋 모델 검증 테스트.
- 관리자 CRUD 테스트.
- 프리셋 정렬 및 유형별 조회 테스트.
- 개인/조직 적용 대상 필터링 테스트.
- 정책 생성 시 서버가 프리셋 값을 스냅샷으로 복사하는지 테스트.
- `preset_id` 없이 생성하면 실패하는지 테스트.
- 프리셋 유형과 endpoint 유형이 다르면 실패하는지 테스트.
- 프리셋 비활성화 후 기존 정책 실행이 유지되는지 테스트.
- 기존 Toss billing confirm/fail cleanup 회귀 테스트.

프론트엔드:

- 프리셋이 있을 때 정기결제/자동결제 탭 표시 테스트.
- 프리셋이 없고 기존 정책도 없을 때 탭 숨김 테스트.
- 프리셋이 없지만 기존 정책이 있을 때 탭 표시 및 해지만 가능한 상태 테스트.
- 프리셋 선택 시 `preset_id`만 생성 API로 전달되는지 테스트.
- 관리자 프리셋 폼의 유형별 필드 표시 테스트.
- i18n 동기화와 locale 등록 테스트.

회귀 검증:

- 기존 일반 Toss 충전.
- 기존 Toss 구독.
- 기존 지갑 자동충전 실행 task.
- 개인/조직 지갑 권한.
- `web/default` build.

## 구현 메모

- JSON marshal/unmarshal은 프로젝트 규칙에 따라 `common` 래퍼를 사용한다.
- DB 마이그레이션은 GORM `AutoMigrate`를 사용하고 SQLite, MySQL, PostgreSQL 호환성을 유지한다.
- raw SQL이나 DB별 부분 인덱스는 사용하지 않는다.
- 새 UI 문구는 `web/default/src/i18n/locales/*`에 반영한다.
- 사용자가 직접 값을 입력하는 기존 UI는 프리셋 선택 UI로 대체한다.
