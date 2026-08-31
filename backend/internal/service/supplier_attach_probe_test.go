//go:build unit

// APEXONE-EXT: 双边市场——接入完成时的同步探测（probeOnAttach）。
//
// 这一组守的是三件事：
//
//  1. **不给 OAuth 开特例。** 这次探测是「第一次探测」，够不够格入池由观察期配置
//     说了算，用的是与后台任务**同一个** supplyProbationEligible。配置改回
//     2 次 / 60 分钟，行为就该回到从前——这条由参数化用例钉住。
//  2. **探测失败不回滚接入。** 号已经建好、有主、绑了组，一次探测失败不该把这三件
//     事撤销。失败留下的是 pending_review + probe_error，供给者看得到原因。
//  3. **没有探测器时行为与这个功能上线之前完全一致。** 部署方没配探测能力时，
//     接入必须照常工作。
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// instantPromoteProbationJSON 立刻入池的观察期配置：一次成功、零观察窗。
// 这是本次改动配套的生产配置。
func instantPromoteProbationJSON() string {
	return `{"enabled":true,"min_observation_minutes":0,"required_successes":1,` +
		`"probe_interval_minutes":15,"drain_window_minutes":10}`
}

// slowProbationJSON 改动之前的那套：两次成功、60 分钟观察窗。
func slowProbationJSON() string {
	return `{"enabled":true,"min_observation_minutes":60,"required_successes":2,` +
		`"probe_interval_minutes":15,"drain_window_minutes":10}`
}

// newAttachFixture 组一个带探测器的接入服务。
//
// 不复用 newOnboardingService：那个入口不暴露观察期配置，而观察期配置正是
// 这一组用例要变的那个自变量。
func newAttachFixture(t *testing.T, probationJSON string, prober *supplierProberStub) (
	*SupplierOnboardingService, *supplierOnboardingRepoStub, *supplierAccountStoreStub,
) {
	t.Helper()
	repo := &supplierOnboardingRepoStub{
		claimSession:               claimedSession(),
		agreementAcceptedByDefault: true,
	}
	store := newSupplierAccountStoreStub()
	settingRepo := &supplyPoolSettingRepoStub{
		value:          enabledSupplyPoolJSON(),
		probationValue: probationJSON,
		agreementValue: publishedAgreementJSON(),
	}
	svc := &SupplierOnboardingService{
		repo:        repo,
		accountRepo: store,
		oauth:       &supplierOAuthStub{},
		settings:    newSupplyPoolSettingService(t, settingRepo),
	}
	if prober != nil {
		svc.prober = prober
	}
	return svc, repo, store
}

func attachInput() *CompleteOAuthInput {
	return &CompleteOAuthInput{UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP}
}

// ---------------------------------------------------------------------------
// 立刻入池
// ---------------------------------------------------------------------------

// 探测通过 + 配置允许 → 授权完成那一刻就在接单。
//
// 这是这次改动的全部意义：此前供给者要等 20~30 分钟，而那段时间没有在观察任何东西，
// 只是在等后台任务的两次轮询。
func TestCompleteOAuthPromotesImmediatelyWhenProbePasses(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, store := newAttachFixture(t, instantPromoteProbationJSON(), prober)

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Len(t, prober.probed, 1, "接入时应当当场探一次")
	assert.Equal(t, SupplyStateActive, view.SupplyState)
	assert.True(t, view.Schedulable, "入池了就必须可调度，否则是个拿不到流量的空壳")
	assert.Empty(t, view.ProbeError)

	id := prober.probed[0]
	assert.True(t, store.schedulableSets[id])
	assert.Equal(t, SupplyStateActive, store.extraUpdates[id][SupplyStateExtraKey])
}

