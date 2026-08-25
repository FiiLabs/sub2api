// APEXONE-EXT: 双边市场——链上打款的金库配置（M6）。
//
// 第七个 settings key。此前这组配置只走环境变量（§9.5：私钥不沾 mapstructure
// 那条会被写回配置文件的路），M6 把它挪进控制台，代价与对策都在私钥上：
//
//   - 落库的是**密文**（enc.v1: 前缀，AES-256-GCM，与收款账号同一把钥匙同一套
//     保护）：一份 pg_dump 拿不到能签交易的东西。
//   - 读路径**永不返回私钥**：GetSupplyPayoutChainSettings 出来的 SignerKey
//     恒为空串，密文只提供给持有解密器的消费者（payoutchain.Manager）。
//     管理端界面上它是只写字段，回显的只有从私钥推导出的金库地址。
//   - 更新时留空 = 保留旧钥匙：换别的参数不需要重新粘一遍私钥。
//
// 环境变量那条路仍然认（settings 没存过时回落），是给存量部署的迁移期用的；
// settings 存过一次之后，env 里除 PAYOUT_MOCK 外的一切不再生效——配置的答案
// 不能有两个来源。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SettingKeySupplyPayoutChain 链上打款配置的 settings key。
const SettingKeySupplyPayoutChain = "supply_payout_chain_settings"

// supplyPayoutSignerCipherPrefix 私钥密文的版本标签。
// 与收款账号的前缀（repository 侧）同一个字面量：它们走的是同一个加密器。
const supplyPayoutSignerCipherPrefix = "enc.v1:"

// supplyPayoutSignerKeyPattern 私钥的形状：64 位十六进制，0x 前缀可选。
// 这里只验形状；「它是不是一把能用的 secp256k1 私钥」由 payoutchain 在构造
// 客户端时判——全零、超曲线阶这类坏值在那里被拒。
var supplyPayoutSignerKeyPattern = regexp.MustCompile(`^(0x)?[0-9a-fA-F]{64}$`)

// SupplyPayoutChainSettings 是链上打款的全部可配内容。
//
// 数值字段的默认与边界刻意与 payoutchain.Config 的 validate 一致——两边不一致
// 的话，控制台存得进去、客户端造不出来，运营看到的是一次保存成功之后的静默失效。
type SupplyPayoutChainSettings struct {
	// Enabled 总开关。关着 = DisabledChainClient，拒绝一切广播。
	Enabled bool `json:"enabled"`
	// RPCURL 节点地址。
	RPCURL string `json:"rpc_url"`
	// SignerKey 金库私钥的**密文**（enc.v1: 前缀）。
	//
	// 这个字段在任何读路径上都被抹成空串（见 GetSupplyPayoutChainSettings），
	// 密文本身只由 SupplyPayoutChainSignerCiphertext 提供给持有解密器的消费者。
	SignerKey string `json:"signer_key"`
	// TokenAddress 稳定币合约地址。
	TokenAddress string `json:"token_address"`
	// TokenSymbol 上面那个合约的标签，默认 USDT。改它不会换合约，
	// 只会改变客户端愿意为哪个渠道结算。
	TokenSymbol string `json:"token_symbol"`
	// DisperseAddress 批量合约。留空 = 退化为逐笔转账。
	DisperseAddress string `json:"disperse_address"`
	// ChainID 链 ID。主网 56，测试网 97。
	ChainID uint64 `json:"chain_id"`
	// NativeUSD 一个 BNB 值多少美元（配置常数，不是喂价）。0 = 不换算。
	NativeUSD float64 `json:"native_usd"`
	// Confirmations 几个确认算终态。
	Confirmations uint64 `json:"confirmations"`
	// FallbackFee 估不出手续费时每笔扣多少美元。
	FallbackFee float64 `json:"fallback_fee"`
	// FeeMultiplier 估算值的安全系数。
	FeeMultiplier float64 `json:"fee_multiplier"`
}

