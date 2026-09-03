package manying

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/shafu"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
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
	Model                   string       `json:"model"`
	Prompt                  string       `json:"prompt"`
	Duration                *flexibleInt `json:"duration,omitempty"`
	Seconds                 *flexibleInt `json:"seconds,omitempty"`
	AspectRatio             string       `json:"aspect_ratio,omitempty"`
	AspectRatioCamel        string       `json:"aspectRatio,omitempty"`
	Ratio                   string       `json:"ratio,omitempty"`
	Resolution              string       `json:"resolution,omitempty"`
	Image                   string       `json:"image,omitempty"`
	ImageURL                string       `json:"image_url,omitempty"`
	InputReference          string       `json:"input_reference,omitempty"`
	InputReferenceCamel     string       `json:"inputReference,omitempty"`
	Images                  []string     `json:"images,omitempty"`
	ReferenceImageURLs      []string     `json:"reference_image_urls,omitempty"`
	ReferenceVideo          string       `json:"reference_video,omitempty"`
	ReferenceVideos         []string     `json:"reference_videos,omitempty"`
	ReferenceVideosCamel    []string     `json:"referenceVideos,omitempty"`
	Videos                  []string     `json:"videos,omitempty"`
	AudioURL                string       `json:"audio_url,omitempty"`
	AudioURLs               []string     `json:"audio_urls,omitempty"`
	ReferenceAudios         []string     `json:"reference_audios,omitempty"`
	ReferenceAudiosCamel    []string     `json:"referenceAudios,omitempty"`
	Audios                  []string     `json:"audios,omitempty"`
	ReferenceMode           string       `json:"reference_mode,omitempty"`
	ReferenceModeCamel      string       `json:"referenceMode,omitempty"`
	VideoReferenceMode      string       `json:"video_reference_mode,omitempty"`
	VideoReferenceModeCamel string       `json:"videoReferenceMode,omitempty"`
	VideoConfig             videoConfig  `json:"video_config,omitempty"`
	GenerateAudio           *bool        `json:"generate_audio,omitempty"`
	GenerateAudioCamel      *bool        `json:"generateAudio,omitempty"`
	NegativePrompt          string       `json:"negative_prompt,omitempty"`
	NegativePromptCamel     string       `json:"negativePrompt,omitempty"`
	FaceProcessing          *bool        `json:"face_processing,omitempty"`
	IdempotencyKey          string       `json:"idempotency_key,omitempty"`
}

