package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// adminTeamServiceStub is a configurable stub implementing AdminTeamService. It
// records the inputs it received so tests can assert routing/argument wiring
// without a real team membership.
type adminTeamServiceStub struct {
	teams []service.AdminTeamSummary

	createTeamInput  *service.AdminCreateTeamInput
	createTeamResult *service.AdminTeamSummary
	createTeamErr    error

	addMemberInput   *service.AdminAddMemberInput
	addMemberResult  *service.TeamMember
	addMemberErr     error
	removeMemberErr  error
	removeMemberArgs [3]int64 // adminUserID, teamID, userID

	transferArgs [2]int64 // teamID, newOwnerUserID
	transferErr  error

	deleteTeamID  int64
	deleteTeamErr error

	// AdminGetTeamOverride 若设置则替代默认实现
	AdminGetTeamOverride func(teamID int64) (*service.AdminTeamSummary, []service.TeamMember, []service.TeamInvitation, error)
}

func (s *adminTeamServiceStub) AdminCreateTeam(_ context.Context, input service.AdminCreateTeamInput) (*service.AdminTeamSummary, error) {
	in := input
	s.createTeamInput = &in
	if s.createTeamErr != nil {
		return nil, s.createTeamErr
	}
	if s.createTeamResult != nil {
		return s.createTeamResult, nil
	}
	return &service.AdminTeamSummary{Team: service.Team{ID: 1, Name: input.Name, Slug: "auto-slug", OwnerUserID: input.OwnerUserID}}, nil
}

func (s *adminTeamServiceStub) AdminListTeams(_ context.Context, _ service.AdminTeamListFilter, params pagination.PaginationParams) ([]service.AdminTeamSummary, *pagination.PaginationResult, error) {
	return s.teams, &pagination.PaginationResult{Total: int64(len(s.teams)), Page: params.Page, PageSize: params.PageSize, Pages: 1}, nil
}

func (s *adminTeamServiceStub) AdminGetTeam(_ context.Context, teamID int64) (*service.AdminTeamSummary, []service.TeamMember, []service.TeamInvitation, error) {
	if s.AdminGetTeamOverride != nil {
		return s.AdminGetTeamOverride(teamID)
	}
	return &service.AdminTeamSummary{Team: service.Team{ID: teamID}}, nil, nil, nil
}

func (s *adminTeamServiceStub) AdminUpdateTeam(_ context.Context, teamID int64, input service.AdminUpdateTeamInput) (*service.AdminTeamSummary, error) {
	summary := &service.AdminTeamSummary{Team: service.Team{ID: teamID}}
	if input.Name != nil {
		summary.Name = *input.Name
	}
	if input.Status != nil {
		summary.Status = *input.Status
	}
	return summary, nil
}

func (s *adminTeamServiceStub) AdminAddMember(_ context.Context, input service.AdminAddMemberInput) (*service.TeamMember, error) {
	in := input
	s.addMemberInput = &in
	if s.addMemberErr != nil {
		return nil, s.addMemberErr
	}
	if s.addMemberResult != nil {
		return s.addMemberResult, nil
	}
	return &service.TeamMember{TeamID: input.TeamID, UserID: input.UserID, Role: input.Role, Status: domain.TeamMemberStatusActive}, nil
}

func (s *adminTeamServiceStub) AdminUpdateMember(_ context.Context, _ int64, teamID, userID int64, input service.UpdateTeamMemberInput) (*service.TeamMember, error) {
	m := &service.TeamMember{TeamID: teamID, UserID: userID, Role: domain.TeamRoleViewer, Status: domain.TeamMemberStatusActive}
	if input.Role != nil {
		m.Role = *input.Role
	}
	if input.Status != nil {
		m.Status = *input.Status
	}
	return m, nil
}

func (s *adminTeamServiceStub) AdminRemoveMember(_ context.Context, adminUserID, teamID, userID int64) error {
	s.removeMemberArgs = [3]int64{adminUserID, teamID, userID}
	return s.removeMemberErr
}

func (s *adminTeamServiceStub) AdminTransferOwnership(_ context.Context, teamID, newOwnerUserID int64) error {
	s.transferArgs = [2]int64{teamID, newOwnerUserID}
	return s.transferErr
}

