//go:build integration

// APEXONE-EXT: 双边市场——挂号奖励仓储的真库测试。
//
// 这一组要钉的三件事都只有真库能回答，替身表达不了：
//
//   - **当天只加一次**。幂等靠的是 UPDATE 里那句 `last_day <> $1`，不是 Go 侧的判断。
//     在 sqlmock 里「加一次」和「加两次」是同一段代码。
//   - **断线不清零**。它不是一行 if，是「不满足 WHERE 就不在更新集里」这个性质——
//     只有真的执行一次 UPDATE 才能证明 days 原地不动而不是被写成 0。
//   - **JSONB 表达式本身**。`extra->'k'->>'days'` 的类型转换、`||` 的合并语义、
//     `jsonb_build_object` 的嵌套，写错任何一处 Postgres 会当场报错，而字符串比对不会。
//
// 断言一律走增量或按账号定点查：这些语句作用于全表，写死绝对值等于假设库里
// 只有这一个测试的数据。
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// activeDaysOf 读某个账号当前的累计在线天数。
// 缺失（从没被 tick 过）返回 -1，与「被 tick 成 0」区分开——后者是 bug。
func activeDaysOf(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client, fmt.Sprintf(
		"SELECT COALESCE((extra->'%s'->>'days')::int, -1) FROM accounts WHERE id = $1",
		service.SupplyActiveDaysExtraKey), accountID)
	require.NoError(t, err)
	return n
}

// setActiveDays 直接把天数写到某个值，省掉「跑 30 轮 tick」。
func setActiveDays(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, days int, lastDay string) {
	t.Helper()
	_, err := client.ExecContext(ctx, fmt.Sprintf(`
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
    '%s', jsonb_build_object('days', $1::int, 'last_day', $2::text))
WHERE id = $3`, service.SupplyActiveDaysExtraKey), days, lastDay, accountID)
	require.NoError(t, err)
}

func setSchedulable(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, schedulable bool) {
	t.Helper()
	_, err := client.ExecContext(ctx,
		"UPDATE accounts SET schedulable = $1 WHERE id = $2", schedulable, accountID)
	require.NoError(t, err)
}

// 同一天跑两次只加一天。
//
// worker 一小时跑一轮，一天会跑 24 次；这条性质失守的表现是所有人的天数
// 以 24 倍速膨胀，两天就把 90 天档发完——而没有任何报错。
func TestSupplyIncentive_TickIsIdempotentWithinSameDay(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-idem")
	account := mustCreateSupplyAccount(t, client, owner, "tick-idem", service.SupplyStateActive, service.StatusActive, true)

	day := "2026-09-14"
	_, err := repo.TickActiveDays(txCtx, day)
	require.NoError(t, err)
	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account))

	// 同一天再跑三次——模拟 worker 一小时一轮。
	for i := 0; i < 3; i++ {
		_, err = repo.TickActiveDays(txCtx, day)
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account),
		"当天重复 tick 必须零行受影响")

	// 换一天才涨。
	_, err = repo.TickActiveDays(txCtx, "2026-09-15")
	require.NoError(t, err)
	assert.Equal(t, int64(2), activeDaysOf(t, txCtx, client, account))
}

// 断线期间不涨、接回来继续涨、**永不清零**。
//
// 这是对外宣称的那句 "the counter pauses — it does not reset"，
// 也是共享者最容易来问的一件事。
func TestSupplyIncentive_TickPausesOnOfflineAndNeverResets(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-pause")
	account := mustCreateSupplyAccount(t, client, owner, "tick-pause", service.SupplyStateActive, service.StatusActive, true)
	setActiveDays(t, txCtx, client, account, 12, "2026-09-13")

	// 号掉线：schedulable=false（熔断、限流下线、供给者暂停都会走到这里）。
	setSchedulable(t, txCtx, client, account, false)
	_, err := repo.TickActiveDays(txCtx, "2026-09-14")
	require.NoError(t, err)
	assert.Equal(t, int64(12), activeDaysOf(t, txCtx, client, account),
		"掉线期间既不该涨，更不该被清零")

	// 接回来：从 12 继续，不是从 0 重来。
	setSchedulable(t, txCtx, client, account, true)
	_, err = repo.TickActiveDays(txCtx, "2026-09-15")
	require.NoError(t, err)
	assert.Equal(t, int64(13), activeDaysOf(t, txCtx, client, account))
}

