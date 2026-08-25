// APEXONE-EXT: 双边市场——最小的以太坊 JSON-RPC 客户端。
//
// 只有六个方法：链 ID、gas 价、nonce、eth_call、广播、收据。
//
// # 传输失败和节点拒绝必须分开
//
// 这两件事在重试逻辑里是相反的处置：
//
//	节点拒绝（返回了 error 对象）——它看过这笔交易并说不行。重试没有意义，
//	                              除非我们改点什么。
//	传输失败（超时、连接断、502）——我们不知道节点看没看到。**尤其是广播**：
//	                              交易可能已经进了内存池。当成"没发过"重发一笔
//	                              新 nonce 的交易，就是把同一笔钱付两次。
//
// 所以这里用两个不同的类型：rpcError 表示前者，普通 error 表示后者。
// 上层（client.go）据此决定是换 nonce 重来还是原样重签重发。
package payoutchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
)

// rpcError 是节点明确返回的错误——它收到了请求，看懂了，并且拒绝。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// asRPCError 判断一个错误是不是节点的明确拒绝。
func asRPCError(err error) (*rpcError, bool) {
	var target *rpcError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Error  *rpcError       `json:"error"`
	Result json.RawMessage `json:"result"`
}

// rpcClient 对着一个 JSON-RPC 端点说话。
type rpcClient struct {
	url  string
	http *http.Client
}

func newRPCClient(url string, httpClient *http.Client) *rpcClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &rpcClient{url: url, http: httpClient}
}

// call 发一个 JSON-RPC 请求，把 result 解进 out。out 为 nil 时丢弃结果。
func (c *rpcClient) call(ctx context.Context, out any, method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("payoutchain: encode %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("payoutchain: build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// 传输失败——我们不知道节点看没看到。
		return fmt.Errorf("payoutchain: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 限一下读取量：一个配错的 URL 可能指向任何东西，而 io.ReadAll 对一个
	// 无限流会一直读到进程内存耗尽。1 MiB 远大于任何一个我们会发的请求的回应。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("payoutchain: %s: read response: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		// 429/502/503 都落在这里，都属于"不知道"。
		return fmt.Errorf("payoutchain: %s: http %d", method, resp.StatusCode)
	}

	var parsed rpcResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("payoutchain: %s: response is not json-rpc", method)
	}
	if parsed.Error != nil {
		return parsed.Error
	}
	if out == nil {
		return nil
	}
	if len(parsed.Result) == 0 || string(parsed.Result) == "null" {
		// eth_getTransactionReceipt 对一笔还没上链的交易就是这个回应，
		// 而那不是错误。调用方用 *string / *receipt 这类可空类型接住它。
		return nil
	}
	if err := json.Unmarshal(parsed.Result, out); err != nil {
		return fmt.Errorf("payoutchain: %s: cannot read result: %w", method, err)
	}
	return nil
}

// chainID 问节点它自己在哪条链上。
func (c *rpcClient) chainID(ctx context.Context) (uint64, error) {
	var hexValue string
	if err := c.call(ctx, &hexValue, "eth_chainId"); err != nil {
		return 0, err
	}
	value, err := parseHexUint(hexValue)
	if err != nil {
		return 0, fmt.Errorf("payoutchain: eth_chainId: %w", err)
	}
	return value, nil
}

// gasPrice 取当前 gas 价（wei）。
func (c *rpcClient) gasPrice(ctx context.Context) (*big.Int, error) {
	var hexValue string
	if err := c.call(ctx, &hexValue, "eth_gasPrice"); err != nil {
		return nil, err
	}
	return parseHexBig(hexValue)
}

// pendingNonce 取金库的下一个可用 nonce。
//
// 用 "pending" 而不是 "latest"：latest 只数已上链的交易，而我们可能刚广播过一笔
// 还在内存池里的。用 latest 会得到一个已被占用的 nonce，新交易要么替换掉那一笔
// （少发一次钱），要么被节点以 "already known"/"replacement underpriced" 拒掉。
func (c *rpcClient) pendingNonce(ctx context.Context, address [20]byte) (uint64, error) {
	var hexValue string
	if err := c.call(ctx, &hexValue, "eth_getTransactionCount", formatAddress(address), "pending"); err != nil {
		return 0, err
	}
	return parseHexUint(hexValue)
}

// ethCall 做一次只读调用。
func (c *rpcClient) ethCall(ctx context.Context, to [20]byte, data []byte) ([]byte, error) {
	var hexValue string
	err := c.call(ctx, &hexValue, "eth_call", map[string]string{
		"to":   formatAddress(to),
		"data": "0x" + hex.EncodeToString(data),
	}, "latest")
	if err != nil {
		return nil, err
	}
	return parseHexBytes(hexValue)
}

// sendRawTransaction 广播一笔签好名的交易，返回节点给的哈希。
func (c *rpcClient) sendRawTransaction(ctx context.Context, raw []byte) (string, error) {
	var hash string
	err := c.call(ctx, &hash, "eth_sendRawTransaction", "0x"+hex.EncodeToString(raw))
	if err != nil {
		return "", err
	}
	return hash, nil
}

// txReceipt 是收据里我们关心的那几项。
type txReceipt struct {
	// Status "0x1" 成功，"0x0" 被 revert。
	Status string `json:"status"`
	// BlockNumber 交易所在区块。用来数确认数。
	BlockNumber string `json:"blockNumber"`
}

// receipt 取一笔交易的收据。返回 nil 表示还没上链——这不是错误。
func (c *rpcClient) receipt(ctx context.Context, txHash string) (*txReceipt, error) {
	var out *txReceipt
	if err := c.call(ctx, &out, "eth_getTransactionReceipt", txHash); err != nil {
		return nil, err
	}
	return out, nil
}

// blockNumber 取当前区块高度。
func (c *rpcClient) blockNumber(ctx context.Context) (uint64, error) {
	var hexValue string
	if err := c.call(ctx, &hexValue, "eth_blockNumber"); err != nil {
		return 0, err
	}
	return parseHexUint(hexValue)
}

// parseHexUint 解析 "0x1a" 这样的数量前缀十六进制。
//
// 空串报错而不是当 0：一个返回空的 eth_getTransactionCount 被当成 nonce=0
// 会去重发第一笔交易。
func parseHexUint(value string) (uint64, error) {
	parsed, err := parseHexBig(value)
	if err != nil {
		return 0, err
	}
	if !parsed.IsUint64() {
		return 0, fmt.Errorf("payoutchain: %q does not fit a uint64", value)
	}
	return parsed.Uint64(), nil
}

func parseHexBig(value string) (*big.Int, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if trimmed == "" {
		return nil, fmt.Errorf("payoutchain: expected a hex quantity, got %q", value)
	}
	parsed, ok := new(big.Int).SetString(trimmed, 16)
	if !ok {
		return nil, fmt.Errorf("payoutchain: %q is not a hex quantity", value)
	}
	return parsed, nil
}

// parseHexBytes 解析 "0x..." 这样的数据串。空数据（"0x"）是合法的。
func parseHexBytes(value string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if trimmed == "" {
		return nil, nil
	}
	raw, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("payoutchain: %q is not hex data", value)
	}
	return raw, nil
}
