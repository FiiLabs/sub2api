//go:build unit

// APEXONE-EXT: 双边市场——绑定服务 + 提现建单取地址的单元测试。
//
// 绑定服务本身很薄，所以它那几条用例也薄：只钉「userID 从会话来、链要认识、
// 没配仓储不能假装成功」。
//
// 重量在后半个文件：提现建单时收款账号从哪来。那里有一条真正会让钱丢掉的路径——
// 链上渠道回落到用户手填的地址。手填的地址没经过绑定时那三道校验、也没经过
// 反女巫唯一索引，一旦落进单子，整套绑定机制就等于没有。因此凡是"拿不到绑定
// 地址"的情形，一律必须**失败关闭**，而不是退回手填值。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierPayoutWalletRepoStub 记下收到的参数并按预设作答。
type supplierPayoutWalletRepoStub struct {
	wallet *SupplierPayoutWallet
	list   []SupplierPayoutWallet

	getErr    error
	upsertErr error
	deleteErr error

	upsertUserID  int64
	upsertNetwork string
	upsertAddress string
	deleteCalls   int
}

func (s *supplierPayoutWalletRepoStub) Get(_ context.Context, _ int64, _ string) (*SupplierPayoutWallet, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.wallet, nil
}

func (s *supplierPayoutWalletRepoStub) List(_ context.Context, _ int64) ([]SupplierPayoutWallet, error) {
	return s.list, nil
}

func (s *supplierPayoutWalletRepoStub) Upsert(_ context.Context, userID int64, network, address string) (*SupplierPayoutWallet, error) {
	s.upsertUserID, s.upsertNetwork, s.upsertAddress = userID, network, address
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return &SupplierPayoutWallet{ID: 1, UserID: userID, Network: network, Address: address}, nil
}

func (s *supplierPayoutWalletRepoStub) Delete(_ context.Context, _ int64, _ string) error {
	s.deleteCalls++
	return s.deleteErr
}

// ============================================================================
// 绑定服务
// ============================================================================

// 没装配仓储时每个方法都必须报「服务不可用」，不能返回一个看起来正常的空结果。
//
// 空结果在这里格外危险：GetOptions 返回空钱包列表，前端会画出"你还没绑地址"，
// 而用户其实绑过——他会再绑一次，然后在换绑上撞见一个莫名其妙的冲突。
func TestPayoutWalletServiceUnavailableWithoutRepo(t *testing.T) {
	for name, svc := range map[string]*SupplierPayoutWalletService{
		"nil":  nil,
		"没装配": {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.GetOptions(context.Background(), 7)
			assert.Error(t, err)
			_, err = svc.Get(context.Background(), 7, SupplierPayoutNetworkBSC)
			assert.Error(t, err)
			_, err = svc.Bind(context.Background(), 7, SupplierPayoutNetworkBSC, "0xde709f2102306220921060314715629080e2fb77")
			assert.Error(t, err)
			assert.Error(t, svc.Unbind(context.Background(), 7, SupplierPayoutNetworkBSC))
		})
	}
}

// userID 必须是会话里的那个人。<=0 一律拒绝，绝不落到仓储上。
//
// 仓储的每一条 SQL 都以 user_id 为条件，一个 0 会安静地匹配不到任何行——
// 于是"未登录"表现成"你还没绑地址"，而绑定会写出一行 user_id=0 的孤儿记录，
// 那一行还会占住一个地址的反女巫名额。
func TestPayoutWalletServiceRejectsAnonymousUser(t *testing.T) {
	repo := &supplierPayoutWalletRepoStub{}
	svc := NewSupplierPayoutWalletService(repo)

	for _, userID := range []int64{0, -1} {
		_, err := svc.GetOptions(context.Background(), userID)
		assert.Error(t, err)
		_, err = svc.Bind(context.Background(), userID, SupplierPayoutNetworkBSC,
			"0xde709f2102306220921060314715629080e2fb77")
		assert.Error(t, err)
		assert.Error(t, svc.Unbind(context.Background(), userID, SupplierPayoutNetworkBSC))
	}
	assert.Zero(t, repo.upsertUserID, "匿名请求走到了仓储")
	assert.Zero(t, repo.deleteCalls, "匿名请求走到了仓储")
}

