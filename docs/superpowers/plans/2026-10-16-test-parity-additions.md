# team 테스트 동등성 보강 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `docs/2026-10-16-team-test-parity-audit.md`에서 `missing`·`partial`로 분류된 Go 테스트 41건과 문서 2건을 `develop`에 추가하고, 테스트만이 아니라 코드도 빠진 세 항목(§4)을 fork 훅으로 포팅한다.

**Architecture:** 테스트는 새 `*_fork_test.go` 파일로만 추가한다(upstream `_test.go`는 손대지 않아 다음 upstream 동기화 때 충돌이 없다). relaykit은 upstream 모듈이라 수정하지 않으며 relaykit 동작은 호스트 패키지(`dto/`, `relay/channel/claude/`)에서 테스트한다. 코드 포팅 셋은 upstream 파일에 `// fork: O11` 한 줄 훅 + `*_fork.go` 로직 + `docs/fork-overlays.md` 행.

**Tech Stack:** Go 1.26(`GOWORK=off`), testify, miniredis, gin httptest, SQLite in-memory GORM.

**Spec:** `docs/2026-10-16-team-test-parity-audit.md` §3·§4. 행 단위 근거: `docs/reviews/2026-10-16-team-test-parity-audit.tsv`. team 원본 테스트는 작업 트리에서 `git show team:<경로>`로 읽는다(team 브랜치 `9ef93eb8c`, 읽기 전용).

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`(시작 HEAD `0fd699b9e`). team 브랜치는 읽기만 한다. `/Users/molla/work/ai/new-api`(team 체크아웃)도 읽기 전용.
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal/UnmarshalJsonStr/DecodeJson`만(테스트도 같다; `encoding/json`은 `json.RawMessage` 타입 참조만 허용).
- `relaykit/`, `tokenkit/` 디렉터리는 수정 금지. upstream `_test.go` 파일 수정 금지 — 새 파일은 `<주제>_fork_test.go`.
- upstream 비테스트 Go 파일을 고칠 때는 한 줄 훅 `// fork: O11`만 두고 로직은 `*_fork.go`에. 훅마다 `docs/fork-overlays.md`의 "### 테스트 동등성 감사 이월 (2026-10-16)" 표(Task 3에서 만든다)에 행을 추가한다. 표 열: overlay | upstream 파일 | 훅 | fork 전용 파일.
- team 테스트를 옮길 때 develop에 없는 심볼(`setupTossBillingModelTestDB`, `ErrQuotaCachePending`, `newRelayTransport` 등)은 develop의 대응 심볼로 바꾼다. 이 계획의 각 Task에 대응 심볼을 적었다.
- 동시성 테스트는 `-race`에서도 결정적이어야 한다: goroutine은 `sync.WaitGroup`+시작 배리어(`chan struct{}` close)로 동시에 출발시키고, 결과는 `atomic` 카운터나 채널로 모은다. `time.Sleep`으로 순서를 맞추지 않는다.
- DB 테스트는 SQLite in-memory(`gorm.Open(sqlite.Open("file::memory:?cache=shared"))` 패턴은 각 패키지의 기존 `setup*TestDB` 헬퍼를 따른다). Redis가 필요하면 `model/user_cache_auth_version_test.go`의 `useUserCacheMiniRedis(t)`처럼 miniredis.
- 네트워크 테스트는 `httptest.Server`만. 타임아웃 값은 수백 ms 이내로 잡아 패키지 테스트가 길어지지 않게 한다.
- 각 Task 끝: 해당 패키지 `go test ./<pkg>/ -count=1`, 그리고 `gofmt -l`이 비어 있어야 한다. 마지막 Task 뒤 `GOWORK=off go test ./... -count=1`.
- 커밋 트레일러: team 테스트를 옮긴 커밋은 `Ported-From: team <sha>`(출처마다 한 줄) + `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. 출처 sha는 각 Task에 적었다.

## Review Focus

1. **잔액 100인 사용자가 30짜리 요청 10개를 동시에 보낼 때:** 정확히 3개만 통과하고 잔액이 음수가 되면 안 된다. → Task 2 `TestTryReserveUserQuotaConcurrentRequestsCannotOverspend`, Task 8 `TestPreConsumeTokenQuotaConcurrentRequestsCannotOverspend`.
2. **관리자가 custom_seconds에 `math.MaxInt64`를 넣은 플랜을 만들 때:** 거부되어야 하고, 사용자 구독이 과거 종료 시각으로 생성되면 안 된다. → Task 3.
3. **usage를 주지 않는 upstream의 tool-call 응답:** tool 이름·인자가 completion 토큰에 들어가야 한다. → Task 7 `TestOpenaiHandlerFallbackUsageCountsToolCalls`.
4. **Gemini SDK가 `?key=`로 보내는 모델 목록 요청:** Authorization으로 승격되어야 하고 채팅 경로는 승격되면 안 된다. → Task 5.
5. **헤더를 보내지 않는 upstream:** `RelayResponseHeaderTimeout` 안에 실패해야 하고, 헤더는 왔지만 본문이 느린 응답은 성공해야 한다. → Task 8.

---

### Task 1: common · types · setting 테스트

**Files:**
- Create: `common/quota_math_fork_test.go`, `types/price_data_other_ratio_fork_test.go`, `setting/model_setting/gemini_imagine_fork_test.go`, `setting/operation_setting/quota_setting_preconsume_fork_test.go`, `setting/rate_limit_group_bounds_fork_test.go`, `setting/ratio_setting/gpt56_ratios_fork_test.go`

**team 원본:** `common/quota_math_test.go`(3ce949f6e), `types/price_data_other_ratio_test.go`(f7b7fa5e0), `setting/model_setting/gemini_imagine_test.go`(0b1b0fd8f), `setting/operation_setting/quota_setting_test.go`(cf68ad6d6), `setting/rate_limit_test.go`(3f1cbeeb7), `setting/ratio_setting/cache_ratio_gpt56_test.go`(309582c2f), `setting/ratio_setting/completion_ratio_gpt56_test.go`(cd40cf22d).

- [ ] **Step 1: 각 team 파일을 `git show team:<경로>`로 읽고 develop 심볼을 확인한다.** `common.ValidateWalletQuota(quota int) error`(common/quota_math.go:22), `types.PriceData.AddOtherRatio/ReplaceOtherRatios`(types/price_data.go:39), `model_setting.IsGeminiModelSupportImagine`(gemini.go:111), `setting.CheckModelRequestRateLimitGroup`(rate_limit.go:62), `ratio_setting.GetCreateCacheRatio/GetModelRatio/GetCompletionRatioInfo`.
- [ ] **Step 2: 테스트를 쓴다.** 각 파일의 케이스:
  - `TestValidateWalletQuota`: `common.MaxWalletQuota` OK, 0 OK, 음수 OK, `MaxWalletQuota+1` 오류이고 메시지에 상한 숫자가 들어 있다.
  - `TestAddOtherRatioRejectsNonFinite`: `math.NaN()`, `math.Inf(1)`, `math.Inf(-1)`은 저장되지 않고 1.5는 저장된다(`ApplyOtherRatiosToFloat(100)`이 150). `ReplaceOtherRatios`도 같은 표.
  - `TestIsGeminiModelSupportImagineGemini3`: `gemini-3-pro-image`, `gemini-3.1-flash-image`와 team 파일에 있는 preview 이름이 true.
  - `TestQuotaSettingPreConsumeDefaults`, `TestValidateQuotaOptionPreConsumeKeys`(표), `TestInputPreConsumeMultiplierRejectsInvalidRuntimeValues`(0, -0.5, NaN, ±Inf → 오류): team 파일 그대로. develop의 함수 이름이 다르면 `setting/operation_setting/quota_setting.go`에서 찾아 바꾼다.
  - `TestCheckModelRequestRateLimitGroupBounds`: 최대값 OK, 최대+1은 정확한 메시지로 오류, 음수·`[1,0]` 메시지, 잘못된 JSON 오류; count 상한이 용량 계산을 wrap하지 못함; `Update`/`GetGroupRateLimit`/`Group2JSONString` 왕복.
  - `TestGpt56RatiosExplicit`: `GetCreateCacheRatio("gpt-5.6-sol"/"-terra"/"-luna")` == 1.25이고 명시됨(bool true); `GetModelRatio`: gpt-5.5=2.5, sol=2.5, terra=1.25, luna=0.5; `GetCompletionRatioInfo(...).Locked == false` 세 모델.
- [ ] **Step 3: 실행.** `GOWORK=off go test ./common/ ./types/ ./setting/... -count=1`
- [ ] **Step 4: 커밋** `test(parity): common/types/setting cases from team` + Ported-From 7줄.

### Task 2: model 테스트

**Files:**
- Create: `model/redemption_ceiling_fork_test.go`, `model/subscription_balance_quota_fork_test.go`, `model/quota_reserve_concurrency_fork_test.go`, `model/user_quota_ceiling_fork_test.go`, `model/user_export_fork_test.go`, `model/user_quota_schema_fork_test.go`

**team 원본:** `model/redemption_test.go`(3f1cbeeb7), `model/subscription_balance_quota_test.go`(3f1cbeeb7), `model/token_quota_reserve_test.go`(a1c25c3a4), `model/user_quota_reserve_test.go`(ba6afab57), `model/user_export_test.go`(4728bdc34; team은 `//go:build integration`이었다 — develop에서는 태그 없이 SQLite로 돈다), `model/user_quota_schema_test.go`(3f1cbeeb7).

