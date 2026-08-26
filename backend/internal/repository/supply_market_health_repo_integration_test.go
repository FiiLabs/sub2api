//go:build integration

// APEXONE-EXT: 双边市场——定价健康度聚合的真库验证。
//
// 这组测试的全部价值在于**口径**。三条 SQL 里每一个 SUM 都可能被换成一个
// 长得差不多、但含义完全不同的列，而换错了不会报任何错：
//
//   - total_cost 与 actual_cost 差着一个倍率（0.18 下正好 5.6 倍）。把营收
//     算成 total_cost，面板会显示一个虚高五倍的收入
//   - 兜底靠 owner_user_id IS NULL 切分。切反了，「共享供给不足」会被读成
//     「共享供给充足」，处置完全相反
//   - 产出榜混进平台自有账号，中位数就被污染——而中位数正是用来证伪
//     产能假设（$3000/月）的那个数
//
// sqlmock 证明不了这些：它只能证明"我发了这条 SQL"。
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

// seedHealthUsage 插一条用量流水。listCost 是官方牌价，billedCost 是消费者实付。
func seedHealthUsage(
	t *testing.T, ctx context.Context, client *dbent.Client,
	userID, apiKeyID, accountID int64, listCost, billedCost float64, daysAgo int,
) {
	t.Helper()
	_, err := client.ExecContext(ctx, `
INSERT INTO usage_logs (user_id, api_key_id, account_id, model, total_cost, actual_cost, created_at)
VALUES ($1, $2, $3, 'claude-sonnet-4-5', $4, $5, NOW() - ($6 || ' days')::interval)`,
		userID, apiKeyID, accountID, listCost, billedCost, fmt.Sprintf("%d", daysAgo))
	require.NoError(t, err)
}

// seedHealthKey 建一把消费者密钥。usage_logs.api_key_id 上有外键，绕不过去。
func seedHealthKey(t *testing.T, client *dbent.Client, userID int64) int64 {
	t.Helper()
	return mustCreateApiKey(t, client, &service.APIKey{UserID: userID}).ID
}

// seedHealthAccount 建一个账号。owner = 0 表示平台自有（兜底池）。
func seedHealthAccount(t *testing.T, ctx context.Context, client *dbent.Client, name string, owner int64) int64 {
	t.Helper()
	var id int64
	query := `
INSERT INTO accounts (name, platform, type, status, credentials, created_at, updated_at, owner_user_id)
VALUES ($1, 'anthropic', 'apikey', 'active', '{}', NOW(), NOW(), $2)
RETURNING id`
	var ownerArg any
	if owner > 0 {
		ownerArg = owner
	}
	rows, err := client.QueryContext(ctx, query, name, ownerArg)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&id))
	return id
}

// 牌价与营收各归各位，兜底那一支按归属人切分。
func TestSupplyHealth_SeparatesListValueRevenueAndOverflow(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplier := mustCreateSupplier(t, client, "health-owner")
	consumer := mustCreateSupplier(t, client, "health-consumer")
	shared := seedHealthAccount(t, txCtx, client, "shared-"+time.Now().Format("150405.000"), supplier)
	firstParty := seedHealthAccount(t, txCtx, client, "first-party-"+time.Now().Format("150405.000"), 0)
	key := seedHealthKey(t, client, consumer)

	// 共享账号服务两笔，兜底账号服务一笔。倍率 0.18。
	seedHealthUsage(t, txCtx, client, consumer, key, shared, 100, 18, 1)
	seedHealthUsage(t, txCtx, client, consumer, key, shared, 200, 36, 2)
	seedHealthUsage(t, txCtx, client, consumer, key, firstParty, 50, 9, 3)

	health, err := NewSupplyMarketHealthRepository(client).Aggregate(txCtx, 30)
	require.NoError(t, err)

	assert.InDelta(t, 350.0, health.ListValue, 1e-6, "牌价等值要用 total_cost")
	assert.InDelta(t, 63.0, health.Revenue, 1e-6, "营收要用 actual_cost；用 total_cost 会把收入虚报 5.6 倍")
	assert.InDelta(t, 50.0, health.OverflowListValue, 1e-6, "兜底承接只算 owner_user_id IS NULL 的账号")
}

// 窗口外的流水一分都不能进来。
func TestSupplyHealth_ExcludesUsageOutsideTheWindow(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplier := mustCreateSupplier(t, client, "health-window")
	account := seedHealthAccount(t, txCtx, client, "win-"+time.Now().Format("150405.000"), supplier)
	key := seedHealthKey(t, client, supplier)

	seedHealthUsage(t, txCtx, client, supplier, key, account, 100, 18, 3)   // 窗口内
	seedHealthUsage(t, txCtx, client, supplier, key, account, 900, 162, 40) // 窗口外

	repo := NewSupplyMarketHealthRepository(client)

	health, err := repo.Aggregate(txCtx, 7)
	require.NoError(t, err)
	assert.InDelta(t, 100.0, health.ListValue, 1e-6, "7 天窗口捞到了 40 天前的流水")

	// 放宽到 90 天，两笔都该在。
	health, err = repo.Aggregate(txCtx, 90)
	require.NoError(t, err)
	assert.InDelta(t, 1000.0, health.ListValue, 1e-6)
}

// 产出榜只列他人挂的号，且折月换算随窗口变化。
func TestSupplyHealth_LeaderboardExcludesFirstPartyAndScalesToMonth(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	supplier := mustCreateSupplier(t, client, "health-board")
	consumer := mustCreateSupplier(t, client, "health-board-c")
	stamp := time.Now().Format("150405.000")
	shared := seedHealthAccount(t, txCtx, client, "board-shared-"+stamp, supplier)
	firstParty := seedHealthAccount(t, txCtx, client, "board-first-"+stamp, 0)
	key := seedHealthKey(t, client, consumer)

	seedHealthUsage(t, txCtx, client, consumer, key, shared, 70, 12.6, 1)
	seedHealthUsage(t, txCtx, client, consumer, key, firstParty, 500, 90, 1)

	health, err := NewSupplyMarketHealthRepository(client).Aggregate(txCtx, 7)
	require.NoError(t, err)

	require.Len(t, health.SupplyAccounts, 1, "平台自有账号混进了供给者产出榜——中位数会被污染")
	row := health.SupplyAccounts[0]
	assert.Equal(t, shared, row.AccountID)
	assert.Equal(t, supplier, row.OwnerUserID)
	assert.InDelta(t, 70.0, row.ListValue, 1e-6)
	// 7 天产出 70 → 折月 300。让不同窗口下的读数能和同一个产能估算比。
	assert.InDelta(t, 300.0, row.MonthlyOutput, 1e-6)
	assert.Equal(t, int64(1), row.Requests)
	assert.Equal(t, 1, health.SupplierCount)
}

// 空窗口返回零值而不是报错，且不会因为除零炸掉。
func TestSupplyHealth_EmptyWindowIsAllZeroes(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)

	health, err := NewSupplyMarketHealthRepository(tx.Client()).Aggregate(txCtx, 1)
	require.NoError(t, err)
	require.NotNil(t, health)
	assert.Zero(t, health.ListValue)
	assert.Zero(t, health.EffectiveShare)
	assert.NotNil(t, health.SupplyAccounts, "空榜要发 []，不是 null——前端的 v-for 迟早会在 null 上炸")
	assert.Empty(t, health.SupplyAccounts)
}
