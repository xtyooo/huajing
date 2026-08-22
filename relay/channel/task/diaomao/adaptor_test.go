package diaomao

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDiaomaoTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newDiaomaoRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
}

func buildDiaomaoBody(t *testing.T, requestBody string, upstreamModel string) (map[string]any, *TaskAdaptor) {
	t.Helper()
	c, _ := newDiaomaoTestContext(t, requestBody)
	adaptor := &TaskAdaptor{}
	info := newDiaomaoRelayInfo()
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	info.UpstreamModelName = upstreamModel

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	return body, adaptor
}

func TestBuildRequestBodyMapsNativeContractAndPreservesFalse(t *testing.T) {
	body, adaptor := buildDiaomaoBody(t, `{
		"model":"gateway-alias",
		"prompt":"@Image1 follows @Video1 and uses @Audio1",
		"duration":"15",
		"size":"1280x720",
		"image_refs":["https://example.test/person.png"],
		"video_refs":["https://example.test/motion.mp4"],
		"audio_refs":["https://example.test/voice.wav"],
		"compliance_enabled":false,
		"compliance_mode":"grid"
	}`, "sd2-c8")

	assert.Equal(t, "sd2-c8", body["model"])
	assert.Equal(t, "@Image1 follows @Video1 and uses @Audio1", body["prompt"])
	assert.EqualValues(t, 15, body["duration"])
	assert.Equal(t, "1280x720", body["size"])
	assert.NotContains(t, body, "aspect_ratio")
	assert.Equal(t, []any{"https://example.test/person.png"}, body["image_refs"])
	assert.Equal(t, []any{"https://example.test/motion.mp4"}, body["video_refs"])
	assert.Equal(t, []any{"https://example.test/voice.wav"}, body["audio_refs"])
	assert.Equal(t, false, body["compliance_enabled"])
	assert.Equal(t, "grid", body["compliance_mode"])
	assert.Equal(t, map[string]float64{"seconds": 15}, adaptor.EstimateBilling(nil, nil))
}

func TestBuildRequestBodyMapsStandardAliasesAndPrefersDuration(t *testing.T) {
	body, _ := buildDiaomaoBody(t, `{
		"model":"sd2-c8",
		"prompt":"reference media",
		"duration":9,
		"seconds":12,
		"resolution":"720p",
		"ratio":"9:16",
		"image_url":"https://example.test/first.png",
		"reference_images":["https://example.test/second.png"],
		"videos":["https://example.test/reference.mp4"],
		"reference_audios":["https://example.test/reference.mp3"]
	}`, "sd2-c8")

	assert.EqualValues(t, 9, body["duration"])
	assert.Equal(t, "9:16", body["aspect_ratio"])
	assert.NotContains(t, body, "resolution")
	assert.Equal(t, []any{"https://example.test/first.png", "https://example.test/second.png"}, body["image_refs"])
	assert.Equal(t, []any{"https://example.test/reference.mp4"}, body["video_refs"])
	assert.Equal(t, []any{"https://example.test/reference.mp3"}, body["audio_refs"])
}

func TestBuildRequestBodyUsesDocumentedDefaults(t *testing.T) {
	body, adaptor := buildDiaomaoBody(t, `{"model":"sd2-c8","prompt":"city night"}`, "sd2-c8")

	assert.EqualValues(t, 8, body["duration"])
	assert.Equal(t, "16:9", body["aspect_ratio"])
	assert.Equal(t, map[string]float64{"seconds": 8}, adaptor.EstimateBilling(nil, nil))
}

func TestValidateRequestRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "duration below minimum", body: `{"model":"m","prompt":"p","duration":4}`, code: "invalid_duration"},
		{name: "duration above maximum", body: `{"model":"m","prompt":"p","duration":16}`, code: "invalid_duration"},
		{name: "unsupported resolution", body: `{"model":"m","prompt":"p","resolution":"1080p"}`, code: "invalid_resolution"},
		{name: "size and ratio conflict", body: `{"model":"m","prompt":"p","size":"1280x720","aspect_ratio":"16:9"}`, code: "invalid_size"},
		{name: "invalid 720p size ratio", body: `{"model":"m","prompt":"p","size":"9999x720"}`, code: "invalid_size"},
		{name: "invalid aspect ratio", body: `{"model":"m","prompt":"p","aspect_ratio":"2:1"}`, code: "invalid_aspect_ratio"},
		{name: "http reference", body: `{"model":"m","prompt":"p","image_refs":["http://example.test/a.png"]}`, code: "invalid_reference_url"},
		{name: "audio without visual reference", body: `{"model":"m","prompt":"p","audio_refs":["https://example.test/a.mp3"]}`, code: "invalid_reference_count"},
		{name: "invalid compliance mode", body: `{"model":"m","prompt":"p","compliance_mode":"unknown"}`, code: "invalid_compliance_mode"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newDiaomaoTestContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newDiaomaoRelayInfo())
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestValidateRequestRejectsReferenceCountOverflow(t *testing.T) {
	images := make([]string, 10)
	for index := range images {
		images[index] = "https://example.test/image.png"
	}
	requestBody, err := common.Marshal(map[string]any{
		"model":      "sd2-c8",
		"prompt":     "too many images",
		"image_refs": images,
	})
	require.NoError(t, err)
	c, _ := newDiaomaoTestContext(t, string(requestBody))

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newDiaomaoRelayInfo())

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_reference_count", taskErr.Code)
}

