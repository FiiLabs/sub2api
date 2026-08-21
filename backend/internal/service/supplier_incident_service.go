// APEXONE-EXT: 双边市场——供给账号失效事件服务。
//
// 一件事被拆成三个出口，它们的读者完全不同：
//
//  1. Sweep —— 后台的。每轮把「坏了但还没记」的号记下来、把「好了但还记着」的
//     事件关掉，然后给每一条新事件的主人发一封信。由 SupplierLifecycleService 驱动。
//  2. List / Summary —— 运营的。事件明细与封禁率报表。
//  3. GuardOnboarding —— 接入路径上的。一个反复往平台塞坏号的人要被拦下来。
//
// # 为什么第 3 个出口在这里而不是在 SupplierOnboardingService 里
//
// 熔断的判据是「这个人最近出了几次事」，只有本服务知道怎么数。把这段逻辑放到
// 接入服务里，就得把事件仓储也注入进去，于是接入服务同时持有两套完全无关的存储；
// 更糟的是「窗口怎么算」会在两个文件里各有一份。这里给出一个谓词，
// 接入服务调一次——它不需要知道事件是什么。
package service

import (
	"context"
	"log/slog"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// supplierIncidentSweepBatch 单轮开/关事件的上限，与仓储侧的批量上限同量级。
	supplierIncidentSweepBatch = 500
	// supplierIncidentNoticeBatch 单轮最多发多少封信。
	//
	// 比开事件的批量小一个量级是刻意的：开事件是一条 SQL，发信是 N 次 SMTP 往返，
	// 而这一整轮跑在生命周期任务 10 分钟的总超时里。一次波及几百个号的上游故障中，
	// 信会分几轮发完——notified_at 保证没有人会因此收到两封。
	supplierIncidentNoticeBatch = 50

	// supplierIncidentDefaultWindowDays / MaxWindowDays 报表窗口。
	// 与看板那两个常量取同样的值但不共用：一个是流水口径，一个是事件口径。
	supplierIncidentDefaultWindowDays = 30
	supplierIncidentMaxWindowDays     = 365
)

// ErrSupplierIncidentRateExceeded 这个人最近坏掉的号太多，暂时不让再接入。
//
// # 文案为什么这么写
//
// 它必须同时做到两件事：让一个真的在刷坏号的人知道自己被拦了，以及让一个
// 只是运气不好（上游一次大面积封号波及了他）的正常供给者知道这不是永久的、
// 也不是他账号本身出了问题。所以说的是「最近」和「稍后再试」，
// 而不是「你被限制了」。
//
// 刻意不告诉他具体阈值：那等于把绕过方法一起发给他。
var ErrSupplierIncidentRateExceeded = infraerrors.BadRequest(
	"SUPPLIER_INCIDENT_RATE_EXCEEDED",
	"too many of your supply accounts have failed recently, please try again later")

// supplierIncidentNoticeSender 是通知器的发信子集。
//
// 窄接口的理由同本模块其它几处：扫描不该有能力读用户、也不该有能力发别的信。
type supplierIncidentNoticeSender interface {
	NotifyIncident(ctx context.Context, incident *SupplierAccountIncident) error
}

// SupplierIncidentService 是失效事件的唯一入口。
type SupplierIncidentService struct {
	repo     SupplierIncidentRepository
	notifier supplierIncidentNoticeSender
}

// NewSupplierIncidentService 构造事件服务。
//
// notifier 允许为 nil：那样检测与报表照常工作，只是不发信。这不是降级容错，
// 是部署形态——没有配 SMTP 的部署本来就发不出信，而「记录坏号」这件事
// 不该因此停摆。显式判空再赋值，理由与 NewSupplierLifecycleService 里那处一样：
// 一个 nil 的具体指针装进接口变量后不是 nil 接口。
func NewSupplierIncidentService(repo SupplierIncidentRepository, notifier *SupplierIncidentNotifier) *SupplierIncidentService {
	svc := &SupplierIncidentService{repo: repo}
	if notifier != nil {
		svc.notifier = notifier
	}
	return svc
}

func (s *SupplierIncidentService) ready() bool {
	return s != nil && s.repo != nil
}

// unavailable 是「服务没装配起来」的统一回答，与运营视图同一个理由：
// 一个空的事件列表和「最近一个号都没坏」长得一模一样。
func (s *SupplierIncidentService) unavailable() error {
	return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supply incident service unavailable")
}

// ============================================================================
// 后台：检测 + 通知
// ============================================================================

// Sweep 跑一轮检测与通知。
//
// # 顺序：先关后开，最后才发信
//
// 先关（ResolveIncidents）再开（OpenIncidents）不是随意的。反过来的话，
// 一个在本轮之前刚刚恢复的号会先被 OpenIncidents 看一眼——它此刻已经健康，
// 所以不会被开出新事件，这一步没问题；但如果顺序反过来而中间又恰好出错返回，
// 留下的是一批本该关掉却还开着的事件，而它们会一直显示在"当前坏着"里。
// 先把已知能关的关掉，是这两步里唯一能减少错误状态的那一步。
//
// 发信排在最后，因为它依赖前两步的结果：只有还开着、且没发过信的事件才发。
//
// # 任何一步失败都不打断后面
//
// 三步之间没有依赖关系（关事件失败不影响开事件，开事件失败也不影响给已有事件
// 发信）。一步失败就整轮返回，会让一次数据库抖动变成"这一轮什么都没做"，
// 而下一轮在五分钟之后。
func (s *SupplierIncidentService) Sweep(ctx context.Context) {
	if !s.ready() {
		return
	}

	if resolved, err := s.repo.ResolveIncidents(ctx, supplierIncidentSweepBatch); err != nil {
		slog.Error("[SupplierIncident] failed to resolve recovered incidents", "error", err)
	} else if resolved > 0 {
		slog.Info("[SupplierIncident] supply accounts recovered", "count", resolved)
	}

	if opened, err := s.repo.OpenIncidents(ctx, supplierIncidentSweepBatch); err != nil {
		slog.Error("[SupplierIncident] failed to open incidents for failed accounts", "error", err)
	} else if opened > 0 {
		// Warn 而不是 Info：新开的事件意味着有供给者此刻正在损失收益。
		// 这条日志是运营在看板之外唯一会主动撞见的信号。
		slog.Warn("[SupplierIncident] supply accounts became unavailable", "count", opened)
	}

	s.notifyPending(ctx)
}

