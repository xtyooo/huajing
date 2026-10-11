package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/snumom"
	"github.com/stretchr/testify/require"
)

func TestSnumomVideoRegistration(t *testing.T) {
	channelType := constant.ChannelTypeSnumom
	apiType, ok := common.ChannelType2APIType(channelType)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(channelType, "future-snumom-model"))
	require.IsType(t, &snumom.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelType))))
}
