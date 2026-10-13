# Upstream 동기화 1단계 기록 — protocol / 과금 포팅 (2026-10-13)

작성일: 2026-10-13

이 문서는 `codex/upstream-sync-2026-10` 브랜치에서 수행한 upstream(QuantumNous)
동기화 1단계(plan `docs/superpowers/plans/2026-10-13-upstream-sync-phase1-2.md` 의
Task 1~18)의 포팅 내역, 운영상 바뀌는 동작, 검증 결과, 보류 항목을 기록한다.
검토 근거와 후보 목록은 `docs/2026-10-13-upstream-protocol-billing-review.md` 를
따른다.

## 검토 범위

- 대상: 2026-08-14 이후 upstream 변경 중 protocol 호환성(Claude, OpenAI,
  Responses, Gemini, Ali/Zhipu/Ollama)과 계산·과금(cached token, 스트림 중단 정산,
  이미지 usage), 그리고 전송 계층 안정성(응답 헤더 대기, HTTP/2 재시도, Bedrock
  취소)에 해당하는 항목.
- 방식: 전체 merge 가 아니라 항목별 선별 포팅. upstream 이 `relaykit/` 모듈 분리,
  `web/src` 단일 트리, task JS 플러그인화로 구조를 바꾸었기 때문에 같은 diff 를
  그대로 적용할 수 없는 항목은 team 코드 구조에 맞춰 다시 작성했다.
- 범위: 커밋 범위 `3504b77ee..afbf87cd3` (22개 커밋, 18개 task). 각 커밋 메시지의
  `Upstream:` trailer 에 원본 커밋 링크가 있다.
- 2단계(Task 20~24: 충전 전 지갑 수용량 검증, pre-consume multiplier, int32 폐기
  대조, tokenkit)는 이 문서의 범위가 아니다.

## 병합된 커밋

fork 커밋은 `git log --reverse 3504b77ee..afbf87cd3` 순서이며, upstream 해시는 모두
`https://github.com/QuantumNous/new-api/commit/<sha>` 이다.

| Task | fork 커밋 | upstream | 동작 요약 |
| --- | --- | --- | --- |
| 1 | `2bf9fe686` | `3e8c358da` | Zhipu 업스트림으로 `reasoning_effort` 전달 (cherry-pick) |
| 1 | `54e7ce9fe` | `8e5e09166` | Ollama 스트림 마지막 프레임의 tool calls 보존 (cherry-pick) |
| 1 | `e30e7d78a` | `0bee5d441` | Ali 이미지 `response_format` 존중 (cherry-pick) |
| 1 | `e9e2d2b0f` | `8c8c4153d` | 사용 통계 로그에서 quota 보존 (cherry-pick) |
| 1 | `28cb053e2` | `b22a43b6e` | task 정산 로그에 `is_task` 표시, tiered 과금 표시 보정 (cherry-pick) |
| 2 | `66673c63b` | `789c97019`, `c2b7a9a9e`, `767a82b87` | Claude DTO 에 `safeguards`, 메시지 단위 `output_config`, `thinking.block_binding` passthrough 추가 |
| 3 | `90874460e` | `767a82b87` | 여러 줄 `anthropic-beta` 헤더를 모두 전달, Bedrock beta 목록 trim, adaptive 재구성 시 `block_binding` 유지 |
| 4 | `4c28add9f` | `4442bb302`, `3dda1d50c` | OpenAI→Claude 변환에서 빈 `tools` 생략, 파라미터 없는 tool 은 `input_schema` 를 type object + 빈 `properties` 기본값으로 채워 보존 |
| 5 | `fb6c2c970` | `036ba376d` | 모델별 Claude effort 허용 범위 표(`relay/channel/claude/effort.go`)로 effort 를 cap/제거 |
| 6 | `94ef85e5d` | `0c7aceb83` | `claude-opus-4-8` 모델 군 등록 (effort 표 포함) |
| 7 | `d9a7cc12a` | `49ec46966`, `d0cb7347c` | gpt-5 계열 및 명명된 gpt-6 snapshot 에 대해 chat capability(`max_completion_tokens` 등) 판정 |
| 8 | `b8014dd45` | `7d09c6954` | Chat→Responses 변환 시 `prompt_cache_key` 전달 |
| 9 | `c6943ad54` | `f11641428` | Responses/DeepSeek cached token 을 cache 가격으로 정산하도록 usage 정규화 |
| 10 | `8cebfe11d` | `55a6cd2a4` | `response.completed` 전에 끊긴 Responses 스트림의 completion 을 추정하고 prompt 추정치로 과금 |
| 11 | `768275e78` | `1086038f5` | Responses→Chat 스트림에서 tool call 이 한 번만 방출됨을 고정하는 테스트 (테스트만) |
| 12 | `64c961e4c` | `07578faf2`, `e2bb05f70` | 이미지 응답 payload 기준 항목 수 계산 함수, JSON→SSE fallback 전달, `output_tokens_details` → `img_o` 매핑 |
| 13 | `d4eca064b` | `78bd5b1cb` | Gemini 누적 스트림 usage 는 마지막 프레임 값을 쓴다는 회귀 테스트 (테스트만) |
| 14 | `868709e9a` | `b518d0033` | `RELAY_RESPONSE_HEADER_TIMEOUT` 로 업스트림 응답 헤더 대기 시간 제한 |
| 15 | `ab1ec3e92` | `d61d6be75` | task 제출 시 업스트림 2xx 전부 성공 처리 (201/202 포함) |
| 16 | `d966f28cf` | `d6b5ce99d` | 릴레이 요청에 `Request.GetBody` 제공, HTTP/2 stream reset 후 재시도 가능 |
| 17 | `6623ed80d` | `bd585d78e` | 클라이언트 연결이 끊기면 AWS Bedrock 요청 취소 |
| 18 | `afbf87cd3` | `66ee6b8f9` | Qwen `thinking_budget` passthrough, 비 Qwen 모델에서는 marshal 시 제거 |

