package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const resolutionPriceSettingPrefix = "resolution_price_setting."

type ResolutionPriceSetting struct {
	Enabled bool               `json:"enabled"`
	Setting map[string]float64 `json:"setting"`
}

func ResolutionPriceKey(modelName string) string {
	return resolutionPriceSettingPrefix + modelName
}

func HasResolutionPricing(modelName string) bool {
	setting, err := LoadResolutionPricing(modelName)
	return err == nil && setting != nil && setting.Enabled
}

func LoadResolutionPricing(modelName string) (*ResolutionPriceSetting, error) {
	PricingConfigRLock()
	defer PricingConfigRUnlock()
	return LoadResolutionPricingSnapshot(modelName)
}

// LoadResolutionPricingSnapshot reads a setting while the caller holds the pricing read lock.
func LoadResolutionPricingSnapshot(modelName string) (*ResolutionPriceSetting, error) {
	if modelName == "" {
		return nil, nil
	}
	common.OptionMapRWMutex.RLock()
	value, exists := common.OptionMap[ResolutionPriceKey(modelName)]
	common.OptionMapRWMutex.RUnlock()
	if !exists {
		return nil, nil
	}
	var setting ResolutionPriceSetting
	if err := common.UnmarshalJsonStr(value, &setting); err != nil {
		return nil, fmt.Errorf("parse resolution pricing for model %s: %w", modelName, err)
	}
	if !setting.Enabled {
		return nil, nil
	}
	return &setting, nil
}

func GetResolutionPrice(modelName, resolution string) (float64, error) {
	setting, err := LoadResolutionPricing(modelName)
	if err != nil {
		return 0, err
	}
	if setting == nil {
		return 0, nil
	}
	return GetResolutionPriceFromSetting(modelName, resolution, setting)
}

func GetResolutionPriceFromSetting(modelName, resolution string, setting *ResolutionPriceSetting) (float64, error) {
	if setting == nil || !setting.Enabled {
		return 0, fmt.Errorf("resolution pricing is not enabled for model %s", modelName)
	}
	if len(setting.Setting) == 0 {
		return 0, fmt.Errorf("resolution price setting enabled but no prices configured for model %s", modelName)
	}

	resolution = strings.ToLower(strings.TrimSpace(resolution))
	price, ok := setting.Setting[resolution]
	if !ok {
		return 0, fmt.Errorf("resolution price setting enabled but resolution %q not configured for model %s", resolution, modelName)
	}

	if price <= 0 {
		return 0, fmt.Errorf("resolution price for %s/%s is <= 0: %f", modelName, resolution, price)
	}

	return price, nil
}
