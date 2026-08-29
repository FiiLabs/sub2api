//go:build integration

// APEXONE-EXT: 双边市场——自助接入仓储的真库测试（迁移 226 + 224 的 owner_user_id）。
//
// 这一组测的全是**只有真 Postgres 才成立的性质**，sqlmock 一条都证不了：
//
//   - 一次性领取靠的是 `UPDATE ... WHERE consumed_at IS NULL RETURNING` 的行锁语义；
//     mock 里它只是一个字符串。
//   - 归属写入靠 `AND owner_user_id IS NULL` 拦住改归属，而这一列本身是迁移 224
//     新加的——列名写错在 mock 里静默通过，在真库里立刻炸。
//   - 按状态扫号的 SQL 是用 service 侧常量 fmt.Sprintf 拼出来的，`extra->>'key'`
//     的 jsonb 取值行为只有真库能验。
//   - 过期清理的 `expires_at <= NOW()` 用的是**数据库的**时钟，不是 Go 的。
package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOAuthSession(userID int64, tag string, ttl time.Duration) *service.SupplierOAuthSession {
	return &service.SupplierOAuthSession{
		SessionID:    fmt.Sprintf("sess-%s-%d", tag, time.Now().UnixNano()),
		UserID:       userID,
		Platform:     service.PlatformAnthropic,
		State:        fmt.Sprintf("state-%s", tag),
		CodeVerifier: fmt.Sprintf("verifier-%s", tag),
		Scope:        "org:create_api_key user:profile user:inference",
		ExpiresAt:    time.Now().Add(ttl),
	}
}

// 会话建得起来、原样取得回来，且**只能被领一次**。
//
// 一次性是这套流程唯一挡住「同一个授权码换两次、建出两个账号」的东西，
// 而它整个落在一条 UPDATE 的 WHERE 上——必须在真库上证。
func TestSupplierOnboarding_SessionIsClaimableExactlyOnce(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "onboard-claim")
	session := newTestOAuthSession(userID, "claim", 10*time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, session))

	claimed, err := repo.ClaimSession(txCtx, session.SessionID, userID, 0)
	require.NoError(t, err)
	require.Equal(t, session.SessionID, claimed.SessionID)
	require.Equal(t, userID, claimed.UserID)
	require.Equal(t, session.Platform, claimed.Platform)
	require.Equal(t, session.State, claimed.State)
	// code_verifier 必须原样取回：PKCE 换 token 时少一个字符就是一次彻底失败的授权。
	require.Equal(t, session.CodeVerifier, claimed.CodeVerifier)
	require.Equal(t, session.Scope, claimed.Scope)

	// 第二次领取拿不到——重放不会再建一个账号。
	_, err = repo.ClaimSession(txCtx, session.SessionID, userID, 0)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)
}

// 拿别人的 session_id 领不走：归属是服务端记下的事实，不是调用方自称的。
func TestSupplierOnboarding_SessionCannotBeClaimedByAnotherUser(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "onboard-owner")
	attackerID := mustCreateSupplier(t, client, "onboard-attacker")
	session := newTestOAuthSession(ownerID, "steal", 10*time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, session))

	_, err := repo.ClaimSession(txCtx, session.SessionID, attackerID, 0)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 失败的领取不能顺手把会话标记成已消费，否则就是一条免费的拒绝服务。
	claimed, err := repo.ClaimSession(txCtx, session.SessionID, ownerID, 0)
	require.NoError(t, err)
	require.Equal(t, ownerID, claimed.UserID)
}

// 过期会话领不走。过期判定用**数据库的** NOW()，多实例时钟漂移不会让它忽然可领。
func TestSupplierOnboarding_ExpiredSessionIsNeitherClaimableNorPending(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "onboard-expired")
	expired := newTestOAuthSession(userID, "expired", -time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, expired))
	live := newTestOAuthSession(userID, "live", 10*time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, live))

	_, err := repo.ClaimSession(txCtx, expired.SessionID, userID, 0)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 限流按「还能用的会话」计数，过期的不该继续占着名额。
	pending, err := repo.CountPendingSessions(txCtx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, pending)

	// 清理只删过期且从未被消费的行。
	deleted, err := repo.DeleteExpiredSessions(txCtx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	// 未过期的那条毫发无伤。
	claimed, err := repo.ClaimSession(txCtx, live.SessionID, userID, 0)
	require.NoError(t, err)
	require.Equal(t, live.SessionID, claimed.SessionID)
}

// 已消费的会话不被过期清理带走：它是「这个账号是谁在什么时候挂上来的」的唯一证据。
func TestSupplierOnboarding_ConsumedSessionSurvivesExpiryCleanup(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "onboard-consumed")
	session := newTestOAuthSession(userID, "consumed", 10*time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, session))
	_, err := repo.ClaimSession(txCtx, session.SessionID, userID, 0)
	require.NoError(t, err)

	// 把它推成过期，但它已经被消费过了。
	_, err = client.ExecContext(txCtx,
		`UPDATE supplier_oauth_sessions SET expires_at = NOW() - INTERVAL '1 hour' WHERE session_id = $1`,
		session.SessionID)
	require.NoError(t, err)

	deleted, err := repo.DeleteExpiredSessions(txCtx, 100)
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.Equal(t, 1, querySingleInt(t, txCtx, client,
		"SELECT COUNT(*)::int FROM supplier_oauth_sessions WHERE session_id = $1", session.SessionID))
}

