package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// CheckinSetting
type CheckinSetting struct {
	Enabled  bool `json:"enabled"`
	MinQuota int  `json:"min_quota"`
	MaxQuota int  `json:"max_quota"`
}

var checkinSetting = CheckinSetting{
	Enabled:  false,
	MinQuota: 1000,  // 1000 ( 0.002 USD)
	MaxQuota: 10000, // 10000 ( 0.02 USD)
}

func init() {
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

// GetCheckinSetting
func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// IsCheckinEnabled
func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

// GetCheckinQuotaRange
func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}
