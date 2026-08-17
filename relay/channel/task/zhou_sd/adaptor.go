package zhou_sd

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
	"github.com/pkg/errors"
)

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		value, err := strconv.Unquote(raw)
		if err != nil {
			return err
		}
		raw = strings.TrimSpace(value)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("must be an integer: %w", err)
	}
	*n = flexibleInt(value)
	return nil
}

type videoConfig struct {
	ReferenceMode string `json:"reference_mode,omitempty"`
}

type standardVideoRequest struct {
	Model              string       `json:"model"`
	Prompt             string       `json:"prompt"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	Seconds            *flexibleInt `json:"seconds,omitempty"`
	Duration           *flexibleInt `json:"duration,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	VideoConfig        videoConfig  `json:"video_config,omitempty"`
}

type upstreamRequest struct {
	Model         string   `json:"model"`
	Prompt        string   `json:"prompt"`
	Duration      int      `json:"duration"`
	AspectRatio   string   `json:"aspect_ratio"`
	ImageURL      string   `json:"image_url,omitempty"`
	ExtraImages   []string `json:"extra_images,omitempty"`
	ExtraVideos   []string `json:"extra_videos,omitempty"`
	ExtraAudios   []string `json:"extra_audios,omitempty"`
	StartImageURL string   `json:"start_image_url,omitempty"`
	EndImageURL   string   `json:"end_image_url,omitempty"`
}

type upstreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type submitResponse struct {
	TaskID string         `json:"task_id"`
	Status string         `json:"status"`
	Model  string         `json:"model"`
	Error  *upstreamError `json:"error,omitempty"`
}

type taskResponse struct {
	TaskID   string         `json:"task_id"`
	Status   string         `json:"status"`
	Progress int            `json:"progress"`
	VideoURL string         `json:"video_url,omitempty"`
	Cost     float64        `json:"cost,omitempty"`
	Error    *upstreamError `json:"error,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
	body    *upstreamRequest
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var request standardVideoRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	duration := 6
	if request.Seconds != nil {
		duration = int(*request.Seconds)
	} else if request.Duration != nil {
		duration = int(*request.Duration)
	}
	if duration < 1 || duration > 30 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 1 and 30"), "invalid_duration", http.StatusBadRequest)
	}

	aspectRatio := strings.TrimSpace(request.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = strings.TrimSpace(request.Ratio)
	}
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	if !validAspectRatio(aspectRatio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect_ratio: %s", aspectRatio), "invalid_aspect_ratio", http.StatusBadRequest)
	}

	images := appendNonEmpty(nil, request.ImageURL)
	images = appendNormalized(images, request.ReferenceImageURLs)
	videos := appendNonEmpty(nil, request.ReferenceVideo)
	videos = appendNormalized(videos, request.ReferenceVideos)
	audios := appendNonEmpty(nil, request.AudioURL)
	audios = appendNormalized(audios, request.AudioURLs)

	if len(images) > 30 || len(videos) > 10 || len(audios) > 10 || len(images)+len(videos)+len(audios) > 50 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validMediaURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTP or HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}
	if len(audios) > 0 && len(images) == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("audio references require at least one image"), "invalid_reference_count", http.StatusBadRequest)
	}

	referenceMode := strings.ToLower(strings.TrimSpace(request.VideoConfig.ReferenceMode))
	if referenceMode == "" {
		referenceMode = "auto"
	}
	if referenceMode != "auto" && referenceMode != "start_frame" && referenceMode != "start_end" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported reference_mode: %s", referenceMode), "invalid_reference_mode", http.StatusBadRequest)
	}
	if referenceMode == "start_frame" && len(images) != 1 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("start_frame requires exactly one image"), "invalid_reference_count", http.StatusBadRequest)
	}
	if referenceMode == "start_end" {
		if len(images) != 2 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("start_end requires exactly two images"), "invalid_reference_count", http.StatusBadRequest)
		}
		if len(videos) > 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("start_end cannot be combined with reference videos"), "invalid_reference_mode", http.StatusBadRequest)
		}
	}

	body := &upstreamRequest{
		Prompt:      strings.TrimSpace(request.Prompt),
		Duration:    duration,
		AspectRatio: aspectRatio,
		ExtraVideos: videos,
		ExtraAudios: audios,
	}
	switch referenceMode {
	case "start_frame":
		body.StartImageURL = images[0]
	case "start_end":
		body.StartImageURL = images[0]
		body.EndImageURL = images[1]
	default:
		if strings.TrimSpace(request.ImageURL) != "" {
			body.ImageURL = strings.TrimSpace(request.ImageURL)
		}
		body.ExtraImages = appendNormalized(nil, request.ReferenceImageURLs)
	}
	a.body = body

	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if a.body == nil || a.body.Duration <= 0 {
		return nil
	}
	return map[string]float64{"seconds": float64(a.body.Duration)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + videosPath, nil
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
	body := *a.body
	body.Model = info.UpstreamModelName
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
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream submitResponse
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "decode zhou_sd submit response"), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := strings.TrimSpace(upstream.TaskID)
	if upstreamID == "" {
		if upstream.Error != nil && strings.TrimSpace(upstream.Error.Message) != "" {
			return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream submit failed: %s", strings.TrimSpace(upstream.Error.Message)), "submit_failed", http.StatusBadRequest)
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
	}

	video := relaydto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Object = "video"
	video.Model = info.OriginModelName
	video.Status = relaydto.VideoStatusQueued
	video.CreatedAt = time.Now().Unix()
	c.JSON(http.StatusOK, video)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	endpoint := strings.TrimRight(baseURL, "/") + videosPath + "/" + url.PathEscape(taskID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
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

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response taskResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "pending":
		result.Status = model.TaskStatusQueued
		result.Progress = progressString(response.Progress, taskcommon.ProgressQueued)
	case "processing":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(response.Progress, taskcommon.ProgressInProgress)
	case "completed":
		if !validMediaURL(response.VideoURL) {
			result.Status = model.TaskStatusFailure
			result.Progress = taskcommon.ProgressComplete
			result.Reason = "completed task is missing a valid video_url"
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = strings.TrimSpace(response.VideoURL)
	case "failed":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		if response.Error != nil {
			result.Reason = strings.TrimSpace(response.Error.Message)
		}
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = fmt.Sprintf("unknown upstream task status: %s", strings.TrimSpace(response.Status))
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	video := originTask.ToOpenAIVideo()
	video.TaskID = originTask.TaskID
	if originTask.Status == model.TaskStatusFailure {
		video.Error = &relaydto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, "task failed"),
			Code:    "upstream_error",
		}
	}
	return common.Marshal(video)
}

func validAspectRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "1:1", "4:3", "3:4", "21:9", "3:2", "2:3":
		return true
	default:
		return false
	}
}

func validMediaURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func appendNonEmpty(values []string, value string) []string {
	if strings.TrimSpace(value) != "" {
		return append(values, strings.TrimSpace(value))
	}
	return values
}

func appendNormalized(values []string, items []string) []string {
	for _, item := range items {
		values = append(values, strings.TrimSpace(item))
	}
	return values
}

func progressString(progress int, fallback string) string {
	if progress <= 0 {
		return fallback
	}
	if progress > 99 {
		progress = 99
	}
	return fmt.Sprintf("%d%%", progress)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
