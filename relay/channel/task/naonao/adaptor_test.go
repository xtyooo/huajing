package naonao

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNaonaoContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newNaonaoInfo(originModel, upstreamModel string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		OriginModelName: originModel,
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	info.UpstreamModelName = upstreamModel
	return info
}

func TestBuildRequestBodyMapsStandardImagesAndAudioToContent(t *testing.T) {
	c, _ := newNaonaoContext(t, `{
		"model":"public-naonao",
		"prompt":"[@图一] follows [@图二] with [@音频一]",
		"duration":"6",
		"aspect_ratio":"9:16",
		"resolution":"1080p",
		"image_url":"https://example.test/main.png?token=abc",
		"reference_image_urls":["https://example.test/ref.png"],
		"audio_url":"https://example.test/audio.mp3",
		"generate_audio":false,
		"watermark":false
	}`)
	adaptor := &TaskAdaptor{}
	info := newNaonaoInfo("public-naonao", "seedance-2.5")
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))

	assert.Equal(t, "seedance-2.5", body["model"])
	assert.Equal(t, "6", body["seconds"])
	assert.Equal(t, "9:16", body["ratio"])
	assert.Equal(t, "720p", body["resolution"])
	assert.Equal(t, false, body["generate_audio"])
	assert.Equal(t, false, body["watermark"])
	content, ok := body["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 4)
	assert.Equal(t, "text", content[0].(map[string]any)["type"])
	assert.Equal(t, "https://example.test/main.png?token=abc", content[1].(map[string]any)["image_url"].(map[string]any)["url"])
	assert.Equal(t, "https://example.test/ref.png", content[2].(map[string]any)["image_url"].(map[string]any)["url"])
	assert.Equal(t, "https://example.test/audio.mp3", content[3].(map[string]any)["audio_url"].(map[string]any)["url"])
	assert.Equal(t, map[string]float64{"seconds": 6}, adaptor.EstimateBilling(c, info))
}

func TestNaonaoDefaultsToFiveSecondsAnd720P(t *testing.T) {
	c, _ := newNaonaoContext(t, `{"model":"wan3.0-video","prompt":"city"}`)
	adaptor := &TaskAdaptor{}
	info := newNaonaoInfo("wan3.0-video", "wan3.0-video")
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	assert.Equal(t, "5", body["seconds"])
	assert.Equal(t, "16:9", body["ratio"])
	assert.Equal(t, "720p", body["resolution"])
}