func (s *adminTeamServiceStub) AdminDeleteTeam(_ context.Context, teamID int64) error {
	s.deleteTeamID = teamID
	return s.deleteTeamErr
}

// adminRouter wires the handler routes the same way the production router does,
// injecting an admin AuthSubject (a platform admin, NOT a team member).
func adminRouter(h *AdminTeamHandler, adminUserID int64) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: adminUserID})
		c.Next()
	})
	teams := router.Group("/api/v1/admin/teams")
	teams.GET("", h.List)
	teams.POST("", h.Create)
	teams.GET("/:id", h.GetByID)
	teams.PATCH("/:id", h.Update)
	teams.POST("/:id/members", h.AddMember)
	teams.PATCH("/:id/members/:user_id", h.UpdateMember)
	teams.DELETE("/:id/members/:user_id", h.RemoveMember)
	teams.POST("/:id/transfer-ownership", h.TransferOwnership)
	teams.DELETE("/:id", h.Delete)
	return router
}

func TestAdminTeamHandlerListWithoutMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := &service.User{ID: 5, Username: "owner", Email: "owner@example.com"}
	stub := &adminTeamServiceStub{teams: []service.AdminTeamSummary{
		{Team: service.Team{ID: 7, Name: "Platform", Slug: "platform", Status: domain.TeamStatusActive, OwnerUserID: 5}, OwnerUser: owner, Balance: 12.5, MemberCount: 3},
	}}
	h := &AdminTeamHandler{teamService: stub}
	// admin user 999 is NOT a member of team 7
	router := adminRouter(h, 999)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/teams?page=1&page_size=20", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"items"`)
	require.Contains(t, body, `"slug":"platform"`)
	require.Contains(t, body, `"member_count":3`)
	require.Contains(t, body, `"balance":12.5`)
	require.Contains(t, body, `"owner":{"id":5`)
	require.Contains(t, body, `"total":1`)
}

func TestAdminTeamHandlerCreateHappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := &service.User{ID: 55, Username: "owner", Email: "owner@example.com"}
	stub := &adminTeamServiceStub{createTeamResult: &service.AdminTeamSummary{
		Team:      service.Team{ID: 9, Name: "Platform", Slug: "platform-ab12cd", Status: domain.TeamStatusActive, OwnerUserID: 55},
		OwnerUser: owner,
	}}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999) // platform admin, not a team member

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams", strings.NewReader(`{"name":"Platform","owner_user_id":55}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"team"`)
	require.Contains(t, body, `"slug":"platform-ab12cd"`)
	require.Contains(t, body, `"owner_user_id":55`)
	require.NotNil(t, stub.createTeamInput)
	require.Equal(t, "Platform", stub.createTeamInput.Name)
	require.Equal(t, int64(55), stub.createTeamInput.OwnerUserID)
	// The admin user id is read from the auth subject and forwarded.
	require.Equal(t, int64(999), stub.createTeamInput.AdminUserID)
}

func TestAdminTeamHandlerCreateRequiresOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams", strings.NewReader(`{"name":"Platform"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// Missing owner_user_id and owner_email is rejected before reaching the service.
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, stub.createTeamInput)
}

func TestAdminTeamHandlerAddMemberWithoutMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999) // admin is not a member of team 7

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/members", strings.NewReader(`{"user_id":42,"role":"developer"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"member"`)
	require.NotNil(t, stub.addMemberInput)
	require.Equal(t, int64(7), stub.addMemberInput.TeamID)
	require.Equal(t, int64(42), stub.addMemberInput.UserID)
	require.Equal(t, domain.TeamRoleDeveloper, stub.addMemberInput.Role)
	// admin user id is read from the auth subject and forwarded as invited_by.
	require.Equal(t, int64(999), stub.addMemberInput.AdminUserID)
}

func TestAdminTeamHandlerAddMemberResolvesByEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/members", strings.NewReader(`{"email":"new@example.com","role":"viewer"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, stub.addMemberInput)
	require.Equal(t, "new@example.com", stub.addMemberInput.Email)
	require.Equal(t, int64(0), stub.addMemberInput.UserID)
	require.Equal(t, domain.TeamRoleViewer, stub.addMemberInput.Role)
}

