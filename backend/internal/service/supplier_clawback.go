// APEXONE-EXT: 双边市场——退款/拒付成功后的分成追回。
//
// 这是不变量 2（「冻结窗 ≥ 拒付窗 ⇒ 已释放 = 拒付安全」）在运营上真正成立的那一半：
// 冻结窗配得再准，没有人在拒付发生时去冻结区把钱拿回来，供给者拿到的仍是消费者没付的钱。
//
// 三个刻意的选择，改动前先读：
//
//  1. **挂在进程级单例上，不进 PaymentService 的结构体**。理由与 supply_overflow_budget.go
//     里那个计数器一样：payment_service.go / payment_refund.go 是每轮 upstream sync 的合并
//     热区，为一个扩展字段每次都要解一遍构造函数的冲突。代价是需要一个注入点，
//     那个点是 ProvideSupplierCreditService（供给侧唯一一处必然被构造的地方）。
//
//  2. **best-effort，绝不让追回失败拖垮退款**。走到调用点时，钱已经在支付通道退出去了，
//     订单状态也已经收敛。此时再返回错误只有两种结局：要么退款事务回滚、订单卡在
//     refund_pending（钱退了、系统说没退），要么错误被上层吞掉——两个都比"追回没跑成"更糟。
//     所以这里只记 ERROR 日志。失败的代价是平台自吃这笔亏损，与追回功能上线前的状态一致，
//     不是新增的故障面；而 clawback 幂等，事后重跑一遍即可补上。
//
//  3. **不看总开关**。结算被关掉之后，冻结区里仍然躺着开着的时候产生的入账，
//     那些钱照样该在拒付时被追回。按 enabled 短路等于给「先关开关再退款」开一条套利路径。
package service

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// SupplierClawbackHandler 是退款路径看得到的唯一入口。
//
// 用接口而不是直接存 *SupplierClawbackService，是为了让 payment 侧的单测能塞一个假的进来，
// 不必为一条断言准备一个真仓储。
type SupplierClawbackHandler interface {
	ClawbackForRefund(ctx context.Context, consumerUserID int64, refundAmount float64, reason string) (*SupplierClawbackResult, error)
}

// supplierClawbackBox 包一层的原因与溢出计数器相同：atomic.Value 存不同动态类型会 panic，
// 装箱后存进去的永远是同一个具体类型。
type supplierClawbackBox struct {
	handler SupplierClawbackHandler
}

var supplierClawbackHolder atomic.Value

// SetSupplierClawbackHandler 装配追回入口。装配点见 ProvideSupplierClawbackHandler。
func SetSupplierClawbackHandler(handler SupplierClawbackHandler) {
	supplierClawbackHolder.Store(supplierClawbackBox{handler: handler})
}

func loadSupplierClawbackHandler() SupplierClawbackHandler {
	box, ok := supplierClawbackHolder.Load().(supplierClawbackBox)
	if !ok {
		return nil
	}
	return box.handler
}

// clawbackSupplierCreditOnRefund 是 core 退款路径上的那一行。
//
// 没装配（未注入 / 单测 / 供给侧整个没编进去）时静默返回：这个函数的唯一职责是
// 「有的话就跑一次」，它不该成为退款路径上任何一种新的失败方式。
func clawbackSupplierCreditOnRefund(ctx context.Context, consumerUserID int64, refundAmount float64, reason string) {
	handler := loadSupplierClawbackHandler()
	if handler == nil || consumerUserID <= 0 || refundAmount <= 0 {
		return
	}
	result, err := handler.ClawbackForRefund(ctx, consumerUserID, refundAmount, reason)
	if err != nil {
		// 用 ERROR 而不是 WARN：这条日志出现就意味着有一笔已经确认拒付的钱留在了供给者的
		// 冻结区里，需要人按 reason 里的订单号手工重跑。
		slog.Error("[SupplyMarket] supplier credit clawback failed",
			"consumerUserID", consumerUserID,
			"refundAmount", refundAmount,
			"reason", reason,
			"error", err)
		return
	}
	if result == nil || result.Entries == 0 {
		return
	}
	// 追回成功也留一条日志：这是事后回答「这个供给者的收益为什么少了」的起点。
	// UncoveredBasis > 0 单独提出来——它是冻结窗配短了的直接证据（见 §6 freeze_hours）。
	slog.Warn("[SupplyMarket] supplier credit clawed back",
		"consumerUserID", consumerUserID,
		"refundAmount", refundAmount,
		"reversedCredit", result.ReversedCredit,
		"reversedBasis", result.ReversedBasis,
		"uncoveredBasis", result.UncoveredBasis,
		"entries", result.Entries,
		"suppliers", result.Suppliers,
		"reason", reason)
}

// SupplierClawbackService 把仓储层的追回包成退款侧那个只有三个标量的接口。
//
// 单独起一个服务而不是把方法挂到 SupplierCreditService 上：那个服务在文件头就声明了
// 自己是只读的（写侧只在计费事务里），追回是货真价实的写，混进去会让「钱的写入口
// 全仓只有一处」这句话失效。
type SupplierClawbackService struct {
	repo SupplierCreditRepository
}

func NewSupplierClawbackService(repo SupplierCreditRepository) *SupplierClawbackService {
	return &SupplierClawbackService{repo: repo}
}

// ClawbackForRefund 追回一次退款对应的分成。
//
// refundAmount 直接当分成基数用，因为它与 accrue 侧的 BasisAmount 是同一个单位
// （都是 users.balance 的口径，见 payment_refund.go 里 BalanceToDeduct 与 u.Balance 的直接比较）。
// 刻意用 RefundAmount 而不是"实际扣掉的余额"：余额不够扣恰恰说明这笔钱已经被花出去了，
// 那正是最需要追回的情形，按实扣额算会把它算成 0。
func (s *SupplierClawbackService) ClawbackForRefund(ctx context.Context, consumerUserID int64, refundAmount float64, reason string) (*SupplierClawbackResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	if consumerUserID <= 0 || refundAmount <= 0 {
		return nil, nil
	}
	return s.repo.Clawback(ctx, SupplierClawbackParams{
		ConsumerUserID: consumerUserID,
		BasisAmount:    refundAmount,
		Reason:         reason,
	})
}
