package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestSnumomTaskPreservesSelectedUpstreamKey(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-alias",
		UserId:          99,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSnumom, ApiKey: "selected-key", UpstreamModelName: "grok-imagine-video-1.5"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "public-task"},
	}
	task := InitTask(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSnumom)), info)
	require.Equal(t, "selected-key", task.PrivateData.Key)
	require.Equal(t, "public-task", task.TaskID)
	require.Equal(t, "public-alias", task.Properties.OriginModelName)
	require.Equal(t, "grok-imagine-video-1.5", task.Properties.UpstreamModelName)
}
