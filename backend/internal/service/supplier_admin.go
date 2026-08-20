// APEXONE-EXT: 双边市场——管理端运营视图的领域类型与仓储接口。
//
// 这一层回答运营在「钱已经在流」之后必须能回答的四个问题：谁挂了几个号（名册）、
// 这个月要付多少（负债与本期入账）、谁的号在被封（健康度）、哪些卡在观察期（状态分布）。
//
// 全部是**只读**的。管理端能改的东西只有 §3.1 那三组设置，改账号归属、改余额、
// 手工放行观察期都不在这一刀里：那些是会动钱和归属的写操作，需要各自的审计与
// 复核路径，混进一个「看板」接口里迟早会被当成看板随手点。
//
// 本文件只放类型与接口；SQL 实现在 internal/repository/supplier_admin_repo.go。
package service

import (
	"context"
	"time"
)

// SupplyWalletBalance 是一个钱包（或一批钱包合计）的四个数。
//
// Available + Frozen 就是平台此刻对这些人的**待付负债**：两笔钱都已经被记为供给者
// 所得，差别只是能不能马上取走。运营问「这个月要付多少」，先看的是这个数。
type SupplyWalletBalance struct {
	// Available 已解冻，随时可被消费或提现。
	Available float64 `json:"available"`
	// Frozen 冻结中，冻结窗内发生拒付还能被追回（见 §5 不变量 2）。
	Frozen float64 `json:"frozen"`
	// History 累计入账，只增不减。
	History float64 `json:"history"`
	// Spent 累计已被消费掉的，只增不减。
	Spent float64 `json:"spent"`
}

// SupplyWalletTotals 是全站钱包合计：余额四件套 + 钱包个数。
//
// 个数单独一层是因为它只在「合计」这个语境下有意义，而名册里的每一行天然是一个人。
// 内嵌 SupplyWalletBalance 让两处共用同一组字段名，前端一个格式化函数吃两种响应。
type SupplyWalletTotals struct {
	// Wallets 有钱包行的供给者数（含余额为零的）。
	Wallets int64 `json:"wallets"`
	SupplyWalletBalance
}

// SupplyAccountCounts 是供给账号按状态的分布。
//
// 刻意做成固定字段而不是 map[string]int64：状态是一个封闭集合，用 map 会让前端
// 拿到一个自己不认识的键时只能猜着渲染，而多出来的那个键恰恰意味着后端加了状态、
// 前端还没跟上——那时应该编译不过，而不是画出一个没有翻译的格子。
type SupplyAccountCounts struct {
	Total         int64 `json:"total"`
	PendingReview int64 `json:"pending_review"`
	Active        int64 `json:"active"`
	Draining      int64 `json:"draining"`
	Retired       int64 `json:"retired"`
	// Unhealthy 上游健康状态不是 active 的（凭证失效、被封、被限）。
	// 这是「谁的号在被封」的直接答案，与接入状态正交：一个 active 的号也可能是坏的。
	Unhealthy int64 `json:"unhealthy"`
	// Schedulable 此刻真的在接单的号。它与 Active 不相等是正常的——
	// 被限流、临时不可调度、排空中的号都会让两个数字岔开。
	Schedulable int64 `json:"schedulable"`
}

// SupplyLedgerWindow 是最近一个窗口内的流水合计，按动作分开。
//
// Thaw 刻意也报出来但**不参与任何加减**：它是钱包内部搬运（frozen → available），
// 与 Accrue 加在一起会把同一笔钱数两遍。报它是为了让运营能看出解冻任务还活着。
type SupplyLedgerWindow struct {
	// Days 窗口天数。
	Days int `json:"days"`
	// Accrued 本窗口新增入账（供给者赚到的）。
	Accrued float64 `json:"accrued"`
	// Clawed 本窗口因拒付被追回的。
	Clawed float64 `json:"clawed"`
	// Thawed 本窗口解冻搬运量，仅用于判断解冻任务是否在跑。
	Thawed float64 `json:"thawed"`
	// Spent 本窗口供给者用钱包抵扣掉的。
	Spent float64 `json:"spent"`
	// Withdrawn 本窗口提现申请扣走的（申请即扣款，见 supplier_withdrawal.go 顶部）。
	//
	// 注意它是**申请额**，不是已打款额：一张 pending 的单子也计在里面。想知道真的
	// 打出去多少，看提现队列里 paid 的那些。
	Withdrawn float64 `json:"withdrawn"`
	// WithdrawReverted 本窗口被拒绝/被撤回退回可用区的。
	//
	// 与 Withdrawn 分开报而不是相减：净额（Withdrawn - WithdrawReverted）只能回答
	// 「钱少了多少」，回答不了「有多少单被拒了」。后者是一个需要有人去看一眼的信号——
	// 大量退回意味着渠道配置或审核标准出了问题。
	WithdrawReverted float64 `json:"withdraw_reverted"`
}

