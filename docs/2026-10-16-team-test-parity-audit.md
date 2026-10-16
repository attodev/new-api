# team 테스트 동등성 감사 (2026-10-16)

대상: `develop` `0fd699b9e` vs `team` `9ef93eb8c`. 질문: team에서 추가·수정한 테스트가 develop에 비슷한 수준으로 있는가. 행 단위 결과는 `docs/reviews/2026-10-16-team-test-parity-audit.tsv`(234행). 기능 포팅 감사(`docs/2026-10-15-team-parity-audit.md`)와 짝을 이룬다.

## 1. 범위와 방법

- team이 upstream 기준점 이후 추가한 Go 테스트 파일 181개(테스트 함수 1,368개), 수정한 upstream 테스트 파일 16개, web/e2e 파일 28개, 테스트 문서 25개.
- develop 쪽 재고: Go 테스트 파일 375개(기준점 이후 추가 85개, 그중 `*_fork_test.go` 34개), 웹 테스트 229개(추가 40개), e2e 20+1개.
- 세 묶음(Go A 70파일, Go B 111파일, 웹·e2e·문서 53파일)을 각각 같은 기준표로 분류: `covered`(develop에 같은 동작의 테스트가 있음), `covered-upstream`(upstream 테스트가 같은 동작을 덮음), `partial`(일부 케이스만), `missing`(동작은 있는데 테스트가 없음), `n/a-dropped`(Toss·언어 등 스펙상 버린 기능), `n/a-superseded`(upstream 구조 변경으로 의미 없음).

## 2. 결과

| 분류 | 행 |
|---|---|
| covered | 58 |
| covered-upstream | 14 |
| partial | 17 |
| missing | 27 |
| n/a-dropped (Toss 결제·다국어 등) | 99 |
| n/a-superseded | 19 |

웹 단위 테스트와 e2e 시나리오는 빠진 것이 없다(조직 e2e 20개 + 한국어 1개, team의 19개를 넘는다). 빠진 것은 모두 Go 테스트 26건·부분 15건과 문서 2건이다. 그중 셋은 테스트만이 아니라 team에 있던 코드 자체가 develop에 없다(§4).

## 3. 빠진 테스트 (패키지별)

괄호는 team 테스트 파일. `missing`은 develop에 동작은 있으나 테스트가 없는 것, `partial`은 케이스 일부가 빠진 것.

### 3.1 common · types · setting
- `common.ValidateWalletQuota` 경계(상한 OK, 상한+1 오류 메시지) — partial (`common/quota_math_test.go`)
- `types.AddOtherRatio`/`ReplaceOtherRatios`의 NaN·±Inf 거부 — missing (`types/price_data_other_ratio_test.go`)
- `IsGeminiModelSupportImagine`의 gemini-3 이미지 모델 — missing (`setting/model_setting/gemini_imagine_test.go`)
- pre-consume 설정 기본값·키 검증·런타임 값(0, 음수, NaN, ±Inf) 거부 — partial (`setting/operation_setting/quota_setting_test.go`)
- `CheckModelRequestRateLimitGroup` 상한·메시지·JSON 왕복 — missing (`setting/rate_limit_test.go`)
- gpt-5.6 계열 cache/model ratio 명시값과 completion ratio unlocked — missing (`setting/ratio_setting/{cache_ratio,completion_ratio}_gpt56_test.go`)

### 3.2 model
- 교환 코드 정확히 상한까지 성공 — partial (`model/redemption_test.go`)
- `calcSubscriptionBalanceQuota` 상한(정확히 OK, +1 오류, 1e30 오류) — missing (`model/subscription_balance_quota_test.go`)
- `TryReserveTokenQuota`/`TryReserveUserQuota` 동시 과소비 불가(잔액 100, 30×10 → 3건만 성공), 음수 거부; `IncreaseUserQuota` 상한 — partial (`model/{token,user}_quota_reserve_test.go`)
- `GetAllUsersForExport`가 비밀번호·access token을 비움 — missing (`model/user_export_test.go`; team에서는 `integration` 태그라 CI에서 돌지 않았음)
- `ensureUserQuotaColumns`/`is64BitIntegerType` 64-bit 스키마 가드 — missing (`model/user_quota_schema_test.go`)

### 3.3 relaykit DTO 동작 (호스트 패키지에서 테스트)
relaykit은 upstream 모듈이라 손대지 않는다. 아래는 호스트 `dto/` 패키지에 fork 테스트로 둔다.
- `ImageRequest` JSON `stream:true/false` 보존(Rule 6) — missing
- `ClaudeRequest.Safeguards` 왕복 — partial
- `InputTokenDetails.CacheCreationTokensTotal` 표와 `cache_write_tokens` 언마샬 — partial
- Responses `client_metadata`·`reasoning.mode/context` 왕복 — missing

### 3.4 middleware · relay 공통
- `TokenAuth`가 Gemini 경로의 `?key=`를 Authorization으로 올림 — missing (`middleware/auth_gemini_path_test.go`)
- zstd 요청 본문 해제 — missing (`middleware/gzip_zstd_test.go`)
- 모델 rate-limit 기간·용량 포화 — missing (`middleware/model_rate_limit_test.go`)
- `streamSupportedChannels[Tencent]` — missing; `copyCodexSSEHeaders`; `ExtendWriteDeadline` nil 안전 — missing (`relay/common`, `relay/helper`)
- task 과금 ratio 1e10 포화 — partial (`relay/relay_task_ratios_test.go`)
- Responses compaction 요청 매핑 — missing (`relay/responses_handler_compaction_test.go`)

