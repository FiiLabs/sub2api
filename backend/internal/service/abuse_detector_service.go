// APEXONE-EXT: 异常使用模式的周期扫描与处置。
//
// 判据的含义与它诚实的能力边界写在 abuse_detection.go 的文件头。本文件只管
// 「多久扫一次、扫到了怎么办」。
//
// # 处置为什么是降速而不是封号
//
// 判据检测的是流量形状，而合法批量任务（数据标注、评测集、内容批处理）与批量
// 抽取的形状**无法区分**。封号+没收余额这种代价，判据支撑不起——误伤的是真实
// 付费用户，而我们拿不出证据自证（证据本来就不存在于我们这一侧）。
//
// 降速的性质完全不同：批量抽取需要**规模**，速率一压它在经济上就不划算；而合法
// 批量用户只是慢一点，工作照做，损失可逆。误判的代价从「赔钱 + 舆情」降到
// 「一次人工确认」。
//
// 而且降速是**可逆**的：运营复核后调回原值即可，用户的数据、余额、密钥都没动过。
//
// # 为什么每轮都重算，不记「已处置」状态
//
// 没有 processed 标记，也没有解除逻辑。每一轮扫描都基于**当前观察窗**重新判定：
// 用户仍然可疑就保持降速，行为回到正常就不再命中——下一次运营调回 RPM 后不会
// 被立刻再压一次。
//
// 反过来（记状态 + 写解除逻辑）要多一张表、一套状态机，以及一个「什么时候算
// 恢复正常」的新判据——而那个判据同样不可靠。让判定保持无状态，是这里唯一
// 不会积累错误的做法。
package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"log/slog"
	"sync"
	"time"
)

const (
	// abuseDetectorDefaultInterval 默认扫描周期。
	//
	// 与观察窗（默认 30 分钟）不同量级是有意的：窗口决定「看多久的行为」，
	// 周期决定「多快做出反应」。5 分钟一轮意味着最坏情况下滥用者能多跑 5 分钟，
	// 而这点损失远小于把周期压到 1 分钟带来的扫描负载。
	abuseDetectorDefaultInterval = 5 * time.Minute
	// abuseDetectorRunTimeout 单轮超时。
	abuseDetectorRunTimeout = 2 * time.Minute
	// abuseDetectorScanLimit 单轮最多看多少个用户。
	//
	// 有上限是因为这是个周期任务：没有上限时，一次异常的流量高峰会让某一轮
	// 扫描跑很久，而下一轮已经在等着了。
	abuseDetectorScanLimit = 500
	// abuseDetectorMaxThrottlePerRun 单轮最多自动降速几个用户。
	//
	// 这是防止「判据配错」变成一次批量事故的最后一道闸：阈值填错的那一刻，
	// 最坏后果是 20 个用户被限速且日志里全是记录，而不是全站用户一起被压。
	abuseDetectorMaxThrottlePerRun = 20

	abuseDetectorLeaderLockKey = "abuse:detector:leader"
	abuseDetectorLeaderLockTTL = 90 * time.Second

	// abuseAlertCooldown 同一个用户两条告警之间的最小间隔。
	//
	// 比观察窗（默认 30 分钟）长：一个持续滥用的人在每一轮扫描里都会命中，
	// 而运营需要知道的是「这个人有问题」，不是「这个人有问题×288」。
	abuseAlertCooldown = 60 * time.Minute
	// abuseAlertSeverity 落进 ops_alert_events 的级别。
	//
	// warning 而不是 critical：判据是启发式的，命中不等于确认滥用，
	// 而 critical 在告警面板上意味着「现在就得有人处理」。
	abuseAlertSeverity = "warning"
)

// abuseUserLimitWriter 是降速需要的那点能力。
//
// 复用 adminService.BatchUpdateLimits 而不是自己写 UPDATE：它已经处理了
// **认证缓存失效**（admin_user.go 里 InvalidateAuthCacheByUserID）。少了那一步，
// 改完 RPM 最长一个 TTL 内不生效——一个静默的空操作。
type abuseUserLimitWriter interface {
	BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error)
}

// abuseUserLookup 用来在降速前确认这个人不是管理员。
type abuseUserLookup interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

// abuseAlertSink 把命中记到一个**运营查得到**的地方。
//
// 存在的理由是一条实测：这个功能此前的全部产出是一行 slog，而生产 CVM 的
// public_logs 是关着的（对一个机密计算产品这是正确设置——公开日志会泄漏），
// 于是那行日志谁也读不到。也就是说「只检测不处置」这个模式在当前部署形态下
// 等于**什么都没做**：既不拦，也没人知道它拦不拦得对。
//
// 落到 ops_alert_events 而不是新建一张表：那里已经有列表、按 severity 过滤、
// 状态流转和邮件通知，而这条信息本来就是一条告警。
type abuseAlertSink interface {
	CreateAlertEvent(ctx context.Context, event *OpsAlertEvent) (*OpsAlertEvent, error)
}

