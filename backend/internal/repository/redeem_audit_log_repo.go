package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type redeemAuditLogRepository struct {
	client *dbent.Client
}

// NewRedeemAuditLogRepository 创建兑换审计流水 repository。
func NewRedeemAuditLogRepository(client *dbent.Client) service.RedeemAuditLogRepository {
	return &redeemAuditLogRepository{client: client}
}

func (r *redeemAuditLogRepository) Create(ctx context.Context, log *service.RedeemAuditLog) error {
	client := clientFromContext(ctx, r.client)
	b := client.RedeemAuditLog.Create().
		SetRedeemCodeID(log.RedeemCodeID).
		SetCode(log.Code).
		SetActorUserID(log.ActorUserID).
		SetCodeType(log.CodeType).
		SetAmount(log.Amount)
	if log.BillingSubjectID > 0 {
		b = b.SetBillingSubjectID(log.BillingSubjectID)
	}
	_, err := b.Save(ctx)
	return err
}
