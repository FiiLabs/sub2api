// APEXONE-EXT: 双边市场——EIP-155 legacy 交易的构造与签名。
//
// # 为什么是 legacy 而不是 EIP-1559
//
// BSC 两种都收。legacy 少两个字段（maxFeePerGas / maxPriorityFeePerGas），
// 手续费就是 gasPrice × gasLimit 一个乘法——而这个乘积正是要从供给者收益里扣的
// 那笔钱（M3）。1559 的实际花费取决于 baseFee，估算与实付之间多一层不确定性，
// 而我们从中得不到任何好处：打款不抢区块，慢一个区块没有代价。
//
// # 私钥
//
// 只从环境变量读，只存在于 signer 结构体里，绝不进日志、绝不进任何错误消息。
// 这个文件里没有一处把 signer 或 privateKey 交给 %v / %+v。
package payoutchain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// signer 持有金库私钥。
type signer struct {
	key     *secp256k1.PrivateKey
	address [20]byte
}

// newSigner 从十六进制私钥造一个签名器。
//
// 错误消息里**只说长度和格式**，不回显任何一段输入：一个配错的私钥常常是把
// 另一个真私钥粘错了位置，把它回显进日志等于把它写进日志。
func newSigner(privateKeyHex string) (*signer, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	if len(trimmed) != 64 {
		return nil, fmt.Errorf("payoutchain: signer key must be 64 hex characters, got %d", len(trimmed))
	}
	raw, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("payoutchain: signer key is not hexadecimal")
	}
	key := secp256k1.PrivKeyFromBytes(raw)
	// 全零私钥会被 PrivKeyFromBytes 接受（它不校验范围），而它对应的公钥是无穷远点。
	// 落到这里表现为一个恒定的、谁都知道对应私钥的地址。
	if key.Key.IsZero() {
		return nil, fmt.Errorf("payoutchain: signer key is zero")
	}
	return &signer{key: key, address: pubKeyToAddress(key.PubKey())}, nil
}

// pubKeyToAddress 从公钥算出 EVM 地址：keccak256(未压缩公钥去掉 0x04 前缀) 的后 20 字节。
func pubKeyToAddress(pub *secp256k1.PublicKey) [20]byte {
	uncompressed := pub.SerializeUncompressed() // 65 字节，首字节是 0x04
	digest := keccak256(uncompressed[1:])
	var address [20]byte
	copy(address[:], digest[12:])
	return address
}

// legacyTx 是一笔 EIP-155 legacy 交易。
type legacyTx struct {
	Nonce    uint64
	GasPrice *big.Int
	GasLimit uint64
	To       [20]byte
	Value    *big.Int
	Data     []byte
	ChainID  uint64
}

// signingPayload 是待签名的 RLP：rlp([nonce, gasPrice, gas, to, value, data, chainID, 0, 0])。
//
// 末尾那两个 0 是 EIP-155 的做法——把 chainID 塞进本来放 v/r/s 的位置，让一条链上的
// 签名在另一条链上解不出同一个发送者。少了它们，同一笔交易可以被原样重放到
// 任何一条 EVM 链上，而金库在多条链上通常是同一个地址。
func (tx *legacyTx) signingPayload() []byte {
	return rlpList(
		rlpUint(tx.Nonce),
		rlpBig(tx.GasPrice),
		rlpUint(tx.GasLimit),
		rlpBytes(tx.To[:]),
		rlpBig(tx.Value),
		rlpBytes(tx.Data),
		rlpUint(tx.ChainID),
		rlpUint(0),
		rlpUint(0),
	)
}

// signedTx 是一笔签好名的交易。
type signedTx struct {
	// Raw 可以直接送进 eth_sendRawTransaction 的字节。
	Raw []byte
	// Hash 交易哈希 = keccak256(Raw)。
	//
	// 本地算而不是用节点返回的那个：节点返回的哈希在广播成功时与本地一致，
	// 但广播**超时**时我们拿不到任何返回值，而那正是最需要知道哈希的时刻——
	// 有了它才能回头去查这笔交易到底上没上链。
	Hash string
}

// sign 给一笔交易签名。
func (s *signer) sign(tx *legacyTx) (*signedTx, error) {
	if tx.ChainID == 0 {
		// chainID 为 0 会让 v 退化成 27/28（EIP-155 之前的形态），交易可跨链重放。
		return nil, fmt.Errorf("payoutchain: refusing to sign without a chain id")
	}
	digest := keccak256(tx.signingPayload())

	// SignCompact 返回 <1 字节恢复码><32 字节 R><32 字节 S>，恢复码 = 27 + recid，
	// 且 S 已被规范化到低半区（以太坊要求 s ≤ N/2）。
	compact := ecdsa.SignCompact(s.key, digest, false)
	if len(compact) != 65 {
		return nil, fmt.Errorf("payoutchain: unexpected signature length %d", len(compact))
	}
	recid := uint64(compact[0] - 27)
	if recid > 1 {
		// recid 的高位（overflow bit）在实践中概率约 2^-128。真出现了要重签而不是
		// 硬编成 0/1：一个错的 v 会让节点从签名里恢复出**另一个地址**，
		// 那笔交易要么被拒（余额不足），要么——如果那个地址恰好有钱——从别处扣钱。
		return nil, fmt.Errorf("payoutchain: signature recovery id %d is out of range", recid)
	}
	r := new(big.Int).SetBytes(compact[1:33])
	sValue := new(big.Int).SetBytes(compact[33:65])
	// EIP-155：v = recid + chainID*2 + 35
	v := recid + tx.ChainID*2 + 35

	raw := rlpList(
		rlpUint(tx.Nonce),
		rlpBig(tx.GasPrice),
		rlpUint(tx.GasLimit),
		rlpBytes(tx.To[:]),
		rlpBig(tx.Value),
		rlpBytes(tx.Data),
		rlpUint(v),
		rlpBig(r),
		rlpBig(sValue),
	)
	return &signedTx{Raw: raw, Hash: "0x" + hex.EncodeToString(keccak256(raw))}, nil
}

// parseAddress 把 0x 开头的 40 位十六进制串变成 20 字节。
//
// 不在这里做 EIP-55 校验和校验：走到这一步的地址来自绑定表（M1 已经校验过并
// 归一化成小写）或配置里的合约地址。在这里再校验一次会让全小写的合约地址被拒，
// 而全小写恰恰是最常见的抄写形态。
func parseAddress(value string) ([20]byte, error) {
	var out [20]byte
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(trimmed) != 40 {
		return out, fmt.Errorf("payoutchain: address must be 40 hex characters, got %q", value)
	}
	raw, err := hex.DecodeString(trimmed)
	if err != nil {
		return out, fmt.Errorf("payoutchain: address %q is not hexadecimal", value)
	}
	copy(out[:], raw)
	return out, nil
}

// formatAddress 把 20 字节变回小写的 0x 串。
func formatAddress(address [20]byte) string {
	return "0x" + hex.EncodeToString(address[:])
}
