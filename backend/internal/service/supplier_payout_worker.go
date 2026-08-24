// APEXONE-EXT: 双边市场——链上打款 worker（M4）。
//
// M3 建单时把四项快照钉在了单子上，这里是把钱真正发出去的那一段。每张单子走
// 一台五步状态机：捞单（租约）→ 预留 nonce（翻 processing）→ 广播 → 记哈希 →
// 等确认（paid / failed）。任何一步的答案是「还不知道」，就交还租约留给下一轮——
// **绝不在不确定的时候做不可逆的事**。
//
// # 这个文件里唯一重要的排序
//
// nonce 必须在广播**之前**落库（BeginPayout），哈希必须在广播**之后**尽快落库
// （RecordPayoutTx）。前者错了：广播响应丢失时重试会向节点要一个新 nonce，
// 同一张单子打两次款；后者错了只是多一轮重播——同一个 nonce 链上最多认一笔，
// 重播的代价是零。两个写库动作的失败处理因此完全不同：nonce 落不下去就**不许广播**，
// 哈希落不下去可以留给重播。
//
// # 为什么顺序处理、不并发
//
// 同一个金库地址的多笔交易靠 nonce 排队，并发广播要么自己管一段 nonce 区间
// （错一个全堵住），要么互相撞。顺序处理慢，但提现的时效承诺是「几分钟」，
// 一轮 20 张 × 每张几秒的广播完全够——批量合约（M5）才是吞吐的正解，不是并发。
//
// # 放弃等确认的期限
//
// 确认查询可能永远等不到结果：广播传输失败后的重播会重签，gasPrice 变了哈希
// 就变了，而真正上链的可能是上一次签的那笔——我们等的哈希永远不会出现。
// 没有期限，这张单子在 processing 里无限转圈，而管理端对 processing 刻意无权
// 处置（防双付）。所以从第一次广播（broadcasted_at）起过了期限还没有答案，
// worker 把单子标成 failed、在 last_error 里写明「结果不明，退款前先查链」，
// 连同 tx_hash 一起交还给人。这不是把不确定变成确定，是把不确定交给唯一
// 有资格处置它的角色。
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// supplierPayoutLeaderLockKey 多实例下只让一个实例打款。
	//
	// 这里选主不是优化而是正确性的一部分：两个实例同时处理同一张单子会被
	// 租约挡住，但两个实例同时处理**不同**单子会各自向节点要 nonce、拿到
	// 同一个号——后广播的那笔被拒，单子多空转一轮。
	supplierPayoutLeaderLockKey = "supplier:payout:leader"
	supplierPayoutLeaderLockTTL = 5 * time.Minute
	// supplierPayoutRunTimeout 单轮预算。小于租约：一轮跑不完的单子还握着
	// 租约，下一轮（还是这个实例，或接班的实例）续上，不会被并发处理。
	supplierPayoutRunTimeout = 4 * time.Minute
	// SupplierPayoutDefaultInterval 扫描间隔。提现的时效承诺是「几分钟」，
	// 30 秒的空扫在部分索引上只是一次探测，代价可以忽略。
	SupplierPayoutDefaultInterval = 30 * time.Second
	// supplierPayoutBatchLimit 单轮最多捞多少张。顺序处理，这个数就是
	// 单轮时长的上界系数；剩下的下一轮继续。
	supplierPayoutBatchLimit = 20
	// supplierPayoutLease 租约时长。必须显著大于单轮预算：租约在单轮之内
	// 到期的话，同一张单子会在处理中途被下一轮重新捞走。
	supplierPayoutLease = 10 * time.Minute
	// supplierPayoutRetryDelay 「还不知道」之后的退避。太短会对着一个抖动的
	// 节点空转，太长会把「几分钟到账」拖成半小时。
	supplierPayoutRetryDelay = 2 * time.Minute
	// supplierPayoutDisabledRetryDelay 链上客户端没配好时的退避。这不是抖动
	// 而是配置状态，修好之前重试全是徒劳——拉长间隔，让日志可读。
	supplierPayoutDisabledRetryDelay = 15 * time.Minute
	// supplierPayoutConfirmWait 单张单子等确认的预算。BSC 出块 3 秒，正常
	// 确认在 15 秒内；等到 45 秒还没有就交还租约，别让一张慢单拖住整轮。
	supplierPayoutConfirmWait = 45 * time.Second
	// supplierPayoutGiveUpAfter 从第一次广播起，超过这个时长还没有确认答案
	// 就放弃、转给运营（见文件头）。BSC 上一笔既没上链也没被驱逐的交易
	// 撑不过几分钟，30 分钟已经是「这条查询路径永远不会有答案」的强信号。
	supplierPayoutGiveUpAfter = 30 * time.Minute
)

