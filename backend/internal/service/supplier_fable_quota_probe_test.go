//go:build unit

// APEXONE-EXT: 双边市场——接入时的 Fable 额度探测。
//
// 免费号 / 没订阅付费方案的号连进来接不了单，只会占着观察期、被后台每 15 分钟
// 无谓地探一次。所以接入这一刻用 Fable 探一下：探到「没额度」就当场拒绝、把刚建
// 的号清掉，并让前端提示「先订阅再重试」。
//
// 这一组守两件容易崩的事：
//  1. 分类器只认真正的「没额度」信号，不把普通限流（也是 429）误判成没额度——
//     误拒一个只是这一刻超速的付费号，比放进一个免费号严重得多。
//  2. 命中时接入被拒、号被干净清掉，而不是留一个死号在列表里。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 判据取自生产 ops_error_logs 里免费/无额度号打 claude-fable-5-1 的真实上游返回。
func TestSupplyProbeNoQuotaClassifier(t *testing.T) {
	// 探测消息的形态是 account_test_service.go 拼的 "API returned %d: %s"（%s = 上游 body）。
	outOfCredits := `API returned 429: {"error":{"details":{"can_user_purchase_credits":true,` +
		`"disabled_reason":"out_of_credits","error_code":"credits_required",` +
		`"exhausted_included_allowance":false,"has_chargeable_saved_payment_method":true}}}`
	orgDisabled := `API returned 429: {"error":{"details":{"can_user_purchase_credits":true,` +
		`"disabled_reason":"org_level_disabled","error_code":"credits_required",` +
		`"exhausted_included_allowance":false}}}`
	plainCredits := `API returned 429: Usage credits are required for this model.`

	// 这些必须判成「没额度」。
	for name, msg := range map[string]string{
		"out_of_credits":     outOfCredits,
		"org_level_disabled": orgDisabled,
		"plain_credits":      plainCredits,
	} {
		t.Run("noquota/"+name, func(t *testing.T) {
			assert.True(t, supplyProbeNoQuota(msg))
		})
	}

	// 这些**不能**判成没额度——它们是瞬态或别的原因，误拒是真伤害。
	rateLimit := `API returned 429: {"error":{"message":"This request would exceed your account's rate limit. Please retry later.","type":"rate_limit_error"}}`
	upstreamRateLimit := `API returned 429: Upstream rate limit exceeded, please retry later`
	auth401 := `API returned 401: OAuth access token has expired`
	overloaded := `API returned 403: Overloaded`
	for name, msg := range map[string]string{
		"rate_limit_error":    rateLimit,
		"upstream_rate_limit": upstreamRateLimit,
		"auth_401":            auth401,
		"overloaded":          overloaded,
		"empty":               "",
	} {
		t.Run("notnoquota/"+name, func(t *testing.T) {
			assert.False(t, supplyProbeNoQuota(msg))
		})
	}
}

// 没配 probe_model 时探测用 Fable，而不是全局默认的 sonnet。
func TestSupplyResolveProbeModelDefaultsToFable(t *testing.T) {
	assert.Equal(t, supplyProbeDefaultModel, supplyResolveProbeModel(nil))
	assert.Equal(t, "claude-fable-5-1", supplyProbeDefaultModel)

	empty := &SupplyProbationSettings{ProbeModel: ""}
	assert.Equal(t, supplyProbeDefaultModel, supplyResolveProbeModel(empty))

	configured := &SupplyProbationSettings{ProbeModel: "claude-opus-5"}
	assert.Equal(t, "claude-opus-5", supplyResolveProbeModel(configured),
		"ops 显式配了就尊重配置，不强塞 Fable")
}

// 接入探测探到「没额度」→ CompleteOAuth 报错 + 刚建的号被干净清掉。
func TestAttachRejectsAccountWithoutFableQuota(t *testing.T) {
	prober := newSupplierProberStub()
	svc, repo, store := newAttachFixture(t, instantPromoteProbationJSON(), prober)
	// 账号 id 由 store 的 nextID 决定（100 起）。喂一个真实的无额度返回。
	prober.results[100] = &ScheduledTestResult{
		Status: "failed",
		ErrorMessage: `API returned 429: {"error":{"details":{"disabled_reason":"out_of_credits",` +
			`"error_code":"credits_required"}}}`,
	}

	view, err := svc.CompleteOAuth(context.Background(), attachInput())

	// 接入被拒，错误是那个专门的码（前端据此提示订阅）。
	require.ErrorIs(t, err, ErrSupplierAccountNoFableQuota)
	assert.Nil(t, view)

	// 号被干净清掉：关调度 → 抹凭证 → 删行，一个都不能少。
	assert.Contains(t, store.calls, "SetSchedulable")
	assert.Contains(t, repo.calls, "ScrubAccountCredentials")
	assert.Equal(t, []int64{100}, store.deletedIDs, "刚建的号必须被删掉，不留死号")
}

// 探到 Fable 探测用的确实是 Fable 模型（没配 probe_model 时）。
func TestAttachProbeUsesFableByDefault(t *testing.T) {
	prober := newSupplierProberStub()
	svc, _, _ := newAttachFixture(t, instantPromoteProbationJSON(), prober)

	_, err := svc.CompleteOAuth(context.Background(), attachInput())
	require.NoError(t, err)

	require.Len(t, prober.models, 1)
	assert.Equal(t, "claude-fable-5-1", prober.models[0])
}