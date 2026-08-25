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

	ownedIDs []int64
	ownedErr error

	// ownedCount / ownedCountErr 是每人上限那道闸读的数。
	//
	// 刻意**不**从 ownedIDs 推导：真实实现里那是两条不同的 SQL，让替身把它们绑成
	// 一个数，就测不出「闸读错了来源」这类错误。默认 0 = 一个号都没挂。
	ownedCount    int
	ownedCountErr error

	// countByIP 按 IP 索引已挂的号数，缺省的键返回 0。
	countByIP       map[string]int
	countByIPErr    error
	countedIPs      []string
	recordedOrigins []supplierOriginRecord
	recordOriginErr error
	// lookupIdentity 按 "键=值" 索引已存在的账号，例如 "account_uuid=uuid-1"。
	// 拍平成一个 map 是为了让测试能一眼看出「哪个键上撞了」。
	lookupIdentity map[string]int64
	lookupKeys     []SupplierIdentityKey
	lookupErr      error

	idsByState map[string][]int64
	stateErr   error

	orphanIDs []int64
	orphanErr error

	scrubCalls [][2]int64
	scrubErr   error

	// acceptances 按 "userID|version" 索引已经存在的同意记录。
	acceptances map[string]*SupplierAgreementAcceptance
	// agreementAcceptedByDefault = 「这个人什么版本都同意过」。
	//
	// newOnboardingService 默认打开它，好让那几十个测接入编排的用例不必每一个都
	// 先补一条同意记录。协议门禁本身有自己的一组用例，那些用例会显式关掉它。
	agreementAcceptedByDefault bool
	recordedAcceptances        []SupplierAgreementAcceptance
	recordAgreementErr         error
	findAgreementErr           error
	latestAgreementErr         error
	latestAgreement            *SupplierAgreementAcceptance

	relayEndpoints map[string]int64
	relayFindErr   error
}

// acceptAgreement 给替身补一条同意记录，供门禁用例摆场景。
func (r *supplierOnboardingRepoStub) acceptAgreement(userID int64, version string) {
	if r.acceptances == nil {
		r.acceptances = map[string]*SupplierAgreementAcceptance{}
	}
	r.acceptances[agreementStubKey(userID, version)] = &SupplierAgreementAcceptance{
		UserID:     userID,
		Version:    version,
		AcceptedAt: time.Unix(1700000000, 0),
	}
}

func agreementStubKey(userID int64, version string) string {
	return strconv.FormatInt(userID, 10) + "|" + version
}

func (r *supplierOnboardingRepoStub) RecordAgreementAcceptance(_ context.Context, acceptance *SupplierAgreementAcceptance) error {
	r.calls = append(r.calls, "RecordAgreementAcceptance")
	r.record("RecordAgreementAcceptance")
	if r.recordAgreementErr != nil {
		return r.recordAgreementErr
	}
	if acceptance == nil {
		return nil
	}
	r.recordedAcceptances = append(r.recordedAcceptances, *acceptance)
	// 与真实实现同构：ON CONFLICT DO NOTHING，重复同意保留最早那一行。
	if r.acceptances == nil {
		r.acceptances = map[string]*SupplierAgreementAcceptance{}
	}
	key := agreementStubKey(acceptance.UserID, acceptance.Version)
	if _, exists := r.acceptances[key]; !exists {
		stored := *acceptance
		r.acceptances[key] = &stored
	}
	return nil
}

func (r *supplierOnboardingRepoStub) FindAgreementAcceptance(_ context.Context, userID int64, version string) (*SupplierAgreementAcceptance, error) {
	// 刻意不进 seq：那条流水记的是跨仓储的**写**顺序（建号→归属→进池），
	// 而这里是一次纯读，混进去只会让那几条顺序断言变得难读。
	r.calls = append(r.calls, "FindAgreementAcceptance")
	if r.findAgreementErr != nil {
		return nil, r.findAgreementErr
	}
	if found, ok := r.acceptances[agreementStubKey(userID, version)]; ok {
		return found, nil
	}
	if r.agreementAcceptedByDefault {
		return &SupplierAgreementAcceptance{
			UserID: userID, Version: version, AcceptedAt: time.Unix(1700000000, 0),
		}, nil
	}
	return nil, nil
}

