// APEXONE-EXT: 双边市场——提现的领域类型与仓储接口。
//
// 钱包（supplier_credit.go）到这里才有第二个出口。在此之前 credit 只能抵扣供给者
// 自己发起的请求——一个只能内部消费的余额不是收入，"把闲置订阅变成钱"这句话在
// 没有提现之前是不成立的。
//
// # 一条贯穿始终的时序：申请即扣款
//
// CreateWithdrawal 在同一个事务里做三件事：建单、写一条 withdraw 流水、从
// available_credit 扣掉。审批只改单子的状态，**不再动钱**（拒绝/撤回时退回，
// 写一条 withdraw_revert）。
//
// 反过来（审批时才扣）在这套系统里是错的：申请到审批之间隔着人的工作时间，
// 那段时间里那笔钱还留在可用区，供给者可以拿它继续付自己的请求。运营看到一张
// 100 的单子点了打款，钱可能早就花完了——要么平台垫付，要么余额被扣成负数。
//
// 代价是被拒时要退回去，而退回去这件事必须只发生一次。两道闸挡它：状态机的
// `WHERE status = 'pending'` 条件更新，以及流水表 (action, request_id) 上的部分
// 唯一索引（revert 流水的 request_id = "withdraw-revert:<单号>"）。
//
// 本文件只放类型与接口；SQL 实现在 internal/repository/supplier_withdrawal_repo.go。
package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrSupplierWithdrawalDisabled 提现总开关关着。
	ErrSupplierWithdrawalDisabled = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_DISABLED", "withdrawals are not open")
	// ErrSupplierWithdrawalNotConfigured 开关开着但一个收款渠道都没配。
	//
	// 与 disabled 分开报，因为两者对运营意味着完全不同的动作：一个是"还没开",
	// 一个是"开了但配漏了"，后者是个会让供给者看到一个点不动的按钮的错误配置。
	ErrSupplierWithdrawalNotConfigured = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_NOT_CONFIGURED", "no payout channel is configured")
	// ErrSupplierWithdrawalChannelInvalid 收款渠道不在白名单里。
	ErrSupplierWithdrawalChannelInvalid = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_CHANNEL_INVALID", "unsupported payout channel")
	// ErrSupplierWithdrawalBelowMinimum 低于起提额。
	ErrSupplierWithdrawalBelowMinimum = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_BELOW_MINIMUM", "amount is below the minimum withdrawal")
	// ErrSupplierWithdrawalFeeExceedsAmount 手续费吃掉了整笔金额。
	//
	// 链上渠道的 gas 从供给者收益里扣（链上实发 = amount - fee）。fee ≥ amount 时
	// 实发为零或负数——**必须在建单之前**拦住：让这张单子建起来，供给者的钱会被
	// 从可用区扣走，然后 worker 发现没得可发，于是一笔钱卡在一张永远推不动的单子上。
	//
	// 4xx 而不是 5xx：这不是故障，是"你提的太少了"，而正确的动作在调用方手里
	// （提多一点，或者等 gas 便宜一点）。运营的对应动作是把起提额调高。
	ErrSupplierWithdrawalFeeExceedsAmount = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_FEE_EXCEEDS_AMOUNT", "the network fee is not covered by this amount")
	// ErrSupplierWithdrawalFeeUnavailable 手续费估算不是一个能落库的数。
	//
	// 5xx 而不是 4xx：调用方没做错任何事，是链上客户端算出了 NaN / 无穷 / 负数
	// （币价配成 0 之类）。把它当成 0 放行，就是静默地让金库替所有人垫 gas；
	// 把它落库，DECIMAL(20,8) 那一列会替我们做一次没人看见的截断。
	// 两种都不如让这笔申请当场失败——它指向运维，而运维改完配置这条路就通了。
	ErrSupplierWithdrawalFeeUnavailable = infraerrors.ServiceUnavailable(
		"SUPPLIER_WITHDRAWAL_FEE_UNAVAILABLE", "the network fee cannot be determined right now")
	// ErrSupplierWithdrawalTooManyPending 未决单已达上限。
	ErrSupplierWithdrawalTooManyPending = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_TOO_MANY_PENDING", "too many pending withdrawals")
	// ErrSupplierWithdrawalNotFound 单子不存在，或不是你的。
	//
	// 两种情况合并成同一个错误，与 ErrSupplierAccountNotFound 同一个理由：
	// 区分它们等于提供一个枚举他人单号的信息面。
	ErrSupplierWithdrawalNotFound = infraerrors.NotFound(
		"SUPPLIER_WITHDRAWAL_NOT_FOUND", "withdrawal not found")
	// ErrSupplierWithdrawalNotPending 单子已经是终态。
	//
	// 这是并发双击/重复审批唯一的出口：条件更新没命中行，就报它。
	ErrSupplierWithdrawalNotPending = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_NOT_PENDING", "withdrawal is already resolved")
	// ErrSupplierWithdrawalProcessing worker 正拿着这张单子。
	//
	// 与 NotPending 分开报：NotPending 说的是「已经处理完了」，这条说的是
	// 「正在处理，稍等」。合并的话，运营对一张 processing 单点拒绝会看到
	// 「已处理」，去列表里找结果又找不到，然后开始怀疑数据。
	ErrSupplierWithdrawalProcessing = infraerrors.BadRequest(
		"SUPPLIER_WITHDRAWAL_PROCESSING", "withdrawal is being settled on-chain")
)

