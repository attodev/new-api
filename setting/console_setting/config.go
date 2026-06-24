package console_setting

import "github.com/QuantumNous/new-api/setting/config"

type ConsoleSetting struct {
	ApiInfo              string `json:"api_info"`              // API (JSON )
	UptimeKumaGroups     string `json:"uptime_kuma_groups"`    // Uptime Kuma (JSON )
	Announcements        string `json:"announcements"`         // (JSON )
	FAQ                  string `json:"faq"`                   // (JSON )
	ApiInfoEnabled       bool   `json:"api_info_enabled"`      // API
	UptimeKumaEnabled    bool   `json:"uptime_kuma_enabled"`   // Uptime Kuma
	AnnouncementsEnabled bool   `json:"announcements_enabled"`
	FAQEnabled           bool   `json:"faq_enabled"`
}

var defaultConsoleSetting = ConsoleSetting{
	ApiInfo:              "",
	UptimeKumaGroups:     "",
	Announcements:        "",
	FAQ:                  "",
	ApiInfoEnabled:       true,
	UptimeKumaEnabled:    true,
	AnnouncementsEnabled: true,
	FAQEnabled:           true,
}

var consoleSetting = defaultConsoleSetting

func init() {
	// console_setting
	config.GlobalConfig.Register("console_setting", &consoleSetting)
}

// GetConsoleSetting ConsoleSetting
func GetConsoleSetting() *ConsoleSetting {
	return &consoleSetting
}
