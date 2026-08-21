//go:build integration

// APEXONE-EXT: 双边市场——失效事件仓储的真库测试。
//
// 这一组要钉死的是本模块唯一一个会自己走进死循环的性质：**开与关必须互补**
// （理由见 supplier_incident_repo.go 顶部——开得比关宽 = 每五分钟给人发一封信，
// 关得比开宽 = 「当前坏着」只增不减）。这个性质在单元测试里根本表达不出来，
// 它是两条 SQL 谓词之间的关系，只有真库能同时执行它们。
//
// 另外三件也只有真库能回答：
//
//   - `ON CONFLICT (account_id) WHERE resolved_at IS NULL DO NOTHING` 到底命中了
//     那条部分唯一索引没有。索引名写错、谓词写歪，Postgres 会直接报错；
//     而在 sqlmock 里两种写法都"通过"。
//   - LEFT JOIN 让被删掉的号的事件能被关掉（INNER JOIN 会让它永远挂着）。
//   - 三个 CTE 的榜单里 open_counts **不带窗口条件**——一个坏了三个月的号
//     不该因为它是窗口之前坏的就从「现在还有几个开着」里消失。
//
// 断言一律走增量：这些是全站聚合，写死绝对值等于假设库里只有这一个测试的数据。
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

// openIncidentCount 数某个号当前有几条未结事件。
// 「几条」而不是「有没有」是刻意的：那条部分唯一索引一旦失效，这里会变成 2。
func openIncidentCount(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client,
		"SELECT COUNT(*) FROM supplier_account_incidents WHERE account_id = $1 AND resolved_at IS NULL", accountID)
	require.NoError(t, err)
	return n
}

func incidentCountFor(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64) int64 {
	t.Helper()
	n, err := scanInt64(ctx, client,
		"SELECT COUNT(*) FROM supplier_account_incidents WHERE account_id = $1", accountID)
	require.NoError(t, err)
	return n
}

// ============================================================================
// 开事件
// ============================================================================

// 谁该被开事件、谁不该——六个号一次说清。
//
// 每一条排除都有一个具体的事故与之对应：
//   - 自营号（无归属人）被算进来 = 平台自己的号坏了却去骚扰某个供给者；
//   - 空状态被算成坏 = 功能上线的第一分钟给一批存量供给者群发邮件；
//   - retired 被算成坏 = 一个自己按了解绑的人收到「你的号坏了」；
//   - draining 被漏掉 = 排空中的号还在接单，坏了却没人被通知。
func TestSupplyIncident_OpenSelectsOnlyBrokenSupplyAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-open")

	// 自营号：坏了也不开事件——它没有主人。
	firstParty := mustCreateAccount(t, client, &service.Account{
		Name: "first-party-broken", Platform: service.PlatformAnthropic, Status: service.StatusError,
	})
	// 空状态：历史遗留，算健康。
	//
	// 这里必须直接改库：mustCreateAccount 会把空 status 兜底成 active（见
	// fixtures_integration_test.go），于是"传 '' 进去"造出来的其实是一个 active 号，
	// 这条断言就变成了在测 healthy 的重复件——"空状态算坏"的变异因此活了下来一次。
	legacy := mustCreateSupplyAccount(t, client, owner, "inc-legacy", service.SupplyStateActive, "", true)
	_, err := client.ExecContext(txCtx, "UPDATE accounts SET status = '' WHERE id = $1", legacy)
	require.NoError(t, err, "把兜底出来的 active 改回真正的空状态")
	healthy := mustCreateSupplyAccount(t, client, owner, "inc-ok", service.SupplyStateActive, service.StatusActive, true)
	broken := mustCreateSupplyAccount(t, client, owner, "inc-err", service.SupplyStateActive, service.StatusError, false)
	disabled := mustCreateSupplyAccount(t, client, owner, "inc-dis", service.SupplyStateActive, service.StatusDisabled, false)
	draining := mustCreateSupplyAccount(t, client, owner, "inc-drain", service.SupplyStateDraining, service.StatusError, true)
	retired := mustCreateSupplyAccount(t, client, owner, "inc-ret", service.SupplyStateRetired, service.StatusError, false)

	opened, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(3), opened, "只有 error/disabled 的在役供给号该被开事件")

	assert.Equal(t, int64(1), openIncidentCount(t, txCtx, client, broken))
	assert.Equal(t, int64(1), openIncidentCount(t, txCtx, client, disabled))
	assert.Equal(t, int64(1), openIncidentCount(t, txCtx, client, draining), "排空中的号还在接单，坏了要通知")

	assert.Zero(t, incidentCountFor(t, txCtx, client, firstParty.ID), "自营号不该有主人被通知")
	assert.Zero(t, incidentCountFor(t, txCtx, client, legacy), "空状态算健康")
	assert.Zero(t, incidentCountFor(t, txCtx, client, healthy))
	assert.Zero(t, incidentCountFor(t, txCtx, client, retired), "他自己下的线，不该收到通知")
}

