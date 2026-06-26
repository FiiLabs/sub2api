package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ConsultHandler handles consult control-plane requests.
type ConsultHandler struct {
	apiKeys  *service.APIKeyService
	subs     *service.SubscriptionService
	pricing  *service.PricingService
	gateway  *service.GatewayService
	accounts service.AccountRepository
	cfg      *config.Config
}

// NewConsultHandler creates a new ConsultHandler.
func NewConsultHandler(
	apiKeys *service.APIKeyService,
	subs *service.SubscriptionService,
	pricing *service.PricingService,
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
