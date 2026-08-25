// APEXONE-EXT: 双边市场——供给者赚取钱包的 SQL 实现。
//
// 结构照抄 affiliate_repo.go（user_affiliates + user_affiliate_ledger）：钱包一行余额 +
// 追加式流水，冻结/解冻语义完全一致。刻意不发明第二套写法，让两个钱包在排障和对账上
// 是同一种东西。
//
// 关键约束：accrue/spend 的真正调用点在 applyUsageBillingEffects 的计费事务内部。因此
// 所有写操作都拆成「接受 executor 的包级 *Tx 函数」+「自己开事务的方法」两层——core
// 侧只需要一行函数调用，不需要知道钱包的任何细节。
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierCreditThawUsersDefaultLimit 单轮解冻任务最多处理多少个供给者。
// 解冻是补偿型任务，跑不完下一轮继续；限流是为了不让一次扫描长时间占住连接。
const supplierCreditThawUsersDefaultLimit = 500

// supplierCreditClawbackDefaultMaxEntries 单次追回最多撤销多少条入账。
//
// 有上限不是因为撤多了不对，而是因为一次退款的追回跑在退款事务之外的一个短事务里，
// 而候选行是 FOR UPDATE 锁住的——不设界的话，一个刷了几十万次请求的消费者退一次款
// 就能把整张流水表的一大片锁上，把计费热路径一起拖住。
// 撤不完的部分如实计入 UncoveredBasis，运营看得见。
const supplierCreditClawbackDefaultMaxEntries = 500

// supplierClawbackRemarkMaxLen 截断阈值。remark 是给人看的，不是日志转储。
const supplierClawbackRemarkMaxLen = 200

// supplierCreditExecer 是 *dbent.Client 与事务客户端的公共子集，
// 让同一段 SQL 既能在独立事务里跑，也能被计费事务复用。
type supplierCreditExecer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type supplierCreditRepository struct {
	client *dbent.Client
}

// NewSupplierCreditRepository 构造赚取钱包仓储。
func NewSupplierCreditRepository(client *dbent.Client) service.SupplierCreditRepository {
	return &supplierCreditRepository{client: client}
}

// ============================================================================
// SQL 常量：抽出来是为了让单测能直接对语句做断言（幂等冲突子句、冻结判定等），
// 不必起数据库也能守住不变量。
// ============================================================================

// supplierCreditLedgerInsertSQL 幂等插入一条带 request_id 的流水。
//
// ON CONFLICT 的推断子句必须与迁移 225 的部分唯一索引谓词逐字一致，
// 否则 Postgres 找不到可用索引会直接报错。
// 冲突时不返回行——「没拿到 id」就是「这笔已经记过账」的信号。
const supplierCreditLedgerInsertSQL = `
INSERT INTO supplier_credit_ledger (
    user_id, action, amount, request_id, account_id, source_user_id,
    basis_amount, share_ratio, frozen_until, created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8,
    CASE WHEN $9::int > 0 THEN NOW() + make_interval(hours => $9::int) ELSE NULL END,
    NOW(), NOW()
)
ON CONFLICT (action, request_id) WHERE request_id IS NOT NULL
DO NOTHING
RETURNING id`

// supplierCreditWalletUpsertSQL 加钱：钱包行不存在就建，存在就累加。
// 用 upsert 同时承担 EnsureWallet 的职责，省掉一次「先查后插」的竞态。
const supplierCreditWalletUpsertSQL = `
INSERT INTO supplier_credits (user_id, available_credit, frozen_credit, history_credit, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (user_id) DO UPDATE
SET available_credit = supplier_credits.available_credit + EXCLUDED.available_credit,
    frozen_credit    = supplier_credits.frozen_credit + EXCLUDED.frozen_credit,
    history_credit   = supplier_credits.history_credit + EXCLUDED.history_credit,
    updated_at       = NOW()
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierCreditSpendSQL 从可用区扣钱。
// 调用前已经 FOR UPDATE 锁行并校验过余额，这里的 WHERE 只是最后一道数据库兜底：
// 任何情况下都不允许可用额被扣成负数。
const supplierCreditSpendSQL = `
UPDATE supplier_credits
SET available_credit = available_credit - $2,
    spent_credit     = spent_credit + $2,
    updated_at       = NOW()