**Interfaces:** develop 심볼 `TryReserveUserQuota(id, quota int) (bool, error)`, `TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error)`(quota_reserve.go), `IncreaseUserQuota(id, quota int, db bool) error`(user.go:1351), `calcSubscriptionBalanceQuota(priceAmount float64) (int, error)`(subscription.go:745), `GetAllUsersForExport()`(user_export.go), `ensureUserQuotaColumns(db *gorm.DB, dbType common.DatabaseType) error`, `is64BitIntegerType(dbType common.DatabaseType, dataType string) bool`(main.go:284,313). SQLite 헬퍼는 `model/organization_test.go:setupOrganizationModelTestDB` 패턴을 따라 이 파일 안에 작은 헬퍼를 둔다(기존 테스트의 전역 `DB`를 바꾸고 `t.Cleanup`으로 복원).

- [ ] **Step 1: team 파일 6개를 읽는다.** team의 `ErrQuotaCachePending`·차단 관련 케이스와 `sync delta`/TTL 케이스는 옮기지 않는다(O12 결정). `setupTossBillingModelTestDB` → 이 Task의 SQLite 헬퍼.
- [ ] **Step 2: 테스트.**
  - 교환 코드: 잔액 `MaxWalletQuota-10` 사용자가 10짜리 코드를 교환하면 성공하고 잔액이 정확히 `MaxWalletQuota`. team `Insert` 0·음수 quota 메시지 단언도 옮긴다.
  - `calcSubscriptionBalanceQuota`: price×`QuotaPerUnit`이 정확히 `MaxWalletQuota`면 OK, +1 단위면 오류, 1e30 오류.
  - 동시 과소비(사용자): Redis 없이(`common.RedisEnabled=false`) 잔액 100, goroutine 10개가 각 30 예약 → 성공 정확히 3, 최종 잔액 10, 음수 금액은 오류. 토큰 쪽도 같은 표(토큰 `RemainQuota` 100, `unlimited=false`) + `unlimited=true`는 항상 성공.
  - `IncreaseUserQuota`: delta > `MaxWalletQuota` 거부; DB 경로에서 잔액이 상한일 때 `ErrWalletQuotaLimitExceeded`(develop 이름을 `grep -n ErrWalletQuota model/*.go`로 확인)이고 잔액 불변.
  - export: 비밀번호와 legacy access token이 있는 사용자 2명 → 반환 행의 `Password` 빈 문자열, `AccessToken` nil 또는 빈, id 오름차순.
  - 스키마 가드: SQLite는 skip(nil); 가짜 `Migrator` 대신 team 방식(team 파일 참고)으로 legacy 32-bit 컬럼이면 오류 메시지에 `users.quota`가 들어가고, 64-bit이면 OK, 테이블 없으면 skip, `SKIP_64BIT_QUOTA_SCHEMA_CHECK` 환경 변수로 건너뜀, `is64BitIntegerType` 표(mysql `bigint`/`int`, postgres `bigint`/`integer`/`int8`/`int4`). `dbType`은 `common.DatabaseType` 값이다(`common.DatabaseTypeMySQL` 등 이름을 `grep -n DatabaseType common/*.go`로 확인).
