//go:build unit

package domain_test

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTeamRolePermissions_DissolveAndTransferOwnerOnly(t *testing.T) {
	owner := domain.TeamRolePermissions(domain.TeamRoleOwner)
	require.True(t, owner[domain.TeamPermissionDissolveTeam], "owner can dissolve")
	require.True(t, owner[domain.TeamPermissionTransferOwnership], "owner can transfer ownership")

	admin := domain.TeamRolePermissions(domain.TeamRoleAdmin)
	require.False(t, admin[domain.TeamPermissionDissolveTeam], "admin cannot dissolve")
	require.False(t, admin[domain.TeamPermissionTransferOwnership], "admin cannot transfer ownership")
	require.True(t, admin[domain.TeamPermissionManageSettings], "admin can manage settings")
}
