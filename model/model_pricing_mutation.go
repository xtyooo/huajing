package model

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ModelPricingModePerToken   = "per-token"
	ModelPricingModePerRequest = "per-request"
	ModelPricingModeResolution = "resolution"
	ModelPricingModeImageSize  = "image-size"
	ModelPricingModeTiered     = "tiered_expr"
)

type ModelPricingMutation struct {
	Mode                 string             `json:"mode"`
	Price                *float64           `json:"price,omitempty"`
	Ratio                *float64           `json:"ratio,omitempty"`
	CacheRatio           *float64           `json:"cache_ratio,omitempty"`
	CompletionRatio      *float64           `json:"completion_ratio,omitempty"`
	ImageRatio           *float64           `json:"image_ratio,omitempty"`
	AudioRatio           *float64           `json:"audio_ratio,omitempty"`
	AudioCompletionRatio *float64           `json:"audio_completion_ratio,omitempty"`
	SkipSeconds          bool               `json:"skip_seconds"`
	ResolutionPrices     map[string]float64 `json:"resolution_prices,omitempty"`
	ImageSizePrices      map[string]float64 `json:"image_size_prices,omitempty"`
}

var (
	pricingConfigMutex      sync.RWMutex
	pricingPersistenceMutex sync.Mutex
)

func isPricingOptionKey(key string) bool {
	switch key {
	case "ModelPrice", "ModelRatio", "CacheRatio", "CreateCacheRatio",
		"CompletionRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio",
		"GroupRatio", "GroupGroupRatio", "SelfUseModeEnabled", "PreConsumedQuota",
		"QuotaPerUnit", "quota_setting.enable_free_model_pre_consume",
		"billing_setting.billing_mode", "billing_setting.billing_expr", "billing_setting.skip_seconds":
		return true
	default:
		return strings.HasPrefix(key, imageSizePriceSettingPrefix) ||
			strings.HasPrefix(key, resolutionPriceSettingPrefix)
	}
}

func hasPricingOptionKeys(values map[string]string) bool {
	for key := range values {
		if isPricingOptionKey(key) {
			return true
		}
	}
	return false
}

func validatePricingOptionValue(key, value string) error {
	switch key {
	case "ModelPrice", "ModelRatio", "CacheRatio", "CreateCacheRatio",
		"CompletionRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio", "GroupRatio":
		return validatePricingJSONMap[float64](key, value)
	case "GroupGroupRatio":
		return validatePricingJSONMap[map[string]float64](key, value)
	case "billing_setting.billing_mode", "billing_setting.billing_expr":
		return validatePricingJSONMap[string](key, value)
	case "billing_setting.skip_seconds":
		return validatePricingJSONMap[bool](key, value)
	case "SelfUseModeEnabled", "quota_setting.enable_free_model_pre_consume":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid pricing option %s: %w", key, err)
		}
		return nil
	case "PreConsumedQuota":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("invalid pricing option %s: %w", key, err)
		}
		return nil
	case "QuotaPerUnit":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("invalid pricing option %s", key)
		}
		return nil
	default:
		if strings.HasPrefix(key, imageSizePriceSettingPrefix) {
			var setting ImageSizePriceSetting
			if err := common.UnmarshalJsonStr(value, &setting); err != nil {
				return fmt.Errorf("invalid pricing option %s: %w", key, err)
			}
			if setting.Enabled {
				for _, tier := range []string{"1k", "2k", "4k"} {
					price := setting.Setting[tier]
					if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
						return fmt.Errorf("invalid pricing option %s: %s price must be greater than 0", key, strings.ToUpper(tier))
					}
				}
			}
			return nil
		}
		if strings.HasPrefix(key, resolutionPriceSettingPrefix) {
			var setting ResolutionPriceSetting
			if err := common.UnmarshalJsonStr(value, &setting); err != nil {
				return fmt.Errorf("invalid pricing option %s: %w", key, err)
			}
			if setting.Enabled && len(setting.Setting) == 0 {
				return fmt.Errorf("invalid pricing option %s: enabled setting has no prices", key)
			}
			for resolution, price := range setting.Setting {
				if strings.TrimSpace(resolution) == "" || math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
					return fmt.Errorf("invalid pricing option %s: resolution prices must be finite and greater than 0", key)
				}
			}
			return nil
		}
		return nil
	}
}