func (r *supplierOnboardingRepoStub) LatestAgreementAcceptance(_ context.Context, _ int64) (*SupplierAgreementAcceptance, error) {
	r.calls = append(r.calls, "LatestAgreementAcceptance")
	if r.latestAgreementErr != nil {
		return nil, r.latestAgreementErr
	}
	return r.latestAgreement, nil
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

// supplierOriginRecord 记下一次接入来源写入，字段顺序与仓储方法一致。
type supplierOriginRecord struct {
	accountID int64
	userID    int64
	clientIP  string
}

// 两个 COUNT 都不进 seq，与 FindAgreementAcceptance 同一个理由：那条流水记的是
// 跨仓储的**写**顺序。它们相对于领会话的先后另有断言（看 r.calls）。
func (r *supplierOnboardingRepoStub) CountAccountsByOwner(_ context.Context, _ int64) (int, error) {
	r.calls = append(r.calls, "CountAccountsByOwner")
	return r.ownedCount, r.ownedCountErr
}

func (r *supplierOnboardingRepoStub) CountAccountsByOriginIP(_ context.Context, clientIP string) (int, error) {
	r.calls = append(r.calls, "CountAccountsByOriginIP")
	r.countedIPs = append(r.countedIPs, clientIP)
	if r.countByIPErr != nil {
		return 0, r.countByIPErr
	}
	return r.countByIP[clientIP], nil
}

func (r *supplierOnboardingRepoStub) RecordAccountOrigin(_ context.Context, accountID, userID int64, clientIP string) error {
	r.calls = append(r.calls, "RecordAccountOrigin")
	r.record("RecordAccountOrigin")
	if r.recordOriginErr != nil {
		return r.recordOriginErr
	}
	r.recordedOrigins = append(r.recordedOrigins, supplierOriginRecord{accountID, userID, clientIP})
	return nil
}

func (r *supplierOnboardingRepoStub) ListAccountIDsBySupplyState(_ context.Context, state string, _ int) ([]int64, error) {
	r.calls = append(r.calls, "ListAccountIDsBySupplyState")
	if r.stateErr != nil {
		return nil, r.stateErr
	}
	return r.idsByState[state], nil
}

func (r *supplierOnboardingRepoStub) ListAccountIDsWithUnavailableOwner(_ context.Context, _ int) ([]int64, error) {
	r.calls = append(r.calls, "ListAccountIDsWithUnavailableOwner")
	if r.orphanErr != nil {
		return nil, r.orphanErr
	}
	return r.orphanIDs, nil
}

func (r *supplierOnboardingRepoStub) ScrubAccountCredentials(_ context.Context, accountID, userID int64) error {
	r.calls = append(r.calls, "ScrubAccountCredentials")
	r.record("ScrubAccountCredentials")
	r.scrubCalls = append(r.scrubCalls, [2]int64{accountID, userID})
	return r.scrubErr
}

// relayEndpoints 已存在的中转端点，键 "baseURL|apiKey" → 账号 id（M7 查重桩）。
func (r *supplierOnboardingRepoStub) FindAccountIDByRelayEndpoint(_ context.Context, _ string, baseURL, apiKey string) (int64, error) {
	if r.relayFindErr != nil {
		return 0, r.relayFindErr
	}
	if id, ok := r.relayEndpoints[baseURL+"|"+apiKey]; ok {
		return id, nil
	}
	return 0, nil
}

func (r *supplierOnboardingRepoStub) FindAccountIDByUpstreamIdentity(_ context.Context, _ string, key SupplierIdentityKey, value string) (int64, error) {
	r.calls = append(r.calls, "FindAccountIDByUpstreamIdentity")
	r.lookupKeys = append(r.lookupKeys, key)
	if r.lookupErr != nil {
		return 0, r.lookupErr
	}
	return r.lookupIdentity[string(key)+"="+value], nil
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

	deletedIDs []int64
	deleteErr  error
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

// Delete 只记账，**不**把账号从 map 里拿掉。
//
// 刻意如此：真实实现是软删，行还在，只是查询看不见了。留着才能让测试在删除之后
// 仍然读得到那一行，去断言凭证是不是真的被抹掉了——那正是解绑最要紧的一条性质。
func (s *supplierAccountStoreStub) Delete(_ context.Context, id int64) error {
	s.calls = append(s.calls, "Delete")
	s.record("Delete")
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletedIDs = append(s.deletedIDs, id)
	return nil
}

func (s *supplierAccountStoreStub) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	s.calls = append(s.calls, "SetSchedulable")
	s.record("SetSchedulable")
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

// testClientIP 是这些用例里"请求来自哪"的默认答案。
//
// 是一个非空值而不是 ""：空 IP 会让每 IP 那道闸整条跳过，用它当默认值等于让所有
// 用例都在一条不具代表性的路径上跑。空 IP 自己有专门的用例。
const testClientIP = "203.0.113.7"

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
	// 接入上限传空 = 走默认（每人 5 个、每 IP 不限）。默认值本身就是一道开着的闸，
	// 所以这些用例里 repo.ownedCount 的默认 0 是有意义的前提，不是无关变量。
	return newOnboardingServiceWithLimits(t, repo, store, oauth, settingsJSON, "")
}

// newOnboardingServiceWithLimits 同上，但显式指定接入上限那一份配置。
//
// 单开一个入口而不是给 newOnboardingService 加参数：上限只有那一组用例关心，
// 让另外几十个调用点都跟着多写一个 "" 只会让它们更难读。
func newOnboardingServiceWithLimits(
	t *testing.T,
	repo *supplierOnboardingRepoStub,
	store *supplierAccountStoreStub,
	oauth *supplierOAuthStub,
	settingsJSON string,
	onboardingJSON string,
) *SupplierOnboardingService {
	t.Helper()
	// 默认场景是「协议已发布，且这个人已经同意」。协议门禁自己那一组用例会把
	// 这两个前提逐个拆掉；其余用例测的是编排，不该每一个都先演一遍签字。
	settingRepo := &supplyPoolSettingRepoStub{
		value:           settingsJSON,
		agreementValue:  publishedAgreementJSON(),
		onboardingValue: onboardingJSON,
	}
	repo.agreementAcceptedByDefault = true
	return &SupplierOnboardingService{
		repo:        repo,
		accountRepo: store,
		oauth:       oauth,
		settings:    newSupplyPoolSettingService(t, settingRepo),
	}
}

// testAgreementVersion 是这些用例里"当前生效"的协议版本。
const testAgreementVersion = "v1"

func publishedAgreementJSON() string {
	return `{"version":"` + testAgreementVersion + `","url":"https://example.com/supplier-terms","body":"条款正文"}`
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

			_, err := svc.StartOAuth(context.Background(), 7, testClientIP)
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
	auth, err := svc.StartOAuth(context.Background(), 7, testClientIP)
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

	_, err := svc.StartOAuth(context.Background(), 7, testClientIP)
	require.NoError(t, err)
	assert.Equal(t, "user:inference", oauth.requestScope)
}

func TestStartOAuthRejectsTooManyPendingSessions(t *testing.T) {
	repo := &supplierOnboardingRepoStub{pendingCount: supplierMaxPendingSessions}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.StartOAuth(context.Background(), 7, testClientIP)
	assert.ErrorIs(t, err, ErrSupplierOAuthTooManyPending)
	assert.Nil(t, repo.createdSession, "超限时不该再写一条会话")
}

func TestStartOAuthRejectsAnonymousCaller(t *testing.T) {
	svc := newOnboardingService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())
	_, err := svc.StartOAuth(context.Background(), 0, testClientIP)
	assert.ErrorIs(t, err, ErrSupplierOnboardingDisabled)
}

