//go:build unit

// APEXONE-EXT: 双边市场——建单时那份链上快照（M3）。
//
// M1 定下「钱打到哪」，M2 造出「谁去打」，这个文件测的是把两者钉在单子上的那一刻：
// network / token_symbol / token_address 三列（免手续费改版后 fee_amount 恒 0）。
//
// # 为什么这四列值得一整个测试文件
//
// 它们写错的两个方向都不报错、都不留日志，而且都要等到 M4 的 worker 跑起来
// 才看得见后果：
//
//   - **多写了**（本该人工的单子带上了 network）——worker 捞起来、发现打不出去、
//     或者更糟：照着一个猜出来的合约地址真发了。而钱在建单那一刻已经从可用区扣走。
//   - **少写了**（本该上链的单子没写全）——单子安静地躺在人工队列里，
//     供给者以为几分钟到账，实际在等一个没人知道要做的人工操作。
//
// 所以本文件的断言几乎全是「交给仓储的那份参数长什么样」，以及「repo 有没有被调用」——
// 后者是"钱扣没扣"在这一层唯一可观察的形态。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundWalletRepo 一个已经绑好 BSC 地址的假仓储。
func boundWalletRepo() *supplierPayoutWalletRepoStub {
	return &supplierPayoutWalletRepoStub{
		wallet: &SupplierPayoutWallet{
			ID: 1, UserID: 7,
			Network: SupplierPayoutNetworkBSC,
			Address: "0xde709f2102306220921060314715629080e2fb77",
		},
	}
}

// withdrawalServiceWithChain 造一个「绑定服务 + 链上客户端都装配好」的提现服务。
//
// chain 传 nil 就是「这套部署没接链上客户端」——M6b（链上-only）之后
// 那种部署的提现整体不可用，本文件里"结算不了一律拒绝"那几条测的就是它。
func withdrawalServiceWithChain(
	repo SupplierWithdrawalRepository,
	walletRepo SupplierPayoutWalletRepository,
	chain SupplierChainClient,
) *SupplierWithdrawalService {
	svc := &SupplierWithdrawalService{
		repo:     repo,
		settings: &supplierWithdrawalSettingsStub{settings: onchainWithdrawalSettings()},
		chain:    chain,
	}
	if walletRepo != nil {
		svc.addresses = NewSupplierPayoutWalletService(walletRepo)
	}
	return svc
}

func onchainRequest(amount float64) SupplierWithdrawalRequest {
	return SupplierWithdrawalRequest{Amount: amount, PayoutChannel: "BSC-USDT", PayoutAccount: "ignored"}
}

// ============================================================================
// 写：四列必须一起落地
// ============================================================================

// 配好了金库的部署上，链上渠道的单子带着完整的一组链上字段。
//
// 重点在 token_address 的**来源**：它来自链上客户端（配置），不是注册表里那个
// 币种符号。注册表只说"这个渠道是 BSC 上的 USDT"，而"USDT 是哪个合约"是配置问题——
// 把符号当地址落库，M4 会拿着字符串 "USDT" 去当合约地址调用。
func TestRequestSnapshotsChainColumns(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	chain := NewMockChainClient(MockChainOptions{})
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), chain)

	created, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err)
	require.NotNil(t, created)

	params := repo.createParams
	assert.Equal(t, SupplierPayoutNetworkBSC, params.Network, "没写 network，M4 的 worker 永远捞不到这张单子")
	assert.Equal(t, "USDT", params.TokenSymbol)
	assert.Equal(t, MockChainTokenAddress, params.TokenAddress,
		"合约地址必须来自链上客户端；落一个币种符号进去，M4 会拿 \"USDT\" 当合约地址调用")
	// amount 仍然是**总额**（免手续费后它同时就是链上实发额）。
	assert.InDelta(t, 100.0, params.Amount, 1e-9)
}

// 老白名单里的人工渠道（支付宝）→ 渠道无效，一张单子都不建（M6b）。
//
// M3 时这条测的是"人工单不带链上列"；链上-only 之后没有任何队列会接住
// 一张人工单，让它建起来 = 钱扣进一张没人处理的单子。
func TestRequestRejectsManualChannelsOutright(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	chain := NewMockChainClient(MockChainOptions{})
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), chain)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝", PayoutAccount: "zhang@example.com",
	})
	assert.ErrorIs(t, err, ErrSupplierWithdrawalChannelInvalid)
	assert.Zero(t, repo.calls)
}

// ============================================================================
// 结算不了：三种形态一律拒绝建单，钱一分不扣（M6b 反转 M3 的"留白退人工"）
// ============================================================================

// 没接链上客户端 → NOT_CONFIGURED，一张单子都不建。
//
// M3 时这里退人工工单，理由是人工打款还是一条活路。M6b 把那条路下掉之后，
// 一张四列留白的单子既不会被 worker 捞到、也没有人工队列接住——它只会
// 安静地躺着，而钱在建单那一刻已经扣走。拒绝是唯一不吞钱的答案。
func TestRequestRejectsWhenChainClientMissing(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), nil)

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	assert.ErrorIs(t, err, ErrSupplierWithdrawalNotConfigured)
	assert.Zero(t, repo.calls, "结算不了却建了单——钱扣进了一张没人会处理的单子")
}

