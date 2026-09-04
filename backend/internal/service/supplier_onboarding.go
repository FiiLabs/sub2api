// APEXONE-EXT: 双边市场——供给者自助接入的领域类型与仓储接口。
//
// 供给者用自己的 Claude 订阅走一遍 OAuth，把授权结果落成一个 accounts 行，
// `owner_user_id` 指向他自己。此后这个账号产出的用量按分成入他的赚取钱包
// （见 supplier_credit.go）。
//
// 本文件只放类型与接口；SQL 实现在 internal/repository/supplier_onboarding_repo.go，
// 流程在 supplier_onboarding_service.go。
package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrSupplierOnboardingDisabled 供给池未配置——没有可挂靠的分组，接入无从谈起。
	ErrSupplierOnboardingDisabled = infraerrors.BadRequest(
		"SUPPLIER_ONBOARDING_DISABLED", "supplier onboarding is not enabled")
	// ErrSupplierOAuthSessionInvalid 会话不存在、已过期、已被兑换，或不属于当前用户。
	//
	// 四种情况刻意合成一个错误：区分它们等于告诉调用方「这个 session_id 存在但不是你的」，
	// 那是一个可以用来枚举他人会话的信息泄漏面。
	ErrSupplierOAuthSessionInvalid = infraerrors.BadRequest(
		"SUPPLIER_OAUTH_SESSION_INVALID", "authorization session is invalid or expired")
	// ErrSupplierAccountAlreadyBound 同一个上游账号已经挂在平台上了。
	ErrSupplierAccountAlreadyBound = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_ALREADY_BOUND", "this upstream account is already connected")
	// ErrSupplierAccountIdentityUnavailable 上游没吐出任何能唯一标识这个订阅的字段。
	//
	// 这是一个**刻意的拒绝**，不是故障兜底。没有身份键就查不了重，查不了重就挡不住
	// 「同一份订阅挂两次、按同一份额度算两份分成」——那是这套系统里唯一一个能凭空
	// 造钱的口子。放行一个查不了重的号，比让供给者重走一遍授权贵得多。
	ErrSupplierAccountIdentityUnavailable = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_IDENTITY_UNAVAILABLE",
		"upstream did not return an identifier for this subscription; please retry the authorization")
	// ErrSupplierAccountNotFound 账号不存在，或不属于当前供给者。
	//
	// 同样合成：「存在但不是你的」和「不存在」对调用方必须是同一个回答，
	// 否则账号 id 就成了一个可枚举的探针。
	ErrSupplierAccountNotFound = infraerrors.NotFound(
		"SUPPLIER_ACCOUNT_NOT_FOUND", "supply account not found")
	// ErrSupplierOAuthTooManyPending 同一个人堆积了太多未完成的授权会话。
	ErrSupplierOAuthTooManyPending = infraerrors.BadRequest(
		"SUPPLIER_OAUTH_TOO_MANY_PENDING", "too many pending authorization sessions")
	// ErrSupplierAccountNoFableQuota 接入时探测发现这个订阅没有 Fable 额度
	//（免费号或未订阅付费方案），当场拒绝：这种号进来也接不了单，只会占着观察期、
	// 被后台每 15 分钟无谓地探一次。前端把它翻成「请先订阅再重试」。
	ErrSupplierAccountNoFableQuota = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_NO_FABLE_QUOTA",
		"this account has no Fable quota; subscribe to a paid Claude plan and retry")
	// ErrSupplierAccountNotRetired 只有已下线或正在排空的账号能被重新挂回来。
	//
	// 这个错误不合并进 NotFound：调用方已经证明了自己是这个账号的主人（能读到它），
	// 所以「它现在不是下线状态」不是泄漏，而是他需要知道的、能自己纠正的信息。
	ErrSupplierAccountNotRetired = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_NOT_RETIRED", "only a retired or draining supply account can be resumed")

	// ErrSupplierReauthIdentityMismatch 这次授权来自另一份订阅，不是这个号原本那份。
	//
	// 与上面那几个刻意合并的错误**相反：这一条要说明白**。调用方已经证明了自己是
	// 这个号的主人（他读得到它），所以这里不存在信息泄漏；而他此刻做错的事——
	// 用另一个 Anthropic 账号点了授权——只有告诉他才纠正得了。含糊成一句
	// 「授权失败」，他会以为是平台坏了，然后回去走解绑重挂，而那正是这条路径要
	// 消灭的动作（不可逆、换新 id、观察期重跑、每日上限丢失）。
	ErrSupplierReauthIdentityMismatch = infraerrors.BadRequest(
		"SUPPLIER_REAUTH_IDENTITY_MISMATCH",
		"this authorization is for a different subscription than the one on this account")
	// ErrSupplierReauthUnsupported 这个号走不了 OAuth 重新授权。
	//
	// 两种成因：中转（API key）账号根本没有 OAuth 身份——它的补救是重新提交 key，
	// 是另一件事；以及号上一个可锚定的身份键都没有——那时无法判定「换进来的是不是
	// 同一份订阅」，而放行一个判不了的重绑等于把账号 id 变成任意订阅的壳子。
	ErrSupplierReauthUnsupported = infraerrors.BadRequest(
		"SUPPLIER_REAUTH_UNSUPPORTED", "this supply account cannot be re-authorized")
	// ErrSupplierAccountRetired 已经下线的号要先挂回来再谈重新授权。
	//
	// 不合并进 NotFound，理由同 ErrSupplierAccountNotRetired：他是主人，
	// 「你自己把它下线了」是他能纠正的信息。对应的动作是「重新挂回」，
	// 而不是在同一个位置上再放一个长得也像「修好它」的按钮。
	ErrSupplierAccountRetired = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_RETIRED", "resume this supply account before re-authorizing it")
)