func TestStartOAuthPropagatesSessionWriteError(t *testing.T) {
	repo := &supplierOnboardingRepoStub{createErr: errors.New("db down")}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.StartOAuth(context.Background(), 7, testClientIP)
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

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})
	require.NoError(t, err)

	// 号建出来 → 立刻写归属 → 记来源 → 最后才进池。中间那一段窗口里它既无主也不
	// 在池里，而且不可调度，所以无论如何都服务不了请求。
	//
	// 来源记在写归属**之后**：那一行说的是「这个属于某人的号从哪来」，归属没写上
	// 时它没有意义。
	assert.Equal(t, []string{"Create", "SetAccountOwner", "RecordAccountOrigin", "BindGroups"}, seq)

	// 两道纯读的门禁（协议、数量上限）排在领会话之前，领会话排在其余一切之前：
	// 门禁挡住的人不该丢掉手上的授权码；而领取一旦发生，兑换之前就已经确认了
	// 这个 code 是这个人的。
	require.GreaterOrEqual(t, len(repo.calls), 3)
	assert.Equal(t,
		[]string{"FindAgreementAcceptance", "CountAccountsByOwner", "ClaimSession"},
		repo.calls[:3])
}

func TestCompleteOAuthRejectsDuplicateUpstreamAccount(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession:   claimedSession(),
		lookupIdentity: map[string]int64{"account_uuid=uuid-1": 999},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	assert.ErrorIs(t, err, ErrSupplierAccountAlreadyBound)
	assert.Empty(t, store.accounts, "查重不通过就不该建号")
}

