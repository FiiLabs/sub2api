//go:build unit

package service

import (
	"context"
	"testing"

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
