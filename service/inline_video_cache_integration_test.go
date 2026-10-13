package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func nativeCacheDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	oldDB, oldCache, oldRedis, oldBatch := model.DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	oldMain := common.MainDatabaseType()
	oldFactory, oldLimit, oldTimeout := service.GetTaskAdaptorFunc, constant.TaskQueryLimit, constant.TaskTimeoutMinutes
	model.DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = db, false, false, false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	constant.TaskQueryLimit, constant.TaskTimeoutMinutes = 100, 0
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Setenv(common.MediaDirEnv, t.TempDir())
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = oldDB, oldCache, oldRedis, oldBatch
		common.SetMainDatabaseType(oldMain)
		service.GetTaskAdaptorFunc, constant.TaskQueryLimit, constant.TaskTimeoutMinutes = oldFactory, oldLimit, oldTimeout
		require.NoError(t, sqlDB.Close())
	})
	return db
}

type nativePayloadAdaptor struct {
	service.TaskPollingAdaptor
	body []byte
}

func (a *nativePayloadAdaptor) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(a.body))}, nil
}

func assertNativePublicVideo(t *testing.T, task *model.Task, wantProgress int) {
	t.Helper()
	video := task.ToOpenAIVideo()
	assert.Equal(t, wantProgress, video.Progress)
	if wantProgress == 100 {
		assert.Equal(t, "completed", string(video.Status))
		assert.Positive(t, video.CompletedAt)
	} else {
		assert.Equal(t, "in_progress", string(video.Status))
		assert.Zero(t, video.CompletedAt)
	}
}

func TestNativeVertexInlineActualParserCachesVideo(t *testing.T) {
	video := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42")
	encoded := base64.StdEncoding.EncodeToString(video)
	for _, body := range []string{
		`{"done":true,"response":{"videos":[{"mimeType":"video/mp4","bytesBase64Encoded":"` + encoded + `"}]}}`,
		`{"done":true,"response":{"bytesBase64Encoded":"` + encoded + `"}}`,
		`{"done":true,"response":{"video":"` + encoded + `"}}`,
	} {
		t.Run(strconv.Itoa(len(body)), func(t *testing.T) {
			db := nativeCacheDatabase(t)
			media := gin.New()
			router.SetMediaRouter(media)
			server := httptest.NewServer(media)
			defer server.Close()
			t.Setenv("MEDIA_BASE_URL", server.URL)
			base := "https://vertex.example.test"
			ch := &model.Channel{Name: "vertex fixture", Type: constant.ChannelTypeVertexAi, Status: common.ChannelStatusEnabled, BaseURL: &base, Key: "unused-fixture"}
			require.NoError(t, db.Create(ch).Error)
			task := &model.Task{TaskID: "task_vertex_inline", Platform: "41", UserId: 1, ChannelId: ch.Id, Status: model.TaskStatusInProgress, Progress: "30%", Action: constant.TaskActionTextToVideo, SubmitTime: time.Now().Unix(), PrivateData: model.TaskPrivateData{UpstreamTaskID: taskcommon.EncodeLocalTaskID("models/veo/operations/fixture"), BillingContext: &model.TaskBillingContext{PerCallBilling: true}}}
			require.NoError(t, db.Create(task).Error)
			service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
				return &nativePayloadAdaptor{TaskPollingAdaptor: relay.GetTaskAdaptor(platform), body: []byte(body)}
			}
			service.RunTaskPollingOnce(context.Background(), nil)
			var saved model.Task
			require.NoError(t, db.First(&saved, task.ID).Error)
			require.Equal(t, model.MediaStatusSuccess, saved.MediaStatus)
			assert.NotContains(t, string(saved.Data), encoded)
			assertNativePublicVideo(t, &saved, 100)
			u, err := url.Parse(saved.MediaURL)
			require.NoError(t, err)
			response, err := http.Get(server.URL + u.RequestURI())
			require.NoError(t, err)
			defer response.Body.Close()
			actual, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Equal(t, video, actual)
		})
	}
}

