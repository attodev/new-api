package system_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// OpenWebUISettings configures the Open WebUI relay integration: Open WebUI
// sends every user's request with one shared service key plus a per-request
// HS256 JWT (X-OpenWebUI-User-Jwt) that identifies the person. new-api
// verifies that JWT and bills the request to that person's own token.
//
// ServiceApiKey, JwtSecret and JwtPreviousSecret are intentionally named so
// controller.GetOptions hides them (suffixes "api_key" / "secret").
type OpenWebUISettings struct {
	RelayEnabled bool `json:"relay_enabled"`
	// ServiceApiKey is the key of the token Open WebUI uses as OPENAI_API_KEYS,
	// stored without the "sk-" prefix (the same form TokenAuth parses).
	ServiceApiKey string `json:"service_api_key"`
	// JwtSecret is Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET.
	JwtSecret string `json:"jwt_secret"`
	// JwtPreviousSecret is accepted alongside JwtSecret during rotation.
	JwtPreviousSecret string `json:"jwt_previous_secret"`
	// JwtIssuer is the expected "iss" claim. Open WebUI always sends "open-webui".
	JwtIssuer string `json:"jwt_issuer"`
	// JwtLeewaySeconds is the clock-skew allowance applied to exp and iat.
	JwtLeewaySeconds int `json:"jwt_leeway_seconds"`
	// AllowUnsignedRequests bills requests that carry the service key but no
	// JWT to the service token itself instead of rejecting them with 401.
	AllowUnsignedRequests bool `json:"allow_unsigned_requests"`
}

var defaultOpenWebUISettings = OpenWebUISettings{
	JwtIssuer:             "open-webui",
	JwtLeewaySeconds:      60,
	AllowUnsignedRequests: true,
}

func init() {
	config.GlobalConfig.Register("openwebui", &defaultOpenWebUISettings)
}

func GetOpenWebUISettings() *OpenWebUISettings {
	return &defaultOpenWebUISettings
}

// NormalizeOpenWebUIServiceKey turns whatever the admin pasted into the form
// TokenAuth compares against: trimmed and without the "sk-" prefix.
func NormalizeOpenWebUIServiceKey(raw string) string {
	key := strings.TrimSpace(raw)
	key = strings.TrimPrefix(key, "sk-")
	return strings.TrimSpace(key)
}