// 两类会话互不通兑（迁移 237 的 account_id + 领取语句里的 IS NOT DISTINCT FROM）。
//
// 只有真库能证：`IS NOT DISTINCT FROM` 的 NULL 语义是 Postgres 的，而这条判断
// **整个落在那条一次性 UPDATE 的 WHERE 上**——它是「用一次重新授权的授权码去建
// 一个新号」这条路径的唯一阻断点。写成领取之后再校验，就是一句能被重构删掉的 if；
// 写在 WHERE 里，删掉它就没有行返回。
func TestSupplierOnboarding_ReauthAndOnboardingSessionsAreNotInterchangeable(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "session-kind")
	account := mustCreateAccount(t, client, &service.Account{
		Name: fmt.Sprintf("session-kind-acct-%d", time.Now().UnixNano()),
	})

	// 一条接入会话（account_id 为 NULL）。
	onboarding := newTestOAuthSession(userID, "kind-onboard", 10*time.Minute)
	require.NoError(t, repo.CreateSession(txCtx, onboarding))

	// 一条重新授权会话，绑在那个号上。
	reauth := newTestOAuthSession(userID, "kind-reauth", 10*time.Minute)
	reauth.AccountID = account.ID
	require.NoError(t, repo.CreateSession(txCtx, reauth))

	// 拿接入会话去修号：领不到。
	_, err := repo.ClaimSession(txCtx, onboarding.SessionID, userID, account.ID)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 拿重新授权会话去建新号：同样领不到。这一支是要紧的那一支——
	// 它挡住「授权一次、既修了旧号又白得一个新号」。
	_, err = repo.ClaimSession(txCtx, reauth.SessionID, userID, 0)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 拿重新授权会话去修**另一个**号：也领不到。
	_, err = repo.ClaimSession(txCtx, reauth.SessionID, userID, account.ID+99999)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 几次失败的领取都不能把会话标成已消费，否则就是一条免费的拒绝服务。
	claimed, err := repo.ClaimSession(txCtx, reauth.SessionID, userID, account.ID)
	require.NoError(t, err)
	require.Equal(t, account.ID, claimed.AccountID, "领回来的会话要带着它绑定的号")

	claimedOnboarding, err := repo.ClaimSession(txCtx, onboarding.SessionID, userID, 0)
	require.NoError(t, err)
	require.Zero(t, claimedOnboarding.AccountID, "接入会话读回来是 0，不是 NULL 扫描错误")
}

// ---------------------------------------------------------------------------
// 就地重新授权（迁移 237）
// ---------------------------------------------------------------------------

// 凭证**整份替换**、extra **合并**。
//
// 两个方向都必须验，而且只有真库能验——它们分别落在同一条 UPDATE 的
// `credentials = $3::jsonb` 与 `extra = COALESCE(extra,'{}') || $4::jsonb` 上。
// 写反任何一个的后果都是静默的：凭证若变成合并，上一份 refresh_token 会留下来，
// 号在下一次刷新时用一份作废的 token；extra 若变成替换，供给者自己设的每日上限
// 和接入状态会一起消失，而界面上只会显示成「他从没设过上限」。
func TestSupplierOnboarding_ReauthReplacesCredentialsAndMergesExtra(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "reauth-owner")
	stamp := time.Now().UnixNano()
	account := mustCreateAccount(t, client, &service.Account{
		Name: fmt.Sprintf("reauth-acct-%d", stamp),
		Credentials: map[string]any{
			"access_token":  "old-at",
			"refresh_token": "old-rt-SECRET",
			"account_uuid":  fmt.Sprintf("reauth-uuid-%d", stamp),
		},
		Extra: map[string]any{
			service.SupplyStateExtraKey:          service.SupplyStateActive,
			service.SupplyProbeErrorExtraKey:     "API returned 401: expired",
			service.SupplyProbePassesExtraKey:    0,
			service.SupplyDailyCostLimitExtraKey: 12.5,
		},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))
	setAccountSchedulable(t, client, account.ID, false)

	require.NoError(t, repo.ApplyReauthCredentials(txCtx, account.ID, ownerID,
		map[string]any{
			"access_token": "new-at",
			"account_uuid": fmt.Sprintf("reauth-uuid-%d", stamp),
		},
		map[string]any{
			service.SupplyProbeErrorExtraKey:  "",
			service.SupplyProbePassesExtraKey: 0,
		}))

	creds, extra, schedulable := readAccountRow(t, txCtx, client, account.ID)
	require.Contains(t, creds, "new-at")
	require.NotContains(t, creds, "old-at")
	require.NotContains(t, creds, "old-rt-SECRET",
		"整份替换：上一份 refresh_token 一个字节都不该留下")

	// extra 是合并：这次没写的键必须还在。
	require.Contains(t, extra, service.SupplyDailyCostLimitExtraKey,
		"供给者自己设的每日上限不能被一次重新授权抹掉")
	require.Contains(t, extra, service.SupplyStateActive, "接入状态不该被这条语句动到")

	// 这条语句刻意不碰 schedulable——把号放回池子要发调度事件，由 accountRepo 做。
	require.False(t, schedulable)
}

