//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAgreementService 组一个协议门禁用的服务：**不**默认打开「已同意」。
//
// 与 newOnboardingService 刻意分开。那个构造器为了让几十个编排用例不必先演一遍签字，
// 默认把协议配好、把人当成已同意；本组用例要测的正是这两个前提被拆掉之后会发生什么，
// 借用那个构造器再手工关掉，等于把测试的前提藏在两层默认值里。
func newAgreementService(t *testing.T, repo *supplierOnboardingRepoStub, agreementJSON string) *SupplierOnboardingService {
	t.Helper()
	settingRepo := &supplyPoolSettingRepoStub{
		value:          enabledSupplyPoolJSON(),
		agreementValue: agreementJSON,
	}
	return &SupplierOnboardingService{
		repo:        repo,
		accountRepo: newSupplierAccountStoreStub(),
		oauth:       &supplierOAuthStub{},
		settings:    newSupplyPoolSettingService(t, settingRepo),
	}
}

// ============================================================================
// GetAgreement
// ============================================================================

// 没发布协议时不去查同意记录：拿一个空版本当查询键，查到什么都是错的。
func TestGetAgreementUnpublishedDoesNotLookUpAcceptance(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, "")

	view, err := svc.GetAgreement(context.Background(), 7)
	require.NoError(t, err)

	assert.False(t, view.Published)
	assert.Empty(t, view.Version)
	assert.False(t, view.Accepted)
	assert.NotContains(t, repo.calls, "FindAgreementAcceptance")
}

func TestGetAgreementReturnsPublishedTextAndAcceptance(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	repo.acceptAgreement(7, testAgreementVersion)
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	view, err := svc.GetAgreement(context.Background(), 7)
	require.NoError(t, err)

	assert.True(t, view.Published)
	assert.Equal(t, testAgreementVersion, view.Version)
	assert.Equal(t, "https://example.com/supplier-terms", view.URL)
	assert.Equal(t, "条款正文", view.Body)
	assert.True(t, view.Accepted)
	require.NotNil(t, view.AcceptedAt)
	assert.Equal(t, testAgreementVersion, view.AcceptedVersion)
	// 已经同意的人不必付那次"他上一版同意的是什么"的查询。
	assert.NotContains(t, repo.calls, "LatestAgreementAcceptance")
}

// 「同意的是旧版」与「从没同意过」在界面上是两句不同的话，服务端必须能分出来。
func TestGetAgreementDistinguishesStaleAcceptanceFromNever(t *testing.T) {
	stale := &SupplierAgreementAcceptance{UserID: 7, Version: "v0"}
	repo := &supplierOnboardingRepoStub{latestAgreement: stale}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	view, err := svc.GetAgreement(context.Background(), 7)
	require.NoError(t, err)
	assert.False(t, view.Accepted)
	assert.Equal(t, "v0", view.AcceptedVersion, "同意过旧版")

	never := newAgreementService(t, &supplierOnboardingRepoStub{}, publishedAgreementJSON())
	viewNever, err := never.GetAgreement(context.Background(), 7)
	require.NoError(t, err)
	assert.False(t, viewNever.Accepted)
	assert.Empty(t, viewNever.AcceptedVersion, "从没同意过")
}

func TestGetAgreementPropagatesLookupError(t *testing.T) {
	repo := &supplierOnboardingRepoStub{findAgreementErr: errors.New("db down")}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.GetAgreement(context.Background(), 7)
	assert.Error(t, err)
}

// ============================================================================
// AcceptAgreement
// ============================================================================

func TestAcceptAgreementRecordsEvidence(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	view, err := svc.AcceptAgreement(context.Background(), 7, testAgreementVersion, "203.0.113.9", "Mozilla/5.0")
	require.NoError(t, err)
	assert.True(t, view.Accepted)

	require.Len(t, repo.recordedAcceptances, 1)
	got := repo.recordedAcceptances[0]
	assert.Equal(t, int64(7), got.UserID)
	assert.Equal(t, testAgreementVersion, got.Version)
	assert.Equal(t, "203.0.113.9", got.IP)
	assert.Equal(t, "Mozilla/5.0", got.UserAgent)
	assert.False(t, got.AcceptedAt.IsZero())
}

// 页面开了很久、协议在这期间改过版：照旧版记一条同意记录，会让证据指向一份
// 他其实没读过的文本。
func TestAcceptAgreementRejectsStaleVersion(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.AcceptAgreement(context.Background(), 7, "v0", "", "")
	assert.ErrorIs(t, err, ErrSupplierAgreementVersionMismatch)
	assert.Empty(t, repo.recordedAcceptances, "版本不符时不该留下任何记录")
}

