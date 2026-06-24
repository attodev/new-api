package performance_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// PerformanceSetting
type PerformanceSetting struct {
	// DiskCacheEnabled
	DiskCacheEnabled bool `json:"disk_cache_enabled"`
	// DiskCacheThresholdMB MB
	DiskCacheThresholdMB int `json:"disk_cache_threshold_mb"`
	// DiskCacheMaxSizeMB MB
	DiskCacheMaxSizeMB int `json:"disk_cache_max_size_mb"`
	// DiskCachePath
	DiskCachePath string `json:"disk_cache_path"`

	// MonitorEnabled
	MonitorEnabled bool `json:"monitor_enabled"`
	// MonitorCPUThreshold CPU %
	MonitorCPUThreshold int `json:"monitor_cpu_threshold"`
	// MonitorMemoryThreshold %
	MonitorMemoryThreshold int `json:"monitor_memory_threshold"`
	// MonitorDiskThreshold %
	MonitorDiskThreshold int `json:"monitor_disk_threshold"`
}

var performanceSetting = PerformanceSetting{
	DiskCacheEnabled:     false,
	DiskCacheThresholdMB: 10,   // 10MB
	DiskCacheMaxSizeMB:   1024, // 1GB
	DiskCachePath:        "",

	MonitorEnabled:         true,
	MonitorCPUThreshold:    90,
	MonitorMemoryThreshold: 90,
	MonitorDiskThreshold:   95,
}

func init() {
	config.GlobalConfig.Register("performance_setting", &performanceSetting)
	// common
	syncToCommon()
}

// syncToCommon common
func syncToCommon() {
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Enabled:     performanceSetting.DiskCacheEnabled,
		ThresholdMB: performanceSetting.DiskCacheThresholdMB,
		MaxSizeMB:   performanceSetting.DiskCacheMaxSizeMB,
		Path:        performanceSetting.DiskCachePath,
	})

	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{
		Enabled:         performanceSetting.MonitorEnabled,
		CPUThreshold:    performanceSetting.MonitorCPUThreshold,
		MemoryThreshold: performanceSetting.MonitorMemoryThreshold,
		DiskThreshold:   performanceSetting.MonitorDiskThreshold,
	})
}

// GetPerformanceSetting
func GetPerformanceSetting() *PerformanceSetting {
	return &performanceSetting
}

// UpdateAndSync common
func UpdateAndSync() {
	syncToCommon()
}

// GetCacheStats common
func GetCacheStats() common.DiskCacheStats {
	return common.GetDiskCacheStats()
}

// ResetStats
func ResetStats() {
	common.ResetDiskCacheStats()
}
