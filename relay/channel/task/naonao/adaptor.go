package naonao

import (
	"bytes"
	"fmt"
	"io"
	"net"
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

type videoConfig struct {
	ReferenceMode string `json:"reference_mode,omitempty"`
}

type standardRequest struct {
	Model              string       `json:"model"`
	Prompt             string       `json:"prompt"`
	Duration           *flexibleInt `json:"duration,omitempty"`
	Seconds            *flexibleInt `json:"seconds,omitempty"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	Image              string       `json:"image,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	InputReference     string       `json:"input_reference,omitempty"`
	Images             []string     `json:"images,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	RefImages          []string     `json:"ref_images,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	RefAudio           string       `json:"ref_audio,omitempty"`
	InputAudio         string       `json:"input_audio,omitempty"`
	RefAudios          []string     `json:"ref_audios,omitempty"`
	ReferenceAudios    []string     `json:"reference_audios,omitempty"`
	Audios             []string     `json:"audios,omitempty"`
	ReferenceMode      string       `json:"reference_mode,omitempty"`
	VideoConfig        videoConfig  `json:"video_config,omitempty"`
	Watermark          *bool        `json:"watermark,omitempty"`
	GenerateAudio      *bool        `json:"generate_audio,omitempty"`
}

type urlObject struct {
	URL string `json:"url"`
}

type contentItem struct {
	Type     string     `json:"type"`
	Text     string     `json:"text,omitempty"`
	ImageURL *urlObject `json:"image_url,omitempty"`
	AudioURL *urlObject `json:"audio_url,omitempty"`
}

type upstreamRequest struct {
	Model         string        `json:"model"`
	Content       []contentItem `json:"content"`
	Ratio         string        `json:"ratio"`
	Resolution    string        `json:"resolution"`
	Seconds       string        `json:"seconds"`
	Watermark     *bool         `json:"watermark,omitempty"`
	GenerateAudio *bool         `json:"generate_audio,omitempty"`
}

type responseMetadata struct {
	URL       string `json:"url,omitempty"`
	ResultURL string `json:"result_url,omitempty"`
}

type responseTask struct {
	ID          string           `json:"id"`
	TaskID      string           `json:"task_id"`
	Model       string           `json:"model,omitempty"`
	Status      string           `json:"status"`
	Progress    any              `json:"progress,omitempty"`
	CreatedAt   any              `json:"created_at,omitempty"`
	CompletedAt any              `json:"completed_at,omitempty"`
	URL         string           `json:"url,omitempty"`
	VideoURL    string           `json:"video_url,omitempty"`
	Metadata    responseMetadata `json:"metadata,omitempty"`
	Message     string           `json:"message,omitempty"`
	Error       any              `json:"error,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey               string
	baseURL              string
	body                 *upstreamRequest
	resolutionPrice      float64
	useResolutionPricing bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.resolutionPrice = 0
	a.useResolutionPricing = false
	var request standardRequest
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
	upstreamModel := firstNonEmpty(info.UpstreamModelName, request.Model)

	duration := 5
	if request.Duration != nil {
		duration = int(*request.Duration)
	} else if request.Seconds != nil {
		duration = int(*request.Seconds)
	}
	if duration < 5 || duration > 30 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 5 and 30"), "invalid_duration", http.StatusBadRequest)
	}
	ratio := strings.TrimSpace(request.AspectRatio)
	if ratioAlias := strings.TrimSpace(request.Ratio); ratio != "" && ratioAlias != "" && ratio != ratioAlias {
		return service.TaskErrorWrapperLocal(fmt.Errorf("aspect_ratio and ratio conflict"), "invalid_aspect_ratio", http.StatusBadRequest)
	} else if ratio == "" {
		ratio = ratioAlias
	}
	if ratio == "" {
		ratio = "16:9"
	}
	if !validRatio(ratio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect_ratio: %s", ratio), "invalid_aspect_ratio", http.StatusBadRequest)
	}

	videos := appendNonEmpty(nil, request.ReferenceVideo)
	videos = appendNormalized(videos, request.ReferenceVideos)
	videos = appendNormalized(videos, request.Videos)
	if len(videos) > 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("naonao does not support reference videos"), "unsupported_reference_media", http.StatusBadRequest)
	}
	referenceMode := strings.ToLower(firstNonEmpty(request.VideoConfig.ReferenceMode, request.ReferenceMode, "auto"))
	if referenceMode == "start_frame" || referenceMode == "start_end" || referenceMode == "frame" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("naonao does not support frame reference modes"), "unsupported_reference_mode", http.StatusBadRequest)
	}
	if referenceMode != "auto" && referenceMode != "image" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported reference_mode: %s", referenceMode), "invalid_reference_mode", http.StatusBadRequest)
	}

	images := appendNonEmpty(nil, firstNonEmpty(request.ImageURL, request.Image, request.InputReference))
	images = appendNormalized(images, request.ReferenceImageURLs)
	images = appendNormalized(images, request.Images)
	images = appendNormalized(images, request.RefImages)
	audios := appendNonEmpty(nil, firstNonEmpty(request.AudioURL, request.RefAudio, request.InputAudio))
	audios = appendNormalized(audios, request.AudioURLs)
	audios = appendNormalized(audios, request.RefAudios)
	audios = appendNormalized(audios, request.ReferenceAudios)
	audios = appendNormalized(audios, request.Audios)
	if len(images) > 10 || len(audios) > 5 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append([]string{}, images...), audios...) {
		if !validPublicHTTPSURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}

	price, enabled, err := taskcommon.ResolutionPrice(info.OriginModelName, "720P")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "resolution_price_invalid", http.StatusBadRequest)
	}
	a.resolutionPrice = price
	a.useResolutionPricing = enabled

	content := make([]contentItem, 0, 1+len(images)+len(audios))
	content = append(content, contentItem{Type: "text", Text: prompt})
	for _, itemURL := range images {
		content = append(content, contentItem{Type: "image_url", ImageURL: &urlObject{URL: itemURL}})
	}
	for _, itemURL := range audios {
		content = append(content, contentItem{Type: "audio_url", AudioURL: &urlObject{URL: itemURL}})
	}
	a.body = &upstreamRequest{
		Model:         upstreamModel,
		Content:       content,
		Ratio:         ratio,
		Resolution:    "720p",
		Seconds:       strconv.Itoa(duration),
		Watermark:     request.Watermark,
		GenerateAudio: request.GenerateAudio,
	}
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if a.body == nil {
		return nil
	}
	duration, err := strconv.Atoi(a.body.Seconds)
	if err != nil || duration < 5 || duration > 30 {
		return nil
	}
	if a.useResolutionPricing {
		taskcommon.ApplyResolutionPerSecondBilling(c, info, "720P", duration, a.resolutionPrice)
		return nil
	}
	return map[string]float64{"seconds": float64(duration)}
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
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return resp, err
	}
	if resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()
	var upstream responseTask
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "decode naonao submit response"), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := firstNonEmpty(upstream.ID, upstream.TaskID)
	if upstreamID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream submit failed: %s", responseErrorMessage(upstream)), "submit_failed", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Model = info.OriginModelName
	video.Status = relaydto.VideoStatusQueued
	video.CreatedAt = time.Now().Unix()
	if a.body != nil {
		video.Seconds = a.body.Seconds
		video.Size = "720p"
	}
	c.JSON(http.StatusOK, video)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, providerVideosURL(baseURL)+"/"+url.PathEscape(strings.TrimSpace(taskID)), nil)
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
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func (*TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response responseTask
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal naonao task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "submitted":
		result.Status = model.TaskStatusQueued
		result.Progress = progressString(progressValue(response.Progress), taskcommon.ProgressQueued)
	case "processing", "in_progress", "in-progress", "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(progressValue(response.Progress), taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded":
		result.Url = firstValidURL(response.URL, response.VideoURL, response.Metadata.URL, response.Metadata.ResultURL)
		result.Progress = taskcommon.ProgressComplete
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed task is missing a valid video URL"
		} else {
			result.Status = model.TaskStatusSuccess
		}
	case "failed", "failure", "error", "cancelled", "canceled":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseErrorMessage(response)
	default:
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = fmt.Sprintf("unknown upstream task status: %s", strings.TrimSpace(response.Status))
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

func validRatio(value string) bool {
	switch value {
	case "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	default:
		return false
	}
}

func validPublicHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		return false
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
	}
	return true
}

func firstValidURL(values ...string) string {
	for _, value := range values {
		if validPublicHTTPSURL(value) {
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
	return baseURL + "/v1/videos"
}

func appendNonEmpty(values []string, value string) []string {
	if strings.TrimSpace(value) != "" {
		return append(values, strings.TrimSpace(value))
	}
	return values
}

func appendNormalized(values []string, items []string) []string {
	for _, item := range items {
		values = appendNonEmpty(values, item)
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func progressValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case string:
		progress, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(typed), "%"))
		return progress
	default:
		return 0
	}
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

func responseErrorMessage(response responseTask) string {
	message := firstNonEmpty(response.Message, errorMessage(response.Error), "upstream request failed")
	message = strings.TrimSpace(common.MaskSensitiveInfo(message))
	runes := []rune(message)
	if len(runes) > 500 {
		message = string(runes[:500])
	}
	return message
}

func errorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"message", "msg", "detail", "error"} {
			if message, ok := typed[key].(string); ok && strings.TrimSpace(message) != "" {
				return message
			}
		}
	}
	return ""
}
