package yaochen

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
)

type flexibleInt int

func (n *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if strings.HasPrefix(raw, `"`) {
		if err := common.Unmarshal(data, &raw); err != nil {
			return err
		}
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("must be an integer")
	}
	*n = flexibleInt(value)
	return nil
}

type standardRequest struct {
	Model              string                  `json:"model"`
	Prompt             string                  `json:"prompt"`
	Duration           *flexibleInt            `json:"duration,omitempty"`
	Seconds            *flexibleInt            `json:"seconds,omitempty"`
	AspectRatio        string                  `json:"aspect_ratio,omitempty"`
	Ratio              string                  `json:"ratio,omitempty"`
	Size               string                  `json:"size,omitempty"`
	Image              string                  `json:"image,omitempty"`
	ImageURL           string                  `json:"image_url,omitempty"`
	InputReference     string                  `json:"input_reference,omitempty"`
	Images             taskcommon.MediaURLList `json:"images,omitempty"`
	ImageURLs          taskcommon.MediaURLList `json:"image_urls,omitempty"`
	ReferenceImageURLs taskcommon.MediaURLList `json:"reference_image_urls,omitempty"`
	RefImages          taskcommon.MediaURLList `json:"ref_images,omitempty"`
	ReferenceImages    taskcommon.MediaURLList `json:"reference_images,omitempty"`
	References         taskcommon.MediaURLList `json:"references,omitempty"`
	ReferenceURLs      taskcommon.MediaURLList `json:"reference_urls,omitempty"`
	ImageRefs          taskcommon.MediaURLList `json:"image_refs,omitempty"`
	ReferenceMode      string                  `json:"reference_mode,omitempty"`
	VideoConfig        struct {
		ReferenceMode string `json:"reference_mode,omitempty"`
	} `json:"video_config,omitempty"`
}

type upstreamRequest struct {
	Model   string   `json:"model"`
	Prompt  string   `json:"prompt"`
	Ratio   string   `json:"ratio"`
	Seconds int      `json:"seconds"`
	Images  []string `json:"images"`
}

