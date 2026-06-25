package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamRolePermissionsGrantAllScopesToOwnerAndAdmin(t *testing.T) {
	owner := TeamRolePermissions(TeamRoleOwner)
	require.True(t, owner[TeamPermissionManageKeysAll])
	require.True(t, owner[TeamPermissionViewUsageAll])

	admin := TeamRolePermissions(TeamRoleAdmin)
	require.True(t, admin[TeamPermissionManageKeysAll])
	require.True(t, admin[TeamPermissionViewUsageAll])

	dev := TeamRolePermissions(TeamRoleDeveloper)
	require.False(t, dev[TeamPermissionManageKeysAll], "developer must not get manage-all")
	require.True(t, dev[TeamPermissionManageKeys], "developer keeps self key baseline")

	viewer := TeamRolePermissions(TeamRoleViewer)
	require.False(t, viewer[TeamPermissionViewUsageAll], "viewer must not get usage view-all")
	require.True(t, viewer[TeamPermissionViewUsage], "viewer keeps self usage baseline")
}

func TestTeamKeyCreatorFilter(t *testing.T) {
	// personal: no restriction
	require.Nil(t, TeamKeyCreatorFilter(BillingSubjectTypeUser, nil, 42))
	// team member (no manage-all): restrict to self
	got := TeamKeyCreatorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionManageKeys: true}, 42)
	require.NotNil(t, got)
	require.Equal(t, int64(42), *got)
	// team owner/admin (manage-all): no restriction
	require.Nil(t, TeamKeyCreatorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionManageKeysAll: true}, 42))
}

func TestTeamUsageActorFilter(t *testing.T) {
	// personal: passthrough
	f, ok := TeamUsageActorFilter(BillingSubjectTypeUser, nil, 42, 0)
	require.True(t, ok)
	require.Equal(t, int64(0), f)
	// member, no requested actor: forced to self
	f, ok = TeamUsageActorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionViewUsage: true}, 42, 0)
	require.True(t, ok)
	require.Equal(t, int64(42), f)
	// member, requests another actor: rejected
	_, ok = TeamUsageActorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionViewUsage: true}, 42, 99)
	require.False(t, ok)
	// member, requests self: allowed
	f, ok = TeamUsageActorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionViewUsage: true}, 42, 42)
	require.True(t, ok)
	require.Equal(t, int64(42), f)
	// owner/admin: passthrough (all when 0, specific when set)
	f, ok = TeamUsageActorFilter(BillingSubjectTypeTeam, map[string]bool{TeamPermissionViewUsageAll: true}, 42, 99)
	require.True(t, ok)
	require.Equal(t, int64(99), f)
}
