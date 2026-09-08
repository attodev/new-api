# Claude adaptive thinking의 빈 thinking 블록 분석 및 수정

작성일: 2026-09-06

이 문서는 Claude Opus 5 및 Sonnet 5 응답에서 thinking 토큰은 과금되면서
thinking 본문이 빈 문자열로 반환되던 문제의 원인 분석과 수정 내용을
기록한다. 외부 사용자로부터 접수된 게이트웨이 오류 보고 4건 중
"thinking 토큰은 사용했으나 thinking 본문이 비어 있음" 항목에 해당한다.

## 증상

응답의 thinking 블록이 다음 형태로 도착했다.

```json
{
  "type": "thinking",
  "thinking": "",
  "signature": "CATSpR8Kpg..."
}
```

- signature는 정상적으로 존재한다.
- usage의 thinking 토큰은 42에서 1344 사이로 집계되며 과금도 이루어진다.
- 보고자의 비교 관찰에 따르면 `claude-opus-4-8`은 thinking 본문을 정상
  반환했고 `claude-opus-5`만 빈 문자열이었다.
- streaming과 non-streaming 양쪽에서 동일하게 재현되므로 SSE 파싱 문제는
  아니다.

## 근본 원인

게이트웨이가 thinking 본문을 삭제한 것이 아니라, 업스트림 요청에
`thinking.display`가 지정되지 않아 모델 기본값이 적용된 결과다.

Anthropic은 Opus 4.7부터 adaptive thinking의 `display` 기본값을
`"omitted"`로 둔다. 이 값이면 thinking 토큰은 그대로 과금되지만 응답의
thinking 블록은 본문 없이 signature만 담아 돌아온다. Opus 4.6은 기본값이
요약 표시였으며, 이 사실은 저장소에 이미 주석으로 기록되어 있었다.

```go
// Opus 4.7 rejects non-default temperature/top_p/top_k with 400
// and defaults display to "omitted"; restore the 4.6 visible summary.
request.Thinking.Display = "summarized"
```

문제는 이 보정이 적용되는 범위였다. 보정은 모델명 하드코딩과 suffix 분기
안에만 존재했다.

- 조건이 `claude-opus-4-7` 접두사로 고정되어 있었다. 저장소 전체에
  `opus-5` 또는 `sonnet-5` 문자열은 존재하지 않았다.
- 보정이 `-high` 등 effort suffix 분기와 `-thinking` suffix 분기 안에만
  있었다. 클라이언트가 `thinking: {"type": "adaptive"}`를 직접 보내는
  경우는 어느 분기도 타지 않아 `display`가 비어 있는 채로 업스트림에
  전달되었다.

두 번째 항목이 실제 보고 경로다. Anthropic native 요청은 게이트웨이에서
사실상 pass-through되므로, 클라이언트가 지정하지 않은 `display`를 채워줄
지점이 없었다.

또한 동일한 하드코딩이 Anthropic native 경로
(`relay/claude_handler.go`)와 OpenAI 호환 경로
(`relay/channel/claude/relay-claude.go`) 양쪽에 중복되어 있어, 한쪽만
고치면 다른 쪽이 남는 구조였다.

## 수정 내용

모델명 기준이 아니라 요청 내용 기준으로 판단하도록 바꾸었다. adaptive
thinking인데 `display`가 비어 있으면 `"summarized"`를 채운다. 클라이언트가
명시한 값은 항상 우선하며, `display`는 adaptive 전용 필드이므로 budget
기반 thinking(`type: "enabled"`)은 건드리지 않는다.

`dto/claude.go`에 단일 규칙을 추가했다.

```go
func (c *Thinking) NormalizeAdaptiveDisplay() {
    if c == nil || c.Type != "adaptive" || c.Display != "" {
        return
    }
    c.Display = "summarized"
}
```

두 경로 모두 thinking 구성이 끝난 지점에서 이 함수를 호출한다.

| 경로 | 호출 위치 |
| --- | --- |
| Anthropic native | `relay/claude_handler.go`의 `applyClaudeThinkingSettings` 말미 |
| OpenAI 호환 | `relay/channel/claude/relay-claude.go`의 reasoning 처리 직후 |

이에 따라 양쪽 경로에 흩어져 있던 `Display: "summarized"` 하드코딩 4곳을
제거했다. Opus 4.7의 `temperature`, `top_p`, `top_k`를 nil로 두는 처리는
`display`와 무관한 별개 제약이므로 그대로 유지했다.

native 경로에서는 `ClaudeHelper` 안에 인라인으로 들어 있던 thinking 처리
분기를 `applyClaudeThinkingSettings(request, originModelName)`로 추출했다.
이 함수는 `request.Model`이 변경되었는지를 반환하므로 호출자가
`info.UpstreamModelName` 동기화를 그대로 수행할 수 있으며, 동작을 단위
테스트로 검증할 수 있게 되었다.

## 검증

재현 테스트를 먼저 작성해 수정 전 실패를 확인했다.

```
dto: NormalizeAdaptiveDisplay undefined
relay/channel/claude: expected "summarized", actual ""   (claude-opus-4-6-high)
```

추가한 테스트는 다음과 같다.

| 파일 | 검증 내용 |
| --- | --- |
| `dto/claude_thinking_display_test.go` | adaptive에 display가 비면 summarized, 명시값 보존, `enabled` 무변경, nil 수신자 안전 |
| `relay/claude_thinking_test.go` | `claude-opus-5`, `claude-sonnet-5`, `claude-opus-4-7`에 대해 클라이언트가 보낸 adaptive가 정규화됨. 명시값 보존, `enabled` 무변경, thinking 미요청 시 nil 유지, effort suffix 분기 |
| `relay/channel/claude/relay_claude_thinking_test.go` | OpenAI 경로의 `-high` suffix 두 모델과 `reasoning_effort` 경로 |

`go build ./...`, `gofmt`(변경 파일 전부), `go test ./...`를 통과했다.

## 동작 변화 요약

- 클라이언트가 `thinking.display`를 지정하지 않고 adaptive thinking을
  요청하면, 모델과 무관하게 요약이 포함된 thinking 블록을 받는다.
- 클라이언트가 `display`를 명시하면 그 값이 그대로 전달된다.
  `"omitted"`를 원하는 클라이언트는 명시하면 된다.
- budget 기반 thinking(`type: "enabled"`)의 요청 형태는 변하지 않는다.
- `claude-opus-4-6`은 기존에도 요약이 기본값이었으므로 실질 동작은
  동일하며, 이제 `display`가 명시적으로 전달된다.

## 남은 작업

`claude-opus-5-high`와 같은 effort suffix는 여전히 인식되지 않는다.
`relay/claude_handler.go`와 `relay/channel/claude/relay-claude.go`의 effort
suffix 분기 조건이 `claude-opus-4-6` 및 `claude-opus-4-7` 접두사로 한정되어
있어, 그 외 모델은 suffix가 제거되지 않은 채 업스트림에 전달된다. 이는 빈
thinking 본문과는 별개의 증상이므로 이번 수정 범위에 포함하지 않았다.
