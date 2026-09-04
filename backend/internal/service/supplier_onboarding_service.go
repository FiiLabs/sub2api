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
	"net/http"
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
	// supplierDefaultConcurrency 新接入账号的并发上限。
	//
	// 曾经是 3——那个值不是算出来的，只是照抄了 ent schema 的字段默认值。
	// 线上因此出过一次 503：供给池里唯一在接单的号并发上限 3，扛了 98% 的流量，
	// 很快打满并撞上上游限流；与此同时兜底号（并发 20）几乎没被用到。两个号在
	// 同一秒被限流的那一刻，消费者收到 "no available accounts"。
	//
	// 改成 15，与自营账号同档（线上自营号是 20），理由与 supplierDefaultPriority
	// 一样：**供给账号不该因为「是外部的」就被系统性地限制得更死**。一个供给者
	// 的订阅额度和自营账号的额度在上游看来没有区别，凭什么它只能跑三分之一的并发。
	//
	// 上界仍由上游限流兜着：真打太猛，Anthropic 会返回 429，RateLimitService
	// 把这个号临时摘掉——那是一道比我们瞎猜一个并发数更准的闸。
	//
	// 这个常量只影响**新接入**的号。已经在池子里的号要单独改，改动不会追溯。
	supplierDefaultConcurrency = 15
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

// supplierSettingsReader 读自助接入要用的四组配置：挂哪个分组，观察期多长，
// 当前生效的供给者协议（见 supplier_agreement.go），以及接入的数量上限
// （见 setting_supply_onboarding.go）。
type supplierSettingsReader interface {
	GetSupplyPoolSettings(ctx context.Context) *SupplyPoolSettings
	GetSupplyProbationSettings(ctx context.Context) *SupplyProbationSettings
	GetSupplyAgreementSettings(ctx context.Context) *SupplyAgreementSettings
	GetSupplyOnboardingSettings(ctx context.Context) *SupplyOnboardingSettings
}

// supplierAccountStore 是自助接入用到的账号读写子集。
//
// 从 AccountRepository 里挑出来的几个方法。窄接口在这里有实际作用：它让「自助接入
// 能对账号做什么」变成一份可以一眼读完的清单——建、读、改 extra、改可调度性、摘号。
// 改凭证、改分组优先级、改归属都不在里面，供给者的接口够不到它们。
//
// Delete 是后来加进来的，加得很不情愿：它让这份清单多了一件破坏性的事。但解绑
// （DetachAccount）没有它就做不干净——只抹凭证不摘号，会在供给者的列表和管理端
// 留下一行"存在但一定用不了"的僵尸记录。加进来的前提是它只被 DetachAccount 调用，
// 且那条路径必须先过 getOwnedAccount。
type supplierAccountStore interface {
	Create(ctx context.Context, account *Account) error
	GetByID(ctx context.Context, id int64) (*Account, error)
	GetByIDs(ctx context.Context, ids []int64) ([]*Account, error)
	BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
	SetSchedulable(ctx context.Context, id int64, schedulable bool) error
	// ClearError 把账号从错误态放回 active（清 status 与 error_message）。
	//
	// 它**不是**一个凭证方法——上面那句「改凭证不在里面」仍然成立。它是
	// SetSchedulable 的同族：改的是调度层看得见的状态，因而必须走仓储而不是混进
	// 重新授权那条 raw SQL——ClearError 还会发调度变更事件、同步调度快照，
	// 而 SQL 做不到那两件事。分工同 DetachAccount 里 SetSchedulable 与
	// ScrubAccountCredentials 的关系。
	//
	// 唯一的调用方是 CompleteReauth（supplier_reauth.go）。
	ClearError(ctx context.Context, id int64) error
	// Delete 软删账号，同时解掉分组绑定、清掉调度快照、发一次调度变更事件。
	Delete(ctx context.Context, id int64) error
}

