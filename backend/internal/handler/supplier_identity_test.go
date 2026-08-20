//go:build unit

// APEXONE-EXT: 双边市场——供给侧"我是谁"的唯一来源。
//
// 这个文件只证一件事：`/user/supply/*` 的每一个端点，用户 id 都只可能来自 JWT。
//
// 它值得单独成文，是因为这条性质失守时**没有任何症状**。多读一个
// `user_id` 入参不会让任何测试变红、不会让任何请求报错，只会让一个人
// 能把账号挂到别人名下、能查别人的流水、能把钱提到自己这里——
// 而这三件事看起来都像功能正常工作。行为测试也覆盖不到：要发现它，
// 得先想到去构造一个"带着 A 的 token、请求体里写 B"的请求，
// 也就是得先怀疑这段代码，而那正是这里不能依赖的东西。
//
// 因此断言是**结构性**的：不检查某次调用的结果，检查这段代码里
// 有没有第二条路径可以回答"我是谁"。
package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierUserSideFiles 是挂着 *SupplierHandler 方法的两个文件。
// supplier_admin_handler.go 刻意不在其中：管理端的 `user_id` 查询参数是筛子，
// 是那一层该有的东西（见 §3.6），而它的鉴权在路由组的 adminAuth 上。
var supplierUserSideFiles = []string{
	"supplier_handler.go",
	"supplier_withdrawal_handler.go",
}

func supplierUserSideSource(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	for _, file := range supplierUserSideFiles {
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		sources[file] = string(content)
	}
	return sources
}

// 每一个 *SupplierHandler 的端点方法都必须走那**一个**取 id 的入口。
//
// 直接在方法体里 `GetAuthSubjectFromContext` 也能拿到同一个 id，但那样
// "取不到 id 时怎么办"就成了每个方法各自的决定——十六份实现里迟早有一份
// 忘了在 !ok 时 return，于是它带着 userID=0 往下走。收敛到一个入口，
// 是让那个决定只有一份。
func TestSupplierUserEndpointsTakeUserIDFromJWTOnly(t *testing.T) {
	// 只匹配**导出**的方法：*SupplierHandler 上的导出方法与路由是一一对应的，
	// 未导出的那几个（currentUserID / mutateAccount / accountIDParam）是辅助函数，
	// 由下面两个测试各自钉住。
	method := regexp.MustCompile(`\nfunc \(h \*SupplierHandler\) ([A-Z]\w*)\(c \*gin\.Context\)[^{]*\{`)

	checked := 0
	for file, source := range supplierUserSideSource(t) {
		locations := method.FindAllStringSubmatchIndex(source, -1)
		for i, location := range locations {
			name := source[location[2]:location[3]]
			end := len(source)
			if i+1 < len(locations) {
				end = locations[i+1][0]
			}
			body := source[location[1]:end]

			checked++
			t.Run(file+"/"+name, func(t *testing.T) {
				assert.True(t,
					strings.Contains(body, "h.currentUserID(c)") || strings.Contains(body, "h.mutateAccount("),
					"这个端点没走 currentUserID：它是怎么知道请求的人是谁的？")
			})
		}
	}
	require.Greater(t, checked, 14, "端点数少于预期，正则大概率没匹配上——这个测试正在空转")
}

// mutateAccount 是第二个入口，它必须自己先取 id。
// 没有这一条，上面那个测试可以被"随便加个走 mutateAccount 的方法"绕过。
func TestSupplierMutateAccountResolvesUserItself(t *testing.T) {
	source := supplierUserSideSource(t)["supplier_handler.go"]
	start := strings.Index(source, "func (h *SupplierHandler) mutateAccount(")
	require.NotEqual(t, -1, start)
	body := source[start:]

	assert.Less(t,
		strings.Index(body, "h.currentUserID(c)"),
		strings.Index(body, "mutate("),
		"mutateAccount 必须先确定是谁，再动账号")
}

// 用户侧一行都不能从请求里读 user_id。
//
// 这是上一条的另一半：上面证的是"正确的来源被用了"，这条证的是
// "没有第二个来源"。两条都要，因为一个方法完全可以既调 currentUserID、
// 又在请求体里认一个 user_id 覆盖掉它。
func TestSupplierUserEndpointsNeverReadUserIDFromRequest(t *testing.T) {
	forbidden := []string{
		`c.Query("user_id")`,
		`c.DefaultQuery("user_id"`,
		`c.PostForm("user_id")`,
		`c.Param("user_id")`,
		"`json:\"user_id\"`",
		"`form:\"user_id\"`",
	}

	for file, source := range supplierUserSideSource(t) {
		// 管理侧那半边（reviewerID 之后）不在此列：它的 user_id 是筛子。
		if cut := strings.Index(source, "// 管理侧"); cut != -1 {
			source = source[:cut]
		}
		for _, pattern := range forbidden {
			assert.NotContainsf(t, source, pattern,
				"%s 从请求里读到了 user_id：这等于让调用方指定自己是谁", file)
		}
	}
}

// 整个用户侧只有一处直接碰 JWT 上下文。
//
// 多一处就多一个"取不到怎么办"的决定，而这些决定里最省事的那个（当成 0 继续走）
// 恰好是最危险的。
func TestSupplierUserSideHasSingleAuthContextRead(t *testing.T) {
	reads := 0
	for _, source := range supplierUserSideSource(t) {
		if cut := strings.Index(source, "// 管理侧"); cut != -1 {
			source = source[:cut]
		}
		reads += strings.Count(source, "GetAuthSubjectFromContext(")
	}
	assert.Equal(t, 1, reads,
		"用户侧出现了第二处直接读 JWT 上下文的地方——应当收敛到 currentUserID")
}