- [ ] **Step 3: 실행.** `GOWORK=off go test ./model/ -count=1` (race는 선택: `-race -run 'Concurrent'`로 이 Task의 동시성 테스트만).
- [ ] **Step 4: 커밋** `test(parity): model quota ceilings, reserve concurrency, export, schema guard` + Ported-From 5줄(3f1cbeeb7, a1c25c3a4, ba6afab57, 4728bdc34).

### Task 3: 구독 플랜 기간·리셋 상한 포팅 (코드 + 테스트)

**Files:**
- Create: `model/subscription_timing_fork.go`, `model/subscription_timing_fork_test.go`
- Modify: `model/subscription.go:309-333`(`calcPlanEndTime`), `model/subscription.go:344-~382`(`calcNextResetTime`), `controller/subscription.go:~202`(AdminCreateSubscriptionPlan)·`~276`(AdminUpdateSubscriptionPlan), `docs/fork-overlays.md`(O11 아래 새 소제목 "### 테스트 동등성 감사 이월 (2026-10-16)" 표)

**team 원본:** `model/subscription.go`의 `ValidateSubscriptionPlanDuration/Reset/Timing`, `calcPlanEndTime`, `calcNextResetTime`(2c619a31e, Toss 커밋 안에 묶여 있음; `git show team:model/subscription.go | sed -n 575,710p`), 테스트 `model/subscription_timing_bounds_test.go`(2c619a31e), controller 훅 `git show team:controller/subscription.go | sed -n 190,200p`.

- [ ] **Step 1: fork 파일.** `model/subscription_timing_fork.go`:
  - 상수 `SubscriptionPlanMaxDurationValue = 1200`, `SubscriptionPlanMaxCustomSeconds int64 = 31_536_000`(조직 플랜 상한 `controller/organization_validation.go:21-22`와 같은 값).
  - `ValidateSubscriptionPlanDuration(plan *SubscriptionPlan) error`, `ValidateSubscriptionPlanReset(plan *SubscriptionPlan) error`, `ValidateSubscriptionPlanTiming(plan *SubscriptionPlan) error` — team 본문 그대로(메시지 포함).
  - `subscriptionEndAfterStart(start, end time.Time) (int64, error)`: `!end.After(start) || end.Unix() <= start.Unix()`면 `errors.New("subscription plan end time overflowed")`.
  - `subscriptionResetAfterBase(base, next time.Time, endUnix int64) int64`: `!next.After(base) || next.Unix() <= base.Unix()`면 0, `endUnix > 0 && next.Unix() > endUnix`면 0, 아니면 `next.Unix()`.
