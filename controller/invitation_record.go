package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllInvitationRecord(c *gin.Context) {

	pageInfo := common.GetPageQuery(c)

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	inviterId, _ := strconv.Atoi(c.Query("inviter_id"))
	inviteeId, _ := strconv.Atoi(c.Query("invitee_id"))

	queryParams := model.SyncInvitationRecordQueryParams{
		InviterId:      inviterId,
		InviterName:    c.Query("inviter_name"),
		InviteeId:      inviteeId,
		InviteeName:    c.Query("invitee_name"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	items := model.InvitationRecordGetAllRecord(
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		queryParams,
	)

	total := model.InvitationRecordCountAllRecord(queryParams)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)

	common.ApiSuccess(c, pageInfo)
}

func GetUserInvitationRecord(c *gin.Context) {

	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	inviteeId, _ := strconv.Atoi(c.Query("invitee_id"))

	queryParams := model.SyncInvitationRecordQueryParams{
		InviteeId:      inviteeId,
		InviteeName:    c.Query("invitee_name"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	items := model.InvitationRecordGetAllUserRecord(
		userId,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		queryParams,
	)

	total := model.InvitationRecordCountAllUserRecord(userId, queryParams)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)

	common.ApiSuccess(c, pageInfo)
}