package sd0717

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
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type responseTask struct {
	ID             string         `json:"id"`
	TaskID         string         `json:"task_id,omitempty"`
	Object         string         `json:"object,omitempty"`
	Model          string         `json:"model,omitempty"`
	Status         string         `json:"status"`
	Progress       int            `json:"progress,omitempty"`
	CreatedAt      int64          `json:"created_at,omitempty"`
	CompletedAt    int64          `json:"completed_at,omitempty"`
	VideoURL       string         `json:"video_url,omitempty"`
	StableVideoURL string         `json:"stable_video_url,omitempty"`
	Result         string         `json:"result,omitempty"`
	Code           string         `json:"code,omitempty"`
	Message        string         `json:"message,omitempty"`
	Error          any            `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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
	return fmt.Sprintf("%s/v2/generate", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, _ *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}
	updated, err := sjson.SetBytes(body, "model", UpstreamModelAivide2)
	if err == nil {
		body = updated
	}
	return bytes.NewReader(body), nil
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

	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamID := firstNonEmpty(dResp.ID, dResp.TaskID)
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	if dResp.Object == "" {
		dResp.Object = "video"
	}
	if dResp.Model == "" {
		dResp.Model = info.OriginModelName
	}
	if dResp.Status == "" {
		dResp.Status = relaydto.VideoStatusQueued
	}
	if dResp.CreatedAt == 0 {
		dResp.CreatedAt = time.Now().Unix()
	}

	c.JSON(http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v2/generate/%s", strings.TrimRight(baseUrl, "/"), taskID)
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
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{Code: 0}
	status := strings.ToLower(strings.TrimSpace(resTask.Status))
	if status == "" {
		taskResult.Reason = firstNonEmpty(extractErrorMessage(resTask.Error), resTask.Message, resTask.Code)
		if taskResult.Reason != "" {
			taskResult.Status = model.TaskStatusFailure
			taskResult.Progress = taskcommon.ProgressComplete
		}
		return &taskResult, nil
	}
	switch status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = progressString(resTask.Progress, taskcommon.ProgressQueued)
	case "running", "processing", "in_progress", "unknown":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = progressString(resTask.Progress, taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Url = extractResultURL(respBody, resTask)
		taskResult.TotalTokens = intFromMetadata(resTask.Metadata, "total_tokens")
		taskResult.CompletionTokens = intFromMetadata(resTask.Metadata, "completion_tokens")
	case "failed", "failure", "error", "cancelled", "canceled":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = extractErrorMessage(resTask.Error)
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = progressString(resTask.Progress, taskcommon.ProgressInProgress)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal sd0717 task data failed")
	}

	openAIVideo := relaydto.NewOpenAIVideo()
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
	if totalTokens := intFromMetadata(dResp.Metadata, "total_tokens"); totalTokens > 0 {
		openAIVideo.SetMetadata("total_tokens", totalTokens)
	}
	if completionTokens := intFromMetadata(dResp.Metadata, "completion_tokens"); completionTokens > 0 {
		openAIVideo.SetMetadata("completion_tokens", completionTokens)
	}
	if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &relaydto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, extractErrorMessage(dResp.Error), "task failed"),
			Code:    "upstream_error",
		}
	}

	return common.Marshal(openAIVideo)
}

func extractResultURL(respBody []byte, resTask responseTask) string {
	for _, key := range []string{"video_url", "stable_video_url", "result", "metadata.url", "url", "data.0.url", "data.0.video_url"} {
		if value := strings.TrimSpace(gjson.GetBytes(respBody, key).String()); strings.HasPrefix(value, "http") {
			return value
		}
	}
	return firstHTTPURL(resTask.VideoURL, resTask.StableVideoURL, resTask.Result)
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

func progressString(progress int, fallback string) string {
	if progress <= 0 {
		return fallback
	}
	if progress > 100 {
		progress = 100
	}
	return fmt.Sprintf("%d%%", progress)
}

func intFromMetadata(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch v := metadata[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstHTTPURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "http") {
			return value
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
