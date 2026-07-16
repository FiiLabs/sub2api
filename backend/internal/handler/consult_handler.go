package handler

import (
	"context"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

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
	seatRR   sync.Map // requestModel(string) -> *atomic.Uint64
}

// nextSeatIndex returns a monotonically increasing per-key counter value
// (starting at 0), so each model rotates its seats independently.
func (h *ConsultHandler) nextSeatIndex(key string) uint64 {
	v, _ := h.seatRR.LoadOrStore(key, new(atomic.Uint64))
	return v.(*atomic.Uint64).Add(1) - 1
}

// seatStart picks the starting (primary) seat index for a request.
//
// When an API-key hash is present it is affinity-hashed, so the SAME key
// deterministically sticks to the SAME primary seat on every turn of a
// conversation. This is the point: meridian's resume cache is per-seat, so a key
// that keeps landing on one seat hits that cache and takes the RESUME path, which
// preserves real turn structure. Round-robin-per-model instead spread a
// conversation's turns across seats, so the receiving seat never had the session,
// forcing the FRESH path — which flattens history and can make the model continue
// the transcript / emit harness scaffolding into replies (the [Assistant:] /
// <system-reminder> leak). Affinity keeps the fresh path rarely taken.
//
// Falls back to per-model round-robin when no key hash is available. Failover is
// unaffected: orderSeats still returns the full seat list, just rotated so the
// affine seat is first.
func (h *ConsultHandler) seatStart(apiKeyHash, model string) uint64 {
	if apiKeyHash != "" {
		f := fnv.New64a()
		_, _ = f.Write([]byte(apiKeyHash))
		return f.Sum64()
	}
	return h.nextSeatIndex(model)
}

// orderSeats returns seats rotated so seats[start % len] is first, preserving
// the remaining order for deterministic failover.
func orderSeats(seats []string, start uint64) []string {
	n := len(seats)
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	off := int(start % uint64(n))
	for i := 0; i < n; i++ {
		out = append(out, seats[(off+i)%n])
	}
	return out
}

// NewConsultHandler creates a new ConsultHandler.
// Concrete parameter types are required for google/wire DI resolution;
// the interface-typed struct fields allow fake injection in tests.
func NewConsultHandler(
	apiKeys *service.APIKeyService,
	subs *service.SubscriptionService,
	pricing *service.PricingService,
	gateway *service.GatewayService,
	accounts service.AccountRepository,
	cfg *config.Config,
) *ConsultHandler {
	return &ConsultHandler{
		apiKeys:  apiKeys,
		subs:     subs,
		pricing:  pricing,
		gateway:  gateway,
		accounts: accounts,
		cfg:      cfg,
	}
}

// resolveConsultRoute resolves a model name against the route_map with the
// following precedence:
//  1. Exact key match wins.
//  2. Longest matching prefix wildcard (pattern ending with "*") wins among ties.
//  3. Catch-all "*" key is used as a last resort.
//
// Returns the matched ConsultRoute and true, or a zero value and false when
// nothing matches.
func resolveConsultRoute(m map[string]config.ConsultRoute, model string) (config.ConsultRoute, bool) {
	if r, ok := m[model]; ok { // exact wins
		return r, true
	}
	bestLen := -1
	var best config.ConsultRoute
	for pat, r := range m {
		if pat == "*" || !strings.HasSuffix(pat, "*") {
			continue
		}
		prefix := strings.TrimSuffix(pat, "*")
		if strings.HasPrefix(model, prefix) && len(prefix) > bestLen {
			bestLen = len(prefix)
			best = r
		}
	}
	if bestLen >= 0 {
		return best, true
	}
	if r, ok := m["*"]; ok { // catch-all fallback
		return r, true
	}
	return config.ConsultRoute{}, false
}

// consultModelCreated is a stable placeholder creation timestamp for all
// catalog entries returned by the /models endpoint.  Using a fixed value
// avoids spurious diffs on every request.
const consultModelCreated int64 = 1700000000 // stable placeholder creation time for catalog entries

