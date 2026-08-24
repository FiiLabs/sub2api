// APEXONE-EXT: 双边市场——链上打款客户端的接口与两个不触网的默认实现。
//
// M1 把「钱该打到哪」这件事定死了（绑定表 + 建单时快照）。这个文件回答下一个问题：
// 「谁去打」。它只定义**接口**和两个永远不碰网络的实现；真正会广播交易的那个
// 实现在 internal/payoutchain 里，方向是它依赖 service，不是反过来。
//
// # 为什么接口在 service、实现在别处
//
// 与 SupplierPayoutWalletRepository 完全同一个理由：service 是被 handler、worker、
// 测试共同引用的包，它多一个依赖，所有人就都多一个依赖。链上客户端要用到的东西
// （RLP、secp256k1、JSON-RPC）与业务逻辑毫无关系，让它们出现在 service 的 import
// 里，跑一个提现金额校验的单测都要先把它们编译一遍。
//
// # 两个默认实现，区别是「拒绝」还是「假装」
//
// 这里有一个很容易写错的地方，值得写在文件头：
//
//   - DisabledChainClient（生产默认）——**拒绝**广播。没配好金库/私钥/RPC 时用它。
//   - MockChainClient（测试，以及显式打开的演示环境）——**假装**广播成功。
//
// 两者都"不触网"，但后果天差地别。M4 的 worker 拿到一个会假装成功的客户端时，
// 会把一张张提现单标成已打款、写上一个凭空造出来的交易哈希、把供给者的余额清掉——
// 而链上一分钱也没动。也就是说「没配好」这一种最常见的运维状态，如果默认落到
// 假装成功的那一边，表现是**静默地把钱记成已发**。所以生产默认必须是拒绝的那个，
// 假装成功的那个要靠一个名字里就写着 mock 的开关才拿得到。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 确认结果的两个终态。
const (
	// ChainTxConfirmed 交易已上链且回执 status = 1。
	ChainTxConfirmed = "confirmed"
	// ChainTxFailed 交易已上链但回执 status = 0（被 revert，gas 照烧）。
	//
	// 它与「广播失败」是两件事，不能混：这里链上有一笔确定失败的交易，
	// nonce 已经被消耗掉了；广播失败则链上什么都没有，nonce 还能复用。
	ChainTxFailed = "failed"
)

var (
	// ErrSupplierPayoutChainDisabled 链上打款没有配置起来。
	//
	// 5xx 而不是 4xx：调用方没做错任何事。它出现在日志里意味着有人（worker 或
	// 管理端的手动推进）试图广播一笔交易，而这套部署根本没有金库——这是配置问题，
	// 不是业务问题，指向的是运维而不是用户。
	ErrSupplierPayoutChainDisabled = infraerrors.ServiceUnavailable(
		"SUPPLIER_PAYOUT_CHAIN_DISABLED", "on-chain payout is not configured")
	// ErrSupplierPayoutChainNoBatch 这条链没有配批量合约。
	//
	// 调用方应当先问 SupportsBatch。真撞上了要能一眼看出是调用顺序错了，
	// 而不是把它当成一次可重试的链上故障——重试一万次结果都一样。
	ErrSupplierPayoutChainNoBatch = infraerrors.ServiceUnavailable(
		"SUPPLIER_PAYOUT_CHAIN_NO_BATCH", "batch payout contract is not configured for this network")
)

