// APEXONE-EXT: 双边市场——供给账号失效事件的领域类型与仓储接口。
//
// 「失效事件」= 一个供给账号从可用变成不可用的一段时间。判据、边界与三个时刻的
// 含义写在 migrations/233_supplier_account_incidents.sql 的文件头，那里是这件事的
// 权威说明；本文件只放 Go 侧的类型与接口，SQL 在
// internal/repository/supplier_incident_repo.go。
//
// # 为什么开/关事件是两条集合语句而不是一个循环
//
// 仓储接口上的 OpenIncidents / ResolveIncidents 都不接收账号列表：它们各自是**一条**
// SQL，在库里一次算完「哪些号坏了但还没建事件」和「哪些事件对应的号已经好了」。
//
// 换成「先列出坏号，再逐个插入」的写法要多跑 N+1 次往返，而且中间那段时间里
// 账号状态还会变——扫到的号在插入前恰好恢复，就会留下一条一出生就该被关掉的事件。
// 集合语句里读和写在同一个快照上，这种竞态不存在。
package service

import (
	"context"
	"time"
)

// SupplierIncidentErrorMaxLen 存进事件行的失败原因上限。
//
// 与探测失败那边（supplyProbeErrorMaxLen）取同一个量级、但**不共用常量**：
// 那个数字约束的是写进 accounts.extra 的一小段 JSON，改动理由是「别让账号行膨胀」；
// 这里约束的是一张追加表的一列，改动理由是「别让报表里一行撑满屏幕」。
// 两者迟早会因为各自的理由分头调整。
const SupplierIncidentErrorMaxLen = 500

