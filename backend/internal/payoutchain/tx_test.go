// APEXONE-EXT: EIP-155 签名的金标向量。
//
// 向量同样由 go-ethereum v1.16.1 在仓库外生成（见 abi_test.go 文件头）。
// 第一组是 EIP-155 提案原文里那个人人都能复算的例子——用它是因为它不只在
// go-ethereum 里对，在任何一个实现里都对；第二组是我们真正会广播的形态：
// BSC 上的一笔 USDT transfer。
//
// # 这些向量钉住了什么
//
// 签名用的私钥是测试常量，所以整条链路是确定性的：同一笔交易永远签出同一串字节
// （secp256k1 的 nonce 走 RFC6979，不掷骰子）。于是 raw 这一串把 RLP 字段顺序、
// 整数的前导零处理、v 的算法、r/s 的编码、以及 keccak 的选择全部一次性钉死。
// 任何一处改动都会让整串对不上。
package payoutchain

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// EIP-155 原文里的那把钥匙：32 个 0x46。
	keyEIP155 = "4646464646464646464646464646464646464646464646464646464646464646"
	// 一把任意的测试私钥，用来跑 BSC 那一组。
	keyBSC = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestSignerDerivesTheSameAddressAsGoEthereum(t *testing.T) {
	// 地址推导错了（比如用了 SerializeCompressed，或者忘了去掉 0x04 前缀，
	// 或者取了前 20 字节而不是后 20 字节），表现是金库地址凭空变成另一个：
	// 我们会往一个没有钱的地址上要 nonce，然后所有交易因余额不足被拒。
	for _, tc := range []struct{ name, key, want string }{
		{"EIP-155 原文的钥匙", keyEIP155, "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F"},
		{"BSC 那一组的钥匙", keyBSC, "0xFCAd0B19bB29D4674531d6f115237E16AfCE377c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := newSigner(tc.key)
			require.NoError(t, err)
			assert.Equal(t, strings.ToLower(tc.want), formatAddress(s.address))
		})
	}
}

func TestSignerAcceptsThe0xPrefixAndSurroundingWhitespace(t *testing.T) {
	// 从环境变量里粘进来的私钥常常带 0x，也常常带一个尾随换行。
	// 这两种都不该让服务在启动时"配置错误"地拒绝掉一把好钥匙。
	s, err := newSigner("  0x" + keyBSC + "\n")
	require.NoError(t, err)
	assert.Equal(t, "0xfcad0b19bb29d4674531d6f115237e16afce377c", formatAddress(s.address))
}

func TestSignerRefusesKeysItCannotUse(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{"空", ""},
		{"太短", "abcd"},
		{"太长", keyBSC + "00"},
		{"不是十六进制", strings.Repeat("z", 64)},
		{"全零", strings.Repeat("0", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newSigner(tc.key)
			require.Error(t, err)
			// 私钥绝不能出现在错误消息里——这条错误多半会被日志记下来，
			// 而配错的那一刻粘进去的常常是另一把**真**钥匙。
			//
			// 空钥匙这一例这里跳过：任何串都"包含"空串，断言恒假。
			// 它由下面 TestSignerNeverLeaksTheKeyIntoAnErrorMessage 顶上。
			if tc.key != "" {
				assert.NotContains(t, err.Error(), tc.key)
			}
		})
	}
}

func TestSignerNeverLeaksTheKeyIntoAnErrorMessage(t *testing.T) {
	// 上面那个测试对"空"这一例是空谈（NotContains 任何串都包含空串会怎样）。
	// 这里直接盯住一把具体的钥匙的每一段。
	_, err := newSigner(keyBSC + "ff")
	require.Error(t, err)
	for _, fragment := range []string{keyBSC, keyBSC[:16], "0123456789abcdef"} {
		assert.NotContains(t, err.Error(), fragment)
	}
}

func TestSignMatchesTheEIP155ReferenceVector(t *testing.T) {
	// EIP-155 提案原文里的例子：nonce 9、gasPrice 20 Gwei、gas 21000、
	// 转 1 ether 给 0x3535...35、chainId 1。
	s, err := newSigner(keyEIP155)
	require.NoError(t, err)

	tx := &legacyTx{
		Nonce:    9,
		GasPrice: big.NewInt(20000000000),
		GasLimit: 21000,
		To:       mustAddress(t, "0x3535353535353535353535353535353535353535"),
		Value:    new(big.Int).SetUint64(1000000000000000000),
		ChainID:  1,
	}

	assert.Equal(t,
		"daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53",
		hex.EncodeToString(keccak256(tx.signingPayload())),
		"待签名摘要——EIP-155 原文里印着这一串")

	signed, err := s.sign(tx)
	require.NoError(t, err)

	assert.Equal(t,
		"f86c098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a7640000"+
			"8025a028ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276"+
			"a067cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83",
		hex.EncodeToString(signed.Raw))
	assert.Equal(t,
		"0x33469b22e9f636356c4160a87eb19df52b7412e8eac32a4a55ffe88ea8350788",
		signed.Hash)
}