// ChainFeeEstimate 是一笔转账的手续费估算。
type ChainFeeEstimate struct {
	// Amount 折算成计价单位（与提现单的 amount 同一个单位）的手续费。
	//
	// 这是唯一会被写进单子、扣供给者钱的字段。它是**估算**：真实花费要等回执
	// 里的 gasUsed，而那时钱已经打出去了。M3 因此按估算收费并留安全系数，
	// 多收的那一点是平台承担波动的对价，少收了平台自己贴——两个方向都不追溯，
	// 因为追溯要给一张已经终态的单子改金额。
	Amount float64
	// GasPriceWei / GasLimit 只为排查留档：同一个 Amount 可能来自完全不同的两组数，
	// 而「手续费怎么突然贵了」这个问题只有拆开这两项才答得出来。
	//
	// GasPriceWei 是十进制字符串而不是数字：wei 的量级放得下 uint64，但它会进日志
	// 和 JSON，而 JSON 的数字在前端是 float64，超过 2^53 就开始悄悄丢低位。
	GasPriceWei string
	GasLimit    uint64
	// Estimated 为假表示这是回落值（RPC 不可用、没配币价），不是真的问过链。
	//
	// 单拎一个布尔出来，是因为回落值和真实估算长得一模一样：都是一个正数。
	// 没有它，「今天手续费怎么一整天都是 0.5」就没人答得上来是巧合还是节点挂了。
	Estimated bool
}

// ChainTransferParams 是一次单笔转账。
type ChainTransferParams struct {
	Network string
	// Token 稳定币合约地址（小写）。由调用方从单子上取——单子上钉着的是建单那一刻
	// 的合约地址（迁移 234），客户端**不许**自己去配置里查：配置会改，而一张
	// 三个月前的单子该发哪个合约的币，答案必须还是三个月前那个。
	Token string
	// To 收款地址（小写）。
	To string
	// Amount 净额，计价单位。换成代币最小单位由 ToTokenAmount 负责。
	Amount float64
	// Nonce 指定广播用的 nonce；nil = 由节点分配。
	//
	// 重试时**必须**传入与首次相同的值。同一个 nonce 在链上最多只有一笔交易能成功，
	// 这是「广播响应丢了」唯一可靠的去重手段：重发最坏是重复广播、链上择一，
	// 而每次向节点重新要 nonce 会拿到下一个号，于是同一张单子打两次款。
	Nonce *uint64
}

// ChainTransferResult 是一次广播的结果。
type ChainTransferResult struct {
	// TxHash 交易哈希。**广播成功不等于转账成功**——要等 WaitForConfirmation。
	TxHash string
}

// ChainBatchItem 是批量发放里的一笔。
type ChainBatchItem struct {
	To     string
	Amount float64
}

// ChainBatchParams 是一次批量发放。
type ChainBatchParams struct {
	Network string
	// Token 整组共用一种币：disperseToken 一次只发一种，混编分组会拿 A 币的额度
	// 去发 B 币。分组键必须包含它。
	Token string
	// Items 收款明细，顺序即链上参数顺序，方便对账。
	Items []ChainBatchItem
	// Nonce 整组共用一个，重试原样复用。语义同 ChainTransferParams.Nonce。
	Nonce *uint64
}

// ChainAllowanceTopUp 是一次自动补额度的结果（额度本就够用时不产生）。
type ChainAllowanceTopUp struct {
	TxHash string
	// Amount 补到的目标额度，代币最小单位的十进制字符串。
	// 不用数字类型：最小单位在 18 位精度下轻易超过 float64 能精确表示的范围。
	Amount string
	Symbol string
}

// ChainConfirmation 是一笔交易的终态。
type ChainConfirmation struct {
	// Status 取 ChainTxConfirmed / ChainTxFailed 之一。
	Status string
	// Reason 失败原因，写进单子的 last_error 给运营看。成功时为空。
	Reason string
}

