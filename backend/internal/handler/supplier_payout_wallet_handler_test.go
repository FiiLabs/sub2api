//go:build unit

// APEXONE-EXT: 双边市场——收款地址绑定 HTTP 层的单元测试。
//
// 这一层薄，但它决定的三件事都不薄：**状态码**（前端靠它分辨"你填错了"
// 和"我们坏了"）、**userID 从哪来**（结构性那一半在 supplier_identity_test.go，
// 这里补上行为的那一半）、以及**响应体里那串地址长什么样**——它会被前端
// 原样显示给用户，然后用户会拿它去核对自己的钱包。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 取自 EIP-55 规范里的示例地址。用规范给的向量而不是随手编一个，
// 是因为「大小写混合的那一版校验和对不对」这件事只有规范说了算。
const (
	walletHandlerAddrMixed = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	walletHandlerAddrLower = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
)

// payoutWalletHandlerRepo 是一份内存绑定表。
//
// 用真的 SupplierPayoutWalletService 套一个假仓储，而不是给 handler 塞一个
// 假 service：handler 与 service 之间那几个错误的映射（NotFound→404、
// Taken→409、链不认识→400）正是这个文件要测的东西，把 service 换掉就等于
// 把被测对象换掉了。
type payoutWalletHandlerRepo struct {
	wallets map[string]service.SupplierPayoutWallet // network → 绑定
	getErr  error
	upsert  error
	del     error

	gotUserIDs []int64 // 每一次调用看到的 userID，用来钉住"是谁在操作"
}

func newPayoutWalletHandlerRepo() *payoutWalletHandlerRepo {
	return &payoutWalletHandlerRepo{wallets: map[string]service.SupplierPayoutWallet{}}
}

func (r *payoutWalletHandlerRepo) Get(_ context.Context, userID int64, network string) (*service.SupplierPayoutWallet, error) {
	r.gotUserIDs = append(r.gotUserIDs, userID)
	if r.getErr != nil {
		return nil, r.getErr
	}
	wallet, ok := r.wallets[network]
	if !ok {
		return nil, nil
	}
	return &wallet, nil
}

func (r *payoutWalletHandlerRepo) List(_ context.Context, userID int64) ([]service.SupplierPayoutWallet, error) {
	r.gotUserIDs = append(r.gotUserIDs, userID)
	if r.getErr != nil {
		return nil, r.getErr
	}
	// 刻意返回 nil 而不是空切片：仓储真的可以这样返回，而"nil 要变成 []"
	// 是 service 层的承诺，这里正好顺带验证它没被绕过。
	var out []service.SupplierPayoutWallet
	for _, wallet := range r.wallets {
		out = append(out, wallet)
	}
	return out, nil
}

func (r *payoutWalletHandlerRepo) Upsert(_ context.Context, userID int64, network, address string) (*service.SupplierPayoutWallet, error) {
	r.gotUserIDs = append(r.gotUserIDs, userID)
	if r.upsert != nil {
		return nil, r.upsert
	}
	// 归一化真的发生在仓储里（那是唯一"写进去就算数"的地方），假仓储照做，
	// 否则这个文件会测出一个真实实现里不存在的行为。
	normalized, err := service.NormalizeSupplierPayoutAddress(network, address)
	if err != nil {
		return nil, err
	}
	wallet := service.SupplierPayoutWallet{ID: 1, UserID: userID, Network: network, Address: normalized}
	r.wallets[network] = wallet
	return &wallet, nil
}

func (r *payoutWalletHandlerRepo) Delete(_ context.Context, userID int64, network string) error {
	r.gotUserIDs = append(r.gotUserIDs, userID)
	if r.del != nil {
		return r.del
	}
	if _, ok := r.wallets[network]; !ok {
		return service.ErrSupplierPayoutWalletNotFound
	}
	delete(r.wallets, network)
	return nil
}

// payoutWalletRouterOn 装一个只挂着三条绑定路由的路由器。
//
// 不带鉴权中间件——鉴权是路由组的事，已由 routes/supplier_test.go 钉住；
// 这里用一层假的 JWT 中间件直接把身份写进上下文，因为这个文件要测的恰恰是
// handler **拿到身份之后**怎么用它。
func payoutWalletRouterOn(repo service.SupplierPayoutWalletRepository, userID int64) *gin.Engine {
	var walletService *service.SupplierPayoutWalletService
	if repo != nil {
		walletService = service.NewSupplierPayoutWalletService(repo)
	}
	h := NewSupplierHandler(nil, nil, nil, nil, nil, nil, walletService, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	if userID > 0 {
		router.Use(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: userID})
			c.Next()
		})
	}
	router.GET("/payout-wallets", h.GetPayoutWalletOptions)
	router.PUT("/payout-wallets/:network", h.BindPayoutWallet)
	router.DELETE("/payout-wallets/:network", h.UnbindPayoutWallet)
	return router
}

