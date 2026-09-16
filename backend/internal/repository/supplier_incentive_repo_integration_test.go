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

// setActiveDays 直接把**总计**写到某个值，省掉「跑 30 轮 tick」。
func setActiveDays(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, days int, lastDay string) {
	t.Helper()
	_, err := client.ExecContext(ctx, fmt.Sprintf(`
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
    '%s', COALESCE(extra->'%s', '{}'::jsonb)
          || jsonb_build_object('days', $1::int, 'last_day', $2::text))
WHERE id = $3`, service.SupplyActiveDaysExtraKey, service.SupplyActiveDaysExtraKey),
		days, lastDay, accountID)
	require.NoError(t, err)
}

// setProgramActiveDays 直接把**某一期活动的桶**写到某个值。
//
// 候选查询读的是这个桶而不是总计，所以发放相关的用例一律用它——用总计的话，
// 断言会在「门槛读错了地方」这种恰恰要防的 bug 下照样通过。
func setProgramActiveDays(
	t *testing.T, ctx context.Context, client *dbent.Client,
	accountID int64, slug string, days int, lastDay string,
) {
	t.Helper()
	// 逐层 `||` 合并，不用 jsonb_set：后者的 create_missing 只补路径的**最后一级**，
	// 中间的 'programs' 不存在时整次写入会被静默丢弃（返回原值、不报错）。
	// 这与生产那条 tick 语句是同一种写法，也就顺带保证了两边建出来的形状一致。
	_, err := client.ExecContext(ctx, fmt.Sprintf(`
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
    '%[1]s', COALESCE(extra->'%[1]s', '{}'::jsonb) || jsonb_build_object(
        'programs', COALESCE(extra->'%[1]s'->'programs', '{}'::jsonb) || jsonb_build_object(
            $1::text, jsonb_build_object('days', $2::int, 'last_day', $3::text))))
WHERE id = $4`, service.SupplyActiveDaysExtraKey),
		slug, days, lastDay, accountID)
	require.NoError(t, err)
}

// programActiveDaysOf 读某个账号在某一期活动里的天数。缺失返回 -1。
func programActiveDaysOf(
	t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, slug string,
) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client, fmt.Sprintf(
		"SELECT COALESCE((extra->'%s'->'programs'->($2::text)->>'days')::int, -1) FROM accounts WHERE id = $1",
		service.SupplyActiveDaysExtraKey), accountID, slug)
	require.NoError(t, err)
	return n
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
	_, err := repo.TickActiveDays(txCtx, day, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account))

	// 同一天再跑三次——模拟 worker 一小时一轮。
	for i := 0; i < 3; i++ {
		_, err = repo.TickActiveDays(txCtx, day, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account),
		"当天重复 tick 必须零行受影响")

	// 换一天才涨。
	_, err = repo.TickActiveDays(txCtx, "2026-09-15", nil)
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
	_, err := repo.TickActiveDays(txCtx, "2026-09-14", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(12), activeDaysOf(t, txCtx, client, account),
		"掉线期间既不该涨，更不该被清零")

	// 接回来：从 12 继续，不是从 0 重来。
	setSchedulable(t, txCtx, client, account, true)
	_, err = repo.TickActiveDays(txCtx, "2026-09-15", nil)
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

	_, err := repo.TickActiveDays(txCtx, "2026-09-14", nil)
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

	setProgramActiveDays(t, txCtx, client, veteran, slug, 40, "2026-09-14")
	setProgramActiveDays(t, txCtx, client, rookie, slug, 11, "2026-09-14")
	setProgramActiveDays(t, txCtx, client, tooNew, slug, 9, "2026-09-14")
	setProgramActiveDays(t, txCtx, client, alreadyPaid, slug, 50, "2026-09-14")

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
		ProgramSlug:   slug,
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
		setProgramActiveDays(t, txCtx, client, id, slug, 20+i, "2026-09-14")
	}

	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
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
	setProgramActiveDays(t, txCtx, client, claude, slug, 20, "2026-09-14")

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
	setProgramActiveDays(t, txCtx, client, openaiAccount.ID, slug, 20, "2026-09-14")

	anthropicOnly, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
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
		ProgramSlug:   slug,
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
	setProgramActiveDays(t, txCtx, client, rebound, slug, 20, "2026-09-14")
	setProgramActiveDays(t, txCtx, client, fresh, slug, 20, "2026-09-14")

	// 不排除任何人时，重挂的号是够格的——这正是要挡的那个形态。
	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
		RequestPrefix: prefix,
		Limit:         50,
	})
	require.NoError(t, err)
	assert.Contains(t, candidateIDs(got), rebound, "按账号计的名额挡不住解绑重挂")

	// 把拿满的人排除掉之后，重挂的号连同他名下别的号一起消失，诚实用户不受影响。
	got, err = repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays:   10,
		ProgramSlug:     slug,
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

