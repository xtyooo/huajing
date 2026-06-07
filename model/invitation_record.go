package model

import (
	"gorm.io/gorm"
)

type InvitationRecord struct {
	Id          int            `json:"id"`
	InviterId   int            `json:"inviter_id" gorm:"index:idx_inviter_id_created"`
	InviterName string         `json:"inviter_name" gorm:"index"`
	InviteeId   int            `json:"invitee_id" gorm:"index:idx_invitee_id_created"`
	InviteeName string         `json:"invitee_name" gorm:"index"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
	CreatedAt   int64          `json:"created_at" gorm:"index:idx_created_at"`
}

type SyncInvitationRecordQueryParams struct {
	InviterId      int
	InviterName    string
	InviteeId      int
	InviteeName    string
	StartTimestamp int64
	EndTimestamp   int64
}

func (invitationRecord *InvitationRecord) Insert() error {
	return DB.Create(invitationRecord).Error
}

func (invitationRecord *InvitationRecord) Update() error {
	return DB.Save(invitationRecord).Error
}

func buildInvitationQuery(queryParams SyncInvitationRecordQueryParams) *gorm.DB {
	query := DB.Model(&InvitationRecord{})

	if queryParams.InviterId != 0 {
		query = query.Where("inviter_id = ?", queryParams.InviterId)
	}
	if queryParams.InviterName != "" {
		query = query.Where("inviter_name = ?", queryParams.InviterName)
	}
	if queryParams.InviteeId != 0 {
		query = query.Where("invitee_id = ?", queryParams.InviteeId)
	}
	if queryParams.InviteeName != "" {
		query = query.Where("invitee_name = ?", queryParams.InviteeName)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("created_at >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("created_at <= ?", queryParams.EndTimestamp)
	}

	return query
}

func rebateAggSubQuery() *gorm.DB {
	return DB.Model(&RebateRecord{}).
		Select(`
			invitee_id,
			SUM(recharge_amount) AS recharge_total,
			SUM(rebate_amount) AS rebate_total
		`).
		Group("invitee_id")
}

func InvitationRecordGetAllRecord(
	startIdx int,
	num int,
	queryParams SyncInvitationRecordQueryParams,
) []map[string]any {

	var result []map[string]any

	query := buildInvitationQuery(queryParams)

	_ = query.
		Table("invitation_records ir").
		Select(`
			ir.id,
			ir.inviter_id,
			ir.inviter_name,
			ir.invitee_id,
			ir.invitee_name,
			ir.created_at,

			COALESCE(rr.recharge_total, 0) AS recharge_total,
			COALESCE(rr.rebate_total, 0) AS rebate_total
		`).
		Joins("LEFT JOIN (?) rr ON rr.invitee_id = ir.invitee_id", rebateAggSubQuery()).
		Order("ir.id DESC").
		Limit(num).
		Offset(startIdx).
		Find(&result).Error

	return result
}

func InvitationRecordGetAllUserRecord(
	userId int,
	startIdx int,
	num int,
	queryParams SyncInvitationRecordQueryParams,
) []map[string]any {

	var result []map[string]any

	query := buildInvitationQuery(queryParams).
		Where("inviter_id = ?", userId)

	_ = query.
		Table("invitation_records ir").
		Select(`
			ir.id,
			ir.inviter_id,
			ir.inviter_name,
			ir.invitee_id,
			ir.invitee_name,
			ir.created_at,

			COALESCE(rr.recharge_total, 0) AS recharge_total,
			COALESCE(rr.rebate_total, 0) AS rebate_total
		`).
		Joins("LEFT JOIN (?) rr ON rr.invitee_id = ir.invitee_id", rebateAggSubQuery()).
		Order("ir.id DESC").
		Limit(num).
		Offset(startIdx).
		Find(&result).Error

	return result
}

func InvitationRecordCountAllRecord(queryParams SyncInvitationRecordQueryParams) int64 {
	var total int64
	_ = buildInvitationQuery(queryParams).Count(&total).Error
	return total
}

func InvitationRecordCountAllUserRecord(userId int, queryParams SyncInvitationRecordQueryParams) int64 {
	var total int64
	_ = buildInvitationQuery(queryParams).
		Where("inviter_id = ?", userId).
		Count(&total).Error
	return total
}
