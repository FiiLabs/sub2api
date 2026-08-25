// APEXONE-EXT: 真客户端的测试。
//
// 全部对着 httptest 起的假节点跑。这个包的任何一个测试都不会碰真实节点，
// 也不会用到任何一把真私钥——signer 用的是写死在 tx_test.go 里的测试常量。
package payoutchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// fakeNode 是一个可编排的假节点：每个方法给一个处理函数，
// 处理函数可以按调用次数给出不同的回应。
type fakeNode struct {
	mu       sync.Mutex
	handlers map[string]func(call int, params []any) (any, *rpcError)
	calls    map[string]int
	seen     []captured
}

func newFakeNode() *fakeNode {
	return &fakeNode{
		handlers: map[string]func(int, []any) (any, *rpcError){},
		calls:    map[string]int{},
	}
}

// on 让某个方法恒定回一个值。
func (n *fakeNode) on(method string, result any) *fakeNode {
	return n.onFunc(method, func(int, []any) (any, *rpcError) { return result, nil })
}

// onFunc 让某个方法按调用次数回不同的值（call 从 0 数起）。
func (n *fakeNode) onFunc(method string, fn func(call int, params []any) (any, *rpcError)) *fakeNode {
	n.handlers[method] = fn
	return n
}

func (n *fakeNode) countOf(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *fakeNode) paramsOf(method string) [][]any {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out [][]any
	for _, c := range n.seen {
		if c.Method == method {
			out = append(out, c.Params)
		}
	}
	return out
}

