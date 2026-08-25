// APEXONE-EXT: 双边市场——按配置挑一个链上打款客户端。
//
// # 三种状态，默认落在最安全的那一个
//
//	没启用        → DisabledChainClient：每一个会动钱的方法都拒绝
//	启用 + 显式 mock → MockChainClient：假装成功，只给联调用
//	启用          → Client：真广播
//
// "没配好"是最常见的运维状态（新环境、密钥没注入、RPC 写错），所以它必须落在
// **拒绝**那一边。如果默认是假装成功，表现就是工单被安静地标成已付、供给者的
// 余额被清零，而链上什么都没发生——而且没有任何一条错误日志。
//
// # PAYOUT_MOCK 单独设置不生效
//
// 必须 PAYOUT_ENABLED 和 PAYOUT_MOCK 同时为真才会拿到假客户端。一个环境变量
// 被误抄进生产配置是会发生的事；要两个同时抄错才会让生产环境假装打款。
// 反过来，只设 PAYOUT_MOCK 的那种误配落到 Disabled——拒绝，看得见。
package payoutchain

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
)

// Mode 说明当前用的是哪一种客户端。给启动日志和管理端自检用。
type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeMock     Mode = "mock"
	ModeLive     Mode = "live"
)

// Resolved 是工厂的产出：客户端本体，加上一句人能读的说明。
type Resolved struct {
	Client service.SupplierChainClient
	Mode   Mode
	// Summary 一句话描述当前状态，适合直接打进启动日志。
	//
	// 里面**没有**私钥，只有从私钥推出来的金库地址——那是公开信息，
	// 每一笔链上交易里都写着它。
	Summary string
}

// Resolve 按配置造出该用的客户端，并向节点确认一次链 ID。
//
// 永远返回一个可用的客户端：配置有问题时返回 Disabled 加上错误，调用方可以
// 选择记一条告警继续启动（提现走不通，但服务的其它部分照常）。这比让整个进程
// 起不来好——一个打款配置的笔误不该让所有用户都登不上。
func Resolve(ctx context.Context, cfg Config, httpClient *http.Client) (Resolved, error) {
	resolved, err := build(cfg, httpClient)
	if err != nil {
		return resolved, err
	}
	client, live := resolved.Client.(*Client)
	if !live {
		return resolved, nil
	}
	// 起服务时就确认节点在我们以为的那条链上。查不通不算致命——节点可能只是
	// 一时不可达，而打款是异步的，worker 下一轮还会再试。
	if err := client.VerifyChain(ctx); err != nil {
		resolved.Summary = fmt.Sprintf("on-chain payout is LIVE on chain %d from treasury %s (chain id not verified: %v)",
			cfg.ChainID, client.TreasuryAddress(), err)
		return resolved, err
	}
	return resolved, nil
}

// build 挑客户端，**不做任何网络调用**。
//
// 拆出来是因为它有两个调用方，而它们要的东西不同：DI（ProvideChainClient）
// 只要一个能用的客户端，启动自检（Resolve）还要额外问一次链 ID。
// 两边共用这一个函数，是为了让「这套部署是 disabled / mock / live 中的哪一种」
// 只有一处判断——否则启动日志说 LIVE、而注入进服务的那个是 Disabled，
// 这种不一致查起来会要命，因为日志本身就是排查的起点。
func build(cfg Config, httpClient *http.Client) (Resolved, error) {
	disabled := Resolved{
		Client:  service.NewDisabledChainClient(cfg.FallbackFee),
		Mode:    ModeDisabled,
		Summary: fmt.Sprintf("on-chain payout is off (%s is not set); withdrawals will not be broadcast", envEnabled),
	}

	if !cfg.Enabled {
		return disabled, nil
	}
	if cfg.Mock {
		return Resolved{
			Client:  service.NewMockChainClient(service.MockChainOptions{Fee: cfg.FallbackFee}),
			Mode:    ModeMock,
			Summary: fmt.Sprintf("on-chain payout is MOCKED (%s is on): nothing will be broadcast", envMock),
		}, nil
	}

	client, err := New(cfg, httpClient)
	if err != nil {
		return disabled, err
	}

	batch := "per-transfer only"
	if client.SupportsBatch(service.SupplierPayoutNetworkBSC) {
		batch = "batch via " + formatAddress(client.disperse)
	}
	return Resolved{
		Client: client,
		Mode:   ModeLive,
		Summary: fmt.Sprintf("on-chain payout is LIVE on chain %d from treasury %s (%s)",
			cfg.ChainID, client.TreasuryAddress(), batch),
	}, nil
}

// ProvideChainManager 是 wire 的入口（M6 起）：造管理器并做第一次装配。
//
// # 为什么它不返回错误
//
// wire 的 provider 一报错，整个进程就起不来。而这里能出的错全都是"打款配好没"
// 这一类的——控制台没配、钥匙解不开、RPC 写错——它们不该让所有用户都登不上。
// 首次 Reload 失败时 Holder 里留着 Disabled（拒绝一切），错误进启动日志与
// Manager.Status，管理端的自检面板能看见。
//
// # 首次 Reload 里那次 VerifyChain
//
// M2 时代 provider 刻意不触网、把核链留给 main 的自检。M6 的 Reload 自带
// 8 秒超时的核链且失败不回滚，性质与当年的自检一致——于是自检并进了这里，
// main 不再单独跑一份（两处各核一次、各打一条日志，迟早有一条骗人）。
func ProvideChainManager(settingService *service.SettingService, encryptor service.SecretEncryptor) *Manager {
	manager := NewManager(settingService, encryptor, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if status, err := manager.Reload(ctx); err != nil {
		slog.Warn("[PayoutChain] initial load left payouts disabled", "error", err, "source", status.Source)
	}
	return manager
}

// ProvideChainClientFromManager 把转发器交给业务侧。
//
// 只导出 SupplierChainClient 这一个面，不让 *Manager 进业务代码的依赖：
// Mode/Status 是给管理端与日志的，业务里出现"如果是 mock 就……"的分支
// 正是这个包最不想要的东西。管理端 handler 要 *Manager 时单独注入。
func ProvideChainClientFromManager(manager *Manager) service.SupplierChainClient {
	return manager.Client()
}

// ProviderSet 供 cmd/server 的 wire 装配。
var ProviderSet = wire.NewSet(ProvideChainManager, ProvideChainClientFromManager)