// 谁该涨天数、谁不该——五个号一次说清。
//
// 每一条排除都对应一次会真的发错钱的事故：
//   - 自营号（无归属人）涨天数 = 平台给自己的号发奖励；
//   - pending_review 涨天数 = 观察期还没过就在攒奖励门槛；
//   - retired 涨天数 = 已经解绑的号继续攒，回头一挂就领钱；
//   - 状态缺失的存量号涨天数 = 一批从没跑过观察期的号直接够格。
func TestSupplyIncentive_TickSelectsOnlyLiveSupplyAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-scope")

	live := mustCreateSupplyAccount(t, client, owner, "tk-live", service.SupplyStateActive, service.StatusActive, true)
	pending := mustCreateSupplyAccount(t, client, owner, "tk-pending", service.SupplyStatePendingReview, service.StatusActive, false)
	retired := mustCreateSupplyAccount(t, client, owner, "tk-retired", service.SupplyStateRetired, service.StatusActive, true)
	legacy := mustCreateSupplyAccount(t, client, owner, "tk-legacy", "", service.StatusActive, true)
	// active 但不可调度：排空中/熔断下线的形态。
	parked := mustCreateSupplyAccount(t, client, owner, "tk-parked", service.SupplyStateActive, service.StatusActive, false)
	// 自营号：没有归属人。
	firstParty := mustCreateAccount(t, client, &service.Account{
		Name:     "tk-first-party",
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	})

	_, err := repo.TickActiveDays(txCtx, "2026-09-14")
	require.NoError(t, err)

	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, live))
	assert.Equal(t, int64(-1), activeDaysOf(t, txCtx, client, pending), "观察期里的号不该攒天数")
	assert.Equal(t, int64(-1), activeDaysOf(t, txCtx, client, retired), "已下线的号不该攒天数")
	assert.Equal(t, int64(-1), activeDaysOf(t, txCtx, client, legacy),
		"状态缺失的存量号兜底成 pending_review，不该攒天数")
	assert.Equal(t, int64(-1), activeDaysOf(t, txCtx, client, parked), "不可调度的号不在供货")
	assert.Equal(t, int64(-1), activeDaysOf(t, txCtx, client, firstParty.ID), "自营号没有收款人")
}

// 候选：够格、未发过、按天数降序。
func TestSupplyIncentive_ListCandidatesFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)
	credit := NewSupplierCreditRepository(client)

	owner := mustCreateSupplier(t, client, "cand")
	slug := fmt.Sprintf("c%d", time.Now().UnixNano()%1e10)
	prefix := service.SupplyIncentiveRequestPrefix(slug, 0)

	veteran := mustCreateSupplyAccount(t, client, owner, "cd-veteran", service.SupplyStateActive, service.StatusActive, true)
	rookie := mustCreateSupplyAccount(t, client, owner, "cd-rookie", service.SupplyStateActive, service.StatusActive, true)
	tooNew := mustCreateSupplyAccount(t, client, owner, "cd-toonew", service.SupplyStateActive, service.StatusActive, true)
	alreadyPaid := mustCreateSupplyAccount(t, client, owner, "cd-paid", service.SupplyStateActive, service.StatusActive, true)

	setActiveDays(t, txCtx, client, veteran, 40, "2026-09-14")
	setActiveDays(t, txCtx, client, rookie, 11, "2026-09-14")
	setActiveDays(t, txCtx, client, tooNew, 9, "2026-09-14")
	setActiveDays(t, txCtx, client, alreadyPaid, 50, "2026-09-14")

	// alreadyPaid 已经拿过这一档。
	applied, err := credit.Accrue(txCtx, service.SupplierAccrueParams{
		SupplierUserID: owner,
		RequestID:      service.SupplyIncentiveRequestID(slug, 0, alreadyPaid),
		AccountID:      &alreadyPaid,
		BasisAmount:    5,
		ShareRatio:     1.0,
		FreezeHours:    72,
	})
	require.NoError(t, err)
	require.True(t, applied)

	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		RequestPrefix: prefix,
		Limit:         10,
	})
	require.NoError(t, err)

	ids := make([]int64, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.AccountID)
	}
	assert.Contains(t, ids, veteran)
	assert.Contains(t, ids, rookie)
	assert.NotContains(t, ids, tooNew, "没到门槛的号不该出现")
	assert.NotContains(t, ids, alreadyPaid, "已发过这一档的号不该再出现")

	// 排序：天数多的在前——这就是对外宣称的「按接入先后分配」。
	vetPos, rookiePos := -1, -1
	for i, id := range ids {
		switch id {
		case veteran:
			vetPos = i
		case rookie:
			rookiePos = i
		}
	}
	require.NotEqual(t, -1, vetPos)
	require.NotEqual(t, -1, rookiePos)
	assert.Less(t, vetPos, rookiePos)

	for _, c := range got {
		if c.AccountID == veteran {
			assert.Equal(t, owner, c.OwnerUserID)
			assert.Equal(t, 40, c.ActiveDays)
		}
	}
}

