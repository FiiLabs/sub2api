//go:build integration

// APEXONE-EXT: 双边市场——管理端运营视图的真库测试。
//
// 这一组全是聚合查询，而聚合是**最容易静默算错**的一类代码：一个 JOIN 写成 INNER、
// 一个 FILTER 谓词漏了条件，结果仍然是一个长得很像样的数字，没有任何报错。运营会
// 照着它决定打多少钱、停哪个号。sqlmock 在这里一点用没有——它只会把我写的 SQL
// 原样还给我，连 `COUNT(*) FILTER (WHERE ...)` 是不是合法语法都不知道。
//
// 因此这里测的是**只有真库能回答**的四件事：
//
//   - jsonb 状态兜底：`COALESCE(NULLIF(extra->>'key', 空串), 'pending_review')` 到底把
//     没有状态的存量号算进哪一桶（算错就是看板报出一批没跑过观察期的「活跃号」）。
//   - 自营账号（owner_user_id IS NULL）一个都不能混进任何一个数字。
//   - 「供给者」的两个来源取并集：号全没了但钱包还有余额的人必须仍在名册里，
//     否则那笔待付负债从看板上整个消失。
//   - 流水窗口用的是**数据库时钟**（`NOW() - make_interval(days => $1)`）。
//
// 断言一律走「先取基线、再比增量」：这几个查询是全站聚合，写死绝对值等于假设
// 整个库只有这一个测试的数据，那个假设迟早被下一个测试文件打破。
package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// mustCreateSupplyAccount 建一个归属明确的供给账号。
//
// 归属与调度位都在建完之后用裸 SQL 写：mustCreateAccount 会把 Schedulable=false
// 翻成 true（见它自己的 if !a.Schedulable），而 owner_user_id 根本不在 service.Account 上。
func mustCreateSupplyAccount(t *testing.T, client *dbent.Client, owner int64, tag, state, status string, schedulable bool) int64 {
	t.Helper()

	extra := map[string]any{}
	if state != "" {
		extra[service.SupplyStateExtraKey] = state
	}
	account := mustCreateAccount(t, client, &service.Account{
		Name:        "supply-" + tag,
		Platform:    service.PlatformAnthropic,
		Status:      status,
		Extra:       extra,
		Credentials: map[string]any{"email_address": tag + "@upstream.test"},
	})

	_, err := client.ExecContext(context.Background(),
		"UPDATE accounts SET owner_user_id = $1, schedulable = $2 WHERE id = $3",
		owner, schedulable, account.ID)
	require.NoError(t, err, "attach supply account to owner")
	return account.ID
}

// 自营账号不进任何一个数字；没有状态的供给号兜底进 pending_review。
//
// 这两条是同一个错误的两面：把 owner_user_id IS NULL 的号算进来，看板会把整个
// 自营池报成「供给」；把状态缺失的号算成 active，看板会报出一批从没跑过观察期的
// 活跃号。两种情况下运营看到的都是一个合理的数字，而它是错的。
func TestSupplyAdmin_OverviewCountsOnlySupplierAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)

	before, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)

	// 自营账号：没有归属人，一个格子都不该动。
	mustCreateAccount(t, client, &service.Account{Name: "first-party", Platform: service.PlatformAnthropic})

	owner := mustCreateSupplier(t, client, "overview")
	// 状态缺失 —— 必须兜底成 pending_review。
	mustCreateSupplyAccount(t, client, owner, "ov-legacy", "", service.StatusActive, false)
	mustCreateSupplyAccount(t, client, owner, "ov-active", service.SupplyStateActive, service.StatusActive, true)
	// 排空中且上游已经坏了：既进 draining，也进 unhealthy——两个维度是正交的。
	mustCreateSupplyAccount(t, client, owner, "ov-drain", service.SupplyStateDraining, service.StatusError, false)
	mustCreateSupplyAccount(t, client, owner, "ov-retired", service.SupplyStateRetired, service.StatusActive, false)

	after, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)

	require.Equal(t, int64(4), after.Accounts.Total-before.Accounts.Total,
		"自营账号混进了供给账号总数")
	require.Equal(t, int64(1), after.Accounts.PendingReview-before.Accounts.PendingReview,
		"状态缺失的存量供给号没有兜底成 pending_review")
	require.Equal(t, int64(1), after.Accounts.Active-before.Accounts.Active)
	require.Equal(t, int64(1), after.Accounts.Draining-before.Accounts.Draining)
	require.Equal(t, int64(1), after.Accounts.Retired-before.Accounts.Retired)
	require.Equal(t, int64(1), after.Accounts.Unhealthy-before.Accounts.Unhealthy,
		"status <> active 的号没有被算成不健康")
	require.Equal(t, int64(1), after.Accounts.Schedulable-before.Accounts.Schedulable)
	require.Equal(t, int64(1), after.Suppliers-before.Suppliers)
}

