//go:build livetest

// APEXONE-EXT: 双边市场——链上打款的**真链**验证（BSC 测试网）。
//
// 这个文件不进任何常规测试跑道（单独的 livetest 标签），因为它做的事与单元
// 测试相反：真的花 gas、真的动链上状态。它存在的理由是 §9.5 的金标向量只能
// 证明"编码等价于 go-ethereum"，证明不了"节点收下并打包了我们签的交易"——
// 后者只有对着一条真链跑一遍才知道。
//
// 运行方式（配置全走环境变量，与生产同一套读法）：
//
//	PAYOUT_ENABLED=true \
//	PAYOUT_BSC_RPC_URL=https://bsc-testnet.publicnode.com \
//	PAYOUT_BSC_CHAIN_ID=97 \
//	PAYOUT_BSC_SIGNER_KEY=0x… \
//	PAYOUT_BSC_TOKEN_ADDRESS=0x… \
//	PAYOUT_BSC_DISPERSE_ADDRESS=0x… \
//	go test -tags livetest -count=1 -v -timeout 10m -run TestLive ./internal/payoutchain/
//
// 转账全部**打给金库自己**：ERC-20 self-transfer 在链上完全合法，事件、
// 余额变动、确认流程与打给别人一模一样，而代币一分不少——只有 gas 是真花的。
package payoutchain

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveAmount 每笔转 0.01 个代币。金额本身不重要（打给自己），
// 但刻意带小数：整数金额撞不上 ToTokenAmount 的定点换算路径。
const liveAmount = 0.01

