package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageHandlerUsesSuccessfulPredictionCountForFixedPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: true}}
	info.PriceData.AddOtherRatio("n", 3)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{"predictions":[
			{"mimeType":"image/png","bytesBase64Encoded":"first"},
			{"raiFilteredReason":"safety"},
			{"mimeType":"image/png","bytesBase64Encoded":"second"}
		]}`)),
	}

	usage, err := GeminiImageHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 516, usage.TotalTokens)
	require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
}

func TestGeminiStreamHandlerUsesSuccessfulInlineImageCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/nano-banana:streamGenerateContent", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "nano-banana-pro"},
		PriceData:   types.PriceData{UsePrice: true},
	}
	info.PriceData.AddOtherRatio("n", 3)
	body := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"first"}}]}}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"second"}}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, err := geminiStreamHandler(c, info, resp, func(string, *dto.GeminiChatResponse) bool { return true })

	require.Nil(t, err)
	require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
}

func TestBuildUsageFromGeminiResponseUsesSuccessfulInlineImageCount(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "nano-banana-pro"},
		PriceData:   types.PriceData{UsePrice: true},
	}
	info.PriceData.AddOtherRatio("n", 4)
	response := &dto.GeminiChatResponse{
		HasUsageMetadata: true,
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     1,
			CandidatesTokenCount: 2,
			TotalTokenCount:      3,
		},
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "first"}},
				{InlineData: &dto.GeminiInlineData{MimeType: "text/plain", Data: "not-an-image"}},
				{InlineData: &dto.GeminiInlineData{MimeType: "image/webp", Data: "second"}},
			}},
		}},
	}

	buildUsageFromGeminiResponse(nil, info, response)

	require.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
}
