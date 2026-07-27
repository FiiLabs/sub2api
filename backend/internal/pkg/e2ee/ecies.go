// Package e2ee implements the ECIES scheme shared with the attestation
// sidecar (deploy/attestor/server.mjs) and the /proof frontend
// (frontend/src/utils/attestation/e2ee.ts):
//
//	ephemeral secp256k1 ECDH + HKDF-SHA256 + AES-256-GCM
//
// Wire format: ephemeral_pubkey(65B uncompressed) || aes_nonce(12B) || ciphertext+tag.
// The shared secret is the 32-byte X coordinate of the ECDH point;
// AES key = HKDF-SHA256(ikm=shared_x, salt=nil, info=publicai.e2ee.v1.secp256k1).
// AAD is "v1|req|" for the request direction and "v1|resp|" for the response.
// All of this must stay byte-for-byte compatible with the JS side.
package e2ee

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/hkdf"
)

const (
	// Algo names the scheme; mirrors E2EE_ALGO in server.mjs.
	Algo = "secp256k1-aes-256-gcm-hkdf-sha256"

	// hkdfInfo mirrors E2EE_INFO in server.mjs.
	hkdfInfo = "publicai.e2ee.v1.secp256k1"

	pubKeyLen = 65 // uncompressed secp256k1 point
	nonceLen  = 12 // AES-GCM standard nonce
	tagLen    = 16 // AES-GCM tag
	minWire   = pubKeyLen + nonceLen + tagLen
)

// AAD constants; ASCII, mirror AAD_REQ / AAD_RESP in server.mjs.
var (
	AADRequest  = []byte("v1|req|")
	AADResponse = []byte("v1|resp|")
)

// deriveAESKey derives the 32-byte AES key from the ECDH shared X coordinate.
func deriveAESKey(sharedX []byte) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sharedX, nil, []byte(hkdfInfo)), key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return key, nil
}

func newGCM(sharedX []byte) (cipher.AEAD, error) {
	key, err := deriveAESKey(sharedX)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt seals plaintext to peerPub (65-byte uncompressed secp256k1 key)
// with a fresh ephemeral key, returning the ECIES wire blob.
func Encrypt(peerPub []byte, plaintext, aad []byte) ([]byte, error) {
	pub, err := ParsePublicKey(peerPub)
	if err != nil {
		return nil, err
	}
	ephPriv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	// GenerateSharedSecret returns the 32-byte X coordinate of priv*pub,
	// identical to noble's getSharedSecret(...)[1..33] on the JS side.
	sharedX := secp256k1.GenerateSharedSecret(ephPriv, pub)
	aead, err := newGCM(sharedX)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := make([]byte, 0, minWire+len(plaintext))
	out = append(out, ephPriv.PubKey().SerializeUncompressed()...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad), nil
}

// Decrypt opens an ECIES wire blob with the recipient private key.
func Decrypt(priv *secp256k1.PrivateKey, wire, aad []byte) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("nil private key")
	}
	if len(wire) < minWire {
		return nil, errors.New("ciphertext too short")
	}
	ephPub, err := ParsePublicKey(wire[:pubKeyLen])
	if err != nil {
		return nil, fmt.Errorf("ephemeral pubkey: %w", err)
	}
	sharedX := secp256k1.GenerateSharedSecret(priv, ephPub)
	aead, err := newGCM(sharedX)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, wire[pubKeyLen:pubKeyLen+nonceLen], wire[pubKeyLen+nonceLen:], aad)
	if err != nil {
		return nil, errors.New("decryption failed")
	}
	return plaintext, nil
}

// ParsePublicKey validates a 65-byte uncompressed secp256k1 public key
// (rejects the point at infinity and off-curve points).
func ParsePublicKey(raw []byte) (*secp256k1.PublicKey, error) {
	if len(raw) != pubKeyLen || raw[0] != 0x04 {
		return nil, errors.New("public key must be a 65-byte uncompressed secp256k1 point")
	}
	pub, err := secp256k1.ParsePubKey(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}
	return pub, nil
}

// PrivateKeyFromSeed normalizes 32 seed bytes into a valid secp256k1 scalar
// exactly like server.mjs: while !isValidPrivateKey(d) { d = sha256(d) }.
func PrivateKeyFromSeed(seed []byte) (*secp256k1.PrivateKey, error) {
	if len(seed) < 32 {
		return nil, fmt.Errorf("seed must be at least 32 bytes, got %d", len(seed))
	}
	d := make([]byte, 32)
	copy(d, seed[:32])
	for !isValidScalar(d) {
		sum := sha256.Sum256(d)
		copy(d, sum[:])
	}
	priv := secp256k1.PrivKeyFromBytes(d)
	return priv, nil
}

// isValidScalar reports whether d is in [1, n-1] (n = secp256k1 group order),
// matching noble's secp256k1.utils.isValidPrivateKey.
func isValidScalar(d []byte) bool {
	var s secp256k1.ModNScalar
	overflow := s.SetByteSlice(d)
	return !overflow && !s.IsZero()
}
