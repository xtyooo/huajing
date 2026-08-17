package anhe

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
		"duration":15,
		"seconds":5,
		"ratio":"16:9",
		"aspect_ratio":"9:16",
		"resolution":"720p",
		"image_url":"https://example.test/main.png",
		"reference_image_urls":["https://example.test/ref.png"],
		"image_urls":["https://example.test/image-urls.png"],
		"reference_images":["https://example.test/reference-images.png"],
		"references":["https://example.test/references.png"],
		"reference_urls":["https://example.test/reference-urls.png"],
		"images":["https://example.test/images.png"],
		"reference_video":"https://example.test/motion.mp4",
		"reference_videos":["https://example.test/reference-video.mp4"],
		"videos":["https://example.test/videos.mp4"],
		"video_urls":["https://example.test/video-urls.mp4"],
		"audio_url":"https://example.test/audio.mp3",
		"reference_audios":["https://example.test/reference-audio.mp3"],
		"audios":["https://example.test/audios.mp3"],
		"audio_urls":["https://example.test/audio-urls.mp3"],
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

	expected := map[string]any{
		"model":          "seedance-2-5-720p",
		"prompt":         "cinematic scene",
		"duration":       float64(15),
		"resolution":     "720P",
		"ratio":          "16:9",
		"generate_audio": false,
		"images": []any{
			"https://example.test/images.png",
		},
		"reference_videos": []any{
			"https://example.test/reference-video.mp4",
		},
		"reference_audios": []any{
			"https://example.test/reference-audio.mp3",
		},
	}
	assert.Equal(t, expected, body, "canonical fields must take precedence over compatibility aliases")
	assert.JSONEq(t, `{
		"model":"seedance-2-5-720p",
		"prompt":"cinematic scene",
		"duration":15,
		"resolution":"720P",
		"ratio":"16:9",
		"images":["https://example.test/images.png"],
		"reference_videos":["https://example.test/reference-video.mp4"],
		"reference_audios":["https://example.test/reference-audio.mp3"],
		"generate_audio":false
	}`, string(data))
	assert.Equal(t, map[string]float64{"seconds": 15, "video_input": 2}, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestBodyNormalizesAliasesWithoutOverridingRatio(t *testing.T) {
	c, _ := newAnheTestContext(t, `{
		"model":"seedance-2-5-480p",
		"prompt":"transition",
		"seconds":"8",
		"size":"1280x720",
		"resolution":"4k",
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

	require.NotNil(t, body.Duration)
	assert.Equal(t, 8, *body.Duration)
	assert.Equal(t, "16:9", body.Ratio)
	assert.Equal(t, "4K", body.Resolution)
	assert.Equal(t, []string{
		"https://example.test/start.png",
		"https://example.test/end.png",
	}, body.Images)
	assert.Equal(t, map[string]float64{"seconds": 8}, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestBodyOmitsOptionalDurationAndResolution(t *testing.T) {
	c, _ := newAnheTestContext(t, `{"model":"gateway-alias","prompt":"cinematic scene"}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "xh-sd2.0-720p-933"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, map[string]any{
		"model":  "xh-sd2.0-720p-933",
		"prompt": "cinematic scene",
		"ratio":  "16:9",
	}, body)
	assert.Nil(t, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestBodyKeepsLegacyMediaAliasesCompatible(t *testing.T) {
	c, _ := newAnheTestContext(t, `{
		"model":"gateway-alias",
		"prompt":"legacy client",
		"image_url":"https://example.test/main.png",
		"reference_image_urls":["https://example.test/ref.png"],
		"reference_video":"https://example.test/video.mp4",
		"audio_url":"https://example.test/audio.mp3"
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = "xh-sd2.0-720p-933"

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, []string{"https://example.test/main.png", "https://example.test/ref.png"}, body.Images)
	assert.Equal(t, []string{"https://example.test/video.mp4"}, body.ReferenceVideos)
	assert.Equal(t, []string{"https://example.test/audio.mp3"}, body.ReferenceAudios)
}

func TestBuildRequestBodyAcceptsProviderSpecificDurationsWithinBillingBound(t *testing.T) {
	for _, duration := range []int{-1, 1, 3, 31, relaycommon.MaxTaskDurationSeconds} {
		t.Run(fmt.Sprintf("duration_%d", duration), func(t *testing.T) {
			c, _ := newAnheTestContext(t, fmt.Sprintf(`{"model":"m","prompt":"p","duration":%d}`, duration))
			adaptor := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			if duration > 0 {
				assert.Equal(t, map[string]float64{"seconds": float64(duration)}, adaptor.EstimateBilling(c, info))
			} else {
				assert.Nil(t, adaptor.EstimateBilling(c, info))
			}
		})
	}
}

func TestBuildRequestBodyNormalizesCommonPixelSizes(t *testing.T) {
	tests := map[string]string{
		"1792x1024": "16:9",
		"1024x1792": "9:16",
		"1024x1024": "1:1",
	}
	for size, expected := range tests {
		t.Run(size, func(t *testing.T) {
			c, _ := newAnheTestContext(t, fmt.Sprintf(`{"model":"m","prompt":"p","size":%q}`, size))
			adaptor := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			assert.Equal(t, expected, adaptor.body.Ratio)
		})
	}
}

func TestValidateRequestRejectsInvalidProviderInputs(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "zero duration", body: `{"model":"m","prompt":"p","duration":0}`, code: "invalid_duration"},
		{name: "negative duration", body: `{"model":"m","prompt":"p","duration":-2}`, code: "invalid_duration"},
		{name: "duration over billing bound", body: `{"model":"m","prompt":"p","seconds":"3601"}`, code: "invalid_duration"},
		{name: "invalid aspect ratio", body: `{"model":"m","prompt":"p","duration":4,"aspect_ratio":"2:1"}`, code: "invalid_aspect_ratio"},
		{name: "non https media", body: `{"model":"m","prompt":"p","duration":4,"image_url":"http://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "invalid resolution", body: `{"model":"m","prompt":"p","duration":4,"resolution":"2K"}`, code: "invalid_resolution"},
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
			upstreamReq := httptest.NewRequest(http.MethodPost, "https://anhedean.cn/v1/video/generations", nil)
			adaptor := &TaskAdaptor{apiKey: "secret"}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

			require.NoError(t, adaptor.BuildRequestHeader(c, upstreamReq, info))
			assert.Equal(t, "Bearer secret", upstreamReq.Header.Get("Authorization"))
			assert.Equal(t, test.expectedKey, upstreamReq.Header.Get("Idempotency-Key"))
		})
	}
}

func TestBuildAndFetchTaskUseVideoGenerationsEndpoints(t *testing.T) {
	var method string
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"task/upstream","status":"processing"}`))
	}))
	t.Cleanup(server.Close)

	for _, baseURL := range []string{server.URL, server.URL + "/v1", server.URL + "/v1/"} {
		t.Run(baseURL, func(t *testing.T) {
			adaptor := &TaskAdaptor{baseURL: baseURL}
			submitURL, err := adaptor.BuildRequestURL(nil)
			require.NoError(t, err)
			assert.Equal(t, server.URL+"/v1/video/generations", submitURL)

			resp, err := adaptor.FetchTask(baseURL, "secret", map[string]any{"task_id": "task/upstream"}, "")
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, http.MethodGet, method)
			assert.Equal(t, "/v1/video/generations/task%2Fupstream", path)
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
		"content":{"video_url":"https://example.test/legacy-content.mp4"},
		"result_url":"https://example.test/result.mp4",
		"url":"https://example.test/fallback.mp4"
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), completed.Status)
	assert.Equal(t, "https://example.test/result.mp4", completed.Url)

	failed, err := adaptor.ParseTaskResult([]byte(`{"status":"expired","error":{"message":"result expired"}}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), failed.Status)
	assert.Equal(t, "result expired", failed.Reason)

	missingURL, err := adaptor.ParseTaskResult([]byte(`{"status":"succeeded","progress":100}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusInProgress), missingURL.Status)
	assert.Equal(t, "99%", missingURL.Progress)
}
