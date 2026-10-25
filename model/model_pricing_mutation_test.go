package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useModelPricingMutationTestDB(t *testing.T) {
	t.Helper()
	testDB := useOptionTestDB(t)
	require.NoError(t, testDB.AutoMigrate(&Model{}))
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
}

func float64Pointer(value float64) *float64 {
	return &value
}

func TestSaveModelWithPricingCommitsTogether(t *testing.T) {
	useModelPricingMutationTestDB(t)
	m := &Model{ModelName: "atomic-image-model", Status: 1, SyncOfficial: 0}
	pricing := ModelPricingMutation{
		Mode:            ModelPricingModeImageSize,
		ImageSizePrices: map[string]float64{"1k": 0.01, "2k": 0.02, "4k": 0.04},
	}

	err := SaveModelWithPricing(m, "", pricing)

	require.NoError(t, err)
	assert.NotZero(t, m.Id)
	var saved Model
	require.NoError(t, DB.First(&saved, m.Id).Error)
	assert.Equal(t, m.ModelName, saved.ModelName)
	price, tier, err := GetImageSizePrice(m.ModelName, "2K")
	require.NoError(t, err)
	assert.Equal(t, "2k", tier)
	assert.Equal(t, 0.02, price)
}

func TestSaveModelWithPricingRollsBackModelWhenOptionWriteFails(t *testing.T) {
	useModelPricingMutationTestDB(t)
	existing := &Model{ModelName: "before", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	require.NoError(t, DB.Exec(`
		CREATE TRIGGER reject_image_pricing
		BEFORE INSERT ON options
		WHEN NEW.key = 'image_size_price_setting.after'
		BEGIN
			SELECT RAISE(ABORT, 'forced option failure');
		END;
	`).Error)

	updated := *existing
	updated.ModelName = "after"
	err := SaveModelWithPricing(&updated, "before", ModelPricingMutation{
		Mode:            ModelPricingModeImageSize,
		ImageSizePrices: map[string]float64{"1k": 0.01, "2k": 0.02, "4k": 0.04},
	})

	require.Error(t, err)
	var saved Model
	require.NoError(t, DB.First(&saved, existing.Id).Error)
	assert.Equal(t, "before", saved.ModelName)
}

func TestSaveModelWithPricingRejectsInvalidImagePricesBeforeCreate(t *testing.T) {
	useModelPricingMutationTestDB(t)
	m := &Model{ModelName: "invalid-image-pricing", Status: 1}

	err := SaveModelWithPricing(m, "", ModelPricingMutation{
		Mode:            ModelPricingModeImageSize,
		ImageSizePrices: map[string]float64{"1k": 0.01, "2k": 0, "4k": 0.04},
	})

	require.ErrorContains(t, err, "2K")
	var count int64
	require.NoError(t, DB.Model(&Model{}).Where("model_name = ?", m.ModelName).Count(&count).Error)
	assert.Zero(t, count)
}

func TestSaveModelWithPricingPreservesOtherModelsFromLatestDatabaseState(t *testing.T) {
	useModelPricingMutationTestDB(t)
	existing := &Model{ModelName: "target", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	require.NoError(t, DB.Create(&Option{
		Key:   "ModelPrice",
		Value: `{"target":0.1,"other-model":0.9}`,
	}).Error)

	err := SaveModelWithPricing(existing, "target", ModelPricingMutation{
		Mode:  ModelPricingModePerRequest,
		Price: float64Pointer(0.2),
	})

	require.NoError(t, err)
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "ModelPrice").First(&option).Error)
	var prices map[string]float64
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &prices))
	assert.Equal(t, 0.2, prices["target"])
	assert.Equal(t, 0.9, prices["other-model"])
}