// 换凭证只能换自己的号，且软删的行拒绝。
//
// 与 ScrubAccountCredentials 同一条 TOCTOU 理由：上层查过归属，但那次查与这次写
// 之间隔着一次网络往返。这里更要紧一点——抹凭证的最坏后果是别人的号停了，
// 而换凭证的最坏后果是**别人的号被换成了我的订阅**。
func TestSupplierOnboarding_ReauthRefusesForeignAndDeletedAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "reauth-mine")
	strangerID := mustCreateSupplier(t, client, "reauth-stranger")
	stamp := time.Now().UnixNano()
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("reauth-guard-%d", stamp),
		Credentials: map[string]any{"refresh_token": "rt-secret"},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))

	err := repo.ApplyReauthCredentials(txCtx, account.ID, strangerID,
		map[string]any{"access_token": "attacker-at"}, nil)
	require.ErrorIs(t, err, service.ErrSupplierAccountNotFound)

	creds, _, _ := readAccountRow(t, txCtx, client, account.ID)
	require.Contains(t, creds, "rt-secret", "别人的凭证一个字节都不该被动到")
	require.NotContains(t, creds, "attacker-at")

	_, execErr := client.ExecContext(txCtx,
		"UPDATE accounts SET deleted_at = NOW() WHERE id = $1", account.ID)
	require.NoError(t, execErr)
	require.ErrorIs(t,
		repo.ApplyReauthCredentials(txCtx, account.ID, ownerID,
			map[string]any{"access_token": "new-at"}, nil),
		service.ErrSupplierAccountNotFound)
}

// extra 传 nil 时不能把整个 extra 变成 NULL。
//
// `extra || NULL` 在 Postgres 里是 NULL——那一下会把接入状态、每日上限、
// 观察期进度一起抹掉，而症状是这个号从供给者的界面上「变回了刚接入的样子」。
func TestSupplierOnboarding_ReauthWithNilExtraKeepsExistingExtra(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "reauth-nil-extra")
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("reauth-nil-%d", time.Now().UnixNano()),
		Credentials: map[string]any{"access_token": "old-at"},
		Extra: map[string]any{
			service.SupplyStateExtraKey:          service.SupplyStateActive,
			service.SupplyDailyCostLimitExtraKey: 7.5,
		},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))

	require.NoError(t, repo.ApplyReauthCredentials(txCtx, account.ID, ownerID,
		map[string]any{"access_token": "new-at"}, nil))

	_, extra, _ := readAccountRow(t, txCtx, client, account.ID)
	require.Contains(t, extra, service.SupplyStateActive)
	require.Contains(t, extra, service.SupplyDailyCostLimitExtraKey)
}

// ---------------------------------------------------------------------------
// 账号归属（迁移 224 的 owner_user_id）
// ---------------------------------------------------------------------------

// 归属只能从「无主」写到「有主」，永远不能从 A 改成 B。
// 归属是钱的去向，改归属必须是显式的运营动作，不能是某条接入路径的副作用。
func TestSupplierOnboarding_AccountOwnerIsWriteOnceFromNull(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "acct-owner")
	otherID := mustCreateSupplier(t, client, "acct-other")
	account := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("acct-%d", time.Now().UnixNano())})

	// 新建的账号是自营的：owner_user_id 为 NULL，读出来是 0。
	owner, err := repo.GetAccountOwner(txCtx, account.ID)
	require.NoError(t, err)
	require.Zero(t, owner)

	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))
	owner, err = repo.GetAccountOwner(txCtx, account.ID)
	require.NoError(t, err)
	require.Equal(t, ownerID, owner)

	// 抢归属必须失败，且不能把已有归属改掉。
	err = repo.SetAccountOwner(txCtx, account.ID, otherID)
	require.Error(t, err)
	owner, err = repo.GetAccountOwner(txCtx, account.ID)
	require.NoError(t, err)
	require.Equal(t, ownerID, owner, "已有归属不能被第二次写入覆盖")
}

// 按归属人列号只列自己的；按接入状态扫号只扫供给号，且状态缺失算 pending_review。
//
// 后半句是拼接 SQL 与 service 常量之间的契约：两边一旦漂移，症状是观察期任务
// 扫不到任何账号——一个完全静默的失效，只有真库能把它照出来。
func TestSupplierOnboarding_ListsAreScopedByOwnerAndState(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "list-owner")
	otherID := mustCreateSupplier(t, client, "list-other")
	stamp := time.Now().UnixNano()

	// 我的：一个显式 active、一个状态缺失（应被当作 pending_review）。
	mine1 := mustCreateAccount(t, client, &service.Account{
		Name:  fmt.Sprintf("mine-active-%d", stamp),
		Extra: map[string]any{service.SupplyStateExtraKey: service.SupplyStateActive},
	})
	mine2 := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("mine-nostate-%d", stamp)})
	// 别人的、以及一个自营号（无主）——两者都不该出现在任何一个列表里。
	theirs := mustCreateAccount(t, client, &service.Account{
		Name:  fmt.Sprintf("theirs-%d", stamp),
		Extra: map[string]any{service.SupplyStateExtraKey: service.SupplyStateActive},
	})
	firstParty := mustCreateAccount(t, client, &service.Account{
		Name:  fmt.Sprintf("first-party-%d", stamp),
		Extra: map[string]any{service.SupplyStateExtraKey: service.SupplyStateActive},
	})

	require.NoError(t, repo.SetAccountOwner(txCtx, mine1.ID, ownerID))
	require.NoError(t, repo.SetAccountOwner(txCtx, mine2.ID, ownerID))
	require.NoError(t, repo.SetAccountOwner(txCtx, theirs.ID, otherID))

	ids, err := repo.ListAccountIDsByOwner(txCtx, ownerID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{mine1.ID, mine2.ID}, ids)

	active, err := repo.ListAccountIDsBySupplyState(txCtx, service.SupplyStateActive, 100)
	require.NoError(t, err)
	require.Contains(t, active, mine1.ID)
	require.Contains(t, active, theirs.ID)
	require.NotContains(t, active, mine2.ID)
	require.NotContains(t, active, firstParty.ID, "自营号（owner_user_id IS NULL）不进供给侧任何扫描")

	pending, err := repo.ListAccountIDsBySupplyState(txCtx, service.SupplyStatePendingReview, 100)
	require.NoError(t, err)
	require.Contains(t, pending, mine2.ID, "状态缺失必须兜底成 pending_review，否则新挂的号永远不被观察期任务扫到")
	require.NotContains(t, pending, mine1.ID)
}

