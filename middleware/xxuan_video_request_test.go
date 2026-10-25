package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXxuanVideoRoutingPreservesProviderFields(t *testing.T) {
	for _, payload := range []string{
		`{"model":"retail-video","prompt":"scene","seconds":10,"ratio":"16:9","assets":{"videos":["https://media.example/source.mp4"]}}`,
		`{"model":"retail-video","prompt":"scene","seconds":"10","ratio":"16:9","assets":{}}`,
		`{"model":"retail-video","video_url":"https://media.example/source.mp4","resolution":"1080p","frame_rate":60}`,
	} {
		t.Run(payload, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(payload))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			request, selectChannel, err := getModelRequest(c)
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.True(t, selectChannel)
			assert.Equal(t, "retail-video", request.Model)
			assert.Equal(t, relayconstant.RelayModeVideoSubmit, c.GetInt("relay_mode"))
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			assert.JSONEq(t, payload, string(body), "routing must preserve numeric seconds, asset groups and promptless upscale fields for the adaptor")
		})
	}
}
