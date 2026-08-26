//go:build integration

// APEXONE-EXT: 双边市场——溢出日配额的真库测试（迁移 227）。
//
// 这套闸门的全部正确性都压在一条语句上：
//
//	INSERT ... ON CONFLICT (day) DO UPDATE SET overflow_count = overflow_count + 1
//	WHERE overflow_count < $2 RETURNING overflow_count
//
// 「判定与计数发生在同一个行锁里」是 Postgres 的语义，sqlmock 里它只是一个字符串。
// 也就是说：**并发下会不会超发，只有真库能回答**。本组最后那个测试就是为这句话准备的。
package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// 溢出计数落在同一张按天分区的行上，而集成测试共用一个库，
// 所以每个测试各取一个互不相干的"天"，避免彼此串味。
var overflowDaySeq = struct {
	sync.Mutex
	n int
}{}

func nextOverflowDay(t *testing.T) time.Time {
	t.Helper()
	overflowDaySeq.Lock()
	defer overflowDaySeq.Unlock()
	overflowDaySeq.n++
	// 往前推，避开任何可能被别的测试用到的"今天"。
	return time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, overflowDaySeq.n)
}

// 配额把溢出次数钉死在一个可预算的数上：前 N 次放行，第 N+1 次拒绝且**不计数**。
// 被拒的次数单独记在 denied_count——混进 overflow_count 的话，同一个数字
// 既可能表示花了钱也可能表示省了钱。
func TestSupplyOverflow_QuotaAllowsExactlyLimitTimes(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	counter := NewSupplyOverflowCounter(client)

	day := nextOverflowDay(t)
	const limit = 3

	for i := 1; i <= limit; i++ {
		allowed, err := counter.TryConsumeDailyOverflow(txCtx, day, limit)
		require.NoError(t, err)
		require.True(t, allowed, "第 %d 次溢出应在配额内", i)
	}

	allowed, err := counter.TryConsumeDailyOverflow(txCtx, day, limit)
	require.NoError(t, err)
	require.False(t, allowed, "配额用满后必须拒绝")

	usage, err := counter.GetDailyOverflowUsage(txCtx, day)
	require.NoError(t, err)
	require.Equal(t, int64(limit), usage.OverflowCount, "被拒的那次不能计进已花的钱")
	require.Equal(t, int64(1), usage.DeniedCount)
	require.Equal(t, day.Format("2006-01-02"), usage.Day)
}

// limit <= 0 = 不限量，但**照常计数**。
// 溢出率是经营信号，不设上限不等于不用看。
func TestSupplyOverflow_ZeroLimitMeansUnlimitedButStillCounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	counter := NewSupplyOverflowCounter(client)

	day := nextOverflowDay(t)
	for i := 0; i < 5; i++ {
		allowed, err := counter.TryConsumeDailyOverflow(txCtx, day, 0)
		require.NoError(t, err)
		require.True(t, allowed)
	}

	usage, err := counter.GetDailyOverflowUsage(txCtx, day)
	require.NoError(t, err)
	require.Equal(t, int64(5), usage.OverflowCount)
	require.Zero(t, usage.DeniedCount)
}

// 配额按自然日结算，跨日重置。昨天用满不影响今天。
func TestSupplyOverflow_QuotaResetsPerDay(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	counter := NewSupplyOverflowCounter(client)

	yesterday := nextOverflowDay(t)
	today := yesterday.AddDate(0, 0, 1)

	allowed, err := counter.TryConsumeDailyOverflow(txCtx, yesterday, 1)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = counter.TryConsumeDailyOverflow(txCtx, yesterday, 1)
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = counter.TryConsumeDailyOverflow(txCtx, today, 1)
	require.NoError(t, err)
	require.True(t, allowed, "跨日必须重置")

	// 两天各自独立记账。
	usage, err := counter.GetDailyOverflowUsage(txCtx, yesterday)
	require.NoError(t, err)
	require.Equal(t, int64(1), usage.OverflowCount)
	require.Equal(t, int64(1), usage.DeniedCount)
}

