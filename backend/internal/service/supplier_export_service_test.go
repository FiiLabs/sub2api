//go:build unit

// APEXONE-EXT: 对账导出服务的单元测试。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var exportNow = time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)

func TestResolveSupplyExportWindowDefaultsToARecentQuarter(t *testing.T) {
	window := ResolveSupplyExportWindow(nil, nil, exportNow)

	assert.Equal(t, exportNow, window.EndAt)
	assert.Equal(t, exportNow.AddDate(0, 0, -supplyExportDefaultWindowDays), window.StartAt)
}

// 只给起点时终点是"现在"，只给终点时起点从终点往回数。
//
// 后者是容易写错的一半：从**现在**往回数的话，一个"导 2026 年 3 月"的请求
// 会得到一个跨越五个月、且几乎不含三月的窗口。
func TestResolveSupplyExportWindowAnchorsOnTheGivenEnd(t *testing.T) {
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	window := ResolveSupplyExportWindow(nil, &end, exportNow)

	assert.Equal(t, end, window.EndAt)
	assert.Equal(t, end.AddDate(0, 0, -supplyExportDefaultWindowDays), window.StartAt,
		"起点是从给定的终点往回数的，不是从现在")

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	both := ResolveSupplyExportWindow(&start, &end, exportNow)
	assert.Equal(t, start, both.StartAt)
	assert.Equal(t, end, both.EndAt)
}

// 起点晚于终点时不做交换。
//
// 交换等于替运营改需求；一个空文件加上尾行里那句"本文件覆盖 X 至 Y"
// 已经把他填反了这件事说清楚了。
func TestResolveSupplyExportWindowDoesNotSwapReversedBounds(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	window := ResolveSupplyExportWindow(&start, &end, exportNow)
	assert.Equal(t, start, window.StartAt)
	assert.Equal(t, end, window.EndAt)
	assert.True(t, window.StartAt.After(window.EndAt), "窗口保持运营填的样子")
}

// ============================================================================

type stubExportRepo struct {
	withdrawals  []SupplierWithdrawalExportRow
	ledger       []SupplyLedgerExportRow
	truncated    bool
	limit        int
	filter       SupplierWithdrawalFilter
	ledgerFilter SupplyAdminLedgerFilter
	err          error
}

func (s *stubExportRepo) StreamWithdrawals(_ context.Context, filter SupplierWithdrawalFilter,
	limit int, fn func(*SupplierWithdrawalExportRow) error) (bool, error) {
	s.filter, s.limit = filter, limit
	for i := range s.withdrawals {
		if err := fn(&s.withdrawals[i]); err != nil {
			return false, err
		}
	}
	return s.truncated, s.err
}

func (s *stubExportRepo) StreamLedger(_ context.Context, filter SupplyAdminLedgerFilter,
	limit int, fn func(*SupplyLedgerExportRow) error) (bool, error) {
	s.ledgerFilter, s.limit = filter, limit
	for i := range s.ledger {
		if err := fn(&s.ledger[i]); err != nil {
			return false, err
		}
	}
	return s.truncated, s.err
}

// 窗口覆盖调用方在 filter 里填的时间字段。
//
// 窗口只能有一个来源：文件名、尾行说明和 WHERE 子句必须是同一段时间，
// 否则一份写着"七月"的文件里装的是别的月份的账。
func TestStreamWithdrawalsOverridesFilterTimesWithTheWindow(t *testing.T) {
	repo := &stubExportRepo{}
	stray := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	window := SupplyExportWindow{
		StartAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}

	_, err := NewSupplierExportService(repo).StreamWithdrawals(context.Background(),
		SupplierWithdrawalFilter{StartAt: &stray, EndAt: &stray}, window,
		func(*SupplierWithdrawalExportRow) error { return nil })
	require.NoError(t, err)

	require.NotNil(t, repo.filter.StartAt)
	assert.Equal(t, window.StartAt, *repo.filter.StartAt, "filter 里的旧时间没被窗口覆盖掉")
	assert.Equal(t, window.EndAt, *repo.filter.EndAt)
	assert.Equal(t, SupplyExportMaxRows, repo.limit, "没把行数上限传下去，导出就没有闸了")
}

func TestStreamReportsRowsAndTruncation(t *testing.T) {
	repo := &stubExportRepo{
		ledger:    []SupplyLedgerExportRow{{ID: 1}, {ID: 2}, {ID: 3}},
		truncated: true,
	}

	outcome, err := NewSupplierExportService(repo).StreamLedger(context.Background(),
		SupplyAdminLedgerFilter{}, SupplyExportWindow{},
		func(*SupplyLedgerExportRow) error { return nil })
	require.NoError(t, err)

	assert.Equal(t, 3, outcome.Rows)
	assert.True(t, outcome.Truncated)
}

// 回调报错时行数不再往上加：那一行没写成功，不该被算进"导出了几行"。
func TestStreamStopsCountingWhenTheSinkFails(t *testing.T) {
	repo := &stubExportRepo{ledger: []SupplyLedgerExportRow{{ID: 1}, {ID: 2}, {ID: 3}}}
	boom := errors.New("client hung up")

	seen := 0
	outcome, err := NewSupplierExportService(repo).StreamLedger(context.Background(),
		SupplyAdminLedgerFilter{}, SupplyExportWindow{},
		func(*SupplyLedgerExportRow) error {
			seen++
			if seen == 2 {
				return boom
			}
			return nil
		})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, outcome.Rows, "写失败的那一行被算进行数了")
}

// 没装配起来时回 503，且**在流开始之前**就能问出来。
func TestExportServiceUnavailableWithoutRepository(t *testing.T) {
	service := NewSupplierExportService(nil)
	assert.False(t, service.Available())

	_, err := service.StreamLedger(context.Background(), SupplyAdminLedgerFilter{}, SupplyExportWindow{},
		func(*SupplyLedgerExportRow) error { return nil })
	assert.ErrorIs(t, err, ErrSupplyExportUnavailable)

	var nilService *SupplierExportService
	assert.False(t, nilService.Available(), "nil 服务上问一句也不该 panic")
}
