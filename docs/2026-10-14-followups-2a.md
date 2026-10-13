# 2-A 후속 수정 기록 — Claude 5.x thinking / Epay 정산 / Gemini image 토큰 외 (2026-10-14)

작성일: 2026-10-14

이 문서는 `codex/followups-2a-2026-10` 브랜치(분기 시점 team `57a2e8f9a`)에서 수행한
후속 수정 8건의 변경 내역, 운영상 바뀌는 동작, 검증 결과, 보류 항목을 기록한다.
8건은 `docs/2026-10-13-upstream-sync-phase2.md` 의 보류 TODO 와 1단계 기록
(`docs/2026-10-13-upstream-sync-phase1.md`)의 후속 후보 중 "실제 동작 문제"로 분류한
것이다. 계획은 `docs/superpowers/plans/2026-10-14-followups-2a.md` 이며, 이 기록의
운영 전제는 1, 2단계 기록의 운영 전제에 더해서 읽는다.

## 검토 범위

- 대상: (1) Claude 5.x / Opus 4.7, 4.8 이 거부하는 `thinking.type:"enabled"` 와 sampling
  파라미터, (2) Epay 콜백의 success 선기록 후 credit 실패, (3) async task polling 의
  201/202 거부, (4) Gemini prompt IMAGE/VIDEO/DOCUMENT modality 누락, (5) 관리자 preset
  threshold 상한 부재, (6) 조직 자동 충전 실패 로그의 target, (7) Ali 매핑 배포명의
  `thinking_budget` 제거, (8) tokenkit codec 첫 호출 지연.
- 방식: upstream 에 대응 커밋이 있는 항목(Task 1)은 그 설계를 따랐고, 나머지는 fork 고유
  수정이다. relaykit 은 도입하지 않았다. 프런트엔드 변경은 없다.
- 범위: 커밋 범위 `team..HEAD` 중 `aca3238e7`(계획 문서)를 제외한 8개 커밋
  (`05bdb2596..338576ab2`). 이 기록과 전체 검증은 Task 9 다.

## 반영 커밋

| Task | fork 커밋 | upstream | 동작 요약 |
| --- | --- | --- | --- |
| 1 | `05bdb2596` | `036ba376d` (effort 표의 출처) | Claude effort 표에 adaptive / manual / strict sampling / disable / default effort 열을 추가하고, 모든 thinking 분기(effort suffix, `-thinking`, `reasoning_effort`, openrouter `reasoning`, native `enabled`/`disabled`)를 모델 capability 로 렌더링. 5.x 에 `enabled`+`budget_tokens` 를 더 이상 보내지 않는다 |
| 2 | `7ab5a8aa9` | 없음 (Stripe `Recharge` 경로와 같은 구조) | `model.RechargeEpay`: 상태 변경, 결제수단 갱신, credit 을 한 transaction 으로 처리. credit 이 실패하면 롤백되어 주문은 pending 으로 남는다 |
| 3 | `1dd9351f6` | 없음 (1단계 `d61d6be75` 의 polling 쪽 짝) | async task polling 이 2xx 전부를 정상 응답으로 처리(201, 202 포함). 4xx/5xx 는 기존대로 오류 |
| 4 | `af1eddc8e` | 없음 | Gemini prompt/tool-use usage 의 IMAGE 는 `ImageTokens`, VIDEO/DOCUMENT/TEXT 는 `TextTokens`, AUDIO 는 `AudioTokens` 로 귀속. cached 와 겹치는 image 는 상한으로 자른다 |
| 5 | `4e172773a` | 없음 | 자동 충전 preset 생성/수정 시 threshold 가 `MaxQuota - 1회 충전량` 을 넘으면 거부 |
| 6 | `3473f535a` | 없음 | 자동 충전 수용량 실패 로그가 실제 target(`organization`/`user` + id)을 기록 |
| 7 | `70b14d049` | 없음 (1단계 Task 18 `66ee6b8f9` 의 보완) | Ali `thinking_budget` 을 클라이언트 모델명과 upstream 모델명 중 하나라도 Qwen 계열이면 유지 |
| 8 | `338576ab2` | 없음 | 서버 시작 시 OpenAI codec(`o200k_base`, `cl100k_base`)을 백그라운드에서 미리 생성 |
| 9 | 이 문서와 같은 커밋 (`fix(relay): cap Gemini image tokens by audio too, ...`) | 없음 | 최종 리뷰 수정: Gemini image 상한에 audio 포함, Epay 정산 실패 시 `fail` 응답으로 재시도, thinking 끄기 변환 시 `display:"omitted"`, 성공 로그에 정산된 주문 기록 |

