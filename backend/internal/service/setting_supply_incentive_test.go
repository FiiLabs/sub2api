//go:build unit

// APEXONE-EXT: 双边市场——挂号奖励规则的单元测试。
//
// 这一组要钉死的性质与另外六组配置都不同：那些配置错了是「行为不对」，
// 这一组配置错了是**钱发错**。所以测试的重心全在两个方向相反的容错上：
// 写路径必须拒绝（不能夹回），读路径必须夹回/丢弃（不能报错，也不能原样照发）。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type supplyIncentiveSettingRepoStub struct {
	value  string
	getErr error

	setKey   string
	setValue string
	setErr   error
}

func (r *supplyIncentiveSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeySupplyIncentive {
		panic("unexpected settings key: " + key)
	}
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}

func (r *supplyIncentiveSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return r.setErr
}

func (r *supplyIncentiveSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *supplyIncentiveSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *supplyIncentiveSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *supplyIncentiveSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *supplyIncentiveSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func newSupplyIncentiveSettingService(t *testing.T, repo *supplyIncentiveSettingRepoStub) *SettingService {
	t.Helper()
	invalidateSupplyIncentiveCache()
	t.Cleanup(invalidateSupplyIncentiveCache)
	return &SettingService{settingRepo: repo}
}

// todayIncentiveStartAt 是「今天」的起算日。写路径拒绝回填，所以夹具里不能钉死一个
// 字面日期——那样这些用例会在写下它的第二天开始全红。
func todayIncentiveStartAt() string {
	return time.Now().UTC().Format(SupplyIncentiveDayFormat)
}

func validIncentiveSettings() *SupplyIncentiveSettings {
	return &SupplyIncentiveSettings{
		Enabled: true,
		Programs: []SupplyIncentiveProgram{{
			Slug:     "bind26q4",
			Platform: "",
			StartAt:  todayIncentiveStartAt(),
			Tiers: []SupplyIncentiveTier{
				{MinActiveDays: 10, AmountUSD: 5, Slots: 60},
				{MinActiveDays: 30, AmountUSD: 20, Slots: 5},
				{MinActiveDays: 60, AmountUSD: 25, Slots: 5},
				{MinActiveDays: 90, AmountUSD: 50, Slots: 5},
			},
		}},
	}
}

// 默认必须是「没有活动」：部署这段代码本身不该让任何一分钱发出去。
func TestDefaultSupplyIncentiveSettingsGrantsNothing(t *testing.T) {
	settings := DefaultSupplyIncentiveSettings()
	assert.False(t, settings.Enabled)
	assert.Empty(t, settings.Programs)
	assert.False(t, settings.Active())
}

// Enabled 但一个活动都没有 = 不发钱。这两个条件是「且」，不是「或」。
func TestSupplyIncentiveActiveRequiresBothEnabledAndPrograms(t *testing.T) {
	assert.False(t, (&SupplyIncentiveSettings{Enabled: true}).Active())
	assert.False(t, (&SupplyIncentiveSettings{Programs: validIncentiveSettings().Programs}).Active())
	assert.True(t, validIncentiveSettings().Active())
}

// 预算是算出来的，不是配出来的——这条断言同时钉死了对外宣称的那个数。
func TestSupplyIncentiveBudgetCapMatchesPlan(t *testing.T) {
	capUSD, bounded := validIncentiveSettings().BudgetCapUSD()
	require.True(t, bounded)
	// 60×5 + 5×20 + 5×25 + 5×50 = 300 + 100 + 125 + 250
	assert.InDelta(t, 775.0, capUSD, 1e-9)
}

// 任何一档不限名额 = 整体没有上界。此时给个数字比说「无上限」更危险：
// 面板会把它当成预算显示，而实际敞口没有尽头。
func TestSupplyIncentiveBudgetCapUnboundedWhenAnyTierUnlimited(t *testing.T) {
	settings := validIncentiveSettings()
	settings.Programs[0].Tiers[0].Slots = 0

	capUSD, bounded := settings.BudgetCapUSD()
	assert.False(t, bounded)
	assert.Zero(t, capUSD)
}

// ---------------------------------------------------------------------------
// 写路径：越界一律拒绝，不夹回。
// ---------------------------------------------------------------------------

// 这是本组最重要的一条：与观察期那组（夹回再回读）方向相反。
// 想填 $50 手滑填成 $5000，夹回上限运营不会察觉，而钱已经在按上限发了。
func TestSetSupplyIncentiveRejectsAmountOverCapInsteadOfClamping(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := validIncentiveSettings()
	settings.Programs[0].Tiers[0].AmountUSD = SupplyIncentiveAmountMaxUSD + 1

	err := svc.SetSupplyIncentiveSettings(context.Background(), settings)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSupplyIncentiveAmountOutOfRange)
	assert.Empty(t, repo.setValue, "被拒的配置不该留下任何写入痕迹")
}

func TestSetSupplyIncentiveRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*SupplyIncentiveSettings)
		wantErr error
	}{
		{
			// slug 进幂等键，非法 slug 会拼出一个无法被唯一索引正确约束的键。
			name:    "slug 含非法字符",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Slug = "bind_26Q4!" },
			wantErr: ErrSupplyIncentiveInvalidSlug,
		},
		{
			name:    "slug 超长",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Slug = "abcdefghijklmnop" },
			wantErr: ErrSupplyIncentiveInvalidSlug,
		},
		{
			// 同 slug 的两个活动会互相「已经发过了」，后配的那个永远发不出钱。
			name: "slug 重复",
			mutate: func(s *SupplyIncentiveSettings) {
				s.Programs = append(s.Programs, s.Programs[0])
			},
			wantErr: ErrSupplyIncentiveDuplicateSlug,
		},
		{
			name:    "没有档位",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Tiers = nil },
			wantErr: ErrSupplyIncentiveNoTiers,
		},
		{
			// 两档同天数 = 同一个人同一天满足两档，而下标不同的幂等键让两笔都发出去。
			name: "天数门槛重复",
			mutate: func(s *SupplyIncentiveSettings) {
				s.Programs[0].Tiers[1].MinActiveDays = s.Programs[0].Tiers[0].MinActiveDays
			},
			wantErr: ErrSupplyIncentiveDuplicateDays,
		},
		{
			name:    "天数门槛为零",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Tiers[0].MinActiveDays = 0 },
			wantErr: ErrSupplyIncentiveDaysOutOfRange,
		},
		{
			name:    "金额为零",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Tiers[0].AmountUSD = 0 },
			wantErr: ErrSupplyIncentiveAmountOutOfRange,
		},
		{
			name:    "名额为负",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Tiers[0].Slots = -1 },
			wantErr: ErrSupplyIncentiveSlotsOutOfRange,
		},
		{
			// 写错一个平台名的现象是「配了活动但一个人都没发」——最难发现的那种失败。
			name:    "未知平台",
			mutate:  func(s *SupplyIncentiveSettings) { s.Programs[0].Platform = "anthropicc" },
			wantErr: ErrSupplyIncentiveUnknownPlatform,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyIncentiveSettingRepoStub{}
			svc := newSupplyIncentiveSettingService(t, repo)

			settings := validIncentiveSettings()
			tc.mutate(settings)

			err := svc.SetSupplyIncentiveSettings(context.Background(), settings)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Empty(t, repo.setValue)
		})
	}
}

// 档位顺序决定幂等键里的下标，所以排序必须在**写库之前**完成。
// 否则同一份配置换个书写顺序，同一个档位就换了 key，已经发过的人会再领一次。
func TestSetSupplyIncentiveSortsTiersBeforePersisting(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := &SupplyIncentiveSettings{
		Enabled: true,
		Programs: []SupplyIncentiveProgram{{
			Slug:    "bind26q4",
			StartAt: todayIncentiveStartAt(),
			Tiers: []SupplyIncentiveTier{
				{MinActiveDays: 90, AmountUSD: 50, Slots: 5},
				{MinActiveDays: 10, AmountUSD: 5, Slots: 60},
				{MinActiveDays: 30, AmountUSD: 20, Slots: 5},
			},
		}},
	}
	require.NoError(t, svc.SetSupplyIncentiveSettings(context.Background(), settings))

	stored := parseSupplyIncentiveSettings(repo.setValue)
	require.Len(t, stored.Programs, 1)
	days := []int{}
	for _, tier := range stored.Programs[0].Tiers {
		days = append(days, tier.MinActiveDays)
	}
	assert.Equal(t, []int{10, 30, 90}, days)
}

