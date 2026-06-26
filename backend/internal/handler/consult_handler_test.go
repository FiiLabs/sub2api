package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