모든 커밋의 `Co-Authored-By` 는 `Claude Fable 5.1 <noreply@anthropic.com>` 다. Task 1 은 task
리뷰 수정 라운드 1회(amend), Task 2, 4, 7 도 각각 1회가 있었고 위 해시는 최종 해시다.

## 반영 후 동작

### Task 1: Claude 5.x thinking / effort 렌더링 (`relay/channel/claude/effort.go`)

Claude 5.x 와 Opus 4.7/4.8 은 `thinking.type:"enabled"`+`budget_tokens` 와 기본값이 아닌
temperature/top_p/top_k 를 거부한다(400). effort 표(`claudeEffortTable`)는 upstream
`claudeCapabilitiesFor`(`relaykit/relayconvert/reasoning/claude.go`)의 열을 그대로 가져온다.
접두사 순서대로 처음 맞는 행이 쓰이고, 표에 없는 모델은 기존 동작을 유지한다(manual 가능,
sampling 제한 없음, disable 가능, default effort high).

capability 표 (adaptive 는 모두 yes 이며 effort/xhigh/max 허용 범위는 1단계 기록의 표를 따른다):

| 모델 접두사 | manual (`enabled`+budget) | strict sampling | disable | default effort |
| --- | --- | --- | --- | --- |
| `claude-fable-5`, `claude-mythos-5` | 불가 | 예 | 불가 (adaptive + effort low) | high |
| `claude-mythos-preview` | 가능 (upstream 이 해제하지 않음) | 예 | 불가 (adaptive + effort low) | high |
| `claude-opus-5-5` | 불가 | 예 | 불가 (adaptive + effort low) | medium |
| `claude-sonnet-5-5` | 불가 | 예 | 불가, `between_tools` 로 끈다 | high |
| `claude-opus-5`, `claude-sonnet-5`, `claude-opus-4-8`, `claude-opus-4-7` | 불가 | 예 | 가능 | high |
| `claude-haiku-5` | 불가 | 예 | 가능 | medium |
| `claude-opus-4-6`, `claude-sonnet-4-6` | 가능 | 아니오 | 가능 | high |
| `claude-opus-4-5` | 가능 (adaptive 불가) | 아니오 | 가능 | high |
| 그 외 4.x, 3.7 sonnet, 표에 없는 모델 | 가능 | 아니오 | 가능 | high |

"disable 불가" 모델에 "thinking 끄기" 요청이 오면(native `thinking.type:"disabled"`,
OpenAI `reasoning_effort:"none"`, openrouter `effort:"none"`) Sonnet 5.5 는
`{"type":"between_tools"}`(다른 thinking 필드 없음, 기존 effort 는 `high` 로 cap), 나머지
disable 불가 모델은 adaptive + `output_config.effort:"low"` 로 바꾼다. 이 adaptive 는
클라이언트가 thinking 을 원하지 않았으므로 `display:"omitted"` 로 렌더링해 요약을 노출하지
않는다(upstream 은 display 를 비워 두지만, 비워 두면 `NormalizeAdaptiveDisplay` 가
`"summarized"` 로 채운다). 이 경로에서만 적용되고 block_binding 은 유지한다. enabled→adaptive
변환 등 다른 경로는 그대로 `summarized` 다. disable 가능 모델은 요청을 그대로 둔다.

경로별 매핑 (`RenderClaudeThinking(req, model, effort, budget)` 이 단일 지점이다. manual 가능
모델에 budget 이 있으면 `{enabled, budget_tokens}`, 아니면 adaptive + `output_config.effort`):

