//go:build unit

// APEXONE-EXT: 双边市场——链上打款 worker 的单元测试。
//
// 这台状态机上每一步的错误都直接是钱：nonce 没先落库是双付，广播失败判成
// 链上失败是把一笔可能在路上的钱退回去（也是双付），发了毛额而不是净额是把
// 手续费白送。所以这个文件的断言全部落在**动作的顺序与参数**上：
// 什么时候许广播、广播了多少、发给谁、用哪个 nonce、答案不明时停在哪。
//
// 仓储用桩（把每次调用记成流水账），链用 MockChainClient——它的 nonce 语义
// 与真节点一致（只在广播后前进），这正是 nonce 复用那几条测试能成立的前提。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 仓储桩：流水账式记录，每个方法可注入错误。
// ============================================================================

type payoutBegin struct {
	ID    int64
	Nonce uint64
}

type payoutRelease struct {
	ID         int64
	LastError  string
	RetryAfter time.Duration
}

type payoutQueueStub struct {
	due      []SupplierWithdrawal
	claimErr error

	begins   []payoutBegin
	beginErr error
	// beginErrFor 只对指定单号注错——批量测试要让组里**一张**掉队而其余照走。
	beginErrFor map[int64]error

	records []struct {
		ID     int64
		TxHash string
	}
	recordErr    error
	recordErrFor map[int64]error

	finishes  []SupplierPayoutFinishParams
	finishErr error

	releases []payoutRelease
}

func (s *payoutQueueStub) ClaimPayoutDue(context.Context, int, time.Duration) ([]SupplierWithdrawal, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.due, nil
}

func (s *payoutQueueStub) BeginPayout(_ context.Context, id int64, nonce uint64) error {
	if err, ok := s.beginErrFor[id]; ok {
		return err
	}
	if s.beginErr != nil {
		return s.beginErr
	}
	s.begins = append(s.begins, payoutBegin{ID: id, Nonce: nonce})
	return nil
}

func (s *payoutQueueStub) RecordPayoutTx(_ context.Context, id int64, txHash string) error {
	if err, ok := s.recordErrFor[id]; ok {
		return err
	}
	if s.recordErr != nil {
		return s.recordErr
	}
	s.records = append(s.records, struct {
		ID     int64
		TxHash string
	}{id, txHash})
	return nil
}

func (s *payoutQueueStub) FinishPayout(_ context.Context, params SupplierPayoutFinishParams) (*SupplierWithdrawal, error) {
	if s.finishErr != nil {
		return nil, s.finishErr
	}
	s.finishes = append(s.finishes, params)
	out := SupplierWithdrawal{ID: params.ID, Status: params.Status}
	return &out, nil
}

func (s *payoutQueueStub) ReleasePayoutLease(_ context.Context, id int64, lastError string, retryAfter time.Duration) error {
	s.releases = append(s.releases, payoutRelease{ID: id, LastError: lastError, RetryAfter: retryAfter})
	return nil
}

// unknownConfirmChain 把 WaitForConfirmation 换成「还不知道」或任意答案。
// Mock 客户端自己永远给出确定答案，而"等不到"恰恰是生产里最常见的那一格。
type unknownConfirmChain struct {
	*MockChainClient
	confirmErr    error
	confirmStatus string
	confirmCalls  int // 数查询次数：一组共享哈希的单子只该问链一次
}

func (c *unknownConfirmChain) WaitForConfirmation(ctx context.Context, network, txHash string) (ChainConfirmation, error) {
	c.confirmCalls++
	if c.confirmErr != nil {
		return ChainConfirmation{}, c.confirmErr
	}
	if c.confirmStatus != "" {
		return ChainConfirmation{Status: c.confirmStatus}, nil
	}
	return c.MockChainClient.WaitForConfirmation(ctx, network, txHash)
}

// ============================================================================
// 造数据
// ============================================================================

const (
	payoutTestAccount = "0xde709f2102306220921060314715629080e2fb77"
	// 单子上的合约快照，**故意**不同于 Mock 客户端自己配的地址：
	// 每条走到 Transfer 的测试都顺带证明了 worker 发的是快照那一个。
	payoutTestToken = "0x55d398326f99059ff775485246999027b3197955"
)

