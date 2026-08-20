// APEXONE-EXT: 双边市场——拒付（chargeback）落地。
//
// 这是不变量 2（「冻结窗 ≥ 拒付窗 ⇒ 已释放 = 拒付安全」）在运营上成立的另一半。
// supplier_clawback.go 那半挂在**我们主动发起的退款**上；这半挂在**别人替我们
// 发起的退款**上——持卡人绕过我们直接找发卡行撤销交易，我们不发起任何请求、
// 也收不到任何一个 payment_intent 事件。在这个文件出现之前，那条路径上什么都不发生。
//
// # 四个刻意的选择，改动前先读
//
//  1. **挂在 PaymentService 上，而不是另起一个服务**。这条路径要做的三件事
//     （扣回消费者余额、追回供给者分成、写订单审计日志）需要的全部依赖
//     ——userRepo、entClient、追回单例——都已经在 PaymentService 上了。
//     另起一个服务意味着把这三样再注入一遍，且 wire 里多一个必须被消费否则
//     会被剪掉的节点（#5a/#5c 那个坑）。代价是 PaymentService 多几个方法，
//     但它们全在这个独立文件里，不进 core 的合并热区。
//
//  2. **副作用只挂在 open 上，不等 closed**。Stripe 在创建争议的同时就把款项
//     从我们账上扣走；等争议关闭要 60–90 天，那时冻结区里对应的分成早已解冻、
//     甚至已经提现出去。「等结果出来再说」听起来稳妥，实际是保证追不回来。
//
//  3. **赢了不自动回补**。争议判我们赢时，钱回到我们账上，但已经扣掉的
//     消费者余额与已经追回的供给分成**不自动还回去**。回补消费者余额是一行加法，
//     回补供给者的分成不是：追回撤销的是一条条带各自冻结到期时间的入账，
//     「重新入账」是一条全新的写路径，跑两遍就是凭空造钱。而只补消费者、
//     不补供给者，等于让干活的那一方独自承担一次判错——比两边都不补更糟。
//     所以这里只记录 + 通知运营，把两个数字算好摆在邮件里，由人去补。
//     这条是 §6 里的待定策略之一。
//
//  4. **失败不让 webhook 报错**。返回 error 会让 handler 回 500，Stripe 于是
//     重投——而重投打到的是同一个已经 claim 过的争议，副作用不会再跑一遍
//     （settled_at 挡着），重投唯一的效果是刷日志。所以这条路径上的错误
//     一律记 ERROR 日志后咽掉，webhook 照常回 200。真正需要人介入的信息
//     在日志和运营邮件里，不在 HTTP 状态码里。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

// PaymentDisputeRecord 是 payment_disputes 表里的一行。
type PaymentDisputeRecord struct {
	ID          int64
	DisputeID   string
	ProviderKey string
	TradeNo     string
	OrderID     *int64
	OutTradeNo  string
	UserID      *int64
	Status      string
	Reason      string
	// SettledAt 非空表示追回与扣回已经执行过。它是幂等闸。
	SettledAt  *time.Time
	ResolvedAt *time.Time
}

// PaymentDisputeUpsert 是一次争议推送落库时携带的全部字段。
type PaymentDisputeUpsert struct {
	DisputeID     string
	ProviderKey   string
	TradeNo       string
	OrderID       *int64
	OutTradeNo    string
	UserID        *int64
	Status        string
	Reason        string
	DisputeAmount float64
	Currency      string
	BasisAmount   float64
	RawData       string
}

// PaymentDisputeSettlement 记录一次副作用执行的结果，用于对账。
type PaymentDisputeSettlement struct {
	DisputeID       string
	BalanceDeducted float64
	ClawedCredit    float64
	ClawedBasis     float64
	UncoveredBasis  float64
}

