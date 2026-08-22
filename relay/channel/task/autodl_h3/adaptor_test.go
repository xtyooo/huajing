package autodl_h3

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAutoDLH3TestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func testRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
}

func decodeRequestBody(t *testing.T, adaptor *TaskAdaptor, c *gin.Context, info *relaycommon.RelayInfo) map[string]any {
	t.Helper()
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	return body
}

func TestTextWorkflowBuildsExactRequestAndSecondsBilling(t *testing.T) {
	c, _ := newAutoDLH3TestContext(t, `{
		"model":"public-alias",
		"prompt":"  cinematic cat  ",
		"duration":12,
		"seconds":4,
		"resolution":"720p",
		"ratio":"16:9"
	}`)
	adaptor := &TaskAdaptor{}
	info := testRelayInfo()
	info.UpstreamModelName = TextWorkflowID

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body := decodeRequestBody(t, adaptor, c, info)

	assert.Equal(t, map[string]any{
		"prompt":     "cinematic cat",
		"duration":   float64(12),
		"resolution": "768p横",
	}, body)
	assert.Equal(t, constant.TaskActionTextGenerate, info.Action)
	assert.Equal(t, map[string]float64{"seconds": 12}, adaptor.EstimateBilling(c, info))
}

func TestReferenceWorkflowMapsSeedImagesAndAudiosInOrder(t *testing.T) {
	c, _ := newAutoDLH3TestContext(t, `{
		"model":"public-reference-alias",
		"prompt":"reference animation",
		"seconds":"15",
		"aspect_ratio":"9:16",
		"resolution":"480p",
		"seed":0,
		"images":["https://example.test/0.png","https://example.test/1.png"],
		"reference_image_urls":["https://example.test/ignored.png"],
		"audios":["https://example.test/0.mp3","https://example.test/1.wav"],
		"audio_url":"https://example.test/ignored.mp3"
	}`)
	adaptor := &TaskAdaptor{}
	info := testRelayInfo()
	info.UpstreamModelName = ReferenceWorkflowID

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body := decodeRequestBody(t, adaptor, c, info)

	assert.Equal(t, map[string]any{
		"prompt":      "reference animation",
		"duration":    float64(15),
		"resolution":  "480p竖",
		"seed":        float64(0),
		"ref_image_0": "https://example.test/0.png",
		"ref_image_1": "https://example.test/1.png",
		"ref_audio_0": "https://example.test/0.mp3",
		"ref_audio_1": "https://example.test/1.wav",
	}, body)
	assert.Equal(t, constant.TaskActionGenerate, info.Action)
	assert.Equal(t, map[string]float64{"seconds": 15}, adaptor.EstimateBilling(c, info))
}

func TestReferenceWorkflowCombinesLegacySingleAndArrayAliases(t *testing.T) {
	c, _ := newAutoDLH3TestContext(t, `{
		"model":"reference-model",
		"prompt":"legacy references",
		"image_url":"https://example.test/main.png",
		"reference_image_urls":["https://example.test/second.png"],
		"audio_url":"https://example.test/main.mp3",
		"reference_audios":["https://example.test/second.wav"]
	}`)
	adaptor := &TaskAdaptor{}
	info := testRelayInfo()
	info.UpstreamModelName = ReferenceWorkflowID

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body := decodeRequestBody(t, adaptor, c, info)

	assert.Equal(t, "https://example.test/main.png", body["ref_image_0"])
	assert.Equal(t, "https://example.test/second.png", body["ref_image_1"])
	assert.Equal(t, "https://example.test/main.mp3", body["ref_audio_0"])
	assert.Equal(t, "https://example.test/second.wav", body["ref_audio_1"])
}

