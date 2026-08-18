// APEXONE-EXT: 双边市场——计费事务内的供给侧结算。
//
// 这里是「消耗 === 入账」不变量的实现处。设计人裁定把结算 accrue 内联进计费事务
// （结算正确性 > 上游合并便利）：扣款与入账要么同时成立，要么同时不成立，中间不存在
// 「钱扣了但供给者没记上」的窗口，也不需要任何对账补偿任务。
//
// usage_billing_repo.go 那边只留两行调用 + APEXONE-EXT 标记，全部细节在本文件，
// 让上游合并时冲突面积尽可能小。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierSettlementBasis 是分成基数：消费者这次请求实际付出的金额。
//
// 刻意用实付而不是官方价：消费者按 0.5× 官方价买，供给者按实付的 70% 分成，
// 平台留 30%——三方口径统一在同一个数上，不需要在两套价格之间做换算。
//
// BalanceCost 与 SubscriptionCost 在命令构造处是互斥的（余额付或订阅额度付），
// 这里相加只是为了不依赖那个互斥前提。两者都已经乘过分组倍率。
func supplierSettlementBasis(cmd *service.UsageBillingCommand) float64 {
	if cmd == nil {
		return 0
	}
	return cmd.BalanceCost + cmd.SubscriptionCost
}

// resolveSupplierOwnerID 查这次请求命中的账号属于哪个供给者。
// 返回 0 表示自营账号（owner_user_id IS NULL），无需结算。
//
// 在事务里现查而不是让 UsageBillingCommand 多带一个字段：这是一次主键点查，
// 代价可以忽略，换来的是 service.Account、账号映射器、调度链路全都不用动。
func resolveSupplierOwnerID(ctx context.Context, tx *sql.Tx, accountID int64) (int64, error) {
	if accountID <= 0 {
		return 0, nil
	}
	var ownerUserID sql.NullInt64
	err := tx.QueryRowContext(ctx,
		"SELECT owner_user_id FROM accounts WHERE id = $1 AND deleted_at IS NULL",
		accountID).Scan(&ownerUserID)
	if errors.Is(err, sql.ErrNoRows) {
		// 账号在本次请求期间被删了。计费照常收敛，只是没有结算对象。
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("resolve supplier owner: %w", err)
	}
	if !ownerUserID.Valid {
		return 0, nil
	}
	return ownerUserID.Int64, nil
}

// accrueSupplierRevenue 给供给者入账本次请求的分成。
//
// 返回错误会让整个计费事务回滚——这是有意的。入账失败却把消费者的钱收了，
// 等于平台单方面吞掉供给者的收入；宁可让这次计费失败、由上层重试，
// 也不能留下对不上的账。
func accrueSupplierRevenue(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.Supplier.ShareRatio <= 0 {
		return nil
	}
	basis := supplierSettlementBasis(cmd)
	if basis <= 0 {
		// 免费分组、零成本请求：没有实付就没有分成。
		return nil
	}

	ownerUserID, err := resolveSupplierOwnerID(ctx, tx, cmd.AccountID)
	if err != nil {
		return err
	}
	if ownerUserID == 0 {
		return nil
	}
	if ownerUserID == cmd.UserID {
		// 供给者用自己挂上去的账号：左手倒右手，不结算。
		// 否则「自供自用」会凭空生成 70% 的返利，是一条白送的套利路径。
		return nil
	}

	accountID := cmd.AccountID
	consumerUserID := cmd.UserID
	_, err = accrueSupplierCreditTx(ctx, tx, service.SupplierAccrueParams{
		SupplierUserID: ownerUserID,
		RequestID:      cmd.RequestID,
		AccountID:      &accountID,
		ConsumerUserID: &consumerUserID,
		BasisAmount:    basis,
		ShareRatio:     cmd.Supplier.ShareRatio,
		FreezeHours:    cmd.Supplier.FreezeHours,
	})
	return err
}

// spendFromSupplierWallet 尝试用消费者的赚取钱包支付本次请求。
//
// 返回 true 表示钱包已经付掉了全部费用，调用方必须跳过 users.balance 扣款——
// 「同一请求只扣一处」就落在这一个布尔量上。
//
// 刻意做成全额或不扣，不做部分抵扣：拆成「钱包扣一半、余额扣一半」会让每笔消费产生
// 两条来源不同的流水，对账、退款、拒付追回全部要按比例拆，复杂度远超省下的那点余额。
// 钱包不够就整笔走余额，用户在仪表盘上看到的钱包余额也因此始终是「能直接花掉的数」。
func spendFromSupplierWallet(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	if cmd == nil || !cmd.Supplier.SpendFromWalletFirst || cmd.BalanceCost <= 0 {
		return false, nil
	}
	return spendSupplierCreditTx(ctx, tx, cmd.UserID, cmd.BalanceCost, cmd.RequestID)
}