// PaymentDisputeStore 是争议台账的持久化接口。
//
// 拆成 Upsert / Claim / RecordSettlement 三步而不是一个大方法：中间那步是
// **原子占坑**，它的返回值决定副作用跑不跑。把三步合成一个方法，占坑就只能在
// 实现内部完成，service 侧的单测再也测不到「两次推送只扣一次」这条最要紧的性质。
type PaymentDisputeStore interface {
	// Upsert 按 dispute_id 插入或更新一行，返回落库后的记录。
	// 已存在时只更新会变的那几列（状态、原因、报文、resolved_at），
	// 绝不覆盖 settled_at 与四个结算金额——那些是第一次执行时写死的事实。
	Upsert(ctx context.Context, params PaymentDisputeUpsert) (*PaymentDisputeRecord, error)
	// ClaimForSettlement 原子地把 settled_at 从 NULL 改成 NOW()。
	// 返回 false 表示已经有人占过坑（重复推送 / 并发投递），调用方必须跳过副作用。
	ClaimForSettlement(ctx context.Context, disputeID string) (bool, error)
	// RecordSettlement 回填副作用的四个金额。失败只影响对账，不影响正确性。
	RecordSettlement(ctx context.Context, settlement PaymentDisputeSettlement) error
}

// --- 进程级单例装配（理由同 supplier_clawback.go）---

type paymentDisputeStoreBox struct {
	store PaymentDisputeStore
}

var paymentDisputeStoreHolder atomic.Value

// SetPaymentDisputeStore 装配争议台账。
// 装配点在 repository.ProvideSupplierCreditRepository——只能在那一侧，因为
// service 不能 import repository；理由与选那个宿主的原因都写在那个函数上。
func SetPaymentDisputeStore(store PaymentDisputeStore) {
	paymentDisputeStoreHolder.Store(paymentDisputeStoreBox{store: store})
}

func loadPaymentDisputeStore() PaymentDisputeStore {
	box, ok := paymentDisputeStoreHolder.Load().(paymentDisputeStoreBox)
	if !ok {
		return nil
	}
	return box.store
}

// disputeClawbackReason 是争议追回写进流水 remark 的唯一格式。
// 与 refundClawbackReason 同样的理由：格式漂了运营就搜不到。
// 刻意带上 dispute id 而不只是订单号——同一个订单可能被拒付一次又退款一次，
// remark 必须能区分这两笔。
func disputeClawbackReason(disputeID string, orderID int64) string {
	if orderID > 0 {
		return fmt.Sprintf("dispute %s order:%d", disputeID, orderID)
	}
	return fmt.Sprintf("dispute %s", disputeID)
}

