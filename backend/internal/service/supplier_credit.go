// APEXONE-EXT: 双边市场——供给者「赚取钱包」的领域类型与仓储接口。
//
// 供给者把自有订阅账号挂进供给池，别人用掉的量按分成比例入账为 credit。credit 有两个
// 出口：抵扣自己发起的请求（spend），或提现（withdraw）。入账先进冻结区，过了冻结窗
// 才搬进可用区（thaw）——冻结窗必须 ≥ 支付通道的拒付窗，否则「已释放 = 拒付安全」这
// 条不变量不成立。
//
// 本文件只放类型与接口；SQL 实现在 internal/repository/supplier_credit_repo.go。
package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrSupplierWalletNotFound 供给者尚未有钱包行（从未入过账）。
	ErrSupplierWalletNotFound = infraerrors.NotFound("SUPPLIER_WALLET_NOT_FOUND", "supplier credit wallet not found")
	// ErrSupplierCreditInsufficient 可用 credit 不足以覆盖本次扣减。
	ErrSupplierCreditInsufficient = infraerrors.BadRequest("SUPPLIER_CREDIT_INSUFFICIENT", "insufficient supplier credit")
)

// 流水动作。金额一律存正数，方向由动作决定，对账时按动作分组求和即可。
//
// 注意 SupplierCreditActionThaw 是钱包内部搬运（frozen → available），统计「供给者赚了
// 多少」时必须排除，否则会与 accrue 重复计数。
const (
	SupplierCreditActionAccrue   = "accrue"
	SupplierCreditActionSpend    = "spend"
	SupplierCreditActionThaw     = "thaw"
	SupplierCreditActionClawback = "clawback"
	SupplierCreditActionWithdraw = "withdraw"
	// SupplierCreditActionWithdrawRevert 提现被拒绝/被撤回时把钱退回可用区。
	//
	// 刻意不做成「把那条 withdraw 流水删掉」：流水是追加式的，删一行等于让
	// 「我的钱去哪了」这个问题在某些时刻没有答案。一进一出两条记录，
	// 供给者看到的是一段完整的经过，而不是一次莫名其妙的余额波动。
	SupplierCreditActionWithdrawRevert = "withdraw_revert"
)