// notifyPending 给还没发过信的未结事件各发一封。
//
// 发信成功才 MarkNotified：反过来（先标记再发）会在 SMTP 出问题时把通知
// 永久丢掉——那条事件此后永远是"已通知"，而供给者一个字也没收到。
// 代价是发成功但标记失败时可能重发一封，那是两者中明显较轻的一个。
func (s *SupplierIncidentService) notifyPending(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	pending, err := s.repo.ListPendingNotice(ctx, supplierIncidentNoticeBatch)
	if err != nil {
		slog.Error("[SupplierIncident] failed to list pending incident notices", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	if len(pending) == supplierIncidentNoticeBatch {
		slog.Warn("[SupplierIncident] notice batch is full, remainder deferred to next run",
			"limit", supplierIncidentNoticeBatch)
	}

	sent := 0
	for i := range pending {
		if ctx.Err() != nil {
			return
		}
		incident := &pending[i]
		if err := s.notifier.NotifyIncident(ctx, incident); err != nil {
			slog.Error("[SupplierIncident] failed to notify supplier about a failed account",
				"incident_id", incident.ID, "account_id", incident.AccountID,
				"user_id", incident.UserID, "error", err)
			continue
		}
		if err := s.repo.MarkNotified(ctx, incident.ID); err != nil {
			// 信已经发出去了，只是没记上。下一轮会重发一封——刺耳但不危险，
			// 而这条日志是把它和"根本没发出去"区分开的唯一线索。
			slog.Warn("[SupplierIncident] notice was sent but could not be recorded; it may be sent again",
				"incident_id", incident.ID, "error", err)
			continue
		}
		sent++
	}
	if sent > 0 {
		slog.Info("[SupplierIncident] notified suppliers about failed accounts", "count", sent)
	}
}

// ============================================================================
// 运营：明细与报表
// ============================================================================

// List 分页读事件明细。
func (s *SupplierIncidentService) List(ctx context.Context, filter SupplierIncidentFilter) ([]SupplierAccountIncident, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	filter.Page, filter.PageSize = clampSupplyAdminPage(filter.Page, filter.PageSize)
	return s.repo.List(ctx, filter)
}

// Summary 读封禁率报表。windowDays <= 0 用默认值。
func (s *SupplierIncidentService) Summary(ctx context.Context, windowDays, topN int) (*SupplierIncidentSummary, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if windowDays <= 0 {
		windowDays = supplierIncidentDefaultWindowDays
	}
	if windowDays > supplierIncidentMaxWindowDays {
		windowDays = supplierIncidentMaxWindowDays
	}
	return s.repo.Summary(ctx, windowDays, topN)
}

// ============================================================================
// 接入：熔断
// ============================================================================

// GuardOnboarding 判断一个人还能不能再接一个号。
//
// # 三条设计约束
//
//  1. **闸默认是关的**（limits.incidentCapEnabled() 为假时直接放行，连查询都不发）。
//     理由与每 IP 上限一样，见 setting_supply_onboarding.go 头部：一道配错的准入闸
//     造成的损失是静默的——人来了、挂不上、走了，不会有人来报障。
//  2. **数的是事件次数，不是坏号个数**。同一个号反复坏是最强的信号，而按号去重
//     会把它压成 1。这与 SupplierIncidentRate.Rate 的口径是同一条。
//  3. **查询失败一律放行**。这是与其它两道闸相反的方向：那两道数的是当下的账号数
//     （一次简单的 COUNT，失败几乎只可能是库挂了，而库挂了接入本来也走不通）；
//     这道闸数的是历史事件，它失败时接入路径的其余部分完全正常——为一次统计查询
//     的抖动把一个正常供给者挡在门外，代价大于放进来一个坏号（下一轮扫描仍然会
//     记下他，闸恢复后照样拦得住）。
func (s *SupplierIncidentService) GuardOnboarding(ctx context.Context, userID int64, limits *SupplyOnboardingSettings) error {
	if !s.ready() || userID <= 0 || !limits.incidentCapEnabled() {
		return nil
	}
	since := time.Now().Add(-limits.incidentWindow())
	recent, err := s.repo.CountRecentByUser(ctx, userID, since)
	if err != nil {
		slog.Warn("[SupplierIncident] failed to count recent incidents, letting onboarding through",
			"user_id", userID, "error", err)
		return nil
	}
	if !limits.incidentCapReached(recent) {
		return nil
	}
	// 每一次拦截都记一条：这道闸拦住的可能是一个真实的人，而他唯一能做的是
	// 找运营。运营那时手上必须有这条日志，否则只能回答「系统说不行」。
	slog.Warn("[SupplierIncident] onboarding blocked by incident rate",
		"user_id", userID, "recent_incidents", recent,
		"max", limits.MaxIncidentsPerUser, "window_hours", limits.IncidentWindowHours)
	return ErrSupplierIncidentRateExceeded
}
