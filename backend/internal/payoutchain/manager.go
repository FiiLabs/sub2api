// APEXONE-EXT: 双边市场——链上客户端的运行期热更换（M6）。
//
// M2–M5 里客户端在 wire 装配时定型，改配置要重启。M6 把配置挪进控制台之后，
// 「保存」必须当场生效——于是消费者（提现服务、打款 worker）拿到的不再是
// 某一个客户端，而是一个**转发器**（Holder）：它实现 SupplierChainClient，
// 把每一次调用转给当前客户端；Manager 负责在配置变化时原子地换掉里面那个。
//
// # 热换为什么是安全的
//
// 换客户端的瞬间可能有一张单子正走在 worker 的五步状态机上。三道既有护栏
// 恰好都是为「配置在中途变了」准备的：
//   - nonce 已钉的单子重播时用的还是钉住的号（换 RPC/换钥匙不会让它换号）；
//   - checkToken 拒绝「单子钉的币 ≠ 新金库配的币」（§9.9），发错币在广播前被拦；
//   - 换成 Disabled 后，下一步链上调用收到明确拒绝 → 单子带着 last_error
//     留在队里，配置修好自动续走。
//
// 也就是说热换最坏的结果是几张单子多退避一轮，不存在钱走错的路径。
//
// # 配置的两个来源与它们的优先级
//
//	settings 存过    → settings 是唯一事实（env 里除 PAYOUT_MOCK 外全部失效）
//	settings 没存过  → 回落到环境变量（存量部署的迁移期）
//	PAYOUT_ENABLED + PAYOUT_MOCK 同时为真 → mock 压过一切（联调环境专用，
//	  刻意**不进**控制台：一个能在生产界面上点出来的"假装打款"开关，
//	  早晚会在生产被点出来）
package payoutchain

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Holder 是消费者持有的转发器。零值不可用，用 NewHolder。
type Holder struct {
	current atomic.Pointer[clientBox]
}

// clientBox 包一层：atomic.Pointer 需要一个具体类型，而客户端是接口。
type clientBox struct {
	client service.SupplierChainClient
}

// NewHolder 造一个初始为「拒绝一切」的转发器。
//
// 初始值不是 nil 而是 Disabled：Manager 第一次 Reload 之前（进程刚起、数据库
// 还没读到）到达的调用必须收到明确拒绝，而不是空指针崩溃。
func NewHolder() *Holder {
	h := &Holder{}
	h.current.Store(&clientBox{client: service.NewDisabledChainClient()})
	return h
}

func (h *Holder) get() service.SupplierChainClient {
	return h.current.Load().client
}

func (h *Holder) swap(client service.SupplierChainClient) {
	h.current.Store(&clientBox{client: client})
}

// SupplierChainClient 的全部方法：纯转发，不加任何逻辑——加了就意味着
// 有一层行为在真客户端的测试覆盖之外。
func (h *Holder) TokenAddress(network, symbol string) (string, bool) {
	return h.get().TokenAddress(network, symbol)
}
func (h *Holder) NextNonce(ctx context.Context, network string) (uint64, error) {
	return h.get().NextNonce(ctx, network)
}
func (h *Holder) Transfer(ctx context.Context, params service.ChainTransferParams) (service.ChainTransferResult, error) {
	return h.get().Transfer(ctx, params)
}
func (h *Holder) SupportsBatch(network string) bool {
	return h.get().SupportsBatch(network)
}
func (h *Holder) EnsureBatchAllowance(ctx context.Context, params service.ChainBatchParams) (*service.ChainAllowanceTopUp, error) {
	return h.get().EnsureBatchAllowance(ctx, params)
}
func (h *Holder) TransferBatch(ctx context.Context, params service.ChainBatchParams) (service.ChainTransferResult, error) {
	return h.get().TransferBatch(ctx, params)
}
func (h *Holder) WaitForConfirmation(ctx context.Context, network, txHash string) (service.ChainConfirmation, error) {
	return h.get().WaitForConfirmation(ctx, network, txHash)
}

// Status 是 Manager 最近一次装配的结果，给管理端与启动日志看。
type Status struct {
	Mode Mode `json:"mode"`
	// Summary 一句话说明。里面没有私钥（见 Resolved.Summary）。
	Summary string `json:"summary"`
	// Treasury 金库地址（live 时非空）。公开信息，每笔交易里都写着它。
	Treasury string `json:"treasury"`
	// ChainVerified 向节点核对链 ID 的结果：nil = 没核（disabled/mock），
	// true/false = 核了。false 不阻止装配——节点可能只是一时不可达。
	ChainVerified *bool `json:"chain_verified,omitempty"`
	// Error 装配或核链的错误文本（人读）。
	Error string `json:"error,omitempty"`
	// Source 配置来自哪：console / env / mock-env。
	Source string `json:"source"`
	// AppliedAt 这份状态生效的时刻。
	AppliedAt time.Time `json:"applied_at"`
}

