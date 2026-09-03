package manju

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

type standardVideoRequest struct {
	Model              string       `json:"model"`
	Prompt             string       `json:"prompt"`
	Duration           *flexibleInt `json:"duration,omitempty"`
	Seconds            *flexibleInt `json:"seconds,omitempty"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	Images             []string     `json:"images,omitempty"`
	ImageURLs          []string     `json:"image_urls,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	ReferenceImages    []string     `json:"reference_images,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
	VideoURLs          []string     `json:"video_urls,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	ReferenceAudios    []string     `json:"reference_audios,omitempty"`
	Audios             []string     `json:"audios,omitempty"`
	VideoConfig        videoConfig  `json:"video_config,omitempty"`
	PromptExtend       *bool        `json:"prompt_extend,omitempty"`
}

type mediaItem struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type upstreamRequest struct {
	Model        string      `json:"model"`
	Prompt       string      `json:"prompt"`
	Media        []mediaItem `json:"media,omitempty"`
	Resolution   string      `json:"resolution"`
	Ratio        string      `json:"ratio"`
	Duration     int         `json:"duration"`
	PromptExtend *bool       `json:"prompt_extend,omitempty"`
}

type responseData struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	TaskStatus  string `json:"task_status"`
	Progress    any    `json:"progress,omitempty"`
	VideoURL    string `json:"video_url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Message     string `json:"message,omitempty"`
	Error       any    `json:"error,omitempty"`
}

