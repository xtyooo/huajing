package controller

import (
	"net/http"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The ordinary suite defaults to SQLite. The explicit selector lets the same
// unchanged HTTP/CAS contracts run once per disposable loopback SQL engine.
func combinedPricingContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	kind := os.Getenv("TEST_COMBINED_PRICING_DIALECT")
	if kind == "" || kind == "sqlite" {
		return modelManagementDB(t, "sqlite", "")
	}
	variable := ""
	switch kind {
	case "mysql":
		variable = "TEST_MYSQL_DSN"
	case "postgres":
		variable = "TEST_POSTGRES_DSN"
	default:
		t.Fatalf("unsupported combined pricing fixture dialect %q", kind)
	}
	dsn := os.Getenv(variable)
	require.NotEmpty(t, dsn, "the selected real-engine contract requires an explicit local fixture DSN")
	return modelManagementDB(t, kind, dsn)
}

func TestCombinedPricingSaveRejectsReadVersionAfterOfficialEditorChangesPrice(t *testing.T) {
	db := combinedPricingContractDB(t)
	metadata := model.Model{ModelName: "stale-editor-model", Description: "original", Status: 1, NameRule: model.NameRuleExact}
	require.NoError(t, db.Create(&metadata).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"stale-editor-model":0.2}`}).Error)
	before, err := model.GetModelPricingSnapshot([]string{metadata.ModelName})
	require.NoError(t, err)
	require.Len(t, before.Entries, 1)
	readVersion := before.Entries[0].Version
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: metadata.ModelName, ExpectedVersion: readVersion, Pricing: model.PricingValues{"ModelPrice": 0.9}}}))
	metadata.Description = "stale editor description"
	var response struct {
		Success bool
		Message string
	}
	recorder := modelManagementRequest(t, SaveModelWithPricingMeta, http.MethodPost, "/api/models/save", map[string]any{
		"model": metadata, "old_model_name": metadata.ModelName,
		"pricing":                   map[string]any{"mode": "per-request", "price": 0.2},
		"expected_pricing_versions": map[string]string{metadata.ModelName: readVersion},
	}, &response)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.False(t, response.Success)
	var persisted model.Model
	require.NoError(t, db.First(&persisted, metadata.Id).Error)
	assert.Equal(t, "original", persisted.Description)
	after, err := model.GetModelPricingSnapshot([]string{metadata.ModelName})
	require.NoError(t, err)
	require.Len(t, after.Entries, 1)
	assert.Equal(t, 0.9, after.Entries[0].Configured["ModelPrice"])
}

func TestScopedCombinedPricingReadPairsOptionsWithTheirPricingVersion(t *testing.T) {
	db := combinedPricingContractDB(t)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"paired-model":0.2,"other-model":0.9}`}).Error)
	snapshot, err := model.GetModelPricingSnapshot([]string{"paired-model"})
	require.NoError(t, err)
	var response struct {
		Success bool
		Data    []model.Option
		Version string `json:"pricing_version"`
	}
	modelManagementRequest(t, GetModelPricingOptionsMeta, http.MethodGet, "/api/models/pricing?model=paired-model", nil, &response)
	require.True(t, response.Success)
	assert.Equal(t, snapshot.Entries[0].Version, response.Version)
	var price string
	for _, option := range response.Data {
		if option.Key == "ModelPrice" {
			price = option.Value
		}
	}
	assert.JSONEq(t, `{"paired-model":0.2}`, price)
}

func TestCombinedPricingSaveChecksSourceAndDestinationReadVersions(t *testing.T) {
	for _, scenario := range []string{"rename target changed", "new target changed", "current versions", "legacy omitted"} {
		t.Run(scenario, func(t *testing.T) {
			db := combinedPricingContractDB(t)
			source := model.Model{ModelName: "source-model", Description: "original", Status: 1, NameRule: model.NameRuleExact}
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"source-model":0.2,"destination-model":0.4}`}).Error)
			read := func(name string) string {
				var response struct {
					Success bool
					Data    []model.Option
					Version string `json:"pricing_version"`
				}
				modelManagementRequest(t, GetModelPricingOptionsMeta, http.MethodGet, "/api/models/pricing?model="+name, nil, &response)
				require.True(t, response.Success)
				require.NotEmpty(t, response.Version)
				require.NotEmpty(t, response.Data, "version metadata must preserve Option-array compatibility")
				return response.Version
			}
			versions := map[string]string{"source-model": read("source-model"), "destination-model": read("destination-model")}
			updated := source
			updated.ModelName = "destination-model"
			updated.Description = "new description"
			if scenario == "new target changed" {
				updated.Id = 0
				delete(versions, "source-model")
			}
			if scenario == "rename target changed" || scenario == "new target changed" {
				require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: "destination-model", ExpectedVersion: versions["destination-model"], Pricing: model.PricingValues{"ModelPrice": 0.9}}}))
			}
			body := map[string]any{"model": updated, "old_model_name": "source-model", "pricing": map[string]any{"mode": "per-request", "price": 0.2}, "expected_pricing_versions": versions}
			if updated.Id == 0 {
				body["old_model_name"] = ""
			}
			if scenario == "legacy omitted" {
				delete(body, "expected_pricing_versions")
			}
			var response struct{ Success bool }
			recorder := modelManagementRequest(t, SaveModelWithPricingMeta, http.MethodPost, "/api/models/save", body, &response)
			if scenario == "current versions" || scenario == "legacy omitted" {
				assert.True(t, response.Success, recorder.Body.String())
				assert.Equal(t, http.StatusOK, recorder.Code)
				return
			}
			assert.Equal(t, http.StatusConflict, recorder.Code)
			assert.False(t, response.Success)
			var persisted model.Model
			require.NoError(t, db.First(&persisted, source.Id).Error)
			assert.Equal(t, "source-model", persisted.ModelName)
			assert.Equal(t, "original", persisted.Description)
			sourceSnapshot, err := model.GetModelPricingSnapshot([]string{"source-model"})
			require.NoError(t, err)
			assert.Equal(t, 0.2, sourceSnapshot.Entries[0].Configured["ModelPrice"], "a rejected rename/new save must preserve source pricing too")
			var count int64
			require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "destination-model").Count(&count).Error)
			assert.Zero(t, count, "failed guards must not leave a renamed/new metadata row")
			if scenario == "rename target changed" || scenario == "new target changed" {
				snapshot, err := model.GetModelPricingSnapshot([]string{"destination-model"})
				require.NoError(t, err)
				assert.Equal(t, 0.9, snapshot.Entries[0].Configured["ModelPrice"])
			}
		})
	}
}
