package ollama

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// TestOpenAIChatToOllamaChat_MapsReasoningEffortToThink reproduces a real
// gap: reasoning_effort (or the Responses-style reasoning.effort) was never
// mapped onto Ollama's own "think" field, so a client requesting a specific
// reasoning effort had no way to reach Ollama's think toggle at all.
func TestOpenAIChatToOllamaChat_MapsReasoningEffortToThink(t *testing.T) {
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

// TestOpenAIChatToOllamaChat_PreservesAssistantThinking reproduces a real
// gap: an assistant message's reasoning content was dropped when
// round-tripping through Ollama's chat format instead of being carried on
// the message's "thinking" field.
func TestOpenAIChatToOllamaChat_PreservesAssistantThinking(t *testing.T) {
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

// TestOpenAIChatToOllamaChat_PreservesToolCallIDRoundTrip reproduces a real
// gap: a tool-result message's tool_call_id was never forwarded to Ollama's
// own tool_call_id field, and the tool's name had to come from message.Name
// even when the client omitted it (Ollama tool-result messages don't always
// repeat the name), breaking multi-tool conversations.
func TestOpenAIChatToOllamaChat_PreservesToolCallIDRoundTrip(t *testing.T) {
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
