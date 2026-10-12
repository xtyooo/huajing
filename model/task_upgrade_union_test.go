package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestTaskUpgradePreservesNativeAndPluginPrivateJSON(t *testing.T) {
	truncateTables(t)
	task := &Task{TaskID: "task_upgrade_union", Platform: "81", Status: TaskStatusSuccess, MediaURL: "https://local.example.test/media/result.mp4", MediaStatus: MediaStatusSuccess, MediaRetryCount: 2, MediaNextRetryAt: 100,
		Properties:  Properties{OriginModelName: "operator-video", UpstreamModelName: "upstream-video", Extra: map[string]any{"ratio": "16:9"}},
		PrivateData: TaskPrivateData{Key: "fixture-key", UpstreamTaskID: "private-id", ResultURL: "https://cdn.example.test/result.mp4", Execution: &TaskExecutionSnapshot{TaskPlugin: &TaskPluginSnapshot{Key: "fixture", Generation: 7}}, PluginState: []byte(`{"phase":2}`), PollFailures: 3, BillingContext: &TaskBillingContext{ImageSizePricing: true, ImageSizeTier: "2k", ModelPrice: 1.5, GroupRatio: 2, TieredSnapshot: &billingexpr.BillingSnapshot{ModelName: "operator-video"}}}, Data: []byte(`{}`)}
	insertTask(t, task)
	var loaded Task
	require.NoError(t, DB.First(&loaded, task.ID).Error)
	require.Equal(t, task.Properties, loaded.Properties)
	require.Equal(t, task.PrivateData, loaded.PrivateData)
	require.Equal(t, task.MediaURL, loaded.MediaURL)
	require.Equal(t, task.MediaRetryCount, loaded.MediaRetryCount)
	require.Equal(t, task.MediaNextRetryAt, loaded.MediaNextRetryAt)
	value, err := loaded.Properties.Value()
	require.NoError(t, err)
	require.IsType(t, "", value, "PG JSON uses text instead of bytea")
	public, err := common.Marshal(&loaded)
	require.NoError(t, err)
	require.NotContains(t, string(public), "fixture-key")
	require.NotContains(t, string(public), "private-id")
}

func TestAllNativeTaskTypesSnapshotSelectedCredential(t *testing.T) {
	for channelType := range constant.ChannelTypeDummy {
		if !constant.IsNativeTaskChannel(channelType) {
			continue
		}
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "operator-future-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, ApiKey: "fixture-selected-key"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
			task := InitTask(constant.TaskPlatform(strconv.Itoa(channelType)), info)
			require.Equal(t, "fixture-selected-key", task.PrivateData.Key)
		})
	}
}
