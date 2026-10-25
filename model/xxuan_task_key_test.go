package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestXxuanTaskPreservesSelectedKeyAndPrivateSchedule(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeXxuan, constant.ChannelTypeXxuanUpscale} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: "public-alias", UserId: 99,
				ChannelMeta:   &relaycommon.ChannelMeta{ChannelType: channelType, ApiKey: "selected-key", UpstreamModelName: "opaque-product"},
				TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "public-task"},
			}
			task := InitTask(constant.TaskPlatform(strconv.Itoa(channelType)), info)
			require.Equal(t, "selected-key", task.PrivateData.Key)
			require.Equal(t, "public-task", task.TaskID)
			require.Equal(t, "public-alias", task.Properties.OriginModelName)
			require.Equal(t, "opaque-product", task.Properties.UpstreamModelName)
			before := task.Snapshot()
			task.PrivateData.NextPollAt = 1234
			require.False(t, before.Equal(task.Snapshot()), "unchanged status must still persist a new polling deadline")
			value, err := task.PrivateData.Value()
			require.NoError(t, err)
			var restored TaskPrivateData
			require.NoError(t, restored.Scan(value))
			require.Equal(t, task.PrivateData, restored)
		})
	}
}
