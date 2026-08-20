//go:build integration

// APEXONE-EXT: 双边市场——争议台账的真库测试。
//
// service 侧的单测已经钉住了「settled_at 非空就跳过副作用」这条判断，但那条判断
// 读到的值是 stub 给的。这张表真正的保证在 SQL 里，只有真 Postgres 能证明：
//
//  1. ON CONFLICT (dispute_id) 能否推断到迁移 231 的那个唯一索引。推断不上不是
//     静默降级而是直接报错，表现是「第二次推送整条 webhook 500」；
//  2. 重复 upsert **绝不能**把 settled_at 与四个结算金额写回去。这是「扣钱只跑一次」
//     在库层的最后一道保证——Stripe 对同一个争议会推五次，其中 closed 那次
//     几乎必然发生在结算之后；
//  3. ClaimForSettlement 在并发下恰好只有一个调用者拿到 true。
//
// 这三条都是「错了就是真金白银多扣一遍」的性质，不能只靠 sqlmock。
package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDisputeID(tag string) string {
	return fmt.Sprintf("dp_%s_%d", tag, time.Now().UnixNano())
}

// disputeUpsert 造一条"创建"推送，字段填满，方便各个用例只改自己关心的那几个。
func disputeUpsert(disputeID string) service.PaymentDisputeUpsert {
	return service.PaymentDisputeUpsert{
		DisputeID:     disputeID,
		ProviderKey:   "stripe",
		TradeNo:       "pi_9",
		OrderID:       int64Ptr(77),
		OutTradeNo:    "OUT-77",
		UserID:        int64Ptr(9),
		Status:        "open",
		Reason:        "fraudulent",
		DisputeAmount: 14.99,
		Currency:      "USD",
		BasisAmount:   100,
		RawData:       `{"id":"evt_1"}`,
	}
}

// 第一条推送落库；第二条推送走 ON CONFLICT 更新同一行，而不是插第二行、也不是报错。
func TestPaymentDispute_UpsertIsIdempotentOnDisputeID(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewPaymentDisputeRepository(tx.Client())

	id := newDisputeID("idem")
	first, err := repo.Upsert(txCtx, disputeUpsert(id))
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotZero(t, first.ID)
	require.True(t, first.SettledAt == nil, "刚落库的争议还没结算")

	second, err := repo.Upsert(txCtx, disputeUpsert(id))
	require.NoError(t, err, "ON CONFLICT 推断不到唯一索引会在这里报错")
	require.Equal(t, first.ID, second.ID, "同一个 dispute_id 必须落在同一行上")

	rows := querySingleInt(t, txCtx, tx.Client(),
		`SELECT COUNT(*) FROM payment_disputes WHERE dispute_id = $1`, id)
	require.Equal(t, 1, rows)
}

// 这是本文件最要紧的一条：结算之后再来的推送（Stripe 的 closed 几乎必然如此）
// 不能把闸门推回去，也不能把已经记下的四个金额清零。
// 任何一条失守，运营看到的现象都是「一笔拒付扣了两遍钱」。
func TestPaymentDispute_UpsertNeverUndoesSettlement(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewPaymentDisputeRepository(client)

	id := newDisputeID("settled")
	_, err := repo.Upsert(txCtx, disputeUpsert(id))
	require.NoError(t, err)

	claimed, err := repo.ClaimForSettlement(txCtx, id)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.RecordSettlement(txCtx, service.PaymentDisputeSettlement{
		DisputeID: id, BalanceDeducted: 40, ClawedCredit: 12.5,
		ClawedBasis: 80, UncoveredBasis: 20,
	}))

	// 关闭那条推送：状态变了、金额也可能被通道重算过。
	closing := disputeUpsert(id)
	closing.Status = "lost"
	closing.BasisAmount = 999 // 通道重算出来的新基数——绝不能覆盖对账凭据
	closing.DisputeAmount = 15.99
	after, err := repo.Upsert(txCtx, closing)
	require.NoError(t, err)

	require.False(t, after.SettledAt == nil, "闸门被 upsert 推回去了：这笔拒付会被扣第二遍")
	require.False(t, after.ResolvedAt == nil, "终态推送要写上关闭时刻")
	assert.Equal(t, "lost", after.Status, "状态本来就该跟着推送走")

	assert.InDelta(t, 40, disputeAmountCol(t, txCtx, client, id, "balance_deducted"), 1e-9)
	assert.InDelta(t, 12.5, disputeAmountCol(t, txCtx, client, id, "clawed_credit"), 1e-9)
	assert.InDelta(t, 80, disputeAmountCol(t, txCtx, client, id, "clawed_basis"), 1e-9)
	assert.InDelta(t, 20, disputeAmountCol(t, txCtx, client, id, "uncovered_basis"), 1e-9)
	assert.InDelta(t, 100, disputeAmountCol(t, txCtx, client, id, "basis_amount"),
		1e-9, "结算后基数被冻住：它是那次追回实际用的数，改了就对不上账")

	// 争议金额与币种反过来**要**跟着走：它们是通道侧的事实，不是我们的凭据。
	assert.InDelta(t, 15.99, disputeAmountCol(t, txCtx, client, id, "dispute_amount"), 1e-9)
}

