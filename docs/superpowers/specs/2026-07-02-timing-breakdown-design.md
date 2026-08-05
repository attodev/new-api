# 요청 타이밍(E2E / LLM / 게이트웨이 오버헤드) 분리 계측 설계

## 배경

고객이 new-api를 경유하면서 발생하는 지연(latency)을 우려하고 있다. 현재 로그의 `UseTime`(초 단위)은 시작점이 "채널 선택 완료 이후"(distributor 미들웨어, `middleware/distributor.go:159`)라서 인증, rate-limit, 채널 선택에 걸린 시간이 누락되어 있고, "new-api 자체가 추가한 지연"과 "모델 프로바이더 응답 자체에 걸린 시간"이 구분되지 않는다.

고객에게 "전체 시간 중 new-api 오버헤드는 이 정도뿐이고 나머지는 모델 사업자 응답 시간"이라는 근거자료를 만들기 위해, 요청 단위로 세 가지 값을 정확히 측정하고 로그 상세 화면에서 확인할 수 있게 한다.

## 지표 정의

| 지표 | 시작점 | 종료점 |
|---|---|---|
| `e2e_ms` | Gin이 요청을 받는 가장 앞단 (CORS보다 앞, 신규 미들웨어) | 클라이언트로 마지막 바이트까지 `Write()` 완료 시각 |
| `llm_ms` | 업스트림으로 `client.Do(req)` 호출 직전 | 업스트림 응답 바디(`resp.Body`)를 끝까지 읽은(EOF) 시각 |
| `gateway_ms` | — | `e2e_ms - llm_ms` (단순 차감) |

내부 전용(관리자 상세 뷰 확장 대비, 현재는 UI 미노출):

| 지표 | 의미 |
|---|---|
| `pre_llm_ms` | 게이트웨이 진입 ~ 업스트림 호출 시작 (인증/rate-limit/채널선택 등) |
| `post_llm_ms` | 업스트림 응답 완료 ~ 클라이언트 전송 완료 (응답 변환/전송) |

기존 `UseTime`(초 단위, distributor 이후 기준)과 `frt`(첫 토큰까지 ms)는 의미와 계산 방식을 그대로 유지한다. 신규 지표는 이를 대체하지 않고 추가된다.

## 계측 구현

### 1. E2E 시작 — 신규 최상단 미들웨어

`router/relay-router.go`의 기존 미들웨어 체인(`CORS → Decompress → BodyStorageCleanup → Stats → ...`)보다 앞에 `RequestTiming` 미들웨어를 추가한다.

```go
func RequestTiming() gin.HandlerFunc {
    return func(c *gin.Context) {
        common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())
        writer := &timingResponseWriter{ResponseWriter: c.Writer}
        c.Writer = writer
        c.Next()
        common.SetContextKey(c, constant.ContextKeyGatewayExitTime, writer.lastWriteTime)
    }
}
```

`timingResponseWriter`는 `gin.ResponseWriter`를 감싸며 `Write()`/`WriteString()` 호출마다 `lastWriteTime = time.Now()`를 갱신한다 (락 없이 단일 goroutine에서만 쓰기 때문에 동기화 불필요).

### 2. LLM 시작/종료 — `doRequest()` 단일 지점

42개 채널 어댑터가 전부 거치는 유일한 공통 지점인 [relay/channel/api_request.go:485](relay/channel/api_request.go:485) `doRequest()`를 수정한다.

```go
func doRequest(c *gin.Context, req *http.Request, info *common.RelayInfo) (*http.Response, error) {
    info.SetUpstreamRequestStart()          // time.Now() 기록
    resp, err := client.Do(req)
    if err != nil {
        info.SetUpstreamResponseEnd()       // 실패도 "대기한 시간"으로 기록
        return resp, err
    }
    resp.Body = newTimingReadCloser(resp.Body, info.SetUpstreamResponseEnd)
    return resp, nil
}
```

`newTimingReadCloser`는 `io.ReadCloser`를 감싸서, `Read()`가 `io.EOF`를 반환하거나 `Close()`가 호출되는 시점(둘 중 먼저 오는 것, `sync.Once`로 중복 방지)에 콜백을 실행한다. 이렇게 하면:
- 비스트리밍 어댑터가 `io.ReadAll(resp.Body)`로 한 번에 다 읽든
- 스트리밍 어댑터가 `stream_scanner.go`에서 청크 단위로 읽든