// 当日无记录返回零值而不是错误：管理端那张卡片在还没溢出过的日子也要画得出来。
func TestSupplyOverflow_UsageIsZeroWhenNothingHappened(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	counter := NewSupplyOverflowCounter(client)

	usage, err := counter.GetDailyOverflowUsage(txCtx, nextOverflowDay(t))
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Zero(t, usage.OverflowCount)
	require.Zero(t, usage.DeniedCount)
}

// 本组存在的全部理由：**并发下不能超发**。
//
// 「先 SELECT 计数、再判断、再 UPDATE」在这个测试里必挂——几十个并发请求会一起读到
// 未满然后各自放行。这里不能用 testEntTx（那是一个会回滚的单事务，串行化了所有写），
// 必须让每个 goroutine 走各自的连接打到真库上，所以用 integrationEntClient 并自行清理。
func TestSupplyOverflow_ConcurrentConsumersNeverExceedQuota(t *testing.T) {
	ctx := context.Background()
	client := integrationEntClient
	counter := NewSupplyOverflowCounter(client)

	day := nextOverflowDay(t)
	dayKey := day.Format("2006-01-02")
	t.Cleanup(func() {
		_, _ = client.ExecContext(context.Background(),
			`DELETE FROM supply_overflow_daily WHERE day = $1`, dayKey)
	})

	const (
		limit    = 10
		attempts = 60
	)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
		errs    []error
	)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽量让所有请求挤在同一瞬间
			allowed, err := counter.TryConsumeDailyOverflow(ctx, day, limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if allowed {
				granted++
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, errs, "并发消耗配额不该报错：报错会被上层当成 fail-closed，白白拒掉本可溢出的请求")
	require.Equal(t, limit, granted, "放行次数必须恰好等于配额——多一次就是平台多亏一次钱")

	usage, err := counter.GetDailyOverflowUsage(ctx, day)
	require.NoError(t, err)
	require.Equal(t, int64(limit), usage.OverflowCount)
	require.Equal(t, int64(attempts-limit), usage.DeniedCount,
		fmt.Sprintf("每一次被拒都要留痕，%d 次尝试里应有 %d 次被拒", attempts, attempts-limit))
}

// 三个计数在真库里**互不串台**（迁移 236）。
//
// sqlmock 只能证明"我发了这条 SQL"，证明不了这三列在同一行上各自独立地累加。
// 而这正是这一列的全部价值所在：三个数字类型相同、含义相反
// （保险生效了 / 被预算挡住了 / 不够赔），任何一次串台都会让面板给出
// 与事实相反的建议，且没有任何症状。
func TestSupplyOverflow_ExhaustedCountIsIndependentOfTheOtherTwo(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	counter := NewSupplyOverflowCounter(client)
	day := nextOverflowDay(t)

	// 先只记「兜底也空了」——此时当日还没有任何一行。
	// 这一步同时钉住 INSERT 分支：第一次记录时不能因为缺行而丢掉。
	require.NoError(t, counter.RecordOverflowExhausted(txCtx, day))
	require.NoError(t, counter.RecordOverflowExhausted(txCtx, day))

	usage, err := counter.GetDailyOverflowUsage(txCtx, day)
	require.NoError(t, err)
	require.Equal(t, int64(2), usage.ExhaustedCount)
	require.Zero(t, usage.OverflowCount, "记兜底耗尽却把溢出数也加了——面板会以为平台在花钱供货")
	require.Zero(t, usage.DeniedCount, "记兜底耗尽却把配额拒绝数也加了——运营会去调预算，而该做的是加账号")

	// 再叠一次真实溢出：走的是 UPDATE 分支，不能把已有的 exhausted 抹掉。
	allowed, err := counter.TryConsumeDailyOverflow(txCtx, day, 0)
	require.NoError(t, err)
	require.True(t, allowed)

	usage, err = counter.GetDailyOverflowUsage(txCtx, day)
	require.NoError(t, err)
	require.Equal(t, int64(1), usage.OverflowCount)
	require.Equal(t, int64(2), usage.ExhaustedCount, "溢出计数的 upsert 把兜底耗尽数覆盖掉了")
	require.Zero(t, usage.DeniedCount)
}
