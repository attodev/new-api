# Toss Payments 하드닝 커밋별 문서 인덱스

- 작성일: 2026-07-12
- 반영 브랜치: toss
- 통합 기준 HEAD: 4171f875b
- 대상: 기준 HEAD 이후 Toss Payments 공식 문서 재감사·수정 결과

## 1. 가장 먼저 알아야 할 점

이번 반복 감사의 변경은 처음에는 HEAD 4171f875b 이후 하나의 큰 미커밋 작업 트리였다. 최종 검증 뒤 `toss` 브랜치에는 다음과 같이 컴파일·빌드 가능한 큰 경계로 반영했다.

| 실제 커밋 | 반영 범위 |
|---|---|
| `474e12e70` | backend·runtime·운영 121개 파일: C01–C15, backend C18a–C18c |
| `1e596af12` | Default frontend 56개 파일: C16a–C16e와 Default locale |
| `34a33c78f` | Classic frontend 25개 파일: C17과 Classic locale |
| 문서 커밋 | 원래 감사 보고서와 이번 commit index·상세·manifest·history 문서 |

따라서 이 문서 묶음에서 C01부터 C18은 실제 commit hash와 일대일 대응하지 않는 **논리 리뷰 단위**다. 변경량이 큰 C16과 C18은 각각 다섯 개와 네 개의 권장 하위 단위로 나누며, 세밀한 재구성·리뷰 기준은 총 25개다. 실제 반영은 대형 shared file의 hunk 의존성과 중간 커밋 컴파일 위험을 피하기 위해 위 통합 커밋 경계를 사용했다.

- 왜 변경했는지 독립적으로 설명할 수 있어야 한다.
- 가능한 한 한 가지 안전성 목표를 가져야 한다.
- 테스트가 구현과 같은 커밋에 들어가야 한다.
- 향후 세밀한 cherry-pick이나 재구성 시 review와 rollback 경계를 제공해야 한다.
- 서로 의존하는 변경은 안전한 적용 순서를 따라야 한다.

이미 2026-06-29부터 2026-07-06 사이에 Git에 들어간 Toss 초기 도입, billing, wallet auto-recharge, quote UI 관련 커밋은 이번 하드닝의 baseline이다. 그 이력은 [기존 실제 커밋 부록](2026-07-12-toss-hardening-historical-commits.md)에 별도로 정리한다.

## 2. 문서 구성

| 문서 | 내용 |
|---|---|
| 현재 문서 | 범위, 커밋 순서, 의존성, 공통 검증 |
| [C01–C06 상세](2026-07-12-toss-hardening-commits-01-06.md) | HTTP·설정·MID·일반 결제·정산 lifecycle |
| [C07–C12 상세](2026-07-12-toss-hardening-commits-07-12.md) | 환불·웹훅·Transaction 대사·구독·billing key |
| [C13–C18 상세](2026-07-12-toss-hardening-commits-13-18.md) | renewal protocol·wallet·scheduler·두 프론트엔드·운영 |
| [변경 파일 완전 매핑](2026-07-12-toss-hardening-file-manifest.md) | Toss 범위 203개 파일을 primary logical commit에 매핑 |
| [기존 실제 커밋 부록](2026-07-12-toss-hardening-historical-commits.md) | 이미 HEAD에 포함된 Toss baseline commit history |
| [공식 문서 기준 최종 감사 보고서](2026-07-12-toss-payments-official-docs-audit-and-hardening.md) | 요구사항별 통합 설명, 배포 체크리스트, runbook |

## 3. 논리 커밋 목록