// 一个号同时只能有一条未结事件——这条由部分唯一索引 + ON CONFLICT 保证。
//
// 跑三轮：扫描每 5 分钟一次，多实例下还会并发。若这条不成立，供给者收到的是
// 每五分钟一封同样的邮件。
func TestSupplyIncident_OpenIsIdempotentWhileStillBroken(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-idem")
	broken := mustCreateSupplyAccount(t, client, owner, "idem", service.SupplyStateActive, service.StatusError, false)

	first, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), first)

	for range 2 {
		again, err := repo.OpenIncidents(txCtx, 100)
		require.NoError(t, err)
		assert.Zero(t, again, "同一个号不该被反复开事件")
	}
	assert.Equal(t, int64(1), incidentCountFor(t, txCtx, client, broken))
}

// 事件快照的是发现当时的样子：号后来被改名、状态又变了，那条事件仍然说得清
// 「当时发生了什么」。这也是台账里不做外键的原因（见迁移文件头）。
func TestSupplyIncident_OpenSnapshotsAccountFacts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-snap")
	accountID := mustCreateSupplyAccount(t, client, owner, "snap", service.SupplyStateActive, service.StatusError, false)
	_, err := client.ExecContext(ctx, "UPDATE accounts SET error_message = $1 WHERE id = $2",
		"upstream said no", accountID)
	require.NoError(t, err)

	_, err = repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	rows, _, err := repo.List(txCtx, service.SupplierIncidentFilter{AccountID: accountID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.Equal(t, owner, rows[0].UserID)
	assert.Equal(t, "supply-snap", rows[0].AccountName)
	assert.Equal(t, service.PlatformAnthropic, rows[0].Platform)
	assert.Equal(t, service.StatusError, rows[0].Status)
	assert.Equal(t, "upstream said no", rows[0].ErrorMessage)
	assert.True(t, rows[0].Open())
}

// 超长的上游错误必须被截断而不是让 INSERT 失败。
// 上游返回一整页 HTML 是常事，而 error_message 是 TEXT——真正的风险不是列宽，
// 是把一大坨东西原样搬进台账再原样发出去。
func TestSupplyIncident_OpenTruncatesUpstreamError(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-long")
	accountID := mustCreateSupplyAccount(t, client, owner, "long", service.SupplyStateActive, service.StatusError, false)

	long := ""
	for range 200 {
		long += "0123456789"
	}
	_, err := client.ExecContext(ctx, "UPDATE accounts SET error_message = $1 WHERE id = $2", long, accountID)
	require.NoError(t, err)

	_, err = repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	rows, _, err := repo.List(txCtx, service.SupplierIncidentFilter{AccountID: accountID, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Len(t, rows[0].ErrorMessage, service.SupplierIncidentErrorMaxLen)
}

// ============================================================================
// 关事件 —— 与开互补
// ============================================================================

// 号还坏着的时候关不掉。这是"互补"这条性质最直接的一半：
// 如果关的谓词比开的宽，事件会在开出来的同一轮里被关掉，下一轮再开一条——
// 每五分钟一封邮件。
func TestSupplyIncident_ResolveDoesNotTouchStillBrokenAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-stable")
	broken := mustCreateSupplyAccount(t, client, owner, "stable", service.SupplyStateActive, service.StatusError, false)

	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	// 状态一个字都没变，连跑两轮完整的扫描顺序（先关后开）。
	for range 2 {
		resolved, err := repo.ResolveIncidents(txCtx, 100)
		require.NoError(t, err)
		assert.Zero(t, resolved, "号还坏着，事件不该被关")

		opened, err := repo.OpenIncidents(txCtx, 100)
		require.NoError(t, err)
		assert.Zero(t, opened, "事件还开着，不该再开一条")
	}
	assert.Equal(t, int64(1), incidentCountFor(t, txCtx, client, broken))
}

// 号恢复之后事件必须被关掉，且再坏一次要开出**第二条**——
// 反复坏是最强的信号，按号去重会把它压成一条（见 SupplierIncidentRate 的口径）。
func TestSupplyIncident_ResolveClosesRecoveredAndReopensOnRelapse(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-relapse")
	accountID := mustCreateSupplyAccount(t, client, owner, "relapse", service.SupplyStateActive, service.StatusError, false)

	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	_, err = client.ExecContext(ctx, "UPDATE accounts SET status = $1 WHERE id = $2", service.StatusActive, accountID)
	require.NoError(t, err)

	resolved, err := repo.ResolveIncidents(txCtx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)
	assert.Zero(t, openIncidentCount(t, txCtx, client, accountID))

	// 再坏一次。
	_, err = client.ExecContext(ctx, "UPDATE accounts SET status = $1 WHERE id = $2", service.StatusError, accountID)
	require.NoError(t, err)

	opened, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), opened, "已结的事件不占那条部分唯一索引，复发要开新的一条")
	assert.Equal(t, int64(2), incidentCountFor(t, txCtx, client, accountID))
}

