package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// convertNativeClaudeForUpstream runs the steps ClaudeHelper takes between the
// client body and the upstream JSON (non pass-through).
func convertNativeClaudeForUpstream(t *testing.T, apiType int, body string) []byte {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	var original dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &original))
	info := &relaycommon.RelayInfo{
		OriginModelName: original.Model,
		Request:         &original,
		ChannelMeta:     &relaycommon.ChannelMeta{ApiType: apiType, UpstreamModelName: original.Model},
	}
	request, err := common.DeepCopy(&original)
	require.NoError(t, err)
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	require.NoError(t, helper.ApplyReasoningModelSuffix(c, info, request))
	adaptor := GetAdaptor(info.ApiType)
	require.NotNil(t, adaptor)
	adaptor.Init(info)
	converted, err := adaptor.ConvertClaudeRequest(c, info, request)
	require.NoError(t, err)
	jsonData, err := common.Marshal(converted)
	require.NoError(t, err)
	return jsonData
}

func TestNativeClaudeAdaptiveThinkingDisplayFork(t *testing.T) {
	apiTypes := map[string]int{
		"anthropic": constant.APITypeAnthropic,
		"aws":       constant.APITypeAws,
		"vertex":    constant.APITypeVertexAi,
	}
	cases := []struct {
		name     string
		thinking string
		want     string
	}{
		{name: "client sets no display", thinking: `{"type":"adaptive"}`, want: "summarized"},
		{name: "explicit omitted stays", thinking: `{"type":"adaptive","display":"omitted"}`, want: "omitted"},
	}
	for apiName, apiType := range apiTypes {
		for _, tc := range cases {
			t.Run(apiName+"/"+tc.name, func(t *testing.T) {
				body := `{"model":"claude-opus-4-7","max_tokens":1024,"thinking":` + tc.thinking +
					`,"messages":[{"role":"user","content":"hi"}]}`
				upstream := convertNativeClaudeForUpstream(t, apiType, body)
				require.Equal(t, "adaptive", gjson.GetBytes(upstream, "thinking.type").String())
				require.Equal(t, tc.want, gjson.GetBytes(upstream, "thinking.display").String())
			})
		}
	}
}

// AWS and Vertex convert Chat Completions to Claude themselves rather than via
// the Anthropic adaptor, so each needs the same default.
func TestOpenAICompatibleClaudeAdaptiveThinkingDisplayFork(t *testing.T) {
	apiTypes := map[string]int{
		"anthropic": constant.APITypeAnthropic,
		"aws":       constant.APITypeAws,
		"vertex":    constant.APITypeVertexAi,
	}
	for apiName, apiType := range apiTypes {
		t.Run(apiName, func(t *testing.T) {
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
				ChannelMeta:     &relaycommon.ChannelMeta{ApiType: apiType, UpstreamModelName: request.Model},
			}
			adaptor := GetAdaptor(apiType)
			require.NotNil(t, adaptor)
			adaptor.Init(info)
			converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
			require.NoError(t, err)
			upstream, err := common.Marshal(converted)
			require.NoError(t, err)
			require.Equal(t, "adaptive", gjson.GetBytes(upstream, "thinking.type").String())
			require.Equal(t, "summarized", gjson.GetBytes(upstream, "thinking.display").String())
		})
	}
}
