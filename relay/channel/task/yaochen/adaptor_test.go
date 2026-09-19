package yaochen

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
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

func requestContext(t *testing.T, payload any) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	data, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(data))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{OriginModelName: "public-video", ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.UpstreamModelName = "admin-published-model"
	info.PublicTaskID = "task_public"
	return c, recorder, info
}

func TestStandardReferencesPreserveOrderDuplicatesAndMapping(t *testing.T) {
	c, _, info := requestContext(t, map[string]any{
		"model": "public-video", "prompt": "图一和图二", "duration": "30", "aspect_ratio": "21:9", "resolution": "720p",
		"images":               []string{"https://example.test/1.png?signature=abc", "https://example.test/1.png?signature=abc"},
		"reference_image_urls": "https://example.test/3.png",
	})
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	assert.Equal(t, map[string]any{
		"model": "admin-published-model", "prompt": "图一和图二", "ratio": "21:9", "seconds": float64(30),
		"images": []any{"https://example.test/1.png?signature=abc", "https://example.test/1.png?signature=abc", "https://example.test/3.png"},
	}, body)
	assert.Nil(t, a.EstimateBilling(c, info), "fixed per-request pricing must not multiply by seconds")
}

func TestSingleImageAliasesAndDefaults(t *testing.T) {
	for _, field := range []string{"image", "image_url", "input_reference", "images", "image_urls", "reference_image_urls", "ref_images", "reference_images", "references", "reference_urls", "image_refs"} {
		t.Run(field, func(t *testing.T) {
			c, _, info := requestContext(t, map[string]any{"model": "public-video", "prompt": "p", field: "https://example.test/i.png"})
			a := &TaskAdaptor{}
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			assert.Equal(t, []string{"https://example.test/i.png"}, a.body.Images)
			assert.Equal(t, 30, a.body.Seconds)
			assert.Equal(t, "16:9", a.body.Ratio)
		})
	}
}

func TestInvalidRequestsDoNotReachUpstream(t *testing.T) {
	tests := []struct {
		field string
		value any
		code  string
	}{
		{"model", "", "missing_model"}, {"prompt", " ", "invalid_prompt"}, {"prompt", strings.Repeat("中", 12001), "invalid_prompt"},
		{"duration", 5, "invalid_duration"}, {"seconds", "0", "invalid_duration"}, {"seconds", "30.1", "invalid_json"},
		{"ratio", "2:1", "invalid_aspect_ratio"}, {"reference_mode", "start_end", "unsupported_reference_mode"},
		{"images", []string{"http://example.test/i.png"}, "invalid_reference_url"},
		{"images", []string{"https://127.0.0.1/i.png"}, "invalid_reference_url"},
		{"images", []string{"data:image/png;base64,bm90YW5pbWFnZQ=="}, "invalid_reference_image"},
		{"images", []string{"https://e.test/1", "https://e.test/2", "https://e.test/3", "https://e.test/4", "https://e.test/5", "https://e.test/6", "https://e.test/7", "https://e.test/8", "https://e.test/9", "https://e.test/10"}, "invalid_reference_count"},
	}
	for _, test := range tests {
		t.Run(test.field+"/"+test.code, func(t *testing.T) {
			body := map[string]any{"model": "public-video", "prompt": "p"}
			body[test.field] = test.value
			c, _, info := requestContext(t, body)
			a := &TaskAdaptor{}
			err := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, test.code, err.Code)
			assert.Nil(t, a.body)
		})
	}
	for _, field := range []string{"videos", "video_urls", "reference_video", "reference_video_urls", "audio_urls", "reference_audio_urls", "input_audio", "audios", "medias", "first_frame", "referenceVideos", "referenceAudios", "video_refs", "audio_refs"} {
		t.Run(field, func(t *testing.T) {
			c, _, info := requestContext(t, map[string]any{"model": "m", "prompt": "p", field: "https://example.test/media"})
			err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, "unsupported_reference_media", err.Code)
		})
	}
}

func TestPixelSizeRatioCompatibility(t *testing.T) {
	for size, ratio := range map[string]string{"1024x1024": "1:1", "1792x1024": "16:9", "1024x1792": "9:16", "1920x1080": "16:9", "1080x1920": "9:16", "1024x768": "4:3", "768x1024": "3:4", "2520x1080": "21:9", "512X512": "1:1"} {
		t.Run(size, func(t *testing.T) {
			c, _, info := requestContext(t, map[string]any{"model": "m", "prompt": "p", "size": size})
			a := &TaskAdaptor{}
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			assert.Equal(t, ratio, a.body.Ratio)
		})
	}
}