func TestSaveModelWithImagePricingClearsTieredConfiguration(t *testing.T) {
	useModelPricingMutationTestDB(t)
	existing := &Model{ModelName: "tiered-image", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_mode", Value: `{"tiered-image":"tiered_expr","other":"tiered_expr"}`}).Error)
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_expr", Value: `{"tiered-image":"tier(\"base\", p)","other":"tier(\"base\", p)"}`}).Error)

	err := SaveModelWithPricing(existing, existing.ModelName, ModelPricingMutation{
		Mode:            ModelPricingModeImageSize,
		ImageSizePrices: map[string]float64{"1k": 0.01, "2k": 0.02, "4k": 0.04},
	})

	require.NoError(t, err)
	for _, key := range []string{"billing_setting.billing_mode", "billing_setting.billing_expr"} {
		var option Option
		require.NoError(t, DB.Where(commonKeyCol+" = ?", key).First(&option).Error)
		var values map[string]string
		require.NoError(t, common.UnmarshalJsonStr(option.Value, &values))
		assert.NotContains(t, values, existing.ModelName)
		assert.Contains(t, values, "other")
	}
}

func TestSaveModelWithPerRequestPricingClearsTieredConfiguration(t *testing.T) {
	useModelPricingMutationTestDB(t)
	existing := &Model{ModelName: "tiered-fixed", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_mode", Value: `{"tiered-fixed":"tiered_expr","other":"tiered_expr"}`}).Error)
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_expr", Value: `{"tiered-fixed":"tier(\"base\", p)","other":"tier(\"base\", p)"}`}).Error)

	err := SaveModelWithPricing(existing, existing.ModelName, ModelPricingMutation{
		Mode:  ModelPricingModePerRequest,
		Price: float64Pointer(0.03),
	})

	require.NoError(t, err)
	for _, key := range []string{"billing_setting.billing_mode", "billing_setting.billing_expr"} {
		var option Option
		require.NoError(t, DB.Where(commonKeyCol+" = ?", key).First(&option).Error)
		var values map[string]string
		require.NoError(t, common.UnmarshalJsonStr(option.Value, &values))
		assert.NotContains(t, values, existing.ModelName)
		assert.Contains(t, values, "other")
	}
}

func TestSaveModelWithTieredPricingPreservesExistingExpression(t *testing.T) {
	useModelPricingMutationTestDB(t)
	existing := &Model{ModelName: "tiered-preserved", Description: "before", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	const modeValue = `{"tiered-preserved":"tiered_expr","other":"tiered_expr"}`
	const exprValue = `{"tiered-preserved":"tier(\"base\", p)","other":"tier(\"base\", c)"}`
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_mode", Value: modeValue}).Error)
	require.NoError(t, DB.Create(&Option{Key: "billing_setting.billing_expr", Value: exprValue}).Error)

	updated := *existing
	updated.Description = "after"
	err := SaveModelWithPricing(&updated, existing.ModelName, ModelPricingMutation{
		Mode: ModelPricingModeTiered,
	})

	require.NoError(t, err)
	for key, want := range map[string]string{
		"billing_setting.billing_mode": modeValue,
		"billing_setting.billing_expr": exprValue,
	} {
		var option Option
		require.NoError(t, DB.Where(commonKeyCol+" = ?", key).First(&option).Error)
		assert.JSONEq(t, want, option.Value)
	}
	var saved Model
	require.NoError(t, DB.First(&saved, existing.Id).Error)
	assert.Equal(t, "after", saved.Description)
}

func TestSaveModelWithPricingRenamesCreateCacheRatio(t *testing.T) {
	useModelPricingMutationTestDB(t)
	savedCreateCacheRatios := ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(savedCreateCacheRatios))
	})
	existing := &Model{ModelName: "old-cache-model", Status: 1, SyncOfficial: 1}
	require.NoError(t, existing.Insert())
	require.NoError(t, DB.Create(&Option{Key: "CreateCacheRatio", Value: `{"old-cache-model":1.7,"other":1.1}`}).Error)

	updated := *existing
	updated.ModelName = "new-cache-model"
	err := SaveModelWithPricing(&updated, existing.ModelName, ModelPricingMutation{
		Mode:  ModelPricingModePerToken,
		Ratio: float64Pointer(1),
	})

	require.NoError(t, err)
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "CreateCacheRatio").First(&option).Error)
	var ratios map[string]float64
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &ratios))
	assert.NotContains(t, ratios, existing.ModelName)
	assert.Equal(t, 1.7, ratios[updated.ModelName])
	assert.Equal(t, 1.1, ratios["other"])
	ratio, ok := ratio_setting.GetCreateCacheRatio(updated.ModelName)
	assert.True(t, ok)
	assert.Equal(t, 1.7, ratio)
}

func TestPricingOptionKeyClassification(t *testing.T) {
	for _, key := range []string{
		"ModelPrice",
		"ModelRatio",
		"billing_setting.billing_mode",
		"billing_setting.billing_expr",
		"billing_setting.skip_seconds",
		ImageSizePriceKey("image-model"),
		ResolutionPriceKey("video-model"),
	} {
		t.Run(key, func(t *testing.T) {
			assert.True(t, isPricingOptionKey(key))
		})
	}
	assert.False(t, isPricingOptionKey("SystemName"))
}

func TestUpdateOptionPricingWaitsForPersistenceWriter(t *testing.T) {
	useModelPricingMutationTestDB(t)

	pricingPersistenceMutex.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			pricingPersistenceMutex.Unlock()
		}
	})

	done := make(chan error, 1)
	go func() {
		done <- UpdateOption("ModelPrice", `{"concurrent-model":0.02}`)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("pricing update completed while another pricing writer held the persistence lock")
	case <-time.After(50 * time.Millisecond):
	}

	pricingPersistenceMutex.Unlock()
	locked = false
	require.NoError(t, <-done)
}

func TestPricingPersistenceDoesNotBlockRuntimeReaders(t *testing.T) {
	pricingPersistenceMutex.Lock()
	defer pricingPersistenceMutex.Unlock()

	done := make(chan struct{})
	go func() {
		PricingConfigRLock()
		PricingConfigRUnlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("runtime pricing reader was blocked by persistence work")
	}
}

func TestUpdateOptionRejectsInvalidPricingBeforeDatabaseWrite(t *testing.T) {
	useModelPricingMutationTestDB(t)
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
	})

	const validPrices = `{"protected-model":0.03}`
	require.NoError(t, UpdateOption("ModelPrice", validPrices))

	err := UpdateOption("ModelPrice", `{not-json}`)

	require.Error(t, err)
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "ModelPrice").First(&option).Error)
	assert.JSONEq(t, validPrices, option.Value)
	price, ok := ratio_setting.GetModelPrice("protected-model", false)
	assert.True(t, ok)
	assert.Equal(t, 0.03, price)
	common.OptionMapRWMutex.RLock()
	assert.JSONEq(t, validPrices, common.OptionMap["ModelPrice"])
	common.OptionMapRWMutex.RUnlock()
}

func TestUpdateOptionsBulkRejectsInvalidPricingBeforeAnyWrite(t *testing.T) {
	useModelPricingMutationTestDB(t)
	require.NoError(t, DB.Create(&Option{Key: "SystemName", Value: "before"}).Error)

	err := UpdateOptionsBulk(map[string]string{
		"SystemName": "after",
		"ModelPrice": `{not-json}`,
	})

	require.Error(t, err)
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "SystemName").First(&option).Error)
	assert.Equal(t, "before", option.Value)
}

func TestLoadOptionsIgnoresInvalidPricingWithoutClearingRuntimeState(t *testing.T) {
	useModelPricingMutationTestDB(t)
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
	})

	const validPrices = `{"protected-model":0.03}`
	require.NoError(t, UpdateOption("ModelPrice", validPrices))
	require.NoError(t, DB.Model(&Option{}).
		Where(commonKeyCol+" = ?", "ModelPrice").
		Update("value", `{not-json}`).Error)

	loadOptionsFromDatabase()

	price, ok := ratio_setting.GetModelPrice("protected-model", false)
	assert.True(t, ok)
	assert.Equal(t, 0.03, price)
	common.OptionMapRWMutex.RLock()
	assert.JSONEq(t, validPrices, common.OptionMap["ModelPrice"])
	common.OptionMapRWMutex.RUnlock()
}

func TestUpdateOptionRejectsIncompleteEnabledImageSizePricing(t *testing.T) {
	useModelPricingMutationTestDB(t)

	err := UpdateOption(ImageSizePriceKey("broken-image-model"), `{"enabled":true,"setting":{"1k":0.01,"2k":0.02}}`)

	require.ErrorContains(t, err, "4K")
	var count int64
	require.NoError(t, DB.Model(&Option{}).
		Where(commonKeyCol+" = ?", ImageSizePriceKey("broken-image-model")).
		Count(&count).Error)
	assert.Zero(t, count)
}