// 早先挂上来的号可能只记下了邮箱（那一次上游没给 uuid），这次上游给了 uuid。
// 只查最强的那个键就会放行同一份订阅，于是同一份额度被计两份分成。
func TestCompleteOAuthRejectsDuplicateByEmailWhenUUIDIsFresh(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession:   claimedSession(),
		lookupIdentity: map[string]int64{"email_address=supplier@example.com": 999},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	assert.ErrorIs(t, err, ErrSupplierAccountAlreadyBound)
	assert.Empty(t, store.accounts)
	// 拿到几个键就查几个，顺序从强到弱。
	assert.Equal(t,
		[]SupplierIdentityKey{SupplierIdentityAccountUUID, SupplierIdentityEmailAddress},
		repo.lookupKeys)
}

// 一个身份键都拿不到时**拒绝挂号**。
//
// 这是整套结算里唯一一个能凭空造钱的口子：查不了重就挡不住同一份订阅挂任意多次，
// 每一份都按同一份额度独立计分成。宁可让供给者重走一遍授权。
func TestCompleteOAuthRefusesWhenUpstreamGivesNoIdentity(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	oauth := &supplierOAuthStub{token: &TokenInfo{
		AccessToken: "at",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		// account / organization 两个块在上游响应里都是可选的，这不是假想情况。
	}}
	svc := newOnboardingService(t, repo, store, oauth, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	assert.ErrorIs(t, err, ErrSupplierAccountIdentityUnavailable)
	assert.Empty(t, store.accounts, "查不了重就不能建号")
	assert.Empty(t, repo.lookupKeys, "没有键可查，不该白打一次库")
}

// 只有邮箱也能挂：邮箱足够把同一份订阅认出来。
func TestCompleteOAuthAcceptsEmailOnlyIdentity(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	oauth := &supplierOAuthStub{token: &TokenInfo{
		AccessToken:  "at",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		EmailAddress: "only-email@example.com",
	}}
	svc := newOnboardingService(t, repo, store, oauth, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.NoError(t, err)
	require.Len(t, store.accounts, 1)
	assert.Equal(t, []SupplierIdentityKey{SupplierIdentityEmailAddress}, repo.lookupKeys)
}

// 查重查不动时必须报错，不能放行——闸门的开关不能建立在「数据库这一刻是否健康」之上。
func TestCompleteOAuthFailsClosedWhenLookupErrors(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		lookupErr:    errors.New("connection reset"),
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c"})
	require.Error(t, err)
	assert.Empty(t, store.accounts)
}

// org_uuid 刻意不是身份键：团队组织下多个成员各有各的订阅席位，
// 拿它查重会把同事的合法第二个号判成重复——一个会挡住真实供给的误报。
func TestSupplierIdentityKeysExcludeOrgUUID(t *testing.T) {
	assert.Equal(t,
		[]SupplierIdentityKey{SupplierIdentityAccountUUID, SupplierIdentityEmailAddress},
		SupplierIdentityKeys)

	values := supplierIdentityValues(&TokenInfo{OrgUUID: "org-1"})
	assert.Empty(t, values, "只有 org_uuid 不算拿到了身份")
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

// 两条通道共有的那一半：无论选哪条，「停止接新单」都是立刻生效的。
func TestPauseAccountAlwaysStopsSchedulingImmediately(t *testing.T) {
	for _, mode := range []string{SupplyPauseModeGraceful, SupplyPauseModeImmediate, "", "gArBaGe"} {
		t.Run("mode="+mode, func(t *testing.T) {
			store := newSupplierAccountStoreStub()
			store.accounts[100] = &Account{
				ID: 100, Schedulable: true,
				Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
			}
			repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
			svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

			require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, mode))
			assert.False(t, store.schedulableSets[100], "下线的第一件事永远是停止接新单")
		})
	}
}

// 优雅下线进排空窗，不直接进终态，并且记下从哪来（取消下线要靠它回退）。
func TestPauseAccountGracefulEntersDrainingWithDeadline(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: true,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeGraceful))
	updates := store.extraUpdates[100]
	assert.Equal(t, SupplyStateDraining, updates[SupplyStateExtraKey])
	assert.Equal(t, SupplyStateActive, updates[SupplyDrainFromExtraKey])

	until, err := time.Parse(time.RFC3339, updates[SupplyDrainUntilExtraKey].(string))
	require.NoError(t, err)
	assert.True(t, until.After(time.Now()), "排空窗必须落在未来")
}

