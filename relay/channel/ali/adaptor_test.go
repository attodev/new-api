package ali

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestMappedAliImageModelUsesUpstreamProtocol reproduces a real bug: URL
// routing, header setup, and request conversion all checked
// info.OriginModelName (the client-facing name before channel model
// mapping) instead of info.UpstreamModelName - a mapped Qwen image model
// could hit the wrong DashScope endpoint/protocol entirely.
func TestMappedAliImageModelUsesUpstreamProtocol(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       constant.RelayModeImagesGenerations,
		OriginModelName: "customer-image-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://dashscope.aliyuncs.com",
			UpstreamModelName: "qwen-image-3.0-pro",
		},
	}

	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", url)

	header := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Empty(t, header.Get("X-DashScope-Async"))

	converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
		Model:  info.UpstreamModelName,
		Prompt: "poster",
	})
	require.NoError(t, err)
	assert.True(t, adaptor.IsSyncImageModel)
	assert.IsType(t, &AliImageRequest{}, converted)
}

func TestAliImageHandlerHonorsRequestResponseFormat(t *testing.T) {
	imageBytes := []byte("ali-image")
	var downloads atomic.Int32
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	t.Cleanup(imageServer.Close)

	fetchSetting := system_setting.GetFetchSetting()
	require.NotNil(t, fetchSetting)
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() {
		*fetchSetting = originalFetchSetting
	})
	originalMaxFileDownloadMB := rootconstant.MaxFileDownloadMB
	rootconstant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		rootconstant.MaxFileDownloadMB = originalMaxFileDownloadMB
	})
	service.InitHttpClient()

	tests := []struct {
		name           string
		responseFormat string
		wantBase64     string
		wantDownloads  int32
	}{
		{
			name:           "base64",
			responseFormat: "b64_json",
			wantBase64:     base64.StdEncoding.EncodeToString(imageBytes),
			wantDownloads:  1,
		},
		{
			name:           "url",
			responseFormat: "url",
		},
		{
			name: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloads.Store(0)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			info := &relaycommon.RelayInfo{
				RelayMode: constant.RelayModeImagesGenerations,
				StartTime: time.Unix(1, 0),
				Request: &dto.ImageRequest{
					ResponseFormat: tt.responseFormat,
				},
			}
			responseBody := fmt.Sprintf(`{"output":{"results":[{"url":%q}]}}`, imageServer.URL)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}

			newAPIError, usage := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, resp, info)
			require.Nil(t, newAPIError)
			require.NotNil(t, usage)

			var imageResponse dto.ImageResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &imageResponse))
			require.Len(t, imageResponse.Data, 1)
			assert.Equal(t, imageServer.URL, imageResponse.Data[0].Url)
			assert.Equal(t, tt.wantBase64, imageResponse.Data[0].B64Json)
			assert.Equal(t, tt.wantDownloads, downloads.Load())
		})
	}
}

func TestConvertOpenAIRequestFiltersThinkingBudgetByUpstreamModel(t *testing.T) {
	tests := []struct {
		name          string
		requestModel  string
		upstreamModel string
		budget        string
		wantBudget    bool
		wantValue     int64
	}{
		{
			name:          "qwen",
			requestModel:  "qwen-plus",
			upstreamModel: "qwen-plus",
			budget:        "128",
			wantBudget:    true,
			wantValue:     128,
		},
		{
			name:          "qwq explicit zero",
			requestModel:  "qwq-32b",
			upstreamModel: "qwq-32b",
			budget:        "0",
			wantBudget:    true,
			wantValue:     0,
		},
		{
			name:          "neither client nor upstream is qwen",
			requestModel:  "my-alias",
			upstreamModel: "deepseek-r1",
			budget:        "128",
			wantBudget:    false,
		},
		{
			// Either name being Qwen-like keeps the budget (see requestOpenAI2Ali).
			name:          "qwen client mapped to unsupported upstream keeps budget",
			requestModel:  "qwen-plus",
			upstreamModel: "deepseek-r1",
			budget:        "128",
			wantBudget:    true,
			wantValue:     128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Model:          tt.requestModel,
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: json.RawMessage(tt.budget),
			}
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: tt.upstreamModel,
				},
			}

			convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
			require.NoError(t, err)
			converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			if tt.wantBudget {
				assert.Equal(t, tt.budget, string(converted.ThinkingBudget))
			} else {
				assert.Nil(t, converted.ThinkingBudget)
			}

			encoded, err := common.Marshal(converted)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.Equal(t, tt.wantBudget, value.Exists())
			if tt.wantBudget {
				assert.Equal(t, tt.wantValue, value.Int())
			}
		})
	}
}

