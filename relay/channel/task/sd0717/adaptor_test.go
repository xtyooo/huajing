package sd0717

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSD0717TestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestBuildRequestBodyForcesAivideModel(t *testing.T) {
	ctx := newSD0717TestContext(t, `{
		"model":"sd0717",
		"prompt":"test",
		"duration":10,
		"ratio":"16:9",
		"resolution":"720p"
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{}

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, UpstreamModelAivide2, payload["model"])
	assert.Equal(t, "test", payload["prompt"])
	assert.EqualValues(t, 10, payload["duration"])
	assert.Equal(t, "16:9", payload["ratio"])
	assert.Equal(t, "720p", payload["resolution"])
}

func TestParseTaskResultCompletedUsesVideoURLPriority(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"id":"task_upstream",
		"status":"completed",
		"progress":100,
		"video_url":"https://example.com/video.mp4",
		"stable_video_url":"https://example.com/stable.mp4",
		"result":"https://example.com/result.mp4"
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "https://example.com/video.mp4", result.Url)
}

func TestParseTaskResultCompletedFallsBackToStableVideoURLAndResult(t *testing.T) {
	adaptor := &TaskAdaptor{}

	stableResult, err := adaptor.ParseTaskResult([]byte(`{
		"id":"task_upstream",
		"status":"success",
		"stable_video_url":"https://example.com/stable.mp4",
		"result":"https://example.com/result.mp4"
	}`))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/stable.mp4", stableResult.Url)

	resultOnly, err := adaptor.ParseTaskResult([]byte(`{
		"id":"task_upstream",
		"status":"succeeded",
		"result":"https://example.com/result.mp4"
	}`))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/result.mp4", resultOnly.Url)
}

func TestParseTaskResultFailureSupportsStringAndObjectError(t *testing.T) {
	adaptor := &TaskAdaptor{}

	stringErr, err := adaptor.ParseTaskResult([]byte(`{
		"id":"task_upstream",
		"status":"failed",
		"error":"任务未成功生成视频"
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), stringErr.Status)
	assert.Equal(t, "任务未成功生成视频", stringErr.Reason)

	objectErr, err := adaptor.ParseTaskResult([]byte(`{
		"id":"task_upstream",
		"status":"error",
		"error":{"code":"task_failed","message":"上游失败"}
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), objectErr.Status)
	assert.Equal(t, "上游失败", objectErr.Reason)
}

func TestParseTaskResultEmptyStatusErrorDoesNotStayInProgress(t *testing.T) {
	adaptor := &TaskAdaptor{}

	openAIErr, err := adaptor.ParseTaskResult([]byte(`{
		"error":{"code":"invalid_token","message":"Invalid token"}
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), openAIErr.Status)
	assert.Equal(t, "Invalid token", openAIErr.Reason)

	generalErr, err := adaptor.ParseTaskResult([]byte(`{
		"code":"invalid_request",
		"message":"prompt is required"
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), generalErr.Status)
	assert.Equal(t, "prompt is required", generalErr.Reason)
}

func TestConvertToOpenAIVideoKeepsResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  1784125202,
		FinishTime: 1784125300,
		Properties: model.Properties{
			OriginModelName: "sd0717",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://example.com/direct.mp4",
		},
		Data: []byte(`{
			"id":"task_upstream",
			"status":"completed",
			"result":"https://example.com/upstream.mp4"
		}`),
	}

	data, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, common.Unmarshal(data, &out))
	assert.Equal(t, "task_public", out["id"])
	assert.Equal(t, "completed", out["status"])
	metadata, ok := out["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/direct.mp4", metadata["url"])
}
