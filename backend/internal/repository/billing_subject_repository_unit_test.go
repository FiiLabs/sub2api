package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/billingsubject"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// newWorkspaceEntClient spins up an in-memory sqlite ent client. The generated
// schema includes the partial unique index on personal billing subjects
// (type='user' AND deleted_at IS NULL), so duplicate-insert attempts are
// rejected here just as they are on Postgres.
func newWorkspaceEntClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(10)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// createWorkspaceTestUser inserts a user directly (bypassing userRepo.Create, so
// no personal billing subject is auto-created) — modelling a user that predates
// the subject backfill or the bootstrap admin created via raw SQL.
func createWorkspaceTestUser(t *testing.T, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	u, err := client.User.Create().
		SetEmail(email).
		SetUsername(email).
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		SetBalance(7.5).
		SetConcurrency(3).
		Save(context.Background())
	require.NoError(t, err)
	return u
}

func countPersonalSubjects(t *testing.T, client *dbent.Client, userID int64) int {
	t.Helper()
	n, err := client.BillingSubject.Query().
		Where(
			billingsubject.TypeEQ(domain.BillingSubjectTypeUser),
			billingsubject.UserIDEQ(userID),
			billingsubject.DeletedAtIsNil(),
		).
		Count(context.Background())
	require.NoError(t, err)
	return n
}

// TestEnsurePersonalForUserIsIdempotent is the core "幂等创建" guarantee: calling
// EnsurePersonalForUser repeatedly returns the same subject and never creates a
// duplicate row.
func TestEnsurePersonalForUserIsIdempotent(t *testing.T) {
	client := newWorkspaceEntClient(t)
	repo := NewBillingSubjectRepository(client)
	ctx := context.Background()

	u := createWorkspaceTestUser(t, client, "idem@example.com")
	svcUser := &service.User{
		ID:                         u.ID,
		Status:                     u.Status,
		Balance:                    u.Balance,
		Concurrency:                u.Concurrency,
		BalanceNotifyThresholdType: "fixed",
	}

	first, err := repo.EnsurePersonalForUser(ctx, svcUser)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, domain.BillingSubjectTypeUser, first.Type)
	require.NotNil(t, first.UserID)
	require.Equal(t, u.ID, *first.UserID)
	require.Equal(t, u.Balance, first.Balance)
	require.Equal(t, u.Concurrency, first.Concurrency)

	// Repeated calls must be no-ops that return the same subject.
	for i := 0; i < 3; i++ {
		again, err := repo.EnsurePersonalForUser(ctx, svcUser)
		require.NoError(t, err)
		require.Equal(t, first.ID, again.ID, "EnsurePersonalForUser must be idempotent")
	}

	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID),
		"exactly one personal subject must exist after repeated EnsurePersonalForUser calls")
}

// TestEnsurePersonalForUserReturnsExistingWhenAlreadyPresent verifies the
// check-first path returns the pre-existing subject untouched.
func TestEnsurePersonalForUserReturnsExistingWhenAlreadyPresent(t *testing.T) {
	client := newWorkspaceEntClient(t)
	repo := NewBillingSubjectRepository(client)
	ctx := context.Background()

	u := createWorkspaceTestUser(t, client, "existing@example.com")

	// Pre-create the personal subject directly.
	pre, err := client.BillingSubject.Create().
		SetType(domain.BillingSubjectTypeUser).
		SetUserID(u.ID).
		SetStatus(u.Status).
		SetBalance(u.Balance).
		SetConcurrency(u.Concurrency).
		Save(ctx)
	require.NoError(t, err)

	got, err := repo.EnsurePersonalForUser(ctx, &service.User{ID: u.ID, Status: u.Status, BalanceNotifyThresholdType: "fixed"})
	require.NoError(t, err)
	require.Equal(t, pre.ID, got.ID)
	require.Equal(t, 1, countPersonalSubjects(t, client, u.ID))
}

// TestEnsurePersonalForUserRejectsInvalidUser guards the input validation.
func TestEnsurePersonalForUserRejectsInvalidUser(t *testing.T) {
	client := newWorkspaceEntClient(t)
	repo := NewBillingSubjectRepository(client)

	_, err := repo.EnsurePersonalForUser(context.Background(), nil)
	require.ErrorIs(t, err, service.ErrUserNotFound)

	_, err = repo.EnsurePersonalForUser(context.Background(), &service.User{ID: 0})
	require.ErrorIs(t, err, service.ErrUserNotFound)
}
