//go:build unit

// APEXONE-EXT: 双边市场——客户端热更换（M6）的单元测试。
//
// 这个文件盯三件事：配置来源的优先级（console > env，mock-env 压过一切）、
// Reload 的换与不换（读不出配置**不换**，造不出客户端**换成 Disabled**——
// 两者的方向相反，各自防一种事故）、以及 Holder 的转发是纯的。
package payoutchain

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// managerSettingsStub 喂配置的桩。
type managerSettingsStub struct {
	settings *service.SupplyPayoutChainSettings
	stored   bool
	sealed   string
}

func (s *managerSettingsStub) GetSupplyPayoutChainSettings(context.Context) (*service.SupplyPayoutChainSettings, bool) {
	if s.settings == nil {
		return service.DefaultSupplyPayoutChainSettings(), s.stored
	}
	clone := *s.settings
	clone.SignerKey = "" // 与真实实现同一条规矩：读路径抹掉私钥
	return &clone, s.stored
}

func (s *managerSettingsStub) SupplyPayoutChainSignerCiphertext(context.Context) string {
	return s.sealed
}

// plainEncryptor 不加密的加密器：manager 只关心「能不能解开」，
// 密码学本身由 AESEncryptor 自己的测试负责。
type plainEncryptor struct{ fail bool }

func (e *plainEncryptor) Encrypt(plaintext string) (string, error) { return plaintext, nil }
func (e *plainEncryptor) Decrypt(ciphertext string) (string, error) {
	if e.fail {
		return "", assert.AnError
	}
	return ciphertext, nil
}

// keyBSC 复用 tx_test.go 的测试私钥常量（同包）。

func consoleSettings() *service.SupplyPayoutChainSettings {
	return &service.SupplyPayoutChainSettings{
		Enabled:       true,
		RPCURL:        "http://127.0.0.1:1", // 连不上没关系——Reload 的核链失败不回滚
		SignerKey:     "enc.v1:" + keyBSC,
		TokenAddress:  addrBSCUSDT,
		TokenSymbol:   "USDT",
		ChainID:       56,
		Confirmations: 3,
		FallbackFee:   0.5,
		FeeMultiplier: 1.5,
	}
}

func managerOn(stub *managerSettingsStub) *Manager {
	return NewManager(stub, &plainEncryptor{}, nil)
}

// 第一次 Reload 之前，Holder 里必须是「拒绝一切」——进程刚起、配置还没读到
// 的窗口里到达的调用不能假装成功，更不能空指针。
func TestManagerStartsDisabled(t *testing.T) {
	manager := managerOn(&managerSettingsStub{})
	_, err := manager.Client().Transfer(context.Background(), service.ChainTransferParams{})
	assert.ErrorIs(t, err, service.ErrSupplierPayoutChainDisabled)
	assert.Equal(t, ModeDisabled, manager.Status().Mode)
}

// settings 存过 → console 是唯一事实：装配出 live 客户端，金库地址来自解开的私钥。
func TestManagerAssemblesLiveFromConsoleSettings(t *testing.T) {
	stub := &managerSettingsStub{settings: consoleSettings(), stored: true, sealed: "enc.v1:" + keyBSC}
	manager := managerOn(stub)

	status, err := manager.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ModeLive, status.Mode)
	assert.Equal(t, "console", status.Source)
	// 地址从私钥推导；出现在状态里的**只有**地址（公开信息），没有钥匙。
	assert.Equal(t, "0xfcad0b19bb29d4674531d6f115237e16afce377c", status.Treasury)
	assert.NotContains(t, status.Summary, keyBSC[2:10], "私钥片段漏进了状态摘要")
	// RPC 连不上 → 核链失败但**不回滚**：客户端已换上，worker 的每一步
	// 会拿到明确错误并退避，而状态里说得清"链没核上"。
	require.NotNil(t, status.ChainVerified)
	assert.False(t, *status.ChainVerified)

	_, transferErr := manager.Client().NextNonce(context.Background(), service.SupplierPayoutNetworkBSC)
	assert.NotErrorIs(t, transferErr, service.ErrSupplierPayoutChainDisabled, "live 客户端没被换进 Holder")
}

// settings 存过但关着 → Disabled，env 里配了什么都不看。
func TestManagerConsoleSettingsWinOverEnv(t *testing.T) {
	t.Setenv("PAYOUT_ENABLED", "true")
	t.Setenv("PAYOUT_BSC_RPC_URL", "http://from-env")
	t.Setenv("PAYOUT_BSC_SIGNER_KEY", keyBSC)
	t.Setenv("PAYOUT_BSC_TOKEN_ADDRESS", addrBSCUSDT)

	disabled := service.DefaultSupplyPayoutChainSettings()
	manager := managerOn(&managerSettingsStub{settings: disabled, stored: true})

	status, err := manager.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ModeDisabled, status.Mode)
	assert.Equal(t, "console", status.Source,
		"settings 存过之后 env 还在生效——配置的答案有了两个来源")
}

