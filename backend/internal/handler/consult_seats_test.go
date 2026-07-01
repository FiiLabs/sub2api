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
