package dto

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOpenAIRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"stream":false,
		"max_tokens":0,
		"max_completion_tokens":0,
		"top_p":0,
		"top_k":0,
		"n":0,
		"frequency_penalty":0,
		"presence_penalty":0,
		"seed":0,
		"logprobs":false,
		"top_logprobs":0,
		"dimensions":0,
		"return_images":false,
		"return_related_questions":false
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_completion_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_k").Exists())
	require.True(t, gjson.GetBytes(encoded, "n").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "seed").Exists())
	require.True(t, gjson.GetBytes(encoded, "logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "dimensions").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_images").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_related_questions").Exists())
}

func TestOpenAIResponsesRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"max_output_tokens":0,
		"max_tool_calls":0,
		"stream":false,
		"top_p":0,
		"frequency_penalty":0,
		"presence_penalty":0
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
}

// TestGeneralOpenAIRequestGetSystemRoleName reproduces a real bug: a bare
// strings.HasPrefix(model, "o") over-matches any model name starting with
// "o" (e.g. "omni-moderation-latest"), silently mangling its system role
// (and, in the adaptor, its temperature/max_tokens handling) as if it were
// an o1/o3/o4 reasoning model.
func TestGeneralOpenAIRequestPreserveQwenThinkingBudget(t *testing.T) {
	raw := []byte(`{
		"model":"qwen-plus",
		"thinking_budget":0
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(0), value.Int())
}

func TestGeneralOpenAIRequestPreserveQwQThinkingBudget(t *testing.T) {
	req := GeneralOpenAIRequest{
		Model:          "QwQ-32B",
		ThinkingBudget: json.RawMessage(`128`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(128), value.Int())
}

func TestGeneralOpenAIRequestDropsThinkingBudgetForNonQwenModel(t *testing.T) {
	req := GeneralOpenAIRequest{
		Model:          "gpt-4.1",
		ThinkingBudget: json.RawMessage(`128`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	assert.False(t, gjson.GetBytes(encoded, "thinking_budget").Exists())
}

// The custom MarshalJSON must not change how the rest of the request is
// encoded, and must work when marshaled through a pointer as well.
func TestGeneralOpenAIRequestMarshalJSONKeepsOtherFields(t *testing.T) {
	req := &GeneralOpenAIRequest{
		Model:          "gpt-4.1",
		ThinkingBudget: json.RawMessage(`128`),
		EnableThinking: json.RawMessage(`true`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4.1", gjson.GetBytes(encoded, "model").String())
	assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
	assert.False(t, gjson.GetBytes(encoded, "thinking_budget").Exists())
	// the caller's struct is untouched (value receiver)
	assert.Equal(t, json.RawMessage(`128`), req.ThinkingBudget)
}

func TestIsQwenThinkingBudgetModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "qwen-plus", want: true},
		{model: "Qwen/Qwen3-235B-A22B-Thinking-2507", want: true},
		{model: "qwq-32b", want: true},
		{model: "provider/qwen-plus", want: true},
		{model: "provider/qwq-32b", want: true},
		{model: "gpt-4.1", want: false},
		{model: "deepseek-r1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, IsQwenThinkingBudgetModel(tt.model))
		})
	}
}

func TestOpenAIResponsesRequestPreserveQwenThinkingBudget(t *testing.T) {
	req := OpenAIResponsesRequest{
		Model:          "qwen-plus",
		ThinkingBudget: json.RawMessage(`0`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(0), value.Int())
}

func TestOpenAIResponsesRequestPreserveQwQThinkingBudget(t *testing.T) {
	req := OpenAIResponsesRequest{
		Model:          "provider/QwQ-32B",
		ThinkingBudget: json.RawMessage(`128`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(128), value.Int())
}

func TestOpenAIResponsesRequestDropsThinkingBudgetForNonQwenModel(t *testing.T) {
	req := OpenAIResponsesRequest{
		Model:          "gpt-4.1",
		ThinkingBudget: json.RawMessage(`128`),
	}

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	assert.False(t, gjson.GetBytes(encoded, "thinking_budget").Exists())
}

func TestGeneralOpenAIRequestGetSystemRoleName(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{name: "o1 uses developer", model: "o1", want: "developer"},
		{name: "o3 family uses developer", model: "o3-mini-high", want: "developer"},
		{name: "o4 family uses developer", model: "o4-mini", want: "developer"},
		{name: "o1 mini stays system", model: "o1-mini", want: "system"},
		{name: "o1 preview stays system", model: "o1-preview", want: "system"},
		{name: "gpt 5 uses developer", model: "gpt-5", want: "developer"},
		{name: "gpt 5.6 uses developer", model: "gpt-5.6-luna", want: "developer"},
		{name: "gpt 6 uses developer", model: "gpt-6-astra", want: "developer"},
		{name: "gpt 6 snapshot uses developer", model: "gpt-6-astra-2026-09-03", want: "developer"},
		{name: "unknown gpt 6 variant stays system", model: "gpt-6-astra-pro", want: "system"},
		{name: "invalid gpt 6 snapshot stays system", model: "gpt-6-astra-2026-99-03", want: "system"},
		{name: "unknown generation stays system", model: "gpt-7", want: "system"},
		{name: "gpt 4.1 stays system", model: "gpt-4.1-nano", want: "system"},
		{name: "omni is not o series", model: "omni-moderation-latest", want: "system"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := GeneralOpenAIRequest{Model: tt.model}

			require.Equal(t, tt.want, req.GetSystemRoleName())
		})
	}
}

func TestIsOpenAIGPT5Model(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5", want: true},
		{model: "gpt-5-mini", want: true},
		{model: "gpt-5-chat-latest", want: true},
		{model: "gpt-5.6-luna", want: true},
		{model: "gpt-5.4-nano", want: true},
		{model: "gpt-5.2-2025-12-11", want: true},
		{model: "gpt-6-astra", want: false},
		{model: "gpt-6-sol", want: false},
		{model: "gpt-6-luna", want: false},
		{model: "gpt-7-x", want: false},
		{model: "gpt-50", want: false},
		{model: "gpt-5custom", want: false},
		{model: " GPT-5 ", want: false},
		{model: "gpt-4.1", want: false},
		{model: "gpt-4.1-nano", want: false},
		{model: "gpt-4o", want: false},
		{model: "gpt-4.5-preview", want: false},
		{model: "gpt-oss-120b", want: false},
		{model: "gpt-image-2", want: false},
		{model: "gpt-realtime", want: false},
		{model: "gpt-realtime-2.1", want: false},
		{model: "chatgpt-4o-latest", want: false},
		{model: "o3-mini", want: false},
		{model: "gpt-", want: false},
		{model: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			require.Equal(t, tt.want, IsOpenAIGPT5Model(tt.model))
		})
	}
}

func TestGetOpenAIChatCapabilities(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		effort        string
		wantMaxCompl  bool
		wantDeveloper bool
		wantTemp      bool
		wantTopP      bool
		wantLogProbs  bool
	}{
		{name: "gpt-6-astra", model: "gpt-6-astra", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-6-astra none", model: "gpt-6-astra", effort: "none", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-6-astra snapshot", model: "gpt-6-astra-2026-09-03", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-6-sol default", model: "gpt-6-sol", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-6-sol none", model: "gpt-6-sol", effort: "none", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-6-sol high", model: "gpt-6-sol", effort: "high", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-6-luna default", model: "gpt-6-luna", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-6-luna high", model: "gpt-6-luna", effort: "high", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-6 unknown variant", model: "gpt-6-astra-pro", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-7-x unchanged", model: "gpt-7-x", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5", model: "gpt-5", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-5.1 default", model: "gpt-5.1", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5.1 none", model: "gpt-5.1", effort: "none", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5.1 low", model: "gpt-5.1", effort: "low", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-5.2 default", model: "gpt-5.2", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5.2 snapshot", model: "gpt-5.2-2025-12-11", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5.2 pro variant", model: "gpt-5.2-pro", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-5.2 chat variant", model: "gpt-5.2-chat-latest", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-5.4 default", model: "gpt-5.4", wantMaxCompl: true, wantDeveloper: true, wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-5.4 high", model: "gpt-5.4", effort: "high", wantMaxCompl: true, wantDeveloper: true},
		{name: "gpt-5.6-luna", model: "gpt-5.6-luna", wantMaxCompl: true, wantDeveloper: true},
		{name: "o3-mini drops temperature only", model: "o3-mini", wantMaxCompl: true, wantDeveloper: true, wantTopP: true, wantLogProbs: true},
		{name: "o1-mini keeps system role", model: "o1-mini", wantMaxCompl: true, wantTopP: true, wantLogProbs: true},
		{name: "o1-preview keeps system role", model: "o1-preview", wantMaxCompl: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-4.1", model: "gpt-4.1", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-4o", model: "gpt-4o", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-oss-120b", model: "gpt-oss-120b", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-image-1", model: "gpt-image-1", wantTemp: true, wantTopP: true, wantLogProbs: true},
		{name: "gpt-realtime", model: "gpt-realtime", wantTemp: true, wantTopP: true, wantLogProbs: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := OpenAIChatCapabilities{
				UseMaxCompletionTokens: tt.wantMaxCompl,
				UseDeveloperRole:       tt.wantDeveloper,
				SupportsTemperature:    tt.wantTemp,
				SupportsTopP:           tt.wantTopP,
				SupportsLogProbs:       tt.wantLogProbs,
			}
			require.Equal(t, want, GetOpenAIChatCapabilities(tt.model, tt.effort))
		})
	}
}

func TestIsOpenAIModelSnapshot(t *testing.T) {
	require.True(t, isOpenAIModelSnapshot("gpt-6-astra", "gpt-6-astra"))
	require.True(t, isOpenAIModelSnapshot("gpt-6-astra-2026-09-03", "gpt-6-astra"))
	require.False(t, isOpenAIModelSnapshot("gpt-6-astra-pro", "gpt-6-astra"))
	require.False(t, isOpenAIModelSnapshot("gpt-6-astra-2026-99-03", "gpt-6-astra"))
	require.False(t, isOpenAIModelSnapshot("gpt-6-astra2", "gpt-6-astra"))
}
