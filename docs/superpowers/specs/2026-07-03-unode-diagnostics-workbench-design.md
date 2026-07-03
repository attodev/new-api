# unode Diagnostics Workbench Design

> 작성일: 2026-07-03
> 대상: `tools/unode-diagnostics/`
> 상태: 사용자 승인 설계안

## 1. 배경 / 문제

운영 검증 중 동일 key/channel 조건에서도 일부 요청은 Claude web search 가능 경로로 처리되고, 일부 요청은 `x-cc-session-id`가 붙는 Claude Code 세션 성격 경로로 처리되는 현상이 관찰됐다.

현재는 `curl`과 운영 로그 조회를 번갈아 사용해야 해서 다음 문제가 있다.

- 요청 body/header/path를 바꿔가며 반복 검증하기 어렵다.
- 요청/응답 전체와 요약 신호를 한 화면에서 비교하기 어렵다.
- 브라우저 새로고침 또는 재실행 후 이전 실험 이력이 사라진다.
- `.env`의 base URL, key, channel id를 매번 수동으로 복사해야 한다.

## 2. 목표

- 로컬 브라우저에서 direct unode 요청과 ALRouter 경유 요청을 반복 실행할 수 있는 standalone 진단 도구를 제공한다.
- `.env`에 있는 운영 검증 값을 읽어 UI 폼을 자동 채움할 수 있게 한다.
- 프리셋 요청과 자유 편집 요청을 모두 지원한다.
- 요청/응답 원문과 자동 요약을 파일 DB에 저장해 브라우저 갱신 후에도 이력을 유지한다.
- 히스토리 항목을 클릭하면 이전 요청/응답이 editor/viewer에 복원된다.
- 저장되는 히스토리에는 인증 토큰/key 원문을 남기지 않는다.

## 3. 비목표

- 운영 서비스 UI에 통합하지 않는다.
- 운영 DB 또는 channel 설정을 수정하지 않는다.
- unode 내부 admin/root 권한이 필요한 하위 channel/session 결정 정보를 직접 추론하지 않는다.
- 복잡한 계정 관리, 사용자 인증, 멀티유저 공유 기능은 제공하지 않는다.
- 히스토리 동기화나 원격 저장소 업로드는 제공하지 않는다.

## 4. 선택한 접근

`tools/unode-diagnostics/` 아래에 로컬 standalone 도구를 만든다.

구성은 작은 Node.js 서버와 정적 브라우저 UI다.

- Node.js 서버: `.env` 로드, API 요청 프록시 실행, 응답 수집, secret masking, history 파일 저장/조회
- 브라우저 UI: Workbench layout으로 history, request editor, response viewer를 한 화면에 표시

브라우저에서 외부 운영 API를 직접 호출하지 않고, 로컬 서버의 `/api/send`로 요청을 보낸다. 이렇게 해야 CORS, `.env` 로드, 파일 DB 저장, secret masking을 안정적으로 처리할 수 있다.

## 5. UI 설계

사용자가 승인한 A안 Workbench layout을 적용한다.

### 5.1 화면 구조

| 영역 | 목적 |
| --- | --- |
| 좌측 history pane | 저장된 요청/응답 이력 목록, 검색/필터, 선택 시 복원 |
| 중앙 request pane | target/preset 선택, URL/method/header/body 편집, `.env 불러오기`, `API 보내기` |
| 우측 response pane | 응답 상태, 주요 헤더, raw body, 자동 요약, 판정 badge |

### 5.2 필수 컨트롤

- `.env 불러오기` 버튼
- 프리셋 선택 버튼 4개
- target 선택: `direct unode`, `ALRouter`
- method/path/base URL 입력
- headers JSON editor
- body JSON editor
- `API 보내기` 버튼
- history reload 버튼
- 선택한 history 항목 삭제 또는 전체 삭제는 초기 버전 범위에서 제외한다.

## 6. 프리셋

