package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCustomChannelTypeIDsRemainStable 锁定线上已持久化的二开渠道编号，防止上游合并静默改变其适配器语义。
func TestCustomChannelTypeIDsRemainStable(t *testing.T) {
	expected := map[string]int{
		"MuseAI":          58,
		"MeAI":            59,
		"Xs":              60,
		"HJ":              61,
		"Mimo":            62,
		"Lingjing":        63,
		"Advanced Custom": 64,
		"kuai":            65,
		"sd0717":          66,
		"wufan":           67,
		"Sub2API":         68,
		"New API":         69,
		"安和":              70,
		"zhou_sd":         71,
		"AutoDL H3":       72,
		"diaomao":         73,
	}

	actual := map[string]int{
		"MuseAI":          ChannelTypeMuse,
		"MeAI":            ChannelTypeMeAI,
		"Xs":              ChannelTypeXs,
		"HJ":              ChannelTypeHJ,
		"Mimo":            ChannelTypeMimo,
		"Lingjing":        ChannelTypeLingjing,
		"Advanced Custom": ChannelTypeAdvancedCustom,
		"kuai":            ChannelTypeKuai,
		"sd0717":          ChannelTypeSD0717,
		"wufan":           ChannelTypeWufan,
		"Sub2API":         ChannelTypeSub2API,
		"New API":         ChannelTypeNewAPI,
		"安和":              ChannelTypeAnhe,
		"zhou_sd":         ChannelTypeZhouSD,
		"AutoDL H3":       ChannelTypeAutoDLH3,
		"diaomao":         ChannelTypeDiaomao,
	}

	assert.Equal(t, expected, actual)
	assert.Equal(t, 74, ChannelTypeDummy)
	require.Len(t, ChannelBaseURLs, ChannelTypeDummy)
	for name, channelType := range actual {
		assert.Equal(t, name, ChannelTypeNames[channelType])
	}
}
