//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupAwareAccountRepo 给 mockAccountRepoForPlatform 补上「按分组分账号」的能力。
//
// 原 mock 的 ListSchedulableByGroupIDAndPlatform(s) 直接忽略 groupID 返回全部账号——
// 对只关心平台过滤的测试够用，但溢出测试的全部戏码就在「供给池空、自营池有号」这个
// 分组差异上，用原 mock 会让两个池看起来一模一样，测试永远绿。
type groupAwareAccountRepo struct {
	*mockAccountRepoForPlatform
	byGroup map[int64][]Account
	// listedGroups 按调用顺序记录被查询过的分组，用来断言「溢出确实又跑了一轮，且跑在
	// 另一个池上」——只看返回的账号 id 分不清它是从哪个池里挑出来的。
	listedGroups []int64
}

func newGroupAwareAccountRepo(byGroup map[int64][]Account) *groupAwareAccountRepo {
	base := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{}}
	for groupID := range byGroup {
		accounts := byGroup[groupID]
		for i := range accounts {
			base.accounts = append(base.accounts, accounts[i])
			base.accountsByID[accounts[i].ID] = &accounts[i]
		}
	}
	return &groupAwareAccountRepo{mockAccountRepoForPlatform: base, byGroup: byGroup}
}

func (m *groupAwareAccountRepo) listGroup(groupID int64, platforms []string) []Account {
	m.listedGroups = append(m.listedGroups, groupID)
	var result []Account
	for _, acc := range m.byGroup[groupID] {
		for _, platform := range platforms {
			if acc.Platform == platform && acc.IsSchedulable() {
				result = append(result, acc)
				break
			}
		}
	}
	return result
}

func (m *groupAwareAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	return m.listGroup(groupID, []string{platform}), nil
}

func (m *groupAwareAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	return m.listGroup(groupID, platforms), nil
}

// supplyPoolAccount 造一个属于 groupID、可调度的 anthropic 账号。
func supplyPoolAccount(id, groupID int64) Account {
	return Account{
		ID:            id,
		Platform:      PlatformAnthropic,
		Status:        StatusActive,
		Schedulable:   true,
		Priority:      1,
		AccountGroups: []AccountGroup{{AccountID: id, GroupID: groupID}},
		GroupIDs:      []int64{groupID},
	}
}

const (
	testSupplyGroupID    = int64(10)
	testFirstPartyGroupI = int64(11)
)

// newOverflowGateway 拼一个最小可跑的调度器：
//   - concurrencyService 为 nil → 走非 load-batch 分支，行为最好推理；
//   - rateLimitService 为 nil → 调度阈值过滤是空操作；
//   - schedulerSnapshot 为 nil → 账号 hydrate 是空操作；
//   - channelService 为 nil → 渠道限价预检查恒为 false。
//
// 于是「选不到号」只可能来自分组里真的没号，正是要测的那件事。
func newOverflowGateway(t *testing.T, repo *groupAwareAccountRepo, settingsJSON string) (*GatewayService, *mockGroupRepoForGateway) {
	t.Helper()
	groupRepo := &mockGroupRepoForGateway{
		groups: map[int64]*Group{
			testSupplyGroupID: {
				ID:       testSupplyGroupID,
				Platform: PlatformAnthropic,
				Status:   StatusActive,
				Hydrated: true,
			},
			testFirstPartyGroupI: {
				ID:       testFirstPartyGroupI,
				Platform: PlatformAnthropic,
				Status:   StatusActive,
				Hydrated: true,
			},
		},
	}
	settingRepo := &supplyPoolSettingRepoStub{value: settingsJSON}
	if settingsJSON == "" {
		settingRepo.getErr = ErrSettingNotFound
	}
	return &GatewayService{
		accountRepo:    repo,
		groupRepo:      groupRepo,
		cfg:            testConfig(),
		settingService: newSupplyPoolSettingService(t, settingRepo),
	}, groupRepo
}

func overflowEnabledJSON() string {
	return `{"enabled":true,"supply_group_id":10,"overflow_group_id":11}`
}

func overflowCtx(t *testing.T, groupRepo *mockGroupRepoForGateway, groupID int64) context.Context {
	t.Helper()
	return context.WithValue(context.Background(), ctxkey.Group, groupRepo.groups[groupID])
}

func TestSelectAccountWithLoadAwareness_OverflowsToFirstPartyPoolWhenSupplyExhausted(t *testing.T) {
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowEnabledJSON())

	groupID := testSupplyGroupID
	result, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Account)
	assert.Equal(t, int64(1), result.Account.ID)
	// 两个池都被查过，且顺序是「先供给池、后自营池」——反过来意味着溢出成了默认路径。
	require.GreaterOrEqual(t, len(repo.listedGroups), 2)
	assert.Equal(t, testSupplyGroupID, repo.listedGroups[0])
	assert.Contains(t, repo.listedGroups, testFirstPartyGroupI)

	// 调用方传进来的 groupID 不能被改写。计费读的是 apiKey.Group，
	// 一旦这里把消费者的分组换成自营池，溢出就顺手把价签也换了。
	assert.Equal(t, testSupplyGroupID, groupID)
}