func TestSignMatchesABSCUSDTTransferVector(t *testing.T) {
	// 我们真正会广播的形态：value 是 0，钱在 data 里。
	s, err := newSigner(keyBSC)
	require.NoError(t, err)

	amount, ok := new(big.Int).SetString("12345678901234567890", 10)
	require.True(t, ok)
	data, err := packERC20Transfer(mustAddress(t, addrRecipient), amount)
	require.NoError(t, err)

	tx := &legacyTx{
		Nonce:    42,
		GasPrice: big.NewInt(1000000000),
		GasLimit: 100000,
		To:       mustAddress(t, addrBSCUSDT),
		Value:    nil, // 合约调用不带原生币
		Data:     data,
		ChainID:  56,
	}

	assert.Equal(t,
		"98237d23ee1fdb9a2ec5e44fd64b623fa61cd47a73412920cad2a03cdec4cd7b",
		hex.EncodeToString(keccak256(tx.signingPayload())))

	signed, err := s.sign(tx)
	require.NoError(t, err)

	assert.Equal(t,
		"f8aa2a843b9aca00830186a09455d398326f99059ff775485246999027b319795580b844"+
			"a9059cbb000000000000000000000000de709f2102306220921060314715629080e2fb77"+
			"000000000000000000000000000000000000000000000000ab54a98ceb1f0ad2"+
			"8193a010d5d9905e745f7ccd5a7dee503da213bd02bbc4c49eff3ee6e127e84aee5e05"+
			"a037737e3ce9b993bad28737c7652a5e7a6031571550168bcdb7aff09b438f24a4",
		hex.EncodeToString(signed.Raw))
	assert.Equal(t,
		"0x098500f3809def22e782be21670b97b1e362b4249de3232aebf7c5b4c1d78386",
		signed.Hash)

	// v 那一段单独看一眼：56*2+35 = 147 = 0x93，加上 recid=0。
	// RLP 里它就是 raw 里 data 之后紧跟着的那个 0x81 0x93。
	assert.Contains(t, hex.EncodeToString(signed.Raw), "8193a0",
		"v 应当是 EIP-155 形态（chainID 56 → 147/148），不是 27/28")
}

func TestSignIsDeterministic(t *testing.T) {
	// RFC6979：同一把钥匙签同一个摘要永远给出同一个签名。
	// 这条不只是好看——重试一笔广播超时的交易时，我们会用同一个 nonce
	// 重新签一遍，而只有签出完全相同的字节，链上才认得出那是同一笔交易
	// 而不是两笔互相竞争的转账。
	s, err := newSigner(keyBSC)
	require.NoError(t, err)
	build := func() *legacyTx {
		return &legacyTx{
			Nonce: 7, GasPrice: big.NewInt(1000000000), GasLimit: 100000,
			To: mustAddress(t, addrBSCUSDT), ChainID: 56,
		}
	}
	first, err := s.sign(build())
	require.NoError(t, err)
	second, err := s.sign(build())
	require.NoError(t, err)
	assert.Equal(t, first.Hash, second.Hash)
	assert.Equal(t, hex.EncodeToString(first.Raw), hex.EncodeToString(second.Raw))
}

func TestSignRefusesWithoutAChainID(t *testing.T) {
	// chainID 为 0 会让 v 退回 27/28，也就是 EIP-155 之前的形态：
	// 同一笔交易能被原样重放到任意一条 EVM 链上，而金库在各链上是同一个地址。
	s, err := newSigner(keyBSC)
	require.NoError(t, err)
	_, err = s.sign(&legacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), GasLimit: 21000,
		To: mustAddress(t, addrRecipient), ChainID: 0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chain id")
}

func TestSigningPayloadCarriesTheChainIDAndTwoZeros(t *testing.T) {
	// EIP-155 的关键就在待签名负载末尾这三项。少了它们，摘要会变成
	// EIP-155 之前的样子——而上面那个参考向量正好能证明我们没少。
	// 这里再单独看一眼末尾的字节：chainID 56 = 0x38，后面两个空串 0x80 0x80。
	tx := &legacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), GasLimit: 21000,
		To: mustAddress(t, addrRecipient), ChainID: 56,
	}
	encoded := hex.EncodeToString(tx.signingPayload())
	assert.True(t, strings.HasSuffix(encoded, "388080"), "实际结尾: %s", encoded[len(encoded)-12:])
}
