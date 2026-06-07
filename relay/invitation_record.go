package relay

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// relay/invitation_record.go
func InvitationRecordModel2Dto(record *model.InvitationRecord) *dto.InvitationRecordDto {
	return &dto.InvitationRecordDto{
		Id:          record.Id,
		InviterId:   record.InviterId,
		InviterName: record.InviterName,
		InviteeId:   record.InviteeId,
		InviteeName: record.InviteeName,
		CreatedAt:   record.CreatedAt,
	}
}
