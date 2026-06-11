package common

import "sync/atomic"

type MediaCleanupConfig struct {
	CleanupInterval int `json:"cleanup_interval"`
	CleanupAge      int `json:"cleanup_age"`
}

var mediaCleanupConfig atomic.Value

func init() {
	mediaCleanupConfig.Store(MediaCleanupConfig{
		CleanupInterval: 300,
		CleanupAge:      180,
	})
}

func GetMediaCleanupConfig() MediaCleanupConfig {
	return mediaCleanupConfig.Load().(MediaCleanupConfig)
}

func SetMediaCleanupConfig(config MediaCleanupConfig) {
	mediaCleanupConfig.Store(config)
}