// AbuseDetectorService 周期扫描用量、标记可疑用户、（可选）自动降速。
type AbuseDetectorService struct {
	reader     AbuseSignalReader
	users      abuseUserLookup
	limits     abuseUserLimitWriter
	settings   *SettingService
	interval   time.Duration
	instanceID string
	lockCache  LeaderLockCache
	db         *sql.DB
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	// alerts 命中信号的去处。可选，见 SetAlertSink。
	alerts abuseAlertSink
	// alertedAt 每个用户上一次落告警的时刻，用来按 abuseAlertCooldown 去重。
	//
	// 放内存而不是查库：扫描每 5 分钟跑一次、观察窗 30 分钟，同一个真实滥用者
	// 会在每一轮里都命中——不去重的话一天 288 条，运营看到的是刷屏而不是信号。
	// 既有告警是按 rule_id 查活跃事件去重的，这里用不上：滥用信号要按**用户**
	// 去重，而这些事件共用一个 rule_id（0，它们不来自任何规则）。
	//
	// 进程重启会丢，代价是重启后可能多出一条重复告警——比为它加一张表便宜。
	alertedAt map[int64]time.Time
	alertMu   sync.Mutex
}

func NewAbuseDetectorService(
	reader AbuseSignalReader,
	users abuseUserLookup,
	limits abuseUserLimitWriter,
	settings *SettingService,
) *AbuseDetectorService {
	return &AbuseDetectorService{
		reader:     reader,
		users:      users,
		limits:     limits,
		settings:   settings,
		interval:   abuseDetectorDefaultInterval,
		instanceID: uuid.NewString(),
		stopCh:     make(chan struct{}),
	}
}

// SetAlertSink 注入命中信号的去处。不注入时行为回到从前：只有一行读不到的日志。
//
// 显式判空再赋值：一个 nil 的 *OpsService 装进接口变量后不是 nil 接口，
// 后面的 `s.alerts == nil` 就永远为假。
func (s *AbuseDetectorService) SetAlertSink(ops *OpsService) {
	if s == nil || ops == nil {
		return
	}
	s.alerts = ops
	s.alertedAt = map[int64]time.Time{}
}

// recordAlert 把一次命中落成一条告警事件，按用户去重。
//
// 失败只记日志：这一步是**观测**，它坏了不该影响检测本身，更不该影响降速。
func (s *AbuseDetectorService) recordAlert(ctx context.Context, sig *AbuseSignal, cfg *AbuseDetectionSettings, now time.Time) {
	if s == nil || s.alerts == nil || sig == nil {
		return
	}

	s.alertMu.Lock()
	if last, ok := s.alertedAt[sig.UserID]; ok && now.Sub(last) < abuseAlertCooldown {
		s.alertMu.Unlock()
		return
	}
	s.alertedAt[sig.UserID] = now
	s.alertMu.Unlock()

	requests := float64(sig.Requests)
	threshold := float64(cfg.MinRequests)
	event := &OpsAlertEvent{
		// rule_id = 0：这条事件不来自任何告警规则。那一列可空且没有外键，
		// 读路径也不 JOIN 规则表，所以 0 能被正常列出与查看。
		RuleID:   0,
		Severity: abuseAlertSeverity,
		Status:   OpsAlertStatusFiring,
		Title:    fmt.Sprintf("Suspicious usage pattern: user %d", sig.UserID),
		// 把三个判据的读数都写进正文：运营要判断的是「这是不是误伤」，
		// 而那个判断需要看到命中的是哪一条、离阈值多远。
		Description: fmt.Sprintf(
			"user=%d requests=%d (>= %d) cache_hit_ratio=%.3f (< %.2f) avg_input_tokens=%.0f (> %d) "+
				"distinct_sessions=%d standard_cost=%.4f window=%dmin auto_throttle=%t",
			sig.UserID, sig.Requests, cfg.MinRequests,
			sig.CacheHitRatio, cfg.MaxCacheHitRatio,
			sig.AvgInputTokens, int64(cfg.MinAvgInputTokens),
			sig.DistinctSessions, sig.StandardCost,
			cfg.WindowMinutes, cfg.AutoThrottle),
		MetricValue:    &requests,
		ThresholdValue: &threshold,
		Dimensions: map[string]any{
			"source":            "abuse_detector",
			"user_id":           sig.UserID,
			"requests":          sig.Requests,
			"cache_hit_ratio":   sig.CacheHitRatio,
			"avg_input_tokens":  sig.AvgInputTokens,
			"distinct_sessions": sig.DistinctSessions,
			"standard_cost":     sig.StandardCost,
			"window_minutes":    cfg.WindowMinutes,
			"auto_throttle":     cfg.AutoThrottle,
		},
		FiredAt:   now,
		CreatedAt: now,
	}

	if _, err := s.alerts.CreateAlertEvent(ctx, event); err != nil {
		slog.Error("[AbuseDetector] failed to record alert event",
			"user_id", sig.UserID, "error", err)
	}
}

