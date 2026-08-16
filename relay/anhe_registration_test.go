package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/anhe"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnheChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeAnhe)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeAnhe, anhe.ModelList[0]))
	assert.IsType(t, &anhe.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAnhe))))
}

func TestAnheFixedPriceCanSkipSecondsRatio(t *testing.T) {
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
