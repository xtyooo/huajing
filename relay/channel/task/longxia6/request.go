package longxia6

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	Size               string                  `json:"size,omitempty"`
	Image              string                  `json:"image,omitempty"`
	ImageURL           string                  `json:"image_url,omitempty"`
	InputReference     string                  `json:"input_reference,omitempty"`
	Images             taskcommon.MediaURLList `json:"images,omitempty"`
	ImageURLs          taskcommon.MediaURLList `json:"image_urls,omitempty"`
	ReferenceImageURLs taskcommon.MediaURLList `json:"reference_image_urls,omitempty"`
	ReferenceImages    taskcommon.MediaURLList `json:"reference_images,omitempty"`
	RefImages          taskcommon.MediaURLList `json:"ref_images,omitempty"`
	ImageRefs          taskcommon.MediaURLList `json:"image_refs,omitempty"`
	References         taskcommon.MediaURLList `json:"references,omitempty"`
	ReferenceURLs      taskcommon.MediaURLList `json:"reference_urls,omitempty"`
	Video              string                  `json:"video,omitempty"`
	VideoURL           string                  `json:"video_url,omitempty"`
	ReferenceVideo     string                  `json:"reference_video,omitempty"`
	Videos             taskcommon.MediaURLList `json:"videos,omitempty"`
	VideoURLs          taskcommon.MediaURLList `json:"video_urls,omitempty"`
	ReferenceVideos    taskcommon.MediaURLList `json:"reference_videos,omitempty"`
	ReferenceVideosAlt taskcommon.MediaURLList `json:"referenceVideos,omitempty"`
	VideoRefs          taskcommon.MediaURLList `json:"video_refs,omitempty"`
	Audio              string                  `json:"audio,omitempty"`
	AudioURL           string                  `json:"audio_url,omitempty"`
	RefAudio           string                  `json:"ref_audio,omitempty"`
	InputAudio         string                  `json:"input_audio,omitempty"`
	Audios             taskcommon.MediaURLList `json:"audios,omitempty"`
	AudioURLs          taskcommon.MediaURLList `json:"audio_urls,omitempty"`
	ReferenceAudios    taskcommon.MediaURLList `json:"reference_audios,omitempty"`
	ReferenceAudiosAlt taskcommon.MediaURLList `json:"referenceAudios,omitempty"`
	AudioRefs          taskcommon.MediaURLList `json:"audio_refs,omitempty"`
	RefAudios          taskcommon.MediaURLList `json:"ref_audios,omitempty"`
	Assets             []asset                 `json:"assets,omitempty"`
	ReferenceMode      string                  `json:"reference_mode,omitempty"`
	FirstFrame         string                  `json:"first_frame,omitempty"`
	LastFrame          string                  `json:"last_frame,omitempty"`
	FirstFrameURL      string                  `json:"first_frame_url,omitempty"`
	LastFrameURL       string                  `json:"last_frame_url,omitempty"`
	VideoConfig        struct {
		ReferenceMode string `json:"reference_mode,omitempty"`
	} `json:"video_config,omitempty"`
}

