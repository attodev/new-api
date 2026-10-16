package ollama

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

// Ported from team relay_ollama_request_test.go (73b87c518).

// reasoning_effort maps onto Ollama's own "think" field.
func TestOpenAIChatToOllamaChatMapsReasoningEffortToThinkFork(t *testing.T) {
	cases := []struct {
		name      string
		effort    string
		wantThink string
	}{
		{name: "high effort maps to string", effort: "high", wantThink: `"high"`},
		{name: "none effort maps to false", effort: "none", wantThink: `false`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model:           "llama3.1",
				ReasoningEffort: tc.effort,
				Messages:        []dto.Message{{Role: "user", Content: "hi"}},
			}
			got, err := openAIChatToOllamaChat(nil, req)
			require.NoError(t, err)
			require.Equal(t, tc.wantThink, string(got.Think))
		})
	}
}

// An assistant message's reasoning content is carried on Ollama's "thinking".
func TestOpenAIChatToOllamaChatPreservesAssistantThinkingFork(t *testing.T) {
	reasoning := "let me think about this"
	req := &dto.GeneralOpenAIRequest{
		Model: "llama3.1",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello", ReasoningContent: &reasoning},
		},
	}
	got, err := openAIChatToOllamaChat(nil, req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	require.JSONEq(t, `"let me think about this"`, string(got.Messages[1].Thinking))
}

// A tool result keeps its tool_call_id, and its tool name is backfilled from
// the matching assistant tool call when the client omits it.
func TestOpenAIChatToOllamaChatPreservesToolCallIDRoundTripFork(t *testing.T) {
	toolCallsJSON := `[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{}"}}]`
	req := &dto.GeneralOpenAIRequest{
		Model: "llama3.1",
		Messages: []dto.Message{
			{Role: "user", Content: "weather in paris?"},
			{Role: "assistant", ToolCalls: []byte(toolCallsJSON)},
			{Role: "tool", ToolCallId: "call_abc", Content: "18C, sunny"},
		},
	}
	got, err := openAIChatToOllamaChat(nil, req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)

	toolResultMsg := got.Messages[2]
	require.Equal(t, "call_abc", toolResultMsg.ToolCallID)
	require.Equal(t, "get_weather", toolResultMsg.ToolName, "tool name must be backfilled from the matching assistant tool call when the client omits it")
}