// SupplierAccountIncident 是一条失效事件。
type SupplierAccountIncident struct {
	ID        int64 `json:"id"`
	AccountID int64 `json:"account_id"`
	UserID    int64 `json:"user_id"`
	// AccountName / Platform 是出事那一刻的快照，见迁移文件头。
	AccountName string `json:"account_name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	// Status 出事时的上游状态（error / rate_limited / ...）。
	Status string `json:"status"`
	// ErrorMessage 上游给的原因。**只在管理端呈现**，不进供给者的邮件。
	ErrorMessage string     `json:"error_message,omitempty"`
	DetectedAt   time.Time  `json:"detected_at"`
	NotifiedAt   *time.Time `json:"notified_at,omitempty"`
	// ResolvedAt 空 = 仍然坏着。前端据此把行标红。
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// Open 事件是否仍然开着。
func (i *SupplierAccountIncident) Open() bool {
	return i != nil && i.ResolvedAt == nil
}

// SupplierIncidentFilter 是事件列表的查询条件。
type SupplierIncidentFilter struct {
	// UserID > 0 时只看某个供给者（从名册或封禁率榜点进来）。
	UserID int64
	// AccountID > 0 时只看某个号的历次事件——「这个号是不是反复坏」。
	AccountID int64
	// OpenOnly 只看还没恢复的。运营每日巡检的默认视图。
	OpenOnly bool
	StartAt  *time.Time
	EndAt    *time.Time
	Page     int
	PageSize int
}

// SupplierIncidentRate 是封禁率报表里的一行：一个供给者在窗口内的失效情况。
//
// # 为什么同时给出次数、账号数和比率
//
// 三个数各自会骗人，凑在一起才有意义：
//
//   - 只看**次数**，挂了 50 个号坏 5 次的人比挂 1 个号坏 3 次的人更糟——而后者
//     其实是那个更该被看一眼的（他的号一直在坏）。
//   - 只看**比率**，一个只挂 1 个号、坏了 1 次的新人是 100%，会盖住榜单。
//   - **未结数**回答的是另一个问题：现在还有几个坏着。它才是「该不该现在联系他」
//     的依据，前两个是「他这个人靠不靠谱」的依据。
//
// 比率在这里定义成 Incidents / Accounts（窗口内事件数 ÷ 当下账号数），不是
// 「坏掉的账号占比」。后者上限是 1，会把「同一个号反复坏」这个最强的信号压平。
type SupplierIncidentRate struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
	// Accounts 此刻名下还在的供给账号数。0 是可能的——他的号全解绑了，
	// 但窗口内的事件仍然算在他头上（那正是需要被看见的一种模式）。
	Accounts int64 `json:"accounts"`
	// Incidents 窗口内新发生的事件数。
	Incidents int64 `json:"incidents"`
	// OpenIncidents 此刻仍未恢复的事件数，**不受窗口限制**——一个坏了三个月的号
	// 不该因为它是在窗口之前坏的就从"现在有几个坏着"里消失。
	OpenIncidents int64 `json:"open_incidents"`
	// Rate = Incidents / Accounts，Accounts 为 0 时给 0（除零在报表里没有意义，
	// 而 Incidents 那一列已经把话说完了）。
	Rate float64 `json:"rate"`
	// LastDetectedAt 窗口内最后一次出事的时刻。
	LastDetectedAt *time.Time `json:"last_detected_at,omitempty"`
}

// SupplierIncidentSummary 是封禁率报表的顶部一屏。
type SupplierIncidentSummary struct {
	// WindowDays 窗口天数。
	WindowDays int `json:"window_days"`
	// Opened 窗口内新发生的事件数。
	Opened int64 `json:"opened"`
	// Resolved 窗口内恢复的事件数。
	//
	// 与 Opened 分开报而不是相减：净增只能回答「坏的号变多了还是变少了」，
	// 回答不了「这些号是自己好了还是一直坏着」。一个 Opened 高、Resolved 也高的
	// 站点是健康的（号会掉线但会恢复）；Opened 高、Resolved 近零的站点不是。
	Resolved int64 `json:"resolved"`
	// Open 此刻仍未恢复的事件总数，不受窗口限制。
	Open int64 `json:"open"`
	// Accounts 此刻全站还在的供给账号数，给 Opened 一个分母。
	Accounts int64 `json:"accounts"`
	// Suppliers 窗口内至少出过一次事的供给者人数。
	Suppliers int64 `json:"suppliers"`
	// Top 按窗口内事件数倒序的供给者榜。长度由调用方给的 topN 决定。
	Top []SupplierIncidentRate `json:"top"`
}

// SupplierIncidentRepository 是失效事件的数据访问。
//
// 手写 SQL 的理由与本模块其他仓储一致：开/关事件是两条跨表的集合语句，
// 报表是 accounts × supplier_account_incidents × users 的聚合，ent 的 builder
// 两者都表达不了。
type SupplierIncidentRepository interface {
	// OpenIncidents 为所有「坏着但还没有未结事件」的供给账号各开一条事件，
	// 返回新开的条数。幂等：已经有未结事件的号不会再开一条。
	OpenIncidents(ctx context.Context, limit int) (int64, error)
	// ResolveIncidents 关掉所有「对应账号已经恢复或已经不在了」的未结事件，
	// 返回关掉的条数。
	ResolveIncidents(ctx context.Context, limit int) (int64, error)

	// ListPendingNotice 取还没通知过的未结事件。
	ListPendingNotice(ctx context.Context, limit int) ([]SupplierAccountIncident, error)
	// MarkNotified 记下「这条事件的信发出去了」。
	MarkNotified(ctx context.Context, id int64) error

	// List 分页读事件明细。
	List(ctx context.Context, filter SupplierIncidentFilter) ([]SupplierAccountIncident, int64, error)
	// Summary 读封禁率报表。windowDays 是统计窗口，topN 是榜单长度。
	Summary(ctx context.Context, windowDays, topN int) (*SupplierIncidentSummary, error)

	// CountRecentByUser 数一个人在 since 之后出了几次事——接入熔断的判据。
	CountRecentByUser(ctx context.Context, userID int64, since time.Time) (int, error)
}
