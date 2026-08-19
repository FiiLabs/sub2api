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

	claimed, err := repo.ClaimSession(txCtx, session.SessionID, userID)
	require.NoError(t, err)
	require.Equal(t, session.SessionID, claimed.SessionID)
	require.Equal(t, userID, claimed.UserID)
	require.Equal(t, session.Platform, claimed.Platform)
	require.Equal(t, session.State, claimed.State)
	// code_verifier 必须原样取回：PKCE 换 token 时少一个字符就是一次彻底失败的授权。
	require.Equal(t, session.CodeVerifier, claimed.CodeVerifier)
	require.Equal(t, session.Scope, claimed.Scope)

	// 第二次领取拿不到——重放不会再建一个账号。
	_, err = repo.ClaimSession(txCtx, session.SessionID, userID)
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

	_, err := repo.ClaimSession(txCtx, session.SessionID, attackerID)
	require.ErrorIs(t, err, service.ErrSupplierOAuthSessionInvalid)

	// 失败的领取不能顺手把会话标记成已消费，否则就是一条免费的拒绝服务。
	claimed, err := repo.ClaimSession(txCtx, session.SessionID, ownerID)
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

	_, err := repo.ClaimSession(txCtx, expired.SessionID, userID)
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
	claimed, err := repo.ClaimSession(txCtx, live.SessionID, userID)
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
	_, err := repo.ClaimSession(txCtx, session.SessionID, userID)
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
