//go:build unit

// APEXONE-EXT: 双边市场——链上收款地址校验的单元测试。
//
// 这个文件的密度与它守的东西成正比：地址校验是整条提现链路上唯一一处
// 「错了就不可逆」的判断。下面每一个用例都对应一种真实会发生的输入，
// 而不是为了覆盖率凑出来的变形。
package service

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEIP55CanonicalVectors 用 EIP-55 规范正文里的向量钉死校验和算法。
//
// 抄规范的向量而不是自己造：eip55 的每一处细节（Legacy Keccak 而非 SHA3-256、
// 半字节的取法、只对字母生效）单独看都像是对的，错了也不会 panic——只会让每个
// 合法地址都被判成校验和不符，或者更糟，让改过的地址被放行。规范向量是唯一
// 能同时钉死这几件事的东西。
func TestEIP55CanonicalVectors(t *testing.T) {
	// 均来自 EIP-55 正文的 "Test cases" 一节。
	vectors := []string{
		// 全大写
		"0x52908400098527886E0F7030069857D2E4169EE7",
		"0x8617E340B3D01FA5F11F306F4090FD50E238070D",
		// 全小写
		"0xde709f2102306220921060314715629080e2fb77",
		"0x27b1fdb04752bbc536007a920d24acb045561c26",
		// 普通混合大小写
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	}
	for _, checksummed := range vectors {
		t.Run(checksummed, func(t *testing.T) {
			lower := checksummed[2:]
			for i := 0; i < len(lower); i++ {
				if lower[i] >= 'A' && lower[i] <= 'F' {
					lower = lower[:i] + string(lower[i]+('a'-'A')) + lower[i+1:]
				}
			}
			assert.Equal(t, checksummed[2:], eip55(lower),
				"EIP-55 校验和与规范向量不符——多半是用了 sha3.New256 而不是 NewLegacyKeccak256")
		})
	}
}

// TestNormalizeAddressAcceptsSingleCase 全小写与全大写都必须放行。
//
// 这一条是「只在混合大小写时校验」那个决定的正面：交易所与区块浏览器给出的
// 地址常常是全小写的，它们没有携带校验和，挡掉它们等于挡掉一大批合法用户。
func TestNormalizeAddressAcceptsSingleCase(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"全小写", "0xde709f2102306220921060314715629080e2fb77",
			"0xde709f2102306220921060314715629080e2fb77"},
		{"全大写", "0x52908400098527886E0F7030069857D2E4169EE7",
			"0x52908400098527886e0f7030069857d2e4169ee7"},
		{"带首尾空格", "  0xde709f2102306220921060314715629080e2fb77  ",
			"0xde709f2102306220921060314715629080e2fb77"},
		// 混合大小写且校验和正确的，同样归一化成小写。
		{"合法混合大小写", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, tc.input)
			require.NoError(t, err)
			// 归一化必须落到小写：库里的唯一索引建在 SHA-256(小写地址) 上，
			// 同一个地址有两种写法就等于唯一约束形同虚设。
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNormalizeAddressRejectsBadChecksum 混合大小写但校验和不对 → 拒绝。
//
// 构造方式是把一个规范向量里的某一位字母改掉大小写——这正是「手工改过地址」
// 在字符层面的样子，也是这道门存在的全部理由。
func TestNormalizeAddressRejectsBadChecksum(t *testing.T) {
	valid := "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	// 第 3 个字符 'A' → 'a'：位数没变、十六进制仍然合法，只有校验和能发现它。
	tampered := "0x5aaeb6053F3E94C9b9A09f33669435E7Ef1BeAed"

	_, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, valid)
	require.NoError(t, err, "规范向量本身必须通过，否则下面的断言没有意义")

	_, err = NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, tampered)
	assert.ErrorIs(t, err, ErrSupplierPayoutAddressChecksum,
		"混合大小写地址的校验和不符必须单独报错，不能混进格式错误里")
}

// TestNormalizeAddressRejectsMalformed 格式不对的各种形态。
func TestNormalizeAddressRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"空串":       "",
		"少一位":      "0xde709f2102306220921060314715629080e2fb7",
		"多一位":      "0xde709f2102306220921060314715629080e2fb777",
		"没有 0x 前缀": "de709f2102306220921060314715629080e2fb7712",
		"含非十六进制字符": "0xde709f2102306220921060314715629080e2fbzz",
		// 交易哈希是 66 字符，粘错栏是真实会发生的事。
		"粘成了交易哈希": "0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, input)
			assert.ErrorIs(t, err, ErrSupplierPayoutAddressInvalid)
		})
	}
}