// SupplierChainClient 是链上打款能力的全部出口。
//
// 方法分三类，读的时候按这个分类看会清楚很多：
//   - 问链要信息：EstimateFee、NextNonce、WaitForConfirmation
//   - 往链上写：Transfer、TransferBatch、EnsureBatchAllowance
//   - 纯本地判断：SupportsBatch
type SupplierChainClient interface {
	// EstimateFee 估一笔转账的手续费。
	//
	// **不返回错误**是刻意的。这个值出现在提现预览里，而一次 RPC 抖动不该让
	// 供给者看到一个"提现暂时不可用"——他会以为是自己的账号出了问题。取不到时
	// 回落到配置里的保守值并把 Estimated 置假，让降级是可见的而不是可感的。
	EstimateFee(ctx context.Context, network string) ChainFeeEstimate
	// TokenAddress 回答「这条链上的这种币，合约地址是多少」。
	//
	// 同步、不触网：它读的是配置，而调用点在**建单事务之前**——那里不该有一次
	// 可能超时的网络调用。
	//
	// 第二个返回值为假表示这套部署此刻结算不了这个币种：没配金库、链不对，
	// 或者金库配的是另一种币。它是建单时「这张单子走链上还是走人工」的**唯一**
	// 判据——取不到地址就不写 network，于是单子退回 229 的人工工单那条路
	// （见 SupplierWithdrawalService.applyChainSnapshot）。
	//
	// 关键是**不许猜一个地址**。猜出来的地址会让单子带着 network 落库、被 M4 的
	// worker 捞起来、然后打不出去，而供给者的钱在建单那一刻就已经被扣走了。
	// 「没有答案」与「一个凑合的答案」在这里差着一笔卡死的钱。
	TokenAddress(network, symbol string) (string, bool)
	// NextNonce 取金库地址下一个可用的 nonce（含内存池里待打包的）。
	//
	// 调用方应当在广播**之前**把它落库，见 ChainTransferParams.Nonce。
	NextNonce(ctx context.Context, network string) (uint64, error)
	// Transfer 广播一笔转账。
	Transfer(ctx context.Context, params ChainTransferParams) (ChainTransferResult, error)
	// SupportsBatch 这条链能不能批量发。
	//
	// 同步、不触网：调用方要在**分组阶段**就知道能否批量，那时还没到任何一次
	// 网络调用。返回假时逐笔发——批量是省 gas 的优化，不是功能前提，
	// 没配批量合约就安静降级，不该让提现链路整条报错。
	SupportsBatch(network string) bool
	// EnsureBatchAllowance 确认批量合约在这一币种上的额度够发这一组，不够就先补。
	//
	// 必须在调用方**预留 nonce 之前**调用：补额度自己要占用金库地址上的一个 nonce，
	// 此时若已有预留，approve 正好会把它吃掉，之后重发撞 nonce too low。
	//
	// 返回 nil 表示额度本就够用，没有发生任何链上交易。
	EnsureBatchAllowance(ctx context.Context, params ChainBatchParams) (*ChainAllowanceTopUp, error)
	// TransferBatch 一笔交易发给多个收款人（all-or-nothing）。
	// 不支持批量的链返回 ErrSupplierPayoutChainNoBatch。
	TransferBatch(ctx context.Context, params ChainBatchParams) (ChainTransferResult, error)
	// WaitForConfirmation 等一笔交易到终态。
	//
	// 返回错误 = **还不知道**（等超时了、节点连不上），不是失败。调用方必须
	// 把这两种情况分开：不知道的单子要留在 processing 下轮继续等，
	// 判成失败会让一笔可能已经打出去的钱被退回给供给者，等于双付。
	WaitForConfirmation(ctx context.Context, network, txHash string) (ChainConfirmation, error)
}

// ChainAmountScale 是计价单位转成代币最小单位时保留的小数位数。
//
// 取 8 是因为提现单的金额列是 DECIMAL(20,8)：库里存得下的精度就是这么多，
// 再多的位数不来自任何真实数据，只来自 float64 表示误差的尾巴。
const ChainAmountScale = 8

// SanitizeChainFee 把一个手续费估算值收敛成可以落库的形态。
//
// 返回值：收敛后的金额，以及"这个估算值本身是不是可信的"。
//
// # 为什么要收敛
//
// fee_amount 是 DECIMAL(20,8)。估算值来自一串浮点乘法（gasPrice × gasLimit ÷ 1e18
// × 币价 × 安全系数），小数位可以有二十几位。不收敛就落库，数据库会替我们截断，
// 于是**服务算出来的 net 与按库里那一行算出来的 net 不是同一个数**——而 M4 打款时
// 读的是库里那一行。差值只有 1e-9 量级，但两个本该相等的数不相等，
// 是一类会在对账时耗掉一整天的问题。
//
// 方向取四舍五入而不是向上取整：在 1e-8 美元这个量级上，多收还是少收都不是钱，
// 而"服务与数据库算出同一个数"才是要的东西。
//
// # 为什么非有限值要回假，而不是当成 0
//
// 当成 0 就是"这笔提现不收手续费"，而它真实的含义是"链上客户端算出了一个不是数的东西"。
// 前者静默地让金库替所有人垫 gas，后者是个配置故障——必须让它以故障的形态出现。
func SanitizeChainFee(fee float64) (float64, bool) {
	if math.IsNaN(fee) || math.IsInf(fee, 0) || fee < 0 {
		return 0, false
	}
	if fee == 0 {
		return 0, true
	}
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(fee, 'f', ChainAmountScale, 64), 64)
	if err != nil {
		return 0, false
	}
	return rounded, true
}