// slug 与 platform 落库前统一小写去空格：同一个活动不该因为运营多打一个空格
// 就换一个幂等键。
func TestSetSupplyIncentiveNormalizesSlugAndPlatformCase(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := validIncentiveSettings()
	settings.Programs[0].Slug = "  BIND26Q4 "
	settings.Programs[0].Platform = " Anthropic "

	require.NoError(t, svc.SetSupplyIncentiveSettings(context.Background(), settings))
	stored := parseSupplyIncentiveSettings(repo.setValue)
	require.Len(t, stored.Programs, 1)
	assert.Equal(t, "bind26q4", stored.Programs[0].Slug)
	assert.Equal(t, PlatformAnthropic, stored.Programs[0].Platform)
}

// ---------------------------------------------------------------------------
// 读路径：坏配置一律收敛成安全值，绝不原样照发。
// ---------------------------------------------------------------------------

// 一份被手工改坏的 JSON 不该让 worker 按它发钱。
func TestGetSupplyIncentiveClampsCorruptValues(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
			{"min_active_days":0,"amount_usd":99999,"slots":-3},
			{"min_active_days":99999,"amount_usd":20,"slots":9999}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := svc.GetSupplyIncentiveSettings(context.Background())
	require.Len(t, settings.Programs, 1)
	tiers := settings.Programs[0].Tiers
	require.Len(t, tiers, 2)

	assert.Equal(t, 1, tiers[0].MinActiveDays, "天数下限夹到 1")
	assert.InDelta(t, float64(SupplyIncentiveAmountMaxUSD), tiers[0].AmountUSD, 1e-9,
		"越界金额夹回上限，而不是照发 99999")
	assert.Zero(t, tiers[0].Slots, "负名额夹成 0")
	assert.Equal(t, SupplyIncentiveMinActiveDaysMax, tiers[1].MinActiveDays)
	assert.Equal(t, SupplyIncentiveSlotsMax, tiers[1].Slots)
}

