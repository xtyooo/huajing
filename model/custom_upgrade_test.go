package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTaskStoresLingjingSelectedKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("63"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeLingjing,
			ApiKey:      "selected-key",
		},
	})

	assert.Equal(t, "selected-key", task.PrivateData.Key)
}

func TestInitTaskStoresSoraSelectedKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("55"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeSora,
			ApiKey:      "selected-sora-key",
		},
	})

	assert.Equal(t, "selected-sora-key", task.PrivateData.Key)
}

func TestInitTaskStoresAnheSelectedKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("70"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAnhe,
			ApiKey:      "selected-anhe-key",
		},
	})

	assert.Equal(t, "selected-anhe-key", task.PrivateData.Key)
}

func TestInitTaskStoresZhouSDSelectedKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("71"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeZhouSD,
			ApiKey:      "selected-zhou-sd-key",
		},
	})

	assert.Equal(t, "selected-zhou-sd-key", task.PrivateData.Key)
}

func TestInitTaskStoresAutoDLH3SelectedKey(t *testing.T) {
	task := InitTask(constant.TaskPlatform("72"), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAutoDLH3,
			ApiKey:      "selected-autodl-h3-key",
		},
	})

	assert.Equal(t, "selected-autodl-h3-key", task.PrivateData.Key)
}

func TestGetUploadChannelRestrictsGroupAndSpecificChannel(t *testing.T) {
	truncateTables(t)
	defaultBaseURL := "https://default.example"
	vipBaseURL := "https://vip.example"
	channels := []*Channel{
		{Type: constant.ChannelTypeLingjing, Key: "default-key", Status: common.ChannelStatusEnabled, Group: "default", BaseURL: &defaultBaseURL},
		{Type: constant.ChannelTypeLingjing, Key: "vip-key", Status: common.ChannelStatusEnabled, Group: "vip", BaseURL: &vipBaseURL},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
	}

	selected, err := GetUploadChannel(constant.ChannelTypeLingjing, "default", 0)
	require.NoError(t, err)
	assert.Equal(t, channels[0].Id, selected.Id)

	_, err = GetUploadChannel(constant.ChannelTypeLingjing, "default", channels[1].Id)
	require.NoError(t, err, "a token-specific channel is allowed regardless of its group")

	_, err = GetUploadChannel(constant.ChannelTypeMimo, "default", channels[0].Id)
	require.Error(t, err, "a token-specific channel cannot switch provider type")
}

func TestInvitationRecordAggregationIsScopedByInviter(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&InvitationRecord{}, &RebateRecord{}))

	require.NoError(t, DB.Create(&InvitationRecord{InviterId: 1, InviterName: "first", InviteeId: 9, InviteeName: "invitee", CreatedAt: 1}).Error)
	require.NoError(t, DB.Create(&InvitationRecord{InviterId: 2, InviterName: "second", InviteeId: 9, InviteeName: "invitee", CreatedAt: 2}).Error)
	require.NoError(t, DB.Create(&RebateRecord{InviterId: 1, InviteeId: 9, RechargeAmount: 100, RebateAmount: 10, CreatedAt: 3}).Error)
	require.NoError(t, DB.Create(&RebateRecord{InviterId: 2, InviteeId: 9, RechargeAmount: 200, RebateAmount: 20, CreatedAt: 4}).Error)

	first := InvitationRecordGetAllUserRecord(1, 0, 10, SyncInvitationRecordQueryParams{})
	require.Len(t, first, 1)
	assert.EqualValues(t, 100, first[0]["recharge_total"])
	assert.EqualValues(t, 10, first[0]["rebate_total"])
}