// HandleDisputeNotification 处理一个已验签的争议事件。
//
// 永远返回 nil 之外的东西只有一种情况：完全没装配台账（单测 / 供给侧没编进去）。
// 见文件头第 4 点——这条路径上的错误不该变成 webhook 的 500。
func (s *PaymentService) HandleDisputeNotification(ctx context.Context, n *payment.DisputeNotification, providerKey string) {
	if s == nil || n == nil {
		return
	}
	disputeID := strings.TrimSpace(n.DisputeID)
	if disputeID == "" {
		// provider 侧已经挡过一次，这里是第二道：没有幂等键就不该产生副作用。
		slog.Error("[Payment Dispute] dispute event without id, ignored", "provider", providerKey)
		return
	}
	store := loadPaymentDisputeStore()
	if store == nil {
		// 台账没装配时**仍然留一条日志**：这是「拒付发生过」的唯一痕迹，
		// 比静默返回值钱得多。
		slog.Error("[Payment Dispute] dispute store not wired, dispute recorded nowhere",
			"provider", providerKey, "disputeID", disputeID, "status", n.Status,
			"tradeNo", n.TradeNo, "amount", n.DisputeAmount, "currency", n.Currency)
		return
	}

	order := s.findDisputedOrder(ctx, n)
	basis := disputeBasisAmount(order, n)

	record, err := store.Upsert(ctx, buildDisputeUpsert(n, providerKey, order, basis))
	if err != nil {
		slog.Error("[Payment Dispute] persist dispute failed",
			"provider", providerKey, "disputeID", disputeID, "status", n.Status, "error", err)
		return
	}

	if order == nil {
		// 对不上订单：同一个 Stripe 账户被多套环境共用时会推来别人的争议。
		// 行已经落库了，剩下的只能靠人。用 WARN 而不是 ERROR——它未必是故障。
		slog.Warn("[Payment Dispute] dispute has no matching order",
			"provider", providerKey, "disputeID", disputeID, "status", n.Status,
			"tradeNo", n.TradeNo, "outTradeNo", n.OrderID)
		s.notifyDispute(n, providerKey, order, basis, nil)
		return
	}

	s.writeAuditLog(ctx, order.ID, disputeAuditAction(n.Status), "system", map[string]any{
		"disputeID":     disputeID,
		"provider":      providerKey,
		"status":        n.Status,
		"reason":        n.Reason,
		"disputeAmount": n.DisputeAmount,
		"currency":      n.Currency,
		"basisAmount":   basis,
	})

	switch n.Status {
	case payment.DisputeStatusOpen:
		s.applyDisputeEffects(ctx, store, n, providerKey, order, basis, record)
	case payment.DisputeStatusWon, payment.DisputeStatusLost:
		// 见文件头第 3 点：赢了也不自动回补，只把该补的数字摆到运营面前。
		s.notifyDispute(n, providerKey, order, basis, nil)
	}
}

// applyDisputeEffects 执行一次拒付的两个副作用，全程幂等。
func (s *PaymentService) applyDisputeEffects(
	ctx context.Context,
	store PaymentDisputeStore,
	n *payment.DisputeNotification,
	providerKey string,
	order *dbent.PaymentOrder,
	basis float64,
	record *PaymentDisputeRecord,
) {
	if record != nil && record.SettledAt != nil {
		// 重复推送的常态路径，连一次 UPDATE 都省掉。
		return
	}
	claimed, err := store.ClaimForSettlement(ctx, n.DisputeID)
	if err != nil {
		slog.Error("[Payment Dispute] claim settlement failed",
			"disputeID", n.DisputeID, "orderID", order.ID, "error", err)
		return
	}
	if !claimed {
		return
	}

	settlement := PaymentDisputeSettlement{DisputeID: n.DisputeID}

	// 一、扣回消费者手上那笔他其实没付的钱。
	//
	// 只对余额订单做。订阅订单要撤的是天数，那件事在退款路径上需要
	// GetActiveSubscription + ExtendSubscription/Revoke 一整套，且有「订阅已经过期
	// 撤不动」的分支——在一条不能失败的 webhook 上跑那套，失败时既不能重试也不能
	// 回滚。订阅拒付因此走人工，邮件里写清楚。
	if order.OrderType == payment.OrderTypeBalance && basis > 0 {
		deducted, deductErr := s.deductAvailableBalance(ctx, order.UserID, basis)
		if deductErr != nil {
			slog.Error("[Payment Dispute] deduct consumer balance failed",
				"disputeID", n.DisputeID, "orderID", order.ID, "userID", order.UserID,
				"basis", basis, "error", deductErr)
		} else {
			settlement.BalanceDeducted = deducted
			if deducted+paymentAmountToleranceForCurrency(payment.DefaultPaymentCurrency) < basis {
				// 扣不满 = 他已经把钱花出去了。这正是拒付欺诈得手的形态，
				// 差额是平台的净亏损，必须能被看见。
				slog.Warn("[Payment Dispute] consumer balance short of disputed basis",
					"disputeID", n.DisputeID, "orderID", order.ID, "userID", order.UserID,
					"basis", basis, "deducted", deducted)
			}
		}
	}

	// 二、追回供给者冻结区里那笔由这个消费者产生的分成。
	//
	// 复用退款侧同一个单例入口：两条路径追回的是同一批入账，算法必须逐字相同。
	if handler := loadSupplierClawbackHandler(); handler != nil && basis > 0 {
		result, clawErr := handler.ClawbackForRefund(ctx, order.UserID, basis, disputeClawbackReason(n.DisputeID, order.ID))
		if clawErr != nil {
			slog.Error("[Payment Dispute] supplier credit clawback failed",
				"disputeID", n.DisputeID, "orderID", order.ID, "userID", order.UserID,
				"basis", basis, "error", clawErr)
		} else if result != nil {
			settlement.ClawedCredit = result.ReversedCredit
			settlement.ClawedBasis = result.ReversedBasis
			settlement.UncoveredBasis = result.UncoveredBasis
			if result.UncoveredBasis > 0 {
				// 冻结窗配短了的直接证据（§6 freeze_hours）。
				slog.Warn("[Payment Dispute] disputed basis not fully recoverable from frozen credit",
					"disputeID", n.DisputeID, "orderID", order.ID,
					"basis", basis, "reversedBasis", result.ReversedBasis,
					"uncoveredBasis", result.UncoveredBasis)
			}
		}
	}

	if err := store.RecordSettlement(ctx, settlement); err != nil {
		// 只影响对账：钱已经扣了、追了，数字没回填而已。
		slog.Error("[Payment Dispute] record settlement amounts failed",
			"disputeID", n.DisputeID, "orderID", order.ID, "error", err)
	}

	slog.Warn("[Payment Dispute] chargeback settled",
		"provider", providerKey, "disputeID", n.DisputeID, "orderID", order.ID,
		"userID", order.UserID, "orderType", order.OrderType, "reason", n.Reason,
		"basis", basis, "balanceDeducted", settlement.BalanceDeducted,
		"clawedCredit", settlement.ClawedCredit, "uncoveredBasis", settlement.UncoveredBasis)

	s.notifyDispute(n, providerKey, order, basis, &settlement)
}

