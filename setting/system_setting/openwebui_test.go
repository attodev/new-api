package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestOpenWebUISettings_Defaults(t *testing.T) {
	s := GetOpenWebUISettings()
	require.False(t, s.RelayEnabled)
	require.Equal(t, "", s.ServiceApiKey)
	require.Equal(t, "open-webui", s.JwtIssuer)
	require.Equal(t, 60, s.JwtLeewaySeconds)
	require.True(t, s.AllowUnsignedRequests)
}

func TestOpenWebUISettings_RegisteredUnderOpenWebUIName(t *testing.T) {
	cfg := config.GlobalConfig.Get("openwebui")
	require.NotNil(t, cfg)
	_, ok := cfg.(*OpenWebUISettings)
	require.True(t, ok)
}

func TestNormalizeOpenWebUIServiceKey(t *testing.T) {
	require.Equal(t, "abc123", NormalizeOpenWebUIServiceKey("  sk-abc123 "))
	require.Equal(t, "abc123", NormalizeOpenWebUIServiceKey("abc123"))
	require.Equal(t, "", NormalizeOpenWebUIServiceKey("sk-"))
	require.Equal(t, "", NormalizeOpenWebUIServiceKey("   "))
}
