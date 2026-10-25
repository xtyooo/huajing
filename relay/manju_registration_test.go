package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/manju"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManjuChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeManju)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeManju, manju.ModelList[0]))
	assert.IsType(t, &manju.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeManju))))
}