// 结算之前基数可以被后到的推送修正——那时它还没被任何一次追回用过。
func TestPaymentDispute_BasisStillMovesBeforeSettlement(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewPaymentDisputeRepository(client)

	id := newDisputeID("basis")
	_, err := repo.Upsert(txCtx, disputeUpsert(id))
	require.NoError(t, err)

	updated := disputeUpsert(id)
	updated.BasisAmount = 120
	_, err = repo.Upsert(txCtx, updated)
	require.NoError(t, err)

	assert.InDelta(t, 120, disputeAmountCol(t, txCtx, client, id, "basis_amount"), 1e-9)
}

// 对不上订单的那条推送先落库（order_id / user_id 为 NULL），
// 事后补上时要被填进去；而已经对上的值不能被后到的空值抹掉。
//
// 这两个方向对应同一个现实：Stripe 的 dispute 对象带不带 payment_intent 的展开
// 在不同事件里不一样，同一个争议的两条推送字段丰俭由人。
func TestPaymentDispute_UpsertFillsButNeverClearsOrderLinkage(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewPaymentDisputeRepository(client)

	// 方向一：孤儿争议落库，随后补上订单。
	orphanID := newDisputeID("orphan")
	orphan := disputeUpsert(orphanID)
	orphan.OrderID, orphan.UserID, orphan.OutTradeNo, orphan.TradeNo = nil, nil, "", ""
	first, err := repo.Upsert(txCtx, orphan)
	require.NoError(t, err, "对不上订单不是错误，这一行照样要落库")
	require.True(t, first.OrderID == nil)
	require.True(t, first.UserID == nil)

	filled, err := repo.Upsert(txCtx, disputeUpsert(orphanID))
	require.NoError(t, err)
	require.False(t, filled.OrderID == nil, "空着的订单号要被后到的推送补上")
	assert.Equal(t, int64(77), *filled.OrderID)
	assert.Equal(t, int64(9), *filled.UserID)
	assert.Equal(t, "OUT-77", filled.OutTradeNo)
	assert.Equal(t, "pi_9", filled.TradeNo)

	// 方向二：已经对上的争议，收到一条字段更少的推送。
	linkedID := newDisputeID("linked")
	_, err = repo.Upsert(txCtx, disputeUpsert(linkedID))
	require.NoError(t, err)

	sparse := disputeUpsert(linkedID)
	sparse.OrderID, sparse.UserID, sparse.OutTradeNo, sparse.TradeNo = nil, nil, "", ""
	kept, err := repo.Upsert(txCtx, sparse)
	require.NoError(t, err)

	require.False(t, kept.OrderID == nil, "一条本来对上了订单的记录不能因为后续推送变回孤儿")
	assert.Equal(t, int64(77), *kept.OrderID)
	assert.Equal(t, int64(9), *kept.UserID)
	assert.Equal(t, "OUT-77", kept.OutTradeNo)
	assert.Equal(t, "pi_9", kept.TradeNo)
}

