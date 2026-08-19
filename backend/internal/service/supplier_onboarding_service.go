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
	"encoding/json"
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

// supplierSettingsReader 读自助接入要用的两组配置：挂哪个分组，观察期多长。
type supplierSettingsReader interface {
	GetSupplyPoolSettings(ctx context.Context) *SupplyPoolSettings
	GetSupplyProbationSettings(ctx context.Context) *SupplyProbationSettings
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
	settings    supplierSettingsReader
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

// probationSettings 读观察期参数；读不到返回 nil。
//
// 刻意不回退到默认值：这份配置在本文件里只用来算给供给者看的 EligibleAt，
// 用一个不是真正生效的窗口算出来的时刻会让他按错误的时间等。算不出就不显示。
func (s *SupplierOnboardingService) probationSettings(ctx context.Context) *SupplyProbationSettings {
	if s == nil || s.settings == nil {
		return nil
	}
	return s.settings.GetSupplyProbationSettings(ctx)
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
	if err := s.rejectDuplicateSubscription(ctx, session.Platform, tokenInfo); err != nil {
		return nil, err
	}

	account := &Account{
		Name:     s.accountName(input.Name, tokenInfo),
		Platform: session.Platform,
		// setup-token：与 scope 一致。类型判错会让 token 刷新走错分支。
		Type:        AccountTypeSetupToken,
		Credentials: buildSupplierClaudeCredentials(tokenInfo),
		Extra: map[string]any{
			SupplyStateExtraKey: SupplyStatePendingReview,
			// 观察窗从建号这一刻开始计时，不等第一次探测。探测只证明「这个号能用」，
			// 观察窗要的是「它在一段时间里一直能用」——那段时间从它挂上来就开始了。
			SupplyProbationSinceExtraKey: time.Now().Format(time.RFC3339),
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

	return newSupplierAccountView(account, s.probationSettings(ctx)), nil
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

// supplierIdentityValues 取出这次授权结果里所有可用的身份键，顺序同 SupplierIdentityKeys。
//
// 返回空 = 上游一个能唯一标识订阅的字段都没给。TokenInfo 里 account / organization
// 两个块在上游响应里都是可选的（oauth_service.go:270-283 逐个判空），所以这不是
// 一个假想的情况。
func supplierIdentityValues(tokenInfo *TokenInfo) map[SupplierIdentityKey]string {
	values := make(map[SupplierIdentityKey]string, len(SupplierIdentityKeys))
	if tokenInfo == nil {
		return values
	}
	if v := strings.TrimSpace(tokenInfo.AccountUUID); v != "" {
		values[SupplierIdentityAccountUUID] = v
	}
	if v := strings.TrimSpace(tokenInfo.EmailAddress); v != "" {
		values[SupplierIdentityEmailAddress] = v
	}
	return values
}

// rejectDuplicateSubscription 在建号之前挡住「同一份订阅挂两次」。
//
// 两条性质，缺一条这个闸门就是纸糊的：
//
//  1. **一个身份键都拿不到时拒绝挂号**，而不是放行。这原本是个 `if uuid != ""` 的
//     乐观分支：上游没吐 uuid 就整个跳过查重，于是同一份订阅可以挂任意多次，每一份
//     都按同一份额度独立计分成——这是整套结算里唯一一个能凭空造钱的口子。代价是
//     偶发的授权要重来一次，比起把钱算错，这个代价不值一提。
//  2. **拿到几个就查几个**，不是"最强的那个查到了就够"。早先挂上来的号可能只记下了
//     邮箱（那次上游没给 uuid），这次给了 uuid——只查 uuid 就会放行同一份订阅。
//     逐个查是 O(键数) 次索引查询，键就两个。
//
// 查询本身出错一律往上抛：查重失败时放行等于关掉闸门，而这个闸门的开关不能建立在
// 「数据库这一刻是否健康」之上。
func (s *SupplierOnboardingService) rejectDuplicateSubscription(ctx context.Context, platform string, tokenInfo *TokenInfo) error {
	values := supplierIdentityValues(tokenInfo)
	if len(values) == 0 {
		slog.Warn("[SupplierOnboarding] upstream returned no identity field, refusing to bind",
			"platform", platform)
		return ErrSupplierAccountIdentityUnavailable
	}

	for _, key := range SupplierIdentityKeys {
		value, ok := values[key]
		if !ok {
			continue
		}
		existingID, err := s.repo.FindAccountIDByUpstreamIdentity(ctx, platform, key, value)
		if err != nil {
			return err
		}
		if existingID > 0 {
			// 不把命中的是哪个键、哪个账号告诉调用方：那会让接入端点变成一个
			// 「这个邮箱在平台上挂过号吗」的探针。日志里留全，响应里不留。
			slog.Info("[SupplierOnboarding] duplicate subscription rejected",
				"platform", platform, "identity_key", string(key), "existing_account_id", existingID)
			return ErrSupplierAccountAlreadyBound
		}
	}
	return nil
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
	// 配置读一次给整批用：GetSupplyProbationSettings 有进程内缓存，但在循环里调
	// 仍然会让同一个列表里的两个号用上不同版本的窗口（缓存正好在中途过期）。
	settings := s.probationSettings(ctx)
	views := make([]SupplierAccountView, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		views = append(views, *newSupplierAccountView(account, settings))
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
	return newSupplierAccountView(account, s.probationSettings(ctx)), nil
}

// PauseAccount 供给者主动下线一个号，走两条通道之一。
//
// 两条通道**共有**的部分是最要紧的那一半：立刻 SetSchedulable(false)。
// 这一下同时切断了新调度和粘性会话的复用（粘性路径会检查 IsSchedulable），
// 所以无论选哪条通道，「不再接新单」都是立刻生效的。
//
// 两条通道**真正的差别**只有两点：终态多快到来，以及还能不能反悔。
//
//   - graceful：进 draining，排空窗内保持这个状态，窗口内可以取消（ResumeAccount
//     会把它放回下线之前的状态，不重走观察期）。窗口到期由后台任务转 retired。
//   - immediate：直接 retired，没有窗口、不能取消，重新挂回来要重走观察期。
//
// 两条通道都**停不掉已经在流的请求**——平台没有连接级 draining 的能力。
// 把这一点写在这里而不是含糊过去：一个以为点了「立即拔出」就能瞬间切断的供给者，
// 比一个知道要等在途请求结束的供给者更容易产生纠纷。
func (s *SupplierOnboardingService) PauseAccount(ctx context.Context, userID, accountID int64, mode string) error {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}
	currentState := supplyStateOf(account)
	if currentState == SupplyStateRetired {
		// 已经是终态。重复点一次不该报错——供给者点两下按钮不是错误。
		return nil
	}

	// 先停调度：无论后面写 extra 成不成功，「不再接新单」这件事已经落地。
	// 顺序反过来的话，中间失败会留下一个写着 retired 却还在接单的号。
	if err := s.accountRepo.SetSchedulable(ctx, account.ID, false); err != nil {
		return err
	}

	window := time.Duration(0)
	if settings := s.probationSettings(ctx); settings != nil {
		window = settings.DrainWindow()
	}
	// 排空窗为 0（配置成 0，或读不到配置）时优雅下线没有意义——一个立刻到期的
	// draining 只是让号在终态之前多绕一轮后台任务。直接走终态。
	if normalizeSupplyPauseMode(mode) != SupplyPauseModeGraceful || window <= 0 {
		return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			SupplyStateExtraKey:       SupplyStateRetired,
			SupplyDrainUntilExtraKey:  "",
			SupplyDrainFromExtraKey:   "",
			SupplyProbePassesExtraKey: 0,
		})
	}

	if currentState == SupplyStateDraining {
		// 已经在排空窗里。不刷新到期时刻——否则反复点「优雅下线」就能把一个号
		// 无限期地停在 draining，那既不是下线也不是在服务。
		return nil
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		SupplyStateExtraKey:      SupplyStateDraining,
		SupplyDrainUntilExtraKey: time.Now().Add(window).Format(time.RFC3339),
		// 记住从哪来，取消下线时原样回退。
		SupplyDrainFromExtraKey: currentState,
	})
}

// normalizeSupplyPauseMode 把请求里的下线通道归一化。
//
// 无法识别的值一律按 graceful：默认给出可反悔的那条路。反过来（默认立即拔出）
// 会让一个拼错参数的调用把号推进不可撤销的终态。
func normalizeSupplyPauseMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), SupplyPauseModeImmediate) {
		return SupplyPauseModeImmediate
	}
	return SupplyPauseModeGraceful
}

