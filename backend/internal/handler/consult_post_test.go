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

// ---- fakes for Post tests -----------------------------------------------

// fakePostAPIKeys implements the extended consultAPIKeys (including quota updater stubs).
// It is separate from fakeAPIKeys in consult_pre_test.go so each test controls its own data.
type fakePostAPIKeys struct {
	byID map[int64]*service.APIKey
}

func (f *fakePostAPIKeys) GetByKeyHash(_ context.Context, hash string) (*service.APIKey, error) {
	return nil, service.ErrAPIKeyNotFound
}

func (f *fakePostAPIKeys) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	if k, ok := f.byID[id]; ok {
		return k, nil
	}
	return nil, service.ErrAPIKeyNotFound
}

func (f *fakePostAPIKeys) UpdateQuotaUsed(_ context.Context, _ int64, _ float64) error {
	return nil
}

func (f *fakePostAPIKeys) UpdateRateLimitUsage(_ context.Context, _ int64, _ float64) error {
	return nil
}

// fakeConsultAccounts implements consultAccounts (narrow seam).
type fakeConsultAccounts struct {
	acc *service.Account
}

func (f *fakeConsultAccounts) GetByID(_ context.Context, _ int64) (*service.Account, error) {
	return f.acc, nil
}

// fakeConsultUsage implements consultUsage and records the last call.
type fakeConsultUsage struct {
	called int
	last   *service.RecordUsageInput
}

func (f *fakeConsultUsage) RecordUsage(_ context.Context, in *service.RecordUsageInput) error {
	f.called++
	f.last = in
	return nil
}

// ---- helpers ---------------------------------------------------------------

// postPost fires POST /consult/post and returns the recorder.
func postPost(t *testing.T, h *ConsultHandler, body any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/consult/post", h.Post)

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/consult/post", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)
	return w
}

// ---- tests -----------------------------------------------------------------

// TestConsultPost_RecordsUsage: valid virtualKeyId with non-nil User → 200 ok=true,
// RecordUsage called once with correct APIKey/User/Usage tokens.
func TestConsultPost_RecordsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{ID: 42, Status: "active", Balance: 100}
	teamKey := &service.APIKey{
		ID:     7,
		UserID: 42,
		TeamID: teamIDPtr(3),
		Status: "active",
		User:   user,
	}

	apiKeys := &fakePostAPIKeys{
		byID: map[int64]*service.APIKey{7: teamKey},
	}
	acc := &service.Account{ID: 99}
	accounts := &fakeConsultAccounts{acc: acc}
	gw := &fakeConsultUsage{}

	cfg := &config.Config{}
	cfg.Consult.PlaceholderAccountID = 99

	h := &ConsultHandler{
		apiKeys:  apiKeys,
		accounts: accounts,
		gateway:  gw,
		cfg:      cfg,
	}

	body := map[string]any{
		"endpoint":     "/v1/messages",
		"requestModel": "claude-3-5-sonnet",
		"virtualKeyId": int64(7),
		"usage": map[string]any{
			"prompt_tokens":               100,
			"completion_tokens":           50,
			"cache_read_input_tokens":     10,
			"cache_creation_input_tokens": 5,
		},
	}

	w := postPost(t, h, body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])

	// RecordUsage must have been called exactly once.
	require.Equal(t, 1, gw.called, "RecordUsage should be called once")

	in := gw.last
	require.NotNil(t, in)
	assert.Equal(t, teamKey, in.APIKey, "APIKey mismatch")
	assert.Equal(t, user, in.User, "User mismatch")
	assert.Equal(t, "claude-3-5-sonnet", in.Result.Model)
	assert.Equal(t, 100, in.Result.Usage.InputTokens, "InputTokens mismatch")
	assert.Equal(t, 50, in.Result.Usage.OutputTokens, "OutputTokens mismatch")
	assert.Equal(t, 10, in.Result.Usage.CacheReadInputTokens, "CacheReadInputTokens mismatch")
	assert.Equal(t, 5, in.Result.Usage.CacheCreationInputTokens, "CacheCreationInputTokens mismatch")
	assert.Equal(t, "/v1/messages", in.InboundEndpoint)
}

// TestConsultPost_NilPlaceholderAccount: GetByID on accounts returns (nil, nil) →
// 200 ok=true AND RecordUsage NOT called (no panic).
func TestConsultPost_NilPlaceholderAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{ID: 55, Status: "active", Balance: 100}
	teamKey := &service.APIKey{
		ID:     11,
		UserID: 55,
		TeamID: teamIDPtr(4),
		Status: "active",
		User:   user,
	}

	apiKeys := &fakePostAPIKeys{
		byID: map[int64]*service.APIKey{11: teamKey},
	}
	// accounts returns nil, nil — simulating a misconfigured PlaceholderAccountID.
	accounts := &fakeConsultAccounts{acc: nil}
	gw := &fakeConsultUsage{}

	cfg := &config.Config{}
	cfg.Consult.PlaceholderAccountID = 999 // points to a non-existent row

	h := &ConsultHandler{
		apiKeys:  apiKeys,
		accounts: accounts,
		gateway:  gw,
		cfg:      cfg,
	}

	body := map[string]any{
		"endpoint":     "/v1/messages",
		"requestModel": "claude-3-5-sonnet",
		"virtualKeyId": int64(11),
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
		},
	}

	w := postPost(t, h, body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])

	// RecordUsage must NOT be called when placeholder account is nil.
	assert.Equal(t, 0, gw.called, "RecordUsage must NOT be called when account is nil")
}

// TestConsultPost_EmptyModel: requestModel is empty → 200 ok=true, RecordUsage NOT called.
func TestConsultPost_EmptyModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{ID: 66, Status: "active", Balance: 100}
	teamKey := &service.APIKey{
		ID:     22,
		UserID: 66,
		TeamID: teamIDPtr(5),
		Status: "active",
		User:   user,
	}

	apiKeys := &fakePostAPIKeys{
		byID: map[int64]*service.APIKey{22: teamKey},
	}
	acc := &service.Account{ID: 99}
	accounts := &fakeConsultAccounts{acc: acc}
	gw := &fakeConsultUsage{}

	cfg := &config.Config{}
	cfg.Consult.PlaceholderAccountID = 99

	h := &ConsultHandler{
		apiKeys:  apiKeys,
		accounts: accounts,
		gateway:  gw,
		cfg:      cfg,
	}

	body := map[string]any{
		"endpoint":     "/v1/messages",
		"requestModel": "", // empty model
		"virtualKeyId": int64(22),
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
		},
	}

	w := postPost(t, h, body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])

	// RecordUsage must NOT be called when requestModel is empty.
	assert.Equal(t, 0, gw.called, "RecordUsage must NOT be called when requestModel is empty")
}

// TestConsultPost_KeyNotFound: GetByID returns not-found → 200 ok=true, RecordUsage NOT called.
func TestConsultPost_KeyNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	apiKeys := &fakePostAPIKeys{
		byID: map[int64]*service.APIKey{}, // empty — any lookup returns not-found
	}
	gw := &fakeConsultUsage{}

	cfg := &config.Config{}
	cfg.Consult.PlaceholderAccountID = 99

	h := &ConsultHandler{
		apiKeys: apiKeys,
		gateway: gw,
		cfg:     cfg,
	}

	body := map[string]any{
		"virtualKeyId": int64(999),
		"requestModel": "claude-3-5-sonnet",
	}

	w := postPost(t, h, body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])

	assert.Equal(t, 0, gw.called, "RecordUsage must NOT be called when key not found")
}
