//go:build unit

// APEXONE-EXT: 双边市场——提现服务的单元测试。
//
// 钱的搬运在仓储的事务里，由真库测试负责。这一层只干三件事，于是只测这三件：
// 读配置、校验入参、把「拒绝/撤回退款、打款不退」翻译成 Refund 布尔。
//
// 最后一件是本文件的重点。Refund 传错的两个方向都不会报错、都不会有日志：
// 传成 true 是给一笔已经打出去的钱再退一次（凭空发钱），传成 false 是拒了单子
// 却不把钱还回去（钱凭空消失）。它们只能靠断言「交给仓储的参数长什么样」抓住。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierWithdrawalRepoStub 把收到的参数原样记下来。
type supplierWithdrawalRepoStub struct {
	createParams  SupplierWithdrawalCreateParams
	resolveParams SupplierWithdrawalResolveParams
	listFilter    SupplierWithdrawalFilter
	calls         int

	pending    int64
	pendingErr error
	createErr  error
	resolveErr error

	// createRowOnErr 让 Create 在报错的同时也返回一行。真实仓储里「半个结果 +
	// 一个错误」的形状很常见，而只有这个形状能区分「先看 err 再通知」和
	// 「先通知再看 err」：createErr 单独用时 created 是 nil，通知会被
	// notifyRequested 的 nil 守卫挡住，于是调用顺序错了也测不出来。
	createRowOnErr  bool
	resolveRowOnErr bool
}

func (s *supplierWithdrawalRepoStub) Create(_ context.Context, params SupplierWithdrawalCreateParams) (*SupplierWithdrawal, error) {
	s.calls++
	s.createParams = params
	row := &SupplierWithdrawal{ID: 1, UserID: params.UserID, Amount: params.Amount, Status: SupplierWithdrawalStatusPending}
	if s.createErr != nil {
		if s.createRowOnErr {
			return row, s.createErr
		}
		return nil, s.createErr
	}
	return row, nil
}

func (s *supplierWithdrawalRepoStub) Resolve(_ context.Context, params SupplierWithdrawalResolveParams) (*SupplierWithdrawal, error) {
	s.calls++
	s.resolveParams = params
	row := &SupplierWithdrawal{ID: params.ID, Status: params.Status}
	if s.resolveErr != nil {
		if s.resolveRowOnErr {
			return row, s.resolveErr
		}
		return nil, s.resolveErr
	}
	return row, nil
}

func (s *supplierWithdrawalRepoStub) List(_ context.Context, filter SupplierWithdrawalFilter) ([]SupplierWithdrawal, int64, error) {
	s.calls++
	s.listFilter = filter
	return nil, 0, nil
}

func (s *supplierWithdrawalRepoStub) CountPending(_ context.Context, _ int64) (int64, error) {
	if s.pendingErr != nil {
		return 0, s.pendingErr
	}
	return s.pending, nil
}

// supplierWithdrawalSettingsStub 直接给一份配置，不碰 SettingService 的缓存。
type supplierWithdrawalSettingsStub struct {
	settings *SupplyWithdrawalSettings
}

func (s *supplierWithdrawalSettingsStub) GetSupplyWithdrawalSettings(context.Context) *SupplyWithdrawalSettings {
	if s.settings == nil {
		return DefaultSupplyWithdrawalSettings()
	}
	return s.settings
}

type supplierWithdrawalWalletStub struct {
	available float64
	err       error
}

func (s *supplierWithdrawalWalletStub) EnsureWallet(_ context.Context, userID int64) (*SupplierCreditSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &SupplierCreditSummary{UserID: userID, AvailableCredit: s.available}, nil
}

// openWithdrawalSettings 一份「开着且配好了」的配置。
func openWithdrawalSettings() *SupplyWithdrawalSettings {
	return &SupplyWithdrawalSettings{
		Enabled:    true,
		MinAmount:  50,
		MaxPending: 2,
		Channels:   []string{"USDT", "支付宝"},
		Notice:     "工作日 3 天内到账",
	}
}

