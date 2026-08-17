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

type standardVideoRequest struct {
	Model              string       `json:"model"`
	Prompt             string       `json:"prompt"`
	Duration           *flexibleInt `json:"duration,omitempty"`
	Seconds            *flexibleInt `json:"seconds,omitempty"`
	Resolution         string       `json:"resolution,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Size               string       `json:"size,omitempty"`
	Image              string       `json:"image,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	Images             []string     `json:"images,omitempty"`
	ImageURLs          []string     `json:"image_urls,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	ReferenceImages    []string     `json:"reference_images,omitempty"`
	References         []string     `json:"references,omitempty"`
	ReferenceURLs      []string     `json:"reference_urls,omitempty"`
	Video              string       `json:"video,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
	VideoURLs          []string     `json:"video_urls,omitempty"`
	Audio              string       `json:"audio,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	ReferenceAudios    []string     `json:"reference_audios,omitempty"`
	Audios             []string     `json:"audios,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	GenerateAudio      *bool        `json:"generate_audio,omitempty"`
}

type upstreamRequest struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	Duration        *int     `json:"duration,omitempty"`
	Resolution      string   `json:"resolution,omitempty"`
	Ratio           string   `json:"ratio"`
	Images          []string `json:"images,omitempty"`
	ReferenceVideos []string `json:"reference_videos,omitempty"`
	ReferenceAudios []string `json:"reference_audios,omitempty"`
	GenerateAudio   *bool    `json:"generate_audio,omitempty"`
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

	var duration *int
	if request.Duration != nil {
		value := int(*request.Duration)
		duration = &value
	} else if request.Seconds != nil {
		value := int(*request.Seconds)
		duration = &value
	}
	if duration != nil && *duration != -1 && (*duration <= 0 || *duration > relaycommon.MaxTaskDurationSeconds) {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration must be -1 or between 1 and %d", relaycommon.MaxTaskDurationSeconds),
			"invalid_duration",
			http.StatusBadRequest,
		)
	}

	ratioInput := firstNonEmpty(request.Ratio, request.AspectRatio, request.Size)
	if ratioInput == "" {
		ratioInput = "16:9"
	}
	ratio, ok := normalizeRatio(ratioInput)
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported ratio: %s", ratioInput), "invalid_aspect_ratio", http.StatusBadRequest)
	}

	resolution, ok := normalizeResolution(request.Resolution)
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported resolution: %s", request.Resolution), "invalid_resolution", http.StatusBadRequest)
	}

	images := firstNonEmptySlice(request.Images, request.ImageURLs, request.ReferenceImages, request.References, request.ReferenceURLs)
	if len(images) == 0 {
		images = appendNonEmpty(nil, firstNonEmpty(request.Image, request.ImageURL))
		images = append(images, firstNonEmptySlice(request.ReferenceImageURLs)...)
	}
	videos := firstNonEmptySlice(request.ReferenceVideos, request.Videos, request.VideoURLs)
	if len(videos) == 0 {
		videos = appendNonEmpty(nil, firstNonEmpty(request.Video, request.ReferenceVideo))
	}
	audios := firstNonEmptySlice(request.ReferenceAudios, request.Audios, request.AudioURLs)
	if len(audios) == 0 {
		audios = appendNonEmpty(nil, firstNonEmpty(request.Audio, request.AudioURL))
	}

	if len(images) > 30 || len(videos) > 10 || len(audios) > 10 || len(images)+len(videos)+len(audios) > 50 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
	}
	for _, rawURL := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validHTTPSURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}

	a.body = &upstreamRequest{
		Prompt:          request.Prompt,
		Duration:        duration,
		Resolution:      resolution,
		Ratio:           ratio,
		Images:          images,
		ReferenceVideos: videos,
		ReferenceAudios: audios,
		GenerateAudio:   request.GenerateAudio,
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
	if a.body.Duration != nil && *a.body.Duration > 0 {
		ratios["seconds"] = float64(*a.body.Duration)
	}
	if len(a.body.ReferenceVideos) > 0 {
		ratios["video_input"] = 2
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return buildVideoGenerationsURL(a.baseURL), nil
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
	endpoint := buildVideoGenerationsURL(baseURL) + "/" + url.PathEscape(taskID)
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
		result.Url = extractResultURL(respBody)
		if result.Url == "" {
			result.Status = model.TaskStatusInProgress
			result.Progress = "99%"
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
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

func validRatio(value string) bool {
	switch value {
	case "16:9", "9:16", "4:3", "1:1", "3:4", "21:9", "adaptive":
		return true
	default:
		return false
	}
}

func normalizeRatio(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if validRatio(value) {
		return value, true
	}
	if ratio, ok := map[string]string{
		"1792x1024": "16:9",
		"1024x1792": "9:16",
		"1280x720":  "16:9",
		"720x1280":  "9:16",
		"1920x1080": "16:9",
		"1080x1920": "9:16",
		"1024x1024": "1:1",
	}[value]; ok {
		return ratio, true
	}
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return "", false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return "", false
	}
	divisor := greatestCommonDivisor(width, height)
	ratio := fmt.Sprintf("%d:%d", width/divisor, height/divisor)
	return ratio, validRatio(ratio)
}

func normalizeResolution(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "", true
	}
	switch value {
	case "480P", "720P", "1080P", "4K":
		return value, true
	default:
		return "", false
	}
}

func greatestCommonDivisor(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
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

func firstNonEmptySlice(groups ...[]string) []string {
	for _, group := range groups {
		values := make([]string, 0, len(group))
		for _, value := range group {
			values = appendNonEmpty(values, value)
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func buildVideoGenerationsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL + strings.TrimPrefix(videoGenerationsPath, "/v1")
	}
	return baseURL + videoGenerationsPath
}

func extractResultURL(body []byte) string {
	for _, path := range []string{"result_url", "url"} {
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
