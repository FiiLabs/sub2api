// APEXONE-EXT: 双边市场——链上打款的配置，只从环境变量读。
//
// # 为什么不走 internal/config
//
// 那个包里的字段全都带 mapstructure 标签，也就是说它们可以从配置文件里来、
// 也可能被写回配置文件。金库私钥不能沾这条路：一份带私钥的配置文件会被复制、
// 会进备份、会被误提交。这里只认环境变量，值只在进程内存里存在。
//
// 同样的理由，本文件里没有任何一处把私钥放进 String()、日志或错误消息。
// Config 有意**没有**实现 fmt.Stringer——一个 %v 打出整个结构体的坑，
// 靠的是让结构体本身不含明文私钥之外的诱惑：SignerKey 字段在校验完就交给
// signer，之后不再被读。
package payoutchain

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// 环境变量名。前缀 PAYOUT_ 之后跟网络名，为的是将来加第二条链时不用改这里的结构。
const (
	envEnabled       = "PAYOUT_ENABLED"
	envMock          = "PAYOUT_MOCK"
	envRPCURL        = "PAYOUT_BSC_RPC_URL"
	envSignerKey     = "PAYOUT_BSC_SIGNER_KEY"
	envTokenAddress  = "PAYOUT_BSC_TOKEN_ADDRESS"
	envTokenSymbol   = "PAYOUT_BSC_TOKEN_SYMBOL"
	envDisperse      = "PAYOUT_BSC_DISPERSE_ADDRESS"
	envChainID       = "PAYOUT_BSC_CHAIN_ID"
	envNativeUSD     = "PAYOUT_BSC_NATIVE_USD"
	envConfirmations = "PAYOUT_BSC_CONFIRMATIONS"
	envFallbackFee   = "PAYOUT_BSC_FALLBACK_FEE"
	envFeeMultiplier = "PAYOUT_FEE_SAFETY_MULTIPLIER"
)

// 默认值。
const (
	defaultChainID       = 56 // BSC 主网
	// defaultTokenSymbol 与 service 里那张链上渠道注册表上的 BSC-USDT 对上。
	//
	// 做成一个可覆盖的默认值而不是写死："这个金库配的是哪种币"必须是可问的——
	// 建单时要拿它去核对渠道要的币种，而把 USDC 的合约地址填进一个只认 USDT 的
	// 客户端里，是一件既不会报错也不会被发现的事，直到有人收到了错的币。
	defaultTokenSymbol = "USDT"
	defaultConfirmations = 3
	// defaultFallbackFee 是估不出手续费时按每笔多少美元扣。
	//
	// BSC 上一笔 ERC-20 转账大约 0.1~0.3 美元。0.5 是个保守值：宁可多扣一点
	// 让金库不亏，也不要少扣到金库慢慢被 gas 耗干——那种亏损没有对账入口，
	// 会一直到金库转不动账才被发现。
	defaultFallbackFee = 0.5
	// defaultFeeMultiplier 给估算值留的余量。gas 价在广播那一刻可能比估的时候高。
	defaultFeeMultiplier = 1.5
)

// Config 是链上打款需要的全部配置。
type Config struct {
	// Enabled 总开关。关着的时候工厂给出 DisabledChainClient（拒绝一切），
	// 而不是 mock（假装成功）——见 service/supplier_payout_chain.go 文件头。
	Enabled bool
	// Mock 显式要求用假客户端。只给联调环境用。
	//
	// 单独一个开关而不是"Enabled 但没配 RPC 就退回 mock"：后者会让一次配置
	// 疏漏（RPC 地址写错、密钥没注入）表现成"打款一切正常"。
	Mock bool

	RPCURL string
	// SignerKey 金库私钥，十六进制。绝不出现在任何输出里。
	SignerKey string
	// TokenAddress 稳定币在 BSC 上的合约地址。
	TokenAddress string
	// TokenSymbol 上面那个合约地址对应的币种符号，默认 USDT。
	//
	// 它是 TokenAddress 的**标签**，不是选择器：改它不会换一个合约，
	// 只会改变这个客户端愿意为哪个渠道结算。两者对不上时建单会被拒。
	TokenSymbol string
	// DisperseAddress 批量发放合约。留空则退化为逐笔转账。
	DisperseAddress string
	ChainID         uint64
	// NativeUSD 一个 BNB 值多少美元。
	//
	// 这是个**配置里的常数，不是喂价**：手续费只是从供给者收益里扣的一小笔，
	// 为它接一个预言机意味着多一个会挂、会被操纵、会让提现链路整条不可用的
	// 外部依赖。填 0 表示不换算，直接用 FallbackFee。
	NativeUSD float64
	// Confirmations 认为一笔交易到终态需要多少个确认。
	Confirmations uint64
	// FallbackFee 估不出来时按每笔多少美元扣。
	FallbackFee float64
	// FeeMultiplier 估算值的安全系数。
	FeeMultiplier float64
}

