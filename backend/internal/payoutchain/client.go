// APEXONE-EXT: 双边市场——service.SupplierChainClient 的真实现。
//
// # 这个文件里唯一真正难的东西是 nonce
//
// nonce 是金库地址上的交易序号，链上严格按序执行。它决定了"重发一笔超时的交易"
// 和"再付一次钱"之间的全部区别：
//
//	用同一个 nonce 重签重发 → 链上认得出是同一笔，最多上一次
//	换一个新 nonce 重发     → 两笔独立的转账，供给者收到两次钱
//
// 所以 Transfer 的 params.Nonce 不是可选的优化项，是**调用方在广播前就把它落库、
// 重试时原样传回来**这条约定的载体。这个客户端自己不记任何状态——记在数据库里
// 的那份才是唯一真相，因为只有它能活过一次进程重启。
//
// # 币种精度只问一次链
//
// USDT 在 BSC 上是 18 位，在以太坊上是 6 位。写死 18 会让以太坊上的一笔
// 1 USDT 变成 1e12 USDT。所以精度必须问合约要，但它永远不变，问一次就够。
package payoutchain

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 各种操作的 gas 上限。取自参考实现（trajector-platform）的实测值。
//
// 这些是**上限**不是实付：没用掉的部分会退回。给宽一点的代价是估算出来的
// 手续费偏高一点点，给窄了的代价是交易 out of gas 失败并且 gas 照扣。
const (
	gasERC20Transfer      = 100_000
	gasERC20Approve       = 80_000
	gasDisperseBase       = 60_000
	gasDispersePerItem    = 70_000
	maxBatchRecipients    = 100
	feeCacheTTL           = 10 * time.Second
	confirmationPollEvery = 3 * time.Second
)

// Client 是照着一条链说话的真客户端。
type Client struct {
	cfg    Config
	rpc    *rpcClient
	signer *signer

	token    [20]byte
	disperse [20]byte

	// pollEvery 是等确认时两次查收据之间隔多久。测试把它调小。
	pollEvery time.Duration

	mu       sync.Mutex
	decimals int       // 0 表示还没问过
	feeAt    time.Time // 上次估价的时刻
	feePrice *big.Int  // 上次估到的 gas 价
}

