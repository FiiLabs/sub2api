//go:build unit

// APEXONE-EXT: 链上打款客户端的接口层测试。
//
// 这个文件里没有一行会碰网络——被测的两个实现本来就不碰。真正值得测的是三件事：
//
//  1. 金额换算的**精确性**。这是整条链路上唯一一处"算错了钱就少发/多发"的地方，
//     而它错起来是每笔差几个最小单位，不会有任何人察觉。
//  2. DisabledChainClient 的每一个方法都**拒绝**，尤其 WaitForConfirmation 回的是
//     错误而不是 failed——那两者在 M4 里会导向完全相反的动作。
//  3. MockChainClient 的 nonce 只在真广播之后前进。worker 防双付的整套逻辑
//     建立在这条行为上，mock 若把它模拟错了，那些测试测的就不是线上跑的东西。
package service

import (
	"context"
	"math"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToTokenAmountIsExactDecimal(t *testing.T) {
	// 逐个是手算的：金额按 8 位定点展开，再补 (decimals-8) 个零。
	cases := []struct {
		name     string
		amount   float64
		decimals int
		want     string
	}{
		{"整数", 1, 18, "1000000000000000000"},
		{"两位小数", 12.34, 18, "12340000000000000000"},
		{"账本能存的最小一档", 0.00000001, 18, "10000000000"},
		{"八位小数全用满", 12.34567891, 18, "12345678910000000000"},
		{"六位精度的币也照样精确", 12.34, 8, "1234000000"},
		{"零", 0, 18, "0"},
		{"大额", 999999.99999999, 18, "999999999999990000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToTokenAmount(tc.amount, tc.decimals)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

// float64 存不下 0.07，存的是一个略大的二进制近似。乘上 1e18 之后那点误差
// 被放大到整数位上，变成实打实多发出去的 8 个最小单位。
//
// 这一条单拎出来，是因为它是「看起来完全正确的写法」唯一会露馅的地方：
// 0.07 这种金额太常见了，而多发 8 个 wei 没有任何人会发现——
// 直到有人把链上转出的总额和账本上的总额加起来对不上。
func TestToTokenAmountDoesNotDriftLikeAFloatMultiply(t *testing.T) {
	// 这个测试防的是一次具体的"简化"：把「格式化成定点串再当整数读」换成
	// 「用浮点连乘 10 放大到账本精度再截断」。两条路看起来等价，绝大多数金额上
	// 也确实等价——0.07、12.34 这些一试就过，所以这条错路很容易被放进来。
	//
	// 下面两个数是把这条错路真正试穿的地方。挑它们不是因为好看，是因为
	// 它们是穷举出来的最小反例（两位小数从 0.01 扫到 2000.00，第一个漏的是 0.47）。
	//
	// 截断的方向永远是**少发**：浮点连乘的误差把结果压到整数边界之下一点点，
	// Int() 再朝零截断，于是差额落在最后一位——而最后一位正是账本的最小单位。
	for _, tc := range []struct {
		name    string
		amount  float64
		want    string
		drifted string
	}{
		{
			name: "两位小数的寻常提现额", amount: 0.47,
			want:    "470000000000000000",
			drifted: "469999990000000000", // 少 0.00000001，正好一个账本单位
		},
		{
			name: "只有零头的小额", amount: 0.00000011,
			want:    "110000000000",
			drifted: "100000000000", // 少了将近一成
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToTokenAmount(tc.amount, 18)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())

			// 下面这段不是在测我们的代码，是在钉住那条错路的具体后果，
			// 免得将来有人以为两条路等价。
			scaled := new(big.Float).SetFloat64(tc.amount)
			for i := 0; i < ChainAmountScale; i++ {
				scaled.Mul(scaled, big.NewFloat(10))
			}
			units, _ := scaled.Int(nil)
			pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(18-ChainAmountScale), nil)
			assert.Equal(t, tc.drifted, units.Mul(units, pow).String())
			assert.NotEqual(t, tc.want, tc.drifted)
		})
	}
}

// 精度低于账本的币（以太坊主网/多数测试网的 USDT 是 6 位）：净额**向下取整**
// 到代币能表示的位数，零头留在金库——方向与手续费侧同一条原则，绝不多发。
//
// 早先这里测的是"直接拒绝"。第一次真链验证（BSC 测试网，6 位 USDT）证明
// 拒绝不可用：手续费是 8 位估算值，净额几乎必然带第 7、8 位的尾巴，每笔都拒。
func TestToTokenAmountFloorsToTokenPrecision(t *testing.T) {
	for name, tc := range map[string]struct {
		amount   float64
		decimals int
		want     string
	}{
		// 整额不受影响：1 USDT = 1_000000。
		"整额原样":  {1, 6, "1000000"},
		"两位小数":  {99.7, 6, "99700000"},
		// 第 7、8 位的尾巴被砍掉，且是**向下**：99.69999999 → 99.699999，
		// 不是四舍五入到 99.700000——入的方向是多发钱。
		"尾巴向下砍": {99.69999999, 6, "99699999"},
		"手续费尾巴": {0.12345678, 6, "123456"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ToTokenAmount(tc.amount, tc.decimals)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}

	t.Run("取整后归零仍然拒绝", func(t *testing.T) {
		// 这不是零头，是整笔钱小于代币最小单位——放行就是"安静地转账 0"，
		// 收款人什么都拿不到而单子标成 paid。
		_, err := ToTokenAmount(0.0000001, 6)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "below")
	})
	t.Run("零精度的币拒绝", func(t *testing.T) {
		_, err := ToTokenAmount(1, 0)
		require.Error(t, err)
	})
}

func TestToTokenAmountRefusesWhatItCannotPayExactly(t *testing.T) {
	t.Run("负数", func(t *testing.T) {
		_, err := ToTokenAmount(-1, 18)
		require.Error(t, err)
	})
	t.Run("NaN", func(t *testing.T) {
		// FormatFloat 会把它变成 "NaN"，SetString 解析失败后返回 0 ——
		// 也就是"安静地转账 0"。必须在更早的地方拦住。
		_, err := ToTokenAmount(math.NaN(), 18)
		require.Error(t, err)
	})
	t.Run("离谱大额", func(t *testing.T) {
		_, err := ToTokenAmount(1e20, 18)
		require.Error(t, err)
	})
}

// 假客户端的 TokenAddress：默认给一个显然是假的地址，NoToken 时恒假。
//
// 这两个分支是 M3 建单路径上的开关——前者让单子带着链上四项落库，
// 后者让它退回人工工单。联调时"为什么这张单子没走自动打款"的答案通常在这里。
func TestMockChainTokenAddressSwitchesTheSettlementPath(t *testing.T) {
	t.Run("默认给一个一眼假的地址", func(t *testing.T) {
		address, ok := NewMockChainClient(MockChainOptions{}).TokenAddress(SupplierPayoutNetworkBSC, "USDT")
		assert.True(t, ok)
		assert.Equal(t, MockChainTokenAddress, address)
		assert.Contains(t, address, "dead", "假地址长得像真的，联调时会被当成金库的真实配置抄走")
	})

	t.Run("NoToken 模拟金库里是另一种币", func(t *testing.T) {
		address, ok := NewMockChainClient(MockChainOptions{NoToken: true}).TokenAddress(SupplierPayoutNetworkBSC, "USDT")
		assert.False(t, ok)
		assert.Empty(t, address, "回假时还给了个地址，调用方漏看 ok 就会拿它去转账")
	})
}

func TestDisabledChainClientRefusesEverything(t *testing.T) {
	ctx := context.Background()
	client := NewDisabledChainClient()

	t.Run("每一个会动钱的方法都拒绝", func(t *testing.T) {
		_, err := client.NextNonce(ctx, SupplierPayoutNetworkBSC)
		assert.ErrorIs(t, err, ErrSupplierPayoutChainDisabled)

		_, err = client.Transfer(ctx, ChainTransferParams{Network: SupplierPayoutNetworkBSC})
		assert.ErrorIs(t, err, ErrSupplierPayoutChainDisabled)

		_, err = client.TransferBatch(ctx, ChainBatchParams{Network: SupplierPayoutNetworkBSC})
		assert.ErrorIs(t, err, ErrSupplierPayoutChainDisabled)

		_, err = client.EnsureBatchAllowance(ctx, ChainBatchParams{Network: SupplierPayoutNetworkBSC})
		assert.ErrorIs(t, err, ErrSupplierPayoutChainDisabled)

		assert.False(t, client.SupportsBatch(SupplierPayoutNetworkBSC))
	})

	t.Run("等待确认回错，不回 failed", func(t *testing.T) {
		// 这是这个类型里唯一需要想一下的地方。failed 是终态，M4 见了会推进单子
		// 并全额退款；而真实情况是「我们压根没广播过」——退款等于凭空发钱。
		confirmation, err := client.WaitForConfirmation(ctx, SupplierPayoutNetworkBSC, "0xdead")
		assert.ErrorIs(t, err, ErrSupplierPayoutChainDisabled)
		assert.NotEqual(t, ChainTxFailed, confirmation.Status)
		assert.Empty(t, confirmation.Status)
	})
}

func TestMockChainClientNonceOnlyMovesAfterABroadcast(t *testing.T) {
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})

	first, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), first)

	// 只是问，没有发——再问一次必须还是同一个号。真实节点就是这样，
	// 而 worker 的「取了 nonce 但广播失败 → 重试再取」这条路正是靠它。
	again, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, first, again)

	_, err = mock.Transfer(ctx, ChainTransferParams{
		Network: SupplierPayoutNetworkBSC, Token: "0xtoken", To: "0xto", Amount: 1,
	})
	require.NoError(t, err)

	after, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), after)
}

