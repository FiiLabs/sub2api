//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func newUserServiceForDisplay(repo BillingSubjectRepository, scoped bool) *UserService {
	cfg := &config.Config{}
	cfg.Billing.QuotaSubjectScoped = scoped
	return &UserService{billingSubjectRepo: repo, cfg: cfg}
}

func TestDisplayBalance_FlagOff_ReturnsFallback(t *testing.T) {
	s := newUserServiceForDisplay(&fakeSubjectRepo{}, false)
	if got := s.DisplayBalance(context.Background(), 1, 7, 12.5); got != 12.5 {
		t.Errorf("got %v, want fallback 12.5", got)
	}
}

func TestDisplayBalance_ScopedSubject(t *testing.T) {
	repo := &fakeSubjectRepo{byID: map[int64]*BillingSubject{7: {ID: 7, Balance: 99}}}
	s := newUserServiceForDisplay(repo, true)
	if got := s.DisplayBalance(context.Background(), 1, 7, 0); got != 99 {
		t.Errorf("got %v, want subject balance 99", got)
	}
}

func TestDisplayBalance_ScopedPersonalWhenNoSubject(t *testing.T) {
	uid := int64(1)
	repo := &fakeSubjectRepo{byID: map[int64]*BillingSubject{5: {ID: 5, UserID: &uid, Balance: 77}}}
	s := newUserServiceForDisplay(repo, true)
	if got := s.DisplayBalance(context.Background(), uid, 0, 0); got != 77 {
		t.Errorf("got %v, want personal subject balance 77", got)
	}
}

func TestDisplayBalance_FailSafeToFallback(t *testing.T) {
	s := newUserServiceForDisplay(&fakeSubjectRepo{}, true) // 空 repo → GetByID 返回 ErrUserNotFound
	if got := s.DisplayBalance(context.Background(), 1, 7, 33.3); got != 33.3 {
		t.Errorf("got %v, want fail-safe fallback 33.3", got)
	}
}
