// APEXONE-EXT: 双边市场——供给侧路由的装配测试。
//
// 这一层此前完全没有测试，而它出错的方式恰好是最安静的一类：路由挂在了错的组上、
// 少了一层中间件、或者新加的一条忘了跟着挂。这三种都不会让任何 handler 报错，
// 只会让一条**本该要登录的接口不要登录**、或者一条会动余额的写接口不进审计日志。
//
// 因此这里的断言一律**从注册结果反推**（`router.Routes()`），而不是照着
// supplier.go 抄一遍路径清单：后者只能证明"我写的和我写的一样"，
// 而前者对将来新增的每一条路由都自动生效——这正是要防的那种遗漏。
package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierTestHandlers 造一组"结构在、依赖全空"的 handler。
//
// 依赖全空是刻意的：这个文件测的是装配，不是业务。让 handler 真的能跑起来
// 需要把六七个 service 都造出来，而那会让这些测试在任何一个 service 的
// 构造签名变化时无故变红——它们跟路由是不是挂对了没有一点关系。
//
// `Admin` 这个空壳是 RegisterAdminRoutes 的硬性前提：上游那边每个
// registerXxxRoutes 都直接读 `h.Admin.<字段>`，`h.Admin` 为 nil 会当场解引用崩掉
// （字段本身为 nil 反而没事——绑定到 nil 接收者的方法值不解引用）。
// 这是上游的既有行为，不在这次改动的范围里。
func supplierTestHandlers() *handler.Handlers {
	return &handler.Handlers{
		Admin:    &handler.AdminHandlers{},
		Supplier: &handler.SupplierHandler{Admin: &handler.SupplierAdminHandler{}},
	}
}

// supplierRoutePaths 把注册结果里 /user/supply 那一段挑出来，并把路径参数填成具体值。
func supplierRoutePaths(t *testing.T, router *gin.Engine, prefix string) []gin.RouteInfo {
	t.Helper()
	var found []gin.RouteInfo
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, prefix) {
			found = append(found, route)
		}
	}
	require.NotEmpty(t, found, "没有一条 %s 路由被注册——装配整个断了", prefix)
	return found
}

// concreteRequestPath 把 /user/supply/accounts/:id 变成能真的发出去的路径。
//
// 填什么值不影响这个文件里的任何断言——gin 的参数段匹配任意非空片段，而这些
// 测试全都在中间件层就收尾了，handler 一次也没被调用。按参数名填成像样的值
// 只是为了让失败信息里出现的是 `/payout-wallets/bsc` 而不是 `/payout-wallets/1`，
// 后者会让读日志的人以为链号是个整数。
func concreteRequestPath(path string) string {
	placeholders := map[string]string{":network": "bsc"}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		if value, ok := placeholders[segment]; ok {
			segments[i] = value
			continue
		}
		segments[i] = "1"
	}
	return strings.Join(segments, "/")
}

// 每一条供给侧路由都必须真的走过 JWT 与审计。
//
// 这里在**审计那一层**收尾（记一笔然后 abort），于是 handler 一次也没被调用——
// 测的是中间件链，不是业务。同时这也顺带证明了审计排在 JWT 之后：
// 如果哪天有人把它挂到了 JWT 前面，下面那个 jwt 计数断言就会归零。
//
// 后台模式闸与面板限流这里**测不到**：两者拿到 nil 依赖时都直接放行，
// 在这套装配里与"根本没挂"完全同形。它们由下面的中间件链清单从源码钉住。
func TestSupplierRoutes_EveryRouteRunsAuthGuardAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	var jwtCalls, auditCalls int
	jwtAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
		jwtCalls++
		c.Next()
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
		auditCalls++
		// 599 是一个不会被任何真实分支产生的哨兵码：断言看到它，就说明请求
		// 恰好走到了这里、而且没有被更早的某一层拦下。
		c.AbortWithStatus(599)
	})

	RegisterSupplierRoutes(router.Group("/api/v1"), supplierTestHandlers(), jwtAuth, auditLog, nil, nil)

	routes := supplierRoutePaths(t, router, "/api/v1/user/supply")
	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			jwtCalls, auditCalls = 0, 0
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(route.Method, concreteRequestPath(route.Path), nil)
			router.ServeHTTP(recorder, request)

			assert.Equal(t, 599, recorder.Code, "这条路由没走到审计层")
			assert.Equal(t, 1, jwtCalls, "这条路由没走 JWT")
			assert.Equal(t, 1, auditCalls)
		})
	}
}

