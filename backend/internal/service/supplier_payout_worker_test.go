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

	records   []struct{ ID int64; TxHash string }
	recordErr error

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
	if s.beginErr != nil {
		return s.beginErr
	}
	s.begins = append(s.begins, payoutBegin{ID: id, Nonce: nonce})
	return nil
}

func (s *payoutQueueStub) RecordPayoutTx(_ context.Context, id int64, txHash string) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	s.records = append(s.records, struct{ ID int64; TxHash string }{id, txHash})
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
}

func (c *unknownConfirmChain) WaitForConfirmation(ctx context.Context, network, txHash string) (ChainConfirmation, error) {
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

// 多张单子顺序处理，nonce 逐张递增——这是"顺序、不并发"的可观测形态。
func TestPayoutWorkerSettlesSequentiallyWithFreshNonces(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := NewMockChainClient(MockChainOptions{})
	payoutWorkerOn(repo, chain).RunOnce(context.Background())

	transfers := chain.Transfers()
	require.Len(t, transfers, 2)
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
func TestPayoutWorkerStopsWhenBudgetExpires(t *testing.T) {
	repo := &payoutQueueStub{due: []SupplierWithdrawal{onchainDueWithdrawal(1), onchainDueWithdrawal(2)}}
	chain := NewMockChainClient(MockChainOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	payoutWorkerOn(repo, chain).RunOnce(ctx)

	assert.Empty(t, chain.Transfers())
	assert.Empty(t, repo.finishes)
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