// setUserStatus / softDeleteUser 直接改库，因为要造的正是「上游代码路径造不出来的状态」。
//
// 用 raw SQL 而不是 ent builder：这条闸要防的就是本仓的销号是**软删**这件事，
// 走 ent 的 Delete 会被 soft-delete mixin 拦成同样的 UPDATE，测试反而看不清自己在造什么。
func setUserStatus(t *testing.T, client *dbent.Client, userID int64, status string) {
	t.Helper()
	_, err := client.ExecContext(context.Background(),
		"UPDATE users SET status = $1 WHERE id = $2", status, userID)
	require.NoError(t, err)
}

func softDeleteUser(t *testing.T, client *dbent.Client, userID int64) {
	t.Helper()
	_, err := client.ExecContext(context.Background(),
		"UPDATE users SET deleted_at = NOW() WHERE id = $1", userID)
	require.NoError(t, err)
}

func setAccountSchedulable(t *testing.T, client *dbent.Client, accountID int64, schedulable bool) {
	t.Helper()
	_, err := client.ExecContext(context.Background(),
		"UPDATE accounts SET schedulable = $1 WHERE id = $2", schedulable, accountID)
	require.NoError(t, err)
}

// 归属人已经不在了，号却还在供货——这一条查询是发现它们的唯一途径。
//
// 为什么必须在真库上测：
//
//   - 销号是软删，`accounts.owner_user_id` 上的 `ON DELETE SET NULL` 因此**永不触发**。
//     这正是这个 bug 的成因，而它只在真库的外键语义下才成立——mock 里没有外键，
//     "级联会不会触发"这个问题根本提不出来。
//   - `u.status <> 'active'` 与 `u.deleted_at IS NOT NULL` 是两个独立的失效来源，
//     漏掉任何一个都是静默放行。
//   - 那个 OR 分支（已 retired 但仍 schedulable）是最危险的一类号：状态写着"已下线"，
//     实际还在接单。它必须被扫到，而这依赖 jsonb 取值与布尔列的组合求值。
func TestSupplierOnboarding_ListsAccountsWhoseOwnerIsGone(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	stamp := time.Now().UnixNano()
	healthy := mustCreateSupplier(t, client, "orphan-healthy")
	disabled := mustCreateSupplier(t, client, "orphan-disabled")
	deleted := mustCreateSupplier(t, client, "orphan-deleted")

	newAccount := func(tag, state string) *service.Account {
		return mustCreateAccount(t, client, &service.Account{
			Name:  fmt.Sprintf("%s-%d", tag, stamp),
			Extra: map[string]any{service.SupplyStateExtraKey: state},
		})
	}

	okAccount := newAccount("owner-ok", service.SupplyStateActive)
	disabledAccount := newAccount("owner-disabled", service.SupplyStateActive)
	deletedAccount := newAccount("owner-deleted", service.SupplyStatePendingReview)
	// 已经收摊的号：状态终态且不可调度，没有任何事情要做，不该每轮都被重扫。
	settledAccount := newAccount("owner-disabled-settled", service.SupplyStateRetired)
	// 最危险的一类：状态写着已下线，schedulable 却还是真——它此刻在接单。
	lyingAccount := newAccount("owner-disabled-still-serving", service.SupplyStateRetired)
	// 自营号：没有归属人，与这条闸无关。
	firstParty := newAccount("first-party", service.SupplyStateActive)

	require.NoError(t, repo.SetAccountOwner(txCtx, okAccount.ID, healthy))
	require.NoError(t, repo.SetAccountOwner(txCtx, disabledAccount.ID, disabled))
	require.NoError(t, repo.SetAccountOwner(txCtx, deletedAccount.ID, deleted))
	require.NoError(t, repo.SetAccountOwner(txCtx, settledAccount.ID, disabled))
	require.NoError(t, repo.SetAccountOwner(txCtx, lyingAccount.ID, disabled))

	setAccountSchedulable(t, client, settledAccount.ID, false)
	setUserStatus(t, client, disabled, service.StatusDisabled)
	softDeleteUser(t, client, deleted)

	ids, err := repo.ListAccountIDsWithUnavailableOwner(txCtx, 100)
	require.NoError(t, err)

	require.Contains(t, ids, disabledAccount.ID, "被停用的人的号必须被扫到")
	require.Contains(t, ids, deletedAccount.ID,
		"注销是软删，owner_user_id 的 ON DELETE SET NULL 永不触发——这条查询是唯一的发现途径")
	require.Contains(t, ids, lyingAccount.ID,
		"状态写着 retired 但仍可调度的号是最危险的一类：它在接单")
	require.NotContains(t, ids, okAccount.ID)
	require.NotContains(t, ids, settledAccount.ID, "已经停稳的号不该每轮重扫")
	require.NotContains(t, ids, firstParty.ID, "自营号没有归属人，不归这条闸管")
}

// ---------------------------------------------------------------------------
// 解绑：抹掉凭证
// ---------------------------------------------------------------------------

// readAccountRow 读回一行账号的凭证 / extra / 可调度性，用来验证 UPDATE 真的落了地。
//
// 走 raw SQL 而不是 ent 的 GetByID：这个测试要看的是**列里现在是什么**，
// 经过 mapper 之后 `credentials = '{}'` 和 `credentials IS NULL` 会长得一模一样。
func readAccountRow(t *testing.T, ctx context.Context, client *dbent.Client, accountID int64) (creds, extra string, schedulable bool) {
	t.Helper()
	rows, err := client.QueryContext(ctx,
		"SELECT credentials::text, extra::text, schedulable FROM accounts WHERE id = $1", accountID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "account %d should still exist", accountID)
	require.NoError(t, rows.Scan(&creds, &extra, &schedulable))
	require.NoError(t, rows.Err())
	return creds, extra, schedulable
}