func newWithdrawalService(
	repo SupplierWithdrawalRepository,
	settings *SupplyWithdrawalSettings,
	wallet supplierWithdrawalWalletReader,
) *SupplierWithdrawalService {
	return &SupplierWithdrawalService{
		repo:     repo,
		wallet:   wallet,
		settings: &supplierWithdrawalSettingsStub{settings: settings},
	}
}

// ============================================================================
// GetOptions：画表单的接口，不能因为一个统计数字整页失败
// ============================================================================

func TestGetWithdrawalOptionsSurvivesBrokenWalletAndCount(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{pendingErr: errors.New("db down")}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), &supplierWithdrawalWalletStub{err: errors.New("db down")})

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err, "读不到余额/未决单数不该让供给者连『提现开着没』都看不到")
	assert.True(t, options.Available)
	assert.Zero(t, options.AvailableCredit)
	assert.Zero(t, options.PendingCount)
	assert.Equal(t, []string{"USDT", "支付宝"}, options.Channels)
	assert.Equal(t, "工作日 3 天内到账", options.Notice)
}

func TestGetWithdrawalOptionsReportsWalletAndPending(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{pending: 1}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), &supplierWithdrawalWalletStub{available: 128.5})

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 128.5, options.AvailableCredit)
	assert.Equal(t, int64(1), options.PendingCount)
	assert.Equal(t, 50.0, options.MinAmount)
	assert.Equal(t, 2, options.MaxPending)
}

// 关着的时候照常返回一份 available=false 的表单，而不是报错：
// 这个接口的用途就是解释「为什么现在提不了」。
func TestGetWithdrawalOptionsWhenClosed(t *testing.T) {
	svc := newWithdrawalService(&supplierWithdrawalRepoStub{}, DefaultSupplyWithdrawalSettings(), nil)

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	assert.False(t, options.Available)
	assert.False(t, options.Enabled)
}

// Channels 必须是副本：直接把配置里那个切片交出去，上层 append 一下就改了全进程的配置。
func TestGetWithdrawalOptionsCopiesChannels(t *testing.T) {
	settings := openWithdrawalSettings()
	svc := newWithdrawalService(&supplierWithdrawalRepoStub{}, settings, nil)

	options, err := svc.GetOptions(context.Background(), 7)
	require.NoError(t, err)
	options.Channels[0] = "被改过的"
	assert.Equal(t, "USDT", settings.Channels[0])
}

// ============================================================================
// Request：校验顺序与每一条边界
// ============================================================================

// 功能没开时先答「没开」，而不是先把字段校验一遍——反过来的话，供给者会先收到
// 一串"账号不能为空"，改完了才发现提现根本没开放。
func TestRequestWithdrawalAnswersSwitchBeforeFields(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, DefaultSupplyWithdrawalSettings(), nil)

	// 一个字段全空、金额为 0 的请求：如果先校验字段，报的就不会是 DISABLED。
	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{})
	require.ErrorIs(t, err, ErrSupplierWithdrawalDisabled)
	assert.Zero(t, repo.calls)
}

// 开着但一个渠道都没配，报的是 NOT_CONFIGURED 而不是 CHANNEL_INVALID：
// 对运营来说这两句话对应完全不同的动作。
func TestRequestWithdrawalDistinguishesNotConfiguredFromInvalidChannel(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, &SupplyWithdrawalSettings{Enabled: true, MaxPending: 1}, nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{Amount: 100, PayoutChannel: "USDT"})
	require.ErrorIs(t, err, ErrSupplierWithdrawalNotConfigured)
	assert.Zero(t, repo.calls)
}

func TestRequestWithdrawalRejectsUnknownChannel(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "usdt", PayoutAccount: "0xabc",
	})
	require.ErrorIs(t, err, ErrSupplierWithdrawalChannelInvalid, "渠道白名单区分大小写")
	assert.Zero(t, repo.calls)
}