// SupplyMarketOverview 是运营看板顶部的一屏。
type SupplyMarketOverview struct {
	// Suppliers 名下有过供给账号或有过钱包的人数。
	Suppliers int64               `json:"suppliers"`
	Accounts  SupplyAccountCounts `json:"accounts"`
	Wallet    SupplyWalletTotals  `json:"wallet"`
	Window    SupplyLedgerWindow  `json:"window"`
}

// SupplierRosterEntry 是名册里的一行：一个供给者的账号分布与钱包快照。
type SupplierRosterEntry struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	// UserStatus 用户自身的状态。一个被封的用户仍然可能有余额待付——
	// 藏起来不显示只会让这笔钱在对账时凭空冒出来。
	UserStatus string              `json:"user_status"`
	Accounts   SupplyAccountCounts `json:"accounts"`
	Wallet     SupplyWalletBalance `json:"wallet"`
	// LastAccrualAt 最后一次入账时刻。空 = 从未赚到过钱（挂了号但没被调度到，
	// 或者一直卡在观察期）——这本身就是一条要跟进的线索。
	LastAccrualAt *time.Time `json:"last_accrual_at,omitempty"`
}

// SupplierRosterSort 是名册的排序键。
//
// 与身份键一样：取值不拼进 SQL，实现按它选一段写死的 ORDER BY，未知值报错。
// 排序键是最容易被顺手改成「前端传什么就拼什么」的地方，而它恰好紧挨着一张
// 带余额的表。
type SupplierRosterSort string

const (
	// SupplierRosterSortOwed 按待付（可用 + 冻结）倒序。默认——「先看要付谁多少钱」。
	SupplierRosterSortOwed SupplierRosterSort = "owed"
	// SupplierRosterSortHistory 按累计入账倒序，看谁贡献最大。
	SupplierRosterSortHistory SupplierRosterSort = "history"
	// SupplierRosterSortAccounts 按挂号数倒序。
	SupplierRosterSortAccounts SupplierRosterSort = "accounts"
	// SupplierRosterSortRecent 按最后入账时间倒序，看谁还活着。
	SupplierRosterSortRecent SupplierRosterSort = "recent"
)

// SupplierRosterSorts 是全部合法排序键，前端拿它渲染下拉框。
var SupplierRosterSorts = []SupplierRosterSort{
	SupplierRosterSortOwed,
	SupplierRosterSortHistory,
	SupplierRosterSortAccounts,
	SupplierRosterSortRecent,
}

// SupplierRosterFilter 是名册的查询条件。
type SupplierRosterFilter struct {
	// Keyword 按邮箱/用户名模糊匹配，空 = 不筛。
	Keyword string
	// Sort 排序键，空 = SupplierRosterSortOwed。
	Sort     SupplierRosterSort
	Page     int
	PageSize int
}

