package wufan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
)

type contentItem struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL map[string]any `json:"image_url,omitempty"`
	VideoURL map[string]any `json:"video_url,omitempty"`
	AudioURL map[string]any `json:"audio_url,omitempty"`
	Role     string         `json:"role,omitempty"`
}

type submitRequest struct {
	Model                 string        `json:"model"`
	Content               []contentItem `json:"content"`
	GenerateAudio         *bool         `json:"generate_audio,omitempty"`
	BitrateMode           string        `json:"bitrate_mode,omitempty"`
	Tools                 any           `json:"tools,omitempty"`
	Resolution            string        `json:"resolution"`
	Ratio                 string        `json:"ratio"`
	Duration              int           `json:"duration"`
	ExecutionExpiresAfter int           `json:"execution_expires_after,omitempty"`
}

type submitResponse struct {
	Success   bool          `json:"success"`
	Message   string        `json:"message,omitempty"`
	Code      int           `json:"code,omitempty"`
	Result    *responseTask `json:"result,omitempty"`
	Timestamp int64         `json:"timestamp,omitempty"`
}

type responseTask struct {
	ID          json.Number `json:"id"`
	Model       string      `json:"model,omitempty"`
	Status      string      `json:"status"`
	CreatedAt   int64       `json:"created_at,omitempty"`
	CompletedAt int64       `json:"completed_at,omitempty"`
	ExpiresAt   int64       `json:"expires_at,omitempty"`
	Error       any         `json:"error,omitempty"`
	Result      *taskResult `json:"result,omitempty"`
}

type taskResult struct {
	Type  string       `json:"type,omitempty"`
	Data  []resultData `json:"data,omitempty"`
	Usage *usageInfo   `json:"usage,omitempty"`
}

type resultData struct {
	URL    string `json:"url,omitempty"`
	Format string `json:"format,omitempty"`
}