// 生产默认（DisabledChainClient）与"没有客户端"必须是同一个结果。
//
// 两者在语义上是一件事——"这套部署此刻不上链"。如果只判了 nil 而没判
// TokenAddress 的返回值，接上 wire 的那一刻（DisabledChainClient 是非 nil 的）
// 行为就会静默地翻个面。
func TestRequestRejectsOnDisabledChainClient(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewDisabledChainClient())

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	assert.ErrorIs(t, err, ErrSupplierWithdrawalNotConfigured)
	assert.Zero(t, repo.calls)
}

// 打款开着，但金库里是另一种币 → 同样拒绝，不能标成 USDT 交给 worker。
//
// 这是"配置与注册表对不上"的那个缝：渠道说 USDT，金库配的是 USDC。
// 把单子标成 USDT，worker 的 checkToken 会拒（§9.9），单子卡成 failed；
// 标成 USDC 又与供给者点的渠道对不上。在建单这一刻拒绝，钱还没扣。
func TestRequestRejectsWhenVaultHoldsAnotherToken(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{NoToken: true}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	assert.ErrorIs(t, err, ErrSupplierWithdrawalNotConfigured)
	assert.Zero(t, repo.calls)
}

// 金库配好了也不放松收款地址那道门：没绑地址仍然拒绝建单。
//
// 两道门管的是两件事——地址来源（M1）与结算方式（M3/M6b）。
// 合在一起判的话，"金库配好了"会顺带把"必须用绑定地址"也关掉。
func TestRequestStillRequiresBoundAddress(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, &supplierPayoutWalletRepoStub{wallet: nil}, NewMockChainClient(MockChainOptions{}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	assert.ErrorIs(t, err, ErrSupplierPayoutWalletNotFound)
	assert.Zero(t, repo.calls, "没绑地址却建了单——钱已经扣了")
}

// ============================================================================
// 单子上那两个派生问题
// ============================================================================

// OnChain 的判据只有 network，且空白串归到人工那边。
//
// 一行 network = '' 在数据库里完全合法（一次写坏的迁移、一次手工 UPDATE），
// 而它既不是人工也不是任何一条链。归到人工，是因为人工那条路上有人看着。
func TestWithdrawalOnChainJudgesByNetworkOnly(t *testing.T) {
	blank := " "
	network := SupplierPayoutNetworkBSC

	assert.False(t, (*SupplierWithdrawal)(nil).OnChain())
	assert.False(t, (&SupplierWithdrawal{}).OnChain())
	assert.False(t, (&SupplierWithdrawal{Network: &blank}).OnChain(),
		"network 是一串空白，却被当成了一条链")
	// 带着手续费和合约地址、却没有 network 的行仍然是人工的：
	// 捞单的判据只有一个，多一个就会有两个能互相矛盾的答案。
	assert.False(t, (&SupplierWithdrawal{FeeAmount: 0.3, TokenAddress: &network}).OnChain())
	assert.True(t, (&SupplierWithdrawal{Network: &network}).OnChain())
}

// 净额 = 总额 - 手续费，且不落库。
//
// 落一个能由另外两列算出来的数，就多了一处能与它们不一致的地方，
// 而不一致时没人知道该信哪个。
func TestWithdrawalNetAmountIsDerived(t *testing.T) {
	assert.Zero(t, (*SupplierWithdrawal)(nil).NetAmount())
	assert.InDelta(t, 99.7, (&SupplierWithdrawal{Amount: 100, FeeAmount: 0.3}).NetAmount(), 1e-9)
	// 人工单：没有手续费，净额就是总额。
	assert.InDelta(t, 100.0, (&SupplierWithdrawal{Amount: 100}).NetAmount(), 1e-9)
}

// ============================================================================
// 线上形状
// ============================================================================

// 提现的两个用户可见响应，键的**全集**钉死。
//
// 与 TestPayoutWireShapeIsSnakeCase 同一个理由，而这两个结构体更值得钉：
// 单子上现在带着一个人的链上收款地址、合约地址和一笔扣款。多一个字段和少一个
// 字段一样值得看一眼——前者可能是把 token_address 之外的东西也发了出去，
// 后者会让前端的"预计实到"永远算错。
func TestWithdrawalWireShapeIsSnakeCase(t *testing.T) {
	t.Run("options", func(t *testing.T) {
		assert.Equal(t,
			[]string{
				"available", "available_credit", "channels", "enabled",
				"max_pending", "min_amount", "notice",
				"onchain_channels", "pending_count",
			},
			jsonKeys(t, &SupplierWithdrawalOptions{}))
	})

	t.Run("withdrawal", func(t *testing.T) {
		// 链上四项在人工单上全是零值，且带 omitempty 的那三个不该出现。
		// fee_amount **没有** omitempty：一个 0 的手续费与"这张单子没有手续费"
		// 是同一件事，但前端要用它做减法，而 undefined 减出来是 NaN。
		assert.Equal(t,
			[]string{"amount", "created_at", "fee_amount", "id", "payout_account", "payout_channel", "status", "updated_at", "user_id"},
			jsonKeys(t, &SupplierWithdrawal{}))
	})
}
