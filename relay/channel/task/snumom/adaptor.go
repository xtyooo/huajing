package snumom

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
	"github.com/tidwall/gjson"
)

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if err := common.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("duration must be an integer")
		}
		raw = strings.TrimSpace(value)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("duration must be an integer")
	}
	*n = flexibleInt(value)
	return nil
}

type standardRequest struct {
	Model              string                  `json:"model"`
	Prompt             string                  `json:"prompt"`
	Duration           *flexibleInt            `json:"duration,omitempty"`
	Seconds            *flexibleInt            `json:"seconds,omitempty"`
	Resolution         string                  `json:"resolution,omitempty"`
	Size               string                  `json:"size,omitempty"`
	AspectRatio        string                  `json:"aspect_ratio,omitempty"`
	Ratio              string                  `json:"ratio,omitempty"`
	Image              string                  `json:"image,omitempty"`
	ImageURL           string                  `json:"image_url,omitempty"`
	InputReference     string                  `json:"input_reference,omitempty"`
	Images             taskcommon.MediaURLList `json:"images,omitempty"`
	ImageURLs          taskcommon.MediaURLList `json:"image_urls,omitempty"`
	ReferenceImages    taskcommon.MediaURLList `json:"reference_images,omitempty"`
	ReferenceImageURLs taskcommon.MediaURLList `json:"reference_image_urls,omitempty"`
	References         taskcommon.MediaURLList `json:"references,omitempty"`
	ReferenceURLs      taskcommon.MediaURLList `json:"reference_urls,omitempty"`
	Video              string                  `json:"video,omitempty"`
	VideoURL           string                  `json:"video_url,omitempty"`
	ReferenceVideo     string                  `json:"reference_video,omitempty"`
	Videos             taskcommon.MediaURLList `json:"videos,omitempty"`
	VideoURLs          taskcommon.MediaURLList `json:"video_urls,omitempty"`
	ReferenceVideos    taskcommon.MediaURLList `json:"reference_videos,omitempty"`
	Audio              string                  `json:"audio,omitempty"`
	AudioURL           string                  `json:"audio_url,omitempty"`
	Audios             taskcommon.MediaURLList `json:"audios,omitempty"`
	AudioURLs          taskcommon.MediaURLList `json:"audio_urls,omitempty"`
	ReferenceAudios    taskcommon.MediaURLList `json:"reference_audios,omitempty"`
	GenerateAudio      *bool                   `json:"generate_audio,omitempty"`
	FirstFrame         string                  `json:"first_frame,omitempty"`
	LastFrame          string                  `json:"last_frame,omitempty"`
	FirstFrameURL      string                  `json:"first_frame_url,omitempty"`
	LastFrameURL       string                  `json:"last_frame_url,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey, baseURL string
	body            map[string]any
	doubaoEndpoint  bool
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	// The provider documents a Jimeng-compatible endpoint for the Doubao token
	// models. Other models use the standard OpenAI-compatible /v1/videos API.
	a.doubaoEndpoint = strings.HasPrefix(strings.ToLower(strings.TrimSpace(info.UpstreamModelName)), "doubao-seedance-")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.body = nil
	var request standardRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return invalid("invalid_json", "invalid video request JSON")
	}
	if strings.TrimSpace(request.Model) == "" {
		return invalid("missing_model", "model is required")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return invalid("invalid_prompt", "prompt is required")
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
		return invalid("invalid_duration", fmt.Sprintf("duration must be -1 or between 1 and %d seconds", relaycommon.MaxTaskDurationSeconds))
	}

	images := mediaValues(firstNonEmpty(request.ImageURL, request.Image, request.InputReference), request.Images, request.ImageURLs, request.ReferenceImages, request.ReferenceImageURLs, request.References, request.ReferenceURLs)
	videos := mediaValues(firstNonEmpty(request.VideoURL, request.Video, request.ReferenceVideo), request.Videos, request.VideoURLs, request.ReferenceVideos)
	audios := mediaValues(firstNonEmpty(request.AudioURL, request.Audio), request.Audios, request.AudioURLs, request.ReferenceAudios)
	for _, value := range append(append(append([]string{}, images...), videos...), audios...) {
		if !validMediaURL(value) {
			return invalid("invalid_reference_url", "reference materials must use public HTTPS URLs or data URLs")
		}
	}
	if len(images)+len(videos)+len(audios) > 100 {
		return invalid("invalid_reference_count", "too many reference materials")
	}

	modelName := firstNonEmpty(info.UpstreamModelName, request.Model)
	a.doubaoEndpoint = strings.HasPrefix(strings.ToLower(modelName), "doubao-seedance-")
	a.body = buildUpstreamBody(modelName, request.Prompt, duration, request.Resolution, request.Size, firstNonEmpty(request.AspectRatio, request.Ratio), images, videos, audios, request.GenerateAudio, a.doubaoEndpoint)
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func invalid(code, message string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	// Snumom exposes both per-call and seconds/token products. The selected
	// pricing mode is controlled by the administrator's model configuration;
	// the adapter therefore avoids guessing a billing multiplier from a model
	// name. Resolution-per-second pricing is handled by the common billing path.
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	if a.doubaoEndpoint {
		return providerURL(a.baseURL, "/v1/video/generations"), nil
	}
	return providerURL(a.baseURL, "/v1/videos"), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = info.PublicTaskID
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	body := cloneMap(a.body)
	body["model"] = firstNonEmpty(info.UpstreamModelName, fmt.Sprint(body["model"]))
	data, err := common.Marshal(body)
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

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	id := extractString(data, "id", "task_id", "data.id", "data.task_id", "data.data.id", "data.data.task_id", "result.id", "result.task_id")
	if id == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = relaydto.VideoStatusQueued, time.Now().Unix()
	c.JSON(http.StatusOK, video)
	return id, data, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	// Polling receives only the private task ID from the shared service. Try the
	// standard endpoint first and fall back to the Doubao generations endpoint
	// on 404, which keeps both protocols usable without leaking model metadata
	// into the task polling interface.
	paths := []string{"/v1/videos", "/v1/video/generations"}
	for index, endpoint := range paths {
		req, err := http.NewRequest(http.MethodGet, providerURL(baseURL, endpoint)+"/"+url.PathEscape(strings.TrimSpace(id)), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if index == 0 && len(paths) > 1 && resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("snumom task query failed")
}

func (a *TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	status := strings.ToLower(extractString(body, "data.data.status", "data.status", "result.status", "status"))
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending", "submitted", "not_start", "waiting":
		result.Status, result.Progress = model.TaskStatusQueued, taskcommon.ProgressQueued
	case "in_progress", "processing", "running", "generating":
		result.Status, result.Progress = model.TaskStatusInProgress, progress(body, taskcommon.ProgressInProgress)
	case "completed", "success", "succeeded", "done":
		result.Url = extractResultURL(body)
		result.Status, result.Progress = model.TaskStatusSuccess, taskcommon.ProgressComplete
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		result.Status, result.Progress, result.Reason = model.TaskStatusFailure, taskcommon.ProgressComplete, extractString(body, "error.message", "error", "message", "detail")
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		result.Status, result.Progress = model.TaskStatusInProgress, progress(body, taskcommon.ProgressInProgress)
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	video.TaskID = task.TaskID
	if task.Status == model.TaskStatusFailure {
		video.Error = &relaydto.OpenAIVideoError{Message: firstNonEmpty(task.FailReason, "task failed"), Code: "upstream_error"}
	}
	return common.Marshal(video)
}

func (*TaskAdaptor) GetModelList() []string { return ModelList }
func (*TaskAdaptor) GetChannelName() string { return ChannelName }

func buildUpstreamBody(modelName, prompt string, duration *int, resolution, size, ratio string, images, videos, audios []string, generateAudio *bool, doubao bool) map[string]any {
	body := map[string]any{"model": modelName, "prompt": prompt}
	if duration != nil {
		if doubao {
			body["duration"] = *duration
		} else if strings.HasPrefix(strings.ToLower(modelName), "wan3.") {
			body["seconds"] = strconv.Itoa(*duration)
		} else {
			body["duration"] = *duration
		}
	}
	if resolution != "" {
		body["resolution"] = resolution
	} else if size != "" {
		body["size"] = size
	}
	if ratio != "" {
		body["aspect_ratio"] = ratio
	}
	if doubao {
		content := []map[string]any{{"type": "text", "text": prompt}}
		for _, value := range images {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": value}, "role": "reference_image"})
		}
		for _, value := range videos {
			content = append(content, map[string]any{"type": "video_url", "video_url": map[string]string{"url": value}, "role": "reference_video"})
		}
		for _, value := range audios {
			content = append(content, map[string]any{"type": "audio_url", "audio_url": map[string]string{"url": value}, "role": "reference_audio"})
		}
		metadata := map[string]any{"content": content, "resolution": firstNonEmpty(resolution, "720p"), "ratio": firstNonEmpty(ratio, "16:9")}
		body["metadata"] = metadata
		delete(body, "resolution")
		delete(body, "size")
		delete(body, "aspect_ratio")
		if generateAudio != nil {
			metadata["generate_audio"] = *generateAudio
		}
		return body
	}
	if len(images) > 0 {
		if strings.EqualFold(modelName, "sd-2.5") {
			body["images"] = images
		} else {
			body["reference_images"] = images
		}
	}
	if len(videos) > 0 {
		body["reference_videos"] = videos
	}
	if len(audios) > 0 {
		body["reference_audios"] = audios
	}
	if generateAudio != nil {
		body["generate_audio"] = *generateAudio
	}
	return body
}

func extractResultURL(body []byte) string {
	paths := []string{"content.video_url", "data.data.content.video_url", "data.content.video_url", "result_url", "video_url", "url", "metadata.url", "metadata.result_url", "result.url", "result.video_url", "data.video_url", "data.result_url", "data.url", "data.metadata.url", "data.metadata.result_url", "data.data.video_url", "data.data.result_url", "data.data.url", "data.data.metadata.url", "data.data.metadata.result_url", "result.metadata.url", "result.metadata.result_url"}
	for _, path := range paths {
		value := strings.TrimSpace(gjson.GetBytes(body, path).String())
		if validMediaURL(value) {
			return value
		}
	}
	return ""
}

func extractTaskStatus(body []byte) string {
	return strings.ToLower(extractString(body, "data.status", "status", "result.status", "data.data.status"))
}

func extractString(body []byte, paths ...string) string {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return strings.TrimSpace(value.String())
		}
	}
	return ""
}

func progress(body []byte, fallback string) string {
	for _, path := range []string{"data.progress", "progress", "result.progress", "data.data.progress"} {
		value := gjson.GetBytes(body, path)
		if value.Exists() {
			raw := strings.TrimSpace(value.String())
			raw = strings.TrimSuffix(raw, "%")
			if parsed, err := strconv.Atoi(raw); err == nil {
				if parsed >= 100 {
					parsed = 99
				}
				if parsed > 0 {
					return fmt.Sprintf("%d%%", parsed)
				}
			}
		}
	}
	return fallback
}

func providerURL(base, endpoint string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		return base + strings.TrimPrefix(endpoint, "/v1")
	}
	return base + endpoint
}

func mediaValues(scalar string, groups ...[]string) []string {
	values := make([]string, 0, 1)
	if strings.TrimSpace(scalar) != "" {
		values = append(values, strings.TrimSpace(scalar))
	}
	for _, group := range groups {
		for _, value := range group {
			if strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	}
	return values
}

func validMediaURL(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "data:") {
		return len(value) <= 64<<20
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
