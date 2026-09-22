package longxia6

import (
	"bytes"
	"encoding/base64"
	"fmt"
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

func contextFor(t *testing.T, payload, upstream string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{OriginModelName: "public-video", ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	info.UpstreamModelName, info.PublicTaskID = upstream, "task_public"
	return c, w, info
}

func TestExactUpstreamRequestPreservesAssetsAndTags(t *testing.T) {
	c, _, info := contextFor(t, `{"model":"public-video","prompt":"Animate @image1 with @image2, @audio1 and @video1","seconds":"12","aspect_ratio":"16:9","resolution":"720p","generate_audio":false,"watermark":false,"assets":[{"category":"image","url":"https://example.com/a.png?sig=a%2Fb"}],"image_urls":"https://example.com/a.png?sig=a%2Fb","audio_urls":["data:audio/mpeg;base64,YWJj"],"reference_videos":"https://example.com/a.mp4"}`, "LongXia-video-seedance2_5-standard-720p-reference-video-express")
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"LongXia-video-seedance2_5-standard-720p-reference-video-express","prompt":"Animate @image1 with @image2, @audio1 and @video1","duration":12,"size":"16:9","assets":[{"category":"image","url":"https://example.com/a.png?sig=a%2Fb"},{"category":"image","url":"https://example.com/a.png?sig=a%2Fb"},{"category":"audio","data_base64":"YWJj"},{"category":"video","url":"https://example.com/a.mp4"}]}`, string(data))
	assert.Nil(t, a.EstimateBilling(c, info))
}

func TestConfiguredModelsDelegateCapabilitiesAndKeepBilling(t *testing.T) {
	for _, upstream := range append(append([]string{}, ModelList...), "future-video", "future-video-PerSecond") {
		c, _, info := contextFor(t, `{"model":"public-video","prompt":"scene","duration":100,"ratio":"2:1"}`, upstream)
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		if strings.HasSuffix(upstream, "-PerSecond") {
			assert.Equal(t, map[string]float64{"seconds": 100}, a.EstimateBilling(c, info))
		} else {
			assert.Nil(t, a.EstimateBilling(c, info))
		}
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		var body upstreamRequest
		require.NoError(t, common.DecodeJson(reader, &body))
		assert.Equal(t, upstream, body.Model)
		assert.Equal(t, 100, body.Duration)
		assert.Equal(t, "2:1", body.Size)
	}
	for _, duration := range []int{0, -1, relaycommon.MaxTaskDurationSeconds + 1} {
		c, _, info := contextFor(t, fmt.Sprintf(`{"model":"public-video","prompt":"scene","duration":%d}`, duration), "future-video")
		require.NotNil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	}
}

func TestDefaultsUnknownMappingAndSizeCompatibility(t *testing.T) {
	for _, tc := range []struct{ extra, size string }{{"", "9:16"}, {`,"size":"1280x720"`, "16:9"}, {`,"size":"720x1280"`, "9:16"}, {`,"size":"1024x1024"`, "1:1"}, {`,"aspect_ratio":"21:9"`, "21:9"}} {
		c, _, info := contextFor(t, `{"model":"public-video","prompt":"scene"`+tc.extra+`}`, "future-provider-video-PerSecond")
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, `{"model":"future-provider-video-PerSecond","prompt":"scene","duration":8,"size":"`+tc.size+`"}`, string(data))
		assert.Equal(t, map[string]float64{"seconds": 8}, a.EstimateBilling(c, info))
	}
}

func TestAliasesAndScalarArrayMedia(t *testing.T) {
	groups := map[string][]string{"image": {"images", "image_urls", "reference_image_urls", "reference_images", "ref_images", "image_refs", "references", "reference_urls"}, "audio": {"audios", "audio_urls", "reference_audios", "referenceAudios", "ref_audios", "audio_refs"}, "video": {"videos", "video_urls", "reference_videos", "referenceVideos", "video_refs"}}
	for category, fields := range groups {
		for _, field := range fields {
			for _, array := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t", field, array), func(t *testing.T) {
					var media any = "https://example.com/ref"
					if array {
						media = []string{"https://example.com/ref"}
					}
					input := map[string]any{"model": "public-video", "prompt": "@" + category + "1", field: media}
					if category == "audio" {
						input["prompt"] = "@audio1 @image1"
						input["image"] = "https://example.com/image"
					}
					data, err := common.Marshal(input)
					require.NoError(t, err)
					c, _, info := contextFor(t, string(data), "LongXia-video-seedance2_5-standard-720p-reference-video-express")
					a := &TaskAdaptor{}
					require.Nil(t, a.ValidateRequestAndSetAction(c, info))
					reader, err := a.BuildRequestBody(c, info)
					require.NoError(t, err)
					var body upstreamRequest
					require.NoError(t, common.DecodeJson(reader, &body))
					assert.Contains(t, body.Assets, asset{Category: category, URL: "https://example.com/ref"})
				})
			}
		}
	}
}

func TestKnownModelsDoNotImposeOldMaterialLimits(t *testing.T) {
	assets := []asset{}
	tags := []string{}
	for _, category := range []string{"image", "audio", "video"} {
		for n := 1; n <= 31; n++ {
			assets = append(assets, asset{Category: category, URL: "https://example.com/ref"})
			tags = append(tags, fmt.Sprintf("@%s%d", category, n))
		}
	}
	data, err := common.Marshal(map[string]any{"model": "public-video", "prompt": strings.Join(tags, " "), "assets": assets})
	require.NoError(t, err)
	for _, upstream := range ModelList {
		c, _, info := contextFor(t, string(data), upstream)
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		var body upstreamRequest
		require.NoError(t, common.DecodeJson(reader, &body))
		assert.Equal(t, assets, body.Assets)
	}
	c, _, info := contextFor(t, `{"model":"public-video","prompt":"@audio1","audios":"https://example.com/a.mp3"}`, "LongXia-video-minimax-h3-express")
	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
}

func TestInvalidRequests(t *testing.T) {
	for _, tc := range []struct{ name, extra, prompt string }{
		{"missing-reference", `,"images":"https://example.com/i"`, "animate"},
		{"out-of-range", `,"images":"https://example.com/i"`, "@image2"},
		{"gap", `,"images":["https://example.com/1","https://example.com/2"]`, "@image2"},
		{"zero-index", `,"images":"https://example.com/i"`, "@image0"},
		{"leading-zero", `,"images":"https://example.com/i"`, "@image01"},
		{"nonexistent", ``, "@video1"},
		{"both-sources", `,"assets":[{"category":"image","url":"https://example.com/i","data_base64":"YWJj"}]`, "@image1"},
		{"empty-source", `,"assets":[{"category":"image","url":""}]`, "@image1"},
		{"extra-field", `,"assets":[{"category":"image","url":"https://example.com/i","role":"reference"}]`, "@image1"},
		{"bad-category", `,"assets":[{"category":"file","url":"https://example.com/i"}]`, "scene"},
		{"null-asset", `,"assets":[null]`, "scene"},
		{"bad-base64", `,"assets":[{"category":"image","data_base64":"%%%"}]`, "@image1"},
		{"base64-prefix", `,"assets":[{"category":"image","data_base64":"data:image/png;base64,YWJj"}]`, "@image1"},
		{"mime-mismatch", `,"images":"data:audio/mpeg;base64,YWJj"`, "@image1"},
		{"http", `,"images":"http://example.com/i"`, "@image1"},
		{"private", `,"images":"https://192.168.0.1/i"`, "@image1"},
		{"frame", `,"first_frame_url":"https://example.com/i"`, "scene"},
		{"frame-mode", `,"reference_mode":"start_end"`, "scene"},
		{"duration-conflict", `,"duration":4,"seconds":"8"`, "scene"},
		{"ratio-conflict", `,"ratio":"9:16","aspect_ratio":"16:9"`, "scene"},
		{"size-conflict", `,"size":"9:16","aspect_ratio":"16:9"`, "scene"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, info := contextFor(t, `{"model":"public-video","prompt":"`+tc.prompt+`"`+tc.extra+`}`, "LongXia-video-seedance2_5-standard-720p-reference-video-express")
			err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, err)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
		})
	}
}

func TestPromptLengthIsDelegatedToProvider(t *testing.T) {
	for _, upstream := range ModelList {
		c, _, info := contextFor(t, `{"model":"public-video","prompt":"`+strings.Repeat("图", 10001)+`"}`, upstream)
		require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
	}
}

func TestInlineBase64Limits(t *testing.T) {
	// Exercise real input bounds: one too-large MP3 and the encoded request-wide cap.
	for _, tc := range []struct {
		category string
		source   string
	}{
		{"audio", base64.StdEncoding.EncodeToString(make([]byte, (15<<20)+1))},
		{"video", strings.Repeat("A", (40<<20)+4)},
	} {
		payload := map[string]any{"model": "public-video", "prompt": "@image1 @" + tc.category + "1", "image": "https://example.com/i", "assets": []asset{{Category: tc.category, DataBase64: tc.source}}}
		data, err := common.Marshal(payload)
		require.NoError(t, err)
		c, _, info := contextFor(t, string(data), "LongXia-video-seedance2_5-standard-720p-reference-video-express")
		validationErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
		require.NotNil(t, validationErr)
		assert.Equal(t, "invalid_reference_media", validationErr.Code)
	}
}

func TestMultipartRepeatedURLs(t *testing.T) {
	var buffer bytes.Buffer
	w := multipart.NewWriter(&buffer)
	for _, field := range [][2]string{{"model", "public-video"}, {"prompt", "@image1 @image2"}, {"seconds", "8"}, {"images", "https://example.com/1"}, {"images", "https://example.com/2"}} {
		require.NoError(t, w.WriteField(field[0], field[1]))
	}
	require.NoError(t, w.Close())
	c, _, info := contextFor(t, "", "LongXia-video-seedance2_0-mini-express")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &buffer)
	c.Request.Header.Set("Content-Type", w.FormDataContentType())
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	var body upstreamRequest
	require.NoError(t, common.DecodeJson(reader, &body))
	assert.Equal(t, []asset{{Category: "image", URL: "https://example.com/1"}, {Category: "image", URL: "https://example.com/2"}}, body.Assets)
}

func TestResolutionPricingFailsBeforeSubmission(t *testing.T) {
	key := model.ResolutionPriceKey("public-video")
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	old, exists := common.OptionMap[key]
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
	for _, setting := range []string{`{"enabled":true,"setting":{"720P":0.1}}`, `{malformed`} {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[key] = setting
		common.OptionMapRWMutex.Unlock()
		c, _, info := contextFor(t, `{"model":"public-video","prompt":"scene"}`, "LongXia-video-seedance2_0-mini-express")
		err := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
		require.NotNil(t, err)
		assert.Equal(t, "unsupported_pricing", err.Code)
	}
}

func TestSubmitHTTPStatusAndIDPrivacy(t *testing.T) {
	for _, status := range []int{200, 201, 202} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/videos", r.URL.Path)
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"task_id":"upstream-private","status":"queued","created_at":"2026-09-22T00:00:00Z"}`)
		}))
		c, w, info := contextFor(t, `{"model":"public-video","prompt":"scene"}`, "LongXia-video-seedance2_0-mini-express")
		info.ChannelBaseUrl, info.ApiKey = server.URL+"/v1/", "test-key"
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
		assert.Equal(t, "upstream-private", id)
		assert.Contains(t, w.Body.String(), "task_public")
		assert.NotContains(t, w.Body.String(), id)
		server.Close()
	}
}

func TestFetchAndResultSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/private-id", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `{"status":"completed","created_at":"1786184799","data":[{"media_type":"image/jpeg","url":"https://example.com/cover.jpg"},{"url":"https://example.com/fallback.mp4"},{"media_type":"video/mp4","url":"https://example.com/video?sig=a%2Fb"}]}`)
	}))
	defer server.Close()
	a := &TaskAdaptor{}
	for _, base := range []string{server.URL, server.URL + "/v1"} {
		resp, err := a.FetchTask(base, "test-key", map[string]any{"task_id": "private-id"}, "")
		require.NoError(t, err)
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		result, err := a.ParseTaskResult(data)
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
		assert.Equal(t, "https://example.com/video?sig=a%2Fb", result.Url)
	}
}

func TestResponseStatesAndMissingVideoFailure(t *testing.T) {
	for _, tc := range []struct{ body, status, url string }{
		{`{"status":"queued","created_at":1786184799}`, string(model.TaskStatusQueued), ""},
		{`{"status":"in_progress","created_at":"1786184799"}`, string(model.TaskStatusInProgress), ""},
		{`{"status":"running","progress":"42"}`, string(model.TaskStatusInProgress), ""},
		{`{"status":"unknown"}`, string(model.TaskStatusInProgress), ""},
		{`{"status":"failed","error":{"message":"generation failed"}}`, string(model.TaskStatusFailure), ""},
		{`{"status":"cancelled"}`, string(model.TaskStatusFailure), ""},
		{`{"status":"completed","data":[{"media_type":"image/jpeg","url":"https://example.com/image.mp4"}]}`, string(model.TaskStatusFailure), ""},
		{`{"status":"completed","data":[]}`, string(model.TaskStatusFailure), ""},
		{`{"status":"completed","url":"https://example.com/not-documented.mp4"}`, string(model.TaskStatusFailure), ""},
		{`{"status":"completed","data":[{"media_type":"video/mp4","url":"https://127.0.0.1/a.mp4"}]}`, string(model.TaskStatusFailure), ""},
		{`{"status":"completed","data":[{"url":"https://example.com/video.mp4?sig=abc"}]}`, string(model.TaskStatusSuccess), "https://example.com/video.mp4?sig=abc"},
	} {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tc.body))
		require.NoError(t, err)
		assert.Equal(t, tc.status, result.Status)
		assert.Equal(t, tc.url, result.Url)
	}
}

