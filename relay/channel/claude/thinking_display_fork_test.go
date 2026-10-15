package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAdaptiveThinkingDisplayFork(t *testing.T) {
	budget := 2048
	cases := []struct {
		name     string
		thinking *dto.Thinking
		want     string
	}{
		{name: "adaptive without display gets the summary", thinking: &dto.Thinking{Type: "adaptive"}, want: "summarized"},
		{name: "explicit omitted wins", thinking: &dto.Thinking{Type: "adaptive", Display: "omitted"}, want: "omitted"},
		{name: "explicit summarized stays", thinking: &dto.Thinking{Type: "adaptive", Display: "summarized"}, want: "summarized"},
		{name: "budget thinking is untouched", thinking: &dto.Thinking{Type: "enabled", BudgetTokens: &budget}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			NormalizeAdaptiveThinkingDisplay(tc.thinking)
			require.Equal(t, tc.want, tc.thinking.Display)
		})
	}
	require.NotPanics(t, func() { NormalizeAdaptiveThinkingDisplay(nil) })
}

// Chat Completions has no knob for the thinking summary, and a reasoning
// effort renders adaptive thinking without display, which Opus 4.7+ answers
// with empty thinking blocks.
func TestConvertOpenAIRequestAdaptiveThinkingIsSummarizedFork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request := &dto.GeneralOpenAIRequest{
		Model:           "claude-opus-4-7",
		ReasoningEffort: "high",
		Messages:        []dto.Message{{Role: "user", Content: "hi"}},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		Request:         request,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: request.Model},
	}

	out, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	claudeRequest, ok := out.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
	require.Equal(t, "summarized", claudeRequest.Thinking.Display)
}
