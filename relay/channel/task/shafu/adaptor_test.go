package shafu

import (
	"bytes"
	"io"
	"mime/multipart"
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

func TestBuildRequestBodyPreservesShafuVideoFields(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(`{
		"model":"gateway-alias",
		"prompt":"@Image1 follows @Video1 and uses @Audio1",
		"duration":"10",
		"aspectRatio":"9:16",
		"negativePrompt":"blur",
		"generateAudio":false,
		"referenceMode":"image",
		"images":["https://example.test/person.png"],
		"referenceVideos":["https://example.test/motion.mp4"],
		"referenceAudios":["https://example.test/voice.mp3"],
		"face_processing":true,
		"idempotency_key":"request-001"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	info.UpstreamModelName = "sdf-720p"
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "sdf-720p", body["model"])
	assert.Equal(t, "10", body["duration"])
	assert.Equal(t, "9:16", body["aspectRatio"])
	assert.Equal(t, "blur", body["negativePrompt"])
	assert.Equal(t, false, body["generateAudio"])
	assert.Equal(t, "image", body["referenceMode"])
	assert.Equal(t, []any{"https://example.test/person.png"}, body["images"])
	assert.Equal(t, []any{"https://example.test/motion.mp4"}, body["referenceVideos"])
	assert.Equal(t, []any{"https://example.test/voice.mp3"}, body["referenceAudios"])
	assert.Equal(t, true, body["face_processing"])
	assert.Equal(t, "request-001", body["idempotency_key"])
	assert.Equal(t, map[string]float64{"seconds": 10}, adaptor.EstimateBilling(c, info))
	assert.False(t, adaptor.SupportsImageSizePricing())
}

func TestValidateAndBillingUseDurationBeforeSecondsWithoutSizeMultiplier(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(`{
		"model":"sd-720p",
		"prompt":"city",
		"duration":9,
		"seconds":"12",
		"aspect_ratio":"16:9",
		"size":"1792x1024"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.UpstreamModelName = "sd-720p"
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	assert.Equal(t, map[string]float64{"seconds": 9}, adaptor.EstimateBilling(c, info))
}

func TestMultipartDurationParticipatesInBilling(t *testing.T) {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	require.NoError(t, writer.WriteField("model", "sd-480p"))
	require.NoError(t, writer.WriteField("prompt", "coast"))
	require.NoError(t, writer.WriteField("duration", "6"))
	require.NoError(t, writer.WriteField("aspect_ratio", "4:3"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &payload)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.UpstreamModelName = "sd-480p"
	adaptor := &TaskAdaptor{}

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	assert.Equal(t, map[string]float64{"seconds": 6}, adaptor.EstimateBilling(c, info))
}

func TestValidateRequestRejectsProviderIncompatibleInputs(t *testing.T) {
	tests := []struct {
		name          string
		upstreamModel string
		body          string
		code          string
	}{
		{name: "unsupported mapped model", upstreamModel: "sdf-1080p", body: `{"model":"alias","prompt":"p"}`, code: "unsupported_model"},
		{name: "duration below minimum", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","duration":3}`, code: "invalid_duration"},
		{name: "duration above maximum", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","duration":16}`, code: "invalid_duration"},
		{name: "decimal duration", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","duration":4.5}`, code: "invalid_duration"},
		{name: "numeric seconds", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","seconds":8}`, code: "invalid_json"},
		{name: "invalid ratio", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","ratio":"2:1"}`, code: "invalid_aspect_ratio"},
		{name: "invalid size", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","size":"1000x720"}`, code: "invalid_size"},
		{name: "invalid mode", upstreamModel: "sd-720p", body: `{"model":"sd-720p","prompt":"p","reference_mode":"video"}`, code: "invalid_reference_mode"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			info.UpstreamModelName = test.upstreamModel

			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestRatioFromSizeAcceptsDocumented480pDimensions(t *testing.T) {
	ratio, ok := ratioFromSize("854x480")

	require.True(t, ok)
	assert.Equal(t, "16:9", ratio)
}

func TestParseTaskResultReadsMetadataResultURL(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"upstream-private",
		"status":"completed",
		"progress":100,
		"metadata":{"result_url":"https://cdn.example.test/video.mp4"}
	}`))

	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Equal(t, "https://cdn.example.test/video.mp4", result.Url)
}

func TestConvertToOpenAIVideoHidesUpstreamIdentifiersAndURL(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_public",
		MediaStatus: model.MediaStatusDownloading,
		Data: []byte(`{
			"id":"upstream-private",
			"task_id":"upstream-private",
			"status":"completed",
			"metadata":{"result_url":"https://upstream.example.test/private.mp4"}
		}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "task_public", body["id"])
	assert.Equal(t, "task_public", body["task_id"])
	metadata, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", metadata["result_url"])
}

func TestShafuModelCatalogMatchesProvider(t *testing.T) {
	assert.Equal(t, []string{"sd-480p", "sd-720p", "sd-1080p", "sdf-480p", "sdf-720p"}, (&TaskAdaptor{}).GetModelList())
	assert.Equal(t, "shafu", (&TaskAdaptor{}).GetChannelName())
}
