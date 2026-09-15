// APEXONE-EXT: 需要 I/O 的那几道调度闸的统一入口。
//
// # 为什么要有这个文件
//
// gateway_scheduling.go 里的候选过滤有两类判据：一类是纯函数（平台匹配、模型支持、
// 配额是否耗尽），另一类要读用量——窗口费用、RPM，以及本次新加的供给者每日上限。
// 后者原本各自散落在 **12 处**调用点上，其中 4 处埋在三百多字符的单行 `&&` 链里。
//
// 那种形状下「新增一道用量闸」是一次 12 处的散点修改，漏掉一处不会报错、不会有测试
// 变红，只会让某条调度路径上的上限永远不生效——而那正是最难被发现的一类 bug。
//
// 所以把它们收成一个入口。新增第四道闸时只改这一个函数，12 处调用点自动全覆盖。
//
// # 为什么改名
//
// 引入这个入口的那次提交把 isAccountSchedulableForWindowCost / ...ForRPM 改成了
// checkWindowCostGate / checkRPMGate。改名本身不是审美——它让所有旧调用点**编译失败**，
// 于是「怎么保证一处都不漏」的答案从「记得改」变成「漏了就编译不过」。
//
// # 顺序即短路顺序
//
// windowCost → RPM → 每日上限，与改名前的求值顺序逐字节一致。每日上限放最后，且它的
// 第一件事就是 HasSupplyDailyCap() 早返回，所以没有任何供给者设过上限的部署里，
// 这个文件的净效果是零——不多一次查询、不多一次缓存读。
package service

import "context"

// 闸门原因串。dynamicLimitGate 用它告诉调用方是哪一道拦下的，
// 因为有两处调用点要靠这个区分来写日志/计数，只回一个 bool 不够。
const (
	dynamicLimitGateWindowCost = "window_cost"
	dynamicLimitGateRPM        = "rpm"
	// dynamicLimitGateSupplyDailyCap 供给者自设的每日共享上限。
	dynamicLimitGateSupplyDailyCap = "supply_daily_cap"
)

// dynamicLimitGate 依次跑所有「需要读用量」的调度闸，返回被哪一道拦下（"" = 全部通过）。
//
// 返回原因串而不是 bool：调用点里有两处要区分是谁拦的——一处维护
// filteredWindowCost 计数器，一处要在 [StickyCacheMiss] 日志里写明
// "gate_check" 还是 "rpm_red"。那两个字符串可能被监控面板依赖，必须原样保留。
func (s *GatewayService) dynamicLimitGate(ctx context.Context, account *Account, isSticky bool) string {
	if !s.checkWindowCostGate(ctx, account, isSticky) {
		return dynamicLimitGateWindowCost
	}
	if !s.checkRPMGate(ctx, account, isSticky) {
		return dynamicLimitGateRPM
	}
	if !s.isAccountSchedulableForSupplyDailyCap(ctx, account, isSticky) {
		return dynamicLimitGateSupplyDailyCap
	}
	return ""
}

// isAccountSchedulableForDynamicLimits 是 dynamicLimitGate 的布尔版，
// 给那 10 处不关心「被谁拦下」的调用点用。
func (s *GatewayService) isAccountSchedulableForDynamicLimits(ctx context.Context, account *Account, isSticky bool) bool {
	return s.dynamicLimitGate(ctx, account, isSticky) == ""
}

// withSchedulingPrefetch 一次性预取本轮候选集需要的全部用量数据。
//
// 与 dynamicLimitGate 一一对应：那里每加一道闸，这里就加一次预取，否则那道闸会退化成
// 每个候选账号一次单点查询。退化不会给出错误答案，只会让延迟变差且没人归因得到这里——
// 所以两者必须一起改，这也是把它们放进同一个文件的原因。
func (s *GatewayService) withSchedulingPrefetch(ctx context.Context, accounts []Account) context.Context {
	ctx = s.prefetchWindowCost(ctx, accounts)
	ctx = s.prefetchRPM(ctx, accounts)
	ctx = s.withSupplyDailyCapPrefetch(ctx, accounts)
	return ctx
}
