//go:build integration

// APEXONE-EXT: 链上收款地址绑定的真库测试。
//
// 这里的每一条断言都对应一种「钱打到错的地址上」的方式，而它们几乎全都只有真
// Postgres 能证伪：
//   - 换绑是覆盖不是新增（唯一索引 + ON CONFLICT 推断子句必须真的匹配上）；
//   - 一个地址只属于一个账号（反女巫索引真的存在、真的会咬）；
//   - 归一化发生在算哈希之前（否则同一个地址的大小写变体能各绑一个号，
//     反女巫索引形同虚设，而且不会有任何报错）；
//   - 落库的是密文，读回来的是明文，盲索引仍然对得上。
//
// 用 sqlmock 写这些，等于把「我以为 ON CONFLICT 会匹配哪个索引」再断言一遍。
package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 两个来自 EIP-55 规范正文的地址，写死是为了让「哈希对不对」这件事可以手算。
const (
	testWalletAddrMixed = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	testWalletAddrLower = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	testWalletAddrOther = "0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359"
)

func payoutWalletRepoOn(client *dbent.Client) service.SupplierPayoutWalletRepository {
	return NewSupplierPayoutWalletRepository(client, testPayoutEncryptor())
}

// payoutWalletRawColumns 读出库里真正存着的密文与盲索引，绕过仓储的解密。
//
// 「绕过」是这个助手的全部意义：走仓储读回来的永远是明文，那证不了任何事。
func payoutWalletRawColumns(t *testing.T, ctx context.Context, client *dbent.Client, id int64) (address, hash string) {
	t.Helper()
	rows, err := client.QueryContext(ctx,
		"SELECT address, address_hash FROM supplier_payout_wallets WHERE id = $1", id)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	require.True(t, rows.Next(), "expected one row")
	require.NoError(t, rows.Scan(&address, &hash))
	require.NoError(t, rows.Err())
	return address, hash
}

func payoutWalletRowCount(t *testing.T, ctx context.Context, client *dbent.Client, userID int64) int {
	t.Helper()
	return querySingleInt(t, ctx, client,
		"SELECT COUNT(*)::int FROM supplier_payout_wallets WHERE user_id = $1", userID)
}

// ============================================================================
// 绑定与读取
// ============================================================================

func TestSupplierPayoutWallet_BindAndRead(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-bind")
	repo := payoutWalletRepoOn(client)

	w, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrMixed)
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.Equal(t, userID, w.UserID)
	assert.Equal(t, service.SupplierPayoutNetworkBSC, w.Network)
	// 归一化成小写：库里的唯一索引建在 SHA-256(小写地址) 上，
	// 同一个地址有两种写法就等于唯一约束形同虚设。
	assert.Equal(t, testWalletAddrLower, w.Address)

	got, err := repo.Get(txCtx, userID, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, w.ID, got.ID)
	assert.Equal(t, testWalletAddrLower, got.Address, "Get 没解密或没归一化")

	list, err := repo.List(txCtx, userID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, testWalletAddrLower, list[0].Address, "List 没解密")
}

