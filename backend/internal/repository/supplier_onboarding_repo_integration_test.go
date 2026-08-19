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

	found, err := repo.FindAccountIDByUpstreamUUID(txCtx, service.PlatformAnthropic, accountUUID)
	require.NoError(t, err)
	require.Equal(t, account.ID, found)

	// 平台不同不算同一个号。
	found, err = repo.FindAccountIDByUpstreamUUID(txCtx, service.PlatformOpenAI, accountUUID)
	require.NoError(t, err)
	require.Zero(t, found)

	// 从未见过的 uuid 返回零值而不是错误——调用方据此判定「可以挂」。
	found, err = repo.FindAccountIDByUpstreamUUID(txCtx, service.PlatformAnthropic, "never-seen")
	require.NoError(t, err)
	require.Zero(t, found)
}