WHERE user_id = $1
  AND available_credit >= $2
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierCreditThawMaturedSQL 把到期的冻结流水标记为已解冻，并汇总解冻金额。
// 更新与汇总放在同一条 CTE 里，避免「先查后改」窗口期被并发任务重复搬运。
const supplierCreditThawMaturedSQL = `
WITH matured AS (
    UPDATE supplier_credit_ledger
    SET frozen_until = NULL, updated_at = NOW()
    WHERE user_id = $1
      AND frozen_until IS NOT NULL
      AND frozen_until <= NOW()
    RETURNING amount
)
SELECT COALESCE(SUM(amount), 0)::double precision FROM matured`

// supplierCreditMoveThawedSQL 把解冻金额从冻结区搬到可用区。
// GREATEST(..., 0) 防御历史脏数据把冻结额减成负数。
const supplierCreditMoveThawedSQL = `
UPDATE supplier_credits
SET available_credit = available_credit + $2,
    frozen_credit    = GREATEST(frozen_credit - $2, 0),
    updated_at       = NOW()
WHERE user_id = $1
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierCreditClawbackCandidatesSQL 找出「还追得回来」的入账。
//
// 四个筛选条件各挡一类错误的追回：
//   - action = 'accrue'：只撤入账，thaw/spend 是钱包内部搬运与消费，撤它们没有意义。
//   - source_user_id = $1：只找这个消费者产生的入账。供给者从别人那里赚的钱与本次退款无关。
//   - frozen_until IS NOT NULL：只动冻结区。这是不变量 2 的实现——已解冻的钱平台自吃，
//     追回它等于事后毁约。注意 thaw 是把到期行的 frozen_until 置 NULL，
//     所以这个条件同时也保证了「这笔钱此刻确实还躺在 frozen_credit 里」。
//   - NOT EXISTS 那一段：一条入账最多被撤一次。它与流水表的部分唯一索引
//     (action, request_id) 是同一条规则的两个身位——索引是跨事务的durable闸门，
//     这里是选行时的早筛，少插一次注定冲突的流水。
//
// ORDER BY id DESC：退款退的是最近一次充值，先撤最近的消费最贴近真实资金流向；
// 而且越近的入账越可能还在冻结区，先撤它们能把 UncoveredBasis 压到最小。
//
// FOR UPDATE OF a：把候选行锁住，挡住并发的第二次退款和后台解冻任务——
// 否则解冻任务可能在我们判定「还冻着」之后、扣钱之前把同一笔钱搬进可用区。
const supplierCreditClawbackCandidatesSQL = `
SELECT a.id,
       a.user_id,
       a.request_id,
       a.account_id,
       a.amount::double precision,
       COALESCE(a.basis_amount, a.amount)::double precision,
       a.share_ratio::double precision
FROM supplier_credit_ledger a
WHERE a.action = 'accrue'
  AND a.source_user_id = $1
  AND a.request_id IS NOT NULL
  AND a.frozen_until IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM supplier_credit_ledger c
      WHERE c.action = 'clawback'
        AND c.request_id = a.request_id
  )
ORDER BY a.id DESC
LIMIT $2
FOR UPDATE OF a`

// supplierCreditClawbackMarkSQL 把被撤销的入账摘出冻结队列。
//
// 这条 UPDATE 不是记账，是**止损**：钱已经从 frozen_credit 里扣走了，若这一行的
// frozen_until 还留着，解冻任务下一轮会把它当成到期入账，再往可用区搬一次同样的金额
// （GREATEST 只保住 frozen 不为负，available 会凭空多出这笔钱）。
//
// 顺手把 remark 写上：accrue 行从不写 remark，这里占用它，让「这条入账为什么消失了」
// 在流水页上一眼可见，而不必去 join 那条 clawback 行。
const supplierCreditClawbackMarkSQL = `
UPDATE supplier_credit_ledger
SET frozen_until = NULL,
    remark       = $2,
    updated_at   = NOW()
WHERE id = $1
  AND action = 'accrue'
  AND frozen_until IS NOT NULL`

// supplierCreditClawbackWalletSQL 从冻结区扣回。
//
// 刻意不动 history_credit：`history_credit = SUM(accrue.amount)` 这条恒等式是全部对账的
// 锚点，一旦追回也去减它，历史累计就再也无法从流水重算出来。供给者看到的「净收益」
// 应当是 history - SUM(clawback.amount)，两条流水都在表里，减法留给读侧。
const supplierCreditClawbackWalletSQL = `
UPDATE supplier_credits
SET frozen_credit = GREATEST(frozen_credit - $2, 0),
    updated_at    = NOW()
