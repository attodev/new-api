package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// fallbackCompletionText is the text counted when the upstream omits usage.
// Tool calls are most of an agent turn's output, so they are counted along
// with content and reasoning: custom tool calls by their raw payload, function
// calls by name plus arguments.
func fallbackCompletionText(msg *dto.Message) string {
	var text strings.Builder
	text.WriteString(msg.StringContent())
	text.WriteString(msg.GetReasoningContent())
	for _, toolCall := range msg.ParseToolCalls() {
		if len(toolCall.Custom) > 0 {
			text.Write(toolCall.Custom)
			continue
		}
		text.WriteString(toolCall.Function.Name)
		text.WriteString(toolCall.Function.Arguments)
	}
	return text.String()
}