// 看板顶部的供给者人数必须恰好等于名册翻页的总数。
//
// 两处一旦用了不同的定义，运营看到「37 个供给者」却只翻得出 31 行，会以为自己漏了
// 一页去反复刷新。这条断言的全部意义是把那个定义钉成一份。
func TestSupplyAdmin_OverviewSupplierCountMatchesRosterTotal(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)
	credits := NewSupplierCreditRepository(client)

	// 一个「号全没了但钱还欠着」的供给者：只有钱包，没有任何账号。
	walletOnly := mustCreateSupplier(t, client, "roster-walletonly")
	ok, err := credits.Accrue(txCtx, service.SupplierAccrueParams{
		SupplierUserID: walletOnly,
		RequestID:      fmt.Sprintf("req-walletonly-%d", time.Now().UnixNano()),
		BasisAmount:    10,
		ShareRatio:     0.7,
		FreezeHours:    24,
	})
	require.NoError(t, err)
	require.True(t, ok)

	// 一个只有账号、还没赚到过钱的供给者。
	accountOnly := mustCreateSupplier(t, client, "roster-accountonly")
	mustCreateSupplyAccount(t, client, accountOnly, "roster-ao", service.SupplyStateActive, service.StatusActive, true)

	overview, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)
	_, total, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Sort: service.SupplierRosterSortOwed, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, overview.Suppliers, total, "看板人数与名册总数用了两套定义")

	// 两个人都必须能被找到：一个只有钱包，一个只有账号。
	walletRows, walletTotal, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Keyword: "roster-walletonly", Sort: service.SupplierRosterSortOwed, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), walletTotal, "号全没了但余额还在的供给者从名册上消失了")
	require.Equal(t, walletOnly, walletRows[0].UserID)
	require.Equal(t, int64(0), walletRows[0].Accounts.Total)
	require.InDelta(t, 7.0, walletRows[0].Wallet.Frozen, 1e-6)
	require.InDelta(t, 7.0, walletRows[0].Wallet.History, 1e-6)
	require.NotNil(t, walletRows[0].LastAccrualAt)

	accountRows, accountTotal, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Keyword: "roster-accountonly", Sort: service.SupplierRosterSortOwed, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), accountTotal)
	require.Equal(t, int64(1), accountRows[0].Accounts.Total)
	require.Equal(t, int64(1), accountRows[0].Accounts.Active)
	require.InDelta(t, 0.0, accountRows[0].Wallet.History, 1e-6)
	require.Nil(t, accountRows[0].LastAccrualAt, "从没入过账却报出了最后入账时间")
}