- [ ] **Step 2: 훅.** `calcPlanEndTime`: 기존 `plan == nil`·`DurationValue <= 0` 검사 두 블록을 `if err := ValidateSubscriptionPlanDuration(plan); err != nil { return 0, err } // fork: O11`로 바꾸고, switch는 `end`를 계산만 하게 한 뒤 `return subscriptionEndAfterStart(start, end) // fork: O11`. custom 분기의 `CustomSeconds <= 0` 검사는 validator가 대신하므로 제거(훅 수는 2줄로 유지; switch의 `return` → 대입 변경은 훅 표에 한 행으로 적는다). `calcNextResetTime`: 맨 위 `if plan == nil { return 0 }`를 `if ValidateSubscriptionPlanReset(plan) != nil { return 0 } // fork: O11`로, 끝의 `endUnix` 비교를 `return subscriptionResetAfterBase(base, next, endUnix) // fork: O11`로. controller 두 곳: `QuotaResetCustomSeconds <= 0` 검사 블록 바로 뒤에 `if err := model.ValidateSubscriptionPlanTiming(&req.Plan); err != nil { common.ApiError(c, err); return } // fork: O11`(3줄이지만 한 블록; 표에 한 행).
- [ ] **Step 3: 테스트.** team `subscription_timing_bounds_test.go`의 세 테스트를 옮긴다. 세 번째(`TestCreateUserSubscriptionRejectsInvalidResetTiming`)는 `setupTossBillingModelTestDB` 대신 Task 2의 SQLite 헬퍼(같은 패키지이므로 재사용), `CreateUserSubscriptionFromPlanTx`는 develop에 있다(subscription.go:484). 기존 테스트가 깨지지 않는지 `go test ./model/ ./controller/ -run 'Subscription|Plan' -count=1`.
- [ ] **Step 4: overlay 표.** `docs/fork-overlays.md`의 O11 "team 재현 감사 이월 (2026-10-15)" 표 뒤에 `### 테스트 동등성 감사 이월 (2026-10-16)` 소제목과 같은 4열 표를 만들고 이 Task의 훅(model 4행, controller 2행)을 적는다. 형식은 위 표를 따른다(파일:행, 바꾼 내용, team sha, fork 파일).
- [ ] **Step 5: 실행·커밋.** `GOWORK=off go test ./model/ ./controller/ -count=1`. 커밋 `fix(subscription): bound plan duration and reset timing like team` + `Ported-From: team 2c619a31e`.

### Task 4: relaykit DTO 동작을 호스트 `dto/` 패키지에서 테스트

**Files:**
- Create: `dto/relaykit_image_stream_fork_test.go`, `dto/relaykit_claude_safeguards_fork_test.go`, `dto/relaykit_cache_write_fork_test.go`, `dto/relaykit_responses_codex_fields_fork_test.go`

**team 원본:** `dto/openai_image_test.go`(65b37a1e8), `dto/claude_passthrough_test.go`(66673c63b), `dto/openai_response_cache_write_test.go`(309582c2f), `dto/openai_responses_codex_fields_test.go`(9b4fed08a).

**Interfaces:** 호스트 `dto` 패키지(`dto/channel_constraints.go`)에서 `rdto "github.com/QuantumNous/new-api/relaykit/dto"`로 import. 타입: `rdto.ImageRequest{Stream *bool}`+`IsStream()`, `rdto.ClaudeRequest.Safeguards`, `rdto.InputTokenDetails.CacheCreationTokensTotal()`(openai_response.go:316), `rdto.Usage`, `rdto.OpenAIResponsesRequest{ClientMetadata, Reasoning{Mode, Context}}`(openai_request.go).

- [ ] **Step 1: team 파일 4개를 읽고 develop relaykit 필드 이름을 확인한다**(`grep -n "Safeguards\|ClientMetadata\|Mode \|Context " relaykit/dto/claude.go relaykit/dto/openai_request.go`).
- [ ] **Step 2: 테스트.** `common.Unmarshal`→`common.Marshal` 왕복으로:
  - image: `{"prompt":"x","stream":true}` → `IsStream()` true; `"stream":false` → 마샬 결과에 `"stream":false`가 남는다(Rule 6); stream 없음 → 마샬에 `stream` 키 없음.
  - safeguards: top-level `safeguards` JSON 객체가 왕복 후 바이트 동등(`require.JSONEq`).
  - cache write: 표 — creation만, write만, 둘 다(=max), 없음(0), 음수(0으로 clamp); `prompt_tokens_details.cache_write_tokens`가 `Usage` 언마샬로 들어온다.
  - codex: `client_metadata`와 `reasoning.mode`/`reasoning.context` 왕복 보존.
- [ ] **Step 3: 실행·커밋.** `GOWORK=off go test ./dto/ -count=1`. 커밋 `test(parity): relaykit dto passthrough cases from host package` + Ported-From 4줄.

### Task 5: middleware · relay 공통 테스트

**Files:**
- Create: `middleware/auth_gemini_path_fork_test.go`, `middleware/gzip_zstd_fork_test.go`, `middleware/model_rate_limit_saturation_fork_test.go`, `relay/common/stream_supported_channels_fork_test.go`, `relay/helper/stream_scanner_codex_headers_fork_test.go`, `relay/helper/stream_scanner_write_deadline_fork_test.go`, `relay/relay_task_ratio_saturation_fork_test.go`, `relay/responses_handler_compaction_fork_test.go`