// ---------------------------------------------------------------------------
// 按活动分桶
// ---------------------------------------------------------------------------

// 这一条是「能连着办几期活动」的地基：第二期的桶从 0 开始，不继承第一期攒下的天数。
//
// 顺带钉住一个只有真库能照出来的细节——**一期今天才开始的活动，必须能在总计已经
// 加过的那一天里把桶建起来**。整行要不要更新如果只看总计的 last_day，新活动的
// 第一天就永远缺一天，而且缺得无声无息。
func TestSupplyIncentive_TickCreatesPerProgramBuckets(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-bucket")
	account := mustCreateSupplyAccount(t, client, owner, "tick-bucket",
		service.SupplyStateActive, service.StatusActive, true)

	// 第一期跑三天。
	for _, day := range []string{"2026-10-01", "2026-10-02", "2026-10-03"} {
		_, err := repo.TickActiveDays(txCtx, day, []string{"first"})
		require.NoError(t, err)
	}
	assert.Equal(t, int64(3), activeDaysOf(t, txCtx, client, account))
	assert.Equal(t, int64(3), programActiveDaysOf(t, txCtx, client, account, "first"))
	assert.Equal(t, int64(-1), programActiveDaysOf(t, txCtx, client, account, "second"),
		"还没开始的活动不该有桶")

	// 第二期在 10-04 开始，与第一期并存。
	_, err := repo.TickActiveDays(txCtx, "2026-10-04", []string{"first", "second"})
	require.NoError(t, err)
	assert.Equal(t, int64(4), activeDaysOf(t, txCtx, client, account))
	assert.Equal(t, int64(4), programActiveDaysOf(t, txCtx, client, account, "first"))
	assert.Equal(t, int64(1), programActiveDaysOf(t, txCtx, client, account, "second"),
		"第二期必须从 0 开始，不能继承第一期的天数——否则新活动一上线就有人直接够格")
}

// 总计当天已经加过，仍然要能给一个当天才开始的活动建桶。
//
// 这是 WHERE 末尾那个 `OR EXISTS` 的唯一理由。没有它的话，运营在当天配好一期活动，
// 这期的第一天就被吞掉了——现象是「活动少发一天」，没有任何报错。
func TestSupplyIncentive_TickAddsNewBucketAfterTotalAlreadyTickedToday(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-late")
	account := mustCreateSupplyAccount(t, client, owner, "tick-late",
		service.SupplyStateActive, service.StatusActive, true)

	day := "2026-10-05"
	_, err := repo.TickActiveDays(txCtx, day, nil) // 今天没有任何活动在跑
	require.NoError(t, err)
	require.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account))

	// 同一天，运营配好了一期活动。
	_, err = repo.TickActiveDays(txCtx, day, []string{"late"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), activeDaysOf(t, txCtx, client, account), "总计当天不该加第二次")
	assert.Equal(t, int64(1), programActiveDaysOf(t, txCtx, client, account, "late"),
		"当天开始的活动，第一天必须记上")

	// 再跑一轮仍然是幂等的。
	_, err = repo.TickActiveDays(txCtx, day, []string{"late"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), programActiveDaysOf(t, txCtx, client, account, "late"))
}

// 桶不会因为活动从 settings 里被移掉而丢失：停止 +1，但天数留着，配回来接着涨。
func TestSupplyIncentive_TickKeepsBucketWhenProgramPaused(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "tick-pause-prog")
	account := mustCreateSupplyAccount(t, client, owner, "tick-pause-prog",
		service.SupplyStateActive, service.StatusActive, true)

	_, err := repo.TickActiveDays(txCtx, "2026-10-01", []string{"p"})
	require.NoError(t, err)
	_, err = repo.TickActiveDays(txCtx, "2026-10-02", nil) // 活动被移出 settings
	require.NoError(t, err)
	assert.Equal(t, int64(1), programActiveDaysOf(t, txCtx, client, account, "p"),
		"活动下架只是停止累加，桶不该被抹掉")

	_, err = repo.TickActiveDays(txCtx, "2026-10-03", []string{"p"}) // 配回来
	require.NoError(t, err)
	assert.Equal(t, int64(2), programActiveDaysOf(t, txCtx, client, account, "p"),
		"配回来要接着涨，不是从头再来")
}

