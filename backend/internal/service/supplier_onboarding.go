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
	// ErrSupplierAccountNotFound 账号不存在，或不属于当前供给者。
	//
	// 同样合成：「存在但不是你的」和「不存在」对调用方必须是同一个回答，
	// 否则账号 id 就成了一个可枚举的探针。
	ErrSupplierAccountNotFound = infraerrors.NotFound(
		"SUPPLIER_ACCOUNT_NOT_FOUND", "supply account not found")
	// ErrSupplierOAuthTooManyPending 同一个人堆积了太多未完成的授权会话。
	ErrSupplierOAuthTooManyPending = infraerrors.BadRequest(
		"SUPPLIER_OAUTH_TOO_MANY_PENDING", "too many pending authorization sessions")
	// ErrSupplierAccountNotRetired 只有已下线或正在排空的账号能被重新挂回来。
	//
	// 这个错误不合并进 NotFound：调用方已经证明了自己是这个账号的主人（能读到它），
	// 所以「它现在不是下线状态」不是泄漏，而是他需要知道的、能自己纠正的信息。
	ErrSupplierAccountNotRetired = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_NOT_RETIRED", "only a retired or draining supply account can be resumed")
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
	// DrainUntil 排空窗到期时刻，仅 draining 状态有值。
	DrainUntil *time.Time `json:"drain_until,omitempty"`
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
	// 归属人不匹配、已过期、已被消费，一律返回 ErrSupplierOAuthSessionInvalid。
	// 「查出来再判断再更新」在并发重放下会让同一个授权码被兑换两次，
	// 所以这必须是一条 UPDATE ... WHERE ... RETURNING。
	ClaimSession(ctx context.Context, sessionID string, userID int64) (*SupplierOAuthSession, error)
	// CountPendingSessions 数某人手上还有几条未消费且未过期的会话。
	CountPendingSessions(ctx context.Context, userID int64) (int, error)
	// DeleteExpiredSessions 清理过期未消费的会话，返回删除行数。
	DeleteExpiredSessions(ctx context.Context, limit int) (int64, error)

	// SetAccountOwner 把账号归属写到 accounts.owner_user_id。
	SetAccountOwner(ctx context.Context, accountID int64, userID int64) error
	// GetAccountOwner 读归属；返回 0 表示自营账号（owner_user_id IS NULL）。
	GetAccountOwner(ctx context.Context, accountID int64) (int64, error)
	// ListAccountIDsByOwner 列出某个供给者名下未删除的账号 id。
	ListAccountIDsByOwner(ctx context.Context, userID int64) ([]int64, error)
	// ListAccountIDsBySupplyState 列出处于某个接入状态的供给账号 id（观察期任务用）。
	//
	// 只扫 owner_user_id IS NOT NULL 的行：自营账号没有接入状态，也永远不该被
	// 这条流水线碰到——它会改 schedulable，把管理员手工停用的自营号推回池子里
	// 是一次静默的、谁也没同意过的变更。
	ListAccountIDsBySupplyState(ctx context.Context, state string, limit int) ([]int64, error)
	// FindAccountIDByUpstreamUUID 按上游账号 uuid 查已存在的账号，用于拒绝重复提交。
	// 返回 0 表示没有。
	FindAccountIDByUpstreamUUID(ctx context.Context, platform, accountUUID string) (int64, error)
}