// 没绑过返回 (nil, nil)：「还没绑」不是错误，前端要靠它决定画输入框还是画地址。
func TestSupplierPayoutWallet_GetMissingReturnsNil(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-none")
	repo := payoutWalletRepoOn(client)

	got, err := repo.Get(txCtx, userID, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Nil(t, got)

	list, err := repo.List(txCtx, userID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// ============================================================================
// 密文与盲索引
// ============================================================================

// 落库的是密文，盲索引是小写地址的 SHA-256。
//
// 后半句必须单独钉：哈希算错了不会有任何报错，只会让反女巫索引挡不住东西——
// 而那是一个「一切正常」的失败。
func TestSupplierPayoutWallet_StoredEncryptedWithBlindIndex(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-cipher")
	repo := payoutWalletRepoOn(client)

	w, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrMixed)
	require.NoError(t, err)

	rawAddress, rawHash := payoutWalletRawColumns(t, txCtx, client, w.ID)
	assert.NotEqual(t, testWalletAddrLower, rawAddress, "库里存的还是明文地址")
	assert.NotContains(t, strings.ToLower(rawAddress), "5aaeb6053f3e94c9",
		"地址片段出现在库里——user_id ↔ address 这条连线一旦泄漏就永久成立")
	assert.Contains(t, rawAddress, supplierPayoutCipherPrefix)

	// 盲索引在这里手算一遍，不调 payoutWalletHash：用被测代码算期望值，
	// 等于断言「它等于它自己」。
	expected := sha256.Sum256([]byte(testWalletAddrLower))
	assert.Equal(t, hex.EncodeToString(expected[:]), rawHash,
		"盲索引不是 SHA-256(小写地址)")
}

// 归一化必须发生在算哈希**之前**。
//
// 这是这个文件里最要紧的一条。混合大小写与全小写是同一个地址；若哈希算在归一化
// 之前，两个账号就能各绑一份「同一个地址」，反女巫索引不会报任何错，
// 平台在补贴一个人却以为在补贴一个市场。
func TestSupplierPayoutWallet_CaseVariantsCollideOnTheSameAddress(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	first := mustCreateSupplier(t, client, "wallet-case-a")
	second := mustCreateSupplier(t, client, "wallet-case-b")
	repo := payoutWalletRepoOn(client)

	_, err := repo.Upsert(txCtx, first, service.SupplierPayoutNetworkBSC, testWalletAddrMixed)
	require.NoError(t, err)

	// 第二个人用全小写形态绑同一个地址。
	_, err = repo.Upsert(txCtx, second, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	assert.ErrorIs(t, err, service.ErrSupplierPayoutAddressTaken,
		"大小写变体绕过了反女巫约束")

	// 全大写形态同样绕不过去。
	_, err = repo.Upsert(txCtx, second, service.SupplierPayoutNetworkBSC,
		"0x"+strings.ToUpper(testWalletAddrLower[2:]))
	assert.ErrorIs(t, err, service.ErrSupplierPayoutAddressTaken)

	assert.Zero(t, payoutWalletRowCount(t, txCtx, client, second), "冲突时不该留下半行")
}

// ============================================================================
// 换绑
// ============================================================================

// 换绑是覆盖，不是新增一行。
//
// 钉的是 ON CONFLICT (user_id, network) 的推断子句真的匹配上了那个唯一索引：
// 匹配不上时 Postgres 会直接报错，而写成 DO NOTHING 则会静默地什么都不改——
// 那时前端显示换绑成功，下一笔钱照旧打到旧地址。
func TestSupplierPayoutWallet_RebindOverwritesInPlace(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-rebind")
	repo := payoutWalletRepoOn(client)

	first, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	second, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrOther)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "换绑新建了一行，说明 ON CONFLICT 没命中")
	assert.Equal(t, testWalletAddrOther, second.Address)
	assert.Equal(t, 1, payoutWalletRowCount(t, txCtx, client, userID))

	// 盲索引必须跟着换。不跟着换的表现是「换了地址但反女巫还认旧的」，
	// 于是旧地址永久占位、新地址可被别人抢注。
	_, rawHash := payoutWalletRawColumns(t, txCtx, client, second.ID)
	expected := sha256.Sum256([]byte(testWalletAddrOther))
	assert.Equal(t, hex.EncodeToString(expected[:]), rawHash, "换绑没更新盲索引")

	got, err := repo.Get(txCtx, userID, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, testWalletAddrOther, got.Address)
}

// 绑成自己已经绑着的那个地址：幂等成功，不是冲突。
//
// 前置反查是按 (network, address_hash) 查的，查到的正是自己那一行——
// 若那里少判一次 owner != userID，用户重复提交一次表单就会收到「地址已被占用」，
// 而占用它的是他自己。
func TestSupplierPayoutWallet_RebindSameAddressIsIdempotent(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-idem")
	repo := payoutWalletRepoOn(client)

	first, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	second, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrMixed)
	require.NoError(t, err, "重复绑定同一个地址被当成了冲突")
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, 1, payoutWalletRowCount(t, txCtx, client, userID))
}

// ============================================================================
// 反女巫
// ============================================================================

