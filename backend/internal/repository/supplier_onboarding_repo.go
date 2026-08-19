// APEXONE-EXT: 双边市场——供给者自助接入的 SQL 实现。
//
// 两类数据：
//  1. supplier_oauth_sessions（迁移 226）——扩展层新表，不进 ent schema。
//  2. accounts.owner_user_id——ent 里有字段，但刻意没暴露到 service.Account 上，
//     所以归属的读写都在这里用 raw SQL 完成（理由见 service/supplier_onboarding.go）。
package repository

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierOnboardingSessionCleanupDefaultLimit 单轮清理最多删多少条过期会话。
const supplierOnboardingSessionCleanupDefaultLimit = 1000

// supplierOnboardingStateScanDefaultLimit 单轮按状态扫描最多取多少个账号。
//
// 有上限是因为观察期任务每扫到一个账号就可能打一次上游探测；一轮扫出几千个号、
// 同时探测，烧的是**供给者自己**的额度。宁可分几轮扫完。
const supplierOnboardingStateScanDefaultLimit = 200

type supplierOnboardingRepository struct {
	client *dbent.Client
}

// NewSupplierOnboardingRepository 构造自助接入仓储。
func NewSupplierOnboardingRepository(client *dbent.Client) service.SupplierOnboardingRepository {
	return &supplierOnboardingRepository{client: client}
}

const supplierOAuthSessionInsertSQL = `
INSERT INTO supplier_oauth_sessions (
    session_id, user_id, platform, state, code_verifier, scope, expires_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())`

// supplierOAuthSessionClaimSQL 原子领取一条会话。
//
// 三个 WHERE 条件（归属人、未消费、未过期）和「打上消费标记」必须在同一条语句里完成，
// 否则并发重放会让两个请求都通过检查、同一个授权码被兑换两次、建出两个账号。
// 拿不到行就是拿不到，调用方不需要知道是三个条件里的哪一个没满足。
const supplierOAuthSessionClaimSQL = `
UPDATE supplier_oauth_sessions
SET consumed_at = NOW(), updated_at = NOW()
WHERE session_id = $1
  AND user_id = $2
  AND consumed_at IS NULL
  AND expires_at > NOW()
RETURNING session_id, user_id, platform, state, code_verifier, scope, expires_at, created_at`

const supplierOAuthSessionCountPendingSQL = `
SELECT COUNT(*)
FROM supplier_oauth_sessions
WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > NOW()`

// supplierOAuthSessionDeleteExpiredSQL 只删「过期且从未被消费」的行。
//
// 已消费的行留着：它们是「这个账号是谁在什么时候挂上来的」的唯一证据，
// 出了归属纠纷要靠它对账。留存清理是独立的一刀，不混在过期清理里。
const supplierOAuthSessionDeleteExpiredSQL = `
DELETE FROM supplier_oauth_sessions
WHERE id IN (
    SELECT id FROM supplier_oauth_sessions
    WHERE consumed_at IS NULL AND expires_at <= NOW()
    ORDER BY id
    LIMIT $1
)`

func (r *supplierOnboardingRepository) CreateSession(ctx context.Context, session *service.SupplierOAuthSession) error {
	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}
	_, err := r.client.ExecContext(ctx, supplierOAuthSessionInsertSQL,
		session.SessionID, session.UserID, session.Platform,
		session.State, session.CodeVerifier, session.Scope, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert supplier oauth session: %w", err)
	}
	return nil
}

func (r *supplierOnboardingRepository) ClaimSession(ctx context.Context, sessionID string, userID int64) (*service.SupplierOAuthSession, error) {
	rows, err := r.client.QueryContext(ctx, supplierOAuthSessionClaimSQL, sessionID, userID)
	if err != nil {
		return nil, fmt.Errorf("claim supplier oauth session: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("claim supplier oauth session: %w", err)
		}
		return nil, service.ErrSupplierOAuthSessionInvalid
	}

	var session service.SupplierOAuthSession
	if err := rows.Scan(
		&session.SessionID, &session.UserID, &session.Platform,
		&session.State, &session.CodeVerifier, &session.Scope,
		&session.ExpiresAt, &session.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan supplier oauth session: %w", err)
	}
	return &session, rows.Err()
}

func (r *supplierOnboardingRepository) CountPendingSessions(ctx context.Context, userID int64) (int, error) {
	rows, err := r.client.QueryContext(ctx, supplierOAuthSessionCountPendingSQL, userID)
	if err != nil {
		return 0, fmt.Errorf("count pending supplier oauth sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("scan pending session count: %w", err)
		}
	}
	return count, rows.Err()
}

func (r *supplierOnboardingRepository) DeleteExpiredSessions(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = supplierOnboardingSessionCleanupDefaultLimit
	}
	result, err := r.client.ExecContext(ctx, supplierOAuthSessionDeleteExpiredSQL, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired supplier oauth sessions: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}

