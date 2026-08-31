//go:build unit

// APEXONE-EXT: SLA 错误口径的定义。
//
// 这条口径决定「哪些错算平台的账」，而它有两处使用（总览与趋势图）。
// 两处各写一份的症状是「总览说 2%、趋势图说 5%」，而两边单独看都对——
// 所以这里既钉住口径本身，也钉住「只有一个定义」这件事。
package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 口径必须同时排除三类：非错误、业务限流、客户端自己的错。
func TestOpsErrorSLAExprExcludesNonPlatformErrors(t *testing.T) {
	assert.Contains(t, opsErrorSLAExpr, "status_code, 0) >= 400",
		"先得是个错")
	assert.Contains(t, opsErrorSLAExpr, "NOT is_business_limited",
		"余额不足/超配额是业务规则按预期工作，不是故障")
	assert.Contains(t, opsErrorSLAExpr, "error_owner, '') <> 'client'",
		"客户端自己造成的错不该算平台的 SLA——2026-08-31 一次客户端断连触发过 P0 误报")
}

// 客户端那一条必须对 NULL 安全。
//
// 生产数据里 error_owner 目前没有 NULL，但裸写 `error_owner <> 'client'`
// 在出现 NULL 的那天会让整行 FILTER 判为 UNKNOWN、把那条错**悄悄漏掉**——
// 方向恰好与本意相反（本意是只排除 client，不是排除未知）。
func TestOpsErrorSLAExprIsNullSafe(t *testing.T) {
	assert.Contains(t, opsErrorSLAExpr, "COALESCE(error_owner, '')",
		"必须用 COALESCE 包住，否则 NULL 会让这条错从统计里消失")
}

// 口径只有一个定义：两处使用都必须引用常量，不能各写一份字面量。
func TestOpsErrorSLAExprHasASingleDefinition(t *testing.T) {
	for _, file := range []string{"ops_repo_dashboard.go", "ops_repo_trends.go"} {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		body := string(src)

		assert.Contains(t, body, "opsErrorSLAExpr",
			"%s 必须引用共享常量", file)

		// 数一下有没有人又把口径抄成了字面量。允许常量自身的定义出现一次。
		literal := "COALESCE(status_code, 0) >= 400 AND NOT is_business_limited)"
		assert.NotContains(t, body, literal,
			"%s 里出现了硬写的 SLA 口径——它会与常量漂移", file)
	}
}

// error_total 与 business_limited 刻意**不**套这个口径。
//
// 它们回答的是「一共出了多少错」，客户端的错也是错，只是不算平台的账。
// 把它们一起改掉会让「错误总数」凭空少一截，而那个数是排查用的。
func TestErrorTotalStillCountsClientErrors(t *testing.T) {
	src, err := os.ReadFile("ops_repo_dashboard.go")
	require.NoError(t, err)

	idx := strings.Index(string(src), "AS error_total")
	require.Greater(t, idx, 0)
	line := string(src)[max(0, idx-140):idx]
	assert.NotContains(t, line, "error_owner",
		"错误总数不该排除任何归属方——它是排查用的分母")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
