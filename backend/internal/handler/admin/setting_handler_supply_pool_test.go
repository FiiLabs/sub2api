//go:build unit

package admin

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 配置和用量拼在同一个响应里，缺哪一半这块面板都没法用：
// 只有配额没有用量，管理员看不出配额是不是快满了；反过来也一样。
func TestNewSupplyPoolSettingsResponseCarriesConfigAndUsage(t *testing.T) {
	resp := newSupplyPoolSettingsResponse(
		&service.SupplyPoolSettings{
			Enabled:            true,
			SupplyGroupID:      10,
			OverflowGroupID:    11,
			DailyOverflowLimit: 500,
		},
		&service.SupplyOverflowUsage{Day: "2026-08-18", OverflowCount: 487, DeniedCount: 12},
	)

	assert.True(t, resp.Enabled)
	assert.Equal(t, 500, resp.DailyOverflowLimit)
	assert.Equal(t, "2026-08-18", resp.UsageDay)
	assert.Equal(t, int64(487), resp.OverflowUsedToday)
	assert.Equal(t, int64(12), resp.OverflowDeniedToday)
}

// 用量读不到时只是那三个字段为零，配置照常返回——只读读数不该拖垮整个配置页。
func TestNewSupplyPoolSettingsResponseTolerantOfMissingHalves(t *testing.T) {
	resp := newSupplyPoolSettingsResponse(&service.SupplyPoolSettings{DailyOverflowLimit: 100}, nil)
	assert.Equal(t, 100, resp.DailyOverflowLimit)
	assert.Empty(t, resp.UsageDay)

	// 配置侧为 nil（服务不可用）也不能 panic。
	resp = newSupplyPoolSettingsResponse(nil, &service.SupplyOverflowUsage{Day: "2026-08-18"})
	assert.False(t, resp.Enabled)
	assert.Equal(t, "2026-08-18", resp.UsageDay)
}

// daily_overflow_limit 用指针接：0 是「不限量」这个有意义的取值，
// 漏传才是「不要动」。用值类型的话，漏传会被当成 0 → 静默把配额清成不限量。
func TestUpdateSupplyPoolSettingsRequestDistinguishesZeroFromAbsent(t *testing.T) {
	var absent UpdateSupplyPoolSettingsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true}`), &absent))
	assert.Nil(t, absent.DailyOverflowLimit)

	var zero UpdateSupplyPoolSettingsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"daily_overflow_limit":0}`), &zero))
	require.NotNil(t, zero.DailyOverflowLimit)
	assert.Equal(t, 0, *zero.DailyOverflowLimit)
}
