package shafu

import (
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TaskAdaptor reuses the upstream NewAPI/Sora video protocol while exposing
// shafu's fixed model catalog as an independent channel type.
type TaskAdaptor struct {
	sora.TaskAdaptor
}

// SupportsImageSizePricing prevents the Sora adaptor's async image pricing
// capability from being promoted to this video-only channel.
func (*TaskAdaptor) SupportsImageSizePricing() bool {
	return false
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := a.TaskAdaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return taskErr
	}

	request, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	upstreamModel := strings.TrimSpace(info.UpstreamModelName)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(request.Model)
	}
	if !isSupportedModel(upstreamModel) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported shafu model: %s", upstreamModel), "unsupported_model", http.StatusBadRequest)
	}

	duration := 4
	ratio := ""
	size := ""
	referenceMode := ""
	hasReference := len(request.Images) > 0
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(contentType, "multipart/form-data") {
		formData, formErr := common.ParseMultipartFormReusable(c)
		if formErr != nil {
			return service.TaskErrorWrapperLocal(formErr, "invalid_multipart_form", http.StatusBadRequest)
		}
		form := formData.Value
		if raw := strings.TrimSpace(formValue(form, "duration")); raw != "" {
			duration, err = strconv.Atoi(raw)
		} else if raw = strings.TrimSpace(formValue(form, "seconds")); raw != "" {
			duration, err = strconv.Atoi(raw)
		}
		ratio = firstNonEmpty(formValue(form, "aspect_ratio"), formValue(form, "aspectRatio"), formValue(form, "ratio"))
		size = strings.TrimSpace(formValue(form, "size"))
		referenceMode = firstNonEmpty(formValue(form, "reference_mode"), formValue(form, "referenceMode"), formValue(form, "video_reference_mode"), formValue(form, "videoReferenceMode"))
		hasReference = hasReference || hasMultipartReference(formData)
	} else {
		var body map[string]any
		if decodeErr := common.UnmarshalBodyReusable(c, &body); decodeErr != nil {
			return service.TaskErrorWrapperLocal(decodeErr, "invalid_json", http.StatusBadRequest)
		}
		if raw, exists := body["duration"]; exists && raw != nil {
			duration, err = parseInteger(raw)
		} else if raw, exists = body["seconds"]; exists && raw != nil {
			if _, ok := raw.(string); !ok {
				err = fmt.Errorf("seconds must be a string")
			} else {
				duration, err = parseInteger(raw)
			}
		}
		ratio = firstNonEmpty(stringValue(body["aspect_ratio"]), stringValue(body["aspectRatio"]), stringValue(body["ratio"]))
		size = strings.TrimSpace(stringValue(body["size"]))
		referenceMode = firstNonEmpty(stringValue(body["reference_mode"]), stringValue(body["referenceMode"]), stringValue(body["video_reference_mode"]), stringValue(body["videoReferenceMode"]))
		hasReference = hasReference || hasJSONReference(body)
	}
	if err != nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration/seconds must be an integer: %w", err), "invalid_duration", http.StatusBadRequest)
	}
	if duration < 4 || duration > 15 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 4 and 15"), "invalid_duration", http.StatusBadRequest)
	}
	if ratio == "" && size != "" {
		var ok bool
		ratio, ok = ratioFromSize(size)
		if !ok {
			return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported size: %s", size), "invalid_size", http.StatusBadRequest)
		}
	}
	if ratio != "" && !isSupportedRatio(ratio) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported aspect ratio: %s", ratio), "invalid_aspect_ratio", http.StatusBadRequest)
	}
	if referenceMode != "" && referenceMode != "image" && referenceMode != "frame" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported reference mode: %s", referenceMode), "invalid_reference_mode", http.StatusBadRequest)
	}

	request.Duration = duration
	request.Seconds = ""
	c.Set("task_request", request)
	if hasReference {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (*TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	request, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	duration := request.Duration
	if duration == 0 {
		duration = 4
	}
	return map[string]float64{"seconds": float64(duration)}
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	result, err := a.TaskAdaptor.ParseTaskResult(respBody)
	if err != nil || result.Status != model.TaskStatusSuccess {
		return result, err
	}
	resultURL := strings.TrimSpace(gjson.GetBytes(respBody, "metadata.result_url").String())
	parsed, parseErr := url.Parse(resultURL)
	if parseErr == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		result.Url = resultURL
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	data, err := a.TaskAdaptor.ConvertToOpenAIVideo(task)
	if err != nil {
		return nil, err
	}
	data, err = sjson.SetBytes(data, "task_id", task.TaskID)
	if err != nil {
		return nil, fmt.Errorf("set task_id failed: %w", err)
	}
	if gjson.GetBytes(data, "metadata.result_url").Exists() {
		data, err = sjson.SetBytes(data, "metadata.result_url", task.GetResultMediaURL())
		if err != nil {
			return nil, fmt.Errorf("set metadata.result_url failed: %w", err)
		}
	}
	return data, nil
}

func (*TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (*TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func isSupportedModel(modelName string) bool {
	for _, candidate := range ModelList {
		if modelName == candidate {
			return true
		}
	}
	return false
}

func parseInteger(value any) (int, error) {
	switch typed := value.(type) {
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	case float64:
		if math.Trunc(typed) != typed {
			return 0, fmt.Errorf("value is not an integer")
		}
		return strconv.Atoi(strconv.FormatFloat(typed, 'f', -1, 64))
	case json.Number:
		return strconv.Atoi(typed.String())
	default:
		return 0, fmt.Errorf("unsupported value type")
	}
}

func isSupportedRatio(value string) bool {
	switch strings.TrimSpace(value) {
	case "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	default:
		return false
	}
}

func ratioFromSize(value string) (string, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	documentedSizes := map[string]string{
		"1120x480":  "21:9",
		"1680x720":  "21:9",
		"2520x1080": "21:9",
		"854x480":   "16:9",
		"1280x720":  "16:9",
		"1920x1080": "16:9",
		"640x480":   "4:3",
		"960x720":   "4:3",
		"1440x1080": "4:3",
		"480x480":   "1:1",
		"720x720":   "1:1",
		"1080x1080": "1:1",
		"480x640":   "3:4",
		"720x960":   "3:4",
		"1080x1440": "3:4",
		"480x854":   "9:16",
		"720x1280":  "9:16",
		"1080x1920": "9:16",
	}
	if ratio, ok := documentedSizes[normalized]; ok {
		return ratio, true
	}
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return "", false
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 10000 || height > 10000 {
		return "", false
	}
	for _, ratio := range []struct {
		name          string
		width, height int
	}{
		{name: "21:9", width: 21, height: 9},
		{name: "16:9", width: 16, height: 9},
		{name: "4:3", width: 4, height: 3},
		{name: "1:1", width: 1, height: 1},
		{name: "3:4", width: 3, height: 4},
		{name: "9:16", width: 9, height: 16},
	} {
		if width*ratio.height == height*ratio.width {
			return ratio.name, true
		}
	}
	return "", false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func formValue(form map[string][]string, key string) string {
	values := form[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func hasJSONReference(body map[string]any) bool {
	for _, key := range []string{"image", "images", "input_reference", "inputReference", "messages", "input", "reference_videos", "referenceVideos", "videos", "reference_audios", "referenceAudios", "audios"} {
		if value, exists := body[key]; exists && value != nil {
			return true
		}
	}
	return false
}

func hasMultipartReference(form *multipart.Form) bool {
	if form == nil {
		return false
	}
	for _, key := range []string{"image", "images", "input_reference", "inputReference", "reference_videos", "referenceVideos", "videos", "reference_audios", "referenceAudios", "audios"} {
		if len(form.Value[key]) > 0 {
			return true
		}
		if len(form.File[key]) > 0 {
			return true
		}
	}
	return false
}
