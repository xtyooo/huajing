package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCanPersistRealtimeTaskStatus(t *testing.T) {
	tests := []struct {
		name   string
		status model.TaskStatus
		want   bool
	}{
		{name: "submitted", status: model.TaskStatusSubmitted, want: true},
		{name: "queued", status: model.TaskStatusQueued, want: true},
		{name: "in progress", status: model.TaskStatusInProgress, want: true},
		{name: "success", status: model.TaskStatusSuccess, want: false},
		{name: "failure", status: model.TaskStatusFailure, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canPersistRealtimeTaskStatus(tt.status))
		})
	}
}

func TestRealtimeGeminiCompletionWaitsForDurableMediaCache(t *testing.T) {
	database := setupRelayChannelDB(t)
	require.NoError(t, database.AutoMigrate(&model.Task{}))
	service.InitHttpClient()
	const publicID = "task_realtime_cache"
	const operation = "models/veo-configured/operations/fixture-private-operation"
	const staleURL = "https://local.example/media/stale.mp4"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, operation)
		assert.Equal(t, "fixture-selected-key", r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"models/veo-configured/operations/fixture-private-operation","done":true,"response":{}}`))
	}))
	t.Cleanup(server.Close)
	channel := model.Channel{Type: constant.ChannelTypeGemini, Name: "fixture-realtime", Key: "fixture-other-key", BaseURL: &server.URL, Status: common.ChannelStatusEnabled}
	require.NoError(t, database.Create(&channel).Error)
	task := model.Task{TaskID: publicID, Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)),
		UserId: 987, ChannelId: channel.Id, Status: model.TaskStatusInProgress, Progress: "50%", Quota: 1234,
		MediaURL: staleURL, PrivateData: model.TaskPrivateData{UpstreamTaskID: taskcommon.EncodeLocalTaskID(operation), Key: "fixture-selected-key"}}
	require.NoError(t, database.Create(&task).Error)

	for _, path := range []string{"/v1/videos/", "/v1/video/generations/"} {
		t.Run(path, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, path+publicID, nil)
			c.Params = gin.Params{{Key: "task_id", Value: publicID}}
			c.Set("id", task.UserId)
			body, taskErr := videoFetchByIDRespBodyBuilder(c)
			require.Nil(t, taskErr)
			if path == "/v1/videos/" {
				assert.Equal(t, "in_progress", gjson.GetBytes(body, "status").String())
				assert.Equal(t, int64(99), gjson.GetBytes(body, "progress").Int())
				assert.False(t, gjson.GetBytes(body, "completed_at").Exists())
			} else {
				assert.Equal(t, "processing", gjson.GetBytes(body, "data.status").String())
				assert.Empty(t, gjson.GetBytes(body, "data.url").String())
			}
			assert.NotContains(t, string(body), staleURL)
			assert.NotContains(t, string(body), "fixture-private-operation")
			assert.NotContains(t, string(body), "fixture-selected-key")
			var persisted model.Task
			require.NoError(t, database.First(&persisted, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
			assert.Equal(t, "50%", persisted.Progress)
			assert.Equal(t, model.MediaStatusNotNeed, persisted.MediaStatus)
			assert.Equal(t, 1234, persisted.Quota, "query never settles billing")
		})
	}
	assert.Equal(t, 2, calls)

	// Once the poller has persisted success, queries use that state and never
	// ask the provider to replace it or reschedule downloads.
	for _, mediaStatus := range []int{model.MediaStatusNotNeed, model.MediaStatusPending, model.MediaStatusDownloading, model.MediaStatusFailed, model.MediaStatusCleaned, model.MediaStatusSuccess} {
		t.Run(strconv.Itoa(mediaStatus), func(t *testing.T) {
			require.NoError(t, database.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
				"status": model.TaskStatusSuccess, "progress": "100%", "media_status": mediaStatus, "media_url": staleURL,
			}).Error)
			for _, path := range []string{"/v1/videos/", "/v1/video/generations/"} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, path+publicID, nil)
				c.Params = gin.Params{{Key: "task_id", Value: publicID}}
				c.Set("id", task.UserId)
				body, taskErr := videoFetchByIDRespBodyBuilder(c)
				require.Nil(t, taskErr)
				if mediaStatus == model.MediaStatusSuccess {
					assert.Contains(t, string(body), staleURL)
				} else {
					assert.NotContains(t, string(body), staleURL)
				}
				if mediaStatus == model.MediaStatusNotNeed && path == "/v1/videos/" {
					assert.Equal(t, "in_progress", gjson.GetBytes(body, "status").String())
					assert.Equal(t, int64(99), gjson.GetBytes(body, "progress").Int())
					assert.False(t, gjson.GetBytes(body, "completed_at").Exists())
				}
			}
			assert.Equal(t, 2, calls, "persisted terminal state prevents another provider poll")
		})
	}
}
