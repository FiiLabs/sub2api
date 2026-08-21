// APEXONE-EXT: 双边市场——合约调用的 ABI 编码。
//
// 只编码，不解码结构体；返回值只有 uint256 一种形态（decimals / allowance /
// balanceOf），解出来就是一个大整数。
//
// # 这个文件是整条链路上唯一"编错了会把钱送到别处"的地方
//
// RLP 编错了签名对不上，节点拒收，链上什么都不会发生。而 ABI 里的收款地址那 32
// 个字节编错了，交易完全合法、会成功上链、区块浏览器上写着已转账——只是收款人
// 不是我们想给的那个。所以这里的每一个选择器和每一段编码都被金标向量逐字节钉住
// （abi_test.go，向量由 go-ethereum 在仓库外生成）。
//
// # 选择器为什么算而不是抄
//
// 抄一个 0xa9059cbb 进来是最省事的写法，但那样签名串里的一个笔误（"transfer(
// address,uint)" 而不是 "uint256"）就永远看不见了——常量和签名串各说各的，
// 而只有常量参与运算。这里从签名串现算，再让测试用抄来的常量校验它：
// 两条独立的路径必须给出同一个答案。
package payoutchain

import (
	"fmt"
	"math/big"

	"golang.org/x/crypto/sha3"
)

// ERC-20 与批量合约里我们会调到的方法签名。
//
// 签名串里**不能有空格**，也不能用别名（uint 不等于 uint256）：
// 选择器是这个串的 keccak256 前 4 字节，差一个字符就是另一个方法。
const (
	sigERC20Transfer  = "transfer(address,uint256)"
	sigERC20Approve   = "approve(address,uint256)"
	sigERC20Allowance = "allowance(address,address)"
	sigERC20BalanceOf = "balanceOf(address)"
	sigERC20Decimals  = "decimals()"
	sigDisperseToken  = "disperseToken(address,address[],uint256[])"
)

// selector 取方法签名的 keccak256 前 4 字节。
func selector(signature string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(signature))
	return hasher.Sum(nil)[:4]
}

// keccak256 是以太坊用的那个 keccak——NIST 定案**之前**的填充规则。
//
// 用 sha3.New256 会得到完全不同的哈希，而且不报错：地址、选择器、交易哈希
// 会全部悄悄错掉。名字里的 Legacy 说的正是这件事。
func keccak256(chunks ...[]byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	for _, chunk := range chunks {
		hasher.Write(chunk)
	}
	return hasher.Sum(nil)
}

// word 把一段不超过 32 字节的数据左填充成一个 ABI 字（32 字节）。
func word(value []byte) []byte {
	if len(value) > 32 {
		// 不可能发生：调用方给的要么是 20 字节地址，要么是已经查过范围的 uint256。
		// 真发生了要炸而不是截断——截断一个金额是把 2^256 变成一个小数目，
		// 截断一个地址是转给别人。
		panic(fmt.Sprintf("payoutchain: abi word overflow (%d bytes)", len(value)))
	}
	out := make([]byte, 32)
	copy(out[32-len(value):], value)
	return out
}

// encodeAddress 把 20 字节地址编成一个字。
func encodeAddress(address [20]byte) []byte { return word(address[:]) }

// encodeUint 把一个非负大整数编成一个字。
func encodeUint(value *big.Int) ([]byte, error) {
	if value == nil {
		return nil, fmt.Errorf("payoutchain: nil uint256")
	}
	if value.Sign() < 0 {
		return nil, fmt.Errorf("payoutchain: negative uint256 %s", value)
	}
	if value.BitLen() > 256 {
		return nil, fmt.Errorf("payoutchain: uint256 overflow (%d bits)", value.BitLen())
	}
	return word(value.Bytes()), nil
}

// packERC20Transfer 编 transfer(to, amount)。
func packERC20Transfer(to [20]byte, amount *big.Int) ([]byte, error) {
	amountWord, err := encodeUint(amount)
	if err != nil {
		return nil, err
	}
	return concat(selector(sigERC20Transfer), encodeAddress(to), amountWord), nil
}

// packERC20Approve 编 approve(spender, amount)。
func packERC20Approve(spender [20]byte, amount *big.Int) ([]byte, error) {
	amountWord, err := encodeUint(amount)
	if err != nil {
		return nil, err
	}
	return concat(selector(sigERC20Approve), encodeAddress(spender), amountWord), nil
}

// packERC20Allowance 编 allowance(owner, spender)。
func packERC20Allowance(owner, spender [20]byte) []byte {
	return concat(selector(sigERC20Allowance), encodeAddress(owner), encodeAddress(spender))
}

// packERC20BalanceOf 编 balanceOf(owner)。
func packERC20BalanceOf(owner [20]byte) []byte {
	return concat(selector(sigERC20BalanceOf), encodeAddress(owner))
}

// packERC20Decimals 编 decimals()。
func packERC20Decimals() []byte { return selector(sigERC20Decimals) }

// packDisperseToken 编 disperseToken(token, recipients[], values[])。
//
// 两个动态数组的布局：头部三个字节是 token、recipients 的偏移、values 的偏移；
// 偏移从**头部开始**算（不是从选择器开始），所以第一个数组的偏移恒为 0x60。
// 数组体是「长度 + 元素」。
//
// 长度不一致会被合约的 LengthMismatch revert 掉，但那要烧掉 gas 才知道；
// 在这里挡住便宜得多。
func packDisperseToken(token [20]byte, recipients [][20]byte, values []*big.Int) ([]byte, error) {
	if len(recipients) != len(values) {
		return nil, fmt.Errorf("payoutchain: disperse has %d recipients but %d values",
			len(recipients), len(values))
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("payoutchain: disperse batch is empty")
	}

	const headWords = 3
	recipientsOffset := int64(headWords * 32)
	// recipients 体 = 1 个长度字 + n 个元素字。
	valuesOffset := recipientsOffset + int64((1+len(recipients))*32)

	head := concat(
		encodeAddress(token),
		word(big.NewInt(recipientsOffset).Bytes()),
		word(big.NewInt(valuesOffset).Bytes()),
	)

	recipientsBody := word(big.NewInt(int64(len(recipients))).Bytes())
	for _, recipient := range recipients {
		recipientsBody = append(recipientsBody, encodeAddress(recipient)...)
	}

	valuesBody := word(big.NewInt(int64(len(values))).Bytes())
	for _, value := range values {
		valueWord, err := encodeUint(value)
		if err != nil {
			return nil, err
		}
		valuesBody = append(valuesBody, valueWord...)
	}

	return concat(selector(sigDisperseToken), head, recipientsBody, valuesBody), nil
}

// decodeUint 把一个 eth_call 的返回值读成大整数。
//
// 长度不足 32 时报错而不是补零：一个返回空数据的 call 意味着那个地址上没有这个
// 方法（多半根本不是我们以为的那个合约），把它读成 0 会得到「额度为 0」「精度为 0」
// 这类看起来合理、实际完全错误的结论。精度为 0 尤其危险——所有金额会少放大 18 个量级。
func decodeUint(data []byte) (*big.Int, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("payoutchain: expected a 32-byte word, got %d bytes", len(data))
	}
	return new(big.Int).SetBytes(data[:32]), nil
}

func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}