// ToTokenAmount 把计价单位的金额换算成代币最小单位。
//
// # 为什么不能直接乘
//
// 最自然的写法是 big.NewFloat(amount) 乘上 10^decimals 再取整。它在绝大多数
// 输入上都对，然后在某些输入上少一个最小单位——因为 float64 存不下 12.34 这样的
// 十进制小数，存的是一个略小或略大的二进制近似，乘上 1e18 之后误差被放大到
// 整数位上。BSC 上的 USDT 是 18 位精度，这个误差是实打实会发生的。
//
// 所以走十进制字符串：先按 ChainAmountScale 位定点格式化（这一步的舍入把 float64
// 的近似值拉回它本来想表示的那个十进制数），再纯整数运算放大到 decimals。
// 全程没有一次浮点乘法。
//
// # 负数与不可表示都报错，不夹带默认值
//
// decimals < ChainAmountScale 的币（以太坊主网与不少测试网的 USDT 是 6 位）
// 会让第 7、8 位小数无处安放。这里**向下取整**到代币能表示的位数：
//
//   - 方向与手续费侧同一条原则（估价上浮、fallback 保守）：宁可金库留下零头，
//     绝不多发。零头 < 10^-decimals（6 位币即百万分之一），且**留在金库**——
//     它不属于任何一张单子，不进任何流水，对账口径是「链上实发 = 净额向下取整
//     到代币精度」，写在 §9.9。
//   - 早先这里是直接拒绝（"先想清楚零头怎么记账"）。第一次真链验证就撞上了
//     6 位的 USDT：拒绝等于把以太坊系最主流的稳定币精度整个关在门外，而
//     手续费是 8 位估算值，净额几乎必然带着第 7、8 位的尾巴——每一笔都拒。
//
// 取整后归零的金额仍然拒绝：那不是零头，是整笔钱都小于代币的最小单位，
// 放行就是"安静地转账 0"，收款人什么都拿不到而单子标成 paid。
func ToTokenAmount(amount float64, decimals int) (*big.Int, error) {
	if decimals <= 0 {
		// 0 位精度的"稳定币"不存在于任何我们要打款的场景；负数是坏数据。
		return nil, fmt.Errorf("token decimals %d is not usable", decimals)
	}
	if amount < 0 {
		return nil, fmt.Errorf("payout amount %v is negative", amount)
	}
	// NaN / ±Inf：FormatFloat 会把它们变成 "NaN" / "+Inf"，而那两个串
	// 解析回来不是错误就是 0，两种都不能拿去转账。
	if amount != amount || amount > 1e15 {
		return nil, fmt.Errorf("payout amount %v is not a finite payable number", amount)
	}

	fixed := strconv.FormatFloat(amount, 'f', ChainAmountScale, 64)
	digits := strings.Replace(fixed, ".", "", 1)
	units, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, fmt.Errorf("cannot read payout amount %q as a decimal", fixed)
	}
	if decimals >= ChainAmountScale {
		// 已经是 ChainAmountScale 位了，再补 decimals - ChainAmountScale 个零。
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-ChainAmountScale)), nil)
		return units.Mul(units, scale), nil
	}
	// 精度低于账本：砍掉代币放不下的尾巴（纯整数除法，向零 = 向下，金额非负）。
	drop := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(ChainAmountScale-decimals)), nil)
	units.Quo(units, drop)
	if units.Sign() == 0 && amount > 0 {
		return nil, fmt.Errorf("payout amount %v is below what a %d-decimal token can represent", amount, decimals)
	}
	return units, nil
}