// SettingsSource 是 Manager 对配置系统的全部依赖面。
// 定义在这边而不是直接吃 *SettingService，是为了测试能用一个桩喂配置。
type SettingsSource interface {
	GetSupplyPayoutChainSettings(ctx context.Context) (*service.SupplyPayoutChainSettings, bool)
	SupplyPayoutChainSignerCiphertext(ctx context.Context) string
}

// Manager 负责按当前配置装配客户端并热换进 Holder。
type Manager struct {
	settings   SettingsSource
	encryptor  service.SecretEncryptor
	httpClient *http.Client
	holder     *Holder

	// mu 让 Reload 串行：两次并发保存各自装配、乱序 swap 的话，
	// 最后生效的可能是先保存的那份。
	mu     sync.Mutex
	status atomic.Pointer[Status]
}

// NewManager 造管理器。此刻不读配置、不触网——第一次 Reload 才会。
func NewManager(settings SettingsSource, encryptor service.SecretEncryptor, httpClient *http.Client) *Manager {
	m := &Manager{
		settings:   settings,
		encryptor:  encryptor,
		httpClient: httpClient,
		holder:     NewHolder(),
	}
	m.status.Store(&Status{
		Mode:      ModeDisabled,
		Summary:   "on-chain payout has not been configured yet",
		Source:    "none",
		AppliedAt: time.Now(),
	})
	return m
}

// Client 消费者要持有的那个转发器。整个进程只有这一个。
func (m *Manager) Client() service.SupplierChainClient { return m.holder }

// Status 最近一次装配的结果。
func (m *Manager) Status() Status { return *m.status.Load() }

// Reload 按当前配置重新装配并热换。返回装配结果；错误 = 这次没换成
// （旧客户端原样保留——一次数据库抖动或一把解不开的钥匙不该把 LIVE 降级）。
func (m *Manager) Reload(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, source, err := m.assembleConfig(ctx)
	if err != nil {
		status := Status{
			Mode:      m.Status().Mode,
			Summary:   m.Status().Summary,
			Source:    source,
			Error:     err.Error(),
			AppliedAt: time.Now(),
		}
		// 刻意**不** swap：读不出配置时保持现状，只把错误挂出来。
		m.status.Store(&status)
		return status, err
	}

	resolved, err := build(cfg, m.httpClient)
	if err != nil {
		// build 失败时 resolved 是 Disabled——这一步**要** swap：配置读出来了
		// 但造不出客户端（钥匙坏了、地址坏了），继续用旧客户端等于按一份
		// 已经被替掉的配置打款。
		m.holder.swap(resolved.Client)
		status := Status{
			Mode: resolved.Mode, Summary: resolved.Summary,
			Source: source, Error: err.Error(), AppliedAt: time.Now(),
		}
		m.status.Store(&status)
		return status, err
	}

	m.holder.swap(resolved.Client)
	status := Status{
		Mode: resolved.Mode, Summary: resolved.Summary,
		Source: source, AppliedAt: time.Now(),
	}
	if live, ok := resolved.Client.(*Client); ok {
		status.Treasury = live.TreasuryAddress()
		verifyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		verified := live.VerifyChain(verifyCtx) == nil
		cancel()
		status.ChainVerified = &verified
		if !verified {
			// 核不上不回滚：worker 的每一步都会拿到明确错误并退避，
			// 而这里的状态让管理端能看见"配置存了、链没对上"。
			status.Error = "chain id could not be verified against the node; check rpc_url/chain_id"
		}
	}
	m.status.Store(&status)
	slog.Info("[PayoutChain] client reloaded", "mode", status.Mode, "source", source, "summary", status.Summary)
	return status, nil
}

// assembleConfig 决定这次装配用哪份配置（见文件头的优先级）。
func (m *Manager) assembleConfig(ctx context.Context) (Config, string, error) {
	// mock 压过一切，且只能从 env 来。
	if envBool(envEnabled) && envBool(envMock) {
		cfg, err := LoadConfig()
		if err != nil {
			return Config{}, "mock-env", err
		}
		return cfg, "mock-env", nil
	}

	stored, hasStored := m.settings.GetSupplyPayoutChainSettings(ctx)
	if !hasStored {
		// settings 没存过：回落环境变量（存量部署）。LoadConfig 自带校验。
		cfg, err := LoadConfig()
		if err != nil {
			return Config{}, "env", err
		}
		return cfg, "env", nil
	}

	cfg := Config{
		Enabled:         stored.Enabled,
		RPCURL:          stored.RPCURL,
		TokenAddress:    stored.TokenAddress,
		TokenSymbol:     stored.TokenSymbol,
		DisperseAddress: stored.DisperseAddress,
		ChainID:         stored.ChainID,
		Confirmations:   stored.Confirmations,
	}
	if stored.Enabled {
		sealed := m.settings.SupplyPayoutChainSignerCiphertext(ctx)
		if sealed == "" {
			return Config{}, "console", fmt.Errorf("payoutchain: signer key is missing from stored settings")
		}
		plain, err := service.OpenSupplyPayoutSignerKey(m.encryptor, sealed)
		if err != nil {
			return Config{}, "console", err
		}
		cfg.SignerKey = plain
	}
	if err := cfg.validate(); err != nil {
		return Config{}, "console", err
	}
	return cfg, "console", nil
}
