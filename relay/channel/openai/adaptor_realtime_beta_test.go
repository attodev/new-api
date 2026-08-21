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

// TestSetupRequestHeaderRealtimeBeta reproduces a real bug: OpenAI retired the
// Realtime Beta API, and GA realtime models (gpt-realtime, gpt-realtime-mini,
// etc.) reject requests carrying the "openai-beta.realtime-v1" WebSocket
// subprotocol or "openai-beta: realtime=v1" header with beta_api_shape_disabled.
// Only the legacy "*-realtime-preview" models still need the beta marker.
func TestSetupRequestHeaderRealtimeBeta(t *testing.T) {
	tests := []struct {
		name              string
		upstreamModel     string
		withWebSocketSwp  bool
		wantBetaInSwp     bool
		wantOpenAIBetaHdr bool
	}{
		{
			name:              "GA model over websocket drops beta subprotocol",
			upstreamModel:     "gpt-realtime",
			withWebSocketSwp:  true,
			wantBetaInSwp:     false,
			wantOpenAIBetaHdr: false,
		},
		{
			name:              "legacy preview model over websocket keeps beta subprotocol",
			upstreamModel:     "gpt-4o-realtime-preview",
			withWebSocketSwp:  true,
			wantBetaInSwp:     true,
			wantOpenAIBetaHdr: false,
		},
		{
			name:              "GA model over plain HTTP drops beta header",
			upstreamModel:     "gpt-realtime-mini",
			withWebSocketSwp:  false,
			wantBetaInSwp:     false,
			wantOpenAIBetaHdr: false,
		},
		{
			name:              "legacy preview model over plain HTTP keeps beta header",
			upstreamModel:     "gpt-4o-realtime-preview",
			withWebSocketSwp:  false,
			wantBetaInSwp:     false,
			wantOpenAIBetaHdr: true,
		},
	}

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

			adaptor := &Adaptor{}
			header := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))

			swp := header.Get("Sec-WebSocket-Protocol")
			if tt.wantBetaInSwp {
				assert.Contains(t, swp, "openai-beta.realtime-v1")
			} else if tt.withWebSocketSwp {
				assert.NotContains(t, swp, "openai-beta.realtime-v1")
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
