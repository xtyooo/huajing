package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