// 门槛读的是**这期的桶**，不是账号总计。
//
// 拿总计当门槛的后果正是分桶要解决的问题：一期新活动刚开跑，老号凭着此前攒下的
// 总天数当场够格——「活动从今天算起」这句话就不成立了。
func TestSupplyIncentive_ListCandidatesReadsProgramBucketNotTotal(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "bucket-scope")
	slug := fmt.Sprintf("b%d", time.Now().UnixNano()%1e10)

	veteran := mustCreateSupplyAccount(t, client, owner, "bs-veteran",
		service.SupplyStateActive, service.StatusActive, true)
	// 总计很高（挂了很久），但这一期才跑了 2 天。
	setActiveDays(t, txCtx, client, veteran, 200, "2026-10-01")
	setProgramActiveDays(t, txCtx, client, veteran, slug, 2, "2026-10-01")

	got, err := repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
		RequestPrefix: service.SupplyIncentiveRequestPrefix(slug, 0),
		Limit:         50,
	})
	require.NoError(t, err)
	assert.NotContains(t, candidateIDs(got), veteran,
		"总计 200 天但这期只跑了 2 天，不该够格")

	setProgramActiveDays(t, txCtx, client, veteran, slug, 10, "2026-10-09")
	got, err = repo.ListCandidates(txCtx, service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
		RequestPrefix: service.SupplyIncentiveRequestPrefix(slug, 0),
		Limit:         50,
	})
	require.NoError(t, err)
	assert.Contains(t, candidateIDs(got), veteran)
}

// ---------------------------------------------------------------------------
// 只发给新供给者
// ---------------------------------------------------------------------------

// 「新」= 在起算日之前名下没有任何供给账号，**含已解绑的**。
//
// 含已解绑那一条是这组里最要紧的：不含的话，「先解绑再挂回来」就能把自己洗成新用户，
// 而解绑只是软删、account 行还在，真库里一查就知道——但 sqlmock 里两种写法长得一样。
func TestSupplyIncentive_ListCandidatesNewUsersOnly(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	slug := fmt.Sprintf("n%d", time.Now().UnixNano()%1e10)
	prefix := service.SupplyIncentiveRequestPrefix(slug, 0)
	startAt := time.Now().UTC().AddDate(0, 0, -10)

	newcomer := mustCreateSupplier(t, client, "nu-newcomer")
	veteran := mustCreateSupplier(t, client, "nu-veteran")
	rebinder := mustCreateSupplier(t, client, "nu-rebinder")

	// 三个号都是活动开始之后挂上来的，天数也都够。
	newAccount := mustCreateSupplyAccount(t, client, newcomer, "nu-new",
		service.SupplyStateActive, service.StatusActive, true)
	veteranNewAccount := mustCreateSupplyAccount(t, client, veteran, "nu-vet-new",
		service.SupplyStateActive, service.StatusActive, true)
	rebinderAccount := mustCreateSupplyAccount(t, client, rebinder, "nu-rebind-new",
		service.SupplyStateActive, service.StatusActive, true)

	// 老贡献者在活动开始前就有一个号（还在用）。
	veteranOldAccount := mustCreateSupplyAccount(t, client, veteran, "nu-vet-old",
		service.SupplyStateActive, service.StatusActive, true)
	// 洗白者在活动开始前有过一个号，**已经解绑**（软删）。
	rebinderOldAccount := mustCreateSupplyAccount(t, client, rebinder, "nu-rebind-old",
		service.SupplyStateActive, service.StatusActive, true)

	backdateAccount(t, txCtx, client, veteranOldAccount, startAt.AddDate(0, 0, -5))
	backdateAccount(t, txCtx, client, rebinderOldAccount, startAt.AddDate(0, 0, -5))
	softDeleteAccount(t, client, rebinderOldAccount)

	// 守住这组用例的前提：那个号必须**真的**被软删了、且创建时间真的在起算日之前。
	// 两者任一没生效，下面「洗白者拿不到」的断言就会因为完全不同的原因通过，
	// 而「含已解绑」这条规则被删掉时测试照样绿。
	require.Equal(t, int64(1), countSoftDeletedAccounts(t, txCtx, client, rebinderOldAccount))
	require.Equal(t, int64(1), countAccountsCreatedBefore(t, txCtx, client, rebinderOldAccount, startAt))

	for _, id := range []int64{newAccount, veteranNewAccount, rebinderAccount, veteranOldAccount} {
		setProgramActiveDays(t, txCtx, client, id, slug, 20, "2026-10-01")
	}

	query := service.SupplyIncentiveCandidateQuery{
		MinActiveDays: 10,
		ProgramSlug:   slug,
		RequestPrefix: prefix,
		Limit:         50,
	}

	// 不限新老时，四个够天数的号都在。
	all, err := repo.ListCandidates(txCtx, query)
	require.NoError(t, err)
	allIDs := candidateIDs(all)
	require.Contains(t, allIDs, newAccount)
	require.Contains(t, allIDs, veteranNewAccount)
	require.Contains(t, allIDs, rebinderAccount)

	// 只发新人时，只剩真正的新人。
	query.NewUsersOnly = true
	query.StartAt = startAt
	fresh, err := repo.ListCandidates(txCtx, query)
	require.NoError(t, err)
	freshIDs := candidateIDs(fresh)

	assert.Contains(t, freshIDs, newAccount, "活动开始后第一次挂号的人该拿到")
	assert.NotContains(t, freshIDs, veteranNewAccount,
		"老贡献者加挂新号也不算新人——摊薄的账在定价文档里算过")
	assert.NotContains(t, freshIDs, veteranOldAccount)
	assert.NotContains(t, freshIDs, rebinderAccount,
		"解绑只是软删，「先解绑再挂回来」不该把自己洗成新用户")
}

