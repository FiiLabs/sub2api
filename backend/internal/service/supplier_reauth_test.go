//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 就地重新授权（supplier_reauth.go）
//
// 这一组用例守的是三件事，重要性递减：
//
//  1. **身份闸**。它是这条路径唯一的安全边界——没有它，一个已经跑完观察期的
//     account id 就是任意订阅的壳子。滥用用例（换了另一份订阅、命中别人的行）
//     必须同时断言「一个字都没写进去」，而不只是断言返回了错误。
//  2. **已 promote 的号要恢复 schedulable**。SetError 会把它置 false，ClearError
//     不还回来，观察期任务又只扫 pending_review——这条路径是唯一能把号放回池子的
//     地方。漏了它，线上表现是「重新授权成功了，但号再也不接单」。
//  3. **写序**：凭证严格早于清错误态。反过来会开一个「active + 死 token」的窗口。
// ============================================================================

// reauthGuardStub 冒充失效熔断器，用来证明重新授权**不**受它管辖。
type reauthGuardStub struct {
	calls int
	err   error
}

func (g *reauthGuardStub) GuardOnboarding(_ context.Context, _ int64, _ *SupplyOnboardingSettings) error {
	g.calls++
	return g.err
}

const (
	reauthUserID    = int64(7)
	reauthAccountID = int64(100)
)

// newReauthFixture 摆一个「号坏了、主人来修」的场景。
//
// 默认身份与 supplierOAuthStub 默认吐出的 token 一致（uuid-1 / supplier@example.com），
// 于是身份闸默认放行——想测拒绝的用例显式改其中一边。
func newReauthFixture(t *testing.T, state string, status string) (
	*SupplierOnboardingService, *supplierOnboardingRepoStub, *supplierAccountStoreStub, *supplierOAuthStub, *[]string,
) {
	t.Helper()
	seq := &[]string{}
	repo := &supplierOnboardingRepoStub{
		seq:            seq,
		ownerByAccount: map[int64]int64{reauthAccountID: reauthUserID},
		claimSession: &SupplierOAuthSession{
			SessionID:    "sess-1",
			UserID:       reauthUserID,
			Platform:     PlatformAnthropic,
			State:        "st-1",
			CodeVerifier: "verifier-1",
			Scope:        "user:inference",
			AccountID:    reauthAccountID,
		},
	}
	store := newSupplierAccountStoreStub()
	store.seq = seq
	store.accounts[reauthAccountID] = &Account{
		ID:       reauthAccountID,
		Name:     "supplier@example.com",
		Platform: PlatformAnthropic,
		Type:     AccountTypeSetupToken,
		Status:   status,
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "old-rt",
			"account_uuid":  "uuid-1",
			"email_address": "supplier@example.com",
		},
		Extra: map[string]any{
			SupplyStateExtraKey:          state,
			SupplyProbationSinceExtraKey: time.Unix(1700000000, 0).Format(time.RFC3339),
			SupplyProbePassesExtraKey:    1,
			SupplyProbeErrorExtraKey:     "API returned 401: token expired",
			SupplyDailyCostLimitExtraKey: 12.5,
		},
	}
	oauth := &supplierOAuthStub{}
	svc := newOnboardingService(t, repo, store, oauth, enabledSupplyPoolJSON())
	return svc, repo, store, oauth, seq
}

func reauthInput() *CompleteReauthInput {
	return &CompleteReauthInput{
		UserID:    reauthUserID,
		AccountID: reauthAccountID,
		SessionID: "sess-1",
		Code:      "auth-code",
		ClientIP:  testClientIP,
	}
}

// ---------------------------------------------------------------------------
// 正常路径
// ---------------------------------------------------------------------------