// SupplierPayoutWorker 周期性把链上提现单的钱发出去。
type SupplierPayoutWorker struct {
	repo     SupplierPayoutQueueRepository
	chain    SupplierChainClient
	notifier *SupplierWithdrawalNotifier

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

// NewSupplierPayoutWorker 构造打款 worker。notifier 可为 nil（不发信，照常打款）。
func NewSupplierPayoutWorker(
	repo SupplierPayoutQueueRepository,
	chain SupplierChainClient,
	notifier *SupplierWithdrawalNotifier,
	interval time.Duration,
) *SupplierPayoutWorker {
	if interval <= 0 {
		interval = SupplierPayoutDefaultInterval
	}
	return &SupplierPayoutWorker{
		repo:       repo,
		chain:      chain,
		notifier:   notifier,
		interval:   interval,
		stopCh:     make(chan struct{}),
		instanceID: uuid.NewString(),
	}
}

// SetLeaderLock 注入选主用的缓存与数据库。两者都为 nil 时不选主直接跑
// （单实例部署与测试的行为）。
func (s *SupplierPayoutWorker) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *SupplierPayoutWorker) Start() {
	// chain 为 nil 不启动：这不是防御性检查，是一类真实部署（wire 之外的
	// 测试装配）。注意 DisabledChainClient **不是** nil——那种部署照常启动，
	// 每张单子在 NextNonce 上收到明确的「没配置」，带长退避地留在 pending，
	// 管理端随时可以人工处理它们。
	if s == nil || s.repo == nil || s.chain == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// 启动即跑一次：进程重启后不必等满一个 interval 才接上停机前的半程单。
		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *SupplierPayoutWorker) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SupplierPayoutWorker) runOnce() {
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, supplierPayoutLeaderLockKey, s.instanceID, supplierPayoutLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	// 独立于任何请求 context：这是后台任务，不该被别人的取消牵连。
	runCtx, cancel := context.WithTimeout(context.Background(), supplierPayoutRunTimeout)
	defer cancel()
	s.RunOnce(runCtx)
}

// RunOnce 处理一轮。导出给测试直接调（绕过 ticker 与选主）。
func (s *SupplierPayoutWorker) RunOnce(ctx context.Context) {
	if s == nil || s.repo == nil || s.chain == nil {
		return
	}
	due, err := s.repo.ClaimPayoutDue(ctx, supplierPayoutBatchLimit, supplierPayoutLease)
	if err != nil {
		slog.Error("[SupplierPayout] failed to claim due withdrawals", "error", err)
		return
	}
	for i := range due {
		if ctx.Err() != nil {
			// 单轮预算用完。没处理到的单子还握着租约，租约到期后自然回队；
			// 刻意不逐张去释放——那要再打 N 次库，而这里已经没有预算了。
			return
		}
		s.settleOne(ctx, &due[i])
	}
}