| 경로 | manual 가능 모델 | manual 불가 모델 |
| --- | --- | --- |
| effort suffix (`-high` 등) | adaptive 를 지원하는 모델만 확장 (opus-4-6, sonnet-4-6 추가, 모든 5.x, fable, mythos). 이전의 opus-4-6/7/8 한정 조건을 표 기반으로 일반화. `claude-opus-4-5-high` 는 adaptive 불가라 확장하지 않는다 (기존과 같음). temperature 1.0, top_p 제거 | adaptive + 해당 effort |
| `-thinking` suffix | 기존과 같다: `enabled`, budget = `max_tokens` 의 80%, max_tokens ≥ 1280, temperature 1.0, top_p 제거 | adaptive + 모델의 default effort |
| `reasoning_effort` (low/medium/high/xhigh/max/minimal) | 기존과 같다: budget 1280/2048/4096 | adaptive + 해당 effort. 소문자로 정규화. `none` 은 위의 disable 규칙 |
| openrouter `reasoning` | `max_tokens` 가 있으면 budget 으로 `enabled`. `effort` 만 있는 요청은 건드리지 않는다 | `max_tokens` 가 있으면 adaptive + `EffortFromBudget` (0 none, ≤1024 low, ≤8192 medium, 그 위 high). 없고 `effort` 가 있으면 `reasoning_effort` 와 같은 규칙 |
| native `thinking:{type:"enabled"}` | 그대로 통과 | adaptive 로 변환. block_binding 은 유지하되 새로 만들지 않는다. effort 는 클라이언트의 `output_config.effort` > budget 에서 계산한 effort > default effort 순 |
| native `thinking:{type:"disabled"}` | 그대로 통과 | 위 disable 규칙 |

strict sampling 모델은 두 경로(OpenAI→Claude 변환, native `/v1/messages`) 모두에서
temperature/top_p/top_k 가 제거된다. `output_config` 는 effort 만 갱신하고 다른 키는 보존한다.
AWS, Vertex 는 `RequestOpenAI2ClaudeMessage` 를 공유하므로 같은 규칙을 받는다.

### Task 2: Epay 콜백은 한 transaction 에서 정산

`EpayNotify` 는 서명 검사 후 `model.RechargeEpay` 를 한 번 호출한다. 이 함수가 주문을 잠그고
(`FOR UPDATE`), provider 가 epay 이고 상태가 pending 인지 확인한 뒤, 결제수단 갱신, 상태
success 변경, `CreditTopUpTarget` 을 한 transaction 에서 수행한다. credit 이 실패하면(예: 지갑이
가득 참, `ErrTopUpQuotaCapacityExceeded`) 전체가 롤백되어 주문은 **pending 으로 남고**
지갑도 바뀌지 않는다. 이전에는 success 로 표시된 뒤 credit 만 실패해 success 인데 지급되지
않은 주문이 생겼다. top-up 로그는 transaction 커밋 뒤 모델 쪽에서 한 번만 기록한다.

응답은 정산 뒤에 정한다(`epayNotifyResponse`). 서명 검증 실패는 `fail`. 정산 성공, 이미 정산된
주문(`model.ErrTopUpOrderNotPending`, 중복 콜백이나 관리자 수동 완료가 먼저 처리한 경우), 무시되는
콜백(성공이 아닌 trade status, 없는 주문, 다른 provider 의 주문)은 `success` 로 확인 응답한다.
그 밖의 정산 오류(예: 지갑이 가득 참)는 `fail` 을 응답하므로 **Epay 가 콜백을 재시도한다.**
주문은 pending 으로 남아 있으므로 재시도 때 지갑 여유가 생겼으면 정산되고, 그 전에 **관리자
`ManualCompleteTopUp`** 으로도 정산할 수 있다. `RechargeEpay` 는 정산된 `*TopUp` 을 돌려주고 성공
로그는 이 값(상태 success, 실제 결제수단)을 기록한다.

### Task 3: task polling 의 2xx

`updateSunoTasks` 가 polling 응답이 200 이 아닐 때 오류로 처리하던 것을 2xx 전부 정상으로 바꿨다.
4xx/5xx 는 이전과 같은 메시지(`Get Task status code: N`)로 오류이고 task 와 quota 는
바뀌지 않는다. 이 때 `defer resp.Body.Close()` 도 상태 검사 앞으로 옮겨 거부 시 body 가 새지
않는다. 판정 함수는 `service` 안에 따로 있다(`relay` 의 `taskSubmitStatusAccepted` 는 import
cycle 때문에 못 쓴다).

### Task 4: Gemini prompt modality 귀속 (`buildUsageFromGeminiMetadata`)

