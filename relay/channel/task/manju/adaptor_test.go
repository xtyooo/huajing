package manju

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newManjuTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newManjuRelayInfo(modelName string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	info.UpstreamModelName = modelName
	return info
}

func TestBuildRequestBodyMapsSixModeSpecificModels(t *testing.T) {
	tests := []struct {
		model           string
		body            string
		wantMedia       []mediaItem
		wantResolution  string
		wantResolutionR float64
		wantFalse       bool
	}{
		{
			model:           "wan3.0-t2v",
			body:            `{"model":"wan3.0-t2v","prompt":"city","seconds":"5","aspect_ratio":"16:9","resolution":"480p"}`,
			wantResolution:  "480P",
			wantResolutionR: 1,
		},
		{
			model:           "wan3.0-i2v",
			body:            `{"model":"wan3.0-i2v","prompt":"move","duration":5,"ratio":"9:16","resolution":"720P","image_url":"https://example.test/first.png","video_config":{"reference_mode":"start_frame"}}`,
			wantMedia:       []mediaItem{{Type: "first_frame", URL: "https://example.test/first.png"}},
			wantResolution:  "720P",
			wantResolutionR: 2,
		},
		{
			model: "wan3.0-r2v",
			body:  `{"model":"wan3.0-r2v","prompt":"references","duration":"5","aspect_ratio":"4:3","resolution":"1080p","reference_image_urls":["https://example.test/a.png"],"reference_video":"https://example.test/v.mp4","audio_url":"https://example.test/a.mp3","prompt_extend":false}`,
			wantMedia: []mediaItem{
				{Type: "reference_image", URL: "https://example.test/a.png"},
				{Type: "reference_video", URL: "https://example.test/v.mp4"},
				{Type: "audio", URL: "https://example.test/a.mp3"},
			},
			wantResolution:  "1080P",
			wantResolutionR: 3.2,
			wantFalse:       true,
		},
		{
			model:           "wan3.0-prime-t2v",
			body:            `{"model":"wan3.0-prime-t2v","prompt":"city","duration":5,"aspect_ratio":"1:1","resolution":"480P"}`,
			wantResolution:  "480P",
			wantResolutionR: 1,
		},
		{
			model:           "wan3.0-prime-i2v",
			body:            `{"model":"wan3.0-prime-i2v","prompt":"move","seconds":5,"aspect_ratio":"3:4","resolution":"720p","reference_image_urls":["https://example.test/first.png"]}`,
			wantMedia:       []mediaItem{{Type: "first_frame", URL: "https://example.test/first.png"}},
			wantResolution:  "720P",
			wantResolutionR: 16.0 / 15.0,
		},
		{
			model: "wan3.0-prime-r2v",
			body:  `{"model":"wan3.0-prime-r2v","prompt":"references","duration":5,"aspect_ratio":"21:9","resolution":"1080P","images":["https://example.test/a.png"],"reference_videos":["https://example.test/v.mp4"],"audio_urls":["https://example.test/a.mp3"],"prompt_extend":false}`,
			wantMedia: []mediaItem{
				{Type: "reference_image", URL: "https://example.test/a.png"},
				{Type: "reference_video", URL: "https://example.test/v.mp4"},
				{Type: "audio", URL: "https://example.test/a.mp3"},
			},
			wantResolution:  "1080P",
			wantResolutionR: 4.0 / 3.0,
			wantFalse:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			c, _ := newManjuTestContext(t, test.body)
			adaptor := &TaskAdaptor{}
			info := newManjuRelayInfo(test.model)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

			reader, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var body upstreamRequest
			require.NoError(t, common.Unmarshal(data, &body))

			assert.Equal(t, test.model, body.Model)
			assert.Equal(t, 5, body.Duration)
			assert.Equal(t, test.wantResolution, body.Resolution)
			assert.Equal(t, test.wantMedia, body.Media)
			if test.wantFalse {
				require.NotNil(t, body.PromptExtend)
				assert.False(t, *body.PromptExtend)
			} else {
				assert.Nil(t, body.PromptExtend)
			}
			assert.Equal(t, map[string]float64{
				"seconds":    5,
				"resolution": test.wantResolutionR,
			}, adaptor.EstimateBilling(c, info))
		})
	}
}

