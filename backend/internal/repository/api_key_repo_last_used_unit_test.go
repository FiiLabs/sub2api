package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newAPIKeyRepoSQLite(t *testing.T) (*apiKeyRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:api_key_repo_last_used?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &apiKeyRepository{client: client}, client
}

func mustCreateAPIKeyRepoUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *service.User {
	t.Helper()
	u, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return userEntityToService(u)
}

func TestAPIKeyRepository_CreateWithLastUsedAt(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "create-last-used@test.com")

	lastUsed := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	key := &service.APIKey{
		UserID:     user.ID,
		Key:        "sk-create-last-used",
		Name:       "CreateWithLastUsed",
		Status:     service.StatusActive,
		LastUsedAt: &lastUsed,
	}

	require.NoError(t, repo.Create(ctx, key))
	require.NotNil(t, key.LastUsedAt)
	require.WithinDuration(t, lastUsed, *key.LastUsedAt, time.Second)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	require.WithinDuration(t, lastUsed, *got.LastUsedAt, time.Second)
}

func TestAPIKeyRepository_UpdateLastUsed(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "update-last-used@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-update-last-used",
		Name:   "UpdateLastUsed",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	before, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, before.LastUsedAt)

	target := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
	require.NoError(t, repo.UpdateLastUsed(ctx, key.ID, target))

	after, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, after.LastUsedAt)
	require.WithinDuration(t, target, *after.LastUsedAt, time.Second)
	require.WithinDuration(t, target, after.UpdatedAt, time.Second)
}

func TestAPIKeyRepository_UpdateLastUsedDeletedKey(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "deleted-last-used@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-update-last-used-deleted",
		Name:   "UpdateLastUsedDeleted",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	require.NoError(t, repo.Delete(ctx, key.ID))

	err := repo.UpdateLastUsed(ctx, key.ID, time.Now().UTC())
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
}

func TestAPIKeyRepository_UpdateLastUsedDBError(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "db-error-last-used@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-update-last-used-db-error",
		Name:   "UpdateLastUsedDBError",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	require.NoError(t, client.Close())
	err := repo.UpdateLastUsed(ctx, key.ID, time.Now().UTC())
	require.Error(t, err)
}

func TestAPIKeyRepository_CreateDuplicateKey(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "duplicate-key@test.com")

	first := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-duplicate",
		Name:   "first",
		Status: service.StatusActive,
	}
	second := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-duplicate",
		Name:   "second",
		Status: service.StatusActive,
	}

	require.NoError(t, repo.Create(ctx, first))
	err := repo.Create(ctx, second)
	require.ErrorIs(t, err, service.ErrAPIKeyExists)
}

func TestListKeysByTeamIDReturnsOnlyTeamKeys(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "list-keys-team@test.com")

	// 需要真实的 team 记录，TeamID 是外键
	teamA, err := client.Team.Create().
		SetName("team-a").
		SetSlug("team-a").
		SetOwnerUserID(user.ID).
		SetCreatedByUserID(user.ID).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	teamB, err := client.Team.Create().
		SetName("team-b").
		SetSlug("team-b").
		SetOwnerUserID(user.ID).
		SetCreatedByUserID(user.ID).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	idA := teamA.ID
	idB := teamB.ID

	mk := func(key string, teamID *int64) {
		k := &service.APIKey{
			UserID: user.ID,
			Key:    key,
			Name:   key,
			Status: service.StatusActive,
			TeamID: teamID,
		}
		require.NoError(t, repo.Create(ctx, k))
	}
	mk("k-team-a-1", &idA)
	mk("k-team-a-2", &idA)
	mk("k-team-b", &idB)
	mk("k-personal", nil)

	keys, err := repo.ListKeysByTeamID(ctx, teamA.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-team-a-1", "k-team-a-2"}, keys)
}

func TestDeleteByTeamIDSoftDeletesTeamKeysAndReturnsStrings(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "delete-team-keys@test.com")

	// TeamID 是外键，需要真实 team 记录
	team7, err := client.Team.Create().
		SetName("team-7").
		SetSlug("team-7").
		SetOwnerUserID(user.ID).
		SetCreatedByUserID(user.ID).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	team8, err := client.Team.Create().
		SetName("team-8").
		SetSlug("team-8").
		SetOwnerUserID(user.ID).
		SetCreatedByUserID(user.ID).
		SetStatus("active").
		Save(ctx)
	require.NoError(t, err)

	mk := func(key string, teamID *int64) {
		k := &service.APIKey{
			UserID: user.ID,
			Key:    key,
			Name:   key,
			Status: service.StatusActive,
			TeamID: teamID,
		}
		require.NoError(t, repo.Create(ctx, k))
	}
	t7, t8 := team7.ID, team8.ID
	mk("k-team7-a", &t7)
	mk("k-team7-b", &t7)
	mk("k-team8", &t8)
	mk("k-personal", nil)

	deleted, err := repo.DeleteByTeamID(ctx, team7.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-team7-a", "k-team7-b"}, deleted)

	// team7 的 key 已软删（active 查询不再返回）
	remaining, err := repo.ListKeysByTeamID(ctx, team7.ID)
	require.NoError(t, err)
	require.Empty(t, remaining)

	// team8 / personal 不受影响
	t8keys, err := repo.ListKeysByTeamID(ctx, team8.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"k-team8"}, t8keys)
}
