package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/snumom"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSnumomVideoRegistration(t *testing.T) {
	channelType := constant.ChannelTypeSnumom
	apiType, ok := common.ChannelType2APIType(channelType)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(channelType, "future-snumom-model"))
	require.IsType(t, &snumom.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelType))))
}

func TestSnumomPublicVideoIsGatedByCacheCompletion(t *testing.T) {
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSnumom))
	for _, tc := range []struct {
		name     string
		media    int
		status   string
		progress int64
	}{
		{"pending", model.MediaStatusPending, "in_progress", 99},
		{"downloading", model.MediaStatusDownloading, "in_progress", 99},
		{"cached", model.MediaStatusSuccess, "completed", 100},
		{"download failed", model.MediaStatusFailed, "failed", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID: "public-id", Platform: platform, Status: model.TaskStatusSuccess,
				Progress: "100%", MediaStatus: tc.media, MediaURL: "https://huajingapi.top/media/public.mp4",
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", ResultURL: "https://upstream.example/private.mp4"},
			}
			body, err := (&snumom.TaskAdaptor{}).ConvertToOpenAIVideo(task)
			require.NoError(t, err)
			body = applyVideoMediaPresentation(body, task)
			require.Equal(t, tc.status, gjson.GetBytes(body, "status").String())
			require.Equal(t, tc.progress, gjson.GetBytes(body, "progress").Int())
			require.NotContains(t, string(body), "private-id")
			require.NotContains(t, string(body), "upstream.example")
			if tc.media == model.MediaStatusSuccess {
				require.Equal(t, task.MediaURL, cachedPublicVideoURL(task))
				require.Contains(t, string(body), task.MediaURL)
			} else {
				require.Empty(t, cachedPublicVideoURL(task))
				require.NotContains(t, string(body), task.MediaURL)
			}
		})
	}
}