// 供给账号的接入状态，存在 accounts.extra 里。
//
// 刻意不新建状态机字段：入池后账号直接挂现有的可调度性状态机
// （schedulable / rate_limited / temp_unschedulable / ...），这里的状态只覆盖
// 「入池之前」那一段——上游状态机管不到、也不该被污染的一段。
const (
	// SupplyStateExtraKey accounts.extra 里存接入状态的键。
	SupplyStateExtraKey = "apexone_supply_state"
	// SupplyStatePendingReview 刚授权完成，等待观察期跑完。账号此时 schedulable=false。
	SupplyStatePendingReview = "pending_review"
	// SupplyStateActive 观察期通过，已入池服务真实流量。
	SupplyStateActive = "active"
	// SupplyStateDraining 优雅下线的排空窗内：已经停止接新单，等在途请求自己结束。
	//
	// 这个状态**不是**一个硬排空——平台没有能力打断已经在流的请求（连接级 draining
	// 是后续的事）。它是一段礼貌等待时间，也是供给者反悔的窗口：窗口内取消下线，
	// 账号回到下线之前的那个状态，不必重走观察期。
	SupplyStateDraining = "draining"
	// SupplyStateRetired 已下线（终态）。供给者点「立即拔出」或排空窗跑完都会到这里。
	SupplyStateRetired = "retired"

	// SupplyProbationSinceExtraKey 进入观察期的时刻（RFC3339）。
	// 接入时写一次，从 retired 重新挂回时再写一次——观察窗从「这一次」算起。
	SupplyProbationSinceExtraKey = "apexone_supply_probation_since"
	// SupplyProbePassesExtraKey 连续探测成功次数。失败一次清零。
	SupplyProbePassesExtraKey = "apexone_supply_probe_passes"
	// SupplyProbeAtExtraKey 上次探测时刻（RFC3339），用来按间隔节流。
	SupplyProbeAtExtraKey = "apexone_supply_probe_at"
	// SupplyProbeErrorExtraKey 上次探测的失败原因。
	//
	// 对供给者可见：观察期迟迟不过去时，他需要知道是「还在等」还是「你的号连不上」，
	// 后者只有他自己能修（重新授权）。
	SupplyProbeErrorExtraKey = "apexone_supply_probe_error"
	// SupplyDrainUntilExtraKey 排空窗到期时刻（RFC3339）。
	SupplyDrainUntilExtraKey = "apexone_supply_drain_until"
	// SupplyDrainFromExtraKey 进入排空之前的状态，用于取消下线时原样回退。
	SupplyDrainFromExtraKey = "apexone_supply_drain_from"
	// SupplyDetachedAtExtraKey 解绑时刻（RFC3339）。
	//
	// 写在已经被软删的行上，唯一的读者是人：出纠纷时要能回答「平台是什么时候
	// 不再持有这份凭证的」。凭证本身那时已经被抹掉，这个时间戳是仅存的证据。
	SupplyDetachedAtExtraKey = "apexone_supply_detached_at"

	// SupplyDailyCostLimitExtraKey 供给者自设的每日金额上限（美元/天），按**官方牌价**计。
	// 0 或缺失 = 不限。
	//
	// 与上面那些键的区别：它们是平台写、供给者只读的生命周期状态；这一个是**供给者写**的
	// 意愿表达。放在同一个前缀下，是因为「谁能写」由接口层管，而 extra 里同族的键
	// 放在一起，下一个读这张表的人才不必猜 apexone_supply_ 是不是还有别处也在用。
	SupplyDailyCostLimitExtraKey = "apexone_supply_daily_cost_limit"
	// SupplyDailyTokenLimitExtraKey 供给者自设的每日 token 上限。0 或缺失 = 不限。
	SupplyDailyTokenLimitExtraKey = "apexone_supply_daily_token_limit"
)

