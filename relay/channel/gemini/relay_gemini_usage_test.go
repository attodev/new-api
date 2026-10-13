package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiStreamHandlerCompletionTokensExcludeToolUsePromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiTextGenerationHandlerPromptTokensIncludeToolUsePromptTokens(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			ToolUsePromptTokenCount: 18329,
			CandidatesTokenCount:    1089,
			ThoughtsTokenCount:      1120,
			TotalTokenCount:         20689,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 18480, usage.PromptTokens)
	require.Equal(t, 2209, usage.CompletionTokens)
	require.Equal(t, 20689, usage.TotalTokens)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiChatHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiStreamHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "partial"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

func TestGeminiTextGenerationHandlerUsesEstimatedPromptTokensWhenUsagePromptMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-flash-preview:generateContent", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "ok"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        0,
			ToolUsePromptTokenCount: 0,
			CandidatesTokenCount:    90,
			ThoughtsTokenCount:      10,
			TotalTokenCount:         110,
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}

	usage, newAPIError := GeminiTextGenerationHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 100, usage.CompletionTokens)
	require.Equal(t, 110, usage.TotalTokens)
}

// Gemini stream usageMetadata is a cumulative snapshot on every frame, so the
// final usage must come from the last frame instead of summing all frames.
func TestGeminiChatStreamHandlerCumulativeUsageTakesLastFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}

	textDetails := []dto.GeminiPromptTokensDetails{
		{Modality: "TEXT", TokenCount: 1089},
	}
	frames := []dto.GeminiChatResponse{
		{
			Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "a"}}}}},
			UsageMetadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     1200,
				CandidatesTokenCount: 10,
				TotalTokenCount:      1210,
				PromptTokensDetails:  textDetails,
			},
		},
		{
			Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "b"}}}}},
			UsageMetadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     1200,
				CandidatesTokenCount: 20,
				TotalTokenCount:      1220,
				PromptTokensDetails:  textDetails,
			},
		},
		{
			Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "c"}}}}},
			UsageMetadata: dto.GeminiUsageMetadata{
				PromptTokenCount:     1200,
				CandidatesTokenCount: 30,
				TotalTokenCount:      1230,
				PromptTokensDetails:  textDetails,
			},
		},
	}

	var streamBody []byte
	for _, frame := range frames {
		data, err := common.Marshal(frame)
		require.NoError(t, err)
		streamBody = append(streamBody, []byte("data: "+string(data)+"\n\n")...)
	}
	streamBody = append(streamBody, []byte("data: [DONE]\n")...)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader(streamBody)),
	}

	usage, newAPIError := GeminiChatStreamHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 1089, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 1200, usage.PromptTokens)
	require.Equal(t, 30, usage.CompletionTokens)
	require.Equal(t, 1230, usage.TotalTokens)
}