// backdateAccount 把账号的创建时间改到过去——「活动开始前就已经是供给者」这个状态
// 造不出来，除非直接改库（ent 的 create 钩子会盖上当前时间）。
func backdateAccount(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, at time.Time) {
	t.Helper()
	_, err := client.ExecContext(ctx,
		"UPDATE accounts SET created_at = $1 WHERE id = $2", at, accountID)
	require.NoError(t, err)
}

func countSoftDeletedAccounts(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client,
		"SELECT COUNT(*) FROM accounts WHERE id = $1 AND deleted_at IS NOT NULL", accountID)
	require.NoError(t, err)
	return n
}

func countAccountsCreatedBefore(
	t *testing.T, ctx context.Context, client *dbent.Client, accountID int64, before time.Time,
) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client,
		"SELECT COUNT(*) FROM accounts WHERE id = $1 AND created_at < $2", accountID, before)
	require.NoError(t, err)
	return n
}

// 升级安全：新版 tick 落在**现网已有的那份 extra** 上，不能弄坏任何东西。
//
// 这条是给上线那一刻准备的。现网账号的 extra 里除了旧形态的天数计数器
// （只有 days/last_day、没有 programs），还并排住着观察期探测、被动用量采样等一堆
// apexone_* 与非 apexone_* 的键。新语句用 `||` 逐层合并，理论上只覆盖它自己那三个
// 字段——但「理论上」在 jsonb 这种地方不值钱：写成 jsonb_build_object 直接赋值的话，
// 同一条语句会把兄弟键连同整份采样数据一起抹掉，而且不报错。
func TestSupplyIncentive_TickPreservesExistingExtraOnUpgrade(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncentiveRepository(client)

	owner := mustCreateSupplier(t, client, "upgrade")
	account := mustCreateSupplyAccount(t, client, owner, "upgrade",
		service.SupplyStateActive, service.StatusActive, true)

	// 照抄一份现网形状：旧计数器（无 programs）+ 观察期字段 + 采样字段。
	_, err := client.ExecContext(txCtx, fmt.Sprintf(`
UPDATE accounts SET extra = extra || jsonb_build_object(
    '%s', jsonb_build_object('days', 2, 'last_day', '2026-09-16'),
    '%s', 'active',
    '%s', 1,
    'passive_usage_7d_utilization', 0.16)
WHERE id = $1`,
		service.SupplyActiveDaysExtraKey, service.SupplyStateExtraKey,
		service.SupplyProbePassesExtraKey), account)
	require.NoError(t, err)

	// 升级后的第一轮：换了一天，且同时开始一期新活动。
	_, err = repo.TickActiveDays(txCtx, "2026-09-17", []string{"bind26q4"})
	require.NoError(t, err)

	assert.Equal(t, int64(3), activeDaysOf(t, txCtx, client, account),
		"旧计数器要接着涨，不是从 0 重来")
	assert.Equal(t, int64(1), programActiveDaysOf(t, txCtx, client, account, "bind26q4"),
		"新活动的桶要能在一份没有 programs 的旧 extra 上建起来")

	// 兄弟键一个都不能少。
	probePasses, err := scanInt64(txCtx, client, fmt.Sprintf(
		"SELECT COALESCE((extra->>'%s')::int, -1) FROM accounts WHERE id = $1",
		service.SupplyProbePassesExtraKey), account)
	require.NoError(t, err)
	assert.Equal(t, int64(1), probePasses, "观察期字段被新语句抹掉了")

	sampled, err := scanInt64(txCtx, client,
		"SELECT COUNT(*) FROM accounts WHERE id = $1 AND extra ? 'passive_usage_7d_utilization'", account)
	require.NoError(t, err)
	assert.Equal(t, int64(1), sampled, "非供给侧的采样字段被新语句抹掉了")

	state, err := scanInt64(txCtx, client, fmt.Sprintf(
		"SELECT COUNT(*) FROM accounts WHERE id = $1 AND extra->>'%s' = '%s'",
		service.SupplyStateExtraKey, service.SupplyStateActive), account)
	require.NoError(t, err)
	assert.Equal(t, int64(1), state, "接入状态被新语句改掉了")
}
