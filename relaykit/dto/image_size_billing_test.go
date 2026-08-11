package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImageRequestBillingMetaIncludesOutputSize 验证图片请求会把输出尺寸带入计费元数据。
func TestImageRequestBillingMetaIncludesOutputSize(t *testing.T) {
	request := &ImageRequest{Size: "2048x2048"}

	meta := request.GetTokenCountMeta()

	assert.True(t, meta.ImageGeneration)
	assert.Equal(t, "2048x2048", meta.ImageSize)
}

// TestGeminiImageBillingMetaSupportsImageSizeAliases 验证 Gemini 的两种尺寸字段写法都能进入计费元数据。
func TestGeminiImageBillingMetaSupportsImageSizeAliases(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{name: "snake case", config: `{"image_size":"2K"}`, want: "2K"},
		{name: "camel case", config: `{"imageSize":"4K"}`, want: "4K"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &GeminiChatRequest{}
			require.NoError(t, request.GenerationConfig.UnmarshalJSON([]byte(`{"image_config":`+test.config+`}`)))

			meta := request.GetTokenCountMeta()

			assert.True(t, meta.ImageGeneration)
			assert.Equal(t, test.want, meta.ImageSize)
		})
	}
}

// TestOpenAICompatibleImageBillingMetaReadsGoogleImageConfig 验证 OpenAI 兼容请求可读取 Google 扩展尺寸。
func TestOpenAICompatibleImageBillingMetaReadsGoogleImageConfig(t *testing.T) {
	request := &GeneralOpenAIRequest{
		ExtraBody:  []byte(`{"google":{"image_config":{"image_size":"4K"}}}`),
		Modalities: []byte(`["TEXT","IMAGE"]`),
	}

	meta := request.GetTokenCountMeta()

	assert.True(t, meta.ImageGeneration)
	assert.Equal(t, "4K", meta.ImageSize)
}

// TestOpenAIResponsesImageBillingMetaUsesImageGenerationToolSize 验证 Responses 图片工具尺寸会进入计费元数据。
func TestOpenAIResponsesImageBillingMetaUsesImageGenerationToolSize(t *testing.T) {
	request := OpenAIResponsesRequest{
		Tools: json.RawMessage(`[{"type":"image_generation","size":"2048x2048"}]`),
	}

	meta := request.GetTokenCountMeta()

	assert.True(t, meta.ImageGeneration)
	assert.Equal(t, "2048x2048", meta.ImageSize)
}

// TestImageChatBillingMetaIncludesRequestedImageCount 验证图片数量会被记录为计费倍率。
func TestImageChatBillingMetaIncludesRequestedImageCount(t *testing.T) {
	openAIN := 3
	openAIRequest := GeneralOpenAIRequest{
		N:          &openAIN,
		Size:       "2K",
		Modalities: json.RawMessage(`["image"]`),
	}
	assert.Equal(t, 3.0, openAIRequest.GetTokenCountMeta().BillingRatios["n"])

	geminiN := 2
	geminiRequest := GeminiChatRequest{
		GenerationConfig: GeminiChatGenerationConfig{
			CandidateCount:     &geminiN,
			ResponseModalities: []string{"IMAGE"},
		},
	}
	assert.Equal(t, 2.0, geminiRequest.GetTokenCountMeta().BillingRatios["n"])
}
