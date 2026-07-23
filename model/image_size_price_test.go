package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveImageSizeTier(t *testing.T) {
	tests := []struct {
		name string
		size string
		want string
	}{
		{name: "explicit 1K", size: "1K", want: "1k"},
		{name: "explicit 2K", size: " 2k ", want: "2k"},
		{name: "explicit 4K", size: "4K", want: "4k"},
		{name: "default size", size: "", want: "1k"},
		{name: "gpt image wide 1K", size: "1584x672", want: "1k"},
		{name: "gpt image portrait 2K", size: "1152x2048", want: "2k"},
		{name: "nano banana wide 2K", size: "3168x1344", want: "2k"},
		{name: "nano banana portrait 4K", size: "3072x5504", want: "4k"},
		{name: "generic at most 1K", size: "800x1024", want: "1k"},
		{name: "generic at most 2K", size: "1200x2000", want: "2k"},
		{name: "generic above 2K", size: "2049x1024", want: "4k"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveImageSizeTier(test.size)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestResolveImageSizeTierRejectsInvalidSize(t *testing.T) {
	for _, size := range []string{"3K", "wide", "0x1024", "1024xabc"} {
		t.Run(size, func(t *testing.T) {
			_, err := ResolveImageSizeTier(size)
			require.Error(t, err)
		})
	}
}

func TestGetImageSizePrice(t *testing.T) {
	setImageSizePricingOptionForTest(t, "image-model", `{"enabled":true,"setting":{"1k":0.01,"2k":0.02,"4k":0.04}}`)

	price, tier, err := GetImageSizePrice("image-model", "3168x1344")

	require.NoError(t, err)
	assert.Equal(t, "2k", tier)
	assert.Equal(t, 0.02, price)
	assert.True(t, HasImageSizePricing("image-model"))
}

func TestLoadImageSizePricingReturnsInvalidJSONError(t *testing.T) {
	setImageSizePricingOptionForTest(t, "broken-model", `{not-json}`)

	setting, err := LoadImageSizePricing("broken-model")

	require.Error(t, err)
	assert.Nil(t, setting)
}

func TestLoadImageSizePricingUsesMemoryWhenDatabaseIsUnavailable(t *testing.T) {
	setImageSizePricingOptionForTest(t, "image-model", `{"enabled":true,"setting":{"1k":0.01,"2k":0.02,"4k":0.04}}`)

	brokenDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := brokenDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	previousDB := DB
	DB = brokenDB
	t.Cleanup(func() { DB = previousDB })

	setting, err := LoadImageSizePricing("image-model")

	require.NoError(t, err)
	require.NotNil(t, setting)
	assert.Equal(t, 0.02, setting.Setting["2k"])
}

func setImageSizePricingOptionForTest(t *testing.T, modelName, value string) {
	t.Helper()
	key := ImageSizePriceKey(modelName)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousValue, existed := common.OptionMap[key]
	common.OptionMap[key] = value
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
}
