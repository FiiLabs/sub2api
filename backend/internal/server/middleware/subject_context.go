package middleware

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const SubjectHeader = "X-Sub2API-Subject-ID"
const TeamHeader = "X-Sub2API-Team-ID"

type WorkspaceLister interface {
	ListWorkspaces(ctx context.Context, userID int64) ([]service.WorkspaceSubject, error)
}

func SubjectContextMiddleware(workspaces WorkspaceLister) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "User not authenticated")
			c.Abort()
			return
		}
		resolved, err := resolveWorkspaceSubject(
			c.Request.Context(),
			workspaces,
			subject.UserID,
			c.GetHeader(SubjectHeader),
			c.GetHeader(TeamHeader),
		)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		subject.BillingSubjectID = resolved.BillingSubjectID
		subject.SubjectType = resolved.Type
		subject.TeamID = resolved.TeamID
		subject.TeamRole = resolved.Role
		subject.Permissions = resolved.Permissions
		c.Set(string(ContextKeyUser), subject)
		c.Next()
	}
}

func resolveWorkspaceSubject(
	ctx context.Context,
	workspaces WorkspaceLister,
	userID int64,
	subjectHeader string,
	teamHeader string,
) (service.WorkspaceSubject, error) {
	items, err := workspaces.ListWorkspaces(ctx, userID)
	if err != nil {
		return service.WorkspaceSubject{}, err
	}
	if len(items) == 0 {
		return service.WorkspaceSubject{}, service.ErrUserNotFound
	}

	wantSubjectID, hasSubjectID, err := parseSelectorHeader(subjectHeader, SubjectHeader)
	if err != nil {
		return service.WorkspaceSubject{}, err
	}
	wantTeamID, hasTeamID, err := parseSelectorHeader(teamHeader, TeamHeader)
	if err != nil {
		return service.WorkspaceSubject{}, err
	}
	for _, item := range items {
		if hasSubjectID && item.BillingSubjectID == wantSubjectID {
			return item, nil
		}
		if hasTeamID && item.TeamID == wantTeamID {
			return item, nil
		}
	}
	// An unmatched selector means the client asked for a workspace this user no
	// longer has: stale localStorage carried over from another account, a team
	// they were removed from, or a deleted workspace. Returning 403 here would
	// hard-fail every authenticated route — including /workspaces itself, the
	// endpoint that would let the client correct its selector — trapping the user
	// with no way to recover. Fall back to the default workspace instead. The
	// resolved subject is always drawn from this user's own ListWorkspaces result,
	// so a bad selector can never widen access beyond what they already have.

	for _, item := range items {
		if item.Type == domain.BillingSubjectTypeUser {
			return item, nil
		}
	}
	return items[0], nil
}

func parseSelectorHeader(raw string, headerName string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false, infraerrors.BadRequest("INVALID_WORKSPACE_SELECTOR", headerName+" must be a positive integer")
	}
	return value, true, nil
}