func TestSelectAccountWithLoadAwareness_NoOverflowWhenSupplyPoolHasAccounts(t *testing.T) {
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {supplyPoolAccount(1, testSupplyGroupID)},
		testFirstPartyGroupI: {supplyPoolAccount(2, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowEnabledJSON())

	groupID := testSupplyGroupID
	result, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.NoError(t, err)
	require.NotNil(t, result.Account)
	assert.Equal(t, int64(1), result.Account.ID)
	// 自营池一次都不该被碰——每碰一次都是平台在自吃成本。
	assert.NotContains(t, repo.listedGroups, testFirstPartyGroupI)
}

func TestSelectAccountWithLoadAwareness_NoOverflowWhenDisabled(t *testing.T) {
	cases := map[string]string{
		"key 没配":    "",
		"开关关着":      `{"enabled":false,"supply_group_id":10,"overflow_group_id":11}`,
		"兜底池 id 未填": `{"enabled":true,"supply_group_id":10}`,
		"自己溢出到自己":   `{"enabled":true,"supply_group_id":10,"overflow_group_id":10}`,
		"JSON 坏了":   `{"enabled":true,`,
		"配的是别的供给分组": `{"enabled":true,"supply_group_id":12,"overflow_group_id":11}`,
	}
	for name, settingsJSON := range cases {
		t.Run(name, func(t *testing.T) {
			repo := newGroupAwareAccountRepo(map[int64][]Account{
				testSupplyGroupID:    {},
				testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
			})
			svc, groupRepo := newOverflowGateway(t, repo, settingsJSON)

			groupID := testSupplyGroupID
			_, err := svc.SelectAccountWithLoadAwareness(
				overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			assert.NotContains(t, repo.listedGroups, testFirstPartyGroupI)
		})
	}
}

func TestSelectAccountWithLoadAwareness_NonSupplyGroupDoesNotOverflow(t *testing.T) {
	// 门开得窄的那条规则的行为面：自营池自己耗尽了就是耗尽了，不会再往回溢一次。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {supplyPoolAccount(1, testSupplyGroupID)},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowEnabledJSON())

	groupID := testFirstPartyGroupI
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testFirstPartyGroupI), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.NotContains(t, repo.listedGroups, testSupplyGroupID)
}

func TestSelectAccountWithLoadAwareness_ReturnsOriginalErrorWhenBothPoolsEmpty(t *testing.T) {
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowEnabledJSON())

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	// 请求打的是消费者自己的分组，报一个指向自营池的错误会把排查的人引到错误的池子上。
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	assert.Contains(t, repo.listedGroups, testFirstPartyGroupI)
}

func TestSelectAccountWithLoadAwareness_NoOverflowOnNonExhaustionError(t *testing.T) {
	// claude_code_only 分组遇上非 CC 客户端且没配 fallback → ErrClaudeCodeOnly。
	// 那是「这个请求不该被服务」，不是「没号了」，拿自营池去补等于绕过一条访问限制。
	repo := newGroupAwareAccountRepo(map[int64][]Account{
		testSupplyGroupID:    {},
		testFirstPartyGroupI: {supplyPoolAccount(1, testFirstPartyGroupI)},
	})
	svc, groupRepo := newOverflowGateway(t, repo, overflowEnabledJSON())
	groupRepo.groups[testSupplyGroupID].ClaudeCodeOnly = true

	groupID := testSupplyGroupID
	_, err := svc.SelectAccountWithLoadAwareness(
		overflowCtx(t, groupRepo, testSupplyGroupID), &groupID, "", "claude-sonnet-4-6", nil, "", 0)

	require.ErrorIs(t, err, ErrClaudeCodeOnly)
	assert.NotContains(t, repo.listedGroups, testFirstPartyGroupI)
}

func TestResolveSupplyOverflowGroupIDIsNilSafe(t *testing.T) {
	groupID := testSupplyGroupID
	ctx := context.Background()

	var nilSvc *GatewayService
	_, ok := nilSvc.resolveSupplyOverflowGroupID(ctx, &groupID)
	assert.False(t, ok)

	// 没注入 SettingService 的老装配路径：静默不溢出，不是 panic。
	_, ok = (&GatewayService{}).resolveSupplyOverflowGroupID(ctx, &groupID)
	assert.False(t, ok)

	repo := newGroupAwareAccountRepo(map[int64][]Account{})
	svc, _ := newOverflowGateway(t, repo, overflowEnabledJSON())
	_, ok = svc.resolveSupplyOverflowGroupID(ctx, nil)
	assert.False(t, ok)
}

func TestResolveSupplyOverflowGroupIDIgnoresUnresolvableGroup(t *testing.T) {
	// 分组在配置之后被删掉：调度侧兜底，退回原错误，而不是往一个不存在的池里再跑一轮。
	repo := newGroupAwareAccountRepo(map[int64][]Account{})
	svc, _ := newOverflowGateway(t, repo, overflowEnabledJSON())

	missing := int64(999)
	_, ok := svc.resolveSupplyOverflowGroupID(context.Background(), &missing)
	assert.False(t, ok)
}
