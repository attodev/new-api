# E2E/LLM Timing Breakdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Measure and expose, per request, how much latency is new-api's own overhead vs. the upstream model provider's response time, so it can be used as customer-facing evidence.

**Architecture:** Add a gateway-entry timestamp via a new first-in-chain middleware that also wraps `gin.ResponseWriter` to record the last `Write()` call (= E2E end). Add upstream-call start/end timestamps at the single choke point all 42 channel adapters share (`doRequest()` in `relay/channel/api_request.go`), using a wrapping `io.ReadCloser` so streaming and non-streaming bodies are both covered without touching each adapter. Compute `e2e_ms`, `llm_ms`, `gateway_ms` (+ internal `pre_llm_ms`/`post_llm_ms`) in the existing `GenerateTextOtherInfo` and store them in `Log.Other` (no schema migration). Surface only `e2e_ms`/`llm_ms`/`gateway_ms` in the web/default log detail dialog.

**Tech Stack:** Go 1.22+ (Gin), React 19 + TypeScript (web/default), existing `Log.Other` JSON column.

**Reference design doc:** `docs/superpowers/specs/2026-07-02-timing-breakdown-design.md`

---

### Task 1: Add gateway-entry context key

**Files:**
- Modify: `constant/context_key.go:10-11`

- [ ] **Step 1: Add the new context key**

Current:
```go
	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"
```

New:
```go
	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"

	// ContextKeyGatewayEntryTime marks when the request first entered the
	// gateway (earliest middleware), used to compute true end-to-end latency
	// independent of auth/rate-limit/channel-selection overhead.
	ContextKeyGatewayEntryTime ContextKey = "gateway_entry_time"
```

- [ ] **Step 2: Build to confirm it compiles**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add constant/context_key.go
git commit -m "feat: add gateway entry time context key"
```

---

### Task 2: Timing response writer (captures E2E end = last Write())

**Files:**
- Create: `common/timing_response_writer.go`
- Create: `common/timing_response_writer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package common

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTimingResponseWriter_TracksLastWriteTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	require.True(t, writer.LastWriteTime().IsZero())

	before := time.Now()
	_, err := c.Writer.Write([]byte("chunk-1"))
	require.NoError(t, err)
	after := time.Now()

	got := writer.LastWriteTime()
	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

func TestTimingResponseWriter_WriteStringUpdatesTimestamp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	_, err := c.Writer.WriteString("hello")
	require.NoError(t, err)
	require.False(t, writer.LastWriteTime().IsZero())
}

func TestGetLastWriteTime_FallsBackWhenNotWrapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	before := time.Now()
	got := GetLastWriteTime(c)
	after := time.Now()

	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

func TestGetLastWriteTime_ReturnsWrappedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	_, err := c.Writer.Write([]byte("x"))
	require.NoError(t, err)

	require.Equal(t, writer.LastWriteTime(), GetLastWriteTime(c))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./common/... -run TestTimingResponseWriter -run TestGetLastWriteTime -v`
Expected: FAIL with "undefined: TimingResponseWriter" / "undefined: GetLastWriteTime"

- [ ] **Step 3: Implement**

```go
package common

import (
	"time"

	"github.com/gin-gonic/gin"
)

// TimingResponseWriter wraps gin.ResponseWriter to record the timestamp of
// the most recent Write()/WriteString() call. It is used to approximate the
// end-to-end latency's end point ("last byte written to the client"),
// including the final chunk of a streaming response.
type TimingResponseWriter struct {
	gin.ResponseWriter
	lastWrite time.Time
}

func (w *TimingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.lastWrite = time.Now()
	return n, err
}

func (w *TimingResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.lastWrite = time.Now()
	return n, err
}

// LastWriteTime returns the timestamp of the last Write()/WriteString()
// call. Zero value means nothing has been written yet.
func (w *TimingResponseWriter) LastWriteTime() time.Time {
	return w.lastWrite
}

