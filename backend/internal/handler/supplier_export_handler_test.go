//go:build unit

// APEXONE-EXT: 对账导出 HTTP 层的单元测试。
//
// 这里测的是"运营点了那个按钮之后拿到的到底是什么"：响应头、字节序、
// 以及最要紧的——**出错时那个文件长什么样**。
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExportRepo 按脚本推行，可以在第 n 行上炸。
type fakeExportRepo struct {
	withdrawals []service.SupplierWithdrawalExportRow
	ledger      []service.SupplyLedgerExportRow
	truncated   bool
	failAfter   int // > 0 时推完这么多行就报错
	gotFilter   service.SupplierWithdrawalFilter
	gotLedger   service.SupplyAdminLedgerFilter
}

func (f *fakeExportRepo) StreamWithdrawals(_ context.Context, filter service.SupplierWithdrawalFilter,
	_ int, fn func(*service.SupplierWithdrawalExportRow) error) (bool, error) {
	f.gotFilter = filter
	for i := range f.withdrawals {
		if f.failAfter > 0 && i == f.failAfter {
			return false, errors.New("database went away")
		}
		if err := fn(&f.withdrawals[i]); err != nil {
			return false, err
		}
	}
	return f.truncated, nil
}

func (f *fakeExportRepo) StreamLedger(_ context.Context, filter service.SupplyAdminLedgerFilter,
	_ int, fn func(*service.SupplyLedgerExportRow) error) (bool, error) {
	f.gotLedger = filter
	for i := range f.ledger {
		if f.failAfter > 0 && i == f.failAfter {
			return false, errors.New("database went away")
		}
		if err := fn(&f.ledger[i]); err != nil {
			return false, err
		}
	}
	return f.truncated, nil
}

// exportRouterOn 装一个只挂着两条导出路由的路由器（不带任何鉴权中间件：
// 鉴权是路由组的事，已由 routes/supplier_test.go 钉住）。
func exportRouterOn(repo service.SupplierExportRepository) (*gin.Engine, *SupplierAdminHandler) {
	gin.SetMode(gin.TestMode)
	h := NewSupplierAdminHandler(nil, nil, service.NewSupplierExportService(repo), nil, nil)
	router := gin.New()
	router.GET("/export/withdrawals", h.ExportWithdrawals)
	router.GET("/export/ledger", h.ExportLedger)
	return router, h
}

func getExport(t *testing.T, router *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestExportWithdrawalsWritesADownloadableCSV(t *testing.T) {
	repo := &fakeExportRepo{withdrawals: []service.SupplierWithdrawalExportRow{{
		ID: 7, UserID: 3, UserEmail: "supplier@example.com",
		Amount: "30.00000000", Status: "paid",
		PayoutChannel: "bank", PayoutAccount: "6222 0202 / 张三 / 招商银行",
		ExternalRef: "TX-9911",
		CreatedAt:   time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
	}}}
	router, _ := exportRouterOn(repo)

	recorder := getExport(t, router, "/export/withdrawals")
	require.Equal(t, http.StatusOK, recorder.Code)

	assert.Equal(t, "text/csv; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), "attachment;")
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), "supply-withdrawals-")
	// 一份含收款账号的文件不该被任何一层留下来。
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))

	body := recorder.Body.String()
	assert.True(t, strings.HasPrefix(body, utf8BOM),
		"BOM 不在头三个字节上，Windows 版 Excel 会把中文显示成乱码")

	lines := strings.Split(strings.TrimRight(strings.TrimPrefix(body, utf8BOM), "\n"), "\n")
	require.Len(t, lines, 3, "表头 + 一行数据 + 尾行")
	assert.Equal(t, strings.Join(supplierWithdrawalCSVHeader, ","), lines[0])
	assert.Contains(t, lines[1], "supplier@example.com")
	assert.Contains(t, lines[1], "TX-9911")
	// 收款账号在文件里是**明文**：这份文件就是打款工作单（§3.7 末段）。
	assert.Contains(t, lines[1], "张三")
	assert.True(t, strings.HasPrefix(lines[2], "#"))
	assert.Contains(t, lines[2], "1 rows")
}

