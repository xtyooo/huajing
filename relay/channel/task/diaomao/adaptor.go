package diaomao

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type standardVideoRequest struct {
	Model              string       `json:"model"`
	Prompt             string       `json:"prompt"`
	Duration           *flexibleInt `json:"duration,omitempty"`
	Seconds            *flexibleInt `json:"seconds,omitempty"`
	Size               string       `json:"size,omitempty"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	Image              string       `json:"image,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	Images             []string     `json:"images,omitempty"`
	ImageURLs          []string     `json:"image_urls,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	ReferenceImages    []string     `json:"reference_images,omitempty"`
	References         []string     `json:"references,omitempty"`
	ReferenceURLs      []string     `json:"reference_urls,omitempty"`
	ImageRefs          []string     `json:"image_refs,omitempty"`
	Video              string       `json:"video,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
	VideoURLs          []string     `json:"video_urls,omitempty"`
	VideoRefs          []string     `json:"video_refs,omitempty"`
	Audio              string       `json:"audio,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	ReferenceAudios    []string     `json:"reference_audios,omitempty"`
	Audios             []string     `json:"audios,omitempty"`
	AudioRefs          []string     `json:"audio_refs,omitempty"`
	ComplianceEnabled  *bool        `json:"compliance_enabled,omitempty"`
	ComplianceMode     string       `json:"compliance_mode,omitempty"`
}

type upstreamRequest struct {
	Model             string   `json:"model"`
	Prompt            string   `json:"prompt"`
	Duration          int      `json:"duration"`
	Size              string   `json:"size,omitempty"`
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	ImageRefs         []string `json:"image_refs,omitempty"`
	VideoRefs         []string `json:"video_refs,omitempty"`
	AudioRefs         []string `json:"audio_refs,omitempty"`
	ComplianceEnabled *bool    `json:"compliance_enabled,omitempty"`
	ComplianceMode    string   `json:"compliance_mode,omitempty"`
}

type upstreamError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

type submitResponse struct {
	ID     string         `json:"id"`
	TaskID string         `json:"task_id"`
	Status string         `json:"status"`
	Error  *upstreamError `json:"error,omitempty"`
}

type taskResponse struct {
	ID       string         `json:"id"`
	TaskID   string         `json:"task_id"`
	Status   string         `json:"status"`
	Progress int            `json:"progress"`
	VideoURL string         `json:"video_url,omitempty"`
	URL      string         `json:"url,omitempty"`
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
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > 10000 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt must contain between 1 and 10000 characters"), "invalid_prompt", http.StatusBadRequest)
	}

	duration := 8
	if request.Duration != nil {
		duration = int(*request.Duration)
	} else if request.Seconds != nil {
		duration = int(*request.Seconds)
	}
	if duration < 5 || duration > 15 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 5 and 15"), "invalid_duration", http.StatusBadRequest)
	}

	resolution := strings.ToLower(strings.TrimSpace(request.Resolution))
	if resolution != "" && resolution != "720p" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("resolution must be 720p"), "invalid_resolution", http.StatusBadRequest)
	}

	size := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(request.Size), " ", ""))
	aspectRatio := strings.TrimSpace(request.AspectRatio)
	ratioAlias := strings.TrimSpace(request.Ratio)
	if aspectRatio != "" && ratioAlias != "" && aspectRatio != ratioAlias {
		return service.TaskErrorWrapperLocal(fmt.Errorf("aspect_ratio and ratio conflict"), "invalid_aspect_ratio", http.StatusBadRequest)
	}
	if aspectRatio == "" {
		aspectRatio = ratioAlias
	}
	if size != "" && aspectRatio != "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("size and aspect_ratio are mutually exclusive"), "invalid_size", http.StatusBadRequest)
	}
	if size != "" && !validSize(size) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported size: %s", size), "invalid_size", http.StatusBadRequest)
	}
	if aspectRatio != "" && !validAspectRatio(aspectRatio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect_ratio: %s", aspectRatio), "invalid_aspect_ratio", http.StatusBadRequest)
	}
	if size == "" && aspectRatio == "" {
		aspectRatio = "16:9"
	}

	images := normalizedReferences(request.ImageRefs)
	if len(images) == 0 {
		images = appendReference(images, request.Image)
		images = appendReference(images, request.ImageURL)
		images = appendReferences(images, request.Images)
		images = appendReferences(images, request.ImageURLs)
		images = appendReferences(images, request.ReferenceImageURLs)
		images = appendReferences(images, request.ReferenceImages)
		images = appendReferences(images, request.References)
		images = appendReferences(images, request.ReferenceURLs)
	}
	videos := normalizedReferences(request.VideoRefs)
	if len(videos) == 0 {
		videos = appendReference(videos, request.Video)
		videos = appendReference(videos, request.ReferenceVideo)
		videos = appendReferences(videos, request.ReferenceVideos)
		videos = appendReferences(videos, request.Videos)
		videos = appendReferences(videos, request.VideoURLs)
	}
	audios := normalizedReferences(request.AudioRefs)
	if len(audios) == 0 {
		audios = appendReference(audios, request.Audio)
		audios = appendReference(audios, request.AudioURL)
		audios = appendReferences(audios, request.AudioURLs)
		audios = appendReferences(audios, request.ReferenceAudios)
		audios = appendReferences(audios, request.Audios)
	}
	if len(images) > 9 || len(videos) > 3 || len(audios) > 3 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validReferenceURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}
	if len(audios) > 0 && len(images)+len(videos) == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("audio_refs require at least one image or video reference"), "invalid_reference_count", http.StatusBadRequest)
	}

	complianceMode := strings.ToLower(strings.TrimSpace(request.ComplianceMode))
	if complianceMode != "" && !validComplianceMode(complianceMode) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported compliance_mode: %s", complianceMode), "invalid_compliance_mode", http.StatusBadRequest)
	}
	a.body = &upstreamRequest{
		Prompt:            prompt,
		Duration:          duration,
		Size:              size,
		AspectRatio:       aspectRatio,
		ImageRefs:         images,
		VideoRefs:         videos,
		AudioRefs:         audios,
		ComplianceEnabled: request.ComplianceEnabled,
		ComplianceMode:    complianceMode,
	}
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if a.body == nil || a.body.Duration < 5 || a.body.Duration > 15 {
		return nil
	}
	return map[string]float64{"seconds": float64(a.body.Duration)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return providerVideosURL(a.baseURL), nil
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

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream submitResponse
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "decode diaomao submit response"), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := firstNonEmpty(upstream.ID, upstream.TaskID)
	if upstreamID == "" {
		message := "task id is empty"
		if upstream.Error != nil && strings.TrimSpace(upstream.Error.Message) != "" {
			message = strings.TrimSpace(upstream.Error.Message)
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream submit failed: %s", message), "submit_failed", http.StatusBadGateway)
	}

	video := relaydto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Model = info.OriginModelName
	video.CreatedAt = time.Now().Unix()
	if a.body != nil {
		video.Seconds = strconv.Itoa(a.body.Duration)
		video.Size = firstNonEmpty(a.body.Size, a.body.AspectRatio)
	}
	c.JSON(http.StatusOK, video)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	endpoint := providerVideosURL(baseURL) + "/" + url.PathEscape(strings.TrimSpace(taskID))
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response taskResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal diaomao task result failed")
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "waiting", "submitted":
		result.Status = model.TaskStatusQueued
		result.Progress = progressString(response.Progress, taskcommon.ProgressQueued)
	case "processing", "in_progress", "in-progress", "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(response.Progress, taskcommon.ProgressInProgress)
	case "completed", "complete", "success", "succeeded":
		result.Url = firstValidResultURL(response.VideoURL, response.URL)
		result.Progress = taskcommon.ProgressComplete
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed task is missing a valid video URL"
		} else {
			result.Status = model.TaskStatusSuccess
		}
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
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
		if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
			result.Reason = strings.TrimSpace(response.Error.Message)
		} else {
			result.Reason = fmt.Sprintf("unknown upstream task status: %s", strings.TrimSpace(response.Status))
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
	case "16:9", "9:16", "1:1", "21:9", "3:4", "4:3":
		return true
	default:
		return false
	}
}

func validSize(value string) bool {
	if validAspectRatio(value) {
		return true
	}
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return false
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 10000 || height > 10000 {
		return false
	}
	if width != 720 && height != 720 {
		return false
	}
	for _, ratio := range [][2]int{{16, 9}, {9, 16}, {1, 1}, {21, 9}, {3, 4}, {4, 3}} {
		if width*ratio[1] == height*ratio[0] {
			return true
		}
	}
	return false
}

func validComplianceMode(value string) bool {
	switch value {
	case "colored-pencil", "watercolor", "fishnet", "grid":
		return true
	default:
		return false
	}
}

func validReferenceURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func validResultURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func firstValidResultURL(values ...string) string {
	for _, value := range values {
		if validResultURL(value) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerVideosURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL + "/videos"
	}
	return baseURL + videosPath
}

func appendReference(values []string, value string) []string {
	if strings.TrimSpace(value) != "" {
		values = append(values, strings.TrimSpace(value))
	}
	return values
}

func appendReferences(values []string, items []string) []string {
	for _, item := range items {
		values = appendReference(values, item)
	}
	return values
}

func normalizedReferences(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return appendReferences(make([]string, 0, len(items)), items)
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
