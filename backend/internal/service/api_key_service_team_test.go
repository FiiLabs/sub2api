package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestCreateAPIKeyUsesBillingSubjectAndActor(t *testing.T) {
	key := &APIKey{ID: 1, UserID: 42, Name: "team key"}
	applySubjectToAPIKey(key, SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           77,
	})

	require.Equal(t, int64(900), key.BillingSubjectID)
	require.NotNil(t, key.TeamID)
	require.Equal(t, int64(77), *key.TeamID)
	require.NotNil(t, key.CreatedByUserID)
	require.Equal(t, int64(42), *key.CreatedByUserID)
}

func TestUpdateAPIKeyForTeamSubjectDoesNotRequireCreatorOwnership(t *testing.T) {
	name := "renamed"
	repo := &teamAPIKeyRepoStub{key: &APIKey{
		ID:               5,
		UserID:           100,
		BillingSubjectID: 900,
		Key:              "sk-team",
		Name:             "old",
		Status:           StatusActive,
	}}
	svc := &APIKeyService{apiKeyRepo: repo}

	key, err := svc.UpdateForSubject(context.Background(), 5, SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           77,
		Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
	}, UpdateAPIKeyRequest{Name: &name})

	require.NoError(t, err)
	require.Equal(t, "renamed", key.Name)
	require.NotNil(t, key.UpdatedByUserID)
	require.Equal(t, int64(42), *key.UpdatedByUserID)
	require.Len(t, repo.updated, 1)
	require.Equal(t, []scopedGetCall{{id: 5, billingSubjectID: 900}}, repo.scopedGets)
	require.Equal(t, []scopedUpdateCall{{id: 5, billingSubjectID: 900}}, repo.scopedUpdated)
}

func TestGetAPIKeyForTeamSubjectUsesScopedLookup(t *testing.T) {
	repo := &teamAPIKeyRepoStub{key: &APIKey{
		ID:               5,
		UserID:           100,
		BillingSubjectID: 900,
		Key:              "sk-team",
		Name:             "team key",
		Status:           StatusActive,
	}}
	svc := &APIKeyService{apiKeyRepo: repo}

	key, err := svc.GetByIDForSubject(context.Background(), 5, SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           77,
		Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
	})

	require.NoError(t, err)
	require.Equal(t, int64(5), key.ID)
	require.Equal(t, []scopedGetCall{{id: 5, billingSubjectID: 900}}, repo.scopedGets)
}

func TestCreateAPIKeyForTeamSubjectPersistsSubjectOnInsert(t *testing.T) {
	repo := &teamAPIKeyRepoStub{}
	svc := NewAPIKeyService(
		repo,
		&teamAPIKeyUserRepoStub{user: &User{ID: 42, Status: StatusActive}},
		nil,
		nil,
		nil,
		nil,
		&config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-"}},
	)

	key, err := svc.CreateForSubject(context.Background(), SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           77,
		Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
	}, CreateAPIKeyRequest{Name: "team key"})

	require.NoError(t, err)
	require.Equal(t, key.ID, repo.created.ID)
	require.Equal(t, key.Key, repo.created.Key)
	require.Equal(t, int64(900), repo.created.BillingSubjectID)
	require.NotNil(t, repo.created.TeamID)
	require.Equal(t, int64(77), *repo.created.TeamID)
	require.NotNil(t, repo.created.CreatedByUserID)
	require.Equal(t, int64(42), *repo.created.CreatedByUserID)
	require.Empty(t, repo.updated)
}

func TestDeleteAPIKeyForTeamSubjectUsesScopedAuditDelete(t *testing.T) {
	repo := &teamAPIKeyRepoStub{key: &APIKey{
		ID:               5,
		UserID:           100,
		BillingSubjectID: 900,
		Key:              "sk-team",
		Name:             "team key",
		Status:           StatusActive,
	}}
	svc := &APIKeyService{apiKeyRepo: repo}

	err := svc.DeleteForSubject(context.Background(), 5, SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		TeamID:           77,
		Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
	})

	require.NoError(t, err)
	require.Equal(t, []scopedDeleteCall{{id: 5, billingSubjectID: 900}}, repo.scopedDeleted)
	require.Empty(t, repo.deleted)
}

type teamAPIKeyRepoStub struct {
	key              *APIKey
	created          *APIKey
	updated          []*APIKey
	deleted          []int64
	scopedGets       []scopedGetCall
	scopedUpdated    []scopedUpdateCall
	scopedDeleted    []scopedDeleteCall
	listUserCalls    []int64
	listSubjectCalls []int64
}

type scopedGetCall struct {
	id               int64
	billingSubjectID int64
}

type scopedUpdateCall struct {
	id               int64
	billingSubjectID int64
}

type scopedDeleteCall struct {
	id               int64
	billingSubjectID int64
}

