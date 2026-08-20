// APEXONE-EXT: 双边市场——对账导出的 HTTP 层。
//
// 两条 GET，都是流式 CSV 下载。方法挂在既有的 *SupplierAdminHandler 上，
// 理由与那个文件顶部写的一样：AdminHandlers / ProvideAdminHandlers / handler wire.go
// 是三处上游合并热区，这一整块运营视图在核心文件里的侵入是零行。
//
// # 这个文件与其它 handler 最大的不同：错误无处可报
//
// 常规 handler 出错时 `response.ErrorFrom(c, err)` 回一个 JSON。流式下载没有这个
// 机会——响应头（200 + Content-Disposition）在第一行数据之前就发出去了，浏览器
// 那时已经在往磁盘上存一个文件。此后无论发生什么，运营看到的都是"下载完成"。
//
// 因此这里的错误处理分成两段，且只有这两段：
//
//   - **写头之前**：能报就报（服务没装配、参数不合法），走正常的 JSON 错误。
//   - **写头之后**：报不了。唯一能做的是**不写那一行尾行**，让文件在结构上
//     显得没写完，同时把错误记进 slog 供事后追。尾行的设计见 supplier_export_csv.go。
//
// 这不是一个优雅的方案，是流式下载唯一诚实的方案。替代方案（先把整份文件攒进
// 内存，成功了再一次性写出）能给出正确的状态码，代价是运营点一次导出就有机会
// 把网关 OOM 掉——那会连带停掉所有消费者的请求。
package handler

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// supplyExportFlushEvery 是往网络刷一次的行间隔。
//
// 不刷的话 csv.Writer 会一直攒在 bufio 里，一份 20 万行的导出在客户端看来是
// "十几秒毫无动静，然后突然完成"——中间任何一个代理都可能先一步判定它超时。
// 每千行刷一次，缓冲区始终是几十 KB 量级，进度条也一直在动。
const supplyExportFlushEvery = 1000

// ExportWithdrawals 把提现单导成 CSV。
// GET /api/v1/admin/supply/export/withdrawals?status=&user_id=&start_at=&end_at=
func (h *SupplierAdminHandler) ExportWithdrawals(c *gin.Context) {
	window, ok := h.beginSupplyExport(c, "withdrawals")
	if !ok {
		return
	}

	filter := service.SupplierWithdrawalFilter{
		UserID: parseSupplyExportInt64(c.Query("user_id")),
		Status: strings.TrimSpace(c.Query("status")),
	}

	writer := csv.NewWriter(c.Writer)
	err := writeSupplyExportCSV(writer, supplierWithdrawalCSVHeader, window,
		func(write func([]string) error) (service.SupplyExportOutcome, error) {
			written := 0
			return h.exportService.StreamWithdrawals(c.Request.Context(), filter, window,
				func(row *service.SupplierWithdrawalExportRow) error {
					if err := write(supplierWithdrawalCSVRow(row)); err != nil {
						return err
					}
					written++
					flushSupplyExport(writer, c, written)
					return nil
				})
		})
	logSupplyExportFailure(c, "withdrawals", err)
}

// ExportLedger 把全站钱包流水导成 CSV。
// GET /api/v1/admin/supply/export/ledger?user_id=&action=&account_id=&request_id=&start_at=&end_at=
func (h *SupplierAdminHandler) ExportLedger(c *gin.Context) {
	window, ok := h.beginSupplyExport(c, "ledger")
	if !ok {
		return
	}

	filter := service.SupplyAdminLedgerFilter{
		UserID:    parseSupplyExportInt64(c.Query("user_id")),
		Action:    strings.TrimSpace(c.Query("action")),
		AccountID: parseSupplyExportInt64(c.Query("account_id")),
		RequestID: strings.TrimSpace(c.Query("request_id")),
	}

	writer := csv.NewWriter(c.Writer)
	err := writeSupplyExportCSV(writer, supplyLedgerCSVHeader, window,
		func(write func([]string) error) (service.SupplyExportOutcome, error) {
			written := 0
			return h.exportService.StreamLedger(c.Request.Context(), filter, window,
				func(row *service.SupplyLedgerExportRow) error {
					if err := write(supplyLedgerCSVRow(row)); err != nil {
						return err
					}
					written++
					flushSupplyExport(writer, c, written)
					return nil
				})
		})
	logSupplyExportFailure(c, "ledger", err)
}

// beginSupplyExport 做一切"还能好好报错"的事，然后把响应头写出去。
//
// 顺序是有讲究的：先判服务可用、再定窗口、最后才写头。写头是一道单行门，
// 过了它这个请求就只能是一个 200 的文件下载了。
func (h *SupplierAdminHandler) beginSupplyExport(c *gin.Context, kind string) (service.SupplyExportWindow, bool) {
	if h == nil || h.exportService == nil || !h.exportService.Available() {
		response.ErrorFrom(c, service.ErrSupplyExportUnavailable)
		return service.SupplyExportWindow{}, false
	}

	window := service.ResolveSupplyExportWindow(
		parseSupplyAdminTimeQuery(c.Query("start_at")),
		parseSupplyAdminTimeQuery(c.Query("end_at")),
		time.Now(),
	)

	filename := fmt.Sprintf("supply-%s-%s-%s.csv", kind,
		window.StartAt.UTC().Format("20060102"), window.EndAt.UTC().Format("20060102"))

	header := c.Writer.Header()
	header.Set("Content-Type", "text/csv; charset=utf-8")
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// 对账文件不该被任何一层缓存留下来：它含收款账号，而且下一次导出的内容
	// 与这一次几乎必然不同——一个"看起来成功了但其实是上周那份"的下载
	// 是这里最难被发现的错误。
	header.Set("Cache-Control", "no-store")
	// 让浏览器/代理不去猜类型。CSV 被嗅成 HTML 的话，里面的内容会被当标记解析。
	header.Set("X-Content-Type-Options", "nosniff")
	c.Writer.WriteHeader(http.StatusOK)

	// BOM 必须是文件的头三个字节，先于表头。理由见 supplier_export_csv.go 第 1 条。
	if _, err := c.Writer.WriteString(utf8BOM); err != nil {
		logSupplyExportFailure(c, kind, err)
		return service.SupplyExportWindow{}, false
	}
	return window, true
}

// flushSupplyExport 按固定行距把缓冲推到网络上。
func flushSupplyExport(writer *csv.Writer, c *gin.Context, written int) {
	if written%supplyExportFlushEvery != 0 {
		return
	}
	writer.Flush()
	c.Writer.Flush()
}

// logSupplyExportFailure 是流开始之后唯一的错误出口。
//
// 只记日志，不动响应：那时状态码已经是 200 了，改不了。客户端侧的表现是
// 文件末尾少了那一行 `#` 尾行。
func logSupplyExportFailure(c *gin.Context, kind string, err error) {
	if err == nil {
		return
	}
	slog.Error("supply export failed mid-stream",
		"kind", kind,
		"error", err,
		"path", c.Request.URL.Path,
	)
}

// parseSupplyExportInt64 解析一个可选的数字筛子。解析不了当没传——
// 与 parseSupplyAdminTimeQuery 同一个理由：一个畸形参数把整份导出拒掉，
// 运营会以为这段时间没有账。
func parseSupplyExportInt64(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