func onchainDueWithdrawal(id int64) SupplierWithdrawal {
	network := SupplierPayoutNetworkBSC
	symbol := "USDT"
	token := payoutTestToken
	return SupplierWithdrawal{
		ID:            id,
		UserID:        7,
		Amount:        100,
		FeeAmount:     0.3,
		Status:        SupplierWithdrawalStatusPending,
		PayoutChannel: "BSC-USDT",
		PayoutAccount: payoutTestAccount,
		Network:       &network,
		TokenSymbol:   &symbol,
		TokenAddress:  &token,
	}
}

func payoutWorkerOn(repo SupplierPayoutQueueRepository, chain SupplierChainClient) *SupplierPayoutWorker {
	return NewSupplierPayoutWorker(repo, chain, nil, time.Hour)
}

// ============================================================================
// 快乐路径：一张单子从 pending 一路走到 paid
// ============================================================================

func TestPayoutWorkerPaysNetToTheBoundAddress(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1)}}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	// nonce 在广播之前落了库，且用的就是节点给的那个。
	require.Len(t, repo.begins, 1)
	assert.Equal(t, uint64(0), repo.begins[0].Nonce)

	transfers := chain.Transfers()
	require.Len(t, transfers, 1)
	// 发的是**净额**。发毛额 = 把本该从收益里切走的手续费白送给供给者，
	// 而且每一笔都送、没有任何报错。
	assert.InDelta(t, 99.7, transfers[0].Amount, 1e-9)
	// 打给绑定地址，合约用单子上的快照（不是 Mock 自己配的 0x…dead）。
	assert.Equal(t, payoutTestAccount, transfers[0].To)
	assert.Equal(t, payoutTestToken, transfers[0].Token)
	require.NotNil(t, transfers[0].Nonce)
	assert.Equal(t, uint64(0), *transfers[0].Nonce)

	// 哈希记了，终局是 paid，且带的就是那个哈希。
	require.Len(t, repo.records, 1)
	require.Len(t, repo.finishes, 1)
	assert.Equal(t, SupplierWithdrawalStatusPaid, repo.finishes[0].Status)
	assert.Equal(t, repo.records[0].TxHash, repo.finishes[0].TxHash)
	assert.Empty(t, repo.releases, "快乐路径不该有任何一次退避")
}

// 不同币种的单子各自成组、顺序处理，nonce 逐张递增——这是"顺序、不并发"的
// 可观测形态。同币种的两张不走这里：它们会合成一笔批量（见 M5 那组测试）。
func TestPayoutWorkerSettlesSequentiallyWithFreshNonces(t *testing.T) {
	second := onchainDueWithdrawal(2)
	otherToken := "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d" // BSC 上的 USDC
	second.TokenAddress = &otherToken
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), second}}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	transfers := chain.Transfers()
	require.Len(t, transfers, 2)
	assert.Empty(t, chain.Batches(), "不同币种绝不能进同一笔批量——那是拿 A 币的额度发 B 币")
	assert.Equal(t, uint64(0), *transfers[0].Nonce)
	assert.Equal(t, uint64(1), *transfers[1].Nonce, "第二张必须拿到广播后前进过的 nonce")
	require.Len(t, repo.finishes, 2)
}

// ============================================================================
// nonce：双付唯一的防线
// ============================================================================

// 落过库的 nonce 必须原样复用，哪怕节点已经走到前面去了。
// 向节点重新要号的那个变异，就是「同一张单子打两次款」本体。
func TestPayoutWorkerReusesThePersistedNonce(t *testing.T) {
	w := onchainDueWithdrawal(1)
	persisted := int64(5)
	w.ChainNonce = &persisted
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := NewMockChainClient(MockChainOptions{})
	// 金库地址上又发生过别的交易：节点视角的下一个号早已不是 5。
	for range [9]int{} {
		chain.BumpNonce(SupplierPayoutNetworkBSC)
	}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	transfers := chain.Transfers()
	require.Len(t, transfers, 1)
	assert.Equal(t, uint64(5), *transfers[0].Nonce)
	require.Len(t, repo.begins, 1)
	assert.Equal(t, uint64(5), repo.begins[0].Nonce, "重播时 BeginPayout 也必须钉同一个号")
}

