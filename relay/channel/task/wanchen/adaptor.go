package wanchen

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

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, `"`) {
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
	Ratio              string                  `json:"ratio,omitempty"`
	AspectRatio        string                  `json:"aspect_ratio,omitempty"`
	Resolution         string                  `json:"resolution,omitempty"`
	Size               string                  `json:"size,omitempty"`
	Image              string                  `json:"image,omitempty"`
	ImageURL           string                  `json:"image_url,omitempty"`
	InputReference     string                  `json:"input_reference,omitempty"`
	Images             taskcommon.MediaURLList `json:"images,omitempty"`
	ImageURLs          taskcommon.MediaURLList `json:"image_urls,omitempty"`
	ReferenceImageURLs taskcommon.MediaURLList `json:"reference_image_urls,omitempty"`
	ReferenceImages    taskcommon.MediaURLList `json:"reference_images,omitempty"`
	RefImages          taskcommon.MediaURLList `json:"ref_images,omitempty"`
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
	RefAudio           string                  `json:"ref_audio,omitempty"`
	InputAudio         string                  `json:"input_audio,omitempty"`
	Audios             taskcommon.MediaURLList `json:"audios,omitempty"`
	AudioURLs          taskcommon.MediaURLList `json:"audio_urls,omitempty"`
	ReferenceAudios    taskcommon.MediaURLList `json:"reference_audios,omitempty"`
	RefAudios          taskcommon.MediaURLList `json:"ref_audios,omitempty"`
	ReferenceMode      string                  `json:"reference_mode,omitempty"`
	FirstFrame         string                  `json:"first_frame,omitempty"`
	LastFrame          string                  `json:"last_frame,omitempty"`
	FirstFrameURL      string                  `json:"first_frame_url,omitempty"`
	LastFrameURL       string                  `json:"last_frame_url,omitempty"`
	VideoConfig        struct {
		ReferenceMode string `json:"reference_mode,omitempty"`
	} `json:"video_config,omitempty"`
}

// HN accepts only these fields. In particular it rejects duration, size and resolution.
type upstreamRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	AspectRatio string   `json:"aspect_ratio"`
	Seconds     string   `json:"seconds"`
	Images      []string `json:"images,omitempty"`
	Videos      []string `json:"videos,omitempty"`
	Audios      []string `json:"audios,omitempty"`
}

