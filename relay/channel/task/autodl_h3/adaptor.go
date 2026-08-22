package autodl_h3

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
	Resolution         string       `json:"resolution,omitempty"`
	Ratio              string       `json:"ratio,omitempty"`
	AspectRatio        string       `json:"aspect_ratio,omitempty"`
	Size               string       `json:"size,omitempty"`
	Seed               *flexibleInt `json:"seed,omitempty"`
	Image              string       `json:"image,omitempty"`
	ImageURL           string       `json:"image_url,omitempty"`
	Images             []string     `json:"images,omitempty"`
	ImageURLs          []string     `json:"image_urls,omitempty"`
	ReferenceImageURLs []string     `json:"reference_image_urls,omitempty"`
	ReferenceImages    []string     `json:"reference_images,omitempty"`
	References         []string     `json:"references,omitempty"`
	ReferenceURLs      []string     `json:"reference_urls,omitempty"`
	Audio              string       `json:"audio,omitempty"`
	AudioURL           string       `json:"audio_url,omitempty"`
	Audios             []string     `json:"audios,omitempty"`
	AudioURLs          []string     `json:"audio_urls,omitempty"`
	ReferenceAudios    []string     `json:"reference_audios,omitempty"`
	Video              string       `json:"video,omitempty"`
	VideoURL           string       `json:"video_url,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
	VideoURLs          []string     `json:"video_urls,omitempty"`
	ReferenceVideo     string       `json:"reference_video,omitempty"`
	ReferenceVideos    []string     `json:"reference_videos,omitempty"`
}

type submitResponse struct {
	Code    string     `json:"code"`
	Data    submitData `json:"data"`
	Msg     string     `json:"msg"`
	Message string     `json:"message"`
	Error   any        `json:"error"`
}

type submitData struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   any    `json:"error"`
}

type taskResponse struct {
	Code    string   `json:"code"`
	Data    taskData `json:"data"`
	Msg     string   `json:"msg"`
	Message string   `json:"message"`
	Error   any      `json:"error"`
}

type taskData struct {
	Status  string       `json:"status"`
	Message string       `json:"message"`
	Error   any          `json:"error"`
	Results []taskResult `json:"results"`
}

type taskResult struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	FileType   string `json:"file_type"`
	OutputType string `json:"output_type"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey   string
	baseURL  string
	body     map[string]any
	duration int
	workflow string
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
	if utf8.RuneCountInString(prompt) < 1 || utf8.RuneCountInString(prompt) > 10000 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt length must be between 1 and 10000 characters"), "invalid_prompt", http.StatusBadRequest)
	}

	duration := 5
	if request.Duration != nil {
		duration = int(*request.Duration)
	} else if request.Seconds != nil {
		duration = int(*request.Seconds)
	}
	if duration < 1 || duration > 15 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 1 and 15"), "invalid_duration", http.StatusBadRequest)
	}

	resolution, ok := normalizeResolution(request.Resolution, firstNonEmpty(request.Ratio, request.AspectRatio, request.Size))
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported resolution or aspect ratio"), "invalid_resolution", http.StatusBadRequest)
	}

	images := firstNonEmptySlice(request.Images, request.ImageURLs)
	if len(images) == 0 {
		images = appendNonEmpty(nil, firstNonEmpty(request.Image, request.ImageURL))
		images = appendNormalized(images, request.ReferenceImageURLs)
		if len(images) == 0 {
			images = firstNonEmptySlice(request.ReferenceImages, request.References, request.ReferenceURLs)
		}
	}
	audios := firstNonEmptySlice(request.Audios, request.AudioURLs)
	if len(audios) == 0 {
		audios = appendNonEmpty(nil, firstNonEmpty(request.Audio, request.AudioURL))
		audios = appendNormalized(audios, request.ReferenceAudios)
	}
	videos := firstNonEmptySlice(request.Videos, request.VideoURLs)
	if len(videos) == 0 {
		videos = appendNonEmpty(nil, firstNonEmpty(request.Video, request.VideoURL, request.ReferenceVideo))
		videos = appendNormalized(videos, request.ReferenceVideos)
	}

	for _, rawURL := range append(append(append([]string{}, images...), audios...), videos...) {
		if !validReferenceURL(rawURL) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media must use a public HTTPS URL"), "invalid_reference_url", http.StatusBadRequest)
		}
	}

	workflow := strings.TrimSpace(info.UpstreamModelName)
	switch workflow {
	case TextWorkflowID:
		if len(images)+len(audios)+len(videos) > 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("text workflow does not support reference media"), "unsupported_reference_media", http.StatusBadRequest)
		}
	case ReferenceWorkflowID:
		if len(images) > 9 || len(audios) > 3 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference workflow supports at most 9 images and 3 audios"), "invalid_reference_count", http.StatusBadRequest)
		}
		if len(videos) > 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference workflow does not support reference videos"), "unsupported_reference_media", http.StatusBadRequest)
		}
	default:
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported AutoDL H3 workflow"), "unsupported_model", http.StatusBadRequest)
	}

	body := map[string]any{
		"prompt":     prompt,
		"duration":   duration,
		"resolution": resolution,
	}
	if workflow == ReferenceWorkflowID {
		if request.Seed != nil {
			body["seed"] = int(*request.Seed)
		}
		for index, rawURL := range images {
			body[fmt.Sprintf("ref_image_%d", index)] = rawURL
		}
		for index, rawURL := range audios {
			body[fmt.Sprintf("ref_audio_%d", index)] = rawURL
		}
	}

	a.body = body
	a.duration = duration
	a.workflow = workflow
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if a.duration < 1 || a.duration > 15 {
		return nil
	}
	return map[string]float64{"seconds": float64(a.duration)}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	workflow := strings.TrimSpace(info.UpstreamModelName)
	if workflow == "" {
		workflow = a.workflow
	}
	if workflow == "" {
		return "", fmt.Errorf("workflow is required")
	}
	return strings.TrimRight(a.baseURL, "/") + workflowPath + "/" + url.PathEscape(workflow), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	if strings.TrimSpace(info.UpstreamModelName) != a.workflow {
		return nil, fmt.Errorf("mapped workflow changed after validation")
	}
	data, err := common.Marshal(a.body)
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
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "decode AutoDL H3 submit response"), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if !strings.EqualFold(strings.TrimSpace(upstream.Code), "Success") {
		message := responseErrorMessage(upstream.Data.Message, upstream.Data.Error, upstream.Msg, upstream.Message, upstream.Error)
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream submit failed: %s", message), "submit_failed", http.StatusBadGateway)
	}
	upstreamID := strings.TrimSpace(upstream.Data.TaskID)
	if upstreamID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusBadGateway)
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
	endpoint := strings.TrimRight(baseURL, "/") + workflowPath + "/result/" + url.PathEscape(strings.TrimSpace(taskID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", key)
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
		return nil, errors.Wrap(err, "unmarshal AutoDL H3 task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0}
	if !strings.EqualFold(strings.TrimSpace(response.Code), "Success") {
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseErrorMessage(response.Data.Message, response.Data.Error, response.Msg, response.Message, response.Error)
		return result, nil
	}

	switch strings.ToLower(strings.TrimSpace(response.Data.Status)) {
	case "queued", "pending", "waiting", "created", "submitted":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress", "in-progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "success", "completed", "complete", "succeeded":
		result.Url = selectVideoResultURL(response.Data.Results)
		result.Progress = taskcommon.ProgressComplete
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed task is missing valid video output URL"
		} else {
			result.Status = model.TaskStatusSuccess
		}
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseErrorMessage(response.Data.Message, response.Data.Error, response.Msg, response.Message, response.Error)
	default:
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = "unknown upstream task status"
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

func normalizeResolution(rawResolution, rawOrientation string) (string, bool) {
	resolution := strings.ToLower(strings.TrimSpace(rawResolution))
	resolution = strings.ReplaceAll(resolution, " ", "")
	switch resolution {
	case "480p竖", "480p横", "768p竖", "768p横":
		if rawOrientation != "" {
			orientation, ok := normalizeOrientation(rawOrientation)
			if !ok || !strings.HasSuffix(resolution, orientation) {
				return "", false
			}
		}
		return resolution, true
	case "":
		resolution = "768p"
	case "480p":
	case "720p", "768p":
		resolution = "768p"
	default:
		return "", false
	}
	orientation := "竖"
	if strings.TrimSpace(rawOrientation) != "" {
		var ok bool
		orientation, ok = normalizeOrientation(rawOrientation)
		if !ok {
			return "", false
		}
	}
	return resolution + orientation, true
}

func normalizeOrientation(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "16:9", "4:3", "3:2", "landscape", "horizontal", "横", "横屏":
		return "横", true
	case "9:16", "3:4", "2:3", "portrait", "vertical", "竖", "竖屏":
		return "竖", true
	}
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return "", false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width == height {
		return "", false
	}
	if width > height {
		return "横", true
	}
	return "竖", true
}

func validReferenceURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func validResultURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func selectVideoResultURL(results []taskResult) string {
	for priority := 0; priority < 3; priority++ {
		for _, item := range results {
			if !validResultURL(item.URL) {
				continue
			}
			isVideo := strings.EqualFold(strings.TrimSpace(item.Type), "video")
			isMP4 := strings.EqualFold(strings.TrimSpace(item.FileType), "mp4")
			isOutput := strings.EqualFold(strings.TrimSpace(item.OutputType), "output")
			if priority == 0 && !(isVideo && isMP4 && isOutput) {
				continue
			}
			if priority == 1 && !(isVideo && isMP4) {
				continue
			}
			if priority == 2 && !isVideo {
				continue
			}
			return strings.TrimSpace(item.URL)
		}
	}
	return ""
}

func responseErrorMessage(values ...any) string {
	for _, value := range values {
		var text string
		switch typed := value.(type) {
		case string:
			text = typed
		case map[string]any:
			for _, key := range []string{"message", "msg", "error", "detail"} {
				if nested, ok := typed[key].(string); ok && strings.TrimSpace(nested) != "" {
					text = nested
					break
				}
			}
		}
		text = strings.TrimSpace(common.MaskSensitiveInfo(text))
		if text != "" {
			runes := []rune(text)
			if len(runes) > 500 {
				text = string(runes[:500])
			}
			return text
		}
	}
	return "upstream request failed"
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
