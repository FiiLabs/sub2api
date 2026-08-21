// APEXONE-EXT: 双边市场——链上收款地址绑定的领域类型与校验。
//
// 229 的提现是工单：运营看着收款账号手工打款。这个文件是自动化的入口——
// 供给者绑一个链上地址，提现走注册在 supplierOnchainChannels 里的渠道时，
// worker 直接把币打过去（M2/M4）。
//
// # 这个文件的重量全在校验上
//
// 链上转账不可逆，且没有任何下游能兜住一个填错的地址：交易会成功，浏览器上
// 明明白白写着「已转账」，钱在一个谁也不认识的地址里。因此校验必须发生在
// **绑定**这一刻——那是整条链路上最后一个「还没有钱牵扯进来」的时刻。
//
// 三道门，每一道挡的是一类真实发生过的事故：
//
//  1. 格式：0x + 40 位十六进制。挡的是复制时漏了字符、把交易哈希当地址粘进来。
//  2. EIP-55 校验和（**仅当地址是混合大小写时**）。挡的是改了一位。
//  3. 零地址。挡的是「跟着示例填」——它格式完全合法，转过去等同销毁。
//
// 第 2 条的条件是关键，见 validateEVMAddress 的注释。
package service

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/crypto/sha3"
)

// 支持的链。
const (
	// SupplierPayoutNetworkBSC BSC 主网。
	//
	// 只上这一条链是刻意的：BSC 的单笔手续费在美分量级，且能用批量合约把 N 个人
	// 摊进一笔交易（M5）。加第二条链时，这里加一个常量、
	// supplierOnchainChannels 加一行、配置加一组 RPC/合约地址即可——
	// 数据表、状态机、worker 都不需要动。
	SupplierPayoutNetworkBSC = "bsc"
)

// SupplierPayoutAddressLen 是带 0x 前缀的 EVM 地址长度。
const SupplierPayoutAddressLen = 42

// supplierPayoutZeroAddress 零地址。
//
// 它格式完全合法，但转过去的钱等同销毁，而且**没有任何下游能兜住它**：
// 批量路径靠合约的 ZeroRecipient 挡下（连累同组一起 revert），单笔路径则直接成功，
// 单子记成 paid，供给者余额清零而钱进了黑洞。更麻烦的是这取决于币的实现——
// 主网 USDC 的 transfer 到零地址会 revert，USDT 不会。也就是说选哪种币决定了
// 这是「发不出去」还是「钱没了」。唯一能同时管住两条路径的位置是绑定这道门。
const supplierPayoutZeroAddress = "0x0000000000000000000000000000000000000000"

// SupplierOnchainChannel 把一个收款渠道名绑到一条链和一种币上。
//
// 渠道名仍然要管理员显式加进 supply_withdrawal_settings.Channels 才可选——
// 这张注册表只回答「**如果**选了这个渠道，该怎么结算」，不回答「能不能选」。
// 两件事分开，是为了让「先把代码放上去、之后再打开」这条上线路径继续成立。
// 三个字段都带 json tag，且都是 snake_case。这不是风格问题：这个结构体会**原样**
// 出现在两个用户可见的响应里（提现 options 的 onchain_channels、绑定表单的 channels），
// 没有 tag 的话字段名会以 Go 的导出名 `Channel`/`Network`/`Token` 发出去——
// 与这个 API 里其他所有字段的写法都不一样，而前端按惯例写的 `channel` 会永远读到
// undefined：不报错，只是渲染出一个空的渠道名。
//
// Token 的 json 名刻意是 token_symbol 而不是 token：M2 起单子上会多一个
// token_address（真正用于结算的东西），那时一个叫 `token` 的字段到底指哪个
// 就要靠读文档才能知道。
type SupplierOnchainChannel struct {
	// Channel 渠道名，与管理员填进白名单的字符串逐字相等（区分大小写，
	// 与 SupplyWithdrawalSettings.HasChannel 同一个规则）。
	Channel string `json:"channel"`
	// Network 链标识，落到提现单的 network 列。
	Network string `json:"network"`
	// Token 币种符号，仅作展示；真正落库并用于结算的是合约地址（见迁移 234）。
	Token string `json:"token_symbol"`
}

// supplierOnchainChannels 链上渠道注册表。
//
// 写成代码里的常量表而不是一项配置：渠道名一旦被写进历史单子的 payout_channel，
// 它对应哪条链就不能再改了。让它可配置，等于允许有人把「BSC-USDT」重新指向另一条链，
// 而所有历史单子会跟着改口径。合约地址那种确实会变的东西才走配置。
var supplierOnchainChannels = []SupplierOnchainChannel{
	{Channel: "BSC-USDT", Network: SupplierPayoutNetworkBSC, Token: "USDT"},
}

