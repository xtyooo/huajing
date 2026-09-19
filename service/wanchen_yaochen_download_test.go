package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func TestWanchenYaochenContentDownloadAndCDNAuth(t *testing.T) {
	previous := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previous })
	for _, kind := range []int{constant.ChannelTypeWanchen, constant.ChannelTypeYaochen} {
		t.Run(strconv.Itoa(kind), func(t *testing.T) {
			base := "https://provider.example.test/v1/"
			ch := &model.Channel{Id: 990000 + kind, Type: kind, BaseURL: &base, Key: "fallback"}
			require.NoError(t, model.DB.Create(ch).Error)
			t.Cleanup(func() { model.DB.Delete(ch) })
			task := &model.Task{ChannelId: ch.Id, Platform: constant.TaskPlatform(strconv.Itoa(kind)), TaskID: "public-id", PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", Key: "selected", ResultURL: taskcommon.BuildProxyURL("public-id")}}
			task.Data = []byte(`{"video_url":"https://127.0.0.1/rejected.mp4"}`)
			target, err := resolveMediaDownloadTarget(task)
			require.NoError(t, err)
			require.Equal(t, "https://provider.example.test/v1/videos/private-id/content", target.URL)
			require.Equal(t, "Bearer selected", target.Headers[0]["Authorization"])
			task.PrivateData.ResultURL = "https://cdn.example.test/video.mp4?signature=abc"
			target, err = resolveMediaDownloadTarget(task)
			require.NoError(t, err)
			require.Empty(t, target.Headers[0])
			task.PrivateData.ResultURL = "https://provider.example.test/v1/videos/private-id/content"
			target, err = resolveMediaDownloadTarget(task)
			require.NoError(t, err)
			require.Equal(t, "Bearer selected", target.Headers[0]["Authorization"])
		})
	}
}

func TestWanchenYaochenCompletedQueuesCache(t *testing.T) {
	for _, kind := range []int{constant.ChannelTypeWanchen, constant.ChannelTypeYaochen} {
		t.Run(strconv.Itoa(kind), func(t *testing.T) {
			truncate(t)
			channelID := 900 + kind
			seedTaskPollingChannelWithType(t, channelID, kind, true)
			task := seedPollingTask(t, channelID, "public-video", "private-video")
			task.Platform = constant.TaskPlatform(strconv.Itoa(kind))
			require.NoError(t, model.DB.Save(task).Error)
			adaptor := &taskPollingFetchAdaptor{responseBody: []byte(`{"task_id":"private-video","status":"completed","video_url":"https://127.0.0.1/rejected.mp4"}`), taskInfo: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%"}}
			previous := GetTaskAdaptorFunc
			GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
			t.Cleanup(func() { GetTaskAdaptorFunc = previous })
			require.NoError(t, UpdateVideoTasks(context.Background(), task.Platform, map[int][]string{channelID: {task.GetUpstreamTaskID()}}, map[string]*model.Task{task.GetUpstreamTaskID(): task}))
			var saved model.Task
			require.NoError(t, model.DB.First(&saved, task.ID).Error)
			require.Equal(t, model.MediaStatusPending, saved.MediaStatus)
			require.Empty(t, saved.MediaURL)
			require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), saved.PrivateData.ResultURL)
		})
	}
}
