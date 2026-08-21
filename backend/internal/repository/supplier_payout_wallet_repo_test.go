// APEXONE-EXT: 盲索引的单元测试。不需要数据库，因此不带 integration 标签。
//
// 单独测 payoutWalletHash 是有理由的：真库那套用例走的是 Upsert，而 Upsert 在算
// 哈希之前已经把地址归一化过了。也就是说 payoutWalletHash 内部那次 ToLower
// 在当前唯一的调用路径上永远不会改变任何东西——真库测试无论怎么写都盖不住它。
//
// 而它一旦被拿掉，代价不是「今天就出错」，是「哪天多一条写路径忘了先归一化，
// 同一个地址就能在库里存两份，反女巫约束当场失效且没有任何报错」。
// 这种失败没有任何下游能发现，所以这个契约要在这里被单独钉死。
package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPayoutWalletHashIsCaseAndSpaceInsensitive 同一个地址的任何写法必须得到同一个哈希。
func TestPayoutWalletHashIsCaseAndSpaceInsensitive(t *testing.T) {
	const lower = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"

	// 期望值手算，不调被测函数：用它自己算期望等于断言「它等于它自己」。
	sum := sha256.Sum256([]byte(lower))
	want := hex.EncodeToString(sum[:])

	variants := map[string]string{
		"全小写":    lower,
		"EIP-55": "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"全大写":    "0x" + strings.ToUpper(lower[2:]),
		"带首尾空格":  "  " + lower + "  ",
	}
	for name, input := range variants {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, payoutWalletHash(input),
				"大小写/空白变体算出了不同的盲索引——反女巫唯一索引会被绕过")
		})
	}
}

// TestPayoutWalletHashSeparatesDistinctAddresses 不同地址必须不同哈希。
//
// 反过来的那一半：上一条只证明了「都一样」，一个恒定返回同一个串的实现
// 同样能让它通过，而那会让所有人的地址互相挡住。
func TestPayoutWalletHashSeparatesDistinctAddresses(t *testing.T) {
	a := payoutWalletHash("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	b := payoutWalletHash("0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359")
	assert.NotEqual(t, a, b)
	assert.Len(t, a, 64, "盲索引长度必须与迁移 234 的 CHAR(64) 一致")
	assert.Len(t, b, 64)
}
