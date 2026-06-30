package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterDataPlaneDisabled mounts hard 410 Gone stubs on every data-plane proxy
// path that RegisterGatewayRoutes would otherwise serve. This is intentional and
// UNCONDITIONAL (NOT a runtime flag): in this build sub2api is a pure control
// plane and MUST NOT forward user requests to any upstream third-party account.
// The real gateway proxy handlers remain compiled but have no route pointing at
// them, so they are unreachable dead code. All data-plane traffic must go through
// the TEE gateway (private-ai-gateway).
//
// This function is a TRUE SUPERSET of every route RegisterGatewayRoutes mounts,
// covering all of the following data-plane prefixes:
//
//	/v1/*                    (Claude/OpenAI v1 API group)
//	/v1beta/*                (Gemini native API group)
//	/responses               (bare alias, GET+POST)
//	/responses/*             (bare alias with subpath)
//	/chat/completions        (bare OpenAI chat alias)
//	/embeddings              (bare OpenAI embeddings alias)
//	/images/*                (bare /images/generations and /images/edits)
//	/backend-api/codex/*     (Codex direct alias)
//	/antigravity/*           (antigravity models + /antigravity/v1/* + /antigravity/v1beta/*)
func RegisterDataPlaneDisabled(r *gin.Engine) {
	gone := func(c *gin.Context) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{
				"code":    "DATA_PLANE_DISABLED",
				"message": "sub2api is control-plane only; route requests through the TEE gateway",
			},
		})
	}
	// Mirror every prefix RegisterGatewayRoutes mounts.
	r.Any("/v1/*path", gone)
	r.Any("/v1beta/*path", gone)
	// /responses bare: gateway.go registers GET + POST; r.Any covers both plus all
	// other HTTP methods (PUT, DELETE, PATCH, …) so non-standard methods also 410.
	// Gin v1.9.1 allows an exact route and a catch-all on the same path prefix.
	r.Any("/responses", gone)
	r.Any("/responses/*subpath", gone)
	// Bare aliases missing from the original stub:
	r.Any("/chat/completions", gone)
	r.Any("/embeddings", gone)
	r.Any("/images/*path", gone) // covers /images/generations and /images/edits
	r.Any("/backend-api/codex/*path", gone)
	r.Any("/antigravity/*path", gone) // covers /antigravity/models, /antigravity/v1/*, /antigravity/v1beta/*
}