| ID | 권장 커밋 제목 | 해결하는 핵심 문제 |
|---|---|---|
| C01 | fix(toss): harden provider transport and request boundaries | TLS·redirect·timeout·body·오류 응답 경계가 결제 안전성을 약화 |
| C02 | fix(toss): make payment configuration atomic and encrypted | 부분 설정, plaintext secret, 손상 generation, unsafe repair |
| C03 | fix(toss): pin MID namespace across credential rotation | API-key scoped 멱등성을 키 교체가 깨뜨릴 수 있음 |
| C04 | fix(toss): persist immutable top-up quote and checkout identity | 현재 단가 재계산, callback 값 신뢰, 금액·quota 혼동 |
| C05 | fix(toss): scope confirm idempotency to the payment key | forged paymentKey 선점, ambiguous POST 뒤 중복 승인 |
| C06 | fix(toss): serialize settlement with account and organization lifecycle | callback·worker·삭제·소유권 변경과 정산 경쟁 |
| C07 | fix(toss): fence refunds and persist cancellation evidence | 취소 후 지급, 부분취소 잔액 방치, 중복 refund POST |
| C08 | fix(toss): authenticate and bound webhook processing | unsigned 일반 webhook, spoofed XFF, 10초 응답 실패 |
| C09 | feat(toss): reconcile missed payments from the Transaction API | callback·웹훅 최종 유실과 cursor 건너뛰기 |
| C10 | fix(toss): snapshot subscription terms and reserve purchase capacity | mutable plan 소급, 동시 구매 제한 초과 |
| C11 | fix(toss): recover billing issue and initial charge safely | 일회성 authKey·ISSUE 응답·첫 charge 응답 유실 |
| C12 | fix(toss): harden billing-key revocation and subscription cancellation | 공유 key 조기 삭제, BILLING_DELETED 오식별, 취소 race |
| C13 | fix(toss): make renewal identities safe across rolling upgrades | 같은 주기 alternate orderId, 오래된 worker, mutable renewal |
| C14 | fix(toss): harden wallet auto-recharge lifecycle | 비구독 계약 경계, preset TOCTOU, 중복·오래된 자동청구 |
| C15 | fix(toss): bound payment schedulers and use database time | clock skew, queue starvation, worker panic, 장기 장애 소급청구 |
| C16a | feat(web-default): add safe Toss browser session primitives | 중복 SDK 실행, stale quote, callback·iframe lifecycle |
| C16b | feat(web-default): bind Toss subscriptions to reviewed immutable terms | 표시한 plan과 server order 불일치, pending reservation 고아 |
| C16c | feat(web-default): pin wallet auto-recharge preset terms | preset TOCTOU, malformed session, immediate charge 오표시 |
| C16d | feat(web-default): integrate safe Toss flows into personal and organization wallets | target route·owner 권한·callback 처리 불일치 |
| C16e | feat(web-default-admin): add atomic Toss repair and maintenance UI | 부분 option 저장, 손상 설정 repair UI 부재 |
| C17 | feat(web-classic): mirror Toss billing and auto-recharge safety | Classic UI의 기능·안전 경계 누락 |
| C18a | fix(toss): redact payment identifiers and database bind values | secret·payment identifier 로그 노출 |
| C18b | i18n(toss): localize checkout, repair and auto-recharge operations | backend·두 frontend locale 누락 |
| C18c | docs(ops): document secrets, rolling migration and webhook policy | encryption·protocol·network rollout 실수 |
| C18d | docs(toss): add official-reference audit and commit handoff | 감사·배포·장애 대응 근거 부재 |

## 4. 의존성 및 권장 적용 순서

~~~mermaid
flowchart TD
    C01["C01 HTTP·입력 경계"] --> C05["C05 confirm 멱등·복구"]
    C01 --> C08["C08 webhook"]
    C01 --> C11["C11 billing ISSUE·첫 charge"]
    C01 --> C14["C14 wallet auto-recharge"]

    C02["C02 atomic config·암호화"] --> C03["C03 MID·credential pinning"]
    C02 --> C05
    C02 --> C11
    C02 --> C13["C13 renewal protocol"]
    C02 --> C14

    C03 --> C05
    C03 --> C11
    C03 --> C13
    C03 --> C14

    C04["C04 quote·checkout snapshot"] --> C05
    C05 --> C06["C06 settlement lifecycle"]
    C06 --> C07["C07 cancellation·refund"]
    C07 --> C08
    C07 --> C09["C09 Transaction reconciliation"]

    C10["C10 subscription snapshot·reservation"] --> C11
    C11 --> C12["C12 key revocation·cancel"]
    C11 --> C13
    C12 --> C13
    C11 --> C14

    C05 --> C15["C15 scheduler·DB clock"]
    C09 --> C15
    C13 --> C15
    C14 --> C15

    C04 --> C16["C16a–C16e Default frontend"]
    C11 --> C16
    C14 --> C16
    C16 --> C17["C17 Classic frontend"]

    C08 --> C18["C18a–C18d 운영·로그·i18n·문서"]
    C15 --> C18
    C16 --> C18
    C17 --> C18
