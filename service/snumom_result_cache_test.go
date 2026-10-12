package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

type snumomEnvelopePollingAdaptor struct {
	body   []byte
	called bool
}

func (*snumomEnvelopePollingAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *snumomEnvelopePollingAdaptor) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(a.body))}, nil
}
func (a *snumomEnvelopePollingAdaptor) ParseTaskResult(*model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error) {
	a.called = true
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%", Url: "https://cdn.example.test/current.mp4?sig=a%2Fb"}, nil
}
func (*snumomEnvelopePollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestSnumomWrappedTaskUsesProviderParserAndQueuesDirectCache(t *testing.T) {
	truncate(t)
	const channelID = 97881
	seedTaskPollingChannelWithType(t, channelID, constant.ChannelTypeSnumom, true)
	ch, err := model.CacheGetChannel(channelID)
	require.NoError(t, err)
	task := seedPollingTask(t, channelID, "public-snumom", "private-snumom")
	task.Platform = constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSnumom))
	require.NoError(t, model.DB.Save(task).Error)
	adaptor := &snumomEnvelopePollingAdaptor{body: []byte(`{"code":"success","data":{"task_id":"private-snumom","status":"SUCCESS","metadata":{"url":"https://cdn.example.test/old.mp4"}}}`)}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
	var saved model.Task
	require.NoError(t, model.DB.First(&saved, task.ID).Error)
	require.True(t, adaptor.called)
	require.Equal(t, model.MediaStatusPending, saved.MediaStatus)
	require.Empty(t, saved.MediaURL)
	require.Equal(t, "https://cdn.example.test/current.mp4?sig=a%2Fb", saved.PrivateData.ResultURL)
}

func TestSnumomSignedContentAndCDNHaveDifferentAuthentication(t *testing.T) {
	previous := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previous })
	base := "https://snumom.example.test"
	ch := &model.Channel{Id: 999081, Type: constant.ChannelTypeSnumom, BaseURL: &base, Key: "other-key"}
	require.NoError(t, model.DB.Create(ch).Error)
	t.Cleanup(func() { model.DB.Delete(ch) })
	for _, tc := range []struct{ url, auth string }{
		{"https://snumom.example.test/v1/videos/private-id/content?exp=123&sign=a%2Fb", "Bearer selected-key"},
		{"https://cdn.example.test/generated.mp4?sig=a%2Fb", ""},
	} {
		task := &model.Task{TaskID: "public-id", ChannelId: ch.Id, Platform: constant.TaskPlatform(strconv.Itoa(ch.Type)), PrivateData: model.TaskPrivateData{Key: "selected-key", UpstreamTaskID: "private-id", ResultURL: tc.url}}
		target, err := resolveMediaDownloadTarget(task)
		require.NoError(t, err)
		require.Equal(t, tc.url, target.URL)
		require.NotEmpty(t, target.Headers)
		require.Equal(t, tc.auth, target.Headers[0]["Authorization"])
	}
	require.False(t, usesOpenAIVideoContentEndpoint(constant.TaskPlatform(strconv.Itoa(ch.Type))), "never invent a content URL for a model not documented to expose one")
	require.Equal(t, ".mp4", resolveMediaFileExtension(constant.TaskPlatform(strconv.Itoa(ch.Type)), base+"/v1/videos/private-id/content?sign=abc", "application/octet-stream"))
}