// New 造一个真客户端。
//
// 到这里说明配置已经校验过（Enabled 且非 Mock），所有必填项都在。
func New(cfg Config, httpClient *http.Client) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled || cfg.Mock {
		return nil, fmt.Errorf("payoutchain: New called for a configuration that is not live")
	}
	s, err := newSigner(cfg.SignerKey)
	if err != nil {
		return nil, err
	}
	token, err := parseAddress(cfg.TokenAddress)
	if err != nil {
		return nil, err
	}
	client := &Client{
		cfg:       cfg,
		rpc:       newRPCClient(cfg.RPCURL, httpClient),
		signer:    s,
		token:     token,
		pollEvery: confirmationPollEvery,
	}
	if cfg.DisperseAddress != "" {
		if client.disperse, err = parseAddress(cfg.DisperseAddress); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// TreasuryAddress 是打款用的金库地址。
//
// 公开出来是给运维用的：起服务时把它打进日志，就能在 BscScan 上盯余额。
// 它是公开信息，从私钥推出来的地址本来就写在每一笔交易里。
func (c *Client) TreasuryAddress() string { return formatAddress(c.signer.address) }

// VerifyChain 起服务时确认节点确实在我们以为的那条链上。
//
// 不做这一步的话，一个指向测试网的 RPC 地址配合主网 chainID，表现是每一笔交易
// 都被节点拒绝——错误消息里只有"invalid sender"，看不出问题在配置里。
func (c *Client) VerifyChain(ctx context.Context) error {
	actual, err := c.rpc.chainID(ctx)
	if err != nil {
		return err
	}
	if actual != c.cfg.ChainID {
		return fmt.Errorf("payoutchain: node reports chain id %d but %s says %d",
			actual, envChainID, c.cfg.ChainID)
	}
	return nil
}

// checkNetwork 拦住不是本客户端负责的那条链。
//
// 一个实例只管一条链。传进来别的网络名说明上层的路由错了——这时候按本链发出去，
// 钱会打到另一条链的地址上（EVM 地址跨链同形，所以不会有任何一步报错）。
func (c *Client) checkNetwork(network string) error {
	if !strings.EqualFold(strings.TrimSpace(network), service.SupplierPayoutNetworkBSC) {
		return fmt.Errorf("%w: this client only serves %q, not %q",
			service.ErrSupplierPayoutChainDisabled, service.SupplierPayoutNetworkBSC, network)
	}
	return nil
}

// tokenDecimals 问一次合约的精度，之后记住。
func (c *Client) tokenDecimals(ctx context.Context) (int, error) {
	c.mu.Lock()
	cached := c.decimals
	c.mu.Unlock()
	if cached > 0 {
		return cached, nil
	}

	data, err := c.rpc.ethCall(ctx, c.token, packERC20Decimals())
	if err != nil {
		return 0, err
	}
	value, err := decodeUint(data)
	if err != nil {
		// 空返回值意味着这个地址上没有 decimals() ——多半根本不是个 ERC-20。
		return 0, fmt.Errorf("payoutchain: %s does not look like an ERC-20 token: %w",
			formatAddress(c.token), err)
	}
	decimals := int(value.Int64())
	if decimals <= 0 || decimals > 36 {
		return 0, fmt.Errorf("payoutchain: token reports %d decimals, which cannot be right", decimals)
	}

	c.mu.Lock()
	c.decimals = decimals
	c.mu.Unlock()
	return decimals, nil
}

// currentGasPrice 取 gas 价，10 秒内复用上一次的结果。
func (c *Client) currentGasPrice(ctx context.Context) (*big.Int, error) {
	c.mu.Lock()
	if c.feePrice != nil && time.Since(c.feeAt) < feeCacheTTL {
		cached := new(big.Int).Set(c.feePrice)
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	price, err := c.rpc.gasPrice(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.feePrice = new(big.Int).Set(price)
	c.feeAt = time.Now()
	c.mu.Unlock()
	return price, nil
}

// EstimateFee 估一笔转账要花多少（折成美元）。
func (c *Client) EstimateFee(ctx context.Context, network string) service.ChainFeeEstimate {
	fallback := service.ChainFeeEstimate{
		Amount:   c.cfg.FallbackFee,
		GasLimit: gasERC20Transfer,
	}
	if err := c.checkNetwork(network); err != nil {
		return fallback
	}
	// 没配 BNB 价就没法换算成美元。这不是故障，是明确选择不接喂价——
	// 直接用配置里的那个固定值，并且如实说它不是估出来的。
	if c.cfg.NativeUSD <= 0 {
		return fallback
	}
	price, err := c.currentGasPrice(ctx)
	if err != nil {
		return fallback
	}

	// wei → 美元：gasPrice × gasLimit / 1e18 × BNB 单价 × 安全系数。
	// 这里用浮点是合适的：结果本来就是个估值，而且它会经过
	// service.ToTokenAmount 的定点换算才变成真金额。
	cost := new(big.Float).SetInt(new(big.Int).Mul(price, big.NewInt(gasERC20Transfer)))
	cost.Quo(cost, big.NewFloat(1e18))
	native, _ := cost.Float64()
	amount := native * c.cfg.NativeUSD * c.cfg.FeeMultiplier
	if amount <= 0 {
		return fallback
	}
	return service.ChainFeeEstimate{
		Amount:      amount,
		GasPriceWei: price.String(),
		GasLimit:    gasERC20Transfer,
		Estimated:   true,
	}
}

// TokenAddress 回答"这条链上的这种币，合约地址是多少"。
//
// 只在**建单**那一刻被调用一次，把地址快照进单子（迁移 234）。此后打款一律读单子上
// 那一列，不再问这里——配置会改，而一张三个月前的单子该发哪个合约的币，
// 答案必须还是三个月前那个。
//
// 币种符号对不上就回假，不是回一个"反正只有一种币"的地址：把 USDC 的合约地址
// 填进一个只认 USDT 的部署，是一件既不报错也不被发现的事，直到有人收到了错的币。
func (c *Client) TokenAddress(network, symbol string) (string, bool) {
	if err := c.checkNetwork(network); err != nil {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(symbol), strings.TrimSpace(c.cfg.TokenSymbol)) {
		return "", false
	}
	return formatAddress(c.token), true
}

// NextNonce 取金库地址下一个可用的 nonce。
func (c *Client) NextNonce(ctx context.Context, network string) (uint64, error) {
	if err := c.checkNetwork(network); err != nil {
		return 0, err
	}
	return c.rpc.pendingNonce(ctx, c.signer.address)
}

// resolveNonce 决定这笔交易用哪个 nonce。
//
// 调用方给了就用给的——那是它落库的那一个，重试必须复用。没给才现问。
func (c *Client) resolveNonce(ctx context.Context, provided *uint64) (uint64, error) {
	if provided != nil {
		return *provided, nil
	}
	return c.rpc.pendingNonce(ctx, c.signer.address)
}

// Transfer 广播一笔 ERC-20 转账。
func (c *Client) Transfer(ctx context.Context, params service.ChainTransferParams) (service.ChainTransferResult, error) {
	var empty service.ChainTransferResult
	if err := c.checkNetwork(params.Network); err != nil {
		return empty, err
	}
	to, err := parseAddress(params.To)
	if err != nil {
		return empty, err
	}
	if to == ([20]byte{}) {
		// 零地址是个合法的 EVM 地址，转给它就是把钱烧掉，而且交易会成功。
		return empty, fmt.Errorf("payoutchain: refusing to pay the zero address")
	}
	decimals, err := c.tokenDecimals(ctx)
	if err != nil {
		return empty, err
	}
	units, err := service.ToTokenAmount(params.Amount, decimals)
	if err != nil {
		return empty, err
	}
	if units.Sign() == 0 {
		return empty, fmt.Errorf("payoutchain: refusing to broadcast a zero-amount transfer")
	}
	data, err := packERC20Transfer(to, units)
	if err != nil {
		return empty, err
	}
	return c.broadcast(ctx, params.Nonce, c.token, gasERC20Transfer, data)
}

// SupportsBatch 有没有配批量合约。同步、不触网。
func (c *Client) SupportsBatch(network string) bool {
	if err := c.checkNetwork(network); err != nil {
		return false
	}
	return c.disperse != [20]byte{}
}

// EnsureBatchAllowance 确认批量合约的额度够发这一组，不够就先 approve。
//
// 必须在调用方预留 nonce **之前**调用：approve 自己要占一个 nonce。
func (c *Client) EnsureBatchAllowance(ctx context.Context, params service.ChainBatchParams) (*service.ChainAllowanceTopUp, error) {
	if err := c.checkNetwork(params.Network); err != nil {
		return nil, err
	}
	if !c.SupportsBatch(params.Network) {
		return nil, service.ErrSupplierPayoutChainNoBatch
	}
	total, _, err := c.batchTotals(ctx, params)
	if err != nil {
		return nil, err
	}

	current, err := c.rpc.ethCall(ctx, c.token, packERC20Allowance(c.signer.address, c.disperse))
	if err != nil {
		return nil, err
	}
	allowance, err := decodeUint(current)
	if err != nil {
		return nil, err
	}
	if allowance.Cmp(total) >= 0 {
		return nil, nil // 够用，没动链
	}

	// 补到无限额度而不是刚好够这一批：每批都 approve 一次意味着每批多烧一笔 gas，
	// 而额度只对**这一个**合约地址生效，而那个合约不持币、没有管理员、不可升级
	// （见 TrajectorDisperse.sol）。给它无限额度和给它这一批的额度，
	// 风险面是同一个。
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	data, err := packERC20Approve(c.disperse, max)
	if err != nil {
		return nil, err
	}
	result, err := c.broadcast(ctx, nil, c.token, gasERC20Approve, data)
	if err != nil {
		return nil, err
	}
	// approve 必须先确认再发批量：批量交易如果抢在 approve 前面被打包，
	// 整批会因额度不足 revert，而 gas 照扣。
	confirmation, err := c.WaitForConfirmation(ctx, params.Network, result.TxHash)
	if err != nil {
		return nil, err
	}
	if confirmation.Status != service.ChainTxConfirmed {
		return nil, fmt.Errorf("payoutchain: allowance top-up %s did not confirm: %s",
			result.TxHash, confirmation.Reason)
	}
	return &service.ChainAllowanceTopUp{
		TxHash: result.TxHash,
		Amount: "unlimited",
		Symbol: params.Token,
	}, nil
}

// batchTotals 把一批金额换算成链上单位，并求和。
func (c *Client) batchTotals(ctx context.Context, params service.ChainBatchParams) (*big.Int, []*big.Int, error) {
	if len(params.Items) == 0 {
		return nil, nil, fmt.Errorf("payoutchain: batch is empty")
	}
	if len(params.Items) > maxBatchRecipients {
		// 太大的批次会超区块 gas 上限，整批失败。
		return nil, nil, fmt.Errorf("payoutchain: batch of %d exceeds the %d recipient limit",
			len(params.Items), maxBatchRecipients)
	}
	decimals, err := c.tokenDecimals(ctx)
	if err != nil {
		return nil, nil, err
	}
	total := new(big.Int)
	values := make([]*big.Int, 0, len(params.Items))
	for _, item := range params.Items {
		units, err := service.ToTokenAmount(item.Amount, decimals)
		if err != nil {
			return nil, nil, err
		}
		if units.Sign() == 0 {
			// 合约会因 ZeroAmount revert 整批——在这里挡住便宜得多。
			return nil, nil, fmt.Errorf("payoutchain: batch contains a zero amount for %s", item.To)
		}
		values = append(values, units)
		total.Add(total, units)
	}
	return total, values, nil
}

// TransferBatch 一笔交易发给多个收款人。
func (c *Client) TransferBatch(ctx context.Context, params service.ChainBatchParams) (service.ChainTransferResult, error) {
	var empty service.ChainTransferResult
	if err := c.checkNetwork(params.Network); err != nil {
		return empty, err
	}
	if !c.SupportsBatch(params.Network) {
		return empty, service.ErrSupplierPayoutChainNoBatch
	}
	_, values, err := c.batchTotals(ctx, params)
	if err != nil {
		return empty, err
	}
	recipients := make([][20]byte, 0, len(params.Items))
	for _, item := range params.Items {
		to, err := parseAddress(item.To)
		if err != nil {
			return empty, err
		}
		if to == ([20]byte{}) {
			return empty, fmt.Errorf("payoutchain: batch contains the zero address")
		}
		recipients = append(recipients, to)
	}
	data, err := packDisperseToken(c.token, recipients, values)
	if err != nil {
		return empty, err
	}
	gas := uint64(gasDisperseBase + gasDispersePerItem*len(recipients))
	return c.broadcast(ctx, params.Nonce, c.disperse, gas, data)
}

// broadcast 签一笔交易并发出去。
func (c *Client) broadcast(ctx context.Context, nonce *uint64, to [20]byte, gasLimit uint64, data []byte) (service.ChainTransferResult, error) {
	var empty service.ChainTransferResult

	resolved, err := c.resolveNonce(ctx, nonce)
	if err != nil {
		return empty, err
	}
	gasPrice, err := c.currentGasPrice(ctx)
	if err != nil {
		return empty, err
	}
	signed, err := c.signer.sign(&legacyTx{
		Nonce:    resolved,
		GasPrice: gasPrice,
		GasLimit: gasLimit,
		To:       to,
		Data:     data,
		ChainID:  c.cfg.ChainID,
	})
	if err != nil {
		return empty, err
	}

	if _, err := c.rpc.sendRawTransaction(ctx, signed.Raw); err != nil {
		// "already known" / "nonce too low"：这笔交易（或占着这个 nonce 的另一笔）
		// 已经在链上或内存池里了。这**不是**失败——本地算出的哈希就是那笔交易的
		// 哈希，回它，让调用方照常去等确认。
		//
		// 当成失败会走到退款分支，而那笔钱可能正在路上：一次双付。
		if rpcErr, ok := asRPCError(err); ok && isAlreadyBroadcast(rpcErr.Message) {
			return service.ChainTransferResult{TxHash: signed.Hash}, nil
		}
		return empty, err
	}
	// 用本地算的哈希而不是节点返回的那个：两者在正常情况下相同，
	// 而广播超时时我们只有本地这一个。统一用它，重试路径才不会分叉。
	return service.ChainTransferResult{TxHash: signed.Hash}, nil
}

// isAlreadyBroadcast 认出"这笔交易已经在了"这一类节点回应。
func isAlreadyBroadcast(message string) bool {
	lowered := strings.ToLower(message)
	for _, marker := range []string{
		"already known",
		"nonce too low",
		"already exists",
		"transaction already in the pool",
	} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// WaitForConfirmation 等一笔交易到终态。
//
// 返回错误 = 还不知道。等不到收据、节点连不上、ctx 到期，全都走这一条——
// 判成 failed 会让一笔可能已经成功的转账被退款，等于双付。
func (c *Client) WaitForConfirmation(ctx context.Context, network, txHash string) (service.ChainConfirmation, error) {
	var empty service.ChainConfirmation
	if err := c.checkNetwork(network); err != nil {
		return empty, err
	}

	ticker := time.NewTicker(c.pollEvery)
	defer ticker.Stop()

	for {
		receipt, err := c.rpc.receipt(ctx, txHash)
		if err != nil {
			return empty, err
		}
		if receipt != nil {
			if receipt.Status == "0x0" {
				// 收据在，状态是 revert：链上明确说这笔没成。这是唯一一种
				// 可以放心退款的失败。
				return service.ChainConfirmation{
					Status: service.ChainTxFailed,
					Reason: "transaction reverted on chain",
				}, nil
			}
			deep, err := c.hasEnoughConfirmations(ctx, receipt)
			if err != nil {
				return empty, err
			}
			if deep {
				return service.ChainConfirmation{Status: service.ChainTxConfirmed}, nil
			}
		}

		select {
		case <-ctx.Done():
			// 超时——这里回错误而不是 failed，见函数头。
			return empty, fmt.Errorf("payoutchain: still waiting on %s: %w", txHash, ctx.Err())
		case <-ticker.C:
		}
	}
}

// hasEnoughConfirmations 判断一笔已上链的交易埋得够不够深。
func (c *Client) hasEnoughConfirmations(ctx context.Context, receipt *txReceipt) (bool, error) {
	mined, err := parseHexUint(receipt.BlockNumber)
	if err != nil {
		return false, err
	}
	head, err := c.rpc.blockNumber(ctx)
	if err != nil {
		return false, err
	}
	if head < mined {
		// 节点回退了（多节点负载均衡后面常见）。当成还不够深继续等，
		// 而不是算出一个下溢的巨大确认数直接判成功。
		return false, nil
	}
	return head-mined+1 >= c.cfg.Confirmations, nil
}

var _ service.SupplierChainClient = (*Client)(nil)
