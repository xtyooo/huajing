package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/autodl_h3"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoDLH3ChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeAutoDLH3)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeAutoDLH3, autodl_h3.ModelList[0]))
	assert.IsType(t, &autodl_h3.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAutoDLH3))))
	assert.Equal(t, []string{
		"minimax_h3_lightx2v_no_pic",
		"minimax_h3_image_audio_to_video_v2_15s",
		"minimax_h3_b99_002",
	}, (&autodl_h3.TaskAdaptor{}).GetModelList())
}

func TestAutoDLH3FixedPriceCanSkipSecondsRatio(t *testing.T) {
	tests := []struct {
		name      string
		skip      bool
		wantQuota int
	}{
		{name: "fixed price skips seconds", skip: true, wantQuota: 100},
		{name: "per-second price applies seconds", skip: false, wantQuota: 1500},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: true, Quota: 100}}
			info.PriceData.AddOtherRatio("seconds", 15)

			applyTaskPriceRatios(info, test.skip)

			assert.Equal(t, test.wantQuota, info.PriceData.Quota)
			assert.Equal(t, map[string]float64{"seconds": 15}, info.PriceData.OtherRatios())
		})
	}
}
