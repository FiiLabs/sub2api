//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 测试替身
// ============================================================================

// supplierOnboardingRepoStub 记下每一次调用，测试靠它断言**顺序**——
// 「建号 → 写归属 → 绑分组」这个顺序本身就是一条安全性质，不是实现细节。
type supplierOnboardingRepoStub struct {
	calls []string
	// seq 是两个替身共用的调用流水，用来断言跨仓储的先后顺序。
	seq *[]string

	createdSession *SupplierOAuthSession
	createErr      error

	claimSession *SupplierOAuthSession
	claimErr     error

	pendingCount int
	pendingErr   error

	ownerByAccount map[int64]int64
	ownerErr       error
	setOwnerErr    error
	setOwnerCalls  [][2]int64

	ownedIDs   []int64
	ownedErr   error
	lookupUUID map[string]int64
	lookupErr  error
}

func (r *supplierOnboardingRepoStub) record(name string) {
	if r.seq != nil {
		*r.seq = append(*r.seq, name)
	}
}

func (s *supplierAccountStoreStub) record(name string) {
	if s.seq != nil {
		*s.seq = append(*s.seq, name)
	}
}

func (r *supplierOnboardingRepoStub) CreateSession(_ context.Context, session *SupplierOAuthSession) error {
	r.calls = append(r.calls, "CreateSession")
	if r.createErr != nil {
		return r.createErr
	}
	copied := *session
	r.createdSession = &copied
	return nil
}

func (r *supplierOnboardingRepoStub) ClaimSession(_ context.Context, _ string, _ int64) (*SupplierOAuthSession, error) {
	r.calls = append(r.calls, "ClaimSession")
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	return r.claimSession, nil
}

func (r *supplierOnboardingRepoStub) CountPendingSessions(_ context.Context, _ int64) (int, error) {
	r.calls = append(r.calls, "CountPendingSessions")
	return r.pendingCount, r.pendingErr
}

func (r *supplierOnboardingRepoStub) DeleteExpiredSessions(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

func (r *supplierOnboardingRepoStub) SetAccountOwner(_ context.Context, accountID, userID int64) error {
	r.calls = append(r.calls, "SetAccountOwner")
	r.record("SetAccountOwner")
	r.setOwnerCalls = append(r.setOwnerCalls, [2]int64{accountID, userID})
	if r.setOwnerErr != nil {
		return r.setOwnerErr
	}
	if r.ownerByAccount == nil {
		r.ownerByAccount = map[int64]int64{}
	}
	r.ownerByAccount[accountID] = userID
	return nil
}

func (r *supplierOnboardingRepoStub) GetAccountOwner(_ context.Context, accountID int64) (int64, error) {
	r.calls = append(r.calls, "GetAccountOwner")
	if r.ownerErr != nil {
		return 0, r.ownerErr
	}
	return r.ownerByAccount[accountID], nil
}

func (r *supplierOnboardingRepoStub) ListAccountIDsByOwner(_ context.Context, _ int64) ([]int64, error) {
	r.calls = append(r.calls, "ListAccountIDsByOwner")
	return r.ownedIDs, r.ownedErr
}

func (r *supplierOnboardingRepoStub) FindAccountIDByUpstreamUUID(_ context.Context, _, accountUUID string) (int64, error) {
	r.calls = append(r.calls, "FindAccountIDByUpstreamUUID")
	if r.lookupErr != nil {
		return 0, r.lookupErr
	}
	return r.lookupUUID[accountUUID], nil
}

// supplierAccountStoreStub 是一个记账式的账号仓储替身。
type supplierAccountStoreStub struct {
	calls []string
	seq   *[]string

	nextID    int64
	accounts  map[int64]*Account
	createErr error

	boundGroups map[int64][]int64
	bindErr     error

	extraUpdates      map[int64]map[string]any
	extraErr          error
	schedulableSets   map[int64]bool
	schedulableCalls  int
	setSchedulableErr error
	getErr            error
}

func newSupplierAccountStoreStub() *supplierAccountStoreStub {
	return &supplierAccountStoreStub{
		nextID:          100,
		accounts:        map[int64]*Account{},
		boundGroups:     map[int64][]int64{},
		extraUpdates:    map[int64]map[string]any{},
		schedulableSets: map[int64]bool{},
	}
}

func (s *supplierAccountStoreStub) Create(_ context.Context, account *Account) error {
	s.calls = append(s.calls, "Create")
	s.record("Create")
	if s.createErr != nil {
		return s.createErr
	}
	account.ID = s.nextID
	s.nextID++
	account.CreatedAt = time.Unix(1700000000, 0)
	stored := *account
	s.accounts[account.ID] = &stored
	return nil
}

func (s *supplierAccountStoreStub) GetByID(_ context.Context, id int64) (*Account, error) {
	s.calls = append(s.calls, "GetByID")
	if s.getErr != nil {
		return nil, s.getErr
	}
	account, ok := s.accounts[id]
	if !ok {
		return nil, errors.New("account not found")
	}
	return account, nil
}

func (s *supplierAccountStoreStub) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	s.calls = append(s.calls, "GetByIDs")
	if s.getErr != nil {
		return nil, s.getErr
	}
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := s.accounts[id]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

func (s *supplierAccountStoreStub) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	s.calls = append(s.calls, "BindGroups")
	s.record("BindGroups")
	if s.bindErr != nil {
		return s.bindErr
	}
	s.boundGroups[accountID] = groupIDs
	return nil
}