// 没登录时每一条都必须被挡下来。
//
// 与上一个测试的区别不是重复：那个证的是"链上挂了这几层"，这个证的是
// "拦下来的时候真的一条都没漏"。一条被挂到 `v1` 而不是 `authenticated` 组上的
// 路由，在上一个测试里会因为 jwtCalls == 0 而红，在这个测试里会因为返回 404/200
// 而红——两条独立的理由指向同一件事，是刻意的。
func TestSupplierRoutes_RejectUnauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	jwtAuth := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
		servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })

	RegisterSupplierRoutes(router.Group("/api/v1"), supplierTestHandlers(), jwtAuth, auditLog, nil, nil)

	for _, route := range supplierRoutePaths(t, router, "/api/v1/user/supply") {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(route.Method, concreteRequestPath(route.Path), nil))
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

// 管理端那十一条走的是 admin 组的四层中间件，没登录同样一条都进不去。
//
// 这条测的是"挂对了组"。挂到用户组上会让一个能看全站流水、能推进提现单的接口
// 只需要一个普通用户的 token——而它不会有任何症状，直到有人发现自己看得见别人的账。
func TestSupplyMarketRoutes_AreUnderAdminGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })

	RegisterAdminRoutes(router.Group("/api/v1"), supplierTestHandlers(), adminAuth, auditLog, stepUp, nil, nil)

	routes := supplierRoutePaths(t, router, "/api/v1/admin/supply")
	require.Len(t, routes, 11, "管理端供给侧路由数变了，改动前先读 §3.6 与 routes/supplier.go 顶部")

	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(route.Method, concreteRequestPath(route.Path), nil))
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

// 管理端供给视图的写路径**清单**——不是"必须为空"，是"只能是这两条"。
//
// §3.6 那条边界（整层只读，唯一的例外是提现审批）在后端唯一的落点就是这里。
// 前端有一条同形状的断言钉住 API 客户端的写方法清单；两边都要求加一条写接口时
// 先改断言，也就是先停下来想一下"这个写动作该不该出现在一个看板里"。
func TestSupplyMarketRoutes_WriteEndpointsAreExactlyWithdrawalApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	noop := func(c *gin.Context) { c.Next() }
	RegisterAdminRoutes(router.Group("/api/v1"), supplierTestHandlers(),
		servermiddleware.AdminAuthMiddleware(noop), servermiddleware.AuditLogMiddleware(noop),
		servermiddleware.StepUpAuthMiddleware(noop), nil, nil)

	var writes []string
	for _, route := range supplierRoutePaths(t, router, "/api/v1/admin/supply") {
		if route.Method != http.MethodGet {
			writes = append(writes, route.Method+" "+route.Path)
		}
	}
	sort.Strings(writes)

	assert.Equal(t, []string{
		"POST /api/v1/admin/supply/withdrawals/:id/paid",
		"POST /api/v1/admin/supply/withdrawals/:id/reject",
	}, writes, "运营视图新增了写接口：它会动什么？谁能撤销？先把理由写进 §3.6")
}

// 供给侧的中间件链必须与 RegisterUserRoutes **一字不差**，顺序也一样。
//
// 这条断言的价值全在"跟着上游走"上：user.go 是上游文件，哪天它给用户组加了第五层
// （比如一道新的合规闸或封禁检查），supplier.go 不会有任何编译错误、任何测试变红，
// 只会安静地少一层——而那一层大概率正是为了拦住某类用户才加的。
// 把两边的 Use 列表逐字比对，是让那次上游改动**必须**路过这个文件的唯一办法。
//
// 只能从源码读：BackendModeUserGuard(nil) 与 panelRateLimiter.Global() 拿到 nil
// 依赖时都直接 c.Next()，在装配测试里与"根本没挂"是同一个观测结果。
func TestSupplierRoutes_MiddlewareChainMatchesUserRoutes(t *testing.T) {
	pattern := regexp.MustCompile(`authenticated\.Use\((.+)\)`)
	chainOf := func(file string) []string {
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		var chain []string
		for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
			chain = append(chain, match[1])
		}
		return chain
	}

	want := []string{
		"gin.HandlerFunc(jwtAuth)",
		"middleware.BackendModeUserGuard(settingService)",
		"panelRateLimiter.Global()",
		// 审计**最后**：前面几层挡下来的请求不该在审计日志里留下一条
		// "某某访问了提现接口"，那会让日志里全是根本没发生的事。
		"gin.HandlerFunc(auditLog)",
	}
	assert.Equal(t, want, chainOf("supplier.go"))
	assert.Equal(t, chainOf("user.go"), chainOf("supplier.go"),
		"上游给用户组改了中间件链，供给侧没跟上——它不会报错，只会少一层")
}