func TestNativeGeminiActualParserCacheAndStaticURL(t *testing.T) {
	db := nativeCacheDatabase(t)
	service.InitHttpClient()
	oldFetch := *system_setting.GetFetchSetting()
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{EnableSSRFProtection: true, AllowPrivateIp: true, AllowedPorts: []string{"1-65535"}}
	t.Cleanup(func() { *system_setting.GetFetchSetting() = oldFetch })
	video := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42")
	operation := "models/veo/operations/fixture"
	media := gin.New()
	router.SetMediaRouter(media)
	server := httptest.NewServer(media)
	defer server.Close()
	t.Setenv("MEDIA_BASE_URL", server.URL)
	const query = "signature=fixture%2Funchanged"
	uri := server.URL + "/generated.mp4?" + query
	queryEvents := make(chan string, 1)
	downloadEvents := make(chan struct{ key, query string }, 1)
	media.GET("/v1beta/models/veo/operations/fixture", func(c *gin.Context) {
		queryEvents <- c.GetHeader("x-goog-api-key")
		c.JSON(http.StatusOK, gin.H{"name": operation, "done": true, "response": gin.H{"generateVideoResponse": gin.H{"generatedVideos": []any{gin.H{"video": gin.H{"uri": uri}}}}}})
	})
	media.GET("/generated.mp4", func(c *gin.Context) {
		key := c.GetHeader("x-goog-api-key")
		select {
		case downloadEvents <- struct{ key, query string }{key, c.Request.URL.RawQuery}:
		default:
		}
		if key != "fixture-selected" {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Data(http.StatusOK, "video/mp4", video)
	})
	base := server.URL
	ch := &model.Channel{Name: "gemini fixture", Type: constant.ChannelTypeGemini, Status: common.ChannelStatusEnabled, BaseURL: &base, Key: "fixture-current-different"}
	require.NoError(t, db.Create(ch).Error)
	task := &model.Task{TaskID: "task_gemini_native", Platform: "24", UserId: 1, ChannelId: ch.Id, Status: model.TaskStatusInProgress, Progress: "30%", Action: constant.TaskActionTextToVideo, SubmitTime: time.Now().Unix(), PrivateData: model.TaskPrivateData{UpstreamTaskID: taskcommon.EncodeLocalTaskID(operation), Key: "fixture-selected", BillingContext: &model.TaskBillingContext{PerCallBilling: true}}}
	require.NoError(t, db.Create(task).Error)
	headers, err := service.MediaDownloadHeadersForResultURL(task, uri)
	require.NoError(t, err)
	require.NotEmpty(t, headers)
	assert.Equal(t, "fixture-selected", headers[0]["x-goog-api-key"], "new cache consumer keeps the legacy content reader's selected key")
	cdnHeaders, err := service.MediaDownloadHeadersForResultURL(task, "https://cdn.example.test/video.mp4")
	require.NoError(t, err)
	require.NotEmpty(t, cdnHeaders)
	assert.Empty(t, cdnHeaders[0])
	service.RunTaskPollingOnce(context.Background(), nil)
	var saved model.Task
	require.NoError(t, db.First(&saved, task.ID).Error)
	assert.Equal(t, "fixture-selected", <-queryEvents)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), saved.Status)
	require.Equal(t, model.MediaStatusPending, saved.MediaStatus, "actual legacy Gemini URI must reach the new cache consumer")
	assert.Equal(t, uri, saved.PrivateData.ResultURL)
	assertNativePublicVideo(t, &saved, 99)
	service.DownloadTaskResultForTest(&saved)
	require.NoError(t, db.First(&saved, task.ID).Error)
	require.Equal(t, model.MediaStatusSuccess, saved.MediaStatus)
	download := <-downloadEvents
	assert.Equal(t, "fixture-selected", download.key)
	assert.Equal(t, query, download.query)
	assertNativePublicVideo(t, &saved, 100)
	require.True(t, strings.HasPrefix(saved.MediaURL, server.URL+"/media/"))
	parsed, err := url.Parse(saved.MediaURL)
	require.NoError(t, err)
	response, err := http.Get(server.URL + parsed.RequestURI())
	require.NoError(t, err)
	defer response.Body.Close()
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, video, actual)
}