func TestPublicVideoUsesCachedURL(t *testing.T) {
	task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, MediaStatus: model.MediaStatusSuccess, MediaURL: "https://huajing.example/media/public.mp4"}
	task.PrivateData.UpstreamTaskID = "private-id"
	task.PrivateData.ResultURL = "https://upstream.example/private.mp4"
	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(data), "task_public")
	assert.NotContains(t, string(data), "private-id")
	assert.NotContains(t, string(data), "upstream.example")
}

func TestScalarAliasPrecedenceAndResolutionHints(t *testing.T) {
	for _, size := range []string{"480p", "720P", "1440p"} {
		c, _, info := contextFor(t, `{"model":"public-video","prompt":"@image1 @audio1 @video1","size":"`+size+`","aspect_ratio":"16:9","image":"https://example.com/i","image_url":"https://example.com/i","input_reference":"https://example.com/i","audio":"https://example.com/a","audio_url":"https://example.com/a","ref_audio":"https://example.com/a","input_audio":"https://example.com/a","video":"https://example.com/v","video_url":"https://example.com/v","reference_video":"https://example.com/v"}`, "LongXia-video-seedance2_5-standard-720p-reference-video-express")
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		var body upstreamRequest
		require.NoError(t, common.DecodeJson(reader, &body))
		assert.Equal(t, "16:9", body.Size)
		assert.Equal(t, []asset{{Category: "image", URL: "https://example.com/i"}, {Category: "audio", URL: "https://example.com/a"}, {Category: "video", URL: "https://example.com/v"}}, body.Assets)
	}
}

func TestProgressClampedUntilTerminalState(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`42`, "42%"}, {`"55%"`, "55%"}, {`100`, "99%"}, {`"1e100"`, "99%"}, {`"NaN"`, "30%"}} {
		result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"in_progress","progress":` + tc.raw + `}`))
		require.NoError(t, err)
		assert.Equal(t, tc.want, result.Progress)
	}
}