// GetLastWriteTime returns the last-write timestamp recorded by a
// TimingResponseWriter installed on c.Writer. If c.Writer was never wrapped
// (e.g. non-relay routes), it falls back to time.Now() so callers always get
// a usable (if slightly less precise) value.
func GetLastWriteTime(c *gin.Context) time.Time {
	if w, ok := c.Writer.(*TimingResponseWriter); ok {
		if !w.LastWriteTime().IsZero() {
			return w.LastWriteTime()
		}
	}
	return time.Now()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./common/... -run TestTimingResponseWriter -run TestGetLastWriteTime -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add common/timing_response_writer.go common/timing_response_writer_test.go
git commit -m "feat: add TimingResponseWriter to track last-write timestamp"
```

---

### Task 3: RequestTiming middleware, registered first in the relay chain

**Files:**
- Create: `middleware/request_timing.go`
- Modify: `router/relay-router.go:14`

- [ ] **Step 1: Implement the middleware**

```go
package middleware

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// RequestTiming records the moment the gateway first sees the request and
// wraps the response writer so the moment the last byte is written to the
// client can be recovered later (see common.GetLastWriteTime). It must be
// registered before any other middleware so downstream auth/rate-limit/
// channel-selection time is included in the resulting e2e_ms metric.
func RequestTiming() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())
		c.Writer = &common.TimingResponseWriter{ResponseWriter: c.Writer}
		c.Next()
	}
}
```

- [ ] **Step 2: Register it as the very first middleware for relay routes**

Current (`router/relay-router.go:13-17`):
```go
func SetRelayRouter(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup())
	router.Use(middleware.StatsMiddleware())
```

New:
```go
func SetRelayRouter(router *gin.Engine) {
	router.Use(middleware.RequestTiming())
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup())
	router.Use(middleware.StatsMiddleware())
```

- [ ] **Step 3: Build and run the existing middleware/router tests**

Run: `go build ./... && go test ./middleware/... ./router/... -v`
Expected: all PASS (no existing test should reference the old middleware order)

- [ ] **Step 4: Commit**

```bash
git add middleware/request_timing.go router/relay-router.go
git commit -m "feat: add RequestTiming middleware as first relay middleware"
```

---

### Task 4: Upstream call timestamps on RelayInfo

**Files:**
- Modify: `relay/common/relay_info.go:96-98`, `relay/common/relay_info.go:482-483`, `relay/common/relay_info.go:654` (area)
- Modify: `relay/common/relay_info_test.go`

- [ ] **Step 1: Write the failing test**

Append to `relay/common/relay_info_test.go`:
```go
func TestRelayInfoSetUpstreamRequestStart(t *testing.T) {
	info := &RelayInfo{}
	require.True(t, info.UpstreamRequestStartTime.IsZero())

	info.SetUpstreamRequestStart()

	require.False(t, info.UpstreamRequestStartTime.IsZero())
}

func TestRelayInfoSetUpstreamResponseEnd(t *testing.T) {
	info := &RelayInfo{}
	require.True(t, info.UpstreamResponseEndTime.IsZero())

	info.SetUpstreamResponseEnd()

	require.False(t, info.UpstreamResponseEndTime.IsZero())
}

func TestRelayInfoUpstreamTimestamps_LatestAttemptWins(t *testing.T) {
	info := &RelayInfo{}

	info.SetUpstreamRequestStart()
	first := info.UpstreamRequestStartTime

	time.Sleep(time.Millisecond)
	info.SetUpstreamRequestStart() // simulates a retry attempt

	require.True(t, info.UpstreamRequestStartTime.After(first))
}
```

Add `"time"` to the test file's import block if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./relay/common/... -run TestRelayInfoSetUpstream -run TestRelayInfoUpstreamTimestamps -v`
Expected: FAIL with "info.UpstreamRequestStartTime undefined" (compile error)

- [ ] **Step 3: Add the fields**