// 提现单状态。
const (
	// SupplierWithdrawalStatusPending 待处理。钱已从可用区扣走，挂在这张单子上。
	SupplierWithdrawalStatusPending = "pending"
	// SupplierWithdrawalStatusProcessing worker 正在上链结算（中间态，仅链上单会进）。
	//
	// 进入这个状态意味着 nonce 已经预留、随时可能已有交易在内存池里。从这一刻起，
	// 除了链上给出的答案（confirmed/failed）没人能推进它——管理端的打款/拒绝
	// 对 processing 一律报 ErrSupplierWithdrawalProcessing。放开这个口子，
	// 运营的一次「拒绝退款」就可能与一笔正在路上的转账同时成立：双付。
	SupplierWithdrawalStatusProcessing = "processing"
	// SupplierWithdrawalStatusPaid 已打款（终态）。
	SupplierWithdrawalStatusPaid = "paid"
	// SupplierWithdrawalStatusFailed 链上打款没成（不是终态：钱还扣着，等运营裁决）。
	//
	// 两种来路，都写在 last_error 里：链上明确 revert（收据在、status=0，钱确定
	// 没出去），或 worker 等确认超过了放弃期限（结果不明，退款前必须先查链）。
	// **刻意不自动退款**，哪怕是确定 revert 的那种：revert 几乎总意味着结构性
	// 配置错误（金库没钱、合约不对），自动退款会抹掉现场并邀请供给者立刻重试
	// 一次注定同样失败的申请。运营从 failed 出发有两条路：核实后标记已打款
	// （链上其实成了 / 人工补打了），或拒绝退款。
	SupplierWithdrawalStatusFailed = "failed"
	// SupplierWithdrawalStatusRejected 运营拒绝（终态），钱已退回可用区。
	SupplierWithdrawalStatusRejected = "rejected"
	// SupplierWithdrawalStatusCanceled 供给者自己撤回（终态），钱已退回可用区。
	SupplierWithdrawalStatusCanceled = "canceled"
)

// 字段长度上限。与迁移 229 的列宽一致——在这里挡住，错误消息才说得清楚是哪个字段，
// 而不是抛一个 Postgres 的 "value too long for type character varying(64)"。
const (
	SupplierPayoutAccountMaxLen  = 256
	SupplierWithdrawalNoteMaxLen = 500
	// SupplierWithdrawalExternalRefMaxLen 打款凭证/交易号。
	SupplierWithdrawalExternalRefMaxLen = 128
)

