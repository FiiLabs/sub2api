//go:build unit

// APEXONE-EXT: 双边市场——建单时那份链上快照（M3）。
//
// M1 定下「钱打到哪」，M2 造出「谁去打」，这个文件测的是把两者钉在单子上的那一刻：
// network / token_symbol / token_address / fee_amount 四列。
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
	"encoding/json"
	"math"
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
// chain 传 nil 就是「这套部署没接链上客户端」——那是 M1/M2 期间的形态，
// 也是本文件里"退回人工工单"那几条的起点。
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
	chain := NewMockChainClient(MockChainOptions{Fee: 0.3})
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), chain)

	created, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err)
	require.NotNil(t, created)

	params := repo.createParams
	assert.Equal(t, SupplierPayoutNetworkBSC, params.Network, "没写 network，M4 的 worker 永远捞不到这张单子")
	assert.Equal(t, "USDT", params.TokenSymbol)
	assert.Equal(t, MockChainTokenAddress, params.TokenAddress,
		"合约地址必须来自链上客户端；落一个币种符号进去，M4 会拿 \"USDT\" 当合约地址调用")
	assert.InDelta(t, 0.3, params.FeeAmount, 1e-9)
	// amount 仍然是**总额**。改成净额的话，ledger 的 withdraw 流水、退款、
	// 对账导出会同时改口径，而它们的读者都以为自己在看"他申请了多少"。
	assert.InDelta(t, 100.0, params.Amount, 1e-9, "amount 被写成了净额——扣款、退款、对账三条路径会一起偏")
}

// 人工渠道一列链上字段都不许带。
//
// 少了这条，一次"给提现加上手续费"的改动会顺手把支付宝提现也扣掉一笔 gas——
// 而那笔 gas 对应的链上交易根本不存在。
func TestRequestLeavesManualChannelWithoutChainColumns(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	chain := NewMockChainClient(MockChainOptions{Fee: 0.3})
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), chain)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝", PayoutAccount: "zhang@example.com",
	})
	require.NoError(t, err)

	params := repo.createParams
	assert.Empty(t, params.Network)
	assert.Empty(t, params.TokenSymbol)
	assert.Empty(t, params.TokenAddress)
	assert.Zero(t, params.FeeAmount, "支付宝提现被扣了一笔链上 gas，而那笔交易并不存在")
}

// ============================================================================
// 不写：三种"此刻不上链"，全都退回人工工单，且都不是错误
// ============================================================================

// 没接链上客户端 → 单子照建，只是走人工那条路。
//
// 这条是 M3 最容易写反的一处。看起来"链上渠道 + 没有链上客户端"是个故障，
// 该报 503；但 M1/M2 期间「BSC-USDT 进白名单、运营看着绑定地址手工转账」
// 是一条完整可用的路径，M3 上线不该把它变成一个 5xx。
//
// 安全性不靠报错来保证，靠**留白**：没写 network，M4 的 worker 就捞不到它。
func TestRequestFallsBackToManualWhenChainClientMissing(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), nil)

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err, "没配金库把一条本来走得通的人工路径变成了 5xx")
	assert.Equal(t, 1, repo.calls)
	assert.Empty(t, repo.createParams.Network, "没有客户端却写了 network——worker 会捞到一张打不出去的单子")
	assert.Zero(t, repo.createParams.FeeAmount, "人工全额打款却扣了 gas")
}

// 生产默认（DisabledChainClient）与"没有客户端"必须是同一个结果。
//
// 两者在语义上是一件事——"这套部署此刻不上链"。如果只判了 nil 而没判
// TokenAddress 的返回值，接上 wire 的那一刻（DisabledChainClient 是非 nil 的）
// 行为就会静默地翻个面。
func TestRequestFallsBackToManualOnDisabledChainClient(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewDisabledChainClient(0.5))

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err)
	assert.Empty(t, repo.createParams.Network)
	assert.Zero(t, repo.createParams.FeeAmount,
		"金库拒绝广播，却按回落值扣了一笔 gas——这笔钱谁也没花出去")
}