type asset struct {
	Category   string `json:"category"`
	URL        string `json:"url,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
}

func (a *asset) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("asset must be an object")
	}
	for key := range fields {
		if key != "category" && key != "url" && key != "data_base64" {
			return fmt.Errorf("asset contains an unsupported field")
		}
	}
	_, hasURL := fields["url"]
	_, hasBase64 := fields["data_base64"]
	if hasURL == hasBase64 {
		return fmt.Errorf("asset requires exactly one of url and data_base64")
	}
	type plainAsset asset
	var value plainAsset
	if err := common.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("asset fields must be strings")
	}
	*a = asset(value)
	return nil
}

type upstreamRequest struct {
	Model    string  `json:"model"`
	Prompt   string  `json:"prompt"`
	Duration int     `json:"duration"`
	Size     string  `json:"size"`
	Assets   []asset `json:"assets,omitempty"`
}

type capabilities struct {
	minDuration, maxDuration, promptMax, images, audios, videos int
	wideRatios, imageRequired, audioNeedsImage                  bool
}

func modelCapabilities(name string) capabilities {
	// Unrecognized mappings retain common resource limits and leave provider capabilities upstream.
	result := capabilities{1, relaycommon.MaxTaskDurationSeconds, 9500, 30, 10, 10, true, false, false}
	base := strings.TrimSuffix(name, "-PerSecond")
	switch base {
	case "LongXia-video-seedance2_0-mini-express", "LongXia-video-seedance2_0-fast-express", "LongXia-video-seedance2_0-standard-express":
		return capabilities{4, 15, 5000, 6, 2, 0, false, false, true}
	case "LongXia-video-seedance2_5-standard-480p-express", "LongXia-video-seedance2_5-standard-720p-express":
		return capabilities{4, 25, 9500, 30, 10, 0, true, false, true}
	case "LongXia-video-seedance2_5-standard-720p-reference-video-express":
		return capabilities{4, 15, 9500, 30, 10, 10, true, false, true}
	case "LongXia-video-minimax-h3-express":
		return capabilities{5, 15, 2000, 5, 3, 0, true, false, true}
	case "LongXia-video-gemini-omni-flash-express":
		return capabilities{3, 10, 5000, 5, 0, 0, false, true, false}
	}
	return result
}

func normalizeRatio(request standardRequest) (string, error) {
	ratio := firstNonEmpty(request.AspectRatio, request.Ratio)
	if request.AspectRatio != "" && request.Ratio != "" && strings.TrimSpace(request.AspectRatio) != strings.TrimSpace(request.Ratio) {
		return "", fmt.Errorf("aspect_ratio and ratio conflict")
	}
	size := strings.ToLower(strings.TrimSpace(request.Size))
	switch size {
	case "480p", "720p", "1080p", "1440p":
		// Standard clients may put a resolution hint in size; the mapped SKU fixes resolution.
		size = ""
	case "1280x720", "1792x1024", "2560x1440":
		size = "16:9"
	case "720x1280", "1024x1792", "1440x2560":
		size = "9:16"
	case "1024x1024", "720x720", "1440x1440":
		size = "1:1"
	case "960x720", "1920x1440":
		size = "4:3"
	case "720x960", "1440x1920":
		size = "3:4"
	case "1680x720", "3360x1440":
		size = "21:9"
	}
	if ratio != "" && size != "" && ratio != size {
		return "", fmt.Errorf("size and aspect ratio conflict")
	}
	ratio = firstNonEmpty(ratio, size, "9:16")
	switch ratio {
	case "9:16", "16:9", "1:1", "4:3", "3:4", "21:9":
		return ratio, nil
	}
	return "", fmt.Errorf("unsupported aspect ratio")
}

func mediaValues(groups ...[]string) []string {
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

func normalizeAssets(request standardRequest) ([]asset, error) {
	assets := append([]asset(nil), request.Assets...)
	groups := []struct {
		category string
		values   []string
	}{
		{"image", mediaValues([]string{firstNonEmpty(request.ImageURL, request.Image, request.InputReference)}, request.Images, request.ImageURLs, request.ReferenceImageURLs, request.ReferenceImages, request.RefImages, request.ImageRefs, request.References, request.ReferenceURLs)},
		{"audio", mediaValues([]string{firstNonEmpty(request.AudioURL, request.Audio, request.RefAudio, request.InputAudio)}, request.Audios, request.AudioURLs, request.ReferenceAudios, request.ReferenceAudiosAlt, request.RefAudios, request.AudioRefs)},
		{"video", mediaValues([]string{firstNonEmpty(request.ReferenceVideo, request.VideoURL, request.Video)}, request.Videos, request.VideoURLs, request.ReferenceVideos, request.ReferenceVideosAlt, request.VideoRefs)},
	}
	for _, group := range groups {
		for _, value := range group.values {
			item := asset{Category: group.category, URL: value}
			if strings.HasPrefix(value, "data:") {
				parts := strings.SplitN(value, ",", 2)
				if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
					return nil, fmt.Errorf("invalid reference data URI")
				}
				mime := strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")
				valid := group.category == "image" && (mime == "image/png" || mime == "image/jpeg" || mime == "image/webp") || group.category == "audio" && (mime == "audio/mpeg" || mime == "audio/mp3") || group.category == "video" && mime == "video/mp4"
				if !valid {
					return nil, fmt.Errorf("unsupported reference data URI media type")
				}
				item.URL, item.DataBase64 = "", parts[1]
			}
			assets = append(assets, item)
		}
	}
	var inlineBytes, encodedBytes int64
	for _, item := range assets {
		if item.Category != "image" && item.Category != "audio" && item.Category != "video" {
			return nil, fmt.Errorf("asset category must be image, audio or video")
		}
		if (item.URL == "") == (item.DataBase64 == "") {
			return nil, fmt.Errorf("asset requires exactly one nonempty source")
		}
		if item.URL != "" {
			if !publicHTTPSURL(item.URL) {
				return nil, fmt.Errorf("asset URL must use public HTTPS")
			}
			continue
		}
		encodedBytes += int64(len(item.DataBase64))
		if encodedBytes > 40<<20 {
			return nil, fmt.Errorf("combined Base64 references exceed 40 MiB")
		}
		limit := int64(64 << 20)
		if item.Category == "image" {
			limit = 25 << 20
		} else if item.Category == "audio" {
			limit = 15 << 20
		}
		n, err := io.Copy(io.Discard, io.LimitReader(base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(item.DataBase64)), limit+1))
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid reference Base64")
		}
		if n > limit {
			return nil, fmt.Errorf("reference exceeds per-item size limit")
		}
		inlineBytes += n
		if inlineBytes > 64<<20 {
			return nil, fmt.Errorf("combined references exceed 64 MiB")
		}
	}
	return assets, nil
}

var referenceTag = regexp.MustCompile(`@(image|audio|video)([0-9]+)`)

func validateReferences(prompt string, assets []asset, limits capabilities) error {
	counts := map[string]int{"image": 0, "audio": 0, "video": 0}
	for _, item := range assets {
		counts[item.Category]++
	}
	if counts["image"] > limits.images || counts["audio"] > limits.audios || counts["video"] > limits.videos {
		return fmt.Errorf("reference counts exceed this model's image/audio/video limits")
	}
	if limits.imageRequired && counts["image"] == 0 {
		return fmt.Errorf("this model requires at least one reference image")
	}
	if limits.audioNeedsImage && counts["audio"] > 0 && counts["image"] == 0 {
		return fmt.Errorf("reference audio requires an image")
	}
	seen := map[string]map[int]bool{"image": {}, "audio": {}, "video": {}}
	for _, match := range referenceTag.FindAllStringSubmatch(prompt, -1) {
		n, err := strconv.Atoi(match[2])
		if err != nil || n < 1 || n > counts[match[1]] || match[2] != strconv.Itoa(n) {
			return fmt.Errorf("prompt references a nonexistent or invalid material index")
		}
		seen[match[1]][n] = true
	}
	for category, count := range counts {
		for n := 1; n <= count; n++ {
			if !seen[category][n] {
				return fmt.Errorf("prompt must reference every material as @%s1 through @%s%d", category, category, count)
			}
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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