Task 1 은 upstream 커밋 5건을 그대로 cherry-pick 했으므로 fork 커밋도 5개이고,
나머지 task 는 task 당 1개 커밋이다. `e9e2d2b0f` 의 본문에는 upstream squash 에서
따라온 `* fix(log)...` 줄이 남아 있다(내용 영향 없음).

### 테스트만 추가한 task 와 fork 에 버그가 없던 task

- Task 11(Responses→Chat 중복 tool call)과 Task 13(Gemini 스트림 usage 누적)은
  upstream 의 버그가 fork 에는 존재하지 않았다. fork 의
  `chat_via_responses.go` 는 `callID` 키 기반 자체 구현이고, `geminiStreamHandler`
  는 프레임마다 usage 를 교체한다. 그래서 production 코드는 바꾸지 않고 현재
  동작을 고정하는 테스트만 커밋했다.
- 이 두 task 외에 test-only 로 끝난 task 는 없다. (task 지시문에는 "세 개의
  test-only task" 로 적혀 있었으나 task 번호는 11, 13 두 개뿐이며 원장에도 두 개만
  기록되어 있다. 이 문서는 원장 기준으로 적는다.)
- Task 13 은 계획의 IMAGE modality assertion 을 TEXT 로 바꿔 작성했다.
  `buildUsageFromGeminiMetadata` 가 prompt 쪽에서는 AUDIO/TEXT 만 매핑하기 때문이며
  (기존 한계, 이번 범위 밖), 테스트는 프레임마다 값이 교체된다는 점을 그대로
  고정한다.
- Task 12 는 부분 포팅이다. fork 에는 응답 항목 수로 정산하는 메커니즘
  (`UpdateImageCount` 류)이 없고 정산은 요청의 `n` 을 쓰므로, upstream 의 개수 기반
  과금 부분은 가져오지 않고 SSE fallback 전달과 `img_o` 매핑만 포팅했다.

## 반영 후 동작

### 운영자가 알아야 하는 동작 변경

