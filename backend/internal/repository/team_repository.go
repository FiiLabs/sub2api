package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	"github.com/Wei-Shaw/sub2api/ent/teaminvitation"
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

// ListMembers returns active/suspended members (with their User loaded) and
// pending invitations for a team.
//
// NOTE: aggregate fields key_count and last_7d_actual_cost are intentionally
// left unset (zero) here. Those metrics require cross-aggregating the api_keys
// and usage_logs tables per member/subject which would add several joins/queries
// to this hot path; the plan permits returning members+invitations with the
// aggregates zeroed. They can be layered on later via a dedicated stats query.
func (r *teamRepository) ListMembers(ctx context.Context, teamID int64) ([]service.TeamMember, []service.TeamInvitation, error) {
	client := clientFromContext(ctx, r.client)

	memberRows, err := client.TeamMember.Query().
		Where(
			teammember.TeamIDEQ(teamID),
			teammember.DeletedAtIsNil(),
			teammember.StatusNEQ(domain.TeamMemberStatusLeft),
		).
		WithUser().
		Order(dbent.Asc(teammember.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	members := make([]service.TeamMember, 0, len(memberRows))
	for _, row := range memberRows {
		m := teamMemberEntityToService(row)
		if row.Edges.User != nil {
			m.User = userEntityToService(row.Edges.User)
		}
		members = append(members, *m)
	}

	inviteRows, err := client.TeamInvitation.Query().
		Where(
			teaminvitation.TeamIDEQ(teamID),
			teaminvitation.DeletedAtIsNil(),
			teaminvitation.StatusEQ(domain.TeamInvitationStatusPending),
		).
		Order(dbent.Asc(teaminvitation.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	invitations := make([]service.TeamInvitation, 0, len(inviteRows))
	for _, row := range inviteRows {
		invitations = append(invitations, *teamInvitationEntityToService(row))
	}

	return members, invitations, nil
}

func (r *teamRepository) InviteMember(ctx context.Context, input service.InviteTeamMemberInput) (*service.TeamInvitation, error) {
	client := clientFromContext(ctx, r.client)
	created, err := client.TeamInvitation.Create().
		SetTeamID(input.TeamID).
		SetEmail(input.Email).
		SetRole(input.Role).
		SetTokenHash(input.TokenHash).
		SetStatus(domain.TeamInvitationStatusPending).
		SetInvitedByUserID(input.ActorUserID).
		SetExpiresAt(input.ExpiresAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return teamInvitationEntityToService(created), nil
}

func (r *teamRepository) UpdateMember(ctx context.Context, actorUserID, teamID, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamMembershipNotFound
	}
	upd := client.TeamMember.UpdateOneID(row.ID)
	if input.Role != nil {
		upd.SetRole(*input.Role)
	}
	if input.Status != nil {
		upd.SetStatus(*input.Status)
	}
	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	return teamMemberEntityToService(updated), nil
}

func (r *teamRepository) RemoveMember(ctx context.Context, actorUserID, teamID, userID int64) error {
	client := clientFromContext(ctx, r.client)
	row, err := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return service.ErrTeamMembershipNotFound
	}
	_, err = client.TeamMember.UpdateOneID(row.ID).
		SetStatus(domain.TeamMemberStatusLeft).
		SetDeletedAt(time.Now()).
		Save(ctx)
	return err
}

func teamInvitationEntityToService(row *dbent.TeamInvitation) *service.TeamInvitation {
	if row == nil {
		return nil
	}
	return &service.TeamInvitation{
		ID:               row.ID,
		TeamID:           row.TeamID,
		Email:            row.Email,
		Role:             row.Role,
		TokenHash:        row.TokenHash,
		Status:           row.Status,
		InvitedByUserID:  row.InvitedByUserID,
		AcceptedByUserID: row.AcceptedByUserID,
		ExpiresAt:        row.ExpiresAt,
	}
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
