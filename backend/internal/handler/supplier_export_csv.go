// APEXONE-EXT: 双边市场——对账导出的 CSV 编码。
//
// 单起一个文件、写成不依赖 gin 的纯函数：列清单和转义规则是这份文件里最容易
// 出错也最需要被测到的部分，而它们与 HTTP 没有任何关系。
//
// # 三条与"这份文件会被 Excel 打开"直接相关的决定
//
//  1. **写 UTF-8 BOM**。Windows 版 Excel 双击打开无 BOM 的 UTF-8 CSV 时按本地
//     代码页解码，中文全成乱码。这份文件里的中文不是装饰：收款账号里有开户人
//     姓名和银行名、备注是供给者自己写的。少三个字节，运营拿到的是一份没法用的
//     文件，而他多半会以为是自己电脑的问题。
//
//  2. **公式注入要挡**。`user_note`、`payout_account`、`review_note` 是自由文本，
//     其中前两个由**供给者**填写。一个以 = + - @ 开头的单元格会被 Excel 当公式执行，
//     `=HYPERLINK(...)`、DDE 那一类的老把戏至今仍然有效。挡法是前置一个单引号。
//     只对文本列做，不对数字列做——对金额列做会把负数变成文本，那是把一个安全
//     问题换成一个对账问题。
//
//  3. **时间一律 UTC 的 RFC3339**。写本地时间会让一份跨时区流转的对账文件
//     没法说清 "2026-08-01 00:00" 是谁的午夜；写 Excel 认得的 "2026/8/1" 格式则
//     会被它自作主张地重新解析成日期序列号，再按打开者的区域设置显示回来。
//     RFC3339 在 Excel 里就是一串文本，这正是我们要的：它不会被改。
package handler

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// utf8BOM 是给 Excel 的三个字节，理由见文件顶部第 1 条。
const utf8BOM = "\xef\xbb\xbf"

// csvFormulaTriggers 是会被表格软件当成公式起手的字符。
//
// 制表符与回车也在内：它们能把一个单元格拆成多个，在某些版本里足以把后面的
// 内容顶到一个新的行/列上，于是"这一行是哪个供给者的"就错位了。
const csvFormulaTriggers = "=+-@\t\r"

// csvText 是文本单元格的唯一出口。
//
// 前置单引号是表格软件公认的"照原样当文本"标记；导入回数据库或用脚本读时，
// 那个引号是可见的、能被 strip 掉的——比一份能在运营机器上执行的表格好得多。
func csvText(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsRune(csvFormulaTriggers, rune(value[0])) {
		return "'" + value
	}
	return value
}

// csvTime 格式化时刻。零值给空串——CSV 里没有 null，一个空单元格就是"没有"。
func csvTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// csvTimePtr 同上，nil 给空串。
func csvTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return csvTime(*t)
}

// csvID 格式化一个可能不存在的 id。0 表示"该行没有这个字段"，给空串。
//
// 与仓储侧把 NULL 折成 0 是配套的：SQL 那边折成 0 是为了让整列保持数字类型，
// 到了文件这边再折回空，运营看到的就是一个空格子而不是一个不存在的 0 号账号。
func csvID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// ============================================================================
// 提现单
// ============================================================================

// supplierWithdrawalCSVHeader 是提现导出的表头。
//
// 列名用 snake_case 的英文而不是中文：这份文件的下游可能是一段脚本、一次
// 数据库导入或一个财务系统，而中文表头在那些地方全都要再映射一次。
// 中文说明留给运营端界面上的下载按钮。
var supplierWithdrawalCSVHeader = []string{
	"id", "user_id", "user_email", "amount", "status",
	"payout_channel", "payout_account", "user_note",
	"ledger_id", "reviewer_id", "reviewer_email", "review_note", "external_ref",
	"created_at", "resolved_at",
}

// supplierWithdrawalCSVRow 把一张单子摊成一行。顺序必须与表头逐字对应。
func supplierWithdrawalCSVRow(row *service.SupplierWithdrawalExportRow) []string {
	return []string{
		strconv.FormatInt(row.ID, 10),
		strconv.FormatInt(row.UserID, 10),
		csvText(row.UserEmail),
		row.Amount, // NUMERIC 原文，不过转义：它不可能以公式字符开头（DECIMAL 且 > 0）
		csvText(row.Status),
		csvText(row.PayoutChannel),
		csvText(row.PayoutAccount),
		csvText(row.UserNote),
		csvID(row.LedgerID),
		csvID(row.ReviewerID),
		csvText(row.ReviewerEmail),
		csvText(row.ReviewNote),
		csvText(row.ExternalRef),
		csvTime(row.CreatedAt),
		csvTimePtr(row.ResolvedAt),
	}
}