// DefaultSupplyPayoutChainSettings 返回「关闭」状态的默认配置。
// 与其它六个 key 同一个上线策略：代码先进生产，管理员显式打开。
func DefaultSupplyPayoutChainSettings() *SupplyPayoutChainSettings {
	return &SupplyPayoutChainSettings{
		Enabled:       false,
		TokenSymbol:   "USDT",
		ChainID:       56,
		Confirmations: 3,
		FallbackFee:   0.5,
		FeeMultiplier: 1.5,
	}
}

// validate 写路径校验。方向与提现参数一致：越界一律拒绝，不 clamp。
func (s *SupplyPayoutChainSettings) validate() error {
	if s.Confirmations == 0 {
		// 0 个确认 = 进了内存池就算成功，而内存池里的交易会被重组掉。
		return fmt.Errorf("confirmations must be at least 1")
	}
	if s.FallbackFee < 0 {
		return fmt.Errorf("fallback fee must not be negative")
	}
	if s.FeeMultiplier < 1 {
		// 小于 1 的系数是在赌 gas 价只会跌。
		return fmt.Errorf("fee safety multiplier must be at least 1")
	}
	if s.NativeUSD < 0 {
		return fmt.Errorf("native token USD price must not be negative")
	}
	if !s.Enabled {
		// 关着的时候链上参数都用不到——允许分步填写。
		return nil
	}
	if s.ChainID == 0 {
		return fmt.Errorf("chain id must not be zero")
	}
	if strings.TrimSpace(s.RPCURL) == "" {
		return fmt.Errorf("rpc url is required when on-chain payout is enabled")
	}
	if strings.TrimSpace(s.SignerKey) == "" {
		return fmt.Errorf("treasury signer key is required when on-chain payout is enabled")
	}
	if !strings.HasPrefix(s.SignerKey, supplyPayoutSignerCipherPrefix) {
		// 只可能是代码路径写错了（明文私钥绝不允许落库），不是运营能修的错。
		return fmt.Errorf("signer key must be sealed before it is stored")
	}
	if _, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, s.TokenAddress); err != nil {
		return fmt.Errorf("token address: %w", err)
	}
	if trimmed := strings.TrimSpace(s.DisperseAddress); trimmed != "" {
		if _, err := NormalizeSupplierPayoutAddress(SupplierPayoutNetworkBSC, trimmed); err != nil {
			return fmt.Errorf("disperse address: %w", err)
		}
	}
	return nil
}

// sanitize 读路径整形：字符串去空白、符号补默认。
func (s *SupplyPayoutChainSettings) sanitize() {
	s.RPCURL = strings.TrimSpace(s.RPCURL)
	s.SignerKey = strings.TrimSpace(s.SignerKey)
	s.TokenAddress = strings.TrimSpace(s.TokenAddress)
	s.DisperseAddress = strings.TrimSpace(s.DisperseAddress)
	if strings.TrimSpace(s.TokenSymbol) == "" {
		s.TokenSymbol = "USDT"
	} else {
		s.TokenSymbol = strings.TrimSpace(s.TokenSymbol)
	}
}

// SealSupplyPayoutSignerKey 把明文私钥变成可落库的密文。
//
// 形状校验在加密**之前**：一段粘错的东西（比如助记词、比如地址）不该被
// 成功加密存进去——那会把「配错了」推迟到第一次打款才发现。
//
// keyDurable = 加密钥匙是不是固定配置的（totp.encryption_key）。没配时进程
// 每次启动**随机生成**一把——用它加密的私钥在下一次重启后就再也解不开，
// 症状是 "message authentication failed"，而保存那一刻一切正常。宁可当场
// 拒绝，也不给一个必然会过期的成功。
func SealSupplyPayoutSignerKey(encryptor SecretEncryptor, plaintext string, keyDurable bool) (string, error) {
	if !keyDurable {
		return "", fmt.Errorf("refusing to store the treasury key: totp.encryption_key is auto-generated on every boot, so the sealed key would become unreadable after the next restart — set a fixed totp.encryption_key (64 hex chars, or env TOTP_ENCRYPTION_KEY) first")
	}
	trimmed := strings.TrimSpace(plaintext)
	if !supplyPayoutSignerKeyPattern.MatchString(trimmed) {
		// 错误消息里没有输入内容——它可能就是一把差一位的真私钥。
		return "", fmt.Errorf("signer key must be 64 hex characters (0x prefix optional); got %d characters", len(trimmed))
	}
	if encryptor == nil {
		return "", fmt.Errorf("secret encryptor is not configured")
	}
	sealed, err := encryptor.Encrypt(trimmed)
	if err != nil {
		return "", fmt.Errorf("seal signer key: %w", err)
	}
	return supplyPayoutSignerCipherPrefix + sealed, nil
}

