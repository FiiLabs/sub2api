package e2ee

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// dstack guest-agent GetKey RPC (mirrors @phala/dstack-sdk getKey):
//
//	POST /GetKey over the guest-agent unix socket, Content-Type: application/json
//	request  {"path":"e2ee/v1","purpose":"encryption","algorithm":"secp256k1"}
//	response {"key":"<hex>","signature_chain":["<hex>",...]}
//
// The first 32 bytes of the hex-decoded key seed the enclave private key
// (normalized via PrivateKeyFromSeed, same as server.mjs).
const (
	dstackSocketEnv     = "DSTACK_SOCKET"
	dstackSocketDefault = "/var/run/dstack.sock"
	dstackKeyPath       = "e2ee/v1"
	dstackKeyPurpose    = "encryption"
	dstackKeyAlgorithm  = "secp256k1"
	dstackRPCTimeout    = 30 * time.Second
)

// KeyProvider yields the enclave E2EE private key. Implementations must be
// safe for concurrent use; tests substitute a static provider.
type KeyProvider interface {
	PrivateKey(ctx context.Context) (*secp256k1.PrivateKey, error)
}

// DstackKeyProvider derives the key from the dstack guest-agent KMS, lazily,
// caching the result for the process lifetime. Failures are not cached so the
// next request retries (mirrors the promise-reset behavior in server.mjs).
type DstackKeyProvider struct {
	socket string

	mu   sync.Mutex
	priv *secp256k1.PrivateKey
}

// NewDstackKeyProvider uses the DSTACK_SOCKET env var, falling back to
// /var/run/dstack.sock.
func NewDstackKeyProvider() *DstackKeyProvider {
	socket := os.Getenv(dstackSocketEnv)
	if socket == "" {
		socket = dstackSocketDefault
	}
	return &DstackKeyProvider{socket: socket}
}

// PrivateKey returns the cached key, deriving it on first use.
func (p *DstackKeyProvider) PrivateKey(ctx context.Context) (*secp256k1.PrivateKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.priv != nil {
		return p.priv, nil
	}
	seed, err := p.getKeySeed(ctx)
	if err != nil {
		return nil, err
	}
	priv, err := PrivateKeyFromSeed(seed)
	if err != nil {
		return nil, err
	}
	p.priv = priv
	return priv, nil
}

type dstackGetKeyRequest struct {
	Path      string `json:"path"`
	Purpose   string `json:"purpose"`
	Algorithm string `json:"algorithm"`
}

type dstackGetKeyResponse struct {
	Key string `json:"key"`
}

// getKeySeed performs the GetKey RPC over the unix socket.
func (p *DstackKeyProvider) getKeySeed(ctx context.Context) ([]byte, error) {
	payload, err := json.Marshal(dstackGetKeyRequest{
		Path:      dstackKeyPath,
		Purpose:   dstackKeyPurpose,
		Algorithm: dstackKeyAlgorithm,
	})
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: dstackRPCTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", p.socket)
			},
		},
	}
	// Host is a placeholder; the connection goes to the unix socket.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/GetKey", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): %w", p.socket, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): read response: %w", p.socket, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): status %d: %s", p.socket, resp.StatusCode, truncateForError(body))
	}
	var parsed dstackGetKeyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): parse response: %w", p.socket, err)
	}
	key, err := hex.DecodeString(parsed.Key)
	if err != nil {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): key is not valid hex: %w", p.socket, err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("dstack GetKey RPC (%s): key too short (%d bytes)", p.socket, len(key))
	}
	return key[:32], nil
}

func truncateForError(b []byte) string {
	const max = 256
	if len(b) > max {
		b = b[:max]
	}
	return string(b)
}