// 撞上限时文件里必须有那句话。
func TestExportLedgerAnnouncesTruncation(t *testing.T) {
	repo := &fakeExportRepo{
		ledger:    []service.SupplyLedgerExportRow{{ID: 1, Action: "accrue", Amount: "0.5"}},
		truncated: true,
	}
	router, _ := exportRouterOn(repo)

	body := getExport(t, router, "/export/ledger").Body.String()
	assert.Contains(t, body, "TRUNCATED",
		"截断了却给了运营一份看起来完整的文件")
}

// 中途出错：状态码仍是 200（改不了），但文件里**没有**尾行。
//
// 这条钉的是这个功能最危险的失败模式。断言 200 不是在认可它，
// 是在记录"这就是流式下载的现实"——正因为如此，尾行才是必需的。
func TestExportKeepsTrailerOffWhenTheStreamDies(t *testing.T) {
	repo := &fakeExportRepo{
		withdrawals: []service.SupplierWithdrawalExportRow{{ID: 1}, {ID: 2}, {ID: 3}},
		failAfter:   2,
	}
	router, _ := exportRouterOn(repo)

	recorder := getExport(t, router, "/export/withdrawals")
	assert.Equal(t, http.StatusOK, recorder.Code, "流已经开始，状态码改不了——这正是尾行存在的理由")

	body := strings.TrimPrefix(recorder.Body.String(), utf8BOM)
	assert.NotContains(t, body, "#", "半截文件被盖上了完整的章")
	assert.Contains(t, body, "\n1,", "已经写出去的行还在——它们本来就已经发到网络上了")
}

// 服务没装配起来时要在写响应头**之前**拒绝。
//
// 过了那道门就只能给他一个空文件了，而一个 0 行的对账文件与
// "这段时间确实没有账"长得一模一样。
func TestExportRefusesBeforeWritingHeadersWhenUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSupplierAdminHandler(nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/export/ledger", h.ExportLedger)

	recorder := getExport(t, router, "/export/ledger")
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.NotContains(t, recorder.Header().Get("Content-Type"), "text/csv")
	assert.NotContains(t, recorder.Body.String(), utf8BOM)
}

// 时间窗必须真的传到仓储上，而不只是印在文件名里。
//
// 两者不一致的表现是：文件名写着七月，内容是最近九十天——而运营是照着文件名
// 把它归档进"七月对账"的。
func TestExportPassesTheResolvedWindowDownToTheQuery(t *testing.T) {
	repo := &fakeExportRepo{}
	router, _ := exportRouterOn(repo)

	recorder := getExport(t, router,
		"/export/withdrawals?start_at=2026-07-01T00:00:00Z&end_at=2026-08-01T00:00:00Z&status=paid&user_id=42")
	require.Equal(t, http.StatusOK, recorder.Code)

	require.NotNil(t, repo.gotFilter.StartAt)
	require.NotNil(t, repo.gotFilter.EndAt)
	assert.Equal(t, "2026-07-01T00:00:00Z", repo.gotFilter.StartAt.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-08-01T00:00:00Z", repo.gotFilter.EndAt.UTC().Format(time.RFC3339))
	assert.Equal(t, "paid", repo.gotFilter.Status)
	assert.Equal(t, int64(42), repo.gotFilter.UserID)

	assert.Contains(t, recorder.Header().Get("Content-Disposition"), "supply-withdrawals-20260701-20260801.csv",
		"文件名与实际查询的窗口必须是同一个窗口")
}

// 畸形的筛选参数当没传，不把整份导出拒掉。
//
// 与 parseSupplyAdminTimeQuery 同一个理由：拒掉的表现是运营以为这段时间没有账。
func TestExportIgnoresMalformedFilters(t *testing.T) {
	repo := &fakeExportRepo{}
	router, _ := exportRouterOn(repo)

	recorder := getExport(t, router, "/export/ledger?user_id=abc&account_id=-5&start_at=yesterday")
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Zero(t, repo.gotLedger.UserID)
	assert.Zero(t, repo.gotLedger.AccountID)
	require.NotNil(t, repo.gotLedger.StartAt, "时间没给就该落到默认窗口上，而不是不筛")
}