// 立即拔出直接进终态，没有窗口。
func TestPauseAccountImmediateGoesStraightToRetired(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: true,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeImmediate))
	updates := store.extraUpdates[100]
	assert.Equal(t, SupplyStateRetired, updates[SupplyStateExtraKey])
	assert.Empty(t, updates[SupplyDrainUntilExtraKey], "终态不该留着排空窗")
}

// 排空窗配成 0 时优雅下线没有意义——直接终态，而不是造一个立刻到期的中间态。
func TestPauseAccountGracefulWithZeroWindowRetiresDirectly(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: true,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	settingRepo := &supplyPoolSettingRepoStub{
		value:          enabledSupplyPoolJSON(),
		probationValue: `{"enabled":true,"drain_window_minutes":0}`,
	}
	svc := &SupplierOnboardingService{
		repo: repo, accountRepo: store, oauth: &supplierOAuthStub{},
		settings: newSupplyPoolSettingService(t, settingRepo),
	}

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeGraceful))
	assert.Equal(t, SupplyStateRetired, store.extraUpdates[100][SupplyStateExtraKey])
}

// 反复点优雅下线不能把排空窗一直往后推——否则号可以无限期停在既不接单也没下线的中间态。
func TestPauseAccountGracefulTwiceDoesNotExtendWindow(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: false,
		Extra: map[string]any{
			SupplyStateExtraKey:      SupplyStateDraining,
			SupplyDrainUntilExtraKey: time.Now().Add(time.Minute).Format(time.RFC3339),
			SupplyDrainFromExtraKey:  SupplyStateActive,
		},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeGraceful))
	assert.Nil(t, store.extraUpdates[100], "已经在排空窗里就什么都不改")
}

