package xxuan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type videoRequest struct {
	model, prompt, resolution, ratio, sourceURL string
	duration, frameRate                         int
	assets                                      map[string][]any
}

// Field aliases describe the standard client protocol, not a provider model catalogue.
func requestField(raw map[string]json.RawMessage, names ...string) (json.RawMessage, error) {
	var found json.RawMessage
	for _, name := range names {
		value, exists := raw[name]
		if !exists {
			continue
		}
		if found != nil {
			var first, next any
			if common.Unmarshal(found, &first) != nil || common.Unmarshal(value, &next) != nil || !reflect.DeepEqual(first, next) {
				return nil, fmt.Errorf("conflicting %s fields", names[0])
			}
		}
		found = value
	}
	return found, nil
}

func stringField(raw map[string]json.RawMessage, names ...string) (string, error) {
	value, err := requestField(raw, names...)
	if err != nil || value == nil {
		return "", err
	}
	var result string
	if common.Unmarshal(value, &result) != nil {
		return "", fmt.Errorf("%s must be a string", names[0])
	}
	return result, nil
}

func integerField(raw map[string]json.RawMessage, names ...string) (int, error) {
	var result int
	found := false
	for _, name := range names {
		value, ok := raw[name]
		if !ok {
			continue
		}
		text := string(value)
		if len(value) > 0 && value[0] == '"' {
			if common.Unmarshal(value, &text) != nil {
				return 0, fmt.Errorf("%s must be an integer", name)
			}
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		if found && result != n {
			return 0, fmt.Errorf("conflicting %s fields", names[0])
		}
		found, result = true, n
	}
	return result, nil
}

func readVideoRequest(c *gin.Context, upscale bool) (videoRequest, error) {
	request := videoRequest{assets: map[string][]any{}}
	var raw map[string]json.RawMessage
	if strings.HasPrefix(c.ContentType(), "multipart/") {
		if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
			return request, fmt.Errorf("invalid multipart request")
		}
		defer c.Request.MultipartForm.RemoveAll()
		if len(c.Request.MultipartForm.File) > 0 {
			return request, fmt.Errorf("provide a public video URL; file form fields are not supported")
		}
		raw = map[string]json.RawMessage{}
		for key, values := range c.Request.MultipartForm.Value {
			if len(values) != 1 {
				return request, fmt.Errorf("duplicate form field %s", key)
			}
			value := strings.TrimSpace(values[0])
			if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
				raw[key] = json.RawMessage(value)
			} else {
				raw[key], _ = common.Marshal(values[0])
			}
		}
	} else if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return request, fmt.Errorf("invalid video request JSON")
	}
	// Only known fields are read below. Top-level fields remain authoritative;
	// metadata/extra can supply compatibility aliases but cannot replace model.
	for _, container := range []string{"extra", "metadata"} {
		if value, ok := raw[container]; ok {
			if len(value) > 0 && value[0] == '"' {
				var text string
				if common.Unmarshal(value, &text) != nil {
					return request, fmt.Errorf("invalid %s", container)
				}
				value = []byte(text)
			}
			var fields map[string]json.RawMessage
			if common.Unmarshal(value, &fields) != nil {
				return request, fmt.Errorf("%s must be an object", container)
			}
			for key, value := range fields {
				if _, exists := raw[key]; !exists && key != "model" {
					raw[key] = value
				}
			}
		}
	}
	var err error
	request.model, err = stringField(raw, "model")
	if err != nil {
		return request, err
	}
	if strings.TrimSpace(request.model) == "" {
		return request, fmt.Errorf("model is required")
	}
	request.resolution, err = stringField(raw, "resolution")
	if err != nil {
		return request, err
	}
	size, err := stringField(raw, "size")
	if err != nil {
		return request, err
	}
	if request.resolution == "" && size != "" && !strings.ContainsAny(size, "xX:") {
		request.resolution = size
	}
	request.prompt, err = stringField(raw, "prompt")
	if err != nil {
		return request, err
	}
	if value, ok := raw["assets"]; ok {
		var groups map[string]json.RawMessage
		if common.Unmarshal(value, &groups) != nil || groups == nil {
			return request, fmt.Errorf("assets must be an object")
		}
		for group, value := range groups {
			if group != "images" && group != "videos" && group != "audios" {
				return request, fmt.Errorf("unsupported assets group")
			}
			request.assets[group], err = parseAssets(value)
			if err != nil {
				return request, err
			}
		}
	}
	aliases := [][]string{
		{"images", "image_urls", "imageUrls", "reference_images", "referenceImages", "reference_image_urls", "referenceImageUrls", "image", "image_url", "imageUrl"},
		{"videos", "video_urls", "videoUrls", "reference_videos", "referenceVideos", "video", "video_url", "videoUrl", "reference_video", "referenceVideo"},
		{"audios", "audio_urls", "audioUrls", "reference_audios", "referenceAudios", "audio", "audio_url", "audioUrl", "reference_audio", "referenceAudio"},
	}
	groups := []string{"images", "videos", "audios"}
	for i, names := range aliases {
		if (upscale && i == 1) || (!upscale && i == 0) {
			names = append(names, "input_reference", "inputReference")
		}
		var selected []any
		for _, name := range names {
			value, exists := raw[name]
			if !exists {
				continue
			}
			items, parseErr := parseAssets(value)
			if parseErr != nil {
				return request, parseErr
			}
			if selected != nil && !reflect.DeepEqual(selected, items) {
				return request, fmt.Errorf("conflicting %s references", groups[i])
			}
			selected = items
		}
		if selected != nil {
			if existing, ok := request.assets[groups[i]]; ok && !reflect.DeepEqual(existing, selected) {
				return request, fmt.Errorf("conflicting %s assets", groups[i])
			}
			request.assets[groups[i]] = selected
		}
	}
	if content, ok := raw["content"]; ok {
		var parts []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Image json.RawMessage `json:"image_url"`
			Video json.RawMessage `json:"video_url"`
			Audio json.RawMessage `json:"audio_url"`
			Role  string          `json:"role"`
		}
		if common.Unmarshal(content, &parts) != nil {
			return request, fmt.Errorf("content must be an array")
		}
		texts := []string{}
		for _, part := range parts {
			if part.Role != "" {
				return request, fmt.Errorf("xxuan assets do not support media roles; use ordered assets and prompt references")
			}
			if part.Type == "text" {
				texts = append(texts, part.Text)
				continue
			}
			var group string
			var value json.RawMessage
			switch part.Type {
			case "image_url":
				group, value = "images", part.Image
			case "video_url":
				group, value = "videos", part.Video
			case "audio_url":
				group, value = "audios", part.Audio
			default:
				return request, fmt.Errorf("unsupported content type")
			}
			items, err := parseAssets(value)
			if err != nil {
				return request, err
			}
			request.assets[group] = append(request.assets[group], items...)
		}
		if request.prompt == "" {
			request.prompt = strings.Join(texts, "\n")
		}
	}
	if upscale {
		if request.resolution == "" {
			return request, fmt.Errorf("resolution is required")
		}
		request.frameRate, err = integerField(raw, "frame_rate", "frameRate", "fps")
		if err != nil {
			return request, err
		}
		if request.frameRate <= 0 {
			return request, fmt.Errorf("frame_rate must be a positive integer")
		}
		if len(request.assets["images"])+len(request.assets["audios"]) != 0 || len(request.assets["videos"]) != 1 {
			return request, fmt.Errorf("upscale requires exactly one public video URL")
		}
		switch source := request.assets["videos"][0].(type) {
		case string:
			request.sourceURL = source
		case map[string]any:
			request.sourceURL, _ = source["source_url"].(string)
		}
		if err = validateMediaURL(request.sourceURL); err != nil {
			return request, fmt.Errorf("upscale requires a public HTTPS video URL, not an existing asset ID")
		}
		return request, nil
	}
	request.duration, err = integerField(raw, "duration_seconds", "duration", "seconds")
	if err != nil {
		return request, err
	}
	if request.duration <= 0 || request.duration > relaycommon.MaxTaskDurationSeconds {
		return request, fmt.Errorf("duration must be between 1 and %d seconds", relaycommon.MaxTaskDurationSeconds)
	}
	request.ratio, err = stringField(raw, "ratio", "aspect_ratio", "aspectRatio")
	if err != nil {
		return request, err
	}
	if request.ratio == "" && strings.Contains(size, ":") {
		request.ratio = size
	}
	if request.ratio == "" && strings.ContainsAny(size, "xX") {
		parts := strings.FieldsFunc(size, func(r rune) bool { return r == 'x' || r == 'X' })
		if len(parts) == 2 {
			w, e1 := strconv.Atoi(parts[0])
			h, e2 := strconv.Atoi(parts[1])
			if e1 == nil && e2 == nil && w > 0 && h > 0 {
				a, b := w, h
				for b != 0 {
					a, b = b, a%b
				}
				request.ratio = fmt.Sprintf("%d:%d", w/a, h/a)
			}
		}
	}
	if strings.TrimSpace(request.ratio) == "" {
		return request, fmt.Errorf("ratio or aspect_ratio is required")
	}
	if strings.TrimSpace(request.prompt) == "" || utf8.RuneCountInString(request.prompt) > 15000 {
		return request, fmt.Errorf("prompt must contain 1 to 15000 Unicode characters")
	}
	return request, nil
}