func TestCompleteReauthPendingReviewReplacesCredentialsAndKeepsAccountOffThePool(t *testing.T) {
	svc, repo, store, _, _ := newReauthFixture(t, SupplyStatePendingReview, StatusActive)

	view, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, repo.reauthCalls, 1)
	call := repo.reauthCalls[0]
	assert.Equal(t, reauthAccountID, call.accountID)
	assert.Equal(t, reauthUserID, call.userID)

	// 凭证是**整份替换**：旧 token 一个都不能留下。断言整份 map 而不是几个字段——
	// 「残留」恰恰是逐字段断言看不见的那种失败。
	assert.Equal(t, "at", call.credentials["access_token"])
	assert.Equal(t, "rt", call.credentials["refresh_token"])
	assert.NotContains(t, call.credentials, "old-at")
	for _, v := range call.credentials {
		assert.NotEqual(t, "old-at", v)
		assert.NotEqual(t, "old-rt", v)
	}

	// 三个属于上一份凭证的观察期字段都被清掉。
	assert.Equal(t, 0, call.extra[SupplyProbePassesExtraKey])
	assert.Equal(t, "", call.extra[SupplyProbeErrorExtraKey])
	assert.Equal(t, "", call.extra[SupplyProbeAtExtraKey])
	// 观察期从这一刻重新计时。
	assert.NotEmpty(t, call.extra[SupplyProbationSinceExtraKey])
	// 接入状态不在这次写里——重新授权不改状态机。
	assert.NotContains(t, call.extra, SupplyStateExtraKey)
	// 每日上限不在这次写里（真实实现靠 `||` 合并保住它）。
	assert.NotContains(t, call.extra, SupplyDailyCostLimitExtraKey)

	assert.Equal(t, []int64{reauthAccountID}, store.clearErrorIDs)
	// pending_review 的号**不**恢复调度：把它放回池子是观察期任务的职责。
	assert.Zero(t, store.schedulableCalls)
}

// 已 promote 的号必须被显式放回池子。
//
// 这是 SetError/ClearError 那个线上坑的回归测试：SetError 会把 schedulable 置 false，
// ClearError 不还回来，而观察期任务只扫 pending_review。少了这一步，一个修好了的
// active 号会永远不接单，且代码里没有第二条路能救它。
func TestCompleteReauthActiveAccountIsReturnedToThePool(t *testing.T) {
	svc, _, store, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	store.accounts[reauthAccountID].Schedulable = false

	view, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, []int64{reauthAccountID}, store.clearErrorIDs)
	assert.Equal(t, 1, store.schedulableCalls)
	assert.True(t, store.schedulableSets[reauthAccountID])
	assert.Equal(t, SupplyStateActive, view.SupplyState)
	assert.True(t, view.Schedulable)
}

// active 的号不重置观察期起点：它不在观察期里，写那个键只会让界面上冒出
// 一段莫名其妙的进度。
func TestCompleteReauthActiveAccountKeepsProbationSince(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)

	require.Len(t, repo.reauthCalls, 1)
	assert.NotContains(t, repo.reauthCalls[0].extra, SupplyProbationSinceExtraKey)
}

// 写凭证严格早于清错误态。反过来会开一个「status=active + 一份死 token」的窗口，
// 而那个窗口里号是会被派真实流量的。
func TestCompleteReauthWritesCredentialsBeforeClearingError(t *testing.T) {
	svc, _, _, _, seq := newReauthFixture(t, SupplyStateActive, StatusError)

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)

	credIdx := indexOfCall(*seq, "ApplyReauthCredentials")
	clearIdx := indexOfCall(*seq, "ClearError")
	schedIdx := indexOfCall(*seq, "SetSchedulable")
	require.GreaterOrEqual(t, credIdx, 0)
	require.GreaterOrEqual(t, clearIdx, 0)
	require.GreaterOrEqual(t, schedIdx, 0)
	assert.Less(t, credIdx, clearIdx, "凭证必须先落地，否则会出现 active + 死 token")
	assert.Less(t, clearIdx, schedIdx, "恢复调度必须是最后一步")
}

