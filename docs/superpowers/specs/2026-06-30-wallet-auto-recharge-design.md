# 지갑 자동충전 설계

## 목적

기존 구독 시스템과 분리된 지갑 기반 자동 결제 정책 두 가지를 추가한다.

- **정기결제**: 사용자가 지정한 금액을 정해진 주기마다 결제하고, 일반 지갑 잔액에 충전한다. 기본 사용 사례는 월 1회 충전이다.
- **자동결제**: 지갑 잔액이 사용자가 지정한 기준 이하로 내려가면, 미리 지정한 금액을 결제하고 일반 지갑 잔액에 충전한다.

두 정책은 Toss 빌링만 지원한다. 이 기능은 기존 구독 quota bucket을 생성하거나 수정하지 않는다. 결제가 성공하면 일반 지갑 충전과 동일하게 `TopUp` 성공 기록을 만들고 대상 지갑 quota를 증가시킨다.

## 범위

이번 기능에서 지원하는 것:

- 개인 지갑.
- 조직 지갑.
- 대상별 활성 정기결제 1개와 활성 자동결제 1개.
- 조직 정책은 조직 소유자만 관리 가능.
- Toss 빌링키 기반 결제.
- 기존 지갑 화면 내부 탭 구성: `충전`, `정기결제`, `자동결제`.

이번 기능에서 지원하지 않는 것:

- Toss 외 결제 수단.
- 같은 대상에 같은 유형의 활성 정책 여러 개.
- 기존 구독 과금 의미 변경.
- 관리자용 구독 플랜 카탈로그와의 연동.

## 결정 사항

- 정기결제는 설정 시 즉시 첫 충전을 할지 사용자가 선택할 수 있다.
- 자동결제 기준은 UI에서 금액처럼 입력받지만, 서버는 내부 quota로 환산한 값을 저장하고 비교한다.
- 자동결제에는 충전 후 대기시간과 일일 충전 횟수 제한을 모두 둔다.
- 조직 지갑은 조직 소유자의 Toss 빌링키로 결제한다.
- 기존 구독 테이블과 API는 분리된 상태로 유지한다.

## 데이터 모델

새 GORM 모델과 테이블 `wallet_auto_recharges`를 추가한다.

핵심 필드:

- `id`
- `type`: `scheduled` 또는 `threshold`
- `target_type`: `user` 또는 `organization`
- `target_id`: 개인 지갑이면 user id, 조직 지갑이면 organization id
- `owner_user_id`: Toss 빌링키를 소유한 사용자 id
- `billing_key_id`: 기존 `user_billing_keys` 참조
- `amount`: 자동으로 결제할 금액. 일반 지갑 충전에서 사용자가 입력하는 금액 단위와 동일하게 취급한다.
- `threshold_amount`: 자동결제 기준 금액. 사용자에게 표시되는 금액 단위다.
- `threshold_quota`: 잔액 비교에 사용하는 서버 내부 quota 값
- `interval_unit`: `month`, `day`, 또는 `custom`
- `interval_value`
- `custom_seconds`
- `charge_immediately`
- `next_charge_time`
- `last_charge_time`
- `cooldown_until`
- `daily_charge_count`
- `daily_charge_date`
- `status`: `pending`, `active`, `cancelled`, 또는 `failed`
- `fail_count`
- `last_error`
- `card_company`
- `card_number_masked`
- `last_trade_no`
- `created_at`
- `updated_at`

테이블 생성은 GORM AutoMigrate를 사용한다. raw SQL은 사용하지 않는다. 활성 정책 1개 제한은 DB별 부분 인덱스 차이를 피하기 위해 애플리케이션 트랜잭션에서 보장한다. 이 방식은 SQLite, MySQL, PostgreSQL에서 동일하게 동작한다.

## 지갑 대상

개인 지갑:

- `target_type = user`
- `target_id = current user id`
- `owner_user_id = current user id`

조직 지갑:

- `target_type = organization`
- `target_id = organization id`
- `owner_user_id = organization.owner_user_id`
- 조직 소유자만 정책을 생성, 수정, 해지할 수 있다.
- 성공한 충전은 `target_type = organization`, `target_id = organization id`인 `TopUp` 기록을 만든다.

## Toss 빌링 흐름

정책 설정은 기존 Toss 구독 자동결제와 같은 Toss billing authorization 패턴을 재사용한다.

