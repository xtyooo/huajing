package xxuan

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
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
	info := &relaycommon.RelayInfo{OriginModelName: "public-video", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "opaque-future-product", ChannelBaseUrl: "https://api.example.com/open/v1", ApiKey: "test-key"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_1234567890"}}
	return c, w, info
}

func TestXxuanGenerationWireAndOrderedAssets(t *testing.T) {
	for _, payload := range []string{
		`{"model":"public-video","prompt":"@图1 @图2 @视频1","seconds":"7","aspect_ratio":"17:9","resolution":"future-resolution","assets":{"images":["ast_owned",{"source_url":"https://cdn.example/a.png?sig=a%2Bb","file_name":"图.png"}],"videos":["https://cdn.example/video.mp4"]},"metadata":{"price":1,"model":"wrong"}}`,
		`{"model":"public-video","prompt":"@图1 @图2 @视频1","duration":7,"ratio":"17:9","resolution":"future-resolution","images":["ast_owned",{"url":"https://cdn.example/a.png?sig=a%2Bb","file_name":"图.png"}],"reference_videos":"https://cdn.example/video.mp4"}`,
	} {
		c, _, info := requestContext(t, payload)
		a := &TaskAdaptor{}
		a.Init(info)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		body, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		data, err := io.ReadAll(body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"product_id":"opaque-future-product","duration_seconds":7,"ratio":"17:9","resolution":"future-resolution","prompt":"@图1 @图2 @视频1","assets":{"images":["ast_owned",{"source_url":"https://cdn.example/a.png?sig=a%2Bb","file_name":"图.png"}],"videos":["https://cdn.example/video.mp4"]}}`, string(data))
		assert.Equal(t, map[string]float64{"seconds": 7}, a.EstimateBilling(c, info))
	}
}

func TestXxuanGenerationEmptyAssetsAndPixelSize(t *testing.T) {
	c, _, info := requestContext(t, `{"model":"public-video","prompt":"scene","duration":6,"size":"1280x720"}`)
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	body, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"product_id":"opaque-future-product","duration_seconds":6,"ratio":"16:9","prompt":"scene","assets":{}}`, string(data))
}

func TestXxuanRequestRejectsMalformedInputsBeforeUpload(t *testing.T) {
	for _, test := range []struct {
		payload string
		upscale bool
	}{
		{`{"model":"m","prompt":"x","duration":0,"ratio":"16:9"}`, false},
		{`{"model":"m","prompt":"x","duration":3601,"ratio":"16:9"}`, false},
		{`{"model":"m","prompt":"x","duration":2,"seconds":"3","ratio":"16:9"}`, false},
		{`{"model":"m","prompt":"x","duration":2,"ratio":"16:9","assets":{"images":[{"asset_id":"ast_x"}]}}`, false},
		{`{"model":"m","prompt":"x","duration":2,"ratio":"16:9","images":"https://127.0.0.1/a"}`, false},
		{`{"model":"m","prompt":"x","duration":2,"ratio":"16:9","images":"https://user:password@cdn.example/a"}`, false},
		{`{"model":"m","prompt":"x","duration":2,"ratio":"16:9","images":"https://cdn.example/a","image_url":"https://cdn.example/b"}`, false},
		{`{"model":"m","resolution":"4k","frame_rate":60,"video":"ast_owned"}`, true},
		{`{"model":"m","resolution":"4k","frame_rate":60,"fps":30,"video_url":"https://cdn.example/a.mp4"}`, true},
		{`{"model":"m","resolution":"4k","frame_rate":60,"videos":["https://cdn.example/a.mp4","https://cdn.example/b.mp4"]}`, true},
	} {
		c, _, info := requestContext(t, test.payload)
		a := &TaskAdaptor{Upscale: test.upscale}
		a.Init(info)
		require.NotNil(t, a.ValidateRequestAndSetAction(c, info), test.payload)
		assert.False(t, c.GetBool("xxuan_task_submit_started"))
	}
}