func TestRequestWithdrawalFieldValidation(t *testing.T) {
	cases := []struct {
		name string
		req  SupplierWithdrawalRequest
	}{
		{"收款账号为空", SupplierWithdrawalRequest{Amount: 100, PayoutChannel: "USDT"}},
		{"收款账号全是空白", SupplierWithdrawalRequest{Amount: 100, PayoutChannel: "USDT", PayoutAccount: "   "}},
		{"收款账号过长", SupplierWithdrawalRequest{
			Amount: 100, PayoutChannel: "USDT",
			PayoutAccount: strings.Repeat("x", SupplierPayoutAccountMaxLen+1),
		}},
		{"备注过长", SupplierWithdrawalRequest{
			Amount: 100, PayoutChannel: "USDT", PayoutAccount: "0xabc",
			UserNote: strings.Repeat("x", SupplierWithdrawalNoteMaxLen+1),
		}},
		{"金额为 0", SupplierWithdrawalRequest{Amount: 0, PayoutChannel: "USDT", PayoutAccount: "0xabc"}},
		{"金额为负", SupplierWithdrawalRequest{Amount: -1, PayoutChannel: "USDT", PayoutAccount: "0xabc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierWithdrawalRepoStub{}
			_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
				Request(context.Background(), 7, tc.req)
			require.Error(t, err)
			assert.Zero(t, repo.calls, "校验没过就不该碰钱")
		})
	}
}

// 长度按**字符**算而不是字节：一个 200 个汉字的收款账号在字节口径下早就超了 256，
// 但它是一个完全合理的地址/姓名。
func TestRequestWithdrawalCountsRunesNotBytes(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "支付宝",
		PayoutAccount: strings.Repeat("张", SupplierPayoutAccountMaxLen),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, repo.calls)
}

// 低于起提额是**拒绝**，不是夹到起提额：替供给者把 5 块的申请改成 50 块，
// 等于替他决定提多少钱。
func TestRequestWithdrawalRejectsBelowMinimumInsteadOfClamping(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 49.99, PayoutChannel: "USDT", PayoutAccount: "0xabc",
	})
	require.ErrorIs(t, err, ErrSupplierWithdrawalBelowMinimum)
	assert.Zero(t, repo.calls)
}

// 交给仓储的参数：user_id 来自调用方（不是请求体）、MaxPending 来自配置、
// 账号与备注都已 trim。MaxPending 从配置里带下去而不是让仓储自己读，
// 是为了让「用什么上限判的」在事务里是一个确定的值。
func TestRequestWithdrawalPassesSanitizedParams(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), nil)

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: " USDT ", PayoutAccount: "  0xabc  ", UserNote: "  麻烦了  ",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), repo.createParams.UserID)
	assert.Equal(t, 100.0, repo.createParams.Amount)
	assert.Equal(t, "USDT", repo.createParams.PayoutChannel)
	assert.Equal(t, "0xabc", repo.createParams.PayoutAccount)
	assert.Equal(t, "麻烦了", repo.createParams.UserNote)
	assert.Equal(t, 2, repo.createParams.MaxPending)
}

// 起提额为 0 = 不设门槛，此时一分钱的申请也该放行（金额本身仍须为正）。
func TestRequestWithdrawalAllowsAnyPositiveAmountWhenNoMinimum(t *testing.T) {
	settings := openWithdrawalSettings()
	settings.MinAmount = 0
	repo := &supplierWithdrawalRepoStub{}

	_, err := newWithdrawalService(repo, settings, nil).Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 0.01, PayoutChannel: "USDT", PayoutAccount: "0xabc",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.01, repo.createParams.Amount)
}

// ============================================================================
// Cancel：撤回不看总开关
// ============================================================================

// 运营关掉提现的那一刻，所有挂着的单子如果连撤回都不让，那笔钱就被锁死在一张
// 谁也不会处理的单子上了。开关是「还收不收新单」，不是「已经扣下来的钱还能不能拿回去」。
func TestCancelWithdrawalIgnoresTheEnabledSwitch(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, DefaultSupplyWithdrawalSettings(), nil)

	_, err := svc.Cancel(context.Background(), 7, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), repo.resolveParams.ID)
	assert.Equal(t, SupplierWithdrawalStatusCanceled, repo.resolveParams.Status)
}