func validatePricingJSONMap[T any](key, value string) error {
	var parsed map[string]T
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil {
		return fmt.Errorf("invalid pricing option %s: %w", key, err)
	}
	if parsed == nil {
		return fmt.Errorf("invalid pricing option %s: expected a JSON object", key)
	}
	return nil
}

func PricingConfigRLock() {
	pricingConfigMutex.RLock()
}

func PricingConfigRUnlock() {
	pricingConfigMutex.RUnlock()
}

func validateModelPricingMutation(pricing ModelPricingMutation) error {
	switch pricing.Mode {
	case ModelPricingModePerToken, ModelPricingModePerRequest, ModelPricingModeResolution, ModelPricingModeImageSize, ModelPricingModeTiered:
	default:
		return fmt.Errorf("unsupported model pricing mode %q", pricing.Mode)
	}
	for name, value := range map[string]*float64{
		"price": pricing.Price, "ratio": pricing.Ratio, "cache ratio": pricing.CacheRatio,
		"completion ratio": pricing.CompletionRatio, "image ratio": pricing.ImageRatio,
		"audio ratio": pricing.AudioRatio, "audio completion ratio": pricing.AudioCompletionRatio,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("%s must be a finite non-negative number", name)
		}
	}
	if pricing.Mode == ModelPricingModeImageSize {
		for _, tier := range []string{"1k", "2k", "4k"} {
			if price := pricing.ImageSizePrices[tier]; math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
				return fmt.Errorf("image size price for tier %s must be greater than 0", strings.ToUpper(tier))
			}
		}
	}
	return nil
}

func loadOptionMapForUpdate[T any](tx *gorm.DB, key string) (map[string]T, error) {
	empty := Option{Key: key, Value: "{}"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&empty).Error; err != nil {
		return nil, err
	}
	var option Option
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(commonKeyCol+" = ?", key).First(&option).Error; err != nil {
		return nil, err
	}
	values := make(map[string]T)
	if err := common.UnmarshalJsonStr(option.Value, &values); err != nil {
		return nil, fmt.Errorf("parse model pricing option %s: %w", key, err)
	}
	return values, nil
}

func marshalOptionValue(value interface{}) (string, error) {
	data, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func saveOptionValue(tx *gorm.DB, key string, value interface{}, written map[string]string) error {
	jsonValue, err := marshalOptionValue(value)
	if err != nil {
		return err
	}
	if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", jsonValue).Error; err != nil {
		return err
	}
	written[key] = jsonValue
	return nil
}

func updateModelOptionEntry[T any](tx *gorm.DB, key, modelName, oldModelName string, value *T, written map[string]string) error {
	values, err := loadOptionMapForUpdate[T](tx, key)
	if err != nil {
		return err
	}
	if oldModelName != "" && oldModelName != modelName {
		delete(values, oldModelName)
	}
	delete(values, modelName)
	if value != nil {
		values[modelName] = *value
	}
	return saveOptionValue(tx, key, values, written)
}

func renameModelOptionEntry[T any](tx *gorm.DB, key, modelName, oldModelName string, written map[string]string) error {
	if oldModelName == "" || oldModelName == modelName {
		return nil
	}
	values, err := loadOptionMapForUpdate[T](tx, key)
	if err != nil {
		return err
	}
	value, exists := values[oldModelName]
	delete(values, oldModelName)
	delete(values, modelName)
	if exists {
		values[modelName] = value
	}
	return saveOptionValue(tx, key, values, written)
}

func boolPointer(value bool) *bool {
	return &value
}

