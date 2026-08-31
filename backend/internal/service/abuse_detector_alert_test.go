//go:build unit

// APEXONE-EXT: 异常使用检测——把命中落成一条运营查得到的告警。
//
// 这一组存在的理由是一条实测：这个功能此前的全部产出是一行 slog，而生产 CVM 的
// public_logs 关着（对机密计算产品这是正确设置），于是那行日志谁也读不到。
// 「只检测不处置」在那种部署下等于什么都没做。
//
// 守两件事：
//
//  1. **按用户去重。** 既有告警是按 rule_id 查活跃事件去重的，这里用不上——
//     滥用信号共用 rule_id=0，按它去重会让一个用户的告警把其他所有人的压掉。
//     所以「不同用户互不影响」这条必须有测试，它正是那个模型不适用的地方。
//  2. **观测坏了不能影响检测。** sink 报错、sink 没装，都不该让扫描出问题。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// abuseAlertSinkStub 记下每一条落进去的告警事件。
type abuseAlertSinkStub struct {
	events []*OpsAlertEvent
	err    error
}

func (s *abuseAlertSinkStub) CreateAlertEvent(_ context.Context, event *OpsAlertEvent) (*OpsAlertEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.events = append(s.events, event)
	return event, nil
}

func newAlertingDetector(sink abuseAlertSink) *AbuseDetectorService {
	return &AbuseDetectorService{
		alerts:    sink,
		alertedAt: map[int64]time.Time{},
	}
}

func abuseSignal(userID int64) *AbuseSignal {
	return &AbuseSignal{
		UserID:           userID,
		Requests:         512,
		CacheHitRatio:    0.02,
		AvgInputTokens:   1800,
		DistinctSessions: 3,
		StandardCost:     4.25,
	}
}

func abuseCfg() *AbuseDetectionSettings {
	cfg := DefaultAbuseDetectionSettings()
	cfg.Enabled = true
	return cfg
}

// 一次命中落一条事件，且带着运营判断误伤所需要的全部读数。
func TestRecordAlertWritesAQueryableEvent(t *testing.T) {
	sink := &abuseAlertSinkStub{}
	svc := newAlertingDetector(sink)

	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), time.Unix(1700000000, 0))

	require.Len(t, sink.events, 1)
	ev := sink.events[0]

	// rule_id=0：这条事件不来自任何规则。那一列可空、无外键，读路径也不 JOIN
	// 规则表——这三条一起才让 0 是安全的。
	assert.Zero(t, ev.RuleID)
	assert.Equal(t, abuseAlertSeverity, ev.Severity)
	assert.Equal(t, OpsAlertStatusFiring, ev.Status)
	assert.Contains(t, ev.Title, "42")

	// 三个判据的读数都要能在事件上找到——运营要判断的是「命中的是哪一条、
	// 离阈值多远」，只给一个「可疑」的结论没法判。
	assert.Equal(t, int64(42), ev.Dimensions["user_id"])
	assert.Equal(t, int64(512), ev.Dimensions["requests"])
	assert.Equal(t, 0.02, ev.Dimensions["cache_hit_ratio"])
	assert.Equal(t, 1800.0, ev.Dimensions["avg_input_tokens"])
	assert.Equal(t, "abuse_detector", ev.Dimensions["source"])
	assert.Contains(t, ev.Description, "cache_hit_ratio=0.020")
	assert.Contains(t, ev.Description, "avg_input_tokens=1800")

	require.NotNil(t, ev.MetricValue)
	require.NotNil(t, ev.ThresholdValue)
	assert.Equal(t, 512.0, *ev.MetricValue)
	assert.Equal(t, 200.0, *ev.ThresholdValue)
}

// 冷却期内不重复落。
//
// 扫描每 5 分钟一轮、观察窗 30 分钟，一个持续滥用的人在**每一轮**里都命中。
// 不去重的话运营看到的是一天 288 条刷屏，而不是一个信号。
func TestRecordAlertDeduplicatesWithinCooldown(t *testing.T) {
	sink := &abuseAlertSinkStub{}
	svc := newAlertingDetector(sink)
	base := time.Unix(1700000000, 0)

	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), base)
	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), base.Add(5*time.Minute))
	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), base.Add(59*time.Minute))

	assert.Len(t, sink.events, 1, "冷却期内只该有第一条")

	// 冷却期一过，同一个人重新可报——他还在持续滥用这件事需要被再说一次。
	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), base.Add(abuseAlertCooldown+time.Minute))
	assert.Len(t, sink.events, 2)
}

// 去重是**按用户**的，不是全局的。
//
// 这条是这次改动与既有告警去重模型的分歧点：那边按 rule_id 查活跃事件，
// 而这些事件共用 rule_id=0——照抄的话，第一个被标记的用户会把其他所有人的
// 告警全部压掉，而运营完全看不出来。
func TestRecordAlertDedupIsPerUserNotGlobal(t *testing.T) {
	sink := &abuseAlertSinkStub{}
	svc := newAlertingDetector(sink)
	now := time.Unix(1700000000, 0)

	svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), now)
	svc.recordAlert(context.Background(), abuseSignal(43), abuseCfg(), now)
	svc.recordAlert(context.Background(), abuseSignal(44), abuseCfg(), now.Add(time.Minute))

	require.Len(t, sink.events, 3, "三个不同用户各自成条")
	seen := map[any]bool{}
	for _, ev := range sink.events {
		seen[ev.Dimensions["user_id"]] = true
	}
	assert.Len(t, seen, 3)
}

// ---------------------------------------------------------------------------
// 观测坏了不能影响检测
// ---------------------------------------------------------------------------

// 没装 sink：行为回到从前（只有一行读不到的日志），不 panic。
func TestRecordAlertWithoutSinkIsANoop(t *testing.T) {
	svc := &AbuseDetectorService{}
	assert.NotPanics(t, func() {
		svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), time.Now())
	})
}

// sink 报错只记日志，不 panic，也不影响后续用户。
func TestRecordAlertSwallowsSinkFailure(t *testing.T) {
	sink := &abuseAlertSinkStub{err: errors.New("db down")}
	svc := newAlertingDetector(sink)

	assert.NotPanics(t, func() {
		svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), time.Now())
	})
	assert.Empty(t, sink.events)
}

// SetAlertSink 传 nil 指针不能把服务弄成「有一个会 panic 的 sink」。
//
// 一个 nil 的 *OpsService 装进接口变量后不是 nil 接口——这个坑在
// NewSupplierLifecycleService 里踩过，用测试钉住不再踩。
func TestSetAlertSinkIgnoresTypedNil(t *testing.T) {
	svc := &AbuseDetectorService{}
	var typedNil *OpsService
	svc.SetAlertSink(typedNil)

	assert.Nil(t, svc.alerts)
	assert.NotPanics(t, func() {
		svc.recordAlert(context.Background(), abuseSignal(42), abuseCfg(), time.Now())
	})
}