func TestLiveBSCTestnet(t *testing.T) {
	cfg, err := LoadConfig()
	require.NoError(t, err)
	if !cfg.Enabled || cfg.Mock {
		t.Skip("livetest 需要 PAYOUT_ENABLED=true 且不带 PAYOUT_MOCK")
	}
	require.NotEqual(t, uint64(56), cfg.ChainID,
		"这个测试会真的广播交易——拒绝在主网上跑（PAYOUT_BSC_CHAIN_ID 是 56）")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// ---- 1. 工厂 + 链 ID 自检（与生产启动同一条路）----
	resolved, err := Resolve(ctx, cfg, &http.Client{Timeout: 30 * time.Second})
	require.NoError(t, err, "Resolve/VerifyChain 失败——链 ID 或 RPC 配置不对")
	require.Equal(t, ModeLive, resolved.Mode)
	t.Logf("startup: %s", resolved.Summary)

	client, ok := resolved.Client.(*Client)
	require.True(t, ok)
	treasury := client.TreasuryAddress()
	t.Logf("treasury: %s", treasury)

	// ---- 2. 余额与合约状态 ----
	vault, err := parseAddress(treasury)
	require.NoError(t, err)

	var bnbHex string
	require.NoError(t, client.rpc.call(ctx, &bnbHex, "eth_getBalance", treasury, "latest"))
	bnbWei, ok := new(big.Int).SetString(strings.TrimPrefix(bnbHex, "0x"), 16)
	require.True(t, ok)
	t.Logf("tBNB balance: %s wei (~%s BNB)", bnbWei, weiToDecimal(bnbWei, 18))
	require.True(t, bnbWei.Sign() > 0,
		"金库没有 tBNB，付不了 gas——先去水龙头领一点再跑：%s", treasury)

	decimals, err := client.tokenDecimals(ctx)
	require.NoError(t, err, "token 合约的 decimals() 读不出来——地址可能不是一个 ERC-20")
	tokenBalance := erc20Balance(t, ctx, client, vault)
	t.Logf("token: %s decimals=%d balance=%s", cfg.TokenAddress, decimals, weiToDecimal(tokenBalance, decimals))

	// 单笔 + 批量两笔 = 3 × liveAmount，全部打给自己，只需要余额盖得住换算。
	needed := tokenUnits(t, 3*liveAmount, decimals)
	require.True(t, tokenBalance.Cmp(needed) >= 0,
		"金库的测试代币不足 %.2f——先给 %s 充一点", 3*liveAmount, treasury)

	// ---- 3. 只读接口面 ----
	address, settleable := client.TokenAddress(service.SupplierPayoutNetworkBSC, cfg.TokenSymbol)
	require.True(t, settleable)
	assert.Equal(t, strings.ToLower(cfg.TokenAddress), address)
	_, unknownToken := client.TokenAddress(service.SupplierPayoutNetworkBSC, "NOPE")
	assert.False(t, unknownToken, "不认识的币种也肯结算——建单的唯一判据失效")

	require.True(t, client.SupportsBatch(service.SupplierPayoutNetworkBSC),
		"配了 disperse 地址却说不支持批量")

	// ---- 4. 单笔转账（worker 单笔路径的全部动作）----
	nonce, err := client.NextNonce(ctx, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	t.Logf("next nonce: %d", nonce)

	single, err := client.Transfer(ctx, service.ChainTransferParams{
		Network: service.SupplierPayoutNetworkBSC,
		Token:   cfg.TokenAddress,
		To:      treasury,
		Amount:  liveAmount,
		Nonce:   &nonce,
	})
	require.NoError(t, err, "单笔广播失败")
	t.Logf("single transfer broadcast: https://testnet.bscscan.com/tx/%s", single.TxHash)

	confirmation, err := client.WaitForConfirmation(ctx, service.SupplierPayoutNetworkBSC, single.TxHash)
	require.NoError(t, err, "单笔确认等不到")
	require.Equal(t, service.ChainTxConfirmed, confirmation.Status,
		"单笔在链上 revert 了：%s", confirmation.Reason)
	t.Logf("single transfer CONFIRMED")

	// ---- 5. 批量（worker 批量路径的全部动作，顺序与生产一致）----
	items := []service.ChainBatchItem{
		{To: treasury, Amount: liveAmount},
		{To: treasury, Amount: liveAmount},
	}
	batchParams := service.ChainBatchParams{
		Network: service.SupplierPayoutNetworkBSC,
		Token:   cfg.TokenSymbol,
		Items:   items,
	}

	// 额度检查在要号之前——与 worker 的顺序一字不差。
	topUp, err := client.EnsureBatchAllowance(ctx, batchParams)
	require.NoError(t, err, "额度确认/补充失败")
	if topUp != nil {
		t.Logf("allowance topped up: https://testnet.bscscan.com/tx/%s", topUp.TxHash)
	} else {
		t.Logf("allowance already sufficient")
	}

	batchNonce, err := client.NextNonce(ctx, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	batchParams.Nonce = &batchNonce

	batch, err := client.TransferBatch(ctx, batchParams)
	require.NoError(t, err, "批量广播失败")
	t.Logf("batch broadcast: https://testnet.bscscan.com/tx/%s", batch.TxHash)

	confirmation, err = client.WaitForConfirmation(ctx, service.SupplierPayoutNetworkBSC, batch.TxHash)
	require.NoError(t, err, "批量确认等不到")
	require.Equal(t, service.ChainTxConfirmed, confirmation.Status,
		"批量在链上 revert 了：%s ——多半是 disperse 合约地址不对或额度没生效", confirmation.Reason)
	t.Logf("batch CONFIRMED")

	// ---- 6. 事后账 ----
	finalBalance := erc20Balance(t, ctx, client, vault)
	assert.Zero(t, finalBalance.Cmp(tokenBalance),
		"全部是 self-transfer，代币余额必须一分不变（before=%s after=%s）",
		weiToDecimal(tokenBalance, decimals), weiToDecimal(finalBalance, decimals))

	var bnbAfterHex string
	require.NoError(t, client.rpc.call(ctx, &bnbAfterHex, "eth_getBalance", treasury, "latest"))
	bnbAfter, _ := new(big.Int).SetString(strings.TrimPrefix(bnbAfterHex, "0x"), 16)
	gasSpent := new(big.Int).Sub(bnbWei, bnbAfter)
	t.Logf("gas spent this run: %s BNB", weiToDecimal(gasSpent, 18))
}

// erc20Balance 读金库在代币合约上的余额。
func erc20Balance(t *testing.T, ctx context.Context, client *Client, owner [20]byte) *big.Int {
	t.Helper()
	raw, err := client.rpc.ethCall(ctx, client.token, packERC20BalanceOf(owner))
	require.NoError(t, err)
	balance, err := decodeUint(raw)
	require.NoError(t, err)
	return balance
}

// tokenUnits 金额换算成最小单位（走生产同一条换算路径）。
func tokenUnits(t *testing.T, amount float64, decimals int) *big.Int {
	t.Helper()
	units, err := service.ToTokenAmount(amount, decimals)
	require.NoError(t, err)
	return units
}

// weiToDecimal 只为日志可读，不参与任何断言算术。
func weiToDecimal(v *big.Int, decimals int) string {
	f := new(big.Float).SetInt(v)
	f.Quo(f, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return fmt.Sprintf("%.6f", f)
}