// 打款开着，但金库里是另一种币 → 同样退回人工，不能标成 USDT。
//
// 这是"配置与注册表对不上"的那个缝：渠道说 USDT，金库配的是 USDC。
// 把单子标成 USDT 再交给 worker，结果是有人收到了错的币；标成 USDC 又与
// 供给者点的那个渠道对不上。唯一诚实的动作是让它走人工，让人看一眼。
func TestRequestFallsBackToManualWhenVaultHoldsAnotherToken(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{NoToken: true, Fee: 0.3}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err)
	assert.Empty(t, repo.createParams.TokenAddress)
	assert.Empty(t, repo.createParams.Network, "金库里是另一种币，单子却被标成了会自动打款")
}

// 退回人工不等于放松收款地址那道门：没绑地址仍然拒绝建单。
//
// 两道门管的是两件事——地址来源（M1）与结算方式（M3）。合在一起判的话，
// "没配金库"会顺带把"必须用绑定地址"也关掉，于是手填地址又溜回来了。
func TestManualFallbackStillRequiresBoundAddress(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, &supplierPayoutWalletRepoStub{wallet: nil}, nil)

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	assert.ErrorIs(t, err, ErrSupplierPayoutWalletNotFound)
	assert.Zero(t, repo.calls, "没绑地址却建了单——钱已经扣了")
}

// ============================================================================
// 手续费：它是从供给者收益里切出去的，所以每一条边界都是钱
// ============================================================================

// 手续费吃掉整笔金额时拒绝建单，且一行都不建。
//
// 放行的话钱会先从可用区扣走，然后 worker 发现没得可发——一笔钱卡在一张
// 推不动的单子上，而供给者的余额已经少了。
func TestRequestRejectsWhenFeeEatsTheWholeAmount(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{Fee: 80}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(60))
	assert.ErrorIs(t, err, ErrSupplierWithdrawalFeeExceedsAmount)
	assert.Zero(t, repo.calls, "手续费比提现金额还高，单子却建起来了——这笔钱推不动也退不回")
}

// 手续费**恰好等于**金额时也拒绝。
//
// 取 >= 而不是 >：实发 0 的转账照样要烧一次 gas，而那笔 gas 是平台白花的，
// 换来一张链上金额为零的交易记录。这一条单独测，是因为把 >= 写成 > 编译过、
// 绝大多数输入上行为一致，只在这个精确的点上漏一笔。
func TestRequestRejectsWhenFeeExactlyEqualsAmount(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{Fee: 60}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(60))
	assert.ErrorIs(t, err, ErrSupplierWithdrawalFeeExceedsAmount)
	assert.Zero(t, repo.calls, "实发 0 的转账仍然要烧一次 gas")
}

// 估不出手续费（NaN / 无穷）时报 503，不当成 0。
//
// 当成 0 是在说"这个渠道不收手续费"，而真相是"链上客户端算出了一个不是数的东西"
// （币价配成 0、RPC 回了垃圾）。前者让金库替所有人垫 gas 且没有任何日志，
// 后者是个能被运维看见并修好的故障。
func TestRequestRejectsUnusableFeeInsteadOfTreatingItAsFree(t *testing.T) {
	for name, fee := range map[string]float64{
		"NaN":  math.NaN(),
		"正无穷":  math.Inf(1),
		"负手续费": -1,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &supplierWithdrawalRepoStub{}
			svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{Fee: fee}))

			_, err := svc.Request(context.Background(), 7, onchainRequest(100))
			assert.ErrorIs(t, err, ErrSupplierWithdrawalFeeUnavailable)
			assert.Zero(t, repo.calls, "一个算不出来的手续费被当成了 0——金库在替所有人垫 gas")
		})
	}
}

