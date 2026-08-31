//go:build unit

// APEXONE-EXT: 比率类告警的最小样本量闸。
//
// 这一组守的是一条能讲清楚的原则：**单独一次失败永远不该触发告警**。
//
// 它来自一次真实误报：2026-08-31 一分钟窗口里只有 3 个请求，其中 1 个是客户端
// 自己断连造成的 400，1÷3 = 33.33% 越过 20% 阈值 → 发出一封 P0 邮件。
// 而 08-30 一天内同类告警触发了 12 次，读数里有 100%、80%、61.5%——
// 那些都不是平台崩了，是那几分钟里总共只有一两个请求。
package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 样本不足时不评估——无论比率有多难看。
//
// 参数化到三个比率指标上：它们共用同一道闸，漏掉任何一个都会让噪音从那条缝里出来。
func TestRateAlertsAreNotEvaluatedBelowMinSamples(t *testing.T) {
	for _, metric := range []string{"error_rate", "success_rate", "upstream_error_rate"} {
		t.Run(metric, func(t *testing.T) {
			// 3 个请求里错 1 个 = 33.33%，正是那封 P0 邮件的读数。
			overview := &OpsDashboardOverview{
				RequestCountSLA:   3,
				ErrorRate:         0.3333,
				SLA:               0.6667,
				UpstreamErrorRate: 0.3333,
			}
			_, ok := opsAlertRateMetric(metric, overview)
			assert.False(t, ok, "样本不足就不该产生读数，更不该比阈值")
		})
	}
}

// 样本够了就正常评估。
func TestRateAlertsEvaluateAtOrAboveMinSamples(t *testing.T) {
	overview := &OpsDashboardOverview{
		RequestCountSLA:   opsAlertMinSamples,
		ErrorRate:         0.10,
		SLA:               0.90,
		UpstreamErrorRate: 0.05,
	}
	v, ok := opsAlertRateMetric("error_rate", overview)
	assert.True(t, ok)
	assert.InDelta(t, 10.0, v, 1e-9, "读数是百分比，不是小数")
}

// 边界：恰好差一个样本仍然不评估。
func TestRateAlertsBoundaryIsExclusive(t *testing.T) {
	overview := &OpsDashboardOverview{RequestCountSLA: opsAlertMinSamples - 1, ErrorRate: 0.99}
	_, ok := opsAlertRateMetric("error_rate", overview)
	assert.False(t, ok)
}

// 「一次失败不触发最敏感的规则」——这条原则本身，用它推出的那个数字来验证。
//
// 最敏感的线上规则是「错误率过高」：`error_rate > 5`。n = opsAlertMinSamples 时
// 一次失败正好是 5%，而运算符是**严格大于**，所以不触发。
// 这条断言的意义在于：将来有人把 opsAlertMinSamples 调小，它会红。
func TestSingleFailureCannotTripTheMostSensitiveRule(t *testing.T) {
	const mostSensitiveThreshold = 5.0

	oneFailureRate := 1.0 / float64(opsAlertMinSamples) * 100
	assert.LessOrEqual(t, oneFailureRate, mostSensitiveThreshold,
		"单次失败的错误率必须不超过最敏感规则的阈值，否则最小样本量取小了")
	assert.False(t, compareMetric(oneFailureRate, ">", mostSensitiveThreshold),
		"严格大于：正好等于阈值时不该触发")
}

// 零请求照旧不评估（这是改动前就有的行为，别在收紧闸门时把它弄丢）。
func TestRateAlertsStillSkipEmptyWindows(t *testing.T) {
	_, ok := opsAlertRateMetric("error_rate", &OpsDashboardOverview{RequestCountSLA: 0})
	assert.False(t, ok)
}

// 未知指标类型仍然返回 false，最小样本量的改动不该影响这条。
func TestUnknownMetricTypeStillRejected(t *testing.T) {
	overview := &OpsDashboardOverview{RequestCountSLA: 1000, ErrorRate: 0.5}
	_, ok := opsAlertRateMetric("no_such_metric", overview)
	assert.False(t, ok)
}
