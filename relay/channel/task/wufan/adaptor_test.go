package wufan

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

func newWufanTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestBuildRequestBodyConvertsOpenAIVideoRequestToWufanContent(t *testing.T) {
	ctx := newWufanTestContext(t, `{
		"model":"wufan",
		"prompt":"一只金色的猎犬在海边奔跑，阳光洒在毛发上，慢镜头特写",
		"duration":5,
		"ratio":"16:9",
		"resolution":"720p",
		"generate_audio":true,
		"images":["https://example.com/ref.jpg"],
		"audio_urls":["https://example.com/voice.mp3"]
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "wufan"}}

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, DefaultUpstreamModel, payload["model"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, "16:9", payload["ratio"])
	assert.EqualValues(t, 5, payload["duration"])
	assert.Equal(t, true, payload["generate_audio"])

	content, ok := payload["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 3)
	textItem := content[0].(map[string]any)
	assert.Equal(t, "text", textItem["type"])
	assert.Contains(t, textItem["text"], "金色的猎犬")
	imageItem := content[1].(map[string]any)
	assert.Equal(t, "image_url", imageItem["type"])
	assert.Equal(t, "https://example.com/ref.jpg", imageItem["image_url"].(map[string]any)["url"])
	audioItem := content[2].(map[string]any)
	assert.Equal(t, "audio_url", audioItem["type"])
	assert.Equal(t, "https://example.com/voice.mp3", audioItem["audio_url"].(map[string]any)["url"])
}

func TestBuildRequestBodyPreservesMappedSeedanceMiniModel(t *testing.T) {
	ctx := newWufanTestContext(t, `{
		"model":"wufan-mini",
		"prompt":"未来城市街道，电影感镜头",
		"duration":4
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: UpstreamModelSeedanceMini}}

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, UpstreamModelSeedanceMini, payload["model"])
}

func TestParseTaskResultSuccessExtractsNestedVideoURLAndUsage(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{
		"success":true,
		"message":"操作成功！",
		"code":0,
		"result":{
			"id":123456,
			"model":"Seedance-2.0",
			"status":"SUCCESS",
			"created_at":1751570400,
			"completed_at":1751571200,
			"expires_at":1751656800,
			"result":{
				"type":"video",
				"data":[{"url":"https://cdn.example.com/generated/video_123456.mp4","format":"mp4"}],
				"usage":{"total_tokens":1280}
			}
		}
	}`)

	result, err := adaptor.ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "https://cdn.example.com/generated/video_123456.mp4", result.Url)
	assert.Equal(t, 1280, result.TotalTokens)
}

func TestDoResponseAcceptsStringIDAndStringTimestamps(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	adaptor := &TaskAdaptor{}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewBufferString(`{
			"success":true,
			"message":"成功",
			"code":200,
			"result":{
				"id":"837544220138016768",
				"model":"Seedance-2.0-Mini",
				"status":"init",
				"created_at":"1785330371788"
			},
			"timestamp":"1785330371814"
		}`)),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: "Seedance-2.0-Mini",
	}

	upstreamID, _, taskErr := adaptor.DoResponse(ctx, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "837544220138016768", upstreamID)
	assert.Equal(t, http.StatusOK, recorder.Code)
	var out map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
	assert.Equal(t, "task_public", out["id"])
	assert.Equal(t, "queued", out["status"])
	assert.EqualValues(t, 1785330371, out["created_at"])
}

func TestParseTaskResultFailureUsesErrorMessage(t *testing.T) {
	adaptor := &TaskAdaptor{}

	result, err := adaptor.ParseTaskResult([]byte(`{
		"success":true,
		"result":{
			"id":123456,
			"status":"FAILED",
			"error":{"code":"task_failed","message":"生成失败"}
		}
	}`))

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), result.Status)
	assert.Equal(t, "生成失败", result.Reason)
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
			OriginModelName: "wufan",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://example.com/direct.mp4",
		},
		Data: []byte(`{
			"success":true,
			"result":{
				"id":123456,
				"model":"Seedance-2.0",
				"status":"SUCCESS",
				"result":{
					"type":"video",
					"data":[{"url":"https://example.com/upstream.mp4","format":"mp4"}],
					"usage":{"total_tokens":1280}
				}
			}
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
	assert.EqualValues(t, 1280, metadata["total_tokens"])
}