// 主人自己把号下线（retired）之后，那条事件也该关掉——他知道自己按了那个按钮。
// 这一支是 healed 谓词里 supply_state = retired 的落点。
func TestSupplyIncident_ResolveClosesWhenOwnerRetiresTheAccount(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-retire")
	accountID := mustCreateSupplyAccount(t, client, owner, "retire", service.SupplyStateActive, service.StatusError, false)

	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	_, err = client.ExecContext(ctx,
		fmt.Sprintf("UPDATE accounts SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{%s}', '\"%s\"') WHERE id = $1",
			service.SupplyStateExtraKey, service.SupplyStateRetired), accountID)
	require.NoError(t, err)

	resolved, err := repo.ResolveIncidents(txCtx, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resolved)
	assert.Zero(t, openIncidentCount(t, txCtx, client, accountID))
}

// 号被删（软删）之后事件也要关掉。LEFT JOIN 就是为这条存在的：
// 用 INNER JOIN 的话这条事件会永远留在「当前坏着」里，指向一个不存在的号。
func TestSupplyIncident_ResolveClosesIncidentsOfDeletedAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-del")
	accountID := mustCreateSupplyAccount(t, client, owner, "del", service.SupplyStateActive, service.StatusError, false)

	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	_, err = client.ExecContext(ctx, "UPDATE accounts SET deleted_at = NOW() WHERE id = $1", accountID)
	require.NoError(t, err)

	resolved, err := repo.ResolveIncidents(txCtx, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resolved)
}

// 归属被摘掉（号退回自营池）之后同样要关：他已经不是这个号的主人了。
func TestSupplyIncident_ResolveClosesWhenOwnershipIsRemoved(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-unown")
	accountID := mustCreateSupplyAccount(t, client, owner, "unown", service.SupplyStateActive, service.StatusError, false)

	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	_, err = client.ExecContext(ctx, "UPDATE accounts SET owner_user_id = NULL WHERE id = $1", accountID)
	require.NoError(t, err)

	resolved, err := repo.ResolveIncidents(txCtx, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resolved)
}

// ============================================================================
// 通知队列
// ============================================================================

