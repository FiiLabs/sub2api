// APEXONE-EXT: 双边市场——供给者自助接入的流程编排。
//
// 供给者点「连接我的 Claude 订阅」→ 拿到授权链接（StartOAuth）→ 在 Anthropic 授权 →
// 把 code 贴回来（CompleteOAuth）→ 平台建出一个 owner_user_id 指向他的 accounts 行。
//
// # 新账号一定是「不可调度」的
//
// CompleteOAuth 建出来的账号 `schedulable=false`、supply_state=pending_review。这不是
// 保守，是必须：账号刚建出来到归属写进去之间有一个窗口，窗口里它是一个**没有主人**的号，
// 若能被调度，这段时间产生的用量按自营账号计——供给者干了活拿不到钱，而且事后无从追认
// （usage_log 不会回溯归属）。所以建号时显式 `Schedulable: false`，并且先不绑分组：
// 两条独立的理由让它服务不了任何请求，任一条失效另一条还在。
//
// 也因此**本切片没有任何东西会把 pending_review 变成 active**——观察期与入池是 #9 的事。
// 现状是：供给者能接上、能在仪表盘看见自己的号、能自己下线，但号还不会接真实流量。
// 这是有意的顺序（先把归属和钱的账本铺好，再放流量），不是漏做。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

const (
	// supplierOAuthSessionTTL 授权会话的有效期。
	//
	// 覆盖「点开链接 → 登录 Anthropic → 授权 → 把 code 贴回来」的人类操作时长，
	// 又短到让一条泄漏的会话没什么可用窗口。
	supplierOAuthSessionTTL = 15 * time.Minute
	// supplierMaxPendingSessions 单个供给者同时可以有几条未完成会话。
	//
	// 不是一条：用户点了链接没授权、换个浏览器再点一次是完全正常的操作。
	// 但也不能不限——每条会话都是一行数据库记录和一次上游授权页跳转。
	supplierMaxPendingSessions = 5
	// supplierSessionCleanupBatch 每次发起授权时顺手清理多少条过期会话。
	supplierSessionCleanupBatch = 200
	// supplierAccountNameMaxLen 供给者能给自己的号起的名字长度上限。
	supplierAccountNameMaxLen = 64
	// supplierDefaultConcurrency 新接入账号的并发上限，取 ent 默认值。
	supplierDefaultConcurrency = 3
	// supplierDefaultPriority 新接入账号的调度优先级，取 ent 默认值。
	//
	// 刻意与自营账号同档：供给账号不该因为「是外部的」就被系统性地排在后面或前面。
	// 池间的取舍由分组做（见 setting_supply_pool.go），不由优先级偷偷做。
	supplierDefaultPriority = 50
)

// supplierClaudeOAuth 是自助接入需要的 OAuth 协议能力。
//
// 窄接口而不是直接吃 *OAuthService：这里只需要「造材料」和「拿材料换 token」两件事，
// 声明成两个方法能让测试不必造一个带 sessionStore 和 HTTP client 的真服务。
type supplierClaudeOAuth interface {
	NewSupplierAuthorization(scope string) (*SupplierAuthorization, error)
	ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*TokenInfo, error)
}

// supplierSupplyPoolReader 读供给池路由配置，用来确定新账号该挂哪个分组。
type supplierSupplyPoolReader interface {
	GetSupplyPoolSettings(ctx context.Context) *SupplyPoolSettings
}

// supplierAccountStore 是自助接入用到的账号读写子集。
//
// 从 AccountRepository 里挑出来的四个方法。窄接口在这里有实际作用：它让「自助接入
// 能对账号做什么」变成一份可以一眼读完的清单——建、读、改 extra、改可调度性。
// 删号、改凭证、改分组绑定都不在里面，供给者的接口够不到它们。
type supplierAccountStore interface {
	Create(ctx context.Context, account *Account) error
	GetByID(ctx context.Context, id int64) (*Account, error)
	GetByIDs(ctx context.Context, ids []int64) ([]*Account, error)
	BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
}

// SupplierOnboardingService 编排供给者自助接入。
type SupplierOnboardingService struct {
	repo        SupplierOnboardingRepository
	accountRepo supplierAccountStore
	oauth       supplierClaudeOAuth
	settings    supplierSupplyPoolReader
}

