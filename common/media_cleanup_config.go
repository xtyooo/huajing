package common

import "sync/atomic"

type MediaCleanupConfig struct {
	CleanupInterval int `json:"cleanup_interval"`
	CleanupAge      int `json:"cleanup_age"`
}

var mediaCleanupConfig atomic.Value
var mediaCleanupConfigChanged = make(chan struct{}, 1)

func init() {
	mediaCleanupConfig.Store(MediaCleanupConfig{
		CleanupInterval: 180,
		CleanupAge:      180,
	})
}

func GetMediaCleanupConfig() MediaCleanupConfig {
	return mediaCleanupConfig.Load().(MediaCleanupConfig)
}

func SetMediaCleanupConfig(config MediaCleanupConfig) {
	previous := GetMediaCleanupConfig()
	mediaCleanupConfig.Store(config)
	if previous != config {
		select {
		case mediaCleanupConfigChanged <- struct{}{}:
		default:
		}
	}
}

func MediaCleanupConfigChanged() <-chan struct{} {
	return mediaCleanupConfigChanged
}
