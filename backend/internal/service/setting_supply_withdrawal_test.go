//go:build unit

// APEXONE-EXT: 双边市场——提现参数的单元测试。
//
// 这一组配置决定钱能不能离开系统，所以测的重点不是「字段存得下来吗」，而是
// **每一种配错的姿势最后落在哪一边**：读路径出问题必须落在「提不了」，
// 写路径越界必须落在「不给保存」而不是悄悄夹回去。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplyWithdrawalSettingRepoStub 只认提现这一个 key，读到别的当场 panic——
// 「顺手多读了一个 key」这种回归会当场暴露，而不是悄悄多打一次库。
type supplyWithdrawalSettingRepoStub struct {
	value    string
	getErr   error
	getCalls int

	setKey   string
	setValue string
	setErr   error
}

func (r *supplyWithdrawalSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeySupplyWithdrawal {
		panic("unexpected settings key: " + key)
	}
	r.getCalls++
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}

func (r *supplyWithdrawalSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return r.setErr
}

func (r *supplyWithdrawalSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *supplyWithdrawalSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *supplyWithdrawalSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *supplyWithdrawalSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *supplyWithdrawalSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

// newWithdrawalSettingService 造一个只有 settingRepo 的 SettingService。
// 缓存是包级变量，前后都清一遍，否则测试之间互相串味。
func newWithdrawalSettingService(t *testing.T, repo *supplyWithdrawalSettingRepoStub) *SettingService {
	t.Helper()
	invalidateSupplyWithdrawalCache()
	t.Cleanup(invalidateSupplyWithdrawalCache)
	return &SettingService{settingRepo: repo}
}

// ============================================================================
// 默认值与 Available
// ============================================================================

func TestDefaultSupplyWithdrawalIsClosed(t *testing.T) {
	settings := DefaultSupplyWithdrawalSettings()
	assert.False(t, settings.Enabled)
	assert.False(t, settings.Available())
	assert.Empty(t, settings.Channels)
	// 默认 1 张而不是 0：0 会让「默认配置」等于「谁也提不了」，
	// 那是一个只有读代码才能发现的关门方式。
	assert.Equal(t, SupplyWithdrawalMaxPendingDefault, settings.MaxPending)
}

// 开关和渠道各自都不足以让提现可用。这是 Available 存在的全部理由——
// 「开了但没配渠道」是最容易发生、也最难在界面上看出来的那种配错。
func TestSupplyWithdrawalAvailableNeedsBothSwitchAndChannel(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyWithdrawalSettings
		want     bool
	}{
		{"nil", nil, false},
		{"关着但配了渠道", &SupplyWithdrawalSettings{Channels: []string{"USDT"}}, false},
		{"开着但没渠道", &SupplyWithdrawalSettings{Enabled: true}, false},
		{"都齐了", &SupplyWithdrawalSettings{Enabled: true, Channels: []string{"USDT"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.settings.Available())
		})
	}
}

// 渠道名区分大小写：它是运营拿去打款的标签，"USDT" 与 "usdt" 在台账上是两个词。
func TestSupplyWithdrawalHasChannelIsCaseSensitiveButTrims(t *testing.T) {
	settings := &SupplyWithdrawalSettings{Channels: []string{"USDT-TRC20", "支付宝"}}
	assert.True(t, settings.HasChannel("  USDT-TRC20 "), "两边的空白应当被忽略")
	assert.True(t, settings.HasChannel("支付宝"))
	assert.False(t, settings.HasChannel("usdt-trc20"))
	assert.False(t, settings.HasChannel(""))
}

// ============================================================================
// 读路径：读不到、读坏了，一律落在「提不了」
// ============================================================================

func TestGetSupplyWithdrawalFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		repo *supplyWithdrawalSettingRepoStub
	}{
		{"没配置", &supplyWithdrawalSettingRepoStub{}},
		{"key 不存在", &supplyWithdrawalSettingRepoStub{getErr: ErrSettingNotFound}},
		{"读库炸了", &supplyWithdrawalSettingRepoStub{getErr: errors.New("db down")}},
		{"JSON 坏了", &supplyWithdrawalSettingRepoStub{value: `{"enabled":`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newWithdrawalSettingService(t, tc.repo)
			settings := svc.GetSupplyWithdrawalSettings(context.Background())
			require.NotNil(t, settings)
			assert.False(t, settings.Available(), "读不出一份可用的配置时必须当作提现关闭")
		})
	}
}

// 读路径上被手改成负数的起提额必须被夹回 0，而不是原样返回——
// 一个 -1 的起提额意味着任何金额都过得了那一关。
func TestGetSupplyWithdrawalClampsHostileNumbers(t *testing.T) {
	raw := `{"enabled":true,"min_amount":-1,"max_pending":9999,"channels":["USDT"]}`
	svc := newWithdrawalSettingService(t, &supplyWithdrawalSettingRepoStub{value: raw})

	settings := svc.GetSupplyWithdrawalSettings(context.Background())
	assert.Equal(t, float64(SupplyWithdrawalMinAmountFloor), settings.MinAmount)
	assert.Equal(t, SupplyWithdrawalMaxPendingCap, settings.MaxPending)
	assert.True(t, settings.Available(), "数值越界不该把整份配置作废")
}

