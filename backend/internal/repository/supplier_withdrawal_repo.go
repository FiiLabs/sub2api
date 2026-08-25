// APEXONE-EXT: 双边市场——提现申请单的 SQL 实现。
//
// 与 supplier_credit_repo.go 分文件但同包：这里要直接用那边的流水插入与余额写回
// 助手（insertSupplierCreditLedger / querySupplierCreditBalances /
// backfillSupplierLedgerSnapshot / lockSupplierCreditAvailable），提现的每一次
// 状态变化都伴随一条流水，绕开那几个助手就等于再写一遍快照回填。
//
// 两条时序在这里落地，改动前先读 docs/two-sided-market.md §3.7：
//  1. 申请即扣款。建单、写 withdraw 流水、扣 available 在同一个事务里。
//  2. 退款只发生一次。状态机的条件更新是第一道闸，流水表 (action, request_id)
//     上的部分唯一索引是第二道——两道都在数据库里，不依赖任何进程内状态。
package repository

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type supplierWithdrawalRepository struct {
	client *dbent.Client
	cipher payoutAccountCipher
}

// NewSupplierWithdrawalRepository 构造提现仓储。
//
// 加密器在**仓储**这一层而不是 service：收款账号在 service 与 handler 里一路都是
// 明文（运营要拿它去打款，供给者要在页面上认出自己填的是哪张卡），
// 只有落库的那一瞬间需要变成密文。把加解密放在这个边界上，
// 上面所有代码都不必知道这件事，也就不存在"某条新加的读路径忘了解密"。
func NewSupplierWithdrawalRepository(client *dbent.Client, encryptor service.SecretEncryptor) service.SupplierWithdrawalRepository {
	return &supplierWithdrawalRepository{
		client: client,
		cipher: payoutAccountCipher{encryptor: encryptor},
	}
}

// ============================================================================
// SQL 常量
// ============================================================================

// supplierWithdrawalColumns 是所有读路径共用的列清单。
//
// 抽成常量而不是各写各的：这张表的读有三处（建单后回读、状态推进后回读、列表），
// 列顺序与 scanSupplierWithdrawal 的 Scan 顺序必须逐字对应，三份手抄的清单
// 迟早会有一份漏一列，而那个错误的表现是「金额显示成了单号」。
const supplierWithdrawalColumns = `
    id, user_id, amount::double precision, status,
    payout_channel, payout_account, user_note,
    ledger_id, reviewer_id, review_note, external_ref,
    created_at, updated_at, resolved_at,
    network, token_symbol, token_address, fee_amount::double precision,
    tx_hash, chain_nonce, broadcasted_at, last_error, leased_until`

// supplierWithdrawalInsertSQL 建单。
//
// 链上那四列一律显式写进 INSERT，人工渠道传 NULL / 0——不靠列默认值。
// 靠默认值意味着"这张单子是不是链上的"这个问题的答案有两个来源（代码传的值，
// 和 schema 里的 DEFAULT），而它们可以不一致。
const supplierWithdrawalInsertSQL = `
INSERT INTO supplier_withdrawals (
    user_id, amount, status, payout_channel, payout_account, user_note, created_at, updated_at,
    network, token_symbol, token_address, fee_amount
)
VALUES ($1, $2, 'pending', $3, $4, $5, NOW(), NOW(), $6, $7, $8, $9)
RETURNING ` + supplierWithdrawalColumns

// supplierWithdrawalCountPendingSQL 未决单计数。
//
// 与建单在同一个事务里跑，且跑在钱包行 FOR UPDATE **之后**：那把锁让同一个人的
// 两次并发申请必然串行，否则两个请求会各自数到 0、双双建单，把「每人一张」
// 变成一句空话。
//
// 「未决」= 还没到终态：pending / processing / failed 都算。只数 pending 的话，
// 一张卡在链上流程里的单子会把名额还给他——未决单上限想挡的恰恰是
// 「同一笔余额挂出多张单」，而 processing/failed 的钱一样还挂在单子上。
const supplierWithdrawalCountPendingSQL = `
SELECT COUNT(*) FROM supplier_withdrawals
WHERE user_id = $1 AND status IN ('pending', 'processing', 'failed')`

