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

// 这一组复用 setting_supply_pool_test.go 里的 supplyPoolSettingRepoStub —— 它已经认得
// 四个 key，接入上限走 onboardingValue 那一路。

func TestDefaultSupplyOnboardingSettings(t *testing.T) {
	settings := DefaultSupplyOnboardingSettings()

	// 每人有闸、每 IP 不设闸：这个不对称是这套配置的核心决定（见文件头）。
	// 一个「默认每 IP 3 个」的世界里，校园网后面的人会在没有任何提示的情况下挂不上号。
	assert.Equal(t, 5, settings.MaxAccountsPerUser)
	assert.Zero(t, settings.MaxAccountsPerIP)
	assert.True(t, settings.userCapEnabled())
	assert.False(t, settings.ipCapEnabled())
}

func TestSupplyOnboardingSettingsNormalize(t *testing.T) {
	cases := []struct {
		name     string
		in       SupplyOnboardingSettings
		wantUser int
		wantIP   int
	}{
		{"正常值原样保留", SupplyOnboardingSettings{MaxAccountsPerUser: 3, MaxAccountsPerIP: 50}, 3, 50},
		{"0 是合法的「不限」", SupplyOnboardingSettings{}, 0, 0},
		// 负数夹成 0（不限）而不是 1：把手工改坏的 JSON 读成「每人最多 1 个」
		// 等于静默地把所有人挡在门外。
		{"负数夹成不限", SupplyOnboardingSettings{MaxAccountsPerUser: -1, MaxAccountsPerIP: -999}, 0, 0},
		{"超上限夹回上限", SupplyOnboardingSettings{
			MaxAccountsPerUser: SupplyOnboardingMaxAccountsPerUserMax + 1,
			MaxAccountsPerIP:   SupplyOnboardingMaxAccountsPerIPMax + 1,
		}, SupplyOnboardingMaxAccountsPerUserMax, SupplyOnboardingMaxAccountsPerIPMax},
		{"恰好等于上限不动", SupplyOnboardingSettings{
			MaxAccountsPerUser: SupplyOnboardingMaxAccountsPerUserMax,
			MaxAccountsPerIP:   SupplyOnboardingMaxAccountsPerIPMax,
		}, SupplyOnboardingMaxAccountsPerUserMax, SupplyOnboardingMaxAccountsPerIPMax},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			got.normalize()
			assert.Equal(t, tc.wantUser, got.MaxAccountsPerUser)
			assert.Equal(t, tc.wantIP, got.MaxAccountsPerIP)
		})
	}

	// nil 上调用不能炸：读路径拿到的可能就是一个 nil。
	var nilSettings *SupplyOnboardingSettings
	assert.NotPanics(t, func() { nilSettings.normalize() })
}

func TestSupplyOnboardingCapReached(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyOnboardingSettings
		current  int
		wantUser bool
		wantIP   bool
	}{
		// 0 = 不限的那两条分支。`current >= 0` 恒真，所以少一个「闸开着吗」的前置判断，
		// 现象就是所有人都挂不上号——这两行是这个测试文件里最重要的两行。
		{"两道闸都关着", &SupplyOnboardingSettings{}, 9999, false, false},
		{"nil 配置等同全关", nil, 9999, false, false},
		{"没到上限", &SupplyOnboardingSettings{MaxAccountsPerUser: 5, MaxAccountsPerIP: 20}, 4, false, false},
		// 边界取 `>=` 而不是 `>`：上限 5 的意思是「最多有 5 个」，已经有 5 个的人
		// 再挂一个就是第 6 个。
		{"恰好到上限就拦", &SupplyOnboardingSettings{MaxAccountsPerUser: 5, MaxAccountsPerIP: 5}, 5, true, true},
		{"超过上限当然拦", &SupplyOnboardingSettings{MaxAccountsPerUser: 5, MaxAccountsPerIP: 5}, 6, true, true},
		// 两道闸互相独立：一道关着不影响另一道。
		{"只开每人这道", &SupplyOnboardingSettings{MaxAccountsPerUser: 2}, 3, true, false},
		{"只开每 IP 这道", &SupplyOnboardingSettings{MaxAccountsPerIP: 2}, 3, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantUser, tc.settings.userCapReached(tc.current))
			assert.Equal(t, tc.wantIP, tc.settings.ipCapReached(tc.current))
		})
	}
}

func TestGetSupplyOnboardingSettingsParsesStoredJSON(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{
		onboardingValue: `{"max_accounts_per_user":3,"max_accounts_per_ip":40}`,
	}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyOnboardingSettings(context.Background())

	require.NotNil(t, settings)
	assert.Equal(t, 3, settings.MaxAccountsPerUser)
	assert.Equal(t, 40, settings.MaxAccountsPerIP)
}

func TestGetSupplyOnboardingSettingsClampsStoredJSON(t *testing.T) {
	// 库里那份也要过一遍 normalize：一份写坏的行（或者更早版本写进去的负数）
	// 不能因为「它已经在库里了」就绕过夹取。
	repo := &supplyPoolSettingRepoStub{
		onboardingValue: `{"max_accounts_per_user":-1,"max_accounts_per_ip":999999}`,
	}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyOnboardingSettings(context.Background())

	assert.Zero(t, settings.MaxAccountsPerUser)
	assert.Equal(t, SupplyOnboardingMaxAccountsPerIPMax, settings.MaxAccountsPerIP)
}