// SupplierOnboardingService 编排供给者自助接入。
type SupplierOnboardingService struct {
	repo        SupplierOnboardingRepository
	accountRepo supplierAccountStore
	oauth       supplierClaudeOAuth
	settings    supplierSettingsReader
	// incidents 失效熔断的判据来源。可选，见 SetIncidentGuard。
	incidents supplierIncidentGuard
	// dailyUsageReader 每日共享上限的「今日已用」来源。可选，见 SetDailyUsageReader。
	dailyUsageReader supplierDailyUsageReader
	// prober 接入完成时的同步探测。可选，见 SetProber。
	prober supplierOnboardingProber

	// relayProbeClient 中转提交时探测用的 HTTP 客户端。nil = 默认（15s 超时）。
	// 单独一个字段是给测试注桩用的——探测是真实网络调用，单测不该出网。
	relayProbeClient *http.Client
}

// supplierOnboardingProber 接入完成时当场探一次。
//
// 与 supplierLifecycleProber 同形且刻意同形——两边探的是同一件事、用的是同一个
// 实现（*AccountTestService）。不共用一个接口名，是因为「本服务需要什么」应该由
// 本服务自己声明；共用会让删掉一边的依赖时，另一边静默地跟着变。
type supplierOnboardingProber interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

// supplierIncidentGuard 是「这个人最近坏掉的号是不是太多了」这一个判断。
//
// 只有一个方法，且方法本身吃 *SupplyOnboardingSettings：窗口与阈值都在配置里，
// 而配置本文件已经读过一次（requireCapacity 里那个 limits）。把它传过去而不是
// 让事件服务自己再读一遍，是为了让同一次接入判断中的两道闸看的是**同一份配置**——
// 两次读之间隔着一个 60 秒的缓存，运营刚改完设置的那一分钟里它们可以是不同的。
type supplierIncidentGuard interface {
	GuardOnboarding(ctx context.Context, userID int64, limits *SupplyOnboardingSettings) error
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

// SetIncidentGuard 注入失效熔断的判据来源。为 nil 时这道闸整个不存在。
//
// setter 而不是构造参数，理由与 SupplierLifecycleService.SetIncidentSweeper 一样：
// 接入服务在没有事件台账的部署里必须照常工作，而构造函数里那四个依赖是它跑起来的
// 必要条件。另有一条本处独有的：反过来注入会成环——事件服务读的是账号表，
// 接入服务写的也是账号表，把它塞进构造签名会让 wire 的装配顺序变成一件要想的事。
// SetProber 注入接入完成时的同步探测能力。为 nil 时这一步整个不存在，
// 新号照旧留在观察期等后台任务来探——也就是这个功能上线之前的行为。
//
// setter 而不是构造参数，理由同 SetIncidentGuard：接入在没有探测能力的部署里
// 必须照常工作。另有一条本处独有的：*AccountTestService 是一个吃十几个依赖的
// 大服务，把它焊进构造签名会让每一个接入相关的测试都得先造出它。
//
// 显式判空再赋值：一个 nil 的 *AccountTestService 装进接口变量后**不是** nil 接口，
// 后面的 `s.prober == nil` 就永远为假，探测会打到一个空指针上。
// 这个坑在 NewSupplierLifecycleService 里已经踩过一次，注释留在那里。
func (s *SupplierOnboardingService) SetProber(prober *AccountTestService) {
	if s == nil || prober == nil {
		return
	}
	s.prober = prober
}

func (s *SupplierOnboardingService) SetIncidentGuard(guard *SupplierIncidentService) {
	if s == nil || guard == nil {
		return
	}
	s.incidents = guard
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
//
// clientIP 是发起方的出口地址，只用来判每 IP 上限。取不到（空串）时那道闸跳过，
// 理由见 requireCapacity。
func (s *SupplierOnboardingService) StartOAuth(ctx context.Context, userID int64, clientIP string) (*SupplierAuthorization, error) {
	if s == nil || s.repo == nil || s.oauth == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if userID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}
	if _, ok := s.supplyGroupID(ctx); !ok {
		return nil, ErrSupplierOnboardingDisabled
	}
	// 协议门禁在这里纯粹是为了体验：真正不可绕过的那一道在 CompleteOAuth。
	// 不拦的话，供给者会跑完一整遍上游授权之后才被告知"你还没同意协议"，
	// 而那时他已经在 Anthropic 那边生成了一个 setup token。
	if err := s.requireAgreement(ctx, userID); err != nil {
		return nil, err
	}
	// 数量上限在这里同样只是体验（真正的那道也在 CompleteOAuth）。差别在于它比
	// 协议门禁更值得前置：一个已经挂满的人走完整遍授权，末了被拒，手上还多出一个
	// 他并不需要、也无从撤销的上游 setup token。
	if err := s.requireCapacity(ctx, userID, clientIP); err != nil {
		return nil, err
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
	// ClientIP 兑换方的出口地址。判每 IP 上限，并作为新账号的接入来源记下来。
	//
	// 取的是**兑换这一刻**的地址，而不是 StartOAuth 时那个（会话表里根本没存它）。
	// 两者可以不同——授权链接在手机上打开、code 贴回电脑是很正常的操作——而对
	// 「这个号是从哪挂上来的」这个问题，账号真正建出来的那一刻才是答案。
	ClientIP string
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
	// 这是不可绕过的那一道协议门禁：下面几行之后，一个握着别人上游凭证的 accounts 行
	// 就建出来了。**必须排在领会话之前**——领取是一次性消费，在它之后被拒的人会丢掉
	// 手上那个授权码，得从头再授权一遍，而他被拒的原因（没同意协议）本来在第一行就
	// 能判出来。
	if err := s.requireAgreement(ctx, input.UserID); err != nil {
		return nil, err
	}
	// 数量上限，**同样必须排在领会话之前**，理由与协议门禁一字不差：领取是一次性
	// 消费，在它之后被拒的人会丢掉手上那个授权码，而"你已经挂满了"这件事在第一行
	// 就能判出来。
	//
	// 这是不可绕过的那一道——StartOAuth 那道是体验。两道之间隔着一整段人类操作
	// 时长（最长 15 分钟的会话有效期），期间他可能又挂上了一个号，也可能运营刚把
	// 上限调低了。只有这一道是在建号之前紧挨着建号跑的。
	if err := s.requireCapacity(ctx, input.UserID, input.ClientIP); err != nil {
		return nil, err
	}

	// 原子领取。会话在这一刻就被标成已消费，即使后面的兑换失败也不退回——
	// 一个授权码本来就只能换一次，失败了就该重新走一遍授权，而不是拿同一个 code 重试。
	// accountID 传 0：这是一条**接入**会话。一条为某个已有账号发起的重新授权会话
	// 在这里领不到（见 ClaimSession 的 IS NOT DISTINCT FROM），于是「用重新授权的
	// 授权码去建一个新号」这条路在 SQL 层就断了。
	session, err := s.repo.ClaimSession(ctx, strings.TrimSpace(input.SessionID), input.UserID, 0)
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
	view, err := s.finalizeSupplyAccount(ctx, account, input.UserID, input.ClientIP, groupID)
	if err != nil {
		return nil, err
	}
	// 当场探一次。这一步之前，供给者点完授权要等 20~30 分钟才看得到「已上线」——
	// 那段时间不是在观察他的号，是在等后台任务的两次轮询。见 probeOnAttach。
	// 探测发现这个订阅没有 Fable 额度会返回错误，此时刚建的号已被清掉。
	return s.probeOnAttach(ctx, account, view, input.UserID)
}

// probeOnAttach 在账号刚挂上来时同步探一次，并按既有规则决定要不要立刻入池。
//
// # 为什么要有这一步
//
// 在它之前，一个 OAuth 接入的号从「授权完成」到「开始接单」要等 20~30 分钟，
// 而这段时间**没有在观察任何东西**：它由「生命周期任务 5 分钟轮询一次」加上
// 「两次探测之间隔 15 分钟」凑出来，与这个号好不好毫无关系。供给者那边看到的是
// 一个「接入成功了但什么也没发生」的页面，而这是他与平台的第一次交互。
//
// 中转接入（SubmitRelay）一直是当场探的，理由写在那里：「不验的话，一个抄错一位的
// key 要等观察期第一次探测才暴露，而供给者早已关掉页面」。同一条理由对 OAuth 成立
// ——token 交换只证明凭证能换出 token，不证明这个订阅还能跑推理（额度用尽、
// 被上游风控、订阅到期都能换出 token 却跑不了）。两条路径此前的不一致没有道理。
//
// # 刻意不给 OAuth 开特例
//
// 这次探测**作为第一次探测参与既有状态机**：写 probe_passes=1 / probe_at，
// 然后用与观察期任务**同一个** supplyProbationEligible 判断够不够格入池。
// 于是「新号多久能上线」完全由观察期配置决定，而不是由「走了哪条接入路径」决定。
// 配成 required_successes=1 + min_observation_minutes=0 就是立刻上线；
// 配回 2 次 / 60 分钟，这次探测就只是提前完成了第一次，行为回到从前。
//
// # 探测失败为什么不回滚接入
//
// 账号已经建好、归属已经写上、分组已经绑上——这三件事都不该因为一次探测失败而
// 撤销，理由与 finalizeSupplyAccount 里「逐步推进不回滚」一字不差。失败留下的是
// 一个 pending_review + probe_error 的号：供给者在仪表盘上看得到失败原因
// （supply.accounts.probeError 那行红字），观察期任务会继续按间隔重试，
// 而认证类失败还会被抬成错误态并触发通知（见 probeOnce）。
//
// 返回值永远非 nil：拿不到更新后的视图就退回接入时那一份，
// 「接入成功」这件事不该因为一次读失败而变成失败。
func (s *SupplierOnboardingService) probeOnAttach(ctx context.Context, account *Account, fallback *SupplierAccountView, ownerUserID int64) (*SupplierAccountView, error) {
	if s == nil || s.prober == nil || account == nil {
		return fallback, nil
	}

	settings := s.probationSettings(ctx)

	probeCtx, cancel := context.WithTimeout(ctx, supplierLifecycleProbeTimeout)
	result, err := s.prober.RunTestBackground(probeCtx, account.ID, supplyResolveProbeModel(settings))
	cancel()

	now := time.Now()
	updates := map[string]any{
		SupplyProbeAtExtraKey: now.Format(time.RFC3339),
	}

	if err != nil || result == nil || result.Status != "success" {
		message := supplyProbeErrorMessage(err, result)

		// 无额度（免费号 / 未订阅付费方案）：当场拒绝接入。这种号进来接不了单，
		// 只会占着观察期、被后台每 15 分钟无谓地探一次。把刚建的号清掉（它没有任何
		// 价值可留），返回一个明确错误让前端提示「请先订阅再重试」。
		//
		// best-effort 清号：清不掉也照样返回无额度错误——留一个还在列表里的死号，
		// 好过让供给者以为接入成功了。日志里记下这次不干净，供给者重试或解绑即好。
		if supplyProbeNoQuota(message) {
			if delErr := s.detachOwnedAccount(ctx, ownerUserID, account.ID); delErr != nil {
				slog.Error("[SupplierOnboarding] no-fable-quota account rejected but cleanup failed",
					"account_id", account.ID, "user_id", ownerUserID, "error", delErr)
			} else {
				slog.Info("[SupplierOnboarding] rejected attach: account has no Fable quota",
					"account_id", account.ID, "user_id", ownerUserID)
			}
			return nil, ErrSupplierAccountNoFableQuota
		}

		updates[SupplyProbePassesExtraKey] = 0
		updates[SupplyProbeErrorExtraKey] = message
		if updErr := s.accountRepo.UpdateExtra(ctx, account.ID, updates); updErr != nil {
			slog.Warn("[SupplierOnboarding] failed to record attach probe failure",
				"account_id", account.ID, "error", updErr)
		}
		// 刻意不在这里调 SetError：那条判定（401 抬成错误态）住在 probeOnce 里，
		// 和事件/通知是一整套。接入这一刻多写一个状态只会让两处判据各自演化。
		slog.Info("[SupplierOnboarding] attach probe failed, account stays in review",
			"account_id", account.ID, "reason", message)
		return s.refreshView(ctx, account, fallback), nil
	}

	updates[SupplyProbePassesExtraKey] = 1
	updates[SupplyProbeErrorExtraKey] = ""
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("[SupplierOnboarding] failed to record attach probe success",
			"account_id", account.ID, "error", err)
		return s.refreshView(ctx, account, fallback), nil
	}

	if !supplyProbationEligible(account, settings, 1, now) {
		// 够不上就留在观察期——这是配置说了算的正常结果，不是故障。
		return s.refreshView(ctx, account, fallback), nil
	}
	if err := supplyPromoteToActive(ctx, s.accountRepo, account, true); err != nil {
		slog.Warn("[SupplierOnboarding] attach probe passed but promotion failed",
			"account_id", account.ID, "error", err)
	}
	return s.refreshView(ctx, account, fallback), nil
}

// detachOwnedAccount 是 DetachAccount 里的清号动作（关调度 → 抹凭证 → 删行），
// 抽出来给接入拒绝复用。不走 DetachAccount 本体是因为那个入口要先 getOwnedAccount
// 做归属校验，而这里的号是本次 CompleteOAuth 刚建、归属确定的，再查一遍多余。
func (s *SupplierOnboardingService) detachOwnedAccount(ctx context.Context, userID, accountID int64) error {
	if err := s.accountRepo.SetSchedulable(ctx, accountID, false); err != nil {
		return err
	}
	if err := s.repo.ScrubAccountCredentials(ctx, accountID, userID); err != nil {
		return err
	}
	return s.accountRepo.Delete(ctx, accountID)
}

// refreshView 重读账号并重建视图；读不到就退回 fallback。
//
// 重读而不是就地改 fallback：上面几步分别写在 UpdateExtra 与 SetSchedulable 两处，
// 手工拼一份视图迟早与库里不一致——而这份视图正是供给者看到的第一屏。
func (s *SupplierOnboardingService) refreshView(ctx context.Context, account *Account, fallback *SupplierAccountView) *SupplierAccountView {
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || updated == nil {
		return fallback
	}
	return newSupplierAccountView(updated, s.probationSettings(ctx))
}

// finalizeSupplyAccount 把一个准备好的账号真正挂进供给池：建行 → 写归属 →
// 记来源 → 绑分组。OAuth 与中转（M7）两条接入路径共用——这段的失败语义
// 是逐步推进不回滚的（理由见各步注释），两条路径各抄一份迟早漂成两套语义。
func (s *SupplierOnboardingService) finalizeSupplyAccount(
	ctx context.Context, account *Account, userID int64, clientIP string, groupID int64,
) (*SupplierAccountView, error) {
	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, fmt.Errorf("create supply account: %w", err)
	}

	if err := s.repo.SetAccountOwner(ctx, account.ID, userID); err != nil {
		// 归属没写上，这个号就是个无主的孤儿。它现在 schedulable=false 且未绑分组，
		// 服务不了任何请求，但留着只会让管理员在账号列表里看到一条来历不明的记录。
		// 补偿失败也只能记日志——此时能做的都做了，剩下的是运维的事。
		slog.Error("[SupplierOnboarding] failed to set account owner, orphan account left behind",
			"account_id", account.ID, "user_id", userID, "error", err)
		return nil, fmt.Errorf("set supply account owner: %w", err)
	}

	// 记接入来源。放在写归属之后：这一行说的是「这个**属于某人的**号从哪来」，
	// 归属没写上时它没有意义。
	//
	// 失败不中断接入，只记日志。这不是随手兜底——号已经建出来、已经有主了，此刻
	// 返回错误也不会撤销这两件事，只会让供给者看到一个"失败了"、实际却挂上了的号。
	// 代价是每 IP 那道闸少数了这一个，日志里有据可查；收益是不会有半失败的接入。
	if err := s.repo.RecordAccountOrigin(ctx, account.ID, userID, strings.TrimSpace(clientIP)); err != nil {
		slog.Error("[SupplierOnboarding] failed to record account origin, per-IP limit will undercount",
			"account_id", account.ID, "user_id", userID, "error", err)
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

// onboardingSettings 读接入数量上限；读不到返回默认值（每人 5 个、每 IP 不限）。
//
// 与 probationSettings 那个 nil 语义刻意相反：观察期参数算不出来就不显示，那是个
// 展示问题；这里算不出来就没有闸，那是个准入问题。所以这个 getter 永远返回一份
// 可用的配置——settings 服务缺失（依赖没接上）也一样，那种情况下让上限静默消失，
// 会让一个装配错误变成一个安全洞。
func (s *SupplierOnboardingService) onboardingSettings(ctx context.Context) *SupplyOnboardingSettings {
	if s == nil || s.settings == nil {
		return DefaultSupplyOnboardingSettings()
	}
	settings := s.settings.GetSupplyOnboardingSettings(ctx)
	if settings == nil {
		return DefaultSupplyOnboardingSettings()
	}
	return settings
}

// requireCapacity 判断「这个人、从这个网络，还能不能再挂一个号」。
//
// 两道闸各挡一件事，说明见 setting_supply_onboarding.go 头部。顺序是先人后网络：
// 前者的拒绝理由他自己能纠正（解绑一个旧号），后者不能，先说能纠正的那个。
//
// 数出来的都是**当下还活着**的号，不是历史累计——解绑一个就腾出一个位置。
//
// # 为什么空 IP 是跳过而不是拒绝
//
// 拿不到客户端 IP 的成因是部署侧的（反向代理没配 X-Forwarded-For、trusted proxies
// 没设对），不是用户侧的。在那种部署里拒绝所有人，等于让一个配置疏忽变成全站接入
// 中断；而放行的代价只是这道本来就默认关着的闸暂时不生效。真要堵住这个口子，
// 该做的是把代理配对，不是在这里立一道谁也过不去的门。
func (s *SupplierOnboardingService) requireCapacity(ctx context.Context, userID int64, clientIP string) error {
	if s == nil || s.repo == nil {
		return ErrSupplierOnboardingDisabled
	}
	limits := s.onboardingSettings(ctx)

	if limits.userCapEnabled() {
		owned, err := s.repo.CountAccountsByOwner(ctx, userID)
		if err != nil {
			return fmt.Errorf("count owned supply accounts: %w", err)
		}
		if limits.userCapReached(owned) {
			return ErrSupplierAccountLimitReached
		}
	}

	clientIP = strings.TrimSpace(clientIP)
	if limits.ipCapEnabled() && clientIP != "" {
		fromIP, err := s.repo.CountAccountsByOriginIP(ctx, clientIP)
		if err != nil {
			return fmt.Errorf("count supply accounts by origin ip: %w", err)
		}
		if limits.ipCapReached(fromIP) {
			return ErrSupplierNetworkLimitReached
		}
	}

	// 第三道闸：最近坏掉的号太多。排在最后是因为它是三道里最贵的一次查询
	// （数的是历史事件而不是当下的行数），而前两道能挡住的人不必走到这里。
	// 它默认是关的，那时这一步连一次查询都不会发（见 GuardOnboarding）。
	if s.incidents != nil {
		if err := s.incidents.GuardOnboarding(ctx, userID, limits); err != nil {
			return err
		}
	}
	return nil
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

// CountAccounts 数某个供给者名下还挂着几个号。
//
// 与 ListAccounts 分开而不是让前端拿列表的长度：这个数唯一的消费者是
// 「这个人是不是供给者」这个判断（前端据此自动进共享模式），为它拉一整份
// 账号列表——每条都要回一次账号仓储、还要解 jsonb 里的观察期状态——
// 是为一个布尔付一次全表往返。
//
// 读失败回 0 而不是报错：判断失败的后果只是默认落到使用模式，
// 那是个安全的答案；让整个状态接口失败会连带把侧栏和接入入口一起打没。
func (s *SupplierOnboardingService) CountAccounts(ctx context.Context, userID int64) int {
	if s == nil || s.repo == nil || userID <= 0 {
		return 0
	}
	owned, err := s.repo.CountAccountsByOwner(ctx, userID)
	if err != nil {
		slog.Warn("[SupplierOnboarding] failed to count owned supply accounts",
			"user_id", userID, "error", err)
		return 0
	}
	return owned
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
	accountByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		accountByID[account.ID] = account
		views = append(views, *newSupplierAccountView(account, settings))
	}
	// 补「今日已用 / 是否触顶」。best-effort：查不到就只显示上限本身。
	s.applyDailyCapUsage(ctx, views, accountByID)
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

// DetachAccount 供给者彻底撤回一个号：平台交还控制权，不再持有他的凭证。
//
// # 为什么必须有这个操作
//
// 在它之前，供给者能做的最强动作是「下线」（PauseAccount）——号不再接单，但那份
// refresh token 仍然原封不动地躺在 accounts.credentials 里，平台随时能刷出新的
// access token。也就是说：供给者交出授权之后就再也收不回来，只能请求平台别用。
// 一个建立在「请相信我们不会用」之上的双边市场是不成立的，所以这条路径不是功能，
// 是这套东西能不能对外讲的前提。
//
// # 上游没有可调用的撤销端点
//
// 抹掉凭证就是这里能给出的全部保证——Anthropic 没有公开 OAuth 撤销端点
// （token 端点是 platform.claude.com/v1/oauth/token，没有对应的 revoke）。
// 刻意**不**去 POST 一个猜出来的地址：那会让每次解绑都打一次必然失败的请求，
// 并在日志里留下"撤销失败"的噪音，营造出一种平台尝试过远端撤销的假象。
// 真正的远端撤销只有供给者自己在 claude.ai 的账号设置里能做，前端会告诉他这一点。
//
// 于是这条路径的承诺被压到一句可验证的话：**平台这边不再有这份凭证**。
//
// # 三步的顺序
//
//  1. SetSchedulable(false)——同 PauseAccount，先把「不再接新单」落地。走仓储而不是
//     混进下面那条 SQL：它还会清调度快照、发调度变更事件，那两件事这条 raw SQL 做不到。
//  2. ScrubAccountCredentials——不可逆的一步。凭证在这一刻消失，同时行上留下解绑时刻。
//  3. Delete——软删。摘掉分组绑定、从供给者与管理端的列表里消失。
//
// 2 在 3 之前是必须的：软删之后那条 UPDATE 的 `deleted_at IS NULL` 就再也匹配不上，
// 凭证会永远留在一行没人再看的记录里。反过来，2 成功而 3 失败是可以接受的失败态——
// 号已经停了、凭证已经没了，剩下的只是一行界面上还看得见的空壳，重试一次即可。
//
// # 不碰的两样东西
//
//   - 钱包：解绑不结算、不清零。已经攒下的余额是平台欠他的债，与他还供不供货无关
//     （同「注销用户仍可能是债主」，见 docs/two-sided-market.md §3.6）。
//   - owner_user_id：留在软删的行上。它是「这个号当时是谁的」的唯一记录，
//     出账目纠纷时要靠它对账。
func (s *SupplierOnboardingService) DetachAccount(ctx context.Context, userID, accountID int64) error {
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}

	if err := s.accountRepo.SetSchedulable(ctx, account.ID, false); err != nil {
		return err
	}
	if err := s.repo.ScrubAccountCredentials(ctx, account.ID, userID); err != nil {
		return err
	}
	if err := s.accountRepo.Delete(ctx, account.ID); err != nil {
		// 凭证已经抹掉了，供给者要的那件事已经成立。把这一步的失败记下来但仍然
		// 报错给调用方：他会看到一个还在列表里的号，重试一次就干净了。
		slog.Error("[SupplierOnboarding] credentials scrubbed but account row not removed",
			"account_id", account.ID, "user_id", userID, "error", err)
		return err
	}

	slog.Info("[SupplierOnboarding] supply account detached",
		"account_id", account.ID, "user_id", userID, "supply_state", supplyStateOf(account))
	return nil
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
		NeedsReauth:  supplyNeedsReauth(account),
	}
	if probedAt, ok := supplyExtraTime(account, SupplyProbeAtExtraKey); ok {
		view.ProbeAt = &probedAt
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
	// 上限本身是纯读 extra，不需要 I/O，所以在这里填。今日**已用**要查
	// usage_logs，由 applyDailyCapUsage 在批量取数之后单独补——分开是为了
	// 这个构造函数保持无副作用，也为了「用量拿不到」时上限照样能显示。
	view.DailyCostLimitUSD = account.GetSupplyDailyCostLimit()
	view.DailyTokenLimit = account.GetSupplyDailyTokenLimit()
	if account.HasSupplyDailyCap() {
		resetAt := supplyDailyWindowStart().Add(24 * time.Hour)
		view.DailyCapResetAt = &resetAt
	}
	return view
}
