//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRequireTeamBillingManage(t *testing.T) {
	tests := []struct {
		name    string
		subject middleware2.AuthSubject
		wantErr bool
	}{
		{"personal always allowed", middleware2.AuthSubject{SubjectType: domain.BillingSubjectTypeUser}, false},
		{"team with billing manage", middleware2.AuthSubject{SubjectType: domain.BillingSubjectTypeTeam, Permissions: map[string]bool{domain.TeamPermissionManageBilling: true}}, false},
		{"team without billing manage", middleware2.AuthSubject{SubjectType: domain.BillingSubjectTypeTeam, Permissions: map[string]bool{domain.TeamPermissionViewUsage: true}}, true},
		{"team nil permissions", middleware2.AuthSubject{SubjectType: domain.BillingSubjectTypeTeam}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireTeamBillingManage(tt.subject)
			if tt.wantErr {
				require.ErrorIs(t, err, service.ErrTeamPermissionDenied)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