// A stream whose last usageMetadata only carries prompt tokens (for example a
// client abort before the final chunk) estimates only the output; the
// upstream-reported prompt and cached tokens are billed as reported.
// An agent turn that only calls a tool still produced output tokens.
func TestGeminiStreamHandlerPromptOnlyUsageMetadataEstimatesCompletionTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	for _, tc := range []struct {
		name      string
		part      dto.GeminiPart
		usageText string
	}{
		{"text", dto.GeminiPart{Text: "partial streamed answer before disconnect"}, "partial streamed answer before disconnect"},
		{"function call", dto.GeminiPart{FunctionCall: &dto.FunctionCall{FunctionName: "read_file", Arguments: map[string]any{"path": "main.go"}}}, `read_file{"path":"main.go"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			info := &relaycommon.RelayInfo{
				OriginModelName: "gemini-3-flash-preview",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "gemini-3-flash-preview",
				},
			}
			info.SetEstimatePromptTokens(20)

			chunk := dto.GeminiChatResponse{
				Candidates: []dto.GeminiChatCandidate{
					{
						Content: dto.GeminiChatContent{
							Role:  "model",
							Parts: []dto.GeminiPart{tc.part},
						},
					},
				},
				UsageMetadata: dto.GeminiUsageMetadata{
					PromptTokenCount:        151,
					CachedContentTokenCount: 100,
					TotalTokenCount:         151,
				},
			}

			chunkData, err := common.Marshal(chunk)
			require.NoError(t, err)

			streamBody := []byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n")
			resp := &http.Response{
				Body: io.NopCloser(bytes.NewReader(streamBody)),
			}

			usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
				return true
			})
			require.Nil(t, newAPIError)
			require.NotNil(t, usage)
			require.Equal(t, 151, usage.PromptTokens)
			require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
			require.Equal(t, tokenkit.Estimate("gemini-3-flash-preview", tc.usageText), usage.CompletionTokens)
			require.Greater(t, usage.CompletionTokens, 0)
			require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
		})
	}
}

// A final frame that reports the prompt but a zero total (and no candidates)
// still keeps the reported prompt; only the completion is estimated.
func TestGeminiStreamHandlerKeepsPromptWhenTotalTokenCountIsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(20)

	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{FunctionCall: &dto.FunctionCall{FunctionName: "read_file", Arguments: map[string]any{"path": "main.go"}}},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     151,
			CandidatesTokenCount: 0,
			TotalTokenCount:      0,
		},
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)

	resp := &http.Response{
		Body: io.NopCloser(bytes.NewReader([]byte("data: " + string(chunkData) + "\n" + "data: [DONE]\n"))),
	}

	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 151, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

// Non-stream responses get the same treatment: a function-call-only turn whose
// usageMetadata reports the prompt but no candidates keeps the reported prompt
// and cached tokens and estimates only the completion.
func TestGeminiNonStreamHandlersPromptOnlyUsageMetadataEstimatesCompletionTokens(t *testing.T) {
	const model = "gemini-3-flash-preview"
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{FunctionCall: &dto.FunctionCall{FunctionName: "read_file", Arguments: map[string]any{"path": "main.go"}}},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        151,
			CachedContentTokenCount: 100,
			TotalTokenCount:         151,
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		format  types.RelayFormat
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"chat", types.RelayFormatOpenAI, GeminiChatHandler},
		{"native", types.RelayFormatGemini, GeminiTextGenerationHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			info := &relaycommon.RelayInfo{
				RelayFormat:     tc.format,
				OriginModelName: model,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: model},
			}
			info.SetEstimatePromptTokens(20)

			usage, newAPIError := tc.handler(c, info, &http.Response{Body: io.NopCloser(bytes.NewReader(body))})
			require.Nil(t, newAPIError)
			require.NotNil(t, usage)
			require.Equal(t, 151, usage.PromptTokens)
			require.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
			require.Equal(t, tokenkit.Estimate(model, `read_file{"path":"main.go"}`), usage.CompletionTokens)
			require.Greater(t, usage.CompletionTokens, 0)
			require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
		})
	}
}

func geminiModalityUsageMetadata(prompt int, promptDetails, toolUseDetails []dto.GeminiPromptTokensDetails, toolUse int) dto.GeminiUsageMetadata {
	return dto.GeminiUsageMetadata{
		PromptTokenCount:           prompt,
		ToolUsePromptTokenCount:    toolUse,
		CandidatesTokenCount:       10,
		TotalTokenCount:            prompt + toolUse + 10,
		PromptTokensDetails:        promptDetails,
		ToolUsePromptTokensDetails: toolUseDetails,
	}
}

func runGeminiChatHandlerForUsage(t *testing.T, metadata dto.GeminiUsageMetadata) *dto.Usage {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3-flash-preview"},
	}
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "ok"}}},
		}},
		UsageMetadata: metadata,
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	usage, newAPIError := GeminiChatHandler(c, info, &http.Response{Body: io.NopCloser(bytes.NewReader(body))})
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	return usage
}

func runGeminiStreamHandlerForUsage(t *testing.T, metadata dto.GeminiUsageMetadata) *dto.Usage {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3-flash-preview"},
	}
	chunk := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "partial"}}},
		}},
		UsageMetadata: metadata,
	}
	chunkData, err := common.Marshal(chunk)
	require.NoError(t, err)
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte("data: " + string(chunkData) + "\ndata: [DONE]\n")))}
	usage, newAPIError := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool { return true })
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	return usage
}

func TestGeminiHandlersAttributeMixedPromptModalities(t *testing.T) {
	metadata := geminiModalityUsageMetadata(2000, []dto.GeminiPromptTokensDetails{
		{Modality: "TEXT", TokenCount: 800},
		{Modality: "IMAGE", TokenCount: 1089},
		{Modality: "VIDEO", TokenCount: 100},
		{Modality: "DOCUMENT", TokenCount: 11},
	}, nil, 0)

	for name, run := range map[string]func(*testing.T, dto.GeminiUsageMetadata) *dto.Usage{
		"chat":   runGeminiChatHandlerForUsage,
		"stream": runGeminiStreamHandlerForUsage,
	} {
		t.Run(name, func(t *testing.T) {
			usage := run(t, metadata)
			require.Equal(t, 2000, usage.PromptTokens)
			require.Equal(t, 1089, usage.PromptTokensDetails.ImageTokens)
			require.Equal(t, 911, usage.PromptTokensDetails.TextTokens)
			require.Equal(t, 0, usage.PromptTokensDetails.AudioTokens)
		})
	}
}

func TestGeminiHandlersAttributeImageOnlyPrompt(t *testing.T) {
	metadata := geminiModalityUsageMetadata(1500, []dto.GeminiPromptTokensDetails{
		{Modality: "IMAGE", TokenCount: 1089},
	}, nil, 0)

	for name, run := range map[string]func(*testing.T, dto.GeminiUsageMetadata) *dto.Usage{
		"chat":   runGeminiChatHandlerForUsage,
		"stream": runGeminiStreamHandlerForUsage,
	} {
		t.Run(name, func(t *testing.T) {
			usage := run(t, metadata)
			require.Equal(t, 1500, usage.PromptTokens)
			require.Equal(t, 1089, usage.PromptTokensDetails.ImageTokens)
			require.Equal(t, 1500-1089, usage.PromptTokensDetails.TextTokens)
			require.Equal(t, 0, usage.PromptTokensDetails.AudioTokens)
		})
	}
}

func TestBuildUsageFromGeminiMetadataToolUseImageTokens(t *testing.T) {
	usage := buildUsageFromGeminiMetadata(geminiModalityUsageMetadata(
		200,
		[]dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 200}},
		[]dto.GeminiPromptTokensDetails{{Modality: " image ", TokenCount: 700}, {Modality: "text", TokenCount: 100}},
		800,
	), 0)
	require.Equal(t, 1000, usage.PromptTokens)
	require.Equal(t, 700, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 300, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 0, usage.PromptTokensDetails.AudioTokens)
}

func TestBuildUsageFromGeminiMetadataPromptDetailsAlwaysSumToPromptTokens(t *testing.T) {
	// Audio + image only, no text-class detail: remainder is billed as text.
	usage := buildUsageFromGeminiMetadata(geminiModalityUsageMetadata(1000, []dto.GeminiPromptTokensDetails{
		{Modality: "AUDIO", TokenCount: 300},
		{Modality: "IMAGE", TokenCount: 500},
	}, nil, 0), 0)
	details := usage.PromptTokensDetails
	require.Equal(t, 200, details.TextTokens)
	require.Equal(t, usage.PromptTokens, details.TextTokens+details.AudioTokens+details.ImageTokens)

	// No details at all: everything is text.
	usage = buildUsageFromGeminiMetadata(geminiModalityUsageMetadata(1000, nil, nil, 0), 0)
	require.Equal(t, 1000, usage.PromptTokensDetails.TextTokens)

	// Detail tokens exceeding the prompt count never produce negative text.
	usage = buildUsageFromGeminiMetadata(geminiModalityUsageMetadata(100, []dto.GeminiPromptTokensDetails{
		{Modality: "IMAGE", TokenCount: 500},
	}, nil, 0), 0)
	require.Equal(t, 0, usage.PromptTokensDetails.TextTokens)
}

func TestBuildUsageFromGeminiMetadataCapsImageTokensToUncachedPrompt(t *testing.T) {
	metadata := geminiModalityUsageMetadata(2000, []dto.GeminiPromptTokensDetails{
		{Modality: "TEXT", TokenCount: 911},
		{Modality: "IMAGE", TokenCount: 1089},
	}, nil, 0)
	metadata.CachedContentTokenCount = 1500

	usage := buildUsageFromGeminiMetadata(metadata, 0)
	details := usage.PromptTokensDetails
	require.Equal(t, 1500, details.CachedTokens)
	require.Equal(t, 500, details.ImageTokens)
	require.Equal(t, 1500, details.TextTokens)
	require.Equal(t, usage.PromptTokens, details.TextTokens+details.AudioTokens+details.ImageTokens)
	require.LessOrEqual(t, details.CachedTokens+details.ImageTokens, usage.PromptTokens)

	// Cache fully covering the prompt leaves no image tokens to price.
	metadata.CachedContentTokenCount = 2000
	usage = buildUsageFromGeminiMetadata(metadata, 0)
	require.Equal(t, 0, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 2000, usage.PromptTokensDetails.TextTokens)

	// Cache that does not overlap the image tokens leaves them untouched.
	metadata.CachedContentTokenCount = 900
	usage = buildUsageFromGeminiMetadata(metadata, 0)
	require.Equal(t, 1089, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 911, usage.PromptTokensDetails.TextTokens)
}