// NewSupplierOnboardingService 构造自助接入服务。
func NewSupplierOnboardingService(
	repo SupplierOnboardingRepository,
	accountRepo AccountRepository,
	oauthService *OAuthService,
	settingService *SettingService,
) *SupplierOnboardingService {
	return &SupplierOnboardingService{
		repo:        repo,
		accountRepo: accountRepo,
		oauth:       oauthService,
		settings:    settingService,
	}
}

// supplyGroupID 返回新账号该挂的供给池分组，同时充当「自助接入是否开放」的判据。
//
// 两件事合成一个判断是刻意的：没有供给池分组，账号建出来就没有池可进，
// 「接入成功但永远不会被调度」是一个比「暂不开放」更难解释的状态。
func (s *SupplierOnboardingService) supplyGroupID(ctx context.Context) (int64, bool) {
	if s == nil || s.settings == nil {
		return 0, false
	}
	settings := s.settings.GetSupplyPoolSettings(ctx)
	if settings == nil || !settings.Enabled || settings.SupplyGroupID <= 0 {
		return 0, false
	}
	return settings.SupplyGroupID, true
}

// IsEnabled 供前端决定要不要显示接入入口。
func (s *SupplierOnboardingService) IsEnabled(ctx context.Context) bool {
	_, ok := s.supplyGroupID(ctx)
	return ok
}

// StartOAuth 为 userID 发起一次授权，返回授权链接与会话句柄。
func (s *SupplierOnboardingService) StartOAuth(ctx context.Context, userID int64) (*SupplierAuthorization, error) {
	if s == nil || s.repo == nil || s.oauth == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if userID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}
	if _, ok := s.supplyGroupID(ctx); !ok {
		return nil, ErrSupplierOnboardingDisabled
	}

	pending, err := s.repo.CountPendingSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pending >= supplierMaxPendingSessions {
		return nil, ErrSupplierOAuthTooManyPending
	}

	// setup-token（user:inference）而不是完整 scope：平台需要的只是替供给者转发推理请求。
	// 完整 scope 还能读 profile、建 API key、列会话——供给者把订阅挂上来不等于把账号交出来，
	// 多要一分权限就多一分「平台能拿它干别的」的空间。
	auth, err := s.oauth.NewSupplierAuthorization(oauth.ScopeInference)
	if err != nil {
		return nil, err
	}

	session := &SupplierOAuthSession{
		SessionID:    auth.SessionID,
		UserID:       userID,
		Platform:     PlatformAnthropic,
		State:        auth.State,
		CodeVerifier: auth.CodeVerifier,
		Scope:        auth.Scope,
		ExpiresAt:    time.Now().Add(supplierOAuthSessionTTL),
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	s.cleanupExpiredSessions()
	return auth, nil
}

// cleanupExpiredSessions 顺手清掉过期会话。
//
// 没有单起一个后台任务：会话行很小、十五分钟就过期、量级是「有多少人在接入」而不是
// 「有多少请求」。挂在发起授权这条低频路径上足够，而且天然自限——没人接入就不用清。
// 异步跑是因为它对本次请求的结果毫无影响，失败了下次再清。
func (s *SupplierOnboardingService) cleanupExpiredSessions() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("[SupplierOnboarding] session cleanup panic", "recover", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.repo.DeleteExpiredSessions(ctx, supplierSessionCleanupBatch); err != nil {
			slog.Warn("[SupplierOnboarding] session cleanup failed", "error", err)
		}
	}()
}

// CompleteOAuthInput 兑换授权码的输入。
type CompleteOAuthInput struct {
	UserID    int64
	SessionID string
	Code      string
	// Name 供给者给这个号起的备注名，可空。
	Name string
}

