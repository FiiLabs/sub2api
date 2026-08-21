// APEXONE-EXT: 双边市场——链上收款地址绑定的 SQL 实现（迁移 234）。
//
// 这个文件比它的行数重，因为它守着两条约束，而两条都只有数据库能真正守住：
//
//  1. 每人每链一个地址。靠 uniq_supplier_payout_wallets_user_network + ON CONFLICT
//     做成幂等的换绑，而不是「先查有没有，没有就插」——后者在并发下会插出两行。
//  2. 一个地址只属于一个账号（反女巫 + 误绑保护）。靠
//     uniq_supplier_payout_wallets_network_hash。
//
// 第 2 条与「地址加密入库」直接冲突：GCM 每次带随机 nonce，同一个地址两次入库是两串
// 不同的密文，唯一索引建在密文上等于没建。解法是额外存一列
// address_hash = SHA-256(小写地址) 当盲索引（为什么这在这里安全，见迁移 234 的文件头）。
//
// 于是每一条写路径都必须同时写 address 与 address_hash，两者不同步的表现是
// 「换了地址但反女巫还认旧的」——所以哈希只在 payoutWalletHash 一处计算。
package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type supplierPayoutWalletRepository struct {
	client *dbent.Client
	cipher payoutAccountCipher
}

// NewSupplierPayoutWalletRepository 构造绑定表仓储。
//
// 复用 payoutAccountCipher 而不是新起一套：它加密的东西与 payout_account 是同一类
// （当事人换不掉的收款标识），密钥也该是同一把。多一套密钥管理只会多一个轮换时
// 会被漏掉的地方。
func NewSupplierPayoutWalletRepository(client *dbent.Client, encryptor service.SecretEncryptor) service.SupplierPayoutWalletRepository {
	return &supplierPayoutWalletRepository{
		client: client,
		cipher: payoutAccountCipher{encryptor: encryptor},
	}
}

// ============================================================================
// SQL 常量
// ============================================================================

// supplierPayoutWalletColumns 读路径共用的列清单，顺序与 scan 逐字对应。
const supplierPayoutWalletColumns = `
    id, user_id, network, address, created_at, updated_at`

const supplierPayoutWalletGetSQL = `
SELECT ` + supplierPayoutWalletColumns + `
FROM supplier_payout_wallets
WHERE user_id = $1 AND network = $2`

const supplierPayoutWalletListSQL = `
SELECT ` + supplierPayoutWalletColumns + `
FROM supplier_payout_wallets
WHERE user_id = $1
ORDER BY network`

// supplierPayoutWalletOwnerSQL 反查一个地址当前属于谁。
//
// 存在的意义只是**报错文案**：唯一索引已经能挡住冲突，但它抛出来的是
// "duplicate key value violates unique constraint ..."，那句话不能给用户看。
// 先查一次能让绝大多数冲突走到一句人话上；查不到而后面仍然撞索引的，
// 是真正的并发，由 23505 兜底。
const supplierPayoutWalletOwnerSQL = `
SELECT user_id FROM supplier_payout_wallets WHERE network = $1 AND address_hash = $2`

// supplierPayoutWalletUpsertSQL 绑定/换绑。
//
// ON CONFLICT 只推断 (user_id, network) 这一个索引——另一个唯一索引
// (network, address_hash) 上的冲突**必须**抛出来，那正是「地址是别人的」。
// 写成 ON CONFLICT DO NOTHING 会把两种冲突混成一种沉默。
const supplierPayoutWalletUpsertSQL = `
INSERT INTO supplier_payout_wallets (user_id, network, address, address_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (user_id, network) DO UPDATE
SET address      = EXCLUDED.address,
    address_hash = EXCLUDED.address_hash,
    updated_at   = NOW()
RETURNING ` + supplierPayoutWalletColumns

const supplierPayoutWalletDeleteSQL = `
DELETE FROM supplier_payout_wallets WHERE user_id = $1 AND network = $2 RETURNING id`

// ============================================================================
// 盲索引
// ============================================================================

// payoutWalletHash 算地址的盲索引。
//
// 输入必须已经是 NormalizeSupplierPayoutAddress 归一化过的小写地址。
// 这里再 ToLower 一次不是防御性冗余，而是因为唯一索引的语义完全取决于它：
// 只要有一条写路径喂进来一个大写地址，同一个地址就能在库里存在两份，
// 反女巫约束当场失效且没有任何报错。
func payoutWalletHash(address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(address))))
	return hex.EncodeToString(sum[:])
}

// ============================================================================
// 读
// ============================================================================

