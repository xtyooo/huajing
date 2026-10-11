package snumom

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testContext(t *testing.T, payload string, upstream string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-video",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: upstream, ChannelBaseUrl: "https://snumom.example", ApiKey: "secret"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	return c, recorder, info
}

func bodyBytes(t *testing.T, adaptor *TaskAdaptor, c *gin.Context, info *relaycommon.RelayInfo) string {
	t.Helper()
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

func TestStandardModelsUseVideosProtocolAndDoNotUseAllowlist(t *testing.T) {
	c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","seconds":"10","resolution":"720p","aspect_ratio":"16:9","images":"https://example.com/ref.jpg","audio_urls":["https://example.com/a.mp3"],"generate_audio":false}`, "future-video-provider-model")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	endpoint, err := a.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://snumom.example/v1/videos", endpoint)
	assert.JSONEq(t, `{"model":"future-video-provider-model","prompt":"scene","duration":10,"resolution":"720p","aspect_ratio":"16:9","reference_images":["https://example.com/ref.jpg"],"reference_audios":["https://example.com/a.mp3"],"generate_audio":false}`, bodyBytes(t, a, c, info))
}

func TestDoubaoTokenModelsUseMetadataGenerationsProtocol(t *testing.T) {
	c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","duration":8,"size":"720p","ratio":"9:16","images":["https://example.com/ref.jpg"],"videos":"https://example.com/ref.mp4","generate_audio":false}`, "doubao-seedance-2-0-fast-260128")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	endpoint, err := a.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://snumom.example/v1/video/generations", endpoint)
	assert.JSONEq(t, `{"model":"doubao-seedance-2-0-fast-260128","prompt":"scene","duration":8,"metadata":{"content":[{"type":"text","text":"scene"},{"type":"image_url","image_url":{"url":"https://example.com/ref.jpg"},"role":"reference_image"},{"type":"video_url","video_url":{"url":"https://example.com/ref.mp4"},"role":"reference_video"}],"resolution":"720p","ratio":"9:16","generate_audio":false}}`, bodyBytes(t, a, c, info))
}

func TestSubmit202AndPublicIDPrivacy(t *testing.T) {
	c, recorder, info := testContext(t, `{"model":"public-video","prompt":"scene"}`, "future-model")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	resp := &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(`{"id":"private-upstream","status":"queued"}`))}
	upstreamID, _, err := a.DoResponse(c, resp, info)
	require.Nil(t, err)
	assert.Equal(t, "private-upstream", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "private-upstream")
	assert.Contains(t, recorder.Body.String(), "task_public")
}

func TestResultURLPriorityAndStatuses(t *testing.T) {
	a := &TaskAdaptor{}
	completed := `{"status":"completed","data":{"data":{"status":"completed","content":{"video_url":"https://cdn.example/current.mp4"}}},"result_url":"https://cdn.example/stale.mp4"}`
	result, err := a.ParseTaskResult([]byte(completed))
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example/current.mp4", result.Url)
	assert.Equal(t, "SUCCESS", string(result.Status))
	for _, body := range []string{`{"status":"queued"}`, `{"status":"in_progress","progress":30}`, `{"status":"failed","message":"bad request"}`} {
		result, err = a.ParseTaskResult([]byte(body))
		require.NoError(t, err)
		assert.NotEmpty(t, result.Status)
	}
}

func TestFetchTaskUsesEndpointByMappedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		if r.URL.Path == "/v1/videos/private-id" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		assert.Equal(t, "/v1/video/generations/private-id", r.URL.Path)
		_, _ = io.WriteString(w, `{"status":"completed","url":"https://cdn.example/video.mp4"}`)
	}))
	defer server.Close()
	a := &TaskAdaptor{}
	resp, err := a.FetchTask(server.URL, "secret", map[string]any{"task_id": "private-id", "model": "doubao-seedance-new"}, "")
	require.NoError(t, err)
	resp.Body.Close()
}
