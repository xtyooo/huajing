package meai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// ============================
// Request / Response structures
// ============================

type meaiInput struct {
	Prompt string      `json:"prompt"`
	Media  []meaiMedia `json:"media,omitempty"`
}

type meaiMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type meaiParameters struct {
	Resolution   string `json:"resolution,omitempty"`
	Ratio        string `json:"ratio,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	PromptExtend *bool  `json:"prompt_extend,omitempty"`
	Watermark    *bool  `json:"watermark,omitempty"`
}

type requestPayload struct {
	Model      string         `json:"model"`
	Input      meaiInput      `json:"input"`
	Parameters meaiParameters `json:"parameters"`
}

type submitResponse struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Object    string `json:"object"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	CreatedAt int64  `json:"created_at"`
}

type queryResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	Seconds   int    `json:"seconds,omitempty"`
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
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// BuildRequestBody converts request into MeAI specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body := a.convertToRequestPayload(&req, info)
	if len(req.Images) == 0 {
		c.Set("action", constant.TaskActionTextGenerate)
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	if action := c.GetString("action"); action != "" {
		info.Action = action
	}
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var mResp submitResponse
	err = common.Unmarshal(responseBody, &mResp)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	switch mResp.Status {
	case "SUCCEEDED":
		ov.Status = dto.VideoStatusCompleted
	case "FAILED":
		ov.Status = dto.VideoStatusFailed
	case "RUNNING":
		ov.Status = dto.VideoStatusInProgress
	default:
		ov.Status = dto.VideoStatusQueued
	}
	if mResp.Progress > 0 {
		ov.Progress = mResp.Progress
	}
	c.JSON(http.StatusOK, ov)
	return mResp.TaskID, responseBody, nil
}

// FetchTask fetch task status from upstream.
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	url := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	taskInfo := &relaycommon.TaskInfo{}
	var qResp queryResponse
	err := common.Unmarshal(respBody, &qResp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}
	taskInfo.TaskID = qResp.ID

	status := qResp.Status
	switch status {
	case "PENDING":
		taskInfo.Status = model.TaskStatusQueued
	case "RUNNING":
		taskInfo.Status = model.TaskStatusInProgress
	case "SUCCEEDED":
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Url = qResp.Object
	case "FAILED":
		taskInfo.Status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown task status: %s", status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var qResp queryResponse
	if err := common.Unmarshal(originTask.Data, &qResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal meai task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = qResp.CreatedAt

	if qResp.Object != "" {
		openAIVideo.SetMetadata("url", qResp.Object)
	}
	if qResp.Seconds > 0 {
		openAIVideo.Seconds = fmt.Sprintf("%d", qResp.Seconds)
	}

	return common.Marshal(openAIVideo)
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) *requestPayload {
	r := &requestPayload{
		Model: info.UpstreamModelName,
		Input: meaiInput{
			Prompt: req.Prompt,
		},
		Parameters: meaiParameters{
			Duration: req.Duration,
		},
	}
	if r.Model == "" {
		r.Model = "seedance-2.0"
	}

	if resolution, ratio := parseSize(req.Size); resolution != "" {
		r.Parameters.Resolution = resolution
		r.Parameters.Ratio = ratio
	}

	if len(req.Images) > 0 {
		for _, img := range req.Images {
			r.Input.Media = append(r.Input.Media, meaiMedia{
				Type: "first_frame",
				URL:  img,
			})
		}
	}

	return r
}

// parseSize converts "1920x1080" to resolution "1080P" and ratio "16:9".
func parseSize(size string) (resolution, ratio string) {
	switch size {
	case "1920x1080", "1280x720", "1080P", "1080":
		return "1080P", "16:9"
	case "1080x1920", "720x1280":
		return "1080P", "9:16"
	case "2048x2048", "1024x1024":
		return "1080P", "1:1"
	case "768x1280":
		return "720P", "9:16"
	case "1280x768":
		return "720P", "16:9"
	case "768x768":
		return "720P", "1:1"
	default:
		return "", ""
	}
}
