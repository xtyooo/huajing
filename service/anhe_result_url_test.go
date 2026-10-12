package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type anheEnvelopePollingAdaptor struct {
	responseBody []byte
	parseCalled  bool
}

func (a *anheEnvelopePollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *anheEnvelopePollingAdaptor) FetchTask(_ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(a.responseBody)),
	}, nil
}

func (a *anheEnvelopePollingAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	a.parseCalled = true
	return &relaycommon.TaskInfo{
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Url:      "https://cdn.example.test/current.mp4",
	}, nil
}

func (a *anheEnvelopePollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateVideoSingleTaskAnheEnvelopeUsesAdaptorURL(t *testing.T) {
	truncate(t)

	const channelID = 97001
	channel := &model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeAnhe,
		Name:    "anhe-envelope-test",
		Key:     "channel-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: stringPtr("https://anhe.example.test"),
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := &model.Task{
		TaskID:    "task_anhe_envelope",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe)),
		UserId:    1,
		ChannelId: channelID,
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			Key:            "channel-key",
			UpstreamTaskID: "remote-public",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &anheEnvelopePollingAdaptor{responseBody: []byte(`{"code":"success","data":{"status":"SUCCESS","task_id":"remote-public","data":{"status":"succeeded","content":{"video_url":"https://cdn.example.test/current.mp4"}},"result_url":"https://anhe.example.test/stale.mp4"}}`)}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, "remote-public", map[string]*model.Task{
		"remote-public": task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.True(t, adaptor.parseCalled, "Anhe must bypass the generic successful envelope parser")
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Equal(t, model.MediaStatusPending, reloaded.MediaStatus)
	assert.Equal(t, "https://cdn.example.test/current.mp4", reloaded.PrivateData.ResultURL)
	assert.NotContains(t, reloaded.PrivateData.ResultURL, "/v1/videos/task_anhe_envelope/content")
}

func TestResolveAnheMediaDownloadTargetRecoversNestedURLFromLegacyTask(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_anhe_legacy",
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe)),
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://huajingapi.top/v1/videos/task_anhe_legacy/content",
		},
		Data: []byte(`{"code":"success","data":{"status":"SUCCESS","data":{"status":"succeeded","content":{"video_url":"https://cdn.example.test/current.mp4"}},"result_url":"https://anhe.example.test/stale.mp4"}}`),
	}

	target, err := resolveMediaDownloadTarget(task)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.test/current.mp4", target.URL)
	require.Len(t, target.Headers, 1)
	assert.Nil(t, target.Headers[0], "Anhe CDN results must be downloaded without channel credentials")
}

func TestResolveAnheMediaDownloadTargetMissingURLReturnsError(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_anhe_missing_url",
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe)),
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://huajingapi.top/v1/videos/task_anhe_missing_url/content",
		},
		Data: []byte(`{"code":"success","data":{"status":"SUCCESS","data":{"status":"succeeded"}}}`),
	}

	_, err := resolveMediaDownloadTarget(task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing a valid video URL")
	assert.False(t, usesOpenAIVideoContentEndpoint(task.Platform))
}

func stringPtr(value string) *string { return &value }