// 按待付排序：欠得多的排前面。
//
// 排序片段是白名单映射出来的一段裸 SQL，别名写错在 Go 侧编译得过、在真库上直接报错，
// 所以四个排序键都得真的跑一遍。
func TestSupplyAdmin_RosterSortsByEveryWhitelistedKey(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)
	credits := NewSupplierCreditRepository(client)

	small := mustCreateSupplier(t, client, "rostersort-small")
	big := mustCreateSupplier(t, client, "rostersort-big")

	accrue := func(userID int64, basis float64, tag string) {
		t.Helper()
		ok, err := credits.Accrue(txCtx, service.SupplierAccrueParams{
			SupplierUserID: userID,
			RequestID:      fmt.Sprintf("req-%s-%d", tag, time.Now().UnixNano()),
			BasisAmount:    basis,
			ShareRatio:     0.5,
			FreezeHours:    24,
		})
		require.NoError(t, err)
		require.True(t, ok)
	}
	accrue(small, 4, "small")
	accrue(big, 40, "big")

	rows, total, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Keyword: "rostersort", Sort: service.SupplierRosterSortOwed, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Equal(t, big, rows[0].UserID, "按待付排序没把欠得多的排前面")
	require.Equal(t, small, rows[1].UserID)
	require.InDelta(t, 20.0, rows[0].Wallet.Frozen, 1e-6)

	// 其余三个键只要求「跑得起来且顺序稳定」——它们的排序列同样是裸 SQL 片段。
	for _, sort := range service.SupplierRosterSorts {
		sorted, sortedTotal, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
			Keyword: "rostersort", Sort: sort, Page: 1, PageSize: 20,
		})
		require.NoError(t, err, "排序键 %s 的 ORDER BY 片段在真库上不合法", sort)
		require.Equal(t, int64(2), sortedTotal)
		require.Len(t, sorted, 2)
	}

	// 分页：每页一条，两页各一个人，不重不漏。
	first, _, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Keyword: "rostersort", Sort: service.SupplierRosterSortOwed, Page: 1, PageSize: 1,
	})
	require.NoError(t, err)
	second, _, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Keyword: "rostersort", Sort: service.SupplierRosterSortOwed, Page: 2, PageSize: 1,
	})
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].UserID, second[0].UserID, "翻页翻出了同一个人")
}

// 未知排序键必须报错，不能静默回落。
//
// 与身份键那条规则同源：一个静默回落的白名单，等于「前端改了键名、后端没跟上」
// 这件事永远没人发现。
func TestSupplyAdmin_RosterRejectsUnknownSort(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewSupplierAdminRepository(tx.Client())

	_, _, err := repo.ListSuppliers(txCtx, service.SupplierRosterFilter{
		Sort: service.SupplierRosterSort("available_credit DESC; DROP TABLE users"),
		Page: 1, PageSize: 20,
	})
	require.Error(t, err)
}

