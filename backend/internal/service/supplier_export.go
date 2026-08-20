// APEXONE-EXT: 双边市场——对账导出的领域类型与仓储接口。
//
// 这一层存在的理由很窄：**财务不在后台里对账**。运营看板回答的是"现在什么样"，
// 对账回答的是"上个月这一笔一笔加起来对不对"，后者的工具是表格软件和银行流水，
// 不是一个分页十条的列表。在导出出现之前，把一个月的提现单拿出来的唯一办法是
// 翻页抄写——抄错一行就是打错一笔钱。
//
// # 三条贯穿这一层的决定
//
//  1. **流式，不缓冲**。仓储把行一条条交给回调，handler 边收边往 http.ResponseWriter
//     写。流水表是这套系统里行数最多的表（每一次计费一行），把一年的量先攒进一个
//     切片再序列化，等于让运营点一次导出就有机会把整个网关 OOM 掉——而网关一停，
//     所有消费者的请求跟着停。为一个后台报表冒这个险不合算。
//
//  2. **金额按 NUMERIC 原文取，不过 float64**。这一层是全仓唯一这么做的地方：
//     别处的金额都是 `::double precision` 扫成 float64，因为那些值要参与计算。
//     导出不参与任何计算，它是给人和表格看的最终值，那就没有理由让它先绕一趟
//     二进制浮点。DECIMAL(20,8) 的高位数值在 float64 里不是精确可表示的，
//     而"对账文件里的数字与库里的数字有一分钱出入"是这份文件唯一不能有的毛病。
//
//  3. **筛子与屏幕共用**。导出复用 SupplierWithdrawalFilter / SupplyAdminLedgerFilter
//     和它们的 WHERE 拼装函数。如果导出比运营刚看过的那一屏多几行或少几行，
//     他会先怀疑账不对，而不是怀疑两边的筛子不一样。
//
// 本文件只放类型与接口；SQL 实现在 internal/repository/supplier_export_repo.go，
// CSV 编码在 internal/handler/supplier_export_csv.go。
package service

import (
	"context"
	"time"
)

// SupplyExportMaxRows 是单次导出的行数硬上限。
//
// 这个数字不是性能调优，是一道**必须能被看见**的闸。流式导出的致命失败模式是
// 静默截断：HTTP 200 与响应头在第一行数据之前就发出去了，此后任何问题都改不了
// 状态码，运营拿到的是一个"下载成功"的、少了一半行的对账文件——他会照着它打款。
// 所以上限存在，且撞上限时必须在文件末尾留下一行明确的记号（见 handler 侧的尾行）。
//
// 20 万行按流水单行约 200 字节算是 40MB 上下：一个表格软件还打得开、一次 HTTP
// 下载还传得完的量级。真要拉一整年的全站流水，正确做法是按月分几次拉。
const SupplyExportMaxRows = 200000

// SupplierWithdrawalExportRow 是导出文件里的一张提现单。
//
// 与 SupplierWithdrawal 刻意是**两个类型**，不是内嵌：
//   - 金额是字符串（NUMERIC 原文，见本文件顶部第 2 条）；
//   - 指针字段全部摊平成值，CSV 里没有 null 这个概念，一个空单元格就是空；
//   - 多了两个只有对账才需要的 email（收款人、处理人）。
//
// 更要紧的是方向：那个类型是**接口返回值**，剥字段是为了少给出去；这个类型是
// 一份给财务的工作单，字段是往里加的。两者的演化压力相反，合成一个迟早会有人
// 为了对账往接口响应里加一个 user_id。
type SupplierWithdrawalExportRow struct {
	ID        int64
	UserID    int64
	UserEmail string
	// Amount 申请金额，NUMERIC(20,8) 原文。
	Amount string
	Status string

	PayoutChannel string
	// PayoutAccount **明文**。库里是密文（迁移 232），这里解开——
	// 这份文件本身就是打款工作单，加密到运营眼前才解开等于没有导出。
	// 与此对应的边界写在 §3.7 末段：加密防的是脱库，不是防运营本人。
	PayoutAccount string
	UserNote      string

	LedgerID int64

	ReviewerID    int64
	ReviewerEmail string
	ReviewNote    string
	// ExternalRef 打款凭证号。对账时把这一列与银行流水对上，是这份文件的主要用途。
	ExternalRef string

	CreatedAt  time.Time
	ResolvedAt *time.Time
}

// SupplyLedgerExportRow 是导出文件里的一条钱包流水。
//
// 金额四件套（amount / basis_amount / share_ratio 与三个余额快照）全是 NUMERIC
// 原文字符串。快照三列是这份文件能被独立核对的关键：拿到一段连续流水，
// 每一行的 available_after 都能用上一行加减本行金额验出来，不必信任服务端算术。
type SupplyLedgerExportRow struct {
	ID        int64
	UserID    int64
	UserEmail string
	Action    string
	Amount    string
	RequestID string
	// AccountID / SourceUserID 0 = 该行没有这个字段（不是 id 为 0 的那一行）。
	AccountID    int64
	SourceUserID int64
	BasisAmount  string
	ShareRatio   string
	FrozenUntil  *time.Time
	// AvailableAfter / FrozenAfter / HistoryAfter 这一行落盘后的钱包快照。
	AvailableAfter string
	FrozenAfter    string
	HistoryAfter   string
	Remark         string
	CreatedAt      time.Time
}

// SupplierExportRepository 是对账导出的数据访问。
//
// 两个方法都是**推**而不是拉（回调，不是返回切片）：调用方拿不到全量结果，
// 也就没有一处代码能不小心把整张表攒进内存。回调返回错误即中止（handler 用它
// 把"客户端断开了"传下来，免得数据库继续为一个没人接的下载扫表）。
//
// 单起一个接口而不是往 SupplierAdminRepository / SupplierWithdrawalRepository 上加：
// 那两个接口各有一套 sqlmock 与集成测试，且提现那套的实现需要密文封装
// （payoutAccountCipher），运营视图那套不需要——合进任何一个都要把另一个的
// 依赖也拖进去。导出自己是一件完整的事，给它自己的接口。
type SupplierExportRepository interface {
	// StreamWithdrawals 按建单时间正序逐行推送提现单。
	//
	// limit <= 0 时用 SupplyExportMaxRows。truncated 为真表示还有行没推完——
	// 调用方**必须**把这件事写进产物，否则就是一份静默残缺的对账文件。
	StreamWithdrawals(ctx context.Context, filter SupplierWithdrawalFilter, limit int,
		fn func(*SupplierWithdrawalExportRow) error) (truncated bool, err error)

	// StreamLedger 按发生时间正序逐行推送全站钱包流水。语义同上。
	StreamLedger(ctx context.Context, filter SupplyAdminLedgerFilter, limit int,
		fn func(*SupplyLedgerExportRow) error) (truncated bool, err error)
}