// CompleteOAuth 兑换授权码并建出供给账号。
//
// 顺序是 领会话 → 换 token → 查重 → 建号 → 写归属 → 绑分组，每一步都在为
// 「账号在有主之前不能服务任何请求」这一条让路。
func (s *SupplierOnboardingService) CompleteOAuth(ctx context.Context, input *CompleteOAuthInput) (*SupplierAccountView, error) {
	if s == nil || s.repo == nil || s.oauth == nil || s.accountRepo == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if input == nil || input.UserID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}
	groupID, ok := s.supplyGroupID(ctx)
	if !ok {
		return nil, ErrSupplierOnboardingDisabled
	}
	if strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.SessionID) == "" {
		return nil, ErrSupplierOAuthSessionInvalid
	}

	// 原子领取。会话在这一刻就被标成已消费，即使后面的兑换失败也不退回——
	// 一个授权码本来就只能换一次，失败了就该重新走一遍授权，而不是拿同一个 code 重试。
	session, err := s.repo.ClaimSession(ctx, strings.TrimSpace(input.SessionID), input.UserID)
	if err != nil {
		return nil, err
	}

	tokenInfo, err := s.oauth.ExchangeSupplierCode(ctx, strings.TrimSpace(input.Code), &SupplierAuthorization{
		State:        session.State,
		CodeVerifier: session.CodeVerifier,
		Scope:        session.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	// 查重必须在建号之前。同一个上游订阅被挂两次（自己挂两遍，或被两个人分别挂），
	// 两个账号会按同一份额度各算各的分成——平台按两份供给计价，实际只有一份。
	if tokenInfo.AccountUUID != "" {
		existingID, err := s.repo.FindAccountIDByUpstreamUUID(ctx, session.Platform, tokenInfo.AccountUUID)
		if err != nil {
			return nil, err
		}
		if existingID > 0 {
			return nil, ErrSupplierAccountAlreadyBound
		}
	}

	account := &Account{
		Name:     s.accountName(input.Name, tokenInfo),
		Platform: session.Platform,
		// setup-token：与 scope 一致。类型判错会让 token 刷新走错分支。
		Type:        AccountTypeSetupToken,
		Credentials: buildSupplierClaudeCredentials(tokenInfo),
		Extra: map[string]any{
			SupplyStateExtraKey: SupplyStatePendingReview,
		},
		Concurrency: supplierDefaultConcurrency,
		Priority:    supplierDefaultPriority,
		Status:      StatusActive,
		// 显式 false，不靠零值。见文件头。
		Schedulable: false,
		// 供给者的订阅到期是他自己的事，平台不替他自动停号——号失效会由上游 401
		// 走既有的错误状态机，那条路径有告警、有错误信息，比静默暂停可诊断。
		AutoPauseOnExpired: false,
	}
	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create supply account: %w", err)
	}

	if err := s.repo.SetAccountOwner(ctx, account.ID, input.UserID); err != nil {
		// 归属没写上，这个号就是个无主的孤儿。它现在 schedulable=false 且未绑分组，
		// 服务不了任何请求，但留着只会让管理员在账号列表里看到一条来历不明的记录。
		// 补偿失败也只能记日志——此时能做的都做了，剩下的是运维的事。
		slog.Error("[SupplierOnboarding] failed to set account owner, orphan account left behind",
			"account_id", account.ID, "user_id", input.UserID, "error", err)
		return nil, fmt.Errorf("set supply account owner: %w", err)
	}

	// 绑分组放在最后：绑上之后它就在供给池里了，只是还不可调度。
	// 这一步失败不回滚——账号已经有主，删掉它等于替供给者做主销毁他的授权结果。
	if err := s.accountRepo.BindGroups(ctx, account.ID, []int64{groupID}); err != nil {
		slog.Error("[SupplierOnboarding] failed to bind supply group, account is owned but unpooled",
			"account_id", account.ID, "group_id", groupID, "error", err)
		return nil, fmt.Errorf("bind supply group: %w", err)
	}

	return newSupplierAccountView(account), nil
}

// accountName 决定新号在管理端和供给者仪表盘里叫什么。
func (s *SupplierOnboardingService) accountName(requested string, tokenInfo *TokenInfo) string {
	name := strings.TrimSpace(requested)
	if name == "" && tokenInfo != nil {
		name = strings.TrimSpace(tokenInfo.EmailAddress)
	}
	if name == "" {
		name = "supply-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if len(name) > supplierAccountNameMaxLen {
		name = name[:supplierAccountNameMaxLen]
	}
	return name
}

// buildSupplierClaudeCredentials 组装 Claude OAuth 凭证。
//
// 字段名与格式（expires_in / expires_at 存字符串）照抄管理端重新授权那条路径
// （admin/account_handler.go），不是随手写的：token 刷新与网关取 token 都按这套键名读，
// 少一个字段或换一种类型，号在第一次刷新时才会炸。
func buildSupplierClaudeCredentials(tokenInfo *TokenInfo) map[string]any {
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"token_type":   tokenInfo.TokenType,
		"expires_in":   strconv.FormatInt(tokenInfo.ExpiresIn, 10),
		"expires_at":   strconv.FormatInt(tokenInfo.ExpiresAt, 10),
	}
	if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if strings.TrimSpace(tokenInfo.Scope) != "" {
		creds["scope"] = tokenInfo.Scope
	}
	if strings.TrimSpace(tokenInfo.OrgUUID) != "" {
		creds["org_uuid"] = tokenInfo.OrgUUID
	}
	if strings.TrimSpace(tokenInfo.AccountUUID) != "" {
		creds["account_uuid"] = tokenInfo.AccountUUID
	}
	if strings.TrimSpace(tokenInfo.EmailAddress) != "" {
		creds["email_address"] = tokenInfo.EmailAddress
	}
	return creds
}