~~~

가장 안전한 실제 커밋 순서는 C01부터 C15, C16a부터 C16e, C17, C18a부터 C18d 순서다. 다만 다음 주의가 필요하다.

- C02와 C03은 같은 대형 파일을 많이 수정한다. 실제 분리 시 model/option.go와 model/toss_billing.go는 hunk 단위 staging이 필요하다.
- C04부터 C09까지 controller/topup_toss.go와 model/topup.go를 공유한다. 각 커밋이 독립 compile되도록 helper 이동 순서를 조정해야 한다.
- C10부터 C14까지 model/subscription.go, model/toss_billing.go, model/wallet_auto_recharge.go를 공유한다. 중간 커밋이 깨지지 않도록 새 model과 migration helper부터 추가해야 한다.
- C16a–C16e와 C17은 대응 backend contract가 먼저 들어간 뒤 적용해야 한다.
- C18a–C18d의 환경변수 rollout 주석은 C02·C03·C13과 함께 review하되 문서 정리는 마지막에 두는 편이 좋다.

## 5. 커밋 상세 문서의 공통 형식

각 논리 커밋은 다음 항목을 포함한다.

1. 권장 subject
2. 선행 커밋
3. 발견한 문제
4. 문제가 발생하는 구체적인 시나리오
5. 원인
6. 적용한 수정
7. 수정 뒤의 불변조건
8. 관련 구현 파일
9. 관련 테스트
10. 남아 있는 위험과 rollback 주의점

“관련 파일”은 대표 파일이고, 모든 변경 파일의 primary ownership은 [변경 파일 완전 매핑](2026-07-12-toss-hardening-file-manifest.md)을 기준으로 한다. 대형 공용 파일은 하나의 primary commit에 배정하고 다른 커밋의 secondary touch를 별도로 기록한다.

## 6. 커밋 분리 원칙

### 6.1 테스트는 구현과 함께

다음처럼 테스트만 마지막에 몰아넣지 않는다.

- confirm idempotency 테스트는 C05
- refund operation 테스트는 C07
- webhook IP·deadline·rate-limit 테스트는 C08
- Transaction cursor·lease 테스트는 C09
- renewal migration·opaque ID 테스트는 C13
- wallet ISSUE·charge race 테스트는 C14
- scheduler panic·bounded batch 테스트는 C15
- Default UI lifecycle 테스트는 C16a–C16e, Classic 테스트는 C17

### 6.2 migration과 runtime reader의 순서

DB column 또는 protocol marker를 추가하는 커밋은 다음 순서를 지켜야 한다.

1. 새 형식을 읽되 기존 형식도 안전하게 처리하는 dual-reader
2. 새 필드 migration
3. 새 writer
4. bounded legacy backfill
5. explicit activation fence
6. 구버전 writer drain

이 순서를 뒤집으면 mixed-version 노드가 새 ciphertext·orderId·attempt state를 잘못 해석할 수 있다.

### 6.3 provider POST와 local state를 같은 커밋에서

provider POST 직전의 durable attempt marker, exact credential, idempotency key, final operational gate는 분리하면 안 된다. 한쪽만 먼저 배포하면 crash recovery가 어떤 namespace를 재생해야 하는지 증명할 수 없다.

### 6.4 안전 정지 정책을 유지

다음 상태는 자동 성공·실패로 바꾸지 않는다.

- timeout, 409, 429, 5xx
- 2xx body read·decode 실패
- 알 수 없는 새 provider 오류 코드
- 복호화 불가 credential
- MID fingerprint가 비어 있거나 잘못됨
- 15일 멱등 보장 이후 unresolved POST
- 입금된 가상계좌인데 refundReceiveAccount가 없음
- 지급 후 cancellation·partial cancellation

