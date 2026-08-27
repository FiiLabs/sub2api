//go:build unit

// APEXONE-EXT: 双边市场——供给者读侧服务的单元测试。
//
// 这里只测一条性质：**供给者读到的流水里没有消费者身份**。
// 它值得单独一个文件，因为破坏它的方式非常安静——repository 照常把 source_user_id
// 填进结构体（运营侧确实要用），service 少抹一次、或者有人为了"少一次拷贝"把
// stripConsumerIdentity 拿掉，接口照样 200，只是从此每一页流水都带着一串
// 「谁在用我的号」的 user_id。没有测试的话，这个回归要等到有人翻页时才被发现。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerRepoStub 只实现 ListLedger。
//
// 内嵌接口而不是实现全部方法：本测试若走到了别的方法上，那说明读流水这条路
// 摸了它不该摸的东西，此时 panic 比静默通过更有价值。
type ledgerRepoStub struct {
	SupplierCreditRepository
	entries []SupplierCreditLedgerEntry
	total   int64
	err     error

	gotFilter SupplierCreditLedgerFilter
}

func (r *ledgerRepoStub) ListLedger(_ context.Context, filter SupplierCreditLedgerFilter) ([]SupplierCreditLedgerEntry, int64, error) {
	r.gotFilter = filter
	if r.err != nil {
		return nil, 0, r.err
	}
	return r.entries, r.total, nil
}

// 包内已有一个 int64Ptr（ops_openai_token_stats_test.go，无构建标签），
// 换个名字免得在 -tags unit 下重复定义。
func consumerIDPtr(v int64) *int64 { return &v }

func TestSupplierCreditServiceListLedgerStripsConsumerIdentity(t *testing.T) {
	repo := &ledgerRepoStub{
		entries: []SupplierCreditLedgerEntry{
			{ID: 1, UserID: 7, Action: SupplierCreditActionAccrue, Amount: 0.5, SourceUserID: consumerIDPtr(1001)},
			{ID: 2, UserID: 7, Action: SupplierCreditActionClawback, Amount: -0.5, SourceUserID: consumerIDPtr(1002)},
			{ID: 3, UserID: 7, Action: SupplierCreditActionThaw, Amount: 0.5},
		},
		total: 3,
	}
	svc := NewSupplierCreditService(repo, nil)

	entries, total, err := svc.ListLedger(context.Background(), SupplierCreditLedgerFilter{UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, entries, 3)

	for _, entry := range entries {
		assert.Nil(t, entry.SourceUserID, "ledger id %d still carries the consumer id", entry.ID)
	}

	// 结构体字段为 nil 还不够：真正出网的是 JSON。tag 上的 omitempty 如果哪天被
	// 删掉，字段会变成一个 "source_user_id": null——键名本身就已经在告诉供给者
	// 这个维度存在，而下一次"顺手修一下 null"就会把值填回来。
	encoded, err := json.Marshal(entries)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "source_user_id")
}

// 出错时不能返回一个"已脱敏"的空切片假装成功——调用方分不清是没有流水还是读失败。
func TestSupplierCreditServiceListLedgerPropagatesRepoError(t *testing.T) {
	repoErr := errors.New("boom")
	svc := NewSupplierCreditService(&ledgerRepoStub{err: repoErr}, nil)

	entries, total, err := svc.ListLedger(context.Background(), SupplierCreditLedgerFilter{UserID: 7})
	require.ErrorIs(t, err, repoErr)
	assert.Nil(t, entries)
	assert.Zero(t, total)
}

// TestShareRatio_UnreadableSettingsYieldZero 钉住 ShareRatio 的失败语义。
//
// 这个数会被供给者界面直接显示成「你拿 X%」，所以它的失败方式必须是**闭嘴**，
// 不是猜。回 0 是与调用方约好的「我说不出」信号，界面据此整块不画；
// 若有人日后改成回 SupplierShareRatioDefault「让它有个合理的值」，界面就会在
// 一台压根没配好的部署上，对着一个正在决定要不要挂号的人报一个平台并不会兑现的比例。
func TestShareRatio_UnreadableSettingsYieldZero(t *testing.T) {
	// 两条 nil 路径分开断言：service 本身为 nil（未装配）和 settingService 为 nil
	// （装配了但没接设置源）是两种不同的部署事故，都不该把默认值当成真实配置报出去。
	var nilSvc *SupplierCreditService
	assert.Zero(t, nilSvc.ShareRatio(context.Background()))

	assert.Zero(t, (&SupplierCreditService{}).ShareRatio(context.Background()))
}