// Limit 就是「剩余名额」，必须是硬的——它是这个功能唯一的成本闸门。
func TestSupplyIncentive_ListCandidatesRespectsLimit(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "cand-limit")
	slug := fmt.Sprintf("l%d", time.Now().UnixNano()%1e10)

	for i := 0; i < 4; i++ {
		id := mustCreateSupplyAccount(t, client, owner, fmt.Sprintf("cl-%d", i),
			service.SupplyStateActive, service.StatusActive, true)
		setActiveDays(t, txCtx, client, id, 20+i, "2026-09-14")
	}

	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		RequestPrefix: service.SupplyIncentiveRequestPrefix(slug, 0),
		Limit:         2,
	})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// 平台过滤：空 = 不限（当前上线的形态就是 Claude 与 ChatGPT 共用一个名额池）。
func TestSupplyIncentive_ListCandidatesFiltersByPlatform(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "cand-plat")
	slug := fmt.Sprintf("p%d", time.Now().UnixNano()%1e10)
	prefix := service.SupplyIncentiveRequestPrefix(slug, 0)

	claude := mustCreateSupplyAccount(t, client, owner, "cp-claude", service.SupplyStateActive, service.StatusActive, true)
	setActiveDays(t, txCtx, client, claude, 20, "2026-09-14")

	openaiAccount := mustCreateAccount(t, client, &service.Account{
		Name:        "cp-openai",
		Platform:    service.PlatformOpenAI,
		Status:      service.StatusActive,
		Extra:       map[string]any{service.SupplyStateExtraKey: service.SupplyStateActive},
		Credentials: map[string]any{"email_address": "cp-openai@upstream.test"},
	})
	_, err := client.ExecContext(txCtx,
		"UPDATE accounts SET owner_user_id = $1, schedulable = TRUE WHERE id = $2", owner, openaiAccount.ID)
	require.NoError(t, err)
	setActiveDays(t, txCtx, client, openaiAccount.ID, 20, "2026-09-14")

	anthropicOnly, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		Platform:      service.PlatformAnthropic,
		RequestPrefix: prefix,
		Limit:         50,
	})
	require.NoError(t, err)
	ids := candidateIDs(anthropicOnly)
	assert.Contains(t, ids, claude)
	assert.NotContains(t, ids, openaiAccount.ID)

	both, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		RequestPrefix: prefix,
		Limit:         50,
	})
	require.NoError(t, err)
	ids = candidateIDs(both)
	assert.Contains(t, ids, claude)
	assert.Contains(t, ids, openaiAccount.ID, "空 platform 必须是「不限」，不是「没有平台的号」")
}

