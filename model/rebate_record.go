package model

import (
	"gorm.io/gorm"
)

type RebateRecord struct {
	Id            int            `json:"id"`
	InviterId     int            `json:"inviter_id" gorm:"index"`
	InviterName   string         `json:"inviter_name" gorm:"type:varchar(191)"`
	InviteeId     int            `json:"invitee_id" gorm:"index:idx_rebate_invitee_created"`
	InviteeName   string         `json:"invitee_name" gorm:"type:varchar(191)"`
	RechargeId    int            `json:"recharge_id"`
	RechargeAmount float64       `json:"recharge_amount"`
	RebateAmount  float64        `json:"rebate_amount"`
	M             float64        `json:"m"`
	N             float64        `json:"n"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
	CreatedAt     int64          `json:"created_at" gorm:"index:idx_rebate_invitee_created"`
}

type SyncRebateRecordQueryParams struct {
	Id            int
	InviterId     int
	InviterName   string
	InviteeId     int
	InviteeName   string
	StartTimestamp int64
	EndTimestamp   int64
}

type RebateStatistics struct {
	InviteeId     int     `json:"invitee_id"`
	RechargeTotal float64 `json:"recharge_total"`
	RebateTotal   float64 `json:"rebate_total"`
}

func (rebateRecord *RebateRecord) Insert() error {
	var err error
	err = DB.Create(rebateRecord).Error
	return err
}

func (rebateRecord *RebateRecord) Update() error {
	var err error
	err = DB.Save(rebateRecord).Error
	return err
}