// APEXONE-EXT: 配置与工厂的测试。
package payoutchain

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// setEnv 设一批环境变量，测试结束自动还原（t.Setenv 会处理）。
func setEnv(t *testing.T, values map[string]string) {
	t.Helper()
	// 先把所有相关变量清空，免得跑测试的机器上恰好设了其中某一个。
	for _, name := range []string{
		envEnabled, envMock, envRPCURL, envSignerKey, envTokenAddress,
		envTokenSymbol, envDisperse, envChainID, envNativeUSD, envConfirmations,
		envFallbackFee, envFeeMultiplier,
	} {
		t.Setenv(name, "")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func TestLoadConfigDefaultsToOffAndStillLoads(t *testing.T) {
	// 一个什么都没配的实例必须能正常起来。否则每一个不打款的环境
	// （本地、CI、预览）都要先编一份假配置才能跑。
	setEnv(t, nil)

	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.Mock)
	assert.Equal(t, uint64(defaultChainID), cfg.ChainID)
	assert.Equal(t, uint64(defaultConfirmations), cfg.Confirmations)
	assert.Equal(t, defaultFallbackFee, cfg.FallbackFee)
	assert.Equal(t, defaultFeeMultiplier, cfg.FeeMultiplier)
}

func TestLoadConfigReadsAFullSetup(t *testing.T) {
	setEnv(t, map[string]string{
		envEnabled:       "true",
		envRPCURL:        "https://bsc-dataseed.example/",
		envSignerKey:     "0x" + keyBSC,
		envTokenAddress:  addrBSCUSDT,
		envDisperse:      addrOther,
		envChainID:       "56",
		envNativeUSD:     "612.5",
		envConfirmations: "5",
		envFallbackFee:   "0.4",
		envFeeMultiplier: "2",
	})

	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, uint64(56), cfg.ChainID)
	assert.Equal(t, uint64(5), cfg.Confirmations)
	assert.Equal(t, 612.5, cfg.NativeUSD)
	assert.Equal(t, 0.4, cfg.FallbackFee)
	assert.Equal(t, 2.0, cfg.FeeMultiplier)
}

func TestEnabledWithoutTheEssentialsIsAnError(t *testing.T) {
	// 开着开关却没配齐，必须在启动时就说清楚缺哪一个——而不是等到
	// 第一笔提现走到一半才发现。
	full := map[string]string{
		envEnabled:      "1",
		envRPCURL:       "https://bsc-dataseed.example/",
		envSignerKey:    keyBSC,
		envTokenAddress: addrBSCUSDT,
	}
	for _, missing := range []string{envRPCURL, envSignerKey, envTokenAddress} {
		t.Run("缺 "+missing, func(t *testing.T) {
			values := map[string]string{}
			for k, v := range full {
				values[k] = v
			}
			delete(values, missing)
			setEnv(t, values)

			_, err := LoadConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), missing, "错误里要指名道姓说缺哪个")
		})
	}
}

func TestConfigErrorsNeverEchoTheSignerKey(t *testing.T) {
	// 配置报错这条路径最容易把私钥带进日志：错误消息通常会被原样记下来。
	setEnv(t, map[string]string{
		envEnabled:      "1",
		envRPCURL:       "https://bsc-dataseed.example/",
		envSignerKey:    keyBSC + "ff", // 长度不对
		envTokenAddress: addrBSCUSDT,
	})
	cfg, err := LoadConfig()
	require.NoError(t, err, "长度问题要到造 signer 时才发现")

	_, err = New(cfg, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), keyBSC)
	assert.NotContains(t, err.Error(), keyBSC[:16])
}

func TestMalformedNumbersAreRejectedInsteadOfSilentlyDefaulting(t *testing.T) {
	// 一个写错的 chain id 静默回落成 56，会让本该发到测试网的交易签成主网的。
	for _, tc := range []struct{ name, value string }{
		{envChainID, "fifty-six"},
		{envChainID, "0x38"}, // 十六进制不收，只认十进制
		{envConfirmations, "many"},
		{envNativeUSD, "$600"},
		{envFallbackFee, "cheap"},
		{envFeeMultiplier, "1.5x"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			setEnv(t, map[string]string{tc.name: tc.value})
			_, err := LoadConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.name)
		})
	}
}

func TestSelfInconsistentSettingsAreRejected(t *testing.T) {
	t.Run("零个确认", func(t *testing.T) {
		// "进了内存池就算成功"——而内存池里的交易会被重组掉。
		setEnv(t, map[string]string{envConfirmations: "0"})
		_, err := LoadConfig()
		require.Error(t, err)
	})
	t.Run("安全系数小于 1", func(t *testing.T) {
		// 把估算值往下压，等于赌 gas 价只会跌。
		setEnv(t, map[string]string{envFeeMultiplier: "0.8"})
		_, err := LoadConfig()
		require.Error(t, err)
	})
	t.Run("负的回落手续费", func(t *testing.T) {
		setEnv(t, map[string]string{envFallbackFee: "-1"})
		_, err := LoadConfig()
		require.Error(t, err)
	})
	t.Run("币地址不成形", func(t *testing.T) {
		setEnv(t, map[string]string{
			envEnabled: "1", envRPCURL: "https://x.example/",
			envSignerKey: keyBSC, envTokenAddress: "0xnot-an-address",
		})
		_, err := LoadConfig()
		require.Error(t, err)
	})
	t.Run("批量合约地址不成形", func(t *testing.T) {
		setEnv(t, map[string]string{
			envEnabled: "1", envRPCURL: "https://x.example/",
			envSignerKey: keyBSC, envTokenAddress: addrBSCUSDT,
			envDisperse: "0xnope",
		})
		_, err := LoadConfig()
		require.Error(t, err)
	})
}

