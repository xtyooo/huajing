package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type xxuanPollingAdaptor struct {
	result          relaycommon.TaskInfo
	fetches, parses int
	key, taskID     string
}

func (*xxuanPollingAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *xxuanPollingAdaptor) FetchTask(_ string, key string, body map[string]any, _ string) (*http.Response, error) {
	a.fetches++
	a.key, a.taskID = key, body["task_id"].(string)
	// Looks like the generic gateway envelope, but must use the provider parser.
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{"code":"success","data":{"status":"SUCCESS","task_id":"private-provider-id","result_url":"https://cdn.example.test/video?signature=private"},"material_ticket":"secret"}`))}, nil
}
func (a *xxuanPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	a.parses++
	return &a.result, nil
}
func (*xxuanPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int { return 0 }

func TestXxuanPollingDeadlinePersistsWithoutStatusChange(t *testing.T) {
	truncate(t)
	const channelID = 97885
	seedTaskPollingChannelWithType(t, channelID, constant.ChannelTypeXxuan, true)
	ch, err := model.CacheGetChannel(channelID)
	require.NoError(t, err)
	task := seedPollingTask(t, channelID, "xxuan-public", "xxuan-private")
	task.Platform = constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeXxuan))
	task.Status, task.Progress = model.TaskStatusInProgress, "30%"
	task.PrivateData.Key = "selected-task-key"
	task.PrivateData.NextPollAt = time.Now().Unix() + 3600
	require.NoError(t, model.DB.Save(task).Error)
	a := &xxuanPollingAdaptor{result: relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "30%", NextPollAfterSeconds: 60}}
	tasks := map[string]*model.Task{task.GetUpstreamTaskID(): task}
	require.NoError(t, updateVideoSingleTask(context.Background(), a, ch, task.GetUpstreamTaskID(), tasks))
	assert.Zero(t, a.fetches)
	task.PrivateData.NextPollAt = 0
	require.NoError(t, updateVideoSingleTask(context.Background(), a, ch, task.GetUpstreamTaskID(), tasks))
	assert.Equal(t, 1, a.parses)
	assert.Equal(t, "selected-task-key", a.key)
	assert.Equal(t, "xxuan-private", a.taskID)
	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	assert.Greater(t, saved.PrivateData.NextPollAt, time.Now().Unix())
	assert.NotContains(t, string(saved.Data), "signature")
	assert.NotContains(t, string(saved.Data), "material_ticket")
	// Keep the sanitized response identical to force snapshot comparison to rely on NextPollAt.
	saved.PrivateData.NextPollAt = 0
	require.NoError(t, model.DB.Save(&saved).Error)
	tasks[saved.GetUpstreamTaskID()] = &saved
	require.NoError(t, updateVideoSingleTask(context.Background(), a, ch, saved.GetUpstreamTaskID(), tasks))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, saved.ID).Error)
	assert.Greater(t, reloaded.PrivateData.NextPollAt, time.Now().Unix())
	tasks[reloaded.GetUpstreamTaskID()] = &reloaded
	require.NoError(t, updateVideoSingleTask(context.Background(), a, ch, reloaded.GetUpstreamTaskID(), tasks))
	assert.Equal(t, 2, a.fetches, "a reloaded task honors the persisted minimum interval")
}

func TestXxuanSuccessQueuesPrivateResultWithoutDownloadAuthorization(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeXxuan, constant.ChannelTypeXxuanUpscale} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			truncate(t)
			const channelID = 97886
			seedTaskPollingChannelWithType(t, channelID, channelType, true)
			ch, err := model.CacheGetChannel(channelID)
			require.NoError(t, err)
			task := seedPollingTask(t, channelID, "xxuan-cache-public", "xxuan-cache-private")
			task.Platform = constant.TaskPlatform(strconv.Itoa(channelType))
			require.NoError(t, model.DB.Save(task).Error)
			a := &xxuanPollingAdaptor{result: relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%", Url: "https://cdn.example.test/result?signature=private"}}
			require.NoError(t, updateVideoSingleTask(context.Background(), a, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			assert.Equal(t, model.MediaStatusPending, saved.MediaStatus)
			assert.Empty(t, saved.MediaURL)
			assert.Equal(t, a.result.Url, saved.PrivateData.ResultURL)
			assert.NotContains(t, string(saved.Data), "private")
			for _, resultURL := range []string{a.result.Url, constant.ChannelBaseURLs[channelType] + "/signed-result?signature=private"} {
				headers, err := mediaDownloadHeadersForResultURL(&saved, resultURL)
				require.NoError(t, err)
				for _, header := range headers {
					assert.Empty(t, header["Authorization"])
				}
			}
			assert.False(t, usesOpenAIVideoContentEndpoint(saved.Platform))
			assert.True(t, requiresValidatedVideoResultURL(saved.Platform))
			assert.Equal(t, ".mp4", resolveMediaFileExtension(saved.Platform, a.result.Url, "application/octet-stream"))
		})
	}
}