func indexOfCall(seq []string, name string) int {
	for i, call := range seq {
		if call == name {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// 身份闸——这条路径唯一的安全边界
// ---------------------------------------------------------------------------

// 授权的是另一份订阅。必须拒绝，而且**一个字都不能写进去**。
func TestCompleteReauthRejectsDifferentSubscription(t *testing.T) {
	svc, repo, store, oauth, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	oauth.token = &TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		AccountUUID:  "uuid-SOMEONE-ELSE",
		EmailAddress: "someone-else@example.com",
	}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierReauthIdentityMismatch)

	assert.Empty(t, repo.reauthCalls, "身份不符时不得写凭证")
	assert.Empty(t, store.clearErrorIDs, "身份不符时不得清错误态")
	assert.Zero(t, store.schedulableCalls, "身份不符时不得恢复调度")
}

// 身份本身对得上，但这个身份在库里指向的是**另一行**（历史脏数据、并发改动）。
// 第二道闸挡住它。
func TestCompleteReauthRejectsIdentityOwnedByAnotherAccount(t *testing.T) {
	svc, repo, store, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	repo.lookupIdentity = map[string]int64{
		string(SupplierIdentityAccountUUID) + "=uuid-1": 999,
	}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierAccountAlreadyBound)

	assert.Empty(t, repo.reauthCalls)
	assert.Empty(t, store.clearErrorIDs)
	assert.Zero(t, store.schedulableCalls)
}

// 命中的行就是自己——正常路径，不能被第二道闸误伤。
func TestCompleteReauthAllowsIdentityPointingAtItself(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	repo.lookupIdentity = map[string]int64{
		string(SupplierIdentityAccountUUID) + "=uuid-1": reauthAccountID,
	}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	assert.Len(t, repo.reauthCalls, 1)
}

// 只比最强的那个键：uuid 一致就是同一份订阅，上游改了邮箱是合法的事实。
// 要求所有共有键都相等，会把一次正常改邮箱判成盗用。
func TestCompleteReauthToleratesUpstreamEmailChangeWhenUUIDMatches(t *testing.T) {
	svc, repo, _, oauth, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	oauth.token = &TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		AccountUUID:  "uuid-1",
		EmailAddress: "renamed@example.com",
	}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	assert.Len(t, repo.reauthCalls, 1)
}

// 没有 uuid 时退到邮箱，且大小写不敏感——对齐查重语句里的 LOWER(...)。
// 按字节比会让上游把 Foo@x.com 显示成 foo@x.com 这种无害变化被判成换了订阅。
func TestCompleteReauthEmailComparisonIsCaseInsensitive(t *testing.T) {
	svc, repo, store, oauth, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	store.accounts[reauthAccountID].Credentials = map[string]any{
		"access_token":  "old-at",
		"email_address": "Supplier@Example.com",
	}
	oauth.token = &TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		EmailAddress: "supplier@example.com",
	}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	assert.Len(t, repo.reauthCalls, 1)
}

// 一个两边都有值的身份键都没有 → 拒绝，不是放行。
// 判不了「是不是同一份订阅」时放行，正好是身份闸要挡的那件事。
func TestCompleteReauthRejectsWhenNoComparableIdentity(t *testing.T) {
	svc, repo, store, oauth, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	store.accounts[reauthAccountID].Credentials = map[string]any{"access_token": "old-at"}
	oauth.token = &TokenInfo{AccessToken: "at", TokenType: "Bearer"}

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierReauthUnsupported)
	assert.Empty(t, repo.reauthCalls)
}

// 身份查询失败往上抛，不放行。这道闸的开关不能建立在「数据库这一刻是否健康」之上。
func TestCompleteReauthPropagatesIdentityLookupFailure(t *testing.T) {
	svc, repo, store, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	repo.lookupErr = errors.New("db down")

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.Error(t, err)
	assert.Empty(t, repo.reauthCalls)
	assert.Empty(t, store.clearErrorIDs)
}

// ---------------------------------------------------------------------------
// 归属与会话作用域
// ---------------------------------------------------------------------------

// 越权：报 NotFound（与「不存在」同一个回答），且**在领会话之前**就被拦下——
// 领取是一次性消费，不能让一次越权尝试烧掉一条会话。
func TestCompleteReauthRejectsForeignAccountBeforeClaimingSession(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	repo.ownerByAccount[reauthAccountID] = 999

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
	assert.NotContains(t, repo.calls, "ClaimSession")
}

