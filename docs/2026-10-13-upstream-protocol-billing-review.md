# Upstream(QuantumNous) 변경 검토 — protocol / 계산 중심 (2026-10-13)

## 기준점

| 항목 | 값 |
|---|---|
| fork 시점 (구 main) | `74985fa87` 2026-05-26 |
| 이전 포팅 기준 | upstream `93d2df85f` 2026-08-14 까지 선별 포팅 53건 (team 2026-08-21~24, 본문에 `Upstream:` 기록) |
| 현재 upstream HEAD (origin/main 동기화 완료) | `63cecab82` 2026-10-13 |
| 로컬 `main` | 아직 `74985fa87` (pull 필요) |

| 범위 | 커밋 수 | backend 변경 | 비고 |
|---|---|---|---|
| A. 이전 포팅 이후 `93d2df85f..63cecab82` | 325 | 200 | 전부 미반영 |
| B. 이전 포팅 범위 `74985fa87..93d2df85f` | 380 | 170 | 51건 포팅, 119건 미반영 |

UI 전용(web/, electron/, docs) 커밋은 제외했다. upstream 은 이 기간에 `web/default` → `web/src` 단일 트리, `relaykit/` 모듈 분리, 비동기 task 어댑터의 JS 플러그인화 등 구조를 크게 바꿨으므로 전체 merge 보다 선별 포팅이 현실적이다.

---

## A. 이전 동기화 이후 (2026-08-14 ~ 10-13)

### A1. 상위 프로바이더 protocol 변동

**Claude (Anthropic / Bedrock / Vertex)**
- `767a82b87` thinking.block_binding 전달 + anthropic-beta 헤더를 여러 줄 받아 콤마 결합. 누락 시 system prompt/tools 가 바뀌는 멀티턴에서 "block is bound to a different conversation" 400. Bedrock `anthropic_beta` 목록 trim.
- `036ba376d` 모델별 effort 허용 범위 매칭. Opus 5 / Haiku 5.5 / Sonnet 5.5 는 high 로 cap, effort 미지원 모델은 effort 제거, thinking-off 요청에도 effort 유지. Fable/Mythos 는 effort 지원 모델로 등록(thinking 끌 수 없어 low effort 로 실행).
- `c2b7a9a9e` 메시지 단위 `output_config`(effort beta) 보존. `789c97019` auto mode safeguard 필드 보존.
- `3dda1d50c` 파라미터 없는 tool 보존, `4442bb302` 빈 tools 배열 주입 금지.
- `4eb3b9160` Claude→Chat 변환 시 tool_result 안의 이미지 블록을 user 메시지 image_url 로 hoist (기존엔 base64 텍스트로 전송).
- `3a9f41ee8` `/v1/messages/count_tokens` 임시 비활성.

**OpenAI / Codex / Responses**
- `49ec46966` gpt-5 판정을 "메이저 버전 ≥5" 로 변경 → gpt-6-* 도 max_completion_tokens 사용, temperature/top_p/logprobs 제거. `d0cb7347c` gpt-6-sol / gpt-6-luna 보완.
- `7d09c6954` Chat→Responses 변환 시 prompt_cache_key 전달.
- `335ebcff7` Responses custom tools(Codex exec/apply_patch)를 Chat 업스트림에 string 입력 function 으로 변환, 응답은 custom_tool_call 로 복원. `1a4166d8e` Claude/Gemini 업스트림에서도 custom tools 유지, 이름 없는 tool call 보류.
- `13c8e90b8` Responses input_image 의 문자열 image_url 을 Chat 형식 `{url}` 객체로 래핑 (vLLM 등 strict 게이트웨이 400 해결).
- `e0483122c` Responses→Claude/Gemini 변환 시 입력 끝의 assistant turn(prefill)을 제거. Claude 4.6+, Gemini 3.5/3.6 이 prefill 을 400 으로 거부하며 Codex 재시도 시 발생.
- `6e9de44a7`, `78bd5b1cb` function_call_output 의 미디어 파트를 user 메시지/inline data 로 전달.
- `9fe0457ee`, `ae249f4ec`, `801456a06`, `75f3d246e` Responses WebSocket 릴레이 신설 (채널별 스위치, HTTP 와 라우팅 공유).
- `610334dbd` 크로스 프로토콜 → OpenAI chat 변환 시 항상 `stream_options.include_usage` 요청.