**team 원본:** `middleware/auth_gemini_path_test.go`(2d1169e84), `middleware/gzip_zstd_test.go`(22f50eaf5), `middleware/model_rate_limit_test.go`(3f1cbeeb7), `relay/common/stream_supported_channels_test.go`(2fa3012ec), `relay/helper/stream_scanner_codex_headers_test.go`(9b4fed08a), `relay/helper/stream_scanner_write_deadline_test.go`(fac49f5de), `relay/relay_task_ratios_test.go`(04afc5a18), `relay/responses_handler_compaction_test.go`(c33293ac3).

**Interfaces:** `middleware.TokenAuth()`(auth.go:528; 기존 `middleware/auth_test.go`의 셋업 방식을 따른다), `DecompressRequestMiddleware()`(gzip.go:26; zstd는 `github.com/klauspost/compress/zstd`로 인코딩), `rateLimitDurationSeconds(int) int64`·`rateLimitCapacity(int, int64) int64`(model-rate-limit.go:210,221), `relaycommon.streamSupportedChannels`(relay_info.go:435; `constant.ChannelTypeTencent`), `helper.copyCodexSSEHeaders(c, resp)`·`helper.ExtendWriteDeadline(c)`(stream_scanner.go:55,76), `relay_task.go` 6단계 `ApplyOtherRatiosToFloat`(~325행), `responses_handler.go:23-40` compaction 매핑.

- [ ] **Step 1: team 파일 8개를 읽는다.** 각각 develop의 호출 방식(gin 컨텍스트 셋업, 기존 같은 패키지 테스트 헬퍼)을 확인.
- [ ] **Step 2: 테스트.**
  - TokenAuth: `GET /v1/models?key=sk-x`, `/v1beta/models/x?key=`, `/v1beta/openai/models?key=`는 `Authorization: Bearer sk-x`로 승격(이후 토큰 조회가 실패해도 헤더 승격 자체를 검증하는 team 방식 유지); `/v1/chat/completions?key=`는 승격되지 않음.
  - zstd: 본문을 zstd로 압축해 `Content-Encoding: zstd`로 보내면 핸들러가 평문을 읽고 `Content-Encoding` 헤더가 비어 있다.
  - rate limit: `TestRateLimitDurationSecondsSaturates`(0, 1, 큰 값 → `math.MaxInt64/60`… team 표 그대로), `TestRateLimitCapacityDoesNotWrap`.
  - `streamSupportedChannels[constant.ChannelTypeTencent]` true.
  - codex headers: upstream 응답의 `X-Reasoning-Included`·`X-Codex-Turn-State`만 복사되고 다른 헤더는 안 됨; `resp == nil`·헤더 nil이어도 panic 없음.
  - `ExtendWriteDeadline(nil)`과 `httptest.NewRecorder` 기반 컨텍스트에서 panic 없음.
  - task ratio: `PriceData`에 other ratio 1e10을 넣고 relay_task 6단계 계산을 호출하면 quota가 `math.MaxInt32`로 포화(team 테스트의 호출 지점을 그대로; develop에서 함수 이름이 다르면 `grep -n ApplyOtherRatiosToFloat relay/relay_task.go`).
  - compaction: `OpenAIResponsesCompactionRequest` → upstream 본문에 `prompt_cache_key`, `prompt_cache_retention`, `service_tier`, `parallel_tool_calls`가 있고 `tools`·`reasoning`·`text` 키가 없다.
- [ ] **Step 3: 실행·커밋.** `GOWORK=off go test ./middleware/ ./relay/ ./relay/common/ ./relay/helper/ -count=1`. 커밋 `test(parity): middleware and relay common cases from team` + Ported-From 8줄.

### Task 6: relay 채널 어댑터 테스트

**Files:**
- Create: `relay/channel/aws/format_request_zero_values_fork_test.go`, `relay/channel/claude/effort_table_fork_test.go`, `relay/channel/claude/openai_to_claude_thinking_fork_test.go`, `relay/channel/deepseek/adaptor_responses_fork_test.go`, `relay/channel/dify/relay_dify_remote_image_fork_test.go`, `relay/channel/ollama/relay_ollama_request_fork_test.go`, `relay/channel/openai/adaptor_realtime_beta_fork_test.go`
- Modify: `relay/channel/openai/image_stream_error_fork_test.go`(fork 파일이므로 케이스 추가 가능), `relay/claude_thinking_display_fork_test.go`(선택 케이스 1개)

**team 원본:** `relay/channel/aws/format_request_test.go`(66f767874), `relay/channel/claude/effort_test.go`(fb6c2c970), `relay/channel/claude/relay_claude_thinking_test.go`(9088a5703), `relay/channel/deepseek/adaptor_responses_test.go`(aaf9a515f), `relay/channel/dify/relay_dify_remote_image_test.go`(571e73d75), `relay/channel/ollama/relay_ollama_request_test.go`(73b87c518), `relay/channel/openai/adaptor_realtime_beta_test.go`(0ab6532c3), `relay/channel/openai/image_stream_error_event_test.go`(b5d733722), `relay/claude_thinking_test.go`(9088a5703).

