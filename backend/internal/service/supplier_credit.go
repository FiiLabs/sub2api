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
	// ThawMatured 把某个供给者已到期的冻结额搬进可用区，返回搬运金额。
	ThawMatured(ctx context.Context, userID int64) (float64, error)
	// ThawAllMaturedUsers 扫描所有有到期冻结额的供给者并逐个解冻，
	// 返回处理人数与总解冻金额。limit <= 0 时用实现的默认上限。
	ThawAllMaturedUsers(ctx context.Context, limit int) (int, float64, error)
	// ListLedger 分页读流水。
	ListLedger(ctx context.Context, filter SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error)
}