func TestConvertOpenAIRequestPreservesExplicitZeroForMappedQwenModel(t *testing.T) {
	const (
		clientModel   = "customer-model"
		upstreamModel = "Qwen/Qwen3-235B-A22B-Thinking-2507"
	)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"customer-model":"Qwen/Qwen3-235B-A22B-Thinking-2507"}`)

	request := &dto.GeneralOpenAIRequest{
		Model:          clientModel,
		EnableThinking: json.RawMessage(`true`),
		ThinkingBudget: json.RawMessage(`0`),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: clientModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: clientModel,
		},
	}

	err := relayhelper.ModelMappedHelper(c, info, request)
	require.NoError(t, err)
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, upstreamModel, info.UpstreamModelName)
	assert.Equal(t, upstreamModel, request.Model)

	convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, json.RawMessage(`0`), converted.ThinkingBudget)

	encoded, err := common.Marshal(converted)
	require.NoError(t, err)

	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists())
	assert.Equal(t, int64(0), value.Int())
}

func convertAliChatBody(t *testing.T, clientModel, mapping, rawBody string) []byte {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if mapping != "" {
		c.Set("model_mapping", mapping)
	}

	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(rawBody), &request))
	info := &relaycommon.RelayInfo{
		OriginModelName: clientModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: clientModel,
		},
	}
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, &request))

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	return encoded
}

// A Qwen client model mapped to a custom deployment name (no "qwen" in it) must
// keep thinking_budget: both the Ali conversion and the generic request
// marshaler used to decide by the upstream name alone and dropped it.
func TestConvertOpenAIRequestKeepsThinkingBudgetForQwenClientMappedToCustomName(t *testing.T) {
	encoded := convertAliChatBody(t, "qwen3-max", `{"qwen3-max":"my-qwen-deploy"}`,
		`{"model":"qwen3-max","messages":[{"role":"user","content":"hi"}],"top_p":1,"enable_thinking":true,"thinking_budget":0}`)

	assert.Equal(t, "my-qwen-deploy", gjson.GetBytes(encoded, "model").String())
	value := gjson.GetBytes(encoded, "thinking_budget")
	assert.True(t, value.Exists(), string(encoded))
	assert.Equal(t, int64(0), value.Int())
	assert.Contains(t, string(encoded), `"thinking_budget":0`)
	assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
	assert.Equal(t, "hi", gjson.GetBytes(encoded, "messages.0.content").String())
	assert.InDelta(t, 0.999, gjson.GetBytes(encoded, "top_p").Float(), 1e-9)
}

// A non-Qwen client model on Ali never gets thinking_budget upstream.
func TestConvertOpenAIRequestDropsThinkingBudgetForNonQwenClientModel(t *testing.T) {
	encoded := convertAliChatBody(t, "deepseek-v3", "",
		`{"model":"deepseek-v3","messages":[{"role":"user","content":"hi"}],"thinking_budget":100}`)

	assert.False(t, gjson.GetBytes(encoded, "thinking_budget").Exists(), string(encoded))
	assert.Equal(t, "hi", gjson.GetBytes(encoded, "messages.0.content").String())
}

// compatible_handler injects ChannelSetting.SystemPrompt only when the
// converted request is a *dto.GeneralOpenAIRequest, and the conversion chain is
// recorded through GuessRelayFormatFromRequest. The Ali adaptor must keep
// returning that concrete type or both are silently skipped.
func TestConvertOpenAIRequestReturnsGeneralOpenAIRequest(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:    "qwen-plus",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "qwen-plus",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "qwen-plus"},
	}

	convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "got %T", convertedValue)

	// Mirrors the SystemPrompt injection in relay/compatible_handler.go.
	converted.Messages = append([]dto.Message{{Role: converted.GetSystemRoleName(), Content: "be brief"}}, converted.Messages...)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.Equal(t, "be brief", gjson.GetBytes(encoded, "messages.0.content").String())

	format, ok := relaycommon.GuessRelayFormatFromRequest(convertedValue)
	assert.True(t, ok)
	assert.Equal(t, types.RelayFormatOpenAI, format)
}