// ResumeAccount 供给者把号重新挂回来。两种情形，差别很大。
//
//  1. draining（排空窗内取消下线）：回到进入排空之前的状态。如果那时是 active，
//     连 schedulable 一起恢复——它本来就在池里，是这次下线把它拿下来的，
//     取消下线就该把它原样放回去，不必重走观察期。
//  2. retired（已下线，重新挂回）：回到 pending_review 并**重置观察期**，
//     schedulable 保持 false。号在下线期间发生了什么平台一无所知（订阅可能已经到期、
//     可能已经被上游封），凭一次点击就把它放回付费流量前面，等于观察期形同虚设。
func (s *SupplierOnboardingService) ResumeAccount(ctx context.Context, userID, accountID int64) error {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}

	switch supplyStateOf(account) {
	case SupplyStateDraining:
		restored := supplyExtraString(account, SupplyDrainFromExtraKey)
		if restored != SupplyStateActive && restored != SupplyStatePendingReview {
			// 来路不明（字段被手工动过、或者是老数据）——回到观察期，是两个候选里
			// 保守的那个：最坏结果只是多观察一段时间，而不是把一个没验证过的号放进池子。
			restored = SupplyStatePendingReview
		}
		if restored == SupplyStateActive {
			if err := s.accountRepo.SetSchedulable(ctx, account.ID, true); err != nil {
				return err
			}
		}
		return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			SupplyStateExtraKey:      restored,
			SupplyDrainUntilExtraKey: "",
			SupplyDrainFromExtraKey:  "",
		})

	case SupplyStateRetired:
		return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			SupplyStateExtraKey: SupplyStatePendingReview,
			// 观察期从头计时。
			SupplyProbationSinceExtraKey: time.Now().Format(time.RFC3339),
			SupplyProbePassesExtraKey:    0,
			SupplyProbeAtExtraKey:        "",
			SupplyProbeErrorExtraKey:     "",
		})

	default:
		return ErrSupplierAccountNotRetired
	}
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

