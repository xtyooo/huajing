package longxia6

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey, baseURL string
	body            *upstreamRequest
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey, a.baseURL = info.ApiKey, info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.body = nil
	var request standardRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return invalid("invalid_json", "invalid video request JSON or asset structure")
	}
	if strings.TrimSpace(request.Model) == "" {
		return invalid("missing_model", "model is required")
	}
	upstreamModel := firstNonEmpty(info.UpstreamModelName, request.Model)
	limits := modelCapabilities(upstreamModel)
	if strings.TrimSpace(request.Prompt) == "" || utf8.RuneCountInString(request.Prompt) > limits.promptMax {
		return invalid("invalid_prompt", fmt.Sprintf("prompt must contain 1–%d characters", limits.promptMax))
	}
	duration := 8
	if request.Duration != nil {
		duration = int(*request.Duration)
	}
	if request.Seconds != nil {
		if request.Duration != nil && duration != int(*request.Seconds) {
			return invalid("invalid_duration", "seconds and duration conflict")
		}
		duration = int(*request.Seconds)
	}
	if duration < limits.minDuration || duration > limits.maxDuration {
		return invalid("invalid_duration", fmt.Sprintf("duration must be %d–%d seconds", limits.minDuration, limits.maxDuration))
	}
	ratio, err := normalizeRatio(request)
	if err != nil {
		return invalid("invalid_aspect_ratio", err.Error())
	}
	if !limits.wideRatios && ratio != "9:16" && ratio != "16:9" {
		return invalid("invalid_aspect_ratio", "this model supports only 9:16 and 16:9")
	}
	mode := strings.ToLower(firstNonEmpty(request.VideoConfig.ReferenceMode, request.ReferenceMode, "auto"))
	if mode != "auto" && mode != "image" && mode != "reference" || firstNonEmpty(request.FirstFrame, request.LastFrame, request.FirstFrameURL, request.LastFrameURL) != "" {
		return invalid("unsupported_reference_mode", "first/last frame inputs are not supported by this provider")
	}
	assets, err := normalizeAssets(request)
	if err != nil {
		return invalid("invalid_reference_media", err.Error())
	}
	if err := validateReferences(request.Prompt, assets, limits); err != nil {
		return invalid("invalid_reference_media", err.Error())
	}
	pricing, err := model.LoadResolutionPricing(info.OriginModelName)
	if err != nil || pricing != nil {
		return invalid("unsupported_pricing", "longxia6 requires model base pricing; resolution-per-second pricing is not supported")
	}
	a.body = &upstreamRequest{Model: upstreamModel, Prompt: request.Prompt, Duration: duration, Size: ratio, Assets: assets}
	info.Action = constant.TaskActionTextGenerate
	if len(assets) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func invalid(code, message string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if a.body != nil && strings.HasSuffix(a.body.Model, "-PerSecond") {
		return map[string]float64{"seconds": float64(a.body.Duration)}
	}
	return nil
}
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return videosURL(a.baseURL), nil
}
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}
func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	if info.UpstreamModelName != "" && info.UpstreamModelName != a.body.Model {
		return nil, fmt.Errorf("model mapping changed after validation")
	}
	data, err := common.Marshal(a.body)
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

type responseTask struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	Progress    any    `json:"progress"`
	CreatedAt   any    `json:"created_at"`
	CompletedAt any    `json:"completed_at"`
	Data        []struct {
		URL       string `json:"url"`
		MediaType string `json:"media_type"`
	} `json:"data"`
	Error   any    `json:"error"`
	Message string `json:"message"`
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("read longxia6 submit response failed"), "read_response_body_failed", http.StatusBadGateway)
	}
	var upstream responseTask
	if err := common.Unmarshal(data, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("invalid longxia6 submit response"), "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	id := firstNonEmpty(upstream.ID, upstream.TaskID)
	if id == "" || failureStatus(upstream.Status) {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", failureReason(upstream)), "submit_failed", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = relaydto.VideoStatusQueued, time.Now().Unix()
	if a.body != nil {
		video.Seconds, video.Size = strconv.Itoa(a.body.Duration), a.body.Size
	}
	c.JSON(http.StatusOK, video)
	return id, data, nil
}
func (*TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, videosURL(baseURL)+"/"+url.PathEscape(strings.TrimSpace(id)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("create task query client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("longxia6 task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}
func (*TaskAdaptor) ParseTaskResult(data []byte) (*relaycommon.TaskInfo, error) {
	var response responseTask
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("invalid longxia6 task response")
	}
	result := &relaycommon.TaskInfo{}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "submitted", "waiting":
		result.Status, result.Progress = model.TaskStatusQueued, taskcommon.ProgressQueued
	case "completed", "success", "succeeded":
		result.Status, result.Progress = model.TaskStatusSuccess, taskcommon.ProgressComplete
		fallback := ""
		for _, item := range response.Data {
			if !publicHTTPSURL(item.URL) {
				continue
			}
			mime := strings.ToLower(strings.TrimSpace(strings.SplitN(item.MediaType, ";", 2)[0]))
			if strings.HasPrefix(mime, "video/") {
				result.Url = strings.TrimSpace(item.URL)
				break
			}
			parsed, _ := url.Parse(item.URL)
			ext := strings.ToLower(path.Ext(parsed.Path))
			if (mime == "" || mime == "application/octet-stream") && (ext == ".mp4" || ext == ".webm" || ext == ".mov") && fallback == "" {
				fallback = strings.TrimSpace(item.URL)
			}
		}
		if result.Url == "" {
			result.Url = fallback
		}
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed longxia6 task is missing a valid video URL"
		}
	case "failed", "failure", "error", "cancelled", "canceled":
		result.Status, result.Progress, result.Reason = model.TaskStatusFailure, taskcommon.ProgressComplete, failureReason(response)
	default:
		result.Status, result.Progress = model.TaskStatusInProgress, taskcommon.ProgressInProgress
	}
	if result.Status == model.TaskStatusInProgress || result.Status == model.TaskStatusQueued {
		var progress float64
		switch value := response.Progress.(type) {
		case float64:
			progress = value
		case string:
			progress, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
		}
		if progress > 0 {
			if progress > 99 {
				progress = 99
			}
			result.Progress = fmt.Sprintf("%d%%", int(progress))
		}
	}
	return result, nil
}
func (*TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	video.TaskID = task.TaskID
	if task.Status == model.TaskStatusFailure {
		video.Error = &relaydto.OpenAIVideoError{Message: firstNonEmpty(task.FailReason, "task failed"), Code: "upstream_error"}
	}
	return common.Marshal(video)
}
func (*TaskAdaptor) GetModelList() []string { return ModelList }
func (*TaskAdaptor) GetChannelName() string { return ChannelName }
func videosURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base + "/videos"
	}
	return base + "/v1/videos"
}
func failureStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "failure", "error", "cancelled", "canceled":
		return true
	}
	return false
}
func failureReason(response responseTask) string {
	message := response.Message
	if message == "" {
		switch value := response.Error.(type) {
		case string:
			message = value
		case map[string]any:
			message, _ = value["message"].(string)
		}
	}
	runes := []rune(common.MaskSensitiveInfo(firstNonEmpty(message, "longxia6 upstream request failed")))
	if len(runes) > 500 {
		runes = runes[:500]
	}
	return string(runes)
}