WHERE user_id = $1
RETURNING available_credit::double precision,
          frozen_credit::double precision,
          history_credit::double precision`

// supplierCreditLedgerRemarkSQL 单独写 remark。
// 不并进共用的插入语句是为了不给 accrue/spend/thaw 三条热路径多加一个永远传 NULL 的参数。
const supplierCreditLedgerRemarkSQL = `
UPDATE supplier_credit_ledger
SET remark = $2, updated_at = NOW()
WHERE id = $1`

const supplierCreditWalletSelectSQL = `
SELECT user_id,
       available_credit::double precision,
       frozen_credit::double precision,
       history_credit::double precision,
       spent_credit::double precision,
       created_at,
       updated_at
FROM supplier_credits
WHERE user_id = $1
LIMIT 1`

// supplierCreditPendingThawUsersSQL 找出有到期冻结额的供给者。
const supplierCreditPendingThawUsersSQL = `
SELECT DISTINCT user_id
FROM supplier_credit_ledger
WHERE frozen_until IS NOT NULL
  AND frozen_until <= NOW()
ORDER BY user_id
LIMIT $1`

// ============================================================================
// 包级事务函数：供计费事务内联调用
// ============================================================================

// accrueSupplierCreditTx 在给定 executor（通常是计费事务的客户端）里入账一笔分成。
//
// 返回 false 表示本次没有产生入账，且这不是错误：要么参数不构成一笔有效分成
// （无供给者、无幂等键、金额非正），要么该 RequestID 已经入过账。计费侧据此静默跳过。
//
// 写入顺序刻意是「先流水后余额」：流水的部分唯一索引是幂等闸门，闸门先落，
// 余额才动；重放时闸门直接挡下，余额一分不会重复加。
func accrueSupplierCreditTx(ctx context.Context, exec supplierCreditExecer, params service.SupplierAccrueParams) (bool, error) {
	requestID := strings.TrimSpace(params.RequestID)
	if params.SupplierUserID <= 0 || requestID == "" {
		return false, nil
	}
	// 金额由基数×比例现算，保证流水里三要素自洽，供给者可自行核对。
	amount := params.BasisAmount * params.ShareRatio
	if amount <= 0 {
		return false, nil
	}
	freezeHours := params.FreezeHours
	if freezeHours < 0 {
		freezeHours = 0
	}

	ledgerID, inserted, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:       params.SupplierUserID,
		Action:       service.SupplierCreditActionAccrue,
		Amount:       amount,
		RequestID:    &requestID,
		AccountID:    params.AccountID,
		SourceUserID: params.ConsumerUserID,
		BasisAmount:  &params.BasisAmount,
		ShareRatio:   &params.ShareRatio,
		FreezeHours:  freezeHours,
	})
	if err != nil {
		return false, err
	}
	if !inserted {
		// 幂等命中：同一个 request_id 已经入过账。
		return false, nil
	}

	availableDelta, frozenDelta := amount, 0.0
	if freezeHours > 0 {
		availableDelta, frozenDelta = 0.0, amount
	}
	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierCreditWalletUpsertSQL,
		params.SupplierUserID, availableDelta, frozenDelta, amount)
	if err != nil {
		return false, fmt.Errorf("upsert supplier credit wallet: %w", err)
	}
	if err := backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, snapshot); err != nil {
		return false, err
	}
	return true, nil
}

// spendSupplierCreditTx 从可用区扣减。
//
// 返回 false 表示钱包不存在或可用额不足——不是错误，计费侧据此回退到 users.balance。
// 「同一请求只扣一处」由调用方在计费事务里保证：这里返回 true 就意味着扣款已完成，
// 调用方不得再扣 balance。
func spendSupplierCreditTx(ctx context.Context, exec supplierCreditExecer, userID int64, amount float64, requestID string) (bool, error) {
	trimmedRequestID := strings.TrimSpace(requestID)
	if userID <= 0 || amount <= 0 || trimmedRequestID == "" {
		return false, nil
	}

	// 先锁行再判余额：锁把「查—扣」变成事务内的原子操作，
	// 否则并发请求会各自读到足额、双双扣款。
	available, ok, err := lockSupplierCreditAvailable(ctx, exec, userID)
	if err != nil {
		return false, err
	}
	if !ok || available < amount {
		return false, nil
	}

	ledgerID, inserted, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:    userID,
		Action:    service.SupplierCreditActionSpend,
		Amount:    amount,
		RequestID: &trimmedRequestID,
	})
	if err != nil {
		return false, err
	}
	if !inserted {
		// 幂等命中：这次请求已经扣过了。返回 true，让调用方照旧不去扣 balance。
		return true, nil
	}

	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierCreditSpendSQL, userID, amount)
	if err != nil {
		return false, fmt.Errorf("spend supplier credit: %w", err)
	}
	if snapshot == nil {
		// 锁已经拿在手里，数据库兜底条件仍不满足，说明有并发路径绕过了锁。
		// 宁可让整个计费事务失败，也不留下一条对不上账的流水。
		return false, service.ErrSupplierCreditInsufficient
	}
	if err := backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, snapshot); err != nil {
		return false, err
	}
	return true, nil
}

// clawbackSupplierCreditTx 撤销该消费者名下仍在冻结区的入账，直到覆盖 params.BasisAmount。
//
// 撤销以**整条入账**为单位，不做按比例拆分：一条 accrue 对一条 clawback，
// 幂等键（request_id）因此天然成对，对账时两行一减就归零。代价是最后一条会略微超额
// （上限是单次请求的分成，量级在分之内），结果如实记在 ReversedBasis 里。
// 反过来「宁可少撤」的话，超出的那部分亏损会一直挂在平台账上且没有任何记录。
func clawbackSupplierCreditTx(ctx context.Context, exec supplierCreditExecer, params service.SupplierClawbackParams) (*service.SupplierClawbackResult, error) {
	result := &service.SupplierClawbackResult{}
	if params.ConsumerUserID <= 0 || params.BasisAmount <= 0 {
		return result, nil
	}
	maxEntries := params.MaxEntries
	if maxEntries <= 0 {
		maxEntries = supplierCreditClawbackDefaultMaxEntries
	}

	candidates, err := listSupplierClawbackCandidates(ctx, exec, params.ConsumerUserID, maxEntries)
	if err != nil {
		return nil, err
	}

	suppliers := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if result.ReversedBasis >= params.BasisAmount {
			break
		}
		reversed, err := reverseSupplierAccrual(ctx, exec, candidate, params)
		if err != nil {
			return nil, err
		}
		if !reversed {
			// 这一条已经被别的追回撤过了（跨事务重放）。跳过，不计数。
			continue
		}
		result.ReversedCredit += candidate.Amount
		result.ReversedBasis += candidate.Basis
		result.Entries++
		suppliers[candidate.SupplierUserID] = struct{}{}
	}
	result.Suppliers = len(suppliers)
	if uncovered := params.BasisAmount - result.ReversedBasis; uncovered > 0 {
		result.UncoveredBasis = uncovered
	}
	return result, nil
}

// reverseSupplierAccrual 撤销一条入账。返回 false = 这条已被撤过，不是错误。
//
// 三步顺序不可交换：流水（幂等闸门）→ 摘出冻结队列（止损）→ 扣钱。
// 闸门在最前面，重放时一分钱都动不了；止损在扣钱前面，因为「摘不掉」意味着这行的状态
// 与我们刚读到的不一致，此时必须让整个事务回滚，而不是扣完钱再发现搬运队列里还留着它。
func reverseSupplierAccrual(ctx context.Context, exec supplierCreditExecer, candidate supplierClawbackCandidate, params service.SupplierClawbackParams) (bool, error) {
	requestID := candidate.RequestID
	ledgerID, inserted, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:       candidate.SupplierUserID,
		Action:       service.SupplierCreditActionClawback,
		Amount:       candidate.Amount,
		RequestID:    &requestID,
		AccountID:    candidate.AccountID,
		SourceUserID: &params.ConsumerUserID,
		// 基数与比例照抄被撤的那条入账：clawback 行因此也满足「基数 × 比例 = 金额」，
		// 供给者拿它和原入账并排看就能确认撤的是同一笔、撤了多少。
		BasisAmount: &candidate.Basis,
		ShareRatio:  candidate.ShareRatio,
	})
	if err != nil {
		return false, err
	}
	if !inserted {
		return false, nil
	}

	marked, err := markSupplierAccrualClawedBack(ctx, exec, candidate.LedgerID, supplierClawbackRemark(params.Reason))
	if err != nil {
		return false, err
	}
	if !marked {
		// 候选行是在同一事务里 FOR UPDATE 锁住读出来的，走到这里说明有并发路径绕过了锁。
		// 宁可整笔退款的追回失败（可重试，且幂等），也不能留下一条会被解冻任务重复搬运的入账。
		return false, fmt.Errorf("supplier accrual %d changed during clawback", candidate.LedgerID)
	}

	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierCreditClawbackWalletSQL,
		candidate.SupplierUserID, candidate.Amount)
	if err != nil {
		return false, fmt.Errorf("clawback supplier credit: %w", err)
	}
	if snapshot == nil {
		// 有入账流水却没有钱包行，数据已经不自洽，不能假装成功。
		return false, service.ErrSupplierWalletNotFound
	}
	if err := backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, snapshot); err != nil {
		return false, err
	}
	if remark := supplierClawbackRemark(params.Reason); remark != "" {
		if _, err := exec.ExecContext(ctx, supplierCreditLedgerRemarkSQL, ledgerID, remark); err != nil {
			return false, fmt.Errorf("write supplier clawback remark: %w", err)
		}
	}
	return true, nil
}

// thawSupplierCreditTx 把某个供给者已到期的冻结额搬进可用区，返回搬运金额。
func thawSupplierCreditTx(ctx context.Context, exec supplierCreditExecer, userID int64) (float64, error) {
	if userID <= 0 {
		return 0, nil
	}
	thawed, err := scanSupplierFloat(ctx, exec, supplierCreditThawMaturedSQL, userID)
	if err != nil {
		return 0, fmt.Errorf("thaw supplier credit: %w", err)
	}
	if thawed <= 0 {
		return 0, nil
	}

	snapshot, err := querySupplierCreditBalances(ctx, exec, supplierCreditMoveThawedSQL, userID, thawed)
	if err != nil {
		return 0, fmt.Errorf("move thawed supplier credit: %w", err)
	}
	if snapshot == nil {
		// 有到期流水却没有钱包行，数据已经不自洽，不能假装成功。
		return 0, service.ErrSupplierWalletNotFound
	}

	// 记一条 thaw 流水：提现纠纷时「这笔钱是什么时候变得可用的」必须查得到。
	// 它是钱包内部搬运，统计收益时须排除（见 service 层动作常量注释）。
	if _, _, err := insertSupplierCreditLedger(ctx, exec, supplierLedgerInsert{
		UserID:         userID,
		Action:         service.SupplierCreditActionThaw,
		Amount:         thawed,
		AvailableAfter: &snapshot.Available,
		FrozenAfter:    &snapshot.Frozen,
		HistoryAfter:   &snapshot.History,
	}); err != nil {
		return 0, err
	}
	return thawed, nil
}

// ============================================================================
// 仓储方法
// ============================================================================

func (r *supplierCreditRepository) EnsureWallet(ctx context.Context, userID int64) (*service.SupplierCreditSummary, error) {
	if userID <= 0 {
		return nil, service.ErrUserNotFound
	}
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 全零 upsert：存在则原值返回，不存在则建行。
		_, err := querySupplierCreditBalances(txCtx, txClient, supplierCreditWalletUpsertSQL, userID, 0.0, 0.0, 0.0)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("ensure supplier credit wallet: %w", err)
	}
	return r.GetWallet(ctx, userID)
}

func (r *supplierCreditRepository) GetWallet(ctx context.Context, userID int64) (*service.SupplierCreditSummary, error) {
	if userID <= 0 {
		return nil, service.ErrUserNotFound
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, supplierCreditWalletSelectSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("query supplier credit wallet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrSupplierWalletNotFound
	}
	var out service.SupplierCreditSummary
	if err := rows.Scan(
		&out.UserID,
		&out.AvailableCredit,
		&out.FrozenCredit,
		&out.HistoryCredit,
		&out.SpentCredit,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

func (r *supplierCreditRepository) Accrue(ctx context.Context, params service.SupplierAccrueParams) (bool, error) {
	var applied bool
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		var err error
		applied, err = accrueSupplierCreditTx(txCtx, txClient, params)
		return err
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (r *supplierCreditRepository) Spend(ctx context.Context, userID int64, amount float64, requestID string) (bool, error) {
	var applied bool
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		var err error
		applied, err = spendSupplierCreditTx(txCtx, txClient, userID, amount, requestID)
		return err
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

// Clawback 在自己的事务里跑一次追回。
//
// 刻意不复用退款事务：退款事务里出任何错都会把已经在支付通道成功的那笔退款状态一起
// 回滚，而追回失败是可以事后补的（幂等），不该拖垮退款收敛。见 supplier_clawback.go
// 对「best-effort」这个选择的完整说明。
func (r *supplierCreditRepository) Clawback(ctx context.Context, params service.SupplierClawbackParams) (*service.SupplierClawbackResult, error) {
	var out *service.SupplierClawbackResult
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		var err error
		out, err = clawbackSupplierCreditTx(txCtx, txClient, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *supplierCreditRepository) ThawMatured(ctx context.Context, userID int64) (float64, error) {
	var thawed float64
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		var err error
		thawed, err = thawSupplierCreditTx(txCtx, txClient, userID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return thawed, nil
}

// ThawAllMaturedUsers 逐个供给者独立开事务解冻。
//
// 刻意不把所有人塞进一个大事务：解冻是补偿型任务，一个人的数据异常不应该让整批回滚，
// 剩下的人下一轮还能被处理。
func (r *supplierCreditRepository) ThawAllMaturedUsers(ctx context.Context, limit int) (int, float64, error) {
	if limit <= 0 {
		limit = supplierCreditThawUsersDefaultLimit
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, supplierCreditPendingThawUsersSQL, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("list supplier credit thaw candidates: %w", err)
	}
	userIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}

	var processed int
	var total float64
	for _, userID := range userIDs {
		thawed, err := r.ThawMatured(ctx, userID)
		if err != nil {
			return processed, total, fmt.Errorf("thaw supplier %d: %w", userID, err)
		}
		if thawed > 0 {
			processed++
			total += thawed
		}
	}
	return processed, total, nil
}

func (r *supplierCreditRepository) ListLedger(ctx context.Context, filter service.SupplierCreditLedgerFilter) ([]service.SupplierCreditLedgerEntry, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	where, args := buildSupplierLedgerWhere(filter)
	client := clientFromContext(ctx, r.client)

	total, err := scanInt64(ctx, client, "SELECT COUNT(*) FROM supplier_credit_ledger"+where, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count supplier credit ledger: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	listQuery := `
