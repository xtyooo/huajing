package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNativeTaskRegistrySurvivesPluginDisable(t *testing.T) {
	previous := constant.TaskPluginEnabled
	constant.TaskPluginEnabled = false
	t.Cleanup(func() { constant.TaskPluginEnabled = previous })
	for channelType := range constant.ChannelTypeDummy {
		if !constant.IsNativeTaskChannel(channelType) || channelType == constant.ChannelTypeSunoAPI {
			continue
		}
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelType)))
			require.NotNil(t, adaptor)
			require.Implements(t, (*channel.TaskAdaptor)(nil), adaptor)
			require.Implements(t, (*channel.OpenAIVideoConverter)(nil), adaptor)
		})
	}
	require.NotNil(t, GetTaskAdaptor(constant.TaskPlatformSuno))
	for _, channelType := range []int{constant.ChannelTypeAdvancedCustom, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI, constant.ChannelTypeTaskPlugin, constant.ChannelTypeVLLM, constant.ChannelTypeSGLang, constant.ChannelTypeDummy} {
		require.Nil(t, getNativeTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelType))))
	}
}

func TestNativeTaskParseCapturesPublicResponseWithoutFlushing(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"operator-future-model","prompt":"scene","seconds":"5"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{OriginModelName: "operator-future-model", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeSnumom, UpstreamModelName: "provider-future-model", ApiKey: "fixture-key"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor := GetTaskAdaptor("81")
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	result, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(`{"id":"provider_private","status":"queued"}`))}, info)
	require.Nil(t, taskErr)
	require.Empty(t, recorder.Body.String(), "parsing must not pass the durable insert barrier")
	require.Equal(t, "provider_private", result.UpstreamTaskID)
	response, ok := result.ClientResponse.(*channel.LegacyTaskClientResponse)
	require.True(t, ok)
	require.Contains(t, string(response.Body), "task_public")
	require.NotContains(t, string(response.Body), "provider_private")
	require.NotContains(t, string(response.Body), "fixture-key")
}

func TestNativeBridgePollUsesStoredPrivateIDAndSelectedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/videos/private-id", r.URL.Path)
		require.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"status":"completed","content":{"video_url":"https://cdn.example.test/current.mp4"}}`)
	}))
	defer server.Close()
	adaptor := GetTaskAdaptor("81")
	task := &model.Task{TaskID: "task_public", Platform: "81", Properties: model.Properties{UpstreamModelName: "operator-future-model"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", Key: "selected-key"}}
	resp, err := adaptor.FetchTask(server.URL, task.PrivateData.Key, task, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	result, err := adaptor.ParseTaskResult(task, resp, body)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "https://cdn.example.test/current.mp4", result.Url)
}

func TestNativeAcceptedMalformedResponseCannotRetrySubmission(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	adaptor := GetTaskAdaptor("81")
	_, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("malformed"))}, &relaycommon.RelayInfo{})
	require.NotNil(t, taskErr)
	require.True(t, taskErr.NoRetry)
}

func TestKuaiStandardHTTPQueryReturnsNoCacheDirectURL(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	task := &model.Task{TaskID: "task_kuai_public", UserId: 700, Platform: "65", Status: model.TaskStatusSuccess, Progress: "100%", MediaStatus: model.MediaStatusNotNeed, MediaURL: "https://cdn.example.test/direct.mp4", PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", ResultURL: "https://cdn.example.test/direct.mp4"}, Data: []byte(`{"id":"private-id","status":"completed"}`)}
	require.NoError(t, db.Create(task).Error)
	router := gin.New()
	// Invoke the real response builder through HTTP; its registered relay mode
	// is used here directly so authentication setup does not need live tokens.
	router.GET("/v1/videos/:task_id", func(c *gin.Context) {
		c.Set("id", 700)
		body, taskErr := videoFetchByIDRespBodyBuilder(c)
		require.Nil(t, taskErr)
		c.Data(http.StatusOK, "application/json", body)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/videos/task_kuai_public", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), task.MediaURL)
	require.Contains(t, recorder.Body.String(), `"progress":100`)
	require.NotContains(t, recorder.Body.String(), "private-id")
}
