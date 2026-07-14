package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSoraTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestBuildRequestBodyNormalizesWFSD2933Protocol(t *testing.T) {
	ctx := newSoraTestContext(t, `{
		"model":"sora-v6-14-933Z-720",
		"prompt":"test",
		"size":"1280x720",
		"duration":10,
		"input_reference":"https://example.test/main.jpg",
		"images":["https://example.test/ref-1.jpg","https://example.test/ref-2.jpg"]
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamModelName = "wf-sd2-933-pro"

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, "wf-sd2-933-pro", payload["model"])
	assert.Equal(t, "16:9", payload["aspect_ratio"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.EqualValues(t, 10, payload["seconds"])
	assert.Equal(t, "https://example.test/main.jpg", payload["image_url"])
	assert.Equal(t, []interface{}{"https://example.test/ref-1.jpg", "https://example.test/ref-2.jpg"}, payload["reference_image_urls"])
	for _, removed := range []string{"size", "duration", "input_reference", "image", "images"} {
		assert.NotContains(t, payload, removed)
	}
}

func TestBuildRequestBodyPreservesNativeWFSD2933Fields(t *testing.T) {
	ctx := newSoraTestContext(t, `{
		"model":"custom-model",
		"prompt":"test",
		"aspect_ratio":"21:9",
		"resolution":"720p",
		"seconds":"15",
		"image_url":"https://example.test/native.jpg"
	}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamModelName = "wf-sd2-933-fast"

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, "21:9", payload["aspect_ratio"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, "15", payload["seconds"])
	assert.Equal(t, "https://example.test/native.jpg", payload["image_url"])
}

func TestBuildRequestBodyLeavesOtherSoraProtocolsUnchanged(t *testing.T) {
	ctx := newSoraTestContext(t, `{"model":"sora-2","prompt":"test","size":"1280x720","seconds":"8"}`)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamModelName = "sora-2"

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))

	assert.Equal(t, "1280x720", payload["size"])
	assert.NotContains(t, payload, "aspect_ratio")
}