**Interfaces:** `aws.formatRequest(io.Reader, http.Header) (*AwsClaudeRequest, error)`(dto.go:34); relaykit `reasoning.ClaudeEffort(model string, effort Effort) Effort`·`ClaudeDefaultEffort(model) Effort`·`RenderClaude(...)`(relaykit/relayconvert/reasoning/claude.go:115,422,429 — import 경로 `github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning`); `relayconvert.OpenAIChatRequestToClaudeMessages(ctx, convmeta.Meta, dto.GeneralOpenAIRequest)`(relaykit/relayconvert/request_compat.go:20 — 기존 develop 테스트에서 `convmeta.Meta`를 만드는 예를 `grep -rn "convmeta.Meta{" relay/channel/claude/*_test.go`로 찾는다); `deepseek.applyDeepSeekV4ResponsesThinkingSuffix`(adaptor.go:175)와 `Adaptor.GetRequestURL/ConvertOpenAIResponsesRequest`; `dify.requestOpenAI2Dify`(relay-dify.go:127); `ollama.openAIChatToOllamaChat`(relay-ollama.go:42); openai `Adaptor.SetupRequestHeader`(adaptor.go:215 `legacyRealtimeBeta` 지역 변수); `isOpenAIImageStreamErrorEvent(data []byte) bool`(relay_image.go:215).

- [ ] **Step 1: team 파일 9개를 읽는다.** team의 Claude thinking 테스트 중 relaykit 변환이 아니라 team 전용 함수(`UsageSemantic`, `normalizeClaudeThinking` 등 develop에 없는 것)를 부르는 케이스는 건너뛰고 ledger에 적는다.
- [ ] **Step 2: 테스트.**
  - AWS: 본문에 `max_tokens:0`, `top_p:0`, `top_k:0`을 명시하면 결과 포인터가 non-nil이고 값 0; 생략하면 nil.
  - Claude effort 표: 4.5는 high로 cap, 4.6 xhigh→max, sonnet-4-5 none, claude-5 xhigh 유지, 알 수 없는 모델 그대로; `ClaudeDefaultEffort`; thinking off일 때 cap(team 표 그대로, 기대값은 develop relaykit 동작에 맞춰 돌려서 확인하고 다르면 ledger에 차이를 적는다 — relaykit 동작이 team과 달라도 relaykit은 고치지 않는다).
  - OpenAI→Claude: effort 접미사가 모델 상한으로 cap, Claude 5 `reasoning_effort`→adaptive thinking, manual effort는 budget 유지, OpenRouter `reasoning.max_tokens`/`effort`, effort none, strict sampling(temperature/top_p 동시 지정 처리), `RenderClaude` 케이스.
  - DeepSeek: `GetRequestURL`이 Responses 모드에서 `/responses`; `ConvertOpenAIResponsesRequest` passthrough; `deepseek-v4-max`/`-none` 접미사 → `reasoning.effort`와 `info.UpstreamModelName` 접미사 제거.
  - Dify: 원격 `image_url` 메시지로 `requestOpenAI2Dify`가 panic 없이 `Files[0].TransferMode == "remote_url"`, `URL` 설정.
  - Ollama: `reasoning_effort: high` → `think: "high"`, `none` → `false`; assistant `reasoning_content` → `thinking`; tool 메시지의 `tool_call_id`와 `tool_name` backfill.
  - OpenAI realtime: `gpt-4o-realtime-preview`는 websocket subprotocol `openai-beta.realtime-v1`과 HTTP `openai-beta: realtime=v1`; `gpt-realtime`은 둘 다 없음.
  - 이미지 스트림 오류 이벤트: 잘못된 JSON은 오류 아님, `"type":"error"`는 오류 — 기존 fork 테스트 파일에 케이스 추가.
  - 선택: `relay/claude_thinking_display_fork_test.go`에 `claude-opus-4-7-high` 모델 + thinking block binding 케이스 1개.
- [ ] **Step 3: 실행·커밋.** `GOWORK=off go test ./relay/... -count=1`. 커밋 `test(parity): channel adaptor cases from team` + Ported-From 9줄.

### Task 7: Claude beta 헤더 trim과 OpenAI tool-call usage fallback 포팅 (코드 + 테스트)

**Files:**
- Create: `relay/channel/claude/beta_header_fork.go`, `relay/channel/claude/beta_header_fork_test.go`, `relay/channel/openai/usage_fallback_fork.go`, `relay/channel/openai/usage_fallback_fork_test.go`
- Modify: `relay/channel/claude/adaptor.go:110`, `relay/channel/openai/relay-openai.go:~312`, `docs/fork-overlays.md`(Task 3의 2026-10-16 표에 2행)

**team 원본:** `git show team:relay/channel/claude/adaptor.go | sed -n 74,90p`와 `relay/channel/claude/adaptor_test.go`(90874460e)의 `TrimsAndDropsEmpty`·`KeepsSingle`; `git show team:relay/channel/openai/relay-openai.go | sed -n 242,258p`와 `relay/channel/openai/relay_openai_tool_call_usage_test.go`(c0b313844).

