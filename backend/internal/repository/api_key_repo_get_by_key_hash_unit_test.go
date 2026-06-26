package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestAPIKeyRepository_GetByKeyHash(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "get-by-key-hash@test.com")

	rawKey := "sk-get-by-key-hash-test-key"
	hash := sha256Hex(rawKey)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     rawKey,
		KeyHash: &hash,
		Name:    "HashTestKey",
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	require.NotZero(t, key.ID)

	// Happy path: found by hash.
	got, err := repo.GetByKeyHash(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, key.ID, got.ID)
	require.Equal(t, rawKey, got.Key)
	require.NotNil(t, got.KeyHash)
	require.Equal(t, hash, *got.KeyHash)

	// Not found: unknown hash returns ErrAPIKeyNotFound.
	_, err = repo.GetByKeyHash(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
}

func TestAPIKeyRepository_GetByKeyHash_SoftDeleted(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "get-by-key-hash-deleted@test.com")

	rawKey := "sk-get-by-key-hash-deleted-key"
	hash := sha256Hex(rawKey)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     rawKey,
		KeyHash: &hash,
		Name:    "HashTestKeyDeleted",
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	require.NotZero(t, key.ID)

	// Soft-delete the key.
	require.NoError(t, repo.Delete(ctx, key.ID))

	// GetByKeyHash must not return soft-deleted keys.
	_, err := repo.GetByKeyHash(ctx, hash)
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
}