type upstreamRequest struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	Duration        int      `json:"duration"`
	AspectRatio     string   `json:"aspect_ratio"`
	GenerateAudio   *bool    `json:"generate_audio,omitempty"`
	ReferenceMode   string   `json:"reference_mode,omitempty"`
	Images          []string `json:"images,omitempty"`
	ReferenceVideos []string `json:"reference_videos,omitempty"`
	ReferenceAudios []string `json:"reference_audios,omitempty"`
	NegativePrompt  string   `json:"negative_prompt,omitempty"`
	FaceProcessing  *bool    `json:"face_processing,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}

type TaskAdaptor struct {
	shafu.TaskAdaptor
	body                 *upstreamRequest
	duration             int
	fixedResolution      string
	resolutionPrice      float64
	useResolutionPricing bool
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.body = nil
	a.duration = 0
	a.fixedResolution = ""
	a.resolutionPrice = 0
	a.useResolutionPricing = false
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		if taskErr := a.TaskAdaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
			return taskErr
		}
		request, err := relaycommon.GetTaskRequest(c)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		a.duration = request.Duration
		if a.duration == 0 {
			a.duration = 4
		}
		return a.validateModelPricing(info, firstNonEmpty(info.UpstreamModelName, request.Model))
	}

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
	if !supportedModel(upstreamModel) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported manying model: %s", upstreamModel), "unsupported_model", http.StatusBadRequest)
	}

	duration := 4
	if request.Duration != nil {
		duration = int(*request.Duration)
	} else if request.Seconds != nil {
		duration = int(*request.Seconds)
	}
	if duration < 4 || duration > 15 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 4 and 15"), "invalid_duration", http.StatusBadRequest)
	}
	ratio := firstNonEmpty(request.AspectRatio, request.AspectRatioCamel, request.Ratio, "16:9")
	if !validRatio(ratio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect_ratio: %s", ratio), "invalid_aspect_ratio", http.StatusBadRequest)
	}

	images := appendNonEmpty(nil, firstNonEmpty(request.ImageURL, request.Image, request.InputReference, request.InputReferenceCamel))
	images = appendNormalized(images, request.ReferenceImageURLs)
	images = appendNormalized(images, request.Images)
	videos := appendNonEmpty(nil, request.ReferenceVideo)
	videos = appendNormalized(videos, request.ReferenceVideos)
	videos = appendNormalized(videos, request.ReferenceVideosCamel)
	videos = appendNormalized(videos, request.Videos)
	audios := appendNonEmpty(nil, request.AudioURL)
	audios = appendNormalized(audios, request.AudioURLs)
	audios = appendNormalized(audios, request.ReferenceAudios)
	audios = appendNormalized(audios, request.ReferenceAudiosCamel)
	audios = appendNormalized(audios, request.Audios)

	referenceMode := strings.ToLower(firstNonEmpty(request.VideoConfig.ReferenceMode, request.ReferenceMode, request.ReferenceModeCamel, request.VideoReferenceMode, request.VideoReferenceModeCamel, "auto"))
	upstreamMode := "image"
	switch referenceMode {
	case "auto", "image":
		if len(images) > 9 || len(videos) > 3 || len(audios) > 3 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("reference media exceeds provider limits"), "invalid_reference_count", http.StatusBadRequest)
		}
	case "start_frame":
		upstreamMode = "frame"
		if len(images) != 1 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("start_frame requires exactly one image"), "invalid_reference_count", http.StatusBadRequest)
		}
	case "start_end":
		upstreamMode = "frame"
		if len(images) != 2 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("start_end requires exactly two images"), "invalid_reference_count", http.StatusBadRequest)
		}
	case "frame":
		upstreamMode = "frame"
		if len(images) < 1 || len(images) > 2 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("frame mode requires one or two images"), "invalid_reference_count", http.StatusBadRequest)
		}
	default:
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported reference_mode: %s", referenceMode), "invalid_reference_mode", http.StatusBadRequest)
	}
	if upstreamMode == "frame" && len(videos)+len(audios) > 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("frame mode does not accept video or audio references"), "unsupported_reference_media", http.StatusBadRequest)
	}

	generateAudio := request.GenerateAudio
	if generateAudio == nil {
		generateAudio = request.GenerateAudioCamel
	}
	a.body = &upstreamRequest{
		Model:           upstreamModel,
		Prompt:          prompt,
		Duration:        duration,
		AspectRatio:     ratio,
		GenerateAudio:   generateAudio,
		ReferenceMode:   upstreamMode,
		Images:          images,
		ReferenceVideos: videos,
		ReferenceAudios: audios,
		NegativePrompt:  firstNonEmpty(request.NegativePrompt, request.NegativePromptCamel),
		FaceProcessing:  request.FaceProcessing,
		IdempotencyKey:  strings.TrimSpace(request.IdempotencyKey),
	}
	a.duration = duration
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: prompt, Model: request.Model, Duration: duration, Images: images})
	if len(images)+len(videos)+len(audios) > 0 {
		info.Action = constant.TaskActionGenerate
	} else {
		info.Action = constant.TaskActionTextGenerate
	}
	return a.validateModelPricing(info, upstreamModel)
}

func (a *TaskAdaptor) validateModelPricing(info *relaycommon.RelayInfo, upstreamModel string) *dto.TaskError {
	resolution, ok := fixedModelResolution(upstreamModel)
	if !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported manying model: %s", upstreamModel), "unsupported_model", http.StatusBadRequest)
	}
	price, enabled, err := taskcommon.ResolutionPrice(info.OriginModelName, resolution)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "resolution_price_invalid", http.StatusBadRequest)
	}
	a.fixedResolution = resolution
	a.resolutionPrice = price
	a.useResolutionPricing = enabled
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if a.body == nil {
		return a.TaskAdaptor.BuildRequestBody(c, info)
	}
	body := *a.body
	body.Model = info.UpstreamModelName
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if a.duration < 4 || a.duration > 15 {
		return nil
	}
	if a.useResolutionPricing {
		taskcommon.ApplyResolutionPerSecondBilling(c, info, a.fixedResolution, a.duration, a.resolutionPrice)
		return nil
	}
	return map[string]float64{"seconds": float64(a.duration)}
}

func (*TaskAdaptor) GetModelList() []string { return ModelList }
func (*TaskAdaptor) GetChannelName() string { return ChannelName }

func supportedModel(value string) bool {
	_, ok := fixedModelResolution(value)
	return ok
}

func fixedModelResolution(value string) (string, bool) {
	switch value {
	case "sd-480p", "sdf-480p":
		return "480P", true
	case "sd-720p", "sdf-720p":
		return "720P", true
	case "sd-1080p":
		return "1080P", true
	default:
		return "", false
	}
}

func validRatio(value string) bool {
	switch value {
	case "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	default:
		return false
	}
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