// 撤回必须带上 UserID：少了它，任何人都能撤别人的单子（并把钱退进别人的钱包）。
func TestCancelWithdrawalScopesToOwner(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc := newWithdrawalService(repo, openWithdrawalSettings(), nil)

	_, err := svc.Cancel(context.Background(), 7, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(7), repo.resolveParams.UserID)
	assert.True(t, repo.resolveParams.Refund, "撤回必须退款，否则钱凭空消失")
	assert.Nil(t, repo.resolveParams.ReviewerID, "供给者自己撤回没有审核人")
}

func TestCancelWithdrawalRejectsAnonymousCaller(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).Cancel(context.Background(), 0, 42)
	require.Error(t, err)
	assert.Zero(t, repo.calls)
}

// ============================================================================
// List / AdminList
// ============================================================================

// 供给者的列表永远被钉在自己身上，且 user_id 只能来自调用方。
func TestListWithdrawalsIsScopedToCaller(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	_, _, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).List(context.Background(), 7, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), repo.listFilter.UserID)
	assert.Equal(t, 1, repo.listFilter.Page)
	assert.Equal(t, supplierWithdrawalDefaultPageSize, repo.listFilter.PageSize)
}

func TestListWithdrawalsClampsPageSize(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	_, _, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
		List(context.Background(), 7, -3, supplierWithdrawalMaxPageSize+1)
	require.NoError(t, err)
	assert.Equal(t, 1, repo.listFilter.Page)
	assert.Equal(t, supplierWithdrawalMaxPageSize, repo.listFilter.PageSize)
}

// 不认识的状态报错而不是当成「不筛」：后者会给出一个看起来正常、实际是全量的列表——
// 运营以为自己在看待办队列，其实在看全部。
func TestAdminListWithdrawalsRejectsUnknownStatus(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	_, _, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
		AdminList(context.Background(), SupplierWithdrawalFilter{Status: "PENDING"})
	require.Error(t, err)
	assert.Zero(t, repo.calls)
}

func TestAdminListWithdrawalsAcceptsKnownStatuses(t *testing.T) {
	for _, status := range []string{
		"",
		SupplierWithdrawalStatusPending,
		SupplierWithdrawalStatusPaid,
		SupplierWithdrawalStatusRejected,
		SupplierWithdrawalStatusCanceled,
	} {
		t.Run("status="+status, func(t *testing.T) {
			repo := &supplierWithdrawalRepoStub{}
			_, _, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
				AdminList(context.Background(), SupplierWithdrawalFilter{Status: status})
			require.NoError(t, err)
			assert.Equal(t, status, repo.listFilter.Status)
		})
	}
}

// ============================================================================
// MarkPaid / Reject：Refund 布尔是这一层最要命的一个字段
// ============================================================================

// 打款**不**退款：钱已经出去了，再退一次就是凭空发钱。
func TestMarkWithdrawalPaidDoesNotRefund(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	reviewer := int64(9)

	_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
		MarkPaid(context.Background(), 42, &reviewer, "TX-123", "已转账")
	require.NoError(t, err)
	assert.Equal(t, SupplierWithdrawalStatusPaid, repo.resolveParams.Status)
	assert.False(t, repo.resolveParams.Refund)
	assert.Equal(t, "TX-123", repo.resolveParams.ExternalRef)
	require.NotNil(t, repo.resolveParams.ReviewerID)
	assert.Equal(t, int64(9), *repo.resolveParams.ReviewerID)
	assert.Zero(t, repo.resolveParams.UserID, "管理端不按归属过滤")
}

// 拒绝**必须**退款：不退的话钱在申请那一刻就消失了，而且流水上找不回来。
func TestRejectWithdrawalRefunds(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}

	_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
		Reject(context.Background(), 42, nil, "  收款账号填错了  ")
	require.NoError(t, err)
	assert.Equal(t, SupplierWithdrawalStatusRejected, repo.resolveParams.Status)
	assert.True(t, repo.resolveParams.Refund)
	assert.Equal(t, "收款账号填错了", repo.resolveParams.ReviewNote)
}

// 拒绝理由必填：一笔被拒的提现是供给者最需要一个解释的时刻，
// 而「无理由拒绝」在一个双边市场里等于随时可以扣住别人的钱。
func TestRejectWithdrawalRequiresAReason(t *testing.T) {
	for _, note := range []string{"", "   "} {
		repo := &supplierWithdrawalRepoStub{}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			Reject(context.Background(), 42, nil, note)
		require.Error(t, err)
		assert.Zero(t, repo.calls)
	}
}

