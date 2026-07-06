# 지갑 정기충전/자동충전 통합 설계서

작성일: 2026-06-30

## 1. 목적

기존 구독 기능은 사용권, 기간, 구독 플랜 중심으로 동작한다. 이번 기능은 그 구독과 분리하여, 일반 지갑 충전 금액과 직접 연동되는 자동 결제 정책을 제공한다.

새 정책은 두 메뉴로 제공한다.

- 정기충전: 사용자가 선택한 금액을 정해진 주기마다 Toss 빌링키로 자동 충전한다.
- 자동충전: 지갑 잔액이 관리자가 정한 기준 금액 이하가 되면 Toss 빌링키로 지정 금액을 자동 충전한다.

두 기능 모두 기존 일반 결제 충전과 같은 `top_ups` 완료 흐름을 사용한다. 따라서 결제 성공 후 사용 가능한 잔액이 실제로 증가한다.

## 2. 범위

포함 범위:

- Toss 빌링키 등록 기반 정기충전/자동충전
- 개인 지갑과 조직 지갑 지원
- 관리자 프리셋 관리
- 사용자 프리셋 선택 UI
- 기존 구독 메뉴와 분리
- 정기충전/자동충전 메뉴가 사용할 수 있는 프리셋이 없으면 메뉴 자체를 숨김
- E2E 문서 캡처용 Playwright 테스트

제외 범위:

- Stripe, PayPal, Pancake 등 Toss 외 결제수단 지원
- 사용자가 임의 금액을 입력하는 방식
- 기존 구독 플랜 결제 정책 변경
- 결제 실패 알림, 이메일 알림, 관리자 감사 로그 상세 확장

## 3. 핵심 개념

### 3.1 프리셋

프리셋은 관리자가 만든 선택지다. 사용자는 직접 금액이나 기준값을 입력하지 않고, 관리자가 준비한 버튼 중 하나를 선택한다.

테이블:

- `wallet_auto_recharge_presets`

역할:

- 관리자 화면의 옵션 상태를 DB에 저장한다.
- 사용자 화면에서 선택 가능한 정기충전/자동충전 버튼을 만든다.
- 기존에 생성된 사용자 정책을 직접 바꾸지는 않는다.

### 3.2 실행 정책

실행 정책은 사용자가 Toss 빌링키 등록을 완료한 뒤 실제로 동작하는 자동 결제 규칙이다.

테이블:

- `wallet_auto_recharges`

역할:

- 특정 사용자 지갑 또는 조직 지갑에 연결된다.
- Toss 빌링키, 카드 마스킹 정보, 상태, 실패 횟수, 마지막 결제 정보, 다음 결제 시간 등을 보관한다.
- 프리셋에서 값을 복사한 스냅샷으로 동작한다.

### 3.3 스냅샷 정책

사용자가 프리셋을 선택하면 서버는 프리셋 값을 `wallet_auto_recharges`에 복사한다.

복사되는 주요 값:

- `preset_id`
- `type`
- `amount`
- `threshold_amount`
- `threshold_quota`
- `interval_unit`
- `interval_value`
- `custom_seconds`
- `charge_immediately`

이후 관리자가 프리셋을 수정하거나 비활성화해도 이미 활성화된 사용자 정책은 즉시 변경되지 않는다. 운영 중인 정책의 예측 가능성을 위해 이 방식을 사용한다.

## 4. 금액과 단위

관리자와 사용자가 보는 금액 단위는 원(KRW)이다.

- 충전 금액: 원(KRW)
- 자동충전 기준 금액: 원(KRW)
- Toss 최소 충전 금액: `setting.TossMinTopUp`, 기본값 1000원

서버 내부에서는 기존 지갑 충전과 동일하게 quota로 변환한다.

- Toss 결제 요청 금액은 원 단위 정수다.
- 기준 금액은 `threshold_quota`로 환산되어 잔액 비교에 사용된다.
- 실제 충전 성공 시 `top_ups.money * common.QuotaPerUnit` 값이 지갑 quota에 더해진다.

## 5. 데이터 모델

### 5.1 `wallet_auto_recharge_presets`

주요 필드:

- `id`: 프리셋 ID
- `type`: `scheduled` 또는 `threshold`
- `target_scope`: `user`, `organization`, `all`
- `name`: 내부 표시명
- `description`: 설명
- `amount`: 충전 금액, KRW
- `threshold_amount`: 자동충전 기준 금액, KRW
- `interval_unit`: `day`, `month`, `custom`
- `interval_value`: 기간 값
- `custom_seconds`: custom 기간 초 단위
- `charge_immediately`: 정기충전 등록 직후 즉시 첫 충전할지 여부
- `sort_order`: 표시 순서
- `enabled`: 활성 여부
- `create_time`, `update_time`

검증:

- `type`은 `scheduled`, `threshold`만 허용
- `target_scope`는 `user`, `organization`, `all`만 허용
- `amount`는 0보다 커야 함
- `amount`는 Toss 최소 충전 금액 이상이어야 함
- 정기충전은 기간 정보가 필요함
- 자동충전은 `threshold_amount >= 0`이어야 함

### 5.2 `wallet_auto_recharges`

주요 필드:

- `id`: 정책 ID
- `preset_id`: 선택 당시 프리셋 ID
- `type`: `scheduled` 또는 `threshold`
- `target_type`: `user` 또는 `organization`
- `target_id`: 대상 사용자 또는 조직 ID
- `active_key`: 같은 대상의 같은 정책 유형 중복 방지 키
- `owner_user_id`: 결제 책임 사용자
- `billing_key_id`: Toss 빌링키 ID
- `amount`: 충전 금액, KRW
- `threshold_amount`: 자동충전 기준 금액, KRW
- `threshold_quota`: 잔액 비교용 quota
- `interval_unit`, `interval_value`, `custom_seconds`: 정기충전 주기
- `charge_immediately`: 등록 직후 즉시 충전 여부
- `next_charge_time`: 다음 정기충전 시각
- `last_charge_time`: 마지막 충전 시각
- `cooldown_until`: 자동충전 쿨다운 만료 시각
- `daily_charge_count`, `daily_charge_date`: 자동충전 일일 제한 관리
- `status`: `pending`, `active`, `cancelled`, `failed`
- `fail_count`, `last_error`
- `card_company`, `card_number_masked`
- `last_trade_no`
- `customer_key`, `auth_trade_no`

중복 제한:

- `active_key = target_type:target_id:type`
- 하나의 대상은 정기충전 active/pending 1개, 자동충전 active/pending 1개만 가질 수 있다.

## 6. 관리자 옵션 UI 설계

관리자 화면은 DB row를 직접 하나씩 편집하는 UI가 아니다. 관리자가 운영 관점에서 이해하기 쉬운 옵션 형태로 편집하고, 저장 시 프리셋 row 조합으로 변환한다.

메뉴 위치:

- 시스템 설정
- Billing & Payment
- Auto Recharge Presets

### 6.1 정기충전 옵션

관리자가 설정하는 값:

- 대상 범위: 개인, 조직, 전체
- 등록 즉시 충전 여부
- 기간 목록: 일별, 주별, 월별, custom
- 기간별 충전 금액 목록

예:

- 일별: 1000, 2000
- 주별: 5000
- 월별: 1000, 2000, 3000
- custom 86400초: 10000

저장 시 생성되는 프리셋:

- `scheduled:daily:1000`
- `scheduled:daily:2000`
- `scheduled:weekly:5000`
- `scheduled:monthly:1000`
- `scheduled:monthly:2000`
- `scheduled:monthly:3000`
- `scheduled:custom(86400):10000`

같은 기간 안에 여러 금액을 입력할 수 있도록 입력 draft 상태를 유지한다. 사용자가 `1000, 2000, 3000`처럼 쉼표, 공백, 줄바꿈으로 여러 값을 입력해도 저장 계획에서 각각의 프리셋으로 분리된다.

### 6.2 자동충전 옵션

관리자가 설정하는 값:

- 대상 범위: 개인, 조직, 전체
- 충전 금액 목록
- 기준 금액 목록

예:

- 충전 금액: 10000, 30000
- 기준 금액: 1000, 3000, 5000