// BeginPayout 说"不"（管理端在租约过期后已处理掉这张单子）→ 一个字节都不许广播。
func TestPayoutWorkerAbortsBroadcastWhenOrderWasResolvedElsewhere(t *testing.T) {
	repo := &payoutQueueStub{
		due:      []SupplierWithdrawal{onchainDueWithdrawal(1)},
		beginErr: ErrSupplierWithdrawalNotPending,
	}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers(), "单子已被别人处理，广播出去就是双付")
	assert.Empty(t, repo.finishes)
	assert.Empty(t, repo.releases, "这不是要重试的情形——单子已经有了终态")
}

// nonce 落不下去（数据库不可达）→ 同样不许广播：没钉住的 nonce 撑不起安全重试。
func TestPayoutWorkerAbortsBroadcastWhenNonceIsNotDurable(t *testing.T) {
	repo := &payoutQueueStub{
		due:      []SupplierWithdrawal{onchainDueWithdrawal(1)},
		beginErr: assert.AnError,
	}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers())
	require.Len(t, repo.releases, 1, "库抖动是要重试的情形，单子得留在队里")
	assert.Empty(t, repo.finishes)
}

// ============================================================================
// 广播与确认的各种"还不知道"
// ============================================================================

// 广播报错（链上结果未知）→ 交还租约等重播，绝不走终局。
func TestPayoutWorkerLeavesUnknownBroadcastForRetry(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1)}}
	chain := NewMockChainClient(MockChainOptions{})
	chain.FailNextTransfer("rpc timeout")
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	// nonce 已钉（重播的前提），但没有任何终局。
	require.Len(t, repo.begins, 1)
	assert.Empty(t, repo.finishes, "结果未知就写终局，两个方向都是错账")
	require.Len(t, repo.releases, 1)
	assert.Contains(t, repo.releases[0].LastError, "broadcast")
}

// 哈希记不下来 → 这一轮到此为止。带着一个没落库的哈希继续走到 paid，
// 对账里就多了一张没有凭证的已打款单。
func TestPayoutWorkerStopsWhenTxHashIsNotDurable(t *testing.T) {
	repo := &payoutQueueStub{
		due:       []SupplierWithdrawal{onchainDueWithdrawal(1)},
		recordErr: assert.AnError,
	}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, chain.Transfers(), 1)
	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 1)
}

// 已有哈希的单子只欠一个答案：不重新广播、不重新要 nonce。
func TestPayoutWorkerOnlyConfirmsWhenHashAlreadyRecorded(t *testing.T) {
	w := onchainDueWithdrawal(1)
	hash := "0xabc123"
	nonce := int64(3)
	broadcasted := time.Now().Add(-time.Minute)
	w.TxHash = &hash
	w.ChainNonce = &nonce
	w.BroadcastedAt = &broadcasted
	w.Status = SupplierWithdrawalStatusProcessing
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := NewMockChainClient(MockChainOptions{})

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers(), "已广播过的单子再广播一次，靠的只能是运气")
	assert.Empty(t, repo.begins)
	require.Len(t, repo.finishes, 1)
	assert.Equal(t, SupplierWithdrawalStatusPaid, repo.finishes[0].Status)
	assert.Equal(t, hash, repo.finishes[0].TxHash, "终局带的必须是单子上那个哈希")
}

// 确认等不到（错误 = 还不知道）→ 退避重试，不是 failed。
// 判成 failed 会让运营把一笔可能已经成功的转账退款：双付。
func TestPayoutWorkerUnknownConfirmationBacksOff(t *testing.T) {
	w := onchainDueWithdrawal(1)
	hash := "0xabc123"
	broadcasted := time.Now().Add(-time.Minute) // 离放弃期限还远
	w.TxHash = &hash
	w.BroadcastedAt = &broadcasted
	w.Status = SupplierWithdrawalStatusProcessing
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := &unknownConfirmChain{MockChainClient: NewMockChainClient(MockChainOptions{}), confirmErr: assert.AnError}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 1)
	assert.Equal(t, supplierPayoutRetryDelay, repo.releases[0].RetryAfter)
}

// 过了放弃期限还没有答案 → 转给运营（failed），reason 里必须带"先查链"。
func TestPayoutWorkerGivesUpConfirmingAfterDeadline(t *testing.T) {
	w := onchainDueWithdrawal(1)
	hash := "0xabc123"
	broadcasted := time.Now().Add(-supplierPayoutGiveUpAfter - time.Minute)
	w.TxHash = &hash
	w.BroadcastedAt = &broadcasted
	w.Status = SupplierWithdrawalStatusProcessing
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := &unknownConfirmChain{MockChainClient: NewMockChainClient(MockChainOptions{}), confirmErr: assert.AnError}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, repo.finishes, 1)
	assert.Equal(t, SupplierWithdrawalStatusFailed, repo.finishes[0].Status)
	// 这句话是给运营的行动指令，不是装饰：不查链就退款可能是双付。
	assert.Contains(t, repo.finishes[0].Reason, "VERIFY ON-CHAIN")
	assert.Contains(t, repo.finishes[0].Reason, hash, "运营要拿着哈希去查，不带哈希等于让他自己翻库")
}

