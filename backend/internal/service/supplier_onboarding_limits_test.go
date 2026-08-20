//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 接入数量上限那两道闸。
//
// 与协议门禁分开一个文件，理由和它当初从 supplier_onboarding_service_test.go 里
// 分出去一样：这一组的每个用例都要摆一份**非默认**的上限配置，混在编排用例里会让
// 「这个用例的前提是什么」变成一件要往上翻两屏才知道的事。

// limitsJSON 拼一份接入上限配置。0 表示那道闸关着。
func limitsJSON(perUser, perIP int) string {
	return `{"max_accounts_per_user":` + strconv.Itoa(perUser) +
		`,"max_accounts_per_ip":` + strconv.Itoa(perIP) + `}`
}

// newLimitsService 组一个带指定上限的接入服务。协议默认已同意——这一组测的不是协议。
func newLimitsService(t *testing.T, repo *supplierOnboardingRepoStub, perUser, perIP int) *SupplierOnboardingService {
	t.Helper()
	return newOnboardingServiceWithLimits(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(perUser, perIP))
}

// ============================================================================
// 每人上限
// ============================================================================

func TestStartOAuthRejectsWhenUserCapReached(t *testing.T) {
	repo := &supplierOnboardingRepoStub{ownedCount: 3}
	svc := newLimitsService(t, repo, 3, 0)

	_, err := svc.StartOAuth(context.Background(), 7, testClientIP)

	assert.ErrorIs(t, err, ErrSupplierAccountLimitReached)
	// 拦在建会话之前：一个挂满了的人不该先跑完一整遍上游授权，末了才被告知这件事——
	// 那时他手上已经多出一个不需要、也无从撤销的 setup token。
	assert.NotContains(t, repo.calls, "CountPendingSessions")
	assert.Nil(t, repo.createdSession)
}

func TestStartOAuthAllowsWhenBelowUserCap(t *testing.T) {
	repo := &supplierOnboardingRepoStub{ownedCount: 2}
	svc := newLimitsService(t, repo, 3, 0)

	auth, err := svc.StartOAuth(context.Background(), 7, testClientIP)

	require.NoError(t, err)
	require.NotNil(t, auth)
}

func TestCompleteOAuthRejectsWhenUserCapReached(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession(), ownedCount: 3}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(3, 0))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	assert.ErrorIs(t, err, ErrSupplierAccountLimitReached)
	// 这是不可绕过的那一道。它必须挡在**领会话**之前：领取是一次性消费，
	// 在它之后被拒的人会白白丢掉手上的授权码。
	assert.NotContains(t, repo.calls, "ClaimSession")
	assert.Empty(t, store.accounts)
}

// 上限是「当下有几个」而不是「历史挂过几个」：解绑一个号必须真的腾出一个位置，
// 否则一个正常换号的供给者会永久地耗尽自己的额度。这条性质落在仓储那条 SQL 的
// `deleted_at IS NULL` 上（见集成测试），这里钉的是服务层确实按这个数判闸。
func TestCompleteOAuthAllowsAfterCountDropsBelowCap(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession(), ownedCount: 2}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(3, 0))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	require.NoError(t, err)
	assert.Len(t, store.accounts, 1)
}

// 上限配 0 = 不限。这条单拎出来是因为 `current >= 0` 恒真：少一个「闸开着吗」的
// 前置判断，现象就是**所有人都挂不上号**，而错的那一行看起来完全正常。
func TestCapsDisabledLetEveryoneThrough(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		ownedCount:   9999,
		countByIP:    map[string]int{testClientIP: 9999},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(0, 0))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	require.NoError(t, err)
	// 闸关着连那次 COUNT 都不该发：接入是一条会被反复重试的路径，白查的代价是每次
	// 重试都多一次 DB 往返。
	assert.NotContains(t, repo.calls, "CountAccountsByOwner")
	assert.NotContains(t, repo.calls, "CountAccountsByOriginIP")
}

// ============================================================================
// 每 IP 上限
// ============================================================================

func TestCompleteOAuthRejectsWhenNetworkCapReached(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		countByIP:    map[string]int{testClientIP: 10},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(0, 10))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	// 两个错误码必须分开。「你挂满了」是他自己能纠正的（解绑一个旧号），
	// 「这个网络挂满了」不是——把后者报成前者，他会去翻自己那一列空空如也的账号。
	assert.ErrorIs(t, err, ErrSupplierNetworkLimitReached)
	assert.NotErrorIs(t, err, ErrSupplierAccountLimitReached)
	assert.NotContains(t, repo.calls, "ClaimSession")
	assert.Equal(t, []string{testClientIP}, repo.countedIPs)
}

