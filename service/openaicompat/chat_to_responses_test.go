package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatCompletionsRequestToResponsesRequestPreservesPenalties reproduces a
// real bug: converting a chat completions request into a Responses request
// silently dropped frequency_penalty/presence_penalty, so an OpenAI-compatible
// upstream (e.g. vLLM) accessed through the Responses relay path never saw
// values the client explicitly set - including explicit zero, which must be
// distinguished from "not set" per this repo's pointer-preservation rule.
func TestChatCompletionsRequestToResponsesRequestPreservesPenalties(t *testing.T) {
	tests := []struct {
		name          string
		frequency     *float64
		frequencyWant json.RawMessage
		presence      *float64
		presenceWant  json.RawMessage
	}{
		{
			name:          "positive values",
			frequency:     lo.ToPtr(0.5),
			frequencyWant: json.RawMessage(`0.5`),
			presence:      lo.ToPtr(1.5),
			presenceWant:  json.RawMessage(`1.5`),
		},
		{
			name:          "explicit zero values",
			frequency:     lo.ToPtr(0.0),
			frequencyWant: json.RawMessage(`0`),
			presence:      lo.ToPtr(0.0),
			presenceWant:  json.RawMessage(`0`),
		},
		{
			name:      "unset stays nil",
			frequency: nil,
			presence:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
				Model:            "gpt-test",
				Messages:         []dto.Message{{Role: "user", Content: "hello"}},
				FrequencyPenalty: tt.frequency,
				PresencePenalty:  tt.presence,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.frequencyWant, got.FrequencyPenalty)
			assert.Equal(t, tt.presenceWant, got.PresencePenalty)
		})
	}
}
