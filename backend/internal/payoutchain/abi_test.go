// APEXONE-EXT: ABI 编码的金标向量。
//
// # 这些十六进制串是哪来的
//
// 用 go-ethereum v1.16.1 的 accounts/abi 在**仓库外**（/tmp 里一个一次性小程序）
// 生成，然后把结果抄进来。go-ethereum 本身没有进依赖图（理由见 rlp.go 文件头）。
//
// 这么做的意义在于：本文件里的期望值和 abi.go 里的实现没有任何共同祖先。
// 如果两边对上了，说明我们的编码和以太坊生态公认的编码是同一个东西；
// 而如果测试是"拿实现算一遍再和自己比"，它对任何错误都会一直是绿的。
package payoutchain

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试里反复用到的三个地址。
const (
	addrRecipient = "0xde709f2102306220921060314715629080e2fb77"
	addrOther     = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	addrBSCUSDT   = "0x55d398326f99059fF775485246999027B3197955"
)

func mustAddress(t *testing.T, value string) [20]byte {
	t.Helper()
	parsed, err := parseAddress(value)
	require.NoError(t, err)
	return parsed
}

func TestSelectorsMatchTheKnownFourBytes(t *testing.T) {
	// 右边这些是公开可查的方法 ID（4byte.directory / 任意区块浏览器上的合约页面）。
	// 左边是从签名串现算的。两条独立的路径必须给出同一个答案——见 abi.go 文件头。
	for _, tc := range []struct {
		signature string
		want      string
	}{
		{sigERC20Transfer, "a9059cbb"},
		{sigERC20Approve, "095ea7b3"},
		{sigERC20Allowance, "dd62ed3e"},
		{sigERC20BalanceOf, "70a08231"},
		{sigERC20Decimals, "313ce567"},
		{sigDisperseToken, "c73a2d60"},
	} {
		t.Run(tc.signature, func(t *testing.T) {
			assert.Equal(t, tc.want, hex.EncodeToString(selector(tc.signature)))
		})
	}
}

func TestKeccakIsTheEthereumOneNotNISTSHA3(t *testing.T) {
	// 空输入的 keccak256。NIST 定案后的 SHA3-256 对同样的空输入给出
	// a7ffc6f8bf1ed766...，完全不同的值，而且两个函数都不会报错。
	// 这一行就是把"用错了那个"这件事变得看得见。
	assert.Equal(t,
		"c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
		hex.EncodeToString(keccak256(nil)))
}

func TestPackERC20TransferMatchesGoEthereum(t *testing.T) {
	amount, ok := new(big.Int).SetString("12345678901234567890", 10)
	require.True(t, ok)

	data, err := packERC20Transfer(mustAddress(t, addrRecipient), amount)
	require.NoError(t, err)

	assert.Equal(t,
		"a9059cbb"+
			"000000000000000000000000de709f2102306220921060314715629080e2fb77"+
			"000000000000000000000000000000000000000000000000ab54a98ceb1f0ad2",
		hex.EncodeToString(data))

	// 收款地址那一段是整条链路上唯一"编错了钱会到别人手里"的地方，
	// 所以除了整体比对，再单独把它的位置和内容钉一次。
	assert.Equal(t, addrRecipient[2:], hex.EncodeToString(data[4+12:4+32]),
		"收款地址必须右对齐在第一个参数字里")
}

func TestPackERC20ApproveMatchesGoEthereum(t *testing.T) {
	// 无限额度：2^256-1。approve 给批量合约用的就是这个值。
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	data, err := packERC20Approve(mustAddress(t, addrRecipient), max)
	require.NoError(t, err)

	assert.Equal(t,
		"095ea7b3"+
			"000000000000000000000000de709f2102306220921060314715629080e2fb77"+
			strings.Repeat("f", 64),
		hex.EncodeToString(data))
}

func TestPackReadOnlyCallsMatchGoEthereum(t *testing.T) {
	t.Run("allowance", func(t *testing.T) {
		data := packERC20Allowance(mustAddress(t, addrRecipient), mustAddress(t, addrOther))
		assert.Equal(t,
			"dd62ed3e"+
				"000000000000000000000000de709f2102306220921060314715629080e2fb77"+
				"0000000000000000000000005aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
			hex.EncodeToString(data))
	})
	t.Run("balanceOf", func(t *testing.T) {
		data := packERC20BalanceOf(mustAddress(t, addrRecipient))
		assert.Equal(t,
			"70a08231"+
				"000000000000000000000000de709f2102306220921060314715629080e2fb77",
			hex.EncodeToString(data))
	})
	t.Run("decimals 没有参数", func(t *testing.T) {
		assert.Equal(t, "313ce567", hex.EncodeToString(packERC20Decimals()))
	})
}