**Gemini**
- `c10780e05` tool/structured output 의 JSON Schema 제약(const, oneOf, $ref, $defs, type 배열)을 Gemini 타입 Schema 로 강제하지 않고 `parametersJsonSchema` / `responseJsonSchema` 로 그대로 전달. `4b13d9388` legacy Schema 경로에서 non-string enum 제거.
- `fa90b2312` thinkingLevel 대소문자 무시, `81336fc69` `:countTokens` 를 generateContent 로 릴레이하지 않고 거부.
- `78bd5b1cb` 스트림 usageMetadata 가 누적 스냅샷인데 프레임마다 합산하던 버그 수정 (이미지 프롬프트 3배 과금).

**Reasoning / thinking 정규화 (relaykit)**
- `7c044d7c5` 모델명 뒤 `@thinking:`, `@effort:`, `@temperature:`, `@topp:` 명시 modifier 도입. 기존 `-thinking`, `-high` 류 suffix 는 gpt/o/claude/gemini 계열에서만 해석, qwen-max 같은 이름은 그대로. 과금 identity 는 `base@effort:E@thinking:S` 사다리로 1회 해석.
- `6b659fd61` reasoning_effort 를 modifier/변환이 없으면 건드리지 않음. `c6bac617d` suffix adapter 토글이 effort suffix 까지 포함.
- `0ed497f06` hosted tool(web search, grounding, citations) 을 프로토콜 간 보존, reasoning intent 중앙화, Responses↔Claude/Gemini 직접 변환기 추가.
- `0a03088e0` 변환 손실 정리: Gemini OpenAPI 스키마(대문자 type, nullable) → JSON Schema 재작성, 미디어를 MIME 기준으로 image/PDF/text/audio 로 매핑, 지원 안 되는 미디어는 진단과 함께 생략.
- `5ac756284` reasoning 과 answer 가 한 chunk 에 오는 업스트림에서 text 소실 수정. `ba0e478e0` tool call 과 함께 온 assistant text 보존. `bbd97446c` Chat→Claude/Gemini 스트림 변환 상태를 gin.Context 에서 RelayInfo 로 이동(채널 재시도 시 초기화).

**채널 추가/변경**
- `8529f209c` vLLM, SGLang 채널. `8f6961c67` vllm `thinking_token_budget` 필드.
- `ba2e9287b` Ollama 에 Claude Messages / OpenAI Responses passthrough, `5caafd3d8` 채널별 OpenAI 호환 chat 스위치, `8e5e09166` 마지막 스트림 프레임 tool calls 보존.
- `cae3676ec` GLM `/v1/responses`, `3e8c358da` zhipu reasoning_effort 전달.
- `6e10f9bc9` Kimi K3 가 system 메시지에 `tools` 배열을 넣는 동적 tool 로딩 지원(`Message.Tools` passthrough). `acb347a19` Moonshot `/v1/responses` 를 그대로 전달, Advanced Custom 에 Claude/Gemini → Responses 변환기, 채널별 web search 인코딩.
- `69a500298` Advanced Custom 라우트별 pass-through. `c6741c36a` 채널별 업스트림 TLS 검증 스킵. `04c597bf6` 파라미터 오버라이드 정규식 조건.
- `0bee5d441` Ali 이미지 response_format 존중.
- `ed7c4e35d`, `972aed197` 업스트림 응답 model 이 요청과 다르면 로그에 표시.