// ============================================================================
// 钱包流水
// ============================================================================

// supplyLedgerCSVHeader 是流水导出的表头。
//
// 三个 *_after 快照列是这份文件能被**独立核对**的关键：拿到一段连续流水，
// 每一行的 available_after 都能由上一行加减本行金额验出来。没有它们，
// 这份文件只能被信任，不能被核对。
var supplyLedgerCSVHeader = []string{
	"id", "user_id", "user_email", "action", "amount",
	"request_id", "account_id", "source_user_id",
	"basis_amount", "share_ratio", "frozen_until",
	"available_after", "frozen_after", "history_after",
	"remark", "created_at",
}

// supplyLedgerCSVRow 把一条流水摊成一行。顺序必须与表头逐字对应。
func supplyLedgerCSVRow(row *service.SupplyLedgerExportRow) []string {
	return []string{
		strconv.FormatInt(row.ID, 10),
		strconv.FormatInt(row.UserID, 10),
		csvText(row.UserEmail),
		csvText(row.Action),
		row.Amount,
		csvText(row.RequestID),
		csvID(row.AccountID),
		csvID(row.SourceUserID),
		row.BasisAmount,
		row.ShareRatio,
		csvTimePtr(row.FrozenUntil),
		row.AvailableAfter,
		row.FrozenAfter,
		row.HistoryAfter,
		csvText(row.Remark),
		csvTime(row.CreatedAt),
	}
}

// ============================================================================
// 尾行
// ============================================================================

// supplyExportTrailerPrefix 是尾行的第一个单元格。
//
// 以 # 开头是为了让人一眼看出它不是数据；用一个固定前缀而不是空行，是为了让
// 脚本能判定"文件到这里是完整的"。
const supplyExportTrailerPrefix = "#"

// supplyExportTrailer 生成文件末尾那一行。
//
// # 为什么一定要有这一行
//
// 流式导出的响应头在第一行数据之前就发出去了，此后无论发生什么都改不了状态码。
// 于是有两件事没有别的地方可说：
//
//   - **撞了行数上限**（后面还有账没导出来）；
//   - **文件是不是完整地写完了**（中途数据库掉线的话，运营拿到的是一个
//     "下载成功"的残缺文件）。
//
// 第二件是更危险的那一件：一份静默截断的对账文件比一次失败的下载危险得多，
// 后者你会重试，前者你会照着它打款。所以尾行**总是**写——看到它就是完整的，
// 没看到就是没写完。代价是表格软件里多出一行，值。
//
// 窗口也写在这一行：一份对账文件如果不写清自己覆盖了哪段时间，
// 在硬盘上放三天之后就没人敢用了。
func supplyExportTrailer(outcome service.SupplyExportOutcome, window service.SupplyExportWindow) []string {
	note := fmt.Sprintf("exported %d rows for %s .. %s (UTC)",
		outcome.Rows, csvTime(window.StartAt), csvTime(window.EndAt))
	if outcome.Truncated {
		note = fmt.Sprintf("TRUNCATED at %d rows (limit %d) for %s .. %s (UTC)"+
			" -- narrow the time range and export again",
			outcome.Rows, service.SupplyExportMaxRows, csvTime(window.StartAt), csvTime(window.EndAt))
	}
	return []string{supplyExportTrailerPrefix, note}
}

// writeSupplyExportCSV 是两条导出路径共用的骨架。
//
// stream 负责把行推给 write，本函数负责 BOM、表头、尾行与刷盘节奏。
// 抽出来是因为"忘了写尾行"是这套东西里最容易犯、也最难被发现的错。
func writeSupplyExportCSV(
	writer *csv.Writer,
	header []string,
	window service.SupplyExportWindow,
	stream func(write func([]string) error) (service.SupplyExportOutcome, error),
) error {
	if err := writer.Write(header); err != nil {
		return err
	}
	outcome, err := stream(writer.Write)
	if err != nil {
		// 把已经攒在缓冲里的行刷出去，然后**不写尾行**就返回。
		//
		// 刷是为了让失败的表现前后一致：错在第 5000 行时，前 4000 多行早就
		// 上了网络，拦不回来了；不刷的话错在第 3 行反而一行都不给，
		// 于是"文件有多短"取决于它在哪一批缓冲里挂掉——一个没有意义的随机数。
		// 无论刷不刷，可辨识特征只有一个：**末尾没有那一行 #**。
		writer.Flush()
		return err
	}
	if err := writer.Write(supplyExportTrailer(outcome, window)); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}