func (r *teamAPIKeyRepoStub) Create(_ context.Context, key *APIKey) error {
	clone := *key
	clone.ID = 9
	clone.Key = key.Key
	r.created = &clone
	*key = clone
	return nil
}
func (r *teamAPIKeyRepoStub) GetByID(_ context.Context, _ int64) (*APIKey, error) {
	panic("unexpected GetByID")
}
func (r *teamAPIKeyRepoStub) GetByIDForBillingSubjectID(_ context.Context, id, billingSubjectID int64) (*APIKey, error) {
	r.scopedGets = append(r.scopedGets, scopedGetCall{id: id, billingSubjectID: billingSubjectID})
	clone := *r.key
	return &clone, nil
}
func (r *teamAPIKeyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID")
}
func (r *teamAPIKeyRepoStub) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKey")
}
func (r *teamAPIKeyRepoStub) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth")
}
func (r *teamAPIKeyRepoStub) Update(_ context.Context, key *APIKey) error {
	clone := *key
	r.updated = append(r.updated, &clone)
	r.key = &clone
	return nil
}
func (r *teamAPIKeyRepoStub) UpdateByBillingSubjectID(_ context.Context, key *APIKey, billingSubjectID int64) error {
	clone := *key
	r.scopedUpdated = append(r.scopedUpdated, scopedUpdateCall{id: key.ID, billingSubjectID: billingSubjectID})
	r.updated = append(r.updated, &clone)
	r.key = &clone
	return nil
}
func (r *teamAPIKeyRepoStub) Delete(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *teamAPIKeyRepoStub) DeleteWithAudit(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *teamAPIKeyRepoStub) DeleteWithAuditByBillingSubjectID(_ context.Context, id, billingSubjectID int64) error {
	r.scopedDeleted = append(r.scopedDeleted, scopedDeleteCall{id: id, billingSubjectID: billingSubjectID})
	return nil
}
func (r *teamAPIKeyRepoStub) ListByUserID(_ context.Context, userID int64, _ pagination.PaginationParams, _ APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	r.listUserCalls = append(r.listUserCalls, userID)
	return nil, &pagination.PaginationResult{}, nil
}
func (r *teamAPIKeyRepoStub) ListByBillingSubjectID(_ context.Context, billingSubjectID int64, _ pagination.PaginationParams, _ APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	r.listSubjectCalls = append(r.listSubjectCalls, billingSubjectID)
	return nil, &pagination.PaginationResult{}, nil
}
func (r *teamAPIKeyRepoStub) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership")
}
func (r *teamAPIKeyRepoStub) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID")
}
func (r *teamAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey")
}
func (r *teamAPIKeyRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID")
}
func (r *teamAPIKeyRepoStub) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys")
}
func (r *teamAPIKeyRepoStub) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID")
}
func (r *teamAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup")
}
func (r *teamAPIKeyRepoStub) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID")
}
func (r *teamAPIKeyRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID")
}
func (r *teamAPIKeyRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID")
}
func (r *teamAPIKeyRepoStub) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed")
}
func (r *teamAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed")
}
func (r *teamAPIKeyRepoStub) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage")
}
func (r *teamAPIKeyRepoStub) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows")
}
func (r *teamAPIKeyRepoStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData")
}

type teamAPIKeyUserRepoStub struct {
	UserRepository
	user *User
}

func (r *teamAPIKeyUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	clone := *r.user
	return &clone, nil
}

func TestListForSubjectPersonalUsesBillingSubject(t *testing.T) {
	repo := &teamAPIKeyRepoStub{}
	svc := &APIKeyService{apiKeyRepo: repo}

	_, _, err := svc.ListForSubject(context.Background(), SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 500, // 个人主体 id（刻意 != ActorUserID，证明用的是 subject 而非 user）
		SubjectType:      domain.BillingSubjectTypeUser,
	}, pagination.PaginationParams{Page: 1, PageSize: 20}, APIKeyListFilters{})

	require.NoError(t, err)
	require.Equal(t, []int64{500}, repo.listSubjectCalls)
	require.Empty(t, repo.listUserCalls)
}

func TestListForSubjectTeamUsesBillingSubject(t *testing.T) {
	repo := &teamAPIKeyRepoStub{}
	svc := &APIKeyService{apiKeyRepo: repo}

	_, _, err := svc.ListForSubject(context.Background(), SubjectResourceContext{
		ActorUserID:      42,
		BillingSubjectID: 900,
		SubjectType:      domain.BillingSubjectTypeTeam,
		Permissions:      map[string]bool{domain.TeamPermissionManageKeys: true},
	}, pagination.PaginationParams{Page: 1, PageSize: 20}, APIKeyListFilters{})

	require.NoError(t, err)
	require.Equal(t, []int64{900}, repo.listSubjectCalls)
	require.Empty(t, repo.listUserCalls)
}