1. 사용자가 정기결제 또는 자동결제 설정값을 제출한다.
2. 백엔드는 Toss 빌링 사용 가능 여부, 대상 접근 권한, 금액 제한, 활성 정책 중복 여부를 검증한다.
3. 백엔드는 `wallet_auto_recharges`에 `pending` 상태 정책을 생성한다.
4. 백엔드는 Toss 빌링 인증에 필요한 `client_key`, `customer_key`, `trade_no`, `success_url`, `fail_url`을 반환한다.
5. 프론트엔드는 Toss billing auth 창을 연다.
6. Toss는 성공 콜백으로 `authKey`와 `customerKey`를 전달한다.
7. 백엔드는 pending 정책과 customer key를 검증한다.
8. 백엔드는 `authKey`를 `billingKey`로 교환한다.
9. 백엔드는 빌링키를 `user_billing_keys`에 저장한다.
10. 백엔드는 정책을 `active`로 전환하고 마스킹된 카드 정보를 저장한다.
11. 정기결제 정책이고 `charge_immediately = true`이면 백엔드는 즉시 첫 지갑 충전을 시도한다.

confirm/fail 콜백은 기존 구독 콜백과 별도로 둔다. 콜백 경로는 지갑 자동충전 전용 라우트에 둔다.

## 결제 실행 흐름

master node에서 1분마다 실행되는 service task를 추가한다.

정기결제 정책:

1. `next_charge_time <= now`인 활성 scheduled 정책을 조회한다.
2. 결제 전에 정책 row를 lock한다.
3. deterministic trade number와 Toss idempotency key로 Toss billing charge를 수행한다.
4. 성공하면 `TopUp` 성공 row를 생성하고 대상 quota를 증가시킨다.
5. 임의의 job 지연 시간이 아니라 의도된 스케줄 기준으로 `next_charge_time`을 다음 주기로 이동한다.
6. `fail_count`를 초기화한다.
7. 실패하면 `fail_count`를 증가시키고, 최대 실패 횟수 이상이면 정책을 `failed`로 전환한다.

자동결제 정책:

1. 활성 threshold 정책을 조회한다.
2. `cooldown_until > now`이면 건너뛴다.
3. 일일 제한 횟수에 도달했으면 건너뛴다.
4. 대상 지갑 quota를 읽는다.
5. `quota <= threshold_quota`일 때만 결제한다.
6. 성공하면 `TopUp` 성공 row를 생성하고 대상 quota를 증가시킨다.
7. `cooldown_until = now + 1 hour`로 설정한다.
8. 현재 날짜의 일일 충전 횟수를 증가시킨다.
9. 실패하면 `fail_count`를 증가시키고, 최대 실패 횟수 이상이면 정책을 `failed`로 전환한다.

기본 안전장치 값:

- 자동결제 cooldown: 1시간.
- 자동결제 일일 충전 제한: 정책당 성공한 자동충전 3회.
- 최대 실패 횟수: 3회.

## TopUp 연동

성공한 자동 결제는 기존 지갑 충전 경로를 사용한다.

- 개인 지갑: `users.quota` 증가.
- 조직 지갑: `organizations.quota` 증가.
- 생성되는 `TopUp` 값:
  - `payment_method = toss`
  - `payment_provider = toss`
  - `status = success`
  - `money = amount`
  - `amount = charged KRW`
  - 조직 대상이면 `target_type`, `target_id` 설정
  - `provider_order_id` 또는 provider payload에 Toss charge 메타데이터 저장

이렇게 하면 자동충전도 기존 지갑 충전 내역에서 자연스럽게 보인다.

## API 설계

개인 지갑:

- `GET /api/wallet/auto-recharge`
- `POST /api/wallet/auto-recharge/scheduled`
- `POST /api/wallet/auto-recharge/threshold`
- `DELETE /api/wallet/auto-recharge/:id`

조직 지갑:

- `GET /api/organization/wallet/auto-recharge`
- `POST /api/organization/wallet/auto-recharge/scheduled`
- `POST /api/organization/wallet/auto-recharge/threshold`
- `DELETE /api/organization/wallet/auto-recharge/:id`

콜백:

- `GET /api/wallet/auto-recharge/toss/confirm`
- `GET /api/wallet/auto-recharge/toss/fail`

콜백은 trade number로 pending 정책을 찾고, DB에 기록된 정책 대상을 적용한다. 개인 지갑과 조직 지갑 설정 흐름은 같은 callback endpoint를 사용한다. 조직 처리는 `target_type = organization`과 `target_id`에서 판단한다.

## 프론트엔드 설계

개인 지갑 화면:

- 기존 지갑 페이지 내부에 탭을 추가한다.
  - `충전`
  - `정기결제`
  - `자동결제`
- 기존 일반 충전 UI는 `충전` 탭에 유지한다.
- `정기결제` 탭에는 금액, 주기, 즉시 충전 토글, 카드/상태, 다음 결제 예정일, 해지 액션을 보여준다.
- `자동결제` 탭에는 기준 잔액, 충전 금액, cooldown/일일 제한 안내, 카드/상태, 최근 충전일, 해지 액션을 보여준다.