## 7. 공통 검증 결과

최종 커밋 직전 합쳐진 작업 트리에서 다음 핵심 검증이 통과했다.

~~~text
go test -count=1 . ./controller ./model ./service ./middleware ./router ./setting ./i18n
go test -race -count=1 ./controller ./model ./service ./middleware ./router ./setting
go vet . ./controller ./model ./service ./middleware ./router ./setting ./i18n
~~~

동시성 핵심 경계는 race detector로 별도 확인했다.

- 일반 충전 동시 정산 1회 지급
- 구독 charge claim 단일 owner
- wallet auto-recharge 동시 정산 1회 지급
- concurrent refund operation이 동일 key 사용
- maintenance 중 subscription plan 변경 차단

프론트엔드:

- Default 전체 테스트 132건 통과(23개 파일)
- Classic Toss 집중 테스트 18건 통과
- Default production build 통과
- Classic production build 통과

정적·형식:

- 핵심 Go 범위 go vet 통과
- 변경 Go 파일 gofmt 확인
- 변경 프론트 파일 Prettier 확인
- Classic 변경 파일 ESLint 확인
- git diff --check 통과
- Default i18n sync 완료, missing·extra key 0

전체 `go test ./...`는 변경되지 않은 `relay/channel/claude`의 기존 expectation 3건과 `relay/helper`의 ticker/global-state 테스트 1건이 실패하므로 전체 green으로 표현하지 않는다. Default `build:check`의 TypeScript 단계와 Default ESLint, Classic 전체 i18n lint 역시 기존 저장소 설정·전역 이슈로 실패했으며 상세 내용은 [최종 감사 보고서의 검증 결과](2026-07-12-toss-payments-official-docs-audit-and-hardening.md#11-검증-결과)를 따른다.

## 8. 작업 트리 범위

원래 코드·프론트엔드·운영·감사 보고서 범위는 203개이며, 이번 커밋 문서 6개를 더한 최종 Toss 산출물은 209개다. 다음 54개 경로는 작업 트리에 존재하지만 Toss 커밋에서 제외했다.

- .aionrs 내부 세션 파일
- .understand-anything 내부의 기존 overlay
- outputs 아래의 조직 관리 프레젠테이션 산출물
- docs/images/organization-manual 아래 이미지
- 2026-06-03, 2026-06-04, 2026-06-07 조직 관련 계획 문서

이 제외 항목은 사용자 또는 다른 작업의 변경으로 보고 수정·삭제·커밋 대상으로 취급하지 않는다.

## 9. 실제 커밋을 만들 때의 체크리스트

각 Cxx를 실제로 커밋하기 전에:

- [ ] 해당 커밋의 primary file과 필요한 secondary hunk만 stage했다.
- [ ] 다른 Cxx의 migration 또는 writer가 우발적으로 섞이지 않았다.
- [ ] 새 테스트가 구현과 같은 commit에 stage됐다.
- [ ] 중간 commit 단독으로 Go compile과 관련 테스트가 통과한다.
- [ ] frontend commit은 대응 backend contract보다 앞서지 않는다.
- [ ] secret, paymentKey, billingKey, authKey가 diff·fixture·로그에 실값으로 들어가지 않았다.
- [ ] SQLite, MySQL, PostgreSQL 호환성을 깨는 raw SQL이 없는지 확인했다.
- [ ] git diff --cached --check가 통과한다.
- [ ] commit message body에 문제와 안전성 불변조건을 적었다.

## 10. 최종 범위 판단

이 문서의 C01–C18은 “코드를 예쁘게 나눈 기능 단위”가 아니라 “금전 상태를 안전하게 review하고 rollback할 수 있는 경계”다. 일부 파일이 여러 커밋에 걸쳐 있어 실제 분리 작업은 단순 파일 단위 staging으로 끝나지 않는다.

특히 model/toss_billing.go, model/wallet_auto_recharge.go, controller/topup_toss.go, model/subscription.go, model/topup.go는 여러 안전성 목표가 한 파일에 교차한다. 실제 커밋을 만들기 전에는 반드시 hunk별 diff를 다시 확인해야 한다.
