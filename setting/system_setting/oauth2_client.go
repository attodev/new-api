package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type OAuth2Settings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
}

var defaultOAuth2Settings = OAuth2Settings{}

func init() {
	config.GlobalConfig.Register("oauth2", &defaultOAuth2Settings)
}

func GetOAuth2Settings() *OAuth2Settings {
	return &defaultOAuth2Settings
}