**비동기 task**
- `eb48396d5` 외 다수: 내장 task 어댑터(Suno, Kling, Jimeng, Vidu, Sora, Veo, Hailuo, Doubao, Alibaba…)를 샌드박스 JS 플러그인(`plugins/tasks`, `pkg/jsplugin`, moejs)으로 전면 교체. `03563a4a7` OpenAI Images API 도 플러그인으로 서비스. 구조 대변경이라 선별 포팅 대상이 아님.
- `d61d6be75` task submit 에서 2xx 전부 성공 처리(201/202). `9df450fe5` polling 훅에 HTTP status 전달, 실패 횟수 bound (이전엔 404/키 폐기가 24h 동안 IN_PROGRESS 로 quota 점유).

### A2. 계산 / 과금 변동

**pre-consume / quota 정수**
- `a073f74b3` int32 폐기. 기동 시 legacy user quota 스키마 거부, wallet bound, count*duration int64 wrap 방지. team 은 이미 `quota-ceiling-and-bigint-migration` 으로 bigint 확장을 했으므로 두 구현을 대조해야 함.
- `9978ee1e2` `PreConsumedQuota` 폐기 → `quota_setting.pre_consume_multiplier`(입력 토큰 추정치 × 배수) 와 `trust_quota_usd`(기본 10 USD, 0 이면 bypass 없음). 지갑과 token 한도 둘 다 임계값 초과 시에만 pre-consume 생략. `9c293e8c0` pre-consume 에서 출력 토큰 추정 제거.
- `6237d9d77` 모델 rate limit 을 예약 방식으로, 성공 판정을 HTTP status 가 아닌 응답 결과로.

**usage 정산**
- `f11641428` Responses 의 cached token 을 PromptTokensDetails 로 옮겨 정산 (누락 시 cache 할인 미적용).
- `3cea2bf79` cached input 의 image/text/audio 세부 항목 보존(명시적 0 유지).
- `bbd97446c` Claude cache_creation 하위 객체와 legacy flat 필드 동기화 → 캐스케이드 배포에서 1h 캐시 과대 청구 경로 차단. Gemini modality 키 정규화 및 중복 합산.
- `78bd5b1cb` Gemini 스트림 token details 누적 중복 수정(위 참조).
- `55a6cd2a4` Responses 스트림이 `response.completed` 전에 끊기면 tool-call 인자/reasoning/refusal delta 로 completion 추정, 업스트림 이벤트가 하나라도 있으면 prompt 추정치 과금 (기존엔 0 정산 후 전액 환불).
- `0ed497f06` 릴레이 hop 간 BillingUsage sidecar 유지, 부분/최종 usage 단조 병합, Gemini per-call tool surcharge 유지.
- `8c8c4153d` 사용량 통계에서 quota 보존. `b22a43b6e` task 정산 로그에 `is_task` 표시.

**이미지**
- `07578faf2` 이미지 수를 `data.#` 길이가 아니라 payload(url/b64_json) 기준으로 계산. object 형 응답 0건, url+b64 분리 응답 2배 과금, revised_prompt 만 있는 항목 과금 버그 수정.
- `e2bb05f70` Images `output_tokens_details` 를 img_o 과금에 매핑.
- `f064bffa2` 이미지 수량 검증을 예약 전에 통일 (Ali `parameters.n=0` 거부, 재시도/정산 동일 경로).
- `f362c7c51` 표현식에 `img_cr`, `image_count` 변수. `f256e40bc` GPT image 모델 표현식 기본값(text/image cache 가격 분리).