// start 起服务并造一个连着它的客户端。cfg 里没填的项用一份可用的默认。
func (n *fakeNode) start(t *testing.T, tweak func(*Config)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req captured
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		n.mu.Lock()
		call := n.calls[req.Method]
		n.calls[req.Method]++
		n.seen = append(n.seen, req)
		handler := n.handlers[req.Method]
		n.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if handler == nil {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unstubbed %s"}}`, req.Method)
			return
		}
		result, rpcErr := handler(call, req.Params)
		if rpcErr != nil {
			encoded, err := json.Marshal(rpcErr)
			require.NoError(t, err)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":%s}`, encoded)
			return
		}
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%s}`, encoded)
	}))
	t.Cleanup(server.Close)

	cfg := Config{
		Enabled:      true,
		RPCURL:       server.URL,
		SignerKey:    keyBSC,
		TokenAddress: addrBSCUSDT,
		// 生产的 LoadConfig 恒有这个默认值；夹具也得带上，否则 checkToken
		// 的符号分支会把每一笔 Token:"USDT" 的测试转账当成发错币拒掉。
		TokenSymbol:   "USDT",
		ChainID:       56,
		Confirmations: 3,
		FallbackFee:   0.5,
		FeeMultiplier: 1.5,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	client, err := New(cfg, server.Client())
	require.NoError(t, err)
	client.pollEvery = time.Millisecond // 测试里别真等 3 秒一轮
	return client
}

// 一个"精度是 18"的 eth_call 回应。
const decimals18 = "0x0000000000000000000000000000000000000000000000000000000000000012"

func nonceOf(value uint64) *uint64 { return &value }

func TestTreasuryAddressComesFromTheKey(t *testing.T) {
	client := newFakeNode().start(t, nil)
	assert.Equal(t, "0xfcad0b19bb29d4674531d6f115237e16afce377c", client.TreasuryAddress())
}

func TestVerifyChainCatchesAWrongNetworkRPC(t *testing.T) {
	t.Run("对上了", func(t *testing.T) {
		client := newFakeNode().on("eth_chainId", "0x38").start(t, nil)
		require.NoError(t, client.VerifyChain(context.Background()))
	})
	t.Run("RPC 指向测试网而配置写着主网", func(t *testing.T) {
		// 不查的话，表现是每一笔交易都被拒，错误消息里只有 "invalid sender"。
		client := newFakeNode().on("eth_chainId", "0x61").start(t, nil) // 0x61 = 97, BSC 测试网
		err := client.VerifyChain(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "97")
		assert.Contains(t, err.Error(), "56")
	})
}

func TestTransferSignsWithTheNonceItWasGiven(t *testing.T) {
	// 这一条是整个重试策略的地基。给定 nonce、gas 价、金额、收款人，
	// 签出来的交易是确定的一串字节——tx_test.go 里那个金标向量正是
	// nonce=42 / gasPrice=1e9 / 收款人 de709f… / 12345678901234567890 的组合。
	//
	// 这里用同样的参数，验证 Transfer 走完一整圈之后广播出去的仍是那一串。
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0xdeadbeef")
	client := node.start(t, nil)

	result, err := client.Transfer(context.Background(), service.ChainTransferParams{
		Network: "bsc",
		Token:   "USDT",
		To:      addrRecipient,
		Amount:  12.3456789, // ×1e18 → 12345678900000000000
		Nonce:   nonceOf(42),
	})
	require.NoError(t, err)

	// 交易哈希本地算，不是节点回的那个 0xdeadbeef。
	assert.NotEqual(t, "0xdeadbeef", result.TxHash)
	assert.True(t, strings.HasPrefix(result.TxHash, "0x"))
	assert.Len(t, result.TxHash, 66)

	// 广播出去的原始字节里，收款地址和金额都能对上。
	raw, ok := node.paramsOf("eth_sendRawTransaction")[0][0].(string)
	require.True(t, ok)
	assert.Contains(t, raw, "a9059cbb", "应当是一次 ERC-20 transfer")
	assert.Contains(t, raw, strings.TrimPrefix(addrRecipient, "0x"))
	assert.Contains(t, raw, "ab54a98ca1890800", "12345678900000000000 的十六进制")
	assert.True(t, strings.HasPrefix(raw, "0xf8aa2a"), "nonce 42 应当原样落进 RLP，实际: %s", raw[:16])
}

func TestTransferReusesTheGivenNonceByteForByteOnRetry(t *testing.T) {
	// 重发一笔广播超时的交易，必须签出**完全相同**的字节。差一个字节，
	// 链上就认成另一笔独立的转账——供给者收到两次钱。
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, nil)

	send := func() (string, string) {
		result, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 5, Nonce: nonceOf(7),
		})
		require.NoError(t, err)
		sent := node.paramsOf("eth_sendRawTransaction")
		rawParam, ok := sent[len(sent)-1][0].(string)
		require.True(t, ok)
		return result.TxHash, rawParam
	}
	firstHash, firstRaw := send()
	secondHash, secondRaw := send()

	assert.Equal(t, firstRaw, secondRaw)
	assert.Equal(t, firstHash, secondHash)

	// 全程没有再问过 nonce——问了就说明"复用"只是碰巧。
	assert.Zero(t, node.countOf("eth_getTransactionCount"))
}

func TestTransferAsksForANonceOnlyWhenNotGivenOne(t *testing.T) {
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_getTransactionCount", "0x9").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, nil)

	_, err := client.Transfer(context.Background(), service.ChainTransferParams{
		Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nil,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, node.countOf("eth_getTransactionCount"))
}

func TestAlreadyBroadcastIsNotAFailure(t *testing.T) {
	// 这四种回应都意味着"这个 nonce 上的交易已经在了"。当成失败会走退款分支，
	// 而那笔钱可能正在路上——一次双付。
	for _, message := range []string{
		"already known",
		"nonce too low",
		"ALREADY KNOWN", // 大小写不该影响判断
		"transaction already in the pool",
	} {
		t.Run(message, func(t *testing.T) {
			node := newFakeNode().
				on("eth_call", decimals18).
				on("eth_gasPrice", "0x3b9aca00").
				onFunc("eth_sendRawTransaction", func(int, []any) (any, *rpcError) {
					return nil, &rpcError{Code: -32000, Message: message}
				})
			client := node.start(t, nil)

			result, err := client.Transfer(context.Background(), service.ChainTransferParams{
				Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(3),
			})
			require.NoError(t, err, "已经广播过不是失败")
			assert.Len(t, result.TxHash, 66, "仍然要给出哈希，好让调用方去等确认")
		})
	}
}

func TestARealRejectionIsStillAFailure(t *testing.T) {
	// 反过来，真正的拒绝不能被当成"已经发过了"——那样会让一笔从没发出去的
	// 转账被记成已发，供给者的余额被清零而钱没动。
	for _, message := range []string{
		"insufficient funds for gas * price + value",
		"intrinsic gas too low",
		"replacement transaction underpriced",
	} {
		t.Run(message, func(t *testing.T) {
			node := newFakeNode().
				on("eth_call", decimals18).
				on("eth_gasPrice", "0x3b9aca00").
				onFunc("eth_sendRawTransaction", func(int, []any) (any, *rpcError) {
					return nil, &rpcError{Code: -32000, Message: message}
				})
			client := node.start(t, nil)

			_, err := client.Transfer(context.Background(), service.ChainTransferParams{
				Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(3),
			})
			require.Error(t, err)
		})
	}
}

func TestTransferRefusesPaymentsThatWouldVanish(t *testing.T) {
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, nil)
	ctx := context.Background()

	t.Run("零地址", func(t *testing.T) {
		// 零地址是合法 EVM 地址，转给它交易会**成功**，钱被烧掉。
		_, err := client.Transfer(ctx, service.ChainTransferParams{
			Network: "bsc", Token: "USDT",
			To: "0x0000000000000000000000000000000000000000", Amount: 1, Nonce: nonceOf(1),
		})
		require.Error(t, err)
	})
	t.Run("零金额", func(t *testing.T) {
		_, err := client.Transfer(ctx, service.ChainTransferParams{
			Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 0, Nonce: nonceOf(1),
		})
		require.Error(t, err)
	})
	t.Run("地址不成形", func(t *testing.T) {
		_, err := client.Transfer(ctx, service.ChainTransferParams{
			Network: "bsc", Token: "USDT", To: "0xnope", Amount: 1, Nonce: nonceOf(1),
		})
		require.Error(t, err)
	})
	// 上面三条都不该有任何东西被广播出去。
	assert.Zero(t, node.countOf("eth_sendRawTransaction"))
}

func TestClientRefusesOtherNetworks(t *testing.T) {
	// 一个实例只管一条链。EVM 地址跨链同形，所以"发到另一条链上"这件事
	// 不会有任何一步报错——只会钱到不了。
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, nil)
	ctx := context.Background()

	_, err := client.Transfer(ctx, service.ChainTransferParams{
		Network: "ethereum", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(1),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrSupplierPayoutChainDisabled)

	_, err = client.NextNonce(ctx, "polygon")
	require.Error(t, err)
	assert.False(t, client.SupportsBatch("ethereum"))
	assert.Zero(t, node.countOf("eth_sendRawTransaction"))

	t.Run("大小写和空白不算另一条链", func(t *testing.T) {
		_, err := client.Transfer(ctx, service.ChainTransferParams{
			Network: " BSC ", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(1),
		})
		require.NoError(t, err)
	})
}

func TestTokenDecimalsIsAskedOnceAndUsedForTheAmount(t *testing.T) {
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, nil)

	for i := 0; i < 3; i++ {
		_, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(uint64(i)),
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 1, node.countOf("eth_call"), "精度不变，问一次就够")
}

func TestTokenDecimalsRefusesAnImplausibleAnswer(t *testing.T) {
	// 精度读成 0，所有金额会少放大 18 个量级——一笔 100 USDT 变成 0.0000000000000001。
	// 交易照样成功，链上看不出任何异常。
	for _, tc := range []struct{ name, result string }{
		{"空返回值（这地址上根本没有 decimals()）", "0x"},
		{"精度是 0", "0x" + strings.Repeat("0", 64)},
		{"精度大得离谱", "0x" + strings.Repeat("0", 62) + "ff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := newFakeNode().
				on("eth_call", tc.result).
				on("eth_gasPrice", "0x3b9aca00").
				on("eth_sendRawTransaction", "0x00")
			client := node.start(t, nil)

			_, err := client.Transfer(context.Background(), service.ChainTransferParams{
				Network: "bsc", Token: "USDT", To: addrRecipient, Amount: 1, Nonce: nonceOf(1),
			})
			require.Error(t, err)
			assert.Zero(t, node.countOf("eth_sendRawTransaction"))
		})
	}
}

func TestEstimateFeeFallsBackVisiblyInsteadOfFailing(t *testing.T) {
	ctx := context.Background()

	t.Run("估到了", func(t *testing.T) {
		node := newFakeNode().on("eth_gasPrice", "0x3b9aca00") // 1 Gwei
		client := node.start(t, func(c *Config) { c.NativeUSD = 600 })

		got := client.EstimateFee(ctx, "bsc")
		assert.True(t, got.Estimated)
		assert.Equal(t, "1000000000", got.GasPriceWei)
		assert.Equal(t, uint64(gasERC20Transfer), got.GasLimit)
		// 1e9 wei × 100000 gas = 1e14 wei = 0.0001 BNB；×600 美元 ×1.5 = 0.09
		assert.InDelta(t, 0.09, got.Amount, 1e-9)
	})

	t.Run("节点挂了也要给得出一个数", func(t *testing.T) {
		// 一次 RPC 抖动不该让供给者在提现页看到"暂时不可用"——他会以为
		// 是自己的账号出了问题。回落，但把 Estimated 置假让降级可见。
		client := newFakeNode().start(t, func(c *Config) { c.NativeUSD = 600 })

		got := client.EstimateFee(ctx, "bsc")
		assert.False(t, got.Estimated)
		assert.Equal(t, 0.5, got.Amount)
	})

	t.Run("没配 BNB 价就用固定值", func(t *testing.T) {
		// 明确选择不接喂价——多一个会挂、会被操纵的外部依赖，
		// 换来的只是一笔小手续费更准一点。
		node := newFakeNode().on("eth_gasPrice", "0x3b9aca00")
		client := node.start(t, func(c *Config) { c.NativeUSD = 0 })

		got := client.EstimateFee(ctx, "bsc")
		assert.False(t, got.Estimated)
		assert.Equal(t, 0.5, got.Amount)
		assert.Zero(t, node.countOf("eth_gasPrice"), "不换算就不必问价")
	})

	t.Run("别的链也回落而不是炸", func(t *testing.T) {
		client := newFakeNode().start(t, nil)
		assert.Equal(t, 0.5, client.EstimateFee(ctx, "ethereum").Amount)
	})
}

func TestWaitForConfirmationTellsApartTheThreeOutcomes(t *testing.T) {
	t.Run("链上明确 revert 才算失败", func(t *testing.T) {
		// 这是唯一一种可以放心退款的失败。
		node := newFakeNode().
			on("eth_getTransactionReceipt", map[string]string{"status": "0x0", "blockNumber": "0x64"})
		client := node.start(t, nil)

		got, err := client.WaitForConfirmation(context.Background(), "bsc", "0xabc")
		require.NoError(t, err)
		assert.Equal(t, service.ChainTxFailed, got.Status)
		assert.NotEmpty(t, got.Reason, "失败必须带原因，否则运维只看到一个空状态")
	})

	t.Run("埋够深了才算确认", func(t *testing.T) {
		node := newFakeNode().
			on("eth_getTransactionReceipt", map[string]string{"status": "0x1", "blockNumber": "0x64"}).
			onFunc("eth_blockNumber", func(call int, _ []any) (any, *rpcError) {
				// 0x64=100 上链。前两轮链头还只到 100、101（1、2 个确认），
				// 第三轮到 102，够 3 个确认。
				return fmt.Sprintf("0x%x", 100+call), nil
			})
		client := node.start(t, nil)

		got, err := client.WaitForConfirmation(context.Background(), "bsc", "0xabc")
		require.NoError(t, err)
		assert.Equal(t, service.ChainTxConfirmed, got.Status)
		assert.Equal(t, 3, node.countOf("eth_blockNumber"), "不该在确认数不够时就返回")
	})

	t.Run("等不到是「不知道」，不是失败", func(t *testing.T) {
		// 判成 failed 会让一笔可能已经成功的转账被退款——双付。
		node := newFakeNode().on("eth_getTransactionReceipt", nil) // 永远还没上链
		client := node.start(t, nil)

		// 轮询间隔特意调得比 ctx 长。否则本地 httptest 快到 ctx 会在某次 HTTP
		// 调用当中过期，函数从"节点连不上"那条路返回——那条路另有测试，而
		// 真正的超时分支（select 里的 ctx.Done）就没人看着了。
		// 这里让第一次收据查询顺利返回 nil，然后必然停在 select 上等 ctx。
		client.pollEvery = 2 * time.Second

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		got, err := client.WaitForConfirmation(ctx, "bsc", "0xabc")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded, "超时要能被上游认出来是超时")
		assert.Contains(t, err.Error(), "0xabc", "错误里要带交易哈希，否则运维无从查起")
		assert.NotEqual(t, service.ChainTxFailed, got.Status)
		assert.Empty(t, got.Status)
		assert.Equal(t, 1, node.countOf("eth_getTransactionReceipt"))
	})

	t.Run("节点连不上也是「不知道」", func(t *testing.T) {
		client := newFakeNode().start(t, nil) // eth_getTransactionReceipt 没打桩 → 回 RPC 错误
		got, err := client.WaitForConfirmation(context.Background(), "bsc", "0xabc")
		require.Error(t, err)
		assert.Empty(t, got.Status)
	})
}

func TestConfirmationSurvivesANodeThatGoesBackwards(t *testing.T) {
	// 多节点负载均衡后面，两次 eth_blockNumber 可能落在不同高度上，
	// 后一次比前一次矮。head-mined 会下溢成一个天文数字，
	// 于是一笔刚上链的交易被判成"确认充分"。
	node := newFakeNode().
		on("eth_getTransactionReceipt", map[string]string{"status": "0x1", "blockNumber": "0x64"}).
		onFunc("eth_blockNumber", func(call int, _ []any) (any, *rpcError) {
			if call == 0 {
				return "0x50", nil // 80 < 100，链头倒退了
			}
			return "0x66", nil // 102，够 3 个确认
		})
	client := node.start(t, nil)

	got, err := client.WaitForConfirmation(context.Background(), "bsc", "0xabc")
	require.NoError(t, err)
	assert.Equal(t, service.ChainTxConfirmed, got.Status)
	assert.Equal(t, 2, node.countOf("eth_blockNumber"), "倒退的那一轮应当继续等而不是判成功")
}

func TestBatchIsOffUntilTheDisperseContractIsConfigured(t *testing.T) {
	// 批量是省 gas 的优化，不是功能前提。没配合约就安静降级到逐笔，
	// 而不是让整条提现链路报错。
	client := newFakeNode().start(t, nil)
	assert.False(t, client.SupportsBatch("bsc"))

	_, err := client.TransferBatch(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	assert.ErrorIs(t, err, service.ErrSupplierPayoutChainNoBatch)

	_, err = client.EnsureBatchAllowance(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	assert.ErrorIs(t, err, service.ErrSupplierPayoutChainNoBatch)
}

func TestTransferBatchPacksEveryRecipient(t *testing.T) {
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, func(c *Config) { c.DisperseAddress = addrOther })
	require.True(t, client.SupportsBatch("bsc"))

	_, err := client.TransferBatch(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{
			{To: addrRecipient, Amount: 1},
			{To: addrBSCUSDT, Amount: 0.25},
		},
		Nonce: nonceOf(11),
	})
	require.NoError(t, err)

	raw, ok := node.paramsOf("eth_sendRawTransaction")[0][0].(string)
	require.True(t, ok)
	assert.Contains(t, raw, "c73a2d60", "应当是一次 disperseToken")
	assert.Contains(t, raw, strings.TrimPrefix(addrRecipient, "0x"))
	assert.Contains(t, raw, strings.ToLower(strings.TrimPrefix(addrBSCUSDT, "0x")))
	assert.Contains(t, raw, "0de0b6b3a7640000", "1.0")
	assert.Contains(t, raw, "03782dace9d90000", "0.25")
	// 发给的是批量合约，不是币合约。
	assert.Contains(t, raw, strings.ToLower(strings.TrimPrefix(addrOther, "0x")))
}

func TestTransferBatchRefusesBatchesTheChainWouldReject(t *testing.T) {
	node := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00")
	client := node.start(t, func(c *Config) { c.DisperseAddress = addrOther })
	ctx := context.Background()

	t.Run("空批次", func(t *testing.T) {
		_, err := client.TransferBatch(ctx, service.ChainBatchParams{Network: "bsc", Token: "USDT"})
		require.Error(t, err)
	})
	t.Run("超过 100 个收款人", func(t *testing.T) {
		// 太大的批次会超区块 gas 上限，整批失败——而 all-or-nothing 意味着
		// 一次失败拖垮所有人的提现。
		items := make([]service.ChainBatchItem, maxBatchRecipients+1)
		for i := range items {
			items[i] = service.ChainBatchItem{To: addrRecipient, Amount: 1}
		}
		_, err := client.TransferBatch(ctx, service.ChainBatchParams{
			Network: "bsc", Token: "USDT", Items: items,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "100")
	})
	t.Run("其中一个是零金额", func(t *testing.T) {
		// 合约会因 ZeroAmount revert **整批**，gas 照扣。
		_, err := client.TransferBatch(ctx, service.ChainBatchParams{
			Network: "bsc", Token: "USDT",
			Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}, {To: addrOther, Amount: 0}},
		})
		require.Error(t, err)
	})
	t.Run("其中一个是零地址", func(t *testing.T) {
		_, err := client.TransferBatch(ctx, service.ChainBatchParams{
			Network: "bsc", Token: "USDT",
			Items: []service.ChainBatchItem{
				{To: addrRecipient, Amount: 1},
				{To: "0x0000000000000000000000000000000000000000", Amount: 1},
			},
		})
		require.Error(t, err)
	})
	assert.Zero(t, node.countOf("eth_sendRawTransaction"))
}

func TestEnsureBatchAllowanceSkipsTheChainWhenTheAllowanceIsEnough(t *testing.T) {
	// 额度够就一笔交易都不该发——每批 approve 一次是白烧 gas，
	// 而那笔 gas 同样是从供给者收益里扣的。
	node := newFakeNode().
		onFunc("eth_call", func(call int, params []any) (any, *rpcError) {
			call0, _ := params[0].(map[string]any)
			data, _ := call0["data"].(string)
			if strings.HasPrefix(data, "0x313ce567") {
				return decimals18, nil
			}
			return "0x" + strings.Repeat("f", 64), nil // allowance 拉满
		}).
		on("eth_gasPrice", "0x3b9aca00")
	client := node.start(t, func(c *Config) { c.DisperseAddress = addrOther })

	topUp, err := client.EnsureBatchAllowance(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	require.NoError(t, err)
	assert.Nil(t, topUp, "额度够用时返回 nil 表示没动链")
	assert.Zero(t, node.countOf("eth_sendRawTransaction"))
}

func TestEnsureBatchAllowanceApprovesAndWaitsBeforeReturning(t *testing.T) {
	// approve 必须先确认再发批量：批量交易若抢在 approve 前被打包，
	// 整批因额度不足 revert，gas 照扣。
	node := newFakeNode().
		onFunc("eth_call", func(_ int, params []any) (any, *rpcError) {
			call0, _ := params[0].(map[string]any)
			data, _ := call0["data"].(string)
			if strings.HasPrefix(data, "0x313ce567") {
				return decimals18, nil
			}
			return "0x" + strings.Repeat("0", 64), nil // 额度是 0
		}).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_getTransactionCount", "0x5").
		on("eth_sendRawTransaction", "0x00").
		on("eth_getTransactionReceipt", map[string]string{"status": "0x1", "blockNumber": "0x64"}).
		on("eth_blockNumber", "0x66")
	client := node.start(t, func(c *Config) { c.DisperseAddress = addrOther })

	topUp, err := client.EnsureBatchAllowance(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, topUp)
	assert.Equal(t, "USDT", topUp.Symbol)
	assert.Len(t, topUp.TxHash, 66)

	raw, ok := node.paramsOf("eth_sendRawTransaction")[0][0].(string)
	require.True(t, ok)
	assert.Contains(t, raw, "095ea7b3", "应当是一次 approve")
	assert.Contains(t, raw, strings.ToLower(strings.TrimPrefix(addrOther, "0x")), "授权给批量合约")
	assert.Contains(t, raw, strings.Repeat("f", 64), "补到无限额度")
	assert.NotZero(t, node.countOf("eth_getTransactionReceipt"), "必须等 approve 确认")
}

func TestEnsureBatchAllowanceReportsARevertedApprove(t *testing.T) {
	node := newFakeNode().
		onFunc("eth_call", func(_ int, params []any) (any, *rpcError) {
			call0, _ := params[0].(map[string]any)
			data, _ := call0["data"].(string)
			if strings.HasPrefix(data, "0x313ce567") {
				return decimals18, nil
			}
			return "0x" + strings.Repeat("0", 64), nil
		}).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_getTransactionCount", "0x5").
		on("eth_sendRawTransaction", "0x00").
		on("eth_getTransactionReceipt", map[string]string{"status": "0x0", "blockNumber": "0x64"})
	client := node.start(t, func(c *Config) { c.DisperseAddress = addrOther })

	_, err := client.EnsureBatchAllowance(context.Background(), service.ChainBatchParams{
		Network: "bsc", Token: "USDT",
		Items: []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	require.Error(t, err, "approve 没成，不能让批量接着发")
}

func TestClientSatisfiesTheServiceInterface(t *testing.T) {
	// 编译期已经有 var _ service.SupplierChainClient = (*Client)(nil) 兜着，
	// 这里再从调用方的角度走一遍：真客户端和 mock 必须能互换。
	var client service.SupplierChainClient = newFakeNode().start(t, nil)
	assert.False(t, client.SupportsBatch("bsc"))
}

// ============================================================================
// 发错币的闸（checkToken）
// ============================================================================

// 单子钉的币与金库配的币不一致 → 拒绝广播。
//
// 这条只会在「建单之后有人换了金库配置」时触发，而它不拦的话链上不会报
// 任何错——交易成功、事件正常，只是收款人拿到了另一种币。拒绝让单子带着
// 明确的 last_error 走 failed → 运营，那是唯一不出错的动作。
func TestTransferRefusesTheWrongCoin(t *testing.T) {
	client := newFakeNode().
		on("eth_call", decimals18).
		on("eth_gasPrice", "0x3b9aca00").
		on("eth_sendRawTransaction", "0x00").
		start(t, nil)

	t.Run("按地址钉（worker 的快照路径）", func(t *testing.T) {
		_, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc",
			Token:   "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", // 另一个合约（USDC）
			To:      addrRecipient, Amount: 1,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wrong coin")
	})
	t.Run("按符号钉（展示路径）", func(t *testing.T) {
		_, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc", Token: "USDC", To: addrRecipient, Amount: 1,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wrong coin")
	})
	t.Run("地址快照与配置一致时放行（大小写无关）", func(t *testing.T) {
		// 走到广播是这条子测的成功条件——用一个会挂的假节点也无妨，
		// 只要错误不是 wrong coin。
		_, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc", Token: addrBSCUSDT, To: addrRecipient, Amount: 1,
		})
		if err != nil {
			assert.NotContains(t, err.Error(), "wrong coin")
		}
	})
	t.Run("空串放行（调用方没有要钉的东西）", func(t *testing.T) {
		_, err := client.Transfer(context.Background(), service.ChainTransferParams{
			Network: "bsc", Token: "", To: addrRecipient, Amount: 1,
		})
		if err != nil {
			assert.NotContains(t, err.Error(), "wrong coin")
		}
	})
}

// 批量同一道闸：整组发错币比单笔更贵——一笔交易里全部收款人都拿错。
func TestTransferBatchRefusesTheWrongCoin(t *testing.T) {
	client := newFakeNode().start(t, func(cfg *Config) { cfg.DisperseAddress = addrOther })

	_, err := client.TransferBatch(context.Background(), service.ChainBatchParams{
		Network: "bsc",
		Token:   "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d",
		Items:   []service.ChainBatchItem{{To: addrRecipient, Amount: 1}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong coin")
}
