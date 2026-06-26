package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// consultAPIKeys is a narrow seam over *service.APIKeyService for unit testing.
// It also embeds service.APIKeyQuotaUpdater so the same value can be passed as
// RecordUsageInput.APIKeyService without an extra cast.
type consultAPIKeys interface {
	GetByKeyHash(ctx context.Context, hash string) (*service.APIKey, error)
	GetByID(ctx context.Context, id int64) (*service.APIKey, error)
	UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error
	UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error
}

// consultUsage is a narrow seam over *service.GatewayService for unit testing.
type consultUsage interface {
	RecordUsage(ctx context.Context, in *service.RecordUsageInput) error
}

// consultAccounts is a narrow seam over service.AccountRepository for unit testing.
type consultAccounts interface {
	GetByID(ctx context.Context, id int64) (*service.Account, error)
}

// consultSubs is a narrow seam over *service.SubscriptionService for unit testing.
type consultSubs interface {
	GetActiveSubscription(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error)
}

// consultPricing is a narrow seam over *service.PricingService for unit testing.
type consultPricing interface {
	GetModelPricing(model string) *service.LiteLLMModelPricing
}

// ConsultHandler handles consult control-plane requests.
type ConsultHandler struct {
	apiKeys  consultAPIKeys
	subs     consultSubs
	pricing  consultPricing
	gateway  consultUsage
	accounts consultAccounts
	cfg      *config.Config
}

// NewConsultHandler creates a new ConsultHandler.
func NewConsultHandler(
	apiKeys consultAPIKeys,
	subs consultSubs,
	pricing consultPricing,
	gateway consultUsage,
	accounts consultAccounts,
	cfg *config.Config,
) *ConsultHandler {
	return &ConsultHandler{apiKeys, subs, pricing, gateway, accounts, cfg}
}

// Models implements GET /models: OpenAI-style catalog from the consult route_map.
func (h *ConsultHandler) Models(c *gin.Context) {
	data := make([]gin.H, 0, len(h.cfg.Consult.RouteMap))
	for id := range h.cfg.Consult.RouteMap {
		data = append(data, gin.H{"id": id, "object": "model"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// deny writes a 200 response with allow=false and the supplied status/message.
func deny(c *gin.Context, status int, msg string) {
	c.JSON(http.StatusOK, gin.H{"allow": false, "status": status, "message": msg})
}

// preRequest is the JSON body for POST /consult/pre.
type preRequest struct {
	APIKeyHash string `json:"apiKeyHash"`
	Model      string `json:"model"`
}

// Pre implements POST /consult/pre: validates a team API key and returns routing
// candidates for the executor.
func (h *ConsultHandler) Pre(c *gin.Context) {
	var req preRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		deny(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Model == "" {
		deny(c, http.StatusBadRequest, "Model parameter is required")
		return
	}

	ctx := c.Request.Context()

	// 1. Look up key by hash.
	key, err := h.apiKeys.GetByKeyHash(ctx, req.APIKeyHash)
	if err != nil {
		deny(c, http.StatusUnauthorized, "Invalid API key")
		return
	}

	// 2. Gate: team mode only.
	if !key.IsTeamMode() {
		deny(c, http.StatusForbidden, "not a team key")
		return
	}

	// 3. Guard: user must be present before any subscription lookup.
	if key.User == nil {
		deny(c, 401, "Invalid API key")
		return
	}

	// 4. Optionally load active subscription.
	var sub *service.UserSubscription
	if key.Group != nil && key.Group.IsSubscriptionType() && h.subs != nil {
		sub, _ = h.subs.GetActiveSubscription(ctx, key.User.ID, key.Group.ID)
	}

	// 5. Billing/status gate.
	if ok, status, _, msg := service.ValidateAPIKey(ctx, key, sub, h.cfg); !ok {
		deny(c, status, msg)
		return
	}

	// 6. Route map lookup.
	route, ok := h.cfg.Consult.RouteMap[req.Model]
	if !ok {
		deny(c, http.StatusNotFound, "Unknown model: "+req.Model)
		return
	}

	// 7. Build candidate.
	cand := gin.H{
		"routeId": route.RouteID,
		"format":  route.Format,
	}
	if route.Engine != "" {
		cand["engine"] = route.Engine
	}

	// 8. Build success response.
	resp := gin.H{
		"allow":        true,
		"candidates":   []gin.H{cand},
		"userId":       key.UserID,
		"virtualKeyId": key.ID,
		"spendMode":    "regular",
	}

	if p := h.pricing.GetModelPricing(req.Model); p != nil {
		resp["pricing"] = p
	}

	c.JSON(http.StatusOK, resp)
}

// postReq is the JSON body for POST /consult/post.
type postReq struct {
	Endpoint     string `json:"endpoint"`
	RequestModel string `json:"requestModel"`
	VirtualKeyID int64  `json:"virtualKeyId"`
	Usage        struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// Post implements POST /consult/post: records usage reported by the executor
// into the standard billing path so quota/balance/rate-limits update.
// It is fire-and-forget from the caller's perspective; it always returns {"ok":true}.
func (h *ConsultHandler) Post(c *gin.Context) {
	var req postReq
	// Tolerant bind: ignore parse errors and still proceed.
	_ = c.ShouldBindJSON(&req)

	ctx := c.Request.Context()

	// Look up the virtual key; bail out (best-effort) on any error.
	key, err := h.apiKeys.GetByID(ctx, req.VirtualKeyID)
	if err != nil || key == nil || key.User == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// Fix 4: skip recording when requestModel is empty — a blank model would
	// produce a meaningless usage log.
	if req.RequestModel == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// Load placeholder account.
	acc, _ := h.accounts.GetByID(ctx, h.cfg.Consult.PlaceholderAccountID)

	// Fix 1: guard nil placeholder account — RecordUsage/recordUsageCore dereferences
	// account.ID without nil-guards, so a nil Account would panic the billing path.
	if acc == nil {
		slog.Warn("[Consult Post] misconfigured placeholder account id — skipping billing",
			"placeholderAccountID", h.cfg.Consult.PlaceholderAccountID,
			"requestModel", req.RequestModel,
			"virtualKeyId", req.VirtualKeyID,
		)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	res := &service.ForwardResult{
		Model: req.RequestModel,
		Usage: service.ClaudeUsage{
			InputTokens:              req.Usage.PromptTokens,
			OutputTokens:             req.Usage.CompletionTokens,
			CacheReadInputTokens:     req.Usage.CacheReadTokens,
			CacheCreationInputTokens: req.Usage.CacheCreationTokens,
		},
	}

	// Fix 3: log RecordUsage errors instead of discarding them.
	if err := h.gateway.RecordUsage(ctx, &service.RecordUsageInput{
		Result:          res,
		APIKey:          key,
		User:            key.User,
		Account:         acc,
		InboundEndpoint: req.Endpoint,
		APIKeyService:   h.apiKeys,
		QuotaPlatform:   service.PlatformFromAPIKey(key),
	}); err != nil {
		slog.Warn("[Consult Post] RecordUsage failed",
			"error", err,
			"requestModel", req.RequestModel,
			"virtualKeyId", req.VirtualKeyID,
		)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