// 落库的手续费必须已经收敛到账本精度上。
//
// 估算值来自一串浮点乘法，小数位可以有二十几位。不收敛就交给仓储，
// DECIMAL(20,8) 那一列会替我们截断，于是**服务算出来的净额与按库里那一行算出来的
// 净额不是同一个数**——而 M4 打款读的是库里那一行。差值在 1e-9 量级，
// 小到不影响任何人的钱，大到足够让两个本该相等的数不相等。
func TestRequestRoundsFeeToLedgerScaleBeforePersisting(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	// 一个 8 位以后还有东西的估算值。
	const raw = 0.123456789123
	svc := withdrawalServiceWithChain(repo, boundWalletRepo(), NewMockChainClient(MockChainOptions{Fee: raw}))

	_, err := svc.Request(context.Background(), 7, onchainRequest(100))
	require.NoError(t, err)

	fee := repo.createParams.FeeAmount
	assert.NotEqual(t, raw, fee, "原样落库——数据库会替我们做一次没人看见的截断")
	assert.Equal(t, 0.12345679, fee, "收敛的方向不是四舍五入到第 8 位")

	// 收敛过的数再走一遍库的精度，必须是它自己（这正是"服务与数据库算同一个数"的定义）。
	again, ok := SanitizeChainFee(fee)
	require.True(t, ok)
	assert.Equal(t, fee, again)
}

// ============================================================================
// 报价：表单上那个"预计实到"从哪来
// ============================================================================

// 能结算时报价，且报价与建单用的是同一个数。
//
// 两处各算一次是刻意的（报价与建单之间隔着一次 gas 波动），但**判据**必须是同一个：
// 如果 options 说"这个渠道自动打款、手续费 0.3"，而建单走了人工路径，
// 供给者会盯着一个永远不动的"预计几分钟到账"。
func TestWithdrawalOptionsQuotesFeeForSettleableChannels(t *testing.T) {
	chain := NewMockChainClient(MockChainOptions{Fee: 0.3})
	svc := withdrawalServiceWithChain(&supplierWithdrawalRepoStub{}, boundWalletRepo(), chain)

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, options.OnchainFees, 1)
	assert.Equal(t, "BSC-USDT", options.OnchainFees[0].Channel)
	assert.InDelta(t, 0.3, options.OnchainFees[0].Fee, 1e-9)
	assert.True(t, options.OnchainFees[0].Estimated, "mock 模拟的是「问过链了」")
}

// 此刻走不了链上的渠道**不出现在报价里**，而且那一栏是 `[]` 不是 null。
//
// 报一个 0 会让界面写出"手续费 0.00，预计实到 100.00，几分钟到账"——
// 三句话里有两句是错的。差集才是前端唯一能据以判断"这个渠道现在是人工"的东西。
func TestWithdrawalOptionsOmitsUnsettleableChannelsFromQuotes(t *testing.T) {
	for name, chain := range map[string]SupplierChainClient{
		"没接客户端":  nil,
		"金库没配":   NewDisabledChainClient(0.5),
		"金库里是别的币": NewMockChainClient(MockChainOptions{NoToken: true, Fee: 0.3}),
		"估不出手续费":  NewMockChainClient(MockChainOptions{Fee: math.NaN()}),
	} {
		t.Run(name, func(t *testing.T) {
			svc := withdrawalServiceWithChain(&supplierWithdrawalRepoStub{}, boundWalletRepo(), chain)

			options, err := svc.GetOptions(context.Background(), 7)
			require.NoError(t, err, "报不出手续费不该让整张表单画不出来")
			assert.Empty(t, options.OnchainFees)

			// 注册表本身照旧全量吐出——"这个渠道是 BSC 上的 USDT"与
			// "它此刻自动不自动"是两个问题，前者不随金库配置变化。
			assert.Len(t, options.OnchainChannels, 1)

			encoded, err := json.Marshal(options)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"onchain_fees":[]`,
				"发成了 null——前端的 v-for/查表迟早会在 null 上炸")
		})
	}
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
	t.Run("fee quote", func(t *testing.T) {
		assert.Equal(t,
			[]string{"channel", "estimated", "fee"},
			jsonKeys(t, SupplierWithdrawalFeeQuote{}))
	})

	t.Run("options", func(t *testing.T) {
		assert.Equal(t,
			[]string{
				"available", "available_credit", "channels", "enabled",
				"max_pending", "min_amount", "notice",
				"onchain_channels", "onchain_fees", "pending_count",
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