// 链上明确 revert → failed + 原因，**没有任何退款动作**（裁决留给人）。
func TestPayoutWorkerFailsRevertedTransactionWithoutRefunding(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1)}}
	chain := NewMockChainClient(MockChainOptions{Outcome: ChainTxFailed})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, repo.finishes, 1)
	assert.Equal(t, SupplierWithdrawalStatusFailed, repo.finishes[0].Status)
	assert.Contains(t, repo.finishes[0].Reason, "reverted")
	assert.Empty(t, repo.releases)
	// 退款不在队列仓储的能力面里（接口上根本没有那个方法）——
	// 这里能断言的是 worker 没有把 failed 伪装成 rejected 之类会触发退款的状态。
	assert.NotEqual(t, SupplierWithdrawalStatusRejected, repo.finishes[0].Status)
}

// 客户端违反自己的约定（confirm 回一个不认识的状态）→ 按"还不知道"处理。
func TestPayoutWorkerTreatsUnexpectedConfirmStatusAsUnknown(t *testing.T) {
	w := onchainDueWithdrawal(1)
	hash := "0xabc123"
	broadcasted := time.Now().Add(-time.Minute)
	w.TxHash = &hash
	w.BroadcastedAt = &broadcasted
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := &unknownConfirmChain{MockChainClient: NewMockChainClient(MockChainOptions{}), confirmStatus: "maybe"}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 1)
}

// ============================================================================
// 配置与数据的坏状态
// ============================================================================

// 客户端没配好（Disabled）→ 长退避留在队里，不广播、不终局。
// 修好配置这条路就通了，单子一张不丢。
func TestPayoutWorkerBacksOffLongWhenChainIsDisabled(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1)}}
	payoutWorkerOn(repo, &DisabledChainClient{}).RunOnce(context.Background())

	assert.Empty(t, repo.begins)
	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 1)
	assert.Equal(t, supplierPayoutDisabledRetryDelay, repo.releases[0].RetryAfter,
		"配置缺失不是抖动，短退避只会刷出一屏一模一样的日志")
}

// 快照残缺（只可能是手改库）→ 直接转给运营，别让它每 30 秒空转一次。
func TestPayoutWorkerFailsCorruptSnapshots(t *testing.T) {
	missingToken := onchainDueWithdrawal(1)
	missingToken.TokenAddress = nil

	feeEatsAll := onchainDueWithdrawal(2)
	feeEatsAll.FeeAmount = feeEatsAll.Amount // net = 0

	repo := &payoutQueueStub{due: []SupplierWithdrawal{missingToken, feeEatsAll}}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers())
	require.Len(t, repo.finishes, 2)
	for _, finish := range repo.finishes {
		assert.Equal(t, SupplierWithdrawalStatusFailed, finish.Status)
	}
}

// 单轮预算耗尽 → 立刻收手，剩下的单子留给租约到期后的下一轮。
//
// 预算闸在确认、广播两个循环里各有一道，**必须分开钉**：确认组一触闸整轮
// 就返回，所以「确认+广播混着放」的场景永远走不到广播段那道闸——第一轮
// 变异矩阵先后抓出两道闸各自没被测到（W13 GREEN 两次），就是这么漏的。
func TestPayoutWorkerStopsWhenBudgetExpires(t *testing.T) {
	ctxDone := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}

	t.Run("确认段", func(t *testing.T) {
		confirming := onchainDueWithdrawal(3)
		hash := "0xwaiting"
		broadcasted := time.Now().Add(-time.Minute)
		confirming.TxHash = &hash
		confirming.BroadcastedAt = &broadcasted
		confirming.Status = SupplierWithdrawalStatusProcessing

		repo := &payoutQueueStub{due: []SupplierWithdrawal{confirming}}
		chain := NewMockChainClient(MockChainOptions{})
		payoutWorkerOn(repo, chain).RunOnce(ctxDone())

		assert.Empty(t, repo.finishes, "预算耗尽后连确认都不该做——Mock 的链无视 ctx，会把单子标成 paid")
	})

	t.Run("广播段", func(t *testing.T) {
		repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
		chain := NewMockChainClient(MockChainOptions{})
		payoutWorkerOn(repo, chain).RunOnce(ctxDone())

		assert.Empty(t, chain.Transfers())
		assert.Empty(t, chain.Batches())
		assert.Empty(t, repo.finishes)
	})
}