`PromptTokensDetails` 와 `ToolUsePromptTokensDetails` 의 modality 를 대소문자/공백을 무시하고
`IMAGE` → `ImageTokens`, `AUDIO` → `AudioTokens`, `TEXT`/`VIDEO`/`DOCUMENT` → `TextTokens`
로 합산한다(Google 이 이들을 text 요율로 과금하므로 VIDEO/DOCUMENT 는 text). text 계열 detail 이
하나도 없으면 `TextTokens = max(PromptTokens - Audio - Image, 0)` 로 채운다(알 수 없는
modality 는 이 나머지에 들어간다). Google 이 text 계열 detail 을 보고했으면 그 합을 믿는다.
`billingexpr` 의 `img` 변수도 이제 Gemini image prompt 토큰을 본다.

cached 와의 겹침: `promptTokensDetails` 는 cached 부분을 포함한다. 정산은
`base = P - cached - image`(0 에서 clamp)에 `cached*cacheRatio + image*imageRatio` 를
더하고, audio 가격이 있는 모델(gemini-2.5-flash, 2.0-flash, robotics-er 는 내장 가격이 있다)은
audio 도 base 에서 뺀 뒤 0 에서 clamp 한다. 그래서 cached 나 audio 와 겹친 image 가 이중 과금될 수
있었다. `ImageTokens` 를 `max(PromptTokens - CachedTokens - AudioTokens, 0)` 로 자르고, 잘린
만큼은 `TextTokens` 로 돌려 합이 P 가 되게 한다. 예 1: P=2000, cached=1500, IMAGE=1089,
TEXT=911 이면 `ImageTokens` = 500, `TextTokens` = 1500, 합 2000 이다(cached 가 2000 이면 image 0,
cached 900 이면 자르지 않음). 이 경우 ImageRatio 1 의 quota 는 text + cached 만 있을 때와
같고(테스트 값 1075), ImageRatio 2 이면 500 개 image 만 2배가 되어 1575 가 된다.
예 2 (audio): gemini-2.5-flash, ImageRatio 1, P=2000, cached=1000, TEXT=700, IMAGE=800,
AUDIO=500 이면 `ImageTokens` = 500, `TextTokens` = 1000 이고 quota 는 image 를 귀속하지 않은 경우와
같은 1200 이다. cap 이 cached 만 빼면 image 800 이 그대로 남아 base 가 clamp 된 채 800 이 더 과금되어
1500 이 된다(`service/text_quota_test.go` 가 두 값을 모두 고정한다).

**과금 변화**: image 가 cached/audio 와 겹치지 않는 요청에서 ImageRatio 가 기본값 1 인 모델은 quota 가
바뀌지 않는다. cached/audio 와 겹치는 image 는 위 cap 으로 잘려 text 요율 이중 과금이 없다.
Gemini 모델에 ImageRatio ≠ 1 을 설정한 운영자는 이제 그 비율이 Gemini image prompt 토큰에 적용된다(이전에는 적용되지 않았다).

### Task 5, 6: 자동 충전 preset / 로그

- Task 5: `normalizeAndValidate` 가 threshold preset 의 `ThresholdQuota` 를
  `ValidateWalletAutoRechargeThresholdCapacity(threshold, 1회 충전 quota)` 로 검사한다.
  `threshold + 충전량` 이 ceiling(`MaxQuota`)을 넘으면 생성과 수정 모두 거부되고, 관리자는
  "top-up quota capacity exceeded: auto-recharge threshold leaves no room for a charge" 를
  본다. 이미 저장된 over-ceiling preset 은 건드리지 않으며 실행 시 기존 fail-closed 검사가
  막는다. scheduled preset 은 영향이 없다.
- Task 6: 수용량 실패 로그 줄을 `topUpCapacityFailureLogLine(userId, targetType, targetId, err)`
  가 만들며, 조직 지갑 자동 충전은 `target_type=organization target_id=<id>` 를 기록한다.

### Task 7: Ali `thinking_budget` (`relay/channel/ali/text.go`, `dto/openai_request.go`)

`requestOpenAI2Ali(request, originModelName, upstreamModelName)` 는 클라이언트 모델명(origin)과
upstream 모델명 중 **어느 한쪽이라도** Qwen 계열이면(`dto.IsQwenThinkingBudgetModel`)
`thinking_budget` 을 유지하고 `GeneralOpenAIRequest.KeepThinkingBudget`(`json:"-"` 플래그)을
켠다. `MarshalJSON` 은 이 플래그가 켜져 있으면 이름 기반 제거를 건너뛴다. 이름 하나가 비어
있으면 `request.Model` 로 대체한다. 결과: `qwen3-max` 를 사용자 지정 배포명 `my-qwen-deploy` 로
매핑해도 명시적 `0` 을 포함한 `thinking_budget` 이 전달된다. 반대로 Qwen 클라이언트 모델을 비
Qwen upstream 으로 매핑해도 이제 유지된다(either-name 규칙). 둘 다 비 Qwen 이면 이전처럼
제거된다.

