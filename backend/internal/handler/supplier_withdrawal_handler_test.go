//go:build unit

// APEXONE-EXT: 双边市场——提现单发给供给者的那份 DTO。
//
// 这个文件只盯一件事：**一张单子上关于钱的那几个数，出门之后还对不对**。
//
// 它有两个方向的失败，都不会让任何东西报错：
//
//   - 少给：`fee_amount` 不出门，供给者申请 100、到账 99.7，中间那 0.3 只存在于
//     数据库里。对他就是一笔凭空少掉的钱，而客服无从解释——因为界面上也没有。
//   - 多给：`token_address`、`reviewer_id` 这种结算/内部字段跟着漏出去。前者是噪音，
//     后者是把具体的运营同事暴露给一个对结果不满的人。
//
// 领域结构体 SupplierWithdrawal 自己的 JSON 形状钉在 service 层
//（supplier_withdrawal_chain_test.go），那一份是**管理端**看到的东西——
// 管理端的 handler 直接返回领域对象，不经过这里。两份必须分开钉：
// 它们本来就该不一样，改动其中一份不该悄悄改掉另一份。
package handler

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withdrawalViewKeys 把 DTO 序列化一遍，回它实际发出去的键。
//
// 断言 Go 结构体的字段等于白断言——tag 写错、omitempty 多写一个，字段还在，
// 出门的 JSON 已经变了。只有真的 Marshal 一次才看得见。
func withdrawalViewKeys(t *testing.T, view supplierWithdrawalView) (map[string]any, []string) {
	t.Helper()
	raw, err := json.Marshal(view)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return decoded, keys
}

// onchainWithdrawalRow 造一张走链上的单子，四列齐全。
func onchainWithdrawalRow() *service.SupplierWithdrawal {
	network := service.SupplierPayoutNetworkBSC
	symbol := "USDT"
	address := "0x55d398326f99059ff775485246999027b3197955"
	return &service.SupplierWithdrawal{
		ID:            9,
		UserID:        7,
		Amount:        100,
		Status:        service.SupplierWithdrawalStatusPending,
		PayoutChannel: "BSC-USDT",
		PayoutAccount: "0xde709f2102306220921060314715629080e2fb77",
		Network:       &network,
		TokenSymbol:   &symbol,
		TokenAddress:  &address,
		FeeAmount:     0.3,
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
		UpdatedAt:     time.Unix(1700000000, 0).UTC(),
	}
}

// 链上单把「扣了多少、到手多少」两个数都发出去。
//
// net_amount 由后端算而不是让前端减一次：这是一条关于钱的公式，抄进 TypeScript
// 就有了第二份，而两份迟早在某次改动里分岔（比如哪天手续费改成加在外面）。
// 这里顺带钉住它与领域方法 NetAmount() 是同一个数，而不是 handler 自己又减了一遍。
func TestWithdrawalViewCarriesFeeAndNet(t *testing.T) {
	row := onchainWithdrawalRow()
	decoded, _ := withdrawalViewKeys(t, toSupplierWithdrawalView(row))

	assert.Equal(t, 100.0, decoded["amount"])
	assert.Equal(t, 0.3, decoded["fee_amount"])
	assert.Equal(t, row.NetAmount(), decoded["net_amount"],
		"净额与领域层算的不是同一个数——handler 自己又减了一遍")
	assert.InDelta(t, 99.7, decoded["net_amount"], 1e-9)

	// 这两项在界面上决定说哪句话：自动打款说"几分钟"，人工打款说"工作日内"。
	assert.Equal(t, service.SupplierPayoutNetworkBSC, decoded["network"])
	assert.Equal(t, "USDT", decoded["token_symbol"])
}

// 人工单上 fee_amount 是 0，但**这个键必须还在**。
//
// 给它加 omitempty 会让人工单的响应里根本没有这个字段，而 0 与 undefined 对前端
// 不是一回事：后者拿去做减法得到的是 NaN，界面上就是「到账 NaN USDT」。
// net_amount 同理——人工单的到手金额就是全额。
func TestWithdrawalViewKeepsZeroFeeOnManualOrders(t *testing.T) {
	manual := &service.SupplierWithdrawal{
		ID: 3, UserID: 7, Amount: 50, Status: service.SupplierWithdrawalStatusPending,
		PayoutChannel: "USDT-TRC20", PayoutAccount: "T...",
	}
	decoded, keys := withdrawalViewKeys(t, toSupplierWithdrawalView(manual))

	assert.Contains(t, keys, "fee_amount", "人工单上 fee_amount 整个键消失了（多半是加了 omitempty）")
	assert.Contains(t, keys, "net_amount")
	assert.Equal(t, 0.0, decoded["fee_amount"])
	assert.Equal(t, 50.0, decoded["net_amount"])

	// 反过来，链上那两项在人工单上不该出现——前端靠「有没有 network」分辨两条路径。
	assert.NotContains(t, keys, "network")
	assert.NotContains(t, keys, "token_symbol")
}

// 供给侧这份 DTO 的完整键集。
//
// 白名单而不是逐个 Contains：这个文件真正要防的是**多**发，而多发只有全集能发现。
// 尤其是 reviewer_id——它在领域对象上是有的，一次顺手的字段复制就会把它带出门。
func TestWithdrawalViewWireShape(t *testing.T) {
	_, keys := withdrawalViewKeys(t, toSupplierWithdrawalView(onchainWithdrawalRow()))

	assert.Equal(t, []string{
		"amount", "created_at", "fee_amount", "id", "net_amount", "network",
		"payout_account", "payout_channel", "status", "token_symbol", "updated_at",
	}, keys)

	// 点名这两个：它们不是"碰巧不在名单里"，是刻意不给。
	//
	// token_address 是结算细节，对供给者是噪音（管理端与对账导出里都拿得到）；
	// reviewer_id 是内部身份，暴露它只会给具体的人招来私下沟通。
	assert.NotContains(t, keys, "token_address")
	assert.NotContains(t, keys, "reviewer_id")
	assert.NotContains(t, keys, "user_id", "单子是从自己的列表里读出来的，用不着再回一次自己是谁")
}

// nil 单子映射成零值，而不是 panic。
//
// 这条路径真实存在：service 返回 (nil, nil) 的分支不该在 HTTP 层炸成 500。
func TestWithdrawalViewHandlesNil(t *testing.T) {
	decoded, keys := withdrawalViewKeys(t, toSupplierWithdrawalView(nil))
	assert.Equal(t, 0.0, decoded["fee_amount"])
	assert.Equal(t, 0.0, decoded["net_amount"])
	assert.NotContains(t, keys, "network")
}
