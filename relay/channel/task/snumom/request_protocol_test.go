package snumom

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocumentedDreaminaRequests(t *testing.T) {
	for _, name := range []string{"dreamina-seedance-2-0", "dreamina-seedance-2-0-fast", "dreamina-seedance-2-0-mini", "dreamina-seedance-2-5"} {
		t.Run(name, func(t *testing.T) {
			resolution := "1080p"
			if strings.HasSuffix(name, "-fast") || strings.HasSuffix(name, "-mini") {
				resolution = "720p"
			}
			payload := fmt.Sprintf(`{"model":"public-video","prompt":"海边日落，镜头缓慢推进","seconds":8,"aspect_ratio":"16:9","resolution":%q,"reference_images":["https://example.com/a.jpg"],"reference_videos":["https://example.com/a.mp4"],"reference_audios":["https://example.com/a.mp3"]}`, resolution)
			c, _, info := testContext(t, payload, name)
			a := &TaskAdaptor{}
			a.Init(info)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			assert.JSONEq(t, fmt.Sprintf(`{"model":%q,"prompt":"海边日落，镜头缓慢推进","seconds":8,"aspect_ratio":"16:9","resolution":%q,"reference_images":["https://example.com/a.jpg"],"reference_videos":["https://example.com/a.mp4"],"reference_audios":["https://example.com/a.mp3"]}`, name, resolution), bodyBytes(t, a, c, info))
		})
	}
}

func TestDocumentedGrokRequests(t *testing.T) {
	for _, name := range []string{"grok-imagine-video-1.5", "grok-imagine-video-1.5-fast", "grok-imagine-video-1.5-lite"} {
		t.Run(name, func(t *testing.T) {
			c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","seconds":"5","size":"1280x720","aspect_ratio":"16:9","reference_images":[{"url":"https://example.com/a.jpg","role":"first_frame"}]}`, name)
			a := &TaskAdaptor{}
			a.Init(info)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			assert.JSONEq(t, fmt.Sprintf(`{"model":%q,"prompt":"scene","seconds":"5","size":"1280x720","aspect_ratio":"16:9","reference_images":[{"url":"https://example.com/a.jpg","role":"first_frame"}]}`, name), bodyBytes(t, a, c, info))
		})
	}
}

func TestFamilyDefaultsAreOwnedByUpstream(t *testing.T) {
	for _, name := range []string{"dreamina-seedance-2-5", "grok-imagine-video-1.5-lite", "future-video-model"} {
		c, _, info := testContext(t, `{"model":"public-video","prompt":"scene"}`, name)
		a := &TaskAdaptor{}
		a.Init(info)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		assert.JSONEq(t, fmt.Sprintf(`{"model":%q,"prompt":"scene"}`, name), bodyBytes(t, a, c, info))
	}
}

func TestDreaminaReferencesPreserveRolesOrderAndContent(t *testing.T) {
	c, _, info := testContext(t, `{"model":"public-video","input_reference":"https://example.com/input.jpg?sig=a%2Bb&z=2","first_frame":"https://example.com/first.jpg","last_frame_url":"https://example.com/last.jpg","referenceImages":[{"url":"https://example.com/ref.jpg","role":"reference_image"}],"content":[{"type":"text","text":"move naturally"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="},"role":"reference_image"},{"type":"video_url","video_url":{"url":"http://example.com/a.mp4"}},{"type":"audio_url","audio_url":{"url":"https://example.com/a.mp3"}}],"extra":{"seconds":"20","aspectRatio":"9:16"},"metadata":{"provider_option":false,"model":"must-not-override"},"generate_audio":false}`, "dreamina-seedance-2-5")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.JSONEq(t, `{"model":"dreamina-seedance-2-5","prompt":"move naturally","seconds":20,"aspect_ratio":"9:16","reference_images":["https://example.com/input.jpg?sig=a%2Bb&z=2",{"url":"https://example.com/first.jpg","role":"first_frame"},{"url":"https://example.com/ref.jpg","role":"reference_image"},{"url":"data:image/png;base64,aGVsbG8=","role":"reference_image"},{"url":"https://example.com/last.jpg","role":"last_frame"}],"reference_videos":["http://example.com/a.mp4"],"reference_audios":["https://example.com/a.mp3"],"metadata":{"provider_option":false,"model":"must-not-override"},"generate_audio":false}`, bodyBytes(t, a, c, info))
}