type responseEnvelope struct {
	ID          string       `json:"id"`
	TaskID      string       `json:"task_id"`
	Status      string       `json:"status"`
	TaskStatus  string       `json:"task_status"`
	Progress    any          `json:"progress,omitempty"`
	VideoURL    string       `json:"video_url,omitempty"`
	DownloadURL string       `json:"download_url,omitempty"`
	Message     string       `json:"message,omitempty"`
	Error       any          `json:"error,omitempty"`
	Output      responseData `json:"output,omitempty"`
	Data        responseData `json:"data,omitempty"`
	Result      responseData `json:"result,omitempty"`
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

	upstreamModel := strings.TrimSpace(info.UpstreamModelName)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(request.Model)
	}
	mode, ok := modelMode(upstreamModel)
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported manju model: %s", upstreamModel), "unsupported_model", http.StatusBadRequest)
	}

	if request.Duration == nil && request.Seconds == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration or seconds is required"), "invalid_duration", http.StatusBadRequest)
	}
	duration := 0
	if request.Duration != nil {
		duration = int(*request.Duration)
	}
	if request.Seconds != nil {
		seconds := int(*request.Seconds)
		if request.Duration != nil && duration != seconds {
			return service.TaskErrorWrapperLocal(fmt.Errorf("duration and seconds conflict"), "invalid_duration", http.StatusBadRequest)
		}
		duration = seconds
	}
	if duration < 2 || duration > 30 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 2 and 30"), "invalid_duration", http.StatusBadRequest)
	}

	ratio := strings.TrimSpace(request.AspectRatio)
	ratioAlias := strings.TrimSpace(request.Ratio)
	if ratio != "" && ratioAlias != "" && ratio != ratioAlias {
		return service.TaskErrorWrapperLocal(fmt.Errorf("aspect_ratio and ratio conflict"), "invalid_aspect_ratio", http.StatusBadRequest)
	}
	if ratio == "" {
		ratio = ratioAlias
	}
	if !validRatio(ratio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect_ratio: %s", ratio), "invalid_aspect_ratio", http.StatusBadRequest)
	}

	resolution := strings.ToUpper(strings.TrimSpace(request.Resolution))
	if !validResolution(resolution) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("resolution must be 480P, 720P, or 1080P"), "invalid_resolution", http.StatusBadRequest)
	}
	resolutionPricing, err := model.LoadResolutionPricing(info.OriginModelName)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "resolution_price_invalid", http.StatusBadRequest)
	}
	if resolutionPricing != nil {
		price, priceErr := model.GetResolutionPriceFromSetting(info.OriginModelName, resolution, resolutionPricing)
		if priceErr != nil {
			return service.TaskErrorWrapperLocal(priceErr, "resolution_price_invalid", http.StatusBadRequest)
		}
		a.resolutionPrice = price
		a.useResolutionPricing = true
	}

	images := appendNonEmpty(nil, request.ImageURL)
	images = appendNormalized(images, request.Images)
	images = appendNormalized(images, request.ImageURLs)
	images = appendNormalized(images, request.ReferenceImageURLs)
	images = appendNormalized(images, request.ReferenceImages)
	videos := appendNonEmpty(nil, request.ReferenceVideo)
	videos = appendNormalized(videos, request.ReferenceVideos)
	videos = appendNormalized(videos, request.Videos)
	videos = appendNormalized(videos, request.VideoURLs)
	audios := appendNonEmpty(nil, request.AudioURL)
	audios = appendNormalized(audios, request.AudioURLs)
	audios = appendNormalized(audios, request.ReferenceAudios)
	audios = appendNormalized(audios, request.Audios)
	if len(images) > 10 || len(videos) > 5 || len(audios) > 5 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	if len(audios) > 0 && len(images) == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("audio references require at least one image"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validPublicHTTPSURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}

	referenceMode := strings.ToLower(strings.TrimSpace(request.VideoConfig.ReferenceMode))
	if referenceMode == "" {
		referenceMode = "auto"
	}
	if referenceMode == "start_end" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("start_end is not supported by manju mode-specific models"), "unsupported_reference_mode", http.StatusBadRequest)
	}
	if referenceMode != "auto" && referenceMode != "start_frame" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported reference_mode: %s", referenceMode), "invalid_reference_mode", http.StatusBadRequest)
	}

	media := make([]mediaItem, 0, len(images)+len(videos)+len(audios))
	switch mode {
	case "t2v":
		if len(images)+len(videos)+len(audios) > 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("t2v models do not accept reference media"), "unsupported_reference_media", http.StatusBadRequest)
		}
		if referenceMode != "auto" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("t2v models do not support reference_mode"), "invalid_reference_mode", http.StatusBadRequest)
		}
		info.Action = constant.TaskActionTextGenerate
	case "i2v":
		if len(images) != 1 || len(videos) > 0 || len(audios) > 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("i2v models require exactly one image and no video or audio"), "invalid_reference_count", http.StatusBadRequest)
		}
		media = append(media, mediaItem{Type: "first_frame", URL: images[0]})
		info.Action = constant.TaskActionGenerate
	case "r2v":
		if referenceMode != "auto" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("r2v models only support automatic reference roles"), "invalid_reference_mode", http.StatusBadRequest)
		}
		if len(images)+len(videos)+len(audios) == 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("r2v models require at least one reference item"), "invalid_reference_count", http.StatusBadRequest)
		}
		for _, itemURL := range images {
			media = append(media, mediaItem{Type: "reference_image", URL: itemURL})
		}
		for _, itemURL := range videos {
			media = append(media, mediaItem{Type: "reference_video", URL: itemURL})
		}
		for _, itemURL := range audios {
			media = append(media, mediaItem{Type: "audio", URL: itemURL})
		}
		info.Action = constant.TaskActionGenerate
	}

	a.body = &upstreamRequest{
		Model:        upstreamModel,
		Prompt:       prompt,
		Media:        media,
		Resolution:   resolution,
		Ratio:        ratio,
		Duration:     duration,
		PromptExtend: request.PromptExtend,
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if a.body == nil || a.body.Duration < 2 || a.body.Duration > 30 {
		return nil
	}
	if a.useResolutionPricing {
		quota, clamp := common.QuotaFromFloatChecked(
			a.resolutionPrice * common.QuotaPerUnit * info.PriceData.GroupRatioInfo.GroupRatio * float64(a.body.Duration),
		)
		info.PriceData.ModelPrice = a.resolutionPrice
		info.PriceData.UsePrice = true
		info.PriceData.Quota = quota
		if clamp != nil && info.QuotaClamp == nil {
			info.QuotaClamp = clamp
		}
		c.Set(string(constant.ContextKeyTaskPropsExtra), map[string]interface{}{
			"resolution": a.body.Resolution,
			"duration":   a.body.Duration,
		})
		return nil
	}
	return map[string]float64{
		"seconds":    float64(a.body.Duration),
		"resolution": resolutionRatio(a.body.Model, a.body.Resolution),
	}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return providerV1URL(a.baseURL, generationsPath), nil
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

	var upstream responseEnvelope
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "decode manju submit response"), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	upstreamID := firstNonEmpty(upstream.ID, upstream.TaskID, upstream.Output.ID, upstream.Output.TaskID, upstream.Data.ID, upstream.Data.TaskID, upstream.Result.ID, upstream.Result.TaskID)
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
		video.Seconds = strconv.Itoa(a.body.Duration)
		video.Size = a.body.Resolution
	}
	c.JSON(http.StatusOK, video)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	endpoint := providerV1URL(baseURL, tasksPath) + "/" + url.PathEscape(strings.TrimSpace(taskID))
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
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response responseEnvelope
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal manju task result failed")
	}
	status := strings.ToLower(firstNonEmpty(response.Status, response.TaskStatus, response.Output.Status, response.Output.TaskStatus, response.Data.Status, response.Data.TaskStatus, response.Result.Status, response.Result.TaskStatus))
	progress := firstProgress(response.Progress, response.Output.Progress, response.Data.Progress, response.Result.Progress)
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending", "waiting", "submitted":
		result.Status = model.TaskStatusQueued
		result.Progress = progressString(progress, taskcommon.ProgressQueued)
	case "running", "processing", "in_progress", "in-progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(progress, taskcommon.ProgressInProgress)
	case "succeeded", "success", "completed", "complete":
		result.Url = firstValidResultURL(response.VideoURL, response.DownloadURL, response.Output.VideoURL, response.Output.DownloadURL, response.Data.VideoURL, response.Data.DownloadURL, response.Result.VideoURL, response.Result.DownloadURL)
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
		result.Reason = responseErrorMessage(response)
	default:
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		if reason := responseErrorMessage(response); reason != "upstream request failed" {
			result.Reason = reason
		} else {
			result.Reason = fmt.Sprintf("unknown upstream task status: %s", status)
		}
	}
	return result, nil
}

