package gemini_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageEndpointAcceptsAdministratorModelAcrossGoogleChannels(t *testing.T) {
	for _, name := range []string{"gemini", "vertex"} {
		for _, model := range []string{"administrator-image-v2", "claude-looking-image", "llama-looking-image", "image-thinking", "imagen-4.0-generate-001"} {
			t.Run(name+"/"+model, func(t *testing.T) {
				var adaptor channel.Adaptor
				if name == "vertex" {
					adaptor = &vertex.Adaptor{}
				} else {
					adaptor = &gemini.Adaptor{}
				}
				info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeImagesGenerations, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model, ChannelBaseUrl: "https://provider.example", ApiKey: `{"project_id":"project"}`}}
				adaptor.Init(info)
				converted, err := adaptor.ConvertImageRequest(nil, info, dto.ImageRequest{Prompt: "draw", Size: "2K"})
				require.NoError(t, err)
				imageRequest, ok := converted.(dto.GeminiImageRequest)
				require.True(t, ok)
				require.Equal(t, "draw", imageRequest.Instances[0].Prompt)
				require.Equal(t, "2K", imageRequest.Parameters.ImageSize)
				url, err := adaptor.GetRequestURL(info)
				require.NoError(t, err)
				require.Contains(t, url, "/models/"+model+":predict")
				require.Equal(t, model, info.UpstreamModelName)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"predictions":[{"mimeType":"image/png","bytesBase64Encoded":"image-data"}]}`))}
				_, apiErr := adaptor.DoResponse(c, resp, info)
				require.Nil(t, apiErr)
				require.Contains(t, recorder.Body.String(), `"b64_json":"image-data"`)
			})
		}
	}
}

func TestGeminiNonImageRoutesRemainUnchanged(t *testing.T) {
	for _, tc := range []struct {
		model  string
		mode   int
		batch  bool
		suffix string
	}{
		{"gemini-2.5-pro", constant.RelayModeChatCompletions, false, ":generateContent"},
		{"gemini-native-image-model", constant.RelayModeGemini, false, ":generateContent"},
		{"text-embedding-004", constant.RelayModeEmbeddings, false, ":embedContent"},
		{"gemini-embedding-001", constant.RelayModeEmbeddings, true, ":batchEmbedContents"},
	} {
		info := &relaycommon.RelayInfo{RelayMode: tc.mode, IsGeminiBatchEmbedding: tc.batch, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model, ChannelBaseUrl: "https://provider.example"}}
		url, err := (&gemini.Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(url, tc.suffix), url)
	}
}

func TestVertexChatProtocolSelectionRemainsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		model  string
		mode   int
		suffix string
	}{
		{"gemini-2.5-pro", vertex.RequestModeGemini, ":generateContent"},
		{"claude-private-model", vertex.RequestModeClaude, ":rawPredict"},
		{"meta/llama-private-maas", vertex.RequestModeOpenSource, "/chat/completions"},
	} {
		info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model, ChannelBaseUrl: "https://provider.example", ApiKey: `{"project_id":"project"}`}}
		a := &vertex.Adaptor{}
		a.Init(info)
		require.Equal(t, tc.mode, a.RequestMode)
		url, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(url, tc.suffix), url)
	}
}
