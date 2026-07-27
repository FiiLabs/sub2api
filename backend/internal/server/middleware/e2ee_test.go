package middleware

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/e2ee"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type staticKeyProvider struct {
	priv *secp256k1.PrivateKey
	err  error
}

func (p *staticKeyProvider) PrivateKey(context.Context) (*secp256k1.PrivateKey, error) {
	return p.priv, p.err
}

type e2eeTestEnv struct {
	router     *gin.Engine
	serverPriv *secp256k1.PrivateKey
	serverPub  []byte
	clientPriv *secp256k1.PrivateKey
	clientPub  []byte
}

func newE2EETestEnv(t *testing.T, handler gin.HandlerFunc) *e2eeTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	serverPriv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	clientPriv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)

	r := gin.New()
	r.POST("/v1/chat/completions", E2EEChatCompletions(&staticKeyProvider{priv: serverPriv}), handler)
	return &e2eeTestEnv{
		router:     r,
		serverPriv: serverPriv,
		serverPub:  serverPriv.PubKey().SerializeUncompressed(),
		clientPriv: clientPriv,
		clientPub:  clientPriv.PubKey().SerializeUncompressed(),
	}
}

// sealRequest builds an E2EE request envelope for plaintext.
func (env *e2eeTestEnv) sealRequest(t *testing.T, plaintext string) *bytes.Reader {
	t.Helper()
	wire, err := e2ee.Encrypt(env.serverPub, []byte(plaintext), e2ee.AADRequest)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"payload": hex.EncodeToString(wire)})
	require.NoError(t, err)
	return bytes.NewReader(body)
}

func (env *e2eeTestEnv) do(t *testing.T, body io.Reader, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(e2eeVersionHeader, e2eeVersion)
	req.Header.Set(e2eeClientPubHeader, hex.EncodeToString(env.clientPub))
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestE2EEChatCompletionsRoundTrip(t *testing.T) {
	const requestJSON = `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"你好"}],"stream":false}`
	responseJSON := map[string]any{"id": "chatcmpl-1", "object": "chat.completion"}

	var seenBody []byte
	var seenContentLength int64
	env := newE2EETestEnv(t, func(c *gin.Context) {
		var err error
		seenBody, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		seenContentLength = c.Request.ContentLength
		// Authorization must survive the transport untouched.
		require.Equal(t, "Bearer sk-test", c.GetHeader("Authorization"))
		c.JSON(http.StatusOK, responseJSON)
	})

	rec := env.do(t, env.sealRequest(t, requestJSON), func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer sk-test")
	})

	// Handler saw the decrypted plaintext with a consistent length.
	require.Equal(t, requestJSON, string(seenBody))
	require.Equal(t, int64(len(requestJSON)), seenContentLength)

	// Response is a sealed envelope.
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, e2eeVersion, rec.Header().Get(e2eeVersionHeader))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var envelope struct {
		Payload string `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	wire, err := hex.DecodeString(envelope.Payload)
	require.NoError(t, err)
	plaintext, err := e2ee.Decrypt(env.clientPriv, wire, e2ee.AADResponse)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(plaintext, &got))
	require.Equal(t, "chatcmpl-1", got["id"])
}

func TestE2EEChatCompletionsNoHeaderPassthrough(t *testing.T) {
	const body = `{"model":"m","stream":true}`
	env := newE2EETestEnv(t, func(c *gin.Context) {
		got, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(got))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := env.do(t, bytes.NewReader([]byte(body)), func(r *http.Request) {
		r.Header.Del(e2eeVersionHeader)
		r.Header.Del(e2eeClientPubHeader)
	})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get(e2eeVersionHeader))
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())
}

func TestE2EEChatCompletionsMissingOrBadClientKey(t *testing.T) {
	env := newE2EETestEnv(t, func(c *gin.Context) {
		t.Fatal("handler must not run")
	})

	// Missing header.
	rec := env.do(t, env.sealRequest(t, `{}`), func(r *http.Request) {
		r.Header.Del(e2eeClientPubHeader)
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "error")

	// Not hex.
	rec = env.do(t, env.sealRequest(t, `{}`), func(r *http.Request) {
		r.Header.Set(e2eeClientPubHeader, "zz-not-hex")
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Compressed (33B) key instead of uncompressed 65B.
	rec = env.do(t, env.sealRequest(t, `{}`), func(r *http.Request) {
		r.Header.Set(e2eeClientPubHeader, hex.EncodeToString(env.clientPriv.PubKey().SerializeCompressed()))
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestE2EEChatCompletionsBadPayload(t *testing.T) {
	env := newE2EETestEnv(t, func(c *gin.Context) {
		t.Fatal("handler must not run")
	})

	// Not JSON.
	rec := env.do(t, bytes.NewReader([]byte("not-json")), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Payload not hex.
	rec = env.do(t, bytes.NewReader([]byte(`{"payload":"zz"}`)), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Valid hex but undecryptable garbage.
	rec = env.do(t, bytes.NewReader([]byte(`{"payload":"`+hex.EncodeToString(bytes.Repeat([]byte{0x04}, 100))+`"}`)), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Sealed with the wrong AAD direction.
	wire, err := e2ee.Encrypt(env.serverPub, []byte(`{}`), e2ee.AADResponse)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"payload": hex.EncodeToString(wire)})
	require.NoError(t, err)
	rec = env.do(t, bytes.NewReader(body), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestE2EEChatCompletionsRejectsStreaming(t *testing.T) {
	env := newE2EETestEnv(t, func(c *gin.Context) {
		t.Fatal("handler must not run")
	})
	rec := env.do(t, env.sealRequest(t, `{"model":"m","stream":true}`), nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "stream")
	require.Empty(t, rec.Header().Get(e2eeVersionHeader))
}

func TestE2EEChatCompletionsNon200PassesThroughPlaintext(t *testing.T) {
	env := newE2EETestEnv(t, func(c *gin.Context) {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": gin.H{"type": "insufficient_quota"}})
	})
	rec := env.do(t, env.sealRequest(t, `{"model":"m"}`), nil)
	require.Equal(t, http.StatusPaymentRequired, rec.Code)
	require.Empty(t, rec.Header().Get(e2eeVersionHeader))
	require.Contains(t, rec.Body.String(), "insufficient_quota")
}

func TestE2EEChatCompletionsKeyUnavailable503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", E2EEChatCompletions(&staticKeyProvider{err: errors.New("dstack socket down")}), func(c *gin.Context) {
		t.Fatal("handler must not run")
	})

	clientPriv, err := secp256k1.GeneratePrivateKey()
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"payload":"00"}`)))
	req.Header.Set(e2eeVersionHeader, e2eeVersion)
	req.Header.Set(e2eeClientPubHeader, hex.EncodeToString(clientPriv.PubKey().SerializeUncompressed()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "e2ee unavailable: dstack socket down")
}