// 下线的两个通道。
const (
	// SupplyPauseModeGraceful 优雅下线：立刻停止接新单，排空窗内保持 draining，
	// 窗口到期转终态。窗口内可以取消。
	SupplyPauseModeGraceful = "graceful"
	// SupplyPauseModeImmediate 立即拔出：直接进终态，没有窗口、不能取消。
	//
	// 与优雅下线的真实差别只有「终态来得多快」和「还能不能反悔」——两条通道都
	// 停不掉已经在流的请求。把这一点说清楚比造一个「紧急停止」的假象重要：
	// 供给者以为点了就能立刻切断，实际没有，那是比没有这个按钮更糟的事。
	SupplyPauseModeImmediate = "immediate"
)

// SupplierOAuthSession 是一次待兑换的授权会话。
type SupplierOAuthSession struct {
	SessionID    string
	UserID       int64
	Platform     string
	State        string
	CodeVerifier string
	Scope        string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	// AccountID 这条会话是为哪个已有账号发起的。
	//
	// 0 = 接入会话，兑换出一个**新**号；非 0 = 就地重新授权那**一个**号
	// （见 supplier_reauth.go）。库里存的是可空列，0 与 NULL 的转换只在仓储那一层。
	//
	// 为什么必须记在服务端而不是兑换时由调用方带上：那样一条为账号 A 发起的授权
	// 就能被兑换到账号 B 上。两者同属一人，所以不是跨租户的洞，但「这次授权是为哪个
	// 号发起的」是一个服务端已经知道的事实，没有理由再问一遍调用方。
	AccountID int64
}