조직 지갑 화면:

- 같은 탭 구성을 추가한다.
- 조직 소유자가 아닌 사용자는 정책 상태를 볼 수 있지만 생성/해지할 수 없다.
- 조직 소유자는 설정과 해지 액션을 사용할 수 있다.

이 기능에서는 "구독"이라는 단어를 피한다. 다음 지갑 중심 표현을 사용한다.

- 정기결제: "정해진 주기마다 지갑에 자동 충전"
- 자동결제: "잔액이 기준 이하일 때 지갑에 자동 충전"

## 검증 규칙

공통 검증:

- Toss 빌링이 활성화되어 있어야 한다.
- 결제 컴플라이언스가 확인되어 있어야 한다.
- 금액은 `TossMinTopUp` 이상이어야 한다.
- 같은 대상에 같은 유형의 활성 정책이 이미 있으면 안 된다.
- 오래된 pending 정책은 만료하거나 새 설정 시 덮어쓸 수 있다.

정기결제 검증:

- 지원하는 interval unit만 허용한다.
- custom seconds를 쓰지 않는 경우 interval value는 양수여야 한다.
- custom seconds는 양수여야 한다.

자동결제 검증:

- 기준 금액은 음수일 수 없다.
- `threshold_quota`는 서버에서 현재 quota 환산 규칙으로 계산한다.
- 충전 금액은 양수이고 `TossMinTopUp` 이상이어야 한다.

조직 검증:

- 현재 사용자가 조직 소유자여야 한다.
- 빌링 인증에는 조직 소유자의 Toss customer key를 사용한다.

## 오류 처리

Toss 빌링 인증 실패:

- pending 정책을 `cancelled` 또는 `failed`로 표시한다.
- 사용자를 지갑 페이지로 되돌린다.

Toss charge 실패:

- `fail_count`를 증가시킨다.
- `last_error`에 짧은 오류 메시지를 저장한다.
- 최대 실패 횟수 이후 정책을 `failed`로 표시한다.

결제는 성공했지만 로컬 충전 반영에 실패한 경우:

- trade number, 사용자/조직, 금액, 정책 id를 포함한 수동 정산 필요 로그를 명확히 남긴다.
- 새 trade number로 조용히 재시도하지 않는다.

중복 job 실행:

- 정책 row를 transaction 안에서 lock한다.
- Toss idempotency key는 정책 id와 scheduled/trigger cycle을 기반으로 만든다.
- TopUp trade number는 unique해야 한다.

## 테스트

백엔드 테스트:

- 개인 정기결제 정책에서 즉시 충전을 선택하면 `TopUp` 성공 기록이 생성되고 `users.quota`가 증가한다.
- 개인 정기결제 정책에서 즉시 충전을 선택하지 않으면 quota 증가 없이 정책만 active가 된다.
- 정기결제 job은 due 정책을 결제하고 `next_charge_time`을 다음 주기로 이동한다.
- 자동결제 job은 잔액이 기준보다 높으면 충전하지 않는다.
- 자동결제 job은 잔액이 기준 이하이면 충전한다.
- 자동결제 cooldown 중에는 반복 충전하지 않는다.
- 자동결제 일일 제한을 넘으면 추가 충전하지 않는다.
- 조직 소유자는 조직 지갑 정책을 생성할 수 있다.
- 조직 소유자가 아닌 사용자는 조직 지갑 정책을 생성하거나 해지할 수 없다.
- 조직 충전은 `organizations.quota`를 증가시키고 `TopUp target_type=organization` 기록을 생성한다.
- Toss 실패는 `fail_count`를 증가시키고, 반복 실패 시 정책을 failed로 만든다.
- 기존 구독 구매와 Toss 구독 자동갱신 테스트는 계속 통과해야 한다.

프론트엔드 테스트:

- 지갑 페이지가 `충전`, `정기결제`, `자동결제` 탭을 렌더링한다.
- 정기결제 폼은 금액과 주기를 검증한다.
- 자동결제 폼은 기준 금액과 충전 금액을 검증한다.
- 조직 소유자가 아닌 사용자는 설정/해지 액션을 제출할 수 없다.
- 기존 지갑 충전 흐름은 `충전` 탭에서 계속 접근 가능하다.

## 배포 메모

- 기존 사용자와 구독 데이터에는 별도 마이그레이션이 필요 없다.
- 새 테이블은 additive 변경이다.
- 기존 Toss 빌링 설정을 재사용한다.
- 기존 Toss 구독 자동갱신은 건드리지 않는다.
- Toss 빌링이 설정되지 않은 환경에서는 이 기능이 자연스럽게 비활성화된다.