초기 프리셋은 4개를 제공한다.

| 프리셋 | 목적 |
| --- | --- |
| Direct unode `/v1/messages` + `web_search_20250305` | direct unode Claude Messages 경로가 web search tool을 실제 사용 가능한지 확인 |
| Direct unode `/v1/chat/completions` + `web_search_options` | direct unode OpenAI-compatible 경로가 web search 옵션을 어떻게 처리하는지 확인 |
| ALRouter `/v1/messages` forced channel | 운영 라우터 경유 native Claude Messages 요청에서 같은 channel이 어떻게 동작하는지 확인 |
| ALRouter `/v1/chat/completions` forced channel | 운영 라우터 경유 OpenAI-compatible 요청과 `web_search_options` 변환 결과 확인 |

프리셋은 editor에 값을 채우는 역할만 한다. 사용자는 path, header, body를 자유롭게 수정할 수 있다.

## 7. 환경 변수 로드

도구는 repo 루트 `.env`에서 다음 값을 읽는다.

| 변수 | 사용처 |
| --- | --- |
| `UNODE_BASE_URL` | direct unode target base URL |
| `UNODE_RELAY_API_KEY` | direct unode relay request 인증 |
| `NEW_API_BASE_URL` | ALRouter target base URL |
| `NEW_API_RELAY_API_KEY` | ALRouter relay request 인증 |
| `NEW_API_CHANNEL_ID` | forced channel suffix 구성 |
| `NEW_API_ADMIN_ACCESS_TOKEN` | 필요 시 운영 로그 조회 확장에 사용 |
| `NEW_API_ADMIN_USER_ID` | 필요 시 운영 로그 조회 확장에 사용 |

초기 구현은 request 실행 중심이며, 운영 로그 조회 자동화는 선택 확장으로 둔다. 단, 데이터 모델은 `upstream_request_id`와 `request_id`를 저장할 수 있게 설계한다.

## 8. Secret 처리 정책

### 8.1 화면 표시

`.env`에서 읽은 key는 UI에 그대로 표시하지 않는다. 예를 들어 `sk-...abcd` 형식으로 표시한다.

요청 실행 시에는 서버가 실제 값을 사용한다. 사용자가 직접 header editor에 key 원문을 입력한 경우에도 저장 전 masking을 적용한다.

### 8.2 히스토리 저장

다음 header/key는 저장 전에 masking한다.

- `authorization`
- `x-api-key`
- `api-key`
- `x-goog-api-key`
- `New-Api-User`
- `cookie`
- `set-cookie`

body 안의 `api_key`, `access_token`, `token`, `key`, `authorization` 계열 필드도 masking 대상이다.

마스킹 값은 원문 길이와 prefix/suffix 일부만 남기는 형태로 저장한다.

## 9. API 설계

| Method | Path | 설명 |
| --- | --- | --- |
| `GET` | `/api/config` | `.env`에서 읽은 표시용 config와 missing variable 목록 반환 |
| `GET` | `/api/presets` | 프리셋 요청 목록 반환 |
| `POST` | `/api/send` | 요청 실행, 응답 수집, masking 후 history 저장 |
| `GET` | `/api/history` | 최신 history 목록 반환 |
| `GET` | `/api/history/:id` | 특정 history 상세 반환 |
| `GET` | `/healthz` | 로컬 서버 상태 확인 |

`POST /api/send` 요청은 다음 필드를 받는다.

```json
{
  "presetId": "alrouter-chat-web-search",
  "target": "alrouter",
  "method": "POST",
  "baseUrl": "https://example.invalid",
  "path": "/v1/chat/completions",
  "headers": {
    "content-type": "application/json"
  },
  "body": {
    "model": "claude-sonnet-4-6"
  },
  "authMode": "env"
}
```

`authMode: "env"`이면 서버가 target과 path에 맞는 인증 header를 구성한다. 사용자가 header editor에서 직접 인증 header를 넣은 경우에도 실행은 허용하되 저장 시 masking한다.

