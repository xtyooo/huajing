package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestPassesExplicitImageSizeTier(t *testing.T) {
	tests := []struct {
		name string
		size string
		want string
	}{
		{name: "2K", size: "2K", want: "2K"},
		{name: "lowercase 4K", size: "4k", want: "4K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "imagen-4.0-generate-001",
			}}
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, dto.ImageRequest{
				Prompt: "draw", Size: tt.size, Quality: "standard",
			})

			require.NoError(t, err)
			request, ok := converted.(dto.GeminiImageRequest)
			require.True(t, ok)
			require.Equal(t, tt.want, request.Parameters.ImageSize)
		})
	}
}