// settleOne 推进一张单子，走到哪算哪。
func (s *SupplierPayoutWorker) settleOne(ctx context.Context, w *SupplierWithdrawal) {
	// M3 保证四列写就写全；这里兜的是「有人手改库」。缺列的单子推不动也
	// 修不好，直接转给运营，别让它每 30 秒空转一次。
	if w.Network == nil || w.TokenAddress == nil {
		s.finishFailed(ctx, w, "chain snapshot incomplete: network/token_address missing")
		return
	}
	net := w.NetAmount()
	if net <= 0 {
		// 建单时挡过 fee >= amount，走到这里同样只能是数据被改过。
		s.finishFailed(ctx, w, fmt.Sprintf("net amount %.8f is not positive", net))
		return
	}

	// 已有哈希：这张单子只欠一个链上的答案。
	if w.TxHash != nil {
		s.confirm(ctx, w, *w.TxHash)
		return
	}

	// nonce：老单子复用落库的那个，新单子向节点要。顺序不能反——
	// 落库的 nonce 意味着可能已有一笔交易占着它，换号就是双付。
	var nonce uint64
	if w.ChainNonce != nil {
		nonce = uint64(*w.ChainNonce)
	} else {
		fresh, err := s.chain.NextNonce(ctx, *w.Network)
		if err != nil {
			s.release(w, fmt.Sprintf("next nonce: %v", err), retryDelayFor(err))
			return
		}
		nonce = fresh
	}

	// 翻 processing + 钉 nonce，广播前的最后一道闸。落不下去有两种含义，
	// 应对相同——都不许广播：条件没命中（管理端在租约过期后已处理掉这张单子），
	// 或数据库不可达（nonce 没有钉住，广播出去就没法安全重试）。
	if err := s.repo.BeginPayout(ctx, w.ID, nonce); err != nil {
		if errors.Is(err, ErrSupplierWithdrawalNotPending) {
			slog.Info("[SupplierPayout] withdrawal resolved elsewhere before broadcast", "withdrawal_id", w.ID)
			return
		}
		s.release(w, fmt.Sprintf("persist nonce: %v", err), supplierPayoutRetryDelay)
		return
	}

	result, err := s.chain.Transfer(ctx, ChainTransferParams{
		Network: *w.Network,
		// 合约地址用**单子上的快照**，不再问客户端：配置在建单之后换过币的话，
		// 该按建单那一刻的约定发——那才是供给者看到并同意过的东西。
		Token:  *w.TokenAddress,
		To:     w.PayoutAccount,
		Amount: net,
		Nonce:  &nonce,
	})
	if err != nil {
		// 广播失败但 nonce 已钉死：留在 processing，下一轮用同一个 nonce 重播。
		// 万一这次其实播出去了，重播会收到 already known / nonce too low，
		// 客户端把那当成功回哈希（§9.5），照常走到确认。
		s.release(w, fmt.Sprintf("broadcast: %v", err), retryDelayFor(err))
		return
	}
	if err := s.repo.RecordPayoutTx(ctx, w.ID, result.TxHash); err != nil {
		// 哈希没记下来不致命（重播代价为零），但这一轮不能继续往确认走：
		// 确认成功会写终态，而一张没有 tx_hash 的 paid 单在对账里是个洞。
		s.release(w, fmt.Sprintf("record tx hash: %v", err), supplierPayoutRetryDelay)
		return
	}
	if w.BroadcastedAt == nil {
		now := time.Now()
		w.BroadcastedAt = &now
	}
	s.confirm(ctx, w, result.TxHash)
}

