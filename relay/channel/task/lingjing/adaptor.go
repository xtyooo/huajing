package lingjing

import (
	"bytes"
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
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// requestPayload is the body sent to Lingjing's create endpoint.
// Docs: POST /api/open/v1/videos
type requestPayload struct {
	Prompt          string   `json:"prompt"`
	Duration        int      `json:"duration"`
	Ratio           string   `json:"ratio,omitempty"`
	ReferenceImages []string `json:"reference_images,omitempty"`
}

// taskResponse mirrors the Lingjing Task object (docs §7.1).
type taskResponse struct {
	ID              int      `json:"id"`
	Prompt          string   `json:"prompt"`
	Ratio           string   `json:"ratio"`
	Duration        int      `json:"duration"`
	Resolution      string   `json:"resolution"`
	ReferenceImages []string `json:"reference_images"`
	Status          string   `json:"status"`
	VideoPath       string   `json:"video_path"`
	ErrorMessage    string   `json:"error_message"`
	Cost            float64  `json:"cost"`
	ProgressPercent int      `json:"progress_percent"`
	Notice          string   `json:"notice"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}

	// duration is optional here (defaults to 5 later); if provided it must be 5/10/15.
	if req.Duration != 0 && req.Duration != 5 && req.Duration != 10 && req.Duration != 15 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be one of 5/10/15"), "invalid_request", http.StatusBadRequest)
	}

	action := constant.TaskActionTextGenerate
	if req.HasImage() {
		action = constant.TaskActionGenerate
	}
	info.Action = action
	return nil
}

// EstimateBilling always returns the duration as the "seconds" ratio.
// Whether it multiplies the price (per-second billing) or is skipped
// (flat per-call billing) is decided by the admin via
// billing_setting.skip_seconds + fixed-price mode.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	duration := taskcommon.DefaultInt(req.Duration, 5)
	return map[string]float64{"seconds": float64(duration)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + createEndpoint, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body := requestPayload{
		Prompt:          req.Prompt,
		Duration:        taskcommon.DefaultInt(req.Duration, 5),
		Ratio:           taskcommon.DefaultString(req.Size, "16:9"),
		ReferenceImages: req.Images,
	}
	// Allow overriding ratio / reference_images via metadata.
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &body); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
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

	var tResp taskResponse
	if err := common.Unmarshal(responseBody, &tResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if tResp.ID == 0 {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("empty task id in response, body=%s", string(responseBody)), "empty_task_id", http.StatusBadRequest)
		return
	}

	if tResp.Status == "failed" {
		reason := tResp.ErrorMessage
		if reason == "" {
			reason = "task failed"
		}
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", reason), "task_failed", http.StatusBadRequest)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return strconv.Itoa(tResp.ID), responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := strings.TrimRight(baseUrl, "/") + queryEndpoint + taskID

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var tResp taskResponse
	if err := common.Unmarshal(respBody, &tResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{}
	switch tResp.Status {
	case "pending":
		taskResult.Status = model.TaskStatusSubmitted
	case "queued":
		taskResult.Status = model.TaskStatusQueued
	case "running":
		taskResult.Status = model.TaskStatusInProgress
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = fmt.Sprintf("%s/api/open/v1/videos/%d/download", a.baseURL, tResp.ID)
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		reason := tResp.ErrorMessage
		if reason == "" {
			reason = "task failed"
		}
		taskResult.Reason = reason
	default:
		taskResult.Status = model.TaskStatusInProgress
	}

	if tResp.ProgressPercent > 0 && tResp.ProgressPercent < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", tResp.ProgressPercent)
	}

	return taskResult, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	// Lingjing's upstream create API has NO model field, so the model name is
	// never sent upstream. It is only used for gateway-side routing and billing,
	// and is fully configured by the admin via the channel's Models field.
	return nil
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	openAIVideo := originTask.ToOpenAIVideo()

	if originTask.Status == model.TaskStatusFailure {
		reason := originTask.FailReason
		if reason == "" {
			reason = "task failed"
		}
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: reason,
		}
	}

	jsonData, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, errors.Wrap(err, "marshal openai video failed")
	}
	return jsonData, nil
}