// ListAccounts 列出某个供给者名下的账号。
func (s *SupplierOnboardingService) ListAccounts(ctx context.Context, userID int64) ([]SupplierAccountView, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil || userID <= 0 {
		return []SupplierAccountView{}, nil
	}
	ids, err := s.repo.ListAccountIDsByOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []SupplierAccountView{}, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	views := make([]SupplierAccountView, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		views = append(views, *newSupplierAccountView(account))
	}
	return views, nil
}

// getOwnedAccount 读一个账号并确认它属于 userID。
//
// 所有单账号操作的唯一入口。归属校验只写在这里一处，是为了让「忘记校验归属」
// 这种错误没有地方可犯——新增一个操作时无法绕过它拿到 *Account。
func (s *SupplierOnboardingService) getOwnedAccount(ctx context.Context, userID, accountID int64) (*Account, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil || userID <= 0 || accountID <= 0 {
		return nil, ErrSupplierAccountNotFound
	}
	ownerID, err := s.repo.GetAccountOwner(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if ownerID != userID {
		// 包括 ownerID == 0（自营账号）。「这是平台自己的号」同样不能告诉调用方。
		return nil, ErrSupplierAccountNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, ErrSupplierAccountNotFound
	}
	return account, nil
}

// GetAccount 读单个供给账号。
func (s *SupplierOnboardingService) GetAccount(ctx context.Context, userID, accountID int64) (*SupplierAccountView, error) {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	return newSupplierAccountView(account), nil
}

// PauseAccount 供给者主动下线一个号。
//
// 立刻停止接受新的调度。**在途请求不受影响**——它们已经拿到了这个账号，
// 打断它们只会让消费者收到一个莫名其妙的失败。优雅排空与紧急拔线的完整双通道是 #9。
func (s *SupplierOnboardingService) PauseAccount(ctx context.Context, userID, accountID int64) error {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}
	if err := s.accountRepo.SetSchedulable(ctx, account.ID, false); err != nil {
		return err
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		SupplyStateExtraKey: SupplyStateRetired,
	})
}

// ResumeAccount 供给者把下线的号重新挂回来。
//
// 只对 retired 生效，且只把状态改回 active——**不碰 schedulable**。
// 一个 pending_review 的号能被它的主人一键变成可调度，等于观察期形同虚设；
// 而重新入池是平台侧的判断（凭证还有效吗、号还健康吗），不是供给者点一下就成立的事。
// 真正的重新入池由 #9 的观察期流程做。
func (s *SupplierOnboardingService) ResumeAccount(ctx context.Context, userID, accountID int64) error {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}
	if supplyStateOf(account) != SupplyStateRetired {
		return ErrSupplierAccountNotRetired
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		SupplyStateExtraKey: SupplyStatePendingReview,
	})
}

// supplyStateOf 读账号的接入状态，读不到按 pending_review 算。
//
// 保守方向：一个 extra 里没有状态的账号（比如管理员手工建的自营号被写了 owner），
// 当成「还没过观察期」比当成「已入池」安全。
func supplyStateOf(account *Account) string {
	if account == nil || account.Extra == nil {
		return SupplyStatePendingReview
	}
	state, _ := account.Extra[SupplyStateExtraKey].(string)
	if state == "" {
		return SupplyStatePendingReview
	}
	return state
}

// newSupplierAccountView 把内部账号裁成供给者能看的那几个字段。
func newSupplierAccountView(account *Account) *SupplierAccountView {
	if account == nil {
		return nil
	}
	return &SupplierAccountView{
		ID:           account.ID,
		Name:         account.Name,
		Platform:     account.Platform,
		SupplyState:  supplyStateOf(account),
		Status:       account.Status,
		ErrorMessage: account.ErrorMessage,
		Schedulable:  account.Schedulable,
		EmailAddress: account.GetCredential("email_address"),
		LastUsedAt:   account.LastUsedAt,
		CreatedAt:    account.CreatedAt,
	}
}