func TestXxuanUpscaleWireUsesOnlyVerifiedSourceAndRequiredFields(t *testing.T) {
	c, _, info := requestContext(t, `{"model":"public-video","resolution":"future-tier","frame_rate":48,"video_url":"https://cdn.example/a.mp4","duration":1,"prompt":"not upstream"}`)
	request, err := readVideoRequest(c, true)
	require.NoError(t, err)
	request.duration = 8 // receipt from upload complete, never the client duration
	a := &TaskAdaptor{Upscale: true, request: request, assetID: "ast_verified"}
	a.Init(info)
	body, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"source":{"asset_id":"ast_verified"},"resolution":"future-tier","frame_rate":48}`, string(data))
	assert.Equal(t, map[string]float64{"seconds": 8}, a.EstimateBilling(c, info))
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com", nil)
	c.Request.Header.Set("Idempotency-Key", "untrusted-replay")
	require.NoError(t, a.BuildRequestHeader(c, req, info))
	key := req.Header.Get("Idempotency-Key")
	assert.Regexp(t, `^[A-Za-z0-9_-]{16,100}$`, key)
	assert.NotEqual(t, "untrusted-replay", key)
	require.NoError(t, a.BuildRequestHeader(c, req, info))
	assert.Equal(t, key, req.Header.Get("Idempotency-Key"))
}

func TestXxuanResolutionBillingWritesSingleAbsoluteQuota(t *testing.T) {
	c, _, info := requestContext(t, `{}`)
	info.PriceData.GroupRatioInfo.GroupRatio = 1.5
	a := &TaskAdaptor{request: videoRequest{duration: 8, resolution: "1080p"}, resolutionBilling: true, resolutionPrice: .2}
	require.Nil(t, a.EstimateBilling(c, info))
	assert.Equal(t, common.QuotaFromFloat(.2*8*1.5*common.QuotaPerUnit), info.PriceData.Quota)
	assert.True(t, info.PriceData.UsePrice)
	extra, ok := c.Get(string(constant.ContextKeyTaskPropsExtra))
	require.True(t, ok)
	assert.Equal(t, map[string]interface{}{"resolution": "1080p", "duration": 8}, extra)
}

func TestXxuanTaskStatesAndResultPrivacy(t *testing.T) {
	a := &TaskAdaptor{}
	for _, test := range []struct{ body, status, progress, url string }{
		{`{"status":"SUBMITTED","progress_percent":null}`, model.TaskStatusSubmitted, "10%", ""},
		{`{"status":"REVIEW_REQUIRED","progress_percent":null,"billing_status":"REFUNDED"}`, model.TaskStatusInProgress, "30%", ""},
		{`{"status":"PROCESSING","progress_percent":100}`, model.TaskStatusInProgress, "99%", ""},
		{`{"status":"SUCCEEDED","result_url":null}`, model.TaskStatusInProgress, "99%", ""},
		{`{"status":"SUCCEEDED","result_url":"https://localhost/a"}`, model.TaskStatusInProgress, "99%", ""},
		{`{"status":"SUCCEEDED","result_url":"https://cdn.example/result?sig=a%2Bb"}`, model.TaskStatusSuccess, "100%", "https://cdn.example/result?sig=a%2Bb"},
		{`{"status":"FAILED","status_message":"https://secret.example/?ticket=private","error_code":"BAD_VIDEO"}`, model.TaskStatusFailure, "100%", ""},
	} {
		result, err := a.ParseTaskResult([]byte(test.body))
		require.NoError(t, err)
		assert.Equal(t, test.status, result.Status)
		assert.Equal(t, test.progress, result.Progress)
		assert.Equal(t, test.url, result.Url)
		assert.NotContains(t, result.Reason, "private")
	}
	for _, body := range []string{`{"status":"NEW_UNKNOWN"}`, `{"status":"PROCESSING","next_poll_after_seconds":-1}`, `{"status":"PROCESSING","next_poll_after_seconds":9223372036854775807}`} {
		_, err := a.ParseTaskResult([]byte(body))
		require.Error(t, err)
	}
	c, w, info := requestContext(t, `{}`)
	resp := &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"task_id":"tsk_private","status":"SUBMITTED","next_poll_after_seconds":45,"result_url":"https://cdn.example/result?sig=private","locked_price":{"amount_cents":90000}}`))}
	before := time.Now().Unix()
	id, data, taskErr := a.DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "tsk_private", id)
	assert.NotContains(t, string(data), "private")
	assert.NotContains(t, w.Body.String(), "private")
	assert.Contains(t, w.Body.String(), info.PublicTaskID)
	assert.GreaterOrEqual(t, c.GetInt64("xxuan_next_poll_at"), before+45)
}

func TestXxuanHTTPSubmitDispatchAndFailureSanitization(t *testing.T) {
	service.InitHttpClient()
	for _, status := range []int{202, 503, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/open/v1/video/tasks", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Empty(t, r.Header.Get("Idempotency-Key"))
				w.Header().Set("Location", "/other")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"task_id":"tsk_private","status":"SUBMITTED","error":{"code":"PROVIDER_BUSY","message":"https://secret.example/?ticket=secret"}}`)
			}))
			defer server.Close()
			c, _, info := requestContext(t, `{}`)
			info.ChannelBaseUrl = server.URL
			a := &TaskAdaptor{}
			a.Init(info)
			resp, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.True(t, c.GetBool("xxuan_task_submit_started"))
			assert.Equal(t, 1, calls)
			if status == 202 {
				assert.Equal(t, 200, resp.StatusCode)
			} else {
				data, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.NotContains(t, string(data), "secret")
				assert.Contains(t, string(data), "PROVIDER_BUSY")
			}
		})
	}
}

func TestXxuanPollingUsesOperationAndSelectedKey(t *testing.T) {
	service.InitHttpClient()
	for _, upscale := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := "/open/v1/video/tasks/tsk_saved"
			if upscale {
				path = "/open/v1/video-upscale/tasks/tsk_saved"
			}
			assert.Equal(t, path, r.URL.Path)
			assert.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, `{"task_id":"tsk_saved","status":"REVIEW_REQUIRED","next_poll_after_seconds":90}`)
		}))
		a := &TaskAdaptor{Upscale: upscale}
		response, err := a.FetchTask(server.URL+"/open/v1", "selected-key", map[string]any{"task_id": "tsk_saved"}, "")
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		_ = response.Body.Close()
		server.Close()
		result, err := a.ParseTaskResult(data)
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusInProgress, result.Status)
		assert.Equal(t, int64(90), result.NextPollAfterSeconds)
	}
}
