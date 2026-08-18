package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestOAuth2Settings_RegisteredUnderOAuth2Name(t *testing.T) {
	cfg := config.GlobalConfig.Get("oauth2")
	require.NotNil(t, cfg, "oauth2 config must be registered with GlobalConfig")
	require.IsType(t, &OAuth2Settings{}, cfg)
}

func TestOAuth2Settings_UpdateViaConfigMap(t *testing.T) {
	original := *GetOAuth2Settings()
	t.Cleanup(func() { *GetOAuth2Settings() = original })

	err := config.UpdateConfigFromMap(GetOAuth2Settings(), map[string]string{
		"enabled":            "true",
		"client_id":          "openwebui",
		"redirect_uri":       "https://chat.example.com/auth/newapi/callback",
		"open_in_new_window": "true",
		"replace_playground": "true",
	})
	require.NoError(t, err)

	s := GetOAuth2Settings()
	require.True(t, s.Enabled)
	require.Equal(t, "openwebui", s.ClientId)
	require.Equal(t, "https://chat.example.com/auth/newapi/callback", s.RedirectURI)
	require.True(t, s.OpenInNewWindow)
	require.True(t, s.ReplacePlayground)
}
