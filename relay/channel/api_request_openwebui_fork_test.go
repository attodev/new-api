// fork: O13 — the Open WebUI identity JWT never reaches an upstream provider (ported from OpenWebUI relay/channel/api_request_test.go).

package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProcessHeaderOverride_PassthroughSkipsOpenWebUIUserJWT(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("X-OpenWebUI-User-Jwt", "header.payload.signature")

	for _, rule := range []string{"*", "re:(?i)^x-"} {
		info := &relaycommon.RelayInfo{
			IsChannelTest: false,
			ChannelMeta: &relaycommon.ChannelMeta{
				HeadersOverride: map[string]any{rule: ""},
			},
		}

		headers, err := processHeaderOverride(info, ctx)
		require.NoError(t, err, rule)
		require.Equal(t, "trace-123", headers["x-trace-id"], rule)
		_, hasJWT := headers["x-openwebui-user-jwt"]
		require.False(t, hasJWT, rule)
	}
}