- [ ] **Step 1: Claude.** `beta_header_fork.go`에 `func joinAnthropicBetaValues(values []string) string` — 각 값을 `,`로 나눠 `strings.TrimSpace`, 빈 항목 제거, `,`로 join. `adaptor.go:110`의 `anthropicBeta := strings.Join(c.Request.Header.Values("anthropic-beta"), ",")`를 `anthropicBeta := joinAnthropicBetaValues(c.Request.Header.Values("anthropic-beta")) // fork: O11`로. 테스트: 단위(`[" a , ,b", "c "]` → `a,b,c`; 빈 입력 → ``)와 team의 두 `SetupRequestHeader` 테스트(기존 `TestSetupRequestHeaderForwardsEveryAnthropicBetaValue`의 셋업 방식을 따른다).
- [ ] **Step 2: OpenAI.** `usage_fallback_fork.go`에 `func fallbackCompletionText(msg *dto.Message) string` — `StringContent()` + `GetReasoningContent()` + 각 `ParseToolCalls()` 항목의 `Custom`(있으면 raw bytes) 또는 `Function.Name`+`Function.Arguments`. `relay-openai.go`의 `ctkm := tokenkit.Count(info.UpstreamModelName, choice.Message.StringContent()+choice.Message.GetReasoningContent())`를 `ctkm := tokenkit.Count(info.UpstreamModelName, fallbackCompletionText(&choice.Message)) // fork: O11`로(`choice`가 range 복사본이면 주소를 넘겨도 된다; 포인터 메서드 여부는 `grep -n "func (m \*Message) ParseToolCalls" relaykit/dto/openai_request.go`). 테스트: usage 없는 비스트림 응답에 tool_calls만 있으면 `CompletionTokens == tokenkit.Count(model, name+args)` (team 테스트가 `OpenaiHandler`를 직접 부르는 방식을 따른다; 기존 `relay/channel/openai/*_test.go`에서 `OpenaiHandler` 호출 예를 찾는다). `fallbackCompletionText` 단위 테스트도 1개(텍스트+tool 2개, custom 1개).
- [ ] **Step 3: overlay 표에 2행.** 파일:행, 바꾼 한 줄, team sha, fork 파일.
- [ ] **Step 4: 실행·커밋.** `GOWORK=off go test ./relay/channel/claude/ ./relay/channel/openai/ -count=1`. 커밋 `fix(relay): trim anthropic-beta values; count tool calls in usage fallback` + `Ported-From: team 90874460e`, `Ported-From: team c0b313844`.

### Task 8: service 테스트

**Files:**
- Create: `service/http_client_response_header_timeout_fork_test.go`, `service/quota_preconsume_concurrency_fork_test.go`, `service/tiered_settle_noop_fork_test.go`, `service/token_counter_estimate_fork_test.go`, `service/testdata/claude_code_request.json`(team 파일 그대로 `git show team:service/testdata/claude_code_request.json > …`, 38,747바이트)

**team 원본:** `service/http_client_response_header_timeout_test.go`(868709e9a), `service/quota_preconsume_test.go`(a1c25c3a4), `service/tiered_settle_group_retry_test.go`(147d3ad03), `service/token_counter_estimate_test.go`(c0b313844).

**Interfaces:** develop에는 `newRelayTransport`가 없다 — `newRelayHTTPTransport() *http.Transport`(http_client.go:79)가 `common.RelayResponseHeaderTimeout`을 적용한다(108-115행; 상한 상수 이름은 그 줄에서 확인). 프록시 경로는 `newTransportFactory(proxyURL, tlsConfig)`(313행)와 `newHTTPClientFromPolicy`(335행). `PreConsumeTokenQuota(relayInfo *relaycommon.RelayInfo, quota int) error`(quota.go:412). `PrepareTieredBillingForSelectedGroup`(tiered_settle.go:168), `refreshTieredBillingGroup(relayInfo) (*billingexpr.…, error)`(140행). `EstimateRequestToken(c, meta *types.TokenCountMeta, info)`(token_counter.go:48).

- [ ] **Step 1: team 파일 4개를 읽는다.** `newRelayTransport` → `newRelayHTTPTransport`; `maxTimeoutSeconds` → develop 상수.
- [ ] **Step 2: 테스트.**
  - transport: `RelayResponseHeaderTimeout` 1 → 1s, 0 → 0, -5 → 0, `math.MaxInt` → 상한; 기존 idle 설정 유지; 프록시 transport(`http://`·`socks5://` 더미 URL)에도 적용; 헤더를 300ms 동안 보내지 않는 `httptest.Server`에 timeout 0.1s(정수 초 단위라면 1초로 하고 서버는 1.5초 지연 — 테스트 총 시간 2초 이내)로 요청하면 오류; 헤더는 바로 주고 본문만 느린 서버는 성공.
  - preconsume 동시성: `model` 패키지의 SQLite+Redis-off 셋업으로 토큰 잔액 100, `RelayInfo` 10개가 각 30 → 성공 3; `unlimited` 토큰은 항상 성공; 음수 quota 오류. 기존 `service/*_test.go`에서 `RelayInfo`와 사용자·토큰을 만드는 헬퍼를 재사용한다.
  - tiered no-op: non-tiered `RelayInfo`에 `PrepareTieredBillingForSelectedGroup`을 불러도 `PriceData` 불변; `refreshTieredBillingGroup`이 non-tiered면 nil, 그룹이 안 바뀌면 no-op.
  - estimate: fixture를 `GeneralOpenAIRequest`/Claude 요청으로 읽어 `EstimateRequestToken` 결과가 team 테스트의 하한·상한 안에 있고, tool 정의를 제거한 요청보다 크다(tool 정의·tool_use·tool_result가 집계됨).
