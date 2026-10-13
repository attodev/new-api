package aws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestFormatRequest_TrimsAnthropicBetaEntries(t *testing.T) {
	header := http.Header{}
	header.Add("anthropic-beta", "fast-mode-2026-02-01, thinking-binding-controls-2026-08-01 ,, ")
	req, err := formatRequest(bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`), header)
	require.NoError(t, err)

	var betas []string
	require.NoError(t, common.Unmarshal(req.AnthropicBeta, &betas))
	require.Equal(t, []string{"fast-mode-2026-02-01", "thinking-binding-controls-2026-08-01"}, betas)

	req, err = formatRequest(bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`), http.Header{"Anthropic-Beta": {" , "}})
	require.NoError(t, err)
	require.Empty(t, req.AnthropicBeta)
}

func TestDoAwsClientRequest_ForwardsThinkingBlockBindingAndBetaList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Header.Set("anthropic-beta", "fast-mode-2026-02-01, thinking-binding-controls-2026-08-01")

	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-5-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-opus-5-5",
		},
	}
	requestBody := bytes.NewBufferString(`{"max_tokens":128,"messages":[{"role":"user","content":"hello"}],` +
		`"thinking":{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"}}}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)
	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload struct {
		AnthropicBeta []string        `json:"anthropic_beta"`
		Thinking      json.RawMessage `json:"thinking"`
	}
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))
	require.Equal(t, []string{"fast-mode-2026-02-01", "thinking-binding-controls-2026-08-01"}, payload.AnthropicBeta)
	require.JSONEq(t, `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"}}`, string(payload.Thinking))
}
