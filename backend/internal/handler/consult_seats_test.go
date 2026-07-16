package handler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOrderSeats_RotatesPreservingFailoverOrder(t *testing.T) {
	seats := []string{"a", "b", "c"}
	assert.Equal(t, []string{"a", "b", "c"}, orderSeats(seats, 0))
	assert.Equal(t, []string{"b", "c", "a"}, orderSeats(seats, 1))
	assert.Equal(t, []string{"c", "a", "b"}, orderSeats(seats, 2))
	assert.Equal(t, []string{"a", "b", "c"}, orderSeats(seats, 3))
}

func TestOrderSeats_Empty(t *testing.T) {
	assert.Nil(t, orderSeats(nil, 0))
}

// A key sticks to ONE primary seat across turns (affinity), so meridian's
// per-seat resume cache hits and the leak-prone fresh path is rarely taken.
// The full seat list is still returned for failover, just rotated.
func TestConsultPre_MultiSeatIsKeyAffine(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {
			Seats:  []string{"meridian-seat1", "meridian-seat2"},
			Format: "anthropic",
		},
	})
	validKey := &service.APIKey{ID: 1, UserID: 1, Status: "active",
		User: &service.User{ID: 1, Status: "active", Balance: 100}}
	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"h": validKey}},
		nil, &fakePricing{}, cfg,
	)

	first := preCandidates(t, h, "h", "claude-sonnet-4-6")
	require.Len(t, first, 2, "all seats returned for failover")
	assert.Equal(t, "anthropic", first[0]["format"])
	primary := first[0]["routeId"]
	// Failover list contains both seats regardless of which is primary.
	ids := map[string]bool{first[0]["routeId"].(string): true, first[1]["routeId"].(string): true}
	assert.True(t, ids["meridian-seat1:claude-sonnet-4-6"] && ids["meridian-seat2:claude-sonnet-4-6"])

	// Same key, repeated calls → SAME primary every time (no rotation).
	for i := 0; i < 5; i++ {
		again := preCandidates(t, h, "h", "claude-sonnet-4-6")
		require.Len(t, again, 2)
		assert.Equal(t, primary, again[0]["routeId"], "same key must stay on the same primary seat")
	}
}

// Different keys spread across the available seats (affinity is not "everyone on
// seat1"): both seats appear as primary for some key.
func TestConsultPre_MultiSeatSpreadsAcrossKeys(t *testing.T) {
	seats := []string{"meridian-seat1", "meridian-seat2"}
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {Seats: seats, Format: "anthropic"},
	})
	byHash := map[string]*service.APIKey{}
	for i := 0; i < 24; i++ {
		byHash[keyN(i)] = &service.APIKey{ID: 1, UserID: 1, Status: "active",
			User: &service.User{ID: 1, Status: "active", Balance: 100}}
	}
	h := newTestConsultHandler(&fakeAPIKeys{byHash: byHash}, nil, &fakePricing{}, cfg)

	primaries := map[string]bool{}
	for i := 0; i < 24; i++ {
		cands := preCandidates(t, h, keyN(i), "claude-sonnet-4-6")
		require.Len(t, cands, 2)
		primaries[cands[0]["routeId"].(string)] = true
	}
	assert.Len(t, primaries, 2, "keys should map to both seats as primary, not all to one")
}

func keyN(i int) string { return "key-" + string(rune('a'+i%26)) + string(rune('0'+i/26)) }

// preCandidates fires /consult/pre and returns the candidates array.
func preCandidates(t *testing.T, h *ConsultHandler, hash, model string) []map[string]any {
	t.Helper()
	w := postPre(t, h, map[string]string{"apiKeyHash": hash, "model": model})
	require.Equal(t, 200, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, true, body["allow"])
	raw := body["candidates"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		out = append(out, c.(map[string]any))
	}
	return out
}

func TestOrderSeats_SingleElement(t *testing.T) {
	assert.Equal(t, []string{"a"}, orderSeats([]string{"a"}, 99))
}

func TestConsultPre_SingleSeatAlwaysSameCandidate(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-opus-4-6": {Seats: []string{"only-seat"}, Format: "anthropic"},
	})
	validKey := &service.APIKey{ID: 1, UserID: 1, Status: "active",
		User: &service.User{ID: 1, Status: "active", Balance: 100}}
	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"h": validKey}},
		nil, &fakePricing{}, cfg,
	)
	for i := 0; i < 3; i++ {
		cands := preCandidates(t, h, "h", "claude-opus-4-6")
		require.Len(t, cands, 1)
		assert.Equal(t, "only-seat:claude-opus-4-6", cands[0]["routeId"])
	}
}

func TestConsultPre_WildcardSeatsUseRequestedModel(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-*": {Seats: []string{"meridian-seat1"}, Format: "anthropic"},
	})
	validKey := &service.APIKey{ID: 1, UserID: 1, Status: "active",
		User: &service.User{ID: 1, Status: "active", Balance: 100}}
	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"h": validKey}},
		nil, &fakePricing{}, cfg,
	)
	cands := preCandidates(t, h, "h", "claude-haiku-9")
	require.Len(t, cands, 1)
	assert.Equal(t, "meridian-seat1:claude-haiku-9", cands[0]["routeId"],
		"routeId must use the requested model, not the wildcard key")
}

func TestConsultPre_MultiSeatPropagatesEngine(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {Seats: []string{"s1", "s2"}, Format: "anthropic", Engine: "sglang"},
	})
	validKey := &service.APIKey{ID: 1, UserID: 1, Status: "active",
		User: &service.User{ID: 1, Status: "active", Balance: 100}}
	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"h": validKey}},
		nil, &fakePricing{}, cfg,
	)
	cands := preCandidates(t, h, "h", "claude-sonnet-4-6")
	require.Len(t, cands, 2)
	for _, c := range cands {
		assert.Equal(t, "sglang", c["engine"])
	}
}

// TestConsultPre_HotReloadRouteMapTakesEffect verifies that atomically
// republishing the consult route map (what the config watcher does when a seat
// is added) is picked up by /consult/pre without rebuilding the handler.
func TestConsultPre_HotReloadRouteMapTakesEffect(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {RouteID: "meridian-seat1:claude-sonnet-4-6", Format: "anthropic"},
	})
	validKey := &service.APIKey{ID: 1, UserID: 1, Status: "active",
		User: &service.User{ID: 1, Status: "active", Balance: 100}}
	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"h": validKey}},
		nil, &fakePricing{}, cfg,
	)

	first := preCandidates(t, h, "h", "claude-sonnet-4-6")
	require.Len(t, first, 1)
	assert.Equal(t, "meridian-seat1:claude-sonnet-4-6", first[0]["routeId"])

	// Hot-swap the route map (simulates the watcher after adding seat2).
	cfg.Consult.SetRoutes(map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {Seats: []string{"meridian-seat1", "meridian-seat2"}, Format: "anthropic"},
	})
	after := preCandidates(t, h, "h", "claude-sonnet-4-6")
	require.Len(t, after, 2, "hot-reloaded seats should be reflected without handler rebuild")
}
