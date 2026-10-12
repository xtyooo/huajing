package service

import (
	"context"
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

func TestTerminalRefundMarkerFailureStopsAutomaticReplay(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 867, 867, 867
	const walletAfterPreconsume, tokenAfterPreconsume, charged = 10_000, 7_000, 3_000
	seedUser(t, userID, walletAfterPreconsume)
	seedToken(t, tokenID, userID, "fixture-refund-reconciliation", tokenAfterPreconsume)
	seedTaskPollingChannelWithType(t, channelID, constant.ChannelTypeSnumom, true)
	seedChargedAccounting(t, userID, channelID, tokenID, charged, 1)

	task := makeTask(userID, channelID, charged, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_refund_marker_failure"
	task.Platform = constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSnumom))
	task.Progress = "30%"
	task.SubmitTime = time.Now().Unix()
	task.PrivateData.UpstreamTaskID = "fixture_upstream_refund"
	task.PrivateData.Key = "fixture-selected-channel-key"
	require.NoError(t, model.DB.Create(task).Error)
	var stale model.Task
	require.NoError(t, model.DB.First(&stale, task.ID).Error)

	// Allow the terminal CAS, then fail only the later refund-marker write.
	// Wallet/token/log writes succeed before the trigger rejects quota=0.
	require.NoError(t, model.DB.Exec(`CREATE TRIGGER fixture_fail_refund_marker
		BEFORE UPDATE OF quota ON tasks
		WHEN OLD.task_id = 'task_refund_marker_failure' AND OLD.quota > 0 AND NEW.quota = 0
		BEGIN SELECT RAISE(ABORT, 'fixture refund marker write failed'); END`).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Exec("DROP TRIGGER IF EXISTS fixture_fail_refund_marker").Error)
	})

	adaptor := &scriptedPollingAdaptor{statusCode: http.StatusOK, parse: &relaycommon.TaskInfo{
		Status: model.TaskStatusFailure,
		Reason: "Fixture provider failed",
	}}
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	ctx := context.Background()
	require.NoError(t, updateVideoSingleTask(ctx, adaptor, &channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	assert.Equal(t, "100%", persisted.Progress)
	assert.Equal(t, charged, persisted.Quota, "the failed marker remains for explicit reconciliation")
	assert.Zero(t, task.Quota, "the current worker knows funds have already been refunded")
	assert.Equal(t, walletAfterPreconsume+charged, getUserQuota(t, userID))
	assert.Equal(t, tokenAfterPreconsume+charged, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	used, requests := getUserUsageAccounting(t, userID)
	assert.Zero(t, used)
	assert.Equal(t, 1, requests)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	require.Equal(t, int64(1), countLogs(t))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, charged, log.Quota)
	assert.Equal(t, tokenID, log.TokenId)
	assert.Equal(t, "test-model", log.ModelName)

	// A concurrent worker that loaded the old nonterminal row loses its CAS.
	require.NoError(t, updateVideoSingleTask(ctx, adaptor, &channel, stale.GetUpstreamTaskID(), map[string]*model.Task{
		stale.GetUpstreamTaskID(): &stale,
	}))
	assert.Equal(t, walletAfterPreconsume+charged, getUserQuota(t, userID))
	assert.Equal(t, tokenAfterPreconsume+charged, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))

	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		t.Fatal("terminal task must not be resubmitted to the polling adaptor")
		return nil
	}
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	assert.Zero(t, RunTaskPollingOnce(ctx, nil).UnfinishedTasks)
	assert.Empty(t, model.GetTimedOutUnfinishedTasks(time.Now().Unix()+3600, 100))
	assert.Equal(t, walletAfterPreconsume+charged, getUserQuota(t, userID))
	assert.Equal(t, tokenAfterPreconsume+charged, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))
	assert.Equal(t, charged, getTaskQuota(t, task.ID), "never infer pending refund from this marker alone")
}
