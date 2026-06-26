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
	// Idempotency, layer 1 (check-first): if a personal subject already exists
	// just return it. This is the common path and the guarantee exercised by
	// repeated/self-heal calls.
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
		// Idempotency, layer 2 (race backstop): a concurrent caller may have
		// inserted the personal subject between the check above and this insert.
		// The partial unique index (idx_billing_subjects_user_unique, on
		// type='user' AND deleted_at IS NULL) rejects the duplicate on Postgres;
		// re-read and return the winner instead of surfacing the conflict. Safe
		// here because this method runs outside a transaction (the self-heal read
		// path), so the failed insert does not poison a surrounding tx.
		if dbent.IsConstraintError(err) {
			if winner, reErr := r.GetPersonalByUserID(ctx, user.ID); reErr == nil {
				return winner, nil
			}
		}
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

// UpdateLimits 设置团队主体的并发与 RPM 上限（0 = 不限制）。
func (r *billingSubjectRepository) UpdateLimits(ctx context.Context, subjectID int64, concurrency, rpmLimit int) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.BillingSubject.UpdateOneID(subjectID).
		SetConcurrency(concurrency).
		SetRpmLimit(rpmLimit).
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