**가격 모델**
- `064ed943e` 조건부 토큰 표현식에 `tier(name, fixed(usd))` 고정 per-request 가격 leaf. token usage 0 인 성공 요청도 안전 정산, Realtime/task 진입점에서는 거부.
- `25ec832fa` legacy 가격 → 표현식 초안 변환/미리보기 API. `d52bdc0b4` 시간 기반 가격 편집기.
- `eb99ab1b4` gpt-6-astra 내장 표현식 가격.
- `7c044d7c5` 과금 identity 정규화(위 참조).
- `0c76e4dae`, `71c1fd7ca`, `3509ae7c1`, `9948b1ef1` 모델/벤더 관리와 가격 재편. team 의 모델/벤더 할인 계층(`setting/ratio_setting/model_discount.go`)과 같은 영역이라 충돌 검토 필요.
- `bb28f2cca` tokenkit 모듈: tool 정의/호출/결과를 토큰 추정에 포함. 실측에서 agent 요청 prompt 추정이 31~93% 과소였고 Claude 4.7+ 는 동일 텍스트에 약 30% 더 많은 토큰. pre-consume 과 fallback 정산 금액에 직접 영향.
- `d10f11fff` Google Search grounding 을 Gemini 3 는 unique query 당($14/1k), 2.5 이하는 grounded prompt 당($35/1k) 과금. `acb347a19` 벤더가 보고한 search 사용량 기반 과금.

**결제/충전**
- `47ba9d2c6` 충전 전 지갑 수용량 검증(`ValidateTopUpQuotaCapacity`), `2a0ce3475` 정산 불가 주문을 결제 전에 거부, zero-ratio Stripe 검증. team 의 PayPal/Toss/조직 충전 컨트롤러에 동일 가드 필요.
- `0908d9e7b` go-epay v0.0.5.

### A3. 안정성 / 인프라 (참고)
- `b518d0033` 업스트림 응답 헤더 대기 시간 bound. 응답 없는 업스트림이 goroutine 과 요청 본문 3벌을 영구 점유해 OOM. 운영상 중요.
- `1751f43ee` SQLite WAL + busy timeout, `66031a09d` PostgreSQL prepared statement 비활성(pooler 호환), `6eb6f35ed` JSON 컬럼 Valuer string 반환, `9a8674425`/`043ff99a5` 재기동 시 중복 마이그레이션 방지.
- `d3874db61` 메모리 rate limiter 할당 최적화. `3f926c3ef`, `a5b40663d` perf metrics 를 요청 경계에서 기록.
- `7bbe85bcb` JSON helper 를 host-injectable codec 으로 (Rule 1 영역).
- 인증/보안 대규모 재편: scoped access token(`caca52f8d`), 관리자 step-up 검증(`6cbb1c7ed`), 비밀번호 전송 암호화(`b80d633cf`, opt-in `918427d8a`), 감사 로그(`d8cb17744`, `3f8a50cf8`), passkey multi-RP(`385d2dfd1`), Telegram OAuth 통합(`3e84ec0ab`). team 의 조직/OIDC 작업과 범위가 겹침.
- `128089fa7` 백엔드 에러 메시지 i18n 전면 재편 — team 한글화와 직접 충돌.

---

## B. 이전 동기화에서 포팅하지 않은 것 (2026-05-27 ~ 08-14)

