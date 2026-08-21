package deepseek

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

// TestGetRequestURL_Responses reproduces a real feature gap: DeepSeek added
// a /v1/responses endpoint, but the adaptor's URL builder had no case for
// RelayModeResponses and fell through to the chat completions URL - a
// client using the Responses relay format against a DeepSeek channel would
// hit the wrong endpoint.
func TestGetRequestURL_Responses(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.deepseek.com",
		},
	}

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.deepseek.com/responses", url)
}

// TestConvertOpenAIResponsesRequest_NotImplemented reproduces the other half
// of the same gap: ConvertOpenAIResponsesRequest unconditionally returned
// "not implemented", so even with the right URL the request body could
// never be built - /v1/responses was completely unusable on DeepSeek
// channels.
func TestConvertOpenAIResponsesRequest_NotImplemented(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "deepseek-chat",
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "deepseek-chat", request.Model)
}

// TestConvertOpenAIResponsesRequest_AppliesV4ThinkingSuffix verifies the
// DeepSeek-V4 "-max"/"-none" model suffix (already handled for the chat
// completions and Claude relay paths) is applied consistently for the
// Responses path too: the suffix is stripped from the model name and
// translated into reasoning.effort.
func TestConvertOpenAIResponsesRequest_AppliesV4ThinkingSuffix(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-chat-max",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-chat-max",
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "deepseek-v4-chat", request.Model)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "max", request.Reasoning.Effort)
	require.Equal(t, "deepseek-v4-chat", info.UpstreamModelName)
	require.Equal(t, "max", info.ReasoningEffort)
}

// TestConvertOpenAIResponsesRequest_AppliesV4DisabledThinkingSuffix covers
// the "-none" suffix, which maps to an empty effort from the shared parser
// but must be normalized to the explicit "none" reasoning effort value.
func TestConvertOpenAIResponsesRequest_AppliesV4DisabledThinkingSuffix(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-chat-none",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-chat-none",
	})
	require.NoError(t, err)

	request, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "deepseek-v4-chat", request.Model)
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "none", request.Reasoning.Effort)
}