func (s *supplierAccountStoreStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	s.calls = append(s.calls, "UpdateExtra")
	if s.extraErr != nil {
		return s.extraErr
	}
	if s.extraUpdates[id] == nil {
		s.extraUpdates[id] = map[string]any{}
	}
	for k, v := range updates {
		s.extraUpdates[id][k] = v
		if account, ok := s.accounts[id]; ok {
			if account.Extra == nil {
				account.Extra = map[string]any{}
			}
			account.Extra[k] = v
		}
	}
	return nil
}

func (s *supplierAccountStoreStub) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	s.calls = append(s.calls, "SetSchedulable")
	s.schedulableCalls++
	if s.setSchedulableErr != nil {
		return s.setSchedulableErr
	}
	s.schedulableSets[id] = schedulable
	if account, ok := s.accounts[id]; ok {
		account.Schedulable = schedulable
	}
	return nil
}

// supplierOAuthStub 冒充协议层。真实的 PKCE 生成与 HTTP 兑换在这里都不相关——
// 本文件测的是编排，不是 OAuth 协议。
type supplierOAuthStub struct {
	auth     *SupplierAuthorization
	authErr  error
	token    *TokenInfo
	tokenErr error

	exchangedCode string
	exchangedAuth *SupplierAuthorization
	requestScope  string
}

func (s *supplierOAuthStub) NewSupplierAuthorization(scope string) (*SupplierAuthorization, error) {
	s.requestScope = scope
	if s.authErr != nil {
		return nil, s.authErr
	}
	if s.auth != nil {
		return s.auth, nil
	}
	return &SupplierAuthorization{
		SessionID:    "sess-1",
		AuthURL:      "https://claude.ai/oauth/authorize?state=st-1",
		State:        "st-1",
		CodeVerifier: "verifier-1",
		Scope:        scope,
	}, nil
}

func (s *supplierOAuthStub) ExchangeSupplierCode(_ context.Context, code string, auth *SupplierAuthorization) (*TokenInfo, error) {
	s.exchangedCode = code
	s.exchangedAuth = auth
	if s.tokenErr != nil {
		return nil, s.tokenErr
	}
	if s.token != nil {
		return s.token, nil
	}
	return &TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		ExpiresAt:    1700003600,
		RefreshToken: "rt",
		Scope:        "user:inference",
		AccountUUID:  "uuid-1",
		EmailAddress: "supplier@example.com",
	}, nil
}

const testOnboardingSupplyGroupID = int64(42)

// newOnboardingService 组一个只带替身的服务。
//
// 直接构造结构体而不走 NewSupplierOnboardingService：构造函数吃的是具体类型
// （*OAuthService / *SettingService），而这里要塞的是替身。字段是包内可见的，
// 测试与被测代码同包，这条路是干净的。
func newOnboardingService(
	t *testing.T,
	repo *supplierOnboardingRepoStub,
	store *supplierAccountStoreStub,
	oauth *supplierOAuthStub,
	settingsJSON string,
) *SupplierOnboardingService {
	t.Helper()
	settingRepo := &supplyPoolSettingRepoStub{value: settingsJSON}
	return &SupplierOnboardingService{
		repo:        repo,
		accountRepo: store,
		oauth:       oauth,
		settings:    newSupplyPoolSettingService(t, settingRepo),
	}
}