// LookupSupplierOnchainChannel 查一个渠道名是不是链上渠道。
//
// 查不到不是错误：绝大多数渠道（支付宝、银行卡）本来就该走人工，
// 「查不到 = 人工打款」是这套设计里两条路径的分岔点。
func LookupSupplierOnchainChannel(channel string) (SupplierOnchainChannel, bool) {
	trimmed := strings.TrimSpace(channel)
	for _, item := range supplierOnchainChannels {
		if item.Channel == trimmed {
			return item, true
		}
	}
	return SupplierOnchainChannel{}, false
}

// SupplierOnchainChannels 返回注册表的副本，供管理端渲染「哪些渠道会自动打款」。
//
// 返回副本而不是切片本身：调用方 append 一下就改了全进程的注册表。
func SupplierOnchainChannels() []SupplierOnchainChannel {
	return append([]SupplierOnchainChannel(nil), supplierOnchainChannels...)
}

// IsSupplierPayoutNetwork 网络标识是否受支持。
func IsSupplierPayoutNetwork(network string) bool {
	return strings.TrimSpace(network) == SupplierPayoutNetworkBSC
}

var (
	// ErrSupplierPayoutNetworkInvalid 不认识的链标识。
	ErrSupplierPayoutNetworkInvalid = infraerrors.BadRequest(
		"SUPPLIER_PAYOUT_NETWORK_INVALID", "unsupported payout network")
	// ErrSupplierPayoutAddressInvalid 地址格式不对。
	ErrSupplierPayoutAddressInvalid = infraerrors.BadRequest(
		"SUPPLIER_PAYOUT_ADDRESS_INVALID", "address must be 0x followed by 40 hexadecimal characters")
	// ErrSupplierPayoutAddressChecksum EIP-55 校验和不匹配。
	//
	// 与格式错误分开报，因为对用户意味着完全不同的动作：格式错是「你少粘了几位」，
	// 校验和错是「你粘的位数对，但其中有一位不是你以为的那一位」——后者几乎必然是
	// 手工改过地址，而那正是最危险的一种错。文案必须让他回去重新复制，而不是
	// 盯着自己手里那串字符找错。
	ErrSupplierPayoutAddressChecksum = infraerrors.BadRequest(
		"SUPPLIER_PAYOUT_ADDRESS_CHECKSUM", "address checksum does not match; re-copy the address from your wallet")
	// ErrSupplierPayoutAddressZero 零地址。
	ErrSupplierPayoutAddressZero = infraerrors.BadRequest(
		"SUPPLIER_PAYOUT_ADDRESS_ZERO", "the zero address cannot receive funds")
	// ErrSupplierPayoutAddressTaken 地址已被别的账号绑定。
	//
	// 路由层要映射成 409 而不是 400：请求本身没有错，是资源冲突。
	ErrSupplierPayoutAddressTaken = infraerrors.Conflict(
		"SUPPLIER_PAYOUT_ADDRESS_TAKEN", "this address is already bound to another account")
	// ErrSupplierPayoutWalletNotFound 该链上还没有绑定地址。
	ErrSupplierPayoutWalletNotFound = infraerrors.NotFound(
		"SUPPLIER_PAYOUT_WALLET_NOT_FOUND", "no payout address is bound for this network")
	// ErrSupplierPayoutWalletUnavailable 绑定服务没装配起来。
	//
	// 单独一个错误码、而且是 5xx，是因为它说的是**我们**坏了，不是调用方错了。
	// 早先这里回的是 NETWORK_INVALID，那会让一次 wire 装配失误在前端显示成
	// "不支持这条链"——用户会去换链重试，运维会去查链配置，而真正坏掉的东西
	// 不在这两个地方的任何一个。
	ErrSupplierPayoutWalletUnavailable = infraerrors.ServiceUnavailable(
		"SUPPLIER_PAYOUT_WALLET_UNAVAILABLE", "supplier payout wallet service unavailable")
)

// SupplierPayoutWallet 是一条绑定记录。
type SupplierPayoutWallet struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	Network string `json:"network"`
	// Address 小写形态的地址。对外展示时前端自行做 EIP-55 美化——
	// 库里存小写是为了让「同一个地址」只有一种写法，比较和去重才成立。
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NormalizeSupplierPayoutAddress 校验并归一化一个收款地址。
//
// 返回小写地址。三道门的顺序是刻意的：先答"这是不是一个地址"，
// 再答"是不是**你的**那个地址"，最后答"这个地址能不能收钱"。
func NormalizeSupplierPayoutAddress(network, address string) (string, error) {
	if !IsSupplierPayoutNetwork(network) {
		return "", ErrSupplierPayoutNetworkInvalid
	}
	return validateEVMAddress(strings.TrimSpace(address))
}

