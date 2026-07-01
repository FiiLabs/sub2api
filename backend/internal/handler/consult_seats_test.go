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

func TestConsultPre_MultiSeatReturnsRotatedCandidates(t *testing.T) {
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
	require.Len(t, first, 2)
	assert.Equal(t, "meridian-seat1:claude-sonnet-4-6", first[0]["routeId"])
	assert.Equal(t, "meridian-seat2:claude-sonnet-4-6", first[1]["routeId"])
	assert.Equal(t, "anthropic", first[0]["format"])

	second := preCandidates(t, h, "h", "claude-sonnet-4-6")
	assert.Equal(t, "meridian-seat2:claude-sonnet-4-6", second[0]["routeId"], "should rotate primary")
}

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
