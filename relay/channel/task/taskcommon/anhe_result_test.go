package taskcommon

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExtractAnheResultURLUsesNestedContentBeforeEnvelopeResultURL(t *testing.T) {
	body := []byte(`{"code":"success","data":{"status":"SUCCESS","task_id":"remote-public","data":{"status":"succeeded","content":{"video_url":"https://cdn.example.test/current.mp4"}},"result_url":"https://anhe.example.test/stale.mp4"}}`)

	require.Equal(t, "https://cdn.example.test/current.mp4", ExtractAnheResultURL(body))
}

func TestExtractAnheResultURLSupportsDocumentedAliases(t *testing.T) {
	paths := []string{
		"content.video_url", "result_url", "video_url", "url", "metadata.url",
		"metadata.result_url", "result.url", "result.video_url", "data.video_url",
		"data.result_url", "data.url",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body := map[string]any{"status": "succeeded"}
			current := body
			parts := strings.Split(path, ".")
			for _, part := range parts[:len(parts)-1] {
				next := map[string]any{}
				current[part] = next
				current = next
			}
			current[parts[len(parts)-1]] = "https://cdn.example.test/" + strings.ReplaceAll(path, ".", "-") + ".mp4"
			data, err := common.Marshal(body)
			require.NoError(t, err)
			require.Equal(t, current[parts[len(parts)-1]], strings.TrimSpace(gjson.GetBytes(data, path).String()))
			require.Contains(t, ExtractAnheResultURL(data), strings.ReplaceAll(path, ".", "-"))
		})
	}
}

func TestExtractAnheResultURLRejectsMalformedAndNonHTTPValues(t *testing.T) {
	body := []byte(`{"status":"succeeded","video_url":"javascript:alert(1)","result_url":"https://user:pass@cdn.example.test/video.mp4"}`)
	require.Empty(t, ExtractAnheResultURL(body))
}
