// APEXONE-EXT: 双边市场——对账导出服务。
//
// 薄到几乎只有三件事：确定时间窗、把行推给回调、把"推了多少行 / 有没有截断"报回去。
// 没有缓存、没有聚合、没有格式化——CSV 长什么样是 handler 的事，行从哪来是仓储的事。
package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// supplyExportDefaultWindowDays 是没给时间范围时的默认回溯天数。
//
// 有默认窗口而不是"不给就导全部"，是因为流式导出撞上行数上限时截掉的是**最新的**
// 那一段（按时间正序推）。运营不填任何条件点一下导出，本意几乎总是"最近的账"，
// 而"导全部"会给他一份从开天辟地开始、在半路被截断的文件——恰好把他想要的那部分
// 截掉了。默认 90 天覆盖一个季度的对账周期；要更久就显式填 start_at。
//
// 这个默认值不是静默的：生效的窗口会同时出现在文件名和文件末尾的说明行里。
const supplyExportDefaultWindowDays = 90

// ErrSupplyExportUnavailable 导出没装配起来。
//
// 与运营视图同一个理由回 503 而不是回空文件：一份 0 行的对账文件与"这段时间
// 确实没有账"长得一模一样，而两者要做的事完全相反。
var ErrSupplyExportUnavailable = infraerrors.ServiceUnavailable(
	"SERVICE_UNAVAILABLE", "supply export service unavailable")

// SupplyExportWindow 是一次导出实际生效的时间范围。
//
// 之所以要把它从服务里交出去而不是留在内部：文件名与文件末尾的说明行都要写上它。
// 一份对账文件如果不写清自己覆盖了哪段时间，它在硬盘上放三天之后就没人敢用了。
type SupplyExportWindow struct {
	StartAt time.Time
	EndAt   time.Time
}

// ResolveSupplyExportWindow 把两个可选的时间参数补成一个确定的窗口。
//
// 刻意做成不依赖任何状态的包级函数：handler 必须在写出第一个响应头**之前**知道
// 窗口（文件名里有它），而那时还没开始查库。now 由调用方传入，测试才能是确定的。
//
// start > end 时不做交换：交换等于替用户改需求，而一个空文件加上末尾那行
// "本文件覆盖 X 至 Y" 已经把问题说清楚了。
func ResolveSupplyExportWindow(startAt, endAt *time.Time, now time.Time) SupplyExportWindow {
	window := SupplyExportWindow{EndAt: now}
	if endAt != nil {
		window.EndAt = *endAt
	}
	window.StartAt = window.EndAt.AddDate(0, 0, -supplyExportDefaultWindowDays)
	if startAt != nil {
		window.StartAt = *startAt
	}
	return window
}

// SupplyExportOutcome 是一次导出结束后要写进文件末尾的那几个数。
type SupplyExportOutcome struct {
	// Rows 实际推出去的行数。
	Rows int
	// Truncated 撞上了行数上限，后面还有行没导出来。
	//
	// 这个布尔量是整条导出链路上最要紧的一个值：响应头早就发出去了，
	// 状态码改不了，它是唯一能告诉运营"这份文件不完整"的东西。
	Truncated bool
}

// SupplierExportService 提供对账导出。
type SupplierExportService struct {
	repo SupplierExportRepository
}

// NewSupplierExportService 构造导出服务。
func NewSupplierExportService(repo SupplierExportRepository) *SupplierExportService {
	return &SupplierExportService{repo: repo}
}

func (s *SupplierExportService) ready() bool {
	return s != nil && s.repo != nil
}

// Available 让 handler 在写出第一个响应头**之前**就能拒绝。
//
// 流式响应只有这一个时机能回一个像样的错误：一旦 200 和 Content-Disposition
// 发出去了，浏览器就已经在存一个文件了，此后再发现"服务没装配起来"也只能
// 交给他一个空文件。这个方法存在的全部理由就是把那一种错误提前到还能报的时候。
func (s *SupplierExportService) Available() bool {
	return s.ready()
}

// StreamWithdrawals 按时间正序把提现单推给 fn。
//
// window 会覆盖 filter 里的时间字段——窗口只有一个来源（ResolveSupplyExportWindow），
// 否则文件名上写的范围与实际查的范围会各说各话。
func (s *SupplierExportService) StreamWithdrawals(
	ctx context.Context,
	filter SupplierWithdrawalFilter,
	window SupplyExportWindow,
	fn func(*SupplierWithdrawalExportRow) error,
) (SupplyExportOutcome, error) {
	if !s.ready() {
		return SupplyExportOutcome{}, ErrSupplyExportUnavailable
	}
	filter.StartAt = &window.StartAt
	filter.EndAt = &window.EndAt

	outcome := SupplyExportOutcome{}
	truncated, err := s.repo.StreamWithdrawals(ctx, filter, SupplyExportMaxRows,
		func(row *SupplierWithdrawalExportRow) error {
			if err := fn(row); err != nil {
				return err
			}
			outcome.Rows++
			return nil
		})
	outcome.Truncated = truncated
	return outcome, err
}

// StreamLedger 按时间正序把全站钱包流水推给 fn。
func (s *SupplierExportService) StreamLedger(
	ctx context.Context,
	filter SupplyAdminLedgerFilter,
	window SupplyExportWindow,
	fn func(*SupplyLedgerExportRow) error,
) (SupplyExportOutcome, error) {
	if !s.ready() {
		return SupplyExportOutcome{}, ErrSupplyExportUnavailable
	}
	filter.StartAt = &window.StartAt
	filter.EndAt = &window.EndAt

	outcome := SupplyExportOutcome{}
	truncated, err := s.repo.StreamLedger(ctx, filter, SupplyExportMaxRows,
		func(row *SupplyLedgerExportRow) error {
			if err := fn(row); err != nil {
				return err
			}
			outcome.Rows++
			return nil
		})
	outcome.Truncated = truncated
	return outcome, err
}