// SupplierAccountView 是供给者能看见的账号视图。
//
// 刻意是一个独立的窄类型而不是直接吐 *Account：Account 上挂着凭证、代理、模型映射、
// 调度内部字段——那些一律不该出现在供给者的响应里。少数几个字段手工搬过去，
// 是为了让「多暴露一个字段」必须是一次显式的代码改动，而不是加字段时的默认后果。
type SupplierAccountView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// SupplyState 接入状态（pending_review / active / retired）。
	SupplyState string `json:"supply_state"`
	// Status 上游账号健康状态（active / error / ...），凭证失效时会变。
	Status string `json:"status"`
	// ErrorMessage 仅在 Status 非正常时有值，供给者需要它才知道要重新授权。
	ErrorMessage string `json:"error_message,omitempty"`
	// Schedulable 是否正在接受调度。
	Schedulable bool `json:"schedulable"`
	// EmailAddress 上游账号邮箱，供给者用来分辨自己挂了哪几个号。
	EmailAddress string     `json:"email_address,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`

	// ---- 观察期 / 排空。只在相关状态下有值。----

	// ProbationSince 本轮观察期起点。
	ProbationSince *time.Time `json:"probation_since,omitempty"`
	// EligibleAt 观察窗最早何时满足（= ProbationSince + 观察窗）。
	//
	// 是一个「不早于」而不是「就在这一刻入池」：入池还要求连续探测成功达标，
	// 且自动入池总开关得是开的。前端按这个语气显示。
	EligibleAt *time.Time `json:"eligible_at,omitempty"`
	// ProbePasses 已连续通过几次探测。
	ProbePasses int `json:"probe_passes"`
	// ProbeError 上次探测失败原因，供给者据此判断要不要重新授权。
	ProbeError string `json:"probe_error,omitempty"`
	// ProbeAt 上次探测时刻。
	//
	// 有它，供给者才分得清「刚刚失败了一次」和「几天前就坏了、一直没人管」——
	// 只给失败原因不给时间，一条陈旧的错误看起来和一条新鲜的一模一样。
	ProbeAt *time.Time `json:"probe_at,omitempty"`
	// DrainUntil 排空窗到期时刻，仅 draining 状态有值。
	DrainUntil *time.Time `json:"drain_until,omitempty"`

	// NeedsReauth 服务端判定的「这个号现在需要他重新授权一次」。
	//
	// 由服务端算而不是让前端拿 status / probe_error 自己拼，理由与 DailyCapReached
	// 一字不差：两边各写一份判据，漂移的那一天界面会对着一个坏号说「一切正常」。
	// 它同时是那个按钮该不该出现的**唯一**依据——前端不要另立门户。
	NeedsReauth bool `json:"needs_reauth"`

	// ---- 每日共享上限。供给者自己设的，0 = 不限。----

	// DailyCostLimitUSD 每日金额上限，**按官方牌价**计。
	DailyCostLimitUSD float64 `json:"daily_cost_limit_usd"`
	// DailyTokenLimit 每日 token 上限。
	DailyTokenLimit int64 `json:"daily_token_limit"`
	// DailyCostUsedUSD / DailyTokensUsed 今天已经用掉的量（UTC 日）。
	//
	// 必须下发，这不是锦上添花：触顶时账号在库里仍是 schedulable=true
	// （闸门是调度层过滤，不写库），所以界面如果只看 Schedulable 就会在一个
	// 一分钱赚不到的号上显示「接单中」。供给者需要看见分子和分母才知道
	// 「为什么现在没在赚钱」——那正是这个功能存在的意义。
	DailyCostUsedUSD float64 `json:"daily_cost_used_usd"`
	DailyTokensUsed  int64   `json:"daily_tokens_used"`
	// DailyCapReached 今天是否已经触顶。由服务端判定，前端不要自己拿
	// used >= limit 去算——那会在两边的边界语义（>= 还是 >）漂移时静默不一致。
	DailyCapReached bool `json:"daily_cap_reached"`
	// DailyCapResetAt 下一个 UTC 零点。只在设了上限时有值。
	DailyCapResetAt *time.Time `json:"daily_cap_reset_at,omitempty"`
}

