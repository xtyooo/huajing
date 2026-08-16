package anhe

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

func newAnheTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func TestBuildRequestBodyMapsStandardVideoProtocol(t *testing.T) {
	c, _ := newAnheTestContext(t, `{
		"model":"gateway-alias",
		"prompt":"cinematic scene",
		"aspect_ratio":"9:16",
		"resolution":"720p",
		"seconds":"15",
		"image_url":"https://example.test/main.png",
		"reference_image_urls":["https://example.test/ref.png"],
		"reference_video":"https://example.test/motion.mp4",
		"audio_url":"https://example.test/audio.mp3",
		"generate_audio":false,
		"return_last_frame":false,
		"seed":0
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "seedance-2-5-720p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "seedance-2-5-720p", body["model"])
	assert.EqualValues(t, 15, body["duration"])
	assert.Equal(t, "9:16", body["aspect_ratio"])
	assert.NotContains(t, body, "resolution")
	assert.Equal(t, false, body["generate_audio"])
	assert.Equal(t, false, body["return_last_frame"])
	assert.EqualValues(t, 0, body["seed"])
	assert.Len(t, body["image_with_roles"], 2)
	assert.Len(t, body["video_with_roles"], 1)
	assert.Len(t, body["audio_with_roles"], 1)
	assert.Equal(t, map[string]float64{"seconds": 15, "video_input": 2}, adaptor.EstimateBilling(c, info))
}

func TestStartEndFramesUseRolesAndAdaptiveRatio(t *testing.T) {
	c, _ := newAnheTestContext(t, `{
		"model":"seedance-2-5-480p",
		"prompt":"transition",
		"duration":-1,
		"aspect_ratio":"16:9",
		"reference_image_urls":["https://example.test/start.png","https://example.test/end.png"],
		"video_config":{"reference_mode":"start_end"}
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "seedance-2-5-480p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, -1, body.Duration)
	assert.Equal(t, "adaptive", body.AspectRatio)
	assert.Equal(t, []imageWithRole{
		{URL: "https://example.test/start.png", Role: "first_frame"},
		{URL: "https://example.test/end.png", Role: "last_frame"},
	}, body.ImageWithRoles)
	assert.Nil(t, adaptor.EstimateBilling(c, info), "automatic duration has no safe positive seconds estimate")
}

func TestValidateRequestRejectsInvalidProviderInputs(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "duration too short", body: `{"model":"m","prompt":"p","duration":3}`, code: "invalid_duration"},
		{name: "duration too long", body: `{"model":"m","prompt":"p","seconds":"31"}`, code: "invalid_duration"},
		{name: "invalid aspect ratio", body: `{"model":"m","prompt":"p","duration":4,"aspect_ratio":"2:1"}`, code: "invalid_aspect_ratio"},
		{name: "non https media", body: `{"model":"m","prompt":"p","duration":4,"image_url":"http://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "start frame count", body: `{"model":"m","prompt":"p","duration":4,"video_config":{"reference_mode":"start_frame"}}`, code: "invalid_reference_count"},
		{name: "start end with video", body: `{"model":"m","prompt":"p","duration":-1,"reference_image_urls":["https://example.test/a.png","https://example.test/b.png"],"reference_video":"https://example.test/v.mp4","video_config":{"reference_mode":"start_end"}}`, code: "invalid_reference_mode"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newAnheTestContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}})
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestBuildRequestHeaderUsesCallerOrPublicTaskIdempotencyKey(t *testing.T) {
	tests := []struct {
		name        string
		callerKey   string
		expectedKey string
	}{
		{name: "caller key", callerKey: "business-order-1", expectedKey: "business-order-1"},
		{name: "public task fallback", expectedKey: "task_public"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newAnheTestContext(t, `{"model":"m","prompt":"p","duration":4}`)
			c.Request.Header.Set("Idempotency-Key", test.callerKey)
			upstreamReq := httptest.NewRequest(http.MethodPost, "https://anhedean.cn/v1/videos", nil)
			adaptor := &TaskAdaptor{apiKey: "secret"}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

			require.NoError(t, adaptor.BuildRequestHeader(c, upstreamReq, info))
			assert.Equal(t, "Bearer secret", upstreamReq.Header.Get("Authorization"))
			assert.Equal(t, test.expectedKey, upstreamReq.Header.Get("Idempotency-Key"))
		})
	}
}

func TestDoRequestNormalizesAcceptedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"upstream"}`))
	}))
	t.Cleanup(server.Close)
	c, _ := newAnheTestContext(t, `{"model":"m","prompt":"p","duration":4}`)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "secret"}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

	resp, err := adaptor.DoRequest(c, info, bytes.NewBufferString(`{}`))

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDoResponseReturnsOnlyPublicTaskID(t *testing.T) {
	c, recorder := newAnheTestContext(t, `{}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"upstream-secret","status":"queued"}`)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: "gateway-model",
	}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "upstream-secret")
	assert.Contains(t, recorder.Body.String(), "task_public")
}

func TestParseTaskResultMapsStatusesAndURLPriority(t *testing.T) {
	adaptor := &TaskAdaptor{}
	completed, err := adaptor.ParseTaskResult([]byte(`{
		"status":"succeeded",
		"content":{"video_url":"https://example.test/content.mp4"},
		"result_url":"https://example.test/result.mp4"
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), completed.Status)
	assert.Equal(t, "https://example.test/content.mp4", completed.Url)

	failed, err := adaptor.ParseTaskResult([]byte(`{"status":"expired","error":{"message":"result expired"}}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), failed.Status)
	assert.Equal(t, "result expired", failed.Reason)
}
