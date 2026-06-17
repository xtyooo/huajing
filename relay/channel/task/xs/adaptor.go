package xs

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string

	parsedReq *XsVideoRequest
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var xsReq XsVideoRequest
	if err := common.UnmarshalBodyReusable(c, &xsReq); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}

	if strings.TrimSpace(xsReq.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	a.parsedReq = &xsReq
	info.Action = constant.TaskActionGenerate

	resolution := xsReq.Resolution
	if resolution == "" {
		resolution = DefaultResolution
	}
	if _, priceErr := model.GetResolutionPrice(info.OriginModelName, resolution); priceErr != nil {
		return service.TaskErrorWrapperLocal(priceErr, "resolution_price_invalid", http.StatusBadRequest)
	}

	if xsReq.Duration != 0 && (xsReq.Duration < 4 || xsReq.Duration > 15) {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration must be between 4 and 15 seconds, got %d", xsReq.Duration),
			"invalid_duration",
			http.StatusBadRequest,
		)
	}

	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	resolution := DefaultResolution
	if a.parsedReq != nil && a.parsedReq.Resolution != "" {
		resolution = a.parsedReq.Resolution
	}

	price, err := model.GetResolutionPrice(info.OriginModelName, resolution)
	if err != nil || price <= 0 {
		common.SysError(fmt.Sprintf("xs estimate billing failed for model %s resolution %s: err=%v price=%f", info.OriginModelName, resolution, err, price))
		return nil
	}

	duration := float64(DefaultDuration)
	if a.parsedReq != nil && a.parsedReq.Duration > 0 {
		duration = float64(a.parsedReq.Duration)
	}

	info.PriceData.Quota = int(price * common.QuotaPerUnit * info.PriceData.GroupRatioInfo.GroupRatio * duration)
	c.Set(string(constant.ContextKeyTaskPropsExtra), map[string]interface{}{
		"resolution": resolution,
		"duration":   int(duration),
	})
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(a.baseURL, "/") + SubmitVideoEndpoint, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body := &XsVideoRequest{
		Model:  info.UpstreamModelName,
		Prompt: "",
		Mode:   "text_to_video",
	}

	if a.parsedReq != nil {
		body.Prompt = a.parsedReq.Prompt
		body.Mode = a.parsedReq.Mode
		body.Resolution = a.parsedReq.Resolution
		body.Ratio = a.parsedReq.Ratio
		body.Duration = a.parsedReq.Duration
		body.ImageUrls = a.parsedReq.ImageUrls
		body.VideoUrls = a.parsedReq.VideoUrls
		body.VideoDurations = a.parsedReq.VideoDurations
		body.AudioUrls = a.parsedReq.AudioUrls
		body.RefImages = a.parsedReq.RefImages
	}

	if body.Mode == "" {
		body.Mode = "text_to_video"
	}
	if body.Resolution == "" {
		body.Resolution = DefaultResolution
	}
	if body.Ratio == "" {
		body.Ratio = DefaultRatio
	}
	if body.Duration <= 0 {
		body.Duration = DefaultDuration
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

	var xsResp XsSubmitResponse
	if err := common.Unmarshal(responseBody, &xsResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if xsResp.TaskID == "" {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("xs api error: no task_id in response, body=%s", string(responseBody)),
			"empty_task_id",
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	if xsResp.CreatedAt > 0 {
		ov.CreatedAt = xsResp.CreatedAt
	}
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return xsResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := strings.TrimRight(baseUrl, "/") + QueryTaskEndpoint + taskID

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
	var queryResp XsQueryResponse
	if err := common.Unmarshal(respBody, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{}

	switch queryResp.Status {
	case "QUEUED", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "PROCESSING", "processing":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCESS", "success":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Url = queryResp.ResultURL
	case "FAILURE", "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		reason := queryResp.ErrorMsg
		if reason == "" {
			reason = parseFailReason(queryResp.FailReason)
		}
		if reason == "" {
			reason = "task failed"
		}
		taskResult.Reason = reason
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressSubmitted
	}

	return taskResult, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var queryResp XsQueryResponse
	if err := common.Unmarshal(originTask.Data, &queryResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal xs task data failed")
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if queryResp.Status == "FAILURE" || queryResp.Status == "failed" {
		reason := queryResp.ErrorMsg
		if reason == "" {
			reason = parseFailReason(queryResp.FailReason)
		}
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

func parseFailReason(val interface{}) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	if m, ok := val.(map[string]interface{}); ok {
		if msg, ok := m["message"].(string); ok && msg != "" {
			return msg
		}
		if msg, ok := m["msg"].(string); ok && msg != "" {
			return msg
		}
	}
	data, err := common.Marshal(val)
	if err != nil {
		return ""
	}
	return string(data)
}