// payoutWalletCall 发一次请求，回状态码与解开的响应体。
func payoutWalletCall(t *testing.T, router *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	var payload map[string]any
	if recorder.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload),
			"响应体不是 JSON：%s", recorder.Body.String())
	}
	return recorder.Code, payload
}

// payoutWalletErrorCode 从响应体里挖出业务错误码。
//
// 错误信封是 {code: <HTTP 状态码>, message, reason: <业务码>}——业务码在
// `reason` 上，`code` 是那个数字。这个命名容易读反，所以只在这一处解析，
// 免得每个测试各读各的、其中一个读到 `code` 拿到 400 还以为自己在断言业务码。
func payoutWalletErrorCode(t *testing.T, payload map[string]any) string {
	t.Helper()
	require.NotNil(t, payload)
	reason, ok := payload["reason"].(string)
	require.Truef(t, ok, "响应体里没有业务错误码：%v", payload)
	return reason
}

// 还没绑过时是 200 + 空数组，不是 404，也不是 null。
//
// 这个接口的用途是画一张绑定表单，而"还没绑"正是那张表单存在的理由。
// `wallets` 为 null 时前端的 v-for 会炸在一个与业务无关的地方。
func TestPayoutWalletHandler_OptionsEmptyIsTwoHundredWithArray(t *testing.T) {
	router := payoutWalletRouterOn(newPayoutWalletHandlerRepo(), 42)

	code, payload := payoutWalletCall(t, router, http.MethodGet, "/payout-wallets", "")
	require.Equal(t, http.StatusOK, code)

	data, ok := payload["data"].(map[string]any)
	require.Truef(t, ok, "响应里没有 data：%v", payload)

	wallets, ok := data["wallets"].([]any)
	require.Truef(t, ok, "wallets 不是数组（多半是 null）：%v", data["wallets"])
	assert.Empty(t, wallets)

	channels, ok := data["channels"].([]any)
	require.Truef(t, ok, "channels 不是数组：%v", data["channels"])
	assert.NotEmpty(t, channels, "链上渠道注册表是空的——前端画不出任何一个可选项")
}

// 绑定成功后回的是**小写**地址。
//
// 提交进来的是 EIP-55 混合大小写，回去的必须是库里那一份小写形态：
// 后端返回什么、前端就该能原样再提交回来，而一个被美化过的地址提交回来
// 会撞在校验和那道门上。
func TestPayoutWalletHandler_BindNormalizesToLowercase(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 42)

	code, payload := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/bsc",
		`{"address":"`+walletHandlerAddrMixed+`"}`)
	require.Equal(t, http.StatusOK, code)

	data, ok := payload["data"].(map[string]any)
	require.Truef(t, ok, "响应里没有 data：%v", payload)
	assert.Equal(t, walletHandlerAddrLower, data["address"])
	assert.Equal(t, service.SupplierPayoutNetworkBSC, data["network"])

	// 落库的也是同一份小写形态。
	assert.Equal(t, walletHandlerAddrLower, repo.wallets[service.SupplierPayoutNetworkBSC].Address)
}

// 绑定的对象一律是 token 里那个人。
//
// 请求体里塞一个 user_id 不该有任何效果——结构上 handler 根本不认这个字段
// （supplier_identity_test.go 钉住了这一点），这里从行为上再证一遍：
// 仓储看到的 userID 是 42，不是请求体里那个 9999。
func TestPayoutWalletHandler_BindsForTokenHolderOnly(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 42)

	code, _ := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/bsc",
		`{"user_id":9999,"address":"`+walletHandlerAddrLower+`"}`)
	require.Equal(t, http.StatusOK, code)

	require.NotEmpty(t, repo.gotUserIDs)
	for _, got := range repo.gotUserIDs {
		assert.Equal(t, int64(42), got, "仓储看到了 token 之外的 userID")
	}
	assert.Equal(t, int64(42), repo.wallets[service.SupplierPayoutNetworkBSC].UserID)
}

