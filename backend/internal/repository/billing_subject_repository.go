package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type billingSubjectRepository struct {
	client *dbent.Client
}

func NewBillingSubjectRepository(client *dbent.Client) service.BillingSubjectRepository {
	return &billingSubjectRepository{client: client}
}

func (r *billingSubjectRepository) GetByID(ctx context.Context, id int64) (*service.BillingSubject, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.BillingSubject.Query().
		Where(billingsubject.IDEQ(id), billingsubject.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return billingSubjectEntityToService(row), nil
}

func (r *billingSubjectRepository) GetPersonalByUserID(ctx context.Context, userID int64) (*service.BillingSubject, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.BillingSubject.Query().
		Where(
			billingsubject.TypeEQ(domain.BillingSubjectTypeUser),
			billingsubject.UserIDEQ(userID),
			billingsubject.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	return billingSubjectEntityToService(row), nil
}

func (r *billingSubjectRepository) EnsurePersonalForUser(ctx context.Context, user *service.User) (*service.BillingSubject, error) {
	if user == nil || user.ID <= 0 {
		return nil, service.ErrUserNotFound
	}
	existing, err := r.GetPersonalByUserID(ctx, user.ID)
	if err == nil {
		return existing, nil
	}
	client := clientFromContext(ctx, r.client)
	row, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeUser).
		SetUserID(user.ID).
		SetStatus(user.Status).
		SetBalance(user.Balance).
		SetTotalRecharged(user.TotalRecharged).
		SetConcurrency(user.Concurrency).
		SetRpmLimit(user.RPMLimit).
		SetBalanceNotifyEnabled(user.BalanceNotifyEnabled).
		SetBalanceNotifyThresholdType(user.BalanceNotifyThresholdType).
		SetNillableBalanceNotifyThreshold(user.BalanceNotifyThreshold).
		SetBalanceNotifyExtraEmails(service.MarshalNotifyEmails(user.BalanceNotifyExtraEmails)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return billingSubjectEntityToService(row), nil
}

func (r *billingSubjectRepository) CreateTeamSubject(ctx context.Context, teamID int64, seed service.BillingSubject) (*service.BillingSubject, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeTeam).
		SetTeamID(teamID).
		SetStatus(domain.StatusActive).
		SetBalance(seed.Balance).
		SetTotalRecharged(seed.TotalRecharged).
		SetConcurrency(seed.Concurrency).
		SetRpmLimit(seed.RPMLimit).
		SetBalanceNotifyEnabled(seed.BalanceNotifyEnabled).
		SetBalanceNotifyThresholdType(seed.BalanceNotifyThresholdType).
		SetNillableBalanceNotifyThreshold(seed.BalanceNotifyThreshold).
		SetBalanceNotifyExtraEmails(service.MarshalNotifyEmails(seed.BalanceNotifyExtraEmails)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return billingSubjectEntityToService(row), nil
}

func (r *billingSubjectRepository) UpdateBalance(ctx context.Context, subjectID int64, delta float64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.BillingSubject.UpdateOneID(subjectID).
		AddBalance(delta).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	return err
}

func (r *billingSubjectRepository) DeductBalance(ctx context.Context, subjectID int64, amount float64) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.BillingSubject.UpdateOneID(subjectID).
		AddBalance(-amount).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	return err
}

func billingSubjectEntityToService(row *dbent.BillingSubject) *service.BillingSubject {
	if row == nil {
		return nil
	}
	return &service.BillingSubject{
		ID:                         row.ID,
		Type:                       row.Type,
		UserID:                     row.UserID,
		TeamID:                     row.TeamID,
		Status:                     row.Status,
		Balance:                    row.Balance,
		TotalRecharged:             row.TotalRecharged,
		Concurrency:                row.Concurrency,
		RPMLimit:                   row.RpmLimit,
		BalanceNotifyEnabled:       row.BalanceNotifyEnabled,
		BalanceNotifyThresholdType: row.BalanceNotifyThresholdType,
		BalanceNotifyThreshold:     row.BalanceNotifyThreshold,
		BalanceNotifyExtraEmails:   service.ParseNotifyEmails(row.BalanceNotifyExtraEmails),
	}
}