### 3.5 relay 채널
- AWS `formatRequest` 명시적 0 보존 — missing
- Claude: `ClaudeEffort` 모델 표·기본 effort(relaykit reasoning, 호스트에서 테스트) — missing; OpenAI→Claude 변환의 effort 접미사·Claude 5 adaptive·OpenRouter·strict sampling — partial; `anthropic-beta` trim(§4) — partial
- DeepSeek Responses URL·V4 `-max/-none` 접미사 — missing
- Dify 원격 이미지 — missing; Ollama 요청 변환(effort, thinking, tool backfill) — missing
- OpenAI realtime beta 헤더(`*-realtime-preview`만) — missing; 이미지 스트림 오류 이벤트 추가 케이스 — partial; tool-call usage fallback(§4) — missing
- 선택: native Claude display의 effort 접미사+block binding 케이스 — partial

### 3.6 service · controller
- `newRelayTransport`의 `ResponseHeaderTimeout` 적용·프록시·느린 본문 허용 — missing (`service/http_client_response_header_timeout_test.go`)
- `PreConsumeTokenQuota` 동시 과소비 불가·무제한 토큰·음수 거부 — partial
- tiered 정산의 non-tiered no-op — partial
- `EstimateRequestToken`의 Claude Code 요청 fixture(tool 정의·tool_use·tool_result 집계) — missing (`service/testdata/claude_code_request.json`)
- controller 상한: `maxTokenQuota`, `getMinTopup`, `settleTestQuota`, 교환 코드 추가/수정 — partial; 토큰 모드 최대 충전액과 결제 엔드포인트 6종의 지갑 용량 거부 — partial

### 3.7 문서
- `docs/manuals/organization-test-guide.md`: 시나리오 N(한국어 UI)이 "develop에서는 미실행(단계 D)"으로 남아 있으나 단계 D는 끝났다. e2e 수가 19로 적혀 있다(실제 20+1). suspend/resume 시나리오와 `korean.e2e.ts`가 빠졌다. 언어 코드 `kr`→`ko`.
- `web/e2e/E2E_TEST_ANALYSIS.md`(team): 커버리지 매트릭스·역할-권한 매트릭스·갭 목록. develop의 `web/e2e/README.md`에는 테스트별 설명만 있다.

## 4. 코드가 빠진 항목 (결정 사항)

사용자가 자리를 비운 상태라 아래 셋은 판정하고 진행한다. 되돌리기 쉬운 작은 변경이고, 셋 다 team 운영 코드와 같아진다. 다른 결정을 원하면 해당 커밋만 되돌리면 된다.

| # | 항목 | develop 상태 | 판정 | 틀렸을 때 비용 |
|---|---|---|---|---|
| 1 | `anthropic-beta` 헤더 값의 공백 제거·빈 항목 제거 (team 90874460e) | 줄을 그대로 `,`로 이어 붙임. `" a , ,b"` 같은 값이 그대로 전달 | **반영.** `relay/channel/claude/beta_header_fork.go`에 `joinAnthropicBetaValues`, upstream `CommonClaudeHeadersOperation`에 한 줄 훅 | 없음(값의 공백·빈 항목만 달라짐) |
| 2 | OpenAI 비스트림 응답에 usage가 없을 때 fallback 토큰 수에 tool call 이름·인자 포함 (team c0b313844) | 본문 텍스트+reasoning만 센다. 에이전트 턴은 출력 대부분이 tool call이라 과소 청구 | **반영.** `relay/channel/openai/usage_fallback_fork.go`에 `fallbackCompletionText`, `OpenaiHandler`에 한 줄 훅 | usage를 주지 않는 드문 upstream에서 team과 같은(더 큰) 청구. 되돌리면 upstream 동작 |
| 3 | 구독 플랜 기간·리셋 주기 상한(1200 단위, 31,536,000초)과 시간 오버플로 검사 (team 2c619a31e, Toss 커밋에 묶여 있었음) | `time.Duration(customSeconds)*time.Second`가 큰 값에서 넘쳐 종료 시각이 과거가 될 수 있음. 조직 플랜에는 같은 상한이 이미 있음(`controller/organization_validation.go`) | **반영.** `model/subscription_timing_fork.go`에 상한 상수·`ValidateSubscriptionPlanTiming`·오버플로 검사, `calcPlanEndTime`/`calcNextResetTime`/관리자 플랜 생성·수정에 훅 | 1200년(단위 기준) 넘는 플랜은 거부된다. 운영에 그런 플랜은 없음 |

반영하지 않는 것: team의 `ErrQuotaCachePending` 차단 테스트(O12 결정으로 비차단), API 키 캐시 동기 델타(사용자 보류), Toss·다국어 테스트 99건(스펙상 삭제).

## 5. 실행

계획: `docs/superpowers/plans/2026-10-16-test-parity-additions.md`. 원칙: upstream `_test.go`는 고치지 않고 새 `*_fork_test.go`로 추가(다음 upstream 동기화 때 충돌을 피함), relaykit 동작은 호스트 패키지에서 테스트, 코드 포팅 셋은 `// fork: O11` 훅 + fork 파일 + `docs/fork-overlays.md` 행.
