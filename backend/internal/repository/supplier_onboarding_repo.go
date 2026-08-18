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

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierOnboardingSessionCleanupDefaultLimit 单轮清理最多删多少条过期会话。
const supplierOnboardingSessionCleanupDefaultLimit = 1000

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

// supplierAccountFindByUpstreamUUIDSQL 按上游账号 uuid 找已存在的账号。
//
// 不限 owner：同一个上游订阅被**另一个供给者**提交过，同样必须拒绝——那正是
// 「一号两卖」的形态，两边都会按同一份额度计分成。也不限 schedulable：
// 一个挂着但停用的号仍然占着这个 uuid。
const supplierAccountFindByUpstreamUUIDSQL = `
SELECT id
FROM accounts
WHERE deleted_at IS NULL
  AND platform = $1
  AND credentials->>'account_uuid' = $2
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

func (r *supplierOnboardingRepository) FindAccountIDByUpstreamUUID(ctx context.Context, platform, accountUUID string) (int64, error) {
	if accountUUID == "" {
		return 0, nil
	}
	rows, err := r.client.QueryContext(ctx, supplierAccountFindByUpstreamUUIDSQL, platform, accountUUID)
	if err != nil {
		return 0, fmt.Errorf("find account by upstream uuid: %w", err)
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