// SetLeaderLock 注入领导者锁依赖。不注入时每个实例各扫各的——扫描本身只读，
// 重复扫不会出错；但自动降速会被重复执行，所以多实例部署必须注入。
func (s *AbuseDetectorService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *AbuseDetectorService) Start() {
	if s == nil || s.reader == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		// 刻意**不**启动即跑：与 SupplierLifecycleService 不同，这里没有
		// 「停机期间积压的到期任务」要补。启动瞬间正是各服务抢资源的时刻，
		// 一个可以等 5 分钟的扫描不该挤在那里。
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *AbuseDetectorService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *AbuseDetectorService) runOnce() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("[AbuseDetector] run panic", "recover", r)
		}
	}()

	cfg := s.currentSettings()
	if !cfg.Enabled {
		return
	}

	// 只有拿到锁的实例才动手。扫描是只读的，重复扫无害；但自动降速重复执行
	// 会把同一个用户压两次（第二次基于已被压低的值），所以整轮都在锁里。
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db,
		abuseDetectorLeaderLockKey, s.instanceID, abuseDetectorLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), abuseDetectorRunTimeout)
	defer cancel()

	since := time.Now().Add(-time.Duration(cfg.WindowMinutes) * time.Minute)
	signals, err := s.reader.ScanAbuseSignals(ctx, since, cfg.MinRequests, abuseDetectorScanLimit)
	if err != nil {
		slog.Error("[AbuseDetector] scan failed", "error", err)
		return
	}

	throttled := 0
	for i := range signals {
		sig := &signals[i]
		if !cfg.IsSuspicious(sig) {
			continue
		}

		// 无论处置与否都留一条日志。留着它是为了本地/自建部署——那里 stdout 读得到。
		//
		// 但在机密计算部署里它读不到（public_logs=false），所以下面那条 recordAlert
		// 才是真正让运营看得见的那一半。两条都留：它们服务的是两种不同的部署形态。
		slog.Warn("[AbuseDetector] suspicious usage pattern",
			"user_id", sig.UserID,
			"requests", sig.Requests,
			"cache_hit_ratio", sig.CacheHitRatio,
			"avg_input_tokens", sig.AvgInputTokens,
			"distinct_sessions", sig.DistinctSessions,
			"standard_cost", sig.StandardCost,
			"window_minutes", cfg.WindowMinutes,
			"auto_throttle", cfg.AutoThrottle,
		)

		// 落一条运营查得到的告警。**在处置判断之前**——自动降速关着时，
		// 这就是这个功能唯一的产出，不该被那个 continue 跳过。
		s.recordAlert(ctx, sig, cfg, time.Now())

		if !cfg.AutoThrottle || cfg.ThrottleRPM <= 0 {
			continue
		}
		if throttled >= abuseDetectorMaxThrottlePerRun {
			slog.Warn("[AbuseDetector] throttle cap reached, remainder deferred",
				"cap", abuseDetectorMaxThrottlePerRun)
			break
		}
		if s.throttle(ctx, sig, cfg.ThrottleRPM) {
			throttled++
		}
	}
}

// throttle 把一个用户的 RPM 压到 limit。返回是否真的改了。
func (s *AbuseDetectorService) throttle(ctx context.Context, sig *AbuseSignal, limit int) bool {
	if s.limits == nil {
		return false
	}

	// 管理员豁免。照抄 content_moderation 里自动封禁的同一道保护：一个配错的
	// 阈值不该把运营自己锁在门外——而运营恰恰是最可能产生高频调用的人。
	if s.users != nil {
		user, err := s.users.GetByID(ctx, sig.UserID)
		if err != nil {
			slog.Warn("[AbuseDetector] user lookup failed, skipping throttle",
				"user_id", sig.UserID, "error", err)
			return false
		}
		if user == nil || user.IsAdmin() {
			slog.Warn("[AbuseDetector] throttle skipped for admin", "user_id", sig.UserID)
			return false
		}
		// 已经被压到不高于目标值了就别再动——重复写会刷掉运营手工调过的值。
		if user.RPMLimit > 0 && user.RPMLimit <= limit {
			return false
		}
	}

	rpm := limit
	if _, err := s.limits.BatchUpdateLimits(ctx, []int64{sig.UserID}, nil, &rpm); err != nil {
		slog.Error("[AbuseDetector] throttle failed", "user_id", sig.UserID, "error", err)
		return false
	}
	slog.Warn("[AbuseDetector] user throttled",
		"user_id", sig.UserID, "rpm_limit", rpm,
		"reason", "suspicious usage pattern; review and restore manually")
	return true
}

func (s *AbuseDetectorService) currentSettings() *AbuseDetectionSettings {
	if s == nil || s.settings == nil {
		return DefaultAbuseDetectionSettings()
	}
	return s.settings.GetAbuseDetectionSettings(context.Background())
}
