package deepseek

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

// Ported from team adaptor_responses_test.go (aaf9a515f).

// The Responses relay mode targets DeepSeek's /responses endpoint, not chat
// completions.
func TestGetRequestURLResponsesFork(t *testing.T) {
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

// A Responses request without a DeepSeek-V4 suffix passes through unchanged.
func TestConvertOpenAIResponsesRequestPassthroughFork(t *testing.T) {
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
	require.Nil(t, request.Reasoning)
}

// The DeepSeek-V4 "-max" suffix is stripped from the model name and sent as
// reasoning.effort, as on the chat completions and Claude paths.
func TestConvertOpenAIResponsesRequestAppliesV4ThinkingSuffixFork(t *testing.T) {
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

// "-none" parses to disabled thinking with no effort and is sent as the
// explicit "none" effort.
func TestConvertOpenAIResponsesRequestAppliesV4DisabledThinkingSuffixFork(t *testing.T) {
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
	require.Equal(t, "deepseek-v4-chat", info.UpstreamModelName)
	require.Equal(t, "none", info.ReasoningEffort)
}
