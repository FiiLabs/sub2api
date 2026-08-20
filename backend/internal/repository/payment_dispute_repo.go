// APEXONE-EXT: 双边市场——支付争议台账的 SQL 实现（迁移 231）。
//
// 三条语句，每条对应 service.PaymentDisputeStore 的一个方法。手写 SQL 而不是走 ent：
// payment_disputes 是扩展层新表，不进 ent schema（理由与 225 / 226 / 228 / 229 / 230 相同——
// 进了 schema 就意味着每次上游改 ent 生成器我们都要重新生成一遍）。
//
// **刻意没有 sqlmock 单测**，与本包其他仓储不同。这里要证的每一条性质
// （ON CONFLICT 推断得到哪个索引、重复 upsert 碰不碰得到 settled_at、
// 并发占坑只有一个赢）都在 Postgres 的语义里，sqlmock 只会把写好的语句原样还回来，
// 连它是否合法都不知道——那种测试会在这张表上给出一种"测过了"的假象。
// 覆盖全在 payment_dispute_repo_integration_test.go，CI 两个标签都跑。
package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type paymentDisputeRepository struct {
	client *dbent.Client
}

// NewPaymentDisputeRepository 构造争议台账仓储。
func NewPaymentDisputeRepository(client *dbent.Client) service.PaymentDisputeStore {
	return &paymentDisputeRepository{client: client}
}

// paymentDisputeUpsertSQL 落一条争议推送。
//
// # ON CONFLICT 里每一条 COALESCE / CASE 都在防同一件事：后到的推送把先前的事实抹掉
//
//   - order_id / user_id / trade_no / out_trade_no 用 COALESCE 与 CASE 只补不覆盖。
//     争议关闭那条推送与创建那条带的字段未必一样全（payment_intent 展开与否会变），
//     直接 EXCLUDED 覆盖的话，一条本来对上了订单的记录会在几十天后变成对不上。
//   - basis_amount 只在**还没结算**时更新。结算后它是那次追回实际用的基数，
//     是对账凭据；让 closed 那条推送重算一遍，等于把凭据改成一个从没被用过的数。
//   - settled_at 与四个结算金额**完全不在 SET 里**。它们只由 ClaimForSettlement 与
//     RecordSettlement 写。这是"副作用只跑一次"这条性质在 SQL 层的保证：
//     无论 upsert 被调多少次，那道闸都不会被重新打开。
//   - resolved_at 用 COALESCE 保留第一次关闭的时刻。
const paymentDisputeUpsertSQL = `
INSERT INTO payment_disputes (
    dispute_id, provider_key, trade_no, order_id, out_trade_no, user_id,
    status, reason, dispute_amount, currency, basis_amount, raw_data,
    resolved_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
ON CONFLICT (dispute_id) DO UPDATE SET
    status         = EXCLUDED.status,
    reason         = EXCLUDED.reason,
    raw_data       = EXCLUDED.raw_data,
    dispute_amount = EXCLUDED.dispute_amount,
    currency       = EXCLUDED.currency,
    provider_key   = EXCLUDED.provider_key,
    trade_no       = CASE WHEN payment_disputes.trade_no = '' THEN EXCLUDED.trade_no ELSE payment_disputes.trade_no END,
    out_trade_no   = CASE WHEN payment_disputes.out_trade_no = '' THEN EXCLUDED.out_trade_no ELSE payment_disputes.out_trade_no END,
    order_id       = COALESCE(payment_disputes.order_id, EXCLUDED.order_id),
    user_id        = COALESCE(payment_disputes.user_id, EXCLUDED.user_id),
    basis_amount   = CASE WHEN payment_disputes.settled_at IS NULL THEN EXCLUDED.basis_amount ELSE payment_disputes.basis_amount END,
    resolved_at    = COALESCE(payment_disputes.resolved_at, EXCLUDED.resolved_at),
    updated_at     = NOW()
RETURNING id, dispute_id, provider_key, trade_no, order_id, out_trade_no, user_id,
          status, reason, settled_at, resolved_at`

// paymentDisputeClaimSQL 原子占坑。
//
// `settled_at IS NULL` 这个条件是整个功能的幂等保证：同一个争议 Stripe 会推五次，
// 而这条路径的副作用是扣钱。没有它，一次拒付会被扣五遍。
// 用 RETURNING 而不是看 RowsAffected：两者在这里等价，但 RETURNING 让"占到坑"
// 与"读回那一行"是同一次往返，将来要在占坑时顺手读点什么不必再改结构。
const paymentDisputeClaimSQL = `
UPDATE payment_disputes
SET settled_at = NOW(), updated_at = NOW()
WHERE dispute_id = $1 AND settled_at IS NULL
RETURNING id`