const supplierWithdrawalAttachLedgerSQL = `
UPDATE supplier_withdrawals SET ledger_id = $2, updated_at = NOW() WHERE id = $1`

// supplierWithdrawalLockSQL 锁住单子并读出当前状态。
//
// 先锁后判，而不是「条件 UPDATE 命中几行」：命中零行有两种完全不同的原因
// （单子不是你的 / 单子已经被处理过），而运营在后台连点两下打款按钮时，
// 必须看到「已处理」而不是「不存在」——后者会让他以为自己点错了单号，再去点一次。
const supplierWithdrawalLockSQL = `
SELECT ` + supplierWithdrawalColumns + `
FROM supplier_withdrawals
WHERE id = $1
FOR UPDATE`

// supplierWithdrawalResolveSQL 人工推进终态。
//
// $6 是「允许从 failed 出发」的开关（只有管理端传真，见 FromFailed）。
// 租约条件挡的是 worker 手里的单子：租约未到期时管理端不许动——
// 那一刻可能已有一笔交易在内存池里，这里的一次拒绝退款就是双付的另一半。
// last_error 在 paid 时清空（$7，陈旧的报错留在一张已打款的单子上只会误导人），
// rejected 时保留（它解释了这张单子为什么走到人工裁决）。清空条件用独立的
// 布尔参数而不是复用 $2 比较：$2 同时出现在赋值位（varchar）与比较位（text）
// 会让 Postgres 对它的类型推导自相矛盾（inconsistent types deduced）。
const supplierWithdrawalResolveSQL = `
UPDATE supplier_withdrawals
SET status       = $2,
    reviewer_id  = $3,
    review_note  = $4,
    external_ref = $5,
    resolved_at  = NOW(),
    updated_at   = NOW(),
    leased_until = NULL,
    last_error   = CASE WHEN $7 THEN NULL ELSE last_error END
WHERE id = $1
  AND (status = 'pending' OR ($6 AND status = 'failed'))
  AND (leased_until IS NULL OR leased_until <= NOW())
RETURNING ` + supplierWithdrawalColumns

// supplierWithdrawalRefundSQL 把钱退回可用区。
//
// 刻意不碰 history_credit / spent_credit：提现从来没有动过这两个数（见 §3.7），
// 退款自然也不该动。available 是这条路径上唯一变化的余额。
const supplierWithdrawalRefundSQL = `
UPDATE supplier_credits
SET available_credit = available_credit + $2,
    updated_at       = NOW()
WHERE user_id = $1
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierWithdrawalDeductSQL 从可用区扣钱。
//
// WHERE 里的 available_credit >= $2 是最后一道数据库兜底：调用前已经 FOR UPDATE
// 锁行并比过余额，这里再挡一次，任何情况下可用额都不允许被扣成负数。
const supplierWithdrawalDeductSQL = `
UPDATE supplier_credits
SET available_credit = available_credit - $2,
    updated_at       = NOW()
WHERE user_id = $1
  AND available_credit >= $2
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// ============================================================================
// 幂等键
// ============================================================================

// supplierWithdrawalRequestID 一张单子的扣款流水幂等键。
//
// 用单号而不是随机串：单号是这条流水唯一的自然键，于是「一张单子只扣一次钱」
// 直接由流水表上那条部分唯一索引 (action, request_id) 保证，不需要任何应用层判断。
func supplierWithdrawalRequestID(id int64) string {
	return fmt.Sprintf("withdraw:%d", id)
}

// supplierWithdrawalRevertRequestID 退款流水的幂等键。
//
// 与扣款用**不同的前缀**而不是同一个：它们的 action 不同，(action, request_id)
// 本来就不会撞——但同一个字符串出现在两条流水上，会让排查的人以为它们是一对
// 「同一笔交易的两半」，进而以为可以按 request_id 去 join。前缀不同，就不会有人这么做。
func supplierWithdrawalRevertRequestID(id int64) string {
	return fmt.Sprintf("withdraw-revert:%d", id)
}