// validateEVMAddress 校验 EVM 地址并返回小写形态。
//
// # EIP-55 只在混合大小写时校验
//
// 这是本文件里唯一一处值得停下来想的判断。EIP-55 把校验和编码在字母的大小写里：
// 地址是全小写或全大写时，**没有校验和信息可用**，只能放行；混合大小写时，
// 大小写模式必须与 keccak256 算出来的一致，否则就是改过。
//
// 两边都不能少：
//   - 一律强制校验和 → 挡掉所有从交易所、区块浏览器复制来的全小写地址。
//     那些地址完全合法，用户会以为自己的钱包坏了。
//   - 一律不校验（trajector 当前的做法）→ 从 MetaMask 复制的地址是混合大小写的，
//     改错一位时那一位的大小写几乎必然与新的校验和冲突，而我们放过了这个信号。
//     白白丢掉一次能挡住不可逆损失的机会。
//
// 于是：有校验和就查，没有就认。这也正是 ethers.js getAddress 的行为。
func validateEVMAddress(address string) (string, error) {
	if len(address) != SupplierPayoutAddressLen || !strings.HasPrefix(address, "0x") {
		return "", ErrSupplierPayoutAddressInvalid
	}
	body := address[2:]
	if _, err := hex.DecodeString(body); err != nil {
		return "", ErrSupplierPayoutAddressInvalid
	}

	lower := strings.ToLower(body)
	upper := strings.ToUpper(body)
	// 混合大小写 = 携带了校验和。两个都不等于原串，说明大小写不统一。
	if body != lower && body != upper && eip55(lower) != body {
		return "", ErrSupplierPayoutAddressChecksum
	}

	normalized := "0x" + lower
	if normalized == supplierPayoutZeroAddress {
		return "", ErrSupplierPayoutAddressZero
	}
	return normalized, nil
}

// eip55 按 EIP-55 给一个小写的 40 位十六进制串加上校验和大小写。
//
// 算法：对小写串的 ASCII 取 keccak256，逐字符看对应的哈希半字节——
// ≥ 8 则该位字母大写。数字不受影响（它们没有大小写，也就承载不了校验和信息，
// 这正是 EIP-55 只有约 2^-19 的漏检率而不是零的原因）。
//
// 用 sha3.NewLegacyKeccak256 而不是 sha3.New256：以太坊用的是 NIST 定案**之前**
// 的 Keccak，两者填充规则不同，算出来的哈希完全不一样。名字里的 "Legacy" 正是
// 在说这件事——用错了不会报错，只会让每一个合法地址都被判成校验和不符。
func eip55(lowerBody string) string {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(lowerBody))
	digest := hasher.Sum(nil)

	out := []byte(lowerBody)
	for i, c := range out {
		if c < 'a' || c > 'f' {
			continue
		}
		// 第 i 个字符对应哈希的第 i 个半字节：偶数位取高 4 位，奇数位取低 4 位。
		nibble := digest[i/2] >> 4
		if i%2 == 1 {
			nibble = digest[i/2] & 0x0f
		}
		if nibble >= 8 {
			out[i] = c - ('a' - 'A')
		}
	}
	return string(out)
}

// SupplierPayoutWalletRepository 是绑定表的持久化接口。
type SupplierPayoutWalletRepository interface {
	// Get 读某人某链的绑定。没有返回 (nil, nil)——「还没绑」不是错误，
	// 提现表单要靠它决定是画输入框还是画已绑地址。
	Get(ctx context.Context, userID int64, network string) (*SupplierPayoutWallet, error)
	// List 读某人的全部绑定。
	List(ctx context.Context, userID int64) ([]SupplierPayoutWallet, error)
	// Upsert 绑定/换绑。地址已属于别人时返回 ErrSupplierPayoutAddressTaken。
	Upsert(ctx context.Context, userID int64, network, address string) (*SupplierPayoutWallet, error)
	// Delete 解绑。没有绑定时返回 ErrSupplierPayoutWalletNotFound——
	// 静默成功会让前端以为删掉了一个本来就不存在的东西，掩盖掉调用方传错 network。
	Delete(ctx context.Context, userID int64, network string) error
}