func TestValidateRequestRejectsWorkflowAndInputViolations(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		body     string
		code     string
	}{
		{name: "missing prompt", upstream: TextWorkflowID, body: `{"model":"m","prompt":" "}`, code: "invalid_prompt"},
		{name: "prompt too long", upstream: TextWorkflowID, body: `{"model":"m","prompt":"` + string(bytes.Repeat([]byte("x"), 10001)) + `"}`, code: "invalid_prompt"},
		{name: "duration zero", upstream: TextWorkflowID, body: `{"model":"m","prompt":"p","duration":0}`, code: "invalid_duration"},
		{name: "duration over max", upstream: TextWorkflowID, body: `{"model":"m","prompt":"p","duration":16}`, code: "invalid_duration"},
		{name: "unsupported square ratio", upstream: TextWorkflowID, body: `{"model":"m","prompt":"p","ratio":"1:1"}`, code: "invalid_resolution"},
		{name: "text workflow image", upstream: TextWorkflowID, body: `{"model":"m","prompt":"p","images":["https://example.test/a.png"]}`, code: "unsupported_reference_media"},
		{name: "reference workflow too many images", upstream: ReferenceWorkflowID, body: `{"model":"m","prompt":"p","images":["https://e.test/0.png","https://e.test/1.png","https://e.test/2.png","https://e.test/3.png","https://e.test/4.png","https://e.test/5.png","https://e.test/6.png","https://e.test/7.png","https://e.test/8.png","https://e.test/9.png"]}`, code: "invalid_reference_count"},
		{name: "reference workflow too many audios", upstream: ReferenceWorkflowID, body: `{"model":"m","prompt":"p","audios":["https://e.test/0.mp3","https://e.test/1.mp3","https://e.test/2.mp3","https://e.test/3.mp3"]}`, code: "invalid_reference_count"},
		{name: "reference workflow rejects video", upstream: ReferenceWorkflowID, body: `{"model":"m","prompt":"p","reference_video":"https://example.test/a.mp4"}`, code: "unsupported_reference_media"},
		{name: "non https image", upstream: ReferenceWorkflowID, body: `{"model":"m","prompt":"p","image_url":"http://example.test/a.png"}`, code: "invalid_reference_url"},
		{name: "unknown mapped workflow", upstream: "unknown_workflow", body: `{"model":"m","prompt":"p"}`, code: "unsupported_model"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newAutoDLH3TestContext(t, test.body)
			info := testRelayInfo()
			info.UpstreamModelName = test.upstream
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Equal(t, test.code, taskErr.Code)
		})
	}
}

func TestSubmitAndPollingUseDocumentedURLsRawTokenAndGET(t *testing.T) {
	var methods []string
	var paths []string
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.EscapedPath())
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Success","data":{"task_id":"upstream/task","status":"QUEUED"}}`))
	}))
	t.Cleanup(server.Close)

	c, _ := newAutoDLH3TestContext(t, `{"model":"m","prompt":"p"}`)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "raw-token"}
	pathInfo := testRelayInfo()
	pathInfo.UpstreamModelName = "workflow/with slash"

	submitURL, err := adaptor.BuildRequestURL(pathInfo)
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/api/v1/comfyui/comfyui_workflow/workflow%2Fwith%20slash", submitURL)

	info := testRelayInfo()
	info.UpstreamModelName = TextWorkflowID
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	submitResp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	require.NoError(t, submitResp.Body.Close())

	resp, err := adaptor.FetchTask(server.URL, "raw-token", map[string]any{"task_id": "upstream/task"}, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, []string{http.MethodPost, http.MethodGet}, methods)
	assert.Equal(t, []string{
		"/api/v1/comfyui/comfyui_workflow/" + TextWorkflowID,
		"/api/v1/comfyui/comfyui_workflow/result/upstream%2Ftask",
	}, paths)
	assert.Equal(t, []string{"raw-token", "raw-token"}, authorizations)
}

