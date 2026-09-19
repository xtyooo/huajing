package wanchen

import (
	"bytes"
	"io"
	"mime/multipart"
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

func requestContext(t *testing.T, payload string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{OriginModelName: "public-hn", ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.UpstreamModelName, info.PublicTaskID = ModelList[0], "task_public"
	return c, w, info
}

func TestStandardMediaMappingAndPerRequestBilling(t *testing.T) {
	c, _, info := requestContext(t, `{"model":"public-hn","prompt":"@image1 @video1 @audio1","duration":20,"ratio":"21:9","resolution":"720p","image_urls":"https://example.com/i.png?sig=a%2Fb","video_urls":["https://example.com/v.mp4"],"audio_urls":"https://example.com/a.mp3","generate_audio":false,"watermark":false}`)
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"SD2.5-满血-HN-720P","prompt":"@image1 @video1 @audio1","seconds":"20","aspect_ratio":"21:9","images":["https://example.com/i.png?sig=a%2Fb"],"videos":["https://example.com/v.mp4"],"audios":["https://example.com/a.mp3"]}`, string(body))
	assert.Nil(t, a.EstimateBilling(c, info), "fixed per-request price must not gain seconds/video multipliers")
	assert.Nil(t, a.AdjustBillingOnSubmit(info, nil))
	assert.Zero(t, a.AdjustBillingOnComplete(nil, nil))
}

func TestDefaultsAndAdministratorMappedModel(t *testing.T) {
	c, _, info := requestContext(t, `{"model":"public-hn","prompt":"scene"}`)
	info.UpstreamModelName = "administrator-hn-alias"
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"administrator-hn-alias","prompt":"scene","seconds":"5","aspect_ratio":"16:9"}`, string(body))
}

func TestPixelSizeAndExplicitRatio(t *testing.T) {
	for _, tc := range []struct{ size, ratio, expected string }{
		{"1280x720", "", "16:9"}, {"720x1280", "", "9:16"}, {"1024x1024", "", "1:1"}, {"720x1280", "4:3", "4:3"},
	} {
		c, _, info := requestContext(t, `{"model":"public-hn","prompt":"scene","size":"`+tc.size+`","ratio":"`+tc.ratio+`"}`)
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, common.DecodeJson(reader, &body))
		assert.Equal(t, tc.expected, body["aspect_ratio"])
		assert.NotContains(t, body, "size")
		assert.NotContains(t, body, "resolution")
	}
}

func TestMultipartURLFields(t *testing.T) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range map[string]string{"model": "public-hn", "prompt": "scene", "seconds": "10", "image_urls": "https://example.com/image.png", "video_url": "https://example.com/video.mp4", "audio_urls": "https://example.com/audio.mp3"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	require.NoError(t, writer.WriteField("image_urls", "https://example.com/image2.png"))
	require.NoError(t, writer.Close())
	c, _, info := requestContext(t, "")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &buffer)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.DecodeJson(reader, &body))
	assert.Equal(t, "10", body["seconds"])
	assert.Equal(t, []any{"https://example.com/image.png", "https://example.com/image2.png"}, body["images"])
	assert.Equal(t, []any{"https://example.com/video.mp4"}, body["videos"])
	assert.Equal(t, []any{"https://example.com/audio.mp3"}, body["audios"])
}

func TestSubmitFailuresAndTaskIDAlias(t *testing.T) {
	for _, tc := range []struct {
		payload string
		success bool
	}{
		{`{"task_id":"private-id","created_at":"1789754400000"}`, true},
		{`{"id":"private-id","status":"failed","error":{"message":"upstream rejected"}}`, false},
		{`{"message":"upstream rejected"}`, false},
		{`<html>secret provider response</html>`, false},
	} {
		c, w, info := requestContext(t, `{"model":"public-hn","prompt":"scene"}`)
		id, _, taskErr := (&TaskAdaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.payload))}, info)
		if tc.success {
			require.Nil(t, taskErr)
			assert.Equal(t, "private-id", id)
			assert.NotContains(t, w.Body.String(), id)
		} else {
			require.NotNil(t, taskErr)
			assert.NotContains(t, taskErr.Message, "secret provider response")
		}
	}
}