// LoadConfig 从环境变量读配置。
//
// 只有在 Enabled 为真时才校验那些必填项：默认（不启用）状态下部署一个什么都没配的
// 实例必须能正常起来，否则每一个不打款的环境都要先编一份假配置才能跑。
func LoadConfig() (Config, error) {
	cfg := Config{
		Enabled:         envBool(envEnabled),
		Mock:            envBool(envMock),
		RPCURL:          envString(envRPCURL),
		SignerKey:       envString(envSignerKey),
		TokenAddress:    envString(envTokenAddress),
		TokenSymbol:     defaultTokenSymbol,
		DisperseAddress: envString(envDisperse),
		ChainID:         defaultChainID,
		Confirmations:   defaultConfirmations,
		FallbackFee:     defaultFallbackFee,
		FeeMultiplier:   defaultFeeMultiplier,
	}

	if symbol := strings.TrimSpace(envString(envTokenSymbol)); symbol != "" {
		cfg.TokenSymbol = symbol
	}

	var err error
	if cfg.ChainID, err = envUint(envChainID, defaultChainID); err != nil {
		return Config{}, err
	}
	if cfg.Confirmations, err = envUint(envConfirmations, defaultConfirmations); err != nil {
		return Config{}, err
	}
	if cfg.NativeUSD, err = envFloat(envNativeUSD, 0); err != nil {
		return Config{}, err
	}
	if cfg.FallbackFee, err = envFloat(envFallbackFee, defaultFallbackFee); err != nil {
		return Config{}, err
	}
	if cfg.FeeMultiplier, err = envFloat(envFeeMultiplier, defaultFeeMultiplier); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate 检查这份配置自洽。
func (c Config) validate() error {
	if c.Confirmations == 0 {
		// 0 个确认意味着"进了内存池就算成功"，而内存池里的交易会被重组掉。
		return fmt.Errorf("payoutchain: %s must be at least 1", envConfirmations)
	}
	if c.FallbackFee < 0 {
		return fmt.Errorf("payoutchain: %s must not be negative", envFallbackFee)
	}
	if c.FeeMultiplier < 1 {
		// 小于 1 的系数会把估算值往下压，那是在赌 gas 价只会跌。
		return fmt.Errorf("payoutchain: %s must be at least 1", envFeeMultiplier)
	}
	if c.NativeUSD < 0 {
		return fmt.Errorf("payoutchain: %s must not be negative", envNativeUSD)
	}

	if !c.Enabled || c.Mock {
		// 没启用，或者显式要 mock —— 下面那些链上参数都用不到。
		return nil
	}
	if c.ChainID == 0 {
		return fmt.Errorf("payoutchain: %s must not be zero", envChainID)
	}
	if c.RPCURL == "" {
		return fmt.Errorf("payoutchain: %s is required when %s is on", envRPCURL, envEnabled)
	}
	if c.SignerKey == "" {
		return fmt.Errorf("payoutchain: %s is required when %s is on", envSignerKey, envEnabled)
	}
	if c.TokenAddress == "" {
		return fmt.Errorf("payoutchain: %s is required when %s is on", envTokenAddress, envEnabled)
	}
	if _, err := parseAddress(c.TokenAddress); err != nil {
		return fmt.Errorf("payoutchain: %s: %w", envTokenAddress, err)
	}
	if c.DisperseAddress != "" {
		if _, err := parseAddress(c.DisperseAddress); err != nil {
			return fmt.Errorf("payoutchain: %s: %w", envDisperse, err)
		}
	}
	return nil
}

func envString(name string) string { return strings.TrimSpace(os.Getenv(name)) }

// envBool 只认明确的真值。空、"0"、"false"、以及任何看不懂的串都是假。
//
// 看不懂就当假，是因为这个开关的两边不对称：错判成假，打款不发生，运维会看到
// 单子堆在队列里；错判成真，钱就出去了。
func envBool(name string) bool {
	switch strings.ToLower(envString(name)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envUint(name string, fallback uint64) (uint64, error) {
	raw := envString(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		// 不静默回落到默认值：一个写错的 chain id 静默变成 56，
		// 会让本来要发到测试网的交易签成主网的。
		return 0, fmt.Errorf("payoutchain: %s must be a whole number, got %q", name, raw)
	}
	return value, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	raw := envString(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("payoutchain: %s must be a number, got %q", name, raw)
	}
	return value, nil
}