// 抹凭证这一条 UPDATE 必须一次做完四件事，缺一件都留下一个可用的口子。
//
// 为什么必须在真库上测：整条语句的正确性全在 Postgres 才有的语义上——
// `'{}'::jsonb` 的转型、`extra || jsonb_build_object(...)` 的合并（而不是覆盖，
// 观察期那几个键得留着给人事后看）、`to_char(... AT TIME ZONE 'UTC')` 的时间格式。
// mock 里这些只是一个字符串，写错了照样"通过"。
func TestSupplierOnboarding_ScrubClearsCredentialsAndMarksDetached(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "detach-owner")
	stamp := time.Now().UnixNano()
	account := mustCreateAccount(t, client, &service.Account{
		Name: fmt.Sprintf("detach-acct-%d", stamp),
		Credentials: map[string]any{
			"access_token":  "at-secret",
			"refresh_token": "rt-secret",
			"account_uuid":  fmt.Sprintf("detach-uuid-%d", stamp),
			"email_address": fmt.Sprintf("detach-%d@example.com", stamp),
		},
		Extra: map[string]any{
			service.SupplyStateExtraKey:       service.SupplyStateActive,
			service.SupplyProbePassesExtraKey: 3,
		},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))
	setAccountSchedulable(t, client, account.ID, true)

	require.NoError(t, repo.ScrubAccountCredentials(txCtx, account.ID, ownerID))

	creds, extra, schedulable := readAccountRow(t, txCtx, client, account.ID)
	require.Equal(t, "{}", creds, "凭证必须整个消失，不是「清掉几个字段」")
	require.NotContains(t, creds, "rt-secret")
	require.False(t, schedulable, "凭证没了却还标着可调度，一秒钟都不能存在")
	require.Contains(t, extra, service.SupplyStateRetired)
	require.Contains(t, extra, service.SupplyDetachedAtExtraKey)
	// 合并而不是覆盖：观察期留下的痕迹要留着给人事后看。
	require.Contains(t, extra, service.SupplyProbePassesExtraKey)
}

// 抹凭证只能抹自己的号。
//
// 这条 WHERE 是防 TOCTOU 的最后一道：上层查过归属，但那次查与这次写之间隔着
// 一次网络往返。没有它，归属在那期间被改掉就会抹错人的凭证。
func TestSupplierOnboarding_ScrubRefusesForeignAndDeletedAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "detach-mine")
	strangerID := mustCreateSupplier(t, client, "detach-stranger")
	stamp := time.Now().UnixNano()
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("detach-guard-%d", stamp),
		Credentials: map[string]any{"refresh_token": "rt-secret"},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))

	err := repo.ScrubAccountCredentials(txCtx, account.ID, strangerID)
	require.ErrorIs(t, err, service.ErrSupplierAccountNotFound)
	creds, _, _ := readAccountRow(t, txCtx, client, account.ID)
	require.Contains(t, creds, "rt-secret", "别人的凭证一个字节都不该被动到")

	// 已经软删的行也拒绝：那条路上凭证早该被抹过了，再抹一次说明调用方的顺序反了。
	_, execErr := client.ExecContext(txCtx,
		"UPDATE accounts SET deleted_at = NOW() WHERE id = $1", account.ID)
	require.NoError(t, execErr)
	require.ErrorIs(t, repo.ScrubAccountCredentials(txCtx, account.ID, ownerID),
		service.ErrSupplierAccountNotFound)
}

// 解绑之后，同一份上游订阅必须能重新挂回来。
//
// 这条性质是两件事叠出来的，两件都容易在实现时漏掉：查重语句带 `deleted_at IS NULL`
// （所以软删的行不再占着身份），以及凭证被抹掉（身份键本身也没了）。任一件缺失，
// 供给者一旦解绑就会被永久挡在门外，且错误信息是「这个号已经连过了」——
// 一个既说不通又无法自助解决的死局。
func TestSupplierOnboarding_DetachedSubscriptionCanBeConnectedAgain(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "detach-rebind")
	stamp := time.Now().UnixNano()
	accountUUID := fmt.Sprintf("rebind-uuid-%d", stamp)
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("rebind-acct-%d", stamp),
		Credentials: map[string]any{"account_uuid": accountUUID},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))

	found, err := repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, accountUUID)
	require.NoError(t, err)
	require.Equal(t, account.ID, found, "挂着的时候当然占着这个身份")

	require.NoError(t, repo.ScrubAccountCredentials(txCtx, account.ID, ownerID))
	_, execErr := client.ExecContext(txCtx,
		"UPDATE accounts SET deleted_at = NOW() WHERE id = $1", account.ID)
	require.NoError(t, execErr)

	found, err = repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, accountUUID)
	require.NoError(t, err)
	require.Zero(t, found, "解绑之后必须能重新挂上来")
}