func TestResolveWithdrawalRejectsOversizedText(t *testing.T) {
	long := strings.Repeat("x", SupplierWithdrawalNoteMaxLen+1)
	longRef := strings.Repeat("x", SupplierWithdrawalExternalRefMaxLen+1)

	t.Run("打款凭证过长", func(t *testing.T) {
		repo := &supplierWithdrawalRepoStub{}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			MarkPaid(context.Background(), 42, nil, longRef, "")
		require.Error(t, err)
		assert.Zero(t, repo.calls)
	})
	t.Run("打款备注过长", func(t *testing.T) {
		repo := &supplierWithdrawalRepoStub{}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			MarkPaid(context.Background(), 42, nil, "", long)
		require.Error(t, err)
		assert.Zero(t, repo.calls)
	})
	t.Run("拒绝理由过长", func(t *testing.T) {
		repo := &supplierWithdrawalRepoStub{}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			Reject(context.Background(), 42, nil, long)
		require.Error(t, err)
		assert.Zero(t, repo.calls)
	})
}

// 打款凭证可以为空（不是每种渠道都有交易号），备注也可以为空。
func TestMarkWithdrawalPaidAllowsEmptyReference(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
		MarkPaid(context.Background(), 42, nil, "", "")
	require.NoError(t, err)
	assert.Equal(t, 1, repo.calls)
}

// ============================================================================
// 装配缺失时不能静默成功
// ============================================================================

// 依赖没装配起来时每个入口都必须报错。返回一个空列表/一个 nil 单子会让
// 「提现整条链路没接上」在界面上长得和「你还没提过现」一模一样。
func TestWithdrawalServiceUnavailableWithoutDeps(t *testing.T) {
	var nilSvc *SupplierWithdrawalService
	empty := &SupplierWithdrawalService{}

	for name, svc := range map[string]*SupplierWithdrawalService{"nil": nilSvc, "没装配": empty} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.GetOptions(context.Background(), 7)
			require.Error(t, err)
			_, err = svc.Request(context.Background(), 7, SupplierWithdrawalRequest{})
			require.Error(t, err)
			_, err = svc.Cancel(context.Background(), 7, 1)
			require.Error(t, err)
			_, _, err = svc.List(context.Background(), 7, 1, 20)
			require.Error(t, err)
			_, _, err = svc.AdminList(context.Background(), SupplierWithdrawalFilter{})
			require.Error(t, err)
			_, err = svc.MarkPaid(context.Background(), 1, nil, "", "")
			require.Error(t, err)
			_, err = svc.Reject(context.Background(), 1, nil, "理由")
			require.Error(t, err)
		})
	}
}

// 仓储的错误必须原样冒上来，不能被吞成一个"成功"。
func TestWithdrawalServicePropagatesRepoErrors(t *testing.T) {
	t.Run("申请时余额不足", func(t *testing.T) {
		repo := &supplierWithdrawalRepoStub{createErr: ErrSupplierCreditInsufficient}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			Request(context.Background(), 7, SupplierWithdrawalRequest{
				Amount: 100, PayoutChannel: "USDT", PayoutAccount: "0xabc",
			})
		require.ErrorIs(t, err, ErrSupplierCreditInsufficient)
	})
	t.Run("单子已是终态", func(t *testing.T) {
		repo := &supplierWithdrawalRepoStub{resolveErr: ErrSupplierWithdrawalNotPending}
		_, err := newWithdrawalService(repo, openWithdrawalSettings(), nil).
			MarkPaid(context.Background(), 42, nil, "", "")
		require.ErrorIs(t, err, ErrSupplierWithdrawalNotPending)
	})
}

// ============================================================================
// 通知：哪一步该发信、哪一步不该
//
// 「不该发」的两条比「该发」更值得测：撤回补一封信只是噪音，而失败的操作发信
// 是在告诉供给者一件没有发生的事——他会以为钱已经扣了，然后来问为什么余额没变。
// 这两条只有能观察到"没调用"才钉得住，所以通知出口做成了接口。
// ============================================================================

