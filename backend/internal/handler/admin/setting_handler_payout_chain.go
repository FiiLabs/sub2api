// APEXONE-EXT: 双边市场——链上打款金库配置的管理端读写（M6）。
//
// 与其它六张供给设置卡同一个 handler、同一条审计中间件。这一张的特殊处
// 全在私钥上：
//
//   - 请求里的 signer_key 是**明文**（管理员从钱包里拿到的就是明文），
//     进门先验形状、再加密，落库的只有密文；
//   - 响应里**永远没有** signer_key——密文也没有。回显的只有
//     signer_configured 和从私钥推导出的金库地址（公开信息）；
//   - 更新时留空 = 保留旧钥匙，换别的参数不用重新粘一遍私钥。
//
// 保存即生效：写完 settings 立刻让 payoutchain.Manager 热换客户端，
// 响应里带上装配结果（LIVE/disabled、链 ID 核没核上）——「保存成功但
// 没生效」这种状态必须当场可见，而不是等第一笔提现卡住才暴露。
package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/payoutchain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SetPayoutChain 注入热换管理器与加密器（与其它可选依赖同一个 setter 模式）。
func (h *SettingHandler) SetPayoutChain(manager *payoutchain.Manager, encryptor service.SecretEncryptor) {
	if h == nil {
		return
	}
	h.payoutChainManager = manager
	h.secretEncryptor = encryptor
}

// SupplyPayoutChainSettingsResponse 是金库配置的对外形态。没有私钥，连密文都没有。
type SupplyPayoutChainSettingsResponse struct {
	Enabled         bool    `json:"enabled"`
	RPCURL          string  `json:"rpc_url"`
	TokenAddress    string  `json:"token_address"`
	TokenSymbol     string  `json:"token_symbol"`
	DisperseAddress string  `json:"disperse_address"`
	ChainID         uint64  `json:"chain_id"`
	NativeUSD       float64 `json:"native_usd"`
	Confirmations   uint64  `json:"confirmations"`
	FallbackFee     float64 `json:"fallback_fee"`
	FeeMultiplier   float64 `json:"fee_multiplier"`

	// SignerConfigured 私钥配过没有。界面靠它决定输入框的占位文案是
	// 「粘贴私钥」还是「已配置，留空保持不变」。
	SignerConfigured bool `json:"signer_configured"`

	// Status 当前客户端的装配状态（模式、金库地址、链 ID 核验结果）。
	// 它回答的是「此刻真的能打款吗」——与上面那些「存了什么」是两个问题。
	Status payoutchain.Status `json:"status"`
}

func (h *SettingHandler) payoutChainResponse(c *gin.Context) SupplyPayoutChainSettingsResponse {
	settings, _ := h.settingService.GetSupplyPayoutChainSettings(c.Request.Context())
	resp := SupplyPayoutChainSettingsResponse{
		Enabled:          settings.Enabled,
		RPCURL:           settings.RPCURL,
		TokenAddress:     settings.TokenAddress,
		TokenSymbol:      settings.TokenSymbol,
		DisperseAddress:  settings.DisperseAddress,
		ChainID:          settings.ChainID,
		NativeUSD:        settings.NativeUSD,
		Confirmations:    settings.Confirmations,
		FallbackFee:      settings.FallbackFee,
		FeeMultiplier:    settings.FeeMultiplier,
		SignerConfigured: h.settingService.SupplyPayoutChainSignerCiphertext(c.Request.Context()) != "",
	}
	if h.payoutChainManager != nil {
		resp.Status = h.payoutChainManager.Status()
	}
	return resp
}

// GetSupplyPayoutChainSettings 读金库配置
// GET /api/v1/admin/settings/supply-payout-chain
func (h *SettingHandler) GetSupplyPayoutChainSettings(c *gin.Context) {
	response.Success(c, h.payoutChainResponse(c))
}

// UpdateSupplyPayoutChainSettingsRequest 更新金库配置请求。
type UpdateSupplyPayoutChainSettingsRequest struct {
	Enabled         bool    `json:"enabled"`
	RPCURL          string  `json:"rpc_url"`
	TokenAddress    string  `json:"token_address"`
	TokenSymbol     string  `json:"token_symbol"`
	DisperseAddress string  `json:"disperse_address"`
	ChainID         uint64  `json:"chain_id"`
	NativeUSD       float64 `json:"native_usd"`
	Confirmations   uint64  `json:"confirmations"`
	FallbackFee     float64 `json:"fallback_fee"`
	FeeMultiplier   float64 `json:"fee_multiplier"`
	// SignerKey 明文私钥。空 = 保留已存的那把。
	SignerKey string `json:"signer_key"`
}

// UpdateSupplyPayoutChainSettings 写金库配置并热换客户端
// PUT /api/v1/admin/settings/supply-payout-chain
func (h *SettingHandler) UpdateSupplyPayoutChainSettings(c *gin.Context) {
	var req UpdateSupplyPayoutChainSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 私钥：新粘的先验形状再加密；留空沿用已存密文。
	sealed := h.settingService.SupplyPayoutChainSignerCiphertext(c.Request.Context())
	if req.SignerKey != "" {
		var err error
		sealed, err = service.SealSupplyPayoutSignerKey(h.secretEncryptor, req.SignerKey)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	settings := &service.SupplyPayoutChainSettings{
		Enabled:         req.Enabled,
		RPCURL:          req.RPCURL,
		SignerKey:       sealed,
		TokenAddress:    req.TokenAddress,
		TokenSymbol:     req.TokenSymbol,
		DisperseAddress: req.DisperseAddress,
		ChainID:         req.ChainID,
		NativeUSD:       req.NativeUSD,
		Confirmations:   req.Confirmations,
		FallbackFee:     req.FallbackFee,
		FeeMultiplier:   req.FeeMultiplier,
	}
	if err := h.settingService.SetSupplyPayoutChainSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 保存即生效。Reload 失败**不算保存失败**：配置已经落库（比如 RPC 一时
	// 连不上），装配的错误随 Status 回给界面——「存了但没活起来」必须可见，
	// 但不该让管理员以为自己什么都没存上。
	if h.payoutChainManager != nil {
		if _, err := h.payoutChainManager.Reload(c.Request.Context()); err != nil {
			// 错误已在 Status.Error 里，界面从那里读；这里不再包一层。
			_ = err
		}
	}
	response.Success(c, h.payoutChainResponse(c))
}

// VerifySupplyPayoutChain 重新装配一次并核链，给「测试连接」按钮用。
// POST /api/v1/admin/settings/supply-payout-chain/verify
//
// 它就是一次 Reload：装配是幂等的，重复执行的代价只是一次 chain id 查询。
// 单独一个端点而不是让界面重放 PUT，是因为测试连接不该有任何写入语义。
func (h *SettingHandler) VerifySupplyPayoutChain(c *gin.Context) {
	if h.payoutChainManager == nil {
		response.Error(c, 503, "payout chain manager is not wired")
		return
	}
	if _, err := h.payoutChainManager.Reload(c.Request.Context()); err != nil {
		_ = err // 同上：错误在 Status.Error 里。
	}
	response.Success(c, h.payoutChainResponse(c))
}
