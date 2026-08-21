//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierSettingRepoStub 只实现结算参数用到的两个方法，其余方法被调用即 panic，
// 这样「读结算参数时顺手读了别的 key」这类回归会当场炸出来而不是悄悄通过。
type supplierSettingRepoStub struct {
	value    string
	getErr   error
	getCalls int

	setKey   string
	setValue string
	setErr   error
}

func (r *supplierSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.getCalls++
	if key != SettingKeySupplierSettlement {
		panic("unexpected settings key: " + key)
	}
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}

func (r *supplierSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return r.setErr
}

func (r *supplierSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *supplierSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *supplierSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *supplierSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *supplierSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

// newSupplierSettingService 造一个只有 settingRepo 的 SettingService，并把进程级缓存
// 清干净——缓存是包级变量，不清的话测试之间会互相串味。
func newSupplierSettingService(t *testing.T, repo *supplierSettingRepoStub) *SettingService {
	t.Helper()
	invalidateSupplierSettlementCache()
	t.Cleanup(invalidateSupplierSettlementCache)
	return &SettingService{settingRepo: repo}
}

func TestDefaultSupplierSettlementSettingsIsDisabled(t *testing.T) {
	settings := DefaultSupplierSettlementSettings()
	// 默认关是上线策略本身：代码先随版本进生产，管理员显式打开才开始动钱。
	assert.False(t, settings.Enabled)
	assert.False(t, settings.SpendFromWalletFirst)
	assert.Equal(t, SupplierShareRatioDefault, settings.ShareRatio)
	assert.Equal(t, SupplierFreezeHoursDefault, settings.FreezeHours)

	// 冻结窗单独钉死字面量 720，而不是只跟着常量走。
	//
	// 上面那一行拿常量比常量，改小默认值它照样绿——而这个值配小了的表现是：
	// 冻结期过后钱已经付给供给者，此后被拒付平台自吃，且要等到第一笔真实拒付
	// 才看得见。代码侧只 clamp 上限，拦不住「配小了」，所以这里放一道会响的闸：
	// 谁改这个数，谁就得在这条断言前停一下，回答"新的值 ≥ 支付通道拒付窗吗"。
	assert.Equal(t, 720, settings.FreezeHours, "默认冻结窗 30 天；要改先读 docs/two-sided-market.md §6")
}

func TestSupplierSettlementSettingsNormalizeClampsGarbage(t *testing.T) {
	cases := []struct {
		name       string
		in         SupplierSettlementSettings
		wantRatio  float64
		wantFreeze int
	}{
		{"negative ratio", SupplierSettlementSettings{ShareRatio: -0.5, FreezeHours: 24}, 0, 24},
		{"ratio over max", SupplierSettlementSettings{ShareRatio: 3, FreezeHours: 24}, SupplierShareRatioMax, 24},
		{"NaN ratio", SupplierSettlementSettings{ShareRatio: math.NaN(), FreezeHours: 24}, 0, 24},
		{"+Inf ratio", SupplierSettlementSettings{ShareRatio: math.Inf(1), FreezeHours: 24}, 0, 24},
		{"-Inf ratio", SupplierSettlementSettings{ShareRatio: math.Inf(-1), FreezeHours: 24}, 0, 24},
		{"negative freeze", SupplierSettlementSettings{ShareRatio: 0.5, FreezeHours: -1}, 0.5, 0},
		{"freeze over max", SupplierSettlementSettings{ShareRatio: 0.5, FreezeHours: 1 << 20}, 0.5, SupplierFreezeHoursMax},
		{"already legal", SupplierSettlementSettings{ShareRatio: 0.7, FreezeHours: 168}, 0.7, 168},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.normalize()
			assert.Equal(t, tc.wantRatio, got.ShareRatio)
			assert.Equal(t, tc.wantFreeze, got.FreezeHours)
		})
	}
}

func TestSupplierSettlementSettingsNormalizeNilYieldsDefault(t *testing.T) {
	var settings *SupplierSettlementSettings
	got := settings.normalize()
	require.NotNil(t, got)
	assert.False(t, got.Enabled)
}