SELECT id,
       user_id,
       action,
       amount::double precision,
       request_id,
       account_id,
       source_user_id,
       basis_amount::double precision,
       share_ratio::double precision,
       frozen_until,
       available_after::double precision,
       frozen_after::double precision,
       history_after::double precision,
       remark,
       created_at
FROM supplier_credit_ledger` + where + `
ORDER BY id DESC
LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))

	rows, err := client.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list supplier credit ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]service.SupplierCreditLedgerEntry, 0)
	for rows.Next() {
		var (
			entry          service.SupplierCreditLedgerEntry
			requestID      sql.NullString
			accountID      sql.NullInt64
			sourceUserID   sql.NullInt64
			basisAmount    sql.NullFloat64
			shareRatio     sql.NullFloat64
			frozenUntil    sql.NullTime
			availableAfter sql.NullFloat64
			frozenAfter    sql.NullFloat64
			historyAfter   sql.NullFloat64
			remark         sql.NullString
		)
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.Action,
			&entry.Amount,
			&requestID,
			&accountID,
			&sourceUserID,
			&basisAmount,
			&shareRatio,
			&frozenUntil,
			&availableAfter,
			&frozenAfter,
			&historyAfter,
			&remark,
			&entry.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if requestID.Valid {
			entry.RequestID = &requestID.String
		}
		if accountID.Valid {
			entry.AccountID = &accountID.Int64
		}
		if sourceUserID.Valid {
			entry.SourceUserID = &sourceUserID.Int64
		}
		entry.BasisAmount = nullableFloat64Ptr(basisAmount)
		entry.ShareRatio = nullableFloat64Ptr(shareRatio)
		if frozenUntil.Valid {
			entry.FrozenUntil = &frozenUntil.Time
		}
		entry.AvailableAfter = nullableFloat64Ptr(availableAfter)
		entry.FrozenAfter = nullableFloat64Ptr(frozenAfter)
		entry.HistoryAfter = nullableFloat64Ptr(historyAfter)
		if remark.Valid {
			entry.Remark = &remark.String
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// ============================================================================
// 内部helpers
// ============================================================================

// supplierLedgerInsert 收拢一条流水的可选字段，避免十来个参数排成一行认不出谁是谁。
type supplierLedgerInsert struct {
	UserID       int64
	Action       string
	Amount       float64
	RequestID    *string
	AccountID    *int64
	SourceUserID *int64
	BasisAmount  *float64
	ShareRatio   *float64
	FreezeHours  int

	// 快照三件套：thaw 这类「先动余额后记流水」的动作可以直接带进来，
	// 省掉一次回填 UPDATE。
	AvailableAfter *float64
	FrozenAfter    *float64
	HistoryAfter   *float64
}

// insertSupplierCreditLedger 插入流水。第二个返回值 false = 幂等冲突（该 request_id
// 的该动作已存在），不是错误。
func insertSupplierCreditLedger(ctx context.Context, exec supplierCreditExecer, in supplierLedgerInsert) (int64, bool, error) {
	// 无 request_id 的动作（thaw/withdraw）走不到部分唯一索引，
	// ON CONFLICT 推断子句对它们是空转，可以共用同一条语句。
	rows, err := exec.QueryContext(ctx, supplierCreditLedgerInsertSQL,
		in.UserID,
		in.Action,
		in.Amount,
		nullableStringArg(in.RequestID),
		nullableInt64Arg(in.AccountID),
		nullableInt64Arg(in.SourceUserID),
		nullableArg(in.BasisAmount),
		nullableArg(in.ShareRatio),
		in.FreezeHours,
	)
	if err != nil {
		return 0, false, fmt.Errorf("insert supplier credit ledger (%s): %w", in.Action, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	var ledgerID int64
	if err := rows.Scan(&ledgerID); err != nil {
		return 0, false, err
	}
	if err := rows.Close(); err != nil {
		return 0, false, err
	}

	if in.AvailableAfter != nil || in.FrozenAfter != nil || in.HistoryAfter != nil {
		if err := backfillSupplierLedgerSnapshot(ctx, exec, ledgerID, &supplierCreditBalances{
			Available: derefFloat64(in.AvailableAfter),
			Frozen:    derefFloat64(in.FrozenAfter),
			History:   derefFloat64(in.HistoryAfter),
		}); err != nil {
			return 0, false, err
		}
	}
	return ledgerID, true, nil
}

// supplierClawbackCandidate 是一条可被撤销的入账。
type supplierClawbackCandidate struct {
	LedgerID       int64
	SupplierUserID int64
	RequestID      string
	AccountID      *int64
	// Amount 这条入账给出去的 credit，也就是要收回的数。
	Amount float64
	// Basis 这条入账对应的消费基数，用来判断「追回够了没有」。
	Basis      float64
	ShareRatio *float64
}

// listSupplierClawbackCandidates 一次把候选全部读出来并关掉 rows。
//
// 刻意先收进切片再逐条处理：同一个连接上 rows 没关就发下一条语句会直接报错，
// 而撤销每条入账都要发四条语句。
func listSupplierClawbackCandidates(ctx context.Context, exec supplierCreditExecer, consumerUserID int64, limit int) ([]supplierClawbackCandidate, error) {
	rows, err := exec.QueryContext(ctx, supplierCreditClawbackCandidatesSQL, consumerUserID, limit)
	if err != nil {
		return nil, fmt.Errorf("list supplier clawback candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]supplierClawbackCandidate, 0)
	for rows.Next() {
		var (
			candidate  supplierClawbackCandidate
			requestID  sql.NullString
			accountID  sql.NullInt64
			shareRatio sql.NullFloat64
		)
		if err := rows.Scan(
			&candidate.LedgerID,
			&candidate.SupplierUserID,
			&requestID,
			&accountID,
			&candidate.Amount,
			&candidate.Basis,
			&shareRatio,
		); err != nil {
			return nil, err
		}
		if !requestID.Valid || strings.TrimSpace(requestID.String) == "" {
			// SQL 已经滤过 NULL，这里挡的是空串：没有幂等键就没法保证只撤一次。
			continue
		}
		candidate.RequestID = requestID.String
		if accountID.Valid {
			id := accountID.Int64
			candidate.AccountID = &id
		}
		candidate.ShareRatio = nullableFloat64Ptr(shareRatio)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

// markSupplierAccrualClawedBack 把入账摘出冻结队列。false = 这行已经不在冻结区了。
func markSupplierAccrualClawedBack(ctx context.Context, exec supplierCreditExecer, ledgerID int64, remark string) (bool, error) {
	res, err := exec.ExecContext(ctx, supplierCreditClawbackMarkSQL, ledgerID, nullableClawbackRemark(remark))
	if err != nil {
		return false, fmt.Errorf("mark supplier accrual clawed back: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// supplierClawbackRemark 规整 remark：去空白、按字符（不是字节）截断。
// 按字节截会把中文理由切出半个字，落库就是一串乱码。
func supplierClawbackRemark(reason string) string {
	remark := strings.TrimSpace(reason)
	if runes := []rune(remark); len(runes) > supplierClawbackRemarkMaxLen {
		remark = string(runes[:supplierClawbackRemarkMaxLen])
	}
	return remark
}

func nullableClawbackRemark(remark string) any {
	if remark == "" {
		return nil
	}
	return remark
}

// supplierCreditBalances 是一次余额写操作之后的三个值。
type supplierCreditBalances struct {
	Available float64
	Frozen    float64
	History   float64
}

// querySupplierCreditBalances 跑一条 RETURNING 三列余额的语句。
// 没有返回行时给 nil（而不是零值），让调用方能区分「改了，结果是 0」与「一行没改到」。
func querySupplierCreditBalances(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (*supplierCreditBalances, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	var out supplierCreditBalances
	if err := rows.Scan(&out.Available, &out.Frozen, &out.History); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

// backfillSupplierLedgerSnapshot 把余额快照写回刚插入的流水行。
func backfillSupplierLedgerSnapshot(ctx context.Context, exec supplierCreditExecer, ledgerID int64, balances *supplierCreditBalances) error {
	if balances == nil {
		return nil
	}
	if _, err := exec.ExecContext(ctx, `
UPDATE supplier_credit_ledger
SET available_after = $2,
    frozen_after    = $3,
    history_after   = $4,
    updated_at      = NOW()
WHERE id = $1`, ledgerID, balances.Available, balances.Frozen, balances.History); err != nil {
		return fmt.Errorf("backfill supplier credit ledger snapshot: %w", err)
	}
	return nil
}

// lockSupplierCreditAvailable 锁住钱包行并返回可用额。ok=false 表示钱包行不存在。
func lockSupplierCreditAvailable(ctx context.Context, exec supplierCreditExecer, userID int64) (float64, bool, error) {
	rows, err := exec.QueryContext(ctx,
		"SELECT available_credit::double precision FROM supplier_credits WHERE user_id = $1 FOR UPDATE",
		userID)
	if err != nil {
		return 0, false, fmt.Errorf("lock supplier credit wallet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	var available float64
	if err := rows.Scan(&available); err != nil {
		return 0, false, err
	}
	return available, true, rows.Err()
}

// scanSupplierFloat 跑一条返回单个 float 的语句（SUM/COALESCE 之类）。
func scanSupplierFloat(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (float64, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var v float64
	if err := rows.Scan(&v); err != nil {
		return 0, err
	}
	return v, rows.Err()
}

func buildSupplierLedgerWhere(filter service.SupplierCreditLedgerFilter) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 5)
	if filter.UserID > 0 {
		args = append(args, filter.UserID)
		clauses = append(clauses, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		args = append(args, action)
		clauses = append(clauses, fmt.Sprintf("action = $%d", len(args)))
	}
	if filter.AccountID != nil {
		args = append(args, *filter.AccountID)
		clauses = append(clauses, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if filter.StartAt != nil {
		args = append(args, *filter.StartAt)
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if filter.EndAt != nil {
		args = append(args, *filter.EndAt)
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *supplierCreditRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	// 显式 READ COMMITTED：这套资金事务的正确性建立在「锁 + 条件更新 + 唯一索引」
	// 上，不依赖更高的隔离级；而依赖服务器默认值曾真实炸过——一台
	// default_transaction_isolation=serializable 的库上，浏览器并发打开控制台
	// 就让两个碰同一行钱包的事务互相 40001（could not serialize access），
	// 普通用户看到 500。隔离级是这段代码的设计前提，就该由这段代码自己声明。
	tx, err := r.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin supplier credit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit supplier credit transaction: %w", err)
	}
	return nil
}

func nullableStringArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func derefFloat64(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