// 放弃期限的 reason 会被写进 last_error 给运营看——顺带钉住给-up 只对
// "已广播"的单子生效：还没广播过的单子没有 BroadcastedAt，永远不会被误伤。
func TestPayoutWorkerNeverGivesUpOnUnbroadcastOrders(t *testing.T) {
	w := onchainDueWithdrawal(1)
	// 建单很久了但从没广播过（比如 worker 一直没配起来）。
	w.CreatedAt = time.Now().Add(-24 * time.Hour)
	repo := &payoutQueueStub{due: []SupplierWithdrawal{w}}
	chain := NewMockChainClient(MockChainOptions{})

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	// 正常走完打款，而不是被"过期"误杀。
	require.Len(t, repo.finishes, 1)
	assert.Equal(t, SupplierWithdrawalStatusPaid, repo.finishes[0].Status)
}

// 捞单本身失败 → 只记日志，本轮作罢；没有半个单子被碰过。
func TestPayoutWorkerSkipsRoundWhenClaimFails(t *testing.T) {
	repo := &payoutQueueStub{claimErr: assert.AnError}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers())
	assert.Empty(t, repo.begins)
	assert.Empty(t, repo.finishes)
	assert.Empty(t, repo.releases)
}

// ============================================================================
// 批量发放（M5）
// ============================================================================

// batchProbeChain 记录「额度检查」与「要号」的先后。这条顺序是批量路径上
// 唯一一条看不见的硬约束：approve 自己要占金库地址上的一个 nonce，
// 顺序反了它会把我们刚要到的号吃掉，批量交易重发时撞 nonce too low。
type batchProbeChain struct {
	*MockChainClient
	calls        []string
	allowanceErr error
	topUp        *ChainAllowanceTopUp
}

func (c *batchProbeChain) EnsureBatchAllowance(ctx context.Context, params ChainBatchParams) (*ChainAllowanceTopUp, error) {
	c.calls = append(c.calls, "allowance")
	if c.allowanceErr != nil {
		return nil, c.allowanceErr
	}
	if c.topUp != nil {
		return c.topUp, nil
	}
	return c.MockChainClient.EnsureBatchAllowance(ctx, params)
}

func (c *batchProbeChain) NextNonce(ctx context.Context, network string) (uint64, error) {
	c.calls = append(c.calls, "nonce")
	return c.MockChainClient.NextNonce(ctx, network)
}

// 同币种多张 → 一笔批量：一个 nonce、一个哈希、每张的净额与地址逐项对上。
func TestPayoutWorkerBatchesSameTokenOrders(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{
		onchainDueWithdrawal(1), onchainDueWithdrawal(2), onchainDueWithdrawal(3),
	}}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers(), "该合批的单子逐笔发，就是把三笔 gas 花成三倍")
	batches := chain.Batches()
	require.Len(t, batches, 1)
	require.Len(t, batches[0].Items, 3)
	for _, item := range batches[0].Items {
		assert.InDelta(t, 99.7, item.Amount, 1e-9, "批量里的每一项都必须是净额")
		assert.Equal(t, payoutTestAccount, item.To)
	}
	assert.Equal(t, payoutTestToken, batches[0].Token, "批量发的必须是单子上快照的那个合约")
	require.NotNil(t, batches[0].Nonce)
	assert.Equal(t, uint64(0), *batches[0].Nonce)

	// 整组共享一个 nonce（这是恢复逻辑按 nonce 归队的前提）。
	require.Len(t, repo.begins, 3)
	for _, begin := range repo.begins {
		assert.Equal(t, uint64(0), begin.Nonce)
	}
	// 整组共享一个哈希，三张全部 paid。
	require.Len(t, repo.records, 3)
	require.Len(t, repo.finishes, 3)
	for _, finish := range repo.finishes {
		assert.Equal(t, SupplierWithdrawalStatusPaid, finish.Status)
		assert.Equal(t, repo.records[0].TxHash, finish.TxHash)
	}
	assert.Empty(t, repo.releases)
}

