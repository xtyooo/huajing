package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const imageSizePriceSettingPrefix = "image_size_price_setting."

type ImageSizePriceSetting struct {
	Enabled bool               `json:"enabled"`
	Setting map[string]float64 `json:"setting"`
}

var knownImageSizeTiers = map[string]string{
	"1584x672": "1k", "1376x768": "1k", "1536x1024": "1k", "1024x768": "1k",
	"1024x1024": "1k", "768x1024": "1k", "1024x1536": "1k", "768x1376": "1k",
	"1264x848": "1k", "1200x896": "1k", "1152x928": "1k", "928x1152": "1k",
	"896x1200": "1k", "848x1264": "1k",

	"2048x864": "2k", "2048x1152": "2k", "2048x1360": "2k", "2048x1536": "2k",
	"2048x2048": "2k", "1536x2048": "2k", "1360x2048": "2k", "1152x2048": "2k",
	"3168x1344": "2k", "2752x1536": "2k", "2528x1696": "2k", "2400x1792": "2k",
	"2304x1856": "2k", "1856x2304": "2k", "1792x2400": "2k", "1696x2528": "2k",
	"1536x2752": "2k",

	"3696x1584": "4k", "3840x2160": "4k", "3520x2352": "4k", "3312x2480": "4k",
	"2880x2880": "4k", "2480x3312": "4k", "2352x3520": "4k", "2160x3840": "4k",
	"6336x2688": "4k", "5504x3072": "4k", "5056x3392": "4k", "4800x3584": "4k",
	"4608x3712": "4k", "4096x4096": "4k", "3712x4608": "4k", "3584x4800": "4k",
	"3392x5056": "4k", "3072x5504": "4k",
}

func ImageSizePriceKey(modelName string) string {
	return imageSizePriceSettingPrefix + modelName
}

func HasImageSizePricing(modelName string) bool {
	setting, err := LoadImageSizePricing(modelName)
	return err == nil && setting != nil && setting.Enabled
}

func LoadImageSizePricing(modelName string) (*ImageSizePriceSetting, error) {
	PricingConfigRLock()
	defer PricingConfigRUnlock()
	return LoadImageSizePricingSnapshot(modelName)
}

// LoadImageSizePricingSnapshot reads a setting while the caller holds the pricing read lock.
func LoadImageSizePricingSnapshot(modelName string) (*ImageSizePriceSetting, error) {
	if modelName == "" {
		return nil, nil
	}
	common.OptionMapRWMutex.RLock()
	value, exists := common.OptionMap[ImageSizePriceKey(modelName)]
	common.OptionMapRWMutex.RUnlock()
	if !exists {
		return nil, nil
	}
	var setting ImageSizePriceSetting
	if err := common.UnmarshalJsonStr(value, &setting); err != nil {
		return nil, fmt.Errorf("parse image size pricing for model %s: %w", modelName, err)
	}
	if !setting.Enabled {
		return nil, nil
	}
	return &setting, nil
}

func GetImageSizePrice(modelName, size string) (float64, string, error) {
	setting, err := LoadImageSizePricing(modelName)
	if err != nil {
		return 0, "", err
	}
	if setting == nil {
		return 0, "", nil
	}
	return GetImageSizePriceFromSetting(modelName, size, setting)
}

func GetImageSizePriceFromSetting(modelName, size string, setting *ImageSizePriceSetting) (float64, string, error) {
	if setting == nil || !setting.Enabled {
		return 0, "", fmt.Errorf("image size pricing is not enabled for model %s", modelName)
	}
	if len(setting.Setting) == 0 {
		return 0, "", fmt.Errorf("image size price setting enabled but no prices configured for model %s", modelName)
	}

	tier, err := ResolveImageSizeTier(size)
	if err != nil {
		return 0, "", err
	}
	price, ok := setting.Setting[tier]
	if !ok {
		return 0, tier, fmt.Errorf("image size price for tier %s is not configured for model %s", strings.ToUpper(tier), modelName)
	}
	if price <= 0 {
		return 0, tier, fmt.Errorf("image size price for %s/%s must be greater than 0", modelName, strings.ToUpper(tier))
	}
	return price, tier, nil
}

func ResolveImageSizeTier(size string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(size))
	if normalized == "" || normalized == "auto" {
		return "1k", nil
	}
	if normalized == "1k" || normalized == "2k" || normalized == "4k" {
		return normalized, nil
	}
	if tier, ok := knownImageSizeTiers[normalized]; ok {
		return tier, nil
	}

	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return "", fmt.Errorf("unsupported image size %q: expected 1K, 2K, 4K, or WIDTHxHEIGHT", size)
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid image size %q", size)
	}
	longestEdge := max(width, height)
	if longestEdge <= 1024 {
		return "1k", nil
	}
	if longestEdge <= 2048 {
		return "2k", nil
	}
	return "4k", nil
}