// 不认识的链在碰库之前就被挡下。
func TestPayoutWalletServiceRejectsUnknownNetwork(t *testing.T) {
	repo := &supplierPayoutWalletRepoStub{}
	svc := NewSupplierPayoutWalletService(repo)

	_, err := svc.Bind(context.Background(), 7, "eth", "0xde709f2102306220921060314715629080e2fb77")
	assert.ErrorIs(t, err, ErrSupplierPayoutNetworkInvalid)
	assert.ErrorIs(t, svc.Unbind(context.Background(), 7, "tron"), ErrSupplierPayoutNetworkInvalid)
	_, err = svc.Get(context.Background(), 7, "")
	assert.ErrorIs(t, err, ErrSupplierPayoutNetworkInvalid)

	assert.Zero(t, repo.upsertNetwork, "未知链走到了仓储")
	assert.Zero(t, repo.deleteCalls, "未知链走到了仓储")
}

// GetOptions 把注册表和本人的绑定一起给出去，且没绑过时是空数组不是 null。
func TestPayoutWalletOptionsCarriesRegistryAndBindings(t *testing.T) {
	repo := &supplierPayoutWalletRepoStub{}
	svc := NewSupplierPayoutWalletService(repo)

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	require.NotNil(t, options.Wallets, "null 会让前端多一条要判的分支，迟早漏一处")
	assert.Empty(t, options.Wallets)
	require.NotEmpty(t, options.Channels)
	assert.Equal(t, SupplierPayoutNetworkBSC, options.Channels[0].Network)

	repo.list = []SupplierPayoutWallet{{ID: 3, UserID: 7, Network: SupplierPayoutNetworkBSC, Address: "0xabc"}}
	options, err = svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, options.Wallets, 1)
	assert.Equal(t, int64(3), options.Wallets[0].ID)
}

// 绑定把 userID 原样往下传，地址不在这一层改。
func TestPayoutWalletBindPassesSessionUser(t *testing.T) {
	repo := &supplierPayoutWalletRepoStub{}
	svc := NewSupplierPayoutWalletService(repo)

	const addr = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	_, err := svc.Bind(context.Background(), 42, SupplierPayoutNetworkBSC, addr)
	require.NoError(t, err)
	assert.Equal(t, int64(42), repo.upsertUserID)
	assert.Equal(t, SupplierPayoutNetworkBSC, repo.upsertNetwork)
	// 归一化是仓储的职责（那里是唯一一个"写进去就算数"的地方）。
	// 在这里也做一次会让两处规则各自演化，而它们必须永远一致。
	assert.Equal(t, addr, repo.upsertAddress, "服务层不该私自改写地址")
}

// ============================================================================
// 提现建单：收款账号从哪来
// ============================================================================

func onchainWithdrawalSettings() *SupplyWithdrawalSettings {
	return &SupplyWithdrawalSettings{
		Enabled:    true,
		MinAmount:  50,
		MaxPending: 2,
		Channels:   []string{"BSC-USDT", "支付宝"},
	}
}

func withdrawalServiceWithWallets(
	repo SupplierWithdrawalRepository,
	walletRepo SupplierPayoutWalletRepository,
) *SupplierWithdrawalService {
	svc := &SupplierWithdrawalService{
		repo:     repo,
		settings: &supplierWithdrawalSettingsStub{settings: onchainWithdrawalSettings()},
	}
	if walletRepo != nil {
		svc.addresses = NewSupplierPayoutWalletService(walletRepo)
	}
	return svc
}

// 链上渠道用绑定的地址，**忽略**手填的那一串。
//
// 这条是整个 M1 的落点：能走到"手填了一个链上地址"这一步的只可能是直接打接口的人，
// 而对他最安全的回答就是"钱打到你自己绑的地址上"。
func TestRequestUsesBoundAddressForOnchainChannel(t *testing.T) {
	const bound = "0xde709f2102306220921060314715629080e2fb77"
	repo := &supplierWithdrawalRepoStub{}
	walletRepo := &supplierPayoutWalletRepoStub{
		wallet: &SupplierPayoutWallet{ID: 1, UserID: 7, Network: SupplierPayoutNetworkBSC, Address: bound},
	}
	svc := withdrawalServiceWithWallets(repo, walletRepo)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount:        100,
		PayoutChannel: "BSC-USDT",
		// 攻击者塞进来的另一个地址。
		PayoutAccount: "0x1111111111111111111111111111111111111111",
	})
	require.NoError(t, err)
	assert.Equal(t, bound, repo.createParams.PayoutAccount,
		"手填的地址被当成了收款地址——整套绑定机制被绕过")
}

// 链上渠道但没绑地址 → 拒绝，且**一行单子都不建**。
//
// 建单会当场从可用区扣钱。放行的话，一笔钱会被扣进一张收款地址是空串的单子里。
func TestRequestRejectsOnchainChannelWithoutBinding(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithWallets(repo, &supplierPayoutWalletRepoStub{wallet: nil})

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "BSC-USDT", PayoutAccount: "随便填的",
	})
	assert.ErrorIs(t, err, ErrSupplierPayoutWalletNotFound)
	assert.Zero(t, repo.calls, "没绑地址却建了单——钱已经扣了")
}

