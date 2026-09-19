package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/wanchen"
	"github.com/QuantumNous/new-api/relay/channel/task/yaochen"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"strconv"
	"testing"
)

func TestWanchenYaochenRegistration(t *testing.T) {
	for _, tc := range []struct {
		channelType int
		adaptor     any
	}{
		{constant.ChannelTypeWanchen, &wanchen.TaskAdaptor{}},
		{constant.ChannelTypeYaochen, &yaochen.TaskAdaptor{}},
	} {
		api, ok := common.ChannelType2APIType(tc.channelType)
		require.True(t, ok)
		require.Equal(t, constant.APITypeOpenAI, api)
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(tc.channelType, "admin-model"))
		require.IsType(t, tc.adaptor, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(tc.channelType))))
	}
}

func TestWanchenYaochenPublicCachePresentation(t *testing.T) {
	for _, kind := range []int{constant.ChannelTypeWanchen, constant.ChannelTypeYaochen} {
		adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(kind)))
		require.NotNil(t, adaptor)
		for _, tc := range []struct {
			media    int
			status   string
			progress int
		}{
			{model.MediaStatusPending, "in_progress", 99},
			{model.MediaStatusDownloading, "in_progress", 99},
			{model.MediaStatusSuccess, "completed", 100},
			{model.MediaStatusFailed, "failed", 100},
		} {
			task := &model.Task{Platform: constant.TaskPlatform(strconv.Itoa(kind)), TaskID: "public-id", Status: model.TaskStatusSuccess, Progress: "100%", MediaStatus: tc.media, MediaURL: "https://huajingapi.top/media/public-id.mp4", FailReason: "download failed"}
			body := applyVideoMediaPresentation([]byte(`{"id":"public-id","status":"completed","progress":100}`), task)
			require.Equal(t, tc.status, gjson.GetBytes(body, "status").String())
			require.EqualValues(t, tc.progress, gjson.GetBytes(body, "progress").Int())
		}
	}
}
