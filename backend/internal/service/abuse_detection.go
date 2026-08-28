// APEXONE-EXT: 异常使用模式检测——「像在批量抽取模型输出」的信号。
//
// # 这个检测器诚实的能力边界
//
// 它**检测不到蒸馏**。蒸馏 = 大量正常调用 + 把输出存下来训练，而「存下来」发生在
// 用户自己的机器上，平台看不见。任何声称能检测蒸馏的东西都是在检测别的东西。
//
// 它能检测的是**流量形状**：一段时间内请求很多、每条 prompt 都不一样、几乎不复用
// 上下文。这个形状确实是批量抽取的必要条件，但它同样是合法批量任务（数据标注、
// 评测集生成、内容批处理）的形状。**两者在流量层面无法区分。**
//
// 所以这里只做两件事：给运营发信号、把速率压下来。**不封号、不没收余额。**
// 判据本身不足以支撑那种代价——它误伤的是真实付费用户，而我们拿不出证据自证。
//
// # 判据是怎么来的
//
// 从线上真实数据反推。一个重度 Claude Code 用户（4.5 小时、几百次请求）长这样：
//
//	缓存读中位 723,655 tok    真实输入中位 2 tok    session 数 1
//
// 高频、但**极高缓存复用**、**真实输入极小**、**单一 session**——因为长会话必然
// 复用上下文。批量抽取恰好相反：每条 prompt 都是新的，缓存无从复用，session 分散。
//
// 于是判据是三者的**合取**，缺一不可：
//
//  1. 请求量足够大（否则任何偶发行为都会中招）
//  2. 缓存命中率低（长会话不可能低）
//  3. 单位请求的真实输入大（长会话的输入极小，因为上下文在缓存里）
//
// 单独任何一条都会误伤：只看请求量会打到重度用户，只看缓存率会打到第一次会话，
// 只看输入量会打到长 prompt 的单次调用。
//
// # 为什么不复用 OpsAlertEvaluatorService
//
// 那套告警的 scope 只认 platform / group / region 三个维度，没有 user 维度
// （ops_alert_evaluator_service.go 的 parseOpsAlertRuleScope）。加一个 user 维度
// 要动那个 switch 和 scope 解析器，而那是运维告警的核心路径。这里的检测是一件
// 独立的事，单独一个服务更清楚，也不会把一次判据调整变成运维告警的回归风险。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// AbuseSignal 是一个用户在观察窗内的流量画像。
//
// 刻意叫 signal 而不是 violation：它是一组读数，不是一个判决。判决由阈值和
// 人工复核共同做出，而读数本身对合法用户和滥用者都成立。
type AbuseSignal struct {
	UserID int64 `json:"user_id"`
	// Requests 观察窗内的请求数。
	Requests int64 `json:"requests"`
	// CacheHitRatio 缓存读取占总输入量的比例（0~1）。
	//
	// 长会话这个值接近 1（上下文全在缓存里），批量抽取接近 0（每条都是新 prompt）。
	// 这是三个判据里区分度最高的一个。
	CacheHitRatio float64 `json:"cache_hit_ratio"`
	// AvgInputTokens 平均每条请求的**真实**输入 token（不含缓存读取）。
	//
	// 实测重度 Claude Code 用户这个值是个位数——因为上下文在缓存里，每轮只发几个
	// token 的新指令。批量抽取每条都要发完整 prompt，这个值是几百到几千。
	AvgInputTokens float64 `json:"avg_input_tokens"`
	// DistinctSessions 观察窗内出现过的不同 session 数。
	//
	// 只作为辅助信息给运营看，**不参与判定**：客户端可以不发 session id，
	// 那时这个值恒为 0，拿它当判据会把一整类客户端误伤。
	DistinctSessions int64 `json:"distinct_sessions"`
	// StandardCost 观察窗内按官方牌价计的消耗。给运营判断严重程度用。
	StandardCost float64 `json:"standard_cost"`
	// WindowStart 观察窗起点。
	WindowStart time.Time `json:"window_start"`
}

// AbuseDetectionSettings 是检测与处置的全部可调参数。
//
// 默认值把这个功能整体关着（Enabled=false）：一个会自动改用户限速的东西，
// 必须由运营看过阈值、确认过自己的流量画像之后再显式打开。
type AbuseDetectionSettings struct {
	// Enabled 总开关。默认关。
	Enabled bool `json:"enabled"`

	// WindowMinutes 观察窗长度。
	WindowMinutes int `json:"window_minutes"`
	// MinRequests 触发判定的最小请求数。低于这个数一律不判——样本太少时，
	// 缓存命中率和平均输入量都不稳定，容易把一次偶发行为放大成一个信号。
	MinRequests int64 `json:"min_requests"`
	// MaxCacheHitRatio 缓存命中率低于此值才算可疑。
	MaxCacheHitRatio float64 `json:"max_cache_hit_ratio"`
	// MinAvgInputTokens 平均真实输入高于此值才算可疑。
	MinAvgInputTokens float64 `json:"min_avg_input_tokens"`

	// ThrottleRPM 命中后把用户 RPM 压到多少。0 = 只告警不降速。
	//
	// 压到一个**仍然可用**的值而不是压到 1：合法的批量用户应该还能继续工作，
	// 只是慢下来；而批量抽取需要规模，速率一降它在经济上就不划算了。
	ThrottleRPM int `json:"throttle_rpm"`
	// AutoThrottle 是否自动降速。关着时只产生信号，由运营手动处置。
	AutoThrottle bool `json:"auto_throttle"`
}

