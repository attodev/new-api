package system_setting

// fork: O13 — built-in OIDC provider settings (config name "oauth2"; distinct from the OIDC client login settings in oidc.go).

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type OAuth2Settings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	// AppUrl is the external chat app's base URL (Open WebUI WEBUI_URL). The
	// Playground "Open in Chat App" link points here; the app itself starts
	// the OIDC login when opened.
	AppUrl string `json:"app_url"`
	// PostLogoutRedirectURI is the only post_logout_redirect_uri the
	// end-session endpoint will honour (Open WebUI WEBUI_AUTH_SIGNOUT_REDIRECT_URL).
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
	// OpenInNewWindow controls whether the frontend's "Open in Chat App"
	// action opens the external app in a new tab (window.open) instead of
	// navigating the current tab away (window.location.href).
	OpenInNewWindow bool `json:"open_in_new_window"`
	// ReplacePlayground makes visiting the built-in Playground page itself
	// immediately act like clicking "Open in Chat App", instead of
	// rendering the built-in playground UI.
	ReplacePlayground bool `json:"replace_playground"`
	// TokenFromKey* configure POST /oauth2/token-from-key, which trades a
	// new-api API key for a short-lived userinfo-only access token (used by
	// the Open WebUI MCP server with Open WebUI's OAuth token exchange).
	TokenFromKeyEnabled bool `json:"token_from_key_enabled"`
	// TokenFromKeyClientId is the client_id recorded on (and introspected
	// for) those tokens. It must differ from ClientId.
	TokenFromKeyClientId string `json:"token_from_key_client_id"`
	// TokenFromKeyTTLSeconds is the issued token lifetime.
	TokenFromKeyTTLSeconds int `json:"token_from_key_ttl_seconds"`
	// TokenFromKeyRateLimit is the per-key issuance limit per minute.
	TokenFromKeyRateLimit int `json:"token_from_key_rate_limit"`
}

const (
	DefaultTokenFromKeyClientId = "openwebui-mcp"
	DefaultTokenFromKeyTTL      = 600
	MinTokenFromKeyTTL          = 60
	MaxTokenFromKeyTTL          = 900
	DefaultTokenFromKeyRate     = 30
	MinTokenFromKeyRate         = 1
	MaxTokenFromKeyRate         = 600
)

var defaultOAuth2Settings = OAuth2Settings{
	TokenFromKeyClientId:   DefaultTokenFromKeyClientId,
	TokenFromKeyTTLSeconds: DefaultTokenFromKeyTTL,
	TokenFromKeyRateLimit:  DefaultTokenFromKeyRate,
}

func init() {
	config.GlobalConfig.Register("oauth2", &defaultOAuth2Settings)
}

func GetOAuth2Settings() *OAuth2Settings {
	return &defaultOAuth2Settings
}

// EffectiveTokenFromKeyClientId is the client_id token-from-key tokens carry:
// the configured one, or the default when it is blank.
func (s *OAuth2Settings) EffectiveTokenFromKeyClientId() string {
	if v := strings.TrimSpace(s.TokenFromKeyClientId); v != "" {
		return v
	}
	return DefaultTokenFromKeyClientId
}

// SameOAuth2ClientId reports whether two client_ids are the same once
// surrounding spaces and case are ignored (MySQL's default *_ci collation
// would treat them as equal too).
func SameOAuth2ClientId(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// TokenFromKeyClientIdCollides reports whether the effective token-from-key
// client_id equals the Open WebUI client_id. Option validation refuses such
// saves, but values loaded from the database at startup bypass it.
func (s *OAuth2Settings) TokenFromKeyClientIdCollides() bool {
	return s.ClientId != "" && SameOAuth2ClientId(s.EffectiveTokenFromKeyClientId(), s.ClientId)
}

// EffectiveTokenFromKeyTTL is the configured lifetime in seconds, or the
// default when it is outside [MinTokenFromKeyTTL, MaxTokenFromKeyTTL].
func (s *OAuth2Settings) EffectiveTokenFromKeyTTL() int {
	if s.TokenFromKeyTTLSeconds < MinTokenFromKeyTTL || s.TokenFromKeyTTLSeconds > MaxTokenFromKeyTTL {
		return DefaultTokenFromKeyTTL
	}
	return s.TokenFromKeyTTLSeconds
}

// EffectiveTokenFromKeyRate is the configured per-key limit per minute, or
// the default when it is outside [MinTokenFromKeyRate, MaxTokenFromKeyRate].
func (s *OAuth2Settings) EffectiveTokenFromKeyRate() int {
	if s.TokenFromKeyRateLimit < MinTokenFromKeyRate || s.TokenFromKeyRateLimit > MaxTokenFromKeyRate {
		return DefaultTokenFromKeyRate
	}
	return s.TokenFromKeyRateLimit
}

// IsAbsoluteHTTPURL reports whether raw is an absolute http(s) URL with a
// host name and no fragment (RFC 6749 §3.1.2 forbids fragments in redirect
// URIs; the same rule is applied to every registered URL).
func IsAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.Fragment == ""
}