// OpenSupplyPayoutSignerKey 解开私钥密文。只给 payoutchain.Manager 用。
func OpenSupplyPayoutSignerKey(encryptor SecretEncryptor, sealed string) (string, error) {
	if !strings.HasPrefix(sealed, supplyPayoutSignerCipherPrefix) {
		return "", fmt.Errorf("signer key is not sealed (missing %q prefix)", supplyPayoutSignerCipherPrefix)
	}
	if encryptor == nil {
		return "", fmt.Errorf("secret encryptor is not configured")
	}
	plain, err := encryptor.Decrypt(strings.TrimPrefix(sealed, supplyPayoutSignerCipherPrefix))
	if err != nil {
		// 与收款账号同一条规矩：解不开必须炸，不能返回空串——空私钥往下走
		// 会在构造客户端时被拒，但错误会说成"没配私钥"，把运维引去重新粘一遍。
		// 提示里点名最常见的成因：加密钥匙没固定、重启后换了一把。
		return "", fmt.Errorf("unseal signer key (key mismatch or corrupt ciphertext — most likely totp.encryption_key is not fixed and changed on restart; set a fixed key and re-paste the treasury key): %w", err)
	}
	return plain, nil
}

const supplyPayoutChainDBTimeout = 3 * time.Second

// GetSupplyPayoutChainSettings 读链上打款配置。第二个返回值 = settings 里存没存过
// （没存过时 payoutchain.Manager 回落到环境变量，给存量部署一条迁移路）。
//
// 返回的 SignerKey **恒为空串**：这个方法的调用方包括管理端 GET，而私钥
// 连密文都不该出现在任何 HTTP 响应里。要密文走 SupplyPayoutChainSignerCiphertext。
//
// 不做缓存：它只在客户端重建与管理端读写时被调，频率与提现设置差三个数量级。
func (s *SettingService) GetSupplyPayoutChainSettings(ctx context.Context) (*SupplyPayoutChainSettings, bool) {
	settings, stored := s.readSupplyPayoutChainSettings(ctx)
	settings.SignerKey = ""
	return settings, stored
}

// SupplyPayoutChainSignerCiphertext 返回私钥**密文**（可能为空串 = 没配过）。
// 唯一的预期消费者是 payoutchain.Manager——它持有解密器。
func (s *SettingService) SupplyPayoutChainSignerCiphertext(ctx context.Context) string {
	settings, _ := s.readSupplyPayoutChainSettings(ctx)
	return settings.SignerKey
}

func (s *SettingService) readSupplyPayoutChainSettings(ctx context.Context) (*SupplyPayoutChainSettings, bool) {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyPayoutChainSettings(), false
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyPayoutChainDBTimeout)
	defer cancel()

	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyPayoutChain)
	if err != nil || strings.TrimSpace(raw) == "" {
		// 读失败与没存过在这里同一个答案：默认（关）。读失败时 Manager 会
		// 保持上一个客户端不动，所以一次数据库抖动不会把 LIVE 降级成 Disabled。
		return DefaultSupplyPayoutChainSettings(), false
	}
	var parsed SupplyPayoutChainSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return DefaultSupplyPayoutChainSettings(), false
	}
	parsed.sanitize()
	return &parsed, true
}

// SetSupplyPayoutChainSettings 写链上打款配置。SignerKey 必须已经是密文
// （handler 层负责 Seal），这里的 validate 会拒绝任何非密文形态的非空私钥。
func (s *SettingService) SetSupplyPayoutChainSettings(ctx context.Context, settings *SupplyPayoutChainSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings must not be nil")
	}
	settings.sanitize()
	if err := settings.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply payout chain settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyPayoutChain, string(data)); err != nil {
		return fmt.Errorf("save supply payout chain settings: %w", err)
	}
	return nil
}
