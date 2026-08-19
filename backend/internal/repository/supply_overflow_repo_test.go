package repository

import (
	"context"
	"errors"
	"math"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func overflowDay(t *testing.T) time.Time {
	t.Helper()
	day, err := time.Parse("2006-01-02", "2026-08-18")
	require.NoError(t, err)
	return day
}

// 配额判定与计数必须在同一条语句里。这条测试钉住的是语句形状本身——
// 一旦有人把它拆成「先 SELECT 再 UPDATE」，并发下配额就会超发，而那种超发在
// 单机测试里几乎不可能复现出来，只能靠这里挡。
func TestSupplyOverflowConsumeSQLDecidesAndCountsInOneStatement(t *testing.T) {
	normalized := normalizeSQL(supplyOverflowConsumeSQL)

	require.Contains(t, normalized, "ON CONFLICT (day) DO UPDATE")
	require.Contains(t, normalized, "SET overflow_count = supply_overflow_daily.overflow_count + 1")
	// WHERE 挂在 DO UPDATE 上：判定和加一发生在同一个行锁里。
	require.Contains(t, normalized, "WHERE supply_overflow_daily.overflow_count < $2")
	// 有没有返回行就是判定结果，所以 RETURNING 不能被删掉。
	require.Contains(t, normalized, "RETURNING overflow_count")
}

// denied 单独计数：混进 overflow_count 的话，同一个数字既可能表示花了钱也可能表示省了钱。
func TestSupplyOverflowDenySQLTouchesOnlyDeniedCount(t *testing.T) {
	normalized := normalizeSQL(supplyOverflowDenySQL)
	require.Contains(t, normalized, "SET denied_count = supply_overflow_daily.denied_count + 1")
	require.NotContains(t, normalized, "SET overflow_count")
}

// 语句里的列必须在迁移里真的存在，否则第一次溢出才会发现表不对。
func TestSupplyOverflowSQLMatchesMigration(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/227_supply_overflow_daily.sql")
	require.NoError(t, err)
	normalized := normalizeSQL(string(migration))

	require.Contains(t, normalized, "CREATE TABLE IF NOT EXISTS supply_overflow_daily")
	// day 是主键，ON CONFLICT (day) 才有可推断的唯一约束——写成普通列会直接报错。
	require.Contains(t, normalized, "day DATE PRIMARY KEY")
	require.Contains(t, normalized, "overflow_count BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, normalized, "denied_count BIGINT NOT NULL DEFAULT 0")
}

func TestTryConsumeDailyOverflowAllowsWhenRowReturned(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supply_overflow_daily")).
		WithArgs("2026-08-18", int64(500)).
		WillReturnRows(sqlmock.NewRows([]string{"overflow_count"}).AddRow(int64(12)))

	allowed, err := NewSupplyOverflowCounter(client).
		TryConsumeDailyOverflow(context.Background(), overflowDay(t), 500)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 无返回行 = 配额已满。此时必须补一次 denied 计数，否则运营看不出「今天省了多少次」。
func TestTryConsumeDailyOverflowDeniesAndRecordsWhenExhausted(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supply_overflow_daily")).
		WithArgs("2026-08-18", int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"overflow_count"}))
	mock.ExpectExec(regexp.QuoteMeta("SET denied_count")).
		WithArgs("2026-08-18").
		WillReturnResult(sqlmock.NewResult(0, 1))

	allowed, err := NewSupplyOverflowCounter(client).
		TryConsumeDailyOverflow(context.Background(), overflowDay(t), 5)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// denied 那一笔写不进去不改变判定：判定已经做完了，因为一次统计写失败而放行才是错的那边。
func TestTryConsumeDailyOverflowStaysDeniedWhenDenyWriteFails(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supply_overflow_daily")).
		WillReturnRows(sqlmock.NewRows([]string{"overflow_count"}))
	mock.ExpectExec(regexp.QuoteMeta("SET denied_count")).
		WillReturnError(errors.New("write failed"))

	allowed, err := NewSupplyOverflowCounter(client).
		TryConsumeDailyOverflow(context.Background(), overflowDay(t), 5)
	require.NoError(t, err)
	require.False(t, allowed)
}

// 查询失败必须**报错**而不是返回 false：调用方对这两者的处理相同（都不溢出），
// 但只有报错会在日志里说清「闸门坏了」，而不是让人以为配额刚好用完。
func TestTryConsumeDailyOverflowReturnsErrorOnQueryFailure(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO supply_overflow_daily")).
		WillReturnError(errors.New("db down"))

	allowed, err := NewSupplyOverflowCounter(client).
		TryConsumeDailyOverflow(context.Background(), overflowDay(t), 5)
	require.Error(t, err)
	require.False(t, allowed)
}

// limit <= 0 = 不限量。翻译成上界而不是跳过判定，是为了让「不限量」也照样计数——
// 溢出率是要看的，不能因为没设上限就不记。
func TestSupplyOverflowLimitBoundTranslatesUnlimited(t *testing.T) {
	require.Equal(t, int64(math.MaxInt64), supplyOverflowLimitBound(0))
	require.Equal(t, int64(math.MaxInt64), supplyOverflowLimitBound(-1))
	require.Equal(t, int64(7), supplyOverflowLimitBound(7))
}

func TestGetDailyOverflowUsageReturnsZeroForDayWithoutRow(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT overflow_count, denied_count")).
		WithArgs("2026-08-18").
		WillReturnRows(sqlmock.NewRows([]string{"overflow_count", "denied_count"}))

	usage, err := NewSupplyOverflowCounter(client).
		GetDailyOverflowUsage(context.Background(), overflowDay(t))
	require.NoError(t, err)
	require.NotNil(t, usage)
	require.Equal(t, "2026-08-18", usage.Day)
	require.Zero(t, usage.OverflowCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetDailyOverflowUsageReadsBothCounters(t *testing.T) {
	client, mock := newSupplierCreditMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT overflow_count, denied_count")).
		WithArgs("2026-08-18").
		WillReturnRows(sqlmock.NewRows([]string{"overflow_count", "denied_count"}).AddRow(int64(31), int64(4)))

	usage, err := NewSupplyOverflowCounter(client).
		GetDailyOverflowUsage(context.Background(), overflowDay(t))
	require.NoError(t, err)
	require.Equal(t, int64(31), usage.OverflowCount)
	require.Equal(t, int64(4), usage.DeniedCount)
	require.NoError(t, mock.ExpectationsWereMet())
}