// 收拾不出安全替代值的东西整个丢掉：slug 进幂等键，没有「差不多的 slug」这回事。
func TestGetSupplyIncentiveDropsUnusablePrograms(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[
			{"slug":"bad slug","tiers":[{"min_active_days":10,"amount_usd":5,"slots":10}]},
			{"slug":"notiers","start_at":"2020-01-01","tiers":[]},
			{"slug":"zeroamt","start_at":"2020-01-01","tiers":[{"min_active_days":10,"amount_usd":0,"slots":10}]},
			{"slug":"badplat","start_at":"2020-01-01","platform":"ollama","tiers":[{"min_active_days":10,"amount_usd":5,"slots":10}]},
			{"slug":"good","start_at":"2020-01-01","tiers":[{"min_active_days":10,"amount_usd":5,"slots":10}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := svc.GetSupplyIncentiveSettings(context.Background())
	require.Len(t, settings.Programs, 1, "只有合法的那个活动应当留下")
	assert.Equal(t, "good", settings.Programs[0].Slug)
}

// 坏 JSON = 没有活动，而不是报错或半份配置。fail-closed 的方向与另外六个 key 一致。
func TestGetSupplyIncentiveFailsClosedOnCorruptJSON(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{value: `{"enabled":true,"programs":`}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := svc.GetSupplyIncentiveSettings(context.Background())
	require.NotNil(t, settings)
	assert.False(t, settings.Active())
}

func TestGetSupplyIncentiveFailsClosedOnReadError(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{getErr: errors.New("db down")}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := svc.GetSupplyIncentiveSettings(context.Background())
	require.NotNil(t, settings)
	assert.False(t, settings.Active(), "读不到配置时必须不发钱")
}

// 缓存返回的必须是深拷贝：Programs/Tiers 是引用，浅拷贝会让上一个调用方
// 改到缓存里那一份，而那份决定发多少钱。
func TestGetSupplyIncentiveReturnsDeepCopy(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"2020-01-01","tiers":[
			{"min_active_days":10,"amount_usd":5,"slots":60}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)
	ctx := context.Background()

	first := svc.GetSupplyIncentiveSettings(ctx)
	require.Len(t, first.Programs, 1)
	first.Programs[0].Slug = "hijacked"
	first.Programs[0].Tiers[0].AmountUSD = 500

	second := svc.GetSupplyIncentiveSettings(ctx)
	require.Len(t, second.Programs, 1)
	assert.Equal(t, "bind26q4", second.Programs[0].Slug)
	assert.InDelta(t, 5.0, second.Programs[0].Tiers[0].AmountUSD, 1e-9)
}

// 幂等键的形状是这个功能的地基：它落在 (action, request_id) 唯一索引上，
// 变一个字符就等于「这一档从没发过」。
func TestSupplyIncentiveRequestKeyShape(t *testing.T) {
	assert.Equal(t, "cmp:bind26q4:t2:a", SupplyIncentiveRequestPrefix("bind26q4", 2))
	assert.Equal(t, "cmp:bind26q4:t2:a4567", SupplyIncentiveRequestID("bind26q4", 2, 4567))
	// 大小写与空格不该产出第二个键。
	assert.Equal(t, "cmp:bind26q4:t0:a1", SupplyIncentiveRequestID(" BIND26Q4 ", 0, 1))
	// request_id 是 VARCHAR(64)：最长 slug + 最大档位下标 + 19 位账号 id 也要塞得下。
	longest := SupplyIncentiveRequestID("abcdefghijkl", SupplyIncentiveTiersMax-1, 9223372036854775807)
	assert.LessOrEqual(t, len(longest), 64)
}

// ---------------------------------------------------------------------------
// 活动起算日
//
// 它是「能连着办几期活动」的全部机制：每期的在线天数桶从这一天才开始涨。
// 所以这一组盯的是两件事——不合法的日期进不来，以及**回填**进不来。
// ---------------------------------------------------------------------------

func TestSupplyIncentiveProgramStartDay(t *testing.T) {
	p := SupplyIncentiveProgram{StartAt: "2026-10-01"}
	day, ok := p.StartDay()
	require.True(t, ok)
	assert.Equal(t, 2026, day.Year())
	assert.Equal(t, time.UTC, day.Location(), "起算日必须按 UTC 解析，与天数桶的日期键同一个时区")

	// 起算日当天就算已开始（不是「第二天才开始」）。
	assert.True(t, p.Started(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, p.Started(time.Date(2026, 10, 9, 23, 59, 0, 0, time.UTC)))
	assert.False(t, p.Started(time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)))

	for _, bad := range []string{"", "2026/10/01", "2026-13-01", "tomorrow"} {
		_, ok := SupplyIncentiveProgram{StartAt: bad}.StartDay()
		assert.False(t, ok, "%q 不该被当成一个起算日", bad)
		assert.False(t, SupplyIncentiveProgram{StartAt: bad}.Started(time.Now()),
			"解析不出起算日的活动永远不算已开始——fail-closed")
	}
}

func TestSetSupplyIncentiveRejectsMissingOrInvalidStartAt(t *testing.T) {
	for _, bad := range []string{"", "2026/10/01", "not-a-date"} {
		repo := &supplyIncentiveSettingRepoStub{}
		svc := newSupplyIncentiveSettingService(t, repo)

		settings := validIncentiveSettings()
		settings.Programs[0].StartAt = bad

		err := svc.SetSupplyIncentiveSettings(context.Background(), settings)
		require.Error(t, err, "start_at=%q", bad)
		assert.ErrorIs(t, err, ErrSupplyIncentiveInvalidStartAt)
		assert.Empty(t, repo.setValue)
	}
}

// 回填必须拒绝而不是夹到今天：天数桶只能往前累加，历史重建不出来。
// 夹回去的话管理员以为活动从他填的那天算起，实际从今天算起，差额是要发出去的钱。
func TestSetSupplyIncentiveRejectsBackdatedStartAtForNewProgram(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := validIncentiveSettings()
	settings.Programs[0].StartAt = time.Now().UTC().AddDate(0, 0, -1).Format(SupplyIncentiveDayFormat)

	err := svc.SetSupplyIncentiveSettings(context.Background(), settings)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSupplyIncentiveBackdatedStartAt)
	assert.Empty(t, repo.setValue)
}

// 但**已经开跑的活动必须还能改**。它的 start_at 必然是过去的日期，一律按回填拒绝
// 的话，活动一开始就再也调不了 slots——而放量节奏本来就要靠调 slots。
func TestSetSupplyIncentiveAllowsEditingRunningProgramWithPastStartAt(t *testing.T) {
	past := time.Now().UTC().AddDate(0, 0, -30).Format(SupplyIncentiveDayFormat)
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"` + past + `",` +
			`"tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := &SupplyIncentiveSettings{
		Enabled: true,
		Programs: []SupplyIncentiveProgram{{
			Slug:    "bind26q4",
			StartAt: past, // 没变
			Tiers:   []SupplyIncentiveTier{{MinActiveDays: 10, AmountUSD: 5, Slots: 5}}, // 放量
		}},
	}
	require.NoError(t, svc.SetSupplyIncentiveSettings(context.Background(), settings))

	stored := parseSupplyIncentiveSettings(repo.setValue)
	require.Len(t, stored.Programs, 1)
	assert.Equal(t, 5, stored.Programs[0].Tiers[0].Slots)
	assert.Equal(t, past, stored.Programs[0].StartAt)
}

// 改动起算日本身则要重新受检：把一期活动的起点往回挪，等于凭空追认一段没人计过数的
// 时间，而那段时间的桶是空的——所有人都不够格，活动看起来配好了却不发钱。
func TestSetSupplyIncentiveRejectsMovingStartAtIntoThePast(t *testing.T) {
	past := time.Now().UTC().AddDate(0, 0, -30).Format(SupplyIncentiveDayFormat)
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[{"slug":"bind26q4","start_at":"` + past + `",` +
			`"tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := &SupplyIncentiveSettings{
		Enabled: true,
		Programs: []SupplyIncentiveProgram{{
			Slug:    "bind26q4",
			StartAt: time.Now().UTC().AddDate(0, 0, -60).Format(SupplyIncentiveDayFormat), // 更早
			Tiers:   []SupplyIncentiveTier{{MinActiveDays: 10, AmountUSD: 5, Slots: 3}},
		}},
	}
	err := svc.SetSupplyIncentiveSettings(context.Background(), settings)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSupplyIncentiveBackdatedStartAt)
}

func TestSetSupplyIncentiveAcceptsFutureStartAt(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{}
	svc := newSupplyIncentiveSettingService(t, repo)

	future := time.Now().UTC().AddDate(0, 0, 14).Format(SupplyIncentiveDayFormat)
	settings := validIncentiveSettings()
	settings.Programs[0].StartAt = future
	settings.Programs[0].NewUsersOnly = true

	require.NoError(t, svc.SetSupplyIncentiveSettings(context.Background(), settings))
	stored := parseSupplyIncentiveSettings(repo.setValue)
	require.Len(t, stored.Programs, 1)
	assert.Equal(t, future, stored.Programs[0].StartAt)
	assert.True(t, stored.Programs[0].NewUsersOnly, "new_users_only 要能存下来")
}

// 读路径：起算日读不出来的活动整个丢掉（含本功能上线前写进去的旧配置）。
// 猜一个原点会直接决定发多少钱，所以这里 fail-closed。
func TestGetSupplyIncentiveDropsProgramsWithoutStartAt(t *testing.T) {
	repo := &supplyIncentiveSettingRepoStub{
		value: `{"enabled":true,"programs":[
			{"slug":"legacy","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"broken","start_at":"nope","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]},
			{"slug":"good","start_at":"2026-10-01","tiers":[{"min_active_days":10,"amount_usd":5,"slots":3}]}]}`,
	}
	svc := newSupplyIncentiveSettingService(t, repo)

	settings := svc.GetSupplyIncentiveSettings(context.Background())
	require.Len(t, settings.Programs, 1, "只有带合法起算日的活动留下")
	assert.Equal(t, "good", settings.Programs[0].Slug)
}

func TestCloneSupplyIncentiveCarriesStartAtAndNewUsersOnly(t *testing.T) {
	src := &SupplyIncentiveSettings{Programs: []SupplyIncentiveProgram{{
		Slug: "bind26q4", StartAt: "2026-10-01", NewUsersOnly: true,
		Tiers: []SupplyIncentiveTier{{MinActiveDays: 10, AmountUSD: 5, Slots: 3}},
	}}}
	clone := cloneSupplyIncentiveSettings(src)
	require.Len(t, clone.Programs, 1)
	assert.Equal(t, "2026-10-01", clone.Programs[0].StartAt)
	assert.True(t, clone.Programs[0].NewUsersOnly)
}
