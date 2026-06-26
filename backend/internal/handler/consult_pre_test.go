package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ---- fakes ---------------------------------------------------------------

// fakeAPIKeys implements consultAPIKeys.
type fakeAPIKeys struct {
	byHash map[string]*service.APIKey
	byID   map[int64]*service.APIKey
}

func (f *fakeAPIKeys) GetByKeyHash(_ context.Context, hash string) (*service.APIKey, error) {
	if k, ok := f.byHash[hash]; ok {
		return k, nil
	}
	return nil, service.ErrAPIKeyNotFound
}

func (f *fakeAPIKeys) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	if k, ok := f.byID[id]; ok {
		return k, nil
	}
	return nil, service.ErrAPIKeyNotFound
}

func (f *fakeAPIKeys) UpdateQuotaUsed(_ context.Context, _ int64, _ float64) error {
	return nil
}

func (f *fakeAPIKeys) UpdateRateLimitUsage(_ context.Context, _ int64, _ float64) error {
	return nil
}

// fakeSubs implements consultSubs.
type fakeSubs struct {
	sub *service.UserSubscription
	err error
}

func (f *fakeSubs) GetActiveSubscription(_ context.Context, _, _ int64) (*service.UserSubscription, error) {
	return f.sub, f.err
}

// fakePricing implements consultPricing.
type fakePricing struct {
	data map[string]*service.LiteLLMModelPricing
}

func (f *fakePricing) GetModelPricing(model string) *service.LiteLLMModelPricing {
	return f.data[model]
}

// ---- helpers -------------------------------------------------------------

func newTestConsultHandler(
	apiKeys consultAPIKeys,
	subs consultSubs,
	pricing consultPricing,
	cfg *config.Config,
) *ConsultHandler {
	return &ConsultHandler{
		apiKeys: apiKeys,
		subs:    subs,
		pricing: pricing,
		cfg:     cfg,
	}
}

func makeConsultConfig(routes map[string]config.ConsultRoute) *config.Config {
	cfg := &config.Config{}
	cfg.Consult.RouteMap = routes
	return cfg
}

// teamID is a helper to get a pointer to an int64, used to set APIKey.TeamID.
func teamIDPtr(v int64) *int64 { return &v }

// postPre fires POST /consult/pre and returns the recorder.
func postPre(t *testing.T, h *ConsultHandler, body any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/consult/pre", h.Pre)

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/consult/pre", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)
	return w
}

// ---- tests ---------------------------------------------------------------

// TestConsultPre_UnknownHash: fake returns ErrAPIKeyNotFound → allow=false, status=401.
func TestConsultPre_UnknownHash(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	})

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{}},
		nil,
		&fakePricing{},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "deadbeef",
		"model":      "m2",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, false, resp["allow"])
	assert.Equal(t, float64(401), resp["status"])
}

// TestConsultPre_PersonalKey: IsTeamMode false (TeamID nil) → allow=false, status=403.
func TestConsultPre_PersonalKey(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	})

	personalKey := &service.APIKey{
		ID:     1,
		UserID: 10,
		TeamID: nil, // personal key — IsTeamMode() == false
		Status: "active",
		User:   &service.User{ID: 10, Status: "active", Balance: 100},
	}

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"hash-personal": personalKey}},
		nil,
		&fakePricing{},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "hash-personal",
		"model":      "m2",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, false, resp["allow"])
	assert.Equal(t, float64(403), resp["status"])
	assert.Equal(t, "not a team key", resp["message"])
}

// TestConsultPre_ModelNotInRouteMap: team key + model NOT in route_map → allow=false, status=404.
func TestConsultPre_ModelNotInRouteMap(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	})

	teamKey := &service.APIKey{
		ID:     2,
		UserID: 20,
		TeamID: teamIDPtr(5),
		Status: "active",
		User:   &service.User{ID: 20, Status: "active", Balance: 100},
	}

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"hash-team": teamKey}},
		nil,
		&fakePricing{},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "hash-team",
		"model":      "unknown-model",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, false, resp["allow"])
	assert.Equal(t, float64(404), resp["status"])
}

// TestConsultPre_ValidationDenied: a TEAM key whose ValidateAPIKey fails (inactive user) →
// allow=false, status=401 (USER_INACTIVE path in ValidateAPIKey).
func TestConsultPre_ValidationDenied(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	})

	// TEAM key (TeamID set, User non-nil) but user is inactive (Status "disabled", Balance 0).
	// ValidateAPIKey check 4: !k.User.IsActive() → returns false, 401, "USER_INACTIVE".
	deniedKey := &service.APIKey{
		ID:     9,
		UserID: 90,
		TeamID: teamIDPtr(3),
		Status: "active",
		User:   &service.User{ID: 90, Status: "disabled", Balance: 0},
	}

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"hash-denied": deniedKey}},
		nil,
		&fakePricing{},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "hash-denied",
		"model":      "m2",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, false, resp["allow"])
	assert.Equal(t, float64(401), resp["status"])
}

// TestConsultPre_Success: team key + active user + balance>0 + model in route_map → allow=true.
func TestConsultPre_Success(t *testing.T) {
	cfg := makeConsultConfig(map[string]config.ConsultRoute{
		"m2": {RouteID: "minimax:m2", Format: "openai"},
	})

	teamKey := &service.APIKey{
		ID:     3,
		UserID: 30,
		TeamID: teamIDPtr(7),
		Status: "active",
		User:   &service.User{ID: 30, Status: "active", Balance: 50},
	}

	h := newTestConsultHandler(
		&fakeAPIKeys{byHash: map[string]*service.APIKey{"hash-ok": teamKey}},
		nil,
		&fakePricing{
			data: map[string]*service.LiteLLMModelPricing{
				"m2": {InputCostPerToken: 0.001, OutputCostPerToken: 0.002},
			},
		},
		cfg,
	)

	w := postPre(t, h, map[string]string{
		"apiKeyHash": "hash-ok",
		"model":      "m2",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, true, resp["allow"])

	candidates, ok := resp["candidates"].([]any)
	require.True(t, ok, "candidates should be an array")
	require.Len(t, candidates, 1)

	cand, ok := candidates[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "minimax:m2", cand["routeId"])
	assert.Equal(t, "openai", cand["format"])

	assert.Equal(t, float64(30), resp["userId"])
	assert.Equal(t, float64(3), resp["virtualKeyId"])
	assert.Equal(t, "regular", resp["spendMode"])

	// pricing should be present
	assert.NotNil(t, resp["pricing"])
}
