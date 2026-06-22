package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	"github.com/Wei-Shaw/sub2api/ent/teaminvitation"
	"github.com/Wei-Shaw/sub2api/ent/teammember"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
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

	// owner defaults to the actor (user self-service); an admin creating a team on
	// behalf of someone else sets OwnerUserID. The creator is always the actor.
	owner := input.ActorUserID
	if input.OwnerUserID > 0 {
		owner = input.OwnerUserID
	}

	created, err := tx.Team.Create().
		SetName(input.Name).
		SetSlug(input.Slug).
		SetOwnerUserID(owner).
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
		SetUserID(owner).
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

// AdminListTeams returns a paginated platform-admin view of teams, enriched with
// the owner user, the team billing-subject balance and the active member count.
//
// The owner user and billing subject are eager-loaded via edges; the active
// member count is computed by eager-loading active, non-deleted members and
// counting them per team. Search matches name OR slug (case-insensitive, via
// ContainsFold → ILIKE on Postgres); status is an optional exact match. Only
// non-deleted teams are returned.
func (r *teamRepository) AdminListTeams(ctx context.Context, filter service.AdminTeamListFilter, params pagination.PaginationParams) ([]service.AdminTeamSummary, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)

	q := client.Team.Query().Where(dbteam.DeletedAtIsNil())
	if filter.Status != "" {
		q = q.Where(dbteam.StatusEQ(filter.Status))
	}
	if filter.Search != "" {
		q = q.Where(dbteam.Or(
			dbteam.NameContainsFold(filter.Search),
			dbteam.SlugContainsFold(filter.Search),
		))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	rows, err := q.
		WithOwner().
		WithBillingSubject().
		WithMembers(func(mq *dbent.TeamMemberQuery) {
			mq.Where(
				teammember.DeletedAtIsNil(),
				teammember.StatusEQ(domain.TeamMemberStatusActive),
			)
		}).
		Order(dbent.Desc(dbteam.FieldCreatedAt)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := make([]service.AdminTeamSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, *adminTeamSummaryFromEntity(row))
	}
	return out, paginationResultFromTotal(int64(total), params), nil
}

func (r *teamRepository) GetTeamByID(ctx context.Context, teamID int64) (*service.Team, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.Team.Query().
		Where(dbteam.IDEQ(teamID), dbteam.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamNotFound
	}
	return teamEntityToService(row), nil
}

func (r *teamRepository) AdminGetTeamSummary(ctx context.Context, teamID int64) (*service.AdminTeamSummary, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.Team.Query().
		Where(dbteam.IDEQ(teamID), dbteam.DeletedAtIsNil()).
		WithOwner().
		WithBillingSubject().
		WithMembers(func(mq *dbent.TeamMemberQuery) {
			mq.Where(
				teammember.DeletedAtIsNil(),
				teammember.StatusEQ(domain.TeamMemberStatusActive),
			)
		}).
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamNotFound
	}
	return adminTeamSummaryFromEntity(row), nil
}

// AddMember creates an active membership for (teamID, userID). If a soft-deleted
// or left membership row already exists it is reactivated (role/status reset,
// deleted_at cleared, joined_at refreshed) rather than inserting a duplicate. An
// existing active membership is rejected with service.ErrTeamMemberExists. The
// returned member has its User edge populated.
func (r *teamRepository) AddMember(ctx context.Context, teamID, userID int64, role string, invitedByUserID int64) (*service.TeamMember, error) {
	client := clientFromContext(ctx, r.client)
	now := time.Now()

	// An active (non-deleted) membership blocks adding a duplicate. The unique
	// index on (team_id, user_id) is partial (WHERE deleted_at IS NULL), so at
	// most one such row can exist.
	active, err := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		Only(ctx)
	if err == nil && active != nil {
		return nil, service.ErrTeamMemberExists
	}
	if err != nil && !dbent.IsNotFound(err) {
		return nil, err
	}

	// Reactivate the most recent soft-deleted/left row if one exists; multiple
	// soft-deleted rows may coexist because the unique index excludes them.
	deleted, derr := client.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtNotNil()).
		Order(dbent.Desc(teammember.FieldID)).
		First(mixins.SkipSoftDelete(ctx))
	if derr != nil && !dbent.IsNotFound(derr) {
		return nil, derr
	}
	if deleted != nil {
		upd := client.TeamMember.UpdateOneID(deleted.ID).
			SetRole(role).
			SetStatus(domain.TeamMemberStatusActive).
			ClearDeletedAt().
			SetJoinedAt(now)
		if invitedByUserID > 0 {
			upd.SetInvitedByUserID(invitedByUserID)
		}
		updated, uerr := upd.Save(ctx)
		if uerr != nil {
			return nil, uerr
		}
		return r.loadMemberWithUser(ctx, updated.ID)
	}

	create := client.TeamMember.Create().
		SetTeamID(teamID).
		SetUserID(userID).
		SetRole(role).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(now)
	if invitedByUserID > 0 {
		create.SetInvitedByUserID(invitedByUserID)
	}
	created, cerr := create.Save(ctx)
	if cerr != nil {
		return nil, cerr
	}
	return r.loadMemberWithUser(ctx, created.ID)
}

