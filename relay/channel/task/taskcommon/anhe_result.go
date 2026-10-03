package taskcommon

import (
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
)

// ExtractAnheResultURL extracts the stable video URL from the documented Anhe
// result aliases. Anhe may wrap the provider payload in data.data; the nested
// content URL must win over an outer stale result_url from that envelope.
func ExtractAnheResultURL(body []byte) string {
	paths := []string{
		"content.video_url",
		"data.data.content.video_url",
		"data.content.video_url",
		// When data.data is the provider payload, check all of its
		// documented aliases before the wrapper's potentially stale result_url.
		"data.data.result_url",
		"data.data.video_url",
		"data.data.url",
		"data.data.metadata.url",
		"data.data.metadata.result_url",
		"data.data.result.url",
		"data.data.result.video_url",
		"result_url",
		"video_url",
		"url",
		"metadata.url",
		"metadata.result_url",
		"result.url",
		"result.video_url",
		"data.video_url",
		"data.result_url",
		"data.url",
		// Some deployments wrap the documented payload aliases one level
		// deeper than the public response contract. Keep these fallbacks after
		// the documented top-level priority.
	}
	for _, path := range paths {
		if candidate := validHTTPResultURL(gjson.GetBytes(body, path).String()); candidate != "" {
			return candidate
		}
	}
	return ""
}

func validHTTPResultURL(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return ""
	}
	return value
}
