package media_cleanup_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type MediaCleanupSetting struct {
	CleanupInterval int `json:"cleanup_interval"`
	CleanupAge      int `json:"cleanup_age"`
}

var mediaCleanupSetting = MediaCleanupSetting{
	CleanupInterval: common.GetMediaCleanupInterval(),
	CleanupAge:      common.GetMediaCleanupAge(),
}

func init() {
	config.GlobalConfig.Register("media_cleanup_setting", &mediaCleanupSetting)
	syncToCommon()
}

func syncToCommon() {
	common.SetMediaCleanupConfig(common.MediaCleanupConfig{
		CleanupInterval: mediaCleanupSetting.CleanupInterval,
		CleanupAge:      mediaCleanupSetting.CleanupAge,
	})
}

func UpdateAndSync() {
	syncToCommon()
}

func GetMediaCleanupSetting() *MediaCleanupSetting {
	return &mediaCleanupSetting
}
