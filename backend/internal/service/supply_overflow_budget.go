// APEXONE-EXT: 双边市场——溢出日配额闸门与溢出率读数。
//
// §3.2 记着一条遗留风险：溢出率此前只有一条 Warn 日志，没有 metric、没有告警、
// 没有熔断。也就是说消费者只要能持续把供给池打空，就能长期用 0.5× 的价买到 1.0×
// 成本的服务，而平台侧唯一的反馈是日志里多几行。这个文件是那条风险的闸门。
//
// 三条设计约束：
//
//  1. **判定与计数是同一次写**。「先读计数、再判断、再加一」在并发下会超发：
//     配额设 100 时，几十个并发的耗尽请求都会读到 99 然后各自放行。所以配额判定
//     下沉成一条带 WHERE 的 upsert（见 repository 侧），返回行 = 允许，无行 = 已满。
//  2. **fail-closed**。计数写失败（数据库抖动、表还没迁移）时**不溢出**。理由与
//     GetSupplyPoolSettings 读不到时不溢出一致：溢出是要花平台的钱的动作，
//     花钱的决定不能建立在「我也不知道现在花了多少」之上。降级后的行为等同于
//     「溢出没开」——请求拿回它原本就会拿到的那个错误，不是新增的故障面。
//  3. **拦下来的次数单独记**。混进 overflow_count 的话，「今天 500 次」既可能是
//     花了 500 次的钱，也可能是省了 500 次，对运营的含义正好相反。
package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// SupplyOverflowCounter 是溢出日计数的存储侧接口，实现在 repository（raw SQL）。
//
// 与 LeaderLockCache 同样的分层理由：service 层不直接依赖具体存储。
type SupplyOverflowCounter interface {
	// TryConsumeDailyOverflow 原子地「判定 + 计数」。
	//
	// limit <= 0 表示不限量（仍然计数）。返回 allowed=false 表示当日配额已满，
	// 此时实现方应当把这一次记进 denied_count。error 非 nil 表示计数不可用，
	// 调用方必须按 fail-closed 处理（不溢出）。
	TryConsumeDailyOverflow(ctx context.Context, day time.Time, limit int) (allowed bool, err error)
	// GetDailyOverflowUsage 读当日计数，不改动任何计数。
	GetDailyOverflowUsage(ctx context.Context, day time.Time) (*SupplyOverflowUsage, error)
	// RecordOverflowExhausted 记一次「溢出了，但兜底池也空了」。
	//
	// 与前两个计数正交：那两个说的是溢出这条路上发生了什么，这个说的是
	// **用户拿到了一个错误**。它是「该不该加兜底账号」的唯一信号。
	//
	// 不返回给调用方任何判定——写失败只记日志。这一步发生在请求已经注定失败
	// 之后，为一次统计写失败再叠一层错误没有任何人受益。
	RecordOverflowExhausted(ctx context.Context, day time.Time) error
}

// SupplyOverflowUsage 是管理端要看的当日读数。
type SupplyOverflowUsage struct {
	// Day 平台时区下的自然日（YYYY-MM-DD）。
	Day string `json:"day"`
	// OverflowCount 当日实际溢出次数——平台按自营成本供了这么多次货。
	OverflowCount int64 `json:"overflow_count"`
	// DeniedCount 当日因配额已满而未溢出的次数。持续为正说明配额正在生效（省钱），
	// 同时也说明供给侧规模已经明显跟不上需求。
	DeniedCount int64 `json:"denied_count"`
	// ExhaustedCount 当日「溢出了但兜底池也空了」的次数——消费者真正拿到
	// "No available accounts" 的那一刻。
	//
	// 三个计数对应三种完全不同的处置：OverflowCount 涨（保险生效了，什么都不用做）、
	// DeniedCount 涨（保险被预算挡住了，调 daily_overflow_limit）、
	// ExhaustedCount 涨（**保险不够赔，加兜底账号**）。
	ExhaustedCount int64 `json:"exhausted_count"`
}