// Heavy 限流的挂载点是一组**决定**，每一条都有理由，所以用清单钉死。
//
// 这条只能从源码读：Heavy 与 Global 都是 PanelRateLimiter 上的闭包，
// 在没有 Redis 的测试里两者都直接放行，注册结果里也分辨不出谁是谁。
// 而这些决定恰恰是最容易被"顺手加一层更安全"改掉的——
// 给撤回提现或解绑账号套上重限流，等于在供给者最急着把钱拿回来、
// 最想撤回授权的时候让他做不到。
func TestSupplierRoutes_HeavyRateLimitManifest(t *testing.T) {
	source, err := os.ReadFile("supplier.go")
	require.NoError(t, err)

	// 匹配 supply.<METHOD>("<path>"，后面跟到行尾，用于判断有没有 Heavy()。
	pattern := regexp.MustCompile(`supply\.(GET|POST|DELETE|PUT|PATCH)\("([^"]+)"([^\n]*)`)
	matches := pattern.FindAllStringSubmatch(string(source), -1)
	require.NotEmpty(t, matches)

	heavy := map[string]bool{}
	for _, match := range matches {
		// registerSupplyMarketRoutes 里的管理端路由用的是同一个 supply 变量名，
		// 但它们一条都不该套 Heavy（admin 组自己有一层 Global）。这里一并收进来，
		// 于是"给管理端某条加了 Heavy"也会被下面的清单断言逮到。
		heavy[match[1]+" "+match[2]] = strings.Contains(match[3], "panelRateLimiter.Heavy()")
	}

	var got []string
	for route, isHeavy := range heavy {
		if isHeavy {
			got = append(got, route)
		}
	}
	sort.Strings(got)

	assert.Equal(t, []string{
		// M7：中转提交会向供给者填的端点发真实探测，被脚本刷 = 平台替人压测。
		"POST /accounts/relay",
		"POST /oauth/complete",
		"POST /oauth/start",
		"POST /withdrawals",
		// 绑定收款地址：唯一一个会往带唯一索引的表里写行的供给侧接口。
		// 不限住的话，反复 PUT 别人的地址、看回的是 200 还是"已被占用"，
		// 就是一个现成的「这个地址有没有人绑过」查询器。
		"PUT /payout-wallets/:network",
	}, got, "Heavy 限流的挂载点变了——每一条的理由都写在 supplier.go 的注释里，改之前先读")

	// 这几条的反面同样重要，单独点名：它们是"把东西拿回去"的动作。
	for _, route := range []string{
		"POST /withdrawals/:id/cancel",
		"DELETE /accounts/:id",
		"POST /agreement/accept",
		"DELETE /payout-wallets/:network",
	} {
		isHeavy, registered := heavy[route]
		require.Truef(t, registered, "%s 不见了", route)
		assert.Falsef(t, isHeavy, "%s 不该套重限流，理由见 supplier.go 里紧挨着它的注释", route)
	}
}

// 收款地址绑定的**全站**路由清单——三条，全在用户自己那一侧。
//
// 这条断言横跨用户端与管理端两次注册，是因为它要钉住的那件事只有合起来看才成立：
// 改一个人的收款地址，等价于替他提现。所以管理端**一条**碰得到这张表的路由都不能有，
// 包括看起来无害的 GET——那会让「谁绑了哪个地址」变成一份可以在后台随手导出的名单。
// 支持有人代改的唯一正确形态是走客服流程改数据库，那至少留得下人工痕迹。
//
// 上面那两个循环测试只能证明"已注册的每一条都挂对了"，证明不了"该有的都在"：
// 一条被误删的路由在它们眼里等于零次循环，安静地全绿。这里用清单补上那一半。
func TestSupplierRoutes_PayoutWalletRoutesAreUserSideOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	noop := func(c *gin.Context) { c.Next() }

	RegisterSupplierRoutes(router.Group("/api/v1"), supplierTestHandlers(),
		servermiddleware.JWTAuthMiddleware(noop), servermiddleware.AuditLogMiddleware(noop), nil, nil)
	RegisterAdminRoutes(router.Group("/api/v1"), supplierTestHandlers(),
		servermiddleware.AdminAuthMiddleware(noop), servermiddleware.AuditLogMiddleware(noop),
		servermiddleware.StepUpAuthMiddleware(noop), nil, nil)

	var wallet []string
	for _, route := range router.Routes() {
		if strings.Contains(route.Path, "payout-wallet") {
			wallet = append(wallet, route.Method+" "+route.Path)
		}
	}
	sort.Strings(wallet)

	assert.Equal(t, []string{
		"DELETE /api/v1/user/supply/payout-wallets/:network",
		"GET /api/v1/user/supply/payout-wallets",
		"PUT /api/v1/user/supply/payout-wallets/:network",
	}, wallet, "收款地址路由清单变了：新增的那条谁能调？管理端一条都不该有，理由见 supplier_payout_wallet_handler.go 顶部")
}

