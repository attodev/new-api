package ali

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// aliUpstream records the body of every request it receives.
type aliUpstream struct {
	server *httptest.Server
	bodies [][]byte
}

func newAliUpstream(t *testing.T) *aliUpstream {
	t.Helper()
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	u := &aliUpstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, int64(len(body)), r.ContentLength)
		u.bodies = append(u.bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *aliUpstream) last(t *testing.T) []byte {
	t.Helper()
	require.NotEmpty(t, u.bodies)
	return u.bodies[len(u.bodies)-1]
}

func newAliRelayContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

// sendAliChat runs one Ali attempt the way relay/compatible_handler.go does:
// model mapping, ConvertOpenAIRequest, common.Marshal, optional body edit
// (stands in for RemoveDisabledFields/ParamOverride), then DoRequest.
func sendAliChat(t *testing.T, c *gin.Context, u *aliUpstream, clientModel, mapping, rawBody string, editBody func([]byte) []byte) []byte {
	t.Helper()
	c.Set("model_mapping", mapping)

	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(rawBody), &request))
	info := &relaycommon.RelayInfo{
		OriginModelName: clientModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: clientModel,
			ChannelBaseUrl:    u.server.URL,
			ApiKey:            "sk-test",
		},
	}
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, &request))

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	if editBody != nil {
		body = editBody(body)
	}

	respValue, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	resp, ok := respValue.(*http.Response)
	require.True(t, ok)
	_ = resp.Body.Close()
	return u.last(t)
}

// A Qwen client model mapped to a custom deployment name keeps an explicit
// zero budget all the way to the upstream body: the Ali conversion keeps it
// and the Ali DoRequest re-inserts what the generic marshaler dropped.
func TestAliThinkingBudgetQwenClientMappedToCustomNameReachesUpstream(t *testing.T) {
	u := newAliUpstream(t)
	got := sendAliChat(t, newAliRelayContext(), u, "qwen3-max", `{"qwen3-max":"my-qwen-deploy"}`,
		`{"model":"qwen3-max","messages":[{"role":"user","content":"hi"}],"enable_thinking":true,"thinking_budget":0}`, nil)

	assert.Equal(t, "my-qwen-deploy", gjson.GetBytes(got, "model").String())
	value := gjson.GetBytes(got, "thinking_budget")
	require.True(t, value.Exists(), string(got))
	assert.Equal(t, "0", value.Raw)
	assert.True(t, gjson.GetBytes(got, "enable_thinking").Bool())
	assert.Equal(t, "hi", gjson.GetBytes(got, "messages.0.content").String())
}

func TestAliThinkingBudgetNonQwenClientIsDropped(t *testing.T) {
	u := newAliUpstream(t)
	got := sendAliChat(t, newAliRelayContext(), u, "deepseek-v3", "",
		`{"model":"deepseek-v3","messages":[{"role":"user","content":"hi"}],"thinking_budget":100}`, nil)

	assert.False(t, gjson.GetBytes(got, "thinking_budget").Exists(), string(got))
}

func TestAliThinkingBudgetQwenClientMappedToNonQwenUpstreamKeepsBudget(t *testing.T) {
	u := newAliUpstream(t)
	got := sendAliChat(t, newAliRelayContext(), u, "qwen-plus", `{"qwen-plus":"deepseek-r1"}`,
		`{"model":"qwen-plus","messages":[{"role":"user","content":"hi"}],"thinking_budget":128}`, nil)

	assert.Equal(t, "deepseek-r1", gjson.GetBytes(got, "model").String())
	assert.Equal(t, "128", gjson.GetBytes(got, "thinking_budget").Raw, string(got))
}

// When the upstream name is itself Qwen, the generic marshaler keeps the
// budget, so a body without it means a later stage (ParamOverride or
// RemoveDisabledFields) removed it on purpose: nothing is re-inserted.
func TestAliThinkingBudgetRemovedAfterMarshalForQwenUpstreamStaysRemoved(t *testing.T) {
	u := newAliUpstream(t)
	removeBudget := func(body []byte) []byte {
		require.True(t, gjson.GetBytes(body, "thinking_budget").Exists(), string(body))
		out, err := sjson.DeleteBytes(body, "thinking_budget")
		require.NoError(t, err)
		return out
	}
	got := sendAliChat(t, newAliRelayContext(), u, "my-alias", `{"my-alias":"qwen3-max"}`,
		`{"model":"my-alias","messages":[{"role":"user","content":"hi"}],"thinking_budget":64}`, removeBudget)

	assert.False(t, gjson.GetBytes(got, "thinking_budget").Exists(), string(got))
}

// Retries reuse the gin context. DoRequest consumes the stash, and every
// ConvertOpenAIRequest resets it, so a budget re-inserted for one attempt is
// never re-inserted into a later attempt whose own body dropped it (here: a
// Qwen upstream whose ParamOverride removes thinking_budget).
func TestAliThinkingBudgetStashDoesNotLeakAcrossAttempts(t *testing.T) {
	u := newAliUpstream(t)
	c := newAliRelayContext()
	const body = `{"model":"qwen3-max","messages":[{"role":"user","content":"hi"}],"thinking_budget":32}`
	removeBudget := func(b []byte) []byte {
		out, err := sjson.DeleteBytes(b, "thinking_budget")
		require.NoError(t, err)
		return out
	}

	first := sendAliChat(t, c, u, "qwen3-max", `{"qwen3-max":"my-qwen-deploy"}`, body, nil)
	assert.Equal(t, "32", gjson.GetBytes(first, "thinking_budget").Raw)

	second := sendAliChat(t, c, u, "qwen3-max", `{"qwen3-max":"qwen-plus"}`, body, removeBudget)
	assert.False(t, gjson.GetBytes(second, "thinking_budget").Exists(), string(second))

	// An attempt that converted but never reached DoRequest leaves a stash;
	// a later pass-through attempt skips conversion and must send its body
	// as is.
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(body), &request))
	request.Model = "my-qwen-deploy"
	info := &relaycommon.RelayInfo{
		OriginModelName: "qwen3-max",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "my-qwen-deploy", ChannelBaseUrl: u.server.URL},
	}
	_, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
	require.NoError(t, err)
	info.ChannelSetting.PassThroughBodyEnabled = true
	raw := []byte(`{"model":"qwen3-max","messages":[]}`)
	respValue, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(raw))
	require.NoError(t, err)
	_ = respValue.(*http.Response).Body.Close()
	assert.JSONEq(t, string(raw), string(u.last(t)))

	// The stash was consumed: a later non-pass-through send adds nothing.
	info.ChannelSetting.PassThroughBodyEnabled = false
	respValue, err = (&Adaptor{}).DoRequest(c, info, bytes.NewReader(raw))
	require.NoError(t, err)
	_ = respValue.(*http.Response).Body.Close()
	assert.JSONEq(t, string(raw), string(u.last(t)))
}

func TestAliKeepsThinkingBudgetEitherName(t *testing.T) {
	tests := []struct {
		origin, upstream string
		want             bool
	}{
		{"qwen3-max", "my-qwen-deploy", true},
		{"my-alias", "qwen3-max", true},
		{"qwq-32b", "qwq-32b", true},
		{"deepseek-v3", "deepseek-r1", false},
	}
	for _, tt := range tests {
		info := &relaycommon.RelayInfo{
			OriginModelName: tt.origin,
			ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tt.upstream},
		}
		assert.Equal(t, tt.want, aliKeepsThinkingBudget(info), "%s -> %s", tt.origin, tt.upstream)
	}
}