// 同一个上游订阅不能被挂两次——不论第二次是谁提交的。
// 这条是「一号两卖」的唯一拦截点：两边都会按同一份额度计分成。
func TestSupplierOnboarding_UpstreamUUIDLookupIgnoresOwnerAndSchedulable(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "uuid-owner")
	accountUUID := fmt.Sprintf("upstream-uuid-%d", time.Now().UnixNano())
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("uuid-acct-%d", time.Now().UnixNano()),
		Credentials: map[string]any{"account_uuid": accountUUID},
	})
	require.NoError(t, repo.SetAccountOwner(txCtx, account.ID, ownerID))
	// 停用它：一个挂着但停用的号仍然占着这个 uuid，查重不看 schedulable。
	// （fixture 会把 Schedulable 兜底成 true，所以在这里显式改。）
	_, err := client.ExecContext(txCtx, `UPDATE accounts SET schedulable = false WHERE id = $1`, account.ID)
	require.NoError(t, err)

	found, err := repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, accountUUID)
	require.NoError(t, err)
	require.Equal(t, account.ID, found)

	// 平台不同不算同一个号。
	found, err = repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformOpenAI, service.SupplierIdentityAccountUUID, accountUUID)
	require.NoError(t, err)
	require.Zero(t, found)

	// 从未见过的 uuid 返回零值而不是错误——调用方据此判定「可以挂」。
	found, err = repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, "never-seen")
	require.NoError(t, err)
	require.Zero(t, found)
}

// 邮箱是次级身份键，用在上游没吐 uuid 的那些授权上。
//
// 大小写不敏感必须在真库上证：上游对 Foo@x.com 与 foo@x.com 是同一个账号，
// 按字节比的话，改一下大小写就能把同一份订阅再挂一遍、再领一份分成。
func TestSupplierOnboarding_EmailIdentityLookupIsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("Supplier-%d@Example.COM", stamp)
	// 刻意只写邮箱、不写 uuid：这正是需要次级键的那种账号。
	account := mustCreateAccount(t, client, &service.Account{
		Name:        fmt.Sprintf("email-acct-%d", stamp),
		Credentials: map[string]any{"email_address": email},
	})

	found, err := repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityEmailAddress, strings.ToLower(email))
	require.NoError(t, err)
	require.Equal(t, account.ID, found, "改大小写不能绕过查重")

	// 用 uuid 键去查这个只有邮箱的号，查不到——两个键各查各的。
	found, err = repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, email)
	require.NoError(t, err)
	require.Zero(t, found)
}

// 未知的身份键必须报错，而不是查一个恒假的条件后返回"没找到"。
// 后者会把「加了新键但忘了加语句」变成一个静默放行的闸门。
func TestSupplierOnboarding_UnknownIdentityKeyIsAnError(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	repo := NewSupplierOnboardingRepository(tx.Client())

	_, err := repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityKey("org_uuid"), "org-1")
	require.Error(t, err)

	// 空值提前返回，不打库——调用方本来就不该拿空值来查。
	found, err := repo.FindAccountIDByUpstreamIdentity(txCtx,
		service.PlatformAnthropic, service.SupplierIdentityAccountUUID, "   ")
	require.NoError(t, err)
	require.Zero(t, found)
}

// ============================================================================
// 协议同意（迁移 228）
// ============================================================================

// 重复点同意保留**最早**那一行。
//
// 这条性质整个落在 `ON CONFLICT (user_id, version) DO NOTHING` 上，而那个冲突目标
// 是一个唯一索引——索引没建出来的话，第二次点同意会插进第二行，而"他什么时候
// 同意的"从此有两个答案。这只有真库能证。
func TestSupplierAgreement_AcceptanceIsIdempotentAndKeepsEarliest(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "agreement-idem")
	first := &service.SupplierAgreementAcceptance{
		UserID: userID, Version: "v1", IP: "203.0.113.9", UserAgent: "ua-1",
	}
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx, first))

	stored, err := repo.FindAgreementAcceptance(txCtx, userID, "v1")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, "203.0.113.9", stored.IP)
	require.Equal(t, "ua-1", stored.UserAgent)
	require.False(t, stored.AcceptedAt.IsZero())

	// 第二次点，换了 IP 和 UA：什么都不该改。
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx, &service.SupplierAgreementAcceptance{
		UserID: userID, Version: "v1", IP: "198.51.100.7", UserAgent: "ua-2",
	}))

	again, err := repo.FindAgreementAcceptance(txCtx, userID, "v1")
	require.NoError(t, err)
	require.NotNil(t, again)
	require.Equal(t, "203.0.113.9", again.IP, "同意记录是证据，后来的一次点击不该改写它")
	require.Equal(t, "ua-1", again.UserAgent)
	require.Equal(t, stored.AcceptedAt.UnixNano(), again.AcceptedAt.UnixNano())

	rows, err := client.QueryContext(txCtx,
		`SELECT COUNT(*) FROM supplier_agreement_acceptances WHERE user_id = $1 AND version = $2`,
		userID, "v1")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.Equal(t, 1, count, "同一版本只该有一行")
}

// 精确版本查询与"最近一次同意"是两个问题：门禁问前者，界面问后者。
func TestSupplierAgreement_ExactVersionAndLatestAreDifferentQuestions(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "agreement-latest")
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx,
		&service.SupplierAgreementAcceptance{UserID: userID, Version: "v1"}))
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx,
		&service.SupplierAgreementAcceptance{UserID: userID, Version: "v2"}))

	// 没同意过的版本查不到——门禁据此拒绝。
	missing, err := repo.FindAgreementAcceptance(txCtx, userID, "v3")
	require.NoError(t, err)
	require.Nil(t, missing, "没同意过就必须查不到，而不是回一条空记录")

	// 同一个事务里两行的 accepted_at 是同一个 NOW()，所以排序还得靠 id 兜底——
	// 这正是 ORDER BY accepted_at DESC, id DESC 里第二个键存在的理由。
	latest, err := repo.LatestAgreementAcceptance(txCtx, userID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, "v2", latest.Version)
}

