package e2ee

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Serves a fake dstack guest-agent on a unix socket and checks that the
// GetKey RPC matches the @phala/dstack-sdk wire format:
// POST /GetKey {"path","purpose","algorithm"} -> {"key":"<hex>",...}.
func TestDstackKeyProviderGetKeyRPC(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "dstack.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	// 32-byte seed, hex-encoded like the real guest-agent response.
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}

	var gotPath string
	var gotBody map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":             hex.EncodeToString(append(append([]byte(nil), seed...), 0xAA, 0xBB)), // extra bytes must be ignored
			"signature_chain": []string{"00"},
		})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	p := &DstackKeyProvider{socket: socket}
	priv, err := p.PrivateKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, "/GetKey", gotPath)
	require.Equal(t, map[string]string{
		"path":      "e2ee/v1",
		"purpose":   "encryption",
		"algorithm": "secp256k1",
	}, gotBody)

	expected, err := PrivateKeyFromSeed(seed)
	require.NoError(t, err)
	require.Equal(t, expected.Serialize(), priv.Serialize())

	// Cached: a second call must not re-hit the socket.
	_ = srv.Close()
	again, err := p.PrivateKey(context.Background())
	require.NoError(t, err)
	require.Equal(t, priv.Serialize(), again.Serialize())
}

func TestDstackKeyProviderSocketUnavailable(t *testing.T) {
	p := &DstackKeyProvider{socket: filepath.Join(t.TempDir(), "missing.sock")}
	_, err := p.PrivateKey(context.Background())
	require.Error(t, err)

	// Failures are not cached: still errors (and would retry the socket).
	_, err = p.PrivateKey(context.Background())
	require.Error(t, err)
}
