package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// consultAPIKeys is a narrow seam over *service.APIKeyService for unit testing.
type consultAPIKeys interface {
	GetByKeyHash(ctx context.Context, hash string) (*service.APIKey, error)
	GetByID(ctx context.Context, id int64) (*service.APIKey, error)
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
	gateway  *service.GatewayService
	accounts service.AccountRepository
	cfg      *config.Config
}

// NewConsultHandler creates a new ConsultHandler.
func NewConsultHandler(
	apiKeys consultAPIKeys,
	subs consultSubs,
	pricing consultPricing,
	gateway *service.GatewayService,
	accounts service.AccountRepository,
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

	// 3. Optionally load active subscription.
	var sub *service.UserSubscription
	if key.Group != nil && key.Group.IsSubscriptionType() && h.subs != nil {
		userID := int64(0)
		if key.User != nil {
			userID = key.User.ID
		}
		sub, _ = h.subs.GetActiveSubscription(ctx, userID, key.Group.ID)
	}

	// 4. Billing/status gate.
	if ok, status, _, msg := service.ValidateAPIKey(ctx, key, sub, h.cfg); !ok {
		deny(c, status, msg)
		return
	}

	// 5. Route map lookup.
	route, ok := h.cfg.Consult.RouteMap[req.Model]
	if !ok {
		deny(c, http.StatusNotFound, "Unknown model: "+req.Model)
		return
	}

	// 6. Build candidate.
	cand := gin.H{
		"routeId": route.RouteID,
		"format":  route.Format,
	}
	if route.Engine != "" {
		cand["engine"] = route.Engine
	}

	// 7. Build success response.
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