// 排空窗内可以升级成立即拔出：反悔的反面也得走得通。
func TestPauseAccountImmediateOverridesDraining(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: false,
		Extra: map[string]any{
			SupplyStateExtraKey:      SupplyStateDraining,
			SupplyDrainUntilExtraKey: time.Now().Add(time.Hour).Format(time.RFC3339),
			SupplyDrainFromExtraKey:  SupplyStateActive,
		},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeImmediate))
	assert.Equal(t, SupplyStateRetired, store.extraUpdates[100][SupplyStateExtraKey])
}

func TestPauseAccountRejectsForeignAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: true}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 8}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	err := svc.PauseAccount(context.Background(), 7, 100, SupplyPauseModeImmediate)
	assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
	assert.Zero(t, store.schedulableCalls, "不是你的号，一个字段都不许动")
}

// 从终态重新挂回只回到观察期，绝不自己变可调度——否则供给者可以绕过观察期把号推进池子。
func TestResumeAccountReturnsToPendingReviewWithoutBecomingSchedulable(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: false,
		Extra: map[string]any{
			SupplyStateExtraKey:       SupplyStateRetired,
			SupplyProbePassesExtraKey: 5,
		},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.ResumeAccount(context.Background(), 7, 100))
	updates := store.extraUpdates[100]
	assert.Equal(t, SupplyStatePendingReview, updates[SupplyStateExtraKey])
	assert.Equal(t, 0, updates[SupplyProbePassesExtraKey], "重新挂回要从零开始观察")
	assert.NotEmpty(t, updates[SupplyProbationSinceExtraKey], "观察窗要重新计时")
	assert.Zero(t, store.schedulableCalls, "resume 不得触碰可调度性")
	assert.False(t, store.accounts[100].Schedulable)
}

// 排空窗内取消下线：回到进入排空之前的那个状态，不重走观察期。
func TestResumeAccountCancelsDrainingBackToActive(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{
		ID: 100, Schedulable: false,
		Extra: map[string]any{
			SupplyStateExtraKey:      SupplyStateDraining,
			SupplyDrainUntilExtraKey: time.Now().Add(time.Hour).Format(time.RFC3339),
			SupplyDrainFromExtraKey:  SupplyStateActive,
		},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.ResumeAccount(context.Background(), 7, 100))
	updates := store.extraUpdates[100]
	assert.Equal(t, SupplyStateActive, updates[SupplyStateExtraKey])
	assert.Empty(t, updates[SupplyDrainUntilExtraKey])
	assert.True(t, store.schedulableSets[100], "本来就在池里，取消下线要原样放回去")
	assert.NotContains(t, updates, SupplyProbationSinceExtraKey, "取消下线不重走观察期")
}

