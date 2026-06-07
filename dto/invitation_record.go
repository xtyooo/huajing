package dto

type InvitationRecordDto struct {
	Id            int          `json:"id"`
	InviterId     int          `json:"inviter_id"`
	InviterName   string       `json:"inviter_name"`
	InviteeId     int          `json:"invitee_id"`
	InviteeName   string       `json:"invitee_name"`
	RechargeTotal float64      `json:"recharge_total"`
	RebateTotal   float64      `json:"rebate_total"`
	CreatedAt     int64        `json:"created_at"`
}