func (r *teamRepository) loadMemberWithUser(ctx context.Context, memberID int64) (*service.TeamMember, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.TeamMember.Query().
		Where(teammember.IDEQ(memberID)).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, err
	}
	m := teamMemberEntityToService(row)
	if row.Edges.User != nil {
		m.User = userEntityToService(row.Edges.User)
	}
	return m, nil
}

// GetInvitationByTokenHash returns the pending, non-deleted invitation whose
// token_hash matches. service.ErrTeamInvitationInvalid is returned when none
// matches (unknown/expired-status/revoked/deleted tokens are indistinguishable
// to the caller by design).
func (r *teamRepository) GetInvitationByTokenHash(ctx context.Context, tokenHash string) (*service.TeamInvitation, error) {
	client := clientFromContext(ctx, r.client)
	row, err := client.TeamInvitation.Query().
		Where(
			teaminvitation.TokenHashEQ(tokenHash),
			teaminvitation.StatusEQ(domain.TeamInvitationStatusPending),
			teaminvitation.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamInvitationInvalid
	}
	return teamInvitationEntityToService(row), nil
}

// AcceptInvitation creates-or-reactivates an active membership for the accepting
// user (role + invited_by taken from the invitation) and marks the invitation
// accepted, in a single transaction. It mirrors AddMember's reactivation pattern.
// It is idempotent when the same user re-accepts an already-accepted invitation:
// the existing membership is returned without further mutation.
func (r *teamRepository) AcceptInvitation(ctx context.Context, invitationID, acceptingUserID, teamID int64, role string) (*service.TeamMember, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	invitation, err := tx.TeamInvitation.Get(ctx, invitationID)
	if err != nil {
		return nil, service.ErrTeamInvitationInvalid
	}

	// Idempotency: an invitation already accepted by this same user just returns the
	// existing active membership (no re-mutation). An invitation accepted by someone
	// else, or otherwise no longer pending, is rejected.
	if invitation.Status != domain.TeamInvitationStatusPending {
		if invitation.Status == domain.TeamInvitationStatusAccepted &&
			invitation.AcceptedByUserID != nil && *invitation.AcceptedByUserID == acceptingUserID {
			member, merr := txLoadActiveMember(ctx, tx, teamID, acceptingUserID)
			if merr != nil {
				return nil, merr
			}
			if cerr := tx.Commit(); cerr != nil {
				return nil, cerr
			}
			return member, nil
		}
		return nil, service.ErrTeamInvitationExpired
	}

	invitedBy := invitation.InvitedByUserID
	member, err := txUpsertActiveMember(ctx, tx, teamID, acceptingUserID, role, invitedBy)
	if err != nil {
		return nil, err
	}

	upd := tx.TeamInvitation.UpdateOneID(invitationID).
		SetStatus(domain.TeamInvitationStatusAccepted).
		SetAcceptedByUserID(acceptingUserID)
	if _, err := upd.Save(ctx); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return member, nil
}

// txUpsertActiveMember creates-or-reactivates an active membership inside a
// transaction, mirroring teamRepository.AddMember's logic (reactivate the most
// recent soft-deleted/left row, else insert). Unlike AddMember it does NOT reject
// an existing active membership — it returns it as-is (idempotent join), which is
// the desired behavior for accept.
func txUpsertActiveMember(ctx context.Context, tx *dbent.Tx, teamID, userID int64, role string, invitedByUserID int64) (*service.TeamMember, error) {
	now := time.Now()

	if active, err := tx.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		Only(ctx); err == nil && active != nil {
		return txLoadMemberWithUser(ctx, tx, active.ID)
	} else if err != nil && !dbent.IsNotFound(err) {
		return nil, err
	}

	deleted, derr := tx.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtNotNil()).
		Order(dbent.Desc(teammember.FieldID)).
		First(mixins.SkipSoftDelete(ctx))
	if derr != nil && !dbent.IsNotFound(derr) {
		return nil, derr
	}
	if deleted != nil {
		upd := tx.TeamMember.UpdateOneID(deleted.ID).
			SetRole(role).
			SetStatus(domain.TeamMemberStatusActive).
			ClearDeletedAt().
			SetJoinedAt(now)
		if invitedByUserID > 0 {
			upd.SetInvitedByUserID(invitedByUserID)
		}
		updated, uerr := upd.Save(ctx)
		if uerr != nil {
			return nil, uerr
		}
		return txLoadMemberWithUser(ctx, tx, updated.ID)
	}

	create := tx.TeamMember.Create().
		SetTeamID(teamID).
		SetUserID(userID).
		SetRole(role).
		SetStatus(domain.TeamMemberStatusActive).
		SetJoinedAt(now)
	if invitedByUserID > 0 {
		create.SetInvitedByUserID(invitedByUserID)
	}
	created, cerr := create.Save(ctx)
	if cerr != nil {
		return nil, cerr
	}
	return txLoadMemberWithUser(ctx, tx, created.ID)
}