1. 새 환경 변수 `RELAY_RESPONSE_HEADER_TIMEOUT` (초, 기본 1800).
   - 요청을 모두 쓴 뒤 업스트림 응답 헤더가 올 때까지의 대기만 제한한다
     (`http.Transport.ResponseHeaderTimeout`). 헤더 수신 후의 스트리밍 본문에는
     적용되지 않는다.
   - relay 기본 transport 와 proxy 가 설정된 transport 모두에 적용된다.
   - `0` 이면 이전처럼 무제한. 비정상적으로 큰 값은 `time.Duration` overflow 를
     막기 위해 상한으로 clamp 한다.
   - 비스트리밍 요청은 업스트림이 생성을 끝낸 뒤에야 헤더를 보내는 경우가 많으므로
     실제 최장 응답 시간보다 충분히 크게 둬야 한다. `.env.example` 에 문서화했다.
2. `doRequest` 는 업스트림의 3xx 응답을 더 이상 따라가지 않는다
   (`http.ErrUseLastResponse`, client 는 shallow copy 로 처리). 3xx 가 그대로
   호출자에게 전달된다. 리다이렉트에 의존하던 채널이 있다면 3xx 를 반환하게 된다.
   task 제출의 307 은 기존 `shouldRetryTaskRelay` 로 재시도되고, 파일 다운로드,
   mjproxy, video_proxy 는 자체 client 를 쓰므로 계속 리다이렉트를 따른다.
3. Claude effort 가 모델별로 정리된다 (`relay/channel/claude/effort.go`).
   - 모델이 지원하는 최대 단계로 cap 한다 (예: `claude-opus-4-6` 의 `xhigh` →
     `max`, `claude-opus-4-5` 의 `max`/`xhigh` → `high`).
   - effort 미지원 모델(예: `claude-sonnet-4-5`, `claude-haiku-4-5`)에는 effort 를
     제거한다.
   - 표에 없는 모델은 값을 그대로 둔다.
   - `claude-*-5` 계열도 표에 있다. `claude-opus-5-5`, `claude-sonnet-5-5`,
     `claude-opus-5`, `claude-haiku-5`, `claude-sonnet-5`, `claude-fable-5`,
     `claude-mythos-5` 는 effort, `xhigh`, `max` 를 모두 받고,
     `claude-mythos-preview` 는 effort 와 `max` 만 받는다
     (`effort.go:29-39`).
   - thinking 이 꺼져 있으면 한도가 더 낮아진다. `claude-sonnet-5-5`,
     `claude-opus-5`, `claude-haiku-5` 는 thinking 이 `disabled` 또는
     `between_tools` 일 때 `xhigh`/`max` 를 `high` 로 낮춘다
     (`ClaudeEffortForThinking`, `effort.go:108-130`, 네이티브 경로
     `NormalizeClaudeOutputConfig` 도 같은 규칙, `effort.go:160` 부근).
   - 모델명 suffix 로 지정한 effort 뿐 아니라 네이티브 `/v1/messages` 의
     `output_config.effort` 도 알려진 모델에서는 정규화한다. upstream 은 suffix 가
     없을 때 그대로 통과시키지만, provider 가 400 으로 거부할 값을 미리 낮추는
     쪽을 택했다. 클라이언트가 고른 effort 가 조용히 낮아질 수 있다.
   - 메시지 단위 `ClaudeMessage.OutputConfig` 의 effort 는 정규화하지 않고 그대로
     통과시킨다(upstream 동일).
4. gpt-5 판정이 좁혀지고 gpt-6 은 명명된 snapshot 만 포함된다. 이전 규칙은
   `strings.HasPrefix(modelName, "gpt-5")` 였고, 지금은 `gpt-5`, `gpt-5-*`,
   `gpt-5.*` 만 gpt-5 계열로 본다. gpt-6 은 명명된 snapshot (`gpt-6-astra`,
   `gpt-6-sol`, `gpt-6-luna`)만 같은 capability 를 받는다.
   upstream 최종 코드(`relaykit/dto/openai_request.go`)를 따랐다. 초기 요약의
   "메이저 버전 ≥ 5" 규칙은 superseded 된 커밋 메시지 기준이어서 채택하지 않았다.
   따라서 이름이 명시되지 않은 gpt-7 같은 미래 세대는 `max_tokens`/sampling
   제한 처리를 상속하지 않으며, snapshot 이 추가될 때까지 거부될 수 있다.