// 额度检查必须排在要号之前——顺序错了 approve 会吃掉我们要用的号。
func TestPayoutWorkerBatchChecksAllowanceBeforeReservingNonce(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := &batchProbeChain{MockChainClient: NewMockChainClient(MockChainOptions{})}
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.GreaterOrEqual(t, len(chain.calls), 2)
	assert.Equal(t, []string{"allowance", "nonce"}, chain.calls[:2])
}

// 没配批量合约 = 没有优化，不是故障：安静退回逐笔，每张自己的 nonce。
func TestPayoutWorkerBatchFallsBackToSinglesWithoutContract(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := NewMockChainClient(MockChainOptions{NoBatch: true})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Batches())
	transfers := chain.Transfers()
	require.Len(t, transfers, 2)
	assert.NotEqual(t, *transfers[0].Nonce, *transfers[1].Nonce, "逐笔回退时两张不能共用一个号")
	require.Len(t, repo.finishes, 2)
}

// 额度检查失败 → 整组交还，一个 nonce 都还没钉、一个字节都没广播。
func TestPayoutWorkerBatchAllowanceFailureReleasesTheGroup(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := &batchProbeChain{
		MockChainClient: NewMockChainClient(MockChainOptions{}),
		allowanceErr:    assert.AnError,
	}
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, repo.begins, "额度都没确认就钉 nonce，等于把一组单子锁死在一个可能发不出去的号上")
	assert.Empty(t, chain.Batches())
	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 2)
}

// 批量广播失败 → 整组带着钉住的 nonce 回队；没有任何终局。
func TestPayoutWorkerBatchBroadcastFailureKeepsTheGroupPinned(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := NewMockChainClient(MockChainOptions{})
	chain.FailNextBatch("rpc timeout")
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, repo.begins, 2, "广播前 nonce 必须已经钉进整组——重播归队全靠它")
	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 2)
	for _, release := range repo.releases {
		assert.Contains(t, release.LastError, "batch broadcast")
	}
}

// 批量在链上 revert → 整组 failed（all-or-nothing，不存在"半成"），不退款。
func TestPayoutWorkerBatchRevertFailsEveryOrder(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := NewMockChainClient(MockChainOptions{Outcome: ChainTxFailed})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, repo.finishes, 2)
	for _, finish := range repo.finishes {
		assert.Equal(t, SupplierWithdrawalStatusFailed, finish.Status)
		assert.Contains(t, finish.Reason, "reverted")
	}
}

// 恢复期：共享同一个落库 nonce 的组原封归队，用**那个** nonce 重播，
// 不做额度检查、不重新要号——两者都可能占掉那个还没花出去的号。
func TestPayoutWorkerBatchRecoveryReusesTheSharedNonce(t *testing.T) {
	first := onchainDueWithdrawal(1)
	second := onchainDueWithdrawal(2)
	pinned := int64(5)
	first.ChainNonce = &pinned
	second.ChainNonce = &pinned
	first.Status = SupplierWithdrawalStatusProcessing
	second.Status = SupplierWithdrawalStatusProcessing
	repo := &payoutQueueStub{due: []SupplierWithdrawal{first, second}}
	chain := &batchProbeChain{MockChainClient: NewMockChainClient(MockChainOptions{})}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.NotContains(t, chain.calls, "allowance",
		"恢复期补 approve 会占一个新号，却换不回任何东西（额度要么已扣、要么原封没动）")
	assert.NotContains(t, chain.calls, "nonce", "恢复期换号 = 双付")
	batches := chain.Batches()
	require.Len(t, batches, 1)
	require.NotNil(t, batches[0].Nonce)
	assert.Equal(t, uint64(5), *batches[0].Nonce)
	require.Len(t, batches[0].Items, 2)
	require.Len(t, repo.finishes, 2)
}

