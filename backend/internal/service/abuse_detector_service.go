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

		// 无论处置与否都留一条日志。这是运营唯一能看到的东西——自动降速关着时，
		// 它就是这个功能的**全部**产出。
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