Current (`relay/common/relay_info.go:88-98`):
```go
type RelayInfo struct {
	TokenId           int
	TokenKey          string
	TokenGroup        string
	UserId            int
	UsingGroup        string // auto
	UserGroup         string
	TokenUnlimited    bool
	StartTime         time.Time
	FirstResponseTime time.Time
	isFirstResponse   bool
```

New:
```go
type RelayInfo struct {
	TokenId           int
	TokenKey          string
	TokenGroup        string
	UserId            int
	UsingGroup        string // auto
	UserGroup         string
	TokenUnlimited    bool
	StartTime         time.Time
	FirstResponseTime time.Time
	isFirstResponse   bool

	// UpstreamRequestStartTime/UpstreamResponseEndTime bracket the actual
	// outbound HTTP call to the model provider (set in
	// relay/channel/api_request.go's doRequest()). On retry, each attempt
	// overwrites these with its own timestamps — only the final attempt's
	// timing is reported.
	UpstreamRequestStartTime time.Time
	UpstreamResponseEndTime  time.Time
```

- [ ] **Step 4: Add the setter methods**

Find `SetFirstResponseTime` (`relay/common/relay_info.go:654`):
```go
func (info *RelayInfo) SetFirstResponseTime() {
	if info.isFirstResponse {
		info.FirstResponseTime = time.Now()
		info.isFirstResponse = false
	}
}
```

Add immediately after it:
```go

// SetUpstreamRequestStart records when the outbound HTTP call to the model
// provider began. Safe to call multiple times across retries; the latest
// call wins.
func (info *RelayInfo) SetUpstreamRequestStart() {
	info.UpstreamRequestStartTime = time.Now()
}

// SetUpstreamResponseEnd records when the model provider's response body was
// fully read (EOF) or the call failed. Safe to call multiple times across
// retries; the latest call wins.
func (info *RelayInfo) SetUpstreamResponseEnd() {
	info.UpstreamResponseEndTime = time.Now()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./relay/common/... -run TestRelayInfoSetUpstream -run TestRelayInfoUpstreamTimestamps -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add relay/common/relay_info.go relay/common/relay_info_test.go
git commit -m "feat: track upstream request start/end timestamps on RelayInfo"
```

---

### Task 5: Timing-aware body reader for upstream responses

**Files:**
- Create: `relay/channel/timing_body.go`
- Create: `relay/channel/timing_body_test.go`

- [ ] **Step 1: Write the failing test**

```go
package channel

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type errCloser struct {
	io.Reader
	closeErr error
	closed   bool
}

func (e *errCloser) Close() error {
	e.closed = true
	return e.closeErr
}

func TestTimingReadCloser_CallsOnDoneOnceOnEOF(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader("hello")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	buf := make([]byte, 3)
	_, err := trc.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 0, calls, "must not fire before EOF")

	_, err = trc.Read(buf)
	require.NoError(t, err)

	_, err = trc.Read(buf)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, calls)

	// Further reads past EOF must not double-fire.
	_, _ = trc.Read(buf)
	require.Equal(t, 1, calls)
}

func TestTimingReadCloser_CallsOnDoneOnClose(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader("hello")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	require.NoError(t, trc.Close())
	require.Equal(t, 1, calls)
	require.True(t, rc.closed)
}

func TestTimingReadCloser_ReadErrorTriggersOnDoneOnce(t *testing.T) {
	calls := 0
	rc := &errCloser{Reader: strings.NewReader(""), closeErr: errors.New("boom")}
	trc := newTimingReadCloser(rc, func() { calls++ })

	buf := make([]byte, 3)
	_, err := trc.Read(buf)
	require.Error(t, err)
	require.Equal(t, 1, calls)

	err = trc.Close()
	require.EqualError(t, err, "boom")
	require.Equal(t, 1, calls, "onDone must not fire twice")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./relay/channel/... -run TestTimingReadCloser -v`
Expected: FAIL with "undefined: newTimingReadCloser"