// settings 没存过 → 回落 env（存量部署的迁移期）。
func TestManagerFallsBackToEnvWhenNothingStored(t *testing.T) {
	t.Setenv("PAYOUT_ENABLED", "true")
	t.Setenv("PAYOUT_BSC_RPC_URL", "http://127.0.0.1:1")
	t.Setenv("PAYOUT_BSC_SIGNER_KEY", keyBSC)
	t.Setenv("PAYOUT_BSC_TOKEN_ADDRESS", addrBSCUSDT)

	manager := managerOn(&managerSettingsStub{stored: false})
	status, err := manager.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ModeLive, status.Mode)
	assert.Equal(t, "env", status.Source)
}

// PAYOUT_ENABLED + PAYOUT_MOCK 同时为真 → mock 压过 console。
// 它只能从 env 来：一个能在生产界面上点出来的"假装打款"开关，早晚会被点出来。
func TestManagerEnvMockOverridesEverything(t *testing.T) {
	t.Setenv("PAYOUT_ENABLED", "true")
	t.Setenv("PAYOUT_MOCK", "true")

	manager := managerOn(&managerSettingsStub{settings: consoleSettings(), stored: true, sealed: "enc.v1:" + keyBSC})
	status, err := manager.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ModeMock, status.Mode)
	assert.Equal(t, "mock-env", status.Source)
}

// 只设 PAYOUT_MOCK（没有 PAYOUT_ENABLED）→ mock **不**生效，console 照常。
//
// 双开关是刻意的：一个环境变量被误抄进生产配置是会发生的事，
// 要两个同时抄错才会让生产环境假装打款（§9.5 的老规矩，热换后仍然成立）。
func TestManagerIgnoresMockWithoutEnabled(t *testing.T) {
	t.Setenv("PAYOUT_MOCK", "true")

	stub := &managerSettingsStub{settings: consoleSettings(), stored: true, sealed: "enc.v1:" + keyBSC}
	status, err := managerOn(stub).Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ModeLive, status.Mode, "单独一个 PAYOUT_MOCK 就把生产切成了假装打款")
	assert.Equal(t, "console", status.Source)
}

// 钥匙解不开 → **不换**客户端，错误挂进状态。
//
// 这一格与「造不出客户端」的方向相反：解不开几乎总是暂时的（加密器没配好、
// 一次读库抖动），把一个正在打款的 LIVE 降级成 Disabled 才是事故。
func TestManagerKeepsCurrentClientWhenKeyWontOpen(t *testing.T) {
	stub := &managerSettingsStub{settings: consoleSettings(), stored: true, sealed: "enc.v1:" + keyBSC}
	manager := managerOn(stub)
	_, err := manager.Reload(context.Background())
	require.NoError(t, err)
	require.Equal(t, ModeLive, manager.Status().Mode)

	manager.encryptor = &plainEncryptor{fail: true}
	_, err = manager.Reload(context.Background())
	require.Error(t, err)

	// 客户端还是 live 的那个；状态里挂着错误。
	_, nonceErr := manager.Client().NextNonce(context.Background(), service.SupplierPayoutNetworkBSC)
	assert.NotErrorIs(t, nonceErr, service.ErrSupplierPayoutChainDisabled,
		"一把暂时解不开的钥匙把正在打款的 LIVE 降级成了 Disabled")
	assert.NotEmpty(t, manager.Status().Error)
}

// 配置读出来了但造不出客户端（坏钥匙内容）→ **换成 Disabled**。
//
// 与上一条方向相反：这里配置是新的、确定的，继续用旧客户端等于按一份
// 已经被替掉的配置打款。
func TestManagerSwapsToDisabledWhenBuildFails(t *testing.T) {
	stub := &managerSettingsStub{settings: consoleSettings(), stored: true, sealed: "enc.v1:" + keyBSC}
	manager := managerOn(stub)
	_, err := manager.Reload(context.Background())
	require.NoError(t, err)

	// 换上一把形状对、内容坏的钥匙（全零私钥会被签名器拒绝）。
	stub.sealed = "enc.v1:0x" + string(make([]byte, 0)) + "0000000000000000000000000000000000000000000000000000000000000000"
	_, err = manager.Reload(context.Background())
	require.Error(t, err)

	_, nonceErr := manager.Client().Transfer(context.Background(), service.ChainTransferParams{})
	assert.ErrorIs(t, nonceErr, service.ErrSupplierPayoutChainDisabled,
		"新配置造不出客户端，旧客户端却还在按被替掉的配置打款")
}
