package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type OAuth2Settings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
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
