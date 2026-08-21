// APEXONE-EXT: 双边市场——链上收款地址绑定的应用服务。
//
// 这一层薄得几乎只剩转发，因为真正的两件事都不在这里：
//   - 地址长什么样才算对，在 supplier_payout_wallet.go（领域校验）；
//   - 一个地址只能属于一个人，在数据库的唯一索引上（迁移 234）。
//
// 它存在的意义是那两件**不属于**上面任何一层的事：
//   1. 绑定必须是登录用户给自己绑。userID 一律由调用方从会话里取，
//      任何一个从请求体读 user_id 的写法都等于允许替别人改收款地址。
//   2. 「有哪些链」这个问题只有一个权威答案（supplierOnchainChannels），
//      前端要靠它渲染表单，所以由这一层原样吐出去，而不是让前端自己写死一份。
package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SupplierPayoutWalletService 是链上收款地址的应用服务。
type SupplierPayoutWalletService struct {
	repo SupplierPayoutWalletRepository
}

// NewSupplierPayoutWalletService 构造绑定服务。
func NewSupplierPayoutWalletService(repo SupplierPayoutWalletRepository) *SupplierPayoutWalletService {
	return &SupplierPayoutWalletService{repo: repo}
}

func (s *SupplierPayoutWalletService) ready() bool {
	return s != nil && s.repo != nil
}

func (s *SupplierPayoutWalletService) unavailable() error {
	return ErrSupplierPayoutWalletUnavailable
}

// SupplierPayoutWalletOptions 是绑定表单需要的一切。
//
// 一次返回「支持哪些链」和「你已经绑了什么」，而不是让前端拼两个接口：
// 两者来自不同时刻时会画出「链列表里没有 bsc、下面却显示着一个 bsc 地址」
// 这种自相矛盾的界面。
type SupplierPayoutWalletOptions struct {
	// Channels 会自动上链结算的渠道（渠道名 → 链 + 币）。
	//
	// 注意它回答的是「**如果**选了这个渠道会怎么结算」，不是「现在能不能选」——
	// 能不能选由管理员的提现渠道白名单决定，那个数在提现 options 接口里。
	// 两件事分开，是为了让「先把代码放上去、之后再打开」这条上线路径成立。
	Channels []SupplierOnchainChannel `json:"channels"`
	// Wallets 本人已绑定的地址。没绑过是空数组，不是 null——
	// 前端对 null 和 [] 的处理迟早会有一处漏掉。
	Wallets []SupplierPayoutWallet `json:"wallets"`
}

// GetOptions 读绑定表单需要的全部信息。
func (s *SupplierPayoutWalletService) GetOptions(ctx context.Context, userID int64) (*SupplierPayoutWalletOptions, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	wallets, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if wallets == nil {
		wallets = []SupplierPayoutWallet{}
	}
	return &SupplierPayoutWalletOptions{
		Channels: SupplierOnchainChannels(),
		Wallets:  wallets,
	}, nil
}

// Get 读某条链上的绑定。没绑返回 (nil, nil)。
func (s *SupplierPayoutWalletService) Get(ctx context.Context, userID int64, network string) (*SupplierPayoutWallet, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if !IsSupplierPayoutNetwork(network) {
		return nil, ErrSupplierPayoutNetworkInvalid
	}
	return s.repo.Get(ctx, userID, network)
}

// Bind 绑定或换绑一个收款地址。
//
// 校验在仓储里做（那里是唯一一个「写进去就算数」的地方），这里只挡住
// 「不是给自己绑」和「链不认识」两件在碰库之前就该有答案的事。
func (s *SupplierPayoutWalletService) Bind(ctx context.Context, userID int64, network, address string) (*SupplierPayoutWallet, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if !IsSupplierPayoutNetwork(network) {
		return nil, ErrSupplierPayoutNetworkInvalid
	}
	return s.repo.Upsert(ctx, userID, network, address)
}

// Unbind 解绑。
//
// 刻意**不**检查「有没有在途的提现单」：在途单的收款地址是建单那一刻的快照
// （payout_account），解绑不会改道任何一笔已经在路上的钱。反过来若在这里加一道
// 「有单不让解绑」，一张卡住的单子就能把人的收款地址锁死。
func (s *SupplierPayoutWalletService) Unbind(ctx context.Context, userID int64, network string) error {
	if !s.ready() {
		return s.unavailable()
	}
	if userID <= 0 {
		return infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	if !IsSupplierPayoutNetwork(network) {
		return ErrSupplierPayoutNetworkInvalid
	}
	return s.repo.Delete(ctx, userID, network)
}

// ResolvePayoutAddress 供提现建单时取「这个渠道该打到哪个地址」。
//
// 返回空串 + false 表示这不是链上渠道——调用方照旧走人工路径，用用户手填的账号。
// 是链上渠道但没绑地址时返回 ErrSupplierPayoutWalletNotFound：**必须失败关闭**。
// 放行的话，供给者手填的一串未经校验的字符会被当成链上地址落进单子，
// 而那正是这整套绑定机制要消灭的东西。
func (s *SupplierPayoutWalletService) ResolvePayoutAddress(ctx context.Context, userID int64, channel string) (SupplierOnchainChannel, string, bool, error) {
	onchain, ok := LookupSupplierOnchainChannel(channel)
	if !ok {
		return SupplierOnchainChannel{}, "", false, nil
	}
	if !s.ready() {
		return onchain, "", true, s.unavailable()
	}
	wallet, err := s.repo.Get(ctx, userID, onchain.Network)
	if err != nil {
		return onchain, "", true, err
	}
	if wallet == nil {
		return onchain, "", true, ErrSupplierPayoutWalletNotFound
	}
	return onchain, wallet.Address, true, nil
}