func TestSupplierSettlementToBillingParams(t *testing.T) {
	t.Run("nil is off", func(t *testing.T) {
		var settings *SupplierSettlementSettings
		assert.Equal(t, UsageBillingSupplierParams{}, settings.ToBillingParams())
	})

	t.Run("disabled zeroes everything", func(t *testing.T) {
		// 关键：即便比例和冻结窗都配好了，开关一关就必须是全零值——
		// 那正是计费里「什么都不做」的那一支。
		settings := &SupplierSettlementSettings{Enabled: false, ShareRatio: 0.7, FreezeHours: 168, SpendFromWalletFirst: true}
		assert.Equal(t, UsageBillingSupplierParams{}, settings.ToBillingParams())
	})

	t.Run("enabled passes values through", func(t *testing.T) {
		settings := &SupplierSettlementSettings{Enabled: true, ShareRatio: 0.7, FreezeHours: 168, SpendFromWalletFirst: true}
		assert.Equal(t, UsageBillingSupplierParams{
			ShareRatio:           0.7,
			FreezeHours:          168,
			SpendFromWalletFirst: true,
		}, settings.ToBillingParams())
	})
}

func TestParseSupplierSettlementSettings(t *testing.T) {
	t.Run("empty is default", func(t *testing.T) {
		assert.False(t, parseSupplierSettlementSettings("").Enabled)
	})

	t.Run("corrupt JSON falls back to disabled", func(t *testing.T) {
		got := parseSupplierSettlementSettings("{not json")
		assert.False(t, got.Enabled)
	})

	t.Run("valid JSON is normalized", func(t *testing.T) {
		got := parseSupplierSettlementSettings(`{"enabled":true,"share_ratio":9,"freeze_hours":-5,"spend_from_wallet_first":true}`)
		assert.True(t, got.Enabled)
		assert.Equal(t, SupplierShareRatioMax, got.ShareRatio)
		assert.Equal(t, 0, got.FreezeHours)
		assert.True(t, got.SpendFromWalletFirst)
	})
}

func TestGetSupplierSettlementSettingsMissingKeyStaysDisabled(t *testing.T) {
	repo := &supplierSettingRepoStub{getErr: ErrSettingNotFound}
	svc := newSupplierSettingService(t, repo)

	got := svc.GetSupplierSettlementSettings(context.Background())
	assert.False(t, got.Enabled)

	// 从未配置过是正常状态，按正常 TTL 缓存：第二次读不该再打库。
	svc.GetSupplierSettlementSettings(context.Background())
	assert.Equal(t, 1, repo.getCalls)
}

func TestGetSupplierSettlementSettingsDBErrorFailsClosed(t *testing.T) {
	repo := &supplierSettingRepoStub{getErr: errors.New("database on fire")}
	svc := newSupplierSettingService(t, repo)

	// fail-closed：读不到配置就不结算。宁可不给钱，也不能按猜出来的比例给。
	got := svc.GetSupplierSettlementSettings(context.Background())
	assert.False(t, got.Enabled)
	assert.Equal(t, UsageBillingSupplierParams{}, got.ToBillingParams())
}

func TestGetSupplierSettlementSettingsCachesAcrossCalls(t *testing.T) {
	repo := &supplierSettingRepoStub{value: `{"enabled":true,"share_ratio":0.6,"freeze_hours":48}`}
	svc := newSupplierSettingService(t, repo)

	for i := 0; i < 5; i++ {
		got := svc.GetSupplierSettlementSettings(context.Background())
		assert.True(t, got.Enabled)
		assert.Equal(t, 0.6, got.ShareRatio)
		assert.Equal(t, 48, got.FreezeHours)
	}
	assert.Equal(t, 1, repo.getCalls, "结算参数在计费热路径上每请求读一次，必须走缓存")
}

func TestGetSupplierSettlementSettingsReturnsCopies(t *testing.T) {
	repo := &supplierSettingRepoStub{value: `{"enabled":true,"share_ratio":0.6,"freeze_hours":48}`}
	svc := newSupplierSettingService(t, repo)

	first := svc.GetSupplierSettlementSettings(context.Background())
	first.ShareRatio = 999 // 调用方污染自己手里那份

	second := svc.GetSupplierSettlementSettings(context.Background())
	assert.Equal(t, 0.6, second.ShareRatio, "缓存里那份必须是共享只读的，出手一律给副本")
}