type usageInfo struct {
	TotalTokens      int `json:"total_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + generationsPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}

	payload, err := buildSubmitRequest(body, info)
	if err != nil {
		return nil, err
	}
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_wufan_request_failed")
	}
	return bytes.NewReader(jsonData), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var submitResp submitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if !submitResp.Success || submitResp.Result == nil {
		msg := firstNonEmpty(submitResp.Message, "upstream submit failed")
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", msg), "upstream_error", http.StatusBadRequest)
		return
	}

	upstreamID := submitResp.Result.ID.String()
	if strings.TrimSpace(upstreamID) == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = info.PublicTaskID
	openAIVideo.TaskID = info.PublicTaskID
	openAIVideo.Object = "video"
	openAIVideo.Model = firstNonEmpty(info.OriginModelName, submitResp.Result.Model)
	openAIVideo.Status = mapToVideoStatus(submitResp.Result.Status)
	openAIVideo.SetProgressStr(progressForStatus(submitResp.Result.Status, 0))
	openAIVideo.CreatedAt = firstNonZero(submitResp.Result.CreatedAt, unixFromMillis(submitResp.Timestamp), time.Now().Unix())

	c.JSON(http.StatusOK, openAIVideo)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s%s/%s", strings.TrimRight(baseUrl, "/"), generationsPath, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var fetchResp submitResponse
	if err := common.Unmarshal(respBody, &fetchResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: fetchResp.Code}
	if !fetchResp.Success || fetchResp.Result == nil {
		reason := firstNonEmpty(fetchResp.Message, extractErrorMessage(gjson.GetBytes(respBody, "error").Value()), "upstream returned error")
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = reason
		return &taskResult, nil
	}

	resTask := fetchResp.Result
	switch strings.ToUpper(strings.TrimSpace(resTask.Status)) {
	case "INIT", "SUBMITTED", "PENDING", "QUEUED", "CREATED":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Url = extractResultURL(respBody, *resTask)
		if resTask.Result != nil && resTask.Result.Usage != nil {
			taskResult.TotalTokens = resTask.Result.Usage.TotalTokens
			taskResult.CompletionTokens = resTask.Result.Usage.CompletionTokens
		}
	case "FAIL", "FAILED", "FAILURE", "ERROR", "CANCELLED", "CANCELED":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = extractErrorMessage(resTask.Error)
		if taskResult.Reason == "" {
			taskResult.Reason = firstNonEmpty(fetchResp.Message, "task failed")
		}
	default:
		if msg := firstNonEmpty(extractErrorMessage(resTask.Error), fetchResp.Message); msg != "" && resTask.Status == "" {
			taskResult.Status = model.TaskStatusFailure
			taskResult.Progress = taskcommon.ProgressComplete
			taskResult.Reason = msg
		} else {
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = taskcommon.ProgressInProgress
		}
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var fetchResp submitResponse
	_ = common.Unmarshal(originTask.Data, &fetchResp)

	var dResp responseTask
	if fetchResp.Result != nil {
		dResp = *fetchResp.Result
	} else if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal wufan task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Object = "video"
	openAIVideo.Model = firstNonEmpty(originTask.Properties.OriginModelName, dResp.Model)
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = firstNonZero(originTask.CreatedAt, dResp.CreatedAt)
	openAIVideo.CompletedAt = firstNonZero(originTask.FinishTime, dResp.CompletedAt, originTask.UpdatedAt)

	if url := firstNonEmpty(originTask.GetResultURL(), extractResultURL(originTask.Data, dResp)); url != "" {
		openAIVideo.SetMetadata("url", url)
	}
	if dResp.ExpiresAt > 0 {
		openAIVideo.SetMetadata("expires_at", dResp.ExpiresAt)
	}
	if dResp.Result != nil && dResp.Result.Usage != nil {
		if dResp.Result.Usage.TotalTokens > 0 {
			openAIVideo.SetMetadata("total_tokens", dResp.Result.Usage.TotalTokens)
		}
		if dResp.Result.Usage.CompletionTokens > 0 {
			openAIVideo.SetMetadata("completion_tokens", dResp.Result.Usage.CompletionTokens)
		}
	}
	if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, extractErrorMessage(dResp.Error), fetchResp.Message, "task failed"),
			Code:    "upstream_error",
		}
	}

	return common.Marshal(openAIVideo)
}

func buildSubmitRequest(body []byte, info *relaycommon.RelayInfo) (*submitRequest, error) {
	var raw map[string]any
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, errors.Wrap(err, "unmarshal_request_body_failed")
	}

	prompt := firstNonEmpty(stringFromMap(raw, "prompt"), gjson.GetBytes(body, "input.prompt").String())
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	modelName := normalizeModel(firstNonEmpty(info.UpstreamModelName, stringFromMap(raw, "model"), DefaultUpstreamModel))
	duration := intFromMap(raw, "duration", 5)
	if duration == 0 {
		duration = intFromMap(raw, "seconds", 5)
	}
	resolution := firstNonEmpty(stringFromMap(raw, "resolution"), "720p")
	ratio := firstNonEmpty(stringFromMap(raw, "ratio"), stringFromMap(raw, "aspect_ratio"), stringFromMap(raw, "aspectRatio"), ratioFromSize(stringFromMap(raw, "size")), "adaptive")

	req := &submitRequest{
		Model:      modelName,
		Content:    buildContentItems(raw, prompt),
		Resolution: resolution,
		Ratio:      ratio,
		Duration:   duration,
	}
	if generateAudio, ok := boolFromMap(raw, "generate_audio"); ok {
		req.GenerateAudio = &generateAudio
	}
	req.BitrateMode = stringFromMap(raw, "bitrate_mode")
	req.Tools = raw["tools"]
	req.ExecutionExpiresAfter = intFromMap(raw, "execution_expires_after", 0)

	return req, nil
}

func buildContentItems(raw map[string]any, prompt string) []contentItem {
	if content, ok := raw["content"].([]any); ok && len(content) > 0 {
		items := make([]contentItem, 0, len(content))
		for _, item := range content {
			if converted, ok := convertContentItem(item); ok {
				items = append(items, converted)
			}
		}
		if len(items) > 0 {
			return ensureTextContent(items, prompt)
		}
	}

	items := []contentItem{{Type: "text", Text: prompt}}
	for _, url := range collectStringList(raw, "image_urls", "images", "image", "input_reference") {
		items = append(items, contentItem{Type: "image_url", Role: "reference_image", ImageURL: map[string]any{"url": url}})
	}
	for _, url := range collectStringList(raw, "video_urls", "videos", "video") {
		items = append(items, contentItem{Type: "video_url", Role: "reference_video", VideoURL: map[string]any{"url": url}})
	}
	for _, url := range collectStringList(raw, "audio_urls", "audios", "audio") {
		items = append(items, contentItem{Type: "audio_url", Role: "reference_audio", AudioURL: map[string]any{"url": url}})
	}
	return items
}

func convertContentItem(item any) (contentItem, bool) {
	obj, ok := item.(map[string]any)
	if !ok {
		return contentItem{}, false
	}
	itemType := strings.TrimSpace(common.Interface2String(obj["type"]))
	switch itemType {
	case "text":
		return contentItem{Type: "text", Text: common.Interface2String(obj["text"])}, true
	case "image_url":
		return contentItem{Type: "image_url", Role: common.Interface2String(obj["role"]), ImageURL: normalizeURLObject(obj["image_url"])}, true
	case "video_url":
		return contentItem{Type: "video_url", Role: common.Interface2String(obj["role"]), VideoURL: normalizeURLObject(obj["video_url"])}, true
	case "audio_url":
		return contentItem{Type: "audio_url", Role: common.Interface2String(obj["role"]), AudioURL: normalizeURLObject(obj["audio_url"])}, true
	default:
		return contentItem{}, false
	}
}

func ensureTextContent(items []contentItem, prompt string) []contentItem {
	for _, item := range items {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return items
		}
	}
	return append([]contentItem{{Type: "text", Text: prompt}}, items...)
}

func normalizeURLObject(v any) map[string]any {
	switch val := v.(type) {
	case map[string]any:
		return val
	case string:
		return map[string]any{"url": val}
	default:
		return map[string]any{}
	}
}

func collectStringList(raw map[string]any, keys ...string) []string {
	urls := make([]string, 0)
	seen := map[string]bool{}
	for _, key := range keys {
		value, exists := raw[key]
		if !exists {
			continue
		}
		for _, url := range anyToStringList(value) {
			url = strings.TrimSpace(url)
			if url == "" || seen[url] {
				continue
			}
			seen[url] = true
			urls = append(urls, url)
		}
	}
	return urls
}

func anyToStringList(v any) []string {
	switch val := v.(type) {
	case string:
		if strings.TrimSpace(val) == "" {
			return nil
		}
		return []string{val}
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s := common.Interface2String(item); strings.TrimSpace(s) != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func normalizeModel(modelName string) string {
	switch strings.TrimSpace(modelName) {
	case "", "wufan":
		return DefaultUpstreamModel
	default:
		return modelName
	}
}

func extractResultURL(respBody []byte, resTask responseTask) string {
	for _, key := range []string{
		"result.result.data.0.url",
		"result.data.0.url",
		"data.0.url",
		"url",
	} {
		if value := strings.TrimSpace(gjson.GetBytes(respBody, key).String()); strings.HasPrefix(value, "http") {
			return value
		}
	}
	if resTask.Result != nil {
		for _, item := range resTask.Result.Data {
			if strings.HasPrefix(strings.TrimSpace(item.URL), "http") {
				return strings.TrimSpace(item.URL)
			}
		}
	}
	return ""
}

func extractErrorMessage(errValue any) string {
	switch v := errValue.(type) {
	case string:
		return v
	case map[string]any:
		if message, ok := v["message"].(string); ok {
			return message
		}
		if code, ok := v["code"].(string); ok {
			return code
		}
	case nil:
		return ""
	default:
		data, err := common.Marshal(v)
		if err == nil {
			var m map[string]any
			if common.Unmarshal(data, &m) == nil {
				return extractErrorMessage(m)
			}
		}
	}
	return ""
}

func mapToVideoStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
		return dto.VideoStatusCompleted
	case "FAIL", "FAILED", "FAILURE", "ERROR", "CANCELLED", "CANCELED":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusQueued
	}
}

func progressForStatus(status string, progress int) string {
	if progress > 0 {
		if progress > 100 {
			progress = 100
		}
		return fmt.Sprintf("%d%%", progress)
	}
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SUCCESS", "SUCCEEDED", "COMPLETED", "FAIL", "FAILED", "FAILURE", "ERROR", "CANCELLED", "CANCELED":
		return taskcommon.ProgressComplete
	case "INIT", "SUBMITTED", "PENDING", "QUEUED", "CREATED":
		return taskcommon.ProgressQueued
	default:
		return taskcommon.ProgressInProgress
	}
}

func stringFromMap(raw map[string]any, key string) string {
	return strings.TrimSpace(common.Interface2String(raw[key]))
}

func intFromMap(raw map[string]any, key string, fallback int) int {
	switch v := raw[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return fallback
}

func boolFromMap(raw map[string]any, key string) (bool, bool) {
	v, ok := raw[key]
	if !ok {
		return false, false
	}
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		return b, err == nil
	default:
		return false, false
	}
}

func ratioFromSize(size string) string {
	switch strings.TrimSpace(size) {
	case "1280x720", "1920x1080":
		return "16:9"
	case "720x1280", "1080x1920":
		return "9:16"
	case "1024x1024":
		return "1:1"
	default:
		return ""
	}
}

func unixFromMillis(ms int64) int64 {
	if ms <= 0 {
		return 0
	}
	if ms > 100000000000 {
		return ms / 1000
	}
	return ms
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
