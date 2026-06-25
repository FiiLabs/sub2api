//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// usageByMemberRepoStub 内嵌 TeamRepository，仅覆写 UsageByMember 链路所需的方法。
// GetMembership 用于控制 Require 的权限检查结果。
type usageByMemberRepoStub struct {
	TeamRepository
	member        *TeamMember
	memberErr     error
	usageRows     []TeamMemberUsage
	usageErr      error
	usageByMemberCalled bool
}

func (s *usageByMemberRepoStub) GetMembership(_ context.Context, _, _ int64) (*TeamMember, error) {
	return s.member, s.memberErr
}

func (s *usageByMemberRepoStub) UsageByMember(_ context.Context, _ int64, _, _ time.Time) ([]TeamMemberUsage, error) {
	s.usageByMemberCalled = true
	return s.usageRows, s.usageErr
}

// TestUsageByMemberRequiresViewAll 验证：developer 角色（无 team.usage.view.all）→ 返回
// ErrTeamPermissionDenied，且 repo.UsageByMember 未被调用。
func TestUsageByMemberRequiresViewAll(t *testing.T) {
	stub := &usageByMemberRepoStub{
		member: &TeamMember{
			Role:   domain.TeamRoleDeveloper,
			Status: domain.TeamMemberStatusActive,
		},
	}
	svc := NewTeamService(stub, nil, nil)

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	_, err := svc.UsageByMember(context.Background(), 42, 1, start, end)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTeamPermissionDenied), "expected ErrTeamPermissionDenied, got %v", err)
	require.False(t, stub.usageByMemberCalled, "repo.UsageByMember must not be called when permission is denied")
}

// TestUsageByMemberReturnsAggregates 验证：owner 通过权限检查后，repo.UsageByMember
// 的返回值被透传（不被裁剪或修改）。
func TestUsageByMemberReturnsAggregates(t *testing.T) {
	expected := []TeamMemberUsage{
		{UserID: 10, Requests: 5, TotalCost: 1.5, ActualCost: 1.2},
		{UserID: 20, Requests: 3, TotalCost: 0.9, ActualCost: 0.8},
	}
	stub := &usageByMemberRepoStub{
		member: &TeamMember{
			Role:   domain.TeamRoleOwner,
			Status: domain.TeamMemberStatusActive,
		},
		usageRows: expected,
	}
	svc := NewTeamService(stub, nil, nil)

	start := time.Now().Add(-7 * 24 * time.Hour)
	end := time.Now()
	got, err := svc.UsageByMember(context.Background(), 7, 1, start, end)

	require.NoError(t, err)
	require.True(t, stub.usageByMemberCalled, "repo.UsageByMember must be called when owner requests")
	require.Equal(t, expected, got)
}
