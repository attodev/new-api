# Upstream 동기화 2단계 기록 — 지갑 수용량 / pre-consume / tokenkit 포팅 (2026-10-13)

작성일: 2026-10-13

이 문서는 `codex/upstream-sync-2026-10` 브랜치에서 수행한 upstream(QuantumNous)
동기화 2단계(plan `docs/superpowers/plans/2026-10-13-upstream-sync-phase1-2.md` 의
Task 20~24)의 포팅 내역, 운영상 바뀌는 동작, 검증 결과, 보류 항목을 기록한다.
1단계(Task 1~18, protocol / 과금 포팅)는 `docs/2026-10-13-upstream-sync-phase1.md`
에 있고, 검토 근거와 후보 목록은 `docs/2026-10-13-upstream-protocol-billing-review.md`
를 따른다. 2단계 기록의 운영 전제는 1단계 기록의 운영 전제에 더해서 읽는다.

## 검토 범위

- 대상: 충전(top-up) 전 지갑 수용량 검증, pre-consume 정책(입력 배수와 신뢰 임계값),
  32-bit 정수 폐기(upstream `a073f74b3`)와 fork 의 bigint quota ceiling 의 대조,
  그리고 token 계산을 `tokenkit` 모듈로 옮기는 변경.
- 방식: 1단계와 같이 전체 merge 가 아니라 항목별 선별 포팅. upstream 의 설계가
  fork 의 기존 결정과 충돌하는 곳(아래 "운영 전제" 의 판단 기록)은 fork 의
  quota 문서(`docs/quota-ceiling-and-bigint-migration.md`) 의도를 따랐다.
- 범위: 커밋 범위 `d2bb1b156..499784e79` (6개 커밋, Task 20~23). Task 24 는 이 기록과
  전체 검증이다. 각 커밋 메시지의 `Upstream:` trailer 에 원본 커밋 링크가 있다.
- 1단계 기록과 합쳐 브랜치는 분기점 team `5167b251d` 에서 시작한다.

## 병합된 커밋

fork 커밋은 `git log --reverse d2bb1b156..499784e79` 순서이며, upstream 해시는
`https://github.com/QuantumNous/new-api/commit/<sha>` 이다.

| Task | fork 커밋 | upstream | 동작 요약 |
| --- | --- | --- | --- |
| 20 | `1fa9b3f1b` | `47ba9d2c6`, `2a0ce3475` | 지급 불가능한 충전 주문을 결제 전에 거부하고 지갑 수용량을 사전 검증. upstream 의 epay/Stripe/Creem/Waffo 에 더해 fork 의 PayPal, Toss, 조직 지갑, 자동 충전 진입점까지 적용 |
| 21 | `cf68ad6d6` | `9978ee1e2` | `quota_setting.pre_consume_multiplier`, `quota_setting.trust_quota_usd` 옵션. 입력 토큰 × 배수만 예약 |
| 22 | `3f1cbeeb7` | `a073f74b3` | saturating 변환과 경계를 bigint quota ceiling 에 정렬. wallet 도메인 helper, `ensureUserQuotaColumns` 시작 검사, 충전성 credit 의 ceiling 보장 |
| 22 | `2ba5cc513` | `a073f74b3` | 리뷰 반영: CSV import, check-in credit 을 지갑 ceiling 으로 제한, `GrantUserQuota` 를 DB 먼저 쓰도록 변경, 메시지 assertion 정리 |
| 23 | `c0b313844` | `bb28f2cca` | token 계산을 `tokenkit` 모듈로 이동, 에이전트 트래픽으로 보정한 추정, tool 정의/tool_result/function call 계산 |
| 23 | `499784e79` | `bb28f2cca` | 리뷰 반영: Gemini 가 보고한 prompt usage 를 completion 이 0 일 때도 유지, CI Go 버전 `>=1.26` |
| 최종 리뷰 | `615d39484` | `55a6cd2a4` | 브랜치 전체 리뷰 반영: Responses 스트림이 `response.incomplete`/`done`/`failed`/`cancelled` 이벤트의 usage 를 정산에 사용(기존엔 `completed` 만), Gemini 스트림 usage 게이트를 `geminiUsageMetadataHasTokens` 로 통일, `anthropic-beta` 값 trim, `expr.md`/`price.go` 의 tiered 예약 문구 수정, `THIRD-PARTY-LICENSES.md` tokenizer v0.8.1, `text_quota_test.go` gofmt |

Task 22 와 Task 23 은 각각 수정 커밋이 하나씩 더 있어 fork 커밋이 두 개다. 마지막 행은
브랜치 전체 리뷰(Task 24 이후) 에서 나온 수정 묶음이다. 모든 커밋의
`Co-Authored-By` 는 `Claude Fable 5.1 <noreply@anthropic.com>` 로 통일했다.