// ============================================================================
// 写路径
// ============================================================================

func (r *supplierWithdrawalRepository) Create(ctx context.Context, params service.SupplierWithdrawalCreateParams) (*service.SupplierWithdrawal, error) {
	if params.UserID <= 0 || params.Amount <= 0 {
		return nil, service.ErrSupplierWithdrawalNotFound
	}

	var out *service.SupplierWithdrawal
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 1. 先锁钱包行。这把锁同时承担两件事：挡住并发扣款，以及让下面那次
		//    未决单计数在同一个人的多个请求之间串行。
		available, ok, err := lockSupplierCreditAvailable(txCtx, txClient, params.UserID)
		if err != nil {
			return err
		}
		if !ok || available < params.Amount {
			// 没有钱包行 = 从没赚到过钱，与余额不足是同一件事，回同一个错误。
			return service.ErrSupplierCreditInsufficient
		}

		// 2. 未决单上限。
		if params.MaxPending > 0 {
			pending, err := scanSupplierInt64(txCtx, txClient, supplierWithdrawalCountPendingSQL, params.UserID)
			if err != nil {
				return fmt.Errorf("count pending withdrawals: %w", err)
			}
			if pending >= int64(params.MaxPending) {
				return service.ErrSupplierWithdrawalTooManyPending
			}
		}

		// 3. 建单。先建单才有单号，而单号是扣款流水的幂等键。
		//
		//    收款账号在这里变成密文（见 supplier_payout_cipher.go）。加密失败就整笔
		//    失败：这一步在扣款**之前**，失败时钱还没动过，供给者重试即可。
		//    反过来把它挪到扣款之后、或者失败时降级存明文，都是拿一个人的银行卡号
		//    去换一次不必要的成功。
		sealedAccount, err := r.cipher.seal(strings.TrimSpace(params.PayoutAccount))
		if err != nil {
			return err
		}
		withdrawal, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalInsertSQL,
			params.UserID,
			params.Amount,
			strings.TrimSpace(params.PayoutChannel),
			sealedAccount,
			nullableTrimmedString(params.UserNote),
			nullableTrimmedString(params.Network),
			nullableTrimmedString(params.TokenSymbol),
			nullableTrimmedString(params.TokenAddress),
			params.FeeAmount,
		)
		if err != nil {
			return fmt.Errorf("insert supplier withdrawal: %w", err)
		}
		if withdrawal == nil {
			return fmt.Errorf("insert supplier withdrawal returned no row")
		}

		// 4. 写扣款流水。先流水后余额，与 accrue/spend 同一个顺序：
		//    流水上的唯一索引是幂等闸门，闸门先落，余额才动。
		requestID := supplierWithdrawalRequestID(withdrawal.ID)
		remark := fmt.Sprintf("withdrawal #%d", withdrawal.ID)
		ledgerID, inserted, err := insertSupplierCreditLedger(txCtx, txClient, supplierLedgerInsert{
			UserID:    params.UserID,
			Action:    service.SupplierCreditActionWithdraw,
			Amount:    params.Amount,
			RequestID: &requestID,
		})
		if err != nil {
			return err
		}
		if !inserted {
			// 单号是 BIGSERIAL，同一个单号不可能被建两次——真撞上了说明有人
			// 手工插过流水或改过序列。宁可整笔失败，也不要一张扣不到钱的单子。
			return fmt.Errorf("withdrawal ledger already exists for request %s", requestID)
		}
		if err := setSupplierLedgerRemark(txCtx, txClient, ledgerID, remark); err != nil {
			return err
		}

		// 5. 扣款。
		snapshot, err := querySupplierCreditBalances(txCtx, txClient, supplierWithdrawalDeductSQL, params.UserID, params.Amount)
		if err != nil {
			return fmt.Errorf("deduct supplier credit for withdrawal: %w", err)
		}
		if snapshot == nil {
			// 锁已经拿在手里，数据库兜底条件仍不满足：有并发路径绕过了锁。
			return service.ErrSupplierCreditInsufficient
		}
		if err := backfillSupplierLedgerSnapshot(txCtx, txClient, ledgerID, snapshot); err != nil {
			return err
		}

		// 6. 把流水号挂回单子上，供给者对账时靠它把单子与流水对上。
		if _, err := txClient.ExecContext(txCtx, supplierWithdrawalAttachLedgerSQL, withdrawal.ID, ledgerID); err != nil {
			return fmt.Errorf("attach ledger to withdrawal: %w", err)
		}
		withdrawal.LedgerID = &ledgerID
		out = withdrawal
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *supplierWithdrawalRepository) Resolve(ctx context.Context, params service.SupplierWithdrawalResolveParams) (*service.SupplierWithdrawal, error) {
	if params.ID <= 0 {
		return nil, service.ErrSupplierWithdrawalNotFound
	}

	var out *service.SupplierWithdrawal
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 1. 锁住单子并看清它现在是什么。
		current, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalLockSQL, params.ID)
		if err != nil {
			return fmt.Errorf("lock supplier withdrawal: %w", err)
		}
		if current == nil {
			return service.ErrSupplierWithdrawalNotFound
		}
		// 归属不符与不存在合并成同一个错误：区分它们等于提供一个枚举他人单号的信息面。
		if params.UserID > 0 && current.UserID != params.UserID {
			return service.ErrSupplierWithdrawalNotFound
		}
		// 三种不许动的情形，报两种错——「正在处理」与「已处理完」对点按钮的人
		// 是完全不同的两句话（见 ErrSupplierWithdrawalProcessing）。
		switch {
		case current.Status == service.SupplierWithdrawalStatusProcessing:
			return service.ErrSupplierWithdrawalProcessing
		case current.LeasedUntil != nil && current.LeasedUntil.After(time.Now()):
			// pending 但 worker 已租走：那一刻起它随时可能翻进 processing。
			return service.ErrSupplierWithdrawalProcessing
		case current.Status == service.SupplierWithdrawalStatusFailed && params.FromFailed:
			// 管理端裁决一张链上失败单：放行。
		case !current.Pending():
			return service.ErrSupplierWithdrawalNotPending
		}

		// 2. 推进状态。WHERE 里的条件与上面的判断重复，是故意的：
		//    锁虽然拿着，但条件写在语句里，读 SQL 的人不必回去确认调用方判过。
		updated, err := r.cipher.scanSupplierWithdrawalRow(txCtx, txClient, supplierWithdrawalResolveSQL,
			params.ID,
			params.Status,
			nullableInt64Arg(params.ReviewerID),
			nullableTrimmedString(params.ReviewNote),
			nullableTrimmedString(params.ExternalRef),
			params.FromFailed,
			params.Status == service.SupplierWithdrawalStatusPaid,
		)
		if err != nil {
			return fmt.Errorf("resolve supplier withdrawal: %w", err)
		}
		if updated == nil {
			return service.ErrSupplierWithdrawalNotPending
		}

		// 3. 退款。paid 不走这里——钱已经打出去了，再退一次就是凭空发钱。
		if params.Refund {
			if err := refundSupplierWithdrawal(txCtx, txClient, updated); err != nil {
				return err
			}
		}
		out = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// refundSupplierWithdrawal 把一张单子的金额退回可用区，并记一条 withdraw_revert。