// DisabledChainClient 是生产默认：不触网，且**拒绝**任何广播。
//
// 见文件头。它存在的全部意义是让「没配好金库」这件事在第一次尝试广播时就
// 大声说出来，而不是变成一串标着 paid、链上查无此笔的单子。
type DisabledChainClient struct {
	// FallbackFee EstimateFee 的回落值。
	//
	// 即便打款关着，提现预览里那个手续费数字仍然要能画出来——链上渠道要不要
	// 出现在选项里是管理员的白名单说了算，而白名单可以先于金库配好。
	FallbackFee float64
}

// NewDisabledChainClient 造一个拒绝广播的客户端。
func NewDisabledChainClient(fallbackFee float64) *DisabledChainClient {
	return &DisabledChainClient{FallbackFee: fallbackFee}
}

// EstimateFee 返回回落值，Estimated 恒为假。
func (c *DisabledChainClient) EstimateFee(context.Context, string) ChainFeeEstimate {
	return ChainFeeEstimate{Amount: c.FallbackFee}
}

// TokenAddress 恒假：没配金库就没有合约地址，而**猜一个**会让建单成功、
// 让单子进 pending、让钱被扣走，然后 worker 到了才发现打不出去。
func (c *DisabledChainClient) TokenAddress(string, string) (string, bool) { return "", false }

// NextNonce 恒错。
func (c *DisabledChainClient) NextNonce(context.Context, string) (uint64, error) {
	return 0, ErrSupplierPayoutChainDisabled
}

// Transfer 恒错。
func (c *DisabledChainClient) Transfer(context.Context, ChainTransferParams) (ChainTransferResult, error) {
	return ChainTransferResult{}, ErrSupplierPayoutChainDisabled
}

// SupportsBatch 恒假：没有金库，批量与逐笔一样发不出去。
func (c *DisabledChainClient) SupportsBatch(string) bool { return false }

// EnsureBatchAllowance 恒错。
func (c *DisabledChainClient) EnsureBatchAllowance(context.Context, ChainBatchParams) (*ChainAllowanceTopUp, error) {
	return nil, ErrSupplierPayoutChainDisabled
}

// TransferBatch 恒错。
func (c *DisabledChainClient) TransferBatch(context.Context, ChainBatchParams) (ChainTransferResult, error) {
	return ChainTransferResult{}, ErrSupplierPayoutChainDisabled
}

// WaitForConfirmation 恒错。
//
// 回错而不是回 failed，是这个类型里唯一需要想一下的地方：failed 是一个**终态**，
// 会让上层把单子推进终态并（按 M4 的设计）全额退款。而这里的真实情况是
// 「我们压根没广播过，不知道链上有没有」——错误才是诚实的答案，它让单子留在原地。
func (c *DisabledChainClient) WaitForConfirmation(context.Context, string, string) (ChainConfirmation, error) {
	return ChainConfirmation{}, ErrSupplierPayoutChainDisabled
}

// MockChainClient 是**假装成功**的实现：测试用，以及显式打开的演示环境。
//
// 它绝不接触网络，但会返回一个像模像样的成功结果。这正是它危险的地方——
// 见文件头。拿到它的唯一途径是显式配置，生产默认拿到的是 DisabledChainClient。
//
// 并发安全：M4 的 worker 会并发调它。
type MockChainClient struct {
	mu sync.Mutex

	fee     float64
	outcome string
	batch   bool
	// token 假的合约地址，见 MockChainTokenAddress。空串 = TokenAddress 恒回假。
	token string

	// nonce 每条链一个，模拟节点视角。
	//
	// 只在**实际广播之后**前进（见 Transfer），与真实节点一致：
	// 「取了 nonce 但广播失败 → 重试再取」必须拿到同一个值，
	// 否则 worker 的 nonce 复用逻辑在测试里根本走不到要测的那条路。
	nonce map[string]uint64
	// seq 让同一组参数的两次广播产出不同的哈希——链上重发同一个 nonce
	// 也会因为 gasPrice 不同而得到不同的哈希，测试要能分辨"这是第几次发的"。
	seq uint64

	failTransfer string
	failBatch    string

	transfers []ChainTransferParams
	batches   []ChainBatchParams
}

