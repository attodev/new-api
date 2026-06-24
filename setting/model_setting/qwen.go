package model_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// QwenSettings defines Qwen model configuration. boolenabled
type QwenSettings struct {
	SyncImageModels []string `json:"sync_image_models"`
}

var defaultQwenSettings = QwenSettings{
	SyncImageModels: []string{
		"z-image",
		"qwen-image",
		"wan2.6",
		"wan2.7",
		"qwen-image-edit",
		"qwen-image-edit-max",
		"qwen-image-edit-max-2026-01-16",
		"qwen-image-edit-plus",
		"qwen-image-edit-plus-2025-12-15",
		"qwen-image-edit-plus-2025-10-30",
	},
}

var qwenSettings = defaultQwenSettings

func init() {
	config.GlobalConfig.Register("qwen", &qwenSettings)
}

// GetQwenSettings
func GetQwenSettings() *QwenSettings {
	return &qwenSettings
}

// IsSyncImageModel
func IsSyncImageModel(model string) bool {
	for _, m := range qwenSettings.SyncImageModels {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}
