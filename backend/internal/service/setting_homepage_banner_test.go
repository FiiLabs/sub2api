//go:build unit

// APEXONE-EXT: 首页活动横幅配置的单元测试。
//
// 这块东西挂在站点最显眼的位置上，而且是给**未登录访客**看的。所以测试的重心是
// 「什么情况下它必须不出现」——一块渲染成空条、或者带着点不动按钮的横幅，
// 比没有横幅更伤信任。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type homepageBannerSettingRepoStub struct {
	value  string
	getErr error

	setKey   string
	setValue string
	setErr   error
}

func (r *homepageBannerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyHomepageBanner {
		panic("unexpected settings key: " + key)
	}
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}

func (r *homepageBannerSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey, r.setValue = key, value
	return r.setErr
}

func (r *homepageBannerSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}
func (r *homepageBannerSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}
func (r *homepageBannerSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}
func (r *homepageBannerSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}
func (r *homepageBannerSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func newHomepageBannerService(t *testing.T, repo *homepageBannerSettingRepoStub) *SettingService {
	t.Helper()
	invalidateHomepageBannerCache()
	t.Cleanup(invalidateHomepageBannerCache)
	return &SettingService{settingRepo: repo}
}

func validBannerSettings() *HomepageBannerSettings {
	return &HomepageBannerSettings{
		Enabled:   true,
		TextZH:    "挂号奖励活动开始了",
		TextEN:    "Idle quota rewards are live",
		CTATextZH: "了解详情",
		CTATextEN: "Learn more",
		CTAURLZH:  "https://docs.apex1.us/zh-cn/earn/idle-quota-rewards/",
		CTAURLEN:  "https://docs.apex1.us/earn/idle-quota-rewards/",
		Variant:   HomepageBannerVariantPromo,
	}
}

// 部署这段代码本身不该让首页多出任何东西。
func TestDefaultHomepageBannerIsHidden(t *testing.T) {
	s := DefaultHomepageBannerSettings()
	assert.False(t, s.Enabled)
	assert.False(t, s.Active())
	assert.Equal(t, HomepageBannerVariantInfo, s.Variant, "默认样式是克制的那一种")
}

// ---------------------------------------------------------------------------
// 写路径：结构性错误一律拒绝
// ---------------------------------------------------------------------------

// 打开了却没文案 = 首页最显眼的位置上一个空条。夹成「关闭」也不行：
// 管理员看到开关是开的、却什么都没出现，会以为是前端坏了。
func TestSetHomepageBannerRejectsEnabledWithoutText(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	s := validBannerSettings()
	s.TextZH, s.TextEN = "", "   "

	err := svc.SetHomepageBannerSettings(context.Background(), s)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHomepageBannerEmptyText)
	assert.Empty(t, repo.setValue, "被拒的配置不该留下写入痕迹")
}

// 关着的横幅允许没有文案——否则「先关掉、下次再改文案」这条很自然的操作会被拦住。
func TestSetHomepageBannerAllowsEmptyTextWhenDisabled(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	require.NoError(t, svc.SetHomepageBannerSettings(context.Background(),
		&HomepageBannerSettings{Enabled: false, Variant: HomepageBannerVariantInfo}))
	assert.NotEmpty(t, repo.setValue)
}

// 坏链接必须拒绝而不是清空：一个有按钮但点不动的横幅，比没有按钮更像故障，
// 而被悄悄清空的链接管理员看不见。
func TestSetHomepageBannerRejectsBadURL(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"not-a-url",
		"ftp://example.com/x",
	} {
		repo := &homepageBannerSettingRepoStub{}
		svc := newHomepageBannerService(t, repo)

		s := validBannerSettings()
		s.CTAURLEN = bad

		err := svc.SetHomepageBannerSettings(context.Background(), s)
		require.Error(t, err, "cta_url=%q", bad)
		assert.ErrorIs(t, err, ErrHomepageBannerInvalidURL)
		assert.Empty(t, repo.setValue)
	}
}

// 有按钮文字却没有链接 = 一个死按钮。
func TestSetHomepageBannerRejectsCTATextWithoutURL(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	s := validBannerSettings()
	s.CTAURLZH, s.CTAURLEN = "", ""

	err := svc.SetHomepageBannerSettings(context.Background(), s)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHomepageBannerCTAWithoutURL)
}

func TestSetHomepageBannerRejectsUnknownVariant(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	s := validBannerSettings()
	s.Variant = "rainbow"

	err := svc.SetHomepageBannerSettings(context.Background(), s)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHomepageBannerInvalidVariant)
}