- [ ] **Step 3: 실행·커밋.** `GOWORK=off go test ./service/ -count=1`. 커밋 `test(parity): service transport timeout, preconsume concurrency, tiered no-op, token estimate` + Ported-From 4줄.

### Task 9: controller 상한 테스트

**Files:**
- Create: `controller/quota_ceiling_fork_test.go`, `controller/topup_quota_limit_fork_test.go`

**team 원본:** `controller/quota_ceiling_test.go`(3f1cbeeb7), `controller/topup_quota_limit_test.go`(1fa9b3f1b)의 `TestGetMaxTopUpAmountInTokenMode`(117행)와 `TestPayEndpointsRejectTopUpThatWouldOverflowWallet`(221행). develop의 같은 이름 upstream 테스트 6개는 그대로 둔다.

**Interfaces:** `maxTokenQuota() int`(token.go:47), `getMinTopup() int64`(topup.go:182), `getMaxTopUpAmount() int64`(topup.go:207), `settleTestQuota(info, priceData, …)`(channel-test.go:540), `AddRedemption`/`UpdateRedemption` 핸들러(redemption.go). SQLite 헬퍼는 `controller/user_manage_test.go:setupManageUserTestDB` 패턴.

- [ ] **Step 1: team 파일 2개를 읽는다.** Toss 엔드포인트 케이스는 건너뛴다(스펙상 삭제).
- [ ] **Step 2: 테스트.**
  - `maxTokenQuota`·`getMinTopup`이 설정값이 커도 `MaxWalletQuota`에서 포화; `settleTestQuota` non-tiered 경로에서 큰 ratio에도 포화; `AddRedemption`/`UpdateRedemption`이 `MaxWalletQuota+1`을 거부(JSON 응답 `success:false`).
  - `TestGetMaxTopUpAmountInTokenMode`: 토큰 모드에서 `getMaxTopUpAmount`가 team 기대값.
  - 결제 엔드포인트 매트릭스: epay, stripe, stripe-amount, creem, waffo, waffo-pancake 각각 사용자 지갑이 넘치는 금액에 거부(team 테스트의 요청 본문과 응답 단언 그대로; develop 라우트 핸들러 이름은 `grep -n "func Request" controller/topup*.go controller/*pay*.go`).
- [ ] **Step 3: 실행·커밋.** `GOWORK=off go test ./controller/ -count=1`. 커밋 `test(parity): controller quota ceilings and pay endpoint matrix` + Ported-From 2줄.

### Task 10: 문서 — 조직 테스트 가이드 갱신, e2e 분석 문서, 전체 게이트

**Files:**
- Modify: `docs/manuals/organization-test-guide.md`(4, 124, 155, 448, 721, 844, 910행 부근), `docs/2026-10-16-team-test-parity-audit.md`(§5에 결과 요약 한 단락)
- Create: `web/e2e/E2E_TEST_ANALYSIS.md`

**team 원본:** `docs/manuals/organization-test-guide.md`(1b5efb24e), `web/default/e2e/E2E_TEST_ANALYSIS.md`(e152b3ea7; team 경로는 `web/default/e2e/`, develop은 `web/e2e/`).

- [ ] **Step 1: 조직 테스트 가이드.** 시나리오 N(한국어 UI)의 "develop에서는 미실행/단계 D" 표기를 지우고 `web/src/i18n/__tests__/languages.test.ts`(존재 확인)와 `web/e2e/korean.e2e.ts`가 자동으로 덮는다고 적는다. e2e 개수 19 → 20+1(suspend/resume 시나리오와 korean.e2e.ts 추가). 언어 코드 `kr` → `ko`. 다른 내용은 바꾸지 않는다.
- [ ] **Step 2: e2e 분석 문서.** team 문서의 4절(커버리지 매트릭스), 5절(역할-권한 매트릭스), 8절(갭)을 `web/e2e/organization.e2e.ts`·`korean.e2e.ts`의 실제 테스트 이름 기준으로 다시 써서 `web/e2e/E2E_TEST_ANALYSIS.md`에 둔다(20+1개, localStorage 인증 없음, 조회-추가 플로우). `web/e2e/README.md`에 링크 한 줄.
- [ ] **Step 3: 전체 게이트.** `GOWORK=off go test ./... -count=1`과 `gofmt -l $(git diff --name-only 0fd699b9e -- '*.go')`가 비어 있음. 실패가 이 계획 밖의 기존 실패(ledger에 적힌 `-race` 전용 실패 등)인지 구분해 보고한다.
- [ ] **Step 4: 감사 문서 §5에 결과 요약**(추가한 테스트 파일 수·테스트 함수 수, 코드 포팅 3건 커밋 sha)을 적고 커밋 `docs: refresh organization test guide and e2e analysis; close test-parity audit` + `Ported-From: team 1b5efb24e`, `Ported-From: team e152b3ea7`.