호출부를 하나도 고치지 않고 동일하게 종료 시각이 잡힌다.

`RelayInfo`(`relay/common/relay_info.go`)에 `UpstreamRequestStartTime`, `UpstreamResponseEndTime` 필드와 `SetUpstreamRequestStart()`/`SetUpstreamResponseEnd()` 메서드를 `SetFirstResponseTime()`과 동일한 패턴(`sync.Once` 기반, 재시도 시 재호출 방지)으로 추가한다.

**재시도(retry) 처리**: 채널 재시도가 발생하면 `doRequest()`가 여러 번 호출될 수 있다. `llm_ms`는 "최종적으로 성공한(혹은 마지막) 시도"만 반영하도록 매 시도마다 값을 덮어쓴다 — 재시도 대기 시간까지 포함하면 게이트웨이 오버헤드가 커지는데, 이는 실제로 맞는 방향(재시도도 new-api가 흡수하는 비용이므로 `gateway_ms`에 자연히 포함됨).

### 3. 게이트웨이 오버헤드 계산 및 저장

응답 완료 후 (`service/text_quota.go`의 소비 로그 기록 지점, 기존 `UseTimeSeconds` 계산과 같은 자리) 다음을 계산해 `Log.Other` JSON에 추가한다.

```go
e2eMs := gatewayExitTime.Sub(gatewayEntryTime).Milliseconds()
other["e2e_ms"] = e2eMs

if relayInfo.UpstreamRequestStartTime가 설정되어 있으면 (업스트림 호출이 실제 발생한 경우) {
    llmMs := relayInfo.UpstreamResponseEndTime.Sub(relayInfo.UpstreamRequestStartTime).Milliseconds()
    other["llm_ms"] = llmMs
    other["gateway_ms"] = e2eMs - llmMs
    other["pre_llm_ms"] = relayInfo.UpstreamRequestStartTime.Sub(gatewayEntryTime).Milliseconds()
    other["post_llm_ms"] = gatewayExitTime.Sub(relayInfo.UpstreamResponseEndTime).Milliseconds()
} else {
    // 인증 실패 등 업스트림에 도달조차 못한 요청: llm_ms/gateway_ms 생략
}
```

기존 컬럼 마이그레이션 없음 (Rule 2 대상 아님) — `Other`는 이미 TEXT/JSON 컬럼으로 세 DB에 존재.

## 에러/타임아웃 케이스

- 업스트림 연결 실패·타임아웃: `client.Do()`가 에러를 반환해도 `SetUpstreamResponseEnd()`를 호출해 "실패까지 대기한 시간"을 `llm_ms`에 반영한다 (낭비된 시간이므로 0으로 지워버리지 않는다).
- 인증/rate-limit 등 게이트웨이 단계에서 막혀 업스트림 호출 자체가 없었던 요청: `llm_ms`, `gateway_ms`를 생략(omit)한다. 이 경우 `e2e_ms`만 기록되어 "LLM에 도달하지 못한 요청"과 "LLM 응답이 느렸던 요청"이 명확히 구분된다.

## 프론트엔드 (web/default만 반영, web/classic 미반영)

- **로그 목록 테이블**: 변경 없음. 기존 `Timing` 컬럼(`UseTime`/`FRT` 기반) 그대로 유지.
- **로그 상세 패널**: `other.e2e_ms`, `other.llm_ms`, `other.gateway_ms`가 존재하면 세 값을 나란히 텍스트로 표시하고, `gateway_ms / e2e_ms * 100`을 비율(%)로 함께 보여준다. 값이 없는 요청(게이트웨이 단계에서 막힘)은 이 섹션 자체를 표시하지 않는다.
- `pre_llm_ms`/`post_llm_ms`는 이번 단계에서는 UI에 노출하지 않고 데이터만 쌓아둔다. 추후 통계/집계용 별도 화면(관리자 대시보드 등)에서 활용할 수 있도록 필드명만 고정한다.

## 범위 밖 (Out of scope)

- 로그 목록 테이블 UI 변경 없음.
- Classic 웹(`web/classic`) 미반영.
- 별도 통계/집계 대시보드는 이번 작업 범위에 포함하지 않는다 (향후 별도 스펙).
- 클라이언트 TCP 소켓 레벨까지의 실제 도달 시각은 측정 불가(OS 버퍼링 등 한계) — `Write()` 완료 시점을 최선의 근사치로 사용.