// 会话作用域双向：重新授权领的是绑在这个号上的会话，接入领的是 account_id 为空的。
// 真实实现把这个判断做在 SQL 的 IS NOT DISTINCT FROM 里，这里能测的是
// 「调用方有没有把种类传对」——传错了，SQL 那道闸就形同虚设。
func TestSessionClaimScopeDistinguishesReauthFromOnboarding(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	assert.Equal(t, []int64{reauthAccountID}, repo.claimAccountIDs)

	// 同一个服务上走一次接入，必须传 0。
	repo.claimAccountIDs = nil
	repo.claimSession = &SupplierOAuthSession{
		SessionID: "sess-2", UserID: reauthUserID, Platform: PlatformAnthropic,
		State: "st-2", CodeVerifier: "v2", Scope: "user:inference",
	}
	_, err = svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: reauthUserID, SessionID: "sess-2", Code: "c", ClientIP: testClientIP,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{0}, repo.claimAccountIDs)
}

// ---------------------------------------------------------------------------
// 资格
// ---------------------------------------------------------------------------

// 中转号走不了 OAuth 重新授权，而且必须在**发起**时就说清楚——
// 放它进去只会在 token 交换之后才失败，那时供给者已经白跑了一趟上游授权。
func TestReauthRejectsRelayAccountAtStart(t *testing.T) {
	svc, _, store, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	store.accounts[reauthAccountID].Type = AccountTypeAPIKey
	store.accounts[reauthAccountID].Credentials = map[string]any{
		"base_url": "https://relay.example.com", "api_key": "k",
	}

	_, err := svc.StartReauth(context.Background(), reauthUserID, reauthAccountID)
	assert.ErrorIs(t, err, ErrSupplierReauthUnsupported)

	_, err = svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierReauthUnsupported)
}

// 已下线的号：对应的动作是「重新挂回」，不是「重新授权」。
func TestReauthRejectsRetiredAccount(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateRetired, StatusActive)

	_, err := svc.StartReauth(context.Background(), reauthUserID, reauthAccountID)
	assert.ErrorIs(t, err, ErrSupplierAccountRetired)

	_, err = svc.CompleteReauth(context.Background(), reauthInput())
	assert.ErrorIs(t, err, ErrSupplierAccountRetired)
	assert.Empty(t, repo.reauthCalls)
}

// 排空中的号放行：凭证坏了就是坏了，与「他正在下线它」是两件独立的事。
// 重新授权也不取消排空——那是 ResumeAccount 的职责。
func TestReauthAllowsDrainingAccountWithoutCancellingTheDrain(t *testing.T) {
	svc, repo, store, _, _ := newReauthFixture(t, SupplyStateDraining, StatusError)
	store.accounts[reauthAccountID].Extra[SupplyDrainUntilExtraKey] =
		time.Now().Add(10 * time.Minute).Format(time.RFC3339)

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)

	require.Len(t, repo.reauthCalls, 1)
	assert.NotContains(t, repo.reauthCalls[0].extra, SupplyStateExtraKey)
	assert.NotContains(t, repo.reauthCalls[0].extra, SupplyDrainUntilExtraKey)
	// draining 不是 active，所以不恢复调度——号仍在停止接单。
	assert.Zero(t, store.schedulableCalls)
}

// ---------------------------------------------------------------------------
// 不适用的闸
// ---------------------------------------------------------------------------

// 失效熔断器不管重新授权。它数的是这个人最近坏掉的号，而重新授权正是**关闭**
// 那些事件的动作；套上去会形成一个修不好也退不出的闭环。
func TestReauthIsNotSubjectToTheIncidentBreaker(t *testing.T) {
	svc, _, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	guard := &reauthGuardStub{err: ErrSupplierIncidentRateExceeded}
	svc.incidents = guard

	_, err := svc.StartReauth(context.Background(), reauthUserID, reauthAccountID)
	require.NoError(t, err)

	_, err = svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)

	assert.Zero(t, guard.calls, "重新授权不该走接入熔断——它一个号也没多塞")
}