// 没登录时一条都进不去，而且**碰都没碰仓储**。
func TestPayoutWalletHandler_RejectsAnonymous(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 0) // 不装身份中间件

	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/payout-wallets", ""},
		{http.MethodPut, "/payout-wallets/bsc", `{"address":"` + walletHandlerAddrLower + `"}`},
		{http.MethodDelete, "/payout-wallets/bsc", ""},
	} {
		t.Run(call.method, func(t *testing.T) {
			code, _ := payoutWalletCall(t, router, call.method, call.path, call.body)
			assert.Equal(t, http.StatusUnauthorized, code)
		})
	}
	assert.Empty(t, repo.gotUserIDs, "没登录的请求走到仓储了")
}

// 不认识的链回 400，不是 404，而且不碰仓储。
//
// 链标识是一个封闭、公开的值集，藏起来没有任何意义，而"你传的链我不支持"
// 是调用方能立刻改对的东西。这一点与单号不同——单号回 404 是为了不让它可枚举。
func TestPayoutWalletHandler_UnknownNetworkIsBadRequest(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 42)

	for _, call := range []struct{ method, body string }{
		{http.MethodPut, `{"address":"` + walletHandlerAddrLower + `"}`},
		{http.MethodDelete, ""},
	} {
		t.Run(call.method, func(t *testing.T) {
			code, payload := payoutWalletCall(t, router, call.method, "/payout-wallets/eth", call.body)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "SUPPLIER_PAYOUT_NETWORK_INVALID", payoutWalletErrorCode(t, payload))
		})
	}
	assert.Empty(t, repo.gotUserIDs, "链都没认出来就去碰库了")
}

// 地址错在哪，前端必须能分辨。
//
// 三种错误码对应三句完全不同的话："你少粘了几位"、"你粘的位数对但改过一位"、
// "这个地址收不到钱"。合并成一个 400 会让第二种（几乎必然是手工改过地址，
// 也是最危险的那一种）读起来像个格式问题。
func TestPayoutWalletHandler_AddressErrorsAreDistinguishable(t *testing.T) {
	router := payoutWalletRouterOn(newPayoutWalletHandlerRepo(), 42)

	for name, testCase := range map[string]struct{ address, code string }{
		"位数不够":   {"0x5aaeb6053f3e94c9b9a09f33669435e7ef1bea", "SUPPLIER_PAYOUT_ADDRESS_INVALID"},
		"校验和不匹配": {"0x5AAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", "SUPPLIER_PAYOUT_ADDRESS_CHECKSUM"},
		"零地址":    {"0x0000000000000000000000000000000000000000", "SUPPLIER_PAYOUT_ADDRESS_ZERO"},
		"空字符串":   {"", "SUPPLIER_PAYOUT_ADDRESS_INVALID"},
	} {
		t.Run(name, func(t *testing.T) {
			code, payload := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/bsc",
				`{"address":"`+testCase.address+`"}`)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, testCase.code, payoutWalletErrorCode(t, payload))
		})
	}
}

// 地址被别人绑走了是 409，不是 400。
//
// 请求本身没有错——同一个请求换个人发就会成功。把它报成 400 会让前端
// 提示"地址格式不对"，而用户会一遍遍去检查一个完全正确的地址。
func TestPayoutWalletHandler_TakenAddressIsConflict(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	repo.upsert = service.ErrSupplierPayoutAddressTaken
	router := payoutWalletRouterOn(repo, 42)

	code, payload := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/bsc",
		`{"address":"`+walletHandlerAddrLower+`"}`)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "SUPPLIER_PAYOUT_ADDRESS_TAKEN", payoutWalletErrorCode(t, payload))
}

// 请求体不是 JSON 时是 400，而且报的是**请求体**坏了，不是地址错了。
//
// 两者都是 400，但对调用方是两句完全不同的话。放着不管的话，一个 JSON 少了
// 半个引号会被报成"地址格式不对"——因为解不开的请求体留下的是一个空 Address，
// 而空地址恰好也过不了校验。于是前端拿着一个语法错误去让用户重新粘地址。
func TestPayoutWalletHandler_MalformedBodyIsReportedAsBody(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 42)

	code, payload := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/bsc", `{"address":`)
	require.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "Invalid request body", payload["message"],
		"请求体的语法错误被报成了别的东西（多半是地址校验，因为空地址也过不了）")
	assert.Empty(t, repo.wallets, "请求体都没解开就写库了")
}

