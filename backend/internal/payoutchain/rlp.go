// APEXONE-EXT: 双边市场——RLP 编码（只做编码，不做解码）。
//
// # 为什么这里自己写，而不是引 go-ethereum
//
// 引 go-ethereum 会给这个仓库带进 7 个新模块、强制升级 3 个已有依赖，其中
// c-kzg / blst 是 cgo 的（本项目的发布产物是 CGO_ENABLED=0 的静态二进制）。
// 换来的是一整套我们用不到的东西：EVM、状态树、P2P、共识。
//
// 我们真正需要的面很窄：一种交易类型（EIP-155 legacy）、三个 JSON-RPC 方法、
// 两个合约调用。而其中真正难的那部分——secp256k1 签名与 keccak256——**没有**
// 自己写：前者用 decred 的 secp256k1（已在依赖图里，且正是 go-ethereum 非 cgo
// 路径用的同一个库），后者用 golang.org/x/crypto/sha3（已是直接依赖）。
//
// 自己写的只有 RLP 和 ABI 这两段纯编码。它们的失败模式也是这套取舍的一部分：
// 编错了，签名对不上、节点直接拒收，链上什么都不会发生。真正会"钱打错地方"的
// 只有 ABI 里收款地址那一段，而那一段被金标向量逐字节钉住（见 abi_test.go）。
//
// # RLP 规则
//
// 只有两种东西：字节串和列表。
//
//	字节串 len==1 且 b[0]<0x80  → 它自己
//	字节串 len<56              → 0x80+len, 内容
//	字节串 len>=56             → 0xb7+len(长度的字节数), 长度, 内容
//	列表   payload<56          → 0xc0+len, payload
//	列表   payload>=56         → 0xf7+len(长度的字节数), 长度, payload
//
// 整数一律先变成**去掉前导零**的大端字节串，0 编码成空串。这条是 RLP 里最容易
// 写错的地方：把 0 编成 0x00（一个字节的串）而不是 0x80（空串），得到的哈希
// 与全世界都不一样，而错误表现只是"节点说签名无效"。
package payoutchain

import (
	"math/big"
)

// rlpBytes 编码一个字节串。
func rlpBytes(value []byte) []byte {
	if len(value) == 1 && value[0] < 0x80 {
		return []byte{value[0]}
	}
	return append(rlpLengthPrefix(len(value), 0x80), value...)
}

// rlpList 把已经编码好的若干项拼成一个列表。
func rlpList(items ...[]byte) []byte {
	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	return append(rlpLengthPrefix(len(payload), 0xc0), payload...)
}

// rlpUint 编码一个无符号整数。
func rlpUint(value uint64) []byte {
	return rlpBytes(trimLeadingZeros(bigEndian(value)))
}

// rlpBig 编码一个大整数。
//
// nil 与 0 都编成空串——它们在交易字段里是同一件事（value=0 的合约调用）。
func rlpBig(value *big.Int) []byte {
	if value == nil || value.Sign() == 0 {
		return rlpBytes(nil)
	}
	return rlpBytes(value.Bytes())
}

// rlpLengthPrefix 按 RLP 规则生成长度前缀。offset 是 0x80（串）或 0xc0（列表）。
func rlpLengthPrefix(length int, offset byte) []byte {
	if length < 56 {
		return []byte{offset + byte(length)}
	}
	lengthBytes := trimLeadingZeros(bigEndian(uint64(length)))
	return append([]byte{offset + 55 + byte(len(lengthBytes))}, lengthBytes...)
}

// bigEndian 把一个 uint64 摊成 8 个字节。
func bigEndian(value uint64) []byte {
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = byte(value)
		value >>= 8
	}
	return out
}

// trimLeadingZeros 去掉前导零字节。全零返回空切片（而不是一个 0x00）——见文件头。
func trimLeadingZeros(value []byte) []byte {
	i := 0
	for i < len(value) && value[i] == 0 {
		i++
	}
	return value[i:]
}
