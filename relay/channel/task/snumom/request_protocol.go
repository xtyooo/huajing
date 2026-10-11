package snumom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Reference objects carry frame roles that cannot be represented by []string.
// Keep scalar and string-array inputs compatible with existing plugins.
type mediaReference struct {
	URL  string `json:"url"`
	Role string `json:"role,omitempty"`
}

type referenceList []mediaReference

func (refs *referenceList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*refs = nil
		return nil
	}
	var items []json.RawMessage
	if len(data) > 0 && data[0] == '[' {
		if err := common.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("references must be URL strings or URL objects")
		}
	} else {
		items = []json.RawMessage{data}
	}
	result := make(referenceList, 0, len(items))
	for _, item := range items {
		item = bytes.TrimSpace(item)
		var ref mediaReference
		switch {
		case len(item) > 0 && item[0] == '"':
			if err := common.Unmarshal(item, &ref.URL); err != nil {
				return fmt.Errorf("reference URL must be a string")
			}
		case len(item) > 0 && item[0] == '{':
			if err := common.Unmarshal(item, &ref); err != nil || strings.TrimSpace(ref.URL) == "" {
				return fmt.Errorf("reference object must contain a URL string")
			}
		default:
			return fmt.Errorf("references must be URL strings or URL objects")
		}
		ref.URL, ref.Role = strings.TrimSpace(ref.URL), strings.TrimSpace(ref.Role)
		if ref.URL != "" {
			result = append(result, ref)
		}
	}
	*refs = result
	return nil
}

type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	Role     string        `json:"role,omitempty"`
	ImageURL referenceList `json:"image_url,omitempty"`
	VideoURL referenceList `json:"video_url,omitempty"`
	AudioURL referenceList `json:"audio_url,omitempty"`
}