5. `thinking_budget` 는 Qwen 계열에서만 업스트림으로 나간다. `OpenAIRequest`
   marshal 시 비 Qwen 모델이면 필드를 제거해 다른 provider 가 알 수 없는 필드로
   400 을 내지 않게 한다. 필드 타입은 `json.RawMessage` 이며, 명시적 `0` 은 raw
   바이트 `0` 이 비어 있지 않으므로 Qwen 모델에서 그대로 전달된다.
6. Responses 스트림이 `response.completed` 전에 끊기면, tool-call 인자,
   reasoning, refusal delta 로 completion 토큰을 추정한다. prompt 추정치는
   `billsPrompt := CompletionTokens != 0 || (started && !failed)` 일 때만 과금한다.
   `failed` 는 `response.failed` 또는 `error` 이벤트로 설정되므로 업스트림이 명시적으로
   실패를 알린 경우는 예외다 (`relay/channel/openai/relay_responses.go`).
   이전 조건은 `PromptTokens == 0 && CompletionTokens != 0` 일 때만 prompt 를
   채우는 것이었다. 중단된 스트림이 이제 과금될 수 있다.
7. Responses 및 DeepSeek 의 cached token 은 `PromptTokensDetails.CachedTokens`
   로 정규화되어 cache 가격으로 정산된다. 기존에 cache 할인이 누락되던 경로가
   저렴해진다.

### 그 밖의 반영 사항

- Claude: `thinking.block_binding`, `safeguards`, 메시지 단위 `output_config` 가
  upstream 으로 그대로 전달된다. `dto.Thinking` 은 `type`, `budget_tokens`,
  `display`, `block_binding` 만 가지는 typed struct 이므로 그 밖의 알 수 없는
  `thinking` 하위 키는 전달 대상이 아니다 (`dto/claude.go`).
  `anthropic-beta` 가 여러 줄로 오면 모두 결합해 전달한다. 빈 `tools` 는 보내지
  않고, 파라미터 없는 function tool 은 `input_schema` 로 보존된다.
  `claude-opus-4-8` 이 등록되었다.
- OpenAI/Responses: Chat→Responses 변환에서 `prompt_cache_key` 가 전달된다.
  이미지 응답은 JSON 이 SSE 로 fallback 될 때 url/b64_json 이 있는 항목만 방출하며,
  `output_tokens_details` 는 이미지 출력 토큰(`img_o`)으로 매핑된다.
- 전송 계층: 모든 릴레이 요청에 `Request.GetBody` 가 설정되어 HTTP/2 stream
  reset 후 `net/http` 가 재시도할 수 있다. 본문이 `BodyStorage` 에서 오면 새
  reader 를 반환하고, `bytes.Reader`/`strings.Reader` 는 `net/http` 에 맡긴다.
- AWS Bedrock: 클라이언트가 끊기면 업스트림 요청 context 가 취소된다.
- task: 제출 응답이 2xx 면(201, 202 포함) 성공으로 처리한다. 4xx/5xx 는 기존대로
  `fail_to_fetch_task` 로 상태를 유지한다.

## 수행한 검증

- `go build ./...` 통과.
- `go vet ./...` 결과는 아래 "go vet 참고" 에 기록했다. 기존 경고이며 이번
  변경으로 늘지 않았다.
- `go test ./... -count=1` 을 전체 실행했다.
  - 패키지 수: 36개 패키지 ok, FAIL 0. 베이스라인
    (`.superpowers/sdd/2026-10-13-upstream-sync-phase1-2/baseline.txt`)은 31개
    패키지 ok 였고, 이번 실행에서는 `i18n`, `pkg/billingexpr`,
    `relay/channel/task/sora`, `router`, `types` 5개가 추가로 ok 로 나왔다.
  - FAIL 비교: 베이스라인의 FAIL 목록과 이번 FAIL 목록을 `diff` 로 비교한 결과
    출력이 없다. 새 FAIL 이 없다.
- 각 task 는 실패하는 테스트를 먼저 작성한 뒤 구현했고, task 별 리뷰를 거쳤다.
  Task 7 은 upstream 최종 capability struct 에 맞추는 수정 1회, Task 11 은 테스트
  이름과 index assertion 수정 1회가 있었다.