type withdrawalNotifierSpy struct {
	requested []SupplierWithdrawal
	resolved  []SupplierWithdrawal
}

func (s *withdrawalNotifierSpy) NotifyRequested(w *SupplierWithdrawal) {
	if w != nil {
		s.requested = append(s.requested, *w)
	}
}

func (s *withdrawalNotifierSpy) NotifyResolved(w *SupplierWithdrawal) {
	if w != nil {
		s.resolved = append(s.resolved, *w)
	}
}

func newWithdrawalServiceWithNotifier(
	repo SupplierWithdrawalRepository,
	settings *SupplyWithdrawalSettings,
) (*SupplierWithdrawalService, *withdrawalNotifierSpy) {
	spy := &withdrawalNotifierSpy{}
	svc := newWithdrawalService(repo, settings, &supplierWithdrawalWalletStub{available: 1000})
	svc.notifier = spy
	return svc, spy
}

func TestWithdrawalRequestNotifies(t *testing.T) {
	repo := &supplierWithdrawalRepoStub{}
	svc, spy := newWithdrawalServiceWithNotifier(repo, openWithdrawalSettings())

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "USDT", PayoutAccount: "T-addr",
	})
	require.NoError(t, err)
	require.Len(t, spy.requested, 1, "新申请必须通知：运营不被叫过来，这张单可以躺三天")
	assert.Equal(t, int64(7), spy.requested[0].UserID)
	assert.Empty(t, spy.resolved)
}

// 申请失败不发信。发了就是在告诉供给者一件没有发生的事。
func TestWithdrawalRequestDoesNotNotifyOnFailure(t *testing.T) {
	cases := []struct {
		name string
		repo *supplierWithdrawalRepoStub
		req  SupplierWithdrawalRequest
	}{
		{
			name: "落库失败（撞未决单上限）",
			repo: &supplierWithdrawalRepoStub{createErr: ErrSupplierWithdrawalTooManyPending},
			req:  SupplierWithdrawalRequest{Amount: 100, PayoutChannel: "USDT", PayoutAccount: "T-addr"},
		},
		{
			name: "校验没过（低于起提额）",
			repo: &supplierWithdrawalRepoStub{},
			req:  SupplierWithdrawalRequest{Amount: 1, PayoutChannel: "USDT", PayoutAccount: "T-addr"},
		},
		{
			// 落库失败但仓储仍然回了一行。这一例钉的是**调用顺序**：通知必须在
			// err 检查之后，否则就会发出一封关于一张并不存在的单子的邮件，而
			// 供给者会以为自己的余额被扣了。
			name: "落库失败但返回了半个结果",
			repo: &supplierWithdrawalRepoStub{createErr: ErrSupplierWithdrawalTooManyPending, createRowOnErr: true},
			req:  SupplierWithdrawalRequest{Amount: 100, PayoutChannel: "USDT", PayoutAccount: "T-addr"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, spy := newWithdrawalServiceWithNotifier(tc.repo, openWithdrawalSettings())
			_, err := svc.Request(context.Background(), 7, tc.req)
			require.Error(t, err)
			assert.Empty(t, spy.requested, "失败的申请不该发出任何通知")
		})
	}
}

func TestWithdrawalMarkPaidAndRejectNotify(t *testing.T) {
	cases := []struct {
		name   string
		invoke func(*SupplierWithdrawalService) error
		status string
	}{
		{
			name: "已打款",
			invoke: func(svc *SupplierWithdrawalService) error {
				_, err := svc.MarkPaid(context.Background(), 3, nil, "TX-1", "")
				return err
			},
			status: SupplierWithdrawalStatusPaid,
		},
		{
			name: "被拒绝",
			invoke: func(svc *SupplierWithdrawalService) error {
				_, err := svc.Reject(context.Background(), 3, nil, "收款账号无效")
				return err
			},
			status: SupplierWithdrawalStatusRejected,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, spy := newWithdrawalServiceWithNotifier(&supplierWithdrawalRepoStub{}, openWithdrawalSettings())
			require.NoError(t, tc.invoke(svc))
			require.Len(t, spy.resolved, 1)
			assert.Equal(t, tc.status, spy.resolved[0].Status)
		})
	}
}