func TestAdminTeamHandlerAddMemberRejectsOwnerRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/members", strings.NewReader(`{"user_id":42,"role":"owner"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// role=owner is rejected at request binding (oneof) before reaching the service.
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, stub.addMemberInput)
}

func TestAdminTeamHandlerAddMemberRequiresUserOrEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/members", strings.NewReader(`{"role":"viewer"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, stub.addMemberInput)
}

func TestAdminTeamHandlerRemoveOwnerIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{removeMemberErr: service.ErrTeamOwnerImmutable}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/teams/7/members/5", nil)
	router.ServeHTTP(rec, req)

	// ErrTeamOwnerImmutable is a BadRequest application error -> HTTP 400.
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, [3]int64{999, 7, 5}, stub.removeMemberArgs)
}

func TestAdminTeamHandlerRemoveMemberSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/teams/7/members/8", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"member removed"`)
	require.Equal(t, [3]int64{999, 7, 8}, stub.removeMemberArgs)
}

func TestAdminTeamHandlerTransferOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999) // platform admin, not a team member

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/transfer-ownership", strings.NewReader(`{"user_id":8}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "ownership transferred")
	require.Equal(t, [2]int64{7, 8}, stub.transferArgs)
}

func TestAdminTeamHandlerTransferOwnershipRequiresUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub}
	router := adminRouter(h, 999)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/teams/7/transfer-ownership", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// Missing user_id is rejected at request binding.
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, [2]int64{0, 0}, stub.transferArgs)
}

// stubConcurrencyLoader 是 teamConcurrencyLoader 的测试桩，返回预设的负载数据。
type stubConcurrencyLoader struct{ load map[int64]*service.UserLoadInfo }

func (s *stubConcurrencyLoader) GetUsersLoadBatch(_ context.Context, _ []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	return s.load, nil
}

func TestAdminGetTeamFillsMemberCurrentConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// teamService 桩：AdminGetTeam 返回 1 个成员(user_id=5)，团队 Concurrency=10
	teamStub := &adminTeamServiceStub{}
	teamStub.AdminGetTeamOverride = func(teamID int64) (*service.AdminTeamSummary, []service.TeamMember, []service.TeamInvitation, error) {
		summary := &service.AdminTeamSummary{Team: service.Team{ID: teamID}}
		summary.Concurrency = 10
		members := []service.TeamMember{{ID: 1, TeamID: teamID, UserID: 5, Role: "developer", Status: "active"}}
		return summary, members, nil, nil
	}

	loader := &stubConcurrencyLoader{load: map[int64]*service.UserLoadInfo{
		5: {CurrentConcurrency: 3},
	}}

	h := &AdminTeamHandler{teamService: teamStub, concurrencyLoader: loader}
	router := adminRouter(h, 999)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/teams/7", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"current_concurrency":3`)
}

// recordingInvalidator 是 teamAuthCacheInvalidator 的测试桩，记录调用的 teamID。
type recordingInvalidator struct {
	calledTeamID          int64
	deleteTeamKeysTeamID  int64
}

func (r *recordingInvalidator) InvalidateAuthCacheByTeamID(_ context.Context, teamID int64) {
	r.calledTeamID = teamID
}

func (r *recordingInvalidator) DeleteTeamKeys(_ context.Context, teamID int64) error {
	r.deleteTeamKeysTeamID = teamID
	return nil
}

func TestAdminUpdateTeamConcurrencyInvalidatesCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inv := &recordingInvalidator{}
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub, authCacheInvalidator: inv}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/teams/7", strings.NewReader(`{"concurrency":20}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), inv.calledTeamID)
}

func TestAdminDeleteTeamHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inv := &recordingInvalidator{}
	stub := &adminTeamServiceStub{}
	h := &AdminTeamHandler{teamService: stub, authCacheInvalidator: inv}
	router := adminRouter(h, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/teams/7", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "team deleted")
	// keys deleted + cache invalidated BEFORE team dissolved
	require.Equal(t, int64(7), inv.deleteTeamKeysTeamID)
	require.Equal(t, int64(7), stub.deleteTeamID)
}