// SupplierCreditSummary 是供给者钱包的余额快照。
type SupplierCreditSummary struct {
	UserID int64 `json:"user_id"`
	// AvailableCredit 已解冻，可消费/可提现。
	AvailableCredit float64 `json:"available_credit"`
	// FrozenCredit 仍在冻结窗内。
	FrozenCredit float64 `json:"frozen_credit"`
	// HistoryCredit 累计入账（含冻结部分），只增不减。
	HistoryCredit float64 `json:"history_credit"`
	// SpentCredit 累计消费，只增不减。
	SpentCredit float64   `json:"spent_credit"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SupplierAccrueParams 描述一笔供给分成入账。
//
// 入账金额刻意不由调用方传入，而是由 BasisAmount × ShareRatio 现算——让流水里的
// 「基数 × 比例 = 金额」三要素天然自洽，供给者拿快照就能自行核对，无需信任服务端算术。
type SupplierAccrueParams struct {
	// SupplierUserID 收款的供给者，取自 account.owner_user_id。
	SupplierUserID int64
	// RequestID 幂等键，取自 usage_log.request_id。空串表示无法幂等，调用方必须拒绝入账。
	RequestID string
	// AccountID 产出这笔入账的供给账号（供给者仪表盘按账号看收益）。
	AccountID *int64
	// ConsumerUserID 消费方。
	ConsumerUserID *int64
	// BasisAmount 分成基数 = 消费者实付金额（不是官方价）。
	BasisAmount float64
	// ShareRatio 分成比例，如 0.70。
	ShareRatio float64
	// FreezeHours 冻结小时数；<= 0 表示直接进可用区（仅用于测试或运营特例）。
	FreezeHours int
}

// SupplierClawbackParams 描述一次拒付/退款追回。
//
// 与 SupplierAccrueParams 同一个脾气：调用方传的是**基数**（退给消费者的钱），
// 不是要收回多少 credit。收回多少由被撤销的那几条入账各自带的 amount 决定，
// 服务端现算——否则退款侧就得复刻一遍分成算术，两处算法一漂移就是钱算错。
type SupplierClawbackParams struct {
	// ConsumerUserID 被退款的消费者。追回只找 source_user_id 命中他的入账，
	// 不会波及同一个供给者从别人那里赚到的钱。
	ConsumerUserID int64
	// BasisAmount 需要追回的消费基数 = 本次退款金额，与 accrue 的 BasisAmount 同口径
	// （都是 users.balance 的单位）。
	BasisAmount float64
	// Reason 写进流水 remark，是运营事后回答「这笔钱为什么被收回」的唯一线索。
	Reason string
	// MaxEntries 单次最多撤销几条入账；<= 0 用实现默认上限。
	MaxEntries int
}

// SupplierClawbackResult 一次追回的结果。全部用于日志与运营核对，不参与任何判定。
type SupplierClawbackResult struct {
	// ReversedCredit 实际从冻结区收回的 credit 总额。
	ReversedCredit float64
	// ReversedBasis 被撤销的入账所对应的消费基数之和。
	ReversedBasis float64
	// UncoveredBasis = max(0, BasisAmount - ReversedBasis)。
	// 大于 0 意味着这部分亏损平台自吃：对应的分成已经解冻（不变量 2 认了这个结果），
	// 或者冻结区里根本没有这个消费者产生的入账。它是「冻结窗配短了」的直接证据。
	UncoveredBasis float64
	// Entries 撤销了几条入账，Suppliers 涉及几个供给者。
	Entries   int
	Suppliers int
}

// SupplierCreditLedgerEntry 是一条钱包流水，自带审计快照。
type SupplierCreditLedgerEntry struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"user_id"`
	Action         string     `json:"action"`
	Amount         float64    `json:"amount"`
	RequestID      *string    `json:"request_id,omitempty"`
	AccountID      *int64     `json:"account_id,omitempty"`
	SourceUserID   *int64     `json:"source_user_id,omitempty"`
	BasisAmount    *float64   `json:"basis_amount,omitempty"`
	ShareRatio     *float64   `json:"share_ratio,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	AvailableAfter *float64   `json:"available_after,omitempty"`
	FrozenAfter    *float64   `json:"frozen_after,omitempty"`
	HistoryAfter   *float64   `json:"history_after,omitempty"`
	Remark         *string    `json:"remark,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// SupplierCreditLedgerFilter 是供给者账单页的筛选条件。
type SupplierCreditLedgerFilter struct {
	UserID    int64
	Action    string
	AccountID *int64
	StartAt   *time.Time
	EndAt     *time.Time
	Page      int
	PageSize  int
}

// SupplierCreditRepository 是赚取钱包的持久化接口。
//
// 实现全部是手写 SQL：写入点在计费事务内部（applyUsageBillingEffects），ent 生成的
// builder 挤不进那条热路径；且本仓已有同形态先例（user_affiliates + 其 ledger），
// 照抄让两个钱包在运维与对账上是同一种东西。
type SupplierCreditRepository interface {
	// EnsureWallet 保证钱包行存在并返回当前余额。
	EnsureWallet(ctx context.Context, userID int64) (*SupplierCreditSummary, error)
	// GetWallet 读余额；无钱包行返回 ErrSupplierWalletNotFound。
	GetWallet(ctx context.Context, userID int64) (*SupplierCreditSummary, error)
	// Accrue 入账一笔分成。返回 false 表示该 RequestID 已入过账（幂等命中），不是错误。
	Accrue(ctx context.Context, params SupplierAccrueParams) (bool, error)
	// Spend 从可用区扣减。返回 false 表示余额不足，调用方应回退到 users.balance。
	Spend(ctx context.Context, userID int64, amount float64, requestID string) (bool, error)
	// Clawback 撤销该消费者名下仍在冻结区的入账，直到覆盖 BasisAmount。
	// 只动冻结区——已解冻的钱按不变量 2 就是拒付安全的，追回它等于毁约。
	Clawback(ctx context.Context, params SupplierClawbackParams) (*SupplierClawbackResult, error)
	// ThawMatured 把某个供给者已到期的冻结额搬进可用区，返回搬运金额。
	ThawMatured(ctx context.Context, userID int64) (float64, error)
	// ThawAllMaturedUsers 扫描所有有到期冻结额的供给者并逐个解冻，
	// 返回处理人数与总解冻金额。limit <= 0 时用实现的默认上限。
	ThawAllMaturedUsers(ctx context.Context, limit int) (int, float64, error)
	// ListLedger 分页读流水。
	ListLedger(ctx context.Context, filter SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error)
}
