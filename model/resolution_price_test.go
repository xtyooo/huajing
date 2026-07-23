package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetResolutionPriceUsesMemorySnapshot(t *testing.T) {
	setResolutionPricingOptionForTest(t, "video-model", `{"enabled":true,"setting":{"480p":0.01,"720p":0.02,"1080p":0.04}}`)

	price, err := GetResolutionPrice("video-model", " 720P ")

	require.NoError(t, err)
	assert.Equal(t, 0.02, price)
	assert.True(t, HasResolutionPricing("video-model"))
}

func TestGetResolutionPriceReturnsInvalidJSONError(t *testing.T) {
	setResolutionPricingOptionForTest(t, "broken-video-model", `{not-json}`)

	price, err := GetResolutionPrice("broken-video-model", "720p")

	require.Error(t, err)
	assert.Zero(t, price)
	assert.False(t, HasResolutionPricing("broken-video-model"))
}

func setResolutionPricingOptionForTest(t *testing.T, modelName, value string) {
	t.Helper()
	key := ResolutionPriceKey(modelName)
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