// 渠道列表在读路径上被清洗：去空白、丢空串、去重、截断数量。
func TestGetSupplyWithdrawalSanitizesChannels(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"enabled":  true,
		"channels": []string{" USDT ", "USDT", "", "   ", strings.Repeat("x", SupplyWithdrawalChannelMaxLen+1), "支付宝"},
	})
	require.NoError(t, err)
	svc := newWithdrawalSettingService(t, &supplyWithdrawalSettingRepoStub{value: string(raw)})

	settings := svc.GetSupplyWithdrawalSettings(context.Background())
	assert.Equal(t, []string{"USDT", "支付宝"}, settings.Channels)
}

// 缓存返回的必须是副本，且 Channels 也得是新的底层数组：
// 浅拷贝时调用方对切片元素赋一次值就改了全进程的收款渠道。
func TestGetSupplyWithdrawalReturnsDeepCopy(t *testing.T) {
	repo := &supplyWithdrawalSettingRepoStub{value: `{"enabled":true,"channels":["USDT"]}`}
	svc := newWithdrawalSettingService(t, repo)

	first := svc.GetSupplyWithdrawalSettings(context.Background())
	require.Len(t, first.Channels, 1)
	first.Channels[0] = "被改过的"
	first.Enabled = false

	second := svc.GetSupplyWithdrawalSettings(context.Background())
	assert.Equal(t, []string{"USDT"}, second.Channels)
	assert.True(t, second.Enabled)
	assert.Equal(t, 1, repo.getCalls, "第二次应当走缓存")
}

// ============================================================================
// 写路径：越界拒绝保存，不夹回去
// ============================================================================

func TestSetSupplyWithdrawalRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyWithdrawalSettings
	}{
		{"nil", nil},
		{"起提额为负", &SupplyWithdrawalSettings{MinAmount: -1, MaxPending: 1}},
		{"起提额超上限", &SupplyWithdrawalSettings{MinAmount: SupplyWithdrawalMinAmountMax + 1, MaxPending: 1}},
		{"未决单上限为 0", &SupplyWithdrawalSettings{MaxPending: 0}},
		{"未决单上限超顶", &SupplyWithdrawalSettings{MaxPending: SupplyWithdrawalMaxPendingCap + 1}},
		{"渠道过多", &SupplyWithdrawalSettings{
			MaxPending: 1,
			Channels:   make([]string, SupplyWithdrawalChannelsMax+1),
		}},
		{"单个渠道名过长", &SupplyWithdrawalSettings{
			MaxPending: 1,
			Channels:   []string{strings.Repeat("x", SupplyWithdrawalChannelMaxLen+1)},
		}},
		{"告知过长", &SupplyWithdrawalSettings{
			MaxPending: 1,
			Notice:     strings.Repeat("x", SupplyWithdrawalNoticeMaxLen+1),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyWithdrawalSettingRepoStub{}
			err := newWithdrawalSettingService(t, repo).SetSupplyWithdrawalSettings(context.Background(), tc.settings)
			require.Error(t, err)
			assert.Empty(t, repo.setKey, "校验没过就不该写库")
		})
	}
}

// 开着开关却一个渠道都没有是个只在供给者点下申请时才暴露的错，必须在保存时就挡住。
func TestSetSupplyWithdrawalRejectsEnabledWithoutChannel(t *testing.T) {
	cases := []struct {
		name     string
		channels []string
	}{
		{"一个都没填", nil},
		{"填的全是空白", []string{"  ", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyWithdrawalSettingRepoStub{}
			err := newWithdrawalSettingService(t, repo).SetSupplyWithdrawalSettings(
				context.Background(),
				&SupplyWithdrawalSettings{Enabled: true, MaxPending: 1, Channels: tc.channels},
			)
			require.Error(t, err)
			assert.Empty(t, repo.setKey)
		})
	}
}

// 关着的时候没渠道是允许的：先把文案和起提额配好、最后再开开关，是一个正常的顺序。
func TestSetSupplyWithdrawalAllowsDisabledWithoutChannel(t *testing.T) {
	repo := &supplyWithdrawalSettingRepoStub{}
	err := newWithdrawalSettingService(t, repo).SetSupplyWithdrawalSettings(
		context.Background(),
		&SupplyWithdrawalSettings{Enabled: false, MaxPending: 1},
	)
	require.NoError(t, err)
	assert.Equal(t, SettingKeySupplyWithdrawal, repo.setKey)
}

func TestSetSupplyWithdrawalPersistsAndInvalidatesCache(t *testing.T) {
	repo := &supplyWithdrawalSettingRepoStub{value: `{"enabled":false}`}
	svc := newWithdrawalSettingService(t, repo)

	// 先读一次把缓存烘热，这样「写完之后读到的是旧值」会被抓住。
	require.False(t, svc.GetSupplyWithdrawalSettings(context.Background()).Enabled)

	require.NoError(t, svc.SetSupplyWithdrawalSettings(context.Background(), &SupplyWithdrawalSettings{
		Enabled:    true,
		MinAmount:  50,
		MaxPending: 2,
		Channels:   []string{" USDT ", "USDT", "支付宝"},
		Notice:     "  工作日 3 天内到账  ",
	}))

	var stored SupplyWithdrawalSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	assert.True(t, stored.Enabled)
	assert.Equal(t, 50.0, stored.MinAmount)
	assert.Equal(t, 2, stored.MaxPending)
	assert.Equal(t, []string{"USDT", "支付宝"}, stored.Channels, "保存前应当去空白与去重")
	assert.Equal(t, "工作日 3 天内到账", stored.Notice)

	repo.value = repo.setValue
	reread := svc.GetSupplyWithdrawalSettings(context.Background())
	assert.True(t, reread.Available(), "写完必须让缓存失效，否则面板显示的是旧配置")
}