func TestMappedModelControlsModeValidationAndUpstreamBody(t *testing.T) {
	c, _ := newManjuTestContext(t, `{"model":"public-alias","prompt":"move","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"https://example.test/first.png"}`)
	adaptor := &TaskAdaptor{}
	info := newManjuRelayInfo("wan3.0-prime-i2v")

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))
	assert.Equal(t, "wan3.0-prime-i2v", body.Model)
	assert.Equal(t, []mediaItem{{Type: "first_frame", URL: "https://example.test/first.png"}}, body.Media)
}

func TestValidateRequestRejectsModeAndProviderViolations(t *testing.T) {
	tests := []struct {
		name  string
		model string
		body  string
		code  string
	}{
		{name: "unsupported mapped model", model: "wan3.0-video", body: `{"model":"alias","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p"}`, code: "unsupported_model"},
		{name: "missing duration", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_duration"},
		{name: "duration conflict", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"seconds":6,"aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_duration"},
		{name: "automatic duration rejected", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":-1,"aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_duration"},
		{name: "duration above maximum", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":31,"aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_duration"},
		{name: "missing ratio", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"resolution":"720p"}`, code: "invalid_aspect_ratio"},
		{name: "invalid ratio", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"aspect_ratio":"2:1","resolution":"720p"}`, code: "invalid_aspect_ratio"},
		{name: "missing resolution", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"aspect_ratio":"16:9"}`, code: "invalid_resolution"},
		{name: "invalid resolution", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"4k"}`, code: "invalid_resolution"},
		{name: "t2v rejects media", model: "wan3.0-t2v", body: `{"model":"wan3.0-t2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"https://example.test/a.png"}`, code: "unsupported_reference_media"},
		{name: "i2v requires image", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_reference_count"},
		{name: "i2v rejects two images", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","reference_image_urls":["https://example.test/a.png","https://example.test/b.png"]}`, code: "invalid_reference_count"},
		{name: "i2v rejects video", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"https://example.test/a.png","reference_video":"https://example.test/v.mp4"}`, code: "invalid_reference_count"},
		{name: "r2v requires media", model: "wan3.0-r2v", body: `{"model":"wan3.0-r2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p"}`, code: "invalid_reference_count"},
		{name: "audio requires image", model: "wan3.0-r2v", body: `{"model":"wan3.0-r2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","audio_url":"https://example.test/a.mp3"}`, code: "invalid_reference_count"},
		{name: "start end rejected", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","reference_image_urls":["https://example.test/a.png","https://example.test/b.png"],"video_config":{"reference_mode":"start_end"}}`, code: "unsupported_reference_mode"},
		{name: "r2v rejects frame mode", model: "wan3.0-r2v", body: `{"model":"wan3.0-r2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"https://example.test/a.png","video_config":{"reference_mode":"start_frame"}}`, code: "invalid_reference_mode"},
		{name: "http media rejected", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"http://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "private address rejected", model: "wan3.0-i2v", body: `{"model":"wan3.0-i2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","image_url":"https://127.0.0.1/a.png"}`, code: "invalid_reference_url"},
		{name: "image limit", model: "wan3.0-r2v", body: `{"model":"wan3.0-r2v","prompt":"p","duration":5,"aspect_ratio":"16:9","resolution":"720p","reference_image_urls":["https://example.test/1.png","https://example.test/2.png","https://example.test/3.png","https://example.test/4.png","https://example.test/5.png","https://example.test/6.png","https://example.test/7.png","https://example.test/8.png","https://example.test/9.png","https://example.test/10.png","https://example.test/11.png"]}`, code: "invalid_reference_count"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newManjuTestContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newManjuRelayInfo(test.model))
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestDoRequestNormalizesProviderSuccessCodes(t *testing.T) {
	for _, statusCode := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		t.Run(fmt.Sprintf("status_%d", statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				assert.Equal(t, "/v1/videos/generations", req.URL.Path)
				assert.Equal(t, "Bearer secret", req.Header.Get("Authorization"))
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`{"id":"upstream"}`))
			}))
			t.Cleanup(server.Close)
			c, _ := newManjuTestContext(t, `{}`)
			adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "secret"}

			resp, err := adaptor.DoRequest(c, newManjuRelayInfo("wan3.0-t2v"), bytes.NewBufferString(`{}`))
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestDoResponseReturnsPublicIDAndAcceptsNestedUpstreamID(t *testing.T) {
	c, recorder := newManjuTestContext(t, `{}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"output":{"task_id":"upstream-secret"}}`)),
	}
	info := newManjuRelayInfo("wan3.0-t2v")
	info.OriginModelName = "public-model"
	info.PublicTaskID = "task_public"

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "upstream-secret")
	assert.Contains(t, recorder.Body.String(), "task_public")
}