func disabledPriceSetting() map[string]interface{} {
	return map[string]interface{}{"enabled": false, "setting": map[string]float64{}}
}

func saveDynamicPriceSetting(tx *gorm.DB, key string, value interface{}, written map[string]string) error {
	empty := Option{Key: key, Value: "{}"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&empty).Error; err != nil {
		return err
	}
	return saveOptionValue(tx, key, value, written)
}

func preserveTieredModelPricing(tx *gorm.DB, modelName, oldModelName string, written map[string]string) error {
	sourceModelName := modelName
	if oldModelName != "" {
		sourceModelName = oldModelName
	}
	modes, err := loadOptionMapForUpdate[string](tx, "billing_setting.billing_mode")
	if err != nil {
		return err
	}
	expressions, err := loadOptionMapForUpdate[string](tx, "billing_setting.billing_expr")
	if err != nil {
		return err
	}
	mode, modeExists := modes[sourceModelName]
	expression, expressionExists := expressions[sourceModelName]
	if !modeExists || mode != ModelPricingModeTiered || !expressionExists || strings.TrimSpace(expression) == "" {
		return fmt.Errorf("tiered pricing configuration is missing for model %s", sourceModelName)
	}
	if sourceModelName == modelName {
		return nil
	}
	for _, key := range []string{
		"ModelPrice", "ModelRatio", "CacheRatio", "CreateCacheRatio", "CompletionRatio",
		"ImageRatio", "AudioRatio", "AudioCompletionRatio",
	} {
		if err := renameModelOptionEntry[float64](tx, key, modelName, sourceModelName, written); err != nil {
			return err
		}
	}
	if err := renameModelOptionEntry[bool](tx, "billing_setting.skip_seconds", modelName, sourceModelName, written); err != nil {
		return err
	}
	if err := updateModelOptionEntry(tx, "billing_setting.billing_mode", modelName, sourceModelName, &mode, written); err != nil {
		return err
	}
	return updateModelOptionEntry(tx, "billing_setting.billing_expr", modelName, sourceModelName, &expression, written)
}

func persistModelPricing(tx *gorm.DB, modelName, oldModelName string, pricing ModelPricingMutation, written map[string]string) error {
	if pricing.Mode == ModelPricingModeTiered {
		return preserveTieredModelPricing(tx, modelName, oldModelName, written)
	}
	if err := renameModelOptionEntry[float64](tx, "CreateCacheRatio", modelName, oldModelName, written); err != nil {
		return err
	}
	numericOptions := []struct {
		key   string
		value *float64
	}{
		{key: "ModelPrice"}, {key: "ModelRatio"}, {key: "CacheRatio"},
		{key: "CompletionRatio"}, {key: "ImageRatio"}, {key: "AudioRatio"},
		{key: "AudioCompletionRatio"},
	}
	if pricing.Mode == ModelPricingModePerRequest {
		numericOptions[0].value = pricing.Price
	} else if pricing.Mode == ModelPricingModePerToken {
		numericOptions[1].value = pricing.Ratio
		numericOptions[2].value = pricing.CacheRatio
		numericOptions[3].value = pricing.CompletionRatio
		numericOptions[4].value = pricing.ImageRatio
		numericOptions[5].value = pricing.AudioRatio
		numericOptions[6].value = pricing.AudioCompletionRatio
	}
	for _, option := range numericOptions {
		if err := updateModelOptionEntry(tx, option.key, modelName, oldModelName, option.value, written); err != nil {
			return err
		}
	}

	var skipSeconds *bool
	if pricing.Mode == ModelPricingModePerRequest && pricing.SkipSeconds {
		skipSeconds = boolPointer(true)
	}
	if err := updateModelOptionEntry(tx, "billing_setting.skip_seconds", modelName, oldModelName, skipSeconds, written); err != nil {
		return err
	}
	// This endpoint only accepts the four non-tiered modes above. Remove any
	// previous tiered expression so the selected mode becomes effective.
	if err := updateModelOptionEntry[string](tx, "billing_setting.billing_mode", modelName, oldModelName, nil, written); err != nil {
		return err
	}
	if err := updateModelOptionEntry[string](tx, "billing_setting.billing_expr", modelName, oldModelName, nil, written); err != nil {
		return err
	}

	if oldModelName != "" && oldModelName != modelName {
		if err := saveDynamicPriceSetting(tx, ResolutionPriceKey(oldModelName), disabledPriceSetting(), written); err != nil {
			return err
		}
		if err := saveDynamicPriceSetting(tx, ImageSizePriceKey(oldModelName), disabledPriceSetting(), written); err != nil {
			return err
		}
	}
	resolutionSetting := disabledPriceSetting()
	if pricing.Mode == ModelPricingModeResolution {
		resolutionSetting = map[string]interface{}{"enabled": true, "setting": pricing.ResolutionPrices}
	}
	if err := saveDynamicPriceSetting(tx, ResolutionPriceKey(modelName), resolutionSetting, written); err != nil {
		return err
	}
	imageSizeSetting := disabledPriceSetting()
	if pricing.Mode == ModelPricingModeImageSize {
		imageSizeSetting = map[string]interface{}{"enabled": true, "setting": pricing.ImageSizePrices}
	}
	return saveDynamicPriceSetting(tx, ImageSizePriceKey(modelName), imageSizeSetting, written)
}

