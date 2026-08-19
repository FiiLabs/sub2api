//go:build unit

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

func newAgreementSettingService(t *testing.T, repo *supplyPoolSettingRepoStub) *SettingService {
	t.Helper()
	return newSupplyPoolSettingService(t, repo)
}

// ============================================================================
// 读路径：读不到、读坏了，一律回「尚未发布」，于是接入被拒
// ============================================================================

func TestDefaultSupplyAgreementIsUnpublished(t *testing.T) {
	settings := DefaultSupplyAgreementSettings()
	assert.False(t, settings.Published())
	assert.Empty(t, settings.Version)
}

func TestGetSupplyAgreementFailsClosedOnUnreadableSettings(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"没配置", ""},
		{"JSON 坏了", `{"version":`},
		{"版本号是空白", `{"version":"   ","body":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAgreementSettingService(t, &supplyPoolSettingRepoStub{agreementValue: tc.raw})
			settings := svc.GetSupplyAgreementSettings(context.Background())
			require.NotNil(t, settings)
			assert.False(t, settings.Published(), "读不出一份可用的协议时必须当作没发布")
		})
	}
}

// 读路径只丢掉出问题的那个字段：整份协议因为一个坏链接消失，会把所有人挡在接入之外。
func TestGetSupplyAgreementDropsUnsafeURLButKeepsBody(t *testing.T) {
	raw := `{"version":"v1","url":"javascript:alert(1)","body":"条款正文"}`
	svc := newAgreementSettingService(t, &supplyPoolSettingRepoStub{agreementValue: raw})

	settings := svc.GetSupplyAgreementSettings(context.Background())
	assert.True(t, settings.Published())
	assert.Equal(t, "v1", settings.Version)
	assert.Empty(t, settings.URL, "javascript: 链接会被渲染成一个可点的 <a href>")
	assert.Equal(t, "条款正文", settings.Body)
}

// 版本号是同意记录的键。库里那份被手改成超长时，宁可当作没发布——截断会让记录
// 挂在一个库里根本不存在的版本上。
func TestGetSupplyAgreementTreatsOversizedVersionAsUnpublished(t *testing.T) {
	raw, err := json.Marshal(map[string]string{
		"version": strings.Repeat("v", SupplyAgreementVersionMaxLen+1),
		"body":    "条款正文",
	})
	require.NoError(t, err)
	svc := newAgreementSettingService(t, &supplyPoolSettingRepoStub{agreementValue: string(raw)})

	settings := svc.GetSupplyAgreementSettings(context.Background())
	assert.False(t, settings.Published())
}

func TestGetSupplyAgreementFailsClosedWhenRepoErrors(t *testing.T) {
	// getErr 只作用在池配置那个 key 上，协议这一路要单独造一个会报错的替身。
	svc := newAgreementSettingService(t, &supplyPoolSettingRepoStub{})
	svc.settingRepo = &agreementErrorSettingRepo{err: errors.New("db down")}

	settings := svc.GetSupplyAgreementSettings(context.Background())
	require.NotNil(t, settings)
	assert.False(t, settings.Published())
}

// agreementErrorSettingRepo 只做一件事：读什么都报错。
type agreementErrorSettingRepo struct {
	supplyPoolSettingRepoStub
	err error
}

func (r *agreementErrorSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", r.err
}

// ============================================================================
// 写路径：越界一律拒绝——这是法律文本，静默截断比报错糟得多
// ============================================================================

func TestSetSupplyAgreementRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		settings *SupplyAgreementSettings
	}{
		{"版本号超长", &SupplyAgreementSettings{
			Version: strings.Repeat("v", SupplyAgreementVersionMaxLen+1), Body: "x"}},
		{"链接超长", &SupplyAgreementSettings{
			Version: "v1", URL: "https://example.com/" + strings.Repeat("a", SupplyAgreementURLMaxLen)}},
		{"正文超长", &SupplyAgreementSettings{
			Version: "v1", Body: strings.Repeat("字", SupplyAgreementBodyMaxLen+1)}},
		{"链接不是 http", &SupplyAgreementSettings{
			Version: "v1", Body: "x", URL: "javascript:alert(1)"}},
		{"发布了却既没正文也没链接", &SupplyAgreementSettings{Version: "v1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyPoolSettingRepoStub{}
			svc := newAgreementSettingService(t, repo)

			err := svc.SetSupplyAgreementSettings(context.Background(), tc.settings)
			require.Error(t, err)
			assert.Empty(t, repo.setKey, "校验没过就不该落库")
		})
	}
}

// 撤回协议（清空版本号）是允许的：它把接入停掉，而那正是运营想撤回时要的效果。
func TestSetSupplyAgreementAllowsUnpublishing(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{}
	svc := newAgreementSettingService(t, repo)

	require.NoError(t, svc.SetSupplyAgreementSettings(context.Background(), &SupplyAgreementSettings{}))
	assert.Equal(t, SettingKeySupplyAgreement, repo.setKey)
}

func TestSetSupplyAgreementPersistsAndInvalidatesCache(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{agreementValue: `{"version":"v1","body":"旧条款"}`}
	svc := newAgreementSettingService(t, repo)

	// 先读一次，把旧值灌进那个 60 秒的进程内缓存。
	assert.Equal(t, "v1", svc.GetSupplyAgreementSettings(context.Background()).Version)

	require.NoError(t, svc.SetSupplyAgreementSettings(context.Background(), &SupplyAgreementSettings{
		Version: "v2", Body: "新条款", URL: "https://example.com/terms",
	}))
	assert.Equal(t, SettingKeySupplyAgreement, repo.setKey)

	var saved SupplyAgreementSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &saved))
	assert.Equal(t, "v2", saved.Version)

	// 写完立刻读：拿到的必须是新版本，否则改了协议之后还有人在旧版上点同意。
	repo.agreementValue = repo.setValue
	assert.Equal(t, "v2", svc.GetSupplyAgreementSettings(context.Background()).Version)
}

func TestSetSupplyAgreementTrimsVersionAndURL(t *testing.T) {
	repo := &supplyPoolSettingRepoStub{}
	svc := newAgreementSettingService(t, repo)

	require.NoError(t, svc.SetSupplyAgreementSettings(context.Background(), &SupplyAgreementSettings{
		Version: "  v1  ", URL: "  https://example.com/terms  ", Body: "条款",
	}))

	var saved SupplyAgreementSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &saved))
	assert.Equal(t, "v1", saved.Version, "版本号带空格会让同意记录挂在一个看不见的键上")
	assert.Equal(t, "https://example.com/terms", saved.URL)
}