// 每 IP 那道闸数的是这个 IP 上的全部号，跨用户。这正是它存在的理由：
// 每人上限的绕过成本就是"再注册一个用户"，而换出口网络不免费。
func TestNetworkCapCountsAcrossUsers(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		ownedCount:   0, // 这个人自己一个号都没有
		countByIP:    map[string]int{testClientIP: 10},
	}
	svc := newOnboardingServiceWithLimits(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(5, 10))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	assert.ErrorIs(t, err, ErrSupplierNetworkLimitReached)
}

// 别的 IP 上挂满了不影响这个 IP：闸判的是精确 IP，不是"最近有人挂满过"。
func TestNetworkCapIsScopedToTheRequestIP(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		countByIP:    map[string]int{"198.51.100.9": 10},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(0, 10))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	require.NoError(t, err)
	assert.Len(t, store.accounts, 1)
}

// IP 取不到时整条闸跳过，而不是拿空串当一个"网络"。
//
// 拿 "" 去数意味着所有拿不到 IP 的请求被归到同一个虚构网络里互相挤占额度——
// 那道闸挡住的会是一群彼此毫无关系的人，且没有任何办法自证清白。
func TestEmptyClientIPSkipsNetworkCapEntirely(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		countByIP:    map[string]int{"": 10},
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(0, 10))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: "   ",
	})

	require.NoError(t, err)
	assert.Empty(t, repo.countedIPs, "拿不到 IP 时不该去数任何东西")
}

// ============================================================================
// 两道闸的先后与错误传播
// ============================================================================

// 每人那道排在每 IP 前面：它更便宜（一条按 owner 的索引 COUNT），且它的错误
// 对当事人更可行动。顺序反了不影响正确性，但会让"你挂满了"这件事被"网络挂满了"
// 盖掉——后者是一句他做不了什么的话。
func TestUserCapIsCheckedBeforeNetworkCap(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession: claimedSession(),
		ownedCount:   5,
		countByIP:    map[string]int{testClientIP: 10},
	}
	svc := newOnboardingServiceWithLimits(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{},
		enabledSupplyPoolJSON(), limitsJSON(5, 10))

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	assert.ErrorIs(t, err, ErrSupplierAccountLimitReached)
	assert.Empty(t, repo.countedIPs, "每人那道已经拒了，就不该再为每 IP 发一次查询")
}

// COUNT 出错 = 闸判不了 = 拒绝。放行等于"数据库一抖就没有上限"，
// 而那恰恰是刷号方最容易制造的状态。
func TestCapCheckFailsClosedWhenCountErrors(t *testing.T) {
	cases := []struct {
		name  string
		mutet func(*supplierOnboardingRepoStub)
	}{
		{"每人那道查不出来", func(r *supplierOnboardingRepoStub) {
			r.ownedCountErr = errors.New("db unreachable")
		}},
		{"每 IP 那道查不出来", func(r *supplierOnboardingRepoStub) {
			r.countByIPErr = errors.New("db unreachable")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
			tc.mutet(repo)
			store := newSupplierAccountStoreStub()
			svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
				enabledSupplyPoolJSON(), limitsJSON(5, 10))

			_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
				UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
			})

			require.Error(t, err)
			// 不是那两个业务错误：它们的文案说的是"你满了"，而真相是"我不知道你满没满"。
			assert.NotErrorIs(t, err, ErrSupplierAccountLimitReached)
			assert.NotErrorIs(t, err, ErrSupplierNetworkLimitReached)
			assert.Empty(t, store.accounts)
			assert.NotContains(t, repo.calls, "ClaimSession")
		})
	}
}

// 读不到配置时退回默认值，而不是退成"不限"。
//
// 这条与本仓其它几处 nil 语义刻意相反：probationSettings 拿不到配置只是显示不准，
// 这里拿不到配置是一道闸整个消失。默认的每人 5 个比运营通常配的值更严，
// 所以这个方向的回退不会放进任何本该被挡住的人。
func TestCapCheckFallsBackToDefaultsWhenSettingsUnavailable(t *testing.T) {
	repo := &supplierOnboardingRepoStub{ownedCount: 5}
	svc := &SupplierOnboardingService{repo: repo} // settings 缺席

	err := svc.requireCapacity(context.Background(), 7, testClientIP)

	assert.ErrorIs(t, err, ErrSupplierAccountLimitReached)
}