// txLoadActiveMember loads the active (non-deleted) membership for (teamID,userID)
// within a tx, returning service.ErrTeamMembershipNotFound when absent.
func txLoadActiveMember(ctx context.Context, tx *dbent.Tx, teamID, userID int64) (*service.TeamMember, error) {
	row, err := tx.TeamMember.Query().
		Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(userID), teammember.DeletedAtIsNil()).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, service.ErrTeamMembershipNotFound
	}
	m := teamMemberEntityToService(row)
	if row.Edges.User != nil {
		m.User = userEntityToService(row.Edges.User)
	}
	return m, nil
}

// txLoadMemberWithUser loads a membership (with its User edge) by id within a tx.
func txLoadMemberWithUser(ctx context.Context, tx *dbent.Tx, memberID int64) (*service.TeamMember, error) {
	row, err := tx.TeamMember.Query().
		Where(teammember.IDEQ(memberID)).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, err
	}
	m := teamMemberEntityToService(row)
	if row.Edges.User != nil {
		m.User = userEntityToService(row.Edges.User)
	}
	return m, nil
}

// TransferOwnership promotes newOwnerUserID to owner and demotes prevOwnerUserID
// to admin, then sets teams.owner_user_id = newOwnerUserID, in a single
// transaction. The new owner must be an active, non-deleted member; otherwise
// service.ErrTeamMembershipNotFound is returned.
func (r *teamRepository) TransferOwnership(ctx context.Context, teamID, newOwnerUserID, prevOwnerUserID int64) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	newOwner, err := tx.TeamMember.Query().
		Where(
			teammember.TeamIDEQ(teamID),
			teammember.UserIDEQ(newOwnerUserID),
			teammember.DeletedAtIsNil(),
			teammember.StatusEQ(domain.TeamMemberStatusActive),
		).
		Only(ctx)
	if err != nil {
		return service.ErrTeamMembershipNotFound
	}

	if _, err := tx.TeamMember.UpdateOneID(newOwner.ID).
		SetRole(domain.TeamRoleOwner).
		Save(ctx); err != nil {
		return err
	}

	// Demote the previous owner's membership to admin (kept in the team). The row
	// may be absent in pathological cases (e.g. a manually-edited owner_user_id); a
	// missing row is tolerated so the transfer still completes.
	if prevOwnerUserID > 0 && prevOwnerUserID != newOwnerUserID {
		if prevOwner, perr := tx.TeamMember.Query().
			Where(teammember.TeamIDEQ(teamID), teammember.UserIDEQ(prevOwnerUserID), teammember.DeletedAtIsNil()).
			Only(ctx); perr == nil && prevOwner != nil {
			if _, err := tx.TeamMember.UpdateOneID(prevOwner.ID).
				SetRole(domain.TeamRoleAdmin).
				Save(ctx); err != nil {
				return err
			}
		} else if perr != nil && !dbent.IsNotFound(perr) {
			return perr
		}
	}

	if _, err := tx.Team.UpdateOneID(teamID).
		SetOwnerUserID(newOwnerUserID).
		Save(ctx); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *teamRepository) UpdateTeam(ctx context.Context, teamID int64, name *string, status *string) (*service.Team, error) {
	client := clientFromContext(ctx, r.client)
	if _, err := client.Team.Query().Where(dbteam.IDEQ(teamID), dbteam.DeletedAtIsNil()).Only(ctx); err != nil {
		return nil, service.ErrTeamNotFound
	}
	upd := client.Team.UpdateOneID(teamID)
	if name != nil {
		upd.SetName(*name)
	}
	if status != nil {
		upd.SetStatus(*status)
	}
	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	return teamEntityToService(updated), nil
}

func adminTeamSummaryFromEntity(row *dbent.Team) *service.AdminTeamSummary {
	if row == nil {
		return nil
	}
	summary := &service.AdminTeamSummary{
		Team:        *teamEntityToService(row),
		MemberCount: len(row.Edges.Members),
	}
	if row.Edges.Owner != nil {
		summary.OwnerUser = userEntityToService(row.Edges.Owner)
	}
	if row.Edges.BillingSubject != nil {
		summary.Balance = row.Edges.BillingSubject.Balance
	}
	return summary
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
		CreatedAt:        row.CreatedAt,
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