func (r *supplierPayoutWalletRepository) Get(ctx context.Context, userID int64, network string) (*service.SupplierPayoutWallet, error) {
	rows, err := r.client.QueryContext(ctx, supplierPayoutWalletGetSQL, userID, network)
	if err != nil {
		return nil, fmt.Errorf("query supplier payout wallet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		// 「还没绑」不是错误：提现表单要靠它决定画输入框还是画已绑地址。
		return nil, rows.Err()
	}
	var out service.SupplierPayoutWallet
	if err := r.cipher.scanSupplierPayoutWallet(rows, &out); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

func (r *supplierPayoutWalletRepository) List(ctx context.Context, userID int64) ([]service.SupplierPayoutWallet, error) {
	rows, err := r.client.QueryContext(ctx, supplierPayoutWalletListSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list supplier payout wallets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.SupplierPayoutWallet, 0, 2)
	for rows.Next() {
		var item service.SupplierPayoutWallet
		if err := r.cipher.scanSupplierPayoutWallet(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ============================================================================
// 写
// ============================================================================

// Upsert 绑定或换绑。
//
// 两道防线一前一后，缺一不可：
//   - 事务内先反查地址归属，为的是给用户一句能看懂的话；
//   - 23505 兜底，为的是并发下**真的**挡住。
//
// 只留前者会在并发时把驱动的原始报错吐给用户；只留后者则让最常见的那次冲突
// （用户抄了别人的地址）也只能得到一句 duplicate key。
func (r *supplierPayoutWalletRepository) Upsert(ctx context.Context, userID int64, network, address string) (*service.SupplierPayoutWallet, error) {
	normalized, err := service.NormalizeSupplierPayoutAddress(network, address)
	if err != nil {
		return nil, err
	}
	hash := payoutWalletHash(normalized)

	sealed, err := r.cipher.seal(normalized)
	if err != nil {
		return nil, err
	}

	var out *service.SupplierPayoutWallet
	txErr := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		owner, found, err := payoutWalletOwnerOf(txCtx, txClient, network, hash)
		if err != nil {
			return err
		}
		// owner == userID 是换绑成自己已绑的同一个地址：幂等，放行。
		if found && owner != userID {
			return service.ErrSupplierPayoutAddressTaken
		}

		wallet, err := r.cipher.scanSupplierPayoutWalletRow(txCtx, txClient,
			supplierPayoutWalletUpsertSQL, userID, network, sealed, hash)
		if err != nil {
			if isUniqueConstraintViolation(err) {
				// 走到这里说明反查之后、插入之前有人抢先绑了同一个地址。
				return service.ErrSupplierPayoutAddressTaken
			}
			return fmt.Errorf("upsert supplier payout wallet: %w", err)
		}
		if wallet == nil {
			return fmt.Errorf("upsert supplier payout wallet returned no row")
		}
		out = wallet
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

func (r *supplierPayoutWalletRepository) Delete(ctx context.Context, userID int64, network string) error {
	rows, err := r.client.QueryContext(ctx, supplierPayoutWalletDeleteSQL, userID, network)
	if err != nil {
		return fmt.Errorf("delete supplier payout wallet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if rowsErr := rows.Err(); rowsErr != nil {
			return rowsErr
		}
		// 没删到不静默成功：静默会掩盖调用方把 network 传错，
		// 而那时前端显示「已解绑」，库里那条绑定还在，下一笔提现照旧打到旧地址。
		return service.ErrSupplierPayoutWalletNotFound
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return fmt.Errorf("scan deleted supplier payout wallet: %w", err)
	}
	return rows.Err()
}

// ============================================================================
// 助手
// ============================================================================

// payoutWalletOwnerOf 反查某条链上某个地址哈希当前属于谁。
func payoutWalletOwnerOf(ctx context.Context, exec supplierCreditExecer, network, hash string) (int64, bool, error) {
	rows, err := exec.QueryContext(ctx, supplierPayoutWalletOwnerSQL, network, hash)
	if err != nil {
		return 0, false, fmt.Errorf("lookup payout wallet owner: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, false, rows.Err()
	}
	var owner int64
	if err := rows.Scan(&owner); err != nil {
		return 0, false, fmt.Errorf("scan payout wallet owner: %w", err)
	}
	return owner, true, rows.Err()
}

// scanSupplierPayoutWallet 是这张表**唯一**的 Scan，解密也只发生在这里。
// 与 scanSupplierWithdrawal 同一个理由：让将来任何一条新的读路径自动解了密。
func (c payoutAccountCipher) scanSupplierPayoutWallet(row supplierWithdrawalScanner, out *service.SupplierPayoutWallet) error {
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&out.ID, &out.UserID, &out.Network, &out.Address, &createdAt, &updatedAt,
	); err != nil {
		return fmt.Errorf("scan supplier payout wallet: %w", err)
	}
	out.CreatedAt = createdAt
	out.UpdatedAt = updatedAt

	address, err := c.open(out.Address)
	if err != nil {
		return err
	}
	out.Address = address
	return nil
}

func (c payoutAccountCipher) scanSupplierPayoutWalletRow(ctx context.Context, exec supplierCreditExecer, query string, args ...any) (*service.SupplierPayoutWallet, error) {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, rows.Err()
	}
	var out service.SupplierPayoutWallet
	if err := c.scanSupplierPayoutWallet(rows, &out); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

func (r *supplierPayoutWalletRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin supplier payout wallet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit supplier payout wallet transaction: %w", err)
	}
	return nil
}
