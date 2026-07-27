package middleware

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/e2ee"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// E2EE chat completions transport (used by the /proof page real-inference
// demo). Opt-in per request via headers; absent the version header the
// middleware is a strict no-op, so existing clients are unaffected.
//
// Contract (mirrors deploy/attestor/server.mjs and
// frontend/src/utils/attestation/e2ee.ts):
//
//	request:  X-E2EE-Version: 1, X-Client-Pub-Key: <hex 65B uncompressed
//	          secp256k1>, body {"payload":"<hex ECIES blob>"} sealed to the
//	          enclave key with AAD "v1|req|". The decrypted plaintext is a
//	          normal chat completions JSON (stream:true is rejected) and is
//	          handed to the regular handler chain — auth, billing and
//	          upstream routing are untouched.
//	response: on HTTP 200 the full response body is sealed to the client key
//	          with AAD "v1|resp|" and rewritten as {"payload":"<hex>"} plus
//	          X-E2EE-Version: 1; non-200 responses pass through as plaintext
//	          so the client can classify failures by status code.
const (
	e2eeVersionHeader   = "X-E2EE-Version"
	e2eeVersion         = "1"
	e2eeClientPubHeader = "X-Client-Pub-Key"
)

// e2eeWireEnvelope is the outer body in both directions: hex ECIES blob.
type e2eeWireEnvelope struct {
	Payload string `json:"payload"`
}

// E2EEChatCompletions returns the E2EE transport middleware for POST
// /chat/completions routes. keys supplies the enclave private key
// (production: e2ee.NewDstackKeyProvider(); tests: a static provider).
func E2EEChatCompletions(keys e2ee.KeyProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost ||
			!strings.Contains(c.Request.URL.Path, "/chat/completions") ||
			c.GetHeader(e2eeVersionHeader) != e2eeVersion {
			c.Next()
			return
		}

		clientPub, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(c.GetHeader(e2eeClientPubHeader), "0x"), "0X"))
		if err != nil || len(clientPub) == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": e2eeClientPubHeader + " header is required (hex 65-byte uncompressed secp256k1 key)"})
			return
		}
		if _, err := e2ee.ParsePublicKey(clientPub); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": e2eeClientPubHeader + ": " + err.Error()})
			return
		}

		priv, err := keys.PrivateKey(c.Request.Context())
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "e2ee unavailable: " + err.Error()})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status := http.StatusBadRequest
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"error": "failed to read request body"})
			return
		}
		var envelope e2eeWireEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "body must be JSON {\"payload\":\"<hex>\"}"})
			return
		}
		wire, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(envelope.Payload, "0x"), "0X"))
		if err != nil || len(wire) == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "payload must be a hex-encoded ECIES blob"})
			return
		}
		plaintext, err := e2ee.Decrypt(priv, wire, e2ee.AADRequest)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "payload decryption failed: " + err.Error()})
			return
		}
		if gjson.GetBytes(plaintext, "stream").Bool() {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "stream:true is not supported over the E2EE transport"})
			return
		}

		// Hand the decrypted request to the normal handler chain.
		c.Request.Body = io.NopCloser(bytes.NewReader(plaintext))
		c.Request.ContentLength = int64(len(plaintext))
		c.Request.Header.Set("Content-Length", strconv.Itoa(len(plaintext)))
		// The sealed response must not be transport-compressed under the
		// caller's original Accept-Encoding expectations.
		c.Request.Header.Set("Accept-Encoding", "identity")

		w := &e2eeResponseWriter{ResponseWriter: c.Writer}
		c.Writer = w
		c.Next()
		c.Writer = w.ResponseWriter
		w.finalize(c, clientPub)
	}
}

// e2eeResponseWriter buffers the downstream response so a 200 body can be
// sealed to the client key before anything reaches the socket.
type e2eeResponseWriter struct {
	gin.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (w *e2eeResponseWriter) WriteHeader(code int) {
	if code > 0 {
		w.status = code
	}
}

// WriteHeaderNow is deferred until finalize.
func (w *e2eeResponseWriter) WriteHeaderNow() {}

func (w *e2eeResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *e2eeResponseWriter) WriteString(s string) (int, error) {
	return w.buf.WriteString(s)
}

func (w *e2eeResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *e2eeResponseWriter) Size() int { return w.buf.Len() }

func (w *e2eeResponseWriter) Written() bool { return w.status != 0 || w.buf.Len() > 0 }

// Flush buffers; the sealed body is flushed once in finalize.
func (w *e2eeResponseWriter) Flush() {}

// finalize writes the captured response to the real writer: 200 bodies are
// ECIES-sealed to clientPub, everything else passes through as plaintext.
func (w *e2eeResponseWriter) finalize(c *gin.Context, clientPub []byte) {
	out := w.ResponseWriter
	status := w.Status()
	if status != http.StatusOK {
		out.WriteHeader(status)
		if w.buf.Len() > 0 {
			_, _ = out.Write(w.buf.Bytes())
		}
		return
	}

	sealed, err := e2ee.Encrypt(clientPub, w.buf.Bytes(), e2ee.AADResponse)
	if err != nil {
		writePlainError(out, http.StatusInternalServerError, "e2ee response encryption failed")
		return
	}
	body, err := json.Marshal(e2eeWireEnvelope{Payload: hex.EncodeToString(sealed)})
	if err != nil {
		writePlainError(out, http.StatusInternalServerError, "e2ee response encoding failed")
		return
	}
	header := out.Header()
	header.Del("Content-Encoding")
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Set(e2eeVersionHeader, e2eeVersion)
	out.WriteHeader(http.StatusOK)
	_, _ = out.Write(body)
}

func writePlainError(out gin.ResponseWriter, status int, msg string) {
	header := out.Header()
	header.Del("Content-Encoding")
	header.Set("Content-Type", "application/json")
	body, _ := json.Marshal(gin.H{"error": msg})
	header.Set("Content-Length", strconv.Itoa(len(body)))
	out.WriteHeader(status)
	_, _ = out.Write(body)
}