// consultModelOwnedBy derives the "owned_by" value from a route_id by
// returning the substring before the first ":".  If there is no ":" or the
// prefix is empty, it falls back to "sub2api".
//
// Examples:
//
//	"claude:sonnet-4-6"  → "claude"
//	"openai:gpt-4o"      → "openai"
//	"nocolon"            → "sub2api"
func consultModelOwnedBy(routeID string) string {
	if i := strings.Index(routeID, ":"); i > 0 {
		return routeID[:i]
	}
	return "sub2api"
}

// Models implements GET /models: OpenAI-style catalog from the consult route_map.
// Pattern keys (entries containing "*") are excluded — they are routing rules,
// not real model identifiers.
func (h *ConsultHandler) Models(c *gin.Context) {
	routes := h.cfg.Consult.Routes()
	data := make([]gin.H, 0, len(routes))
	for id, route := range routes {
		if strings.Contains(id, "*") {
			continue
		}
		routeID := route.RouteID
		if routeID == "" && len(route.Seats) > 0 {
			routeID = route.Seats[0]
		}
		data = append(data, gin.H{
			"id":       id,
			"object":   "model",
			"created":  consultModelCreated,
			"owned_by": consultModelOwnedBy(routeID),
		})
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

// Pre implements POST /consult/pre: validates an API key and returns routing
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

	// 2. Guard: user must be present before any subscription lookup.
	if key.User == nil {
		deny(c, 401, "Invalid API key")
		return
	}

	// 3. Optionally load active subscription.
	var sub *service.UserSubscription
	if key.Group != nil && key.Group.IsSubscriptionType() && h.subs != nil {
		sub, _ = h.subs.GetActiveSubscription(ctx, key.User.ID, key.Group.ID)
	}

	// 4. Billing/status gate.
	if ok, status, _, msg := service.ValidateAPIKey(ctx, key, sub, h.cfg); !ok {
		deny(c, status, msg)
		return
	}

	// 5. Route map lookup (exact → longest-prefix wildcard → catch-all "*").
	route, ok := resolveConsultRoute(h.cfg.Consult.Routes(), req.Model)
	if !ok {
		deny(c, http.StatusNotFound, "Unknown model: "+req.Model)
		return
	}

	// 6. Build candidate(s): multi-seat round-robin + failover, or single route.
	var candidates []gin.H
	if len(route.Seats) > 0 {
		for _, seat := range orderSeats(route.Seats, h.seatStart(req.APIKeyHash, req.Model)) {
			cand := gin.H{"routeId": seat + ":" + req.Model, "format": route.Format}
			if route.Engine != "" {
				cand["engine"] = route.Engine
			}
			candidates = append(candidates, cand)
		}
	} else {
		cand := gin.H{"routeId": route.RouteID, "format": route.Format}
		if route.Engine != "" {
			cand["engine"] = route.Engine
		}
		candidates = append(candidates, cand)
	}

	// 7. Build success response.
	resp := gin.H{
		"allow":        true,
		"candidates":   candidates,
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
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		// Anthropic-format upstreams (client /v1/messages) report usage as
		// input_tokens/output_tokens rather than prompt_tokens/completion_tokens.
		// The executor forwards raw upstream usage unchanged, so accept both.
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
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

	// Accept OpenAI (prompt/completion) or Anthropic (input/output) token field
	// names — the executor forwards raw upstream usage, whose naming follows the
	// upstream/endpoint format. Fall back so Anthropic /v1/messages bills correctly.
	inputTokens := req.Usage.PromptTokens
	if inputTokens == 0 {
		inputTokens = req.Usage.InputTokens
	}
	outputTokens := req.Usage.CompletionTokens
	if outputTokens == 0 {
		outputTokens = req.Usage.OutputTokens
	}

	res := &service.ForwardResult{
		Model: req.RequestModel,
		Usage: service.ClaudeUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
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
