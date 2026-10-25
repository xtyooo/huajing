package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSnumomResolutionBillingSubmissionAndAccounting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const publicModel = "snumom-public-billing"
	const upstreamModel = "dreamina-seedance-renamed"
	const balance = 10000000
	const charge = 1400000 // $0.28/second * 10 seconds * group 1 * 500000.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch, oldLog, oldExport := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldBatch, oldLog, oldExport
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	user := model.User{Id: 98101, Username: "snumom-billing-test", Quota: balance, Status: common.UserStatusEnabled}
	token := model.Token{Id: 98101, UserId: user.Id, Key: "snumom-test-token", RemainQuota: balance, Status: common.TokenStatusEnabled}
	channel := model.Channel{Id: 98101, Type: constant.ChannelTypeSnumom, Name: "snumom-billing-test"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Create(&channel).Error)

	key := model.ResolutionPriceKey(publicModel)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	oldOption, exists := common.OptionMap[key]
	common.OptionMap[key] = `{"enabled":true,"setting":{"720p":0.28}}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if exists {
			common.OptionMap[key] = oldOption
		} else {
			delete(common.OptionMap, key)
		}
	})
	oldGroups, oldPrices := ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	settings, ok := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	require.True(t, ok)
	oldSkip := settings.SkipSeconds
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		settings.SkipSeconds = oldSkip
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"snumom-public-billing":0.07}`))

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/videos", r.URL.Path)
		assert.Equal(t, "Bearer selected-provider-key", r.Header.Get("Authorization"))
		assert.Equal(t, "task_public_billing", r.Header.Get("Idempotency-Key"))
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		assert.Equal(t, upstreamModel, body["model"])
		assert.Equal(t, float64(10), body["seconds"])
		assert.Equal(t, "720P", body["resolution"])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"private-provider-task","status":"queued","error":null}`))
	}))
	t.Cleanup(server.Close)
	newContext := func(payload string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("platform", strconv.Itoa(constant.ChannelTypeSnumom))
		c.Set("model_mapping", `{"snumom-public-billing":"dreamina-seedance-renamed"}`)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSnumom)
		common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
		common.SetContextKey(c, constant.ContextKeyChannelKey, "selected-provider-key")
		t.Cleanup(func() { common.CleanupBodyStorage(c) })
		info := &relaycommon.RelayInfo{
			OriginModelName: publicModel, UserGroup: "default", UsingGroup: "default", UserId: user.Id,
			TokenId: token.Id, TokenKey: token.Key, IsPlayground: true,
			UserSetting:   relaydto.UserSetting{BillingPreference: "wallet_only"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_billing"},
		}
		return c, recorder, info
	}
	t.Run("valid submission precharges once and logs exact specification", func(t *testing.T) {
		settings.SkipSeconds = map[string]bool{publicModel: true} // A stale toggle cannot erase the absolute per-second charge.
		c, recorder, info := newContext(`{"model":"snumom-public-billing","prompt":"scene","seconds":"10","resolution":"720P"}`)
		result, taskErr := RelayTaskSubmit(c, info)
		require.Nil(t, taskErr)
		require.NotNil(t, result)
		assert.Equal(t, 1, calls)
		assert.Equal(t, charge, result.Quota)
		assert.Equal(t, charge, info.Billing.GetPreConsumedQuota())
		assert.Empty(t, info.PriceData.OtherRatios())
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "task_public_billing")
		assert.NotContains(t, recorder.Body.String(), "private-provider-task")
		assert.Equal(t, "private-provider-task", result.UpstreamTaskID)
		assert.Equal(t, publicModel, info.OriginModelName)
		assert.Equal(t, upstreamModel, info.UpstreamModelName)
		require.NoError(t, info.Billing.Settle(result.Quota))
		assert.False(t, info.Billing.NeedsRefund())
		service.LogTaskConsumption(c, info)
		var chargedUser model.User
		var chargedToken model.Token
		var log model.Log
		require.NoError(t, db.First(&chargedUser, user.Id).Error)
		require.NoError(t, db.First(&chargedToken, token.Id).Error)
		require.NoError(t, db.Where("user_id = ?", user.Id).First(&log).Error)
		assert.Equal(t, balance-charge, chargedUser.Quota)
		assert.Equal(t, charge, chargedUser.UsedQuota)
		assert.Equal(t, balance, chargedToken.RemainQuota, "isolated playground fixture exercises wallet accounting without token lookup")
		assert.Zero(t, chargedToken.UsedQuota)
		assert.Equal(t, charge, log.Quota)
		assert.Equal(t, publicModel, log.ModelName)
		var audit map[string]any
		require.NoError(t, common.Unmarshal([]byte(log.Other), &audit))
		assert.Equal(t, float64(10), audit["duration"])
		assert.Equal(t, "720p", audit["resolution"])
		assert.Equal(t, 0.28, audit["model_price"])
		assert.Equal(t, float64(1), audit["group_ratio"])
	})
	t.Run("unsafe estimate rejected before billing or upstream", func(t *testing.T) {
		for _, attributes := range []string{`"resolution":"720p"`, `"seconds":-1,"resolution":"720p"`, `"duration":10,"resolution":"720p","metadata":{"seconds":20}`} {
			c, _, info := newContext(`{"model":"snumom-public-billing","prompt":"scene",` + attributes + `}`)
			result, taskErr := RelayTaskSubmit(c, info)
			require.Nil(t, result)
			require.NotNil(t, taskErr)
			assert.True(t, taskErr.LocalError)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Nil(t, info.Billing)
			assert.Equal(t, 1, calls)
		}
	})
	t.Run("oversized prices fail safely before precharge", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[key] = `{"enabled":true,"setting":{"720p":100000000}}`
		common.OptionMapRWMutex.Unlock()
		c, _, info := newContext(fmt.Sprintf(`{"model":"snumom-public-billing","prompt":"scene","duration":%d,"resolution":"720p"}`, relaycommon.MaxTaskDurationSeconds))
		result, taskErr := RelayTaskSubmit(c, info)
		require.Nil(t, result)
		require.NotNil(t, taskErr)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		require.NotNil(t, info.QuotaClamp)
		assert.Equal(t, common.MaxQuota, info.PriceData.Quota)
		assert.Nil(t, info.Billing)
		assert.Equal(t, 1, calls)
	})
	t.Run("disabled resolution mode preserves fixed price and skip seconds", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[key] = `{"enabled":false}`
		common.OptionMapRWMutex.Unlock()
		for _, skip := range []bool{false, true} {
			settings.SkipSeconds = map[string]bool{publicModel: skip}
			c, _, info := newContext(`{"model":"snumom-public-billing","prompt":"scene","seconds":"10","resolution":"720P"}`)
			result, taskErr := RelayTaskSubmit(c, info)
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			assert.Equal(t, 35000, result.Quota, "configured fixed $0.07 price must not multiply by 10 seconds")
			assert.Empty(t, info.PriceData.OtherRatios())
			require.NoError(t, info.Billing.Settle(result.Quota))
		}
		assert.Equal(t, 3, calls)
	})
}
