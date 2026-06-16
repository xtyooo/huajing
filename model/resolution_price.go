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
	if modelName == "" {
		return false
	}
	var option Option
	result := DB.Where("`key` = ?", ResolutionPriceKey(modelName)).First(&option)
	if result.Error != nil {
		return false
	}
	var setting ResolutionPriceSetting
	if err := common.UnmarshalJsonStr(option.Value, &setting); err != nil {
		return false
	}
	return setting.Enabled
}

func GetResolutionPrice(modelName, resolution string) (float64, error) {
	if modelName == "" {
		return 0, nil
	}

	var option Option
	result := DB.Where("`key` = ?", ResolutionPriceKey(modelName)).First(&option)
	if result.Error != nil {
		return 0, nil
	}

	var setting ResolutionPriceSetting
	if err := common.UnmarshalJsonStr(option.Value, &setting); err != nil {
		return 0, nil
	}

	if !setting.Enabled {
		return 0, nil
	}

	if len(setting.Setting) == 0 {
		return 0, fmt.Errorf("resolution price setting enabled but no prices configured for model %s", modelName)
	}

	resolution = strings.ToLower(resolution)
	price, ok := setting.Setting[resolution]
	if !ok {
		return 0, fmt.Errorf("resolution price setting enabled but resolution %q not configured for model %s", resolution, modelName)
	}

	if price <= 0 {
		return 0, fmt.Errorf("resolution price for %s/%s is <= 0: %f", modelName, resolution, price)
	}

	return price, nil
}