func TestInlineImagesDecodeAndEnforceDimensions(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	value := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	c, _, info := requestContext(t, map[string]any{"model": "m", "prompt": "p", "images": value})
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, []string{value}, a.body.Images)
	_, err := validateInlineImage(strings.Replace(value, "image/png", "image/jpeg", 1))
	require.Error(t, err)
	_, err = validateInlineImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()[:33]))
	require.Error(t, err, "valid header is not sufficient; full image must decode")
	encoded.Reset()
	require.NoError(t, png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 8193, 1))))
	_, err = validateInlineImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()))
	require.ErrorContains(t, err, "8192")
}

func TestCreateStatusHeadersAndPublicTaskIsolation(t *testing.T) {
	for _, status := range []int{200, 201, 202} {
		for _, idempotency := range []string{"", "client-idempotency"} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/videos", r.URL.Path)
				assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				assert.Equal(t, firstNonEmpty(idempotency, "task_public"), r.Header.Get("Idempotency-Key"))
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"task_id":"private_upstream","status":"queued","created_at":"2026-09-15T08:00:00+00:00"}`)
			}))
			c, recorder, info := requestContext(t, map[string]any{"model": "m", "prompt": "p"})
			c.Request.Header.Set("Idempotency-Key", idempotency)
			a := &TaskAdaptor{apiKey: "secret", baseURL: server.URL + "/v1"}
			resp, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode)
			id, _, taskErr := a.DoResponse(c, resp, info)
			require.Nil(t, taskErr)
			assert.Equal(t, "private_upstream", id)
			assert.Equal(t, 200, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "task_public")
			assert.NotContains(t, recorder.Body.String(), "private_upstream")
			server.Close()
		}
	}
}

func TestPollingStatusesAndContentFallback(t *testing.T) {
	tests := []struct{ body, status, progress, url, reason string }{
		{`{"status":"queued","progress":0}`, string(model.TaskStatusQueued), "20%", "", ""},
		{`{"status":"submitting","progress":5}`, string(model.TaskStatusInProgress), "5%", "", ""},
		{`{"status":"unknown","progress":100}`, string(model.TaskStatusInProgress), "99%", "", ""},
		{`{"status":"in_progress","progress":"50%"}`, string(model.TaskStatusInProgress), "50%", "", ""},
		{`{"status":"completed","result":{"url":"https://example.test/result.mp4?sig=x"}}`, string(model.TaskStatusSuccess), "100%", "https://example.test/result.mp4?sig=x", ""},
		{`{"status":"completed","result":null}`, string(model.TaskStatusSuccess), "100%", "", ""},
		{`{"status":"failed","error":"provider rejected"}`, string(model.TaskStatusFailure), "100%", "", "provider rejected"},
		{`{"status":"failed","error":{"message":"rejected"}}`, string(model.TaskStatusFailure), "100%", "", "rejected"},
	}
	for _, test := range tests {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(test.body))
		require.NoError(t, err)
		assert.Equal(t, test.status, result.Status)
		assert.Equal(t, test.progress, result.Progress)
		assert.Equal(t, test.url, result.Url)
		assert.Equal(t, test.reason, result.Reason)
	}
}

func TestFetchUsesPrivateTaskAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/videos/private_task", r.URL.Path)
		assert.Equal(t, "Bearer channel-secret", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"task_id":"private_task","status":"unknown"}`)
	}))
	defer server.Close()
	for _, base := range []string{server.URL, server.URL + "/v1/"} {
		resp, err := (&TaskAdaptor{}).FetchTask(base, "channel-secret", map[string]any{"task_id": "private_task"}, "")
		require.NoError(t, err)
		resp.Body.Close()
	}
}

func TestPublicConversionUsesOnlyCachedURL(t *testing.T) {
	task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, Progress: "100%", MediaStatus: model.MediaStatusPending, PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.test/private.mp4"}}
	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "upstream.test")
	task.MediaStatus = model.MediaStatusSuccess
	task.MediaURL = "https://huajingapi.top/media/public.mp4"
	data, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(data), task.MediaURL)
}