//
// 流水插不进去（幂等命中）时**直接返回、不动余额**：那意味着这张单子已经退过一次，
// 再加一次钱就是凭空发钱。这一条与状态机的 pending 判断是同一条规则的两个身位，
// 留着它是因为两者失效的方式不同——状态判断挡不住「有人手工把状态改回 pending」。
func refundSupplierWithdrawal(ctx context.Context, exec supplierCreditExecer, w *service.SupplierWithdrawal) error {
	requestID := supplierWithdrawalRevertRequestID(w.ID)
	ledgerID, inserted, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:    w.UserID,
		Action:    service.SupplierCreditActionWithdrawRevert,
		Amount:    w.Amount,
		RequestID: &requestID,
	})
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	remark := fmt.Sprintf("withdrawal #%d %s", w.ID, w.Status)
	if err := setSupplierLedgerRemark(ctx, exec, ledgerID, remark); err != nil {
		return err
	}

	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierWithdrawalRefundSQL, w.UserID, w.Amount)
	if err != nil {
		return fmt.Errorf("refund supplier withdrawal: %w", err)
	}
	if snapshot == nil {
		// 钱包行没了，但单子还在。这是数据层面的不一致，不能静默——
		// 供给者的钱会凭空消失，而且没有任何一条记录说明去了哪。
		return fmt.Errorf("supplier credit wallet not found for withdrawal #%d", w.ID)
	}
	return backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, snapshot)
}