// 读绑定出错时同样失败关闭，不回落到手填值。
//
// 「数据库抖了一下」与「他没绑」在这里必须是同一个结果：任何一条把错误吞掉
// 继续往下走的路径，都会在最不该发生的时刻把一个未校验的地址落进单子。
func TestRequestFailsClosedWhenBindingLookupErrors(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithWallets(repo, &supplierPayoutWalletRepoStub{getErr: errors.New("db down")})

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "BSC-USDT", PayoutAccount: "0x2222222222222222222222222222222222222222",
	})
	require.Error(t, err)
	assert.Zero(t, repo.calls)
}

// 白名单里放了链上渠道，但这套部署没装绑定服务 → 也必须失败关闭。
//
// 这是配置与部署不一致时的那个缝：管理员在设置页把 BSC-USDT 加进白名单，
// 而运行中的二进制没有装配绑定服务。回落到手填地址会让这个缝变成一条静默的
// 出钱通道，而且从设置页上完全看不出来。
func TestRequestFailsClosedWhenWalletServiceMissing(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithWallets(repo, nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "BSC-USDT", PayoutAccount: "0x3333333333333333333333333333333333333333",
	})
	assert.ErrorIs(t, err, ErrSupplierPayoutWalletNotFound)
	assert.Zero(t, repo.calls)
}

// 人工渠道一点没变：仍然用手填的账号，仍然做非空与长度校验。
//
// 这条是那个"两条路径分岔点"的反面。少了它，一次让链上渠道更安全的改动
// 会顺手把支付宝提现也变成"必须先绑一个 BSC 地址"。
func TestRequestKeepsManualChannelBehaviour(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := withdrawalServiceWithWallets(repo, &supplierPayoutWalletRepoStub{wallet: nil})

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝", PayoutAccount: "  zhang@example.com  ",
	})
	require.NoError(t, err, "人工渠道被链上渠道的绑定要求连累了")
	assert.Equal(t, "zhang@example.com", repo.createParams.PayoutAccount)

	// 空账号仍然被拒。
	repo2 := &supplierWithdrawalRepoStub{}
	svc2 := withdrawalServiceWithWallets(repo2, &supplierPayoutWalletRepoStub{})
	_, err = svc2.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝", PayoutAccount: "   ",
	})
	require.Error(t, err)
	assert.Zero(t, repo2.calls)

	// 超长账号仍然被拒。
	repo3 := &supplierWithdrawalRepoStub{}
	svc3 := withdrawalServiceWithWallets(repo3, &supplierPayoutWalletRepoStub{})
	long := make([]rune, SupplierPayoutAccountMaxLen+1)
	for i := range long {
		long[i] = '账'
	}
	_, err = svc3.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝", PayoutAccount: string(long),
	})
	require.Error(t, err)
	assert.Zero(t, repo3.calls)
}

// 渠道白名单仍然排在取地址之前。
//
// 顺序有意义：一个没进白名单的链上渠道必须报"渠道不可用"，而不是先去查绑定、
// 再报"你还没绑地址"——后者会让人去绑一个绑了也用不上的地址。
func TestRequestChecksChannelWhitelistBeforeBinding(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	walletRepo := &supplierPayoutWalletRepoStub{}
	svc := &SupplierWithdrawalService{
		repo: repo,
		settings: &supplierWithdrawalSettingsStub{settings: &SupplyWithdrawalSettings{
			Enabled: true, MaxPending: 2, Channels: []string{"支付宝"},
		}},
		addresses: NewSupplierPayoutWalletService(walletRepo),
	}

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "BSC-USDT", PayoutAccount: "x",
	})
	assert.ErrorIs(t, err, ErrSupplierWithdrawalChannelInvalid)
}

// options 里带上链上渠道注册表，且是副本。
func TestWithdrawalOptionsExposesOnchainChannels(t *testing.T) {
	svc := newWithdrawalService(&supplierWithdrawalRepoStub{}, onchainWithdrawalSettings(), nil)

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	require.NotEmpty(t, options.OnchainChannels)
	assert.Equal(t, "BSC-USDT", options.OnchainChannels[0].Channel)

	options.OnchainChannels[0].Network = "tampered"
	again, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, SupplierPayoutNetworkBSC, again.OnchainChannels[0].Network,
		"options 直接吐出了全进程的注册表")
}
