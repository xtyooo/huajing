package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/diaomao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiaomaoChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeDiaomao)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeDiaomao, diaomao.ModelList[0]))
	assert.IsType(t, &diaomao.TaskAdaptor{}, GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDiaomao))))
}
