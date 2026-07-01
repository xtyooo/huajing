package mimo

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

	parsedReq *MimoSubmitReq
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var raw map[string]interface{}
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}

	prompt, _ := raw["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	duration := 8
	if d, ok := raw["duration"].(float64); ok && d > 0 {
		duration = int(d)
	}

	aspectRatio := "9:16"
	if ar, ok := raw["aspectRatio"].(string); ok && ar != "" {
		aspectRatio = ar
	}
	if metadata, ok := raw["metadata"].(map[string]interface{}); ok {
		if ar, ok := metadata["aspectRatio"].(string); ok && ar != "" {
			aspectRatio = ar
		}
	}

	req := &MimoSubmitReq{
		Prompt:      prompt,
		Duration:    duration,
		AspectRatio: aspectRatio,
	}

	if imagesRaw, ok := raw["images"].([]interface{}); ok {
		for _, img := range imagesRaw {
			switch v := img.(type) {
			case string:
				req.Images = append(req.Images, MimoImageItem{ImageUrl: v})
			case map[string]interface{}:
				item := MimoImageItem{}
				if u, ok := v["imageUri"].(string); ok {
					item.ImageUri = u
				}
				if u, ok := v["imageUrl"].(string); ok {
					item.ImageUrl = u
				}
				req.Images = append(req.Images, item)
			}
		}
	}

	a.parsedReq = req
	info.Action = constant.TaskActionGenerate
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	duration := 8.0
	if a.parsedReq != nil && a.parsedReq.Duration > 0 {
		duration = float64(a.parsedReq.Duration)
	}
	return map[string]float64{"duration": duration}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + "/api/video/generate", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body := &MimoSubmitReq{
		ModelName:   info.UpstreamModelName,
		Prompt:      " ",
		Duration:    8,
		AspectRatio: "9:16",
	}

	if a.parsedReq != nil {
		body.Prompt = a.parsedReq.Prompt
		body.Duration = a.parsedReq.Duration
		body.AspectRatio = a.parsedReq.AspectRatio
		body.Images = a.parsedReq.Images
	}

	if body.Prompt == "" {
		body.Prompt = " "
	}
	if body.Duration <= 0 {
		body.Duration = 8
	}
	if body.AspectRatio == "" {
		body.AspectRatio = "9:16"
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

	var submitResp MimoSubmitResp
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if submitResp.Code != 200 {
		msg := submitResp.Msg
		if msg == "" {
			msg = fmt.Sprintf("upstream error code %d", submitResp.Code)
		}
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", msg), "upstream_error", http.StatusBadRequest)
		return
	}

	if submitResp.Data.ID == "" {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("mimo: empty task id in response, body=%s", string(responseBody)),
			"empty_task_id",
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return submitResp.Data.ID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := strings.TrimRight(baseUrl, "/") + "/api/video/batch-status"

	batchReq := MimoBatchReq{TaskIDs: []string{taskID}}
	reqBytes, err := common.Marshal(batchReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var batchResp MimoBatchResp
	if err := common.Unmarshal(respBody, &batchResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal mimo batch result failed")
	}

	if len(batchResp.Data) == 0 {
		return nil, fmt.Errorf("mimo: task not found in batch status response")
	}

	item := batchResp.Data[0]

	videoURL := item.VideoURL
	coverURL := item.CoverURL

	if videoURL == "" && len(item.SubTasks) > 0 {
		last := item.SubTasks[len(item.SubTasks)-1]
		if last.VideoInfo != nil {
			videoURL = last.VideoInfo.VideoURL
			coverURL = last.VideoInfo.CoverURL
		}
	}

	taskResult := &relaycommon.TaskInfo{}

	switch item.Status {
	case 0:
		taskResult.Status = model.TaskStatusSubmitted
		taskResult.Progress = taskcommon.ProgressSubmitted
	case 50:
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case 20, 60:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case 1:
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = taskcommon.ProgressComplete
		if videoURL != "" {
			if strings.HasPrefix(videoURL, "/") {
				videoURL = a.baseURL + videoURL
			}
			taskResult.Url = videoURL
		}
		if coverURL != "" {
			taskResult.RemoteUrl = coverURL
		}
	case 40:
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = "task failed"
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressSubmitted
	}

	return taskResult, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return nil
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}