// DefaultAbuseDetectionSettings 返回「关闭」状态的默认配置。
//
// 阈值取自线上实测：重度 Claude Code 用户的缓存命中率 >0.95、平均真实输入个位数。
// 所以 0.30 和 200 这两个门槛离正常用法很远——宁可漏报也不误伤，因为误伤的代价
// 是一个真实付费用户被限速，而漏报的代价只是继续观察。
func DefaultAbuseDetectionSettings() *AbuseDetectionSettings {
	return &AbuseDetectionSettings{
		Enabled:           false,
		WindowMinutes:     30,
		MinRequests:       200,
		MaxCacheHitRatio:  0.30,
		MinAvgInputTokens: 200,
		ThrottleRPM:       20,
		AutoThrottle:      false,
	}
}

// IsSuspicious 判断一组读数是否命中全部判据。
//
// 三个条件是**合取**，不是打分。用打分（比如加权求和过阈值）看起来更聪明，
// 但那会让「请求特别多」单独一条就把一个重度用户推过线，而那正是最该避免的
// 误伤——重度用户是我们最好的客户。
func (s *AbuseDetectionSettings) IsSuspicious(sig *AbuseSignal) bool {
	if s == nil || sig == nil || !s.Enabled {
		return false
	}
	if sig.Requests < s.MinRequests {
		return false
	}
	if sig.CacheHitRatio >= s.MaxCacheHitRatio {
		return false
	}
	if sig.AvgInputTokens <= s.MinAvgInputTokens {
		return false
	}
	return true
}

// AbuseSignalReader 是检测需要的那点数据访问。
//
// 窄接口而不是依赖整个 UsageLogRepository：这里只读一个聚合，声明成能装配的最小面，
// 单测里也就只用桩一个方法。
type AbuseSignalReader interface {
	// ScanAbuseSignals 返回观察窗内请求数达到 minRequests 的用户画像。
	//
	// 过滤下推到 SQL：全表拉回来再在 Go 里筛，会随用量线性变慢，而这是个
	// 周期任务，慢下去不会有人立刻发现。
	ScanAbuseSignals(ctx context.Context, since time.Time, minRequests int64, limit int) ([]AbuseSignal, error)
}

// SettingKeyAbuseDetection 异常使用检测配置的 settings key。
const SettingKeyAbuseDetection = "abuse_detection_settings"

// GetAbuseDetectionSettings 读检测配置，永不返回错误。
//
// **刻意不加进程内缓存**，与 supply_pool / supplier_settlement 那几个不同：
// 那些在计费与调度热路径上被反复读，缓存是必需的；这个每 5 分钟读一次，
// 加一层缓存只会让「运营刚改完配置、还要等一个 TTL 才生效」变成一个新的困惑点。
//
// fail-closed：读不到就回默认值（Enabled=false）。一个会自动改用户限速的东西，
// 绝不能因为一次配置读失败就"按上次的样子继续跑"。
func (s *SettingService) GetAbuseDetectionSettings(ctx context.Context) *AbuseDetectionSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultAbuseDetectionSettings()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyAbuseDetection)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			slog.Warn("[AbuseDetector] failed to read settings, detection stays disabled", "error", err)
		}
		return DefaultAbuseDetectionSettings()
	}

	settings := DefaultAbuseDetectionSettings()
	if err := json.Unmarshal([]byte(raw), settings); err != nil {
		slog.Warn("[AbuseDetector] settings malformed, detection stays disabled", "error", err)
		return DefaultAbuseDetectionSettings()
	}
	return normalizeAbuseDetectionSettings(settings)
}

// SetAbuseDetectionSettings 写检测配置。夹回合法区间而不是报错——
// 一个越界的阈值应当被收敛成最接近的合法值，而不是让整次保存失败。
func (s *SettingService) SetAbuseDetectionSettings(ctx context.Context, settings *AbuseDetectionSettings) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("setting service unavailable")
	}
	if settings == nil {
		settings = DefaultAbuseDetectionSettings()
	}
	normalized := normalizeAbuseDetectionSettings(settings)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, SettingKeyAbuseDetection, string(payload))
}

// normalizeAbuseDetectionSettings 把参数夹回合法区间。
//
// 每一条下限都在防同一件事：一个手滑的 0 把判据变成「所有人都可疑」。
// 比如 MinRequests=0 会让任何发过一次请求的人都进入判定，而此时缓存命中率
// 由单条请求决定，几乎必然为 0——于是全站用户一起被降速。
func normalizeAbuseDetectionSettings(s *AbuseDetectionSettings) *AbuseDetectionSettings {
	out := *s
	if out.WindowMinutes < 5 {
		out.WindowMinutes = 5
	}
	if out.WindowMinutes > 24*60 {
		out.WindowMinutes = 24 * 60
	}
	if out.MinRequests < 50 {
		// 下限 50 而不是 1：样本太少时 cache_hit_ratio 与 avg_input 都不稳定，
		// 判据会退化成噪声放大器。
		out.MinRequests = 50
	}
	if out.MaxCacheHitRatio < 0 {
		out.MaxCacheHitRatio = 0
	}
	if out.MaxCacheHitRatio > 1 {
		out.MaxCacheHitRatio = 1
	}
	if out.MinAvgInputTokens < 0 {
		out.MinAvgInputTokens = 0
	}
	if out.ThrottleRPM < 0 {
		out.ThrottleRPM = 0
	}
	return &out
}
