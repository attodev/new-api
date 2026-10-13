package operation_setting

import (
	"fmt"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	QuotaSettingTrustQuotaUSDKey        = "quota_setting.trust_quota_usd"
	QuotaSettingPreConsumeMultiplierKey = "quota_setting.pre_consume_multiplier"
)

type QuotaSetting struct {
	EnableFreeModelPreConsume bool    `json:"enable_free_model_pre_consume"`
	TrustQuotaUSD             float64 `json:"trust_quota_usd"`        // Wallet pre-consume bypass threshold in USD; 0 disables the bypass.
	PreConsumeMultiplier      float64 `json:"pre_consume_multiplier"` // Multiplier on the estimated input cost; affects only the reservation.
}

var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
	TrustQuotaUSD:             10,
	PreConsumeMultiplier:      1,
}

func init() {
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}

// ValidateQuotaOption validates reservation settings before they are persisted.
func ValidateQuotaOption(key, value string) error {
	if key != QuotaSettingTrustQuotaUSDKey && key != QuotaSettingPreConsumeMultiplierKey {
		return nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return fmt.Errorf("%s must be a finite non-negative number", key)
	}
	if key == QuotaSettingPreConsumeMultiplierKey && number == 0 {
		return fmt.Errorf("%s must be greater than zero", key)
	}
	return nil
}

// InputPreConsumeMultiplier rejects invalid runtime settings as well as invalid saves.
func InputPreConsumeMultiplier() (float64, error) {
	multiplier := quotaSetting.PreConsumeMultiplier
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 0, fmt.Errorf("pre-consume multiplier must be a finite number greater than zero")
	}
	return multiplier, nil
}

// TrustQuota returns the wallet pre-consume bypass threshold in quota units
// (TrustQuotaUSD × common.QuotaPerUnit). It is a float so fractional USD
// thresholds compare exactly. It returns 0 — no bypass — when the result is
// 0, negative, NaN or infinite.
func TrustQuota() float64 {
	trustQuota := quotaSetting.TrustQuotaUSD * common.QuotaPerUnit
	if trustQuota <= 0 || math.IsNaN(trustQuota) || math.IsInf(trustQuota, 0) {
		return 0
	}
	return trustQuota
}