// paymentDisputeRecordSettlementSQL 回填四个结算金额。
//
// 刻意不带 `settled_at IS NULL` 之类的守卫：调用它的前提就是刚刚占坑成功，
// 而占坑那一步已经把 settled_at 写非空了。这里再判一次只会让它永远不生效。
const paymentDisputeRecordSettlementSQL = `
UPDATE payment_disputes
SET balance_deducted = $2,
    clawed_credit    = $3,
    clawed_basis     = $4,
    uncovered_basis  = $5,
    updated_at       = NOW()
WHERE dispute_id = $1`

func (r *paymentDisputeRepository) Upsert(ctx context.Context, params service.PaymentDisputeUpsert) (*service.PaymentDisputeRecord, error) {
	disputeID := strings.TrimSpace(params.DisputeID)
	if disputeID == "" {
		// 没有幂等键的行落进这张表比不落更糟：它会让"这个争议处理过吗"
		// 这个问题从此没有答案。
		return nil, fmt.Errorf("upsert payment dispute requires a dispute id")
	}

	rows, err := r.client.QueryContext(ctx, paymentDisputeUpsertSQL,
		disputeID,
		strings.TrimSpace(params.ProviderKey),
		strings.TrimSpace(params.TradeNo),
		nullableInt64Arg(params.OrderID),
		strings.TrimSpace(params.OutTradeNo),
		nullableInt64Arg(params.UserID),
		params.Status,
		params.Reason,
		params.DisputeAmount,
		params.Currency,
		params.BasisAmount,
		params.RawData,
		paymentDisputeResolvedAt(params.Status),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert payment dispute: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("upsert payment dispute: %w", err)
		}
		// INSERT ... ON CONFLICT DO UPDATE ... RETURNING 永远回一行。走到这里
		// 说明驱动层出了别的问题，当成错误报出去——与 COUNT 那个「没有行返回 0」
		// 的判断方向相反，因为这里拿不到行就等于不知道 settled_at，
		// 而不知道 settled_at 就不能安全地决定副作用跑不跑。
		return nil, fmt.Errorf("upsert payment dispute returned no row")
	}
	record, err := scanPaymentDispute(rows)
	if err != nil {
		return nil, err
	}
	return record, rows.Err()
}

func (r *paymentDisputeRepository) ClaimForSettlement(ctx context.Context, disputeID string) (bool, error) {
	id := strings.TrimSpace(disputeID)
	if id == "" {
		return false, fmt.Errorf("claim payment dispute requires a dispute id")
	}
	rows, err := r.client.QueryContext(ctx, paymentDisputeClaimSQL, id)
	if err != nil {
		return false, fmt.Errorf("claim payment dispute: %w", err)
	}
	defer func() { _ = rows.Close() }()

	claimed := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("claim payment dispute: %w", err)
	}
	return claimed, nil
}

func (r *paymentDisputeRepository) RecordSettlement(ctx context.Context, settlement service.PaymentDisputeSettlement) error {
	id := strings.TrimSpace(settlement.DisputeID)
	if id == "" {
		return fmt.Errorf("record payment dispute settlement requires a dispute id")
	}
	_, err := r.client.ExecContext(ctx, paymentDisputeRecordSettlementSQL,
		id,
		settlement.BalanceDeducted,
		settlement.ClawedCredit,
		settlement.ClawedBasis,
		settlement.UncoveredBasis,
	)
	if err != nil {
		return fmt.Errorf("record payment dispute settlement: %w", err)
	}
	return nil
}

// paymentDisputeResolvedAt 只有终态才带关闭时刻。
//
// 用服务端的时间而不是 SQL 里的 NOW()：这样"什么时候关闭的"与 RETURNING 回来的
// 值在同一次调用里是同一个时刻，测试不必为一个由数据库生成的时间放宽断言。
func paymentDisputeResolvedAt(status string) any {
	switch status {
	case "won", "lost":
		return time.Now()
	default:
		return nil
	}
}

// paymentDisputeRowScanner 是 *sql.Rows 里这段代码用到的那一小块。
type paymentDisputeRowScanner interface {
	Scan(dest ...any) error
}

func scanPaymentDispute(rows paymentDisputeRowScanner) (*service.PaymentDisputeRecord, error) {
	var (
		record     service.PaymentDisputeRecord
		orderID    *int64
		userID     *int64
		settledAt  *time.Time
		resolvedAt *time.Time
	)
	if err := rows.Scan(
		&record.ID,
		&record.DisputeID,
		&record.ProviderKey,
		&record.TradeNo,
		&orderID,
		&record.OutTradeNo,
		&userID,
		&record.Status,
		&record.Reason,
		&settledAt,
		&resolvedAt,
	); err != nil {
		return nil, fmt.Errorf("scan payment dispute: %w", err)
	}
	record.OrderID = orderID
	record.UserID = userID
	record.SettledAt = settledAt
	record.ResolvedAt = resolvedAt
	return &record, nil
}