// MockChainOptions 是 MockChainClient 的初始状态。
type MockChainOptions struct {
	// Fee EstimateFee 返回的金额。
	Fee float64
	// Outcome WaitForConfirmation 的结果，空 = confirmed。
	Outcome string
	// NoBatch 为真时 SupportsBatch 返回假。
	//
	// 默认（零值）是**支持**批量：M5 的分组逻辑要靠它才走得到，
	// 而"默认不支持"会让那些测试全部安静地走进逐笔分支，看起来还是绿的。
	NoBatch bool
	// TokenAddress 覆盖 TokenAddress 返回的地址；空 = MockChainTokenAddress。
	//
	// 留一个覆盖口，是为了让"单子上钉的地址与此刻配置的不一样"这种情况可测——
	// 那正是 M4 该按单子上那一列打款、而不是按配置打款的理由。
	TokenAddress string
	// NoToken 为真时 TokenAddress 恒回假，模拟"链上打款开着，但这种币结算不了"。
	NoToken bool
}

// NewMockChainClient 造一个假装成功的客户端。
func NewMockChainClient(opts MockChainOptions) *MockChainClient {
	outcome := opts.Outcome
	if outcome == "" {
		outcome = ChainTxConfirmed
	}
	token := strings.TrimSpace(opts.TokenAddress)
	if token == "" {
		token = MockChainTokenAddress
	}
	if opts.NoToken {
		token = ""
	}
	return &MockChainClient{
		fee:     opts.Fee,
		outcome: outcome,
		batch:   !opts.NoBatch,
		token:   token,
		nonce:   map[string]uint64{},
	}
}

// MockChainTokenAddress 是假客户端默认返回的合约地址。
//
// 它是一个格式合法、但**一眼假**的地址（末尾 dead）。理由与 MockChainTxPrefix
// 一样：运营在单子上看到它、粘进区块浏览器、发现那不是任何一个稳定币合约，
// 排查就在那一刻结束了。造一个长得像真 USDT 合约的地址，才是这里最容易犯的错。
const MockChainTokenAddress = "0x000000000000000000000000000000000000dead"

// TokenAddress 返回假的合约地址。NoToken 为真时恒假。
func (m *MockChainClient) TokenAddress(string, string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token == "" {
		return "", false
	}
	return m.token, true
}

// EstimateFee 返回配置的固定值。Estimated 为真——它模拟的是"问过链了"。
func (m *MockChainClient) EstimateFee(context.Context, string) ChainFeeEstimate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ChainFeeEstimate{Amount: m.fee, GasPriceWei: "1000000000", GasLimit: 100000, Estimated: true}
}

// SetFee 改手续费。
func (m *MockChainClient) SetFee(fee float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fee = fee
}

// SetOutcome 改 WaitForConfirmation 的结果。
func (m *MockChainClient) SetOutcome(outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcome = outcome
}

// NextNonce 返回当前链上 nonce，不推进。
func (m *MockChainClient) NextNonce(_ context.Context, network string) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nonce[network], nil
}

// BumpNonce 模拟金库地址上发生了一笔与 worker 无关的交易。
//
// 这不是假想场景：批量补额度的 approve、运维手动转账，都会这样推进 nonce，
// 而那正是"预留的 nonce 被吃掉、重发撞 nonce too low"的来源。
func (m *MockChainClient) BumpNonce(network string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nonce[network]++
}

// FailNextTransfer 让下一次 Transfer 报错，模拟广播时 RPC 故障——链上结果未知。
func (m *MockChainClient) FailNextTransfer(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failTransfer = message
}

