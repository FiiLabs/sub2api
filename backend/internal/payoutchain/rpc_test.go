// APEXONE-EXT: JSON-RPC 客户端的测试。
//
// 全部对着 httptest 起的本地服务器跑——这个包的测试**永远不会**碰真实节点。
package payoutchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captured 记下一次请求的方法名和参数，供断言查看。
type captured struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

// stubNode 起一个假节点：按方法名回固定结果，并把收到的请求记下来。
func stubNode(t *testing.T, results map[string]any) (*rpcClient, *[]captured) {
	t.Helper()
	var seen []captured
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req captured
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		seen = append(seen, req)

		result, ok := results[req.Method]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method %s not found"}}`, req.Method)
			return
		}
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%s}`, encoded)
	}))
	t.Cleanup(server.Close)
	return newRPCClient(server.URL, server.Client()), &seen
}

func TestRPCReadsHexQuantities(t *testing.T) {
	client, _ := stubNode(t, map[string]any{
		"eth_chainId":             "0x38",
		"eth_gasPrice":            "0x3b9aca00",
		"eth_blockNumber":         "0x2b1e5c1",
		"eth_getTransactionCount": "0x2a",
	})
	ctx := context.Background()

	chainID, err := client.chainID(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(56), chainID, "0x38 是 BSC 主网")

	price, err := client.gasPrice(ctx)
	require.NoError(t, err)
	assert.Equal(t, "1000000000", price.String())

	height, err := client.blockNumber(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(45213121), height)

	nonce, err := client.pendingNonce(ctx, mustAddress(t, addrRecipient))
	require.NoError(t, err)
	assert.Equal(t, uint64(42), nonce)
}

func TestPendingNonceAsksForThePendingState(t *testing.T) {
	// 用 "latest" 会漏掉刚广播、还在内存池里的那一笔，于是拿到一个已被占用的
	// nonce——新交易要么顶掉那一笔（少发一次钱），要么被节点直接拒。
	client, seen := stubNode(t, map[string]any{"eth_getTransactionCount": "0x0"})
	_, err := client.pendingNonce(context.Background(), mustAddress(t, addrRecipient))
	require.NoError(t, err)

	require.Len(t, *seen, 1)
	assert.Equal(t, "eth_getTransactionCount", (*seen)[0].Method)
	require.Len(t, (*seen)[0].Params, 2)
	assert.Equal(t, addrRecipient, (*seen)[0].Params[0])
	assert.Equal(t, "pending", (*seen)[0].Params[1])
}

func TestEthCallSendsToAndDataAndReadsBackBytes(t *testing.T) {
	client, seen := stubNode(t, map[string]any{
		"eth_call": "0x0000000000000000000000000000000000000000000000000000000000000012",
	})
	data, err := client.ethCall(context.Background(), mustAddress(t, addrBSCUSDT), packERC20Decimals())
	require.NoError(t, err)

	decimals, err := decodeUint(data)
	require.NoError(t, err)
	assert.Equal(t, int64(18), decimals.Int64())

	require.Len(t, *seen, 1)
	params := (*seen)[0].Params[0].(map[string]any)
	assert.Equal(t, "0x55d398326f99059ff775485246999027b3197955", params["to"])
	assert.Equal(t, "0x313ce567", params["data"])
	assert.Equal(t, "latest", (*seen)[0].Params[1])
}

func TestSendRawTransactionForwardsTheHexAndReturnsTheHash(t *testing.T) {
	want := "0x098500f3809def22e782be21670b97b1e362b4249de3232aebf7c5b4c1d78386"
	client, seen := stubNode(t, map[string]any{"eth_sendRawTransaction": want})

	got, err := client.sendRawTransaction(context.Background(), []byte{0xde, 0xad, 0xbe, 0xef})
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, "0xdeadbeef", (*seen)[0].Params[0])
}

func TestReceiptIsNilBeforeTheTransactionLands(t *testing.T) {
	// 节点对一笔还在内存池里的交易回 null。这不是错误——把它当错误处理，
	// worker 会在每一轮把"还没确认"记成"查询失败"。
	client, _ := stubNode(t, map[string]any{"eth_getTransactionReceipt": nil})
	got, err := client.receipt(context.Background(), "0xabc")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestReceiptReadsStatusAndBlock(t *testing.T) {
	client, _ := stubNode(t, map[string]any{
		"eth_getTransactionReceipt": map[string]string{
			"status":      "0x1",
			"blockNumber": "0x2b1e5c1",
		},
	})
	got, err := client.receipt(context.Background(), "0xabc")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "0x1", got.Status)

	height, err := parseHexUint(got.BlockNumber)
	require.NoError(t, err)
	assert.Equal(t, uint64(45213121), height)
}

func TestNodeRejectionIsDistinguishableFromTransportFailure(t *testing.T) {
	// 这个区分是整个重试策略的地基：节点明确拒绝的交易重发多少次都一样，
	// 而"没问到"的那一笔可能已经进了内存池，换个 nonce 重发就是付两次钱。
	t.Run("节点明确拒绝", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nonce too low"}}`)
		}))
		defer server.Close()

		_, err := newRPCClient(server.URL, server.Client()).
			sendRawTransaction(context.Background(), []byte{0x01})
		require.Error(t, err)

		rpcErr, ok := asRPCError(err)
		require.True(t, ok, "必须认得出这是节点的拒绝")
		assert.Equal(t, -32000, rpcErr.Code)
		assert.Equal(t, "nonce too low", rpcErr.Message)
	})

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"网关 502", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{"限流 429", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }},
		{"回的不是 json", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>maintenance</html>") }},
	} {
		t.Run(tc.name+"不算拒绝", func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			_, err := newRPCClient(server.URL, server.Client()).
				sendRawTransaction(context.Background(), []byte{0x01})
			require.Error(t, err)
			_, ok := asRPCError(err)
			assert.False(t, ok, "这是「不知道」，不是「节点说不行」")
		})
	}

	t.Run("连不上不算拒绝", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := server.URL
		server.Close() // 端口关掉，制造一次连接失败

		_, err := newRPCClient(url, nil).sendRawTransaction(context.Background(), []byte{0x01})
		require.Error(t, err)
		_, ok := asRPCError(err)
		assert.False(t, ok)
	})
}

func TestCallHonoursContextCancellation(t *testing.T) {
	client, _ := stubNode(t, map[string]any{"eth_chainId": "0x38"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.chainID(ctx)
	require.Error(t, err)
	_, ok := asRPCError(err)
	assert.False(t, ok, "取消是「不知道」")
}

func TestHexParsersRefuseEmptyInsteadOfReadingZero(t *testing.T) {
	// 空的 eth_getTransactionCount 被当成 nonce=0，会去重发第一笔交易。
	for _, bad := range []string{"", "0x", "  ", "0xzz", "not-hex"} {
		t.Run("拒绝 "+bad, func(t *testing.T) {
			_, err := parseHexUint(bad)
			require.Error(t, err)
		})
	}
	t.Run("超出 uint64 的数量", func(t *testing.T) {
		_, err := parseHexUint("0x1" + "0000000000000000")
		require.Error(t, err)
	})
	t.Run("数据串里 0x 是合法的空", func(t *testing.T) {
		// 数量和数据是两种不同的东西：eth_call 返回 "0x" 表示没有返回值，
		// 那是个正常回答（虽然 decodeUint 之后会拒绝它）。
		got, err := parseHexBytes("0x")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