// 名额计数不能跨档、跨活动串味：串了的后果是某一档以为自己发完了（或者没发完）。
func TestSupplyIncentive_CountGrantedIsScopedToTier(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)
	credit := NewSupplierCreditRepository(client)

	owner := mustCreateSupplier(t, client, "count")
	slug := fmt.Sprintf("g%d", time.Now().UnixNano()%1e10)
	otherSlug := slug[:len(slug)-1] + "z"

	grant := func(s string, tier int, accountID int64) {
		applied, err := credit.Accrue(txCtx, service.SupplierAccrueParams{
			SupplierUserID: owner,
			RequestID:      service.SupplyIncentiveRequestID(s, tier, accountID),
			AccountID:      &accountID,
			BasisAmount:    5,
			ShareRatio:     1.0,
			FreezeHours:    72,
		})
		require.NoError(t, err)
		require.True(t, applied)
	}

	a1 := mustCreateSupplyAccount(t, client, owner, "ct-1", service.SupplyStateActive, service.StatusActive, true)
	a2 := mustCreateSupplyAccount(t, client, owner, "ct-2", service.SupplyStateActive, service.StatusActive, true)
	a3 := mustCreateSupplyAccount(t, client, owner, "ct-3", service.SupplyStateActive, service.StatusActive, true)

	grant(slug, 0, a1)
	grant(slug, 0, a2)
	grant(slug, 1, a3)      // 同活动，另一档
	grant(otherSlug, 0, a3) // 另一个活动的同一档

	stats, err := repo.GrantStats(txCtx, service.SupplyIncentiveRequestPrefix(slug, 0))
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Total)
	assert.Equal(t, 2, stats.ByUser[owner], "同一个人在这一档拿了两份")

	stats, err = repo.GrantStats(txCtx, service.SupplyIncentiveRequestPrefix(slug, 1))
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Total)
}

// 解绑重挂的那条路：拿满的人整个被排除。
//
// 只有这一层挡得住它——上游订阅查重看的是 deleted_at IS NULL，解绑会软删该行
// 并把 credentials 抹成 {}，于是同一份订阅换一个新 account id 就能再挂一次，
// 幂等键也跟着换成新的。按账号计的名额对此完全无感。
func TestSupplyIncentive_ListCandidatesExcludesCappedUsers(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	slug := fmt.Sprintf("x%d", time.Now().UnixNano()%1e10)
	prefix := service.SupplyIncentiveRequestPrefix(slug, 0)

	farmer := mustCreateSupplier(t, client, "farmer")
	honest := mustCreateSupplier(t, client, "honest")

	// farmer 解绑重挂之后的那个新号：全新的 account id，没有任何发放记录。
	rebound := mustCreateSupplyAccount(t, client, farmer, "xc-rebound", service.SupplyStateActive, service.StatusActive, true)
	fresh := mustCreateSupplyAccount(t, client, honest, "xc-fresh", service.SupplyStateActive, service.StatusActive, true)
	setActiveDays(t, txCtx, client, rebound, 20, "2026-09-14")
	setActiveDays(t, txCtx, client, fresh, 20, "2026-09-14")

	// 不排除任何人时，重挂的号是够格的——这正是要挡的那个形态。
	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		RequestPrefix: prefix,
		Limit:         50,
	})
	require.NoError(t, err)
	assert.Contains(t, candidateIDs(got), rebound, "按账号计的名额挡不住解绑重挂")

	// 把拿满的人排除掉之后，重挂的号连同他名下别的号一起消失，诚实用户不受影响。
	got, err = repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays:   10,
		RequestPrefix:   prefix,
		ExcludedUserIDs: []int64{farmer},
		Limit:           50,
	})
	require.NoError(t, err)
	ids := candidateIDs(got)
	assert.NotContains(t, ids, rebound)
	assert.Contains(t, ids, fresh, "排除是按人的，不该波及别人")
}

func candidateIDs(list []service.SupplyIncentiveCandidate) []int64 {
	ids := make([]int64, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.AccountID)
	}
	return ids
}
