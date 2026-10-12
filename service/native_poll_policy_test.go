package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestNativeTransientPollDoesNotUsePluginFailureThreshold(t *testing.T) {
	truncate(t)
	previous := constant.TaskPollMaxFailures
	constant.TaskPollMaxFailures = 1
	t.Cleanup(func() { constant.TaskPollMaxFailures = previous })
	const userID, channelID = 911, 912
	seedUser(t, userID, 10000)
	task := makeTask(userID, channelID, 2500, 0, BillingSourceWallet, 0)
	task.Platform = "81"
	task.TaskID = "task_native_retry"
	task.PrivateData.UpstreamTaskID = "private-native"
	task.PrivateData.PollFailures = 25
	require.NoError(t, model.DB.Create(task).Error)
	adaptor := &scriptedPollingAdaptor{statusCode: http.StatusTooManyRequests}
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeSnumom, Key: "fixture", Status: common.ChannelStatusEnabled}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
	var loaded model.Task
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), loaded.Status)
	require.Equal(t, 2500, loaded.Quota)
	require.Equal(t, 10000, getUserQuota(t, userID))
	require.Zero(t, countLogs(t))
}