func TestReferenceAliasesAcceptScalarOrArray(t *testing.T) {
	groups := map[string][]string{
		"images": {"images", "image_urls", "reference_image_urls", "reference_images", "ref_images", "references", "reference_urls"},
		"videos": {"videos", "video_urls", "reference_videos"},
		"audios": {"audios", "audio_urls", "reference_audios", "ref_audios"},
	}
	for upstream, fields := range groups {
		for _, field := range fields {
			for _, array := range []bool{false, true} {
				t.Run(field+map[bool]string{true: "/array", false: "/scalar"}[array], func(t *testing.T) {
					var media any = "https://example.com/media"
					if array {
						media = []string{"https://example.com/media"}
					}
					payload := map[string]any{"model": "public-hn", "prompt": "scene", field: media}
					if upstream == "audios" {
						payload["image"] = "https://example.com/image.png"
					}
					data, err := common.Marshal(payload)
					require.NoError(t, err)
					c, _, info := requestContext(t, string(data))
					a := &TaskAdaptor{}
					require.Nil(t, a.ValidateRequestAndSetAction(c, info))
					reader, err := a.BuildRequestBody(c, info)
					require.NoError(t, err)
					var result map[string]any
					require.NoError(t, common.DecodeJson(reader, &result))
					assert.Equal(t, []any{"https://example.com/media"}, result[upstream])
				})
			}
		}
	}
}

func TestRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ name, extra, code string }{
		{"duration", `"duration":6`, "invalid_duration"},
		{"zero", `"seconds":0`, "invalid_duration"},
		{"conflict", `"duration":5,"seconds":"10"`, "invalid_duration"},
		{"resolution", `"resolution":"1080P"`, "invalid_resolution"},
		{"size", `"size":"480P"`, "invalid_resolution"},
		{"ratio", `"ratio":"2:1"`, "invalid_aspect_ratio"},
		{"ratio-conflict", `"ratio":"16:9","aspect_ratio":"9:16"`, "invalid_aspect_ratio"},
		{"http", `"images":"http://example.com/a.png"`, "invalid_reference_url"},
		{"private", `"images":"https://127.0.0.1/a.png"`, "invalid_reference_url"},
		{"local", `"images":"https://localhost./a.png"`, "invalid_reference_url"},
		{"data", `"images":"data:image/png;base64,YQ=="`, "invalid_reference_url"},
		{"audio-only", `"audios":"https://example.com/a.mp3"`, "invalid_reference_count"},
		{"frames", `"reference_mode":"start_end"`, "unsupported_reference_mode"},
		{"first-frame", `"first_frame":"https://example.com/frame.png"`, "unsupported_reference_mode"},
		{"last-frame", `"last_frame":"https://example.com/frame.png"`, "unsupported_reference_mode"},
		{"first-frame-url", `"first_frame_url":"https://example.com/frame.png"`, "unsupported_reference_mode"},
		{"last-frame-url", `"last_frame_url":"https://example.com/frame.png"`, "unsupported_reference_mode"},
		{"bad-array", `"images":[5]`, "invalid_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := requestContext(t, `{"model":"public-hn","prompt":"scene",`+tc.extra+`}`)
			err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, tc.code, err.Code)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
		})
	}
}

func TestReferenceCountsAndAllowedDurations(t *testing.T) {
	for _, tc := range []struct {
		field string
		max   int
	}{{"images", 30}, {"videos", 15}, {"audios", 15}} {
		for _, count := range []int{tc.max, tc.max + 1} {
			values := make([]string, count)
			for i := range values {
				values[i] = "https://example.com/reference"
			}
			payload := map[string]any{"model": "public-hn", "prompt": "scene", tc.field: values}
			if tc.field == "audios" {
				payload["image"] = "https://example.com/image"
			}
			data, err := common.Marshal(payload)
			require.NoError(t, err)
			c, _, info := requestContext(t, string(data))
			validationErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			if count == tc.max {
				require.Nil(t, validationErr)
			} else {
				require.NotNil(t, validationErr)
				assert.Equal(t, "invalid_reference_count", validationErr.Code)
			}
		}
	}
	for _, seconds := range []string{"5", "10", "20", "30"} {
		c, _, info := requestContext(t, `{"model":"public-hn","prompt":"scene","seconds":"`+seconds+`"}`)
		require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	}
}