// 链不认识时，答案与请求体长什么样无关。
//
// 这条钉住的是**顺序**：链的校验必须排在解请求体之前。反过来的话，
// "链传错了 + 请求体也坏了"会被报成请求体的问题，而调用方改好请求体再发一次，
// 才发现真正的错在路径上——两轮才拿到本来一轮就该给的答案。
//
// 它同时是"handler 这层的链校验不是多余的"的唯一证据：service 层确实也校验链，
// 单看一个格式正确的请求，两层的结果一模一样。差别只在这里显出来。
func TestPayoutWalletHandler_NetworkCheckedBeforeBody(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	router := payoutWalletRouterOn(repo, 42)

	code, payload := payoutWalletCall(t, router, http.MethodPut, "/payout-wallets/eth", `{"address":`)
	require.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "SUPPLIER_PAYOUT_NETWORK_INVALID", payoutWalletErrorCode(t, payload),
		"链的校验排到了解请求体后面")
	assert.Empty(t, repo.gotUserIDs)
}

// 解绑成功回一个明确的"现在没绑"，解绑不存在的回 404。
//
// 后者刻意不静默成功：静默成功会让前端显示"已解绑"，而库里那条绑定还在——
// 最常见的成因是调用方把 network 传错了，而那正是需要被看见的。
func TestPayoutWalletHandler_Unbind(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	repo.wallets[service.SupplierPayoutNetworkBSC] = service.SupplierPayoutWallet{
		ID: 1, UserID: 42, Network: service.SupplierPayoutNetworkBSC, Address: walletHandlerAddrLower,
	}
	router := payoutWalletRouterOn(repo, 42)

	code, payload := payoutWalletCall(t, router, http.MethodDelete, "/payout-wallets/bsc", "")
	require.Equal(t, http.StatusOK, code)
	data, ok := payload["data"].(map[string]any)
	require.Truef(t, ok, "响应里没有 data：%v", payload)
	assert.Equal(t, service.SupplierPayoutNetworkBSC, data["network"])
	assert.Equal(t, false, data["bound"])
	assert.Empty(t, repo.wallets)

	// 再解一次：这次库里没有了。
	code, payload = payoutWalletCall(t, router, http.MethodDelete, "/payout-wallets/bsc", "")
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "SUPPLIER_PAYOUT_WALLET_NOT_FOUND", payoutWalletErrorCode(t, payload))
}

// 服务没装配起来时是 503，而且错误码说的是"我们坏了"。
//
// 这里回 400 SUPPLIER_PAYOUT_NETWORK_INVALID（早先的写法）会把一次 wire 装配
// 失误显示成"不支持这条链"：用户会去换链重试，运维会去查链配置，
// 而真正坏掉的东西不在这两个地方的任何一个。
func TestPayoutWalletHandler_UnwiredServiceIsUnavailable(t *testing.T) {
	router := payoutWalletRouterOn(nil, 42)

	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/payout-wallets", ""},
		{http.MethodPut, "/payout-wallets/bsc", `{"address":"` + walletHandlerAddrLower + `"}`},
		{http.MethodDelete, "/payout-wallets/bsc", ""},
	} {
		t.Run(call.method, func(t *testing.T) {
			code, payload := payoutWalletCall(t, router, call.method, call.path, call.body)
			assert.Equal(t, http.StatusServiceUnavailable, code)
			assert.Equal(t, "SUPPLIER_PAYOUT_WALLET_UNAVAILABLE", payoutWalletErrorCode(t, payload))
		})
	}
}

// 仓储炸了不能被当成"还没绑"。
//
// 这是失败关闭在 HTTP 层的那一半：读绑定出错时回 5xx，而不是回一个
// 空的绑定列表让前端画出"你还没绑过任何地址"——后者会让人以为
// 自己的绑定丢了，然后去重新绑一遍。
func TestPayoutWalletHandler_RepositoryErrorIsNotEmptyState(t *testing.T) {
	repo := newPayoutWalletHandlerRepo()
	repo.getErr = assert.AnError
	router := payoutWalletRouterOn(repo, 42)

	code, _ := payoutWalletCall(t, router, http.MethodGet, "/payout-wallets", "")
	assert.GreaterOrEqual(t, code, 500, "读绑定失败被当成了一个正常的空状态")
}