func TestGetSupplyOnboardingSettingsFallsBackToDefaultsOnCorruptJSON(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{onboardingValue: `{"max_accounts_per_user":`}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyOnboardingSettings(context.Background())

	// 坏配置不该被读成一道敞开的门：每人上限退回默认的 5，而不是 0（不限）。
	require.NotNil(t, settings)
	assert.Equal(t, 5, settings.MaxAccountsPerUser)
	assert.Zero(t, settings.MaxAccountsPerIP)
}

func TestGetSupplyOnboardingSettingsFallsBackWhenUnconfigured(t *testing.T) {
	// 绝大多数部署永远不会显式配这个 key，「没配」是最常见的状态。
	// 它必须落在有闸的那一侧（每人 5 个），而不是「没配 = 不限」。
	repo := &supplyPoolSettingRepoStub{onboardingValue: ""}
	svc := newSupplyPoolSettingService(t, repo)

	settings := svc.GetSupplyOnboardingSettings(context.Background())
	assert.Equal(t, 5, settings.MaxAccountsPerUser)
	assert.Zero(t, settings.MaxAccountsPerIP)
}

func TestGetSupplyOnboardingSettingsHitsDBOnce(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{
		onboardingValue: `{"max_accounts_per_user":3,"max_accounts_per_ip":40}`,
	}
	svc := newSupplyPoolSettingService(t, repo)

	for i := 0; i < 10; i++ {
		require.Equal(t, 3, svc.GetSupplyOnboardingSettings(context.Background()).MaxAccountsPerUser)
	}
	// 接入这条路径每次挂号都要读一次配置，缓存不生效等于每次接入多一次 DB 往返。
	assert.Equal(t, 1, repo.getCalls)
}

func TestGetSupplyOnboardingSettingsReturnsCopy(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{
		onboardingValue: `{"max_accounts_per_user":3,"max_accounts_per_ip":40}`,
	}
	svc := newSupplyPoolSettingService(t, repo)

	first := svc.GetSupplyOnboardingSettings(context.Background())
	// 调用方把它改成「不限」——如果返回的是缓存里那一份，这道闸就对全进程永久失效了。
	first.MaxAccountsPerUser = 0
	first.MaxAccountsPerIP = 0

	second := svc.GetSupplyOnboardingSettings(context.Background())
	assert.Equal(t, 3, second.MaxAccountsPerUser)
	assert.Equal(t, 40, second.MaxAccountsPerIP)
}

func TestGetSupplyOnboardingSettingsIsNilSafe(t *testing.T) {
	var nilSvc *SettingService
	assert.Equal(t, 5, nilSvc.GetSupplyOnboardingSettings(context.Background()).MaxAccountsPerUser)
	assert.Equal(t, 5, (&SettingService{}).GetSupplyOnboardingSettings(context.Background()).MaxAccountsPerUser)
}

func TestSetSupplyOnboardingSettingsPersistsClampedAndInvalidatesCache(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{}
	svc := newSupplyPoolSettingService(t, repo)

	require.Equal(t, 5, svc.GetSupplyOnboardingSettings(context.Background()).MaxAccountsPerUser)

	require.NoError(t, svc.SetSupplyOnboardingSettings(context.Background(), &SupplyOnboardingSettings{
		MaxAccountsPerUser: SupplyOnboardingMaxAccountsPerUserMax + 7,
		MaxAccountsPerIP:   -3,
	}))

	var stored SupplyOnboardingSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	assert.Equal(t, SettingKeySupplyOnboarding, repo.setKey)
	// 落库的是夹过的值，不是管理员填的那个——回读契约靠的就是这一点。
	assert.Equal(t, SupplyOnboardingMaxAccountsPerUserMax, stored.MaxAccountsPerUser)
	assert.Zero(t, stored.MaxAccountsPerIP)

	// 写完立刻读必须看到新值，否则管理员会以为没生效而重复操作。
	repo.onboardingValue = repo.setValue
	assert.Equal(t, SupplyOnboardingMaxAccountsPerUserMax,
		svc.GetSupplyOnboardingSettings(context.Background()).MaxAccountsPerUser)
	assert.Equal(t, 2, repo.getCalls)
}

func TestSetSupplyOnboardingSettingsPropagatesRepoError(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{setErr: errors.New("write failed")}
	svc := newSupplyPoolSettingService(t, repo)

	require.Error(t, svc.SetSupplyOnboardingSettings(context.Background(), &SupplyOnboardingSettings{
		MaxAccountsPerUser: 3,
	}))
}

func TestSetSupplyOnboardingSettingsIsNilSafe(t *testing.T) {
	var nilSvc *SettingService
	assert.Error(t, nilSvc.SetSupplyOnboardingSettings(context.Background(), &SupplyOnboardingSettings{}))
	assert.Error(t, (&SettingService{}).SetSupplyOnboardingSettings(context.Background(), &SupplyOnboardingSettings{}))
	// nil 配置也必须报错而不是被当成「全部清零」——那等于一次点击就把两道闸都关了。
	repo := &supplyPoolSettingRepoStub{}
	svc := newSupplyPoolSettingService(t, repo)
	assert.Error(t, svc.SetSupplyOnboardingSettings(context.Background(), nil))
	assert.Empty(t, repo.setKey)
}