// supplyExtraTime 从 extra 里读一个 RFC3339 时刻。第二个返回值区分「没有」与「零值」。
//
// 值来自 JSONB，写进去时是字符串；解析失败一律当作没有——一个格式坏掉的时间戳
// 若被当成零值，会让所有「到期了吗」的判断瞬间全部为真。
func supplyExtraTime(account *Account, key string) (time.Time, bool) {
	if account == nil || account.Extra == nil {
		return time.Time{}, false
	}
	raw, _ := account.Extra[key].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// supplyExtraInt 从 extra 里读一个整数。
//
// 三种分支不是防御性冗余：同一个字段在进程内是 int（刚写完还没回库），
// 从 JSONB 读回来是 float64，而经过某些 JSON 往返会变成 json.Number。
func supplyExtraInt(account *Account, key string) int {
	if account == nil || account.Extra == nil {
		return 0
	}
	switch value := account.Extra[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	default:
		return 0
	}
}

// supplyExtraString 从 extra 里读一个字符串。
func supplyExtraString(account *Account, key string) string {
	if account == nil || account.Extra == nil {
		return ""
	}
	value, _ := account.Extra[key].(string)
	return strings.TrimSpace(value)
}

// newSupplierAccountView 把内部账号裁成供给者能看的那几个字段。
//
// settings 可以为 nil：那时算不出 EligibleAt，前端就只显示「观察中」而不显示进度条。
// 不为此回退到默认观察窗——给出一个由默认值算出来的、与实际生效配置不同的时刻，
// 比不给更糟，供给者会按那个时刻等。
func newSupplierAccountView(account *Account, settings *SupplyProbationSettings) *SupplierAccountView {
	if account == nil {
		return nil
	}
	view := &SupplierAccountView{
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
		ProbePasses:  supplyExtraInt(account, SupplyProbePassesExtraKey),
		ProbeError:   supplyExtraString(account, SupplyProbeErrorExtraKey),
	}
	if since, ok := supplyExtraTime(account, SupplyProbationSinceExtraKey); ok {
		view.ProbationSince = &since
		if settings != nil {
			eligibleAt := since.Add(settings.ObservationWindow())
			view.EligibleAt = &eligibleAt
		}
	}
	if until, ok := supplyExtraTime(account, SupplyDrainUntilExtraKey); ok {
		view.DrainUntil = &until
	}
	return view
}
