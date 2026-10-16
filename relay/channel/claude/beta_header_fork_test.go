package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 90874460e (relay/channel/claude/adaptor_test.go).
func TestSetupRequestHeaderKeepsSingleAnthropicBetaValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("anthropic-beta", "a-2026-01-01,b-2026-02-02")
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-5-5",
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "sk-test"},
	}

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Equal(t, []string{"a-2026-01-01,b-2026-02-02"}, header.Values("anthropic-beta"))

	c.Request.Header.Del("anthropic-beta")
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Empty(t, header.Values("anthropic-beta"))
}

// Ported from team 90874460e (relay/channel/claude/adaptor_test.go).
func TestSetupRequestHeaderTrimsAndDropsEmptyAnthropicBetaValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Add("anthropic-beta", "a")
	c.Request.Header.Add("anthropic-beta", "")
	c.Request.Header.Add("anthropic-beta", " b , ,c ")
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-5-5",
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "sk-test"},
	}

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Equal(t, []string{"a,b,c"}, header.Values("anthropic-beta"))

	c.Request.Header.Set("anthropic-beta", " , ")
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Empty(t, header.Values("anthropic-beta"))
}

func TestJoinAnthropicBetaValuesTrimsAndDropsEmpty(t *testing.T) {
	assert.Equal(t, "a,b,c", joinAnthropicBetaValues([]string{" a , ,b", "c "}))
	assert.Equal(t, "", joinAnthropicBetaValues(nil))
	assert.Equal(t, "", joinAnthropicBetaValues([]string{"", " , "}))
}