// 排空来路不明（字段被手工动过）时回到观察期，不回到 active。
func TestResumeAccountCancelsDrainingWithUnknownOriginFallsBackToPendingReview(t *testing.T) {
	for _, origin := range []any{nil, "", "nonsense", 42} {
		extra := map[string]any{
			SupplyStateExtraKey:      SupplyStateDraining,
			SupplyDrainUntilExtraKey: time.Now().Add(time.Hour).Format(time.RFC3339),
		}
		if origin != nil {
			extra[SupplyDrainFromExtraKey] = origin
		}
		store := newSupplierAccountStoreStub()
		store.accounts[100] = &Account{ID: 100, Extra: extra}
		repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
		svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

		require.NoError(t, svc.ResumeAccount(context.Background(), 7, 100))
		assert.Equal(t, SupplyStatePendingReview, store.extraUpdates[100][SupplyStateExtraKey])
		assert.Zero(t, store.schedulableCalls, "来路不明就不许自己变可调度")
	}
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
// 解绑
// ============================================================================

// 解绑的三步必须按「停调度 → 抹凭证 → 摘号」的顺序发生。
//
// 顺序不是风格问题，两处颠倒各有各的后果：
//   - 抹凭证排在停调度之前，中间失败会留下一个还在接单、却已经没有凭证的号；
//   - 摘号排在抹凭证之前，软删之后那条带 `deleted_at IS NULL` 的 UPDATE 就再也
//     匹配不上，凭证会永久留在一行没人再看的记录里——正是这个功能要消灭的东西。
func TestDetachAccountStopsScrubsThenRemovesInThatOrder(t *testing.T) {
	var seq []string
	store := newSupplierAccountStoreStub()
	store.seq = &seq
	store.accounts[100] = &Account{
		ID: 100, Schedulable: true,
		Extra: map[string]any{SupplyStateExtraKey: SupplyStateActive},
	}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}, seq: &seq}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.DetachAccount(context.Background(), 7, 100))

	assert.Equal(t, []string{"SetSchedulable", "ScrubAccountCredentials", "Delete"}, seq)
	assert.False(t, store.schedulableSets[100])
	assert.Equal(t, []int64{100}, store.deletedIDs)
}

// 抹凭证时必须把归属人一起传下去。
//
// 仓储那条 UPDATE 的 WHERE 里带着 owner_user_id，传错人等于抹不掉（或者更糟，
// 抹掉别人的）。这个断言守的是参数，不是行为——因为传错了不会有任何症状。
func TestDetachAccountScrubsScopedToOwner(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	require.NoError(t, svc.DetachAccount(context.Background(), 7, 100))
	assert.Equal(t, [][2]int64{{100, 7}}, repo.scrubCalls)
}

// 别人的号一个字段都不许动——尤其不许抹凭证。
func TestDetachAccountRejectsForeignAccount(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: true}
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 8}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	err := svc.DetachAccount(context.Background(), 7, 100)
	assert.ErrorIs(t, err, ErrSupplierAccountNotFound)
	assert.Empty(t, repo.scrubCalls, "不是你的号，凭证轮不到你抹")
	assert.Empty(t, store.deletedIDs)
	assert.Zero(t, store.schedulableCalls)
}

// 抹凭证失败就必须停下来，绝不能继续摘号。
//
// 继续走下去的后果是最坏的那一个：行被软删（谁也看不见了），凭证却还在里面，
// 而供给者收到的是一句「已解绑」。
func TestDetachAccountDoesNotRemoveAccountWhenScrubFails(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100, Schedulable: true}
	repo := &supplierOnboardingRepoStub{
		ownerByAccount: map[int64]int64{100: 7},
		scrubErr:       errors.New("boom"),
	}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	err := svc.DetachAccount(context.Background(), 7, 100)
	require.Error(t, err)
	assert.Empty(t, store.deletedIDs, "凭证还在，号就不能消失")
	assert.False(t, store.schedulableSets[100], "但停调度已经落地，不回滚")
}

// 摘号失败照样报错。凭证已经没了，但供给者的列表里还留着一行，他需要知道要重试。
func TestDetachAccountReportsDeleteFailureAfterScrub(t *testing.T) {
	store := newSupplierAccountStoreStub()
	store.accounts[100] = &Account{ID: 100}
	store.deleteErr = errors.New("boom")
	repo := &supplierOnboardingRepoStub{ownerByAccount: map[int64]int64{100: 7}}
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	err := svc.DetachAccount(context.Background(), 7, 100)
	require.Error(t, err)
	assert.Len(t, repo.scrubCalls, 1, "凭证该抹的已经抹了")
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
	}, nil)

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

	_, err := svc.StartOAuth(context.Background(), 7, testClientIP)
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
