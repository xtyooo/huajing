package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func TestLongxia6DirectDownloadNoCredentialsOrContentFallback(t *testing.T) {
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeLongxia6))
	require.False(t, usesOpenAIVideoContentEndpoint(platform))
	require.True(t, requiresValidatedVideoResultURL(platform))
	task := &model.Task{TaskID: "public-id", Platform: platform, PrivateData: model.TaskPrivateData{Key: "private-key", UpstreamTaskID: "private-id", ResultURL: "https://media.longxiaai.store/result.mp4?sig=a%2Fb"}}
	target, err := resolveMediaDownloadTarget(task)
	require.NoError(t, err)
	require.Equal(t, task.PrivateData.ResultURL, target.URL)
	require.Len(t, target.Headers, 1)
	require.Empty(t, target.Headers[0])
	task.PrivateData.ResultURL = ""
	task.Data = []byte(`{"video_url":"https://127.0.0.1/rejected","data":[{"media_type":"image/jpeg","url":"https://media.longxiaai.store/image.jpg"}]}`)
	target, err = resolveMediaDownloadTarget(task)
	require.NoError(t, err)
	require.Empty(t, target.URL)
}
