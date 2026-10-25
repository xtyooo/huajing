package xxuan

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const maxTaskResponseBytes = 1 << 20

type TaskAdaptor struct {
	taskcommon.BaseBilling
	Upscale           bool
	apiKey, baseURL   string
	request           videoRequest
	assetID           string
	resolutionPrice   float64
	resolutionBilling bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey, a.baseURL = info.ApiKey, info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	request, err := readVideoRequest(c, a.Upscale)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_video_request", http.StatusBadRequest)
	}
	price, enabled, err := taskcommon.ResolutionPrice(info.OriginModelName, request.resolution)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "resolution_price_invalid", http.StatusBadRequest)
	}
	if a.Upscale {
		_, defaultPrice := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if !enabled && !defaultPrice && !info.UserSetting.AcceptUnsetRatioModel && !helper.HasModelBillingConfig(info.OriginModelName) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("model pricing must be configured before uploading video"), "model_price_error", http.StatusBadRequest)
		}
		uploaded, err := uploadVideo(c.Request.Context(), a.baseURL, a.apiKey, info.ChannelSetting.Proxy, request.sourceURL)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "video_upload_failed", http.StatusBadRequest)
		}
		request.duration = uploaded.DurationSeconds
		a.assetID = uploaded.AssetID
	}
	a.request, a.resolutionPrice, a.resolutionBilling = request, price, enabled
	c.Set("task_request", relaycommon.TaskSubmitReq{Model: request.model, Prompt: request.prompt, Resolution: request.resolution, Duration: request.duration})
	info.Action = constant.TaskActionTextGenerate
	if a.Upscale || len(request.assets["images"])+len(request.assets["videos"])+len(request.assets["audios"]) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if a.resolutionBilling {
		taskcommon.ApplyResolutionPerSecondBilling(c, info, a.request.resolution, a.request.duration, a.resolutionPrice)
		return nil
	}
	c.Set(string(constant.ContextKeyTaskPropsExtra), map[string]interface{}{"resolution": a.request.resolution, "duration": a.request.duration})
	return map[string]float64{"seconds": float64(a.request.duration)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	path := "/video/tasks"
	if a.Upscale {
		path = "/video-upscale/tasks"
	}
	return providerURL(a.baseURL, path), nil
}

func providerURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://api.xxuanaigc.cn/open/v1"
	}
	if !strings.HasSuffix(base, "/open/v1") {
		if strings.HasSuffix(base, "/v1") {
			base = strings.TrimSuffix(base, "/v1")
		}
		if strings.HasSuffix(base, "/open") {
			base += "/v1"
		} else {
			base += "/open/v1"
		}
	}
	return base + path
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.Upscale {
		if info.PublicTaskID == "" {
			return fmt.Errorf("public task ID is unavailable")
		}
		digest := sha256.Sum256([]byte(info.PublicTaskID))
		req.Header.Set("Idempotency-Key", fmt.Sprintf("xxuan_%x", digest))
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.request.model == "" {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	var body any
	if a.Upscale {
		if a.assetID == "" {
			return nil, fmt.Errorf("verified source asset is unavailable")
		}
		body = map[string]any{"source": map[string]string{"asset_id": a.assetID}, "resolution": a.request.resolution, "frame_rate": a.request.frameRate}
	} else {
		product := info.UpstreamModelName
		if product == "" {
			product = a.request.model
		}
		fields := map[string]any{"product_id": product, "duration_seconds": a.request.duration, "ratio": a.request.ratio, "prompt": a.request.prompt, "assets": a.request.assets}
		if a.request.resolution != "" {
			fields["resolution"] = a.request.resolution
		}
		body = fields
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	endpoint, _ := a.BuildRequestURL(info)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("invalid xxuan API URL")
	}
	if err = a.BuildRequestHeader(c, req, info); err != nil {
		return nil, err
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, fmt.Errorf("invalid channel proxy configuration")
	}
	// No replay, including net/http's idempotent-request retry and redirects.
	// Generation explicitly does not deduplicate; upscale replay across accounts
	// would also change asset ownership and the local billing identity.
	req.GetBody = nil
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Set("xxuan_task_submit_started", true)
	resp, err := localClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xxuan submission outcome is uncertain; do not automatically resubmit")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		resp.StatusCode = http.StatusOK
		return resp, nil
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxTaskResponseBytes+1))
	_ = resp.Body.Close()
	message := fmt.Sprintf("xxuan submission returned HTTP %d", resp.StatusCode)
	if readErr == nil && len(data) <= maxTaskResponseBytes {
		if code := responseErrorCode(data); code != "" {
			message += " (" + code + ")"
		}
		// Decode the optional trace independently: an invalid trace type must not
		// hide a valid machine error code. Never forward free-text provider errors.
		var trace struct {
			RequestID string `json:"request_id"`
		}
		if common.Unmarshal(data, &trace) == nil && strings.HasPrefix(trace.RequestID, "req_") && len(trace.RequestID) > 4 && len(trace.RequestID) <= 128 {
			valid := true
			for _, ch := range trace.RequestID[4:] {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
					valid = false
					break
				}
			}
			if valid {
				c.Set(common.UpstreamRequestIdKey, trace.RequestID)
				message += " upstream_request_id=" + trace.RequestID
			}
		}
	}
	// Summarize validated representations only, never their values. Fixed groups,
	// labels and the per-group item cap keep output bounded even with DEBUG on.
	if a.Upscale {
		if a.assetID != "" {
			message += " media=source=asset_id"
		} else {
			message += " media=source=missing"
		}
	} else {
		message += " media="
		for i, group := range []string{"images", "videos", "audios"} {
			if i > 0 {
				message += " "
			}
			items := a.request.assets[group]
			message += fmt.Sprintf("%s=%d[", group, len(items))
			const maxSummaryItems = 3
			for j, item := range items[:min(len(items), maxSummaryItems)] {
				if j > 0 {
					message += ","
				}
				switch value := item.(type) {
				case string:
					if strings.HasPrefix(value, "ast_") {
						message += "asset_id"
					} else {
						message += "url_string"
					}
				case map[string]any:
					message += "url_object"
				default:
					message += "unknown"
				}
			}
			if len(items) > maxSummaryItems {
				message += fmt.Sprintf(",+%d", len(items)-maxSummaryItems)
			}
			message += "]"
		}
	}
	resp.Body = io.NopCloser(strings.NewReader(message))
	resp.ContentLength = int64(len(message))
	return resp, nil
}

