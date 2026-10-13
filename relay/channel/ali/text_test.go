package ali

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

// TestRequestOpenAI2AliTopP reproduces a real bug: an omitted top_p was
// read via lo.FromPtrOr(request.TopP, 0), treating "not set" the same as
// "explicitly 0" and forcing it to near-greedy decoding (0.001) - sampling
// behavior the client never asked for.
func TestRequestOpenAI2AliTopP(t *testing.T) {
	tests := []struct {
		name string
		topP *float64
		want *float64
	}{
		{
			name: "omitted top_p is not injected",
			topP: nil,
			want: nil,
		},
		{
			name: "in-range top_p is preserved",
			topP: lo.ToPtr(0.8),
			want: lo.ToPtr(0.8),
		},
		{
			name: "top_p of 1 is clamped",
			topP: lo.ToPtr(1.0),
			want: lo.ToPtr(0.999),
		},
		{
			name: "top_p above 1 is clamped",
			topP: lo.ToPtr(1.5),
			want: lo.ToPtr(0.999),
		},
		{
			name: "top_p of 0 is clamped",
			topP: lo.ToPtr(0.0),
			want: lo.ToPtr(0.001),
		},
		{
			name: "negative top_p is clamped",
			topP: lo.ToPtr(-0.3),
			want: lo.ToPtr(0.001),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestOpenAI2Ali(dto.GeneralOpenAIRequest{
				Model: "qwen-plus",
				TopP:  tt.topP,
			}, "qwen-plus", "qwen-plus")

			assert.Equal(t, tt.want, got.TopP)
		})
	}
}

// thinking_budget is kept when either the origin or the upstream name is
// Qwen-like; an empty name falls back to request.Model.
func TestRequestOpenAI2AliThinkingBudgetNames(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		origin   string
		upstream string
		want     bool
	}{
		{"empty origin with qwen upstream", "my-deploy", "", "qwen3-max", true},
		{"qwen origin with empty upstream", "my-deploy", "qwen3-max", "", true},
		{"qwen origin with custom upstream", "my-deploy", "qwen3-max", "my-deploy", true},
		{"custom origin with qwen upstream", "qwen3-max", "my-alias", "qwen3-max", true},
		{"empty names fall back to qwen request model", "qwen-plus", "", "", true},
		{"neither name qwen", "deepseek-v3", "deepseek-v3", "deepseek-v3", false},
		{"empty names fall back to non-qwen request model", "deepseek-v3", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestOpenAI2Ali(dto.GeneralOpenAIRequest{
				Model:          tt.model,
				ThinkingBudget: json.RawMessage(`0`),
			}, tt.origin, tt.upstream)

			assert.Equal(t, tt.want, got.KeepThinkingBudget)
			assert.Equal(t, tt.want, got.ThinkingBudget != nil)
		})
	}
}