// 待发队列只含「未结 + 没发过」，按发现时刻升序（积压时先通知停得最久的那个人）。
func TestSupplyIncident_PendingNoticeQueue(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-notice")

	// 三条人造事件：一条已通知、一条已结、两条待发（发现时刻一早一晚）。
	insert := func(accountID int64, detected string, notified, resolved bool) {
		t.Helper()
		_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at, notified_at, resolved_at)
VALUES ($1, $2, $3, 'anthropic', 'error', NOW() - $4::interval,
        CASE WHEN $5 THEN NOW() ELSE NULL END,
        CASE WHEN $6 THEN NOW() ELSE NULL END)`,
			accountID, owner, fmt.Sprintf("acc-%d", accountID), detected, notified, resolved)
		require.NoError(t, err)
	}
	insert(910001, "10 minutes", false, false) // 待发（较晚）
	insert(910002, "3 hours", false, false)    // 待发（最早）
	insert(910003, "1 hour", true, false)      // 已通知
	insert(910004, "1 hour", false, true)      // 已结

	pending, err := repo.ListPendingNotice(txCtx, 100)
	require.NoError(t, err)

	var got []int64
	for _, p := range pending {
		if p.AccountID >= 910001 && p.AccountID <= 910004 {
			got = append(got, p.AccountID)
		}
	}
	assert.Equal(t, []int64{910002, 910001}, got, "只发未结且没发过的，最早出事的排最前")
}

// MarkNotified 把事件移出待发队列；重复调用不覆盖第一次的时刻。
//
// 记的是**第一封**信发出去的时刻——两个实例撞在一起时两封信都已经发出去了，
// 这条闸拦不住那个，但它保证台账上的时刻是真实的第一次。
func TestSupplyIncident_MarkNotifiedKeepsFirstTimestamp(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-mark")
	accountID := mustCreateSupplyAccount(t, client, owner, "mark", service.SupplyStateActive, service.StatusError, false)
	_, err := repo.OpenIncidents(txCtx, 100)
	require.NoError(t, err)

	pending, err := repo.ListPendingNotice(txCtx, 100)
	require.NoError(t, err)
	var incidentID int64
	for _, p := range pending {
		if p.AccountID == accountID {
			incidentID = p.ID
		}
	}
	require.NotZero(t, incidentID)

	// 先人为写一个明显更早的时刻，再让 MarkNotified 试着覆盖它。
	// 事务里的 NOW() 是事务开始时刻，两次调用取不到不同的值——用一个人造的
	// 旧时刻才能真正观察到「不覆盖」。
	_, err = client.ExecContext(ctx,
		"UPDATE supplier_account_incidents SET notified_at = NOW() - interval '2 hours' WHERE id = $1", incidentID)
	require.NoError(t, err)

	require.NoError(t, repo.MarkNotified(txCtx, incidentID))

	var elapsed float64
	rows, err := client.QueryContext(ctx,
		"SELECT EXTRACT(EPOCH FROM (NOW() - notified_at)) FROM supplier_account_incidents WHERE id = $1", incidentID)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&elapsed))
	require.NoError(t, rows.Close())
	assert.Greater(t, elapsed, 3600.0, "notified_at 被第二次调用覆盖成了当前时刻")

	// 已通知的不再出现在待发队列里。
	pending, err = repo.ListPendingNotice(txCtx, 100)
	require.NoError(t, err)
	for _, p := range pending {
		assert.NotEqual(t, incidentID, p.ID)
	}
}

// ============================================================================
// 明细列表
// ============================================================================

// 筛选与排序：未结的排最前，时间筛的是 detected_at。
//
// 按 resolved_at 筛会漏掉所有还没恢复的号——那正是运营最想看的那一批。
func TestSupplyIncident_ListFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-list")
	other := mustCreateSupplier(t, client, "inc-list-other")

	insert := func(user, accountID int64, detectedAgo string, resolved bool) {
		t.Helper()
		_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at, resolved_at)
VALUES ($1, $2, 'x', 'anthropic', 'error', NOW() - $3::interval,
        CASE WHEN $4 THEN NOW() ELSE NULL END)`, accountID, user, detectedAgo, resolved)
		require.NoError(t, err)
	}
	insert(owner, 920001, "1 hour", true)   // 已结、较新
	insert(owner, 920002, "2 hours", false) // 未结、较旧
	insert(owner, 920003, "50 days", false) // 未结、远在窗口之外
	insert(other, 920004, "1 hour", false)  // 别人的

	rows, total, err := repo.List(txCtx, service.SupplierIncidentFilter{UserID: owner, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, int64(3), total, "user_id 筛选把别人的算进来了")
	require.Len(t, rows, 3)
	assert.True(t, rows[0].Open(), "未结的必须排在最前")
	assert.True(t, rows[1].Open())
	assert.False(t, rows[2].Open())

	openOnly, total, err := repo.List(txCtx, service.SupplierIncidentFilter{UserID: owner, OpenOnly: true, Page: 1, PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	for _, row := range openOnly {
		assert.True(t, row.Open())
	}

	// 时间窗：只要最近 24 小时里发现的。
	start := time.Now().Add(-24 * time.Hour)
	windowed, total, err := repo.List(txCtx, service.SupplierIncidentFilter{
		UserID: owner, StartAt: &start, Page: 1, PageSize: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total, "detected_at 窗口没生效")
	for _, row := range windowed {
		assert.NotEqual(t, int64(920003), row.AccountID)
	}

	byAccount, total, err := repo.List(txCtx, service.SupplierIncidentFilter{AccountID: 920004, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	assert.Equal(t, other, byAccount[0].UserID)
}

// 分页真的在翻页，而不是每页都还给你同一批。
func TestSupplyIncident_ListPaginates(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-page")
	for i := range 5 {
		_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at)
VALUES ($1, $2, 'x', 'anthropic', 'error', NOW() - ($3 || ' minutes')::interval)`,
			930000+i, owner, i)
		require.NoError(t, err)
	}

	first, total, err := repo.List(txCtx, service.SupplierIncidentFilter{UserID: owner, Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, first, 2)

	second, _, err := repo.List(txCtx, service.SupplierIncidentFilter{UserID: owner, Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.NotEqual(t, first[0].ID, second[0].ID)
	assert.NotEqual(t, first[1].ID, second[0].ID)
}

// ============================================================================
// 封禁率报表
// ============================================================================

// 四个计数与榜单。
//
// 最要紧的一条：open_counts **不带窗口**。一个 50 天前坏掉、至今没好的号，
// 在 30 天窗口的报表里不算「窗口内新开」，但必须仍然算在「现在还开着」里——
// 否则运营看到的是「当前 0 个坏着」，而实际上有一个人已经三个月没收入了。
func TestSupplyIncident_SummaryCountsAndTop(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	before, err := repo.Summary(txCtx, 30, 10)
	require.NoError(t, err)

	heavy := mustCreateSupplier(t, client, "inc-heavy")
	light := mustCreateSupplier(t, client, "inc-light")

	// heavy 名下两个在役号，窗口内出了三次事，其中一次还开着。
	mustCreateSupplyAccount(t, client, heavy, "heavy-a", service.SupplyStateActive, service.StatusActive, true)
	mustCreateSupplyAccount(t, client, heavy, "heavy-b", service.SupplyStateActive, service.StatusActive, true)
	// light 名下一个号，窗口内一次事，已结。
	mustCreateSupplyAccount(t, client, light, "light-a", service.SupplyStateActive, service.StatusActive, true)

	insert := func(user, accountID int64, detectedAgo string, resolved bool) {
		t.Helper()
		_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at, resolved_at)
VALUES ($1, $2, 'x', 'anthropic', 'error', NOW() - $3::interval,
        CASE WHEN $4 THEN NOW() ELSE NULL END)`, accountID, user, detectedAgo, resolved)
		require.NoError(t, err)
	}
	insert(heavy, 940001, "1 hour", true)
	insert(heavy, 940002, "2 days", true)
	insert(heavy, 940003, "3 days", false)
	insert(light, 940004, "1 day", true)
	// 窗口之外、且还开着 —— 只该进 Open，不该进 Opened。
	insert(light, 940005, "50 days", false)

	after, err := repo.Summary(txCtx, 30, 10)
	require.NoError(t, err)

	assert.Equal(t, int64(4), after.Opened-before.Opened, "窗口外的那条被算进了「窗口内新开」")
	assert.Equal(t, int64(3), after.Resolved-before.Resolved)
	assert.Equal(t, int64(2), after.Open-before.Open, "窗口外仍开着的那条从「当前坏着」里消失了")
	assert.Equal(t, int64(2), after.Suppliers-before.Suppliers)
	assert.Equal(t, int64(3), after.Accounts-before.Accounts)

	var heavyRow, lightRow *service.SupplierIncidentRate
	for i := range after.Top {
		switch after.Top[i].UserID {
		case heavy:
			heavyRow = &after.Top[i]
		case light:
			lightRow = &after.Top[i]
		}
	}
	require.NotNil(t, heavyRow)
	require.NotNil(t, lightRow)

	assert.Equal(t, int64(3), heavyRow.Incidents)
	assert.Equal(t, int64(1), heavyRow.OpenIncidents)
	assert.Equal(t, int64(2), heavyRow.Accounts)
	assert.InDelta(t, 1.5, heavyRow.Rate, 0.001, "比率 = 窗口内事件数 / 在役号数")

	assert.Equal(t, int64(1), lightRow.Incidents, "窗口外的那条不该算进窗口内事件数")
	assert.Equal(t, int64(1), lightRow.OpenIncidents, "open_counts 不带窗口")
	assert.InDelta(t, 1.0, lightRow.Rate, 0.001)

	// 榜按事件数倒序：heavy 必须排在 light 前面。
	heavyPos, lightPos := -1, -1
	for i := range after.Top {
		if after.Top[i].UserID == heavy {
			heavyPos = i
		}
		if after.Top[i].UserID == light {
			lightPos = i
		}
	}
	assert.Less(t, heavyPos, lightPos, "榜没有按事件数倒序")
}

// 一个名下已经没有号（全解绑了）的人仍然要能出现在榜上，且不许除零。
//
// 这是最容易 500 的一行：`incidents / accounts` 在 accounts = 0 时炸掉，
// 而它恰恰发生在「一个刷完坏号就跑的人」身上——正是这张榜要抓的那个人。
func TestSupplyIncident_SummaryHandlesSupplierWithNoAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	churner := mustCreateSupplier(t, client, "inc-churn")
	_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at)
VALUES (950001, $1, 'gone', 'anthropic', 'error', NOW())`, churner)
	require.NoError(t, err)

	summary, err := repo.Summary(txCtx, 30, 100)
	require.NoError(t, err)

	var row *service.SupplierIncidentRate
	for i := range summary.Top {
		if summary.Top[i].UserID == churner {
			row = &summary.Top[i]
		}
	}
	require.NotNil(t, row, "号全没了的人也必须留在榜上")
	assert.Zero(t, row.Accounts)
	assert.Zero(t, row.Rate, "没有号时比率是 0 而不是一次除零")
	assert.Equal(t, int64(1), row.Incidents)
	assert.NotEmpty(t, row.Email, "榜上要认得出这是谁")
}

// ============================================================================
// 熔断判据
// ============================================================================

// 数的是**事件次数**（不是坏号个数），窗口边界之外的不算，别人的不算。
//
// 按号去重会把「同一个号反复坏」压成 1——那正是最强的信号。
func TestSupplyIncident_CountRecentByUser(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierIncidentRepository(client)

	owner := mustCreateSupplier(t, client, "inc-count")
	other := mustCreateSupplier(t, client, "inc-count-other")

	insert := func(user, accountID int64, detectedAgo string) {
		t.Helper()
		_, err := client.ExecContext(ctx, `
INSERT INTO supplier_account_incidents (account_id, user_id, account_name, platform, status, detected_at, resolved_at)
VALUES ($1, $2, 'x', 'anthropic', 'error', NOW() - $3::interval, NOW())`, accountID, user, detectedAgo)
		require.NoError(t, err)
	}
	// 同一个号坏了两次 —— 必须数成 2。
	insert(owner, 960001, "1 hour")
	insert(owner, 960001, "2 hours")
	insert(owner, 960002, "3 hours")
	insert(owner, 960003, "10 days") // 窗口外
	insert(other, 960004, "1 hour")  // 别人的

	since := time.Now().Add(-24 * time.Hour)
	count, err := repo.CountRecentByUser(txCtx, owner, since)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "同一个号的两次必须各算一次；窗口外与他人的不算")

	// 已结的事件也算：熔断问的是「最近出过几次事」，不是「现在还坏着几个」。
	// 上面五条全部是已结的，count 仍然是 3 —— 这一条由上面那个断言一并钉住。

	count, err = repo.CountRecentByUser(txCtx, other, since)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