func TestBuildRequestURLAcceptsRootOrV1BaseURL(t *testing.T) {
	for _, baseURL := range []string{"https://llm.chre3.com", "https://llm.chre3.com/v1/"} {
		adaptor := &TaskAdaptor{baseURL: strings.TrimRight(baseURL, "/")}
		requestURL, err := adaptor.BuildRequestURL(nil)
		require.NoError(t, err)
		assert.Equal(t, "https://llm.chre3.com/v1/videos", requestURL)
	}
}

func TestBuildRequestHeaderUsesBearerAuthentication(t *testing.T) {
	c, _ := newDiaomaoTestContext(t, `{}`)
	req := httptest.NewRequest(http.MethodPost, "https://llm.chre3.com/v1/videos", nil)

	require.NoError(t, (&TaskAdaptor{apiKey: "selected-key"}).BuildRequestHeader(c, req, newDiaomaoRelayInfo()))

	assert.Equal(t, "Bearer selected-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestDoResponseKeepsUpstreamTaskIDPrivate(t *testing.T) {
	for _, responseBody := range []string{
		`{"id":"upstream-secret","status":"processing"}`,
		`{"task_id":"upstream-secret","status":"processing"}`,
	} {
		c, recorder := newDiaomaoTestContext(t, `{}`)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
		}
		info := newDiaomaoRelayInfo()
		info.PublicTaskID = "task_public"
		info.OriginModelName = "gateway-model"

		upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

		require.Nil(t, taskErr)
		assert.Equal(t, "upstream-secret", upstreamID)
		assert.NotContains(t, recorder.Body.String(), "upstream-secret")
		assert.Contains(t, recorder.Body.String(), "task_public")
	}
}

func TestFetchTaskUsesGETUpstreamIDAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "/v1/videos/upstream-task", req.URL.Path)
		assert.Equal(t, "Bearer selected-key", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"upstream-task","status":"processing","progress":45}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL+"/v1", "selected-key", map[string]any{"task_id": "upstream-task"}, "")

	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
}

func TestFetchTaskTreatsNonSuccessStatusAsRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"busy"}}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "selected-key", map[string]any{"task_id": "upstream-task"}, "")

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "429")
}

func TestParseTaskResultMapsStatusesAndResultURLPriority(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   string
		wantProgress string
		wantURL      string
		wantReason   string
	}{
		{name: "queued", body: `{"status":"pending","progress":0}`, wantStatus: model.TaskStatusQueued, wantProgress: "20%"},
		{name: "processing", body: `{"status":"processing","progress":45}`, wantStatus: model.TaskStatusInProgress, wantProgress: "45%"},
		{name: "processing clamps premature completion", body: `{"status":"processing","progress":100}`, wantStatus: model.TaskStatusInProgress, wantProgress: "99%"},
		{name: "video url wins", body: `{"status":"completed","progress":100,"video_url":"https://cdn.example.test/preferred.mp4","url":"https://cdn.example.test/fallback.mp4"}`, wantStatus: model.TaskStatusSuccess, wantProgress: "100%", wantURL: "https://cdn.example.test/preferred.mp4"},
		{name: "url fallback", body: `{"status":"completed","url":"https://cdn.example.test/fallback.mp4"}`, wantStatus: model.TaskStatusSuccess, wantProgress: "100%", wantURL: "https://cdn.example.test/fallback.mp4"},
		{name: "completed missing url", body: `{"status":"completed","progress":100}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "completed task is missing a valid video URL"},
		{name: "failed", body: `{"status":"failed","error":{"message":"generation rejected"}}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "generation rejected"},
		{name: "unknown status", body: `{"status":"paused"}`, wantStatus: model.TaskStatusFailure, wantProgress: "100%", wantReason: "unknown upstream task status: paused"},
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
