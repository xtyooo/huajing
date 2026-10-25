package xxuan

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXxuanSubmissionFailureDiagnosticTraceAndWire(t *testing.T) {
	service.InitHttpClient()
	payload := `{"model":"public-video","prompt":"private-prompt","seconds":7,"ratio":"16:9","assets":{"images":["ast_private", "https://private.example/image.png?sig=private-signature",{"source_url":"https://private.example/other.png","file_name":"private-name.png"}],"videos":["https://private.example/video.mp4"],"audios":["ast_private_audio"]}}`
	c, _, info := requestContext(t, payload)
	c.Set(common.RequestIdKey, "gateway-local-id")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/open/v1/video/tasks", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Idempotency-Key"))
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"product_id":"opaque-future-product","prompt":"private-prompt","duration_seconds":7,"ratio":"16:9","assets":{"images":["ast_private","https://private.example/image.png?sig=private-signature",{"source_url":"https://private.example/other.png","file_name":"private-name.png"}],"videos":["https://private.example/video.mp4"],"audios":["ast_private_audio"]}}`, string(body))
		w.Header().Set("Location", "/must-not-follow")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"request_id":"req_provider-Trace_123","error":{"code":"UNSUPPORTED_MEDIA","message":"private-prompt https://private.example/image.png?sig=private-signature Bearer test-key material-ticket-private"},"task_id":"tsk_private","file_name":"private-name.png","asset_id":"ast_private"}`)
	}))
	defer server.Close()
	info.ChannelBaseUrl = server.URL
	a := &TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	body, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	response, err := a.DoRequest(c, info, body)
	require.NoError(t, err)
	defer response.Body.Close()
	diagnostic, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.True(t, c.GetBool("xxuan_task_submit_started"))
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
	assert.Equal(t, "gateway-local-id", c.GetString(common.RequestIdKey))
	assert.Equal(t, "req_provider-Trace_123", c.GetString(common.UpstreamRequestIdKey))
	assert.Contains(t, string(diagnostic), "UNSUPPORTED_MEDIA")
	assert.Contains(t, string(diagnostic), "upstream_request_id=req_provider-Trace_123")
	assert.Contains(t, string(diagnostic), "images=3[asset_id,url_string,url_object] videos=1[url_string] audios=1[asset_id]")
	assert.Equal(t, int64(len(diagnostic)), response.ContentLength)
	for _, secret := range []string{"private", "test-key", "Bearer", "sig=", "https://", "tsk_"} {
		assert.NotContains(t, string(diagnostic), secret)
	}
}

func TestXxuanSubmissionFailureDiagnosticFallbacks(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		name, response, code, trace string
	}{
		{"missing trace", `{"error_code":"UNSUPPORTED_MEDIA"}`, "UNSUPPORTED_MEDIA", ""},
		{"null trace", `{"code":"UNSUPPORTED_MEDIA","request_id":null}`, "UNSUPPORTED_MEDIA", ""},
		{"numeric trace", `{"error":{"code":"UNSUPPORTED_MEDIA"},"request_id":123}`, "UNSUPPORTED_MEDIA", ""},
		{"object trace", `{"status_reason":"UNSUPPORTED_MEDIA","request_id":{"key":"private"}}`, "UNSUPPORTED_MEDIA", ""},
		{"bare prefix", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_"}`, "UNSUPPORTED_MEDIA", ""},
		{"control trace", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_private\nInjected"}`, "UNSUPPORTED_MEDIA", ""},
		{"url trace", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_https://private.example/?sig=private"}`, "UNSUPPORTED_MEDIA", ""},
		{"unicode trace", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_私有"}`, "UNSUPPORTED_MEDIA", ""},
		{"long trace", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_` + strings.Repeat("a", 200) + `"}`, "UNSUPPORTED_MEDIA", ""},
		{"valid trace invalid code", `{"code":"private message","request_id":"req_trace123"}`, "", "req_trace123"},
		{"malformed json", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_trace123"`, "", ""},
		{"unreadable response", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_trace123"}`, "", ""},
		{"non json", `<html>private-prompt Bearer test-key</html>`, "", ""},
		{"oversize", `{"code":"UNSUPPORTED_MEDIA","request_id":"req_trace123","message":"` + strings.Repeat("a", 1<<20) + `"}`, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if test.name == "unreadable response" {
					w.Header().Set("Content-Length", fmt.Sprint(len(test.response)+10))
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			c, _, info := requestContext(t, `{}`)
			c.Set(common.RequestIdKey, "gateway-local-id")
			info.ChannelBaseUrl = server.URL
			a := &TaskAdaptor{}
			a.Init(info)
			response, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			defer response.Body.Close()
			diagnostic, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
			assert.Equal(t, "gateway-local-id", c.GetString(common.RequestIdKey))
			assert.Equal(t, test.trace, c.GetString(common.UpstreamRequestIdKey))
			assert.Contains(t, string(diagnostic), "xxuan submission returned HTTP 422")
			assert.Contains(t, string(diagnostic), "images=0[] videos=0[] audios=0[]")
			if test.code != "" {
				assert.Contains(t, string(diagnostic), "("+test.code+")")
			} else {
				assert.NotContains(t, string(diagnostic), "UNSUPPORTED_MEDIA")
			}
			if test.trace != "" {
				assert.Contains(t, string(diagnostic), "upstream_request_id="+test.trace)
			} else {
				assert.NotContains(t, string(diagnostic), "upstream_request_id=")
			}
			assert.LessOrEqual(t, len(diagnostic), 768)
			assert.NotContains(t, string(diagnostic), "private")
			assert.NotContains(t, string(diagnostic), "test-key")
		})
	}
}

func TestXxuanSubmissionFailureDiagnosticBoundedMediaAndUpscale(t *testing.T) {
	service.InitHttpClient()
	previousDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() { common.DebugEnabled = previousDebug })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"code":"UNSUPPORTED_MEDIA"}`)
	}))
	defer server.Close()
	for _, upscale := range []bool{false, true} {
		t.Run(fmt.Sprintf("upscale=%t", upscale), func(t *testing.T) {
			c, _, info := requestContext(t, `{}`)
			info.ChannelBaseUrl = server.URL
			a := &TaskAdaptor{Upscale: upscale, assetID: "ast_private_verified", request: videoRequest{assets: map[string][]any{}}}
			for _, group := range []string{"images", "videos", "audios"} {
				a.request.assets[group] = []any{"ast_private", "https://private.example/a?sig=private", map[string]any{"source_url": "https://private.example/a", "file_name": "private"}, "https://private.example/b", "ast_private_other"}
			}
			a.Init(info)
			response, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			defer response.Body.Close()
			diagnostic, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			if upscale {
				assert.Contains(t, string(diagnostic), "source=asset_id")
				assert.NotContains(t, string(diagnostic), "images=")
			} else {
				for _, group := range []string{"images", "videos", "audios"} {
					assert.Contains(t, string(diagnostic), group+"=5[asset_id,url_string,url_object,+2]")
				}
			}
			assert.LessOrEqual(t, len(diagnostic), 768)
			assert.NotContains(t, string(diagnostic), "private")
		})
	}
}