- [ ] **Step 3: Implement**

```go
package channel

import (
	"io"
	"sync"
)

// timingReadCloser wraps an io.ReadCloser and invokes onDone exactly once,
// at whichever happens first: the wrapped reader returning any error
// (typically io.EOF) or Close() being called. This lets a single wrap point
// (doRequest) capture "upstream response fully consumed" for both streaming
// readers (which read to EOF chunk by chunk) and non-streaming callers
// (which io.ReadAll the whole body) without changing either call site.
type timingReadCloser struct {
	io.ReadCloser
	once   sync.Once
	onDone func()
}

func newTimingReadCloser(rc io.ReadCloser, onDone func()) *timingReadCloser {
	return &timingReadCloser{ReadCloser: rc, onDone: onDone}
}

func (t *timingReadCloser) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if err != nil {
		t.markDone()
	}
	return n, err
}

func (t *timingReadCloser) Close() error {
	t.markDone()
	return t.ReadCloser.Close()
}

func (t *timingReadCloser) markDone() {
	t.once.Do(t.onDone)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./relay/channel/... -run TestTimingReadCloser -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/timing_body.go relay/channel/timing_body_test.go
git commit -m "feat: add timingReadCloser to detect upstream body EOF once"
```

---

### Task 6: Wire timestamps into doRequest()

**Files:**
- Modify: `relay/channel/api_request.go:485-531`

- [ ] **Step 1: Write the failing test**

Add to `relay/channel/api_request_test.go`:
```go
func TestDoRequest_RecordsUpstreamTimingOnSuccess(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{}
	resp, err := DoRequest(ctx, req, info)
	require.NoError(t, err)
	require.False(t, info.UpstreamRequestStartTime.IsZero())
	require.True(t, info.UpstreamResponseEndTime.IsZero(), "end time is only set once the body is drained")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.NoError(t, resp.Body.Close())

	require.False(t, info.UpstreamResponseEndTime.IsZero())
	require.False(t, info.UpstreamResponseEndTime.Before(info.UpstreamRequestStartTime))
}

func TestDoRequest_RecordsUpstreamTimingOnFailure(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:0", nil)
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{}
	_, err = DoRequest(ctx, req, info)
	require.Error(t, err)
	require.False(t, info.UpstreamRequestStartTime.IsZero())
	require.False(t, info.UpstreamResponseEndTime.IsZero())
}
```