func TestDoResponseAcceptsDocumentedFlatUpstreamIDs(t *testing.T) {
	for _, responseBody := range []string{
		`{"id":"upstream-id"}`,
		`{"task_id":"upstream-id"}`,
	} {
		c, recorder := newManjuTestContext(t, `{}`)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(responseBody))}
		info := newManjuRelayInfo("wan3.0-t2v")
		info.OriginModelName = "wan3.0-t2v"
		info.PublicTaskID = "task_public"

		upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

		require.Nil(t, taskErr)
		assert.Equal(t, "upstream-id", upstreamID)
		assert.NotContains(t, recorder.Body.String(), "upstream-id")
	}
}

func TestFetchTaskUsesDocumentedPathAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/v1/videos/tasks/task_upstream", req.URL.Path)
		assert.Equal(t, "Bearer selected-key", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL+"/v1", "selected-key", map[string]any{"task_id": "task_upstream"}, "")

	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
}

func TestParseTaskResultMapsStatusesAndResultPriority(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   model.TaskStatus
		wantProgress string
		wantURL      string
		wantReason   string
	}{
		{name: "queued", body: `{"status":"queued","progress":5}`, wantStatus: model.TaskStatusQueued, wantProgress: "5%"},
		{name: "running clamps completion", body: `{"status":"running","progress":"100%"}`, wantStatus: model.TaskStatusInProgress, wantProgress: "99%"},
		{name: "top video wins", body: `{"status":"succeeded","video_url":"https://cdn.example.test/video.mp4","download_url":"https://cdn.example.test/download.mp4","output":{"video_url":"https://cdn.example.test/output.mp4"}}`, wantStatus: model.TaskStatusSuccess, wantProgress: "100%", wantURL: "https://cdn.example.test/video.mp4"},
		{name: "nested download fallback", body: `{"output":{"task_status":"SUCCEEDED","download_url":"https://cdn.example.test/download.mp4"}}`, wantStatus: model.TaskStatusSuccess, wantProgress: "100%", wantURL: "https://cdn.example.test/download.mp4"},
		{name: "completed missing url", body: `{"status":"completed"}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "completed task is missing a valid video URL"},
		{name: "completed rejects http url", body: `{"status":"completed","video_url":"http://cdn.example.test/video.mp4"}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "completed task is missing a valid video URL"},
		{name: "failed", body: `{"status":"failed","error":{"message":"provider rejected request"}}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "provider rejected request"},
		{name: "failed string error with numeric code", body: `{"status":"failed","code":500,"error":"provider unavailable"}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "provider unavailable"},
		{name: "missing status preserves provider error", body: `{"error":{"message":"task not found"}}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "task not found"},
		{name: "unknown", body: `{"status":"mystery"}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "unknown upstream task status: mystery"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, model.TaskStatus(result.Status))
			assert.Equal(t, test.wantProgress, result.Progress)
			assert.Equal(t, test.wantURL, result.Url)
			assert.Equal(t, test.wantReason, result.Reason)
		})
	}
}

func TestConvertToOpenAIVideoOnlyUsesPublicAndCachedMediaURLs(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_public",
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		MediaStatus: model.MediaStatusPending,
		PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.example.test/private.mp4"},
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var pending relaydto.OpenAIVideo
	require.NoError(t, common.Unmarshal(data, &pending))
	assert.Equal(t, "task_public", pending.ID)
	assert.Equal(t, "task_public", pending.TaskID)
	assert.Empty(t, pending.Metadata)

	task.MediaStatus = model.MediaStatusSuccess
	task.MediaURL = "https://huajingapi.top/media/task_public.mp4"
	data, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var completed relaydto.OpenAIVideo
	require.NoError(t, common.Unmarshal(data, &completed))
	assert.Equal(t, task.MediaURL, completed.Metadata["url"])
}