func TestAcceptAgreementRefusesWhenUnpublished(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, "")

	_, err := svc.AcceptAgreement(context.Background(), 7, "v1", "", "")
	assert.ErrorIs(t, err, ErrSupplierAgreementNotConfigured)
	assert.Empty(t, repo.recordedAcceptances)
}

// 重复点同意保留最早那一行——同意时刻是证据，不该被后来的一次点击往后推。
func TestAcceptAgreementTwiceKeepsEarliestRow(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	first, err := svc.AcceptAgreement(context.Background(), 7, testAgreementVersion, "1.1.1.1", "ua-1")
	require.NoError(t, err)
	require.NotNil(t, first.AcceptedAt)

	second, err := svc.AcceptAgreement(context.Background(), 7, testAgreementVersion, "2.2.2.2", "ua-2")
	require.NoError(t, err)
	require.NotNil(t, second.AcceptedAt)

	assert.Equal(t, *first.AcceptedAt, *second.AcceptedAt, "第二次点同意不该改写同意时刻")
}

// UA 超长就截断而不是让同意失败：它是旁证，不是同意本身。
func TestAcceptAgreementTruncatesOversizedUserAgent(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.AcceptAgreement(context.Background(), 7, testAgreementVersion,
		strings.Repeat("9", supplierAgreementIPMaxLen+40),
		strings.Repeat("x", supplierAgreementUserAgentMaxLen+100))
	require.NoError(t, err)

	require.Len(t, repo.recordedAcceptances, 1)
	assert.LessOrEqual(t, len(repo.recordedAcceptances[0].UserAgent), supplierAgreementUserAgentMaxLen)
	assert.LessOrEqual(t, len(repo.recordedAcceptances[0].IP), supplierAgreementIPMaxLen)
}

// 多字节字符不能被切成半个：截断按字符收边，而列宽按字节。
func TestTruncateRunesKeepsMultibyteCharactersWhole(t *testing.T) {
	got := truncateRunes(strings.Repeat("中", 100), 64)
	assert.LessOrEqual(t, len(got), 64, "按字节不越列宽")
	assert.Equal(t, strings.Repeat("中", 21), got, "按字符收边，不切出半个字")
}

// ============================================================================
// 门禁
// ============================================================================

func TestStartOAuthRequiresAgreement(t *testing.T) {
	cases := []struct {
		name          string
		agreementJSON string
		accepted      bool
		wantErr       error
	}{
		{"平台没发布协议", "", false, ErrSupplierAgreementNotConfigured},
		{"没同意当前版本", publishedAgreementJSON(), false, ErrSupplierAgreementRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierOnboardingRepoStub{}
			if tc.accepted {
				repo.acceptAgreement(7, testAgreementVersion)
			}
			svc := newAgreementService(t, repo, tc.agreementJSON)

			_, err := svc.StartOAuth(context.Background(), 7)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, repo.createdSession, "被协议挡住时不该写会话")
		})
	}
}

func TestStartOAuthPassesWhenAgreementAccepted(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	repo.acceptAgreement(7, testAgreementVersion)
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.StartOAuth(context.Background(), 7)
	require.NoError(t, err)
	assert.NotNil(t, repo.createdSession)
}

// 门禁必须排在领会话之前。领取是一次性消费：在它之后被拒的人会丢掉手上那个授权码，
// 而他被拒的原因在第一行就判得出来。
func TestCompleteOAuthChecksAgreementBeforeClaimingSession(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "code-1",
	})
	assert.ErrorIs(t, err, ErrSupplierAgreementRequired)
	assert.NotContains(t, repo.calls, "ClaimSession", "被协议挡住时那个一次性会话必须还在")
}

func TestCompleteOAuthRefusesWhenAgreementUnpublished(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	svc := newAgreementService(t, repo, "")

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "code-1",
	})
	assert.ErrorIs(t, err, ErrSupplierAgreementNotConfigured)
	assert.NotContains(t, repo.calls, "ClaimSession")
}

// 查同意记录失败时 fail-closed：一次数据库抖动不该变成"没有协议也能挂号"。
func TestCompleteOAuthFailsClosedWhenAgreementLookupErrors(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession:     claimedSession(),
		findAgreementErr: errors.New("db down"),
	}
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "code-1",
	})
	require.Error(t, err)
	assert.NotContains(t, repo.calls, "ClaimSession")
}

func TestCompleteOAuthPassesWhenAgreementAccepted(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	repo.acceptAgreement(7, testAgreementVersion)
	svc := newAgreementService(t, repo, publishedAgreementJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "code-1",
	})
	require.NoError(t, err)
	assert.Contains(t, repo.calls, "ClaimSession")
}