func TestDoResponseValidatesBusinessSuccessAndKeepsPrivateIDPrivate(t *testing.T) {
	c, recorder := newAutoDLH3TestContext(t, `{}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewBufferString(`{
			"code":"Success",
			"data":{"task_id":"upstream-secret","status":"QUEUED"}
		}`)),
	}
	info := testRelayInfo()
	info.OriginModelName = "public-model"

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "upstream-secret", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "upstream-secret")
	assert.Contains(t, recorder.Body.String(), "task_public")

	failedResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"code":"Failed","msg":"invalid token","data":{}}`)),
	}
	_, _, failed := (&TaskAdaptor{}).DoResponse(c, failedResp, info)
	require.NotNil(t, failed)
	assert.Equal(t, "submit_failed", failed.Code)
}

func TestParseTaskResultMapsNestedStatusesAndSelectsVideoOutput(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		status     model.TaskStatus
		progress   string
		url        string
		reasonPart string
	}{
		{name: "queued", body: `{"code":"Success","data":{"status":"QUEUED"}}`, status: model.TaskStatusQueued, progress: taskcommon.ProgressQueued},
		{name: "running", body: `{"code":"Success","data":{"status":"running"}}`, status: model.TaskStatusInProgress, progress: taskcommon.ProgressInProgress},
		{name: "completed priority", body: `{"code":"Success","data":{"status":"completed","results":[{"url":"https://example.test/image.png","type":"image","file_type":"png","output_type":"output"},{"url":"https://example.test/preview.mp4","type":"video","file_type":"mp4","output_type":"preview"},{"url":"https://example.test/final.mp4","type":"video","file_type":"mp4","output_type":"output"}]}}`, status: model.TaskStatusSuccess, progress: "100%", url: "https://example.test/final.mp4"},
		{name: "failed", body: `{"code":"Success","data":{"status":"FAILED","message":"workflow node failed"}}`, status: model.TaskStatusFailure, progress: "100%", reasonPart: "workflow node failed"},
		{name: "success without video", body: `{"code":"Success","data":{"status":"SUCCESS","results":[]}}`, status: model.TaskStatusFailure, progress: "100%", reasonPart: "missing valid video"},
		{name: "business failure", body: `{"code":"Failed","msg":"request rejected","data":{}}`, status: model.TaskStatusFailure, progress: "100%", reasonPart: "request rejected"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, string(test.status), result.Status)
			assert.Equal(t, test.progress, result.Progress)
			assert.Equal(t, test.url, result.Url)
			if test.reasonPart != "" {
				assert.Contains(t, result.Reason, test.reasonPart)
			}
		})
	}
}

func TestAdaptorMetadataConversionAndUnvalidatedGuards(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "raw-token",
		ChannelBaseUrl: "https://autodl.art/",
	}})

	assert.Equal(t, ChannelName, adaptor.GetChannelName())
	assert.Equal(t, ModelList, adaptor.GetModelList())
	assert.Nil(t, adaptor.EstimateBilling(nil, nil), "unvalidated requests must not create billing ratios")

	_, err := adaptor.BuildRequestURL(testRelayInfo())
	require.ErrorContains(t, err, "workflow is required")
	_, err = adaptor.BuildRequestBody(nil, nil)
	require.ErrorContains(t, err, "validated request is unavailable")
	_, err = adaptor.FetchTask("https://autodl.art", "raw-token", map[string]any{}, "")
	require.ErrorContains(t, err, "invalid task_id")

	task := &model.Task{
		TaskID:   "task_public",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: TextWorkflowID,
		},
		MediaURL: "https://huajing.example.test/media/cached.mp4",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example.test/short-lived.mp4",
		},
	}
	data, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public", response["id"])
	assert.Equal(t, "completed", response["status"])
	metadata, ok := response["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://huajing.example.test/media/cached.mp4", metadata["url"])
	assert.NotContains(t, string(data), "short-lived.mp4")
}

func TestResponseErrorMessageTruncatesWithoutBreakingUTF8(t *testing.T) {
	message := responseErrorMessage(strings.Repeat("错", 501))

	assert.True(t, utf8.ValidString(message))
	assert.Equal(t, 500, utf8.RuneCountInString(message))
}