type responseTask struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	Progress    any    `json:"progress"`
	URL         string `json:"url"`
	VideoURL    string `json:"video_url"`
	ResultURL   string `json:"result_url"`
	DownloadURL string `json:"download_url"`
	Metadata    struct {
		URL       string `json:"url"`
		VideoURL  string `json:"video_url"`
		ResultURL string `json:"result_url"`
	} `json:"metadata"`
	Error   any    `json:"error"`
	Message string `json:"message"`
	// Provider timestamps may be numeric, quoted or ISO; local task timestamps are authoritative.
	CreatedAt   any `json:"created_at"`
	CompletedAt any `json:"completed_at"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
	body    *upstreamRequest
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.body = nil
	var request standardRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("invalid video request JSON"), "invalid_json", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Model) == "" {
		return invalid("missing_model", "model is required")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return invalid("invalid_prompt", "prompt is required")
	}
	duration := 5
	if request.Duration != nil {
		duration = int(*request.Duration)
	}
	if request.Seconds != nil {
		if request.Duration != nil && duration != int(*request.Seconds) {
			return invalid("invalid_duration", "duration and seconds conflict")
		}
		duration = int(*request.Seconds)
	}
	switch duration {
	case 5, 10, 20, 30:
	default:
		return invalid("invalid_duration", "duration must be 5, 10, 20 or 30 seconds")
	}
	ratio := firstNonEmpty(request.AspectRatio, request.Ratio)
	if request.AspectRatio != "" && request.Ratio != "" && strings.TrimSpace(request.AspectRatio) != strings.TrimSpace(request.Ratio) {
		return invalid("invalid_aspect_ratio", "aspect_ratio and ratio conflict")
	}
	sizeRatio := ""
	switch strings.ToLower(strings.TrimSpace(request.Size)) {
	case "", "720p":
	case "1280x720", "1792x1024":
		sizeRatio = "16:9"
	case "720x1280", "1024x1792":
		sizeRatio = "9:16"
	case "720x720", "1024x1024":
		sizeRatio = "1:1"
	case "960x720":
		sizeRatio = "4:3"
	case "720x960":
		sizeRatio = "3:4"
	case "1680x720":
		sizeRatio = "21:9"
	default:
		return invalid("invalid_resolution", "size must be 720P or a supported standard video size")
	}
	ratio = firstNonEmpty(ratio, sizeRatio, "16:9")
	switch ratio {
	case "16:9", "9:16", "1:1", "4:3", "3:4", "21:9":
	default:
		return invalid("invalid_aspect_ratio", "unsupported aspect ratio")
	}
	if value := strings.TrimSpace(request.Resolution); value != "" && !strings.EqualFold(value, "720p") {
		return invalid("invalid_resolution", "wanchen HN supports only 720P")
	}
	mode := strings.ToLower(firstNonEmpty(request.VideoConfig.ReferenceMode, request.ReferenceMode, "auto"))
	if firstNonEmpty(request.FirstFrame, request.LastFrame, request.FirstFrameURL, request.LastFrameURL) != "" {
		return invalid("unsupported_reference_mode", "wanchen HN does not support first/last frame inputs")
	}
	if mode != "auto" && mode != "image" && mode != "reference" {
		return invalid("unsupported_reference_mode", "wanchen HN supports reference materials, not frame modes")
	}
	images := mediaURLs([]string{firstNonEmpty(request.ImageURL, request.Image, request.InputReference)}, request.Images, request.ImageURLs, request.ReferenceImageURLs, request.ReferenceImages, request.RefImages, request.References, request.ReferenceURLs)
	videos := mediaURLs([]string{firstNonEmpty(request.ReferenceVideo, request.VideoURL, request.Video)}, request.Videos, request.ReferenceVideos, request.VideoURLs)
	audios := mediaURLs([]string{firstNonEmpty(request.AudioURL, request.Audio, request.RefAudio, request.InputAudio)}, request.Audios, request.AudioURLs, request.ReferenceAudios, request.RefAudios)
	if len(images) > 30 || len(videos) > 15 || len(audios) > 15 {
		return invalid("invalid_reference_count", "wanchen HN allows at most 30 images, 15 videos and 15 audios")
	}
	if len(audios) > 0 && len(images)+len(videos) == 0 {
		return invalid("invalid_reference_count", "audio references require an image or video reference")
	}
	for _, group := range [][]string{images, videos, audios} {
		for _, value := range group {
			if !publicHTTPSURL(value) {
				return invalid("invalid_reference_url", "reference materials must use public HTTPS URLs")
			}
		}
	}
	a.body = &upstreamRequest{Model: firstNonEmpty(info.UpstreamModelName, request.Model), Prompt: request.Prompt, AspectRatio: ratio, Seconds: strconv.Itoa(duration), Images: images, Videos: videos, Audios: audios}
	info.Action = constant.TaskActionTextGenerate
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func invalid(code, message string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
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
	body := *a.body
	body.Model = firstNonEmpty(info.UpstreamModelName, body.Model)
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
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("read wanchen submit response failed"), "read_response_body_failed", http.StatusBadGateway)
	}
	var upstream responseTask
	if err := common.Unmarshal(data, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("invalid wanchen submit response"), "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	id := firstNonEmpty(upstream.ID, upstream.TaskID)
	if id == "" || failureStatus(upstream.Status) {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", failureReason(upstream)), "submit_failed", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = relaydto.VideoStatusQueued, time.Now().Unix()
	if a.body != nil {
		video.Seconds, video.Size = a.body.Seconds, "720p"
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
		return nil, fmt.Errorf("wanchen task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}
func (*TaskAdaptor) ParseTaskResult(data []byte) (*relaycommon.TaskInfo, error) {
	var response responseTask
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("invalid wanchen task response")
	}
	result := &relaycommon.TaskInfo{}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "submitted", "init", "waiting":
		result.Status, result.Progress = model.TaskStatusQueued, taskcommon.ProgressQueued
	case "completed", "success", "succeeded", "done":
		result.Status, result.Progress = model.TaskStatusSuccess, taskcommon.ProgressComplete
		for _, value := range []string{response.VideoURL, response.URL, response.ResultURL, response.DownloadURL, response.Metadata.VideoURL, response.Metadata.URL, response.Metadata.ResultURL} {
			if publicHTTPSURL(value) {
				result.Url = strings.TrimSpace(value)
				break
			}
		}
		// Empty URL is intentional: the service downloads the documented authenticated content endpoint.
	case "failed", "failure", "error", "cancelled", "canceled", "rejected":
		result.Status, result.Progress, result.Reason = model.TaskStatusFailure, taskcommon.ProgressComplete, failureReason(response)
	default:
		result.Status, result.Progress = model.TaskStatusInProgress, taskcommon.ProgressInProgress
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
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
func mediaURLs(groups ...[]string) []string {
	var result []string
	for _, group := range groups {
		for _, value := range group {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}
func publicHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
	}
	return true
}
func failureStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "failure", "error", "cancelled", "canceled", "rejected":
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
	runes := []rune(common.MaskSensitiveInfo(firstNonEmpty(message, "wanchen upstream request failed")))
	if len(runes) > 500 {
		runes = runes[:500]
	}
	return string(runes)
}