// 恢复期批次撞上被关掉的批量配置：整组共用一个号拆不成单笔（每笔要自己的号，
// 换号 = 双付）——只能带长退避原地等配置修回来，并把话说清楚。
func TestPayoutWorkerBatchRecoveryBlockedWithoutContract(t *testing.T) {
	first := onchainDueWithdrawal(1)
	second := onchainDueWithdrawal(2)
	pinned := int64(5)
	first.ChainNonce = &pinned
	second.ChainNonce = &pinned
	repo := &payoutQueueStub{due: []SupplierWithdrawal{first, second}}
	chain := NewMockChainClient(MockChainOptions{NoBatch: true})

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Transfers(), "共用一个 nonce 的组拆成单笔广播，第二笔就是双付的候选")
	assert.Empty(t, chain.Batches())
	assert.Empty(t, repo.finishes)
	require.Len(t, repo.releases, 2)
	for _, release := range repo.releases {
		assert.Equal(t, supplierPayoutDisabledRetryDelay, release.RetryAfter)
		assert.Contains(t, release.LastError, "share nonce")
	}
}

// 组里一张在钉号时发现已被别人处理 → 它无声退出，其余照播（同号安全，
// 组成允许缩水；换号才是禁区）。
func TestPayoutWorkerBatchShrinksWhenAnOrderWasResolvedElsewhere(t *testing.T) {
	repo := &payoutQueueStub{
		due:         []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)},
		beginErrFor: map[int64]error{2: ErrSupplierWithdrawalNotPending},
	}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	batches := chain.Batches()
	require.Len(t, batches, 1)
	require.Len(t, batches[0].Items, 1, "掉队的那张不能再出现在批量里——它的钱已经被别的路径处理了")
	require.Len(t, repo.finishes, 1)
	assert.Equal(t, int64(1), repo.finishes[0].ID)
	assert.Empty(t, repo.releases, "掉队不是错误，不需要退避")
}

// 哈希没落库的那张不能进终局：它退出确认组、带着钉住的 nonce 回队；
// 其余照常走到 paid。
func TestPayoutWorkerBatchDropsOrderWhoseHashIsNotDurable(t *testing.T) {
	repo := &payoutQueueStub{
		due:          []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)},
		recordErrFor: map[int64]error{2: assert.AnError},
	}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	require.Len(t, repo.finishes, 1)
	assert.Equal(t, int64(1), repo.finishes[0].ID)
	assert.Equal(t, SupplierWithdrawalStatusPaid, repo.finishes[0].Status)
	require.Len(t, repo.releases, 1)
	assert.Equal(t, int64(2), repo.releases[0].ID)
	assert.Contains(t, repo.releases[0].LastError, "record tx hash")
}

// 共享一个哈希的确认组只问链一次，一次的答案定整组。
func TestPayoutWorkerConfirmGroupQueriesTheChainOnce(t *testing.T) {
	first := onchainDueWithdrawal(1)
	second := onchainDueWithdrawal(2)
	hash := "0xbatch-hash"
	broadcasted := time.Now().Add(-time.Minute)
	for _, w := range []*SupplierWithdrawal{&first, &second} {
		w.TxHash = &hash
		w.BroadcastedAt = &broadcasted
		w.Status = SupplierWithdrawalStatusProcessing
	}
	repo := &payoutQueueStub{due: []SupplierWithdrawal{first, second}}
	chain := &unknownConfirmChain{MockChainClient: NewMockChainClient(MockChainOptions{})}

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Equal(t, 1, chain.confirmCalls, "同一个哈希问两次链，答案不会更对，只会更慢")
	require.Len(t, repo.finishes, 2)
	for _, finish := range repo.finishes {
		assert.Equal(t, SupplierWithdrawalStatusPaid, finish.Status)
		assert.Equal(t, hash, finish.TxHash)
	}
}

// 钉了**不同** nonce 的恢复单绝不能进同一批：批量只有一个 nonce 字段，
// 合并意味着其中一张被换号——那正是双付的形状。
func TestPayoutWorkerRecoveryKeepsDistinctNoncesApart(t *testing.T) {
	first := onchainDueWithdrawal(1)
	second := onchainDueWithdrawal(2)
	nonceA, nonceB := int64(5), int64(7)
	first.ChainNonce = &nonceA
	second.ChainNonce = &nonceB
	repo := &payoutQueueStub{due: []SupplierWithdrawal{first, second}}
	chain := NewMockChainClient(MockChainOptions{})

	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	assert.Empty(t, chain.Batches())
	transfers := chain.Transfers()
	require.Len(t, transfers, 2)
	assert.Equal(t, uint64(5), *transfers[0].Nonce)
	assert.Equal(t, uint64(7), *transfers[1].Nonce)
}