// supplierAccountSetOwnerSQL 写归属。
//
// `AND owner_user_id IS NULL` 让这条语句只能把自营账号变成供给账号，永远不能把
// 一个已经属于 A 的账号改成属于 B。归属是钱的去向，改归属必须是一次显式的运营动作，
// 不能是某条接入路径的副作用。
const supplierAccountSetOwnerSQL = `
UPDATE accounts
SET owner_user_id = $2, updated_at = NOW()
WHERE id = $1 AND owner_user_id IS NULL AND deleted_at IS NULL`

const supplierAccountGetOwnerSQL = `
SELECT COALESCE(owner_user_id, 0)
FROM accounts
WHERE id = $1 AND deleted_at IS NULL`

const supplierAccountListByOwnerSQL = `
SELECT id
FROM accounts
WHERE owner_user_id = $1 AND deleted_at IS NULL
ORDER BY id DESC`

// supplierAccountListByStateSQL 按接入状态列出供给账号。
//
// extra 的键名与「状态缺失时算 pending_review」这条兜底规则都从 service 侧的常量
// 拼进来，而不是在 SQL 里再写一遍字面量：这两处一旦漂移，症状是观察期任务扫不到
// 任何账号——一个完全静默的失效。拼接的是包内常量，不是任何外部输入。
var supplierAccountListByStateSQL = fmt.Sprintf(`
SELECT id
FROM accounts
WHERE deleted_at IS NULL
  AND owner_user_id IS NOT NULL
  AND COALESCE(NULLIF(extra->>'%s', ''), '%s') = $1
ORDER BY id
LIMIT $2`, service.SupplyStateExtraKey, service.SupplyStatePendingReview)

// supplierAccountListOrphanedSQL 列出「归属人已经不可用、号却还在供货」的账号。
//
// 不可用的两种形态都必须查，而且都不体现在 accounts 行上：
//   - `u.deleted_at IS NOT NULL`：用户注销。本仓的销号是**软删**（user_repo.deleteUser
//     只清认证身份再走 mixin 的软删），所以 accounts.owner_user_id 上那条
//     `ON DELETE SET NULL` 一次也不会触发——这正是"注销后号还在跑"的成因。
//   - `u.status <> 'active'`：用户被停用（含内容风控自动封禁）。
//
// 最后一个条件是**降噪**，不是判据：已经 retired 且不可调度的号无事可做，再返回它们
// 只会让每一轮扫描重复处理同一批历史行。两个条件用 OR 而不是 AND——一个
// state=retired 却仍然 schedulable 的号是最危险的那种（它在接单），必须被扫到。
//
// 状态字面量与 extra 键名从 service 常量拼进来，理由同 supplierAccountListByStateSQL：
// 漂移了不会报错，只会让这条闸静默失效。拼的全是包内常量，不是外部输入。
var supplierAccountListOrphanedSQL = fmt.Sprintf(`
SELECT a.id
FROM accounts a
JOIN users u ON u.id = a.owner_user_id
WHERE a.deleted_at IS NULL
  AND a.owner_user_id IS NOT NULL
  AND (u.deleted_at IS NOT NULL OR u.status <> '%s')
  AND (
        a.schedulable = TRUE
        OR COALESCE(NULLIF(a.extra->>'%s', ''), '%s') <> '%s'
      )
ORDER BY a.id
LIMIT $1`,
	domain.StatusActive,
	service.SupplyStateExtraKey, service.SupplyStatePendingReview, service.SupplyStateRetired)

// supplierAccountScrubCredentialsSQL 抹掉凭证并把行标成已解绑。
//
// `credentials = '{}'` 而不是 NULL：这一列在 ent 里是非空 JSON，写 NULL 会让任何
// 还在读它的代码路径拿到一个 nil map 之外的东西（扫描期的 sql.Null 处理、
// mapper 里的类型断言），空对象则处处都能安全地读成"没有凭证"。
//
// 三个 WHERE 条件里 `owner_user_id = $2` 是要紧的那个：它让这条语句在任何调用点上
// 都只能抹掉调用者自己的号。上层已经查过归属了，这里再查一遍是因为两次之间隔着
// 一次网络往返——归属在那期间变了，没有这个条件就会抹错人。
//
// schedulable 也在这里再压一次。上层已经先调过 SetSchedulable（那条路还会清调度
// 快照、发事件，这条 SQL 做不到，所以不能省），这里只是保证「凭证没了但还标着
// 可调度」这个状态一秒钟都不存在。
//
// 状态字面量与 extra 键名从 service 常量拼进来，同本文件其它语句。
var supplierAccountScrubCredentialsSQL = fmt.Sprintf(`
UPDATE accounts
SET credentials = '{}'::jsonb,
    extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
        '%s', '%s',
        '%s', to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
    ),
    schedulable = FALSE,
    updated_at = NOW()
WHERE id = $1
  AND owner_user_id = $2
  AND deleted_at IS NULL`,
	service.SupplyStateExtraKey, service.SupplyStateRetired,
	service.SupplyDetachedAtExtraKey)

