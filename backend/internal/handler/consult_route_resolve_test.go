package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ---------------------------------------------------------------------------
// Unit tests for resolveConsultRoute
// ---------------------------------------------------------------------------

// TestResolveConsultRoute_ExactBeatsWildcard verifies that an exact key wins
// over a prefix wildcard when both are present.
func TestResolveConsultRoute_ExactBeatsWildcard(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"claude-sonnet": {RouteID: "exact-route", Format: "openai"},
		"claude-*":      {RouteID: "wildcard-route", Format: "openai"},
	}

	route, ok := resolveConsultRoute(m, "claude-sonnet")
	require.True(t, ok)
	assert.Equal(t, "exact-route", route.RouteID)
}

// TestResolveConsultRoute_PrefixWildcard verifies that a prefix wildcard
// matches a model not listed as an exact key.
func TestResolveConsultRoute_PrefixWildcard(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"claude-*": {RouteID: "claude-wildcard", Format: "openai"},
	}

	route, ok := resolveConsultRoute(m, "claude-sonnet-4-6")
	require.True(t, ok)
	assert.Equal(t, "claude-wildcard", route.RouteID)
}

// TestResolveConsultRoute_LongestPrefixWins verifies that among multiple
// matching prefix wildcards the one with the longest prefix is chosen.
func TestResolveConsultRoute_LongestPrefixWins(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"claude-*":        {RouteID: "claude-generic", Format: "openai"},
		"claude-sonnet-*": {RouteID: "claude-sonnet-specific", Format: "openai"},
	}

	route, ok := resolveConsultRoute(m, "claude-sonnet-4-6")
	require.True(t, ok)
	assert.Equal(t, "claude-sonnet-specific", route.RouteID)
}

// TestResolveConsultRoute_CatchAllFallback verifies that the catch-all "*"
// key is used only when no exact key and no prefix wildcard matches.
func TestResolveConsultRoute_CatchAllFallback(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"gpt-4": {RouteID: "gpt4-exact", Format: "openai"},
		"*":     {RouteID: "catch-all-route", Format: "openai"},
	}

	route, ok := resolveConsultRoute(m, "some-unknown-model")
	require.True(t, ok)
	assert.Equal(t, "catch-all-route", route.RouteID)
}

// TestResolveConsultRoute_NoMatch verifies that when no exact, prefix, or
// catch-all entry matches the resolver returns false.
func TestResolveConsultRoute_NoMatch(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"gpt-4":    {RouteID: "gpt4-exact", Format: "openai"},
		"claude-*": {RouteID: "claude-wildcard", Format: "openai"},
	}

	_, ok := resolveConsultRoute(m, "completely-unknown")
	assert.False(t, ok)
}

// TestResolveConsultRoute_CatchAllNotUsedWhenPrefixMatches verifies that
// the catch-all is skipped when a prefix wildcard already matches.
func TestResolveConsultRoute_CatchAllNotUsedWhenPrefixMatches(t *testing.T) {
	m := map[string]config.ConsultRoute{
		"claude-*": {RouteID: "claude-wildcard", Format: "openai"},
		"*":        {RouteID: "catch-all-route", Format: "openai"},
	}

	route, ok := resolveConsultRoute(m, "claude-haiku-3-5")
	require.True(t, ok)
	assert.Equal(t, "claude-wildcard", route.RouteID)
}

// ---------------------------------------------------------------------------
// End-to-end Pre test: wildcard entry → allow:true with correct routeId
// ---------------------------------------------------------------------------

// TestConsultPre_WildcardRouteAllowed verifies that a request with a model
// matched only by a prefix wildcard entry is allowed and returns the correct
// routeId.
func TestConsultPre_WildcardRouteAllowed(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-*": {RouteID: "claude-wildcard-route", Format: "openai"},
	})

	teamKey := &service.APIKey{
		ID:     42,
		UserID: 42,
		TeamID: teamIDPtr(7),
		Status: "active",
		User:   &service.User{ID: 42, Status: "active", Balance: 100},
	}

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"hash-wildcard": teamKey}},
		nil,
		&fakePricing{},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "hash-wildcard",
		"model":      "claude-sonnet-4-6",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, true, body["allow"], "expected allow=true for wildcard match")

	candidates, ok := body["candidates"].([]any)
	require.True(t, ok, "candidates should be an array")
	require.Len(t, candidates, 1)

	cand, ok := candidates[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "claude-wildcard-route", cand["routeId"])
}

// ---------------------------------------------------------------------------
// Models test: pattern keys excluded from catalog
// ---------------------------------------------------------------------------

// getModels fires GET /models against h and returns the recorder.
func getModels(t *testing.T, h *ConsultHandler) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/models", h.Models)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/models", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	return w
}

// TestConsultHandler_Models_ExcludesPatternKeys verifies that wildcard keys
// ("claude-*", "*") do NOT appear in the /models catalog but concrete keys do.
func TestConsultHandler_Models_ExcludesPatternKeys(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"gpt-4":    {RouteID: "gpt4", Format: "openai"},
		"claude-*": {RouteID: "claude-wildcard", Format: "openai"},
		"*":        {RouteID: "catch-all", Format: "openai"},
	})

	h := newTestConsultHandler(
		&fakeAPIKeys{},
		nil,
		&fakePricing{},
		cfg,
	)

	w := getModels(t, h)
	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	data, ok := body["data"].([]any)
	require.True(t, ok, "data should be an array")

	var ids []string
	for _, item := range data {
		entry, ok := item.(map[string]any)
		require.True(t, ok)
		ids = append(ids, entry["id"].(string))
	}
	sort.Strings(ids)

	assert.Contains(t, ids, "gpt-4", "concrete key gpt-4 should be in catalog")
	assert.NotContains(t, ids, "claude-*", "pattern key claude-* must NOT appear in catalog")
	assert.NotContains(t, ids, "*", "catch-all * must NOT appear in catalog")
}