func TestGetSupplierSettlementSettingsNilServiceIsSafe(t *testing.T) {
	var svc *SettingService
	assert.False(t, svc.GetSupplierSettlementSettings(context.Background()).Enabled)
	assert.False(t, (&SettingService{}).GetSupplierSettlementSettings(context.Background()).Enabled)
}

func TestSetSupplierSettlementSettingsRejectsIllegalWhenEnabled(t *testing.T) {
	cases := []struct {
		name string
		in   SupplierSettlementSettings
	}{
		{"zero ratio", SupplierSettlementSettings{Enabled: true, ShareRatio: 0, FreezeHours: 168}},
		{"negative ratio", SupplierSettlementSettings{Enabled: true, ShareRatio: -0.1, FreezeHours: 168}},
		{"ratio over max", SupplierSettlementSettings{Enabled: true, ShareRatio: 1.5, FreezeHours: 168}},
		{"negative freeze", SupplierSettlementSettings{Enabled: true, ShareRatio: 0.7, FreezeHours: -1}},
		{"freeze over max", SupplierSettlementSettings{Enabled: true, ShareRatio: 0.7, FreezeHours: SupplierFreezeHoursMax + 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierSettingRepoStub{}
			svc := newSupplierSettingService(t, repo)
			// 写路径报错而非静默 clamp：面板上的越界值是笔误，
			// 夹回去反而会让人以为存的是自己填的数。
			require.Error(t, svc.SetSupplierSettlementSettings(context.Background(), &tc.in))
			assert.Empty(t, repo.setKey, "校验失败不应落库")
		})
	}
}

func TestSetSupplierSettlementSettingsAllowsGarbageWhenDisabled(t *testing.T) {
	repo := &supplierSettingRepoStub{}
	svc := newSupplierSettingService(t, repo)

	// 开关关着时比例无意义，不必拦；但落库前仍要 normalize，
	// 免得下次打开开关时读到一个 -0.5。
	require.NoError(t, svc.SetSupplierSettlementSettings(context.Background(),
		&SupplierSettlementSettings{Enabled: false, ShareRatio: -0.5, FreezeHours: -9}))

	var stored SupplierSettlementSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	assert.Equal(t, float64(0), stored.ShareRatio)
	assert.Equal(t, 0, stored.FreezeHours)
}

func TestSetSupplierSettlementSettingsPersistsAndInvalidatesCache(t *testing.T) {
	repo := &supplierSettingRepoStub{getErr: ErrSettingNotFound}
	svc := newSupplierSettingService(t, repo)

	// 先读一次，把「关闭」灌进缓存。
	require.False(t, svc.GetSupplierSettlementSettings(context.Background()).Enabled)

	repo.getErr = nil
	repo.value = ""
	require.NoError(t, svc.SetSupplierSettlementSettings(context.Background(),
		&SupplierSettlementSettings{Enabled: true, ShareRatio: 0.65, FreezeHours: 72, SpendFromWalletFirst: true}))

	assert.Equal(t, SettingKeySupplierSettlement, repo.setKey)
	var stored SupplierSettlementSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	assert.True(t, stored.Enabled)
	assert.Equal(t, 0.65, stored.ShareRatio)
	assert.Equal(t, 72, stored.FreezeHours)
	assert.True(t, stored.SpendFromWalletFirst)

	// 写完必须让缓存立即失效，管理员不该等满一个 TTL 才看到效果。
	repo.value = repo.setValue
	got := svc.GetSupplierSettlementSettings(context.Background())
	assert.True(t, got.Enabled)
	assert.Equal(t, 0.65, got.ShareRatio)
}

func TestSetSupplierSettlementSettingsGuards(t *testing.T) {
	var nilSvc *SettingService
	assert.Error(t, nilSvc.SetSupplierSettlementSettings(context.Background(), DefaultSupplierSettlementSettings()))
	assert.Error(t, (&SettingService{}).SetSupplierSettlementSettings(context.Background(), DefaultSupplierSettlementSettings()))

	repo := &supplierSettingRepoStub{}
	svc := newSupplierSettingService(t, repo)
	assert.Error(t, svc.SetSupplierSettlementSettings(context.Background(), nil))
}
