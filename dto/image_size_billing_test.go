package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRequestBillingMetaIncludesOutputSize(t *testing.T) {
	request := &ImageRequest{Size: "2048x2048"}

	meta := request.GetTokenCountMeta()

	assert.True(t, meta.ImageGeneration)
	assert.Equal(t, "2048x2048", meta.ImageSize)
}

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

func TestOpenAICompatibleImageBillingMetaReadsGoogleImageConfig(t *testing.T) {
	request := &GeneralOpenAIRequest{
		ExtraBody:  []byte(`{"google":{"image_config":{"image_size":"4K"}}}`),
		Modalities: []byte(`["TEXT","IMAGE"]`),
	}

	meta := request.GetTokenCountMeta()

	assert.True(t, meta.ImageGeneration)
	assert.Equal(t, "4K", meta.ImageSize)
}
