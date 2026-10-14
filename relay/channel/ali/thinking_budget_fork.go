package ali

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// fork: O11 — Ali thinking_budget either-name rule (team afbf87cd3, 70b14d049).

// aliThinkingBudgetContextKey holds the raw thinking_budget of the current Ali
// attempt when the generic marshaler will drop it. It is request scoped (gin
// context), read only by the Ali DoRequest, and consumed by it.
const aliThinkingBudgetContextKey = "fork_ali_thinking_budget"

// aliKeepsThinkingBudget is the team either-name rule (70b14d049): a request
// keeps thinking_budget when the client model or the mapped upstream model is
// a Qwen/QwQ model, so a Qwen client mapped to a custom deployment name keeps
// an explicit budget (including 0, which turns thinking off).
func aliKeepsThinkingBudget(info *relaycommon.RelayInfo) bool {
	return dto.IsQwenThinkingBudgetModel(info.OriginModelName) || dto.IsQwenThinkingBudgetModel(info.UpstreamModelName)
}

// stashAliThinkingBudget decides whether the converted request keeps
// thinking_budget and, when the generic marshaler (relaykit
// GeneralOpenAIRequest.MarshalJSON, which looks only at the post-mapping
// request.Model) would drop a kept budget, stashes its raw value so
// doAliRequest can put it back. Every call resets the stash, so an earlier
// attempt of a retried request never leaks into this one.
func stashAliThinkingBudget(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) bool {
	keep := aliKeepsThinkingBudget(info)
	if !keep && (info.OriginModelName == "" || info.UpstreamModelName == "") {
		// Upstream fell back to request.Model when a name is missing.
		keep = dto.IsQwenThinkingBudgetModel(request.Model)
	}
	if c == nil {
		return keep
	}
	var stash json.RawMessage
	if keep && len(request.ThinkingBudget) > 0 && !dto.IsQwenThinkingBudgetModel(request.Model) {
		stash = append(json.RawMessage(nil), request.ThinkingBudget...)
	}
	c.Set(aliThinkingBudgetContextKey, stash)
	return keep
}

// doAliRequest re-inserts a stashed thinking_budget after the generic request
// marshaler (relaykit, upstream) dropped it by the mapped model name. A body
// that already carries the field is sent unchanged. The rebuilt body is a
// bytes.Reader, for which net/http sets ContentLength and GetBody itself.
func doAliRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	value, _ := c.Get(aliThinkingBudgetContextKey)
	raw, _ := value.(json.RawMessage)
	if len(raw) == 0 {
		return channel.DoApiRequest(a, c, info, body)
	}
	c.Set(aliThinkingBudgetContextKey, json.RawMessage(nil))
	// A pass-through body skipped ConvertOpenAIRequest, so the stash belongs
	// to an earlier attempt that never reached DoRequest.
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		return channel.DoApiRequest(a, c, info, body)
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(data, "thinking_budget").Exists() {
		if data, err = sjson.SetRawBytes(data, "thinking_budget", raw); err != nil {
			return nil, err
		}
	}
	return channel.DoApiRequest(a, c, info, bytes.NewReader(data))
}