// 第一次关闭的时刻要留住：won 之后又来一条 lost（或反过来）时，
// resolved_at 记的是这场争议第一次有结论的时间。
func TestPaymentDispute_ResolvedAtKeepsFirstClosure(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewPaymentDisputeRepository(tx.Client())

	id := newDisputeID("resolved")
	opening := disputeUpsert(id)
	first, err := repo.Upsert(txCtx, opening)
	require.NoError(t, err)
	require.True(t, first.ResolvedAt == nil, "open 态不该有关闭时刻")

	closing := disputeUpsert(id)
	closing.Status = "lost"
	closed, err := repo.Upsert(txCtx, closing)
	require.NoError(t, err)
	require.False(t, closed.ResolvedAt == nil)

	// 这两句都是为了让"被改写过"可分辨，缺一不可：
	//   - sleep：两次 upsert 之间隔着的只有一次本地往返，同一毫秒内打完是常态，
	//     不拉开距离的话改写与没改写长得一模一样；
	//   - Equal 而不是 WithinDuration：这个值要么原封不动、要么就是被重写了，
	//     中间没有"差不多"的余地，放宽容差等于把这条断言废掉。
	time.Sleep(5 * time.Millisecond)

	again := disputeUpsert(id)
	again.Status = "won"
	reclosed, err := repo.Upsert(txCtx, again)
	require.NoError(t, err)
	require.False(t, reclosed.ResolvedAt == nil)
	assert.True(t, closed.ResolvedAt.Equal(*reclosed.ResolvedAt),
		"关闭时刻只记第一次：第一次 %s，再推一次后变成 %s", closed.ResolvedAt, reclosed.ResolvedAt)
}

// 串行重放：Stripe 对同一个争议推五次，只有第一次能占到坑。
func TestPaymentDispute_ClaimSucceedsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewPaymentDisputeRepository(tx.Client())

	id := newDisputeID("claim")
	_, err := repo.Upsert(txCtx, disputeUpsert(id))
	require.NoError(t, err)

	claimed, err := repo.ClaimForSettlement(txCtx, id)
	require.NoError(t, err)
	require.True(t, claimed)

	for i := 0; i < 4; i++ {
		claimed, err = repo.ClaimForSettlement(txCtx, id)
		require.NoError(t, err)
		require.False(t, claimed, "第 %d 次重放占到了坑：这笔拒付会被扣两遍", i+2)
	}
}

// 不存在的争议占不到坑，且不该报错——那是"这条推送我们没落过库"的正常表达。
func TestPaymentDispute_ClaimOnMissingRowIsNotAnError(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewPaymentDisputeRepository(tx.Client())

	claimed, err := repo.ClaimForSettlement(txCtx, newDisputeID("missing"))
	require.NoError(t, err)
	assert.False(t, claimed)
}

// 并发占坑：Stripe 的重试与首推可能同时到达两个实例。
//
// 这里不能用 testEntTx——那是单个会回滚的事务，会把所有写串行化，
// 恰好把要验的竞态抹平。必须让每个 goroutine 走各自的连接打到真库上。
func TestPaymentDispute_ConcurrentClaimOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	client := integrationEntClient
	repo := NewPaymentDisputeRepository(client)

	id := newDisputeID("race")
	t.Cleanup(func() {
		_, _ = client.ExecContext(context.Background(),
			`DELETE FROM payment_disputes WHERE dispute_id = $1`, id)
	})

	_, err := repo.Upsert(ctx, disputeUpsert(id))
	require.NoError(t, err)

	const attempts = 16
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		won  int
		errs []error
	)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽量让所有占坑挤在同一瞬间
			claimed, err := repo.ClaimForSettlement(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if claimed {
				won++
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, errs)
	require.Equal(t, 1, won, "并发下有 %d 个调用者占到坑——多一个就是多扣一遍钱", won)
}

// 没有 dispute_id 的调用在进库之前就被挡下来：一行没有幂等键的记录
// 会让"这个争议处理过吗"从此没有答案。
func TestPaymentDispute_RejectsBlankDisputeID(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewPaymentDisputeRepository(tx.Client())

	blank := disputeUpsert("   ")
	_, err := repo.Upsert(txCtx, blank)
	require.Error(t, err)

	_, err = repo.ClaimForSettlement(txCtx, "   ")
	require.Error(t, err)

	err = repo.RecordSettlement(txCtx, service.PaymentDisputeSettlement{DisputeID: "  "})
	require.Error(t, err)

	count := querySingleInt(t, txCtx, tx.Client(),
		`SELECT COUNT(*) FROM payment_disputes WHERE dispute_id = ''`)
	assert.Zero(t, count, "空 id 的行不该进库")
}

func disputeAmountCol(t *testing.T, ctx context.Context, client *dbent.Client, disputeID, column string) float64 {
	t.Helper()
	// #nosec G201 -- column 只来自本文件里的字面量，不来自输入。
	return querySingleFloat(t, ctx, client,
		fmt.Sprintf(`SELECT %s::double precision FROM payment_disputes WHERE dispute_id = $1`, column),
		disputeID)
}