func TestGrokStandardImageAliasesReachRecognizedFields(t *testing.T) {
	for _, payload := range []string{
		`{"model":"public-video","prompt":"scene","duration":8,"resolution":"720p","ratio":"9:16","image_url":"https://example.com/a.jpg"}`,
		`{"model":"public-video","prompt":"scene","seconds":8,"size":"720p","aspect_ratio":"9:16","image_urls":"https://example.com/a.jpg"}`,
	} {
		c, _, info := testContext(t, payload, "grok-imagine-video-1.5")
		a := &TaskAdaptor{}
		a.Init(info)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		assert.JSONEq(t, `{"model":"grok-imagine-video-1.5","prompt":"scene","seconds":"8","size":"720p","aspect_ratio":"9:16","reference_images":[{"url":"https://example.com/a.jpg","role":"reference_image"}]}`, bodyBytes(t, a, c, info))
	}
	c, _, info := testContext(t, `{"model":"public-video","content":[{"type":"text","text":"scene"}],"input_reference":"https://example.com/a.jpg","first_frame_url":"https://example.com/b.jpg"}`, "grok-imagine-video-1.5")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.JSONEq(t, `{"model":"grok-imagine-video-1.5","prompt":"scene","reference_images":[{"url":"https://example.com/a.jpg","role":"reference_image"},{"url":"https://example.com/b.jpg","role":"first_frame"}]}`, bodyBytes(t, a, c, info))
}

func TestFamilyProtocolRejectsUnsafeOrUnrepresentableInputs(t *testing.T) {
	for _, test := range []struct{ name, fields, code string }{
		{"dreamina-seedance-2-5", `"duration":0`, "invalid_duration"},
		{"dreamina-seedance-2-5", `"seconds":-2`, "invalid_duration"},
		{"dreamina-seedance-2-5", fmt.Sprintf(`"seconds":%d`, relaycommon.MaxTaskDurationSeconds+1), "invalid_duration"},
		{"dreamina-seedance-2-5", `"duration":5,"seconds":6`, "invalid_duration"},
		{"dreamina-seedance-2-5", `"metadata":{"seconds":999999}`, "invalid_duration"},
		{"dreamina-seedance-2-5", `"metadata":{"parameters":{"duration":"999999"}}`, "invalid_duration"},
		{"dreamina-seedance-2-5", `"extra":{"duration":999999}`, "invalid_duration"},
		{"dreamina-seedance-2-5", `"reference_audios":"data:audio/mp3;base64,aGVsbG8="`, "invalid_reference_url"},
		{"dreamina-seedance-2-5", `"reference_videos":"data:video/mp4;base64,aGVsbG8="`, "invalid_reference_url"},
		{"dreamina-seedance-2-5", `"images":"https://secret@example.com/a.jpg"`, "invalid_reference_url"},
		{"dreamina-seedance-2-5", `"images":"https://127.0.0.1/a.jpg"`, "invalid_reference_url"},
		{"dreamina-seedance-2-5", `"images":"https://[fd00::1]/a.jpg"`, "invalid_reference_url"},
		{"grok-imagine-video-1.5", `"audio_urls":"https://example.com/a.mp3"`, "unsupported_reference_media"},
		{"grok-imagine-video-1.5", `"content":[{"type":"video_url","video_url":{"url":"https://example.com/a.mp4"}}]`, "unsupported_reference_media"},
		{"grok-imagine-video-1.5-lite", `"last_frame":"https://example.com/a.jpg"`, "unsupported_reference_mode"},
		{"grok-imagine-video-1.5-lite", `"reference_images":[{"url":"https://example.com/a.jpg","role":"last_frame"}]`, "unsupported_reference_mode"},
		{"grok-imagine-video-1.5", `"images":"data:image/png;base64,aGVsbG8="`, "invalid_reference_url"},
		{"grok-imagine-video-1.5", `"metadata":{"content":[{"type":"audio_url","audio_url":{"url":"https://example.com/a.mp3"}}]}`, "unsupported_reference_media"},
		{"dreamina-seedance-2-5", `"metadata":{"reference_videos":"data:video/mp4;base64,aGVsbG8="}`, "invalid_reference_url"},
	} {
		t.Run(test.name+test.fields, func(t *testing.T) {
			c, _, info := testContext(t, `{"model":"public-video","prompt":"scene",`+test.fields+`}`, test.name)
			a := &TaskAdaptor{}
			a.Init(info)
			err := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, test.code, err.Code)
		})
	}
}

func TestNoSnapshotCapabilityLimits(t *testing.T) {
	for _, name := range []string{"dreamina-seedance-new", "grok-imagine-video-new", "future-model"} {
		c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","seconds":100,"resolution":"4k"}`, name)
		a := &TaskAdaptor{}
		a.Init(info)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		assert.Nil(t, a.EstimateBilling(c, info), "name must not select billing mode")
	}
}

func TestGrokResultPriorityAndInvalidCompletion(t *testing.T) {
	a := &TaskAdaptor{}
	for _, test := range []struct{ body, want string }{
		{`{"model":"grok-imagine-video-1.5","status":"completed","url":"https://cdn.example/top.mp4?sig=a%2Bb","result_url":"https://cdn.example/result.mp4","metadata":{"url":"https://cdn.example/stale.mp4"},"content":{"video_url":"https://cdn.example/wrong.mp4"}}`, "https://cdn.example/top.mp4?sig=a%2Bb"},
		{`{"status":"completed","result_url":"https://cdn.example/result.mp4","metadata":{"url":"https://cdn.example/stale.mp4"}}`, "https://cdn.example/result.mp4"},
		{`{"status":"completed","result_url":"https://cdn.example/result.mp4","content":{"video_url":"https://cdn.example/wrong.mp4"}}`, "https://cdn.example/result.mp4"},
	} {
		result, err := a.ParseTaskResult([]byte(test.body))
		require.NoError(t, err)
		assert.Equal(t, test.want, result.Url)
	}
	for _, body := range []string{`{"status":"completed"}`, `{"status":"completed","url":"data:video/mp4;base64,aGVsbG8="}`, `{"status":"completed","url":"https://[fd00::1]/result.mp4"}`} {
		result, err := a.ParseTaskResult([]byte(body))
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusFailure, result.Status)
		assert.Contains(t, result.Reason, "video URL")
	}
}