func TestPackDisperseTokenMatchesGoEthereum(t *testing.T) {
	// 两个动态数组的布局最容易写错，而写错的后果不是"报错"，是
	// 合约把某个偏移当成数组长度去读——要么 revert（烧 gas），
	// 要么读出一串垃圾地址。这里逐字节钉住。
	values := []*big.Int{
		new(big.Int).SetUint64(1000000000000000000), // 1.0
		new(big.Int).SetUint64(250000000000000000),  // 0.25
	}
	data, err := packDisperseToken(
		mustAddress(t, addrBSCUSDT),
		[][20]byte{mustAddress(t, addrRecipient), mustAddress(t, addrOther)},
		values,
	)
	require.NoError(t, err)

	assert.Equal(t,
		"c73a2d60"+
			// 头部三个字：token、recipients 偏移(0x60)、values 偏移(0xc0)
			"00000000000000000000000055d398326f99059ff775485246999027b3197955"+
			"0000000000000000000000000000000000000000000000000000000000000060"+
			"00000000000000000000000000000000000000000000000000000000000000c0"+
			// recipients：长度 2 + 两个地址
			"0000000000000000000000000000000000000000000000000000000000000002"+
			"000000000000000000000000de709f2102306220921060314715629080e2fb77"+
			"0000000000000000000000005aaeb6053f3e94c9b9a09f33669435e7ef1beaed"+
			// values：长度 2 + 两个金额
			"0000000000000000000000000000000000000000000000000000000000000002"+
			"0000000000000000000000000000000000000000000000000de0b6b3a7640000"+
			"00000000000000000000000000000000000000000000000003782dace9d90000",
		hex.EncodeToString(data))
}

func TestPackDisperseTokenOffsetsFollowTheRecipientCount(t *testing.T) {
	// 上一个测试用的是 2 个收款人，而 values 的偏移是 0x60+(1+n)*32 ——
	// 一个把 (1+n) 写死成 3 的实现在 n=2 时和正确实现完全一样。
	// 换一个 n 就分道扬镳了。
	for _, tc := range []struct {
		count      int
		wantValues string
	}{
		{count: 1, wantValues: "00000000000000000000000000000000000000000000000000000000000000a0"},
		{count: 3, wantValues: "00000000000000000000000000000000000000000000000000000000000000e0"},
	} {
		recipients := make([][20]byte, tc.count)
		values := make([]*big.Int, tc.count)
		for i := range recipients {
			recipients[i] = mustAddress(t, addrRecipient)
			values[i] = big.NewInt(1)
		}
		data, err := packDisperseToken(mustAddress(t, addrBSCUSDT), recipients, values)
		require.NoError(t, err)
		// 第三个头部字（选择器 4 + 两个字 64 之后）就是 values 的偏移。
		assert.Equal(t, tc.wantValues, hex.EncodeToString(data[4+64:4+96]),
			"收款人数量 %d 时 values 的偏移", tc.count)
	}
}

func TestPackDisperseTokenRefusesMalformedBatches(t *testing.T) {
	token := mustAddress(t, addrBSCUSDT)
	one := mustAddress(t, addrRecipient)

	t.Run("长度不一致", func(t *testing.T) {
		_, err := packDisperseToken(token, [][20]byte{one, one}, []*big.Int{big.NewInt(1)})
		require.Error(t, err)
	})
	t.Run("空批次", func(t *testing.T) {
		// 合约会 revert（EmptyBatch），但那要先花掉一笔 gas 才知道。
		_, err := packDisperseToken(token, nil, nil)
		require.Error(t, err)
	})
	t.Run("金额是 nil", func(t *testing.T) {
		_, err := packDisperseToken(token, [][20]byte{one}, []*big.Int{nil})
		require.Error(t, err)
	})
}

func TestEncodeUintRefusesWhatCannotFitAWord(t *testing.T) {
	t.Run("负数", func(t *testing.T) {
		_, err := encodeUint(big.NewInt(-1))
		require.Error(t, err)
	})
	t.Run("超过 256 位", func(t *testing.T) {
		// 截断的话 2^256 会变成 0——一笔"成功"的零转账。
		_, err := encodeUint(new(big.Int).Lsh(big.NewInt(1), 256))
		require.Error(t, err)
	})
	t.Run("恰好 256 位是合法的", func(t *testing.T) {
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		got, err := encodeUint(max)
		require.NoError(t, err)
		assert.Equal(t, strings.Repeat("f", 64), hex.EncodeToString(got))
	})
}

func TestDecodeUintRefusesShortReturns(t *testing.T) {
	// 空返回值意味着那个地址上没有这个方法。读成 0 会得到"精度为 0"，
	// 而精度为 0 会让所有金额少放大 18 个量级。
	_, err := decodeUint(nil)
	require.Error(t, err)

	_, err = decodeUint(make([]byte, 31))
	require.Error(t, err)

	got, err := decodeUint(append(make([]byte, 31), 18))
	require.NoError(t, err)
	assert.Equal(t, int64(18), got.Int64())
}

func TestParseAddressRoundTrips(t *testing.T) {
	t.Run("大小写混写的地址原样解析", func(t *testing.T) {
		parsed, err := parseAddress(addrBSCUSDT)
		require.NoError(t, err)
		assert.Equal(t, strings.ToLower(addrBSCUSDT), formatAddress(parsed))
	})
	t.Run("不带 0x 也收", func(t *testing.T) {
		parsed, err := parseAddress(addrRecipient[2:])
		require.NoError(t, err)
		assert.Equal(t, addrRecipient, formatAddress(parsed))
	})
	for _, bad := range []string{"", "0x", "0xde709f", addrRecipient + "00", "0xzz709f2102306220921060314715629080e2fb77"} {
		t.Run("拒绝 "+bad, func(t *testing.T) {
			_, err := parseAddress(bad)
			require.Error(t, err)
		})
	}
}