type responseTask struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Progress any    `json:"progress"`
	Result   *struct {
		URL string `json:"url"`
	} `json:"result"`
	Error any `json:"error"`
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
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}
	if strings.TrimSpace(request.Model) == "" {
		return invalid("missing_model", "model is required")
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > 12000 {
		return invalid("invalid_prompt", "prompt must contain between 1 and 12000 Unicode characters")
	}
	for _, duration := range []*flexibleInt{request.Duration, request.Seconds} {
		if duration != nil && *duration != 30 {
			return invalid("invalid_duration", "yaochen requires a fixed duration of 30 seconds")
		}
	}
	ratio := firstNonEmpty(request.AspectRatio, request.Ratio)
	if request.AspectRatio != "" && request.Ratio != "" && strings.TrimSpace(request.AspectRatio) != strings.TrimSpace(request.Ratio) {
		return invalid("invalid_aspect_ratio", "aspect_ratio and ratio conflict")
	}
	if ratio == "" {
		switch strings.ToLower(strings.TrimSpace(request.Size)) {
		case "720x1280", "1080x1920", "1024x1792":
			ratio = "9:16"
		case "1280x720", "1920x1080", "1792x1024":
			ratio = "16:9"
		case "1024x1024":
			ratio = "1:1"
		case "1024x768":
			ratio = "4:3"
		case "768x1024":
			ratio = "3:4"
		case "", "720p":
			ratio = "16:9"
		default:
			ratio = strings.TrimSpace(request.Size)
			if widthText, heightText, ok := strings.Cut(strings.ToLower(ratio), "x"); ok {
				width, widthErr := strconv.Atoi(strings.TrimSpace(widthText))
				height, heightErr := strconv.Atoi(strings.TrimSpace(heightText))
				if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
					a, b := width, height
					for b != 0 {
						a, b = b, a%b
					}
					ratio = fmt.Sprintf("%d:%d", width/a, height/a)
					if ratio == "7:3" {
						ratio = "21:9"
					}
				}
			}
		}
	}
	switch ratio {
	case "16:9", "9:16", "1:1", "3:4", "4:3", "21:9":
	default:
		return invalid("invalid_aspect_ratio", "unsupported aspect ratio")
	}
	mode := firstNonEmpty(request.VideoConfig.ReferenceMode, request.ReferenceMode)
	if mode != "" && mode != "auto" && mode != "image" {
		return invalid("unsupported_reference_mode", "yaochen supports ordered image references, not frame modes")
	}

	// Inspect unsupported standard reference aliases so they cannot silently disappear.
	var fields map[string]any
	if err := common.UnmarshalBodyReusable(c, &fields); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}
	for _, name := range []string{"video", "video_url", "video_urls", "reference_video", "reference_videos", "reference_video_urls", "referenceVideos", "video_refs", "videos", "ref_video", "ref_videos", "audio", "audio_url", "audio_urls", "reference_audio", "reference_audios", "reference_audio_urls", "referenceAudios", "audio_refs", "audios", "ref_audio", "ref_audios", "input_audio", "first_frame", "last_frame", "first_frame_url", "last_frame_url", "medias"} {
		value := fields[name]
		if value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		case []any:
			if len(typed) == 0 {
				continue
			}
		}
		return invalid("unsupported_reference_media", "yaochen supports image references only; unsupported field: "+name)
	}
	images := make([]string, 0)
	if single := firstNonEmpty(request.ImageURL, request.Image, request.InputReference); single != "" {
		images = append(images, single)
	}
	for _, list := range []taskcommon.MediaURLList{request.Images, request.ReferenceImageURLs, request.ImageURLs, request.RefImages, request.ReferenceImages, request.References, request.ReferenceURLs, request.ImageRefs} {
		for _, item := range list {
			if strings.TrimSpace(item) != "" {
				images = append(images, strings.TrimSpace(item))
			}
		}
	}
	if len(images) > 9 {
		return invalid("invalid_reference_count", "yaochen supports at most 9 reference images")
	}
	inlineBytes := 0
	for _, value := range images {
		if strings.HasPrefix(value, "data:") {
			size, err := validateInlineImage(value)
			if err != nil {
				return invalid("invalid_reference_image", err.Error())
			}
			inlineBytes += size
			if inlineBytes > 20<<20 {
				return invalid("invalid_reference_image", "inline reference images exceed 20 MiB total")
			}
		} else if !validPublicHTTPSURL(value) {
			return invalid("invalid_reference_url", "reference images must be public HTTPS URLs or PNG/JPEG data URIs")
		}
	}
	// Remote image decoding/size checks belong upstream; do not download untrusted URLs here.
	a.body = &upstreamRequest{Model: firstNonEmpty(info.UpstreamModelName, request.Model), Prompt: prompt, Ratio: ratio, Seconds: 30, Images: images}
	info.Action = constant.TaskActionTextGenerate
	if len(images) > 0 {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func validateInlineImage(value string) (int, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || (header != "data:image/png;base64" && header != "data:image/jpeg;base64" && header != "data:image/jpg;base64") {
		return 0, fmt.Errorf("reference image must be a PNG/JPEG base64 data URI")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(20<<20) {
		return 0, fmt.Errorf("inline reference image exceeds 20 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > 20<<20 {
		return 0, fmt.Errorf("invalid or oversized base64 reference image")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return 0, fmt.Errorf("reference image is not a valid PNG/JPEG")
	}
	if (format == "png") != (header == "data:image/png;base64") {
		return 0, fmt.Errorf("reference image format does not match its data URI")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 40000000 {
		return 0, fmt.Errorf("reference image exceeds 8192 pixels per edge or 40 million pixels")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return 0, fmt.Errorf("reference image cannot be fully decoded")
	}
	return len(data), nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return providerVideosURL(a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", firstNonEmpty(c.GetHeader("Idempotency-Key"), info.PublicTaskID))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return nil, fmt.Errorf("validated request is unavailable")
	}
	body := *a.body
	body.Model = firstNonEmpty(info.UpstreamModelName, body.Model)
	data, err := common.Marshal(body)
	return bytes.NewReader(data), err
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
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	var task responseTask
	if err := common.Unmarshal(data, &task); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("decode yaochen submit response: %w", err), "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	if strings.TrimSpace(task.TaskID) == "" || strings.EqualFold(task.Status, "failed") {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream submit failed: %s", responseErrorMessage(task.Error)), "submit_failed", http.StatusBadGateway)
	}
	video := relaydto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Model = info.OriginModelName
	video.Status = relaydto.VideoStatusQueued
	video.CreatedAt = time.Now().Unix()
	video.Seconds = "30"
	c.JSON(http.StatusOK, video)
	return strings.TrimSpace(task.TaskID), data, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, providerVideosURL(baseURL)+"/"+url.PathEscape(strings.TrimSpace(id)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream task query returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func (*TaskAdaptor) ParseTaskResult(data []byte) (*relaycommon.TaskInfo, error) {
	var task responseTask
	if err := common.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("decode yaochen task: %w", err)
	}
	result := &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: taskcommon.ProgressInProgress}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "queued":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "submitting", "in_progress", "unknown":
	case "completed":
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		if task.Result != nil && validPublicHTTPSURL(task.Result.URL) {
			result.Url = strings.TrimSpace(task.Result.URL)
		}
		// Empty URL is resolved by the authenticated content fallback in the service layer.
		return result, nil
	case "failed":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseErrorMessage(task.Error)
		return result, nil
	default:
		return nil, fmt.Errorf("unrecognized yaochen task status: %s", common.MaskSensitiveInfo(task.Status))
	}
	var progress float64
	switch value := task.Progress.(type) {
	case float64:
		progress = value
	case string:
		progress, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
	}
	if progress > 99 {
		progress = 99
	}
	if progress > 0 {
		result.Progress = fmt.Sprintf("%d%%", int(progress))
	}
	return result, nil
}

func (*TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := task.ToOpenAIVideo()
	video.TaskID = task.TaskID
	if task.Status == model.TaskStatusFailure {
		video.Error = &relaydto.OpenAIVideoError{Code: "upstream_error", Message: firstNonEmpty(task.FailReason, "task failed")}
	}
	return common.Marshal(video)
}

func (*TaskAdaptor) GetModelList() []string { return ModelList }
func (*TaskAdaptor) GetChannelName() string { return ChannelName }

func invalid(code, message string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", message), code, http.StatusBadRequest)
}

func providerVideosURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
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

func validPublicHTTPSURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback()
	}
	return true
}

func responseErrorMessage(value any) string {
	message, _ := value.(string)
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"message", "detail", "error", "msg"} {
			if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
				message = text
				break
			}
		}
	}
	runes := []rune(common.MaskSensitiveInfo(firstNonEmpty(message, "upstream task failed")))
	if len(runes) > 500 {
		runes = runes[:500]
	}
	return string(runes)
}