// 长度超限**夹回**而不是拒绝：截断管理员一眼看得见，所以夹回是安全的。
// 这条与上面几条的分界线，就是「他会不会察觉」。
func TestSetHomepageBannerClampsOverlongText(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	s := validBannerSettings()
	s.TextZH = strings.Repeat("字", HomepageBannerTextMaxLen*2)
	s.CTATextEN = strings.Repeat("x", HomepageBannerCTATextMaxLen*2)

	require.NoError(t, svc.SetHomepageBannerSettings(context.Background(), s))

	stored := parseHomepageBannerSettings(repo.setValue)
	assert.Equal(t, HomepageBannerTextMaxLen, len([]rune(stored.TextZH)))
	assert.Equal(t, HomepageBannerCTATextMaxLen, len([]rune(stored.CTATextEN)))
}

// 按字符截断，不是按字节——按字节会把一个汉字劈成两半，
// 产出一段非法 UTF-8，一路穿到浏览器变成替换字符。
func TestHomepageBannerClampsByRuneNotByte(t *testing.T) {
	out := clampRunes(strings.Repeat("中", 10), 3)
	assert.Equal(t, "中中中", out)
	assert.True(t, len(out) > 3, "3 个汉字远不止 3 字节，说明截的是字符")
}

// ---------------------------------------------------------------------------
// 读路径：坏配置一律收拾成「不显示」，而不是报错或原样渲染
// ---------------------------------------------------------------------------

func TestGetHomepageBannerFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		repo *homepageBannerSettingRepoStub
	}{
		{"库挂了", &homepageBannerSettingRepoStub{getErr: errors.New("db down")}},
		{"没配过", &homepageBannerSettingRepoStub{getErr: ErrSettingNotFound}},
		{"JSON 坏了", &homepageBannerSettingRepoStub{value: `{"enabled":true,`}},
		{"空值", &homepageBannerSettingRepoStub{value: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newHomepageBannerService(t, tc.repo)
			s := svc.GetHomepageBannerSettings(context.Background())
			require.NotNil(t, s)
			assert.False(t, s.Active(), "读不到就不该在首页挂东西")
		})
	}
}

// 手工改坏的 JSON：打开但没文案 → 视为关闭；坏链接 → 连同按钮文字一起清掉。
func TestGetHomepageBannerNormalizesCorruptValues(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{
		value: `{"enabled":true,"text_zh":"  ","text_en":"","variant":"rainbow"}`,
	}
	svc := newHomepageBannerService(t, repo)
	s := svc.GetHomepageBannerSettings(context.Background())
	assert.False(t, s.Enabled, "打开但没文案 = 首页一个空条，必须当成关闭")
	assert.Equal(t, HomepageBannerVariantInfo, s.Variant)

	repo2 := &homepageBannerSettingRepoStub{
		value: `{"enabled":true,"text_en":"hi","cta_text_en":"Go","cta_url_en":"javascript:alert(1)"}`,
	}
	svc2 := newHomepageBannerService(t, repo2)
	s2 := svc2.GetHomepageBannerSettings(context.Background())
	assert.True(t, s2.Active(), "文案还在，横幅照常显示")
	assert.Empty(t, s2.CTAURLEN, "坏链接必须被丢掉")
	assert.Empty(t, s2.CTATextEN, "没有链接就不该留下按钮文字，否则渲染成死按钮")
}

// ---------------------------------------------------------------------------
// 语言回退
// ---------------------------------------------------------------------------

// 只填了一种语言时，另一种语言的访客看到已填的那份，而不是一块空白。
// 空白会被当成页面坏了。
func TestHomepageBannerLanguageFallback(t *testing.T) {
	onlyEN := &HomepageBannerSettings{TextEN: "English only", CTATextEN: "Go"}
	assert.Equal(t, "English only", onlyEN.TextFor("zh-CN"), "缺中文时回退到英文")
	assert.Equal(t, "Go", onlyEN.CTATextFor("zh-CN"))

	onlyZH := &HomepageBannerSettings{TextZH: "只有中文"}
	assert.Equal(t, "只有中文", onlyZH.TextFor("en-US"))

	both := &HomepageBannerSettings{TextZH: "中", TextEN: "EN"}
	assert.Equal(t, "中", both.TextFor("zh"))
	assert.Equal(t, "中", both.TextFor("zh-Hant-TW"), "zh 的各种变体都该走中文分支")
	assert.Equal(t, "EN", both.TextFor("en"))
	assert.Equal(t, "EN", both.TextFor(""), "语言未知时走英文")

	var nilSettings *HomepageBannerSettings
	assert.Empty(t, nilSettings.TextFor("zh"))
}

func TestHomepageBannerActive(t *testing.T) {
	assert.False(t, (&HomepageBannerSettings{Enabled: false, TextEN: "x"}).Active())
	assert.False(t, (&HomepageBannerSettings{Enabled: true, TextEN: "  "}).Active())
	assert.True(t, (&HomepageBannerSettings{Enabled: true, TextEN: "x"}).Active())
	var nilSettings *HomepageBannerSettings
	assert.False(t, nilSettings.Active())
}

