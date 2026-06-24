//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// subRepoStub 内嵌接口，仅覆写 4 个 list 方法记录调用。
type subRepoStub struct {
	UserSubscriptionRepository
	byUser    []int64
	bySubject []int64
}

func (s *subRepoStub) ListByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	s.byUser = append(s.byUser, userID)
	return nil, nil
}
func (s *subRepoStub) ListActiveByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	s.byUser = append(s.byUser, userID)
	return nil, nil
}
func (s *subRepoStub) ListByBillingSubjectID(_ context.Context, subjectID int64) ([]UserSubscription, error) {
	s.bySubject = append(s.bySubject, subjectID)
	return nil, nil
}
func (s *subRepoStub) ListActiveByBillingSubjectID(_ context.Context, subjectID int64) ([]UserSubscription, error) {
	s.bySubject = append(s.bySubject, subjectID)
	return nil, nil
}

func newSubjectSubService(stub UserSubscriptionRepository, scoped bool) *SubscriptionService {
	cfg := &config.Config{}
	cfg.Billing.QuotaSubjectScoped = scoped
	return &SubscriptionService{userSubRepo: stub, cfg: cfg}
}

func TestListSubscriptionsForSubject_ScopedRoutesBySubject(t *testing.T) {
	stub := &subRepoStub{}
	s := newSubjectSubService(stub, true)
	_, err := s.ListSubscriptionsForSubject(context.Background(), 9, 900)
	require.NoError(t, err)
	require.Equal(t, []int64{900}, stub.bySubject)
	require.Empty(t, stub.byUser)
}

func TestListActiveSubscriptionsForSubject_FlagOffRoutesByUser(t *testing.T) {
	stub := &subRepoStub{}
	s := newSubjectSubService(stub, false)
	_, err := s.ListActiveSubscriptionsForSubject(context.Background(), 9, 900)
	require.NoError(t, err)
	require.Equal(t, []int64{9}, stub.byUser)
	require.Empty(t, stub.bySubject)
}
