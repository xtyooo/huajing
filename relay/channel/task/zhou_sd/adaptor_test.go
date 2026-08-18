package zhou_sd

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

func newZhouSDTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newZhouSDRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
}

func TestBuildRequestBodyMapsStandardProtocol(t *testing.T) {
	c, _ := newZhouSDTestContext(t, `{
		"model":"gateway-alias",
		"prompt":"@Image1 follows @Video1",
		"seconds":"15",
		"aspect_ratio":"9:16",
		"resolution":"720p",
		"image_url":"https://example.test/main.png",
		"reference_image_urls":["https://example.test/extra.png"],
		"reference_video":"https://example.test/motion.mp4",
		"audio_urls":["https://example.test/music.mp3"],
		"generate_audio":false
	}`)
	adaptor := &TaskAdaptor{}
	info := newZhouSDRelayInfo()
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "sd2-fast-720p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "sd2-fast-720p", body["model"])
	assert.EqualValues(t, 15, body["duration"])
	assert.Equal(t, "9:16", body["aspect_ratio"])
	assert.Equal(t, "https://example.test/main.png", body["image_url"])
	assert.Equal(t, []any{"https://example.test/extra.png"}, body["extra_images"])
	assert.Equal(t, []any{"https://example.test/motion.mp4"}, body["extra_videos"])
	assert.Equal(t, []any{"https://example.test/music.mp3"}, body["extra_audios"])
	assert.NotContains(t, body, "resolution")
	assert.NotContains(t, body, "generate_audio")
	assert.Equal(t, map[string]float64{"seconds": 15}, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestBodyUsesProviderDefaults(t *testing.T) {
	c, _ := newZhouSDTestContext(t, `{"model":"sd2-720p","prompt":"city night"}`)
	adaptor := &TaskAdaptor{}
	info := newZhouSDRelayInfo()
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "sd2-720p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, 6, body.Duration)
	assert.Equal(t, "16:9", body.AspectRatio)
	assert.Equal(t, map[string]float64{"seconds": 6}, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestBodySupportsRatioAliasAndStartFrame(t *testing.T) {
	c, _ := newZhouSDTestContext(t, `{
		"model":"sd2-720p",
		"prompt":"begin from this frame",
		"duration":"8",
		"ratio":"3:2",
		"image_url":"https://example.test/start.png",
		"video_config":{"reference_mode":"start_frame"}
	}`)
	adaptor := &TaskAdaptor{}
	info := newZhouSDRelayInfo()
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "sd2-720p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, 8, body.Duration)
	assert.Equal(t, "3:2", body.AspectRatio)
	assert.Equal(t, "https://example.test/start.png", body.StartImageURL)
	assert.Empty(t, body.ImageURL)
}

func TestBuildRequestBodyMapsStartEndFrames(t *testing.T) {
	c, _ := newZhouSDTestContext(t, `{
		"model":"sd2-720p",
		"prompt":"day to night",
		"duration":10,
		"reference_image_urls":["http://example.test/start.png","https://example.test/end.png"],
		"video_config":{"reference_mode":"start_end"}
	}`)
	adaptor := &TaskAdaptor{}
	info := newZhouSDRelayInfo()
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "sd2-720p"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "http://example.test/start.png", body.StartImageURL)
	assert.Equal(t, "https://example.test/end.png", body.EndImageURL)
	assert.Empty(t, body.ImageURL)
	assert.Empty(t, body.ExtraImages)
}

func TestValidateRequestRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "zero duration", body: `{"model":"m","prompt":"p","duration":0}`, code: "invalid_duration"},
		{name: "duration too long", body: `{"model":"m","prompt":"p","seconds":"31"}`, code: "invalid_duration"},
		{name: "invalid ratio", body: `{"model":"m","prompt":"p","duration":6,"aspect_ratio":"2:1"}`, code: "invalid_aspect_ratio"},
		{name: "invalid media scheme", body: `{"model":"m","prompt":"p","duration":6,"image_url":"ftp://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "audio without image", body: `{"model":"m","prompt":"p","duration":6,"audio_url":"https://example.test/a.mp3"}`, code: "invalid_reference_count"},
		{name: "start frame count", body: `{"model":"m","prompt":"p","duration":6,"video_config":{"reference_mode":"start_frame"}}`, code: "invalid_reference_count"},
		{name: "start end with video", body: `{"model":"m","prompt":"p","duration":6,"reference_image_urls":["https://example.test/a.png","https://example.test/b.png"],"reference_video":"https://example.test/v.mp4","video_config":{"reference_mode":"start_end"}}`, code: "invalid_reference_mode"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newZhouSDTestContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newZhouSDRelayInfo())
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestValidateRequestRejectsTooManyImages(t *testing.T) {
	images := make([]string, 31)
	for index := range images {
		images[index] = "https://example.test/image.png"
	}
	body, err := common.Marshal(map[string]any{
		"model":                "m",
		"prompt":               "p",
		"duration":             6,
		"reference_image_urls": images,
	})
	require.NoError(t, err)
	c, _ := newZhouSDTestContext(t, string(body))

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newZhouSDRelayInfo())

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_reference_count", taskErr.Code)
}

func TestBuildRequestHeaderUsesBearerAuthentication(t *testing.T) {
	c, _ := newZhouSDTestContext(t, `{}`)
	req := httptest.NewRequest(http.MethodPost, "https://bf.dszyym.com/v1/videos", nil)

	require.NoError(t, (&TaskAdaptor{apiKey: "selected-key"}).BuildRequestHeader(c, req, newZhouSDRelayInfo()))

	assert.Equal(t, "Bearer selected-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestDoResponseReturnsOnlyPublicTaskID(t *testing.T) {
	c, recorder := newZhouSDTestContext(t, `{}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"task_id":"upstream-secret","status":"pending","model":"sd2-720p"}`)),
	}
	info := newZhouSDRelayInfo()
	info.PublicTaskID = "task_public"
	info.OriginModelName = "gateway-model"

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "upstream-secret")
	assert.Contains(t, recorder.Body.String(), "task_public")
}

func TestFetchTaskUsesUpstreamIDAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/videos/upstream-task", req.URL.Path)
		assert.Equal(t, "Bearer selected-key", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"upstream-task","status":"processing","progress":45}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "selected-key", map[string]any{"task_id": "upstream-task"}, "")

	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
}

func TestParseTaskResultMapsProviderContract(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   string
		wantProgress string
		wantURL      string
		wantReason   string
	}{
		{name: "pending", body: `{"status":"pending","progress":0}`, wantStatus: model.TaskStatusQueued, wantProgress: "20%"},
		{name: "processing", body: `{"status":"processing","progress":45}`, wantStatus: model.TaskStatusInProgress, wantProgress: "45%"},
		{name: "processing clamps premature completion", body: `{"status":"processing","progress":100}`, wantStatus: model.TaskStatusInProgress, wantProgress: "99%"},
		{name: "completed", body: `{"status":"completed","progress":100,"video_url":"https://cdn.example.test/result.mp4"}`, wantStatus: model.TaskStatusSuccess, wantProgress: "100%", wantURL: "https://cdn.example.test/result.mp4"},
		{name: "failed", body: `{"status":"failed","error":{"message":"generation rejected"}}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "generation rejected"},
		{name: "completed missing url", body: `{"status":"completed","progress":100}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "completed task is missing a valid video_url"},
		{name: "unknown status", body: `{"status":"paused","progress":50}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "unknown upstream task status: paused"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, result.Status)
			assert.Equal(t, test.wantProgress, result.Progress)
			assert.Equal(t, test.wantURL, result.Url)
			assert.Equal(t, test.wantReason, result.Reason)
		})
	}
}