// 撤回是供给者自己刚点的按钮，界面上已经有确认框和 toast。再补一封邮件只是噪音，
// 而噪音的代价是他开始忽略这个发件人——包括下一封"你的提现被拒了"。
func TestWithdrawalCancelDoesNotNotify(t *testing.T) {
	svc, spy := newWithdrawalServiceWithNotifier(&supplierWithdrawalRepoStub{}, openWithdrawalSettings())
	_, err := svc.Cancel(context.Background(), 7, 3)
	require.NoError(t, err)
	assert.Empty(t, spy.resolved, "撤回不该发信")
	assert.Empty(t, spy.requested)
}

// 审批失败（单子已经不是 pending 了）同样不发信。
func TestWithdrawalResolveDoesNotNotifyOnFailure(t *testing.T) {
	// resolveRowOnErr：见 createRowOnErr 的注释。没有这一行的话，resolved 是
	// nil，拦住通知的是 notifyResolved 的 nil 守卫而不是这里要钉的 err 检查顺序。
	repo := &supplierWithdrawalRepoStub{resolveErr: ErrSupplierWithdrawalNotPending, resolveRowOnErr: true}
	svc, spy := newWithdrawalServiceWithNotifier(repo, openWithdrawalSettings())

	_, err := svc.MarkPaid(context.Background(), 3, nil, "", "")
	require.Error(t, err)
	assert.Empty(t, spy.resolved, "一笔没成功的打款不该告诉供给者钱已经到账")
}

// 没配通知时提现必须照常工作。这条看着像废话，但它挡住的是一个真实的写法：
// 把 notifier 直接塞进结构体而不判 nil，于是"没配邮件"变成提现主路径上的
// 一次空指针 panic。
func TestWithdrawalWorksWithoutNotifier(t *testing.T) {
	svc := newWithdrawalService(&supplierWithdrawalRepoStub{}, openWithdrawalSettings(), &supplierWithdrawalWalletStub{available: 1000})
	require.True(t, svc.notifier == nil) //nolint:testifylint // 见 TestNewWithdrawalServiceKeepsNilNotifierNil

	_, err := svc.Request(context.Background(), 7, SupplierWithdrawalRequest{
		Amount: 100, PayoutChannel: "USDT", PayoutAccount: "T-addr",
	})
	assert.NoError(t, err)

	_, err = svc.MarkPaid(context.Background(), 3, nil, "", "")
	assert.NoError(t, err)
}

// 构造函数收到一个 nil 的具体指针时，对应字段必须仍是 nil **接口**。
// 直接赋值的话会得到一个"非 nil 接口装着 nil 指针"，下游的 nil 判断就全失效了。
//
// 两个字段都要钉，而且两处的代价不一样：
//   - notifier：typed-nil 会让 s.notifier == nil 判假，于是提现主路径上一次空指针 panic。
//   - addresses：typed-nil 会让 resolveOnchainAccount 走进"绑定服务已装配"那条分支，
//     在一个 nil 的 *SupplierPayoutWalletService 上调方法。那个方法的接收者判了 nil
//     （ready()），所以不会 panic，而是返回 unavailable——比 panic 更糟：
//     链上渠道会变成"服务不可用"，而运营会去查一个根本没坏的绑定服务。
func TestNewWithdrawalServiceKeepsNilDepsNil(t *testing.T) {
	svc := NewSupplierWithdrawalService(&supplierWithdrawalRepoStub{}, nil, nil, nil, nil, nil)
	// 用 == nil 而不是 assert.Nil：testify 的 Nil 会对"接口里装着一个 nil 指针"
	// 也返回 true，而那恰好就是这条测试要拦的东西——它会把自己要测的 bug 判成通过。
	// 生产代码里的 `s.notifier == nil` 用的是这里这个语义。
	assert.True(t, svc.notifier == nil, "typed-nil 接口会让所有 nil 判断失效")  //nolint:testifylint
	assert.True(t, svc.addresses == nil, "typed-nil 接口会让所有 nil 判断失效") //nolint:testifylint
}
