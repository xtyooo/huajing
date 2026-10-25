package relay

import (
	"context"
	"fmt"
	"net"
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
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestXxuanSubmissionBillingAndWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name                                     string
		upscale, fixed, resolution, insufficient bool
		want                                     int
	}{
		{name: "generation per second", want: 1000000},
		{name: "generation per task", fixed: true, want: 100000},
		{name: "generation resolution overrides stale fixed toggle", fixed: true, resolution: true, want: 2800000},
		{name: "upscale trusted ceil duration", upscale: true, want: 700000},
		{name: "upscale fixed price", upscale: true, fixed: true, want: 100000},
		{name: "upscale resolution price", upscale: true, resolution: true, want: 1960000},
		{name: "insufficient balance never creates paid task", upscale: true, insufficient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const publicModel = "xxuan-billing-public"
			const upstreamModel = "opaque-renamed-product"
			const balance = 10000000
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldRedis, oldBatch, oldLog, oldExport := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, true, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLogDB
				common.SetDatabaseTypes(oldMain, oldLogType)
				common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldBatch, oldLog, oldExport
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
			user := model.User{Id: 98501, Username: "xxuan-billing", Quota: balance, Status: common.UserStatusEnabled}
			if tc.insufficient {
				user.Quota = 1
			}
			token := model.Token{Id: 98501, UserId: user.Id, Key: strings.Repeat("x", 48), RemainQuota: balance, Status: common.TokenStatusEnabled}
			channelType := constant.ChannelTypeXxuan
			if tc.upscale {
				channelType = constant.ChannelTypeXxuanUpscale
			}
			ch := model.Channel{Id: 98501, Type: channelType, Name: "xxuan-test"}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.Create(&ch).Error)

			priceKey := model.ResolutionPriceKey(publicModel)
			common.OptionMapRWMutex.Lock()
			if common.OptionMap == nil {
				common.OptionMap = map[string]string{}
			}
			oldOption, hadOption := common.OptionMap[priceKey]
			common.OptionMap[priceKey] = `{"enabled":false}`
			if tc.resolution {
				common.OptionMap[priceKey] = `{"enabled":true,"setting":{"720p":0.28}}`
			}
			common.OptionMapRWMutex.Unlock()
			oldGroups, oldPrices := ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
			settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
			oldSkip := settings.SkipSeconds
			t.Cleanup(func() {
				common.OptionMapRWMutex.Lock()
				if hadOption {
					common.OptionMap[priceKey] = oldOption
				} else {
					delete(common.OptionMap, priceKey)
				}
				common.OptionMapRWMutex.Unlock()
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
				settings.SkipSeconds = oldSkip
			})
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"xxuan-billing-public":0.1}`))
			settings.SkipSeconds = map[string]bool{publicModel: tc.fixed}

			paidCalls, completeCalls := 0, 0
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/source.mp4":
					assert.Empty(t, r.Header.Get("Authorization"))
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"))
				case "/open/v1/uploads":
					assert.Equal(t, "Bearer selected-xxuan-key", r.Header.Get("Authorization"))
					_, _ = fmt.Fprintf(w, `{"upload_id":"upl_test","upload_mode":"single","upload_url":%q,"upload_method":"PUT","upload_headers":{"Content-Type":"video/mp4"},"max_bytes":1024,"material_ticket":"private-ticket"}`, server.URL+"/storage")
				case "/storage":
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Empty(t, r.Header.Get("Authorization"))
					w.Header().Set("ETag", `"source-etag"`)
				case "/open/v1/uploads/upl_test/complete":
					completeCalls++
					assert.Equal(t, "private-ticket", r.Header.Get("X-Material-Ticket"))
					var body map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &body))
					assert.NotContains(t, body, "media_metadata")
					_, _ = w.Write([]byte(`{"asset_id":"ast_trusted","media_kind":"video","duration_ms":6200}`))
				case "/open/v1/video/tasks", "/open/v1/video-upscale/tasks":
					paidCalls++
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "Bearer selected-xxuan-key", r.Header.Get("Authorization"))
					var body map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &body))
					if tc.upscale {
						assert.Equal(t, "/open/v1/video-upscale/tasks", r.URL.Path)
						assert.Len(t, body, 3)
						assert.Equal(t, map[string]any{"asset_id": "ast_trusted"}, body["source"])
						assert.Equal(t, float64(60), body["frame_rate"])
						assert.Regexp(t, `^[A-Za-z0-9_-]{16,100}$`, r.Header.Get("Idempotency-Key"))
					} else {
						assert.Equal(t, "/open/v1/video/tasks", r.URL.Path)
						assert.Equal(t, upstreamModel, body["product_id"])
						assert.Equal(t, float64(10), body["duration_seconds"])
						assert.Equal(t, "16:9", body["ratio"])
						assert.Equal(t, map[string]any{}, body["assets"])
						assert.NotContains(t, body, "model")
					}
					assert.Equal(t, "720p", body["resolution"])
					w.WriteHeader(http.StatusAccepted)
					_, _ = w.Write([]byte(`{"task_id":"private-xxuan-task","status":"SUBMITTED","next_poll_after_seconds":30,"locked_price":{"amount_cents":9999999,"currency":"CNY"}}`))
				default:
					t.Errorf("unexpected upstream route %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			fetch := system_setting.GetFetchSetting()
			oldFetch, oldTLS := *fetch, common.TLSInsecureSkipVerify
			fetch.EnableSSRFProtection = false
			common.TLSInsecureSkipVerify = true
			service.InitHttpClient()
			mediaTransport := service.GetHttpClient().Transport.(*http.Transport).Clone()
			mediaTransport.Proxy = nil
			mediaTransport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if address == "media.example:443" {
					address = server.Listener.Addr().String()
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			service.GetHttpClient().Transport = mediaTransport
			t.Cleanup(mediaTransport.CloseIdleConnections)
			t.Cleanup(func() { *fetch = oldFetch; common.TLSInsecureSkipVerify = oldTLS; service.InitHttpClient() })

			payload := `{"model":"xxuan-billing-public","prompt":"scene","seconds":"10","ratio":"16:9","resolution":"720p"}`
			if tc.upscale {
				payload = `{"model":"xxuan-billing-public","video_url":"https://media.example/source.mp4","seconds":"1","resolution":"720p","frame_rate":60}`
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("platform", strconv.Itoa(channelType))
			c.Set("model_mapping", `{"xxuan-billing-public":"opaque-renamed-product"}`)
			common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
			common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL+"/open/v1")
			common.SetContextKey(c, constant.ContextKeyChannelKey, "selected-xxuan-key")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{OriginModelName: publicModel, UserGroup: "default", UsingGroup: "default", UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, IsPlayground: true, UserSetting: relaydto.UserSetting{BillingPreference: "wallet_only"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_xxuan_public_billing"}}
			result, taskErr := RelayTaskSubmit(c, info)
			if tc.insufficient {
				require.NotNil(t, taskErr)
				assert.Nil(t, result)
				assert.Zero(t, paidCalls)
				assert.Equal(t, 1, completeCalls, "trusted duration preparation precedes precharge, never paid creation")
				return
			}
			require.Nil(t, taskErr)
			require.NotNil(t, result)
			assert.Equal(t, 1, paidCalls)
			assert.Equal(t, tc.want, result.Quota)
			require.NotNil(t, info.Billing)
			assert.Equal(t, tc.want, info.Billing.GetPreConsumedQuota())
			require.NoError(t, info.Billing.Settle(result.Quota))
			service.LogTaskConsumption(c, info)
			var charged model.User
			var log model.Log
			require.NoError(t, db.First(&charged, user.Id).Error)
			require.NoError(t, db.Where("user_id = ?", user.Id).First(&log).Error)
			assert.Equal(t, balance-tc.want, charged.Quota)
			assert.Equal(t, tc.want, log.Quota)
			assert.Equal(t, publicModel, log.ModelName)
			assert.Contains(t, recorder.Body.String(), "task_xxuan_public_billing")
			assert.NotContains(t, recorder.Body.String(), "private-xxuan-task")
			assert.NotContains(t, string(result.TaskData), "private-ticket")
			assert.True(t, c.GetBool("xxuan_task_submit_started"))
			assert.Positive(t, c.GetInt64("xxuan_next_poll_at"))
		})
	}
}