// 账号明细的三个筛子各自成立，且观察期字段真的从 jsonb 里解出来了。
func TestSupplyAdmin_AccountsFilterByStateHealthAndOwner(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)

	owner := mustCreateSupplier(t, client, "acclist")
	other := mustCreateSupplier(t, client, "acclist-other")

	legacyID := mustCreateSupplyAccount(t, client, owner, "al-legacy", "", service.StatusActive, false)
	activeID := mustCreateSupplyAccount(t, client, owner, "al-active", service.SupplyStateActive, service.StatusActive, true)
	brokenID := mustCreateSupplyAccount(t, client, owner, "al-broken", service.SupplyStateActive, service.StatusError, false)
	mustCreateSupplyAccount(t, client, other, "al-other", service.SupplyStateActive, service.StatusActive, true)

	// 观察期字段写进 extra——运营页要靠它回答「这个号卡在哪一步」。
	since := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	_, err := client.ExecContext(ctx, fmt.Sprintf(
		`UPDATE accounts SET extra = extra || jsonb_build_object('%s', $1::text, '%s', 2, '%s', $2::text) WHERE id = $3`,
		service.SupplyProbationSinceExtraKey, service.SupplyProbePassesExtraKey, service.SupplyProbeErrorExtraKey),
		since.Format(time.RFC3339), "upstream 401", legacyID)
	require.NoError(t, err)

	// 只看这个人的号。
	owned, ownedTotal, err := repo.ListAccounts(txCtx, service.SupplyAccountAdminFilter{
		OwnerUserID: owner, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), ownedTotal, "归属筛子漏进了别人的号")
	require.Len(t, owned, 3)

	// 状态缺失的号必须能被 pending_review 筛出来——观察期页面靠这个筛子。
	pending, pendingTotal, err := repo.ListAccounts(txCtx, service.SupplyAccountAdminFilter{
		OwnerUserID: owner, State: service.SupplyStatePendingReview, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), pendingTotal)
	require.Equal(t, legacyID, pending[0].ID)
	require.Equal(t, service.SupplyStatePendingReview, pending[0].SupplyState)
	require.NotNil(t, pending[0].ProbationSince)
	require.WithinDuration(t, since, *pending[0].ProbationSince, time.Second)
	require.Equal(t, 2, pending[0].ProbePasses, "jsonb 里的数字没被解出来")
	require.Equal(t, "upstream 401", pending[0].ProbeError)
	require.Equal(t, "al-legacy@upstream.test", pending[0].EmailAddress)
	require.Equal(t, owner, pending[0].OwnerUserID)
	require.NotEmpty(t, pending[0].OwnerEmail, "运营手上只有邮箱，没有 user_id")

	// 「谁的号在被封」。
	unhealthy, unhealthyTotal, err := repo.ListAccounts(txCtx, service.SupplyAccountAdminFilter{
		OwnerUserID: owner, Health: service.SupplyAccountHealthUnhealthy, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), unhealthyTotal)
	require.Equal(t, brokenID, unhealthy[0].ID)
	require.Equal(t, service.StatusError, unhealthy[0].Status)

	healthy, healthyTotal, err := repo.ListAccounts(txCtx, service.SupplyAccountAdminFilter{
		OwnerUserID: owner, Health: service.SupplyAccountHealthHealthy, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), healthyTotal)
	require.NotEqual(t, brokenID, healthy[0].ID)
	require.NotEqual(t, brokenID, healthy[1].ID)

	// 健康 + 已入池 = 真正在服务流量的那一批。
	servingIDs := map[int64]bool{}
	serving, _, err := repo.ListAccounts(txCtx, service.SupplyAccountAdminFilter{
		OwnerUserID: owner, State: service.SupplyStateActive,
		Health: service.SupplyAccountHealthHealthy, Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	for _, v := range serving {
		servingIDs[v.ID] = true
	}
	require.True(t, servingIDs[activeID])
	require.False(t, servingIDs[brokenID])
}

// 全站流水：不传 user_id 看所有人，传了就只看一个人。
//
// 这是运营视图与供给者视图**唯一**的语义差别，也是最危险的一处：如果两边共用了
// 同一个 filter，供给者侧任何一处漏传 user_id 的 bug 就会从「查不到」变成「看到全站的账」。
func TestSupplyAdmin_LedgerSpansAllUsersAndNarrowsToOne(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)
	credits := NewSupplierCreditRepository(client)

	alice := mustCreateSupplier(t, client, "ledger-alice")
	bob := mustCreateSupplier(t, client, "ledger-bob")

	aliceReq := fmt.Sprintf("req-alice-%d", time.Now().UnixNano())
	bobReq := fmt.Sprintf("req-bob-%d", time.Now().UnixNano())
	for _, spec := range []struct {
		userID    int64
		requestID string
		basis     float64
	}{{alice, aliceReq, 10}, {bob, bobReq, 20}} {
		ok, err := credits.Accrue(txCtx, service.SupplierAccrueParams{
			SupplierUserID: spec.userID,
			RequestID:      spec.requestID,
			ConsumerUserID: &spec.userID,
			BasisAmount:    spec.basis,
			ShareRatio:     0.5,
			FreezeHours:    24,
		})
		require.NoError(t, err)
		require.True(t, ok)
	}

	all, allTotal, err := repo.ListLedger(txCtx, service.SupplyAdminLedgerFilter{Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.GreaterOrEqual(t, allTotal, int64(2))
	seen := map[int64]bool{}
	for _, entry := range all {
		seen[entry.UserID] = true
	}
	require.True(t, seen[alice] && seen[bob], "不传 user_id 时没有看到全站流水")

	mine, mineTotal, err := repo.ListLedger(txCtx, service.SupplyAdminLedgerFilter{
		UserID: alice, Page: 1, PageSize: 100,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), mineTotal)
	require.Equal(t, alice, mine[0].UserID)
	require.NotEmpty(t, mine[0].UserEmail, "流水没带出收款人邮箱")
	require.Equal(t, service.SupplierCreditActionAccrue, mine[0].Action)
	require.InDelta(t, 5.0, mine[0].Amount, 1e-6)
	require.NotNil(t, mine[0].BasisAmount)
	require.InDelta(t, 10.0, *mine[0].BasisAmount, 1e-6)

	// 对账时手上通常只有一个 request_id。
	byRequest, byRequestTotal, err := repo.ListLedger(txCtx, service.SupplyAdminLedgerFilter{
		RequestID: bobReq, Page: 1, PageSize: 100,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), byRequestTotal)
	require.Equal(t, bob, byRequest[0].UserID)

	// 动作筛子：这一批里没有提现。
	_, withdrawTotal, err := repo.ListLedger(txCtx, service.SupplyAdminLedgerFilter{
		UserID: alice, Action: service.SupplierCreditActionWithdraw, Page: 1, PageSize: 100,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), withdrawTotal)
}

// 流水窗口用的是数据库时钟，且窗口外的行真的被排除。
//
// 这条断言存在的理由很具体：`NOW() - make_interval(days => $1)` 里的 $1 是一个
// 需要被推断成 integer 的参数。推断不出来在 Go 侧毫无征兆，在真库上是一个运行期错误；
// 而如果谓词写反了，看板会把历史上所有入账都报成「本期新增」。
func TestSupplyAdmin_OverviewWindowExcludesOlderLedger(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierAdminRepository(client)
	credits := NewSupplierCreditRepository(client)

	before, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)

	supplier := mustCreateSupplier(t, client, "window")
	requestID := fmt.Sprintf("req-window-%d", time.Now().UnixNano())
	ok, err := credits.Accrue(txCtx, service.SupplierAccrueParams{
		SupplierUserID: supplier,
		RequestID:      requestID,
		BasisAmount:    100,
		ShareRatio:     0.6,
		FreezeHours:    24,
	})
	require.NoError(t, err)
	require.True(t, ok)

	fresh, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)
	require.InDelta(t, 60.0, fresh.Window.Accrued-before.Window.Accrued, 1e-6)
	require.Equal(t, 30, fresh.Window.Days)

	// 把这一笔挪到 60 天前：30 天窗口里就不该再看见它。
	_, err = client.ExecContext(ctx,
		`UPDATE supplier_credit_ledger SET created_at = NOW() - INTERVAL '60 days' WHERE request_id = $1`,
		requestID)
	require.NoError(t, err)

	aged, err := repo.Overview(txCtx, 30)
	require.NoError(t, err)
	require.InDelta(t, 0.0, aged.Window.Accrued-before.Window.Accrued, 1e-6,
		"窗口外的入账仍然被算进了本期新增")

	// 但把窗口拉到 90 天，它必须回来——证明排除它的是窗口，不是别的什么。
	wide, err := repo.Overview(txCtx, 90)
	require.NoError(t, err)
	require.InDelta(t, 60.0, wide.Window.Accrued-before.Window.Accrued, 1e-6)
	require.Equal(t, 90, wide.Window.Days)
}