// 同意记录按人隔离：查别人的同意记录必须查不到，否则一个人签字就能放行全站。
func TestSupplierAgreement_AcceptanceIsScopedByUser(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	signer := mustCreateSupplier(t, client, "agreement-signer")
	bystander := mustCreateSupplier(t, client, "agreement-bystander")
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx,
		&service.SupplierAgreementAcceptance{UserID: signer, Version: "v1"}))

	found, err := repo.FindAgreementAcceptance(txCtx, bystander, "v1")
	require.NoError(t, err)
	require.Nil(t, found)

	latest, err := repo.LatestAgreementAcceptance(txCtx, bystander)
	require.NoError(t, err)
	require.Nil(t, latest, "从没同意过的人不该有「最近一次同意」")
}

// IP/UA 是可空的旁证：不给也能记下同意本身。
func TestSupplierAgreement_AcceptanceWithoutEvidenceFieldsIsStillValid(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	userID := mustCreateSupplier(t, client, "agreement-noevidence")
	require.NoError(t, repo.RecordAgreementAcceptance(txCtx,
		&service.SupplierAgreementAcceptance{UserID: userID, Version: "v1"}))

	stored, err := repo.FindAgreementAcceptance(txCtx, userID, "v1")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Empty(t, stored.IP)
	require.Empty(t, stored.UserAgent)
}

// ============================================================================
// 接入数量上限（迁移 230）
// ============================================================================

// softDeleteAccount 直接改库把号软删掉。
//
// 与 softDeleteUser 同一个理由：这两条 COUNT 要证的正是「解绑之后位置腾出来了」，
// 而本仓的解绑就是软删。走 ent 的 Delete 会被 soft-delete mixin 转成同样的 UPDATE，
// 但那样就看不出测试到底在造什么状态。
func softDeleteAccount(t *testing.T, client *dbent.Client, accountID int64) {
	t.Helper()
	_, err := client.ExecContext(context.Background(),
		"UPDATE accounts SET deleted_at = NOW() WHERE id = $1", accountID)
	require.NoError(t, err)
}

// 每人上限数的是「当下还在的号」，不是历史累计。
//
// 为什么必须在真库上测：整条性质就落在 `deleted_at IS NULL` 这半行上，而本仓的
// 解绑是软删——行还在表里。少了这半行，一个正常换号的供给者会在解绑几次之后
// 永久地耗尽自己的额度，且他名下一个号都看不到。mock 里没有软删这回事。
func TestSupplierOnboarding_OwnerCountExcludesDeletedAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "count-owner")
	otherID := mustCreateSupplier(t, client, "count-other")
	stamp := time.Now().UnixNano()

	count, err := repo.CountAccountsByOwner(txCtx, ownerID)
	require.NoError(t, err)
	require.Zero(t, count, "一个号都没挂的人必须数出 0，而不是报「没有行」")

	kept := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("kept-%d", stamp)})
	gone := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("gone-%d", stamp)})
	theirs := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("theirs-%d", stamp)})
	orphan := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("orphan-%d", stamp)})
	require.NoError(t, repo.SetAccountOwner(txCtx, kept.ID, ownerID))
	require.NoError(t, repo.SetAccountOwner(txCtx, gone.ID, ownerID))
	require.NoError(t, repo.SetAccountOwner(txCtx, theirs.ID, otherID))
	_ = orphan // 自营号（owner_user_id IS NULL），不该算在任何人头上

	count, err = repo.CountAccountsByOwner(txCtx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	softDeleteAccount(t, client, gone.ID)

	count, err = repo.CountAccountsByOwner(txCtx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 1, count, "解绑必须真的腾出一个位置")

	count, err = repo.CountAccountsByOwner(txCtx, otherID)
	require.NoError(t, err)
	require.Equal(t, 1, count, "别人的号不该算进来")
}

// 接入来源写一次就定了：同一个号重复记不会把它在那个 IP 上数成两个。
//
// 为什么必须在真库上测：幂等整个落在 `ON CONFLICT (account_id) DO NOTHING` 上，
// 而那依赖 account_id 真的是主键（迁移 230 建的）。约束写掉了在 mock 里静默通过，
// 在这里会当场变成「同一个号数两次」——每 IP 那道闸于是会凭空提前拦人。
func TestSupplierOnboarding_AccountOriginIsWriteOnce(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "origin-owner")
	stamp := time.Now().UnixNano()
	acct := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("origin-%d", stamp)})
	require.NoError(t, repo.SetAccountOwner(txCtx, acct.ID, ownerID))

	ip := fmt.Sprintf("198.51.100.%d", stamp%200+1)
	require.NoError(t, repo.RecordAccountOrigin(txCtx, acct.ID, ownerID, ip))
	require.NoError(t, repo.RecordAccountOrigin(txCtx, acct.ID, ownerID, ip))
	// 换个 IP 再记一次也不该改写第一次的记录：那一行是「它当初从哪来」的证据，
	// 不是一个会跟着人走的属性。
	require.NoError(t, repo.RecordAccountOrigin(txCtx, acct.ID, ownerID, "203.0.113.200"))

	count, err := repo.CountAccountsByOriginIP(txCtx, ip)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	count, err = repo.CountAccountsByOriginIP(txCtx, "203.0.113.200")
	require.NoError(t, err)
	require.Zero(t, count, "第一次写下的来源不该被后来的重复调用改掉")
}