최종 리뷰에서 남긴 후속 후보: Chat 요청의 `reasoning_effort` 가 Opus 4.7/4.8 에서
`thinking.type:"enabled"` 로 매핑되어 400 이 나는 문제(`relay-claude.go:273-290`, 브랜치 이전부터 존재),
`service/task_polling.go` 의 201/202 거부, Gemini prompt IMAGE modality 누락, `EpayNotify` 의
success 선기록 순서 (네 항목 모두 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`)). 릴리스 노트에 적을 것: classic UI 관리자는 새 옵션 두 개를 설정할 수 없다,
master 노드를 먼저 배포한다(비 master 는 widen 되지 않은 스키마를 거부), Claude/Gemini 스트림의
token 추정 시 이미지 URL fetch 가 발생할 수 있다.

### Task 20: 결제 진입점별 사전 검증 범위

각 행의 "credited quota" 는 해당 provider 의 settlement 가 쓰는 변환과 같은 식으로
계산한다. 모든 guard 는 주문 row 를 만들기 전, provider 를 호출하기 전에 돈다.

| Route | Handler | credited quota (settlement 와 동일 변환) | guard |
| --- | --- | --- | --- |
| POST /api/user/pay | RequestEpay | `getTopUpQuota(req.Amount)` (tokens 모드는 /QPU 를 truncate 한 뒤 ×QPU) | 있음 |
| POST /api/user/amount | RequestAmount | 같음 | 있음 |
| POST /api/user/stripe/pay | RequestStripePay | `chargedMoney × QPU` (`model.Recharge` 와 동일) | 있음 |
| POST /api/user/stripe/amount | RequestStripeAmount | `getStripeCreditedQuota(amount, group)` | 있음 |
| POST /api/user/paypal/pay | RequestPayPalPay | `getPayPalCreditedQuota(getPayPalPayMoney(...))` | 있음 |
| POST /api/user/paypal/amount | RequestPayPalAmount | 같음 | 있음 |
| POST /api/user/toss/pay | RequestTossPay | `quote.CreditQuota` (krw/quota 모드 모두, `TopUp.Quota` 에 스냅샷) | 있음 |
| POST /api/user/toss/amount | RequestTossAmount | 같음 | 있음 |
| POST /api/user/creem/pay | RequestCreemPay | `selectedProduct.Quota` | 있음 |
| POST /api/user/waffo/pay, /amount | RequestWaffoPay / Amount | `normalizeWaffoTopUpAmount(req.Amount) × QPU` | 있음 |
| POST /api/user/waffo-pancake/pay, /amount | RequestWaffoPancakePay / Amount | `normalizeWaffoPancakeTopUpAmount × QPU` | 있음 |
| POST /api/organization/{pay,amount,stripe/\*,paypal/\*,toss/\*,creem/pay,waffo/\*,waffo-pancake/\*} | RequestOrganization\* | 위 사용자 핸들러에 위임, 조직 지갑(`organizations.quota`) 기준 | 있음 |
| POST /api/user/wallet/auto-recharge/{scheduled,threshold} | RequestWallet\*Recharge | `WalletAutoRechargeCreditQuota(preset.Amount)` | 있음 |
| POST /api/organization/wallet/auto-recharge/{scheduled,threshold} | RequestOrganizationWallet\* | 같음, 조직 지갑 기준 | 있음 |
| POST /api/user/topup | TopUp (상환 코드) | 결제 아님 | 범위 밖 (Task 22 가 별도로 ceiling 적용) |
| /api/subscription/\*/pay | Subscription\* | quota 가 아니라 구독 구매 | 범위 밖 |
| webhook, capture, confirm, notify | - | settlement 쪽, 이미 atomic | 해당 없음 |

- 조직 대상은 `setTopUpTarget` 으로 판별하고, 조직 지갑이 가득 찬 경우만 조직 충전을
  막는다(소유자 지갑이 가득 차도 조직 충전은 막지 않는다. 테스트로 양방향 확인).
- 자동 충전: threshold 정책은 `ThresholdQuota + 1회 충전액 > MaxQuota` 일 때
  등록을 거부하고 현재 잔액은 보지 않는다. scheduled 정책은 잔액과 무관하게 충전하므로
  다음 충전액이 현재 잔액에 들어가야 한다. 이후의 성장은 settlement guard 가 잡는다.
- settlement 쪽은 upstream 의 `creditTopUpQuota` 를 포팅하지 않았다.
  `model.CreditTopUpTarget` 의 조건부 `UPDATE ... WHERE id = ? AND quota <= ?` 가 이미
  atomic 하게 상한을 지키기 때문이다. 사전 검사와 settlement 가 같은 한도를 쓰도록
  `topUpQuotaMaxCurrent` 를 공유한다. 상한은 포함(inclusive)이다: 최종 잔액이
  `MaxQuota` 와 같은 것은 허용한다.

### Task 21: pre-consume 호출 위치

| 호출 위치 | 예약량 | 사용하는 helper |
| --- | --- | --- |
| `relay/helper/price.go` `ModelPriceHelper` 비율 경로 | 입력 토큰 × multiplier × modelRatio × groupRatio | `operation_setting.InputPreConsumeMultiplier()` |
| `relay/helper/price.go` `modelPriceHelperTiered` | expr(P=Len=prompt, C=0) × multiplier × groupRatio. snapshot 에 기록 | `operation_setting.InputPreConsumeMultiplier()` |
| `ModelPriceHelper` `usePrice` 경로 | ModelPrice × groupRatio, multiplier 없음 | 없음 (불변) |
| `ModelPriceHelperPerCall` | 호출당 가격 | 없음 (불변) |
| `controller/relay.go:164` `PreConsumeBilling(priceData.QuotaToPreConsume)` | 위 표의 값 | 간접 (배수는 이미 적용됨) |
| `service/tiered_settle.go` group retry | `EstimatedQuotaBeforeGroup`(이미 배수 적용) × 새 group ratio | 간접 |
| `relay/relay_task.go:211` task `PreConsumeBilling` | task 가격, `ForcePreConsume=true` | 없음 (task 가격 불변, 신뢰 우회 없음) |
| `service/billing_session.go` `shouldTrust` (지갑) | 신뢰 우회 | `operation_setting.TrustQuota()` |
| 조직 지갑 / 구독 / 조직 구독 funding | `NewBillingSession` 이 넘긴 같은 예약량 | 배수는 간접 적용, `shouldTrust` 는 항상 false |
| `service/pre_consume_quota.go` `PreConsumeQuota` (호출자 없음) | 신뢰 우회 | `operation_setting.TrustQuota()` |

### Task 22: upstream hunk 대조표 (`a073f74b3`, 50행)

범례: 포팅 = 포팅(필요하면 fork 에 맞게 조정), 동등 = fork 에 이미 같은 내용이 있음,
제외 = 이유와 함께 가져오지 않음, 해당 없음 = 그 코드가 fork 에 없음.
이름 대응: upstream 의 단일 요청 `MaxQuota`/`MinQuota`(int32) 는 fork 의
`common.MaxRequestQuota`/`MinRequestQuota`, upstream 의 `MaxWalletQuota`(2^53-1) 는
fork 의 기존 `common.MaxQuota`(5e14, 변경 없음)에 대응한다.

| # | upstream 파일 / hunk | 분류 | fork 처리 |
| --- | --- | --- | --- |
| 1 | `common/quota_math.go` 헤더 주석, `MaxWalletQuota` 상수 | 포팅(조정) | wallet 상한은 기존 `common.MaxQuota`. 단일 요청 상한은 `MaxRequestQuota`/`MinRequestQuota` 로 이름 변경, 분리 이유를 헤더 주석에 기록 |
| 2 | `common/quota_math.go` `ValidateWalletQuota` | 포팅 | int64 인자, 상한 `MaxQuota` |
| 3 | `common/quota_math.go` `QuotaClamp` 주석 / Op 목록 | 해당 없음 | fork 에 `QuotaClamp` 감사 타입 없음 |
| 4 | `common/quota_math.go` `saturateQuota` → `saturateQuotaBounded`, `>`/`<` 경계 | 포팅(조정) | `QuotaFromFloat`/`Strict` 가 bounded helper 를 사용(clamp struct 없음) |
| 5 | `common/quota_math.go` `QuotaFromDecimalStrict` 주석 | 해당 없음 | `common.QuotaFromDecimal*` 없음. `service.decimalToQuota` 주석만 갱신 |
| 6 | `common/quota_math.go` `WalletQuotaFromDecimalStrict` | 포팅(조정) | int64 결과, ±`MaxQuota`, 정확한 decimal 비교 |
| 7 | `common/quota_math_test.go` | 포팅(조정) | request 경계가 int32 로 남는 테스트, `ValidateWalletQuota`, `WalletQuotaFromDecimalStrict` 테스트 |
| 8 | `middleware/model-rate-limit.go` `rateLimitDurationSeconds`, `rateLimitCapacity`, 호출부 | 포팅 | 그대로. `middleware/model_rate_limit_test.go` 추가 |
| 9 | `setting/rate_limit.go` `encoding/json` → `common.Marshal/Unmarshal` | 포팅 | Rule 1 |
| 10 | `setting/rate_limit.go` `maxRateLimitDurationSeconds` 상수 | 포팅 | |
| 11 | `setting/rate_limit.go` 그룹 횟수 상한을 MaxInt32 → MaxInt64/86400 으로 완화 | 제외 (상한은 MaxInt32 유지) | `InMemoryRateLimiter.Request` 가 `make([]int64, 0, maxRequestNum)` 을 호출하므로 큰 횟수를 허용하면 option 저장 한 번으로 다음 요청에서 프로세스가 죽을 수 있음. 상수와 새 오류 문구만 포팅 |
| 12 | `model/main.go` `ensureUserQuotaColumns`, `is64BitIntegerType`, `userQuotaColumns` | 포팅(조정) | fork 에 `common.DatabaseType` 가 없어 `dbType string` 인자와 `mainDatabaseType()` 사용. `Migrator().ColumnTypes` 로 이식성 확보. 오류 문구에 환경변수 override 명시 |
| 13 | `model/main.go` migration 전 배선 | 포팅(위치 조정) | master 는 `migrateDB()` 뒤, 비 master 는 return 전(upstream 과 동일) |
| 14 | `model/utils.go` `addNewRecord` 오버플로 saturation | 포팅(조정) | fork 의 저장소는 int64 이므로 `MaxInt64`/`MinInt64` 에서 saturate (`addNewRecordLocked`) |
| 15 | `model/user.go` `IncreaseUserQuota` 단일 delta `ValidateWalletQuota` | 포팅 | |
| 16 | `model/user.go` cache 증가를 DB 쓰기 뒤로 이동 | 동등 | fork 는 Redis delta 를 먼저 적용하고 DB 실패 시 보상(`applyUserQuotaDelta`) |
| 17 | `model/user.go` `increaseUserQuota` atomic ceiling + `RowsAffected`/count | 일반 helper 는 제외, 지급 경로에 포팅 | fork quota 문서 §6 이 이 변경을 이미 되돌렸음(환불 유실, `batchUpdate` 재큐잉). 신규 credit 이 지갑에 들어가는 지점에서 공유 helper `creditUserQuotaWithinCeiling` 으로 제한(top-up, Redeem, `GrantUserQuota`). 환불은 누적 잔액 상한 없이 단일 delta 검증만 |
| 18 | `model/redemption.go` `Redeem` credit 을 `creditTopUpQuota` 로 | 포팅(조정) | 같은 transaction 의 조건부 GORM UPDATE (`quota <= MaxQuota - credit`) |
| 19 | `model/redemption.go` `Insert`/`Update` 양수 + wallet 상한 | 포팅 | 공유 `model.ValidateRedemptionQuota` |
| 20 | `model/redemption_test.go` | 포팅(조정) | 신규 파일(fork 에 없었음). `Redemption` 을 model `TestMain` migration 에 추가 |
| 21 | `model/quota_reserve_test.go` batch 누적 + saturate | 포팅(조정) | fork 의 `model/user_quota_reserve_test.go` 에 추가 |
| 22 | `model/subscription.go` `calcSubscriptionBalanceQuota` → `WalletQuotaFromDecimalStrict` | 포팅 | 기존에는 상한 없는 `IntPart()`. 테스트 추가 |
| 23 | `model/topup.go` `topUpQuotaMaxCurrent`, `ErrWalletQuotaLimitExceeded`, `Recharge*` → `WalletQuotaFromDecimalStrict` | 동등 | Task 20 이 이미 `common.MaxQuota` 로 옮겼음. fork 에는 `RechargeEpay` 가 없음. Toss 의 결제당 MaxInt32 는 저장 한도가 아닌 정책이라 유지 |
| 24 | `model/payment_method_guard_test.go` | 동등 | Task 20 이 이미 조정 |
| 25 | `controller/redemption.go` Add/Update quota 검사 | 포팅 | `model.ValidateRedemptionQuota` 호출 |
| 26 | `controller/token.go` `maxTokenQuota()` | 포팅(조정) | 맨 `int(1e9*QuotaPerUnit)` 변환 대체. int64, `MaxQuota` 로 cap |
| 27 | `controller/topup.go` `getMinTopup` | 포팅(조정) | `WalletQuotaFromDecimalStrict(...Truncate(0))`: 기존 truncate 유지, `MaxQuota` 에서 saturate |
| 28 | `controller/topup.go` `getTopUpQuota`, `getMaxTopUpAmount`, `validateCreditedQuota` | 동등 | Task 20 이 이미 `common.MaxQuota` 로 제한 |
| 29 | `controller/topup_quota_limit_test.go` | 동등 | Task 20 이 이미 조정 |
| 30 | `controller/user.go` `ManageUser` add/override `ValidateWalletQuota` | 포팅 (add 는 `GrantUserQuota`) | |
| 31 | `controller/user_manage_test.go` `TestManageUserQuotaRespectsWalletCeiling` | 포팅(조정) | 신규 `controller/quota_ceiling_test.go` (fork 에 해당 파일 없음) |
| 32 | `controller/channel-test.go` `settleTestQuota` 맨 변환 | 포팅(조정) | `common.QuotaFromFloat(math.Round(..))`. fork 에 `common.QuotaRound` 없음 |
| 33 | `service/quota.go` `CalcOpenRouterCacheCreateTokens` clamp 시 -1 | 포팅(조정) | `QuotaFromFloatStrict(math.Round(v))`, 오류면 -1 |
| 34 | `service/violation_fee.go` saturate | 포팅(조정) | fork 의 `decimalToQuota`(요청 단위 int32 saturation) 사용 |
| 35 | `service/token_counter.go` 이미지 token 반올림 | 포팅(조정) | `common.QuotaFromFloat(math.Round(..))` |
| 36 | `service/quota_saturation_test.go` 추가분 | 포팅(조정) | 신규 파일. `attachQuotaSaturation` 테스트는 해당 없음 |
| 37 | `service/text_quota.go` 주석 | 포팅(주석) | |
| 38 | `service/text_quota_test.go`, `service/tiered_settle.go` 주석 | 해당 없음 | tiered settle 경로에 `QuotaClamp`/`noteQuotaClamp` 없음. 기존 overflow 테스트가 이미 `math.MaxInt32` 를 검증 |
| 39 | `pkg/billingexpr/types.go` `Clamp` 주석, `settle_clamp_test.go` | 해당 없음 | fork 에 `TieredResult.Clamp` 없음 |
| 40 | `pkg/billingexpr/billingexpr_test.go` 기대값 | 동등 | `math.MaxInt32`(= `MaxRequestQuota`). `round.go` 의 오래된 "32-bit columns" 주석만 정정 |
| 41 | `relay/common/relay_info.go` `QuotaClamp` 주석 | 해당 없음 | 필드가 fork 에 없음 |
| 42 | `relay/helper/price_test.go` expression 상수 | 동등 | tiered pre-consume overflow 테스트가 변경 없이 통과 |
| 43 | `.env.example` `SKIP_64BIT_QUOTA_SCHEMA_CHECK` | 포팅 | |
| 44 | `AGENTS.md` quota 문단 | 해당 없음 | fork 의 AGENTS.md 에 해당 문단 없음 |
| 45 | `web/src/features/usage-logs/types.ts` 주석 | 해당 없음 | 경로가 fork 에 없음(`web/default`), `quota_saturation` 도 없음 |
| 46 | (fork 전용) `service/org_user_export.go` 기존 구성원 행 | 포팅(수정 커밋) | 맨 `int(usd*QuotaPerUnit)` 변환 대신 `importedWalletQuota`: decimal 곱 → truncate → `WalletQuotaFromDecimalStrict`, NaN/Inf 거부. 범위 밖이면 해당 행 전체 거부. 증가는 `GrantUserQuota`, 오류는 행 단위 보고 |
| 47 | (fork 전용) `service/org_user_export.go` 신규 구성원 행 | 포팅(수정 커밋) | 같은 helper. 사용자 생성 전에 거부. quota 설정은 `setImportedUserQuota` (증가는 `GrantUserQuota`, 감소는 `DecreaseUserQuota`) |
| 48 | (fork 전용) `service/user_export.go` | 포팅(수정 커밋) | `quota` 와 `aff_quota` 모두 `ValidateWalletQuota` (aff 는 이후 `TransferAffQuotaToQuota` 로 지갑에 들어감). `Insert` 전에 행 거부 |
| 49 | (fork 전용) `model/checkin.go` transaction 경로 (MySQL/PG) | 포팅(수정 커밋) | `creditUserQuotaWithinCeiling(tx, ...)`. 거부되면 check-in row 도 rollback |
| 50 | (fork 전용) `model/checkin.go` SQLite 경로 | 포팅(수정 커밋) | `GrantUserQuota`. 실패 시 기존처럼 check-in row 삭제 |

46~50 은 upstream hunk 가 아니라 fork 전용 지급 경로다. 같은 wallet 상한 규칙에
해당하므로 리뷰에서 추가했다. 지갑이 이미 ceiling 이면 check-in 은
`check-in failed: wallet quota limit reached` 를 반환한다. import 변환이 float 에서 정확한
decimal 곱으로 바뀌어 예를 들어 0.29 USD 는 144999 가 아니라 145000 이 된다.

### Task 23: token 추정치 전/후

fixture: `service/testdata/claude_code_request.json` (Claude Code 형태의 `/v1/messages`
요청. system 2 블록, tool 정의 20개, tool_use/tool_result 왕복 포함 30개 메시지).
테스트 `service/token_counter_estimate_test.go` 의
`TestEstimateRequestTokenClaudeCodeFixture` 가 값을 고정한다.

| 모델 (family) | 이전 `2ba5cc513` | 이후 `c0b313844` | 변화 |
| --- | --- | --- | --- |
| `claude-sonnet-4-5-20250929` (claude) | 3650 | 7748 | +112% |
| `claude-opus-4-7` (claude4.7) | 3650 | 10200 | +179% |

이전 값은 같은 fixture 를 `2ba5cc513` 의 임시 worktree 에서 돌린 결과다. 이전 코드는
Claude tool 정의를 전부 건너뛰어(`ToolsCount=0`, 합친 텍스트 10,364 bytes) 두 모델이
같은 값이었다. 이후에는 `ToolsCount=20`, 합친 텍스트 23,391 bytes 이며 Claude 4.7
family 가 따로 보정된다.

`tokenkit` 호출자 대응:

| 이전 | 이후 |
| --- | --- |
| `service.CountTextToken` (token_counter, 릴레이 fallback) | `tokenkit.Count` |
| `service.EstimateTokenByModel` / `EstimateToken` / `getMultipliers` | `tokenkit.Estimate` / `tokenkit.EstimateFamily` (재보정한 가중치) |
| `getTokenEncoder` / `getTokenNum` / `InitTokenEncoders` | 삭제. tokenkit 이 `sync.OnceValue` 로 지연 생성 |
| `common.IsOpenAITextModel`, `OpenAITextModels` | 삭제. `tokenkit.ModelFamily` |
| `getImageToken` 의 OpenAI 타일/패치 계산, 비 OpenAI 520 고정, glm-4 1047 | `tokenkit.ImageTokens` / `ImageNeedsSize` (provider 별 규칙, 모르는 family 는 520 유지) |

## 반영 후 동작

### 충전과 지갑 상한 (Task 20, 22)

- 충전 요청이 대상 지갑의 `common.MaxQuota`(5e14)를 넘게 되면 결제 provider 를 호출하기
  전에 거부된다. 사용자/조직 지갑을 구분하고, 자동 충전 등록도 같은 검사를 받는다.
- 거부는 HTTP 200 + `success:false` 로 응답한다(fork 핸들러 관례. 프런트가 이미 success
  플래그를 본다). Creem 은 기존 `{"message":"error","data":...}` 형식.
- 환불은 상한이 없다. 충전, 상환 코드(`Redeem`), 관리자 부여(`ManageUser` add), CSV
  import, check-in 등 새 credit 이 지갑에 들어가는 경로만 `MaxQuota` 로 제한된다.
- `Redemption` 은 quota 가 양수이고 wallet 범위 안이어야 한다. 기존 quota ≤ 0 상환 코드는
  `status_only` 로 상태를 바꿀 수 없고(삭제는 가능), 상환하면 실패한다.
- 단일 요청의 saturation(`QuotaFromFloat*`)은 int32 에 그대로 두었다.
  `QuotaFromFloatStrict(2147483647)` 은 이제 허용되고(경계 `>=` → `>`), 2147483648 이상은
  여전히 거부된다.
- 기타: 구독 balance 계산(`calcSubscriptionBalanceQuota`)과 token 최대 quota 가 상한을
  지킨다. 위반 수수료, OpenRouter cache 생성 token 계산, 채널 테스트 정산은 bare cast 대신
  saturating 변환을 쓴다. batch update 누적이 int64 에서 wrap 하지 않고 saturate 한다.

### pre-consume (Task 21)

- 예약량은 입력 토큰 × `quota_setting.pre_consume_multiplier`(비율 경로는 그대로
  modelRatio × groupRatio). 이전의 최소 500 token, `max_tokens` 가산, tiered 의 8192
  출력 추정은 모두 없어졌다. tiered 는 `C=0`, `P=Len=prompt` 로 context tier 를 고르고,
  CountToken 이 꺼져 있으면 입력 추정이 0 이므로 예약도 0 이다.
- tier 선택과 정산(settlement)은 배수의 영향을 받지 않는다. snapshot 에
  `pre_consume_multiplier` 를 기록하며, 없는(구) snapshot 은 ×1 로 본다.
- 지갑과 token 이 모두 `quota_setting.trust_quota_usd`(USD)를 넘으면 예약을 건너뛴다.
  구독, 조직 지갑, 비동기 task 는 우회하지 않는다. 비교는 float 이라 소수 임계값(10.5)도
  정확히 비교된다.
- 옵션은 DB 쓰기 전에 검증된다. 잘못된 값 저장은 저장값과 메모리 값 모두 바뀌지 않는다.
- 관리자 UI(`web/default`)에 두 입력이 추가되고, 쓰지 않는 `PreConsumedQuota` 입력은
  제거됐다.

### tokenkit (Task 23)

- 모든 텍스트 token 계산이 `tokenkit` 으로 간다. 이미지는 provider 별 규칙으로 계산한다.
- Claude tool 정의가 이제 계산에 들어간다. `tool_result` 는 텍스트/이미지/문서 파트를
  구분해 센다. Gemini 는 `systemInstruction`, function call/response, `mediaResolution`
  을 센다. Chat 은 assistant `tool_calls`, custom tool, JSON 으로 직렬화한 parameters 를
  센다. Responses 는 문자열 instructions 와 `function_call`/`custom_tool_call` 및 출력을
  센다.
- Claude 스트림이 끊기면 tool 이름, `input_json_delta` 와 `tool_use` 개수(×40)를
  completion fallback 에 포함한다. OpenAI 비스트림 fallback(usage 가 없을 때만)은
  tool call 이름과 인자를 센다(upstream 은 카운터만 바꿨고 이 부분은 fork 에서 더한
  것이다. usage 가 없을 때만 실행되므로 이중 계산은 없다).
- Gemini: 응답이 prompt 만 보고하고 completion 이 0 이면 보고된 prompt 와 cache usage 를
  유지하고 completion 만 추정한다(이미지만 있으면 이미지당 1400). usage 가 아예 없는
  비스트림 응답은 응답 텍스트로 completion 을 추정한다(이전에는 0).
- `ParseContent` 는 생략된 `image_url.detail` 을 더 이상 `"high"` 로 채우지 않는다.
  `zhipu_4v` 변환은 클라이언트가 생략하면 `detail` 을 보내지 않는다.
- 이미지 계산: Claude, Gemini 이미지는 flat 520 대신 provider 별로 계산한다.
  `GET_MEDIA_TOKEN`(기본 true)이 이미지를 실제로 디코딩/다운로드해 크기를 잴지
  결정하고, 비스트림 요청에는 `GET_MEDIA_TOKEN_NOT_STREAM`(기본 false)도 켜져 있어야 한다.
  즉 기본값에서는 스트림 요청만 크기를 잰다. 형식을 알 수 없고 디코딩이 안 되는
  Claude 이미지는 이제 오류가 난다(이전에는 비 OpenAI 모델에서 520 고정). `glm-4*` 이미지는
  1047 에서 모르는 family 의 520 이 된다.
- 빌드: `go.mod` 가 `go 1.26`, `tokenkit` replace, tiktoken tokenizer v0.8.1 이다.
  Dockerfile / Dockerfile.dev 가 `go mod download` 전에 `tokenkit/go.mod` 를 복사하고,
  makefile 에 `test` 타깃(root + tokenkit)이 생겼으며, release/electron workflow 의
  `go-version` 은 `>=1.26` 이다.

## 수행한 검증

실행한 명령과 결과는 아래와 같다 (작업 디렉터리 `/Users/molla/work/ai/new-api`,
HEAD `499784e79`, Go 1.27 toolchain).

- `go build ./...`: 통과 (종료 코드 0).
- `go vet ./...`: `common/custom-event.go` 의 lock by value 경고 4건(60, 73, 75, 78행;
  `CustomEvent` 가 `sync.Mutex` 를 포함)만 출력한다. 1단계 이전부터 있던 경고이며 이번
  변경으로 늘지 않았다. 이 경고 때문에 종료 코드는 1 이다.
- `go test ./... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort`:
  - 36개 패키지 ok, FAIL 0. 베이스라인(31개)보다 `i18n`, `pkg/billingexpr`,
    `relay/channel/task/sora`, `router`, `types` 5개가 늘어난 것은 1단계와 같다.
  - FAIL 비교: `diff <(grep FAIL baseline.txt | sort) <(grep FAIL after2.txt)` 출력이 없다.
    새 FAIL 이 없다.
- `(cd tokenkit && go test ./...)`: ok. `tokenkit` 은 별도 모듈이라 루트 `./...` 에는
  포함되지 않는다.
- 프런트엔드(`web/default`):
  - `bun run typecheck`: `error TS` 23건. 이 브랜치가 건드리지 않은 파일에서만 나오며
    브랜치 시작 전부터 있던 오류다. 파일별 건수는
    `organizations/components/organization-dashboard.tsx` 1,
    `organizations/components/organization-users-table.tsx` 3,
    `system-settings/integrations/wallet-auto-recharge-presets-section.test.ts` 1,
    `system-settings/models/vendor-discount-visual-editor.tsx` 1,
    `usage-logs/components/common-logs-filter-bar.tsx` 9,
    `usage-logs/components/task-logs-filter-bar.tsx` 2,
    `usage-logs/components/usage-logs-mobile-card.tsx` 2,
    `usage-logs/components/usage-logs-table.tsx` 1,
    `usage-logs/index.tsx` 1,
    `wallet/lib/auto-recharge-options.test.ts` 1,
    `i18n/languages.test.ts` 1 이다 (경로는 모두 `src/features/` 아래, 마지막만
    `src/i18n/`). 이 브랜치가 수정한 web 파일(`system-settings/billing/*`,
    `general/quota-settings-section*`, `types.ts`, `en.json`, `kr.json`)에서는 오류가
    나지 않는다.
  - `bun run build`: 성공.
- 각 task 는 실패하는 테스트를 먼저 작성한 뒤 구현했고(RED 확인 후 GREEN), task 별 리뷰를
  거쳤다. Task 22 와 Task 23 에 수정 라운드가 1회씩 있었다.
- model/controller/service 테스트는 in-memory SQLite 로 실행했다. MySQL/PostgreSQL 에서의
  별도 실행은 하지 않았다. 새 DB 코드는 GORM 만 쓰며(`Migrator().ColumnTypes`,
  조건부 `Update`), raw SQL 은 SQLite 전용 test fixture 의 `CREATE TABLE` 뿐이다.
  Task 21 의 controller DB 매트릭스는 SQLite 만 포팅했다(fork 에 MySQL/PG leg 용
  helper 가 없다).
- 32-bit 대상 확인: `GOOS=linux GOARCH=386 go build` 는 `common`, `model`, `setting`,
  `pkg/billingexpr` 에서 통과한다. 전체 386 빌드는 Task 14 에서 들어온 상수 때문에
  깨진다(아래 보류 TODO, 지원 대상 아님).

## 운영 전제

### 새 옵션과 환경 변수

- 새 옵션 `quota_setting.pre_consume_multiplier` (기본 1, 0 보다 큰 유한 수)와
  `quota_setting.trust_quota_usd` (기본 10 USD, 0 이상, **0 이면 신뢰 우회 끔**).
  기존 `PreConsumedQuota` 옵션은 구 클라이언트가 읽고 쓸 수 있도록 남겨 두었지만
  과금에는 쓰이지 않는다.
- 새 환경 변수 `SKIP_64BIT_QUOTA_SCHEMA_CHECK` (기본 꺼짐, `.env.example` 에 문서화).
  `RELAY_RESPONSE_HEADER_TIMEOUT` 은 1단계에서 추가된 것이며(기본 1800초, 1단계 기록
  참조) 2단계에서 바뀐 것은 없다.
- 시작 검사 `ensureUserQuotaColumns`: MySQL/PostgreSQL 에서 `users.quota`,
  `users.used_quota`, `users.aff_quota`, `users.aff_history` 가 32-bit 정수면 시작이
  실패한다. master 노드는 `migrateDB()` 뒤에 검사하므로 자동 widening 이 성공한
  배포는 영향이 없고, 비 master 노드는 migration 하지 않으므로 아직 widening 되지 않은
  스키마에서는 시작이 거부된다. 잠시 우회하려면 `SKIP_64BIT_QUOTA_SCHEMA_CHECK=true`.
  SQLite 는 검사하지 않는다.
- 빌드: `go.mod` 가 `go 1.26` 이므로 빌드 이미지와 CI 는 Go ≥ 1.26 이어야 한다.
  repo 의 Dockerfile 과 release/electron workflow 는 이미 맞췄지만 별도 이미지나 로컬
  CI 는 확인해야 한다. `tokenkit` 은 replace 로 연결된 하위 모듈이므로 Docker 빌드
  context 에 `tokenkit/` 가 있어야 한다.

### 예약량과 token 추정의 변화 (과금 영향)

- 예약량이 줄어든다(release note 대상). 비율 모델은 최소 500 token 에 `max_tokens` 를
  더하던 예약이, tiered 모델은 출력 `max_tokens`(없으면 8192)를 더하던 예약이 모두
  입력 × 배수만으로 줄어든다. CountToken 이 꺼져 있으면 0 이다. 한 요청의 실제 비용이
  예약보다 크면 정산 시 초과분을 추가로 차감한다. 예약이 작아지는 만큼 잔액이 부족한
  사용자의 요청이 사전 단계에서 덜 걸러지므로, 필요하면 `pre_consume_multiplier` 를
  올린다.
  공식이 바뀌어(최소 500 token, `max_tokens`, tiered 8192 출력 추정 제거) token 당
  예약은 줄어들지만, 아래처럼 tokenkit 보정이 에이전트 트래픽의 입력 token 추정을 올린다.
  순효과는 모델과 트래픽 형태에 따라 다르며, tool 정의가 많은 Claude Code / Codex 류
  요청에서는 추정치 상승이 더 크게 작용한다.
- 에이전트 트래픽의 token 추정이 2~3배 높아진다 (표의 fixture 기준 +112%, +179%).
  추정치는 usage 를 못 받았을 때의 fallback 과 pre-consume 예약에 쓰이므로, 예약량은
  그만큼 올라가고(예약이 입력 추정 × 배수이므로) 잔액이 작은 사용자는 이전보다 더 쉽게
  "quota 부족" 을 만난다. 반대로 usage 를 받는 정상 응답의 정산은 영향이 없다.
- Gemini: completion 이 0 으로 오는 응답은 보고된 prompt usage 를 유지하고 completion
  만 추정한다. usage 가 아예 없는 비스트림 응답은 completion 을 응답 텍스트로
  추정한다(이전에는 0 으로 과소 과금).
- `GET_MEDIA_TOKEN`(기본 true)은 provider 별 이미지 token 계산을 위해 이미지를 실제로
  읽을지를 결정하며, 기본값에서는 스트림 요청만 해당한다(비스트림은
  `GET_MEDIA_TOKEN_NOT_STREAM=true` 필요). 이미지를
  디코딩할 수 없는 Claude 요청은 이제 오류가 난다. `image_url.detail` 이 기본 `high`
  가 아니게 되어 detail 을 생략하는 클라이언트의 이미지 추정이 낮아진다.
- Responses 스트림 fallback 은 tool 이름을 세지 않는다(upstream 과 동일).

### 판단 기록 (ruling)

원장의 `Task 20~23: Ruling:` 줄이며, 틀렸을 때의 비용을 함께 적는다.

- Task 20: 지갑 수용량 거부는 HTTP 400 이 아니라 HTTP 200 + `success:false` 로 응답한다
  (fork 핸들러 관례). 틀렸다면: 상태 400 을 기준으로 거부를 판단하던 클라이언트는
  거부를 보지 못한다. 프런트는 success 플래그를 쓰므로 영향 없음.
- Task 20: upstream 의 `creditTopUpQuota` 는 포팅하지 않았다. `model.CreditTopUpTarget` 의
  조건부 UPDATE 가 이미 상한을 atomic 하게 지킨다(리뷰어가 확인). 틀렸다면: 동시 callback
  으로 `MaxQuota` 를 넘어 과충전될 수 있다(TOCTOU).
- Task 22: upstream 의 최종 설계를 따라 단일 요청 saturation(`QuotaFromFloat*`)은 int32
  에 두고 wallet 도메인 helper 를 따로 두었다. 계획의 "MaxInt32 → MaxQuota 치환" 은
  upstream 의 최종 설계와 다르다. wallet helper 의 상한은 upstream 의 2^53-1 대신 fork 의
  기존 `common.MaxQuota`(5e14)이다. 틀렸다면: 한 요청이 약 $4.3k 상당을 넘으면 saturate
  된다(upstream 도 같다).
- Task 22: `increaseUserQuota` 의 누적 잔액 ceiling 은 포팅하지 않았다. fork quota 문서
  §6 이 이미 되돌린 설계다(환불 유실, `batchUpdate` 재큐잉). 대신 새 credit 이 들어가는
  지점(충전, 상환, 관리자 부여, import, check-in)에서 atomic helper 로 제한한다.
  틀렸다면: 환불이 지갑을 `MaxQuota` 위로 올릴 수 있다(직전 정산 금액으로 제한됨).
- Task 22: `ensureUserQuotaColumns` 는 master 에서 `migrateDB()` 뒤에, 비 master 에서는
  return 전에 돈다. fork 는 AutoMigrate 가 컬럼을 bigint 로 넓히므로 migration 전에
  검사하면 widening 이 안 된 MySQL/PG 가 부팅하지 못한다. 틀렸다면: widening migration 이
  실패한 master 가 오류가 나기 한 단계 전까지 진행한다.
- Task 22: `GOOS=386` 빌드의 `service/http_client.go:39`(Task 14 의 `maxTimeoutSeconds`
  상수) 깨짐은 지원 대상이 아니다(Dockerfile/release 는 amd64/arm64). 틀렸다면: 32-bit
  빌드가 필요해질 때 상수를 int64 로 바꾸면 된다.
- Task 23: Gemini 가 prompt 만 보고하고 completion 이 0 일 때 보고된 prompt 를 유지하는
  것은 upstream 방식을 따랐다. 아래 세 가지를 검토했다. D1 = OpenAI 비스트림 fallback 이 tool call 이름/인자를
  세는 것(usage 가 없을 때만 실행되어 중복 계산이 불가능하므로 수용). D2 = 이미지 token 을
  provider 별로 산정하고(`GET_MEDIA_TOKEN` 게이트, 기본은 스트림만) 디코딩할 수 없는
  Claude 이미지는 오류로 처리하는 것(upstream 동일). D3 = `image_url.detail` 기본값
  `"high"` 제거(upstream 동일). 틀렸다면: 이미 usage 가 있는 요청에는 영향이 없으므로 추정만 약간 어긋난다.

## 보류된 TODO

### 2026-10-14 해소 현황

`codex/followups-2a-2026-10` 브랜치가 아래 8건을 해소했다(항목은 지우지 않고 남긴다).
상세는 `docs/2026-10-14-followups-2a.md`.

- Chat 요청의 `reasoning_effort` 등이 Claude 5.x/Opus 4.7, 4.8 에서 `thinking.type:"enabled"` 로
  매핑되어 400 이 나는 문제 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`)
  (Task 1, `05bdb2596`).
- `EpayNotify` 의 success 선기록 순서 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 2, `7ab5a8aa9`).
- `service/task_polling.go` 의 201/202 거부 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 3, `1dd9351f6`).
- Gemini prompt IMAGE modality 누락 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 4, `af1eddc8e`).
- 관리자 preset 생성 경로의 `ThresholdQuota` 무제한 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 5, `4e172773a`).
- 자동 충전 fail-closed 로그의 조직 target → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 6, `3473f535a`).
- 매핑된 Qwen 배포명의 `thinking_budget` 제거(1단계 Task 18 의 보완) → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 7, `70b14d049`).
- tokenkit codec 첫 호출 지연 → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`) (Task 8, `338576ab2`).

### 의도적으로 포팅하지 않은 upstream 항목 (relaykit 과제)

1단계 기록의 목록(아래)을 그대로 유지하고, 2단계에서 검토하고 제외한 항목을 더한다.
모두 upstream 의 `relaykit` 모듈 분리, `web/src` 단일 트리, JS 플러그인 구조에 의존하며
team 에는 해당 기반이 없다.

- 모델명 `@` modifier (`@thinking:`, `@effort:`, `@temperature:`, `@topp:`)와 이에
  의존하는 과금 identity 사다리.
- hosted tool(web search, grounding, citations)의 프로토콜 간 변환, Responses↔
  Claude/Gemini 직접 변환기, Responses custom tools(Codex exec/apply_patch) 변환.
- Responses WebSocket 릴레이.
- 비동기 task 어댑터의 JS 플러그인화(`plugins/tasks`, `pkg/jsplugin`).
- auth, i18n 재작업.
- `web/src` 단일 트리 이동에 따른 프런트엔드 변경.
- (2단계에서 추가) search 과금 `d10f11fff`, `acb347a19`.
- (2단계에서 추가) expression 확장 `064ed943e`, `f362c7c51`, `25ec832fa`.
- (2단계에서 추가) 모델/vendor 가격 재작업 `0c76e4dae`.

### 리뷰에서 나온 후속 후보 (task 별)

기능 결함으로 판정되어 막힌 것은 없고, 모두 minor 로 보류했다. 1단계의 보류 항목은
1단계 기록을 따른다.

- Task 20 (충전)
  - `EpayNotify` 가 주문을 success 로 표시한 뒤 crediting 을 transaction 밖에서 한다.
    crediting 이 실패하면(예: 동시 증가로 수용량 초과) 주문은 success 인데 지급되지 않고
    이후 callback 도 재시도하지 않는다. 사전 검사가 가능성을 낮출 뿐 없애지는 못한다
    (기존 동작, 후속 후보). → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`).
  - 자동 충전 fail-closed 로그가 조직 검사에서도 사용자 target 을 보고한다
    (`controller/topup.go:367`) → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`). `Validate*Capacity` helper 중복. 테스트 공백: Waffo
    tokens 모드 최소 1, 조직 threshold 자동 충전, 조직 Toss krw 모드, tokens 모드 PayPal.
    Stripe 견적과 결제 사이 1단위 truncation 은 upstream 도 같다.
  - 관리자 preset 생성 경로는 `ThresholdQuota` 를 제한하지 않는다. 사용자 등록 시에는
    거부되지만 관리자는 만들 수 있다. → 2026-10-14 해소 (`docs/2026-10-14-followups-2a.md`).
- Task 21 (pre-consume)
  - `expr.md` 와 `price.go` 의 "tiered leaf 는 모두 token 가격" 문구는 과장이다.
    상수/param 항도 같이 배수로 커진다.
  - `relay/helper/valid_request.go:117-119` 주석이 낡았다. `web/classic` 은 아직
    `PreConsumedQuota` 입력을 보이고 새 두 옵션이 없다. `service/pre_consume_quota.go`
    는 호출자 없는 죽은 코드이며 정리 후보다.
- Task 22 (ceiling)
  - `Recharge*`/`CreditedQuotaForTopUp` 의 맨 `IntPart()`(upstream 에서는 검증으로
    제한됨), 상환/부여에 `ErrTopUpQuotaCapacityExceeded` 를 재사용하는 이름 문제,
    `model-rate-limit.go:54,71,128` 의 `Duration(minutes)*time.Minute` 가드 없음
    (upstream 동일).
  - SQLite check-in 경로는 create 뒤 delete 방식이라 atomic 하지 않다(기존 모양).
    import 변환은 truncate 하므로 `QuotaPerUnit` 이 끝나지 않는 소수면 `Round(0)` 를
    고려. 조직 import 는 grant 보다 field 갱신을 먼저 적용한다.
    `TransferAffQuotaToQuota` 와 `QuotaForNewUser` 는 아직 상한이 없다(upstream 동일).
  - 32-bit 전체 빌드(`service/http_client.go:39` 상수 int64 화), `Redeem` 이 Redis
    사용자 cache 를 건드리지 않고 ManageUser `override` 가 cache 를 갱신하지 않는 기존
    동작.
  - `docs/quota-ceiling-and-bigint-migration.md` §4.3 ("관리자 부여 경로의 상한 부재")과
    §9.3 은 ManageUser add/override 와 상환 코드에 대해 이번에 해소됐다. 그 문서에
    반영하는 작업은 이 범위에서 하지 않았다.
  - 모델별 요청 rate limit 그룹 횟수 상한은 MaxInt32 로 유지했다(in-memory limiter 가
    횟수만큼 preallocate 한다. 관리자 설정만 해당).
- Task 23 (tokenkit)
  - Responses 스트림 fallback 이 tool 이름을 세지 않는다(upstream 동일). OpenAI 비스트림
    fallback 에는 스트림 경로가 더하는 `toolCount*7` 이 없다 (두 경로가 tool call 을
    다르게 추정한다).
  - `THIRD-PARTY-LICENSES.md` 의 tokenizer 버전이 낡았다. `dto/gemini.go` 는 기존부터
    gofmt 되어 있지 않다.
  - Gemini 스트림 usage 판정은 `TotalTokenCount != 0` 이다. upstream 의
    `HasGeminiUsageMetadataTokens` 는 더 넓어서, total 이 0 인 prompt-only chunk 는 전체
    추정으로 떨어진다. 스트림은 last-frame-wins 이며 upstream 의 non-zero merge 와 다르다.
  - `go mod tidy` 가 `aws-sdk-go-v2/aws/protocol/eventstream` 의 `// indirect` 표시를
    지우려 한다(`2ba5cc513` 부터 있던 드리프트, 버전/모듈 변화 없음).

## 저장소 상태 참고

작업은 1단계와 같이 worktree 없이 main checkout 의 `codex/upstream-sync-2026-10`
브랜치(분기 시점 team `5167b251d`)에서 로컬 커밋으로만 수행했으며 push 하지 않았다.
`team` 에 병합하지 않았다. 병합은 사용자 확인 뒤
`git checkout team && git merge --no-ff codex/upstream-sync-2026-10` 로 진행한다.