func TestNaonaoRejectsUnsupportedAndUnsafeInputs(t *testing.T) {
	tests := []struct {
		name  string
		model string
		body  string
		code  string
	}{
		{name: "unsupported mapped model", model: "other", body: `{"model":"alias","prompt":"p"}`, code: "unsupported_model"},
		{name: "duration too short", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","duration":4}`, code: "invalid_duration"},
		{name: "duration too long", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","seconds":31}`, code: "invalid_duration"},
		{name: "invalid ratio", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","ratio":"21:9"}`, code: "invalid_aspect_ratio"},
		{name: "reference video", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","reference_video":"https://example.test/v.mp4"}`, code: "unsupported_reference_media"},
		{name: "start frame", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","image_url":"https://example.test/a.png","video_config":{"reference_mode":"start_frame"}}`, code: "unsupported_reference_mode"},
		{name: "start end", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","reference_image_urls":["https://example.test/a.png","https://example.test/b.png"],"video_config":{"reference_mode":"start_end"}}`, code: "unsupported_reference_mode"},
		{name: "http image", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","image_url":"http://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "too many audio files", model: "seedance-2.0", body: `{"model":"seedance-2.0","prompt":"p","audio_urls":["https://example.test/1.mp3","https://example.test/2.mp3","https://example.test/3.mp3","https://example.test/4.mp3","https://example.test/5.mp3","https://example.test/6.mp3"]}`, code: "invalid_reference_count"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newNaonaoContext(t, test.body)
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, newNaonaoInfo(test.model, test.model))
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestNaonaoResolutionPricingAlwaysUses720PForOriginalModel(t *testing.T) {
	setNaonaoResolutionPricing(t, "public-naonao", `{"enabled":true,"setting":{"720p":0.13}}`)
	c, _ := newNaonaoContext(t, `{"model":"public-naonao","prompt":"city","resolution":"1080p"}`)
	info := newNaonaoInfo("public-naonao", "wan3.0-video")
	c.Set("group", "default")
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	priceData, err := relayhelper.ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	info.PriceData = priceData

	ratios := adaptor.EstimateBilling(c, info)

	assert.Nil(t, ratios)
	assert.True(t, info.PriceData.UsePrice)
	assert.Equal(t, 0.13, info.PriceData.ModelPrice)
	assert.Equal(t, 325000, info.PriceData.Quota)
	props, ok := c.Get(string(constant.ContextKeyTaskPropsExtra))
	require.True(t, ok)
	assert.Equal(t, map[string]interface{}{"resolution": "720P", "duration": 5}, props)
}

func TestNaonaoDoResponseAndPollingKeepUpstreamDataPrivate(t *testing.T) {
	c, recorder := newNaonaoContext(t, `{}`)
	info := newNaonaoInfo("public-naonao", "seedance-2.0")
	info.PublicTaskID = "task_public"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"upstream-secret","status":"queued"}`))}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.Contains(t, recorder.Body.String(), "task_public")
	assert.NotContains(t, recorder.Body.String(), "upstream-secret")

	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"completed","url":"https://cdn.example.test/video.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), result.Status)
	assert.Equal(t, "https://cdn.example.test/video.mp4", result.Url)

	missing, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"completed"}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusFailure), missing.Status)
	assert.Contains(t, missing.Reason, "missing a valid video URL")
}

func TestNaonaoDoRequestAndFetchUseDocumentedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
		switch request.Method {
		case http.MethodPost:
			assert.Equal(t, "/v1/videos", request.URL.Path)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"upstream"}`))
		case http.MethodGet:
			assert.Equal(t, "/v1/videos/task_upstream", request.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"in_progress"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	c, _ := newNaonaoContext(t, `{}`)
	adaptor := &TaskAdaptor{apiKey: "secret", baseURL: server.URL}

	resp, err := adaptor.DoRequest(c, newNaonaoInfo("seedance-2.0", "seedance-2.0"), bytes.NewBufferString(`{}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = adaptor.FetchTask(server.URL+"/v1", "secret", map[string]any{"task_id": "task_upstream"}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
}

func TestNaonaoStatusAliasesAndFailureReasons(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   string
		wantProgress string
		wantReason   string
	}{
		{name: "queued", body: `{"status":"pending","progress":10}`, wantStatus: string(model.TaskStatusQueued), wantProgress: "10%"},
		{name: "running caps premature completion", body: `{"status":"running","progress":"100%"}`, wantStatus: string(model.TaskStatusInProgress), wantProgress: "99%"},
		{name: "failed object error", body: `{"status":"failed","error":{"message":"provider rejected"}}`, wantStatus: string(model.TaskStatusFailure), wantProgress: "100%", wantReason: "provider rejected"},
		{name: "cancelled string error", body: `{"status":"cancelled","error":"cancelled upstream"}`, wantStatus: string(model.TaskStatusFailure), wantProgress: "100%", wantReason: "cancelled upstream"},
		{name: "unknown", body: `{"status":"mystery"}`, wantStatus: string(model.TaskStatusFailure), wantProgress: "100%", wantReason: "unknown upstream task status: mystery"},
		{name: "http result rejected", body: `{"status":"completed","video_url":"http://cdn.example.test/video.mp4"}`, wantStatus: string(model.TaskStatusFailure), wantProgress: "100%", wantReason: "completed task is missing a valid video URL"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, result.Status)
			assert.Equal(t, test.wantProgress, result.Progress)
			assert.Equal(t, test.wantReason, result.Reason)
		})
	}
}

func TestNaonaoResolutionPricingRejectsMissing720PTier(t *testing.T) {
	setNaonaoResolutionPricing(t, "public-naonao", `{"enabled":true,"setting":{"1080p":0.40}}`)
	c, _ := newNaonaoContext(t, `{"model":"public-naonao","prompt":"city"}`)
	info := newNaonaoInfo("public-naonao", "seedance-2.0")

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "resolution_price_invalid", taskErr.Code)
}

func TestNaonaoConvertToOpenAIVideoUsesOnlyCachedURL(t *testing.T) {
	task := &model.Task{
		TaskID:      "task_public",
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		MediaStatus: model.MediaStatusPending,
		PrivateData: model.TaskPrivateData{ResultURL: "https://upstream.example.test/private.mp4"},
	}
	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var pending map[string]any
	require.NoError(t, common.Unmarshal(data, &pending))
	assert.Equal(t, "task_public", pending["id"])
	assert.NotContains(t, pending, "metadata")

	task.MediaStatus = model.MediaStatusSuccess
	task.MediaURL = "https://huajingapi.top/media/task_public.mp4"
	data, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var completed map[string]any
	require.NoError(t, common.Unmarshal(data, &completed))
	assert.Equal(t, task.MediaURL, completed["metadata"].(map[string]any)["url"])
}

func setNaonaoResolutionPricing(t *testing.T, modelName, value string) {
	t.Helper()
	key := model.ResolutionPriceKey(modelName)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if existed {
			common.OptionMap[key] = previous
		} else {
			delete(common.OptionMap, key)
		}
	})
}

func TestNaonaoCatalog(t *testing.T) {
	assert.Equal(t, []string{"wan3.0-video", "seedance-2.0", "seedance-2.0-fast", "seedance-2.5"}, (&TaskAdaptor{}).GetModelList())
	assert.Equal(t, "naonao", (&TaskAdaptor{}).GetChannelName())
}
