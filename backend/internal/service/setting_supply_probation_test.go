//go:build unit

// APEXONE-EXT: 双边市场——观察期参数的单元测试。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplyProbationSettingRepoStub 只认观察期这一个 key，读到别的就 panic
// （与另外两组配置的替身同样的把戏，理由见 setting_supply_pool_test.go）。
type supplyProbationSettingRepoStub struct {
	value    string
	getErr   error
	getCalls int

	setKey   string
	setValue string
	setErr   error
}

func (r *supplyProbationSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.getCalls++
	if key != SettingKeySupplyProbation {
		panic("unexpected settings key: " + key)
	}
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}

func (r *supplyProbationSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return r.setErr
}

func (r *supplyProbationSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *supplyProbationSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *supplyProbationSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *supplyProbationSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *supplyProbationSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func newSupplyProbationSettingService(t *testing.T, repo *supplyProbationSettingRepoStub) *SettingService {
	t.Helper()
	invalidateSupplyProbationCache()
	t.Cleanup(invalidateSupplyProbationCache)
	return &SettingService{settingRepo: repo}
}

// 默认必须是「不自动入池」：代码进生产后，没有任何陌生人的号会因为部署了这段代码
// 而自动站到付费流量前面。
func TestDefaultSupplyProbationSettingsDisablesAutoPromotion(t *testing.T) {
	settings := DefaultSupplyProbationSettings()
	assert.False(t, settings.Enabled)
	assert.Positive(t, settings.MinObservationMinutes)
	assert.Positive(t, settings.RequiredSuccesses)
	assert.GreaterOrEqual(t, settings.ProbeIntervalMinutes, SupplyProbationProbeIntervalMinutesMin)
	// 排空窗有正的默认值：0 会让「优雅下线」静默退化成「立即拔出」。
	assert.Positive(t, settings.DrainWindowMinutes)
}

// 读路径也要 clamp：一份被手工改坏的配置不该让观察期任务每秒去戳别人的账号。
func TestGetSupplyProbationSettingsClampsCorruptValues(t *testing.T) {
	repo := &supplyProbationSettingRepoStub{
		value: `{"enabled":true,"min_observation_minutes":-5,"required_successes":0,` +
			`"probe_interval_minutes":1,"drain_window_minutes":-1}`,
	}
	svc := newSupplyProbationSettingService(t, repo)

	settings := svc.GetSupplyProbationSettings(context.Background())
	require.NotNil(t, settings)
	assert.True(t, settings.Enabled)
	assert.Zero(t, settings.MinObservationMinutes, "负的观察窗夹成 0")
	assert.Equal(t, supplyProbationDefaultRequiredSuccesses, settings.RequiredSuccesses)
	assert.Equal(t, SupplyProbationProbeIntervalMinutesMin, settings.ProbeIntervalMinutes,
		"探测间隔的下限守的是供给者的额度，不能被配置绕过")
	assert.Zero(t, settings.DrainWindowMinutes)
}

func TestGetSupplyProbationSettingsClampsUpperBounds(t *testing.T) {
	repo := &supplyProbationSettingRepoStub{
		value: `{"enabled":true,"min_observation_minutes":99999999,"required_successes":9999,` +
			`"probe_interval_minutes":99999,"drain_window_minutes":99999}`,
	}
	svc := newSupplyProbationSettingService(t, repo)

	settings := svc.GetSupplyProbationSettings(context.Background())
	assert.Equal(t, SupplyProbationMinObservationMinutesMax, settings.MinObservationMinutes)
	assert.Equal(t, SupplyProbationRequiredSuccessesMax, settings.RequiredSuccesses)
	assert.Equal(t, SupplyProbationProbeIntervalMinutesMax, settings.ProbeIntervalMinutes)
	assert.Equal(t, SupplyProbationDrainWindowMinutesMax, settings.DrainWindowMinutes)
}

// 读不到配置（库挂了、JSON 坏了）一律不自动入池，但排空窗回落到默认值而不是 0。
func TestGetSupplyProbationSettingsFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		repo *supplyProbationSettingRepoStub
	}{
		{"读库出错", &supplyProbationSettingRepoStub{getErr: errors.New("db down")}},
		{"配置损坏", &supplyProbationSettingRepoStub{value: `{not json`}},
		{"没配过", &supplyProbationSettingRepoStub{getErr: ErrSettingNotFound}},
		{"空值", &supplyProbationSettingRepoStub{value: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSupplyProbationSettingService(t, tc.repo)
			settings := svc.GetSupplyProbationSettings(context.Background())
			require.NotNil(t, settings)
			assert.False(t, settings.Enabled, "读不到配置就不许自动入池")
			assert.Positive(t, settings.DrainWindowMinutes,
				"排空窗不是安全属性，读不到要回落到默认值而不是 0")
		})
	}
}