## 10. History 파일 DB

파일 DB는 append-only JSONL로 둔다.

기본 위치:

```text
tools/unode-diagnostics/data/history.jsonl
```

각 record 구조:

```json
{
  "id": "diag_20260703_000001",
  "createdAt": "2026-07-03T12:00:00.000Z",
  "presetId": "alrouter-chat-web-search",
  "target": "alrouter",
  "request": {
    "method": "POST",
    "url": "https://example.invalid/v1/chat/completions",
    "headers": {},
    "body": {}
  },
  "response": {
    "status": 200,
    "headers": {},
    "bodyText": "",
    "json": {}
  },
  "summary": {
    "hasCcSessionId": false,
    "requestId": "",
    "upstreamRequestId": "",
    "contentTypes": [],
    "toolErrorCodes": [],
    "webSearchRequests": 0,
    "classification": "unknown"
  }
}
```

서버 시작 시 JSONL을 읽어 최신순으로 정렬한다. 손상된 라인이 있으면 전체 로드를 실패시키지 않고 해당 라인을 skip하며 경고를 UI에 표시한다.

## 11. 자동 요약 / 판정

응답마다 다음 신호를 추출한다.

- HTTP status
- `x-cc-session-id` 존재 여부
- `x-request-id`, `x-oneapi-request-id`, `x-newapi-request-id`
- Anthropic content block type 목록
- OpenAI choices message content 일부
- `usage.server_tool_use.web_search_requests`
- `too_many_requests` 등 tool result error code
- `Claude Web Search called` 문구

판정 값:

| classification | 조건 |
| --- | --- |
| `web_search_used` | server tool use 또는 로그성 문구에서 web search 수행 신호가 확인됨 |
| `claude_code_session_suspected` | `x-cc-session-id`가 있거나 응답 문구가 도구 미지원/Claude Code 세션 성격을 보임 |
| `tool_rate_limited` | tool result error code에 `too_many_requests`가 있음 |
| `tool_not_used` | tool 사용 신호 없이 일반 응답만 존재 |
| `request_error` | HTTP 4xx/5xx 또는 JSON 파싱/네트워크 오류 |
| `unknown` | 위 조건에 해당하지 않음 |

## 12. Error Handling

- `.env` 누락: 서버는 기동하되 UI에 missing variable을 표시하고, 관련 target 실행은 막는다.
- JSON editor 파싱 실패: 요청 전 UI에서 오류 표시.
- 네트워크 오류: response pane과 history에 오류 record 저장.
- timeout: 기본 90초. UI에서 timeout 발생 여부를 표시.
- history 파일 쓰기 실패: 요청 응답은 화면에 표시하되 저장 실패를 별도 경고로 표시.

## 13. 테스트 / 검증

최소 검증:

- `.env` 로드 API가 secret 원문을 반환하지 않는지 확인
- masking 함수가 인증 header와 token-like body field를 제거하는지 확인
- history JSONL append/read round trip 확인
- preset 4개가 유효한 method/path/body를 채우는지 확인
- response summarizer가 `x-cc-session-id`, `too_many_requests`, `server_tool_use.web_search_requests`를 추출하는지 확인

수동 검증:

- 로컬 서버 실행 후 브라우저에서 `.env 불러오기`
- 프리셋 4개 각각 실행
- 브라우저 새로고침 후 history 유지 확인
- history 클릭 시 request/response pane 복원 확인
- 저장된 `history.jsonl`에 secret 원문이 없는지 확인

## 14. 운영상 주의

- 이 도구는 로컬 진단용이다. 운영 배포물에 포함하거나 외부에 노출하지 않는다.
- history 파일에는 응답 본문이 저장되므로 고객 데이터가 포함된 prompt는 사용하지 않는 것이 좋다.
- `.superpowers/` visual companion 산출물은 커밋 대상이 아니다.