// 按上游身份键找已存在的账号。每个键一条写死的语句，**键名不拼进 SQL**。
//
// 为什么不写成一条 `credentials->>$2 = $3` 的通用语句：那样键名就成了运行期数据，
// 一个拼错的键会静默地永远查不到任何行——查重于是变成一个恒真的放行闸，而症状
// （重复的号悄悄进池）要到对账时才看得见。写死成几条，键名错了是编译期的事。
//
// 三条共同约束：
//   - **不限 owner**：同一个上游订阅被另一个供给者提交过同样必须拒绝——那正是
//     「一号两卖」的形态，两边都会按同一份额度计分成。
//   - **不限 schedulable**：一个挂着但停用的号仍然占着这个身份。
//   - **不限接入状态**：已下线（retired）的号也算占着——它随时能被主人重新挂回来。
const supplierAccountFindByAccountUUIDSQL = `
SELECT id
FROM accounts
WHERE deleted_at IS NULL
  AND platform = $1
  AND credentials->>'account_uuid' = $2
LIMIT 1`

// 邮箱比对大小写不敏感：上游对 Foo@x.com 与 foo@x.com 是同一个账号，
// 按字节比会让改一下大小写就能把同一份订阅再挂一遍。
const supplierAccountFindByEmailSQL = `
SELECT id
FROM accounts
WHERE deleted_at IS NULL
  AND platform = $1
  AND LOWER(credentials->>'email_address') = LOWER($2)
LIMIT 1`

func (r *supplierOnboardingRepository) SetAccountOwner(ctx context.Context, accountID int64, userID int64) error {
	result, err := r.client.ExecContext(ctx, supplierAccountSetOwnerSQL, accountID, userID)
	if err != nil {
		return fmt.Errorf("set account owner: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		// 驱动不报行数时不能假定失败——语句本身已经成功了。
		return nil
	}
	if affected == 0 {
		return fmt.Errorf("account %d is missing or already owned", accountID)
	}
	return nil
}

func (r *supplierOnboardingRepository) ScrubAccountCredentials(ctx context.Context, accountID int64, userID int64) error {
	result, err := r.client.ExecContext(ctx, supplierAccountScrubCredentialsSQL, accountID, userID)
	if err != nil {
		return fmt.Errorf("scrub supply account credentials: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		// 驱动不报行数——语句本身已经成功了，不能倒过来当成没抹掉。
		return nil
	}
	if affected == 0 {
		// 号不存在、不是他的、或已经被删。三种情况对调用方是同一个回答，
		// 理由同 ErrSupplierAccountNotFound 本身。
		return service.ErrSupplierAccountNotFound
	}
	return nil
}

func (r *supplierOnboardingRepository) GetAccountOwner(ctx context.Context, accountID int64) (int64, error) {
	rows, err := r.client.QueryContext(ctx, supplierAccountGetOwnerSQL, accountID)
	if err != nil {
		return 0, fmt.Errorf("get account owner: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("get account owner: %w", err)
		}
		return 0, service.ErrSupplierAccountNotFound
	}
	var ownerID int64
	if err := rows.Scan(&ownerID); err != nil {
		return 0, fmt.Errorf("scan account owner: %w", err)
	}
	return ownerID, rows.Err()
}

func (r *supplierOnboardingRepository) ListAccountIDsByOwner(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := r.client.QueryContext(ctx, supplierAccountListByOwnerSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list accounts by owner: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan owned account id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *supplierOnboardingRepository) ListAccountIDsBySupplyState(ctx context.Context, state string, limit int) ([]int64, error) {
	if state == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = supplierOnboardingStateScanDefaultLimit
	}
	rows, err := r.client.QueryContext(ctx, supplierAccountListByStateSQL, state, limit)
	if err != nil {
		return nil, fmt.Errorf("list accounts by supply state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan supply state account id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *supplierOnboardingRepository) ListAccountIDsWithUnavailableOwner(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = supplierOnboardingStateScanDefaultLimit
	}
	rows, err := r.client.QueryContext(ctx, supplierAccountListOrphanedSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list accounts with unavailable owner: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan orphaned account id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// supplierIdentitySQL 把身份键翻译成对应的那条语句。
//
// 未知的键返回空串而不是随便挑一条：调用方据此报错，而不是拿一条不相干的语句
// 去查一个恒假的条件——后者会把「加了新键但忘了加语句」变成静默放行。
func supplierIdentitySQL(key service.SupplierIdentityKey) string {
	switch key {
	case service.SupplierIdentityAccountUUID:
		return supplierAccountFindByAccountUUIDSQL
	case service.SupplierIdentityEmailAddress:
		return supplierAccountFindByEmailSQL
	default:
		return ""
	}
}

func (r *supplierOnboardingRepository) FindAccountIDByUpstreamIdentity(ctx context.Context, platform string, key service.SupplierIdentityKey, value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	query := supplierIdentitySQL(key)
	if query == "" {
		return 0, fmt.Errorf("unsupported supplier identity key %q", key)
	}
	rows, err := r.client.QueryContext(ctx, query, platform, value)
	if err != nil {
		return 0, fmt.Errorf("find account by upstream identity %s: %w", key, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, rows.Err()
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return 0, fmt.Errorf("scan account id: %w", err)
	}
	return id, rows.Err()
}
