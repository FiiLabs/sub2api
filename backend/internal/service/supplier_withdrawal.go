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
)

// 提现单状态。
const (
	// SupplierWithdrawalStatusPending 待处理。钱已从可用区扣走，挂在这张单子上。
	SupplierWithdrawalStatusPending = "pending"
	// SupplierWithdrawalStatusPaid 已打款（终态）。
	SupplierWithdrawalStatusPaid = "paid"
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

// SupplierWithdrawalCreateParams 是一次申请。
type SupplierWithdrawalCreateParams struct {
	UserID        int64
	Amount        float64
	PayoutChannel string
	PayoutAccount string
	UserNote      string
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
	Refund      bool
	ReviewNote  string
	ExternalRef string
}

// SupplierWithdrawalFilter 是提现列表的筛选条件。
type SupplierWithdrawalFilter struct {
	// UserID 0 = 不限（仅管理端会这么传）。
	UserID int64
	// Status 空 = 不限。
	Status   string
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