### B1. protocol
- `86ac0f774`, `60a1acb70`, `b8bb3f40a` 프로토콜 변환 계층을 `relaykit/` 모듈로 분리 (team 커밋 본문에 참조만 있고 실제 분리는 미반영).
- `2d5a04163`, `3a506f50f`, `1086038f5` Responses↔Chat 변환 상태 머신(assistant text + tool calls 보존, output_index 바인딩, 중복 tool call 방지).
- `c36418c86` 텍스트 프로토콜 변환 통합, Gemini thought signature 처리, 프로토콜 인식 billing usage.
- `a043eef55` Gemini→OpenAI chat 스트림 변환.
- `3f2c0aeda`, `55b00fcf0`, `a6cf42c0f` Advanced Custom 채널(경로 매칭, 업스트림 모델 fetch). `398cdafec` New API 채널, `2d23cdf29` Sub2API 채널 + `/v1/alpha/search`, `57746fc97` Codex 모델 discovery.
- `66ee6b8f9` Qwen thinking_budget passthrough(0 포함). `0c7aceb83` Claude Opus 4.8. `bb234ff41` Responses compact 모델 suffix 처리 제거. `eab18a835` usage 로그에 reasoning effort 일관 기록.
- `d6b5ce99d` `Request.GetBody` 설정으로 HTTP/2 REFUSED_STREAM/GOAWAY 후 투명 재시도. `ea4f02101` replay 메타데이터를 요청 본문으로 이동.
- `bd585d78e` 클라이언트 끊김 시 Bedrock 요청 취소 + 실제 과금 경로 로그. `269e4ff39` 이미지 스트림 disconnect 처리/과금 조정. `8461e5339` New API 채널 multipart 이미지 편집 보존.
- `0ab020206` token auto group 순서 지정. `e99a9bd86` 채널별 HTTP transport 설정, `933ea0cdd` idle timeout, `df087b022` SSRF 보호, `d2f7f9ee3` 익명 요청 본문 제한, `4a188deea` 채널 비활성 시 affinity 유지 옵션.

### B2. 계산
- `2d23cdf29` 관리자 설정 가능한 tool 호출 가격과 cross-provider surcharge 정산. `4cf9107f0` 로그에 매칭된 조건부 multiplier 표시.
- `c36418c86` 프로토콜 인식 billing usage 의미 보존.
- `f6c260437` 구독 플랜 wallet overflow / downgrade group. `158802708` 구독 잔액 환급 토글. `9b93d61b7` 관리자 quota reset. `70ea899e3`, `4e570389d` 트랜잭션 row locking 중앙화.
- `bc14c18f6` task 환불 로직 정리. `d10fc762f` 비동기 task 로그를 시작 노드에 귀속(team 은 `8ba1e86e8` 로 별도 구현).
- `8874d1929` quota 로깅 동기화.

### B3. 기타 (참고)
- `6dc4030fd` ClickHouse 로그 DB, `4aee5f7d5` casbin 기반 관리자 권한, `31d70fca3` 대시보드 세션을 stateless token 으로, `2cbdfa039` 시스템 인스턴스 패널, `efd6c445a`/`5d3423bec` 채널 테스트 모드, `2f23a6673` SMTP STARTTLS/NTLM, `56dbaab1d` Secure 세션 쿠키.

---

## 권장 포팅 순서

1. **즉시 (소규모, 호환성 영향 큼)**: `767a82b87`, `036ba376d`, `c2b7a9a9e`, `789c97019`, `49ec46966`+`d0cb7347c`, `7d09c6954`, `3dda1d50c`+`4442bb302`, `78bd5b1cb`(Gemini 누적 usage), `f11641428`, `55a6cd2a4`, `07578faf2`, `e2bb05f70`, `b518d0033`, `d61d6be75`, `3e8c358da`, `8e5e09166`, `0bee5d441`, `8c8c4153d`, `b22a43b6e`.
2. **설계 대조 후 반영 (team 자체 구현과 겹침)**: `a073f74b3`(int32 폐기 vs bigint 확장), `9978ee1e2`(pre-consume multiplier/trust threshold), `47ba9d2c6`+`2a0ce3475`(충전 가드 → PayPal/Toss/조직 지갑), `0c76e4dae` 계열(모델/벤더 가격 vs 할인 계층), `bb28f2cca`(tokenkit), `d10f11fff`+`acb347a19`(search 과금), `064ed943e`+`f362c7c51`(표현식 확장).
3. **구조 변경 (별도 프로젝트로)**: relaykit 분리 + Responses↔Chat/Claude/Gemini 변환기 일괄, `@` modifier 및 과금 identity(`7c044d7c5`, `6b659fd61`, `0ed497f06`), task JS 플러그인 시스템, Responses WebSocket, 인증/보안 재편, i18n 재편, `web/src` 프론트.