func (*TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (*TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (*TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
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

func modelMode(modelName string) (string, bool) {
	for _, candidate := range ModelList {
		if modelName == candidate {
			parts := strings.Split(candidate, "-")
			return parts[len(parts)-1], true
		}
	}
	return "", false
}

func validRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "4:3", "3:4", "1:1", "21:9":
		return true
	default:
		return false
	}
}

func validResolution(value string) bool {
	return value == "480P" || value == "720P" || value == "1080P"
}

func resolutionRatio(modelName, resolution string) float64 {
	if strings.HasPrefix(modelName, "wan3.0-prime-") {
		switch resolution {
		case "720P":
			return 16.0 / 15.0
		case "1080P":
			return 4.0 / 3.0
		default:
			return 1
		}
	}
	switch resolution {
	case "720P":
		return 2
	case "1080P":
		return 3.2
	default:
		return 1
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

func firstValidResultURL(values ...string) string {
	for _, value := range values {
		if validPublicHTTPSURL(value) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerV1URL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL + path
	}
	return baseURL + "/v1" + path
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

func firstProgress(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(typed), "%"))
			if err == nil {
				return parsed
			}
		}
	}
	return 0
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

func responseErrorMessage(response responseEnvelope) string {
	message := firstNonEmpty(
		response.Message,
		errorMessage(response.Error),
		response.Output.Message,
		errorMessage(response.Output.Error),
		response.Data.Message,
		errorMessage(response.Data.Error),
		response.Result.Message,
		errorMessage(response.Result.Error),
	)
	message = strings.TrimSpace(common.MaskSensitiveInfo(message))
	if message == "" {
		return "upstream request failed"
	}
	runes := []rune(message)
	if len(runes) > 500 {
		message = string(runes[:500])
	}
	return message
}

func errorMessage(upstream any) string {
	switch value := upstream.(type) {
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"message", "msg", "detail", "error"} {
			if message, ok := value[key].(string); ok && strings.TrimSpace(message) != "" {
				return message
			}
		}
	}
	return ""
}