Add `"io"` to the test file's imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./relay/channel/... -run TestDoRequest_RecordsUpstreamTiming -v`
Expected: FAIL (`UpstreamRequestStartTime`/`UpstreamResponseEndTime` stay zero because `doRequest` doesn't set them yet)

- [ ] **Step 3: Implement**

Current (`relay/channel/api_request.go:485-531`):
```go
func doRequest(c *gin.Context, req *http.Request, info *common.RelayInfo) (*http.Response, error) {
	var client *http.Client
	var err error
	if info.ChannelSetting.Proxy != "" {
		client, err = service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		client = service.GetHttpClient()
	}

	var stopPinger context.CancelFunc
	if info.IsStream {
		helper.SetEventStreamHeaders(c)
		// ping
		generalSettings := operation_setting.GetGeneralSetting()
		if generalSettings.PingIntervalEnabled && !info.DisablePing {
			pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
			stopPinger = startPingKeepAlive(c, pingInterval)
			// deferping goroutine
			defer func() {
				if stopPinger != nil {
					stopPinger()
					logger.LogDebug(c, "SSE ping goroutine stopped by defer")
				}
			}()
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c, "do request failed: "+err.Error())
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithHideErrMsg("upstream error: do request failed"))
	}
	if resp == nil {
		return nil, errors.New("resp is nil")
	}

	if upID := resp.Header.Get(common2.RequestIdKey); upID != "" {
		c.Set(common2.UpstreamRequestIdKey, upID)
	}

	_ = req.Body.Close()
	_ = c.Request.Body.Close()
	return resp, nil
}
```

New:
```go
func doRequest(c *gin.Context, req *http.Request, info *common.RelayInfo) (*http.Response, error) {
	var client *http.Client
	var err error
	if info.ChannelSetting.Proxy != "" {
		client, err = service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		client = service.GetHttpClient()
	}

	var stopPinger context.CancelFunc
	if info.IsStream {
		helper.SetEventStreamHeaders(c)
		// ping
		generalSettings := operation_setting.GetGeneralSetting()
		if generalSettings.PingIntervalEnabled && !info.DisablePing {
			pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
			stopPinger = startPingKeepAlive(c, pingInterval)
			// deferping goroutine
			defer func() {
				if stopPinger != nil {
					stopPinger()
					logger.LogDebug(c, "SSE ping goroutine stopped by defer")
				}
			}()
		}
	}

	info.SetUpstreamRequestStart()
	resp, err := client.Do(req)
	if err != nil {
		info.SetUpstreamResponseEnd()
		logger.LogError(c, "do request failed: "+err.Error())
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithHideErrMsg("upstream error: do request failed"))
	}
	if resp == nil {
		info.SetUpstreamResponseEnd()
		return nil, errors.New("resp is nil")
	}

	if upID := resp.Header.Get(common2.RequestIdKey); upID != "" {
		c.Set(common2.UpstreamRequestIdKey, upID)
	}

	resp.Body = newTimingReadCloser(resp.Body, info.SetUpstreamResponseEnd)

	_ = req.Body.Close()
	_ = c.Request.Body.Close()
	return resp, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./relay/channel/... -run TestDoRequest_RecordsUpstreamTiming -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite to check for regressions**

Run: `go test ./relay/channel/... -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add relay/channel/api_request.go relay/channel/api_request_test.go
git commit -m "feat: record upstream call start/end timing in doRequest"
```

---

### Task 7: Compute e2e_ms/llm_ms/gateway_ms in GenerateTextOtherInfo

**Files:**
- Modify: `service/log_info_generate.go:1-53`
- Create: `service/log_info_generate_test.go`

- [ ] **Step 1: Write the failing test**

```go
package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfo_TimingBreakdown_FullPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)

	relayInfo := &relaycommon.RelayInfo{}
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(2 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	_, err := c.Writer.Write([]byte("done"))
	require.NoError(t, err)

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	e2eMs, ok := other["e2e_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, e2eMs, int64(0))

	llmMs, ok := other["llm_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, llmMs, int64(2))

	gatewayMs, ok := other["gateway_ms"].(int64)
	require.True(t, ok)
	require.Equal(t, e2eMs-llmMs, gatewayMs)

	require.Contains(t, other, "pre_llm_ms")
	require.Contains(t, other, "post_llm_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsLlmWhenNoUpstreamCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())

	relayInfo := &relaycommon.RelayInfo{} // no upstream call happened

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	require.Contains(t, other, "e2e_ms")
	require.NotContains(t, other, "llm_ms")
	require.NotContains(t, other, "gateway_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsE2eWhenNoGatewayEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	// No ContextKeyGatewayEntryTime set (e.g. request never went through
	// RequestTiming middleware, such as in some internal/test call paths).

	relayInfo := &relaycommon.RelayInfo{}

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	require.NotContains(t, other, "e2e_ms")
	require.NotContains(t, other, "llm_ms")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./service/... -run TestGenerateTextOtherInfo_TimingBreakdown -v`
Expected: FAIL (`other["e2e_ms"]` etc. don't exist yet)

- [ ] **Step 3: Implement**

Current (`service/log_info_generate.go:36-53`):
```go
func GenerateTextOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64, modelPrice float64, userGroupRatio float64) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_ratio"] = modelRatio
	other["group_ratio"] = groupRatio
	other["completion_ratio"] = completionRatio
	other["cache_tokens"] = cacheTokens
	other["cache_ratio"] = cacheRatio
	other["model_price"] = modelPrice
	other["user_group_ratio"] = userGroupRatio
	other["frt"] = float64(relayInfo.FirstResponseTime.UnixMilli() - relayInfo.StartTime.UnixMilli())
	if relayInfo.ReasoningEffort != "" {
		other["reasoning_effort"] = relayInfo.ReasoningEffort
	}
	if relayInfo.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = relayInfo.UpstreamModelName
	}
```

New:
```go
func GenerateTextOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64, modelPrice float64, userGroupRatio float64) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_ratio"] = modelRatio
	other["group_ratio"] = groupRatio
	other["completion_ratio"] = completionRatio
	other["cache_tokens"] = cacheTokens
	other["cache_ratio"] = cacheRatio
	other["model_price"] = modelPrice
	other["user_group_ratio"] = userGroupRatio
	other["frt"] = float64(relayInfo.FirstResponseTime.UnixMilli() - relayInfo.StartTime.UnixMilli())
	appendTimingBreakdown(ctx, relayInfo, other)
	if relayInfo.ReasoningEffort != "" {
		other["reasoning_effort"] = relayInfo.ReasoningEffort
	}
	if relayInfo.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = relayInfo.UpstreamModelName
	}
```

Add this new function after `appendRequestPath` (`service/log_info_generate.go`, right before `GenerateTextOtherInfo`):
```go
// appendTimingBreakdown splits total request latency into the portion spent
// waiting on the model provider (llm_ms) vs. everything new-api itself adds
// (gateway_ms = e2e_ms - llm_ms). e2e_ms spans from the earliest gateway
// middleware to the last byte written to the client, so it captures auth,
// rate-limiting, channel selection, and response transformation/transport —
// not just the distributor-onward window that UseTime measures.
//
// llm_ms/gateway_ms/pre_llm_ms/post_llm_ms are omitted when no upstream call
// was made (e.g. the request was rejected before reaching a channel), so
// "blocked before reaching a provider" stays distinguishable from "provider
// was slow" in the stored data.
func appendTimingBreakdown(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if ctx == nil || relayInfo == nil || other == nil {
		return
	}
	gatewayEntryTime := common.GetContextKeyTime(ctx, constant.ContextKeyGatewayEntryTime)
	if gatewayEntryTime.IsZero() {
		return
	}
	gatewayExitTime := common.GetLastWriteTime(ctx)

	e2eMs := gatewayExitTime.Sub(gatewayEntryTime).Milliseconds()
	if e2eMs < 0 {
		e2eMs = 0
	}
	other["e2e_ms"] = e2eMs

	if relayInfo.UpstreamRequestStartTime.IsZero() || relayInfo.UpstreamResponseEndTime.IsZero() {
		return
	}

	llmMs := relayInfo.UpstreamResponseEndTime.Sub(relayInfo.UpstreamRequestStartTime).Milliseconds()
	if llmMs < 0 {
		llmMs = 0
	}
	other["llm_ms"] = llmMs
	other["gateway_ms"] = e2eMs - llmMs
	other["pre_llm_ms"] = relayInfo.UpstreamRequestStartTime.Sub(gatewayEntryTime).Milliseconds()
	other["post_llm_ms"] = gatewayExitTime.Sub(relayInfo.UpstreamResponseEndTime).Milliseconds()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./service/... -run TestGenerateTextOtherInfo_TimingBreakdown -v`
Expected: PASS

- [ ] **Step 5: Run the full service package suite to check for regressions**

Run: `go test ./service/... -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add service/log_info_generate.go service/log_info_generate_test.go
git commit -m "feat: compute e2e/llm/gateway timing breakdown in log Other data"
```

---

### Task 8: Full backend build + vet

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 2: Vet everything**

Run: `go vet ./...`
Expected: no warnings related to changed files

- [ ] **Step 3: Run the full backend test suite**

Run: `go test ./... 2>&1 | tail -60`
Expected: no new failures (pre-existing unrelated failures, if any, are out of scope)

No commit for this task — it's a checkpoint.

---

### Task 9: Frontend types — add timing fields to LogOtherData

**Files:**
- Modify: `web/default/src/features/usage-logs/types.ts:96-142`

- [ ] **Step 1: Add the new optional fields**

Current (`web/default/src/features/usage-logs/types.ts:140-142`):
```ts
  audio_ratio?: number
  audio_completion_ratio?: number
  frt?: number
```

New:
```ts
  audio_ratio?: number
  audio_completion_ratio?: number
  frt?: number
  // Timing breakdown (ms). e2e_ms spans gateway entry to the last byte
  // written to the client; llm_ms is the upstream provider call only;
  // gateway_ms = e2e_ms - llm_ms is new-api's own overhead. Omitted when
  // the request never reached an upstream provider.
  e2e_ms?: number
  llm_ms?: number
  gateway_ms?: number
```

- [ ] **Step 2: Type-check**

Run: `cd web/default && bun run typecheck` (or `bunx tsc --noEmit` if no dedicated script — check `package.json` first with `cat web/default/package.json | grep -A3 '"scripts"'`)
Expected: no new type errors

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/usage-logs/types.ts
git commit -m "feat(web): add timing breakdown fields to LogOtherData"
```

---

### Task 10: i18n keys for the new detail-panel labels

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/kr.json`

- [ ] **Step 1: Add English keys**

In `web/default/src/i18n/locales/en.json`, find the alphabetically-sorted block containing `"Response Time": "Response Time",` and add these four keys in alphabetical order across the file (search for the correct alphabetical spot for each — do not simply cluster them together):

```json
    "Gateway Overhead": "Gateway Overhead",
```
(near existing "G..." entries)

```json
    "Model Provider": "Model Provider",
```
(near existing "M..." entries)

```json
    "Timing Breakdown": "Timing Breakdown",
    "Total (End-to-End)": "Total (End-to-End)",
```
(near existing "T..." entries)

- [ ] **Step 2: Add matching Korean keys**

In `web/default/src/i18n/locales/kr.json`, add the same four keys (same English key on the left, Korean translation on the right) in the same alphabetical positions as `en.json`:

```json
    "Gateway Overhead": "게이트웨이 오버헤드",
    "Model Provider": "모델 제공사",
    "Timing Breakdown": "시간 분석",
    "Total (End-to-End)": "전체 (E2E)",
```

- [ ] **Step 3: Run the i18n sync/lint tooling**

Run: `cd web/default && bun run i18n:sync`
Expected: exits cleanly; if it reformats/reorders the files, that's expected — re-check the four new keys are still present with `grep -n "Gateway Overhead\|Model Provider\|Timing Breakdown\|Total (End-to-End)" src/i18n/locales/en.json src/i18n/locales/kr.json`

- [ ] **Step 4: Commit**

```bash
git add web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json
git commit -m "feat(web): add i18n keys for timing breakdown labels"
```

---

### Task 11: Show the timing breakdown in the log detail dialog

**Files:**
- Modify: `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx:792-826`

- [ ] **Step 1: Add the new detail section right after the existing "Response Time" row**

Current (`web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx:792-827`):
```tsx
              {showTiming && props.log.use_time > 0 && (
                <DetailRow
                  label={t('Response Time')}
                  value={
                    <span
                      className={cn(
                        'font-medium',
                        timingTextColorClass(
                          getResponseTimeColor(
                            props.log.use_time,
                            props.log.completion_tokens
                          )
                        )
                      )}
                    >
                      {formatUseTime(props.log.use_time)}
                      {props.log.is_stream &&
                        other?.frt != null &&
                        other.frt > 0 && (
                          <span
                            className={cn(
                              'font-normal',
                              timingTextColorClass(
                                getFirstResponseTimeColor(other.frt / 1000)
                              )
                            )}
                          >
                            {' '}
                            (FRT: {formatUseTime(other.frt / 1000)})
                          </span>
                        )}
                    </span>
                  }
                />
              )}
            </div>
```

New:
```tsx
              {showTiming && props.log.use_time > 0 && (
                <DetailRow
                  label={t('Response Time')}
                  value={
                    <span
                      className={cn(
                        'font-medium',
                        timingTextColorClass(
                          getResponseTimeColor(
                            props.log.use_time,
                            props.log.completion_tokens
                          )
                        )
                      )}
                    >
                      {formatUseTime(props.log.use_time)}
                      {props.log.is_stream &&
                        other?.frt != null &&
                        other.frt > 0 && (
                          <span
                            className={cn(
                              'font-normal',
                              timingTextColorClass(
                                getFirstResponseTimeColor(other.frt / 1000)
                              )
                            )}
                          >
                            {' '}
                            (FRT: {formatUseTime(other.frt / 1000)})
                          </span>
                        )}
                    </span>
                  }
                />
              )}
            </div>

            {showTiming &&
              other?.e2e_ms != null &&
              other?.llm_ms != null &&
              other?.gateway_ms != null && (
                <DetailSection label={t('Timing Breakdown')}>
                  <DetailRow
                    label={t('Total (End-to-End)')}
                    value={formatUseTime(other.e2e_ms / 1000)}
                    mono
                  />
                  <DetailRow
                    label={t('Model Provider')}
                    value={formatUseTime(other.llm_ms / 1000)}
                    mono
                  />
                  <DetailRow
                    label={t('Gateway Overhead')}
                    value={`${formatUseTime(other.gateway_ms / 1000)} (${
                      other.e2e_ms > 0
                        ? ((other.gateway_ms / other.e2e_ms) * 100).toFixed(1)
                        : '0.0'
                    }%)`}
                    mono
                  />
                </DetailSection>
              )}
```

- [ ] **Step 2: Type-check and lint**

Run: `cd web/default && bunx tsc --noEmit`
Expected: no new errors

- [ ] **Step 3: Manual verification**

Run: `cd web/default && bun run dev`, open the app, go to Usage Logs, click into a chat-completion log entry, and confirm:
- The existing "Response Time" row is unchanged.
- A new "Timing Breakdown" section appears below it with Total / Model Provider / Gateway Overhead rows and a sane percentage.
- For a log predating this change (no `e2e_ms` in `other`), the new section does not render and nothing else breaks.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx
git commit -m "feat(web): show timing breakdown in log detail dialog"
```

---

### Task 12: End-to-end manual verification against a real request

**Files:** none (verification only)

- [ ] **Step 1: Start the backend**

Run: `go run main.go` (or the project's existing run command)

- [ ] **Step 2: Send a real chat completion request through the gateway**

```bash
curl -s http://localhost:3000/v1/chat/completions \
  -H "Authorization: Bearer <a-real-token>" \
  -H "Content-Type: application/json" \
  -d '{"model":"<a-configured-model>","messages":[{"role":"user","content":"hi"}]}' | head -c 500
```

- [ ] **Step 3: Inspect the resulting log's Other JSON directly in the DB**

Run (SQLite example, adjust for the project's actual DB):
```bash
sqlite3 one-api.db "SELECT other FROM logs ORDER BY id DESC LIMIT 1;"
```
Expected: JSON contains `e2e_ms`, `llm_ms`, `gateway_ms`, `pre_llm_ms`, `post_llm_ms`, with `gateway_ms` small (tens of ms) relative to `llm_ms`, and `e2e_ms == llm_ms + gateway_ms`.

- [ ] **Step 4: Repeat with a streaming request (`"stream": true`) and confirm the same fields populate correctly, with `llm_ms` roughly matching the full stream duration rather than just time-to-first-token.**

- [ ] **Step 5: Confirm in the web/default UI that the new detail-dialog section shows values consistent with the DB row inspected above.**

No commit for this task — it's a checkpoint confirming the whole feature works end-to-end.