저장 시 생성되는 프리셋은 충전 금액과 기준 금액의 모든 조합이다.

- 10000 충전, 1000 이하일 때
- 10000 충전, 3000 이하일 때
- 10000 충전, 5000 이하일 때
- 30000 충전, 1000 이하일 때
- 30000 충전, 3000 이하일 때
- 30000 충전, 5000 이하일 때

## 7. 사용자 UI 설계

메뉴 위치:

- 개인 지갑: `Wallet`
- 조직 지갑: `Organization Wallet`

표시 정책:

- 정기충전 프리셋이 있거나 해당 유형의 active/pending 정책이 있으면 정기충전 탭을 표시한다.
- 자동충전 프리셋이 있거나 해당 유형의 active/pending 정책이 있으면 자동충전 탭을 표시한다.
- 프리셋과 정책이 모두 없으면 해당 메뉴 자체를 숨긴다.

### 7.1 정기충전 사용자 흐름

1. 사용자가 `Scheduled recharge` 탭을 연다.
2. 기간 종류가 여러 개이면 기간 버튼을 먼저 선택한다.
3. 기간 종류가 하나뿐이면 기간 선택 단계는 생략된다.
4. 선택된 기간의 충전 금액 버튼을 선택한다.
5. 서버는 선택 프리셋 ID로 pending 정책을 만든다.
6. 프론트는 Toss 빌링키 등록 화면으로 이동한다.
7. Toss 승인 성공 후 정책이 active가 된다.

### 7.2 자동충전 사용자 흐름

1. 사용자가 `Auto recharge` 탭을 연다.
2. 충전 금액 버튼을 먼저 선택한다.
3. 해당 충전 금액에 연결된 기준 금액이 여러 개이면 기준 금액 버튼을 추가로 선택한다.
4. 기준 금액이 하나뿐이면 바로 프리셋을 선택한다.
5. 서버는 pending 정책을 만들고 Toss 빌링키 등록 정보를 반환한다.
6. Toss 승인 성공 후 정책이 active가 된다.

## 8. API 설계

### 8.1 관리자 프리셋 API

- `GET /api/admin/wallet/auto-recharge/presets`
  - 비활성 프리셋 포함 전체 목록
- `POST /api/admin/wallet/auto-recharge/presets`
  - 프리셋 생성
- `PUT /api/admin/wallet/auto-recharge/presets/:id`
  - 프리셋 수정
- `DELETE /api/admin/wallet/auto-recharge/presets/:id`
  - 물리 삭제가 아니라 `enabled=false` 비활성화

### 8.2 사용자 API

- `GET /api/user/wallet/auto-recharge`
  - 개인 지갑 정책 목록
- `GET /api/user/wallet/auto-recharge/presets`
  - 개인 지갑에서 선택 가능한 활성 프리셋
- `POST /api/user/wallet/auto-recharge/scheduled`
  - 정기충전 pending 정책 생성
- `POST /api/user/wallet/auto-recharge/threshold`
  - 자동충전 pending 정책 생성
- `DELETE /api/user/wallet/auto-recharge/pending/:trade_no`
  - Toss 승인 전 pending 정책 취소
- `DELETE /api/user/wallet/auto-recharge/:id`
  - active/pending 정책 취소

요청 본문:

```json
{
  "preset_id": 1
}
```

### 8.3 조직 API

- `GET /api/organization/wallet/auto-recharge`
- `GET /api/organization/wallet/auto-recharge/presets`
- `POST /api/organization/wallet/auto-recharge/scheduled`
- `POST /api/organization/wallet/auto-recharge/threshold`
- `DELETE /api/organization/wallet/auto-recharge/pending/:trade_no`
- `DELETE /api/organization/wallet/auto-recharge/:id`

조직 API는 조직 소유자만 사용할 수 있다. 결제 책임자는 조직 소유자이며, 충전 대상은 조직 지갑이다.

### 8.4 Toss 콜백 API

- `GET /api/wallet/auto-recharge/toss/confirm`
- `GET /api/wallet/auto-recharge/toss/fail`

confirm 필수 파라미터:

- `trade_no`
- `authKey`
- `customerKey`

confirm 성공 시:

- Toss 빌링키 발급
- 빌링키 암호화 저장
- pending 정책 active 전환
- `charge_immediately=true`이면 즉시 첫 충전 실행
- 개인 지갑이면 `/wallet?wallet_auto_recharge=success`
- 조직 지갑이면 `/organization/wallet?wallet_auto_recharge=success`

## 9. 결제 실행 설계

### 9.1 작업 스케줄러

`service.StartWalletAutoRechargeTask()`는 master node에서만 시작한다.

- tick: 1분
- batch size: 100
- max fails: 3

한 tick에서 처리하는 순서:

1. due 상태의 정기충전 정책 조회
2. 각 정기충전 정책 결제
3. active 자동충전 정책 조회
4. 각 자동충전 정책 조건 검사 후 결제

동시 실행 방지:

- `walletAutoRechargeRunning` atomic flag로 한 노드 내 중복 tick 실행을 막는다.

### 9.2 정기충전 실행

실행 조건:

- `type = scheduled`
- `status = active`
- `next_charge_time > 0`
- `next_charge_time <= now`

성공 후:

- 일반 `top_ups` pending row 생성
- Toss billing charge 성공 확인
- `top_ups.status = success`
- 대상 지갑에 quota 적립
- `last_charge_time`, `last_trade_no` 갱신
- `next_charge_time`을 다음 주기로 이동

다음 주기 계산:

- `day`: `AddDate(0, 0, interval_value)`
- `month`: `AddDate(0, interval_value, 0)`
- `custom`: `Add(custom_seconds * time.Second)`

### 9.3 자동충전 실행

실행 조건:

- `type = threshold`
- `status = active`
- 현재 시간이 `cooldown_until` 이후
- 오늘 충전 횟수가 `WalletAutoRechargeDailyLimit` 미만
- 대상 잔액 quota가 `threshold_quota` 이하

제한:

- 충전 성공 후 1시간 쿨다운
- 하루 최대 3회

성공 후:

- 일반 `top_ups` 성공 완료
- 대상 지갑 quota 적립
- `cooldown_until = now + 3600초`
- `daily_charge_count += 1`
- `daily_charge_date` 갱신

## 10. 일반 충전과의 연동

이 기능의 가장 중요한 차이는 결제 성공 후 기존 일반 충전 완료 경로를 사용한다는 점이다.

충전 생성:

- `ensureWalletAutoRechargePendingTopUp`
- `payment_provider = toss`
- `payment_method = toss`
- `status = pending`
- `trade_no = wallet_auto_...`

충전 완료:

- `completeWalletAutoRechargeTopUp`
- `top_ups.status = success`
- `CreditTopUpTarget` 호출

따라서 정기충전/자동충전은 기존 구독처럼 별도 사용권만 갱신하지 않고, 실제 지갑 잔액을 늘린다.

## 11. 장애/중복 처리

### 11.1 active 중복 방지

같은 대상과 같은 정책 유형에는 하나의 active/pending 정책만 허용한다.

예:

- 개인 지갑 정기충전 1개
- 개인 지갑 자동충전 1개
- 조직 지갑 정기충전 1개
- 조직 지갑 자동충전 1개

새 정책을 만들려면 기존 정책을 취소해야 한다.

### 11.2 결제 중복 방지

정기충전 trade no:

- `wallet_auto_{policyId}_{nextChargeTime}`

자동충전 trade no:

- `wallet_auto_{policyId}_{yyyyMMddHH}_{dailyChargeCount+1}`

동일 trade no가 이미 존재하면 기존 `top_ups` 상태를 확인하여 중복 충전을 막는다.

### 11.3 Toss 결제 성공 후 내부 정산 실패

Toss 결제는 성공했지만 내부 `top_ups` 완료 또는 quota 적립에서 실패할 수 있다. 이 경우:

- pending charged top-up을 탐지한다.
- `last_error`에 reconciliation pending 메시지를 저장한다.
- 다음 작업 tick에서 이미 결제된 top-up을 찾아 정산을 재개한다.

### 11.4 실패 누적

빌링키 조회 실패, Toss 결제 실패, 금액 불일치 등은 실패로 기록한다.

