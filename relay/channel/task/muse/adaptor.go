package muse

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type mediaItem struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
}

type requestPayload struct {
	Model    string      `json:"model"`
	Prompt   string      `json:"prompt"`
	Ratio    string      `json:"ratio"`
	Duration int         `json:"duration"`
	Images   []mediaItem `json:"images"`
	Audios   []mediaItem `json:"audios,omitempty"`
	Videos   []mediaItem `json:"videos,omitempty"`
}

type submitResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data int64  `json:"data"`
}

type taskStatusResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		URL        string `json:"url,omitempty"`
		FailReason string `json:"failReason,omitempty"`
		CreateTime string `json:"createTime"`
		UpdateTime string `json:"updateTime"`
	} `json:"data"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, "generate")
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v2/videos", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	var images []mediaItem
	for i, imgURL := range req.Images {
		images = append(images, mediaItem{
			Tag: fmt.Sprintf("image%d", i+1),
			URL: imgURL,
		})
	}
	if len(images) == 0 {
		images = []mediaItem{}
	}

	body := &requestPayload{
		Model:    taskcommon.DefaultString(info.UpstreamModelName, "MUSE_SEE_DANCE_2_0_REAL_PERSON"),
		Prompt:   req.Prompt,
		Ratio:    taskcommon.DefaultString(req.Size, "1:1"),
		Duration: taskcommon.DefaultInt(req.Duration, 5),
		Images:   images,
	}

	if err := taskcommon.UnmarshalMetadata(req.Metadata, body); err != nil {
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

	var mResp submitResponse
	if err = common.Unmarshal(responseBody, &mResp); err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if mResp.Code != 0 {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", mResp.Msg), "task_failed", http.StatusBadRequest)
		return
	}

	ov := relaydto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return fmt.Sprintf("%d", mResp.Data), responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	url := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resp taskStatusResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response body")
	}

	info := &relaycommon.TaskInfo{}
	switch resp.Data.Status {
	case "RUNNING":
		info.Status = model.TaskStatusInProgress
	case "COMPLETED":
		info.Status = model.TaskStatusSuccess
		info.Url = resp.Data.URL
	case "FAILED":
		info.Status = model.TaskStatusFailure
		info.Reason = resp.Data.FailReason
	default:
		info.Status = model.TaskStatusSubmitted
	}
	return info, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var resp taskStatusResponse
	if err := common.Unmarshal(originTask.Data, &resp); err != nil {
		return nil, errors.Wrap(err, "unmarshal muse task data failed")
	}

	ov := relaydto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt

	if resp.Data.URL != "" {
		ov.SetMetadata("url", resp.Data.URL)
	}
	if resp.Data.FailReason != "" {
		ov.Error = &relaydto.OpenAIVideoError{Message: resp.Data.FailReason}
	}
	return common.Marshal(ov)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"MUSE_SEE_DANCE_2_0_REAL_PERSON", "MUSE_SEE_DANCE_2_0_REAL_PERSON_COMPANY"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return "MuseAI"
}
