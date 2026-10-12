package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"math"
)

type RebateRecord struct {
	Id             int            `json:"id"`
	InviterId      int            `json:"inviter_id" gorm:"index"`
	InviterName    string         `json:"inviter_name" gorm:"type:varchar(191)"`
	InviteeId      int            `json:"invitee_id" gorm:"index:idx_rebate_invitee_created"`
	InviteeName    string         `json:"invitee_name" gorm:"type:varchar(191)"`
	RechargeId     int            `json:"recharge_id"`
	RechargeAmount float64        `json:"recharge_amount"`
	RebateAmount   float64        `json:"rebate_amount"`
	M              float64        `json:"m"`
	N              float64        `json:"n"`
	DeletedAt      gorm.DeletedAt `gorm:"index"`
	CreatedAt      int64          `json:"created_at" gorm:"index:idx_rebate_invitee_created"`
}

type SyncRebateRecordQueryParams struct {
	Id             int
	InviterId      int
	InviterName    string
	InviteeId      int
	InviteeName    string
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

// creditEpayRebate runs under RechargeEpay's locked pending-order transaction.
// The order success marker, both wallet credits and this record commit together.
// Historical successful orders are never revisited or recomputed.
func creditEpayRebate(tx *gorm.DB, topUp *TopUp) (*RebateRecord, int, error) {
	var invitee User
	if err := tx.Select("id", "username", "inviter_id").First(&invitee, topUp.UserId).Error; err != nil {
		return nil, 0, err
	}
	if invitee.InviterId == 0 {
		return nil, 0, nil
	}
	var inviter User
	if err := lockForUpdate(tx).Select("id", "username", "m").First(&inviter, invitee.InviterId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if math.IsNaN(inviter.M) || math.IsInf(inviter.M, 0) || inviter.M < 0 || common.QuotaForInviter < 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) || topUp.Money < 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) || common.QuotaPerUnit <= 0 {
		return nil, 0, fmt.Errorf("invalid invitation rebate configuration")
	}
	m, n := math.Min(inviter.M, 50), math.Min(float64(common.QuotaForInviter), 50)
	percentage := m + n
	if percentage >= 50 {
		percentage = 49.9
	}
	amount := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(percentage)).Div(decimal.NewFromInt(100))
	if amount.LessThan(decimal.NewFromFloat(0.000001)) {
		return nil, 0, nil
	}
	quota, err := common.WalletQuotaFromDecimalStrict(amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		return nil, 0, ErrInvalidTopUpQuota
	}
	if quota == 0 {
		return nil, 0, nil
	}
	// A pending order with any previous record signals inconsistent legacy data;
	// stop for reconciliation rather than silently issuing another rebate.
	var existing int64
	if err := tx.Unscoped().Model(&RebateRecord{}).Where("recharge_id = ?", topUp.Id).Count(&existing).Error; err != nil {
		return nil, 0, err
	}
	if existing != 0 {
		return nil, 0, fmt.Errorf("pending topup already has a rebate record")
	}
	record := &RebateRecord{InviterId: inviter.Id, InviterName: inviter.Username, InviteeId: invitee.Id, InviteeName: invitee.Username, RechargeId: topUp.Id, RechargeAmount: topUp.Money, RebateAmount: amount.InexactFloat64(), M: m, N: n, CreatedAt: common.GetTimestamp()}
	if err := tx.Create(record).Error; err != nil {
		return nil, 0, err
	}
	if err := creditTopUpQuota(tx, inviter.Id, quota, nil); err != nil {
		return nil, 0, err
	}
	return record, quota, nil
}
