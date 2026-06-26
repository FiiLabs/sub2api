//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestUpdateTeamSettings_OwnerCanRename(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Old"})
	require.NoError(t, err)

	name := "Renamed"
	updated, err := svc.UpdateTeamSettings(context.Background(), 7, team.ID, UpdateTeamSettingsInput{Name: &name})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
}

func TestUpdateTeamSettings_NonMemberDenied(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Old"})
	require.NoError(t, err)

	name := "X"
	_, err = svc.UpdateTeamSettings(context.Background(), 999, team.ID, UpdateTeamSettingsInput{Name: &name})
	require.Error(t, err)
}

func TestUpdateTeamSettings_EmptyRejected(t *testing.T) {
	repo := newTeamRepoMemory()
	svc := NewTeamService(repo, nil, nil)
	team, err := svc.CreateTeam(context.Background(), CreateTeamInput{ActorUserID: 7, Name: "Old"})
	require.NoError(t, err)

	_, err = svc.UpdateTeamSettings(context.Background(), 7, team.ID, UpdateTeamSettingsInput{Name: nil})
	require.Error(t, err)
}

// dissolveRepoStub 内嵌 TeamRepository，仅覆写解散链路所需的几个方法，
// 用于精确控制成员角色、活跃 key 数与被解散的团队 id，便于断言守卫语义。
type dissolveRepoStub struct {
	TeamRepository
	member         *TeamMember
	team           *Team
	activeKeys     int
	dissolved      []int64
	dissolvedTeamID int64 // 记录最近一次 DissolveTeam 调用的 teamID（供 AdminDeleteTeam 测试使用）
}

func (s *dissolveRepoStub) GetMembership(_ context.Context, _, _ int64) (*TeamMember, error) {
	return s.member, nil
}
func (s *dissolveRepoStub) GetTeamByID(_ context.Context, _ int64) (*Team, error) { return s.team, nil }
func (s *dissolveRepoStub) CountActiveAPIKeysByBillingSubjectID(_ context.Context, _ int64) (int, error) {
	return s.activeKeys, nil
}
func (s *dissolveRepoStub) DissolveTeam(_ context.Context, teamID int64) error {
	s.dissolved = append(s.dissolved, teamID)
	s.dissolvedTeamID = teamID
	return nil
}

func (s *dissolveRepoStub) UsageByMember(_ context.Context, _ int64, _, _ time.Time) ([]TeamMemberUsage, error) {
	return nil, nil
}

// dissolveSubjectStub 内嵌 BillingSubjectRepository，仅覆写 GetByID 以返回可配置余额。
type dissolveSubjectStub struct {
	BillingSubjectRepository
	balance float64
}

func (s *dissolveSubjectStub) GetByID(_ context.Context, _ int64) (*BillingSubject, error) {
	return &BillingSubject{Balance: s.balance}, nil
}

func dissolvePtrInt64(v int64) *int64 { return &v }

func newDissolveService(role string, balance float64, activeKeys int) (*TeamService, *dissolveRepoStub) {
	repo := &dissolveRepoStub{
		member:     &TeamMember{Role: role, Status: domain.TeamMemberStatusActive},
		team:       &Team{ID: 1, BillingSubjectID: dissolvePtrInt64(900)},
		activeKeys: activeKeys,
	}
	svc := NewTeamService(repo, &dissolveSubjectStub{balance: balance}, nil)
	return svc, repo
}

func TestDissolveTeam_OwnerCleanSucceeds(t *testing.T) {
	svc, repo := newDissolveService(domain.TeamRoleOwner, 0, 0)
	require.NoError(t, svc.DissolveTeam(context.Background(), 7, 1))
	require.Equal(t, []int64{1}, repo.dissolved)
}

func TestDissolveTeam_AdminDenied(t *testing.T) {
	svc, repo := newDissolveService(domain.TeamRoleAdmin, 0, 0)
	require.ErrorIs(t, svc.DissolveTeam(context.Background(), 7, 1), ErrTeamPermissionDenied)
	require.Empty(t, repo.dissolved)
}

func TestDissolveTeam_RejectsWithBalance(t *testing.T) {
	svc, repo := newDissolveService(domain.TeamRoleOwner, 5, 0)
	require.ErrorIs(t, svc.DissolveTeam(context.Background(), 7, 1), ErrTeamDissolveHasBalance)
	require.Empty(t, repo.dissolved)
}

func TestDissolveTeam_RejectsWithActiveKeys(t *testing.T) {
	svc, repo := newDissolveService(domain.TeamRoleOwner, 0, 2)
	require.ErrorIs(t, svc.DissolveTeam(context.Background(), 7, 1), ErrTeamDissolveHasActiveKeys)
	require.Empty(t, repo.dissolved)
}

func TestAdminDeleteTeamCallsRepoDissolveWithoutGuards(t *testing.T) {
	repo := &dissolveRepoStub{}
	svc := NewTeamService(repo, nil, nil)
	require.NoError(t, svc.AdminDeleteTeam(context.Background(), 7))
	require.Equal(t, int64(7), repo.dissolvedTeamID)
}