// supplyOverflowCounterHolder 是进程级的计数器句柄。
//
// 为什么是包级变量而不是 GatewayService/SettingService 上的字段：这两个结构体分别
// 在 gateway_service.go 与 setting_service.go 里，都是上游合并热区，为一个扩展功能
// 往里加字段要付一次每轮 sync 都会遇到的冲突。本包的扩展层已经有同形态的包级状态
// （supplyPoolCache / supplyProbationCache 两个配置缓存），进程内单例这一点也确实成立。
// 代价是测试要能重置，所以下面给了 setter 与 reset。
var supplyOverflowCounterHolder atomic.Value // SupplyOverflowCounter

// SetSupplyOverflowCounter 注入计数器。由 wire 在构造 SettingService 时调用一次。
func SetSupplyOverflowCounter(counter SupplyOverflowCounter) {
	if counter == nil {
		supplyOverflowCounterHolder.Store(supplyOverflowCounterBox{})
		return
	}
	supplyOverflowCounterHolder.Store(supplyOverflowCounterBox{counter: counter})
}

// supplyOverflowCounterBox 包一层是因为 atomic.Value 要求每次 Store 的动态类型一致，
// 而不同实现（真实 repo / 测试桩）是不同类型，直接存接口会 panic。
type supplyOverflowCounterBox struct {
	counter SupplyOverflowCounter
}

func loadSupplyOverflowCounter() SupplyOverflowCounter {
	box, ok := supplyOverflowCounterHolder.Load().(supplyOverflowCounterBox)
	if !ok {
		return nil
	}
	return box.counter
}

// allowSupplyOverflow 判定这一次是否还能溢出。
//
// 没有配置计数器时（例如单实例部署下这个 provider 没接上、或单测里）返回 true：
// 那是「本功能没装」而不是「配额已满」，把它当成满会静默地关掉溢出，比不装更难查。
// 真正的 fail-closed 只针对**装了但报错**——那是「装了却不知道花了多少」。
func allowSupplyOverflow(ctx context.Context, limit int) bool {
	counter := loadSupplyOverflowCounter()
	if counter == nil {
		return true
	}

	allowed, err := counter.TryConsumeDailyOverflow(ctx, timezone.Now(), limit)
	if err != nil {
		slog.Warn("[SupplyPool] overflow budget counter unavailable, refusing to overflow",
			"error", err, "daily_limit", limit)
		return false
	}
	return allowed
}

// recordSupplyOverflowExhausted 记一次「溢出了但兜底池也空了」。
//
// 与 allowSupplyOverflow 相反，这里**没有任何判定要做**：请求在调用它之前就已经
// 注定失败了。所以计数器没装、写失败，都只是少了一个统计数，绝不改变任何行为——
// 为一次统计写失败再叠一层错误，没有任何人受益。
//
// 用 context.WithoutCancel：客户端此刻多半正在断开（他刚拿到一个错误），
// 而这个计数恰恰在那种时刻最该被记下来。跟着请求 ctx 一起被取消的话，
// 越是集中爆发的耗尽，越是数不到。
func recordSupplyOverflowExhausted(ctx context.Context) {
	counter := loadSupplyOverflowCounter()
	if counter == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyOverflowRecordTimeout)
	defer cancel()
	if err := counter.RecordOverflowExhausted(writeCtx, timezone.Now()); err != nil {
		slog.Warn("[SupplyPool] failed to record an exhausted overflow", "error", err)
	}
}

// supplyOverflowRecordTimeout 统计写的上限。短：它挂在失败路径上，
// 而失败路径本身就是要尽快把错误还给调用方的。
const supplyOverflowRecordTimeout = 2 * time.Second

// GetSupplyOverflowUsage 给管理端读当日溢出读数。
//
// 挂在 SettingService 上是因为管理端那两个 handler 已经持有它，这样池配置页拿
// 「配置 + 当前用量」不必新增一条依赖。读不到时返回**零值而不是错误**：这是一块
// 只读的经营读数，让整个池配置页因为它打不开是不成比例的。
func (s *SettingService) GetSupplyOverflowUsage(ctx context.Context) *SupplyOverflowUsage {
	day := timezone.Now()
	empty := &SupplyOverflowUsage{Day: day.Format("2006-01-02")}

	counter := loadSupplyOverflowCounter()
	if counter == nil {
		return empty
	}
	usage, err := counter.GetDailyOverflowUsage(ctx, day)
	if err != nil {
		slog.Warn("[SupplyPool] failed to read overflow usage", "error", err)
		return empty
	}
	if usage == nil {
		return empty
	}
	return usage
}