func parseAssets(value json.RawMessage) ([]any, error) {
	value = bytes.TrimSpace(value)
	var values []json.RawMessage
	if len(value) > 0 && value[0] == '[' {
		if common.Unmarshal(value, &values) != nil {
			return nil, fmt.Errorf("invalid media array")
		}
	} else {
		values = []json.RawMessage{value}
	}
	result := make([]any, 0, len(values))
	for _, item := range values {
		var text string
		if common.Unmarshal(item, &text) == nil {
			if strings.HasPrefix(text, "ast_") && len(text) > 4 {
				result = append(result, text)
				continue
			}
			if err := validateMediaURL(text); err != nil {
				return nil, err
			}
			result = append(result, text)
			continue
		}
		var object map[string]json.RawMessage
		if common.Unmarshal(item, &object) != nil || object == nil {
			return nil, fmt.Errorf("media must be an asset ID, HTTPS URL, or source_url object")
		}
		for key := range object {
			if key != "source_url" && key != "url" && key != "file_name" {
				return nil, fmt.Errorf("unsupported media object field %s", key)
			}
		}
		text, err := stringField(object, "source_url", "url")
		if err != nil {
			return nil, err
		}
		if err = validateMediaURL(text); err != nil {
			return nil, err
		}
		wire := map[string]any{"source_url": text}
		if name, exists := object["file_name"]; exists {
			var filename string
			if common.Unmarshal(name, &filename) != nil || utf8.RuneCountInString(filename) < 1 || utf8.RuneCountInString(filename) > 160 || strings.IndexFunc(filename, unicode.IsControl) >= 0 {
				return nil, fmt.Errorf("file_name must contain 1 to 160 characters without controls")
			}
			wire["file_name"] = filename
		}
		result = append(result, wire)
	}
	return result, nil
}

func validateMediaURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || len(value) > 4096 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("media URL must be a public HTTPS URL without credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return fmt.Errorf("media URL must have a public host")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()) {
		return fmt.Errorf("media URL must have a public host")
	}
	return nil
}
