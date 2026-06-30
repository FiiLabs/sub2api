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
)

func TestConsultHandler_Models(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Consult.RouteMap = map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	}

	h := NewConsultHandler(nil, nil, nil, nil, nil, cfg)

	r := gin.New()
	r.GET("/models", h.Models)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/models", nil)
	require.NoError(t, err)

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, "list", body["object"])

	data, ok := body["data"].([]any)
	require.True(t, ok, "data should be an array")

	found := false
	for _, item := range data {
		entry, ok := item.(map[string]any)
		require.True(t, ok)
		if entry["id"] == "m2" {
			found = true
		}
	}
	assert.True(t, found, "expected model id 'm2' in response data")
}

// TestConsultHandler_Models_CreatedAndOwnedBy verifies that each concrete
// model entry in the /models response includes the standard OpenAI fields
// "created" (stable int64 timestamp) and "owned_by" (derived from the
// RouteID prefix before the first ":"). Pattern keys ("*") are excluded.
func TestConsultHandler_Models_CreatedAndOwnedBy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Consult.RouteMap = map[string]config.ConsultRoute{
		"claude-sonnet-4-6": {RouteID: "claude:sonnet-4-6", Format: "openai"},
		"claude-opus-4-6":   {RouteID: "claude:opus-4-6", Format: "openai"},
		"claude-*":          {RouteID: "claude:sonnet-4-6", Format: "openai"},
	}

	h := NewConsultHandler(nil, nil, nil, nil, nil, cfg)

	r := gin.New()
	r.GET("/models", h.Models)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/models", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, "list", body["object"])

	data, ok := body["data"].([]any)
	require.True(t, ok, "data should be an array")

	// Exactly the 2 concrete entries (pattern key excluded).
	require.Len(t, data, 2, "expected exactly 2 concrete model entries")

	var ids []string
	for _, item := range data {
		entry, ok := item.(map[string]any)
		require.True(t, ok)

		// id must be one of the concrete keys.
		id, ok := entry["id"].(string)
		require.True(t, ok, "id must be a string")
		ids = append(ids, id)

		// object field.
		assert.Equal(t, "model", entry["object"], "object field must be \"model\"")

		// created must equal the stable placeholder timestamp.
		created, ok := entry["created"].(float64) // JSON numbers decode as float64.
		require.True(t, ok, "created must be a number")
		assert.Equal(t, float64(consultModelCreated), created, "created must equal consultModelCreated")

		// owned_by must be "claude" (prefix of "claude:…").
		assert.Equal(t, "claude", entry["owned_by"], "owned_by must be the provider prefix")
	}

	sort.Strings(ids)
	assert.Equal(t, []string{"claude-opus-4-6", "claude-sonnet-4-6"}, ids)
}

// TestConsultModelOwnedBy tests the consultModelOwnedBy helper for various
// RouteID formats.
func TestConsultModelOwnedBy(t *testing.T) {
	cases := []struct {
		routeID string
		want    string
	}{
		{"claude:sonnet-4-6", "claude"},
		{"openai:gpt-4o", "openai"},
		{"minimax:m2", "minimax"},
		{"nocolon", "sub2api"},       // no colon → fallback
		{":empty-prefix", "sub2api"}, // colon at position 0 → fallback
		{"", "sub2api"},              // empty string → fallback
	}
	for _, tc := range cases {
		got := consultModelOwnedBy(tc.routeID)
		assert.Equal(t, tc.want, got, "consultModelOwnedBy(%q)", tc.routeID)
	}
}
