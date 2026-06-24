
package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// MigrateConsoleSetting console_setting.*
func MigrateConsoleSetting(c *gin.Context) {
	// option
	opts, err := model.AllOption()
	if err != nil {
		common.SysError("failed to get all options: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgConsoleGetConfigFailed)
		return
	}
	// map
	valMap := map[string]string{}
	for _, o := range opts {
		valMap[o.Key] = o.Value
	}

	// APIInfo
	if v := valMap["ApiInfo"]; v != "" {
		var arr []map[string]interface{}
		if err := common.Unmarshal([]byte(v), &arr); err == nil {
			if len(arr) > 50 {
				arr = arr[:50]
			}
			bytes, _ := common.Marshal(arr)
			model.UpdateOption("console_setting.api_info", string(bytes))
		}
		model.UpdateOption("ApiInfo", "")
	}
	// Announcements
	if v := valMap["Announcements"]; v != "" {
		model.UpdateOption("console_setting.announcements", v)
		model.UpdateOption("Announcements", "")
	}
	// FAQ
	if v := valMap["FAQ"]; v != "" {
		var arr []map[string]interface{}
		if err := common.Unmarshal([]byte(v), &arr); err == nil {
			out := []map[string]interface{}{}
			for _, item := range arr {
				q, _ := item["question"].(string)
				if q == "" {
					q, _ = item["title"].(string)
				}
				a, _ := item["answer"].(string)
				if a == "" {
					a, _ = item["content"].(string)
				}
				if q != "" && a != "" {
					out = append(out, map[string]interface{}{"question": q, "answer": a})
				}
			}
			if len(out) > 50 {
				out = out[:50]
			}
			bytes, _ := common.Marshal(out)
			model.UpdateOption("console_setting.faq", string(bytes))
		}
		model.UpdateOption("FAQ", "")
	}
	// Uptime Kuma groups console_setting.uptime_kuma_groups
	url := valMap["UptimeKumaUrl"]
	slug := valMap["UptimeKumaSlug"]
	if url != "" && slug != "" {
		// URL Slug
		groups := []map[string]interface{}{
			{
				"id":           1,
				"categoryName": "old",
				"url":          url,
				"slug":         slug,
				"description":  "",
			},
		}
		bytes, _ := common.Marshal(groups)
		model.UpdateOption("console_setting.uptime_kuma_groups", string(bytes))
	}
	if url != "" {
		model.UpdateOption("UptimeKumaUrl", "")
	}
	if slug != "" {
		model.UpdateOption("UptimeKumaSlug", "")
	}

	oldKeys := []string{"ApiInfo", "Announcements", "FAQ", "UptimeKumaUrl", "UptimeKumaSlug"}
	model.DB.Where("key IN ?", oldKeys).Delete(&model.Option{})

	// OptionMap
	model.InitOptionMap()
	common.SysLog("console setting migrated")
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgMigrated)})
}