// SupplyAccountAdminView 是运营看到的供给账号。
//
// 比 SupplierAccountView 多了归属人，少了什么都没少——因为供给者能看的字段
// 运营也该能看。凭证仍然一个字节都不出现：运营不需要它，泄漏它却能直接冒用
// 供给者的订阅。
type SupplyAccountAdminView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	OwnerUserID int64  `json:"owner_user_id"`
	OwnerEmail  string `json:"owner_email,omitempty"`
	SupplyState string `json:"supply_state"`
	Status      string `json:"status"`
	// ErrorMessage 上游报的失败原因，运营判断「是号坏了还是我们打不通」的第一手材料。
	ErrorMessage string     `json:"error_message,omitempty"`
	Schedulable  bool       `json:"schedulable"`
	EmailAddress string     `json:"email_address,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`

	// ---- 观察期 / 排空 ----

	ProbationSince *time.Time `json:"probation_since,omitempty"`
	ProbePasses    int        `json:"probe_passes"`
	ProbeError     string     `json:"probe_error,omitempty"`
	DrainUntil     *time.Time `json:"drain_until,omitempty"`
}

// SupplyAccountHealth 是账号健康度筛选。
type SupplyAccountHealth string

const (
	// SupplyAccountHealthAny 不按健康度筛。
	SupplyAccountHealthAny SupplyAccountHealth = ""
	// SupplyAccountHealthHealthy 只看 status = active 的。
	SupplyAccountHealthHealthy SupplyAccountHealth = "healthy"
	// SupplyAccountHealthUnhealthy 只看 status <> active 的——「谁的号在被封」。
	SupplyAccountHealthUnhealthy SupplyAccountHealth = "unhealthy"
)

// SupplyAccountAdminFilter 是账号明细的查询条件。
type SupplyAccountAdminFilter struct {
	// State 接入状态，空 = 全部。非法值不报错而是查不到——它来自前端下拉框，
	// 传了个没见过的状态就该是空结果，不是 500。
	State string
	// Health 健康度，见 SupplyAccountHealth。
	Health SupplyAccountHealth
	// OwnerUserID > 0 时只看某个供给者的号（从名册点进来）。
	OwnerUserID int64
	Page        int
	PageSize    int
}

// SupplyAdminLedgerEntry 是运营看到的一条流水：钱包流水加上归属人的邮箱。
//
// 内嵌而不是复制字段：供给者看的和运营看的必须是同一条记录，两份结构体迟早
// 会有一份忘了跟着加字段，而那个字段多半是金额相关的。
type SupplyAdminLedgerEntry struct {
	SupplierCreditLedgerEntry
	// UserEmail 收款人邮箱。运营检索流水时手上有的是邮箱，不是 user_id。
	UserEmail string `json:"user_email,omitempty"`
}

// SupplyAdminLedgerFilter 是全局流水检索条件。
//
// 与供给者侧的 SupplierCreditLedgerFilter 的关键差别：UserID 可以是 0（看全站）。
// 所以它必须是一个**独立的类型**——把那个结构体的 UserID 改成「0 = 全部」，
// 供给者侧任何一处漏传 user_id 的 bug 就会从「查不到」变成「看到所有人的账」。
type SupplyAdminLedgerFilter struct {
	// UserID > 0 时只看某人。
	UserID int64
	// Action 空 = 全部动作。
	Action string
	// AccountID > 0 时只看某个账号产出的流水。
	AccountID int64
	// RequestID 精确匹配。对账时手上通常只有一个 request_id。
	RequestID string
	StartAt   *time.Time
	EndAt     *time.Time
	Page      int
	PageSize  int
}

// SupplierAdminRepository 是管理端运营视图的数据访问。
//
// 与其它几个仓储一样是手写 SQL：这里做的全是跨表聚合（accounts × supplier_credits
// × supplier_credit_ledger × users），ent 的 builder 表达不了，也没必要。
type SupplierAdminRepository interface {
	// Overview 读看板汇总。windowDays 是流水窗口天数。
	Overview(ctx context.Context, windowDays int) (*SupplyMarketOverview, error)
	// ListSuppliers 分页读名册。
	ListSuppliers(ctx context.Context, filter SupplierRosterFilter) ([]SupplierRosterEntry, int64, error)
	// ListAccounts 分页读供给账号明细。
	ListAccounts(ctx context.Context, filter SupplyAccountAdminFilter) ([]SupplyAccountAdminView, int64, error)
	// ListLedger 分页读全站钱包流水。
	ListLedger(ctx context.Context, filter SupplyAdminLedgerFilter) ([]SupplyAdminLedgerEntry, int64, error)
}