func TestSetHomepageBannerInvalidatesCache(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{value: `{"enabled":false}`}
	svc := newHomepageBannerService(t, repo)

	require.False(t, svc.GetHomepageBannerSettings(context.Background()).Active())

	require.NoError(t, svc.SetHomepageBannerSettings(context.Background(), validBannerSettings()))
	repo.value = repo.setValue

	// 缓存必须在写入时失效，否则运营点了保存、去首页看不到，会再点一次。
	assert.True(t, svc.GetHomepageBannerSettings(context.Background()).Active())
}

// 按语言各指各的落地页。双语站上「点了按钮落到看不懂的语言」是这个字段拆开的全部理由。
func TestHomepageBannerCTAURLPerLanguage(t *testing.T) {
	s := &HomepageBannerSettings{
		CTAURLZH: "https://docs.apex1.us/zh-cn/earn/idle-quota-rewards/",
		CTAURLEN: "https://docs.apex1.us/earn/idle-quota-rewards/",
	}
	assert.Contains(t, s.CTAURLFor("zh-CN"), "/zh-cn/")
	assert.NotContains(t, s.CTAURLFor("en"), "/zh-cn/")

	// 只做了英文落地页时，中文访客回退到英文页——好过点了一个没反应的按钮。
	onlyEN := &HomepageBannerSettings{CTAURLEN: "https://example.com/en"}
	assert.Equal(t, "https://example.com/en", onlyEN.CTAURLFor("zh"))

	onlyZH := &HomepageBannerSettings{CTAURLZH: "https://example.com/zh"}
	assert.Equal(t, "https://example.com/zh", onlyZH.CTAURLFor("en"))

	var nilSettings *HomepageBannerSettings
	assert.Empty(t, nilSettings.CTAURLFor("zh"))
}

// 有按钮文字时只要求**至少一个**链接：只配了英文链接的中文按钮会回退过去，
// 那是可用的，不该被拦下。
func TestSetHomepageBannerAcceptsSingleURLForBothLanguages(t *testing.T) {
	repo := &homepageBannerSettingRepoStub{}
	svc := newHomepageBannerService(t, repo)

	s := validBannerSettings()
	s.CTAURLZH = ""

	require.NoError(t, svc.SetHomepageBannerSettings(context.Background(), s))
	stored := parseHomepageBannerSettings(repo.setValue)
	assert.Empty(t, stored.CTAURLZH)
	assert.NotEmpty(t, stored.CTAURLEN)
	assert.Equal(t, stored.CTAURLEN, stored.CTAURLFor("zh"), "中文按钮回退到英文链接")
}

// Version 必须**与语言无关**。
//
// 这条是一个真实 bug 的回归：最初前端拿渲染出来的文案当「关掉过」的记忆键，
// 而文案随语言变——关掉中文横幅后切到英文它又冒出来，切回中文又消失，
// 用户看到的是横幅忽隐忽现。要记的是「这一期活动被关过」，不是「这一句话被关过」。
func TestHomepageBannerVersionIsLanguageIndependent(t *testing.T) {
	s := validBannerSettings()
	base := s.Version()
	assert.NotEmpty(t, base)

	// 同一份配置，反复调用必须稳定。
	assert.Equal(t, base, s.Version())

	// 换一期内容——任何一项改动都该让 version 变，横幅要重新出现。
	for name, mutate := range map[string]func(*HomepageBannerSettings){
		"改中文文案":   func(x *HomepageBannerSettings) { x.TextZH = "新一期活动" },
		"改英文文案":   func(x *HomepageBannerSettings) { x.TextEN = "A new campaign" },
		"改中文按钮":   func(x *HomepageBannerSettings) { x.CTATextZH = "马上看" },
		"改英文按钮":   func(x *HomepageBannerSettings) { x.CTATextEN = "See it" },
		"改中文链接":   func(x *HomepageBannerSettings) { x.CTAURLZH = "https://example.com/zh" },
		"改英文链接":   func(x *HomepageBannerSettings) { x.CTAURLEN = "https://example.com/en" },
		"改样式":     func(x *HomepageBannerSettings) { x.Variant = HomepageBannerVariantInfo },
	} {
		t.Run(name, func(t *testing.T) {
			v := validBannerSettings()
			mutate(v)
			assert.NotEqual(t, base, v.Version(), "%s 之后 version 必须变", name)
		})
	}

	// Enabled 不进指纹：关掉再打开同一期内容，不该让已经关过的人又看到它。
	off := validBannerSettings()
	off.Enabled = false
	assert.Equal(t, base, off.Version(), "开关不属于「内容」，不该改变 version")

	var nilSettings *HomepageBannerSettings
	assert.Empty(t, nilSettings.Version())
}

// 分隔符不可省：没有它，("ab","c") 与 ("a","bc") 会算出同一个指纹，
// 于是某些改动会被当成「没变过」，横幅不再重新出现。
func TestHomepageBannerVersionSeparatesFields(t *testing.T) {
	a := &HomepageBannerSettings{TextZH: "ab", TextEN: "c"}
	b := &HomepageBannerSettings{TextZH: "a", TextEN: "bc"}
	assert.NotEqual(t, a.Version(), b.Version())
}
