package snumom

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := common.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("duration must be an integer")
		}
		raw = strings.TrimSpace(value)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("duration must be an integer")
	}
	*n = flexibleInt(value)
	return nil
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey, baseURL string
	body            map[string]any
	doubaoEndpoint  bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	// The provider documents a Jimeng-compatible endpoint for the Doubao token
	// models. Other models use the standard OpenAI-compatible /v1/videos API.
	a.doubaoEndpoint = strings.HasPrefix(strings.ToLower(strings.TrimSpace(info.UpstreamModelName)), "doubao-seedance-")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.body = nil
	request, err := readStandardRequest(c)
	if err != nil {
		return invalid("invalid_json", "invalid video request JSON")
	}
	if strings.TrimSpace(request.Model) == "" {
		return invalid("missing_model", "model is required")
	}
	prompt, images, videos, audios, err := requestMaterials(request)
	if err != nil {
		return invalid("invalid_reference_media", err.Error())
	}
	if strings.TrimSpace(prompt) == "" {
		// Keep provider-native metadata.content intact while accepting its text
		// as the standard prompt, when there is no top-level prompt/content.
		if content, ok := request.Metadata["content"].([]any); ok {
			parts := []string{}
			for _, value := range content {
				part, ok := value.(map[string]any)
				if !ok || part["type"] != "text" {
					continue
				}
				if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
			prompt = strings.Join(parts, "\n")
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return invalid("invalid_prompt", "prompt is required")
	}

	var duration *int
	if request.Duration != nil {
		value := int(*request.Duration)
		duration = &value
	} else if request.Seconds != nil {
		value := int(*request.Seconds)
		duration = &value
	}
	if request.Duration != nil && request.Seconds != nil && *request.Duration != *request.Seconds {
		return invalid("invalid_duration", "seconds and duration conflict")
	}
	if duration != nil && !validDuration(*duration) {
		return invalid("invalid_duration", fmt.Sprintf("duration must be -1 or between 1 and %d seconds", relaycommon.MaxTaskDurationSeconds))
	}
	if err := validateDurationFields(request.Metadata); err != nil {
		return invalid("invalid_duration", err.Error())
	}
	modelName := firstNonEmpty(info.UpstreamModelName, request.Model)
	grok := strings.HasPrefix(strings.ToLower(modelName), "grok-imagine-video-")
	if err := validateReferenceInputs(images, videos, audios, grok); err != nil {
		return err
	}
	if err := validateForwardedMetadata(request.Metadata, grok); err != nil {
		return err
	}
	pricing, err := model.LoadResolutionPricing(info.OriginModelName)
	if err != nil || pricing != nil {
		return invalid("unsupported_pricing", "snumom requires configured model base pricing; resolution-per-second pricing is not supported")
	}
	a.doubaoEndpoint = strings.HasPrefix(strings.ToLower(modelName), "doubao-seedance-")
	a.body = buildVideoProtocolBody(modelName, request, duration, prompt, images, videos, audios, a.doubaoEndpoint)
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func invalid(code, message string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	// Snumom exposes both per-call and seconds/token products. The selected
	// base price is controlled by the administrator's model configuration.
	// The adapter never infers a multiplier from model names. Unsupported
	// resolution-per-second pricing is rejected before submission.
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	if a.doubaoEndpoint {
		return providerURL(a.baseURL, "/v1/video/generations"), nil
	}
	return providerURL(a.baseURL, "/v1/videos"), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = info.PublicTaskID
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	body := cloneMap(a.body)
	body["model"] = firstNonEmpty(info.UpstreamModelName, fmt.Sprint(body["model"]))
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, body)
	if err == nil && resp != nil && (resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted) {
		resp.StatusCode = http.StatusOK
	}
	return resp, err
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	if !gjson.ValidBytes(data) {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("invalid upstream task response JSON"), "invalid_response", http.StatusBadGateway)
	}
	result, err := a.ParseTaskResult(data)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "invalid_response", http.StatusBadGateway)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.Status == model.TaskStatusFailure || hasUpstreamError(data) {
		message := firstNonEmpty(result.Reason, "upstream rejected video task")
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", common.MaskSensitiveInfo(message)), "upstream_error", http.StatusBadGateway)
	}
	id := extractString(data, "id", "task_id", "data.id", "data.task_id", "data.data.id", "data.data.task_id", "result.id", "result.task_id")
	if id == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = relaydto.VideoStatusQueued, time.Now().Unix()
	c.JSON(http.StatusOK, video)
	return id, data, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	// Polling receives only the private task ID from the shared service. Try the
	// standard endpoint first and fall back to the Doubao generations endpoint
	// on 404, which keeps both protocols usable without leaking model metadata
	// into the task polling interface.
	paths := []string{"/v1/videos", "/v1/video/generations"}
	for index, endpoint := range paths {
		req, err := http.NewRequest(http.MethodGet, providerURL(baseURL, endpoint)+"/"+url.PathEscape(strings.TrimSpace(id)), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if index == 0 && len(paths) > 1 && resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("snumom task query returned HTTP %d", resp.StatusCode)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("snumom task query failed")
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid upstream task response JSON")
	}
	status := strings.ToLower(extractString(body, "data.data.status", "data.status", "result.status", "status"))
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending", "submitted", "not_start", "waiting":
		result.Status, result.Progress = model.TaskStatusQueued, taskcommon.ProgressQueued
	case "in_progress", "processing", "running", "generating":
		result.Status, result.Progress = model.TaskStatusInProgress, progress(body, taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded", "done":
		result.Url = extractResultURL(body)
		result.Status, result.Progress = model.TaskStatusSuccess, taskcommon.ProgressComplete
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed task is missing a valid video URL; check the upstream result response"
		}
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		result.Status, result.Progress, result.Reason = model.TaskStatusFailure, taskcommon.ProgressComplete, common.MaskSensitiveInfo(extractString(body, "error.message", "error", "message", "detail", "data.error.message", "data.message"))
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		result.Status, result.Progress = model.TaskStatusInProgress, progress(body, taskcommon.ProgressInProgress)
		if hasUpstreamError(body) {
			result.Status, result.Progress = model.TaskStatusFailure, taskcommon.ProgressComplete
			result.Reason = common.MaskSensitiveInfo(firstNonEmpty(extractString(body, "error.message", "error", "message"), "upstream task query failed"))
		}
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	publicTask := *task
	if task.Status != model.TaskStatusSuccess || task.MediaStatus != model.MediaStatusSuccess {
		publicTask.MediaURL = ""
	}
	video := publicTask.ToOpenAIVideo()
	video.TaskID = task.TaskID
	if task.Status == model.TaskStatusFailure {
		video.Error = &relaydto.OpenAIVideoError{Message: firstNonEmpty(task.FailReason, "task failed"), Code: "upstream_error"}
	}
	return common.Marshal(video)
}

