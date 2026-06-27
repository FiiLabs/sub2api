//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDeleteTeamKeysDeletesAndInvalidates(t *testing.T) {
	cache := &authCacheStub{}
	var gotTeamID int64
	repo := &authRepoStub{
		deleteByTeamID: func(_ context.Context, teamID int64) ([]string, error) {
			gotTeamID = teamID
			return []string{"k1", "k2"}, nil
		},
	}
	cfg := &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{
			L2TTLSeconds: 60,
		},
	}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, cfg)

	require.NoError(t, svc.DeleteTeamKeys(context.Background(), 7))
	require.Equal(t, int64(7), gotTeamID)
	require.Len(t, cache.deleteAuthKeys, 2)
}