// notifyDispute 把一次争议摆到运营面前。没装配通知出口时静默返回——
// 与追回单例同样的理由：通知不该成为这条路径上新的失败方式。
func (s *PaymentService) notifyDispute(
	n *payment.DisputeNotification,
	providerKey string,
	order *dbent.PaymentOrder,
	basis float64,
	settlement *PaymentDisputeSettlement,
) {
	notifier := loadPaymentDisputeNotifier()
	if notifier == nil || n == nil {
		return
	}
	notice := PaymentDisputeNotice{
		DisputeID:     strings.TrimSpace(n.DisputeID),
		ProviderKey:   providerKey,
		Status:        n.Status,
		Reason:        n.Reason,
		Currency:      n.Currency,
		DisputeAmount: n.DisputeAmount,
		OutTradeNo:    strings.TrimSpace(n.OrderID),
		BasisAmount:   basis,
		Settlement:    settlement,
	}
	if order != nil {
		notice.OrderID = order.ID
		notice.UserID = order.UserID
		notice.UserEmail = order.UserEmail
		notice.OrderType = order.OrderType
		if notice.OutTradeNo == "" {
			notice.OutTradeNo = order.OutTradeNo
		}
	}
	notifier.NotifyDispute(notice)
}

// findDisputedOrder 找出被争议的订单。
//
// 先按 payment_trade_no（支付意图 id）查——那是 Stripe 争议对象上唯一确定带着的
// 我方标识；OrderID 只在报文恰好展开了 payment_intent 时才有值，作兜底。
// 两条都查不到不是错误，见 HandleDisputeNotification 里的 order == nil 分支。
func (s *PaymentService) findDisputedOrder(ctx context.Context, n *payment.DisputeNotification) *dbent.PaymentOrder {
	if s == nil || s.entClient == nil || n == nil {
		return nil
	}
	if tradeNo := strings.TrimSpace(n.TradeNo); tradeNo != "" {
		orders, err := s.entClient.PaymentOrder.Query().
			Where(paymentorder.PaymentTradeNoEQ(tradeNo)).
			Order(dbent.Desc(paymentorder.FieldID)).
			Limit(1).
			All(ctx)
		if err == nil && len(orders) == 1 {
			return orders[0]
		}
	}
	if outTradeNo := strings.TrimSpace(n.OrderID); outTradeNo != "" {
		orders, err := s.entClient.PaymentOrder.Query().
			Where(paymentorder.OutTradeNoEQ(outTradeNo)).
			Limit(1).
			All(ctx)
		if err == nil && len(orders) == 1 {
			return orders[0]
		}
	}
	return nil
}