초안은 `aliChatRequest` 정의 타입을 반환했으나 `relay/compatible_handler.go` 의
`(*dto.GeneralOpenAIRequest)` 단언(system prompt 주입)과 relay 포맷 추정이 깨져 플래그
방식으로 교체했다. 다른 어댑터는 이 플래그를 설정하지 않는다.

### Task 8: tokenkit codec 사전 생성

`tokenkit.Warm()` 이 `o200k_base` 와 `cl100k_base` 를 `sync.OnceValue` 로 미리 만든다(여러 번
호출해도 안전). `main.go` 가 `service.InitHttpClient()` 뒤에서 `go tokenkit.Warm()` 으로 호출하므로
시작을 막지 않으며, 첫 요청이 codec 생성 비용을 내지 않는다. 호출하지 않아도 이전처럼 지연
생성된다.

## 수행한 검증

실행한 명령과 결과 (작업 디렉터리 `/Users/molla/work/ai/new-api`, HEAD `338576ab2`,
Go 1.27 toolchain):

- `go build ./...`: 통과.
- `go vet ./...`: `common/custom-event.go` 의 lock by value 경고 4건(60, 73, 75, 78행)만
  출력한다. 이전 기록과 같은 기존 경고이며 늘지 않았다.
- `go test ./... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort`: 36개 패키지 ok, FAIL 0.
  베이스라인(`.superpowers/sdd/2026-10-14-followups-2a/baseline.txt`, 36 ok, 0 FAIL)과의
  `diff <(grep FAIL baseline.txt | sort) <(grep FAIL after-2a.txt)` 출력이 없다.
- `(cd tokenkit && go test ./... -count=1)`: ok.
- 각 task 는 실패하는 테스트를 먼저 작성한 뒤 구현했고(RED 확인 후 GREEN) task 별 리뷰를 거쳤다.
  Task 1, 2, 4, 7 에 수정 라운드가 1회씩 있었다. Task 5 는 기존 controller 테스트 하나
  (`TestRequestWalletThresholdRechargeRejectsThresholdPlusChargeAboveCeiling`)가 검증되는 Create
  경로로 over-ceiling preset 을 만들던 것을, 유효한 preset 을 만든 뒤 GORM 으로 threshold 를
  올리는 방식(legacy row)으로 바꿨다.
- model/controller/service 테스트는 in-memory SQLite 로 실행했다. 새 DB 코드는 GORM 만 쓰고,
  `FOR UPDATE` 는 기존 `Recharge` 와 같은 PostgreSQL/MySQL 인용 분기를 따른다. MySQL/PostgreSQL
  실제 실행은 하지 않았다.
- 알려진 flake: `relay/helper` 의 `TestStreamScannerHandler_StreamStatus_HandlerStop` 이 Task 1
  작업 중 전체 `./relay/...` 실행에서 한 번 panic 했다(`time.NewTicker` 의 non-positive
  interval). 단독 20회 반복, 깨끗한 tree, 다음 전체 실행에서 모두 통과하는 브랜치 이전부터의
  flake 이며 이번 최종 전체 실행에서는 발생하지 않았다.

## 운영 전제

운영자에게 보이는 변경과 판단 기록이다.

- Claude 5.x(`fable-5`, `mythos-5`, `opus-5-5`, `sonnet-5-5`, `opus-5`, `haiku-5`,
  `sonnet-5`)와 Opus 4.7/4.8 로 가는 Chat Completions(`reasoning_effort`, `-thinking`,
  effort suffix, openrouter `reasoning`)와 native `thinking:{type:"enabled"}` 요청이 이제
  400 대신 동작한다. 클라이언트가 보낸 budget 은 adaptive 와 effort 로 바뀌므로 응답의 thinking
  양이 budget 과 정확히 일치하지는 않는다. 4.6 이하(manual 가능 모델)의 동작은 같다.
- 판단: `claude-mythos-preview` 는 upstream 이 `supportsManual` 을 해제하지 않으므로 manual 가능으로
  뒀다(계획의 목록과 다르며 upstream 을 따랐다). effort suffix 확장은 adaptive 지원 모델로 한정해
  `claude-opus-4-5-<effort>` 는 확장하지 않는다. 틀리면 Mythos Preview 에 `enabled` 를 보내 400
  이 날 수 있으며 표의 한 필드를 바꾸면 된다.