type taskResponse struct {
	TaskID               string  `json:"task_id"`
	Status               string  `json:"status"`
	Progress             *int    `json:"progress_percent"`
	ResultURL            *string `json:"result_url"`
	NextPollAfterSeconds int64   `json:"next_poll_after_seconds"`
	ErrorCode            *string `json:"error_code"`
	StatusReason         *string `json:"status_reason"`
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTaskResponseBytes+1))
	_ = resp.Body.Close()
	if err != nil || len(data) > maxTaskResponseBytes {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("invalid xxuan task receipt; submission outcome is uncertain"), "invalid_response", http.StatusBadGateway)
	}
	var receipt taskResponse
	if common.Unmarshal(data, &receipt) != nil || strings.TrimSpace(receipt.TaskID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("xxuan receipt has no task ID; submission outcome is uncertain"), "invalid_response", http.StatusBadGateway)
	}
	result, err := a.ParseTaskResult(data)
	if err != nil || result.Status == model.TaskStatusFailure {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("xxuan did not return an accepted task receipt"), "invalid_response", http.StatusBadGateway)
	}
	c.Set("xxuan_next_poll_at", time.Now().Unix()+result.NextPollAfterSeconds)
	video := relaydto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = relaydto.VideoStatusQueued, time.Now().Unix()
	c.JSON(http.StatusOK, video)
	safeData, _ := common.Marshal(map[string]any{"status": receipt.Status, "next_poll_after_seconds": result.NextPollAfterSeconds})
	return receipt.TaskID, safeData, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("task ID is required")
	}
	path := "/video/tasks/"
	if a.Upscale {
		path = "/video-upscale/tasks/"
	}
	req, err := http.NewRequest(http.MethodGet, providerURL(baseURL, path)+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid xxuan API URL")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("invalid channel proxy configuration")
	}
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := localClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xxuan task query failed")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("xxuan task query returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTaskResponseBytes+1))
	_ = resp.Body.Close()
	if err != nil || len(data) > maxTaskResponseBytes {
		return nil, fmt.Errorf("invalid xxuan task query response")
	}
	if _, err = a.ParseTaskResult(data); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var response taskResponse
	if common.Unmarshal(body, &response) != nil {
		return nil, fmt.Errorf("invalid xxuan task response JSON")
	}
	if response.NextPollAfterSeconds < 0 || response.NextPollAfterSeconds > math.MaxInt64-time.Now().Unix() {
		return nil, fmt.Errorf("invalid xxuan polling interval")
	}
	result := &relaycommon.TaskInfo{TaskID: response.TaskID, NextPollAfterSeconds: response.NextPollAfterSeconds}
	switch response.Status {
	case "SUBMITTED":
		result.Status, result.Progress = model.TaskStatusSubmitted, taskcommon.ProgressSubmitted
	case "PROCESSING", "REVIEW_REQUIRED":
		result.Status, result.Progress = model.TaskStatusInProgress, taskcommon.ProgressInProgress
	case "SUCCEEDED":
		result.Status, result.Progress = model.TaskStatusInProgress, "99%"
		if response.ResultURL != nil && validateMediaURL(*response.ResultURL) == nil {
			result.Status, result.Progress, result.Url = model.TaskStatusSuccess, taskcommon.ProgressComplete, *response.ResultURL
		}
	case "FAILED":
		result.Status, result.Progress, result.Reason = model.TaskStatusFailure, taskcommon.ProgressComplete, "xxuan video task failed"
		if code := responseErrorCode(body); code != "" {
			result.Reason += " (" + code + ")"
		}
	default:
		return nil, fmt.Errorf("unrecognized xxuan task status")
	}
	if response.Progress != nil && response.Status != "SUCCEEDED" && response.Status != "FAILED" {
		progress := *response.Progress
		if progress < 0 || progress > 100 {
			return nil, fmt.Errorf("invalid xxuan task progress")
		}
		if progress == 100 {
			progress = 99
		}
		result.Progress = strconv.Itoa(progress) + "%"
	}
	return result, nil
}

// Only machine-readable codes are safe to reuse; free text can echo signed
// source URLs, material tickets, or API keys.
func responseErrorCode(body []byte) string {
	var response struct {
		Code         string `json:"code"`
		ErrorCode    string `json:"error_code"`
		StatusReason string `json:"status_reason"`
		Error        struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if common.Unmarshal(body, &response) != nil {
		return ""
	}
	for _, code := range []string{response.ErrorCode, response.StatusReason, response.Error.Code, response.Code} {
		if len(code) == 0 || len(code) > 80 {
			continue
		}
		valid := true
		for _, r := range code {
			if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
				valid = false
				break
			}
		}
		if valid {
			return code
		}
	}
	return ""
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	public := *task
	if task.Status != model.TaskStatusSuccess || task.MediaStatus != model.MediaStatusSuccess {
		public.MediaURL = ""
	}
	video := public.ToOpenAIVideo()
	video.TaskID = task.TaskID
	return common.Marshal(video)
}

func (a *TaskAdaptor) GetModelList() []string {
	if a.Upscale {
		return []string{"xxuan-video-upscale"}
	}
	return []string{}
}
func (a *TaskAdaptor) GetChannelName() string {
	if a.Upscale {
		return "xxuan-video-upscale"
	}
	return "xxuan"
}