// nil service / nil settings 都不能炸——这两个 getter 在后台任务里被调用，
// panic 会带走整个任务。
func TestGetSupplyProbationSettingsIsNilSafe(t *testing.T) {
	var svc *SettingService
	settings := svc.GetSupplyProbationSettings(context.Background())
	require.NotNil(t, settings)
	assert.False(t, settings.Enabled)

	var nilSettings *SupplyProbationSettings
	assert.Positive(t, nilSettings.ObservationWindow())
	assert.Positive(t, nilSettings.ProbeInterval())
	assert.Positive(t, nilSettings.DrainWindow())
}

// 写路径夹回区间而不是报错，并且落库的是夹过之后的值——回读给管理员看的必须是
// 库里真正生效的那一份。
func TestSetSupplyProbationSettingsClampsInsteadOfRejecting(t *testing.T) {
	repo := &supplyProbationSettingRepoStub{}
	svc := newSupplyProbationSettingService(t, repo)

	err := svc.SetSupplyProbationSettings(context.Background(), &SupplyProbationSettings{
		Enabled:              true,
		ProbeIntervalMinutes: 1,
		RequiredSuccesses:    -3,
	})
	require.NoError(t, err)
	assert.Equal(t, SettingKeySupplyProbation, repo.setKey)
	assert.Contains(t, repo.setValue, `"probe_interval_minutes":`+
		itoaForTest(SupplyProbationProbeIntervalMinutesMin))
	assert.Contains(t, repo.setValue, `"required_successes":`+
		itoaForTest(supplyProbationDefaultRequiredSuccesses))
}

func TestSetSupplyProbationSettingsRejectsNil(t *testing.T) {
	repo := &supplyProbationSettingRepoStub{}
	svc := newSupplyProbationSettingService(t, repo)
	assert.Error(t, svc.SetSupplyProbationSettings(context.Background(), nil))
	assert.Empty(t, repo.setKey)
}

// 写完必须让缓存失效，否则管理员改完之后最多 60 秒里读到的还是旧值——
// 而他会在那 60 秒里以为自己改的没生效，再改一次。
func TestSetSupplyProbationSettingsInvalidatesCache(t *testing.T) {
	repo := &supplyProbationSettingRepoStub{value: `{"enabled":false}`}
	svc := newSupplyProbationSettingService(t, repo)

	require.False(t, svc.GetSupplyProbationSettings(context.Background()).Enabled)
	before := repo.getCalls

	repo.value = `{"enabled":true,"min_observation_minutes":30,"required_successes":1,"probe_interval_minutes":10}`
	require.NoError(t, svc.SetSupplyProbationSettings(context.Background(), &SupplyProbationSettings{
		Enabled: true, MinObservationMinutes: 30, RequiredSuccesses: 1, ProbeIntervalMinutes: 10,
	}))

	assert.True(t, svc.GetSupplyProbationSettings(context.Background()).Enabled)
	assert.Greater(t, repo.getCalls, before, "写完要重新读库，不能吃旧缓存")
}

func TestSupplyProbationDurationHelpers(t *testing.T) {
	settings := &SupplyProbationSettings{
		MinObservationMinutes: 90,
		ProbeIntervalMinutes:  15,
		DrainWindowMinutes:    7,
	}
	assert.Equal(t, 90*time.Minute, settings.ObservationWindow())
	assert.Equal(t, 15*time.Minute, settings.ProbeInterval())
	assert.Equal(t, 7*time.Minute, settings.DrainWindow())
}

func itoaForTest(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}