// FailNextBatch 让下一次 TransferBatch 报错。
func (m *MockChainClient) FailNextBatch(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failBatch = message
}

// Transfers 返回至今广播过的每一笔（副本）。
func (m *MockChainClient) Transfers() []ChainTransferParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ChainTransferParams(nil), m.transfers...)
}

// Batches 返回至今广播过的每一组（副本）。
func (m *MockChainClient) Batches() []ChainBatchParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ChainBatchParams(nil), m.batches...)
}

// Transfer 记一笔并返回一个假哈希。
func (m *MockChainClient) Transfer(_ context.Context, params ChainTransferParams) (ChainTransferResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg := m.failTransfer; msg != "" {
		m.failTransfer = ""
		return ChainTransferResult{}, fmt.Errorf("mock transfer failed: %s", msg)
	}
	m.transfers = append(m.transfers, params)
	m.nonce[params.Network]++
	return ChainTransferResult{TxHash: m.hash(params.Network, params.Token, params.To, params.Amount)}, nil
}

// SupportsBatch 见 MockChainOptions.NoBatch。
func (m *MockChainClient) SupportsBatch(string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.batch
}

// EnsureBatchAllowance 恒返回 nil（假装额度总是够）。
func (m *MockChainClient) EnsureBatchAllowance(_ context.Context, params ChainBatchParams) (*ChainAllowanceTopUp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.batch {
		return nil, ErrSupplierPayoutChainNoBatch
	}
	_ = params
	return nil, nil
}

// TransferBatch 记一组并返回一个假哈希。
func (m *MockChainClient) TransferBatch(_ context.Context, params ChainBatchParams) (ChainTransferResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.batch {
		return ChainTransferResult{}, ErrSupplierPayoutChainNoBatch
	}
	if msg := m.failBatch; msg != "" {
		m.failBatch = ""
		return ChainTransferResult{}, fmt.Errorf("mock batch failed: %s", msg)
	}
	m.batches = append(m.batches, params)
	m.nonce[params.Network]++
	var total float64
	for _, item := range params.Items {
		total += item.Amount
	}
	return ChainTransferResult{TxHash: m.hash(params.Network, params.Token, "batch", total)}, nil
}

// WaitForConfirmation 返回配置的终态。
func (m *MockChainClient) WaitForConfirmation(_ context.Context, _, _ string) (ChainConfirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outcome == ChainTxFailed {
		return ChainConfirmation{Status: ChainTxFailed, Reason: "mock outcome is failed"}, nil
	}
	return ChainConfirmation{Status: ChainTxConfirmed}, nil
}

// MockChainTxPrefix 是假哈希的前缀。
//
// 它刻意**不是**一个合法的交易哈希形态（合法形态是 0x + 64 位十六进制）。
// 造一个长得像真哈希的串是这里最容易犯的错：运营在单子上看到它，粘进区块浏览器，
// 得到"查无此笔"，于是去怀疑节点同步延迟——而真相是这套部署的打款从来没开过。
// 一个一眼假的前缀把那次排查缩短成零。
const MockChainTxPrefix = "mock:"

// hash 造一个确定性的假哈希。调用方已持有 m.mu。
func (m *MockChainClient) hash(parts ...any) string {
	m.seq++
	digest := sha256.Sum256([]byte(fmt.Sprint(m.seq, parts)))
	return MockChainTxPrefix + hex.EncodeToString(digest[:])
}

// IsMockChainTx 判断一个交易哈希是不是假的。
//
// 给管理端和导出用：一份对账文件里混进假哈希时，要能在生成它的那一刻就标出来，
// 而不是等对账的人一个个去区块浏览器上查。
func IsMockChainTx(txHash string) bool {
	return strings.HasPrefix(txHash, MockChainTxPrefix)
}

// 两个默认实现都必须满足接口。写成编译期断言而不是靠调用点：
// 接口以后加方法时，这两行会在**这个文件**里报错，而不是在某个 wire 装配处。
var (
	_ SupplierChainClient = (*DisabledChainClient)(nil)
	_ SupplierChainClient = (*MockChainClient)(nil)
)
