package relay

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/shafu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShafuChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeShafu)
	require.True(t, ok)
	assert.Equal(t, constant.APITypeOpenAI, apiType)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, common.GetEndpointTypesByChannelType(constant.ChannelTypeShafu, shafu.ModelList[0]))
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeShafu)))
	assert.IsType(t, &shafu.TaskAdaptor{}, adaptor)
	imagePricingAdaptor, ok := adaptor.(channel.TaskImageSizePricingAdaptor)
	require.True(t, ok)
	assert.False(t, imagePricingAdaptor.SupportsImageSizePricing())
}