// ============================================================================
// 打款 worker 的队列面（M4）
// ============================================================================

// NewSupplierPayoutQueueRepository 给 worker 一个只含队列操作的仓储面。
//
// 与 NewSupplierWithdrawalRepository 是同一个结构体：队列操作与人工审批
// 必须看到同一张表、同一套解密——分开的只是**接口**，让提现服务拿不到
// 「跳过状态机直接标 paid」的能力（见 service.SupplierPayoutQueueRepository）。
func NewSupplierPayoutQueueRepository(client *dbent.Client, encryptor service.SecretEncryptor) service.SupplierPayoutQueueRepository {
	return &supplierWithdrawalRepository{
		client: client,
		cipher: payoutAccountCipher{encryptor: encryptor},
	}
}

// supplierWithdrawalClaimSQL 捞单 + 续租，一条语句。
//
// FOR UPDATE SKIP LOCKED：与并发的 Resolve（FOR UPDATE 锁着行）互不阻塞——
// 正被人工处理的行直接跳过，下一轮再看。只动租约不动状态，理由见接口注释。
// 按 id 升序：先来的单子先打，供给者之间不插队。
const supplierWithdrawalClaimSQL = `
UPDATE supplier_withdrawals
SET leased_until = NOW() + make_interval(secs => $2),
    updated_at   = NOW()
WHERE id IN (
    SELECT id FROM supplier_withdrawals
    WHERE network IS NOT NULL
      AND status IN ('pending', 'processing')
      AND (leased_until IS NULL OR leased_until <= NOW())
    ORDER BY id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING ` + supplierWithdrawalColumns