// disputeBasisAmount 把通道币种的争议金额换算成 users.balance 口径的基数。
//
// 追回与扣回都按这个数走，所以换算错的代价是直接的钱算错。三条规则：
//
//   - 对不上订单、或订单金额不可用时，返回 0——**宁可不追，不可乱追**。
//     零基数会让两个副作用都空转，留下的是一条 uncovered 的日志和一封给运营的信。
//   - 争议金额与订单实付额相等（常态：整笔被拒）时直接取订单金额，
//     不走乘除，避免浮点在最常见的那条路径上引入尾差。
//   - 部分争议按比例折算，且**夹在订单金额以内**——通道侧的部分退款、
//     汇率浮动都可能让争议金额略大于实付额，那不该变成一次超额追回。
func disputeBasisAmount(order *dbent.PaymentOrder, n *payment.DisputeNotification) float64 {
	if order == nil || n == nil {
		return 0
	}
	if order.Amount <= 0 {
		return 0
	}
	disputed := n.DisputeAmount
	payAmount := order.PayAmount
	if disputed <= 0 || payAmount <= 0 {
		// 拿不到可比的通道金额时按整笔算：争议对象默认针对整笔扣款，
		// 而这里的"整笔"有订单自己的 Amount 作准，不涉及任何换算。
		return order.Amount
	}
	currency := PaymentOrderCurrency(order)
	if math.Abs(disputed-payAmount) <= paymentAmountToleranceForCurrency(currency) {
		return order.Amount
	}
	basis := decimal.NewFromFloat(order.Amount).
		Mul(decimal.NewFromFloat(disputed)).
		Div(decimal.NewFromFloat(payAmount)).
		Round(2).
		InexactFloat64()
	if basis > order.Amount {
		return order.Amount
	}
	if basis < 0 {
		return 0
	}
	return basis
}

func buildDisputeUpsert(n *payment.DisputeNotification, providerKey string, order *dbent.PaymentOrder, basis float64) PaymentDisputeUpsert {
	params := PaymentDisputeUpsert{
		DisputeID:     strings.TrimSpace(n.DisputeID),
		ProviderKey:   providerKey,
		TradeNo:       strings.TrimSpace(n.TradeNo),
		OutTradeNo:    strings.TrimSpace(n.OrderID),
		Status:        n.Status,
		Reason:        n.Reason,
		DisputeAmount: n.DisputeAmount,
		Currency:      n.Currency,
		BasisAmount:   basis,
		RawData:       n.RawData,
	}
	if order != nil {
		orderID := order.ID
		userID := order.UserID
		params.OrderID = &orderID
		params.UserID = &userID
		if params.OutTradeNo == "" {
			params.OutTradeNo = order.OutTradeNo
		}
	}
	return params
}

// disputeAuditAction 是订单审计日志里的动作名。
// 三个状态各一个动作而不是统一一个 DISPUTE：审计日志是按 action 筛的，
// 合成一个就意味着「这个订单有没有被判输」要靠读 detail 的 JSON 才能回答。
func disputeAuditAction(status string) string {
	switch status {
	case payment.DisputeStatusWon:
		return "DISPUTE_WON"
	case payment.DisputeStatusLost:
		return "DISPUTE_LOST"
	default:
		return "DISPUTE_OPENED"
	}
}
