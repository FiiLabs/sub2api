//go:build unit

// APEXONE-EXT: 双边市场——金库配置（M6）的单元测试。
//
// 重心全在私钥的三条纪律上：形状不对不加密、明文不落库、读路径不吐钥匙。
// 三条各自失守的形态都是静默的：加密一段粘错的东西 = 把「配错了」推迟到
// 第一次打款；明文落库 = 一份 pg_dump 就是金库；读路径带钥匙 = 任何一个
// 打这个接口的管理端页面都在往浏览器里发私钥。
package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passthroughEncryptor 前缀式假加密：足够区分「加密过」与「没加密」。
type passthroughEncryptor struct{}

func (passthroughEncryptor) Encrypt(plaintext string) (string, error) { return "sealed:" + plaintext, nil }
func (passthroughEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "sealed:"), nil
}

const payoutTestKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSealSupplyPayoutSignerKeyValidatesShapeFirst(t *testing.T) {
	enc := passthroughEncryptor{}

	t.Run("64 位十六进制放行，0x 可选", func(t *testing.T) {
		for _, key := range []string{payoutTestKey, "0x" + payoutTestKey, "  " + payoutTestKey + "  "} {
			sealed, err := SealSupplyPayoutSignerKey(enc, key)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(sealed, "enc.v1:"), "落库形态必须带版本前缀")
		}
	})

	t.Run("粘错的东西在加密之前被拒", func(t *testing.T) {
		for name, bad := range map[string]string{
			"太短":   payoutTestKey[:63],
			"太长":   payoutTestKey + "0",
			"非十六进制": strings.Replace(payoutTestKey, "0", "g", 1),
			"助记词":  "abandon abandon abandon abandon abandon abandon",
			"空串":   "",
		} {
			t.Run(name, func(t *testing.T) {
				_, err := SealSupplyPayoutSignerKey(enc, bad)
				require.Error(t, err)
				// 错误消息里不许出现输入内容——它可能就是一把差一位的真私钥。
				if bad != "" {
					assert.NotContains(t, err.Error(), bad[:6])
				}
			})
		}
	})
}

func TestOpenSupplyPayoutSignerKeyRoundTripsAndFailsLoud(t *testing.T) {
	enc := passthroughEncryptor{}
	sealed, err := SealSupplyPayoutSignerKey(enc, payoutTestKey)
	require.NoError(t, err)

	plain, err := OpenSupplyPayoutSignerKey(enc, sealed)
	require.NoError(t, err)
	assert.Equal(t, payoutTestKey, plain)

	// 没有前缀 = 不是我们封的：必须报错，不能猜着解。
	_, err = OpenSupplyPayoutSignerKey(enc, payoutTestKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enc.v1:")
}

// validate：开着时必填项缺一不可，且非密文形态的私钥被拒——
// 「明文私钥落库」只能来自代码路径写错，validate 是最后一道闸。
func TestSupplyPayoutChainSettingsValidate(t *testing.T) {
	valid := func() *SupplyPayoutChainSettings {
		return &SupplyPayoutChainSettings{
			Enabled:       true,
			RPCURL:        "https://node",
			SignerKey:     "enc.v1:whatever",
			TokenAddress:  "0x55d398326f99059ff775485246999027b3197955",
			TokenSymbol:   "USDT",
			ChainID:       56,
			Confirmations: 3,
			FallbackFee:   0.5,
			FeeMultiplier: 1.5,
		}
	}
	require.NoError(t, valid().validate())

	t.Run("明文私钥拒绝落库", func(t *testing.T) {
		s := valid()
		s.SignerKey = payoutTestKey // 没有 enc.v1: 前缀
		require.Error(t, s.validate())
	})
	t.Run("关着时链上参数全可缺省", func(t *testing.T) {
		require.NoError(t, DefaultSupplyPayoutChainSettings().validate())
	})
	t.Run("零确认拒绝", func(t *testing.T) {
		s := valid()
		s.Confirmations = 0
		require.Error(t, s.validate())
	})
	t.Run("安全系数小于 1 拒绝", func(t *testing.T) {
		s := valid()
		s.FeeMultiplier = 0.9
		require.Error(t, s.validate())
	})
	t.Run("坏的合约地址拒绝", func(t *testing.T) {
		s := valid()
		s.TokenAddress = "0x1234"
		require.Error(t, s.validate())
	})
	t.Run("开着但没配 RPC 拒绝", func(t *testing.T) {
		s := valid()
		s.RPCURL = ""
		require.Error(t, s.validate())
	})
}

// 真 SettingService 的读路径**必须**抹掉私钥——上一轮变异矩阵抓出这条纪律
// 只在桩上成立（G4 GREEN）：桩自己做了抹除，真实现里那一行删掉也测不出来。
// 这里喂一份带密文的真 JSON，走真的 GetSupplyPayoutChainSettings。
func TestGetSupplyPayoutChainSettingsNeverReturnsTheKey(t *testing.T) {
	stored := `{"enabled":true,"rpc_url":"https://node","signer_key":"enc.v1:sealed-secret",` +
		`"token_address":"0x55d398326f99059ff775485246999027b3197955","token_symbol":"USDT",` +
		`"chain_id":56,"confirmations":3,"fallback_fee":0.5,"fee_multiplier":1.5}`
	svc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeySupplyPayoutChain: stored,
	}}, nil)

	settings, storedFlag := svc.GetSupplyPayoutChainSettings(context.Background())
	assert.True(t, storedFlag)
	assert.Empty(t, settings.SignerKey, "读路径把私钥（哪怕是密文）吐出去了——它会进任何一个调用方的 JSON 响应")
	assert.Equal(t, "https://node", settings.RPCURL, "抹钥匙不该把别的字段也抹了")

	// 密文只从专用通道拿。
	assert.Equal(t, "enc.v1:sealed-secret", svc.SupplyPayoutChainSignerCiphertext(context.Background()))
}