// SupplierOnboardingRepository 是自助接入需要的、上游仓储给不了的那部分数据访问。
//
// 全部是 raw SQL：会话表是扩展层新表（不进 ent schema），而 owner_user_id 虽然在 ent
// 里有字段，却没有出现在 service.Account 上——把它加进去要连带改上游的 Account 结构体
// 与 mapper，是两处合并热区的侵入，换来的只是这里少一条 UPDATE。
type SupplierOnboardingRepository interface {
	// CreateSession 落一条待兑换会话。
	CreateSession(ctx context.Context, session *SupplierOAuthSession) error
	// ClaimSession 原子领取：把会话标记为已消费并返回它。
	//
	// accountID 传 0 表示领取一条**接入**会话（兑换出新号），非 0 表示领取一条为
	// 那个账号发起的**重新授权**会话。两类互不通兑，且这个判断在同一条 UPDATE 的
	// WHERE 里完成——领取之后再校验是一句可以被重构删掉的 if。
	//
	// 做成参数而不是另开一个方法：两条几乎一样的 SQL 早晚会漂移，而参数化之后
	// 编译器会强迫每一个调用点显式说出自己要的是哪一类。
	//
	// 归属人不匹配、种类不匹配、已过期、已被消费，一律返回 ErrSupplierOAuthSessionInvalid。
	// 「查出来再判断再更新」在并发重放下会让同一个授权码被兑换两次，
	// 所以这必须是一条 UPDATE ... WHERE ... RETURNING。
	ClaimSession(ctx context.Context, sessionID string, userID, accountID int64) (*SupplierOAuthSession, error)
	// CountPendingSessions 数某人手上还有几条未消费且未过期的会话。
	CountPendingSessions(ctx context.Context, userID int64) (int, error)
	// DeleteExpiredSessions 清理过期未消费的会话，返回删除行数。
	DeleteExpiredSessions(ctx context.Context, limit int) (int64, error)

	// FindAccountIDByRelayEndpoint 按 (base_url, api_key) 查已有的中转账号（M7 查重）。
	// 查全部 apikey 账号——含管理员手动加的自营号：同一个端点挂两次，
	// 两个账号会按同一份供给各算各的分成。没有命中返回 0。
	FindAccountIDByRelayEndpoint(ctx context.Context, platform, baseURL, apiKey string) (int64, error)

	// SetAccountOwner 把账号归属写到 accounts.owner_user_id。
	SetAccountOwner(ctx context.Context, accountID int64, userID int64) error
	// GetAccountOwner 读归属；返回 0 表示自营账号（owner_user_id IS NULL）。
	GetAccountOwner(ctx context.Context, accountID int64) (int64, error)
	// ListAccountIDsByOwner 列出某个供给者名下未删除的账号 id。
	ListAccountIDsByOwner(ctx context.Context, userID int64) ([]int64, error)
	// CountAccountsByOwner 数某个供给者名下未删除的账号有几个（每人上限那道闸）。
	//
	// 不复用 ListAccountIDsByOwner 再取 len：那条查询没有 LIMIT，而这道闸恰恰会在
	// 「某人挂了异常多的号」时被触发——把最坏情况下最长的那个列表整个拉回内存，
	// 只为了数一个数。COUNT 在数据库里做，这是它擅长的事。
	CountAccountsByOwner(ctx context.Context, userID int64) (int, error)
	// ListAccountIDsBySupplyState 列出处于某个接入状态的供给账号 id（观察期任务用）。
	//
	// 只扫 owner_user_id IS NOT NULL 的行：自营账号没有接入状态，也永远不该被
	// 这条流水线碰到——它会改 schedulable，把管理员手工停用的自营号推回池子里
	// 是一次静默的、谁也没同意过的变更。
	ListAccountIDsBySupplyState(ctx context.Context, state string, limit int) ([]int64, error)
	// ListAccountIDsWithUnavailableOwner 列出「归属人已经不可用、号却还在供货」的账号 id。
	//
	// 不可用 = 用户被注销（软删）或被停用。这两种情况下 accounts 行**一点变化都没有**：
	// owner_user_id 上的 ON DELETE SET NULL 永远不会触发（销号是软删，外键看不见），
	// 账号照常可调度、照常被派单。结果是一个已经离开平台的人的订阅额度在继续被消耗，
	// 而他既管不到也停不下。这条查询是唯一发现它们的途径。
	//
	// 只返回「还在供货」的行（可调度，或接入状态尚未到终态），已经 retired 且不可调度的
	// 不返回——否则每一轮扫描都会把同一批历史账号重扫一遍，扫描量随时间只增不减。
	ListAccountIDsWithUnavailableOwner(ctx context.Context, limit int) ([]int64, error)
	// ScrubAccountCredentials 抹掉一个供给账号的凭证，并把它标成已解绑。
	//
	// 这是解绑里**唯一不可逆、也唯一真正兑现了承诺的那一步**：上游没有公开的
	// token 撤销端点（见 supplier_onboarding_service.go 的 DetachAccount 注释），
	// 所以「平台不再持有可用凭证」只能靠平台自己把它删掉来保证，而不是靠一次
	// 可能失败、也无法验证的远端调用。
	//
	// 必须是一条语句：读出来再写回去会在两次操作之间留下一个窗口，窗口里凭证还在。
	// 归属条件写进 WHERE 而不是只依赖调用方先查一遍——调用方确实会先查（见
	// getOwnedAccount），但那次查与这次写之间隔着一次网络往返，归属在那期间被
	// 改掉时，抹的就会是别人的号。
	//
	// 账号不存在、不属于 userID、或已经被删，一律返回 ErrSupplierAccountNotFound。
	ScrubAccountCredentials(ctx context.Context, accountID int64, userID int64) error
	// ApplyReauthCredentials 原地换掉一个供给账号的凭证，并合并一批 extra 键。
	//
	// 与 ScrubAccountCredentials 同族（同一张表、同样把归属写进 WHERE、同样是一条
	// 语句），也同样刻意**不**放进 supplierAccountStore：那个窄接口是「自助接入能对
	// 账号做什么」的清单，而它的注释明确写着改凭证不在里面。重新授权确实要改凭证，
	// 但它不是接入——把这个能力加进那份清单，会让「供给者的接口够不到凭证」这句话
	// 从此不再成立，而那句话是别处几个判断的前提。
	//
	// credentials 整体替换（旧 token 必须消失），extra 合并（每日上限、接入状态要留着）。
	// 不碰 status / schedulable——那两样要发调度事件，由 accountRepo 改。
	//
	// 账号不存在、不属于 userID、或已经被删，一律返回 ErrSupplierAccountNotFound。
	ApplyReauthCredentials(ctx context.Context, accountID int64, userID int64, credentials map[string]any, extra map[string]any) error
	// RecordAgreementAcceptance 记一条协议同意（见 supplier_agreement.go）。
	//
	// 幂等：同一个人对同一个版本重复点同意不报错，且库里保留**最早**那一行——
	// 那才是他做出决定的时刻，后面几次只是重复点击。
	RecordAgreementAcceptance(ctx context.Context, acceptance *SupplierAgreementAcceptance) error
	// FindAgreementAcceptance 查某人是否同意过某个具体版本；没有返回 (nil, nil)。
	//
	// 门禁比对的就是它。刻意是"精确版本"而不是"最近一次同意"：运营把协议回滚到
	// 上一版时，同意过那一版的人不该被要求重新同意。
	FindAgreementAcceptance(ctx context.Context, userID int64, version string) (*SupplierAgreementAcceptance, error)
	// LatestAgreementAcceptance 查某人最近一次同意的记录；没有返回 (nil, nil)。
	// 只用来在界面上区分「从没同意过」与「同意的是旧版」，不参与门禁判断。
	LatestAgreementAcceptance(ctx context.Context, userID int64) (*SupplierAgreementAcceptance, error)

	// RecordAccountOrigin 记一个新供给账号的接入来源 IP（迁移 230）。
	//
	// 幂等（account_id 是主键，冲突不覆盖）：重复调用保留第一次那行。一个账号只可能
	// 被接入一次，第二次写进来的 IP 必然来自某条不该存在的路径，覆盖它等于把证据改掉。
	//
	// clientIP 为空时**不写行**：库里不该存在「来源未知」的记录，那种行会被按 IP 的
	// COUNT 当成一个真实的来源聚在一起——反向代理没配好的部署里，那等于把所有人算成
	// 同一个网络，一道闸瞬间变成全站封禁。
	RecordAccountOrigin(ctx context.Context, accountID int64, userID int64, clientIP string) error
	// CountAccountsByOriginIP 数某个来源 IP 上还活着的供给账号有几个（每 IP 上限那道闸）。
	//
	// 「还活着」= 对应的 accounts 行未被软删。来源表本身只增不删（它是取证材料），
	// 所以这条查询必须 JOIN 回 accounts——否则一个正常换过几次号的供给者，
	// 会被他自己早就解绑掉的历史记录顶到上限。
	CountAccountsByOriginIP(ctx context.Context, clientIP string) (int, error)

	// FindAccountIDByUpstreamIdentity 按某个上游身份键查已存在的账号，用于拒绝重复提交。
	// 返回 0 表示没有。key 只接受 SupplierIdentityKeys 里的值，实现按它选一条写死的
	// 语句——键名绝不拼进 SQL。
	FindAccountIDByUpstreamIdentity(ctx context.Context, platform string, key SupplierIdentityKey, value string) (int64, error)
}

