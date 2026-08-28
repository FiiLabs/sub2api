//go:build unit

// APEXONE-EXT: 异常使用检测的单测。
//
// 这个文件真正在守的是**误伤**。判据本身很容易写对；难的是它在遇到一个合法的
// 重度用户时也保持沉默——而那类用户是我们最好的客户。
//
// 所以每一条正向用例（"该抓的抓到了"）都配一条反向用例（"不该抓的没被抓"），
// 且反向用例的数据直接取自线上实测的重度 Claude Code 用户画像。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 线上实测：重度 Claude Code 用户，4.5 小时数百次请求。
// 判据必须对这组数字保持沉默——这是整个功能的安全底线。
func legitHeavyUser() *AbuseSignal {
	return &AbuseSignal{
		UserID:           1,
		Requests:         820,    // 请求非常多
		CacheHitRatio:    0.9993, // 但缓存几乎全命中
		AvgInputTokens:   2,      // 真实输入极小
		DistinctSessions: 1,      // 单一会话
		StandardCost:     117,
	}
}

// 批量抽取：每条 prompt 都是新的，缓存无从复用。
func extractionPattern() *AbuseSignal {
	return &AbuseSignal{
		UserID:           2,
		Requests:         300,
		CacheHitRatio:    0.0,
		AvgInputTokens:   3000,
		DistinctSessions: 300,
		StandardCost:     15,
	}
}

func enabledSettings() *AbuseDetectionSettings {
	s := DefaultAbuseDetectionSettings()
	s.Enabled = true
	return s
}

// 核心用例：合法重度用户绝不能被判可疑。
//
// 这条比"抓到滥用者"更重要——漏报只是继续观察，误报是把一个真实付费用户限速。
func TestLegitimateHeavyUserIsNeverFlagged(t *testing.T) {
	cfg := enabledSettings()
	assert.False(t, cfg.IsSuspicious(legitHeavyUser()),
		"线上实测的重度 Claude Code 用户被判为可疑——这是必须避免的误伤")
}

func TestExtractionPatternIsFlagged(t *testing.T) {
	cfg := enabledSettings()
	assert.True(t, cfg.IsSuspicious(extractionPattern()))
}

// 三个判据是合取，缺一不可。
//
// 这组用例守的是「有人把它改成打分制」——那样"请求特别多"单独一条就能把
// 重度用户推过线。每一条都是：只满足两个条件时必须不命中。
func TestAllThreeCriteriaRequired(t *testing.T) {
	cfg := enabledSettings()
	cases := []struct {
		name string
		sig  *AbuseSignal
	}{
		{"请求量够+缓存低，但输入小（长会话首轮）", &AbuseSignal{
			Requests: 300, CacheHitRatio: 0.05, AvgInputTokens: 10,
		}},
		{"请求量够+输入大，但缓存高（重度用户发长 prompt）", &AbuseSignal{
			Requests: 300, CacheHitRatio: 0.95, AvgInputTokens: 5000,
		}},
		{"缓存低+输入大，但请求少（一次性批量任务）", &AbuseSignal{
			Requests: 10, CacheHitRatio: 0.0, AvgInputTokens: 5000,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, cfg.IsSuspicious(tc.sig), "只满足两个判据就命中了——判据必须是合取")
		})
	}
}

// 总开关关着时一律不判。默认配置就是关的。
func TestDisabledByDefault(t *testing.T) {
	assert.False(t, DefaultAbuseDetectionSettings().Enabled, "这个功能必须默认关闭")
	assert.False(t, DefaultAbuseDetectionSettings().IsSuspicious(extractionPattern()))
	assert.False(t, DefaultAbuseDetectionSettings().AutoThrottle, "自动降速必须默认关闭")
}