func TestCreateFailureWithIDIsNotSuccess(t *testing.T) {
	c, recorder, info := testContext(t, `{"model":"public-video","prompt":"scene"}`, "grok-imagine-video-1.5")
	a := &TaskAdaptor{}
	a.Init(info)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"private-upstream","status":"failed","error":{"message":"generation failed"}}`))}
	_, _, err := a.DoResponse(c, resp, info)
	require.NotNil(t, err)
	assert.Equal(t, "", recorder.Body.String())
}

func TestEmptyErrorDoesNotRejectQueuedCreation(t *testing.T) {
	for _, value := range []string{"null", `""`, `{}`} {
		c, recorder, info := testContext(t, `{"model":"public-video","prompt":"scene"}`, "grok-imagine-video-1.5")
		a := &TaskAdaptor{}
		a.Init(info)
		resp := &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(`{"id":"private-upstream","status":"queued","error":` + value + `}`))}
		id, _, err := a.DoResponse(c, resp, info)
		require.Nil(t, err)
		assert.Equal(t, "private-upstream", id)
		assert.Contains(t, recorder.Body.String(), "task_public")
		assert.NotContains(t, recorder.Body.String(), "private-upstream")
	}
}

func TestNativeDoubaoContentStaysNestedAndIntact(t *testing.T) {
	c, _, info := testContext(t, `{"model":"public-video","metadata":{"content":[{"type":"text","text":"scene"},{"type":"image_url","image_url":{"url":"https://example.com/a.jpg"},"role":"reference_image","provider_flag":false}],"ratio":"9:16","resolution":"1080p","generate_audio":false}}`, "doubao-seedance-test")
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.JSONEq(t, `{"model":"doubao-seedance-test","prompt":"scene","metadata":{"content":[{"type":"text","text":"scene"},{"type":"image_url","image_url":{"url":"https://example.com/a.jpg"},"role":"reference_image","provider_flag":false}],"ratio":"9:16","resolution":"1080p","generate_audio":false}}`, bodyBytes(t, a, c, info))
}

func TestPollHTTPFailuresRemainFetchErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/videos/private-upstream", r.URL.Path)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"temporary failure"}}`)
			}))
			defer server.Close()
			resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "secret", map[string]any{"task_id": "private-upstream"}, "")
			require.Error(t, err)
			assert.Nil(t, resp, "HTTP failures must not enter terminal task response parsing/refunds")
			assert.Contains(t, err.Error(), fmt.Sprint(status))
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestPublicVideoNeverContainsUnreadyCacheURL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status model.TaskStatus
		media  int
	}{
		{"pending", model.TaskStatusSuccess, model.MediaStatusPending},
		{"downloading", model.TaskStatusSuccess, model.MediaStatusDownloading},
		{"failed download", model.TaskStatusSuccess, model.MediaStatusFailed},
		{"cleaned", model.TaskStatusSuccess, model.MediaStatusCleaned},
		{"failed task", model.TaskStatusFailure, model.MediaStatusSuccess},
		{"cached", model.TaskStatusSuccess, model.MediaStatusSuccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID: "task_public", Status: tc.status, MediaStatus: tc.media,
				MediaURL: "https://huajingapi.top/media/stale.mp4", Progress: "100%",
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-id", ResultURL: "https://upstream.example/private.mp4"},
			}
			data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
			require.NoError(t, err)
			if tc.status == model.TaskStatusSuccess && tc.media == model.MediaStatusSuccess {
				assert.Contains(t, string(data), task.MediaURL)
			} else {
				assert.NotContains(t, string(data), task.MediaURL)
			}
			assert.NotContains(t, string(data), "private-id")
			assert.NotContains(t, string(data), "upstream.example")
			assert.Equal(t, "https://huajingapi.top/media/stale.mp4", task.MediaURL, "presentation must not mutate persisted task state")
		})
	}
}

func TestConfiguredResolutionPricingIsAccepted(t *testing.T) {
	key := model.ResolutionPriceKey("public-video")
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	old, exists := common.OptionMap[key]
	common.OptionMap[key] = `{"enabled":true,"setting":{"720p":0.1}}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if exists {
			common.OptionMap[key] = old
		} else {
			delete(common.OptionMap, key)
		}
	})
	c, _, info := testContext(t, `{"model":"public-video","prompt":"scene","seconds":8,"resolution":"720p"}`, "dreamina-seedance-2-0")
	err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, err)
}
