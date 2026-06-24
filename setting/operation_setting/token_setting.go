package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// TokenSetting
type TokenSetting struct {
	MaxUserTokens int `json:"max_user_tokens"`
}

var tokenSetting = TokenSetting{
	MaxUserTokens: 1000, // 1000
}

func init() {
	config.GlobalConfig.Register("token_setting", &tokenSetting)
}

// GetTokenSetting
func GetTokenSetting() *TokenSetting {
	return &tokenSetting
}

// GetMaxUserTokens
func GetMaxUserTokens() int {
	return GetTokenSetting().MaxUserTokens
}