func TestMockChainClientFailedBroadcastLeavesNonceAlone(t *testing.T) {
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})
	mock.FailNextTransfer("rpc timeout")

	_, err := mock.Transfer(ctx, ChainTransferParams{Network: SupplierPayoutNetworkBSC, Amount: 1})
	require.Error(t, err)

	nonce, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), nonce)
	assert.Empty(t, mock.Transfers(), "没发出去的不该记进已广播清单")

	// 一次性的：下一笔正常。
	_, err = mock.Transfer(ctx, ChainTransferParams{Network: SupplierPayoutNetworkBSC, Amount: 1})
	require.NoError(t, err)
	assert.Len(t, mock.Transfers(), 1)
}

func TestMockChainClientBumpNonceSimulatesAnOutsideTransaction(t *testing.T) {
	// 金库地址上任何一笔与 worker 无关的交易都会这样推进 nonce——
	// 批量补额度的 approve、运维手动转账。「预留的号被吃掉」是真实故障。
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})
	mock.BumpNonce(SupplierPayoutNetworkBSC)

	nonce, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), nonce)
}

func TestMockChainTxHashIsObviouslyFake(t *testing.T) {
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})

	result, err := mock.Transfer(ctx, ChainTransferParams{
		Network: SupplierPayoutNetworkBSC, Token: "0xtoken", To: "0xto", Amount: 1,
	})
	require.NoError(t, err)

	// 合法的交易哈希是 0x + 64 位十六进制。这个刻意不是——运营把它粘进区块浏览器
	// 得到的应该是"这不是个哈希"，而不是"查无此笔"（后者会被当成节点同步延迟，
	// 真相却是这套部署的打款从来没开过）。
	assert.False(t, strings.HasPrefix(result.TxHash, "0x"))
	assert.True(t, IsMockChainTx(result.TxHash))
	assert.False(t, IsMockChainTx("0x098500f3809def22e782be21670b97b1e362b4249de3232aebf7c5b4c1d78386"))

	// 两次广播必须给出不同的哈希：链上重发同一个 nonce 也会因 gasPrice 不同而
	// 换一个哈希，测试要能分辨"这是第几次发的"。
	second, err := mock.Transfer(ctx, ChainTransferParams{
		Network: SupplierPayoutNetworkBSC, Token: "0xtoken", To: "0xto", Amount: 1,
	})
	require.NoError(t, err)
	assert.NotEqual(t, result.TxHash, second.TxHash)
}

