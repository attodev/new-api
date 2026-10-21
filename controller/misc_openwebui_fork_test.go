// fork: O13 — GetStatus exposes the oauth2_* fields the frontend uses for the Open WebUI entry points.

package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusExposesOAuth2Fields(t *testing.T) {
	settings := system_setting.GetOAuth2Settings()
	original := *settings
	originalOptionMap := common.OptionMap
	t.Cleanup(func() {
		*settings = original
		common.OptionMap = originalOptionMap
	})
	common.OptionMap = map[string]string{}

	cases := []system_setting.OAuth2Settings{
		{},
		{Enabled: true, OpenInNewWindow: true, ReplacePlayground: true, AppUrl: "https://chat.example.com",
			ClientId: "openwebui", ClientSecret: "super-secret-value", RedirectURI: "https://chat.example.com/cb"},
	}
	for _, want := range cases {
		*settings = want
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

		GetStatus(c)

		var payload struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &payload))
		require.Equal(t, want.Enabled, payload.Data["oauth2_enabled"])
		require.Equal(t, want.OpenInNewWindow, payload.Data["oauth2_open_in_new_window"])
		require.Equal(t, want.ReplacePlayground, payload.Data["oauth2_replace_playground"])
		require.Equal(t, want.AppUrl, payload.Data["oauth2_app_url"])
		// The client credentials never reach the public status endpoint.
		require.NotContains(t, rec.Body.String(), "super-secret-value")
		require.NotContains(t, payload.Data, "oauth2_client_secret")
	}
}