func enabledSupplyPoolJSON() string {
	return `{"enabled":true,"supply_group_id":` + strconv.FormatInt(testOnboardingSupplyGroupID, 10) + `,"overflow_group_id":43}`
}

// ============================================================================
// 开关
// ============================================================================

func TestSupplierOnboardingDisabledWhenSupplyPoolNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"未配置", ""},
		{"开关关闭", `{"enabled":false,"supply_group_id":42,"overflow_group_id":43}`},
		{"没有供给分组", `{"enabled":true,"supply_group_id":0,"overflow_group_id":43}`},
		{"供给分组为负", `{"enabled":true,"supply_group_id":-1,"overflow_group_id":43}`},
		{"配置损坏", `{not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newOnboardingService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), &supplierOAuthStub{}, tc.json)

			assert.False(t, svc.IsEnabled(context.Background()))

			_, err := svc.StartOAuth(context.Background(), 7)
			assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)

			_, err = svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "s", Code: "c"})
			assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)
		})
	}
}

func TestSupplierOnboardingEnabledWhenSupplyGroupConfigured(t *testing.T) {
	svc := newOnboardingService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())
	assert.True(t, svc.IsEnabled(context.Background()))
}

// ============================================================================
// StartOAuth
// ============================================================================

func TestStartOAuthPersistsSessionWithOwner(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	oauth := &supplierOAuthStub{}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), oauth, enabledSupplyPoolJSON())

	before := time.Now()
	auth, err := svc.StartOAuth(context.Background(), 7)
	require.NoError(t, err)

	assert.Equal(t, "https://claude.ai/oauth/authorize?state=st-1", auth.AuthURL)
	assert.Equal(t, "sess-1", auth.SessionID)

	require.NotNil(t, repo.createdSession)
	// 归属人来自参数，不是任何一个请求字段。
	assert.Equal(t, int64(7), repo.createdSession.UserID)
	assert.Equal(t, "sess-1", repo.createdSession.SessionID)
	assert.Equal(t, "st-1", repo.createdSession.State)
	assert.Equal(t, "verifier-1", repo.createdSession.CodeVerifier)
	assert.Equal(t, PlatformAnthropic, repo.createdSession.Platform)
	assert.True(t, repo.createdSession.ExpiresAt.After(before))
	assert.True(t, repo.createdSession.ExpiresAt.Before(before.Add(supplierOAuthSessionTTL+time.Minute)))
}

// 只要 inference scope。要到完整 scope 就等于顺手拿到了建 API key、读 profile 的权限，
// 而平台需要的只是替供给者转发推理请求。
func TestStartOAuthRequestsInferenceScopeOnly(t *testing.T) {
	oauth := &supplierOAuthStub{}
	svc := newOnboardingService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), oauth, enabledSupplyPoolJSON())

	_, err := svc.StartOAuth(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "user:inference", oauth.requestScope)
}

func TestStartOAuthRejectsTooManyPendingSessions(t *testing.T) {
	repo := &supplierOnboardingRepoStub{pendingCount: supplierMaxPendingSessions}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.StartOAuth(context.Background(), 7)
	assert.ErrorIs(t, err, ErrSupplierOAuthTooManyPending)
	assert.Nil(t, repo.createdSession, "超限时不该再写一条会话")
}

func TestStartOAuthRejectsAnonymousCaller(t *testing.T) {
	svc := newOnboardingService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())
	_, err := svc.StartOAuth(context.Background(), 0)
	assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)
}

func TestStartOAuthPropagatesSessionWriteError(t *testing.T) {
	repo := &supplierOnboardingRepoStub{createErr: errors.New("db down")}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.StartOAuth(context.Background(), 7)
	assert.Error(t, err)
}

// ============================================================================
// CompleteOAuth
// ============================================================================

func claimedSession() *SupplierOAuthSession {
	return &SupplierOAuthSession{
		SessionID:    "sess-1",
		UserID:       7,
		Platform:     PlatformAnthropic,
		State:        "st-1",
		CodeVerifier: "verifier-1",
		Scope:        "user:inference",
	}
}

func TestCompleteOAuthCreatesUnschedulablePendingAccount(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	view, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "code-1", Name: "我的订阅",
	})
	require.NoError(t, err)

	require.Len(t, store.accounts, 1)
	var created *Account
	for _, a := range store.accounts {
		created = a
	}

	// 这三条是本切片的核心安全性质：新号不可调度、处于观察期、有主。
	assert.False(t, created.Schedulable, "新接入的号必须不可调度")
	assert.Equal(t, SupplyStatePendingReview, created.Extra[SupplyStateExtraKey])
	assert.Equal(t, int64(7), repo.ownerByAccount[created.ID])

	assert.Equal(t, "我的订阅", created.Name)
	assert.Equal(t, PlatformAnthropic, created.Platform)
	assert.Equal(t, AccountTypeSetupToken, created.Type)
	assert.Equal(t, StatusActive, created.Status)
	assert.False(t, created.AutoPauseOnExpired)
	assert.Equal(t, []int64{testOnboardingSupplyGroupID}, store.boundGroups[created.ID])

	require.NotNil(t, view)
	assert.Equal(t, created.ID, view.ID)
	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.False(t, view.Schedulable)
	assert.Equal(t, "supplier@example.com", view.EmailAddress)
}

// 顺序即安全：号建出来到归属写进去之间它必须既不可调度、也不在任何分组里。
// 绑分组排在写归属之后，这条断言把它钉住。
func TestCompleteOAuthWritesOwnerBeforeBindingGroup(t *testing.T) {
	var seq []string
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession(), seq: &seq}
	store := newSupplierAccountStoreStub()
	store.seq = &seq
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.NoError(t, err)

	// 号建出来 → 立刻写归属 → 最后才进池。中间那一段窗口里它既无主也不在池里，
	// 而且不可调度，所以无论如何都服务不了请求。
	assert.Equal(t, []string{"Create", "SetAccountOwner", "BindGroups"}, seq)

	// 会话领取必须是第一件事：兑换之前就要确认这个 code 是这个人的。
	assert.Equal(t, "ClaimSession", repo.calls[0])
}

func TestCompleteOAuthRejectsDuplicateUpstreamAccount(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		lookupUUID:   map[string]int64{"uuid-1": 999},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	assert.ErrorIs(t, err, ErrSupplierAccountAlreadyBound)
	assert.Empty(t, store.accounts, "查重不通过就不该建号")
}

func TestCompleteOAuthPropagatesClaimFailure(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimErr: ErrSupplierOAuthSessionInvalid}
	store := newSupplierAccountStoreStub()
	oauth := &supplierOAuthStub{}
	svc := newOnboardingService(t, repo, store, oauth, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	assert.ErrorIs(t, err, ErrSupplierOAuthSessionInvalid)
	assert.Empty(t, oauth.exchangedCode, "领不到会话就不该拿 code 去兑换")
	assert.Empty(t, store.accounts)
}

func TestCompleteOAuthRejectsEmptyCodeOrSession(t *testing.T) {
	cases := []struct {
		name  string
		input *CompleteOAuthInput
	}{
		{"空 code", &CompleteOAuthInput{UserID: 7, SessionID: "s", Code: "  "}},
		{"空 session", &CompleteOAuthInput{UserID: 7, SessionID: " ", Code: "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
			svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

			_, err := svc.CompleteOAuth(context.Background(), tc.input)
			assert.ErrorIs(t, err, ErrSupplierOAuthSessionInvalid)
			assert.NotContains(t, repo.calls, "ClaimSession")
		})
	}
}

func TestCompleteOAuthUsesSessionPKCEMaterial(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	oauth := &supplierOAuthStub{}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), oauth, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: " code-1 "})
	require.NoError(t, err)

	assert.Equal(t, "code-1", oauth.exchangedCode)
	require.NotNil(t, oauth.exchangedAuth)
	assert.Equal(t, "verifier-1", oauth.exchangedAuth.CodeVerifier)
	assert.Equal(t, "st-1", oauth.exchangedAuth.State)
	assert.Equal(t, "user:inference", oauth.exchangedAuth.Scope)
}

func TestCompleteOAuthFailsWhenOwnerCannotBeWritten(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession(), setOwnerErr: errors.New("conflict")}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.Error(t, err)
	// 写不上归属就绝不绑分组——无主的号不能进池。
	assert.NotContains(t, store.calls, "BindGroups")
}

func TestCompleteOAuthNamesAccountFromEmailWhenUnnamed(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	view, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.NoError(t, err)
	assert.Equal(t, "supplier@example.com", view.Name)
}

func TestCompleteOAuthPropagatesExchangeFailure(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	oauth := &supplierOAuthStub{tokenErr: errors.New("invalid_grant")}
	svc := newOnboardingService(t, repo, store, oauth, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.Error(t, err)
	assert.Empty(t, store.accounts)
}

// ============================================================================
// 凭证组装
// ============================================================================

func TestBuildSupplierClaudeCredentialsMatchesAdminShape(t *testing.T) {
	creds := buildSupplierClaudeCredentials(&TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		ExpiresAt:    1700003600,
		RefreshToken: "rt",
		Scope:        "user:inference",
		OrgUUID:      "org",
		AccountUUID:  "acct",
		EmailAddress: "a@b.c",
	})

	// expires_in / expires_at 必须是字符串：token 刷新与网关按这个类型读。
	assert.Equal(t, "3600", creds["expires_in"])
	assert.Equal(t, "1700003600", creds["expires_at"])
	assert.Equal(t, "at", creds["access_token"])
	assert.Equal(t, "Bearer", creds["token_type"])
	assert.Equal(t, "rt", creds["refresh_token"])
	assert.Equal(t, "acct", creds["account_uuid"])
}

func TestBuildSupplierClaudeCredentialsOmitsEmptyOptionalFields(t *testing.T) {
	creds := buildSupplierClaudeCredentials(&TokenInfo{AccessToken: "at", TokenType: "Bearer"})
	assert.NotContains(t, creds, "refresh_token")
	assert.NotContains(t, creds, "scope")
	assert.NotContains(t, creds, "org_uuid")
	assert.NotContains(t, creds, "account_uuid")
	assert.NotContains(t, creds, "email_address")
}

// ============================================================================
// 归属校验
// ============================================================================

func TestGetAccountRejectsForeignAndFirstPartyAccounts(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Name: "别人的号"}
	store.accounts[101] = &Account{ID: 101, Name: "自营号"}

	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{
		100: 8, // 属于别人
		101: 0, // 自营（owner_user_id IS NULL）
	}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	for _, id := range []int64{100, 101, 999} {
		_, err := svc.GetAccount(context.Background(), 7, id)
		assert.ErrorIs(t, err, ErrSupplierAccountNotFound, "account %d", id)
	}
}

func TestGetAccountReturnsOwnedAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Name: "我的号", Platform: PlatformAnthropic, Status: StatusActive,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	view, err := svc.GetAccount(context.Background(), 7, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(100), view.ID)
	assert.Equal(t, SupplyStateActive, view.SupplyState)
}

func TestListAccountsReturnsOnlyOwnedAccounts(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Name: "我的号 A"}
	store.accounts[101] = &Account{ID: 101, Name: "别人的号"}

	repo := &supplierOnboardingRepoStub{ownedIDs: []int64{100}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	views, err := svc.ListAccounts(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, int64(100), views[0].ID)
}

func TestListAccountsReturnsEmptySliceWhenNothingOwned(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	views, err := svc.ListAccounts(context.Background(), 7)
	require.NoError(t, err)
	assert.NotNil(t, views, "空列表也要是 [] 而不是 null")
	assert.Empty(t, views)
	assert.NotContains(t, store.calls, "GetByIDs", "没有账号就不该再查一次库")
}

// ============================================================================
// 下线 / 重新挂回
// ============================================================================

func TestPauseAccountStopsSchedulingAndMarksRetired(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: true,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100))
	assert.False(t, store.schedulableSets[100])
	assert.Equal(t, SupplyStateRetired, store.extraUpdates[100][SupplyStateExtraKey])
}

func TestPauseAccountRejectsForeignAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: true}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 8}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	err := svc.PauseAccount(context.Background(), 7, 100)
	assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
	assert.Zero(t, store.schedulableCalls, "不是你的号，一个字段都不许动")
}

// 重新挂回只回到观察期，绝不自己变可调度——否则供给者可以绕过观察期把号推进池子。
func TestResumeAccountReturnsToPendingReviewWithoutBecomingSchedulable(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: false,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateRetired},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.ResumeAccount(context.Background(), 7, 100))
	assert.Equal(t, SupplyStatePendingReview, store.extraUpdates[100][SupplyStateExtraKey])
	assert.Zero(t, store.schedulableCalls, "resume 不得触碰可调度性")
	assert.False(t, store.accounts[100].Schedulable)
}

func TestResumeAccountRejectsNonRetiredAccount(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"观察期中", map[string]any{SupplyStateExtraKey: SupplyStatePendingReview}},
		{"已入池", map[string]any{SupplyStateExtraKey: SupplyStateActive}},
		{"没有状态", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newSupplierAccountStoreStub()
			store.accounts[100] = &Account{ID: 100, Extra: tc.extra}
			repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
			svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

			err := svc.ResumeAccount(context.Background(), 7, 100)
			assert.ErrorIs(t, err, ErrSupplierAccountNotRetired)
			assert.NotContains(t, store.calls, "UpdateExtra")
		})
	}
}

// ============================================================================
// 视图裁剪
// ============================================================================

// 视图必须是白名单式的：新增一个内部字段不该自动出现在供给者的响应里。
func TestSupplierAccountViewOmitsInternalFields(t *testing.T) {
	lastUsed := time.Unix(1700001000, 0)
	view := newSupplierAccountView(&Account{
		ID:           100,
		Name:         "号",
		Platform:     PlatformAnthropic,
		Status:       StatusError,
		ErrorMessage: "token expired",
		Schedulable:  true,
		LastUsedAt:   &lastUsed,
		Credentials: map[string]any{
			"access_token":  "SECRET",
			"refresh_token": "SECRET",
			"email_address": "a@b.c",
		},
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	})

	require.NotNil(t, view)
	assert.Equal(t, "a@b.c", view.EmailAddress)
	assert.Equal(t, StatusError, view.Status)
	assert.Equal(t, "token expired", view.ErrorMessage)
	assert.Equal(t, SupplyStateActive, view.SupplyState)

	// 视图是一个固定字段的结构体，凭证根本没有落脚点——这条断言守的是
	// 「以后有人往 SupplierAccountView 上加 Credentials 字段」这种改动。
	assert.NotContains(t, marshalKeys(t, view), "credentials")
	assert.NotContains(t, marshalKeys(t, view), "access_token")
}

func TestSupplyStateOfDefaultsToPendingReview(t *testing.T) {
	assert.Equal(t, SupplyStatePendingReview, supplyStateOf(nil))
	assert.Equal(t, SupplyStatePendingReview, supplyStateOf(&Account{}))
	assert.Equal(t, SupplyStatePendingReview, supplyStateOf(&Account{Extra: map[string]any{}}))
	assert.Equal(t, SupplyStatePendingReview, supplyStateOf(&Account{Extra: map[string]any{SupplyStateExtraKey: ""}}))
	assert.Equal(t, SupplyStateActive, supplyStateOf(&Account{Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive}}))
	// 类型不对（比如被人手工写成数字）按观察期算，不按已入池算。
	assert.Equal(t, SupplyStatePendingReview, supplyStateOf(&Account{Extra: map[string]any{SupplyStateExtraKey: 1}}))
}

func TestSupplierOnboardingServiceIsNilSafe(t *testing.T) {
	var svc *SupplierOnboardingService
	assert.False(t, svc.IsEnabled(context.Background()))

	_, err := svc.StartOAuth(context.Background(), 7)
	assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)

	_, err = svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7})
	assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)

	views, err := svc.ListAccounts(context.Background(), 7)
	assert.NoError(t, err)
	assert.Empty(t, views)

	_, err = svc.GetAccount(context.Background(), 7, 1)
	assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
}

// ============================================================================
// 小工具
// ============================================================================

func indexOf(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

func marshalKeys(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}
