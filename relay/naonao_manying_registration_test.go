package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/manying"
	"github.com/QuantumNous/new-api/relay/channel/task/naonao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNaonaoAndManyingTaskAdaptorRegistration(t *testing.T) {
	tests := []struct {
		channelType int
		model       string
		wantAdaptor any
	}{
		{channelType: constant.ChannelTypeNaonao, model: naonao.ModelList[0], wantAdaptor: &naonao.TaskAdaptor{}},
		{channelType: constant.ChannelTypeManying, model: manying.ModelList[0], wantAdaptor: &manying.TaskAdaptor{}},
	}

	for _, test := range tests {
		apiType, ok := common.ChannelType2APIType(test.channelType)
		require.True(t, ok)
		assert.Equal(t, constant.APITypeOpenAI, apiType)
		assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(test.channelType, test.model))
		assert.IsType(t, test.wantAdaptor, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(test.channelType))))
	}
}