// TestNormalizeAddressRejectsZero 零地址单独报错。
//
// 它格式完全合法，所以只可能被这一道门挡住；而错误码必须与格式错误分开，
// 否则用户会以为自己粘错了位数，回去再粘一次同样的东西。
func TestNormalizeAddressRejectsZero(t *testing.T) {
	_, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC,
		"0x0000000000000000000000000000000000000000")
	assert.ErrorIs(t, err, ErrSupplierPayoutAddressZero)
}

// TestNormalizeAddressRejectsUnknownNetwork 网络校验排在地址校验之前。
//
// 顺序有意义：网络不对时报地址格式错误，会让人盯着地址找一个不存在的问题。
func TestNormalizeAddressRejectsUnknownNetwork(t *testing.T) {
	_, err := NormalizeSupplierPayoutAddress("eth",
		"0xde709f2102306220921060314715629080e2fb77")
	assert.ErrorIs(t, err, ErrSupplierPayoutNetworkInvalid)

	// 连格式一望即知不合法的地址也要先报网络错，才说明顺序真的是这样。
	_, err = NormalizeSupplierPayoutAddress("tron", "nonsense")
	assert.ErrorIs(t, err, ErrSupplierPayoutNetworkInvalid)
}

// TestOnchainChannelRegistry 渠道注册表的查表行为。
func TestOnchainChannelRegistry(t *testing.T) {
	got, ok := LookupSupplierOnchainChannel("BSC-USDT")
	require.True(t, ok)
	assert.Equal(t, SupplierPayoutNetworkBSC, got.Network)
	assert.Equal(t, "USDT", got.Token)

	// 前后空白要吃掉：管理员在白名单里填渠道名时很容易带上空格。
	got, ok = LookupSupplierOnchainChannel("  BSC-USDT  ")
	require.True(t, ok)
	assert.Equal(t, SupplierPayoutNetworkBSC, got.Network)

	// 大小写敏感，与 SupplyWithdrawalSettings.HasChannel 同一个规则：
	// 两处规则不一致的话，会出现一个「白名单里有、注册表里查不到」的渠道，
	// 它能被选中却永远不会被 worker 处理。
	_, ok = LookupSupplierOnchainChannel("bsc-usdt")
	assert.False(t, ok, "渠道名区分大小写，与提现设置的白名单比对规则保持一致")

	// 人工渠道查不到 —— 这是两条路径的分岔点，不是错误。
	_, ok = LookupSupplierOnchainChannel("支付宝")
	assert.False(t, ok)
}

// TestOnchainChannelsReturnsCopy 注册表返回副本。
//
// 直接返回切片本身时，调用方一次 append 就改了全进程的注册表——
// 而这张表决定「哪个渠道会自动把钱打到链上」。
func TestOnchainChannelsReturnsCopy(t *testing.T) {
	first := SupplierOnchainChannels()
	require.NotEmpty(t, first)
	first[0].Network = "tampered"

	second := SupplierOnchainChannels()
	assert.Equal(t, SupplierPayoutNetworkBSC, second[0].Network)
}

// 这两个结构体是**直接**序列化给前端的，不经过 dto 层的转换。
//
// 单独钉住 json 键名，是因为漏一个 tag 没有任何症状：Go 会拿导出字段名
// （`Channel`、`CreatedAt`）当键发出去，编译过、测试过、请求 200，
// 前端按这个 API 的惯例写的 `channel` 读到 undefined，于是渠道名渲染成空白。
// 这类错只会在有人打开页面时被发现，而那时它已经上线了。
//
// 用键的**全集**断言而不是逐个 Contains：多出来的字段和少掉的一样值得看一眼——
// 这两个响应会带上一个人的链上身份，往里面加字段该是一次明确的决定。
func TestPayoutWireShapeIsSnakeCase(t *testing.T) {
	t.Run("onchain channel", func(t *testing.T) {
		assert.Equal(t,
			[]string{"channel", "network", "token_symbol"},
			jsonKeys(t, SupplierOnchainChannels()[0]))
	})

	t.Run("wallet", func(t *testing.T) {
		assert.Equal(t,
			[]string{"address", "created_at", "id", "network", "updated_at", "user_id"},
			jsonKeys(t, SupplierPayoutWallet{Network: SupplierPayoutNetworkBSC, UpdatedAt: time.Now()}))
	})

	t.Run("options", func(t *testing.T) {
		// 空绑定必须是 `[]`，不是 `null`——服务层承诺过这件事，
		// 而这里是它在**线上**成不成立的那一层。
		options := &SupplierPayoutWalletOptions{Channels: SupplierOnchainChannels(), Wallets: []SupplierPayoutWallet{}}
		assert.Equal(t, []string{"channels", "wallets"}, jsonKeys(t, options))

		encoded, err := json.Marshal(options)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), `"wallets":[]`,
			"没绑过时 wallets 发成了 null——前端的 v-for 会在 null 上炸，而 [] 只是画一个空表单")
	})
}

func jsonKeys(t *testing.T, value any) []string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