- model/controller 패키지 테스트는 in-memory SQLite 로 실행했다. MySQL/PostgreSQL
  에서의 별도 실행은 하지 않았다. 이번 범위의 변경은 모델 스키마나 raw SQL 을
  건드리지 않는다.

### go vet 참고

`go vet ./...` 는 `common/custom-event.go` 의 lock by value 경고 4건
(`encode`, `Render`, `encode` 호출, `WriteContentType`; `CustomEvent` 가
`sync.Mutex` 를 포함)만 출력했다. 이번 변경 이전부터 있던 경고이며 이번 task 의
범위가 아니므로 수정하지 않았다. 이 경고 때문에 `go vet` 의 종료 코드는 0 이
아니다.

## 운영 전제

- 새로 필요한 설정은 `RELAY_RESPONSE_HEADER_TIMEOUT` 하나이며 기본값(1800초)으로
  동작한다.
- 응답 시간이 30분을 넘을 수 있는 비스트리밍 요청(긴 reasoning, 대형 이미지/영상
  생성 등)이 있으면 배포 전에 `RELAY_RESPONSE_HEADER_TIMEOUT` 을 늘리거나 `0` 으로
  둔다.
- 3xx 를 반환하던 업스트림에 기대는 채널이 없는지 배포 전에 확인한다. 있다면
  channel base URL 을 최종 주소로 바꿔야 한다.
- 클라이언트가 보낸 Claude effort 가 모델 한도를 넘으면 조용히 낮아진다.
  (provider 는 어차피 거부했을 값이다.) 반면 메시지 단위
  `ClaudeMessage.OutputConfig` 의 effort 는 정규화하지 않고 통과시키므로(upstream
  동일, Task 5 판단), effort 만 담은 system 메시지가 모델이 지원하지 않는 단계를
  보내면 provider 가 400 으로 거부할 수 있다.
- 과금 결과가 달라지는 변경이다. 배포 후 사용량/정산 로그를 확인한다.
  - 끊긴 Responses 스트림이 prompt 추정치로 과금될 수 있다 (명시적 실패 제외).
  - Responses/DeepSeek cached token 이 cache 가격으로 정산되므로 해당 요청의
    과금액이 바뀐다(cache 가격이 입력 가격보다 낮으면 내려간다).
  - 비 Qwen 모델로 보낸 `thinking_budget` 은 marshal 시 조용히 제거된다. 클라이언트
    에는 오류가 나지 않는다.
- 새 gpt-6 계열 snapshot 이나 gpt-7 은 코드에 이름이 추가되기 전까지 gpt-5
  capability 를 받지 않는다. 새 모델을 열기 전에 capability 판정을 확인한다.

## 보류된 TODO

### 의도적으로 포팅하지 않은 upstream 항목

다음은 upstream 의 `relaykit` 모듈 분리 또는 구조 변경에 의존하므로 1단계에서
다루지 않았다. team 에는 해당 기반이 없다.

- 모델명 `@` modifier (`@thinking:`, `@effort:`, `@temperature:`, `@topp:`)와 이에
  의존하는 과금 identity 사다리.
- hosted tool(web search, grounding, citations)의 프로토콜 간 변환, Responses↔
  Claude/Gemini 직접 변환기, Responses custom tools(Codex exec/apply_patch) 변환.
- Responses WebSocket 릴레이.
- 비동기 task 어댑터의 JS 플러그인화(`plugins/tasks`, `pkg/jsplugin`). team 은 내장
  Go 어댑터를 유지한다.
- auth, i18n 재작업.
- `web/src` 단일 트리 이동에 따른 프런트엔드 변경. team 은 `web/default`,
  `web/classic` 구조를 유지한다.

### 리뷰에서 나온 후속 후보 (task 별)

기능 결함으로 판정되어 막힌 것은 없고, 모두 minor 로 보류했다.