// 探测用的是观察期配置里那个模型，不是随便挑一个——
// 那个字段的意义就是「用最便宜的模型探」，而额度花的是供给者的。
func TestAttachProbeUsesConfiguredProbeModel(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, _ := newAttachFixture(t,
		`{"enabled":true,"min_observation_minutes":0,"required_successes":1,`+
			`"probe_interval_minutes":15,"probe_model":"claude-haiku-4-5","drain_window_minutes":10}`,
		prober)

	_, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)

	require.Len(t, prober.models, 1)
	assert.Equal(t, "claude-haiku-4-5", prober.models[0])
}

// ---------------------------------------------------------------------------
// 不给 OAuth 开特例：够不够格由配置说了算
// ---------------------------------------------------------------------------

// 同一次成功的探测，在「两次成功 + 60 分钟窗口」的配置下**不**入池。
//
// 这条是「不开特例」的证据：改动没有给 OAuth 路径塞一条绕过观察期的近路，
// 只是把第一次探测提前到了接入那一刻。
func TestCompleteOAuthRespectsProbationConfigInsteadOfBypassingIt(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, store := newAttachFixture(t, slowProbationJSON(), prober)

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)

	assert.Len(t, prober.probed, 1, "照样探，只是不够格入池")
	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.False(t, view.Schedulable)
	assert.Zero(t, store.schedulableCalls, "不够格时一次都不该碰调度开关")

	// 但第一次探测的成果要留下：后台任务接着攒第二次，而不是从零开始。
	id := prober.probed[0]
	assert.Equal(t, 1, store.extraUpdates[id][SupplyProbePassesExtraKey])
	assert.Equal(t, 1, view.ProbePasses)
}

// 自动入池总开关关掉时，探测照跑、入池不发生。
func TestAttachProbeDoesNotPromoteWhenProbationDisabled(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, store := newAttachFixture(t,
		`{"enabled":false,"min_observation_minutes":0,"required_successes":1,`+
			`"probe_interval_minutes":15,"drain_window_minutes":10}`, prober)

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)

	assert.Len(t, prober.probed, 1)
	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.Zero(t, store.schedulableCalls)
}

// ---------------------------------------------------------------------------
// 探测失败
// ---------------------------------------------------------------------------

// 探测失败**不回滚接入**：号建好了、有主了、绑了组，这三件事不该被一次探测撤销。
func TestAttachProbeFailureKeepsTheAccountAndRecordsTheReason(t *testing.T) {
	prober := newSupplierProberStub()
	svc, repo, store := newAttachFixture(t, instantPromoteProbationJSON(), prober)
	// 账号 id 由 store 的 nextID 决定（100 起）。
	prober.results[100] = &ScheduledTestResult{Status: "failed", ErrorMessage: "quota exhausted"}

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err, "探测失败不该让接入本身失败")
	require.NotNil(t, view)

	// 接入的三件事都完成了。
	assert.Len(t, repo.setOwnerCalls, 1, "归属照写")
	assert.NotEmpty(t, store.boundGroups[100], "分组照绑")
	assert.Contains(t, store.accounts, int64(100), "号照建")

	// 但它留在观察期，且带着可行动的原因。
	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.False(t, view.Schedulable)
	assert.Equal(t, "quota exhausted", view.ProbeError)
	assert.Equal(t, 0, store.extraUpdates[100][SupplyProbePassesExtraKey])
	assert.Zero(t, store.schedulableCalls, "探测失败绝不入池")
}

// 探测本身报错（超时、网络）与「探测返回失败」走同一条路。
func TestAttachProbeTransportErrorIsTreatedAsFailure(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, store := newAttachFixture(t, instantPromoteProbationJSON(), prober)
	prober.errs[100] = errors.New("context deadline exceeded")

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)
	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.Equal(t, "context deadline exceeded", view.ProbeError)
	assert.Zero(t, store.schedulableCalls)
}