// 每 IP 上限同样只数「还在的号」，且从没记过来源的 IP 数出 0 而不是报错。
//
// 后半句不是琐碎的：这两条 COUNT 一旦把「没有行」当成错误，requireCapacity 会
// 把它当成「判不了闸」而拒绝——于是这道闸会拦下**每一个来自陌生 IP 的人**，
// 也就是几乎所有人。
func TestSupplierOnboarding_OriginIPCountExcludesDeletedAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	stamp := time.Now().UnixNano()
	ip := fmt.Sprintf("192.0.2.%d", stamp%200+1)

	count, err := repo.CountAccountsByOriginIP(txCtx, ip)
	require.NoError(t, err)
	require.Zero(t, count, "陌生 IP 必须数出 0，绝不能是错误")

	// 同一个 IP 上的两个号分属两个人——每 IP 这道闸的意义就在于跨用户地数，
	// 否则"再注册一个号"就能绕过。
	alice := mustCreateSupplier(t, client, "origin-alice")
	bob := mustCreateSupplier(t, client, "origin-bob")
	a1 := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("a1-%d", stamp)})
	b1 := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("b1-%d", stamp)})
	require.NoError(t, repo.SetAccountOwner(txCtx, a1.ID, alice))
	require.NoError(t, repo.SetAccountOwner(txCtx, b1.ID, bob))
	require.NoError(t, repo.RecordAccountOrigin(txCtx, a1.ID, alice, ip))
	require.NoError(t, repo.RecordAccountOrigin(txCtx, b1.ID, bob, ip))

	count, err = repo.CountAccountsByOriginIP(txCtx, ip)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	softDeleteAccount(t, client, b1.ID)

	// 号没了，origins 里那一行还在（迁移 230 刻意没建外键——软删让级联永不触发）。
	// 这条 JOIN 就是为此存在的：不 JOIN 的话，一个 IP 上的额度会被历史上删掉的号
	// 永久占着。
	count, err = repo.CountAccountsByOriginIP(txCtx, ip)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// 空 IP 数出 0 且不发查询。
//
// 拿 "" 当一个"网络"去数，会把所有取不到 IP 的请求归到同一个虚构来源里
// 互相挤占额度——被拦下的会是一群彼此毫无关系的人，且无从自证。
func TestSupplierOnboarding_OriginIPCountIgnoresEmptyIP(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	ownerID := mustCreateSupplier(t, client, "origin-empty")
	stamp := time.Now().UnixNano()
	acct := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("empty-%d", stamp)})
	require.NoError(t, repo.SetAccountOwner(txCtx, acct.ID, ownerID))

	// 空 IP 不写行：写进去的话，它会和别人的空 IP 记录挤在同一个键上。
	require.NoError(t, repo.RecordAccountOrigin(txCtx, acct.ID, ownerID, "   "))

	// 直接查表，不经过 CountAccountsByOriginIP：那个方法对空 IP 是短路返回 0 的，
	// 拿它来证「没写行」等于用一个短路证明另一个短路。
	rows, err := client.QueryContext(context.Background(),
		"SELECT COUNT(*) FROM supplier_account_origins WHERE account_id = $1", acct.ID)
	require.NoError(t, err)
	var rowsForAccount int
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&rowsForAccount))
	require.NoError(t, rows.Close())
	require.Zero(t, rowsForAccount, "拿不到 IP 时不该留下一行空来源")

	count, err := repo.CountAccountsByOriginIP(txCtx, "")
	require.NoError(t, err)
	require.Zero(t, count)

	count, err = repo.CountAccountsByOriginIP(txCtx, "   ")
	require.NoError(t, err)
	require.Zero(t, count)
}

// 中转查重（M7）：键是 (base_url, api_key) 组合，两者各差一个字符都算另一份供给。
//
// 只有真库能证的点在 jsonb：credentials->>'base_url' 这条路径对「键不存在」
// 「值是 null」「值是数字」的行为，sqlmock 里全是我以为的行为。
func TestSupplierOnboarding_FindAccountIDByRelayEndpoint(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	repo := NewSupplierOnboardingRepository(client)

	stamp := time.Now().UnixNano()
	existing := mustCreateAccount(t, client, &service.Account{
		Name:     fmt.Sprintf("relay-%d", stamp),
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://relay.example.com/api",
			"api_key":  "sk-relay-abc",
		},
	})
	// 一个没有这两个键的 OAuth 号：jsonb 路径取不到值时必须是「不命中」，不是报错。
	mustCreateAccount(t, client, &service.Account{
		Name:     fmt.Sprintf("oauth-%d", stamp),
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeSetupToken,
		Credentials: map[string]any{
			"access_token": "tok",
		},
	})

	t.Run("组合完全一致才命中", func(t *testing.T) {
		id, err := repo.FindAccountIDByRelayEndpoint(txCtx, service.PlatformAnthropic,
			"https://relay.example.com/api", "sk-relay-abc")
		require.NoError(t, err)
		assert.Equal(t, existing.ID, id)
	})
	t.Run("同端点不同 key 不命中", func(t *testing.T) {
		id, err := repo.FindAccountIDByRelayEndpoint(txCtx, service.PlatformAnthropic,
			"https://relay.example.com/api", "sk-relay-DIFFERENT")
		require.NoError(t, err)
		assert.Zero(t, id, "同一个端点配两把 key 是两份供给（限速分片），合并算一份会漏掉真复用")
	})
	t.Run("同 key 不同端点不命中", func(t *testing.T) {
		id, err := repo.FindAccountIDByRelayEndpoint(txCtx, service.PlatformAnthropic,
			"https://other.example.com", "sk-relay-abc")
		require.NoError(t, err)
		assert.Zero(t, id)
	})
	t.Run("空参不查库直接回零", func(t *testing.T) {
		id, err := repo.FindAccountIDByRelayEndpoint(txCtx, service.PlatformAnthropic, "", "")
		require.NoError(t, err)
		assert.Zero(t, id)
	})
}