// SupplierWithdrawal 是一张提现申请单。
//
// 字段分三段：供给者填的（金额、渠道、账号、备注）、系统写的（状态、流水号、时间）、
// 运营写的（审核人、意见、打款凭证）。三段刻意不混，读代码时能一眼看出谁能改什么。
type SupplierWithdrawal struct {
	ID     int64   `json:"id"`
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`

	PayoutChannel string  `json:"payout_channel"`
	PayoutAccount string  `json:"payout_account"`
	UserNote      *string `json:"user_note,omitempty"`

	// ---- 链上结算（迁移 234）。人工渠道的单子这四项全是零值。----

	// Network 非空 = 这张单子由 worker 上链结算；空 = 沿用人工打款。
	//
	// 用一个可空列分辨两条路径，而不是加一个 is_onchain 布尔：布尔为真时还要再问
	// 一次"那是哪条链"，于是两个字段就能不一致。
	Network *string `json:"network,omitempty"`
	// TokenSymbol 仅作展示。
	TokenSymbol *string `json:"token_symbol,omitempty"`
	// TokenAddress 稳定币合约地址，**建单那一刻的快照**。
	//
	// 落地的是地址而不是符号：符号→地址的映射来自配置，配置一改，历史单子上
	// 那个 "USDT" 指的是哪个合约就被悄悄改写了。钉在行上，这张单子发的是什么，
	// 十年后还答得出来。
	TokenAddress *string `json:"token_address,omitempty"`
	// FeeAmount gas 手续费，从 Amount **内部**切分出来（链上实发 = Amount - FeeAmount）。
	//
	// Amount 仍然是从可用区扣走的总额，这一条不能改：ledger 的 withdraw 流水、
	// 退款、对账导出全部按 Amount 走。于是退款时按 Amount 全额退——gas 还没花出去。
	FeeAmount float64 `json:"fee_amount"`

	// ---- worker 执行留痕（M4）。人工单与还没被 worker 碰过的单子上全是 nil。----
	// 全部走管理端与对账，不进供给侧 DTO（供给者看 external_ref 就够了）。

	// TxHash 广播出去那笔交易的哈希（本地签名时算出的那一个，见 §9.5）。
	TxHash *string `json:"tx_hash,omitempty"`
	// ChainNonce 广播用的 nonce，**广播之前**落库、重试原样复用——防双付的唯一手段。
	ChainNonce *int64 `json:"chain_nonce,omitempty"`
	// BroadcastedAt 第一次广播的时刻，只写一次；worker 放弃等确认的时钟。
	BroadcastedAt *time.Time `json:"broadcasted_at,omitempty"`
	// LastError 上一次处理失败的原因。成功进入 paid 时清空。
	LastError *string `json:"last_error,omitempty"`
	// LeasedUntil worker 租约。非空且在未来 = 有人正在处理。
	LeasedUntil *time.Time `json:"leased_until,omitempty"`

	// LedgerID 申请时那条 withdraw 流水。供给者对账时用它把单子和流水对上。
	LedgerID *int64 `json:"ledger_id,omitempty"`

	// ReviewerID 处理人。刻意**不**对供给者暴露（见 dto 层的映射）：
	// 运营是谁与这笔钱到不到账无关，暴露它只是给具体的人招来私下沟通。
	ReviewerID  *int64  `json:"reviewer_id,omitempty"`
	ReviewNote  *string `json:"review_note,omitempty"`
	ExternalRef *string `json:"external_ref,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// Pending 单子是否还挂着。
func (w *SupplierWithdrawal) Pending() bool {
	return w != nil && w.Status == SupplierWithdrawalStatusPending
}

// OnChain 这张单子是不是由 worker 上链结算。
//
// 判据只有 Network 一个，且要求它非空**且非空串**：一条 network = '' 的行
// 在数据库里是完全合法的，而它既不是"人工"也不是任何一条链——把它归到人工那边，
// 是因为人工那条路上有人看着。
func (w *SupplierWithdrawal) OnChain() bool {
	return w != nil && w.Network != nil && strings.TrimSpace(*w.Network) != ""
}

// NetAmount 链上实际到账的金额 = 总额 - 手续费。
//
// 它不落库：落一个可以由另外两列算出来的数，就多了一处能与它们不一致的地方，
// 而不一致时没人知道该信哪个。
func (w *SupplierWithdrawal) NetAmount() float64 {
	if w == nil {
		return 0
	}
	return w.Amount - w.FeeAmount
}

// SupplierWithdrawalCreateParams 是一次申请。
type SupplierWithdrawalCreateParams struct {
	UserID        int64
	Amount        float64
	PayoutChannel string
	PayoutAccount string
	UserNote      string
	// Network / TokenSymbol / TokenAddress / FeeAmount 链上结算的四项快照，
	// 人工渠道全传零值。它们由服务层在建单**之前**定下来（渠道注册表 + 链上客户端），
	// 不让仓储自己去查：一条 SQL 依赖一次 RPC 调用，事务就会被一次网络抖动拖住。
	Network      string
	TokenSymbol  string
	TokenAddress string
	FeeAmount    float64
	// MaxPending 允许的未决单上限，由服务层从设置里读出来传进来。
	//
	// 不让仓储自己去读设置：那会让一条 SQL 依赖一个带缓存的服务，
	// 也会让「用什么上限判的」在事务里变成一个不确定的值。
	MaxPending int
}

// SupplierWithdrawalResolveParams 是一次终态推进（打款/拒绝/撤回）。
type SupplierWithdrawalResolveParams struct {
	ID     int64
	Status string
	// UserID 非 0 时额外要求单子属于这个人。供给者撤回自己的单子时用它，
	// 管理端处理时传 0。写成一个可选条件而不是两条 SQL：
	// 「只能动自己的单子」是一个 WHERE 子句，不是一段流程。
	UserID     int64
	ReviewerID *int64
	// Refund 为真时把金额退回可用区并写一条 withdraw_revert 流水。
	// rejected/canceled 传真，paid 传假——钱已经出去了，退回来就是凭空发钱。
	Refund bool
	// FromFailed 允许从 failed 推进。**只有管理端传真**：failed 是「链上打款
	// 没成、等运营裁决」的停靠位，供给者的撤回不该绕过这次裁决——尤其是
	// 「结果不明」那一种 failed，退款之前必须有人先去链上看一眼那笔哈希。
	FromFailed  bool
	ReviewNote  string
	ExternalRef string
}

// SupplierWithdrawalFilter 是提现列表的筛选条件。
type SupplierWithdrawalFilter struct {
	// UserID 0 = 不限（仅管理端会这么传）。
	UserID int64
	// Status 空 = 不限。
	Status string
	// StartAt / EndAt 按建单时间筛，nil = 不限。
	//
	// 屏幕上的列表目前不传这两个（分页翻着看不需要），加在这里是为了让**导出**
	// 与列表共用同一个 WHERE 拼装函数。这不是省几行代码：一份对账文件如果比
	// 运营刚看过的那一屏多几行或少几行，他会先怀疑账不对而不是怀疑筛子不同，
	// 而两个各写各的筛选函数迟早会漂成两个筛子。
	StartAt  *time.Time
	EndAt    *time.Time
	Page     int
	PageSize int
}

// SupplierWithdrawalRepository 是提现单的持久化接口。
//
// 与 SupplierCreditRepository 分开是因为它们的事务边界不同：钱包那套的写入点在
// 计费热路径的事务内部，所以拆成了「接受 executor 的包级函数」；提现这套永远
// 自己开事务，没有任何一条路径需要挤进别人的事务里。
type SupplierWithdrawalRepository interface {
	// Create 建单 + 扣款 + 写流水，一个事务。余额不足返回 ErrSupplierCreditInsufficient，
	// 未决单超限返回 ErrSupplierWithdrawalTooManyPending。
	Create(ctx context.Context, params SupplierWithdrawalCreateParams) (*SupplierWithdrawal, error)
	// Resolve 把一张 pending 单推进终态；Refund 为真时同时退款。
	// 单子不在 pending 返回 ErrSupplierWithdrawalNotPending，不存在/不是你的返回
	// ErrSupplierWithdrawalNotFound。
	Resolve(ctx context.Context, params SupplierWithdrawalResolveParams) (*SupplierWithdrawal, error)
	// List 分页读。
	List(ctx context.Context, filter SupplierWithdrawalFilter) ([]SupplierWithdrawal, int64, error)
	// CountPending 统计某人的未决单数（管理端看板与申请前置校验共用）。
	CountPending(ctx context.Context, userID int64) (int64, error)
}

// SupplierPayoutFinishParams 是 worker 给一张单子的最终答案。
type SupplierPayoutFinishParams struct {
	ID int64
	// Status 只取 paid / failed 两个值。
	Status string
	// TxHash paid 时写进 external_ref（供给者对账的凭证）与 tx_hash。
	TxHash string
	// Reason failed 时写进 last_error，给运营裁决用。
	Reason string
}

// SupplierPayoutQueueRepository 是打款 worker 的仓储面。
//
// 与 SupplierWithdrawalRepository 分开定义（同一个具体实现），是因为消费者
// 完全不同：提现服务永远不该拿得到「跳过状态机直接标 paid」这种能力，
// 而 worker 恰恰需要它——合并成一个接口，每个 service 层的测试桩都要
// 陪着实现五个自己用不到的方法。
//
// 五个方法合起来是一台状态机，调用顺序是硬约束（见 SupplierPayoutWorker）：
// Claim → Begin →（广播）→ RecordTx →（等确认）→ Finish；
// 任何一步陷入「还不知道」就 Release，把单子留给下一轮。
type SupplierPayoutQueueRepository interface {
	// ClaimPayoutDue 捞出到期该处理的链上单并续上租约。
	//
	// 只动租约、不动状态：拿着租约不代表要动钱（客户端可能没配好），
	// 状态要等第一个不可逆动作（预留 nonce）之前才翻成 processing。
	// 租约本身就是与管理端的互斥——Resolve 对租约未到期的单子拒绝动手。
	ClaimPayoutDue(ctx context.Context, limit int, lease time.Duration) ([]SupplierWithdrawal, error)
	// BeginPayout 把 nonce 落库并把单子翻进 processing，**必须在广播之前**。
	//
	// 条件更新：单子已不在 pending/processing、或已带着另一个 nonce 时返回
	// ErrSupplierWithdrawalNotPending——调用方看到它必须放弃广播，
	// 这是「管理端在租约过期后已把单子处理掉」唯一的防线。
	BeginPayout(ctx context.Context, id int64, nonce uint64) error
	// RecordPayoutTx 记下广播出去的哈希；broadcasted_at 只在第一次写。
	RecordPayoutTx(ctx context.Context, id int64, txHash string) error
	// FinishPayout 写终局：paid（清 last_error、写 external_ref、resolved_at）
	// 或 failed（写 last_error，等运营）。两者都不退款——退款只有一个入口
	// （Resolve 的 Refund），让「会加钱的代码」只有一处。
	FinishPayout(ctx context.Context, params SupplierPayoutFinishParams) (*SupplierWithdrawal, error)
	// ReleasePayoutLease 交还租约（可带退避），单子留在原状态等下一轮。
	ReleasePayoutLease(ctx context.Context, id int64, lastError string, retryAfter time.Duration) error
}