func refreshPricingOptions(options map[string]string) error {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validatePricingOptionValue(key, options[key]); err != nil {
			return fmt.Errorf("validate model pricing option %s: %w", key, err)
		}
		if err := updateOptionMap(key, options[key]); err != nil {
			return fmt.Errorf("refresh model pricing option %s: %w", key, err)
		}
	}
	return nil
}

func SaveModelWithPricing(m *Model, oldModelName string, pricing ModelPricingMutation) error {
	if m == nil || strings.TrimSpace(m.ModelName) == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	if err := validateModelPricingMutation(pricing); err != nil {
		return err
	}

	pricingPersistenceMutex.Lock()
	defer pricingPersistenceMutex.Unlock()
	writtenOptions := make(map[string]string)
	err := DB.Transaction(func(tx *gorm.DB) error {
		persistedModelName := ""
		if m.Id != 0 {
			var persisted Model
			if err := tx.Select("id", "model_name").First(&persisted, m.Id).Error; err != nil {
				return err
			}
			persistedModelName = persisted.ModelName
			if oldModelName != "" && oldModelName != persistedModelName {
				return fmt.Errorf("model name changed while pricing settings were being edited")
			}
		}

		var duplicateCount int64
		if err := tx.Model(&Model{}).Where("model_name = ? AND id <> ?", m.ModelName, m.Id).Count(&duplicateCount).Error; err != nil {
			return err
		}
		if duplicateCount > 0 {
			return fmt.Errorf("model name already exists")
		}

		now := common.GetTimestamp()
		if m.Id == 0 {
			m.CreatedTime = now
			m.UpdatedTime = now
			originalStatus := m.Status
			originalSyncOfficial := m.SyncOfficial
			if err := tx.Create(m).Error; err != nil {
				return err
			}
			if err := tx.Model(&Model{}).Where("id = ?", m.Id).Updates(map[string]interface{}{
				"status": originalStatus, "sync_official": originalSyncOfficial,
			}).Error; err != nil {
				return err
			}
		} else {
			m.UpdatedTime = now
			if err := tx.Model(&Model{}).Where("id = ?", m.Id).
				Select("model_name", "description", "icon", "tags", "vendor_id", "endpoints", "status", "sync_official", "name_rule", "updated_time").
				Updates(m).Error; err != nil {
				return err
			}
		}
		return persistModelPricing(tx, m.ModelName, persistedModelName, pricing, writtenOptions)
	})
	if err != nil {
		return err
	}
	pricingConfigMutex.Lock()
	defer pricingConfigMutex.Unlock()
	return refreshPricingOptions(writtenOptions)
}