func TestSubmitHTTPAndPublicTaskID(t *testing.T) {
	for _, status := range []int{200, 201, 202} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/videos", r.URL.Path)
			assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"id":"private-id","status":"queued","created_at":"2026-09-19T10:00:00Z"}`)
		}))
		c, w, info := requestContext(t, `{"model":"public-hn","prompt":"scene"}`)
		info.ChannelBaseUrl, info.ApiKey = server.URL+"/v1/", "channel-key"
		a := &TaskAdaptor{}
		a.Init(info)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		body, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		resp, err := a.DoRequest(c, info, body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		id, _, taskErr := a.DoResponse(c, resp, info)
		require.Nil(t, taskErr)
		assert.Equal(t, "private-id", id)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "task_public")
		assert.NotContains(t, w.Body.String(), "private-id")
		server.Close()
	}
}

func TestFetchTaskUsesPrivateIDAndAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/private-id", r.URL.Path)
		assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `{"task_id":"private-id","status":"processing"}`)
	}))
	defer server.Close()
	for _, base := range []string{server.URL, server.URL + "/v1/"} {
		resp, err := (&TaskAdaptor{}).FetchTask(base, "channel-key", map[string]any{"task_id": "private-id"}, "")
		require.NoError(t, err)
		resp.Body.Close()
	}
}

func TestStatusAliasesAndContentFallback(t *testing.T) {
	for _, tc := range []struct {
		status   string
		expected model.TaskStatus
	}{
		{"queued", model.TaskStatusQueued}, {"pending", model.TaskStatusQueued}, {"submitted", model.TaskStatusQueued}, {"init", model.TaskStatusQueued}, {"waiting", model.TaskStatusQueued},
		{"processing", model.TaskStatusInProgress}, {"in_progress", model.TaskStatusInProgress}, {"running", model.TaskStatusInProgress}, {"unknown", model.TaskStatusInProgress},
		{"completed", model.TaskStatusSuccess}, {"success", model.TaskStatusSuccess}, {"succeeded", model.TaskStatusSuccess}, {"done", model.TaskStatusSuccess},
		{"failed", model.TaskStatusFailure}, {"failure", model.TaskStatusFailure}, {"error", model.TaskStatusFailure}, {"cancelled", model.TaskStatusFailure}, {"canceled", model.TaskStatusFailure}, {"rejected", model.TaskStatusFailure},
	} {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"` + tc.status + `","created_at":"2026-09-19T10:00:00Z","completed_at":1789754400000}`))
		require.NoError(t, err)
		assert.Equal(t, string(tc.expected), result.Status, tc.status)
		assert.Empty(t, result.Url, "missing URL must remain empty for authenticated service content fallback")
	}
	for _, field := range []string{"video_url", "url", "result_url", "download_url"} {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"completed","` + field + `":"https://example.com/result.mp4?sig=a%2Fb"}`))
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/result.mp4?sig=a%2Fb", result.Url)
	}
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"completed","url":"https://example.com/second.mp4","video_url":"https://example.com/first.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/first.mp4", result.Url)
}

func TestPublicResponseDoesNotExposeUpstreamFields(t *testing.T) {
	task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: "https://huajing.example/media/result.mp4"}
	task.PrivateData.UpstreamTaskID = "private-id"
	task.PrivateData.ResultURL = "https://upstream.example/private.mp4"
	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(data), "task_public")
	assert.NotContains(t, string(data), "private-id")
	assert.NotContains(t, string(data), "upstream.example")
}