- `fail_count += 1`
- `last_error` 저장
- `fail_count >= 3`이면 `status = failed`
- failed가 되면 `active_key`를 비워 새 정책 생성을 허용한다.

## 12. 권한 정책

관리자 프리셋:

- 관리자 권한 필요
- 라우트는 admin route 아래에 있다.

개인 지갑:

- 로그인 사용자 본인만 가능

조직 지갑:

- 조직 소유자만 가능
- actor가 조직에 속해 있어야 함
- actor role이 owner여야 함
- 조직의 `owner_user_id`와 actor id가 일치해야 함

## 13. 프론트엔드 구성

주요 파일:

- `web/default/src/features/wallet/lib/auto-recharge-options.ts`
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/organizations/components/organization-wallet.tsx`

옵션 헬퍼 역할:

- 프리셋을 기간별 그룹으로 묶는다.
- 자동충전 프리셋을 충전 금액별, 기준 금액별로 묶는다.
- 관리자 옵션 상태를 프리셋 생성/수정/비활성화 계획으로 변환한다.
- 같은 기간 안의 복수 금액 입력 draft를 보존한다.

## 14. E2E 문서 캡처

문서용 E2E:

- `web/default/e2e/wallet-auto-recharge-docs.e2e.ts`

검증하는 내용:

- 새 SQLite DB에서 setup 초기화
- root 계정 로그인
- 관리자 API로 샘플 프리셋 생성
- 관리자 프리셋 옵션 화면 캡처
- 개인 지갑 정기충전 화면 캡처
- 개인 지갑 자동충전 화면 캡처
- 자동충전 기준 금액 선택 단계 캡처

실행 예:

```bash
cd web/default
SQLITE_PATH='/tmp/wallet-auto-recharge-doc-e2e.db?_busy_timeout=30000' \
GOCACHE=/tmp/go-cache \
E2E_ADMIN_USERNAME=root \
E2E_ADMIN_PASSWORD=12345678 \
E2E_BASE_URL=http://127.0.0.1:3101 \
E2E_BACKEND_URL=http://127.0.0.1:3100 \
bun run e2e -- wallet-auto-recharge-docs.e2e.ts
```

생성된 스크린샷:

- `docs/manuals/images/wallet-auto-recharge/admin-auto-recharge-options.png`
- `docs/manuals/images/wallet-auto-recharge/admin-auto-recharge-threshold-options.png`
- `docs/manuals/images/wallet-auto-recharge/user-wallet-scheduled-recharge.png`
- `docs/manuals/images/wallet-auto-recharge/user-wallet-auto-recharge.png`
- `docs/manuals/images/wallet-auto-recharge/user-wallet-auto-recharge-threshold.png`

## 15. 테스트 범위

백엔드 테스트:

- 프리셋 생성/수정/비활성화 검증
- 대상 scope 필터링
- 프리셋 선택 정책 생성
- Toss confirm/fail 처리
- 결제 중복 방지
- 자동충전 threshold, 쿨다운, 일일 제한
- 정산 재개
- 라우터 등록

프론트엔드 테스트:

- 프리셋 그룹핑 헬퍼
- 관리자 옵션 상태 변환
- 저장 계획 생성
- 복수 금액 draft 보존
- 사용자 카드 렌더링
- 정기충전/자동충전 선택 흐름

E2E:

- 문서 스크린샷 생성 경로

## 16. 운영 시 주의사항

- Toss 결제 설정과 `ServerAddress`가 올바르지 않으면 사용자가 정책 등록을 시작할 수 없다.
- 프리셋이 없으면 사용자 메뉴가 보이지 않는다.
- 프리셋 비활성화는 기존 active 정책을 중단하지 않는다.
- 사용자의 기존 active 정책을 바꾸려면 사용자가 취소 후 새 프리셋을 다시 선택해야 한다.
- 자동충전 기준 금액은 원(KRW)으로 입력하지만 서버는 현재 환산 규칙 기준 quota로 비교한다.
- 환율/단가 설정이 바뀌면 새 정책의 `threshold_quota`는 새 규칙으로 계산되지만, 기존 정책은 생성 당시 스냅샷을 유지한다.