// 地址走请求体，不走路径。
//
// PUT /payout-wallets/:network 的参数段是**链名**，不是地址。这不是风格问题：
// 路径会原样落进 access log、反代日志和浏览器历史，而一条能把某个账户的钱
// 全部取走的地址不该出现在这三个地方里的任何一个。
//
// 从注册结果读而不是从源码读：这里要证的恰好是路由树里那个参数段的**名字**，
// 而它就是注册结果本身。
func TestSupplierRoutes_PayoutWalletPathCarriesNoAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	noop := func(c *gin.Context) { c.Next() }

	RegisterSupplierRoutes(router.Group("/api/v1"), supplierTestHandlers(),
		servermiddleware.JWTAuthMiddleware(noop), servermiddleware.AuditLogMiddleware(noop), nil, nil)

	for _, route := range supplierRoutePaths(t, router, "/api/v1/user/supply/payout-wallets") {
		for _, segment := range strings.Split(route.Path, "/") {
			if !strings.HasPrefix(segment, ":") {
				continue
			}
			assert.Equalf(t, ":network", segment,
				"%s %s 的路径参数不是链名——地址一旦进了路径就会进日志", route.Method, route.Path)
		}
	}
}

// 没装配 handler 时一条路由都不注册，而不是注册一批会 nil panic 的路由。
//
// 这不是理论情况：Supplier handler 走的是 wire，而 wire 里任何一处装配失误
// 都会让它是 nil。那时正确的表现是"这些接口 404"，不是"一打就 502"。
func TestSupplierRoutes_NotRegisteredWithoutHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	noop := func(c *gin.Context) { c.Next() }

	for name, handlers := range map[string]*handler.Handlers{
		"handlers 为 nil": nil,
		"Supplier 为 nil": {},
	} {
		t.Run("用户侧/"+name, func(t *testing.T) {
			router := gin.New()
			require.NotPanics(t, func() {
				RegisterSupplierRoutes(router.Group("/api/v1"), handlers,
					servermiddleware.JWTAuthMiddleware(noop), servermiddleware.AuditLogMiddleware(noop), nil, nil)
			})
			assert.Empty(t, router.Routes(),
				"依赖没装配却把路由挂上去了：一打就是 502，而正确的表现是 404")
		})
	}

	// 管理端那半边直接调 registerSupplyMarketRoutes，不走 RegisterAdminRoutes：
	// 后者在 h.Admin 为 nil 时会自己先崩（上游行为，见 supplierTestHandlers 的注释），
	// 那样这个测试测的就是上游的容错，而不是这次加的这三道判空。
	//
	// 第三例（Supplier 在、Supplier.Admin 不在）是这三道判空里唯一不平凡的一条：
	// 管理端 handler 与用户端 handler 在 wire 里是两个独立的构造，
	// 只装配上其中一个是完全可能发生的。
	for name, handlers := range map[string]*handler.Handlers{
		"handlers 为 nil":       nil,
		"Supplier 为 nil":       {},
		"Supplier.Admin 为 nil": {Supplier: &handler.SupplierHandler{}},
	} {
		t.Run("管理端/"+name, func(t *testing.T) {
			router := gin.New()
			require.NotPanics(t, func() {
				registerSupplyMarketRoutes(router.Group("/api/v1/admin"), handlers)
			})
			assert.Empty(t, router.Routes(),
				"依赖没装配却把路由挂上去了：一打就是 502，而正确的表现是 404")
		})
	}
}