func TestMockChainClientBatchDefaultsToSupported(t *testing.T) {
	ctx := context.Background()

	// 零值就支持批量。反过来（默认不支持）会让 M5 那些分组测试全部安静地
	// 走进逐笔分支——它们照样是绿的，只是测的不是要测的东西。
	on := NewMockChainClient(MockChainOptions{})
	assert.True(t, on.SupportsBatch(SupplierPayoutNetworkBSC))
	result, err := on.TransferBatch(ctx, ChainBatchParams{
		Network: SupplierPayoutNetworkBSC, Token: "0xtoken",
		Items: []ChainBatchItem{{To: "0xa", Amount: 1}, {To: "0xb", Amount: 2}},
	})
	require.NoError(t, err)
	assert.True(t, IsMockChainTx(result.TxHash))
	require.Len(t, on.Batches(), 1)
	assert.Len(t, on.Batches()[0].Items, 2)

	off := NewMockChainClient(MockChainOptions{NoBatch: true})
	assert.False(t, off.SupportsBatch(SupplierPayoutNetworkBSC))
	_, err = off.TransferBatch(ctx, ChainBatchParams{Network: SupplierPayoutNetworkBSC})
	assert.ErrorIs(t, err, ErrSupplierPayoutChainNoBatch)
}

func TestMockChainClientOutcomeIsConfigurable(t *testing.T) {
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})

	confirmation, err := mock.WaitForConfirmation(ctx, SupplierPayoutNetworkBSC, "mock:x")
	require.NoError(t, err)
	assert.Equal(t, ChainTxConfirmed, confirmation.Status)
	assert.Empty(t, confirmation.Reason)

	mock.SetOutcome(ChainTxFailed)
	confirmation, err = mock.WaitForConfirmation(ctx, SupplierPayoutNetworkBSC, "mock:x")
	require.NoError(t, err)
	assert.Equal(t, ChainTxFailed, confirmation.Status)
	assert.NotEmpty(t, confirmation.Reason, "失败必须带上原因，它会进单子的 last_error")
}

// M4 的 worker 会并发调这个 mock。竞态在这里的表现不是崩溃而是 nonce 少加一次，
// 而少加一次 nonce 正好等于两张单子拿到同一个号——测试环境里的双付。
func TestMockChainClientIsSafeUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	mock := NewMockChainClient(MockChainOptions{})

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = mock.Transfer(ctx, ChainTransferParams{
				Network: SupplierPayoutNetworkBSC, Token: "0xtoken", To: "0xto", Amount: 1,
			})
		}()
	}
	wg.Wait()

	nonce, err := mock.NextNonce(ctx, SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, uint64(n), nonce)
	assert.Len(t, mock.Transfers(), n)
}