// 接入路径**不**把 401 抬成错误态。
//
// 那条判定（连同它触发的失效事件与通知邮件）住在 probeOnce 里，是一整套。
// 在接入这一刻多写一个状态，只会让两处判据各自演化——而它们判的是同一件事。
func TestAttachProbeDoesNotMarkAccountErroredOnAuthFailure(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, store := newAttachFixture(t, instantPromoteProbationJSON(), prober)
	prober.results[100] = &ScheduledTestResult{
		Status:       "failed",
		ErrorMessage: `API returned 401: {"error":"authentication_error"}`,
	}

	_, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)

	assert.Empty(t, store.setErrorCalls, "置错误态是观察期任务的职责，不是接入的")
}

// ---------------------------------------------------------------------------
// 没有探测器
// ---------------------------------------------------------------------------

// 没配探测能力的部署：接入照常，行为与这个功能上线之前一模一样。
func TestCompleteOAuthWorksWithoutAProber(t *testing.T) {
	svc, repo, store := newAttachFixture(t, instantPromoteProbationJSON(), nil)

	view, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, SupplyStatePendingReview, view.SupplyState)
	assert.False(t, view.Schedulable)
	assert.Zero(t, store.schedulableCalls)
	assert.Len(t, repo.setOwnerCalls, 1)
	// 一次 extra 都没写：没有探测就没有探测结果可记。
	assert.Empty(t, store.extraUpdates[100])
}

// SetProber 传 nil 指针不能把服务弄成「有一个会 panic 的探测器」。
//
// 一个 nil 的 *AccountTestService 装进接口变量后不是 nil 接口——这个坑在
// NewSupplierLifecycleService 里踩过一次，这里用测试钉住不再踩第二次。
func TestSetProberIgnoresTypedNil(t *testing.T) {
	svc, _, _ := newAttachFixture(t, instantPromoteProbationJSON(), nil)
	var typedNil *AccountTestService
	svc.SetProber(typedNil)

	assert.Nil(t, svc.prober, "typed nil 不能被装进接口字段")
	_, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err, "探测器没装上时接入必须照常工作")
}

// ---------------------------------------------------------------------------
// 两条路径共用同一份入池判据
// ---------------------------------------------------------------------------

// supplyProbationEligible 是接入侧与观察期侧**唯一**的判据。
//
// 直接测这个包级函数，是因为它现在有两个调用方。此前它是
// SupplierLifecycleService 的一个方法，接入侧要用只能抄一份——
// 而两份判据漂移的症状是「接入时说不够格、十五分钟后又够格了」，
// 两处代码单独看都对。
func TestSupplyProbationEligibleIsTheSingleRule(t *testing.T) {
	now := time.Unix(1700000000, 0)
	withSince := func(since time.Time) *Account {
		return &Account{ID: 1, Extra: map[string]any{
			SupplyProbationSinceExtraKey: since.Format(time.RFC3339),
		}}
	}
	justNow := withSince(now)
	longAgo := withSince(now.Add(-2 * time.Hour))

	instant := &SupplyProbationSettings{Enabled: true, RequiredSuccesses: 1, MinObservationMinutes: 0}
	slow := &SupplyProbationSettings{Enabled: true, RequiredSuccesses: 2, MinObservationMinutes: 60}

	assert.True(t, supplyProbationEligible(justNow, instant, 1, now))
	assert.False(t, supplyProbationEligible(justNow, slow, 1, now), "次数不够")
	assert.False(t, supplyProbationEligible(justNow, slow, 2, now), "窗口没跑满")
	assert.True(t, supplyProbationEligible(longAgo, slow, 2, now), "次数够且窗口跑满")

	assert.False(t, supplyProbationEligible(justNow, nil, 9, now), "读不到配置一律不入池")
	assert.False(t, supplyProbationEligible(justNow,
		&SupplyProbationSettings{Enabled: false, RequiredSuccesses: 1}, 9, now), "总开关关着不入池")
	assert.False(t, supplyProbationEligible(&Account{ID: 1}, instant, 9, now),
		"没有观察起点的号不入池")
}
