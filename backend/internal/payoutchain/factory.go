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
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
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

// Resolve 按配置造出该用的客户端。
//
// 永远返回一个可用的客户端：配置有问题时返回 Disabled 加上错误，调用方可以
// 选择记一条告警继续启动（提现走不通，但服务的其它部分照常）。这比让整个进程
// 起不来好——一个打款配置的笔误不该让所有用户都登不上。
func Resolve(ctx context.Context, cfg Config, httpClient *http.Client) (Resolved, error) {
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
	// 起服务时就确认节点在我们以为的那条链上。查不通不算致命——节点可能只是
	// 一时不可达，而打款是异步的，worker 下一轮还会再试。
	if err := client.VerifyChain(ctx); err != nil {
		return Resolved{
			Client: client,
			Mode:   ModeLive,
			Summary: fmt.Sprintf("on-chain payout is LIVE on chain %d from treasury %s (chain id not verified: %v)",
				cfg.ChainID, client.TreasuryAddress(), err),
		}, err
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