func TestEnabledFlagOnlyAcceptsExplicitTruths(t *testing.T) {
	// 这个开关的两边不对称：错判成假，钱不发，运维看到单子堆积；
	// 错判成真，钱就出去了。所以看不懂的值一律当假。
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Run("真: "+value, func(t *testing.T) {
			setEnv(t, map[string]string{
				envEnabled: value, envRPCURL: "https://x.example/",
				envSignerKey: keyBSC, envTokenAddress: addrBSCUSDT,
			})
			cfg, err := LoadConfig()
			require.NoError(t, err)
			assert.True(t, cfg.Enabled)
		})
	}
	for _, value := range []string{"", "0", "false", "no", "off", "maybe", "enabled"} {
		t.Run("假: "+value, func(t *testing.T) {
			setEnv(t, map[string]string{envEnabled: value})
			cfg, err := LoadConfig()
			require.NoError(t, err)
			assert.False(t, cfg.Enabled)
		})
	}
}

func TestResolvePicksTheSafeClientWhenNothingIsConfigured(t *testing.T) {
	// 默认必须落在"拒绝"而不是"假装成功"：后者会把工单安静地标成已付、
	// 把供给者余额清零，而链上什么都没发生，也没有一条错误日志。
	resolved, err := Resolve(context.Background(), Config{FallbackFee: 0.5}, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeDisabled, resolved.Mode)

	_, err = resolved.Client.Transfer(context.Background(), service.ChainTransferParams{
		Network: "bsc", To: addrRecipient, Amount: 1,
	})
	assert.ErrorIs(t, err, service.ErrSupplierPayoutChainDisabled)

	// 手续费仍然给得出——提现预览不该因为打款没配好而整页报错。
	assert.Equal(t, 0.5, resolved.Client.EstimateFee(context.Background(), "bsc").Amount)
}

func TestResolveIgnoresAStrayMockFlag(t *testing.T) {
	// PAYOUT_MOCK 被误抄进生产配置是会发生的事。它单独出现时必须无效——
	// 要两个变量同时抄错，生产环境才会假装打款。
	resolved, err := Resolve(context.Background(), Config{Mock: true, FallbackFee: 0.5}, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeDisabled, resolved.Mode, "只设 mock 而没设 enabled，应当落到拒绝")
}

func TestResolveGivesTheMockOnlyWhenBothFlagsAreOn(t *testing.T) {
	resolved, err := Resolve(context.Background(), Config{Enabled: true, Mock: true, FallbackFee: 0.5}, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeMock, resolved.Mode)
	assert.Contains(t, resolved.Summary, "MOCK", "启动日志里必须大声说这是假的")

	result, err := resolved.Client.Transfer(context.Background(), service.ChainTransferParams{
		Network: "bsc", To: addrRecipient, Amount: 1,
	})
	require.NoError(t, err)
	assert.True(t, service.IsMockChainTx(result.TxHash), "假哈希必须一眼看得出是假的")
}

func TestResolveFallsBackToDisabledWhenTheLiveClientCannotBeBuilt(t *testing.T) {
	// 一个打款配置的笔误不该让整个进程起不来——所有用户都登不上，
	// 代价远大于提现暂时走不通。返回 Disabled 加错误，让调用方记告警继续。
	resolved, err := Resolve(context.Background(), Config{
		Enabled: true, RPCURL: "https://x.example/",
		SignerKey: "not-a-key", TokenAddress: addrBSCUSDT,
		ChainID: 56, Confirmations: 3, FeeMultiplier: 1, FallbackFee: 0.5,
	}, nil)
	require.Error(t, err)
	require.NotNil(t, resolved.Client, "出错也要给一个能用的客户端")
	assert.Equal(t, ModeDisabled, resolved.Mode)
	assert.NotContains(t, err.Error(), "not-a-key")
}

func TestResolveSummaryNeverCarriesTheKey(t *testing.T) {
	// Summary 是拿去打启动日志的，最容易被整行记下来。
	node := newFakeNode().on("eth_chainId", "0x38")
	client := node.start(t, nil) // 顺带确认真客户端这条路能走通

	resolved, err := Resolve(context.Background(), Config{
		Enabled: true, RPCURL: client.cfg.RPCURL, SignerKey: keyBSC,
		TokenAddress: addrBSCUSDT, ChainID: 56, Confirmations: 3,
		FeeMultiplier: 1, FallbackFee: 0.5,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeLive, resolved.Mode)

	assert.NotContains(t, resolved.Summary, keyBSC)
	assert.NotContains(t, resolved.Summary, keyBSC[:16])
	// 金库地址反过来**应该**在里面：它是公开信息，运维靠它盯余额。
	assert.Contains(t, strings.ToLower(resolved.Summary), "0xfcad0b19bb29d4674531d6f115237e16afce377c")
	assert.Contains(t, resolved.Summary, "per-transfer only", "没配批量合约时说清楚")
}

func TestResolveReportsAChainIDMismatchWithoutRefusingToStart(t *testing.T) {
	node := newFakeNode().on("eth_chainId", "0x61") // 测试网
	client := node.start(t, nil)

	resolved, err := Resolve(context.Background(), Config{
		Enabled: true, RPCURL: client.cfg.RPCURL, SignerKey: keyBSC,
		TokenAddress: addrBSCUSDT, ChainID: 56, Confirmations: 3,
		FeeMultiplier: 1, FallbackFee: 0.5,
	}, nil)
	require.Error(t, err)
	assert.Equal(t, ModeLive, resolved.Mode)
	assert.Contains(t, resolved.Summary, "not verified")
}