func TestSupplierPayoutWallet_AddressTakenByAnotherAccount(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	owner := mustCreateSupplier(t, client, "wallet-owner")
	intruder := mustCreateSupplier(t, client, "wallet-intruder")
	repo := payoutWalletRepoOn(client)

	_, err := repo.Upsert(txCtx, owner, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	_, err = repo.Upsert(txCtx, intruder, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.ErrorIs(t, err, service.ErrSupplierPayoutAddressTaken)

	// 原主人的绑定一动不动：冲突不该有任何副作用。
	got, err := repo.Get(txCtx, owner, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Equal(t, testWalletAddrLower, got.Address)
}

// 唯一索引本身真的存在、真的会咬，而且它抛出的错真的会被认成唯一冲突。
//
// 这条绕过仓储直接插一行重复的盲索引——那正是「前置反查之后、插入之前有人抢先绑了」
// 这个并发窗口在数据库层面的样子。它同时钉住两件事：索引存在，以及
// isUniqueConstraintViolation 认得出 lib/pq 抛出来的那个具体错误。
// 少了后半句，Upsert 里的 23505 兜底分支就永远走不到，并发时用户看到的是
// 一句 duplicate key。
func TestSupplierPayoutWallet_UniqueIndexBitesAndIsRecognized(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	owner := mustCreateSupplier(t, client, "wallet-idx-a")
	intruder := mustCreateSupplier(t, client, "wallet-idx-b")
	repo := payoutWalletRepoOn(client)

	_, err := repo.Upsert(txCtx, owner, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte(testWalletAddrLower))
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_payout_wallets (user_id, network, address, address_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		intruder, service.SupplierPayoutNetworkBSC, "enc.v1:whatever", hex.EncodeToString(sum[:]))

	require.Error(t, err, "反女巫唯一索引没有生效")
	assert.True(t, isUniqueConstraintViolation(err),
		"唯一冲突没被认出来，Upsert 的 23505 兜底分支等于不存在：%v", err)
}

// 换一条链就不冲突——唯一索引是 (network, address_hash) 而不是只有地址。
//
// 现在只有一条链，所以这条用直接 SQL 造出第二条链的行：它钉的是索引的**形状**，
// 而形状要在加第二条链之前就是对的，否则那天所有人的地址会互相挡住。
func TestSupplierPayoutWallet_SameAddressOnAnotherNetworkIsAllowed(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	owner := mustCreateSupplier(t, client, "wallet-net-a")
	other := mustCreateSupplier(t, client, "wallet-net-b")
	repo := payoutWalletRepoOn(client)

	_, err := repo.Upsert(txCtx, owner, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte(testWalletAddrLower))
	_, err = client.ExecContext(txCtx, `
INSERT INTO supplier_payout_wallets (user_id, network, address, address_hash, created_at, updated_at)
VALUES ($1, 'polygon', $2, $3, NOW(), NOW())`,
		other, "enc.v1:whatever", hex.EncodeToString(sum[:]))
	require.NoError(t, err, "唯一索引没有把 network 算进去")
}

// ============================================================================
// 校验发生在碰库之前
// ============================================================================

// 地址不合法时一行都不该写进去。
//
// 校验挪到 service 层之后仍要在这里钉一次：仓储是唯一一个「写进去就算数」的地方，
// 而链上转账不可逆——绑定这一刻是最后一个还没有钱牵扯进来的时刻。
func TestSupplierPayoutWallet_RejectsInvalidAddressWithoutWriting(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-invalid")
	repo := payoutWalletRepoOn(client)

	cases := map[string]struct {
		address string
		want    error
	}{
		"少一位":    {"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beae", service.ErrSupplierPayoutAddressInvalid},
		"校验和不对":  {"0x5aaeb6053F3E94C9b9A09f33669435E7Ef1BeAed", service.ErrSupplierPayoutAddressChecksum},
		"零地址":    {"0x0000000000000000000000000000000000000000", service.ErrSupplierPayoutAddressZero},
		"粘成交易哈希": {"0x88df016429689c079f3b2f6ad39fa052532c56795b733da78a91ebe6a713944b", service.ErrSupplierPayoutAddressInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, tc.address)
			assert.ErrorIs(t, err, tc.want)
		})
	}
	assert.Zero(t, payoutWalletRowCount(t, txCtx, client, userID), "非法地址写进库里了")

	// 网络不认识同样不碰库。
	_, err := repo.Upsert(txCtx, userID, "eth", testWalletAddrLower)
	assert.ErrorIs(t, err, service.ErrSupplierPayoutNetworkInvalid)
	assert.Zero(t, payoutWalletRowCount(t, txCtx, client, userID))
}

// ============================================================================
// 解绑
// ============================================================================

func TestSupplierPayoutWallet_DeleteUnbinds(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-del")
	repo := payoutWalletRepoOn(client)

	_, err := repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(txCtx, userID, service.SupplierPayoutNetworkBSC))
	assert.Zero(t, payoutWalletRowCount(t, txCtx, client, userID))

	got, err := repo.Get(txCtx, userID, service.SupplierPayoutNetworkBSC)
	require.NoError(t, err)
	assert.Nil(t, got)

	// 解绑之后地址要能被别人绑走：反女巫索引是「当前绑定」的约束，不是黑名单。
	// 否则一个人换个号就再也用不了自己的地址了。
	other := mustCreateSupplier(t, client, "wallet-del-next")
	_, err = repo.Upsert(txCtx, other, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	assert.NoError(t, err, "解绑后地址仍被占着")
}

// 没绑过就删 → 明确报错，不静默成功。
func TestSupplierPayoutWallet_DeleteMissingReportsNotFound(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	userID := mustCreateSupplier(t, client, "wallet-del-missing")
	repo := payoutWalletRepoOn(client)

	err := repo.Delete(txCtx, userID, service.SupplierPayoutNetworkBSC)
	assert.True(t, errors.Is(err, service.ErrSupplierPayoutWalletNotFound),
		"没删到却成功了，会掩盖调用方把 network 传错：%v", err)

	// 绑了 bsc 之后删别的链，同样必须报错而不是把 bsc 那条删掉。
	_, err = repo.Upsert(txCtx, userID, service.SupplierPayoutNetworkBSC, testWalletAddrLower)
	require.NoError(t, err)
	assert.ErrorIs(t, repo.Delete(txCtx, userID, "polygon"), service.ErrSupplierPayoutWalletNotFound)
	assert.Equal(t, 1, payoutWalletRowCount(t, txCtx, client, userID), "删错链把绑定删掉了")
}