- strict sampling 모델은 클라이언트가 보낸 temperature/top_p/top_k 가 조용히 제거된다.
  `reasoning_effort` 와 openrouter effort 의 `minimal`/`xhigh`/`max` 가 manual 불가 모델에서는
  이제 adaptive thinking 을 켠다(이전에는 무시). thinking 을 끌 수 없는 모델에서 "끄기" 요청은
  adaptive + low(또는 Sonnet 5.5 는 `between_tools`)가 되어 thinking 이 완전히 꺼지지 않는다.
이 adaptive 는 `display:"omitted"` 라 응답에 요약은 노출되지 않지만 thinking 토큰은 과금된다.
- Epay (동작 변경): credit 이 실패한 주문은 pending 으로 남고 콜백은 `fail` 을 응답하므로 Epay 가
  재시도한다(재시도 횟수와 간격은 Epay 설정에 따른다). 지갑 여유를 확보하면 다음 재시도가 정산하고,
  그 전에 관리자 화면의 수동 완료(`ManualCompleteTopUp`)로도 정산할 수 있다. 사용자는 정산 전까지
  결제했지만 잔액이 늘지 않은 상태로 보일 수 있다. 주문이 없거나 다른 provider 의 주문인 콜백은
  재시도해도 소용없으므로 이전처럼 `success` 로 확인 응답한다.
- Gemini: ImageRatio 를 기본값(1)이 아닌 값으로 설정한 Gemini 모델은 image prompt 토큰이 그
  비율로 과금된다(과금액 변화). ImageRatio 1 인 모델의 quota 는 변하지 않는다. cached 와 겹치는
  image 는 cache 요율로, audio 와 겹치는 image 는 audio 가격으로만 과금된다(cap `P - cached - audio`).
- 자동 충전 preset: `threshold + 1회 충전량` 이 ceiling 을 넘는 threshold preset 은 생성/수정이
  거부된다. 이미 저장된 over-ceiling preset 은 그대로 남아 있고 실행 시 fail-closed 로 막힌다. 먼저
  수정이나 삭제로 정리한다.
- Ali: `thinking_budget` 은 클라이언트 모델명과 upstream 모델명 중 하나라도 Qwen 계열이면
  전달된다. 사용자 지정 배포명으로 매핑한 Qwen 채널이 이제 `thinking_budget` 을 받는다. Qwen
  client 모델을 비 Qwen upstream 으로 매핑한 채널은 `thinking_budget` 을 upstream 이 받게 되므로,
  그 upstream 이 모르는 필드를 거부하지 않는지 확인한다.
- async task polling: 201/202 응답을 정상 처리한다. 이전에는 거부되어 task 가 갱신되지 않았다.
- tokenkit: 시작 시 백그라운드 goroutine 이 codec 을 만든다. 시작 직후 짧은 시간의 CPU/메모리
  사용이 늘 수 있고 시작을 막지는 않는다.

## 보류된 TODO

### 이 브랜치 리뷰에서 나온 minor (보류)

- Task 1: effort 표의 `supportsDisable` 가 zero-value `false` 라 새 행을 추가할 때 빠뜨리면
  disable 불가로 취급되는 함정이 있다. `cannotDisable` 로 뒤집는 편이 안전하다.
- Task 2: `RechargeEpay` 가 `Recharge` 와 약 80% 중복이다(파일 전반의 패턴). 오류를
  `errors.New` 로 만들어 하위 DB 오류를 버린다. `quota <= 0` 가드가 없다(기존과 같다).
- Task 3: `taskPollStatusAccepted` 가 `relay.taskSubmitStatusAccepted` 와 중복이다
  (import cycle 로 공유하지 못했다).
- Task 4: 알 수 없는 modality 는 text 계열 detail 이 하나도 없을 때만 나머지(text)로 흡수되고
  그렇지 않으면 무시된다. AUDIO 가 cached 와 겹치는 위험(기존 동작)은 audio 를 이중으로 세지는
  않으며 image cap 이 audio 를 반영하도록 최종 리뷰에서 고쳤다. tool-use 와 cached 가 함께 있는
  조합은 테스트가 없다.