// 参数夹取：一个手滑的 0 不能把判据变成「所有人都可疑」。
func TestNormalizeClampsDangerousValues(t *testing.T) {
	out := normalizeAbuseDetectionSettings(&AbuseDetectionSettings{
		WindowMinutes: 0, MinRequests: 0, MaxCacheHitRatio: 5, MinAvgInputTokens: -1, ThrottleRPM: -5,
	})
	assert.GreaterOrEqual(t, out.WindowMinutes, 5)
	assert.GreaterOrEqual(t, out.MinRequests, int64(50), "MinRequests=0 会让任何发过一次请求的人都进入判定")
	assert.LessOrEqual(t, out.MaxCacheHitRatio, 1.0)
	assert.GreaterOrEqual(t, out.MinAvgInputTokens, 0.0)
	assert.GreaterOrEqual(t, out.ThrottleRPM, 0)
}

// ---------------------------------------------------------------------------
// 处置
// ---------------------------------------------------------------------------

type stubSignalReader struct {
	signals []AbuseSignal
	err     error
	calls   int
	since   time.Time
	minReq  int64
}

func (s *stubSignalReader) ScanAbuseSignals(_ context.Context, since time.Time, minRequests int64, _ int) ([]AbuseSignal, error) {
	s.calls++
	s.since = since
	s.minReq = minRequests
	return s.signals, s.err
}

type stubLimitWriter struct {
	calls   int
	userIDs []int64
	rpm     *int
	err     error
}

func (s *stubLimitWriter) BatchUpdateLimits(_ context.Context, userIDs []int64, _, rpmLimit *int) (int, error) {
	s.calls++
	s.userIDs = append(s.userIDs, userIDs...)
	s.rpm = rpmLimit
	return len(userIDs), s.err
}

type stubUserLookup struct {
	users map[int64]*User
	err   error
}

func (s *stubUserLookup) GetByID(_ context.Context, id int64) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.users[id], nil
}

func newDetector(reader AbuseSignalReader, users abuseUserLookup, limits abuseUserLimitWriter) *AbuseDetectorService {
	return &AbuseDetectorService{reader: reader, users: users, limits: limits, interval: time.Minute}
}

// 管理员豁免。一个配错的阈值不该把运营自己锁在门外——而运营恰恰是最可能
// 产生高频调用的人（我们今天就在这么用）。
func TestThrottleSkipsAdmin(t *testing.T) {
	limits := &stubLimitWriter{}
	users := &stubUserLookup{users: map[int64]*User{
		7: {ID: 7, Role: RoleAdmin},
	}}
	d := newDetector(&stubSignalReader{}, users, limits)

	ok := d.throttle(context.Background(), &AbuseSignal{UserID: 7}, 20)
	assert.False(t, ok)
	assert.Zero(t, limits.calls, "管理员被降速了")
}

func TestThrottleAppliesToNormalUser(t *testing.T) {
	limits := &stubLimitWriter{}
	users := &stubUserLookup{users: map[int64]*User{
		9: {ID: 9, Role: RoleUser, RPMLimit: 0},
	}}
	d := newDetector(&stubSignalReader{}, users, limits)

	ok := d.throttle(context.Background(), &AbuseSignal{UserID: 9}, 20)
	require.True(t, ok)
	assert.Equal(t, []int64{9}, limits.userIDs)
	require.NotNil(t, limits.rpm)
	assert.Equal(t, 20, *limits.rpm)
}

// 已经被压到不高于目标值就别再动——重复写会刷掉运营手工调过的值。
func TestThrottleSkipsAlreadyThrottled(t *testing.T) {
	limits := &stubLimitWriter{}
	users := &stubUserLookup{users: map[int64]*User{
		9: {ID: 9, Role: RoleUser, RPMLimit: 10},
	}}
	d := newDetector(&stubSignalReader{}, users, limits)

	assert.False(t, d.throttle(context.Background(), &AbuseSignal{UserID: 9}, 20))
	assert.Zero(t, limits.calls)
}

// 查不到用户时不降速。宁可漏处置，也不对一个身份不明的 id 动手。
func TestThrottleSkipsOnLookupFailure(t *testing.T) {
	limits := &stubLimitWriter{}
	d := newDetector(&stubSignalReader{}, &stubUserLookup{err: errors.New("db down")}, limits)
	assert.False(t, d.throttle(context.Background(), &AbuseSignal{UserID: 9}, 20))
	assert.Zero(t, limits.calls)
}
