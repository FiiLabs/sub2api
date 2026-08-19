//go:build unit

// APEXONE-EXT: 双边市场——管理端运营视图服务的单元测试。
//
// 聚合正确性由真库测试负责（supplier_admin_repo_integration_test.go）。这里只测
// 一件事：**到底有什么被交给了仓储**。这一层唯一的职责就是夹取与白名单，而它
// 犯的错全是静默的——分页夹错了只是多查一点，排序键悄悄回落只是排序按钮没反应，
// 依赖没装配起来时返回空结果，界面上和「这个站没有供给者」长得一模一样。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierAdminRepoStub 把收到的过滤条件原样记下来。
type supplierAdminRepoStub struct {
	windowDays    int
	rosterFilter  SupplierRosterFilter
	accountFilter SupplyAccountAdminFilter
	ledgerFilter  SupplyAdminLedgerFilter
	calls         int
}

func (s *supplierAdminRepoStub) Overview(_ context.Context, windowDays int) (*SupplyMarketOverview, error) {
	s.calls++
	s.windowDays = windowDays
	return &SupplyMarketOverview{}, nil
}

func (s *supplierAdminRepoStub) ListSuppliers(_ context.Context, filter SupplierRosterFilter) ([]SupplierRosterEntry, int64, error) {
	s.calls++
	s.rosterFilter = filter
	return nil, 0, nil
}

func (s *supplierAdminRepoStub) ListAccounts(_ context.Context, filter SupplyAccountAdminFilter) ([]SupplyAccountAdminView, int64, error) {
	s.calls++
	s.accountFilter = filter
	return nil, 0, nil
}

func (s *supplierAdminRepoStub) ListLedger(_ context.Context, filter SupplyAdminLedgerFilter) ([]SupplyAdminLedgerEntry, int64, error) {
	s.calls++
	s.ledgerFilter = filter
	return nil, 0, nil
}

// 排序键：空串取默认，认识的放行，不认识的报错且**一次仓储都不查**。
//
// 最后半句是重点：静默回落到默认排序会让「前端改了键名」这种漂移永远没人发现。
func TestSupplierAdminService_ListSuppliersSortWhitelist(t *testing.T) {
	t.Run("空串回落到待付排序", func(t *testing.T) {
		repo := &supplierAdminRepoStub{}
		_, _, err := NewSupplierAdminService(repo).ListSuppliers(context.Background(), SupplierRosterFilter{})
		require.NoError(t, err)
		assert.Equal(t, SupplierRosterSortOwed, repo.rosterFilter.Sort)
	})

	t.Run("白名单里的键原样透传", func(t *testing.T) {
		for _, sort := range SupplierRosterSorts {
			repo := &supplierAdminRepoStub{}
			_, _, err := NewSupplierAdminService(repo).ListSuppliers(
				context.Background(), SupplierRosterFilter{Sort: sort})
			require.NoError(t, err, "白名单里的 %s 被拒了", sort)
			assert.Equal(t, sort, repo.rosterFilter.Sort)
		}
	})

	t.Run("未知键报错且不查库", func(t *testing.T) {
		repo := &supplierAdminRepoStub{}
		_, _, err := NewSupplierAdminService(repo).ListSuppliers(
			context.Background(), SupplierRosterFilter{Sort: SupplierRosterSort("owed; DROP TABLE users")})
		require.ErrorIs(t, err, ErrSupplyAdminInvalidSort)
		assert.Zero(t, repo.calls, "未知排序键仍然把请求送到了仓储")
	})
}

// 分页：非法值夹回合法区间，且 page_size 有上限。
//
// 上限是这一层唯一挡住「page_size=100000 把整张流水表拉出来」的地方。
func TestSupplierAdminService_ClampsPaging(t *testing.T) {
	cases := []struct {
		name                   string
		page, pageSize         int
		wantPage, wantPageSize int
	}{
		{"零值取默认", 0, 0, 1, supplyAdminDefaultPageSize},
		{"负页码归一", -3, 50, 1, 50},
		{"超上限压回上限", 2, 100000, 2, supplyAdminMaxPageSize},
		{"合法值不动", 7, 30, 7, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierAdminRepoStub{}
			svc := NewSupplierAdminService(repo)

			_, _, err := svc.ListSuppliers(context.Background(),
				SupplierRosterFilter{Page: tc.page, PageSize: tc.pageSize})
			require.NoError(t, err)
			assert.Equal(t, tc.wantPage, repo.rosterFilter.Page)
			assert.Equal(t, tc.wantPageSize, repo.rosterFilter.PageSize)

			_, _, err = svc.ListAccounts(context.Background(),
				SupplyAccountAdminFilter{Page: tc.page, PageSize: tc.pageSize})
			require.NoError(t, err)
			assert.Equal(t, tc.wantPage, repo.accountFilter.Page)
			assert.Equal(t, tc.wantPageSize, repo.accountFilter.PageSize)

			_, _, err = svc.ListLedger(context.Background(),
				SupplyAdminLedgerFilter{Page: tc.page, PageSize: tc.pageSize})
			require.NoError(t, err)
			assert.Equal(t, tc.wantPage, repo.ledgerFilter.Page)
			assert.Equal(t, tc.wantPageSize, repo.ledgerFilter.PageSize)
		})
	}
}