// nilOnboardingSettingsReader 是一个把接入上限读成 nil 的配置源。
//
// 现役的 *SettingService 永不返回 nil，但 s.settings 是个接口——挡住 nil 的那行
// 是给未来那个实现留的，而"留着的防御从没被证明有效"和没有防御是一回事。
type nilOnboardingSettingsReader struct{}

func (nilOnboardingSettingsReader) GetSupplyPoolSettings(context.Context) *SupplyPoolSettings {
	return &SupplyPoolSettings{Enabled: true, SupplyGroupID: testOnboardingSupplyGroupID, OverflowGroupID: 43}
}
func (nilOnboardingSettingsReader) GetSupplyProbationSettings(context.Context) *SupplyProbationSettings {
	return nil
}
func (nilOnboardingSettingsReader) GetSupplyAgreementSettings(context.Context) *SupplyAgreementSettings {
	return nil
}
func (nilOnboardingSettingsReader) GetSupplyOnboardingSettings(context.Context) *SupplyOnboardingSettings {
	return nil
}

func TestCapCheckFallsBackToDefaultsWhenSettingsReadReturnsNil(t *testing.T) {
	repo := &supplierOnboardingRepoStub{ownedCount: 5}
	svc := &SupplierOnboardingService{repo: repo, settings: nilOnboardingSettingsReader{}}

	err := svc.requireCapacity(context.Background(), 7, testClientIP)

	assert.ErrorIs(t, err, ErrSupplierAccountLimitReached)
}

// ============================================================================
// 接入来源的落库
// ============================================================================

func TestCompleteOAuthRecordsAccountOrigin(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: "  " + testClientIP + "  ",
	})
	require.NoError(t, err)

	require.Len(t, store.accounts, 1)
	var created *Account
	for _, a := range store.accounts {
		created = a
	}
	// 三元组必须对得上，尤其是 accountID：记错了号，每 IP 那道闸数的就是别人的账。
	// IP 落库前去掉两侧空白——同一个来源写成两个不同的字符串会把闸悄悄拆成两道。
	require.Len(t, repo.recordedOrigins, 1)
	assert.Equal(t, supplierOriginRecord{accountID: created.ID, userID: 7, clientIP: testClientIP},
		repo.recordedOrigins[0])
}

// 拿不到 IP 时不写行。写一条 client_ip='' 的记录会让所有来源不明的号在
// CountAccountsByOriginIP 里挤在同一个键上——见 EmptyClientIP 那条用例的理由。
func TestCompleteOAuthSkipsOriginWhenClientIPUnknown(t *testing.T) {
	repo := &supplierOnboardingRepoStub{claimSession: claimedSession()}
	svc := newOnboardingService(t, repo, newSupplierAccountStoreStub(), &supplierOAuthStub{}, enabledSupplyPoolJSON())

	_, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: "",
	})
	require.NoError(t, err)

	// 服务层照常调用仓储，由仓储决定"空 IP 不写行"——那条判断只有一个地方，
	// 而这里断言的是传下去的确实是空串（而不是被塞了个占位值）。
	require.Len(t, repo.recordedOrigins, 1)
	assert.Empty(t, repo.recordedOrigins[0].clientIP)
}

// 来源写失败不该让一次已经成功的接入变成"失败"。
//
// 号已经建出来、已经有主了，此刻返回错误也撤销不了这两件事，只会让供给者看到一个
// 失败提示、实际却挂上了的号——然后他会重试，于是有了第二个号。
// 代价是每 IP 那道闸少数一个，日志里有据可查。
func TestOriginWriteFailureDoesNotFailOnboarding(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		claimSession:    claimedSession(),
		recordOriginErr: errors.New("origins table unreachable"),
	}
	store := newSupplierAccountStoreStub()
	svc := newOnboardingService(t, repo, store, &supplierOAuthStub{}, enabledSupplyPoolJSON())

	view, err := svc.CompleteOAuth(context.Background(), &CompleteOAuthInput{
		UserID: 7, SessionID: "sess-1", Code: "c", ClientIP: testClientIP,
	})

	require.NoError(t, err)
	require.NotNil(t, view)
	require.Len(t, store.accounts, 1)
	// 而且必须继续走完最后一步：绑分组在它后面，被这次失败带跑的话，
	// 号会有主但不在池里——一个只有翻日志才看得见的状态。
	var created *Account
	for _, a := range store.accounts {
		created = a
	}
	assert.Equal(t, []int64{testOnboardingSupplyGroupID}, store.boundGroups[created.ID])
}
