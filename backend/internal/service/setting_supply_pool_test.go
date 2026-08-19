//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplyPoolSettingRepoStub 与 supplierSettingRepoStub 同样的把戏：读到不认识的 key 就 panic，
// 好让「顺手多读了一个 key」这类回归当场暴露而不是悄悄通过。
//
// 认两个 key：池配置和观察期配置。自助接入服务两个都读（一个决定挂哪个分组，
// 一个决定排空窗和 EligibleAt），所以这个替身不能只认一个。
type supplyPoolSettingRepoStub struct {
	value string
	// probationValue 观察期配置的原始 JSON。空串 = 走默认（不自动入池、10 分钟排空窗）。
	probationValue string
	getErr         error
	getCalls       int

	setKey   string
	setValue string
	setErr   error
}

func (r *supplyPoolSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.getCalls++
	switch key {
	case SettingKeySupplyPool:
		if r.getErr != nil {
			return "", r.getErr
		}
		return r.value, nil
	case SettingKeySupplyProbation:
		return r.probationValue, nil
	default:
		panic("unexpected settings key: " + key)
	}
}

func (r *supplyPoolSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return r.setErr
}

func (r *supplyPoolSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *supplyPoolSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *supplyPoolSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *supplyPoolSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *supplyPoolSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

// newSupplyPoolSettingService 造一个只有 settingRepo 的 SettingService。缓存是包级变量，
// 前后都清一遍，否则测试之间互相串味。
func newSupplyPoolSettingService(t *testing.T, repo *supplyPoolSettingRepoStub) *SettingService {
	t.Helper()
	invalidateSupplyPoolCache()
	invalidateSupplyProbationCache()
	t.Cleanup(invalidateSupplyPoolCache)
	t.Cleanup(invalidateSupplyProbationCache)
	return &SettingService{settingRepo: repo}
}

func TestDefaultSupplyPoolSettingsIsDisabled(t *testing.T) {
	settings := DefaultSupplyPoolSettings()
	// 默认关 = 代码进生产后调度行为与上游一字不差，直到管理员显式配置。
	assert.False(t, settings.Enabled)
	assert.Zero(t, settings.SupplyGroupID)
	assert.Zero(t, settings.OverflowGroupID)
}

func TestSupplyPoolOverflowTargetFor(t *testing.T) {
	enabled := func(supply, overflow int64) *SupplyPoolSettings {
		return &SupplyPoolSettings{Enabled: true, SupplyGroupID: supply, OverflowGroupID: overflow}
	}

	cases := []struct {
		name     string
		settings *SupplyPoolSettings
		resolved int64
		wantID   int64
		wantOK   bool
	}{
		{"命中供给池", enabled(10, 11), 10, 11, true},
		{"nil 配置", nil, 10, 0, false},
		{"开关关着", &SupplyPoolSettings{SupplyGroupID: 10, OverflowGroupID: 11}, 10, 0, false},
		{"供给池 id 未配", enabled(0, 11), 10, 0, false},
		{"兜底池 id 未配", enabled(10, 0), 10, 0, false},
		{"负数 id", enabled(-1, -2), -1, 0, false},
		{"自己溢出到自己", enabled(10, 10), 10, 0, false},
		// 门开得窄的那条规则：不是供给池的分组耗尽了，就是它自己的事。
		{"别的分组耗尽不溢出", enabled(10, 11), 12, 0, false},
		{"兜底池自己耗尽不再溢出", enabled(10, 11), 11, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := tc.settings.overflowTargetFor(tc.resolved)
			assert.Equal(t, tc.wantOK, gotOK)
			assert.Equal(t, tc.wantID, gotID)
		})
	}
}

func TestGetSupplyPoolSettingsParsesStoredJSON(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{value: `{"enabled":true,"supply_group_id":10,"overflow_group_id":11}`}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyPoolSettings(context.Background())

	require.NotNil(t, settings)
	assert.True(t, settings.Enabled)
	assert.Equal(t, int64(10), settings.SupplyGroupID)
	assert.Equal(t, int64(11), settings.OverflowGroupID)
}

func TestGetSupplyPoolSettingsFailsClosedOnDBError(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{getErr: errors.New("settings table unreachable")}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyPoolSettings(context.Background())

	// 读不到就不溢出：溢出等于平台按自营成本供货却按供给池价收费，这个决定不能来自一次猜测。
	require.NotNil(t, settings)
	assert.False(t, settings.Enabled)
}

func TestGetSupplyPoolSettingsFailsClosedOnCorruptJSON(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{value: `{"enabled":true,`}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyPoolSettings(context.Background())

	require.NotNil(t, settings)
	assert.False(t, settings.Enabled)
}