// 流水窗口：<=0 取默认，超过一年压回一年。
//
// 上限不是性能洁癖——这是个同步接口，一个 `window_days=100000` 会让它去扫全表。
func TestSupplierAdminService_ClampsOverviewWindow(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, supplyAdminDefaultWindowDays},
		{-1, supplyAdminDefaultWindowDays},
		{7, 7},
		{supplyAdminMaxWindowDays, supplyAdminMaxWindowDays},
		{100000, supplyAdminMaxWindowDays},
	}
	for _, tc := range cases {
		repo := &supplierAdminRepoStub{}
		_, err := NewSupplierAdminService(repo).Overview(context.Background(), tc.in)
		require.NoError(t, err)
		assert.Equal(t, tc.want, repo.windowDays, "window_days=%d", tc.in)
	}
}

// Health 是封闭集合：不认识的值归一成「不筛」，而不是带着一个仓储也不认识的
// 字符串往下传，靠 SQL 的默认分支去兜。
func TestSupplierAdminService_NormalizesAccountHealth(t *testing.T) {
	cases := []struct{ in, want SupplyAccountHealth }{
		{SupplyAccountHealthHealthy, SupplyAccountHealthHealthy},
		{SupplyAccountHealthUnhealthy, SupplyAccountHealthUnhealthy},
		{SupplyAccountHealthAny, SupplyAccountHealthAny},
		{SupplyAccountHealth("banned"), SupplyAccountHealthAny},
	}
	for _, tc := range cases {
		repo := &supplierAdminRepoStub{}
		_, _, err := NewSupplierAdminService(repo).ListAccounts(
			context.Background(), SupplyAccountAdminFilter{Health: tc.in})
		require.NoError(t, err)
		assert.Equal(t, tc.want, repo.accountFilter.Health, "health=%q", tc.in)
	}
}

// 管理端流水的 user_id=0 是「看全站」，不是「没传所以查不到」。
//
// 这是运营视图与供给者自助视图唯一的语义差别，值得钉一下：供给者侧同名方法在
// user_id<=0 时必须拒绝，两者不能共用同一份过滤逻辑。
func TestSupplierAdminService_LedgerZeroUserMeansWholeSite(t *testing.T) {
	repo := &supplierAdminRepoStub{}
	svc := NewSupplierAdminService(repo)

	_, _, err := svc.ListLedger(context.Background(), SupplyAdminLedgerFilter{UserID: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(0), repo.ledgerFilter.UserID)
	assert.Equal(t, 1, repo.calls, "user_id=0 没有被送到仓储")

	// 负数是脏输入，归一成「全站」而不是拼进 SQL。
	_, _, err = svc.ListLedger(context.Background(), SupplyAdminLedgerFilter{UserID: -1, AccountID: -1})
	require.NoError(t, err)
	assert.Equal(t, int64(0), repo.ledgerFilter.UserID)
	assert.Equal(t, int64(0), repo.ledgerFilter.AccountID)
}

// 没装配起来时回 503，不回空看板。
//
// nil service 与 nil repo 都要覆盖：前者是路由挂了但依赖没注入，后者是 Wire
// 把 provider 剪掉了。两种情况下返回一个空 overview 都会被读成「站上没有供给者」。
func TestSupplierAdminService_UnavailableWhenNotWired(t *testing.T) {
	for name, svc := range map[string]*SupplierAdminService{
		"nil service": nil,
		"nil repo":    NewSupplierAdminService(nil),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			overview, err := svc.Overview(ctx, 30)
			require.Error(t, err)
			assert.Nil(t, overview)

			_, _, err = svc.ListSuppliers(ctx, SupplierRosterFilter{})
			require.Error(t, err)
			_, _, err = svc.ListAccounts(ctx, SupplyAccountAdminFilter{})
			require.Error(t, err)
			_, _, err = svc.ListLedger(ctx, SupplyAdminLedgerFilter{})
			require.Error(t, err)
		})
	}
}
