package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	"github.com/Wei-Shaw/sub2api/ent/teammember"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type teamRepository struct {
	client *dbent.Client
}

func NewTeamRepository(client *dbent.Client) service.TeamRepository {
	return &teamRepository{client: client}
}

func (r *teamRepository) CreateTeam(ctx context.Context, input service.CreateTeamInput) (*service.Team, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	created, err := tx.Team.Create().
		SetName(input.Name).
		SetSlug(input.Slug).
		SetOwnerUserID(input.ActorUserID).
		SetCreatedByUserID(input.ActorUserID).
		SetStatus(domain.TeamStatusActive).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	subject, err := tx.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeTeam).
		SetTeamID(created.ID).
		SetStatus(domain.StatusActive).
		SetConcurrency(5).
		SetRpmLimit(0).
		SetBalanceNotifyEnabled(true).
		SetBalanceNotifyThresholdType("fixed").
		SetBalanceNotifyExtraEmails("[]").
		Save(ctx)
	if err != nil {
		return nil, err
	}

	created, err = tx.Team.UpdateOneID(created.ID).
		SetBillingSubjectID(subject.ID).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = tx.TeamMember.Create().
		SetTeamID(created.ID).
		SetUserID(input.ActorUserID).
		SetRole(domain.TeamRoleOwner).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return teamEntityToService(created), nil
}

func (r *teamRepository) GetMembership(ctx context.Context, teamID, userID int64) (*service.TeamMember, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamMembershipNotFound
	}
	return teamMemberEntityToService(row), nil
}

func (r *teamRepository) ListWorkspaces(ctx context.Context, userID int64) ([]service.WorkspaceSubject, error) {
	out := make([]service.WorkspaceSubject, 0, 4)
	client := clientFromContext(ctx, r.client)
	personal, err := client.BillingSubject.Query().
		Where(
			billingsubject.TypeEQ(domain.BillingSubjectTypeUser),
			billingsubject.UserIDEQ(userID),
			billingsubject.DeletedAtIsNil(),
		).
		Only(ctx)
	if err == nil {
		out = append(out, service.WorkspaceSubject{
			BillingSubjectID: personal.ID,
			Type:             domain.BillingSubjectTypeUser,
			UserID:           userID,
			Name:             "Personal",
			Role:             domain.TeamRoleOwner,
			Permissions:      domain.TeamRolePermissions(domain.TeamRoleOwner),
			Balance:          personal.Balance,
		})
	}

	members, err := client.TeamMember.Query().
		Where(
			teammember.UserIDEQ(userID),
			teammember.StatusEQ(domain.TeamMemberStatusActive),
			teammember.DeletedAtIsNil(),
		).
		WithTeam(func(q *dbent.TeamQuery) {
			q.Where(dbteam.DeletedAtIsNil(), dbteam.StatusEQ(domain.TeamStatusActive))
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		t := member.Edges.Team
		if t == nil || t.BillingSubjectID == nil {
			continue
		}
		out = append(out, service.WorkspaceSubject{
			BillingSubjectID: *t.BillingSubjectID,
			Type:             domain.BillingSubjectTypeTeam,
			TeamID:           t.ID,
			Name:             t.Name,
			Role:             member.Role,
			Permissions:      domain.TeamRolePermissions(member.Role),
		})
	}
	return out, nil
}

func teamEntityToService(row *dbent.Team) *service.Team {
	if row == nil {
		return nil
	}
	return &service.Team{
		ID:               row.ID,
		Name:             row.Name,
		Slug:             row.Slug,
		OwnerUserID:      row.OwnerUserID,
		BillingSubjectID: row.BillingSubjectID,
		Status:           row.Status,
		AvatarURL:        row.AvatarURL,
		CreatedByUserID:  row.CreatedByUserID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func teamMemberEntityToService(row *dbent.TeamMember) *service.TeamMember {
	if row == nil {
		return nil
	}
	return &service.TeamMember{
		ID:              row.ID,
		TeamID:          row.TeamID,
		UserID:          row.UserID,
		Role:            row.Role,
		Status:          row.Status,
		InvitedByUserID: row.InvitedByUserID,
		JoinedAt:        row.JoinedAt,
		LastActiveAt:    row.LastActiveAt,
	}
}