func TestGetSupplyPoolSettingsCachesMissingKeyAtNormalTTL(t *testing.T) {
	// 绝大多数部署永远不会配这个 key。若把「没配」按错误短 TTL 缓存，
	// 每 5 秒一次的 DB 查询会白白挂在调度失败路径上。
	repo := &supplyPoolSettingRepoStub{getErr: ErrSettingNotFound}
	svc := newSupplyPoolSettingService(t, repo)

	for i := 0; i < 5; i++ {
		assert.False(t, svc.GetSupplyPoolSettings(context.Background()).Enabled)
	}
	assert.Equal(t, 1, repo.getCalls)
}

func TestGetSupplyPoolSettingsHitsDBOnce(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{value: `{"enabled":true,"supply_group_id":10,"overflow_group_id":11}`}
	svc := newSupplyPoolSettingService(t, repo)

	for i := 0; i < 10; i++ {
		require.True(t, svc.GetSupplyPoolSettings(context.Background()).Enabled)
	}
	assert.Equal(t, 1, repo.getCalls)
}

func TestGetSupplyPoolSettingsReturnsCopy(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{value: `{"enabled":true,"supply_group_id":10,"overflow_group_id":11}`}
	svc := newSupplyPoolSettingService(t, repo)

	first := svc.GetSupplyPoolSettings(context.Background())
	first.OverflowGroupID = 999
	first.Enabled = false

	second := svc.GetSupplyPoolSettings(context.Background())
	assert.True(t, second.Enabled)
	assert.Equal(t, int64(11), second.OverflowGroupID)
}

func TestGetSupplyPoolSettingsIsNilSafe(t *testing.T) {
	var nilSvc *SettingService
	assert.False(t, nilSvc.GetSupplyPoolSettings(context.Background()).Enabled)
	assert.False(t, (&SettingService{}).GetSupplyPoolSettings(context.Background()).Enabled)
}

func TestSetSupplyPoolSettingsRejectsBadConfigWhenEnabled(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyPoolSettings
	}{
		{"nil", nil},
		{"供给池 id 缺失", &SupplyPoolSettings{Enabled: true, OverflowGroupID: 11}},
		{"兜底池 id 缺失", &SupplyPoolSettings{Enabled: true, SupplyGroupID: 10}},
		{"两者相同", &SupplyPoolSettings{Enabled: true, SupplyGroupID: 10, OverflowGroupID: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyPoolSettingRepoStub{}
			svc := newSupplyPoolSettingService(t, repo)

			require.Error(t, svc.SetSupplyPoolSettings(context.Background(), tc.settings))
			// 写路径报错而不是夹一个「差不多」的值进去：面板上填错的 id 是笔误，不是意图。
			assert.Empty(t, repo.setKey)
		})
	}
}

func TestSetSupplyPoolSettingsAllowsIncompleteConfigWhenDisabled(t *testing.T) {
	// 分两步配置（先填 id、再打开开关）是管理员的正常操作，关着时不该拦。
	repo := &supplyPoolSettingRepoStub{}
	svc := newSupplyPoolSettingService(t, repo)

	require.NoError(t, svc.SetSupplyPoolSettings(context.Background(), &SupplyPoolSettings{SupplyGroupID: 10}))
	assert.Equal(t, SettingKeySupplyPool, repo.setKey)
}

func TestSetSupplyPoolSettingsPersistsAndInvalidatesCache(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{getErr: ErrSettingNotFound}
	svc := newSupplyPoolSettingService(t, repo)

	require.False(t, svc.GetSupplyPoolSettings(context.Background()).Enabled)

	require.NoError(t, svc.SetSupplyPoolSettings(context.Background(), &SupplyPoolSettings{
		Enabled: true, SupplyGroupID: 10, OverflowGroupID: 11,
	}))

	var stored SupplyPoolSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	assert.Equal(t, SupplyPoolSettings{Enabled: true, SupplyGroupID: 10, OverflowGroupID: 11}, stored)

	// 写完立刻读必须看到新值，否则管理员会以为配置没生效而重复操作。
	repo.getErr = nil
	repo.value = repo.setValue
	assert.True(t, svc.GetSupplyPoolSettings(context.Background()).Enabled)
	assert.Equal(t, 2, repo.getCalls)
}

func TestSetSupplyPoolSettingsPropagatesRepoError(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{setErr: errors.New("write failed")}
	svc := newSupplyPoolSettingService(t, repo)

	require.Error(t, svc.SetSupplyPoolSettings(context.Background(), &SupplyPoolSettings{
		Enabled: true, SupplyGroupID: 10, OverflowGroupID: 11,
	}))
}

func TestSetSupplyPoolSettingsIsNilSafe(t *testing.T) {
	var nilSvc *SettingService
	assert.Error(t, nilSvc.SetSupplyPoolSettings(context.Background(), &SupplyPoolSettings{}))
	assert.Error(t, (&SettingService{}).SetSupplyPoolSettings(context.Background(), &SupplyPoolSettings{}))
}
