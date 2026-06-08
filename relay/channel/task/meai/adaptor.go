package meai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// ValidateRequestAndSetAction parses body for billing but skips prompt validation (passthrough mode).
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
	}
	if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
		req.Images = []string{req.Image}
	}
	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	if len(req.Images) == 0 {
		c.Set("action", constant.TaskActionTextGenerate)
	}
	return
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	return nil
}

// BuildRequestBody passes raw request body to upstream, only replacing model.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}

	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var bodyMap map[string]interface{}
		if err := common.Unmarshal(cachedBody, &bodyMap); err == nil {
			bodyMap["model"] = info.UpstreamModelName
			if newBody, err := common.Marshal(bodyMap); err == nil {
				return bytes.NewReader(newBody), nil
			}
		}
		return bytes.NewReader(cachedBody), nil
	}

	return bytes.NewReader(cachedBody), nil
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