type standardRequest struct {
	Model              string         `json:"model"`
	Prompt             string         `json:"prompt"`
	Duration           *flexibleInt   `json:"duration,omitempty"`
	Seconds            *flexibleInt   `json:"seconds,omitempty"`
	Resolution         string         `json:"resolution,omitempty"`
	Size               string         `json:"size,omitempty"`
	AspectRatio        string         `json:"aspect_ratio,omitempty"`
	Ratio              string         `json:"ratio,omitempty"`
	Image              string         `json:"image,omitempty"`
	ImageURL           string         `json:"image_url,omitempty"`
	InputReference     string         `json:"input_reference,omitempty"`
	Images             referenceList  `json:"images,omitempty"`
	ImageURLs          referenceList  `json:"image_urls,omitempty"`
	ReferenceImages    referenceList  `json:"reference_images,omitempty"`
	ReferenceImageURLs referenceList  `json:"reference_image_urls,omitempty"`
	References         referenceList  `json:"references,omitempty"`
	ReferenceURLs      referenceList  `json:"reference_urls,omitempty"`
	Video              string         `json:"video,omitempty"`
	VideoURL           string         `json:"video_url,omitempty"`
	ReferenceVideo     string         `json:"reference_video,omitempty"`
	Videos             referenceList  `json:"videos,omitempty"`
	VideoURLs          referenceList  `json:"video_urls,omitempty"`
	ReferenceVideos    referenceList  `json:"reference_videos,omitempty"`
	Audio              string         `json:"audio,omitempty"`
	AudioURL           string         `json:"audio_url,omitempty"`
	ReferenceAudio     string         `json:"reference_audio,omitempty"`
	Audios             referenceList  `json:"audios,omitempty"`
	AudioURLs          referenceList  `json:"audio_urls,omitempty"`
	ReferenceAudios    referenceList  `json:"reference_audios,omitempty"`
	GenerateAudio      *bool          `json:"generate_audio,omitempty"`
	FirstFrame         string         `json:"first_frame,omitempty"`
	LastFrame          string         `json:"last_frame,omitempty"`
	FirstFrameURL      string         `json:"first_frame_url,omitempty"`
	LastFrameURL       string         `json:"last_frame_url,omitempty"`
	Content            []contentPart  `json:"content,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

var requestAliases = map[string]string{
	"aspectRatio": "aspect_ratio", "generateAudio": "generate_audio",
	"imageUrl": "image_url", "imageUrls": "image_urls", "inputReference": "input_reference",
	"referenceImages": "reference_images", "referenceImageUrls": "reference_image_urls",
	"referenceUrls": "reference_urls", "referenceVideo": "reference_video",
	"videoUrl": "video_url", "videoUrls": "video_urls", "referenceVideos": "reference_videos",
	"audioUrl": "audio_url", "audioUrls": "audio_urls", "referenceAudio": "reference_audio",
	"referenceAudios": "reference_audios", "firstFrame": "first_frame", "lastFrame": "last_frame",
	"firstFrameUrl": "first_frame_url", "lastFrameUrl": "last_frame_url",
	"first_image_url": "first_frame_url", "last_image_url": "last_frame_url",
}

func readStandardRequest(c *gin.Context) (standardRequest, error) {
	var raw map[string]any
	var request standardRequest
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return request, err
	}
	// Top-level canonical keys win over aliases and extra. Never flatten metadata.
	for alias, canonical := range requestAliases {
		if value, exists := raw[alias]; exists {
			if _, specified := raw[canonical]; !specified {
				raw[canonical] = value
			}
		}
	}
	if extra, ok := raw["extra"].(map[string]any); ok {
		for key, value := range extra {
			if canonical, ok := requestAliases[key]; ok {
				key = canonical
			}
			if _, specified := raw[key]; !specified && key != "model" && key != "prompt" {
				raw[key] = value
			}
		}
	}
	data, err := common.Marshal(raw)
	if err != nil {
		return request, err
	}
	if err = common.Unmarshal(data, &request); err != nil {
		return request, err
	}
	return request, nil
}

// Metadata remains a provider extension, but duration remains a bounded billing
// input on every forwarded path, including nested metadata and native content.
func validateDurationFields(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if (key == "duration" || key == "seconds") && child != nil {
				data, err := common.Marshal(child)
				var duration flexibleInt
				if err != nil || common.Unmarshal(data, &duration) != nil || !validDuration(int(duration)) {
					return fmt.Errorf("duration must be -1 or between 1 and %d seconds", relaycommon.MaxTaskDurationSeconds)
				}
			}
			if err := validateDurationFields(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := validateDurationFields(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validDuration(duration int) bool {
	return duration == -1 || duration > 0 && duration <= relaycommon.MaxTaskDurationSeconds
}

func requestMaterials(request standardRequest) (string, referenceList, referenceList, referenceList, error) {
	prompt := request.Prompt
	images := mediaReferences("", request.ReferenceImages, request.Images, request.ImageURLs, request.ReferenceImageURLs, request.References, request.ReferenceURLs)
	if value := firstNonEmpty(request.ImageURL, request.Image); value != "" {
		images = append(referenceList{{URL: value}}, images...)
	}
	if value := firstNonEmpty(request.FirstFrameURL, request.FirstFrame); value != "" {
		images = append(referenceList{{URL: value, Role: "first_frame"}}, images...)
	}
	if value := request.InputReference; strings.TrimSpace(value) != "" {
		images = append(referenceList{{URL: strings.TrimSpace(value)}}, images...)
	}
	videos := mediaReferences(firstNonEmpty(request.VideoURL, request.Video, request.ReferenceVideo), request.ReferenceVideos, request.Videos, request.VideoURLs)
	audios := mediaReferences(firstNonEmpty(request.AudioURL, request.Audio, request.ReferenceAudio), request.ReferenceAudios, request.Audios, request.AudioURLs)
	text := []string{}
	for _, part := range request.Content {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) != "" {
				text = append(text, part.Text)
			}
		case "image_url", "video_url", "audio_url":
			refs := part.ImageURL
			if part.Type == "video_url" {
				refs = part.VideoURL
			} else if part.Type == "audio_url" {
				refs = part.AudioURL
			}
			if len(refs) == 0 {
				return "", nil, nil, nil, fmt.Errorf("content media requires its URL")
			}
			for _, ref := range refs {
				if ref.Role == "" {
					ref.Role = part.Role
				}
				switch part.Type {
				case "image_url":
					images = append(images, ref)
				case "video_url":
					videos = append(videos, ref)
				case "audio_url":
					audios = append(audios, ref)
				}
			}
		default:
			return "", nil, nil, nil, fmt.Errorf("unsupported content type")
		}
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = strings.Join(text, "\n")
	}
	if value := firstNonEmpty(request.LastFrameURL, request.LastFrame); value != "" {
		images = append(images, mediaReference{URL: value, Role: "last_frame"})
	}
	return prompt, images, videos, audios, nil
}

func mediaReferences(scalar string, groups ...referenceList) referenceList {
	result := referenceList{}
	if strings.TrimSpace(scalar) != "" {
		result = append(result, mediaReference{URL: strings.TrimSpace(scalar)})
	}
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

func referenceWireValues(refs referenceList, defaultRole string) []any {
	values := make([]any, 0, len(refs))
	for _, ref := range refs {
		role := firstNonEmpty(ref.Role, defaultRole)
		if role == "" {
			values = append(values, ref.URL)
		} else {
			values = append(values, mediaReference{URL: ref.URL, Role: role})
		}
	}
	return values
}

func validPublicHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsUnspecified() || ip.IsMulticast() || ip.IsPrivate() || common.IsPrivateIP(ip)) {
		return false
	}
	return true
}

func validImageURL(value string, allowData bool) bool {
	if allowData && strings.HasPrefix(value, "data:image/") {
		return strings.Contains(value, ";base64,") && len(value) <= 64<<20
	}
	return validPublicHTTPURL(value)
}

func validateReferenceInputs(images, videos, audios referenceList, grok bool) *dto.TaskError {
	if grok && len(videos)+len(audios) > 0 {
		return invalid("unsupported_reference_media", "Grok video protocol does not support reference videos or audios")
	}
	for _, ref := range images {
		if grok && ref.Role == "last_frame" {
			return invalid("unsupported_reference_mode", "Grok video protocol does not support last_frame")
		}
		if !validImageURL(ref.URL, !grok) {
			return invalid("invalid_reference_url", "reference images must use public HTTP(S) URLs; Dreamina also supports image data URLs")
		}
	}
	for _, ref := range append(append(referenceList{}, videos...), audios...) {
		if !validPublicHTTPURL(ref.URL) {
			return invalid("invalid_reference_url", "reference videos and audios must use public HTTP(S) URLs")
		}
	}
	return nil
}

func validateForwardedMetadata(value any, grok bool) *dto.TaskError {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if canonical, ok := requestAliases[key]; ok {
				key = canonical
			}
			var images, videos, audios referenceList
			switch key {
			case "image", "image_url", "images", "image_urls", "reference_images", "reference_image_urls", "input_reference", "first_frame", "first_frame_url", "last_frame", "last_frame_url":
				data, err := common.Marshal(child)
				if err != nil || images.UnmarshalJSON(data) != nil {
					return invalid("invalid_reference_media", "invalid image reference in metadata")
				}
				if key == "last_frame" || key == "last_frame_url" {
					for index := range images {
						images[index].Role = "last_frame"
					}
				}
			case "video", "video_url", "videos", "video_urls", "reference_video", "reference_videos":
				data, err := common.Marshal(child)
				if err != nil || videos.UnmarshalJSON(data) != nil {
					return invalid("invalid_reference_media", "invalid video reference in metadata")
				}
			case "audio", "audio_url", "audios", "audio_urls", "reference_audio", "reference_audios":
				data, err := common.Marshal(child)
				if err != nil || audios.UnmarshalJSON(data) != nil {
					return invalid("invalid_reference_media", "invalid audio reference in metadata")
				}
			case "content":
				data, err := common.Marshal(child)
				var request standardRequest
				if err != nil || common.Unmarshal(data, &request.Content) != nil {
					return invalid("invalid_reference_media", "invalid multimedia content in metadata")
				}
				_, images, videos, audios, err = requestMaterials(request)
				if err != nil {
					return invalid("invalid_reference_media", err.Error())
				}
			}
			if err := validateReferenceInputs(images, videos, audios, grok); err != nil {
				return err
			}
			if err := validateForwardedMetadata(child, grok); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := validateForwardedMetadata(child, grok); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildVideoProtocolBody(modelName string, request standardRequest, duration *int, prompt string, images, videos, audios referenceList, doubao bool) map[string]any {
	body := map[string]any{"model": modelName, "prompt": prompt}
	if len(request.Metadata) > 0 {
		body["metadata"] = cloneMap(request.Metadata)
	}
	name := strings.ToLower(modelName)
	grok := strings.HasPrefix(name, "grok-imagine-video-")
	dreamina := strings.HasPrefix(name, "dreamina-seedance-")
	if duration != nil {
		switch {
		case grok || strings.HasPrefix(name, "wan3."):
			body["seconds"] = strconv.Itoa(*duration)
		case dreamina:
			body["seconds"] = *duration
		default:
			body["duration"] = *duration
		}
	}
	if grok {
		if size := firstNonEmpty(request.Resolution, request.Size); size != "" {
			body["size"] = size
		}
	} else if request.Resolution != "" {
		body["resolution"] = request.Resolution
	} else if request.Size != "" {
		body["size"] = request.Size
	}
	if ratio := firstNonEmpty(request.AspectRatio, request.Ratio); ratio != "" {
		body["aspect_ratio"] = ratio
	}
	if doubao {
		metadata := cloneMap(request.Metadata)
		if len(images)+len(videos)+len(audios) > 0 || metadata["content"] == nil {
			content := []map[string]any{{"type": "text", "text": prompt}}
			for _, material := range []struct {
				kind, role string
				refs       referenceList
			}{
				{"image_url", "reference_image", images}, {"video_url", "reference_video", videos}, {"audio_url", "reference_audio", audios},
			} {
				for _, ref := range material.refs {
					content = append(content, map[string]any{"type": material.kind, material.kind: map[string]string{"url": ref.URL}, "role": firstNonEmpty(ref.Role, material.role)})
				}
			}
			metadata["content"] = content
		}
		if resolution := firstNonEmpty(request.Resolution, request.Size); resolution != "" {
			metadata["resolution"] = resolution
		} else if metadata["resolution"] == nil {
			metadata["resolution"] = "720p" // Legacy Doubao contract.
		}
		if ratio := firstNonEmpty(request.AspectRatio, request.Ratio); ratio != "" {
			metadata["ratio"] = ratio
		} else if metadata["ratio"] == nil {
			metadata["ratio"] = "16:9"
		}
		if request.GenerateAudio != nil {
			metadata["generate_audio"] = *request.GenerateAudio
		}
		body["metadata"] = metadata
		delete(body, "resolution")
		delete(body, "size")
		delete(body, "aspect_ratio")
		return body
	}
	if len(images) > 0 {
		key, defaultRole := "reference_images", ""
		if strings.EqualFold(modelName, "sd-2.5") {
			key = "images"
		}
		if grok {
			defaultRole = "reference_image"
		}
		body[key] = referenceWireValues(images, defaultRole)
	}
	if len(videos) > 0 {
		body["reference_videos"] = referenceWireValues(videos, "")
	}
	if len(audios) > 0 {
		body["reference_audios"] = referenceWireValues(audios, "")
	}
	if request.GenerateAudio != nil {
		body["generate_audio"] = *request.GenerateAudio
	}
	return body
}
