package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/xxuan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestXxuanRegistrationAndCacheGate(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeXxuan, constant.ChannelTypeXxuanUpscale} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			apiType, ok := common.ChannelType2APIType(channelType)
			require.True(t, ok)
			assert.Equal(t, constant.APITypeOpenAI, apiType)
			assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(channelType, "future-product"))
			platform := constant.TaskPlatform(strconv.Itoa(channelType))
			adaptor, ok := GetTaskAdaptor(platform).(*xxuan.TaskAdaptor)
			require.True(t, ok)
			assert.Equal(t, channelType == constant.ChannelTypeXxuanUpscale, adaptor.Upscale)
			for _, tc := range []struct {
				name     string
				media    int
				status   string
				progress int64
			}{
				{"pending", model.MediaStatusPending, "in_progress", 99},
				{"downloading", model.MediaStatusDownloading, "in_progress", 99},
				{"cached", model.MediaStatusSuccess, "completed", 100},
				{"failed", model.MediaStatusFailed, "failed", 100},
			} {
				t.Run(tc.name, func(t *testing.T) {
					task := &model.Task{TaskID: "public-id", Platform: platform, Status: model.TaskStatusSuccess,
						Progress: "100%", MediaStatus: tc.media, MediaURL: "https://gateway.example/media/public.mp4",
						PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", ResultURL: "https://provider.example/signed?secret=value"}}
					body, err := adaptor.ConvertToOpenAIVideo(task)
					require.NoError(t, err)
					body = applyVideoMediaPresentation(body, task)
					assert.Equal(t, tc.status, gjson.GetBytes(body, "status").String())
					assert.Equal(t, tc.progress, gjson.GetBytes(body, "progress").Int())
					assert.NotContains(t, string(body), "private-id")
					assert.NotContains(t, string(body), "provider.example")
					if tc.media == model.MediaStatusSuccess {
						assert.Equal(t, task.MediaURL, cachedPublicVideoURL(task))
						assert.Contains(t, string(body), task.MediaURL)
					} else {
						assert.Empty(t, cachedPublicVideoURL(task))
						assert.NotContains(t, string(body), task.MediaURL)
					}
				})
			}
		})
	}
}