- Task 5: 경계값(`MaxQuota - 충전량` 과 정확히 같은 threshold)의 accept 케이스 테스트가 없다.
- Task 6: 조직 target 이 로그에 전달되는 호출부 연결은 코드를 읽어서만 확인했다(주입 가능한 DB
  실패 지점이 없어 handler 수준 테스트가 없다).
- Task 7: 플래그 방식이라 이후 다른 어댑터가 `KeepThinkingBudget` 을 잘못 설정할 여지가 있다(현재는
  없다). `TextHelper` 전체 scaffolding 은 무거워 handler 수준 회귀 테스트 대신 반환 타입 단언으로
  대체했다.
- 실행 전 gofmt: `relay/channel/ali/dto.go` 등 이 브랜치가 건드리지 않은 파일이 gofmt 되어 있지
  않다(기존).

### 2단계 기록의 보류 항목 중 이 브랜치가 해소하지 않은 것

2단계 기록의 목록 가운데 이번 8건에 포함되지 않은 항목은 모두 그대로 남아 있다. 후속 묶음
(2-B, 2-C)의 후보다.

- Task 20 (충전): `Validate*Capacity` helper 중복. 테스트 공백(Waffo tokens 모드 최소 1, 조직
  threshold 자동 충전, 조직 Toss krw 모드, tokens 모드 PayPal). Stripe 견적과 결제 사이 1단위
  truncation(upstream 동일).
- Task 21 (pre-consume): `expr.md` 와 `price.go` 의 "tiered leaf 는 모두 token 가격" 문구,
  `relay/helper/valid_request.go` 주석, `web/classic` 의 `PreConsumedQuota` 입력, 호출자 없는
  `service/pre_consume_quota.go` 정리.
- Task 22 (ceiling): `Recharge*`/`CreditedQuotaForTopUp` 의 `IntPart()`, 상환/부여의 오류 이름 재사용,
  `model-rate-limit.go` 의 `Duration(minutes)*time.Minute` 가드, SQLite check-in 의 비 atomic 경로,
  import 변환의 `Round(0)`, 조직 import 의 적용 순서, `TransferAffQuotaToQuota`/`QuotaForNewUser`
  상한, 32-bit 전체 빌드(`service/http_client.go` 상수), `Redeem`/ManageUser `override` 의 Redis cache
  갱신, `docs/quota-ceiling-and-bigint-migration.md` §4.3, §9.3 반영, 모델별 rate limit 횟수 상한.
- Task 23 (tokenkit): Responses 스트림 fallback 의 tool 이름 미계산과 두 경로의 tool call 추정
  차이, `THIRD-PARTY-LICENSES.md` 버전, `dto/gemini.go` gofmt, Gemini 스트림 usage 판정과 last-frame
  -wins, `go mod tidy` 의 `// indirect` 드리프트.
- 1단계 기록의 보류 항목(과금/usage, 대문자/`null` effort 의 native normalizer 테스트, Claude
  prefix predicate 중복 등)은 1단계 기록을 따른다. 이 중 `service/task_polling.go` 의 201/202 와
  Gemini IMAGE modality 는 이번에 해소됐다.

### 의도적으로 포팅하지 않은 upstream 항목 (relaykit 과제)

2단계 기록의 목록 그대로이며 이 브랜치는 relaykit 을 도입하지 않았다: 모델명 `@` modifier 와
과금 identity 사다리, hosted tool 프로토콜 간 변환과 Responses↔Claude/Gemini 직접 변환기,
Responses custom tools, Responses WebSocket 릴레이, async task 어댑터의 JS 플러그인화, auth/i18n
재작업, `web/src` 단일 트리 이동, search 과금(`d10f11fff`, `acb347a19`), expression 확장(`064ed943e`,
`f362c7c51`, `25ec832fa`), 모델/vendor 가격 재작업(`0c76e4dae`). 이번 Task 1 이 의존한 upstream 의 capability
표는 `relaykit` 모듈 안에 있으나 표의 내용만 옮겼고 모듈은 가져오지 않았다.

## 저장소 상태 참고

작업은 worktree 없이 main checkout 의 `codex/followups-2a-2026-10` 브랜치(분기 시점 team
`57a2e8f9a`)에서 로컬 커밋으로만 수행했으며 push 하지 않았다. `team` 에 병합하지 않았다. 병합은
사용자 확인 뒤 `git checkout team && git merge --no-ff codex/followups-2a-2026-10` 로 진행한다.
