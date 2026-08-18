//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplySupplierSettlementParamsAttachesEnabledConfig(t *testing.T) {
	repo := &supplierSettingRepoStub{value: `{"enabled":true,"share_ratio":0.65,"freeze_hours":72,"spend_from_wallet_first":true}`}
	deps := &billingDeps{settingService: newSupplierSettingService(t, repo)}
	cmd := &UsageBillingCommand{RequestID: "req-1"}

	applySupplierSettlementParams(context.Background(), cmd, deps)

	assert.Equal(t, UsageBillingSupplierParams{
		ShareRatio:           0.65,
		FreezeHours:          72,
		SpendFromWalletFirst: true,
	}, cmd.Supplier)
}

func TestApplySupplierSettlementParamsLeavesZeroWhenSettlementOff(t *testing.T) {
	// 开关关着时比例照样存在，但必须落成零值——那正是计费里「什么都不做」的那一支。
	repo := &supplierSettingRepoStub{value: `{"enabled":false,"share_ratio":0.65,"freeze_hours":72}`}
	deps := &billingDeps{settingService: newSupplierSettingService(t, repo)}
	cmd := &UsageBillingCommand{RequestID: "req-1"}

	applySupplierSettlementParams(context.Background(), cmd, deps)

	assert.Equal(t, UsageBillingSupplierParams{}, cmd.Supplier)
}

func TestApplySupplierSettlementParamsFailsClosedOnReadError(t *testing.T) {
	repo := &supplierSettingRepoStub{getErr: errors.New("settings table unreachable")}
	deps := &billingDeps{settingService: newSupplierSettingService(t, repo)}
	cmd := &UsageBillingCommand{RequestID: "req-1"}

	applySupplierSettlementParams(context.Background(), cmd, deps)

	// 读配置失败绝不能让一笔正常计费失败，也不能按猜出来的比例给钱。
	assert.Equal(t, UsageBillingSupplierParams{}, cmd.Supplier)
}

func TestApplySupplierSettlementParamsIsNilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		applySupplierSettlementParams(context.Background(), nil, nil)
		applySupplierSettlementParams(context.Background(), &UsageBillingCommand{}, nil)
		// deps 存在但没注入 SettingService（老装配路径）：静默关闭，不是 panic。
		applySupplierSettlementParams(context.Background(), &UsageBillingCommand{}, &billingDeps{})
	})
}

func TestApplySupplierSettlementParamsOverwritesStaleValue(t *testing.T) {
	// 命令是复用/重建出来的时候，上一次的结算参数不能残留。
	repo := &supplierSettingRepoStub{getErr: ErrSettingNotFound}
	deps := &billingDeps{settingService: newSupplierSettingService(t, repo)}
	cmd := &UsageBillingCommand{
		RequestID: "req-1",
		Supplier:  UsageBillingSupplierParams{ShareRatio: 0.9, FreezeHours: 1},
	}

	applySupplierSettlementParams(context.Background(), cmd, deps)

	assert.Equal(t, UsageBillingSupplierParams{}, cmd.Supplier)
}
