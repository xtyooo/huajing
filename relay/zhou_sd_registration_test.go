package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/zhou_sd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZhouSDChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeZhouSD)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeZhouSD, zhou_sd.ModelList[0]))
	assert.IsType(t, &zhou_sd.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeZhouSD))))
}