- 과금/usage
  - `PostAudioConsumeQuota` 와 채널 테스트의 `settleTestQuota` 는 tiered 과금
    cache 변수에 대해 `normalizeCachedUsage` 를 거치지 않는다 (Task 9). cache
    creation 테스트가 quota 가 아니라 필드만 검증하고, `CacheWriteTokens` 채움
    테스트가 없다.
  - Gemini prompt 의 IMAGE modality 토큰이 TEXT 가 함께 있을 때
    `PromptTokensDetails` 에서 빠진다 (`relay-gemini.go` 의
    `buildUsageFromGeminiMetadata`, Task 13). 기존 한계이며 modality 기반 과금
    expression 에 영향을 준다. 후속 후보.
  - `openaiImageResponseCount` 는 production 호출자가 없다 (Task 12). 응답 개수 기반
    정산을 넣을 때 연결한다. 그 전까지 이미지는 요청의 `n` 으로 정산된다. 잘못된
    JSON 본문 테스트가 없고 `gjson.ValidBytes` 는 `[]` 본문도 허용한다.
  - 중단된 Responses 스트림 테스트가 error event 전달과 delta/usage 없이 output 만
    있는 completed 경로를 검증하지 않는다 (Task 10).
- task
  - `service/task_polling.go:201` 은 아직 201/202 polling 응답을 거부한다. upstream
    불변식(2xx 허용)과 어긋나는 기존 동작이며 후속 후보다 (Task 15). 제출 경로
    테스트는 판정 함수의 table test 이며, `relay_task.go:227` 호출부를 되돌려도
    잡지 못한다. `{299, true}` 경계 케이스도 추가할 만하다.
- Claude
  - 4.6/4.7/4.8 prefix 조건이 `relay-claude.go` 와 `claude_handler.go` 에 중복되어
    있어 공통 predicate 로 묶는 편이 좋다. 네이티브 경로의 opus-4-8 테스트가 없다
    (Task 6).
  - 대문자/`null` effort, 잘못된 `output_config` 에 대한 네이티브 normalizer 테스트가
    없고, `claudeSupportsEffort` 는 테스트에서만 쓰인다 (Task 5).
  - Claude 쪽 `anthropic-beta` Join 이 빈 헤더 줄을 trim/제외하지 않아, 빈 줄이
    있으면 쉼표가 남을 수 있다 (Task 3).
  - `input_schema` 의 `required[]` + 중첩 `properties` passthrough 와 `type` 배열
    테스트가 없다 (Task 4). `safeguards`/`output_config` 의 omitempty 부재 테스트가
    없다 (Task 2).
- OpenAI
  - `prompt_cache_key` 테스트가 marshal 결과의 wire key 이름을 직접 검증하지 않는다
    (Task 8). adaptor 의 suffix stripping 에서 `UseMaxCompletionTokens` gate 에
    설명 주석이 없다 (Task 7).
- 전송 계층
  - 응답 헤더 대기 테스트의 상한 1.9초는 부하가 큰 CI 에서 flake 가 날 수 있어 5초
    정도로 넓히는 편이 좋다. ollama, ali-image, topup 의 HTTP client 는 여전히
    무제한이다 (relay 경로가 아니며 upstream 도 동일, Task 14).
  - 16개 handler 에 size/`getBody` 바인딩 블록이 중복되어 있다(upstream 배치와
    맞추기 위해 유지). Windows 의 unlink-while-open 경계 사례, 일부 부정확하거나
    빠진 주석(`diskStorage.NewReader` 보장 문구, `keepUpstreamRedirectResponse`
    근거)이 있다 (Task 16).
  - Bedrock 취소 후 `[DONE]`/최종 usage 쓰기를 고정하는 테스트가 없고
    `stream.Close` 가 두 번 호출된다(upstream 동일, Task 17).
- 기타
  - `go test ./relay/helper` 에서 구현 중 flake 가 1회 보고되었으나 `-count=4` 재실행에
    서는 재현되지 않았다 (Task 18). 새 테스트가 `GetSystemRoleName` 의 doc comment 와
    그 테스트 사이에 끼어 있어 주석 위치가 어긋났다.
  - Task 1 의 `e9e2d2b0f` 본문에 upstream squash 에서 온 `* fix(log)...` 줄이 남아
    있다.

## 저장소 상태 참고

작업은 worktree 없이 main checkout 에서 `codex/upstream-sync-2026-10` 브랜치(분기
시점 team `5167b251d`)를 직접 사용해 로컬 커밋으로만 수행했으며 push 는 하지 않았다. 병합과 원격 반영은 2단계 기록 후
별도 작업으로 취급한다.
