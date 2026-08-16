package anhe

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
	"github.com/tidwall/gjson"
)

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*n = 0
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
	Model              string      `json:"model"`
	Prompt             string      `json:"prompt"`
	AspectRatio        string      `json:"aspect_ratio,omitempty"`
	Ratio              string      `json:"ratio,omitempty"`
	Resolution         string      `json:"resolution,omitempty"`
	Seconds            flexibleInt `json:"seconds,omitempty"`
	Duration           flexibleInt `json:"duration,omitempty"`
	ImageURL           string      `json:"image_url,omitempty"`
	ReferenceImageURLs []string    `json:"reference_image_urls,omitempty"`
	Images             []string    `json:"images,omitempty"`
	ReferenceVideo     string      `json:"reference_video,omitempty"`
	ReferenceVideos    []string    `json:"reference_videos,omitempty"`
	Videos             []string    `json:"videos,omitempty"`
	AudioURL           string      `json:"audio_url,omitempty"`
	AudioURLs          []string    `json:"audio_urls,omitempty"`
	Audios             []string    `json:"audios,omitempty"`
	VideoConfig        videoConfig `json:"video_config,omitempty"`
	ClientBusinessID   string      `json:"client_business_id,omitempty"`
	OutputFormat       string      `json:"output_format,omitempty"`
	GenerateAudio      *bool       `json:"generate_audio,omitempty"`
	ReturnLastFrame    *bool       `json:"return_last_frame,omitempty"`
	CallbackURL        string      `json:"callback_url,omitempty"`
	TraceID            string      `json:"trace_id,omitempty"`
	Seed               *int        `json:"seed,omitempty"`
}

type imageWithRole struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type mediaWithRole struct {
	URL string `json:"url"`
}

type upstreamRequest struct {
	Model            string          `json:"model"`
	Prompt           string          `json:"prompt"`
	ClientBusinessID string          `json:"client_business_id,omitempty"`
	Duration         int             `json:"duration"`
	AspectRatio      string          `json:"aspect_ratio"`
	ImageWithRoles   []imageWithRole `json:"image_with_roles,omitempty"`
	VideoWithRoles   []mediaWithRole `json:"video_with_roles,omitempty"`
	AudioWithRoles   []mediaWithRole `json:"audio_with_roles,omitempty"`
	OutputFormat     string          `json:"output_format,omitempty"`
	GenerateAudio    *bool           `json:"generate_audio,omitempty"`
	ReturnLastFrame  *bool           `json:"return_last_frame,omitempty"`
	CallbackURL      string          `json:"callback_url,omitempty"`
	TraceID          string          `json:"trace_id,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
}

type submitResponse struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Model  string `json:"model"`
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

	duration := int(request.Seconds)
	if duration == 0 {
		duration = int(request.Duration)
	}
	if duration != -1 && (duration < 4 || duration > 30) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be -1 or between 4 and 30"), "invalid_duration", http.StatusBadRequest)
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
	images = append(images, request.ReferenceImageURLs...)
	images = append(images, request.Images...)
	videos := appendNonEmpty(nil, request.ReferenceVideo)
	videos = append(videos, request.ReferenceVideos...)
	videos = append(videos, request.Videos...)
	audios := appendNonEmpty(nil, request.AudioURL)
	audios = append(audios, request.AudioURLs...)
	audios = append(audios, request.Audios...)

	if len(images) > 30 || len(videos) > 10 || len(audios) > 10 || len(images)+len(videos)+len(audios) > 50 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validHTTPSURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
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
		aspectRatio = "adaptive"
	}

	imageRoles := make([]imageWithRole, 0, len(images))
	for index, imageURL := range images {
		role := "reference_image"
		if referenceMode == "start_frame" {
			role = "first_frame"
		} else if referenceMode == "start_end" && index == 0 {
			role = "first_frame"
		} else if referenceMode == "start_end" {
			role = "last_frame"
		}
		imageRoles = append(imageRoles, imageWithRole{URL: imageURL, Role: role})
	}

	a.body = &upstreamRequest{
		Prompt:           request.Prompt,
		ClientBusinessID: request.ClientBusinessID,
		Duration:         duration,
		AspectRatio:      aspectRatio,
		ImageWithRoles:   imageRoles,
		VideoWithRoles:   toMediaWithRoles(videos),
		AudioWithRoles:   toMediaWithRoles(audios),
		OutputFormat:     request.OutputFormat,
		GenerateAudio:    request.GenerateAudio,
		ReturnLastFrame:  request.ReturnLastFrame,
		CallbackURL:      request.CallbackURL,
		TraceID:          request.TraceID,
		Seed:             request.Seed,
	}

	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if a.body == nil {
		return nil
	}
	ratios := make(map[string]float64, 2)
	if a.body.Duration > 0 {
		ratios["seconds"] = float64(a.body.Duration)
	}
	if len(a.body.VideoWithRoles) > 0 {
		ratios["video_input"] = 2
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + videosPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = info.PublicTaskID
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
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
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return resp, err
	}
	if resp != nil && resp.StatusCode == http.StatusAccepted {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream submitResponse
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := firstNonEmpty(upstream.ID, upstream.TaskID)
	if upstreamID == "" {
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
	status := strings.ToLower(strings.TrimSpace(gjson.GetBytes(respBody, "status").String()))
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "in_progress", "processing", "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressFromBody(respBody, taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded":
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = extractResultURL(respBody)
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = extractErrorMessage(respBody)
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		if reason := extractErrorMessage(respBody); status == "" && reason != "" {
			result.Status = model.TaskStatusFailure
			result.Progress = taskcommon.ProgressComplete
			result.Reason = reason
		} else {
			result.Status = model.TaskStatusInProgress
			result.Progress = progressFromBody(respBody, taskcommon.ProgressInProgress)
		}
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
	case "16:9", "9:16", "4:3", "1:1", "3:4", "21:9", "adaptive":
		return true
	default:
		return false
	}
}

func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func appendNonEmpty(values []string, value string) []string {
	if strings.TrimSpace(value) != "" {
		return append(values, strings.TrimSpace(value))
	}
	return values
}

func toMediaWithRoles(values []string) []mediaWithRole {
	items := make([]mediaWithRole, 0, len(values))
	for _, value := range values {
		items = append(items, mediaWithRole{URL: value})
	}
	return items
}

func extractResultURL(body []byte) string {
	for _, path := range []string{
		"content.video_url",
		"result_url",
		"video_url",
		"url",
		"metadata.url",
		"metadata.result_url",
		"result.url",
		"result.video_url",
		"data.video_url",
		"data.result_url",
		"data.url",
	} {
		value := strings.TrimSpace(gjson.GetBytes(body, path).String())
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			return value
		}
	}
	return ""
}

func extractErrorMessage(body []byte) string {
	for _, path := range []string{"error.message", "error", "message", "detail"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return strings.TrimSpace(value.String())
		}
	}
	return ""
}

func progressFromBody(body []byte, fallback string) string {
	progress := gjson.GetBytes(body, "progress").Int()
	if progress <= 0 {
		return fallback
	}
	if progress > 100 {
		progress = 100
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
