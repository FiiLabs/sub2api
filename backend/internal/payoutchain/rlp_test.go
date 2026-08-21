// APEXONE-EXT: RLP 编码的测试。
//
// 这些期望值取自 RLP 规范原文里的例子（ethereum.org 的 RLP 页面上逐条列着），
// 不是从我们的实现里反推的。
package payoutchain

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRLPBytesFollowsTheSpec(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input []byte
		want  string
	}{
		// 单个小字节是它自己——不带任何前缀。
		{"单字节 0x00", []byte{0x00}, "00"},
		{"单字节 0x7f", []byte{0x7f}, "7f"},
		// 0x80 已经不算"小"了，要退回带前缀的形态。
		{"单字节 0x80", []byte{0x80}, "8180"},
		{"空串", nil, "80"},
		{"dog", []byte("dog"), "83646f67"},
		// 55 字节是短串的上界，56 字节就要改用长度的长度。
		{"55 字节", make([]byte, 55), "b7" + strings.Repeat("00", 55)},
		{"56 字节", make([]byte, 56), "b838" + strings.Repeat("00", 56)},
		{"256 字节", make([]byte, 256), "b90100" + strings.Repeat("00", 256)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hex.EncodeToString(rlpBytes(tc.input)))
		})
	}
}

func TestRLPIntegersDropLeadingZeros(t *testing.T) {
	// 这是 RLP 里最容易写错的一条：0 编成空串（0x80），不是 0x00。
	// 写成 0x00 得到的交易哈希和全世界都不一样，而错误表现只是
	// "节点说签名无效"——从那句话完全看不出问题在哪。
	for _, tc := range []struct {
		name  string
		value uint64
		want  string
	}{
		{"零编成空串", 0, "80"},
		{"一", 1, "01"},
		{"127", 127, "7f"},
		{"128 要带前缀", 128, "8180"},
		{"1024", 1024, "820400"},
		{"20 Gwei", 20000000000, "8504a817c800"},
		{"最大 uint64", ^uint64(0), "88ffffffffffffffff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hex.EncodeToString(rlpUint(tc.value)))
		})
	}
}

func TestRLPBigTreatsNilAndZeroAlike(t *testing.T) {
	// 交易里的 value 字段，nil 和 0 是同一件事（一次不带原生币的合约调用）。
	assert.Equal(t, "80", hex.EncodeToString(rlpBig(nil)))
	assert.Equal(t, "80", hex.EncodeToString(rlpBig(big.NewInt(0))))
	assert.Equal(t, "880de0b6b3a7640000",
		hex.EncodeToString(rlpBig(new(big.Int).SetUint64(1000000000000000000))))
}

func TestRLPListFollowsTheSpec(t *testing.T) {
	t.Run("空列表", func(t *testing.T) {
		assert.Equal(t, "c0", hex.EncodeToString(rlpList()))
	})
	t.Run("cat 和 dog", func(t *testing.T) {
		assert.Equal(t, "c88363617483646f67",
			hex.EncodeToString(rlpList(rlpBytes([]byte("cat")), rlpBytes([]byte("dog")))))
	})
	t.Run("负载超过 55 字节要改用长度的长度", func(t *testing.T) {
		// 一个 60 字节的串编出来是 1(前缀)+1(长度)+60 = 62 字节负载。
		encoded := rlpList(rlpBytes(make([]byte, 60)))
		assert.Equal(t, "f83e", hex.EncodeToString(encoded[:2]))
		assert.Len(t, encoded, 2+62)
	})
}

func TestRLPListLengthPrefixCountsThePayloadNotTheItems(t *testing.T) {
	// 一个把项数当成长度写进去的实现，在"一项且这项一字节"时和正确实现
	// 一模一样。两项、每项两字节就分开了：负载 4 字节，前缀是 0xc4 而不是 0xc2。
	encoded := rlpList(rlpBytes([]byte{0x81, 0x82}), rlpBytes([]byte{0x83, 0x84}))
	assert.Equal(t, "c6", hex.EncodeToString(encoded[:1]))
	assert.Len(t, encoded, 7)
}