func (*TaskAdaptor) GetModelList() []string { return ModelList }
func (*TaskAdaptor) GetChannelName() string { return ChannelName }

func extractResultURL(body []byte) string {
	paths := []string{"content.video_url", "data.data.content.video_url", "data.content.video_url", "result_url", "video_url", "url", "metadata.url", "metadata.result_url", "result.url", "result.video_url", "data.video_url", "data.result_url", "data.url", "data.metadata.url", "data.metadata.result_url", "data.data.video_url", "data.data.result_url", "data.data.url", "data.data.metadata.url", "data.data.metadata.result_url", "result.metadata.url", "result.metadata.result_url"}
	// Grok never requires a model echo on polling. Its top-level url is the
	// canonical result, ahead of metadata and nested compatibility envelopes.
	paths = append([]string{"url"}, paths...)
	name := strings.ToLower(extractString(body, "model", "data.model", "data.data.model"))
	if name == "" || strings.HasPrefix(name, "grok-imagine-video-") || strings.HasPrefix(name, "dreamina-seedance-") {
		paths = append([]string{"url", "result_url", "video_url"}, paths...)
	}
	for _, path := range paths {
		value := strings.TrimSpace(gjson.GetBytes(body, path).String())
		if validPublicHTTPURL(value) {
			return value
		}
	}
	return ""
}

func hasUpstreamError(body []byte) bool {
	for _, path := range []string{"error", "data.error", "data.data.error", "result.error"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.Null {
			continue
		}
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return true
		}
		if value.IsObject() && (value.Get("message").String() != "" || value.Get("code").String() != "") {
			return true
		}
	}
	if success := gjson.GetBytes(body, "success"); success.Type == gjson.False {
		return true
	}
	// A flat error code/message without task status is an error response, not a
	// task that should remain in_progress forever.
	if extractString(body, "status", "data.status", "data.data.status", "result.status") == "" {
		code := gjson.GetBytes(body, "code")
		if code.Exists() && code.Type != gjson.Null {
			value := code.String()
			return value != "" && value != "0" && value != "200" && value != "201" && value != "202" && value != "success"
		}
	}
	return false
}

func extractTaskStatus(body []byte) string {
	return strings.ToLower(extractString(body, "data.status", "status", "result.status", "data.data.status"))
}

func extractString(body []byte, paths ...string) string {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return strings.TrimSpace(value.String())
		}
	}
	return ""
}

func progress(body []byte, fallback string) string {
	for _, path := range []string{"data.progress", "progress", "result.progress", "data.data.progress"} {
		value := gjson.GetBytes(body, path)
		if value.Exists() {
			raw := strings.TrimSpace(value.String())
			raw = strings.TrimSuffix(raw, "%")
			if parsed, err := strconv.Atoi(raw); err == nil {
				if parsed >= 100 {
					parsed = 99
				}
				if parsed > 0 {
					return fmt.Sprintf("%d%%", parsed)
				}
			}
		}
	}
	return fallback
}

func providerURL(base, endpoint string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base + strings.TrimPrefix(endpoint, "/v1")
	}
	return base + endpoint
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
