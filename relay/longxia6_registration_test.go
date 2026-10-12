package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/longxia6"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"strconv"
	"testing"
)

func TestLongxia6RegistrationAndPublicCacheState(t *testing.T) {
	kind := constant.ChannelTypeLongxia6
	api, ok := common.ChannelType2APIType(kind)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, api)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(kind, "admin-model"))
	platform := constant.TaskPlatform(strconv.Itoa(kind))
	require.IsType(t, &longxia6.TaskAdaptor{}, getNativeTaskAdaptor(platform))
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
		task := &model.Task{TaskID: "public-id", Platform: platform, Status: model.TaskStatusSuccess, Progress: "100%", MediaStatus: tc.media, MediaURL: "https://huajingapi.top/media/public.mp4"}
		body := applyVideoMediaPresentation([]byte(`{"id":"public-id","status":"completed","progress":100}`), task)
		require.Equal(t, tc.status, gjson.GetBytes(body, "status").String())
		require.EqualValues(t, tc.progress, gjson.GetBytes(body, "progress").Int())
		url := cachedPublicVideoURL(task)
		if tc.media == model.MediaStatusSuccess {
			require.Equal(t, task.MediaURL, url)
		} else {
			require.Empty(t, url)
		}
	}
}