// 已经挂到人均上限的人仍然修得了自己的坏号。这条路径不建号，
// 数「你还能挂几个」是在回答一个没人问的问题。
func TestReauthWorksWhenOwnerIsAtTheAccountCap(t *testing.T) {
	svc, repo, _, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	repo.ownedCount = 9999

	_, err := svc.StartReauth(context.Background(), reauthUserID, reauthAccountID)
	require.NoError(t, err)

	_, err = svc.CompleteReauth(context.Background(), reauthInput())
	require.NoError(t, err)
	assert.Len(t, repo.reauthCalls, 1)
}

// 发起重新授权时把会话绑在这个号上，且 scope 与接入路径一字不差
// （一次修凭证不是重新协商权限的时机）。
func TestStartReauthBindsSessionToTheAccountAndKeepsScope(t *testing.T) {
	svc, repo, _, oauth, _ := newReauthFixture(t, SupplyStateActive, StatusError)

	auth, err := svc.StartReauth(context.Background(), reauthUserID, reauthAccountID)
	require.NoError(t, err)
	require.NotNil(t, auth)

	require.NotNil(t, repo.createdSession)
	assert.Equal(t, reauthAccountID, repo.createdSession.AccountID)
	assert.Equal(t, reauthUserID, repo.createdSession.UserID)
	assert.Equal(t, "user:inference", oauth.requestScope)
}

// ---------------------------------------------------------------------------
// 部分失败
// ---------------------------------------------------------------------------

// 清错误态失败：凭证已经换好了（安全的那一半——号仍是错误态，不接单也不被探测），
// 但照常报错，供给者重试一次即可。不得因为"凭证写成功了"就吞掉这个错误。
func TestCompleteReauthReportsClearErrorFailureAfterCredentialsLanded(t *testing.T) {
	svc, repo, store, _, _ := newReauthFixture(t, SupplyStateActive, StatusError)
	store.clearErrorErr = errors.New("db down")

	_, err := svc.CompleteReauth(context.Background(), reauthInput())
	require.Error(t, err)

	assert.Len(t, repo.reauthCalls, 1, "凭证应该已经落地")
	assert.Zero(t, store.schedulableCalls, "错误态没清掉就不能把号放回池子")
}

// ---------------------------------------------------------------------------
// needs_reauth 判据
// ---------------------------------------------------------------------------

func TestSupplyNeedsReauth(t *testing.T) {
	oauthAccount := func(status, state, probeError string) *Account {
		return &Account{
			Type:   AccountTypeSetupToken,
			Status: status,
			Extra: map[string]any{
				SupplyStateExtraKey:      state,
				SupplyProbeErrorExtraKey: probeError,
			},
		}
	}

	cases := []struct {
		name    string
		account *Account
		want    bool
	}{
		{"错误态", oauthAccount(StatusError, SupplyStateActive, ""), true},
		{
			// 本次改动上线之前就卡在观察期 401 循环里的存量号（#2 就是这种）：
			// status 还是 active，但探测早就看见 401 了。有这一支，它们一上线
			// 就显示按钮，不必等下一次探测。
			"状态还没翻过来但探测已经 401",
			oauthAccount(StatusActive, SupplyStatePendingReview, "API returned 401: token expired"),
			true,
		},
		{"健康", oauthAccount(StatusActive, SupplyStateActive, ""), false},
		{"非认证类探测失败", oauthAccount(StatusActive, SupplyStatePendingReview, "context deadline exceeded"), false},
		{"限流不是凭证失效", oauthAccount(StatusActive, SupplyStatePendingReview, "API returned 429: rate limited"), false},
		{"已下线是他自己按的", oauthAccount(StatusError, SupplyStateRetired, ""), false},
		{"管理员停用——让他去授权是原地转圈", oauthAccount(StatusDisabled, SupplyStateActive, ""), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, supplyNeedsReauth(tc.account))
		})
	}

	t.Run("中转号不挂这个徽章", func(t *testing.T) {
		relay := oauthAccount(StatusError, SupplyStateActive, "")
		relay.Type = AccountTypeAPIKey
		assert.False(t, supplyNeedsReauth(relay), "中转号走不了 OAuth 重新授权，挂徽章是把人骗进必被拒的流程")
	})
}
