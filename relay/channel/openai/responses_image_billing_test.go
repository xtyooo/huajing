package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerUsesActualImageCallCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"output":[{"type":"image_generation_call","status":"completed"},{"type":"image_generation_call","status":"completed"}]}`,
		)),
	}
	info := &relaycommon.RelayInfo{}
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 1)

	_, err := OaiResponsesHandler(c, info, response)

	require.Nil(t, err)
	require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
	require.Equal(t, 2, c.GetInt("image_generation_call_count"))
}

func TestOaiResponsesHandlerIgnoresFailedImageCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"output":[{"type":"image_generation_call","status":"failed","quality":"high","size":"4096x4096"},{"type":"image_generation_call","status":"completed","quality":"low","size":"1024x1024"},{"type":"image_generation_call","status":"incomplete"}]}`,
		)),
	}
	info := &relaycommon.RelayInfo{}
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 3)

	_, err := OaiResponsesHandler(c, info, response)

	require.Nil(t, err)
	require.Equal(t, 1.0, info.PriceData.OtherRatios()["n"])
	require.Equal(t, 1, c.GetInt("image_generation_call_count"))
	require.Equal(t, "low", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesStreamHandlerUsesActualImageCallCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","status":"completed"},{"type":"image_generation_call","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	c, _, response, info := newImageTestContext(t, body, "text/event-stream", true)
	info.PriceData.UsePrice = true
	info.PriceData.AddOtherRatio("n", 1)

	_, err := OaiResponsesStreamHandler(c, info, response)

	require.Nil(t, err)
	require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
	require.Equal(t, 2, c.GetInt("image_generation_call_count"))
}

func TestOaiResponsesStreamHandlerSupportsAlternateFinalEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	for _, eventType := range []string{"response.done", "response.incomplete"} {
		t.Run(eventType, func(t *testing.T) {
			body := strings.Join([]string{
				`data: {"type":"` + eventType + `","response":{"output":[{"type":"image_generation_call","status":"completed"},{"type":"image_generation_call","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			c, _, response, info := newImageTestContext(t, body, "text/event-stream", true)
			info.PriceData.UsePrice = true
			info.PriceData.AddOtherRatio("n", 1)

			_, err := OaiResponsesStreamHandler(c, info, response)

			require.Nil(t, err)
			require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
			require.Equal(t, 2, c.GetInt("image_generation_call_count"))
		})
	}
}