// SupplierIdentityKey 是用来判定「这是不是同一份上游订阅」的凭证字段名。
//
// 取值同时也是 accounts.credentials 里的 jsonb 键名——查重语句读的就是建号时写进去
// 的那几个字段，两边共用同一组常量，漂移了会直接编译不过。
type SupplierIdentityKey string

const (
	// SupplierIdentityAccountUUID 上游账号 uuid。最强的键：一个订阅一个值。
	SupplierIdentityAccountUUID SupplierIdentityKey = "account_uuid"
	// SupplierIdentityEmailAddress 上游账号邮箱。次强：同一个人重挂会被它抓住。
	SupplierIdentityEmailAddress SupplierIdentityKey = "email_address"
)

// SupplierIdentityKeys 是查重时依次尝试的键，强度从高到低。
//
// **刻意不含 org_uuid**。团队组织下多个成员各有各的订阅席位，用 org_uuid 查重会把
// 同事的合法第二个号判成重复——那是一个会挡住真实供给的误报，比漏报更难被发现
// （供给者只会看到一句「已经连过了」然后放弃）。org_uuid 仍然写进 credentials，
// 只是不拿它当身份键。
var SupplierIdentityKeys = []SupplierIdentityKey{
	SupplierIdentityAccountUUID,
	SupplierIdentityEmailAddress,
}
