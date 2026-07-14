package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/media_cleanup_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	mediaCleanupIntervalKey = "media_cleanup_setting.cleanup_interval"
	mediaCleanupAgeKey      = "media_cleanup_setting.cleanup_age"
)

func useOptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	previousDB := DB
	DB = testDB
	t.Cleanup(func() { DB = previousDB })
	return testDB
}

func preserveMediaCleanupState(t *testing.T) {
	t.Helper()
	previousSetting := *media_cleanup_setting.GetMediaCleanupSetting()
	previousRuntime := common.GetMediaCleanupConfig()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousInterval, hadInterval := common.OptionMap[mediaCleanupIntervalKey]
	previousAge, hadAge := common.OptionMap[mediaCleanupAgeKey]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		*media_cleanup_setting.GetMediaCleanupSetting() = previousSetting
		common.SetMediaCleanupConfig(previousRuntime)
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadInterval {
			common.OptionMap[mediaCleanupIntervalKey] = previousInterval
		} else {
			delete(common.OptionMap, mediaCleanupIntervalKey)
		}
		if hadAge {
			common.OptionMap[mediaCleanupAgeKey] = previousAge
		} else {
			delete(common.OptionMap, mediaCleanupAgeKey)
		}
	})
}

func TestUpdateOptionsBulkPersistsAndSyncsMediaCleanupSettings(t *testing.T) {
	testDB := useOptionTestDB(t)
	preserveMediaCleanupState(t)

	err := UpdateOptionsBulk(map[string]string{
		mediaCleanupIntervalKey: "500",
		mediaCleanupAgeKey:      "400",
	})

	require.NoError(t, err)
	var options []Option
	require.NoError(t, testDB.Order("key").Find(&options).Error)
	require.Len(t, options, 2)
	assert.Equal(t, common.MediaCleanupConfig{CleanupInterval: 500, CleanupAge: 400}, common.GetMediaCleanupConfig())
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "500", common.OptionMap[mediaCleanupIntervalKey])
	assert.Equal(t, "400", common.OptionMap[mediaCleanupAgeKey])
	common.OptionMapRWMutex.RUnlock()
}

func TestUpdateOptionsBulkDoesNotChangeMediaCleanupSettingsWhenDatabaseWriteFails(t *testing.T) {
	preserveMediaCleanupState(t)
	brokenDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := brokenDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	previousDB := DB
	DB = brokenDB
	t.Cleanup(func() { DB = previousDB })

	*media_cleanup_setting.GetMediaCleanupSetting() = media_cleanup_setting.MediaCleanupSetting{CleanupInterval: 180, CleanupAge: 180}
	common.SetMediaCleanupConfig(common.MediaCleanupConfig{CleanupInterval: 180, CleanupAge: 180})
	common.OptionMapRWMutex.Lock()
	common.OptionMap[mediaCleanupIntervalKey] = "180"
	common.OptionMap[mediaCleanupAgeKey] = "180"
	common.OptionMapRWMutex.Unlock()

	err = UpdateOptionsBulk(map[string]string{
		mediaCleanupIntervalKey: "500",
		mediaCleanupAgeKey:      "400",
	})

	require.Error(t, err)
	assert.Equal(t, common.MediaCleanupConfig{CleanupInterval: 180, CleanupAge: 180}, common.GetMediaCleanupConfig())
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "180", common.OptionMap[mediaCleanupIntervalKey])
	assert.Equal(t, "180", common.OptionMap[mediaCleanupAgeKey])
	common.OptionMapRWMutex.RUnlock()
}

func TestUpdateOptionDoesNotChangeMemoryWhenDatabaseWriteFails(t *testing.T) {
	brokenDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := brokenDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	previousDB := DB
	DB = brokenDB
	t.Cleanup(func() { DB = previousDB })

	const key = mediaCleanupIntervalKey
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousValue, existed := common.OptionMap[key]
	common.OptionMap[key] = "180"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = previousValue
		} else {
			delete(common.OptionMap, key)
		}
	})

	err = UpdateOption(key, "500")

	require.Error(t, err)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "180", common.OptionMap[key])
	common.OptionMapRWMutex.RUnlock()
}
