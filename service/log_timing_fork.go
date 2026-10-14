package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// fork: O7 — ported from team service/log_info_generate.go.
//
// appendTimingBreakdown splits total request latency into the portion spent
// waiting on the model provider (llm_ms) vs. everything new-api itself adds
// (gateway_ms = e2e_ms - llm_ms). e2e_ms spans from the earliest gateway
// middleware (middleware.RequestTiming) to the last byte written to the
// client (common.GetLastWriteTime), so it captures auth, rate-limiting,
// channel selection, and response transformation/transport — not just the
// distributor-onward window that UseTime measures.
//
// llm_ms/gateway_ms/pre_llm_ms/post_llm_ms are omitted when no upstream call
// was recorded (e.g. the request was rejected before reaching a channel, or
// the adaptor bypassed relay/channel.doRequest), so "blocked before reaching
// a provider" stays distinguishable from "provider was slow" in the stored
// data. Nothing is recorded when no response byte went through the
// TimingResponseWriter (unwrapped writer, or a hijacked realtime WebSocket
// whose frames bypass it) — "now - entry" would be session age, not latency.
// e2e_ms/llm_ms/gateway_ms are public; pre_llm_ms/post_llm_ms are internal
// phases and go to the admin scope. All values are int64 milliseconds and
// negative values are floored
// to zero (e.g. a client that disconnects makes the last client Write happen
// before the upstream body is closed).
//
// Known limitation (retries): RelayInfo's upstream timestamps (see
// relay/common/relay_info.go) are plain overwrites, so on a retried request
// they only reflect the final attempt, while e2e_ms spans the whole request
// including earlier failed attempts on other channels. gateway_ms therefore
// absorbs that failed-attempt time as if it were new-api's own overhead,
// even though it was really time spent waiting on (other) upstream
// providers. This is acceptable for the common case (first-attempt
// success) but means gateway_ms can overstate new-api's overhead on
// requests that needed a retry.
func appendTimingBreakdown(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other *model.LogOther) {
	if ctx == nil || relayInfo == nil || other == nil {
		return
	}
	gatewayEntryTime := common.GetContextKeyTime(ctx, constant.ContextKeyGatewayEntryTime)
	if gatewayEntryTime.IsZero() {
		return
	}
	gatewayExitTime, ok := common.GetLastWriteTime(ctx)
	if !ok {
		return
	}

	e2eMs := nonNegativeMs(gatewayExitTime.Sub(gatewayEntryTime).Milliseconds())
	other.SetPublic("e2e_ms", e2eMs)

	if relayInfo.UpstreamRequestStartTime.IsZero() || relayInfo.UpstreamResponseEndTime.IsZero() {
		return
	}

	llmMs := nonNegativeMs(relayInfo.UpstreamResponseEndTime.Sub(relayInfo.UpstreamRequestStartTime).Milliseconds())
	other.SetPublic("llm_ms", llmMs)
	other.SetPublic("gateway_ms", nonNegativeMs(e2eMs-llmMs))
	// Internal phases: admin scope only.
	other.SetAdmin("pre_llm_ms", nonNegativeMs(relayInfo.UpstreamRequestStartTime.Sub(gatewayEntryTime).Milliseconds()))
	other.SetAdmin("post_llm_ms", nonNegativeMs(gatewayExitTime.Sub(relayInfo.UpstreamResponseEndTime).Milliseconds()))
}

func nonNegativeMs(ms int64) int64 {
	if ms < 0 {
		return 0
	}
	return ms
}