// confirm 等一笔交易的终态，等不到就交还租约。
func (s *SupplierPayoutWorker) confirm(ctx context.Context, w *SupplierWithdrawal, txHash string) {
	waitCtx, cancel := context.WithTimeout(ctx, supplierPayoutConfirmWait)
	confirmation, err := s.chain.WaitForConfirmation(waitCtx, *w.Network, txHash)
	cancel()
	if err != nil {
		// 「还不知道」。先看是不是已经等了太久——期限一到就转给运营，
		// 期限没到就退避重试。
		if w.BroadcastedAt != nil && time.Since(*w.BroadcastedAt) > supplierPayoutGiveUpAfter {
			s.finishFailed(ctx, w, fmt.Sprintf(
				"confirmation still unknown %s after first broadcast (tx %s) — VERIFY ON-CHAIN before refunding",
				supplierPayoutGiveUpAfter, txHash))
			return
		}
		s.release(w, fmt.Sprintf("confirm %s: %v", txHash, err), supplierPayoutRetryDelay)
		return
	}
	switch confirmation.Status {
	case ChainTxConfirmed:
		paid, err := s.repo.FinishPayout(ctx, SupplierPayoutFinishParams{
			ID: w.ID, Status: SupplierWithdrawalStatusPaid, TxHash: txHash,
		})
		if err != nil {
			// 链上已成、库里没写上。下一轮会拿着同一个哈希再确认一次并重写——
			// FinishPayout 的条件更新保证第二次写不会叠加任何东西。
			s.release(w, fmt.Sprintf("finish paid: %v", err), supplierPayoutRetryDelay)
			return
		}
		slog.Info("[SupplierPayout] withdrawal paid on-chain",
			"withdrawal_id", w.ID, "tx_hash", txHash, "net", w.NetAmount())
		if s.notifier != nil {
			s.notifier.NotifyResolved(paid)
		}
	case ChainTxFailed:
		// 链上明确 revert：钱确定没出去，但**不自动退款**（见状态常量的注释）。
		s.finishFailed(ctx, w, fmt.Sprintf("transaction reverted on-chain (tx %s): %s", txHash, confirmation.Reason))
	default:
		// 客户端违反了自己的约定。当成「还不知道」处理——保守的那一边。
		s.release(w, fmt.Sprintf("confirm %s: unexpected status %q", txHash, confirmation.Status), supplierPayoutRetryDelay)
	}
}

// finishFailed 把单子停靠到 failed 并叫运营来看。
func (s *SupplierPayoutWorker) finishFailed(ctx context.Context, w *SupplierWithdrawal, reason string) {
	failed, err := s.repo.FinishPayout(ctx, SupplierPayoutFinishParams{
		ID: w.ID, Status: SupplierWithdrawalStatusFailed, Reason: reason,
	})
	if err != nil {
		s.release(w, fmt.Sprintf("finish failed (%s): %v", reason, err), supplierPayoutRetryDelay)
		return
	}
	slog.Error("[SupplierPayout] withdrawal needs operator attention",
		"withdrawal_id", w.ID, "reason", reason)
	if s.notifier != nil {
		s.notifier.NotifyPayoutFailed(failed)
	}
}

// release 交还租约。释放失败只记日志：租约反正会自己到期，这里再重试
// 只是把一次失败变成两次。
func (s *SupplierPayoutWorker) release(w *SupplierWithdrawal, lastError string, retryAfter time.Duration) {
	// 用后台 ctx 而不是本轮的：走到这里往往正是因为本轮预算耗尽或外部超时，
	// 一个已经取消的 ctx 会让这次释放必然失败。
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.ReleasePayoutLease(releaseCtx, w.ID, lastError, retryAfter); err != nil {
		slog.Error("[SupplierPayout] failed to release lease", "withdrawal_id", w.ID, "error", err)
	}
	slog.Warn("[SupplierPayout] withdrawal deferred", "withdrawal_id", w.ID, "reason", lastError, "retry_after", retryAfter)
}

// retryDelayFor 按错误的性质挑退避：配置缺失修好前重试全是徒劳，用长的；
// 其余（网络抖动、节点忙）用短的。
func retryDelayFor(err error) time.Duration {
	if errors.Is(err, ErrSupplierPayoutChainDisabled) {
		return supplierPayoutDisabledRetryDelay
	}
	return supplierPayoutRetryDelay
}
