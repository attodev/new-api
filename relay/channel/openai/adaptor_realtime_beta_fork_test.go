package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team adaptor_realtime_beta_test.go (0ab6532c3). OpenAI retired
// the Realtime Beta API: GA realtime models reject the
// "openai-beta.realtime-v1" subprotocol and the "openai-beta: realtime=v1"
// header, and only legacy "*-realtime-preview" models still need them. A
// client Sec-WebSocket-Protocol header selects the websocket branch.
func TestSetupRequestHeaderRealtimeBetaFork(t *testing.T) {
	tests := []struct {
		name              string
		upstreamModel     string
		withWebSocketSwp  bool
		wantBetaInSwp     bool
		wantOpenAIBetaHdr bool
	}{
		{name: "GA model over websocket drops beta subprotocol", upstreamModel: "gpt-realtime", withWebSocketSwp: true},
		{name: "legacy preview model over websocket keeps beta subprotocol", upstreamModel: "gpt-4o-realtime-preview", withWebSocketSwp: true, wantBetaInSwp: true},
		{name: "GA model over plain HTTP drops beta header", upstreamModel: "gpt-realtime"},
		{name: "GA mini model over plain HTTP drops beta header", upstreamModel: "gpt-realtime-mini"},
		{name: "legacy preview model over plain HTTP keeps beta header", upstreamModel: "gpt-4o-realtime-preview", wantOpenAIBetaHdr: true},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			if tt.withWebSocketSwp {
				c.Request.Header.Set("Sec-WebSocket-Protocol", "realtime")
			}

			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeRealtime,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					ApiKey:            "sk-test",
					UpstreamModelName: tt.upstreamModel,
				},
			}

			header := http.Header{}
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

			swp := header.Get("Sec-WebSocket-Protocol")
			if tt.withWebSocketSwp {
				assert.Contains(t, swp, "realtime")
				if tt.wantBetaInSwp {
					assert.Contains(t, swp, "openai-beta.realtime-v1")
				} else {
					assert.NotContains(t, swp, "openai-beta.realtime-v1")
				}
			} else {
				assert.Empty(t, swp)
			}

			betaHeader := header.Get("openai-beta")
			if tt.wantOpenAIBetaHdr {
				assert.Equal(t, "realtime=v1", betaHeader)
			} else {
				assert.Empty(t, betaHeader)
			}
		})
	}
}