func (r *supplierWithdrawalRepository) ClaimPayoutDue(ctx context.Context, limit int, lease time.Duration) ([]service.SupplierWithdrawal, error) {
	if limit <= 0 || lease <= 0 {
		return nil, nil
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, supplierWithdrawalClaimSQL, limit, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim payout due: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []service.SupplierWithdrawal
	for rows.Next() {
		var item service.SupplierWithdrawal
		if err := r.cipher.scanSupplierWithdrawal(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// supplierWithdrawalBeginSQL 钉 nonce + 翻 processing，广播前的最后一道闸。
//
// chain_nonce 的条件允许两种情形：还没有 nonce（第一次），或已经是**同一个**
// nonce（上一轮广播失败后的重播）。带着另一个 nonce 说明数据被动过，
// 条件不命中、调用方放弃广播——保守的那一边。
const supplierWithdrawalBeginSQL = `
UPDATE supplier_withdrawals
SET status      = 'processing',
    chain_nonce = $2,
    updated_at  = NOW()
WHERE id = $1
  AND status IN ('pending', 'processing')
  AND (chain_nonce IS NULL OR chain_nonce = $2)`

func (r *supplierWithdrawalRepository) BeginPayout(ctx context.Context, id int64, nonce uint64) error {
	client := clientFromContext(ctx, r.client)
	res, err := client.ExecContext(ctx, supplierWithdrawalBeginSQL, id, int64(nonce))
	if err != nil {
		return fmt.Errorf("begin payout: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("begin payout rows affected: %w", err)
	}
	if affected == 0 {
		return service.ErrSupplierWithdrawalNotPending
	}
	return nil
}

// supplierWithdrawalRecordTxSQL 记哈希。broadcasted_at 只写第一次——
// 它是放弃等确认的时钟，重播刷新它会让期限永远到不了。
//
// 用 clock_timestamp() 而不是 NOW()：NOW() 在一个事务里是常量（事务开始时刻），
// 真库测试整个跑在一个回滚事务里，两次 RecordPayoutTx 会拿到同一个 NOW()，
// 于是「重播刷新了时钟」这类回归在测试里不可见。生产里两次调用各自成句，
// 两者本无差别——差别只在可测性上。
const supplierWithdrawalRecordTxSQL = `
UPDATE supplier_withdrawals
SET tx_hash        = $2,
    broadcasted_at = COALESCE(broadcasted_at, clock_timestamp()),
    updated_at     = NOW()
WHERE id = $1
  AND status = 'processing'`

func (r *supplierWithdrawalRepository) RecordPayoutTx(ctx context.Context, id int64, txHash string) error {
	trimmed := strings.TrimSpace(txHash)
	if trimmed == "" {
		return fmt.Errorf("record payout tx: empty tx hash")
	}
	client := clientFromContext(ctx, r.client)
	res, err := client.ExecContext(ctx, supplierWithdrawalRecordTxSQL, id, trimmed)
	if err != nil {
		return fmt.Errorf("record payout tx: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("record payout tx rows affected: %w", err)
	}
	if affected == 0 {
		return service.ErrSupplierWithdrawalNotPending
	}
	return nil
}

// supplierWithdrawalFinishPaidSQL 链上确认成功的终局。
//
// external_ref 与 tx_hash 写同一个值（§234 的迁移注释：前者给人对账，
// 后者给程序对账，同源不同用途）。两条 Finish 语句都钉着 status = 'processing'：
// paid/failed 只能从 processing 出发，一张没经过「钉 nonce」的单子拿不到终局。
const supplierWithdrawalFinishPaidSQL = `
UPDATE supplier_withdrawals
SET status       = 'paid',
    tx_hash      = $2,
    external_ref = $2,
    last_error   = NULL,
    leased_until = NULL,
    resolved_at  = NOW(),
    updated_at   = NOW()
WHERE id = $1
  AND status = 'processing'
RETURNING ` + supplierWithdrawalColumns

// supplierWithdrawalFinishFailedSQL 停靠到 failed 等运营。
// 不写 resolved_at——它没被 resolve，钱还挂着。
const supplierWithdrawalFinishFailedSQL = `
UPDATE supplier_withdrawals
SET status       = 'failed',
    last_error   = $2,
    leased_until = NULL,
    updated_at   = NOW()
WHERE id = $1
  AND status = 'processing'
RETURNING ` + supplierWithdrawalColumns

func (r *supplierWithdrawalRepository) FinishPayout(ctx context.Context, params service.SupplierPayoutFinishParams) (*service.SupplierWithdrawal, error) {
	client := clientFromContext(ctx, r.client)
	var (
		updated *service.SupplierWithdrawal
		err     error
	)
	switch params.Status {
	case service.SupplierWithdrawalStatusPaid:
		hash := strings.TrimSpace(params.TxHash)
		if hash == "" {
			// 一张没有哈希的 paid 单在对账里是个洞，宁可不写。
			return nil, fmt.Errorf("finish payout: paid without tx hash")
		}
		updated, err = r.cipher.scanSupplierWithdrawalRow(ctx, client, supplierWithdrawalFinishPaidSQL, params.ID, hash)
	case service.SupplierWithdrawalStatusFailed:
		updated, err = r.cipher.scanSupplierWithdrawalRow(ctx, client, supplierWithdrawalFinishFailedSQL,
			params.ID, nullableTrimmedString(params.Reason))
	default:
		return nil, fmt.Errorf("finish payout: unsupported status %q", params.Status)
	}
	if err != nil {
		return nil, fmt.Errorf("finish payout: %w", err)
	}
	if updated == nil {
		return nil, service.ErrSupplierWithdrawalNotPending
	}
	return updated, nil
}

// supplierWithdrawalReleaseLeaseSQL 交还租约（可带退避），单子留在原状态。
const supplierWithdrawalReleaseLeaseSQL = `
UPDATE supplier_withdrawals
SET leased_until = NOW() + make_interval(secs => $3),
    last_error   = $2,
    updated_at   = NOW()
WHERE id = $1
  AND status IN ('pending', 'processing')`

func (r *supplierWithdrawalRepository) ReleasePayoutLease(ctx context.Context, id int64, lastError string, retryAfter time.Duration) error {
	if retryAfter < 0 {
		retryAfter = 0
	}
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, supplierWithdrawalReleaseLeaseSQL,
		id, nullableTrimmedString(lastError), retryAfter.Seconds()); err != nil {
		return fmt.Errorf("release payout lease: %w", err)
	}
	return nil
}

// ============================================================================
// 读路径
// ============================================================================

func (r *supplierWithdrawalRepository) List(ctx context.Context, filter service.SupplierWithdrawalFilter) ([]service.SupplierWithdrawal, int64, error) {
	where, args := buildSupplierWithdrawalWhere(filter)
	client := clientFromContext(ctx, r.client)

	total, err := scanSupplierInt64(ctx, client, "SELECT COUNT(*) FROM supplier_withdrawals"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supplier withdrawals: %w", err)
	}
	if total == 0 {
		return []service.SupplierWithdrawal{}, 0, nil
	}

	page, pageSize := filter.Page, filter.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	query := "SELECT" + supplierWithdrawalColumns + " FROM supplier_withdrawals" + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)

	rows, err := client.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supplier withdrawals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.SupplierWithdrawal, 0, pageSize)
	for rows.Next() {
		var item service.SupplierWithdrawal
		if err := r.cipher.scanSupplierWithdrawal(rows, &item); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *supplierWithdrawalRepository) CountPending(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	client := clientFromContext(ctx, r.client)
	return scanSupplierInt64(ctx, client, supplierWithdrawalCountPendingSQL, userID)
}

// buildSupplierWithdrawalWhere 拼筛选条件。四个筛子都是参数化的，没有一处字符串拼值。
func buildSupplierWithdrawalWhere(filter service.SupplierWithdrawalFilter) (string, []any) {
	return buildSupplierWithdrawalWhereOn(filter, "")
}

// buildSupplierWithdrawalWhereOn 是同一个筛子，但给列名加一个表别名前缀。
//
// 导出（supplier_export_repo.go）要 LEFT JOIN users 才拿得到邮箱，而 `status`
// 这一列在 supplier_withdrawals 和 users 上**都存在**——不限定表名的话
// Postgres 直接报 ambiguous，而那是一条只在"导出且按状态筛"时才走到的路径。
//
// 拆成一个带前缀的版本而不是给导出另写一个筛子：列表与导出必须选出同一批行
// （理由见 SupplierWithdrawalFilter 的注释），共用一份实现是唯一能保证这件事的做法。
func buildSupplierWithdrawalWhereOn(filter service.SupplierWithdrawalFilter, prefix string) (string, []any) {
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if filter.UserID > 0 {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("%suser_id = $%d", prefix, len(args)))
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("%sstatus = $%d", prefix, len(args)))
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("%screated_at >= $%d", prefix, len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("%screated_at <= $%d", prefix, len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ============================================================================
// 扫描助手
// ============================================================================

// supplierWithdrawalScanner 是 *sql.Rows 上这段代码用到的那部分。
type supplierWithdrawalScanner interface {
	Scan(dest ...any) error
}

func (c payoutAccountCipher) scanSupplierWithdrawal(row supplierWithdrawalScanner, out *service.SupplierWithdrawal) error {
	var (
		userNote      *string
		ledgerID      *int64
		reviewerID    *int64
		reviewNote    *string
		externalRef   *string
		resolvedAt    *time.Time
		network       *string
		tokenSymbol   *string
		tokenAddress  *string
		txHash        *string
		chainNonce    *int64
		broadcastedAt *time.Time
		lastError     *string
		leasedUntil   *time.Time
	)
	if err := row.Scan(
		&out.ID, &out.UserID, &out.Amount, &out.Status,
		&out.PayoutChannel, &out.PayoutAccount, &userNote,
		&ledgerID, &reviewerID, &reviewNote, &externalRef,
		&out.CreatedAt, &out.UpdatedAt, &resolvedAt,
		&network, &tokenSymbol, &tokenAddress, &out.FeeAmount,
		&txHash, &chainNonce, &broadcastedAt, &lastError, &leasedUntil,
	); err != nil {
		return fmt.Errorf("scan supplier withdrawal: %w", err)
	}
	out.UserNote = userNote
	out.LedgerID = ledgerID
	out.ReviewerID = reviewerID
	out.ReviewNote = reviewNote
	out.ExternalRef = externalRef
	out.ResolvedAt = resolvedAt
	out.Network = network
	out.TokenSymbol = tokenSymbol
	out.TokenAddress = tokenAddress
	out.TxHash = txHash
	out.ChainNonce = chainNonce
	out.BroadcastedAt = broadcastedAt
	out.LastError = lastError
	out.LeasedUntil = leasedUntil

	// 解密放在这里而不是各个调用点：这是这张表**唯一**的 Scan，
	// 把它放在这一行上，等于将来任何一条新的读路径都自动解了密。
	account, err := c.open(out.PayoutAccount)
	if err != nil {
		return err
	}
	out.PayoutAccount = account
	return nil
}

// scanSupplierWithdrawalRow 跑一条最多返回一行的语句。没有行时返回 nil（不是错误），
// 让调用方自己决定「没有行」意味着什么。
func (c payoutAccountCipher) scanSupplierWithdrawalRow(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (*service.SupplierWithdrawal, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, rows.Err()
	}
	var out service.SupplierWithdrawal
	if err := c.scanSupplierWithdrawal(rows, &out); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

// scanSupplierInt64 跑一条返回单个整数的语句（COUNT 之类）。
func scanSupplierInt64(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (int64, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var out int64
	if err := rows.Scan(&out); err != nil {
		return 0, err
	}
	return out, rows.Err()
}

// nullableTrimmedString 空白串存 NULL 而不是空字符串。
//
// 两者在 Postgres 里是不同的值，而在「这条备注有没有填」这个问题上必须是同一个答案——
// 否则前端要同时判 null 和 ""，总有一处会漏。
func nullableTrimmedString(v string) any {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// setSupplierLedgerRemark 给刚插入的流水写备注。
func setSupplierLedgerRemark(ctx context.Context, exec supplierCreditExecer, ledgerID int64, remark string) error {
	if strings.TrimSpace(remark) == "" {
		return nil
	}
	if _, err := exec.ExecContext(ctx, supplierCreditLedgerRemarkSQL, ledgerID, remark); err != nil {
		return fmt.Errorf("set supplier ledger remark: %w", err)
	}
	return nil
}

func (r *supplierWithdrawalRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	// 显式 READ COMMITTED：这套资金事务的正确性建立在「锁 + 条件更新 + 唯一索引」
	// 上，不依赖更高的隔离级；而依赖服务器默认值曾真实炸过——一台
	// default_transaction_isolation=serializable 的库上，浏览器并发打开控制台
	// 就让两个碰同一行钱包的事务互相 40001（could not serialize access），
	// 普通用户看到 500。隔离级是这段代码的设计前提，就该由这段代码自己声明。
	tx, err := r.client.BeginTx(ctx, &stdsql.TxOptions{Isolation: stdsql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin supplier withdrawal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit supplier withdrawal transaction: %w", err)
	}
	return nil
}
