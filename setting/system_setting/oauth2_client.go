package system_setting

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
}

var defaultOAuth2Settings = OAuth2Settings{}

func init() {
	config.GlobalConfig.Register("oauth2", &defaultOAuth2Settings)
}

func GetOAuth2Settings() *OAuth2Settings {
	return &defaultOAuth2Settings
}

// IsAbsoluteHTTPURL reports whether raw is an absolute http(s) URL with a host.
func IsAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
